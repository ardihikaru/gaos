package main

import (
	"fmt"
	"testing"
	"time"
)

// The fixtures that replaced the fake claude binary (M14.7). A gateway turn
// needs no folder and no subprocess, so a test that once planted a script in a
// TempDir now points the store at the fake ai-manager in aimanager_test.go.

// gatewayOn points s at a fresh fake ai-manager and returns it. The reply is
// a complete handoff for whichever run the caller names through f.reply.
func gatewayOn(t *testing.T, s *MemStore) (*fakeAIM, *AIMAdapter) {
	t.Helper()
	f, ad := newFakeAIM(t)
	s.SetAdapter(ad)
	return f, ad
}

// completeTurn scripts the fake to answer attempt of task with a complete
// handoff costing cost.
func completeTurn(f *fakeAIM, task string, attempt int, cost string) {
	f.reply = chatReply("done\n\n"+handoffBlock(task, runIDFor(task, attempt)), cost, "")
}

// askTurn scripts the fake to answer with a question for the operator: three
// options, the first recommended, and no handoff (M15).
func askTurn(f *fakeAIM, qid string) {
	f.reply = chatReply("I need a decision before I build the store.", "0.05",
		fmt.Sprintf(`,"finish_reason":"awaiting_input","question":{"id":%q,"question":"Which database should the orders service use?",`+
			`"context":"The BRD names none and the two choices change the schema.","options":[`+
			`{"label":"PostgreSQL","description":"what the other services already run"},`+
			`{"label":"SQLite","description":"simplest, single node"},`+
			`{"label":"ArcadeDB","description":"the graph the platform uses"}]}`, qid))
}

// clearedFor is the screened brief for attempt of task on desk, carrying the
// task's session the way the scheduler's own route would.
func clearedFor(t *testing.T, s *MemStore, desk, task string, attempt int, parts ...PacketPart) Cleared {
	t.Helper()
	s.mu.RLock()
	ta, _ := s.adapter.(TurnAdapter)
	var sid, reason string
	if ta != nil {
		sid, reason = s.sessionForLocked(task, ta, time.Now().Unix())
	}
	s.mu.RUnlock()
	if len(parts) == 0 {
		parts = []PacketPart{{Path: ".gaos/packet/task.md", Kind: KindTask, Body: "# " + task}}
	}
	c, _, err := Screen(EgressPolicy{}, Target{Provider: "anthropic"}, Packet{TaskID: task, RunID: runIDFor(task, attempt),
		DeskID: desk, SessionID: sid, SessionReason: reason, Parts: parts})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// withRepo makes s hand every packet these repository files, the way M1.2's
// index (or a knowledge source, OQ-28) will. path -> body.
func withRepo(s *MemStore, files map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.repoParts = func(string) []PacketPart {
		var out []PacketPart
		for p, b := range files {
			out = append(out, PacketPart{Path: p, Kind: KindRepo, Body: b})
		}
		return out
	}
}
