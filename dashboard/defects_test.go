package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Defect intake (D-091): the route, the keys, the states and the cards. The
// product, repository and defect ids below are invented, as D-085 requires.

const (
	defRepo   = "acme-org/sample-service"
	defCase1  = "TC-SAMPLE-008"
	defCase2  = "TC-SAMPLE-009"
	defSpec   = "tests/api/graphql/sample-service/records/edit.spec.ts"
	defMarker = "ZZEXCERPTMARKERZZ"
)

// defectFixture is a product that owns acme-org/sample-service and holds two cases.
func defectFixture(t *testing.T) (*MemStore, string) {
	t.Helper()
	s, pid := qaFixtureRepo(t)
	importCases(t, s, pid, false, qaCase(defCase1), qaCase(defCase2))
	return s, pid
}

func qaFixtureRepo(t *testing.T) (*MemStore, string) {
	t.Helper()
	// A seeded product has a desk, which the planner needs; it also owns the
	// invented repository from here on.
	s := NewMemStore(1)
	s.mu.Lock()
	m := s.masterPtr("p-1")
	m.Repos = append(m.Repos, defRepo)
	s.mu.Unlock()
	return s, "p-1"
}

func defItem(kind, defectID, caseID string) DefectItem {
	return DefectItem{CaseID: caseID, DefectID: defectID, Kind: kind, Level: "GQL", AutomationType: "be",
		Backend: "graphql", TargetRepos: []string{defRepo}, Operations: []string{"editSampleRecord"},
		Spec: defSpec, Title: "a negative quantity is accepted", Excerpt: "expected 422, got 200", RunRef: "run-1",
		RunAt: time.Now().Unix()}
}

func report(t *testing.T, s *MemStore, pid string, items ...DefectItem) DefectReportOut {
	t.Helper()
	out, _, err := s.ReportDefects(context.Background(), pid, DefectReportIn{Source: "push", Items: items})
	if err != nil {
		t.Fatalf("ReportDefects: %v", err)
	}
	return out
}

func defectCards(s *MemStore, pid string) []TaskRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []TaskRecord
	for _, t := range s.tasks {
		if t.DeskID == pid && strings.HasPrefix(t.DedupKey, "defect:") {
			out = append(out, t.Clone())
		}
	}
	return out
}

func cardByKey(s *MemStore, key string) *TaskRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if t := s.taskByDedupKeyLocked(key); t != nil {
		c := t.Clone()
		return &c
	}
	return nil
}

func streamOf(s *MemStore, typ EventType) []Event {
	var out []Event
	for _, e := range s.rawEvents() {
		if e.Type == typ {
			out = append(out, e)
		}
	}
	return out
}

func postDefects(t *testing.T, srv *server, pid, body string) (int, string) {
	t.Helper()
	rec := call(t, srv, "POST", "/v1/products/"+pid+"/qa/defects", tokQA, body)
	return rec.Code, rec.Body.String()
}

func defectServer(t *testing.T) (*server, *MemStore, string) {
	t.Helper()
	srv, _ := newTestServer(t)
	s, pid := defectFixture(t)
	srv.store = s
	srv.api = newAPIConfig(tokAdmin, tokQA, tokOut, pid)
	return srv, s, pid
}

func jsonItems(t *testing.T, items ...DefectItem) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{"source": "push", "items": items})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestDefectRouteRejectsABadItem(t *testing.T) {
	srv, s, pid := defectServer(t)
	bad := func(f func(*DefectItem)) DefectItem {
		it := defItem(DefectKindXfail, "DEF-101", defCase1)
		f(&it)
		return it
	}
	items := []DefectItem{
		bad(func(i *DefectItem) { i.Kind = "maybe" }),
		bad(func(i *DefectItem) { i.CaseID = "TC-SAMPLE-777" }),
		bad(func(i *DefectItem) { i.DefectID = "DEF-1x" }),
		bad(func(i *DefectItem) { i.Spec = "../etc/passwd" }),
		bad(func(i *DefectItem) { i.Operations = []string{"a b"} }),
		defItem(DefectKindXfail, "DEF-102", defCase2),
	}
	code, body := postDefects(t, srv, pid, jsonItems(t, items...))
	if code != 200 {
		t.Fatalf("answered %d (%s), want 200: a bad item is rejected, the rest apply", code, body)
	}
	var out DefectReportOut
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, r := range out.Rejected {
		got[r.Reason] = true
	}
	for _, want := range []string{"bad-kind", "unknown-case", "bad-defect-id", "bad-spec", "bad-operation"} {
		if !got[want] {
			t.Errorf("no item was rejected %q; rejected = %+v", want, out.Rejected)
		}
	}
	if out.Applied != 1 {
		t.Errorf("applied = %d, want 1 (the good item)", out.Applied)
	}
	if ds, _ := s.Defects(context.Background(), pid); len(ds) != 1 {
		t.Errorf("%d records stored, want 1", len(ds))
	}
	// Strict decoding: an unknown field is a 400, and so is any source but push.
	if code, body := postDefects(t, srv, pid, `{"source":"push","items":[],"extra":1}`); code != 400 || !strings.Contains(body, "bad-json") {
		t.Errorf("an unknown field answered %d %s, want 400 bad-json", code, body)
	}
	if code, body := postDefects(t, srv, pid, `{"source":"pull","items":[]}`); code != 400 || !strings.Contains(body, "bad-source") {
		t.Errorf("source pull answered %d %s, want 400 bad-source", code, body)
	}
	// The product scope of the QA token holds.
	if rec := call(t, srv, "POST", "/v1/products/p-2/qa/defects", tokQA, jsonItems(t)); rec.Code != 404 {
		t.Errorf("another product answered %d, want 404", rec.Code)
	}
}

func TestDefectLimit413(t *testing.T) {
	srv, _, pid := defectServer(t)
	huge := defItem(DefectKindXfail, "DEF-101", defCase1)
	huge.Excerpt = strings.Repeat("x", 300<<10)
	code, body := postDefects(t, srv, pid, jsonItems(t, huge))
	if code != 413 || !strings.Contains(body, "too-large") || !strings.Contains(body, "256 KiB") {
		t.Errorf("a 300 KiB body answered %d %s, want 413 too-large naming 256 KiB", code, body)
	}
	var many []DefectItem
	for i := 0; i < defectMaxItems+1; i++ {
		many = append(many, defItem(DefectKindXfail, "DEF-"+strconv.Itoa(100+i), defCase1))
	}
	code, body = postDefects(t, srv, pid, jsonItems(t, many...))
	if code != 413 || !strings.Contains(body, "50 items") {
		t.Errorf("51 items answered %d %s, want 413 naming 50 items", code, body)
	}
}

func TestDefectRepeatPlansNoCard(t *testing.T) {
	srv, s, pid := defectServer(t)
	code, _ := postDefects(t, srv, pid, jsonItems(t, defItem(DefectKindXfail, "DEF-101", defCase1)))
	if code != 200 || len(defectCards(s, pid)) != 1 {
		t.Fatalf("the first report answered %d and planned %d cards, want 200 and 1", code, len(defectCards(s, pid)))
	}
	code, body := postDefects(t, srv, pid, jsonItems(t, defItem(DefectKindXfail, "DEF-101", defCase1)))
	var out DefectReportOut
	_ = json.Unmarshal([]byte(body), &out)
	if code != 200 || out.Unchanged != 1 || out.Applied != 0 {
		t.Errorf("a repeat answered %d %s, want 200 with unchanged 1", code, body)
	}
	if n := len(defectCards(s, pid)); n != 1 {
		t.Errorf("a repeat left %d cards, want 1", n)
	}
	if n := len(streamOf(s, DefectReported)); n != 1 {
		t.Errorf("%d DefectReported events, want 1", n)
	}
	// A repeat moves lastSeen and runRef and nothing else.
	later := defItem(DefectKindXfail, "DEF-101", defCase1)
	later.RunRef = "run-2"
	if _, _, err := s.ReportDefects(context.Background(), pid, DefectReportIn{Source: "push", Items: []DefectItem{later}, now: time.Now().Unix() + 100}); err != nil {
		t.Fatal(err)
	}
	d, _ := s.DefectByKey(context.Background(), pid, pid+"/DEF-101")
	if d.RunRef != "run-2" || d.LastSeen <= d.FirstSeen || d.Gen != 1 {
		t.Errorf("after a repeat: runRef %q lastSeen %d firstSeen %d gen %d", d.RunRef, d.LastSeen, d.FirstSeen, d.Gen)
	}
}

func TestDefectNewKeyLimit(t *testing.T) {
	s, pid := defectFixture(t)
	now := time.Now().Unix()
	var batch []DefectItem
	for i := 0; i < defectMaxNewKeys; i++ {
		batch = append(batch, defItem(DefectKindXfail, "DEF-"+strconv.Itoa(100+i), defCase1))
	}
	if out := report(t, s, pid, batch...); out.Applied != defectMaxNewKeys {
		t.Fatalf("applied %d of %d new keys", out.Applied, defectMaxNewKeys)
	}
	out := report(t, s, pid, defItem(DefectKindXfail, "DEF-900", defCase1))
	if len(out.Rejected) != 1 || out.Rejected[0].Reason != "defect-limit" {
		t.Errorf("the 51st key of the day: %+v, want rejected defect-limit", out)
	}
	// A repeat of a known key is not a new key.
	if out := report(t, s, pid, defItem(DefectKindXfail, "DEF-100", defCase1)); out.Unchanged != 1 {
		t.Errorf("a repeat past the limit: %+v, want unchanged 1", out)
	}
	// Another UTC day counts from zero again.
	next, _, err := s.ReportDefects(context.Background(), pid, DefectReportIn{Source: "push",
		Items: []DefectItem{defItem(DefectKindXfail, "DEF-900", defCase1)}, now: now + 86400})
	if err != nil || next.Applied != 1 {
		t.Errorf("the next day: %+v %v, want applied 1", next, err)
	}
}

func TestDefectXpassCancelsCards(t *testing.T) {
	s, pid := defectFixture(t)
	report(t, s, pid, defItem(DefectKindXfail, "DEF-101", defCase1))
	key := pid + "/DEF-101"
	card := cardByKey(s, defectCardKey(key, 1, "triage"))
	if card == nil {
		t.Fatal("no triage card was planned")
	}
	before := len(defectCards(s, pid))
	out := report(t, s, pid, defItem(DefectKindXpass, "DEF-101", defCase1))
	if out.Applied != 1 {
		t.Errorf("xpass: %+v, want applied 1", out)
	}
	d, _ := s.DefectByKey(context.Background(), pid, key)
	if d.State != DefectStateFixed {
		t.Errorf("state %q, want fixed", d.State)
	}
	if c := cardByKey(s, defectCardKey(key, 1, "triage")); c.Status != StatusCanceled {
		t.Errorf("the triage card is %s, want CANCELED", c.Status)
	}
	if len(defectCards(s, pid)) != before {
		t.Error("an xpass planned a card")
	}
	var sawRule bool
	for _, e := range streamOf(s, TaskMoved) {
		sawRule = sawRule || e.Payload.Rule == RuleDefectFixed
	}
	if !sawRule {
		t.Errorf("no TaskMoved named the rule %s", RuleDefectFixed)
	}
	if n := len(streamOf(s, DefectStateChanged)); n != 1 {
		t.Errorf("%d DefectStateChanged events, want 1", n)
	}
	// Again: nothing moves.
	if out := report(t, s, pid, defItem(DefectKindXpass, "DEF-101", defCase1)); out.Unchanged != 1 {
		t.Errorf("a second xpass: %+v, want unchanged", out)
	}
}

func TestDefectRegressionLooksUpByCase(t *testing.T) {
	s, pid := defectFixture(t)
	report(t, s, pid, defItem(DefectKindXfail, "DEF-101", defCase1))
	report(t, s, pid, defItem(DefectKindXpass, "DEF-101", defCase1))
	keys := func() int { ds, _ := s.Defects(context.Background(), pid); return len(ds) }
	if keys() != 1 {
		t.Fatalf("%d records before the regression", keys())
	}
	out := report(t, s, pid, defItem(DefectKindFail, "", defCase1))
	if out.Applied != 1 || keys() != 1 {
		t.Fatalf("regression: %+v with %d records, want applied 1 and still 1 record (found by case id)", out, keys())
	}
	key := pid + "/DEF-101"
	d, _ := s.DefectByKey(context.Background(), pid, key)
	if d.State != DefectStateOpen || d.Gen != 2 || !d.Regression {
		t.Errorf("record: state %q gen %d regression %v, want open 2 true", d.State, d.Gen, d.Regression)
	}
	tri := cardByKey(s, defectCardKey(key, 2, "triage"))
	if tri == nil {
		t.Fatal("no generation-2 triage card")
	}
	// The verdict of a regression never makes a card an automatic one.
	if _, err := s.ResolveTriage(context.Background(), pid, tri.ID, TriageVerdict{Difficulty: "easy", Class: "min-value", Repo: defRepo, Files: []string{"a.go"}}); err != nil {
		t.Fatalf("ResolveTriage: %v", err)
	}
	fix := cardByKey(s, defectCardKey(key, 2, "fix"))
	if fix == nil || fix.Status != StatusBlocked || fix.BlockReason != BlockNotEasy {
		t.Errorf("the regression's fix card is %+v, want BLOCKED %s", fix, BlockNotEasy)
	}
	var changed int
	for _, e := range streamOf(s, DefectStateChanged) {
		if e.Payload.Rule == RuleDefectRegression {
			changed++
		}
	}
	if changed != 1 {
		t.Errorf("%d regression state events, want 1", changed)
	}
	// It does not spend a new-key slot: the day's count is the one record.
	s.mu.RLock()
	n := s.newKeysTodayLocked(pid, time.Now().Unix())
	s.mu.RUnlock()
	if n != 1 {
		t.Errorf("the day's new keys = %d, want 1", n)
	}
}

func TestDefectFailSupersededByXfail(t *testing.T) {
	s, pid := defectFixture(t)
	report(t, s, pid, defItem(DefectKindFail, "", defCase2))
	failKey := pid + "/" + defCase2
	if cardByKey(s, defectCardKey(failKey, 1, "triage")) == nil {
		t.Fatal("the fail record has no triage card")
	}
	report(t, s, pid, defItem(DefectKindXfail, "DEF-102", defCase2))
	f, _ := s.DefectByKey(context.Background(), pid, failKey)
	if f.SupersededBy != pid+"/DEF-102" || f.State != DefectStateClosed {
		t.Errorf("the fail record: supersededBy %q state %q, want the xfail key and closed", f.SupersededBy, f.State)
	}
	if c := cardByKey(s, defectCardKey(failKey, 1, "triage")); c.Status != StatusCanceled {
		t.Errorf("the fail record's card is %s, want CANCELED", c.Status)
	}
	var rule bool
	for _, e := range streamOf(s, TaskMoved) {
		rule = rule || e.Payload.Rule == RuleDefectSuperseded
	}
	if !rule {
		t.Errorf("no move named %s", RuleDefectSuperseded)
	}
	if x, err := s.DefectByKey(context.Background(), pid, pid+"/DEF-102"); err != nil || x.State != DefectStateOpen {
		t.Errorf("the xfail record: %+v %v, want open", x, err)
	}
	// A later fail report is a repeat of the live xfail record, not a regression.
	if out := report(t, s, pid, defItem(DefectKindFail, "", defCase2)); out.Unchanged != 1 {
		t.Errorf("a fail after the takeover: %+v, want unchanged", out)
	}
}

func TestDefectExcerptNeverInEventOrTitle(t *testing.T) {
	s, pid := defectFixture(t)
	it := defItem(DefectKindXfail, "DEF-101", defCase1)
	it.Excerpt = "stack " + defMarker + " end"
	it.Title = "TITLEMARKER failure"
	report(t, s, pid, it)
	report(t, s, pid, defItem(DefectKindXpass, "DEF-101", defCase1))
	d, _ := s.DefectByKey(context.Background(), pid, pid+"/DEF-101")
	if !strings.Contains(d.Excerpt, defMarker) {
		t.Fatalf("the record dropped the excerpt: %q", d.Excerpt)
	}
	for _, e := range s.rawEvents() {
		dump := fmt.Sprintf("%s %+v", e.Text, e.Payload)
		if strings.Contains(dump, defMarker) {
			t.Errorf("event %d (%s) carries the excerpt", e.Seq, e.Type)
		}
		// The card title rides TaskPlanned by design; no other event names the title.
		if e.Type != TaskPlanned && strings.Contains(dump, "TITLEMARKER") {
			t.Errorf("event %d (%s) carries the defect title", e.Seq, e.Type)
		}
	}
	for _, c := range defectCards(s, pid) {
		if strings.Contains(c.Title, defMarker) {
			t.Errorf("card %s carries the excerpt in its title", c.ID)
		}
	}
}

func TestDefectBR53Stripping(t *testing.T) {
	s, pid := defectFixture(t)
	it := defItem(DefectKindXfail, "DEF-101", defCase1)
	it.Title = "a `quoted` <b>title</b>\nsecond line"
	it.Excerpt = "line one\r\nline `two` <three>"
	report(t, s, pid, it)
	d, _ := s.DefectByKey(context.Background(), pid, pid+"/DEF-101")
	for _, f := range []string{d.Title, d.Excerpt} {
		if strings.ContainsAny(f, "`<>\r\n") {
			t.Errorf("an untrusted field kept a stripped character: %q", f)
		}
	}
	if d.Title == "" || d.Excerpt == "" {
		t.Errorf("stripping must keep the words: title %q excerpt %q", d.Title, d.Excerpt)
	}
	// A secret in the excerpt drops the text, keeps the item and says so.
	sec := defItem(DefectKindXfail, "DEF-102", defCase2)
	sec.Excerpt = "the log said API_TOKEN=abcdef0123456789abcdef"
	out := report(t, s, pid, sec)
	if out.Applied != 1 {
		t.Fatalf("an item with a secret in its excerpt: %+v, want it kept", out)
	}
	d2, _ := s.DefectByKey(context.Background(), pid, pid+"/DEF-102")
	if d2.Excerpt != "" || !d2.ExcerptWithheld {
		t.Errorf("excerpt %q withheld %v, want empty and withheld", d2.Excerpt, d2.ExcerptWithheld)
	}
	// The packet says it is withheld and never prints the excerpt.
	if body := renderDefectPacket(d2); !strings.Contains(body, "withheld") || strings.Contains(body, "abcdef0123") {
		t.Errorf("the packet text for a withheld excerpt: %q", body)
	}
}

func TestDefectCardsExcludedFromPRCap(t *testing.T) {
	t.Setenv("GAOS_PR_MAX_OPEN", "2")
	s, pid := defectFixture(t)
	for i := 0; i < 5; i++ {
		report(t, s, pid, defItem(DefectKindXfail, "DEF-"+strconv.Itoa(100+i), defCase1))
	}
	if n := len(defectCards(s, pid)); n != 5 {
		t.Fatalf("%d defect cards, want 5", n)
	}
	ev := PREvent{Kind: "github", Type: prTypeOpened, EventID: "gh-1", Seq: 7, Org: "acme-org", Repo: "sample-service", PRNumber: 12, Title: "A change"}
	planned, _, _, err := s.PlanPullRequestCards(context.Background(), pid, ev)
	if err != nil || len(planned) != 2 {
		t.Errorf("pr.opened beside 5 open defect cards: planned %v, err %v; want 2 cards and no cap refusal", planned, err)
	}
	s.mu.RLock()
	open := s.openPRCardsLocked(pid)
	s.mu.RUnlock()
	if open != 2 {
		t.Errorf("the PR cap counts %d open cards, want 2 (defect cards are not counted)", open)
	}
}

func TestDefectCardsAreReadiedAndNeverScheduled(t *testing.T) {
	s, pid := defectFixture(t)
	report(t, s, pid, defItem(DefectKindXfail, "DEF-101", defCase1))
	s.RunOnce(context.Background(), nil)
	c := cardByKey(s, defectCardKey(pid+"/DEF-101", 1, "triage"))
	if c.Status != StatusReady {
		t.Errorf("the triage card is %s after a pass, want READY", c.Status)
	}
	if c.Role != "triager" || len(c.Labels) != 1 || c.Labels[0] != LabelDefectTriage {
		t.Errorf("the triage card: role %q labels %v", c.Role, c.Labels)
	}
	for _, r := range Schedule(s.schedulerInput(time.Now().Unix())).Ready {
		if r.TaskID == c.ID {
			t.Error("the scheduler offered a defect card for dispatch")
		}
	}
}

func TestDefectCloseRouteStoreMethod(t *testing.T) {
	s, pid := defectFixture(t)
	report(t, s, pid, defItem(DefectKindXfail, "DEF-101", defCase1))
	if _, err := s.CloseDefect(context.Background(), "p-2", "DEF-101", "x@example.test"); err == nil {
		t.Error("a record of another product must be not found")
	}
	res, err := s.CloseDefect(context.Background(), pid, "DEF-101", "Ops.Person@example.test")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range res.NewEvents {
		if strings.Contains(e.Text, "@") || strings.Contains(fmt.Sprint(e.Payload.Actor), "@") {
			t.Errorf("an address reached an event: %q", e.Text)
		}
	}
	d, _ := s.DefectByKey(context.Background(), pid, pid+"/DEF-101")
	if d.State != DefectStateClosed {
		t.Errorf("state %q, want closed", d.State)
	}
	// By case id too.
	report(t, s, pid, defItem(DefectKindFail, "", defCase2))
	if _, err := s.CloseDefect(context.Background(), pid, defCase2, "ops"); err != nil {
		t.Errorf("close by case id: %v", err)
	}
}

func TestDefectEventsAreInTheCatalogAndCarryNoText(t *testing.T) {
	s, pid := defectFixture(t)
	it := defItem(DefectKindXfail, "DEF-101", defCase1)
	report(t, s, pid, it)
	tri := cardByKey(s, defectCardKey(pid+"/DEF-101", 1, "triage"))
	if _, err := s.ResolveTriage(context.Background(), pid, tri.ID, TriageVerdict{Difficulty: "complex", Class: "auth", Reason: "touches sign in"}); err != nil {
		t.Fatal(err)
	}
	for _, typ := range []EventType{DefectReported, DefectTriaged, DefectStateChanged} {
		if len(streamOf(s, typ)) == 0 {
			t.Errorf("no %s event", typ)
		}
		if !catalog[typ] {
			t.Errorf("%s is not in the closed catalog", typ)
		}
	}
	if _, err := s.SetAutoFix(context.Background(), pid, AutoFix{Enabled: true, PRTemplate: "SECRET-TEMPLATE-TEXT"}, "Ops@example.test", "fine-grained"); err != nil {
		t.Fatal(err)
	}
	cfg := streamOf(s, AutoFixConfigured)
	if len(cfg) != 1 || strings.Contains(fmt.Sprintf("%+v", cfg[0]), "SECRET-TEMPLATE-TEXT") ||
		strings.Contains(cfg[0].Text, "@") || !strings.Contains(cfg[0].Payload.Reason, "token=fine-grained") {
		t.Errorf("AutoFixConfigured: %+v", cfg)
	}
	af, _ := s.AutoFixOf(context.Background(), pid)
	if !af.Enabled || af.FixCap != autoFixFixCapDefault || af.TriageCap != autoFixTriageCapDefault {
		t.Errorf("the stored configuration: %+v, want enabled with the default caps", af)
	}
}
