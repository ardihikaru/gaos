package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// The runner. T-050 and T-052, requirements D-2 and D-3.
//
// One supervising goroutine per run. It takes the workspace lease, writes the
// RunRecord, hands the cleared packet to the adapter, beats while the work
// runs, and records what came back. The child it starts is detached: it has its
// own process group and a context that no longer descends from the
// orchestrator's signal handler, so an orchestrator restart does not take a
// paid model call with it.
//
// # What the heartbeat actually attests to
//
// The supervisor, NOT the model. Claude Code knows nothing about GAOS, so the
// thing that beats is this goroutine -- which means that when an orchestrator
// dies, the beat goes stale WHILE THE WORK CONTINUES.
//
// That asymmetry is easy to read backwards, and reading it backwards is how a
// reconciler ends up requeuing a task whose child is still editing the
// repository. It is why D-4 has three branches rather than two, and why the
// middle one (lease held, beat stale) has to kill before it requeues -- see
// reconcile.go. Nothing about a stale beat says the child is gone; it says only
// that nobody is watching it any more.
//
// # Two cadences, not one
//
// The beat is a field write every heartbeatInterval. The Heartbeat EVENT is
// written every heartbeatEventEvery, plus always the first beat of a run and
// always the last before it exits. D-053 has the argument; lease.go has the
// numbers.

// runnerEnabled is the runner's own switch, deliberately separate from whether
// an adapter is configured.
//
// An operator who sets GAOS_MODEL_MODE=cli to make the boot smoke test run has
// not thereby authorised spending money on model calls. Two different
// decisions, two different variables, and the default on this one is off.
func runnerEnabled() bool {
	switch strings.ToLower(envOr("GAOS_RUNNER", "off")) {
	case "on", "1", "true", "yes":
		return true
	}
	return false
}

// refuseIfUnderTest is the guard of last resort in front of a paid model call.
//
// Importing "testing" into non-test code is ugly and it is here anyway. The
// four guards in front of it are all configuration -- GAOS_MODEL_MODE defaults
// to off, only serve() builds a real adapter, GAOS_WORKSPACE_ROOT is unset so a
// Cleared has no workspace, and GAOS_RUNNER is off -- and every one of them can
// be switched on by a developer who then runs `go test ./...` and spends real
// money that does not come back. This is the only guard that cannot be
// forgotten, because it does not depend on anybody remembering anything.
//
// It is not a substitute for the other five. It is the one that holds when they
// have all been turned off for a good reason on a laptop.
//
// It refuses a REAL tool, not a subprocess. The adapter's own tests drive the
// fake in testdata/fakecli and must keep working, so the rule is the narrow
// one that says what is actually meant: inside a test binary, the only thing
// this package may exec is something under a testdata directory.
func refuseIfUnderTest(argv []string) error {
	if !testing.Testing() {
		return nil
	}
	for _, a := range argv {
		if strings.Contains(a, "testdata") {
			return nil
		}
	}
	return fmt.Errorf("refusing to invoke %q from a test binary: a paid model call triggered by "+
		"go test is money that does not come back, and the fake in testdata/fakecli exists for this",
		strings.Join(argv, " "))
}

// beatEmitsEvent is the pure half of the heartbeat: given when the last event
// was written, say whether this beat writes another.
//
// Pure and separate so the cadence can be asserted against a fake clock rather
// than by waiting five minutes. The first and last beats of a run always emit,
// because "alive at T" and "silent since T" are the only two questions a replay
// asks of a heartbeat, and both need an endpoint.
func beatEmitsEvent(lastEventAt, now int64, first, final bool) bool {
	if first || final {
		return true
	}
	return now-lastEventAt >= heartbeatEventEvery
}

// runIDFor names a run. Derived rather than random: the scheduler is a pure
// function and the reconciler compares this against the handoff document's
// `run` field, so a name that needed a random source would be a name two
// replicas could not both produce.
func runIDFor(taskID string, attempt int) string {
	return fmt.Sprintf("%s-a%d", taskID, attempt)
}

// superviseRun is the whole of one run's life. Blocking: the caller decides
// whether it wants a goroutine.
//
// Every failure before the spawn returns without emitting RunStarted. That is
// deliberate and events.go is explicit about why -- emitting a RunStarted for a
// process that never started, or a Heartbeat nobody sent, would put invented
// evidence in the one record the whole audit story rests on.
func (s *MemStore) superviseRun(ctx context.Context, ad Adapter, deskID string, c Cleared, attempt int) (Attempt, error) {
	ws := c.Workspace()
	// A turn needs no folder on this host (M14.5): the workspace that matters
	// is the product's, on the worker, and one live turn per product is the
	// graph lease the scheduler reads (BR-38). The lockfile, the run record and
	// the packet on disk below exist for a local process, and only happen when
	// there is a local folder to put them in.
	ta, isTurn := ad.(TurnAdapter)
	if ws == "" && !isTurn {
		return Attempt{}, fmt.Errorf("supervise: the cleared packet has no workspace")
	}
	taskID := c.Packet().TaskID
	if taskID == "" {
		return Attempt{}, fmt.Errorf("supervise: the cleared packet names no task")
	}
	runID := runIDFor(taskID, attempt)
	now := time.Now().Unix()

	var files []string
	if ws != "" {
		if err := AcquireLease(ws, Lease{
			TaskID: taskID, RunID: runID, Holder: hostHolder(),
			StartedAt: now, LeaseUntil: now + leaseTTL,
		}); err != nil {
			return Attempt{}, err
		}
		// The lease is released on every path out of here, including a panic in the
		// adapter: a workspace left leased by a run that is over is a desk nothing
		// can ever schedule again, and it would take a restart plus a reconciler to
		// notice.
		defer func() { _ = ReleaseLease(ws, runID) }()

		if err := WriteRunRecord(ws, RunRecord{
			RunID: runID, TaskID: taskID, Attempt: attempt, Workspace: ws,
			Digest: c.Digest(), Model: ad.Name(), StartedAt: now,
		}); err != nil {
			return Attempt{}, err
		}

		// The packet, as files, in the leased workspace. T-061, requirement D-8.
		//
		// After the lease and before anything else touches the directory, because
		// the lease is what makes this workspace ours to write into -- materialising
		// first would mean two orchestrators racing to write one packet, which is
		// the exact corruption D-3 exists to prevent.
		//
		// In the runner rather than in the adapter, and that is BR-33: both modes
		// freeze the same packet and materialise it the same way, so a reviewer
		// cannot tell from the workspace which one ran.
		var err error
		if files, err = MaterialisePacket(c); err != nil {
			return Attempt{}, err
		}
	}
	requestID := ""
	if isTurn {
		// Recorded BEFORE the turn is sent: it is the only thing a restarted
		// orchestrator can recover the turn by (D-060).
		requestID = ta.RequestIDFor(runID)
	}

	// The graph's record of this attempt, and the slice it is about to be given.
	// T-054 and T-055.
	//
	// Both before the spawn, and the RUN.json above is why the order is safe to
	// reason about: that file is what an adopting process finds on disk, this is
	// what an auditor finds in the graph, and a run that dies between them has
	// left both. The slice is recorded rather than re-derived because by the time
	// anyone asks what attempt two was given, the record it was assembled from
	// will have moved on.
	s.mu.Lock()
	slice := s.recordSlice(c, taskID, deskID, attempt)
	pinned, _ := s.blueprintGate(deskID) // BR-39: the version this attempt runs under
	s.startRun(TaskRunRecord{
		ID: runID, TaskID: taskID, DeskID: deskID, Attempt: attempt,
		Status: RunStatusStarted, Model: ad.Name(), Invocation: ad.Mode(),
		StartedAt: now, SliceDigest: slice.Digest, BlueprintVersion: pinned, RequestID: requestID,
		// No CostBasis yet, and deliberately not "estimated". A run that has
		// spent nothing has no basis to record; BR-31 is about never aggregating
		// a measured number with a guessed one, and a default here would seed
		// every abandoned attempt with a guess.
	})
	s.mu.Unlock()

	// DISPATCHED -> RUNNING. The distinction exists for exactly one reader: boot
	// reconciliation, which D-4 scopes to "every task claiming to be dispatched
	// or running". A task that had been decided on but never reached a process
	// and one that has a live child are different recoveries, and collapsing
	// them would mean either killing work that was fine or requeuing work that
	// never started.
	s.moveTaskByID(taskID, StatusRunning)
	s.mu.Lock()
	s.markRun(runID, RunStatusRunning)
	s.mu.Unlock()

	s.emitLocked(RunStarted, deskID,
		fmt.Sprintf("run %s started, attempt %d · packet %d file(s)", runID, attempt, len(files)),
		KindInfo, EventPayload{Digest: c.Digest(), Bytes: c.Bytes()})

	// The beat. It renews the lease every heartbeatInterval and writes an event
	// far less often -- D-053.
	done := make(chan struct{})
	go s.beat(deskID, ws, runID, now, done)

	att, err := ad.Dispatch(ctx, c)
	close(done)
	if err == nil {
		s.sessionEvents(deskID, c, att)
	}

	if err != nil {
		s.finishRun(deskID, taskID, runID, Attempt{Digest: c.Digest()}, err)
		return Attempt{}, err
	}

	// The last beat before the exit always writes, so a replay has an endpoint
	// for "silent since T" rather than having to infer one from the RunExited.
	s.beatOnce(deskID, ws, runID, time.Now().Unix(), 0, false, true)

	s.finishRun(deskID, taskID, runID, att, nil)
	return att, nil
}

// finishRun applies what the run changed and emits RunExited, under one lock.
//
// One lock and one function because the two must not be able to disagree. The
// event carries the desk's running total (D-048: absolute, not a delta, so a
// fold that replays it twice is idempotent) and the desk carries the same
// number -- a reducer setting a value the live path never set would make
// TestFoldFromGenesisMatchesTheLiveFloor describe a floor that never existed.
//
// What it does NOT do is decide whether the work succeeded. RunExited says a
// process ended and what it cost; whether the output is any good is the review
// engine's, M5. D-050 forbids reading that from an exit, and this is the place
// it would be tempting to.
func (s *MemStore) finishRun(deskID, taskID, runID string, att Attempt, runErr error) {
	s.mu.Lock()

	// The attempt's own record, closed under the same lock as the event that
	// describes it. Two writers could otherwise leave a TaskRun saying one thing
	// and a RunExited saying another, and there would be no way to tell which of
	// them the auditor should believe.
	s.closeRun(runID, att, runErr)

	// What it handed over, stored where the next agent can be cited it. T-064.
	//
	// Under the same lock for the same reason, and keyed by the run so two
	// attempts leave two documents. The workspace copy is not the record: the
	// lease is released moments from now and the next attempt truncates the file
	// before it starts.
	// The turn's transcript, per attempt (BR-41): the session that carried it
	// expires, and what was asked and answered is the record, not transport.
	for kind, body := range map[string]string{ArtifactPrompt: att.Prompt, ArtifactReply: att.Reply} {
		if body != "" {
			s.recordHandoff(ArtifactRecord{
				ID: runID + "-" + kind, Kind: kind, Title: kind + " of " + runID,
				TaskID: taskID, DeskID: deskID, RunID: runID,
				Attempt: attemptOfRun(s.runs, runID), Body: body, CreatedAt: time.Now().Unix(),
			})
		}
	}
	if att.HandoffRaw != "" {
		s.recordHandoff(ArtifactRecord{
			ID: runID + "-handoff", Kind: ArtifactHandoff,
			Title:  fmt.Sprintf("handoff from %s", runID),
			TaskID: taskID, DeskID: deskID, RunID: runID,
			Attempt: attemptOfRun(s.runs, runID), Body: att.HandoffRaw,
			CreatedAt: time.Now().Unix(),
		})
	}

	p := EventPayload{Digest: att.Digest}
	text := fmt.Sprintf("run %s exited %s, outcome %s", runID, att.ExitClass, att.Outcome())
	kind := exitKind(att)

	switch {
	case runErr != nil:
		p.Reason = runErr.Error()
		text = fmt.Sprintf("run %s could not start: %v", runID, runErr)
		kind = KindFail
	case att.Failure != "":
		// BR-30: no handoff document is a failed attempt with a signature, and
		// the desk must not be left looking like nothing happened.
		p.Reason = att.Failure
		kind = KindFail
	}

	// Where the task goes next, and pointedly not "done".
	//
	// A run that ended is a run that produced something to judge, not a verdict.
	// D-050 forbids reading the outcome from the exit, and BR-13 scores every
	// attempt, so the furthest this may move a task is REVIEW -- M5 decides the
	// rest. The two exceptions are failures of the invocation rather than of the
	// work: no handoff at all (BR-30), and a run that never started.
	switch {
	case runErr != nil, att.Failure != "":
		s.moveTaskLocked(taskID, StatusFailed)
	case att.Outcome() == HandoffBlocked:
		s.moveTaskLocked(taskID, StatusBlocked)
	default:
		s.moveTaskLocked(taskID, StatusReview)
	}

	if i := s.deskIdxByID(deskID); i >= 0 {
		d := &s.desks[i]
		if att.CostUSD > 0 {
			d.Cost += att.CostUSD
			s.spend += att.CostUSD
		}
		p.Cost = d.Cost
		if p.Reason != "" && d.State != StateBlocked {
			// setState rather than an assignment: idle -> blocked and
			// active -> blocked are both legal, and a run that ended badly on a
			// desk in some third state is a bug worth hearing about rather than
			// forcing through.
			if err := s.setState(d, StateBlocked); err == nil {
				d.BlockReason = p.Reason
			}
		}
	}
	e := s.emit(RunExited, deskID, text, kind, p)
	s.mu.Unlock()

	// Outside the lock on purpose: publish is a database round trip, and one
	// slow write must not hold the floor closed against every reader.
	s.publish(e)
}

// beat runs until the run is over. Its own goroutine because the adapter call
// blocks for as long as the work takes.
func (s *MemStore) beat(deskID, ws, runID string, startedAt int64, done <-chan struct{}) {
	tick := time.NewTicker(heartbeatInterval * time.Second)
	defer tick.Stop()

	lastEvent := startedAt
	first := true
	for {
		select {
		case <-done:
			return
		case <-tick.C:
			now := time.Now().Unix()
			if s.beatOnce(deskID, ws, runID, now, lastEvent, first, false) {
				lastEvent = now
			}
			first = false
		}
	}
}

// beatOnce writes the beat and, when the cadence says so, the event. Returns
// whether it wrote an event.
//
// A renewal that fails is NOT treated as fatal here. The lease may have been
// broken by a reconciler that decided this run was dead, and the right response
// to that is to let the run finish and let the reconciler's kill land -- a
// supervisor that tore its own work down on a failed renew would race the very
// process that is trying to arbitrate.
func (s *MemStore) beatOnce(deskID, ws, runID string, now, lastEvent int64, first, final bool) bool {
	_ = RenewLease(ws, runID, now+leaseTTL)
	if !beatEmitsEvent(lastEvent, now, first, final) {
		return false
	}
	s.emitLocked(Heartbeat, deskID, "run "+runID+" is alive", KindInfo, EventPayload{})
	return true
}

// emitLocked takes the store lock and writes one event. Every other emit() call
// in the package is made by something that already holds the lock; the runner
// is the first writer that does not, because it spends most of its life
// blocked on a subprocess and holding the floor's lock for twenty minutes would
// stop every reader on it.
func (s *MemStore) emitLocked(t EventType, deskID, text, kind string, p EventPayload) {
	s.mu.Lock()
	e := s.emit(t, deskID, text, kind, p)
	s.mu.Unlock()
	s.publish(e)
}

// moveTaskByID takes the lock and moves one task. moveTaskLocked assumes it is
// already held, same split as every other pair in the store.
//
// A task id that names nothing is NOT an error. The operator-triggered dispatch
// route freezes a packet with a display id (see freezePacketFor) and has no
// planned task behind it at all, so "no record to move" is the ordinary case
// rather than a failure -- and turning it into one would mean the runner
// refused to supervise anything an operator asked for by hand.
//
// A refused TRANSITION is different, and is dropped on the floor here on
// purpose: moveTask already returns the reason, the caller is a supervisor with
// nowhere to put it, and the state machine's job is to refuse the write rather
// than to unwind the run around it.
func (s *MemStore) moveTaskByID(id, to string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.moveTaskLocked(id, to)
}

func (s *MemStore) moveTaskLocked(id, to string) {
	for i := range s.tasks {
		if s.tasks[i].ID == id {
			_ = moveTask(&s.tasks[i], to)
			return
		}
	}
}

// schedulerTick is how often the control loop asks. Seconds rather than
// milliseconds: a pass reads the graph, stats every leased workspace and starts
// processes, and nothing on this floor becomes ready faster than a run finishes.
const schedulerTick = 2 * time.Second

// RunOnce is one pass of the control loop: ask the scheduler what may start,
// and start it.
//
// The clock is read HERE and nowhere below. Schedule is a pure function of what
// it is handed (D-1), and a second read further down would let two tasks in one
// decision be judged against two different nows.
//
// Everything it starts runs in its own goroutine, because superviseRun blocks
// for as long as the work takes and a loop that waited would dispatch one task
// per twenty minutes.
func (s *MemStore) RunOnce(ctx context.Context, ad Adapter) ScheduleDecision {
	now := time.Now().Unix()
	decision := Schedule(s.schedulerInput(now))
	if ad == nil || !runnerEnabled() {
		// The decision still stands and is still worth having -- T-109 renders
		// the blocked half whether or not anything may run. What does not happen
		// is the spending.
		return decision
	}
	for _, d := range decision.Ready {
		s.mu.RLock()
		attempt := s.nextAttemptLocked(d.TaskID)
		s.mu.RUnlock()
		// BR-39 before the egress pass: a product whose workspace does not hold
		// its published Blueprint is refused as drift, with the same event an
		// egress refusal writes, and stays blocked until a sync releases it.
		s.mu.RLock()
		_, drift := s.blueprintGate(d.DeskID)
		s.mu.RUnlock()
		if drift != nil {
			s.refuseDispatch(d, *drift)
			continue
		}
		c, v, err := s.clearFor(d, attempt)
		if err != nil {
			// A refusal on this path used to be a `continue`, and that was a bug
			// found by running it: the scheduler would decline to dispatch, over
			// and over, every two seconds, leaving nothing in the timeline and
			// nothing on the desk. L-5 does not have a scheduler exemption -- "a
			// refused dispatch with its reason appears in the timeline like any
			// other state change" -- and a refusal only the logs know about is
			// indistinguishable from a scheduler that has stopped working.
			//
			// The task goes BLOCKED rather than back to PENDING. An egress refusal
			// is a policy problem an operator has to fix; retrying it on the next
			// tick would spin forever against a packet that cannot change on its
			// own.
			s.refuseDispatch(d, v)
			continue
		}
		if c.empty() {
			// The desk went away between the scheduler's decision and this. Not
			// a refusal and not worth an event: nothing was screened, so there is
			// no state change to describe.
			continue
		}
		// PENDING -> READY -> DISPATCHED, both legal and both meaningful. READY
		// is the scheduler's verdict; DISPATCHED is this loop acting on it, and
		// the gap between them is what boot reconciliation reads.
		s.moveTaskByID(d.TaskID, StatusReady)
		s.moveTaskByID(d.TaskID, StatusDispatched)

		dispatch, task := d, d.TaskID
		go func() {
			if _, err := s.superviseRun(context.WithoutCancel(ctx), ad, dispatch.DeskID, c, attempt); err != nil {
				// The run never started. Put the task back rather than leaving
				// it DISPATCHED forever, which would make the next boot
				// reconcile a run that does not exist.
				s.moveTaskByID(task, StatusReady)
			}
		}()
	}
	return decision
}

// clearFor freezes and screens the packet for one scheduled task. BR-32: the
// only way to a provider is through Screen, and the only thing that returns a
// Cleared is Screen.
// The verdict comes back as well as the packet, and the error separately from
// the boolean, because the two failures are different facts: a desk that has
// gone away is nothing to report, and a packet the egress pass refused is a
// state change L-5 requires on the timeline.
func (s *MemStore) clearFor(d Dispatch, attempt int) (Cleared, EgressVerdict, error) {
	s.mu.RLock()
	idx := s.deskIdxByID(d.DeskID)
	if idx < 0 {
		s.mu.RUnlock()
		return Cleared{}, EgressVerdict{}, nil
	}
	pkt := s.freezePacketFor(s.desks[idx], d.TaskID, attempt)
	if ta, ok := s.adapter.(TurnAdapter); ok {
		pkt.SessionID, pkt.SessionReason = s.sessionForLocked(d.TaskID, ta, time.Now().Unix())
	}
	pol, tgt := s.egress, s.target
	s.mu.RUnlock()

	c, v, err := Screen(pol, tgt, pkt)
	if err != nil {
		return Cleared{}, v, err
	}
	if c.empty() {
		return Cleared{}, v, nil
	}
	return c, v, nil
}

// refuseDispatch records a scheduled dispatch the egress pass refused.
//
// The same two events MemStore.DispatchTask writes on the operator's route, for
// the reason BR-33 gives about adapters and L-5 gives about refusals: which
// route reached the pass is not something the timeline should be able to tell.
func (s *MemStore) refuseDispatch(d Dispatch, v EgressVerdict) {
	s.mu.Lock()
	var ev []Event
	if i := s.deskIdxByID(d.DeskID); i >= 0 {
		dk := &s.desks[i]
		if len(v.Redactions) > 0 {
			ev = append(ev, s.emit(ContextRedacted, dk.ID,
				fmt.Sprintf("Context slice cut by %d part(s) before dispatch", len(v.Redactions)),
				KindWarn, EventPayload{
					Redactions: len(v.Redactions), Bytes: v.Bytes, Digest: v.Digest,
				}))
		}
		if err := s.setState(dk, StateBlocked); err == nil {
			dk.BlockReason = v.Reason
		}
		s.log(dk, "egress: dispatch refused — "+v.Reason, "r")
		ev = append(ev, s.emit(DispatchRefused, dk.ID, v.Reason, KindFail, EventPayload{
			Reason: v.Reason, Rule: v.Rule, Bytes: v.Bytes, Digest: v.Digest,
			Redactions: len(v.Redactions),
		}))
	}
	s.mu.Unlock()

	s.moveTaskByID(d.TaskID, StatusBlocked)
	s.publish(ev...)
}

// ---------------------------------------------------------------- recovery
//
// The pure decision is in reconcile.go. This is the boot pass that reads the
// evidence for it, acts on what it says, and writes the result down.

// claimsFrom gathers the evidence for every task that says it is running.
//
// The two signals come from different places on purpose. The STATUS is in the
// graph, which is what a restart rebuilds from; the LEASE and the BEAT come off
// the workspace, which is what survives when the graph does not. That is also
// why the acceptance test for build-order step 5 cannot run against
// GAOS_STORE=memory: MemStore loses every task record on restart, so there is
// no claim left to reconcile, and recovery is an ArcadeDB-deployment property.
func (s *MemStore) claimsFrom() []RunClaim {
	s.mu.RLock()
	type pending struct{ id, desk, ws, status string }
	var todo []pending
	for _, t := range s.tasks {
		if t.Status != StatusDispatched && t.Status != StatusRunning {
			continue
		}
		todo = append(todo, pending{t.ID, t.DeskID, s.workspaceForDesk(t.DeskID), t.Status})
	}
	s.mu.RUnlock()

	// The filesystem read is outside the lock, same argument schedulerInput
	// makes: one slow disk must not stall every reader on the floor.
	out := make([]RunClaim, 0, len(todo))
	for _, p := range todo {
		claim := RunClaim{TaskID: p.id, DeskID: p.desk, Workspace: p.ws, Status: p.status}
		if l, ok, err := ReadLease(p.ws); err == nil && ok {
			claim.RunID = l.RunID
			claim.LeaseUntil = l.LeaseUntil
			// The beat is not stored separately: every beat renews the lease to
			// now+leaseTTL, so the last renewal IS the last beat and one number
			// carries both. Deriving it here rather than writing it twice means
			// the two cannot disagree, which is worth more than the field.
			claim.LastBeatAt = l.LeaseUntil - leaseTTL
		}
		out = append(out, claim)
	}
	return out
}

// Reconcile is the boot pass. D-4: "this is the whole of recovery; there is no
// other path."
//
// It runs before the listener is up, which is what makes the read-only replay
// handle irrelevant here -- no operator can be in replay yet -- and it is why a
// failure fails the boot. A dashboard that could not work out what was running,
// then served a floor as if nothing were, would look completely healthy while a
// detached process edited a repository nobody was watching.
func (s *MemStore) Reconcile(ctx context.Context) (Result, error) {
	now := time.Now().Unix()
	claims := s.claimsFrom()
	decisions := Reconcile(claims, now, heartbeatTimeout)

	byID := map[string]RunClaim{}
	for _, c := range claims {
		byID[c.TaskID] = c
	}

	s.mu.RLock()
	ad := s.adapter
	s.mu.RUnlock()

	// Gateway turns are recovered by request id, not by lease (D-060).
	var ev []Event
	taken := map[string]bool{}
	if ta, ok := ad.(TurnAdapter); ok {
		taken, ev = s.recoverTurns(ctx, ad, ta)
	}
	for _, d := range decisions {
		if taken[d.TaskID] {
			continue
		}
		c := byID[d.TaskID]
		switch s.resolveAction(d, ad, c) {
		case ReconcileAdopt:
			// The child is alive and the beat is fresh. Nothing is killed and
			// nothing restarted: the task carries on being RUNNING under a new
			// supervisor, which is the whole point of having detached it.
			s.moveTaskByID(d.TaskID, StatusRunning)
			s.setRunStatus(c.RunID, RunStatusRunning)
			ev = append(ev, s.emitReconciled(TaskResumed, c.DeskID,
				"adopted the run on "+d.TaskID, KindInfo, "", d))

		case ReconcileRequeue:
			// D-054, the kill half. The pseudocode's "process gone" is not a
			// fact anybody here has established -- see reconcile.go -- so the
			// run is MADE dead before its workspace is handed on.
			if r, ok, err := ReadRunRecord(c.Workspace); err == nil && ok && ad != nil {
				_ = ad.Stop(r)
			}
			_ = BreakLease(c.Workspace)
			s.moveTaskByID(d.TaskID, StatusReady)
			s.setRunStatus(c.RunID, RunStatusKilled)
			ev = append(ev, s.emitReconciled(TaskResumed, c.DeskID,
				"requeued "+d.TaskID+" after making its unsupervised run dead", KindWarn,
				StateIdle, d))

		case ReconcileOrphan:
			_ = BreakLease(c.Workspace)
			s.moveTaskByID(d.TaskID, StatusFailed)
			s.setRunStatus(c.RunID, RunStatusOrphaned)
			ev = append(ev, s.emitReconciled(TaskOrphaned, c.DeskID,
				"orphaned "+d.TaskID, KindFail, StateBlocked, d))
		}
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	return Result{
		Floor:     s.floor(),
		NewEvents: ev,
		Toast:     fmt.Sprintf("Reconciled %d claimed run(s).", len(decisions)),
	}, nil
}

// attemptOfRun reads an attempt number off the record rather than re-deriving it
// from the run id's suffix. The id's shape is runIDFor's business and parsing it
// back would make that format load-bearing in a second place.
func attemptOfRun(runs []TaskRunRecord, runID string) int {
	for _, r := range runs {
		if r.ID == runID {
			return r.Attempt
		}
	}
	return 0
}

// setRunStatus records what recovery did to an attempt, taking the lock itself.
//
// Separate from markRun so that the reconciliation loop, which holds no lock,
// does not have to grow one around every branch. A run id that names nothing is
// not an error: a task can claim to be dispatched with no lease on disk at all,
// which is precisely the claim the orphan branch exists for.
func (s *MemStore) setRunStatus(runID, status string) {
	if runID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.markRun(runID, status)
}

// resolveAction is the one place the boot pass may overrule the pure decision,
// and it does so in exactly one direction.
//
// Requeue promises that the previous run has been made dead. With no adapter
// configured there is nothing that knows how to kill it, so that promise cannot
// be kept -- and a requeue whose kill silently did nothing is the second run in
// one workspace that D-3 exists to prevent, with a reassuring event in the
// timeline saying it was handled.
//
// So it degrades to orphan. A task that failed and needs a human is a worse
// outcome than a clean requeue and a far better one than silent corruption.
func (s *MemStore) resolveAction(d Reconciliation, ad Adapter, c RunClaim) string {
	if d.Action != ReconcileRequeue {
		return d.Action
	}
	if ad == nil {
		return ReconcileOrphan
	}
	if r, ok, err := ReadRunRecord(c.Workspace); err != nil || !ok || r.Detail["pgid"] == "" {
		// Nothing recorded a process, so nothing can be proved dead.
		return ReconcileOrphan
	}
	return ReconcileRequeue
}

// emitReconciled moves the desk and writes the event, under one lock.
//
// One lock and one function for the same reason finishRun is: the reducer's
// case for this event has to reproduce exactly what the live path did, and two
// separate steps are two chances for them to diverge. deskState may be empty,
// which is the adopt branch -- an adopted run changes nothing on the floor,
// because nothing about it stopped.
//
// D-052's precedent for the payload: adopt and requeue share TaskResumed and
// differ only in Rule, so the catalog stays closed (BR-05) and no reducer has
// to parse a sentence somebody might improve.
func (s *MemStore) emitReconciled(t EventType, deskID, text, kind, deskState string, d Reconciliation) Event {
	s.mu.Lock()
	if deskState != "" {
		if i := s.deskIdxByID(deskID); i >= 0 {
			if err := s.setState(&s.desks[i], deskState); err == nil {
				if deskState == StateBlocked {
					s.desks[i].BlockReason = d.Reason
				} else {
					s.desks[i].BlockReason = ""
				}
			}
		}
	}
	e := s.emit(t, deskID, text, kind, EventPayload{Rule: d.Rule, Reason: d.Reason})
	s.mu.Unlock()
	s.publish(e)
	return e
}

func exitKind(a Attempt) string {
	switch a.Outcome() {
	case HandoffComplete:
		return KindDone
	case HandoffBlocked:
		return KindWarn
	default:
		return KindFail
	}
}

// hostHolder names which orchestrator took a lease. Best effort: it is evidence
// for a human reading a lockfile, never something the code branches on.
func hostHolder() string {
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return "orchestrator"
}
