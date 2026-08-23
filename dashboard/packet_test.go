package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// M4.5: the packet on disk (T-061), the contract in it (T-062), the signature
// when the document does not come back (T-063), and the document stored for the
// next agent (T-064).

// TestTheContractsOwnExampleParses is the one case in this file that would have
// caught the whole feature being useless.
//
// The contract tells a model exactly what to write and handoff.go decides
// whether it is readable. Those are two files, written at different times, and
// nothing but this test makes them agree -- an example the parser rejects is a
// contract that punishes the models which follow it most carefully, and it would
// look completely fine in review.
func TestTheContractsOwnExampleParses(t *testing.T) {
	h, err := ParseHandoff([]byte(handoffExample("t-9", "t-9-a1", "backend")))
	if err != nil {
		t.Fatalf("the contract's own example does not parse: %v; a model that copies it "+
			"faithfully fails the attempt, and the contract is the only instruction it got", err)
	}
	if h.Task != "t-9" || h.Run != "t-9-a1" || h.Role != "backend" {
		t.Errorf("the example parsed to task %q run %q role %q; the ids in the contract are the "+
			"ones GAOS matches the document against", h.Task, h.Run, h.Role)
	}
	if h.Status != HandoffComplete || len(h.Criteria) == 0 {
		t.Errorf("the example parsed to status %q with %d criteria", h.Status, len(h.Criteria))
	}
}

// TestTheContractNamesTheRunTheDocumentMustCarry is T-062's substance. A
// contract that described the fields without filling them in leaves the model
// guessing at an id, and it will eventually guess a different spelling.
func TestTheContractNamesTheRunTheDocumentMustCarry(t *testing.T) {
	c := handoffContract("t-42", "t-42-a2", "qa")
	for _, want := range []string{HandoffPath, "t-42", "t-42-a2", "qa", SigHandoffMissing} {
		if !strings.Contains(c, want) {
			t.Errorf("the contract never mentions %q; a model cannot satisfy a requirement "+
				"nobody stated", want)
		}
	}
}

// TestAFrozenPacketCarriesTheContract checks the contract is in the PACKET
// rather than bolted on at spawn time. In the packet means inside the frozen
// slice and inside the digest, so a replay can prove what the run was asked for
// as well as what it was given.
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
		t.Fatal("the frozen packet has no contract part; every real invocation would do the " +
			"work, exit 0 and fail with handoff-missing, because nothing ever told it what to write")
	}
	if !strings.Contains(contract.Body, runIDFor(taskIDOf(s.desks[idx]), 1)) {
		t.Errorf("the contract does not name the run this attempt will be recorded as")
	}
	if !strings.Contains(instructionOf(pkt), HandoffPath) {
		t.Error("the instruction handed to the tool never mentions the handoff path; the " +
			"contract is in the packet and not in what the packet is turned into")
	}
}

// TestMaterialiseWritesThePacketAndDeclaresTheHandoff is T-061 and D-8: the
// packet is files in the leased workspace, not a prompt string.
func TestMaterialiseWritesThePacketAndDeclaresTheHandoff(t *testing.T) {
	ws := t.TempDir()
	pkt := Packet{
		TaskID: "t-1", DeskID: "d-1", Workspace: ws,
		Parts: []PacketPart{
			{Path: ".gaos/packet/task.md", Kind: KindTask, Body: "# t-1\nDo the thing.\n"},
			{Path: ".gaos/packet/rules.md", Kind: KindRules, Body: "- round half-up\n"},
			{Path: ".gaos/packet/handoff.md", Kind: KindContract, Body: handoffContract("t-1", "t-1-a1", "backend")},
		},
	}
	c, _, err := Screen(EgressPolicy{}, Target{Provider: "anthropic"}, pkt)
	if err != nil {
		t.Fatal(err)
	}

	written, err := MaterialisePacket(c)
	if err != nil {
		t.Fatalf("materialise: %v", err)
	}
	if len(written) != 4 {
		t.Fatalf("materialise wrote %v, want the three parts and the declared handoff", written)
	}
	for _, rel := range written {
		if _, err := os.Stat(filepath.Join(ws, rel)); err != nil {
			t.Errorf("%s is not on disk: %v; cmd.Dir points at this directory and the tool "+
				"has nothing to read", rel, err)
		}
	}
	// Declared and EMPTY. A model that finds the previous attempt's document
	// there and leaves it alone would have its predecessor's work scored as its
	// own.
	body, err := os.ReadFile(filepath.Join(ws, HandoffPath))
	if err != nil || len(body) != 0 {
		t.Errorf("the declared handoff is %d bytes (err %v), want an empty file", len(body), err)
	}
}

// TestMaterialiseNeverWritesARedactedPart is the security half of T-061. The
// workspace is the one directory the tool is pointed at and free to read;
// writing a part the egress pass cut would put it there anyway, which is a
// longer way round to the leak BR-32 refuses at the front door.
func TestMaterialiseNeverWritesARedactedPart(t *testing.T) {
	ws := t.TempDir()
	pkt := Packet{
		TaskID: "t-1", DeskID: "d-1", Workspace: ws,
		Parts: []PacketPart{
			{Path: ".gaos/packet/task.md", Kind: KindTask, Body: "# t-1\n"},
			{Path: "config/.env", Kind: KindRepo, Body: "AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMIK7MDENGbPxRfiCY\n"},
		},
	}
	c, v, err := Screen(EgressPolicy{}, Target{Provider: "anthropic"}, pkt)
	if err != nil {
		t.Fatalf("the packet did not clear: %v", err)
	}
	if len(v.Redactions) == 0 {
		t.Fatal("the env file was not redacted; the rest of this case proves nothing")
	}
	if _, err := MaterialisePacket(c); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(ws, "config/.env")); err == nil {
		t.Error("a redacted part was written into the workspace the tool reads; the egress " +
			"pass refused it at the front door and the packet writer let it in the back")
	}
}

// TestTheWriterRefusesAPathThatLeavesTheWorkspace is the same check Screen
// makes, at the boundary that actually writes. Not redundant: the check that
// matters is the one next to the syscall, and a future caller that assembles a
// packet some other way meets this one whether or not it met the other.
//
// Tested at insideWorkspace rather than through MaterialisePacket, and that is
// deliberate rather than lazy. Driving it from the top would mean forging a
// Cleared holding an escaping part, and TestNothingOutsideTheEgressPassConstructsACleared
// walks this package's syntax tree to forbid exactly that -- a guard worth more
// than the convenience of a prettier test. It caught this one, which is the
// evidence it works.
func TestTheWriterRefusesAPathThatLeavesTheWorkspace(t *testing.T) {
	ws := t.TempDir()
	for _, bad := range []string{"../escaped.md", "../../etc/passwd", "/etc/passwd", ".."} {
		if got, err := insideWorkspace(ws, bad); err == nil {
			t.Errorf("path %q resolved to %q instead of being refused; the workspace is a "+
				"lease and anything outside it belongs to somebody else", bad, got)
		}
	}
	good, err := insideWorkspace(ws, ".gaos/packet/task.md")
	if err != nil {
		t.Fatalf("a legitimate packet path was refused: %v", err)
	}
	if !strings.HasPrefix(good, ws+string(filepath.Separator)) {
		t.Errorf("a legitimate path resolved to %q, outside %q", good, ws)
	}
}

// TestARunWithNoHandoffFailsWithTheSignature is T-063 through the whole
// supervisor: the fake tool is told to write nothing, and the attempt has to
// come back failed with the one signature BR-30 names.
func TestARunWithNoHandoffFailsWithTheSignature(t *testing.T) {
	s := NewMemStore(3)
	a, ws := fakeCLI(t, "nohandoff", "0")

	att, err := s.superviseRun(t.Context(), a, s.desks[0].ID, clearedFor(t, ws), 1)
	if err != nil {
		t.Fatalf("supervise: %v", err)
	}
	if att.Failure != SigHandoffMissing {
		t.Errorf("a run that wrote no handoff came back with failure %q, want %q; BR-30 makes "+
			"the document mandatory and the exit code says only that the process ran",
			att.Failure, SigHandoffMissing)
	}
	if att.Outcome() != "failed" {
		t.Errorf("the outcome is %q; a run with no document has not succeeded, whatever it did",
			att.Outcome())
	}
	runs, _ := s.Runs(t.Context(), "t-1")
	if len(runs) != 1 || runs[0].FailureSignature != SigHandoffMissing {
		t.Errorf("the attempt record carries signature %q; BR-16 routes a retry on whether the "+
			"signature CHANGED, and a signature nobody recorded cannot change",
			runs[0].FailureSignature)
	}
}

// TestTheHandoffIsStoredForTheNextAgent is T-064 and D-10: the document reaches
// the next agent through its own frozen slice, never by being left in a
// workspace that is about to be re-leased and truncated.
func TestTheHandoffIsStoredForTheNextAgent(t *testing.T) {
	s := NewMemStore(3)
	a, ws := fakeCLI(t, "complete", "0")
	deskID := s.desks[0].ID

	if _, err := s.superviseRun(t.Context(), a, deskID, clearedFor(t, ws), 1); err != nil {
		t.Fatal(err)
	}

	s.mu.RLock()
	stored := len(s.artifacts)
	art, ok := s.latestHandoffFor(deskID, "t-1", 2)
	s.mu.RUnlock()
	if stored != 1 || !ok {
		t.Fatalf("the run produced a handoff and the store holds %d artifact(s); the workspace "+
			"copy is not the record -- the lease is released and the next attempt truncates it",
			stored)
	}
	if !strings.Contains(art.Body, "task:") {
		t.Errorf("the stored document does not look like the one the run wrote: %q", art.Body)
	}

	// And the next packet cites it, which is the half D-10 is actually about.
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
			"to have received is not a handoff, it is a file", art.ID)
	}
}

// TestAnAttemptDoesNotCiteItsOwnHandoff is the negative half. A packet frozen
// twice for one attempt -- which happens, because DispatchTask freezes and the
// supervisor freezes again -- must not start feeding the attempt its own
// output.
func TestAnAttemptDoesNotCiteItsOwnHandoff(t *testing.T) {
	s := NewMemStore(3)
	a, ws := fakeCLI(t, "complete", "0")
	deskID := s.desks[0].ID

	if _, err := s.superviseRun(t.Context(), a, deskID, clearedFor(t, ws), 1); err != nil {
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
