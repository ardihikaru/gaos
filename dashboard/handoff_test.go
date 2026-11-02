package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The handoff document. BR-30, and since D-050 the only authority on what an
// invocation did.
//
// §7.6 calls it a contract rather than a convention: "an agent that writes
// prose where a parser expects a field has failed the attempt." These tests are
// that sentence, both halves of it.

func fixtureHandoff(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "handoff", name))
	if err != nil {
		t.Fatalf("reading fixture %s: %v", name, err)
	}
	return b
}

// TestAWellFormedHandoffParsesEveryMandatoryField is the positive half.
func TestAWellFormedHandoffParsesEveryMandatoryField(t *testing.T) {
	h, err := ParseHandoff(fixtureHandoff(t, "complete.md"))
	if err != nil {
		t.Fatalf("the declared shape from BRD §7.6 did not parse: %v", err)
	}

	for _, c := range []struct{ field, got, want string }{
		{"task", h.Task, "T-1042"},
		{"run", h.Run, "R-1042-2"},
		{"role", h.Role, "backend"},
		{"status", h.Status, HandoffComplete},
		{"handoff.next_role", h.NextRole, "qa"},
	} {
		if c.got != c.want {
			t.Errorf("field %s parsed as %q, want %q; the handoff is a contract, not a convention, and a parser that is lenient about the shape is a chain that degrades quietly", c.field, c.got, c.want)
		}
	}

	if len(h.Criteria) != 2 {
		t.Fatalf("parsed %d acceptance criteria, want 2; no criterion means no verdict, and the reviewer will invent one", len(h.Criteria))
	}
	if h.Criteria[0].ID != "AC-7" || !h.Criteria[0].Met {
		t.Errorf("criterion 0 is %+v, want AC-7 met", h.Criteria[0])
	}
	if h.Criteria[0].Evidence != "tests/pricing_test.go::TestRounding" {
		t.Errorf("criterion 0 evidence is %q; the evidence is what makes a met criterion checkable rather than claimed", h.Criteria[0].Evidence)
	}
	if len(h.Artifacts) != 2 || h.Artifacts[0].Path != "src/pricing.go" || h.Artifacts[0].Kind != "code" {
		t.Errorf("artifacts parsed as %+v, want src/pricing.go/code first", h.Artifacts)
	}
	if h.Usage == nil || h.Usage.TokensIn != 184320 || h.Usage.Basis != CostMeasured {
		t.Errorf("usage parsed as %+v, want 184320 in at basis measured", h.Usage)
	}
	if !h.Met() {
		t.Error("a document whose every criterion is met reports Met() false")
	}
}

// TestProseWithNoFrontMatterIsHandoffMissing is the failure §7.6 names.
func TestProseWithNoFrontMatterIsHandoffMissing(t *testing.T) {
	_, err := ParseHandoff(fixtureHandoff(t, "no-frontmatter.md"))
	if err == nil {
		t.Fatal("a document with no front matter parsed successfully; §7.6 says an agent that writes prose where a parser expects a field has failed the attempt")
	}
	if !strings.Contains(err.Error(), SigHandoffMissing) {
		t.Fatalf("the failure reads %q; BR-30 requires the signature %q so the retry loop can see it", err, SigHandoffMissing)
	}
}

// TestAHandoffMissingAMandatoryFieldIsHandoffMissing
func TestAHandoffMissingAMandatoryFieldIsHandoffMissing(t *testing.T) {
	_, err := ParseHandoff(fixtureHandoff(t, "missing-fields.md"))
	if err == nil {
		t.Fatal("a handoff with no run id and no criteria was accepted; all four ways this fails — absent, no front matter, unparseable, incomplete — are one failure to the retry loop and carry one signature")
	}
	for _, want := range []string{"run", "criteria"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the failure %q does not name the missing field %q; a refusal that does not say what is missing is a dead end", err, want)
		}
	}

	// A status outside the closed vocabulary is the same failure.
	_, err = ParseHandoff([]byte("---\ntask: T-1\nrun: R-1\nrole: qa\nstatus: probably-fine\ncriteria:\n  - id: AC-1 met: true\n---\n"))
	if err == nil {
		t.Fatal("status \"probably-fine\" was accepted; the contract declares complete|blocked|partial and a fourth value is a document GAOS cannot score")
	}
}

// TestAHandoffWithNoUsageBlockIsLegalAndReportsNothing
func TestAHandoffWithNoUsageBlockIsLegalAndReportsNothing(t *testing.T) {
	h, err := ParseHandoff(fixtureHandoff(t, "no-usage.md"))
	if err != nil {
		t.Fatalf("a document that omitted the usage block was rejected: %v; §7.6 says to omit it entirely when the invocation reported nothing, so its absence is legal", err)
	}
	if h.Usage != nil {
		t.Fatalf("the absent usage block parsed as %+v, want nil; nil is what makes the basis unavailable rather than a zero somebody invented", h.Usage)
	}
	if h.Status != HandoffPartial {
		t.Errorf("status parsed as %q, want partial", h.Status)
	}
	if h.Met() {
		t.Error("a document with an unmet criterion reports Met() true; the reviewer would score work that did not happen")
	}
}
