package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The unit-results route (D-106, docs/designs/release-b4-unit-tests.md §5.4).
// The product, repositories, tests and credentials are invented (D-085); no
// password here is a real one.

const (
	urPass1 = "unit-results-password-one-0123456789"
	urPass2 = "unit-results-password-two-0123456789"
)

// urNotes collects the group notices the route sends.
type urNotes struct{ lines []string }

// unitResultsServer is a server whose store has unit checks on with the Jenkins
// source for the invented fix-target repository, and a unit-results credential
// for p-1 and p-2.
func unitResultsServer(t *testing.T) (*server, *MemStore, string, *urNotes) {
	t.Helper()
	srv, _ := newTestServer(t)
	st, pid := unitOnStore(t, UnitChecks{Source: unitSourceJenkins})
	srv.store = st
	path := filepath.Join(t.TempDir(), "unit-results")
	body := "# one line per product\np-1:jenkins-p1:" + urPass1 + "\np-2:jenkins-p2:" + urPass2 + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GAOS_UNIT_RESULTS_BASIC_AUTH_FILE", path)
	srv.unitResults = unitResultsAuthFromEnv()
	if !srv.unitResults.enabled() {
		t.Fatalf("the unit-results credentials did not load: %s", srv.unitResults.why)
	}
	srv.unitResults.logf = func(string, ...any) {}
	n := &urNotes{}
	srv.unitNotify = func(_ context.Context, _ string, _ int, text, _ string) { n.lines = append(n.lines, text) }
	return srv, st, pid, n
}

// urCall posts a body to the unit-results route; hdr is header name, value pairs.
func urCall(srv *server, product, user, pass, body string, hdr ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/products/"+product+"/unit-results", strings.NewReader(body))
	if user != "" {
		req.SetBasicAuth(user, pass)
	}
	req.RemoteAddr = "192.0.2.7:4444"
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	return rec
}

// urBody is a posted result for merge n of the invented repository on develop.
func urBody(n int, result string, failing ...string) string {
	b := map[string]any{"repo": defRepo, "commit": strings.ToUpper(ucSHAn(n)), "branch": "develop", "result": result,
		"total": len(failing), "failing": []map[string]string{}, "summary": "", "runUrl": "https://jenkins.acme.example/job/x/" + fmt.Sprint(n) + "/"}
	var fs []map[string]string
	for _, f := range uf(failing...) {
		fs = append(fs, map[string]string{"test": f.Test, "file": f.File})
	}
	if fs != nil {
		b["failing"] = fs
	}
	out, _ := json.Marshal(b)
	return string(out)
}

func urPost(srv *server, n int, result string, failing ...string) *httptest.ResponseRecorder {
	return urCall(srv, "p-1", "jenkins-p1", urPass1, urBody(n, result, failing...))
}

func urWatch(t *testing.T, st Store, pid string, n int) UnitWatch {
	t.Helper()
	w, ok, err := st.UnitWatch(context.Background(), unitWatchKey(pid, defRepo, ucSHAn(n)))
	if err != nil || !ok {
		t.Fatalf("watch %d: %v %v", n, ok, err)
	}
	return w
}

func urError(rec *httptest.ResponseRecorder) string {
	var b struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &b)
	return b.Error
}

// TC-873
func TestUnitResultsAreAuthenticatedLikeStaging(t *testing.T) {
	srv, st, pid, _ := unitResultsServer(t)
	watchOn(t, st, pid, "develop", 1, 1000)
	// A request through another mount never reaches the route (BR-52).
	if rec := urCall(srv, "p-1", "jenkins-p1", urPass1, urBody(1, "passed"), "X-Forwarded-Prefix", "/app"); rec.Code != http.StatusNotFound {
		t.Errorf("through /app answered %d, want 404", rec.Code)
	}
	// No credential, a wrong one: 401 with the Basic challenge, never echoing it.
	for name, c := range map[string][2]string{"none": {"", ""}, "wrong password": {"jenkins-p1", "wrong-password-0123456789-0123456789"}} {
		rec := urCall(srv, "p-1", c[0], c[1], urBody(1, "passed"))
		if rec.Code != http.StatusUnauthorized || rec.Header().Get("WWW-Authenticate") != `Basic realm="gaos"` {
			t.Errorf("%s answered %d %q, want 401 with the Basic challenge", name, rec.Code, rec.Header().Get("WWW-Authenticate"))
		}
		if strings.Contains(rec.Body.String(), urPass1) {
			t.Errorf("%s: the answer carries the credential", name)
		}
	}
	// Another product's credential: 404, and it counts toward its own key.
	if rec := urCall(srv, "p-1", "jenkins-p2", urPass2, urBody(1, "passed")); rec.Code != http.StatusNotFound || urError(rec) != "not-found" {
		t.Errorf("product 2's credential on product 1 answered %d %s, want 404 not-found", rec.Code, rec.Body.String())
	}
	srv.unitResults.mu.Lock()
	counted := srv.unitResults.fails["192.0.2.7|unit-results|p-2"].n
	srv.unitResults.mu.Unlock()
	if counted != 1 {
		t.Errorf("the wrong-product failure counted %d against the credential's product; want 1", counted)
	}
	// A staging credential never opens unit results, nor the reverse.
	sp := filepath.Join(t.TempDir(), "staging")
	if err := os.WriteFile(sp, []byte("p-1:jenkins:"+stagingPass1+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GAOS_STAGING_BASIC_AUTH_FILE", sp)
	srv.staging = stagingAuthFromEnv()
	if rec := urCall(srv, "p-1", "jenkins", stagingPass1, urBody(1, "passed")); rec.Code != http.StatusUnauthorized {
		t.Errorf("a staging credential on unit results answered %d, want 401", rec.Code)
	}
	if rec := stagingCall(srv, "p-1", "jenkins-p1", urPass1, stagingOK); rec.Code != http.StatusUnauthorized {
		t.Errorf("a unit-results credential on staging answered %d, want 401", rec.Code)
	}
	// The right credential is accepted, and it is not a staging callback.
	if rec := urPost(srv, 1, "passed"); rec.Code != http.StatusAccepted {
		t.Fatalf("the right credential answered %d %s, want 202", rec.Code, rec.Body.String())
	}
	if _, ok := srv.staging.LastCallback("p-1"); ok {
		t.Error("a unit result was recorded as the last staging callback")
	}
	// Off: 503 jenkins-off, for an unset file and for a password under 32 characters.
	srv.unitResults = nil
	if rec := urPost(srv, 1, "passed"); rec.Code != http.StatusServiceUnavailable || urError(rec) != "jenkins-off" {
		t.Errorf("with no credential file answered %d %s, want 503 jenkins-off", rec.Code, rec.Body.String())
	}
	short := filepath.Join(t.TempDir(), "short")
	if err := os.WriteFile(short, []byte("p-1:u:"+strings.Repeat("x", 31)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GAOS_UNIT_RESULTS_BASIC_AUTH_FILE", short)
	if a := unitResultsAuthFromEnv(); a.enabled() || !strings.Contains(a.why, "at least 32 characters") || strings.Contains(a.why, "xxxx") {
		t.Errorf("a 31-character password was accepted or named: %q", a.why)
	}
	if !srv.staging.has("p-1") || srv.staging.has("p-2") {
		t.Error("has does not answer the product's line")
	}
}

// TC-874
func TestUnitResultsBodyIsStrict(t *testing.T) {
	srv, st, pid, _ := unitResultsServer(t)
	watchOn(t, st, pid, "develop", 1, 1000)
	ok := urBody(1, "failed", "TestTotal")
	for name, body := range map[string]string{
		"unknown field":       strings.Replace(ok, `"branch"`, `"extra":1,"branch"`, 1),
		"trailing data":       ok + `{}`,
		"not json":            `repo=acme`,
		"over 64 KiB":         strings.Replace(ok, `"summary":""`, `"summary":"`+strings.Repeat("a", 70<<10)+`"`, 1),
		"repo":                strings.Replace(ok, defRepo, "no-slash", 1),
		"commit":              strings.Replace(ok, strings.ToUpper(ucSHAn(1)), "abc123", 1),
		"a 64-hex commit":     strings.Replace(ok, strings.ToUpper(ucSHAn(1)), strings.Repeat("a", 64), 1),
		"branch":              strings.Replace(ok, `"develop"`, `"release/../main"`, 1),
		"result":              strings.Replace(ok, `"failed"`, `"green"`, 1),
		"total":               strings.Replace(ok, `"total":1`, `"total":100001`, 1),
		"negative total":      strings.Replace(ok, `"total":1`, `"total":-1`, 1),
		"failing with passed": strings.Replace(ok, `"failed"`, `"passed"`, 1),
		"failing with build":  strings.Replace(ok, `"failed"`, `"build-failed"`, 1),
		"failed with none":    urBody(1, "failed"),
		"summary with failed": strings.Replace(ok, `"summary":""`, `"summary":"boom"`, 1),
		"summary with passed": strings.Replace(urBody(1, "passed"), `"summary":""`, `"summary":"boom"`, 1),
		"fifty-one failing":   urBody(1, "failed", fiftyTests(51)...),
	} {
		rec := urCall(srv, "p-1", "jenkins-p1", urPass1, body)
		if rec.Code != http.StatusUnprocessableEntity || urError(rec) != "rejected" {
			t.Errorf("%s answered %d %s, want 422 rejected", name, rec.Code, rec.Body.String())
		}
	}
	if w := urWatch(t, st, pid, 1); w.State != UnitWatchPending {
		t.Fatalf("a refused body changed the watch: %+v", w)
	}
	if rec := urCall(srv, "p-1", "jenkins-p1", urPass1, urBody(1, "failed")); !strings.Contains(rec.Body.String(), "a failed result names at least one test; use build-failed") {
		t.Errorf("an empty failed list reads %s", rec.Body.String())
	}
	// Display-only fields are cut or dropped, never refused.
	in, _, _, u := unitResultIn(unitResultBody{Result: "passed", RunURL: "http://jenkins.acme.example/job/1/"})
	_ = in
	if u != "" {
		t.Errorf("an http runUrl was kept: %q", u)
	}
	for name, raw := range map[string]string{
		"over 512 bytes": "https://jenkins.acme.example/" + strings.Repeat("a", 600),
		"userinfo":       "https://user:pw@jenkins.acme.example/job/1/",
		"no host":        "https:///job/1/",
		"unparsable":     "https://jenkins.acme.example/%zz",
		"a control":      "https://jenkins.acme.example/job/1/\n",
	} {
		if got := unitRunURL(raw); got != "" {
			t.Errorf("a runUrl with %s was kept: %q", name, got)
		}
	}
	long := strings.Replace(urBody(2, "failed", "TestTotal", "TestKeep"), "TestTotal", "Test"+strings.Repeat("x", 600), 1)
	watchOn(t, st, pid, "develop", 2, 2000)
	rec := urCall(srv, "p-1", "jenkins-p1", urPass1, strings.Replace(long, `https://jenkins.acme.example/job/x/2/`, `http://jenkins.acme.example/job/x/2/`, 1))
	w := urWatch(t, st, pid, 2)
	if rec.Code != http.StatusAccepted || w.RunURL != "" || w.Outcome != UnitFailed {
		t.Fatalf("an http runUrl and a 600-byte test answered %d %s, watch %+v; want 202, stored empty", rec.Code, rec.Body.String(), w)
	}
	if d := defectOf(t, st, pid, w.DefectKey); d.UnitTests != "TestKeep\tpkg/records/keep_test.go" {
		t.Errorf("the 600-byte test was not dropped: %q", d.UnitTests)
	}
	// A 3 KiB summary is capped on a rune boundary.
	summary := strings.Repeat("é", 1536) // 3 KiB
	bin, _, _, _ := unitResultIn(unitResultBody{Result: unitResultBuildFailed, Summary: summary})
	if len(bin.Summary) > unitSummaryMax || !strings.HasPrefix(summary, bin.Summary) || len(bin.Summary) < unitSummaryMax-1 {
		t.Errorf("a 3 KiB summary became %d bytes, valid %t", len(bin.Summary), strings.HasPrefix(summary, bin.Summary))
	}
}

func fiftyTests(n int) []string {
	var out []string
	for i := 0; i < n; i++ {
		out = append(out, fmt.Sprintf("TestCase%02d", i))
	}
	return out
}

// TC-875
func TestAPostedResultFinishesTheWatch(t *testing.T) {
	ctx := context.Background()
	srv, st, pid, notes := unitResultsServer(t)
	// A failure files a held defect, with the poller's outcome, the posted facts and the actor.
	watchOn(t, st, pid, "develop", 1, 1000)
	rec := urPost(srv, 1, "failed", "TestTotal", "TestSum")
	w := urWatch(t, st, pid, 1)
	if rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), `"outcome":"failed"`) || w.Outcome != UnitFailed ||
		w.PostedResult != UnitFailed || w.PostedFrom != "192.0.2.7" || w.RunURL != "https://jenkins.acme.example/job/x/1/" {
		t.Fatalf("a failed result answered %d %s, watch %+v", rec.Code, rec.Body.String(), w)
	}
	d := defectOf(t, st, pid, w.DefectKey)
	if d.RunRef != idSHA(ucSHAn(1))+"@jenkins" || !strings.Contains(d.UnitTests, "TestSum") {
		t.Errorf("the defect is %+v", d)
	}
	held := false
	cards, _ := st.PRCards(ctx, pid)
	for _, c := range cards {
		held = held || (c.ID == d.TriageTask && c.Status == StatusBlocked && c.BlockReason == ReasonUnitHeld)
	}
	if !held {
		t.Errorf("the failed result's triage card is not held: %+v", cards)
	}
	jenkins := 0
	st.mu.RLock()
	for _, e := range st.events {
		if e.Payload.Actor == "jenkins:"+pid {
			jenkins++
		}
	}
	st.mu.RUnlock()
	if jenkins < 2 {
		t.Errorf("%d events carry the actor jenkins:%s; want the report and the card", jenkins, pid)
	}
	// A later pass marks the held defect.
	watchOn(t, st, pid, "develop", 2, 2000)
	if rec := urPost(srv, 2, "passed"); rec.Code != http.StatusAccepted || urWatch(t, st, pid, 2).Outcome != UnitPassed {
		t.Fatalf("a passed result answered %d %s", rec.Code, rec.Body.String())
	}
	if d := defectOf(t, st, pid, d.Key); d.UnitPassedAt != 2000 || defectFinal(d.State) {
		t.Errorf("a pass over a held defect: %+v; want it marked, not closed", d)
	}
	// Released, a newer failure of the same set then a pass closes it.
	releaseHeld(t, st, defectOf(t, st, pid, d.Key))
	watchOn(t, st, pid, "develop", 3, 3000)
	urPost(srv, 3, "failed", "TestTotal", "TestSum")
	watchOn(t, st, pid, "develop", 4, 4000)
	urPost(srv, 4, "passed")
	if d := defectOf(t, st, pid, d.Key); d.State != DefectStateFixed {
		t.Errorf("a pass over a released defect left it %s", d.State)
	}
	// build-failed files the ~build defect with the screened summary.
	watchOn(t, st, pid, "develop", 5, 5000)
	body := strings.Replace(urBody(5, "build-failed"), `"summary":""`, `"summary":"pkg/x.go:3: undefined: y\nexit 2"`, 1)
	if rec := urCall(srv, "p-1", "jenkins-p1", urPass1, body); rec.Code != http.StatusAccepted {
		t.Fatalf("build-failed answered %d %s", rec.Code, rec.Body.String())
	}
	bd := defectOf(t, st, pid, urWatch(t, st, pid, 5).DefectKey)
	if !strings.HasSuffix(bd.CaseID, "~build") || bd.Excerpt != "pkg/x.go:3: undefined: y exit 2" {
		t.Errorf("the build defect is %s %q", bd.CaseID, bd.Excerpt)
	}
	// Every entry withheld: refused:withheld and the notice.
	watchOn(t, st, pid, "develop", 6, 6000)
	body = strings.Replace(urBody(6, "failed", "TestTotal"), `"test":"TestTotal"`, `"test":"not a test"`, 1)
	if rec := urCall(srv, "p-1", "jenkins-p1", urPass1, body); rec.Code != http.StatusAccepted || urWatch(t, st, pid, 6).Outcome != "refused:withheld" {
		t.Errorf("all withheld answered %d %s", rec.Code, rec.Body.String())
	}
	if len(notes.lines) != 1 || notes.lines[0] != unitWithheldNotice {
		t.Errorf("the withheld notice: %q", notes.lines)
	}
	// 50 sent, 10 kept, Total from the count kept before the cut; screening drops.
	tests := fiftyTests(50)
	in, _, _, _ := unitResultIn(unitResultBody{Result: UnitFailed, Total: 3, Failing: append([]unitResultFailure{
		{Test: "TestLeak", File: "pkg/token=abcdefghijklmnop_test.go"}, {Test: "Test`x`"}, {Test: "TestBad", File: "../etc/passwd"}},
		func() []unitResultFailure {
			var o []unitResultFailure
			for _, x := range tests {
				o = append(o, unitResultFailure{Test: x})
			}
			return o
		}()...)})
	if len(in.Failing) != 10 || in.Total != 50 || in.Failing[0].Test != "TestCase00" {
		t.Errorf("53 sent: kept %d, total %d, first %v; want 10, 50, TestCase00", len(in.Failing), in.Total, in.Failing)
	}
	// Another branch: 409 branch-mismatch, nothing stored.
	watchOn(t, st, pid, "develop", 7, 7000)
	body = strings.Replace(urBody(7, "passed"), `"develop"`, `"staging"`, 1)
	if rec := urCall(srv, "p-1", "jenkins-p1", urPass1, body); rec.Code != http.StatusConflict || urError(rec) != "branch-mismatch" ||
		urWatch(t, st, pid, 7).State != UnitWatchPending {
		t.Errorf("another branch answered %d %s", rec.Code, rec.Body.String())
	}
	// Not a target: 422 not-target without a watch; with one, inconclusive 202.
	other := strings.Replace(urBody(8, "passed"), defRepo, "acme-org/other", 1)
	if rec := urCall(srv, "p-1", "jenkins-p1", urPass1, other); rec.Code != http.StatusUnprocessableEntity || urError(rec) != "not-target" {
		t.Errorf("a repository that is not a target answered %d %s", rec.Code, rec.Body.String())
	}
	a := unitAF()
	a.AutomationRepos = []string{defRepo}
	if _, err := st.SetAutoFix(ctx, pid, a, "ops", tokenKindFine); err != nil {
		t.Fatal(err)
	}
	if rec := urPost(srv, 7, "passed"); rec.Code != http.StatusAccepted || urWatch(t, st, pid, 7).Outcome != UnitInconclusive {
		t.Errorf("a watch no longer a target answered %d %s", rec.Code, rec.Body.String())
	}
	// The poller-race re-read: a watch the poller ended first is answered for the stored state.
	if _, err := st.SetAutoFix(ctx, pid, unitAF(), "ops", tokenKindFine); err != nil {
		t.Fatal(err)
	}
	key := watchOn(t, st, pid, "develop", 9, 9000)
	racing := &raceStore{Store: st, key: key}
	srv.store = racing
	rec = urPost(srv, 9, "failed", "TestTotal")
	if rec.Code != http.StatusConflict || urError(rec) != "already-finished" || !racing.fired {
		t.Errorf("a result that lost the race answered %d %s", rec.Code, rec.Body.String())
	}
}

// raceStore ends a watch inconclusive (as the poller would) right before the
// route's finish.
type raceStore struct {
	Store
	key   string
	fired bool
}

func (r *raceStore) FinishUnitWatchFrom(ctx context.Context, key, outcome string, in *UnitDefectIn, pass *UnitPassIn, from UnitPosted) (Result, error) {
	if key == r.key && !r.fired {
		r.fired = true
		if _, err := r.Store.FinishUnitWatch(ctx, key, UnitInconclusive, nil, nil); err != nil {
			return Result{}, err
		}
	}
	return r.Store.FinishUnitWatchFrom(ctx, key, outcome, in, pass, from)
}

// TC-876
func TestUnitResultsRefusedWithoutAJenkinsWatch(t *testing.T) {
	ctx := context.Background()
	srv, st, pid, _ := unitResultsServer(t)
	watchOn(t, st, pid, "develop", 1, 1000)
	// No watch for a fix target: 409 no-watch, Retry-After 60; six of them lock nothing.
	for i := 0; i < 6; i++ {
		rec := urPost(srv, 2, "passed")
		if rec.Code != http.StatusConflict || urError(rec) != "no-watch" || rec.Header().Get("Retry-After") != "60" {
			t.Fatalf("attempt %d with no watch answered %d %s Retry-After %q; want 409 no-watch, 60", i+1, rec.Code, rec.Body.String(), rec.Header().Get("Retry-After"))
		}
	}
	if n := srv.unitResults.failCount(); n != 0 {
		t.Errorf("no-watch answers counted %d failure keys; want none", n)
	}
	if rec := urPost(srv, 1, "passed"); rec.Code != http.StatusAccepted {
		t.Errorf("after six no-watch answers the right post answered %d", rec.Code)
	}
	// Off, and GitHub Actions: 409, nothing stored.
	watchOn(t, st, pid, "develop", 3, 3000)
	for want, u := range map[string]UnitChecks{
		"unit-checks-off": {Source: unitSourceJenkins},
		"not-jenkins":     {Enabled: true},
	} {
		if _, err := st.SetUnitChecks(ctx, pid, u, testEmail); err != nil {
			t.Fatal(err)
		}
		rec := urPost(srv, 3, "passed")
		if rec.Code != http.StatusConflict || urError(rec) != want {
			t.Errorf("%s: answered %d %s", want, rec.Code, rec.Body.String())
		}
		if w := urWatch(t, st, pid, 3); w.State != UnitWatchPending || w.PostedResult != "" {
			t.Errorf("%s: the watch changed: %+v", want, w)
		}
	}
}

// TC-879
func TestALateOrConflictingUnitResult(t *testing.T) {
	ctx := context.Background()
	srv, st, pid, notes := unitResultsServer(t)
	// A result over no-ci within 24 hours is accepted and files.
	k1 := watchOn(t, st, pid, "develop", 1, 1000)
	if _, err := st.FinishUnitWatch(ctx, k1, UnitNoCI, nil, nil); err != nil {
		t.Fatal(err)
	}
	rec := urPost(srv, 1, "failed", "TestTotal")
	if w := urWatch(t, st, pid, 1); rec.Code != http.StatusAccepted || w.Outcome != UnitFailed || w.DefectKey == "" || w.PostedResult != UnitFailed {
		t.Fatalf("a late result answered %d %s, watch %+v; want it filed", rec.Code, rec.Body.String(), w)
	}
	// The same result again: 200 duplicate. A different one: 409, one notice, the first stands.
	if rec := urPost(srv, 1, "failed", "TestTotal"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"duplicate":true`) {
		t.Errorf("the same result answered %d %s; want 200 duplicate", rec.Code, rec.Body.String())
	}
	for i := 0; i < 20; i++ {
		rec := urPost(srv, 1, "passed")
		if rec.Code != http.StatusConflict || urError(rec) != "already-finished" || !strings.Contains(rec.Body.String(), `"outcome":"failed"`) {
			t.Fatalf("conflict %d answered %d %s", i+1, rec.Code, rec.Body.String())
		}
	}
	if len(notes.lines) != 1 || notes.lines[0] != unitConflictNotice {
		t.Errorf("20 conflicting posts sent %q; want one conflict notice", notes.lines)
	}
	if w := urWatch(t, st, pid, 1); !w.ConflictNoticeSent || w.Outcome != UnitFailed || w.PostedResult != UnitFailed {
		t.Errorf("after the conflicts the watch is %+v", w)
	}
	// A poller-finished watch compares by class: passed is passed, build-failed is failed.
	k2 := watchOn(t, st, pid, "develop", 2, 2000)
	if _, err := st.FinishUnitWatch(ctx, k2, UnitPassed, nil, &UnitPassIn{RunID: 7}); err != nil {
		t.Fatal(err)
	}
	if rec := urPost(srv, 2, "passed"); rec.Code != http.StatusOK {
		t.Errorf("a pass over a GitHub pass answered %d", rec.Code)
	}
	// After 24 hours a no-ci watch is finished: the same class is a duplicate, any other a conflict.
	k3 := watchOn(t, st, pid, "develop", 3, 3000)
	if _, err := st.FinishUnitWatch(ctx, k3, UnitNoCI, nil, nil); err != nil {
		t.Fatal(err)
	}
	st.mu.Lock()
	w3 := st.unitWatches[k3]
	w3.DoneAt = time.Now().Add(-25 * time.Hour).Unix()
	st.unitWatches[k3] = w3
	st.mu.Unlock()
	if rec := urPost(srv, 3, "passed"); rec.Code != http.StatusConflict || urError(rec) != "already-finished" {
		t.Errorf("a result 25 hours after no-ci answered %d %s; want 409", rec.Code, rec.Body.String())
	}
	if w := urWatch(t, st, pid, 3); w.Outcome != UnitNoCI || w.PostedResult != "" {
		t.Errorf("a refused late result changed the watch: %+v", w)
	}
}

// TC-882
func TestTheDocumentedJenkinsBodyIsAccepted(t *testing.T) {
	ctx := context.Background()
	b, err := os.ReadFile("../pustaka/docs/integrations/agents.html")
	if err != nil {
		t.Fatal(err)
	}
	page := string(b)
	at := strings.Index(page, `id="release-b-unit-jenkins"`)
	if at < 0 {
		t.Fatal("the agents page has no #release-b-unit-jenkins section")
	}
	const open = `<pre id="unit-results-sample">`
	i := strings.Index(page[at:], open)
	if i < 0 {
		t.Fatal(`the #release-b-unit-jenkins section has no <pre id="unit-results-sample">`)
	}
	rest := page[at+i+len(open):]
	j := strings.Index(rest, "</pre>")
	if j < 0 {
		t.Fatal("the sample is not closed")
	}
	sample := html.UnescapeString(rest[:j])

	srv, st, pid, _ := unitResultsServer(t)
	const repo = "acme/route-api"
	st.mu.Lock()
	st.masterPtr(pid).Repos = append(st.masterPtr(pid).Repos, repo)
	st.mu.Unlock()
	af := unitAF()
	af.TargetRepos = append(af.TargetRepos, repo)
	if _, err := st.SetAutoFix(ctx, pid, af, "ops", tokenKindFine); err != nil {
		t.Fatal(err)
	}
	const commit = "0123456789abcdef0123456789abcdef01234567"
	if _, err := st.PlantUnitWatch(ctx, pid, UnitWatch{Repo: repo, PR: 42, SHA: commit, Base: "develop", MergedAt: 1000}); err != nil {
		t.Fatal(err)
	}
	rec := urCall(srv, pid, "jenkins-p1", urPass1, sample)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("the documented body answered %d %s; want 202\n%s", rec.Code, rec.Body.String(), sample)
	}
	w, _, _ := st.UnitWatch(ctx, unitWatchKey(pid, repo, commit))
	if w.Outcome != UnitFailed || w.RunURL != "https://jenkins.acme.example/job/route-api/job/develop/42/" {
		t.Errorf("the documented body finished the watch as %+v", w)
	}
}
