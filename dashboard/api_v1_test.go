package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// The machine API (api_v1.go). Tokens here are fixtures; none is a real one.

const (
	tokAdmin = "gaos_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	tokQA    = "gaos_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	tokOut   = "gaos_cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
)

// apiFixture is a server with two created products, p-N scoped to the QA token.
func apiFixture(t *testing.T, cfg func(pids []string) *apiConfig) (*server, *MemStore, []string) {
	t.Helper()
	srv, st := newTestServer(t)
	var pids []string
	for _, n := range []string{"API One", "API Two"} {
		if _, err := st.CreateProduct(context.Background(), ProductDraft{Name: n}); err != nil {
			t.Fatal(err)
		}
		ps, _ := st.Products(context.Background())
		pids = append(pids, ps[len(ps)-1].ID)
	}
	srv.api = cfg(pids)
	return srv, st, pids
}

func bothTokens(pids []string) *apiConfig {
	return newAPIConfig(tokAdmin, tokQA, tokOut, pids[0])
}

func call(t *testing.T, srv *server, method, path, token, body string, hdr ...string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	return rec
}

func TestAPIFailsClosed(t *testing.T) {
	srv, _, pids := apiFixture(t, bothTokens)
	sum := "/v1/products/" + pids[0] + "/qa/summary"
	cat := "/v1/products/" + pids[0] + "/qa/catalog"

	check := func(name string, rec *httptest.ResponseRecorder, want int) {
		t.Helper()
		if rec.Code != want {
			t.Errorf("%s: answered %d, want %d (%s)", name, rec.Code, want, strings.TrimSpace(rec.Body.String()))
		}
		for _, tok := range []string{tokAdmin, tokQA, tokOut} {
			if strings.Contains(rec.Body.String(), tok) {
				t.Errorf("%s: a token appeared in the response body", name)
			}
		}
	}
	check("no header", call(t, srv, "GET", sum, "", ""), 401)
	check("wrong token", call(t, srv, "GET", sum, "gaos_"+strings.Repeat("z", 40), ""), 401)
	check("qa token on the admin route", call(t, srv, "POST", cat, tokQA, `{"cases":[]}`), 403)
	check("admin token on a qa route", call(t, srv, "GET", sum, tokAdmin, ""), 200)
	check("qa token on its route", call(t, srv, "GET", sum, tokQA, ""), 200)
	check("through /app", call(t, srv, "GET", sum, tokQA, "", "X-Forwarded-Prefix", "/app"), 404)
	// The stripped path is /v1; the public /api/v1 never reaches this mux.
	if rec := call(t, srv, "GET", "/api"+sum, tokQA, ""); rec.Code == 200 {
		t.Error("/api/v1/... answered 200: the mux must register the STRIPPED path (Traefik removes /api)")
	}

	// Not configured at all: nil, no tokens, equal tokens, malformed tokens.
	for name, cfg := range map[string]*apiConfig{
		"nil":            nil,
		"no tokens":      newAPIConfig("", "", "", ""),
		"equal":          newAPIConfig(tokAdmin, tokAdmin, "", ""),
		"equal to out":   newAPIConfig(tokAdmin, tokQA, tokQA, ""),
		"admin == out":   newAPIConfig(tokAdmin, "", tokAdmin, ""),
		"only unset":     newAPIConfig("", "", tokOut, ""),
		"short is unset": newAPIConfig("", "", "", ""),
	} {
		srv.api = cfg
		check(name+" -> disabled", call(t, srv, "GET", sum, tokAdmin, ""), 503)
	}
	// One configured, one not: the configured one works, the other 401s.
	srv.api = newAPIConfig("", tokQA, "", pids[0])
	check("only qa configured", call(t, srv, "GET", sum, tokQA, ""), 200)
	check("admin unset", call(t, srv, "POST", cat, tokAdmin, `{}`), 401)

	// A write in flight blocks a second one of the same principal (429), not a read.
	srv.api = bothTokens(pids)
	srv.api.qaBusy.Store(true)
	rec := call(t, srv, "POST", "/v1/products/"+pids[0]+"/qa/results", tokQA, `{"items":[]}`)
	check("second concurrent write", rec, 429)
	if rec.Header().Get("Retry-After") == "" {
		t.Error("a 429 carries no Retry-After")
	}
	check("a read while a write is in flight", call(t, srv, "GET", sum, tokQA, ""), 200)
	srv.api.qaBusy.Store(false)
	check("write after release", call(t, srv, "POST", "/v1/products/"+pids[0]+"/qa/results", tokQA, `{"items":[]}`), 200)

	// The token files: empty, missing, malformed and good.
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	for name, path := range map[string]string{
		"empty": write("empty", ""), "no prefix": write("np", strings.Repeat("a", 50)),
		"short": write("sh", "gaos_abc"), "bad alphabet": write("ba", "gaos_"+strings.Repeat("a-", 20)), "missing": filepath.Join(dir, "absent"), "unset": "",
	} {
		t.Setenv("GAOS_T_FILE", path)
		if got := readTokenFile("GAOS_T_FILE"); got != "" {
			t.Errorf("token file %q must be treated as unset, got a token", name)
		}
	}
	t.Setenv("GAOS_T_FILE", write("good", tokQA+"\n"))
	if got := readTokenFile("GAOS_T_FILE"); got != tokQA {
		t.Errorf("a good token file with a trailing newline was not read back")
	}
	if d := newAPIConfig(tokAdmin, tokQA, "", "p-1").Describe(); strings.Contains(d, "gaos_") {
		t.Errorf("the boot line names a token: %q", d)
	}
}

func TestAPIQaTokenIsScopedToItsProducts(t *testing.T) {
	srv, _, pids := apiFixture(t, bothTokens)
	own, other := pids[0], pids[1]

	body := call(t, srv, "GET", "/v1/products", tokQA, "").Body.String()
	if !strings.Contains(body, `"`+own+`"`) || strings.Contains(body, `"`+other+`"`) {
		t.Errorf("the QA token's product list must hold only %s: %s", own, body)
	}
	if !strings.Contains(call(t, srv, "GET", "/v1/products", tokAdmin, "").Body.String(), `"`+other+`"`) {
		t.Error("the admin token must list every product")
	}
	for _, path := range []string{"/qa/summary", "/qa/cases"} {
		if rec := call(t, srv, "GET", "/v1/products/"+other+path, tokQA, ""); rec.Code != 404 {
			t.Errorf("GET %s on an unscoped product answered %d, want 404 (a 403 would say it exists)", path, rec.Code)
		}
	}
	if rec := call(t, srv, "POST", "/v1/products/"+other+"/qa/results", tokQA, `{"items":[]}`); rec.Code != 404 {
		t.Errorf("results on an unscoped product answered %d, want 404", rec.Code)
	}
	if rec := call(t, srv, "GET", "/v1/products/"+other+"/qa/summary", tokAdmin, ""); rec.Code != 200 {
		t.Errorf("the admin token must reach every product, got %d", rec.Code)
	}
	if rec := call(t, srv, "GET", "/v1/products/p-9999/qa/summary", tokAdmin, ""); rec.Code != 404 {
		t.Errorf("an unknown product answered %d, want 404", rec.Code)
	}

	// Unset scope reaches nothing (fail closed). A NAME in the list matches nothing:
	// names are not unique and anyone with the cookie can make one.
	for name, products := range map[string]string{"unset": "", "a name": "API One"} {
		srv.api = newAPIConfig(tokAdmin, tokQA, "", products)
		if rec := call(t, srv, "GET", "/v1/products/"+own+"/qa/summary", tokQA, ""); rec.Code != 404 {
			t.Errorf("QA scope %s: answered %d, want 404", name, rec.Code)
		}
		var out struct {
			Products []apiProductOut `json:"products"`
		}
		_ = json.Unmarshal(call(t, srv, "GET", "/v1/products", tokQA, "").Body.Bytes(), &out)
		if len(out.Products) != 0 {
			t.Errorf("QA scope %s: listed %d products, want none", name, len(out.Products))
		}
	}
	// force is the admin's.
	srv.api = bothTokens(pids)
	if rec := call(t, srv, "POST", "/v1/products/"+own+"/qa/results", tokQA, `{"force":true,"items":[]}`); rec.Code != 403 {
		t.Errorf("force with the QA token answered %d, want 403", rec.Code)
	}
}

func TestAPILimitsAndStrictDecoding(t *testing.T) {
	srv, st, pids := apiFixture(t, bothTokens)
	id := pids[0]
	res, cat := "/v1/products/"+id+"/qa/results", "/v1/products/"+id+"/qa/catalog"

	items := func(n int) string {
		var b []string
		for i := 0; i < n; i++ {
			b = append(b, fmt.Sprintf(`{"caseId":"TC-AUTH-%03d"}`, i%900+1))
		}
		return `{"items":[` + strings.Join(b, ",") + `]}`
	}
	if rec := call(t, srv, "POST", res, tokQA, items(qaMaxItems+1)); rec.Code != 413 {
		t.Errorf("%d items answered %d, want 413", qaMaxItems+1, rec.Code)
	}
	if rec := call(t, srv, "POST", res, tokQA, items(qaMaxItems)); rec.Code != 200 {
		t.Errorf("%d items answered %d, want 200", qaMaxItems, rec.Code)
	}
	big := `{"source":"` + strings.Repeat("a", apiMaxBody) + `","items":[]}`
	if rec := call(t, srv, "POST", res, tokQA, big); rec.Code != 413 {
		t.Errorf("a body over 1 MiB answered %d, want 413", rec.Code)
	}
	if rec := call(t, srv, "POST", res, tokQA, `{"items":[{"caseId":"TC-AUTH-001","decision":"wont"}]}`); rec.Code != 400 {
		t.Errorf("a results item naming a planning field answered %d, want 400", rec.Code)
	}
	if rec := call(t, srv, "POST", res, tokQA, `{"items":[]} {"x":1}`); rec.Code != 400 {
		t.Errorf("trailing JSON answered %d, want 400", rec.Code)
	}
	if rec := call(t, srv, "POST", res, tokQA, `not json`); rec.Code != 400 {
		t.Errorf("a non-JSON body answered %d, want 400", rec.Code)
	}
	if rec := call(t, srv, "POST", cat, tokAdmin, `{"cases":[],"surprise":1}`); rec.Code != 400 {
		t.Errorf("an unknown catalog field answered %d, want 400", rec.Code)
	}
	var cases []string
	for i := 0; i <= qaMaxItems; i++ {
		cases = append(cases, `{}`)
	}
	if rec := call(t, srv, "POST", cat, tokAdmin, `{"cases":[`+strings.Join(cases, ",")+`]}`); rec.Code != 413 {
		t.Errorf("%d catalog items answered %d, want 413", qaMaxItems+1, rec.Code)
	}
	// A catalog round trip over HTTP: created, then unchanged.
	one := `{"cases":[{"caseId":"TC-AUTH-001","epic":"AUTH","module":"A13","features":["A13.03"],"userStory":"US-AUTH-01",` +
		`"scenario":"Given a, when b, then c","polarity":"+","level":"API","pri":"P1","decision":"planned","reason":""}]}`
	for i, want := range []string{`"created":1`, `"unchanged":1`} {
		rec := call(t, srv, "POST", cat, tokAdmin, one)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), want) {
			t.Errorf("catalog post %d: %d %s, want %s", i+1, rec.Code, strings.TrimSpace(rec.Body.String()), want)
		}
	}
	// A GET's bad query
	for _, q := range []string{"after=-1", "after=x", "limit=0", "limit=201", "epic=lower"} {
		if rec := call(t, srv, "GET", "/v1/products/"+id+"/qa/cases?"+q, tokAdmin, ""); rec.Code != 400 {
			t.Errorf("?%s answered %d, want 400", q, rec.Code)
		}
	}
	if m, _ := st.Master(context.Background(), id); m.Qa.CatHead != 1 {
		t.Errorf("CatHead = %d after one created case, want 1", m.Qa.CatHead)
	}
}

func TestAPICasesCursorCoversEveryCaseOnce(t *testing.T) {
	srv, st, pids := apiFixture(t, bothTokens)
	id := pids[0]
	var all []QaCase
	want := map[string]bool{}
	for i := 1; i <= 7; i++ {
		c := qaCase(fmt.Sprintf("TC-AUTH-%03d", i))
		all = append(all, c)
		want[c.CaseID] = true
	}
	if _, _, err := st.QaImport(context.Background(), id, QaImportIn{Cases: all}); err != nil {
		t.Fatal(err)
	}
	// Re-touch one in the middle: it moves to the end of the feed, still once.
	all[2].Scenario = "Given changed, when edited, then it moves"
	if _, _, err := st.QaImport(context.Background(), id, QaImportIn{Cases: all[2:3]}); err != nil {
		t.Fatal(err)
	}

	seen := map[string]int{}
	after, pages := 0, 0
	for {
		pages++
		rec := call(t, srv, "GET", fmt.Sprintf("/v1/products/%s/qa/cases?after=%d&limit=3", id, after), tokQA, "")
		if rec.Code != 200 {
			t.Fatalf("page %d answered %d", pages, rec.Code)
		}
		var pg struct {
			Items []QaCase `json:"items"`
			Next  *int     `json:"next"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &pg); err != nil {
			t.Fatal(err)
		}
		for _, c := range pg.Items {
			seen[c.CaseID]++
			if c.Status == "" {
				t.Errorf("%s: the derived status is missing from the API's case", c.CaseID)
			}
		}
		if pg.Next == nil {
			break
		}
		after = *pg.Next
		if pages > 10 {
			t.Fatal("the cursor did not terminate")
		}
	}
	if len(seen) != len(want) {
		t.Errorf("the feed returned %d distinct cases, want %d: %v", len(seen), len(want), seen)
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("%s came back %d times, want once", id, n)
		}
	}
	if pages != 3 {
		t.Errorf("7 cases at 3 a page took %d pages, want 3 (the last, short page has next=null)", pages)
	}
	// Concurrent readers of a product that has NO cases must not write the
	// catalog map under a read lock (found by review: it crashed the process).
	bare := pids[1]
	srv.api = newAPIConfig(tokAdmin, tokQA, tokOut, id+","+bare)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = st.QaCases(context.Background(), bare, QaQuery{})
			_, _ = st.QaSummary(context.Background(), bare)
			_, _ = st.Board(context.Background(), bare)
			call(t, srv, "GET", "/v1/products/"+bare+"/qa/cases", tokQA, "")
		}()
	}
	wg.Wait()
	// the epic filter
	rec := call(t, srv, "GET", "/v1/products/"+id+"/qa/cases?epic=NONE", tokQA, "")
	if !strings.Contains(rec.Body.String(), `"items":[]`) {
		t.Errorf("an epic with no cases must answer an empty list, got %s", rec.Body.String())
	}
}

func TestQaWritesNeverTouchTheFloorInReplay(t *testing.T) {
	srv, st, pids := apiFixture(t, bothTokens)
	ctx := context.Background()
	id := pids[0]
	if _, _, err := st.QaImport(ctx, id, QaImportIn{Cases: []QaCase{qaCase("TC-AUTH-001")}}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Replay(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if f, _ := st.Snapshot(ctx); !f.Stats.Replay {
		t.Fatal("the fixture did not park the floor")
	}
	st.mu.RLock()
	live := make([]Desk, len(st.desks))
	for i, d := range st.desks {
		live[i] = d.Clone()
	}
	st.mu.RUnlock()

	now := qaNow() - 5
	body := fmt.Sprintf(`{"items":[{"caseId":"TC-AUTH-001","implemented":true,"spec":"a.spec.ts","result":"pass","runAt":%d}]}`, now)
	if rec := call(t, srv, "POST", "/v1/products/"+id+"/qa/results", tokQA, body); rec.Code != 200 {
		t.Errorf("a results post while parked answered %d (%s), want 200: a QA write carries no floor", rec.Code, strings.TrimSpace(rec.Body.String()))
	}
	one := `{"cases":[{"caseId":"TC-AUTH-002","epic":"AUTH","scenario":"Given a, when b, then c","polarity":"-","level":"API","pri":"P2","decision":"planned"}]}`
	if rec := call(t, srv, "POST", "/v1/products/"+id+"/qa/catalog", tokAdmin, one); rec.Code != 200 {
		t.Errorf("a catalog post while parked answered %d, want 200", rec.Code)
	}
	// What the write produced carries no floor, and the live desks did not move.
	_, res, _ := st.QaApplyResults(ctx, id, QaResultsIn{Items: []QaResultItem{{CaseID: "TC-AUTH-001", Spec: strp("b.spec.ts")}}})
	if len(res.Floor.Desks) != 0 {
		t.Errorf("a QA Result carried %d desks; persist would save the replayed past over the live floor", len(res.Floor.Desks))
	}
	st.mu.RLock()
	same := reflect.DeepEqual(live, st.desks)
	st.mu.RUnlock()
	if !same {
		t.Error("a QA write changed the live desks while the floor was parked")
	}
	// The back office's own sync path is refused in replay: s.mut hands out the read-only handle.
	req := httptest.NewRequest("POST", "/products/"+id+"/qa/sync/pull", nil)
	if _, _, err := srv.mut(req).QaApplyResults(ctx, id, QaResultsIn{}); err == nil {
		t.Error("the back office's QA write was allowed during replay")
	}
	if _, err := srv.mut(req).QaRecordSync(ctx, id, "pull", QaSyncSide{}); err == nil {
		t.Error("QaRecordSync was allowed during replay")
	}
	_, _ = st.Replay(ctx, -1)
	_ = http.StatusOK
}
