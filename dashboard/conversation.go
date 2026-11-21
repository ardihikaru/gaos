package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

// A card's conversation: every attempt's prompt and reply, its question and
// the operator's answer; archive, restore and prune. D-082,
// docs/designs/conversations.md.
//
// The conversation is a projection, like the inbox: prompt and reply are the
// artifacts the runner records per attempt (runner.go), the question and the
// answer are events, and the archive state and the prune certificates are
// two events of their own. Nothing here is a second source of truth.
//
// What prune empties, and what it does not, is the design's §3.7, and the
// certificate says it again in its Kept list: a prune empties documents and
// deletes gateway sessions; it never rewrites an event (BR-04), a run record
// (BR-22) or a context slice (BR-32).

// CardRef names one card.
type CardRef struct{ ProductID, TaskID string }

// Conversation is one card's conversation.
type Conversation struct {
	ProductID, ProductName, TaskID, TaskTitle, Status string
	State                                             ConversationState
	Attempts                                          []ConversationAttempt
}

// ConversationState is a card's archive state, folded from its events.
type ConversationState struct {
	Archived   bool
	ArchivedAt int64
	ArchivedBy string
	Pruned     bool
	PrunedAt   int64
	PrunedBy   string
	Bodies     *PruneRecord // the bodies certificate, once pruned
	Sessions   *PruneRecord // the sessions certificate, once recorded
}

// ConversationAttempt is one attempt of the card.
type ConversationAttempt struct {
	Attempt            int
	RunID, Status      string
	Model, Provider    string
	StartedAt, EndedAt int64
	Prompt, Reply      string
	Question           *QuestionPayload
}

// PruneRecord is a prune's certificate (L-8). Stage "bodies" is written in
// the same transaction that empties the documents; stage "sessions" after the
// gateway was asked to delete the sessions. No session id is ever in it.
type PruneRecord struct {
	Stage           string       `json:"stage"`
	Runs            []string     `json:"runs,omitempty"`
	Bodies          []PrunedBody `json:"bodies,omitempty"`
	Kept            []string     `json:"kept,omitempty"`
	SessionsNamed   int          `json:"sessionsNamed,omitempty"`
	SessionsDeleted int          `json:"sessionsDeleted,omitempty"`
	SessionsGone    int          `json:"sessionsGone,omitempty"`
	SessionsFailed  int          `json:"sessionsFailed,omitempty"`
	SessionsShared  int          `json:"sessionsShared,omitempty"`
}

// PrunedBody is one emptied document: what it was, by digest and size.
type PrunedBody struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	SHA256 string `json:"sha256"`
	Bytes  int    `json:"bytes"`
}

// pruneKept is what survives a prune, by name -- the certificate's half of
// L-8 ("disclosed rather than discovered"). Fixed, and asserted by TC-483.
var pruneKept = []string{
	"events, including the full question and answer",
	"run records",
	"context slices, including the prompt's packet content",
	"artifact rows (id, kind, title, run, attempt)",
	"handoff, design and review artifacts",
	"gateway question records and turn results, until the gateway expires them",
	"database backups and exports taken before this prune",
	"this certificate",
}

// The prune's typed confirmation and its form text (design §3.6), one place.
const (
	pruneConfirmWord = "PRUNE"
	pruneFormText    = "Prune empties the stored prompt, reply and question documents of these conversations and deletes " +
		"their gateway sessions. It cannot be undone. What survives: the events — including each question and your " +
		"full answer, which stay on the product Timeline; the context slices — the packet each prompt was built from; " +
		"the run records; the handoff, design and review documents; the gateway's own question records and turn " +
		"results until it expires them; and any backup taken before now. Each card gets a certificate of what was emptied."
)

// SessionJanitor is what an adapter offers when it can delete a gateway
// session (D-082). A capability asserted by type, like WorkspaceGateway.
type SessionJanitor interface {
	DeleteSession(ctx context.Context, vendor, sessionID string) error
}

// The session janitor's answers. Bare sentinels: none of them names the
// session, because a session id lets its holder continue the conversation.
var (
	ErrSessionBusy     = errors.New("ai-manager: the session is in use (409)")
	ErrSessionNotFound = errors.New("ai-manager: no such session to delete (404)")
	ErrNoAdminKey      = errors.New("ai-manager: no admin key")
)

var sessionUUID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// ---------- the pure projections ----------

// conversationOf builds one card's conversation. Pure. Only events of the
// card's own product count.
func conversationOf(t TaskRecord, runs []TaskRunRecord, arts map[string]ArtifactRecord, events []Event) Conversation {
	c := Conversation{ProductID: t.DeskID, TaskID: t.ID, TaskTitle: t.Title, Status: t.Status}
	c.State = conversationStateOf(t, events)
	asked := map[string]*QuestionPayload{}    // by run id
	answered := map[string]*QuestionPayload{} // by question id
	for _, e := range events {
		if e.DeskID != t.DeskID || e.Payload.Task != t.ID || e.Payload.Question == nil {
			continue
		}
		q := *e.Payload.Question
		switch e.Type {
		case QuestionAsked:
			asked[q.RunID] = &q
		case QuestionAnswered:
			answered[q.ID] = &q
		}
	}
	rs := append([]TaskRunRecord(nil), runs...)
	sort.SliceStable(rs, func(i, j int) bool { return rs[i].Attempt < rs[j].Attempt })
	for _, r := range rs {
		if r.TaskID != t.ID {
			continue
		}
		a := ConversationAttempt{Attempt: r.Attempt, RunID: r.ID, Status: r.Status, Model: r.Model,
			Provider: runVendor(r), StartedAt: r.StartedAt, EndedAt: r.EndedAt,
			Prompt: arts[r.ID+"-"+ArtifactPrompt].Body, Reply: arts[r.ID+"-"+ArtifactReply].Body}
		if q := asked[r.ID]; q != nil {
			if ans := answered[q.ID]; ans != nil {
				q = ans
			}
			a.Question = q
		}
		c.Attempts = append(c.Attempts, a)
	}
	return c
}

// conversationStateOf folds a card's archive and prune events. Pure.
func conversationStateOf(t TaskRecord, events []Event) ConversationState {
	evs := append([]Event(nil), events...)
	sort.SliceStable(evs, func(i, j int) bool { return evs[i].Seq < evs[j].Seq })
	var st ConversationState
	for _, e := range evs {
		if e.DeskID != t.DeskID || e.Payload.Task != t.ID {
			continue
		}
		switch e.Type {
		case ConversationArchived:
			if e.Payload.To == "archived" {
				st.Archived, st.ArchivedAt, st.ArchivedBy = true, e.At, e.Payload.Actor
			} else {
				st.Archived = false
			}
		case ConversationPruned:
			if e.Payload.Prune == nil {
				continue
			}
			p := *e.Payload.Prune
			if p.Stage == "sessions" {
				st.Sessions = &p
				continue
			}
			st.Pruned, st.PrunedAt, st.PrunedBy, st.Bodies = true, e.At, e.Payload.Actor, &p
			st.Archived = false
		}
	}
	return st
}

func actorName(a string) string {
	if a == "" {
		return "operator"
	}
	return a
}

// ---------- MemStore ----------

func (s *MemStore) cardLocked(productID, taskID string) *TaskRecord {
	for i := range s.tasks {
		if s.tasks[i].ID == taskID && s.tasks[i].DeskID == productID {
			return &s.tasks[i]
		}
	}
	return nil
}

func (s *MemStore) productNameLocked(id string) string {
	for _, p := range s.products {
		if p.ID == id {
			return p.Name
		}
	}
	return id
}

// conversationLocked is one card's conversation from memory. Caller holds s.mu.
func (s *MemStore) conversationLocked(t TaskRecord) Conversation {
	arts := map[string]ArtifactRecord{}
	runs := s.runsFor(t.ID)
	for _, r := range runs {
		for _, k := range []string{ArtifactPrompt, ArtifactReply} {
			if a, ok := s.artifacts[r.ID+"-"+k]; ok {
				arts[a.ID] = a
			}
		}
	}
	c := conversationOf(t, runs, arts, s.taskEventsLocked(t.ID))
	c.ProductName = s.productNameLocked(t.DeskID)
	return c
}

// Conversation is one card's conversation (Store).
func (s *MemStore) Conversation(ctx context.Context, productID, taskID string) (Conversation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t := s.cardLocked(productID, taskID)
	if t == nil {
		return Conversation{}, ErrNotFound
	}
	return s.conversationLocked(*t), nil
}

// hasOpenRunLocked: an attempt of the task is still STARTED or RUNNING.
func (s *MemStore) hasOpenRunLocked(taskID string) bool {
	for _, r := range s.runs {
		if r.TaskID == taskID && (r.Status == RunStatusStarted || r.Status == RunStatusRunning) {
			return true
		}
	}
	return false
}

// ArchiveConversation archives (or restores) a finished card's conversation.
func (s *MemStore) ArchiveConversation(ctx context.Context, productID, taskID, actor string, archive bool) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.cardLocked(productID, taskID)
	if t == nil {
		return Result{}, ErrNotFound
	}
	if len(s.runsFor(taskID)) == 0 {
		return Result{}, reject(taskID + " has no conversation yet.")
	}
	if !terminal(t.Status) || s.hasOpenRunLocked(taskID) {
		return Result{}, reject(fmt.Sprintf("%s is %s; a conversation is archived when its card is done or canceled and no attempt is running.", taskID, t.Status))
	}
	st := conversationStateOf(*t, s.taskEventsLocked(taskID))
	switch {
	case st.Pruned:
		return Result{}, reject(taskID + "'s conversation was pruned; there is nothing to archive or restore.")
	case archive && st.Archived:
		return Result{}, reject(taskID + "'s conversation is already archived.")
	case !archive && !st.Archived:
		return Result{}, reject(taskID + "'s conversation is not archived.")
	}
	to, verb, toast := "archived", "archived", "Conversation archived — find it on the Archive page."
	if !archive {
		to, verb, toast = "active", "restored", "Conversation restored to "+taskID+"."
	}
	e := s.emit(ConversationArchived, productID, fmt.Sprintf("%s: conversation %s by %s", taskID, verb, actorName(actor)),
		KindInfo, EventPayload{Task: taskID, To: to, Actor: actor})
	return s.result(toast, e), nil
}

// ArchivedRow is one archived conversation on the Archive page.
type ArchivedRow struct {
	ProductID, ProductName, TaskID, TaskTitle string
	Attempts                                  int
	ArchivedAt                                int64
	ArchivedBy                                string
}

// PruneRow is one pruned card's certificates.
type PruneRow struct {
	ProductID, ProductName, TaskID, TaskTitle string
	PrunedAt                                  int64
	PrunedBy                                  string
	Bodies, Sessions                          *PruneRecord
}

// ArchiveView is the Archive page's data.
type ArchiveView struct {
	Archived []ArchivedRow
	Pruned   []PruneRow
}

// ArchivedConversations folds every archive and prune event (Store).
func (s *MemStore) ArchivedConversations(ctx context.Context) (ArchiveView, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.archiveViewLocked(), nil
}

func (s *MemStore) archiveViewLocked() ArchiveView {
	byCard := map[CardRef][]Event{}
	var order []CardRef
	for _, e := range s.conversationEventsLocked() {
		k := CardRef{e.DeskID, e.Payload.Task}
		if _, ok := byCard[k]; !ok {
			order = append(order, k)
		}
		byCard[k] = append(byCard[k], e)
	}
	v := ArchiveView{Archived: []ArchivedRow{}, Pruned: []PruneRow{}}
	for _, k := range order {
		t := s.cardLocked(k.ProductID, k.TaskID)
		if t == nil {
			continue
		}
		st := conversationStateOf(*t, byCard[k])
		name := s.productNameLocked(k.ProductID)
		switch {
		case st.Archived:
			v.Archived = append(v.Archived, ArchivedRow{ProductID: k.ProductID, ProductName: name, TaskID: k.TaskID,
				TaskTitle: t.Title, Attempts: len(s.runsFor(k.TaskID)), ArchivedAt: st.ArchivedAt, ArchivedBy: st.ArchivedBy})
		case st.Pruned:
			v.Pruned = append(v.Pruned, PruneRow{ProductID: k.ProductID, ProductName: name, TaskID: k.TaskID,
				TaskTitle: t.Title, PrunedAt: st.PrunedAt, PrunedBy: st.PrunedBy, Bodies: st.Bodies, Sessions: st.Sessions})
		}
	}
	sort.SliceStable(v.Archived, func(i, j int) bool { return v.Archived[i].ArchivedAt > v.Archived[j].ArchivedAt })
	sort.SliceStable(v.Pruned, func(i, j int) bool { return v.Pruned[i].PrunedAt > v.Pruned[j].PrunedAt })
	return v
}

// ---------- prune ----------

type sessionRef struct{ Vendor, ID string }

type pruneTarget struct {
	Card     CardRef
	Runs     []string
	IDs      []string // candidate documents: <run>-prompt, -reply, -question
	Sessions []sessionRef
	Shared   int
	Named    int
}

// pruneSelectLocked picks the listed cards that are archived and not pruned.
// Sessions also named by a run of a task outside the selection are kept and
// counted. Caller holds s.mu.
func (s *MemStore) pruneSelectLocked(cards []CardRef) []pruneTarget {
	selected := map[string]bool{}
	var out []pruneTarget
	for _, c := range cards {
		t := s.cardLocked(c.ProductID, c.TaskID)
		if t == nil || selected[t.ID] {
			continue
		}
		st := conversationStateOf(*t, s.taskEventsLocked(t.ID))
		if !st.Archived || st.Pruned {
			continue
		}
		selected[t.ID] = true
		out = append(out, pruneTarget{Card: c})
	}
	for k := range out {
		tg := &out[k]
		seen := map[string]bool{}
		for _, r := range s.runsFor(tg.Card.TaskID) {
			tg.Runs = append(tg.Runs, r.ID)
			for _, kind := range []string{ArtifactPrompt, ArtifactReply, ArtifactQuestion} {
				tg.IDs = append(tg.IDs, r.ID+"-"+kind)
			}
			if r.SessionID == "" || seen[r.SessionID] {
				continue
			}
			seen[r.SessionID] = true
			tg.Named++
			shared := false
			for _, o := range s.runs {
				if o.SessionID == r.SessionID && !selected[o.TaskID] {
					shared = true
				}
			}
			if shared {
				tg.Shared++
				continue
			}
			tg.Sessions = append(tg.Sessions, sessionRef{Vendor: runVendor(r), ID: r.SessionID})
		}
	}
	return out
}

func kindOfArtifactID(id string) string {
	for _, k := range []string{ArtifactPrompt, ArtifactReply, ArtifactQuestion} {
		if strings.HasSuffix(id, "-"+k) {
			return k
		}
	}
	return ""
}

func bodyDigest(body string) string {
	h := sha256.Sum256([]byte(body))
	return hex.EncodeToString(h[:])
}

// pruneCommit empties the selected cards' documents and writes one bodies
// certificate per card -- in memory only if commit (the database's half, nil
// for the in-memory store) succeeds. The events are built with their sequence
// numbers first and appended after the commit, so a failed commit leaves the
// projection exactly as it was. bodies holds every existing document's text,
// read before the lock (step 2). Returns the committed targets.
func (s *MemStore) pruneCommit(actor string, targets []pruneTarget, bodies map[string]string,
	commit func(evs []Event, emptied []string) error) (Result, []pruneTarget, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var keep []pruneTarget
	var evs []Event
	var emptied []string
	now := time.Now().Unix()
	for _, tg := range targets {
		t := s.cardLocked(tg.Card.ProductID, tg.Card.TaskID)
		if t == nil {
			continue
		}
		if st := conversationStateOf(*t, s.taskEventsLocked(t.ID)); !st.Archived || st.Pruned {
			continue // restored or pruned since the selection: skipped
		}
		rec := &PruneRecord{Stage: "bodies", Runs: append([]string(nil), tg.Runs...), Kept: append([]string(nil), pruneKept...)}
		for _, id := range tg.IDs {
			body, ok := bodies[id]
			if !ok {
				continue // never recorded: nothing to empty, nothing certified
			}
			rec.Bodies = append(rec.Bodies, PrunedBody{ID: id, Kind: kindOfArtifactID(id), SHA256: bodyDigest(body), Bytes: len(body)})
			emptied = append(emptied, id)
		}
		e := Event{Seq: s.seq + len(evs) + 1, Type: ConversationPruned, DeskID: tg.Card.ProductID,
			Text: fmt.Sprintf("%s: conversation pruned by %s — %d documents emptied", tg.Card.TaskID, actorName(actor), len(rec.Bodies)),
			Kind: KindInfo, T: s.clock, At: now,
			Payload: EventPayload{Task: tg.Card.TaskID, Actor: actor, Prune: rec}}
		evs = append(evs, e)
		keep = append(keep, tg)
	}
	if len(evs) == 0 {
		return Result{}, nil, nil
	}
	if commit != nil {
		if err := commit(evs, emptied); err != nil {
			return Result{}, nil, err
		}
	}
	for _, e := range evs {
		s.appendPreparedLocked(e)
	}
	out := evs
	for _, id := range emptied {
		if a, ok := s.artifacts[id]; ok {
			a.Body = ""
			s.artifacts[id] = a
		}
	}
	return s.result("", out...), keep, nil
}

// deleteSessions asks the gateway to delete each committed card's sessions,
// one at a time, with no store lock held. Never logs or returns an id.
func (s *MemStore) deleteSessions(ctx context.Context, targets []pruneTarget) map[CardRef]*PruneRecord {
	s.mu.RLock()
	jan, _ := s.adapter.(SessionJanitor)
	s.mu.RUnlock()
	out := map[CardRef]*PruneRecord{}
	deadline := time.Now().Add(60 * time.Second)
	for _, tg := range targets {
		rec := &PruneRecord{Stage: "sessions", SessionsNamed: tg.Named, SessionsShared: tg.Shared}
		for _, ref := range tg.Sessions {
			if jan == nil || time.Now().After(deadline) || !sessionUUID.MatchString(ref.ID) ||
				(ref.Vendor != VendorClaude && ref.Vendor != VendorAntigravity) {
				rec.SessionsFailed++
				continue
			}
			cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := jan.DeleteSession(cctx, ref.Vendor, ref.ID)
			cancel()
			switch {
			case err == nil:
				rec.SessionsDeleted++
			case errors.Is(err, ErrSessionNotFound):
				rec.SessionsGone++
			default:
				rec.SessionsFailed++
			}
		}
		out[tg.Card] = rec
	}
	return out
}

// recordSessions writes the sessions certificate of each committed card.
func (s *MemStore) recordSessions(actor string, recs map[CardRef]*PruneRecord) Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	var evs []Event
	cards := make([]CardRef, 0, len(recs))
	for c := range recs {
		cards = append(cards, c)
	}
	sort.Slice(cards, func(i, j int) bool { return cards[i].TaskID < cards[j].TaskID })
	for _, c := range cards {
		r := recs[c]
		evs = append(evs, s.emit(ConversationPruned, c.ProductID,
			fmt.Sprintf("%s: %d of %d gateway sessions deleted, %d already gone, %d failed, %d kept (shared)",
				c.TaskID, r.SessionsDeleted, r.SessionsNamed, r.SessionsGone, r.SessionsFailed, r.SessionsShared),
			KindInfo, EventPayload{Task: c.TaskID, Actor: actor, Prune: r}))
	}
	return s.result("", evs...)
}

// prunePlan is the store-specific half of a prune: where bodies not in memory
// come from, how the bodies commit reaches the database, and how the
// sessions certificate is written.
type prunePlan struct {
	loadBodies func(ctx context.Context, ids []string) (map[string]string, error)
	commit     func(evs []Event, emptied []string) error
	guard      func() (release func()) // held around the commit; nil for the in-memory store
	record     func(actor string, recs map[CardRef]*PruneRecord) (Result, error)
}

// pruneWith runs the five steps of design §3.7.
func (s *MemStore) pruneWith(ctx context.Context, actor string, cards []CardRef, plan prunePlan) (Result, error) {
	// 1. select
	s.mu.RLock()
	targets := s.pruneSelectLocked(cards)
	bodies := map[string]string{}
	var missing []string
	for _, tg := range targets {
		for _, id := range tg.IDs {
			if a, ok := s.artifacts[id]; ok {
				bodies[id] = a.Body
			} else {
				missing = append(missing, id)
			}
		}
	}
	s.mu.RUnlock()
	skipped := len(cards) - len(targets)
	// 2. read bodies not in memory
	if len(missing) > 0 && plan.loadBodies != nil {
		more, err := plan.loadBodies(ctx, missing)
		if err != nil {
			return Result{}, err
		}
		for id, b := range more {
			bodies[id] = b
		}
	}
	// 3. commit bodies and certificates together
	release := func() {}
	if plan.guard != nil {
		release = plan.guard()
	}
	res, committed, err := s.pruneCommit(actor, targets, bodies, plan.commit)
	release()
	if err != nil {
		return Result{}, err
	}
	skipped += len(targets) - len(committed)
	if len(committed) == 0 {
		res.Toast = fmt.Sprintf("Nothing was pruned; %d skipped (no longer archived).", skipped)
		return res, nil
	}
	// 4. delete sessions, 5. record them
	recs := s.deleteSessions(ctx, committed)
	if plan.record != nil {
		r2, err := plan.record(actor, recs)
		if err != nil {
			log.Printf("prune: sessions outcome not recorded for %d cards", len(recs))
		} else {
			res.NewEvents = append(res.NewEvents, r2.NewEvents...)
			res.Floor = r2.Floor
		}
	}
	res.Toast = fmt.Sprintf("Pruned %d conversation(s); %d skipped (no longer archived).", len(committed), skipped)
	return res, nil
}

// PruneConversations prunes the listed archived conversations (Store).
func (s *MemStore) PruneConversations(ctx context.Context, actor string, cards []CardRef) (Result, error) {
	return s.pruneWith(ctx, actor, cards, prunePlan{
		record: func(actor string, recs map[CardRef]*PruneRecord) (Result, error) {
			return s.recordSessions(actor, recs), nil
		},
	})
}

// decorateBoardConversation marks each card that has attempts (D-082).
// Caller holds s.mu.
func (s *MemStore) decorateBoardConversation(b *Board, productID string) {
	n := map[string]int{}
	for _, r := range s.runs {
		n[r.TaskID]++
	}
	for i := range b.Cards {
		c := &b.Cards[i]
		if n[c.ID] == 0 {
			continue
		}
		t := s.cardLocked(productID, c.ID)
		if t == nil {
			continue
		}
		st := conversationStateOf(*t, s.taskEventsLocked(c.ID))
		c.Conversation = &BoardConversation{Attempts: n[c.ID], Archived: st.Archived, Pruned: st.Pruned}
	}
}

// ---------- the sheet's view model ----------

// ConversationView is the conversation sheet (templates/needs.html).
type ConversationView struct {
	Conversation
	Mode       string // active | archived | pruned
	CanArchive bool
	ArchivedAt string
	PrunedAt   string
	Attempts   []AttemptView
	Emptied    []string // pruned mode: one line per emptied document
	Survives   string
	Sessions   string
	ArchiveURL string
	RestoreURL string
}

type AttemptView struct {
	ConversationAttempt
	Started  string
	Prompt   template.HTML
	Reply    template.HTML
	Answer   string // "" when not answered
	Answered string
	Options  []string
}

func buildConversationView(c Conversation, canArchive bool) *ConversationView {
	v := &ConversationView{Conversation: c, Mode: "active", CanArchive: canArchive, Survives: pruneFormText}
	switch {
	case c.State.Pruned:
		v.Mode = "pruned"
	case c.State.Archived:
		v.Mode = "archived"
	}
	v.ArchivedAt, v.PrunedAt = absTime(c.State.ArchivedAt), absTime(c.State.PrunedAt)
	base := "products/" + url.PathEscape(c.ProductID) + "/board/cards/" + url.PathEscape(c.TaskID) + "/conversation/"
	v.ArchiveURL, v.RestoreURL = base+"archive", base+"restore"
	if b := c.State.Bodies; b != nil {
		for _, d := range b.Bodies {
			v.Emptied = append(v.Emptied, fmt.Sprintf("%s: %d bytes emptied, SHA-256 %s", d.Kind, d.Bytes, d.SHA256[:12]))
		}
	}
	if r := c.State.Sessions; r != nil {
		v.Sessions = fmt.Sprintf("Gateway sessions: %d deleted, %d already gone, %d failed, %d kept (shared) of %d.",
			r.SessionsDeleted, r.SessionsGone, r.SessionsFailed, r.SessionsShared, r.SessionsNamed)
	} else if c.State.Pruned {
		v.Sessions = "Session outcome not recorded."
	}
	for _, a := range c.Attempts {
		av := AttemptView{ConversationAttempt: a, Started: absTime(a.StartedAt)}
		if v.Mode != "pruned" {
			if a.Prompt != "" {
				av.Prompt = renderMarkdown(a.Prompt)
			}
			if a.Reply != "" {
				av.Reply = renderMarkdown(a.Reply)
			}
		}
		if q := a.Question; q != nil && v.Mode != "pruned" {
			for _, o := range q.Options {
				av.Options = append(av.Options, o.Label)
			}
			if q.AnsweredAt > 0 {
				label := "in your own words"
				if q.Choice >= 1 && q.Choice <= len(q.Options) {
					label = q.Options[q.Choice-1].Label
				}
				av.Answer = label
				if q.Text != "" {
					av.Answer += ": " + q.Text
				}
				av.Answered = absTime(q.AnsweredAt)
			}
		}
		v.Attempts = append(v.Attempts, av)
	}
	return v
}

// appendPreparedLocked appends an event built ahead of its commit, exactly as
// it was written to the database: its sequence number was allocated as
// s.seq+n while this lock was held, so nothing can have taken it. The tail of
// emit, without emit's re-stamping. Caller holds s.mu.
func (s *MemStore) appendPreparedLocked(e Event) {
	e.Payload = e.Payload.Clone()
	s.seq = e.Seq
	if needsNotice(e) {
		s.notifs++
	}
	s.events = append(s.events, e)
	s.live.Publish(e)
	if e.Seq%snapshotEvery == 0 {
		s.snaps = append(s.snaps, floorSnapshot{UptoSeq: e.Seq, State: s.foldTo(e.Seq)})
	}
}
