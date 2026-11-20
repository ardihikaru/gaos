package main

import (
	"context"
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

// v0.40.1: an agent is busy when it carries a live turn, not when a desk shows
// it seated.
//
// Found on prod under GAOS_RUNNER=manual: nothing ever left backlog. The seeded
// floor seats a-1, the only architect, at p-1 and p-3, and both stay active for
// ever because nothing runs them; CreateWorkflow seats a-9, the only planner,
// on every desk it activates, so two products with a workflow each seat it
// twice. The shell read a seat at two live desks as "busy everywhere", so every
// architect and planner task on the floor was blocked as "every X agent is
// busy" with no run anywhere to finish and free them. The v0.39.1 regression
// above did not see it because it idles every other desk first.

// plannedProducts creates a workflow on the first n plannable products and
// leaves the seeded floor exactly as NewMemStore built it.
func plannedProducts(t *testing.T, n int) (*MemStore, []string) {
	t.Helper()
	_, s := newTestServer(t)
	ctx := context.Background()
	masters, _ := s.Masters(ctx)
	var desks []string
	for _, m := range masters {
		if len(desks) == n {
			break
		}
		if m.ReadinessScore() < PlanFloor || len(m.Stories) == 0 {
			continue
		}
		draft := WorkflowDraft{ProductID: m.ID, Goal: "seats", Stories: []string{m.Stories[0].ID}}
		plan, err := s.PlanWorkflow(ctx, draft)
		if err != nil {
			continue
		}
		if _, err := s.CreateWorkflow(ctx, draft, plan); err != nil {
			continue
		}
		desks = append(desks, m.ID)
	}
	if len(desks) < n {
		t.Fatalf("only %d seeded product(s) could be planned, want %d", len(desks), n)
	}
	return s, desks
}

func firstTaskOf(s *MemStore, desk string) TaskRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, tk := range s.tasks {
		if tk.DeskID == desk && tk.Ordinal == 0 {
			return tk
		}
	}
	return TaskRecord{}
}

// TestASeatWithNoLiveTurnDoesNotHoldTheAgent is the regression: with the seeded
// floor untouched and two workflows each seating the planner, the scheduler
// must ready the first workflow's planner task and, once that card is parked
// in READY (manual mode drops it from the input), the second's.
func TestASeatWithNoLiveTurnDoesNotHoldTheAgent(t *testing.T) {
	s, desks := plannedProducts(t, 2)
	first, second := firstTaskOf(s, desks[0]), firstTaskOf(s, desks[1])
	if first.Role != "planner" || second.Role != "planner" {
		t.Fatalf("the plans start with %q and %q, want two planner tasks", first.Role, second.Role)
	}

	in := s.schedulerInput(time.Now().Unix())
	for _, a := range in.Agents {
		if a.Busy {
			t.Errorf("agent %s (%s) is busy with no task DISPATCHED or RUNNING anywhere; a seat on a desk is not a turn", a.ID, a.Role)
		}
	}
	dec := Schedule(in)
	if b := blockedFor(dec, first.ID); b != nil {
		t.Fatalf("%s is blocked (%s: %s) on a floor with no live turn; nothing would ever free it", first.ID, b.Reason, b.Detail)
	}
	readied := map[string]string{}
	for _, r := range dec.Ready {
		readied[r.TaskID] = r.AgentID
	}
	if readied[first.ID] != "a-9" {
		t.Fatalf("%s was not readied with the planner a-9; ready = %+v", first.ID, dec.Ready)
	}
	// One planner, one card per pass: the second waits for this pass only.
	if b := blockedFor(dec, second.ID); b == nil || b.Detail != "every planner agent is busy" {
		t.Fatalf("%s reported %+v in the same pass, want capability: every planner agent is busy", second.ID, b)
	}

	// The next manual pass: the first card is READY and left out of the input.
	setStatus(s, first.ID, StatusReady)
	in = s.schedulerInput(time.Now().Unix())
	in.Tasks = withoutReady(in.Tasks)
	dec = Schedule(in)
	ok := false
	for _, r := range dec.Ready {
		ok = ok || r.TaskID == second.ID
	}
	if !ok {
		t.Fatalf("%s was not readied once %s was parked in READY: %+v", second.ID, first.ID, dec)
	}
}

// TestALiveTurnHoldsItsAgent is the negative half: a task that is RUNNING
// holds one agent of its role, so the same role on another product waits.
func TestALiveTurnHoldsItsAgent(t *testing.T) {
	s, desks := plannedProducts(t, 2)
	first, second := firstTaskOf(s, desks[0]), firstTaskOf(s, desks[1])
	setStatus(s, first.ID, StatusRunning)

	in := s.schedulerInput(time.Now().Unix())
	var planner AgentSlot
	for _, a := range in.Agents {
		if a.ID == "a-9" {
			planner = a
		}
	}
	if !planner.Busy || planner.Desk != desks[0] {
		t.Fatalf("a-9 is %+v while %s is RUNNING on %s, want busy at %s", planner, first.ID, desks[0], desks[0])
	}
	dec := Schedule(in)
	if b := blockedFor(dec, second.ID); b == nil || b.Reason != BlockedByCapability || b.Detail != "every planner agent is busy" {
		t.Fatalf("%s reported %+v while the only planner runs %s, want capability: every planner agent is busy", second.ID, b, first.ID)
	}
	for _, r := range dec.Ready {
		if r.AgentID == "a-9" {
			t.Fatalf("the planner was handed %s while it runs %s", r.TaskID, first.ID)
		}
	}
}
