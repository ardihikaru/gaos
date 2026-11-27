package main

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// Agent assignment per product (D-102, docs/designs/agent-seats-and-pr-agents.md §3).

func masterOf(t *testing.T, st Store, id string) Master {
	t.Helper()
	ms, err := st.Masters(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range ms {
		if m.ID == id {
			return m
		}
	}
	t.Fatalf("no product %s", id)
	return Master{}
}

// TC-800
func TestSetAssignedRefusesAndRecordsTheActor(t *testing.T) {
	ctx := context.Background()
	st := NewMemStore(1)

	if _, err := st.SetAssigned(ctx, "p-404", nil, true, testEmail); !errors.Is(err, ErrNotFound) {
		t.Errorf("an unknown product answered %v, want ErrNotFound", err)
	}
	if _, err := st.SetAssigned(ctx, "p-1", []string{"a-99"}, true, testEmail); !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "a-99") {
		t.Errorf("an unknown agent answered %v, want a rejection naming it", err)
	}
	if _, err := st.SetAssigned(ctx, "p-1", []string{"a-10"}, false, testEmail); !errors.Is(err, ErrRejected) {
		t.Errorf("a reset with agents answered %v, want a rejection", err)
	}

	res, err := st.SetAssigned(ctx, "p-1", []string{"a-11", "a-10", "a-10", " "}, true, testEmail)
	if err != nil {
		t.Fatal(err)
	}
	m := masterOf(t, st, "p-1")
	if !m.AssignedSet || strings.Join(m.Assigned, ",") != "a-10,a-11" {
		t.Errorf("assigned %v set=%t, want a-10,a-11 and set", m.Assigned, m.AssignedSet)
	}
	if len(res.NewEvents) != 1 || res.NewEvents[0].Type != RecordUpdated {
		t.Fatalf("events %v, want one RecordUpdated", res.NewEvents)
	}
	ev := res.NewEvents[0]
	if ev.Payload.Actor != actorAlias(testEmail) || strings.Contains(ev.Text, "@") ||
		!strings.Contains(ev.Text, "a-10 a-11") || !strings.Contains(ev.Text, "request changes") {
		t.Errorf("event %q actor %q: want the alias, the list and the first-save notice, never the address", ev.Text, ev.Payload.Actor)
	}

	// A second save does not repeat the first-save notice; a reset clears.
	res, _ = st.SetAssigned(ctx, "p-1", nil, true, testEmail)
	if strings.Contains(res.NewEvents[0].Text, "request changes") || !strings.Contains(res.NewEvents[0].Text, "no agents assigned") {
		t.Errorf("second save said %q", res.NewEvents[0].Text)
	}
	if _, err := st.SetAssigned(ctx, "p-1", nil, false, testEmail); err != nil {
		t.Fatal(err)
	}
	if m := masterOf(t, st, "p-1"); m.AssignedSet || m.Assigned != nil {
		t.Errorf("after reset: %v set=%t, want empty and unset", m.Assigned, m.AssignedSet)
	}
}

// TC-802 (the gate half; the COMMENT half is in prreview_test.go)
func TestNotConfiguredIsTodaysBehaviour(t *testing.T) {
	ctx := context.Background()
	st := NewMemStore(1)
	m := masterOf(t, st, "p-1")
	if m.AssignedSet {
		t.Fatal("a seeded product starts configured")
	}
	for _, role := range assignGatedRoles {
		if !roleRuns(m, role) {
			t.Errorf("role %s does not run on an unconfigured product", role)
		}
	}
	planned, _, res, err := st.PlanPullRequestCards(ctx, "p-1", prEvent(prTypeOpened, "laundry-api", 7))
	if err != nil || len(planned) != 2 || res.Skipped != "" {
		t.Errorf("planned %v skipped %q err %v, want both cards and no skip", planned, res.Skipped, err)
	}
}

// TC-805
func TestUnassignedRoleGetsNoCard(t *testing.T) {
	ctx := context.Background()
	st := NewMemStore(1)
	// A card planned before the product was configured.
	before, _, _, err := st.PlanPullRequestCards(ctx, "p-1", prEvent(prTypeOpened, "laundry-api", 6))
	if err != nil || len(before) != 2 {
		t.Fatalf("before: %v %v", before, err)
	}
	// Only the qa role is on.
	if _, err := st.SetAssigned(ctx, "p-1", []string{"a-5"}, true, testEmail); err != nil {
		t.Fatal(err)
	}
	planned, _, res, err := st.PlanPullRequestCards(ctx, "p-1", prEvent(prTypeOpened, "laundry-api", 8))
	if err != nil {
		t.Fatal(err)
	}
	if len(planned) != 1 || !strings.HasSuffix(planned[0], "e2e-build") && !strings.Contains(planned[0], "e2e") {
		t.Errorf("planned %v, want only the e2e card", planned)
	}
	if res.Skipped != skippedUnassigned+"pr-reviewer" {
		t.Errorf("skipped %q, want unassigned:pr-reviewer", res.Skipped)
	}
	// Every role off: nothing planned, not an error.
	if _, err := st.SetAssigned(ctx, "p-1", nil, true, testEmail); err != nil {
		t.Fatal(err)
	}
	planned, _, res, err = st.PlanPullRequestCards(ctx, "p-1", prEvent(prTypeOpened, "laundry-api", 9))
	if err != nil || len(planned) != 0 || res.Skipped != skippedUnassigned+"pr-reviewer,qa" {
		t.Errorf("planned %v skipped %q err %v", planned, res.Skipped, err)
	}
	// The cards planned before stay as they were.
	cards := prCards(t, st, "p-1")
	for _, id := range before {
		if c, ok := cards[id]; !ok || c.Status != StatusPending {
			t.Errorf("the earlier card %s is %+v, want it left pending", id, c.Status)
		}
	}

	// The staging callback answers 422 unassigned.
	srv, sst := stagingServer(t)
	if _, err := sst.SetAssigned(ctx, "p-1", []string{"a-10"}, true, testEmail); err != nil {
		t.Fatal(err)
	}
	rec := stagingCall(srv, "p-1", "jenkins", stagingPass1, stagingOK)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "unassigned") ||
		strings.Contains(rec.Body.String(), "a-5") {
		t.Errorf("staging answered %d %s, want 422 unassigned naming no agent", rec.Code, rec.Body.String())
	}
}

// TC-806 (in memory; the ArcadeDB half is TestGatedOpenStillWritesThePullRequestRow)
func TestMergeStillCancelsWhenUnitTestIsUnassigned(t *testing.T) {
	ctx := context.Background()
	st := NewMemStore(1)
	open, _, _, err := st.PlanPullRequestCards(ctx, "p-1", prEvent(prTypeOpened, "laundry-api", 11))
	if err != nil || len(open) != 2 {
		t.Fatal(open, err)
	}
	if _, err := st.SetAssigned(ctx, "p-1", []string{"a-10", "a-5"}, true, testEmail); err != nil {
		t.Fatal(err)
	}
	planned, _, res, err := st.PlanPullRequestCards(ctx, "p-1", prEvent(prTypeMerged, "laundry-api", 11))
	if err != nil || len(planned) != 0 || res.Skipped != skippedUnassigned+"unit-test" {
		t.Fatalf("merge planned %v skipped %q err %v", planned, res.Skipped, err)
	}
	cards := prCards(t, st, "p-1")
	for _, id := range open {
		if cards[id].Status != StatusCanceled {
			t.Errorf("%s is %s after the merge, want canceled", id, cards[id].Status)
		}
	}
	// A gated-out open still writes its Pull requests row.
	if _, err := st.SetAssigned(ctx, "p-1", nil, true, testEmail); err != nil {
		t.Fatal(err)
	}
	_, _, res, err = st.PlanPullRequestCards(ctx, "p-1", prEvent(prTypeOpened, "laundry-api", 12))
	if err != nil || len(res.PullRequests) == 0 {
		t.Errorf("a gated open carried %d rows, err %v; want its row", len(res.PullRequests), err)
	}
}

// TC-804
func TestAssignRouteIsAdminOnly(t *testing.T) {
	srv, st := newTestServer(t)
	body := url.Values{"product": {"p-1"}, "agent": {"a-10"}, "action": {"save"}}.Encode()

	if rec := formPost(t, srv, "/agents/assign", body, false); rec.Code == http.StatusOK {
		t.Errorf("no session answered %d", rec.Code)
	}
	t.Setenv("GAOS_AUTOFIX_ADMINS", "")
	if rec := formPost(t, srv, "/agents/assign", body, true); rec.Code != http.StatusForbidden {
		t.Errorf("a non-admin answered %d, want 403", rec.Code)
	}
	autoFixAdminsFile(t, 0o600, testEmail)
	rec := formPost(t, srv, "/agents/assign", body, true)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Edit assignments") {
		t.Fatalf("an admin answered %d", rec.Code)
	}
	if m := masterOf(t, st, "p-1"); !m.AssignedSet || strings.Join(m.Assigned, ",") != "a-10" {
		t.Errorf("assigned %v set=%t", m.Assigned, m.AssignedSet)
	}
	// A reset ignores every ticked box.
	reset := url.Values{"product": {"p-1"}, "agent": {"a-10", "a-11"}, "action": {"reset"}}.Encode()
	if rec := formPost(t, srv, "/agents/assign", reset, true); rec.Code != http.StatusOK {
		t.Fatalf("reset answered %d %s", rec.Code, rec.Body.String())
	}
	if m := masterOf(t, st, "p-1"); m.AssignedSet || m.Assigned != nil {
		t.Errorf("after reset %v set=%t", m.Assigned, m.AssignedSet)
	}
	bad := url.Values{"product": {"p-1"}, "action": {"drop"}}.Encode()
	if rec := formPost(t, srv, "/agents/assign", bad, true); rec.Code != http.StatusBadRequest {
		t.Errorf("an unknown action answered %d, want 400", rec.Code)
	}
}

// TC-803
func TestAgentsPageFiltersByProductAndPagesKeepTheFilter(t *testing.T) {
	srv, st := newTestServer(t)
	ctx := context.Background()
	if _, err := st.SetAssigned(ctx, "p-1", []string{"a-10", "a-11"}, true, testEmail); err != nil {
		t.Fatal(err)
	}
	row := `<div class="proj agent-row">`
	at := func(view string) map[string]string {
		return map[string]string{"HX-Current-URL": "https://gaos.example/" + view}
	}

	all := boardHX(t, srv, "GET", "/ui/bo/agents?page=2", at("agents")) // Juno, a-10, is on page 2
	if !strings.Contains(all.Body.String(), `title="Assigned to `) {
		t.Error("the unfiltered page shows no assignment chip")
	}
	one := boardHX(t, srv, "GET", "/ui/bo/agents?product=p-1", at("agents"))
	b := one.Body.String()
	if n := strings.Count(b, row); n != 2 || !strings.Contains(b, "Juno") || !strings.Contains(b, "Kite") {
		t.Errorf("the p-1 filter lists %d agents, want Juno and Kite", n)
	}
	if u := one.Header().Get("HX-Replace-Url"); !strings.HasSuffix(u, "/agents?product=p-1") {
		t.Errorf("the address is %q, want agents?product=p-1", u)
	}
	// An unconfigured product lists every agent under its notice.
	p2 := boardHX(t, srv, "GET", "/ui/bo/agents?product=p-2", at("agents")).Body.String()
	if !strings.Contains(p2, "Assignments not configured") {
		t.Error("an unconfigured product does not say so")
	}
	// An unknown product is no filter, and never reaches the address.
	odd := boardHX(t, srv, "GET", "/ui/bo/agents?product=p-zzz%22x", at("agents"))
	if strings.Contains(odd.Body.String(), "p-zzz") || strings.Contains(odd.Header().Get("HX-Replace-Url"), "product=") {
		t.Error("an unknown product reached the page or the address")
	}
	// Pages keep the filter: an unconfigured product has every agent, three pages.
	pg := boardHX(t, srv, "GET", "/ui/bo/agents?product=p-2&page=2", at("agents"))
	if u := pg.Header().Get("HX-Replace-Url"); !strings.HasSuffix(u, "/agents?product=p-2&page=2") {
		t.Errorf("page 2 address %q, want product then page", u)
	}
	if !strings.Contains(pg.Body.String(), `hx-get="ui/bo/agents?product=p-2&amp;page=3"`) {
		t.Error("the pager link drops the product filter")
	}
}

// TC-801: assignments survive a restart on ArcadeDB.
func TestAssignmentsSurviveARestart(t *testing.T) {
	arc := arcadeTestTarget(t)
	ctx := t.Context()
	tenant := "conf-assign-restart"
	first := newArcadeTestStore(t, arc, tenant)
	if _, err := first.SetAssigned(ctx, "p-1", []string{"a-11", "a-10"}, true, testEmail); err != nil {
		t.Fatal(err)
	}
	second, err := NewArcadeStoreFor(ctx, arc, 1, tenant)
	if err != nil {
		t.Fatal(err)
	}
	m := masterOf(t, second, "p-1")
	if !m.AssignedSet || strings.Join(m.Assigned, ",") != "a-10,a-11" {
		t.Errorf("after a reopen: %v set=%t, want a-10,a-11 set", m.Assigned, m.AssignedSet)
	}
	if o := masterOf(t, second, "p-2"); o.AssignedSet || o.Assigned != nil {
		t.Errorf("an untouched product reopened as %v set=%t", o.Assigned, o.AssignedSet)
	}
}

// TC-806: a gated-out open still writes its Pull requests row, on ArcadeDB too.
func TestGatedOpenStillWritesThePullRequestRow(t *testing.T) {
	arc := arcadeTestTarget(t)
	ctx := t.Context()
	tenant := "conf-assign-gated-row"
	first := newArcadeTestStore(t, arc, tenant)
	if _, err := first.SetAssigned(ctx, "p-1", nil, true, testEmail); err != nil {
		t.Fatal(err)
	}
	planned, _, res, err := first.PlanPullRequestCards(ctx, "p-1", prEvent(prTypeOpened, "laundry-api", 31))
	if err != nil || len(planned) != 0 || !strings.HasPrefix(res.Skipped, skippedUnassigned) {
		t.Fatalf("planned %v skipped %q err %v", planned, res.Skipped, err)
	}
	second, err := NewArcadeStoreFor(ctx, arc, 1, tenant)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := second.PullRequests(ctx, "p-1")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range rows {
		found = found || r.Number == 31
	}
	if !found {
		t.Errorf("the gated open's row did not survive a reopen (%d rows)", len(rows))
	}
}

// TC-818: the cards assignment does not gate say where they came from (D-102 §22).
func TestCardSourceLabels(t *testing.T) {
	ctx := context.Background()
	st := NewMemStore(1)
	api, _, _, err := st.PlanAPICard(ctx, "p-1", APICardIn{Client: "plat", ExternalID: "e1", Kind: APIKindPRReview,
		Owner: "loop", Repo: "laundry-api", Number: 51, Dispatch: true}, 1000)
	if err != nil || !hasLabel(api.Labels, LabelSourceAPI) {
		t.Fatalf("an API card has labels %v (err %v), want source:api", api.Labels, err)
	}
	tp, created, _, err := st.PlanBundleCard(ctx, "p-1", "taskmgr:ABC-1", 2)
	if err != nil || !created || !hasLabel(tp.Labels, LabelSourceBundle) {
		t.Fatalf("a test-planner card has labels %v (created %t, err %v), want source:bundle", tp.Labels, created, err)
	}
	// A Brain Town card carries no source label.
	planned, _, _, _ := st.PlanPullRequestCards(ctx, "p-1", prEvent(prTypeOpened, "laundry-api", 52))
	st.mu.RLock()
	for _, id := range planned {
		if l := st.taskByIDLocked(id).Labels; severityOf(l) != "" || strings.Contains(strings.Join(l, ","), "source:") {
			t.Errorf("a Brain Town card carries %v", l)
		}
	}
	st.mu.RUnlock()
	// The planner sets it; an operator cannot.
	if _, err := st.SetTaskLabel(ctx, "p-1", planned[0], LabelSourceAPI, testEmail); err == nil {
		t.Error("an operator set a source label")
	}
	// It is a facet, so the board's label filter offers it.
	p, err := st.BoardPage(ctx, "p-1", BoardQuery{Label: LabelSourceAPI}, BoardWindow{})
	if err != nil || p.Query.Label != LabelSourceAPI || p.Matched != 1 {
		t.Errorf("the label filter source:api kept %q and matched %d, err %v; want the one API card", p.Query.Label, p.Matched, err)
	}
}
