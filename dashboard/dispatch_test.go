package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The dispatch route. T-126 and T-127 seen from the store, which is where the
// events L-5 requires actually get written.
//
// L-5: "Redactions and refusals are events (ContextRedacted, a refused dispatch
// with its reason) and appear in the timeline like any other state change." An
// event with no emitter appears nowhere, which is why these tests go through
// DispatchTask rather than through Screen.

// idleDesk parks a desk so it can be dispatched, and returns its index.
func idleDesk(t *testing.T, s *MemStore) int {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.desks {
		if s.desks[i].State == StateIdle {
			return i
		}
	}
	// Nothing seeded idle: park the first desk directly. Not through setState,
	// because this is test setup rather than a transition under test.
	s.desks[0].State = StateIdle
	s.desks[0].Agents = nil
	s.desks[0].Meeting = nil
	if s.meetingIdx == 0 {
		s.meetingIdx = -1
	}
	return 0
}

func eventsOfType(evs []Event, want EventType) []Event {
	var out []Event
	for _, e := range evs {
		if e.Type == want {
			out = append(out, e)
		}
	}
	return out
}

// TestDispatchEmitsTaskDispatchedWhenTheSliceClears is the positive half.
func TestDispatchEmitsTaskDispatchedWhenTheSliceClears(t *testing.T) {
	s := NewMemStore(20260807)
	s.egress = EgressPolicy{} // no path restriction: the packet is GAOS's own
	idx := idleDesk(t, s)

	res, err := s.DispatchTask(context.Background(), idx)
	if err != nil {
		t.Fatalf("dispatching a clean desk failed: %v", err)
	}
	if len(eventsOfType(res.NewEvents, TaskDispatched)) != 1 {
		t.Fatalf("a dispatch that cleared the egress pass emitted %v; the stream is the record (BR-04) and a dispatch nothing wrote never happened", typesOf(res.NewEvents))
	}

	d, _, err := s.Desk(context.Background(), idx)
	if err != nil {
		t.Fatal(err)
	}
	if d.State != StateActive {
		t.Fatalf("the desk is %q after a cleared dispatch, want active", d.State)
	}

	// The digest travels on the event, so a replay can prove which slice this
	// attempt saw. 04-execution.sql declares ContextSlice.digest for exactly it.
	ev := eventsOfType(res.NewEvents, TaskDispatched)[0]
	if ev.Payload.Digest == "" || ev.Payload.Bytes == 0 {
		t.Fatalf("TaskDispatched carries digest %q and %d bytes; an attempt that does not name its input cannot be audited against one", ev.Payload.Digest, ev.Payload.Bytes)
	}
}

// TestARefusedDispatchIsAnEventNotAnError is the surprising line in
// DispatchTask, pinned so nobody "fixes" it into an ErrRejected.
func TestARefusedDispatchIsAnEventNotAnError(t *testing.T) {
	s := NewMemStore(20260807)
	// LocalOnly against a non-local target: a refusal that needs no planted
	// credential, so the test is about the plumbing rather than the detector.
	s.egress = EgressPolicy{LocalOnly: true}
	s.target = Target{Provider: "anthropic", Local: false}
	idx := idleDesk(t, s)

	res, err := s.DispatchTask(context.Background(), idx)
	if err != nil {
		t.Fatalf("DispatchTask returned err=%v on a refusal; a refusal is a recorded state change, and returning ErrRejected means the two events L-5 requires are written to the stream and never reach the response that renders them", err)
	}
	if len(eventsOfType(res.NewEvents, DispatchRefused)) != 1 {
		t.Fatalf("a refused dispatch emitted %v; L-5 wants the refusal on the timeline with its reason", typesOf(res.NewEvents))
	}
	if !strings.Contains(res.Toast, "refused") {
		t.Fatalf("the toast reads %q and does not say the dispatch was refused; the operator pressed a button and must be told it did not happen", res.Toast)
	}
}

// TestARefusedDispatchBlocksTheDeskAndSaysWhy is the transition BR-32 forced
// into state.go, asserted from the other side.
func TestARefusedDispatchBlocksTheDeskAndSaysWhy(t *testing.T) {
	s := NewMemStore(20260807)
	s.egress = EgressPolicy{LocalOnly: true}
	s.target = Target{Provider: "anthropic", Local: false}
	idx := idleDesk(t, s)

	before, _ := s.Snapshot(context.Background())
	res, err := s.DispatchTask(context.Background(), idx)
	if err != nil {
		t.Fatal(err)
	}

	d, _, err := s.Desk(context.Background(), idx)
	if err != nil {
		t.Fatal(err)
	}
	if d.State != StateBlocked {
		t.Fatalf("the desk is %q after a refused dispatch, want blocked — a desk left idle says nothing happened, and something did", d.State)
	}
	if d.BlockReason == "" || !strings.Contains(d.BlockReason, "no-egress") {
		t.Fatalf("the refusal reads %q; it has to name what was found or the operator has nowhere to go", d.BlockReason)
	}

	after, _ := s.Snapshot(context.Background())
	if after.Stats.Attention <= before.Stats.Attention {
		t.Fatalf("a refused dispatch did not raise the attention count (%d -> %d); stats() counts blocked desks, and a refusal nobody is told about is indistinguishable from a broken filter", before.Stats.Attention, after.Stats.Attention)
	}

	// The machine-readable half rides on the payload, so the budget guard can
	// reuse this event under BR-35 without anyone parsing English.
	ev := eventsOfType(res.NewEvents, DispatchRefused)[0]
	if ev.Payload.Rule != RuleLocalOnly {
		t.Fatalf("DispatchRefused carries rule %q, want %q; a reducer that had to parse the sentence would break the first time somebody improved the wording", ev.Payload.Rule, RuleLocalOnly)
	}
}

// TestARedactedSliceStillDispatchesAndSaysSoOnTheTimeline is L-5's first half.
func TestARedactedSliceStillDispatchesAndSaysSoOnTheTimeline(t *testing.T) {
	s := NewMemStore(20260807)
	s.egress = EgressPolicy{AllowPaths: []string{"src/**"}}
	idx := idleDesk(t, s)

	// A real workspace with one file inside the allowlist and one outside it.
	// Real bytes on a real disk, because an egress pass over labels would be
	// theatre -- §8.12.1's largest class is the file contents themselves.
	s.mu.Lock()
	s.workspaceRoot = t.TempDir()
	ws := s.workspaceFor(s.desks[idx])
	s.mu.Unlock()

	writeWorkspaceFile(t, ws, "src/keep.go", "package keep\n")
	writeWorkspaceFile(t, ws, "docs/drop.md", "internal notes nobody reviewed\n")

	res, err := s.DispatchTask(context.Background(), idx)
	if err != nil {
		t.Fatalf("a redacted slice refused the dispatch: %v; the allowlist bounds what leaves, it does not abort", err)
	}
	if len(eventsOfType(res.NewEvents, ContextRedacted)) != 1 {
		t.Fatalf("a slice with a part cut dispatched without a ContextRedacted: %v; L-5 exists because an invisible filter is indistinguishable from a broken one", typesOf(res.NewEvents))
	}
	if len(eventsOfType(res.NewEvents, TaskDispatched)) != 1 {
		t.Fatalf("a redacted slice did not go on to dispatch: %v", typesOf(res.NewEvents))
	}

	ev := eventsOfType(res.NewEvents, ContextRedacted)[0]
	if ev.Payload.Redactions != 1 {
		t.Fatalf("ContextRedacted reports %d redactions, want 1; the count is what tells the operator how much was cut", ev.Payload.Redactions)
	}
}

// TestTheRefusalAndTheRedactionFoldTheSameWayTwice is fold.go's contract
// applied to two brand-new event types -- the likeliest place for it to stop
// being true.
func TestTheRefusalAndTheRedactionFoldTheSameWayTwice(t *testing.T) {
	s := NewMemStore(20260807)
	s.egress = EgressPolicy{LocalOnly: true}
	s.target = Target{Provider: "anthropic", Local: false}
	idx := idleDesk(t, s)

	if _, err := s.DispatchTask(context.Background(), idx); err != nil {
		t.Fatal(err)
	}

	evs, err := s.Events(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	first := fold(emptyFloor(), evs, allEvents)
	second := fold(emptyFloor(), evs, allEvents)

	if len(first.Desks) != len(second.Desks) {
		t.Fatalf("two folds of one stream produced %d and %d desks", len(first.Desks), len(second.Desks))
	}
	for i := range first.Desks {
		if first.Desks[i].State != second.Desks[i].State {
			t.Fatalf("folding the stream twice put desk %s in %q then %q; the reducer is required to be pure and order-stable, and two new event types are the likeliest place that stops being true", first.Desks[i].ID, first.Desks[i].State, second.Desks[i].State)
		}
	}

	// And the fold must agree with the live floor about the refusal.
	live, _, _ := s.Desk(context.Background(), idx)
	for _, d := range first.Desks {
		if d.ID == live.ID && d.State != live.State {
			t.Fatalf("the folded desk is %q and the live one is %q; a refusal the reducer does not reproduce is a refusal that vanishes on restart", d.State, live.State)
		}
	}
}

// TestDispatchIsRefusedInReplay. T-099's read-only handle covers the new
// mutation like its fifteen neighbours.
func TestDispatchIsRefusedInReplay(t *testing.T) {
	s := NewMemStore(20260807)
	ro := readOnly{s}

	if _, err := ro.DispatchTask(context.Background(), 0); err == nil {
		t.Fatal("the read-only handle dispatched a task; the floor is parked in the past, and a dispatch from there would send a slice assembled out of a historical record")
	}
}

// writeWorkspaceFile plants a real file in a real workspace.
func writeWorkspaceFile(t *testing.T, ws, rel, body string) {
	t.Helper()
	full := filepath.Join(ws, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestACredentialInTheWorkspaceRefusesTheDispatch is the end-to-end version of
// the thing R-11 is about: a file nobody reviewed, chosen automatically,
// carrying a key.
func TestACredentialInTheWorkspaceRefusesTheDispatch(t *testing.T) {
	s := NewMemStore(20260807)
	s.egress = EgressPolicy{} // no path restriction: the credential is the point
	idx := idleDesk(t, s)

	s.mu.Lock()
	s.workspaceRoot = t.TempDir()
	ws := s.workspaceFor(s.desks[idx])
	s.mu.Unlock()

	writeWorkspaceFile(t, ws, "src/config.go",
		"package config\n\nconst Key = \"sk-ant-api03-AAAAAAAAAAAAAAAAAAAAAA\"\n")

	res, err := s.DispatchTask(context.Background(), idx)
	if err != nil {
		t.Fatal(err)
	}
	if len(eventsOfType(res.NewEvents, TaskDispatched)) != 0 {
		t.Fatal("a workspace carrying a credential was dispatched; BR-32 says a detection refuses the dispatch, and the safety score in domain E cannot recall a sent slice")
	}
	ref := eventsOfType(res.NewEvents, DispatchRefused)
	if len(ref) != 1 || ref[0].Payload.Rule != RuleSecret {
		t.Fatalf("a credential in the workspace produced %v; want one DispatchRefused under rule %q", typesOf(res.NewEvents), RuleSecret)
	}
	if strings.Contains(ref[0].Text, "sk-ant-api03") {
		t.Fatal("the refusal pill contains the credential; the event stream is append-only and rendered to a browser, so this writes the key permanently into the record that exists to prove it never left")
	}
}

func typesOf(evs []Event) []string {
	out := make([]string, 0, len(evs))
	for _, e := range evs {
		out = append(out, string(e.Type))
	}
	return out
}
