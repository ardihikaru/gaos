package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
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
	b, err := s.store.Board(r.Context(), r.PathValue("id"))
	if errors.Is(err, ErrNotFound) {
		writeBoardJSON(w, http.StatusNotFound, map[string]string{"error": "no such product"})
		return
	}
	if err != nil {
		writeBoardJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
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
	res, err := s.mut(r).MoveCard(r.Context(), id, taskID, mv)
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
	b, _ := s.store.Board(r.Context(), id)
	for _, c := range b.Cards {
		if c.ID == taskID {
			writeBoardJSON(w, http.StatusOK, map[string]any{"card": c})
			return
		}
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
