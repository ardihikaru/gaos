package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
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

// TC-853
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

// TC-854
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

// unitPRSource is a pull-request source that also reads a default branch, and
// counts the reads.
type unitPRSource struct {
	pr      ghPR
	err     error
	def     string
	defErr  error
	defRead int
}

func (f *unitPRSource) pullRequest(context.Context, string, string, int) (ghPR, error) {
	return f.pr, f.err
}
func (f *unitPRSource) pullFiles(context.Context, string, string, int) ([]ghFile, error) {
	return nil, nil
}
func (f *unitPRSource) repoDefault(context.Context, string, string) (string, error) {
	f.defRead++
	return f.def, f.defErr
}

// recordedMerge is the recorded GitHub answer for a merged pull request.
func recordedMerge(t *testing.T) ghPR {
	t.Helper()
	b, err := os.ReadFile("testdata/unit/pr-merged.json")
	if err != nil {
		t.Fatal(err)
	}
	var pr ghPR
	if err := json.Unmarshal(b, &pr); err != nil {
		t.Fatal(err)
	}
	return pr
}

func unitMergeEvent(n int) PREvent {
	return PREvent{Kind: "github", Type: prTypeMerged, EventID: "gh-9", Seq: 9, Org: "acme-org", Repo: "sample-service", PRNumber: n}
}

// unitOnStore is unitStore with unit checks on.
func unitOnStore(t *testing.T, u UnitChecks) (*MemStore, string) {
	t.Helper()
	s, pid := unitStore(t)
	u.Enabled = true
	if _, err := s.SetUnitChecks(context.Background(), pid, u, testEmail); err != nil {
		t.Fatal(err)
	}
	return s, pid
}

func unitWatchesOf(t *testing.T, s Store, pid string) []UnitWatch {
	t.Helper()
	ws, err := s.UnitWatches(context.Background(), pid)
	if err != nil {
		t.Fatal(err)
	}
	return ws
}

// TC-855
func TestAMergeIntoAUnitBasePlantsAWatchNotAnAgentCard(t *testing.T) {
	ctx := context.Background()
	pr := recordedMerge(t)
	if pr.MergeCommitSHA != "3c5e7a9b1d2f4a6c8e0b1d3f5a7c9e1b3d5f7a9c" || pr.MergedAt == nil ||
		pr.MergedAt.Unix() != 1795745398 || pr.Base.Ref != "develop" || !pr.Merged {
		t.Fatalf("the recorded body decoded as sha %q merged_at %v base %q", pr.MergeCommitSHA, pr.MergedAt, pr.Base.Ref)
	}
	var logs []string
	logf := func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }
	s, pid := unitOnStore(t, UnitChecks{})
	src := &unitPRSource{pr: pr, def: "develop"}
	flow := &prFlow{st: s, gh: src, logf: logf}
	ev := unitMergeEvent(41)
	if out := flow.handle(ctx, ev); out != flowDone {
		t.Fatalf("the merge answered %v, want done", out)
	}
	ws := unitWatchesOf(t, s, pid)
	if len(ws) != 1 {
		t.Fatalf("%d watches after the merge (%v); want 1", len(ws), logs)
	}
	w := ws[0]
	if w.Key != pid+"/acme-org/sample-service@"+pr.MergeCommitSHA || w.SHA != pr.MergeCommitSHA || w.Base != "develop" ||
		w.MergedAt != pr.MergedAt.Unix() || w.PR != 41 || w.State != UnitWatchPending || w.NextAt != w.FirstSeen+120 {
		t.Errorf("the watch is %+v", w)
	}
	if c := cardByKey(s, ev.Key(prJobUnitTest)); c != nil {
		t.Errorf("an agent unit-test card was planned beside the watch: %+v", c)
	}
	// A replay plants nothing more, and the default branch is read once in 10 minutes.
	flow.handle(ctx, ev)
	if n := len(unitWatchesOf(t, s, pid)); n != 1 || src.defRead != 1 {
		t.Errorf("a replay left %d watches and %d default reads; want 1 and 1", n, src.defRead)
	}

	// The miss classes: a transient re-read retries, a permanent one, no merged_at
	// and a bad merge commit are recorded as done watches.
	transient := &unitPRSource{err: &ghError{Status: 502}}
	s2, _ := unitOnStore(t, UnitChecks{})
	if out := (&prFlow{st: s2, gh: transient, logf: logf}).handle(ctx, ev); out != flowRetry || len(unitWatchesOf(t, s2, pid)) != 0 {
		t.Errorf("a transient re-read answered %v with %d watches; want a retry and none", out, len(unitWatchesOf(t, s2, pid)))
	}
	noMerge := pr
	noMerge.MergedAt = nil
	badSHA := pr
	badSHA.MergeCommitSHA = strings.Repeat("a", 64)
	for class, src := range map[string]*unitPRSource{
		"skipped:read":      {err: &ghError{Status: 404}},
		"skipped:merged-at": {pr: noMerge, def: "develop"},
		"skipped:sha":       {pr: badSHA, def: "develop"},
	} {
		s3, _ := unitOnStore(t, UnitChecks{})
		if out := (&prFlow{st: s3, gh: src, logf: logf}).handle(ctx, ev); out != flowDone {
			t.Errorf("%s: answered %v", class, out)
		}
		ws := unitWatchesOf(t, s3, pid)
		if len(ws) != 1 || ws[0].Outcome != class || ws[0].State != UnitWatchDone || ws[0].DoneAt == 0 ||
			ws[0].Key != unitSkippedKey(pid, defRepo, 41) {
			t.Errorf("%s: the watches are %+v", class, ws)
		}
		if c := cardByKey(s3, ev.Key(prJobUnitTest)); c != nil {
			t.Errorf("%s: an agent unit-test card was planned", class)
		}
	}

	// The base: a feature branch plants nothing (and plans no card); a listed extra
	// base plants without reading the default; an unreadable default plants nothing.
	feature := pr
	feature.Base.Ref = "feature/other"
	s4, _ := unitOnStore(t, UnitChecks{})
	(&prFlow{st: s4, gh: &unitPRSource{pr: feature, def: "develop"}, logf: logf}).handle(ctx, ev)
	if n := len(unitWatchesOf(t, s4, pid)); n != 0 || cardByKey(s4, ev.Key(prJobUnitTest)) != nil {
		t.Errorf("a merge into a feature branch left %d watches or a card", n)
	}
	extra := pr
	extra.Base.Ref = "release/next"
	s5, _ := unitOnStore(t, UnitChecks{Bases: []string{"release/next"}})
	src5 := &unitPRSource{pr: extra, defErr: errors.New("never read")}
	(&prFlow{st: s5, gh: src5, logf: logf}).handle(ctx, ev)
	if ws := unitWatchesOf(t, s5, pid); len(ws) != 1 || ws[0].Base != "release/next" || src5.defRead != 0 {
		t.Errorf("a merge into a listed base left %+v after %d default reads", ws, src5.defRead)
	}
	s6, _ := unitOnStore(t, UnitChecks{})
	logs = nil
	(&prFlow{st: s6, gh: &unitPRSource{pr: pr, defErr: &ghError{Status: 403}}, logf: logf}).handle(ctx, ev)
	if n := len(unitWatchesOf(t, s6, pid)); n != 0 || !strings.Contains(strings.Join(logs, "\n"), "unit-watch-skipped default-token") {
		t.Errorf("an unreadable default left %d watches; logs %v", n, logs)
	}
	// AutoFix.BaseBranch is the effective base when set: no default read.
	s7, _ := unitStore(t)
	af := unitAF()
	af.BaseBranch = "develop"
	if _, err := s7.SetAutoFix(ctx, pid, af, "ops", tokenKindFine); err != nil {
		t.Fatal(err)
	}
	if _, err := s7.SetUnitChecks(ctx, pid, UnitChecks{Enabled: true}, testEmail); err != nil {
		t.Fatal(err)
	}
	src7 := &unitPRSource{pr: pr, defErr: errors.New("never read")}
	(&prFlow{st: s7, gh: src7, logf: logf}).handle(ctx, ev)
	if n := len(unitWatchesOf(t, s7, pid)); n != 1 || src7.defRead != 0 {
		t.Errorf("with a base branch set: %d watches after %d default reads; want 1 and 0", n, src7.defRead)
	}
}

// TC-856
func TestOtherMergesKeepTodaysUnitCard(t *testing.T) {
	ctx := context.Background()
	pr := recordedMerge(t)
	ev := unitMergeEvent(41)
	planned := func(s *MemStore) bool { return cardByKey(s, ev.Key(prJobUnitTest)) != nil }
	// Unit checks off: today's card, no watch.
	s, pid := unitStore(t)
	(&prFlow{st: s, gh: &unitPRSource{pr: pr, def: "develop"}, logf: func(string, ...any) {}}).handle(ctx, ev)
	if !planned(s) || len(unitWatchesOf(t, s, pid)) != 0 {
		t.Errorf("with unit checks off: card %t, %d watches; want the card and none", planned(s), len(unitWatchesOf(t, s, pid)))
	}
	// On, but the repository is not a fix target (an automation repository, or not listed).
	for name, mod := range map[string]func(*AutoFix){
		"an automation repository": func(a *AutoFix) { a.TargetRepos = append(a.TargetRepos, "loop/laundry-api") },
		"an unlisted repository":   func(a *AutoFix) {},
	} {
		s, pid := unitOnStore(t, UnitChecks{})
		a := unitAF()
		mod(&a)
		if name == "an automation repository" {
			a.AutomationRepos = []string{"loop/laundry-api"}
		}
		if _, err := s.SetAutoFix(ctx, pid, a, "ops", tokenKindFine); err != nil {
			t.Fatal(err)
		}
		other := PREvent{Kind: "github", Type: prTypeMerged, Org: "loop", Repo: "laundry-api", PRNumber: 5}
		(&prFlow{st: s, gh: &unitPRSource{pr: pr, def: "develop"}, logf: func(string, ...any) {}}).handle(ctx, other)
		if cardByKey(s, other.Key(prJobUnitTest)) == nil || len(unitWatchesOf(t, s, pid)) != 0 {
			t.Errorf("%s: card %t, %d watches; want the card and none", name, cardByKey(s, other.Key(prJobUnitTest)) != nil,
				len(unitWatchesOf(t, s, pid)))
		}
	}
	// Another product keeps its card while one product has unit checks on.
	s2, _ := unitOnStore(t, UnitChecks{})
	o := PREvent{Kind: "github", Type: prTypeMerged, Org: "loop", Repo: "billing-core", PRNumber: 3}
	if _, _, res, err := s2.PlanPullRequestCards(ctx, "p-2", o); err != nil || cardByKey(s2, o.Key(prJobUnitTest)) == nil ||
		strings.Contains(res.Skipped, skippedUnitChecks) {
		t.Errorf("another product: err %v, skipped %q, card %t", err, res.Skipped, cardByKey(s2, o.Key(prJobUnitTest)) != nil)
	}
	// The planner says why it planned no unit card for a watched repository.
	s3, pid3 := unitOnStore(t, UnitChecks{})
	_, _, res, err := s3.PlanPullRequestCards(ctx, pid3, ev)
	if err != nil || !strings.Contains(res.Skipped, skippedUnitChecks) || planned(s3) {
		t.Errorf("a watched repository: err %v, skipped %q, card %t; want unit-checks and no card", err, res.Skipped, planned(s3))
	}
}

// TC-868
func TestUnitChecksRouteAndSection(t *testing.T) {
	s, pid := unitStore(t)
	srv, _ := newTestServer(t)
	srv.store = s
	path := "/products/" + pid + "/autofix/unit"
	on := "enabled=on&bases=release%2Fnext&dailyCap=3"
	autoFixAdminsFile(t, 0o600, "admin@example.test")
	if w := formPost(t, srv, path, on, false); w.Code != http.StatusUnauthorized {
		t.Errorf("no session answered %d; want 401", w.Code)
	}
	if w := formPost(t, srv, path, on, true); w.Code != http.StatusForbidden {
		t.Errorf("an account that is not an admin answered %d; want 403", w.Code)
	}
	autoFixAdminsFile(t, 0o600, testEmail)
	for want, c := range map[string]struct {
		body  string
		setup func()
	}{
		"GitHub is not configured; unit checks need it.":          {on, func() { srv.codeSrc, srv.btConsumer = nil, &btConsumer{} }},
		"Brain Town is not connected; no merge would reach GAOS.": {on, func() { srv.codeSrc, srv.btConsumer = fakeCode{}, nil }},
		"The daily cap is 1 to 20.":                               {"dailyCap=many", func() {}},
		"Turn auto-fix on first.": {on, func() {
			srv.codeSrc, srv.btConsumer = fakeCode{}, &btConsumer{}
			a := unitAF()
			a.Enabled = false
			_, _ = s.SetAutoFix(context.Background(), pid, a, "ops", tokenKindFine)
		}},
	} {
		c.setup()
		w := formPost(t, srv, path, c.body, true)
		if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), want) {
			t.Errorf("%q: answered %d %s", want, w.Code, w.Body.String())
		}
		_, _ = s.SetAutoFix(context.Background(), pid, unitAF(), "ops", tokenKindFine)
	}
	if m := masterOf(t, s, pid); m.UnitChecks.Enabled {
		t.Fatal("a refused save turned unit checks on")
	}
	// A save: the setting, and the Auto-fix page filtered to the product with its section open.
	srv.codeSrc, srv.btConsumer = fakeCode{}, &btConsumer{}
	w := formPost(t, srv, path, on, true)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Unit tests (CI) for") ||
		!strings.Contains(w.Body.String(), "Saved. The next merge uses these settings.") {
		t.Fatalf("the save answered %d without the open section", w.Code)
	}
	if u := masterOf(t, s, pid).UnitChecks; !u.Enabled || u.Cap() != 3 || strings.Join(u.Bases, ",") != "release/next" {
		t.Errorf("the route saved %+v", u)
	}
	// The page: each fix-target repository's last watch, and the warning after three no-ci.
	for i := 1; i <= 3; i++ {
		if _, err := s.PlantUnitWatch(context.Background(), pid, UnitWatch{Repo: defRepo, SHA: fmt.Sprintf("%040x", i), Base: "develop",
			State: UnitWatchDone, Outcome: UnitNoCI, DoneAt: int64(1000 + i)}); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/ui/bo/autofix?product="+pid, nil)
	for _, c := range boCookies() {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, defRepo) || !strings.Contains(body, "no-ci") ||
		!strings.Contains(body, "</code> run on recent merges: is the workflow installed?") {
		t.Errorf("the section does not show the last watch and the warning (%d)", rec.Code)
	}
	if strings.Contains(body, "Saved. The next merge") {
		t.Error("a plain read shows the Saved line")
	}
}
