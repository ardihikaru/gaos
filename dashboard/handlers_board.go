package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
)

// The board's routes (M14.6). The JSON half is the sisflow REST provider's
// shape, so a client-side board can sit on it unchanged; the HTMX half renders
// the back office's Board tab. Every route needs the back-office session.
//
//	GET    /products/{id}/board                       {columns, rows, cards}
//	PUT    /products/{id}/board/cards/{taskId}/move   {column, row, index} -> 200 {card} | 409 {error, reason}
//	POST   /products/{id}/board/cards                 409: cards are planned, not created here
//	PUT    /products/{id}/board/cards/{taskId}        409: a card is not edited here
//	DELETE /products/{id}/board/cards/{taskId}        409: a task is canceled, not deleted

func wantsHTMX(r *http.Request) bool { return r.Header.Get("HX-Request") == "true" }

func writeBoardJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func refuseCard(w http.ResponseWriter, reason string) {
	writeBoardJSON(w, http.StatusConflict, map[string]string{"error": "move refused", "reason": reason})
}

func (s *server) handleBoard(w http.ResponseWriter, r *http.Request) {
	if !s.requireBO(w, r) {
		return
	}
	// ?paged=1 is the client board's document (D-095, board_view.go): one page,
	// filtered. Without it this is the document it has always been.
	if r.URL.Query().Get("paged") == "1" {
		s.handleBoardPaged(w, r, r.PathValue("id"))
		return
	}
	b, err := s.store.Board(r.Context(), r.PathValue("id"))
	if errors.Is(err, ErrNotFound) {
		writeBoardJSON(w, http.StatusNotFound, map[string]string{"error": "no such product"})
		return
	}
	if err != nil {
		writeBoardJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	trimUnitCards(requestAdmin(r), b.Cards)
	writeBoardJSON(w, http.StatusOK, b)
}

func (s *server) handleMoveCard(w http.ResponseWriter, r *http.Request) {
	if !s.requireBO(w, r) {
		return
	}
	id, taskID := r.PathValue("id"), r.PathValue("taskId")
	var mv CardMove
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&mv); err != nil {
			writeBoardJSON(w, http.StatusBadRequest, map[string]string{"error": `body must be {"column","row","index"}`})
			return
		}
	} else {
		_ = r.ParseForm()
		mv = CardMove{Column: r.FormValue("column"), Row: r.FormValue("row"), Index: atoiDefault(r.FormValue("index"), -1)}
	}
	// D-105 §7.3: who moves, from the session, never from the body.
	if ss, ok := sessionOf(r); ok {
		mv.Actor = ss.Email
	}
	mv.Admin = requestAdmin(r)
	res, err := s.mut(r).MoveCard(r.Context(), id, taskID, mv)
	if errors.Is(err, ErrUnitHeld) {
		s.failStatus(w, r, err, http.StatusConflict)
		return
	}
	if err != nil {
		if wantsHTMX(r) {
			s.fail(w, r, err)
			return
		}
		if errors.Is(err, ErrNotFound) {
			writeBoardJSON(w, http.StatusNotFound, map[string]string{"error": "no such card"})
			return
		}
		refuseCard(w, err.Error())
		return
	}
	// A move made on the Board is answered with the Board: the columns that
	// changed, at the depth and under the filter the browser is showing
	// (D-095). The whole sheet used to come back for one card.
	if wantsHTMX(r) {
		s.renderBoardAfter(w, r, res, id)
		return
	}
	// One card, not the board (D-095): on the client board every drag ends here.
	if c, err := s.store.BoardCard(r.Context(), id, taskID); err == nil {
		one := []BoardCard{c}
		trimUnitCards(mv.Admin, one)
		writeBoardJSON(w, http.StatusOK, map[string]any{"card": one[0]})
		return
	}
	writeBoardJSON(w, http.StatusOK, map[string]any{"card": nil})
}

// handleSendBack is POST /products/{id}/board/cards/{taskId}/send-back: the
// operator sends the design a review blocked back to its author (D-084). A
// form post only, read from the body alone: d<N> is finding N's disposition,
// n<N> its note. The actor is the session's account, never the request's.
func (s *server) handleSendBack(w http.ResponseWriter, r *http.Request) {
	ss, ok := sessionOf(r)
	if !ok || ss.Email == "" {
		writeBoardJSON(w, http.StatusUnauthorized, map[string]string{"error": "sign in to the back office"})
		return
	}
	id, taskID := r.PathValue("id"), r.PathValue("taskId")
	fail := func(status int, msg string) {
		if wantsHTMX(r) {
			s.fail(w, r, reject(msg))
			return
		}
		writeBoardJSON(w, status, map[string]string{"error": msg})
	}
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/x-www-form-urlencoded") {
		fail(http.StatusUnsupportedMediaType, "the send-back is a form post")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
	if err := r.ParseForm(); err != nil {
		fail(http.StatusBadRequest, "the triage could not be read")
		return
	}
	var triage []FindingTriage
	for n := 1; n <= 1000; n++ {
		d, dok := r.PostForm["d"+strconv.Itoa(n)]
		note, nok := r.PostForm["n"+strconv.Itoa(n)]
		if !dok && !nok {
			break
		}
		f := FindingTriage{N: n}
		if len(d) > 0 {
			f.Disposition = d[0]
		}
		if len(note) > 0 {
			f.Note = note[0]
		}
		triage = append(triage, f)
	}
	res, err := s.mut(r).SendBackDesign(r.Context(), id, taskID, ss.Email, triage)
	if err != nil {
		if wantsHTMX(r) {
			s.fail(w, r, err)
			return
		}
		if errors.Is(err, ErrNotFound) {
			writeBoardJSON(w, http.StatusNotFound, map[string]string{"error": "no such card"})
			return
		}
		refuseCard(w, err.Error())
		return
	}
	if wantsHTMX(r) {
		s.renderBOAfter(w, r, res, id, "board")
		return
	}
	// One card, not the board (D-095): on the client board every drag ends here.
	if c, err := s.store.BoardCard(r.Context(), id, taskID); err == nil {
		writeBoardJSON(w, http.StatusOK, map[string]any{"card": c})
		return
	}
	writeBoardJSON(w, http.StatusOK, map[string]any{"card": nil})
}

// handleCardTiming is GET /products/{id}/board/cards/{taskId}/timing: one
// card's whole timing history as JSON (D-081). A store failure answers a fixed
// body and logs the cause; it never echoes the error to the browser.
func (s *server) handleCardTiming(w http.ResponseWriter, r *http.Request) {
	if !s.requireBOJSON(w, r) {
		return
	}
	id, taskID := r.PathValue("id"), r.PathValue("taskId")
	ct, err := s.store.CardTiming(r.Context(), id, taskID)
	if errors.Is(err, ErrNotFound) {
		writeBoardJSON(w, http.StatusNotFound, map[string]string{"error": "no such card"})
		return
	}
	if err != nil {
		log.Printf("card timing %s/%s: %v", id, taskID, err)
		writeBoardJSON(w, http.StatusInternalServerError, map[string]string{"error": "timing unavailable"})
		return
	}
	writeBoardJSON(w, http.StatusOK, ct)
}

// handleCardRefused answers the card operations a board does not have.
func (s *server) handleCardRefused(w http.ResponseWriter, r *http.Request) {
	if !s.requireBO(w, r) {
		return
	}
	reason := map[string]string{
		http.MethodPost:   "cards are planned from the product's stories, not created on the board",
		http.MethodPut:    "a card is the task's plan; it is not edited on the board",
		http.MethodDelete: "a task is canceled, not deleted; move it to canceled",
	}[r.Method]
	refuseCard(w, reason)
}

// handleSetLabel is POST /products/{id}/board/cards/{taskId}/label: an operator
// changes a card's label (D-088). A form post with one field, `label`. The
// actor is the session's account, never the request's, and the event names the
// alias, never the address.
func (s *server) handleSetLabel(w http.ResponseWriter, r *http.Request) {
	ss, ok := sessionOf(r)
	if !ok || ss.Email == "" {
		writeBoardJSON(w, http.StatusUnauthorized, map[string]string{"error": "sign in to the back office"})
		return
	}
	id, taskID := r.PathValue("id"), r.PathValue("taskId")
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := r.ParseForm(); err != nil {
		writeBoardJSON(w, http.StatusBadRequest, map[string]string{"error": "the label could not be read"})
		return
	}
	label := strings.TrimSpace(r.PostFormValue("label"))
	res, err := s.mut(r).SetTaskLabel(r.Context(), id, taskID, label, ss.Email)
	if err == nil && len(res.NewEvents) > 0 && s.nt != nil &&
		(label == LabelPRReviewManual || label == LabelPRReviewNeedsManual) {
		// One notice per change: an unchanged label returns no event (D-101).
		if cards, cerr := s.store.PRCards(r.Context(), id); cerr == nil {
			for _, c := range cards {
				if c.ID == taskID {
					go s.nt.Notify(context.WithoutCancel(r.Context()), c, KindReviewManual, "A person must review this pull request")
				}
			}
		}
	}
	if err != nil {
		if wantsHTMX(r) {
			s.fail(w, r, err)
			return
		}
		if errors.Is(err, ErrNotFound) {
			writeBoardJSON(w, http.StatusNotFound, map[string]string{"error": "no such card"})
			return
		}
		writeBoardJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	if wantsHTMX(r) {
		s.renderBOAfter(w, r, res, id, "board")
		return
	}
	writeBoardJSON(w, http.StatusOK, map[string]string{"ok": "label set"})
}
