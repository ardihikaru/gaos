package main

import (
	"bytes"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// D-083, docs/designs/answer-source.md: a machine client answers the inbox
// items it asked, and every answer records where it came from.

const (
	tokHikaru = "gaos_hhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhh"
	tokTwo    = "gaos_iiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiii"
	tokNone   = "gaos_jjjjjjjjjjjjjjjjjjjjjjjjjjjjjjjjjjjjjjjjjjjjjjjjjjjjjjjjjjjjjjjj"
)

// qaWithOrigin is bothTokens with the QA tool's origin set, as apiFromEnv does.
func qaWithOrigin(pids []string) *apiConfig {
	c := bothTokens(pids)
	c.setQAOrigin("qa.example")
	return c
}

func answerPath(pid, ext string) string { return inboxPath(pid, ext) + "/answer" }

func apiAnswerJSON(choice int, text, user string) string {
	b, _ := json.Marshal(map[string]any{"choice": choice, "text": text, "actor": map[string]string{"username": user}})
	return string(b)
}

// decodeState reads a GET-shaped body, with the error fields when present.
type stateWithErr struct {
	apiInboxState
	Error  string `json:"error"`
	Reason string `json:"reason"`
}

func decodeState(t *testing.T, b []byte) stateWithErr {
	t.Helper()
	var out stateWithErr
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("the body is not JSON: %s", b)
	}
	return out
}

func TestAPIInboxAnswerFromTheQATool(t *testing.T) {
	srv, st, pids := apiFixture(t, qaWithOrigin)
	pid := pids[0]
	mustAsk(t, srv, pid, askJSON("hist-1"))

	var logs bytes.Buffer
	log.SetOutput(&logs)
	defer log.SetOutput(os.Stderr)
	rec := call(t, srv, "POST", answerPath(pid, "hist-1"), tokQA,
		`{"choice":1,"text":"ok","actor":{"username":"ardi","display":"Ardi H"}}`, "X-Forwarded-For", "203.0.113.7, 10.10.0.1")
	if rec.Code != 200 {
		t.Fatalf("the QA tool's answer answered %d %s; want 200", rec.Code, rec.Body)
	}
	got := decodeState(t, rec.Body.Bytes())
	want := apiSource{Kind: "qa-tool", Client: "qa-tools", Origin: "qa.example", Actor: "ardi"}
	if got.Status != "answered" || got.Choice != 1 || got.OptionLabel != "Approve" || got.Source == nil || *got.Source != want {
		t.Fatalf("the answer body is %+v; want answered, choice 1, Approve, source %+v", got, want)
	}
	var ev *Event
	for i := range st.events {
		if st.events[i].Type == ExternalQuestionAnswered {
			ev = &st.events[i]
		}
	}
	if ev == nil || ev.Payload.Question.AnsweredBy == nil || ev.Payload.Actor != "qa-tools/ardi" ||
		!strings.HasSuffix(ev.Text, "— via QA tool (qa.example) by ardi") {
		t.Fatalf("the answer event is %+v; want answeredBy, actor qa-tools/ardi and the phrase in its text", ev)
	}
	if strings.Contains(rec.Body.String(), "Ardi H") || strings.Contains(string(mustJSON(t, st.events)), "Ardi H") {
		t.Error("actor.display was stored or returned; it is accepted and dropped")
	}
	if pend, _ := st.ExternalPending(t.Context()); len(pend) != 1 || pend[0].ExternalID != "hist-1" {
		t.Errorf("ExternalPending = %+v; the QA tool's own answer is still owed to its webhook", pend)
	}
	if _, x := getState(t, srv, pid, "hist-1"); x.Source == nil || *x.Source != want {
		t.Errorf("GET reads source %+v; want %+v", x.Source, want)
	}
	if !strings.Contains(logs.String(), `api: inbox answer `+pid+`/hist-1 by qa-tools peer=`) ||
		!strings.Contains(logs.String(), `xff="203.0.113.7, 10.10.0.1"`) {
		t.Errorf("the server log reads %q; want the answer line with the forwarded chain", logs.String())
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestAPIInboxAnswerFirstWins(t *testing.T) {
	srv, st, pids := apiFixture(t, qaWithOrigin)
	pid := pids[0]

	mustAsk(t, srv, pid, askJSON("fw-1"))
	first := call(t, srv, "POST", answerPath(pid, "fw-1"), tokQA, apiAnswerJSON(2, "not yet", "ardi"))
	if first.Code != 200 {
		t.Fatalf("the first answer answered %d %s", first.Code, first.Body)
	}
	n := len(st.events)
	again := call(t, srv, "POST", answerPath(pid, "fw-1"), tokQA, apiAnswerJSON(2, "  not yet ", "ardi"))
	if again.Code != 200 || decodeState(t, again.Body.Bytes()).AnsweredAt != decodeState(t, first.Body.Bytes()).AnsweredAt || len(st.events) != n {
		t.Fatalf("an identical retry answered %d %s and wrote %d event(s); want 200 with the first answer and nothing written",
			again.Code, again.Body, len(st.events)-n)
	}
	for _, body := range []string{apiAnswerJSON(1, "", "ardi"), apiAnswerJSON(2, "not yet", "budi")} {
		rec := call(t, srv, "POST", answerPath(pid, "fw-1"), tokQA, body)
		got := decodeState(t, rec.Body.Bytes())
		if rec.Code != 409 || got.Error != "already_answered" || got.Source == nil || got.Source.Actor != "ardi" || got.Choice != 2 ||
			got.Reason != "That question was already answered via QA tool (qa.example) by ardi." {
			t.Errorf("a different second answer answered %d %s; want 409 already_answered carrying the first", rec.Code, rec.Body)
		}
	}

	// The operator first, then the QA tool: the API never hands out the email.
	mustAsk(t, srv, pid, askJSON("fw-2"))
	if w := answerPost(t, srv, "/products/"+pid+"/inbox/fw-2/answer", `{"choice":1}`, true); w.Code != 200 {
		t.Fatalf("the back-office answer answered %d", w.Code)
	}
	rec := call(t, srv, "POST", answerPath(pid, "fw-2"), tokQA, apiAnswerJSON(1, "", "ardi"))
	got := decodeState(t, rec.Body.Bytes())
	if rec.Code != 409 || got.Source == nil || got.Source.Kind != "gaos" || got.Source.Actor != "operator" ||
		strings.Contains(rec.Body.String(), testEmail) {
		t.Fatalf("the QA tool after the operator answered %d %s; want 409 naming the operator, never the account", rec.Code, rec.Body)
	}
	if _, x := getState(t, srv, pid, "fw-2"); x.Source == nil || x.Source.Actor != "operator" {
		t.Errorf("GET of an operator's answer reads source %+v; want actor operator", x.Source)
	}

	// The QA tool first, then the operator: the toast names who answered.
	mustAsk(t, srv, pid, askJSON("fw-3"))
	call(t, srv, "POST", answerPath(pid, "fw-3"), tokQA, apiAnswerJSON(1, "", "ardi"))
	w := answerPost(t, srv, "/products/"+pid+"/inbox/fw-3/answer", `{"choice":2}`, true)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "already answered via QA tool (qa.example) by ardi") {
		t.Errorf("the operator after the QA tool answered %d %s; want 409 naming the QA tool's answer", w.Code, w.Body)
	}
}

func TestAPIInboxAnswerRefusals(t *testing.T) {
	srv, st, pids := apiFixture(t, qaWithOrigin)
	pid := pids[0]
	for _, id := range []string{"rf-1", "rf-old"} {
		mustAsk(t, srv, pid, askJSON(id))
	}
	// rf-old is past its deadline and swept; rf-lap2 is past it and not swept yet.
	st.mu.Lock()
	for i := range st.events {
		if q := st.events[i].Payload.Question; st.events[i].Type == ExternalQuestionAsked && q.ExternalID == "rf-old" {
			q.Deadline = time.Now().Unix() - 60
		}
	}
	st.expireExternalLocked(time.Now().Unix())
	st.mu.Unlock()
	mustAsk(t, srv, pid, askJSON("rf-lap2"))
	st.mu.Lock()
	for i := range st.events {
		if q := st.events[i].Payload.Question; st.events[i].Type == ExternalQuestionAsked && q.ExternalID == "rf-lap2" {
			q.Deadline = time.Now().Unix() - 1
		}
	}
	st.mu.Unlock()

	split := "gaos_ZZZZZZZZZZZZZZZZ‎ZZZZZZZZZZZZZZZZZZZZ"
	for _, c := range []struct {
		name, path, token, body string
		want                    int
		code                    string
	}{
		{"no token", answerPath(pid, "rf-1"), "", apiAnswerJSON(1, "", "ardi"), 401, "unauthorized"},
		{"the admin token", answerPath(pid, "rf-1"), tokAdmin, apiAnswerJSON(1, "", "ardi"), 403, "forbidden"},
		{"a product out of scope", answerPath(pids[1], "rf-1"), tokQA, apiAnswerJSON(1, "", "ardi"), 404, "not-found"},
		{"an unknown id", answerPath(pid, "nope"), tokQA, apiAnswerJSON(1, "", "ardi"), 404, "not-found"},
		{"expired", answerPath(pid, "rf-old"), tokQA, apiAnswerJSON(1, "", "ardi"), 410, "expired"},
		{"lapsed, not swept", answerPath(pid, "rf-lap2"), tokQA, apiAnswerJSON(1, "", "ardi"), 410, "expired"},
		{"choice 5", answerPath(pid, "rf-1"), tokQA, apiAnswerJSON(5, "", "ardi"), 422, "bad-choice"},
		{"own words, empty", answerPath(pid, "rf-1"), tokQA, apiAnswerJSON(4, " ", "ardi"), 422, "bad-choice"},
		{"text too long", answerPath(pid, "rf-1"), tokQA, apiAnswerJSON(4, strings.Repeat("x", 4001), "ardi"), 422, "bad-text"},
		{"no actor", answerPath(pid, "rf-1"), tokQA, `{"choice":1}`, 400, "bad-actor"},
		{"a long actor", answerPath(pid, "rf-1"), tokQA, apiAnswerJSON(1, "", strings.Repeat("a", 65)), 400, "bad-actor"},
		{"an actor with a space", answerPath(pid, "rf-1"), tokQA, apiAnswerJSON(1, "", "ardi h"), 400, "bad-actor"},
		{"the actor operator", answerPath(pid, "rf-1"), tokQA, apiAnswerJSON(1, "", "Operator"), 400, "bad-actor"},
		{"a back-office account", answerPath(pid, "rf-1"), tokQA, apiAnswerJSON(1, "", testEmail), 400, "bad-actor"},
		{"a secret in the text", answerPath(pid, "rf-1"), tokQA, apiAnswerJSON(4, "use "+tokTwo, "ardi"), 400, "secret-in-body"},
		{"a secret as the actor", answerPath(pid, "rf-1"), tokQA, apiAnswerJSON(1, "", tokTwo), 400, "secret-in-body"},
		{"a secret split by a bidi mark", answerPath(pid, "rf-1"), tokQA, apiAnswerJSON(4, split, "ardi"), 400, "secret-in-body"},
		{"an unknown field", answerPath(pid, "rf-1"), tokQA, `{"choice":1,"actor":{"username":"a"},"source":{"kind":"gaos"}}`, 400, "bad-json"},
	} {
		rec := call(t, srv, "POST", c.path, c.token, c.body)
		got := decodeState(t, rec.Body.Bytes())
		if rec.Code != c.want || got.Error != c.code {
			t.Errorf("%s: answered %d %s; want %d %s", c.name, rec.Code, rec.Body, c.want, c.code)
		}
		if c.want == 410 && got.Status != "expired" {
			t.Errorf("%s: the 410 body says status %q; want expired", c.name, got.Status)
		}
	}
	// An unoffered option: the ask offered two.
	mustAsk(t, srv, pid, `{"externalId":"rf-two","question":"Ship?","options":[{"label":"Yes"},{"label":"No"}]}`)
	if rec := call(t, srv, "POST", answerPath(pid, "rf-two"), tokQA, apiAnswerJSON(3, "", "ardi")); rec.Code != 422 {
		t.Errorf("an unoffered option answered %d %s; want 422", rec.Code, rec.Body)
	}
	srv.api.qaBusy.Store(true)
	if rec := call(t, srv, "POST", answerPath(pid, "rf-1"), tokQA, apiAnswerJSON(1, "", "ardi")); rec.Code != 429 {
		t.Errorf("an answer while the QA tool's write is in flight answered %d; want 429", rec.Code)
	}
	srv.api.qaBusy.Store(false)
	if _, x := getState(t, srv, pid, "rf-1"); x.Status != "open" {
		t.Fatalf("refused answers changed rf-1 to %q", x.Status)
	}
}

// tokenFile writes a token file with the given mode and returns its path.
func tokenFile(t *testing.T, dir, name, tok string, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(tok+"\n"), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAPIClientRegistry(t *testing.T) {
	dir := t.TempDir()
	good := tokenFile(t, dir, "hikaru", tokHikaru, 0o600)
	two := tokenFile(t, dir, "two", tokTwo, 0o600)
	none := tokenFile(t, dir, "none", tokNone, 0o600)
	open := tokenFile(t, dir, "open", "gaos_"+strings.Repeat("k", 40), 0o640)
	dup := tokenFile(t, dir, "dup", "gaos_"+strings.Repeat("m", 40), 0o600)
	asQA := tokenFile(t, dir, "asqa", tokQA, 0o600)
	asOut := tokenFile(t, dir, "asout", tokOut, 0o600)
	other := tokenFile(t, dir, "other", "gaos_"+strings.Repeat("n", 40), 0o600)

	srv, st, pids := apiFixture(t, qaWithOrigin)
	qaP, cliP := pids[0], pids[1]
	file := strings.Join([]string{
		"# name kind origin token products",
		"hikaru-app hikaru-app app.hikaru.example " + good + " " + cliP,
		"hikaru-two hikaru-two - " + two + " " + cliP,
		"hikaru-none hikaru-none - " + none + " -",
		"gaos k1 a.example " + other + " " + cliP,          // reserved name
		"cli-kind qa-tool a.example " + other + " " + cliP, // reserved kind
		"hikaru-app k2 b.example " + dup + " " + cliP,      // duplicate name
		"bad-origin k3 bad_host! " + other + " " + cliP,    // bad origin
		"same-origin k4 QA.Example " + other + " " + cliP,  // the QA tool's origin
		"open-file k5 c.example " + open + " " + cliP,      // readable by others
		"no-file k6 d.example " + filepath.Join(dir, "missing") + " " + cliP,
		"as-qa k7 e.example " + asQA + " " + cliP,                // the QA token
		"as-out k8 f.example " + asOut + " " + cliP,              // the outbound token
		"overlap k9 g.example " + other + " " + qaP + "," + cliP, // a QA product
	}, "\n")
	var logged []string
	logf := func(f string, a ...any) { logged = append(logged, f) }
	srv.api.addClients(parseClients(strings.NewReader(file), readTokenPathStrict, logf), []string{"qa.example", testLabel}, logf)
	var names []string
	for _, c := range srv.api.clients {
		names = append(names, c.Name)
	}
	if strings.Join(names, ",") != "qa-tools,hikaru-app,hikaru-two,hikaru-none" {
		t.Fatalf("the loaded clients are %v; want qa-tools and the three good lines, every bad line skipped", names)
	}
	if len(logged) != 10 {
		t.Errorf("%d skips were logged; want one per bad line, 10", len(logged))
	}

	// What a registry client may not reach.
	for _, c := range []struct{ method, path string }{
		{"GET", "/v1/products"}, {"GET", "/v1/products/" + cliP + "/qa/summary"}, {"POST", "/v1/seed/product"},
	} {
		if rec := call(t, srv, c.method, c.path, tokHikaru, "{}"); rec.Code != 403 {
			t.Errorf("a registry client on %s %s answered %d; want 403", c.method, c.path, rec.Code)
		}
	}
	if rec := call(t, srv, "POST", inboxPath(qaP), tokHikaru, askJSON("x")); rec.Code != 404 {
		t.Errorf("a registry client asking in a product not in its list answered %d; want 404", rec.Code)
	}
	if rec := call(t, srv, "POST", inboxPath(cliP), tokNone, askJSON("x")); rec.Code != 404 {
		t.Errorf("a client with products - asking answered %d; want 404, it reaches nothing", rec.Code)
	}

	// Its own item: asked, labelled with its name, answered with its source, never pushed.
	if rec := call(t, srv, "POST", inboxPath(cliP), tokHikaru, askJSON("h-1")); rec.Code != 201 {
		t.Fatalf("a registry client's ask answered %d %s", rec.Code, rec.Body)
	}
	qs, _ := st.OpenQuestions(t.Context(), cliP)
	if len(qs) != 1 || qs[0].Source != "hikaru-app" || qs[0].AskedBy != "hikaru-app" {
		t.Fatalf("the registry client's item reads %+v; want it tagged hikaru-app, never QA tool", qs)
	}
	html := do(t, srv, "GET", "/ui/bo/inbox", nil, boCookies()...).Body.String()
	if !strings.Contains(html, `class="ir-src">hikaru-app<`) || strings.Contains(html, `class="ir-src">QA tool<`) {
		t.Error("the inbox does not tag the registry client's item with its name")
	}
	if rec := call(t, srv, "POST", inboxPath(cliP), tokTwo, askJSON("h-1")); rec.Code != 409 || !strings.Contains(rec.Body.String(), `"id-taken"`) {
		t.Errorf("another client asking a taken id answered %d %s; want 409 id-taken", rec.Code, rec.Body)
	}
	if rec := call(t, srv, "GET", inboxPath(cliP, "h-1"), tokTwo, ""); rec.Code != 404 {
		t.Errorf("another client reading the item answered %d; want 404", rec.Code)
	}
	if rec := call(t, srv, "POST", answerPath(cliP, "h-1"), tokTwo, apiAnswerJSON(1, "", "b")); rec.Code != 404 {
		t.Errorf("another client answering the item answered %d; want 404", rec.Code)
	}
	cl := srv.api.clients[1]
	cl.busy.Store(true)
	if rec := call(t, srv, "POST", answerPath(cliP, "h-1"), tokHikaru, apiAnswerJSON(1, "", "rina")); rec.Code != 429 {
		t.Errorf("an answer with its own slot held answered %d; want 429", rec.Code)
	}
	cl.busy.Store(false)
	srv.api.qaBusy.Store(true)
	rec := call(t, srv, "POST", answerPath(cliP, "h-1"), tokHikaru, apiAnswerJSON(1, "", "rina"))
	srv.api.qaBusy.Store(false)
	got := decodeState(t, rec.Body.Bytes())
	want := apiSource{Kind: "hikaru-app", Client: "hikaru-app", Origin: "app.hikaru.example", Actor: "rina"}
	if rec.Code != 200 || got.Source == nil || *got.Source != want {
		t.Fatalf("the registry client's answer (QA slot held) answered %d %s; want 200 with source %+v", rec.Code, rec.Body, want)
	}
	if pend, _ := st.ExternalPending(t.Context()); len(pend) != 0 {
		t.Errorf("ExternalPending holds %+v; a registry client's decision must never go to the QA tool's webhook", pend)
	}

	// The open cap counts per asker.
	for i := 0; i < externalMaxOpen; i++ {
		if _, _, _, err := st.AskExternal(t.Context(), cliP, ExternalAsk{ExternalID: "cap-" + strconv.Itoa(i), Question: "q",
			AskedBy: "hikaru-two", Label: "hikaru-two"}); err != nil {
			t.Fatalf("ask %d: %v", i, err)
		}
	}
	if rec := call(t, srv, "POST", inboxPath(cliP), tokHikaru, askJSON("h-2")); rec.Code != 201 {
		t.Errorf("another client's full inbox refused this client's ask: %d %s", rec.Code, rec.Body)
	}
	if _, _, _, err := st.AskExternal(t.Context(), cliP, ExternalAsk{ExternalID: "cap-over", Question: "q", AskedBy: "hikaru-two"}); err == nil {
		t.Error("the 201st open item of one client was accepted")
	}

	// Equal built-in tokens are final; with none, a registry client enables the API.
	eq := newAPIConfig(tokAdmin, tokAdmin, tokOut, "")
	eq.addClients(parseClients(strings.NewReader("hikaru-app hk - "+good+" "+cliP), readTokenPathStrict, logf), nil, logf)
	srv.api = eq
	if rec := call(t, srv, "GET", inboxPath(cliP, "h-1"), tokHikaru, ""); rec.Code != 503 {
		t.Errorf("equal admin and QA tokens plus a registry line answered %d; want 503", rec.Code)
	}
	bare := newAPIConfig("", "", "", "")
	bare.addClients(parseClients(strings.NewReader("hikaru-app hk - "+good+" "+cliP), readTokenPathStrict, logf), []string{"", ""}, logf)
	srv.api = bare
	if rec := call(t, srv, "GET", inboxPath(cliP, "h-1"), tokHikaru, ""); rec.Code != 200 {
		t.Errorf("a registry client alone answered %d; want the API enabled for it", rec.Code)
	}
}

func TestQAOriginResolution(t *testing.T) {
	var logged int
	logf := func(string, ...any) { logged++ }
	for _, c := range []struct{ env, url, want string }{
		{"qa.example.org", "http://10.10.0.9:8140", "qa.example.org"},
		{"bad host!", "", ""},
		{"", "http://qa.example:8140", "qa.example"},
		{"", "http://10.10.0.9:8140", ""},
		{"", "::not a url", ""},
		{"", "", ""},
	} {
		if got := qaOrigin(c.env, c.url, logf); got != c.want {
			t.Errorf("qaOrigin(%q, %q) = %q; want %q", c.env, c.url, got, c.want)
		}
	}
	if logged != 1 {
		t.Errorf("%d warnings were logged; want 1, for the invalid GAOS_QA_ORIGIN", logged)
	}
}

func TestBackOfficeAnswersRecordTheirSource(t *testing.T) {
	srv, st, pids := apiFixture(t, qaWithOrigin)
	pid := pids[0]
	mustAsk(t, srv, pid, askJSON("bo-1"))
	if w := answerPost(t, srv, "/products/"+pid+"/inbox/bo-1/answer", `{"choice":2}`, true); w.Code != 200 {
		t.Fatalf("the back-office answer to an inbox item answered %d %s", w.Code, w.Body)
	}
	plantQuestion(st, pid, "wf-ui-src", "Which database?", time.Now().Unix()-60)
	if w := answerPost(t, srv, "/products/"+pid+"/tasks/wf-ui-src/answer", `{"choice":1}`, true); w.Code != 200 {
		t.Fatalf("the back-office answer to a task question answered %d %s", w.Code, w.Body)
	}
	want := AnswerSource{Kind: "gaos", Client: "back-office", Origin: testLabel, Actor: testEmail}
	suffix := "— in GAOS (" + testLabel + ") by " + testEmail
	seen := 0
	for _, e := range st.events {
		if e.Type != ExternalQuestionAnswered && e.Type != QuestionAnswered {
			continue
		}
		seen++
		q := e.Payload.Question
		if q.AnsweredBy == nil || *q.AnsweredBy != want || e.Payload.Actor != testEmail || !strings.HasSuffix(e.Text, suffix) {
			t.Errorf("%s reads %+v %q actor %q; want source %+v and the text ending %q", e.Type, q.AnsweredBy, e.Text, e.Payload.Actor, want, suffix)
		}
		if e.Type == QuestionAnswered && strings.Contains(questionBody(*q), testEmail) {
			t.Error("the next packet's answer part carries the operator's email; the source never reaches a model")
		}
	}
	if seen != 2 {
		t.Fatalf("%d answer events; want 2", seen)
	}
}

func TestAnswerSourcePhrase(t *testing.T) {
	for _, c := range []struct {
		src                         *AnswerSource
		phrase, refusal, row, sheet string
	}{
		{nil, "", "That question was already answered (source not recorded).", "answered · source not recorded", "source not recorded"},
		{&AnswerSource{}, "", "That question was already answered (source not recorded).", "answered · source not recorded", "source not recorded"},
		{&AnswerSource{Kind: "gaos", Client: "back-office", Origin: "gaos.example", Actor: "agent@gaos.local"},
			"in GAOS (gaos.example) by agent@gaos.local", "That question was already answered in GAOS (gaos.example) by agent@gaos.local.",
			"answered in GAOS (gaos.example) by agent@gaos.local", "in GAOS (gaos.example) by agent@gaos.local"},
		{&AnswerSource{Kind: "qa-tool", Client: "qa-tools", Origin: "qa-tool.example", Actor: "ardi"},
			"via QA tool (qa-tool.example) by ardi", "That question was already answered via QA tool (qa-tool.example) by ardi.",
			"answered via QA tool (qa-tool.example) by ardi", "via QA tool (qa-tool.example) by ardi"},
		{&AnswerSource{Kind: "hikaru-app", Client: "hikaru-app", Origin: "app.example", Actor: "rina"},
			"via hikaru-app (app.example) by rina", "That question was already answered via hikaru-app (app.example) by rina.",
			"answered via hikaru-app (app.example) by rina", "via hikaru-app (app.example) by rina"},
		{&AnswerSource{Kind: "qa-tool", Client: "qa-tools"}, "via QA tool", "That question was already answered via QA tool.",
			"answered via QA tool", "via QA tool"},
		{&AnswerSource{Kind: "gaos", Client: "back-office", Origin: "gaos"}, "in GAOS (gaos)", "That question was already answered in GAOS (gaos).",
			"answered in GAOS (gaos)", "in GAOS (gaos)"},
	} {
		if got := c.src.Phrase(); got != c.phrase {
			t.Errorf("Phrase(%+v) = %q; want %q", c.src, got, c.phrase)
		}
		if got := answeredSentence(c.src); got != c.refusal {
			t.Errorf("answeredSentence(%+v) = %q; want %q", c.src, got, c.refusal)
		}
		if got := recentSentence(c.src); got != c.row {
			t.Errorf("recentSentence(%+v) = %q; want %q", c.src, got, c.row)
		}
		if got := sheetSentence(c.src); got != c.sheet {
			t.Errorf("sheetSentence(%+v) = %q; want %q", c.src, got, c.sheet)
		}
	}
	if got := textSuffix(nil); got != "" {
		t.Errorf("textSuffix(nil) = %q; an answer without a source gains no suffix", got)
	}
	if a := apiSourceOf(&AnswerSource{Kind: "gaos", Client: "back-office", Origin: "x", Actor: "agent@gaos.local"}); a.Actor != "operator" {
		t.Errorf("apiSourceOf hands a machine %q; want operator", a.Actor)
	}
}
