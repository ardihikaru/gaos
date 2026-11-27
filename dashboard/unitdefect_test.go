package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// Unit defects (D-105, docs/designs/release-b4-unit-tests.md §7). The
// repository, branches and tests are invented, as D-085 requires.

func uf(tests ...string) []unitFailure {
	var out []unitFailure
	for _, t := range tests {
		f := "pkg/records/" + strings.ToLower(strings.TrimPrefix(strings.SplitN(t, "/", 2)[0], "Test")) + "_test.go"
		out = append(out, unitFailure{Test: t, File: f})
	}
	return out
}

func ucSHAn(n int) string { return fmt.Sprintf("%040x", n) }

// watchOn plants a pending watch of a merge on base and answers its key.
func watchOn(t *testing.T, s Store, pid, base string, n int, mergedAt int64) string {
	t.Helper()
	w := UnitWatch{Repo: defRepo, PR: n, SHA: ucSHAn(n), Base: base, MergedAt: mergedAt}
	if _, err := s.PlantUnitWatch(context.Background(), pid, w); err != nil {
		t.Fatal(err)
	}
	return unitWatchKey(pid, defRepo, ucSHAn(n))
}

// failOn ends a new watch with a failing check and answers the Result and the watch.
func failOn(t *testing.T, s Store, pid, base string, n int, mergedAt int64, in UnitDefectIn) (Result, UnitWatch) {
	t.Helper()
	key := watchOn(t, s, pid, base, n, mergedAt)
	in.RunID = int64(900 + n)
	res, err := s.FinishUnitWatch(context.Background(), key, UnitFailed, &in, nil)
	if err != nil {
		t.Fatalf("finish %d: %v", n, err)
	}
	return res, res.UnitWatches[0]
}

// passOn ends a new watch with a passing check.
func passOn(t *testing.T, s Store, pid, base string, n int, mergedAt int64) Result {
	t.Helper()
	key := watchOn(t, s, pid, base, n, mergedAt)
	res, err := s.FinishUnitWatch(context.Background(), key, UnitPassed, nil, &UnitPassIn{RunID: int64(900 + n)})
	if err != nil {
		t.Fatalf("pass %d: %v", n, err)
	}
	return res
}

func defectOf(t *testing.T, s Store, pid, key string) Defect {
	t.Helper()
	d, err := s.DefectByKey(context.Background(), pid, key)
	if err != nil {
		t.Fatalf("defect %s: %v", key, err)
	}
	return d
}

// releaseHeld moves a held triage card to READY as a released one would be.
func releaseHeld(t *testing.T, s *MemStore, d Defect) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if p := s.taskByIDLocked(d.TriageTask); p != nil {
		p.BlockReason = ""
	}
	if _, ok := s.moveTaskLocked(d.TriageTask, StatusReady, MoveUnblock); !ok {
		t.Fatalf("the held card %s could not be released", d.TriageTask)
	}
}

// TC-855
func TestUnitDefectRecordDedupAndUpdate(t *testing.T) {
	ctx := context.Background()
	s, pid := unitOnStore(t, UnitChecks{})
	in := UnitDefectIn{Total: 3, Failing: uf("TestQuantity/negative", "TestTotal")}
	res, w := failOn(t, s, pid, "develop", 1, 1000, in)
	if !res.UnitNew || len(res.Defects) != 1 || w.Outcome != UnitFailed || w.State != UnitWatchDone || w.DoneAt == 0 {
		t.Fatalf("the first failure: new %t, %d defects, watch %+v", res.UnitNew, len(res.Defects), w)
	}
	d := res.Defects[0]
	h := unitHash(in.Failing)
	if h != unitHash([]unitFailure{in.Failing[1], in.Failing[0]}) || len(h) != 16 {
		t.Errorf("the hash %q depends on the order", h)
	}
	wantCase := "unit:" + defRepo + "~develop~" + h
	if d.Key != pid+"/"+wantCase || d.CaseID != wantCase || d.Kind != DefectKindUnit || d.AutomationType != "be" ||
		d.State != DefectStateOpen || d.Gen != 1 || d.Title != "Unit tests failing in "+defRepo+" (3)" || d.UnitBase != "develop" ||
		d.UnitSHA != ucSHAn(1) || d.UnitMergedAt != 1000 || d.RunRef != idSHA(ucSHAn(1))+"@901" || d.BaseOverride != "" ||
		strings.Join(d.TargetRepos, ",") != defRepo || w.DefectKey != d.Key {
		t.Errorf("the record is %+v", d)
	}
	wantEx := "TestQuantity/negative — pkg/records/quantity_test.go\nTestTotal — pkg/records/total_test.go\n… and 1 more"
	if d.Excerpt != wantEx || d.UnitTests != "TestQuantity/negative\tpkg/records/quantity_test.go\nTestTotal\tpkg/records/total_test.go" ||
		strings.Join(d.UnitFiles, ",") != "pkg/records/quantity_test.go,pkg/records/total_test.go" {
		t.Errorf("the failing tests read %q / %q / %v", d.Excerpt, d.UnitTests, d.UnitFiles)
	}
	card := cardByKey(s, defectCardKey(d.Key, 1, defectJobTriage))
	if card == nil || card.ID != d.TriageTask || card.Status != StatusBlocked || card.BlockReason != ReasonUnitHeld ||
		!hasLabel(card.Labels, LabelDefectTriage) || card.Role != "triager" {
		t.Fatalf("the triage card is %+v; want it held", card)
	}
	var types []string
	for _, e := range res.NewEvents {
		types = append(types, string(e.Type))
	}
	if strings.Join(types, ",") != "DefectReported,TaskPlanned,TaskMoved" {
		t.Errorf("the events are %v", types)
	}

	// The same failing set at a newer merge: the record follows it, no new card.
	res, w = failOn(t, s, pid, "develop", 2, 2000, UnitDefectIn{Total: 2, Failing: uf("TestTotal", "TestQuantity/negative")})
	d2 := defectOf(t, s, pid, d.Key)
	if w.Outcome != UnitDedup || res.UnitNew || len(res.NewEvents) != 0 || d2.UnitSHA != ucSHAn(2) || d2.UnitMergedAt != 2000 ||
		d2.RunRef != idSHA(ucSHAn(2))+"@902" || d2.TriageTask != d.TriageTask || w.DefectKey != d.Key {
		t.Errorf("a repeat: outcome %s, events %d, record %+v", w.Outcome, len(res.NewEvents), d2)
	}
	// An older merge of the same set moves nothing back.
	_, w = failOn(t, s, pid, "develop", 3, 1500, UnitDefectIn{Total: 2, Failing: uf("TestTotal", "TestQuantity/negative")})
	if d3 := defectOf(t, s, pid, d.Key); w.Outcome != UnitDedup || d3.UnitSHA != ucSHAn(2) {
		t.Errorf("an older repeat moved the record to %s (outcome %s)", d3.UnitSHA, w.Outcome)
	}

	// A different failing set on the same base updates the open defect: the union,
	// newest first, same key and same held card.
	res, w = failOn(t, s, pid, "develop", 4, 3000, UnitDefectIn{Total: 5, Failing: uf("TestRefund", "TestTotal")})
	d4 := defectOf(t, s, pid, d.Key)
	if w.Outcome != UnitUpdated || res.UnitNew || len(res.NewEvents) != 0 || w.DefectKey != d.Key {
		t.Fatalf("a new set: outcome %s, new %t, %d events", w.Outcome, res.UnitNew, len(res.NewEvents))
	}
	if d4.UnitTests != "TestRefund\tpkg/records/refund_test.go\nTestTotal\tpkg/records/total_test.go\nTestQuantity/negative\tpkg/records/quantity_test.go" ||
		d4.Title != "Unit tests failing in "+defRepo+" (5)" || d4.UnitSHA != ucSHAn(4) || d4.TriageTask != d.TriageTask ||
		len(d4.UnitFiles) != 3 || !strings.HasSuffix(d4.Excerpt, "… and 2 more") {
		t.Errorf("the updated record is %+v", d4)
	}
	if c := cardByKey(s, defectCardKey(d.Key, 1, defectJobTriage)); c == nil || c.Status != StatusBlocked || c.BlockReason != ReasonUnitHeld {
		t.Errorf("the update moved the held card: %+v", c)
	}
	if n := len(defectCards(s, pid)); n != 1 {
		t.Errorf("%d defect cards; want the one held card", n)
	}

	// A failed build is its own defect, on the ~build key; its repeat refreshes the summary.
	res, w = failOn(t, s, pid, "develop", 5, 4000, UnitDefectIn{Build: true, Summary: "cannot compile pkg/records"})
	if !res.UnitNew || w.Outcome != UnitFailed {
		t.Fatalf("a build failure: new %t outcome %s", res.UnitNew, w.Outcome)
	}
	b := res.Defects[0]
	if b.CaseID != "unit:"+defRepo+"~develop~build" || b.Title != "The build failed in "+defRepo || b.Excerpt != "cannot compile pkg/records" ||
		b.UnitTests != "" || len(b.UnitFiles) != 0 || unitH8(b) != "build" {
		t.Errorf("the build record is %+v", b)
	}
	_, w = failOn(t, s, pid, "develop", 6, 5000, UnitDefectIn{Build: true, Summary: "cannot compile pkg/orders"})
	if b2 := defectOf(t, s, pid, b.Key); w.Outcome != UnitDedup || b2.Excerpt != "cannot compile pkg/orders" {
		t.Errorf("a build repeat: outcome %s, excerpt %q", w.Outcome, b2.Excerpt)
	}
	// A failing set on another base is another defect.
	if _, err := s.SetUnitChecks(ctx, pid, UnitChecks{Enabled: true, Bases: []string{"release/next"}}, testEmail); err != nil {
		t.Fatal(err)
	}
	res, _ = failOn(t, s, pid, "release/next", 7, 6000, UnitDefectIn{Failing: uf("TestTotal")})
	if !res.UnitNew || res.Defects[0].UnitBase != "release/next" || res.Defects[0].Key == d.Key {
		t.Errorf("another base: new %t, %+v", res.UnitNew, res.Defects)
	}
	// The store checks what the poller screened.
	key := watchOn(t, s, pid, "develop", 8, 7000)
	for name, bad := range map[string]UnitDefectIn{
		"no test":          {},
		"a bad name":       {Failing: []unitFailure{{Test: "Quantity"}}},
		"a bad file":       {Failing: []unitFailure{{Test: "TestA", File: "../etc/passwd"}}},
		"eleven failures":  {Failing: uf("TestA", "TestB", "TestC", "TestD", "TestE", "TestF", "TestG", "TestH", "TestI", "TestJ", "TestK")},
		"a secret-ish one": {Failing: []unitFailure{{Test: "TestA", File: ".ssh/id_rsa"}}},
	} {
		if _, err := s.FinishUnitWatch(ctx, key, UnitFailed, &bad, nil); err == nil {
			t.Errorf("%s was filed", name)
		}
	}
}

// TC-856
func TestUnitDefectGateAndCaps(t *testing.T) {
	ctx := context.Background()
	in := UnitDefectIn{Failing: uf("TestTotal")}
	for slug, mod := range map[string]func(*MemStore, string){
		"off": func(s *MemStore, pid string) {
			s.mu.Lock()
			s.masterPtr(pid).UnitChecks.Enabled = false
			s.mu.Unlock()
		},
		"no-fixer":   func(s *MemStore, pid string) { _, _ = s.SetAssigned(ctx, pid, []string{"a-12"}, true, testEmail) },
		"no-triager": func(s *MemStore, pid string) { _, _ = s.SetAssigned(ctx, pid, []string{"a-13"}, true, testEmail) },
		"autofix-off": func(s *MemStore, pid string) {
			a := unitAF()
			a.Enabled = false
			_, _ = s.SetAutoFix(ctx, pid, a, "ops", tokenKindFine)
		},
		"not-target": func(s *MemStore, pid string) {
			a := unitAF()
			a.AutomationRepos = []string{defRepo}
			_, _ = s.SetAutoFix(ctx, pid, a, "ops", tokenKindFine)
		},
		"defect-cap": func(s *MemStore, pid string) {
			s.mu.Lock()
			now := autoFixNow()
			for i := 0; i < defectMaxNewKeys; i++ {
				k := fmt.Sprintf("%s/DEF-%d", pid, i)
				s.defects[k] = &Defect{Key: k, ProductID: pid, Kind: DefectKindFail, State: DefectStateClosed, FirstSeen: now}
			}
			s.mu.Unlock()
		},
	} {
		s, pid := unitOnStore(t, UnitChecks{})
		mod(s, pid)
		res, w := failOn(t, s, pid, "develop", 1, 1000, in)
		if w.Outcome != "refused:"+slug || len(res.Defects) != 0 || len(res.NewEvents) != 0 || len(defectCards(s, pid)) != 0 {
			t.Errorf("%s: outcome %q, %d defects, %d events", slug, w.Outcome, len(res.Defects), len(res.NewEvents))
		}
	}
	// The daily unit cap counts defects first seen or reopened today.
	s, pid := unitOnStore(t, UnitChecks{DailyCap: 1, Bases: []string{"release/next"}})
	if _, w := failOn(t, s, pid, "develop", 1, 1000, in); w.Outcome != UnitFailed {
		t.Fatalf("the first defect: %s", w.Outcome)
	}
	if _, w := failOn(t, s, pid, "release/next", 2, 1000, in); w.Outcome != "refused:unit-cap" {
		t.Errorf("past the cap: %s, want refused:unit-cap", w.Outcome)
	}
	// An open defect is still updated past the cap: it is not a new one.
	if _, w := failOn(t, s, pid, "develop", 3, 2000, UnitDefectIn{Failing: uf("TestRefund")}); w.Outcome != UnitUpdated {
		t.Errorf("an update past the cap: %s, want updated", w.Outcome)
	}
	// A watch ends once: finishing it again changes nothing.
	key := watchOn(t, s, pid, "develop", 4, 3000)
	if _, err := s.FinishUnitWatch(ctx, key, UnitNoCI, nil, nil); err != nil {
		t.Fatal(err)
	}
	if r, err := s.FinishUnitWatch(ctx, key, UnitPassed, nil, &UnitPassIn{}); err != nil || len(r.UnitWatches) != 0 {
		t.Errorf("a done watch finished again: %+v %v", r, err)
	}
	if _, err := s.FinishUnitWatch(ctx, "p-1/none@x", UnitNoCI, nil, nil); err == nil {
		t.Error("an unknown watch was finished")
	}
}

// TC-857
func TestAPassClosesOrMarksUnitDefects(t *testing.T) {
	ctx := context.Background()
	s, pid := unitOnStore(t, UnitChecks{Bases: []string{"release/next"}})
	res, _ := failOn(t, s, pid, "develop", 1, 1000, UnitDefectIn{Failing: uf("TestTotal")})
	held := res.Defects[0]
	// A pass on another base, or of an older merge, touches nothing.
	if r := passOn(t, s, pid, "release/next", 2, 5000); len(r.Defects) != 0 || r.UnitMarked || r.UnitClosed {
		t.Errorf("a pass on another base touched %+v", r.Defects)
	}
	if r := passOn(t, s, pid, "develop", 3, 900); len(r.Defects) != 0 {
		t.Errorf("an older pass touched %+v", r.Defects)
	}
	// A pass of a newer merge marks a held defect once: one head line, the card stays held.
	r := passOn(t, s, pid, "develop", 4, 2000)
	d := defectOf(t, s, pid, held.Key)
	if !r.UnitMarked || r.UnitClosed || d.State != DefectStateOpen || d.UnitPassedAt != 2000 ||
		!strings.HasPrefix(d.Excerpt, "checks passed at "+idSHA(ucSHAn(4))+"\n") || d.RunRef != idSHA(ucSHAn(4))+"@904" {
		t.Errorf("the first pass: marked %t closed %t, record %+v", r.UnitMarked, r.UnitClosed, d)
	}
	if c := cardByKey(s, defectCardKey(held.Key, 1, defectJobTriage)); c == nil || c.Status != StatusBlocked || c.BlockReason != ReasonUnitHeld {
		t.Errorf("a pass moved the held card: %+v", c)
	}
	r = passOn(t, s, pid, "develop", 5, 3000)
	d = defectOf(t, s, pid, held.Key)
	if r.UnitMarked || strings.Count(d.Excerpt, "checks passed at") != 1 || !strings.HasPrefix(d.Excerpt, "checks passed at "+idSHA(ucSHAn(5))) {
		t.Errorf("a second pass: marked %t, excerpt %q", r.UnitMarked, d.Excerpt)
	}
	// A newer failure of the same set clears the mark.
	failOn(t, s, pid, "develop", 6, 4000, UnitDefectIn{Failing: uf("TestTotal")})
	if d = defectOf(t, s, pid, held.Key); d.UnitPassedAt != 0 || strings.Contains(d.Excerpt, "checks passed") {
		t.Errorf("a newer failure kept the mark: %+v", d)
	}
	// Released, a pass closes it: fixed, its open cards canceled.
	releaseHeld(t, s, d)
	r = passOn(t, s, pid, "develop", 7, 6000)
	d = defectOf(t, s, pid, held.Key)
	if !r.UnitClosed || r.UnitMarked || d.State != DefectStateFixed || d.UnitPassedAt != 6000 {
		t.Errorf("a released defect's pass: closed %t, record %+v", r.UnitClosed, d)
	}
	if c := cardByKey(s, defectCardKey(held.Key, 1, defectJobTriage)); c == nil || c.Status != StatusCanceled {
		t.Errorf("the released card is %+v; want it canceled", c)
	}
	// Seen again, final: reopened, a new generation and a new held card.
	res, w := failOn(t, s, pid, "develop", 8, 7000, UnitDefectIn{Failing: uf("TestTotal")})
	d = defectOf(t, s, pid, held.Key)
	if !res.UnitReopened || w.Outcome != UnitFailed || d.Gen != 2 || !d.Regression || d.State != DefectStateOpen ||
		d.UnitReopenedAt == 0 || d.UnitPassedAt != 0 || d.UnitNoticeSent || d.UnitPassNoticeSent || d.Context.Built {
		t.Errorf("a reopen: reopened %t, outcome %s, record %+v", res.UnitReopened, w.Outcome, d)
	}
	if c := cardByKey(s, defectCardKey(held.Key, 2, defectJobTriage)); c == nil || c.Status != StatusBlocked || c.BlockReason != ReasonUnitHeld {
		t.Errorf("the reopened card is %+v; want it held", c)
	}
	// A build defect closes on a pass; a non-build defect whose tests retention emptied does not.
	releaseHeld(t, s, d)
	failOn(t, s, pid, "develop", 9, 8000, UnitDefectIn{Build: true, Summary: "cannot compile"})
	s.mu.Lock()
	s.defects[held.Key].UnitTests = ""
	s.mu.Unlock()
	bkey := pid + "/unit:" + defRepo + "~develop~build"
	releaseHeld(t, s, defectOf(t, s, pid, bkey))
	r = passOn(t, s, pid, "develop", 10, 9000)
	if b := defectOf(t, s, pid, bkey); b.State != DefectStateFixed || !r.UnitClosed {
		t.Errorf("the build defect after a pass: %s", b.State)
	}
	if d = defectOf(t, s, pid, held.Key); defectFinal(d.State) {
		t.Errorf("a defect with no tests left was pass-closed: %s", d.State)
	}
	_ = ctx
}

// TC-858
func TestAnOlderFailureNeverFilesWhatAPassCovered(t *testing.T) {
	s, pid := unitOnStore(t, UnitChecks{})
	// A newer merge passed, no defect exists yet: the older failure files nothing.
	passOn(t, s, pid, "develop", 1, 2000)
	res, w := failOn(t, s, pid, "develop", 2, 1000, UnitDefectIn{Failing: uf("TestTotal")})
	if w.Outcome != "refused:older" || len(res.Defects) != 0 || len(defectCards(s, pid)) != 0 {
		t.Errorf("an older failure: outcome %s, %d defects", w.Outcome, len(res.Defects))
	}
	// A failure newer than every pass files.
	res, w = failOn(t, s, pid, "develop", 3, 3000, UnitDefectIn{Failing: uf("TestTotal")})
	if w.Outcome != UnitFailed || !res.UnitNew {
		t.Fatalf("a newer failure: outcome %s", w.Outcome)
	}
	// A final record whose pass is newer refuses an older failure even when the pass watch is gone.
	d := res.Defects[0]
	releaseHeld(t, s, d)
	passOn(t, s, pid, "develop", 4, 4000)
	s.mu.Lock()
	for k, x := range s.unitWatches {
		if x.Outcome == UnitPassed {
			delete(s.unitWatches, k)
		}
	}
	s.mu.Unlock()
	if _, w = failOn(t, s, pid, "develop", 5, 3500, UnitDefectIn{Failing: uf("TestTotal")}); w.Outcome != "refused:older" {
		t.Errorf("a failure older than the record's pass: %s", w.Outcome)
	}
	if d = defectOf(t, s, pid, d.Key); d.State != DefectStateFixed || d.Gen != 1 {
		t.Errorf("the closed record moved: %+v", d)
	}
}

// TC-859
func TestUnitDefectFieldsSurviveARestart(t *testing.T) {
	arc := arcadeTestTarget(t)
	ctx := t.Context()
	tenant := "conf-unit-defect"
	first := newArcadeTestStore(t, arc, tenant)
	pid := "p-1"
	repo := "loop/laundry-api"
	if _, err := first.SetAutoFix(ctx, pid, AutoFix{Enabled: true, TargetRepos: []string{repo}}, "ops", tokenKindFine); err != nil {
		t.Fatal(err)
	}
	if _, err := first.SetAssigned(ctx, pid, []string{"a-12", "a-13"}, true, testEmail); err != nil {
		t.Fatal(err)
	}
	if _, err := first.SetUnitChecks(ctx, pid, UnitChecks{Enabled: true}, testEmail); err != nil {
		t.Fatal(err)
	}
	plant := func(n int, mergedAt int64) string {
		w := UnitWatch{Repo: repo, PR: n, SHA: ucSHAn(n), Base: "develop", MergedAt: mergedAt}
		if _, err := first.PlantUnitWatch(ctx, pid, w); err != nil {
			t.Fatal(err)
		}
		return unitWatchKey(pid, repo, ucSHAn(n))
	}
	in := UnitDefectIn{Total: 4, RunID: 77, Failing: []unitFailure{{Test: "TestTotal", File: "pkg/records/total_test.go"}, {Test: "ExampleTotal"}}}
	res, err := first.FinishUnitWatch(ctx, plant(1, 1000), UnitFailed, &in, nil)
	if err != nil || len(res.Defects) != 1 {
		t.Fatalf("finish: %+v %v", res.Defects, err)
	}
	key := res.Defects[0].Key
	if _, err := first.MarkUnitNotice(ctx, pid, key, UnitNoticeHeld); err != nil {
		t.Fatal(err)
	}
	if _, err := first.FinishUnitWatch(ctx, plant(2, 2000), UnitPassed, nil, &UnitPassIn{RunID: 78}); err != nil {
		t.Fatal(err)
	}
	want := defectOf(t, first, pid, key)
	second, err := NewArcadeStoreFor(ctx, arc, 1, tenant)
	if err != nil {
		t.Fatal(err)
	}
	got := defectOf(t, second, pid, key)
	if got.Kind != DefectKindUnit || got.UnitBase != "develop" || got.UnitSHA != ucSHAn(1) || got.UnitMergedAt != 1000 ||
		got.UnitPassedAt != 2000 || got.UnitTests != want.UnitTests || strings.Join(got.UnitFiles, ",") != "pkg/records/total_test.go" ||
		!got.UnitNoticeSent || got.UnitPassNoticeSent || got.Excerpt != want.Excerpt || got.RunRef != want.RunRef ||
		got.TriageTask != want.TriageTask || got.CaseID != want.CaseID || got.Title != want.Title {
		t.Errorf("after a reopen %+v\nwant %+v", got, want)
	}
	cards, _ := second.PRCards(ctx, pid)
	held := false
	for _, c := range cards {
		held = held || (c.ID == got.TriageTask && c.Status == StatusBlocked && c.BlockReason == ReasonUnitHeld)
	}
	if !held {
		t.Errorf("the held triage card did not come back held: %+v", cards)
	}
	ws, _ := second.UnitWatches(ctx, pid)
	outcomes := map[string]string{}
	for _, w := range ws {
		outcomes[w.SHA] = w.Outcome + "/" + w.DefectKey
	}
	if outcomes[ucSHAn(1)] != UnitFailed+"/"+key || outcomes[ucSHAn(2)] != UnitPassed+"/"+key {
		t.Errorf("the watches came back as %v", outcomes)
	}
}
