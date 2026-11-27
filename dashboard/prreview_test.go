package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func prReplyOf(sev, body string) string {
	return "Done.\n```" + PRReviewFence + "\nseverity: " + sev + "\nbody: " + body + "\n```\n```gaos-handoff\nx\n```"
}

func TestParsePRReview(t *testing.T) {
	r, err := parsePRReview(prReplyOf("medium", "a.go:1 — x. Reason: y."))
	if err != nil || r.Severity != "medium" || !strings.Contains(r.Body, "a.go:1") {
		t.Fatalf("got %+v, %v", r, err)
	}
	// Multi-line body.
	r, err = parsePRReview("```gaos-pr-review\nseverity: low\nbody: |\n  one\n  two\n```")
	if err != nil || !strings.Contains(r.Body, "two") {
		t.Fatalf("multi-line: %+v %v", r, err)
	}
	for name, reply := range map[string]string{
		"no block":         "nothing here",
		"unknown severity": prReplyOf("catastrophic", "x"),
		"empty body":       "```gaos-pr-review\nseverity: low\nbody:\n```",
		"too long":         prReplyOf("low", strings.Repeat("a", prReviewMaxBody+1)),
		"unclosed":         "```gaos-pr-review\nseverity: low\nbody: x",
	} {
		if _, err := parsePRReview(reply); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
}

func TestTheReviewerContractNamesTheBlockAndTheHandoff(t *testing.T) {
	c := contractsFor(TaskKindImplement, "t-1", "t-1-r1", "pr-reviewer")
	if !strings.Contains(c, PRReviewFence) || !strings.Contains(c, "gaos-handoff") {
		t.Errorf("the contract is missing a block: %q", c[:80])
	}
	if strings.Contains(contractsFor(TaskKindImplement, "t-1", "r", "backend"), PRReviewFence) {
		t.Error("another role must not owe a review block")
	}
}

type fakePoster struct {
	posted []string
	events []string
	err    error
}

func (f *fakePoster) postReview(_ context.Context, _, _ string, _ int, body, event string) error {
	f.posted = append(f.posted, body)
	f.events = append(f.events, event)
	return f.err
}

func prReviewFixture(t *testing.T) (*MemStore, TaskRecord) {
	t.Helper()
	st := NewMemStore(1)
	planned, _, _, err := st.PlanPullRequestCards(context.Background(), "p-1", prEvent(prTypeOpened, "laundry-api", 77))
	if err != nil {
		t.Fatal(err)
	}
	// The card has run: it is in REVIEW.
	st.mu.Lock()
	tk := st.taskByIDLocked(planned[0])
	tk.Status = StatusReview
	c := tk.Clone()
	st.mu.Unlock()
	return st, c
}

func TestAReviewIsPostedAsACommentAndTheCardIsDone(t *testing.T) {
	st, card := prReviewFixture(t)
	fp := &fakePoster{}
	var notified []string
	rv := &prReviewer{st: st, gh: fp, logf: func(string, ...any) {},
		notify: func(_ context.Context, _ TaskRecord, kind, _ string) { notified = append(notified, kind) }}
	rv.afterRun(context.Background(), card, prReplyOf("medium", "a.go:1 — x. Reason: y."))
	if len(fp.posted) != 1 {
		t.Fatalf("posted %d reviews, want 1", len(fp.posted))
	}
	if got := st.taskByIDLocked(card.ID).Status; got != StatusDone {
		t.Errorf("the card is %s, want DONE", got)
	}
	if len(notified) != 1 || notified[0] != "review:medium" {
		t.Errorf("notified %v, want a medium review", notified)
	}
}

func TestALowFindingPostsButDoesNotNotify(t *testing.T) {
	st, card := prReviewFixture(t)
	notified := 0
	rv := &prReviewer{st: st, gh: &fakePoster{}, logf: func(string, ...any) {},
		notify: func(context.Context, TaskRecord, string, string) { notified++ }}
	rv.afterRun(context.Background(), card, prReplyOf("low", "x"))
	if notified != 0 {
		t.Error("a low finding must not reach a person")
	}
}

func TestABodyCarryingASecretIsNeverPosted(t *testing.T) {
	st, card := prReviewFixture(t)
	fp := &fakePoster{}
	rv := &prReviewer{st: st, gh: fp, logf: func(string, ...any) {}}
	rv.afterRun(context.Background(), card, prReplyOf("high", "found it: API_TOKEN=abcdef123456789"))
	if len(fp.posted) != 0 {
		t.Fatal("a body with a secret was posted")
	}
	got := st.taskByIDLocked(card.ID)
	if !hasLabel(got.Labels, LabelPRReviewNeedsManual) || hasLabel(got.Labels, LabelPRReviewAuto) {
		t.Errorf("labels %v, want needs-manual in place of auto", got.Labels)
	}
}

func TestAHighFindingIsPostedAndHandedToAPerson(t *testing.T) {
	st, card := prReviewFixture(t)
	rv := &prReviewer{st: st, gh: &fakePoster{}, logf: func(string, ...any) {}}
	rv.afterRun(context.Background(), card, prReplyOf("critical", "bad. Reason: worse."))
	got := st.taskByIDLocked(card.ID)
	if got.Status != StatusReview || !hasLabel(got.Labels, LabelPRReviewNeedsManual) {
		t.Errorf("status %s labels %v; want REVIEW and needs-manual", got.Status, got.Labels)
	}
}

func TestAFailedPostFailsTheCardWithAClass(t *testing.T) {
	st, card := prReviewFixture(t)
	rv := &prReviewer{st: st, gh: &fakePoster{err: &ghError{Status: 404}}, logf: func(string, ...any) {}}
	rv.afterRun(context.Background(), card, prReplyOf("low", "x"))
	got := st.taskByIDLocked(card.ID)
	if got.Status != StatusFailed || got.BlockReason != "github-token" {
		t.Errorf("status %s reason %q; want FAILED github-token", got.Status, got.BlockReason)
	}
	// An unreadable reply fails too.
	st2, card2 := prReviewFixture(t)
	(&prReviewer{st: st2, gh: &fakePoster{}, logf: func(string, ...any) {}}).afterRun(context.Background(), card2, "no block")
	if got := st2.taskByIDLocked(card2.ID); got.Status != StatusFailed || got.BlockReason != "review-unreadable" {
		t.Errorf("status %s reason %q", got.Status, got.BlockReason)
	}
	_ = errors.New
}

func TestTheReviewerPacketCarriesThePullRequestAsData(t *testing.T) {
	cache := &prContextCache{}
	task := TaskRecord{Role: "pr-reviewer", DedupKey: "loop/api#5:pr-reviewer"}
	parts := prPacketParts(cache, task)
	if len(parts) != 1 || !strings.Contains(parts[0].Body, "could not be loaded") {
		t.Fatalf("a missing context must say so: %+v", parts)
	}
	cache.put(task.DedupKey, prContext{Title: "T", Body: "B", Author: "A", Diff: "D"})
	parts = prPacketParts(cache, task)
	if len(parts) != 1 || !strings.Contains(parts[0].Body, "untrusted data") || !strings.Contains(parts[0].Body, "## Diff\nD") {
		t.Fatalf("the part is wrong: %+v", parts)
	}
	if prPacketParts(cache, TaskRecord{Role: "backend", DedupKey: "x"}) != nil {
		t.Error("only a reviewer card gets the part")
	}
}

func TestTheContextCacheIsBounded(t *testing.T) {
	c := &prContextCache{}
	for i := 0; i < prContextMax+10; i++ {
		c.put(strings.Repeat("k", i+1), prContext{})
	}
	if len(c.m) != prContextMax {
		t.Errorf("the cache holds %d entries, want %d", len(c.m), prContextMax)
	}
}

func TestPRKeyParts(t *testing.T) {
	o, r, n, ok := prKeyParts("loop/laundry-api#42:pr-reviewer")
	if !ok || o != "loop" || r != "laundry-api" || n != 42 {
		t.Errorf("got %s %s %d %v", o, r, n, ok)
	}
	if _, _, _, ok := prKeyParts("loop/api@abc:e2e-run"); ok {
		t.Error("a staging key has no pull request")
	}
}
