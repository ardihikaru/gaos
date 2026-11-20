package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// v0.40.2: a board move hung for minutes on gaos.sample-product.com. The scheduler's
// background write held the ArcadeStore lock while it rewrote every desk, task,
// run and context slice in the floor -- an existence probe per citation edge --
// against a slow database, and every handler and the question sweep queued
// behind it. These pin the three halves of the fix against a fake ArcadeDB.

// fakeGraph is an ArcadeDB that answers every statement with an empty result
// and records what it was sent.
type fakeGraph struct {
	mu         sync.Mutex
	stmts      []string
	failCommit bool
}

func (g *fakeGraph) handler(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasPrefix(r.URL.Path, "/api/v1/begin/"):
		w.Header().Set("arcadedb-session-id", "AS-fake")
		w.WriteHeader(http.StatusNoContent)
	case strings.HasPrefix(r.URL.Path, "/api/v1/commit/"):
		g.mu.Lock()
		fail := g.failCommit
		g.mu.Unlock()
		if fail {
			w.WriteHeader(http.StatusInternalServerError)
			io.WriteString(w, `{"error":"Remote transaction not found or expired"}`)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case strings.HasPrefix(r.URL.Path, "/api/v1/rollback/"):
		w.WriteHeader(http.StatusNoContent)
	default:
		var body struct {
			Command string `json:"command"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		g.mu.Lock()
		g.stmts = append(g.stmts, strings.Join(strings.Fields(body.Command), " "))
		g.mu.Unlock()
		io.WriteString(w, `{"result":[]}`)
	}
}

// take returns the statements sent since the last take, and forgets them.
func (g *fakeGraph) take() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := g.stmts
	g.stmts = nil
	return out
}

func countPrefix(stmts []string, prefix string) int {
	n := 0
	for _, s := range stmts {
		if strings.HasPrefix(s, prefix) {
			n++
		}
	}
	return n
}

// arcadeOver puts an ArcadeStore over a planned MemStore without the boot path,
// which would apply the schema against the fake.
func arcadeOver(t *testing.T, h http.HandlerFunc) (*ArcadeStore, *MemStore, string) {
	t.Helper()
	_, ms, desk := plannedProduct(t)
	return &ArcadeStore{arc: testArcade(t, h), tenant: "t-writes", proj: ms}, ms, desk
}

func persistOnce(t *testing.T, s *ArcadeStore, ms *MemStore, desk string, seq int) error {
	t.Helper()
	f, err := ms.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.persist(context.Background(), Result{Floor: f,
		NewEvents: []Event{{Seq: seq, Type: TaskMoved, DeskID: desk}}})
}

func TestAPersistWritesOnlyTheRecordsThatChanged(t *testing.T) {
	g := &fakeGraph{}
	s, ms, desk := arcadeOver(t, g.handler)
	tasks := productTasks(ms, desk)
	if len(tasks) == 0 {
		t.Fatal("the planned product has no tasks")
	}

	if err := persistOnce(t, s, ms, desk, 9001); err != nil {
		t.Fatalf("first persist: %v", err)
	}
	if n := countPrefix(g.take(), "UPDATE Task "); n < len(tasks) {
		t.Fatalf("the first persist upserted %d Task vertices; want at least the %d planned", n, len(tasks))
	}

	// Nothing changed: the event is the only thing to write.
	if err := persistOnce(t, s, ms, desk, 9002); err != nil {
		t.Fatalf("second persist: %v", err)
	}
	second := g.take()
	if n := countPrefix(second, "UPDATE "); n != 0 {
		t.Fatalf("a persist with nothing changed sent %d upsert(s); want 0 -- every write rewrote the whole floor, "+
			"which is what held the store lock for minutes on a slow database", n)
	}
	if n := countPrefix(second, "INSERT INTO Event"); n != 1 {
		t.Fatalf("a persist with one event inserted %d event(s); want 1", n)
	}

	// One task changed: that task, and nothing else of the task graph.
	setStatus(ms, tasks[0].ID, StatusReady)
	if err := persistOnce(t, s, ms, desk, 9003); err != nil {
		t.Fatalf("third persist: %v", err)
	}
	if n := countPrefix(g.take(), "UPDATE Task "); n != 1 {
		t.Fatalf("one changed task produced %d Task upsert(s); want exactly 1", n)
	}

	// A commit that fails must not leave the record marked as written.
	setStatus(ms, tasks[0].ID, StatusPending)
	g.mu.Lock()
	g.failCommit = true
	g.mu.Unlock()
	if err := persistOnce(t, s, ms, desk, 9004); err == nil {
		t.Fatal("a persist whose commit failed reported success")
	}
	g.take()
	g.mu.Lock()
	g.failCommit = false
	g.mu.Unlock()
	if err := persistOnce(t, s, ms, desk, 9005); err != nil {
		t.Fatalf("persist after the failed commit: %v", err)
	}
	if n := countPrefix(g.take(), "UPDATE Task "); n != 1 {
		t.Fatalf("after a failed commit the next persist upserted %d Task(s); want 1 -- "+
			"a write that never landed was forgotten as if it had", n)
	}
}

func TestABackgroundWriteReleasesTheStoreLockWhenTheGraphStalls(t *testing.T) {
	release := make(chan struct{})
	h := func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/v1/begin/"):
			w.Header().Set("arcadedb-session-id", "AS-slow")
			w.WriteHeader(http.StatusNoContent)
		case strings.HasPrefix(r.URL.Path, "/api/v1/rollback/"):
			w.WriteHeader(http.StatusNoContent)
		default:
			select { // a database that answers, eventually
			case <-release:
			case <-time.After(3 * time.Second):
			}
			io.WriteString(w, `{"result":[]}`)
		}
	}
	s, ms, desk := arcadeOver(t, h)
	t.Cleanup(func() { close(release) }) // before srv.Close; see TestArcadeQueryHonoursTheContextDeadline

	old := backgroundPersistBudget
	backgroundPersistBudget = 150 * time.Millisecond
	t.Cleanup(func() { backgroundPersistBudget = old })

	f, _ := ms.Snapshot(context.Background())
	start := time.Now()
	err := s.persistBackground(context.Background(), Result{Floor: f,
		NewEvents: []Event{{Seq: 9101, Type: TaskMoved, DeskID: desk}}})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("a background write against a stalled database reported success")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("the background write held the store lock for %s against a stalled database; "+
			"want it bounded by backgroundPersistBudget, or a board move waits behind it", elapsed)
	}
	if !s.mu.TryLock() {
		t.Fatal("the store lock is still held after the background write gave up")
	}
	s.mu.Unlock()
}

func TestAManualPassPublishesItsReadiesAsOneWrite(t *testing.T) {
	_, s, desk := plannedProduct(t)
	tasks := productTasks(s, desk)
	if len(tasks) < 2 {
		t.Fatalf("the plan has %d tasks; this case needs two", len(tasks))
	}
	var writes [][]Event
	s.SetOnWrite(func(r Result) { writes = append(writes, r.NewEvents) })

	a, b := tasks[0].ID, tasks[1].ID
	setStatus(s, a, StatusPending)
	setStatus(s, b, StatusPending)
	both := ScheduleDecision{Ready: []Dispatch{{TaskID: a, DeskID: desk}, {TaskID: b, DeskID: desk}}}
	s.readyPending(both)
	if len(writes) != 1 || len(writes[0]) != 2 {
		t.Fatalf("a pass that readied two cards published %d write(s); want 1 carrying both moves -- "+
			"one transaction per card multiplies the time the store lock is held", len(writes))
	}

	// Both are READY now: the same decision again moves nothing and writes nothing.
	writes = nil
	s.readyPending(both)
	if len(writes) != 0 {
		t.Fatalf("a pass that moved nothing published %d write(s); want 0", len(writes))
	}
}
