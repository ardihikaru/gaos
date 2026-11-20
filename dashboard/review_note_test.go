package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The review note's length limit (v0.40.4).
//
// Found by replaying one real reviewer reply
// (testdata/review-reply-long-note.txt): a correct review, no blockers, one
// major and two nits, refused because the major's note ran past the parser's
// 300-rune cap -- a cap the contract never stated, behind a refusal that never
// said which rule was broken.
//
// Every test here is a top-level func Test…, and each has a row in the QA
// register (pustaka/docs/product/qa-test-plan.html).

// TestTheReviewContractStatesTheNoteLimit: the limit the reviewer reads is the
// limit the parser holds it to, read from the same constant.
func TestTheReviewContractStatesTheNoteLimit(t *testing.T) {
	c := reviewContract("t-42", "t-42-a1", "qa")
	want := fmt.Sprintf("The note is one line of at most %d characters", maxFindingNote)
	if !strings.Contains(c, want) {
		t.Errorf("the review contract never says %q; a reviewer whose note runs long fails "+
			"an otherwise complete review on a rule nobody stated", want)
	}
	if !strings.Contains(c, "A longer\n  note fails the whole review") {
		t.Errorf("the review contract does not say what a longer note costs")
	}
}

// TestAFindingNoteIsHeldToTheStatedLimit is the boundary: exactly the limit
// passes, one rune over is refused naming both numbers, and a line break is
// refused as a line break rather than as a length.
func TestAFindingNoteIsHeldToTheStatedLimit(t *testing.T) {
	block := func(note string) []byte {
		return []byte("---\ntask: t-9\nrun: t-9-a1\nrole: qa\nfindings:\n" +
			"  - severity: nit   scope: \"a.go:1\"   note: \"" + note + "\"\n---\n")
	}
	// A multi-byte rune, so the limit is shown to count characters, not bytes.
	at := strings.Repeat("é", maxFindingNote)
	if _, err := ParseReview(block(at)); err != nil {
		t.Errorf("a note of exactly %d characters was refused: %v", maxFindingNote, err)
	}
	_, err := ParseReview(block(at + "x"))
	if err == nil {
		t.Fatalf("a note of %d characters was accepted; the limit is %d", maxFindingNote+1, maxFindingNote)
	}
	for _, want := range []string{SigReviewMissing, fmt.Sprintf("is %d characters", maxFindingNote+1),
		fmt.Sprintf("the limit is %d", maxFindingNote)} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal %q does not say %q; a retry must be told what to shorten, and by how much",
				err, want)
		}
	}
	_, err = ParseReview(block("one line\x00two"))
	if err == nil || !strings.Contains(err.Error(), "line break or a control character") {
		t.Errorf("a note carrying a control character gave %v; it must be refused as one", err)
	}
}

// TestARealReviewWithALongNoteSaysWhyItWasRefused replays the real reply,
// and then drives a review with a long note through the gate, whose run line
// on the timeline must carry the parser's reason.
func TestARealReviewWithALongNoteSaysWhyItWasRefused(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("testdata", "review-reply-long-note.txt"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = ExtractReview(string(b), "wf-8-t-4", "wf-8-t-4-a1")
	if err == nil {
		t.Fatalf("the real reply with a note past %d characters was accepted; the parser must keep refusing it",
			maxFindingNote)
	}
	want := fmt.Sprintf("characters and the limit is %d", maxFindingNote)
	if !strings.Contains(err.Error(), want) {
		t.Errorf("the real reply was refused with %q; it must say %q", err, want)
	}

	f := gateFixtureNew(t)
	designID := f.taskOfKind(t, TaskKindDesign)
	reviewID := f.taskOfKind(t, TaskKindReview)
	f.finish(t, designID, designID+"-a1", designReply(designID, designID+"-a1", "# The design"))
	long := strings.Repeat("n", maxFindingNote+1)
	f.finish(t, reviewID, reviewID+"-a1", reviewReply(reviewID, reviewID+"-a1",
		`  - severity: major   scope: "docs/design.md"   note: "`+long+`"`))

	if r := f.taskRecord(t, reviewID); r.Status != StatusBlocked || r.BlockReason != ReasonReviewFailed {
		t.Errorf("the review is %q (%q); a refused review is BLOCKED with %q", r.Status, r.BlockReason, ReasonReviewFailed)
	}
	f.s.mu.RLock()
	defer f.s.mu.RUnlock()
	line := ""
	for _, e := range f.s.events {
		if e.Type == RunExited && strings.Contains(e.Text, reviewID+"-a1") {
			line = e.Text
		}
	}
	wantLine := fmt.Sprintf("the review was refused: %s: the note of a finding is %d characters and the limit is %d",
		SigReviewMissing, maxFindingNote+1, maxFindingNote)
	if !strings.Contains(line, wantLine) {
		t.Errorf("the run's line on the timeline is %q; it must carry %q, or the operator retries "+
			"a review without knowing what the reviewer must change", line, wantLine)
	}
}
