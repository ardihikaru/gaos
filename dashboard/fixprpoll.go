package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"
)

// Following the pull request (D-091, design §9.1) and the excerpt retention
// (§5.1). One loop, one goroutine: every non-final FixPR with a number is read
// from GitHub with the READ token every 5 minutes for its first 7 days, then
// hourly, and no longer after 30 days. A change is one transaction: the record,
// FixPRStateChanged and the defect's move (§5.2). The loop keeps nothing a
// restart would lose: it recomputes from the stored records.

// The poll cadence.
const (
	fixPRPollTick     = 30 * time.Second
	fixPRPollFast     = 5 * time.Minute
	fixPRPollSlow     = time.Hour
	fixPRFastWindow   = 7 * 24 * time.Hour
	fixPRPollLifetime = 30 * 24 * time.Hour
	fixPRBackoffCap   = time.Hour
)

// BlockPRClosed is the reason of the card planned when a pull request closes
// unmerged.
const BlockPRClosed = "autofix-pr-closed"

// RuleFixPRPoll names the actor of a move the poller makes.
const RuleFixPRPoll = "autofix-pr-poll"

// ghPRState is what the poller reads of a pull request.
type ghPRState struct {
	Draft, Merged bool
	State         string
	Title, Author string
	Head, Base    string
	Body          string // the PR template (D-100); pullRequest already reads it
	HeadSHA       string // D-103 §4.2: a merged spec runs only at its approved commit
}

// pullState reads a pull request's draft flag, merged flag, state, title, author and branches.
func (c *ghClient) pullState(ctx context.Context, owner, repo string, n int) (ghPRState, error) {
	pr, err := c.pullRequest(ctx, owner, repo, n)
	if err != nil {
		return ghPRState{}, err
	}
	return ghPRState{Draft: pr.Draft, Merged: pr.Merged, State: pr.State, Title: pr.Title,
		Author: pr.User.Login, Head: pr.Head.Ref, Base: pr.Base.Ref, Body: pr.Body, HeadSHA: pr.Head.SHA}, nil
}

// prRowStateOf maps what GitHub says to a PullRequest row's state.
func prRowStateOf(s ghPRState) string {
	switch {
	case s.Merged:
		return PRStateMerged
	case s.State == "closed":
		return PRStateClosed
	case s.Draft:
		return PRStateDraft
	}
	return PRStateOpen
}

// fixPRFinal reports whether a record's state is one that is never polled again.
func fixPRFinal(state string) bool { return state == FixPRMerged || state == FixPRClosed }

// fixPRStateOf maps what GitHub says to the record's state.
func fixPRStateOf(draft, merged bool, state string) string {
	switch {
	case merged:
		return FixPRMerged
	case state == "closed":
		return FixPRClosed
	case draft:
		return FixPRDraft
	}
	return FixPRReady
}

// fixPRInterval is how long to wait between two reads of a record of this age;
// stop is true once the record is past its 30 days.
func fixPRInterval(age time.Duration) (every time.Duration, stop bool) {
	switch {
	case age >= fixPRPollLifetime:
		return 0, true
	case age < fixPRFastWindow:
		return fixPRPollFast, false
	}
	return fixPRPollSlow, false
}

// FixPollStatus is what the Auto-fix view shows of the poller. LastError is a
// class (ghErrorClass), never a response body.
type FixPollStatus struct {
	LastPoll  time.Time
	LastError string
	Polled    int // reads in the last pass
}

// fixPRPoller is the loop's state.
type fixPRPoller struct {
	st   Store
	gh   *ghClient // nil: only the retention runs
	now  func() time.Time
	logf func(string, ...any)

	mu    sync.Mutex
	due   map[string]time.Time
	fails map[string]int
	// baseTried marks a fix record whose base was read once after its 30 days.
	baseTried map[string]bool
	lastDay   int64
	status    FixPollStatus
	haveDay   bool
	// alert posts the missing-code comment and line (D-100 §7.4); set in main.go.
	alert prAlerter
	// ghSrc replaces gh for the bundle pass in tests.
	ghSrc bundleSource
	// tm is the TaskMgr link (D-104): it validates a row's task-manager code. nil: no check.
	tm *taskmgrLink
	// The unit checks (D-105, unitpoll.go): the pass counter, the product the last unit
	// pass served, the context builds tried, and the group notice (nil: none is sent).
	unitN      int
	unitLast   string
	unitCtx    map[string]time.Time
	unitNotify func(ctx context.Context, productID string, pr int, text, cardID string)
}

// bundleGH is the GitHub source of the bundle pass: nil when there is none.
func (p *fixPRPoller) bundleGH() bundleSource {
	if p.ghSrc != nil {
		return p.ghSrc
	}
	if p.gh != nil {
		return p.gh
	}
	return nil
}

func newFixPRPoller(st Store, gh *ghClient) *fixPRPoller {
	return &fixPRPoller{st: st, gh: gh, now: time.Now, logf: log.Printf,
		due: map[string]time.Time{}, fails: map[string]int{}, baseTried: map[string]bool{}}
}

// Status answers the poller's last poll and last error class.
func (p *fixPRPoller) Status() FixPollStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.status
}

// Run passes until ctx ends.
func (p *fixPRPoller) Run(ctx context.Context) {
	p.Pass(ctx)
	tk := time.NewTicker(fixPRPollTick)
	defer tk.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tk.C:
			p.Pass(ctx)
		}
	}
}

// Pass is one pass: the retention once per UTC day, then every due record.
func (p *fixPRPoller) Pass(ctx context.Context) {
	f, err := p.st.Snapshot(ctx)
	if err != nil || f.Stats.Replay {
		return
	}
	now := p.now()
	p.retain(ctx, now)
	// A review that ends in REVIEW after its pull request merged has no poll left
	// to finish it; this sweep does, every tick (design §3.1).
	if _, err := p.st.FinishMergedReviews(ctx); err != nil {
		p.logf("pull requests: merged reviews could not be finished: %v", err)
	}
	if p.gh == nil {
		return
	}
	polled := 0
	for _, d := range f.Desks {
		m, err := p.st.Master(ctx, d.ID)
		if err != nil {
			continue
		}
		recs, err := p.st.FixPRs(ctx, d.ID)
		if err != nil {
			continue
		}
		sort.Slice(recs, func(i, j int) bool { return recs[i].ID < recs[j].ID })
		for _, r := range recs {
			if ctx.Err() != nil {
				return
			}
			if p.pollOne(ctx, r, m, now) {
				polled++
			}
		}
		rows, err := p.st.PullRequests(ctx, d.ID)
		if err != nil {
			continue
		}
		for _, row := range rows {
			if ctx.Err() != nil {
				return
			}
			if p.pollRow(ctx, row, m, now) {
				polled++
			}
		}
	}
	if polled > 0 {
		p.mu.Lock()
		p.status.LastPoll, p.status.Polled = now, polled
		p.mu.Unlock()
	}
	p.bundlePass(ctx, now)
	// The unit checks (D-105 §5.1): every fourth pass, 2 minutes at the 30 s tick.
	p.mu.Lock()
	run := p.unitN%unitPassEvery == 0
	p.unitN++
	p.mu.Unlock()
	if run {
		p.unitPass(ctx, now)
	}
}

// retain empties the excerpts of records last seen 30 days ago, once per UTC day.
func (p *fixPRPoller) retain(ctx context.Context, now time.Time) {
	day := now.Unix() / 86400
	p.mu.Lock()
	if p.haveDay && p.lastDay == day {
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()
	// The unit watches' retention (D-105 §4.2): its own branch, so a failed excerpt
	// retention never skips it.
	if _, err := p.st.PruneUnitWatches(ctx, now.Unix()-unitWatchKeepDays*86400); err != nil {
		p.logf("unit checks: pruning the watches failed: %v", err)
	}
	if _, err := p.st.ExpireExcerpts(ctx, now.Unix()); err != nil {
		p.logf("autofix: the excerpt retention failed: %v", err)
		return
	}
	if _, err := p.st.PrunePullRequests(ctx, now.Unix()-pullRequestPruneSeconds); err != nil {
		p.logf("pull requests: pruning failed: %v", err)
		return
	}
	p.mu.Lock()
	p.lastDay, p.haveDay = day, true
	p.mu.Unlock()
}

// pollOne reads one fix record if it is due. It answers whether it read GitHub.
func (p *fixPRPoller) pollOne(ctx context.Context, r FixPR, m Master, now time.Time) bool {
	if fixPRFinal(r.State) || r.Number <= 0 || (r.State != FixPRDraft && r.State != FixPRReady) {
		return false
	}
	owner, repo, ok := ghOwnerRepo(r.Repo)
	if !ok || !productOwns(defectOwnedRepos(m), owner, repo) {
		return false // a repository the product no longer owns is not read (design §4.2)
	}
	every, stop := fixPRInterval(now.Sub(time.Unix(r.CreatedAt, 0)))
	if stop {
		// Past the 30 days a record with no base is read once more to learn it (design §4.2).
		p.mu.Lock()
		tried := p.baseTried[r.ID]
		p.mu.Unlock()
		if r.Base != "" || tried {
			return false
		}
		every = fixPRPollSlow
	}
	p.mu.Lock()
	if t, ok := p.due[r.ID]; ok && now.Before(t) {
		p.mu.Unlock()
		return false
	}
	p.mu.Unlock()
	gs, err := p.gh.pullState(ctx, owner, repo, r.Number)
	p.mu.Lock()
	defer p.mu.Unlock()
	if err != nil {
		if ctx.Err() != nil {
			return true
		}
		p.fails[r.ID]++
		wait := every
		for i := 0; i < p.fails[r.ID] && wait < fixPRBackoffCap; i++ {
			wait *= 2
		}
		wait = min(wait, fixPRBackoffCap)
		p.due[r.ID] = now.Add(wait)
		p.status.LastError = ghErrorClass(err)
		p.logf("autofix: reading %s#%d failed: %s", r.Repo, r.Number, p.status.LastError)
		return true
	}
	delete(p.fails, r.ID)
	p.due[r.ID] = now.Add(every)
	p.status.LastError = ""
	p.baseTried[r.ID] = true
	to := fixPRStateOf(gs.Draft, gs.Merged, gs.State)
	base := ""
	if prRefOK(gs.Base) {
		base = gs.Base
	}
	// The poll writes when the state changed OR the base is empty and now known (the back-fill
	// for a record opened before the base was kept).
	if to == r.State && (r.Base != "" || base == "") {
		return true
	}
	next := r
	next.State, next.UpdatedAt = to, now.Unix()
	if r.Base == "" {
		next.Base = base
	}
	p.mu.Unlock()
	_, err = p.st.AdvanceFixPR(ctx, r.ProductID, next)
	p.mu.Lock()
	if err != nil && !errors.Is(err, ErrNotFound) {
		p.logf("autofix: the state of %s#%d could not be recorded: %v", r.Repo, r.Number, err)
		delete(p.due, r.ID)
	}
	return true
}

// pollRow reads one review or QA pull-request row if it is due: the same cadence as a fix record
// for 7 days, hourly after, and no longer after 90 days (design §4.2). The row's repository must
// still be one the product follows for its kind.
func (p *fixPRPoller) pollRow(ctx context.Context, r PullRequest, m Master, now time.Time) bool {
	if prFinal(r.State) {
		return false
	}
	owner, repo, ok := ghOwnerRepo(r.Repo)
	if !ok || !productOwns(prRepos(&m, r.Kind), owner, repo) {
		return false
	}
	age := now.Sub(time.Unix(r.OpenedAt, 0))
	if age >= time.Duration(pullRequestPollSeconds)*time.Second {
		return false
	}
	every := fixPRPollFast
	if age >= fixPRFastWindow {
		every = fixPRPollSlow
	}
	id := "row:" + r.Key
	p.mu.Lock()
	if t, ok := p.due[id]; ok && now.Before(t) {
		p.mu.Unlock()
		return false
	}
	p.mu.Unlock()
	gs, err := p.gh.pullState(ctx, owner, repo, r.Number)
	p.mu.Lock()
	if err != nil {
		if ctx.Err() != nil {
			p.mu.Unlock()
			return true
		}
		p.fails[id]++
		wait := every
		for i := 0; i < p.fails[id] && wait < fixPRBackoffCap; i++ {
			wait *= 2
		}
		p.due[id] = now.Add(min(wait, fixPRBackoffCap))
		p.status.LastError = ghErrorClass(err)
		p.logf("pull requests: reading %s#%d failed: %s", r.Repo, r.Number, p.status.LastError)
		p.mu.Unlock()
		return true
	}
	delete(p.fails, id)
	p.due[id] = now.Add(every)
	p.status.LastError = ""
	p.mu.Unlock()
	f := PRFields{State: prRowStateOf(gs), Title: gs.Title, Author: gs.Author, HeadRef: gs.Head, BaseRef: gs.Base, CheckedAt: now.Unix(), HeadSHA: gs.HeadSHA}
	if _, err := p.st.AdvancePullRequest(ctx, r.ProductID, r.Key, f); err != nil && !errors.Is(err, ErrNotFound) {
		p.logf("pull requests: %s#%d could not be recorded: %v", r.Repo, r.Number, err)
		p.mu.Lock()
		delete(p.due, id)
		p.mu.Unlock()
		return true
	}
	// D-100: a listed product's review row re-reads its template on every poll until its
	// bundle is planned, then the missing-code alert is weighed.
	if r.Kind == PRKindReview && templateProducts()[r.ProductID] {
		tmpl := parsePRTemplate(gs.Body)
		if row, _, err := p.st.ApplyBundleLinks(ctx, r.ProductID, r.Key, tmpl, now.Unix()); err == nil {
			p.maybeAlert(ctx, row, tmpl, now)
			p.maybeAlertUnknown(ctx, row, now)
		}
	}
	return true
}

// ---------------------------------------------------------------- the store half

// AdvanceFixPR records a state change of a pull request: the record, the
// FixPRStateChanged event and, from §5.2, the defect's move, in one
// transaction. A record already in that state, or already final, is left alone.
func (s *MemStore) AdvanceFixPR(ctx context.Context, productID string, f FixPR) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.fixPRs[f.ID]
	if !ok || cur.ProductID != productID || s.masterPtr(productID) == nil {
		return Result{}, ErrNotFound
	}
	// A review fix whose base changed under it (GitHub retargets a draft whose base
	// branch went away after a merge, D-102 §7): the change is recorded and the
	// defect closed; GAOS closes nothing on GitHub, a person decides.
	if d := s.defects[cur.DefectKey]; d != nil && d.Kind == DefectKindReview && cur.Base != "" && f.Base != "" &&
		prRefOK(f.Base) && !strings.EqualFold(cur.Base, f.Base) {
		cur.Base = f.Base
		s.fixPRs[cur.ID] = cur
		res := Result{FixPRs: []FixPR{cur}}
		if !defectFinal(d.State) {
			res.NewEvents = []Event{s.setDefectStateLocked(d, DefectStateClosed, ReviewBaseChanged, "")}
			res.Defects = []Defect{d.Clone()}
		}
		return res, nil
	}
	// The base back-fill: a record with no base learns it even if its state did not change.
	if cur.Base == "" && f.Base != "" && prRefOK(f.Base) {
		cur.Base = f.Base
		if fixPRFinal(cur.State) || cur.State == f.State {
			s.fixPRs[cur.ID] = cur
			return Result{FixPRs: []FixPR{cur}}, nil // a cache fill: no event (design §3.4)
		}
	}
	if fixPRFinal(cur.State) || cur.State == f.State {
		return Result{}, nil
	}
	from := cur.State
	cur.State, cur.UpdatedAt = f.State, f.UpdatedAt
	s.fixPRs[cur.ID] = cur
	d := s.defects[cur.DefectKey]
	ref := cur.DefectKey
	if d != nil {
		ref = d.Ref()
	}
	evs := []Event{s.defectEventLocked(FixPRStateChanged, productID,
		fmt.Sprintf("pull request %s#%d of defect %s: %s → %s", cur.Repo, cur.Number, ref, from, cur.State), KindInfo,
		EventPayload{Task: cur.DefectKey, Rule: RuleFixPRPoll, To: cur.State, Reason: from + "→" + cur.State})}
	res := Result{FixPRs: []FixPR{cur}}
	// A record a retarget replaced, or one whose retarget is pending, updates itself and leaves the
	// defect alone (design §4.2); a merge while the retarget is pending lands the fix.
	replaced, pending := s.fixPRReplacedLocked(cur, d)
	switch {
	case replaced:
		// its own state only
	case pending && cur.State == FixPRMerged && d != nil && d.ProductID == productID:
		evs = append(evs, s.setDefectStateLocked(d, DefectStateFixed, RuleFixPRPoll, ""))
		d.BaseOverride = ""
		if t := s.taskByIDLocked(d.FixTask); t != nil && liveFixCard(t) {
			if blocked, err := s.resolveCardLocked(t, StatusBlocked, BlockSuperseded, RuleFixPRPoll); err == nil {
				evs = append(evs, blocked...)
			}
		}
		res.Defects = []Defect{d.Clone()}
	case pending:
		// a close or a draft/ready change while the retarget is pending: the defect is left alone
	case d != nil && d.ProductID == productID && d.State == DefectStateFixing:
		switch cur.State {
		case FixPRMerged:
			evs = append(evs, s.setDefectStateLocked(d, DefectStateFixed, RuleFixPRPoll, ""))
			d.BaseOverride = ""
			res.Defects = []Defect{d.Clone()}
		case FixPRClosed:
			if d.Kind == DefectKindReview {
				// A review fix closed unmerged is done with (D-102 §7): no new generation.
				evs = append(evs, s.setDefectStateLocked(d, DefectStateClosed, ReviewFixClosed, ""))
				res.Defects = []Defect{d.Clone()}
				break
			}
			// A person-only second attempt: never an easy card, never automatic.
			d.Gen++
			d.BaseOverride = "" // any generation change the retarget did not cause ends its override
			d.Difficulty = "medium"
			evs = append(evs, s.setDefectStateLocked(d, DefectStateTriaged, RuleFixPRPoll, ""))
			_, planned := s.planDefectCardLocked(d, defectJobFix, StatusBlocked, BlockPRClosed, LabelDefectMedium)
			evs = append(evs, planned...)
			res.Defects = []Defect{d.Clone()}
		}
	}
	res.Floor, res.NewEvents = s.floor(), evs
	return res, nil
}

func (s *ArcadeStore) AdvanceFixPR(ctx context.Context, productID string, f FixPR) (Result, error) {
	return s.mutate(ctx, func() (Result, error) { return s.proj.AdvanceFixPR(ctx, productID, f) })
}

func (readOnly) AdvanceFixPR(context.Context, string, FixPR) (Result, error) {
	return Result{}, reject(replayRefusal)
}

// ExpireExcerpts empties the excerpt of every record last seen 30 days before
// now (§5.1). A silent field clear: no event.
func (s *MemStore) ExpireExcerpts(ctx context.Context, now int64) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var res Result
	for _, d := range s.defects {
		// A unit defect's failing tests go with its excerpt (D-105 §7.5); UnitFiles stay.
		if (d.Excerpt != "" || d.UnitTests != "") && now-d.LastSeen >= defectExcerptDays*86400 {
			d.Excerpt, d.UnitTests = "", ""
			res.Defects = append(res.Defects, d.Clone())
		}
	}
	sort.Slice(res.Defects, func(i, j int) bool { return res.Defects[i].Key < res.Defects[j].Key })
	return res, nil
}

func (s *ArcadeStore) ExpireExcerpts(ctx context.Context, now int64) (Result, error) {
	return s.mutate(ctx, func() (Result, error) { return s.proj.ExpireExcerpts(ctx, now) })
}

func (readOnly) ExpireExcerpts(context.Context, int64) (Result, error) {
	return Result{}, nil
}
