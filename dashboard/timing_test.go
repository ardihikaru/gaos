package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// D-081, docs/designs/card-timing.md: a card's timing is a pure function of
// its TaskPlanned/TaskMoved events and its runs.

const tmDesk = "p-9"

func tmPlan(seq int, at int64, ids ...string) Event {
	var pts []PlannedTask
	for _, id := range ids {
		pts = append(pts, PlannedTask{ID: id})
	}
	return Event{Seq: seq, Type: TaskPlanned, DeskID: tmDesk, At: at, Payload: EventPayload{WorkflowID: "wf-1", Tasks: pts}}
}

func tmMove(seq int, at int64, task, from, to, rule string) Event {
	return Event{Seq: seq, Type: TaskMoved, DeskID: tmDesk, At: at,
		Payload: EventPayload{Task: task, Rule: rule, Reason: from + "→" + to, To: to}}
}

func tmTask(id, status string) TaskRecord {
	return TaskRecord{ID: id, DeskID: tmDesk, WorkflowID: "wf-1", Status: status}
}

// tmHistory is one card's full life: planned at 1000, two attempts, one
// question, approved at 9000.
func tmHistory() []Event {
	return []Event{
		tmPlan(1, 1000, "t-1"),
		tmMove(2, 1600, "t-1", StatusPending, StatusReady, MoveSchedule),
		tmMove(3, 2200, "t-1", StatusReady, StatusDispatched, MoveDispatch),
		tmMove(4, 2210, "t-1", StatusDispatched, StatusRunning, MoveStart),
		tmMove(5, 3410, "t-1", StatusRunning, StatusAwaitingInput, MoveExit),
		tmMove(6, 5210, "t-1", StatusAwaitingInput, StatusReady, MoveAnswer),
		tmMove(7, 5810, "t-1", StatusReady, StatusDispatched, MoveDispatch),
		tmMove(8, 5815, "t-1", StatusDispatched, StatusRunning, MoveStart),
		tmMove(9, 6815, "t-1", StatusRunning, StatusReview, MoveExit),
		tmMove(10, 9000, "t-1", StatusReview, StatusDone, MoveApprove),
	}
}

func partitionSum(c CardTiming) int64 {
	return c.AISeconds + c.WaitingOperatorSeconds + c.QueuedSeconds + c.BlockedSeconds + c.BacklogSeconds
}

func TestCardTimingFollowsEveryMove(t *testing.T) {
	c := cardTiming(tmTask("t-1", StatusDone), tmHistory(), nil, 20000, RunnerManual)
	type seg struct {
		st, from, rule string
		in, out, sec   int64
	}
	want := []seg{
		{StatusPending, "", "plan", 1000, 1600, 600},
		{StatusReady, StatusPending, MoveSchedule, 1600, 2200, 600},
		{StatusDispatched, StatusReady, MoveDispatch, 2200, 2210, 10},
		{StatusRunning, StatusDispatched, MoveStart, 2210, 3410, 1200},
		{StatusAwaitingInput, StatusRunning, MoveExit, 3410, 5210, 1800},
		{StatusReady, StatusAwaitingInput, MoveAnswer, 5210, 5810, 600},
		{StatusDispatched, StatusReady, MoveDispatch, 5810, 5815, 5},
		{StatusRunning, StatusDispatched, MoveStart, 5815, 6815, 1000},
		{StatusReview, StatusRunning, MoveExit, 6815, 9000, 2185},
		{StatusDone, StatusReview, MoveApprove, 9000, 9000, 0},
	}
	if len(c.Segments) != len(want) {
		t.Fatalf("the history gave %d segments %+v; want one per status the card stayed in, %d", len(c.Segments), c.Segments, len(want))
	}
	for i, w := range want {
		g := c.Segments[i]
		if g.Status != w.st || g.From != w.from || g.Rule != w.rule || g.EnteredAt != w.in || g.LeftAt != w.out || g.Seconds != w.sec {
			t.Fatalf("segment %d is %+v; want %s from %q by %q, %d→%d, %ds", i, g, w.st, w.from, w.rule, w.in, w.out, w.sec)
		}
	}
	if !c.Known || !c.Complete || !c.Finished || c.CreatedAt != 1000 || c.FinishedAt != 9000 || c.LeadSeconds != 8000 {
		t.Fatalf("the card reads known=%v complete=%v finished=%v created=%d finished=%d lead=%d; want a finished card, lead 8000 s from 1000 to 9000",
			c.Known, c.Complete, c.Finished, c.CreatedAt, c.FinishedAt, c.LeadSeconds)
	}
}

func TestCardTimingBucketsPartitionTheLeadTime(t *testing.T) {
	for _, mode := range []string{RunnerManual, RunnerOff, RunnerOn} {
		c := cardTiming(tmTask("t-1", StatusDone), tmHistory(), nil, 20000, mode)
		if partitionSum(c) != c.LeadSeconds || c.LeadSeconds != 8000 {
			t.Fatalf("under %s the buckets sum to %d and the lead is %d; every second of the lead belongs to exactly one bucket", mode, partitionSum(c), c.LeadSeconds)
		}
		if c.AISeconds != 10+1200+5+1000 || c.IdleSeconds != c.LeadSeconds-c.AISeconds || c.BacklogSeconds != 600 {
			t.Fatalf("under %s AI is %d, idle %d, backlog %d; want AI = DISPATCHED+RUNNING = 2215, idle = lead - AI, backlog 600", mode, c.AISeconds, c.IdleSeconds, c.BacklogSeconds)
		}
		ready := int64(600 + 600)
		wantOp, wantQ := 1800+2185+ready, int64(0)
		if mode == RunnerOn {
			wantOp, wantQ = 1800+2185, ready
		}
		if c.WaitingOperatorSeconds != wantOp || c.QueuedSeconds != wantQ {
			t.Fatalf("under %s waiting-on-operator is %d and queued %d; want %d and %d -- READY waits on the operator unless the runner is on", mode, c.WaitingOperatorSeconds, c.QueuedSeconds, wantOp, wantQ)
		}
	}
}

func TestCardTimingAnOpenCardRunsToNow(t *testing.T) {
	evs := tmHistory()[:5] // ends RUNNING → AWAITING_INPUT at 3410
	c := cardTiming(tmTask("t-1", StatusAwaitingInput), evs, nil, 4410, RunnerManual)
	last := c.Segments[len(c.Segments)-1]
	if !last.Open || last.LeftAt != 0 || last.Seconds != 1000 || c.Finished || c.LeadSeconds != 3410 || partitionSum(c) != c.LeadSeconds {
		t.Fatalf("an open card's last segment is %+v and its lead %d; want it open, measured to now (1000 s), and the lead now - created = 3410", last, c.LeadSeconds)
	}
	if c.LeadText != "56m" {
		t.Fatalf("the chip text is %q; want relSpan of the lead, 56m", c.LeadText)
	}
}

func TestCardTimingWithoutTimesIsUnknownAndAClockStepCannotBreakThePartition(t *testing.T) {
	evs := tmHistory()
	evs[3].At = 0
	c := cardTiming(tmTask("t-1", StatusDone), evs, nil, 20000, RunnerManual)
	if c.Known || c.LeadSeconds != 0 || c.AISeconds != 0 || c.LeadText != "—" || c.Reason == "" || len(c.Segments) != 10 {
		t.Fatalf("an event with no time gave known=%v lead=%d ai=%d text=%q reason=%q; want an unknown timing with every number 0 and its segments still listed", c.Known, c.LeadSeconds, c.AISeconds, c.LeadText, c.Reason)
	}
	step := []Event{
		tmPlan(1, 0+1, "t-1"),
		tmMove(2, 100, "t-1", StatusPending, StatusReady, MoveSchedule),
		tmMove(3, 50, "t-1", StatusReady, StatusDispatched, MoveDispatch),
		tmMove(4, 200, "t-1", StatusDispatched, StatusRunning, MoveStart),
	}
	c = cardTiming(tmTask("t-1", StatusRunning), step, nil, 260, RunnerManual)
	if !c.Known || partitionSum(c) != c.LeadSeconds || c.LeadSeconds != 259 || c.IdleSeconds < 0 {
		t.Fatalf("a clock that stepped back gave lead %d, bucket sum %d, idle %d; want the partition exact (259) and idle never negative", c.LeadSeconds, partitionSum(c), c.IdleSeconds)
	}
	for _, s := range c.Segments {
		if s.Seconds < 0 {
			t.Fatalf("a clock step produced a negative segment %+v", s)
		}
	}
}

func TestCardTimingIgnoresOtherTasksAndProducts(t *testing.T) {
	evs := tmHistory()
	noise := []Event{
		tmMove(11, 9500, "t-2", StatusReady, StatusDispatched, MoveDispatch),
		{Seq: 12, Type: RunStarted, DeskID: tmDesk, At: 9600, Payload: EventPayload{Task: "t-1"}},
		{Seq: 13, Type: TaskMoved, DeskID: "p-other", At: 9700, Payload: EventPayload{Task: "t-1", To: StatusReady, Rule: MoveRetry}},
		{Seq: 14, Type: TaskPlanned, DeskID: "p-other", At: 1, Payload: EventPayload{Tasks: []PlannedTask{{ID: "t-1"}}}},
	}
	clean := cardTiming(tmTask("t-1", StatusDone), evs, nil, 20000, RunnerManual)
	noisy := cardTiming(tmTask("t-1", StatusDone), append(append([]Event(nil), evs...), noise...), nil, 20000, RunnerManual)
	a, _ := json.Marshal(clean)
	b, _ := json.Marshal(noisy)
	if string(a) != string(b) {
		t.Fatalf("another task's move, another product's events and a non-move event changed the timing:\n%s\nvs\n%s", a, b)
	}
}

func TestCardTimingThatDisagreesWithTheCardIsIncomplete(t *testing.T) {
	evs := tmHistory()[:3] // ends READY → DISPATCHED... cut to end in READY
	evs = evs[:2]
	c := cardTiming(tmTask("t-1", StatusDone), evs, nil, 20000, RunnerManual)
	if c.Complete || c.Status != StatusDone || !strings.Contains(c.Reason, "READY") || !strings.Contains(c.Reason, "DONE") || c.LeadText != "—" {
		t.Fatalf("a history ending in READY for a DONE card gave complete=%v status=%s reason=%q text=%q; want incomplete, the card's status, and a reason naming both", c.Complete, c.Status, c.Reason, c.LeadText)
	}
	c = cardTiming(tmTask("t-1", StatusRunning), tmHistory()[2:4], nil, 3000, RunnerManual)
	if c.Complete || c.Reason != "no plan event for this card" || c.Segments[0].Status != StatusReady || c.Segments[0].Seconds != 0 {
		t.Fatalf("a history with no plan gave complete=%v reason=%q first=%+v; want incomplete, opened on the first move's FROM for no time", c.Complete, c.Reason, c.Segments[0])
	}
}

func TestCardTimingAttemptsCarryOnlyTheAllowedFields(t *testing.T) {
	runs := []TaskRunRecord{
		{ID: "r2", TaskID: "t-1", DeskID: tmDesk, Attempt: 2, Status: RunStatusRunning, Model: "m", Provider: "claude",
			StartedAt: 5815, TokensIn: 7, TokensOut: 3, CostUSD: 0.5, CostBasis: CostEstimated,
			SessionID: "sess-SECRET-1", RequestID: "req-SECRET-1", LogRef: "log-SECRET-1", Workspace: "ws-SECRET-1"},
		{ID: "r1", TaskID: "t-1", DeskID: tmDesk, Attempt: 1, Status: RunStatusExited, Model: "m", Provider: "claude",
			StartedAt: 2205, EndedAt: 3405, TokensIn: 10, TokensOut: 5, CostUSD: 1.25, CostBasis: CostMeasured},
		{ID: "r3", TaskID: "t-9", Attempt: 1, StartedAt: 1, EndedAt: 2, CostUSD: 99, CostBasis: CostMeasured},
	}
	c := cardTiming(tmTask("t-1", StatusRunning), tmHistory()[:8], runs, 6000, RunnerManual)
	if len(c.Runs) != 2 || c.Runs[0].Attempt != 1 || c.Runs[0].Seconds != 1200 || c.Runs[1].Seconds != 185 {
		t.Fatalf("the attempts are %+v; want this task's two, in order, 1200 s ended and 185 s running to now", c.Runs)
	}
	if c.Attempts != 2 || c.TokensIn != 17 || c.TokensOut != 8 || c.CostMeasuredUSD != 1.25 || c.CostEstimatedUSD != 0.5 || c.CostUnavailable != 0 {
		t.Fatalf("the totals are attempts=%d tokens=%d/%d measured=%v estimated=%v; want 2, 17/8, and the cost kept per basis (BR-31)", c.Attempts, c.TokensIn, c.TokensOut, c.CostMeasuredUSD, c.CostEstimatedUSD)
	}
	raw, _ := json.Marshal(c)
	for _, leak := range []string{"SECRET", "sessionId", "requestId", "logRef", "workspace", "SessionID"} {
		if strings.Contains(string(raw), leak) {
			t.Fatalf("the timing JSON carries %q: %s -- an attempt goes out through the AttemptTiming allowlist only", leak, raw)
		}
	}
}

func TestWorkflowTimingTotals(t *testing.T) {
	a := cardTiming(tmTask("t-1", StatusDone), tmHistory(), nil, 20000, RunnerManual)
	b := cardTiming(tmTask("t-2", StatusDone), []Event{tmPlan(1, 500, "t-2"),
		tmMove(2, 600, "t-2", StatusPending, StatusReady, MoveSchedule),
		tmMove(3, 700, "t-2", StatusReady, StatusDispatched, MoveDispatch),
		tmMove(4, 800, "t-2", StatusDispatched, StatusDone, MoveApprove)}, nil, 20000, RunnerManual)
	w := workflowTiming("wf-1", []CardTiming{a, b}, 20000)
	if !w.Known || !w.Finished || w.StartedAt != 500 || w.FinishedAt != 9000 || w.LeadSeconds != 8500 ||
		w.AISeconds != a.AISeconds+b.AISeconds || w.IdleSeconds != 8500-w.AISeconds || w.Cards != 2 || w.Done != 2 {
		t.Fatalf("the workflow reads %+v; want started 500, finished 9000, lead 8500, AI the sum of its cards', idle lead - AI", w)
	}
	open := cardTiming(tmTask("t-3", StatusReady), []Event{tmPlan(1, 700, "t-3"),
		tmMove(2, 800, "t-3", StatusPending, StatusReady, MoveSchedule)}, nil, 20000, RunnerManual)
	w = workflowTiming("wf-1", []CardTiming{a, open}, 20000)
	if w.Finished || w.FinishedAt != 0 || w.LeadSeconds != 20000-700 {
		t.Fatalf("a workflow with an open card reads %+v; want it unfinished and its lead running to now", w)
	}
	u := a
	u.Known = false
	if w = workflowTiming("wf-1", []CardTiming{u, b}, 20000); w.Known || w.LeadSeconds != 0 {
		t.Fatalf("a workflow with an unknown card reads %+v; want it unknown, with no numbers", w)
	}
}

func TestTheTimingRoutesAnswer404ForAnUnknownCard(t *testing.T) {
	srv, _, desk := plannedProduct(t)
	r := httptest.NewRequest(http.MethodGet, "/products/"+desk+"/board/cards/wf-404-t-1/timing", nil)
	for _, c := range boCookies() {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, r)
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), `"error":"no such card"`) {
		t.Fatalf("the JSON route for an unknown card answered %d %s; want 404 {\"error\":\"no such card\"}", w.Code, w.Body.String())
	}
	h := do(t, srv, "GET", "/ui/timing/"+desk+"/wf-404-t-1", nil, boCookies()...)
	if h.Code != http.StatusNotFound || !strings.Contains(h.Body.String(), "That record is gone.") {
		t.Fatalf("the sheet for an unknown card answered %d; want 404 and the toast \"That record is gone.\"", h.Code)
	}
}

func TestTheBoardAndTheSheetCarryTheTiming(t *testing.T) {
	srv, s, desk := plannedProduct(t)
	id := productTasks(s, desk)[0].ID
	s.mu.Lock()
	s.runs = append(s.runs, TaskRunRecord{ID: runIDFor(id, 1), TaskID: id, DeskID: desk, Attempt: 1,
		Status: RunStatusExited, Model: "claude-x", Provider: "claude", StartedAt: time.Now().Unix() - 90, EndedAt: time.Now().Unix() - 30,
		TokensIn: 11, TokensOut: 22, CostUSD: 0.42, CostBasis: CostMeasured,
		SessionID: "sess-SECRET-2", RequestID: "req-SECRET-2", LogRef: "log-SECRET-2", Workspace: "ws-SECRET-2"})
	s.mu.Unlock()

	get := func(path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		for _, c := range boCookies() {
			r.AddCookie(c)
		}
		w := httptest.NewRecorder()
		srv.routes().ServeHTTP(w, r)
		return w
	}
	bw := get("/products/" + desk + "/board")
	var b struct {
		Cards []struct {
			ID     string          `json:"id"`
			Timing json.RawMessage `json:"timing"`
		} `json:"cards"`
		Workflows []WorkflowTiming `json:"workflows"`
	}
	if err := json.Unmarshal(bw.Body.Bytes(), &b); err != nil || len(b.Cards) == 0 || len(b.Workflows) == 0 {
		t.Fatalf("the board answered %d %s; want cards and workflows (%v)", bw.Code, bw.Body.String(), err)
	}
	for _, c := range b.Cards {
		var sum CardTimingSummary
		if json.Unmarshal(c.Timing, &sum) != nil || !sum.Known || !sum.Complete || sum.CreatedAt == 0 || sum.LeadText == "" {
			t.Fatalf("card %s carries timing %s; want a known, complete summary with a created time and a lead text", c.ID, c.Timing)
		}
	}
	jw := get("/products/" + desk + "/board/cards/" + id + "/timing")
	var ct CardTiming
	if err := json.Unmarshal(jw.Body.Bytes(), &ct); err != nil || jw.Code != 200 || len(ct.Segments) == 0 ||
		ct.Segments[0].Rule != "plan" || len(ct.Runs) != 1 || ct.Workflow == nil {
		t.Fatalf("the card's timing route answered %d %s; want its segments from the plan, its attempt and its workflow", jw.Code, jw.Body.String())
	}
	sheet := do(t, srv, "GET", "/ui/timing/"+desk+"/"+id, nil, boCookies()...)
	page := do(t, srv, "GET", "/products/"+desk+"?tab=board", nil, boCookies()...)
	body := sheet.Body.String()
	if sheet.Code != 200 || !strings.Contains(body, `id="timingModal"`) || strings.Count(body, `<li class="tm-step`) != len(ct.Segments) ||
		!strings.Contains(body, "Idle — no AI working on it") || !strings.Contains(body, "claude-x") {
		t.Fatalf("the timing sheet answered %d without the modal, one row per segment (%d), the idle total or the attempt", sheet.Code, len(ct.Segments))
	}
	if !strings.Contains(page.Body.String(), `data-timing-open="ui/timing/`+desk+`/`) {
		t.Fatalf("the server board's cards carry no timing chip")
	}
	for name, out := range map[string]string{"board": bw.Body.String(), "timing route": jw.Body.String(), "sheet": body} {
		if strings.Contains(out, "SECRET") {
			t.Fatalf("the %s carries a run's session, request, log or workspace id; only the AttemptTiming allowlist may leave the store", name)
		}
	}
}

func TestTheDatabaseZoneOffsetRoundsToAQuarterHour(t *testing.T) {
	for _, c := range []struct{ db, app, want int64 }{
		{1_000_000, 1_000_000 + 7*3600 + 40, 7 * 3600},
		{1_000_000 + 7*3600 - 25, 1_000_000, -7 * 3600},
		{1_000_000, 1_000_040, 0},
		{1_000_000, 1_000_000 + 5*3600 + 30*60 - 3, 5*3600 + 30*60},
	} {
		if got := tzOffset(c.db, c.app); got != c.want {
			t.Fatalf("tzOffset(%d, %d) = %d; want %d -- a zone, never a seconds skew", c.db, c.app, got, c.want)
		}
	}
}

func TestEventTimeSurvivesTheGraphRoundTrip(t *testing.T) {
	arc := arcadeTestTarget(t)
	ctx := context.Background()
	tenant := "conf-event-time"
	first := newArcadeTestStore(t, arc, tenant)
	if _, err := first.CreateProduct(ctx, ProductDraft{Name: "Timing", Owner: "ardi"}); err != nil {
		t.Fatalf("create product: %v", err)
	}
	before := first.(*ArcadeStore).proj.rawEvents()
	// A row written the way a pre-v0.41.0 build wrote it: no "at" in the payload.
	old := before[len(before)-1].Seq + 1
	if err := arc.Command(ctx, `INSERT INTO Event SET id = :id, tenantId = :t, seq = :seq, type = 'RecordUpdated',
		 entityId = 'p-1', entityType = 'Workflow', actor = 'operator', ts = sysdate(), payload = :p`,
		map[string]any{"id": fmt.Sprintf("%s-ev-%d", tenant, old), "t": tenant, "seq": old,
			"p": map[string]any{"text": "old row", "kind": "info", "clock": 0}}, nil); err != nil {
		t.Fatalf("insert an old-style row: %v", err)
	}
	second, err := NewArcadeStoreFor(ctx, arc, 1, tenant)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	after := map[int]Event{}
	for _, e := range second.proj.rawEvents() {
		after[e.Seq] = e
	}
	for _, e := range before {
		if e.At == 0 || after[e.Seq].At != e.At {
			t.Fatalf("event %d had At %d and reads back %d; the wall clock must survive a restart", e.Seq, e.At, after[e.Seq].At)
		}
	}
	if got, now := after[old].At, time.Now().Unix(); got < now-300 || got > now+300 {
		t.Fatalf("a row with no \"at\" read back At %d at %d; want its ts, corrected to the app's clock", got, now)
	}
}

// TestALeftDoneSegmentIsBookedAsBlocked is D-084's timing half: a design sent
// back leaves DONE, and the segment it left must still count, or the partition
// would lose it. It is booked as blocked -- a convention, stated in D-084.
func TestALeftDoneSegmentIsBookedAsBlocked(t *testing.T) {
	evs := []Event{
		tmPlan(1, 1000, "t-1"),
		tmMove(2, 1100, "t-1", StatusPending, StatusReady, MoveSchedule),
		tmMove(3, 1200, "t-1", StatusReady, StatusDispatched, MoveDispatch),
		tmMove(4, 1210, "t-1", StatusDispatched, StatusRunning, MoveStart),
		tmMove(5, 1810, "t-1", StatusRunning, StatusReview, MoveExit),
		tmMove(6, 1810, "t-1", StatusReview, StatusDone, RuleDesignDelivered),
		tmMove(7, 4810, "t-1", StatusDone, StatusReady, MoveSendBack),
		tmMove(8, 4900, "t-1", StatusReady, StatusDispatched, MoveDispatch),
		tmMove(9, 4910, "t-1", StatusDispatched, StatusRunning, MoveStart),
		tmMove(10, 5510, "t-1", StatusRunning, StatusReview, MoveExit),
		tmMove(11, 5510, "t-1", StatusReview, StatusDone, RuleDesignDelivered),
	}
	c := cardTiming(tmTask("t-1", StatusDone), evs, nil, 9999, RunnerManual)
	if !c.Known || !c.Complete || !c.Finished {
		t.Fatalf("a design sent back once is not a complete, finished history: %+v", c)
	}
	var left *TimingSegment
	for i := range c.Segments {
		if c.Segments[i].Status == StatusDone && !c.Segments[i].Open && c.Segments[i].Seconds > 0 {
			left = &c.Segments[i]
		}
	}
	if left == nil {
		t.Fatalf("no left DONE segment in %+v", c.Segments)
	}
	if left.Bucket != BucketBlocked || left.Seconds != 3000 {
		t.Errorf("the DONE segment the send-back left is %s for %ds; it must be blocked for 3000s", left.Bucket, left.Seconds)
	}
	if c.LeadSeconds != 5510-1000 || partitionSum(c) != c.LeadSeconds {
		t.Errorf("lead %d, buckets sum %d; both must be %d", c.LeadSeconds, partitionSum(c), 5510-1000)
	}
}
