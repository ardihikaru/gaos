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
	Archived []ArchivedRowView // this page of them
	Pruned   []PruneRowView    // this page of them
	FormText string
	Confirm  string
	// PruneCards is what one prune takes, and what its confirmation lists:
	// the pruneBatch oldest archived conversations. The typed word is bound to
	// cards the operator has seen (D-082), outside the pager (D-097).
	PruneCards    []ArchivedRowView
	ArchivedTotal int
	ArchivedPager Pager
	PrunedPager   Pager
}

// pruneBatch is the most conversations one prune takes and lists (D-097).
const pruneBatch = 100

// archiveAt is the Archive's address with both tables' pages.
func archiveAt(apage, ppage int) (string, string) {
	q := pageQ("apage", apage)
	if p := pageQ("ppage", ppage); p != "" {
		if q != "" {
			q += "&"
		}
		q += p
	}
	if q == "" {
		return "ui/bo/archive", "archive"
	}
	return "ui/bo/archive?" + q, "archive?" + q
}

type ArchivedRowView struct {
	ArchivedRow
	When, OpenURL, RestoreURL, Card string
}

type PruneRowView struct {
	PruneRow
	When, Docs, Sessions, OpenURL string
}

func archivePage(v ArchiveView, apage, ppage int) *ArchivePage {
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
	// The lists are newest first; a prune takes the oldest.
	p.ArchivedTotal = len(p.Archived)
	p.PruneCards = p.Archived[max(0, len(p.Archived)-pruneBatch):]
	// Each table's links keep the other table's page, clamped first.
	ap, _, _ := clampPage(len(p.Archived), apage, listPageSize)
	pp, _, _ := clampPage(len(p.Pruned), ppage, listPageSize)
	p.ArchivedPager = paginate(len(p.Archived), ap, listPageSize, func(n int) (string, string) { return archiveAt(n, pp) })
	p.PrunedPager = paginate(len(p.Pruned), pp, listPageSize, func(n int) (string, string) { return archiveAt(ap, n) })
	p.ArchivedPager.Label, p.PrunedPager.Label = "Archived pages", "Pruned pages"
	p.Archived = sliceFor(p.Archived, p.ArchivedPager)
	p.Pruned = sliceFor(p.Pruned, p.PrunedPager)
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
			s.sheet(w, r, v, "products", id, "board")
		} else {
			s.sheet(w, r, v, "archive", "", "")
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
	s.sheet(w, r, v, "archive", "", "")
	s.rend.RenderWithChrome(w, "bo-main-close-talk", v)
}
