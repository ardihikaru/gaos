package main

import (
	"context"
	"encoding/json"
	"fmt"
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
func (s *ArcadeStore) AnswerQuestion(ctx context.Context, id, taskID string, choice int, text string) (Result, error) {
	return s.mutate(ctx, func() (Result, error) { return s.proj.AnswerQuestion(ctx, id, taskID, choice, text) })
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
func (s *ArcadeStore) AnswerExternal(ctx context.Context, id, externalID string, choice int, text string) (Result, error) {
	return s.mutate(ctx, func() (Result, error) { return s.proj.AnswerExternal(ctx, id, externalID, choice, text) })
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
