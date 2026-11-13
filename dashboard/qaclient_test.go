package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// GAOS calling the QA tools (qaclient.go, handlers_qa.go). The QA tools are an
// httptest server standing in for the contract in design §4.11.

func TestQaClientURLValidation(t *testing.T) {
	for url, ok := range map[string]bool{
		"https://qa-tool.example.com":           true,
		"https://qa-tool.example.com/base":      true,
		"http://10.10.0.9:8140":                 true,
		"http://127.0.0.1:9000":                 true,
		"http://qa-tool.example.com":            false, // a hostname over plain http
		"http://192.168.1.5":                    false, // RFC1918 outside the tunnel subnet
		"http://10.10.1.9":                      false,
		"https://user:pw@qa-tool.example.com":   false, // userinfo
		"https://qa-tool.example.com/?token=x":  false, // a query
		"https://qa-tool.example.com/#frag":     false,
		"ftp://qa-tool.example.com":             false,
		"":                                      false,
		"not a url":                             false,
		"https://":                              false,
		"http://10.10.0.4:8099":                 false, // denied below: GAOS's own gateway host
		"https://db.internal.example":           false, // denied below: GAOS's own database host
		"HTTPS://QA-TOOL.EXAMPLE.COM":           true,
		"http://[::1]:9000":                     true,
		"http://10.10.0.9@evil.example.com":     false,
		"https://qa-tool.example.com:8443/path": true,
	} {
		o := newQaOutbound(url, tokOut, []string{"10.10.0.4", "db.internal.example"})
		if got := o.why == ""; got != ok {
			t.Errorf("%q: usable=%v (%s), want %v", url, got, o.why, ok)
		}
	}
	if o := newQaOutbound("https://qa-tool.example.com", "", nil); o.why == "" {
		t.Error("a missing token must leave the client unconfigured")
	}
	// A cursor over 128 bytes, or with control characters, is refused.
	for cur, ok := range map[string]bool{"": true, "abc-123": true, strings.Repeat("a", 128): true,
		strings.Repeat("a", 129): false, "a\nb": false, "é": false} {
		if validCursor(cur) != ok {
			t.Errorf("cursor %q: valid=%v, want %v", cur, !ok, ok)
		}
	}
}

// fakeQA serves the QA tools' side of the contract.
type fakeQA struct {
	srv       *httptest.Server
	pages     map[string]string // after-cursor -> body
	hits      atomic.Int32
	auth      atomic.Value
	pushCodes []int
	pushBody  atomic.Int32
	pushed    [][]string
}

func newFakeQA(t *testing.T) *fakeQA {
	f := &fakeQA{pages: map[string]string{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.auth.Store(r.Header.Get("Authorization"))
		switch {
		case r.URL.Path == "/api/v1/gaos/results":
			f.hits.Add(1)
			body, ok := f.pages[r.URL.Query().Get("after")]
			if !ok {
				http.Error(w, "no such cursor", 404)
				return
			}
			_, _ = w.Write([]byte(body))
		case r.URL.Path == "/api/v1/gaos/catalog":
			n := int(f.pushBody.Add(1)) - 1
			var in qaPushBody
			_ = json.NewDecoder(r.Body).Decode(&in)
			var ids []string
			for _, c := range in.Cases {
				ids = append(ids, c.CaseID)
			}
			f.pushed = append(f.pushed, ids)
			code := 200
			if n < len(f.pushCodes) {
				code = f.pushCodes[n]
			}
			w.WriteHeader(code)
			if code == 200 {
				rej := ""
				if n == 0 {
					rej = fmt.Sprintf(`{"caseId":%q,"reason":"unknown epic"},{"caseId":"not a case id","reason":"x"}`, ids[0])
				}
				_, _ = fmt.Fprintf(w, `{"accepted":%d,"rejected":[%s]}`, len(ids), rej)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func TestPullAppliesPagesAndKeepsTheCursor(t *testing.T) {
	s, pid := qaFixture(t)
	ctx := context.Background()
	importCases(t, s, pid, false, qaCase("TC-AUTH-001"), qaCase("TC-AUTH-002"), qaCase("TC-AUTH-003"))
	now := qaNow() - 10
	// A fresher run is already stored for 003, so the pulled one is stale.
	if _, _, err := s.QaApplyResults(ctx, pid, QaResultsIn{Items: []QaResultItem{{CaseID: "TC-AUTH-003", Result: strp("pass"), RunAt: i64p(now)}}}); err != nil {
		t.Fatal(err)
	}
	f := newFakeQA(t)
	f.pages[""] = fmt.Sprintf(`{"product":%q,"items":[{"caseId":"TC-AUTH-001","implemented":true,"spec":"a.spec.ts","result":"pass","runAt":%d}],"next":"c1","more":true}`, pid, now)
	f.pages["c1"] = fmt.Sprintf(`{"product":%q,"items":[{"caseId":"TC-AUTH-002","result":"fail","runAt":%d},{"caseId":"TC-AUTH-003","result":"fail","runAt":%d},{"caseId":"TC-GONE-001","implemented":true}],"next":"c2","more":false}`, pid, now, now-500)
	o := newQaOutbound(f.srv.URL, tokOut, nil)
	if o.why != "" {
		t.Fatal(o.why)
	}

	side := o.qaPull(ctx, s, pid, QaSyncSide{})
	if !side.OK || side.Cursor != "c2" || side.Items != 4 {
		t.Fatalf("pull = %+v, want ok, cursor c2, 4 items", side)
	}
	if !strings.Contains(side.Note, "2 applied") || !strings.Contains(side.Note, "2 rejected") {
		t.Errorf("note %q must say 2 applied and 2 rejected (one unknown, one stale)", side.Note)
	}
	if got, _ := f.auth.Load().(string); got != "Bearer "+tokOut {
		t.Error("the outbound call did not carry the outbound bearer")
	}
	sum, _ := s.QaSummary(ctx, pid)
	if sum.Totals.Pass != 2 || sum.Totals.Fail != 1 || sum.Totals.Automated != 1 {
		t.Errorf("after the pull: %+v, want 2 pass (001 and the older 003), 1 fail (002), 1 automated", sum.Totals)
	}
	// Resuming from the stored cursor asks for it.
	f.pages["c2"] = fmt.Sprintf(`{"product":%q,"items":[],"next":"c2","more":false}`, pid)
	hits := f.hits.Load()
	if side2 := o.qaPull(ctx, s, pid, side); !side2.OK || side2.Cursor != "c2" || f.hits.Load() != hits+1 {
		t.Errorf("a resumed pull = %+v after %d more requests, want ok, cursor c2 and one request", side2, f.hits.Load()-hits)
	}

	// What comes back is untrusted: each of these fails the sync and writes nothing.
	for name, body := range map[string]string{
		"another product":   `{"product":"p-999","items":[],"next":"","more":false}`,
		"a planning field":  fmt.Sprintf(`{"product":%q,"items":[{"caseId":"TC-AUTH-001","decision":"wont"}],"next":"","more":false}`, pid),
		"an unknown field":  fmt.Sprintf(`{"product":%q,"items":[],"next":"","more":false,"extra":1}`, pid),
		"a hostile cursor":  fmt.Sprintf(`{"product":%q,"items":[],"next":"a\nb","more":false}`, pid),
		"too long a cursor": fmt.Sprintf(`{"product":%q,"items":[],"next":%q,"more":false}`, pid, strings.Repeat("c", 129)),
		"not json":          `<html>`,
	} {
		f.pages["bad"] = body
		got := o.qaPull(ctx, s, pid, QaSyncSide{Cursor: "bad"})
		if got.OK || got.Cursor != "bad" {
			t.Errorf("%s: pull = %+v, want a failure that keeps the cursor", name, got)
		}
	}
	over := make([]string, qaMaxItems+1)
	for i := range over {
		over[i] = `{"caseId":"TC-AUTH-001"}`
	}
	f.pages["big"] = fmt.Sprintf(`{"product":%q,"items":[%s],"next":"","more":false}`, pid, strings.Join(over, ","))
	if got := o.qaPull(ctx, s, pid, QaSyncSide{Cursor: "big"}); got.OK {
		t.Error("a page of more than 200 items was accepted")
	}
	// A non-2xx is a recorded failure.
	f.pages["404"] = ""
	if got := o.qaPull(ctx, s, pid, QaSyncSide{Cursor: "nope"}); got.OK || !strings.Contains(got.Note, "HTTP 404") {
		t.Errorf("a 404 = %+v, want a failure naming the status", got)
	}
}

func TestPushAdvancesPerBatchAndKeepsRejections(t *testing.T) {
	s, pid := qaFixture(t)
	ctx := context.Background()
	var cs []QaCase
	for i := 1; i <= 450; i++ {
		cs = append(cs, qaCase(fmt.Sprintf("TC-AUTH-%03d", i)))
	}
	for i := 0; i < len(cs); i += qaMaxItems {
		importCases(t, s, pid, false, cs[i:min(i+qaMaxItems, len(cs))]...)
	}
	prods, _ := s.Products(ctx)
	var p Product
	for _, x := range prods {
		if x.ID == pid {
			p = x
		}
	}

	f := newFakeQA(t)
	f.pushCodes = []int{200, 500} // the second batch fails
	o := newQaOutbound(f.srv.URL, tokOut, nil)
	side := o.qaPush(ctx, s, p, QaSyncSide{})
	if side.OK || side.Cursor != "200" || side.Items != 200 {
		t.Fatalf("push = %+v, want a failure after batch 1: cursor 200, 200 items", side)
	}
	if !strings.Contains(side.Note, "HTTP 500") {
		t.Errorf("note %q must name the status", side.Note)
	}
	if side.RejectedN != 2 || len(side.Rejected) != 1 || side.Rejected[0] != "TC-AUTH-001" {
		t.Errorf("rejections: n=%d ids=%v, want 2 counted and only the well-formed id kept", side.RejectedN, side.Rejected)
	}
	if len(f.pushed) != 2 || len(f.pushed[0]) != 200 {
		t.Errorf("the QA tools saw %d batches, want 2 (the second refused)", len(f.pushed))
	}
	// Resuming from the stored cursor sends the rest, once.
	f.pushCodes, f.pushBody = nil, atomic.Int32{}
	f.pushed = nil
	side2 := o.qaPush(ctx, s, p, side)
	if !side2.OK || side2.Cursor != "450" || side2.Items != 250 || len(f.pushed) != 2 {
		t.Errorf("resumed push = %+v over %d batches, want ok, cursor 450, 250 items, 2 batches", side2, len(f.pushed))
	}
	if len(f.pushed) == 2 && f.pushed[0][0] != "TC-AUTH-201" {
		t.Errorf("the resumed push began at %s, want TC-AUTH-201", f.pushed[0][0])
	}
	// Nothing new: nothing is sent.
	f.pushed = nil
	if side3 := o.qaPush(ctx, s, p, side2); !side3.OK || side3.Items != 0 || len(f.pushed) != 0 {
		t.Errorf("a push with nothing new = %+v, %d batches; want ok and none sent", side3, len(f.pushed))
	}
}

func TestSyncRefusesRedirectsOverlapAndCooldown(t *testing.T) {
	srv, st, pids := apiFixture(t, func(p []string) *apiConfig { return newAPIConfig(tokAdmin, tokQA, tokOut, p[0]) })
	ctx := context.Background()
	id := pids[0]
	cookie := &http.Cookie{Name: boCookie, Value: "1"}
	post := func(path string) *httptest.ResponseRecorder {
		return do(t, srv, "POST", path, nil, cookie)
	}
	pull := "/products/" + id + "/qa/sync/pull"

	// Unconfigured: 409, and nothing was called.
	srv.qaOut = newQaOutbound("", "", nil)
	if rec := post(pull); rec.Code != 409 {
		t.Errorf("an unconfigured pull answered %d, want 409", rec.Code)
	}
	srv.qaOut = nil
	if rec := post(pull); rec.Code != 409 {
		t.Errorf("a pull with no outbound at all answered %d, want 409", rec.Code)
	}
	// A product outside GAOS_API_QA_PRODUCTS is refused even when the client works.
	f := newFakeQA(t)
	f.pages[""] = fmt.Sprintf(`{"product":%q,"items":[],"next":"n1","more":false}`, id)
	srv.qaOut = newQaOutbound(f.srv.URL, tokOut, nil)
	if rec := post("/products/" + pids[1] + "/qa/sync/pull"); rec.Code != 409 || f.hits.Load() != 0 {
		t.Errorf("an out-of-scope product answered %d after %d calls to the QA tools, want 409 and none", rec.Code, f.hits.Load())
	}
	// No cookie, no sync.
	if rec := do(t, srv, "POST", pull, nil); rec.Code != 401 {
		t.Errorf("a pull without the back-office cookie answered %d, want 401", rec.Code)
	}

	// The happy click records the outcome and emits one event.
	ev := countEventsOfType(st, RecordUpdated)
	if rec := post(pull); rec.Code != 200 {
		t.Fatalf("a pull answered %d: %s", rec.Code, rec.Body.String())
	}
	m, _ := st.Master(ctx, id)
	if !m.Qa.Pull.OK || m.Qa.Pull.Cursor != "n1" || m.Qa.Pull.At == 0 {
		t.Errorf("the pull was not recorded: %+v", m.Qa.Pull)
	}
	if countEventsOfType(st, RecordUpdated) != ev+1 {
		t.Error("a sync must emit exactly one RecordUpdated")
	}
	// A second click at once is the cooldown, process-wide (the other direction too).
	rec := post(pull)
	if rec.Code != 409 || rec.Header().Get("Retry-After") == "" {
		t.Errorf("a second click answered %d (Retry-After %q), want 409 with Retry-After", rec.Code, rec.Header().Get("Retry-After"))
	}
	if rec := post("/products/" + id + "/qa/sync/push"); rec.Code != 409 {
		t.Errorf("the other direction within the cooldown answered %d, want 409 (the cooldown is process-wide)", rec.Code)
	}
	// ... which passes once the cooldown has, and an overlapping sync is refused.
	srv.qaOut.cooldown = 0
	release, code, _, _ := srv.qaOut.begin()
	if release == nil {
		t.Fatalf("begin refused with cooldown 0: %s", code)
	}
	if rec := post(pull); rec.Code != 409 {
		t.Errorf("an overlapping sync answered %d, want 409", rec.Code)
	}
	release()
	if rec := post(pull); rec.Code != 200 {
		t.Errorf("a sync after the release answered %d, want 200", rec.Code)
	}
	// A redirect from the QA tools is never followed.
	var followed atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { followed.Store(true) }))
	defer target.Close()
	redir := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/api/v1/gaos/results", http.StatusFound)
	}))
	defer redir.Close()
	o := newQaOutbound(redir.URL, tokOut, nil)
	if side := o.qaPull(ctx, st, id, QaSyncSide{}); side.OK || followed.Load() || !strings.Contains(side.Note, "redirect") {
		t.Errorf("a redirect: ok=%v followed=%v note=%q, want a refusal that never followed it", side.OK, followed.Load(), side.Note)
	}
	// The tab's view model says why it is disabled, in visible text.
	srv.qaOut = newQaOutbound("", "", nil)
	if pane := srv.buildQAPane(ctx, m); pane.Sync.Enabled || !strings.Contains(pane.Sync.Why, "not connected") {
		t.Errorf("the pane for an unconfigured client: enabled=%v why=%q", pane.Sync.Enabled, pane.Sync.Why)
	}
	// Replay refuses the click, and the pane says so.
	srv.qaOut = newQaOutbound(f.srv.URL, tokOut, nil)
	srv.qaOut.cooldown = time.Nanosecond
	if _, err := st.Replay(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if rec := post(pull); rec.Code != 409 {
		t.Errorf("a pull during replay answered %d, want 409", rec.Code)
	}
	if pane := srv.buildQAPane(ctx, m); pane.Sync.Enabled || !strings.Contains(pane.Sync.Why, "replay") {
		t.Errorf("the pane during replay: enabled=%v why=%q", pane.Sync.Enabled, pane.Sync.Why)
	}
	_, _ = st.Replay(ctx, -1)
	_ = strconv.Itoa
}
