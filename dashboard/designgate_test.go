package main

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"
)

// The design gate: M17, T-184…T-188, D-068 and D-069.
//
// The claim under test is the one the whole change exists for: a platform-run
// reviewer is isolated by the PACKET, not by the workspace, and the
// implementation does not start until that reviewer resolves.
//
// Every test here is a top-level func Test…, and each has a row in the QA
// register (pustaka/docs/product/qa-test-plan.html#ni-egm).

// fakeTurnAdapter is the smallest thing that satisfies TurnAdapter, so the
// session logic can be exercised without a gateway.
type fakeTurnAdapter struct{}

func (fakeTurnAdapter) RequestIDFor(runID string) string { return "req-" + runID }
func (fakeTurnAdapter) ConfiguredModel() string          { return "claude-sonnet-5" }
func (fakeTurnAdapter) TurnDeadline() time.Duration      { return time.Minute }
func (fakeTurnAdapter) SessionMaxAge() time.Duration     { return 25 * 24 * time.Hour }

// ---------------------------------------------------------------- the fixture

// gateFixture is a store with one product, one story, and a created workflow
// whose plan carries the gate.
type gateFixture struct {
	s      *MemStore
	desk   string
	plan   []PlanTask
	byKind map[string]string // kind -> task id, for the first of each kind
}

func gateFixtureNew(t *testing.T) gateFixture {
	t.Helper()
	ctx := context.Background()
	s := NewMemStore(20261104)
	if _, err := s.CreateProduct(ctx, ProductDraft{Name: "Gate", Owner: "ardi", Goal: "hold the gate"}); err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}
	masters, err := s.Masters(ctx)
	if err != nil || len(masters) == 0 {
		t.Fatalf("Masters: %v", err)
	}
	id := masters[0].ID
	if _, err := s.AddStory(ctx, id, Story{
		ID: "S-1", Pri: "must", Role: "backend", Want: "the gate holds",
		So: "the implementation waits for the review", AC: []string{"AC-1 the implementation waits"},
	}); err != nil {
		t.Fatalf("AddStory: %v", err)
	}
	draft := WorkflowDraft{ProductID: id, Goal: "hold the gate", Stories: []string{"S-1"}}
	plan, err := s.PlanWorkflow(ctx, draft)
	if err != nil {
		t.Fatalf("PlanWorkflow: %v", err)
	}
	if _, err := s.CreateWorkflow(ctx, draft, plan); err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	f := gateFixture{s: s, desk: id, plan: plan, byKind: map[string]string{}}
	// The store QUALIFIES the plan's local ids (qualify(), graph.go), so the
	// ids the store holds are not the ids the plan carries.
	//
	// And "the implementation" is the STORY's implementation -- the one with
	// the backend role -- not the planner, which is also of kind implement and
	// is not gated.
	s.mu.RLock()
	for i := range s.tasks {
		t := &s.tasks[i]
		kind := taskKind(t.Kind)
		if kind == TaskKindImplement && t.Role != "backend" {
			continue
		}
		if _, seen := f.byKind[kind]; !seen {
			f.byKind[kind] = t.ID
		}
	}
	s.mu.RUnlock()
	return f
}

// taskOfKind is the task id of one kind in the fixture's plan.
func (f gateFixture) taskOfKind(t *testing.T, kind string) string {
	t.Helper()
	id, ok := f.byKind[kind]
	if !ok {
		t.Fatalf("the plan holds no task of kind %q", kind)
	}
	return id
}

// taskRecord is one task, by id.
func (f gateFixture) taskRecord(t *testing.T, id string) TaskRecord {
	t.Helper()
	f.s.mu.RLock()
	defer f.s.mu.RUnlock()
	for i := range f.s.tasks {
		if f.s.tasks[i].ID == id {
			return f.s.tasks[i]
		}
	}
	t.Fatalf("no task %q", id)
	return TaskRecord{}
}

// packetFor freezes the packet of one task.
func (f gateFixture) packetFor(t *testing.T, id string) Packet {
	t.Helper()
	f.s.mu.RLock()
	defer f.s.mu.RUnlock()
	idx := f.s.deskIdxByID(f.desk)
	if idx < 0 {
		t.Fatalf("no desk %q", f.desk)
	}
	return f.s.freezePacketFor(f.s.desks[idx], id, 1)
}

// partPaths is the paths of a packet's parts, in order.
func partPaths(p Packet) []string {
	var out []string
	for _, part := range p.Parts {
		out = append(out, part.Path)
	}
	return out
}

// promptParts is the parts a packet's prompt actually carries: RenderPrompt
// drops the contract and the handoff part, because the contract is re-rendered
// at the end of the prompt and the handoff is where the model writes.
func promptParts(p Packet) []PacketPart {
	var out []PacketPart
	for _, part := range p.Parts {
		if part.Kind != KindContract && part.Kind != KindHandoff {
			out = append(out, part)
		}
	}
	return out
}

// partOf is one part of a packet, by path.
func partOf(p Packet, path string) (PacketPart, bool) {
	for _, part := range p.Parts {
		if part.Path == path {
			return part, true
		}
	}
	return PacketPart{}, false
}

// reviewReply builds a reply that carries a review block and a handoff.
func reviewReply(taskID, runID string, findings ...string) string {
	body := ""
	if len(findings) == 0 {
		body = "no-findings: true\nfindings:"
	} else {
		body = "findings:\n" + strings.Join(findings, "\n")
	}
	return "the review\n\n" + fence + ReviewFence + "\n" +
		"---\ntask: " + taskID + "\nrun: " + runID + "\nrole: qa\n" + body + "\n---\n" + fence +
		"\n\n" + fence + HandoffFence + "\n" +
		handoffExample(taskID, runID, "qa") + "\n" + fence + "\n"
}

// designReply builds a reply that carries a design block and a handoff.
func designReply(taskID, runID, document string) string {
	return "the design\n\n" + fence + DesignFence + "\n" +
		"---\ntask: " + taskID + "\nrun: " + runID + "\nrole: architect\n---\n" + document + "\n" + fence +
		"\n\n" + fence + HandoffFence + "\n" +
		handoffExample(taskID, runID, "architect") + "\n" + fence + "\n"
}

// finish drives one run's end, which is where the gate's hook lives.
func (f gateFixture) finish(t *testing.T, taskID, runID, reply string) {
	t.Helper()
	f.s.mu.Lock()
	f.s.startRun(TaskRunRecord{ID: runID, TaskID: taskID, DeskID: f.desk, Attempt: 1,
		Status: RunStatusRunning, StartedAt: time.Now().Unix()})
	f.s.mu.Unlock()
	// The states a real run goes through: PENDING -> READY -> DISPATCHED ->
	// RUNNING. The gate's move comes out of RUNNING, so the task has to be
	// there.
	f.s.moveTaskByID(taskID, StatusReady, MoveSchedule)
	f.s.moveTaskByID(taskID, StatusDispatched, MoveSchedule)
	f.s.moveTaskByID(taskID, StatusRunning, MoveStart)
	f.s.finishRun(f.desk, taskID, runID, Attempt{Reply: reply, ExitClass: ExitRan, HandoffRaw: reply}, nil)
}

// ------------------------------------------------------------------ the tests

// TestTheReviewPacketCarriesTheDesignAndTheRulesAndNothingElse is the
// isolation claim, in one assertion: a reviewer's packet holds the design and
// the rubric, and none of the author's session state.
func TestTheReviewPacketCarriesTheDesignAndTheRulesAndNothingElse(t *testing.T) {
	f := gateFixtureNew(t)
	designID := f.taskOfKind(t, TaskKindDesign)
	reviewID := f.taskOfKind(t, TaskKindReview)

	// The design task produced its document.
	f.finish(t, designID, designID+"-a1", designReply(designID, designID+"-a1", "# The design\n\nthe shape of it"))

	pkt := f.packetFor(t, reviewID)

	// What the reviewer READS is what RenderPrompt keeps: the contract part is
	// the evidence of what the model was told, and RenderPrompt drops it and
	// re-renders the contract. So the isolation claim is about the prompt.
	read := promptParts(pkt)
	kinds := map[string]bool{}
	for _, part := range read {
		kinds[part.Kind] = true
	}
	if len(read) != 2 {
		t.Fatalf("the reviewer's prompt carries %d parts (%v); it must carry the design and "+
			"the rubric and nothing else (D-069 clause 3)", len(read), partPaths(pkt))
	}
	if !kinds[KindDesign] || !kinds[KindRules] {
		t.Errorf("the reviewer's prompt carries kinds %v; it must carry %q and %q",
			kinds, KindDesign, KindRules)
	}
	for _, path := range []string{
		".gaos/packet/task.md",
		".gaos/packet/context/answer.md",
		".gaos/packet/context/handoff-previous.md",
		".gaos/packet/context/failure-previous.md",
		".gaos/packet/context/record.md",
	} {
		if _, ok := partOf(pkt, path); ok {
			t.Errorf("the review packet carries %s; the author's session state must not reach "+
				"the reviewer (D-069 clause 3)", path)
		}
	}
	// The contract part is the evidence of what the reviewer was told, and it
	// is not the author's session state -- but it must not reach the prompt.
	if c, ok := partOf(pkt, ".gaos/packet/handoff.md"); ok {
		for _, part := range read {
			if part.Path == c.Path {
				t.Errorf("the contract part reaches the reviewer's prompt; it is the evidence " +
					"of what the model was told, not something to read")
			}
		}
	}
	design, ok := partOf(pkt, ".gaos/packet/design.md")
	if !ok {
		t.Fatal("the review packet carries no design")
	}
	if !strings.Contains(design.Body, "the shape of it") {
		t.Errorf("the design part does not carry the document verbatim: %q", design.Body)
	}

	// And the reviewer READS it. A part that is in the packet but not in the
	// prompt's kind order is a part the model never sees, and the frozen slice
	// would be lying about what was sent.
	// Through the real path: the egress pass is the only thing that may build
	// a Cleared (BR-32), so the prompt is read off one it produced.
	c, _, err := f.s.clearFor(context.Background(), Dispatch{TaskID: reviewID, DeskID: f.desk}, 1)
	if err != nil {
		t.Fatalf("the review's packet did not clear the egress pass: %v", err)
	}
	prompt := RenderPrompt(c)
	if !strings.Contains(prompt, "the shape of it") {
		t.Errorf("the reviewer's prompt does not carry the design; the packet holds it and "+
			"the prompt drops it\n--- prompt ---\n%s", prompt)
	}
	for _, path := range []string{".gaos/packet/task.md", ".gaos/packet/context/record.md"} {
		if strings.Contains(prompt, path) {
			t.Errorf("the reviewer's prompt carries %s; it must carry the design and the rubric "+
				"and nothing else", path)
		}
	}
}

// TestAnImplementPacketCarriesTheDesignAndTheFindings is the findings' return
// path: the implementer sees the design it is implementing and the review
// that was made about it.
func TestAnImplementPacketCarriesTheDesignAndTheFindings(t *testing.T) {
	f := gateFixtureNew(t)
	designID := f.taskOfKind(t, TaskKindDesign)
	reviewID := f.taskOfKind(t, TaskKindReview)
	implID := f.taskOfKind(t, TaskKindImplement)

	f.finish(t, designID, designID+"-a1", designReply(designID, designID+"-a1", "# The design\n\nthe shape of it"))
	f.finish(t, reviewID, reviewID+"-a1", reviewReply(reviewID, reviewID+"-a1",
		`- severity: major   scope: "src/thing.go:12"   note: "the order is not stated"`))

	pkt := f.packetFor(t, implID)
	design, ok := partOf(pkt, ".gaos/packet/design.md")
	if !ok {
		t.Errorf("the implement packet carries no design; the implementer cannot build what it "+
			"cannot read (%v)", partPaths(pkt))
	} else if !strings.Contains(design.Body, "the shape of it") {
		t.Errorf("the design part does not carry the document verbatim: %q", design.Body)
	}
	findings, ok := partOf(pkt, ".gaos/packet/context/findings.md")
	if !ok {
		t.Fatalf("the implement packet carries no findings (%v); the findings must travel back "+
			"(D-069 clause 6)", partPaths(pkt))
	}
	if findings.Kind != KindContext {
		t.Errorf("the findings part is of kind %q; the findings travel as context", findings.Kind)
	}
	if !strings.Contains(findings.Body, "src/thing.go:12") {
		t.Errorf("the findings part does not carry the finding: %q", findings.Body)
	}
}

// TestThePlanGivesEveryStoryADesignTaskAndAReviewTask is the plan's shape.
func TestThePlanGivesEveryStoryADesignTaskAndAReviewTask(t *testing.T) {
	f := gateFixtureNew(t)
	kinds := map[string]int{}
	for _, pt := range f.plan {
		kinds[pt.Kind]++
	}
	if kinds[TaskKindDesign] < 1 || kinds[TaskKindReview] != 1 {
		t.Errorf("the plan holds %d design tasks and %d review tasks; a story is designed, "+
			"reviewed and then implemented", kinds[TaskKindDesign], kinds[TaskKindReview])
	}
	// The implementation depends on the REVIEW, and that edge is the gate.
	implID := f.taskOfKind(t, TaskKindImplement)
	reviewID := f.taskOfKind(t, TaskKindReview)
	impl := f.taskRecord(t, implID)
	found := false
	for _, dep := range impl.Deps {
		if dep == reviewID {
			found = true
		}
	}
	if !found {
		t.Errorf("the implementation %s depends on %v; it must depend on the review %s, "+
			"because that edge is the gate (D-069 clause 7)", implID, impl.Deps, reviewID)
	}
}

// TestAPlanWithAnImplementationAndNoReviewIsRefused is BR-46's enforcement
// point. The plan is an input: CreateWorkflow is public, so the check cannot
// live in the planner.
func TestAPlanWithAnImplementationAndNoReviewIsRefused(t *testing.T) {
	plan := []PlanTask{
		{ID: "t-1", Title: "plan", Role: "planner", Kind: TaskKindImplement},
		{ID: "t-2", Title: "build", Role: "backend", Kind: TaskKindImplement, Deps: []string{"t-1"}},
	}
	err := validatePlan(plan)
	if err == nil {
		t.Fatal("a plan whose implementation has no review was accepted; BR-46 says every " +
			"implementation is reviewed before it runs, and the plan is an input")
	}
	if !strings.Contains(err.Error(), "review") {
		t.Errorf("the refusal says %q; it must name what is missing", err.Error())
	}
}

// TestTheImplementationTaskIsBlockedWhileTheReviewIsUnreviewed is the gate's
// reporting half: the card says why it is waiting.
func TestTheImplementationTaskIsBlockedWhileTheReviewIsUnreviewed(t *testing.T) {
	f := gateFixtureNew(t)
	implID := f.taskOfKind(t, TaskKindImplement)
	impl := f.taskRecord(t, implID)
	if impl.Status != StatusBlocked {
		t.Errorf("the implementation is %q; a task waiting for a review is BLOCKED", impl.Status)
	}
	if impl.BlockReason != ReasonDesignUnreviewed {
		t.Errorf("the implementation's reason is %q; it must be %q", impl.BlockReason, ReasonDesignUnreviewed)
	}
}

// TestTheDesignArtifactExistsWithKindDesignAndIsAttachedToItsTask is the
// design artifact's half: it exists, it is of the right kind, it is attached to
// the task that wrote it, and a design quoting a reserved contract is refused.
func TestTheDesignArtifactExistsWithKindDesignAndIsAttachedToItsTask(t *testing.T) {
	f := gateFixtureNew(t)
	designID := f.taskOfKind(t, TaskKindDesign)
	f.finish(t, designID, designID+"-a1", designReply(designID, designID+"-a1", "# The design\n\nthe shape of it"))

	f.s.mu.RLock()
	art, ok := f.s.designArtifactFor(designID)
	f.s.mu.RUnlock()
	if !ok {
		t.Fatal("no design artifact was recorded for the design task")
	}
	if art.Kind != ArtifactDesign {
		t.Errorf("the design artifact is of kind %q; it must be %q", art.Kind, ArtifactDesign)
	}
	if art.TaskID != designID {
		t.Errorf("the design artifact is attached to %q; it must be attached to the task that wrote it", art.TaskID)
	}
	if !strings.Contains(art.Body, "the shape of it") {
		t.Errorf("the design artifact does not carry the document: %q", art.Body)
	}

	// A design that quotes a contract example would nest a reserved block in
	// the reply, and the last block of that tag would win.
	quoted := "# The design\n\n" + fence + HandoffFence + "\n---\ntask: x\n---\n" + fence
	if _, err := ParseDesign([]byte("---\ntask: t-1\nrun: t-1-a1\nrole: architect\n---\n" + quoted)); err == nil {
		t.Error("a design document carrying a reserved contract block was accepted; it would " +
			"put a second block of that tag in the reply")
	}
}

// TestAReviewWithAnOpenBlockerHoldsTheImplementation is the gate's whole
// point: a Blocker stops the implementation.
func TestAReviewWithAnOpenBlockerHoldsTheImplementation(t *testing.T) {
	f := gateFixtureNew(t)
	designID := f.taskOfKind(t, TaskKindDesign)
	reviewID := f.taskOfKind(t, TaskKindReview)
	implID := f.taskOfKind(t, TaskKindImplement)

	f.finish(t, designID, designID+"-a1", designReply(designID, designID+"-a1", "# The design"))
	f.finish(t, reviewID, reviewID+"-a1", reviewReply(reviewID, reviewID+"-a1",
		`- severity: blocker   scope: "src/thing.go:12"   note: "the order is not stated"`))

	review := f.taskRecord(t, reviewID)
	if review.Status != StatusBlocked {
		t.Errorf("the review task is %q; an open Blocker leaves it BLOCKED", review.Status)
	}
	if review.BlockReason != ReasonDesignBlocked {
		t.Errorf("the review task's reason is %q; it must be %q", review.BlockReason, ReasonDesignBlocked)
	}
	impl := f.taskRecord(t, implID)
	if impl.Status != StatusBlocked {
		t.Errorf("the implementation is %q; an open Blocker holds it", impl.Status)
	}
	if impl.BlockReason != ReasonDesignBlocked {
		t.Errorf("the implementation's reason is %q; it must be %q", impl.BlockReason, ReasonDesignBlocked)
	}
}

// TestAReviewWithNoOpenBlockerClearsTheImplementationExactlyOnce is the happy
// path, and it is checked twice so a second clear cannot slip through.
func TestAReviewWithNoOpenBlockerClearsTheImplementationExactlyOnce(t *testing.T) {
	f := gateFixtureNew(t)
	designID := f.taskOfKind(t, TaskKindDesign)
	reviewID := f.taskOfKind(t, TaskKindReview)
	implID := f.taskOfKind(t, TaskKindImplement)

	f.finish(t, designID, designID+"-a1", designReply(designID, designID+"-a1", "# The design"))
	f.finish(t, reviewID, reviewID+"-a1", reviewReply(reviewID, reviewID+"-a1"))

	review := f.taskRecord(t, reviewID)
	if review.Status != StatusDone {
		t.Errorf("the review task is %q; a review with no open Blocker is DONE", review.Status)
	}
	impl := f.taskRecord(t, implID)
	if impl.Status != StatusReady {
		t.Errorf("the implementation is %q; a cleared review releases it to READY", impl.Status)
	}
	if impl.BlockReason != "" {
		t.Errorf("the implementation still carries the reason %q after it was released", impl.BlockReason)
	}

	// The record the gate wrote.
	f.s.mu.RLock()
	art, ok := f.s.reviewArtifactFor(reviewID)
	f.s.mu.RUnlock()
	if !ok {
		t.Fatal("the gate recorded no review artifact")
	}
	if !strings.Contains(art.Body, "verdict: "+VerdictAccept) {
		t.Errorf("the review record does not carry the derived verdict: %q", art.Body)
	}
}

// TestAReviewWithNoReviewBlockIsAFailureAndNotAVerdict is D-068 clause 7: a
// failed review is not a verdict, and it consumes none of the attempt-scoped
// limits.
func TestAReviewWithNoReviewBlockIsAFailureAndNotAVerdict(t *testing.T) {
	f := gateFixtureNew(t)
	designID := f.taskOfKind(t, TaskKindDesign)
	reviewID := f.taskOfKind(t, TaskKindReview)
	implID := f.taskOfKind(t, TaskKindImplement)

	f.finish(t, designID, designID+"-a1", designReply(designID, designID+"-a1", "# The design"))
	// A reply with a handoff but no review block.
	f.finish(t, reviewID, reviewID+"-a1", "I could not form a judgement.\n\n"+fence+HandoffFence+"\n"+
		handoffExample(reviewID, reviewID+"-a1", "qa")+"\n"+fence+"\n")

	review := f.taskRecord(t, reviewID)
	if review.Status != StatusBlocked {
		t.Errorf("the review task is %q; a review that produced no verdict is BLOCKED", review.Status)
	}
	if review.BlockReason != ReasonReviewFailed {
		t.Errorf("the review task's reason is %q; it must be %q", review.BlockReason, ReasonReviewFailed)
	}
	if review.Retry != 0 || review.Esc != 0 {
		t.Errorf("the failed review consumed a retry (%d) or an escalation (%d); a failed review "+
			"consumes none of the three attempt-scoped limits (D-068 clause 7)", review.Retry, review.Esc)
	}
	impl := f.taskRecord(t, implID)
	if impl.Status != StatusBlocked {
		t.Errorf("the implementation is %q; a review that failed holds it", impl.Status)
	}
}

// TestTheDesignContractAndTheReviewContractParse keeps the two contracts and
// their parsers honest about each other, the way the handoff's own example
// test does.
func TestTheDesignContractAndTheReviewContractParse(t *testing.T) {
	d, err := ExtractDesign(designReply("t-9", "t-9-a1", "# The design"), "t-9", "t-9-a1")
	if err != nil {
		t.Fatalf("the design contract's own example does not parse: %v", err)
	}
	if d.Task != "t-9" || d.Run != "t-9-a1" || !strings.Contains(d.Document, "# The design") {
		t.Errorf("the design example parsed to task %q run %q document %q", d.Task, d.Run, d.Document)
	}
	r, err := ExtractReview(reviewReply("t-9", "t-9-a1",
		`- severity: blocker   scope: "src/thing.go:12"   note: "the order is not stated"`), "t-9", "t-9-a1")
	if err != nil {
		t.Fatalf("the review contract's own example does not parse: %v", err)
	}
	if r.OpenBlockers() != 1 || r.Verdict() != VerdictIterate {
		t.Errorf("the review example parsed to %d blockers and the verdict %q", r.OpenBlockers(), r.Verdict())
	}

	// A malformed finding is refused.
	bad := "---\ntask: t-9\nrun: t-9-a1\nrole: qa\nfindings:\n  - severity: catastrophic   scope: \"a.go:1\"   note: \"x\"\n---\n"
	if _, err := ParseReview([]byte(bad)); err == nil {
		t.Error("a finding with a severity outside the vocabulary was accepted")
	}
	// A scope carrying prose is refused.
	bad = "---\ntask: t-9\nrun: t-9-a1\nrole: qa\nfindings:\n  - severity: major   scope: \"the rounding is wrong\"   note: \"x\"\n---\n"
	if _, err := ParseReview([]byte(bad)); err == nil {
		t.Error("a scope carrying prose was accepted; the scope is persisted and forwarded")
	}
	// An empty findings list must say it is empty.
	empty := "---\ntask: t-9\nrun: t-9-a1\nrole: qa\nfindings:\n---\n"
	if _, err := ParseReview([]byte(empty)); err == nil {
		t.Error("an empty findings list without no-findings was accepted; a review that found " +
			"nothing and a review that was never finished must not look the same")
	}
}

// TestAReviewTaskRunsOnAFreshSessionWithTheReviewerReason is the isolation's
// transport half: the reviewer is not the author's session.
func TestAReviewTaskRunsOnAFreshSessionWithTheReviewerReason(t *testing.T) {
	f := gateFixtureNew(t)
	reviewID := f.taskOfKind(t, TaskKindReview)

	f.s.mu.RLock()
	sess, reason := f.s.sessionForLocked(reviewID, fakeTurnAdapter{}, "", "", time.Now().Unix())
	f.s.mu.RUnlock()

	if sess != "" {
		t.Errorf("the review task resumed session %q; a reviewer must open a fresh one", sess)
	}
	if reason != SessionReviewer {
		t.Errorf("the review task's session reason is %q; it must be %q", reason, SessionReviewer)
	}
}

// TestAReviewTurnIsMeteredLikeAnyOtherTurn is T-188's budget half: a review
// turn is not exempt from the cost the product accumulates.
func TestAReviewTurnIsMeteredLikeAnyOtherTurn(t *testing.T) {
	f := gateFixtureNew(t)
	reviewID := f.taskOfKind(t, TaskKindReview)

	f.s.mu.Lock()
	f.s.startRun(TaskRunRecord{ID: reviewID + "-a1", TaskID: reviewID, DeskID: f.desk, Attempt: 1,
		Status: RunStatusRunning, StartedAt: time.Now().Unix()})
	f.s.mu.Unlock()
	f.s.finishRun(f.desk, reviewID, reviewID+"-a1",
		Attempt{Reply: reviewReply(reviewID, reviewID+"-a1"), ExitClass: ExitRan, CostUSD: 0.25, CostBasis: CostMeasured}, nil)

	f.s.mu.RLock()
	deskCost := f.s.desks[f.s.deskIdxByID(f.desk)].Cost
	spend := f.s.spend
	f.s.mu.RUnlock()
	if deskCost < 0.25 {
		t.Errorf("the desk's cost is %v after a review turn that cost 0.25; a review turn is "+
			"metered like any other", deskCost)
	}
	if spend < 0.25 {
		t.Errorf("the tenant's spend is %v after a review turn that cost 0.25", spend)
	}
}

// TestTheDesignDriftReportNamesADesignWrittenUnderAnOlderBlueprint is T-188's
// drift half: a design written under a superseded Blueprint is reported.
func TestTheDesignDriftReportNamesADesignWrittenUnderAnOlderBlueprint(t *testing.T) {
	f := gateFixtureNew(t)
	designID := f.taskOfKind(t, TaskKindDesign)
	reviewID := f.taskOfKind(t, TaskKindReview)

	// The design was written under Blueprint version 3.
	f.s.mu.Lock()
	f.s.startRun(TaskRunRecord{ID: designID + "-a1", TaskID: designID, DeskID: f.desk, Attempt: 1,
		Status: RunStatusRunning, StartedAt: time.Now().Unix(), BlueprintVersion: 3})
	f.s.mu.Unlock()
	f.finish(t, designID, designID+"-a1", designReply(designID, designID+"-a1", "# The design"))

	// The review runs under version 7.
	f.s.mu.Lock()
	f.s.startRun(TaskRunRecord{ID: reviewID + "-a1", TaskID: reviewID, DeskID: f.desk, Attempt: 1,
		Status: RunStatusRunning, StartedAt: time.Now().Unix(), BlueprintVersion: 7})
	f.s.mu.Unlock()
	f.finish(t, reviewID, reviewID+"-a1", reviewReply(reviewID, reviewID+"-a1"))

	f.s.mu.RLock()
	art, ok := f.s.reviewArtifactFor(reviewID)
	f.s.mu.RUnlock()
	if !ok {
		t.Fatal("the gate recorded no review artifact")
	}
	if !strings.Contains(art.Body, "drift:") {
		t.Errorf("the review record does not report the drift: %q", art.Body)
	}
	if !strings.Contains(art.Body, "version 3") || !strings.Contains(art.Body, "version 7") {
		t.Errorf("the drift line does not name both versions: %q", art.Body)
	}
}

// TestTheClosedVocabulariesMatchTheDDL holds the three vocabularies D-069
// clause 9 grows against the DDL that admits them, in both directions.
func TestTheClosedVocabulariesMatchTheDDL(t *testing.T) {
	files := schemaSQL(t)

	t.Run("task kind", func(t *testing.T) {
		m := regexpTaskProp(files, "14-design-gate.sql", `(?i)Task\.kind[^;]*regexp '([^']+)'`)
		if m == "" {
			t.Fatal("14-design-gate.sql declares no Task.kind regexp")
		}
		re := regexp.MustCompile(m)
		for _, kind := range []string{TaskKindImplement, TaskKindDesign, TaskKindReview} {
			if !re.MatchString(kind) {
				t.Errorf("Task.kind's regexp %q does not admit %q", m, kind)
			}
		}
		if knownTaskKind("review-x") {
			t.Error("an unknown kind was accepted; the vocabulary is closed")
		}
		// And the other direction: a value outside the vocabulary must not
		// satisfy the declared regexp, or the database and the Go side could
		// disagree about what the three names are. This is what the anchoring
		// is for.
		for _, outside := range []string{"review-x", "implementing", "", "IMPLEMENT"} {
			if re.MatchString(outside) {
				t.Errorf("Task.kind's regexp %q accepts %q, which is not one of the three kinds",
					m, outside)
			}
		}
		assertSameSet(t, "Task.kind", m, []string{TaskKindImplement, TaskKindDesign, TaskKindReview})
	})

	t.Run("artifact kind", func(t *testing.T) {
		m := regexpTaskProp(files, "14-design-gate.sql", `(?i)Artifact\.kind[^;]*regexp '([^']+)'`)
		if m == "" {
			t.Fatal("14-design-gate.sql declares no Artifact.kind regexp")
		}
		re := regexp.MustCompile(m)
		for _, kind := range []string{ArtifactHandoff, ArtifactPrompt, ArtifactReply, ArtifactQuestion, ArtifactDesign, ArtifactReview} {
			if !re.MatchString(kind) {
				t.Errorf("Artifact.kind's regexp %q does not admit %q", m, kind)
			}
		}
		// The CREATE and the newest ALTER must agree, or a fresh database and
		// an old one accept different kinds.
		create := regexpTaskProp(files, "02-master.sql", `(?i)Artifact\.kind[^;]*regexp '([^']+)'`)
		if create != m {
			t.Errorf("02-master.sql creates Artifact.kind as %q but 14-design-gate.sql widens it "+
				"to %q; a fresh database and an old one would accept different kinds", create, m)
		}
		assertSameSet(t, "Artifact.kind", m, []string{
			ArtifactSequence, ArtifactERD, ArtifactFlow, ArtifactDiff, ArtifactCommit,
			ArtifactReport, ArtifactLog, ArtifactUpload, ArtifactHandoff, ArtifactPrompt,
			ArtifactReply, ArtifactQuestion, ArtifactDesign, ArtifactReview})
	})

	t.Run("citation role", func(t *testing.T) {
		m := regexpTaskProp(files, "14-design-gate.sql", `(?i)USES_CONTEXT\.role[^;]*regexp '([^']+)'`)
		if m == "" {
			t.Fatal("14-design-gate.sql declares no USES_CONTEXT.role regexp")
		}
		for _, role := range []string{CiteRequirement, CiteRule, CiteBoundary, CiteStory, CiteCriterion,
			CiteDiagram, CiteDecision, CiteLesson, CiteCode, CiteDiff, CitePolicy, CiteDesign} {
			if !regexp.MustCompile(m).MatchString(role) {
				t.Errorf("USES_CONTEXT.role's regexp %q does not admit %q", m, role)
			}
		}
		create := regexpTaskProp(files, "08-edges.sql", `(?i)USES_CONTEXT\.role[^;]*regexp '([^']+)'`)
		if create != m {
			t.Errorf("08-edges.sql creates USES_CONTEXT.role as %q but 14-design-gate.sql widens "+
				"it to %q; a fresh database and an old one would accept different roles", create, m)
		}
		assertSameSet(t, "USES_CONTEXT.role", m, []string{
			CiteRequirement, CiteRule, CiteBoundary, CiteStory, CiteCriterion, CiteDiagram,
			CiteDecision, CiteLesson, CiteCode, CiteDiff, CitePolicy, CiteDesign})
	})
}

// regexpAlternatives is the names a declared regexp admits: the anchors and
// the grouping stripped, then split on the alternation.
func regexpAlternatives(pattern string) []string {
	s := strings.TrimPrefix(pattern, "^")
	s = strings.TrimSuffix(s, "$")
	s = strings.TrimPrefix(s, "(")
	s = strings.TrimSuffix(s, ")")
	return strings.Split(s, "|")
}

// assertSameSet holds the DDL's vocabulary and the Go one to each other as
// SETS, which is what the house pattern does for Task.status: a name the DDL
// admits and Go does not declare is a name nothing can write, and a name Go
// declares and the DDL does not admit is a row ArcadeDB refuses at commit.
func assertSameSet(t *testing.T, what, pattern string, declared []string) {
	t.Helper()
	want := map[string]bool{}
	for _, d := range declared {
		want[d] = true
	}
	got := map[string]bool{}
	for _, alt := range regexpAlternatives(pattern) {
		got[alt] = true
	}
	for name := range want {
		if !got[name] {
			t.Errorf("%s declares %q, and the DDL's regexp %q does not admit it; a row the "+
				"database refuses is a row that fails at commit and in no test", what, name, pattern)
		}
	}
	for name := range got {
		if !want[name] {
			t.Errorf("the DDL's regexp %q admits %q, and %s declares no such value; a name "+
				"nothing can write is a name with no writer", pattern, name, what)
		}
	}
}

// regexpTaskProp reads the first regexp out of a schema file.
func regexpTaskProp(files map[string]string, file, pattern string) string {
	sql := files[file]
	if sql == "" {
		return ""
	}
	m := regexp.MustCompile(pattern).FindStringSubmatch(sql)
	if m == nil {
		return ""
	}
	return m[1]
}

// TestAReviewDispatchDoesNotTripTheDriftGate is TC-365: the reviewer's
// workspace holds the published Blueprint whole, so a review is not a drift.
func TestAReviewDispatchDoesNotTripTheDriftGate(t *testing.T) {
	f := gateFixtureNew(t)
	reviewID := f.taskOfKind(t, TaskKindReview)

	// The drift gate compares the published version with the workspace's
	// digest. A review changes nothing in the workspace, so the gate must
	// find no drift after one.
	f.s.mu.RLock()
	_, drift := f.s.blueprintGate(f.desk)
	f.s.mu.RUnlock()
	if drift != nil {
		t.Errorf("the reviewer's workspace already drifts: %s", drift.Reason)
	}
	f.s.mu.RLock()
	review := f.s.taskByIDLocked(reviewID)
	kind := ""
	if review != nil {
		kind = taskKind(review.Kind)
	}
	f.s.mu.RUnlock()
	if kind != TaskKindReview {
		t.Errorf("the task %q is of kind %q; the drift gate and the review must agree on which "+
			"task is the reviewer's", reviewID, kind)
	}
}

// TestTheOperatorDispatchRouteRefusesAnUnmetDependency is the gate's second
// door. The board's dispatch route applies the lease, the drift gate and the
// egress pass, and until M17 no dependency check at all.
func TestTheOperatorDispatchRouteRefusesAnUnmetDependency(t *testing.T) {
	f := gateFixtureNew(t)
	implID := f.taskOfKind(t, TaskKindImplement)

	// The implementation waits for a review that has not resolved.
	f.s.mu.RLock()
	waiting := f.s.unmetDepsLocked(implID)
	f.s.mu.RUnlock()
	if len(waiting) == 0 {
		t.Fatal("the implementation has no unmet prerequisite; the fixture is wrong")
	}

	// The board must refuse to unblock it by hand.
	f.s.mu.RLock()
	impl := f.s.taskByIDLocked(implID)
	gated, review := f.s.gateHoldsLocked(impl)
	f.s.mu.RUnlock()
	if !gated {
		t.Errorf("the board may unblock %s by hand while %s has not resolved", implID, review)
	}
}

// TestTheEventCatalogIsUnchanged is BR-05: M17 adds no event name. The gate
// speaks in TaskMoved's rule and in artifacts, so the whole flow must be
// recorded with names the catalog already holds.
func TestTheEventCatalogIsUnchanged(t *testing.T) {
	f := gateFixtureNew(t)
	designID := f.taskOfKind(t, TaskKindDesign)
	reviewID := f.taskOfKind(t, TaskKindReview)

	f.finish(t, designID, designID+"-a1", designReply(designID, designID+"-a1", "# The design"))
	f.finish(t, reviewID, reviewID+"-a1", reviewReply(reviewID, reviewID+"-a1"))

	names := eventCatalogNames()
	for _, gate := range []string{RuleDesignDelivered, RuleReviewCleared, RuleDesignBlocked, RuleReviewFailed} {
		if strings.Contains(names, gate) {
			t.Errorf("%q is an event name; the gate speaks in TaskMoved's rule, because BR-05 "+
				"closes the catalog", gate)
		}
	}
	// And every event the flow wrote is a name the catalog holds.
	for _, e := range f.s.events {
		if !catalog[e.Type] && !uiLifecycle[e.Type] {
			t.Errorf("the gate wrote an event of type %q, which is in neither the model "+
				"document's catalog nor the UI-lifecycle block", e.Type)
		}
	}
}

// eventCatalogNames is every event name the closed catalog holds.
func eventCatalogNames() string {
	var b strings.Builder
	for name := range catalog {
		b.WriteString(string(name))
		b.WriteString(" ")
	}
	return b.String()
}

// TestTheGateHoldsThroughTheOperatorsOwnRoutes is the gate's second door, and
// it is exercised through the routes themselves rather than through the
// helpers behind them.
//
// The first round of the review found that the operator could walk around the
// gate: the board's dispatch route applied the lease, the drift gate and the
// egress pass, and no dependency check at all. A test that calls the helpers
// behind those routes would have passed anyway, so this one calls the routes.
func TestTheGateHoldsThroughTheOperatorsOwnRoutes(t *testing.T) {
	f := gateFixtureNew(t)
	implID := f.taskOfKind(t, TaskKindImplement)
	ctx := context.Background()

	// The board must refuse to unblock a task the gate is holding.
	_, err := f.s.MoveCard(ctx, f.desk, implID, CardMove{Column: ColReady})
	if err == nil {
		t.Error("the board unblocked a task the design gate is holding; the gate has two " +
			"doors and one of them was open")
	} else if !strings.Contains(err.Error(), "design gate") {
		t.Errorf("the board refused with %q; the refusal must name the gate", err.Error())
	}

	// And the dispatch route must refuse a task whose prerequisite is not done.
	t.Setenv("GAOS_RUNNER", "on")
	f2, ad := newFakeAIM(t)
	f.s.SetAdapter(ad)
	_, err = f.s.dispatchNow(ctx, f.desk, implID)
	if err == nil {
		t.Error("the dispatch route dispatched a task whose review has not resolved")
	} else if !strings.Contains(err.Error(), "waits on") {
		t.Errorf("the dispatch route refused with %q; it must say what it waits on", err.Error())
	}
	_ = f2
}

// TestTheGateDoesNotOpenOnAReviewThatHadNoDesign is the hole the second
// review found: a design task whose document was refused by the screening
// recorded nothing, so the review ran with no design, found nothing, and the
// gate opened on nothing at all.
func TestTheGateDoesNotOpenOnAReviewThatHadNoDesign(t *testing.T) {
	f := gateFixtureNew(t)
	designID := f.taskOfKind(t, TaskKindDesign)
	reviewID := f.taskOfKind(t, TaskKindReview)
	implID := f.taskOfKind(t, TaskKindImplement)

	// The design task's reply carries a body the screening refuses, so no
	// design artifact is ever recorded.
	poisoned := "# The design\n\nAPI_KEY = xxxxxxxxxxxxxxxxxxxx\n"
	f.finish(t, designID, designID+"-a1", designReply(designID, designID+"-a1", poisoned))

	design := f.taskRecord(t, designID)
	if design.Status != StatusBlocked {
		t.Errorf("the design task is %q; a design the screening refused is BLOCKED, not "+
			"finished-looking", design.Status)
	}
	if design.BlockReason != ReasonDesignScreened {
		t.Errorf("the design task's reason is %q; it must be %q", design.BlockReason, ReasonDesignScreened)
	}

	// The review then has nothing to attack, and must not clear the gate.
	f.finish(t, reviewID, reviewID+"-a1", reviewReply(reviewID, reviewID+"-a1"))
	review := f.taskRecord(t, reviewID)
	if review.Status != StatusBlocked {
		t.Errorf("the review task is %q; a review with no design is BLOCKED", review.Status)
	}
	if review.BlockReason != ReasonDesignMissing {
		t.Errorf("the review task's reason is %q; it must be %q", review.BlockReason, ReasonDesignMissing)
	}
	impl := f.taskRecord(t, implID)
	if impl.Status != StatusBlocked {
		t.Errorf("the implementation is %q; it must stay held", impl.Status)
	}
}

// TestAnUnknownTaskKindIsRefusedBeforeTheFirstWrite is the fail-closed
// promise: a kind outside the vocabulary is refused, not normalised into an
// implement task.
func TestAnUnknownTaskKindIsRefusedBeforeTheFirstWrite(t *testing.T) {
	plan := []PlanTask{
		{ID: "t-1", Title: "plan", Role: "planner", Kind: TaskKindImplement},
		{ID: "t-2", Title: "design it", Role: "architect", Kind: TaskKindDesign, Deps: []string{"t-1"}},
		{ID: "t-3", Title: "review it", Role: "qa", Kind: TaskKindReview, Deps: []string{"t-2"}},
		{ID: "t-4", Title: "build it", Role: "backend", Kind: TaskKindImplement, Deps: []string{"t-3"}, Story: "S-1"},
		{ID: "t-5", Title: "sneaky", Role: "backend", Kind: "review-x", Deps: []string{"t-1"}, Story: "S-1"},
	}
	err := validatePlan(plan)
	if err == nil {
		t.Fatal("a plan with a task whose kind is outside the vocabulary was accepted; the " +
			"fail-open direction is contamination")
	}
	if !strings.Contains(err.Error(), "review-x") {
		t.Errorf("the refusal says %q; it must name the kind it refused", err.Error())
	}
}

// TestTheGateRecordsAReviewOnlyWhenItHasOne is the other half: a review that
// reached DONE without a recorded finding is a review that never ran, and the
// implementation stays held.
func TestTheGateRecordsAReviewOnlyWhenItHasOne(t *testing.T) {
	f := gateFixtureNew(t)
	reviewID := f.taskOfKind(t, TaskKindReview)
	implID := f.taskOfKind(t, TaskKindImplement)

	// The operator approves the review card by hand -- no review ran.
	f.s.moveTaskByID(reviewID, StatusReady, MoveSchedule)
	f.s.moveTaskByID(reviewID, StatusDispatched, MoveSchedule)
	f.s.moveTaskByID(reviewID, StatusRunning, MoveStart)
	f.s.moveTaskByID(reviewID, StatusReview, MoveExit)
	f.s.moveTaskByID(reviewID, StatusDone, MoveApprove)

	f.s.mu.RLock()
	state, gated := f.s.gateStateLocked(implID)
	f.s.mu.RUnlock()
	if !gated {
		t.Fatal("the implementation is not under the gate at all")
	}
	if state == GateCleared {
		t.Error("the gate cleared on a review task that reached DONE with no recorded " +
			"finding; an approval is not a verdict")
	}
}
