package main

import (
	"context"
	"strings"
	"testing"
)

// Release B, phase B3: an approved spec that merged runs its case (D-103 §4).

const approved = "0123456789abcdef0123456789abcdef01234567"

// mergedFixture: p-1 follows loop/laundry-auto, has the case planned (implemented
// when impl), and a QA row for pull request #4 approved at `approved`.
func mergedFixture(t *testing.T, impl bool) (*MemStore, string) {
	t.Helper()
	st := NewMemStore(1)
	ctx := context.Background()
	st.mu.Lock()
	st.masterPtr("p-1").AutoFix.AutomationRepos = []string{"loop/laundry-auto"}
	st.mu.Unlock()
	importCases(t, st, "p-1", true, levelCase("TC-API-001", "API"))
	if impl {
		implement(t, st)
	}
	if _, err := st.UpsertPullRequest(ctx, PullRequest{ProductID: "p-1", Repo: "loop/laundry-auto", Number: 4,
		Kind: PRKindQA, QAJob: "job-1", QACase: "TC-API-001", QASha: approved}); err != nil {
		t.Fatal(err)
	}
	return st, pullRequestKey("p-1", "loop/laundry-auto", 4)
}

func implement(t *testing.T, st Store) {
	t.Helper()
	if _, _, err := st.QaApplyResults(context.Background(), "p-1", QaResultsIn{Source: "qa-tool",
		Items: []QaResultItem{{CaseID: "TC-API-001", Implemented: boolp(true)}}}); err != nil {
		t.Fatal(err)
	}
}

func mergedRuns(t *testing.T, st Store) []TaskRecord {
	t.Helper()
	cards, _ := st.PRCards(context.Background(), "p-1")
	var out []TaskRecord
	for _, c := range cards {
		if strings.HasPrefix(c.DedupKey, qaMergedPrefix) {
			out = append(out, c)
		}
	}
	return out
}

func merge(t *testing.T, st Store, key, head string) Result {
	t.Helper()
	res, err := st.AdvancePullRequest(context.Background(), "p-1", key, PRFields{State: PRStateMerged, HeadSHA: head, CheckedAt: 1})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func rowNote(t *testing.T, st *MemStore, key string) PullRequest {
	t.Helper()
	st.mu.RLock()
	defer st.mu.RUnlock()
	return st.pullRequests[key]
}

// TC-831
func TestAMergedApprovedSpecRunsItsCaseOnce(t *testing.T) {
	st, key := mergedFixture(t, true)
	merge(t, st, key, strings.ToUpper(approved))
	runs := mergedRuns(t, st)
	if len(runs) != 1 {
		t.Fatalf("%d runs, want 1", len(runs))
	}
	r := runs[0]
	if r.DedupKey != "qa-merged/p-1:loop/laundry-auto#4:TC-API-001" || r.Title != "E2E run of TC-API-001 after its spec merged" ||
		!hasLabel(r.Labels, LabelE2ERun) || repoKindOf(r.Labels) != "" {
		t.Errorf("the run is %q %q %v", r.DedupKey, r.Title, r.Labels)
	}
	if row := rowNote(t, st, key); row.QANote != QANotePlanned || row.FinalAt == 0 {
		t.Errorf("the row is note=%q finalAt=%d", row.QANote, row.FinalAt)
	}
	// A repeat of the merge plans nothing more; the catalog sync neither.
	merge(t, st, key, approved)
	implement(t, st)
	if n := len(mergedRuns(t, st)); n != 1 {
		t.Errorf("%d runs after a repeat, want 1", n)
	}
	// The runner runs that case alone, outside the pull-request cap.
	qa := &fakeQAJobs{statuses: []string{"running"}}
	newQAJobRunner(st, qa, &prContextCache{}, nil, nil).Tick(context.Background())
	if len(qa.started) != 1 || qa.started[0] != "p-1|TC-API-001|run" {
		t.Errorf("started %v", qa.started)
	}
	if _, _, _, ok := prKeyParts(r.DedupKey); ok {
		t.Error("a merged-run key read as a pull request")
	}
}

// TC-832
func TestAMergedUnapprovedHeadIsReportedAndDoesNotRun(t *testing.T) {
	st, key := mergedFixture(t, true)
	res := merge(t, st, key, strings.Repeat("f", 40))
	if n := len(mergedRuns(t, st)); n != 0 {
		t.Fatalf("an unapproved head planned %d runs", n)
	}
	if row := rowNote(t, st, key); row.QANote != QANoteUnapproved {
		t.Errorf("the row note is %q", row.QANote)
	}
	found := false
	for _, e := range res.NewEvents {
		found = found || strings.Contains(e.Text, "not the approved commit")
	}
	if !found {
		t.Error("no event records the unapproved head")
	}
	// A row with no approved commit recorded never runs either.
	st2, key2 := mergedFixture(t, true)
	st2.mu.Lock()
	row := st2.pullRequests[key2]
	row.QASha = ""
	st2.pullRequests[key2] = row
	st2.mu.Unlock()
	merge(t, st2, key2, approved)
	if n := len(mergedRuns(t, st2)); n != 0 {
		t.Errorf("a row with no approved commit planned %d runs", n)
	}
}

// TC-833
func TestAMergedSpecWaitsForTheCatalog(t *testing.T) {
	st, key := mergedFixture(t, false)
	merge(t, st, key, approved)
	if n := len(mergedRuns(t, st)); n != 0 || rowNote(t, st, key).QANote != QANoteCatalog {
		t.Fatalf("before the catalog: %d runs, note %q", n, rowNote(t, st, key).QANote)
	}
	implement(t, st)
	if n := len(mergedRuns(t, st)); n != 1 || rowNote(t, st, key).QANote != QANotePlanned {
		t.Errorf("after the catalog: %d runs, note %q", n, rowNote(t, st, key).QANote)
	}
	// Past the 14-day window a late catalog plans nothing.
	old := autoFixNow
	t.Cleanup(func() { autoFixNow = old })
	st2, key2 := mergedFixture(t, false)
	merge(t, st2, key2, approved)
	now := old()
	autoFixNow = func() int64 { return now + 15*86400 }
	implement(t, st2)
	if n := len(mergedRuns(t, st2)); n != 0 {
		t.Errorf("a catalog 15 days late planned %d runs", n)
	}
}

// TC-834
func TestAMergedSpecWithoutQARoleDoesNotRun(t *testing.T) {
	st, key := mergedFixture(t, true)
	if _, err := st.SetAssigned(context.Background(), "p-1", []string{"a-10"}, true, testEmail); err != nil {
		t.Fatal(err)
	}
	merge(t, st, key, approved)
	if n := len(mergedRuns(t, st)); n != 0 || rowNote(t, st, key).QANote != QANoteUnassigned {
		t.Errorf("%d runs, note %q", n, rowNote(t, st, key).QANote)
	}
}

// TC-835
func TestAutoMergeFollowsSwitchAndBand(t *testing.T) {
	ctx := context.Background()
	st := NewMemStore(1)
	p1 := masterOf(t, st, "p-1")
	band := ReadinessBand(p1.ReadinessScore())
	if band != "working" && band != "complete" {
		t.Fatalf("the fixture p-1 is in band %s; this test needs Working or Complete", band)
	}
	if specAutoMergeAllowed(p1) {
		t.Error("allowed with the switch off")
	}
	p1.AutoMergeSpecs = true
	if !specAutoMergeAllowed(p1) {
		t.Error("refused with the switch on in a Working band")
	}
	p5 := masterOf(t, st, "p-5")
	p5.AutoMergeSpecs = true
	if b := ReadinessBand(p5.ReadinessScore()); (b == "working" || b == "complete") != specAutoMergeAllowed(p5) {
		t.Errorf("p-5 in band %s answered %t", b, specAutoMergeAllowed(p5))
	}
	// The runner asks the rule on a create, and only of a tool that lists auto-merge.
	for _, tc := range []struct {
		caps map[string]bool
		on   bool
		want string
	}{{nil, true, "<nil>"}, {map[string]bool{"auto-merge": true}, false, "false"}, {map[string]bool{"auto-merge": true}, true, "true"}} {
		qa := &fakeQAJobs{statuses: []string{"running"}, caps: tc.caps}
		r, st, _, _ := e2eFixture(t, qa, "QA-case: CART-12\n")
		_, _ = st.SetRepoKinds(ctx, "p-1", nil, false, tc.on, testEmail)
		r.autoMerge = storeAutoMerge(st)
		r.Tick(ctx)
		got := "<nil>"
		if len(qa.ins) == 1 && qa.ins[0].AutoMerge != nil {
			got = map[bool]string{true: "true", false: "false"}[*qa.ins[0].AutoMerge]
		}
		if got != tc.want {
			t.Errorf("caps %v switch %t sent auto_merge %s, want %s", tc.caps, tc.on, got, tc.want)
		}
	}
}

// TC-836
func TestQaShaIsRecordedEvenWhenTheURLComesFirst(t *testing.T) {
	ctx := context.Background()
	r, st, _, _ := qaRunnerWithPR(t, "https://github.com/loop/laundry-auto/pull/9", "", []string{"loop/laundry-auto"},
		"awaiting_approval", "running", "running")
	qa := r.qa.(*fakeQAJobs)
	qa.caps = map[string]bool{"auto-merge": true}
	r.Tick(ctx) // started
	r.Tick(ctx) // the URL, no sha yet
	key := pullRequestKey("p-1", "loop/laundry-auto", 9)
	if row := rowNote(t, st, key); row.QASha != "" {
		t.Fatalf("a sha appeared before the tool sent one: %q", row.QASha)
	}
	qa.sha = "not-a-sha"
	r.Tick(ctx)
	if row := rowNote(t, st, key); row.QASha != "" {
		t.Errorf("a malformed sha was stored: %q", row.QASha)
	}
	qa.sha = strings.ToUpper(approved)
	r.Tick(ctx)
	if row := rowNote(t, st, key); row.QASha != approved {
		t.Errorf("the sha is %q, want the approved commit, lower-cased", row.QASha)
	}
}

// TC-837
func TestSpecAutoMergeRoute(t *testing.T) {
	srv, st, _ := apiFixture(t, func([]string) *apiConfig { return newAPIConfig(tokAdmin, tokQA, tokOut, "p-1") })
	path := "/v1/products/p-1/spec-auto-merge"
	if rec := call(t, srv, "GET", path, "", ""); rec.Code != 401 {
		t.Errorf("no token answered %d", rec.Code)
	}
	if rec := call(t, srv, "GET", path, tokOut, ""); rec.Code == 200 {
		t.Error("the outbound token reached the route")
	}
	if rec := call(t, srv, "GET", "/v1/products/p-2/spec-auto-merge", tokQA, ""); rec.Code != 404 {
		t.Errorf("an unscoped product answered %d, want 404", rec.Code)
	}
	if b := call(t, srv, "GET", path, tokQA, "").Body.String(); !strings.Contains(b, `"allowed":false`) {
		t.Errorf("with the switch off: %s", b)
	}
	_, _ = st.SetRepoKinds(context.Background(), "p-1", nil, false, true, testEmail)
	if b := call(t, srv, "GET", path, tokQA, "").Body.String(); !strings.Contains(b, `"allowed":true`) {
		t.Errorf("with the switch on in a Working band: %s", b)
	}
}

// TC-838: the Release B fields of a pull-request row survive a restart on ArcadeDB.
func TestPullRequestReleaseBFieldsSurviveARestart(t *testing.T) {
	arc := arcadeTestTarget(t)
	ctx := t.Context()
	tenant := "conf-release-b-row"
	first := newArcadeTestStore(t, arc, tenant)
	if _, err := first.SetAutoFix(ctx, "p-1", AutoFix{AllowedGlobs: []string{"tests/**"}, AutomationRepos: []string{"loop/laundry-auto"}}, "ops", tokenKindFine); err != nil {
		t.Fatal(err)
	}
	if _, err := first.UpsertPullRequest(ctx, PullRequest{ProductID: "p-1", Repo: "loop/laundry-auto", Number: 4,
		Kind: PRKindQA, QAJob: "job-1", QACase: "TC-API-001", QASha: approved}); err != nil {
		t.Fatal(err)
	}
	key := pullRequestKey("p-1", "loop/laundry-auto", 4)
	if _, err := first.AdvancePullRequest(ctx, "p-1", key, PRFields{State: PRStateMerged, HeadSHA: approved, CheckedAt: 1}); err != nil {
		t.Fatal(err)
	}
	second, err := NewArcadeStoreFor(ctx, arc, 1, tenant)
	if err != nil {
		t.Fatal(err)
	}
	rows, _ := second.PullRequests(ctx, "p-1")
	for _, r := range rows {
		if r.Key == key {
			if r.QASha != approved || r.QANote != QANoteCatalog || r.FinalAt == 0 {
				t.Errorf("reopened as sha=%q note=%q finalAt=%d", r.QASha, r.QANote, r.FinalAt)
			}
			return
		}
	}
	t.Error("the row did not survive a reopen")
}
