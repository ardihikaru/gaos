package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"math/rand/v2"
	"reflect"
	"strings"
	"testing"
)

// T-048 and T-049. The scheduler, and the property D-1 asks for that a working
// scheduler would not have on its own.

// oneReadyGraph builds an input with a single task that clears all five
// conditions, so a test can break exactly one of them and see the reason
// change. Everything is explicit -- no fixtures, no store -- because the
// function under test is supposed to be readable from its arguments alone.
func oneReadyGraph() SchedulerInput {
	return SchedulerInput{
		Tasks: []TaskNode{
			{ID: "wf-9-t-1", WorkflowID: "wf-9", DeskID: "p-1", Role: "backend",
				Model: "claude-sonnet-5", Status: StatusPending, Ordinal: 0},
		},
		Agents:     []AgentSlot{{ID: "a-1", Role: "backend"}},
		Readiness:  map[string]int{"p-1": 80},
		Workspaces: map[string]string{"wf-9-t-1": "/ws/p-1"},
		Budget:     BudgetNormal,
		Now:        1000,
	}
}

// TestTwoReplicasSchedulingTheSameGraphAgree is T-049.
//
// The shuffle IS the test. Two replicas read the same rows out of the same
// database and are not promised the same order across buckets, so a scheduler
// that inherited slice order would pass a naive version of this -- where both
// calls get the same slice -- and diverge in production, which is the one place
// nobody is watching for it.
func TestTwoReplicasSchedulingTheSameGraphAgree(t *testing.T) {
	in := SchedulerInput{
		Tasks: []TaskNode{
			{ID: "wf-1-t-1", WorkflowID: "wf-1", DeskID: "p-1", Role: "planner", Status: StatusDone, Ordinal: 0},
			{ID: "wf-1-t-2", WorkflowID: "wf-1", DeskID: "p-1", Role: "architect", Status: StatusDone, Ordinal: 1, Deps: []string{"wf-1-t-1"}},
			{ID: "wf-1-t-3", WorkflowID: "wf-1", DeskID: "p-1", Role: "backend", Status: StatusPending, Ordinal: 2, Deps: []string{"wf-1-t-2"}},
			{ID: "wf-1-t-4", WorkflowID: "wf-1", DeskID: "p-1", Role: "backend", Status: StatusPending, Ordinal: 3, Deps: []string{"wf-1-t-2"}},
			{ID: "wf-1-t-5", WorkflowID: "wf-1", DeskID: "p-1", Role: "qa", Status: StatusPending, Ordinal: 4, Deps: []string{"wf-1-t-3", "wf-1-t-4"}},
			{ID: "wf-2-t-1", WorkflowID: "wf-2", DeskID: "p-2", Role: "backend", Status: StatusPending, Ordinal: 0},
		},
		Agents: []AgentSlot{
			{ID: "a-1", Role: "backend"}, {ID: "a-2", Role: "backend"},
			{ID: "a-3", Role: "qa"}, {ID: "a-4", Role: "planner"},
		},
		Leases:     []LeaseState{{Workspace: "/ws/p-2", TaskID: "other", Until: 5000}},
		Readiness:  map[string]int{"p-1": 80, "p-2": 70},
		Workspaces: map[string]string{"wf-1-t-3": "/ws/p-1a", "wf-1-t-4": "/ws/p-1b", "wf-2-t-1": "/ws/p-2"},
		Budget:     BudgetNormal,
		Now:        1000,
	}

	// The second replica gets the same facts in a different order, which is the
	// only difference between them.
	rng := rand.New(rand.NewPCG(42, 1701))
	other := in
	other.Tasks = append([]TaskNode(nil), in.Tasks...)
	other.Agents = append([]AgentSlot(nil), in.Agents...)
	other.Leases = append([]LeaseState(nil), in.Leases...)
	rng.Shuffle(len(other.Tasks), func(i, j int) { other.Tasks[i], other.Tasks[j] = other.Tasks[j], other.Tasks[i] })
	rng.Shuffle(len(other.Agents), func(i, j int) { other.Agents[i], other.Agents[j] = other.Agents[j], other.Agents[i] })

	a := Schedule(in)
	b := Schedule(other)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("two replicas over the same graph produced different decisions (%+v vs %+v); "+
			"D-1 makes the scheduler a function of the graph, and a scheduler that depends on "+
			"row order is a function of the database's bucket layout", a, b)
	}
	if len(a.Ready) == 0 {
		t.Fatal("the fixture scheduled nothing, so agreeing about it proves nothing")
	}

	// And calling it twice on untouched input is the plain purity half, the
	// same shape TestTheEgressPassIsPure uses on the other pure function.
	if !reflect.DeepEqual(Schedule(in), a) {
		t.Error("two identical calls disagreed; a decision that varies between calls becomes " +
			"an event, and a replay would disagree with the run it replays")
	}
}

// TestTheSchedulerIsAPureFunctionOfTheGraph enforces D-1 structurally, because
// the property cannot be tested by calling the function: a clock read that
// happened to return the same value twice would pass every behavioural test
// there is.
//
// Same technique as TestNothingAboveTheAdapterBranchesOnTheInvocationMode and
// TestNothingOutsideTheEgressPassConstructsACleared, inverted: instead of
// sweeping the package for a forbidden pattern, it reads one file and requires
// it to stay small.
func TestTheSchedulerIsAPureFunctionOfTheGraph(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "scheduler.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse scheduler.go: %v", err)
	}

	allowed := map[string]bool{`"fmt"`: true, `"sort"`: true, `"strings"`: true}
	for _, imp := range f.Imports {
		if !allowed[imp.Path.Value] {
			t.Errorf("scheduler.go imports %s; a scheduler that can reach the clock is one edit "+
				"from reading it, and D-1 says the answer must be a function of the graph alone",
				imp.Path.Value)
		}
	}

	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Recv == nil {
			continue
		}
		t.Errorf("scheduler.go declares a method (%s); a method on the store has the lock and "+
			"the rng in scope by definition, and the file's whole claim is that it has neither",
			fn.Name.Name)
	}

	// The belt to the braces above. Methods are already refused and the import
	// list is already closed, so this only catches one remaining shape: a plain
	// function in this file taking a *MemStore and reaching through it.
	//
	// "Now" is deliberately NOT on this list. SchedulerInput.Now is how the
	// clock is supposed to arrive -- read once by the shell and passed as a
	// value -- and time.Now() is unreachable anyway, because "time" cannot be
	// imported. Forbidding the field name would be forbidding the fix.
	forbidden := map[string]bool{"rng": true, "IntN": true, "Float64": true, "mu": true}
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if forbidden[sel.Sel.Name] {
			t.Errorf("scheduler.go reads .%s; SchedulerInput is the only input the decision may "+
				"have, and every field on it is a value the caller already resolved", sel.Sel.Name)
		}
		return true
	})
}

// TestTheSchedulerNamesTheFiveBlockingReasonsAndNoOthers is BRD §7.4 held to its
// number. The requirement is explicit that all five are surfaced, "because an
// unexplained idle floor is indistinguishable from a broken product", so the
// count is asserted rather than assumed and T-109 cannot drift from it.
func TestTheSchedulerNamesTheFiveBlockingReasonsAndNoOthers(t *testing.T) {
	if len(blockingReasons) != 5 {
		t.Fatalf("the scheduler declares %d blocking reasons, BRD §7.4 names five; a sixth is a "+
			"reason the UI has no row for, and a fifth missing is a floor that looks idle for "+
			"a reason nobody can explain", len(blockingReasons))
	}
	seen := map[string]bool{}
	for _, r := range blockingReasons {
		if seen[r] {
			t.Errorf("%q is declared twice in the precedence list", r)
		}
		seen[r] = true
	}
	for _, want := range []string{
		BlockedByReadiness, BlockedByDependency, BlockedByCapability,
		BlockedByBudget, BlockedByLease,
	} {
		if !seen[want] {
			t.Errorf("%q is not in the precedence list, so a task blocked by it would be "+
				"reported as ready", want)
		}
	}

	// Each reason, produced. A constant nothing can emit is a row the UI will
	// render blank forever -- and budget is the one at real risk, because its
	// guard is M10.3 and does not exist.
	cases := []struct {
		name, want string
		bend       func(*SchedulerInput)
	}{
		{"below the planning floor", BlockedByReadiness, func(in *SchedulerInput) {
			in.Readiness["p-1"] = PlanFloor - 1
		}},
		{"an unfinished prerequisite", BlockedByDependency, func(in *SchedulerInput) {
			in.Tasks = append(in.Tasks, TaskNode{ID: "wf-9-t-0", WorkflowID: "wf-9",
				DeskID: "p-1", Role: "planner", Status: StatusRunning, Ordinal: -1})
			in.Tasks[0].Deps = []string{"wf-9-t-0"}
		}},
		{"no free agent for the role", BlockedByCapability, func(in *SchedulerInput) {
			in.Agents = []AgentSlot{{ID: "a-1", Role: "backend", Busy: true}}
		}},
		{"a throttled budget", BlockedByBudget, func(in *SchedulerInput) {
			in.Budget = BudgetThrottled
		}},
		{"a workspace somebody else holds", BlockedByLease, func(in *SchedulerInput) {
			in.Leases = []LeaseState{{Workspace: "/ws/p-1", TaskID: "wf-8-t-2", Until: in.Now + 300}}
		}},
	}
	for _, c := range cases {
		in := oneReadyGraph()
		c.bend(&in)
		got := Schedule(in)
		blocked := blockedFor(got, "wf-9-t-1")
		if blocked == nil {
			t.Errorf("%s did not block the task; %s is a reason the scheduler declares and "+
				"nothing produces", c.name, c.want)
			continue
		}
		if blocked.Reason != c.want {
			t.Errorf("%s reported %q, want %q", c.name, blocked.Reason, c.want)
		}
		if strings.TrimSpace(blocked.Detail) == "" {
			t.Errorf("%s blocked with no detail; a reason without a specific is a dead end (B-3) "+
				"and leaves the operator nothing to act on", c.name)
		}
	}
}

// TestATaskBlockedForFiveReasonsReportsThePrecedenceOne is the rule Screen
// already states for the egress pass, applied here: the sentence an operator
// reads must be a function of the task rather than of the order the checks
// happened to run in.
func TestATaskBlockedForFiveReasonsReportsThePrecedenceOne(t *testing.T) {
	in := oneReadyGraph()
	in.Readiness["p-1"] = PlanFloor - 10
	in.Tasks[0].Deps = []string{"wf-9-t-missing"}
	in.Agents = []AgentSlot{{ID: "a-1", Role: "backend", Busy: true}}
	in.Budget = BudgetCapped
	in.Leases = []LeaseState{{Workspace: "/ws/p-1", TaskID: "wf-8-t-2", Until: in.Now + 300}}

	got := Schedule(in)
	if len(got.Ready) != 0 {
		t.Fatal("a task failing all five conditions was dispatched")
	}
	blocked := blockedFor(got, "wf-9-t-1")
	if blocked == nil {
		t.Fatal("a task failing all five conditions was reported as neither ready nor blocked")
	}
	if blocked.Reason != BlockedByReadiness {
		t.Errorf("a task failing all five reported %q; readiness comes first because below the "+
			"planning floor nothing else is worth saying, and the reported reason must not "+
			"depend on which check ran first", blocked.Reason)
	}
	if n := len(got.Blocked); n != 1 {
		t.Errorf("one task produced %d blocked entries; a task has one reason at a time or the "+
			"floor shows the same desk five times", n)
	}
}

// TestTheSchedulerRefusesACycleAndNamesTheTasksInIt is the negative pair.
// CreateWorkflow refuses a cycle at planning time, so one reaching the
// scheduler means the graph was written by something else or edited since --
// and the scheduler must say so rather than walking an unsatisfiable set on
// every pass forever.
func TestTheSchedulerRefusesACycleAndNamesTheTasksInIt(t *testing.T) {
	in := SchedulerInput{
		Tasks: []TaskNode{
			{ID: "t-1", WorkflowID: "wf-1", DeskID: "p-1", Role: "backend", Status: StatusPending, Ordinal: 0, Deps: []string{"t-3"}},
			{ID: "t-2", WorkflowID: "wf-1", DeskID: "p-1", Role: "backend", Status: StatusPending, Ordinal: 1, Deps: []string{"t-1"}},
			{ID: "t-3", WorkflowID: "wf-1", DeskID: "p-1", Role: "backend", Status: StatusPending, Ordinal: 2, Deps: []string{"t-2"}},
		},
		Agents:    []AgentSlot{{ID: "a-1", Role: "backend"}, {ID: "a-2", Role: "backend"}, {ID: "a-3", Role: "backend"}},
		Readiness: map[string]int{"p-1": 80},
		Budget:    BudgetNormal,
		Now:       1000,
	}
	got := Schedule(in)
	if len(got.Ready) != 0 {
		t.Fatalf("%d task(s) in a three-way cycle were dispatched; none of them can ever have "+
			"its prerequisite satisfied", len(got.Ready))
	}
	if len(got.Blocked) != 3 {
		t.Fatalf("%d of 3 cycle members were reported blocked; a member the floor does not "+
			"mention is a desk sitting idle for no stated reason", len(got.Blocked))
	}
	for _, b := range got.Blocked {
		if b.Reason != BlockedByDependency {
			t.Errorf("%s in a cycle reported %q; the BRD makes acyclicity part of the dependency "+
				"condition rather than a sixth reason", b.TaskID, b.Reason)
		}
		for _, member := range []string{"t-1", "t-2", "t-3"} {
			if !strings.Contains(b.Detail, member) {
				t.Errorf("%s's detail is %q and does not name %s; an operator cannot break a "+
					"cycle they have not been shown", b.TaskID, b.Detail, member)
			}
		}
	}
}

// TestNothingIsDispatchedBelowReadinessFortyFive is BR-07 at the scheduler,
// paired with the planner-side refusal readiness_test.go already asserts.
//
// Both are needed and they are not the same check. The planner refuses to
// produce a plan below the floor; this refuses to dispatch a plan that was
// produced above it and whose record has decayed since -- which is the only way
// a task reaches here under a failing gate, and exactly the case a check at
// planning time cannot see.
func TestNothingIsDispatchedBelowReadinessFortyFive(t *testing.T) {
	in := oneReadyGraph()
	in.Readiness["p-1"] = PlanFloor
	if len(Schedule(in).Ready) != 1 {
		t.Fatalf("a task exactly at the floor of %d was not dispatched; BR-07 says BELOW the "+
			"floor, and an off-by-one here refuses work that is allowed to run", PlanFloor)
	}

	in.Readiness["p-1"] = PlanFloor - 1
	got := Schedule(in)
	if len(got.Ready) != 0 {
		t.Fatal("a task on a record below the planning floor was dispatched; BR-07 ends " +
			"\"Nothing is dispatched\", and a model called against a record this thin is " +
			"money spent on a brief nobody finished")
	}
	if b := blockedFor(got, "wf-9-t-1"); b == nil || b.Reason != BlockedByReadiness {
		t.Errorf("the refusal is %+v, want a readiness block", b)
	}
}

// TestTwoReadyTasksAreNeverHandedTheSameAgentOrWorkspace is the within-one-pass
// half of D-3. Two tasks in one decision that were given the same seat would be
// a conflict the scheduler created and did not record, and the runner would
// discover it only when the second lease failed.
func TestTwoReadyTasksAreNeverHandedTheSameAgentOrWorkspace(t *testing.T) {
	in := SchedulerInput{
		Tasks: []TaskNode{
			{ID: "t-a", WorkflowID: "wf-1", DeskID: "p-1", Role: "backend", Status: StatusPending, Ordinal: 0},
			{ID: "t-b", WorkflowID: "wf-1", DeskID: "p-1", Role: "backend", Status: StatusPending, Ordinal: 1},
		},
		Agents:     []AgentSlot{{ID: "a-1", Role: "backend"}, {ID: "a-2", Role: "backend"}},
		Readiness:  map[string]int{"p-1": 80},
		Workspaces: map[string]string{"t-a": "/ws/p-1", "t-b": "/ws/p-1"},
		Budget:     BudgetNormal,
		Now:        1000,
	}
	got := Schedule(in)
	if len(got.Ready) != 1 {
		t.Fatalf("%d tasks were dispatched into one workspace; D-3 exists because \"two runs in "+
			"one workspace corrupt each other silently\"", len(got.Ready))
	}
	if b := blockedFor(got, "t-b"); b == nil || b.Reason != BlockedByLease {
		t.Errorf("the second task reported %+v, want a lease block naming the workspace", b)
	}

	// Distinct products, and now both may run -- on distinct agents. (Distinct
	// workspaces of ONE product used to be enough; BR-38 made the product the
	// unit, since one product has one workspace through the gateway.)
	in.Tasks[1].DeskID = "p-2"
	in.Readiness["p-2"] = 80
	in.Workspaces["t-b"] = "/ws/p-2"
	got = Schedule(in)
	if len(got.Ready) != 2 {
		t.Fatalf("%d of 2 independent tasks were dispatched", len(got.Ready))
	}
	if got.Ready[0].AgentID == got.Ready[1].AgentID {
		t.Errorf("both tasks were seated on %s; one agent cannot carry two runs, and the "+
			"conflict would surface as a lease failure rather than as a decision",
			got.Ready[0].AgentID)
	}
}

// TestAgentSeatingIsDeterministic covers the rng that advance() used to reach
// for. It is here rather than in main_test.go because the property belongs to
// D-1: a reseat drawn from a private rng makes two replicas disagree about who
// is busy, and every capability decision downstream inherits it.
func TestAgentSeatingIsDeterministic(t *testing.T) {
	seat := func(seed uint64) []string {
		s := NewMemStore(seed)
		s.mu.Lock()
		defer s.mu.Unlock()
		var out []string
		for i := range s.desks {
			d := &s.desks[i]
			if d.State != StateActive {
				continue
			}
			s.advance(d)
			out = append(out, d.ID+":"+strings.Join(d.Agents, ","))
		}
		return out
	}
	a, b := seat(1), seat(999)
	if !reflect.DeepEqual(a, b) {
		t.Errorf("two stores with different seeds seated different agents (%v vs %v); "+
			"fixtures.go says a dispatch that seated a different agent on every replay would "+
			"make the fold disagree with the run, and D-1 extends that to two replicas", a, b)
	}
}

func blockedFor(d ScheduleDecision, id string) *BlockedTask {
	for i := range d.Blocked {
		if d.Blocked[i].TaskID == id {
			return &d.Blocked[i]
		}
	}
	return nil
}
