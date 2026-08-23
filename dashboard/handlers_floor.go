package main

import (
	"net/http"
	"strings"
)

// The floor handlers. Each one renders exactly one named template plus the
// out-of-band chrome, which is the whole HTMX contract: a response is a
// fragment, and the fragment names its own target.

func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	v, _ := s.newView(r)
	s.rend.Render(w, http.StatusOK, "page", v)
}

// handleFloor is the poll target. #carousel refetches it on a timer, which is
// the pass-1 stand-in for the event stream. When the SSE broker lands the
// client attribute changes and this handler does not.
func (s *server) handleFloor(w http.ResponseWriter, r *http.Request) {
	v, _ := s.newView(r)
	s.rend.RenderWithChrome(w, "floor-tick", v)
}

func (s *server) handleRail(w http.ResponseWriter, r *http.Request) {
	v, _ := s.newView(r)
	s.rend.Render(w, http.StatusOK, "rail-oob", v)
}

func (s *server) handleInspector(w http.ResponseWriter, r *http.Request) {
	v, _ := s.newView(r)
	s.rend.Render(w, http.StatusOK, "inspector", v)
}

// handleSelectDesk moves the focus. It is a GET because it changes what the
// operator is looking at, not what the floor is doing -- but it does write
// focusIdx, so the response carries the carousel and the product list out of
// band to keep the highlight in three places consistent.
func (s *server) handleSelectDesk(w http.ResponseWriter, r *http.Request) {
	idx := atoiDefault(r.PathValue("idx"), 0)
	res, err := s.store.Focus(r.Context(), idx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	v := s.applyResult(r, res)
	s.rend.RenderWithChrome(w, "floor-tick", v)
}

// handleMeeting is polled only while a review is in session. The overlay is
// rendered closed when there is no meeting, so the same fragment both opens and
// closes it.
func (s *server) handleMeeting(w http.ResponseWriter, r *http.Request) {
	v, _ := s.newView(r)
	s.rend.Render(w, http.StatusOK, "meeting", v)
}

func (s *server) handleTimeline(w http.ResponseWriter, r *http.Request) {
	v, _ := s.newView(r)
	s.rend.Render(w, http.StatusOK, "tl-row-oob", v)
}

// handleEvents is the §09 route. It answers HTML to HTMX and JSON to anything
// else, so the replay source is readable by a client that is not a browser --
// which is what "GET /events?entity={id}&until={ts}" is for in the document.
func (s *server) handleEvents(w http.ResponseWriter, r *http.Request) {
	evs, err := s.store.Events(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if r.Header.Get("HX-Request") == "" {
		entity := r.URL.Query().Get("entity")
		out := make([]Event, 0, len(evs))
		for _, e := range evs {
			if entity != "" && e.DeskID != entity {
				continue
			}
			out = append(out, e)
		}
		jsonOut(w, map[string]any{"events": out, "count": len(out)})
		return
	}
	v, _ := s.newView(r)
	s.rend.Render(w, http.StatusOK, "tl-row-oob", v)
}

// handleReplay folds the floor back to an event. The {workflow} segment is in
// the path because §09 puts it there; the cursor is ?t=, an event index in this
// pass and a timestamp once the event store is real.
func (s *server) handleReplay(w http.ResponseWriter, r *http.Request) {
	t := atoiDefault(r.URL.Query().Get("t"), -1)
	res, err := s.store.Replay(r.Context(), t)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	v := s.applyResult(r, res)
	s.rend.RenderWithChrome(w, "replay-tick", v)
}

func (s *server) handleGoLive(w http.ResponseWriter, r *http.Request) {
	res, err := s.store.Replay(r.Context(), -1)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.rend.RenderWithChrome(w, "replay-tick", s.applyResult(r, res))
}

func (s *server) handlePause(w http.ResponseWriter, r *http.Request) {
	f, _ := s.store.Snapshot(r.Context())
	res, err := s.mut(r).SetPaused(r.Context(), !f.Stats.Paused)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.rend.RenderWithChrome(w, "floor-ctl", s.applyResult(r, res))
}

func (s *server) handleSpeed(w http.ResponseWriter, r *http.Request) {
	res, err := s.mut(r).SetSpeed(r.Context(), atoiDefault(r.PathValue("n"), 1))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.rend.RenderWithChrome(w, "speed", s.applyResult(r, res))
}

// handleBell clears the notification count and jumps to whatever wanted
// attention -- the bell is not a list, it is a pointer.
func (s *server) handleBell(w http.ResponseWriter, r *http.Request) {
	res, err := s.mut(r).ClearNotifications(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.rend.RenderWithChrome(w, "floor-tick", s.applyResult(r, res))
}

// deskIdx resolves the {id} path segment.
//
// In this pass a "task id" is the desk index, because one desk is one workflow
// and one workflow shows one task. The ROUTE is already the documented
// /tasks/{id}/... so nothing has to be renamed when Task vertices are real and
// this becomes a lookup; only this function changes.
func deskIdx(r *http.Request) int {
	id := r.PathValue("id")
	if i := strings.IndexByte(id, ':'); i >= 0 {
		id = id[:i]
	}
	return atoiDefault(id, 0)
}

// handleDispatch is the operator-triggered half of the §7.6 execution flow:
// freeze the packet, screen it, dispatch what survives. T-126, T-127.
//
// It looks exactly like its neighbours, and that is the point: a refusal comes
// back as a Result with a toast rather than as an error, so this handler needs
// no special case for the thing BR-32 exists to do. The refusal's pill reaches
// the timeline through applyResult like every other event.
//
// s.mut(r) is the whole replay story: parked in the past, the handle is a
// readOnly and readOnly.DispatchTask refuses like its fifteen neighbours.
func (s *server) handleDispatch(w http.ResponseWriter, r *http.Request) {
	res, err := s.mut(r).DispatchTask(r.Context(), deskIdx(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.rend.RenderWithChrome(w, "floor-tick", s.applyResult(r, res))
}

func (s *server) handleApprove(w http.ResponseWriter, r *http.Request) {
	res, err := s.mut(r).ApproveTask(r.Context(), deskIdx(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.rend.RenderWithChrome(w, "floor-tick", s.applyResult(r, res))
}

func (s *server) handleRetry(w http.ResponseWriter, r *http.Request) {
	res, err := s.mut(r).RetryTask(r.Context(), deskIdx(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.rend.RenderWithChrome(w, "floor-tick", s.applyResult(r, res))
}

func (s *server) handleCancel(w http.ResponseWriter, r *http.Request) {
	res, err := s.mut(r).CancelTask(r.Context(), deskIdx(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.rend.RenderWithChrome(w, "floor-tick", s.applyResult(r, res))
}

// handleTaskContext is §09's "the exact slice sent to the model". It renders
// the chips card on its own so the inspector can refresh just that block.
func (s *server) handleTaskContext(w http.ResponseWriter, r *http.Request) {
	idx := deskIdx(r)
	view, err := s.store.TaskContext(r.Context(), idx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if r.Header.Get("HX-Request") == "" {
		// frozen is first because it is the field that decides what the rest of
		// this response is worth. D-6 wants the slice an attempt was actually
		// given; when nothing has run there is none, and a caller reading this as
		// an audit answer has to be able to see that without knowing our history.
		jsonOut(w, map[string]any{
			"task": idx, "frozen": view.Frozen, "digest": view.Digest,
			"assembledAt": view.AssembledAt, "attempt": view.Attempt,
			"context": view.Chips, "cites": view.Cites,
		})
		return
	}
	v, _ := s.newView(r)
	if v.Insp != nil {
		v.Insp.Ctx = view.Chips
		v.Insp.CtxFrozen, v.Insp.CtxDigest = view.Frozen, view.Digest
	}
	s.rend.Render(w, http.StatusOK, "inspector", v)
}

func (s *server) handleCallReview(w http.ResponseWriter, r *http.Request) {
	res, err := s.mut(r).CallReview(r.Context(), atoiDefault(r.PathValue("idx"), 0))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.rend.RenderWithChrome(w, "floor-tick", s.applyResult(r, res))
}

func (s *server) handleCloseMeeting(w http.ResponseWriter, r *http.Request) {
	res, err := s.mut(r).CloseMeeting(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.rend.RenderWithChrome(w, "floor-tick", s.applyResult(r, res))
}
