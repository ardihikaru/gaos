package main

import (
	"context"
	"regexp"
	"strings"
	"testing"
)

// The reviewer's own workspace: M17.5, T-189…T-192, D-071.
//
// The claim under test is the one this change exists for: a reviewer run does
// not happen in the author's folder. The reviewer's prompt was already minimal
// and its session already fresh; the workspace was shared, so the reviewer
// could reach the author's files.
//
// Every test here is a top-level func Test…, and each has a row in the QA
// register (pustaka/docs/product/qa-test-plan.html#go-reviewer-workspace).

// reviewFixture is a gated plan on a product with a published Blueprint, and
// the fake gateway that holds the workspaces.
func reviewFixture(t *testing.T) (gateFixture, *fakeAIM, *AIMAdapter) {
	t.Helper()
	f := gateFixtureNew(t)
	ctx := context.Background()
	if _, err := f.s.SavePolicy(ctx, f.desk, Policy{Key: "go-style", Kind: PolicyCoding, Title: "Go style", Body: "gofmt"}); err != nil {
		t.Fatalf("SavePolicy: %v", err)
	}
	if _, err := f.s.PublishBlueprint(ctx, f.desk); err != nil {
		t.Fatalf("PublishBlueprint: %v", err)
	}
	fake, ad := gatewayOn(t, f.s)
	return f, fake, ad
}

// publishedFiles is the file set the published Blueprint wrote.
func publishedFiles(t *testing.T, f gateFixture) []BlueprintFile {
	t.Helper()
	f.s.mu.RLock()
	defer f.s.mu.RUnlock()
	m := f.s.masterPtr(f.desk)
	if m == nil {
		t.Fatalf("no product %q", f.desk)
	}
	v, ok := m.Blueprint.Latest()
	if !ok {
		t.Fatalf("the product %q has no published Blueprint", f.desk)
	}
	return v.Files
}

// spaceFiles is what a workspace holds in the fake gateway.
func spaceFiles(fake *fakeAIM, id string) []WorkspaceFile {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	out := make([]WorkspaceFile, len(fake.spaces[id]))
	copy(out, fake.spaces[id])
	return out
}

// 1 — the review turn runs in the product's own reviewer workspace.
func TestTheReviewTurnRunsInTheProductOwnReviewerWorkspace(t *testing.T) {
	f, fake, ad := reviewFixture(t)
	review := f.taskOfKind(t, TaskKindReview)
	want := ad.ReviewWorkspaceFor(f.desk)

	pkt := f.packetFor(t, review)
	if pkt.WorkspaceID != want {
		t.Fatalf("the review packet names workspace %q; want the reviewer's own %q", pkt.WorkspaceID, want)
	}

	c, _, err := f.s.clearFor(context.Background(), Dispatch{TaskID: review, DeskID: f.desk}, 1)
	if err != nil {
		t.Fatalf("clearFor: %v", err)
	}
	completeTurn(fake, review, 1, "0.1")
	if _, err := ad.Dispatch(context.Background(), c); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	fake.mu.Lock()
	last := fake.bodies[len(fake.bodies)-1]
	fake.mu.Unlock()
	if last.WorkspaceID != want {
		t.Fatalf("the gateway received workspace %q; want the reviewer's own %q", last.WorkspaceID, want)
	}
}

// 2 — the reviewer's workspace is never the product's.
func TestTheReviewerWorkspaceIsNotTheProductWorkspace(t *testing.T) {
	ad := &AIMAdapter{Tenant: "acme"}
	for _, p := range []string{"p-1", "p-2", "p-3"} {
		review, product := ad.ReviewWorkspaceFor(p), ad.WorkspaceFor(p)
		if review == product {
			t.Fatalf("the reviewer for %s runs in the product's own workspace %q", p, product)
		}
	}
}

// 3 — the id obeys the gateway's grammar.
func TestTheReviewerWorkspaceIdObeysTheGatewayIdGrammar(t *testing.T) {
	ad := &AIMAdapter{Tenant: "acme"}
	grammar := regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)
	for _, p := range []string{"p-1", "P-2", "product three", "ümlaut", strings.Repeat("x", 80)} {
		id := ad.ReviewWorkspaceFor(p)
		if !grammar.MatchString(id) {
			t.Fatalf("the reviewer workspace id %q for %q does not match the gateway's grammar", id, p)
		}
	}
	long := strings.Repeat("y", 80)
	if ad.ReviewWorkspaceFor(long) == ad.ReviewWorkspaceFor(long+"z") {
		t.Fatalf("two long product ids collapsed onto one reviewer workspace %q", ad.ReviewWorkspaceFor(long))
	}
}

// 4 — the reviewer's workspace holds the standing files and nothing else.
func TestTheReviewerWorkspaceIsResetToThePublishedBlueprintBeforeEveryReviewTurn(t *testing.T) {
	f, fake, ad := reviewFixture(t)
	review := f.taskOfKind(t, TaskKindReview)
	reviewWS, authorWS := ad.ReviewWorkspaceFor(f.desk), ad.WorkspaceFor(f.desk)

	// The author's folder holds the author's file; the reviewer's folder holds
	// an earlier review's leftover. Neither may survive the reset.
	fake.mu.Lock()
	fake.spaces[authorWS] = append(fake.spaces[authorWS], WorkspaceFile{Path: "notes.txt", Content: "the author's notes"})
	fake.spaces[reviewWS] = append(fake.spaces[reviewWS], WorkspaceFile{Path: "old-review.md", Content: "an earlier review's leftovers"})
	fake.mu.Unlock()

	if _, _, err := f.s.clearFor(context.Background(), Dispatch{TaskID: review, DeskID: f.desk}, 1); err != nil {
		t.Fatalf("clearFor: %v", err)
	}

	want := map[string]bool{}
	for _, file := range publishedFiles(t, f) {
		want[file.Path] = true
	}
	held := spaceFiles(fake, reviewWS)
	for _, file := range held {
		if !want[file.Path] {
			t.Fatalf("the reviewer workspace holds %q, which the published Blueprint did not write; the folder must hold exactly the standing files", file.Path)
		}
	}
	if len(held) != len(want) {
		t.Fatalf("the reviewer workspace holds %d file(s); the published Blueprint wrote %d", len(held), len(want))
	}
}

// 5 — a reviewer workspace that cannot be prepared refuses the dispatch.
func TestAReviewDispatchIsRefusedWhenTheReviewerWorkspaceCannotBePrepared(t *testing.T) {
	f, fake, ad := reviewFixture(t)
	review := f.taskOfKind(t, TaskKindReview)
	fake.mu.Lock()
	fake.adminDown = true
	fake.mu.Unlock()

	_, verdict, err := f.s.clearFor(context.Background(), Dispatch{TaskID: review, DeskID: f.desk}, 1)
	if err == nil {
		t.Fatal("the dispatch was cleared although the reviewer workspace could not be prepared")
	}
	if verdict.Rule != RuleBlueprintDrift || !strings.Contains(verdict.Reason, "(BR-39)") {
		t.Fatalf("the refusal is %+v; want rule %s and a reason carrying (BR-39), so a sync releases the desk", verdict, RuleBlueprintDrift)
	}
	if fake.posts["/v1/chat"] != 0 {
		t.Fatalf("the gateway received %d chat turn(s); a refused dispatch spends nothing", fake.posts["/v1/chat"])
	}
	_ = ad
}

// 6 — a reviewer's turn does not mark the product's workspace drifted.
func TestAReviewTurnDoesNotMarkTheProductWorkspaceDrifted(t *testing.T) {
	f, fake, ad := reviewFixture(t)
	review := f.taskOfKind(t, TaskKindReview)
	reviewWS := ad.ReviewWorkspaceFor(f.desk)

	if _, err := f.s.SyncBlueprint(context.Background(), f.desk); err != nil {
		t.Fatalf("SyncBlueprint: %v", err)
	}
	f.s.mu.RLock()
	before := f.s.masterPtr(f.desk).Workspace.Holds(VendorClaude).Digest
	f.s.mu.RUnlock()

	// The reviewer wrote into its own folder during the turn.
	fake.mu.Lock()
	fake.spaces[reviewWS] = append(fake.spaces[reviewWS], WorkspaceFile{Path: "review-notes.md", Content: "the reviewer's notes"})
	fake.mu.Unlock()
	f.s.checkAfterTurn(context.Background(), f.desk, runIDFor(review, 1), Attempt{})

	f.s.mu.RLock()
	after := f.s.masterPtr(f.desk).Workspace.Holds(VendorClaude).Digest
	f.s.mu.RUnlock()
	if after != before {
		t.Fatalf("the product's workspace digest moved from %q to %q after a reviewer's turn; the author's folder must not be marked drifted by the reviewer's behaviour", before, after)
	}
}

// 7 — the observed drift check still fires for a review turn.
func TestTheObservedDriftCheckStillFiresForAReviewTurn(t *testing.T) {
	f, fake, ad := reviewFixture(t)
	review := f.taskOfKind(t, TaskKindReview)
	if _, err := f.s.SyncBlueprint(context.Background(), f.desk); err != nil {
		t.Fatalf("SyncBlueprint: %v", err)
	}
	f.s.mu.RLock()
	v, _ := f.s.masterPtr(f.desk).Blueprint.Latest()
	f.s.mu.RUnlock()

	// The reviewer rewrote its own CLAUDE.md.
	fake.mu.Lock()
	fake.reply = chatReply("done\n\n"+handoffBlock(review, runIDFor(review, 1)), "0.1",
		observedFor(v, func(o *ObservedContext) {
			o.ClaudeMD = append(o.ClaudeMD, ObservedFile{Path: "/work/CLAUDE.local.md", Bytes: 12, Kind: "local"})
		}))
	fake.mu.Unlock()

	c, _, err := f.s.clearFor(context.Background(), Dispatch{TaskID: review, DeskID: f.desk}, 1)
	if err != nil {
		t.Fatalf("clearFor: %v", err)
	}
	if _, err := f.s.superviseRun(context.Background(), ad, f.desk, c, 1); err != nil {
		t.Fatalf("superviseRun: %v", err)
	}
	var refusals int
	for _, e := range f.s.rawEvents() {
		if e.Type == DispatchRefused && e.Payload.Rule == RuleObservedDrift {
			refusals++
		}
	}
	if refusals != 1 {
		t.Fatalf("a reviewer that rewrote its standing files produced %d observed-drift refusal(s); want one", refusals)
	}
}

// 8 — the Blueprint sync keeps both workspaces in step.
func TestTheBlueprintSyncKeepsBothWorkspacesInStep(t *testing.T) {
	f, fake, ad := reviewFixture(t)
	if _, err := f.s.SyncBlueprint(context.Background(), f.desk); err != nil {
		t.Fatalf("SyncBlueprint: %v", err)
	}
	want := map[string]bool{}
	for _, file := range publishedFiles(t, f) {
		want[file.Path] = true
	}
	for _, id := range []string{ad.WorkspaceFor(f.desk), ad.ReviewWorkspaceFor(f.desk)} {
		held := spaceFiles(fake, id)
		if len(held) != len(want) {
			t.Fatalf("the workspace %s holds %d file(s); the published Blueprint wrote %d", id, len(held), len(want))
		}
		for _, file := range held {
			if !want[file.Path] {
				t.Fatalf("the workspace %s holds %q, which the published Blueprint did not write", id, file.Path)
			}
		}
	}
	f.s.mu.RLock()
	review := f.s.masterPtr(f.desk).Workspace.Review
	f.s.mu.RUnlock()
	if review.ID != ad.ReviewWorkspaceFor(f.desk) {
		t.Fatalf("the record names the reviewer workspace %q; want %q", review.ID, ad.ReviewWorkspaceFor(f.desk))
	}
	if review.Holds(VendorClaude).Digest == "" {
		t.Fatal("the record does not say what the reviewer's copy holds")
	}
}

// 9 — each product keeps its own workspace root.
func TestEachProductKeepsItsOwnWorkspaceRoot(t *testing.T) {
	ad := &AIMAdapter{Tenant: "acme"}
	products := map[string]bool{}
	reviewers := map[string]bool{}
	for _, p := range []string{"p-1", "p-2", "p-3"} {
		product, review := ad.WorkspaceFor(p), ad.ReviewWorkspaceFor(p)
		if products[product] {
			t.Fatalf("two products share the workspace root %q", product)
		}
		if reviewers[review] {
			t.Fatalf("two products share the reviewer workspace %q", review)
		}
		products[product], reviewers[review] = true, true
	}
	if len(products) != 3 || len(reviewers) != 3 {
		t.Fatalf("three products produced %d workspace root(s) and %d reviewer workspace(s); want three of each", len(products), len(reviewers))
	}
}
