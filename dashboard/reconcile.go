package main

import (
	"fmt"
	"sort"
)

// Boot reconciliation. T-053, and D-4 does not hedge about its importance:
//
//	"On boot, reconcile every task claiming to be dispatched or running against
//	 its lease and heartbeat: adopt, requeue, or fail as orphaned. This is the
//	 whole of recovery; there is no other path."
//
// Because execution does not die with orchestration (D-2), a restart finds
// tasks that claim to be running. The recovery is a reconciliation, not a
// reset, and the architecture page states the shape:
//
//	for task in tasks where status in (DISPATCHED, RUNNING):
//	    if lease > now and beat > now - heartbeat_timeout:  adopt
//	    elif lease > now:                                    requeue
//	    else:                                                orphan
//
// "A task that cannot be proved alive is not assumed alive. The heartbeat and
// the lease are two independent signals precisely so that a hung process and a
// dead one are distinguishable."
//
// # The correction this file makes to that pseudocode
//
// The middle branch's comment reads "lease held, process gone". IT DOES NOT
// KNOW THE PROCESS IS GONE. It knows the BEAT is stale, and a stale beat means
// the supervisor died -- which says nothing whatever about the child, because
// D-2 detached the child precisely so it would outlive its supervisor.
//
// Requeuing on that basis starts a second run in a workspace that may still
// have a live one in it: the exact silent corruption D-3 exists to prevent,
// reached through the recovery routine. So requeue is KILL-THEN-REQUEUE. A run
// that can be proved neither alive nor dead is MADE dead before its workspace
// is handed to anybody. D-054.
//
// That does not contradict BR-35's "never kills a run already in flight", and
// the difference is the subject rather than the verb. BR-35 is a BUDGET rule
// about a run this orchestrator is supervising and whose spend it can still
// account for; forfeiting that is throwing away money already spent. This is a
// RECOVERY rule about an unsupervised run whose supervisor is already gone.
// Different subject, opposite risk.
//
// # Purity
//
// Reconcile itself is a pure function, held to the same discipline as the
// scheduler and for the same reason: it is the whole of recovery, it runs
// exactly once per boot, and a decision that varied between two runs over the
// same evidence would be untestable in the only way that matters. The clock
// arrives as a parameter. The killing and the writing are the caller's.

// The three actions. Named rather than inlined so the reconciler's own tests,
// the events it produces and the reasons it records cannot drift apart.
const (
	ReconcileAdopt   = "adopt"
	ReconcileRequeue = "requeue"
	ReconcileOrphan  = "orphan"
)

// The machine-readable reasons, carried in EventPayload.Rule.
//
// D-052 set the precedent when it named DispatchRefused: the rule travels in
// the payload as a value, never parsed out of the sentence, because "a reducer
// that had to parse the sentence would break the first time somebody improved
// the wording". Adopt and requeue reuse TaskResumed and differ only here.
const (
	RuleAdopted      = "adopted"
	RuleRequeued     = "requeued"
	RuleLeaseExpired = "lease-expired"
)

// RunClaim is one task that says it is running, plus the two independent
// signals D-4 weighs it against.
type RunClaim struct {
	TaskID     string
	DeskID     string
	Workspace  string
	Status     string
	LeaseUntil int64
	LastBeatAt int64
}

// Reconciliation is what to do about one claim, and why.
type Reconciliation struct {
	TaskID string
	Action string
	Rule   string
	Reason string
}

// Reconcile weighs every claim. Pure: no clock, no filesystem, no store.
//
// Sorted on entry for the same reason the scheduler sorts: this runs on a
// restart, possibly on a replica, and a recovery whose answer depended on row
// order would be a recovery nobody could reproduce from the record.
func Reconcile(claims []RunClaim, now, staleAfter int64) []Reconciliation {
	in := append([]RunClaim(nil), claims...)
	sort.Slice(in, func(i, j int) bool { return in[i].TaskID < in[j].TaskID })

	out := make([]Reconciliation, 0, len(in))
	for _, c := range in {
		// D-4's scope, and it is a scope rather than a filter: a task in any
		// other status is not making a claim about a process, so there is
		// nothing to reconcile and touching it would be inventing a recovery.
		if c.Status != StatusDispatched && c.Status != StatusRunning {
			continue
		}
		switch {
		case c.LeaseUntil > now && c.LastBeatAt > now-staleAfter:
			out = append(out, Reconciliation{
				TaskID: c.TaskID, Action: ReconcileAdopt, Rule: RuleAdopted,
				Reason: fmt.Sprintf("lease holds for another %ds and the last beat was %ds ago",
					c.LeaseUntil-now, now-c.LastBeatAt),
			})
		case c.LeaseUntil > now:
			out = append(out, Reconciliation{
				TaskID: c.TaskID, Action: ReconcileRequeue, Rule: RuleRequeued,
				Reason: fmt.Sprintf("the lease holds but the last beat was %ds ago; "+
					"the supervisor is gone and the child must be made dead before the "+
					"workspace is handed on", now-c.LastBeatAt),
			})
		default:
			out = append(out, Reconciliation{
				TaskID: c.TaskID, Action: ReconcileOrphan, Rule: RuleLeaseExpired,
				Reason: fmt.Sprintf("the lease expired %ds ago and no beat since", now-c.LeaseUntil),
			})
		}
	}
	return out
}
