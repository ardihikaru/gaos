package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// The unit pass (D-105, docs/designs/release-b4-unit-tests.md §5) against a
// fake GitHub. The repositories, branches, tests and run ids are invented.

// ucNow is the clock of these tests: 2026-11-27 12:00:00 UTC.
var ucNow = time.Unix(1795780800, 0)

// unitGH is a fake GitHub that answers the unit pass's reads and logs them.
type unitGH struct {
	mu     sync.Mutex
	runs   map[string][]ghWorkflowRun     // by head_sha
	checks map[string][]map[string]any    // by sha
	anns   map[int64][]map[string]any     // by check run id
	dirs   map[string][]map[string]string // by directory
	files  map[string]string              // by path
	fail   map[string]int                 // a path substring -> the status every matching read answers
	log    []string                       // "<path>?<query>" of every request
}

func newUnitGH() *unitGH {
	return &unitGH{runs: map[string][]ghWorkflowRun{}, checks: map[string][]map[string]any{},
		anns: map[int64][]map[string]any{}, dirs: map[string][]map[string]string{}, files: map[string]string{}, fail: map[string]int{}}
}

func (f *unitGH) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := r.URL.Path
	f.log = append(f.log, p+"?"+r.URL.RawQuery)
	for frag, st := range f.fail {
		if strings.Contains(p, frag) {
			if st == http.StatusTooManyRequests {
				w.Header().Set("Retry-After", "0")
			}
			w.WriteHeader(st)
			return
		}
	}
	const pre = "/repos/acme-org/sample-service"
	switch {
	case p == pre+"/actions/runs":
		fakeJSON(w, 200, map[string]any{"workflow_runs": f.runs[r.URL.Query().Get("head_sha")]})
	case strings.HasPrefix(p, pre+"/commits/") && strings.HasSuffix(p, "/check-runs"):
		sha := strings.TrimSuffix(strings.TrimPrefix(p, pre+"/commits/"), "/check-runs")
		var out []map[string]any
		for _, c := range f.checks[sha] {
			if c["name"] == r.URL.Query().Get("check_name") {
				out = append(out, c)
			}
		}
		fakeJSON(w, 200, map[string]any{"check_runs": out})
	case strings.HasPrefix(p, pre+"/check-runs/") && strings.HasSuffix(p, "/annotations"):
		id, _ := strconv.ParseInt(strings.TrimSuffix(strings.TrimPrefix(p, pre+"/check-runs/"), "/annotations"), 10, 64)
		fakeJSON(w, 200, f.anns[id])
	case p == pre+"/contents" || strings.HasPrefix(p, pre+"/contents/"):
		rel := strings.TrimPrefix(strings.TrimPrefix(p, pre+"/contents"), "/")
		if d, ok := f.dirs[rel]; ok {
			fakeJSON(w, 200, d)
			return
		}
		if text, ok := f.files[rel]; ok {
			fakeJSON(w, 200, map[string]any{"type": "file", "encoding": "base64", "sha": "b-" + rel, "size": len(text),
				"content": base64.StdEncoding.EncodeToString([]byte(text))})
			return
		}
		w.WriteHeader(404)
	default:
		w.WriteHeader(404)
	}
}

func (f *unitGH) requests(frag string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, l := range f.log {
		if strings.Contains(l, frag) {
			n++
		}
	}
	return n
}

func (f *unitGH) set(fn func(*unitGH)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

// ucReported is the workflow's total notice: it says the tests ran.
var ucReported = ucAnn("notice", ".github", unitTotalTitle, "1")

// ucRun is the push run of the default workflow on sha at develop.
func ucRun(id int64, attempt int, suite int64, sha string) ghWorkflowRun {
	return ghWorkflowRun{ID: id, Path: unitWorkflowDefault, Event: "push", HeadBranch: "develop", HeadSHA: sha,
		RunAttempt: attempt, CheckSuiteID: suite, Status: "completed"}
}

func ucCheck(id int64, name, app string, suite int64, status, conclusion string) map[string]any {
	return map[string]any{"id": id, "name": name, "status": status, "conclusion": conclusion,
		"app": map[string]any{"slug": app}, "check_suite": map[string]any{"id": suite},
		"output": map[string]any{"title": "unit-tests", "summary": "go test ./..."}}
}

func ucAnn(level, path, title, msg string) map[string]any {
	return map[string]any{"annotation_level": level, "path": path, "title": title, "message": msg}
}

// unitPollEnv is a product with unit checks on, a fake GitHub and a poller at ucNow.
type unitPollEnv struct {
	s      *MemStore
	pid    string
	gh     *unitGH
	p      *fixPRPoller
	said   []string
	saidMu sync.Mutex
}

func newUnitPollEnv(t *testing.T, u UnitChecks) *unitPollEnv {
	t.Helper()
	old := autoFixNow
	autoFixNow = func() int64 { return ucNow.Unix() }
	t.Cleanup(func() { autoFixNow = old })
	e := &unitPollEnv{gh: newUnitGH()}
	e.s, e.pid = unitOnStore(t, u)
	srv := httptest.NewServer(e.gh)
	t.Cleanup(srv.Close)
	e.p = newFixPRPoller(e.s, newGHClient(ghConfig{token: "ghp_" + strings.Repeat("a", 30), base: srv.URL}))
	e.p.logf = func(string, ...any) {}
	e.p.now = func() time.Time { return ucNow }
	e.p.unitNotify = func(_ context.Context, productID string, pr int, text, cardID string) {
		e.saidMu.Lock()
		defer e.saidMu.Unlock()
		e.said = append(e.said, fmt.Sprintf("%s|%d|%s|%s", productID, pr, text, cardID))
	}
	return e
}

// watch plants a due pending watch of merge n, merged age ago.
func (e *unitPollEnv) watch(t *testing.T, n int, age time.Duration) string {
	t.Helper()
	w := UnitWatch{Repo: defRepo, PR: n, SHA: ucSHAn(n), Base: "develop", MergedAt: ucNow.Add(-age).Unix()}
	if _, err := e.s.PlantUnitWatch(context.Background(), e.pid, w); err != nil {
		t.Fatal(err)
	}
	return unitWatchKey(e.pid, defRepo, ucSHAn(n))
}

func (e *unitPollEnv) get(t *testing.T, key string) UnitWatch {
	t.Helper()
	e.s.mu.RLock()
	defer e.s.mu.RUnlock()
	return e.s.unitWatches[key]
}

func (e *unitPollEnv) notices() []string {
	e.saidMu.Lock()
	defer e.saidMu.Unlock()
	return append([]string(nil), e.said...)
}

// greenAt makes merge n's check pass (run 100+n, suite 200+n).
func (e *unitPollEnv) greenAt(n int, conclusion string) {
	sha := ucSHAn(n)
	e.gh.set(func(f *unitGH) {
		f.runs[sha] = []ghWorkflowRun{ucRun(int64(100+n), 1, int64(200+n), sha)}
		f.checks[sha] = []map[string]any{ucCheck(int64(300+n), "unit-tests", unitActionsSlug, int64(200+n), "completed", conclusion)}
	})
}

// TC-862
func TestOnlyTheWorkflowsCheckIsRead(t *testing.T) {
	ctx := context.Background()
	e := newUnitPollEnv(t, UnitChecks{})
	key := e.watch(t, 1, 10*time.Minute)
	sha := ucSHAn(1)
	other := ucRun(11, 1, 211, sha)
	other.Path = ".github/workflows/other.yml"
	pr := ucRun(12, 1, 212, sha)
	pr.Event = "pull_request"
	mainRun := ucRun(13, 1, 213, sha)
	mainRun.HeadBranch = "main"
	e.gh.set(func(f *unitGH) {
		// The run of the workflow is 10; its second attempt is suite 202.
		f.runs[sha] = []ghWorkflowRun{other, pr, mainRun, ucRun(10, 1, 201, sha), ucRun(10, 2, 202, sha)}
		f.checks[sha] = []map[string]any{
			ucCheck(1, "unit-tests", unitActionsSlug, 202, "completed", "success"),
			ucCheck(2, "unit-tests", unitActionsSlug, 201, "completed", "failure"), // the first attempt
			ucCheck(3, "unit-tests", unitActionsSlug, 999, "completed", "failure"), // another workflow's forgery
			ucCheck(4, "unit-tests", "someone-else", 202, "completed", "failure"),  // another app
			ucCheck(5, "unit-tests", unitActionsSlug, 211, "completed", "failure"), // the other workflow's run
		}
	})
	e.p.unitPass(ctx, ucNow)
	if w := e.get(t, key); w.State != UnitWatchDone || w.Outcome != UnitPassed {
		t.Errorf("the workflow's latest attempt passed; the watch is %+v", w)
	}
	if n := e.gh.requests("/annotations"); n != 0 {
		t.Errorf("a passing check read %d annotation pages", n)
	}
	want := "/repos/acme-org/sample-service/actions/runs?head_sha=" + sha + "&event=push&per_page=20"
	if n := e.gh.requests(want); n != 1 {
		t.Errorf("the run listing was read %d times as %s", n, want)
	}
	if n := e.gh.requests("/statuses") + e.gh.requests("/status"); n != 0 {
		t.Errorf("commit statuses were read %d times", n)
	}

	// Only a forged check of another suite: no check of the run yet, so the watch waits.
	e2 := newUnitPollEnv(t, UnitChecks{})
	k2 := e2.watch(t, 2, 10*time.Minute)
	sha2 := ucSHAn(2)
	e2.gh.set(func(f *unitGH) {
		f.runs[sha2] = []ghWorkflowRun{ucRun(20, 1, 220, sha2)}
		f.checks[sha2] = []map[string]any{ucCheck(6, "unit-tests", unitActionsSlug, 999, "completed", "success")}
	})
	e2.p.unitPass(ctx, ucNow)
	if w := e2.get(t, k2); w.State != UnitWatchPending || w.NextAt != ucNow.Add(unitWaitFast).Unix() || w.Reads != 1 {
		t.Errorf("a forged check of another suite moved the watch: %+v", w)
	}
	// Two distinct runs of the workflow on the commit: inconclusive.
	e2.gh.set(func(f *unitGH) {
		f.runs[sha2] = append(f.runs[sha2], ucRun(21, 1, 221, sha2))
	})
	e2.s.mu.Lock()
	w := e2.s.unitWatches[k2]
	w.NextAt = 0
	e2.s.unitWatches[k2] = w
	e2.s.mu.Unlock()
	e2.p.unitPass(ctx, ucNow)
	if w := e2.get(t, k2); w.Outcome != UnitInconclusive {
		t.Errorf("two runs of the workflow: the watch is %+v; want inconclusive", w)
	}

	// The configured workflow and check name: the name is escaped and matched exactly.
	e3 := newUnitPollEnv(t, UnitChecks{Workflow: ".github/workflows/go.yaml", CheckName: "go test"})
	k3 := e3.watch(t, 3, 10*time.Minute)
	sha3 := ucSHAn(3)
	run := ucRun(30, 1, 230, sha3)
	run.Path = ".github/workflows/go.yaml"
	e3.gh.set(func(f *unitGH) {
		f.runs[sha3] = []ghWorkflowRun{ucRun(31, 1, 231, sha3), run}
		f.checks[sha3] = []map[string]any{ucCheck(7, "go test", unitActionsSlug, 230, "completed", "success")}
	})
	e3.p.unitPass(ctx, ucNow)
	if w := e3.get(t, k3); w.Outcome != UnitPassed {
		t.Errorf("the configured workflow's check: the watch is %+v", w)
	}
	if n := e3.gh.requests("/commits/" + sha3 + "/check-runs?check_name=go+test&filter=latest&per_page=100&page=1"); n != 1 {
		t.Errorf("the check name was not escaped in the query (%d reads)", n)
	}
	if unitPickRunN(run, ".github/workflows/go.yaml") != 1 || unitPickRunN(run, ".github/workflows/go.yml") != 0 {
		t.Error("the workflow path is not matched exactly")
	}
}

func unitPickRunN(r ghWorkflowRun, workflow string) int {
	_, n := unitPickRun([]ghWorkflowRun{r}, workflow, r.HeadBranch, r.HeadSHA)
	return n
}

// TC-863
func TestUnitOutcomes(t *testing.T) {
	ctx := context.Background()
	// success, failure, build, all withheld, other conclusions: one watch each.
	type tc struct {
		conclusion string
		anns       []map[string]any
		want       string
	}
	for name, c := range map[string]tc{
		"success": {"success", nil, UnitPassed},
		"failure": {"failure", []map[string]any{ucAnn("failure", "pkg/records/total_test.go", "TestTotal", "failed"),
			ucAnn("notice", ".github", unitTotalTitle, "1")}, UnitFailed},
		"build":        {"failure", []map[string]any{ucAnn("warning", "go.mod", "x", "y"), ucReported}, UnitFailed},
		"all withheld": {"failure", []map[string]any{ucAnn("failure", "../secrets_test.go", "TestX", ""), ucReported}, "refused:withheld"},
		// A job that never started (found live: a spending limit) carries no total notice.
		"not started": {"failure", []map[string]any{ucAnn("failure", ".github", "",
			"The job was not started because recent account payments have failed or your spending limit needs to be increased.")}, UnitInconclusive},
		"no report":       {"failure", []map[string]any{ucAnn("failure", "pkg/records/total_test.go", "TestTotal", "failed")}, UnitInconclusive},
		"cancelled":       {"cancelled", nil, UnitInconclusive},
		"skipped":         {"skipped", nil, UnitInconclusive},
		"neutral":         {"neutral", nil, UnitInconclusive},
		"timed_out":       {"timed_out", nil, UnitInconclusive},
		"action_required": {"action_required", nil, UnitInconclusive},
		"stale":           {"stale", nil, UnitInconclusive},
		"something new":   {"exploded", nil, UnitInconclusive},
	} {
		e := newUnitPollEnv(t, UnitChecks{})
		key := e.watch(t, 1, 10*time.Minute)
		e.greenAt(1, c.conclusion)
		e.gh.set(func(f *unitGH) { f.anns[301] = c.anns })
		e.p.unitPass(ctx, ucNow)
		w := e.get(t, key)
		if w.State != UnitWatchDone || w.Outcome != c.want || w.DoneAt != ucNow.Unix() {
			t.Errorf("%s: the watch is %+v; want %s", name, w, c.want)
		}
		defs, _ := e.s.Defects(ctx, e.pid)
		filed := len(defs)
		if wantFiled := map[bool]int{true: 1, false: 0}[c.want == UnitFailed]; filed != wantFiled {
			t.Errorf("%s: %d defects filed, want %d", name, filed, wantFiled)
		}
		if c.want == UnitInconclusive && c.conclusion == "failure" && !strings.Contains(strings.Join(e.notices(), "\n"),
			"the unit-tests workflow did not report (job not started?)") {
			t.Errorf("%s: the daily notice was not sent: %q", name, e.notices())
		}
		if name == "build" && (filed != 1 || !unitIsBuild(defs[0]) || defs[0].Excerpt != "unit-tests go test ./...") {
			t.Errorf("build: the defect is %+v", defs)
		}
		if name == "failure" && (filed != 1 || defs[0].UnitTests != "TestTotal\tpkg/records/total_test.go" || defs[0].RunRef != idSHA(ucSHAn(1))+"@101") {
			t.Errorf("failure: the defect is %+v", defs)
		}
	}

	// No run yet: the waits grow with the merge's age; after 6 hours, no-ci.
	for age, wait := range map[time.Duration]time.Duration{10 * time.Minute: unitWaitFast, 45 * time.Minute: unitWaitMid, 3 * time.Hour: unitWaitSlow} {
		e := newUnitPollEnv(t, UnitChecks{})
		key := e.watch(t, 1, age)
		e.p.unitPass(ctx, ucNow)
		if w := e.get(t, key); w.State != UnitWatchPending || w.NextAt != ucNow.Add(wait).Unix() || w.Reads != 1 || w.Tries != 0 {
			t.Errorf("no run %s after the merge: %+v; want the next read in %s", age, w, wait)
		}
	}
	e := newUnitPollEnv(t, UnitChecks{})
	key := e.watch(t, 1, 7*time.Hour)
	e.p.unitPass(ctx, ucNow)
	if w := e.get(t, key); w.Outcome != UnitNoCI {
		t.Errorf("no run 7 hours after the merge: %+v; want no-ci", w)
	}
	// In progress: wait, then inconclusive after 6 hours.
	for age, want := range map[time.Duration]string{time.Hour: UnitWatchPending, 7 * time.Hour: UnitInconclusive} {
		e := newUnitPollEnv(t, UnitChecks{})
		key := e.watch(t, 1, age)
		e.greenAt(1, "")
		e.gh.set(func(f *unitGH) { f.checks[ucSHAn(1)][0]["status"] = "in_progress" })
		e.p.unitPass(ctx, ucNow)
		w := e.get(t, key)
		if got := w.State; (want == UnitWatchPending && got != want) || (want != UnitWatchPending && w.Outcome != want) {
			t.Errorf("in progress %s after the merge: %+v; want %s", age, w, want)
		}
	}

	// Unread: a failed read counts and backs off; a rate limit is a failed read; 20
	// failures, or 6 hours with no answer, end inconclusive -- never no-ci.
	for name, st := range map[string]int{"an error": 500, "a rate limit": http.StatusTooManyRequests} {
		e := newUnitPollEnv(t, UnitChecks{})
		key := e.watch(t, 1, 10*time.Minute)
		e.gh.set(func(f *unitGH) { f.fail["/actions/runs"] = st })
		e.p.unitPass(ctx, ucNow)
		if w := e.get(t, key); w.State != UnitWatchPending || w.Tries != 1 || w.Reads != 0 || w.NextAt != ucNow.Add(2*unitWaitFast).Unix() {
			t.Errorf("%s: %+v; want one failed read and a doubled wait", name, w)
		}
		if name == "a rate limit" && e.gh.requests("/actions/runs") != 3 {
			t.Errorf("a rate limit was read %d times; want the two retries and no more", e.gh.requests("/actions/runs"))
		}
	}
	e = newUnitPollEnv(t, UnitChecks{})
	key = e.watch(t, 1, 10*time.Minute)
	e.gh.set(func(f *unitGH) { f.fail["/actions/runs"] = 500 })
	if _, err := e.s.DelayUnitWatch(ctx, key, 0, unitMaxTries-1, 3); err != nil {
		t.Fatal(err)
	}
	e.p.unitPass(ctx, ucNow)
	if w := e.get(t, key); w.Outcome != UnitInconclusive {
		t.Errorf("the 20th failed read: %+v; want inconclusive", w)
	}
	e = newUnitPollEnv(t, UnitChecks{})
	key = e.watch(t, 1, 7*time.Hour)
	e.gh.set(func(f *unitGH) { f.fail["/actions/runs"] = 500 })
	e.p.unitPass(ctx, ucNow)
	if w := e.get(t, key); w.Outcome != UnitInconclusive {
		t.Errorf("never answered in 7 hours: %+v; want inconclusive, never no-ci", w)
	}

	// Ownership: a repository that is no longer a fix target ends inconclusive with no read.
	e = newUnitPollEnv(t, UnitChecks{})
	key = e.watch(t, 1, 10*time.Minute)
	a := unitAF()
	a.AutomationRepos = []string{defRepo}
	if _, err := e.s.SetAutoFix(ctx, e.pid, a, "ops", tokenKindFine); err != nil {
		t.Fatal(err)
	}
	e.p.unitPass(ctx, ucNow)
	if w := e.get(t, key); w.Outcome != UnitInconclusive || e.gh.requests("/repos/") != 0 {
		t.Errorf("a repository no longer a fix target: %+v after %d reads", w, e.gh.requests("/repos/"))
	}

	// The cadence: every fourth poller pass. The budget: 10 watches a pass. The
	// round-robin: the next pass starts after the product the last one served.
	e = newUnitPollEnv(t, UnitChecks{})
	for i := 1; i <= 12; i++ {
		e.watch(t, i, 10*time.Minute)
	}
	if n := e.p.unitPass(ctx, ucNow); n != unitPassBudget {
		t.Errorf("one pass read %d watches; want %d", n, unitPassBudget)
	}
	e2 := newUnitPollEnv(t, UnitChecks{})
	for i := 1; i <= 3; i++ {
		e2.watch(t, i, 10*time.Minute)
	}
	runs := func() int { return e2.gh.requests("/actions/runs") }
	for i := 0; i < 8; i++ {
		e2.p.now = func() time.Time { return ucNow.Add(time.Duration(i) * fixPRPollTick) }
		e2.s.mu.Lock()
		for k, w := range e2.s.unitWatches { // every watch is due at every pass
			w.NextAt = 0
			e2.s.unitWatches[k] = w
		}
		e2.s.mu.Unlock()
		e2.p.Pass(ctx)
		if want := 3 * (i/unitPassEvery + 1); runs() != want {
			t.Errorf("after poller pass %d the runs were read %d times; want %d", i+1, runs(), want)
		}
	}
	rr := newUnitPollEnv(t, UnitChecks{})
	if _, err := rr.s.SetAssigned(ctx, "p-2", []string{"a-12", "a-13"}, true, testEmail); err != nil {
		t.Fatal(err)
	}
	rr.s.mu.Lock()
	rr.s.masterPtr("p-2").Repos = append(rr.s.masterPtr("p-2").Repos, defRepo)
	rr.s.mu.Unlock()
	if _, err := rr.s.SetAutoFix(ctx, "p-2", unitAF(), "ops", tokenKindFine); err != nil {
		t.Fatal(err)
	}
	if _, err := rr.s.SetUnitChecks(ctx, "p-2", UnitChecks{Enabled: true}, testEmail); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 12; i++ {
		rr.watch(t, i, 10*time.Minute)
	}
	p2 := UnitWatch{Repo: defRepo, PR: 99, SHA: ucSHAn(99), Base: "develop", MergedAt: ucNow.Add(-10 * time.Minute).Unix()}
	if _, err := rr.s.PlantUnitWatch(ctx, "p-2", p2); err != nil {
		t.Fatal(err)
	}
	rr.p.unitPass(ctx, ucNow)
	if rr.gh.requests("head_sha="+ucSHAn(99)) != 0 {
		t.Error("the first pass reached the second product past its budget")
	}
	rr.p.unitPass(ctx, ucNow)
	if rr.gh.requests("head_sha="+ucSHAn(99)) != 1 {
		t.Error("the second pass did not start with the product after the one served last")
	}
}

// TC-864
func TestAnnotationsAreScreened(t *testing.T) {
	anns := []map[string]any{
		ucAnn("failure", "main_test.go", "TestRoot", "the root package"),
		ucAnn("failure", "pkg/records/total_test.go", "ExampleTotal", ""),
		ucAnn("failure", "pkg/records/fuzz_test.go", "FuzzParse/seed#1", ""), // '#' is not in a test name
		ucAnn("failure", "pkg/records/fuzz_test.go", "FuzzParse", ""),
		ucAnn("failure", ".github", "TestNoFile/sub_case", ""),
		ucAnn("failure", "", "TestEmptyPath", ""),
		ucAnn("failure", "pkg/records/a_test.go", "BenchmarkTotal", ""),
		ucAnn("failure", "pkg/records/a_test.go", "Test with spaces", ""),
		ucAnn("failure", "../outside_test.go", "TestOutside", ""),
		ucAnn("failure", ".ssh/id_rsa_test.go", "TestKey", ""),
		ucAnn("failure", "pkg/a_test.go", "TestLeak/ghp_"+strings.Repeat("a", 36), ""),
		ucAnn("failure", "pkg/records/total_test.go", "ExampleTotal", ""), // a repeat
		ucAnn("warning", "pkg/records/total_test.go", "TestWarned", ""),
		ucAnn("notice", ".github", unitTotalTitle, "25"),
	}
	var raw []ghAnnotation
	b, _ := json.Marshal(anns)
	_ = json.Unmarshal(b, &raw)
	in, reported, withheld := unitScreen(raw, "", "")
	var got []string
	for _, f := range in.Failing {
		got = append(got, f.Test+"@"+f.File)
	}
	want := "TestRoot@main_test.go,ExampleTotal@pkg/records/total_test.go,FuzzParse@pkg/records/fuzz_test.go,TestNoFile/sub_case@,TestEmptyPath@"
	if !reported || withheld || in.Build || strings.Join(got, ",") != want || in.Total != 25 {
		t.Errorf("kept %v (total %d, build %t, withheld %t); want %s and 25", got, in.Total, in.Build, withheld, want)
	}
	if files := unitFilesOf(in.Failing); strings.Join(files, ",") != "main_test.go,pkg/records/total_test.go,pkg/records/fuzz_test.go" {
		t.Errorf("the protected files are %v; an empty path protects nothing", files)
	}
	// The total is clamped to what was kept and to 100000, and is the kept count without a notice.
	for msg, wantTotal := range map[string]int{"2": 5, "999999": unitTotalMax, "x": 5, "": 5} {
		a := append([]ghAnnotation(nil), raw[:6]...)
		if msg != "" {
			a = append(a, ghAnnotation{AnnotationLevel: "notice", Title: unitTotalTitle, Message: msg})
		}
		if in, _, _ := unitScreen(a, "", ""); in.Total != wantTotal {
			t.Errorf("total notice %q: total %d, want %d", msg, in.Total, wantTotal)
		}
	}
	// At most 10 kept.
	var many []ghAnnotation
	for i := 0; i < 12; i++ {
		many = append(many, ghAnnotation{AnnotationLevel: "failure", Path: "pkg/a_test.go", Title: fmt.Sprintf("TestCase%d", i)})
	}
	if in, rep, _ := unitScreen(many, "", ""); rep || len(in.Failing) != unitMaxFailing || in.Total != unitMaxFailing {
		t.Errorf("twelve failures with no total notice kept %d with total %d, or read as reported; want 10, 10 and not reported", len(in.Failing), in.Total)
	}
	// Every failure withheld: nothing kept, withheld.
	if in, _, withheld := unitScreen(raw[6:11], "", ""); !withheld || len(in.Failing) != 0 {
		t.Errorf("all withheld: kept %v, withheld %t", in.Failing, withheld)
	}
	// No failure-level annotation: a build failure, its summary one screened paragraph of at most 2 KiB.
	in, reported, withheld = unitScreen(raw[12:], "compile error", "pkg/records:\n  undefined: total\n"+strings.Repeat("x", 3000))
	if !reported || withheld || !in.Build || strings.Contains(in.Summary, "\n") || len(in.Summary) > unitSummaryMax ||
		!strings.HasPrefix(in.Summary, "compile error pkg/records:") {
		t.Errorf("a build failure: build %t, summary %q", in.Build, in.Summary)
	}
	if in, _, _ := unitScreen(nil, "boom", "API_TOKEN=abcdefghijklmnop0123"); in.Summary != "(withheld: the summary matched a secret detector)" {
		t.Errorf("a secret in the summary: %q", in.Summary)
	}
}

// TC-869
func TestUnitNoticesAreSentOnceAndResent(t *testing.T) {
	ctx := context.Background()
	e := newUnitPollEnv(t, UnitChecks{})
	key := e.watch(t, 1, 10*time.Minute)
	e.greenAt(1, "failure")
	e.gh.set(func(f *unitGH) {
		f.anns[301] = []map[string]any{ucAnn("failure", "pkg/records/total_test.go", "TestTotal", ""), ucReported}
	})
	e.p.unitPass(ctx, ucNow)
	d := defectOf(t, e.s, e.pid, e.get(t, key).DefectKey)
	h8 := unitH8(d)
	held := fmt.Sprintf("%s|0|unit tests failed on a merge; defect %s waits for an auto-fix admin|%s", e.pid, h8, d.TriageTask)
	if got := e.notices(); len(got) != 1 || got[0] != held {
		t.Fatalf("the held notice: %q; want %q", got, held)
	}
	if d = defectOf(t, e.s, e.pid, d.Key); !d.UnitNoticeSent {
		t.Error("the held notice was not recorded")
	}
	e.p.unitPass(ctx, ucNow)
	if n := len(e.notices()); n != 1 {
		t.Errorf("a second pass sent %d notices; want the one", n)
	}
	// A crash between the store write and the send: the next scan re-sends.
	e.s.mu.Lock()
	e.s.defects[d.Key].UnitNoticeSent = false
	e.s.mu.Unlock()
	e.p.unitPass(ctx, ucNow)
	if got := e.notices(); len(got) != 2 || got[1] != held {
		t.Errorf("the re-send: %q", got)
	}
	// A pass marks the held defect: one passed notice, once.
	e.watch(t, 2, 5*time.Minute)
	e.greenAt(2, "success")
	e.p.unitPass(ctx, ucNow)
	e.p.unitPass(ctx, ucNow)
	passed := fmt.Sprintf("%s|0|defect %s: its failing tests passed at %s; an admin closes it by cancelling the held card|%s",
		e.pid, h8, idSHA(ucSHAn(2)), d.TriageTask)
	if got := e.notices(); len(got) != 3 || got[2] != passed {
		t.Errorf("the passed notice: %q; want %q", got, passed)
	}
	// Released, a pass closes it: one closed notice, once.
	releaseHeld(t, e.s, defectOf(t, e.s, e.pid, d.Key))
	e.watch(t, 3, 4*time.Minute)
	e.greenAt(3, "success")
	e.p.unitPass(ctx, ucNow)
	e.p.unitPass(ctx, ucNow)
	closed := fmt.Sprintf("%s|0|defect %s closed: its tests passed|", e.pid, h8)
	if got := e.notices(); len(got) != 4 || got[3] != closed {
		t.Errorf("the closed notice: %q; want %q", got, closed)
	}
	// All withheld: one notice naming the pull request, nothing filed.
	e.watch(t, 4, 3*time.Minute)
	e.greenAt(4, "failure")
	e.gh.set(func(f *unitGH) {
		f.anns[304] = []map[string]any{ucAnn("failure", "../x_test.go", "TestX", ""), ucReported}
	})
	e.p.unitPass(ctx, ucNow)
	if got := e.notices(); len(got) != 5 || got[4] != e.pid+"|4|unit-test annotations on a merge were all withheld; nothing filed|" {
		t.Errorf("the withheld notice: %q", got)
	}
	// No run: at most one no-ci notice per product per UTC day.
	e.watch(t, 5, 7*time.Hour)
	e.watch(t, 6, 8*time.Hour)
	e.p.unitPass(ctx, ucNow)
	noci := e.pid + "|6|no unit-tests run on a merge; is the workflow installed?|"
	got := e.notices()
	if len(got) != 6 || (got[5] != noci && got[5] != strings.Replace(noci, "|6|", "|5|", 1)) {
		t.Errorf("the no-ci notices: %q", got[5:])
	}
	if day := masterOf(t, e.s, e.pid).UnitChecks.LastNoCINoticeDay; day != "2026-11-27" {
		t.Errorf("the no-ci day is %q", day)
	}
}

// TC-870
func TestUnitRetentionAndContext(t *testing.T) {
	ctx := context.Background()
	e := newUnitPollEnv(t, UnitChecks{})
	sha := ucSHAn(1)
	e.gh.set(func(f *unitGH) {
		f.dirs["pkg/records"] = []map[string]string{
			{"type": "file", "name": "total.go", "path": "pkg/records/total.go"},
			{"type": "file", "name": "total_test.go", "path": "pkg/records/total_test.go"},
			{"type": "file", "name": "README.md", "path": "pkg/records/README.md"},
			{"type": "dir", "name": "sub", "path": "pkg/records/sub"},
		}
		f.files["pkg/records/total_test.go"] = "package records\n\nfunc TestTotal(t *testing.T) {}\n"
		f.files["pkg/records/total.go"] = "package records\n\nfunc total() int { return 1 }\n"
	})
	key := e.watch(t, 1, 10*time.Minute)
	e.greenAt(1, "failure")
	e.gh.set(func(f *unitGH) {
		f.anns[301] = []map[string]any{ucAnn("failure", "pkg/records/total_test.go", "TestTotal", ""), ucReported}
	})
	e.p.unitPass(ctx, ucNow)
	d := defectOf(t, e.s, e.pid, e.get(t, key).DefectKey)
	var paths []string
	for _, f := range d.Context.Files {
		paths = append(paths, f.Path)
	}
	if !d.Context.Built || strings.Join(paths, ",") != "pkg/records/total_test.go,pkg/records/total.go" || d.Context.HeadOf(defRepo) != sha {
		t.Errorf("the context is %v at %s (built %t); want the failing test file, then its directory's source", paths,
			d.Context.HeadOf(defRepo), d.Context.Built)
	}
	if n := e.gh.requests("/contents/pkg/records?ref=" + sha); n != 1 {
		t.Errorf("the directory was listed %d times at the merge commit", n)
	}
	// The QA search never runs for a unit defect.
	if err := buildAndStoreDefectContext(ctx, e.s, e.p.gh, e.pid, d.Key, noSleep); err != nil || e.gh.requests("/search/code") != 0 {
		t.Errorf("the QA context ran for a unit defect: %v, %d searches", err, e.gh.requests("/search/code"))
	}
	// A failed build of the context is retried by a scan, at most every 30 minutes.
	e2 := newUnitPollEnv(t, UnitChecks{})
	e2.gh.set(func(f *unitGH) { f.fail["/contents"] = 500 })
	k2 := e2.watch(t, 1, 10*time.Minute)
	e2.greenAt(1, "failure")
	e2.gh.set(func(f *unitGH) {
		f.anns[301] = []map[string]any{ucAnn("failure", "pkg/records/total_test.go", "TestTotal", ""), ucReported}
	})
	e2.p.unitPass(ctx, ucNow)
	d2 := defectOf(t, e2.s, e2.pid, e2.get(t, k2).DefectKey)
	if d2.Context.Built {
		t.Fatal("a failed context build stored a context")
	}
	e2.p.unitPass(ctx, ucNow.Add(time.Minute))
	if n := e2.gh.requests("/contents"); n != 1 {
		t.Errorf("the context was retried %d times within 30 minutes; want once in all", n)
	}
	e2.gh.set(func(f *unitGH) {
		delete(f.fail, "/contents")
		f.files["pkg/records/total_test.go"] = "package records\n"
	})
	e2.p.now = func() time.Time { return ucNow.Add(31 * time.Minute) }
	e2.p.unitPass(ctx, ucNow.Add(31*time.Minute))
	if d2 = defectOf(t, e2.s, e2.pid, d2.Key); !d2.Context.Built || len(d2.Context.Files) != 1 {
		t.Errorf("the context was not rebuilt after 30 minutes: %+v", d2.Context)
	}

	// Retention: the excerpt and the failing tests go after 30 days, the protected files stay;
	// done watches go 30 days after they ended.
	old := ucNow.Add(31 * 24 * time.Hour).Unix()
	if _, err := e.s.ExpireExcerpts(ctx, old); err != nil {
		t.Fatal(err)
	}
	if d = defectOf(t, e.s, e.pid, d.Key); d.Excerpt != "" || d.UnitTests != "" || strings.Join(d.UnitFiles, ",") != "pkg/records/total_test.go" {
		t.Errorf("after the retention: excerpt %q, tests %q, files %v", d.Excerpt, d.UnitTests, d.UnitFiles)
	}
	pending := e.watch(t, 2, 10*time.Minute)
	e.p.now = func() time.Time { return time.Unix(old, 0) }
	e.p.retain(ctx, time.Unix(old, 0))
	ws, _ := e.s.UnitWatches(ctx, e.pid)
	if len(ws) != 1 || ws[0].Key != pending {
		t.Errorf("after the prune %+v; want only the pending watch", ws)
	}
}
