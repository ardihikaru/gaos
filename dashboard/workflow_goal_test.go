package main

import (
	"context"
	"strings"
	"testing"
)

// v0.40.5: a second workflow on a product was briefed with the first one's goal.
//
// Found on gaos.sample-product.com: sample-product (p-6) had its seeded workflow, and a
// second one was created with a goal of two VSYS cases. Every one of the second
// workflow's packets said "## Objective" followed by the SEEDED goal, so the
// planner planned the whole product. Two halves: the Objective was read from
// the desk -- one per product, so a goal for the desk rather than for the task's
// own workflow -- and ArcadeStore.load seated one desk per Workflow vertex, so
// after a restart p-6 had two desks and deskIdxByID found the older one first.

// objectiveOf returns the text under "## Objective" in a packet's task.md.
func objectiveOf(t *testing.T, pkt Packet) string {
	t.Helper()
	for _, p := range pkt.Parts {
		if p.Path != ".gaos/packet/task.md" {
			continue
		}
		_, rest, ok := strings.Cut(p.Body, "## Objective\n")
		if !ok {
			t.Fatalf("task.md has no Objective:\n%s", p.Body)
		}
		obj, _, _ := strings.Cut(rest, "\n\n## ")
		return obj
	}
	t.Fatalf("the packet for %s has no task.md", pkt.TaskID)
	return ""
}

// twoWorkflows creates two workflows on one product of s and returns the id of
// the first task of each.
func twoWorkflows(t *testing.T, s Store, product, firstGoal, secondGoal string) (string, string) {
	t.Helper()
	ctx := context.Background()
	m, err := s.Master(ctx, product)
	if err != nil {
		t.Fatalf("master: %v", err)
	}
	if len(m.Stories) == 0 {
		t.Fatalf("%s has no stories to plan", product)
	}
	var ids []string
	for _, goal := range []string{firstGoal, secondGoal} {
		draft := WorkflowDraft{ProductID: product, Goal: goal, Stories: []string{m.Stories[0].ID}}
		plan, err := s.PlanWorkflow(ctx, draft)
		if err != nil {
			t.Fatalf("PlanWorkflow: %v", err)
		}
		if _, err := s.CreateWorkflow(ctx, draft, plan); err != nil {
			t.Fatalf("CreateWorkflow: %v", err)
		}
		ids = append(ids, "")
	}
	tasks := s.(graphReader).tasksSnapshot(product)
	wfs := []string{}
	for _, task := range tasks {
		if len(wfs) == 0 || wfs[len(wfs)-1] != task.WorkflowID {
			wfs = append(wfs, task.WorkflowID)
			ids[len(wfs)-1] = task.ID
		}
		if len(wfs) == 2 {
			break
		}
	}
	if len(wfs) != 2 {
		t.Fatalf("two workflows planned tasks on %d workflows: %v", len(wfs), wfs)
	}
	return ids[0], ids[1]
}

const (
	goalFirst  = "Automate the whole test catalog epic by epic"
	goalSecond = "Automate the two remaining read-only VSYS cases only"
)

// TestEachTaskIsBriefedWithItsOwnWorkflowsGoal is the in-memory half: with two
// workflows on one desk, a task of the first must still carry the first goal.
func TestEachTaskIsBriefedWithItsOwnWorkflowsGoal(t *testing.T) {
	s := NewMemStore(7)
	first, second := twoWorkflows(t, s, "p-1", goalFirst, goalSecond)

	s.mu.RLock()
	defer s.mu.RUnlock()
	d := s.desks[s.deskIdxByID("p-1")]
	for _, c := range []struct{ task, want string }{{first, goalFirst}, {second, goalSecond}} {
		pkt := s.freezePacketFor(d, c.task, 1)
		if got := objectiveOf(t, pkt); got != c.want {
			t.Errorf("the packet for %s says the objective is %q, want its own workflow's goal %q; "+
				"the desk holds the LATEST workflow's goal, not this task's", c.task, got, c.want)
		}
		if again := s.freezePacketFor(d, c.task, 1); digestOf(again.Parts) != digestOf(pkt.Parts) {
			t.Errorf("two freezes of %s produced two digests; the objective must be a function of the record", c.task)
		}
	}
}

// TestASecondWorkflowKeepsItsGoalAcrossARestart is the production case: two
// workflows on one product, a reopened ArcadeStore, and the second workflow's
// tasks must still be briefed with its own goal -- with one desk for the
// product, seated on the latest workflow.
func TestASecondWorkflowKeepsItsGoalAcrossARestart(t *testing.T) {
	arc := arcadeTestTarget(t)
	ctx := context.Background()
	tenant := "conf-workflow-goal"

	firstStore := newArcadeTestStore(t, arc, tenant)
	first, second := twoWorkflows(t, firstStore, "p-1", goalFirst, goalSecond)
	wantWF := firstStore.(graphReader).tasksSnapshot("p-1")
	var latest string
	for _, task := range wantWF {
		if task.ID == second {
			latest = task.WorkflowID
		}
	}

	reopened, err := NewArcadeStoreFor(ctx, arc, 1, tenant)
	if err != nil {
		t.Fatalf("reopen the store on the same tenant: %v", err)
	}
	ms := reopened.proj
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	seated := 0
	for _, d := range ms.desks {
		if d.ID == "p-1" {
			seated++
			if d.WorkflowID != latest {
				t.Errorf("p-1 is seated on %s after the reopen, want the latest workflow %s", d.WorkflowID, latest)
			}
		}
	}
	if seated != 1 {
		t.Fatalf("p-1 has %d desks after the reopen, want 1; load seated one desk per Workflow "+
			"vertex, and deskIdxByID then finds the oldest workflow's desk first", seated)
	}
	d := ms.desks[ms.deskIdxByID("p-1")]
	for _, c := range []struct{ task, want string }{{first, goalFirst}, {second, goalSecond}} {
		if got := objectiveOf(t, ms.freezePacketFor(d, c.task, 1)); got != c.want {
			t.Errorf("after the reopen the packet for %s says the objective is %q, want its own "+
				"workflow's goal %q", c.task, got, c.want)
		}
	}
}
