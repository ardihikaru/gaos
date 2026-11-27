package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// The external-tasks API (D-099, docs/designs/gaos-external-tasks-and-pr-bundles.md §4-§5).
// Tokens are fixtures.

const (
	tokTasks  = "gaos_dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	tokTasks2 = "gaos_eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	tokInbox  = "gaos_ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
)

func testClient(name, tok string, caps []string, products ...string) *apiClient {
	cl := &apiClient{Name: name, Kind: "platform", sum: sumOf(tok), products: map[string]bool{},
		busy: new(atomic.Bool), scope: scopeClient, caps: map[string]bool{}}
	for _, p := range products {
		cl.products[p] = true
	}
	for _, c := range caps {
		cl.caps[c] = true
	}
	return cl
}

// countingPR is a GitHub source that counts its calls.
type countingPR struct {
	pr    ghPR
	err   error
	calls *int
}

func (c countingPR) pullRequest(context.Context, string, string, int) (ghPR, error) {
	*c.calls++
	return c.pr, c.err
}
func (c countingPR) pullFiles(context.Context, string, string, int) ([]ghFile, error) {
	return []ghFile{{Filename: "a.go", Patch: "+x"}}, nil
}

func openPR() ghPR {
	pr := ghPR{Number: 1, Title: "A change", State: "open", AuthorAssociation: "MEMBER"}
	pr.Head.Repo.FullName, pr.Base.Repo.FullName = "loop/laundry-api", "loop/laundry-api"
	return pr
}

// tasksFixture is a server whose p-1 follows loop/laundry-api, with the admin and QA tokens (QA
// reaches p-1), a tasks client "plat" and a second "plat-2" on p-1, and an inbox client on p-2.
func tasksFixture(t *testing.T, pr ghPR) (*server, *MemStore, *int) {
	t.Helper()
	t.Setenv("GAOS_RUNNER", "manual")
	srv, st := newTestServer(t)
	c := newAPIConfig(tokAdmin, tokQA, tokOut, "p-1")
	c.addClients([]*apiClient{
		testClient("plat", tokTasks, []string{CapTasks}, "p-1"),
		testClient("plat-2", tokTasks2, []string{CapTasks}, "p-1"),
		testClient("inb", tokInbox, []string{CapInbox}, "p-2"),
	}, nil, func(string, ...any) {})
	srv.api = c
	calls := 0
	srv.flow = &prFlow{st: st, gh: countingPR{pr: pr, calls: &calls}, ctx: &prContextCache{}, logf: func(string, ...any) {}}
	srv.qaOut = newQaOutbound("http://127.0.0.1:1", "tok-tok-tok-tok-tok-tok-tok-tok-tok-tok", nil) // configured, never reached
	return srv, st, &calls
}

func taskBody(kind, ext, pr string, extra ...string) string {
	b := `{"kind":"` + kind + `","externalId":"` + ext + `"`
	if pr != "" {
		b += `,"pr":"` + pr + `"`
	}
	for i := 0; i+1 < len(extra); i += 2 {
		b += `,"` + extra[i] + `":` + extra[i+1]
	}
	return b + "}"
}

func taskOut(t *testing.T, body string) apiTaskOut {
	t.Helper()
	var o apiTaskOut
	if err := json.Unmarshal([]byte(body), &o); err != nil {
		t.Fatalf("an answer that is not a task: %s", body)
	}
	return o
}

func errCode(body string) string {
	var e struct{ Error string }
	_ = json.Unmarshal([]byte(body), &e)
	return e.Error
}

func TestClientsFileCapabilities(t *testing.T) {
	tok := func(path string) string {
		return map[string]string{"/a": tokTasks, "/b": tokTasks2, "/c": tokInbox}[path]
	}
	var logged []string
	logf := func(f string, a ...any) { logged = append(logged, f) }
	cs := parseClients(strings.NewReader(strings.Join([]string{
		"old platform - /c p-2",
		"tsk platform - /a p-6 tasks",
		"both platform - /b p-6 inbox,tasks",
		"typo platform - /a p-6 task",
	}, "\n")), tok, logf)
	byName := map[string]*apiClient{}
	for _, c := range cs {
		byName[c.Name] = c
	}
	if c := byName["old"]; c == nil || !c.caps[CapInbox] || c.caps[CapTasks] {
		t.Errorf("a line without the sixth field must mean inbox only, got %+v", c)
	}
	if c := byName["tsk"]; c == nil || !c.caps[CapTasks] || c.caps[CapInbox] {
		t.Errorf("a tasks line must hold tasks only, got %+v", c)
	}
	if byName["typo"] != nil {
		t.Error("an unknown capability must skip the line, not be ignored")
	}
	cfg := newAPIConfig(tokAdmin, tokQA, tokOut, "p-6")
	cfg.addClients(cs, nil, func(string, ...any) {})
	kept := map[string]bool{}
	for _, c := range cfg.clients {
		kept[c.Name] = true
	}
	if !kept["tsk"] {
		t.Error("a tasks-only client may share a product with the QA token (it can take no inbox id)")
	}
	if kept["both"] {
		t.Error("a client holding inbox must still be dropped on a product the QA token reaches")
	}
	if q := cfg.qaClient(); q == nil || !q.caps[CapInbox] || q.caps[CapTasks] {
		t.Errorf("the QA tools' client must hold inbox and never tasks, got %+v", q)
	}
	// An open clients file is refused.
	dir := t.TempDir()
	p := filepath.Join(dir, "clients")
	if err := os.WriteFile(p, []byte("x platform - /a p-6 tasks\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GAOS_API_CLIENTS_FILE", p)
	for _, v := range []string{"GAOS_API_ADMIN_TOKEN_FILE", "GAOS_API_QA_TOKEN_FILE", "GAOS_QA_TOKEN_FILE"} {
		t.Setenv(v, "")
	}
	if c := apiFromEnv(); len(c.clients) != 0 {
		t.Errorf("a clients file readable by group or others must be refused, got %d clients", len(c.clients))
	}
}

func TestTaskRoutesCapabilityMatrix(t *testing.T) {
	srv, _, _ := tasksFixture(t, openPR())
	tasks := "/v1/products/p-1/tasks"
	body := taskBody(APIKindPRReview, "x1", "loop/laundry-api#3")
	cases := []struct {
		name, method, path, tok string
		want                    int
	}{
		{"no token", "POST", tasks, "", 401},
		{"an inbox client on a task route", "POST", "/v1/products/p-2/tasks", tokInbox, 403},
		{"the QA token on a task route", "POST", tasks, tokQA, 403},
		{"a tasks client on another product", "POST", "/v1/products/p-2/tasks", tokTasks, 404},
		{"the admin token planning", "POST", tasks, tokAdmin, 403},
		{"a tasks client on an inbox route", "GET", inboxPath("p-1", "nope"), tokTasks, 403},
	}
	for _, c := range cases {
		if got := call(t, srv, c.method, c.path, c.tok, body).Code; got != c.want {
			t.Errorf("%s answered %d, want %d", c.name, got, c.want)
		}
	}
	// The admin token may read; the QA token keeps the inbox.
	if got := call(t, srv, "GET", tasks+"/nope", tokAdmin, "").Code; got != 404 {
		t.Errorf("the admin token reading an unknown task answered %d, want 404 (not refused)", got)
	}
	if got := call(t, srv, "GET", inboxPath("p-1", "nope"), tokQA, "").Code; got == 403 {
		t.Error("the QA token must still reach the inbox routes")
	}
	if got := call(t, srv, "POST", tasks, tokTasks, body).Code; got != 201 {
		t.Errorf("a tasks client on its own product answered %d, want 201", got)
	}
}

func TestPlanAPICardMatchesBrainTown(t *testing.T) {
	ctx := context.Background()
	in := func(kind string, n int) APICardIn {
		return APICardIn{Client: "plat", ExternalID: "e" + kind, Kind: kind, Owner: "loop", Repo: "laundry-api", Number: n, Dispatch: true}
	}
	for _, c := range []struct {
		kind string
		ev   string
		job  string
	}{
		{APIKindPRReview, prTypeOpened, prJobReviewer},
		{APIKindUnitTest, prTypeMerged, prJobUnitTest},
		{APIKindAutoCreate, prTypeOpened, prJobE2EBuild},
	} {
		api, bt := NewMemStore(1), NewMemStore(1)
		card, created, res, err := api.PlanAPICard(ctx, "p-1", in(c.kind, 40), 1000)
		if err != nil || !created {
			t.Fatalf("%s: planned %v, err %v", c.kind, created, err)
		}
		if _, _, _, err := bt.PlanPullRequestCards(ctx, "p-1", prEvent(c.ev, "laundry-api", 40)); err != nil {
			t.Fatal(err)
		}
		bt.mu.RLock()
		want := bt.taskByDedupKeyLocked(prEvent(c.ev, "laundry-api", 40).Key(c.job)).Clone()
		bt.mu.RUnlock()
		if card.DedupKey != want.DedupKey || card.ID != want.ID || card.Role != want.Role ||
			card.Title != want.Title || strings.Join(withoutSource(card.Labels), ",") != strings.Join(want.Labels, ",") {
			t.Errorf("%s: the API card %+v is not Brain Town's %+v", c.kind, card, want)
		}
		// D-102 §22: an API card also says where it came from; Brain Town's does not.
		if !hasLabel(card.Labels, LabelSourceAPI) || hasLabel(want.Labels, LabelSourceAPI) {
			t.Errorf("%s: the API card's labels %v lack source:api, or Brain Town's %v carry it", c.kind, card.Labels, want.Labels)
		}
		for _, e := range res.NewEvents {
			if e.Type == PullRequestOpened || e.Type == PullRequestMerged || e.Type == StagingDeployed {
				t.Errorf("%s: an API card emitted %s, a pull-request event that did not happen", c.kind, e.Type)
			}
		}
		if c.ev == prTypeOpened {
			api.mu.RLock()
			_, ok := api.pullRequests[pullRequestKey("p-1", "loop/laundry-api", 40)]
			api.mu.RUnlock()
			if !ok {
				t.Errorf("%s: an API card must write the Pull requests page row", c.kind)
			}
		}
		if card.APIOwner != "plat" || card.APIAt != 1000 || len(card.APIRefs) != 1 {
			t.Errorf("%s: the card does not record who asked: %+v", c.kind, card)
		}
	}
	st := NewMemStore(1)
	run, _, _, err := st.PlanAPICard(ctx, "p-1", APICardIn{Client: "plat", ExternalID: "r1", Kind: APIKindAutoRun, CaseCode: "*"}, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if run.DedupKey != "qa-run/p-1/plat/ALL/r1:e2e-run" || run.Title != "E2E run of every automated case" ||
		!hasLabel(run.Labels, LabelE2ERun) || !strings.HasPrefix(run.ID, "wf-pr-p-1-qa-run-") {
		t.Errorf("the automation run card is %+v", run)
	}
	if run.AutoDispatch {
		t.Error("a QA-tool card must never be marked for the unattended start")
	}
}

func TestAPICardAdoption(t *testing.T) {
	ctx := context.Background()
	st := NewMemStore(1)
	if _, _, _, err := st.PlanPullRequestCards(ctx, "p-1", prEvent(prTypeOpened, "laundry-api", 12)); err != nil {
		t.Fatal(err)
	}
	req := func(client, ext string) APICardIn {
		return APICardIn{Client: client, ExternalID: ext, Kind: APIKindPRReview, Owner: "loop", Repo: "laundry-api", Number: 12, Manual: true, Dispatch: true}
	}
	card, created, _, err := st.PlanAPICard(ctx, "p-1", req("a", "1"), 1)
	if err != nil || created {
		t.Fatalf("a Brain Town card must be adopted, got created=%v err=%v", created, err)
	}
	if card.APIOwner != "" || card.AutoDispatch || !hasLabel(card.Labels, LabelPRReviewAuto) {
		t.Errorf("adoption must change no owner, no dispatch flag and no label: %+v", card)
	}
	if apiDispatchAnswer(card) != "operator" {
		t.Errorf("the dispatch answer must come from the card as it stands, got %q", apiDispatchAnswer(card))
	}
	if _, err := st.APICardGet(ctx, "p-1", card.ID, "", "a"); err != nil {
		t.Errorf("an adopter must be able to read the card: %v", err)
	}
	again, _, _, _ := st.PlanAPICard(ctx, "p-1", req("a", "2"), 1)
	if len(again.APIRefs) != 1 {
		t.Errorf("one ref per client, got %v", again.APIRefs)
	}
	for i := 0; i < 7; i++ {
		if _, _, _, err := st.PlanAPICard(ctx, "p-1", req("c"+string(rune('0'+i)), "x"), 1); err != nil {
			t.Fatalf("adoption %d refused: %v", i, err)
		}
	}
	if _, _, _, err := st.PlanAPICard(ctx, "p-1", req("ninth", "x"), 1); errCodeOf(err) != "too-many-refs" {
		t.Errorf("the ninth client must be refused too-many-refs, got %v", err)
	}
	// An API-created card keeps its owner's ref at the head.
	own, _, _, err := st.PlanAPICard(ctx, "p-1", APICardIn{Client: "owner", ExternalID: "o", Kind: APIKindAutoCreate, Owner: "loop", Repo: "laundry-api", Number: 13}, 1)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 9; i++ {
		_, _, _, _ = st.PlanAPICard(ctx, "p-1", APICardIn{Client: "o" + string(rune('0'+i)), ExternalID: "o", Kind: APIKindAutoCreate, Owner: "loop", Repo: "laundry-api", Number: 13}, 1)
	}
	got, _ := st.APICardGet(ctx, "p-1", own.ID, "", "owner")
	if len(got.APIRefs) == 0 || got.APIRefs[0] != "owner:o" || got.APIOwner != "owner" {
		t.Errorf("the owner's ref was evicted or moved: %+v", got)
	}
}

func errCodeOf(err error) string {
	if ce, ok := err.(*apiCardErr); ok {
		return ce.Code
	}
	return ""
}

func TestAPICardIdempotency(t *testing.T) {
	srv, st, calls := tasksFixture(t, openPR())
	path := "/v1/products/p-1/tasks"
	body := taskBody(APIKindPRReview, "same", "loop/laundry-api#5")
	if rec := call(t, srv, "POST", path, tokTasks, body); rec.Code != 201 {
		t.Fatalf("the first request answered %d %s", rec.Code, rec.Body)
	}
	before := *calls
	rec := call(t, srv, "POST", path, tokTasks, body)
	if rec.Code != 200 || taskOut(t, rec.Body.String()).Created {
		t.Errorf("the same request again answered %d %s, want 200 created:false", rec.Code, rec.Body)
	}
	if *calls != before {
		t.Errorf("a replay called GitHub %d more time(s); the lookup comes first", *calls-before)
	}
	other := taskBody(APIKindPRReview, "same", "loop/laundry-api#6")
	if rec := call(t, srv, "POST", path, tokTasks, other); rec.Code != 409 || errCode(rec.Body.String()) != "externalId-reused" {
		t.Errorf("the same externalId for another request answered %d %s, want 409 externalId-reused", rec.Code, rec.Body)
	}
	// A natural key planned between the lookup and the plan is adopted under the lock.
	ctx := context.Background()
	in := APICardIn{Client: "plat", ExternalID: "late", Kind: APIKindPRReview, Owner: "loop", Repo: "laundry-api", Number: 7}
	if _, kind, _ := st.APICardLookup(ctx, "p-1", in); kind != APIMiss {
		t.Fatalf("the fixture already has #7")
	}
	if _, _, _, err := st.PlanPullRequestCards(ctx, "p-1", prEvent(prTypeOpened, "laundry-api", 7)); err != nil {
		t.Fatal(err)
	}
	card, created, _, err := st.PlanAPICard(ctx, "p-1", in, 1)
	if err != nil || created || !hasRefOf(&card, "plat") {
		t.Errorf("a card planned during prepare must be adopted, got created=%v err=%v refs=%v", created, err, card.APIRefs)
	}
}

func TestAPICardRefusals(t *testing.T) {
	path := "/v1/products/p-1/tasks"
	check := func(srv *server, name, body string, status int, code string) {
		t.Helper()
		rec := call(t, srv, "POST", path, tokTasks, body)
		if rec.Code != status || (code != "" && errCode(rec.Body.String()) != code) {
			t.Errorf("%s answered %d %s, want %d %s", name, rec.Code, rec.Body, status, code)
		}
	}
	srv, _, _ := tasksFixture(t, openPR())
	check(srv, "an unknown kind", taskBody("deploy", "a", "loop/laundry-api#1"), 422, "invalid-kind")
	check(srv, "a bad externalId", taskBody(APIKindPRReview, "a:b", "loop/laundry-api#1"), 422, "invalid-field")
	check(srv, "a bad pr", taskBody(APIKindPRReview, "a", "laundry-api"), 422, "invalid-field")
	check(srv, "a bad caseCode", taskBody(APIKindAutoRun, "a", "", "caseCode", `"tc 1"`), 422, "invalid-field")
	check(srv, "a run with a pr", taskBody(APIKindAutoRun, "a", "loop/laundry-api#1", "caseCode", `"*"`), 422, "invalid-field")
	check(srv, "a repository the product does not follow", taskBody(APIKindPRReview, "a", "other/repo#1"), 422, "repo-not-owned")
	check(srv, "a title field", `{"kind":"pr-review","externalId":"a","pr":"loop/laundry-api#1","title":"x"}`, 400, "")
	draft := openPR()
	draft.Draft = true
	srv, _, _ = tasksFixture(t, draft)
	check(srv, "a draft", taskBody(APIKindPRReview, "d", "loop/laundry-api#1"), 409, "skipped")
	fork := openPR()
	fork.Head.Repo.FullName = "stranger/laundry-api"
	srv, _, _ = tasksFixture(t, fork)
	check(srv, "a fork", taskBody(APIKindAutoCreate, "f", "loop/laundry-api#1"), 409, "skipped")
	stranger := openPR()
	stranger.AuthorAssociation = "NONE"
	srv, _, _ = tasksFixture(t, stranger)
	check(srv, "a stranger", taskBody(APIKindPRReview, "s", "loop/laundry-api#1"), 409, "skipped")
	merged := openPR()
	merged.Merged, merged.State = true, "closed"
	srv, _, _ = tasksFixture(t, merged)
	check(srv, "a review of a merged pull request", taskBody(APIKindPRReview, "m", "loop/laundry-api#1"), 409, "skipped")
	srv, _, _ = tasksFixture(t, openPR())
	check(srv, "unit tests of an unmerged pull request", taskBody(APIKindUnitTest, "u", "loop/laundry-api#1"), 409, "skipped")
	srv.flow.gh = nil
	for _, k := range []string{APIKindPRReview, APIKindUnitTest, APIKindAutoCreate} {
		check(srv, k+" without GitHub", taskBody(k, "g"+k, "loop/laundry-api#2"), 503, "github-off")
	}
	srv, _, _ = tasksFixture(t, openPR())
	srv.qaOut = nil
	check(srv, "an automation without the QA tool", taskBody(APIKindAutoCreate, "q1", "loop/laundry-api#3"), 503, "qa-off")
	check(srv, "a run without the QA tool", taskBody(APIKindAutoRun, "q2", "", "caseCode", `"*"`), 503, "qa-off")
	t.Setenv("GAOS_RUNNER", "off")
	check(srv, "a run under GAOS_RUNNER=off", taskBody(APIKindAutoRun, "o", "", "caseCode", `"*"`), 409, "runner-off")
}

func TestAPICapsKeepThePRFlow(t *testing.T) {
	ctx := context.Background()
	t.Setenv("GAOS_API_MAX_OPEN", "2")
	st := NewMemStore(1)
	pr := func(client string, n int) APICardIn {
		return APICardIn{Client: client, ExternalID: "e" + string(rune('a'+n)), Kind: APIKindPRReview, Owner: "loop", Repo: "laundry-api", Number: 100 + n}
	}
	for n := 0; n < 2; n++ {
		if _, _, _, err := st.PlanAPICard(ctx, "p-1", pr("plat", n), 1); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, _, err := st.PlanAPICard(ctx, "p-1", pr("plat", 2), 1); errCodeOf(err) != "cap" {
		t.Errorf("a client at GAOS_API_MAX_OPEN must be refused cap, got %v", err)
	}
	// The PR cap does not count the API's cards: Brain Town still plans.
	t.Setenv("GAOS_PR_MAX_OPEN", "1")
	if planned, _, _, err := st.PlanPullRequestCards(ctx, "p-1", prEvent(prTypeOpened, "laundry-api", 300)); err != nil || len(planned) == 0 {
		t.Errorf("a client at its cap made Brain Town skip: planned %v, err %v", planned, err)
	}
	// The daily cap counts canceled cards; one run per product.
	t.Setenv("GAOS_API_DAILY", "2")
	t.Setenv("GAOS_API_MAX_OPEN", "9")
	run := func(ext string) APICardIn {
		return APICardIn{Client: "ops", ExternalID: ext, Kind: APIKindAutoRun, CaseCode: "TC-A-1"}
	}
	r1, _, _, err := st.PlanAPICard(ctx, "p-1", run("r1"), 5000)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := st.PlanAPICard(ctx, "p-1", run("r2"), 5000); errCodeOf(err) != "run-in-flight" {
		t.Errorf("a second open run must be refused run-in-flight, got %v", err)
	}
	if _, err := st.APICardCancel(ctx, "p-1", r1.ID, "ops"); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := st.PlanAPICard(ctx, "p-1", run("r3"), 5000); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := st.PlanAPICard(ctx, "p-1", run("r4"), 5000); errCodeOf(err) != "daily-cap" {
		t.Errorf("a canceled card must count toward GAOS_API_DAILY, got %v", err)
	}
}

func TestAPIGetAndCancelScope(t *testing.T) {
	srv, st, _ := tasksFixture(t, openPR())
	path := "/v1/products/p-1/tasks"
	rec := call(t, srv, "POST", path, tokTasks, taskBody(APIKindAutoRun, "run1", "", "caseCode", `"TC-OUTREG-004"`))
	if rec.Code != 201 {
		t.Fatalf("the run answered %d %s", rec.Code, rec.Body)
	}
	id := taskOut(t, rec.Body.String()).TaskID
	if got := call(t, srv, "GET", path+"/"+id, tokTasks2, "").Code; got != 404 {
		t.Errorf("another client reading the card answered %d, want 404", got)
	}
	if got := call(t, srv, "POST", path+"/"+id+"/cancel", tokTasks2, "").Code; got != 404 {
		t.Errorf("plat-2 canceling plat's card answered %d, want 404 (the name is compared whole)", got)
	}
	if got := call(t, srv, "GET", path+"?externalId=run1", tokTasks, "").Code; got != 200 {
		t.Errorf("the owner reading by externalId answered %d", got)
	}
	ctx := context.Background()
	st.mu.Lock()
	c := st.taskByIDLocked(id)
	c.BlockReason = RuleSecretHeld
	st.mu.Unlock()
	o := taskOut(t, call(t, srv, "GET", path+"/"+id, tokTasks, "").Body.String())
	if o.Reason == nil || *o.Reason != "held" {
		t.Errorf("a secret hold must read held, got %v", o.Reason)
	}
	if r := apiPublicReason(TaskRecord{Status: StatusFailed, BlockReason: "qa-job:123 boom"}); r != "failed" {
		t.Errorf("a QA job id must not leave GAOS, got %q", r)
	}
	st.mu.Lock()
	st.taskByIDLocked(id).BlockReason = ""
	st.mu.Unlock()
	if rec := call(t, srv, "POST", path+"/"+id+"/cancel", tokTasks, ""); rec.Code != 200 || taskOut(t, rec.Body.String()).Status != StatusCanceled {
		t.Errorf("the owner canceling its run answered %d %s", rec.Code, rec.Body)
	}
	rec = call(t, srv, "POST", path, tokTasks, taskBody(APIKindPRReview, "rv", "loop/laundry-api#9"))
	rv := taskOut(t, rec.Body.String()).TaskID
	if rec := call(t, srv, "POST", path+"/"+rv+"/cancel", tokTasks, ""); rec.Code != 409 || errCode(rec.Body.String()) != "not-cancelable" {
		t.Errorf("canceling a natural-key card answered %d %s, want 409 not-cancelable", rec.Code, rec.Body)
	}
	_ = ctx
}

func TestAPIDispatchAnswer(t *testing.T) {
	model := TaskRecord{DeskID: "p-1", Labels: []string{LabelPRReviewAuto}, AutoDispatch: true}
	cases := []struct {
		runner, autorun string
		card            TaskRecord
		want            string
	}{
		{"on", "", model, "scheduler"},
		{"manual", "p-1", model, "auto"},
		{"manual", "", model, "operator"},
		{"manual", "p-1", TaskRecord{DeskID: "p-1", Labels: []string{LabelPRReviewAuto}}, "operator"},
		{"on", "p-1", TaskRecord{DeskID: "p-1", Labels: []string{LabelPRReviewManual}}, "manual-review"},
		{"manual", "p-1", TaskRecord{DeskID: "p-1", Labels: []string{LabelE2ERun}, AutoDispatch: true}, "qa-tool"},
		{"on", "", TaskRecord{DeskID: "p-1", Labels: []string{LabelE2EAutomation}}, "qa-tool"},
	}
	for _, c := range cases {
		t.Setenv("GAOS_RUNNER", c.runner)
		t.Setenv("GAOS_API_AUTORUN_PRODUCTS", c.autorun)
		if got := apiDispatchAnswer(c.card); got != c.want {
			t.Errorf("runner %s, autorun %q, labels %v, autoDispatch %v: dispatch is %q, want %q",
				c.runner, c.autorun, c.card.Labels, c.card.AutoDispatch, got, c.want)
		}
	}
}

func TestAPIAutoDispatchClaim(t *testing.T) {
	ctx := context.Background()
	st := NewMemStore(1)
	card, _, _, err := st.PlanAPICard(ctx, "p-1", APICardIn{Client: "plat", ExternalID: "a", Kind: APIKindPRReview,
		Owner: "loop", Repo: "laundry-api", Number: 50, Dispatch: true}, 1)
	if err != nil {
		t.Fatal(err)
	}
	claim := func() error {
		st.mu.Lock()
		defer st.mu.Unlock()
		return st.apiClaimLocked("p-1", st.taskByIDLocked(card.ID), false)
	}
	t.Setenv("GAOS_RUNNER", "manual")
	t.Setenv("GAOS_API_AUTORUN_PRODUCTS", "")
	if claim() == nil {
		t.Error("a product not listed in GAOS_API_AUTORUN_PRODUCTS must wait for an operator")
	}
	t.Setenv("GAOS_API_AUTORUN_PRODUCTS", "p-1")
	if err := claim(); err != nil {
		t.Errorf("a listed product's autoDispatch card must be claimed, got %v", err)
	}
	t.Setenv("GAOS_RUNNER", "on")
	if claim() == nil || apiAutoPass(ctx, st) != 0 {
		t.Error("under GAOS_RUNNER=on the unattended pass must do nothing: the scheduler dispatches")
	}
	t.Setenv("GAOS_RUNNER", "manual")
	// The daily cap counts autoDispatch cards only, not defect cards.
	t.Setenv("GAOS_API_AUTORUN_DAILY", "1")
	st.mu.Lock()
	st.tasks = append(st.tasks, TaskRecord{ID: "defect-x", DeskID: "p-1", AutoAt: autoFixNow()})
	st.mu.Unlock()
	if err := claim(); err != nil {
		t.Errorf("a defect card's stamp must not count toward GAOS_API_AUTORUN_DAILY, got %v", err)
	}
	st.mu.Lock()
	st.tasks = append(st.tasks, TaskRecord{ID: "api-x", DeskID: "p-1", AutoDispatch: true, AutoAt: autoFixNow()})
	st.mu.Unlock()
	if claim() == nil {
		t.Error("a second unattended start in one UTC day must be refused at GAOS_API_AUTORUN_DAILY=1")
	}
	t.Setenv("GAOS_API_AUTORUN_DAILY", "9")
	if _, err := st.SetTaskLabel(ctx, "p-1", card.ID, LabelPRReviewManual, "ops"); err != nil {
		t.Fatal(err)
	}
	if claim() == nil {
		t.Error("a card an operator relabelled manual must not start unattended")
	}
	if _, err := st.SetTaskLabel(ctx, "p-1", card.ID, LabelPRReviewAuto, "ops"); err != nil {
		t.Fatal(err)
	}
	st.mu.Lock()
	st.taskByIDLocked(card.ID).BlockReason = BlockEgress
	st.mu.Unlock()
	if claim() == nil {
		t.Error("a card the egress pass refused must not be retried by the pass")
	}
	st.mu.Lock()
	st.taskByIDLocked(card.ID).BlockReason = ""
	st.mu.Unlock()
	// The one dispatch function: with no adapter it refuses with dispatch's own reason.
	walkTo(t, st, card.ID, StatusReady)
	if _, err := st.APIAutoDispatch(ctx, "p-1", card.ID); err == nil || !strings.Contains(err.Error(), "no model adapter") {
		t.Errorf("APIAutoDispatch must go through dispatch's gates, got %v", err)
	}
}

func TestQARunKeys(t *testing.T) {
	r := &qaJobRunner{}
	ctx := context.Background()
	if got := r.caseCodeFor(ctx, TaskRecord{DedupKey: "qa-run/p-1/plat/TC-OUTREG-004/x:e2e-run"}, "run"); got != "TC-OUTREG-004" {
		t.Errorf("a specific run must run its code, not every case, got %q", got)
	}
	if got := r.caseCodeFor(ctx, TaskRecord{DedupKey: "qa-run/p-1/plat/ALL/x:e2e-run"}, "run"); got != "*" {
		t.Errorf("ALL must mean every automated case, got %q", got)
	}
	for _, k := range []string{"qa-run/p-1/plat/A-1/x:e2e-run", "bundle/p-1/pr:loop/laundry-api#4:test-planner"} {
		if _, _, _, ok := prKeyParts(k); ok {
			t.Errorf("prKeyParts read %q as a pull request", k)
		}
	}
	if (&notifier{}).loginFor(ctx, TaskRecord{DedupKey: "qa-run/p-1/plat/A-1/x:e2e-run"}, "e2e:failure") != "" {
		t.Error("a run's notice must go to the group")
	}
}

func TestTestPlannerRoleIsSeated(t *testing.T) {
	st := NewMemStore(1)
	st.mu.Lock()
	m := st.masterPtr("p-1")
	idx := st.deskIdxByID("p-1")
	planned, _, _ := st.planCardsLocked(m, st.desks[idx], []cardItem{{Key: "bundle/p-1/pr:loop/laundry-api#1:test-planner",
		ID: "wf-pr-p-1-bundle-0000", Title: "Test plan", Spec: prCardSpec{Job: "test-planner", Role: "test-planner"}}}, "1 task(s) planned from bundle")
	st.mu.Unlock()
	if len(planned) != 1 {
		t.Fatal("the test-planner card was not planned")
	}
	if _, ok := roles["test-planner"]; !ok {
		t.Fatal("the test-planner role is missing from roles")
	}
	d := Schedule(st.schedulerInput(1000))
	for _, r := range d.Ready {
		if r.TaskID == planned[0] {
			return
		}
	}
	t.Errorf("a test-planner card was not readied: no agent holds the role? decision %+v", d)
}

func TestArcadeTaskAPIFieldsRoundTrip(t *testing.T) {
	g := &fakeGraph{}
	s, _, _ := arcadeOver(t, g.handler)
	ctx := context.Background()
	g.take()
	if _, _, _, err := s.PlanAPICard(ctx, "p-1", APICardIn{Client: "plat", ExternalID: "a", Kind: APIKindPRReview,
		Owner: "loop", Repo: "laundry-api", Number: 60, Dispatch: true}, 1234); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(g.take(), "\n")
	for _, want := range []string{"apiOwner", "apiRefs", "apiAt", "autoDispatch"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the Task upsert does not carry %q", want)
		}
	}
}

// withoutSource drops the source:* label, for comparing an API card with Brain Town's.
func withoutSource(labels []string) []string {
	var out []string
	for _, l := range labels {
		if !strings.HasPrefix(l, labelFamilySource+":") {
			out = append(out, l)
		}
	}
	return out
}
