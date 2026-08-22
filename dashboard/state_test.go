package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// The task state machine, T-017 and the half of BR-06 that was missing.

// TestStateMachineAllowsTheDeclaredTransitions walks every move the table says
// is legal and requires it to be taken. The positive half: a machine that
// refuses legal work is as broken as one that permits illegal work, and it is
// the failure mode a transition table gets wrong most often.
func TestStateMachineAllowsTheDeclaredTransitions(t *testing.T) {
	for from, tos := range transitions {
		for _, to := range tos {
			if !canTransition(from, to) {
				t.Errorf("the table declares %s -> %s and canTransition refuses it", from, to)
			}
		}
		if !canTransition(from, from) {
			t.Errorf("%s -> %s refused; a mutation that leaves the state alone is still "+
				"a mutation, and every call site would need its own exception", from, from)
		}
	}
}

// The negative half. These are the moves nothing may make, and the ones that
// used to be prevented only by no code path happening to attempt them.
func TestStateMachineRefusesIllegalTransitions(t *testing.T) {
	illegal := []struct{ from, to, why string }{
		{StateIdle, StateReview, "an empty desk has produced nothing to review"},
		// idle -> blocked USED TO BE HERE, with the reason "nothing is running,
		// so nothing can be blocked". BR-32 created the counter-example on
		// 2026-08-22: a dispatch the pre-dispatch egress pass refuses is a task
		// that never started, must not look idle, and needs an operator to
		// change a policy before it can. It is asserted as LEGAL in
		// TestARefusedDispatchBlocksTheDeskAndSaysWhy, and the row is inverted
		// here rather than deleted so the reversal is visible to a reader who
		// remembers the old rule.
		{StateIdle, StateCompleted, "a workflow nobody started cannot have finished"},
		{StateIdle, StateMeeting, "there is no work to hold a design review about"},
		{StateIdle, StateDeferred, "the budget guard defers ready work, not empty desks"},
		{StateReview, StateBlocked, "review is not waiting on a dependency, it is waiting on a verdict"},
		{StateReview, StateDeferred, "the attempt is already spent; deferring it now saves nothing"},
		{StateBlocked, StateReview, "blocked work has not been finished, so there is nothing to score"},
		{StateBlocked, StateCompleted, "a blocker cannot resolve itself into a shipped workflow"},
		{StateBlocked, StateMeeting, "call the review from the running task, not from the block"},
		{StateCompleted, StateReview, "the work already shipped; re-scoring it is a new attempt"},
		{StateCompleted, StateBlocked, "nothing is running to be blocked"},
		{StateMeeting, StateReview, "closeMeeting applies the decision and re-dispatches"},
		{StateMeeting, StateCompleted, "a decision is not a delivery"},
		{StateDeferred, StateReview, "deferred work never ran, so it produced nothing to review"},
		{StateDeferred, StateBlocked, "BR-34: a deferral is not a failure and not a blocker"},
	}
	for _, c := range illegal {
		if canTransition(c.from, c.to) {
			t.Errorf("%s -> %s is permitted; %s", c.from, c.to, c.why)
		}
	}

	// A state nobody declared is a typo, and it gets its own message so the
	// first person to hit it does not go looking in the transition table.
	if canTransition(StateActive, "shipping-it") {
		t.Error(`"shipping-it" is not a task state and the machine accepted it`)
	}
	if canTransition("shipping-it", StateActive) {
		t.Error(`the machine accepted a move out of a state it does not declare`)
	}
}

// setState is the boundary. An illegal move must come back as an ErrRejected
// carrying a sentence, not as a silent no-op and not as a 500 -- that is exactly
// what BR-06 asks a mutation to return.
func TestSetStateRejectsWithAReason(t *testing.T) {
	s := NewMemStore(1)
	d := &Desk{ID: "p-1", State: StateIdle}

	err := s.setState(d, StateCompleted)
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("illegal transition returned %v, want ErrRejected", err)
	}
	if !strings.Contains(err.Error(), "cannot go from idle to completed") {
		t.Errorf("the rejection reads %q; it has to name both states or it explains nothing", err)
	}
	if d.State != StateIdle {
		t.Errorf("a refused transition still moved the desk to %q", d.State)
	}

	if err := s.setState(d, StateActive); err != nil {
		t.Fatalf("idle -> active is declared legal and was refused: %v", err)
	}
	if d.State != StateActive {
		t.Errorf("a permitted transition left the desk in %q", d.State)
	}

	err = s.setState(d, "nonsense")
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("an undeclared target returned %v, want ErrRejected", err)
	}
	if !strings.Contains(err.Error(), "is not a task state") {
		t.Errorf("an undeclared target reads %q; it must say the name is unknown "+
			"rather than that the move is illegal", err)
	}
}

// Every state a desk can actually be in is a state the machine knows. A desk
// sitting in a state absent from the table would be stuck: nothing could move it
// and the reason would be a message about a typo.
func TestEveryReachableDeskStateIsDeclared(t *testing.T) {
	ctx := context.Background()
	s := runAWorkflow(t, 17, 200)

	f, _ := s.Snapshot(ctx)
	for _, d := range f.Desks {
		if !knownState(d.State) {
			t.Errorf("desk %s is in state %q, which the transition table does not declare", d.ID, d.State)
		}
	}

	// And every state the reducer can produce, which is the same set seen from
	// the other side -- a fold that lands a desk somewhere the machine does not
	// know would make replay and live disagree about what is even possible.
	folded := fold(emptyFloor(), s.events, s.events[len(s.events)-1].Seq)
	for _, d := range folded.Desks {
		if !knownState(d.State) {
			t.Errorf("the fold put desk %s in state %q, which is not a declared state", d.ID, d.State)
		}
	}
}

// The mutations that go through the machine still answer with a reason rather
// than a bare success or a 500 -- BR-06 end to end, through the seam.
func TestMutationsRefusedByTheMachineExplainThemselves(t *testing.T) {
	ctx := context.Background()
	s := NewMemStore(1)

	f, _ := s.Snapshot(ctx)
	idle := -1
	for i, d := range f.Desks {
		if d.State == StateIdle {
			idle = i
			break
		}
	}
	if idle < 0 {
		t.Skip("no idle desk in the fixtures")
	}

	// An idle desk has no task, so both of these are refused before the machine
	// is even consulted -- and the refusal still carries a sentence.
	for name, err := range map[string]error{
		"RetryTask":  mustErr(s.RetryTask(ctx, idle)),
		"CancelTask": mustErr(s.CancelTask(ctx, idle)),
	} {
		if !errors.Is(err, ErrRejected) {
			t.Errorf("%s on an idle desk returned %v, want ErrRejected", name, err)
		}
		if err != nil && err.Error() == "" {
			t.Errorf("%s refused without saying why", name)
		}
	}
}

func mustErr(_ Result, err error) error { return err }
