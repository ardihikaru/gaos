package main

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

type fakeAuthors struct{ login string }

func (f fakeAuthors) pullRequest(context.Context, string, string, int) (ghPR, error) {
	var p ghPR
	p.User.Login = f.login
	return p, nil
}
func (f fakeAuthors) pullCommitAuthors(context.Context, string, string, int) ([]string, error) {
	return []string{f.login}, nil
}
func (f fakeAuthors) commitAuthor(context.Context, string, string, string) (string, error) {
	return f.login, nil
}

func notifyFixture(t *testing.T, status int) (*notifier, *MemStore, *[]string) {
	t.Helper()
	var phones []string
	w, _ := waTestNotifier(t, func(rw http.ResponseWriter, r *http.Request) {
		b := make([]byte, 1024)
		n, _ := r.Body.Read(b)
		phones = append(phones, string(b[:n]))
		if status != 200 {
			rw.WriteHeader(status)
			return
		}
		_, _ = rw.Write([]byte(`{"code":"SUCCESS"}`))
	})
	st := NewMemStore(1)
	var logs []string
	return &notifier{st: st, wa: w, gh: fakeAuthors{login: "dev"}, logf: func(f string, a ...any) { logs = append(logs, f) }}, st, &phones
}

func TestAReviewFindingReachesThePullRequestAuthorsContact(t *testing.T) {
	n, st, phones := notifyFixture(t, 200)
	ctx := context.Background()
	st.UpsertContact(ctx, Contact{GitHubLogin: "dev", DisplayName: "Dev", Destination: "555@g.us"}, "o@x.y")
	task := TaskRecord{ID: "wf-pr-p-1-x-1-pr-reviewer", DeskID: "p-1", DedupKey: "loop/laundry-api#9:pr-reviewer"}
	n.Notify(ctx, task, "review:high", "review held")
	if len(*phones) != 1 || !strings.Contains((*phones)[0], "555@g.us") || !strings.Contains((*phones)[0], task.ID) {
		t.Fatalf("sent %v, want one message to the contact naming the card", *phones)
	}
	if strings.Contains((*phones)[0], "laundry-api") {
		t.Errorf("the message carries the repository: %s", (*phones)[0])
	}
	// The one event: the alias, the kind and the outcome.
	var ev *Event
	for i := range st.events {
		if st.events[i].Type == ContactNotified {
			ev = &st.events[i]
		}
	}
	if ev == nil || strings.Contains(ev.Text, "555") || !strings.Contains(ev.Text, "accepted") {
		t.Errorf("event %+v", ev)
	}
}

func TestAFailureWithNoContactGoesToTheGroupAndAFailedSendIsRecordedNotFatal(t *testing.T) {
	n, st, phones := notifyFixture(t, 429)
	task := TaskRecord{ID: "c1", DeskID: "p-1", DedupKey: "loop/api@abc1234:e2e-run", Status: StatusFailed}
	n.Notify(context.Background(), task, "e2e:failure", "e2e run failed")
	if len(*phones) != 1 || !strings.Contains((*phones)[0], testGroup) {
		t.Fatalf("sent %v, want the pinned group", *phones)
	}
	failed := false
	for _, e := range st.events {
		if e.Type == ContactNotified && strings.Contains(e.Text, "failed") {
			failed = true
		}
	}
	if !failed {
		t.Error("a failed send was not recorded")
	}
	if task.Status != StatusFailed {
		t.Error("a notification must not touch the card")
	}
}

func TestANilNotifierIsSilent(t *testing.T) {
	var n *notifier
	n.Notify(context.Background(), TaskRecord{}, "review:high", "x") // must not panic
	(&notifier{}).Notify(context.Background(), TaskRecord{}, "review:high", "x")
}

func TestAProductTheGroupIsNotLinkedToIsNeverNotified(t *testing.T) {
	n, st, phones := notifyFixture(t, 200)
	task := TaskRecord{ID: "c1", DeskID: "p-2", DedupKey: "loop/api@abc1234:e2e-run"} // the fixture links p-1 only
	n.Notify(context.Background(), task, "e2e:failure", "x")
	if len(*phones) != 0 {
		t.Fatalf("a message went out for a product the group is not linked to: %v", *phones)
	}
	for _, e := range st.events {
		if e.Type == ContactNotified {
			t.Error("an unlinked product left a ContactNotified event")
		}
	}
}

func TestAnApprovalNoticeGoesToTheGroupNotToTheCommitter(t *testing.T) {
	n, st, phones := notifyFixture(t, 200)
	ctx := context.Background()
	st.UpsertContact(ctx, Contact{GitHubLogin: "dev", Destination: "555@g.us"}, "o@x.y")
	task := TaskRecord{ID: "c1", DeskID: "p-1", DedupKey: "loop/laundry-api#9:e2e-automation"}
	n.Notify(ctx, task, "e2e:approval", "e2e spec waits for approval")
	if len(*phones) != 1 || strings.Contains((*phones)[0], "555@g.us") || !strings.Contains((*phones)[0], testGroup) {
		t.Fatalf("sent %v, want the pinned group and not the committer's contact", *phones)
	}
}
