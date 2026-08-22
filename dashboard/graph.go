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
	indeg := make(map[string]int, len(plan))
	dependents := make(map[string][]string, len(plan))
	for _, t := range plan {
		indeg[t.ID] += 0
		for _, dep := range t.Deps {
			indeg[t.ID]++
			dependents[dep] = append(dependents[dep], t.ID)
		}
	}

	// The queue is seeded in plan order, and grows in plan order, which is what
	// makes the result reproducible.
	var queue []string
	for _, t := range plan {
		if indeg[t.ID] == 0 {
			queue = append(queue, t.ID)
		}
	}

	out := make([]string, 0, len(plan))
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

	if len(out) != len(plan) {
		placed := make(map[string]bool, len(out))
		for _, id := range out {
			placed[id] = true
		}
		var stuck []string
		for _, t := range plan {
			if !placed[t.ID] {
				stuck = append(stuck, t.ID)
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
	_, err := topoOrder(plan)
	return err
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
