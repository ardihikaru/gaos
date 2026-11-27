package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Unit defects (D-105, docs/designs/release-b4-unit-tests.md §7).
//
// A failing unit-test check of a merge commit files -- or updates -- the one
// open unit defect of that repository and base (plus one for a failed build).
// Its triage card is born BLOCKED with ReasonUnitHeld: an auto-fix admin reads
// the failing tests and releases it; then the D-091 path runs. A later merge on
// the same base whose check passes closes the defect, except that a still-held
// one is only marked. The failing tests' names and files come from the code
// under test: they are data, screened on intake and fenced in the packet.

// RuleUnitPassed is the rule of a pass's close and card cancel.
const RuleUnitPassed = "unit-passed"

// unitBuildHash is the h of a failed build: a check that failed with no test named.
const unitBuildHash = "build"

// unitMarkPrefix starts the head line of a held defect whose check passed.
const unitMarkPrefix = "checks passed at "

// unitMaxFailing is the most failing tests a defect names (GitHub keeps at most
// 10 error annotations per step).
const unitMaxFailing = 10

// unitTestRe is a failing test's name: a Go test, example or fuzz target and its subtests.
var unitTestRe = regexp.MustCompile(`^(Test|Example|Fuzz)[A-Za-z0-9_/]{0,195}$`)

// unitFailure is one kept failing test and the file that declares it ("" = unknown).
type unitFailure struct {
	Test string
	File string
}

// UnitDefectIn is a failing check, as the poller read and screened it (§7.1).
type UnitDefectIn struct {
	Repo, SHA, Base string
	PR              int
	MergedAt        int64
	RunID           int64
	Build           bool // no failure-level annotation
	Total           int
	Failing         []unitFailure // at most 10 kept
	Summary         string        // a build failure's summary, screened, at most 2 KiB
}

// UnitPassIn is a passing check (§5.1).
type UnitPassIn struct {
	Repo, SHA, Base string
	MergedAt        int64
	RunID           int64 // the workflow run, recorded in RunRef
}

func unitPair(f unitFailure) string { return f.Test + "\t" + f.File }

// unitHash is the first 16 hex of sha256 over the sorted kept pairs.
func unitHash(fs []unitFailure) string {
	pairs := make([]string, 0, len(fs))
	for _, f := range fs {
		pairs = append(pairs, unitPair(f))
	}
	sort.Strings(pairs)
	sum := sha256.Sum256([]byte(strings.Join(pairs, "\n")))
	return hex.EncodeToString(sum[:])[:16]
}

// unitCaseID is unit:<owner>/<repo>~<base>~<h>; "~" cannot occur in a branch name.
func unitCaseID(repo, base, h string) string {
	return "unit:" + strings.ToLower(repo) + "~" + base + "~" + h
}

// unitCaseParts reads a unit defect's case id back.
func unitCaseParts(caseID string) (repo, base, h string, ok bool) {
	rest, found := strings.CutPrefix(caseID, "unit:")
	if !found {
		return "", "", "", false
	}
	parts := strings.Split(rest, "~")
	if len(parts) != 3 {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}

// unitH8 is the short name of a unit defect in a notice.
func unitH8(d Defect) string {
	_, _, h, _ := unitCaseParts(d.CaseID)
	if len(h) > 8 {
		return h[:8]
	}
	return h
}

// unitIsBuild reports a failed-build defect.
func unitIsBuild(d Defect) bool {
	_, _, h, _ := unitCaseParts(d.CaseID)
	return h == unitBuildHash
}

// unitRunRef is <sha12>@<runID>.
func unitRunRef(sha string, runID int64) string {
	return idSHA(sha) + "@" + strconv.FormatInt(runID, 10)
}

// unitFailuresFrom reads UnitTests back.
func unitFailuresFrom(tests string) []unitFailure {
	var out []unitFailure
	for _, l := range strings.Split(tests, "\n") {
		if l == "" {
			continue
		}
		t, f, _ := strings.Cut(l, "\t")
		out = append(out, unitFailure{Test: t, File: f})
	}
	return out
}

func unitTestsText(fs []unitFailure) string {
	pairs := make([]string, 0, len(fs))
	for _, f := range fs {
		pairs = append(pairs, unitPair(f))
	}
	return strings.Join(pairs, "\n")
}

func unitFilesOf(fs []unitFailure) []string {
	var out []string
	for _, f := range fs {
		if f.File != "" && !containsFold(out, f.File) {
			out = append(out, f.File)
		}
	}
	return out
}

// unitExcerpt is one line per kept failure, then "… and <k> more", cut to 4 KiB.
func unitExcerpt(fs []unitFailure, total int) string {
	var sb strings.Builder
	for _, f := range fs {
		sb.WriteString(f.Test)
		if f.File != "" {
			sb.WriteString(" — " + f.File)
		}
		sb.WriteString("\n")
	}
	if k := total - len(fs); k > 0 {
		fmt.Fprintf(&sb, "… and %d more\n", k)
	}
	return capBytes(strings.TrimRight(sb.String(), "\n"), defectMaxExcerpt)
}

func unitTitle(in UnitDefectIn, total int) string {
	if in.Build {
		return "The build failed in " + strings.ToLower(in.Repo)
	}
	return fmt.Sprintf("Unit tests failing in %s (%d)", strings.ToLower(in.Repo), total)
}

// unitWithoutMark drops a passed mark's head line.
func unitWithoutMark(ex string) string {
	if strings.HasPrefix(ex, unitMarkPrefix) {
		if _, rest, ok := strings.Cut(ex, "\n"); ok {
			return rest
		}
		return ""
	}
	return ex
}

// validUnitIn is the store's own check of what the poller screened.
func validUnitIn(in UnitDefectIn) bool {
	if !defectRepo.MatchString(in.Repo) || !baseBranchRe.MatchString(in.Base) || strings.Contains(in.Base, "..") ||
		strings.Contains(in.Base, "~") || len(in.Failing) > unitMaxFailing || (!in.Build && len(in.Failing) == 0) {
		return false
	}
	for _, f := range in.Failing {
		if !unitTestRe.MatchString(f.Test) || (f.File != "" && !contextPathOK(f.File)) {
			return false
		}
	}
	return true
}

// unitHeldLocked reports whether a defect's triage card is held for an admin.
func (s *MemStore) unitHeldLocked(d *Defect) bool {
	t := s.taskByIDLocked(d.TriageTask)
	return t != nil && t.Status == StatusBlocked && t.BlockReason == ReasonUnitHeld
}

// unitPlanRefusalLocked is §7.2's gate, beside reviewPlanRefusalLocked: "" plans,
// else the slug of the first condition that refuses.
func (s *MemStore) unitPlanRefusalLocked(m *Master, repo string, now int64) string {
	switch {
	case !m.UnitChecks.Enabled:
		return "off"
	case !m.AssignedSet || !roleAssigned(*m, "fixer"):
		return "no-fixer"
	case !m.AssignedSet || !roleAssigned(*m, "triager"):
		return "no-triager"
	case !m.AutoFix.Enabled:
		return "autofix-off"
	case !unitFixTarget(*m, repo):
		return "not-target"
	}
	n := 0
	for _, d := range s.defects {
		if d.ProductID == m.ID && d.Kind == DefectKindUnit &&
			(d.FirstSeen/86400 == now/86400 || (d.UnitReopenedAt > 0 && d.UnitReopenedAt/86400 == now/86400)) {
			n++
		}
	}
	if n >= m.UnitChecks.Cap() {
		return "unit-cap"
	}
	if s.newKeysTodayLocked(m.ID, now) >= defectMaxNewKeys {
		return "defect-cap"
	}
	return ""
}

// unitFill writes a failing check's facts onto a record.
func unitFill(d *Defect, in UnitDefectIn, fs []unitFailure, total int) {
	d.Title = unitTitle(in, total)
	if in.Build {
		d.Excerpt = capBytes(in.Summary, defectMaxExcerpt)
	} else {
		d.Excerpt = unitExcerpt(fs, total)
	}
	d.ExcerptWithheld = false
	d.UnitTests = unitTestsText(fs)
	d.UnitFiles = unitFilesOf(fs)
	d.UnitBase = in.Base
	d.UnitSHA, d.UnitMergedAt = strings.ToLower(in.SHA), in.MergedAt
	d.RunRef = unitRunRef(in.SHA, in.RunID)
}

// planUnitDefectLocked files a failing check (§7.1, §7.2). It answers the record
// it filed or touched (nil when refused), the Result and the watch's outcome.
func (s *MemStore) planUnitDefectLocked(m *Master, in UnitDefectIn, now int64) (*Defect, Result, string) {
	repo := strings.ToLower(in.Repo)
	in.Repo = repo
	// 1. Out of order: a newer merge of this repository and base already passed.
	for _, w := range s.unitWatches {
		if w.ProductID == m.ID && w.Repo == repo && w.Base == in.Base && w.State == UnitWatchDone &&
			w.Outcome == UnitPassed && w.MergedAt > in.MergedAt {
			return nil, Result{}, "refused:older"
		}
	}
	for _, d := range s.defects {
		if d.ProductID != m.ID || d.Kind != DefectKindUnit || !defectFinal(d.State) || d.UnitPassedAt == 0 {
			continue
		}
		if r, b, _, ok := unitCaseParts(d.CaseID); ok && r == repo && b == in.Base && d.UnitPassedAt >= in.MergedAt {
			return nil, Result{}, "refused:older"
		}
	}
	h := unitBuildHash
	if !in.Build {
		h = unitHash(in.Failing)
	}
	caseID := unitCaseID(repo, in.Base, h)
	key := m.ID + "/" + caseID
	total := max(in.Total, len(in.Failing))
	newer := func(d *Defect) bool { return in.MergedAt > d.UnitMergedAt }
	resetPass := func(d *Defect) {
		if d.UnitPassedAt > 0 && in.MergedAt > d.UnitPassedAt {
			d.UnitPassedAt = 0
			d.UnitPassNoticeSent = false
			d.Excerpt = unitWithoutMark(d.Excerpt)
		}
	}
	// 2. The same failing set, open: the record follows the newer merge.
	if d := s.defects[key]; d != nil && !defectFinal(d.State) {
		d.LastSeen = max(d.LastSeen, now)
		if newer(d) {
			d.UnitSHA, d.UnitMergedAt, d.RunRef = strings.ToLower(in.SHA), in.MergedAt, unitRunRef(in.SHA, in.RunID)
			if in.Build {
				d.Excerpt = capBytes(in.Summary, defectMaxExcerpt)
			}
		}
		resetPass(d)
		if d.UnitTests == "" && !in.Build {
			mark := ""
			if strings.HasPrefix(d.Excerpt, unitMarkPrefix) {
				mark, _, _ = strings.Cut(d.Excerpt, "\n")
				mark += "\n"
			}
			d.UnitTests = unitTestsText(in.Failing)
			d.Excerpt = mark + unitExcerpt(in.Failing, total)
		}
		if d.Excerpt == "" && in.Build {
			d.Excerpt = capBytes(in.Summary, defectMaxExcerpt)
		}
		return d, Result{Defects: []Defect{d.Clone()}}, UnitDedup
	}
	// 3. A different failing set while a non-build unit defect of this repository
	// and base is open: that one is updated, never a parallel one.
	if !in.Build {
		var keys []string
		for k, d := range s.defects {
			if d.ProductID != m.ID || d.Kind != DefectKindUnit || defectFinal(d.State) || k == key {
				continue
			}
			if r, b, oh, ok := unitCaseParts(d.CaseID); ok && r == repo && b == in.Base && oh != unitBuildHash {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		if len(keys) > 0 {
			d := s.defects[keys[0]]
			union := append([]unitFailure(nil), in.Failing...)
			for _, o := range unitFailuresFrom(d.UnitTests) {
				if len(union) >= unitMaxFailing {
					break
				}
				dup := false
				for _, u := range union {
					dup = dup || u == o
				}
				if !dup {
					union = append(union, o)
				}
			}
			t := max(total, len(union))
			d.Title = unitTitle(in, t)
			d.UnitTests, d.UnitFiles = unitTestsText(union), unitFilesOf(union)
			d.Excerpt = unitExcerpt(union, t)
			if newer(d) {
				d.UnitSHA, d.UnitMergedAt, d.RunRef = strings.ToLower(in.SHA), in.MergedAt, unitRunRef(in.SHA, in.RunID)
			}
			d.LastSeen = max(d.LastSeen, now)
			// A new failing set: whatever passed before no longer covers it.
			d.UnitPassedAt, d.UnitPassNoticeSent = 0, false
			d.Excerpt = unitWithoutMark(d.Excerpt)
			return d, Result{Defects: []Defect{d.Clone()}}, UnitUpdated
		}
	}
	// 4. The gate.
	if slug := s.unitPlanRefusalLocked(m, repo, now); slug != "" {
		return nil, Result{}, "refused:" + slug
	}
	// 5. A final record of the same set is reopened; otherwise a new record.
	if d := s.defects[key]; d != nil {
		evs := s.reopenUnitDefectLocked(d, in, total, now)
		return d, Result{NewEvents: evs, Defects: []Defect{d.Clone()}, UnitReopened: true}, UnitFailed
	}
	d := &Defect{
		Key: key, ProductID: m.ID, CaseID: caseID, Kind: DefectKindUnit,
		// "be" passes D-091's backend-only gates by decision (D-091 (8) amended): a unit
		// test is application code, as a review finding is.
		AutomationType: "be", Repo: repo, TargetRepos: []string{repo},
		FirstSeen: now, LastSeen: now, Gen: 1, State: DefectStateOpen,
	}
	unitFill(d, in, in.Failing, total)
	s.defects[key] = d
	evs := []Event{s.defectEventLocked(DefectReported, m.ID, "defect reported: "+d.Ref()+" (unit)", KindInfo,
		EventPayload{Task: key, Rule: DefectKindUnit, To: DefectStateOpen, Reason: DefectKindUnit})}
	_, planned := s.planDefectCardLocked(d, defectJobTriage, StatusBlocked, ReasonUnitHeld, LabelDefectTriage,
		planOpts{Title: "Triage " + d.Title})
	evs = append(evs, planned...)
	return d, Result{NewEvents: evs, Defects: []Defect{d.Clone()}, UnitNew: true}, UnitFailed
}

// reopenUnitDefectLocked is §7.4's "seen again, final": a new generation with a
// new held card. The gate already passed.
func (s *MemStore) reopenUnitDefectLocked(d *Defect, in UnitDefectIn, total int, now int64) []Event {
	from := d.State
	d.Gen++
	d.BaseOverride = ""
	d.Regression = true
	d.Difficulty, d.Class, d.Reason = "", "", ""
	d.Files = nil
	d.TriageTask, d.FixTask, d.FixPR = "", "", ""
	d.Context = DefectContext{}
	unitFill(d, in, in.Failing, total)
	d.Repo, d.TargetRepos = strings.ToLower(in.Repo), []string{strings.ToLower(in.Repo)}
	d.LastSeen = max(d.LastSeen, now)
	d.UnitReopenedAt, d.UnitPassedAt = now, 0
	d.UnitNoticeSent, d.UnitPassNoticeSent = false, false
	d.State = from // setDefectStateLocked records the move from it
	evs := []Event{s.setDefectStateLocked(d, DefectStateOpen, RuleDefectRegression, "")}
	_, planned := s.planDefectCardLocked(d, defectJobTriage, StatusBlocked, ReasonUnitHeld, LabelDefectTriage,
		planOpts{Title: "Triage " + d.Title})
	return append(evs, planned...)
}

// passUnitDefectsLocked is §7.4's "passed": every non-final unit defect of the
// repository and base merged before the pass is closed, or marked if its triage
// card is still held. A non-build defect whose tests retention emptied is never
// pass-closed: nothing says which tests it was.
func (s *MemStore) passUnitDefectsLocked(m *Master, in UnitPassIn) (evs []Event, touched []Defect, marked, closed bool) {
	repo := strings.ToLower(in.Repo)
	var keys []string
	for k, d := range s.defects {
		if d.ProductID != m.ID || d.Kind != DefectKindUnit || defectFinal(d.State) || d.UnitMergedAt >= in.MergedAt {
			continue
		}
		r, b, h, ok := unitCaseParts(d.CaseID)
		if !ok || r != repo || b != in.Base || d.UnitBase != in.Base || (h != unitBuildHash && d.UnitTests == "") {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		d := s.defects[k]
		d.RunRef = unitRunRef(in.SHA, in.RunID)
		prev := d.UnitPassedAt
		d.UnitPassedAt = max(d.UnitPassedAt, in.MergedAt)
		if s.unitHeldLocked(d) {
			d.Excerpt = unitMarkPrefix + idSHA(strings.ToLower(in.SHA)) + "\n" + unitWithoutMark(d.Excerpt)
			d.Excerpt = capBytes(d.Excerpt, defectMaxExcerpt)
			if prev == 0 {
				marked = true
				d.UnitPassNoticeSent = false
			}
		} else {
			evs = append(evs, s.cancelDefectCardsLocked(d, RuleUnitPassed)...)
			evs = append(evs, s.setDefectStateLocked(d, DefectStateFixed, RuleUnitPassed, ""))
			d.UnitPassNoticeSent = false // the closed notice is still to be sent
			closed = true
		}
		touched = append(touched, d.Clone())
	}
	return evs, touched, marked, closed
}

// FinishUnitWatch ends a pending watch with its outcome and, in the same lock,
// files the failure (in) or applies the pass (pass): a crash never leaves a
// defect without its watch ending, or the reverse (§5.1). A failure's outcome
// is the planner's: failed, updated, dedup or refused:<slug>.
func (s *MemStore) FinishUnitWatch(ctx context.Context, key, outcome string, in *UnitDefectIn, pass *UnitPassIn) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, ok := s.unitWatches[key]
	if !ok {
		return Result{}, ErrNotFound
	}
	if w.State == UnitWatchDone {
		return Result{}, nil
	}
	m := s.masterPtr(w.ProductID)
	if m == nil {
		return Result{}, ErrNotFound
	}
	now := autoFixNow()
	var res Result
	switch {
	case in != nil:
		c := *in
		c.Repo, c.Base, c.SHA, c.PR, c.MergedAt = w.Repo, w.Base, w.SHA, w.PR, w.MergedAt
		if !validUnitIn(c) {
			return Result{}, reject("a unit defect needs a repository, a base and at most ten valid failing tests")
		}
		var d *Defect
		d, res, outcome = s.planUnitDefectLocked(m, c, now)
		if d != nil {
			w.DefectKey = d.Key
		}
	case pass != nil:
		c := *pass
		c.Repo, c.Base, c.SHA, c.MergedAt = w.Repo, w.Base, w.SHA, w.MergedAt
		evs, ds, marked, closed := s.passUnitDefectsLocked(m, c)
		res = Result{NewEvents: evs, Defects: ds, UnitMarked: marked, UnitClosed: closed}
		if len(ds) > 0 {
			w.DefectKey = ds[0].Key
		}
	}
	if !unitOutcomeRe.MatchString(outcome) {
		return Result{}, reject("not a unit watch outcome")
	}
	w.State, w.Outcome, w.DoneAt = UnitWatchDone, outcome, now
	s.unitWatches[key] = w
	res.UnitWatches = []UnitWatch{w}
	if len(res.NewEvents) > 0 {
		// ArcadeDB writes Task rows only for the desks a Result lists.
		res.Floor = s.floor()
	}
	return res, nil
}

func (s *ArcadeStore) FinishUnitWatch(ctx context.Context, key, outcome string, in *UnitDefectIn, pass *UnitPassIn) (Result, error) {
	return s.mutate(ctx, func() (Result, error) { return s.proj.FinishUnitWatch(ctx, key, outcome, in, pass) })
}

func (readOnly) FinishUnitWatch(context.Context, string, string, *UnitDefectIn, *UnitPassIn) (Result, error) {
	return Result{}, reject(replayRefusal)
}
