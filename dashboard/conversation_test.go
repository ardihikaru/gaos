package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// D-082, docs/designs/conversations.md: a card's conversation, its archive
// and its prune.

const (
	cvSession1 = "11111111-2222-4333-8444-555555555555"
	cvSession2 = "66666666-7777-4888-9999-aaaaaaaaaaaa"
	cvActor    = "ardi@example.test"
)

// fakeJanitor stands in for the gateway's session delete.
type fakeJanitor struct {
	Adapter // never called
	mu      sync.Mutex
	calls   []string
	answer  map[string]error
}

func (f *fakeJanitor) DeleteSession(ctx context.Context, vendor, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, vendor+"/"+id)
	return f.answer[id]
}

// convCard gives one planned task of desk two attempts -- prompt, reply, a
// question with its answer on the first -- and the status given. It returns
// the task id.
func convCard(t *testing.T, s *MemStore, desk string, k int, status, session string) string {
	t.Helper()
	id := productTasks(s, desk)[k].ID
	s.mu.Lock()
	for n := 1; n <= 2; n++ {
		run := runIDFor(id, n)
		s.runs = append(s.runs, TaskRunRecord{ID: run, TaskID: id, DeskID: desk, Attempt: n, Status: RunStatusExited,
			Model: "claude-x", Provider: VendorClaude, Invocation: "gateway", CostBasis: CostMeasured, StartedAt: int64(1000 * n), EndedAt: int64(1000*n + 60), SessionID: session})
		s.recordHandoff(ArtifactRecord{ID: run + "-prompt", Kind: ArtifactPrompt, TaskID: id, DeskID: desk, RunID: run, Attempt: n,
			Body: fmt.Sprintf("## Task %d\n- do it", n)})
		s.recordHandoff(ArtifactRecord{ID: run + "-reply", Kind: ArtifactReply, TaskID: id, DeskID: desk, RunID: run, Attempt: n,
			Body: fmt.Sprintf("Reply %d with `code` and <script>x</script>", n)})
		s.recordHandoff(ArtifactRecord{ID: run + "-handoff", Kind: ArtifactHandoff, TaskID: id, DeskID: desk, RunID: run, Attempt: n,
			Body: "handoff stays"})
	}
	q := QuestionPayload{ID: "q-" + id, RunID: runIDFor(id, 1), Attempt: 1, Question: "Which schema?",
		Options: []QuestionOption{{Label: "v1"}, {Label: "v2"}}, AskedAt: 1100}
	s.recordHandoff(ArtifactRecord{ID: runIDFor(id, 1) + "-question", Kind: ArtifactQuestion, TaskID: id, DeskID: desk,
		RunID: runIDFor(id, 1), Attempt: 1, Body: "Which schema?"})
	s.emit(QuestionAsked, desk, id+" asks: Which schema?", KindWarn, EventPayload{Task: id, Question: &q})
	a := q
	a.Choice, a.Text, a.AnsweredAt = 2, "use the new one", 1200
	s.emit(QuestionAnswered, desk, id+": answered", KindInfo, EventPayload{Task: id, Question: &a})
	s.mu.Unlock()
	setStatus(s, id, status)
	return id
}

func TestTheConversationListsEveryAttemptWithItsQuestionAndAnswer(t *testing.T) {
	_, s, desk := plannedProduct(t)
	id := convCard(t, s, desk, 0, StatusDone, cvSession1)
	// the same question id in another product must not become this card's answer
	s.mu.Lock()
	s.emit(QuestionAnswered, "p-other", "noise", KindInfo, EventPayload{Task: id,
		Question: &QuestionPayload{ID: "q-" + id, RunID: runIDFor(id, 1), Choice: 1, AnsweredAt: 9}})
	s.mu.Unlock()
	c, err := s.Conversation(context.Background(), desk, id)
	if err != nil || len(c.Attempts) != 2 || c.Attempts[0].Attempt != 1 || c.Attempts[1].Attempt != 2 {
		t.Fatalf("the conversation is %+v (%v); want two attempts in order", c.Attempts, err)
	}
	a := c.Attempts[0]
	if !strings.Contains(a.Prompt, "Task 1") || !strings.Contains(a.Reply, "Reply 1") || a.Question == nil ||
		a.Question.Question != "Which schema?" || a.Question.Choice != 2 || a.Question.Text != "use the new one" {
		t.Fatalf("attempt 1 is %+v; want its prompt, its reply, the question and the operator's answer", a)
	}
	if c.Attempts[1].Question != nil {
		t.Fatalf("attempt 2 carries a question it never asked: %+v", c.Attempts[1].Question)
	}
	if _, err := s.Conversation(context.Background(), desk, "wf-999-t-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an unknown card answered %v; want ErrNotFound", err)
	}
}

func boardJSON(t *testing.T, srv *server, desk string) map[string]*BoardConversation {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/products/"+desk+"/board", nil)
	for _, c := range boCookies() {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, r)
	var b struct {
		Cards []struct {
			ID           string             `json:"id"`
			Conversation *BoardConversation `json:"conversation"`
		} `json:"cards"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &b)
	out := map[string]*BoardConversation{}
	for _, c := range b.Cards {
		out[c.ID] = c.Conversation
	}
	return out
}

func TestArchiveHidesTheChipAndRestoreBringsItBack(t *testing.T) {
	srv, s, desk := plannedProduct(t)
	id := convCard(t, s, desk, 0, StatusDone, cvSession1)
	if c := boardJSON(t, srv, desk)[id]; c == nil || c.Attempts != 2 || c.Archived {
		t.Fatalf("before archive the card's conversation is %+v; want 2 attempts, active", c)
	}
	page := do(t, srv, "GET", "/products/"+desk+"?tab=board", nil, boCookies()...).Body.String()
	if !strings.Contains(page, `id="cv-`+id+`"`) {
		t.Fatalf("the server board shows no conversation chip for %s", id)
	}
	w := do(t, srv, "POST", "/products/"+desk+"/board/cards/"+id+"/conversation/archive", url.Values{}, boCookies()...)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `id="conversationModal"  hidden`) &&
		!strings.Contains(w.Body.String(), `id="conversationModal" hidden`) {
		t.Fatalf("archive answered %d without closing the sheet", w.Code)
	}
	var arch *Event
	for _, e := range s.rawEvents() {
		if e.Type == ConversationArchived {
			e := e
			arch = &e
		}
	}
	if arch == nil || arch.Payload.To != "archived" || arch.Payload.Actor != testEmail {
		t.Fatalf("archive wrote %+v; want ConversationArchived to archived by the signed-in account", arch)
	}
	if c := boardJSON(t, srv, desk)[id]; c == nil || !c.Archived {
		t.Fatalf("after archive the board JSON says %+v; want archived: true", c)
	}
	page = do(t, srv, "GET", "/products/"+desk+"?tab=board", nil, boCookies()...).Body.String()
	if strings.Contains(page, `id="cv-`+id+`"`) {
		t.Fatalf("the server board still shows %s's conversation chip after archive", id)
	}
	ap := do(t, srv, "GET", "/ui/bo/archive", nil, boCookies()...).Body.String()
	if !strings.Contains(ap, `name="card" value="`+desk+"/"+id+`"`) || !strings.Contains(ap, "Prune 1 conversation") {
		t.Fatalf("the Archive page does not list %s for prune", id)
	}
	sheet := do(t, srv, "GET", "/ui/conversation/"+desk+"/"+id, nil, boCookies()...).Body.String()
	if !strings.Contains(sheet, "Archived ") || !strings.Contains(sheet, "Reply 1") || !strings.Contains(sheet, ">Restore<") ||
		strings.Contains(sheet, ">Archive<") {
		t.Fatalf("the archived sheet must show the attempts read-only with Restore and no Archive")
	}
	if w := do(t, srv, "POST", "/products/"+desk+"/board/cards/"+id+"/conversation/restore", url.Values{}, boCookies()...); w.Code != 200 {
		t.Fatalf("restore answered %d", w.Code)
	}
	if c := boardJSON(t, srv, desk)[id]; c == nil || c.Archived {
		t.Fatalf("after restore the board JSON says %+v; want active", c)
	}
}

func TestArchiveIsRefusedForAnUnfinishedCardOrTwice(t *testing.T) {
	_, s, desk := plannedProduct(t)
	ctx := context.Background()
	for k, st := range []string{StatusReady, StatusAwaitingInput} {
		id := convCard(t, s, desk, k, st, "")
		_, err := s.ArchiveConversation(ctx, desk, id, cvActor, true)
		if err == nil || !strings.Contains(err.Error(), "is "+st+"; a conversation is archived when its card is done or canceled") {
			t.Fatalf("archiving a %s card answered %v; want the refusal naming its status", st, err)
		}
	}
	id := convCard(t, s, desk, 2, StatusCanceled, "")
	s.mu.Lock()
	s.runs = append(s.runs, TaskRunRecord{ID: runIDFor(id, 3), TaskID: id, DeskID: desk, Attempt: 3, Status: RunStatusRunning})
	s.mu.Unlock()
	if _, err := s.ArchiveConversation(ctx, desk, id, cvActor, true); err == nil {
		t.Fatalf("a canceled card with a RUNNING attempt was archived; a turn could still write into it")
	}
	done := convCard(t, s, desk, 3, StatusDone, "")
	if _, err := s.ArchiveConversation(ctx, desk, done, cvActor, false); err == nil || !strings.Contains(err.Error(), "is not archived") {
		t.Fatalf("restoring an active conversation answered %v; want a refusal", err)
	}
	if _, err := s.ArchiveConversation(ctx, desk, done, cvActor, true); err != nil {
		t.Fatalf("archive: %v", err)
	}
	if _, err := s.ArchiveConversation(ctx, desk, done, cvActor, true); err == nil || !strings.Contains(err.Error(), "already archived") {
		t.Fatalf("archiving twice answered %v; want a refusal", err)
	}
	bare := productTasks(s, desk)[4].ID
	setStatus(s, bare, StatusDone)
	if _, err := s.ArchiveConversation(ctx, desk, bare, cvActor, true); err == nil || !strings.Contains(err.Error(), "has no conversation yet") {
		t.Fatalf("archiving a card with no attempt answered %v; want a refusal", err)
	}
	if _, err := (readOnly{s}).ArchiveConversation(ctx, desk, done, cvActor, false); err == nil {
		t.Fatalf("the replay handle restored a conversation")
	}
}

func TestPruneEmptiesDocumentsAndSessionsAndLeavesACertificate(t *testing.T) {
	_, s, desk := plannedProduct(t)
	ctx := context.Background()
	jan := &fakeJanitor{}
	s.mu.Lock()
	s.adapter = jan
	s.mu.Unlock()
	id := convCard(t, s, desk, 0, StatusDone, cvSession1)
	before := map[string]string{}
	for _, k := range []string{"-prompt", "-reply", "-question"} {
		before[runIDFor(id, 1)+k] = s.artifacts[runIDFor(id, 1)+k].Body
	}
	runsBefore := len(s.runsFor(id))
	if _, err := s.ArchiveConversation(ctx, desk, id, cvActor, true); err != nil {
		t.Fatal(err)
	}
	nEvents := len(s.rawEvents())
	res, err := s.PruneConversations(ctx, cvActor, []CardRef{{desk, id}})
	if err != nil || !strings.HasPrefix(res.Toast, "Pruned 1 conversation(s); 0 skipped") {
		t.Fatalf("prune answered %q, %v", res.Toast, err)
	}
	for aid := range before {
		if s.artifacts[aid].Body != "" {
			t.Fatalf("%s still holds its body after the prune", aid)
		}
	}
	if s.artifacts[runIDFor(id, 1)+"-handoff"].Body != "handoff stays" || len(s.runsFor(id)) != runsBefore {
		t.Fatalf("prune touched a handoff or a run record; both survive")
	}
	if len(jan.calls) != 1 || jan.calls[0] != "claude/"+cvSession1 {
		t.Fatalf("the gateway was asked %v; want the one session once", jan.calls)
	}
	evs := s.rawEvents()
	if len(evs) != nEvents+2 {
		t.Fatalf("prune wrote %d events; want the bodies and the sessions certificates", len(evs)-nEvents)
	}
	var bodies, sessions *PruneRecord
	for _, e := range evs[nEvents:] {
		if e.Type != ConversationPruned || e.Payload.Actor != cvActor {
			t.Fatalf("prune wrote %s by %q; want ConversationPruned by the account", e.Type, e.Payload.Actor)
		}
		if e.Payload.Prune.Stage == "bodies" {
			bodies = e.Payload.Prune
		} else {
			sessions = e.Payload.Prune
		}
	}
	if bodies == nil || len(bodies.Bodies) != 5 || strings.Join(bodies.Kept, "|") != strings.Join(pruneKept, "|") {
		t.Fatalf("the bodies certificate is %+v; want five documents (2 prompts, 2 replies, 1 question) and the Kept list", bodies)
	}
	for _, b := range bodies.Bodies {
		if want, ok := before[b.ID]; ok && (b.SHA256 != bodyDigest(want) || b.Bytes != len(want)) {
			t.Fatalf("%s is certified as %s/%d; want the original text's digest and length", b.ID, b.SHA256, b.Bytes)
		}
	}
	if sessions == nil || sessions.SessionsNamed != 1 || sessions.SessionsDeleted != 1 {
		t.Fatalf("the sessions certificate is %+v; want 1 named, 1 deleted", sessions)
	}
	raw, _ := json.Marshal(evs[nEvents:])
	if strings.Contains(string(raw), cvSession1) || strings.Contains(res.Toast, cvSession1) {
		t.Fatalf("a certificate or the toast carries a session id")
	}
	c, _ := s.Conversation(ctx, desk, id)
	if !c.State.Pruned || c.Attempts[0].Question == nil {
		t.Fatalf("after the prune the state is %+v; want pruned, the question still in the events", c.State)
	}
}

func TestPruneSkipsWhatChangedAndKeepsWhatIsShared(t *testing.T) {
	srv, s, desk := plannedProduct(t)
	ctx := context.Background()
	jan := &fakeJanitor{answer: map[string]error{cvSession2: ErrSessionBusy}}
	s.mu.Lock()
	s.adapter = jan
	s.mu.Unlock()
	restored := convCard(t, s, desk, 0, StatusDone, cvSession1)
	busy := convCard(t, s, desk, 1, StatusDone, cvSession2)
	shared := convCard(t, s, desk, 2, StatusDone, cvSession1) // shares a session with an unselected card
	other := convCard(t, s, desk, 3, StatusCanceled, "")
	for _, id := range []string{restored, busy, other} {
		if _, err := s.ArchiveConversation(ctx, desk, id, cvActor, true); err != nil {
			t.Fatal(err)
		}
	}
	// the page listed restored and busy; restored is restored before the POST,
	// and other is archived but was not on the page.
	if _, err := s.ArchiveConversation(ctx, desk, restored, cvActor, false); err != nil {
		t.Fatal(err)
	}
	_ = shared
	res, err := s.PruneConversations(ctx, cvActor, []CardRef{{desk, restored}, {desk, busy}})
	if err != nil || res.Toast != "Pruned 1 conversation(s); 1 skipped (no longer archived)." {
		t.Fatalf("prune answered %q, %v; want one pruned, one skipped", res.Toast, err)
	}
	if s.artifacts[runIDFor(restored, 1)+"-reply"].Body == "" || s.artifacts[runIDFor(other, 1)+"-reply"].Body == "" {
		t.Fatalf("prune emptied a card that was restored, or one that was not listed")
	}
	if strings.Contains(res.Toast, cvSession2) || strings.Contains(fmt.Sprint(res.NewEvents), cvSession2) {
		t.Fatalf("a busy session's id reached the result")
	}
	var rec *PruneRecord
	for _, e := range res.NewEvents {
		if e.Payload.Prune != nil && e.Payload.Prune.Stage == "sessions" {
			rec = e.Payload.Prune
		}
	}
	if rec == nil || rec.SessionsFailed != 1 || rec.SessionsDeleted != 0 {
		t.Fatalf("the sessions certificate is %+v; want the busy session counted failed", rec)
	}

	// a session also named by a card outside the prune is kept
	if _, err := s.ArchiveConversation(ctx, desk, shared, cvActor, true); err != nil {
		t.Fatal(err)
	}
	jan.calls = nil
	res, _ = s.PruneConversations(ctx, cvActor, []CardRef{{desk, shared}})
	rec = nil
	for _, e := range res.NewEvents {
		if e.Payload.Prune != nil && e.Payload.Prune.Stage == "sessions" {
			rec = e.Payload.Prune
		}
	}
	if len(jan.calls) != 0 || rec == nil || rec.SessionsShared != 1 {
		t.Fatalf("a shared session got calls %v and certificate %+v; want it kept, counted shared", jan.calls, rec)
	}

	// a commit that fails changes nothing
	if _, err := s.ArchiveConversation(ctx, desk, restored, cvActor, true); err != nil {
		t.Fatal(err)
	}
	n := len(s.rawEvents())
	_, err = s.pruneWith(ctx, cvActor, []CardRef{{desk, restored}}, prunePlan{
		commit: func([]Event, []string) error { return errors.New("database down") },
	})
	if err == nil || len(s.rawEvents()) != n || s.artifacts[runIDFor(restored, 1)+"-reply"].Body == "" {
		t.Fatalf("a failed commit answered %v, wrote %d events and left the body %q; want nothing changed",
			err, len(s.rawEvents())-n, s.artifacts[runIDFor(restored, 1)+"-reply"].Body)
	}
	if sheet := do(t, srv, "GET", "/ui/conversation/"+desk+"/"+restored, nil, boCookies()...).Body.String(); strings.Contains(sheet, "Pruned ") {
		t.Fatalf("after a failed commit the sheet says pruned")
	}
}

func TestPruneWithoutTheTypedConfirmationDeletesNothing(t *testing.T) {
	srv, s, desk := plannedProduct(t)
	id := convCard(t, s, desk, 0, StatusDone, "")
	if _, err := s.ArchiveConversation(context.Background(), desk, id, cvActor, true); err != nil {
		t.Fatal(err)
	}
	n := len(s.rawEvents())
	for _, form := range []url.Values{
		{"card": {desk + "/" + id}},
		{"card": {desk + "/" + id}, "confirm": {"prune"}},
		{"confirm": {"PRUNE"}},
	} {
		w := do(t, srv, "POST", "/conversations/prune", form, boCookies()...)
		want := "Type PRUNE to confirm; nothing was deleted."
		if form.Get("confirm") == "PRUNE" {
			want = "Nothing to prune."
		}
		if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), want) {
			t.Fatalf("prune with %v answered %d; want 422 and %q", form, w.Code, want)
		}
	}
	if len(s.rawEvents()) != n || s.artifacts[runIDFor(id, 1)+"-reply"].Body == "" {
		t.Fatalf("an unconfirmed prune wrote events or emptied a body")
	}
	w := do(t, srv, "POST", "/conversations/prune", url.Values{"card": {desk + "/" + id}, "confirm": {"PRUNE"}}, boCookies()...)
	if w.Code != 200 || s.artifacts[runIDFor(id, 1)+"-reply"].Body != "" || !strings.Contains(w.Body.String(), "Pruned 1 conversation(s)") {
		t.Fatalf("a confirmed prune answered %d and left the body", w.Code)
	}
}

func TestTheConversationRoutesAreBackOfficeAndSameOrigin(t *testing.T) {
	srv, s, desk := plannedProduct(t)
	id := convCard(t, s, desk, 0, StatusDone, "")
	for _, path := range []string{
		"/products/" + desk + "/board/cards/" + id + "/conversation/archive",
		"/products/" + desk + "/board/cards/" + id + "/conversation/restore",
		"/conversations/prune",
	} {
		for _, h := range [][2]string{{"Sec-Fetch-Site", "cross-site"}, {"Origin", "https://evil.example"}} {
			r := httptest.NewRequest(http.MethodPost, path, strings.NewReader("confirm=PRUNE&card="+desk+"/"+id))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.Header.Set(h[0], h[1])
			for _, c := range boCookies() {
				r.AddCookie(c)
			}
			w := httptest.NewRecorder()
			srv.routes().ServeHTTP(w, r)
			if w.Code != http.StatusForbidden {
				t.Fatalf("POST %s with %s: %s answered %d; want 403", path, h[0], h[1], w.Code)
			}
		}
	}
	if w := do(t, srv, "GET", "/ui/conversation/"+desk+"/wf-999-t-1", nil, boCookies()...); w.Code != http.StatusNotFound ||
		!strings.Contains(w.Body.String(), "That record is gone.") {
		t.Fatalf("an unknown card's sheet answered %d; want 404 and the toast", w.Code)
	}
	w := do(t, srv, "GET", "/ui/conversation/"+desk+"/"+id, nil, boCookies()...)
	if w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Fatalf("the sheet answered Cache-Control %q and CSP %q; want no-store and the back-office policy",
			w.Header().Get("Cache-Control"), w.Header().Get("Content-Security-Policy"))
	}
	if body := w.Body.String(); strings.Contains(body, "<script>x</script>") || !strings.Contains(body, "&lt;script&gt;") {
		t.Fatalf("the sheet emitted the model's HTML as HTML")
	}
	if w := do(t, srv, "GET", "/ui/bo/archive", nil, boCookies()...); w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("the Archive view answered Cache-Control %q; want no-store", w.Header().Get("Cache-Control"))
	}
}

func TestAPrunedConversationStaysPrunedAfterARestart(t *testing.T) {
	arc := arcadeTestTarget(t)
	ctx := context.Background()
	tenant := "conf-conversation"
	first := newArcadeTestStore(t, arc, tenant).(*ArcadeStore)
	m, _ := first.Master(ctx, "p-1")
	draft := WorkflowDraft{ProductID: "p-1", Goal: "Talk", Stories: []string{m.Stories[0].ID}}
	plan, err := first.PlanWorkflow(ctx, draft)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.CreateWorkflow(ctx, draft, plan); err != nil {
		t.Fatal(err)
	}
	id := convCard(t, first.proj, "p-1", 0, StatusDone, "")
	reply := first.proj.artifacts[runIDFor(id, 1)+"-reply"].Body
	// persist what convCard put in memory: the runs, artifacts and events
	if _, err := first.mutate(ctx, func() (Result, error) {
		first.proj.mu.Lock()
		defer first.proj.mu.Unlock()
		var evs []Event
		for _, e := range first.proj.events {
			if e.Type == QuestionAsked || e.Type == QuestionAnswered {
				evs = append(evs, e)
			}
		}
		return first.proj.result("", evs...), nil
	}); err != nil {
		t.Fatal(err)
	}

	second, err := NewArcadeStoreFor(ctx, arc, 1, tenant)
	if err != nil {
		t.Fatal(err)
	}
	c, err := second.Conversation(ctx, "p-1", id)
	if err != nil || len(c.Attempts) != 2 || c.Attempts[0].Reply != reply {
		t.Fatalf("after a reopen the conversation is %+v (%v); want the reply read from the database", c.Attempts, err)
	}
	if _, err := second.ArchiveConversation(ctx, "p-1", id, cvActor, true); err != nil {
		t.Fatal(err)
	}
	if _, err := second.PruneConversations(ctx, cvActor, []CardRef{{"p-1", id}}); err != nil {
		t.Fatal(err)
	}

	third, err := NewArcadeStoreFor(ctx, arc, 1, tenant)
	if err != nil {
		t.Fatal(err)
	}
	bodies, err := third.artifactBodies(ctx, []string{runIDFor(id, 1) + "-reply", runIDFor(id, 2) + "-prompt"})
	if err != nil || len(bodies) != 2 || bodies[runIDFor(id, 1)+"-reply"] != "" || bodies[runIDFor(id, 2)+"-prompt"] != "" {
		t.Fatalf("after the prune and a reopen the database holds %v (%v); want both bodies empty", bodies, err)
	}
	c, _ = third.Conversation(ctx, "p-1", id)
	if !c.State.Pruned || c.State.Bodies == nil || c.State.PrunedBy != cvActor || c.Attempts[0].Question == nil {
		t.Fatalf("after a reopen the state is %+v; want pruned by the account, the certificate and the question events", c.State)
	}
	var cert *PrunedBody
	for _, b := range c.State.Bodies.Bodies {
		if b.ID == runIDFor(id, 1)+"-reply" {
			b := b
			cert = &b
		}
	}
	if cert == nil || cert.SHA256 != bodyDigest(reply) {
		t.Fatalf("the certificate for the reply read from the database is %+v; want the original text's digest", cert)
	}
	var actors []struct {
		Actor string `json:"actor"`
	}
	if err := arc.Query(ctx, `SELECT actor FROM Event WHERE tenantId = :t AND type = 'ConversationPruned'`,
		map[string]any{"t": tenant}, &actors); err != nil || len(actors) == 0 || actors[0].Actor != cvActor {
		t.Fatalf("the actor column is %+v (%v); want the account", actors, err)
	}
}
