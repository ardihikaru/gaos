package main

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// T-050 and T-052. Everything here runs against the stand-in tool in
// testdata/fakecli; nothing makes a network call, reads a credential or spends
// money, and TestNoTestEverReachesTheRealProviderCommand is what keeps that
// true rather than customary.

// TestARunEmitsRunStartedAndRunExitedAroundTheWorkItSupervised is the positive
// case, and it is also the first time three names come off the owed list in
// events.go -- so it asserts the events exist, in order, and that the cost the
// tool reported reached the desk.
func TestARunEmitsRunStartedAndRunExitedAroundTheWorkItSupervised(t *testing.T) {
	s := NewMemStore(3)
	a, ws := fakeCLI(t, "complete", "0")
	deskID := s.desks[0].ID
	before := len(s.rawEvents())

	att, err := s.superviseRun(t.Context(), a, deskID, clearedFor(t, ws), 1)
	if err != nil {
		t.Fatalf("supervising one run failed: %v", err)
	}
	if att.Outcome() != HandoffComplete {
		t.Fatalf("the run reported %q, want %q; the handoff document is the only authority "+
			"on what happened (D-050)", att.Outcome(), HandoffComplete)
	}

	got := typesOf(s.rawEvents()[before:])
	want := []string{string(RunStarted), string(Heartbeat), string(RunExited)}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the run wrote %v, want %v; RunStarted before the work and RunExited after it "+
			"is the shape a replay reads a run's duration from", got, want)
	}

	// D-051: the cost comes from the tool's own report, and it has to reach the
	// desk or the budget guard has nothing to guard.
	s.mu.RLock()
	deskCost := s.desks[0].Cost
	s.mu.RUnlock()
	if att.CostBasis != CostMeasured {
		t.Errorf("the attempt's basis is %q, want %q; D-051 requires measured from the first run",
			att.CostBasis, CostMeasured)
	}
	if deskCost <= 0 {
		t.Error("the run cost money and the desk still reads zero; a cost nothing recorded is a " +
			"cost the budget guard will never see")
	}
}

// TestARunThatCouldNotStartEmitsNoRunStarted is the negative half, and the rule
// it protects is written in events.go: emitting a RunStarted for a process that
// never started is inventing evidence in the one record the audit story rests
// on.
func TestARunThatCouldNotStartEmitsNoRunStarted(t *testing.T) {
	s := NewMemStore(3)
	a, ws := fakeCLI(t, "complete", "0")
	deskID := s.desks[0].ID

	// Somebody else holds the workspace, so the run never reaches the adapter.
	if err := AcquireLease(ws, Lease{
		TaskID: "other", RunID: "other-a1", LeaseUntil: time.Now().Unix() + 300,
	}); err != nil {
		t.Fatal(err)
	}
	before := len(s.rawEvents())

	if _, err := s.superviseRun(t.Context(), a, deskID, clearedFor(t, ws), 1); err == nil {
		t.Fatal("a run started in a workspace somebody else had leased")
	}
	if after := s.rawEvents()[before:]; len(after) != 0 {
		t.Errorf("a run that never started wrote %v; a RunStarted from a process that never "+
			"started is invented evidence, and events.go says so in as many words",
			typesOf(after))
	}
}

// TestTheHeartbeatWritesTheBeatMoreOftenThanItWritesAnEvent is D-053 made
// checkable, against the pure half so it does not take five minutes to run.
func TestTheHeartbeatWritesTheBeatMoreOftenThanItWritesAnEvent(t *testing.T) {
	// Twelve minutes of beats at the declared interval.
	const start = 1_000_000
	beats, events := 0, 0
	last := int64(start)
	for now := int64(start); now < start+12*60; now += heartbeatInterval {
		beats++
		first := now == start
		if beatEmitsEvent(last, now, first, false) {
			events++
			last = now
		}
	}
	if beats != 24 {
		t.Fatalf("twelve minutes at a %ds interval is %d beats, want 24", heartbeatInterval, beats)
	}
	if events != 3 {
		t.Errorf("the same twelve minutes wrote %d Heartbeat events, want 3; D-053 separates the "+
			"cadences so a run does not put one pill per beat into a stream that is append-only, "+
			"snapshotted every %d events and rendered in full", events, snapshotEvery)
	}

	// The two endpoints always write, because "alive at T" and "silent since T"
	// are the only questions a replay asks and both need one.
	if !beatEmitsEvent(start, start, true, false) {
		t.Error("the first beat of a run wrote no event; a run with no opening beat has no " +
			"start for a replay to measure silence from")
	}
	if !beatEmitsEvent(start, start+1, false, true) {
		t.Error("the last beat before an exit wrote no event")
	}
	if beatEmitsEvent(start, start+heartbeatEventEvery-1, false, false) {
		t.Error("a beat inside the event cadence wrote an event anyway; the whole point of two " +
			"numbers is that most beats are field writes")
	}
}

// TestAnAdoptedRunAndASupervisedRunProduceTheSameAttempt is BR-33 as a
// comparison rather than a promise.
//
// An adopting orchestrator cannot cmd.Wait() on a process it did not fork, so
// Resume is a second entry point -- and a second entry point is a second place
// for the two to drift apart. They share collect() precisely so they cannot,
// and this is what would notice if somebody stopped sharing it.
func TestAnAdoptedRunAndASupervisedRunProduceTheSameAttempt(t *testing.T) {
	a, ws := fakeCLI(t, "complete", "0")
	c := clearedFor(t, ws)

	supervised, err := a.Dispatch(context.Background(), c)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	// The same workspace, read by something that never held the child. Detail is
	// empty, so alive() reports false at once and Resume goes straight to the
	// evidence -- which is the state an adopting orchestrator finds when the run
	// has already finished.
	adopted, err := a.Resume(context.Background(), c, RunRecord{
		RunID: "t-1-a1", Workspace: ws, Digest: c.Digest(),
		Model: supervised.Model, LogRef: supervised.LogRef,
	})
	if err != nil {
		t.Fatalf("resume: %v", err)
	}

	// The exit status is the one honest difference: nobody waited on the adopted
	// run, so there is none to read. Everything the WORK produced must match.
	supervised.ExitClass, supervised.ExitCode = "", 0
	adopted.ExitClass, adopted.ExitCode = "", 0
	if !reflect.DeepEqual(supervised, adopted) {
		t.Errorf("an adopted run produced %+v and a supervised one %+v; BR-33 asks that nothing "+
			"above the seam be able to tell them apart, and a scoring difference between the two "+
			"would be one nobody could explain", adopted, supervised)
	}
	if adopted.CostBasis != CostMeasured {
		t.Errorf("the adopted run's basis is %q, want measured; the cost lives in a file rather "+
			"than in the dead orchestrator's memory precisely so this works", adopted.CostBasis)
	}
}

// TestAHandoffFromThePreviousAttemptIsNotReadAsThisOnes is the truncation
// handoff.go had always claimed and nothing did.
//
// Without it a retry in a reused workspace reads the previous attempt's
// document and scores a run that already finished -- and it would look like a
// success, which is the worst available direction for the mistake.
func TestAHandoffFromThePreviousAttemptIsNotReadAsThisOnes(t *testing.T) {
	a, ws := fakeCLI(t, "nohandoff", "0")
	c := clearedFor(t, ws)

	// A complete document from an earlier attempt, left in the workspace.
	stale, err := os.ReadFile(filepath.Join("testdata", "handoff", "complete.md"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(ws, HandoffPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, stale, 0o600); err != nil {
		t.Fatal(err)
	}

	att, err := a.Dispatch(context.Background(), c)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if att.Failure != SigHandoffMissing {
		t.Fatalf("a run that wrote no handoff reported %q, want %q; the previous attempt's "+
			"document was still on disk and was read as this one's",
			att.Failure, SigHandoffMissing)
	}
	if att.Outcome() != "failed" {
		t.Errorf("the attempt's outcome is %q; a stale document made a run that produced "+
			"nothing look like one that succeeded", att.Outcome())
	}
}

// TestTheDetachedChildIsNotKilledByTheOrchestratorsContext is D-2, at the level
// a Go test can reach it.
//
// The line under test is context.WithoutCancel in Dispatch. Before it, dc
// descended from serve()'s signal context, so SIGTERM cancelled the context,
// CommandContext killed the child, and "work runs in detached processes that
// survive the orchestrator" was false on exactly the path an operator uses.
//
// The full property -- kill the orchestrator PROCESS and the work survives --
// cannot be a Go test: the test binary is the parent of everything it spawns
// and go test reaps the tree on exit, so the harness undoes the thing being
// tested. That is deploy/acceptance/kill-mid-task.sh, and T-053.
func TestTheDetachedChildIsNotKilledByTheOrchestratorsContext(t *testing.T) {
	a, ws := fakeCLI(t, "complete", "0")
	t.Setenv("FAKE_CLI_SLEEP", "1")
	a.SessionEnv = append(a.SessionEnv, "FAKE_CLI_SLEEP")

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(150 * time.Millisecond)
		cancel() // the orchestrator is going down
	}()

	att, err := a.Dispatch(ctx, clearedFor(t, ws))
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if att.Outcome() != HandoffComplete {
		t.Fatalf("the child reported %q after its caller's context was cancelled; D-2 says work "+
			"runs in detached processes that survive the orchestrator, and an orchestrator "+
			"restart must not kill a paid model call in flight", att.Outcome())
	}
	if att.ExitClass != ExitRan {
		t.Errorf("the child exited %q, want %q; a cancelled context reached it", att.ExitClass, ExitRan)
	}
}

// TestTheRunReleasesItsLeaseWhateverHappens. A workspace left leased by a run
// that is over is a desk nothing can schedule again, and it would take a
// restart plus a reconciler to notice.
func TestTheRunReleasesItsLeaseWhateverHappens(t *testing.T) {
	for _, mode := range []string{"complete", "nohandoff"} {
		t.Run(mode, func(t *testing.T) {
			s := NewMemStore(3)
			a, ws := fakeCLI(t, mode, "0")
			if _, err := s.superviseRun(t.Context(), a, s.desks[0].ID, clearedFor(t, ws), 1); err != nil {
				t.Fatalf("supervise: %v", err)
			}
			if _, held, err := ReadLease(ws); held || err != nil {
				t.Errorf("the lease survived a %s run (held=%v err=%v); the workspace can never "+
					"be scheduled again and nothing short of a restart would notice",
					mode, held, err)
			}
		})
	}
}

// TestTheRunnerRefusesToSpawnWithoutAWorkspaceRoot. GAOS_WORKSPACE_ROOT is
// unset in every deployment today, which makes it the strongest of the guards
// that already existed: with no workspace there is nothing to lease, nothing to
// run in, and no way to reach a provider.
func TestTheRunnerRefusesToSpawnWithoutAWorkspaceRoot(t *testing.T) {
	s := NewMemStore(3)
	a, _ := fakeCLI(t, "complete", "0")

	p := Packet{TaskID: "t-1", DeskID: "d-1"} // no workspace
	c, _, err := Screen(EgressPolicy{}, Target{Provider: "anthropic"}, p)
	if err == nil {
		if _, err := s.superviseRun(t.Context(), a, s.desks[0].ID, c, 1); err == nil {
			t.Fatal("a run was supervised with no workspace; there would be nothing to lease " +
				"and nothing to be exclusive about")
		}
		return
	}
	// An empty packet does not clear the egress pass at all, which is a stronger
	// refusal and arrives earlier. Either is correct; neither reaches a tool.
	if !strings.Contains(err.Error(), "empty") && !strings.Contains(err.Error(), "no ") {
		t.Logf("the empty packet was refused as %v", err)
	}
}

// TestNoTestEverReachesTheRealProviderCommand is the money guard, checked
// structurally.
//
// Five configuration guards stand in front of a paid call and every one of them
// can be switched off by a developer with a good reason who then runs
// `go test ./...`. This is the sixth, and the only one that does not depend on
// anybody remembering anything.
func TestNoTestEverReachesTheRealProviderCommand(t *testing.T) {
	// The guard itself, first: it must refuse a real tool and permit the fake.
	if err := refuseIfUnderTest([]string{"claude"}); err == nil {
		t.Error("refuseIfUnderTest permitted the real provider command from a test binary; a " +
			"paid model call triggered by go test is money that does not come back")
	}
	if err := refuseIfUnderTest([]string{"sh", "/x/testdata/fakecli/claude"}); err != nil {
		t.Errorf("refuseIfUnderTest refused the stand-in tool (%v); the adapter's own tests "+
			"drive it and a guard that blocks them is a guard somebody will delete", err)
	}

	// And the guard has to still be ON the spawn path. A behavioural test cannot
	// see this: with the call deleted, every test here still passes, because
	// they all drive the fake and the fake is what the guard permits. Only the
	// day somebody points a real deployment at it does the deletion show up, and
	// by then it has cost money.
	//
	// Dispatch and not Smoke, deliberately. Smoke runs `--version`, which is
	// free; Dispatch is the one that spends.
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "adapter_cli.go", nil, 0)
	if err != nil {
		t.Fatalf("parse adapter_cli.go: %v", err)
	}
	guarded := false
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "Dispatch" || fn.Recv == nil {
			continue
		}
		ast.Inspect(fn, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "refuseIfUnderTest" {
				guarded = true
			}
			return true
		})
	}
	if !guarded {
		t.Error("CLIAdapter.Dispatch does not call refuseIfUnderTest; every test in this package " +
			"would still pass without it, because they all drive the fake, and the deletion " +
			"would only surface as a bill")
	}
}
