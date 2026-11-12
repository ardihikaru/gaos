package main

import (
	"fmt"
	"sort"
	"strings"
)

// The task graph. T-042 -- planned tasks get real dependencies, and a plan that
// loops is refused rather than written.
//
// Direction is not a free choice here. schema/08-edges.sql declares it:
// "DEPENDS_ON points from the dependent task to its prerequisite: Task
// -DEPENDS_ON-> Task means the OUT side waits for the IN side. Every readiness
// query in §12 assumes that direction." So PlanTask.Deps holds prerequisites,
// and a task is eligible exactly when every id in Deps has finished.
//
// Nothing in this file reads a clock, a lock or the store. It is the same
// discipline the scheduler is held to, for the same reason: a topological order
// that varied between two processes reading the same rows would make D-1
// unachievable one layer up.

// DepText renders the prerequisites for the plan preview. html/template cannot
// join a slice, and render.go has no FuncMap worth growing for one join.
func (p PlanTask) DepText() string { return strings.Join(p.Deps, ", ") }

// CycleError is a plan whose dependencies loop. It satisfies ErrRejected, so
// every handler that already turns a rejection into a toast keeps working
// unchanged -- and it carries the members, because "the plan is invalid" sends
// the operator back to a wizard with no idea which row to fix. Same argument
// NotReadyError makes about the missing-signals list.
type CycleError struct{ Members []string }

func (e *CycleError) Error() string {
	return fmt.Sprintf("the plan has a dependency cycle through %s — a task graph that loops has no first task",
		strings.Join(e.Members, ", "))
}
func (e *CycleError) Is(target error) bool { return target == ErrRejected }

// topoOrder returns the plan's task ids in an order where every prerequisite
// precedes the task that waits on it, or a CycleError naming the tasks that
// could not be placed.
//
// Kahn's algorithm, with one deliberate constraint: ready tasks are taken in
// the order they appear in the plan rather than out of a map. Two callers
// walking the same plan must produce the same order -- a topological sort is
// not unique, and one that varied with map iteration would be a fresh source of
// disagreement for every replica that consumed it.
//
// A dangling prerequisite is NOT this function's error to raise; it counts as
// an unsatisfiable edge and lands as a cycle here, which would be a confusing
// sentence to show an operator. validatePlan checks for that first, so by the
// time this runs every id in every Deps resolves.
func topoOrder(plan []PlanTask) ([]string, error) {
	ids := make([]string, len(plan))
	deps := make(map[string][]string, len(plan))
	for i, t := range plan {
		ids[i] = t.ID
		deps[t.ID] = t.Deps
	}
	order, ce := topoIDs(ids, deps)
	// The nil check is not ceremony. Returning a typed nil pointer through an
	// error interface produces a non-nil error, and every caller of this
	// function tests err != nil.
	if ce != nil {
		return nil, ce
	}
	return order, nil
}

// topoIDs is the sort itself, over ids and their prerequisites.
//
// Split out from topoOrder so the scheduler can reach it without owning a
// []PlanTask -- it works from TaskRecords, and converting one to the other just
// to sort it would be a copy whose only purpose was to satisfy a signature.
//
// ids is a slice and not a map's keys, and that is the determinism. The queue
// is seeded in ids order and grows in ids order; deps is only ever indexed,
// never ranged over. A Kahn that seeded from a map would return a different
// valid order per process, and D-1 asks for the same answer, not an equally
// correct one.
func topoIDs(ids []string, deps map[string][]string) ([]string, *CycleError) {
	indeg := make(map[string]int, len(ids))
	dependents := make(map[string][]string, len(ids))
	for _, id := range ids {
		indeg[id] += 0
		for _, dep := range deps[id] {
			indeg[id]++
			dependents[dep] = append(dependents[dep], id)
		}
	}

	var queue []string
	for _, id := range ids {
		if indeg[id] == 0 {
			queue = append(queue, id)
		}
	}

	out := make([]string, 0, len(ids))
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		out = append(out, id)
		for _, next := range dependents[id] {
			indeg[next]--
			if indeg[next] == 0 {
				queue = append(queue, next)
			}
		}
	}

	if len(out) != len(ids) {
		placed := make(map[string]bool, len(out))
		for _, id := range out {
			placed[id] = true
		}
		var stuck []string
		for _, id := range ids {
			if !placed[id] {
				stuck = append(stuck, id)
			}
		}
		sort.Strings(stuck)
		return nil, &CycleError{Members: stuck}
	}
	return out, nil
}

// validatePlan is the gate a plan passes before a single Task vertex is
// written. Two checks, in this order.
//
// It lives here rather than inside PlanWorkflow because a check on the
// planner's own output proves nothing: the planner cannot build a cycle. What
// can is a caller. CreateWorkflow takes `plan []PlanTask` as an argument, and
// while the wizard hands back what the planner produced, the Store method is
// public and an API caller may hand it anything at all. This is the boundary
// that makes T-042's "reject a cycle" true for every caller rather than for the
// one that happens to behave.
func validatePlan(plan []PlanTask) error {
	seen := make(map[string]bool, len(plan))
	for _, t := range plan {
		if strings.TrimSpace(t.ID) == "" {
			return reject("A task in the plan has no id.")
		}
		if seen[t.ID] {
			return reject(fmt.Sprintf("The plan names %s twice; a task id identifies one task.", t.ID))
		}
		seen[t.ID] = true
	}
	for _, t := range plan {
		for _, dep := range t.Deps {
			if !seen[dep] {
				return reject(fmt.Sprintf(
					"%s depends on %s, which is not in this plan.", t.ID, dep))
			}
			if dep == t.ID {
				return &CycleError{Members: []string{t.ID}}
			}
		}
	}
	// The cycle check first: a plan that loops is a different refusal from a
	// plan that does not wire the gate, and the operator needs the one that
	// names the row.
	if _, err := topoOrder(plan); err != nil {
		return err
	}
	return validateTheGate(plan)
}

// validateTheGate is BR-46's enforcement point (M17, D-069).
//
// A plan that omits the gate is a plan whose implementation task would be
// built with nobody reviewing the design, so it is refused before the first
// write -- here, and not in PlanWorkflow, because a check on the planner's own
// output proves nothing while CreateWorkflow is public and any caller may hand
// it a graph.
//
// Two shapes are refused: an implementation task with no review prerequisite,
// and a review task with no design prerequisite.
func validateTheGate(plan []PlanTask) error {
	kinds := make(map[string]string, len(plan))
	for _, t := range plan {
		// A kind outside the vocabulary is refused here, before the first
		// write, rather than normalised into an implement task further down.
		// The fail-open direction is contamination: a task the gate cannot
		// see is a task that gets the author's brief and no review.
		if !knownTaskKind(t.Kind) {
			return reject(fmt.Sprintf(
				"%s has the kind %q, which is not one of implement|design|review.", t.ID, t.Kind))
		}
		kinds[t.ID] = taskKind(t.Kind)
	}
	// A plan with no gate in it at all is refused, whatever it contains.
	var designs, reviews int
	for _, t := range plan {
		switch taskKind(t.Kind) {
		case TaskKindDesign:
			designs++
		case TaskKindReview:
			reviews++
		}
	}
	if designs == 0 || reviews == 0 {
		return reject("the plan wires no design gate: it needs at least one design task and one " +
			"review task (BR-46).")
	}
	hasDep := func(id, want string) bool {
		for _, t := range plan {
			if t.ID != id {
				continue
			}
			for _, dep := range t.Deps {
				if kinds[dep] == want {
					return true
				}
			}
		}
		return false
	}
	// Every review needs something to attack.
	for _, t := range plan {
		if taskKind(t.Kind) != TaskKindReview {
			continue
		}
		if !hasDep(t.ID, TaskKindDesign) {
			return reject(fmt.Sprintf(
				"%s is a review with no design to read; a review needs something to attack (BR-47).", t.ID))
		}
	}

	// Every STORY's implementation is gated: at least one implementation task
	// carrying that story waits directly on a review.
	//
	// "At least one" and not "all" because a story may carry several
	// implementation tasks -- the one that builds it and the one that verifies
	// it -- and only the first is the implementation the rule is about. The
	// verifier waits on the implementation, which waits on the review, so it
	// is gated too, one hop further out.
	byStory := map[string]bool{}
	for _, t := range plan {
		if taskKind(t.Kind) == TaskKindImplement && t.Story != "" {
			byStory[t.Story] = true
		}
	}
	gated := map[string]bool{}
	for _, t := range plan {
		if taskKind(t.Kind) != TaskKindImplement {
			continue
		}
		for _, dep := range t.Deps {
			if kinds[dep] == TaskKindReview {
				gated[t.Story] = true
			}
		}
	}
	if len(byStory) == 0 {
		// A plan with no story at all still has to wire the gate somewhere.
		any := false
		for _, t := range plan {
			if taskKind(t.Kind) == TaskKindImplement {
				for _, dep := range t.Deps {
					if kinds[dep] == TaskKindReview {
						any = true
					}
				}
			}
		}
		if !any {
			return reject("the plan has no implementation waiting on a review; every " +
				"implementation is reviewed before it runs (BR-46).")
		}
		return nil
	}
	for story := range byStory {
		if !gated[story] {
			return reject(fmt.Sprintf(
				"the implementation of %s does not wait on a review; every story is "+
					"designed, reviewed and then built (BR-46).", story))
		}
	}
	return nil
}

// qualify rewrites a pure plan's local ids into graph ids, prerequisites
// included.
//
// PlanWorkflow numbers from t-1 every time, because it is pure and does not
// know which workflow it is planning for -- and the preview reads better for
// it. That is fine right up until two plans reach the graph, where
// schema/09-indexes.sql declares Task (tenantId, id) UNIQUE: the second t-1
// would upsert over the first and take its DEPENDS_ON edges with it. So the
// workflow id is prefixed here, on the way to storage, and the Deps are
// rewritten through the same map so no edge is left pointing at a name that no
// longer exists.
func qualify(workflowID string, plan []PlanTask) []PlanTask {
	names := make(map[string]string, len(plan))
	for _, t := range plan {
		names[t.ID] = workflowID + "-" + t.ID
	}
	out := make([]PlanTask, len(plan))
	for i, t := range plan {
		out[i] = t
		out[i].ID = names[t.ID]
		out[i].Deps = nil
		for _, dep := range t.Deps {
			out[i].Deps = append(out[i].Deps, names[dep])
		}
	}
	return out
}
