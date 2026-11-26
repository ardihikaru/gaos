package main

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The defect records reach the graph in the transaction of the change that
// touched them (D-091), against a fake ArcadeDB that records what it is sent.

func TestDefectRecordsAndAutoAtArePersisted(t *testing.T) {
	g := &fakeGraph{}
	s, ms, _ := arcadeOver(t, g.handler)
	ctx := context.Background()
	ms.mu.Lock()
	m := ms.masterPtr("p-1")
	m.Repos = append(m.Repos, defRepo)
	ms.mu.Unlock()
	importCases(t, ms, "p-1", false, qaCase(defCase1))
	g.take()

	out, _, err := s.ReportDefects(ctx, "p-1", DefectReportIn{Source: "push", Items: []DefectItem{defItem(DefectKindXfail, "DEF-101", defCase1)}})
	if err != nil || out.Applied != 1 {
		t.Fatalf("ReportDefects: %+v %v", out, err)
	}
	stmts := g.take()
	if countPrefix(stmts, "UPDATE Defect ") != 1 {
		t.Errorf("%d Defect upserts, want 1 (statements: %d)", countPrefix(stmts, "UPDATE Defect "), len(stmts))
	}
	if countPrefix(stmts, "UPDATE Task ") == 0 {
		t.Error("the planned triage card was not written with the record")
	}
	if !strings.Contains(strings.Join(stmts, "\n"), "autoAt") {
		t.Error("the Task upsert does not carry autoAt")
	}

	if _, err := s.UpsertFixPR(ctx, FixPR{ID: "fx-1", ProductID: "p-1", DefectKey: "p-1/DEF-101", Repo: defRepo,
		State: FixPRPreparing, CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(), Reply: "{}"}); err != nil {
		t.Fatalf("UpsertFixPR: %v", err)
	}
	if n := countPrefix(g.take(), "UPDATE FixPR "); n != 1 {
		t.Errorf("%d FixPR upserts, want 1", n)
	}
	if _, err := s.SetDefectContext(ctx, "p-1", "p-1/DEF-101", DefectContext{Built: true}); err != nil {
		t.Fatal(err)
	}
	if n := countPrefix(g.take(), "UPDATE Defect "); n != 1 {
		t.Errorf("a stored context wrote %d Defect upserts, want 1", n)
	}
	if _, err := s.SetAutoFix(ctx, "p-1", AutoFix{AllowedGlobs: []string{"pkg/**"}}, "ops", "fine-grained"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(g.take(), "\n"), "autoFix") {
		t.Error("the configuration was not written on the Product record")
	}
}

// TestDefectRecordsBehaveTheSameInBothStores runs one script against both
// stores through the Store seam.
func TestDefectRecordsBehaveTheSameInBothStores(t *testing.T) {
	run := func(t *testing.T, s Store) []string {
		ctx := context.Background()
		var trail []string
		out, _, err := s.ReportDefects(ctx, "p-1", DefectReportIn{Source: "push", Items: []DefectItem{
			defItem(DefectKindXfail, "DEF-101", defCase1), defItem(DefectKindXfail, "DEF-101", defCase1),
			defItem(DefectKindFail, "", "TC-SAMPLE-999")}})
		if err != nil {
			t.Fatal(err)
		}
		trail = append(trail, strings.Join([]string{strconv.Itoa(out.Applied), strconv.Itoa(out.Unchanged), strconv.Itoa(len(out.Rejected))}, "/"))
		ds, _ := s.Defects(ctx, "p-1")
		for _, d := range ds {
			trail = append(trail, d.Key+":"+d.State+":"+strconv.Itoa(d.Gen))
		}
		return trail
	}
	build := func() *MemStore {
		s := NewMemStore(1)
		s.mu.Lock()
		s.masterPtr("p-1").Repos = append(s.masterPtr("p-1").Repos, defRepo)
		s.mu.Unlock()
		importCases(t, s, "p-1", false, qaCase(defCase1))
		return s
	}
	mem := run(t, build())
	g := &fakeGraph{}
	ms := build()
	arc := &ArcadeStore{arc: testArcade(t, g.handler), tenant: "t-defects", proj: ms}
	viaArc := run(t, arc)
	if strings.Join(mem, "|") != strings.Join(viaArc, "|") {
		t.Errorf("the stores disagree: memory %v, arcade %v", mem, viaArc)
	}
}

// A dispatch the egress pass refuses records itself through the projection's write hook, which
// takes the ArcadeStore's write lock. AutoDispatch used to run inside mutate, which holds that
// lock: the first refusal deadlocked the store and every later mutation hung (found live,
// 2026-11-26, in a goroutine dump). The same held for an operator's board move to "running".
func TestARefusedDispatchDoesNotFreezeTheArcadeStore(t *testing.T) {
	e := newAutoEnv(t, "DEF-101")
	g := &fakeGraph{}
	s := &ArcadeStore{arc: testArcade(t, g.handler), tenant: "t-dl", proj: e.s}
	e.s.SetOnWrite(func(r Result) { _ = s.persistBackground(context.Background(), r) })
	c := e.triage("DEF-101")
	secret := "package x\nconst k = \"AKIAIOSFODNN7EXAMPLE\"\n"
	if _, err := e.s.SetDefectContext(context.Background(), e.pid, e.pid+"/DEF-101", DefectContext{Built: true,
		Repos: []DefectContextRepo{{Repo: defRepo, Head: "h"}},
		Files: []DefectContextFile{{Repo: defRepo, Path: "pkg/x.go", SHA: "b", Text: secret}}}); err != nil {
		t.Fatal(err)
	}
	within := func(name string, f func()) {
		t.Helper()
		done := make(chan struct{})
		go func() { f(); close(done) }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatalf("%s did not return: the store is deadlocked on its own write lock", name)
		}
	}
	var err error
	within("a refused AutoDispatch", func() { _, err = s.AutoDispatch(context.Background(), e.pid, c.ID) })
	if !errors.Is(err, ErrRejected) {
		t.Errorf("a refused AutoDispatch gave %v, want a rejection", err)
	}
	within("a mutation after the refusal", func() {
		_, err = s.SetAutoFix(context.Background(), e.pid, fixAF(), "ops", tokenKindFine)
	})
	if err != nil {
		t.Errorf("a mutation after the refusal gave %v", err)
	}
}

// The same for the operator's side: a board move to "running" that the egress pass refuses.
func TestARefusedBoardMoveDoesNotFreezeTheArcadeStore(t *testing.T) {
	e := newAutoEnv(t, "DEF-101")
	g := &fakeGraph{}
	s := &ArcadeStore{arc: testArcade(t, g.handler), tenant: "t-dl2", proj: e.s}
	e.s.SetOnWrite(func(r Result) { _ = s.persistBackground(context.Background(), r) })
	c := e.triage("DEF-101")
	secret := "package x\nconst k = \"AKIAIOSFODNN7EXAMPLE\"\n"
	if _, err := e.s.SetDefectContext(context.Background(), e.pid, e.pid+"/DEF-101", DefectContext{Built: true,
		Repos: []DefectContextRepo{{Repo: defRepo, Head: "h"}},
		Files: []DefectContextFile{{Repo: defRepo, Path: "pkg/x.go", SHA: "b", Text: secret}}}); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := s.MoveCard(context.Background(), e.pid, c.ID, CardMove{Column: ColWorking, Index: -1})
		if err == nil {
			_, err = s.SetAutoFix(context.Background(), e.pid, fixAF(), "ops", tokenKindFine)
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, ErrRejected) {
			t.Errorf("the refused move gave %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("a refused board move did not return: the store is deadlocked on its own write lock")
	}
	if _, err := s.SetAutoFix(context.Background(), e.pid, fixAF(), "ops", tokenKindFine); err != nil {
		t.Errorf("a mutation after the refused move gave %v", err)
	}
}
