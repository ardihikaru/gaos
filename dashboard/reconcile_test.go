package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// T-053. D-4: "On boot, reconcile every task claiming to be dispatched or
// running against its lease and heartbeat: adopt, requeue, or fail as orphaned.
// This is the whole of recovery; there is no other path."

// TestTheThreeRecoveryBranches is the architecture page's pseudocode, one row
// per branch, with the reason each is what it is printed on failure.
//
// A table because the three are one decision read three ways, and splitting
// them into three functions would let two pass while the middle one -- the only
// one that is dangerous -- quietly changed.
func TestTheThreeRecoveryBranches(t *testing.T) {
	const now = 1_000_000
	cases := []struct {
		name        string
		lease, beat int64
		want, rule  string
		why         string
	}{
		{
			"a live lease with a fresh beat", now + 200, now - 10,
			ReconcileAdopt, RuleAdopted,
			"the process is alive and somebody is watching it; killing or restarting it " +
				"would throw away work that is going fine",
		},
		{
			"a live lease with a stale beat", now + 200, now - heartbeatTimeout - 1,
			ReconcileRequeue, RuleRequeued,
			"the supervisor stopped beating, which says nothing about the child -- D-2 " +
				"detached it precisely so it would outlive its supervisor",
		},
		{
			"an expired lease", now - 1, now - 10_000,
			ReconcileOrphan, RuleLeaseExpired,
			"neither signal can prove the run alive, and a task that cannot be proved " +
				"alive is not assumed alive",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Reconcile([]RunClaim{{
				TaskID: "wf-1-t-1", Status: StatusRunning,
				LeaseUntil: c.lease, LastBeatAt: c.beat,
			}}, now, heartbeatTimeout)
			if len(got) != 1 {
				t.Fatalf("one claim produced %d decisions", len(got))
			}
			if got[0].Action != c.want {
				t.Errorf("%s was %s, want %s; %s", c.name, got[0].Action, c.want, c.why)
			}
			if got[0].Rule != c.rule {
				t.Errorf("the rule is %q, want %q; D-052 puts the machine-readable reason in "+
					"the payload so no reducer has to parse the sentence", got[0].Rule, c.rule)
			}
			if strings.TrimSpace(got[0].Reason) == "" {
				t.Error("the decision carries no reason; a recovery nobody can explain is a " +
					"recovery nobody can audit")
			}
		})
	}
}

// TestTheReconcilerLooksAtEveryTaskClaimingToBeDispatchedOrRunningAndNoOthers is
// D-4's scope, and it is a scope rather than a filter: a task in any other
// status is not making a claim about a process, so there is nothing to
// reconcile and touching it would be inventing a recovery.
func TestTheReconcilerLooksAtEveryTaskClaimingToBeDispatchedOrRunningAndNoOthers(t *testing.T) {
	const now = 1_000_000
	var claims []RunClaim
	for _, st := range taskStatuses() {
		claims = append(claims, RunClaim{
			TaskID: "t-" + st, Status: st, LeaseUntil: now - 1,
		})
	}
	got := Reconcile(claims, now, heartbeatTimeout)
	if len(got) != 2 {
		t.Fatalf("%d of the ten statuses were reconciled, want 2; D-4 scopes recovery to "+
			"DISPATCHED and RUNNING, and a task in any other status is not claiming a process",
			len(got))
	}
	seen := map[string]bool{}
	for _, d := range got {
		seen[d.TaskID] = true
	}
	for _, want := range []string{"t-" + StatusDispatched, "t-" + StatusRunning} {
		if !seen[want] {
			t.Errorf("%s was not reconciled; a task claiming a process that nobody checked is "+
				"the hole recovery exists to close", want)
		}
	}
}

// TestReconciliationIsPureAndDoesNotReadTheClock. Same argument as the
// scheduler's: the decision runs once per boot, over evidence nobody can
// reproduce afterwards, so a clock read inside it would be untestable in the
// one way that matters.
func TestReconciliationIsPureAndDoesNotReadTheClock(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "reconcile.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse reconcile.go: %v", err)
	}
	allowed := map[string]bool{`"fmt"`: true, `"sort"`: true}
	for _, imp := range f.Imports {
		if !allowed[imp.Path.Value] {
			t.Errorf("reconcile.go imports %s; the whole of recovery has to be a function of the "+
				"evidence it was handed, and a file that can reach the clock is one edit from "+
				"a recovery nobody can reproduce", imp.Path.Value)
		}
	}
	for _, d := range f.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Recv != nil {
			t.Errorf("reconcile.go declares a method (%s); the impure half belongs in runner.go, "+
				"where it can hold a lock and touch a disk without this file claiming it does not",
				fn.Name.Name)
		}
	}
}

// TestAdoptAndRequeueReuseTheCatalogNamesAndDifferOnlyInTheRule pins the
// decision that kept the catalog closed.
//
// BR-05 makes a new event type a schema change to the fold. Adopt and requeue
// are two outcomes of one recovery, and giving each its own name would have
// been two constants, two reducer cases and an amendment to the model document
// -- for a distinction that fits in a field. D-052 set the precedent with
// DispatchRefused; this follows it.
func TestAdoptAndRequeueReuseTheCatalogNamesAndDifferOnlyInTheRule(t *testing.T) {
	const now = 1_000_000
	adopt := Reconcile([]RunClaim{{TaskID: "a", Status: StatusRunning,
		LeaseUntil: now + 200, LastBeatAt: now}}, now, heartbeatTimeout)
	requeue := Reconcile([]RunClaim{{TaskID: "b", Status: StatusRunning,
		LeaseUntil: now + 200, LastBeatAt: now - heartbeatTimeout - 1}}, now, heartbeatTimeout)

	if adopt[0].Rule == requeue[0].Rule {
		t.Fatalf("both recoveries carry rule %q; they share TaskResumed, so the rule is the "+
			"only thing that distinguishes them and a reader of the stream could not tell an "+
			"adopted run from a requeued one", adopt[0].Rule)
	}
	if adopt[0].Rule != RuleAdopted || requeue[0].Rule != RuleRequeued {
		t.Errorf("the rules are %q and %q, want %q and %q",
			adopt[0].Rule, requeue[0].Rule, RuleAdopted, RuleRequeued)
	}
}

// TestARequeueThatCannotKillDegradesToAnOrphan is D-054's teeth, and the case
// most likely to be read as over-caution.
//
// Requeue PROMISES the previous run has been made dead. With no adapter
// configured there is nothing that knows how to kill it, so the promise cannot
// be kept -- and a requeue whose kill silently did nothing is the second run in
// one workspace that D-3 exists to prevent, wearing a reassuring event in the
// timeline saying it was handled. Failing loudly is worse than a clean requeue
// and far better than silent corruption.
func TestARequeueThatCannotKillDegradesToAnOrphan(t *testing.T) {
	s, ws, deskID := reconcilerFixture(t)

	// A lease that is live, with a beat far enough back to be stale. That is the
	// requeue branch -- and there is no adapter on this store.
	now := time.Now().Unix()
	if err := AcquireLease(ws, Lease{
		TaskID: "wf-1-t-1", RunID: "wf-1-t-1-a1",
		StartedAt: now - 1000, LeaseUntil: now + 60,
	}); err != nil {
		t.Fatal(err)
	}

	res, err := s.Reconcile(t.Context())
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(res.NewEvents) != 1 {
		t.Fatalf("one claimed run produced %d events", len(res.NewEvents))
	}
	if got := res.NewEvents[0].Type; got != TaskOrphaned {
		t.Errorf("a requeue with nothing able to kill emitted %s, want %s; the requeue would "+
			"have handed the workspace on while claiming the previous run was dead, which is "+
			"the corruption D-3 exists to prevent reached through the recovery routine",
			got, TaskOrphaned)
	}
	if st := s.taskStatusOf("wf-1-t-1"); st != StatusFailed {
		t.Errorf("the task is %s after an orphan, want %s", st, StatusFailed)
	}
	if d := s.deskByIDForTest(deskID); d.State != StateBlocked {
		t.Errorf("the desk is %q after an orphan, want blocked; an orphan is not work waiting "+
			"to be picked up, it is work whose fate nobody can establish", d.State)
	}
}

// TestAnAdoptedRunIsNotKilledAndNotRestarted. The positive half, and the one
// the step-5 acceptance test lands on: a supervisor that died and came back
// within the staleness window finds its work still running and simply picks it
// up.
func TestAnAdoptedRunIsNotKilledAndNotRestarted(t *testing.T) {
	s, ws, deskID := reconcilerFixture(t)
	now := time.Now().Unix()
	if err := AcquireLease(ws, Lease{
		TaskID: "wf-1-t-1", RunID: "wf-1-t-1-a1",
		StartedAt: now, LeaseUntil: now + leaseTTL,
	}); err != nil {
		t.Fatal(err)
	}

	res, err := s.Reconcile(t.Context())
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(res.NewEvents) != 1 || res.NewEvents[0].Type != TaskResumed {
		t.Fatalf("adoption wrote %v, want one TaskResumed", typesOf(res.NewEvents))
	}
	if rule := res.NewEvents[0].Payload.Rule; rule != RuleAdopted {
		t.Errorf("the adoption carries rule %q, want %q", rule, RuleAdopted)
	}
	if st := s.taskStatusOf("wf-1-t-1"); st != StatusRunning {
		t.Errorf("the task is %s after adoption, want %s; nothing stopped, so nothing should "+
			"have moved", st, StatusRunning)
	}
	if _, held, _ := ReadLease(ws); !held {
		t.Error("adoption broke the lease; the run is still going and its workspace is still " +
			"its own")
	}
	if d := s.deskByIDForTest(deskID); d.State == StateBlocked {
		t.Error("adoption blocked the desk; an adopted run is work continuing, not work stuck")
	}
}

// TestOrphanReleasesTheWorkspaceSoSomethingCanUseItAgain is BRD §7.5's
// exception row: "release the workspace, emit TaskOrphaned, apply the retry
// policy."
func TestOrphanReleasesTheWorkspaceSoSomethingCanUseItAgain(t *testing.T) {
	s, ws, _ := reconcilerFixture(t)
	if err := AcquireLease(ws, Lease{
		TaskID: "wf-1-t-1", RunID: "wf-1-t-1-a1", StartedAt: 1, LeaseUntil: 2,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Reconcile(t.Context()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if _, held, _ := ReadLease(ws); held {
		t.Error("an orphaned run kept its lease; the workspace can never be scheduled again, " +
			"and the expiry that made it an orphan would go on making it one forever")
	}
}

// ---- fixtures ----

// reconcilerFixture builds a store with one task claiming to be running, and a
// real directory for it to have been running in.
func reconcilerFixture(t *testing.T) (*MemStore, string, string) {
	t.Helper()
	s := NewMemStore(5)
	root := t.TempDir()
	deskID := s.desks[0].ID
	ws := filepath.Join(root, deskID)
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.workspaceRoot = root
	s.tasks = append(s.tasks, TaskRecord{
		ID: "wf-1-t-1", WorkflowID: "wf-1", DeskID: deskID,
		Title: "work nobody was watching", Role: "backend", Status: StatusRunning,
	})
	s.mu.Unlock()
	return s, ws, deskID
}

func (s *MemStore) taskStatusOf(id string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, t := range s.tasks {
		if t.ID == id {
			return t.Status
		}
	}
	return ""
}

func (s *MemStore) deskByIDForTest(id string) Desk {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if i := s.deskIdxByID(id); i >= 0 {
		return s.desks[i].Clone()
	}
	return Desk{}
}
