package main

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Release B, phase B2: missing automation created on a staging deploy (D-103 §3).

func levelCase(id, level string) QaCase {
	c := qaCase(id)
	c.Level = level
	return c
}

// createFixture: p-1 with a be and a fullstack repository, staging creates on,
// and planned cases of every level.
func createFixture(t *testing.T, kinds map[string]string, cases ...QaCase) *MemStore {
	t.Helper()
	st := NewMemStore(1)
	if _, err := st.SetRepoKinds(context.Background(), "p-1", kinds, true, false, testEmail); err != nil {
		t.Fatal(err)
	}
	importCases(t, st, "p-1", true, cases...)
	return st
}

func createCards(t *testing.T, st *MemStore) map[string]TaskRecord {
	t.Helper()
	cards, _ := st.PRCards(context.Background(), "p-1")
	out := map[string]TaskRecord{}
	for _, c := range cards {
		if strings.HasPrefix(c.DedupKey, stagingCreatePrefix) {
			out[stagingCreateCase(c.DedupKey)] = c
		}
	}
	return out
}

var bothKinds = map[string]string{"loop/laundry-api": "be", "loop/laundry-web": "fullstack"}

func sha(c string) string { return strings.Repeat(c, 40) }

// TC-825
func TestStagingPlansCreatesForUncoveredCases(t *testing.T) {
	ctx := context.Background()
	cases := []QaCase{levelCase("TC-API-001", "API"), levelCase("TC-WEB-001", "WEB"), levelCase("TC-GQL-001", "GQL")}

	// The switch off: exactly today.
	off := createFixture(t, bothKinds, cases...)
	_, _ = off.SetRepoKinds(ctx, "p-1", bothKinds, false, false, testEmail)
	if _, _, n, _, _ := off.PlanStagingCards(ctx, "p-1", stagingEvent("laundry-api", sha("1"))); n != 0 {
		t.Errorf("with the switch off %d creates were planned", n)
	}

	// A be deploy creates the API case only, labelled be; GQL is never created.
	st := createFixture(t, bothKinds, cases...)
	_, _, n, _, err := st.PlanStagingCards(ctx, "p-1", stagingEvent("laundry-api", sha("2")))
	got := createCards(t, st)
	if err != nil || n != 1 || len(got) != 1 || !hasLabel(got["TC-API-001"].Labels, LabelE2EKindBE) ||
		!hasLabel(got["TC-API-001"].Labels, LabelE2EAutomation) {
		t.Fatalf("a be deploy planned %d: %v (err %v)", n, got, err)
	}
	if c := got["TC-API-001"]; c.Title != "E2E automation of TC-API-001 (staging deploy of loop/laundry-api)" || c.DedupKey != "staging-create/p-1:TC-API-001#1" {
		t.Errorf("the create is %q %q", c.Title, c.DedupKey)
	}
	// A replay of the commit plans none.
	if _, _, n, _, _ := st.PlanStagingCards(ctx, "p-1", stagingEvent("laundry-api", sha("2"))); n != 0 {
		t.Errorf("a replay planned %d creates", n)
	}
	// A fullstack deploy creates the WEB case, labelled fullstack.
	_, _, n, _, _ = st.PlanStagingCards(ctx, "p-1", stagingEvent("laundry-web", sha("3")))
	if got := createCards(t, st); n != 1 || !hasLabel(got["TC-WEB-001"].Labels, LabelE2EKindFullstack) {
		t.Errorf("a fullstack deploy planned %d: %v", n, got)
	}

	// A fullstack-only product creates its API case too, and asks for a backend spec.
	solo := createFixture(t, map[string]string{"loop/laundry-web": "fullstack"}, cases...)
	_, _, n, _, _ = solo.PlanStagingCards(ctx, "p-1", stagingEvent("laundry-web", sha("4")))
	got = createCards(t, solo)
	if n != 2 || !hasLabel(got["TC-API-001"].Labels, LabelE2EKindBE) || !hasLabel(got["TC-WEB-001"].Labels, LabelE2EKindFullstack) {
		t.Errorf("a fullstack-only product planned %d: %v", n, got)
	}

	// A case code that could break a key is never planned.
	if caseCodeOK("TC-1:x") || caseCodeOK("TC#1") || caseCodeOK("") || !caseCodeOK("TC-API-001") {
		t.Error("caseCodeOK does not refuse a key delimiter")
	}
	// The runner reads the case from the key, before the run shortcut.
	r := newQAJobRunner(st, &fakeQAJobs{}, &prContextCache{}, nil, nil)
	if code := r.caseCodeFor(ctx, TaskRecord{DedupKey: "staging-create/p-1:TC-API-001#2"}, "create"); code != "TC-API-001" {
		t.Errorf("caseCodeFor read %q", code)
	}
}

// TC-826
func TestCreateBlockingRules(t *testing.T) {
	ctx := context.Background()
	old := autoFixNow
	t.Cleanup(func() { autoFixNow = old })
	now := time.Now().Unix()
	day := int64(86400)
	plan := func(st *MemStore, c string) int {
		_, _, n, _, _ := st.PlanStagingCards(ctx, "p-1", stagingEvent("laundry-api", sha(c)))
		return n
	}
	one := map[string]string{"loop/laundry-api": "be"}

	// An open create blocks; a failed one blocks for a week, then retries as #2.
	st := createFixture(t, one, levelCase("TC-API-001", "API"))
	plan(st, "1")
	if plan(st, "2") != 0 {
		t.Error("an open create did not block")
	}
	c := createCards(t, st)["TC-API-001"]
	if _, err := st.ResolvePRCard(ctx, "p-1", c.ID, StatusFailed, "qa-job-failed"); err != nil {
		t.Fatal(err)
	}
	if plan(st, "3") != 0 {
		t.Error("a create that failed today was retried")
	}
	autoFixNow = func() int64 { return now + 8*day }
	if plan(st, "4") != 1 {
		t.Error("a create that failed eight days ago was not retried")
	}
	if c := createCards(t, st)["TC-API-001"]; c.DedupKey != "staging-create/p-1:TC-API-001#2" {
		t.Errorf("the retry's key is %q, want #2", c.DedupKey)
	}
	autoFixNow = old

	// A pull-request card whose recorded case is the case blocks.
	st = createFixture(t, one, levelCase("TC-API-001", "API"))
	opened, _, _, _ := st.PlanPullRequestCards(ctx, "p-1", prEvent(prTypeOpened, "laundry-api", 70))
	for _, id := range opened {
		_, _ = st.ResolvePRCard(ctx, "p-1", id, StatusRunning, qaJobReason("job-5", "TC-API-001", "started"))
	}
	if plan(st, "5") != 0 {
		t.Error("a pull request's automation of the case did not block")
	}

	// The spec rows: open 30 days, merged-not-implemented 14, closed 7.
	for _, tc := range []struct {
		state   string
		age     int64
		blocked bool
	}{
		{PRStateOpen, 29, true}, {PRStateOpen, 31, false},
		{PRStateMerged, 13, true}, {PRStateMerged, 15, false},
		{PRStateClosed, 6, true}, {PRStateClosed, 8, false},
	} {
		st := createFixture(t, one, levelCase("TC-API-001", "API"))
		at := now - tc.age*day
		st.mu.Lock()
		st.pullRequests["row"] = PullRequest{Key: "row", ProductID: "p-1", Repo: "loop/laundry-auto", Number: 3,
			Kind: PRKindQA, State: tc.state, QACase: "TC-API-001", OpenedAt: at, FinalAt: at}
		st.mu.Unlock()
		if got := plan(st, "6") == 0; got != tc.blocked {
			t.Errorf("a %s row %d days old: blocked=%t, want %t", tc.state, tc.age, got, tc.blocked)
		}
	}
}

// TC-827
func TestCreatesHaveTheirOwnCap(t *testing.T) {
	ctx := context.Background()
	var cases []QaCase
	for _, id := range []string{"TC-API-001", "TC-API-002", "TC-API-003", "TC-API-004", "TC-API-005", "TC-API-006"} {
		cases = append(cases, levelCase(id, "API"))
	}
	st := createFixture(t, map[string]string{"loop/laundry-api": "be"}, cases...)
	counts := []int{}
	for _, c := range []string{"1", "2", "3"} {
		_, _, n, _, _ := st.PlanStagingCards(ctx, "p-1", stagingEvent("laundry-api", sha(c)))
		counts = append(counts, n)
	}
	// Two per deploy, four open: 2, 2, 0.
	if counts[0] != 2 || counts[1] != 2 || counts[2] != 0 {
		t.Errorf("creates per deploy %v, want [2 2 0]", counts)
	}
	// Creates are outside the pull-request cap.
	st.mu.Lock()
	n := st.openPRCardsLocked("p-1")
	st.mu.Unlock()
	if n != 3 {
		t.Errorf("the pull-request cap counts %d open cards, want the 3 runs only", n)
	}
	if _, _, _, ok := prKeyParts("staging-create/p-1:TC-API-001#1"); ok {
		t.Error("a create key read as a pull request")
	}
	t.Setenv("GAOS_E2E_CREATE_MAX", "1")
	st2 := createFixture(t, map[string]string{"loop/laundry-api": "be"}, cases...)
	if _, _, n, _, _ := st2.PlanStagingCards(ctx, "p-1", stagingEvent("laundry-api", sha("9"))); n != 1 {
		t.Errorf("GAOS_E2E_CREATE_MAX=1 planned %d", n)
	}
}

// TC-828
func TestStagingAnswersCarryCreates(t *testing.T) {
	srv, st := stagingServer(t)
	ctx := context.Background()
	_, _ = st.SetRepoKinds(ctx, "p-1", map[string]string{"loop/laundry-api": "be"}, true, false, testEmail)
	importCases(t, st, "p-1", true, levelCase("TC-API-001", "API"))
	rec := stagingCall(srv, "p-1", "jenkins", stagingPass1, stagingOK)
	if rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), `"creates":1`) {
		t.Fatalf("answered %d %s, want 202 with one create", rec.Code, rec.Body.String())
	}
	rec = stagingCall(srv, "p-1", "jenkins", stagingPass1, stagingOK)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"creates":0`) {
		t.Errorf("a replay answered %d %s", rec.Code, rec.Body.String())
	}
	// Capped: 409 with the creates that were still planned.
	t.Setenv("GAOS_PR_MAX_OPEN", "1")
	importCases(t, st, "p-1", false, levelCase("TC-API-002", "API"))
	other := strings.Replace(stagingOK, "0123456789abcdef", "fedcba9876543210", 1)
	rec = stagingCall(srv, "p-1", "jenkins", stagingPass1, other)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"creates":1`) {
		t.Errorf("a capped run answered %d %s, want 409 with one create", rec.Code, rec.Body.String())
	}
	// Unassigned: 422.
	_, _ = st.SetAssigned(ctx, "p-1", nil, true, testEmail)
	third := strings.Replace(stagingOK, "0123456789abcdef", "00000000aaaaaaaa", 1)
	if rec := stagingCall(srv, "p-1", "jenkins", stagingPass1, third); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("an unassigned product answered %d", rec.Code)
	}
}

// TC-829: a capped run's creates persist on ArcadeDB.
func TestCreatesPersistWhenTheRunIsCapped(t *testing.T) {
	arc := arcadeTestTarget(t)
	ctx := t.Context()
	tenant := "conf-staging-create-capped"
	first := newArcadeTestStore(t, arc, tenant)
	if _, err := first.SetRepoKinds(ctx, "p-1", map[string]string{"loop/laundry-api": "be"}, true, false, testEmail); err != nil {
		t.Fatal(err)
	}
	if _, _, err := first.QaImport(ctx, "p-1", QaImportIn{Seed: true, Cases: []QaCase{levelCase("TC-API-001", "API")}}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GAOS_PR_MAX_OPEN", "1")
	if _, _, _, err := first.PlanPullRequestCards(ctx, "p-1", prEvent(prTypeOpened, "laundry-web", 80)); err != nil {
		t.Fatal(err)
	}
	_, _, n, res, err := first.PlanStagingCards(ctx, "p-1", stagingEvent("laundry-api", sha("7")))
	if err != nil || !res.Capped || n != 1 {
		t.Fatalf("capped=%t creates=%d err=%v", res.Capped, n, err)
	}
	second, err := NewArcadeStoreFor(ctx, arc, 1, tenant)
	if err != nil {
		t.Fatal(err)
	}
	cards, _ := second.PRCards(ctx, "p-1")
	// The pull request's own cards are written at once too (they waited for a later
	// mutation that carried the floor before Release B).
	if len(cards) != 3 {
		t.Errorf("%d cards after a reopen, want the review, the automation and the create", len(cards))
	}
	found := false
	for _, c := range cards {
		found = found || c.DedupKey == "staging-create/p-1:TC-API-001#1"
	}
	if !found {
		t.Error("the create planned beside a capped run did not survive a reopen")
	}
}

// TC-830
func TestCreateDoneReasons(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		auto, want string
		caps       map[string]bool
	}{
		{"enabled", "case:TC-API-001 spec PR open (https://github.com/loop/laundry-auto/pull/4); it merges when its required check passes", map[string]bool{"kind": true}},
		{"refused:no-ruleset", "a person merges it (auto-merge refused: no-ruleset)", map[string]bool{"kind": true}},
		{"", "; a person merges it (kind not sent)", nil},
	} {
		st := createFixture(t, map[string]string{"loop/laundry-api": "be"}, levelCase("TC-API-001", "API"))
		st.mu.Lock()
		st.masterPtr("p-1").AutoFix.AutomationRepos = []string{"loop/laundry-auto"}
		st.mu.Unlock()
		if _, _, _, _, err := st.PlanStagingCards(ctx, "p-1", stagingEvent("laundry-api", sha("8"))); err != nil {
			t.Fatal(err)
		}
		// A link the QA tool wrote is never repeated: GAOS rebuilds it.
		qa := &fakeQAJobs{statuses: []string{"succeeded"}, caps: tc.caps, auto: tc.auto,
			pr: "https://github.com/loop/laundry-auto/pull/4"}
		r := newQAJobRunner(st, qa, &prContextCache{}, nil, nil)
		r.Tick(ctx)
		r.Tick(ctx)
		c := createCards(t, st)["TC-API-001"]
		if c.Status != StatusDone || !strings.Contains(c.BlockReason, tc.want) {
			t.Errorf("auto_merge %q ended %s %q, want …%s", tc.auto, c.Status, c.BlockReason, tc.want)
		}
	}
	if got := autoMergeOutcome("refused:" + strings.Repeat("x", 200)); len(got) > 140 {
		t.Errorf("a long refusal is not bounded: %d", len(got))
	}
	if got := autoMergeOutcome("bogus"); got != "a person merges it" {
		t.Errorf("an unknown auto_merge read %q", got)
	}
}
