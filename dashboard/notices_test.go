package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// The manual-review notices (D-101, docs/designs/gaos-external-tasks-and-pr-bundles.md §8).

// waCounter is a WhatsApp gateway that records every message, safely across goroutines.
type waCounter struct {
	mu   sync.Mutex
	msgs []string
}

func (c *waCounter) handler(rw http.ResponseWriter, r *http.Request) {
	b := make([]byte, 2048)
	n, _ := r.Body.Read(b)
	c.mu.Lock()
	c.msgs = append(c.msgs, string(b[:n]))
	c.mu.Unlock()
	_, _ = rw.Write([]byte(`{"code":"SUCCESS"}`))
}

func (c *waCounter) all() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.msgs...)
}

// settle waits until the count reaches want, or a short while when want is the current count.
func (c *waCounter) settle(want int) []string {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(c.all()) < want {
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	return c.all()
}

func noticeServer(t *testing.T) (*server, *MemStore, *waCounter) {
	t.Helper()
	srv, st := newTestServer(t)
	c := &waCounter{}
	w, _ := waTestNotifier(t, c.handler)
	srv.nt = &notifier{st: st, wa: w, gh: fakeAuthors{login: "dev"}, logf: func(string, ...any) {}}
	if _, err := st.UpsertContact(context.Background(), Contact{GitHubLogin: "dev", DisplayName: "Dev", Destination: "555@s.whatsapp.net"}, "o@x.y"); err != nil {
		t.Fatal(err)
	}
	return srv, st, c
}

func TestManualLabelSendsOneNotice(t *testing.T) {
	srv, st, wa := noticeServer(t)
	planned, _, _, err := st.PlanPullRequestCards(context.Background(), "p-1", prEvent(prTypeOpened, "laundry-api", 31))
	if err != nil {
		t.Fatal(err)
	}
	path := "/products/p-1/board/cards/" + planned[0] + "/label"
	if w := formPost(t, srv, path, "label="+LabelPRReviewManual, true); w.Code != http.StatusOK {
		t.Fatalf("the label route answered %d", w.Code)
	}
	msgs := wa.settle(1)
	if len(msgs) != 1 || !strings.Contains(msgs[0], "555@s.whatsapp.net") || !strings.Contains(msgs[0], "A person must review") {
		t.Fatalf("an operator's manual label must send one line to the author's contact, got %v", msgs)
	}
	formPost(t, srv, path, "label="+LabelPRReviewManual, true)
	formPost(t, srv, path, "label="+LabelPRReviewAuto, true)
	if got := wa.settle(1); len(got) != 1 {
		t.Errorf("the same label again, or a return to auto, must send nothing; %d lines", len(got))
	}
	formPost(t, srv, path, "label="+LabelPRReviewNeedsManual, true)
	if got := wa.settle(2); len(got) != 2 {
		t.Errorf("needs-manual set by an operator must send one line, got %d", len(got))
	}
	// A client asking for a manual review: the group, naming the client; adoption sends nothing.
	tsrv, tst, _ := tasksFixture(t, openPR())
	twa := &waCounter{}
	w, _ := waTestNotifier(t, twa.handler)
	tsrv.nt = &notifier{st: tst, wa: w, gh: fakeAuthors{login: "dev"}, logf: func(string, ...any) {}}
	tst.UpsertContact(context.Background(), Contact{GitHubLogin: "dev", DisplayName: "Dev", Destination: "555@s.whatsapp.net"}, "o@x.y")
	body := taskBody(APIKindPRReview, "m1", "loop/laundry-api#44", "review", `"manual"`)
	if rec := call(t, tsrv, "POST", "/v1/products/p-1/tasks", tokTasks, body); rec.Code != 201 {
		t.Fatalf("the manual review answered %d %s", rec.Code, rec.Body)
	}
	got := twa.settle(1)
	if len(got) != 1 || !strings.Contains(got[0], testGroup) || !strings.Contains(got[0], "Client plat asked for a manual review") {
		t.Errorf("an API manual review must reach the group, naming the client, got %v", got)
	}
	if _, _, _, err := tst.PlanPullRequestCards(context.Background(), "p-1", prEvent(prTypeOpened, "laundry-api", 45)); err != nil {
		t.Fatal(err)
	}
	call(t, tsrv, "POST", "/v1/products/p-1/tasks", tokTasks2, taskBody(APIKindPRReview, "m2", "loop/laundry-api#45", "review", `"manual"`))
	if got := twa.settle(1); len(got) != 1 {
		t.Errorf("adopting a card must send no notice, got %d lines", len(got))
	}
}

func TestHeldAndHighReviewsSendOneLine(t *testing.T) {
	st, card := prReviewFixture(t)
	var kinds, texts []string
	rv := &prReviewer{st: st, gh: &fakePoster{}, logf: func(string, ...any) {},
		notify: func(_ context.Context, _ TaskRecord, kind, text string) { kinds = append(kinds, kind); texts = append(texts, text) }}
	rv.afterRun(context.Background(), card, prReplyOf("low", "found it: API_TOKEN=abcdef123456789"))
	if len(kinds) != 1 || kinds[0] != KindReviewNeedsManual {
		t.Errorf("a held review must send one review:needs-manual line whatever its severity, got %v", kinds)
	}
	if got := st.taskByIDLocked(card.ID).BlockReason; got != RuleSecretHeld {
		t.Errorf("a held review must carry the secret-held reason, got %q", got)
	}
	st2, card2 := prReviewFixture(t)
	kinds, texts = nil, nil
	rv.st = st2
	rv.afterRun(context.Background(), card2, prReplyOf("high", "a.go:1 — x. Reason: y."))
	if len(kinds) != 1 || kinds[0] != "review:high" {
		t.Errorf("a high review must send review:high only, got %v", kinds)
	}
	if len(texts) == 1 && !strings.Contains(texts[0], "a person must take over") {
		t.Errorf("a high review's line must say a person must take over, got %q", texts[0])
	}
}

func TestNotifyOnWantsManualKindsExactly(t *testing.T) {
	for _, k := range []string{KindReviewManual, KindReviewNeedsManual, "pr:" + alertNoTaskCode} {
		if err := ValidContact(Contact{GitHubLogin: "dev", Destination: "555@s.whatsapp.net", NotifyOn: []string{k}}); err != nil {
			t.Errorf("a contact must be able to opt into %s: %v", k, err)
		}
	}
	medium := Contact{NotifyOn: []string{"review:medium"}}
	if notifyOnWants(medium, KindReviewManual) || notifyOnWants(medium, KindReviewNeedsManual) {
		t.Error("a review:medium contact must not get the manual-review kinds through the severity rank")
	}
	if !notifyOnWants(medium, "review:high") {
		t.Error("a review:medium contact must still get review:high")
	}
	if !notifyOnWants(Contact{}, KindReviewManual) || !notifyOnWants(Contact{NotifyOn: []string{KindReviewManual}}, KindReviewManual) {
		t.Error("an empty list or an exact entry must get review:manual")
	}
}

var _ = httptest.NewRecorder
