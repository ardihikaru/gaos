package main

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Unit checks (D-105, docs/designs/release-b4-unit-tests.md §4, §5).
//
// GAOS runs no unit test. A fix-target repository's owner adds a GitHub
// Actions workflow (§11) and GAOS reads the result of that one workflow's
// push run on each merge commit: a failing check becomes a held unit defect
// (unitdefect.go), a passing one closes it. This file is the setting and the
// watch -- the persisted record that one merge commit's check is awaited.
// Every watch write is silent bookkeeping, like ExpireExcerpts: it emits no
// event (BR-02); the defect and card changes it causes emit the usual ones.

// ReasonUnitHeld is the block reason of a unit defect's triage card: only an
// auto-fix admin moves it out (§7.3).
const ReasonUnitHeld = "unit tests failed; an auto-fix admin releases this card after reading the failing tests on the Auto-fix page"

// The watch states and the limits of §4.2.
const (
	UnitWatchPending   = "pending"
	UnitWatchDone      = "done"
	unitMaxPending     = 200
	unitWatchKeepDays  = 30
	unitZeroSHA        = "0000000000000000000000000000000000000000"
	unitBaseUnknown    = "unknown"
	unitDefaultCacheTT = 10 * time.Minute
)

// The waits between two reads of a pending watch (§5.1): every 2 minutes for the
// first 30 minutes after the merge, every 10 until 2 hours, then every 30. A watch
// with no answer 6 hours after its merge ends.
const (
	unitWaitFast    = 2 * time.Minute
	unitWaitMid     = 10 * time.Minute
	unitWaitSlow    = 30 * time.Minute
	unitFastWindow  = 30 * time.Minute
	unitMidWindow   = 2 * time.Hour
	unitGiveUpAfter = 6 * time.Hour
)

// The watch outcomes (§6).
const (
	UnitPassed       = "passed"
	UnitFailed       = "failed"
	UnitNoCI         = "no-ci"
	UnitInconclusive = "inconclusive"
	UnitDedup        = "dedup"
	UnitUpdated      = "updated"
)

var (
	unitWorkflowRe = regexp.MustCompile(`^\.github/workflows/[A-Za-z0-9._-]{1,80}\.ya?ml$`)
	unitCheckRe    = regexp.MustCompile(`^[A-Za-z0-9 ._-]{1,100}$`)
	unitSHARe      = regexp.MustCompile(`^[0-9a-f]{40}$`)
	unitOutcomeRe  = regexp.MustCompile(`^(passed|failed|no-ci|inconclusive|dedup|updated|refused:[a-z-]+|skipped:[a-z-]+)$`)
)

// UnitWatch is one merge commit whose check GAOS awaits (§4.2).
type UnitWatch struct {
	Key       string // <productId>/<owner>/<repo>@<sha>, lower-cased: the vertex id
	ProductID string
	Repo      string // owner/repo, lower-cased
	PR        int
	SHA       string // the merge commit, 40 hex
	Base      string
	MergedAt  int64
	FirstSeen int64
	NextAt    int64 // the next read (§5.1)
	Tries     int   // failed reads
	Reads     int   // answered reads
	State     string
	Outcome   string
	DefectKey string // set when the outcome filed or touched a defect
	DoneAt    int64  // set when State becomes done

	// D-106 (schema 22): a result Jenkins posted. RunURL is its build link
	// (https, screened, else empty), PostedFrom the caller's address as the gate
	// keyed it (never the credential), PostedResult what it posted (passed,
	// failed or build-failed). ConflictNoticeSent: the "conflicting unit result"
	// notice of this watch was sent, so later conflicting posts are only logged.
	RunURL             string
	PostedFrom         string
	PostedResult       string
	ConflictNoticeSent bool
}

// unitWatchKey is a watch's vertex id.
func unitWatchKey(productID, repo, sha string) string {
	return strings.ToLower(productID + "/" + repo + "@" + sha)
}

// unitSkippedKey is the id of a watch whose merge could not be read: it has no
// merge commit, so the pull request names it.
func unitSkippedKey(productID, repo string, pr int) string {
	return strings.ToLower(fmt.Sprintf("%s/%s#%d", productID, repo, pr))
}

// unitFixTarget reports whether owner/repo is a fix-target repository: listed in
// AutoFix.TargetRepos and not in AutoFix.AutomationRepos.
func unitFixTarget(m Master, repo string) bool {
	return containsFold(m.AutoFix.TargetRepos, repo) && !containsFold(m.AutoFix.AutomationRepos, repo)
}

// unitWatchFor is §4.2: whether a merge of owner/repo into base plants a watch.
// effective is the effective base (AutoFix.BaseBranch, else the default branch).
func unitWatchFor(m Master, owner, repo, base, effective string) bool {
	if !m.UnitChecks.Enabled || !unitFixTarget(m, owner+"/"+repo) || base == "" {
		return false
	}
	return base == effective || unitIsExtraBase(m.UnitChecks, base)
}

// unitIsExtraBase reports whether base is one of UnitChecks.Bases (exact).
func unitIsExtraBase(u UnitChecks, base string) bool {
	for _, b := range u.Bases {
		if b == base {
			return true
		}
	}
	return false
}

// validateUnitChecks is SetUnitChecks's refusals that need no product (§4.1).
func validateUnitChecks(u UnitChecks) error {
	switch {
	case u.Source != "" && u.Source != unitSourceGitHub && u.Source != unitSourceJenkins:
		return reject("The source is GitHub Actions or Jenkins.")
	case u.DailyCap < 0 || u.DailyCap > unitCapMax:
		return reject("The daily cap is 1 to 20.")
	case len(u.Bases) > unitMaxBases:
		return reject("A unit-test base is a branch name.")
	case u.CheckName != "" && !unitCheckRe.MatchString(u.CheckName):
		return reject("The check name is 1 to 100 letters, digits, spaces, dots, dashes or underscores.")
	case u.Workflow != "" && (!unitWorkflowRe.MatchString(u.Workflow) || strings.Contains(u.Workflow, "..")):
		return reject("The workflow is a file under .github/workflows/.")
	}
	for _, b := range u.Bases {
		if !baseBranchRe.MatchString(b) || strings.Contains(b, "..") {
			return reject("A unit-test base is a branch name.")
		}
	}
	return nil
}

// SetUnitChecks saves a product's unit-check setting (§4.1). It keeps the stored
// LastNoCINoticeDay and, to turn the check on, needs auto-fix on, a fixer and a
// triager assigned and a fix-target repository.
func (s *MemStore) SetUnitChecks(ctx context.Context, productID string, u UnitChecks, actor string) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.masterPtr(productID)
	if m == nil {
		return Result{}, ErrNotFound
	}
	u = u.Clone()
	u.Workflow, u.CheckName = strings.TrimSpace(u.Workflow), strings.TrimSpace(u.CheckName)
	var bases []string
	for _, b := range u.Bases {
		if b = strings.TrimSpace(b); b != "" && !unitIsExtraBase(UnitChecks{Bases: bases}, b) {
			bases = append(bases, b)
		}
	}
	u.Bases = bases
	if err := validateUnitChecks(u); err != nil {
		return Result{}, err
	}
	if u.Enabled {
		target := false
		for _, r := range m.AutoFix.TargetRepos {
			target = target || unitFixTarget(*m, r)
		}
		switch {
		case !m.AutoFix.Enabled:
			return Result{}, reject("Turn auto-fix on first.")
		case !m.AssignedSet || !roleAssigned(*m, "fixer") || !roleAssigned(*m, "triager"):
			return Result{}, reject("Assign a fixer and a triager first.")
		case !target:
			return Result{}, reject("List a fix-target repository first.")
		}
	}
	u.LastNoCINoticeDay = m.UnitChecks.LastNoCINoticeDay
	m.UnitChecks = u
	alias := actorAlias(actor)
	text := fmt.Sprintf("%s: unit checks %s (%s, %s, bases %s, cap %d)", s.productName(productID), onOff(u.Enabled),
		u.WorkflowPath(), u.Name(), reasonList(u.Bases), u.Cap())
	if u.SourceName() == unitSourceJenkins {
		// The workflow and the check are kept, but not in force (D-106).
		text = fmt.Sprintf("%s: unit checks %s (jenkins, bases %s, cap %d)", s.productName(productID), onOff(u.Enabled),
			reasonList(u.Bases), u.Cap())
	}
	ev := s.emit(RecordUpdated, productID, text, KindInfo, EventPayload{Actor: alias, Rule: "unit-checks"})
	c := m.Clone()
	return Result{Master: &c, NewEvents: []Event{ev}, Toast: "Unit checks saved."}, nil
}

func (s *ArcadeStore) SetUnitChecks(ctx context.Context, productID string, u UnitChecks, actor string) (Result, error) {
	return s.mutate(ctx, func() (Result, error) { return s.proj.SetUnitChecks(ctx, productID, u, actor) })
}

func (readOnly) SetUnitChecks(context.Context, string, UnitChecks, string) (Result, error) {
	return Result{}, reject(replayRefusal)
}

// PlantUnitWatch stores a watch (§4.2). It is idempotent on the key: a replayed
// merge plants nothing. A product holds at most 200 pending watches.
func (s *MemStore) PlantUnitWatch(ctx context.Context, productID string, w UnitWatch) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.masterPtr(productID) == nil {
		return Result{}, ErrNotFound
	}
	w.ProductID = productID
	w.Repo, w.SHA = strings.ToLower(strings.TrimSpace(w.Repo)), strings.ToLower(strings.TrimSpace(w.SHA))
	if w.State == "" {
		w.State = UnitWatchPending
	}
	if w.Key == "" {
		w.Key = unitWatchKey(productID, w.Repo, w.SHA)
	}
	w.Key = strings.ToLower(w.Key)
	switch {
	case !defectRepo.MatchString(w.Repo):
		return Result{}, reject("a unit watch needs owner/repo")
	case !unitSHARe.MatchString(w.SHA):
		return Result{}, reject("a unit watch needs a 40-hex merge commit")
	case w.Base == "":
		return Result{}, reject("a unit watch needs a base")
	case w.State != UnitWatchPending && w.State != UnitWatchDone:
		return Result{}, reject("a unit watch is pending or done")
	case w.Outcome != "" && !unitOutcomeRe.MatchString(w.Outcome):
		return Result{}, reject("not a unit watch outcome")
	}
	if _, ok := s.unitWatches[w.Key]; ok {
		return Result{}, nil
	}
	if w.State == UnitWatchPending {
		n := 0
		for _, o := range s.unitWatches {
			if o.ProductID == productID && o.State == UnitWatchPending {
				n++
			}
		}
		if n >= unitMaxPending {
			return Result{}, reject("Too many pending unit checks.")
		}
	}
	if w.FirstSeen == 0 {
		w.FirstSeen = autoFixNow()
	}
	s.unitWatches[w.Key] = w
	return Result{UnitWatches: []UnitWatch{w}}, nil
}

func (s *ArcadeStore) PlantUnitWatch(ctx context.Context, productID string, w UnitWatch) (Result, error) {
	return s.mutate(ctx, func() (Result, error) { return s.proj.PlantUnitWatch(ctx, productID, w) })
}

func (readOnly) PlantUnitWatch(context.Context, string, UnitWatch) (Result, error) {
	return Result{}, reject(replayRefusal)
}

// DelayUnitWatch moves a pending watch's next read and records its read counts.
func (s *MemStore) DelayUnitWatch(ctx context.Context, key string, nextAt int64, tries, reads int) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, ok := s.unitWatches[key]
	if !ok {
		return Result{}, ErrNotFound
	}
	if w.State != UnitWatchPending {
		return Result{}, nil
	}
	w.NextAt, w.Tries, w.Reads = nextAt, tries, reads
	s.unitWatches[key] = w
	return Result{UnitWatches: []UnitWatch{w}}, nil
}

func (s *ArcadeStore) DelayUnitWatch(ctx context.Context, key string, nextAt int64, tries, reads int) (Result, error) {
	return s.mutate(ctx, func() (Result, error) { return s.proj.DelayUnitWatch(ctx, key, nextAt, tries, reads) })
}

func (readOnly) DelayUnitWatch(context.Context, string, int64, int, int) (Result, error) {
	return Result{}, reject(replayRefusal)
}

// UnitWatches are a product's watches, as copies, by key.
func (s *MemStore) UnitWatches(ctx context.Context, productID string) ([]UnitWatch, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []UnitWatch
	for _, w := range s.unitWatches {
		if w.ProductID == productID {
			out = append(out, w)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

func (s *ArcadeStore) UnitWatches(ctx context.Context, productID string) ([]UnitWatch, error) {
	return s.proj.UnitWatches(ctx, productID)
}

func (readOnly) UnitWatches(context.Context, string) ([]UnitWatch, error) { return nil, nil }

// UnitWatch is one watch by its key, as a copy (D-106: the unit-results route).
func (s *MemStore) UnitWatch(ctx context.Context, key string) (UnitWatch, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	w, ok := s.unitWatches[strings.ToLower(key)]
	return w, ok, nil
}

func (s *ArcadeStore) UnitWatch(ctx context.Context, key string) (UnitWatch, bool, error) {
	return s.proj.UnitWatch(ctx, key)
}

func (readOnly) UnitWatch(context.Context, string) (UnitWatch, bool, error) {
	return UnitWatch{}, false, nil
}

// MarkUnitWatchConflict records that a done watch's "conflicting unit result"
// notice is being sent (D-106, §5.4 state 6c): the first conflicting post sets
// it and sends; later ones find it set and are only logged. Result.UnitWatches
// is empty when it was already set. No event: bookkeeping, like MarkUnitNotice.
func (s *MemStore) MarkUnitWatchConflict(ctx context.Context, key string) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key = strings.ToLower(key)
	w, ok := s.unitWatches[key]
	if !ok {
		return Result{}, ErrNotFound
	}
	if w.ConflictNoticeSent {
		return Result{}, nil
	}
	w.ConflictNoticeSent = true
	s.unitWatches[key] = w
	return Result{UnitWatches: []UnitWatch{w}}, nil
}

func (s *ArcadeStore) MarkUnitWatchConflict(ctx context.Context, key string) (Result, error) {
	return s.mutate(ctx, func() (Result, error) { return s.proj.MarkUnitWatchConflict(ctx, key) })
}

func (readOnly) MarkUnitWatchConflict(context.Context, string) (Result, error) {
	return Result{}, reject(replayRefusal)
}

// PruneUnitWatches deletes the done watches that ended before before (§4.2).
func (s *MemStore) PruneUnitWatches(ctx context.Context, before int64) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var res Result
	for k, w := range s.unitWatches {
		if w.State == UnitWatchDone && w.DoneAt < before {
			delete(s.unitWatches, k)
			res.UnitWatchDeletes = append(res.UnitWatchDeletes, k)
		}
	}
	sort.Strings(res.UnitWatchDeletes)
	return res, nil
}

func (s *ArcadeStore) PruneUnitWatches(ctx context.Context, before int64) (Result, error) {
	return s.mutate(ctx, func() (Result, error) { return s.proj.PruneUnitWatches(ctx, before) })
}

func (readOnly) PruneUnitWatches(context.Context, int64) (Result, error) {
	return Result{}, reject(replayRefusal)
}

// The notices MarkUnitNotice records (§5.3).
const (
	UnitNoticeHeld   = "held"
	UnitNoticePassed = "passed"
	UnitNoticeClosed = "closed"
	UnitNoticeNoCI   = "no-ci"
)

// MarkUnitNotice records that a unit notice was sent: "held" and "passed" set
// the defect's two flags, "closed" sets the passed flag of a defect a pass
// closed, and "no-ci" writes the product's LastNoCINoticeDay (key is ""). No
// event: it is bookkeeping of a send the stream already records.
func (s *MemStore) MarkUnitNotice(ctx context.Context, productID, key, which string) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.masterPtr(productID)
	if m == nil {
		return Result{}, ErrNotFound
	}
	if which == UnitNoticeNoCI {
		m.UnitChecks.LastNoCINoticeDay = time.Unix(autoFixNow(), 0).UTC().Format("2006-01-02")
		c := m.Clone()
		return Result{Master: &c}, nil
	}
	d := s.defects[key]
	if d == nil || d.ProductID != productID || d.Kind != DefectKindUnit {
		return Result{}, ErrNotFound
	}
	switch which {
	case UnitNoticeHeld:
		d.UnitNoticeSent = true
	case UnitNoticePassed, UnitNoticeClosed:
		d.UnitPassNoticeSent = true
	default:
		return Result{}, reject("not a unit notice")
	}
	return Result{Defects: []Defect{d.Clone()}}, nil
}

func (s *ArcadeStore) MarkUnitNotice(ctx context.Context, productID, key, which string) (Result, error) {
	return s.mutate(ctx, func() (Result, error) { return s.proj.MarkUnitNotice(ctx, productID, key, which) })
}

func (readOnly) MarkUnitNotice(context.Context, string, string, string) (Result, error) {
	return Result{}, reject(replayRefusal)
}

// restoreUnitWatches loads the watches read back from the graph. Boot only.
func (s *MemStore) restoreUnitWatches(ws []UnitWatch) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, w := range ws {
		s.unitWatches[w.Key] = w
	}
}

// skippedUnitChecks is Result.Skipped's word when the planner dropped the agent
// unit-test card for a watched repository (§4.2).
const skippedUnitChecks = "unit-checks"

// joinSkipped adds a reason to Result.Skipped.
func joinSkipped(have, add string) string {
	if have == "" {
		return add
	}
	return have + ";" + add
}

// unitDefaultCache keeps repositories' default branches for unitDefaultCacheTT.
type unitDefaultCache struct {
	mu sync.Mutex
	at map[string]unitDefault
}

type unitDefault struct {
	branch string
	read   time.Time
}

// defaultReader is the slice of the GitHub client the effective base needs.
type defaultReader interface {
	repoDefault(ctx context.Context, owner, repo string) (string, error)
}

// errNoDefaultReader says the flow's GitHub source cannot read a default branch.
var errNoDefaultReader = errors.New("no default-branch reader")

// unitDefaultBranch is owner/repo's default branch, read at most once per 10 minutes.
func (f *prFlow) unitDefaultBranch(ctx context.Context, owner, repo string) (string, error) {
	key := strings.ToLower(owner + "/" + repo)
	now := time.Now()
	f.unitDefaults.mu.Lock()
	if d, ok := f.unitDefaults.at[key]; ok && now.Sub(d.read) < unitDefaultCacheTT {
		f.unitDefaults.mu.Unlock()
		return d.branch, nil
	}
	f.unitDefaults.mu.Unlock()
	rd, ok := f.gh.(defaultReader)
	if !ok {
		return "", errNoDefaultReader
	}
	b, err := rd.repoDefault(ctx, owner, repo)
	if err != nil {
		return "", err
	}
	f.unitDefaults.mu.Lock()
	if f.unitDefaults.at == nil {
		f.unitDefaults.at = map[string]unitDefault{}
	}
	f.unitDefaults.at[key] = unitDefault{branch: b, read: now}
	f.unitDefaults.mu.Unlock()
	return b, nil
}

// plantUnitWatch is §4.2 for one merge of a watched repository: got is the fresh
// read of the pull request and rerr its permanent error, if any. A merge that
// cannot be read is recorded as a done watch "skipped:<class>", so the page and the
// metric see the miss; a merge into a base that is not a unit base plants nothing.
func (f *prFlow) plantUnitWatch(ctx context.Context, m Master, ev PREvent, got ghPR, rerr error) {
	repo := strings.ToLower(ev.Org + "/" + ev.Repo)
	now := autoFixNow()
	base := got.Base.Ref
	sha := strings.ToLower(got.MergeCommitSHA)
	skip := func(class string) {
		w := UnitWatch{Key: unitSkippedKey(m.ID, repo, ev.PRNumber), Repo: repo, PR: ev.PRNumber, SHA: unitZeroSHA,
			Base: unitBaseUnknown, State: UnitWatchDone, Outcome: "skipped:" + class, DoneAt: now}
		if rerr == nil && baseBranchRe.MatchString(base) {
			w.Base = base
		}
		if got.MergedAt != nil && !got.MergedAt.IsZero() {
			w.MergedAt = got.MergedAt.Unix()
		}
		if _, err := f.st.PlantUnitWatch(ctx, m.ID, w); err != nil {
			f.logf("prflow: the skipped unit watch could not be recorded: %v", err)
		}
		f.logf("prflow: unit-watch skipped:%s", class)
	}
	switch {
	case rerr != nil:
		skip("read")
		return
	case got.MergedAt == nil || got.MergedAt.IsZero():
		skip("merged-at")
		return
	case !shaRe.MatchString(sha) || !unitSHARe.MatchString(sha):
		skip("sha")
		return
	}
	effective := m.AutoFix.BaseBranch
	if effective == "" && !unitIsExtraBase(m.UnitChecks, base) {
		def, err := f.unitDefaultBranch(ctx, ev.Org, ev.Repo)
		if err != nil {
			f.logf("prflow: unit-watch-skipped default-%s", ghErrorClass(err))
			return
		}
		effective = def
	}
	if !unitWatchFor(m, ev.Org, ev.Repo, base, effective) {
		f.logf("prflow: no unit watch: the base is not a unit base")
		return
	}
	w := UnitWatch{Repo: repo, PR: ev.PRNumber, SHA: sha, Base: base, MergedAt: got.MergedAt.Unix(),
		FirstSeen: now, NextAt: now + int64(unitWaitFast/time.Second)}
	res, err := f.st.PlantUnitWatch(ctx, m.ID, w)
	switch {
	case err != nil:
		f.logf("prflow: the unit watch could not be planted: %v", err)
	case len(res.UnitWatches) > 0:
		f.logf("prflow: unit watch planted on %s", m.ID)
	}
}
