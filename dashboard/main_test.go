package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// The tests pin the things that fail SILENTLY.
//
// A dashboard has almost no assertions of its own: a wrong template renders a
// page that looks fine, a dropped style attribute renders a page that looks
// fine in one theme, and a mount-point bug renders a page that looks fine until
// it is behind a proxy. Each of those has a test here, and the reason is
// written above it.

func newTestServer(t *testing.T) (*server, *MemStore) {
	t.Helper()
	rend, err := NewRenderer(false, ".")
	if err != nil {
		t.Fatalf("templates: %v", err)
	}
	// A pinned seed. Every branch in sim.go is a probability; a test that
	// cannot reproduce a sequence cannot assert on one.
	st := NewMemStore(1)
	return &server{store: st, rend: rend, arc: &Arcade{DB: "gaos"}, auth: newTestAuthority()}, st
}

// The test authority: fixed key, one account, the minimum iteration count so
// the suite stays fast. newTestServer builds a FRESH authority per test from
// these constants, so throttle state never leaks between tests, and boCookies()
// signs with the same constants.
const (
	testEmail    = "ardi@example.test"
	testPassword = "correct horse battery"
	testLabel    = "gaos-test"
)

var testKey = []byte("0123456789abcdef0123456789abcdef-test-key")

var testHashLine = sync.OnceValue(func() string {
	h, err := hashWith(testPassword, []byte("fixed-test-salt!"), pbkdf2MinIter)
	if err != nil {
		panic(err)
	}
	return h
})

func newTestAuthority() *authority {
	accts := parseUsers(strings.NewReader(testEmail+":"+testHashLine()+"\n"), func(string, ...any) {})
	return newAuthority(accts, testKey, testLabel)
}

func do(t *testing.T, s *server, method, path string, form url.Values, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
	}
	r := httptest.NewRequest(method, path, body)
	if form != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	r.Header.Set("HX-Request", "true")
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, r)
	return w
}

func boCookies() []*http.Cookie {
	a := newTestAuthority()
	return []*http.Cookie{{Name: boCookie, Value: a.sign(testEmail, time.Now().Add(time.Hour))}}
}

// ---------- routes ----------

func TestEveryRouteAnswers(t *testing.T) {
	s, _ := newTestServer(t)
	cs := boCookies()

	cases := []struct {
		method, path string
		want         int
	}{
		{"GET", "/", 200},
		{"GET", "/healthz", 200},
		{"GET", "/static/app.css", 200},
		{"GET", "/events", 200},
		// Seq 1 is FloorOpened, which every store has from boot. Not 0: the
		// cursor is an event Seq now and Seq starts at 1.
		{"POST", "/replay/floor?t=1", 200},
		{"POST", "/replay/floor?t=-1", 200}, // back to live, or every mutation below is refused (BR-20)
		// Desk 0 is running in the fixtures, so this is the "already in
		// flight" rejection rather than a dispatch: a 409 with the reason,
		// since the desk routes answer JSON (M14.7). Asserting the route
		// ANSWERS is what this table is for -- the dispatch path's own
		// behaviour is dispatch_test.go's job.
		{"POST", "/tasks/0/dispatch", 409},
		{"POST", "/tasks/0/retry", 200},
		{"POST", "/tasks/0/cancel", 200},
		{"GET", "/tasks/1/context", 200},
		{"GET", "/ui/bo", 200},
		{"GET", "/ui/bo/agents", 200},
		{"GET", "/ui/bo/stories", 200},
		{"GET", "/ui/bo/diagrams", 200},
		{"GET", "/ui/bo/knowledge", 200},
		{"GET", "/products", 200},
		{"GET", "/products/p-1", 200},
		{"GET", "/products/p-1?tab=stories", 200},
		{"GET", "/products/p-1?tab=diagrams", 200},
		{"GET", "/products/p-1?tab=knowledge", 200},
		{"GET", "/products/p-1?tab=board", 200},
		{"GET", "/products/p-1?tab=board&view=table&sort=id", 200},
		{"GET", "/products/p-1/board/live", 200},    // D-095: the Board's own route
		{"GET", "/products/p-1/board?paged=1", 200}, // and the client board's document
		{"GET", "/products/nope/board/live", 404},
		{"GET", "/products/p-1?tab=blueprint", 200},
		{"GET", "/products/p-1?tab=timeline", 200},
		{"GET", "/products/p-1/readiness", 200},
		{"POST", "/products/p-1/inject", 200},
		{"GET", "/ui/cw", 200},
		// D-094: the page routes -- nine views, a product section by slug, and
		// the two redirects (the id form, and a product with no section).
		{"GET", "/inbox", 200},
		{"GET", "/stories", 200},
		{"GET", "/diagrams", 200},
		{"GET", "/knowledge", 200},
		{"GET", "/agents", 200},
		{"GET", "/archive", 200},
		{"GET", "/integrations", 200},
		{"GET", "/autofix", 200},
		{"GET", "/prs", 200},
		{"GET", "/p/laundry-membership/board", 200},
		{"GET", "/p/p-1/board", 302},
		{"GET", "/p/laundry-membership", 302},
		{"GET", "/p/nope/board", 404},
		{"GET", "/products/nope", 404},

		// The floor UI, retired in M14.7 (T-167). A route that still answered
		// would be a floor nobody maintains, writing events nobody asked for.
		{"GET", "/ui/floor", 404},
		{"GET", "/ui/inspector", 404},
		{"GET", "/ui/timeline", 404},
		{"POST", "/ui/pause", 404},
		{"POST", "/ui/desk/1/review", 404},
	}
	for _, c := range cases {
		w := do(t, s, c.method, c.path, nil, cs...)
		if w.Code != c.want {
			t.Errorf("%s %s = %d, want %d\nbody: %s", c.method, c.path, w.Code, c.want, trunc(w.Body.String()))
		}
	}
}

// A template that fails to execute must not leave a 200 and half a page on the
// wire, and no response may contain the literal ZgotmplZ -- see below.
func TestNoTemplateErrorEscapesAsSuccess(t *testing.T) {
	s, _ := newTestServer(t)
	for _, p := range []string{"/", "/products/p-1?tab=timeline", "/products/p-1?tab=board"} {
		w := do(t, s, "GET", p, nil)
		if strings.Contains(w.Body.String(), "template error") {
			t.Errorf("%s rendered a template error", p)
		}
	}
}

// THE STYLE-ATTRIBUTE TRAP. html/template refuses to interpolate a plain string
// into a style="" context and substitutes the literal text ZgotmplZ. The page
// still renders, still validates, and has silently lost every colour, width and
// role tint. This is the single most likely regression in this codebase, so it
// is checked on every surface that carries a computed style.
func TestNoZgotmplZ(t *testing.T) {
	s, _ := newTestServer(t)
	cs := boCookies()
	for _, p := range []string{
		"/", "/ui/bo/agents", "/products/p-1", "/products/p-1?tab=timeline", "/ui/cw",
	} {
		w := do(t, s, "GET", p, nil, cs...)
		if strings.Contains(w.Body.String(), "ZgotmplZ") {
			t.Errorf("%s emitted ZgotmplZ -- a computed style lost its template.CSS type", p)
		}
	}
}

// ---------- mount point ----------

// If X-Forwarded-Prefix stops arriving, or arrives forged, the <base href> is
// wrong and every relative asset resolves against whatever owns the site root.
// The failure is silent and browser-side; a status-code check will not see it.
func TestBaseHrefFollowsForwardedPrefix(t *testing.T) {
	s, _ := newTestServer(t)

	cases := []struct{ header, want string }{
		{"", `<base href="/">`},
		{"/app", `<base href="/app/">`},
		{"/app/", `<base href="/app/">`},

		// Every one of these must be REFUSED, not sanitised into something
		// plausible. The header is attacker-controlled on an unproxied request
		// and its value lands in browser-visible output.
		{"https://evil.example", `<base href="/">`},
		{"//evil.example", `<base href="/">`},
		{"app", `<base href="/">`},
		{"/a\\b", `<base href="/">`},
		{"/a\"b", `<base href="/">`},
		{"/a\nb", `<base href="/">`},
		{"/" + strings.Repeat("x", 200), `<base href="/">`},
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "/", nil)
		if c.header != "" {
			r.Header.Set("X-Forwarded-Prefix", c.header)
		}
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, r)
		if !strings.Contains(w.Body.String(), c.want) {
			t.Errorf("prefix %q: want %s", c.header, c.want)
		}
		// D-094: a page three levels down carries the same base, so every
		// relative URL on it still resolves against the mount point. Signed
		// out it is the 401 sign-in page, which is a page all the same.
		r = httptest.NewRequest("GET", "/p/laundry-membership/board", nil)
		if c.header != "" {
			r.Header.Set("X-Forwarded-Prefix", c.header)
		}
		w = httptest.NewRecorder()
		s.routes().ServeHTTP(w, r)
		if !strings.Contains(w.Body.String(), c.want) {
			t.Errorf("prefix %q on a deep URL: want %s", c.header, c.want)
		}
	}
}

// ---------- health ----------

func TestHealthUnconfiguredIsHealthy(t *testing.T) {
	s, _ := newTestServer(t)
	w := do(t, s, "GET", "/healthz", nil)
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("healthz is not JSON: %v", err)
	}
	if body["ok"] != true {
		t.Errorf("an unconfigured database must stay a 200: that is the local stack")
	}
	if body["store"] != "memory" {
		t.Errorf("store = %v", body["store"])
	}
}

// A configured-but-unreachable database is the failure the deploy check exists
// to catch: wrong network name, wrong alias, wrong password. It must be a 503.
func TestHealthUnreachableDatabaseIs503(t *testing.T) {
	s, _ := newTestServer(t)
	s.arc = &Arcade{URL: "http://127.0.0.1:1", DB: "gaos", client: http.DefaultClient}
	w := do(t, s, "GET", "/healthz", nil)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["database"] != "unreachable" {
		t.Errorf("database = %v, want unreachable", body["database"])
	}
}

// ---------- auth boundary ----------

// Every back-office route must refuse an unauthenticated caller. A route that
// quietly never had a check is the thing this test exists to find, so it walks
// the list rather than sampling it.
func TestBackOfficeRoutesRequireASession(t *testing.T) {
	s, _ := newTestServer(t)
	guarded := [][2]string{
		{"GET", "/ui/bo/agents"},
		{"GET", "/products"},
		{"GET", "/products/p-1"},
		{"POST", "/products"},
		{"PUT", "/products/p-1/brd"},
		{"POST", "/products/p-1/stories"},
		{"POST", "/products/p-1/diagrams"},
		{"POST", "/products/p-1/knowledge"},
		{"POST", "/products/p-1/inject"},
	}
	for _, g := range guarded {
		w := do(t, s, g[0], g[1], url.Values{})
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s = %d, want 401", g[0], g[1], w.Code)
		}
	}
}

func TestLoginRejectsANonEmail(t *testing.T) {
	s, _ := newTestServer(t)
	w := do(t, s, "POST", "/auth/login", url.Values{"email": {"nope"}})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Enter the email") {
		t.Error("the error message did not reach the form")
	}
}

// The tenant label contains a middle dot. net/http DROPS invalid bytes from a
// cookie value silently, so an unescaped value is a truncated header and a
// warning in the log, not an error.
func TestTenantCookieSurvivesNonASCII(t *testing.T) {
	s, _ := newTestServer(t)
	w := do(t, s, "POST", "/auth/login", url.Values{"email": {testEmail}, "password": {testPassword}, "tenant": {"loop-eng"}})
	var raw string
	for _, c := range w.Result().Cookies() {
		if c.Name == tenantCookie {
			raw = c.Value
		}
	}
	if raw == "" {
		t.Fatal("no tenant cookie was set")
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(&http.Cookie{Name: tenantCookie, Value: raw})
	if got := tenantOf(r); got != "loop-eng" {
		t.Errorf("round-trip = %q", got)
	}
}

// ---------- the store ----------

func TestApproveRequiresAGate(t *testing.T) {
	s, _ := newTestServer(t)
	ctx := context.Background()
	if _, err := s.store.ApproveTask(ctx, 0); err == nil {
		t.Fatal("approving a desk with no gate must be rejected")
	}
}

// A projection is what ArcadeStore.load() builds when it rebuilds the floor
// from the graph: a MemStore with no fixtures in it. It is NOT a MemStore with
// no fields, and the difference used to be a segfault.
//
// load() wrote its own struct literal and left rng nil, because nothing in the
// load path reads it. advance() does, through pickTask, and advance() is
// reachable from ApproveTask -- so restarting a dashboard against a populated
// database and releasing one merge gate killed the process. The conformance
// suite missed it because every store it builds goes through NewMemStore, which
// sets rng.
//
// This test builds the store the way load() does and drives the path that
// panicked. It is the reason newProjection exists.
func TestTheProjectionSurvivesAnApprovalWithNoFixturesBehindIt(t *testing.T) {
	s := newProjection()
	s.desks = []Desk{{
		ID: "wf-1", Name: "Rebuilt", WorkflowID: "wf-1",
		State: StateActive, StageIdx: 0, ApprovalRequired: true,
	}}

	res, err := s.ApproveTask(context.Background(), 0)
	if err != nil {
		t.Fatalf("approving a gate on a rebuilt floor: %v", err)
	}
	if len(res.NewEvents) == 0 {
		t.Fatal("releasing a merge gate must emit at least ApprovalGranted")
	}
	if s.desks[0].Task == "" {
		t.Fatal("advance must have seated a task; a nil rng used to panic here instead")
	}
}

// Architecture §06: max_retry_count is 3 per task. The guardrail belongs in the
// store, not the UI, because the UI is not the only caller it will ever have.
func TestRetryGuardExhausts(t *testing.T) {
	s, _ := newTestServer(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := s.store.RetryTask(ctx, 0); err != nil {
			t.Fatalf("retry %d: %v", i+1, err)
		}
	}
	_, err := s.store.RetryTask(ctx, 0)
	if err == nil {
		t.Fatal("the fourth retry must be refused")
	}
	if !strings.Contains(err.Error(), "guard") {
		t.Errorf("the refusal must say why: %v", err)
	}
}

func TestCancelReleasesTheDesk(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()
	if _, err := s.store.CancelTask(ctx, 0); err != nil {
		t.Fatal(err)
	}
	d, _, _ := st.Desk(ctx, 0)
	if d.State != StateIdle || len(d.Agents) != 0 {
		t.Errorf("state=%s agents=%v, want idle with no agents", d.State, d.Agents)
	}
}

// A snapshot that shares its slices with the live floor is a replay that keeps
// changing under the operator. Clone is the whole defence, so it is tested
// directly rather than through a handler.
func TestSnapshotsDoNotAliasTheLiveFloor(t *testing.T) {
	_, st := newTestServer(t)
	ctx := context.Background()

	f, _ := st.Snapshot(ctx)
	before := len(f.Desks[0].Logs)
	f.Desks[0].Logs = append(f.Desks[0].Logs, LogLine{Line: "mutated by the caller"})
	f.Desks[0].Agents = append(f.Desks[0].Agents, "a-9")

	again, _ := st.Snapshot(ctx)
	if len(again.Desks[0].Logs) != before {
		t.Error("mutating a snapshot changed the store")
	}
}

// The stream is the record, not a window. This test used to assert the opposite
// -- that events were capped at 160 and the replay cursor was shifted when the
// window moved -- which is what BR-04 says a stream must not be, and what T-013
// removed. The genesis event surviving a long run is the whole assertion: it is
// the one event that can never be re-derived, so if anything is ever dropped
// from the front again, this is where it shows up.
func TestTheEventStreamIsUnboundedAndKeepsGenesis(t *testing.T) {
	_, st := newTestServer(t)
	ctx := context.Background()

	before, _ := st.Events(ctx)
	for i := 0; i < 400; i++ {
		st.Tick()
	}
	evs, _ := st.Events(ctx)

	if len(evs) <= len(before) {
		t.Fatalf("400 ticks produced no events: %d then %d", len(before), len(evs))
	}
	if evs[0].Type != FloorOpened {
		t.Fatalf("oldest event is %s, want FloorOpened -- the front of the stream was dropped", evs[0].Type)
	}
	if evs[0].Seq != 1 {
		t.Fatalf("oldest event has seq %d, want 1", evs[0].Seq)
	}
	for i := 1; i < len(evs); i++ {
		if evs[i].Seq != evs[i-1].Seq+1 {
			t.Fatalf("gap in the stream: seq %d follows %d", evs[i].Seq, evs[i-1].Seq)
		}
	}

	// Genesis is still reachable, which is the point of not dropping it.
	if _, err := st.Replay(ctx, evs[0].Seq); err != nil {
		t.Fatalf("replay to genesis: %v", err)
	}
	if _, err := st.Replay(ctx, evs[len(evs)-1].Seq+50); err == nil {
		t.Error("replaying past the end must be a not-found, not a silent clamp")
	}
}

// Replay is a read-only view. A ticking simulation must not rewrite the past
// the operator is looking at.
func TestReplayFreezesTheFloor(t *testing.T) {
	_, st := newTestServer(t)
	ctx := context.Background()
	for i := 0; i < 20; i++ {
		st.Tick()
	}
	if _, err := st.Replay(ctx, 2); err != nil {
		t.Fatal(err)
	}
	before, _ := st.Snapshot(ctx)
	for i := 0; i < 20; i++ {
		st.Tick()
	}
	after, _ := st.Snapshot(ctx)
	if before.Desks[0].Progress != after.Desks[0].Progress {
		t.Error("the floor advanced while an operator was replaying it")
	}
	if _, err := st.Replay(ctx, -1); err != nil {
		t.Fatal(err)
	}
}

func TestSimulationActuallyMoves(t *testing.T) {
	_, st := newTestServer(t)
	ctx := context.Background()
	before, _ := st.Snapshot(ctx)
	for i := 0; i < 40; i++ {
		st.Tick()
	}
	after, _ := st.Snapshot(ctx)
	if before.Stats.EventCount == after.Stats.EventCount {
		t.Error("40 ticks emitted no events")
	}
	if after.Stats.Spend <= before.Stats.Spend {
		t.Error("40 ticks cost nothing")
	}
}

func TestPausedFloorDoesNotTick(t *testing.T) {
	_, st := newTestServer(t)
	ctx := context.Background()
	if _, err := st.SetPaused(ctx, true); err != nil {
		t.Fatal(err)
	}
	before, _ := st.Snapshot(ctx)
	for i := 0; i < 30; i++ {
		st.Tick()
	}
	after, _ := st.Snapshot(ctx)
	if before.Stats.Clock != after.Stats.Clock {
		t.Error("a paused floor advanced its clock")
	}
}

// ---------- the master record ----------

func TestProjectionAndCoverage(t *testing.T) {
	_, st := newTestServer(t)
	m, err := st.Master(context.Background(), "p-1")
	if err != nil {
		t.Fatal(err)
	}
	pr := m.Project()
	// 1 product + (3 goals + 4 rules) + 4 stories + 10 criteria + 2 diagrams
	// + 2 decisions + 2 repos
	if pr.Vertices != 1+7+4+10+2+2+2 {
		t.Errorf("vertices = %d", pr.Vertices)
	}
	// 3 of 4 stories carry a task.
	if got := m.Coverage(); got != 75 {
		t.Errorf("coverage = %d, want 75", got)
	}
}

func TestReadinessIsHonestAboutAnEmptyRecord(t *testing.T) {
	_, st := newTestServer(t)
	ctx := context.Background()
	full, _ := st.Master(ctx, "p-1")
	thin, _ := st.Master(ctx, "p-5")

	met := func(items []ReadinessItem) int {
		n := 0
		for _, i := range items {
			if i.OK {
				n++
			}
		}
		return n
	}
	// p-1 carries every signal the record can express. The twelfth, verification
	// strategy, has nowhere to live until M1 widens the record, so 11 is the
	// ceiling and the score that goes with it is 95 -- still "complete".
	if got := met(full.Readiness()); got != 11 {
		t.Errorf("p-1 is a complete record: %d of 12 signals, want 11", got)
	}
	if got := full.ReadinessScore(); got != 95 {
		t.Errorf("p-1 scores %d, want 95", got)
	}
	if met(thin.Readiness()) >= 7 {
		t.Error("p-5 has no stories and no diagrams; readiness must say so")
	}
	// And saying so means BR-07 will refuse it, which is the point of scoring.
	if got := thin.ReadinessScore(); got >= PlanFloor {
		t.Errorf("p-5 scores %d, which clears the planning floor of %d", got, PlanFloor)
	}
}

func TestAddStoryRejectsAnEmptyOne(t *testing.T) {
	_, st := newTestServer(t)
	if _, err := st.AddStory(context.Background(), "p-1", Story{Want: "something"}); err == nil {
		t.Fatal("a story with no role must be refused")
	}
}

func TestMasterCloneIsDeep(t *testing.T) {
	_, st := newTestServer(t)
	ctx := context.Background()
	m, _ := st.Master(ctx, "p-1")
	m.Stories[0].AC = append(m.Stories[0].AC, "injected by the caller")
	m.BRD.Rules = append(m.BRD.Rules, "injected")

	again, _ := st.Master(ctx, "p-1")
	if len(again.Stories[0].AC) != 3 || len(again.BRD.Rules) != 4 {
		t.Error("mutating a returned Master changed the store")
	}
}

// ---------- planning ----------

func TestPlanShapeAndDependencies(t *testing.T) {
	_, st := newTestServer(t)
	ctx := context.Background()
	plan, err := st.PlanWorkflow(ctx, WorkflowDraft{ProductID: "p-1", Stories: []string{"US-01", "US-02"}, Gate: true})
	if err != nil {
		t.Fatal(err)
	}
	// planner, architect, (design+review+backend+qa)x2, security, devops
	//
	// The design gate (M17) put two tasks between the architect and the
	// implementation of each story: the story is designed, the design is
	// reviewed, and only then is it built.
	if len(plan) != 12 {
		t.Fatalf("plan has %d tasks, want 12: %+v", len(plan), plan)
	}
	if plan[0].Role != "planner" || plan[1].Role != "architect" {
		t.Error("a plan always starts planner then architect")
	}
	// And the gate is an edge: every implementation waits on its review.
	for _, task := range plan {
		if task.Kind != TaskKindImplement || task.Role != "backend" {
			continue
		}
		if len(task.Deps) != 1 {
			t.Errorf("%s waits on %v; an implementation waits on its review alone", task.ID, task.Deps)
			continue
		}
		if dep := plan[0]; dep.ID == task.Deps[0] && dep.Kind != TaskKindReview {
			t.Errorf("%s waits on %s, which is a %s; an implementation waits on a review",
				task.ID, task.Deps[0], dep.Kind)
		}
	}

	// DEPENDS_ON points from the dependent to its prerequisite, and the chain
	// must be acyclic -- the planner invariant from the ArcadeDB model §11.
	seen := map[string]bool{}
	for _, task := range plan {
		for _, dep := range task.Deps {
			if !seen[dep] {
				t.Errorf("%s depends on %s, which is not earlier in the plan", task.ID, dep)
			}
		}
		seen[task.ID] = true
	}

	last := plan[len(plan)-1]
	if last.Role != "devops" || !last.Gate {
		t.Error("the gated task is the last one, and it is devops")
	}
	if last.Model != "claude-opus-5" {
		t.Errorf("a gated task runs on the top tier, got %s", last.Model)
	}
}

// TestPlanWithoutStoriesStillProducesOne is gone, and its old TC-076 is struck
// rather than renumbered. It asserted that a record with no stories still got
// the six-task stage template -- which is precisely what BR-07 forbids. The
// behaviour it pinned was the absence of the rule, not a feature of it. The
// refusal is covered by TestThePlannerRefusesBelowTheFloorAndSaysWhatIsMissing
// in readiness_test.go.

func TestCreateWorkflowMovesCoverage(t *testing.T) {
	_, st := newTestServer(t)
	ctx := context.Background()
	d := WorkflowDraft{ProductID: "p-1", Goal: "Ship membership pricing", Stories: []string{"US-04"}, Gate: true}
	plan, _ := st.PlanWorkflow(ctx, d)

	before, _ := st.Master(ctx, "p-1")
	if _, err := st.CreateWorkflow(ctx, d, plan); err != nil {
		t.Fatal(err)
	}
	after, _ := st.Master(ctx, "p-1")
	if after.Coverage() <= before.Coverage() {
		t.Errorf("US-04 had no tasks; planning it must raise coverage (%d -> %d)", before.Coverage(), after.Coverage())
	}
}

func TestCreateWorkflowNeedsAnObjective(t *testing.T) {
	_, st := newTestServer(t)
	ctx := context.Background()
	d := WorkflowDraft{ProductID: "p-1"}
	plan, _ := st.PlanWorkflow(ctx, d)
	if _, err := st.CreateWorkflow(ctx, d, plan); err == nil {
		t.Fatal("a workflow with no objective must be refused")
	}
}

// The wizard's draft round-trips through hidden form fields. If that breaks,
// step 3 silently plans against the wrong product.
func TestWizardDraftRoundTrips(t *testing.T) {
	s, _ := newTestServer(t)
	form := url.Values{
		"step": {"2"}, "product": {"p-1"}, "goal": {"Ship it"},
		"priority": {"medium"}, "gate": {"no"},
		"stories": {"US-01", "US-02"},
	}
	w := do(t, s, "POST", "/ui/cw/step/3", form, boCookies()...)
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{`name="product" value="p-1"`, `name="goal" value="Ship it"`, `value="US-01"`, `value="US-02"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the draft lost %s", want)
		}
	}
}

func TestWizardRefusesToAdvanceWithoutAProduct(t *testing.T) {
	s, _ := newTestServer(t)
	w := do(t, s, "POST", "/ui/cw/step/2", url.Values{"step": {"1"}}, boCookies()...)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Pick the product") {
		t.Error("the refusal must reach the operator as a toast")
	}
}

// Going BACK must never be blocked by the field you went back to fix.
func TestWizardBackIsNeverValidated(t *testing.T) {
	s, _ := newTestServer(t)
	w := do(t, s, "POST", "/ui/cw/step/1", url.Values{"step": {"2"}, "product": {""}}, boCookies()...)
	if w.Code != 200 {
		t.Fatalf("going back was blocked: %d", w.Code)
	}
}

// ---------- the sequence renderer ----------

func TestRenderSequence(t *testing.T) {
	svg := string(RenderSequence("Cashier -> OrderAPI: submit order\nOrderAPI --> Cashier: total"))
	for _, want := range []string{`<svg class="seq"`, "Cashier", "OrderAPI", "submit order", `class="arrow ret"`} {
		if !strings.Contains(svg, want) {
			t.Errorf("missing %q in\n%s", want, svg)
		}
	}
}

func TestRenderSequenceFallsBackToText(t *testing.T) {
	got := string(RenderSequence("Customer 1---0..1 Membership"))
	if !strings.HasPrefix(got, "<pre>") {
		t.Errorf("a source with no arrow is shown as text, got %q", got)
	}
}

// The renderer builds SVG by hand, so every value that reaches it must be
// escaped by hand too. This is the one place in the codebase where
// html/template is not doing that work.
func TestRenderSequenceEscapes(t *testing.T) {
	got := string(RenderSequence(`<script>x</script> -> B: </text><script>alert(1)</script>`))
	if strings.Contains(got, "<script>") {
		t.Errorf("unescaped markup reached the SVG:\n%s", got)
	}
}

// ---------- small helpers ----------

func TestSplitLinesDropsBlanks(t *testing.T) {
	got := splitLines("one\n\n  two  \n\n")
	if len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Errorf("got %q", got)
	}
}

func TestSlug(t *testing.T) {
	for in, want := range map[string]string{
		"Route Planner": "route-planner",
		"  A  B  ":      "a-b",
		"Ops/Warehouse": "ops-warehouse",
	} {
		if got := slug(in); got != want {
			t.Errorf("slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func trunc(s string) string {
	if len(s) > 400 {
		return s[:400] + "…"
	}
	return s
}

// TestTheBoardHostHasABoundedHeight pins a failure that is silent by nature: the
// vendored board (sisflow-board.css) is `height:100%; overflow:hidden` with
// `.sfb-body` as its one scroller and `overscroll-behavior:contain` on it. In the
// back office's normal-flow block the host had no height, so the board grew to
// its content (13,000px for 40 story lanes), `.sfb-body` could never scroll
// vertically, its horizontal scrollbar sat off screen, and `contain` stopped the
// wheel reaching the page: scrolling over the board did nothing, and nothing
// errored. Found in a real browser (the wheel moved nothing); this keeps the two
// rules that fix it from being "tidied" away.
func TestTheBoardHostHasABoundedHeight(t *testing.T) {
	b, err := os.ReadFile("static/app.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(b)
	host := regexp.MustCompile(`\.sisflow-board-host\{[^}]*height:calc\(100d?vh - \d+px\)[^}]*min-height:\d+px[^}]*\}`)
	if !host.MatchString(css) {
		t.Errorf(".sisflow-board-host has no bounded height (height:calc(100vh - Npx) with a min-height): " +
			"the board grows to its content and .sfb-body can never scroll vertically")
	}
	if !strings.Contains(css, ".sisflow-board-host .sfb-body{overscroll-behavior:auto}") {
		t.Errorf(".sfb-body still has overscroll-behavior:contain: a wheel over the board never reaches the page")
	}
	// D-095: the server board is bounded the same way, so each column scrolls
	// inside it and its header stays in view; and the columns are as wide as
	// one variable says, on both boards.
	ssr := regexp.MustCompile(`\.board-ssr\{[^}]*height:calc\(100d?vh - \d+px\)[^}]*min-height:\d+px[^}]*\}`)
	if !ssr.MatchString(css) || !strings.Contains(css, ".board-col-cards{flex:1 1 auto;min-height:48px;overflow-y:auto") {
		t.Errorf(".board-ssr has no bounded height, or .board-col-cards does not scroll inside it: a long column would push the page")
	}
	if !strings.Contains(css, "--board-col:300px") || !strings.Contains(css, "grid-auto-columns:var(--board-col)") ||
		!strings.Contains(css, ".sisflow-board-host .sfb-column{width:var(--board-col)}") {
		t.Errorf("the board's column width is not the one variable --board-col on both boards")
	}
}

// TC-762: the product's Timeline and QA tables are paged by ten (D-097), and the
// page is part of the tab's address.
func TestTheTimelineAndQAPanesPageByTen(t *testing.T) {
	srv, st := newTestServer(t)
	slug := slugOfProduct(t, st, "p-1")
	st.mu.Lock()
	for i := 0; i < 14; i++ {
		st.emit(TaskMoved, "p-1", fmt.Sprintf("paging event %d", i), KindInfo, EventPayload{})
	}
	st.mu.Unlock()
	cur := map[string]string{"HX-Current-URL": "https://gaos.example/p/" + slug + "/timeline"}
	rows := func(b string) int { return strings.Count(b, `<tr class="ev-row`) }
	total := func(b string) int {
		m := regexp.MustCompile(`Showing 1–10 of (\d+)`).FindStringSubmatch(b)
		if m == nil {
			return 0
		}
		n, _ := strconv.Atoi(m[1])
		return n
	}
	p1 := boardHX(t, srv, "GET", "/products/p-1?tab=timeline", cur).Body.String()
	n := total(p1)
	if rows(p1) != 10 || n < 14 {
		t.Fatalf("Timeline page 1: %d row(s) of %d; want 10 of at least 14", rows(p1), n)
	}
	last := (n + 9) / 10
	pl := boardHX(t, srv, "GET", "/products/p-1?tab=timeline&page="+strconv.Itoa(last), cur)
	if got, want := rows(pl.Body.String()), n-(last-1)*10; got != want {
		t.Errorf("Timeline page %d: %d row(s), want %d", last, got, want)
	}
	if u := pl.Header().Get("HX-Replace-Url"); !strings.HasSuffix(u, "p/"+slug+"/timeline?page="+strconv.Itoa(last)) {
		t.Errorf("Timeline page %d wrote the address %q; want p/%s/timeline?page=%d", last, u, slug, last)
	}

	var cases []QaCase
	var epicIn []QaEpic
	for i := 1; i <= 14; i++ {
		code := fmt.Sprintf("EP%c", 'A'+rune(i-1))
		epicIn = append(epicIn, QaEpic{Code: code, Title: "Epic " + code, Service: "svc"})
		c := qaCase("TC-" + code + "-001")
		c.Epic = code
		cases = append(cases, c)
	}
	out, _, err := st.QaImport(context.Background(), "p-1", QaImportIn{Seed: true, Epics: epicIn, Cases: cases})
	if err != nil || len(out.Rejected) > 0 {
		t.Fatalf("QaImport: %v, rejected %+v", err, out.Rejected)
	}
	q1 := boardHX(t, srv, "GET", "/products/p-1?tab=qa", nil).Body.String()
	q2 := boardHX(t, srv, "GET", "/products/p-1?tab=qa&page=2", nil).Body.String()
	epics := func(b string) int { return strings.Count(b, `<td class="mono qa-code">`) }
	if epics(q1) != 10 || epics(q2) != 4 || !strings.Contains(q1, `By epic <span class="sp mono">14</span>`) {
		t.Errorf("QA by epic: %d then %d row(s); want 10 then 4, with the heading counting all 14", epics(q1), epics(q2))
	}
}

// TC-759: the wizard opened from a product shows the product instead of the
// picker, and keeps showing it through every step. Presentation only.
func TestTheWizardLocksTheProduct(t *testing.T) {
	s, _ := newTestServer(t)
	open := do(t, s, "GET", "/ui/cw?product=p-1&lock=1", nil, boCookies()...)
	b := open.Body.String()
	if open.Code != 200 || strings.Contains(b, "data-pick=") || !strings.Contains(b, `class="cw-locked"`) || !strings.Contains(b, "New tasks for ") || !strings.Contains(b, `name="lock" value="1"`) {
		t.Fatalf("the wizard opened locked answered %d; want no picker, a cw-locked block, \"New tasks for\" and the lock carried", open.Code)
	}
	form := url.Values{"step": {"1"}, "product": {"p-1"}, "lock": {"1"}, "goal": {"Ship it"}, "priority": {"high"}, "gate": {"yes"}}
	two := do(t, s, "POST", "/ui/cw/step/2", form, boCookies()...).Body.String()
	if !strings.Contains(two, `name="lock" value="1"`) {
		t.Errorf("step 2 lost the lock")
	}
	form.Set("step", "2")
	back := do(t, s, "POST", "/ui/cw/step/1", form, boCookies()...).Body.String()
	if strings.Contains(back, "data-pick=") || !strings.Contains(back, `class="cw-locked"`) {
		t.Errorf("going back to step 1 shows the picker again; the lock must round-trip")
	}
	three := do(t, s, "POST", "/ui/cw/step/3", form, boCookies()...).Body.String()
	if !strings.Contains(three, ">Create tasks</button>") {
		t.Errorf("step 3 of the locked wizard does not submit with \"Create tasks\"")
	}
	for _, q := range []string{"/ui/cw?product=p-1", "/ui/cw?product=nope&lock=1"} {
		w := do(t, s, "GET", q, nil, boCookies()...)
		if w.Code != 200 || !strings.Contains(w.Body.String(), "data-pick=") || strings.Contains(w.Body.String(), `class="cw-locked"`) {
			t.Errorf("%s answered %d without the picker; an unlocked or unknown product shows every product", q, w.Code)
		}
	}
}

// TC-761: Sync record to graph is offered on the tabs whose content it writes,
// and the Board does not repeat Create workflow beside its own New task.
func TestSyncRecordShowsOnlyOnRecordTabs(t *testing.T) {
	s, _ := newTestServer(t)
	title := func(tab string) string {
		b := boardHX(t, s, "GET", "/products/p-1?tab="+tab, nil).Body.String()
		i := strings.Index(b, `<header class="bo-title">`)
		if i < 0 {
			t.Fatalf("tab %s has no title row", tab)
		}
		return b[i : i+strings.Index(b[i:], "</header>")]
	}
	for tab, want := range map[string]bool{"brd": true, "blueprint": true, "stories": true, "board": false, "timeline": false, "qa": false} {
		if got := strings.Contains(title(tab), ">Sync record to graph</button>"); got != want {
			t.Errorf("tab %s: Sync record to graph shown = %v, want %v", tab, got, want)
		}
	}
	if strings.Contains(title("board"), ">Create workflow</button>") {
		t.Errorf("the Board's title row repeats Create workflow beside New task")
	}
	if !strings.Contains(title("brd"), `hx-get="ui/cw?product=p-1&amp;lock=1"`) {
		t.Errorf("Create workflow on a product tab does not open the wizard locked to the product")
	}
}
