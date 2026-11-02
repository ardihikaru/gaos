package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// M15, "Needs you" (D-064, BR-44): an agent that ends its turn on a question
// waits for the operator, and the answer resumes the session that asked.

// plantRunning adds a task the runner has just dispatched, so a turn the test
// supervises has a record to move.
func plantRunning(s *MemStore, desk, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks = append(s.tasks, TaskRecord{ID: id, WorkflowID: "wf-q", DeskID: desk,
		Title: "build the orders store", Role: "backend", Status: StatusDispatched})
}

// askedTask runs one asking turn for a fresh task and returns its id.
func askedTask(t *testing.T, s *MemStore, f *fakeAIM, ad *AIMAdapter, id string) string {
	t.Helper()
	desk := s.desks[0].ID
	plantRunning(s, desk, id)
	askTurn(f, "q-"+id)
	if _, err := s.superviseRun(t.Context(), ad, desk, clearedFor(t, s, desk, id, 1), 1); err != nil {
		t.Fatalf("an asking turn failed to run: %v", err)
	}
	return id
}

func TestAQuestionTurnWaitsOnTheOperator(t *testing.T) {
	s := NewMemStore(1)
	f, ad := gatewayOn(t, s)
	id := askedTask(t, s, f, ad, "wf-q-t-1")

	if got := taskStatusOf(s, id); got != StatusAwaitingInput {
		t.Fatalf("a turn that ended on a question left the task %s; want AWAITING_INPUT", got)
	}
	if r := waitForRun(t, s, runIDFor(id, 1), RunStatusExited); r.FailureSignature != "" {
		t.Fatalf("an asking turn was recorded as failed (%s); a question is not a missing handoff", r.FailureSignature)
	}
	qs, _ := s.OpenQuestions(t.Context(), "")
	if len(qs) != 1 || qs[0].TaskID != id || len(qs[0].Options) != 3 || qs[0].Options[0].Label != "PostgreSQL" {
		t.Fatalf("the inbox holds %+v; want the one question with its three options, the recommended first", qs)
	}
	var asked bool
	for _, e := range s.events {
		asked = asked || (e.Type == QuestionAsked && e.Payload.Question != nil && e.Payload.Question.ID == "q-"+id)
	}
	if !asked {
		t.Fatal("no QuestionAsked carries the question; the inbox would not survive a restart")
	}
	s.mu.RLock()
	_, ok := s.artifacts[runIDFor(id, 1)+"-question"]
	s.mu.RUnlock()
	if !ok {
		t.Fatal("the question was not kept as an artifact of the attempt that asked it")
	}
	s.mu.RLock()
	notifs := s.notifs
	s.mu.RUnlock()
	if notifs == 0 {
		t.Fatal("a waiting question did not count toward the operator's notices")
	}
}

func TestAnAnswerPutsTheTaskBackInTheQueue(t *testing.T) {
	s := NewMemStore(1)
	f, ad := gatewayOn(t, s)
	id := askedTask(t, s, f, ad, "wf-q-t-2")
	desk := s.desks[0].ID

	r, err := s.AnswerQuestion(t.Context(), desk, id, ChoiceOther, "  Use PostgreSQL 17, the shared cluster.  ")
	if err != nil {
		t.Fatalf("answering in the operator's own words failed: %v", err)
	}
	if got := taskStatusOf(s, id); got != StatusReady {
		t.Fatalf("an answered task is %s; want READY, back in the queue", got)
	}
	if !strings.Contains(r.Toast, "resumes") {
		t.Fatalf("the toast says %q; want the outcome, that the task resumes", r.Toast)
	}
	if qs, _ := s.OpenQuestions(t.Context(), ""); len(qs) != 0 {
		t.Fatalf("the inbox still holds %d question(s) after the answer", len(qs))
	}
	if _, err := s.AnswerQuestion(t.Context(), desk, id, ChoiceRecommended, ""); !errors.Is(err, ErrRejected) {
		t.Fatalf("a second answer returned %v; want ErrRejected, the question is closed", err)
	}
}

func TestAnInvalidAnswerIsRefusedAndChangesNothing(t *testing.T) {
	s := NewMemStore(1)
	f, ad := gatewayOn(t, s)
	id := askedTask(t, s, f, ad, "wf-q-t-3")
	desk := s.desks[0].ID
	before := len(s.events)

	for _, c := range []struct {
		choice int
		text   string
	}{{0, ""}, {5, "x"}, {ChoiceOther, "   "}} {
		if _, err := s.AnswerQuestion(t.Context(), desk, id, c.choice, c.text); !errors.Is(err, ErrRejected) {
			t.Fatalf("choice %d with text %q returned %v; want ErrRejected", c.choice, c.text, err)
		}
	}
	if _, err := s.AnswerQuestion(t.Context(), "p-elsewhere", id, 1, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an answer through another product returned %v; want ErrNotFound", err)
	}
	if len(s.events) != before || taskStatusOf(s, id) != StatusAwaitingInput {
		t.Fatal("a refused answer wrote an event or moved the task")
	}
}

func TestTheNextTurnResumesThroughTheAnswerRoute(t *testing.T) {
	s := NewMemStore(1)
	f, ad := gatewayOn(t, s)
	id := askedTask(t, s, f, ad, "wf-q-t-4")
	desk := s.desks[0].ID
	if _, err := s.AnswerQuestion(t.Context(), desk, id, 2, "keep it on one node"); err != nil {
		t.Fatal(err)
	}

	c, _, err := s.clearFor(Dispatch{TaskID: id, DeskID: desk}, 2)
	if err != nil {
		t.Fatal(err)
	}
	p := c.Packet()
	if p.Answer == nil || p.Answer.QuestionID != "q-"+id || p.Answer.Choice != 2 {
		t.Fatalf("the next packet carries answer %+v; want the operator's choice 2 for q-%s", p.Answer, id)
	}
	var part string
	for _, pp := range p.Parts {
		if pp.Path == ".gaos/packet/context/answer.md" {
			part = pp.Body
		}
	}
	if !strings.Contains(part, "Option 2: SQLite") || !strings.Contains(part, "keep it on one node") {
		t.Fatalf("the answer part reads %q; want the chosen option and the operator's note", part)
	}

	s.moveTaskByID(id, StatusDispatched, MoveDispatch) // what the scheduler does before it supervises
	completeTurn(f, id, 2, "0.1")
	if _, err := s.superviseRun(t.Context(), ad, desk, c, 2); err != nil {
		t.Fatal(err)
	}
	got := f.answers["q-"+id]
	if len(got) != 1 || got[0].RequestID != ad.RequestIDFor(runIDFor(id, 2)) || got[0].Choice != 2 ||
		!strings.Contains(got[0].Prompt, HandoffFence) {
		t.Fatalf("the answer route received %+v; want one answer under the attempt's request id, carrying the reply contract", got)
	}
	if f.posts[ad.RequestIDFor(runIDFor(id, 2))] != 0 {
		t.Fatal("the answered turn was also posted to /v1/chat; one turn was started twice")
	}
	if got := taskStatusOf(s, id); got != StatusReview {
		t.Fatalf("the resumed turn left the task %s; want REVIEW", got)
	}
}

func TestARefusedAnswerRouteFallsBackToAnOrdinaryTurn(t *testing.T) {
	for _, status := range []int{http.StatusConflict, http.StatusGone, http.StatusServiceUnavailable} {
		s := NewMemStore(1)
		f, ad := gatewayOn(t, s)
		id := askedTask(t, s, f, ad, "wf-q-t-5")
		desk := s.desks[0].ID
		s.AnswerQuestion(t.Context(), desk, id, 1, "")
		f.answerStatus = status

		c, _, _ := s.clearFor(Dispatch{TaskID: id, DeskID: desk}, 2)
		s.moveTaskByID(id, StatusDispatched, MoveDispatch) // what the scheduler does before it supervises
		completeTurn(f, id, 2, "0.1")
		if _, err := s.superviseRun(t.Context(), ad, desk, c, 2); err != nil {
			t.Fatal(err)
		}
		if f.posts[ad.RequestIDFor(runIDFor(id, 2))] != 1 {
			t.Fatalf("a %d from the answer route stranded the turn; want it sent once through /v1/chat", status)
		}
		if got := taskStatusOf(s, id); got != StatusReview {
			t.Fatalf("after a %d the fallback turn left the task %s; want REVIEW", status, got)
		}
	}
}

func TestAMalformedQuestionIsNotAQuestion(t *testing.T) {
	s := NewMemStore(1)
	f, ad := gatewayOn(t, s)
	desk := s.desks[0].ID
	plantRunning(s, desk, "wf-q-t-6")
	f.reply = chatReply("which one?", "0.01",
		`,"question":{"id":"q-bad","question":"A or B?","options":[{"label":"A"},{"label":"B"}]}`)
	if _, err := s.superviseRun(t.Context(), ad, desk, clearedFor(t, s, desk, "wf-q-t-6", 1), 1); err != nil {
		t.Fatal(err)
	}
	if got := taskStatusOf(s, "wf-q-t-6"); got != StatusFailed {
		t.Fatalf("a two-option question with no handoff left the task %s; want FAILED (handoff-missing)", got)
	}
	if qs, _ := s.OpenQuestions(t.Context(), ""); len(qs) != 0 {
		t.Fatal("a malformed question reached the inbox")
	}
}

func TestAWaitingCardMovesOnlyByAnAnswerOrACancel(t *testing.T) {
	s := NewMemStore(1)
	f, ad := gatewayOn(t, s)
	id := askedTask(t, s, f, ad, "wf-q-t-7")
	desk := s.desks[0].ID
	ctx := context.Background()

	for _, col := range []string{ColReady, ColReview, ColDone} {
		if _, err := s.MoveCard(ctx, desk, id, CardMove{Column: col}); !errors.Is(err, ErrRejected) {
			t.Fatalf("dragging a waiting card to %s returned %v; want ErrRejected", col, err)
		}
	}
	if _, err := s.MoveCard(ctx, desk, id, CardMove{Column: ColCanceled}); err != nil {
		t.Fatalf("canceling a waiting task failed: %v", err)
	}
	if qs, _ := s.OpenQuestions(ctx, ""); len(qs) != 0 {
		t.Fatal("a canceled task's question is still in the inbox")
	}
	if a, _ := legalMove(TaskRecord{Status: StatusReady}, ColNeedsYou); a != "" {
		t.Fatal("a card can be dropped into Needs you; only an agent may put it there")
	}
}

func TestTheGatewayClientMapsTheAnswerRoute(t *testing.T) {
	f, ad := newFakeAIM(t)
	ctx := context.Background()
	if err := ad.Client.SubmitAnswer(ctx, "q-1", "req-1", AnswerRequest{Choice: 1}); err != nil {
		t.Fatalf("a 202 from the answer route returned %v", err)
	}
	for status, want := range map[int]error{http.StatusConflict: ErrQuestionAnswered,
		http.StatusGone: ErrQuestionExpired, http.StatusNotFound: ErrRequestUnknown} {
		f.answerStatus = status
		if err := ad.Client.SubmitAnswer(ctx, "q-1", "req-2", AnswerRequest{Choice: 1}); !errors.Is(err, want) {
			t.Fatalf("a %d from the answer route returned %v; want %v", status, err, want)
		}
	}
}

func TestTheQuestionDDLWidensWhatTheCreateDeclares(t *testing.T) {
	files := schemaSQL(t)
	create := regexp.MustCompile(`Task\.status[^;]*regexp '([^']+)'`).FindStringSubmatch(files["04-execution.sql"])
	alter := regexp.MustCompile(`ALTER PROPERTY Task\.status REGEXP '([^']+)'`).FindStringSubmatch(files["12-questions.sql"])
	if create == nil || alter == nil {
		t.Fatal("04-execution.sql or 12-questions.sql no longer declares Task.status")
	}
	if create[1] != alter[1] {
		t.Fatalf("12-questions.sql widens Task.status to %q but 04-execution.sql creates %q; "+
			"an old database and a new one would accept different statuses", alter[1], create[1])
	}
	kind := regexp.MustCompile(`ALTER PROPERTY Artifact\.kind REGEXP '([^']+)'`).FindStringSubmatch(files["12-questions.sql"])
	if kind == nil || !strings.Contains("|"+kind[1]+"|", "|"+ArtifactQuestion+"|") {
		t.Fatal("Artifact.kind does not admit 'question'; the asking attempt's artifact would be refused at commit")
	}
}

func answerPost(t *testing.T, srv *server, path, body string, signedIn bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if signedIn {
		for _, c := range boCookies() {
			req.AddCookie(c)
		}
	}
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, req)
	return w
}

func TestTheInboxAndTheAnswerRouteSpeakJSON(t *testing.T) {
	srv, s, desk := plannedProduct(t)
	f, ad := gatewayOn(t, s)
	id := "wf-q-t-8"
	plantRunning(s, desk, id)
	askTurn(f, "q-"+id)
	if _, err := s.superviseRun(t.Context(), ad, desk, clearedFor(t, s, desk, id, 1), 1); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/products/"+desk+"/questions", nil)
	for _, c := range boCookies() {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, req)
	var list struct {
		Questions []Question `json:"questions"`
	}
	json.Unmarshal(w.Body.Bytes(), &list)
	if w.Code != http.StatusOK || len(list.Questions) != 1 || list.Questions[0].TaskTitle == "" {
		t.Fatalf("GET questions answered %d %s; want 200 with the one waiting task, titled", w.Code, w.Body)
	}

	path := "/products/" + desk + "/tasks/" + id + "/answer"
	if w := answerPost(t, srv, path, `{"choice":1}`, false); w.Code != http.StatusUnauthorized {
		t.Fatalf("an answer without the back-office session answered %d; want 401", w.Code)
	}
	if w := answerPost(t, srv, path, `{"choice":4,"text":""}`, true); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("an empty own-words answer answered %d %s; want 422", w.Code, w.Body)
	}
	if w := answerPost(t, srv, path, `{"choice":1}`, true); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "QuestionAnswered") {
		t.Fatalf("a valid answer answered %d %s; want 200 with the QuestionAnswered event", w.Code, w.Body)
	}
	if w := answerPost(t, srv, path, `{"choice":1}`, true); w.Code != http.StatusConflict {
		t.Fatalf("a second answer answered %d; want 409, the task is no longer waiting", w.Code)
	}
	if w := answerPost(t, srv, "/products/"+desk+"/tasks/nope/answer", `{"choice":1}`, true); w.Code != http.StatusNotFound {
		t.Fatalf("an answer for an unknown task answered %d; want 404", w.Code)
	}
}
