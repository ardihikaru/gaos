package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// One suite, both stores. This is T-008, and the reason the docs give for it is
// the reason it is written this way: "running both implementations against one
// interface suite is what stops the seam from rotting."
//
// Every case takes a Store and nothing else. That constraint is doing real
// work -- it is why none of them can reach for MemStore.Tick(), which is not on
// the interface, and why a case that passes here passes for a caller that only
// ever holds the seam. The four Tick-driven tests in main_test.go stay where
// they are for exactly that reason: they test the simulation, which is one
// store's private business.
//
// Cases come in positive/negative pairs. A check with no stated failure mode
// has not been thought through, and D-039 makes every case in the register part
// of what "success" means for this project.
//
// The ArcadeDB half is skipped unless GAOS_ARCADE_TEST_URL names a reachable
// database. `deploy/schema/scratch.sh up` prints that line.

type storeCase struct {
	name string
	run  func(t *testing.T, s Store)
}

var storeContract = []storeCase{
	{"KindIsOneOfTheTwoDeclaredValues", caseKind},
	{"ReadyReportsTheStoreIsUsable", caseReady},
	{"SnapshotHasOneDeskPerSeedProduct", caseSnapshotShape},
	{"SnapshotDoesNotAliasTheLiveFloor", caseSnapshotNoAlias},
	{"DeskOutOfRangeIsNotFound", caseDeskOutOfRange},
	{"ProductsAndMastersAgreeOnIDs", caseProductsMastersAgree},
	{"MasterOfAnUnknownProductIsNotFound", caseMasterUnknown},
	{"CreateProductAddsAProductAndAMaster", caseCreateProduct},
	{"CreateProductWithoutANameIsRejected", caseCreateProductNoName},
	{"UpdateBRDRoundTrips", caseUpdateBRD},
	{"UpdateBRDOfAnUnknownProductIsNotFound", caseUpdateBRDUnknown},
	{"AddStoryRoundTrips", caseAddStory},
	{"AddStoryWithoutARoleOrWantIsRejected", caseAddStoryEmpty},
	{"AddDiagramRoundTrips", caseAddDiagram},
	{"AddDiagramWithoutASourceIsRejected", caseAddDiagramEmpty},
	{"AddDecisionRoundTrips", caseAddDecision},
	{"AddDecisionWithoutATitleIsRejected", caseAddDecisionEmpty},
	{"InjectReportsTheProjectionItPromised", caseInject},
	{"EveryMutationEmitsAtLeastOneEvent", caseMutationEmits},
	{"EventSequenceIsStrictlyIncreasing", caseEventSeq},
	{"EventsCarryNoPayloadToTheCaller", caseEventsNoPayload},
	{"EventsNeverReferenceARID", caseEventsNeverReferenceARID},
	{"ReplayFoldsTheSameFloorTwice", caseReplayFoldsTheSameFloorTwice},
	{"ReadOnlyHandleRefusesEveryMutation", caseReadOnlyHandleRefusesEveryMutation},
	{"ApproveWithoutAGateIsRejected", caseApproveNoGate},
	{"CancelReleasesTheDeskAndCancellingTwiceIsRejected", caseCancel},
	{"SpeedOutsideTheAllowedSetIsRejected", caseSpeed},
	{"PauseRoundTrips", casePaused},
	{"ReplayFreezesTheFloorAndMinusOneReturnsToLive", caseReplay},
	{"ReplayPastTheEndIsNotFound", caseReplayPastEnd},
	{"PlanWorkflowWritesNothing", casePlanIsPure},
	{"CreateWorkflowWithoutAnObjectiveIsRejected", caseCreateWorkflowNoGoal},
	{"CreateWorkflowMovesTheDeskAndTheCoverage", caseCreateWorkflow},
	{"PlanningWritesTasksAndDependencies", casePlanWritesGraph},
	{"APlanWithACycleIsRejected", casePlanCycleRejected},
	{"ContextSliceIsAssembledFromTheRecord", caseContextSlice},
	{"DispatchScreensBeforeItDispatches", caseDispatchScreens},
	{"DispatchOnADeskAlreadyRunningIsRejected", caseDispatchBusy},
	{"ARefusedDispatchBlocksTheDeskAndSaysWhy", caseDispatchRefusedBlocks},
	{"FocusClampsRatherThanFailing", caseFocus},
}

func TestStoreConformanceMemory(t *testing.T) {
	for _, c := range storeContract {
		t.Run(c.name, func(t *testing.T) { c.run(t, NewMemStore(1)) })
	}
}

func TestStoreConformanceArcadeDB(t *testing.T) {
	arc := arcadeTestTarget(t)
	for i, c := range storeContract {
		t.Run(c.name, func(t *testing.T) {
			c.run(t, newArcadeTestStore(t, arc, fmt.Sprintf("conf-%03d", i)))
		})
	}
}

// TestArcadeStoreRebuildsTheFloorFromTheGraph is the case that cannot be
// written against the interface, because "restart the process" is not something
// a Store exposes. It is also the case that decides whether the projection in
// store_arcade.go is a cache or a lie: BR-01 allows anything cached in memory
// that is reconstructible, so a second store opened on the same tenant must see
// what the first one wrote and nothing it merely remembered.
func TestArcadeStoreRebuildsTheFloorFromTheGraph(t *testing.T) {
	arc := arcadeTestTarget(t)
	ctx := context.Background()
	tenant := "conf-restart"

	first := newArcadeTestStore(t, arc, tenant)
	if _, err := first.CreateProduct(ctx, ProductDraft{Name: "Ledger Rewrite", Owner: "ardi"}); err != nil {
		t.Fatalf("create product: %v", err)
	}
	if _, err := first.AddStory(ctx, "p-1", Story{Role: "operator", Want: "an audit trail"}); err != nil {
		t.Fatalf("add story: %v", err)
	}
	before, err := first.Snapshot(ctx)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	beforeEvents, _ := first.Events(ctx)

	second, err := NewArcadeStoreFor(ctx, arc, 1, tenant)
	if err != nil {
		t.Fatalf("reopen the store on the same tenant: %v", err)
	}
	after, err := second.Snapshot(ctx)
	if err != nil {
		t.Fatalf("snapshot after reopen: %v", err)
	}

	if len(after.Desks) != len(before.Desks) {
		t.Fatalf("reopened floor has %d desks, the graph was written with %d -- "+
			"the projection is holding state the database never received",
			len(after.Desks), len(before.Desks))
	}
	for i := range before.Desks {
		if after.Desks[i].ID != before.Desks[i].ID || after.Desks[i].State != before.Desks[i].State {
			t.Errorf("desk %d came back as %s/%s, was %s/%s", i,
				after.Desks[i].ID, after.Desks[i].State,
				before.Desks[i].ID, before.Desks[i].State)
		}
	}
	afterEvents, _ := second.Events(ctx)
	if len(afterEvents) != len(beforeEvents) {
		t.Errorf("reopened store has %d events, wrote %d -- BR-02 says a state "+
			"change that emitted no event never happened, and half of these did not",
			len(afterEvents), len(beforeEvents))
	}
	products, _ := second.Products(ctx)
	found := false
	for _, p := range products {
		if p.Name == "Ledger Rewrite" {
			found = true
		}
	}
	if !found {
		t.Error("the product created before the reopen is gone; the write never reached the graph")
	}
	m, err := second.Master(ctx, "p-1")
	if err != nil {
		t.Fatalf("master after reopen: %v", err)
	}
	if len(m.Stories) == 0 {
		t.Error("the story added before the reopen is gone from the master record")
	}
}

// TestTheTaskGraphSurvivesARestart is the BR-01 half of T-042, and it has to be
// its own test for the same reason as the one above: "restart the process" is
// not something the Store interface offers.
//
// It matters more here than for the floor. The floor is folded from the stream,
// so a projection that lost it could rebuild it; the task graph is deliberately
// NOT folded -- see the TaskPlanned case in fold.go -- so ArcadeStore.load is
// the only path back. If this fails, the graph is memory wearing a record's
// clothes, and the scheduler that reads it after a restart is scheduling
// against nothing.
//
// The direction of DEPENDS_ON is asserted rather than assumed. 08-edges.sql
// says "the OUT side waits for the IN side" and every readiness query depends
// on it, so an edge written the other way round would round-trip perfectly and
// invert every scheduling decision made from it.
func TestTheTaskGraphSurvivesARestart(t *testing.T) {
	arc := arcadeTestTarget(t)
	ctx := context.Background()
	tenant := "conf-graph-restart"

	first := newArcadeTestStore(t, arc, tenant)
	m, err := first.Master(ctx, "p-1")
	if err != nil {
		t.Fatalf("master: %v", err)
	}
	if len(m.Stories) < 2 {
		t.Skip("p-1 has fewer than two stories, so there is no fan-in to assert")
	}
	draft := WorkflowDraft{
		ProductID: "p-1",
		Goal:      "Prove the graph comes back",
		Stories:   []string{m.Stories[0].ID, m.Stories[1].ID},
	}
	plan, err := first.PlanWorkflow(ctx, draft)
	if err != nil {
		t.Fatalf("PlanWorkflow: %v", err)
	}
	if _, err := first.CreateWorkflow(ctx, draft, plan); err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	before := first.(graphReader).tasksSnapshot("p-1")
	if len(before) == 0 {
		t.Fatal("nothing was planned, so there is nothing to prove about a restart")
	}

	second, err := NewArcadeStoreFor(ctx, arc, 1, tenant)
	if err != nil {
		t.Fatalf("reopen the store on the same tenant: %v", err)
	}
	after := second.tasksSnapshot("p-1")

	if len(after) != len(before) {
		t.Fatalf("the reopened graph holds %d tasks, %d were written -- the graph is not "+
			"folded from the stream, so load() is the only way back and it did not bring "+
			"them", len(after), len(before))
	}
	byID := map[string]TaskRecord{}
	for _, task := range after {
		byID[task.ID] = task
	}
	for _, want := range before {
		got, ok := byID[want.ID]
		if !ok {
			t.Errorf("%s is missing after the reopen", want.ID)
			continue
		}
		if got.Title != want.Title || got.Role != want.Role || got.Status != want.Status {
			t.Errorf("%s came back as %q/%s/%s, was %q/%s/%s", want.ID,
				got.Title, got.Role, got.Status, want.Title, want.Role, want.Status)
		}
		if got.WorkflowID != want.WorkflowID || got.Ordinal != want.Ordinal {
			t.Errorf("%s came back on workflow %q at ordinal %d, was %q at %d; the ordinal is "+
				"what puts the slice back in plan order, which D-1 needs to be a property of "+
				"the data rather than of the bucket layout",
				want.ID, got.WorkflowID, got.Ordinal, want.WorkflowID, want.Ordinal)
		}
		if !reflect.DeepEqual(sortedCopy(got.Deps), sortedCopy(want.Deps)) {
			t.Errorf("%s waits on %v after the reopen, wrote %v; DEPENDS_ON points from the "+
				"dependent to its prerequisite, and an edge that came back reversed would "+
				"invert every readiness answer computed from it",
				want.ID, got.Deps, want.Deps)
		}
	}

	// The counter, which is the other thing load() has to put back. A restarted
	// dashboard that resumed numbering from a count would hand the next workflow
	// an id an earlier one already holds.
	if _, err := second.CreateWorkflow(ctx, draft, plan); err != nil {
		t.Fatalf("CreateWorkflow after the reopen: %v", err)
	}
	third := second.tasksSnapshot("p-1")
	seen := map[string]bool{}
	for _, task := range third {
		if seen[task.ID] {
			t.Errorf("%s was written twice after a restart; the workflow counter restarted "+
				"too, and Task (tenantId, id) UNIQUE means the second write upserted over "+
				"the first", task.ID)
		}
		seen[task.ID] = true
	}
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

// TestFoldSurvivesTheGraphRoundTrip is the other case the interface cannot
// express, and the one that decides whether the event deltas are really being
// persisted or merely held in the projection that wrote them.
//
// A payload that never reaches the database looks perfect until the process
// restarts, at which point every event folds to nothing and the replay is a
// blank floor. So: write through one store, reopen on the same tenant, and
// require the reloaded stream to fold to the floor the graph itself reports.
func TestFoldSurvivesTheGraphRoundTrip(t *testing.T) {
	arc := arcadeTestTarget(t)
	ctx := context.Background()
	tenant := "conf-foldtrip"

	first := newArcadeTestStore(t, arc, tenant)
	if _, err := first.CreateProduct(ctx, ProductDraft{Name: "Fold Trip", Owner: "ardi"}); err != nil {
		t.Fatalf("create product: %v", err)
	}
	f, _ := first.Snapshot(ctx)
	for i, d := range f.Desks {
		if d.ApprovalRequired {
			if _, err := first.ApproveTask(ctx, i); err != nil {
				t.Fatalf("approve desk %d: %v", i, err)
			}
		}
	}
	if _, err := first.CancelTask(ctx, 0); err != nil {
		t.Fatalf("cancel desk 0: %v", err)
	}

	second, err := NewArcadeStoreFor(ctx, arc, 1, tenant)
	if err != nil {
		t.Fatalf("reopen the store on the same tenant: %v", err)
	}

	evs := second.proj.rawEvents()
	if len(evs) == 0 {
		t.Fatal("the reopened store has no events at all")
	}
	empty := EventPayload{}
	carried := 0
	for _, e := range evs {
		if !reflect.DeepEqual(e.Payload, empty) {
			carried++
		}
	}
	if carried == 0 {
		t.Fatal("every event came back from the graph with an empty payload; the " +
			"reducer deltas are not being persisted and a replay would fold to a blank floor")
	}

	last := evs[len(evs)-1].Seq
	folded := fold(emptyFloor(), evs, last)
	live, err := second.Snapshot(ctx)
	if err != nil {
		t.Fatalf("snapshot after reopen: %v", err)
	}
	if len(folded.Desks) != len(live.Desks) {
		t.Fatalf("the reloaded stream folds to %d desks, the graph reports %d",
			len(folded.Desks), len(live.Desks))
	}
	for i, d := range live.Desks {
		if folded.Desks[i].State != d.State {
			t.Errorf("desk %s: the graph says %q, the reloaded stream folds to %q",
				d.ID, d.State, folded.Desks[i].State)
		}
		if folded.Desks[i].ApprovalRequired != d.ApprovalRequired {
			t.Errorf("desk %s: the graph says gate=%v, the reloaded stream folds to %v",
				d.ID, d.ApprovalRequired, folded.Desks[i].ApprovalRequired)
		}
	}
}

// ---------------------------------------------------------------- the cases

func caseKind(t *testing.T, s Store) {
	switch k := s.Kind(); k {
	case "memory", "arcadedb":
	default:
		t.Fatalf("Kind() is %q; store.go declares the vocabulary as \"memory\" | \"arcadedb\"", k)
	}
}

func caseReady(t *testing.T, s Store) {
	if err := s.Ready(context.Background()); err != nil {
		t.Fatalf("Ready() says the store is unusable: %v", err)
	}
}

func caseSnapshotShape(t *testing.T, s Store) {
	f, err := s.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(f.Desks) != len(seedProducts) {
		t.Fatalf("floor has %d desks, the fixtures seed %d", len(f.Desks), len(seedProducts))
	}
	if f.Stats.DeskCount != len(f.Desks) {
		t.Errorf("Stats.DeskCount is %d but the floor carries %d desks; the topbar "+
			"would disagree with the carousel", f.Stats.DeskCount, len(f.Desks))
	}
	if len(f.Rooms) == 0 {
		t.Error("no rooms on the floor; the rail has nothing to draw")
	}
}

func caseSnapshotNoAlias(t *testing.T, s Store) {
	ctx := context.Background()
	f, _ := s.Snapshot(ctx)
	if len(f.Desks) == 0 {
		t.Skip("no desks to alias")
	}
	f.Desks[0].Name = "MUTATED BY THE CALLER"
	f.Desks[0].Agents = append(f.Desks[0].Agents, "a-999")

	again, _ := s.Snapshot(ctx)
	if again.Desks[0].Name == "MUTATED BY THE CALLER" {
		t.Fatal("a caller writing to the returned Floor changed the store's own desk; " +
			"Snapshot handed out a reference, not a copy")
	}
	for _, a := range again.Desks[0].Agents {
		if a == "a-999" {
			t.Fatal("the desk's Agents slice is shared with the caller")
		}
	}
}

func caseDeskOutOfRange(t *testing.T, s Store) {
	ctx := context.Background()
	f, _ := s.Snapshot(ctx)
	for _, idx := range []int{-1, len(f.Desks), len(f.Desks) + 50} {
		if _, _, err := s.Desk(ctx, idx); !errors.Is(err, ErrNotFound) {
			t.Errorf("Desk(%d) returned %v, want ErrNotFound", idx, err)
		}
	}
	if _, _, err := s.Desk(ctx, 0); err != nil {
		t.Errorf("Desk(0) failed on a floor with %d desks: %v", len(f.Desks), err)
	}
}

func caseProductsMastersAgree(t *testing.T, s Store) {
	ctx := context.Background()
	ps, err := s.Products(ctx)
	if err != nil {
		t.Fatalf("Products: %v", err)
	}
	ms, err := s.Masters(ctx)
	if err != nil {
		t.Fatalf("Masters: %v", err)
	}
	if len(ps) != len(ms) {
		t.Fatalf("%d products but %d masters; one product owns one record", len(ps), len(ms))
	}
	byID := map[string]bool{}
	for _, m := range ms {
		byID[m.ID] = true
	}
	for _, p := range ps {
		if !byID[p.ID] {
			t.Errorf("product %s has no master record", p.ID)
		}
	}
}

func caseMasterUnknown(t *testing.T, s Store) {
	if _, err := s.Master(context.Background(), "p-does-not-exist"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Master of an unknown product returned %v, want ErrNotFound", err)
	}
}

func caseCreateProduct(t *testing.T, s Store) {
	ctx := context.Background()
	before, _ := s.Products(ctx)
	r, err := s.CreateProduct(ctx, ProductDraft{Name: "Settlement Engine", Owner: "ardi"})
	if err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}
	if r.Master == nil {
		t.Fatal("CreateProduct returned no Master; the back office renders it from this call")
	}
	after, _ := s.Products(ctx)
	if len(after) != len(before)+1 {
		t.Fatalf("products went from %d to %d, want one more", len(before), len(after))
	}
	m, err := s.Master(ctx, r.Master.ID)
	if err != nil {
		t.Fatalf("the master just created is not readable back: %v", err)
	}
	if m.Owner != "ardi" {
		t.Errorf("owner came back as %q, want %q", m.Owner, "ardi")
	}
}

func caseCreateProductNoName(t *testing.T, s Store) {
	_, err := s.CreateProduct(context.Background(), ProductDraft{Name: "   "})
	assertRejected(t, err, "Give the product a name.")
}

func caseUpdateBRD(t *testing.T, s Store) {
	ctx := context.Background()
	id := firstMasterID(t, s)
	brd := BRD{
		Problem: "Quotes are recalculated on every keystroke and the pricing service melts.",
		Goals:   []string{"Cut the recalculation rate by an order of magnitude"},
		Rules:   []string{"A quote is never recalculated more than once per second"},
	}
	if _, err := s.UpdateBRD(ctx, id, brd); err != nil {
		t.Fatalf("UpdateBRD: %v", err)
	}
	m, err := s.Master(ctx, id)
	if err != nil {
		t.Fatalf("Master: %v", err)
	}
	if m.BRD.Problem != brd.Problem {
		t.Errorf("problem came back as %q, want %q", m.BRD.Problem, brd.Problem)
	}
	if len(m.BRD.Goals) != 1 || m.BRD.Goals[0] != brd.Goals[0] {
		t.Errorf("goals came back as %v, want %v", m.BRD.Goals, brd.Goals)
	}
	if len(m.BRD.Rules) != 1 {
		t.Errorf("rules came back as %v, want one", m.BRD.Rules)
	}
}

func caseUpdateBRDUnknown(t *testing.T, s Store) {
	_, err := s.UpdateBRD(context.Background(), "p-nope", BRD{Problem: "x"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateBRD on an unknown product returned %v, want ErrNotFound", err)
	}
}

func caseAddStory(t *testing.T, s Store) {
	ctx := context.Background()
	id := firstMasterID(t, s)
	before, _ := s.Master(ctx, id)
	_, err := s.AddStory(ctx, id, Story{
		Role: "operator", Want: "to see why a task was refused",
		So: "I can fix the record instead of retrying", AC: []string{"The reason is shown on the desk"},
	})
	if err != nil {
		t.Fatalf("AddStory: %v", err)
	}
	after, _ := s.Master(ctx, id)
	if len(after.Stories) != len(before.Stories)+1 {
		t.Fatalf("stories went from %d to %d, want one more", len(before.Stories), len(after.Stories))
	}
	got := after.Stories[len(after.Stories)-1]
	if got.Want != "to see why a task was refused" {
		t.Errorf("want came back as %q", got.Want)
	}
	if len(got.AC) != 1 {
		t.Errorf("acceptance criteria came back as %v, want one", got.AC)
	}
	if got.ID == "" {
		t.Error("the store assigned no id to the story")
	}
}

func caseAddStoryEmpty(t *testing.T, s Store) {
	id := firstMasterID(t, s)
	_, err := s.AddStory(context.Background(), id, Story{Role: "", Want: ""})
	assertRejected(t, err, "A story needs at least a role and a want.")
}

func caseAddDiagram(t *testing.T, s Store) {
	ctx := context.Background()
	id := firstMasterID(t, s)
	before, _ := s.Master(ctx, id)
	if _, err := s.AddDiagram(ctx, id, Diagram{
		Kind: "sequence", Title: "Quote refresh", Src: "UI -> Pricing: quote",
	}); err != nil {
		t.Fatalf("AddDiagram: %v", err)
	}
	after, _ := s.Master(ctx, id)
	if len(after.Diagrams) != len(before.Diagrams)+1 {
		t.Fatalf("diagrams went from %d to %d, want one more", len(before.Diagrams), len(after.Diagrams))
	}
	if got := after.Diagrams[len(after.Diagrams)-1]; got.Src != "UI -> Pricing: quote" {
		t.Errorf("source came back as %q", got.Src)
	}
}

func caseAddDiagramEmpty(t *testing.T, s Store) {
	id := firstMasterID(t, s)
	_, err := s.AddDiagram(context.Background(), id, Diagram{Title: "Nothing", Src: ""})
	assertRejected(t, err, "A diagram needs a title and a source.")
}

func caseAddDecision(t *testing.T, s Store) {
	ctx := context.Background()
	id := firstMasterID(t, s)
	before, _ := s.Master(ctx, id)
	if _, err := s.AddDecision(ctx, id, Decision{
		Title: "Round half-even in the pricing service", Confidence: 0.9,
	}); err != nil {
		t.Fatalf("AddDecision: %v", err)
	}
	after, _ := s.Master(ctx, id)
	if len(after.Decisions) != len(before.Decisions)+1 {
		t.Fatalf("decisions went from %d to %d, want one more",
			len(before.Decisions), len(after.Decisions))
	}
	if got := after.Decisions[len(after.Decisions)-1]; got.Title != "Round half-even in the pricing service" {
		t.Errorf("title came back as %q", got.Title)
	}
}

func caseAddDecisionEmpty(t *testing.T, s Store) {
	id := firstMasterID(t, s)
	_, err := s.AddDecision(context.Background(), id, Decision{Title: "  "})
	assertRejected(t, err, "Write the decision first.")
}

// caseInject checks the one number the back office promises the operator. Both
// stores compute it from Master.Project(), which is why they must agree even
// though only one of them writes anything.
func caseInject(t *testing.T, s Store) {
	ctx := context.Background()
	id := firstMasterID(t, s)
	m, _ := s.Master(ctx, id)
	want := m.Project()

	r, err := s.Inject(ctx, id)
	if err != nil {
		t.Fatalf("Inject: %v", err)
	}
	if len(r.NewEvents) == 0 {
		t.Fatal("Inject emitted no event; BR-02 says a state change that emitted none never happened")
	}
	needle := fmt.Sprintf("%d vertices and %d edges", want.Vertices, want.Edges)
	if !strings.Contains(r.Toast, needle) {
		t.Errorf("Inject reported %q, want it to contain %q -- the back office promised "+
			"that number from the same Project() call", r.Toast, needle)
	}
	if _, err := s.Inject(ctx, "p-nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Inject on an unknown product returned %v, want ErrNotFound", err)
	}
}

func caseMutationEmits(t *testing.T, s Store) {
	ctx := context.Background()
	id := firstMasterID(t, s)
	r, err := s.AddDecision(ctx, id, Decision{Title: "Ship the thing"})
	if err != nil {
		t.Fatalf("AddDecision: %v", err)
	}
	if len(r.NewEvents) == 0 {
		t.Fatal("a successful mutation returned no NewEvents; the timeline appends " +
			"from this field and would show nothing happened")
	}
	if len(r.Floor.Desks) == 0 {
		t.Error("a successful mutation returned an empty Floor; the handler renders the fragment from it")
	}
}

func caseEventSeq(t *testing.T, s Store) {
	ctx := context.Background()
	id := firstMasterID(t, s)
	for i := 0; i < 3; i++ {
		if _, err := s.AddDecision(ctx, id, Decision{Title: fmt.Sprintf("Decision number %d", i)}); err != nil {
			t.Fatalf("AddDecision %d: %v", i, err)
		}
	}
	evs, err := s.Events(ctx)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(evs) < 3 {
		t.Fatalf("only %d events after three mutations", len(evs))
	}
	for i := 1; i < len(evs); i++ {
		if evs[i].Seq <= evs[i-1].Seq {
			t.Fatalf("event %d has seq %d, after seq %d -- the stream is append-only "+
				"and its order is the only thing replay can trust", i, evs[i].Seq, evs[i-1].Seq)
		}
	}
}

func caseEventsNoPayload(t *testing.T, s Store) {
	evs, err := s.Events(context.Background())
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	empty := EventPayload{}
	for i, e := range evs {
		if !reflect.DeepEqual(e.Payload, empty) {
			t.Fatalf("event %d carries its reducer delta to the caller; the timeline "+
				"renders four fields, and a payload that escapes the store is a handle "+
				"onto the stream's own slices", i)
		}
	}
}

// T-010 and BR-03: an event names the business id, never the physical RID. A
// restore into different buckets renumbers every RID in the database, and a
// history keyed by one would be unreadable the moment it mattered most.
func caseEventsNeverReferenceARID(t *testing.T, s Store) {
	evs, err := s.Events(context.Background())
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	rid := regexp.MustCompile(`^#\d+:\d+$`)
	for i, e := range evs {
		if rid.MatchString(e.DeskID) {
			t.Fatalf("event %d (%s) references RID %q instead of a business id",
				i, e.Type, e.DeskID)
		}
		if e.DeskID == "" {
			t.Fatalf("event %d (%s) references nothing at all", i, e.Type)
		}
	}
}

// The fold acceptance test, run through the seam so both stores answer it.
// BRD §4.1, TC-050: fold from genesis equals fold from the nearest snapshot.
//
// Replay is the only way to ask a Store for a folded floor, so this drives it:
// scrub to an event, read the floor, and require it to be the same floor
// whichever route the implementation took to build it. The store-specific half
// -- throwing the snapshot ladder away entirely -- is TestFoldNeedsNoSnapshotAtAll,
// which needs access MemStore has and the interface deliberately does not.
func caseReplayFoldsTheSameFloorTwice(t *testing.T, s Store) {
	ctx := context.Background()
	evs, err := s.Events(ctx)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(evs) == 0 {
		t.Skip("no events to fold")
	}
	target := evs[len(evs)-1].Seq

	first, err := s.Replay(ctx, target)
	if err != nil {
		t.Fatalf("Replay to seq %d: %v", target, err)
	}
	if _, err := s.Replay(ctx, -1); err != nil {
		t.Fatalf("back to live: %v", err)
	}
	second, err := s.Replay(ctx, target)
	if err != nil {
		t.Fatalf("Replay to seq %d again: %v", target, err)
	}
	if !reflect.DeepEqual(first.Floor.Desks, second.Floor.Desks) {
		t.Fatalf("folding to seq %d twice produced two different floors; "+
			"a fold that is not deterministic is not an audit trail", target)
	}
	if _, err := s.Replay(ctx, -1); err != nil {
		t.Fatalf("back to live: %v", err)
	}
}

// BR-20 and T-099: the handle a replaying caller holds cannot write.
func caseReadOnlyHandleRefusesEveryMutation(t *testing.T, s Store) {
	ctx := context.Background()
	ro := readOnly{s}

	for name, call := range map[string]func() (Result, error){
		"DispatchTask":       func() (Result, error) { return ro.DispatchTask(ctx, 0) },
		"ApproveTask":        func() (Result, error) { return ro.ApproveTask(ctx, 0) },
		"RetryTask":          func() (Result, error) { return ro.RetryTask(ctx, 0) },
		"CancelTask":         func() (Result, error) { return ro.CancelTask(ctx, 0) },
		"CallReview":         func() (Result, error) { return ro.CallReview(ctx, 0) },
		"CloseMeeting":       func() (Result, error) { return ro.CloseMeeting(ctx) },
		"SetPaused":          func() (Result, error) { return ro.SetPaused(ctx, true) },
		"SetSpeed":           func() (Result, error) { return ro.SetSpeed(ctx, 2) },
		"ClearNotifications": func() (Result, error) { return ro.ClearNotifications(ctx) },
		"CreateProduct":      func() (Result, error) { return ro.CreateProduct(ctx, ProductDraft{Name: "X"}) },
		"UpdateBRD":          func() (Result, error) { return ro.UpdateBRD(ctx, "p-1", BRD{}) },
		"AddStory":           func() (Result, error) { return ro.AddStory(ctx, "p-1", Story{Role: "a", Want: "b"}) },
		"AddDiagram":         func() (Result, error) { return ro.AddDiagram(ctx, "p-1", Diagram{Title: "t", Src: "s"}) },
		"AddDecision":        func() (Result, error) { return ro.AddDecision(ctx, "p-1", Decision{Title: "t"}) },
		"Inject":             func() (Result, error) { return ro.Inject(ctx, "p-1") },
		"CreateWorkflow":     func() (Result, error) { return ro.CreateWorkflow(ctx, WorkflowDraft{}, nil) },
	} {
		if _, err := call(); !errors.Is(err, ErrRejected) {
			t.Errorf("readOnly.%s returned %v, want ErrRejected -- §08 asks for a "+
				"handle that cannot write, not a flag somebody remembers to check", name, err)
		}
	}

	// Reads still work. A handle that refused those would not be read-only, it
	// would be closed, and the operator could not look at the past at all.
	if _, err := ro.Snapshot(ctx); err != nil {
		t.Errorf("readOnly.Snapshot: %v", err)
	}
	if _, err := ro.Events(ctx); err != nil {
		t.Errorf("readOnly.Events: %v", err)
	}
	if _, err := ro.Replay(ctx, -1); err != nil {
		t.Errorf("readOnly.Replay back to live: %v", err)
	}
}

// The three dispatch cases. T-126/T-127 seen through the seam, so both stores
// have to agree that BR-32 holds -- an egress pass that ran in memory and not
// against the graph would be a rule with a hole exactly the shape of production.

// findDeskInState returns the index of the first desk in one of the states, or
// -1. Dispatch is only legal from idle or blocked, and which desks the fixtures
// seed in which state is not something a conformance case should assume.
func findDeskInState(f Floor, states ...string) int {
	for i, d := range f.Desks {
		for _, want := range states {
			if d.State == want {
				return i
			}
		}
	}
	return -1
}

func caseDispatchScreens(t *testing.T, s Store) {
	ctx := context.Background()
	f, _ := s.Snapshot(ctx)
	idx := findDeskInState(f, StateIdle, StateBlocked)
	if idx < 0 {
		t.Skip("no seeded desk is idle or blocked, so nothing is dispatchable")
	}

	res, err := s.DispatchTask(ctx, idx)
	if err != nil {
		t.Fatalf("DispatchTask on an idle desk returned %v; with the default policy and no workspace the packet is GAOS's own writing and must clear", err)
	}
	if len(res.NewEvents) == 0 {
		t.Fatal("a dispatch emitted nothing; the stream is the record (BR-04), and a dispatch nothing wrote never happened")
	}

	var dispatched bool
	for _, e := range res.NewEvents {
		if e.Type == TaskDispatched {
			dispatched = true
			if e.Payload.Digest == "" {
				t.Error("TaskDispatched carries no digest; an attempt that does not name its input cannot be audited against one")
			}
		}
	}
	if !dispatched {
		t.Errorf("a cleared dispatch emitted %v with no TaskDispatched", res.NewEvents)
	}

	if _, err := s.DispatchTask(ctx, 9999); !errors.Is(err, ErrNotFound) {
		t.Errorf("DispatchTask on a desk that does not exist returned %v, want ErrNotFound", err)
	}
}

func caseDispatchBusy(t *testing.T, s Store) {
	ctx := context.Background()
	f, _ := s.Snapshot(ctx)
	idx := findDeskInState(f, StateActive, StateReview)
	if idx < 0 {
		t.Skip("no seeded desk has work in flight")
	}

	_, err := s.DispatchTask(ctx, idx)
	assertRejected(t, err, "This desk already has a task in flight.")
}

func caseDispatchRefusedBlocks(t *testing.T, s Store) {
	ctx := context.Background()
	f, _ := s.Snapshot(ctx)
	idx := findDeskInState(f, StateIdle, StateBlocked)
	if idx < 0 {
		t.Skip("no seeded desk is idle or blocked, so nothing is dispatchable")
	}

	// Reach past the seam to set the policy: there is deliberately no Store
	// method for it, because §7.6.1 makes the egress block a deployment
	// decision rather than something a caller may vary per request.
	type policySetter interface{ setEgressForTest(EgressPolicy, Target) }
	ps, ok := s.(policySetter)
	if !ok {
		t.Skip("this store does not expose the egress policy for testing")
	}
	ps.setEgressForTest(EgressPolicy{LocalOnly: true}, Target{Provider: "anthropic", Local: false})

	res, err := s.DispatchTask(ctx, idx)
	if err != nil {
		t.Fatalf("a refusal came back as err=%v; a refusal is a recorded state change, and an error means the events L-5 requires never reach the response that renders them", err)
	}

	var refused bool
	for _, e := range res.NewEvents {
		if e.Type == DispatchRefused {
			refused = true
			if e.Payload.Rule != RuleLocalOnly {
				t.Errorf("DispatchRefused carries rule %q, want %q", e.Payload.Rule, RuleLocalOnly)
			}
		}
	}
	if !refused {
		t.Fatalf("a refused dispatch emitted %v with no DispatchRefused; L-5 wants the refusal on the timeline with its reason", res.NewEvents)
	}

	d, _, err := s.Desk(ctx, idx)
	if err != nil {
		t.Fatal(err)
	}
	if d.State != StateBlocked || d.BlockReason == "" {
		t.Fatalf("after a refused dispatch the desk is %q with reason %q, want blocked with a reason — a desk left idle says nothing happened, and something did", d.State, d.BlockReason)
	}
}

func caseApproveNoGate(t *testing.T, s Store) {
	ctx := context.Background()
	f, _ := s.Snapshot(ctx)
	idx := -1
	for i, d := range f.Desks {
		if !d.ApprovalRequired {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Skip("every seeded desk is holding a gate")
	}
	_, err := s.ApproveTask(ctx, idx)
	assertRejected(t, err, "Nothing is holding the merge gate on this desk.")

	if _, err := s.ApproveTask(ctx, 9999); !errors.Is(err, ErrNotFound) {
		t.Errorf("ApproveTask on a desk that does not exist returned %v, want ErrNotFound", err)
	}
}

func caseCancel(t *testing.T, s Store) {
	ctx := context.Background()
	f, _ := s.Snapshot(ctx)
	idx := -1
	for i, d := range f.Desks {
		if d.State != StateIdle {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Skip("every seeded desk is already idle")
	}
	r, err := s.CancelTask(ctx, idx)
	if err != nil {
		t.Fatalf("CancelTask: %v", err)
	}
	if r.Floor.Desks[idx].State != StateIdle {
		t.Errorf("desk %d is %q after cancel, want %q", idx, r.Floor.Desks[idx].State, StateIdle)
	}
	if len(r.Floor.Desks[idx].Agents) != 0 {
		t.Errorf("desk %d still seats %v after cancel; the workspace lease was not released",
			idx, r.Floor.Desks[idx].Agents)
	}
	_, err = s.CancelTask(ctx, idx)
	assertRejected(t, err, "This desk is already idle.")
}

func caseSpeed(t *testing.T, s Store) {
	ctx := context.Background()
	for _, ok := range []int{1, 2, 4} {
		r, err := s.SetSpeed(ctx, ok)
		if err != nil {
			t.Fatalf("SetSpeed(%d): %v", ok, err)
		}
		if r.Floor.Stats.Speed != ok {
			t.Errorf("SetSpeed(%d) left the speed at %d", ok, r.Floor.Stats.Speed)
		}
	}
	for _, bad := range []int{0, 3, -1, 8} {
		_, err := s.SetSpeed(ctx, bad)
		assertRejected(t, err, "speed must be 1, 2 or 4")
	}
}

func casePaused(t *testing.T, s Store) {
	ctx := context.Background()
	r, err := s.SetPaused(ctx, true)
	if err != nil {
		t.Fatalf("SetPaused(true): %v", err)
	}
	if !r.Floor.Stats.Paused {
		t.Error("SetPaused(true) did not report a paused floor")
	}
	r, err = s.SetPaused(ctx, false)
	if err != nil {
		t.Fatalf("SetPaused(false): %v", err)
	}
	if r.Floor.Stats.Paused {
		t.Error("SetPaused(false) did not report a running floor")
	}
}

func caseReplay(t *testing.T, s Store) {
	ctx := context.Background()
	evs, _ := s.Events(ctx)
	if len(evs) == 0 {
		t.Skip("no events to replay to")
	}
	genesis := evs[0].Seq
	r, err := s.Replay(ctx, genesis)
	if err != nil {
		t.Fatalf("Replay(%d): %v", genesis, err)
	}
	if !r.Floor.Stats.Replay {
		t.Errorf("Replay(%d) did not report a floor in replay", genesis)
	}
	if r.Floor.Stats.ReplayAt != genesis {
		t.Errorf("Replay(%d) parked the cursor at %d", genesis, r.Floor.Stats.ReplayAt)
	}
	r, err = s.Replay(ctx, -1)
	if err != nil {
		t.Fatalf("Replay(-1): %v", err)
	}
	if r.Floor.Stats.Replay {
		t.Error("Replay(-1) did not return the floor to live; -1 is the sentinel for live")
	}
}

func caseReplayPastEnd(t *testing.T, s Store) {
	ctx := context.Background()
	evs, _ := s.Events(ctx)
	last := 0
	if n := len(evs); n > 0 {
		last = evs[n-1].Seq
	}
	if _, err := s.Replay(ctx, last+10); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Replay past the end of the stream returned %v, want ErrNotFound", err)
	}
	// Seq is 1-based, so 0 is a seq no event will ever carry. It has to be a
	// not-found rather than a silent clamp to genesis: an operator who lands
	// there by mistake must be told, not quietly shown a different moment.
	if _, err := s.Replay(ctx, 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Replay(0) returned %v, want ErrNotFound -- no event carries seq 0", err)
	}
}

// casePlanIsPure guards the property store.go states in a comment: "PlanWorkflow
// is pure: it reads the master and returns a task graph without writing
// anything, so the wizard can preview a plan the user may discard."
func casePlanIsPure(t *testing.T, s Store) {
	ctx := context.Background()
	id := firstMasterID(t, s)
	before, _ := s.Events(ctx)

	plan, err := s.PlanWorkflow(ctx, WorkflowDraft{ProductID: id, Goal: "Ship the settlement rewrite"})
	if err != nil {
		t.Fatalf("PlanWorkflow: %v", err)
	}
	if len(plan) == 0 {
		t.Fatal("PlanWorkflow produced no tasks; the wizard has nothing to preview")
	}
	after, _ := s.Events(ctx)
	if len(after) != len(before) {
		t.Fatalf("PlanWorkflow emitted %d event(s); it is declared pure and the "+
			"operator has not agreed to anything yet", len(after)-len(before))
	}
	if _, err := s.PlanWorkflow(ctx, WorkflowDraft{ProductID: "p-nope"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("PlanWorkflow for an unknown product returned %v, want ErrNotFound", err)
	}
}

func caseCreateWorkflowNoGoal(t *testing.T, s Store) {
	ctx := context.Background()
	id := firstMasterID(t, s)
	plan, _ := s.PlanWorkflow(ctx, WorkflowDraft{ProductID: id, Goal: "x"})
	_, err := s.CreateWorkflow(ctx, WorkflowDraft{ProductID: id, Goal: "  "}, plan)
	assertRejected(t, err, "Write the objective in one sentence.")

	_, err = s.CreateWorkflow(ctx, WorkflowDraft{ProductID: id, Goal: "A real objective"}, nil)
	assertRejected(t, err, "The plan is empty. Go back and build it.")
}

// graphReader is the task graph, reached past the seam on purpose.
//
// Same door-with-a-sign-on-it as policySetter in caseDispatchRefusedBlocks: the
// graph is not on the Store interface -- nothing above the store needs to read
// it yet, and the scheduler that will is T-048 -- but a conformance case that
// could not see it would be asserting that CreateWorkflow does not crash.
type graphReader interface {
	tasksSnapshot(deskID string) []TaskRecord
}

// casePlanWritesGraph is T-042's positive half at the seam: creating a workflow
// leaves a real graph behind, in both stores, with the prerequisites intact.
func casePlanWritesGraph(t *testing.T, s Store) {
	g, ok := s.(graphReader)
	if !ok {
		t.Skip("this store does not expose the task graph for testing")
	}
	ctx := context.Background()
	id := firstMasterID(t, s)
	m, _ := s.Master(ctx, id)
	if len(m.Stories) == 0 {
		t.Skip("the first master record has no stories to plan against")
	}
	draft := WorkflowDraft{
		ProductID: id,
		Goal:      "Persist the plan as a graph rather than a preview",
		Stories:   []string{m.Stories[0].ID},
	}
	plan, err := s.PlanWorkflow(ctx, draft)
	if err != nil {
		t.Fatalf("PlanWorkflow: %v", err)
	}
	if _, err := s.CreateWorkflow(ctx, draft, plan); err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}

	tasks := g.tasksSnapshot(id)
	if len(tasks) != len(plan) {
		t.Fatalf("the plan had %d tasks and the graph holds %d; a plan that is previewed and "+
			"not stored is a plan the scheduler cannot see", len(plan), len(tasks))
	}
	ids := map[string]bool{}
	deps := 0
	for _, task := range tasks {
		ids[task.ID] = true
		deps += len(task.Deps)
		if task.Status != StatusPending {
			t.Errorf("%s was written as %q; READY is the scheduler's word (D-1) and the "+
				"planner has no lease, no capacity and no budget zone to decide it with",
				task.ID, task.Status)
		}
		if task.WorkflowID == "" || !strings.HasPrefix(task.ID, task.WorkflowID+"-") {
			t.Errorf("%s is not qualified by its workflow %q; two plans both numbering from "+
				"t-1 collide under Task (tenantId, id) UNIQUE", task.ID, task.WorkflowID)
		}
	}
	if deps == 0 {
		t.Error("the graph holds no prerequisites at all; DEPENDS_ON is what makes it a graph " +
			"rather than a list, and T-042 exists because it used to be neither")
	}
	for _, task := range tasks {
		for _, dep := range task.Deps {
			if !ids[dep] {
				t.Errorf("%s depends on %s, which is not in the stored graph; the edge points "+
					"at a vertex that does not exist and the task can never become ready",
					task.ID, dep)
			}
		}
	}
}

// casePlanCycleRejected is the negative half, and the reason validatePlan lives
// in CreateWorkflow rather than in PlanWorkflow: the planner cannot produce a
// cycle, but this method takes the plan from the caller and the seam is public.
func casePlanCycleRejected(t *testing.T, s Store) {
	g, ok := s.(graphReader)
	if !ok {
		t.Skip("this store does not expose the task graph for testing")
	}
	ctx := context.Background()
	id := firstMasterID(t, s)
	before := len(g.tasksSnapshot(id))

	_, err := s.CreateWorkflow(ctx, WorkflowDraft{ProductID: id, Goal: "A real objective"},
		[]PlanTask{
			{ID: "t-1", Title: "a", Role: "planner", Deps: []string{"t-2"}},
			{ID: "t-2", Title: "b", Role: "backend", Deps: []string{"t-1"}},
		})
	if err == nil {
		t.Fatal("a plan whose two tasks wait on each other was accepted; neither can ever " +
			"be ready, and the scheduler would walk that graph on every pass forever")
	}
	if !errors.Is(err, ErrRejected) {
		t.Errorf("the refusal is %v, which is not an ErrRejected; BR-06 wants a reason the "+
			"handler can put in a toast", err)
	}
	if after := len(g.tasksSnapshot(id)); after != before {
		t.Errorf("a refused plan left %d task(s) behind; the validation runs before the first "+
			"write so a rejection changes nothing", after-before)
	}
}

func caseCreateWorkflow(t *testing.T, s Store) {
	ctx := context.Background()
	id := firstMasterID(t, s)
	m, _ := s.Master(ctx, id)
	if len(m.Stories) == 0 {
		t.Skip("the first master record has no stories to plan against")
	}
	draft := WorkflowDraft{
		ProductID: id,
		Goal:      "Move rounding into the pricing service and prove it with a replay",
		Stories:   []string{m.Stories[0].ID},
	}
	plan, err := s.PlanWorkflow(ctx, draft)
	if err != nil {
		t.Fatalf("PlanWorkflow: %v", err)
	}
	r, err := s.CreateWorkflow(ctx, draft, plan)
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	if len(r.NewEvents) == 0 {
		t.Fatal("CreateWorkflow emitted no event")
	}
	idx := -1
	for i, d := range r.Floor.Desks {
		if d.ID == id {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatalf("no desk for product %s after creating its workflow", id)
	}
	if got := r.Floor.Desks[idx]; got.Goal != draft.Goal {
		t.Errorf("desk goal is %q, want the objective %q", got.Goal, draft.Goal)
	}
	after, _ := s.Master(ctx, id)
	if after.Stories[0].Tasks == 0 {
		t.Error("the planned story still has no tasks; the coverage bar would not move")
	}
}

func caseContextSlice(t *testing.T, s Store) {
	ctx := context.Background()
	slice, err := s.ContextSlice(ctx, 0)
	if err != nil {
		t.Fatalf("ContextSlice(0): %v", err)
	}
	if len(slice) == 0 {
		t.Fatal("the context slice is empty; the inspector card claims it is what the model receives")
	}
	if _, err := s.ContextSlice(ctx, 9999); !errors.Is(err, ErrNotFound) {
		t.Errorf("ContextSlice of a desk that does not exist returned %v, want ErrNotFound", err)
	}
}

// caseFocus records a deliberate asymmetry: Focus clamps where Desk refuses.
// Moving the inspector is navigation, not a domain change, and an operator
// whose click landed one past the end wants the last desk rather than an error
// toast.
func caseFocus(t *testing.T, s Store) {
	ctx := context.Background()
	f, _ := s.Snapshot(ctx)
	r, err := s.Focus(ctx, 9999)
	if err != nil {
		t.Fatalf("Focus past the end returned an error rather than clamping: %v", err)
	}
	if r.Floor.FocusIdx != len(f.Desks)-1 {
		t.Errorf("Focus(9999) parked on desk %d, want the last one at %d",
			r.Floor.FocusIdx, len(f.Desks)-1)
	}
	if r, err = s.Focus(ctx, -5); err != nil {
		t.Fatalf("Focus(-5) returned an error rather than clamping: %v", err)
	} else if r.Floor.FocusIdx != 0 {
		t.Errorf("Focus(-5) parked on desk %d, want 0", r.Floor.FocusIdx)
	}
}

// ---------------------------------------------------------------- helpers

func assertRejected(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("the store accepted what it should have refused; want a rejection reading %q", want)
	}
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("got %v, want an ErrRejected -- §09 says a mutation that will not "+
			"happen says why, in a form the handler can turn into a toast", err)
	}
	if err.Error() != want {
		t.Errorf("rejection reads %q, want %q", err.Error(), want)
	}
}

func firstMasterID(t *testing.T, s Store) string {
	t.Helper()
	ms, err := s.Masters(context.Background())
	if err != nil {
		t.Fatalf("Masters: %v", err)
	}
	if len(ms) == 0 {
		t.Fatal("no master records; the fixtures seed five")
	}
	return ms[0].ID
}

// ---------------------------------------------------------------- the arcade lane

var (
	arcadeOnce sync.Once
	arcadeArc  *Arcade
	arcadeErr  error
)

// arcadeTestTarget resolves the database the ArcadeDB half runs against, or
// skips. Skipping is the right default rather than a failure: `go test ./...`
// has always run anywhere in this repository, deploy/rebuild-restart.sh gates
// every deploy on it, and a suite that needed a database would turn that gate
// into an obstacle on the one machine that has no ArcadeDB.
func arcadeTestTarget(t *testing.T) *Arcade {
	t.Helper()
	url := os.Getenv("GAOS_ARCADE_TEST_URL")
	if url == "" {
		t.Skip("GAOS_ARCADE_TEST_URL is unset; run deploy/schema/scratch.sh up for the ArcadeDB half")
	}
	arcadeOnce.Do(func() {
		arcadeArc = &Arcade{
			URL:        strings.TrimRight(url, "/"),
			DB:         envOr("GAOS_ARCADE_TEST_DB", "gaos_conformance"),
			User:       envOr("GAOS_ARCADE_TEST_USER", "root"),
			Password:   os.Getenv("GAOS_ARCADE_TEST_PASSWORD"),
			client:     &http.Client{Timeout: 10 * time.Second},
			stmtClient: &http.Client{Timeout: 120 * time.Second},
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		_, _, arcadeErr = arcadeArc.ApplySchema(ctx)
	})
	if arcadeErr != nil {
		t.Fatalf("could not prepare %s: %v", os.Getenv("GAOS_ARCADE_TEST_URL"), arcadeErr)
	}
	return arcadeArc
}

// newArcadeTestStore gives one case its own tenant, wiped first so a re-run
// starts from the fixtures rather than from the last run's leftovers.
func newArcadeTestStore(t *testing.T, arc *Arcade, tenant string) Store {
	t.Helper()
	ctx := context.Background()
	wipeTenant(t, arc, tenant)
	s, err := NewArcadeStoreFor(ctx, arc, 1, tenant)
	if err != nil {
		t.Fatalf("NewArcadeStoreFor(%s): %v", tenant, err)
	}
	return s
}

func wipeTenant(t *testing.T, arc *Arcade, tenant string) {
	t.Helper()
	ctx := context.Background()
	for _, typ := range []string{
		"Product", "Workflow", "Task", "TaskRun", "Requirement", "Issue",
		"AcceptanceCriterion", "Artifact", "Decision", "Repository",
		"Event", "Snapshot", "Agent", "Room", "Tenant",
	} {
		if err := arc.Command(ctx,
			fmt.Sprintf("DELETE FROM %s WHERE tenantId = :t", typ),
			map[string]any{"t": tenant}, nil); err != nil {
			t.Fatalf("wipe %s for tenant %s: %v", typ, tenant, err)
		}
	}
}
