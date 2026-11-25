package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
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
	jobID   string
	started time.Time
	card    TaskRecord
}

type qaJobRunner struct {
	st       Store
	qa       qaJobClient
	src      prSource // finds the pull request's body when the cache lost it; may be nil
	cache    *prContextCache
	nt       *notifier
	logf     func(string, ...any)
	interval time.Duration
	timeout  time.Duration
	now      func() time.Time
	jobs     map[string]*qaTracked
}

const (
	qaJobInterval = 15 * time.Second
	qaJobTimeout  = 40 * time.Minute
)

func newQAJobRunner(st Store, qa qaJobClient, cache *prContextCache, nt *notifier, src prSource) *qaJobRunner {
	return &qaJobRunner{st: st, qa: qa, cache: cache, nt: nt, src: src, logf: func(string, ...any) {},
		interval: qaJobInterval, timeout: qaJobTimeout, now: time.Now, jobs: map[string]*qaTracked{}}
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
		b, err := r.st.Board(ctx, m.ID)
		if err != nil {
			continue
		}
		for _, c := range b.Cards {
			mode, ok := isE2ECard(c.Labels)
			if !ok || r.jobs[c.ID] != nil || (c.Status != StatusPending && c.Status != StatusReady) {
				continue
			}
			r.start(ctx, m.ID, c, mode)
		}
	}
}

// caseCodeFor is the QA case a card is for: every automated case for a run,
// and the pull request's `QA-case: <code>` line for a new automation.
func (r *qaJobRunner) caseCodeFor(ctx context.Context, c BoardCard, mode string) string {
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

func (r *qaJobRunner) start(ctx context.Context, product string, c BoardCard, mode string) {
	card := TaskRecord{ID: c.ID, DeskID: product, DedupKey: c.DedupKey, Labels: c.Labels}
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
	if _, err := r.st.ResolvePRCard(ctx, product, c.ID, StatusRunning, "qa job started"); err != nil {
		r.logf("qajobs: the card could not be marked running")
	}
	r.jobs[c.ID] = &qaTracked{jobID: id, started: r.now(), card: card}
}

func (r *qaJobRunner) poll(ctx context.Context) {
	for id, t := range r.jobs {
		if r.now().Sub(t.started) > r.timeout {
			delete(r.jobs, id)
			r.fail(ctx, t.card, "qa-job-timeout")
			continue
		}
		j, _, err := r.qa.jobStatus(ctx, t.jobID)
		if err != nil {
			continue // a poll that failed is tried again; the timeout bounds it
		}
		done, ok := j.final()
		if !done {
			continue
		}
		delete(r.jobs, id)
		if ok {
			_, _ = r.st.ResolvePRCard(ctx, t.card.DeskID, t.card.ID, StatusDone, "qa job succeeded")
			continue
		}
		r.fail(ctx, t.card, "qa-job-failed")
	}
}

// fail ends the card FAILED with a class and tells the person who must act.
func (r *qaJobRunner) fail(ctx context.Context, card TaskRecord, reason string) {
	_, _ = r.st.ResolvePRCard(ctx, card.DeskID, card.ID, StatusFailed, reason)
	if r.nt != nil {
		r.nt.Notify(ctx, card, "e2e:failure", "e2e "+reason)
	}
}
