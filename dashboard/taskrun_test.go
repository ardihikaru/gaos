package main

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// M4.3: T-054 (one TaskRun per attempt), T-055 (the frozen ContextSlice and its
// USES_CONTEXT provenance) and T-056 (serving the frozen one).
//
// Everything here runs against the fake ai-manager in aimanager_test.go, under
// the same rule the rest of the runner tests are held to: nothing leaves the
// host, reads a credential or spends money.

// TestEachAttemptGetsItsOwnRunRecord is D-5 and BR-22 in one assertion. Two
// attempts on one task, and the first one's evidence must still be there
// afterwards -- a retry that overwrote the record it retried would leave the
// guard in BR-16 comparing an attempt against itself.
func TestEachAttemptGetsItsOwnRunRecord(t *testing.T) {
	s := NewMemStore(3)
	f, a := gatewayOn(t, s)
	deskID := s.desks[0].ID

	for attempt := 1; attempt <= 2; attempt++ {
		completeTurn(f, "t-1", attempt, "0.1")
		if _, err := s.superviseRun(t.Context(), a, deskID, clearedFor(t, s, deskID, "t-1", attempt), attempt); err != nil {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
	}

	runs, err := s.Runs(t.Context(), "t-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 {
		t.Fatalf("two attempts left %d record(s); BR-22 makes each attempt its own record, "+
			"and a retry that overwrites the attempt it retried destroys the only evidence "+
			"the retry policy has to reason from", len(runs))
	}
	if runs[0].Attempt != 1 || runs[1].Attempt != 2 {
		t.Errorf("the attempts came back as %d then %d, want 1 then 2; ordering by attempt is "+
			"how an auditor reads a task's history", runs[0].Attempt, runs[1].Attempt)
	}
	if runs[0].ID == runs[1].ID {
		t.Errorf("both attempts share the run id %q; two rows that cannot be told apart are one row",
			runs[0].ID)
	}
	for _, r := range runs {
		if r.Status != RunStatusExited {
			t.Errorf("attempt %d ended as %q, want %q", r.Attempt, r.Status, RunStatusExited)
		}
		if r.SliceDigest == "" {
			t.Errorf("attempt %d names no context slice; an attempt that does not name its input "+
				"cannot be audited against one", r.Attempt)
		}
	}
}

// TestAFinishedRunRecordsWhatItCostAndWhatItLeftBehind is the other half of
// D-5: the record is only worth having if the fields on it are filled in.
func TestAFinishedRunRecordsWhatItCostAndWhatItLeftBehind(t *testing.T) {
	s := NewMemStore(3)
	f, a := gatewayOn(t, s)
	completeTurn(f, "t-1", 1, "0.42")
	deskID := s.desks[0].ID

	att, err := s.superviseRun(t.Context(), a, deskID, clearedFor(t, s, deskID, "t-1", 1), 1)
	if err != nil {
		t.Fatal(err)
	}

	runs, _ := s.Runs(t.Context(), "t-1")
	if len(runs) != 1 {
		t.Fatalf("one run produced %d record(s)", len(runs))
	}
	r := runs[0]

	if r.CostUSD != att.CostUSD || r.TokensIn != att.TokensIn || r.TokensOut != att.TokensOut {
		t.Errorf("the record holds cost %.4f / %d in / %d out and the attempt reported "+
			"%.4f / %d / %d; a figure that does not survive the write is a figure the budget "+
			"guard will never see", r.CostUSD, r.TokensIn, r.TokensOut,
			att.CostUSD, att.TokensIn, att.TokensOut)
	}
	if r.CostBasis != CostMeasured {
		t.Errorf("the recorded basis is %q, want %q; BR-31 exists so a measured number and a "+
			"guessed one are never added together", r.CostBasis, CostMeasured)
	}
	if r.Invocation != a.Mode() {
		t.Errorf("the record says invocation %q, adapter mode is %q; D-13 requires it recorded "+
			"precisely so the prohibition on branching can be checked rather than trusted",
			r.Invocation, a.Mode())
	}
	if r.EndedAt < r.StartedAt || r.StartedAt == 0 {
		t.Errorf("the run ran from %d to %d; a duration that cannot be read is a run nobody can "+
			"replay the timing of", r.StartedAt, r.EndedAt)
	}
	s.mu.RLock()
	reply := s.artifacts[r.ID+"-reply"]
	s.mu.RUnlock()
	if r.RequestID == "" || reply.Body == "" {
		t.Error("the record names no turn or keeps no reply; BR-25 keeps the output out of the " +
			"vertex and BR-41 keeps it in GAOS, so the artifact is the only way back to it")
	}
}

// TestARunThatCouldNotStartIsNotRecordedAsMeasured is the negative case for
// BR-31, and it is the one that is tempting to get wrong: the DDL defaults
// costBasis to "estimated", so writing a zero cost with the default would put a
// number nobody measured into the same column as ones somebody did. Here the
// gateway refuses the turn, so nothing was spent and nothing was measured.
func TestARunThatCouldNotStartIsNotRecordedAsMeasured(t *testing.T) {
	s := NewMemStore(3)
	f, a := gatewayOn(t, s)
	f.submit = http.StatusBadGateway // the gateway refuses the submit: no turn ran
	deskID := s.desks[0].ID

	if _, err := s.superviseRun(t.Context(), a, deskID, clearedFor(t, s, deskID, "t-1", 1), 1); err != nil {
		t.Fatalf("a refused submit came back as an error (%v); it is a failed attempt with a signature", err)
	}

	runs, _ := s.Runs(t.Context(), "t-1")
	if len(runs) != 1 || runs[0].FailureSignature == "" || runs[0].CostBasis == CostMeasured {
		t.Fatalf("a turn the gateway refused was recorded as %+v; want one failed attempt whose cost "+
			"is not measured, because nothing was, and BR-31 forbids aggregating it with figures that were", runs)
	}
}

// TestTheSliceIsFrozenWhenTheDispatchClears is T-055 through the operator's own
// route: the desk has no adapter behind it, and the slice is still recorded.
// D-6 is about what was assembled and cleared, not about what a provider did
// with it afterwards.
func TestTheSliceIsFrozenWhenTheDispatchClears(t *testing.T) {
	s := NewMemStore(20260807)
	s.egress = EgressPolicy{} // no path restriction: the packet is GAOS's own
	idx := idleDesk(t, s)
	before := len(s.rawEvents())

	if _, err := s.DispatchTask(t.Context(), idx); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	var digest string
	for _, e := range s.rawEvents()[before:] {
		if e.Type == TaskDispatched {
			digest = e.Payload.Digest
		}
	}
	if digest == "" {
		t.Fatal("the dispatch event carries no digest; an attempt that does not name its input " +
			"cannot be audited against one")
	}

	s.mu.RLock()
	rec, ok := s.slices[digest]
	s.mu.RUnlock()
	if !ok {
		t.Fatalf("nothing was frozen for digest %s; D-6 stores the slice, and a slice reassembled "+
			"later answers a different question convincingly", digest)
	}
	if rec.Rendered == "" || rec.NodeCount == 0 {
		t.Errorf("the frozen slice holds %d part(s) and %d bytes of text; an empty record is worse "+
			"than none, because it looks like an answer", rec.NodeCount, len(rec.Rendered))
	}
	if len(rec.Cites) == 0 {
		t.Error("the frozen slice cites nothing; USES_CONTEXT is what makes it possible to ask " +
			"why a node was in the prompt rather than only that something was")
	}
	for _, c := range rec.Cites {
		if c.Role == "" {
			t.Errorf("a citation of %s/%s carries no role; role is mandatory on USES_CONTEXT and "+
				"an unlabelled edge answers only that a node was there", c.NodeType, c.NodeID)
		}
	}
}

// TestARedactedPartTakesItsCitationsWithIt is the negative half of T-055 and
// the honesty of the whole audit edge. A citation kept for a part the egress
// pass cut would claim the model was given something BR-32 refused to send.
func TestARedactedPartTakesItsCitationsWithIt(t *testing.T) {
	pkt := Packet{
		TaskID: "t-1", DeskID: "d-1", Workspace: "/tmp/ws",
		Parts: []PacketPart{
			{Path: ".gaos/packet/rules.md", Kind: KindRules, Body: "- money is rounded half-up\n"},
			{Path: "vendor/big.go", Kind: KindRepo, Body: strings.Repeat("x", 4096)},
		},
		Cites: []Citation{
			{NodeType: "Requirement", NodeID: "p-1-rule-1", Role: CiteRule, Path: ".gaos/packet/rules.md"},
			{NodeType: "", NodeID: "", Role: CiteCode, Path: "vendor/big.go"},
		},
	}
	// An allowlist that admits the record and not the vendored file, which is
	// the documented shape of the policy in §7.6.1.
	c, v, err := Screen(EgressPolicy{AllowPaths: []string{".gaos/**"}}, Target{Provider: "anthropic"}, pkt)
	if err != nil {
		t.Fatalf("the packet did not clear: %v", err)
	}
	if len(v.Redactions) != 1 {
		t.Fatalf("the pass cut %d part(s), want 1; the rest of this case depends on it having cut one",
			len(v.Redactions))
	}

	cites := c.Cites()
	if len(cites) != 1 {
		t.Fatalf("the cleared packet cites %d node(s), want 1; a citation whose part was redacted "+
			"claims the model was given something the pass refused to send", len(cites))
	}
	if cites[0].Path != ".gaos/packet/rules.md" {
		t.Errorf("the surviving citation names %q; the one that survived should be the one whose "+
			"part did", cites[0].Path)
	}
}

// TestTwoAttemptsGivenTheSameContextShareOneSlice is the identity rule. The
// digest is computed over the surviving parts in path order precisely so that
// "were these two runs given the same context" is a string comparison, which is
// what H-4's comparison scope will need.
func TestTwoAttemptsGivenTheSameContextShareOneSlice(t *testing.T) {
	s := NewMemStore(3)
	f, a := gatewayOn(t, s)
	deskID := s.desks[0].ID

	// The same brief twice: clearedFor builds both from one part list, and the
	// run id is not part of what the digest covers.
	for attempt := 1; attempt <= 2; attempt++ {
		completeTurn(f, "t-1", attempt, "0.1")
		if _, err := s.superviseRun(t.Context(), a, deskID, clearedFor(t, s, deskID, "t-1", attempt), attempt); err != nil {
			t.Fatal(err)
		}
	}

	s.mu.RLock()
	stored := len(s.slices)
	s.mu.RUnlock()
	if stored != 1 {
		t.Errorf("two attempts on identical context stored %d slices, want 1; storing it twice "+
			"makes comparing two runs a diff of two copies of one string", stored)
	}

	runs, _ := s.Runs(t.Context(), "t-1")
	if runs[0].SliceDigest != runs[1].SliceDigest {
		t.Errorf("the two attempts name different slices (%s, %s) for identical context",
			runs[0].SliceDigest, runs[1].SliceDigest)
	}
	s.mu.RLock()
	first := s.slices[runs[0].SliceDigest].Attempt
	s.mu.RUnlock()
	if first != 1 {
		t.Errorf("the shared slice records attempt %d; the first attempt is when this context "+
			"came into existence, and the second did not re-create it", first)
	}
}

// TestTaskContextAnswersFromTheFrozenSliceOnceSomethingHasRun is T-056: the
// route stops recomputing the answer the moment there is a real one.
func TestTaskContextAnswersFromTheFrozenSliceOnceSomethingHasRun(t *testing.T) {
	s := NewMemStore(3)
	f, a := gatewayOn(t, s)
	completeTurn(f, "t-1", 1, "0.1")
	deskID := s.desks[0].ID

	before, err := s.TaskContext(t.Context(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if before.Frozen {
		t.Fatal("a desk that has never run an attempt reported a frozen slice; there is no " +
			"evidence to serve and saying otherwise is the failure D-6 describes")
	}

	if _, err := s.superviseRun(t.Context(), a, deskID, clearedFor(t, s, deskID, "t-1", 1), 1); err != nil {
		t.Fatal(err)
	}

	after, err := s.TaskContext(t.Context(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if !after.Frozen {
		t.Fatal("an attempt has run and the route still computes a slice live; GET " +
			"/tasks/{id}/context has to match what the model actually received (D-6)")
	}
	runs, _ := s.Runs(t.Context(), "t-1")
	if after.Digest != runs[0].SliceDigest {
		t.Errorf("the route served digest %q and the attempt was given %q; two different answers "+
			"to one question", after.Digest, runs[0].SliceDigest)
	}
	if after.Attempt != 1 {
		t.Errorf("the route reports attempt %d, want 1", after.Attempt)
	}
}

// TestTheFrozenSliceDoesNotChangeWhenTheRecordDoes is why a frozen slice is
// worth storing at all. Edit the master afterwards and the evidence must not
// move -- otherwise the answer to "what was this attempt given" quietly becomes
// "whatever the record says now".
func TestTheFrozenSliceDoesNotChangeWhenTheRecordDoes(t *testing.T) {
	s := NewMemStore(3)
	f, a := gatewayOn(t, s)
	completeTurn(f, "t-1", 1, "0.1")
	deskID := s.desks[0].ID
	productID := s.desks[0].ID

	if _, err := s.superviseRun(t.Context(), a, deskID, clearedFor(t, s, deskID, "t-1", 1), 1); err != nil {
		t.Fatal(err)
	}
	frozen, _ := s.TaskContext(t.Context(), 0)

	if _, err := s.AddStory(t.Context(), productID, Story{
		Role: "operator", Want: "a story added after the attempt", So: "the record moved",
	}); err != nil {
		t.Fatalf("adding a story: %v", err)
	}

	after, _ := s.TaskContext(t.Context(), 0)
	if after.Digest != frozen.Digest {
		t.Errorf("the frozen slice's digest moved from %q to %q when the record was edited; a "+
			"slice that follows the record is not evidence of anything", frozen.Digest, after.Digest)
	}
	if len(after.Chips) != len(frozen.Chips) {
		t.Errorf("the frozen slice rendered %d chips and now renders %d; it is read from the "+
			"citations precisely so a later edit cannot reach it", len(frozen.Chips), len(after.Chips))
	}
}

// TestBootReconciliationClosesTheAttemptItRecovered is the recovery half of
// T-054. A restart that moved the task and left the attempt saying RUNNING
// would leave the graph asserting a turn that nobody believes is alive. The
// gateway no longer knows the request and its deadline has passed, so the
// attempt is closed as orphaned (D-060).
func TestBootReconciliationClosesTheAttemptItRecovered(t *testing.T) {
	s := NewMemStore(3)
	f, a := gatewayOn(t, s)
	// The deadline bounds the whole poll, including the HTTP call that brings
	// back the 404. At 1ms that call sometimes timed out first, read as a
	// transient gateway failure, and the run was finished rather than
	// orphaned (a flake, roughly 1 run in 6). The hold after "lost" is already
	// over without a tiny deadline: the run started 9000s ago.
	a.Deadline = time.Minute
	f.status, f.reply = http.StatusNotFound, `{"error":"unknown request"}`
	deskID := s.desks[0].ID

	taskID := "t-orphan"
	run := runIDFor(taskID, 1)
	s.mu.Lock()
	s.tasks = append(s.tasks, TaskRecord{
		ID: taskID, DeskID: deskID, WorkflowID: "wf-1",
		Title: "an abandoned task", Role: "backend", Status: StatusRunning,
	})
	s.runs = append(s.runs, TaskRunRecord{
		ID: run, TaskID: taskID, DeskID: deskID, Attempt: 1, Status: RunStatusRunning,
		RequestID: a.RequestIDFor(run), StartedAt: time.Now().Unix() - 9000,
	})
	s.mu.Unlock()

	if _, err := s.Reconcile(t.Context()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	waitForRun(t, s, run, RunStatusOrphaned)

	runs, _ := s.Runs(t.Context(), taskID)
	if len(runs) != 1 {
		t.Fatalf("recovery left %d record(s) for one attempt", len(runs))
	}
	if runs[0].Status != RunStatusOrphaned {
		t.Errorf("the recovered attempt is still %q, want %q; a run the reconciler gave up on "+
			"and left marked RUNNING is the graph asserting a process nobody believes in",
			runs[0].Status, RunStatusOrphaned)
	}
	if runs[0].EndedAt == 0 {
		t.Error("the orphaned attempt has no end time; a run that is over and has no end is one " +
			"every duration query will treat as still going")
	}
}

// TestTheFrozenSliceSurvivesARestart is the ArcadeDB half of T-055, and it has
// to be its own test for the same reason TestTheTaskGraphSurvivesARestart does:
// "restart the process" is not something the Store interface offers.
//
// It matters more here than for the task graph. A task graph that was lost
// could at least be re-planned; a context slice that was lost is the answer to
// "what was this attempt actually given", and there is no second source for it
// anywhere -- the record it was assembled from has moved on by definition.
func TestTheFrozenSliceSurvivesARestart(t *testing.T) {
	arc := arcadeTestTarget(t)
	ctx := t.Context()
	tenant := "conf-slice-restart"

	first := newArcadeTestStore(t, arc, tenant)
	idx := idleDeskIn(t, first)
	if _, err := first.DispatchTask(ctx, idx); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	before, err := first.TaskContext(ctx, idx)
	if err != nil {
		t.Fatal(err)
	}
	if !before.Frozen {
		t.Skip("this deployment's egress policy refused the dispatch; nothing was frozen to restart with")
	}

	second, err := NewArcadeStoreFor(ctx, arc, 1, tenant)
	if err != nil {
		t.Fatalf("reopen the store on the same tenant: %v", err)
	}
	after, err := second.TaskContext(ctx, idx)
	if err != nil {
		t.Fatal(err)
	}
	if !after.Frozen {
		t.Fatalf("the reopened store answers with a live assembly; the slice was written and "+
			"load() did not bring it back, so the process that could answer what digest %s "+
			"contained is gone", before.Digest)
	}
	if after.Digest != before.Digest {
		t.Errorf("the reopened store serves digest %q, %q was written", after.Digest, before.Digest)
	}
	if len(after.Cites) != len(before.Cites) {
		t.Errorf("the slice came back with %d citation(s), %d were written; USES_CONTEXT is an "+
			"edge, and edges that do not survive a restart are provenance nobody has",
			len(after.Cites), len(before.Cites))
	}
}

// TestAnAttemptSurvivesARestart is the ArcadeDB half of T-054. BR-22 makes each
// attempt its own record, and a record that only exists in the projection is
// not one -- the retry policy that reads a task's history after a restart would
// read an empty one and conclude nothing had ever been tried.
func TestAnAttemptSurvivesARestart(t *testing.T) {
	arc := arcadeTestTarget(t)
	ctx := t.Context()
	tenant := "conf-run-restart"

	first := newArcadeTestStore(t, arc, tenant)
	st, ok := first.(*ArcadeStore)
	if !ok {
		t.Fatalf("newArcadeTestStore returned %T, want *ArcadeStore", first)
	}
	fa, a := gatewayOn(t, st.proj)
	completeTurn(fa, "t-1", 1, "0.1")

	// Through the projection directly: the runner is driven by the scheduler in
	// a deployment, and this test is about what reaches the database, not about
	// what decides to run.
	f, err := first.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	deskID := f.Desks[0].ID
	if _, err := st.proj.superviseRun(ctx, a, deskID, clearedFor(t, st.proj, deskID, "t-1", 1), 1); err != nil {
		t.Fatalf("supervising one run: %v", err)
	}

	before, err := first.Runs(ctx, "t-1")
	if err != nil || len(before) != 1 {
		t.Fatalf("the run left %d record(s) in the projection (err %v)", len(before), err)
	}

	second, err := NewArcadeStoreFor(ctx, arc, 1, tenant)
	if err != nil {
		t.Fatalf("reopen the store on the same tenant: %v", err)
	}
	after, err := second.Runs(ctx, "t-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 1 {
		t.Fatalf("the reopened store holds %d attempt(s), one was written; a TaskRun that does "+
			"not survive a restart is an attempt the retry guard will never count", len(after))
	}
	got, want := after[0], before[0]
	if got.Status != want.Status || got.Attempt != want.Attempt || got.Model != want.Model {
		t.Errorf("the attempt came back as %s/%d/%s, was %s/%d/%s",
			got.Status, got.Attempt, got.Model, want.Status, want.Attempt, want.Model)
	}
	if got.CostUSD != want.CostUSD || got.CostBasis != want.CostBasis {
		t.Errorf("cost came back as %.4f (%s), was %.4f (%s); BR-31 keeps the basis attached to "+
			"the figure, and a basis lost in the write is a measured number that reads as a guess",
			got.CostUSD, got.CostBasis, want.CostUSD, want.CostBasis)
	}
	if got.SliceDigest != want.SliceDigest {
		t.Errorf("the attempt names slice %q, %q was written; the ASSEMBLED edge is how an "+
			"auditor gets from an attempt to what it was given", got.SliceDigest, want.SliceDigest)
	}
}

// TestTheReadOnlyHandleStillAnswersRunsAndContext keeps BR-20 honest in the
// direction it is easy to get wrong. Replay refuses mutations; it must not
// refuse to be read from, because looking at a past moment is the whole purpose
// of the handle.
func TestTheReadOnlyHandleStillAnswersRunsAndContext(t *testing.T) {
	s := NewMemStore(3)
	ro := readOnly{s}

	if _, err := ro.TaskContext(t.Context(), 0); err != nil {
		t.Errorf("the read-only handle refused TaskContext: %v; a handle that cannot read is "+
			"not a replay handle", err)
	}
	if _, err := ro.Runs(t.Context(), "t-1"); err != nil {
		t.Errorf("the read-only handle refused Runs: %v", err)
	}
	if _, err := ro.DispatchTask(t.Context(), 0); err == nil {
		t.Error("the read-only handle dispatched a task; the floor is in replay and BR-20 makes " +
			"that structurally impossible rather than a check somebody remembers")
	}
}
