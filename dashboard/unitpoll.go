package main

import (
	"context"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// The unit pass (D-105, docs/designs/release-b4-unit-tests.md §5): the poller
// reads the check of each due watch -- the push run of the configured workflow
// on the merge commit, and the check run of that run's check suite -- and ends
// the watch with its outcome. Nothing runs on GAOS's hosts. A missing, unread
// or inconclusive check is never a pass.

// The pass limits (§5.1).
const (
	unitPassEvery   = 4  // every fourth poller pass
	unitPassBudget  = 10 // watches read per pass
	unitMaxTries    = 20 // failed reads before a watch ends inconclusive
	unitTotalMax    = 100000
	unitSummaryMax  = 2 << 10
	unitCtxRetry    = 30 * time.Minute
	unitCtxMaxDirs  = 2
	unitActionsSlug = "github-actions"
	unitTotalTitle  = "unit-tests-total"
)

// unitWait is how long a pending watch waits before its next read.
func unitWait(age time.Duration) time.Duration {
	switch {
	case age < unitFastWindow:
		return unitWaitFast
	case age < unitMidWindow:
		return unitWaitMid
	}
	return unitWaitSlow
}

// unitPass is one pass over the due watches, at most unitPassBudget of them, one
// product at a time in round-robin: it starts after the product the last pass
// served. Every product's held defects are scanned for a notice or a context
// still to be sent or built. It answers how many watches it read.
func (p *fixPRPoller) unitPass(ctx context.Context, now time.Time) int {
	if p.gh == nil {
		return 0
	}
	masters, err := p.st.Masters(ctx)
	if err != nil {
		return 0
	}
	sort.Slice(masters, func(i, j int) bool { return masters[i].ID < masters[j].ID })
	p.mu.Lock()
	last := p.unitLast
	p.mu.Unlock()
	start := 0
	for i, m := range masters {
		if m.ID > last {
			start = i
			break
		}
		if i == len(masters)-1 {
			start = 0
		}
	}
	budget, read := unitPassBudget, 0
	for i := range masters {
		if ctx.Err() != nil {
			return read
		}
		m := masters[(start+i)%len(masters)]
		if budget > 0 {
			ws, _ := p.st.UnitWatches(ctx, m.ID)
			var due []UnitWatch
			for _, w := range ws {
				if w.State == UnitWatchPending && w.NextAt <= now.Unix() {
					due = append(due, w)
				}
			}
			sort.Slice(due, func(a, b int) bool {
				if due[a].NextAt != due[b].NextAt {
					return due[a].NextAt < due[b].NextAt
				}
				return due[a].Key < due[b].Key
			})
			for _, w := range due {
				if budget == 0 {
					break
				}
				budget--
				read++
				p.unitCheck(ctx, m, w, now)
			}
			if len(due) > 0 {
				p.mu.Lock()
				p.unitLast = m.ID
				p.mu.Unlock()
			}
		}
		p.unitNotices(ctx, m, now)
	}
	return read
}

// unitCheck reads one watch's check and ends or delays it (§5.1 steps 1-5).
func (p *fixPRPoller) unitCheck(ctx context.Context, m Master, w UnitWatch, now time.Time) {
	age := now.Sub(time.Unix(w.MergedAt, 0))
	if !unitFixTarget(m, w.Repo) {
		p.unitFinish(ctx, m, w, UnitInconclusive, nil, nil)
		return
	}
	owner, repo, ok := ghOwnerRepo(w.Repo)
	if !ok {
		p.unitFinish(ctx, m, w, UnitInconclusive, nil, nil)
		return
	}
	runs, err := p.gh.workflowRuns(ctx, owner, repo, w.SHA)
	if err != nil {
		p.unitReadFailed(ctx, w, age, err, now)
		return
	}
	w.Reads++
	run, n := unitPickRun(runs, m.UnitChecks.WorkflowPath(), w.Base, w.SHA)
	switch {
	case n > 1:
		p.unitFinish(ctx, m, w, UnitInconclusive, nil, nil)
		return
	case n == 0:
		p.unitNoRun(ctx, m, w, age, now)
		return
	}
	checks, err := p.gh.checkRuns(ctx, owner, repo, w.SHA, m.UnitChecks.Name())
	if err != nil {
		p.unitReadFailed(ctx, w, age, err, now)
		return
	}
	var match []ghCheckRun
	for _, c := range checks {
		if c.App.Slug == unitActionsSlug && c.Name == m.UnitChecks.Name() && c.CheckSuite.ID == run.CheckSuiteID {
			match = append(match, c)
		}
	}
	switch {
	case len(match) > 1:
		p.unitFinish(ctx, m, w, UnitInconclusive, nil, nil)
		return
	case len(match) == 0:
		p.unitNoRun(ctx, m, w, age, now)
		return
	}
	cr := match[0]
	if cr.Status != "completed" {
		if age >= unitGiveUpAfter {
			p.unitFinish(ctx, m, w, UnitInconclusive, nil, nil)
			return
		}
		p.unitDelay(ctx, w, now.Add(unitWait(age)))
		return
	}
	switch cr.Conclusion {
	case "success":
		p.unitFinish(ctx, m, w, UnitPassed, nil, &UnitPassIn{RunID: run.ID})
	case "failure":
		anns, err := p.gh.checkAnnotations(ctx, owner, repo, cr.ID)
		if err != nil {
			p.unitReadFailed(ctx, w, age, err, now)
			return
		}
		in, withheld := unitScreen(anns, cr.Output.Title, cr.Output.Summary)
		if withheld {
			p.unitFinish(ctx, m, w, "refused:withheld", nil, nil)
			p.unitSay(ctx, m.ID, w.PR, "unit-test annotations on a merge were all withheld; nothing filed", "")
			return
		}
		in.RunID = run.ID
		p.unitFinish(ctx, m, w, UnitFailed, &in, nil)
	default:
		// cancelled, skipped, neutral, timed_out, action_required, stale, anything else
		p.unitFinish(ctx, m, w, UnitInconclusive, nil, nil)
	}
}

// unitPickRun keeps the push runs of the workflow on the merge commit and base.
// Re-runs reuse a run's id: the highest attempt of the one id is the run, and
// two distinct runs answer n > 1.
func unitPickRun(runs []ghWorkflowRun, workflow, base, sha string) (ghWorkflowRun, int) {
	var best ghWorkflowRun
	ids := map[int64]bool{}
	for _, r := range runs {
		if r.Path != workflow || r.Event != "push" || r.HeadBranch != base || !strings.EqualFold(r.HeadSHA, sha) {
			continue
		}
		if !ids[r.ID] || r.RunAttempt > best.RunAttempt {
			best = r
		}
		ids[r.ID] = true
	}
	return best, len(ids)
}

// unitNoRun is step 3: wait until 6 hours after the merge, then no-ci when a read
// was answered, else inconclusive.
func (p *fixPRPoller) unitNoRun(ctx context.Context, m Master, w UnitWatch, age time.Duration, now time.Time) {
	if age < unitGiveUpAfter {
		p.unitDelay(ctx, w, now.Add(unitWait(age)))
		return
	}
	if w.Reads == 0 {
		p.unitFinish(ctx, m, w, UnitInconclusive, nil, nil)
		return
	}
	p.unitFinish(ctx, m, w, UnitNoCI, nil, nil)
	day := now.UTC().Format("2006-01-02")
	// The master of the pass may be stale: an earlier watch of this pass may have sent today's.
	if cur, err := p.st.Master(ctx, m.ID); err != nil || cur.UnitChecks.LastNoCINoticeDay == day {
		return
	}
	p.unitSay(ctx, m.ID, w.PR, "no unit-tests run on a merge; is the workflow installed?", "")
	if _, err := p.st.MarkUnitNotice(ctx, m.ID, "", UnitNoticeNoCI); err != nil {
		p.logf("unit checks: the no-ci notice could not be recorded: %v", err)
	}
}

// unitReadFailed counts a failed read and backs off like pollRow. A rate-limited
// read is a failed read: GAOS never waits for the reset. After 20 failed reads,
// or 6 hours with no answer at all, the watch ends inconclusive.
func (p *fixPRPoller) unitReadFailed(ctx context.Context, w UnitWatch, age time.Duration, err error, now time.Time) {
	if ctx.Err() != nil {
		return
	}
	w.Tries++
	p.mu.Lock()
	p.status.LastError = ghErrorClass(err)
	p.mu.Unlock()
	p.logf("unit checks: reading %s failed: %s", w.Key, ghErrorClass(err))
	if w.Tries >= unitMaxTries || (age >= unitGiveUpAfter && w.Reads == 0) {
		m, merr := p.st.Master(ctx, w.ProductID)
		if merr == nil {
			p.unitFinish(ctx, m, w, UnitInconclusive, nil, nil)
		}
		return
	}
	wait := unitWait(age)
	for i := 0; i < w.Tries && wait < fixPRBackoffCap; i++ {
		wait *= 2
	}
	p.unitDelay(ctx, w, now.Add(min(wait, fixPRBackoffCap)))
}

func (p *fixPRPoller) unitDelay(ctx context.Context, w UnitWatch, next time.Time) {
	if _, err := p.st.DelayUnitWatch(ctx, w.Key, next.Unix(), w.Tries, w.Reads); err != nil {
		p.logf("unit checks: a watch could not be delayed: %v", err)
	}
}

// unitFinish ends a watch through the one store call, then builds the context of
// a newly held defect outside every lock.
func (p *fixPRPoller) unitFinish(ctx context.Context, m Master, w UnitWatch, outcome string, in *UnitDefectIn, pass *UnitPassIn) {
	res, err := p.st.FinishUnitWatch(ctx, w.Key, outcome, in, pass)
	if err != nil {
		p.logf("unit checks: a watch could not be ended: %v", err)
		return
	}
	if len(res.UnitWatches) > 0 {
		p.logf("unit checks: %s ended %s", w.Key, res.UnitWatches[0].Outcome)
	}
	if (res.UnitNew || res.UnitReopened) && len(res.Defects) > 0 {
		p.unitContext(ctx, res.Defects[0])
	}
}

// unitSay sends one group notice of kind unit:failure. The lines name no repository.
func (p *fixPRPoller) unitSay(ctx context.Context, productID string, pr int, text, cardID string) {
	if p.unitNotify != nil {
		p.unitNotify(ctx, productID, pr, text, cardID)
	}
}

// unitNotices is the re-send scan (§5.3): a held card whose notice was not sent,
// a marked defect whose passed notice was not sent, a defect a pass closed whose
// closed notice was not sent, and a held defect with no context yet. A crash
// between a store write and its send is repaired here.
func (p *fixPRPoller) unitNotices(ctx context.Context, m Master, now time.Time) {
	defs, err := p.st.Defects(ctx, m.ID)
	if err != nil {
		return
	}
	var cards map[string]TaskRecord
	for _, d := range defs {
		if d.Kind != DefectKindUnit {
			continue
		}
		if defectFinal(d.State) {
			if d.State == DefectStateFixed && d.UnitPassedAt > 0 && !d.UnitPassNoticeSent {
				p.unitSay(ctx, m.ID, 0, "defect "+unitH8(d)+" closed: its tests passed", "")
				p.unitMark(ctx, m.ID, d.Key, UnitNoticeClosed)
			}
			continue
		}
		if cards == nil {
			cards = map[string]TaskRecord{}
			cs, _ := p.st.PRCards(ctx, m.ID)
			for _, c := range cs {
				cards[c.ID] = c
			}
		}
		c, ok := cards[d.TriageTask]
		if !ok || c.Status != StatusBlocked || c.BlockReason != ReasonUnitHeld {
			continue
		}
		if !d.UnitNoticeSent {
			p.unitSay(ctx, m.ID, 0, "unit tests failed on a merge; defect "+unitH8(d)+" waits for an auto-fix admin", c.ID)
			p.unitMark(ctx, m.ID, d.Key, UnitNoticeHeld)
		}
		if d.UnitPassedAt > 0 && !d.UnitPassNoticeSent {
			sha12, _, _ := strings.Cut(d.RunRef, "@")
			p.unitSay(ctx, m.ID, 0, "defect "+unitH8(d)+": its failing tests passed at "+sha12+
				"; an admin closes it by cancelling the held card", c.ID)
			p.unitMark(ctx, m.ID, d.Key, UnitNoticePassed)
		}
		if !d.Context.Built {
			p.mu.Lock()
			at, tried := p.unitCtx[d.Key]
			p.mu.Unlock()
			if !tried || now.Sub(at) >= unitCtxRetry {
				p.unitContext(ctx, d)
			}
		}
	}
}

func (p *fixPRPoller) unitMark(ctx context.Context, productID, key, which string) {
	if _, err := p.st.MarkUnitNotice(ctx, productID, key, which); err != nil {
		p.logf("unit checks: the %s notice could not be recorded: %v", which, err)
	}
}

// unitContext builds a unit defect's source slice and stores it. A failure leaves
// no context: the triager is told to answer medium, and a later scan retries.
func (p *fixPRPoller) unitContext(ctx context.Context, d Defect) {
	p.mu.Lock()
	if p.unitCtx == nil {
		p.unitCtx = map[string]time.Time{}
	}
	p.unitCtx[d.Key] = p.now()
	p.mu.Unlock()
	if p.gh == nil {
		return
	}
	c, err := buildUnitContext(ctx, p.gh, d, d.UnitSHA, d.UnitFiles)
	if err != nil {
		p.logf("unit checks: the source slice of a unit defect could not be built: %s", defectErrClass(err))
		return
	}
	if _, err := p.st.SetDefectContext(ctx, d.ProductID, d.Key, c); err != nil {
		p.logf("unit checks: the source slice could not be stored: %v", err)
	}
}

// buildUnitContext reads a unit defect's slice at the merge commit (§7.5): the
// failing test files first, then the non-test .go files of at most two of their
// directories, at most defectMaxFetch reads and defectMaxFiles files, with the
// limits and egress screen of the review context.
func buildUnitContext(ctx context.Context, src codeSource, d Defect, sha string, failing []string) (DefectContext, error) {
	owner, repo, ok := ghOwnerRepo(d.Repo)
	if !ok || !unitSHARe.MatchString(sha) {
		return DefectContext{}, errSearchFailed
	}
	var paths, dirs []string
	add := func(p string) {
		if contextPathOK(p) && !containsFold(paths, p) {
			paths = append(paths, p)
		}
	}
	for _, f := range failing {
		add(f)
		if dir := path.Dir(f); !containsFold(dirs, dir) && len(dirs) < unitCtxMaxDirs {
			dirs = append(dirs, dir)
		}
	}
	for _, dir := range dirs {
		files, err := src.dirAt(ctx, owner, repo, dir, sha)
		if err != nil {
			return DefectContext{}, err
		}
		for _, f := range files {
			add(f)
		}
	}
	if len(paths) > defectMaxFetch {
		paths = paths[:defectMaxFetch]
	}
	return buildReviewContext(ctx, src, owner+"/"+repo, sha, paths)
}

// unitPrintable reports one line of printable text.
func unitPrintable(s string) bool {
	for _, r := range s {
		if !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

// unitScreen is §5.2: from a failed check's annotations to the kept failing tests.
// withheld is true when failure annotations exist and every one was withheld.
func unitScreen(anns []ghAnnotation, title, summary string) (in UnitDefectIn, withheld bool) {
	failures, dropped := 0, 0
	total := -1
	for _, a := range anns {
		if a.AnnotationLevel == "notice" && a.Title == unitTotalTitle {
			if n, err := strconv.Atoi(strings.TrimSpace(a.Message)); err == nil && n >= 0 {
				total = n
			}
			continue
		}
		if a.AnnotationLevel != "failure" {
			continue
		}
		failures++
		test, file := a.Title, a.Path
		if file == ".github" { // GitHub's path for an annotation that named no file
			file = ""
		}
		ok := unitTestRe.MatchString(test) && (file == "" || contextPathOK(file)) &&
			unitPrintable(test) && unitPrintable(file) && stripBR53(test) == test && stripBR53(file) == file &&
			!screenBody(test) && !screenBody(file)
		if !ok {
			dropped++
			continue
		}
		f := unitFailure{Test: test, File: file}
		dup := false
		for _, k := range in.Failing {
			dup = dup || k == f
		}
		if !dup && len(in.Failing) < unitMaxFailing {
			in.Failing = append(in.Failing, f)
		}
	}
	if failures == 0 {
		in.Build = true
		text := strings.TrimSpace(stripBR53(title) + " " + stripBR53(summary))
		if screenBody(title) || screenBody(summary) || screenBody(text) {
			text = "(withheld: the summary matched a secret detector)"
		}
		in.Summary = capBytes(text, unitSummaryMax)
		return in, false
	}
	if len(in.Failing) == 0 {
		return in, true
	}
	in.Total = len(in.Failing)
	if total >= 0 {
		in.Total = min(max(total, len(in.Failing)), unitTotalMax)
	}
	return in, false
}
