package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// The pull-request flow (D-088, docs/designs/gaos-pr-agent-flows.md §6).
//
// A pull-request event arrives from Brain Town's reader, and GAOS plans the
// cards the work needs:
//
//	pr.opened          -> a PR review card and an e2e automation card
//	pr.merged          -> a unit test card
//	staging.deployed   -> an e2e run card (the staging callback, §8.2)
//
// The cards live on the product's PR workflow, never on the desk's own, so a
// pull request cannot reset the product's plan, progress or score.
//
// Three rules matter:
//
//   - The repository GAOS acts on is the PRODUCT's. The event says which
//     repository it saw; the product says which repositories it owns.
//   - A card carries its de-duplication key. A pull request that arrives twice
//     (the reader reconnects and replays) finds the card already there and
//     plans nothing. The store IS the record, so a restart forgets nothing.
//   - A draft, a fork and a burst are refused before they cost a model turn.

// ErrPRSkipped says an event was understood and deliberately not acted on. The
// message names the class and nothing the author controls.
var ErrPRSkipped = errors.New("pull request skipped")

// prWorkflowID is the product's PR workflow.
func prWorkflowID(productID string) string { return "wf-pr-" + productID }

// The jobs a card can be, and the key suffix each one carries.
const (
	prJobReviewer   = "pr-reviewer"
	prJobE2EBuild   = "e2e-automation"
	prJobE2ERun     = "e2e-run"
	prJobUnitTest   = "unit-test"
	prTypeOpened    = "pr.opened"
	prTypeMerged    = "pr.merged"
	prTypeStaging   = "staging.deployed"
	prDefaultMaxOpn = 5
)

// prMaxOpen is the number of open PR cards a product may hold (GAOS_PR_MAX_OPEN,
// default 5). A burst of pull requests is refused past it, so it cannot starve
// the product's own work.
func prMaxOpen() int {
	if n, err := strconv.Atoi(strings.TrimSpace(os.Getenv("GAOS_PR_MAX_OPEN"))); err == nil && n > 0 {
		return n
	}
	return prDefaultMaxOpn
}

// prAllowForks reports whether a pull request from a fork may open cards
// (GAOS_PR_ALLOW_FORKS=1). Off by default: a stranger's pull request would
// otherwise spend a model turn.
func prAllowForks() bool { return strings.TrimSpace(os.Getenv("GAOS_PR_ALLOW_FORKS")) == "1" }

// prCardSpec is one card of the routing table.
type prCardSpec struct {
	Job    string
	Role   string
	Labels []string
	Title  string
}

// prCardsFor is the routing table (§6.1). Every card is an implement task: a
// "review" task in GAOS is the reviewer of a written design, which a pull
// request is not.
func prCardsFor(ev PREvent) []prCardSpec {
	ref := ev.Repo + "#" + strconv.Itoa(ev.PRNumber)
	switch ev.Type {
	case prTypeOpened:
		return []prCardSpec{
			{Job: prJobReviewer, Role: "pr-reviewer", Labels: []string{LabelPRReviewAuto}, Title: "Review " + ref},
			{Job: prJobE2EBuild, Role: "qa", Labels: []string{LabelE2EAutomation}, Title: "E2E automation for " + ref},
		}
	case prTypeMerged:
		return []prCardSpec{
			{Job: prJobUnitTest, Role: "unit-test", Labels: []string{LabelUnitTest}, Title: "Unit tests for " + ref},
		}
	case prTypeStaging:
		return []prCardSpec{
			{Job: prJobE2ERun, Role: "qa", Labels: []string{LabelE2ERun},
				Title: "E2E run on staging " + ev.Repo + "@" + shortSHA(ev.Commit)},
		}
	}
	return nil
}

// idSHA is the part of a commit a card id carries: 12 characters, so two
// commits of one repository sharing a prefix is a 1-in-2^48 event, and the
// planner still checks the key rather than trusting the id (see freeCardID).
func idSHA(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

func shortSHA(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

// prTitle is a card title made safe: it is capped, stripped of control
// characters and screened before it reaches a card, because the outbound Brain
// Town feed republishes titles. A title that fails the screen is replaced by
// the card's id (§7.1).
func prTitle(title, id string) string { return btTitle(true, id, title) }

// sanitizeIDPart keeps the letters, digits and dashes of a name, and turns
// everything else into a dash.
func sanitizeIDPart(s string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			sb.WriteRune(r)
		default:
			sb.WriteRune('-')
		}
	}
	out := strings.Trim(sb.String(), "-")
	if out == "" {
		out = "x"
	}
	return out
}

// prCardID is a card's id: readable, and unique per key.
func prCardID(workflow string, ev PREvent, job string) string {
	if ev.PRNumber == 0 {
		return workflow + "-" + sanitizeIDPart(ev.Org+"-"+ev.Repo) + "-" + sanitizeIDPart(idSHA(ev.Commit)) + "-" + job
	}
	return workflow + "-" + sanitizeIDPart(ev.Org+"-"+ev.Repo) + "-" + strconv.Itoa(ev.PRNumber) + "-" + job
}

// freeCardID answers an id no card holds. The key is the identity, not the id:
// an id already held by a card with another key (a commit prefix collision) gets
// a numeric suffix, so the new event plans its own card instead of being read
// as a replay of the other.
func (s *MemStore) freeCardID(id string) string {
	if s.taskByIDLocked(id) == nil {
		return id
	}
	for n := 2; ; n++ {
		cand := id + "-" + strconv.Itoa(n)
		if s.taskByIDLocked(cand) == nil {
			return cand
		}
	}
}

// taskByDedupKeyLocked finds the card that already carries a key.
func (s *MemStore) taskByDedupKeyLocked(key string) *TaskRecord {
	for i := range s.tasks {
		if s.tasks[i].DedupKey == key {
			return &s.tasks[i]
		}
	}
	return nil
}

// openPRCardsLocked counts a product's PR cards that are not finished.
func (s *MemStore) openPRCardsLocked(productID string) int {
	n := 0
	for i := range s.tasks {
		t := &s.tasks[i]
		// Defect cards have their own caps (D-091 §5.3); so do the API's cards and the
		// bundles' test planners (D-099, D-100): neither may make Brain Town skip.
		if t.DeskID != productID || t.DedupKey == "" || strings.HasPrefix(t.DedupKey, "defect:") ||
			t.APIOwner != "" || strings.HasPrefix(t.DedupKey, bundlePrefix) {
			continue
		}
		switch t.Status {
		case StatusDone, StatusFailed, StatusCanceled:
		default:
			n++
		}
	}
	return n
}

// RulePRMerged names the actor of the move that cancels a card made moot.
const RulePRMerged = "pr-merged"

// PlanPullRequestCards plans the cards a pull-request event needs.
//
// It answers the ids of the cards it planned and of the ones already there;
// a replay plans nothing. A skipped event (a draft, the cap) answers
// ErrPRSkipped wrapped with its class. The events ride in the Result, like
// every other mutation's, so the caller commits them.
func (s *MemStore) PlanPullRequestCards(ctx context.Context, productID string, ev PREvent) (planned []string, existing []string, res Result, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	m := s.masterPtr(productID)
	if m == nil {
		return nil, nil, Result{}, ErrNotFound
	}
	idx := s.deskIdxByID(productID)
	if idx < 0 {
		return nil, nil, Result{}, fmt.Errorf("no such product")
	}
	specs := prCardsFor(ev)
	if len(specs) == 0 {
		return nil, nil, Result{}, nil
	}
	if !productOwns(m.Repos, ev.Org, ev.Repo) {
		return nil, nil, Result{}, fmt.Errorf("the product does not own %s/%s", ev.Org, ev.Repo)
	}
	if ev.Draft {
		return nil, nil, Result{}, fmt.Errorf("%w: draft", ErrPRSkipped)
	}
	d := s.desks[idx]
	wf := prWorkflowID(productID)

	// Which of the cards already exist.
	var fresh []prCardSpec
	for _, spec := range specs {
		if t := s.taskByDedupKeyLocked(ev.Key(spec.Job)); t != nil {
			existing = append(existing, t.ID)
			continue
		}
		fresh = append(fresh, spec)
	}
	var events []Event

	// A merge makes a review or an e2e build that has not started moot, and
	// finishes a review that is waiting in REVIEW (design §3.1).
	if ev.Type == prTypeMerged {
		for _, job := range []string{prJobReviewer, prJobE2EBuild} {
			t := s.taskByDedupKeyLocked(ev.Key(job))
			if t == nil {
				continue
			}
			if job == prJobReviewer && t.Status == StatusReview {
				events = append(events, s.finishMergedReviewLocked(t)...)
				continue
			}
			if t.Status != StatusPending && t.Status != StatusReady {
				continue
			}
			if mv, ok := s.moveTaskLocked(t.ID, StatusCanceled, RulePRMerged); ok {
				events = append(events, mv)
			}
		}
	}
	// The Pull requests page's row (design §4.3): written after the ownership and draft checks and
	// before the early return, so a repeated event fills a missing row. A pull request refused by
	// the cap below has no card and so no row.
	rows := s.reviewRowLocked(m, ev)
	if len(fresh) == 0 && len(events) == 0 {
		return nil, existing, Result{PullRequests: rows}, nil
	}
	if len(fresh) > 0 && s.openPRCardsLocked(productID) >= prMaxOpen() {
		if len(events) == 0 {
			return nil, existing, Result{}, fmt.Errorf("%w: cap", ErrPRSkipped)
		}
		// The moves above are already applied to the projection: commit them,
		// or they would live in memory only (design §3.1). Nothing is planned.
		c := m.Clone()
		return nil, existing, Result{Master: &c, NewEvents: events, PullRequests: rows, Capped: true,
			Toast: "the cap refused the new card(s); the moot moves were kept"}, nil
	}

	items := make([]cardItem, 0, len(fresh))
	for _, spec := range fresh {
		items = append(items, cardItem{Key: ev.Key(spec.Job), ID: prCardID(wf, ev, spec.Job), Title: spec.Title, Spec: spec})
	}
	var pevs []Event
	planned, _, pevs = s.planCardsLocked(m, d, items, fmt.Sprintf("%d task(s) planned from %s on %s", len(fresh), ev.Type, ev.Repo))
	events = append(events, pevs...)
	if len(planned) > 0 {
		switch ev.Type {
		case prTypeOpened:
			events = append(events, s.emit(PullRequestOpened, productID,
				"pull request opened: "+ev.Repo+"#"+strconv.Itoa(ev.PRNumber), KindKnow, deskPayload(d)))
		case prTypeMerged:
			events = append(events, s.emit(PullRequestMerged, productID,
				"pull request merged: "+ev.Repo+"#"+strconv.Itoa(ev.PRNumber), KindKnow, deskPayload(d)))
		case prTypeStaging:
			events = append(events, s.emit(StagingDeployed, productID,
				"staging deployed: "+ev.Repo+"@"+shortSHA(ev.Commit), KindKnow, deskPayload(d)))
		}
	}
	// The row now knows its reviewer card even when that card was planned just above.
	if len(planned) > 0 {
		rows = s.reviewRowLocked(m, ev)
	}
	c := m.Clone()
	res = Result{Master: &c, NewEvents: events, PullRequests: rows,
		Toast: fmt.Sprintf("%d task(s) planned from %s on %s.", len(planned), ev.Type, ev.Repo)}
	return planned, existing, res, nil
}

// productOwns reports whether the product's repositories include owner/repo.
func productOwns(repos []string, owner, repo string) bool {
	pair := owner + "/" + repo
	for _, r := range repos {
		if strings.EqualFold(strings.TrimSpace(r), pair) {
			return true
		}
	}
	return false
}

// prSource is the slice of the GitHub client the flow needs.
type prSource interface {
	pullRequest(ctx context.Context, owner, repo string, number int) (ghPR, error)
	pullFiles(ctx context.Context, owner, repo string, number int) ([]ghFile, error)
}

// prFlow is the reader's consumer: it turns one pull-request event into the
// cards the work needs.
type prFlow struct {
	st Store
	gh prSource // nil: the fork check is skipped and only the event's draft flag is read
	// ctx holds the fetched pull request until the reviewer card is dispatched.
	ctx  *prContextCache
	logf func(string, ...any)
}

// prAssociationOK reports whether the author's association may open cards.
func prAssociationOK(a string) bool {
	switch strings.ToUpper(a) {
	case "OWNER", "MEMBER", "COLLABORATOR", "":
		return true
	}
	return false
}

// flowOutcome is what the reader does with the cursor after an event (design §3.2).
type flowOutcome int

const (
	// flowDone: committed, or skipped on purpose -- the cursor may pass the event.
	flowDone flowOutcome = iota
	// flowRetry: a transient failure -- the cursor stays and the event comes back.
	flowRetry
)

// lookupTransient reports whether a GitHub lookup failure may pass on a retry:
// the network, a 5xx or a rate limit. A 403, 404, 410 or 422 will not.
func lookupTransient(err error) bool {
	var ge *ghError
	if errors.As(err, &ge) {
		return ge.Status >= 500 || ge.Status == http.StatusTooManyRequests
	}
	return true
}

// handle plans the cards for one event. It returns only after the cards are
// committed, so the reader saves its cursor past a frame only when it is safe
// (design §3.2). A store error is not retried: the projection already holds the
// change, so a replay would plan nothing; the poller repairs the state.
func (f *prFlow) handle(ctx context.Context, ev PREvent) flowOutcome {
	if ev.Type != prTypeOpened && ev.Type != prTypeMerged {
		return flowDone
	}
	masters, err := f.st.Masters(ctx)
	if err != nil {
		f.logf("prflow: the products could not be read: %v", err)
		return flowRetry
	}
	for _, m := range masters {
		if !productOwns(m.Repos, ev.Org, ev.Repo) {
			continue
		}
		var pr ghPR
		if ev.Type == prTypeOpened && f.gh != nil && !ev.Draft {
			var err error
			pr, err = f.gh.pullRequest(ctx, ev.Org, ev.Repo, ev.PRNumber)
			switch {
			case err != nil && lookupTransient(err):
				f.logf("prflow: pr-lookup-failed %s; retrying", ghErrorClass(err))
				return flowRetry
			case err != nil:
				// The check could not be made; open nothing rather than spend on a stranger.
				f.logf("prflow: skip pr-lookup-%s", ghErrorClass(err))
				return flowDone
			case pr.Draft:
				ev.Draft = true
			case !prAllowForks() && pr.Head.Repo.FullName != "" && !strings.EqualFold(pr.Head.Repo.FullName, pr.Base.Repo.FullName):
				f.logf("prflow: skip fork")
				return flowDone
			case !prAssociationOK(pr.AuthorAssociation):
				f.logf("prflow: skip association")
				return flowDone
			}
		}
		planned, existing, res, err := f.st.PlanPullRequestCards(ctx, m.ID, ev)
		switch {
		case errors.Is(err, ErrPRSkipped):
			f.logf("prflow: skip %s", strings.TrimPrefix(err.Error(), ErrPRSkipped.Error()+": "))
		case err != nil:
			f.logf("prflow: planning failed: %v", err)
		case res.Capped:
			f.logf("prflow: skip cap")
		case len(planned) > 0:
			f.logf("prflow: planned %d card(s) on %s", len(planned), m.ID)
			if ev.Type == prTypeOpened && f.gh != nil && f.ctx != nil {
				f.fetchContext(ctx, ev, pr)
			}
			if ev.Type == prTypeOpened && f.gh != nil && templateProducts()[m.ID] {
				key := pullRequestKey(m.ID, ev.Org+"/"+ev.Repo, ev.PRNumber)
				if _, _, err := f.st.ApplyBundleLinks(ctx, m.ID, key, parsePRTemplate(pr.Body), time.Now().Unix()); err != nil && !errors.Is(err, ErrNotFound) {
					f.logf("prflow: the template could not be recorded: %v", err)
				}
			}
		case len(existing) > 0:
			f.logf("prflow: duplicate")
		}
		return flowDone
	}
	f.logf("prflow: no product owns this repository; skipped")
	return flowDone
}

// labelFamily is the part of a label before the colon: one label per family.
func labelFamily(l string) string {
	if i := strings.Index(l, ":"); i > 0 {
		return l[:i]
	}
	return l
}

// actorAlias is the alias an event names for a signed-in account: the local
// part of the address, as a slug. The address itself never reaches an event.
func actorAlias(email string) string {
	if i := strings.Index(email, "@"); i > 0 {
		email = email[:i]
	}
	return sanitizeIDPart(email)
}

// SetTaskLabel changes a card's label (D-088). The new label replaces the one
// the card holds in the same family, so `pr-review:*` labels exclude each
// other. A label outside the vocabulary is refused. The change is a state
// change, so it is an event (BR-02) naming the actor's alias.
func (s *MemStore) SetTaskLabel(ctx context.Context, productID, taskID, label, actor string) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.setTaskLabelLocked(productID, taskID, label, actor)
}

func (s *MemStore) setTaskLabelLocked(productID, taskID, label, actor string) (Result, error) {
	m := s.masterPtr(productID)
	if m == nil {
		return Result{}, ErrNotFound
	}
	if !ValidLabel(label) {
		return Result{}, reject("That label is not one of: " + strings.Join(labelOrder, ", ") + ".")
	}
	if labelFamily(label) == labelFamilyDefect {
		return Result{}, reject("The defect labels are set by the planner only.")
	}
	var t *TaskRecord
	for i := range s.tasks {
		if s.tasks[i].ID == taskID && s.tasks[i].DeskID == productID {
			t = &s.tasks[i]
		}
	}
	if t == nil {
		return Result{}, ErrNotFound
	}
	fam := labelFamily(label)
	old := ""
	kept := make([]string, 0, len(t.Labels)+1)
	for _, l := range t.Labels {
		if labelFamily(l) == fam {
			old = l
			continue
		}
		kept = append(kept, l)
	}
	if old == label {
		return Result{}, nil
	}
	t.Labels = normalizeLabels(append(kept, label))
	idx := s.deskIdxByID(productID)
	p := deskPayload(s.desks[idx])
	p.Actor = actorAlias(actor)
	p.Reason = old + "→" + label
	p.Rule = "label"
	ev := s.emit(TaskLabeled, productID, "label "+taskID+": "+old+" → "+label+" by "+p.Actor, KindInfo, p)
	c := m.Clone()
	return Result{Master: &c, NewEvents: []Event{ev}, Toast: "Label set to " + label + "."}, nil
}

// RulePRResolved names the actor of the move that ends a pull-request card.
const RulePRResolved = "pr-flow"

// resolveFrom is, per target, the statuses ResolvePRCard may move a card from.
// The QA tool's report must never override a person: a card an operator
// blocked or canceled stays where they put it, and a finished card never comes
// back. The runner walks the machine because an e2e card never ran through an
// agent; every hop is a TaskMoved naming the rule "pr-flow", so the stream shows
// who moved it (D-088, D-089).
var resolveFrom = map[string][]string{
	StatusRunning:       {StatusPending, StatusReady, StatusRunning, StatusAwaitingInput},
	StatusAwaitingInput: {StatusRunning},
	StatusBlocked:       {StatusPending, StatusReady, StatusReview}, // a fix that GAOS refuses ends BLOCKED
	StatusDone:          {StatusPending, StatusReady, StatusRunning, StatusReview},
	StatusFailed:        {StatusPending, StatusReady, StatusDispatched, StatusRunning, StatusReview, StatusAwaitingInput, StatusBlocked},
}

// ResolvePRCard moves a pull-request card to RUNNING, AWAITING_INPUT, BLOCKED,
// DONE or FAILED and records why on the card (D-088). A target outside those,
// a card that is not a PR card, or a start state the table above does not
// allow is refused.
func (s *MemStore) ResolvePRCard(ctx context.Context, productID, taskID, to, reason string) (Result, error) {
	if _, ok := resolveFrom[to]; !ok {
		return Result{}, reject("A pull-request card moves to RUNNING, AWAITING_INPUT, BLOCKED, DONE or FAILED.")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.masterPtr(productID)
	if m == nil {
		return Result{}, ErrNotFound
	}
	t := s.taskByIDLocked(taskID)
	if t == nil || t.DeskID != productID || t.DedupKey == "" {
		return Result{}, ErrNotFound
	}
	evs, err := s.resolveCardLocked(t, to, reason, RulePRResolved)
	if err != nil {
		return Result{}, err
	}
	c := m.Clone()
	return Result{Master: &c, NewEvents: evs, Floor: s.floor()}, nil
}

// resolveCardLocked walks a card to a status resolveFrom allows and records why
// on it. rule names the actor of every hop. The guarantees of ResolvePRCard
// hold for every caller: a card an operator blocked or canceled stays there and
// a finished card never comes back.
func (s *MemStore) resolveCardLocked(t *TaskRecord, to, reason, rule string) ([]Event, error) {
	allowed := false
	for _, from := range resolveFrom[to] {
		allowed = allowed || from == t.Status
	}
	if !allowed {
		return nil, reject("The card is " + t.Status + "; it does not move to " + to + " from there.")
	}
	path := statusPath(t.Status, to)
	if path == nil {
		return nil, reject("The card cannot move to " + to + " from " + t.Status + ".")
	}
	t.BlockReason = reason
	var evs []Event
	for _, step := range path {
		mv, ok := s.moveTaskLocked(t.ID, step, rule)
		if !ok {
			return nil, reject("The card cannot move to " + step + " from " + t.Status + ".")
		}
		evs = append(evs, mv)
	}
	return evs, nil
}

// statusPath is the shortest run of legal moves from one status to another,
// excluding the start and including the end. A card the QA-tool job runner
// drives never ran through an agent, so it walks the machine instead of
// jumping it. nil means there is no path.
func statusPath(from, to string) []string {
	if from == to {
		return []string{}
	}
	prev := map[string]string{from: ""}
	queue := []string{from}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, nx := range taskTransitions[cur] {
			if _, seen := prev[nx]; seen {
				continue
			}
			// A walk never passes through a finished state.
			if nx != to && (nx == StatusDone || nx == StatusFailed || nx == StatusCanceled) {
				continue
			}
			prev[nx] = cur
			if nx == to {
				var rev []string
				for at := to; at != from; at = prev[at] {
					rev = append(rev, at)
				}
				for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
					rev[i], rev[j] = rev[j], rev[i]
				}
				return rev
			}
			queue = append(queue, nx)
		}
	}
	return nil
}

// fetchContext reads the pull request's diff and keeps it for the reviewer.
// The title is untrusted and is kept as it is; the egress pass screens the
// packet it ends up in.
func (f *prFlow) fetchContext(ctx context.Context, ev PREvent, pr ghPR) {
	files, err := f.gh.pullFiles(ctx, ev.Org, ev.Repo, ev.PRNumber)
	if err != nil {
		f.logf("prflow: the diff could not be read: %s", ghErrorClass(err))
		return
	}
	f.ctx.put(ev.Key(prJobReviewer), prContext{
		Title: pr.Title, Body: pr.Body, Author: pr.User.Login, Diff: renderDiff(files)})
}

// PRCards are a product's pull-request cards, as copies.
func (s *MemStore) PRCards(ctx context.Context, productID string) ([]TaskRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []TaskRecord
	for _, t := range s.tasks {
		if t.DeskID == productID && t.DedupKey != "" {
			out = append(out, t.Clone())
		}
	}
	return out, nil
}

// RuleSecretHeld is the reason a review card carries when its reply held a
// secret: it is never finished by a merge (design §3.1).
const RuleSecretHeld = "secret-held"

// finishMergedReviewLocked moves t, a pr-reviewer card in REVIEW, to DONE with
// rule pr-merged. Any other status, or a secret hold, moves nothing.
func (s *MemStore) finishMergedReviewLocked(t *TaskRecord) []Event {
	if t == nil || t.Role != "pr-reviewer" || t.Status != StatusReview || t.BlockReason == RuleSecretHeld {
		return nil
	}
	evs, err := s.resolveCardLocked(t, StatusDone, "pull request merged", RulePRMerged)
	if err != nil {
		return nil
	}
	return evs
}

// FinishMergedReviews finishes every REVIEW pr-reviewer card whose pull request
// row is merged. The poller calls it every tick: a review that was RUNNING when
// its pull request merged reaches REVIEW after the row went final, and no poll of
// the row comes again (design §3.1).
func (s *MemStore) FinishMergedReviews(ctx context.Context) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var evs []Event
	for i := range s.tasks {
		t := &s.tasks[i]
		if t.Role != "pr-reviewer" || t.Status != StatusReview || t.DedupKey == "" {
			continue
		}
		owner, repo, n, ok := prKeyParts(t.DedupKey)
		if !ok {
			continue
		}
		row, ok := s.pullRequests[pullRequestKey(t.DeskID, owner+"/"+repo, n)]
		if !ok || row.State != PRStateMerged {
			continue
		}
		evs = append(evs, s.finishMergedReviewLocked(t)...)
	}
	if len(evs) == 0 {
		return Result{}, nil
	}
	return Result{NewEvents: evs, Floor: s.floor()}, nil
}

// HoldReview sets pr-review:needs-manual and the secret-held reason on one card,
// in one TaskLabeled event: a held review must never be finished by a merge.
func (s *MemStore) HoldReview(ctx context.Context, productID, taskID string) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.setTaskLabelLocked(productID, taskID, LabelPRReviewNeedsManual, "gaos")
	if err != nil {
		return res, err
	}
	t := s.taskByIDLocked(taskID)
	if t == nil || t.DeskID != productID {
		return Result{}, ErrNotFound
	}
	t.BlockReason = RuleSecretHeld
	if res.Master == nil {
		if m := s.masterPtr(productID); m != nil {
			c := m.Clone()
			res.Master = &c
		}
	}
	res.Floor = s.floor()
	return res, nil
}

// errGitHubOff answers prepare when no GitHub client is configured.
var errGitHubOff = errors.New("the GitHub client is off")

// prepare reads a pull request and applies the flow's gates -- draft, fork, stranger -- for a
// caller that is not Brain Town (D-099). skip names the gate that refused, "" when none did.
func (f *prFlow) prepare(ctx context.Context, owner, repo string, n int) (ghPR, string, error) {
	if f == nil || f.gh == nil {
		return ghPR{}, "", errGitHubOff
	}
	pr, err := f.gh.pullRequest(ctx, owner, repo, n)
	switch {
	case err != nil:
		return ghPR{}, "", err
	case pr.Draft:
		return pr, "draft", nil
	case !prAllowForks() && pr.Head.Repo.FullName != "" && !strings.EqualFold(pr.Head.Repo.FullName, pr.Base.Repo.FullName):
		return pr, "fork", nil
	case !prAssociationOK(pr.AuthorAssociation):
		return pr, "stranger", nil
	}
	return pr, "", nil
}
