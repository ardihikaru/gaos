package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// The Blueprint in ArcadeDB (D-058, schema/11-blueprint.sql).
//
// The draft — policies and knowledge sources — lives in the record document on
// the Product vertex while a human edits it, beside the BRD (D-009: schema-less
// where humans write). Each PUBLISHED version is its own BlueprintVersion
// vertex, immutable and content-addressed, because that is what an attempt will
// pin (PINNED_TO) and what a machine reads. Injection materialises the draft as
// Policy and KnowledgeSource vertices, as it does the rest of the record.

// PublishBlueprint runs the rule on the projection, then persists the record
// and the new version in the mutation's one transaction.
func (s *ArcadeStore) PublishBlueprint(ctx context.Context, id string) (Result, error) {
	return s.mutate(ctx, func() (Result, error) { return s.proj.PublishBlueprint(ctx, id) })
}

// SyncBlueprint persists what the sync recorded: the workspace on the record
// document, and the events. The network call runs inside mutate, so two syncs
// of one product cannot interleave.
func (s *ArcadeStore) SyncBlueprint(ctx context.Context, id string) (Result, error) {
	return s.mutate(ctx, func() (Result, error) { return s.proj.SyncBlueprint(ctx, id) })
}

// The board reads the projection and moves through mutate, so a move's
// status change and its event reach the graph in one transaction.
func (s *ArcadeStore) Board(ctx context.Context, id string) (Board, error) {
	return s.proj.Board(ctx, id)
}
func (s *ArcadeStore) CardTiming(ctx context.Context, id, taskID string) (CardTiming, error) {
	return s.proj.CardTiming(ctx, id, taskID)
}

// ---- D-082: a card's conversation ----

// Conversation answers from the projection, then reads the prompt and reply
// bodies the boot load left in the database -- one bound-list query, without
// the store lock.
func (s *ArcadeStore) Conversation(ctx context.Context, id, taskID string) (Conversation, error) {
	c, err := s.proj.Conversation(ctx, id, taskID)
	if err != nil || c.State.Pruned {
		return c, err
	}
	var missing []string
	for _, a := range c.Attempts {
		if a.Prompt == "" {
			missing = append(missing, a.RunID+"-"+ArtifactPrompt)
		}
		if a.Reply == "" {
			missing = append(missing, a.RunID+"-"+ArtifactReply)
		}
	}
	if len(missing) == 0 {
		return c, nil
	}
	bodies, err := s.artifactBodies(ctx, missing)
	if err != nil {
		return c, err
	}
	for i := range c.Attempts {
		a := &c.Attempts[i]
		if a.Prompt == "" {
			a.Prompt = bodies[a.RunID+"-"+ArtifactPrompt]
		}
		if a.Reply == "" {
			a.Reply = bodies[a.RunID+"-"+ArtifactReply]
		}
	}
	return c, nil
}

// artifactBodies reads artifact bodies by id: only ids that exist come back.
func (s *ArcadeStore) artifactBodies(ctx context.Context, ids []string) (map[string]string, error) {
	var rows []struct {
		ID   string `json:"id"`
		Body string `json:"body"`
	}
	if err := s.arc.Query(ctx, `SELECT id, body FROM Artifact WHERE tenantId = :t AND id IN :ids`,
		map[string]any{"t": s.tenant, "ids": ids}, &rows); err != nil {
		return nil, err
	}
	out := make(map[string]string, len(rows))
	for _, r := range rows {
		out[r.ID] = r.Body
	}
	return out, nil
}

func (s *ArcadeStore) ArchiveConversation(ctx context.Context, id, taskID, actor string, archive bool) (Result, error) {
	return s.mutate(ctx, func() (Result, error) { return s.proj.ArchiveConversation(ctx, id, taskID, actor, archive) })
}

func (s *ArcadeStore) ArchivedConversations(ctx context.Context) (ArchiveView, error) {
	return s.proj.ArchivedConversations(ctx)
}

// pruneBudget bounds the bodies transaction, which runs while the projection's
// lock is held (design §3.7 step 3). A var so a test can shorten it.
var pruneBudget = 30 * time.Second

// PruneConversations runs the projection's prune with the database's halves:
// bodies not in memory are read from the graph; the emptied bodies and the
// certificates commit in one transaction before the projection changes; the
// sessions certificate goes through mutate like any other write.
//
// The store's own lock is taken around the commit and around the record, in
// the same order mutate takes it (this lock, then the projection's), and is
// not held while the gateway deletes sessions.
func (s *ArcadeStore) PruneConversations(ctx context.Context, actor string, cards []CardRef) (Result, error) {
	return s.proj.pruneWith(ctx, actor, cards, prunePlan{
		loadBodies: s.artifactBodies,
		guard: func() func() {
			s.mu.Lock()
			return s.mu.Unlock
		},
		commit: func(evs []Event, emptied []string) error {
			cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), pruneBudget)
			defer cancel()
			tx, err := s.arc.Begin(cctx)
			if err != nil {
				return err
			}
			defer tx.Rollback(cctx)
			for _, e := range evs {
				if err := s.writeEvent(cctx, tx, e); err != nil {
					return err
				}
			}
			for _, id := range emptied {
				if err := tx.Command(cctx, `UPDATE Artifact SET body = '' WHERE tenantId = :t AND id = :id`,
					map[string]any{"t": s.tenant, "id": id}, nil); err != nil {
					return err
				}
			}
			return tx.Commit(cctx)
		},
		record: func(actor string, recs map[CardRef]*PruneRecord) (Result, error) {
			s.mu.Lock()
			defer s.mu.Unlock()
			r := s.proj.recordSessions(actor, recs)
			return r, s.persist(ctx, r)
		},
	})
}

func (s *ArcadeStore) SendBackDesign(ctx context.Context, id, taskID, actor string, triage []FindingTriage) (Result, error) {
	return s.mutate(ctx, func() (Result, error) { return s.proj.SendBackDesign(ctx, id, taskID, actor, triage) })
}
func (s *ArcadeStore) SendBackSheet(ctx context.Context, id, taskID string) (SendBackView, error) {
	return s.proj.SendBackSheet(ctx, id, taskID)
}
func (s *ArcadeStore) MoveCard(ctx context.Context, id, taskID string, m CardMove) (Result, error) {
	return s.mutate(ctx, func() (Result, error) { return s.proj.MoveCard(ctx, id, taskID, m) })
}

// Questions are a projection of the event stream (questions.go); an answer is
// an event and a task move, so it goes through mutate like a board move.
func (s *ArcadeStore) OpenQuestions(ctx context.Context, id string) ([]Question, error) {
	return s.proj.OpenQuestions(ctx, id)
}
func (s *ArcadeStore) ExpireQuestions(ctx context.Context, now int64) (Result, error) {
	return s.mutate(ctx, func() (Result, error) { return s.proj.ExpireQuestions(ctx, now) })
}
func (s *ArcadeStore) AnswerQuestion(ctx context.Context, id, taskID string, choice int, text string, by AnswerSource) (Result, error) {
	return s.mutate(ctx, func() (Result, error) { return s.proj.AnswerQuestion(ctx, id, taskID, choice, text, by) })
}
func (s *ArcadeStore) RecentAnswers(ctx context.Context, id string, limit int) ([]RecentAnswer, error) {
	return s.proj.RecentAnswers(ctx, id, limit)
}

// The QA tool's inbox items (external_inbox.go, D-078) are events too: the ask,
// the answer or expiry and each delivery attempt persist with the events they
// emit, and load() rebuilds them from the stream.
func (s *ArcadeStore) AskExternal(ctx context.Context, id string, in ExternalAsk) (ExternalItem, bool, Result, error) {
	var (
		item    ExternalItem
		created bool
	)
	r, err := s.mutate(ctx, func() (Result, error) {
		x, c, r, err := s.proj.AskExternal(ctx, id, in)
		item, created = x, c
		return r, err
	})
	return item, created, r, err
}
func (s *ArcadeStore) ExternalGet(ctx context.Context, id, externalID string) (ExternalItem, error) {
	return s.proj.ExternalGet(ctx, id, externalID)
}
func (s *ArcadeStore) AnswerExternal(ctx context.Context, id, externalID string, in ExternalAnswer) (Result, error) {
	return s.mutate(ctx, func() (Result, error) { return s.proj.AnswerExternal(ctx, id, externalID, in) })
}
func (s *ArcadeStore) ExternalPending(ctx context.Context) ([]ExternalItem, error) {
	return s.proj.ExternalPending(ctx)
}
func (s *ArcadeStore) RecordExternalDelivery(ctx context.Context, id, externalID string, attempt int, outcome, reason string, at int64) (Result, error) {
	return s.mutate(ctx, func() (Result, error) {
		return s.proj.RecordExternalDelivery(ctx, id, externalID, attempt, outcome, reason, at)
	})
}

func blueprintVersionID(productID string, n int) string { return fmt.Sprintf("%s-bp-%d", productID, n) }

// writeBlueprintVersion writes the newest version, once. Older versions were
// written when they were published and never change, so rewriting them on
// every record edit would only move their contents around for nothing.
func (s *ArcadeStore) writeBlueprintVersion(ctx context.Context, tx *ArcadeTx, m Master) error {
	v, ok := m.Blueprint.Latest()
	if !ok {
		return nil
	}
	files, err := jsonList(v.Files)
	if err != nil {
		return err
	}
	id := blueprintVersionID(m.ID, v.N)
	if err := s.upsert(ctx, tx, "BlueprintVersion", id, map[string]any{
		"productId": m.ID, "n": v.N, "digest": v.Digest, "files": files,
		"publishedAt": unixStamp(v.PublishedAt),
	}); err != nil {
		return err
	}
	return s.edge(ctx, tx, "HAS_BLUEPRINT_VERSION", "Product", m.ID, "BlueprintVersion", id)
}

// loadBlueprintVersions reads every version back, grouped by product.
func (s *ArcadeStore) loadBlueprintVersions(ctx context.Context) (map[string][]BlueprintVersion, error) {
	var rows []struct {
		ProductID   string          `json:"productId"`
		N           int             `json:"n"`
		Digest      string          `json:"digest"`
		Files       json.RawMessage `json:"files"`
		PublishedAt string          `json:"publishedAt"`
	}
	if err := s.arc.Query(ctx,
		`SELECT productId, n, digest, files, publishedAt FROM BlueprintVersion
		 WHERE tenantId = :t ORDER BY productId, n`,
		map[string]any{"t": s.tenant}, &rows); err != nil {
		return nil, err
	}
	out := map[string][]BlueprintVersion{}
	for _, r := range rows {
		v := BlueprintVersion{N: r.N, Digest: r.Digest, PublishedAt: parseStamp(r.PublishedAt)}
		if err := decodeInto(r.Files, &v.Files); err != nil {
			return nil, err
		}
		out[r.ProductID] = append(out[r.ProductID], v)
	}
	return out, nil
}

// injectBlueprint materialises the draft as vertices: one Policy per policy,
// one KnowledgeSource per source, each linked from the Product.
func (s *ArcadeStore) injectBlueprint(ctx context.Context, tx *ArcadeTx, m Master) error {
	for _, p := range m.Blueprint.Policies {
		id := m.ID + "-policy-" + p.Key
		if err := s.optional(s.upsert(ctx, tx, "Policy", id, map[string]any{
			"productId": m.ID, "key": p.Key, "kind": p.Kind, "title": p.Title, "body": p.Body, "order": p.Order,
		})); err != nil {
			return err
		}
		if err := s.optional(s.edge(ctx, tx, "HAS_POLICY", "Product", m.ID, "Policy", id)); err != nil {
			return err
		}
	}
	for i, k := range m.Blueprint.Sources {
		id := fmt.Sprintf("%s-ks-%d", m.ID, i+1)
		purpose := k.Purpose
		if purpose == "" {
			purpose = "reference"
		}
		if err := s.optional(s.upsert(ctx, tx, "KnowledgeSource", id, map[string]any{
			"productId": m.ID, "kind": "git-repo", "url": k.URL, "ref": k.Ref, "purpose": purpose, "note": k.Note,
		})); err != nil {
			return err
		}
		if err := s.optional(s.edge(ctx, tx, "HAS_KNOWLEDGE_SOURCE", "Product", m.ID, "KnowledgeSource", id)); err != nil {
			return err
		}
	}
	return nil
}
