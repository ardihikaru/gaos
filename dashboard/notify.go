package main

import (
	"context"
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
}

// loginFor finds the account a notification is for. Any failure answers "",
// which sends to the group.
func (n *notifier) loginFor(ctx context.Context, t TaskRecord, kind string) string {
	if n.gh == nil || t.DedupKey == "" {
		return ""
	}
	if owner, repo, num, ok := prKeyParts(t.DedupKey); ok {
		if strings.HasPrefix(kind, "review:") {
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
	if n == nil || n.wa == nil {
		return
	}
	book := ContactBook{}
	if cs, err := n.st.ListContacts(ctx); err == nil {
		for _, c := range cs {
			_ = book.Add(c)
		}
	}
	dest, alias := resolveDestination(book, t.DeskID, n.loginFor(ctx, t, kind), kind)
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
