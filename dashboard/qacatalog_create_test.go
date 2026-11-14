package main

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// The QA tools' create-only door and the cards it may add (qacatalog_create.go,
// design §4.12).

// workflowFixture is a created product with a gated three-task workflow:
// design, review, implement.
func workflowFixture(t *testing.T, st *MemStore, name string) string {
	t.Helper()
	ctx := context.Background()
	if _, err := st.CreateProduct(ctx, ProductDraft{Name: name}); err != nil {
		t.Fatal(err)
	}
	ms, _ := st.Masters(ctx)
	id := ms[len(ms)-1].ID
	if _, err := st.AddStory(ctx, id, Story{Pri: "must", Role: "qa", Want: "w " + name, So: "s", AC: []string{"one", "two"}}); err != nil {
		t.Fatal(err)
	}
	plan := []PlanTask{
		{ID: "t-1", Title: "Design", Role: "architect", Kind: TaskKindDesign},
		{ID: "t-2", Title: "Review", Role: "qa", Kind: TaskKindReview, Deps: []string{"t-1"}},
		{ID: "t-3", Title: "Build", Role: "qa", Story: "US-01", Kind: TaskKindImplement, Deps: []string{"t-2"}},
	}
	if _, err := st.CreateWorkflow(ctx, WorkflowDraft{ProductID: id, Goal: "plan " + name, Stories: []string{"US-01"}}, plan); err != nil {
		t.Fatal(err)
	}
	return id
}

func statusesOf(st *MemStore, pid string) map[string]string {
	out := map[string]string{}
	for _, tk := range st.tasksSnapshot(pid) {
		out[tk.ID] = tk.Status + "|" + tk.BlockReason
	}
	return out
}

func postCases(t *testing.T, srv *server, pid, token string, body any) (int, QaCreateOut, string) {
	t.Helper()
	b, _ := json.Marshal(body)
	rec := call(t, srv, "POST", "/v1/products/"+pid+"/qa/cases", token, string(b))
	var out QaCreateOut
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out, rec.Body.String()
}

func newCase(id, epic string) map[string]any {
	return map[string]any{"caseId": id, "epic": epic, "module": "", "features": []string{}, "userStory": "",
		"scenario": "Given a, when b, then c", "polarity": "+", "level": "API", "pri": "P1", "decision": "planned", "reason": ""}
}

func TestQaToolsMayCreateButNeverChangeACase(t *testing.T) {
	srv, st := newTestServer(t)
	srv.api = newAPIConfig(tokAdmin, tokQA, tokOut, "p-6")
	pid := workflowFixture(t, st, "Create Probe")
	srv.api = newAPIConfig(tokAdmin, tokQA, tokOut, pid)
	ctx := context.Background()

	// A new valid case is created planned.
	code, out, body := postCases(t, srv, pid, tokQA, map[string]any{"source": "qa", "cases": []any{newCase("TC-AUTH-001", "AUTH")}})
	if code != 200 || out.Created != 1 || out.EpicsAdded != 1 {
		t.Fatalf("create = %d %s, want 200, 1 created, 1 epic", code, body)
	}
	pg, _ := st.QaCases(ctx, pid, QaQuery{})
	if len(pg.Items) != 1 || pg.Items[0].Decision != DecisionPlanned || pg.Items[0].Implemented {
		t.Errorf("the created case: %+v", pg.Items)
	}
	// Identical again: unchanged. Different: exists-differs, and not changed.
	if _, o, _ := postCases(t, srv, pid, tokQA, map[string]any{"cases": []any{newCase("TC-AUTH-001", "AUTH")}}); o.Unchanged != 1 || o.Created != 0 {
		t.Errorf("an identical re-post: %+v, want unchanged", o)
	}
	diff := newCase("TC-AUTH-001", "AUTH")
	diff["scenario"] = "Given other, when changed, then it is refused"
	_, o, _ := postCases(t, srv, pid, tokQA, map[string]any{"cases": []any{diff}})
	if len(o.Rejected) != 1 || o.Rejected[0].Reason != "exists-differs" {
		t.Errorf("a differing existing case: %+v, want exists-differs", o)
	}
	if pg, _ := st.QaCases(ctx, pid, QaQuery{}); pg.Items[0].Scenario != "Given a, when b, then c" {
		t.Errorf("the QA token changed an existing case's planning half: %q", pg.Items[0].Scenario)
	}

	// The refusals, each by its code.
	mut := func(f func(c map[string]any)) map[string]any {
		c := newCase("TC-AUTH-010", "AUTH")
		f(c)
		return c
	}
	for want, c := range map[string]map[string]any{
		"decision-not-allowed": mut(func(c map[string]any) { c["decision"], c["reason"] = "blocked", "defect:DEF-1" }),
		"use-results":          mut(func(c map[string]any) { c["result"], c["runAt"] = "pass", 1 }),
		"bad-case-epic":        mut(func(c map[string]any) { c["epic"] = "BILL" }),
		"bad-scenario":         mut(func(c map[string]any) { c["scenario"] = " " }),
		"bad-spec":             mut(func(c map[string]any) { c["implemented"], c["spec"] = true, "/abs" }),
	} {
		_, o, _ := postCases(t, srv, pid, tokQA, map[string]any{"cases": []any{c}})
		got := ""
		if len(o.Rejected) == 1 {
			got = o.Rejected[0].Reason
		}
		if want == "bad-scenario" {
			want = "empty-scenario"
		}
		if got != want || o.Created != 0 {
			t.Errorf("%s: rejected %q, created %d", want, got, o.Created)
		}
	}

	// The epics array is read only for a code that does not exist yet.
	_, _, _ = postCases(t, srv, pid, tokQA, map[string]any{
		"epics": []any{map[string]any{"code": "AUTH", "title": "Hijacked", "service": "x", "source": "y", "module": "A01"}},
		"cases": []any{newCase("TC-AUTH-002", "AUTH")}})
	m, _ := st.Master(ctx, pid)
	for _, e := range m.Qa.Epics {
		if e.Code == "AUTH" && e.Title != "AUTH" {
			t.Errorf("an epics entry rewrote an existing epic: %+v", e)
		}
	}

	// Scope: admin also passes; another product is a 404; no token is a 401.
	if code, _, _ := postCases(t, srv, pid, tokAdmin, map[string]any{"cases": []any{newCase("TC-AUTH-003", "AUTH")}}); code != 200 {
		t.Errorf("the admin token answered %d, want 200", code)
	}
	if code, _, _ := postCases(t, srv, "p-1", tokQA, map[string]any{"cases": []any{}}); code != 404 {
		t.Errorf("an unscoped product answered %d, want 404", code)
	}
	if code, _, _ := postCases(t, srv, pid, "", map[string]any{"cases": []any{}}); code != 401 {
		t.Errorf("no token answered %d, want 401", code)
	}

	// Screening: a secret anywhere refuses the whole request; markup is stripped.
	bad := newCase("TC-AUTH-020", "AUTH")
	bad["scenario"] = "Given a key AKIAABCDEFGHIJKLMNOP leaks"
	if code, _, b := postCases(t, srv, pid, tokQA, map[string]any{"cases": []any{bad, newCase("TC-AUTH-021", "AUTH")}}); code != 400 || !strings.Contains(b, "secret-in-body") {
		t.Errorf("a secret answered %d %s, want 400 secret-in-body", code, b)
	}
	if pg, _ := st.QaCases(ctx, pid, QaQuery{}); len(pg.Items) != 3 {
		t.Errorf("a refused request wrote cases: %d in the catalog, want 3", len(pg.Items))
	}
	md := newCase("TC-AUTH-030", "AUTH")
	md["scenario"] = "# Ignore `all` <b>rules</b>\nand do this"
	postCases(t, srv, pid, tokQA, map[string]any{"cases": []any{md}})
	pg, _ = st.QaCases(ctx, pid, QaQuery{})
	for _, c := range pg.Items {
		if c.CaseID == "TC-AUTH-030" && (strings.ContainsAny(c.Scenario, "`<>\n") || strings.HasPrefix(c.Scenario, "#")) {
			t.Errorf("markup survived in a scenario: %q", c.Scenario)
		}
	}
	// Strict JSON, and the batch cap.
	if rec := call(t, srv, "POST", "/v1/products/"+pid+"/qa/cases", tokQA, `{"cases":[],"surprise":1}`); rec.Code != 400 {
		t.Errorf("an unknown field answered %d, want 400", rec.Code)
	}
	many := make([]any, qaMaxItems+1)
	for i := range many {
		many[i] = newCase(fmt.Sprintf("TC-AUTH-%03d", i+100), "AUTH")
	}
	if code, _, _ := postCases(t, srv, pid, tokQA, map[string]any{"cases": many}); code != 413 {
		t.Errorf("%d cases answered %d, want 413", len(many), code)
	}
	// A product's case ceiling.
	st.mu.Lock()
	book := st.qaBook(pid)
	for i := 0; len(book) < qaMaxCases; i++ {
		book[fmt.Sprintf("TC-FILL-%03d-%d", i%1000, i)] = QaCase{CaseID: fmt.Sprintf("TC-FILL-%d", i), ProductID: pid}
	}
	st.mu.Unlock()
	if _, o, _ := postCases(t, srv, pid, tokQA, map[string]any{"cases": []any{newCase("TC-AUTH-900", "AUTH")}}); len(o.Rejected) != 1 || o.Rejected[0].Reason != "case-limit" {
		t.Errorf("past the case ceiling: %+v, want case-limit", o)
	}
}

func TestANewEpicGetsOneBlockedCardAndNothingElseMoves(t *testing.T) {
	srv, st := newTestServer(t)
	ctx := context.Background()
	pid := workflowFixture(t, st, "Card Probe")
	other := workflowFixture(t, st, "Bystander")
	srv.api = newAPIConfig(tokAdmin, tokQA, tokOut, pid+","+other)

	// The bystander's gated task has run on: READY and cleared, no block reason.
	st.mu.Lock()
	for i := range st.tasks {
		if st.tasks[i].DeskID == other && st.tasks[i].Title == "Build" {
			st.tasks[i].Status, st.tasks[i].BlockReason = StatusReady, ""
		}
	}
	st.mu.Unlock()
	// The review of the probe has CLEARED: that must not release a QA card.
	st.mu.Lock()
	for i := range st.tasks {
		if st.tasks[i].DeskID == pid && st.tasks[i].Title == "Review" {
			st.tasks[i].Status = StatusDone
		}
	}
	st.mu.Unlock()
	before, beforeOther := statusesOf(st, pid), statusesOf(st, other)
	evTP, evTM := countEventsOfType(st, TaskPlanned), countEventsOfType(st, TaskMoved)

	code, out, body := postCases(t, srv, pid, tokQA, map[string]any{
		"epics": []any{map[string]any{"code": "NEW", "title": "New Epic", "service": "svc", "source": "docs/x.md"}},
		"cases": []any{newCase("TC-NEW-001", "NEW"), newCase("TC-NEW-002", "NEW")}})
	if code != 200 || out.CardsAdded != 1 || out.EpicsAdded != 1 || out.Created != 2 {
		t.Fatalf("create = %d %s, want 1 epic, 1 card, 2 cases", code, body)
	}
	var card *TaskRecord
	for _, tk := range st.tasksSnapshot(pid) {
		if tk.Title == "Automate: New Epic" {
			tk := tk
			card = &tk
		}
	}
	if card == nil {
		t.Fatal("no card was added")
	}
	if card.Status != StatusBlocked || card.BlockReason != ReasonQaEpicAdded || card.Kind != TaskKindImplement ||
		card.Story != "" || len(card.Deps) != 1 || card.Ordinal != 3 {
		t.Errorf("the card = %+v, want BLOCKED with the QA reason, implement, Plan lane, one dep, ordinal 3", *card)
	}
	if countEventsOfType(st, TaskPlanned) != evTP+1 || countEventsOfType(st, TaskMoved) != evTM+1 {
		t.Error("a new card must write exactly one TaskPlanned and one TaskMoved")
	}
	// Nothing else moved: this product's tasks, and the bystander's, are as they were.
	after := statusesOf(st, pid)
	for id, was := range before {
		if after[id] != was {
			t.Errorf("task %s went from %q to %q", id, was, after[id])
		}
	}
	if !reflect.DeepEqual(beforeOther, statusesOf(st, other)) {
		t.Error("a QA write changed another product's tasks")
	}
	m, _ := st.Master(ctx, pid)
	if m.Qa.Epics[0].Task != card.ID {
		t.Errorf("the epic's card id = %q, want %q", m.Qa.Epics[0].Task, card.ID)
	}
	// A second case in the epic adds no card; neither does a repeat.
	if _, o, _ := postCases(t, srv, pid, tokQA, map[string]any{"cases": []any{newCase("TC-NEW-003", "NEW")}}); o.CardsAdded != 0 || o.EpicsAdded != 0 {
		t.Errorf("a second case in the epic: %+v, want no new card", o)
	}
	// The fold rebuilds the same card, ordinal included.
	var folded *TaskRecord
	for _, tk := range foldTasks(st.rawEvents(), pid) {
		if tk.ID == card.ID {
			tk := tk
			folded = &tk
		}
	}
	if folded == nil || folded.Status != StatusBlocked || folded.Ordinal != card.Ordinal {
		t.Errorf("the fold's card = %+v, want BLOCKED at ordinal %d", folded, card.Ordinal)
	}
	// The board carries live counts on the card and nowhere else.
	b, _ := st.Board(ctx, pid)
	for _, c := range b.Cards {
		if c.ID == card.ID {
			if c.Qa == nil || c.Qa.Cases != 3 || c.Qa.Epic != "NEW" {
				t.Errorf("the card's badge = %+v, want 3 cases of NEW", c.Qa)
			}
		} else if c.Qa != nil {
			t.Errorf("card %q carries a badge it should not", c.Label)
		}
	}

	// A product with no workflow records the epic and makes no card.
	if _, err := st.CreateProduct(ctx, ProductDraft{Name: "No Workflow"}); err != nil {
		t.Fatal(err)
	}
	ms, _ := st.Masters(ctx)
	nw := ms[len(ms)-1].ID
	srv.api = newAPIConfig(tokAdmin, tokQA, tokOut, pid+","+nw)
	_, o, _ := postCases(t, srv, nw, tokQA, map[string]any{"cases": []any{newCase("TC-ZED-001", "ZED")}})
	if o.EpicsAdded != 1 || o.CardsAdded != 0 || o.CardsDeferred != "no-workflow" {
		t.Errorf("no workflow: %+v, want an epic, no card, no-workflow", o)
	}
	if sum, _ := st.QaSummary(ctx, nw); len(sum.NoCard) != 1 || sum.NoCard[0] != "ZED" {
		t.Errorf("the summary's noCard = %v, want [ZED]", sum.NoCard)
	}

	// Parked: the epic is recorded, the card deferred, and the live desks are untouched.
	st.mu.RLock()
	liveDesks := make([]Desk, len(st.desks))
	for i, d := range st.desks {
		liveDesks[i] = d.Clone()
	}
	st.mu.RUnlock()
	if _, err := st.Replay(ctx, 1); err != nil {
		t.Fatal(err)
	}
	_, o, _ = postCases(t, srv, pid, tokQA, map[string]any{"cases": []any{newCase("TC-PRK-001", "PRK")}})
	if o.EpicsAdded != 1 || o.CardsAdded != 0 || o.CardsDeferred != "replay" {
		t.Errorf("parked: %+v, want an epic, no card, replay", o)
	}
	st.mu.RLock()
	same := reflect.DeepEqual(liveDesks, st.desks)
	st.mu.RUnlock()
	if !same {
		t.Error("a QA write changed the live desks while the floor was parked")
	}
	_, _ = st.Replay(ctx, -1)

	// Caps: at most 5 new epics per request, 100 per product.
	var six []any
	for i := 0; i < 6; i++ {
		six = append(six, newCase(fmt.Sprintf("TC-EP%c-001", 'A'+i), fmt.Sprintf("EP%c", 'A'+i)))
	}
	_, o, _ = postCases(t, srv, pid, tokQA, map[string]any{"cases": six})
	if o.EpicsAdded != 5 || len(o.Rejected) != 1 || o.Rejected[0].Reason != "epic-limit" {
		t.Errorf("six new epics: %+v, want 5 added and 1 epic-limit", o)
	}
	st.mu.Lock()
	mm := st.masterPtr(pid)
	for i := len(mm.Qa.Epics); i < qaMaxEpics; i++ {
		mm.Qa.Epics = append(mm.Qa.Epics, QaEpic{Code: fmt.Sprintf("FL%c%c", 'A'+i/26, 'A'+i%26), Title: "fill"})
	}
	st.mu.Unlock()
	if _, o, _ := postCases(t, srv, pid, tokQA, map[string]any{"cases": []any{newCase("TC-OVER-001", "OVER")}}); len(o.Rejected) != 1 || o.Rejected[0].Reason != "epic-limit" {
		t.Errorf("past the epic ceiling: %+v, want epic-limit", o)
	}

	// CreateWorkflow's gate loop touches only its own workflow: planning a third
	// product left the bystander's cleared task alone (checked at the top by
	// statusesOf(other) staying equal across the writes above).
	if got := statusesOf(st, other)[firstTaskTitled(st, other, "Build")]; got != StatusReady+"|" {
		t.Errorf("the bystander's task = %q after other products were planned", got)
	}
	workflowFixture(t, st, "Third")
	if got := statusesOf(st, other)[firstTaskTitled(st, other, "Build")]; got != StatusReady+"|" {
		t.Errorf("planning another product re-blocked the bystander's task: %q", got)
	}
}

func firstTaskTitled(st *MemStore, pid, title string) string {
	for _, tk := range st.tasksSnapshot(pid) {
		if tk.Title == title {
			return tk.ID
		}
	}
	return ""
}

func TestTheBoardCardCarriesLiveCaseCounts(t *testing.T) {
	srv, st := newTestServer(t)
	srv.api = newAPIConfig(tokAdmin, tokQA, tokOut, "")
	ctx := context.Background()
	var product ProductSeed
	raw := readSeedFile(t, "product.json", &product)
	code, seeded, body := seedCall(t, srv, raw)
	if code != 200 {
		t.Fatalf("seed: %d %s", code, body)
	}
	pid := seeded.ProductID
	m, _ := st.Master(ctx, pid)
	for _, e := range m.Qa.Epics {
		if e.Task == "" {
			t.Errorf("epic %s has no card id after the seed", e.Code)
		}
	}
	b, _ := st.Board(ctx, pid)
	withBadge := 0
	for _, c := range b.Cards {
		if c.Qa != nil {
			withBadge++
			if c.Qa.Cases != 0 {
				t.Errorf("%q shows %d cases before any case exists", c.Label, c.Qa.Cases)
			}
		} else if strings.HasPrefix(c.Label, "Automate: ") {
			t.Errorf("epic card %q has no badge", c.Label)
		}
	}
	if withBadge != len(product.Epics) {
		t.Errorf("%d badges, want %d (one per epic card, none on design, review or release)", withBadge, len(product.Epics))
	}
	// Cases and results move the badge, and the task is not written.
	taskBefore := statusesOf(st, pid)
	if _, _, err := st.QaImport(ctx, pid, QaImportIn{Cases: []QaCase{qaCase("TC-AUTH-001"), qaCase("TC-AUTH-002")}}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.QaApplyResults(ctx, pid, QaResultsIn{Items: []QaResultItem{
		{CaseID: "TC-AUTH-001", Implemented: boolp(true), Spec: strp("a.spec.ts"), Result: strp("fail"), RunAt: i64p(qaNow() - 5)}}}); err != nil {
		t.Fatal(err)
	}
	b, _ = st.Board(ctx, pid)
	for _, c := range b.Cards {
		if c.Qa != nil && c.Qa.Epic == "AUTH" && (c.Qa.Cases != 2 || c.Qa.Automated != 1 || c.Qa.Fail != 1 || c.Qa.Pct != 50) {
			t.Errorf("the AUTH badge = %+v, want 2 cases, 1 automated, 1 failing, 50%%", c.Qa)
		}
	}
	if !reflect.DeepEqual(taskBefore, statusesOf(st, pid)) {
		t.Error("a case or a result changed a task")
	}
	if sum, _ := st.QaSummary(ctx, pid); len(sum.NoCard) != 0 {
		t.Errorf("noCard = %v after a full seed, want none", sum.NoCard)
	}
}
