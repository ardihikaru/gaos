package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// The pull-request poller and the excerpt retention (D-091, design §9.1, §5.1).
// The repository, product and defect ids are invented.

type fakeGHRead struct {
	mu    sync.Mutex
	state map[int]string // number -> "draft" | "ready" | "merged" | "closed"
	limit bool           // answer 403 with the rate-limit header
	hits  int
}

func (f *fakeGHRead) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hits++
	if f.limit {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(403)
		return
	}
	var n int
	if _, err := fmt.Sscanf(r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:], "%d", &n); err != nil {
		w.WriteHeader(404)
		return
	}
	st := f.state[n]
	body := `{"number":%d,"state":"%s","draft":%t,"merged":%t}`
	switch st {
	case "draft":
		fmt.Fprintf(w, body, n, "open", true, false)
	case "ready":
		fmt.Fprintf(w, body, n, "open", false, false)
	case "merged":
		fmt.Fprintf(w, body, n, "closed", false, true)
	case "closed":
		fmt.Fprintf(w, body, n, "closed", false, false)
	default:
		w.WriteHeader(404)
	}
}

func (f *fakeGHRead) set(n int, st string) { f.mu.Lock(); f.state[n] = st; f.mu.Unlock() }
func (f *fakeGHRead) count() int           { f.mu.Lock(); defer f.mu.Unlock(); return f.hits }

type pollEnv struct {
	s   *MemStore
	pid string
	key string
	gh  *fakeGHRead
	p   *fixPRPoller
	t0  time.Time
	now time.Time
}

func newPollEnv(t *testing.T) *pollEnv {
	t.Helper()
	e := &pollEnv{gh: &fakeGHRead{state: map[int]string{7: "draft"}}, t0: time.Unix(1_800_000_000, 0)}
	e.now = e.t0
	s, pid, key, triage := easyFixture(t, []string{"pkg/**"}, map[string]string{fxFile: fxText})
	ctx := context.Background()
	if _, err := s.SetAutoFix(ctx, pid, fixAF(), "ops", tokenKindFine); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ResolveTriage(ctx, pid, triage.ID, TriageVerdict{Difficulty: "easy", Class: "min-value",
		Repo: defRepo, Files: []string{fxFile}, Reason: "negative quantity"}); err != nil {
		t.Fatal(err)
	}
	fix := cardByKey(s, defectCardKey(key, 1, "fix"))
	d, _ := s.DefectByKey(ctx, pid, key)
	rec := FixPR{ID: fixPRID(d), DefectKey: key, Repo: defRepo, Number: 7, URL: "https://example.test/pull/7",
		CreatedAt: e.t0.Unix(), Branch: fxBranch}
	if _, err := s.OpenFixPR(ctx, pid, fix.ID, rec); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(e.gh)
	t.Cleanup(srv.Close)
	e.s, e.pid, e.key = s, pid, key
	e.p = newFixPRPoller(s, &ghClient{token: "ghp_" + strings.Repeat("a", 30), base: srv.URL, hc: srv.Client(),
		logf: func(string, ...any) {}})
	e.p.logf = func(string, ...any) {}
	e.p.now = func() time.Time { return e.now }
	return e
}

func (e *pollEnv) rec() FixPR {
	rs, _ := e.s.FixPRs(context.Background(), e.pid)
	return rs[0]
}

func (e *pollEnv) defect() Defect {
	d, _ := e.s.DefectByKey(context.Background(), e.pid, e.key)
	return d
}

func (e *pollEnv) pass(advance time.Duration) {
	e.now = e.now.Add(advance)
	e.p.Pass(context.Background())
}

func TestFixPRPollerStates(t *testing.T) {
	e := newPollEnv(t)
	if d := e.defect(); d.State != DefectStateFixing {
		t.Fatalf("the defect is %s, want fixing", d.State)
	}
	e.pass(time.Minute)
	if e.rec().State != FixPRDraft || len(streamOf(e.s, FixPRStateChanged)) != 0 {
		t.Fatal("an unchanged pull request must change nothing")
	}
	e.gh.set(7, "ready")
	e.pass(10 * time.Minute)
	if e.rec().State != FixPRReady || len(streamOf(e.s, FixPRStateChanged)) != 1 || e.defect().State != DefectStateFixing {
		t.Fatalf("ready: %s, %d events, defect %s", e.rec().State, len(streamOf(e.s, FixPRStateChanged)), e.defect().State)
	}
	e.gh.set(7, "draft")
	e.pass(10 * time.Minute)
	if e.rec().State != FixPRDraft || len(streamOf(e.s, FixPRStateChanged)) != 2 {
		t.Fatalf("a toggle back to draft: %s", e.rec().State)
	}
	e.gh.set(7, "merged")
	e.pass(10 * time.Minute)
	if e.rec().State != FixPRMerged || e.defect().State != DefectStateFixed {
		t.Fatalf("merged: %s, defect %s", e.rec().State, e.defect().State)
	}
	evs := streamOf(e.s, FixPRStateChanged)
	last := evs[len(evs)-1]
	if len(evs) != 3 || !strings.Contains(last.Text, "merged") || strings.Contains(last.Text, "negative") {
		t.Errorf("events: %d, last %q", len(evs), last.Text)
	}
	hits := e.gh.count()
	e.pass(time.Hour)
	e.pass(time.Hour)
	if e.gh.count() != hits {
		t.Error("a merged pull request was polled again")
	}
	if st := e.p.Status(); st.LastPoll.IsZero() || st.LastError != "" {
		t.Errorf("status %+v", st)
	}

	// closed unmerged: a person-only card, born blocked, gen + 1.
	c := newPollEnv(t)
	c.gh.set(7, "closed")
	c.pass(10 * time.Minute)
	d := c.defect()
	if c.rec().State != FixPRClosed || d.State != DefectStateTriaged || d.Gen != 2 {
		t.Fatalf("closed: %s, defect %s gen %d", c.rec().State, d.State, d.Gen)
	}
	card := cardByKey(c.s, defectCardKey(c.key, 2, "fix"))
	if card == nil || card.Status != StatusBlocked || card.BlockReason != BlockPRClosed || d.FixTask != card.ID ||
		hasLabel(card.Labels, LabelDefectEasy) {
		t.Fatalf("the re-planned card: %+v", card)
	}
	n := len(c.s.rawEvents())
	c.pass(time.Hour)
	if len(c.s.rawEvents()) != n || len(defectCards(c.s, c.pid)) != 3 {
		t.Error("a closed record is final: nothing is planned twice")
	}
}

func TestFixPRPollerBackoff(t *testing.T) {
	e := newPollEnv(t)
	e.gh.mu.Lock()
	e.gh.limit = true
	e.gh.mu.Unlock()
	e.pass(time.Minute)
	first := e.gh.count()
	if first == 0 || e.p.Status().LastError != "token" {
		t.Fatalf("hits %d status %+v", first, e.p.Status())
	}
	// Not due again: ticks within the back-off read nothing.
	for i := 0; i < 5; i++ {
		e.pass(time.Minute)
	}
	if e.gh.count() != first {
		t.Fatalf("a hot loop: %d reads, want %d", e.gh.count(), first)
	}
	// The wait doubles and is capped at an hour.
	e.pass(10 * time.Minute)
	second := e.gh.count()
	if second == first {
		t.Fatal("the record was never retried")
	}
	for i := 0; i < 10; i++ {
		e.pass(fixPRBackoffCap)
	}
	if e.p.due[e.rec().ID].Sub(e.now) > fixPRBackoffCap {
		t.Error("the back-off is not bounded")
	}
	// It recovers.
	e.gh.mu.Lock()
	e.gh.limit = false
	e.gh.mu.Unlock()
	e.gh.set(7, "ready")
	e.pass(fixPRBackoffCap + time.Minute)
	if e.rec().State != FixPRReady || e.p.Status().LastError != "" {
		t.Fatalf("no recovery: %s %+v", e.rec().State, e.p.Status())
	}

	// The interval rules.
	for _, c := range []struct {
		age  time.Duration
		want time.Duration
		stop bool
	}{
		{0, 5 * time.Minute, false}, {6 * 24 * time.Hour, 5 * time.Minute, false},
		{7 * 24 * time.Hour, time.Hour, false}, {29 * 24 * time.Hour, time.Hour, false}, {30 * 24 * time.Hour, 0, true},
	} {
		if got, stop := fixPRInterval(c.age); got != c.want || stop != c.stop {
			t.Errorf("age %v: %v %v, want %v %v", c.age, got, stop, c.want, c.stop)
		}
	}
	// Five minutes between reads in the first week, never before.
	f := newPollEnv(t)
	f.pass(0)
	n := f.gh.count()
	f.pass(4 * time.Minute)
	if f.gh.count() != n {
		t.Error("read before 5 minutes")
	}
	f.pass(2 * time.Minute)
	if f.gh.count() != n+1 {
		t.Error("no read after 5 minutes")
	}
	f.pass(8 * 24 * time.Hour)
	n = f.gh.count()
	f.pass(30 * time.Minute)
	if f.gh.count() != n {
		t.Error("after a week the poll is hourly")
	}
	f.pass(31 * time.Minute)
	if f.gh.count() != n+1 {
		t.Error("no hourly read")
	}
	f.pass(31 * 24 * time.Hour)
	n = f.gh.count()
	f.pass(48 * time.Hour)
	if f.gh.count() != n {
		t.Error("polled after 30 days")
	}
}

func TestFixPRPollerExcerptRetention(t *testing.T) {
	s, pid := defectFixture(t)
	report(t, s, pid, defItem(DefectKindXfail, "DEF-101", defCase1), defItem(DefectKindXfail, "DEF-102", defCase2))
	now := time.Unix(1_800_000_000, 0)
	s.mu.Lock()
	s.defects[pid+"/DEF-101"].LastSeen = now.Add(-31 * 24 * time.Hour).Unix()
	s.defects[pid+"/DEF-102"].LastSeen = now.Add(-29 * 24 * time.Hour).Unix()
	s.mu.Unlock()
	events := len(s.rawEvents())
	p := newFixPRPoller(s, nil) // no read client: the retention still runs
	p.logf = func(string, ...any) {}
	p.now = func() time.Time { return now }
	p.Pass(context.Background())
	old, _ := s.DefectByKey(context.Background(), pid, pid+"/DEF-101")
	kept, _ := s.DefectByKey(context.Background(), pid, pid+"/DEF-102")
	if old.Excerpt != "" || kept.Excerpt == "" || old.Title == "" {
		t.Fatalf("excerpts: 31 days %q, 29 days %q", old.Excerpt, kept.Excerpt)
	}
	if len(s.rawEvents()) != events {
		t.Error("the retention is silent: no event")
	}
	// Once per UTC day: the 29-day record ages past 30 days the next day, and
	// is untouched until then.
	s.mu.Lock()
	s.defects[pid+"/DEF-102"].LastSeen = now.Add(-31 * 24 * time.Hour).Unix()
	s.mu.Unlock()
	now = now.Add(time.Hour)
	if now.Unix()/86400 == p.lastDay {
		p.Pass(context.Background())
		if k, _ := s.DefectByKey(context.Background(), pid, pid+"/DEF-102"); k.Excerpt == "" {
			t.Error("the retention ran twice in one UTC day")
		}
	}
	now = time.Unix((p.lastDay+1)*86400+60, 0)
	p.Pass(context.Background())
	if k, _ := s.DefectByKey(context.Background(), pid, pid+"/DEF-102"); k.Excerpt != "" {
		t.Error("the next day's retention did not run")
	}
}

// Both stores take the same writes; the graph gets the records in one transaction.
func TestFixPRPollerWritesThroughArcade(t *testing.T) {
	g := &fakeGraph{}
	s, ms, _ := arcadeOver(t, g.handler)
	ctx := context.Background()
	ms.mu.Lock()
	ms.masterPtr("p-1").Repos = append(ms.masterPtr("p-1").Repos, defRepo)
	ms.mu.Unlock()
	importCases(t, ms, "p-1", false, qaCase(defCase1))
	if _, _, err := s.ReportDefects(ctx, "p-1", DefectReportIn{Source: "push", Items: []DefectItem{defItem(DefectKindXfail, "DEF-101", defCase1)}}); err != nil {
		t.Fatal(err)
	}
	key := "p-1/DEF-101"
	ms.mu.Lock()
	ms.defects[key].State = DefectStateFixing
	ms.mu.Unlock()
	rec := FixPR{ID: "fx-1", ProductID: "p-1", DefectKey: key, Repo: defRepo, Number: 3, State: FixPRDraft, CreatedAt: 1}
	if _, err := s.UpsertFixPR(ctx, rec); err != nil {
		t.Fatal(err)
	}
	g.take()
	rec.State, rec.UpdatedAt = FixPRClosed, 2
	if _, err := s.AdvanceFixPR(ctx, "p-1", rec); err != nil {
		t.Fatal(err)
	}
	stmts := g.take()
	if countPrefix(stmts, "UPDATE FixPR ") != 1 || countPrefix(stmts, "UPDATE Defect ") != 1 || countPrefix(stmts, "UPDATE Task ") == 0 {
		t.Errorf("the change was not written together: %d statements", len(stmts))
	}
	ms.mu.Lock()
	ms.defects[key].LastSeen = 1
	ms.mu.Unlock()
	if _, err := s.ExpireExcerpts(ctx, 1+31*86400); err != nil {
		t.Fatal(err)
	}
	if countPrefix(g.take(), "UPDATE Defect ") != 1 {
		t.Error("the cleared excerpt was not persisted")
	}
}
