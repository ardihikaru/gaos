package main

import (
	"context"
	"reflect"
	"testing"
)

// The fold tests. TC-050 and TC-051 in the QA register are the BRD §4.1
// acceptance test these implement, and they are the reason M0.3 was worth doing
// before anything was built on top of it: a divergence here proves the event
// model is incomplete, and everything downstream of an incomplete event model is
// built on a history that cannot be reconstructed.

// runAWorkflow drives a store through a real workflow: enough ticks that every
// branch in sim.go has fired -- dispatch, block, resume, review, retry,
// escalate, meet, approve, complete -- and the stream has something to say.
//
// It answers the gates as it goes, and it has to. A gated task holds until a
// human releases it and does not time out, which is the entire point of a gate;
// leave them alone and every desk ends up waiting on one and the floor stops
// producing events forever. An unattended simulation is not a workflow, it is a
// queue nobody is working.
func runAWorkflow(t *testing.T, seed uint64, wantEvents int) *MemStore {
	t.Helper()
	ctx := context.Background()
	s := NewMemStore(seed)

	for tick := 0; len(s.events) < wantEvents; tick++ {
		if tick > wantEvents*200 {
			t.Fatalf("%d ticks produced only %d of the %d events wanted; the floor has stalled",
				tick, len(s.events), wantEvents)
		}
		s.Tick()
		f, _ := s.Snapshot(ctx)
		for i, d := range f.Desks {
			if d.ApprovalRequired {
				if _, err := s.ApproveTask(ctx, i); err != nil {
					t.Fatalf("approve desk %d: %v", i, err)
				}
			}
		}
	}
	return s
}

// TestFoldFromGenesisMatchesTheLiveFloor is the determinism half, T-135, and the
// one that decides whether BR-02 actually holds: if a state change emitted no
// event, the fold cannot know about it and the two floors part company.
//
// It folds at the last event's seq rather than "now" on purpose. state(t) is
// defined over events with ts <= t, so the only moment a fold can be compared
// against the live floor is a moment an event marks. Ticking to a quiet point
// and comparing there is what makes that precise: after Tick has produced its
// last event, the live floor and the fold describe the same instant.
func TestFoldFromGenesisMatchesTheLiveFloor(t *testing.T) {
	s := runAWorkflow(t, 7, 120)

	last := s.events[len(s.events)-1].Seq
	folded := fold(emptyFloor(), s.events, last)

	// Everything the last event left behind. Progress and cost accrue on ticks
	// that emit nothing, so the comparison is pinned to the event boundary by
	// folding the live desks the same way -- see foldTo, which is what Replay
	// uses and therefore what the operator actually sees.
	atLast := s.foldTo(last)
	if !reflect.DeepEqual(folded.Desks, atLast.Desks) {
		t.Fatalf("fold from genesis and fold through the ladder disagree at seq %d", last)
	}

	// The desks the fold produces must be the desks the store has, for every
	// field an event is responsible for. State, gate, score and counters are all
	// event-driven; progress and cost move on bare ticks as well, which is the
	// simulator standing in for a runner and is M4's to fix.
	for i, live := range s.desks {
		got := folded.Desks[i]
		if got.ID != live.ID {
			t.Fatalf("desk %d folded as %s, live is %s", i, got.ID, live.ID)
		}
		if got.State != live.State {
			t.Errorf("desk %s folded to state %q, live is %q", live.ID, got.State, live.State)
		}
		if got.ApprovalRequired != live.ApprovalRequired {
			t.Errorf("desk %s folded to gate=%v, live is %v", live.ID, got.ApprovalRequired, live.ApprovalRequired)
		}
		if got.Retry != live.Retry || got.Esc != live.Esc {
			t.Errorf("desk %s folded to retry=%d esc=%d, live is retry=%d esc=%d",
				live.ID, got.Retry, got.Esc, live.Retry, live.Esc)
		}
		if got.Scored != live.Scored || got.Score != live.Score {
			t.Errorf("desk %s folded to scored=%v/%d, live is %v/%d",
				live.ID, got.Scored, got.Score, live.Scored, live.Score)
		}
		if got.StageIdx != live.StageIdx || got.TaskSeq != live.TaskSeq {
			t.Errorf("desk %s folded to stage=%d seq=%d, live is stage=%d seq=%d",
				live.ID, got.StageIdx, got.TaskSeq, live.StageIdx, live.TaskSeq)
		}
		if got.Task != live.Task {
			t.Errorf("desk %s folded to task %q, live is %q", live.ID, got.Task, live.Task)
		}
		if !reflect.DeepEqual(got.Agents, live.Agents) {
			t.Errorf("desk %s folded to agents %v, live is %v", live.ID, got.Agents, live.Agents)
		}
		if (got.Meeting == nil) != (live.Meeting == nil) {
			t.Errorf("desk %s folded to meeting=%v, live is %v", live.ID, got.Meeting != nil, live.Meeting != nil)
		}
	}
}

// TestFoldFromGenesisEqualsFoldFromNearestSnapshot is TC-050, the positive half
// of the BRD §4.1 fold test, and T-012.
//
// The ladder is built at snapshotEvery, so the run has to be long enough to
// cross it -- a test that never reaches a snapshot would compare a fold from
// genesis against a fold from genesis and pass without proving anything.
func TestFoldFromGenesisEqualsFoldFromNearestSnapshot(t *testing.T) {
	s := runAWorkflow(t, 11, snapshotEvery*2+1)
	if len(s.snaps) < 2 {
		t.Fatalf("%d events produced %d snapshot rungs; the ladder is not being built",
			len(s.events), len(s.snaps))
	}

	// Every rung, plus a handful of moments between them. Between is where a
	// broken ladder hides: a snapshot that is only ever asked for its own seq
	// is never asked to be folded forward from.
	var seqs []int
	for _, snap := range s.snaps {
		seqs = append(seqs, snap.UptoSeq, snap.UptoSeq+1, snap.UptoSeq+37)
	}
	seqs = append(seqs, 1, snapshotEvery/2, s.events[len(s.events)-1].Seq)

	for _, seq := range seqs {
		if seq < 1 || seq > s.events[len(s.events)-1].Seq {
			continue
		}
		genesis := fold(emptyFloor(), s.events, seq)
		laddered := s.foldTo(seq)
		if !reflect.DeepEqual(genesis, laddered) {
			t.Fatalf("at seq %d, fold-from-genesis and fold-from-nearest-snapshot "+
				"produced different floors; divergence proves the event model is incomplete", seq)
		}
	}
}

// TestFoldNeedsNoSnapshotAtAll is T-100 and BR-21: "snapshots are an
// optimisation and never a source of truth."
//
// Throw the entire ladder away and demand the same answer. If this ever needs
// the ladder to pass, the ladder has quietly become the record, and the event
// stream -- the thing the whole audit story rests on -- has stopped being it.
func TestFoldNeedsNoSnapshotAtAll(t *testing.T) {
	s := runAWorkflow(t, 3, snapshotEvery+50)
	seq := s.events[len(s.events)-1].Seq

	withLadder := s.foldTo(seq)
	s.snaps = nil
	withoutLadder := s.foldTo(seq)

	if !reflect.DeepEqual(withLadder, withoutLadder) {
		t.Fatal("discarding every snapshot changed the folded floor; the snapshots " +
			"have become the source of truth and the event stream has stopped being it")
	}
}

// TestFoldIsTheNegativeCaseToo is TC-051. A fold that diverges must be
// detectable, which means the comparison has to be capable of failing -- a test
// that would pass on any two floors proves nothing about the two it was given.
//
// So this corrupts a stream on purpose and requires the answer to change.
func TestFoldIsTheNegativeCaseToo(t *testing.T) {
	s := runAWorkflow(t, 5, 120)
	seq := s.events[len(s.events)-1].Seq
	good := fold(emptyFloor(), s.events, seq)

	// Drop the LAST event that moved a desk, not the first. Events carry
	// absolute values, so an early one is usually overwritten by a later one on
	// the same desk and dropping it changes nothing -- which is correct
	// behaviour and a useless test. The last one has nothing after it to paper
	// over the hole.
	cut := -1
	for i := len(s.events) - 1; i >= 0; i-- {
		switch s.events[i].Type {
		case TaskDispatched, TaskBlocked, TaskCompleted, ReviewCompleted, ApprovalGranted:
			cut = i
		}
		if cut >= 0 {
			break
		}
	}
	if cut < 0 {
		t.Skip("this run emitted nothing that moves a desk")
	}
	trimmed := append(append([]Event(nil), s.events[:cut]...), s.events[cut+1:]...)

	bad := fold(emptyFloor(), trimmed, seq)
	if reflect.DeepEqual(good, bad) {
		t.Fatalf("dropping the %s at seq %d did not change the folded floor; "+
			"the comparison cannot fail, so passing it means nothing",
			s.events[cut].Type, s.events[cut].Seq)
	}
}

// TestReducerSkipsUnknownEventTypes is T-098's totality: "an unknown event type
// is skipped, never fatal."
//
// The scenario is not hypothetical. The catalog is closed but it is not frozen
// -- D-037 added three names to it in this very change -- so a stream written by
// a build that knows a name this one does not is exactly what happens during a
// rolling upgrade. The fold has to keep working.
func TestReducerSkipsUnknownEventTypes(t *testing.T) {
	s := runAWorkflow(t, 9, 90)
	seq := s.events[len(s.events)-1].Seq
	want := fold(emptyFloor(), s.events, seq)

	// Interleave names this build has never heard of, including one that looks
	// plausible and one that does not.
	var spiked []Event
	next := seq
	for _, e := range s.events {
		spiked = append(spiked, e)
		next++
		spiked = append(spiked, Event{
			Seq: next, Type: EventType("QuantumFluxDetected"), DeskID: e.DeskID,
			Text: "from a build that does not exist yet", Kind: KindInfo, T: e.T,
			Payload: EventPayload{Task: "should never be applied", Score: 999, Retry: 42},
		})
	}

	got := fold(emptyFloor(), spiked, next)
	if !reflect.DeepEqual(got.Desks, want.Desks) {
		t.Fatal("an unknown event type changed the folded floor; reduce must skip " +
			"what it does not recognise, not guess at it")
	}
}

// The reducer is pure: same inputs, same output, no clock, no random. Folding
// the same stream twice has to give the same answer, and folding it in a fresh
// process has to give the answer the first one did.
func TestReducerIsPure(t *testing.T) {
	s := runAWorkflow(t, 13, 120)
	seq := s.events[len(s.events)-1].Seq

	first := fold(emptyFloor(), s.events, seq)
	second := fold(emptyFloor(), s.events, seq)
	if !reflect.DeepEqual(first, second) {
		t.Fatal("folding the same stream twice gave two answers")
	}

	// And it does not write through to its input.
	third := fold(first, nil, seq)
	third.Desks[0].Name = "MUTATED BY THE CALLER"
	if first.Desks[0].Name == "MUTATED BY THE CALLER" {
		t.Fatal("the fold handed back a floor aliasing its own base state")
	}
}

// Order-stable: ties broken by (T, seq) so replays are identical. Feed the same
// events in a scrambled order and require the same floor.
func TestFoldIsOrderStable(t *testing.T) {
	s := runAWorkflow(t, 21, 120)
	seq := s.events[len(s.events)-1].Seq
	want := fold(emptyFloor(), s.events, seq)

	scrambled := make([]Event, len(s.events))
	copy(scrambled, s.events)
	for i := range scrambled {
		j := (i * 7) % len(scrambled)
		scrambled[i], scrambled[j] = scrambled[j], scrambled[i]
	}

	got := fold(emptyFloor(), scrambled, seq)
	if !reflect.DeepEqual(got, want) {
		t.Fatal("the same events in a different order folded to a different floor; " +
			"§08 requires ties broken by (ts, seq) so a replay is the same replay every time")
	}
}

// Replay through the seam reaches genesis and every moment after it. The old
// implementation could not: the stream was a 160-entry window and everything
// before it was unreachable, which is what T-013 removed.
func TestReplayReachesEveryMomentInTheStream(t *testing.T) {
	ctx := context.Background()
	s := runAWorkflow(t, 4, 200)

	evs, _ := s.Events(ctx)
	for _, e := range evs {
		if _, err := s.Replay(ctx, e.Seq); err != nil {
			t.Fatalf("replay to seq %d (%s): %v", e.Seq, e.Type, err)
		}
	}
	if _, err := s.Replay(ctx, -1); err != nil {
		t.Fatalf("back to live: %v", err)
	}
}
