package main

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Auto-fix configuration and dispatch (D-091, design §6): the switch, the
// admins, the caps, the gates and the record-based eligibility.

func autoFixAdminsFile(t *testing.T, mode os.FileMode, lines ...string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "admins")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GAOS_AUTOFIX_ADMINS", p)
}

func validCfg() url.Values {
	return url.Values{"enabled": {"on"}, "fixCap": {"2"}, "triageCap": {"5"}, "allowedGlobs": {"pkg/**"},
		"testGlobs": {"**/*_test.go"}, "ciConfirmed": {"on"}, "prTemplate": {"TEMPLATEMARKER {{before}} {{after}}"}}
}

func cfgPost(t *testing.T, srv *server, pid string, v url.Values) int {
	t.Helper()
	return formPost(t, srv, "/products/"+pid+"/autofix", v.Encode(), true).Code
}

func TestAutoFixConfigRefusedWithoutPreconditions(t *testing.T) {
	autoFixAdminsFile(t, 0o600, testEmail)
	srv, s, pid := defectServer(t)
	srv.writeTokenKind = tokenKindFine
	without := func(keys ...string) url.Values {
		v := validCfg()
		for _, k := range keys {
			v.Del(k)
		}
		return v
	}
	bad := map[string]url.Values{
		"no allowed globs":             without("allowedGlobs"),
		"no test globs":                without("testGlobs"),
		"CI not confirmed":             without("ciConfirmed"),
		"a template without slots":     func() url.Values { v := validCfg(); v.Set("prTemplate", "no slots here"); return v }(),
		"a test glob of **":            func() url.Values { v := validCfg(); v.Set("testGlobs", "**"); return v }(),
		"a test glob without a marker": func() url.Values { v := validCfg(); v.Set("testGlobs", "pkg/**"); return v }(),
		"a fix cap over 10":            func() url.Values { v := validCfg(); v.Set("fixCap", "11"); return v }(),
		"a triage cap over 50":         func() url.Values { v := validCfg(); v.Set("triageCap", "51"); return v }(),
		"a glob with ..":               func() url.Values { v := validCfg(); v.Set("allowedGlobs", "pkg/../**"); return v }(),
	}
	for name, v := range bad {
		if code := cfgPost(t, srv, pid, v); code != 422 {
			t.Errorf("%s: answered %d, want 422", name, code)
		}
	}
	if af, _ := s.AutoFixOf(context.Background(), pid); af.Enabled {
		t.Fatal("a refused configuration was stored")
	}
	// No write token: refused even when everything else is right.
	srv.writeTokenKind = ""
	if code := cfgPost(t, srv, pid, validCfg()); code != 422 {
		t.Errorf("without the write token: answered %d, want 422", code)
	}
	// Turning it off needs none of it.
	off := url.Values{"enabled": {""}}
	if code := cfgPost(t, srv, pid, off); code != 200 {
		t.Errorf("switching it off answered %d, want 200", code)
	}
	srv.writeTokenKind = tokenKindClassic
	if code := cfgPost(t, srv, pid, validCfg()); code != 200 {
		t.Fatalf("a valid configuration answered %d, want 200", code)
	}
	af, _ := s.AutoFixOf(context.Background(), pid)
	if !af.Enabled || af.FixCap != 2 || af.TriageCap != 5 || !af.CIConfirmed {
		t.Errorf("the stored configuration is %+v", af)
	}
	evs := streamOf(s, AutoFixConfigured)
	last := evs[len(evs)-1]
	if last.Payload.Actor != "ardi" || !strings.Contains(last.Payload.Reason, "token=classic (broad)") {
		t.Errorf("AutoFixConfigured names actor %q and %q; want the alias and the token kind", last.Payload.Actor, last.Payload.Reason)
	}
	for _, e := range evs {
		if strings.Contains(e.Text+e.Payload.Reason, "TEMPLATEMARKER") || strings.Contains(e.Text+e.Payload.Reason, testEmail) {
			t.Error("an AutoFixConfigured event carries the template text or the address")
		}
	}
}

func TestAutoFixConfigAdminsOnly(t *testing.T) {
	srv, s, pid := defectServer(t)
	srv.writeTokenKind = tokenKindFine
	report(t, s, pid, defItem(DefectKindXfail, "DEF-101", defCase1))
	// No file, no variable: nobody.
	t.Setenv("GAOS_AUTOFIX_ADMINS", "")
	if code := cfgPost(t, srv, pid, validCfg()); code != 403 {
		t.Errorf("with no admin list: %d, want 403", code)
	}
	t.Setenv("GAOS_AUTOFIX_ADMINS", filepath.Join(t.TempDir(), "missing"))
	if code := cfgPost(t, srv, pid, validCfg()); code != 403 {
		t.Errorf("with a missing admin file: %d, want 403", code)
	}
	autoFixAdminsFile(t, 0o644, testEmail)
	if code := cfgPost(t, srv, pid, validCfg()); code != 403 {
		t.Errorf("with a world-readable admin file: %d, want 403", code)
	}
	autoFixAdminsFile(t, 0o600, "someone-else@example.test", "# "+testEmail)
	if code := cfgPost(t, srv, pid, validCfg()); code != 403 {
		t.Errorf("for an account that is not listed: %d, want 403", code)
	}
	if code := formPost(t, srv, "/products/"+pid+"/autofix", validCfg().Encode(), false).Code; code == 200 {
		t.Error("an anonymous request changed the configuration")
	}
	if af, _ := s.AutoFixOf(context.Background(), pid); af.Enabled {
		t.Fatal("a refused request changed the configuration")
	}
	autoFixAdminsFile(t, 0o600, strings.ToUpper(testEmail))
	if code := cfgPost(t, srv, pid, validCfg()); code != 200 {
		t.Errorf("for a listed account: %d, want 200", code)
	}

	// The close route: any signed-in operator; another product's record is 404.
	if code := formPost(t, srv, "/products/p-2/defects/DEF-101/close", "", true).Code; code != 404 {
		t.Errorf("closing a record of another product answered %d, want 404", code)
	}
	if code := formPost(t, srv, "/products/"+pid+"/defects/DEF-101/close", "", true).Code; code != 200 {
		t.Fatalf("closing a defect answered %d, want 200", code)
	}
	if d, _ := s.DefectByKey(context.Background(), pid, pid+"/DEF-101"); d.State != DefectStateClosed {
		t.Errorf("the defect is %s, want closed", d.State)
	}
	if code := formPost(t, srv, "/products/"+pid+"/defects/DEF-101/close", "", false).Code; code == 200 {
		t.Error("an anonymous request closed a defect")
	}
}

// ---------------------------------------------------------------- dispatch

type autoEnv struct {
	s   *MemStore
	pid string
	f   *fakeAIM
}

// newAutoEnv is product p-1 with auto-fix on, the runner manual and a fake
// gateway; the clock is pinned and put back at the end.
func newAutoEnv(t *testing.T, defects ...string) *autoEnv {
	t.Helper()
	t.Setenv("GAOS_RUNNER", "manual")
	s, pid := defectFixture(t)
	f, _ := gatewayOn(t, s)
	idleFleet(s)
	af := fixAF()
	af.TriageCap = 1
	if _, err := s.SetAutoFix(context.Background(), pid, af, "ops", tokenKindFine); err != nil {
		t.Fatal(err)
	}
	for i, id := range defects {
		c := defCase1
		if i == 1 {
			c = defCase2
		}
		report(t, s, pid, defItem(DefectKindXfail, id, c))
	}
	s.readyDefectCards()
	old := autoFixNow
	t.Cleanup(func() { autoFixNow = old })
	return &autoEnv{s: s, pid: pid, f: f}
}

func (e *autoEnv) triage(id string) TaskRecord {
	return *cardByKey(e.s, defectCardKey(e.pid+"/"+id, 1, "triage"))
}

// script makes the next turn of a card end with its handoff.
func (e *autoEnv) script(card string) { completeTurn(e.f, card, 1, "0.1") }

func rejectedAndReady(t *testing.T, e *autoEnv, card TaskRecord, name string) {
	t.Helper()
	_, err := e.s.AutoDispatch(context.Background(), e.pid, card.ID)
	if !errors.Is(err, ErrRejected) {
		t.Errorf("%s: AutoDispatch gave %v, want a rejection", name, err)
	}
	got := *cardByKey(e.s, card.DedupKey)
	if got.Status != StatusReady || got.AutoAt != 0 {
		t.Errorf("%s: the card is %s with autoAt %d, want READY and unstamped", name, got.Status, got.AutoAt)
	}
	if n := chatPosts(e.f); n != 0 {
		t.Errorf("%s: %d turn(s) reached the gateway", name, n)
	}
}

func TestAutoFixSchedulerNeverDispatchesDefectCardsInAnyMode(t *testing.T) {
	for _, mode := range []string{"on", "manual", "off"} {
		e := newAutoEnv(t, "DEF-101")
		t.Setenv("GAOS_RUNNER", mode)
		_, ad := newFakeAIM(t)
		e.s.SetAdapter(ad)
		for i := 0; i < 5; i++ {
			e.s.RunOnce(context.Background(), ad)
		}
		c := e.triage("DEF-101")
		if c.Status != StatusReady && c.Status != StatusPending {
			t.Errorf("GAOS_RUNNER=%s: the triage card is %s; the scheduler must only ready it", mode, c.Status)
		}
		e.s.mu.RLock()
		runs := len(e.s.runs)
		e.s.mu.RUnlock()
		if runs != 0 {
			t.Errorf("GAOS_RUNNER=%s: %d run(s) started", mode, runs)
		}
	}
	for _, l := range []string{LabelDefectTriage, LabelDefectEasy, LabelDefectMedium, LabelDefectComplex} {
		if labelsAllowDispatch([]string{l}) {
			t.Errorf("the scheduler may dispatch %s", l)
		}
	}
}

func TestAutoFixAutoDispatchPersistsUnderArcade(t *testing.T) {
	t.Setenv("GAOS_RUNNER", "manual")
	g := &fakeGraph{}
	s, ms, pid := arcadeOver(t, g.handler)
	ms.mu.Lock()
	ms.masterPtr(pid).Repos = append(ms.masterPtr(pid).Repos, defRepo)
	ms.mu.Unlock()
	importCases(t, ms, pid, false, qaCase(defCase1))
	f, _ := gatewayOn(t, ms)
	idleFleet(ms)
	if _, err := ms.SetAutoFix(context.Background(), pid, fixAF(), "ops", tokenKindFine); err != nil {
		t.Fatal(err)
	}
	report(t, ms, pid, defItem(DefectKindXfail, "DEF-101", defCase1))
	ms.readyDefectCards()
	card := *cardByKey(ms, defectCardKey(pid+"/DEF-101", 1, "triage"))
	completeTurn(f, card.ID, 1, "0.1")
	g.take()
	if _, err := s.AutoDispatch(context.Background(), pid, card.ID); err != nil {
		t.Fatalf("AutoDispatch: %v", err)
	}
	stmts := strings.Join(g.take(), "\n")
	moved := false
	for _, ev := range streamOf(ms, TaskMoved) {
		moved = moved || ev.Payload.Rule == MoveAutoFix
	}
	if !moved || countPrefix(strings.Split(stmts, "\n"), "INSERT INTO Event") == 0 {
		t.Error("the TaskMoved of the automatic dispatch was not written to the graph")
	}
	if !strings.Contains(stmts, "UPDATE Task ") || !strings.Contains(stmts, "autoAt") {
		t.Error("the stamped task was not written to the graph")
	}
	waitForRun(t, ms, runIDFor(card.ID, 1), RunStatusExited)
	// A replayed floor refuses it.
	if _, err := (readOnly{ms}).AutoDispatch(context.Background(), pid, card.ID); !errors.Is(err, ErrRejected) {
		t.Errorf("a read-only store gave %v, want a rejection", err)
	}
}

func TestAutoFixCapCountsDispatches(t *testing.T) {
	e := newAutoEnv(t, "DEF-101", "DEF-102")
	ctx := context.Background()
	a, b := e.triage("DEF-101"), e.triage("DEF-102")
	e.script(a.ID)
	if n := autoFixPass(ctx, e.s, nil); n != 1 {
		t.Fatalf("the first pass dispatched %d card(s), want 1", n)
	}
	waitForRun(t, e.s, runIDFor(a.ID, 1), RunStatusExited)
	if got := *cardByKey(e.s, a.DedupKey); got.AutoAt == 0 {
		t.Error("the dispatched card was not stamped")
	}
	// The triage cap is 1: the second card waits, READY, with nothing spent.
	e.script(b.ID)
	if n := autoFixPass(ctx, e.s, nil); n != 0 {
		t.Fatalf("a pass over the cap dispatched %d card(s), want 0", n)
	}
	if got := *cardByKey(e.s, b.DedupKey); got.Status != StatusReady || got.AutoAt != 0 {
		t.Errorf("the second card is %s (autoAt %d), want READY and unstamped", got.Status, got.AutoAt)
	}
	// The count is per UTC day, and the fix cap is a separate count.
	tomorrow := e.triage("DEF-101").AutoAt + 86400
	autoFixNow = func() int64 { return tomorrow }
	if n := autoFixPass(ctx, e.s, nil); n != 1 {
		t.Fatalf("the next day's pass dispatched %d card(s), want 1", n)
	}
	waitForRun(t, e.s, runIDFor(b.ID, 1), RunStatusExited)
}

func TestAutoFixEachGateIsApplied(t *testing.T) {
	secret := "package x\nconst k = \"AKIAIOSFODNN7EXAMPLE\"\n"
	gates := []struct {
		name   string
		arm    func(t *testing.T, e *autoEnv, c TaskRecord)
		status string // after the refusal; READY unless the gate blocks
		reason string
	}{
		{"the runner is off (BR-56)", func(t *testing.T, e *autoEnv, c TaskRecord) { t.Setenv("GAOS_RUNNER", "off") }, StatusReady, ""},
		{"readiness below the floor (BR-07)", func(t *testing.T, e *autoEnv, c TaskRecord) {
			e.s.mu.Lock()
			m := e.s.masterPtr(e.pid)
			m.Stories, m.BRD = nil, BRD{}
			low := m.ReadinessScore() < PlanFloor
			e.s.mu.Unlock()
			if !low {
				t.Skip("the product still clears the floor")
			}
		}, StatusReady, ""},
		{"one live turn per product (BR-38)", func(t *testing.T, e *autoEnv, c TaskRecord) {
			setStatus(e.s, e.triage("DEF-102").ID, StatusDispatched)
		}, StatusReady, ""},
		{"a prerequisite that is not done", func(t *testing.T, e *autoEnv, c TaskRecord) {
			dep := e.triage("DEF-102").ID
			setStatus(e.s, dep, StatusPending)
			e.s.mu.Lock()
			e.s.taskPtr(c.ID).Deps = []string{dep}
			e.s.mu.Unlock()
		}, StatusReady, ""},
		{"the egress pass (BR-32)", func(t *testing.T, e *autoEnv, c TaskRecord) {
			key := e.pid + "/DEF-101"
			if _, err := e.s.SetDefectContext(context.Background(), e.pid, key, DefectContext{Built: true,
				Repos: []DefectContextRepo{{Repo: defRepo, Head: "h"}},
				Files: []DefectContextFile{{Repo: defRepo, Path: "pkg/x.go", SHA: "b", Text: secret}}}); err != nil {
				t.Fatal(err)
			}
		}, StatusBlocked, BlockEgress},
	}
	for _, g := range gates {
		e := newAutoEnv(t, "DEF-101", "DEF-102")
		c := e.triage("DEF-101")
		g.arm(t, e, c)
		if _, err := e.s.AutoDispatch(context.Background(), e.pid, c.ID); !errors.Is(err, ErrRejected) {
			t.Errorf("%s: AutoDispatch gave %v, want a rejection", g.name, err)
		}
		got := *cardByKey(e.s, c.DedupKey)
		if got.Status != g.status || got.AutoAt != 0 || got.BlockReason != g.reason {
			t.Errorf("%s: the card is %s %q (autoAt %d), want %s %q unstamped", g.name, got.Status, got.BlockReason, got.AutoAt, g.status, g.reason)
		}
		if n := chatPosts(e.f); n != 0 {
			t.Errorf("%s: %d turn(s) reached the gateway", g.name, n)
		}
	}
	// The drift gate (BR-39), when the fake gateway can hold a workspace.
	e := newAutoEnv(t, "DEF-101")
	if _, err := e.s.PublishBlueprint(context.Background(), e.pid); err == nil {
		if _, ok := e.s.adapter.(WorkspaceGateway); ok {
			c := e.triage("DEF-101")
			_, err := e.s.AutoDispatch(context.Background(), e.pid, c.ID)
			if !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "BR-39") {
				t.Errorf("a published Blueprint the workspace does not hold: AutoDispatch gave %v, want a BR-39 rejection", err)
			}
		}
	}
}

func TestAutoFixSwitchOffLeavesReady(t *testing.T) {
	e := newAutoEnv(t, "DEF-101")
	if _, err := e.s.SetAutoFix(context.Background(), e.pid, AutoFix{}, "ops", tokenKindFine); err != nil {
		t.Fatal(err)
	}
	c := e.triage("DEF-101")
	e.script(c.ID)
	if n := autoFixPass(context.Background(), e.s, nil); n != 0 {
		t.Errorf("a pass with the switch off dispatched %d card(s)", n)
	}
	rejectedAndReady(t, e, c, "the switch is off")
	for _, ev := range streamOf(e.s, TaskMoved) {
		if ev.Payload.Rule == MoveAutoFix {
			t.Error("an automatic move was written with the switch off")
		}
	}
	// Switching it on is all it takes: nothing had to be released.
	if _, err := e.s.SetAutoFix(context.Background(), e.pid, fixAF(), "ops", tokenKindFine); err != nil {
		t.Fatal(err)
	}
	if n := autoFixPass(context.Background(), e.s, nil); n != 1 {
		t.Fatalf("after the switch went on, the pass dispatched %d card(s), want 1", n)
	}
	waitForRun(t, e.s, runIDFor(c.ID, 1), RunStatusExited)
}

// mediumFix is a product whose defect was triaged with the verdict v.
func mediumEnv(t *testing.T, v TriageVerdict) *autoEnv {
	t.Helper()
	e := newAutoEnv(t)
	report(t, e.s, e.pid, defItem(DefectKindXfail, "DEF-101", defCase1))
	key := e.pid + "/DEF-101"
	if _, err := e.s.SetDefectContext(context.Background(), e.pid, key, DefectContext{Built: true,
		Repos: []DefectContextRepo{{Repo: defRepo, Head: "h"}},
		Files: []DefectContextFile{{Repo: defRepo, Path: fxFile, SHA: "b-" + fxFile, Text: fxText}}}); err != nil {
		t.Fatal(err)
	}
	triage := cardByKey(e.s, defectCardKey(key, 1, "triage"))
	if _, err := e.s.ResolveTriage(context.Background(), e.pid, triage.ID, v); err != nil {
		t.Fatal(err)
	}
	return e
}

func TestAutoFixMediumNeverAutoDispatches(t *testing.T) {
	for _, diff := range []string{"medium", "complex"} {
		e := mediumEnv(t, TriageVerdict{Difficulty: diff, Class: "business-rule", Repo: defRepo, Files: []string{fxFile}, Reason: "x"})
		fix := *cardByKey(e.s, defectCardKey(e.pid+"/DEF-101", 1, "fix"))
		if fix.Status != StatusBlocked || fix.BlockReason != BlockNotEasy {
			t.Fatalf("a %s fix card is %s %q, want BLOCKED %s", diff, fix.Status, fix.BlockReason, BlockNotEasy)
		}
		// Even forced to READY (a person's move), the loop and the claim refuse it.
		setStatus(e.s, fix.ID, StatusReady)
		e.script(fix.ID)
		if n := autoFixPass(context.Background(), e.s, nil); n != 0 {
			t.Errorf("%s: a pass dispatched %d card(s)", diff, n)
		}
		rejectedAndReady(t, e, fix, diff)
	}
	// An easy card of an end-to-end defect or a regression is refused by its record.
	for name, mut := range map[string]func(*Defect){
		"an end-to-end defect": func(d *Defect) { d.AutomationType = "e2e" },
		"a regression":         func(d *Defect) { d.Regression = true },
	} {
		e := mediumEnv(t, TriageVerdict{Difficulty: "easy", Class: "min-value", Repo: defRepo, Files: []string{fxFile}, Reason: "x"})
		key := e.pid + "/DEF-101"
		fix := *cardByKey(e.s, defectCardKey(key, 1, "fix"))
		e.s.mu.Lock()
		mut(e.s.defects[key])
		e.s.mu.Unlock()
		setStatus(e.s, fix.ID, StatusReady)
		rejectedAndReady(t, e, fix, name)
	}
}

func TestAutoFixHandMovedMediumEndsInReviewWithoutPR(t *testing.T) {
	env := newFixEnvWith(t, TriageVerdict{Difficulty: "medium", Class: "business-rule", Repo: defRepo, Files: []string{fxFile}, Reason: "x"})
	env.run(fixReplyText(goodFix()))
	if c := env.now(); c.Status != StatusReview {
		t.Errorf("a medium card ended %s %q, want REVIEW", c.Status, c.BlockReason)
	}
	if n := len(env.gh.calls); n != 0 {
		t.Errorf("GitHub saw %d request(s) for a medium card", n)
	}
	if recs, _ := env.s.FixPRs(context.Background(), env.pid); len(recs) != 0 || len(env.notes) != 0 {
		t.Errorf("%d pull-request record(s) and %d notification(s) for a medium card", len(recs), len(env.notes))
	}
	// An easy card of a product whose switch is off ends the same way.
	easy := newFixEnv(t)
	if _, err := easy.s.SetAutoFix(context.Background(), easy.pid, AutoFix{}, "ops", tokenKindFine); err != nil {
		t.Fatal(err)
	}
	easy.run(fixReplyText(goodFix()))
	if c := easy.now(); c.Status != StatusReview || len(easy.gh.calls) != 0 {
		t.Errorf("with the switch off the card is %s with %d GitHub request(s), want REVIEW and none", c.Status, len(easy.gh.calls))
	}
}

func TestAutoFixRelabelledCardNeverOpensAPR(t *testing.T) {
	e := newFixEnvWith(t, TriageVerdict{Difficulty: "medium", Class: "business-rule", Repo: defRepo, Files: []string{fxFile}, Reason: "x"})
	// An operator cannot relabel it: the defect family is the planner's.
	if _, err := e.s.SetTaskLabel(context.Background(), e.pid, e.card.ID, LabelDefectEasy, "ops"); !errors.Is(err, ErrRejected) {
		t.Errorf("relabelling a defect card gave %v, want a rejection", err)
	}
	// Even a card whose label was forced to defect:easy is judged by its record.
	e.s.mu.Lock()
	e.s.taskPtr(e.card.ID).Labels = []string{LabelDefectEasy}
	e.s.mu.Unlock()
	e.card = e.now()
	e.run(fixReplyText(goodFix()))
	if c := e.now(); c.Status != StatusReview {
		t.Errorf("the relabelled card is %s, want REVIEW", c.Status)
	}
	if len(e.gh.calls) != 0 || len(e.notes) != 0 {
		t.Errorf("a relabelled card reached GitHub (%d requests) or the group (%d)", len(e.gh.calls), len(e.notes))
	}
}
