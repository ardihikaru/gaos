package main

import (
	"errors"
	"net/http"
	"strings"
)

// The home page and the documented JSON surface (architecture §09). The floor
// UI these sat beside -- the carousel, the inspector, the meeting room, the
// timeline strip and their controls -- was retired in M14.7 (T-167): a product
// is managed on its own page, and its work on its board.

// handleIndex is the product master. Signed in, it opens on the product list;
// signed out, on the sign-in sheet. There is nothing behind either: the
// product list IS the home page (T-168).
func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	v, _ := s.newView(r)
	if v.SignedIn {
		v.BO = s.buildBO(r, "products", "", "brd")
	} else {
		v.ShowLogin = true
	}
	s.rend.Render(w, http.StatusOK, "page", v)
}

// handleEvents is the §09 route: the stream as JSON, optionally for one
// entity, which is what "GET /events?entity={id}" is for in the document.
func (s *server) handleEvents(w http.ResponseWriter, r *http.Request) {
	evs, err := s.store.Events(r.Context())
	if err != nil {
		jsonStatus(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	entity := r.URL.Query().Get("entity")
	out := make([]Event, 0, len(evs))
	for _, e := range evs {
		if entity != "" && e.DeskID != entity {
			continue
		}
		out = append(out, e)
	}
	jsonOut(w, map[string]any{"events": out, "count": len(out)})
}

// handleReplay parks the store at an event, or returns it to live with t=-1.
// While parked every mutation is refused (BR-20) -- the board and the
// Blueprint included, since both go through s.mut(r).
func (s *server) handleReplay(w http.ResponseWriter, r *http.Request) {
	t := atoiDefault(r.URL.Query().Get("t"), -1)
	res, err := s.store.Replay(r.Context(), t)
	if err != nil {
		jsonStatus(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	jsonOut(w, map[string]any{"replay": res.Floor.Stats.Replay, "at": res.Floor.Stats.ReplayAt, "toast": res.Toast})
}

// deskIdx resolves the {id} path segment of the desk routes below. They are
// kept as an API until the task fold (TC-308) retires the desk projection;
// the board's moves, keyed by task id, are what the UI uses.
func deskIdx(r *http.Request) int {
	id := r.PathValue("id")
	if i := strings.IndexByte(id, ':'); i >= 0 {
		id = id[:i]
	}
	return atoiDefault(id, 0)
}

// taskResult answers a desk mutation: 200 with the toast and the events it
// wrote, or 409 with the refusal. A refused dispatch is a 200 -- BR-32's
// refusal is an event, not an error -- and its DispatchRefused is in events.
func taskResult(w http.ResponseWriter, res Result, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		jsonStatus(w, http.StatusNotFound, map[string]string{"error": "no such task"})
	case err != nil:
		jsonStatus(w, http.StatusConflict, map[string]string{"error": err.Error()})
	default:
		// Without payloads, as Events() answers: the payload is the fold's
		// input, not part of what the seam hands a caller.
		ev := make([]Event, 0, len(res.NewEvents))
		for _, e := range res.NewEvents {
			e.Payload = EventPayload{}
			ev = append(ev, e)
		}
		jsonOut(w, map[string]any{"toast": res.Toast, "events": ev})
	}
}

// handleDispatch is the operator-triggered half of the §7.6 execution flow:
// freeze the packet, screen it, dispatch what survives. T-126, T-127.
// s.mut(r) is the whole replay story: parked in the past, the handle is a
// readOnly and readOnly.DispatchTask refuses.
func (s *server) handleDispatch(w http.ResponseWriter, r *http.Request) {
	res, err := s.mut(r).DispatchTask(r.Context(), deskIdx(r))
	taskResult(w, res, err)
}

func (s *server) handleApprove(w http.ResponseWriter, r *http.Request) {
	res, err := s.mut(r).ApproveTask(r.Context(), deskIdx(r))
	taskResult(w, res, err)
}

func (s *server) handleRetry(w http.ResponseWriter, r *http.Request) {
	res, err := s.mut(r).RetryTask(r.Context(), deskIdx(r))
	taskResult(w, res, err)
}

func (s *server) handleCancel(w http.ResponseWriter, r *http.Request) {
	res, err := s.mut(r).CancelTask(r.Context(), deskIdx(r))
	taskResult(w, res, err)
}

// handleTaskContext is §09's "the exact slice sent to the model".
func (s *server) handleTaskContext(w http.ResponseWriter, r *http.Request) {
	idx := deskIdx(r)
	view, err := s.store.TaskContext(r.Context(), idx)
	if err != nil {
		taskResult(w, Result{}, err)
		return
	}
	// frozen is first because it is the field that decides what the rest of
	// this response is worth. D-6 wants the slice an attempt was actually
	// given; when nothing has run there is none, and a caller reading this as
	// an audit answer has to be able to see that without knowing our history.
	jsonOut(w, map[string]any{
		"task": idx, "frozen": view.Frozen, "digest": view.Digest,
		"assembledAt": view.AssembledAt, "attempt": view.Attempt,
		"context": view.Chips, "cites": view.Cites,
	})
}
