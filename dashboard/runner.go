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
	if ws == "" {
		return Attempt{}, fmt.Errorf("supervise: the cleared packet has no workspace")
	}
	taskID := c.Packet().TaskID
	if taskID == "" {
		return Attempt{}, fmt.Errorf("supervise: the cleared packet names no task")
	}
	runID := runIDFor(taskID, attempt)
	now := time.Now().Unix()

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

	s.emitLocked(RunStarted, deskID,
		fmt.Sprintf("run %s started, attempt %d", runID, attempt), KindInfo,
		EventPayload{Digest: c.Digest(), Bytes: c.Bytes()})

	// The beat. It renews the lease every heartbeatInterval and writes an event
	// far less often -- D-053.
	done := make(chan struct{})
	go s.beat(deskID, ws, runID, now, done)

	att, err := ad.Dispatch(ctx, c)
	close(done)

	if err != nil {
		s.finishRun(deskID, runID, Attempt{Digest: c.Digest()}, err)
		return Attempt{}, err
	}

	// The last beat before the exit always writes, so a replay has an endpoint
	// for "silent since T" rather than having to infer one from the RunExited.
	s.beatOnce(deskID, ws, runID, time.Now().Unix(), 0, false, true)

	s.finishRun(deskID, runID, att, nil)
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
func (s *MemStore) finishRun(deskID, runID string, att Attempt, runErr error) {
	s.mu.Lock()
	defer s.mu.Unlock()

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
	s.emit(RunExited, deskID, text, kind, p)
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
	defer s.mu.Unlock()
	s.emit(t, deskID, text, kind, p)
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
