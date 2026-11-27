package main

import (
	"context"
	"strconv"
	"strings"
)

// The notification service (D-088, docs/designs/gaos-pr-agent-flows.md §8.3-8.4).
//
// A finding that needs a person is sent as ONE line and the card's id: the
// findings stay on the card, and no code is pasted into a chat. The destination
// is the contact of the right account (the pull request's author for a review,
// the last committer for a failure), else the pinned group.
//
// A failed send never fails the work: the card already holds the finding, so
// the failure is recorded on the stream and the card's status is untouched.

// authorSource is the slice of the GitHub client that finds who to tell.
type authorSource interface {
	pullRequest(ctx context.Context, owner, repo string, number int) (ghPR, error)
	pullCommitAuthors(ctx context.Context, owner, repo string, number int) ([]string, error)
	commitAuthor(ctx context.Context, owner, repo, sha string) (string, error)
}

type notifier struct {
	st   Store
	wa   *waNotifier
	gh   authorSource // nil: every message goes to the group
	logf func(string, ...any)
	// tm is the TaskMgr link (D-104 §5.3): a login with no contact reaches the TaskMgr person with
	// that checked GitHub login before the group. nil: contacts only.
	tm *taskmgrLink
}

// viaTaskMgr is §5.3's second step. It applies only when the registry answered the group AND no
// contact exists for the login at all: a contact that filters a kind out keeps its opt-out. An
// approval and every bundle kind keep their group routing.
func (n *notifier) viaTaskMgr(ctx context.Context, book ContactBook, productID, login, kind, dest, alias string) (string, string) {
	if dest != "" || n.tm == nil || login == "" || kind == "e2e:approval" || strings.HasPrefix(kind, "bundle:") {
		return dest, alias
	}
	if _, ok := book.Find(productID, login); ok {
		return dest, alias
	}
	m, err := n.st.Master(ctx, productID)
	if err != nil {
		return dest, alias
	}
	if _, ok := n.tm.ready(m); !ok {
		return dest, alias
	}
	if p, ok := n.tm.people.login(login); ok && p.usable() {
		return p.jid(), "tm:" + sanitizeIDPart(p.Username)
	}
	return dest, alias
}

// notifyTo sends one line about a card to a destination chosen by the caller ("" is the group),
// and records it under the alias. The bundle's QA notice uses it (D-104 §5.2).
func (n *notifier) notifyTo(ctx context.Context, t TaskRecord, kind, text, dest, alias string) {
	if n == nil || n.wa == nil || !n.wa.covers(t.DeskID) {
		return
	}
	if dest == "" {
		alias = "group"
	}
	err := n.wa.Send(ctx, dest, text+" · card "+t.ID)
	outcome := "accepted"
	if err != nil {
		outcome = "failed"
	}
	n.logf("wa: notify %s %s", alias, map[bool]string{true: "sent", false: "failed"}[err == nil])
	if _, rerr := n.st.RecordNotification(ctx, t.DeskID, alias, kind, outcome); rerr != nil {
		n.logf("wa: the notification could not be recorded")
	}
}

// loginFor finds the account a notification is for. Any failure answers "",
// which sends to the group.
func (n *notifier) loginFor(ctx context.Context, t TaskRecord, kind string) string {
	// An approval notice is for whoever approves specs in the QA Inbox, not for the
	// committer: it goes to the group.
	// A defect card has no author to find: its message goes to the pinned group,
	// by design (D-091).
	if kind == "e2e:approval" || n.gh == nil || t.DedupKey == "" || strings.HasPrefix(t.DedupKey, "defect:") {
		return ""
	}
	if owner, repo, num, ok := prKeyParts(t.DedupKey); ok {
		if strings.HasPrefix(kind, "review:") || strings.HasPrefix(kind, "pr:") {
			if pr, err := n.gh.pullRequest(ctx, owner, repo, num); err == nil {
				return pr.User.Login
			}
			return ""
		}
		if as, err := n.gh.pullCommitAuthors(ctx, owner, repo, num); err == nil && len(as) > 0 {
			return as[0]
		}
		return ""
	}
	// A staging key: owner/repo@sha:e2e-run.
	if at := strings.Index(t.DedupKey, "@"); at > 0 {
		if owner, repo, ok := ghOwnerRepo(t.DedupKey[:at]); ok {
			rest := t.DedupKey[at+1:]
			if i := strings.Index(rest, ":"); i > 0 {
				if l, err := n.gh.commitAuthor(ctx, owner, repo, rest[:i]); err == nil {
					return l
				}
			}
		}
	}
	return ""
}

// Notify sends one line about a card. kind is "review:<severity>",
// "e2e:failure" or "unit:failure".
func (n *notifier) Notify(ctx context.Context, t TaskRecord, kind, text string) {
	if n == nil || n.wa == nil || !n.wa.covers(t.DeskID) {
		return // off, or the group is not linked to this product
	}
	book := ContactBook{}
	if cs, err := n.st.ListContacts(ctx); err == nil {
		for _, c := range cs {
			_ = book.Add(c)
		}
	}
	login := n.loginFor(ctx, t, kind)
	dest, alias := resolveDestination(book, t.DeskID, login, kind)
	dest, alias = n.viaTaskMgr(ctx, book, t.DeskID, login, kind, dest, alias)
	err := n.wa.Send(ctx, dest, text+" · card "+t.ID)
	outcome := "accepted"
	if err != nil {
		outcome = "failed"
	}
	n.logf("wa: notify %s %s", alias, map[bool]string{true: "sent", false: "failed"}[err == nil])
	if _, rerr := n.st.RecordNotification(ctx, t.DeskID, alias, kind, outcome); rerr != nil {
		n.logf("wa: the notification could not be recorded")
	}
}

// NotifyPR sends one line about owner/repo#n on productID, for a fact with no card (D-100): a
// "pr:" kind goes to the pull request's author's contact, else the group; a "bundle:" kind to the
// group. It records a ContactNotified like Notify. Like every line (D-088), it names no repository:
// it ends "· card <cardID>" when the pull request has a card, else "· PR #<n>".
func (n *notifier) NotifyPR(ctx context.Context, productID, owner, repo string, num int, kind, text, cardID string) {
	if n == nil || n.wa == nil || !n.wa.covers(productID) {
		return
	}
	login := ""
	if strings.HasPrefix(kind, "pr:") && n.gh != nil {
		if pr, err := n.gh.pullRequest(ctx, owner, repo, num); err == nil {
			login = pr.User.Login
		}
	}
	book := ContactBook{}
	if cs, err := n.st.ListContacts(ctx); err == nil {
		for _, c := range cs {
			_ = book.Add(c)
		}
	}
	dest, alias := resolveDestination(book, productID, login, kind)
	dest, alias = n.viaTaskMgr(ctx, book, productID, login, kind, dest, alias)
	suffix := ""
	switch {
	case cardID != "":
		suffix = " · card " + cardID
	case num > 0:
		suffix = " · PR #" + strconv.Itoa(num)
	}
	err := n.wa.Send(ctx, dest, text+suffix)
	outcome := "accepted"
	if err != nil {
		outcome = "failed"
	}
	n.logf("wa: notify %s %s", alias, map[bool]string{true: "sent", false: "failed"}[err == nil])
	if _, rerr := n.st.RecordNotification(ctx, productID, alias, kind, outcome); rerr != nil {
		n.logf("wa: the notification could not be recorded")
	}
}
