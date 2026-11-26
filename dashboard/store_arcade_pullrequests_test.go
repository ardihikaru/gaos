package main

import (
	"context"
	"strings"
	"testing"
)

// The PullRequest vertex and the new Defect and FixPR properties reach the graph in the transaction of
// the change that touched them (D-092, design §3.2, §4.1), against a fake ArcadeDB that records what
// it is sent.

func TestPullRequestsPersistThroughArcade(t *testing.T) {
	g := &fakeGraph{}
	s, ms, _ := arcadeOver(t, g.handler)
	ctx := context.Background()
	ms.mu.Lock()
	m := ms.masterPtr("p-1")
	m.Repos = append(m.Repos, "loop/laundry-api")
	m.AutoFix.AutomationRepos = []string{"loop/laundry-auto"}
	ms.mu.Unlock()
	g.take()

	if _, err := s.UpsertPullRequest(ctx, PullRequest{ProductID: "p-1", Repo: "loop/laundry-api", Number: 5, Kind: PRKindReview, Title: "a title", OpenedAt: 100}); err != nil {
		t.Fatal(err)
	}
	stmts := g.take()
	if countPrefix(stmts, "UPDATE PullRequest ") != 1 {
		t.Errorf("%d PullRequest upserts, want 1 (statements: %d)", countPrefix(stmts, "UPDATE PullRequest "), len(stmts))
	}
	joined := strings.Join(stmts, "\n")
	for _, want := range []string{"productId", "repo", "kind", "state", "openedAt", "checkedAt", "qaLink"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the PullRequest upsert does not carry %q", want)
		}
	}
	// A write that changes nothing writes nothing.
	if _, err := s.UpsertPullRequest(ctx, PullRequest{ProductID: "p-1", Repo: "loop/laundry-api", Number: 5, Kind: PRKindReview, Title: "ignored"}); err != nil {
		t.Fatal(err)
	}
	if n := countPrefix(g.take(), "UPDATE PullRequest "); n != 0 {
		t.Errorf("an unchanged row wrote %d statements", n)
	}
	// The poller's write and the prune.
	key := pullRequestKey("p-1", "loop/laundry-api", 5)
	if _, err := s.AdvancePullRequest(ctx, "p-1", key, PRFields{State: PRStateMerged, BaseRef: "develop", CheckedAt: 50}); err != nil {
		t.Fatal(err)
	}
	if n := countPrefix(g.take(), "UPDATE PullRequest "); n != 1 {
		t.Errorf("the poller's write made %d upserts, want 1", n)
	}
	if _, err := s.PrunePullRequests(ctx, 1000); err != nil {
		t.Fatal(err)
	}
	if n := countPrefix(g.take(), "DELETE FROM PullRequest"); n != 1 {
		t.Errorf("the prune made %d deletes, want 1", n)
	}

	// The four new properties of existing types: the base and the links of a fix record.
	if _, err := s.UpsertFixPR(ctx, FixPR{ID: "fx-1", ProductID: "p-1", DefectKey: "p-1/DEF-1", Repo: "loop/laundry-api",
		State: FixPRPreparing, CreatedAt: 1, UpdatedAt: 1, Reply: "{}", Base: "develop", RetargetedFrom: "fx-0", RetargetedTo: "fx-2"}); err != nil {
		t.Fatal(err)
	}
	joined = strings.Join(g.take(), "\n")
	for _, want := range []string{"baseName", "retargetedFrom", "retargetedTo"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the FixPR upsert does not carry %q", want)
		}
	}
	assertABaseOverrideIsWritten(t)
}

// The override of a defect is written with it, so a restart in the middle of a retarget keeps it.
func assertABaseOverrideIsWritten(t *testing.T) {
	t.Helper()
	g := &fakeGraph{}
	s, ms, _ := arcadeOver(t, g.handler)
	ctx := context.Background()
	ms.mu.Lock()
	m := ms.masterPtr("p-1")
	m.Repos = append(m.Repos, defRepo)
	ms.mu.Unlock()
	importCases(t, ms, "p-1", false, qaCase(defCase1))
	if _, _, err := s.ReportDefects(ctx, "p-1", DefectReportIn{Source: "push", Items: []DefectItem{defItem(DefectKindXfail, "DEF-101", defCase1)}}); err != nil {
		t.Fatal(err)
	}
	ms.mu.Lock()
	ms.defects["p-1/DEF-101"].BaseOverride = "staging"
	ms.mu.Unlock()
	g.take()
	if _, err := s.SetDefectContext(ctx, "p-1", "p-1/DEF-101", DefectContext{Built: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(g.take(), "\n"), "baseOverride") {
		t.Error("the Defect upsert does not carry baseOverride")
	}
}
