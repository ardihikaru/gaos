package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Nested fences and the usage.basis vocabulary (v0.40.3).
//
// Both were found by replaying one real architect reply
// (testdata/design-reply-nested-fences.txt): its gaos-design block carried
// ```ts code samples, and the first bare ``` that closed a sample also closed
// the design, so the reviewer would have been handed a third of it. The same
// reply's handoff wrote `basis: unmeasured`, which the parser refuses and the
// contract never said it would.
//
// Every test here is a top-level func Test…, and each has a row in the QA
// register (pustaka/docs/product/qa-test-plan.html).

// TestADesignWithCodeSamplesReachesItsReviewerWhole replays the real reply.
func TestADesignWithCodeSamplesReachesItsReviewerWhole(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("testdata", "design-reply-nested-fences.txt"))
	if err != nil {
		t.Fatal(err)
	}
	reply := string(b)
	d, err := ExtractDesign(reply, "wf-8-t-3", "wf-8-t-3-a1")
	if err != nil {
		t.Fatalf("the real design reply does not parse: %v", err)
	}
	if d.Task != "wf-8-t-3" || d.Run != "wf-8-t-3-a1" || d.Role != "architect" {
		t.Errorf("the design parsed to task %q run %q role %q", d.Task, d.Run, d.Role)
	}
	for _, want := range []string{"```ts", "Running against production", "merged cases."} {
		if !strings.Contains(d.Document, want) {
			t.Errorf("the design document (%d bytes) lacks %q; a code sample's closing fence "+
				"closed the design, and the reviewer would be handed a truncated document", len(d.Document), want)
		}
	}
	if strings.Contains(d.Document, HandoffFence) {
		t.Error("the design document ran on into the handoff block after it")
	}

	// The handoff in the same reply is still read, and still refused for its
	// basis: the fix is in the contract, never in the parser.
	_, _, err = ExtractHandoff(reply, "wf-8-t-3", "wf-8-t-3-a1")
	if err == nil || !strings.Contains(err.Error(), `usage.basis "unmeasured" is not one of measured|estimated`) {
		t.Errorf("the reply's handoff with basis unmeasured gave %v; the parser must keep refusing it", err)
	}
}

// TestAFencedBlockClosesOnlyAtItsOwnDepth is the scanner's table: what opens
// a nested fence, what closes it, and what closes the reserved block.
func TestAFencedBlockClosesOnlyAtItsOwnDepth(t *testing.T) {
	cases := []struct {
		name, reply string
		want        []string
	}{
		{"plain block, unchanged",
			"x\n```gaos-handoff\na: 1\n```\ny", []string{"a: 1"}},
		{"info-string fence nests",
			"```gaos-design\nA\n```yaml\nk: v\n```\nB\n```\ntail", []string{"A\n```yaml\nk: v\n```\nB"}},
		{"indented nested fence in a list",
			"```gaos-design\n- step\n  ```bash\n  ls\n  ```\nB\n```", []string{"- step\n  ```bash\n  ls\n  ```\nB"}},
		{"four-backtick block keeps a bare triple",
			"````gaos-design\nA\n```\nB\n````\ntail", []string{"A\n```\nB"}},
		{"longer nested fence closes only with its own length",
			"```gaos-design\n````md\n```\ninner\n```\n````\nB\n```", []string{"````md\n```\ninner\n```\n````\nB"}},
		{"tilde fence nests and hides a bare backtick fence",
			"```gaos-design\n~~~\n```\n~~~\nB\n```", []string{"~~~\n```\n~~~\nB"}},
		{"unclosed block is ignored",
			"```gaos-design\nA\n```ts\nx\n```\n", nil},
		{"two blocks, in order",
			"```gaos-review\n1\n```\n```gaos-review\n2\n```", []string{"1", "2"}},
	}
	for _, c := range cases {
		tag := HandoffFence
		if strings.Contains(c.reply, DesignFence) {
			tag = DesignFence
		} else if strings.Contains(c.reply, ReviewFence) {
			tag = ReviewFence
		}
		got := fenceBlocks(c.reply, tag)
		if strings.Join(got, "\x00") != strings.Join(c.want, "\x00") || len(got) != len(c.want) {
			t.Errorf("%s: fenceBlocks gave %q, want %q", c.name, got, c.want)
		}
	}

	// A reserved tag nested inside a design is now kept inside the design
	// rather than closing it, and ParseDesign still refuses it.
	reply := "```gaos-design\n---\ntask: t-1\nrun: t-1-a1\n---\n# Doc\n```gaos-handoff\nstatus: complete\n```\nafter\n```\n"
	if _, err := ExtractDesign(reply, "t-1", "t-1-a1"); err == nil || !strings.Contains(err.Error(), "which is reserved") {
		t.Errorf("a design quoting a gaos-handoff block gave %v; it must still be refused as reserved", err)
	}
}

// TestTheContractStatesTheAllowedUsageBasis: the parser refuses any basis but
// measured or estimated, so the contract the model reads must say so next to
// the field rather than leave it to be guessed from one example.
func TestTheContractStatesTheAllowedUsageBasis(t *testing.T) {
	c := gatewayContract("t-42", "t-42-a2", "qa")
	for _, want := range []string{"usage.basis is exactly one of: measured | estimated", "use estimated when"} {
		if !strings.Contains(c, want) {
			t.Errorf("the contract never says %q; a model that writes basis: unmeasured fails an "+
				"otherwise complete turn on a rule nobody stated", want)
		}
	}
	for _, basis := range []string{CostMeasured, CostEstimated} {
		if !strings.Contains(c, basis) {
			t.Errorf("the contract does not name the basis %q the parser accepts", basis)
		}
	}
}
