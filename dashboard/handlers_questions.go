package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
)

// "Needs you" (M15, D-064). Every route needs the back-office session; the
// JSON half is for a client, the HTMX half renders the inbox.
//
//	GET  /questions                                   {questions: [...]}, every product, oldest first
//	GET  /products/{id}/questions                     the same, one product
//	POST /products/{id}/tasks/{taskId}/answer         {choice: 1..4, text} -> 200 {toast, events}
//	                                                  | 404 {error} | 409 {error, reason} | 422 {error, reason}

func (s *server) handleQuestions(w http.ResponseWriter, r *http.Request) {
	if !s.requireBO(w, r) {
		return
	}
	qs, err := s.store.OpenQuestions(r.Context(), r.PathValue("id"))
	if err != nil {
		writeBoardJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if wantsHTMX(r) {
		s.renderInbox(w, r, Result{}, r.PathValue("id"))
		return
	}
	writeBoardJSON(w, http.StatusOK, map[string]any{"questions": qs})
}

// answerBody is the answer from either a JSON client or the answer sheet's
// form. A missing choice is 0, which the store refuses with a sentence.
func answerBody(w http.ResponseWriter, r *http.Request) (int, string, error) {
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		var b struct {
			Choice int    `json:"choice"`
			Text   string `json:"text"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&b); err != nil {
			return 0, "", err
		}
		return b.Choice, b.Text, nil
	}
	if err := r.ParseForm(); err != nil {
		return 0, "", err
	}
	c, _ := strconv.Atoi(r.FormValue("choice"))
	return c, r.FormValue("text"), nil
}

func (s *server) handleAnswer(w http.ResponseWriter, r *http.Request) {
	if !s.requireBO(w, r) {
		return
	}
	id, taskID := r.PathValue("id"), r.PathValue("taskId")
	choice, text, err := answerBody(w, r)
	if err != nil {
		writeBoardJSON(w, http.StatusBadRequest, map[string]string{"error": `body must be {"choice": 1..4, "text"}`})
		return
	}
	res, err := s.mut(r).AnswerQuestion(r.Context(), id, taskID, choice, text, s.boSource(r))
	if err != nil {
		if wantsHTMX(r) {
			s.fail(w, r, err)
			return
		}
		switch {
		case errors.Is(err, ErrNotFound):
			writeBoardJSON(w, http.StatusNotFound, map[string]string{"error": "no such task"})
		case choice < ChoiceRecommended || choice > ChoiceOther ||
			(choice == ChoiceOther && strings.TrimSpace(text) == ""):
			writeBoardJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "answer refused", "reason": err.Error()})
		default:
			writeBoardJSON(w, http.StatusConflict, map[string]string{"error": "answer refused", "reason": err.Error()})
		}
		return
	}
	if wantsHTMX(r) {
		// An answer given from the board re-renders the board, so the operator
		// stays where they were and sees the card leave Needs you.
		if r.URL.Query().Get("from") == "board" {
			s.renderBOAfter(w, r, res, id, "board")
			return
		}
		s.renderInbox(w, r, res, r.URL.Query().Get("product"))
		return
	}
	writeBoardJSON(w, http.StatusOK, map[string]any{"toast": res.Toast, "events": stripPayloads(res.NewEvents)})
}

// handleExternalAnswer is POST /products/{id}/inbox/{externalId}/answer: the
// operator answers one of the QA tool's questions (D-078). The same session
// gate, body and refusals as the task route; the answer is recorded here and
// delivered to the QA tool by the worker, so a webhook that is down never
// makes this fail.
//
//	POST /products/{id}/inbox/{externalId}/answer   {choice: 1..4, text} -> 200 {toast, events}
//	                                                | 404 {error} | 409 {error, reason} | 422 {error, reason}
func (s *server) handleExternalAnswer(w http.ResponseWriter, r *http.Request) {
	if !s.requireBO(w, r) {
		return
	}
	id, ext := r.PathValue("id"), r.PathValue("externalId")
	choice, text, err := answerBody(w, r)
	if err != nil {
		writeBoardJSON(w, http.StatusBadRequest, map[string]string{"error": `body must be {"choice": 1..4, "text"}`})
		return
	}
	res, err := s.mut(r).AnswerExternal(r.Context(), id, ext, ExternalAnswer{Choice: choice, Text: text, By: s.boSource(r)})
	if err != nil {
		if wantsHTMX(r) {
			s.fail(w, r, err)
			return
		}
		switch {
		case errors.Is(err, ErrNotFound):
			writeBoardJSON(w, http.StatusNotFound, map[string]string{"error": "no such question"})
		case choice < ChoiceRecommended || choice > ChoiceOther ||
			(choice == ChoiceOther && strings.TrimSpace(text) == ""):
			writeBoardJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "answer refused", "reason": err.Error()})
		default:
			writeBoardJSON(w, http.StatusConflict, map[string]string{"error": "answer refused", "reason": err.Error()})
		}
		return
	}
	s.inbox.Kick() // tell the QA tool soon; never wait for it
	if wantsHTMX(r) {
		s.renderInbox(w, r, res, r.URL.Query().Get("product"))
		return
	}
	writeBoardJSON(w, http.StatusOK, map[string]any{"toast": res.Toast, "events": stripPayloads(res.NewEvents)})
}

// boSource is where a back-office answer comes from (D-083): GAOS itself, this
// deployment's label, the signed-in account.
func (s *server) boSource(r *http.Request) AnswerSource {
	origin := ""
	if s.auth != nil {
		origin = s.auth.label
	}
	return AnswerSource{Kind: SourceKindGAOS, Client: ClientBackOffice, Origin: origin, Actor: actorOf(r)}
}

// renderInbox renders the back office's inbox view after a read or an answer,
// with the chrome riding along so the toast and the badge move together.
func (s *server) renderInbox(w http.ResponseWriter, r *http.Request, res Result, productID string) {
	v := s.applyResult(r, res)
	s.sheet(w, r, v, "inbox", productID, "")
	s.rend.RenderWithChrome(w, "bo-main-oob", v)
}

// stripPayloads is what taskResult does to the events it returns: the payload
// is the fold's input, not part of what the seam hands a caller.
func stripPayloads(in []Event) []Event {
	out := make([]Event, 0, len(in))
	for _, e := range in {
		e.Payload = EventPayload{}
		out = append(out, e)
	}
	return out
}
