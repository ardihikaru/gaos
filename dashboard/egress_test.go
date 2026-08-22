package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The pre-dispatch egress pass. T-126, requirement L-4, rule BR-32.
//
// R-11 is the risk these tests exist against: "Egress of source code to a model
// provider is automatic, continuous, and widens on every retry ... the one way
// GAOS can harm a user badly without any component failing: correct behaviour,
// unreviewed content, irreversible send." A control against that is only worth
// having if it is tested at the boundary, and the boundary is a pure function.

func repoPart(path, body string) PacketPart {
	return PacketPart{Path: path, Kind: KindRepo, Body: body}
}

func packetOf(parts ...PacketPart) Packet {
	return Packet{TaskID: "t-1", DeskID: "d-1", Workspace: "/w", Parts: parts}
}

// openPolicy screens content but restricts no paths, so a test about detectors
// is not accidentally a test about the allowlist.
func openPolicy() EgressPolicy { return EgressPolicy{} }

// TestTheEgressPassIsPure is fold.go's first rule applied to the thing that
// feeds it. The verdict becomes an event; an event whose content varies between
// two identical calls makes a replay disagree with the run it replays.
func TestTheEgressPassIsPure(t *testing.T) {
	p := packetOf(
		repoPart("src/a.go", "package a\n"),
		repoPart("docs/b.md", "notes\n"),
		repoPart("src/c.go", "const k = \"AKIAIOSFODNN7EXAMPLE\"\n"),
	)
	pol := EgressPolicy{AllowPaths: []string{"src/**"}}

	_, first, _ := Screen(pol, Target{Provider: "anthropic"}, p)
	_, second, _ := Screen(pol, Target{Provider: "anthropic"}, p)

	if !reflect.DeepEqual(first, second) {
		t.Fatalf("Screen returned a different verdict on the second call with identical input:\n first: %+v\nsecond: %+v\nthe reducer folds what this produces, and a pass that is not a function of its arguments makes a replay disagree with the run", first, second)
	}
}

// TestACleanSliceClearsAndCarriesADigest is the positive half. A control that
// never lets anything through is not a control.
func TestACleanSliceClearsAndCarriesADigest(t *testing.T) {
	p := packetOf(
		PacketPart{Path: ".gaos/packet/task.md", Kind: KindTask, Body: "# t-1\nDo the thing.\n"},
		repoPart("src/pricing.go", "package pricing\n\nfunc Round(x float64) float64 { return x }\n"),
	)

	c, v, err := Screen(EgressPolicy{AllowPaths: []string{"src/**"}}, Target{Provider: "anthropic"}, p)
	if err != nil {
		t.Fatalf("a slice with nothing to find was refused: rule=%q reason=%q — a pass that never lets anything through is not a control, it is an outage", v.Rule, v.Reason)
	}
	if len(v.Redactions) != 0 {
		t.Fatalf("a clean slice reported %d redaction(s): %+v", len(v.Redactions), v.Redactions)
	}
	if c.Digest() == "" {
		t.Fatalf("the cleared packet carries digest %q; 04-execution.sql declares ContextSlice.digest so two runs can be proved to have seen the same slice, and an empty one proves nothing", c.Digest())
	}
	if c.Bytes() != v.Bytes || c.Bytes() == 0 {
		t.Fatalf("cleared bytes %d and verdict bytes %d disagree, or are zero; the budget the size limit is enforced against has to be the thing that actually goes out", c.Bytes(), v.Bytes)
	}
}

// TestACredentialInTheSliceRefusesTheDispatch is L-4's whole sentence: it runs
// before the model sees anything, and a positive detection refuses.
func TestACredentialInTheSliceRefusesTheDispatch(t *testing.T) {
	cases := []struct{ name, body string }{
		{"anthropic-key", "const k = \"sk-ant-api03-AAAAAAAAAAAAAAAAAAAAAA\"\n"},
		{"aws-access-key-id", "AWS_ID = AKIAIOSFODNN7EXAMPLE\n"},
		{"private-key-block", "-----BEGIN RSA PRIVATE KEY-----\nMIIE...\n"},
		{"github-token", "token: ghp_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\n"},
		{"env-assignment", "DATABASE_PASSWORD=hunter2hunter2\n"},
		{"jwt", "auth = eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dBjftJeZ4CVPmB92K27uhbUJU1p1r_wW1gFWFOEjXk\n"},
		{"basic-auth-url", "repo = https://ci:s3cr3tpassword@git.example.com/x.git\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := packetOf(repoPart("src/leak.go", c.body))
			_, v, err := Screen(openPolicy(), Target{Provider: "anthropic"}, p)
			if err == nil {
				t.Fatalf("a %s was in the slice and Screen allowed the dispatch; L-4 says a positive detection refuses BEFORE the model sees anything, and a secret scored afterwards has already left the building", c.name)
			}
			if v.Rule != RuleSecret {
				t.Fatalf("a %s was refused under rule %q, want %q; the budget guard reuses DispatchRefused with its own rule and the two must stay distinguishable", c.name, v.Rule, RuleSecret)
			}
		})
	}
}

// TestARefusalNeverCarriesTheSecretItFound guards the single most dangerous
// thing this file could do.
func TestARefusalNeverCarriesTheSecretItFound(t *testing.T) {
	const secret = "sk-ant-api03-NEVERWRITETHISDOWN00"
	p := packetOf(repoPart("src/leak.go", "const k = \""+secret+"\"\n"))

	_, v, err := Screen(openPolicy(), Target{Provider: "anthropic"}, p)
	if err == nil {
		t.Fatal("the fixture credential was not detected at all; the rest of this test proves nothing")
	}

	for _, f := range v.Findings {
		if strings.Contains(f.Path+f.Detector+f.Class, secret) {
			t.Fatalf("the verdict for detector %q contains the matched text; a DispatchRefused event is append-only (BR-04) and rendered to a browser, so this writes the credential permanently into the record that exists to prove it never left", f.Detector)
		}
	}
	if strings.Contains(v.Reason, secret) {
		t.Fatalf("the refusal reason contains the matched text; the reason becomes the event payload and the pill on the timeline")
	}
}

// TestAPathOutsideTheAllowlistIsRedactedNotRefused is the distinction the
// allowlist and the detectors are on opposite sides of.
func TestAPathOutsideTheAllowlistIsRedactedNotRefused(t *testing.T) {
	p := packetOf(
		repoPart("src/keep.go", "package keep\n"),
		repoPart("vendor/drop.go", "package drop\n"),
	)

	c, v, err := Screen(EgressPolicy{AllowPaths: []string{"src/**"}}, Target{Provider: "anthropic"}, p)
	if err != nil {
		t.Fatalf("%q is outside the allowlist and the whole dispatch was refused; the allowlist bounds what leaves, it does not abort — the part is dropped and ContextRedacted records it", "vendor/drop.go")
	}
	if len(v.Redactions) != 1 || v.Redactions[0].Path != "vendor/drop.go" {
		t.Fatalf("redactions are %+v, want exactly vendor/drop.go", v.Redactions)
	}
	for _, part := range c.Packet().Parts {
		if part.Path == "vendor/drop.go" {
			t.Fatal("the redacted part is still in the cleared packet; a redaction that does not remove anything is a log line pretending to be a control")
		}
	}
}

// TestASecretInsideARedactedFileIsNotARefusal is the ordering the pass depends
// on: step 2 drops the file, so step 3 never sees what was in it.
func TestASecretInsideARedactedFileIsNotARefusal(t *testing.T) {
	p := packetOf(
		repoPart("src/keep.go", "package keep\n"),
		repoPart(".env", "ANTHROPIC_API_KEY=sk-ant-api03-AAAAAAAAAAAAAAAAAAAAAA\n"),
	)

	_, v, err := Screen(EgressPolicy{}, Target{Provider: "anthropic"}, p)
	if err != nil {
		t.Fatalf("a key inside a dropped .env refused the dispatch: rule=%q; the file never left, so it is a redaction — refusing here would make every repository with a .env undispatchable", v.Rule)
	}
	if len(v.Redactions) != 1 || v.Redactions[0].Rule != "env-file" {
		t.Fatalf("redactions are %+v, want one env-file redaction for .env", v.Redactions)
	}
}

// TestTheAllowlistIsNotFooledByADotDotPath
func TestTheAllowlistIsNotFooledByADotDotPath(t *testing.T) {
	p := packetOf(repoPart("src/../../etc/shadow", "root:x:0:0\n"))

	_, v, err := Screen(EgressPolicy{AllowPaths: []string{"src/**"}}, Target{Provider: "anthropic"}, p)
	if err == nil {
		t.Fatalf("%q cleaned to a path outside the workspace and the pass allowed it; a path that escapes the workspace is a refusal, or an allowlist is a suggestion", "src/../../etc/shadow")
	}
	if v.Rule != RulePathEscape {
		t.Fatalf("an escaping path was refused under rule %q, want %q", v.Rule, RulePathEscape)
	}
}

// TestGlobMatchingCrossesSeparatorsForDoubleStar is the bug path.Match hands
// you for free.
func TestGlobMatchingCrossesSeparatorsForDoubleStar(t *testing.T) {
	cases := []struct {
		pattern, name string
		want          bool
	}{
		{"src/**", "src/a.go", true},
		{"src/**", "src/deep/nested/a.go", true},
		{"src/**", "vendor/a.go", false},
		{"src/**", "src", true},
		{"tests/**", "tests/unit/x_test.go", true},
		{"**/*.go", "a/b/c.go", true},
		{"*.md", "README.md", true},
		{"*.md", "docs/README.md", false},
	}
	for _, c := range cases {
		if got := matchGlob(c.pattern, c.name); got != c.want {
			t.Errorf("matchGlob(%q, %q) = %v, want %v; path.Match's * never crosses a separator, so \"src/**\" that matched only \"src/a\" would be a policy that looks configured and protects nothing", c.pattern, c.name, got, c.want)
		}
	}
}

// TestAnOversizedSliceIsRefusedRatherThanTruncated
func TestAnOversizedSliceIsRefusedRatherThanTruncated(t *testing.T) {
	body := strings.Repeat("x", 4096)
	p := packetOf(repoPart("src/big.go", body))

	_, v, err := Screen(EgressPolicy{MaxBytes: 1024}, Target{Provider: "anthropic"}, p)
	if err == nil {
		t.Fatalf("a %d-byte slice against a %d-byte limit was allowed with %d bytes; truncating makes the frozen packet a lie about what the model received (D-051), so the limit refuses", len(body), 1024, v.Bytes)
	}
	if v.Rule != RuleSize {
		t.Fatalf("an oversized slice was refused under rule %q, want %q", v.Rule, RuleSize)
	}
}

// TestAnOversizedFileIsRedactedAgainstThePerPartLimit is the other half of the
// size rule: one huge file is a different risk from many small ones.
func TestAnOversizedFileIsRedactedAgainstThePerPartLimit(t *testing.T) {
	p := packetOf(
		repoPart("src/small.go", "package small\n"),
		repoPart("src/huge.go", strings.Repeat("y", 4096)),
	)

	_, v, err := Screen(EgressPolicy{MaxPartBytes: 1024}, Target{Provider: "anthropic"}, p)
	if err != nil {
		t.Fatalf("one oversized file refused the whole dispatch: rule=%q; the per-file limit drops the file, the whole-slice limit refuses, and collapsing the two loses the distinction", v.Rule)
	}
	if len(v.Redactions) != 1 || v.Redactions[0].Path != "src/huge.go" {
		t.Fatalf("redactions are %+v, want exactly src/huge.go", v.Redactions)
	}
}

// TestLocalOnlyRefusesANonLocalTarget is L-6.
func TestLocalOnlyRefusesANonLocalTarget(t *testing.T) {
	p := packetOf(repoPart("src/a.go", "package a\n"))
	pol := EgressPolicy{LocalOnly: true}

	if _, v, err := Screen(pol, Target{Provider: "anthropic", Local: false}, p); err == nil {
		t.Fatalf("local_only is set and target %q is not local, and Screen allowed it; L-6 fails the task rather than downgrading quietly", "anthropic")
	} else if v.Rule != RuleLocalOnly {
		t.Fatalf("a no-egress violation was refused under rule %q, want %q", v.Rule, RuleLocalOnly)
	}

	if _, _, err := Screen(pol, Target{Provider: "ollama", Local: true}, p); err != nil {
		t.Fatalf("local_only refused a locally-hosted model: %v; no-egress mode permits local models, it does not forbid all work", err)
	}
}

// TestARefusalReportsTheMostDangerousReasonWhenSeveralApply pins the precedence
// order, which is a determinism property and a usability one at once.
func TestARefusalReportsTheMostDangerousReasonWhenSeveralApply(t *testing.T) {
	p := packetOf(repoPart("src/leak.go",
		"const k = \"sk-ant-api03-AAAAAAAAAAAAAAAAAAAAAA\"\n"+strings.Repeat("z", 4096)))

	_, v, err := Screen(EgressPolicy{MaxBytes: 1024, LocalOnly: true}, Target{Provider: "anthropic"}, p)
	if err == nil {
		t.Fatal("a slice that was oversized, non-local and carrying a credential was allowed")
	}
	if v.Rule != RuleSecret {
		t.Fatalf("a slice that was both oversized and carried a credential was refused with rule %q; the operator must be told about the key, not the byte count", v.Rule)
	}
}

// TestAnEmptyPolicyStillRunsEveryDetector pins the two defaults that differ
// from each other, because a reader will assume they match.
func TestAnEmptyPolicyStillRunsEveryDetector(t *testing.T) {
	leak := packetOf(repoPart("anywhere/leak.go", "const k = \"sk-ant-api03-AAAAAAAAAAAAAAAAAAAAAA\"\n"))
	if _, _, err := Screen(EgressPolicy{}, Target{Provider: "anthropic"}, leak); err == nil {
		t.Fatalf("EgressPolicy{} let a %s through; an empty Redact list is a scanner nobody armed, and the default for a forgotten security control is on", "anthropic-key")
	}

	plain := packetOf(repoPart("anywhere/plain.go", "package plain\n"))
	_, v, err := Screen(EgressPolicy{}, Target{Provider: "anthropic"}, plain)
	if err != nil {
		t.Fatalf("EgressPolicy{} refused a clean slice: %v", err)
	}
	if len(v.Redactions) != 0 {
		t.Fatalf("EgressPolicy{} dropped %q; an empty AllowPaths is an allowlist nobody wrote, which cannot be a list of files somebody meant to exclude", v.Redactions[0].Path)
	}
}

// TestEveryPartRedactedIsARefusalNotAnEmptyDispatch
func TestEveryPartRedactedIsARefusalNotAnEmptyDispatch(t *testing.T) {
	p := packetOf(repoPart("vendor/a.go", "package a\n"), repoPart("vendor/b.go", "package b\n"))

	_, v, err := Screen(EgressPolicy{AllowPaths: []string{"src/**"}}, Target{Provider: "anthropic"}, p)
	if err == nil {
		t.Fatal("every part was redacted and the dispatch went ahead with an empty slice; spending an attempt on a packet with nothing in it burns a retry against BR-17's guard and teaches the reviewer nothing")
	}
	if v.Rule != RuleEmptySlice {
		t.Fatalf("a fully-redacted packet was refused under rule %q, want %q", v.Rule, RuleEmptySlice)
	}
}

// TestNothingOutsideTheEgressPassConstructsACleared turns the honest limit of
// the Cleared type into a failing test.
//
// The type makes BR-32 unforgeable BY FORGETTING: Dispatch takes a Cleared, and
// Screen is the only thing that returns one. It cannot stop a deliberate
// `Cleared{...}` literal, because this is package main and everything here can
// write one. So the deliberate version is caught here instead, by walking the
// package's own syntax tree -- the same standard readOnly meets, made checkable.
func TestNothingOutsideTheEgressPassConstructsACleared(t *testing.T) {
	fset := token.NewFileSet()
	sources, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("listing the package: %v", err)
	}

	for _, name := range sources {
		if name == "egress.go" || name == "egress_test.go" {
			continue // Screen lives here, and this test builds one on purpose
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			id, ok := lit.Type.(*ast.Ident)
			if !ok || id.Name != "Cleared" {
				return true
			}
			// Cleared{} with no fields is the zero value, which Dispatch
			// refuses at its first line. A populated literal is a forged
			// clearance.
			if len(lit.Elts) > 0 {
				pos := fset.Position(lit.Pos())
				t.Errorf("%s:%d constructs a populated Cleared outside egress.go; BR-32 says nothing reaches a provider without the egress pass, and the only reason that is true is that Screen is the only thing that makes the argument Dispatch takes", name, pos.Line)
			}
			return true
		})
	}
}
