package main

import (
	"context"
	"reflect"
	"testing"
)

// The supervisor (M4.2, re-based on the gateway in M14.7): what a turn writes
// to the timeline, the heartbeat's two cadences, and BR-33's promise that a
// recovered turn and a supervised one are the same Attempt.

// TestARunEmitsRunStartedAndRunExitedAroundTheWorkItSupervised is the positive
// case: the events exist, in order, and the cost the gateway reported reached
// the desk.
func TestARunEmitsRunStartedAndRunExitedAroundTheWorkItSupervised(t *testing.T) {
	s := NewMemStore(3)
	f, a := gatewayOn(t, s)
	completeTurn(f, "t-1", 1, "0.42")
	deskID := s.desks[0].ID
	before := len(s.rawEvents())

	att, err := s.superviseRun(t.Context(), a, deskID, clearedFor(t, s, deskID, "t-1", 1), 1)
	if err != nil {
		t.Fatalf("supervising one turn failed: %v", err)
	}
	if att.Outcome() != HandoffComplete {
		t.Fatalf("the turn reported %q, want %q; the handoff block is the only authority "+
			"on what happened (D-050, D-057)", att.Outcome(), HandoffComplete)
	}

	var got []string
	for _, ty := range typesOf(s.rawEvents()[before:]) {
		if ty != string(SessionOpened) { // the session is transport (BR-41), not the run's shape
			got = append(got, ty)
		}
	}
	want := []string{string(RunStarted), string(Heartbeat), string(RunExited)}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the turn wrote %v, want %v; RunStarted before the work and RunExited after it "+
			"is the shape a replay reads a run's duration from", got, want)
	}

	s.mu.RLock()
	deskCost := s.desks[0].Cost
	s.mu.RUnlock()
	if att.CostBasis != CostMeasured || deskCost <= 0 {
		t.Errorf("the attempt's basis is %q and the desk reads %.2f; D-062 measures the gateway's "+
			"cost_usd, and a cost nothing recorded is a cost the budget guard will never see",
			att.CostBasis, deskCost)
	}
}

// TestARunThatCouldNotStartEmitsNoRunStarted is the negative half: emitting a
// RunStarted for a turn that was never sent is inventing evidence in the one
// record the audit story rests on. A brief that names no task never reaches
// the gateway.
func TestARunThatCouldNotStartEmitsNoRunStarted(t *testing.T) {
	s := NewMemStore(3)
	f, a := gatewayOn(t, s)
	deskID := s.desks[0].ID
	before := len(s.rawEvents())

	if _, err := s.superviseRun(t.Context(), a, deskID, clearedFor(t, s, deskID, "", 1), 1); err == nil {
		t.Fatal("a brief naming no task was supervised as a turn")
	}
	if after := s.rawEvents()[before:]; len(after) != 0 || len(f.bodies) != 0 {
		t.Errorf("a turn that never started wrote %v and sent %d request(s); a RunStarted for "+
			"a turn nobody sent is invented evidence", typesOf(after), len(f.bodies))
	}
}

// TestTheHeartbeatWritesTheBeatMoreOftenThanItWritesAnEvent is D-053 made
// checkable, against the pure half so it does not take five minutes to run.
func TestTheHeartbeatWritesTheBeatMoreOftenThanItWritesAnEvent(t *testing.T) {
	// Twelve minutes of beats at the declared interval.
	const start = 1_000_000
	beats, events := 0, 0
	last := int64(start)
	for now := int64(start); now < start+12*60; now += heartbeatInterval {
		beats++
		first := now == start
		if beatEmitsEvent(last, now, first, false) {
			events++
			last = now
		}
	}
	if beats != 24 {
		t.Fatalf("twelve minutes at a %ds interval is %d beats, want 24", heartbeatInterval, beats)
	}
	if events != 3 {
		t.Errorf("the same twelve minutes wrote %d Heartbeat events, want 3; D-053 separates the "+
			"cadences so a run does not put one pill per beat into a stream that is append-only, "+
			"snapshotted every %d events and rendered in full", events, snapshotEvery)
	}
	if !beatEmitsEvent(start, start, true, false) {
		t.Error("the first beat of a run wrote no event; a run with no opening beat has no " +
			"start for a replay to measure silence from")
	}
	if !beatEmitsEvent(start, start+1, false, true) {
		t.Error("the last beat before an exit wrote no event")
	}
	if beatEmitsEvent(start, start+heartbeatEventEvery-1, false, false) {
		t.Error("a beat inside the event cadence wrote an event anyway; the whole point of two " +
			"numbers is that most beats are field writes")
	}
}

// TestAnAdoptedRunAndASupervisedRunProduceTheSameAttempt is BR-33 as a
// comparison rather than a promise. Recovery polls a request this process did
// not submit, through Resume -- a second entry point, and a second place for
// the two to drift apart. They share poll() so they cannot.
func TestAnAdoptedRunAndASupervisedRunProduceTheSameAttempt(t *testing.T) {
	s := NewMemStore(3)
	f, a := gatewayOn(t, s)
	completeTurn(f, "t-1", 1, "0.3")
	c := clearedFor(t, s, s.desks[0].ID, "t-1", 1)

	supervised, err := a.Dispatch(context.Background(), c)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	adopted, err := a.Resume(context.Background(), c, RunRecord{
		RunID: runIDFor("t-1", 1), Digest: c.Digest(),
		Detail: map[string]string{"requestId": a.RequestIDFor(runIDFor("t-1", 1))},
	})
	if err != nil {
		t.Fatalf("resume: %v", err)
	}

	// The prompt is the one honest difference: the adopting process did not
	// send it, and the record already holds it from the process that did.
	supervised.Prompt = ""
	if !reflect.DeepEqual(supervised, adopted) {
		t.Errorf("an adopted turn produced %+v and a supervised one %+v; BR-33 asks that nothing "+
			"above the seam be able to tell them apart", adopted, supervised)
	}
	if adopted.CostBasis != CostMeasured {
		t.Errorf("the adopted turn's basis is %q, want measured; the cost travels with the "+
			"request's result precisely so this works", adopted.CostBasis)
	}
}
