package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// The send-back (D-084, docs/designs/design-iterate.md).
//
// A design the review blocked goes back to its author: the design DONE ->
// READY, the review BLOCKED -> PENDING on its own DEPENDS_ON edge, so the
// scheduler promotes it again when the revision is delivered. The design's
// next packet carries its previous version, the review's findings and the
// operator's triage; the review's next attempt opens a fresh session. At most
// maxDesignSendBacks per design, then the work is canceled. There is no
// clearance without a review: the brief's conditional override is a contested
// send-back that a fresh review clears.

// maxDesignSendBacks is the cap. Every "of 3" the operator or the designer
// reads is this constant.
const maxDesignSendBacks = 3

// triageDispositions are the four dispositions of
// docs/rules/gaos-design-review.md ("The flow", step 5).
var triageDispositions = []string{"CONTEST", "ESCALATE", "PROPOSE FIX", "DECLINE"}

// SendBackView is what the send-back sheet shows: the two tasks, the count,
// the findings, and the refusal sentence when the design may not go back now.
type SendBackView struct {
	Review, Design string
	Sent, Cap      int
	Findings       []ReviewFinding
	Refusal        string // "" when the design may be sent back now
	DesignBytes    int    // the size of the design being sent back
	PartLimit      int    // the egress per-part limit; a larger design is dropped from a packet
}

// sendBack is the shared check's answer.
type sendBack struct {
	Review, Design string
	Sent           int
	Findings       []ReviewFinding
	Artifact       ArtifactRecord // the review artifact
}

// reviewVerdictOf reads a review artifact's verdict from its anchored line:
// the heading, a blank line, then `verdict: accept|iterate`
// (renderReviewArtifact). Anything else is "" -- a reviewer's note that says
// "verdict: accept" somewhere in the body cannot satisfy it.
func reviewVerdictOf(body string) string {
	lines := strings.Split(body, "\n")
	if len(lines) < 3 || !strings.HasPrefix(lines[0], "# Design review") || lines[1] != "" {
		return ""
	}
	v, ok := strings.CutPrefix(lines[2], "verdict: ")
	if !ok || (v != VerdictAccept && v != VerdictIterate) {
		return ""
	}
	return v
}

// reviewFindingsOf reads back the findings renderReviewArtifact wrote, one per
// `- <severity>  <scope>  <note>` line. A severity and a scope never hold a
// space (validScope), so the split on the first two double spaces is exact even
// when a note holds double spaces of its own. A line that does not split into
// three non-empty parts is skipped.
func reviewFindingsOf(body string) []ReviewFinding {
	var out []ReviewFinding
	for _, line := range strings.Split(body, "\n") {
		rest, ok := strings.CutPrefix(line, "- ")
		if !ok {
			continue
		}
		sev, rest, ok1 := strings.Cut(rest, "  ")
		scope, note, ok2 := strings.Cut(rest, "  ")
		if !ok1 || !ok2 || sev == "" || scope == "" || strings.TrimSpace(note) == "" {
			continue
		}
		out = append(out, ReviewFinding{Severity: sev, Scope: scope, Note: note})
	}
	return out
}

// sendBacksLocked counts the send-backs of a design and returns the newest
// one's event (the zero Event when there is none). One scan of the stream.
// Caller holds the lock.
func (s *MemStore) sendBacksLocked(designID string) (int, Event) {
	n, last := 0, Event{}
	for _, e := range s.events {
		if e.Type == TaskMoved && e.Payload.Task == designID && e.Payload.Rule == MoveSendBack {
			n++
			if e.Seq > last.Seq {
				last = e
			}
		}
	}
	return n, last
}

// reviewSentBackSinceRunLocked reports whether a review was sent back (its
// design returned to its author) after its newest attempt that really ran. A
// task reaches RUNNING only after its run record exists, so a dispatch that
// never started a run cannot hide a send-back. Decided from the stream's
// sequence, never from a clock. Caller holds the lock.
func (s *MemStore) reviewSentBackSinceRunLocked(reviewID string) bool {
	sb, rn := 0, 0
	for _, e := range s.events {
		if e.Type != TaskMoved || e.Payload.Task != reviewID {
			continue
		}
		if e.Payload.Rule == MoveSendBack && e.Seq > sb {
			sb = e.Seq
		}
		if e.Payload.To == StatusRunning && e.Seq > rn {
			rn = e.Seq
		}
	}
	return sb > 0 && sb > rn
}

// reviewOfDesignLocked is the review task gated on a design: the first review
// task whose design dependency is it. The plan builds one per design.
func (s *MemStore) reviewOfDesignLocked(designID string) string {
	for i := range s.tasks {
		t := &s.tasks[i]
		if taskKind(t.Kind) == TaskKindReview && s.gateDesignDepLocked(t) == designID {
			return t.ID
		}
	}
	return ""
}

// designArtifactsOfLocked counts the design artifacts a design task produced:
// one per delivery.
func (s *MemStore) designArtifactsOfLocked(designID string) int {
	n := 0
	for _, a := range s.artifacts {
		if a.TaskID == designID && a.Kind == ArtifactDesign {
			n++
		}
	}
	return n
}

// operatorHolds reports whether a block reason is one only an operator's
// decision releases: the gate's verdict and hold, and a QA-added card
// (BR-54). A Blueprint sync's drift release leaves such a task blocked.
func operatorHolds(reason string) bool {
	switch reason {
	case ReasonDesignBlocked, ReasonDesignUnreviewed, ReasonQaEpicAdded, ReasonUnitHeld:
		return true
	}
	return false
}

// sendBackCheckLocked is the one reading of "may this review's design be sent
// back now?", shared by the write and the sheet. Caller holds the lock.
func (s *MemStore) sendBackCheckLocked(productID, reviewTaskID string) (sendBack, error) {
	t := s.taskByIDLocked(reviewTaskID)
	if t == nil || t.DeskID != productID {
		return sendBack{}, ErrNotFound
	}
	if taskKind(t.Kind) != TaskKindReview {
		return sendBack{}, reject(fmt.Sprintf(
			"%s is not a design review; only a review the design gate blocked sends a design back", t.ID))
	}
	if t.Status != StatusBlocked || t.BlockReason != ReasonDesignBlocked {
		reason := t.BlockReason
		if reason == "" {
			reason = "-"
		}
		return sendBack{}, reject(fmt.Sprintf(
			"%s is %s (%s); a design goes back only from a review whose findings blocked it", t.ID, t.Status, reason))
	}
	designID := s.gateDesignDepLocked(t)
	d := s.taskByIDLocked(designID)
	if designID == "" || d == nil || d.Status != StatusDone {
		return sendBack{}, reject(fmt.Sprintf("%s has no delivered design to send back", t.ID))
	}
	art, ok := s.reviewArtifactFor(reviewTaskID)
	if !ok || reviewVerdictOf(art.Body) != VerdictIterate {
		return sendBack{}, reject(fmt.Sprintf("%s has no recorded verdict that asks for a revision", t.ID))
	}
	sent, _ := s.sendBacksLocked(designID)
	if sent >= maxDesignSendBacks {
		return sendBack{}, reject(fmt.Sprintf(
			"%s has been sent back %d times, which is the limit; cancel %s and the work it holds, and plan the change again",
			designID, sent, t.ID))
	}
	return sendBack{Review: t.ID, Design: designID, Sent: sent, Findings: reviewFindingsOf(art.Body), Artifact: art}, nil
}

// normalizeTriage checks and tidies the operator's triage against the number
// of findings. Pure. Entries with neither a disposition nor a note are
// dropped; the rest are checked in a fixed order and sorted by N.
func normalizeTriage(in []FindingTriage, findings int) ([]FindingTriage, error) {
	var kept []FindingTriage
	seen := map[int]bool{}
	for _, f := range in {
		f.Note = strings.TrimSpace(f.Note)
		f.Disposition = strings.TrimSpace(f.Disposition)
		if f.Disposition == "" && f.Note == "" {
			continue
		}
		if f.N < 1 || f.N > findings {
			return nil, reject(fmt.Sprintf("finding %d does not exist; the review has %d findings", f.N, findings))
		}
		if seen[f.N] {
			return nil, reject(fmt.Sprintf("finding %d is triaged twice", f.N))
		}
		seen[f.N] = true
		if f.Disposition != "" && !containsString(triageDispositions, f.Disposition) {
			return nil, reject(fmt.Sprintf(
				"finding %d: %s is not a disposition; use CONTEST, ESCALATE, PROPOSE FIX or DECLINE", f.N, f.Disposition))
		}
		if n := len([]rune(f.Note)); n > maxFindingNote {
			return nil, reject(fmt.Sprintf("finding %d: the note is %d characters and the limit is %d", f.N, n, maxFindingNote))
		}
		for _, r := range f.Note {
			if unicode.In(r, unicode.Cc, unicode.Cf, unicode.Zl, unicode.Zp) {
				return nil, reject(fmt.Sprintf("finding %d: the note is one line of plain text", f.N))
			}
		}
		if reservedFenceIn(f.Note) != "" {
			return nil, reject(fmt.Sprintf("finding %d: the note carries a reserved block", f.N))
		}
		kept = append(kept, f)
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].N < kept[j].N })
	return kept, nil
}

func containsString(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// renderTriage is the operator's triage as the designer reads it, under the
// review artifact in findings.md. No actor: an email never enters a prompt.
// Every note is a quoted Go string literal, so a quote inside it cannot end it.
func renderTriage(findings []ReviewFinding, triage []FindingTriage, revision int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Sent back for revision %d of %d\n\n", revision, maxDesignSendBacks)
	b.WriteString("The operator's triage of the findings above. A finding with no line here was not triaged.\n\n")
	if len(triage) == 0 {
		b.WriteString("- none\n")
	}
	for _, f := range triage {
		sev, scope := "?", "?"
		if f.N >= 1 && f.N <= len(findings) {
			sev, scope = findings[f.N-1].Severity, findings[f.N-1].Scope
		}
		disp := f.Disposition
		if disp == "" {
			disp = "—"
		}
		fmt.Fprintf(&b, "- finding %d (%s, %s): %s", f.N, sev, scope, disp)
		if f.Note != "" {
			if f.Disposition != "" {
				b.WriteString(" —")
			}
			b.WriteString(" " + strconv.Quote(f.Note))
		}
		b.WriteString("\n")
	}
	b.WriteString("\nRevise the design in .gaos/packet/design.md and deliver it whole. Change the design so it stands on its own: " +
		"where you keep a choice a finding questioned, state why the choice is right as a property of the design, " +
		"without referring to any review, finding or triage. A fresh reviewer who has seen none of them reads the result.\n")
	return b.String()
}

// SendBackDesign sends the design a review blocked back to its author (D-084).
// actor is the signed-in account; an empty one is refused.
func (s *MemStore) SendBackDesign(ctx context.Context, productID, reviewTaskID, actor string, triage []FindingTriage) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(actor) == "" {
		return Result{}, reject("a send-back needs a signed-in account")
	}
	sb, err := s.sendBackCheckLocked(productID, reviewTaskID)
	if err != nil {
		return Result{}, err
	}
	kept, err := normalizeTriage(triage, len(sb.Findings))
	if err != nil {
		return Result{}, err
	}
	// Screened before anything is written: the events are append-only (BR-04).
	if !s.screenArtifactBody(".gaos/packet/context/findings.md", renderTriage(sb.Findings, kept, sb.Sent+1)) {
		return Result{}, reject("the triage was refused by the egress screen; remove the secret and send it again")
	}
	var out []Event
	e, ok := s.moveTaskWithLocked(sb.Design, StatusReady, MoveSendBack, EventPayload{Actor: actor, Triage: kept})
	if !ok {
		return Result{}, reject(fmt.Sprintf("%s could not be re-opened", sb.Design))
	}
	out = append(out, e)
	if m, ok := s.moveTaskWithLocked(sb.Review, StatusPending, MoveSendBack, EventPayload{Actor: actor}); ok {
		out = append(out, m)
	}
	if r := s.taskByIDLocked(sb.Review); r != nil {
		r.BlockReason = ""
	}
	for i := range s.tasks {
		t := &s.tasks[i]
		if taskKind(t.Kind) == TaskKindImplement && t.Status == StatusBlocked && s.gateReviewDepLocked(t) == sb.Review {
			t.BlockReason = ReasonDesignUnreviewed
		}
	}
	return s.result(fmt.Sprintf("%s goes back to its author (revision %d of %d); %s reviews it again when it is delivered.",
		sb.Design, sb.Sent+1, maxDesignSendBacks, sb.Review), out...), nil
}

// SendBackSheet is the sheet's read: the same check, without writing.
func (s *MemStore) SendBackSheet(ctx context.Context, productID, reviewTaskID string) (SendBackView, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v := SendBackView{Review: reviewTaskID, Cap: maxDesignSendBacks, PartLimit: s.egress.MaxPartBytes}
	sb, err := s.sendBackCheckLocked(productID, reviewTaskID)
	if errors.Is(err, ErrNotFound) {
		return SendBackView{}, err
	}
	if err != nil {
		v.Refusal = err.Error()
		if t := s.taskByIDLocked(reviewTaskID); t != nil {
			v.Design = s.gateDesignDepLocked(t)
			v.Sent, _ = s.sendBacksLocked(v.Design)
		}
		return v, nil
	}
	v.Design, v.Sent, v.Findings = sb.Design, sb.Sent, sb.Findings
	if art, ok := s.designArtifactFor(sb.Design); ok {
		v.DesignBytes = len(art.Body)
	}
	return v, nil
}

// linesChanged is how much of the previous design a revision did not keep: the
// lines of prev (trailing spaces trimmed, blank lines ignored) that do not
// occur in next. A set difference, not a diff -- it is a record for the
// operator, not a merge.
func linesChanged(prev, next string) (changed, total int) {
	have := map[string]bool{}
	for _, l := range strings.Split(next, "\n") {
		have[strings.TrimRight(l, " \t\r")] = true
	}
	for _, l := range strings.Split(prev, "\n") {
		l = strings.TrimRight(l, " \t\r")
		if l == "" {
			continue
		}
		total++
		if !have[l] {
			changed++
		}
	}
	return changed, total
}

// decorateCardsGate is the gate's read-time decoration of the board (D-084):
// the reason a BLOCKED card is held, the send-back state of a design-blocked
// review, and the removal of the moves the board would refuse -- unblocking
// that review, and approving a design or review task. Read-time, like the
// timing, so the fold's board (TC-308) is compared without it. Caller holds the
// lock.
//
// byID is the product's tasks by id: a lookup per card, where this used to scan
// every task for every card (D-095).
func (s *MemStore) decorateCardsGate(cards []BoardCard, byID map[string]*TaskRecord) {
	for i := range cards {
		c := &cards[i]
		t := byID[c.ID]
		if t == nil {
			continue
		}
		if t.Status == StatusBlocked && t.BlockReason != "" {
			c.Reason = t.BlockReason
		}
		kind := taskKind(t.Kind)
		if kind == TaskKindReview && t.Status == StatusBlocked && t.BlockReason == ReasonDesignBlocked {
			design := s.gateDesignDepLocked(t)
			sent, _ := s.sendBacksLocked(design)
			c.SendBack = &BoardSendBack{Design: design, Sent: sent, Cap: maxDesignSendBacks, Open: sent < maxDesignSendBacks}
			c.Moves = withoutMove(c.Moves, ColReady)
		}
		if (kind == TaskKindDesign || kind == TaskKindReview) && t.Status == StatusReview {
			c.Moves = withoutMove(c.Moves, ColDone)
		}
	}
}

func withoutMove(moves []string, col string) []string {
	out := moves[:0:0]
	for _, m := range moves {
		if m != col {
			out = append(out, m)
		}
	}
	return out
}
