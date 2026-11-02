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
	if got := strings.Join(cols, ","); got != "backlog,ready,working,review,blocked,failed,done,canceled" {
		t.Fatalf("columns are %s; want the eight declared ones in order", got)
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
	t.Setenv("GAOS_RUNNER", "on")
	f, ad := newFakeAIM(t)
	s.SetAdapter(ad)
	id := tasks[5].ID
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
