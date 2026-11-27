package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// Unit checks (D-105, docs/designs/release-b4-unit-tests.md §4): the setting
// and the watch. The product, repositories and branches are invented, as
// D-085 requires.

const (
	ucSHA  = "1111111111111111111111111111111111111111"
	ucSHA2 = "2222222222222222222222222222222222222222"
)

// unitAF is an auto-fix setting with the invented repository as its fix target.
func unitAF() AutoFix {
	af := fixAF()
	af.TargetRepos = []string{defRepo}
	return af
}

// unitStore is a product that may turn unit checks on: auto-fix on, a fixer and
// a triager assigned, a fix-target repository.
func unitStore(t *testing.T) (*MemStore, string) {
	t.Helper()
	s, pid := qaFixtureRepo(t)
	ctx := context.Background()
	if _, err := s.SetAutoFix(ctx, pid, unitAF(), "ops", tokenKindFine); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetAssigned(ctx, pid, []string{"a-11", "a-12", "a-13"}, true, testEmail); err != nil {
		t.Fatal(err)
	}
	return s, pid
}

// TC-851
func TestUnitChecksAreSavedAndRefused(t *testing.T) {
	ctx := context.Background()
	// The refusals that need no product state, each alone.
	s, pid := unitStore(t)
	for want, u := range map[string]UnitChecks{
		"The daily cap is 1 to 20.":           {DailyCap: 21},
		"The daily cap is 1 to 20. ":          {DailyCap: -1},
		"A unit-test base is a branch name.":  {Bases: []string{"release/../main"}},
		"A unit-test base is a branch name. ": {Bases: []string{"a", "b", "c", "d", "e", "f", "g", "h", "i"}},
		"The check name is 1 to 100 letters, digits, spaces, dots, dashes or underscores.": {CheckName: "unit;tests"},
		"The workflow is a file under .github/workflows/.":                                 {Workflow: "ci/unit.yml"},
		"The workflow is a file under .github/workflows/. ":                                {Workflow: ".github/workflows/sub/unit.yml"},
	} {
		_, err := s.SetUnitChecks(ctx, pid, u, testEmail)
		if err == nil || !errors.Is(err, ErrRejected) || err.Error() != strings.TrimSpace(want) {
			t.Errorf("%+v answered %v; want %q", u, err, strings.TrimSpace(want))
		}
	}
	// Turning it on needs auto-fix, a fixer and a triager, and a fix-target repository.
	for want, mod := range map[string]func(*MemStore){
		"Turn auto-fix on first.": func(s *MemStore) {
			a := unitAF()
			a.Enabled = false
			_, _ = s.SetAutoFix(ctx, pid, a, "ops", tokenKindFine)
		},
		"Assign a fixer and a triager first.": func(s *MemStore) {
			_, _ = s.SetAssigned(ctx, pid, []string{"a-13"}, true, testEmail)
		},
		"Assign a fixer and a triager first. ": func(s *MemStore) {
			_, _ = s.SetAssigned(ctx, pid, nil, false, testEmail)
		},
		"List a fix-target repository first.": func(s *MemStore) {
			a := unitAF()
			a.AutomationRepos = []string{defRepo}
			_, _ = s.SetAutoFix(ctx, pid, a, "ops", tokenKindFine)
		},
	} {
		s, _ := unitStore(t)
		mod(s)
		if _, err := s.SetUnitChecks(ctx, pid, UnitChecks{Enabled: true}, testEmail); err == nil || err.Error() != strings.TrimSpace(want) {
			t.Errorf("%s: answered %v", strings.TrimSpace(want), err)
		}
		// Off is always saved.
		if _, err := s.SetUnitChecks(ctx, pid, UnitChecks{DailyCap: 3}, testEmail); err != nil {
			t.Errorf("%s: saving it off answered %v", strings.TrimSpace(want), err)
		}
	}
	// A save: the values, the defaults, the event, the actor's alias.
	res, err := s.SetUnitChecks(ctx, pid, UnitChecks{Enabled: true, Bases: []string{" release/next ", "release/next"}}, testEmail)
	if err != nil {
		t.Fatal(err)
	}
	m := masterOf(t, s, pid)
	u := m.UnitChecks
	if !u.Enabled || u.WorkflowPath() != ".github/workflows/unit-tests.yml" || u.Name() != "unit-tests" || u.Cap() != 5 ||
		strings.Join(u.Bases, ",") != "release/next" {
		t.Errorf("saved %+v (workflow %s, check %s, cap %d)", u, u.WorkflowPath(), u.Name(), u.Cap())
	}
	want := "unit checks on (.github/workflows/unit-tests.yml, unit-tests, bases release/next, cap 5)"
	if len(res.NewEvents) != 1 || res.NewEvents[0].Type != RecordUpdated || !strings.Contains(res.NewEvents[0].Text, want) ||
		res.NewEvents[0].Payload.Actor != "ardi" {
		t.Errorf("the event reads %+v; want %q by ardi", res.NewEvents, want)
	}
	// The stored no-ci day survives a save; a save never writes it.
	if _, err := s.MarkUnitNotice(ctx, pid, "", UnitNoticeNoCI); err != nil {
		t.Fatal(err)
	}
	day := masterOf(t, s, pid).UnitChecks.LastNoCINoticeDay
	if _, err := s.SetUnitChecks(ctx, pid, UnitChecks{Enabled: true, Workflow: ".github/workflows/go.yaml", CheckName: "go test",
		DailyCap: 20, LastNoCINoticeDay: "1999-01-01"}, testEmail); err != nil {
		t.Fatal(err)
	}
	u = masterOf(t, s, pid).UnitChecks
	if day == "" || u.LastNoCINoticeDay != day || u.WorkflowPath() != ".github/workflows/go.yaml" || u.Name() != "go test" || u.Cap() != 20 {
		t.Errorf("after a second save %+v; want the no-ci day %q kept", u, day)
	}
	// The read-only handle refuses every write.
	ro := readOnly{s}
	for name, call := range map[string]func() error{
		"SetUnitChecks":    func() error { _, e := ro.SetUnitChecks(ctx, pid, UnitChecks{}, testEmail); return e },
		"PlantUnitWatch":   func() error { _, e := ro.PlantUnitWatch(ctx, pid, UnitWatch{}); return e },
		"DelayUnitWatch":   func() error { _, e := ro.DelayUnitWatch(ctx, "k", 0, 0, 0); return e },
		"PruneUnitWatches": func() error { _, e := ro.PruneUnitWatches(ctx, 0); return e },
		"MarkUnitNotice":   func() error { _, e := ro.MarkUnitNotice(ctx, pid, "", UnitNoticeNoCI); return e },
	} {
		if err := call(); !errors.Is(err, ErrRejected) {
			t.Errorf("readOnly.%s answered %v, want ErrRejected", name, err)
		}
	}
	if ws, err := ro.UnitWatches(ctx, pid); ws != nil || err != nil {
		t.Errorf("readOnly.UnitWatches answered %v %v, want nil", ws, err)
	}
}

func unitWatchIn(sha string) UnitWatch {
	return UnitWatch{Repo: "Acme-Org/Sample-Service", PR: 7, SHA: strings.ToUpper(sha), Base: "develop", MergedAt: 1000, NextAt: 1120}
}

// TC-852
func TestUnitWatchIsPlantedOnceAndPersists(t *testing.T) {
	ctx := context.Background()
	s, pid := unitStore(t)
	res, err := s.PlantUnitWatch(ctx, pid, unitWatchIn(ucSHA))
	if err != nil || len(res.UnitWatches) != 1 || len(res.NewEvents) != 0 {
		t.Fatalf("plant: %+v %v; want one watch and no event", res, err)
	}
	w := res.UnitWatches[0]
	if w.Key != pid+"/acme-org/sample-service@"+ucSHA || w.Repo != "acme-org/sample-service" || w.SHA != ucSHA ||
		w.State != UnitWatchPending || w.FirstSeen == 0 {
		t.Errorf("the watch is %+v", w)
	}
	if again, err := s.PlantUnitWatch(ctx, pid, unitWatchIn(ucSHA)); err != nil || len(again.UnitWatches) != 0 {
		t.Errorf("a replayed merge planted again: %+v %v", again, err)
	}
	for name, w := range map[string]UnitWatch{
		"a short sha":     {Repo: defRepo, SHA: "abc123", Base: "develop"},
		"no repository":   {Repo: "nope", SHA: ucSHA, Base: "develop"},
		"no base":         {Repo: defRepo, SHA: ucSHA2},
		"a bad outcome":   {Repo: defRepo, SHA: ucSHA2, Base: "develop", State: UnitWatchDone, Outcome: "maybe"},
		"a 64-hex commit": {Repo: defRepo, SHA: strings.Repeat("a", 64), Base: "develop"},
	} {
		if _, err := s.PlantUnitWatch(ctx, pid, w); !errors.Is(err, ErrRejected) {
			t.Errorf("%s was planted: %v", name, err)
		}
	}
	if _, err := s.PlantUnitWatch(ctx, "p-404", unitWatchIn(ucSHA2)); !errors.Is(err, ErrNotFound) {
		t.Errorf("a watch of no product answered %v", err)
	}
	// At most 200 pending per product; a done watch is not counted.
	for i := 0; i < unitMaxPending-1; i++ {
		if _, err := s.PlantUnitWatch(ctx, pid, UnitWatch{Repo: defRepo, SHA: fmt.Sprintf("%040x", i+16), Base: "develop"}); err != nil {
			t.Fatalf("watch %d: %v", i, err)
		}
	}
	if _, err := s.PlantUnitWatch(ctx, pid, UnitWatch{Repo: defRepo, SHA: ucSHA2, Base: "develop"}); err == nil || err.Error() != "Too many pending unit checks." {
		t.Errorf("the 201st pending watch answered %v", err)
	}
	if _, err := s.PlantUnitWatch(ctx, pid, UnitWatch{Key: unitSkippedKey(pid, defRepo, 9), Repo: defRepo, SHA: unitZeroSHA,
		Base: unitBaseUnknown, State: UnitWatchDone, Outcome: "skipped:read", DoneAt: 50}); err != nil {
		t.Errorf("a done watch past the pending cap answered %v", err)
	}
	// Delay, list, prune.
	if r, err := s.DelayUnitWatch(ctx, w.Key, 5000, 2, 3); err != nil || len(r.UnitWatches) != 1 || r.UnitWatches[0].NextAt != 5000 ||
		r.UnitWatches[0].Tries != 2 || r.UnitWatches[0].Reads != 3 {
		t.Errorf("delay: %+v %v", r, err)
	}
	ws, _ := s.UnitWatches(ctx, pid)
	if len(ws) != unitMaxPending+1 {
		t.Errorf("listed %d watches, want %d", len(ws), unitMaxPending+1)
	}
	pr, err := s.PruneUnitWatches(ctx, 100)
	if err != nil || strings.Join(pr.UnitWatchDeletes, ",") != unitSkippedKey(pid, defRepo, 9) {
		t.Errorf("prune deleted %v %v; want only the done watch", pr.UnitWatchDeletes, err)
	}

	// ArcadeDB: the watch, its delay, its prune and the setting survive a reopen.
	arc := arcadeTestTarget(t)
	tenant := "conf-unit-watch"
	first := newArcadeTestStore(t, arc, tenant)
	apid := "p-1"
	af := AutoFix{Enabled: true, TargetRepos: []string{"loop/laundry-api"}}
	if _, err := first.SetAutoFix(ctx, apid, af, "ops", tokenKindFine); err != nil {
		t.Fatal(err)
	}
	if _, err := first.SetAssigned(ctx, apid, []string{"a-12", "a-13"}, true, testEmail); err != nil {
		t.Fatal(err)
	}
	if _, err := first.SetUnitChecks(ctx, apid, UnitChecks{Enabled: true, Bases: []string{"release/next"}, DailyCap: 7}, testEmail); err != nil {
		t.Fatal(err)
	}
	if _, err := first.MarkUnitNotice(ctx, apid, "", UnitNoticeNoCI); err != nil {
		t.Fatal(err)
	}
	in := UnitWatch{Repo: "loop/laundry-api", PR: 4, SHA: ucSHA, Base: "develop", MergedAt: 1000, NextAt: 1120}
	if _, err := first.PlantUnitWatch(ctx, apid, in); err != nil {
		t.Fatal(err)
	}
	key := unitWatchKey(apid, "loop/laundry-api", ucSHA)
	if _, err := first.DelayUnitWatch(ctx, key, 9000, 1, 2); err != nil {
		t.Fatal(err)
	}
	gone := UnitWatch{Repo: "loop/laundry-api", PR: 5, SHA: ucSHA2, Base: "develop", State: UnitWatchDone, Outcome: UnitNoCI, DoneAt: 10}
	if _, err := first.PlantUnitWatch(ctx, apid, gone); err != nil {
		t.Fatal(err)
	}
	skipped := UnitWatch{Key: unitSkippedKey(apid, "loop/laundry-api", 6), Repo: "loop/laundry-api", PR: 6, SHA: unitZeroSHA,
		Base: unitBaseUnknown, State: UnitWatchDone, Outcome: "skipped:sha", DoneAt: 5000}
	if _, err := first.PlantUnitWatch(ctx, apid, skipped); err != nil {
		t.Fatal(err)
	}
	if _, err := first.PruneUnitWatches(ctx, 100); err != nil {
		t.Fatal(err)
	}
	second, err := NewArcadeStoreFor(ctx, arc, 1, tenant)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := second.UnitWatches(ctx, apid)
	if len(got) != 2 {
		t.Fatalf("after a reopen %d watches (%+v); want the pending and the skipped one", len(got), got)
	}
	byKey := map[string]UnitWatch{}
	for _, g := range got {
		byKey[g.Key] = g
	}
	if g := byKey[key]; g.NextAt != 9000 || g.Tries != 1 || g.Reads != 2 || g.State != UnitWatchPending || g.PR != 4 ||
		g.MergedAt != 1000 || g.Base != "develop" || g.SHA != ucSHA {
		t.Errorf("the pending watch came back as %+v", g)
	}
	if g := byKey[skipped.Key]; g.Outcome != "skipped:sha" || g.State != UnitWatchDone || g.DoneAt != 5000 {
		t.Errorf("the skipped watch came back as %+v", g)
	}
	uc := masterOf(t, second, apid).UnitChecks
	if !uc.Enabled || uc.Cap() != 7 || strings.Join(uc.Bases, ",") != "release/next" || uc.LastNoCINoticeDay == "" {
		t.Errorf("the setting came back as %+v", uc)
	}
	if o := masterOf(t, second, "p-2").UnitChecks; o.Enabled || o.Bases != nil {
		t.Errorf("an untouched product reopened with %+v", o)
	}
}
