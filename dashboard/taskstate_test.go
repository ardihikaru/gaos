package main

import (
	"errors"
	"strings"
	"testing"
)

// The task state machine, T-042's companion. Same shape of test as
// state_test.go, because it is the same kind of object: a table nothing may go
// around, and a table is only worth having if both halves are asserted.

// TestEveryDeclaredTaskTransitionIsPermitted is the positive half. A table that
// refuses a move it declares is as broken as one that permits a move it does
// not, and it is the failure a hand-written transition map gets wrong most
// often.
func TestEveryDeclaredTaskTransitionIsPermitted(t *testing.T) {
	for from, tos := range taskTransitions {
		for _, to := range tos {
			if !canTaskTransition(from, to) {
				t.Errorf("the table declares %s -> %s and canTaskTransition refuses it", from, to)
			}
		}
		if !canTaskTransition(from, from) {
			t.Errorf("%s -> %s refused; recording a beat on a task that is already there is "+
				"still a write, and every call site would need its own exception", from, from)
		}
	}
}

// TestEveryUndeclaredTaskTransitionIsRefused is the negative half. Every row
// carries the reason it is illegal, and the reason is what gets printed -- a
// refusal nobody can explain is a rule somebody will later delete as an
// oversight.
func TestEveryUndeclaredTaskTransitionIsRefused(t *testing.T) {
	illegal := []struct{ from, to, why string }{
		{StatusPending, StatusDispatched, "a task whose prerequisites are unproven has not been scheduled"},
		{StatusPending, StatusRunning, "nothing dispatched it, so no process is carrying it"},
		{StatusPending, StatusDone, "a task nobody ran produced nothing to finish"},
		{StatusReady, StatusRunning, "DISPATCHED is where the lease is taken; skipping it loses the workspace"},
		{StatusReady, StatusFailed, "a task that never started has no attempt to fail"},
		{StatusDispatched, StatusReview, "there is no output to score until a run has exited"},
		{StatusDispatched, StatusDone, "a dispatch is not a delivery"},
		{StatusRunning, StatusDone, "REVIEW is not optional; BR-13 scores every attempt"},
		{StatusDeferred, StatusFailed, "BR-34: a deferral consumes no retry and leaves no failure signature"},
		{StatusDeferred, StatusDispatched, "a deferred task rejoins the queue at READY, it does not jump it"},
		{StatusDone, StatusReady, "the work shipped; re-running it is a new task, not a new status"},
		{StatusDone, StatusFailed, "a finished task cannot be retroactively unfinished"},
		{StatusCanceled, StatusReady, "cancellation is terminal, or it is a pause with a misleading name"},
		{StatusBlocked, StatusRunning, "the blocker has to clear through READY so the scheduler sees it"},
		{StatusFailed, StatusDone, "a failed attempt is retried, never promoted"},
	}
	for _, c := range illegal {
		if canTaskTransition(c.from, c.to) {
			t.Errorf("%s -> %s is permitted; %s", c.from, c.to, c.why)
		}
	}

	// A status nobody declared is a typo, and it gets its own message so the
	// first person to hit it does not go reading the transition table.
	if canTaskTransition(StatusRunning, "SHIPPING") {
		t.Error(`"SHIPPING" is not a task status and the machine accepted it`)
	}
	if canTaskTransition("SHIPPING", StatusRunning) {
		t.Error("the machine accepted a move out of a status it does not declare")
	}
}

// TestADeferredTaskCanNeverGoStraightToFailed is BR-34 as its own test rather
// than one row in the table above.
//
// It gets its own function because of how it is likely to break. DEFERRED ->
// FAILED looks like an omission: every other non-terminal status can reach
// FAILED, and a reader adding the missing row would be making the table
// symmetrical and the product wrong. BR-34 is explicit -- "a task deferred for
// budget consumes no retry, no escalation and no failure signature, and keeps
// its place in the graph" -- and a deferral that failed would consume all
// three.
func TestADeferredTaskCanNeverGoStraightToFailed(t *testing.T) {
	if canTaskTransition(StatusDeferred, StatusFailed) {
		t.Fatal("DEFERRED -> FAILED is permitted; BR-34 says a deferral is not a failure, " +
			"and a task that failed for want of budget has spent a retry it never used")
	}

	task := TaskRecord{ID: "wf-9-t-1", Status: StatusDeferred}
	err := moveTask(&task, StatusFailed)
	if err == nil {
		t.Fatal("moveTask carried a deferred task to FAILED; the table is the boundary " +
			"and a caller that can go around it means there is no boundary")
	}
	if !errors.Is(err, ErrRejected) {
		t.Errorf("moveTask returned %v, which is not an ErrRejected; BR-06 wants a reason "+
			"a handler can put in a toast, not a 500", err)
	}
	if !strings.Contains(err.Error(), "DEFERRED") || !strings.Contains(err.Error(), "FAILED") {
		t.Errorf("the refusal reads %q and names neither status; an operator cannot act on that", err)
	}
	if task.Status != StatusDeferred {
		t.Errorf("the task is %q after a refused move, want DEFERRED — a rejected transition "+
			"that still wrote is worse than one that was never checked", task.Status)
	}
}
