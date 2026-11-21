package main

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// catalog is the closed list from artifacts/v1/gaos-arcadedb-model.html §08,
// written out here on purpose rather than derived from events.go.
//
// Deriving it would make this test agree with the code by construction, which
// is the one thing it must not do. BR-05 says the catalog in code is the catalog
// in the document; the only way to check that is to write the document's version
// down separately and compare.
var catalog = map[EventType]bool{
	// master
	ProductCreated: true, RecordUpdated: true, KnowledgeInjected: true,
	ReadinessEvaluate: true, ReadinessDropped: true, BlueprintPublished: true,
	WorkspaceProvisioned: true, BlueprintSynced: true, SessionOpened: true, SessionLost: true,
	TaskMoved: true, TaskReordered: true,
	// planning
	WorkflowCreated: true, TaskPlanned: true, DependencyRewired: true, WorkflowCanceled: true,
	// execution
	TaskDispatched: true, RunStarted: true, Heartbeat: true, RunExited: true,
	TaskBlocked: true, TaskResumed: true, TaskOrphaned: true,
	// execution -- M15, D-064
	QuestionAsked: true, QuestionAnswered: true,
	// execution -- D-078, the QA tool's inbox items
	ExternalQuestionAsked: true, ExternalQuestionAnswered: true, ExternalAnswerDelivery: true,
	// execution -- D-082, a card's conversation
	ConversationArchived: true, ConversationPruned: true,
	// judgment
	ReviewCompleted: true, RetryRequested: true, ModelEscalated: true,
	SafetyVeto: true, GuardExhausted: true,
	// gates
	ApprovalRequested: true, ApprovalGranted: true, ApprovalRejected: true,
	// collaboration
	MeetingStarted: true, ProposalRecorded: true, DecisionRecorded: true, MeetingClosed: true,
	// memory
	KnowledgeExtracted: true, KnowledgeReused: true, DecisionSuperseded: true,
	// budget -- D-037
	BudgetThresholdReached: true, BudgetCapReached: true, TaskDeferred: true,
	// egress -- L-5, D-052
	ContextRedacted: true, DispatchRefused: true,
}

// uiLifecycle is the separate block D-020 fences off: names the prototype
// emitted that are not domain events. Kept apart so nobody mistakes one for the
// other, which is the whole reason the fence exists.
var uiLifecycle = map[EventType]bool{
	FloorOpened: true, TaskCompleted: true, TaskCanceled: true, WorkflowCompleted: true,
}

// TestEveryEmittedEventIsInTheClosedCatalog is BR-05 with teeth. An event
// written under an invented name is a state change nothing downstream can read,
// and the failure is silent: the pill renders, the stream grows, and the fold
// skips it forever.
func TestEveryEmittedEventIsInTheClosedCatalog(t *testing.T) {
	s := runAWorkflow(t, 31, 400)

	// Plus the mutations the simulator never reaches on its own.
	drive(t, s)

	seen := map[EventType]int{}
	for _, e := range s.events {
		seen[e.Type]++
		if !catalog[e.Type] && !uiLifecycle[e.Type] {
			t.Errorf("event seq %d was written as %q, which is in neither the model "+
				"document's catalog nor the UI-lifecycle block", e.Seq, e.Type)
		}
	}
	if len(seen) < 10 {
		t.Fatalf("only %d distinct event types were emitted; the run is not exercising "+
			"enough of the catalog for this to mean anything", len(seen))
	}
}

// The names the code declares and does not write. This is the honest half of
// T-014: nine of them belong to the runner (M4), the review engine (M5), the
// planner's task graph (M3.2), recall (M7.3) and the budget guard (M10.3), and
// emitting one from sim.go would be inventing evidence -- a Heartbeat nobody
// sent, a RunExited from a process that never started.
//
// The test pins the list so the next person to close one of those modules has to
// come here and say so, rather than the gap quietly growing or quietly shrinking.
func TestTheUnemittedNamesAreExactlyTheOnesStillOwedToLaterModules(t *testing.T) {
	owed := map[EventType]string{
		DependencyRewired:      "M3.2 -- the planner's task graph",
		SafetyVeto:             "M5.2 -- the real review engine",
		GuardExhausted:         "M5.5 -- the six guards",
		KnowledgeReused:        "M7.3 -- recall at planning time",
		DecisionSuperseded:     "M7.3 -- contradiction detection",
		BudgetThresholdReached: "M10.3 -- the budget guard",
		BudgetCapReached:       "M10.3 -- the budget guard",
		TaskDeferred:           "M10.3 -- the budget guard",
	}

	s := runAWorkflow(t, 41, 400)
	drive(t, s)
	driveRunner(t, s)
	driveRecovery(t, s)

	emitted := map[EventType]bool{}
	for _, e := range s.events {
		emitted[e.Type] = true
	}

	for name, why := range owed {
		if emitted[name] {
			t.Errorf("%s is being emitted, but this list still says it is owed to %s -- "+
				"if the module landed, take it out of the list", name, why)
		}
	}
	for name := range catalog {
		if !emitted[name] && owed[name] == "" {
			t.Errorf("%s is in the catalog, is not emitted, and is not listed as owed to a "+
				"later module; a declared name nothing writes needs a reason next to it", name)
		}
	}
}

// drive exercises the mutations the simulator does not reach by itself, so the
// catalog tests see the operator-driven half of the stream too.
func drive(t *testing.T, s *MemStore) {
	t.Helper()
	ctx := t.Context()

	id := "p-2"
	// Filled in properly, not just a problem statement. The record has to land in
	// a band comfortably above the one the emptying below drops it to, or there is
	// no downward crossing and BR-10's ReadinessDropped never fires -- which is one
	// of the names this whole helper exists to provoke.
	if _, err := s.UpdateBRD(ctx, id, BRD{
		Problem:    strings.Repeat("a problem worth stating. ", 3),
		Goals:      []string{"Fold the stream back into the floor"},
		Rules:      []string{"Append only", "The reducer is pure", "Order is stable"},
		OutOfScope: []string{"Compaction"},
		Metrics:    []string{"Fold-from-genesis matches fold-from-snapshot"},
	}); err != nil {
		t.Fatalf("UpdateBRD: %v", err)
	}
	// D-058: a policy and a publish, so BlueprintPublished is on the stream.
	if _, err := s.SavePolicy(ctx, id, Policy{Key: "go-style", Kind: PolicyCoding, Title: "Go style", Body: "gofmt, no globals"}); err != nil {
		t.Fatalf("SavePolicy: %v", err)
	}
	if _, err := s.PublishBlueprint(ctx, id); err != nil {
		t.Fatalf("PublishBlueprint: %v", err)
	}
	// M14.4: sync it through a fake gateway, so WorkspaceProvisioned and
	// BlueprintSynced are on the stream; then detach, so the rest of the
	// drive runs with no adapter as before.
	fake, gw := newFakeAIM(t)
	s.SetAdapter(gw)
	if _, err := s.SyncBlueprint(ctx, id); err != nil {
		t.Fatalf("SyncBlueprint: %v", err)
	}
	// M14.5: a first turn opens a session (SessionOpened), and a resumed turn
	// the gateway answers 410 loses it (SessionLost).
	turnPacket := func(run, session string) Cleared {
		c, _, err := Screen(EgressPolicy{}, Target{Provider: "anthropic"}, Packet{TaskID: "t-drive", RunID: run, DeskID: id,
			SessionID: session, Parts: []PacketPart{{Path: ".gaos/packet/task.md", Kind: KindTask, Body: "# t-drive"}}})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	fake.reply = chatReply(handoffBlock("t-drive", "t-drive-a1"), "0.1", "")
	first, err := s.superviseRun(ctx, gw, id, turnPacket("t-drive-a1", ""), 1)
	if err != nil {
		t.Fatalf("first turn: %v", err)
	}
	fake.submit = http.StatusGone
	if _, err := s.superviseRun(ctx, gw, id, turnPacket("t-drive-a2", first.SessionID), 2); err != nil {
		t.Fatalf("resumed turn: %v", err)
	}
	s.SetAdapter(nil)
	// Two criteria, not one. AddStory defaults a new story to "must", and BR-07's
	// rubric forfeits the whole criteria-coverage signal when any must story is
	// thinner than that -- which would drop this record below the planning floor
	// and refuse the plan several lines down.
	if _, err := s.AddStory(ctx, id, Story{Role: "operator", Want: "an audit trail",
		AC: []string{"it folds", "the fold matches a replay from genesis"}}); err != nil {
		t.Fatalf("AddStory: %v", err)
	}
	if _, err := s.AddDiagram(ctx, id, Diagram{Title: "Fold", Src: "a -> b"}); err != nil {
		t.Fatalf("AddDiagram: %v", err)
	}
	if _, err := s.AddDecision(ctx, id, Decision{Title: "Fold from genesis"}); err != nil {
		t.Fatalf("AddDecision: %v", err)
	}
	if _, err := s.Inject(ctx, id); err != nil {
		t.Fatalf("Inject: %v", err)
	}
	if _, err := s.CreateProduct(ctx, ProductDraft{Name: "Catalog Probe"}); err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}

	// A planned workflow: TaskPlanned rides with WorkflowCreated. This has to
	// happen BEFORE the record is emptied below -- BR-07 refuses to plan under 45,
	// so a plan asked for after the emptying would get a refusal and this helper
	// would never reach the events it exists to provoke.
	plan, err := s.PlanWorkflow(ctx, WorkflowDraft{ProductID: id, Goal: "Prove the catalog is exercised"})
	if err != nil {
		t.Fatalf("PlanWorkflow: %v", err)
	}
	if _, err := s.CreateWorkflow(ctx, WorkflowDraft{
		ProductID: id, Goal: "Prove the catalog is exercised",
	}, plan); err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}

	// A readiness band crossing: the record above was filled in, so emptying it
	// and re-injecting drops it a band. BR-10's ReadinessDropped.
	if _, err := s.UpdateBRD(ctx, id, BRD{}); err != nil {
		t.Fatalf("UpdateBRD (emptied): %v", err)
	}
	if _, err := s.Inject(ctx, id); err != nil {
		t.Fatalf("Inject after emptying: %v", err)
	}

	// A review, and its scripted decision.
	f, _ := s.Snapshot(ctx)
	for i, d := range f.Desks {
		if d.State != StateIdle && d.State != StateMeeting {
			if _, err := s.CallReview(ctx, i); err == nil {
				for j := 0; j < 40; j++ {
					s.Tick()
				}
			}
			break
		}
	}

	// The two egress events. Both are operator-driven and neither is reachable
	// from the simulator, which is the point: sim.go emitting them would be
	// inventing evidence about a dispatch that never happened.
	//
	// A real workspace with a real file outside the allowlist, so the redaction
	// is a fact about bytes rather than a fixture asserting itself.
	driveEgress(t, s)

	// A gate answered with no, on the last stage, which is both an
	// ApprovalRejected and a WorkflowCanceled. Drive a desk there rather than
	// waiting for the dice: this is a scenario the operator can always reach,
	// and a test that only sometimes reaches it tests nothing on the other runs.
	f, _ = s.Snapshot(ctx)
	for i := range f.Desks {
		d := &s.desks[i]
		if d.State == StateIdle || d.State == StateMeeting {
			continue
		}
		d.StageIdx = len(stages) - 1
		d.ApprovalRequired = true
		if _, err := s.CancelTask(ctx, i); err != nil {
			t.Fatalf("cancel a gated desk on the last stage: %v", err)
		}
		break
	}
	// M14.6: a reorder and a cancel on the board, so TaskReordered and
	// TaskMoved are on the stream.
	s.mu.RLock()
	var boardTask, boardDesk string
	for _, tk := range s.tasks {
		if tk.Status == StatusPending {
			boardTask, boardDesk = tk.ID, tk.DeskID
			break
		}
	}
	s.mu.RUnlock()
	if boardTask == "" {
		t.Fatal("drive found no pending task to move on the board")
	}
	if _, err := s.MoveCard(ctx, boardDesk, boardTask, CardMove{Column: ColBacklog, Index: 0}); err != nil {
		t.Fatalf("reorder: %v", err)
	}
	if _, err := s.MoveCard(ctx, boardDesk, boardTask, CardMove{Column: ColCanceled}); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	// D-082: the cancelled card, given one recorded attempt, is archived,
	// restored, archived and pruned, so ConversationArchived and
	// ConversationPruned are on the stream.
	s.mu.Lock()
	s.runs = append(s.runs, TaskRunRecord{ID: runIDFor(boardTask, 1), TaskID: boardTask, DeskID: boardDesk,
		Attempt: 1, Status: RunStatusExited, StartedAt: 1, EndedAt: 2})
	s.recordHandoff(ArtifactRecord{ID: runIDFor(boardTask, 1) + "-reply", Kind: ArtifactReply,
		TaskID: boardTask, DeskID: boardDesk, RunID: runIDFor(boardTask, 1), Attempt: 1, Body: "done"})
	s.mu.Unlock()
	for _, archive := range []bool{true, false, true} {
		if _, err := s.ArchiveConversation(ctx, boardDesk, boardTask, "drive@example.test", archive); err != nil {
			t.Fatalf("archive=%v: %v", archive, err)
		}
	}
	if _, err := s.PruneConversations(ctx, "drive@example.test", []CardRef{{boardDesk, boardTask}}); err != nil {
		t.Fatalf("prune: %v", err)
	}
}

// driveEgress provokes ContextRedacted and DispatchRefused, the two names L-5
// requires and T-127 makes real.
//
// Two dispatches, because the pair is the point: one slice is cut down and goes
// anyway, one is refused outright. A helper that only ever produced the refusal
// would leave ContextRedacted declared-but-unemitted, which is exactly the gap
// TestTheUnemittedNamesAreExactlyTheOnesStillOwedToLaterModules exists to catch.
func driveEgress(t *testing.T, s *MemStore) {
	t.Helper()
	ctx := t.Context()

	park := func() int {
		s.mu.Lock()
		defer s.mu.Unlock()
		for i := range s.desks {
			if s.desks[i].State == StateIdle {
				return i
			}
		}
		d := &s.desks[0]
		d.State, d.Agents, d.Meeting = StateIdle, nil, nil
		if s.meetingIdx == 0 {
			s.meetingIdx = -1
		}
		return 0
	}

	// ---- a redacted slice that still dispatches ----
	idx := park()
	s.mu.Lock()
	s.egress = EgressPolicy{AllowPaths: []string{"src/**"}}
	s.target = Target{Provider: "anthropic"}
	s.mu.Unlock()
	withRepo(s, map[string]string{
		"src/keep.go":  "package keep\n",
		"docs/drop.md": "notes nobody reviewed before they were sent\n",
	})
	if _, err := s.DispatchTask(ctx, idx); err != nil {
		t.Fatalf("a redacted slice refused the dispatch: %v", err)
	}

	// ---- a refusal ----
	idx = park()
	s.mu.Lock()
	s.egress = EgressPolicy{LocalOnly: true}
	s.target = Target{Provider: "anthropic", Local: false}
	s.repoParts = nil
	s.mu.Unlock()

	if _, err := s.DispatchTask(ctx, idx); err != nil {
		t.Fatalf("a refused dispatch came back as an error: %v", err)
	}
}

// ReadinessDropped is not free: BR-10 says a downward band crossing is an event,
// not a silent recalculation. This drives a record down a band and requires the
// event, then drives it back up and requires silence -- an upward crossing is
// good news and the requirement does not ask for it.
func TestReadinessDroppedFiresOnlyOnADownwardBandCrossing(t *testing.T) {
	ctx := t.Context()
	s := NewMemStore(2)
	id := "p-1"

	full := BRD{
		Problem: strings.Repeat("a problem statement long enough to count. ", 2),
		Goals:   []string{"ship it"}, Rules: []string{"no rounding drift"},
	}
	if _, err := s.UpdateBRD(ctx, id, full); err != nil {
		t.Fatalf("UpdateBRD: %v", err)
	}
	if _, err := s.Inject(ctx, id); err != nil {
		t.Fatalf("first inject: %v", err)
	}
	high := lastReadiness(t, s, id)

	// Gut the record and re-inject. The score has to fall, and far enough to
	// change band, or the test is not testing a band crossing.
	if _, err := s.UpdateBRD(ctx, id, BRD{}); err != nil {
		t.Fatalf("UpdateBRD (emptied): %v", err)
	}
	r, err := s.Inject(ctx, id)
	if err != nil {
		t.Fatalf("second inject: %v", err)
	}
	low := lastReadiness(t, s, id)

	if low >= high {
		t.Fatalf("emptying the record did not lower readiness: %d then %d", high, low)
	}
	if ReadinessBand(low) == ReadinessBand(high) {
		t.Skipf("readiness fell from %d to %d without leaving band %q", high, low, ReadinessBand(high))
	}
	if !hasEvent(r.NewEvents, ReadinessDropped) {
		t.Fatalf("readiness fell from %s (%d) to %s (%d) and no ReadinessDropped was "+
			"emitted; BR-10 calls that a silent recalculation",
			ReadinessBand(high), high, ReadinessBand(low), low)
	}

	// Back up. An upward crossing is not a drop.
	if _, err := s.UpdateBRD(ctx, id, full); err != nil {
		t.Fatalf("UpdateBRD (restored): %v", err)
	}
	r, err = s.Inject(ctx, id)
	if err != nil {
		t.Fatalf("third inject: %v", err)
	}
	if hasEvent(r.NewEvents, ReadinessDropped) {
		t.Error("readiness went back up and ReadinessDropped was emitted anyway")
	}
	if !hasEvent(r.NewEvents, ReadinessEvaluate) {
		t.Error("an injection that rescored the record emitted no ReadinessEvaluated")
	}
}

func hasEvent(evs []Event, want EventType) bool {
	for _, e := range evs {
		if e.Type == want {
			return true
		}
	}
	return false
}

func lastReadiness(t *testing.T, s *MemStore, id string) int {
	t.Helper()
	for i := len(s.events) - 1; i >= 0; i-- {
		if s.events[i].Type == ReadinessEvaluate && s.events[i].DeskID == id {
			return s.events[i].Payload.Score
		}
	}
	t.Fatalf("no ReadinessEvaluated for %s in the stream", id)
	return 0
}

// driveRunner supervises one turn against the fake gateway, so the catalog
// test sees the names the runner writes: RunStarted, Heartbeat (the final beat
// always writes), RunExited and SessionOpened.
//
// It has to be here rather than asserted in the abstract: TC-144 asks whether a
// name is EMITTED, and a name is only emitted if something emits it.
func driveRunner(t *testing.T, s *MemStore) {
	t.Helper()
	f, ad := gatewayOn(t, s)
	desk := s.desks[0].ID
	completeTurn(f, "wf-drive-t-1", 1, "0.1")
	if _, err := s.superviseRun(t.Context(), ad, desk, clearedFor(t, s, desk, "wf-drive-t-1", 1), 1); err != nil {
		t.Fatalf("supervising one turn against the fake gateway failed: %v", err)
	}
	// M15: a turn that asks, and the operator's answer -- the only writers of
	// QuestionAsked and QuestionAnswered.
	plantRunning(s, desk, "wf-drive-t-2")
	askTurn(f, "q-drive")
	if _, err := s.superviseRun(t.Context(), ad, desk, clearedFor(t, s, desk, "wf-drive-t-2", 1), 1); err != nil {
		t.Fatalf("supervising an asking turn against the fake gateway failed: %v", err)
	}
	if _, err := s.AnswerQuestion(t.Context(), desk, "wf-drive-t-2", ChoiceRecommended, ""); err != nil {
		t.Fatalf("answering the driven question failed: %v", err)
	}
	driveExternal(t, s)
}

// driveRecovery plants one task whose turn the gateway no longer knows, and
// reconciles it. That is the only way TaskOrphaned is ever written: no live
// supervisor witnesses an orphan, so the event exists only on the boot path
// (D-060). The deadline is short so the requeue (TaskResumed) lands too.
func driveRecovery(t *testing.T, s *MemStore) {
	t.Helper()
	f, ad := gatewayOn(t, s)
	ad.Deadline = 50 * time.Millisecond
	f.status, f.reply = http.StatusNotFound, `{"error":"unknown request"}`
	deskID := s.desks[0].ID
	run := runIDFor("wf-1-t-9", 1)
	s.mu.Lock()
	s.tasks = append(s.tasks, TaskRecord{
		ID: "wf-1-t-9", WorkflowID: "wf-1", DeskID: deskID,
		Title: "a turn nobody was watching", Role: "backend", Status: StatusRunning,
	})
	s.runs = append(s.runs, TaskRunRecord{ID: run, TaskID: "wf-1-t-9", DeskID: deskID, Attempt: 1,
		Status: RunStatusRunning, RequestID: ad.RequestIDFor(run), StartedAt: time.Now().Unix()})
	s.mu.Unlock()

	if _, err := s.Reconcile(t.Context()); err != nil {
		t.Fatalf("boot reconciliation failed: %v", err)
	}
	waitForRun(t, s, run, RunStatusOrphaned)
}
