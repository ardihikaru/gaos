package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// v0.60.1 (docs/designs/gaos-external-tasks-and-pr-bundles.md §3): a merge finishes its
// review card, the moot moves survive the cap, and the reader acknowledges an event
// only once it is committed.

// walkTo moves a card through the state machine to status, as the runner would.
func walkTo(t *testing.T, st *MemStore, id, status string) {
	t.Helper()
	st.mu.RLock()
	cur := st.taskByIDLocked(id).Status
	st.mu.RUnlock()
	for _, step := range statusPath(cur, status) {
		st.moveTaskByID(id, step, "test")
	}
	st.mu.RLock()
	defer st.mu.RUnlock()
	if got := st.taskByIDLocked(id).Status; got != status {
		t.Fatalf("%s is %s, want %s", id, got, status)
	}
}

func cardStatus(st *MemStore, id string) string {
	st.mu.RLock()
	defer st.mu.RUnlock()
	return st.taskByIDLocked(id).Status
}

func reviewerOf(t *testing.T, st *MemStore, n int) string {
	t.Helper()
	ev := prEvent(prTypeOpened, "laundry-api", n)
	st.mu.RLock()
	defer st.mu.RUnlock()
	c := st.taskByDedupKeyLocked(ev.Key(prJobReviewer))
	if c == nil {
		t.Fatalf("no reviewer card for #%d", n)
	}
	return c.ID
}

func TestMergedPRFinishesItsReviewCard(t *testing.T) {
	ctx := context.Background()
	st := NewMemStore(1)
	for _, n := range []int{1, 2, 3} {
		if _, _, _, err := st.PlanPullRequestCards(ctx, "p-1", prEvent(prTypeOpened, "laundry-api", n)); err != nil {
			t.Fatal(err)
		}
	}
	inReview, running, held := reviewerOf(t, st, 1), reviewerOf(t, st, 2), reviewerOf(t, st, 3)
	walkTo(t, st, inReview, StatusReview)
	walkTo(t, st, running, StatusRunning)
	walkTo(t, st, held, StatusReview)
	if _, err := st.HoldReview(ctx, "p-1", held); err != nil {
		t.Fatal(err)
	}
	for _, n := range []int{1, 2, 3} {
		if _, _, _, err := st.PlanPullRequestCards(ctx, "p-1", prEvent(prTypeMerged, "laundry-api", n)); err != nil {
			t.Fatal(err)
		}
	}
	if got := cardStatus(st, inReview); got != StatusDone {
		t.Errorf("a review in REVIEW whose pull request merged is %s, want DONE", got)
	}
	if got := cardStatus(st, running); got != StatusRunning {
		t.Errorf("a RUNNING review must be left to its run, got %s", got)
	}
	if got := cardStatus(st, held); got != StatusReview {
		t.Errorf("a secret-held review must never be finished by a merge, got %s", got)
	}
	// The e2e card of #1 had not started: still canceled, as before.
	e2e := prEvent(prTypeOpened, "laundry-api", 1).Key(prJobE2EBuild)
	st.mu.RLock()
	c := st.taskByDedupKeyLocked(e2e)
	st.mu.RUnlock()
	if c == nil || c.Status != StatusCanceled {
		t.Errorf("a PENDING e2e card must still be canceled by the merge, got %+v", c)
	}
}

func TestMergeMovesSurviveTheCap(t *testing.T) {
	t.Setenv("GAOS_PR_MAX_OPEN", "10")
	ctx := context.Background()
	st := NewMemStore(1)
	for _, n := range []int{4, 5} {
		if _, _, _, err := st.PlanPullRequestCards(ctx, "p-1", prEvent(prTypeOpened, "laundry-api", n)); err != nil {
			t.Fatal(err)
		}
	}
	rv := reviewerOf(t, st, 4)
	walkTo(t, st, rv, StatusReview)
	// #5's two cards stay open: a cap of 2 refuses the merge's unit-test card
	// even after #4's own cards are finished and canceled.
	t.Setenv("GAOS_PR_MAX_OPEN", "2")
	planned, _, res, err := st.PlanPullRequestCards(ctx, "p-1", prEvent(prTypeMerged, "laundry-api", 4))
	if err != nil {
		t.Fatalf("a capped merge with moves to commit answered %v, want no error", err)
	}
	if !res.Capped || len(planned) != 0 {
		t.Errorf("a capped merge must report Capped and plan nothing, got capped=%v planned=%v", res.Capped, planned)
	}
	if len(res.NewEvents) == 0 {
		t.Error("the moot moves must ride the Result so the store commits them")
	}
	if got := cardStatus(st, rv); got != StatusDone {
		t.Errorf("the review card is %s after a capped merge, want DONE", got)
	}
	// With nothing to move, the cap is still the old refusal.
	if _, _, _, err := st.PlanPullRequestCards(ctx, "p-1", prEvent(prTypeMerged, "laundry-api", 99)); !errors.Is(err, ErrPRSkipped) {
		t.Errorf("a capped merge with nothing to move answered %v, want ErrPRSkipped", err)
	}
}

func TestFinishMergedReviewsSweep(t *testing.T) {
	ctx := context.Background()
	st := NewMemStore(1)
	if _, _, _, err := st.PlanPullRequestCards(ctx, "p-1", prEvent(prTypeOpened, "laundry-api", 8)); err != nil {
		t.Fatal(err)
	}
	rv := reviewerOf(t, st, 8)
	walkTo(t, st, rv, StatusRunning)
	// The merge arrives while the review is RUNNING; then the poller marks the row merged.
	if _, _, _, err := st.PlanPullRequestCards(ctx, "p-1", prEvent(prTypeMerged, "laundry-api", 8)); err != nil {
		t.Fatal(err)
	}
	key := pullRequestKey("p-1", "loop/laundry-api", 8)
	if _, err := st.AdvancePullRequest(ctx, "p-1", key, PRFields{State: PRStateMerged, CheckedAt: time.Now().Unix()}); err != nil {
		t.Fatal(err)
	}
	walkTo(t, st, rv, StatusReview) // the run ends after the row went final
	p := newFixPRPoller(st, nil)
	p.logf = func(string, ...any) {}
	p.Pass(ctx)
	if got := cardStatus(st, rv); got != StatusDone {
		t.Errorf("a review that reached REVIEW after its pull request merged is %s after a poll, want DONE", got)
	}
	// A secret hold is never swept.
	if _, _, _, err := st.PlanPullRequestCards(ctx, "p-1", prEvent(prTypeOpened, "laundry-api", 9)); err != nil {
		t.Fatal(err)
	}
	held := reviewerOf(t, st, 9)
	walkTo(t, st, held, StatusReview)
	if _, err := st.HoldReview(ctx, "p-1", held); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AdvancePullRequest(ctx, "p-1", pullRequestKey("p-1", "loop/laundry-api", 9), PRFields{State: PRStateMerged, CheckedAt: time.Now().Unix()}); err != nil {
		t.Fatal(err)
	}
	p.Pass(ctx)
	if got := cardStatus(st, held); got != StatusReview {
		t.Errorf("the sweep finished a secret-held review (%s)", got)
	}
}

// drainOnce runs Drain over what is queued, then stops it.
func drainOnce(t *testing.T, c *btConsumer, handle func(context.Context, PREvent) flowOutcome) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Drain(ctx, handle); close(done) }()
	deadline := time.Now().Add(3 * time.Second)
	for len(c.out) > 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond)
	cancel()
	<-done
}

func TestConsumerCursorSingleWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cursor")
	c := newBTConsumer(btConsumerConfig{enabled: true, cursor: path}, NewMemStore(1))
	c.logf = func(string, ...any) {}
	// Seq 10 is an event still queued; seq 11 is a frame the reader filtered.
	c.hand(PREvent{Type: prTypeOpened, Seq: 10, Org: "loop", Repo: "laundry-api", PRNumber: 1})
	c.marker(11)
	var seen []int64
	drainOnce(t, c, func(_ context.Context, ev PREvent) flowOutcome {
		seen = append(seen, ev.Seq)
		if got := c.readCursor(); got >= 10 {
			t.Errorf("the cursor reached %d before seq 10 was committed", got)
		}
		return flowDone
	})
	if got := c.readCursor(); got != 11 {
		t.Errorf("after the event and the marker the cursor is %d, want 11", got)
	}
	// Monotonic: an older seq never moves it back.
	c.writeCursor(5)
	if got := c.readCursor(); got != 11 {
		t.Errorf("the cursor moved back to %d", got)
	}
	// A retry discards what the old session queued behind the failed event.
	c.hand(PREvent{Type: prTypeOpened, Seq: 12, Org: "loop", Repo: "laundry-api", PRNumber: 2})
	c.hand(PREvent{Type: prTypeOpened, Seq: 13, Org: "loop", Repo: "laundry-api", PRNumber: 3})
	saved := btRetryWaits
	btRetryWaits = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond, time.Millisecond, time.Millisecond}
	defer func() { btRetryWaits = saved }()
	seen = nil
	drainOnce(t, c, func(_ context.Context, ev PREvent) flowOutcome {
		seen = append(seen, ev.Seq)
		return flowRetry
	})
	if len(seen) != 1 || seen[0] != 12 {
		t.Errorf("after a retry Drain handled %v, want only seq 12 (13 belongs to the ended session)", seen)
	}
	if got := c.readCursor(); got != 11 {
		t.Errorf("a retried event moved the cursor to %d", got)
	}
	// A full channel restarts the session rather than dropping the frame.
	small := newBTConsumer(btConsumerConfig{enabled: true, cursor: filepath.Join(t.TempDir(), "c2")}, NewMemStore(1))
	small.logf = func(string, ...any) {}
	small.out = make(chan PREvent, 1)
	small.hand(PREvent{Type: prTypeOpened, Seq: 1})
	gen := small.gen
	small.hand(PREvent{Type: prTypeOpened, Seq: 2})
	if small.gen == gen {
		t.Error("a full channel must end the session (a new generation), not drop the frame silently")
	}
	if small.discardBelow != 0 {
		t.Error("a full channel must not discard what is already queued")
	}
}

func TestFlowOutcomeClassesAndRetryBound(t *testing.T) {
	ctx := context.Background()
	st := NewMemStore(1)
	cases := []struct {
		err  error
		want flowOutcome
	}{
		{errors.New("dial tcp: connection refused"), flowRetry},
		{&ghError{Status: 502}, flowRetry},
		{&ghError{Status: 429}, flowRetry},
		{&ghError{Status: 403}, flowDone},
		{&ghError{Status: 404}, flowDone},
		{&ghError{Status: 422}, flowDone},
	}
	for _, c := range cases {
		flow := &prFlow{st: st, gh: fakePRSource{err: c.err}, logf: func(string, ...any) {}}
		if got := flow.handle(ctx, prEvent(prTypeOpened, "laundry-api", 30)); got != c.want {
			t.Errorf("a lookup failing with %v answered %v, want %v", c.err, got, c.want)
		}
	}
	// A failed read of the products is a retry.
	bad := &prFlow{st: failingMasters{st}, logf: func(string, ...any) {}}
	if got := bad.handle(ctx, prEvent(prTypeOpened, "laundry-api", 31)); got != flowRetry {
		t.Errorf("a failed products read answered %v, want flowRetry", got)
	}
	// The bound: five retries, then the event is skipped and the cursor passes it.
	path := filepath.Join(t.TempDir(), "cursor")
	c := newBTConsumer(btConsumerConfig{enabled: true, cursor: path}, st)
	var logged []string
	c.logf = func(f string, a ...any) { logged = append(logged, f) }
	saved := btRetryWaits
	btRetryWaits = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond, time.Millisecond, time.Millisecond}
	defer func() { btRetryWaits = saved }()
	calls := 0
	for i := 0; i <= len(btRetryWaits); i++ {
		c.hand(PREvent{Type: prTypeOpened, Seq: 40, Org: "loop", Repo: "laundry-api", PRNumber: 4})
		drainOnce(t, c, func(context.Context, PREvent) flowOutcome { calls++; return flowRetry })
	}
	if calls != len(btRetryWaits)+1 {
		t.Errorf("the event was tried %d times, want %d", calls, len(btRetryWaits)+1)
	}
	if got := c.readCursor(); got != 40 {
		t.Errorf("after the retry bound the cursor is %d, want 40 (skipped)", got)
	}
	if !strings.Contains(strings.Join(logged, "\n"), "gave up seq") {
		t.Error("giving up must be logged as `brain town reader: gave up seq <n>`")
	}
	if row := c.Status(); !strings.Contains(row.Cursor, "gave up 40") {
		t.Errorf("the Integrations row must show the skipped seq, got %q", row.Cursor)
	}
	// The backoff count is kept across sessions: it lives on the reader, not the session.
	if c.retries[40] != 0 {
		t.Error("a skipped event must leave no retry count behind")
	}
}

// failingMasters is a store whose product list cannot be read.
type failingMasters struct{ Store }

func (failingMasters) Masters(context.Context) ([]Master, error) {
	return nil, errors.New("database unavailable")
}

// TestArcadeMergeMovesPersist: a capped merge's cancel and finish reach the graph in
// the merge's transaction. Before v0.60.1 the cap returned an error after the moves
// were applied in memory, and ArcadeStore.mutate persisted nothing.
func TestArcadeMergeMovesPersist(t *testing.T) {
	t.Setenv("GAOS_PR_MAX_OPEN", "10")
	g := &fakeGraph{}
	s, ms, _ := arcadeOver(t, g.handler)
	ctx := context.Background()
	ms.mu.Lock()
	m := ms.masterPtr("p-1")
	if !productOwns(m.Repos, "loop", "laundry-api") {
		m.Repos = append(m.Repos, "loop/laundry-api")
	}
	ms.mu.Unlock()
	for _, n := range []int{6, 7} {
		if _, _, _, err := s.PlanPullRequestCards(ctx, "p-1", prEvent(prTypeOpened, "laundry-api", n)); err != nil {
			t.Fatal(err)
		}
	}
	rv := reviewerOf(t, ms, 6)
	walkTo(t, ms, rv, StatusReview)
	g.take()
	t.Setenv("GAOS_PR_MAX_OPEN", "2")
	_, _, res, err := s.PlanPullRequestCards(ctx, "p-1", prEvent(prTypeMerged, "laundry-api", 6))
	if err != nil || !res.Capped {
		t.Fatalf("the capped merge answered capped=%v err=%v", res.Capped, err)
	}
	stmts := g.take()
	if n := countPrefix(stmts, "INSERT INTO Event"); n < 2 {
		t.Errorf("a capped merge wrote %d events, want the finish (two hops at least) and the cancel", n)
	}
}
