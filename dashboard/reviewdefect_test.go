package main

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
)

// Review defects (D-102 §6, §7). The product, repository and branch names are
// invented, as D-085 requires.

const (
	rvBranch = "feature/route-limits"
	rvHead   = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	rvHead2  = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func reviewAF() AutoFix {
	af := fixAF()
	af.TargetRepos = []string{defRepo}
	af.ReviewFixHeads = []string{"feature/*"}
	return af
}

// reviewStore is a product that may plan review fixes: a fixer assigned,
// auto-fix on, the branch pattern and the target set.
func reviewStore(t *testing.T) (*MemStore, string) {
	t.Helper()
	s, pid := qaFixtureRepo(t)
	ctx := context.Background()
	if _, err := s.SetAutoFix(ctx, pid, reviewAF(), "ops", tokenKindFine); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetAssigned(ctx, pid, []string{"a-10", "a-13"}, true, testEmail); err != nil {
		t.Fatal(err)
	}
	return s, pid
}

func reviewIn(n int) ReviewDefectIn {
	return ReviewDefectIn{Repo: defRepo, Number: n, Branch: rvBranch, HeadSHA: rvHead, Files: []string{fxFile},
		Excerpt: "pkg/records/validate.go:4 — a zero check lets -1 through. Reason: the quantity must be positive.",
		Context: DefectContext{Built: true, Repos: []DefectContextRepo{{Repo: defRepo, Head: rvHead}},
			Files: []DefectContextFile{{Repo: defRepo, Path: fxFile, SHA: "b-" + fxFile, Text: fxText}}}}
}

func rvOpenPR(head string) ghPR {
	var pr ghPR
	pr.State, pr.ChangedFiles = "open", 1
	pr.User.Login = "someone"
	pr.Head.Ref, pr.Head.SHA = rvBranch, head
	pr.Head.Repo.FullName, pr.Base.Repo.FullName = defRepo, defRepo
	pr.Base.Ref = "develop"
	return pr
}

// TC-811
func TestLowReviewPlansAReviewDefectOnlyWhenBounded(t *testing.T) {
	ctx := context.Background()
	s, pid := reviewStore(t)
	res, err := s.PlanReviewDefect(ctx, pid, reviewIn(41))
	if err != nil || res.Skipped != "" || len(res.Defects) != 1 {
		t.Fatalf("plan: skipped %q err %v", res.Skipped, err)
	}
	d := res.Defects[0]
	key := pid + "/review:" + defRepo + "#41"
	if d.Key != key || d.Kind != DefectKindReview || d.Gen != 1 || d.AutomationType != "be" || d.State != DefectStateTriaged ||
		d.Difficulty != "easy" || d.ReviewBranch != rvBranch || d.ReviewHead != rvHead || d.BaseOverride != "" ||
		d.FirstSeen == 0 || d.LastSeen == 0 || d.FixTask == "" || d.TriageTask != "" || d.Regression {
		t.Errorf("record %+v", d)
	}
	card := cardByKey(s, defectCardKey(key, 1, "fix"))
	if card == nil || card.ID != d.FixTask || card.Role != "fixer" || !hasLabel(card.Labels, LabelDefectEasy) || card.Status != StatusPending {
		t.Fatalf("fix card %+v", card)
	}
	var types []string
	for _, e := range res.NewEvents {
		types = append(types, string(e.Type))
	}
	if strings.Join(types, ",") != "DefectReported,DefectTriaged,TaskPlanned" {
		t.Errorf("events %v", types)
	}
	// One per pull request.
	if again, _ := s.PlanReviewDefect(ctx, pid, reviewIn(41)); again.Skipped == "" {
		t.Error("a second plan for the same pull request planned again")
	}

	// Each condition of §6.1 alone.
	cond := map[string]func(*MemStore){
		"; no fixer assigned": func(s *MemStore) { _, _ = s.SetAssigned(ctx, pid, []string{"a-10"}, true, testEmail) },
		"; auto-fix off": func(s *MemStore) {
			a := reviewAF()
			a.Enabled = false
			_, _ = s.SetAutoFix(ctx, pid, a, "ops", tokenKindFine)
		},
		"; branch not allowed for review fixes": func(s *MemStore) {
			a := reviewAF()
			a.ReviewFixHeads = []string{"bugfix/*"}
			_, _ = s.SetAutoFix(ctx, pid, a, "ops", tokenKindFine)
		},
		"; repository not a fix target": func(s *MemStore) {
			a := reviewAF()
			a.AutomationRepos = []string{defRepo}
			_, _ = s.SetAutoFix(ctx, pid, a, "ops", tokenKindFine)
		},
	}
	for want, mod := range cond {
		s, _ := reviewStore(t)
		mod(s)
		if r, err := s.PlanReviewDefect(ctx, pid, reviewIn(50)); err != nil || r.Skipped != want {
			t.Errorf("%s: skipped %q err %v", want, r.Skipped, err)
		}
	}
	// A burst of 10 lows with cap 3 plans 3.
	s2, _ := reviewStore(t)
	planned := 0
	for i := 0; i < 10; i++ {
		if r, _ := s2.PlanReviewDefect(ctx, pid, reviewIn(60+i)); r.Skipped == "" {
			planned++
		} else if r.Skipped != "; review fix cap reached" {
			t.Errorf("burst %d: %q", i, r.Skipped)
		}
	}
	if planned != 3 {
		t.Errorf("a burst of 10 planned %d, want the cap of 3", planned)
	}
	// The branch-pattern rule.
	for p, ok := range map[string]bool{"feature/*": true, "fix/**": true, "*": false, "**": false, "staging": false,
		"*/x": false, "feature/../main": false, "feature/": false} {
		if reviewFixHeadOK(p) != ok {
			t.Errorf("reviewFixHeadOK(%q) = %t", p, !ok)
		}
	}
	a := reviewAF()
	a.ReviewFixHeads = []string{"*"}
	if err := validateAutoFix(a, tokenKindFine); err == nil {
		t.Error("validateAutoFix accepted the pattern *")
	}
	// A review defect never arrives from the QA tool.
	it := defItem(DefectKindReview, "DEF-900", defCase1)
	if why := validateDefectItem(it, it.RunAt); why == "" {
		t.Error("the QA intake accepted kind review")
	}
}

// TC-811, ArcadeDB: the record and its two new properties survive a reopen.
func TestReviewDefectSurvivesARestart(t *testing.T) {
	arc := arcadeTestTarget(t)
	ctx := t.Context()
	tenant := "conf-review-defect"
	first := newArcadeTestStore(t, arc, tenant)
	ms, _ := first.Masters(ctx)
	m := ms[0]
	if _, err := first.SetAutoFix(ctx, m.ID, reviewAF(), "ops", tokenKindFine); err != nil {
		t.Fatal(err)
	}
	if _, err := first.SetAssigned(ctx, m.ID, []string{"a-13"}, true, testEmail); err != nil {
		t.Fatal(err)
	}
	if r, err := first.PlanReviewDefect(ctx, m.ID, reviewIn(77)); err != nil || r.Skipped != "" {
		t.Fatalf("plan: %q %v", r.Skipped, err)
	}
	second, err := NewArcadeStoreFor(ctx, arc, 1, tenant)
	if err != nil {
		t.Fatal(err)
	}
	d, err := second.DefectByKey(ctx, m.ID, m.ID+"/review:"+defRepo+"#77")
	if err != nil || d.Kind != DefectKindReview || d.ReviewBranch != rvBranch || d.ReviewHead != rvHead || d.Gen != 1 {
		t.Errorf("after a reopen %+v err %v", d, err)
	}
}

// fakeCode is a codeSource that answers files at any sha.
type fakeCode struct{ files map[string]string }

func (f fakeCode) defaultHead(context.Context, string, string, string) (string, error) {
	return rvHead, nil
}
func (f fakeCode) searchCode(context.Context, string, string, string) ([]ghCodeHit, error) {
	return nil, nil
}
func (f fakeCode) dirAt(context.Context, string, string, string, string) ([]string, error) {
	return nil, nil
}
func (f fakeCode) fileAt(_ context.Context, _, _, p, _ string) (ghBlob, bool, error) {
	t, ok := f.files[p]
	return ghBlob{SHA: "b-" + p, Text: t}, ok, nil
}

// TC-812
func TestReviewDefectPinsTheReviewedHead(t *testing.T) {
	ctx := context.Background()
	key := "loop/laundry-api#9:" + prJobReviewer
	card := TaskRecord{ID: "c-1", DeskID: "p-1", DedupKey: key}
	files := []ghFile{{Filename: fxFile, Status: "modified"}}
	run := func(mod func(*prContextCache, *fakeReader, *ghPR)) string {
		s, pid := reviewStore(t)
		card.DeskID = pid
		cache := &prContextCache{}
		cache.put(key, prContext{HeadSHA: rvHead})
		rd := &fakeReader{files: files}
		pr := rvOpenPR(rvHead)
		if mod != nil {
			mod(cache, rd, &pr)
		}
		p := &prReviewer{st: s, rd: rd, cache: cache, src: fakeCode{files: map[string]string{fxFile: fxText}}, logf: func(string, ...any) {}}
		return p.planLowFix(ctx, card, "loop", "laundry-api", 9, pr, "x")
	}
	// The repo of the card key is loop/laundry-api; the fixture's target is defRepo, so
	// the planner refuses it as not a target unless the repo is defRepo.
	if got := run(nil); got != "; repository not a fix target" {
		t.Fatalf("a non-target repository: %q", got)
	}
	owner, repo, _ := ghOwnerRepo(defRepo)
	key = owner + "/" + repo + "#9:" + prJobReviewer
	card.DedupKey = key
	run2 := func(mod func(*prContextCache, *fakeReader, *ghPR)) string {
		s, pid := reviewStore(t)
		card.DeskID = pid
		cache := &prContextCache{}
		cache.put(key, prContext{HeadSHA: rvHead})
		rd := &fakeReader{files: files}
		pr := rvOpenPR(rvHead)
		if mod != nil {
			mod(cache, rd, &pr)
		}
		p := &prReviewer{st: s, rd: rd, cache: cache, src: fakeCode{files: map[string]string{fxFile: fxText}}, logf: func(string, ...any) {}}
		return p.planLowFix(ctx, card, owner, repo, 9, pr, "x")
	}
	if got := run2(nil); !strings.HasPrefix(got, "; review fix planned (") {
		t.Errorf("the good case answered %q", got)
	}
	for want, mod := range map[string]func(*prContextCache, *fakeReader, *ghPR){
		"; review context lost":     func(c *prContextCache, _ *fakeReader, _ *ghPR) { *c = prContextCache{} },
		"; head moved since review": func(_ *prContextCache, _ *fakeReader, pr *ghPR) { pr.Head.SHA = rvHead2 },
		"; file list incomplete":    func(_ *prContextCache, _ *fakeReader, pr *ghPR) { pr.ChangedFiles = 2 },
		"; workflows changed": func(_ *prContextCache, rd *fakeReader, pr *ghPR) {
			rd.files = append(rd.files, ghFile{Filename: ".github/workflows/ci.yml"})
			pr.ChangedFiles = 2
		},
		"; files unreadable": func(_ *prContextCache, rd *fakeReader, _ *ghPR) { rd.filesErr = errors.New("boom") },
	} {
		if got := run2(mod); got != want {
			t.Errorf("want %q, got %q", want, got)
		}
	}
	// A removed file is never read, and the slice holds exactly the files that landed.
	if got := reviewFilesFor([]ghFile{{Filename: "b.go", Status: "removed"}, {Filename: "a.go"}}); strings.Join(got, ",") != "a.go" {
		t.Errorf("files %v", got)
	}
}

// TC-813
func TestReviewFixRefusals(t *testing.T) {
	m := Master{ID: "p-1", AssignedSet: true, Assigned: []string{"a-13"}, AutoFix: reviewAF()}
	d := Defect{Kind: DefectKindReview, Repo: defRepo, ReviewBranch: rvBranch, ReviewHead: rvHead}
	files := []ghFile{{Filename: fxFile}}
	if why := reviewFixRefusal(d, rvOpenPR(rvHead), files, "main", false, m); why != "" {
		t.Fatalf("the good case refused %q", why)
	}
	for want, mod := range map[string]func(*ghPR, *[]ghFile, *string, *bool, *Master){
		ReviewPRClosed:     func(pr *ghPR, _ *[]ghFile, _ *string, _ *bool, _ *Master) { pr.State = "closed" },
		ReviewBaseChanged:  func(pr *ghPR, _ *[]ghFile, _ *string, _ *bool, _ *Master) { pr.Head.Ref = "feature/other" },
		ReviewBaseMoved:    func(pr *ghPR, _ *[]ghFile, _ *string, _ *bool, _ *Master) { pr.Head.SHA = rvHead2 },
		ReviewFork:         func(pr *ghPR, _ *[]ghFile, _ *string, _ *bool, _ *Master) { pr.Head.Repo.FullName = "stranger/fork" },
		ReviewSharedBranch: func(_ *ghPR, _ *[]ghFile, _ *string, p *bool, _ *Master) { *p = true },
		ReviewBranchNotAllow: func(_ *ghPR, _ *[]ghFile, _ *string, _ *bool, m *Master) {
			m.AutoFix.ReviewFixHeads = []string{"bugfix/*"}
		},
		ReviewFilesIncomplete: func(pr *ghPR, _ *[]ghFile, _ *string, _ *bool, _ *Master) { pr.ChangedFiles = 3 },
		ReviewWorkflows: func(pr *ghPR, f *[]ghFile, _ *string, _ *bool, _ *Master) {
			*f = append(*f, ghFile{Filename: ".github/workflows/x.yml"})
			pr.ChangedFiles = 2
		},
		ReviewNotTarget:  func(_ *ghPR, _ *[]ghFile, _ *string, _ *bool, m *Master) { m.AutoFix.TargetRepos = nil },
		ReviewUnassigned: func(_ *ghPR, _ *[]ghFile, _ *string, _ *bool, m *Master) { m.Assigned = []string{"a-10"} },
	} {
		pr, fs, def, prot, mm := rvOpenPR(rvHead), append([]ghFile(nil), files...), "main", false, m
		mm.AutoFix = m.AutoFix.Clone()
		mod(&pr, &fs, &def, &prot, &mm)
		if why := reviewFixRefusal(d, pr, fs, def, prot, mm); why != want {
			t.Errorf("want %s, got %q", want, why)
		}
	}
	// The default branch and the auto-fix base are shared branches too.
	if why := reviewFixRefusal(d, rvOpenPR(rvHead), files, rvBranch, false, m); why != ReviewSharedBranch {
		t.Errorf("the default branch: %q", why)
	}

	// Check point 1: autoFixPass refuses before any model turn, closes the record
	// first and leaves the card Blocked with the refusal as its reason.
	ctx := context.Background()
	s, pid := reviewStore(t)
	res, _ := s.PlanReviewDefect(ctx, pid, reviewIn(88))
	d1 := res.Defects[0]
	setStatus(s, d1.FixTask, StatusReady)
	f := newFakeGHW()
	f.pull = map[string]any{"state": "closed", "merged": true, "changed_files": 1,
		"head": map[string]any{"ref": rvBranch, "sha": rvHead, "repo": map[string]string{"full_name": defRepo}},
		"base": map[string]any{"ref": "develop", "repo": map[string]string{"full_name": defRepo}}}
	f.pullFiles = []map[string]any{{"filename": fxFile, "status": "modified"}}
	w := newFakeWriter(t, f)
	if n := autoFixPass(ctx, s, w.c); n != 0 {
		t.Errorf("autoFixPass dispatched %d", n)
	}
	got := cardByKey(s, defectCardKey(d1.Key, 1, "fix"))
	if got.Status != StatusBlocked || got.BlockReason != ReviewPRClosed {
		t.Errorf("card %s %q, want BLOCKED %s", got.Status, got.BlockReason, ReviewPRClosed)
	}
	if rec, _ := s.DefectByKey(ctx, pid, d1.Key); rec.State != DefectStateClosed {
		t.Errorf("the record is %s, want closed", rec.State)
	}
	// A second pass inside the throttle window makes no GitHub call.
	setStatus(s, d1.FixTask, StatusReady)
	before := len(f.calls)
	autoFixPass(ctx, s, w.c)
	if len(f.calls) != before {
		t.Errorf("a pass inside the throttle window made %d GitHub call(s)", len(f.calls)-before)
	}
}

// reviewFixEnv is a review defect whose fix card has run, with a fake GitHub
// that holds the reviewed branch at the pinned head.
func reviewFixEnv(t *testing.T, mod func(f *fakeGHW)) (*fixEnv, Defect, *fakePoster2) {
	t.Helper()
	ctx := context.Background()
	s, pid := reviewStore(t)
	res, err := s.PlanReviewDefect(ctx, pid, reviewIn(91))
	if err != nil || len(res.Defects) != 1 {
		t.Fatal(res.Skipped, err)
	}
	d := res.Defects[0]
	setStatus(s, d.FixTask, StatusReview)
	f := newFakeGHW()
	f.refs[rvBranch] = rvHead
	f.commits[rvHead] = ghCommit{Message: "reviewed", Tree: "tree:"}
	f.pull = map[string]any{"state": "open", "changed_files": 1,
		"head": map[string]any{"ref": rvBranch, "sha": rvHead, "repo": map[string]string{"full_name": defRepo}},
		"base": map[string]any{"ref": "develop", "repo": map[string]string{"full_name": defRepo}}}
	f.pullFiles = []map[string]any{{"filename": fxFile, "status": "modified"}}
	if mod != nil {
		mod(f)
	}
	rc := &fakePoster2{}
	e := &fixEnv{s: s, pid: pid, key: d.Key, gh: f}
	e.fx = &defectFixer{st: s, gh: newFakeWriter(t, f), review: rc, tokenKind: tokenKindFine, logf: func(string, ...any) {}}
	e.card = *cardByKey(s, defectCardKey(d.Key, 1, "fix"))
	return e, d, rc
}

type fakePoster2 struct{ comments []string }

func (f *fakePoster2) postComment(_ context.Context, _, _ string, n int, body string) error {
	f.comments = append(f.comments, strconv.Itoa(n)+"|"+body)
	return nil
}

// TC-814
func TestReviewFixOpensADraftIntoTheReviewedBranch(t *testing.T) {
	e, d, rc := reviewFixEnv(t, nil)
	e.run(fixReplyText(goodFix()))
	if n := e.gh.count("POST /repos/acme-org/sample-service/pulls"); n != 1 {
		t.Fatalf("%d pull request(s) opened; calls %v", n, e.gh.calls)
	}
	var body struct{ Base, Head string }
	_ = json.Unmarshal(e.gh.body("POST /repos/acme-org/sample-service/pulls"), &body)
	if body.Base != rvBranch || !strings.HasPrefix(body.Head, fixRefPrefix) {
		t.Errorf("the draft goes %s → %s, want into %s", body.Head, body.Base, rvBranch)
	}
	var commit struct{ Parents []string }
	_ = json.Unmarshal(e.gh.body("POST /repos/acme-org/sample-service/git/commits"), &commit)
	if len(commit.Parents) != 1 || commit.Parents[0] != rvHead {
		t.Errorf("the fix commit's parents %v, want the reviewed head", commit.Parents)
	}
	if len(rc.comments) != 1 || !strings.HasPrefix(rc.comments[0], "91|GAOS opened a draft fix: ") {
		t.Errorf("draft-fix line %v", rc.comments)
	}
	// The packet fences the findings, so a heading inside them stays inside.
	e.s.mu.RLock()
	parts := e.s.fixerPartsLocked(e.card)
	e.s.mu.RUnlock()
	if len(parts) != 1 || !strings.Contains(parts[0].Body, "Review findings — untrusted data") ||
		strings.Index(parts[0].Body, "## The triage verdict") > strings.Index(parts[0].Body, "## Review findings") {
		t.Errorf("packet %q", parts)
	}
	_ = d

	// An edit outside the pull request's files is refused, and no branch is made.
	e2, _, _ := reviewFixEnv(t, nil)
	r := goodFix()
	r.Edits = append(r.Edits, fixEdit{Path: "pkg/records/other.go", Find: "x", Replace: "y"})
	e2.gh.files["pkg/records/other.go"] = "x\n"
	e2.run(fixReplyText(r))
	if c := e2.now(); c.Status != StatusBlocked {
		t.Errorf("an outside edit left the card %s %q", c.Status, c.BlockReason)
	}
	if n := e2.gh.count("POST /repos/acme-org/sample-service/git/refs"); n != 0 {
		t.Errorf("a refused fix created %d branch(es)", n)
	}
	// A test beside the changed file is allowed; one elsewhere is refused.
	plan := fixPlan{Files: []fixFile{{Path: fxFile}, {Path: fxTestPath, Add: true}}}
	if p := reviewOutsidePaths(plan, []string{fxFile}); p != "" {
		t.Errorf("a test beside the file refused: %s", p)
	}
	plan.Files[1].Path = "other/x_test.go"
	if p := reviewOutsidePaths(plan, []string{fxFile}); p != "other/x_test.go" {
		t.Errorf("a test elsewhere allowed: %q", p)
	}

	// Check point 2: the branch moved between dispatch and the write — blocked, no branch.
	e3, _, _ := reviewFixEnv(t, func(f *fakeGHW) { f.refs[rvBranch] = rvHead2; f.commits[rvHead2] = ghCommit{Tree: "tree:"} })
	e3.run(fixReplyText(goodFix()))
	if c := e3.now(); c.Status != StatusBlocked || c.BlockReason != ReviewBaseMoved {
		t.Errorf("a moved head left the card %s %q", c.Status, c.BlockReason)
	}
	if n := e3.gh.count("POST /repos/acme-org/sample-service/git/refs"); n != 0 {
		t.Errorf("a moved head created %d branch(es)", n)
	}
	if rec := e3.defect(); rec.State != DefectStateClosed {
		t.Errorf("a moved head left the record %s", rec.State)
	}
}

// TC-815 and TC-807
func TestReviewFixLifecycle(t *testing.T) {
	ctx := context.Background()
	owner, repo, _ := ghOwnerRepo(defRepo)
	merged := PREvent{Type: prTypeMerged, Org: owner, Repo: repo, PRNumber: 92}

	// A merge before dispatch: only a GitHub-confirmed merge closes it.
	s, pid := reviewStore(t)
	res, _ := s.PlanReviewDefect(ctx, pid, reviewIn(92))
	d := res.Defects[0]
	if _, _, _, err := s.PlanPullRequestCards(ctx, pid, merged); err != nil {
		t.Fatal(err)
	}
	if rec, _ := s.DefectByKey(ctx, pid, d.Key); rec.State != DefectStateTriaged {
		t.Errorf("an unconfirmed merge moved the record to %s", rec.State)
	}
	merged.MergeChecked = true
	merged.EventID = "gh-2"
	if _, _, r, err := s.PlanPullRequestCards(ctx, pid, merged); err != nil || len(r.Defects) != 1 {
		t.Fatalf("confirmed merge: %v %d", err, len(r.Defects))
	}
	if rec, _ := s.DefectByKey(ctx, pid, d.Key); rec.State != DefectStateClosed {
		t.Errorf("a confirmed merge left the record %s", rec.State)
	}
	if c := cardByKey(s, defectCardKey(d.Key, 1, "fix")); c.Status != StatusCanceled {
		t.Errorf("the fix card is %s after the merge", c.Status)
	}

	// The draft is open; its base changes: recorded, the record closed, nothing closed on GitHub.
	s2, _ := reviewStore(t)
	res, _ = s2.PlanReviewDefect(ctx, pid, reviewIn(93))
	d2 := res.Defects[0]
	fp := FixPR{ID: "fx-93", ProductID: pid, DefectKey: d2.Key, Repo: defRepo, Number: 120, State: FixPRDraft,
		Base: rvBranch, Branch: fixBranch(d2), TaskID: d2.FixTask}
	if _, err := s2.UpsertFixPR(ctx, fp); err != nil {
		t.Fatal(err)
	}
	moved := fp
	moved.Base = "develop"
	if _, err := s2.AdvanceFixPR(ctx, pid, moved); err != nil {
		t.Fatal(err)
	}
	if rec, _ := s2.DefectByKey(ctx, pid, d2.Key); rec.State != DefectStateClosed {
		t.Errorf("a base change left the record %s", rec.State)
	}
	// GAOS's own review fix merging into the feature branch plans nothing (TC-807).
	own := PREvent{Type: prTypeMerged, Org: owner, Repo: repo, PRNumber: 120, MergeChecked: true}
	if planned, _, r, err := s2.PlanPullRequestCards(ctx, pid, own); err != nil || len(planned) != 0 || r.Skipped != "own-fix" {
		t.Errorf("own fix merge planned %v skipped %q err %v", planned, r.Skipped, err)
	}

	// A review fix draft closed unmerged closes the record, with no new generation.
	s3, _ := reviewStore(t)
	res, _ = s3.PlanReviewDefect(ctx, pid, reviewIn(94))
	d3 := res.Defects[0]
	s3.mu.Lock()
	s3.defects[d3.Key].State = DefectStateFixing
	s3.mu.Unlock()
	fp3 := FixPR{ID: "fx-94", ProductID: pid, DefectKey: d3.Key, Repo: defRepo, Number: 121, State: FixPRDraft,
		Base: rvBranch, Branch: fixBranch(d3), TaskID: d3.FixTask}
	if _, err := s3.UpsertFixPR(ctx, fp3); err != nil {
		t.Fatal(err)
	}
	closedPR := fp3
	closedPR.State = FixPRClosed
	if _, err := s3.AdvanceFixPR(ctx, pid, closedPR); err != nil {
		t.Fatal(err)
	}
	if rec, _ := s3.DefectByKey(ctx, pid, d3.Key); rec.State != DefectStateClosed || rec.Gen != 1 {
		t.Errorf("a closed review fix left %s gen %d", rec.State, rec.Gen)
	}
	// A review fix is never retargeted.
	s3.mu.Lock()
	s3.defects[d3.Key].State = DefectStateFixing
	s3.defects[d3.Key].FixPR = "fx-94"
	fx := s3.fixPRs["fx-94"]
	fx.State = FixPRDraft
	s3.fixPRs["fx-94"] = fx
	s3.mu.Unlock()
	if _, err := s3.RetargetFixPR(ctx, pid, "fx-94", "develop", testEmail, tokenKindFine); err == nil ||
		!strings.Contains(err.Error(), "cannot be retargeted") {
		t.Errorf("a retarget answered %v", err)
	}

	// A D-091 fix pull request's merge still plans its unit-test card (TC-807).
	s4, _ := qaFixtureRepo(t)
	report(t, s4, pid, defItem(DefectKindXfail, "DEF-101", defCase1))
	if _, err := s4.UpsertFixPR(ctx, FixPR{ID: "fx-x", ProductID: pid, DefectKey: pid + "/DEF-101", Repo: defRepo, Number: 130,
		State: FixPRDraft}); err != nil {
		t.Fatal(err)
	}
	if planned, _, r, err := s4.PlanPullRequestCards(ctx, pid, PREvent{Type: prTypeMerged, Org: owner, Repo: repo, PRNumber: 130}); err != nil ||
		len(planned) != 1 || r.Skipped != "" {
		t.Errorf("a D-091 fix merge planned %v skipped %q err %v, want its unit-test card", planned, r.Skipped, err)
	}
}
