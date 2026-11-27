package main

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// The Pull requests page's read model (docs/designs/gaos-pr-overview.md, D-092).
//
// Fix pull requests stay in FixPR. Pull requests of the review flow and of QA-tool jobs are
// PullRequest rows: a cache of GitHub's state, kept outside the event floor like Defect and
// FixPR (BR-02 position in design §3.4). A row is keyed by product and owner/repo#n.

// The kinds and states of a PullRequest row.
const (
	PRKindReview = "review"
	PRKindQA     = "qa"

	PRStateOpen   = "open"
	PRStateDraft  = "draft"
	PRStateMerged = "merged"
	PRStateClosed = "closed"
)

// pullRequestPollLifetime is how long a non-final row is followed (design §4.2).
const pullRequestPollSeconds = 90 * 24 * 3600

// pullRequestPruneSeconds is how long a final row is kept after its last read.
const pullRequestPruneSeconds = 30 * 24 * 3600

// PullRequest is one review or QA-tool pull request of a product.
type PullRequest struct {
	Key       string // productId + "/" + owner/repo#n, lower-cased: the vertex id
	ProductID string
	Repo      string // owner/repo
	Number    int
	Kind      string
	State     string
	Title     string
	Author    string
	HeadRef   string
	BaseRef   string
	TaskID    string
	QAJob     string
	QACase    string
	QALink    string
	OpenedAt  int64
	CheckedAt int64
	// The PR template (D-100, prtemplate.go, bundles.go): the task-manager code, the parsed links
	// (owner/repo#n, JSON on the vertex), the bundle the row belongs to and when that last changed,
	// the alert already sent, and whether this code path has read the body (rows from before
	// v0.62.0 stay false and are never alerted).
	TaskCode     string
	Links        []string
	BundleKey    string
	BundledAt    int64
	Alerted      string
	TemplateSeen bool
}

// PRFields are the fields the poller fills from GitHub.
type PRFields struct {
	State     string
	Title     string
	Author    string
	HeadRef   string
	BaseRef   string
	CheckedAt int64
}

// pullRequestKey is the vertex id of a row.
func pullRequestKey(productID, repo string, n int) string {
	return strings.ToLower(productID + "/" + repo + "#" + fmt.Sprint(n))
}

// prFinal reports whether a state is one a row is never read in again.
func prFinal(state string) bool { return state == PRStateMerged || state == PRStateClosed }

// prRefOK says whether a branch name read from GitHub may be stored and shown.
func prRefOK(ref string) bool {
	return ref != "" && baseBranchRe.MatchString(ref) && !strings.Contains(ref, "..")
}

// prRepos are the repositories whose pull requests a kind of row may name: the product's own
// for a review row, the automation repositories for a QA row (design §4.4).
func prRepos(m *Master, kind string) []string {
	if kind == PRKindQA {
		return m.AutoFix.AutomationRepos
	}
	return m.Repos
}

// upsertPullRequestLocked inserts a row or fills the EMPTY fields of an existing one. It never
// changes the product, the kind or the task of an existing row, and never turns a QA row into a
// review row. It refuses a repository the product does not own for the row's kind. s.mu is held.
func (s *MemStore) upsertPullRequestLocked(m *Master, p PullRequest) (PullRequest, bool, error) {
	if p.Number <= 0 || !defectRepo.MatchString(p.Repo) {
		return PullRequest{}, false, reject("That is not a pull request.")
	}
	if p.Kind != PRKindReview && p.Kind != PRKindQA {
		return PullRequest{}, false, reject("That kind of pull request is not listed.")
	}
	owner, repo, _ := ghOwnerRepo(p.Repo)
	if !productOwns(prRepos(m, p.Kind), owner, repo) {
		return PullRequest{}, false, reject("That repository is not one the product follows for this kind of pull request.")
	}
	p.ProductID = m.ID
	p.Key = pullRequestKey(m.ID, p.Repo, p.Number)
	if !prRefOK(p.HeadRef) {
		p.HeadRef = ""
	}
	if !prRefOK(p.BaseRef) {
		p.BaseRef = ""
	}
	if p.State == "" {
		p.State = PRStateOpen
	}
	cur, ok := s.pullRequests[p.Key]
	if !ok {
		if p.OpenedAt == 0 {
			p.OpenedAt = autoFixNow()
		}
		s.pullRequests[p.Key] = p
		return p, true, nil
	}
	changed := false
	fill := func(dst *string, v string) {
		if *dst == "" && v != "" {
			*dst, changed = v, true
		}
	}
	fill(&cur.Title, p.Title)
	fill(&cur.Author, p.Author)
	fill(&cur.HeadRef, p.HeadRef)
	fill(&cur.BaseRef, p.BaseRef)
	fill(&cur.TaskID, p.TaskID)
	if cur.Kind == PRKindQA {
		fill(&cur.QAJob, p.QAJob)
		fill(&cur.QACase, p.QACase)
		fill(&cur.QALink, p.QALink)
	}
	if changed {
		s.pullRequests[p.Key] = cur
	}
	return cur, changed, nil
}

// UpsertPullRequest is the QA runner's entry point; the review planner calls the locked helper
// inside its own transaction.
func (s *MemStore) UpsertPullRequest(ctx context.Context, p PullRequest) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.masterPtr(p.ProductID)
	if m == nil {
		return Result{}, ErrNotFound
	}
	row, changed, err := s.upsertPullRequestLocked(m, p)
	if err != nil {
		return Result{}, err
	}
	if !changed {
		return Result{}, nil
	}
	return Result{PullRequests: []PullRequest{row}}, nil
}

func (s *ArcadeStore) UpsertPullRequest(ctx context.Context, p PullRequest) (Result, error) {
	return s.mutate(ctx, func() (Result, error) { return s.proj.UpsertPullRequest(ctx, p) })
}

func (readOnly) UpsertPullRequest(context.Context, PullRequest) (Result, error) {
	return Result{}, reject(replayRefusal)
}

// PullRequests are a product's rows, newest first.
func (s *MemStore) PullRequests(ctx context.Context, productID string) ([]PullRequest, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []PullRequest
	for _, p := range s.pullRequests {
		if p.ProductID == productID {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].OpenedAt != out[j].OpenedAt {
			return out[i].OpenedAt > out[j].OpenedAt
		}
		return out[i].Key < out[j].Key
	})
	return out, nil
}

func (s *ArcadeStore) PullRequests(ctx context.Context, productID string) ([]PullRequest, error) {
	return s.proj.PullRequests(ctx, productID)
}

func (readOnly) PullRequests(context.Context, string) ([]PullRequest, error) { return nil, nil }

// AdvancePullRequest is the poller's write: what GitHub says of a row. A branch name that does
// not look like one is dropped, so nothing GitHub-controlled that is not a branch is stored.
func (s *MemStore) AdvancePullRequest(ctx context.Context, productID, key string, f PRFields) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.pullRequests[key]
	if !ok || cur.ProductID != productID {
		return Result{}, ErrNotFound
	}
	if f.State != "" {
		cur.State = f.State
	}
	if f.Title != "" {
		cur.Title = afLineText(f.Title, 200)
	}
	if f.Author != "" {
		cur.Author = afLineText(f.Author, 60)
	}
	if prRefOK(f.HeadRef) {
		cur.HeadRef = f.HeadRef
	}
	if prRefOK(f.BaseRef) {
		cur.BaseRef = f.BaseRef
	}
	cur.CheckedAt = f.CheckedAt
	s.pullRequests[key] = cur
	return Result{PullRequests: []PullRequest{cur}}, nil
}

func (s *ArcadeStore) AdvancePullRequest(ctx context.Context, productID, key string, f PRFields) (Result, error) {
	return s.mutate(ctx, func() (Result, error) { return s.proj.AdvancePullRequest(ctx, productID, key, f) })
}

func (readOnly) AdvancePullRequest(context.Context, string, string, PRFields) (Result, error) {
	return Result{}, reject(replayRefusal)
}

// PrunePullRequests removes the rows in a final state last read before finalBefore.
func (s *MemStore) PrunePullRequests(ctx context.Context, finalBefore int64) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var res Result
	for k, p := range s.pullRequests {
		// A member of a bundle not yet planned is kept (D-100 §7.2).
		if prFinal(p.State) && p.CheckedAt > 0 && p.CheckedAt < finalBefore && !s.pruneKeepLocked(p) {
			delete(s.pullRequests, k)
			res.PullRequestDeletes = append(res.PullRequestDeletes, k)
		}
	}
	sort.Strings(res.PullRequestDeletes)
	return res, nil
}

func (s *ArcadeStore) PrunePullRequests(ctx context.Context, finalBefore int64) (Result, error) {
	return s.mutate(ctx, func() (Result, error) { return s.proj.PrunePullRequests(ctx, finalBefore) })
}

func (readOnly) PrunePullRequests(context.Context, int64) (Result, error) { return Result{}, nil }

// restorePullRequests loads the rows at boot.
func (s *MemStore) restorePullRequests(ps []PullRequest) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range ps {
		s.pullRequests[p.Key] = p
	}
}

// afLineText cuts a GitHub-controlled line: control and invisible characters out, a length cap.
func afLineText(s string, max int) string {
	s = stripBidi(strings.TrimSpace(s))
	var b strings.Builder
	n := 0
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			r = ' '
		}
		if n >= max {
			break
		}
		b.WriteRune(r)
		n++
	}
	return strings.TrimSpace(b.String())
}

// liveFixCard reports whether a fix card is one a turn may still start or finish on.
func liveFixCard(t *TaskRecord) bool {
	switch t.Status {
	case StatusPending, StatusReady, StatusDispatched, StatusRunning, StatusReview, StatusAwaitingInput:
		return true
	}
	return false
}

// fixPRReplacedLocked says whether a fix record was replaced by a retarget (RetargetedTo set), or
// has one pending: the defect names it as its pull request, carries a base override and its fix
// card is live (design §4.2). s.mu is held.
func (s *MemStore) fixPRReplacedLocked(cur FixPR, d *Defect) (replaced, pending bool) {
	if cur.RetargetedTo != "" {
		return true, false
	}
	if d == nil || d.BaseOverride == "" || d.FixPR != cur.ID {
		return false, false
	}
	t := s.taskByIDLocked(d.FixTask)
	return false, t != nil && liveFixCard(t)
}

// reviewRowLocked upserts the row of a pr.opened event of a repository the product follows and
// answers the rows that changed. A merge event writes none, and none is written for a pull request
// that already is a fix record (a fix pull request's merge must not shadow it). s.mu is held.
func (s *MemStore) reviewRowLocked(m *Master, ev PREvent) []PullRequest {
	if ev.Type != prTypeOpened || ev.PRNumber <= 0 {
		return nil
	}
	repo := ev.Org + "/" + ev.Repo
	for _, f := range s.fixPRs {
		if f.ProductID == m.ID && strings.EqualFold(f.Repo, repo) && f.Number == ev.PRNumber {
			return nil
		}
	}
	task := ""
	if t := s.taskByDedupKeyLocked(ev.Key(prJobReviewer)); t != nil {
		task = t.ID
	}
	row, changed, err := s.upsertPullRequestLocked(m, PullRequest{Repo: repo, Number: ev.PRNumber, Kind: PRKindReview,
		State: PRStateOpen, Title: afLineText(ev.Title, 200), TaskID: task})
	if err != nil || !changed {
		return nil
	}
	return []PullRequest{row}
}

// BackfillReviewRows writes a row for every pull request that has a live review card and no row
// yet: the pull requests already open when this release starts (design §4.3). It is idempotent.
func (s *MemStore) BackfillReviewRows(ctx context.Context) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var res Result
	for i := range s.tasks {
		t := &s.tasks[i]
		owner, rp, n, ok := prKeyParts(t.DedupKey)
		job := t.DedupKey[strings.LastIndex(t.DedupKey, ":")+1:]
		if !ok || job != prJobReviewer || t.Status == StatusDone || t.Status == StatusCanceled || t.Status == StatusFailed {
			continue
		}
		repo := owner + "/" + rp
		m := s.masterPtr(t.DeskID)
		if m == nil {
			continue
		}
		row, changed, err := s.upsertPullRequestLocked(m, PullRequest{Repo: repo, Number: n, Kind: PRKindReview,
			State: PRStateOpen, TaskID: t.ID})
		if err == nil && changed {
			res.PullRequests = append(res.PullRequests, row)
		}
	}
	return res, nil
}

func (s *ArcadeStore) BackfillReviewRows(ctx context.Context) (Result, error) {
	return s.mutate(ctx, func() (Result, error) { return s.proj.BackfillReviewRows(ctx) })
}

func (readOnly) BackfillReviewRows(context.Context) (Result, error) { return Result{}, nil }

// parseGitHubPRURL reads https://github.com/<owner>/<repo>/pull/<n> and nothing else: no other
// host, no user info, no query or fragment.
func parseGitHubPRURL(raw string) (owner, repo string, n int, ok bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", "", 0, false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 4 || parts[2] != "pull" {
		return "", "", 0, false
	}
	o, r, found := ghOwnerRepo(parts[0] + "/" + parts[1])
	num, err := strconv.Atoi(parts[3])
	if !found || err != nil || num <= 0 {
		return "", "", 0, false
	}
	return o, r, num, true
}

// qaLinkOK returns the job's page only if it can be trusted as a link: the origin GAOS_QA_ORIGIN
// names is set, the url is https without user info, query or fragment, and its host (port
// included) equals the origin exactly. "" otherwise (design §4.4). The origin that qaOrigin falls
// back to (the API host) does not count: only the explicit setting is passed here.
func qaLinkOK(origin, raw string) string {
	origin = strings.ToLower(strings.TrimSpace(origin))
	if origin == "" || raw == "" {
		return ""
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		u.Host == "" || strings.ToLower(u.Host) != origin {
		return ""
	}
	return u.String()
}
