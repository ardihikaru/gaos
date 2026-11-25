package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// The QA-tool job runner (D-088, docs/designs/gaos-pr-agent-flows.md §8.1).
//
// The e2e:automation and e2e:run cards are not given to an agent. The QA tool's
// own machine already does generate -> write -> run -> repair -> approve ->
// push -> PR in the automation repository, so a model turn here would repeat
// work it already did. GAOS asks for a job and polls it:
//
//	POST /api/v1/gaos/automation   {product, caseCode, mode: "create"|"run"} -> 202 {job_id}
//	GET  /api/v1/gaos/jobs/{id}                                              -> {status, phase, job_type, error?}
//
// The scheduler never dispatches these cards (labelsAllowDispatch), so this
// runner is their only driver. A QA result never moves a task (BR-54); the
// job's own status is what closes the card.

// qaJob is the job's answer. It is decoded leniently on purpose: the QA tool
// owns the shape, and GAOS reads only the four fields it needs.
type qaJob struct {
	Status  string `json:"status"`
	Phase   string `json:"phase"`
	JobType string `json:"job_type"`
	Error   string `json:"error"`
}

// waiting reports whether the job waits for a person: the QA tool holds an open
// approval of the spec it wrote. That is a human wait, not work in progress.
func (j qaJob) waiting() bool { return strings.ToLower(j.Status) == "awaiting_approval" }

// final reports whether a status ends the job, and how.
func (j qaJob) final() (done, ok bool) {
	switch strings.ToLower(j.Status) {
	case "succeeded", "success", "completed", "done":
		return true, true
	case "failed", "error", "canceled", "cancelled":
		return true, false
	}
	return false, false
}

type qaStartAnswer struct {
	JobID string `json:"job_id"`
}

// startAutomation asks the QA tool for a job.
func (o *qaOutbound) startAutomation(ctx context.Context, productID, caseCode, mode string) (string, int, error) {
	code, body, err := o.do(ctx, "POST", "/api/v1/gaos/automation", nil,
		map[string]string{"product": productID, "caseCode": caseCode, "mode": mode})
	if err != nil {
		return "", 0, err
	}
	if code != 202 && code != 200 {
		return "", code, fmt.Errorf("the QA tools answered HTTP %d", code)
	}
	var a qaStartAnswer
	if err := json.Unmarshal(body, &a); err != nil || a.JobID == "" || !validCursor(a.JobID) {
		return "", code, fmt.Errorf("the QA tools' job id is not short printable text")
	}
	return a.JobID, code, nil
}

// jobStatus reads a job.
func (o *qaOutbound) jobStatus(ctx context.Context, id string) (qaJob, int, error) {
	code, body, err := o.do(ctx, "GET", "/api/v1/gaos/jobs/"+url.PathEscape(id), nil, nil)
	if err != nil {
		return qaJob{}, 0, err
	}
	if code < 200 || code > 299 {
		return qaJob{}, code, fmt.Errorf("the QA tools answered HTTP %d", code)
	}
	var j qaJob
	if err := json.Unmarshal(body, &j); err != nil {
		return qaJob{}, code, fmt.Errorf("the QA tools' job is not JSON")
	}
	return j, code, nil
}

// qaJobClient is the part of the outbound client the runner needs.
type qaJobClient interface {
	startAutomation(ctx context.Context, productID, caseCode, mode string) (string, int, error)
	jobStatus(ctx context.Context, id string) (qaJob, int, error)
}

type qaTracked struct {
	jobID string
	card  TaskRecord
	// active is the time the job spent working; the timeout counts this, not the
	// time a person took to approve. waited is the time it spent waiting, summed
	// over every wait of the job, and is never reset: one budget per job.
	active   time.Duration
	waited   time.Duration
	lastPoll time.Time
	waiting  bool
	noticed  bool // the approval notice goes out once per job, not once per wait
	waits    int  // how many times the job entered the wait
}

type qaJobRunner struct {
	st       Store
	qa       qaJobClient
	src      prSource // finds the pull request's body when the cache lost it; may be nil
	cache    *prContextCache
	nt       *notifier
	logf     func(string, ...any)
	interval time.Duration
	timeout  time.Duration // active (non-waiting) time
	// approvalWait is how long, in total, a job may wait for a person to approve
	// a spec before the card fails (GAOS_QA_APPROVAL_WAIT_HOURS, default 24).
	approvalWait time.Duration
	now          func() time.Time
	jobs         map[string]*qaTracked
}

const (
	qaJobInterval = 15 * time.Second
	qaJobTimeout  = 40 * time.Minute
)

// qaApprovalWaitFromEnv is how long a spec may wait for its approval.
func qaApprovalWaitFromEnv() time.Duration {
	n, err := strconv.Atoi(strings.TrimSpace(os.Getenv("GAOS_QA_APPROVAL_WAIT_HOURS")))
	switch {
	case err != nil || n <= 0:
		return 24 * time.Hour
	case n > 720: // thirty days; a larger value would also overflow a Duration
		log.Printf("qajobs: GAOS_QA_APPROVAL_WAIT_HOURS=%d is over 720; using 720", n)
		return 720 * time.Hour
	}
	return time.Duration(n) * time.Hour
}

func newQAJobRunner(st Store, qa qaJobClient, cache *prContextCache, nt *notifier, src prSource) *qaJobRunner {
	return &qaJobRunner{st: st, qa: qa, cache: cache, nt: nt, src: src, logf: func(string, ...any) {},
		interval: qaJobInterval, timeout: qaJobTimeout, approvalWait: qaApprovalWaitFromEnv(),
		now: time.Now, jobs: map[string]*qaTracked{}}
}

// Run drives the cards until the context ends.
func (r *qaJobRunner) Run(ctx context.Context) {
	t := time.NewTicker(r.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.Tick(ctx)
		}
	}
}

func isE2ECard(labels []string) (mode string, ok bool) {
	switch {
	case hasLabel(labels, LabelE2EAutomation):
		return "create", true
	case hasLabel(labels, LabelE2ERun):
		return "run", true
	}
	return "", false
}

// Tick polls the running jobs and starts the waiting ones.
func (r *qaJobRunner) Tick(ctx context.Context) {
	r.poll(ctx)
	masters, err := r.st.Masters(ctx)
	if err != nil {
		return
	}
	for _, m := range masters {
		cards, err := r.st.PRCards(ctx, m.ID)
		if err != nil {
			continue
		}
		for _, c := range cards {
			mode, ok := isE2ECard(c.Labels)
			if !ok || r.jobs[c.ID] != nil {
				continue
			}
			switch c.Status {
			case StatusPending, StatusReady, StatusRunning, StatusAwaitingInput:
				// RUNNING and AWAITING_INPUT with no tracked job are cards the
				// process lost on a restart: the job id is on the card.
				r.start(ctx, m.ID, c, mode)
			}
		}
	}
}

// caseCodeFor is the QA case a card is for: every automated case for a run,
// and the pull request's `QA-case: <code>` line for a new automation.
func (r *qaJobRunner) caseCodeFor(ctx context.Context, c TaskRecord, mode string) string {
	if mode == "run" {
		return "*"
	}
	body := ""
	revKey := strings.TrimSuffix(c.DedupKey, prJobE2EBuild) + prJobReviewer
	if v, ok := r.cache.get(revKey); ok {
		body = v.Body
	} else if owner, repo, n, ok := prKeyParts(c.DedupKey); ok && r.src != nil {
		if pr, err := r.src.pullRequest(ctx, owner, repo, n); err == nil {
			body = pr.Body
		}
	}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(line), "qa-case:") {
			code := strings.TrimSpace(line[len("qa-case:"):])
			if validCursor(code) {
				return code
			}
		}
	}
	return ""
}

// The card keeps its job id in its reason, as "qa-job:<id> <text>".
const qaJobPrefix = "qa-job:"

func qaJobReason(id, text string) string { return qaJobPrefix + id + " " + text }

// jobIDFromReason reads the job id a card's reason carries, or "".
func jobIDFromReason(reason string) string {
	if !strings.HasPrefix(reason, qaJobPrefix) {
		return ""
	}
	id := strings.Fields(strings.TrimPrefix(reason, qaJobPrefix))
	if len(id) == 0 || !validCursor(id[0]) {
		return ""
	}
	return id[0]
}

func (r *qaJobRunner) start(ctx context.Context, product string, c TaskRecord, mode string) {
	card := TaskRecord{ID: c.ID, DeskID: product, DedupKey: c.DedupKey, Labels: c.Labels}
	// A card that already names its job (a restart lost the runner's memory, or
	// an operator released a requeued card) is picked up, never started again:
	// a second `create` job could open a second pull request.
	if id := jobIDFromReason(c.BlockReason); id != "" {
		r.jobs[c.ID] = &qaTracked{jobID: id, card: card, lastPoll: r.now(), waiting: c.Status == StatusAwaitingInput}
		return
	}
	code := r.caseCodeFor(ctx, c, mode)
	if code == "" {
		// Nothing is guessed: the card waits for a person to name the case.
		_, _ = r.st.ResolvePRCard(ctx, product, c.ID, StatusBlocked,
			"no QA-case line in the pull request description; add `QA-case: <code>` there and move the card to Ready")
		return
	}
	id, status, err := r.qa.startAutomation(ctx, product, code, mode)
	switch {
	case status == 404:
		r.fail(ctx, card, "qa-routes-missing")
		return
	case err != nil:
		r.fail(ctx, card, "qa-start-failed")
		return
	}
	if _, err := r.st.ResolvePRCard(ctx, product, c.ID, StatusRunning, qaJobReason(id, "started")); err != nil {
		r.logf("qajobs: the card could not be marked running")
	}
	r.jobs[c.ID] = &qaTracked{jobID: id, card: card, lastPoll: r.now()}
}

// qaMaxWaits bounds how many times one job may enter the approval wait: a QA
// tool that flaps between waiting and running must not hold a card, or the
// notifier, for ever.
const qaMaxWaits = 5

func (r *qaJobRunner) poll(ctx context.Context) {
	for id, t := range r.jobs {
		now := r.now()
		// Time is charged to the state the job was in since the last look.
		if t.waiting {
			t.waited += now.Sub(t.lastPoll)
		} else {
			t.active += now.Sub(t.lastPoll)
		}
		t.lastPoll = now
		// Ask first, then apply the limits, so an approval given in the last
		// interval before a cap is seen.
		j, _, err := r.qa.jobStatus(ctx, t.jobID)
		if err == nil {
			if done, ok := j.final(); done {
				delete(r.jobs, id)
				if ok {
					_, _ = r.st.ResolvePRCard(ctx, t.card.DeskID, t.card.ID, StatusRunning, qaJobReason(t.jobID, "finished")) // a no-op unless it waited
					_, _ = r.st.ResolvePRCard(ctx, t.card.DeskID, t.card.ID, StatusDone, "qa job succeeded")
				} else {
					r.fail(ctx, t.card, "qa-job-failed")
				}
				continue
			}
			switch {
			case j.waiting() && !t.waiting:
				t.waits++
				if t.waits > qaMaxWaits {
					delete(r.jobs, id)
					r.fail(ctx, t.card, "qa-job-flapping")
					continue
				}
				// A person must approve the spec: Needs you, the clock stops.
				t.waiting = true
				_, _ = r.st.ResolvePRCard(ctx, t.card.DeskID, t.card.ID, StatusAwaitingInput,
					qaJobReason(t.jobID, "the QA tool waits for you to approve the spec (open the QA Inbox)"))
				if !t.noticed && r.nt != nil {
					t.noticed = true
					r.nt.Notify(ctx, t.card, "e2e:approval", "e2e spec waits for approval")
				}
			case !j.waiting() && t.waiting:
				// Answered: the job works again, and so does the clock.
				t.waiting = false
				_, _ = r.st.ResolvePRCard(ctx, t.card.DeskID, t.card.ID, StatusRunning, qaJobReason(t.jobID, "resumed after approval"))
			}
		}
		if t.waited > r.approvalWait {
			delete(r.jobs, id)
			r.fail(ctx, t.card, "qa-approval-timeout")
			continue
		}
		if t.active > r.timeout {
			delete(r.jobs, id)
			r.fail(ctx, t.card, "qa-job-timeout")
		}
	}
}

// fail ends the card FAILED with a class and tells the person who must act.
func (r *qaJobRunner) fail(ctx context.Context, card TaskRecord, reason string) {
	_, _ = r.st.ResolvePRCard(ctx, card.DeskID, card.ID, StatusFailed, reason)
	if r.nt != nil {
		r.nt.Notify(ctx, card, "e2e:failure", "e2e "+reason)
	}
}
