package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// The Pull requests page's rows (D-092, docs/designs/gaos-pr-overview.md §3, §4). Owners, repositories
// and tasks are invented (D-085).

const prRepoOwned = "loop/laundry-api" // a repository NewMemStore's product p-1 follows

func rowOf(t *testing.T, s *MemStore, key string) (PullRequest, bool) {
	t.Helper()
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.pullRequests[key]
	return p, ok
}

func TestPullRequestRowsAreOwnedAndKeepTheirKind(t *testing.T) {
	s := NewMemStore(1)
	ctx := context.Background()
	key := pullRequestKey("p-1", prRepoOwned, 5)
	if _, err := s.UpsertPullRequest(ctx, PullRequest{ProductID: "p-1", Repo: prRepoOwned, Number: 5, Kind: PRKindReview, TaskID: "T-1"}); err != nil {
		t.Fatal(err)
	}
	// A second write fills EMPTY fields only: a title is learned, a task is never replaced.
	if _, err := s.UpsertPullRequest(ctx, PullRequest{ProductID: "p-1", Repo: prRepoOwned, Number: 5, Kind: PRKindReview, Title: "fix a thing", TaskID: "T-2"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertPullRequest(ctx, PullRequest{ProductID: "p-1", Repo: prRepoOwned, Number: 5, Kind: PRKindReview, Title: "another title"}); err != nil {
		t.Fatal(err)
	}
	if p, _ := rowOf(t, s, key); p.Title != "fix a thing" || p.TaskID != "T-1" || p.Kind != PRKindReview {
		t.Errorf("the row is %+v; want the first title and task kept", p)
	}
	// A repository the product does not follow is refused.
	if _, err := s.UpsertPullRequest(ctx, PullRequest{ProductID: "p-1", Repo: "acme-org/elsewhere", Number: 1, Kind: PRKindReview}); !errors.Is(err, ErrRejected) {
		t.Errorf("an unowned repository gave %v, want a rejection", err)
	}
	// A QA row needs the repository in AutomationRepos, which is not Master.Repos.
	qa := PullRequest{ProductID: "p-1", Repo: "loop/laundry-auto", Number: 9, Kind: PRKindQA, QAJob: "job-1", QACase: "CART-12"}
	if _, err := s.UpsertPullRequest(ctx, qa); !errors.Is(err, ErrRejected) {
		t.Errorf("a QA row with no automation repository gave %v, want a rejection", err)
	}
	s.mu.Lock()
	s.masterPtr("p-1").AutoFix.AutomationRepos = []string{"loop/laundry-auto"}
	s.mu.Unlock()
	if _, err := s.UpsertPullRequest(ctx, qa); err != nil {
		t.Fatalf("a QA row of a listed repository: %v", err)
	}
	// The kind never changes: a review write for a QA row changes nothing.
	if _, err := s.UpsertPullRequest(ctx, PullRequest{ProductID: "p-1", Repo: "loop/laundry-auto", Number: 9, Kind: PRKindReview}); !errors.Is(err, ErrRejected) {
		t.Errorf("a review write for a repository only the QA tool uses gave %v", err)
	}
	if p, _ := rowOf(t, s, pullRequestKey("p-1", "loop/laundry-auto", 9)); p.Kind != PRKindQA {
		t.Errorf("the kind became %q", p.Kind)
	}
	// A branch name that is not one is dropped, never stored.
	if _, err := s.UpsertPullRequest(ctx, PullRequest{ProductID: "p-1", Repo: prRepoOwned, Number: 6, Kind: PRKindReview, HeadRef: "bad name; <b>", BaseRef: "develop"}); err != nil {
		t.Fatal(err)
	}
	if p, _ := rowOf(t, s, pullRequestKey("p-1", prRepoOwned, 6)); p.HeadRef != "" || p.BaseRef != "develop" {
		t.Errorf("branches %q -> %q; want the invalid head dropped", p.HeadRef, p.BaseRef)
	}
}

func TestAReviewEventDoesNotShadowAFixPR(t *testing.T) {
	s := NewMemStore(1)
	ctx := context.Background()
	key := pullRequestKey("p-1", prRepoOwned, 50)
	_, _, res, err := s.PlanPullRequestCards(ctx, "p-1", prEvent(prTypeOpened, "laundry-api", 50))
	if err != nil || len(res.PullRequests) != 1 {
		t.Fatalf("an opened event wrote %d rows (%v), want 1", len(res.PullRequests), err)
	}
	row, _ := rowOf(t, s, key)
	if row.Kind != PRKindReview || row.State != PRStateOpen || row.TaskID == "" {
		t.Errorf("the row is %+v; want an open review row with its reviewer card", row)
	}
	// A repeated event, with the row lost, fills it again (the write is before the early return).
	s.mu.Lock()
	delete(s.pullRequests, key)
	s.mu.Unlock()
	if _, _, res, err = s.PlanPullRequestCards(ctx, "p-1", prEvent(prTypeOpened, "laundry-api", 50)); err != nil || len(res.PullRequests) != 1 {
		t.Errorf("a repeated event wrote %d rows (%v), want the missing row back", len(res.PullRequests), err)
	}
	// A merge event writes no row.
	if _, _, res, _ = s.PlanPullRequestCards(ctx, "p-1", prEvent(prTypeMerged, "laundry-api", 51)); len(res.PullRequests) != 0 {
		t.Errorf("a merge event wrote %d rows", len(res.PullRequests))
	}
	// A pull request that is a fix record is never shadowed by a review row.
	s.mu.Lock()
	s.fixPRs["fx-1"] = FixPR{ID: "fx-1", ProductID: "p-1", Repo: prRepoOwned, Number: 77, State: FixPRDraft}
	s.mu.Unlock()
	if _, _, res, _ = s.PlanPullRequestCards(ctx, "p-1", prEvent(prTypeOpened, "laundry-api", 77)); len(res.PullRequests) != 0 {
		t.Errorf("an event for a fix pull request wrote %d review rows", len(res.PullRequests))
	}
	if _, ok := rowOf(t, s, pullRequestKey("p-1", prRepoOwned, 77)); ok {
		t.Error("a review row shadows a fix pull request")
	}
}

func TestReviewRowsAreBackfilledFromExistingCards(t *testing.T) {
	s := NewMemStore(1)
	ctx := context.Background()
	for _, n := range []int{50, 51} {
		if _, _, _, err := s.PlanPullRequestCards(ctx, "p-1", prEvent(prTypeOpened, "laundry-api", n)); err != nil {
			t.Fatal(err)
		}
	}
	s.mu.Lock()
	s.pullRequests = map[string]PullRequest{} // rows from before this release
	s.mu.Unlock()
	res, err := s.BackfillReviewRows(ctx)
	if err != nil || len(res.PullRequests) != 2 {
		t.Fatalf("the back-fill wrote %d rows (%v), want 2", len(res.PullRequests), err)
	}
	if res, _ = s.BackfillReviewRows(ctx); len(res.PullRequests) != 0 {
		t.Errorf("a second back-fill wrote %d rows, want none", len(res.PullRequests))
	}
}

// countingStore counts the rows a runner writes.
type countingStore struct {
	Store
	upserts int
}

func (c *countingStore) UpsertPullRequest(ctx context.Context, p PullRequest) (Result, error) {
	c.upserts++
	return c.Store.UpsertPullRequest(ctx, p)
}

func qaRunnerWithPR(t *testing.T, pr, link string, automation []string, statuses ...string) (*qaJobRunner, *MemStore, *countingStore, string) {
	t.Helper()
	qa := &fakeQAJobs{statuses: statuses, pr: pr, link: link}
	r, st, _, e2e := e2eFixture(t, qa, "what changed\nQA-case: CART-12\n")
	st.mu.Lock()
	st.masterPtr("p-1").AutoFix.AutomationRepos = automation
	st.mu.Unlock()
	cs := &countingStore{Store: st}
	r.st = cs
	return r, st, cs, e2e
}

func TestAQAPullRequestOfAnUnlistedRepoMakesNoRowAndNoCall(t *testing.T) {
	r, st, cs, _ := qaRunnerWithPR(t, "https://github.com/acme-org/elsewhere/pull/9", "", []string{"loop/laundry-auto"}, "running", "succeeded")
	for i := 0; i < 3; i++ {
		r.Tick(context.Background())
	}
	st.mu.RLock()
	n := 0
	for _, p := range st.pullRequests {
		if p.Kind == PRKindQA {
			n++
		}
	}
	st.mu.RUnlock()
	if n != 0 {
		t.Errorf("a pull request of an unlisted repository made %d QA rows", n)
	}
	if cs.upserts > 1 {
		t.Errorf("the refused row was tried %d times; once is the rule", cs.upserts)
	}
}

func TestQAJobLinkIsKeptOnlyOnTheQAHost(t *testing.T) {
	const origin = "qa.example.test"
	for name, c := range map[string]struct{ origin, raw, want string }{
		"the QA host":               {origin, "https://qa.example.test/jobs/j1", "https://qa.example.test/jobs/j1"},
		"upper case host":           {origin, "https://QA.example.test/jobs/j1", "https://QA.example.test/jobs/j1"},
		"another host":              {origin, "https://evil.example.test/jobs/j1", ""},
		"no origin set":             {"", "https://qa.example.test/jobs/j1", ""},
		"an empty host":             {origin, "https:///jobs/j1", ""},
		"a different port":          {origin, "https://qa.example.test:8443/jobs/j1", ""},
		"the port is in the origin": {"qa.example.test:8443", "https://qa.example.test:8443/jobs/j1", "https://qa.example.test:8443/jobs/j1"},
		"plain http":                {origin, "http://qa.example.test/jobs/j1", ""},
		"user info":                 {origin, "https://user:pw@qa.example.test/jobs/j1", ""},
		"a query":                   {origin, "https://qa.example.test/jobs/j1?next=x", ""},
		"a fragment":                {origin, "https://qa.example.test/jobs/j1#x", ""},
		"a javascript url":          {origin, "javascript:alert(1)", ""},
	} {
		if got := qaLinkOK(c.origin, c.raw); got != c.want {
			t.Errorf("%s: qaLinkOK(%q, %q) = %q, want %q", name, c.origin, c.raw, got, c.want)
		}
	}
	for name, c := range map[string]struct {
		raw string
		ok  bool
	}{
		"a pull request":   {"https://github.com/acme-org/sample-automation/pull/7", true},
		"a trailing slash": {"https://github.com/acme-org/sample-automation/pull/7/", true},
		"another host":     {"https://gitlab.com/acme-org/sample-automation/pull/7", false},
		"an issue":         {"https://github.com/acme-org/sample-automation/issues/7", false},
		"a query":          {"https://github.com/acme-org/sample-automation/pull/7?x=1", false},
		"a files tab":      {"https://github.com/acme-org/sample-automation/pull/7/files", false},
		"zero":             {"https://github.com/acme-org/sample-automation/pull/0", false},
	} {
		if _, _, _, ok := parseGitHubPRURL(c.raw); ok != c.ok {
			t.Errorf("%s: parseGitHubPRURL(%q) ok=%v, want %v", name, c.raw, ok, c.ok)
		}
	}
}

func TestQAPullRequestIsRecordedOnceOnTheFinalPoll(t *testing.T) {
	// The job reports its pull request only on its final poll, as it ends: the row is still written.
	r, st, cs, e2e := qaRunnerWithPR(t, "https://github.com/loop/laundry-auto/pull/9", "https://qa.example.test/jobs/job-1",
		[]string{"loop/laundry-auto"}, "running", "succeeded")
	t.Setenv("GAOS_QA_ORIGIN", "qa.example.test")
	r.linkOrigin = "qa.example.test"
	for i := 0; i < 4; i++ {
		r.Tick(context.Background())
	}
	row, ok := rowOf(t, st, pullRequestKey("p-1", "loop/laundry-auto", 9))
	if !ok {
		t.Fatal("no QA row after the job ended")
	}
	if row.Kind != PRKindQA || row.QAJob != "job-1" || row.QACase != "CART-12" || row.TaskID != e2e || row.QALink != "https://qa.example.test/jobs/job-1" {
		t.Errorf("the row is %+v", row)
	}
	if cs.upserts != 1 {
		t.Errorf("the row was written %d times, want once", cs.upserts)
	}
	// A job the runner adopted after a restart does not know its case, and never asks GitHub for it.
	r2, st2, _, e2e2 := qaRunnerWithPR(t, "https://github.com/loop/laundry-auto/pull/10", "", []string{"loop/laundry-auto"}, "succeeded")
	r2.jobs[e2e2] = &qaTracked{jobID: "job-9", card: TaskRecord{ID: e2e2, DeskID: "p-1"}, lastPoll: time.Now()}
	r2.Tick(context.Background())
	if row, ok := rowOf(t, st2, pullRequestKey("p-1", "loop/laundry-auto", 10)); !ok || row.QACase != "" || row.QALink != "" {
		t.Errorf("an adopted job's row is %+v (found %v); want it with no case and no link", row, ok)
	}
	if !strings.Contains(row.QAJob, "job") {
		t.Errorf("the job id is %q", row.QAJob)
	}
}
