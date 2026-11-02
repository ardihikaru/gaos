package main

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Turns: what supervision means when the work is a request, not a process.
// M14.5, D-056, D-060, BR-40, BR-41.
//
// Through the gateway an attempt is ONE TURN: a prompt submitted to
// ai-manager under a deterministic request id, polled to a result. There is
// no process to watch, no folder this host must lease, and nothing to kill.
// What survives a restart is the request id, recorded on the TaskRun before
// the turn is sent; recovery polls it. A session belongs to a task (D-056):
// a retry resumes it so the model sees its own failed reply, and an
// escalation, a lost session or age opens a new one and says so.

// TurnAdapter is the capability an adapter has when its attempts are turns.
// Asserted by type -- a capability, never a branch on the invocation mode
// (BR-33). The CLI adapter does not implement it.
type TurnAdapter interface {
	RequestIDFor(runID string) string
	ConfiguredModel() string
	TurnDeadline() time.Duration
	SessionMaxAge() time.Duration
}

// SigRequestLost: the gateway no longer knows a request this orchestrator
// submitted (404) -- ai-manager restarted before its records were durable, or
// the record aged out. The turn may still be running, so it is never
// requeued on a hope (D-060): the product stays held until the turn must have
// ended.
const SigRequestLost = "request-lost"

// Why a new session was opened (SessionOpened.Rule).
const (
	SessionFirst      = "first"
	SessionEscalation = "escalation"
	SessionLostReason = "lost"
	SessionRotation   = "rotation"
	// SessionVendor: the task's vendor changed since its last turn (M16).
	SessionVendor = "vendor"
)

const defaultSessionMaxAge = 25 * 24 * time.Hour

// The TaskResumed rules recovery writes. Kept after the lease-based reconciler
// was retired (M14.7) because the fold reads them -- "adopted" changes nothing
// on the floor, "requeued" makes the desk idle -- and streams written before
// the retirement carry them.
const (
	RuleAdopted  = "adopted"
	RuleRequeued = "requeued"
)

func (a *AIMAdapter) ConfiguredModel() string { return a.Model }

func (a *AIMAdapter) TurnDeadline() time.Duration {
	if a.Deadline > 0 {
		return a.Deadline
	}
	return defaultAIMDeadline
}

func (a *AIMAdapter) SessionMaxAge() time.Duration {
	if a.MaxSessionAge > 0 {
		return a.MaxSessionAge
	}
	return defaultSessionMaxAge
}

// nextAttemptLocked numbers an attempt: one more than the task has had. The
// number names the run and therefore the request id, so two attempts can
// never share one -- ai-manager answers a repeated id with the FIRST
// request's result. Caller holds s.mu.
func (s *MemStore) nextAttemptLocked(taskID string) int {
	n := 0
	for _, r := range s.runs {
		if r.TaskID == taskID && r.Attempt > n {
			n = r.Attempt
		}
	}
	return n + 1
}

// sessionForLocked decides which session the next attempt at taskID runs in:
// the task's last session when it may be resumed, otherwise none and the
// reason a new one is about to open. Caller holds s.mu.
//
// vendor and model are where this attempt goes (M16): a task whose last turn
// went to another vendor opens a new session, because a session lives with
// the provider that created it (BR-45); model is the vendor's own model, ""
// for the adapter's configured one.
func (s *MemStore) sessionForLocked(taskID string, ta TurnAdapter, vendor, model string, now int64) (string, string) {
	var last *TaskRunRecord
	for i := range s.runs {
		r := &s.runs[i]
		if r.TaskID == taskID && (last == nil || r.Attempt > last.Attempt) {
			last = r
		}
	}
	switch {
	case last == nil:
		return "", SessionFirst
	// Before the empty-session case: a turn whose request was lost never
	// learned its session id, and its successor opens a new one BECAUSE of
	// that loss, which is what the event must say.
	case last.FailureSignature == SigSessionLost || last.FailureSignature == SigTurnUnsaved ||
		last.FailureSignature == SigRequestLost:
		return "", SessionLostReason // BR-40: never resumed after it was lost
	case last.SessionID == "":
		return "", SessionFirst
	case vendor != "" && runVendor(*last) != vendor:
		return "", SessionVendor
	}
	want := model
	if want == "" && vendor != VendorAntigravity {
		want = ta.ConfiguredModel() // GAOS_MODEL is a Claude model
	}
	if want != "" && last.Model != "" && !strings.HasPrefix(last.Model, want) {
		return "", SessionEscalation
	}
	opened := last.StartedAt
	for _, r := range s.runs {
		if r.SessionID == last.SessionID && r.StartedAt > 0 && r.StartedAt < opened {
			opened = r.StartedAt
		}
	}
	if now-opened > int64(ta.SessionMaxAge()/time.Second) && !s.answerResumesLocked(taskID, last, now) {
		return "", SessionRotation
	}
	return last.SessionID, ""
}

// sessionEvents reports what a turn did to the task's session: SessionLost on
// a 410, SessionOpened when the turn ran in a session other than the one it
// was sent with. Caller holds no lock.
func (s *MemStore) sessionEvents(deskID string, c Cleared, att Attempt) {
	p := c.Packet()
	switch {
	case att.Failure == SigSessionLost:
		s.emitLocked(SessionLost, deskID, fmt.Sprintf("session %s of %s is gone (410); the next attempt opens a new one", short(p.SessionID), p.TaskID),
			KindWarn, EventPayload{Session: p.SessionID, Rule: SessionLostReason})
	case att.SessionID != "" && att.SessionID != p.SessionID:
		reason := p.SessionReason
		if reason == "" {
			reason = SessionFirst
		}
		s.emitLocked(SessionOpened, deskID, fmt.Sprintf("%s runs in session %s (%s)", p.TaskID, short(att.SessionID), reason),
			KindInfo, EventPayload{Session: att.SessionID, Rule: reason})
	}
}

func short(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// recoverTurns is boot recovery for gateway runs (D-060). Every run still
// STARTED or RUNNING with a request id is polled rather than reconciled
// against a lease: a completed or failed request is adopted into the same
// attempt a supervised run gets; a lost one is orphaned, holds its product
// until the turn must have ended, and only then is requeued -- on a new
// session, because its run carries SigRequestLost. It returns the tasks it
// took, which the lease-based reconciler must leave alone.
func (s *MemStore) recoverTurns(ctx context.Context, ad Adapter, ta TurnAdapter) (map[string]bool, []Event) {
	type turn struct {
		run  TaskRunRecord
		desk Desk
	}
	var todo []turn
	s.mu.RLock()
	for _, r := range s.runs {
		if r.RequestID == "" || (r.Status != RunStatusStarted && r.Status != RunStatusRunning) {
			continue
		}
		if i := s.deskIdxByID(r.DeskID); i >= 0 {
			todo = append(todo, turn{run: r, desk: s.desks[i]})
		}
	}
	pol, tgt := s.egress, s.target
	s.mu.RUnlock()

	taken := map[string]bool{}
	var ev []Event
	for _, t := range todo {
		r := t.run
		taken[r.TaskID] = true
		s.mu.RLock()
		pkt := s.freezePacketFor(t.desk, r.TaskID, r.Attempt)
		s.mu.RUnlock()
		c, _, _ := Screen(pol, tgt, pkt) // a refusal now leaves a zero Cleared; Resume still polls by id
		s.moveTaskByID(r.TaskID, StatusRunning, RuleAdopted)
		s.setRunStatus(r.ID, RunStatusRunning)
		s.mu.Lock()
		ev = append(ev, s.emit(TaskResumed, r.DeskID, "adopted the turn "+r.RequestID+" on "+r.TaskID, KindInfo, EventPayload{Rule: RuleAdopted}))
		s.mu.Unlock()
		go s.collectTurn(ctx, ad, ta, c, r)
	}
	return taken, ev
}

// collectTurn polls one recovered turn to its end and finishes it.
func (s *MemStore) collectTurn(ctx context.Context, ad Adapter, ta TurnAdapter, c Cleared, r TaskRunRecord) {
	att, err := ad.Resume(context.WithoutCancel(ctx), c, RunRecord{
		RunID: r.ID, TaskID: r.TaskID, Attempt: r.Attempt, Detail: map[string]string{"requestId": r.RequestID},
	})
	if err != nil || att.Failure == SigRequestLost {
		s.mu.Lock()
		e := s.emit(TaskOrphaned, r.DeskID, fmt.Sprintf("turn %s on %s is unknown to the gateway; the product stays held until it must have ended", r.RequestID, r.TaskID),
			KindFail, EventPayload{Rule: SigRequestLost})
		s.deskStateLocked(r.DeskID, StateBlocked) // what the fold does with TaskOrphaned
		s.mu.Unlock()
		s.publish(e)
		if wait := time.Until(time.Unix(r.StartedAt, 0).Add(ta.TurnDeadline())); wait > 0 {
			time.Sleep(wait)
		}
		s.mu.Lock()
		for i := range s.runs {
			if s.runs[i].ID == r.ID {
				s.runs[i].Status, s.runs[i].FailureSignature, s.runs[i].EndedAt = RunStatusOrphaned, SigRequestLost, time.Now().Unix()
			}
		}
		var out []Event
		if m, ok := s.moveTaskLocked(r.TaskID, StatusReady, RuleRequeued); ok {
			out = append(out, m)
		}
		out = append(out, s.emit(TaskResumed, r.DeskID, r.TaskID+" requeued: its lost turn has passed its deadline", KindWarn, EventPayload{Rule: RuleRequeued}))
		s.deskStateLocked(r.DeskID, StateIdle) // what the fold does with requeued
		s.mu.Unlock()
		s.publish(out...)
		return
	}
	s.finishRun(r.DeskID, r.TaskID, r.ID, att, nil)
	s.checkAfterTurn(context.WithoutCancel(ctx), r.DeskID, r.ID, att)
}

// deskStateLocked moves a desk the way the fold will when it replays the event
// just written, so a replayed floor and the live one agree. Caller holds s.mu.
func (s *MemStore) deskStateLocked(deskID, state string) {
	if i := s.deskIdxByID(deskID); i >= 0 {
		_ = s.setState(&s.desks[i], state)
	}
}
