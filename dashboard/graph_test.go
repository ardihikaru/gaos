package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// T-042: the plan is a graph, it is persisted, and a plan that loops is refused
// before a vertex is written.

// TestThePlannerProducesADAGWithFanOutAndFanIn is the positive half, and it
// asserts the shape rather than the count.
//
// The count was already asserted before this change and passed against a chain
// -- eight tasks is eight tasks whether they are in a line or a graph. What
// makes T-042 true is that two stories become two independent branches and that
// the security review waits on both, so those are what this reads.
func TestThePlannerProducesADAGWithFanOutAndFanIn(t *testing.T) {
	s := NewMemStore(7)
	plan, err := s.PlanWorkflow(context.Background(), WorkflowDraft{
		ProductID: "p-1", Goal: "two stories at once", Stories: []string{"US-01", "US-02"},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Fan-out: every story's chain hangs off the architect, not off the
	// previous story's verifier. A chain would give the second implementer the
	// first story's qa task as its prerequisite, and the two stories could never
	// run at once.
	//
	// The shape is counted by KIND and not by role since M17, because the plan
	// now holds two architect tasks (the workflow-wide one and each story's
	// design) and two qa tasks (the review and the verify), and a role no
	// longer says what a task is for.
	arch := ""
	var impls, verifiers, designs, reviews []PlanTask
	for _, p := range plan {
		switch {
		case p.Kind == TaskKindDesign && p.Role == "architect" && arch == "":
			arch = p.ID
		case p.Kind == TaskKindReview:
			reviews = append(reviews, p)
		case p.Kind == TaskKindImplement && p.Role == "backend":
			impls = append(impls, p)
		case p.Kind == TaskKindImplement && p.Role == "qa":
			verifiers = append(verifiers, p)
		}
	}
	_ = designs
	if len(impls) != 2 || len(verifiers) != 2 || len(reviews) != 2 {
		t.Fatalf("two picked stories produced %d implement, %d verify and %d review tasks, "+
			"want 2 and 2 and 2; the chain per story is what the fan-out is made of "+
			"(M17)", len(impls), len(verifiers), len(reviews))
	}
	for _, p := range impls {
		// The implementation waits on the REVIEW, and the review waits on the
		// design, and the design waits on the architect. The gate is that first
		// edge (D-069 clause 7).
		if len(p.Deps) != 1 {
			t.Errorf("%s waits on %v; it must wait on its review alone", p.ID, p.Deps)
			continue
		}
		var rev PlanTask
		for _, r := range reviews {
			if r.ID == p.Deps[0] {
				rev = r
			}
		}
		if rev.ID == "" {
			t.Errorf("%s waits on %v, which is not a review; the gate is the edge to the "+
				"review and nothing else", p.ID, p.Deps)
			continue
		}
		if len(rev.Deps) != 1 {
			t.Errorf("the review %s waits on %v; it must wait on its design alone", rev.ID, rev.Deps)
		}
	}

	// Fan-in: security waits on every verifier there is. This is the assertion
	// that makes Deps a slice -- one prerequisite could not express it, and a
	// security review that ran after only one story had been verified would be
	// reviewing half the change.
	var sec PlanTask
	for _, p := range plan {
		if p.Role == "security" {
			sec = p
		}
	}
	wantDeps := []string{verifiers[0].ID, verifiers[1].ID}
	if !reflect.DeepEqual(sec.Deps, wantDeps) {
		t.Errorf("security waits on %v, want %v; the fan-in is per story, so its prerequisite "+
			"count is a property of the record rather than a number written in the planner",
			sec.Deps, wantDeps)
	}

	// And the whole thing still sorts, which is the property everything above
	// this depends on.
	order, err := topoOrder(plan)
	if err != nil {
		t.Fatalf("the planner's own output does not sort: %v", err)
	}
	if len(order) != len(plan) {
		t.Errorf("the topological order holds %d of %d tasks", len(order), len(plan))
	}
}

// TestEveryDependencyPointsAtATaskEarlierInTheTopologicalOrder is the invariant
// the ArcadeDB model §11 states for the planner, checked against the sort
// rather than against the slice.
//
// The old version of this checked slice order, which happened to be
// topological because the planner emitted a line. It is still topological, but
// asserting it that way would be asserting an accident of how the planner walks
// the stories.
func TestEveryDependencyPointsAtATaskEarlierInTheTopologicalOrder(t *testing.T) {
	s := NewMemStore(7)
	plan, err := s.PlanWorkflow(context.Background(), WorkflowDraft{
		ProductID: "p-1", Goal: "order", Stories: []string{"US-01", "US-02"},
	})
	if err != nil {
		t.Fatal(err)
	}
	order, err := topoOrder(plan)
	if err != nil {
		t.Fatal(err)
	}
	at := map[string]int{}
	for i, id := range order {
		at[id] = i
	}
	for _, p := range plan {
		for _, dep := range p.Deps {
			if at[dep] >= at[p.ID] {
				t.Errorf("%s is placed at %d and its prerequisite %s at %d; DEPENDS_ON points "+
					"from the dependent to the prerequisite, so the prerequisite runs first",
					p.ID, at[p.ID], dep, at[dep])
			}
		}
	}
}

// TestThePlannerRefusesAPlanWhoseDependenciesFormACycle is the negative half of
// T-042, and it goes through CreateWorkflow rather than through topoOrder.
//
// That is the point of the test. The planner cannot build a cycle, so proving
// the sort detects one proves nothing about the product; what can hand a cycle
// to the graph is a caller, and CreateWorkflow takes the plan as an argument
// across a public seam.
func TestThePlannerRefusesAPlanWhoseDependenciesFormACycle(t *testing.T) {
	s := NewMemStore(7)
	ctx := context.Background()
	before := len(s.tasks)

	cycle := []PlanTask{
		{ID: "t-1", Title: "a", Role: "planner", Deps: []string{"t-3"}},
		{ID: "t-2", Title: "b", Role: "backend", Deps: []string{"t-1"}},
		{ID: "t-3", Title: "c", Role: "qa", Deps: []string{"t-2"}},
	}
	_, err := s.CreateWorkflow(ctx, WorkflowDraft{ProductID: "p-1", Goal: "loop"}, cycle)
	if err == nil {
		t.Fatal("a plan that loops was accepted; a task graph with no first task can never " +
			"be scheduled, and the scheduler would spin over it forever")
	}
	if !errors.Is(err, ErrRejected) {
		t.Errorf("the refusal is %v, which is not an ErrRejected; every handler already turns "+
			"a rejection into a toast and this one would become a 500", err)
	}
	var ce *CycleError
	if !errors.As(err, &ce) {
		t.Fatalf("the refusal is %T and does not carry the cycle; sending an operator back to "+
			"the wizard without naming a row is a dead end (B-3)", err)
	}
	if len(ce.Members) != 3 {
		t.Errorf("the cycle names %v, want all three tasks in it", ce.Members)
	}
	if len(s.tasks) != before {
		t.Errorf("a refused plan wrote %d task(s); every check in CreateWorkflow runs before "+
			"the first write precisely so a rejection leaves nothing behind", len(s.tasks)-before)
	}
}

// TestADependencyOnATaskOutsideThePlanIsRejectedAndNamesIt is the other way a
// caller-supplied plan is unusable. It is separated from the cycle case because
// the operator's next action is different -- a dangling prerequisite is a typo
// in one row, a cycle is a shape -- and a message that conflated them would
// send them to the wrong place.
func TestADependencyOnATaskOutsideThePlanIsRejectedAndNamesIt(t *testing.T) {
	s := NewMemStore(7)
	dangling := []PlanTask{
		{ID: "t-1", Title: "a", Role: "planner"},
		{ID: "t-2", Title: "b", Role: "backend", Deps: []string{"t-99"}},
	}
	_, err := s.CreateWorkflow(context.Background(),
		WorkflowDraft{ProductID: "p-1", Goal: "dangling"}, dangling)
	if err == nil {
		t.Fatal("a task depending on something outside the plan was accepted; the DEPENDS_ON " +
			"edge would point at a vertex that does not exist and the task would never be ready")
	}
	if !strings.Contains(err.Error(), "t-99") {
		t.Errorf("the refusal reads %q and does not name the missing prerequisite", err)
	}
}

// TestTaskIdsAreUniqueAcrossTwoWorkflowsOnTheSameDesk is the id-collision bug,
// pinned.
//
// PlanWorkflow numbers from t-1 every time, which is right -- it is pure and
// does not know the workflow it is planning for. schema/09-indexes.sql declares
// Task (tenantId, id) UNIQUE, so two plans reaching the graph unqualified means
// the second t-1 upserts over the first and takes its DEPENDS_ON edges with it.
// The workflow counter had the same shape of bug: len(desks)+1 never changes,
// so every workflow ever created was called wf-7.
func TestTaskIdsAreUniqueAcrossTwoWorkflowsOnTheSameDesk(t *testing.T) {
	s := NewMemStore(7)
	ctx := context.Background()
	draft := WorkflowDraft{ProductID: "p-1", Goal: "first", Stories: []string{"US-01"}}

	plan, err := s.PlanWorkflow(ctx, draft)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateWorkflow(ctx, draft, plan); err != nil {
		t.Fatal(err)
	}
	first := s.tasksSnapshot("p-1")

	draft.Goal = "second"
	plan, err = s.PlanWorkflow(ctx, draft)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateWorkflow(ctx, draft, plan); err != nil {
		t.Fatal(err)
	}
	both := s.tasksSnapshot("p-1")

	if len(both) != 2*len(first) {
		t.Fatalf("two workflows left %d tasks on the desk, want %d; the second plan overwrote "+
			"the first rather than joining it", len(both), 2*len(first))
	}
	seen := map[string]bool{}
	for _, task := range both {
		if seen[task.ID] {
			t.Errorf("%s appears twice; Task (tenantId, id) is UNIQUE, so the second write "+
				"would upsert over the first and inherit its DEPENDS_ON edges", task.ID)
		}
		seen[task.ID] = true
	}
	wfs := map[string]bool{}
	for _, task := range both {
		wfs[task.WorkflowID] = true
	}
	if len(wfs) != 2 {
		t.Errorf("the two workflows are called %v; len(desks)+1 does not change between "+
			"creations, which is how every workflow ended up being wf-7", keysOf(wfs))
	}
	// Every prerequisite still resolves inside its own workflow after
	// qualification -- an edge rewritten to a name that no longer exists would
	// leave the second task permanently unschedulable.
	ids := map[string]bool{}
	for _, task := range both {
		ids[task.ID] = true
	}
	for _, task := range both {
		for _, dep := range task.Deps {
			if !ids[dep] {
				t.Errorf("%s depends on %s, which qualification left pointing at nothing",
					task.ID, dep)
			}
		}
	}
}

// TestTheGraphSurvivesAFoldBecauseItIsNotInTheFloor pins the decision the
// TaskPlanned case in fold.go argues for.
//
// The graph is authoritative persisted state, restored by ArcadeStore.load the
// way the master record is, and deliberately not folded. If somebody later
// moves it into FloorState it will look like a tidy-up -- the events are right
// there in the payload -- and this is the test that says no. Two claims: the
// folded floor is identical whether or not a plan exists, and the graph is
// still there afterwards.
func TestTheGraphSurvivesAFoldBecauseItIsNotInTheFloor(t *testing.T) {
	ctx := context.Background()

	plain := NewMemStore(7)
	planned := NewMemStore(7)
	draft := WorkflowDraft{ProductID: "p-1", Goal: "plan me", Stories: []string{"US-01"}}
	plan, err := planned.PlanWorkflow(ctx, draft)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := planned.CreateWorkflow(ctx, draft, plan); err != nil {
		t.Fatal(err)
	}
	// The same mutation minus the graph, so the two streams differ only in what
	// TaskPlanned carries rather than in what happened on the floor.
	if _, err := plain.CreateWorkflow(ctx, draft, plan); err != nil {
		t.Fatal(err)
	}

	a := fold(emptyFloor(), plain.rawEvents(), allEvents)
	b := fold(emptyFloor(), planned.rawEvents(), allEvents)
	if !reflect.DeepEqual(a, b) {
		t.Error("the folded floor differs between two runs whose only difference is the task " +
			"graph; the reducer is reading TaskPlanned's payload, and FloorState has just " +
			"grown a copy of the graph that every snapshot rung now carries")
	}

	if len(planned.tasksSnapshot("p-1")) == 0 {
		t.Fatal("the graph is empty after planning; not being folded is the reason it must " +
			"be persisted, and a graph that is neither is not state at all")
	}
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
