package main

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// M15, "Needs you": the UI half. questions_test.go pins the store and the JSON
// routes; these pin what the operator sees -- the chip, the inbox, the answer
// sheet and the board badge -- because a template that renders the wrong view
// still answers 200.

// plantQuestion puts a task in AWAITING_INPUT with an open question, the state
// a gateway turn leaves behind, without running one.
func plantQuestion(s *MemStore, desk, id, question string, askedAt int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks = append(s.tasks, TaskRecord{ID: id, WorkflowID: "wf-ui", DeskID: desk,
		Title: "build the orders store", Role: "backend", Status: StatusAwaitingInput})
	s.emit(QuestionAsked, desk, id+": asks the operator", KindWarn, EventPayload{Task: id, Question: &QuestionPayload{
		ID: "q-" + id, RunID: runIDFor(id, 1), Attempt: 1, Question: question,
		Context: "The BRD names none.", AskedAt: askedAt,
		Options: []QuestionOption{
			{Label: "PostgreSQL", Description: "what the other services already run"},
			{Label: "SQLite", Description: "simplest, single node"},
			{Label: "ArcadeDB", Description: "the graph the platform uses"},
		},
	}})
}

func TestTheInboxListsEveryWaitingAgentOldestFirst(t *testing.T) {
	srv, s := newTestServer(t)
	a, b := s.desks[0].ID, s.desks[1].ID
	now := time.Now().Unix()
	plantQuestion(s, a, "wf-ui-new", "Which queue?\nKafka or NATS.", now-12*60)
	plantQuestion(s, b, "wf-ui-old", "Which database should the orders service use?", now-8*24*3600)

	w := do(t, srv, "GET", "/ui/bo/inbox", nil, boCookies()...)
	body := w.Body.String()
	if w.Code != http.StatusOK || !strings.Contains(body, `data-view="inbox"`) {
		t.Fatalf("GET /ui/bo/inbox answered %d without the inbox view; buildBO has no inbox case", w.Code)
	}
	oldAt, newAt := strings.Index(body, `id="inbox-`+b+`-wf-ui-old"`), strings.Index(body, `id="inbox-`+a+`-wf-ui-new"`)
	if oldAt < 0 || newAt < 0 || oldAt > newAt {
		t.Fatalf("the inbox rows are missing or out of order (old at %d, new at %d); want both, oldest first", oldAt, newAt)
	}
	if !strings.Contains(body, "waiting 12m") || !strings.Contains(body, `title="Asked `) {
		t.Fatal("a row does not say how long it has waited, with the absolute time in its title")
	}
	if !strings.Contains(body, "expires in 21d") || !strings.Contains(body, "expires in 29d") {
		t.Fatal("a row does not say when its question closes with its session; want expires in 29d and 21d")
	}
	if !strings.Contains(body, `data-since="`) || !strings.Contains(body, `data-until="`) {
		t.Fatal("the waiting and expiry labels carry no datetime for live.js to re-tick; they would freeze at render time")
	}
	plantQuestion(s, b, "wf-ui-late", "Which region?", now-28*24*3600)
	w = do(t, srv, "GET", "/ui/bo/inbox", nil, boCookies()...)
	if n := strings.Count(w.Body.String(), `class="ir-exp urgent"`); n != 1 {
		t.Fatalf("%d expiry labels are urgent; want 1, the question with under three days left", n)
	}
	if !strings.Contains(body, `<span class="ir-q" dir="auto">Which queue?</span>`) {
		t.Fatal("a row shows more than the question's first line")
	}

	w = do(t, srv, "GET", "/ui/bo/inbox?product="+url.QueryEscape(a), nil, boCookies()...)
	if body := w.Body.String(); strings.Contains(body, "wf-ui-old") || !strings.Contains(body, "wf-ui-new") {
		t.Fatal("the inbox filtered to one product still lists another product's agent")
	}
}

func TestAnEmptyInboxSaysNoAgentIsWaiting(t *testing.T) {
	srv, _ := newTestServer(t)
	w := do(t, srv, "GET", "/ui/bo/inbox", nil, boCookies()...)
	if !strings.Contains(w.Body.String(), "No agent is waiting on you.") {
		t.Fatalf("an empty inbox answered %d without its empty state", w.Code)
	}
}

func TestTheChipCountsWaitingAgentsOnlyForASession(t *testing.T) {
	srv, s := newTestServer(t)
	now := time.Now().Unix()
	plantQuestion(s, s.desks[0].ID, "wf-ui-1", "One?", now)
	plantQuestion(s, s.desks[1].ID, "wf-ui-2", "Two?", now)

	body := do(t, srv, "GET", "/ui/needs", nil, boCookies()...).Body.String()
	if !strings.Contains(body, `innerHTML:#needsSlot`) || strings.Contains(body, `id="needsChip" hidden`) ||
		!strings.Contains(body, `aria-label="2 agents need your answer"`) || !strings.Contains(body, `innerHTML:#navNeedsCount`) {
		t.Fatalf("GET /ui/needs signed in = %q; want a visible chip naming 2 agents and the nav count", body)
	}
	body = do(t, srv, "GET", "/ui/needs", nil).Body.String()
	if strings.Contains(body, "needsChip") || strings.Contains(body, "navNeedsCount") {
		t.Fatalf("GET /ui/needs signed out = %q; want neither the chip nor the count", body)
	}
	body = do(t, srv, "GET", "/ui/bo/inbox", nil, boCookies()...).Body.String()
	if !strings.Contains(body, `id="needsSlot"`) || !strings.Contains(body, `id="needsChip"`) {
		t.Fatal("the back-office sheet's head has no slot for the chip; signed in, the sheet hides the topbar")
	}
	// The chip rides on every mutation, so it cannot fall behind an answer.
	w := do(t, srv, "POST", "/products/"+s.desks[0].ID+"/tasks/wf-ui-1/answer", url.Values{"choice": {"1"}}, boCookies()...)
	if !strings.Contains(w.Body.String(), `aria-label="1 agent needs your answer"`) {
		t.Fatalf("an answer's response (%d) did not carry the chip down to 1", w.Code)
	}
}

func TestTheAnswerSheetPreselectsTheRecommendation(t *testing.T) {
	srv, s := newTestServer(t)
	desk := s.desks[0].ID
	plantQuestion(s, desk, "wf-ui-q", "Which database should the orders service use?", time.Now().Unix())

	w := do(t, srv, "GET", "/ui/answer/"+desk+"/wf-ui-q?from=inbox", nil, boCookies()...)
	body := w.Body.String()
	if w.Code != http.StatusOK || !strings.Contains(body, `role="dialog"`) || !strings.Contains(body, `aria-modal="true"`) {
		t.Fatalf("GET the answer sheet answered %d without a modal dialog", w.Code)
	}
	if !strings.Contains(body, `value="1" checked`) || strings.Count(body, `class="rec"`) != 1 {
		t.Fatal("option 1 is not preselected and labelled Recommended, alone")
	}
	if !strings.Contains(body, `value="4"`) || !strings.Contains(body, `id="answerOwn"`) {
		t.Fatal("the sheet has no fourth answer in the operator's own words")
	}
	if !strings.Contains(body, `hx-post="products/`+desk+`/tasks/wf-ui-q/answer"`) {
		t.Fatal("the sheet does not post to the answer route with a relative URL")
	}

	w = do(t, srv, "GET", "/ui/answer/"+desk+"/wf-ui-none", nil, boCookies()...)
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "is not waiting on an answer any more") {
		t.Fatalf("a sheet for a task that is not waiting answered %d; want 422 and a toast saying so", w.Code)
	}
	if w := do(t, srv, "GET", "/ui/answer/"+desk+"/wf-ui-q", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("the answer sheet without a session answered %d; want 401", w.Code)
	}
}

func TestAnAnswerReRendersTheViewItCameFrom(t *testing.T) {
	srv, s := newTestServer(t)
	desk := s.desks[0].ID
	now := time.Now().Unix()
	plantQuestion(s, desk, "wf-ui-a", "A?", now)
	plantQuestion(s, desk, "wf-ui-b", "B?", now)

	w := do(t, srv, "POST", "/products/"+desk+"/tasks/wf-ui-a/answer", url.Values{"choice": {"4"}, "text": {"Use NATS."}}, boCookies()...)
	if body := w.Body.String(); w.Code != http.StatusOK || !strings.Contains(body, `data-view="inbox"`) || !strings.Contains(body, "Answer sent — wf-ui-a resumes.") {
		t.Fatalf("an answer from the inbox answered %d without the inbox and the outcome toast", w.Code)
	}
	w = do(t, srv, "POST", "/products/"+desk+"/tasks/wf-ui-b/answer?from=board", url.Values{"choice": {"2"}}, boCookies()...)
	if body := w.Body.String(); w.Code != http.StatusOK || !strings.Contains(body, `data-tab="board"`) {
		t.Fatalf("an answer from the board answered %d without the board; the operator was moved off it", w.Code)
	}
	w = do(t, srv, "POST", "/products/"+desk+"/tasks/wf-ui-b/answer", url.Values{"choice": {"1"}}, boCookies()...)
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), `id="toast"`) {
		t.Fatalf("a second answer through the form answered %d; want 422 with the refusal as a toast", w.Code)
	}
}

func TestTheBoardBadgesAWaitingCard(t *testing.T) {
	srv, s := newTestServer(t)
	desk := s.desks[0].ID
	plantQuestion(s, desk, "wf-ui-c", "C?", time.Now().Unix())

	body := do(t, srv, "GET", "/products/"+desk+"?tab=board", nil, boCookies()...).Body.String()
	if !strings.Contains(body, `data-col="needs-you"`) {
		t.Fatal("the board has no Needs you column")
	}
	if !strings.Contains(body, `data-answer-open="ui/answer/`+desk+`/wf-ui-c?from=board"`) {
		t.Fatal("a waiting card carries no Needs you badge that opens its answer sheet")
	}
}

func TestTheLiveStreamIsNotGzipped(t *testing.T) {
	srv, _ := newTestServer(t)
	ts := httptest.NewServer(gzipMiddleware(srv.routes()))
	defer ts.Close()
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, ts.URL+"/events/stream", nil)
	req.Header.Set("Accept-Encoding", "gzip") // every browser sends it
	req.Header.Set("Accept", "text/event-stream")
	req.AddCookie(boCookies()[0])
	res, err := (&http.Transport{DisableCompression: true}).RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Encoding") != "" {
		t.Fatalf("the stream behind the gzip middleware answered %d, encoding %q; want 200 uncompressed",
			res.StatusCode, res.Header.Get("Content-Encoding"))
	}
	line, _ := bufio.NewReader(res.Body).ReadString('\n')
	if line != "retry: 3000\n" {
		t.Fatalf("the stream's first line is %q; want the retry hint, readable as sent", line)
	}
}

// D-083: "Answered recently" says who answered what, and from where.
func TestInboxShowsRecentAnswers(t *testing.T) {
	srv, st, pids := apiFixture(t, qaWithOrigin)
	a, b := pids[0], pids[1]
	ctx := t.Context()
	mustAsk(t, srv, a, `{"externalId":"ra-qa","question":"Quarantine the flaky specs?","title":"<script>x</script>"}`)
	call(t, srv, "POST", answerPath(a, "ra-qa"), tokQA, apiAnswerJSON(1, "", "ardi"))
	mustAsk(t, srv, a, askJSON("ra-bo"))
	answerPost(t, srv, "/products/"+a+"/inbox/ra-bo/answer", `{"choice":2}`, true)
	plantQuestion(st, a, "wf-ra-old", "Which cache?", time.Now().Unix()-60)
	if _, err := st.AnswerQuestion(ctx, a, "wf-ra-old", 4, "Redis, the shared one", AnswerSource{}); err != nil {
		t.Fatal(err) // no source: an answer given before D-083 reads the same
	}
	mustAsk(t, srv, a, `{"externalId":"ra-exp","question":"Ship tonight?","expiresInHours":1}`)
	st.ExpireQuestions(ctx, time.Now().Unix()+2*3600)

	body := do(t, srv, "GET", "/ui/bo/inbox", nil, boCookies()...).Body.String()
	for _, want := range []string{
		`id="recentTitle" class="eyebrow">Answered recently<`,
		"answered via QA tool (qa.example) by ardi",
		"answered in GAOS (" + testLabel + ") by " + testEmail,
		"answered · source not recorded",
		"own words: Redis, the shared one",
		"expired unanswered",
		"&lt;script&gt;x&lt;/script&gt;",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the inbox's Answered recently lacks %q", want)
		}
	}
	if strings.Contains(body, "<script>x</script>") {
		t.Error("an item's title reached the page unescaped")
	}
	if strings.Contains(do(t, srv, "GET", "/ui/bo/inbox?product="+url.QueryEscape(b), nil, boCookies()...).Body.String(), "Answered recently") {
		t.Error("the inbox filtered to another product still lists this product's answers")
	}

	for i := 0; i < 25; i++ {
		id := "ra-many-" + strings.Repeat("x", i+1)
		st.AskExternal(ctx, a, ExternalAsk{ExternalID: id, Question: "q"})
		st.AnswerExternal(ctx, a, id, ExternalAnswer{Choice: 1})
	}
	body = do(t, srv, "GET", "/ui/bo/inbox", nil, boCookies()...).Body.String()
	if n := strings.Count(body, `class="rr-q"`); n != recentLimit {
		t.Errorf("Answered recently lists %d rows; want at most %d", n, recentLimit)
	}
	rs, _ := st.RecentAnswers(ctx, "", 0)
	for i := 1; i < len(rs); i++ {
		if rs[i].AnsweredAt > rs[i-1].AnsweredAt {
			t.Fatalf("RecentAnswers is not newest first at %d", i)
		}
	}
}
