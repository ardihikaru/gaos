package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// Release B, phase B1: repository kinds (D-103 §2).

// TC-819
func TestRepoKindsAreSavedAndValidated(t *testing.T) {
	st := NewMemStore(1)
	ctx := context.Background()
	if _, err := st.SetAssigned(ctx, "p-1", []string{"a-5"}, true, testEmail); err != nil {
		t.Fatal(err)
	}
	for name, kinds := range map[string]map[string]string{
		"not a repository of the product": {"loop/elsewhere": "be"},
		"an unknown kind":                 {"loop/laundry-api": "frontend"},
	} {
		if _, err := st.SetRepoKinds(ctx, "p-1", kinds, false, false, testEmail); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	res, err := st.SetRepoKinds(ctx, "p-1", map[string]string{"Loop/Laundry-API": "be", "loop/laundry-web": "fullstack"}, true, false, testEmail)
	if err != nil {
		t.Fatal(err)
	}
	m := masterOf(t, st, "p-1")
	if m.RepoKinds["loop/laundry-api"] != "be" || m.RepoKinds["loop/laundry-web"] != "fullstack" || !m.StagingCreates || m.AutoMergeSpecs {
		t.Errorf("saved %v staging=%t merge=%t", m.RepoKinds, m.StagingCreates, m.AutoMergeSpecs)
	}
	want := "repository kinds by "
	if len(res.NewEvents) != 1 || !strings.Contains(res.NewEvents[0].Text, want) ||
		!strings.Contains(res.NewEvents[0].Text, "loop/laundry-api=be loop/laundry-web=fullstack; staging creates on; spec auto-merge off") {
		t.Errorf("the event reads %+v", res.NewEvents)
	}
	// The map replaces: an empty value drops a kind.
	if _, err := st.SetRepoKinds(ctx, "p-1", map[string]string{"loop/laundry-api": "be", "loop/laundry-web": ""}, false, false, testEmail); err != nil {
		t.Fatal(err)
	}
	m = masterOf(t, st, "p-1")
	if len(m.RepoKinds) != 1 || m.StagingCreates {
		t.Errorf("after a replace %v staging=%t", m.RepoKinds, m.StagingCreates)
	}
	// Assignment is untouched.
	if !m.AssignedSet || strings.Join(m.Assigned, ",") != "a-5" {
		t.Errorf("kinds changed the assignment: %v %t", m.Assigned, m.AssignedSet)
	}
	// And the route: action=kinds, admins only, assignment untouched.
	srv, st2 := newTestServer(t)
	autoFixAdminsFile(t, 0o600, testEmail)
	body := url.Values{"product": {"p-1"}, "action": {"kinds"}, "kind.loop/laundry-web": {"fullstack"}, "autoMergeSpecs": {"on"}}.Encode()
	rec := formPost(t, srv, "/agents/assign", body, true)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "E2E repository kinds") {
		t.Fatalf("the kinds form answered %d", rec.Code)
	}
	if m := masterOf(t, st2, "p-1"); m.RepoKinds["loop/laundry-web"] != "fullstack" || !m.AutoMergeSpecs || m.AssignedSet {
		t.Errorf("the route saved %v merge=%t assignedSet=%t", m.RepoKinds, m.AutoMergeSpecs, m.AssignedSet)
	}
}

// TC-820: kinds and the two switches survive a restart on ArcadeDB.
func TestRepoKindsSurviveARestart(t *testing.T) {
	arc := arcadeTestTarget(t)
	ctx := t.Context()
	tenant := "conf-repo-kinds-restart"
	first := newArcadeTestStore(t, arc, tenant)
	if _, err := first.SetRepoKinds(ctx, "p-1", map[string]string{"loop/laundry-api": "be"}, true, true, testEmail); err != nil {
		t.Fatal(err)
	}
	second, err := NewArcadeStoreFor(ctx, arc, 1, tenant)
	if err != nil {
		t.Fatal(err)
	}
	m := masterOf(t, second, "p-1")
	if m.RepoKinds["loop/laundry-api"] != "be" || !m.StagingCreates || !m.AutoMergeSpecs {
		t.Errorf("after a reopen %v staging=%t merge=%t", m.RepoKinds, m.StagingCreates, m.AutoMergeSpecs)
	}
	if o := masterOf(t, second, "p-2"); o.RepoKinds != nil || o.StagingCreates || o.AutoMergeSpecs {
		t.Errorf("an untouched product reopened with %v %t %t", o.RepoKinds, o.StagingCreates, o.AutoMergeSpecs)
	}
}

func stagingEvent(repo, commit string) PREvent {
	return PREvent{Kind: "staging", Type: prTypeStaging, Org: "loop", Repo: repo, Commit: commit}
}

// TC-821
func TestRunCardsCarryTheRepositoryKind(t *testing.T) {
	st := NewMemStore(1)
	ctx := context.Background()
	if _, err := st.SetRepoKinds(ctx, "p-1", map[string]string{"loop/laundry-api": "be"}, false, false, testEmail); err != nil {
		t.Fatal(err)
	}
	planned, _, _, _, err := st.PlanStagingCards(ctx, "p-1", stagingEvent("laundry-api", strings.Repeat("a", 40)))
	if err != nil || len(planned) != 1 {
		t.Fatalf("planned %v err %v", planned, err)
	}
	if c := prCards(t, st, "p-1")[planned[0]]; !hasLabel(c.Labels, LabelE2EKindBE) {
		t.Errorf("the staging run is labelled %v, want e2e-kind:be", c.Labels)
	}
	// A repository with no kind: no label.
	planned, _, _, _, _ = st.PlanStagingCards(ctx, "p-1", stagingEvent("laundry-web", strings.Repeat("b", 40)))
	if c := prCards(t, st, "p-1")[planned[0]]; repoKindOf(c.Labels) != "" {
		t.Errorf("an unconfigured repository's run is labelled %v", c.Labels)
	}
	// A pull request's automation card: no label.
	opened, _, _, _ := st.PlanPullRequestCards(ctx, "p-1", prEvent(prTypeOpened, "laundry-api", 61))
	for _, id := range opened {
		if c := prCards(t, st, "p-1")[id]; repoKindOf(c.Labels) != "" {
			t.Errorf("a pr.opened card %s is labelled %v", id, c.Labels)
		}
	}
	// The operator cannot set it.
	if _, err := st.SetTaskLabel(ctx, "p-1", opened[0], LabelE2EKindFullstack, testEmail); err == nil {
		t.Error("an operator set a repository-kind label")
	}
}

// capsServer answers the QA tool's capabilities route; it counts the calls.
func capsServer(t *testing.T, code *int, body *string, calls *int) *qaOutbound {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*calls++
		w.WriteHeader(*code)
		_, _ = w.Write([]byte(*body))
	}))
	t.Cleanup(srv.Close)
	o := newQaOutbound(srv.URL, "tok-tok-tok-tok-tok-tok-tok-tok-tok-tok", nil)
	if o == nil {
		t.Skip("the test URL is not an allowed outbound origin")
	}
	return o
}

// TC-822
func TestKindAndAutoMergeAreSentOnlyWithTheCapability(t *testing.T) {
	ctx := context.Background()
	code, body, calls := 200, `{"capabilities":["kind","auto-merge"]}`, 0
	o := capsServer(t, &code, &body, &calls)
	if c := o.capabilities(ctx); !c["kind"] || !c["auto-merge"] {
		t.Fatalf("capabilities %v", c)
	}
	o.capabilities(ctx)
	if calls != 1 {
		t.Errorf("%d calls inside the cache window, want 1", calls)
	}
	// After the window an error keeps the last known set.
	o.capsAt = time.Now().Add(-qaCapsTTL - time.Second)
	code, body = 500, "no"
	if c := o.capabilities(ctx); !c["kind"] {
		t.Errorf("an error dropped the last known set: %v", c)
	}
	// A 404 is the empty set.
	o.capsAt = time.Now().Add(-qaCapsTTL - time.Second)
	code = 404
	if c := o.capabilities(ctx); len(c) != 0 {
		t.Errorf("a 404 answered %v", c)
	}
	// The body: omitempty keeps today's shape when nothing is set.
	b, _ := json.Marshal(qaStartIn{Product: "p-1", CaseCode: "A-1", Mode: "run"})
	if string(b) != `{"product":"p-1","caseCode":"A-1","mode":"run"}` {
		t.Errorf("an empty request reads %s", b)
	}
	yes := true
	b, _ = json.Marshal(qaStartIn{Product: "p-1", CaseCode: "A-1", Mode: "create", Kind: "be", AutoMerge: &yes})
	if !strings.Contains(string(b), `"kind":"be"`) || !strings.Contains(string(b), `"auto_merge":true`) {
		t.Errorf("a full request reads %s", b)
	}
	// The runner sends the kind only to a tool that lists it.
	for _, tc := range []struct {
		caps     map[string]bool
		wantKind string
	}{{nil, ""}, {map[string]bool{"kind": true}, "be"}} {
		st := NewMemStore(1)
		_, _ = st.SetRepoKinds(ctx, "p-1", map[string]string{"loop/laundry-api": "be"}, false, false, testEmail)
		if _, _, _, _, err := st.PlanStagingCards(ctx, "p-1", stagingEvent("laundry-api", strings.Repeat("c", 40))); err != nil {
			t.Fatal(err)
		}
		qa := &fakeQAJobs{statuses: []string{"running"}, caps: tc.caps}
		r := newQAJobRunner(st, qa, &prContextCache{}, nil, nil)
		r.autoMerge = func(context.Context, string) bool { return true }
		r.Tick(ctx)
		if len(qa.ins) != 1 || qa.ins[0].Kind != tc.wantKind || qa.ins[0].AutoMerge != nil {
			t.Errorf("caps %v sent %+v, want kind %q and no auto_merge on a run", tc.caps, qa.ins, tc.wantKind)
		}
	}
}

// TC-823
func TestNoKindIsTodaysBehaviour(t *testing.T) {
	ctx := context.Background()
	st := NewMemStore(1)
	planned, _, creates, _, err := st.PlanStagingCards(ctx, "p-1", stagingEvent("laundry-api", strings.Repeat("d", 40)))
	if err != nil || len(planned) != 1 || creates != 0 {
		t.Fatalf("planned %v creates %d err %v", planned, creates, err)
	}
	qa := &fakeQAJobs{statuses: []string{"succeeded"}, caps: map[string]bool{"kind": true, "auto-merge": true}}
	r := newQAJobRunner(st, qa, &prContextCache{}, nil, nil)
	r.Tick(ctx)
	if len(qa.ins) != 1 || qa.ins[0] != (qaStartIn{Product: "p-1", CaseCode: "*", Mode: "run"}) {
		t.Errorf("sent %+v, want today's run of every case", qa.ins)
	}
	r.Tick(ctx)
	c := prCards(t, st, "p-1")[planned[0]]
	if c.Status != StatusDone || strings.Contains(c.Reason, "kind not sent") {
		t.Errorf("the run ended %s %q", c.Status, c.Reason)
	}
}

// TC-824
func TestEveryReasonCarriesTheCase(t *testing.T) {
	ctx := context.Background()
	qa := &fakeQAJobs{statuses: []string{"awaiting_approval"}}
	r, st, _, e2e := e2eFixture(t, qa, "QA-case: CART-12\n")
	r.Tick(ctx) // started
	r.Tick(ctx) // waits for approval
	var reason string
	cards, _ := st.PRCards(ctx, "p-1")
	for _, c := range cards {
		if c.ID == e2e {
			reason = c.BlockReason
		}
	}
	if caseFromReason(reason) != "CART-12" || jobIDFromReason(reason) != "job-1" {
		t.Fatalf("the waiting reason %q lost the job or the case", reason)
	}
	// A restart adopts the job with its case: the row it records names it.
	st.mu.Lock()
	st.masterPtr("p-1").AutoFix.AutomationRepos = []string{"loop/laundry-auto"}
	st.mu.Unlock()
	qa2 := &fakeQAJobs{statuses: []string{"awaiting_approval"}, pr: "https://github.com/loop/laundry-auto/pull/9"}
	r2 := newQAJobRunner(st, qa2, &prContextCache{}, nil, nil)
	r2.Tick(ctx)
	r2.Tick(ctx)
	if len(qa2.started) != 0 {
		t.Errorf("an adopted job was started again: %v", qa2.started)
	}
	rows, _ := st.PullRequests(ctx, "p-1")
	found := false
	for _, row := range rows {
		found = found || (row.Kind == PRKindQA && row.Number == 9 && row.QACase == "CART-12")
	}
	if !found {
		t.Errorf("the adopted job's row does not name its case: %+v", rows)
	}
}
