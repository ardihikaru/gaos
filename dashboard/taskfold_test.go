package main

import (
	"context"
	"reflect"
	"testing"
	"time"
)

// T-164: the task fold. Folding the stream must reproduce the board the store
// reports (TC-308), and a stream missing one move must not (TC-316).

// scheduleOne asks the scheduler for one task, scripts the fake's reply for it
// with script, and runs the pass. It answers the task it dispatched, or "".
func scheduleOne(t *testing.T, s *MemStore, f *fakeAIM, ad Adapter, script func(task string, attempt int) string) string {
	t.Helper()
	dec := Schedule(s.schedulerInput(time.Now().Unix()))
	if len(dec.Ready) == 0 {
		for _, b := range dec.Blocked {
			t.Logf("blocked %s: %s %s", b.TaskID, b.Reason, b.Detail)
		}
		return ""
	}
	id := dec.Ready[0].TaskID
	s.mu.RLock()
	n := s.nextAttemptLocked(id)
	s.mu.RUnlock()
	f.mu.Lock()
	f.reply = script(id, n)
	f.mu.Unlock()
	s.RunOnce(context.Background(), ad)
	return id
}

// boardHistory drives one product's board through every actor that moves a
// task: the scheduler, the runner (a turn that reaches review, one that
// fails), the board (approve, retry, a reorder, a cancel), a drift refusal and
// the sync that releases it. Only through the store's own routes -- a direct
// write to a status would be a change no event describes, which is exactly
// what this test exists to rule out.
func boardHistory(t *testing.T) (*MemStore, string) {
	t.Helper()
	t.Setenv("GAOS_RUNNER", "on")
	_, s, desk := plannedProduct(t)
	ctx := context.Background()
	f, ad := gatewayOn(t, s)
	// The seeded desks still seat agents (the desk projection), so the
	// planner's and architect's seats read as busy; free them all, as an idle
	// fleet would be. A desk's state is not a task's status, so nothing the
	// fold reads is written behind its back.
	s.mu.Lock()
	for i := range s.desks {
		s.desks[i].State = StateIdle
	}
	s.mu.Unlock()

	ok := func(task string, n int) string {
		return chatReply("done\n\n"+handoffBlock(task, runIDFor(task, n)), "0.1", "")
	}
	bad := func(string, int) string { return chatReply("no handoff at all", "0.1", "") }

	// 1. A turn that reaches review, approved on the board.
	a := scheduleOne(t, s, f, ad, ok)
	if a == "" {
		t.Fatal("the scheduler found nothing ready in a freshly planned product")
	}
	waitForRun(t, s, runIDFor(a, 1), RunStatusExited)
	if _, err := s.MoveCard(ctx, desk, a, CardMove{Column: ColDone}); err != nil {
		t.Fatalf("approving %s: %v", a, err)
	}

	// 2. A turn with no handoff fails (BR-30), and the board retries it.
	b := scheduleOne(t, s, f, ad, bad)
	if b == "" {
		t.Fatal("nothing became ready after the first task was approved")
	}
	waitForRun(t, s, runIDFor(b, 1), RunStatusExited)
	if _, err := s.MoveCard(ctx, desk, b, CardMove{Column: ColReady}); err != nil {
		t.Fatalf("retrying %s: %v", b, err)
	}

	// 3. A reorder inside the backlog, and a cancel.
	var backlog []string
	for _, tk := range productTasks(s, desk) {
		if tk.Status == StatusPending {
			backlog = append(backlog, tk.ID)
		}
	}
	if len(backlog) < 2 {
		t.Fatalf("the backlog holds %d task(s); this case needs two", len(backlog))
	}
	if _, err := s.MoveCard(ctx, desk, backlog[len(backlog)-1], CardMove{Index: 0}); err != nil {
		t.Fatalf("reordering: %v", err)
	}
	if _, err := s.MoveCard(ctx, desk, backlog[0], CardMove{Column: ColCanceled}); err != nil {
		t.Fatalf("canceling: %v", err)
	}

	// 4. A published Blueprint the workspace does not hold refuses the next
	// dispatch (BR-39) and blocks the task; the sync releases it.
	if _, err := s.SavePolicy(ctx, desk, Policy{Key: "go-style", Kind: PolicyCoding, Title: "Go style", Body: "gofmt"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PublishBlueprint(ctx, desk); err != nil {
		t.Fatal(err)
	}
	s.RunOnce(ctx, ad)
	if _, err := s.SyncBlueprint(ctx, desk); err != nil {
		t.Fatal(err)
	}
	return s, desk
}

func foldedBoard(s *MemStore, desk string, events []Event) (Board, Board) {
	s.mu.RLock()
	m := s.masterPtr(desk)
	s.mu.RUnlock()
	live, _ := s.Board(context.Background(), desk)
	// The card timing (D-081) is read-time decoration derived from the same
	// events, like the QA badge; the fold reproduces the projection under it.
	live.Workflows = nil
	for i := range live.Cards {
		live.Cards[i].Timing = nil
		live.Cards[i].Conversation = nil
	}
	return boardOf(desk, m, foldTasks(events, desk)), live
}

func TestTheTaskFoldReproducesTheBoard(t *testing.T) {
	s, desk := boardHistory(t)
	folded, live := foldedBoard(s, desk, s.rawEvents())

	cols := map[string]bool{}
	for _, c := range live.Cards {
		cols[c.Column] = true
	}
	for _, want := range []string{ColBacklog, ColReady, ColDone, ColCanceled} {
		if !cols[want] {
			t.Fatalf("the live board has cards in %v and none in %s; the history did not reach every actor", cols, want)
		}
	}
	rules := map[string]bool{}
	for _, e := range s.rawEvents() {
		if e.Type == TaskMoved && e.DeskID == desk {
			rules[e.Payload.Rule] = true
		}
	}
	for _, want := range []string{MoveSchedule, MoveStart, MoveExit, MoveApprove, MoveRetry, MoveCancel, MoveRefused, MoveDriftRelease} {
		if !rules[want] {
			t.Fatalf("the history wrote TaskMoved with rules %v and none with %q; every actor that moves a task must name it", rules, want)
		}
	}
	if !reflect.DeepEqual(folded, live) {
		t.Fatalf("folding the events gave the board %+v; the store reports %+v -- a status change wrote no TaskMoved (T-164, BR-04)", folded.Cards, live.Cards)
	}
}

func TestATaskFoldMissingAMoveDisagreesWithTheBoard(t *testing.T) {
	s, desk := boardHistory(t)
	events := s.rawEvents()
	last := -1
	for i, e := range events {
		if e.Type == TaskMoved && e.DeskID == desk {
			last = i
		}
	}
	if last < 0 {
		t.Fatal("the history wrote no TaskMoved at all")
	}
	dropped := append(append([]Event(nil), events[:last]...), events[last+1:]...)
	folded, live := foldedBoard(s, desk, dropped)
	if reflect.DeepEqual(folded, live) {
		t.Fatalf("a fold missing the TaskMoved %q still reproduced the board; the fold is not reading the stream (TC-316)", events[last].Text)
	}
}
