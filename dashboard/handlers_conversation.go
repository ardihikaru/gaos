package main

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// The conversation routes (D-082, docs/designs/conversations.md §3.8). All of
// them are back-office: the gate requires a verified session, and a non-GET
// from another origin is refused before it reaches here.
//
//	GET  /ui/conversation/{id}/{taskId}                               the sheet
//	POST /products/{id}/board/cards/{taskId}/conversation/archive     archive
//	POST /products/{id}/board/cards/{taskId}/conversation/restore     restore
//	POST /conversations/prune                                         prune (confirm=PRUNE, card=...)
//	GET  /ui/bo/archive                                               the Archive page (buildBO)

// ArchivePage is the Archive view's model.
type ArchivePage struct {
	Archived []ArchivedRowView
	Pruned   []PruneRowView
	FormText string
	Confirm  string
}

type ArchivedRowView struct {
	ArchivedRow
	When, OpenURL, RestoreURL, Card string
}

type PruneRowView struct {
	PruneRow
	When, Docs, Sessions, OpenURL string
}

func archivePage(v ArchiveView) *ArchivePage {
	p := &ArchivePage{FormText: pruneFormText, Confirm: pruneConfirmWord}
	for _, a := range v.Archived {
		p.Archived = append(p.Archived, ArchivedRowView{ArchivedRow: a, When: absTime(a.ArchivedAt),
			OpenURL: conversationURL(a.ProductID, a.TaskID), Card: a.ProductID + "/" + a.TaskID,
			RestoreURL: conversationBase(a.ProductID, a.TaskID) + "restore"})
	}
	for _, r := range v.Pruned {
		row := PruneRowView{PruneRow: r, When: absTime(r.PrunedAt), OpenURL: conversationURL(r.ProductID, r.TaskID)}
		if b := r.Bodies; b != nil {
			n := 0
			for _, d := range b.Bodies {
				n += d.Bytes
			}
			row.Docs = fmt.Sprintf("%d document(s), %s", len(b.Bodies), byteSize(n))
		}
		if x := r.Sessions; x != nil {
			row.Sessions = fmt.Sprintf("%d deleted · %d gone · %d failed · %d shared, of %d",
				x.SessionsDeleted, x.SessionsGone, x.SessionsFailed, x.SessionsShared, x.SessionsNamed)
		} else {
			row.Sessions = "not recorded"
		}
		p.Pruned = append(p.Pruned, row)
	}
	return p
}

func byteSize(n int) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1<<20:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
}

func conversationBase(productID, taskID string) string {
	return "products/" + url.PathEscape(productID) + "/board/cards/" + url.PathEscape(taskID) + "/conversation/"
}

// actorOf is the signed-in account, "" when the request carries none.
func actorOf(r *http.Request) string {
	ss, _ := sessionOf(r)
	return ss.Email
}

// handleConversationSheet is GET /ui/conversation/{id}/{taskId}.
func (s *server) handleConversationSheet(w http.ResponseWriter, r *http.Request) {
	if !s.requireBO(w, r) {
		return
	}
	c, err := s.store.Conversation(r.Context(), r.PathValue("id"), r.PathValue("taskId"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	canArchive := terminal(c.Status)
	for _, a := range c.Attempts {
		if a.Status == RunStatusStarted || a.Status == RunStatusRunning {
			canArchive = false
		}
	}
	v, _ := s.newView(r)
	v.Talk = buildConversationView(c, canArchive && len(c.Attempts) > 0)
	w.Header().Set("Cache-Control", "no-store")
	s.rend.Render(w, http.StatusOK, "conversation-modal", v)
}

// handleArchiveConversation is POST …/conversation/archive and …/restore.
func (s *server) handleArchiveConversation(archive bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.requireBO(w, r) {
			return
		}
		id, taskID := r.PathValue("id"), r.PathValue("taskId")
		res, err := s.mut(r).ArchiveConversation(r.Context(), id, taskID, actorOf(r), archive)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		v := s.applyResult(r, res)
		if archive {
			v.BO = s.buildBO(r, "products", id, "board")
		} else {
			v.BO = s.buildBO(r, "archive", "", "")
		}
		s.rend.RenderWithChrome(w, "bo-main-close-talk", v)
	}
}

// handlePrune is POST /conversations/prune.
func (s *server) handlePrune(w http.ResponseWriter, r *http.Request) {
	if !s.requireBO(w, r) {
		return
	}
	_ = r.ParseForm()
	if r.PostFormValue("confirm") != pruneConfirmWord {
		s.fail(w, r, reject("Type PRUNE to confirm; nothing was deleted."))
		return
	}
	var cards []CardRef
	for _, c := range r.PostForm["card"] {
		pid, tid, ok := strings.Cut(c, "/")
		if ok && pid != "" && tid != "" {
			cards = append(cards, CardRef{ProductID: pid, TaskID: tid})
		}
	}
	if len(cards) == 0 {
		s.fail(w, r, reject("Nothing to prune."))
		return
	}
	res, err := s.mut(r).PruneConversations(r.Context(), actorOf(r), cards)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	v := s.applyResult(r, res)
	v.BO = s.buildBO(r, "archive", "", "")
	s.rend.RenderWithChrome(w, "bo-main-close-talk", v)
}
