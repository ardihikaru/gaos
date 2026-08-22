package main

import (
	"fmt"
	"sort"
	"strings"
)

// The scheduler. T-048, and D-1 asks for something stronger than a working one:
//
//	"The scheduler is a pure function of the graph — two replicas computing it
//	 independently reach the same answer."
//
// The reason is in the sentence after it. Leader election then protects against
// duplicate DISPATCH, not against divergent STATE, and a restart stops being
// interesting -- which is the whole of BR-01's "the orchestrator holds no
// authoritative state" at the one layer that would otherwise need some.
//
// # What this file may not touch
//
// No clock, no rand, no lock, no store, no filesystem, no network, no context.
// The import list is exactly {fmt, sort, strings} and
// TestTheSchedulerIsAPureFunctionOfTheGraph asserts that by parsing this file:
// a pure function that imports time is one edit away from calling time.Now(),
// and the diff that adds the import is the diff a reviewer can see. The same
// test refuses any declaration here with a receiver, because a method on
// *MemStore would have the lock and the rng in scope by definition.
//
// The clock arrives as SchedulerInput.Now, read once by the impure shell
// (MemStore.schedulerInput) and passed down as a value.
//
// graph.go is called from here and holds to the same discipline. It is not in
// the allowlist because it is a different file; it is safe because a
// topological sort that varied between processes would break D-1 one layer down
// rather than one layer up.
//
// # What it does NOT decide
//
// Not which agent is best for the work -- that is routing, T-044, and it comes
// later with a capability model. Not how many runs a deployment may have in
// flight at once: a global concurrency cap is tenancy and capacity, which is
// M10, and putting one here would invent a sixth blocking reason that BRD §7.4
// does not have. Capacity here is the agents, and nothing else.

// The five blocking reasons, and there are exactly five.
//
// BRD §7.4 is unusually prescriptive: autonomy begins when the scheduler can
// find a ready task without a human naming it, "which requires five things to
// already be true at once ... Miss any one and the floor looks idle for reasons
// nobody can explain. All five must be surfaced in the UI as the blocking
// reason -- this is a business requirement, not a nicety, because an
// unexplained idle floor is indistinguishable from a broken product."
//
// So they are constants rather than strings at call sites, the count is
// asserted in a test, and T-109 renders them. A cycle is BlockedByDependency
// with its members in Detail rather than a sixth reason, because the BRD makes
// acyclicity part of the dependency condition rather than a separate one.
const (
	BlockedByDependency = "dependency"
	BlockedByCapability = "capability"
	BlockedByLease      = "lease"
	BlockedByReadiness  = "readiness"
	BlockedByBudget     = "budget"
)

// blockingReasons is the closed set, in precedence order. See Schedule.
var blockingReasons = []string{
	BlockedByReadiness,
	BlockedByDependency,
	BlockedByCapability,
	BlockedByBudget,
	BlockedByLease,
}

// The budget zones of BRD §8.10.1. The guard that produces anything but normal
// is M10.3 and is not built, so today SchedulerInput.Budget is always
// BudgetNormal -- but the branch exists and is covered by a test that
// constructs a throttled input directly, because a reason with no producer is
// dead code that rots before the module arrives.
const (
	BudgetNormal    = "normal"
	BudgetThrottled = "throttled"
	BudgetCapped    = "capped"
)

// TaskNode is one vertex as the scheduler sees it: the fields a scheduling
// decision reads and no others. Deliberately not TaskRecord -- the lease and
// the heartbeat are on that record and are M4.2's, and a decision that could
// read them would be tempted to.
type TaskNode struct {
	ID         string
	WorkflowID string
	DeskID     string
	Title      string
	Role       string
	Model      string
	Status     string
	Deps       []string
	Priority   int
	Ordinal    int
	Gate       bool
}

// AgentSlot is a seat. Busy is computed by the shell from the floor; whether an
// agent is busy is a fact about the world, not something the scheduler infers.
type AgentSlot struct {
	ID   string
	Role string
	Busy bool
}

// LeaseState is one workspace lease as it stands. TaskID is who holds it, so a
// task is never blocked by its own lease. Until is unix seconds, compared
// against SchedulerInput.Now -- never against a clock read here.
type LeaseState struct {
	Workspace string
	TaskID    string
	Until     int64
}

// SchedulerInput is everything the decision may read. There is no other input,
// and that is the point of the type: a scheduler taking a *MemStore could reach
// anything, and "pure" would be a comment rather than a property.
type SchedulerInput struct {
	Tasks      []TaskNode
	Agents     []AgentSlot
	Leases     []LeaseState
	Readiness  map[string]int    // deskID (productID) -> readiness score
	Workspaces map[string]string // taskID -> workspace path
	Budget     string
	Now        int64 // unix seconds, passed in
}

// Dispatch is one task the scheduler says may start, with everything the runner
// needs to start it. The agent and the workspace are named here rather than
// chosen later, because two tasks handed the same agent or the same workspace
// would be a decision the scheduler made and did not record.
type Dispatch struct {
	TaskID     string
	WorkflowID string
	DeskID     string
	Role       string
	AgentID    string
	Workspace  string
	Model      string
}

// BlockedTask is one task that may not start, and why. Detail is the sentence
// under the reason -- "waits on wf-7-t-3", "no free qa agent" -- because a
// reason without a specific is a dead end (B-3), and T-109 has to render
// something an operator can act on.
type BlockedTask struct {
	TaskID string
	Reason string
	Detail string
}

// ScheduleDecision is the whole answer. Both halves matter: Ready is what the
// runner starts, and Blocked is what the floor shows instead of looking idle.
//
// Named ScheduleDecision rather than Decision because model.go already has a
// Decision -- an ADR on the product master. Same collision the task statuses
// have with the event names, and the same resolution: two different things do
// not get to answer to one name.
type ScheduleDecision struct {
	Ready   []Dispatch
	Blocked []BlockedTask
}

// Schedule is the pure function.
//
// # Determinism
//
// Every collection is sorted on entry, and the maps are only ever indexed.
// This matters more than it looks: two replicas reading the same ArcadeDB have
// no guaranteed row order across buckets, so a scheduler that inherited slice
// order would agree in a naive test -- where both got the same slice -- and
// disagree in production. TestTwoReplicasSchedulingTheSameGraphAgree shuffles
// one copy for exactly that reason.
//
// # Precedence, not sequence
//
// Every reason is computed before one is reported, and the one reported is the
// first in blockingReasons. This is the same rule Screen states for the egress
// pass: "The order below is PRECEDENCE, not sequence ... so the sentence the
// operator reads is a function of the packet rather than of the order the
// checks happened to run in."
//
// Readiness first, because below the planning floor nothing else is worth
// saying. Lease last, because it is the only one of the five that clears by
// itself.
func Schedule(in SchedulerInput) ScheduleDecision {
	tasks := append([]TaskNode(nil), in.Tasks...)
	sort.Slice(tasks, func(i, j int) bool {
		a, b := tasks[i], tasks[j]
		if a.WorkflowID != b.WorkflowID {
			return a.WorkflowID < b.WorkflowID
		}
		if a.Ordinal != b.Ordinal {
			return a.Ordinal < b.Ordinal
		}
		return a.ID < b.ID
	})

	agents := append([]AgentSlot(nil), in.Agents...)
	sort.Slice(agents, func(i, j int) bool { return agents[i].ID < agents[j].ID })

	leases := append([]LeaseState(nil), in.Leases...)
	sort.Slice(leases, func(i, j int) bool { return leases[i].Workspace < leases[j].Workspace })

	status := make(map[string]string, len(tasks))
	for _, t := range tasks {
		status[t.ID] = t.Status
	}

	// The cycle check. CreateWorkflow refuses one at planning time, so finding
	// a cycle here means the graph was written by something else or edited
	// since -- and the scheduler must say so rather than walking the same
	// unsatisfiable set on every pass forever.
	inCycle := map[string]string{}
	{
		ids := make([]string, len(tasks))
		deps := make(map[string][]string, len(tasks))
		for i, t := range tasks {
			ids[i] = t.ID
			deps[t.ID] = t.Deps
		}
		if _, ce := topoIDs(ids, deps); ce != nil {
			members := strings.Join(ce.Members, ", ")
			for _, id := range ce.Members {
				inCycle[id] = members
			}
		}
	}

	// Leases held right now, by workspace. A lease whose expiry has passed is
	// not a lease -- D-3 gives every one an expiry precisely so a dead holder
	// stops blocking the queue without anybody intervening.
	heldBy := map[string]LeaseState{}
	for _, l := range leases {
		if l.Until > in.Now {
			heldBy[l.Workspace] = l
		}
	}

	// Seats taken as we go, so two tasks in one pass cannot be handed the same
	// agent. This is state, but it is state of THIS call over its own inputs --
	// the function is still a value in, value out.
	taken := map[string]bool{}
	for _, a := range agents {
		if a.Busy {
			taken[a.ID] = true
		}
	}
	claimed := map[string]bool{}

	var out ScheduleDecision
	for _, t := range tasks {
		if t.Status != StatusPending && t.Status != StatusReady {
			continue
		}
		reasons := map[string]string{}

		// 1. Readiness. BR-07: below the floor the planner refuses to plan and
		// nothing is dispatched. A graph planned before the record decayed is
		// exactly how a task reaches here under a failing gate.
		if score, ok := in.Readiness[t.DeskID]; ok && score < PlanFloor {
			reasons[BlockedByReadiness] = fmt.Sprintf(
				"readiness %d is below the planning floor of %d", score, PlanFloor)
		}

		// 2. Dependency, cycles included.
		if members, ok := inCycle[t.ID]; ok {
			reasons[BlockedByDependency] = "dependency cycle through " + members
		} else if waiting := unmetDeps(t, status); len(waiting) > 0 {
			reasons[BlockedByDependency] = "waits on " + strings.Join(waiting, ", ")
		}

		// 3. Capability: a capable agent exists for the role AND can take it.
		// An agent that exists and is busy is not a capable agent for this
		// task, and the two cases get different sentences because the
		// operator's next action differs -- hire versus wait.
		seat := freeAgent(agents, t.Role, taken)
		if seat == "" {
			if countRole(agents, t.Role) == 0 {
				reasons[BlockedByCapability] = "no agent holds the " + t.Role + " role"
			} else {
				reasons[BlockedByCapability] = "every " + t.Role + " agent is busy"
			}
		}

		// 4. Budget. BR-35: a limit that only warns is not a limit, so the
		// scheduler refuses rather than reports. BR-34: this is a deferral, not
		// a failure -- nothing here touches a retry count.
		switch in.Budget {
		case BudgetCapped:
			reasons[BlockedByBudget] = "the budget cap is reached; dispatch is refused until the window moves"
		case BudgetThrottled:
			reasons[BlockedByBudget] = "the budget is throttled; ready work is deferred with a resume time"
		}

		// 5. Lease. D-3: exclusive, with an expiry, because "two runs in one
		// workspace corrupt each other silently".
		ws := in.Workspaces[t.ID]
		switch {
		case ws == "":
			// No workspace root configured, so there is nothing to be exclusive
			// about. Today that is every deployment; see MemStore.workspaceFor.
		case claimed[ws]:
			reasons[BlockedByLease] = ws + " was taken by an earlier task in this same pass"
		default:
			if l, held := heldBy[ws]; held && l.TaskID != t.ID {
				reasons[BlockedByLease] = fmt.Sprintf("%s is leased to %s for another %ds",
					ws, l.TaskID, l.Until-in.Now)
			}
		}

		if len(reasons) > 0 {
			for _, r := range blockingReasons {
				if detail, ok := reasons[r]; ok {
					out.Blocked = append(out.Blocked, BlockedTask{
						TaskID: t.ID, Reason: r, Detail: detail,
					})
					break
				}
			}
			continue
		}

		taken[seat] = true
		if ws != "" {
			claimed[ws] = true
		}
		out.Ready = append(out.Ready, Dispatch{
			TaskID: t.ID, WorkflowID: t.WorkflowID, DeskID: t.DeskID,
			Role: t.Role, AgentID: seat, Workspace: ws, Model: t.Model,
		})
	}
	return out
}

// unmetDeps lists the prerequisites that have not finished, in the order they
// are declared on the task. A dependency naming a task that is not in the graph
// at all counts as unmet and says so, rather than being silently satisfied --
// an edge pointing at nothing must not read as "nothing to wait for".
func unmetDeps(t TaskNode, status map[string]string) []string {
	var out []string
	for _, dep := range t.Deps {
		st, ok := status[dep]
		if !ok {
			out = append(out, dep+" (not in the graph)")
			continue
		}
		if st != StatusDone {
			out = append(out, dep)
		}
	}
	return out
}

// freeAgent returns the first unclaimed agent holding the role, scanning in
// sorted id order. First rather than best: choosing best is routing, T-044, and
// it needs a capability model this does not have.
func freeAgent(agents []AgentSlot, role string, taken map[string]bool) string {
	for _, a := range agents {
		if a.Role == role && !taken[a.ID] {
			return a.ID
		}
	}
	return ""
}

func countRole(agents []AgentSlot, role string) int {
	n := 0
	for _, a := range agents {
		if a.Role == role {
			n++
		}
	}
	return n
}
