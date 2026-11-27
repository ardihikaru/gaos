package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// The pull-request flow: which cards a pull-request event plans, that a replay
// plans nothing, and the filters that keep a stranger from spending a turn.

func prEvent(kind, repo string, number int) PREvent {
	return PREvent{Kind: "github", Type: kind, EventID: "gh-1", Seq: 7,
		Org: "loop", Repo: repo, PRNumber: number, Title: "A change"}
}

func prCards(t *testing.T, st Store, product string) map[string]BoardCard {
	t.Helper()
	b, err := st.Board(context.Background(), product)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]BoardCard{}
	for _, c := range b.Cards {
		if strings.HasPrefix(c.ID, "wf-pr-") {
			out[c.ID] = c
		}
	}
	return out
}

func TestPRCardsForIsTheRoutingTable(t *testing.T) {
	opened := prCardsFor(prEvent(prTypeOpened, "sample-service", 42))
	if len(opened) != 2 {
		t.Fatalf("pr.opened plans %d cards, want 2", len(opened))
	}
	if opened[0].Role != "pr-reviewer" || opened[0].Labels[0] != LabelPRReviewAuto {
		t.Errorf("the first card is %+v, want the PR reviewer, labelled auto", opened[0])
	}
	if opened[1].Role != "qa" || opened[1].Labels[0] != LabelE2EAutomation {
		t.Errorf("the second card is %+v, want the QA-tool e2e automation card", opened[1])
	}
	merged := prCardsFor(prEvent(prTypeMerged, "sample-service", 42))
	if len(merged) != 1 || merged[0].Role != "unit-test" {
		t.Fatalf("pr.merged plans %+v, want the unit test card alone", merged)
	}
	if got := prCardsFor(prEvent("pr.closed", "sample-service", 42)); got != nil {
		t.Errorf("an event the flow does not use must plan nothing, got %+v", got)
	}
}

func TestPREventKeyCarriesTheOwner(t *testing.T) {
	a := prEvent(prTypeOpened, "api", 4)
	b := a
	b.Org = "other"
	if a.Key("pr-reviewer") == b.Key("pr-reviewer") {
		t.Error("acme/api#4 and other/api#4 must not share a key")
	}
	s := PREvent{Type: prTypeStaging, Org: "loop", Repo: "api", Commit: "abc123"}
	if got := s.Key(prJobE2ERun); got != "loop/api@abc123:e2e-run" {
		t.Errorf("the staging key is %q", got)
	}
}

func TestPlanPullRequestCardsPlansAndDeduplicates(t *testing.T) {
	ctx := context.Background()
	st := NewMemStore(1)
	ev := prEvent(prTypeOpened, "laundry-api", 42)

	planned, existing, res, err := st.PlanPullRequestCards(ctx, "p-1", ev)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(planned) != 2 || len(existing) != 0 {
		t.Fatalf("planned %v, existing %v; want two planned", planned, existing)
	}
	var sawPlanned, sawOpened bool
	for _, e := range res.NewEvents {
		sawPlanned = sawPlanned || e.Type == TaskPlanned
		sawOpened = sawOpened || e.Type == PullRequestOpened
	}
	if !sawPlanned || !sawOpened {
		t.Errorf("the result must carry TaskPlanned and PullRequestOpened, got %d events", len(res.NewEvents))
	}

	// A replay plans nothing, and finds both cards.
	planned, existing, _, err = st.PlanPullRequestCards(ctx, "p-1", ev)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if len(planned) != 0 || len(existing) != 2 {
		t.Fatalf("a replay planned %v and found %v; want nothing planned", planned, existing)
	}

	// The cards live on the PR workflow and leave the desk's own alone.
	cards := prCards(t, st, "p-1")
	if len(cards) != 2 {
		t.Fatalf("the board holds %d PR cards, want 2", len(cards))
	}
	for _, c := range cards {
		if c.Status != StatusPending {
			t.Errorf("card %s is %s, want PENDING", c.ID, c.Status)
		}
	}
	for _, tk := range st.tasks {
		if tk.DedupKey != "" && tk.WorkflowID != prWorkflowID("p-1") {
			t.Errorf("card %s is on %s, want the PR workflow", tk.ID, tk.WorkflowID)
		}
	}
}

func TestAMergeWithoutAnOpenStillPlansTheUnitTestCard(t *testing.T) {
	ctx := context.Background()
	st := NewMemStore(1)
	planned, _, _, err := st.PlanPullRequestCards(ctx, "p-1", prEvent(prTypeMerged, "laundry-api", 7))
	if err != nil || len(planned) != 1 {
		t.Fatalf("a merge alone planned %v, err %v; want one card", planned, err)
	}
	cards := prCards(t, st, "p-1")
	for _, c := range cards {
		if c.Role != "unit-test" || c.Labels[0] != LabelUnitTest {
			t.Errorf("card %+v is not the unit test card", c)
		}
	}
}

func TestAMergeCancelsAReviewThatHasNotStarted(t *testing.T) {
	ctx := context.Background()
	st := NewMemStore(1)
	if _, _, _, err := st.PlanPullRequestCards(ctx, "p-1", prEvent(prTypeOpened, "laundry-api", 9)); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := st.PlanPullRequestCards(ctx, "p-1", prEvent(prTypeMerged, "laundry-api", 9)); err != nil {
		t.Fatal(err)
	}
	canceled := 0
	for _, c := range prCards(t, st, "p-1") {
		if c.Status == StatusCanceled {
			canceled++
		}
	}
	if canceled != 2 {
		t.Errorf("%d cards were canceled, want the review and the e2e card", canceled)
	}
}

func TestPlanPullRequestCardsRefusesARepositoryTheProductDoesNotOwn(t *testing.T) {
	st := NewMemStore(1)
	ev := prEvent(prTypeOpened, "laundry-api", 42)
	ev.Org = "other-org"
	if _, _, _, err := st.PlanPullRequestCards(context.Background(), "p-1", ev); err == nil {
		t.Fatal("a repository the product does not own must be refused")
	}
}

func TestADraftOpensNothing(t *testing.T) {
	st := NewMemStore(1)
	ev := prEvent(prTypeOpened, "laundry-api", 42)
	ev.Draft = true
	_, _, _, err := st.PlanPullRequestCards(context.Background(), "p-1", ev)
	if !errors.Is(err, ErrPRSkipped) {
		t.Fatalf("a draft answered %v, want a skip", err)
	}
	if n := len(prCards(t, st, "p-1")); n != 0 {
		t.Errorf("a draft planned %d cards", n)
	}
}

func TestTheCapRefusesABurst(t *testing.T) {
	t.Setenv("GAOS_PR_MAX_OPEN", "2")
	ctx := context.Background()
	st := NewMemStore(1)
	if _, _, _, err := st.PlanPullRequestCards(ctx, "p-1", prEvent(prTypeOpened, "laundry-api", 1)); err != nil {
		t.Fatal(err)
	}
	_, _, _, err := st.PlanPullRequestCards(ctx, "p-1", prEvent(prTypeOpened, "laundry-api", 2))
	if !errors.Is(err, ErrPRSkipped) {
		t.Fatalf("the third and fourth cards answered %v, want the cap", err)
	}
}

func TestAnEvictedKeyCannotMakeADuplicate(t *testing.T) {
	// The store is the record: the key is looked up on the cards, so there is no
	// cache to evict from.
	ctx := context.Background()
	st := NewMemStore(1)
	ev := prEvent(prTypeOpened, "laundry-api", 5)
	st.PlanPullRequestCards(ctx, "p-1", ev)
	for i := 0; i < 3; i++ {
		planned, _, _, _ := st.PlanPullRequestCards(ctx, "p-1", ev)
		if len(planned) != 0 {
			t.Fatalf("replay %d planned %v", i, planned)
		}
	}
}

type fakePRSource struct {
	pr  ghPR
	err error
}

func (f fakePRSource) pullRequest(context.Context, string, string, int) (ghPR, error) {
	return f.pr, f.err
}
func (f fakePRSource) pullFiles(context.Context, string, string, int) ([]ghFile, error) {
	return []ghFile{{Filename: "a.go", Patch: "+x"}}, nil
}

func TestPRFlowSkipsAForkAndAStranger(t *testing.T) {
	ctx := context.Background()
	fork := ghPR{}
	fork.Head.Repo.FullName = "someone/laundry-api"
	fork.Base.Repo.FullName = "loop/laundry-api"
	stranger := ghPR{AuthorAssociation: "NONE"}
	stranger.Head.Repo.FullName = "loop/laundry-api"
	stranger.Base.Repo.FullName = "loop/laundry-api"
	for name, src := range map[string]fakePRSource{"fork": {pr: fork}, "stranger": {pr: stranger}, "lookup failure": {err: errors.New("x")}} {
		st := NewMemStore(1)
		flow := &prFlow{st: st, gh: src, logf: func(string, ...any) {}}
		flow.handle(ctx, prEvent(prTypeOpened, "laundry-api", 3))
		if n := len(prCards(t, st, "p-1")); n != 0 {
			t.Errorf("%s opened %d cards", name, n)
		}
	}
	// A member's pull request from the same repository opens both.
	ok := ghPR{AuthorAssociation: "MEMBER"}
	ok.Head.Repo.FullName = "loop/laundry-api"
	ok.Base.Repo.FullName = "loop/laundry-api"
	st := NewMemStore(1)
	(&prFlow{st: st, gh: fakePRSource{pr: ok}, logf: func(string, ...any) {}}).handle(ctx, prEvent(prTypeOpened, "laundry-api", 3))
	if n := len(prCards(t, st, "p-1")); n != 2 {
		t.Errorf("a member's pull request opened %d cards, want 2", n)
	}
}

func TestPRFlowSkipsAnEventNoProductOwns(t *testing.T) {
	st := NewMemStore(1)
	flow := &prFlow{st: st, logf: func(string, ...any) {}}
	ev := prEvent(prTypeOpened, "laundry-api", 42)
	ev.Org = "other-org"
	if flow.handle(context.Background(), ev) != flowDone {
		t.Error("an event no product owns must be acknowledged (flowDone), so the cursor passes it")
	}
	if n := len(prCards(t, st, "p-1")); n != 0 {
		t.Errorf("an event no product owns planned %d cards", n)
	}
}

func TestTheSchedulerNeverDispatchesAManualOrQAToolCard(t *testing.T) {
	for _, l := range []string{LabelPRReviewManual, LabelPRReviewNeedsManual, LabelE2EAutomation, LabelE2ERun} {
		if labelsAllowDispatch([]string{l}) {
			t.Errorf("a card labelled %s must not be dispatched", l)
		}
	}
	for _, l := range []string{LabelPRReviewAuto, LabelUnitTest} {
		if !labelsAllowDispatch([]string{l}) {
			t.Errorf("a card labelled %s must be dispatchable", l)
		}
	}
}

func TestAnOperatorChangesALabelAndItIsAnEvent(t *testing.T) {
	ctx := context.Background()
	st := NewMemStore(1)
	planned, _, _, _ := st.PlanPullRequestCards(ctx, "p-1", prEvent(prTypeOpened, "laundry-api", 11))
	review := planned[0]
	res, err := st.SetTaskLabel(ctx, "p-1", review, LabelPRReviewManual, "Ardi.Hikaru@example.com")
	if err != nil {
		t.Fatalf("set label: %v", err)
	}
	if len(res.NewEvents) != 1 || res.NewEvents[0].Type != TaskLabeled {
		t.Fatalf("the change must be one TaskLabeled, got %+v", res.NewEvents)
	}
	if strings.Contains(res.NewEvents[0].Text, "@") || strings.Contains(res.NewEvents[0].Text, "example.com") {
		t.Errorf("the event names an address: %q", res.NewEvents[0].Text)
	}
	got := prCards(t, st, "p-1")[review].Labels
	if len(got) != 1 || got[0] != LabelPRReviewManual {
		t.Errorf("the labels are %v, want the manual label alone (one per family)", got)
	}
	if _, err := st.SetTaskLabel(ctx, "p-1", review, "not-a-label", "a@b.c"); err == nil {
		t.Error("a label outside the vocabulary must be refused")
	}
	if res, err := st.SetTaskLabel(ctx, "p-1", review, LabelPRReviewManual, "a@b.c"); err != nil || len(res.NewEvents) != 0 {
		t.Errorf("setting the same label again must be a no-op, got %v %v", res.NewEvents, err)
	}
	if _, err := st.SetTaskLabel(ctx, "p-1", "nope", LabelPRReviewAuto, "a@b.c"); !errors.Is(err, ErrNotFound) {
		t.Errorf("an unknown card answered %v, want not found", err)
	}
}

func TestTwoCommitsSharingAPrefixGetTwoCards(t *testing.T) {
	ctx := context.Background()
	st := NewMemStore(1)
	a := PREvent{Type: prTypeStaging, Org: "loop", Repo: "laundry-api", Commit: "0123456789abAAAA"}
	b := PREvent{Type: prTypeStaging, Org: "loop", Repo: "laundry-api", Commit: "0123456789abBBBB"} // same first 12 characters
	pa, _, _, err := st.PlanPullRequestCards(ctx, "p-1", a)
	if err != nil || len(pa) != 1 {
		t.Fatalf("first: %v %v", pa, err)
	}
	pb, existing, _, err := st.PlanPullRequestCards(ctx, "p-1", b)
	if err != nil || len(pb) != 1 || len(existing) != 0 {
		t.Fatalf("second commit planned %v, found %v, err %v; want its own card", pb, existing, err)
	}
	if pa[0] == pb[0] {
		t.Errorf("both commits got the id %q", pa[0])
	}
	// A replay of either is still a no-op.
	if p, ex, _, _ := st.PlanPullRequestCards(ctx, "p-1", b); len(p) != 0 || len(ex) != 1 {
		t.Errorf("a replay planned %v, found %v", p, ex)
	}
}
