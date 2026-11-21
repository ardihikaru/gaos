package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// M14.6: the board, its legal-move table, and the refusals (D-061, BR-43).

// plannedProduct plans a workflow on the first product that clears the
// planning floor, and returns the server, the store and that product's id.
func plannedProduct(t *testing.T) (*server, *MemStore, string) {
	t.Helper()
	srv, s := newTestServer(t)
	ctx := context.Background()
	masters, _ := s.Masters(ctx)
	for _, m := range masters {
		if m.ReadinessScore() < PlanFloor || len(m.Stories) == 0 {
			continue
		}
		draft := WorkflowDraft{ProductID: m.ID, Goal: "board", Stories: []string{m.Stories[0].ID}}
		plan, err := s.PlanWorkflow(ctx, draft)
		if err != nil {
			continue
		}
		if _, err := s.CreateWorkflow(ctx, draft, plan); err != nil {
			continue
		}
		return srv, s, m.ID
	}
	t.Fatal("no seeded product could be planned")
	return nil, nil, ""
}

func setStatus(s *MemStore, id, status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.tasks {
		if s.tasks[i].ID == id {
			s.tasks[i].Status = status
		}
	}
}

func productTasks(s *MemStore, desk string) []TaskRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []TaskRecord
	for _, t := range s.tasks {
		if t.DeskID == desk {
			out = append(out, t)
		}
	}
	return out
}

func TestTheBoardHasItsColumnsEveryTaskOnceAndALanePerStory(t *testing.T) {
	srv, s, desk := plannedProduct(t)
	if w := do(t, srv, "GET", "/products/"+desk+"?tab=board", nil, boCookies()...); w.Code != 200 ||
		!strings.Contains(w.Body.String(), "board-card") {
		t.Fatalf("the Board tab rendered %d without a card; a template error here is a 500 no store test sees", w.Code)
	}
	b, err := s.Board(context.Background(), desk)
	if err != nil {
		t.Fatal(err)
	}
	empty, _ := s.Board(context.Background(), firstProductWithoutTasks(t, s))
	if raw, _ := json.Marshal(empty); !strings.Contains(string(raw), `"cards":[]`) {
		t.Fatalf("a product with no tasks answered %s; cards must be an empty array, not null", raw)
	}
	var cols []string
	for _, c := range b.Columns {
		cols = append(cols, c.ID)
	}
	if got := strings.Join(cols, ","); got != "backlog,ready,working,needs-you,review,blocked,failed,done,canceled" {
		t.Fatalf("columns are %s; want the nine declared ones in order", got)
	}
	tasks := productTasks(s, desk)
	seen := map[string]int{}
	lanes := map[string]bool{}
	for _, r := range b.Rows {
		lanes[r.ID] = true
	}
	for _, c := range b.Cards {
		seen[c.ID]++
		if !lanes[c.Row] {
			t.Errorf("card %s sits in lane %q, which the board does not declare", c.ID, c.Row)
		}
	}
	if len(seen) != len(tasks) || len(b.Cards) != len(tasks) || !lanes[PlanLane] {
		t.Fatalf("the board has %d cards for %d tasks (lanes %v); want every task exactly once and a plan lane", len(b.Cards), len(tasks), b.Rows)
	}
}

func TestEachLegalMoveProducesItsEventAndStatus(t *testing.T) {
	_, s, desk := plannedProduct(t)
	ctx := context.Background()
	tasks := productTasks(s, desk)
	if len(tasks) < 6 {
		t.Fatalf("the plan has %d tasks; this case needs six", len(tasks))
	}
	cases := []struct{ from, to, action, want string }{
		{StatusReview, ColDone, MoveApprove, StatusDone},
		{StatusReview, ColReady, MoveRedo, StatusReady},
		{StatusFailed, ColReady, MoveRetry, StatusReady},
		{StatusBlocked, ColReady, MoveUnblock, StatusReady},
		{StatusPending, ColCanceled, MoveCancel, StatusCanceled},
	}
	for i, c := range cases {
		id := tasks[i].ID
		setStatus(s, id, c.from)
		r, err := s.MoveCard(ctx, desk, id, CardMove{Column: c.to})
		got := taskStatusOf(s, id)
		if err != nil || got != c.want || len(r.NewEvents) != 1 || r.NewEvents[0].Type != TaskMoved || r.NewEvents[0].Payload.Rule != c.action {
			t.Errorf("%s -> %s gave status %s, events %v, err %v; want %s and one TaskMoved %s", c.from, c.to, got, r.NewEvents, err, c.want, c.action)
		}
	}
	r, err := s.MoveCard(ctx, desk, tasks[5].ID, CardMove{Column: columnOf(tasks[5].Status), Index: 0})
	if err != nil || len(r.NewEvents) != 1 || r.NewEvents[0].Type != TaskReordered {
		t.Fatalf("a move inside its column gave %v %v; want one TaskReordered", r.NewEvents, err)
	}

	// ready -> working dispatches, through the gateway, with the runner on.
	//
	// The task dispatched is one with no unmet prerequisite: M17 made the
	// board's dispatch route refuse a task whose prerequisite is not DONE, the
	// same rule the scheduler applies, and a review task's dependency is not
	// met while the review has not run.
	t.Setenv("GAOS_RUNNER", "on")
	f, ad := newFakeAIM(t)
	s.SetAdapter(ad)
	id := ""
	for _, task := range tasks {
		if len(task.Deps) == 0 {
			id = task.ID
			break
		}
	}
	if id == "" {
		t.Fatal("the plan holds no task without a prerequisite to dispatch")
	}
	setStatus(s, id, StatusReady)
	f.reply = chatReply(handoffBlock(id, runIDFor(id, 1)), "0.1", "")
	r, err = s.MoveCard(ctx, desk, id, CardMove{Column: ColWorking})
	if err != nil || r.NewEvents[0].Payload.Rule != MoveDispatch {
		t.Fatalf("ready -> working gave %v %v; want a dispatch", r.NewEvents, err)
	}
	waitForRun(t, s, runIDFor(id, 1), RunStatusExited)
}

func boardPut(t *testing.T, srv *server, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for _, c := range boCookies() {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, req)
	return w
}

func TestAMoveOutsideTheTableAnswers409AndWritesNoEvent(t *testing.T) {
	srv, s, desk := plannedProduct(t)
	id := productTasks(s, desk)[0].ID
	cases := []struct{ status, to string }{
		{StatusPending, ColReady},    // the scheduler owns dependencies
		{StatusDone, ColReady},       // finished
		{StatusDeferred, ColReady},   // the budget guard owns it
		{StatusPending, ColBlocked},  // nobody drops work into blocked
		{StatusReview, ColWorking},   // only ready may dispatch
		{StatusRunning, ColCanceled}, // a live turn cannot be stopped (OQ-26)
		{StatusCanceled, ColBacklog}, // finished
	}
	for _, c := range cases {
		setStatus(s, id, c.status)
		before := len(s.events)
		w := boardPut(t, srv, "/products/"+desk+"/board/cards/"+id+"/move", `{"column":"`+c.to+`"}`)
		var body map[string]string
		json.Unmarshal(w.Body.Bytes(), &body)
		if w.Code != http.StatusConflict || body["reason"] == "" || len(s.events) != before || taskStatusOf(s, id) != c.status {
			t.Errorf("%s -> %s answered %d %v and left %s with %d new event(s); want 409 with a reason and nothing written (BR-43)",
				c.status, c.to, w.Code, body, taskStatusOf(s, id), len(s.events)-before)
		}
	}
}

func TestALaneChangeACreateAndADeleteAnswer409(t *testing.T) {
	srv, s, desk := plannedProduct(t)
	id := productTasks(s, desk)[0].ID
	setStatus(s, id, StatusReview)
	if w := boardPut(t, srv, "/products/"+desk+"/board/cards/"+id+"/move", `{"column":"done","row":"US-99"}`); w.Code != http.StatusConflict {
		t.Fatalf("a lane change answered %d; a card's lane is its story and cannot move", w.Code)
	}
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		path := "/products/" + desk + "/board/cards"
		if m != http.MethodPost {
			path += "/" + id
		}
		req := httptest.NewRequest(m, path, strings.NewReader(`{}`))
		for _, c := range boCookies() {
			req.AddCookie(c)
		}
		w := httptest.NewRecorder()
		srv.routes().ServeHTTP(w, req)
		if w.Code != http.StatusConflict {
			t.Errorf("%s %s answered %d; cards are planned, not created, edited or deleted on the board", m, path, w.Code)
		}
	}
}

func TestTheReplayHandleRefusesEveryMove(t *testing.T) {
	_, s, desk := plannedProduct(t)
	id := productTasks(s, desk)[0].ID
	if _, err := (readOnly{s}).MoveCard(context.Background(), desk, id, CardMove{Column: ColCanceled}); !errors.Is(err, ErrRejected) {
		t.Fatalf("the read-only handle moved a card (%v); replay must not write (BR-20)", err)
	}
	if taskStatusOf(s, id) == StatusCanceled {
		t.Fatal("a refused replay move changed the task anyway")
	}
}

func firstProductWithoutTasks(t *testing.T, s *MemStore) string {
	t.Helper()
	ms, _ := s.Masters(context.Background())
	for _, m := range ms {
		if len(productTasks(s, m.ID)) == 0 {
			return m.ID
		}
	}
	t.Fatal("every seeded product has tasks")
	return ""
}

// TestTheGatesVerdictIsNeverRerolled is TC-509 (D-084): a review blocked by its
// own verdict is never re-run on the same design -- not by an unblock, not by a
// Blueprint sync's drift release -- and a design or review task is never
// approved, so a revision that delivered nothing cannot reach DONE.
func TestTheGatesVerdictIsNeverRerolled(t *testing.T) {
	ctx := context.Background()
	f, design, review, impl := blockedGate(t)

	_, err := f.s.MoveCard(ctx, f.desk, review, CardMove{Column: ColReady})
	want := review + "'s findings block its design; send the design back to its author (Revise design) " +
		"instead of re-running the review on the same design"
	if err == nil || err.Error() != want {
		t.Errorf("unblocking a design-blocked review answered %v; want %q", err, want)
	}
	card := func(id string) BoardCard {
		b, _ := f.s.Board(ctx, f.desk)
		for _, c := range b.Cards {
			if c.ID == id {
				return c
			}
		}
		t.Fatalf("no card %s on the board", id)
		return BoardCard{}
	}
	rc := card(review)
	for _, m := range rc.Moves {
		if m == ColReady {
			t.Error("the board offers ready on a design-blocked review")
		}
	}
	if rc.Reason != ReasonDesignBlocked || rc.SendBack == nil || !rc.SendBack.Open || rc.SendBack.Design != design {
		t.Errorf("the review card is reason %q, sendBack %+v; want design-blocked and an open send-back of %s", rc.Reason, rc.SendBack, design)
	}

	// Approve is refused for a design and a review task, and not offered.
	f.s.mu.Lock()
	f.s.taskByIDLocked(review).Status = StatusReview
	f.s.mu.Unlock()
	for _, id := range []string{review} {
		_, err := f.s.MoveCard(ctx, f.desk, id, CardMove{Column: ColDone})
		if err == nil || !strings.Contains(err.Error(), "the design gate decides when it is done, not an approval") {
			t.Errorf("approving %s answered %v; the gate is the only writer of its DONE", id, err)
		}
		for _, m := range card(id).Moves {
			if m == ColDone {
				t.Errorf("the board offers done on %s, a gated task in review", id)
			}
		}
	}
	g := gateFixtureNew(t)
	gd := g.taskOfKind(t, TaskKindDesign)
	g.run(t, gd, 1, "no design here\n\n"+fence+HandoffFence+"\n"+handoffExample(gd, gd+"-a1", "architect")+"\n"+fence+"\n", "", 0)
	if st := g.taskRecord(t, gd).Status; st != StatusReview {
		t.Fatalf("a design reply with no design block left the task %s; the fixture needs REVIEW", st)
	}
	if _, err := g.s.MoveCard(ctx, g.desk, gd, CardMove{Column: ColDone}); err == nil ||
		!strings.Contains(err.Error(), "is a design task; the design gate decides when it is done, not an approval") {
		t.Errorf("approving a design that delivered nothing answered %v; it must be refused", err)
	}

	// An unblock clears the reason of the block it ends.
	f.s.mu.Lock()
	r := f.s.taskByIDLocked(review)
	r.Status, r.BlockReason = StatusBlocked, ReasonReviewFailed
	f.s.mu.Unlock()
	if _, err := f.s.MoveCard(ctx, f.desk, review, CardMove{Column: ColReady}); err != nil {
		t.Fatalf("a review-failed review must still unblock: %v", err)
	}
	if rr := f.taskRecord(t, review); rr.Status != StatusReady || rr.BlockReason != "" {
		t.Errorf("the unblocked review is %s with reason %q; an unblock clears the reason", rr.Status, rr.BlockReason)
	}

	// The drift release leaves what only an operator may release.
	f.s.mu.Lock()
	r.Status, r.BlockReason = StatusBlocked, ReasonDesignBlocked
	f.s.taskByIDLocked(impl).BlockReason = ReasonDesignUnreviewed
	f.s.tasks = append(f.s.tasks,
		TaskRecord{ID: "t-qa", DeskID: f.desk, Status: StatusBlocked, BlockReason: ReasonQaEpicAdded},
		TaskRecord{ID: "t-drift", DeskID: f.desk, Status: StatusBlocked, BlockReason: "a reason of another block"})
	i := f.s.deskIdxByID(f.desk)
	f.s.desks[i].BlockReason = "the workspace does not hold the published Blueprint (BR-39)"
	f.s.releaseDriftLocked(f.desk)
	f.s.mu.Unlock()
	for id, st := range map[string]string{review: StatusBlocked, impl: StatusBlocked, "t-qa": StatusBlocked, "t-drift": StatusReady} {
		if got := f.taskRecord(t, id).Status; got != st {
			t.Errorf("after a drift release %s is %s; want %s", id, got, st)
		}
	}
}
