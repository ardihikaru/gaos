package main

import (
	"context"
	"strings"
	"testing"
)

// M14.4: one workspace per product, the sync, the drift gate, the pin, and the
// graph lease. Against the fake ai-manager in aimanager_test.go, which keeps
// files per workspace and computes the worker's digest.

// publishedStore is a MemStore with an idle product's Blueprint published and
// the fake gateway attached; it returns that product's id.
func publishedStore(t *testing.T) (*MemStore, *fakeAIM, string) {
	t.Helper()
	s := NewMemStore(1)
	ctx := context.Background()
	id := ""
	for _, d := range s.desks {
		if d.State == StateIdle {
			id = d.ID
			break
		}
	}
	if id == "" {
		t.Fatal("the fixtures seed no idle desk, so nothing can be dispatched")
	}
	if _, err := s.SavePolicy(ctx, id, Policy{Key: "go-style", Kind: PolicyCoding, Title: "Go style", Body: "gofmt"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PublishBlueprint(ctx, id); err != nil {
		t.Fatal(err)
	}
	f, ad := newFakeAIM(t)
	s.SetAdapter(ad)
	return s, f, id
}

func eventsOf(r Result, typ EventType) []Event {
	var out []Event
	for _, e := range r.NewEvents {
		if e.Type == typ {
			out = append(out, e)
		}
	}
	return out
}

func TestProvisioningIsIdempotentAndTheWorkspaceIDIsDerived(t *testing.T) {
	s, f, id := publishedStore(t)
	ctx := context.Background()
	r, err := s.SyncBlueprint(ctx, id)
	if err != nil || len(eventsOf(r, WorkspaceProvisioned)) != 1 || len(eventsOf(r, BlueprintSynced)) != 1 {
		t.Fatalf("the first sync gave %v %v; want one WorkspaceProvisioned and one BlueprintSynced", r.NewEvents, err)
	}
	m, _ := s.Master(ctx, id)
	if want := "gaos-t1-" + id; m.Workspace.ID != want || f.spaces[want] == nil {
		t.Fatalf("the workspace is %q (worker has %d workspace(s)); want %s, derived from tenant and product", m.Workspace.ID, len(f.spaces), want)
	}
	r, _ = s.SyncBlueprint(ctx, id)
	if len(eventsOf(r, WorkspaceProvisioned)) != 0 || f.writes != 1 {
		t.Fatalf("a second sync provisioned again or rewrote unchanged files (%d writes); both must be no-ops", f.writes)
	}
}

func TestADriftedWorkspaceRefusesTheDispatchAndASyncReleasesIt(t *testing.T) {
	s, f, id := publishedStore(t)
	ctx := context.Background()
	r, err := s.DispatchTask(ctx, s.deskIdxByID(id))
	refused := eventsOf(r, DispatchRefused)
	if err != nil || len(refused) != 1 || refused[0].Payload.Rule != RuleBlueprintDrift {
		t.Fatalf("dispatching with an unsynced Blueprint gave %v %v; want one DispatchRefused rule %s (BR-39)", r.NewEvents, err, RuleBlueprintDrift)
	}
	if len(f.bodies) != 0 {
		t.Fatalf("a drifted dispatch still sent %d turn(s) to the gateway", len(f.bodies))
	}
	if _, err := s.SyncBlueprint(ctx, id); err != nil {
		t.Fatal(err)
	}
	s.mu.RLock()
	pin, drift := s.blueprintGate(id)
	reason := s.desks[s.deskIdxByID(id)].BlockReason
	s.mu.RUnlock()
	if drift != nil || pin != 1 || reason != "" {
		t.Fatalf("after the sync the gate says pin %d drift %+v, block reason %q; want version 1, no drift, released", pin, drift, reason)
	}
}

func TestACredentialInAPolicyRefusesTheSyncAndNothingReachesTheGateway(t *testing.T) {
	s, f, id := publishedStore(t)
	ctx := context.Background()
	s.SavePolicy(ctx, id, Policy{Key: "deploy", Kind: PolicyTechnical, Title: "Deploy", Body: "use key AKIAIOSFODNN7EXAMPLE for the bucket"})
	if _, err := s.PublishBlueprint(ctx, id); err != nil {
		t.Fatal(err)
	}
	r, err := s.SyncBlueprint(ctx, id)
	refused := eventsOf(r, DispatchRefused)
	if err != nil || len(refused) != 1 || refused[0].Payload.Rule != RuleEgressBlueprint {
		t.Fatalf("a Blueprint carrying a credential synced with %v %v; want DispatchRefused rule %s (BR-32)", r.NewEvents, err, RuleEgressBlueprint)
	}
	if f.writes != 0 || len(f.spaces) != 0 {
		t.Fatalf("the gateway received %d write(s) of a Blueprint the egress pass refused", f.writes)
	}
}

func TestTheAttemptRecordPinsTheBlueprintAndTheGatewayIDsAcrossAReopen(t *testing.T) {
	arc := arcadeTestTarget(t)
	ctx := t.Context()
	tenant := "conf-pin-restart"
	first := newArcadeTestStore(t, arc, tenant)
	st := first.(*ArcadeStore)
	f, ad := newFakeAIM(t)
	snap, _ := first.Snapshot(ctx)
	desk := snap.Desks[0].ID
	first.SavePolicy(ctx, desk, Policy{Key: "go-style", Kind: PolicyCoding, Title: "Go style", Body: "gofmt"})
	first.PublishBlueprint(ctx, desk)
	st.SetAdapter(ad)
	if _, err := first.SyncBlueprint(ctx, desk); err != nil {
		t.Fatalf("sync: %v", err)
	}
	run := runIDFor("t-1", 1)
	f.reply = chatReply(handoffBlock("t-1", run), "0.2", "")
	c, _, err := Screen(EgressPolicy{}, Target{Provider: "anthropic"}, Packet{TaskID: "t-1", RunID: run, DeskID: desk, Workspace: t.TempDir(),
		Parts: []PacketPart{{Path: ".gaos/packet/task.md", Kind: KindTask, Body: "# t-1"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.proj.superviseRun(ctx, ad, desk, c, 1); err != nil {
		t.Fatalf("supervise: %v", err)
	}
	second, err := NewArcadeStoreFor(ctx, arc, 1, tenant)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	runs, _ := second.Runs(ctx, "t-1")
	if len(runs) != 1 || runs[0].BlueprintVersion != 1 || runs[0].RequestID != ad.RequestIDFor(run) ||
		runs[0].SessionID == "" || runs[0].SliceDigest == "" || runs[0].Invocation != ad.Mode() {
		t.Fatalf("the reopened attempt is %+v; want pinned to version 1 with its request id, session id, slice digest and invocation gateway", runs)
	}
}

func TestASecondTaskInAProductWithALiveTurnIsBlocked(t *testing.T) {
	in := SchedulerInput{
		Tasks: []TaskNode{
			{ID: "t-a", WorkflowID: "wf-1", DeskID: "p-1", Role: "backend", Status: StatusRunning, Ordinal: 0},
			{ID: "t-b", WorkflowID: "wf-1", DeskID: "p-1", Role: "backend", Status: StatusPending, Ordinal: 1},
			{ID: "t-c", WorkflowID: "wf-2", DeskID: "p-2", Role: "backend", Status: StatusPending, Ordinal: 0},
		},
		Agents:    []AgentSlot{{ID: "a-1", Role: "backend"}, {ID: "a-2", Role: "backend"}},
		Readiness: map[string]int{"p-1": 80, "p-2": 80},
		Budget:    BudgetNormal, Now: 1000,
	}
	got := Schedule(in)
	if b := blockedFor(got, "t-b"); b == nil || b.Reason != BlockedByLease || !strings.Contains(b.Detail, "BR-38") {
		t.Fatalf("t-b reported %+v; with t-a running in the same product it must be blocked by the graph lease (BR-38)", b)
	}
	if len(got.Ready) != 1 || got.Ready[0].TaskID != "t-c" {
		t.Fatalf("dispatched %+v; another product's task must still run", got.Ready)
	}
}
