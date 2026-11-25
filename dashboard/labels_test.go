package main

import (
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
