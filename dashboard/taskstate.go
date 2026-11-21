package main

import (
	"fmt"
	"sort"
)

// The task state machine. T-042's companion, and the half of the execution
// vocabulary state.go was never able to carry.
//
// This is a SECOND machine, not an extension of the first, and the distinction
// is load-bearing rather than stylistic. state.go is about Desk.State: seven
// lowercase names, one desk per product, each of which model.go says is "a
// distinct CSS selector in app.css (data-state), so adding a state means adding
// a rule". Task.status is a different vocabulary, of a different size, on a
// different cardinality -- many tasks per workflow -- and four of its names
// (PENDING, READY, DISPATCHED, RUNNING) are never a desk's state at all. Four
// CSS rules that could never render is the tell that the two do not belong in
// one table, and setState's signature -- (d *Desk, to string) -- is the other.
//
// The vocabulary is not ours to invent. schema/04-execution.sql declares
// Task.status with a regexp, and TestTheTaskStatusVocabularyMatchesTheDDL
// asserts the two agree in BOTH directions: a Go constant the DDL would refuse
// is a row the scheduler cannot write, and a DDL name Go cannot produce is a
// state nothing can ever reach.
//
// The constants are prefixed Status rather than Task because TaskDispatched,
// TaskBlocked, TaskResumed, TaskDeferred, TaskCompleted, TaskCanceled and
// TaskOrphaned are already EventType constants in events.go. A status and the
// event announcing it are different things and must not answer to one name.
const (
	StatusPending    = "PENDING"
	StatusReady      = "READY"
	StatusDispatched = "DISPATCHED"
	StatusRunning    = "RUNNING"
	StatusReview     = "REVIEW"
	StatusBlocked    = "BLOCKED"
	StatusDeferred   = "DEFERRED"
	StatusDone       = "DONE"
	StatusFailed     = "FAILED"
	StatusCanceled   = "CANCELED"
	// StatusAwaitingInput is M15: the agent ended its turn with a question
	// for the operator (D-064). It is not BLOCKED: a blocker is a fact about
	// the work, a question is a decision only a human can make, and BR-44
	// keeps a waiting task out of the scheduler until that human answers.
	StatusAwaitingInput = "AWAITING_INPUT"
)

// taskTransitions is the whole machine: from -> the set of statuses it may go
// to. A status may always go to itself, same rule and same reasoning as the
// desk table: re-recording a heartbeat on a task that is already RUNNING is a
// real write that leaves the status alone.
//
// Read down the middle and it is the execution flow: a planned task is PENDING
// until its prerequisites clear, READY until the scheduler picks it, DISPATCHED
// until the runner has a process, RUNNING until the work exits, then REVIEW.
//
// Four rows carry an argument rather than a mechanism, and each is the kind of
// row a later reader deletes as an oversight unless it says why it is here:
//
//   - READY -> PENDING is the regression row. A prerequisite that was satisfied
//     can stop being one -- a dependency retried, a task requeued behind it --
//     and without this row the scheduler could only ever ratchet forward,
//     leaving a task READY over a graph that no longer supports it.
//
//   - DEFERRED has no edge to FAILED, on purpose. BR-34: "a task deferred for
//     budget consumes no retry, no escalation and no failure signature, and
//     keeps its place in the graph." This table is where that stops being a
//     sentence in the BRD and becomes something the code refuses to do.
//
//   - DISPATCHED -> READY and RUNNING -> READY are the requeue rows, and they
//     are what boot reconciliation needs (D-4). A run whose supervisor died
//     goes back to the queue; it does not fail, because nothing about it was
//     judged.
//
//   - DONE -> READY is the send-back row (D-084), and it is the one exit from
//     DONE. A design the design gate's review blocked did not ship, and a new
//     task per revision would need the review's and the implementation's
//     DEPENDS_ON edges to move. The row is global in this table and narrow in
//     practice: moveTask refuses it for every task that is not a design, and
//     moveTaskLocked (runner.go) refuses it under every rule but send-back.
//
// CANCELED is terminal and appears with an empty set rather than being left
// out, so "this status goes nowhere" is a decision somebody made instead of a
// lookup that happens to miss.
var taskTransitions = map[string][]string{
	StatusPending:    {StatusReady, StatusBlocked, StatusDeferred, StatusCanceled},
	StatusReady:      {StatusDispatched, StatusPending, StatusBlocked, StatusDeferred, StatusCanceled},
	StatusDispatched: {StatusRunning, StatusReady, StatusBlocked, StatusFailed, StatusCanceled},
	StatusRunning:    {StatusReview, StatusReady, StatusBlocked, StatusFailed, StatusCanceled, StatusAwaitingInput},
	StatusReview:     {StatusDone, StatusReady, StatusBlocked, StatusFailed, StatusCanceled},
	StatusBlocked:    {StatusReady, StatusPending, StatusFailed, StatusCanceled},
	StatusDeferred:   {StatusReady, StatusPending, StatusCanceled},
	StatusDone:       {StatusReady},
	StatusFailed:     {StatusReady, StatusCanceled},
	StatusCanceled:   {},
	// AWAITING_INPUT leaves by an answer (READY), by the operator giving up
	// on the task (CANCELED), or -- D-066 -- by the question outliving the
	// session that asked it (BLOCKED, rule question-expired). No edge to
	// FAILED: nothing was judged.
	StatusAwaitingInput: {StatusReady, StatusCanceled, StatusBlocked},
}

// knownTaskStatus reports whether s is one of the ten. Same split as
// knownState: a status that is not in the table is a typo, not a task that
// cannot move, and the two need different messages or the first person to hit
// it goes looking in the wrong place.
func knownTaskStatus(s string) bool {
	_, ok := taskTransitions[s]
	return ok
}

// taskStatuses returns the declared vocabulary, sorted. Sorted because the
// caller that matters is a test comparing it against the DDL's regexp, and a
// map's iteration order would make that test disagree with itself.
func taskStatuses() []string {
	out := make([]string, 0, len(taskTransitions))
	for s := range taskTransitions {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// canTaskTransition is the machine as a predicate.
func canTaskTransition(from, to string) bool {
	if !knownTaskStatus(from) || !knownTaskStatus(to) {
		return false
	}
	if from == to {
		return true
	}
	for _, allowed := range taskTransitions[from] {
		if allowed == to {
			return true
		}
	}
	return false
}

// moveTask is the boundary. Every write to TaskRecord.Status goes through here,
// exactly as every write to Desk.State goes through setState, and for the same
// reason: a transition enforced by whichever code path happens to run is not
// enforced at all.
//
// The returned error is an ErrRejected carrying a sentence a handler can put in
// a toast -- BR-06's "rejection reason" half.
// canMoveTask asks moveTask's question without writing: the board checks a
// move before it commits to the TaskMoved that records it.
func canMoveTask(t TaskRecord, to string) error { return moveTask(&t, to) }

func moveTask(t *TaskRecord, to string) error {
	if !knownTaskStatus(to) {
		return reject(fmt.Sprintf("%q is not a task status.", to))
	}
	if !knownTaskStatus(t.Status) {
		return reject(fmt.Sprintf("Task %s is in unknown status %q.", t.ID, t.Status))
	}
	if !canTaskTransition(t.Status, to) {
		return reject(fmt.Sprintf("A task cannot go from %s to %s.", t.Status, to))
	}
	// The send-back row is a design task's alone (D-084). Defensive: the board
	// refuses any move out of Done before it asks, and moveTaskLocked refuses
	// the row under any rule but send-back.
	if t.Status == StatusDone && to != StatusDone && taskKind(t.Kind) != TaskKindDesign {
		return reject(fmt.Sprintf("A task cannot go from %s to %s.", t.Status, to))
	}
	t.Status = to
	return nil
}
