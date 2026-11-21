package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// D-080, BR-56: GAOS_RUNNER=manual. The scheduler readies work; only the
// operator's board dispatch spends.

// chatPosts is how many turns reached the fake gateway's /v1/chat.
func chatPosts(f *fakeAIM) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.posts {
		n += c
	}
	return n
}

// idleFleet frees every seat, as an idle floor would (see boardHistory).
func idleFleet(s *MemStore) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.desks {
		s.desks[i].State = StateIdle
	}
}

func scheduleMoves(evs []Event) int {
	n := 0
	for _, e := range evs {
		if e.Type == TaskMoved && e.Payload.Rule == MoveSchedule {
			n++
		}
	}
	return n
}

func liveOrRun(t *testing.T, s *MemStore, desk string) {
	t.Helper()
	for _, tk := range productTasks(s, desk) {
		if tk.Status == StatusDispatched || tk.Status == StatusRunning {
			t.Fatalf("%s is %s under a manual runner; the scheduler must never dispatch", tk.ID, tk.Status)
		}
	}
	s.mu.RLock()
	runs := len(s.runs)
	s.mu.RUnlock()
	if runs != 0 {
		t.Fatalf("%d TaskRun(s) exist under a manual runner; nothing was dispatched, so nothing ran", runs)
	}
}

func TestRunnerModeReadsGAOS_RUNNER(t *testing.T) {
	cases := []struct {
		val, want string
		spend     bool
	}{
		{"", RunnerOff, false}, {"off", RunnerOff, false}, {"maybe", RunnerOff, false}, {"manaul", RunnerOff, false},
		{"on", RunnerOn, true}, {"ON", RunnerOn, true}, {"1", RunnerOn, true}, {"true", RunnerOn, true}, {"yes", RunnerOn, true},
		{"manual", RunnerManual, true}, {"MANUAL", RunnerManual, true}, {" manual ", RunnerManual, true},
	}
	for _, c := range cases {
		t.Setenv("GAOS_RUNNER", c.val)
		if got := runnerMode(); got != c.want {
			t.Errorf("GAOS_RUNNER=%q reads as %q; want %q (anything unrecognised must be off)", c.val, got, c.want)
		}
		if got := runnerMaySpend(); got != c.spend {
			t.Errorf("GAOS_RUNNER=%q: runnerMaySpend() = %v; want %v", c.val, got, c.spend)
		}
	}
}

func TestAManualRunnerReadiesWorkAndNeverDispatchesIt(t *testing.T) {
	t.Setenv("GAOS_RUNNER", "manual")
	_, s, desk := plannedProduct(t)
	idleFleet(s)
	f, ad := gatewayOn(t, s)
	ctx := context.Background()

	// What the scheduler would start, judged the way `on` judges it.
	want := map[string]bool{}
	for _, d := range Schedule(s.schedulerInput(time.Now().Unix())).Ready {
		want[d.TaskID] = true
	}
	if len(want) == 0 {
		t.Fatal("the scheduler found nothing ready in a freshly planned product")
	}

	before := len(s.rawEvents())
	s.RunOnce(ctx, ad)
	if n := scheduleMoves(s.rawEvents()[before:]); n != 1 {
		t.Fatalf("the first manual pass wrote %d schedule move(s); want exactly 1, one per product per pass", n)
	}
	for i := 0; i < 50; i++ {
		before = len(s.rawEvents())
		s.RunOnce(ctx, ad)
		if len(s.rawEvents()) == before {
			break
		}
	}
	before = len(s.rawEvents())
	s.RunOnce(ctx, ad)
	if n := len(s.rawEvents()) - before; n != 0 {
		t.Fatalf("a pass with every eligible card already ready wrote %d event(s); want 0", n)
	}
	liveOrRun(t, s, desk)
	if n := chatPosts(f); n != 0 {
		t.Fatalf("the gateway received %d turn(s) under a manual runner; want 0", n)
	}
	for id := range want {
		if st := taskStatusOf(s, id); st != StatusReady {
			t.Errorf("%s is %s; the scheduler judged it ready, so a manual pass must leave it READY", id, st)
		}
	}
}

func TestTheManualReadyStepMovesOnlyAPendingTask(t *testing.T) {
	_, s, desk := plannedProduct(t)
	tasks := productTasks(s, desk)
	if len(tasks) < 2 {
		t.Fatalf("the plan has %d tasks; this case needs two", len(tasks))
	}
	live, pending := tasks[0].ID, tasks[1].ID
	setStatus(s, live, StatusDispatched)
	setStatus(s, pending, StatusPending)

	// A decision computed before the operator dispatched `live`.
	stale := ScheduleDecision{Ready: []Dispatch{{TaskID: live, DeskID: desk}}}
	before := len(s.rawEvents())
	s.readyPending(stale)
	if st := taskStatusOf(s, live); st != StatusDispatched || len(s.rawEvents()) != before {
		t.Fatalf("a stale decision moved a dispatched task to %s and wrote %d event(s); want it left DISPATCHED and nothing written",
			st, len(s.rawEvents())-before)
	}
	s.readyPending(ScheduleDecision{Ready: []Dispatch{{TaskID: pending, DeskID: desk}}})
	if st := taskStatusOf(s, pending); st != StatusReady {
		t.Fatalf("a pending task in the decision is %s; want READY", st)
	}
}

// readyRoot puts one prerequisite-free task of the product in READY and
// returns it.
func readyRoot(t *testing.T, s *MemStore, desk string, skip ...string) string {
	t.Helper()
	for _, tk := range productTasks(s, desk) {
		if len(tk.Deps) != 0 {
			continue
		}
		taken := false
		for _, x := range skip {
			taken = taken || x == tk.ID
		}
		if !taken {
			setStatus(s, tk.ID, StatusReady)
			return tk.ID
		}
	}
	t.Fatal("the plan holds no free task without a prerequisite")
	return ""
}

func TestAManualRunnerDispatchesTheCardTheOperatorMoves(t *testing.T) {
	t.Setenv("GAOS_RUNNER", "manual")
	_, s, desk := plannedProduct(t)
	f, _ := gatewayOn(t, s)
	id := readyRoot(t, s, desk)
	f.reply = chatReply(handoffBlock(id, runIDFor(id, 1)), "0.1", "")

	r, err := s.MoveCard(context.Background(), desk, id, CardMove{Column: ColWorking})
	if err != nil || len(r.NewEvents) == 0 || r.NewEvents[0].Payload.Rule != MoveDispatch {
		t.Fatalf("ready -> working under a manual runner gave %v %v; want a dispatch", r.NewEvents, err)
	}
	waitForRun(t, s, runIDFor(id, 1), RunStatusExited)
	if n := chatPosts(f); n != 1 {
		t.Fatalf("the gateway received %d turn(s) for one board dispatch; want 1", n)
	}
}

func TestTheBoardDispatchRefusesWithoutPermission(t *testing.T) {
	ctx := context.Background()
	refused := func(t *testing.T, s *MemStore, desk, id, want string) {
		t.Helper()
		before := len(s.rawEvents())
		_, err := s.MoveCard(ctx, desk, id, CardMove{Column: ColWorking})
		if !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), want) || len(s.rawEvents()) != before {
			t.Fatalf("the board dispatch returned %v and wrote %d event(s); want a refusal saying %q and nothing written",
				err, len(s.rawEvents())-before, want)
		}
		if st := taskStatusOf(s, id); st != StatusReady {
			t.Fatalf("a refused dispatch left the card %s; want READY", st)
		}
	}

	t.Run("off", func(t *testing.T) {
		t.Setenv("GAOS_RUNNER", "off")
		_, s, desk := plannedProduct(t)
		f, _ := gatewayOn(t, s)
		refused(t, s, desk, readyRoot(t, s, desk),
			"the runner is off (GAOS_RUNNER); dispatching spends money and needs it on or manual")
		if chatPosts(f) != 0 {
			t.Fatal("a refused dispatch reached the gateway")
		}
	})
	t.Run("no adapter", func(t *testing.T) {
		t.Setenv("GAOS_RUNNER", "manual")
		_, s, desk := plannedProduct(t)
		refused(t, s, desk, readyRoot(t, s, desk),
			"no model adapter is configured (GAOS_MODEL_MODE); nothing can be dispatched")
	})
	t.Run("below the floor", func(t *testing.T) {
		t.Setenv("GAOS_RUNNER", "manual")
		_, s, desk := plannedProduct(t)
		f, _ := gatewayOn(t, s)
		id := readyRoot(t, s, desk)
		s.mu.Lock()
		m := s.masterPtr(desk)
		m.BRD = BRD{}
		m.Stories = nil
		score := m.ReadinessScore()
		s.mu.Unlock()
		if score >= PlanFloor {
			t.Fatalf("emptying the record left readiness at %d; this case needs it below %d", score, PlanFloor)
		}
		refused(t, s, desk, id, "is below the planning floor")
		if chatPosts(f) != 0 {
			t.Fatal("a dispatch below the readiness floor reached the gateway")
		}
	})
	t.Run("desk route under manual", func(t *testing.T) {
		t.Setenv("GAOS_RUNNER", "manual")
		s := NewMemStore(20260807)
		s.egress = EgressPolicy{}
		f, _ := gatewayOn(t, s)
		if _, err := s.DispatchTask(ctx, idleDesk(t, s)); err != nil {
			t.Fatalf("the desk route failed: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
		if n := chatPosts(f); n != 0 {
			t.Fatalf("the desk route sent %d turn(s) under a manual runner; only the board dispatch may spend", n)
		}
	})
}

func TestTwoBoardDispatchesInOneProductStartOneTurn(t *testing.T) {
	t.Setenv("GAOS_RUNNER", "manual")
	_, s, desk := plannedProduct(t)
	f, _ := gatewayOn(t, s)
	a := readyRoot(t, s, desk)
	// A second card with no prerequisite, as a retry or an answer leaves one:
	// the plan has a single root, so free another task of its dependencies.
	s.mu.Lock()
	for i := range s.tasks {
		if s.tasks[i].DeskID == desk && s.tasks[i].ID != a {
			s.tasks[i].Deps = nil
			break
		}
	}
	s.mu.Unlock()
	b := readyRoot(t, s, desk, a)
	f.pending = 1000 // keep the first turn live while the second is tried
	ctx := context.Background()

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, id := range []string{a, b} {
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			_, errs[i] = s.MoveCard(ctx, desk, id, CardMove{Column: ColWorking})
		}(i, id)
	}
	wg.Wait()
	ok := 0
	for _, err := range errs {
		if err == nil {
			ok++
		} else if !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "BR-38") {
			t.Fatalf("the losing dispatch returned %v; want the BR-38 refusal", err)
		}
	}
	if ok != 1 {
		t.Fatalf("%d of two concurrent dispatches in one product succeeded; want exactly 1 (BR-38)", ok)
	}
	deadline := time.Now().Add(2 * time.Second)
	for chatPosts(f) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	if n := chatPosts(f); n != 1 {
		t.Fatalf("the gateway received %d turn(s) from two concurrent dispatches in one product; want 1", n)
	}
}

func TestAnAnsweredTaskWaitsInReadyUnderAManualRunner(t *testing.T) {
	t.Setenv("GAOS_RUNNER", "manual")
	s := NewMemStore(1)
	f, ad := gatewayOn(t, s)
	id := askedTask(t, s, f, ad, "wf-q-t-9")
	desk := s.desks[0].ID
	if _, err := s.AnswerQuestion(t.Context(), desk, id, 2, "keep it on one node", AnswerSource{}); err != nil {
		t.Fatal(err)
	}
	postsBefore := chatPosts(f)

	s.RunOnce(context.Background(), ad)
	if st := taskStatusOf(s, id); st != StatusReady {
		t.Fatalf("an answered task is %s after a manual pass; want READY, waiting for the operator", st)
	}
	if len(f.answers["q-"+id]) != 0 || chatPosts(f) != postsBefore {
		t.Fatal("a manual pass sent the answered turn; only the operator's board move may")
	}

	completeTurn(f, id, 2, "0.1")
	if _, err := s.MoveCard(t.Context(), desk, id, CardMove{Column: ColWorking}); err != nil {
		t.Fatalf("the board dispatch of the answered task failed: %v", err)
	}
	waitForRun(t, s, runIDFor(id, 2), RunStatusExited)
	f.mu.Lock()
	got := f.answers["q-"+id]
	chat := f.posts[ad.RequestIDFor(runIDFor(id, 2))]
	f.mu.Unlock()
	if len(got) != 1 || got[0].RequestID != ad.RequestIDFor(runIDFor(id, 2)) || got[0].Choice != 2 {
		t.Fatalf("the answer route received %+v; want one answer under the next attempt's request id", got)
	}
	if chat != 0 {
		t.Fatal("the answered turn was also posted to /v1/chat; the board route must resume through the answer route")
	}
}

func TestHealthzSaysWhichRunnerModeIsOn(t *testing.T) {
	for _, c := range []struct{ env, want string }{{"", "off"}, {"manual", "manual"}, {"on", "on"}} {
		t.Setenv("GAOS_RUNNER", c.env)
		srv, _ := newTestServer(t)
		w := do(t, srv, "GET", "/healthz", nil)
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("healthz is not JSON: %v", err)
		}
		if body["runner"] != c.want {
			t.Errorf("GAOS_RUNNER=%q: healthz says runner %v; want %q", c.env, body["runner"], c.want)
		}
	}
}
