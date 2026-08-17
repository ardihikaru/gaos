package main

import (
	"context"
	"fmt"
	"time"
)

// The simulation. This is the port of simulate() from the prototype
// (gaos-mission-control.html lines 1853-1967), moved server-side.
//
// WHY IT RUNS AT ALL IN PASS 1. The point of this pass is to prove a
// deployment: edge -> tunnel -> Traefik -> stripPrefix -> container -> template
// -> HTMX swap. A frozen snapshot proves the container starts. A floor that
// moves proves the whole chain works repeatedly, under concurrency, with a
// writer racing every reader -- which is what actually breaks in production and
// what a static page would hide until the first real backend arrived.
//
// It is also the API contract in disguise: every field it mutates is a field
// the Task/TaskRun/Review vertices carry, and every name it emits is in the
// closed catalog in events.go. When the scheduler and the runner are real, this
// file is deleted and nothing else changes.
//
// The tick is 1400ms / speed, the prototype's own number (line 2035).

const baseTick = 1400 * time.Millisecond

// RunSim drives the floor until ctx is cancelled. One goroutine, one writer.
func (s *MemStore) RunSim(ctx context.Context) {
	s.mu.Lock()
	s.simOn = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.simOn = false
		s.mu.Unlock()
	}()

	t := time.NewTimer(baseTick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.Tick()
			t.Reset(s.tickInterval())
		}
	}
}

func (s *MemStore) tickInterval() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sp := s.speed
	if sp < 1 {
		sp = 1
	}
	return baseTick / time.Duration(sp)
}

// Tick advances the floor by one step. Exported so the tests can drive it
// deterministically instead of sleeping, which is the difference between a test
// suite that is fast and one that is flaky.
func (s *MemStore) Tick() {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Paused means paused, and a replaying operator must not have the past
	// rewritten under them. Both checks are here rather than in the caller so
	// that Tick() is always safe to call.
	if s.paused || s.replayAtSeq != liveMode {
		return
	}
	s.clock++

	// A meeting freezes the rest of the floor, exactly as it does in the
	// prototype: the room has everyone's attention.
	if s.meetingIdx >= 0 {
		s.tickMeeting()
		return
	}

	for i := range s.desks {
		s.tickDesk(&s.desks[i])
		// startMeeting inside tickDesk claims the floor; stop here so the rest
		// of the desks do not advance during a review that has just begun.
		if s.meetingIdx >= 0 {
			return
		}
	}
}

func (s *MemStore) tickDesk(d *Desk) {
	switch d.State {
	case StateMeeting:
		return

	case StateIdle:
		// A free chair fills when the scheduler finds capacity. The capacity
		// check is the READY-task rule in miniature: a room at capacity queues
		// the work instead of over-seating it.
		if s.rng.Float64() < .12 && s.occupancy("backend") < 4 {
			d.Agents = []string{"a-4"}
			s.mustSetState(d, StateActive)
			d.Progress = 2
			d.Task = s.pickTask(*d)
			s.log(d, "workspace ws-5 leased · branch feat/reconcile", "t")
			s.emit(TaskDispatched, d.ID, d.Name+": "+d.Task, KindInfo, deskPayload(*d))
		}
		return

	case StateBlocked:
		if s.rng.Float64() < .22 {
			s.mustSetState(d, StateActive)
			d.BlockReason = ""
			s.log(d, "blocker cleared, resuming", "g")
			s.emit(TaskResumed, d.ID, "Dependency resolved", KindInfo, deskPayload(*d))
		}
		return

	case StateCompleted:
		if s.rng.Float64() < .18 {
			d.StageIdx = 0
			d.Progress = 3
			s.mustSetState(d, StateActive)
			d.Scored, d.Score, d.ScoreParts = false, 0, nil
			d.Retry = 0
			d.TaskSeq++
			d.Task = s.pickTask(*d)
			s.emit(WorkflowCreated, d.ID, "Next objective planned for "+d.Name, KindInfo, deskPayload(*d))
		}
		return
	}

	// A gated task holds until a human releases it. That is the whole point of
	// a gate: it does not time out and it does not progress.
	if d.ApprovalRequired {
		return
	}

	d.Progress += 3 + s.rng.Float64()*9
	if d.Progress > 100 {
		d.Progress = 100
	}
	tickCost := 0.008 + s.rng.Float64()*0.03
	d.Cost += tickCost
	s.spend += tickCost

	if s.rng.Float64() < .5 {
		pool := logPool[stageOf(*d)]
		if len(pool) == 0 {
			pool = logPool["coding"]
		}
		s.log(d, pool[s.rng.IntN(len(pool))], "")
	}

	if d.State == StateActive && d.Progress < 90 && s.rng.Float64() < .035 {
		s.mustSetState(d, StateBlocked)
		d.BlockReason = "Waiting on " + taskIDOf(*d) + "-dep · migration must land first"
		s.log(d, "blocked: dependency not satisfied", "y")
		s.emit(TaskBlocked, d.ID, d.BlockReason, KindWarn, deskPayload(*d))
		return
	}

	if d.Progress < 100 {
		return
	}

	if d.State == StateActive {
		s.mustSetState(d, StateReview)
		d.Progress = 100
		s.log(d, "runner exited 0 · handing off to review", "t")
		s.emit(TaskCompleted, d.ID, d.Task, KindDone, deskPayload(*d))
		return
	}

	if d.State == StateReview {
		s.review(d)
	}
}

// review scores the work and applies the retry policy from architecture §06:
//
//	85-100  accept
//	70-84   accept with cleanup
//	50-69   iterate with a widened context
//	30-49   escalate a tier
//	 0-29   stop, call a design review
//
// The two-retries-then-review rule is the same document's
// same_failure_signature_limit in its simplest form.
func (s *MemStore) review(d *Desk) {
	sc := s.score(d)

	verdict := "accept"
	cls := "g"
	switch {
	case sc >= 85:
	case sc >= 70:
		verdict = "accept with cleanup"
	case sc >= 50:
		verdict = "iterate"
		cls = "y"
	default:
		verdict = "escalate"
		cls = "r"
	}
	s.log(d, fmt.Sprintf("score %d (%s)", sc, verdict), cls)
	kind := KindDone
	if sc < 70 {
		kind = KindWarn
	}
	s.emit(ReviewCompleted, d.ID, fmt.Sprintf("Score %d", sc), kind, deskPayload(*d))

	if sc < 50 || (d.Retry >= 2 && sc < 70) {
		reason := "Low score on " + taskIDOf(*d)
		if sc >= 50 {
			reason = "Repeated retries on " + taskIDOf(*d)
		}
		s.startMeeting(s.deskIdxByID(d.ID), reason)
		return
	}
	if sc < 70 {
		d.Retry++
		s.mustSetState(d, StateActive)
		d.Progress = 12
		d.Task = "Fix: " + lowerFirst(d.Task)
		// The verdict belongs to the attempt that earned it, and this is a new
		// attempt. MemStore.RetryTask has always cleared the score on the
		// operator's retry; this path did not, so the same event name meant two
		// different things depending on who asked for it. The fold is what found
		// it -- one reducer case cannot honour both readings, and having to write
		// the case is what made the disagreement visible.
		d.Scored, d.Score, d.ScoreParts = false, 0, nil
		s.emit(RetryRequested, d.ID, fmt.Sprintf("Retry %d with richer context", d.Retry), KindWarn, deskPayload(*d))
		return
	}
	// The gate policy: work that reaches the review stage holds for a human
	// most of the time. gatePolicy on the Workflow vertex replaces the dice.
	if stageOf(*d) == "review" && !d.ApprovalRequired && s.rng.Float64() < .7 {
		d.ApprovalRequired = true
		s.log(d, "approval gate: merge held for a human", "y")
		s.emit(ApprovalRequested, d.ID, "Merge held on "+taskIDOf(*d), KindWarn, deskPayload(*d))
		return
	}
	s.advance(d)
}

// score produces the five dimensions and the weighted total. The weights are in
// scoreDims; the total is recomputed from them rather than stored separately,
// so a weight change cannot leave the bars and the number disagreeing.
func (s *MemStore) score(d *Desk) int {
	base := 46 + s.rng.Float64()*48 + boolTo(d.Esc > 0, 12) - boolTo(d.Retry > 0, 4)
	parts := make([]ScorePart, len(scoreDims))
	total := 0.0
	for i, dim := range scoreDims {
		v := base + (s.rng.Float64()*22 - 11)
		if v < 20 {
			v = 20
		}
		if v > 100 {
			v = 100
		}
		parts[i] = ScorePart{Key: dim.Key, Weight: dim.Weight, Value: v}
		total += v * dim.Weight
	}
	d.ScoreParts = parts
	d.Scored = true
	d.Score = int(total + 0.5)
	return d.Score
}

// tickMeeting walks the scripted review. Every other tick, so the transcript is
// readable rather than a blur. Assumes the lock is held.
func (s *MemStore) tickMeeting() {
	if s.meetingIdx < 0 || s.meetingIdx >= len(s.desks) {
		return
	}
	d := &s.desks[s.meetingIdx]
	if d.Meeting == nil {
		s.meetingIdx = -1
		return
	}
	m := d.Meeting
	m.Ticks++
	if m.Ticks%2 == 1 {
		return
	}
	if m.Step < len(meetingScript) {
		m.Log = append(m.Log, meetingScript[m.Step])
		m.Step++
		if len(m.Log) > 3 {
			m.Log = m.Log[len(m.Log)-3:]
		}
		m.Conf += 11 + s.rng.Float64()*6
		if m.Conf > 96 {
			m.Conf = 96
		}
		return
	}
	s.closeMeeting()
}

func boolTo(b bool, v float64) float64 {
	if b {
		return v
	}
	return 0
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	if r[0] >= 'A' && r[0] <= 'Z' {
		r[0] = r[0] + 32
	}
	return string(r)
}
