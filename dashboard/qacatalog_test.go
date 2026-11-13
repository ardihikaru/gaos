package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// The QA catalog's rules (D-072, BR-51, BR-54). Each test is named for what a
// regression would break, and each failure message says what was seen.

func qaFixture(t *testing.T) (*MemStore, string) {
	t.Helper()
	s := NewMemStore(1)
	if _, err := s.CreateProduct(context.Background(), ProductDraft{Name: "QA Probe", Owner: "ardi", Priority: "high"}); err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}
	ms, _ := s.Masters(context.Background())
	return s, ms[len(ms)-1].ID
}

func qaCase(id string) QaCase {
	return QaCase{CaseID: id, Epic: "AUTH", Module: "A13", Features: []string{"A13.03"}, UserStory: "US-AUTH-01",
		Scenario: "Given a user, when they sign in, then they see the home page", Polarity: "+", Level: "WEB",
		Pri: "P0", Decision: DecisionPlanned}
}

func importCases(t *testing.T, s *MemStore, pid string, seed bool, cs ...QaCase) QaImportOut {
	t.Helper()
	out, _, err := s.QaImport(context.Background(), pid, QaImportIn{Seed: seed, Cases: cs})
	if err != nil {
		t.Fatalf("QaImport: %v", err)
	}
	return out
}

func strp(s string) *string { return &s }
func boolp(b bool) *bool    { return &b }
func i64p(n int64) *int64   { return &n }

func countEventsOfType(s *MemStore, typ EventType) int {
	n := 0
	for _, e := range s.rawEvents() {
		if e.Type == typ {
			n++
		}
	}
	return n
}

func TestQaCaseVocabularyIsClosed(t *testing.T) {
	s, pid := qaFixture(t)
	bad := map[string]func(c *QaCase){
		"bad-case-id":       func(c *QaCase) { c.CaseID = "TC-auth-1" },
		"bad-epic":          func(c *QaCase) { c.Epic = "a" },
		"bad-module":        func(c *QaCase) { c.Module = "Z99" },
		"bad-user-story":    func(c *QaCase) { c.UserStory = "US-1" },
		"bad-feature":       func(c *QaCase) { c.Features = []string{"A1.1"} },
		"too-many-features": func(c *QaCase) { c.Features = make([]string, 13) },
		"empty-scenario":    func(c *QaCase) { c.Scenario = "  " },
		"scenario-too-long": func(c *QaCase) { c.Scenario = strings.Repeat("x", qaMaxScenario+1) },
		"bad-polarity":      func(c *QaCase) { c.Polarity = "?" },
		"bad-level":         func(c *QaCase) { c.Level = "UI" },
		"bad-pri":           func(c *QaCase) { c.Pri = "P9" },
		"bad-decision":      func(c *QaCase) { c.Decision = "automated" },
		"reason-on-planned": func(c *QaCase) { c.Reason = "other:why" },
		"bad-reason":        func(c *QaCase) { c.Decision, c.Reason = DecisionBlocked, "no colon" },
		"bad-reason-cat":    func(c *QaCase) { c.Decision, c.Reason = DecisionWont, "whim:because" },
		"bad-reason-empty":  func(c *QaCase) { c.Decision, c.Reason = DecisionWont, "other:  " },
	}
	for want, mut := range bad {
		c := qaCase("TC-AUTH-001")
		mut(&c)
		out := importCases(t, s, pid, false, c)
		code := want
		if want == "bad-reason-cat" || want == "bad-reason-empty" {
			code = "bad-reason"
		}
		if len(out.Rejected) != 1 || out.Rejected[0].Reason != code || out.Created != 0 {
			t.Errorf("%s: want one rejection %q and nothing created, got %+v", want, code, out)
		}
	}
	// And the good ones go in: every decision with its reason shape.
	ok := qaCase("TC-AUTH-002")
	blocked := qaCase("TC-AUTH-003")
	blocked.Decision, blocked.Reason = DecisionBlocked, "defect:DEF-152"
	if out := importCases(t, s, pid, false, ok, blocked); out.Created != 2 || len(out.Rejected) != 0 {
		t.Errorf("valid cases refused: %+v", out)
	}
	// A spec must be relative: no leading slash, no "..".
	for _, spec := range []string{"/etc/passwd", "../x", "a/../../b", `a\b`} {
		c := qaCase("TC-AUTH-004")
		c.Implemented, c.Spec = true, spec
		if out := importCases(t, s, pid, true, c); out.Created != 0 || len(out.Rejected) != 1 || out.Rejected[0].Reason != "bad-spec" {
			t.Errorf("spec %q was not refused as bad-spec: %+v", spec, out)
		}
	}
}

func TestEffectiveStatusAndNeedsDecision(t *testing.T) {
	s, pid := qaFixture(t)
	a, b := qaCase("TC-AUTH-001"), qaCase("TC-AUTH-002")
	b.Decision, b.Reason = DecisionBlocked, "blocked:needs seed data"
	a.Implemented, a.Spec = true, "tests/a.spec.ts"
	b.Implemented, b.Spec = true, "tests/b.spec.ts"
	importCases(t, s, pid, true, a, b)
	sum, err := s.QaSummary(context.Background(), pid)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Totals.Automated != 1 || sum.Totals.Blocked != 1 {
		t.Errorf("planned+implemented must be automated and blocked+implemented must stay blocked, got %+v", sum.Totals)
	}
	if len(sum.NeedsDecision) != 1 || sum.NeedsDecision[0] != "TC-AUTH-002" {
		t.Errorf("a script on a parked case must be listed as needs-decision, got %v", sum.NeedsDecision)
	}
	page, _ := s.QaCases(context.Background(), pid, QaQuery{})
	for _, c := range page.Items {
		want := map[string]string{"TC-AUTH-001": "automated", "TC-AUTH-002": "blocked"}[c.CaseID]
		if c.Status != want {
			t.Errorf("%s: status %q, want %q", c.CaseID, c.Status, want)
		}
	}
}

func TestImportAndResultsOwnTheirOwnHalf(t *testing.T) {
	s, pid := qaFixture(t)
	ctx := context.Background()
	c := qaCase("TC-AUTH-001")
	c.Implemented, c.Spec = true, "tests/a.spec.ts"

	// Without seed, a NEW case is born without script facts.
	importCases(t, s, pid, false, c)
	got, _ := s.QaCases(ctx, pid, QaQuery{})
	if got.Items[0].Implemented || got.Items[0].Spec != "" {
		t.Fatalf("an import without seed wrote the automation half: %+v", got.Items[0])
	}
	// A results post sets it.
	if _, _, err := s.QaApplyResults(ctx, pid, QaResultsIn{Items: []QaResultItem{{CaseID: "TC-AUTH-001", Implemented: boolp(true), Spec: strp("tests/a.spec.ts")}}}); err != nil {
		t.Fatal(err)
	}
	// A later import, even seeded, never rewrites the automation half of an existing case.
	c.Implemented, c.Spec, c.Scenario = false, "tests/other.spec.ts", "Given x, when y, then z"
	if out := importCases(t, s, pid, true, c); out.Updated != 1 {
		t.Fatalf("a changed scenario must update the planning half: %+v", out)
	}
	got, _ = s.QaCases(ctx, pid, QaQuery{})
	if !got.Items[0].Implemented || got.Items[0].Spec != "tests/a.spec.ts" {
		t.Errorf("an import overwrote the automation half: %+v", got.Items[0])
	}
	if got.Items[0].Scenario != "Given x, when y, then z" {
		t.Errorf("the import did not change the planning half: %+v", got.Items[0])
	}
	// And a results post cannot change a planning field: it has none to carry.
	var in QaResultsIn
	if err := decodeStrict(strings.NewReader(`{"items":[{"caseId":"TC-AUTH-001","decision":"wont"}]}`), &in); err == nil {
		t.Error("a results item naming a planning field must be a decode error, got none")
	}
	// Seed creates with script facts only for a case not yet present.
	n := qaCase("TC-AUTH-009")
	n.Implemented, n.Spec = true, "tests/n.spec.ts"
	importCases(t, s, pid, true, n)
	got, _ = s.QaCases(ctx, pid, QaQuery{})
	for _, it := range got.Items {
		if it.CaseID == "TC-AUTH-009" && (!it.Implemented || it.Spec != "tests/n.spec.ts") {
			t.Errorf("a seeded new case lost its script facts: %+v", it)
		}
	}
}

func TestAnIdenticalRepostChangesNothing(t *testing.T) {
	s, pid := qaFixture(t)
	ctx := context.Background()
	c := qaCase("TC-AUTH-001")
	importCases(t, s, pid, false, c)
	head := func() int { m, _ := s.Master(ctx, pid); return m.Qa.CatHead }
	h0, ev0 := head(), countEventsOfType(s, RecordUpdated)
	out := importCases(t, s, pid, false, c)
	if out.Unchanged != 1 || out.Created+out.Updated != 0 {
		t.Errorf("an identical re-post must be unchanged, got %+v", out)
	}
	if head() != h0 || countEventsOfType(s, RecordUpdated) != ev0 {
		t.Errorf("an identical re-post moved the revision (%d -> %d) or emitted an event", h0, head())
	}
	items := []QaResultItem{{CaseID: "TC-AUTH-001", Result: strp("pass"), RunAt: i64p(qaNow() - 10)}}
	if o, _, _ := s.QaApplyResults(ctx, pid, QaResultsIn{Items: items}); o.Applied != 1 {
		t.Fatalf("first results post must apply: %+v", o)
	}
	ev1 := countEventsOfType(s, RecordUpdated)
	if o, _, _ := s.QaApplyResults(ctx, pid, QaResultsIn{Items: items}); o.Unchanged != 1 || o.Applied != 0 {
		t.Errorf("an identical results post must be unchanged, got %+v", o)
	}
	if countEventsOfType(s, RecordUpdated) != ev1 {
		t.Error("an unchanged results post emitted an event")
	}
}

func TestResultsRejectUnknownStaleAndFuture(t *testing.T) {
	s, pid := qaFixture(t)
	ctx := context.Background()
	importCases(t, s, pid, false, qaCase("TC-AUTH-001"), qaCase("TC-AUTH-002"))
	now := qaNow()
	apply := func(force bool, it ...QaResultItem) QaResultsOut {
		o, _, err := s.QaApplyResults(ctx, pid, QaResultsIn{Force: force, Items: it})
		if err != nil {
			t.Fatal(err)
		}
		return o
	}
	reasons := func(o QaResultsOut) map[string]string {
		m := map[string]string{}
		for _, r := range o.Rejected {
			m[r.CaseID] = r.Reason
		}
		return m
	}

	o := apply(false,
		QaResultItem{CaseID: "TC-AUTH-404", Implemented: boolp(true)},
		QaResultItem{CaseID: "TC-AUTH-001", Result: strp("pass"), RunAt: i64p(now - 100)},
		QaResultItem{CaseID: "TC-AUTH-002", Result: strp("fail")}, // a result with no runAt
	)
	r := reasons(o)
	if r["TC-AUTH-404"] != "unknown-case" || r["TC-AUTH-002"] != "run-incomplete" || o.Applied != 1 {
		t.Errorf("unknown case and a run without runAt must be rejected while the valid item applies: %+v", o)
	}
	// stale: an older run does not overwrite a newer one.
	if r := reasons(apply(false, QaResultItem{CaseID: "TC-AUTH-001", Result: strp("fail"), RunAt: i64p(now - 200)})); r["TC-AUTH-001"] != "stale" {
		t.Errorf("an older run must be stale, got %v", r)
	}
	// future: a millisecond timestamp must not lock the case.
	if r := reasons(apply(false, QaResultItem{CaseID: "TC-AUTH-001", Result: strp("pass"), RunAt: i64p(now * 1000)})); r["TC-AUTH-001"] != "future" {
		t.Errorf("a timestamp in the future must be rejected, got %v", r)
	}
	// An equal run with equal fields is unchanged; the same instant with a new verdict applies.
	if o := apply(false, QaResultItem{CaseID: "TC-AUTH-001", Result: strp("pass"), RunAt: i64p(now - 100)}); o.Unchanged != 1 {
		t.Errorf("an equal run must be unchanged: %+v", o)
	}
	// force (the admin repair path) skips stale.
	if o := apply(true, QaResultItem{CaseID: "TC-AUTH-001", Result: strp("fail"), RunAt: i64p(now - 500)}); o.Applied != 1 {
		t.Errorf("force must override stale: %+v", o)
	}
	// bad values
	for name, it := range map[string]QaResultItem{
		"bad-result":  {CaseID: "TC-AUTH-001", Result: strp("maybe"), RunAt: i64p(now)},
		"bad-spec":    {CaseID: "TC-AUTH-001", Spec: strp("/abs")},
		"bad-run-ref": {CaseID: "TC-AUTH-001", Result: strp("pass"), RunAt: i64p(now), RunRef: strp("a\nb")},
	} {
		if r := reasons(apply(true, it)); r["TC-AUTH-001"] != name {
			t.Errorf("%s: got %v", name, r)
		}
	}
}

func TestACreatedProductGetsADeskAndCanBePlanned(t *testing.T) {
	ctx := context.Background()
	s := NewMemStore(1)
	before, _ := s.Snapshot(ctx)
	if _, err := s.CreateProduct(ctx, ProductDraft{Name: "Runtime Product", Owner: "ardi", Priority: "high"}); err != nil {
		t.Fatal(err)
	}
	ms, _ := s.Masters(ctx)
	id := ms[len(ms)-1].ID
	if ms[len(ms)-1].Priority != "high" {
		t.Errorf("ProductDraft.Priority was ignored: %q", ms[len(ms)-1].Priority)
	}
	if _, err := s.AddStory(ctx, id, Story{Pri: "must", Role: "qa", Want: "w", So: "s", AC: []string{"one", "two"}}); err != nil {
		t.Fatal(err)
	}
	d := WorkflowDraft{ProductID: id, Goal: "plan it", Stories: []string{"US-01"}}
	plan := []PlanTask{
		{ID: "t-1", Title: "Design", Role: "architect", Kind: TaskKindDesign},
		{ID: "t-2", Title: "Review", Role: "qa", Kind: TaskKindReview, Deps: []string{"t-1"}},
		{ID: "t-3", Title: "Build", Role: "qa", Story: "US-01", Kind: TaskKindImplement, Deps: []string{"t-2"}},
	}
	if _, err := s.CreateWorkflow(ctx, d, plan); err != nil {
		t.Fatalf("a product made in the back office must be plannable (D-076), got %v", err)
	}
	after, _ := s.Snapshot(ctx)
	if len(after.Desks) != len(before.Desks)+1 {
		t.Errorf("one desk must be seated for the new product: %d -> %d", len(before.Desks), len(after.Desks))
	}
	got := map[string]string{}
	for _, tk := range s.tasksSnapshot(id) {
		got[tk.Title] = tk.Status
	}
	if got["Design"] != StatusPending || got["Review"] != StatusPending || got["Build"] != StatusBlocked {
		t.Errorf("a runtime product's tasks must start exactly as a seeded product's (PENDING, PENDING, gate-BLOCKED), got %v", got)
	}
	// A parked floor with a runtime desk present still answers.
	if _, err := s.Replay(ctx, 1); err != nil {
		t.Fatalf("Replay with a runtime desk present: %v", err)
	}
	if _, err := s.Snapshot(ctx); err != nil {
		t.Errorf("Snapshot while parked: %v", err)
	}
	_, _ = s.Replay(ctx, -1)
	// An unknown product is still not found.
	if _, err := s.CreateWorkflow(ctx, WorkflowDraft{ProductID: "p-999", Goal: "x"}, plan); err == nil {
		t.Error("CreateWorkflow for an unknown product must fail")
	}
	checkCookielessRoutesAreGated(t)
	checkDeskOrderSortsByIDLengthThenID(t)
}

func checkCookielessRoutesAreGated(t *testing.T) {
	t.Helper()
	srv, _ := newTestServer(t)
	for _, c := range []struct{ method, path string }{
		{"POST", "/workflows"},
		{"POST", "/tasks/0/dispatch"},
		{"POST", "/tasks/0/approve"},
		{"POST", "/tasks/0/retry"},
		{"POST", "/tasks/0/cancel"},
	} {
		rec := do(t, srv, c.method, c.path, url.Values{"product": {"p-1"}, "goal": {"x"}})
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without the back-office cookie answered %d, want 401", c.method, c.path, rec.Code)
		}
	}
	rec := do(t, srv, "POST", "/tasks/0/approve", nil, &http.Cookie{Name: boCookie, Value: "1"})
	if rec.Code == http.StatusUnauthorized {
		t.Errorf("a signed-in caller must pass the gate, got %d", rec.Code)
	}
}

// desk order after a restart: p-10 sorts after p-9, not between p-1 and p-2.
func checkDeskOrderSortsByIDLengthThenID(t *testing.T) {
	t.Helper()
	ids := []string{"p-10", "p-2", "p-1", "p-9", "p-11"}
	sortDeskIDs(ids)
	if fmt.Sprint(ids) != "[p-1 p-2 p-9 p-10 p-11]" {
		t.Errorf("desk order = %v, want [p-1 p-2 p-9 p-10 p-11]", ids)
	}
}
