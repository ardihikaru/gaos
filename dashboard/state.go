package main

import "fmt"

// The task state machine. T-017, and build-order step 2 in D-018.
//
// BR-06 says every mutation returns the new state or a rejection reason, never a
// bare 200. store.go gave that its shape -- Result and reject() -- and the
// implementation plan was precise about what was still missing: "transitions are
// enforced by the code path that happens to run, not by a boundary that refuses
// an illegal one."
//
// This is the boundary. Every write to Desk.State goes through setState, and
// setState consults one table. The difference is not stylistic. Before this, a
// desk could go from review straight to idle if some future handler forgot the
// intermediate step, and nothing would notice until a fold produced a history
// nobody could explain. Now that move is an ErrRejected with the reason in it.
//
// The states are the six in model.go plus deferred, which D-037 adds and which
// the budget guard (M10.3) will produce.

// transitions is the whole machine: from -> the set of states it may go to.
//
// Read across a row and it is the workflow, in order: an idle desk is dispatched
// to active; active work either blocks, gets deferred, finishes into review, or
// is canceled; review either escalates into a meeting, retries, completes, or is
// canceled; a meeting always resolves back to active because closeMeeting
// applies the decision and re-dispatches; completed loops back to active when
// the next objective is planned.
//
// A state may always go to itself. Approving a gate, recording a proposal or
// resuming a run that was already active are all real mutations that leave the
// state alone, and making those illegal would mean writing the same exception at
// every call site.
var transitions = map[string][]string{
	StateIdle:      {StateActive},
	StateActive:    {StateBlocked, StateDeferred, StateReview, StateMeeting, StateIdle, StateCompleted},
	StateBlocked:   {StateActive, StateIdle},
	StateDeferred:  {StateActive, StateIdle},
	StateReview:    {StateActive, StateMeeting, StateCompleted, StateIdle},
	StateMeeting:   {StateActive, StateIdle},
	StateCompleted: {StateActive, StateIdle},
}

// knownState reports whether s is one of the seven. A state that is not in the
// table at all is not "unable to move", it is a typo, and the two need different
// messages or the first person to hit it will go looking in the wrong place.
func knownState(s string) bool {
	_, ok := transitions[s]
	return ok
}

// canTransition is the machine, as a predicate. Exported shape kept small on
// purpose: the tests drive it directly, and nothing else needs to ask.
func canTransition(from, to string) bool {
	if !knownState(from) || !knownState(to) {
		return false
	}
	if from == to {
		return true
	}
	for _, allowed := range transitions[from] {
		if allowed == to {
			return true
		}
	}
	return false
}

// setState moves a desk, or refuses and explains. Assumes the store's lock is
// held, same as every other private helper in store_mem.go.
//
// The returned error is an ErrRejected carrying a sentence a handler can put in
// a toast -- that is the "rejection reason" half of BR-06, and it is why this
// returns reject() rather than a bare errors.New.
func (s *MemStore) setState(d *Desk, to string) error {
	if !knownState(to) {
		return reject(fmt.Sprintf("%q is not a task state.", to))
	}
	if !knownState(d.State) {
		return reject(fmt.Sprintf("Desk %s is in unknown state %q.", d.ID, d.State))
	}
	if !canTransition(d.State, to) {
		return reject(fmt.Sprintf("A task cannot go from %s to %s.", d.State, to))
	}
	d.State = to
	return nil
}

// mustSetState is setState where the transition is already known to be legal --
// the simulator's own tick, which drives the machine along its declared path.
//
// It panics rather than ignoring the error, and that is deliberate: a transition
// this file believes is unreachable actually being reached is a bug in the
// table or in the caller, and swallowing it would put the desk in a state the
// fold cannot explain. The tests exercise every one of these paths, so a panic
// here is a failing test rather than a production incident.
func (s *MemStore) mustSetState(d *Desk, to string) {
	if err := s.setState(d, to); err != nil {
		panic("state machine: " + err.Error())
	}
}
