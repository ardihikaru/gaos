package main

import (
	"strings"
	"testing"
	"time"
)

// v0.39.1: a freshly created workflow must be able to start, and the task it
// starts must be briefed as itself.

// TestAFreshWorkflowsFirstPlannerTaskIsSchedulable is the regression for the
// planner seat. CreateWorkflow seats a-9, the only planner, on the desk it
// activates, and the shell used to count that seat as busy against the desk's
// own planner task -- so the first task of every new workflow was blocked as
// "every planner agent is busy" and everything behind it waited forever.
// deploy/acceptance/live-aim-run.sh cancelled the desk to get past it.
func TestAFreshWorkflowsFirstPlannerTaskIsSchedulable(t *testing.T) {
	_, s, desk := plannedProduct(t)

	// Every OTHER desk idle, so nothing but the new desk's own seat can hold
	// the planner. The new desk stays exactly as CreateWorkflow left it.
	s.mu.Lock()
	var first string
	for i := range s.desks {
		if s.desks[i].ID != desk {
			s.desks[i].State = StateIdle
			continue
		}
		if s.desks[i].State != StateActive || len(s.desks[i].Agents) == 0 {
			s.mu.Unlock()
			t.Fatalf("CreateWorkflow left desk %s %s seating %v; the test assumes an active desk with the planner seated",
				desk, s.desks[i].State, s.desks[i].Agents)
		}
	}
	for _, tk := range s.tasks {
		if tk.DeskID == desk && tk.Ordinal == 0 {
			first = tk.ID
		}
	}
	s.mu.Unlock()

	dec := Schedule(s.schedulerInput(time.Now().Unix()))
	for _, r := range dec.Ready {
		if r.TaskID == first {
			if r.AgentID != "a-9" {
				t.Fatalf("%s was dispatched to %s, want the planner seated at its own desk, a-9", first, r.AgentID)
			}
			return
		}
	}
	for _, b := range dec.Blocked {
		if b.TaskID == first {
			t.Fatalf("the first task of a fresh workflow is blocked (%s: %s); the desk's own seated planner must be free for it",
				b.Reason, b.Detail)
		}
	}
	t.Fatalf("the first task of a fresh workflow (%s) was neither ready nor blocked", first)
}

// TestASeatedAgentIsStillBusyForAnotherDesk is the negative half: the seat
// frees the agent for its own desk only. A planner task on a second product
// must still be told the planner is busy.
func TestASeatedAgentIsStillBusyForAnotherDesk(t *testing.T) {
	in := SchedulerInput{
		Readiness: map[string]int{"p-1": 100, "p-2": 100},
		Budget:    BudgetNormal,
		Now:       1000,
		Tasks: []TaskNode{
			{ID: "wf-2-t-1", WorkflowID: "wf-2", DeskID: "p-2", Role: "planner", Status: StatusPending},
		},
		Agents: []AgentSlot{{ID: "a-9", Role: "planner", Busy: true, Desk: "p-1"}},
	}
	dec := Schedule(in)
	if len(dec.Ready) != 0 {
		t.Fatalf("a planner seated at p-1 was dispatched to p-2's task: %+v", dec.Ready)
	}
	b := blockedFor(dec, "wf-2-t-1")
	if b == nil || b.Reason != BlockedByCapability || b.Detail != "every planner agent is busy" {
		t.Fatalf("p-2's planner task reported %+v, want capability: every planner agent is busy", b)
	}

	// Busy with no desk named is busy everywhere, including p-1.
	in.Tasks[0].DeskID = "p-1"
	in.Agents[0].Desk = ""
	if dec := Schedule(in); len(dec.Ready) != 0 {
		t.Fatalf("an agent busy at no named desk was dispatched: %+v", dec.Ready)
	}
}

// TestTheTaskPacketBriefsTheDispatchedTask is the regression for task.md. It
// used to carry the desk's display line, which CreateWorkflow sets to the
// plan's first task, so an implementation task was briefed as the planner.
func TestTheTaskPacketBriefsTheDispatchedTask(t *testing.T) {
	_, s, desk := plannedProduct(t)

	s.mu.RLock()
	defer s.mu.RUnlock()
	var impl TaskRecord
	for _, tk := range s.tasks {
		if tk.DeskID == desk && tk.Role == "backend" && tk.Story != "" && strings.HasPrefix(tk.Title, "Implement ") {
			impl = tk
		}
	}
	if impl.ID == "" {
		t.Fatal("the planned workflow has no story implementation task")
	}
	var story Story
	for _, st := range s.masterPtr(desk).Stories {
		if st.ID == impl.Story {
			story = st
		}
	}
	d := s.desks[s.deskIdxByID(desk)]

	pkt := s.freezePacketFor(d, impl.ID, 1)
	var task string
	for _, p := range pkt.Parts {
		if p.Path == ".gaos/packet/task.md" {
			task = p.Body
		}
	}
	if task == "" {
		t.Fatal("the implementation task's packet has no task.md")
	}
	if strings.Contains(task, d.Task) {
		t.Errorf("task.md for %s carries the desk's line %q; it must brief the dispatched task:\n%s", impl.ID, d.Task, task)
	}
	want := []string{impl.Title, "Kind: " + TaskKindImplement, "Role: backend", "Story " + story.ID, story.Want}
	want = append(want, story.AC...)
	for _, w := range want {
		if !strings.Contains(task, w) {
			t.Errorf("task.md for %s does not say %q:\n%s", impl.ID, w, task)
		}
	}
	if pkt.Role != "backend" {
		t.Errorf("the packet for %s names role %q, want the task's own, backend", impl.ID, pkt.Role)
	}
	if again := s.freezePacketFor(d, impl.ID, 1); digestOf(again.Parts) != digestOf(pkt.Parts) {
		t.Error("two freezes of one attempt produced two digests; the brief must be a function of the record")
	}
}
