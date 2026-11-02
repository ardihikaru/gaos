package main

import (
	"context"
	"net/http"
	"testing"
	"time"
)

// M14.5: sessions per task, recovery by request id, the transcript. Against
// the fake ai-manager in aimanager_test.go; no folder on this host, which is
// what a gateway turn no longer needs.

func turnStore(t *testing.T) (*MemStore, *fakeAIM, *AIMAdapter, string) {
	t.Helper()
	s := NewMemStore(1)
	f, ad := newFakeAIM(t)
	s.SetAdapter(ad)
	return s, f, ad, s.desks[0].ID
}

func turnCleared(t *testing.T, s *MemStore, desk, task string, attempt int) Cleared {
	t.Helper()
	s.mu.RLock()
	sid, reason := s.sessionForLocked(task, s.adapter.(TurnAdapter), time.Now().Unix())
	s.mu.RUnlock()
	c, _, err := Screen(EgressPolicy{}, Target{Provider: "anthropic"}, Packet{TaskID: task, RunID: runIDFor(task, attempt), DeskID: desk,
		SessionID: sid, SessionReason: reason, Parts: []PacketPart{{Path: ".gaos/packet/task.md", Kind: KindTask, Body: "# " + task}}})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func storeEvents(s *MemStore, typ EventType) []Event {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Event
	for _, e := range s.events {
		if e.Type == typ {
			out = append(out, e)
		}
	}
	return out
}

func TestARetryResumesTheTasksSessionAndAnEscalationOpensANewOne(t *testing.T) {
	s, f, ad, desk := turnStore(t)
	ctx := context.Background()
	for a := 1; a <= 2; a++ {
		f.reply = chatReply(handoffBlock("t-1", runIDFor("t-1", a)), "0.1", "")
		if _, err := s.superviseRun(ctx, ad, desk, turnCleared(t, s, desk, "t-1", a), a); err != nil {
			t.Fatalf("attempt %d: %v", a, err)
		}
	}
	if first, retry := f.bodies[0], f.bodies[1]; !first.NewSession || retry.NewSession || retry.SessionID == "" {
		t.Fatalf("attempt 1 sent %+v and the retry %+v; a retry on the same model must resume the task's session (D-056)", first, retry)
	}
	ad.Model = "claude-opus-5" // an escalation
	f.reply = chatReply(handoffBlock("t-1", runIDFor("t-1", 3)), "0.1", "")
	if _, err := s.superviseRun(ctx, ad, desk, turnCleared(t, s, desk, "t-1", 3), 3); err != nil {
		t.Fatal(err)
	}
	opened := storeEvents(s, SessionOpened)
	if !f.bodies[2].NewSession || len(opened) != 2 || opened[1].Payload.Rule != SessionEscalation {
		t.Fatalf("after an escalation the turn sent %+v and SessionOpened was %v; want a new session opened with reason escalation", f.bodies[2], opened)
	}
}

func TestRecoveryAdoptsACompletedRequest(t *testing.T) {
	s, f, ad, desk := turnStore(t)
	run := runIDFor("t-1", 1)
	f.reply = chatReply(handoffBlock("t-1", run), "0.3", "")
	s.mu.Lock()
	s.runs = append(s.runs, TaskRunRecord{ID: run, TaskID: "t-1", DeskID: desk, Attempt: 1, Status: RunStatusRunning,
		Invocation: ad.Mode(), RequestID: ad.RequestIDFor(run), StartedAt: time.Now().Unix()})
	s.mu.Unlock()
	if _, err := s.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	r := waitForRun(t, s, run, RunStatusExited)
	if r.FailureSignature != "" || r.SessionID == "" || r.CostBasis != CostMeasured {
		t.Fatalf("the recovered turn closed as %+v; want the completed attempt a supervised run would have produced", r)
	}
	if len(f.bodies) != 0 {
		t.Fatalf("recovery sent %d new turn(s); it must poll the request it already has", len(f.bodies))
	}
}

func TestALostRequestIsOrphanedHeldUntilTheDeadlineThenRequeued(t *testing.T) {
	s, f, ad, desk := turnStore(t)
	ad.Deadline = 200 * time.Millisecond
	f.status, f.reply = http.StatusNotFound, `{"error":"unknown request"}`
	run := runIDFor("t-lost", 1)
	s.mu.Lock()
	s.tasks = append(s.tasks, TaskRecord{ID: "t-lost", DeskID: desk, Status: StatusRunning})
	s.runs = append(s.runs, TaskRunRecord{ID: run, TaskID: "t-lost", DeskID: desk, Attempt: 1, Status: RunStatusRunning,
		RequestID: ad.RequestIDFor(run), StartedAt: time.Now().Unix() + 1})
	s.mu.Unlock()
	s.Reconcile(context.Background())
	deadline := time.Now().Add(3 * time.Second)
	for len(storeEvents(s, TaskOrphaned)) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	orphaned := storeEvents(s, TaskOrphaned)
	if len(orphaned) != 1 || orphaned[0].Payload.Rule != SigRequestLost || taskStatusOf(s, "t-lost") != StatusRunning {
		t.Fatalf("a lost request gave %v with the task %s; want TaskOrphaned rule %s and the product still held (D-060)", orphaned, taskStatusOf(s, "t-lost"), SigRequestLost)
	}
	r := waitForRun(t, s, run, RunStatusOrphaned)
	s.mu.RLock()
	_, reason := s.sessionForLocked("t-lost", ad, time.Now().Unix())
	s.mu.RUnlock()
	if taskStatusOf(s, "t-lost") != StatusReady || r.FailureSignature != SigRequestLost || reason != SessionLostReason {
		t.Fatalf("after the deadline the task is %s, the run %+v, next session reason %q; want READY, request-lost, and a new session", taskStatusOf(s, "t-lost"), r, reason)
	}
}

func TestThePromptAndReplyOutliveTheSession(t *testing.T) {
	s, f, ad, desk := turnStore(t)
	ctx := context.Background()
	f.reply = chatReply("my notes\n"+handoffBlock("t-1", runIDFor("t-1", 1)), "0.1", "")
	if _, err := s.superviseRun(ctx, ad, desk, turnCleared(t, s, desk, "t-1", 1), 1); err != nil {
		t.Fatal(err)
	}
	f.submit = http.StatusGone
	if _, err := s.superviseRun(ctx, ad, desk, turnCleared(t, s, desk, "t-1", 2), 2); err != nil {
		t.Fatal(err)
	}
	if lost := storeEvents(s, SessionLost); len(lost) != 1 {
		t.Fatalf("the 410 gave %d SessionLost event(s), want 1", len(lost))
	}
	s.mu.RLock()
	prompt, reply := s.artifacts[runIDFor("t-1", 1)+"-prompt"], s.artifacts[runIDFor("t-1", 1)+"-reply"]
	s.mu.RUnlock()
	if prompt.Kind != ArtifactPrompt || prompt.Body == "" || reply.Kind != ArtifactReply || reply.Body[:8] != "my notes" {
		t.Fatalf("after the session was lost the store holds prompt %+v and reply %+v; both must outlive it (BR-41)", prompt, reply)
	}
}

func waitForRun(t *testing.T, s *MemStore, id, status string) TaskRunRecord {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.RLock()
		for _, r := range s.runs {
			if r.ID == id && r.Status == status {
				s.mu.RUnlock()
				return r
			}
		}
		s.mu.RUnlock()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("run %s never reached %s", id, status)
	return TaskRunRecord{}
}

func taskStatusOf(s *MemStore, id string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, t := range s.tasks {
		if t.ID == id {
			return t.Status
		}
	}
	return ""
}
