package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// The twelve-signal, hundred-point rubric of BRD §8.2, and BR-07's refusal
// below 45. M2.1, M2.2 and M2.5.
//
// These tests target what fails quietly. A rubric whose weights no longer sum
// to 100 still produces a number and still sorts records; a planner that plans
// an empty record still returns a plan, and the plan is confidently wrong. Both
// look healthy from every direction except the one that counts.

// completeRecord carries every signal §8.2 asks for that the in-memory record is
// able to express. Built here rather than borrowed from fixtures.go so a change
// to the demo data cannot quietly move what "complete" means.
func completeRecord() Master {
	return Master{
		ID:    "p-complete",
		Repos: []string{"loop/thing"},
		BRD: BRD{
			Problem:    "Invoices drift by a minor unit when discounted lines combine, and finance reconciles by hand.",
			Goals:      []string{"One rounding rule everywhere"},
			Rules:      []string{"Round at the line", "Half-up", "Versioned changes"},
			OutOfScope: []string{"Tax engine"},
			Metrics:    []string{"Zero reconciliation entries"},
		},
		Stories: []Story{
			{ID: "US-01", Pri: "must", Role: "finance officer", Want: "totals that match", So: "I stop adjusting the ledger",
				AC: []string{"Lines sum to the total", "A replay matches to the minor unit"}},
		},
		Diagrams: []Diagram{
			{ID: "DG-01", Kind: "sequence", Title: "Pricing", Src: "a -> b"},
			{ID: "DG-02", Kind: "erd", Title: "Model", Src: "A 1---* B"},
		},
		Decisions: []Decision{{ID: "ADR-01", Title: "Integer minor units", Confidence: 0.9}},
	}
}

// TestTheRubricWeightsSumToOneHundred pins §8.2's arithmetic. The weights are
// the document's, and a rubric that adds up to 97 or 104 still returns a plausible
// number for every record -- the bands just quietly stop meaning what they say.
func TestTheRubricWeightsSumToOneHundred(t *testing.T) {
	items := completeRecord().Readiness()
	if len(items) != 12 {
		t.Fatalf("the rubric has %d signals, want 12", len(items))
	}
	total := 0
	for _, it := range items {
		total += it.Points
	}
	if total != 100 {
		t.Errorf("the twelve weights sum to %d, want 100", total)
	}
	// Every signal must say what to do about it, or the missing list has a hole
	// exactly where someone needed it.
	for _, it := range items {
		if strings.TrimSpace(it.Missing) == "" {
			t.Errorf("signal %q carries no missing-list line", it.Label)
		}
	}
}

// TestACompleteRecordScoresNinetyFive is the honest ceiling. Eleven of the twelve
// signals can be earned today; "Verification strategy" has nowhere to live in the
// record until M1 widens it, and the rubric says so rather than dropping the
// signal it cannot measure.
func TestACompleteRecordScoresNinetyFive(t *testing.T) {
	m := completeRecord()
	if got := m.ReadinessScore(); got != 95 {
		t.Errorf("a complete record scores %d, want 95", got)
	}
	if got := ReadinessBand(m.ReadinessScore()); got != "complete" {
		t.Errorf("95 is band %q, want complete", got)
	}
	missing := m.ReadinessMissing()
	if len(missing) != 1 {
		t.Fatalf("a complete record is missing %d signals, want 1: %v", len(missing), missing)
	}
	if !strings.Contains(missing[0], "correctness is proven") {
		t.Errorf("the one deferred signal should be verification strategy, got %q", missing[0])
	}
}

// TestAnEmptyRecordScoresZeroAndNamesEverySignal is the other end. Nothing is
// earned, and the author gets twelve lines telling them what to write -- B-3's
// "a refusal that does not say what is missing is a dead end".
func TestAnEmptyRecordScoresZeroAndNamesEverySignal(t *testing.T) {
	var m Master
	if got := m.ReadinessScore(); got != 0 {
		t.Errorf("an empty record scores %d, want 0", got)
	}
	if got := ReadinessBand(0); got != "insufficient" {
		t.Errorf("0 is band %q, want insufficient", got)
	}
	if got := len(m.ReadinessMissing()); got != 12 {
		t.Errorf("an empty record is missing %d signals, want all 12", got)
	}
}

// TestCriteriaCoverageIsAllOrNothingAcrossMustStories guards the signal most
// likely to be softened into partial credit. §8.2: "Every must story has at least
// 2 acceptance criteria". One thin must story forfeits all twelve points -- and a
// record with no stories at all must not collect them by vacuous truth, which is
// the quiet way an empty brief scores well.
func TestCriteriaCoverageIsAllOrNothingAcrossMustStories(t *testing.T) {
	coverage := func(m Master) ReadinessItem {
		for _, it := range m.Readiness() {
			if it.Label == "Criteria coverage" {
				return it
			}
		}
		t.Fatal("the rubric has no Criteria coverage signal")
		return ReadinessItem{}
	}

	full := completeRecord()
	if !coverage(full).OK {
		t.Error("every must story has two criteria; coverage should be earned")
	}

	thin := completeRecord()
	thin.Stories = append(thin.Stories, Story{
		ID: "US-02", Pri: "must", Role: "engineer", Want: "one helper", So: "they cannot disagree",
		AC: []string{"only one is exported"},
	})
	if coverage(thin).OK {
		t.Error("one must story with a single criterion must forfeit the whole signal")
	}
	if got := thin.ReadinessScore(); got != 83 {
		t.Errorf("losing coverage costs the full 12 points: scored %d, want 83", got)
	}

	none := completeRecord()
	none.Stories = nil
	if coverage(none).OK {
		t.Error("a record with no stories must not earn coverage by vacuous truth")
	}
}

// TestThePlannerRefusesBelowTheFloorAndSaysWhatIsMissing is BR-07 itself. Below
// 45 there is no plan, only a list -- and the refusal has to carry that list, or
// the author is told "no" with nowhere to go.
//
// Runs against MemStore, which is also what ArcadeStore plans through: its
// PlanWorkflow delegates to the projection, so there is one implementation of
// this rule and the two stores cannot disagree about it.
func TestThePlannerRefusesBelowTheFloorAndSaysWhatIsMissing(t *testing.T) {
	_, st := newTestServer(t)
	ctx := context.Background()

	// p-5 is the draft record: one rule, no stories, no diagrams, no ADRs.
	thin, _ := st.Master(ctx, "p-5")
	if thin.ReadinessScore() >= PlanFloor {
		t.Fatalf("this test needs a record below the floor; p-5 scores %d", thin.ReadinessScore())
	}

	plan, err := st.PlanWorkflow(ctx, WorkflowDraft{ProductID: "p-5"})
	if err == nil {
		t.Fatalf("planned %d tasks for a record scoring %d; BR-07 refuses below %d",
			len(plan), thin.ReadinessScore(), PlanFloor)
	}
	if plan != nil {
		t.Error("a refused plan must be nil, not a half-built one")
	}

	// The handlers turn any ErrRejected into a toast rather than a 500, so the
	// refusal has to satisfy it.
	if !errors.Is(err, ErrRejected) {
		t.Errorf("a refusal is a rejection, not a failure: %v", err)
	}

	var nr *NotReadyError
	if !errors.As(err, &nr) {
		t.Fatalf("the refusal must carry the missing list, got %T", err)
	}
	if len(nr.Missing) == 0 {
		t.Error("a refusal that does not say what is missing is a dead end")
	}
	if nr.Score != thin.ReadinessScore() || nr.Band != ReadinessBand(nr.Score) {
		t.Errorf("the refusal reports %d/%s, the record scores %d/%s",
			nr.Score, nr.Band, thin.ReadinessScore(), ReadinessBand(thin.ReadinessScore()))
	}

	// And the same record, once filled in, plans. A floor that never lets anything
	// through is a wall.
	full, _ := st.Master(ctx, "p-1")
	if _, err := st.PlanWorkflow(ctx, WorkflowDraft{ProductID: "p-1"}); err != nil {
		t.Errorf("p-1 scores %d and must still plan: %v", full.ReadinessScore(), err)
	}
}
