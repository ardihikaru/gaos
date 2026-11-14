package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// D-078, the QA tool's questions in the inbox: the machine API that asks them,
// the back office that answers them, and the webhook that carries the answer
// back. The design is docs/designs/qa-inbox-api.md.

// driveExternal writes the three event types the QA inbox adds, so the catalog
// test sees them emitted (see driveRunner).
func driveExternal(t *testing.T, s *MemStore) {
	t.Helper()
	ctx := t.Context()
	pid := s.desks[0].ID
	ask := ExternalAsk{ExternalID: "drive-1", Question: "Ship it?",
		Options: []QuestionOption{{Label: "Approve"}, {Label: "Reject"}}}
	if _, _, _, err := s.AskExternal(ctx, pid, ask); err != nil {
		t.Fatalf("asking the driven QA tool question failed: %v", err)
	}
	if _, err := s.AnswerExternal(ctx, pid, "drive-1", 1, ""); err != nil {
		t.Fatalf("answering the driven QA tool question failed: %v", err)
	}
	if _, err := s.RecordExternalDelivery(ctx, pid, "drive-1", 1, DeliveryDelivered, "", time.Now().Unix()); err != nil {
		t.Fatalf("recording the driven delivery failed: %v", err)
	}
}

func inboxPath(pid string, ext ...string) string {
	p := "/v1/products/" + pid + "/inbox"
	if len(ext) > 0 {
		p += "/" + ext[0]
	}
	return p
}

func askJSON(id string) string {
	return `{"externalId":"` + id + `","question":"Merge the flaky-test quarantine?","context":"3 specs, 2 weeks","title":"Quarantine"}`
}

// mustAsk asks one question as the QA token and returns the decoded answer.
func mustAsk(t *testing.T, srv *server, pid, body string) (int, apiInboxCreated) {
	t.Helper()
	rec := call(t, srv, "POST", inboxPath(pid), tokQA, body)
	var out apiInboxCreated
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func getState(t *testing.T, srv *server, pid, ext string) (int, apiInboxState) {
	t.Helper()
	rec := call(t, srv, "GET", inboxPath(pid, ext), tokQA, "")
	var out apiInboxState
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

// restartFrom is a process restart as far as the projection can tell: a new
// store whose stream is the old one's events after the same JSON round trip
// ArcadeStore.writeEvent and load() put them through (BR-01).
func restartFrom(t *testing.T, s *MemStore) *MemStore {
	t.Helper()
	n := NewMemStore(1)
	s.mu.RLock()
	defer s.mu.RUnlock()
	n.events = nil
	for _, e := range s.events {
		b, err := json.Marshal(eventPayloadWire{Text: e.Text, Kind: e.Kind, Clock: e.T, Delta: e.Payload})
		if err != nil {
			t.Fatal(err)
		}
		var w eventPayloadWire
		if err := json.Unmarshal(b, &w); err != nil {
			t.Fatal(err)
		}
		n.events = append(n.events, Event{Seq: e.Seq, Type: e.Type, DeskID: e.DeskID, Text: w.Text, Kind: w.Kind, T: w.Clock, Payload: w.Delta})
		if e.Seq > n.seq {
			n.seq = e.Seq
		}
	}
	return n
}

func TestAnInboxAskNeedsTheQAScopeAndTheProduct(t *testing.T) {
	srv, st, pids := apiFixture(t, bothTokens) // the QA token reaches pids[0] only
	ok, other := pids[0], pids[1]

	for _, tc := range []struct {
		name, method, path, token, body string
		want                            int
	}{
		{"no token", "POST", inboxPath(ok), "", askJSON("a1"), 401},
		{"wrong token", "POST", inboxPath(ok), "gaos_" + strings.Repeat("z", 40), askJSON("a1"), 401},
		{"the outbound token", "POST", inboxPath(ok), tokOut, askJSON("a1"), 401},
		{"in-scope product", "POST", inboxPath(ok), tokQA, askJSON("a1"), 201},
		{"out-of-scope product", "POST", inboxPath(other), tokQA, askJSON("a1"), 404},
		{"unknown product", "POST", inboxPath("p-nope"), tokQA, askJSON("a1"), 404},
		{"admin token, any product", "POST", inboxPath(other), tokAdmin, askJSON("a1"), 201},
		{"read in scope", "GET", inboxPath(ok, "a1"), tokQA, "", 200},
		{"read out of scope", "GET", inboxPath(other, "a1"), tokQA, "", 404},
		{"read an unknown id", "GET", inboxPath(ok, "nope"), tokQA, "", 404},
		{"read with a malformed id", "GET", inboxPath(ok, "a%20b"), tokQA, "", 404},
		{"read another product's id", "GET", inboxPath(ok, "a1"), tokAdmin, "", 200},
		// The QA token is not an admin: the routes that need one say 403.
		{"qa token on the catalog", "POST", "/v1/products/" + ok + "/qa/catalog", tokQA, `{"cases":[]}`, 403},
		{"qa token on the seed", "POST", "/v1/seed/product", tokQA, `{}`, 403},
		// And it is not a back-office session: the answer routes want the cookie.
		{"qa token answering", "POST", "/products/" + ok + "/inbox/a1/answer", tokQA, `{"choice":1}`, 401},
		{"qa token opening the sheet", "GET", "/ui/answer-ext/" + ok + "/a1", tokQA, "", 401},
	} {
		rec := call(t, srv, tc.method, tc.path, tc.token, tc.body, "Content-Type", "application/json")
		if rec.Code != tc.want {
			t.Errorf("%s: answered %d, want %d (%s)", tc.name, rec.Code, tc.want, strings.TrimSpace(rec.Body.String()))
		}
	}
	// Two items exist: the QA token's own, and the admin's on the other product.
	// The refused calls (out of scope, unknown product) wrote nothing.
	if items := st.externalItemsLocked(); len(items) != 2 {
		t.Errorf("%d items exist after the table; want 2, the refused calls wrote nothing", len(items))
	}
	for _, e := range st.events {
		if e.Type == ExternalQuestionAsked && !needsNotice(e) {
			t.Errorf("event %d (an ask) does not count toward the operator's notices", e.Seq)
		}
	}
	// A write in flight blocks a second one by the same token, like the other doors.
	srv.api.qaBusy.Store(true)
	if rec := call(t, srv, "POST", inboxPath(ok), tokQA, askJSON("a2")); rec.Code != 429 {
		t.Errorf("a second concurrent ask answered %d, want 429", rec.Code)
	}
	srv.api.qaBusy.Store(false)
}

func TestAnInboxAskIsIdempotentOnTheExternalID(t *testing.T) {
	srv, st, pids := apiFixture(t, bothTokens)
	pid := pids[0]

	code, first := mustAsk(t, srv, pid, askJSON("run-42:case-7"))
	if code != 201 || !first.Created || first.Status != "open" || first.ExternalID != "run-42:case-7" || first.ID == "" {
		t.Fatalf("the first ask answered %d %+v; want 201 created, open, with an id", code, first)
	}
	// Same id again, even with a different question: the original stands.
	code, again := mustAsk(t, srv, pid, `{"externalId":"run-42:case-7","question":"A different question?"}`)
	if code != 200 || again.Created || again.ID != first.ID {
		t.Fatalf("the second ask answered %d %+v; want 200, created false, the same id %q", code, again, first.ID)
	}
	st.mu.RLock()
	items := st.externalItemsLocked()
	st.mu.RUnlock()
	if len(items) != 1 || items[0].Question != "Merge the flaky-test quarantine?" {
		t.Fatalf("the store holds %d item(s) %+v; want the one original, never a duplicate", len(items), items)
	}
	asked := 0
	for _, e := range st.events {
		if e.Type == ExternalQuestionAsked {
			asked++
		}
	}
	if asked != 1 {
		t.Fatalf("%d ExternalQuestionAsked events; want 1", asked)
	}

	// After it is answered the same id still returns the item and reopens nothing.
	if _, err := st.AnswerExternal(t.Context(), pid, "run-42:case-7", 2, ""); err != nil {
		t.Fatal(err)
	}
	code, late := mustAsk(t, srv, pid, askJSON("run-42:case-7"))
	if code != 200 || late.Created || late.Status != "answered" {
		t.Fatalf("an ask after the answer gave %d %+v; want 200, not created, answered", code, late)
	}
	// The same id on another product is another item.
	if code, _ := mustAsk(t, srv, pid, askJSON("run-42:case-8")); code != 201 {
		t.Fatalf("a new id answered %d, want 201", code)
	}
	// The cap on open items per product refuses a runaway client without a duplicate.
	for i := 0; i < externalMaxOpen; i++ {
		ask := ExternalAsk{ExternalID: fmt.Sprintf("bulk-%d", i), Question: "q"}
		if _, _, _, err := st.AskExternal(t.Context(), pid, ask); err != nil {
			if !strings.Contains(err.Error(), "already holds") {
				t.Fatalf("unexpected refusal: %v", err)
			}
			return
		}
	}
	t.Fatalf("%d more open items were accepted; want a refusal at %d", externalMaxOpen, externalMaxOpen)
}

func TestAnInboxAskIsValidatedAndSanitised(t *testing.T) {
	srv, st, pids := apiFixture(t, bothTokens)
	pid := pids[0]
	long := func(n int) string { return strings.Repeat("a", n) }

	bad := []struct{ name, body, code string }{
		{"missing id", `{"question":"q"}`, "bad-external-id"},
		{"id with a space", `{"externalId":"a b","question":"q"}`, "bad-external-id"},
		{"id with a slash", `{"externalId":"a/b","question":"q"}`, "bad-external-id"},
		{"id of dots", `{"externalId":"..","question":"q"}`, "bad-external-id"},
		{"id too long", `{"externalId":"` + long(81) + `","question":"q"}`, "bad-external-id"},
		{"no question", `{"externalId":"a"}`, "bad-question"},
		{"blank question", `{"externalId":"a","question":"  \n "}`, "bad-question"},
		{"question too long", `{"externalId":"a","question":"` + long(501) + `"}`, "bad-question"},
		{"context too long", `{"externalId":"a","question":"q","context":"` + long(4001) + `"}`, "bad-context"},
		{"title too long", `{"externalId":"a","question":"q","title":"` + long(121) + `"}`, "bad-title"},
		{"one option", `{"externalId":"a","question":"q","options":[{"label":"Yes"}]}`, "bad-options"},
		{"four options", `{"externalId":"a","question":"q","options":[{"label":"1"},{"label":"2"},{"label":"3"},{"label":"4"}]}`, "bad-options"},
		{"blank option label", `{"externalId":"a","question":"q","options":[{"label":" "},{"label":"No"}]}`, "bad-options"},
		{"option label too long", `{"externalId":"a","question":"q","options":[{"label":"` + long(61) + `"},{"label":"No"}]}`, "bad-options"},
		{"option description too long", `{"externalId":"a","question":"q","options":[{"label":"Y","description":"` + long(201) + `"},{"label":"N"}]}`, "bad-options"},
		{"expiry zero", `{"externalId":"a","question":"q","expiresInHours":0}`, "bad-expiry"},
		{"expiry beyond 720", `{"externalId":"a","question":"q","expiresInHours":721}`, "bad-expiry"},
		{"an unknown field", `{"externalId":"a","question":"q","productId":"p-1"}`, "bad-json"},
		{"not JSON", `question=q`, "bad-json"},
		{"a secret in the context", `{"externalId":"a","question":"q","context":"token ` + tokQA + `"}`, "secret-in-body"},
		// A bidi mark splits the token so the raw-body screen misses it; cleaning joins it again.
		{"a secret split by a bidi mark", `{"externalId":"a","question":"q","context":"token ` + tokQA[:9] + `\u202a` + tokQA[9:] + `"}`, "secret-in-body"},
	}
	for _, tc := range bad {
		rec := call(t, srv, "POST", inboxPath(pid), tokQA, tc.body)
		var e map[string]string
		_ = json.Unmarshal(rec.Body.Bytes(), &e)
		if rec.Code != 400 || e["error"] != tc.code {
			t.Errorf("%s: answered %d %q, want 400 %q", tc.name, rec.Code, e["error"], tc.code)
		}
	}
	// 1 MiB cap.
	if rec := call(t, srv, "POST", inboxPath(pid), tokQA, `{"externalId":"a","question":"`+strings.Repeat("a", 1<<20)+`"}`); rec.Code != 413 {
		t.Errorf("an oversized body answered %d, want 413", rec.Code)
	}
	if len(st.externalItemsLocked()) != 0 {
		t.Fatalf("%d item(s) exist after only refused asks; a refusal writes nothing", len(st.externalItemsLocked()))
	}

	// Sanitising: control characters and bidi overrides go, the edges are
	// trimmed, line breaks normalise, the options default.
	code, _ := mustAsk(t, srv, pid, `{"externalId":"clean","title":"  Two\u0000   spaces‮ ","question":"  Line one\r\nLine two\u0007 ‏","context":"\u0001ctx\tkept"}`)
	if code != 201 {
		t.Fatalf("a body with control characters answered %d, want 201 (cleaned)", code)
	}
	x, err := st.ExternalGet(t.Context(), pid, "clean")
	if err != nil {
		t.Fatal(err)
	}
	if x.Question != "Line one\nLine two" || x.Context != "ctx\tkept" || x.Title != "Two spaces" {
		t.Errorf("stored question %q context %q title %q; want them cleaned of control and bidi characters", x.Question, x.Context, x.Title)
	}
	if len(x.Options) != 3 || x.Options[0].Label != "Approve" || x.Options[1].Label != "Reject" || x.Options[2].Label != "Needs changes" {
		t.Errorf("default options %+v; want Approve, Reject, Needs changes", x.Options)
	}
	if want := x.AskedAt + 168*3600; x.Deadline != want {
		t.Errorf("default deadline %d, want asked + 168h = %d", x.Deadline, want)
	}
	if code, _ := mustAsk(t, srv, pid, `{"externalId":"hours","question":"q","expiresInHours":720,"options":[{"label":"Yes","description":"  go "},{"label":"No"}]}`); code != 201 {
		t.Fatalf("expiresInHours 720 and two options answered %d, want 201", code)
	}
	y, _ := st.ExternalGet(t.Context(), pid, "hours")
	if y.Deadline != y.AskedAt+720*3600 || len(y.Options) != 2 || y.Options[0].Description != "go" {
		t.Errorf("expiry %d / options %+v; want 720h and the two given options, trimmed", y.Deadline-y.AskedAt, y.Options)
	}
}

func TestTheQAToolsQuestionIsAnsweredFromTheInbox(t *testing.T) {
	srv, st, pids := apiFixture(t, bothTokens)
	pid := pids[0]
	if code, _ := mustAsk(t, srv, pid, askJSON("ui-1")); code != 201 {
		t.Fatal("the ask failed")
	}

	// The chip and the nav count see it, with no task anywhere.
	body := do(t, srv, "GET", "/ui/needs", nil, boCookies()...).Body.String()
	if !strings.Contains(body, `aria-label="1 agent needs your answer"`) {
		t.Fatalf("the chip after a QA tool question = %q; want it to count 1", body)
	}
	// The inbox row carries the source tag and the title, and opens the QA sheet.
	w := do(t, srv, "GET", "/ui/bo/inbox", nil, boCookies()...)
	html := w.Body.String()
	for _, want := range []string{`class="ir-src">QA tool<`, "Merge the flaky-test quarantine?", "Quarantine",
		`data-answer-open="ui/answer-ext/` + pid + `/ui-1?from=inbox"`, `id="inbox-` + pid + `-ext-ui-1"`} {
		if !strings.Contains(html, want) {
			t.Errorf("the inbox lacks %q", want)
		}
	}
	// The sheet: three options, none preselected, posting to the external route.
	w = do(t, srv, "GET", "/ui/answer-ext/"+pid+"/ui-1?from=inbox", nil, boCookies()...)
	sheet := w.Body.String()
	if w.Code != 200 || !strings.Contains(sheet, `hx-post="products/`+pid+`/inbox/ui-1/answer"`) ||
		strings.Contains(sheet, "checked") || strings.Contains(sheet, `class="rec"`) ||
		!strings.Contains(sheet, "<b dir=\"auto\">Approve</b>") || !strings.Contains(sheet, `value="4"`) {
		t.Fatalf("the QA sheet answered %d and is not three unchecked options with the 4th and the external post URL", w.Code)
	}
	if w := do(t, srv, "GET", "/ui/answer-ext/"+pid+"/ui-1", nil); w.Code != 401 {
		t.Errorf("the QA sheet without a session answered %d, want 401", w.Code)
	}
	if w := do(t, srv, "GET", "/ui/answer-ext/"+pid+"/nope", nil, boCookies()...); w.Code != 422 {
		t.Errorf("the QA sheet of an unknown item answered %d, want 422", w.Code)
	}

	post := func(ext, body string, signed bool) *httptest.ResponseRecorder {
		return answerPost(t, srv, "/products/"+pid+"/inbox/"+ext+"/answer", body, signed)
	}
	for _, tc := range []struct {
		name, ext, body string
		signed          bool
		want            int
	}{
		{"no session", "ui-1", `{"choice":1}`, false, 401},
		{"no choice", "ui-1", `{}`, true, 422},
		{"own words, empty", "ui-1", `{"choice":4,"text":" "}`, true, 422},
		{"an option nobody offered", "ui-1", `{"choice":5}`, true, 422},
		{"unknown item", "nope", `{"choice":1}`, true, 404},
	} {
		if w := post(tc.ext, tc.body, tc.signed); w.Code != tc.want {
			t.Errorf("%s: answered %d %s, want %d", tc.name, w.Code, w.Body, tc.want)
		}
	}
	if _, x := getState(t, srv, pid, "ui-1"); x.Status != "open" {
		t.Fatalf("refused answers changed the item to %q", x.Status)
	}

	// A real answer through the form route the sheet posts to.
	w = do(t, srv, "POST", "/products/"+pid+"/inbox/ui-1/answer", url.Values{"choice": {"3"}, "text": {"after the freeze"}}, boCookies()...)
	if body := w.Body.String(); w.Code != 200 || !strings.Contains(body, `data-view="inbox"`) ||
		!strings.Contains(body, "Answer recorded") || strings.Contains(body, `id="inbox-`+pid+`-ext-ui-1"`) {
		t.Fatalf("answering from the inbox answered %d; want the inbox back, the toast, and the row gone", w.Code)
	}
	code, x := getState(t, srv, pid, "ui-1")
	if code != 200 || x.Status != "answered" || x.Choice != 3 || x.OptionLabel != "Needs changes" || x.Text != "after the freeze" || x.AnsweredAt == 0 {
		t.Fatalf("GET after the answer = %d %+v; want answered, choice 3, Needs changes, the note and a time", code, x)
	}
	if w := post("ui-1", `{"choice":1}`, true); w.Code != 409 {
		t.Errorf("a second answer answered %d, want 409", w.Code)
	}
	body = do(t, srv, "GET", "/ui/needs", nil, boCookies()...).Body.String()
	if strings.Contains(body, "needs your answer") {
		t.Errorf("the chip after the answer = %q; want no waiting count", body)
	}

	// Option 4 and the JSON answer.
	mustAsk(t, srv, pid, askJSON("ui-2"))
	if w := post("ui-2", `{"choice":4,"text":"Quarantine only the two slowest"}`, true); w.Code != 200 || !strings.Contains(w.Body.String(), "ExternalQuestionAnswered") {
		t.Fatalf("a JSON answer answered %d %s; want 200 with the event", w.Code, w.Body)
	}
	if _, x := getState(t, srv, pid, "ui-2"); x.Choice != 4 || x.OptionLabel != "" || x.Text != "Quarantine only the two slowest" {
		t.Errorf("the own-words answer reads back as %+v; want choice 4, no label, the text", x)
	}
	// A replayed floor refuses the answer, like every other mutation.
	mustAsk(t, srv, pid, askJSON("ui-3"))
	if _, err := (readOnly{st}).AnswerExternal(t.Context(), pid, "ui-3", 1, ""); err == nil {
		t.Error("the read-only handle accepted an answer")
	}
}

func TestTheInboxStatusReadsOpenAnsweredAndExpired(t *testing.T) {
	srv, st, pids := apiFixture(t, bothTokens)
	pid := pids[0]
	mustAsk(t, srv, pid, askJSON("s-open"))
	mustAsk(t, srv, pid, askJSON("s-done"))
	st.AnswerExternal(t.Context(), pid, "s-done", 1, "go")
	mustAsk(t, srv, pid, askJSON("s-late"))

	// One past its deadline whose expiry the sweep has not written yet reads expired.
	st.mu.Lock()
	for i := range st.events {
		if q := st.events[i].Payload.Question; q != nil && q.ExternalID == "s-late" {
			st.events[i].Payload.Question.Deadline = time.Now().Unix() - 5
		}
	}
	st.mu.Unlock()

	for _, tc := range []struct {
		ext, status string
		choice      int
		label       string
	}{
		{"s-open", "open", 0, ""},
		{"s-done", "answered", 1, "Approve"},
		{"s-late", "expired", 0, ""},
	} {
		code, x := getState(t, srv, pid, tc.ext)
		if code != 200 || x.ExternalID != tc.ext || x.Status != tc.status || x.Choice != tc.choice || x.OptionLabel != tc.label || x.AskedAt == 0 {
			t.Errorf("GET %s = %d %+v; want %s, choice %d %q", tc.ext, code, x, tc.status, tc.choice, tc.label)
		}
	}
	// The lapsed one is gone from the inbox and cannot be answered.
	if qs, _ := st.OpenQuestions(t.Context(), pid); len(qs) != 1 || qs[0].ExternalID != "s-open" {
		t.Errorf("the inbox holds %+v; want only s-open", qs)
	}
	if _, err := st.AnswerExternal(t.Context(), pid, "s-late", 1, ""); err == nil {
		t.Error("an item past its deadline accepted an answer")
	}
	// JSON keeps absent facts absent: an open item has no choice or answeredAt.
	rec := call(t, srv, "GET", inboxPath(pid, "s-open"), tokQA, "")
	if strings.Contains(rec.Body.String(), `"choice"`) || strings.Contains(rec.Body.String(), `"answeredAt"`) {
		t.Errorf("an open item's JSON %s carries choice or answeredAt", rec.Body)
	}
}

func TestAnUnansweredInboxItemExpires(t *testing.T) {
	st := NewMemStore(1)
	pid := st.desks[0].ID
	ctx := t.Context()
	now := time.Now().Unix()
	for _, a := range []ExternalAsk{
		{ExternalID: "short", Question: "q", ExpiresInHours: 1},
		{ExternalID: "long", Question: "q", ExpiresInHours: 100},
		{ExternalID: "default", Question: "q"},
	} {
		if _, _, _, err := st.AskExternal(ctx, pid, a); err != nil {
			t.Fatal(err)
		}
	}
	if r, _ := st.ExpireQuestions(ctx, now); len(r.NewEvents) != 0 {
		t.Fatalf("a sweep before any deadline wrote %d event(s)", len(r.NewEvents))
	}
	r, err := st.ExpireQuestions(ctx, now+2*3600)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.NewEvents) != 1 || r.NewEvents[0].Type != ExternalQuestionAnswered || r.NewEvents[0].Payload.Question.Status != ExtExpired {
		t.Fatalf("the sweep wrote %+v; want one ExternalQuestionAnswered with status expired", r.NewEvents)
	}
	if r.Floor.Desks != nil {
		t.Error("an external expiry carried a floor; it must not touch the desks (BR-20)")
	}
	short, _ := st.ExternalGet(ctx, pid, "short")
	if short.Status != ExtExpired || short.AnsweredAt != short.Deadline || short.Choice != 0 {
		t.Errorf("the expired item reads %+v; want expired, no choice, answeredAt = its deadline", short)
	}
	if _, err := st.AnswerExternal(ctx, pid, "short", 1, ""); err == nil {
		t.Error("an expired item accepted an answer")
	}
	if qs, _ := st.OpenQuestions(ctx, ""); len(qs) != 2 {
		t.Errorf("the inbox holds %d after the sweep; want the 2 still open", len(qs))
	}
	if again, _ := st.ExpireQuestions(ctx, now+2*3600); len(again.NewEvents) != 0 {
		t.Errorf("a second sweep wrote %d event(s); an item expires once", len(again.NewEvents))
	}
	// An answered item is never expired afterwards.
	if _, err := st.AnswerExternal(ctx, pid, "long", 2, ""); err != nil {
		t.Fatal(err)
	}
	if r, _ := st.ExpireQuestions(ctx, now+1000*3600); len(r.NewEvents) != 1 {
		t.Errorf("the late sweep wrote %d event(s); want 1, for the one still open", len(r.NewEvents))
	}
	if x, _ := st.ExternalGet(ctx, pid, "long"); x.Status != ExtAnswered {
		t.Errorf("an answered item reads %q after a late sweep; want answered", x.Status)
	}
	// Task-bound questions keep their own rule beside it.
	plantQuestion(st, pid, "wf-ext-exp", "Which cache?", now-int64(QuestionLifetime/time.Second)-1)
	if r, _ := st.ExpireQuestions(ctx, now); len(r.NewEvents) != 1 || r.NewEvents[0].Payload.Rule != MoveQuestionExpired {
		t.Errorf("a task question's expiry wrote %+v; want the one question-expired move", r.NewEvents)
	}
}

func TestAnInboxItemSurvivesARestart(t *testing.T) {
	st := NewMemStore(1)
	pid := st.desks[0].ID
	ctx := t.Context()
	for _, id := range []string{"r-open", "r-done", "r-gone", "r-sent", "r-live"} {
		hours := 5
		if id == "r-live" {
			hours = 100
		}
		if _, _, _, err := st.AskExternal(ctx, pid, ExternalAsk{ExternalID: id, Question: "q " + id, Title: "T " + id,
			Options: []QuestionOption{{Label: "Yes"}, {Label: "No"}}, ExpiresInHours: hours}); err != nil {
			t.Fatal(err)
		}
	}
	st.AnswerExternal(ctx, pid, "r-done", 2, "because")
	st.AnswerExternal(ctx, pid, "r-sent", 1, "")
	st.RecordExternalDelivery(ctx, pid, "r-sent", 1, DeliveryDelivered, "", time.Now().Unix())
	st.RecordExternalDelivery(ctx, pid, "r-done", 1, "retry", "HTTP 503", 1234)
	st.ExpireQuestions(ctx, time.Now().Unix()+6*3600) // r-open and r-gone expire

	n := restartFrom(t, st)
	before, _ := st.ExternalPending(ctx)
	after, _ := n.ExternalPending(ctx)
	if len(after) != 3 || len(before) != len(after) {
		t.Fatalf("after the restart %d item(s) await delivery, before it %d; want the same 3", len(after), len(before))
	}
	for _, id := range []string{"r-open", "r-done", "r-gone", "r-sent", "r-live"} {
		a, errA := st.ExternalGet(ctx, pid, id)
		b, errB := n.ExternalGet(ctx, pid, id)
		if errA != nil || errB != nil {
			t.Fatalf("%s: %v / %v", id, errA, errB)
		}
		if a.Status != b.Status || a.Choice != b.Choice || a.Text != b.Text || a.AnsweredAt != b.AnsweredAt ||
			a.Deadline != b.Deadline || a.ItemID != b.ItemID || a.Delivery != b.Delivery || len(a.Options) != len(b.Options) {
			t.Errorf("%s reads %+v before the restart and %+v after", id, a, b)
		}
	}
	d, _ := n.ExternalGet(ctx, pid, "r-done")
	if d.Delivery.Attempts != 1 || d.Delivery.State != DeliveryPending || d.Delivery.LastError != "HTTP 503" || d.Delivery.LastAt != 1234 {
		t.Errorf("r-done's delivery after the restart = %+v; want attempt 1 pending with its error and time", d.Delivery)
	}
	if sent, _ := n.ExternalGet(ctx, pid, "r-sent"); sent.Delivery.State != DeliveryDelivered {
		t.Errorf("a delivered answer reads %q after the restart; want delivered, so it is not sent twice", sent.Delivery.State)
	}
	// The inbox, the count and the idempotent ask all work on the restarted store.
	qs, _ := n.OpenQuestions(ctx, pid)
	if len(qs) != 1 || qs[0].ExternalID != "r-live" || !qs[0].External {
		t.Errorf("the restarted inbox holds %+v; want only r-live, still open", qs)
	}
	if _, created, _, err := n.AskExternal(ctx, pid, ExternalAsk{ExternalID: "r-done", Question: "again"}); err != nil || created {
		t.Errorf("asking r-done again after the restart: created=%v err=%v; want the existing item", created, err)
	}
	// The folded floor is unchanged by the new names.
	if a, b := st.foldTo(st.seq), n.foldTo(n.seq); len(a.Desks) != len(b.Desks) {
		t.Errorf("the fold has %d desks before and %d after the restart", len(a.Desks), len(b.Desks))
	}
}

// hook is a fake QA tool: it records each webhook it gets and answers with
// whatever status the test says.
type hook struct {
	mu     sync.Mutex
	got    []inboxAnswerHook
	auth   []string
	paths  []string
	status int
	block  chan struct{}
}

func (h *hook) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.block != nil {
		<-h.block
	}
	b, _ := io.ReadAll(r.Body)
	var in inboxAnswerHook
	_ = json.Unmarshal(b, &in)
	h.mu.Lock()
	h.got, h.auth, h.paths = append(h.got, in), append(h.auth, r.Header.Get("Authorization")), append(h.paths, r.URL.Path+" "+r.Method)
	status := h.status
	h.mu.Unlock()
	w.WriteHeader(status)
}

func (h *hook) n() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.got)
}

func hookWorker(t *testing.T, st Store, h *hook) (*inboxWorker, *httptest.Server) {
	t.Helper()
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	out := newQaOutbound(ts.URL, tokOut, nil)
	if out.why != "" {
		t.Fatalf("the fake QA tool's URL was refused: %s", out.why)
	}
	return newInboxWorker(st, out), ts
}

func TestTheAnswerWebhookDeliversTheDecision(t *testing.T) {
	st := NewMemStore(1)
	pid := st.desks[0].ID
	ctx := t.Context()
	h := &hook{status: 200}
	w, _ := hookWorker(t, st, h)

	opts := []QuestionOption{{Label: "Approve"}, {Label: "Reject"}}
	for _, id := range []string{"w-pick", "w-words", "w-expire", "w-open"} {
		if _, _, _, err := st.AskExternal(ctx, pid, ExternalAsk{ExternalID: id, Question: "q", Options: opts, ExpiresInHours: 1}); err != nil {
			t.Fatal(err)
		}
	}
	if n := w.RunOnce(ctx, time.Now().Unix()); n != 0 {
		t.Fatalf("a pass over open items made %d call(s); nothing is decided yet", n)
	}
	st.AnswerExternal(ctx, pid, "w-pick", 2, "")
	st.AnswerExternal(ctx, pid, "w-words", 4, "ship on Monday")
	st.ExpireQuestions(ctx, time.Now().Unix()+3601) // the two still open, w-expire and w-open, expire
	if n := w.RunOnce(ctx, time.Now().Unix()); n != 4 {
		t.Fatalf("the pass delivered %d item(s); want 4 (two answers, two expiries)", n)
	}
	byID := map[string]inboxAnswerHook{}
	for _, g := range h.got {
		byID[g.ExternalID] = g
	}
	if g := byID["w-pick"]; g.Status != "answered" || g.Choice != 2 || g.OptionLabel != "Reject" || g.ProductID != pid || g.AnsweredAt == 0 || g.Text != "" {
		t.Errorf("w-pick was sent as %+v; want answered, 2, Reject, the product and a time", g)
	}
	if g := byID["w-words"]; g.Choice != 4 || g.OptionLabel != "" || g.Text != "ship on Monday" {
		t.Errorf("w-words was sent as %+v; want choice 4 and the text", g)
	}
	if g := byID["w-expire"]; g.Status != "expired" || g.Choice != 0 || g.AnsweredAt == 0 {
		t.Errorf("w-expire was sent as %+v; want expired, choice 0", g)
	}
	for i, a := range h.auth {
		if a != "Bearer "+tokOut {
			t.Errorf("call %d carried Authorization %q; want the outbound token", i, a)
		}
		if h.paths[i] != "/api/v1/gaos/inbox-answer POST" {
			t.Errorf("call %d was %q; want POST /api/v1/gaos/inbox-answer", i, h.paths[i])
		}
	}
	// Delivered is delivered: a later pass, and a restart, send nothing.
	if n := w.RunOnce(ctx, time.Now().Unix()+86400); n != 0 {
		t.Errorf("a second pass made %d call(s); a delivered answer is sent once", n)
	}
	rw := newInboxWorker(restartFrom(t, st), w.out)
	if n := rw.RunOnce(ctx, time.Now().Unix()+86400); n != 0 {
		t.Errorf("after a restart the worker made %d call(s); the delivery events say all are done", n)
	}
	for _, e := range st.events {
		if e.Type == ExternalAnswerDelivery && (e.Payload.Rule != DeliveryDelivered || e.Payload.Question.Attempt != 1) {
			t.Errorf("a delivery event reads %+v; want delivered on attempt 1", e.Payload)
		}
	}
	// No outbound configured: nothing is attempted and nothing is recorded.
	idle := newInboxWorker(st, newQaOutbound("", "", nil))
	st.AskExternal(ctx, pid, ExternalAsk{ExternalID: "w-late", Question: "q", Options: opts})
	st.AnswerExternal(ctx, pid, "w-late", 1, "")
	seq := st.seq
	if n := idle.RunOnce(ctx, time.Now().Unix()); n != 0 || st.seq != seq {
		t.Errorf("an unconfigured worker made %d call(s) and wrote %d event(s); want none", n, st.seq-seq)
	}
	// An outbound URL the QA sync would refuse is refused here too.
	if o := newQaOutbound("http://example.com", tokOut, nil); o.why == "" {
		t.Error("plain http to a public host was accepted for the webhook")
	}
}

func TestTheAnswerWebhookRetriesWithBackoffThenGivesUp(t *testing.T) {
	st := NewMemStore(1)
	pid := st.desks[0].ID
	ctx := t.Context()
	h := &hook{status: 503}
	w, _ := hookWorker(t, st, h)
	st.AskExternal(ctx, pid, ExternalAsk{ExternalID: "x1", Question: "q", Options: []QuestionOption{{Label: "A"}, {Label: "B"}}})
	st.AnswerExternal(ctx, pid, "x1", 1, "")

	t0 := time.Now().Unix()
	// A fake clock: each row is when a pass runs and how many calls it makes.
	steps := []struct {
		name  string
		at    int64
		calls int
		total int
	}{
		{"first try, at once", t0, 1, 1},
		{"too soon after the first failure", t0 + 29, 0, 1},
		{"after 30 s", t0 + 30, 1, 2},
		{"too soon after the second", t0 + 30 + 119, 0, 2},
		{"after 2 min", t0 + 30 + 120, 1, 3},
		{"after 10 min", t0 + 30 + 120 + 600, 1, 4},
		{"too soon after the fourth", t0 + 30 + 120 + 600 + 3599, 0, 4},
		{"after 1 h, the fifth and last", t0 + 30 + 120 + 600 + 3600, 1, 5},
		{"never again", t0 + 30 + 120 + 600 + 3600 + 100000, 0, 5},
	}
	for _, s := range steps {
		if n := w.RunOnce(ctx, s.at); n != s.calls || h.n() != s.total {
			t.Fatalf("%s: the pass made %d call(s) (%d in all); want %d (%d in all)", s.name, n, h.n(), s.calls, s.total)
		}
	}
	x, _ := st.ExternalGet(ctx, pid, "x1")
	if x.Delivery.State != DeliveryGaveUp || x.Delivery.Attempts != 5 || !strings.Contains(x.Delivery.LastError, "HTTP 503") {
		t.Errorf("the delivery reads %+v; want gave-up after 5 attempts, with the last error", x.Delivery)
	}
	var rules []string
	for _, e := range st.events {
		if e.Type == ExternalAnswerDelivery {
			rules = append(rules, e.Payload.Rule)
		}
	}
	if strings.Join(rules, ",") != "retry,retry,retry,retry,gave-up" {
		t.Errorf("the delivery events are %v; want four retries then gave-up", rules)
	}
	// Polling still works, and says what was decided.
	if x.Status != ExtAnswered || x.Choice != 1 {
		t.Errorf("after giving up the item reads %+v; GET must still serve the answer", x)
	}

	// A restart mid-way resumes the count instead of starting over.
	h2 := &hook{status: 500}
	w2, _ := hookWorker(t, st, h2)
	st.AskExternal(ctx, pid, ExternalAsk{ExternalID: "x2", Question: "q", Options: []QuestionOption{{Label: "A"}, {Label: "B"}}})
	st.AnswerExternal(ctx, pid, "x2", 2, "")
	w2.RunOnce(ctx, t0)
	w2.RunOnce(ctx, t0+30)
	restarted, _ := hookWorker(t, restartFrom(t, st), h2)
	if n := restarted.RunOnce(ctx, t0+30+1); n != 0 {
		t.Errorf("a restarted worker retried 1 s after the last attempt (%d call(s)); the backoff must survive the restart", n)
	}
	if n := restarted.RunOnce(ctx, t0+30+120); n != 1 {
		t.Errorf("a restarted worker made %d call(s) when the third was due; want 1", n)
	}
	// A QA tool that recovers is told once.
	h3 := &hook{status: 500}
	w3, _ := hookWorker(t, st, h3)
	st.AskExternal(ctx, pid, ExternalAsk{ExternalID: "x3", Question: "q", Options: []QuestionOption{{Label: "A"}, {Label: "B"}}})
	st.AnswerExternal(ctx, pid, "x3", 1, "")
	w3.RunOnce(ctx, t0)
	h3.mu.Lock()
	h3.status = 204
	h3.mu.Unlock()
	if n := w3.RunOnce(ctx, t0+31); n != 1 {
		t.Fatalf("the retry after recovery made %d call(s), want 1", n)
	}
	if x3, _ := st.ExternalGet(ctx, pid, "x3"); x3.Delivery.State != DeliveryDelivered || x3.Delivery.Attempts != 2 {
		t.Errorf("x3's delivery reads %+v; want delivered on attempt 2", x3.Delivery)
	}
}

func TestAnAnswerNeverWaitsOnTheWebhook(t *testing.T) {
	srv, st, pids := apiFixture(t, bothTokens)
	pid := pids[0]
	h := &hook{status: 200, block: make(chan struct{})}
	w, _ := hookWorker(t, st, h)
	srv.inbox = w
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()
	t.Cleanup(func() {
		close(h.block) // unblock the handler before the server closes
		cancel()
		<-done
	})

	mustAsk(t, srv, pid, askJSON("nb-1"))
	start := time.Now()
	rec := answerPost(t, srv, "/products/"+pid+"/inbox/nb-1/answer", `{"choice":1}`, true)
	if rec.Code != 200 || time.Since(start) > 3*time.Second {
		t.Fatalf("the answer took %v and answered %d while the QA tool hangs; it must return at once", time.Since(start), rec.Code)
	}
	// The worker is stuck in the hung call, and the answer is recorded regardless.
	if _, x := getState(t, srv, pid, "nb-1"); x.Status != "answered" {
		t.Errorf("GET reads %q while the webhook hangs; want answered", x.Status)
	}
	// An unreachable QA tool does not break answering either.
	srv2, _, pids2 := apiFixture(t, bothTokens)
	srv2.inbox = newInboxWorker(srv2.store, newQaOutbound("http://127.0.0.1:1", tokOut, nil))
	mustAsk(t, srv2, pids2[0], askJSON("nb-2"))
	if rec := answerPost(t, srv2, "/products/"+pids2[0]+"/inbox/nb-2/answer", `{"choice":2}`, true); rec.Code != 200 {
		t.Errorf("answering with the QA tool down answered %d, want 200", rec.Code)
	}
	srv2.inbox.RunOnce(t.Context(), time.Now().Unix())
	if x, _ := srv2.store.ExternalGet(t.Context(), pids2[0], "nb-2"); x.Delivery.Attempts != 1 || x.Delivery.State != DeliveryPending || x.Delivery.LastError == "" {
		t.Errorf("a refused connection left the delivery %+v; want one failed attempt, still pending", x.Delivery)
	}
	var nilWorker *inboxWorker
	nilWorker.Kick() // a server with no worker (tests) must not panic
}

// caseExternalInbox is the QA tool's inbox through the seam, so both stores
// agree on it.
func caseExternalInbox(t *testing.T, s Store) {
	ctx := context.Background()
	pid := firstMasterID(t, s)
	ask := ExternalAsk{ExternalID: "conf-1", Question: "Ship?", Options: []QuestionOption{{Label: "Yes"}, {Label: "No"}}}
	x, created, r, err := s.AskExternal(ctx, pid, ask)
	if err != nil || !created || x.Status != ExtOpen || len(r.NewEvents) != 1 || r.NewEvents[0].Type != ExternalQuestionAsked {
		t.Fatalf("AskExternal = %+v created=%v events=%d err=%v; want a new open item and one ExternalQuestionAsked", x, created, len(r.NewEvents), err)
	}
	if _, created, r, err := s.AskExternal(ctx, pid, ask); err != nil || created || len(r.NewEvents) != 0 {
		t.Fatalf("a second AskExternal created=%v events=%d err=%v; want the same item and no event", created, len(r.NewEvents), err)
	}
	if _, _, _, err := s.AskExternal(ctx, "p-nope", ask); err == nil {
		t.Error("AskExternal on an unknown product succeeded")
	}
	qs, _ := s.OpenQuestions(ctx, pid)
	if len(qs) != 1 || !qs[0].External || qs[0].ExternalID != "conf-1" {
		t.Fatalf("OpenQuestions = %+v; want the one external item", qs)
	}
	if _, err := s.AnswerExternal(ctx, pid, "conf-1", 3, ""); err == nil {
		t.Error("an option nobody offered was accepted")
	}
	if _, err := s.AnswerExternal(ctx, pid, "conf-1", 2, "n"); err != nil {
		t.Fatalf("AnswerExternal: %v", err)
	}
	if _, err := s.AnswerExternal(ctx, pid, "conf-1", 1, ""); err == nil {
		t.Error("a second answer was accepted")
	}
	got, err := s.ExternalGet(ctx, pid, "conf-1")
	if err != nil || got.Status != ExtAnswered || got.Choice != 2 || got.OptionLabel() != "No" || got.Text != "n" {
		t.Fatalf("ExternalGet = %+v err=%v; want answered, 2, No, n", got, err)
	}
	if _, err := s.ExternalGet(ctx, pid, "nope"); err == nil {
		t.Error("ExternalGet of an unknown id succeeded")
	}
	pend, _ := s.ExternalPending(ctx)
	if len(pend) != 1 {
		t.Fatalf("ExternalPending = %d; want the answered item", len(pend))
	}
	if _, err := s.RecordExternalDelivery(ctx, pid, "conf-1", 1, "sideways", "", 1); err == nil {
		t.Error("an unknown delivery outcome was accepted")
	}
	if _, err := s.RecordExternalDelivery(ctx, pid, "conf-1", 1, DeliveryDelivered, "", 1); err != nil {
		t.Fatal(err)
	}
	if pend, _ := s.ExternalPending(ctx); len(pend) != 0 {
		t.Errorf("ExternalPending = %d after delivery; want 0", len(pend))
	}
}

// failingRecord makes the delivery record fail, as a database outage would.
type failingRecord struct{ Store }

func (failingRecord) RecordExternalDelivery(context.Context, string, string, int, string, string, int64) (Result, error) {
	return Result{}, errors.New("database unavailable")
}

func TestAWebhookWhoseRecordFailsIsNotResentEveryTick(t *testing.T) {
	st := NewMemStore(1)
	pid := st.desks[0].ID
	ctx := t.Context()
	h := &hook{status: 200}
	w, _ := hookWorker(t, failingRecord{st}, h)
	st.AskExternal(ctx, pid, ExternalAsk{ExternalID: "r1", Question: "q", Options: []QuestionOption{{Label: "A"}, {Label: "B"}}})
	st.AnswerExternal(ctx, pid, "r1", 1, "")

	t0 := time.Now().Unix()
	if n := w.RunOnce(ctx, t0); n != 1 {
		t.Fatalf("the first pass made %d calls; want 1", n)
	}
	for i := int64(1); i <= 20; i++ { // ten-second ticks for 200 s: the 30 s backoff must hold
		if i*10 < 30 {
			if n := w.RunOnce(ctx, t0+i*10); n != 0 {
				t.Fatalf("at +%d s the pass re-sent (%d calls); the failed record must still back off", i*10, n)
			}
		}
	}
	if n := w.RunOnce(ctx, t0+30); n != 1 {
		t.Fatalf("after the backoff the pass made %d calls; want 1", n)
	}
}
