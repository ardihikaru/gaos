package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// The send-back (D-084, docs/designs/design-iterate.md). Every test here is a
// top-level func Test... with its own row in the QA register (TC-500...TC-507).
// They reuse the design gate's fixture (designgate_test.go).

const sbActor = "operator@example.test"

// The documents and replies of one blocked gate.
const (
	sbDesign1 = "# The design\n\nThe order is A then B.\nThe store is one table.\n"
	sbDesign2 = "# The design\n\nThe order is A then B, and B waits on A's commit.\nThe store is one table.\n"
	// The blocker's note holds a double space on purpose: reviewFindingsOf
	// must split on the first two double spaces only.
	sbBlocker = `- severity: blocker   scope: "src/thing.go:12"   note: "the order  of A and B is not stated as a guarantee"`
	sbNit     = `- severity: nit   scope: "docs/a.md"   note: "a typo in the heading"`
)

// run drives one attempt of a task to its end: a run record with the given
// attempt, session and Blueprint pin, the moves a real run goes through, and
// finishRun -- which is where the gate's hook lives.
func (f gateFixture) run(t *testing.T, taskID string, attempt int, reply, session string, pin int) {
	t.Helper()
	runID := fmt.Sprintf("%s-a%d", taskID, attempt)
	f.s.mu.Lock()
	f.s.startRun(TaskRunRecord{ID: runID, TaskID: taskID, DeskID: f.desk, Attempt: attempt,
		Status: RunStatusRunning, StartedAt: time.Now().Unix(), BlueprintVersion: pin})
	f.s.mu.Unlock()
	f.s.moveTaskByID(taskID, StatusReady, MoveSchedule)
	f.s.moveTaskByID(taskID, StatusDispatched, MoveSchedule)
	f.s.moveTaskByID(taskID, StatusRunning, MoveStart)
	f.s.finishRun(f.desk, taskID, runID, Attempt{Reply: reply, ExitClass: ExitRan, HandoffRaw: reply, SessionID: session}, nil)
}

// blockedGate is a gate whose review blocked the design: design DONE, review
// BLOCKED / design-blocked with two findings, implementation held.
func blockedGate(t *testing.T) (f gateFixture, design, review, impl string) {
	t.Helper()
	f = gateFixtureNew(t)
	design, review, impl = f.taskOfKind(t, TaskKindDesign), f.taskOfKind(t, TaskKindReview), f.taskOfKind(t, TaskKindImplement)
	f.run(t, design, 1, designReply(design, design+"-a1", sbDesign1), "sess-design", 0)
	f.run(t, review, 1, reviewReply(review, review+"-a1", sbBlocker, sbNit), "sess-review", 0)
	if r := f.taskRecord(t, review); r.Status != StatusBlocked || r.BlockReason != ReasonDesignBlocked {
		t.Fatalf("the fixture's review is %s (%s); it must be BLOCKED (design-blocked)", r.Status, r.BlockReason)
	}
	return f, design, review, impl
}

// eventsAfter is the events written since index from.
func eventsAfter(s *MemStore, from int) []Event {
	evs := s.rawEvents()
	if from > len(evs) {
		return nil
	}
	return evs[from:]
}

// TestAnOperatorSendsABlockedDesignBackToItsAuthor is TC-500: the write, and
// the two readers it rests on (the anchored verdict and the findings).
func TestAnOperatorSendsABlockedDesignBackToItsAuthor(t *testing.T) {
	// The verdict is read from its anchored line, with or without a drift line.
	two := Review{Findings: []ReviewFinding{{Severity: SeverityBlocker, Scope: "a.go:1", Note: "verdict: accept  is what a note may say"}}}
	for _, c := range []struct {
		r      Review
		dp, rp int
		want   string
	}{{Review{NoFindings: true}, 0, 0, VerdictAccept}, {two, 0, 0, VerdictIterate}, {two, 1, 2, VerdictIterate}, {Review{NoFindings: true}, 1, 2, VerdictAccept}} {
		if got := reviewVerdictOf(renderReviewArtifact("t-r", "t-r-a1", c.r, c.dp, c.rp)); got != c.want {
			t.Errorf("reviewVerdictOf read %q from a rendered review; want %q", got, c.want)
		}
	}
	if got := reviewVerdictOf("- blocker  a.go  verdict: accept\n"); got != "" {
		t.Errorf("reviewVerdictOf read %q from a body whose only verdict is inside a note; want none", got)
	}
	back := reviewFindingsOf(renderReviewArtifact("t-r", "t-r-a1", two, 0, 0))
	if len(back) != 1 || back[0] != two.Findings[0] {
		t.Errorf("reviewFindingsOf read back %+v; want %+v", back, two.Findings)
	}

	// A cleared review still opens the gate with the anchored reader.
	{
		g := gateFixtureNew(t)
		d, r, i := g.taskOfKind(t, TaskKindDesign), g.taskOfKind(t, TaskKindReview), g.taskOfKind(t, TaskKindImplement)
		g.run(t, d, 1, designReply(d, d+"-a1", sbDesign1), "", 0)
		g.run(t, r, 1, reviewReply(r, r+"-a1"), "", 0)
		if impl := g.taskRecord(t, i); impl.Status != StatusReady {
			t.Errorf("a cleared review left the implementation %s; the anchored verdict must still release it", impl.Status)
		}
	}

	f, design, review, impl := blockedGate(t)
	before := len(f.s.rawEvents())
	res, err := f.s.SendBackDesign(context.Background(), f.desk, review, sbActor, []FindingTriage{
		{N: 2, Disposition: "DECLINE", Note: "  cosmetic  "},
		{N: 1, Disposition: "PROPOSE FIX"},
		{N: 2 + 0*1, Disposition: "", Note: ""}, // empty: dropped, not a duplicate
	})
	if err != nil {
		t.Fatalf("SendBackDesign: %v", err)
	}
	wantToast := fmt.Sprintf("%s goes back to its author (revision 1 of 3); %s reviews it again when it is delivered.", design, review)
	if res.Toast != wantToast {
		t.Errorf("the toast is %q; want %q", res.Toast, wantToast)
	}
	if d := f.taskRecord(t, design); d.Status != StatusReady {
		t.Errorf("the design is %s after a send-back; it must be READY", d.Status)
	}
	if r := f.taskRecord(t, review); r.Status != StatusPending || r.BlockReason != "" {
		t.Errorf("the review is %s (%q) after a send-back; it must be PENDING with no reason", r.Status, r.BlockReason)
	}
	if i := f.taskRecord(t, impl); i.Status != StatusBlocked || i.BlockReason != ReasonDesignUnreviewed {
		t.Errorf("the implementation is %s (%s) after a send-back; it must be BLOCKED (design-unreviewed)", i.Status, i.BlockReason)
	}
	var moves []Event
	for _, e := range eventsAfter(f.s, before) {
		if e.Type == TaskMoved {
			moves = append(moves, e)
		}
	}
	if len(moves) != 2 {
		t.Fatalf("a send-back wrote %d TaskMoved; want 2 (the design and the review)", len(moves))
	}
	dm, rm := moves[0], moves[1]
	if dm.Payload.Task != design || dm.Payload.Rule != MoveSendBack || dm.Payload.To != StatusReady || dm.Payload.Actor != sbActor {
		t.Errorf("the design's send-back event is %+v; it must name the design, send-back, READY and the actor", dm.Payload)
	}
	if rm.Payload.Task != review || rm.Payload.Rule != MoveSendBack || rm.Payload.To != StatusPending || rm.Payload.Actor != sbActor {
		t.Errorf("the review's send-back event is %+v; it must name the review, send-back, PENDING and the actor", rm.Payload)
	}
	wantTriage := []FindingTriage{{N: 1, Disposition: "PROPOSE FIX"}, {N: 2, Disposition: "DECLINE", Note: "cosmetic"}}
	if fmt.Sprint(dm.Payload.Triage) != fmt.Sprint(wantTriage) {
		t.Errorf("the design's send-back carries the triage %+v; want %+v (sorted, trimmed, empty dropped)", dm.Payload.Triage, wantTriage)
	}
	if want := fmt.Sprintf("%s: send-back (DONE → READY) by %s", design, sbActor); dm.Text != want {
		t.Errorf("the send-back's caption is %q; want %q", dm.Text, want)
	}
	for _, e := range f.s.rawEvents()[:before] {
		if strings.Contains(e.Text, " by ") && e.Type == TaskMoved {
			t.Errorf("a move that is not a send-back carries an actor in its caption: %q", e.Text)
		}
	}
}

// TestTheRevisionCarriesItsDesignTheFindingsAndTheTriage is TC-501.
func TestTheRevisionCarriesItsDesignTheFindingsAndTheTriage(t *testing.T) {
	f, design, review, _ := blockedGate(t)
	freeze := func(attempt int) Packet {
		f.s.mu.RLock()
		defer f.s.mu.RUnlock()
		return f.s.freezePacketFor(f.s.desks[f.s.deskIdxByID(f.desk)], design, attempt)
	}
	if _, ok := partOf(freeze(2), ".gaos/packet/context/findings.md"); ok {
		t.Error("a design never sent back carries findings.md; it must carry neither the findings nor the triage")
	}
	if _, ok := partOf(freeze(2), ".gaos/packet/design.md"); ok {
		t.Error("a design never sent back carries design.md")
	}

	note := `keep A before B; the "order" is the contract`
	if _, err := f.s.SendBackDesign(context.Background(), f.desk, review, sbActor,
		[]FindingTriage{{N: 1, Disposition: "CONTEST", Note: note}}); err != nil {
		t.Fatalf("SendBackDesign: %v", err)
	}
	pkt := freeze(2)
	d, ok := partOf(pkt, ".gaos/packet/design.md")
	if !ok || d.Body != sbDesign1 || d.Kind != KindDesign {
		t.Errorf("the revision's design.md is %+v (present %v); it must be the design that was sent back", d, ok)
	}
	fd, ok := partOf(pkt, ".gaos/packet/context/findings.md")
	if !ok {
		t.Fatal("the revision's packet carries no findings.md")
	}
	f.s.mu.RLock()
	art, _ := f.s.reviewArtifactFor(review)
	f.s.mu.RUnlock()
	wantTriage := "## Sent back for revision 1 of 3\n\n" +
		"The operator's triage of the findings above. A finding with no line here was not triaged.\n\n" +
		"- finding 1 (blocker, src/thing.go:12): CONTEST — \"keep A before B; the \\\"order\\\" is the contract\"\n" +
		"\nRevise the design in .gaos/packet/design.md and deliver it whole. Change the design so it stands on its own: " +
		"where you keep a choice a finding questioned, state why the choice is right as a property of the design, " +
		"without referring to any review, finding or triage. A fresh reviewer who has seen none of them reads the result.\n"
	if fd.Body != art.Body+"\n"+wantTriage {
		t.Errorf("findings.md is\n%s\nwant the review artifact and then\n%s", fd.Body, wantTriage)
	}
	for _, part := range pkt.Parts {
		if strings.Contains(part.Body, sbActor) {
			t.Errorf("the revision's part %s carries the operator's account; an email never enters a prompt", part.Path)
		}
	}
	if c, _, err := Screen(f.s.egress, f.s.target, pkt); err == nil && strings.Contains(RenderPrompt(c), sbActor) {
		t.Error("the revision's prompt carries the operator's account")
	}
	f.s.mu.RLock()
	sess, reason := f.s.sessionForLocked(design, fakeTurnAdapter{}, "", "", time.Now().Unix())
	f.s.mu.RUnlock()
	if sess != "sess-design" || reason != "" {
		t.Errorf("the revision opens session %q (%q); it must resume the designer's own session", sess, reason)
	}
}

// TestTheReviewReturnsToReadyWhenTheRevisionIsDelivered is TC-502.
func TestTheReviewReturnsToReadyWhenTheRevisionIsDelivered(t *testing.T) {
	f, design, review, impl := blockedGate(t)
	if _, err := f.s.SendBackDesign(context.Background(), f.desk, review, sbActor, nil); err != nil {
		t.Fatalf("SendBackDesign: %v", err)
	}
	before := len(f.s.rawEvents())
	f.run(t, design, 2, designReply(design, design+"-a2", sbDesign2), "sess-design", 0)
	if d := f.taskRecord(t, design); d.Status != StatusDone {
		t.Fatalf("the revision is %s; a delivered revision is DONE", d.Status)
	}
	f.s.mu.RLock()
	n := f.s.designArtifactsOfLocked(design)
	f.s.mu.RUnlock()
	if n != 2 {
		t.Errorf("the design task holds %d design artifacts after one revision; want 2", n)
	}
	caption := ""
	for _, e := range eventsAfter(f.s, before) {
		if e.Type == RunExited {
			caption = e.Text
		}
	}
	if !strings.Contains(caption, "; revision 1 changes 1 of 3 lines of the design it replaces") {
		t.Errorf("the revision's RunExited caption is %q; it must record how much of the design it changed", caption)
	}

	// The scheduler promotes the waiting review, as it promoted the first one.
	// The design's own prerequisites are marked done so the review is the
	// one task the pass may promote (one claim per product, BR-38).
	designOrdinal := f.taskRecord(t, design).Ordinal
	f.s.mu.Lock()
	for i := range f.s.tasks {
		if f.s.tasks[i].DeskID == f.desk && f.s.tasks[i].Status == StatusPending && f.s.tasks[i].ID != review {
			if f.s.tasks[i].Ordinal < designOrdinal {
				f.s.tasks[i].Status = StatusDone
			}
		}
	}
	f.s.mu.Unlock()
	idleFleet(f.s)
	in := f.s.schedulerInput(time.Now().Unix())
	in.Tasks = withoutReady(in.Tasks)
	f.s.readyPending(Schedule(in))
	if r := f.taskRecord(t, review); r.Status != StatusReady {
		t.Fatalf("the review is %s after the revision was delivered; the scheduler must promote it to READY", r.Status)
	}
	f.run(t, review, 2, reviewReply(review, review+"-a2"), "sess-rr", 0)
	if i := f.taskRecord(t, impl); i.Status != StatusReady {
		t.Errorf("the implementation is %s after the re-review cleared; it must be released", i.Status)
	}
	f.s.mu.RLock()
	pkt := f.s.freezePacketFor(f.s.desks[f.s.deskIdxByID(f.desk)], impl, 1)
	f.s.mu.RUnlock()
	if d, ok := partOf(pkt, ".gaos/packet/design.md"); !ok || d.Body != sbDesign2 {
		t.Errorf("the implementation's design.md is %q; it must be the revised design", d.Body)
	}
	if fd, ok := partOf(pkt, ".gaos/packet/context/findings.md"); !ok || reviewVerdictOf(fd.Body) != VerdictAccept {
		t.Errorf("the implementation's findings.md is %q; it must be the re-review's accepting record", fd.Body)
	}

	// A design that reached DONE with no new artifact leaves its review holding.
	g, gd, gr, _ := blockedGate(t)
	if _, err := g.s.SendBackDesign(context.Background(), g.desk, gr, sbActor, nil); err != nil {
		t.Fatalf("SendBackDesign: %v", err)
	}
	g.s.mu.Lock()
	g.s.taskByIDLocked(gd).Status = StatusDone // as an approval of a reply with no design block would have
	g.s.mu.Unlock()
	g.run(t, gr, 2, reviewReply(gr, gr+"-a2"), "", 0)
	if r := g.taskRecord(t, gr); r.Status != StatusBlocked || r.BlockReason != ReasonDesignMissing {
		t.Errorf("a re-review with no new design is %s (%s); it must be BLOCKED (design-missing)", r.Status, r.BlockReason)
	}
}

// TestTheReReviewOpensAFreshSessionAndSeesOnlyTheNewDesign is TC-503.
func TestTheReReviewOpensAFreshSessionAndSeesOnlyTheNewDesign(t *testing.T) {
	f, design, review, _ := blockedGate(t)
	session := func() (string, string) {
		f.s.mu.RLock()
		defer f.s.mu.RUnlock()
		return f.s.sessionForLocked(review, fakeTurnAdapter{}, "", "", time.Now().Unix())
	}
	if sess, _ := session(); sess != "sess-review" {
		t.Fatalf("before a send-back the review resumes %q; want its own session (a retry, D-056)", sess)
	}
	triageNote := "the operator thinks this ordering is already explicit enough"
	if _, err := f.s.SendBackDesign(context.Background(), f.desk, review, sbActor,
		[]FindingTriage{{N: 1, Disposition: "CONTEST", Note: triageNote}}); err != nil {
		t.Fatalf("SendBackDesign: %v", err)
	}
	f.run(t, design, 2, designReply(design, design+"-a2", sbDesign2), "sess-design", 2)

	if sess, reason := session(); sess != "" || reason != SessionReviewer {
		t.Errorf("the re-review opens session %q (%q); it must open a fresh one with reason %q", sess, reason, SessionReviewer)
	}
	// A dispatch that never started a run cannot hide the send-back.
	f.s.moveTaskByID(review, StatusReady, MoveSchedule)
	f.s.moveTaskByID(review, StatusDispatched, MoveSchedule)
	f.s.moveTaskByID(review, StatusReady, RuleRequeued)
	if sess, reason := session(); sess != "" || reason != SessionReviewer {
		t.Errorf("after a requeue with no run the re-review opens %q (%q); it must still be fresh", sess, reason)
	}

	f.s.mu.RLock()
	pkt := f.s.freezePacketFor(f.s.desks[f.s.deskIdxByID(f.desk)], review, 2)
	f.s.mu.RUnlock()
	if d, ok := partOf(pkt, ".gaos/packet/design.md"); !ok || d.Body != sbDesign2 {
		t.Errorf("the re-review's design.md is %q; it must be the revised design", d.Body)
	}
	for _, part := range pkt.Parts {
		if strings.Contains(part.Body, triageNote) || strings.Contains(part.Body, "is not stated as a guarantee") {
			t.Errorf("the re-review's part %s carries the triage or the previous findings; a fresh reviewer sees neither", part.Path)
		}
	}

	// The re-review runs under a newer Blueprint: its record names the pins of
	// the revision and of the run being judged.
	f.run(t, review, 2, reviewReply(review, review+"-a2", sbBlocker), "sess-rr", 3)
	f.s.mu.RLock()
	art, _ := f.s.reviewArtifactFor(review)
	f.s.mu.RUnlock()
	if !strings.Contains(art.Body, "drift: this design was written under Blueprint version 2; the review runs under version 3") {
		t.Errorf("the re-review's record is\n%s\nit must name the revision's pin and the re-review run's pin", art.Body)
	}
	// After the re-review really ran, its retry resumes it.
	if sess, reason := session(); sess != "sess-rr" || reason != "" {
		t.Errorf("a retry of the re-review opens %q (%q); it must resume the re-review's own session", sess, reason)
	}
}

// TestSendingADesignBackIsRefusedUnlessItsReviewBlockedIt is TC-505.
func TestSendingADesignBackIsRefusedUnlessItsReviewBlockedIt(t *testing.T) {
	ctx := context.Background()
	f, design, review, impl := blockedGate(t)
	refused := func(name, product, task, actor string, triage []FindingTriage, want string) {
		t.Helper()
		before := len(f.s.rawEvents())
		was := f.taskRecord(t, design).Status
		_, err := f.s.SendBackDesign(ctx, product, task, actor, triage)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: SendBackDesign answered %v; want a refusal containing %q", name, err, want)
		}
		if n := len(f.s.rawEvents()) - before; n != 0 {
			t.Errorf("%s: a refused send-back wrote %d event(s)", name, n)
		}
		if d := f.taskRecord(t, design); d.Status != was {
			t.Errorf("%s: a refused send-back moved the design from %s to %s", name, was, d.Status)
		}
	}
	refused("no actor", f.desk, review, " ", nil, "a send-back needs a signed-in account")
	if _, err := f.s.SendBackDesign(ctx, "p-elsewhere", review, sbActor, nil); err != ErrNotFound {
		t.Errorf("another product's card answered %v; want ErrNotFound", err)
	}
	refused("not a review", f.desk, impl, sbActor, nil, "is not a design review")
	refused("bad N", f.desk, review, sbActor, []FindingTriage{{N: 3, Disposition: "CONTEST"}}, "finding 3 does not exist; the review has 2 findings")
	refused("twice", f.desk, review, sbActor, []FindingTriage{{N: 1, Note: "a"}, {N: 1, Note: "b"}}, "finding 1 is triaged twice")
	refused("disposition", f.desk, review, sbActor, []FindingTriage{{N: 1, Disposition: "contest"}}, "contest is not a disposition")
	refused("long", f.desk, review, sbActor, []FindingTriage{{N: 1, Note: strings.Repeat("x", 301)}}, "the note is 301 characters and the limit is 300")
	refused("line separator", f.desk, review, sbActor, []FindingTriage{{N: 1, Note: "a b"}}, "the note is one line of plain text")
	refused("bidi", f.desk, review, sbActor, []FindingTriage{{N: 1, Note: "a‮b"}}, "the note is one line of plain text")
	refused("fence", f.desk, review, sbActor, []FindingTriage{{N: 1, Note: "see " + fence + DesignFence}}, "the note carries a reserved block")
	refused("secret", f.desk, review, sbActor, []FindingTriage{{N: 1, Note: "key AKIA" + "IOSFODNN7EXAMPLE"}}, "refused by the egress screen")

	// The review must be the one the gate blocked, with a verdict that asks.
	f.s.mu.Lock()
	r := f.s.taskByIDLocked(review)
	r.BlockReason = ReasonReviewFailed
	f.s.mu.Unlock()
	refused("review-failed", f.desk, review, sbActor, nil, "is BLOCKED (review-failed); a design goes back only from a review whose findings blocked it")
	f.s.mu.Lock()
	r.BlockReason = ReasonDesignBlocked
	f.s.taskByIDLocked(design).Status = StatusReady
	f.s.mu.Unlock()
	refused("no delivered design", f.desk, review, sbActor, nil, "has no delivered design to send back")
	f.s.mu.Lock()
	f.s.taskByIDLocked(design).Status = StatusDone
	art, _ := f.s.reviewArtifactFor(review)
	art.Body = strings.Replace(art.Body, "verdict: iterate", "verdict: accept", 1)
	f.s.artifacts[art.ID] = art
	f.s.mu.Unlock()
	refused("verdict", f.desk, review, sbActor, nil, "has no recorded verdict that asks for a revision")
}

// TestADesignIsSentBackAtMostThreeTimes is TC-506.
func TestADesignIsSentBackAtMostThreeTimes(t *testing.T) {
	ctx := context.Background()
	f, design, review, _ := blockedGate(t)
	for round := 1; round <= maxDesignSendBacks; round++ {
		if _, err := f.s.SendBackDesign(ctx, f.desk, review, sbActor, nil); err != nil {
			t.Fatalf("send-back %d: %v", round, err)
		}
		a := round + 1
		f.run(t, design, a, designReply(design, fmt.Sprintf("%s-a%d", design, a), fmt.Sprintf("# The design, revision %d\n", round)), "sess-design", 0)
		f.run(t, review, a, reviewReply(review, fmt.Sprintf("%s-a%d", review, a), sbBlocker), "", 0)
		if r := f.taskRecord(t, review); r.BlockReason != ReasonDesignBlocked {
			t.Fatalf("round %d: the re-review is %s (%s); the fixture needs it blocked again", round, r.Status, r.BlockReason)
		}
	}
	_, err := f.s.SendBackDesign(ctx, f.desk, review, sbActor, nil)
	want := fmt.Sprintf("%s has been sent back 3 times, which is the limit; cancel %s and the work it holds, and plan the change again", design, review)
	if err == nil || err.Error() != want {
		t.Errorf("the fourth send-back answered %v; want %q", err, want)
	}
	b, _ := f.s.Board(ctx, f.desk)
	for _, c := range b.Cards {
		if c.ID != review {
			continue
		}
		if c.SendBack == nil || c.SendBack.Sent != 3 || c.SendBack.Open || c.SendBack.Cap != 3 {
			t.Errorf("the review card's sendBack is %+v; want sent 3 of 3, closed", c.SendBack)
		}
		for _, m := range c.Moves {
			if m == ColReady {
				t.Error("the review card at the cap offers ready; its findings block the design")
			}
		}
	}
	v, err := f.s.SendBackSheet(ctx, f.desk, review)
	if err != nil || v.Refusal != want || len(v.Findings) != 0 || v.Sent != 3 {
		t.Errorf("the sheet at the cap is %+v (%v); want the cap sentence, no findings, sent 3", v, err)
	}
}

// TestTheSendBackRoutesAreBackOfficeAndSameOrigin is TC-507: the two routes
// are the back office's, the POST is same-origin and form-only, the actor is
// the session's account, and the sheet and the board render the gate's state
// escaped -- including the break-ui worst cases.
func TestTheSendBackRoutesAreBackOfficeAndSameOrigin(t *testing.T) {
	f, design, review, _ := blockedGate(t)
	rend, err := NewRenderer(false, ".")
	if err != nil {
		t.Fatalf("templates: %v", err)
	}
	srv := &server{store: f.s, rend: rend, arc: &Arcade{DB: "gaos"}, auth: newTestAuthority()}
	path := "/products/" + f.desk + "/board/cards/" + review + "/send-back"
	form := url.Values{"d1": {"CONTEST"}, "n1": {"the order is stated"}}
	post := func(ct string, body string, headers map[string]string, cookies bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		if ct != "" {
			r.Header.Set("Content-Type", ct)
		}
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		if cookies {
			for _, c := range boCookies() {
				r.AddCookie(c)
			}
		}
		w := httptest.NewRecorder()
		srv.routes().ServeHTTP(w, r)
		return w
	}
	const urlenc = "application/x-www-form-urlencoded"
	if w := post(urlenc, form.Encode(), nil, false); w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "sign in to the back office") {
		t.Errorf("a send-back with no session answered %d %s; want 401", w.Code, w.Body.String())
	}
	if w := do(t, srv, "POST", path, form); w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), `id="loginSheet"`) {
		t.Errorf("an HTMX send-back with no session answered %d; want the login sheet", w.Code)
	}
	for _, h := range []map[string]string{
		{"Sec-Fetch-Site": "cross-site"}, {"Sec-Fetch-Site": "same-site"}, {"Origin": "https://evil.example"},
	} {
		if w := post(urlenc, form.Encode(), h, true); w.Code != http.StatusForbidden {
			t.Errorf("a send-back with %v answered %d; want 403", h, w.Code)
		}
	}
	for _, ct := range []string{"application/json", "multipart/form-data; boundary=x", ""} {
		if w := post(ct, `{"d1":"CONTEST"}`, nil, true); w.Code != http.StatusUnsupportedMediaType {
			t.Errorf("a send-back with content type %q answered %d; want 415", ct, w.Code)
		}
	}
	other := httptest.NewRequest(http.MethodGet, "/ui/send-back/p-elsewhere/"+review, nil)
	other.Header.Set("HX-Request", "true")
	for _, c := range boCookies() {
		other.AddCookie(c)
	}
	ow := httptest.NewRecorder()
	srv.routes().ServeHTTP(ow, other)
	if ow.Code != http.StatusNotFound {
		t.Errorf("the sheet of another product's card answered %d; want 404", ow.Code)
	}

	// The sheet and the server board, with worst-case findings: many, long,
	// non-Latin, unbroken, and one carrying markup.
	f.s.mu.Lock()
	art, _ := f.s.reviewArtifactFor(review)
	var b strings.Builder
	b.WriteString(art.Body)
	for i := 0; i < 38; i++ {
		fmt.Fprintf(&b, "- nit  docs/%s.md  %s\n", strings.Repeat("d", 150), strings.Repeat("مراجعة ", 40))
	}
	b.WriteString("- major  a.go:1  <script>alert(1)</script>" + strings.Repeat("x", 280) + "\n")
	art.Body = b.String()
	f.s.artifacts[art.ID] = art
	des, _ := f.s.designArtifactFor(design)
	des.Body = strings.Repeat("A long design line.\n", 4000) // over a 64 KiB part
	f.s.artifacts[des.ID] = des
	f.s.egress.MaxPartBytes = 64 << 10
	f.s.mu.Unlock()
	sheet := do(t, srv, "GET", "/ui/send-back/"+f.desk+"/"+review, nil, boCookies()...)
	body := sheet.Body.String()
	if sheet.Code != 200 || !strings.Contains(body, `id="sendBackModal"`) || !strings.Contains(body, `id="sendBackForm"`) ||
		strings.Count(body, `<li class="sb-finding">`) != 41 || !strings.Contains(body, "revision 1 of 3") {
		t.Fatalf("the sheet answered %d without the modal, the form, one row per finding (41) or the counter", sheet.Code)
	}
	if strings.Contains(body, "<script>alert(1)</script>") || !strings.Contains(body, "&lt;script&gt;") {
		t.Error("the sheet printed a finding's markup unescaped")
	}
	if !strings.Contains(body, "a packet part carries at most 64 KiB; the designer sees it only through its own session") {
		t.Error("the sheet does not warn that the design is larger than a packet part")
	}
	page := do(t, srv, "GET", "/products/"+f.desk+"?tab=board", nil, boCookies()...)
	pb := page.Body.String()
	if !strings.Contains(pb, "blocked: design-blocked") || !strings.Contains(pb, `data-sheet-open="ui/send-back/`+f.desk+`/`+review+`"`) {
		t.Error("the server board does not show the review's block reason and its Revise design button")
	}

	// A triage in the query string is never read; the posted one is.
	path += "?d2=DECLINE&n2=from-the-url"
	w := post(urlenc, form.Encode(), nil, true)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"card"`) {
		t.Fatalf("a signed-in send-back answered %d %s; want 200 and the review card", w.Code, w.Body.String())
	}
	f.s.mu.RLock()
	n, last := f.s.sendBacksLocked(design)
	f.s.mu.RUnlock()
	if n != 1 || last.Payload.Actor != testEmail || len(last.Payload.Triage) != 1 || last.Payload.Triage[0].Note != "the order is stated" {
		t.Errorf("the send-back event is %+v; its actor must be the session's account and its triage the posted one", last.Payload)
	}
}
