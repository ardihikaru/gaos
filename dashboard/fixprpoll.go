package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
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

// pullState reads a pull request's draft flag, merged flag and state.
func (c *ghClient) pullState(ctx context.Context, owner, repo string, n int) (draft, merged bool, state string, err error) {
	pr, err := c.pullRequest(ctx, owner, repo, n)
	if err != nil {
		return false, false, "", err
	}
	return pr.Draft, pr.Merged, pr.State, nil
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

	mu      sync.Mutex
	due     map[string]time.Time
	fails   map[string]int
	lastDay int64
	status  FixPollStatus
	haveDay bool
}

func newFixPRPoller(st Store, gh *ghClient) *fixPRPoller {
	return &fixPRPoller{st: st, gh: gh, now: time.Now, logf: log.Printf,
		due: map[string]time.Time{}, fails: map[string]int{}}
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
	if p.gh == nil {
		return
	}
	polled := 0
	for _, d := range f.Desks {
		recs, err := p.st.FixPRs(ctx, d.ID)
		if err != nil {
			continue
		}
		sort.Slice(recs, func(i, j int) bool { return recs[i].ID < recs[j].ID })
		for _, r := range recs {
			if ctx.Err() != nil {
				return
			}
			if p.pollOne(ctx, r, now) {
				polled++
			}
		}
	}
	if polled > 0 {
		p.mu.Lock()
		p.status.LastPoll, p.status.Polled = now, polled
		p.mu.Unlock()
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
	if _, err := p.st.ExpireExcerpts(ctx, now.Unix()); err != nil {
		p.logf("autofix: the excerpt retention failed: %v", err)
		return
	}
	p.mu.Lock()
	p.lastDay, p.haveDay = day, true
	p.mu.Unlock()
}

// pollOne reads one record if it is due. It answers whether it read GitHub.
func (p *fixPRPoller) pollOne(ctx context.Context, r FixPR, now time.Time) bool {
	if fixPRFinal(r.State) || r.Number <= 0 || (r.State != FixPRDraft && r.State != FixPRReady) {
		return false
	}
	every, stop := fixPRInterval(now.Sub(time.Unix(r.CreatedAt, 0)))
	if stop {
		return false
	}
	p.mu.Lock()
	if t, ok := p.due[r.ID]; ok && now.Before(t) {
		p.mu.Unlock()
		return false
	}
	p.mu.Unlock()
	owner, repo, ok := ghOwnerRepo(r.Repo)
	if !ok {
		return false
	}
	draft, merged, state, err := p.gh.pullState(ctx, owner, repo, r.Number)
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
	to := fixPRStateOf(draft, merged, state)
	if to == r.State {
		return true
	}
	next := r
	next.State, next.UpdatedAt = to, now.Unix()
	p.mu.Unlock()
	_, err = p.st.AdvanceFixPR(ctx, r.ProductID, next)
	p.mu.Lock()
	if err != nil && !errors.Is(err, ErrNotFound) {
		p.logf("autofix: the state of %s#%d could not be recorded: %v", r.Repo, r.Number, err)
		delete(p.due, r.ID)
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
	if d != nil && d.ProductID == productID && d.State == DefectStateFixing {
		switch cur.State {
		case FixPRMerged:
			evs = append(evs, s.setDefectStateLocked(d, DefectStateFixed, RuleFixPRPoll, ""))
			res.Defects = []Defect{d.Clone()}
		case FixPRClosed:
			// A person-only second attempt: never an easy card, never automatic.
			d.Gen++
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
		if d.Excerpt != "" && now-d.LastSeen >= defectExcerptDays*86400 {
			d.Excerpt = ""
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
