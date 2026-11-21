package main

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Card timing. D-081, docs/designs/card-timing.md.
//
// How long a card spent in each status, which move took it there, and how much
// of its lead time no AI was working on it. Everything here is DERIVED from
// the event stream -- the task's TaskPlanned and TaskMoved events -- and its
// run records. Nothing is stored: a stored rollup would be a second source of
// truth for something the stream already says (D-001).
//
// The clock is Event.At, the app's wall clock at emit. Rows written before
// v0.41.0 have none and read the database's ts instead (store_arcade.go,
// load), corrected by the database's zone (tzOffset).

// Segment buckets: every second of a card's lead time lands in exactly one.
const (
	BucketAI       = "ai"       // DISPATCHED, RUNNING: a turn is in flight
	BucketOperator = "operator" // AWAITING_INPUT, REVIEW, FAILED; READY unless the runner is on
	BucketQueued   = "queued"   // READY under GAOS_RUNNER=on: the scheduler dispatches it
	BucketBlocked  = "blocked"  // BLOCKED, DEFERRED
	BucketBacklog  = "backlog"  // PENDING
	BucketDone     = "done"     // DONE, CANCELED: terminal, always 0 s
)

// CardTimingSummary is what every card carries on the board.
type CardTimingSummary struct {
	Known                  bool    `json:"known"`
	Complete               bool    `json:"complete"`
	Reason                 string  `json:"reason,omitempty"`
	Status                 string  `json:"status"`
	Finished               bool    `json:"finished"`
	CreatedAt              int64   `json:"createdAt"`
	FinishedAt             int64   `json:"finishedAt,omitempty"`
	AsOf                   int64   `json:"asOf"`
	LeadSeconds            int64   `json:"leadSeconds"`
	AISeconds              int64   `json:"aiSeconds"`
	IdleSeconds            int64   `json:"idleSeconds"`
	WaitingOperatorSeconds int64   `json:"waitingOperatorSeconds"`
	QueuedSeconds          int64   `json:"queuedSeconds"`
	BlockedSeconds         int64   `json:"blockedSeconds"`
	BacklogSeconds         int64   `json:"backlogSeconds"`
	Attempts               int     `json:"attempts"`
	TokensIn               int64   `json:"tokensIn"`
	TokensOut              int64   `json:"tokensOut"`
	CostMeasuredUSD        float64 `json:"costMeasuredUsd"`
	CostEstimatedUSD       float64 `json:"costEstimatedUsd"`
	CostUnavailable        int     `json:"costUnavailable"`
	LeadText               string  `json:"leadText"`
	TitleText              string  `json:"title"`
}

// TimingSegment is one stay in one status.
type TimingSegment struct {
	Status    string `json:"status"`
	From      string `json:"from"`
	Rule      string `json:"rule"`
	EnteredAt int64  `json:"enteredAt"`
	LeftAt    int64  `json:"leftAt"`
	Seconds   int64  `json:"seconds"`
	Open      bool   `json:"open"`
	Bucket    string `json:"bucket"`
}

// AttemptTiming is one attempt, as the HTTP surface may show it. It is an
// ALLOWLIST built field by field from TaskRunRecord: the session id, request
// id, log ref, workspace, slice digest, failure signature, invocation, pinned
// version and exit code never leave the store through it (SEC-1 of the
// design's review).
type AttemptTiming struct {
	Attempt   int     `json:"attempt"`
	Status    string  `json:"status"`
	Model     string  `json:"model"`
	Provider  string  `json:"provider"`
	StartedAt int64   `json:"startedAt"`
	EndedAt   int64   `json:"endedAt,omitempty"`
	Seconds   int64   `json:"seconds"`
	TokensIn  int64   `json:"tokensIn"`
	TokensOut int64   `json:"tokensOut"`
	CostUSD   float64 `json:"costUsd"`
	CostBasis string  `json:"costBasis"`
}

// CardTiming is one card's whole timing history.
type CardTiming struct {
	CardTimingSummary
	TaskID   string          `json:"taskId"`
	Segments []TimingSegment `json:"segments"`
	Runs     []AttemptTiming `json:"runs"`
	Workflow *WorkflowTiming `json:"workflow,omitempty"`
}

// WorkflowTiming rolls a workflow's cards up.
type WorkflowTiming struct {
	ID          string `json:"id"`
	Known       bool   `json:"known"`
	Cards       int    `json:"cards"`
	Done        int    `json:"done"`
	Finished    bool   `json:"finished"`
	StartedAt   int64  `json:"startedAt"`
	FinishedAt  int64  `json:"finishedAt,omitempty"`
	LeadSeconds int64  `json:"leadSeconds"`
	AISeconds   int64  `json:"aiSeconds"`
	IdleSeconds int64  `json:"idleSeconds"`
}

// bucketOf says where a status's seconds go. READY depends on who acts next:
// under GAOS_RUNNER=on the scheduler dispatches it; under manual (D-080) only
// the operator does, and under off nothing will until the operator turns the
// runner on.
func bucketOf(status, mode string) string {
	switch status {
	case StatusDispatched, StatusRunning:
		return BucketAI
	case StatusAwaitingInput, StatusReview, StatusFailed:
		return BucketOperator
	case StatusReady:
		if mode == RunnerOn {
			return BucketQueued
		}
		return BucketOperator
	case StatusBlocked, StatusDeferred:
		return BucketBlocked
	case StatusDone, StatusCanceled:
		return BucketDone
	}
	return BucketBacklog
}

func terminal(status string) bool { return status == StatusDone || status == StatusCanceled }

// plans reports whether a TaskPlanned event lists the task.
func plans(e Event, taskID string) bool {
	for _, pt := range e.Payload.Tasks {
		if pt.ID == taskID {
			return true
		}
	}
	return false
}

// cardTiming computes one card's timing history. Pure: it reads only its
// arguments and modifies none of them. t supplies the id, the product and the
// card's CURRENT status; events may hold anything -- only this product's
// TaskPlanned listing the task and TaskMoved naming it count, in Seq order.
func cardTiming(t TaskRecord, events []Event, runs []TaskRunRecord, now int64, mode string) CardTiming {
	evs := make([]Event, 0, 16)
	for _, e := range events {
		if e.DeskID != t.DeskID {
			continue
		}
		switch {
		case e.Type == TaskPlanned && plans(e, t.ID):
		case e.Type == TaskMoved && e.Payload.Task == t.ID && e.Payload.To != "":
		default:
			continue
		}
		evs = append(evs, e)
	}
	sort.SliceStable(evs, func(i, j int) bool { return evs[i].Seq < evs[j].Seq })

	ct := CardTiming{TaskID: t.ID, Segments: []TimingSegment{}, Runs: []AttemptTiming{}}
	ct.Status, ct.AsOf, ct.Known, ct.Complete = t.Status, now, true, true

	var eff int64 // the running maximum: a clock that steps back cannot open a negative interval
	cur := ""
	open := func(status, from, rule string, at int64) {
		ct.Segments = append(ct.Segments, TimingSegment{Status: status, From: from, Rule: rule,
			EnteredAt: at, Bucket: bucketOf(status, mode)})
		cur = status
	}
	closeAt := func(at int64) {
		if n := len(ct.Segments); n > 0 {
			s := &ct.Segments[n-1]
			s.LeftAt, s.Seconds = at, at-s.EnteredAt
			// A terminal segment that is left (a design sent back, D-084) is
			// booked as blocked: a convention that keeps the partition exact,
			// since BucketDone is in none of the sums. The time was the
			// review's turn and the operator's wait.
			if terminal(s.Status) {
				s.Bucket = BucketBlocked
			}
		}
	}
	for _, e := range evs {
		if e.At == 0 {
			ct.Known = false
		}
		if e.At > eff {
			eff = e.At
		}
		if cur == "" {
			ct.CreatedAt = eff
			if e.Type == TaskPlanned {
				open(StatusPending, "", "plan", eff)
				continue
			}
			// A stream that starts after the plan: open on the move's FROM half,
			// for no time at all, and say the history is not the whole story.
			from, _, _ := strings.Cut(e.Payload.Reason, "→")
			if from == "" {
				from = StatusPending
			}
			open(from, "", "", eff)
			ct.Complete, ct.Reason = false, "no plan event for this card"
		}
		if e.Type != TaskMoved || e.Payload.To == cur {
			continue
		}
		closeAt(eff)
		open(e.Payload.To, cur, e.Payload.Rule, eff)
	}

	if len(ct.Segments) == 0 {
		ct.Known, ct.Complete, ct.Reason = false, false, "no history for this card"
	} else {
		last := &ct.Segments[len(ct.Segments)-1]
		if terminal(last.Status) {
			last.LeftAt, last.Seconds = last.EnteredAt, 0
			ct.Finished, ct.FinishedAt = true, last.EnteredAt
		} else {
			last.Open = true
			if now > last.EnteredAt {
				last.Seconds = now - last.EnteredAt
			}
		}
		if last.Status != t.Status {
			ct.Complete = false
			ct.Reason = fmt.Sprintf("the history ends in %s but the card is %s; a move was not recorded", last.Status, t.Status)
			ct.Finished = terminal(t.Status)
		}
	}
	if !ct.Known {
		ct.Reason = "no time recorded for this card's history"
		if len(ct.Segments) == 0 {
			ct.Reason = "no history for this card"
		}
		ct.CreatedAt, ct.FinishedAt = 0, 0
		for i := range ct.Segments {
			ct.Segments[i].EnteredAt, ct.Segments[i].LeftAt, ct.Segments[i].Seconds = 0, 0, 0
		}
	}

	// The partition: every second of every segment in one bucket, and the lead
	// is their sum -- equal to (finished or now) - created by construction,
	// since the segments tile that interval with no gap and no overlap.
	if ct.Known {
		for _, s := range ct.Segments {
			switch s.Bucket {
			case BucketAI:
				ct.AISeconds += s.Seconds
			case BucketOperator:
				ct.WaitingOperatorSeconds += s.Seconds
			case BucketQueued:
				ct.QueuedSeconds += s.Seconds
			case BucketBlocked:
				ct.BlockedSeconds += s.Seconds
			case BucketBacklog:
				ct.BacklogSeconds += s.Seconds
			}
		}
		ct.LeadSeconds = ct.AISeconds + ct.WaitingOperatorSeconds + ct.QueuedSeconds + ct.BlockedSeconds + ct.BacklogSeconds
		ct.IdleSeconds = max(0, ct.LeadSeconds-ct.AISeconds)
	}

	rs := append([]TaskRunRecord(nil), runs...)
	sort.SliceStable(rs, func(i, j int) bool { return rs[i].Attempt < rs[j].Attempt })
	for _, r := range rs {
		if r.TaskID != t.ID {
			continue
		}
		a := AttemptTiming{Attempt: r.Attempt, Status: r.Status, Model: r.Model, Provider: r.Provider,
			StartedAt: r.StartedAt, EndedAt: r.EndedAt, TokensIn: r.TokensIn, TokensOut: r.TokensOut,
			CostUSD: r.CostUSD, CostBasis: r.CostBasis}
		switch {
		case r.EndedAt > 0 && r.StartedAt > 0:
			a.Seconds = max(0, r.EndedAt-r.StartedAt)
		case r.EndedAt == 0 && r.StartedAt > 0 && (r.Status == RunStatusStarted || r.Status == RunStatusRunning):
			a.Seconds = max(0, now-r.StartedAt)
		}
		ct.Runs = append(ct.Runs, a)
		ct.Attempts++
		ct.TokensIn += r.TokensIn
		ct.TokensOut += r.TokensOut
		// BR-31: a cost keeps its basis through every aggregation, so the
		// card carries one total per basis and never a blend of them.
		switch r.CostBasis {
		case CostMeasured:
			ct.CostMeasuredUSD += r.CostUSD
		case CostEstimated:
			ct.CostEstimatedUSD += r.CostUSD
		default:
			ct.CostUnavailable++
		}
	}

	ct.LeadText, ct.TitleText = "—", ct.Reason
	if ct.Known && ct.Complete {
		ct.LeadText = relSpan(time.Duration(ct.LeadSeconds) * time.Second)
		ct.TitleText = fmt.Sprintf("Lead %s · AI %s · idle %s — open the timing history",
			ct.LeadText, relSpan(time.Duration(ct.AISeconds)*time.Second), relSpan(time.Duration(ct.IdleSeconds)*time.Second))
	}
	return ct
}

// workflowTiming rolls one workflow's cards up. Σ AI is a union, not an
// overcount: a product has at most one live turn (BR-38) and a workflow is
// one product's.
func workflowTiming(id string, cards []CardTiming, now int64) WorkflowTiming {
	w := WorkflowTiming{ID: id, Known: true, Finished: len(cards) > 0}
	for _, c := range cards {
		w.Cards++
		if c.Status == StatusDone {
			w.Done++
		}
		if !c.Known || !c.Complete {
			w.Known = false
			continue
		}
		if w.StartedAt == 0 || c.CreatedAt < w.StartedAt {
			w.StartedAt = c.CreatedAt
		}
		if !c.Finished {
			w.Finished = false
		} else if c.FinishedAt > w.FinishedAt {
			w.FinishedAt = c.FinishedAt
		}
		w.AISeconds += c.AISeconds
	}
	if !w.Known {
		return WorkflowTiming{ID: id, Cards: w.Cards, Done: w.Done}
	}
	end := now
	if w.Finished {
		end = w.FinishedAt
	} else {
		w.FinishedAt = 0
	}
	w.LeadSeconds = max(0, end-w.StartedAt)
	w.IdleSeconds = max(0, w.LeadSeconds-w.AISeconds)
	return w
}

// tzOffset is the database's zone: how far its printed clock (parsed as UTC)
// is from the app's, rounded to the nearest quarter hour so a few seconds of
// skew between two hosts never becomes a correction.
func tzOffset(dbAsUTC, appNow int64) int64 {
	d := appNow - dbAsUTC
	const q = 900
	if d >= 0 {
		return (d + q/2) / q * q
	}
	return -((-d + q/2) / q * q)
}

// fmtDur is the timing sheet's duration: finer than relSpan, because a
// timeline of "<1m" rows tells nothing.
func fmtDur(sec int64) string {
	if sec < 0 {
		sec = 0
	}
	switch {
	case sec < 60:
		return fmt.Sprintf("%ds", sec)
	case sec < 3600:
		return fmt.Sprintf("%dm %02ds", sec/60, sec%60)
	case sec < 48*3600:
		return fmt.Sprintf("%dh %02dm", sec/3600, sec%3600/60)
	}
	return fmt.Sprintf("%dd %dh", sec/86400, sec%86400/3600)
}

func timingURL(productID, taskID string) string {
	return "ui/timing/" + url.PathEscape(productID) + "/" + url.PathEscape(taskID)
}

// conversationURL is a card's conversation sheet (D-082), escaped the same way.
func conversationURL(productID, taskID string) string {
	return "ui/conversation/" + url.PathEscape(productID) + "/" + url.PathEscape(taskID)
}

// sendBackURL is the send-back sheet's URL (D-084), both ids path-escaped.
func sendBackURL(productID, taskID string) string {
	return "ui/send-back/" + url.PathEscape(productID) + "/" + url.PathEscape(taskID)
}

// ---------- the store side ----------

// taskEventsLocked returns the events that can bear on one task: the
// TaskPlanned events listing it, and the TaskMoved, QuestionAsked,
// QuestionAnswered, ConversationArchived and ConversationPruned events naming
// it. The stream is unbounded, so it is never rescanned: the per-task index
// is extended from its high-water mark to the stream's end, whoever appended
// those events (emit, or ArcadeStore's load). Caller holds s.mu (read or
// write), so s.events cannot change underneath.
func (s *MemStore) taskEventsLocked(taskID string) []Event {
	s.timingMu.Lock()
	defer s.timingMu.Unlock()
	s.extendIndexLocked()
	idx := s.timingIdx[taskID]
	out := make([]Event, len(idx))
	for k, i := range idx {
		out[k] = s.events[i]
	}
	return out
}

// conversationEventsLocked is every ConversationArchived and
// ConversationPruned on the stream, in order (D-082): the Archive page's
// source, read without rescanning. Caller holds s.mu.
func (s *MemStore) conversationEventsLocked() []Event {
	s.timingMu.Lock()
	defer s.timingMu.Unlock()
	s.extendIndexLocked()
	out := make([]Event, len(s.convIdx))
	for k, i := range s.convIdx {
		out[k] = s.events[i]
	}
	return out
}

// extendIndexLocked brings both indexes to the stream's end. Caller holds
// s.timingMu and s.mu.
func (s *MemStore) extendIndexLocked() {
	if s.timingIdx == nil || len(s.events) < s.timingN {
		s.timingIdx, s.convIdx, s.timingN = map[string][]int{}, nil, 0
	}
	for i := s.timingN; i < len(s.events); i++ {
		e := s.events[i]
		switch e.Type {
		case TaskPlanned:
			for _, pt := range e.Payload.Tasks {
				s.timingIdx[pt.ID] = append(s.timingIdx[pt.ID], i)
			}
			continue
		case ConversationArchived, ConversationPruned:
			s.convIdx = append(s.convIdx, i)
		case TaskMoved, QuestionAsked, QuestionAnswered:
		default:
			continue
		}
		if e.Payload.Task != "" {
			s.timingIdx[e.Payload.Task] = append(s.timingIdx[e.Payload.Task], i)
		}
	}
	s.timingN = len(s.events)
}

// productTimingsLocked computes every card of one product, keyed by task id.
// Caller holds s.mu.
func (s *MemStore) productTimingsLocked(productID string, now int64) (map[string]CardTiming, map[string][]string) {
	runs := map[string][]TaskRunRecord{}
	for _, r := range s.runs {
		if r.DeskID == productID || r.DeskID == "" {
			runs[r.TaskID] = append(runs[r.TaskID], r)
		}
	}
	mode := runnerMode()
	out := map[string]CardTiming{}
	byWF := map[string][]string{}
	for _, t := range s.tasks {
		if t.DeskID != productID {
			continue
		}
		out[t.ID] = cardTiming(t, s.taskEventsLocked(t.ID), runs[t.ID], now, mode)
		byWF[t.WorkflowID] = append(byWF[t.WorkflowID], t.ID)
	}
	return out, byWF
}

func workflowOf(id string, ids []string, cards map[string]CardTiming, now int64) WorkflowTiming {
	cs := make([]CardTiming, 0, len(ids))
	for _, tid := range ids {
		cs = append(cs, cards[tid])
	}
	return workflowTiming(id, cs, now)
}

// decorateBoardTiming puts each card's summary and each workflow's totals on
// the board. Caller holds s.mu.
func (s *MemStore) decorateBoardTiming(b *Board, productID string, now int64) {
	cards, byWF := s.productTimingsLocked(productID, now)
	for i := range b.Cards {
		if c, ok := cards[b.Cards[i].ID]; ok {
			sum := c.CardTimingSummary
			b.Cards[i].Timing = &sum
		}
	}
	b.Workflows = []WorkflowTiming{}
	ids := make([]string, 0, len(byWF))
	for id := range byWF {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		b.Workflows = append(b.Workflows, workflowOf(id, byWF[id], cards, now))
	}
}

// CardTiming is one card's full history (Store).
func (s *MemStore) CardTiming(ctx context.Context, productID, taskID string) (CardTiming, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var task *TaskRecord
	for i := range s.tasks {
		if s.tasks[i].ID == taskID && s.tasks[i].DeskID == productID {
			task = &s.tasks[i]
		}
	}
	if task == nil {
		return CardTiming{}, ErrNotFound
	}
	now := time.Now().Unix()
	cards, byWF := s.productTimingsLocked(productID, now)
	ct := cards[taskID]
	w := workflowOf(task.WorkflowID, byWF[task.WorkflowID], cards, now)
	ct.Workflow = &w
	return ct, nil
}

// ---------- the sheet's view model ----------

// TimingView is the timing sheet (templates/needs.html, timing-modal).
type TimingView struct {
	ProductName, TaskID, TaskTitle, Status string
	Known, Complete                        bool
	Reason                                 string
	Totals                                 []TimingTotal
	Created, Finished, AsOf                string
	Segments                               []TimingRow
	Attempts                               []AttemptRow
	Workflow                               string
	Cost                                   string
}

type TimingTotal struct {
	Label, Value string
	Zero         bool
}

type TimingRow struct {
	Status, From, Rule, Entered, EnteredISO, Elapsed string
	Open                                             bool
}

type AttemptRow struct {
	Attempt                                         int
	Status, Model, Provider, Duration, Tokens, Cost string
}

func absTime(sec int64) string {
	if sec <= 0 {
		return ""
	}
	return time.Unix(sec, 0).UTC().Format("2006-01-02 15:04 UTC")
}

func usd(v float64) string { return fmt.Sprintf("$%.2f", v) }

func buildTimingView(productName, title string, ct CardTiming) *TimingView {
	v := &TimingView{ProductName: productName, TaskID: ct.TaskID, TaskTitle: title, Status: ct.Status,
		Known: ct.Known, Complete: ct.Complete, Reason: ct.Reason,
		Created: absTime(ct.CreatedAt), Finished: absTime(ct.FinishedAt), AsOf: absTime(ct.AsOf)}
	if ct.Known {
		for _, t := range []struct {
			l string
			s int64
		}{
			{"Lead time", ct.LeadSeconds}, {"AI working", ct.AISeconds},
			{"Idle — no AI working on it", ct.IdleSeconds}, {"Waiting on you", ct.WaitingOperatorSeconds},
			{"Queued", ct.QueuedSeconds}, {"Blocked", ct.BlockedSeconds}, {"Backlog", ct.BacklogSeconds},
		} {
			v.Totals = append(v.Totals, TimingTotal{Label: t.l, Value: fmtDur(t.s), Zero: t.s == 0})
		}
	}
	for _, s := range ct.Segments {
		r := TimingRow{Status: s.Status, From: s.From, Rule: s.Rule, Open: s.Open}
		if ct.Known {
			r.Entered, r.Elapsed = absTime(s.EnteredAt), fmtDur(s.Seconds)
			if s.EnteredAt > 0 {
				r.EnteredISO = time.Unix(s.EnteredAt, 0).UTC().Format(time.RFC3339)
			}
		}
		v.Segments = append(v.Segments, r)
	}
	for _, a := range ct.Runs {
		cost := usd(a.CostUSD) + " " + a.CostBasis
		if a.CostBasis == CostUnavailable || a.CostBasis == "" {
			cost = "unavailable"
		}
		v.Attempts = append(v.Attempts, AttemptRow{Attempt: a.Attempt, Status: a.Status, Model: a.Model,
			Provider: a.Provider, Duration: fmtDur(a.Seconds),
			Tokens: fmt.Sprintf("%d in · %d out", a.TokensIn, a.TokensOut), Cost: cost})
	}
	var parts []string
	if ct.CostMeasuredUSD > 0 {
		parts = append(parts, usd(ct.CostMeasuredUSD)+" measured")
	}
	if ct.CostEstimatedUSD > 0 {
		parts = append(parts, usd(ct.CostEstimatedUSD)+" estimated")
	}
	if ct.CostUnavailable > 0 {
		parts = append(parts, fmt.Sprintf("%d unavailable", ct.CostUnavailable))
	}
	v.Cost = strings.Join(parts, " · ")
	if w := ct.Workflow; w != nil && w.Known {
		v.Workflow = fmt.Sprintf("%s: %d/%d cards done · lead %s · AI %s · idle %s", w.ID, w.Done, w.Cards,
			fmtDur(w.LeadSeconds), fmtDur(w.AISeconds), fmtDur(w.IdleSeconds))
	}
	return v
}
