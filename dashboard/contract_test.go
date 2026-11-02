package main

import (
	"net/http"
	"strings"
	"testing"
)

// The reply contract (T-062, re-based on D-057 in M14.7) and the handoff kept
// for the next agent (T-064). The contract tells a model what to write and
// handoff_reply.go decides whether it is readable; nothing but these tests
// makes the two agree.

// TestTheContractsOwnExampleParses: an example the parser rejects is a
// contract that punishes the models which follow it most carefully.
func TestTheContractsOwnExampleParses(t *testing.T) {
	reply := "the work\n\n" + fence + HandoffFence + "\n" + handoffExample("t-9", "t-9-a1", "backend") + "\n" + fence + "\n"
	h, _, err := ExtractHandoff(reply, "t-9", "t-9-a1")
	if err != nil {
		t.Fatalf("the contract's own example does not parse: %v; a model that copies it "+
			"faithfully fails the attempt, and the contract is the only instruction it got", err)
	}
	if h.Task != "t-9" || h.Run != "t-9-a1" || h.Role != "backend" {
		t.Errorf("the example parsed to task %q run %q role %q; the ids in the contract are the "+
			"ones GAOS matches the block against", h.Task, h.Run, h.Role)
	}
	if h.Status != HandoffComplete || len(h.Criteria) == 0 {
		t.Errorf("the example parsed to status %q with %d criteria", h.Status, len(h.Criteria))
	}
}

// TestTheContractNamesTheRunTheDocumentMustCarry is T-062's substance. A
// contract that described the fields without filling them in leaves the model
// guessing at an id, and it will eventually guess a different spelling.
func TestTheContractNamesTheRunTheDocumentMustCarry(t *testing.T) {
	c := gatewayContract("t-42", "t-42-a2", "qa")
	for _, want := range []string{HandoffFence, "t-42", "t-42-a2", "qa", SigHandoffMissing} {
		if !strings.Contains(c, want) {
			t.Errorf("the contract never mentions %q; a model cannot satisfy a requirement "+
				"nobody stated", want)
		}
	}
}

// TestAFrozenPacketCarriesTheContract checks the contract is in the PACKET
// rather than bolted on at submit time: inside the frozen slice and inside the
// digest, so a replay can prove what the turn was asked for as well as what
// it was given.
func TestAFrozenPacketCarriesTheContract(t *testing.T) {
	s := NewMemStore(20260807)
	idx := idleDesk(t, s)

	s.mu.RLock()
	pkt := s.freezePacket(s.desks[idx], 1)
	s.mu.RUnlock()

	var contract *PacketPart
	for i := range pkt.Parts {
		if pkt.Parts[i].Kind == KindContract {
			contract = &pkt.Parts[i]
		}
	}
	if contract == nil {
		t.Fatal("the frozen packet has no contract part; every turn would do the work and fail " +
			"with handoff-missing, because nothing ever told it what to write")
	}
	if !strings.Contains(contract.Body, runIDFor(taskIDOf(s.desks[idx]), 1)) {
		t.Errorf("the contract does not name the run this attempt will be recorded as")
	}
	c, _, err := Screen(EgressPolicy{}, Target{Provider: "anthropic"}, pkt)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(RenderPrompt(c), HandoffFence) {
		t.Error("the prompt sent to the gateway never mentions the handoff block; the contract " +
			"is in the packet and not in what the packet is turned into")
	}
}

// TestTheHandoffIsStoredForTheNextAgent is T-064 and D-10: the block reaches
// the next agent through its own frozen slice, never through a session that
// expires (BR-41).
func TestTheHandoffIsStoredForTheNextAgent(t *testing.T) {
	s := NewMemStore(3)
	f, a := gatewayOn(t, s)
	completeTurn(f, "t-1", 1, "0.1")
	deskID := s.desks[0].ID

	if _, err := s.superviseRun(t.Context(), a, deskID, clearedFor(t, s, deskID, "t-1", 1), 1); err != nil {
		t.Fatal(err)
	}

	s.mu.RLock()
	art, ok := s.latestHandoffFor(deskID, "t-1", 2)
	s.mu.RUnlock()
	if !ok || art.Kind != ArtifactHandoff {
		t.Fatalf("the turn replied with a handoff and the store holds none for the next attempt; " +
			"the session is transport, the artifact is the record")
	}
	if !strings.Contains(art.Body, "task:") {
		t.Errorf("the stored document does not look like the block the turn replied with: %q", art.Body)
	}

	s.mu.RLock()
	next := s.freezePacketFor(s.desks[0], "t-2", 1)
	s.mu.RUnlock()
	var cited bool
	for _, c := range next.Cites {
		if c.NodeType == "Artifact" && c.NodeID == art.ID && c.Role == CiteLesson {
			cited = true
		}
	}
	if !cited {
		t.Errorf("the next packet does not cite %s; a document the next agent cannot be shown "+
			"to have received is not a handoff", art.ID)
	}
}

// TestAnAttemptDoesNotCiteItsOwnHandoff is the negative half. A packet frozen
// twice for one attempt must not start feeding the attempt its own output.
func TestAnAttemptDoesNotCiteItsOwnHandoff(t *testing.T) {
	s := NewMemStore(3)
	f, a := gatewayOn(t, s)
	completeTurn(f, "t-1", 1, "0.1")
	deskID := s.desks[0].ID

	if _, err := s.superviseRun(t.Context(), a, deskID, clearedFor(t, s, deskID, "t-1", 1), 1); err != nil {
		t.Fatal(err)
	}

	s.mu.RLock()
	same := s.freezePacketFor(s.desks[0], "t-1", 1)
	s.mu.RUnlock()
	for _, c := range same.Cites {
		if c.NodeType == "Artifact" {
			t.Errorf("attempt 1 of t-1 was cited artifact %s, which attempt 1 of t-1 produced; "+
				"an attempt reading its own output is a loop with extra steps", c.NodeID)
		}
	}
}

// TestAFailedTurnIsCitedAsAFailureNotAHandoff: a failed turn's error text is
// still its attempt's stored handoff, but the next agent is shown it under
// failure-previous.md, said to be an error first. A clean turn's handoff is
// still handoff-previous.md, unlabelled.
func TestAFailedTurnIsCitedAsAFailureNotAHandoff(t *testing.T) {
	s := NewMemStore(3)
	f, a := gatewayOn(t, s)
	deskID := s.desks[0].ID
	part := func(p Packet, prefix string) (PacketPart, bool) {
		for _, x := range p.Parts {
			if strings.HasPrefix(x.Path, prefix) {
				return x, true
			}
		}
		return PacketPart{}, false
	}

	f.submit = http.StatusGone // the session is gone: a failed turn with an error text
	s.superviseRun(t.Context(), a, deskID, clearedFor(t, s, deskID, "t-1", 1), 1)
	s.mu.RLock()
	next := s.freezePacketFor(s.desks[0], "t-2", 1)
	s.mu.RUnlock()
	got, ok := part(next, ".gaos/packet/context/failure-previous.md")
	_, asHandoff := part(next, ".gaos/packet/context/handoff-previous.md")
	if !ok || asHandoff || !strings.HasPrefix(got.Body, "The previous attempt (run "+runIDFor("t-1", 1)+") failed with `") {
		t.Fatalf("after a failed turn the next packet holds failure-previous.md %v (body %q) and handoff-previous.md %v; want the error labelled as a failure, never shown as a handoff",
			ok, got.Body, asHandoff)
	}

	f.submit = 0
	completeTurn(f, "t-2", 1, "0.1")
	if _, err := s.superviseRun(t.Context(), a, deskID, clearedFor(t, s, deskID, "t-2", 1), 1); err != nil {
		t.Fatal(err)
	}
	s.mu.RLock()
	next = s.freezePacketFor(s.desks[0], "t-3", 1)
	s.mu.RUnlock()
	got, ok = part(next, ".gaos/packet/context/handoff-previous.md")
	if _, failed := part(next, ".gaos/packet/context/failure-previous.md"); !ok || failed || strings.HasPrefix(got.Body, "The previous attempt") {
		t.Fatalf("after a clean turn the next packet holds handoff-previous.md %v and failure-previous.md %v; want the handoff, unlabelled", ok, failed)
	}
}
