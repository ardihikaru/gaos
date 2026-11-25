package main

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
)

// The task's role and its labels are closed vocabularies, and each has a
// second copy: the Go set and the DDL's regexp. A name the DDL refuses is a
// row the store cannot write, and it fails at commit in production rather
// than in a test. So the two are held to each other here, in both
// directions.

// TestTheTaskRoleVocabularyMatchesTheDDL holds the roles map and the DDL's
// Task.role regexp to each other.
//
// The role vocabulary is closed for the same reason the status vocabulary is:
// a role names a seat, and the seat carries a model, a room and a policy. A
// role the DDL would refuse is a task nobody can dispatch.
func TestTheTaskRoleVocabularyMatchesTheDDL(t *testing.T) {
	sql := schemaSQL(t)["04-execution.sql"]
	if sql == "" {
		t.Fatal("04-execution.sql is empty or missing")
	}
	m := regexp.MustCompile(`Task\.role[^;]*regexp '([^']+)'`).FindStringSubmatch(sql)
	if m == nil {
		t.Fatal("Task.role declares no regexp; its vocabulary is unconstrained")
	}
	inDDL := map[string]bool{}
	for _, v := range strings.Split(m[1], "|") {
		inDDL[v] = true
	}
	inGo := map[string]bool{}
	for v := range roles {
		inGo[v] = true
	}
	// Agent.role has its own regexp (05-capacity.sql), and a database that already
	// exists keeps the regexp it first declared, so 17-defects.sql ALTERs both.
	// Found live: a fresh tenant refused the agent of a newer role at seed.
	want := m[1]
	if am := regexp.MustCompile(`Agent\.role[^;]*regexp '([^']+)'`).FindStringSubmatch(schemaSQL(t)["05-capacity.sql"]); am == nil || am[1] != want {
		t.Errorf("Agent.role regexp in 05-capacity.sql is %v; it must equal Task.role's %q", am, want)
	}
	alter := schemaSQL(t)["17-defects.sql"]
	for _, prop := range []string{"Task.role", "Agent.role"} {
		if !regexp.MustCompile(`ALTER PROPERTY ` + regexp.QuoteMeta(prop) + `\s+REGEXP '` + regexp.QuoteMeta(want) + `'`).MatchString(alter) {
			t.Errorf("17-defects.sql does not ALTER %s to %q: an existing database would refuse a newer role at commit", prop, want)
		}
	}

	for v := range inGo {
		if !inDDL[v] {
			t.Errorf("Task.role permits %q; Go declares %q - a role the DDL refuses is a row the store cannot write", m[1], v)
		}
	}
	for v := range inDDL {
		if !inGo[v] {
			t.Errorf("Task.role permits %q; Go declares no such role - the DDL would accept a seat that has no model and no room", v)
		}
	}
	// The role order lists every role exactly once.
	seen := map[string]bool{}
	for _, v := range roleOrder {
		if seen[v] {
			t.Errorf("roleOrder lists %q twice", v)
		}
		seen[v] = true
		if _, ok := roles[v]; !ok {
			t.Errorf("roleOrder lists %q, which the roles map does not declare", v)
		}
	}
	for v := range roles {
		if !seen[v] {
			t.Errorf("the roles map declares %q, which roleOrder does not list", v)
		}
	}
}

// TestThePullRequestAgentRolesAreDeclared checks the two roles the
// pull-request flow needs are in the map, each with a model and a room.
func TestThePullRequestAgentRolesAreDeclared(t *testing.T) {
	for _, key := range []string{"pr-reviewer", "unit-test"} {
		r, ok := roles[key]
		if !ok {
			t.Errorf("the role %q is not declared", key)
			continue
		}
		if r.Model == "" {
			t.Errorf("the role %q has no model", key)
		}
		if r.Room == "" {
			t.Errorf("the role %q has no room", key)
		}
		if r.Label == "" || r.Short == "" {
			t.Errorf("the role %q has no label", key)
		}
	}
}

// TestTheLabelVocabularyIsClosed checks the label vocabulary is closed, and
// that its order and its set agree.
func TestTheLabelVocabularyIsClosed(t *testing.T) {
	if len(labelSet) == 0 {
		t.Fatal("the label vocabulary is empty")
	}
	if len(labelSet) != len(labelOrder) {
		t.Errorf("the vocabulary has %d labels and the order lists %d", len(labelSet), len(labelOrder))
	}
	seen := map[string]bool{}
	for _, l := range labelOrder {
		if seen[l] {
			t.Errorf("labelOrder lists %q twice", l)
		}
		seen[l] = true
		if !labelSet[l] {
			t.Errorf("labelOrder lists %q, which the vocabulary does not declare", l)
		}
	}
	for l := range labelSet {
		if !seen[l] {
			t.Errorf("the vocabulary declares %q, which labelOrder does not list", l)
		}
	}
	// A label outside the vocabulary is refused.
	if _, ok := validateLabels([]string{LabelPRReviewAuto}); !ok {
		t.Error("a known label must be accepted")
	}
	if l, ok := validateLabels([]string{"pr-review:auto", "not-a-label"}); ok {
		t.Error("an unknown label must be refused")
	} else if l != "not-a-label" {
		t.Errorf("the refusal must name the offender, got %q", l)
	}
	// The default for a pull-request review is the agent's.
	if !labelSet[LabelPRReviewAuto] {
		t.Error("the default review label must be in the vocabulary")
	}
}

// TestNormalizeLabelsKeepsTheVocabularyOrder checks the normalization: it
// trims, drops the empty, drops what is not in the vocabulary, and orders by
// the vocabulary so two cards with the same labels read the same way.
func TestNormalizeLabelsKeepsTheVocabularyOrder(t *testing.T) {
	got := normalizeLabels([]string{" " + LabelE2ERun + " ", LabelPRReviewAuto, "", "not-a-label", LabelPRReviewAuto})
	want := []string{LabelPRReviewAuto, LabelE2ERun}
	if len(got) != len(want) {
		t.Fatalf("normalizeLabels = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("normalizeLabels = %v, want %v", got, want)
		}
	}
}

// TestDefectLabelFamilyIsPlannerOnly holds D-091's two refusals: the scheduler
// never dispatches a defect:* card, and an operator can never set one.
func TestDefectLabelFamilyIsPlannerOnly(t *testing.T) {
	ctx := context.Background()
	family := []string{LabelDefectTriage, LabelDefectEasy, LabelDefectMedium, LabelDefectComplex}
	for _, l := range family {
		if !ValidLabel(l) {
			t.Errorf("%s is not in the closed vocabulary", l)
		}
		if labelsAllowDispatch([]string{l}) {
			t.Errorf("the scheduler may dispatch a card labelled %s", l)
		}
	}
	s, pid := defectFixture(t)
	report(t, s, pid, defItem(DefectKindXfail, "DEF-101", defCase1))
	card := cardByKey(s, defectCardKey(pid+"/DEF-101", 1, "triage"))
	for _, l := range family {
		if _, err := s.SetTaskLabel(ctx, pid, card.ID, l, "ops"); !errors.Is(err, ErrRejected) {
			t.Errorf("SetTaskLabel(%s) = %v, want ErrRejected (422)", l, err)
		}
	}
	// A card of the PR flow still takes its own family.
	planned, _, _, err := s.PlanPullRequestCards(ctx, pid, PREvent{Kind: "github", Type: prTypeOpened, EventID: "gh-1", Seq: 1,
		Org: "acme-org", Repo: "sample-service", PRNumber: 5, Title: "x"})
	if err != nil || len(planned) == 0 {
		t.Fatalf("plan: %v %v", planned, err)
	}
	if _, err := s.SetTaskLabel(ctx, pid, planned[0], LabelPRReviewManual, "ops"); err != nil {
		t.Errorf("a pr-review label was refused: %v", err)
	}
	// And over HTTP the refusal is a 422.
	srv, _ := newTestServer(t)
	srv.store = s
	rec := formPost(t, srv, "/products/"+pid+"/board/cards/"+card.ID+"/label", "label="+LabelDefectEasy, true)
	if rec.Code != 422 {
		t.Errorf("the label route answered %d for defect:easy, want 422", rec.Code)
	}
}
