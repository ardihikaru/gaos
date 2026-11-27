package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeQAJobs struct {
	startStatus int
	startErr    error
	statuses    []string
	polls       int
	started     []string
	canceled    []string
	cancelErr   error
	pr, link    string // pr_url and job_url the status carries (design §4.4)
	// Release B (D-103): what the tool lists, what it was sent, what it reports.
	caps      map[string]bool
	ins       []qaStartIn
	sha, auto string
}

func (f *fakeQAJobs) capabilities(context.Context) map[string]bool {
	if f.caps == nil {
		return map[string]bool{}
	}
	return f.caps
}

func (f *fakeQAJobs) cancelJob(_ context.Context, id string) error {
	f.canceled = append(f.canceled, id)
	return f.cancelErr
}

func (f *fakeQAJobs) startAutomation(_ context.Context, in qaStartIn) (string, int, error) {
	f.ins = append(f.ins, in)
	f.started = append(f.started, in.Product+"|"+in.CaseCode+"|"+in.Mode)
	if f.startErr != nil || f.startStatus != 0 {
		return "", f.startStatus, errors.New("x")
	}
	return "job-1", 202, nil
}
func (f *fakeQAJobs) jobStatus(context.Context, string) (qaJob, int, error) {
	s := f.statuses[min(f.polls, len(f.statuses)-1)]
	f.polls++
	return qaJob{Status: s, PRURL: f.pr, JobURL: f.link, PushedSHA: f.sha, AutoMerge: f.auto}, 200, nil
}

func e2eFixture(t *testing.T, qa *fakeQAJobs, body string) (*qaJobRunner, *MemStore, string, string) {
	t.Helper()
	st := NewMemStore(1)
	planned, _, _, err := st.PlanPullRequestCards(context.Background(), "p-1", prEvent(prTypeOpened, "laundry-api", 50))
	if err != nil {
		t.Fatal(err)
	}
	cache := &prContextCache{}
	cache.put("loop/laundry-api#50:pr-reviewer", prContext{Body: body})
	r := newQAJobRunner(st, qa, cache, nil, nil)
	return r, st, planned[0], planned[1] // the review card, the e2e card
}

func TestAnE2ECardStartsAJobAndIsDoneWhenTheJobSucceeds(t *testing.T) {
	qa := &fakeQAJobs{statuses: []string{"running", "succeeded"}}
	r, st, _, e2e := e2eFixture(t, qa, "what changed\nQA-case: CART-12\n")
	ctx := context.Background()
	r.Tick(ctx)
	if len(qa.started) != 1 || qa.started[0] != "p-1|CART-12|create" {
		t.Fatalf("started %v, want the case named in the description", qa.started)
	}
	if got := prCards(t, st, "p-1")[e2e].Status; got != StatusRunning {
		t.Errorf("the card is %s, want RUNNING", got)
	}
	r.Tick(ctx) // running
	if got := prCards(t, st, "p-1")[e2e].Status; got != StatusRunning {
		t.Errorf("mid-job the card is %s", got)
	}
	r.Tick(ctx) // succeeded
	if got := prCards(t, st, "p-1")[e2e].Status; got != StatusDone {
		t.Errorf("the card is %s, want DONE", got)
	}
	if len(qa.started) != 1 {
		t.Errorf("the job was started %d times", len(qa.started))
	}
}

func TestAFailedJobFailsTheCardAndATimeoutDoes(t *testing.T) {
	qa := &fakeQAJobs{statuses: []string{"failed"}}
	r, st, _, e2e := e2eFixture(t, qa, "QA-case: A-1")
	r.Tick(context.Background())
	r.Tick(context.Background())
	if c := prCards(t, st, "p-1")[e2e]; c.Status != StatusFailed {
		t.Errorf("the card is %s, want FAILED", c.Status)
	}

	qa2 := &fakeQAJobs{statuses: []string{"running"}}
	r2, st2, _, e2e2 := e2eFixture(t, qa2, "QA-case: A-1")
	now := time.Now()
	r2.now = func() time.Time { return now }
	r2.Tick(context.Background())
	now = now.Add(r2.timeout + time.Minute)
	r2.Tick(context.Background())
	if c := prCards(t, st2, "p-1")[e2e2]; c.Status != StatusFailed {
		t.Errorf("after the timeout the card is %s, want FAILED", c.Status)
	}
	for _, tk := range st2.tasks {
		if tk.ID == e2e2 && tk.BlockReason != "qa-job-timeout" {
			t.Errorf("reason %q, want qa-job-timeout", tk.BlockReason)
		}
	}
}

func TestMissingQARoutesFailTheCardWithAClass(t *testing.T) {
	r, st, _, e2e := e2eFixture(t, &fakeQAJobs{startStatus: 404}, "QA-case: A-1")
	r.Tick(context.Background())
	for _, tk := range st.tasks {
		if tk.ID == e2e && (tk.Status != StatusFailed || tk.BlockReason != "qa-routes-missing") {
			t.Errorf("status %s reason %q; want FAILED qa-routes-missing", tk.Status, tk.BlockReason)
		}
	}
}

func TestACardWithNoCaseNamedWaitsForAPersonAndIsNeverGuessed(t *testing.T) {
	qa := &fakeQAJobs{statuses: []string{"succeeded"}}
	r, st, _, e2e := e2eFixture(t, qa, "no case named here")
	r.Tick(context.Background())
	if len(qa.started) != 0 {
		t.Fatalf("a job was started without a case: %v", qa.started)
	}
	for _, tk := range st.tasks {
		if tk.ID == e2e && (tk.Status != StatusBlocked || !strings.Contains(tk.BlockReason, "QA-case")) {
			t.Errorf("status %s reason %q; want BLOCKED naming the QA-case line", tk.Status, tk.BlockReason)
		}
	}
}

func TestAStagingRunAsksForEveryCase(t *testing.T) {
	st := NewMemStore(1)
	st.PlanPullRequestCards(context.Background(), "p-1", PREvent{Type: prTypeStaging, Org: "loop", Repo: "laundry-api", Commit: "abcdef0123"})
	qa := &fakeQAJobs{statuses: []string{"succeeded"}}
	r := newQAJobRunner(st, qa, &prContextCache{}, nil, nil)
	r.Tick(context.Background())
	if len(qa.started) != 1 || qa.started[0] != "p-1|*|run" {
		t.Errorf("started %v, want the run of every case", qa.started)
	}
}

func TestTheOutboundClientSpeaksTheAgreedRoutes(t *testing.T) {
	var gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.Method + " " + r.URL.Path
		if r.Method == "POST" {
			b := make([]byte, 512)
			n, _ := r.Body.Read(b)
			gotBody = string(b[:n])
			w.WriteHeader(202)
			_, _ = w.Write([]byte(`{"job_id":"j-9"}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"succeeded","phase":"done","job_type":"run","report":{"x":1}}`))
	}))
	defer srv.Close()
	o := newQaOutbound(srv.URL, "tok-tok-tok-tok-tok-tok-tok-tok-tok-tok", nil)
	if o == nil {
		t.Skip("the test URL is not an allowed outbound origin")
	}
	id, code, err := o.startAutomation(context.Background(), qaStartIn{Product: "p-1", CaseCode: "A-1", Mode: "create"})
	if err != nil || id != "j-9" || code != 202 || gotPath != "POST /api/v1/gaos/automation" || !strings.Contains(gotBody, `"caseCode":"A-1"`) {
		t.Fatalf("start: %q %d %v %q %q", id, code, err, gotPath, gotBody)
	}
	j, _, err := o.jobStatus(context.Background(), "j-9")
	if err != nil || j.Status != "succeeded" || gotPath != "GET /api/v1/gaos/jobs/j-9" {
		t.Fatalf("status: %+v %v %q", j, err, gotPath)
	}
}

func TestAnApprovalWaitStopsTheClockAndPutsTheCardInNeedsYou(t *testing.T) {
	qa := &fakeQAJobs{statuses: []string{"running", "awaiting_approval", "awaiting_approval", "running", "succeeded"}}
	r, st, _, e2e := e2eFixture(t, qa, "QA-case: A-1")
	now := time.Now()
	r.now = func() time.Time { return now }
	ctx := context.Background()
	status := func() string { return prCards(t, st, "p-1")[e2e].Status }

	r.Tick(ctx) // starts the job
	now = now.Add(10 * time.Minute)
	r.Tick(ctx) // running
	now = now.Add(10 * time.Minute)
	r.Tick(ctx) // awaiting approval
	if got := status(); got != StatusAwaitingInput {
		t.Fatalf("the card is %s while the spec waits for a person, want AWAITING_INPUT", got)
	}
	// Hours pass while a person is away: well past the 40-minute work limit,
	// inside the approval wait. The card must not fail.
	now = now.Add(6 * time.Hour)
	r.Tick(ctx)
	if got := status(); got != StatusAwaitingInput {
		t.Fatalf("after six hours of waiting the card is %s, want it still waiting for the person", got)
	}
	// Approved: the job works again and finishes.
	now = now.Add(time.Minute)
	r.Tick(ctx)
	if got := status(); got != StatusRunning {
		t.Errorf("after the approval the card is %s, want RUNNING", got)
	}
	now = now.Add(time.Minute)
	r.Tick(ctx)
	if got := status(); got != StatusDone {
		t.Errorf("the card is %s, want DONE", got)
	}
}

func TestAnApprovalNobodyAnswersFailsTheCardWithItsOwnReasonAndNotifiesOnce(t *testing.T) {
	qa := &fakeQAJobs{statuses: []string{"awaiting_approval"}}
	r, st, _, e2e := e2eFixture(t, qa, "QA-case: A-1")
	n, _, _ := notifyFixture(t, 200)
	n.st = st
	r.nt = n
	now := time.Now()
	r.now = func() time.Time { return now }
	r.approvalWait = 2 * time.Hour
	ctx := context.Background()
	r.Tick(ctx)
	now = now.Add(time.Minute)
	r.Tick(ctx) // enters the wait: one notice
	now = now.Add(time.Hour)
	r.Tick(ctx) // still inside the wait
	if got := prCards(t, st, "p-1")[e2e].Status; got != StatusAwaitingInput {
		t.Fatalf("inside the wait the card is %s", got)
	}
	now = now.Add(2 * time.Hour)
	r.Tick(ctx)
	for _, tk := range st.tasks {
		if tk.ID == e2e && (tk.Status != StatusFailed || tk.BlockReason != "qa-approval-timeout") {
			t.Errorf("status %s reason %q; want FAILED qa-approval-timeout", tk.Status, tk.BlockReason)
		}
	}
	asked := 0
	for _, e := range st.events {
		if e.Type == ContactNotified && strings.Contains(e.Text, "e2e:approval") {
			asked++
		}
	}
	if asked != 1 {
		t.Errorf("%d approval notices, want exactly one", asked)
	}
}

func TestTheApprovalWaitIsConfigurable(t *testing.T) {
	t.Setenv("GAOS_QA_APPROVAL_WAIT_HOURS", "3")
	if got := qaApprovalWaitFromEnv(); got != 3*time.Hour {
		t.Errorf("got %v, want 3h", got)
	}
	t.Setenv("GAOS_QA_APPROVAL_WAIT_HOURS", "")
	if got := qaApprovalWaitFromEnv(); got != 24*time.Hour {
		t.Errorf("default %v, want 24h", got)
	}
}

func TestARestartPicksTheJobUpFromTheCardAndNeverStartsASecond(t *testing.T) {
	qa := &fakeQAJobs{statuses: []string{"succeeded"}}
	r, st, _, e2e := e2eFixture(t, qa, "QA-case: A-1")
	ctx := context.Background()
	// The card was RUNNING with its job id when the process died.
	if _, err := st.ResolvePRCard(ctx, "p-1", e2e, StatusRunning, qaJobReason("job-777", "", "started")); err != nil {
		t.Fatal(err)
	}
	r.Tick(ctx) // a fresh runner: adopts the job
	if len(qa.started) != 0 {
		t.Fatalf("a second job was started: %v", qa.started)
	}
	r.Tick(ctx) // polls it: succeeded
	if got := prCards(t, st, "p-1")[e2e].Status; got != StatusDone {
		t.Errorf("the adopted job's card is %s, want DONE", got)
	}
	// A card waiting for approval is adopted too, and goes on waiting.
	r2, st2, _, e2e2 := e2eFixture(t, &fakeQAJobs{statuses: []string{"awaiting_approval"}}, "QA-case: A-1")
	st2.ResolvePRCard(ctx, "p-1", e2e2, StatusRunning, qaJobReason("job-778", "", "started"))
	st2.ResolvePRCard(ctx, "p-1", e2e2, StatusAwaitingInput, qaJobReason("job-778", "", "waits"))
	r2.Tick(ctx)
	r2.Tick(ctx)
	if got := prCards(t, st2, "p-1")[e2e2].Status; got != StatusAwaitingInput {
		t.Errorf("the adopted waiting card is %s, want AWAITING_INPUT", got)
	}
	if jobIDFromReason("qa-job:job-9 something") != "job-9" || jobIDFromReason("no job here") != "" || jobIDFromReason("qa-job:") != "" {
		t.Error("jobIDFromReason reads the wrong id")
	}
}

func TestAJobThatFlapsBetweenWaitingAndRunningIsFailedAndNotifiesOnce(t *testing.T) {
	flap := make([]string, 0, 40)
	for i := 0; i < 20; i++ {
		flap = append(flap, "awaiting_approval", "running")
	}
	qa := &fakeQAJobs{statuses: flap}
	r, st, _, e2e := e2eFixture(t, qa, "QA-case: A-1")
	n, _, _ := notifyFixture(t, 200)
	n.st = st
	r.nt = n
	now := time.Now()
	r.now = func() time.Time { return now }
	for i := 0; i < 30; i++ {
		now = now.Add(time.Minute)
		r.Tick(context.Background())
	}
	for _, tk := range st.tasks {
		if tk.ID == e2e && (tk.Status != StatusFailed || tk.BlockReason != "qa-job-flapping") {
			t.Fatalf("status %s reason %q; want FAILED qa-job-flapping", tk.Status, tk.BlockReason)
		}
	}
	notices := 0
	for _, e := range st.events {
		if e.Type == ContactNotified && strings.Contains(e.Text, "e2e:approval") {
			notices++
		}
	}
	if notices != 1 {
		t.Errorf("%d approval notices over %d waits, want one per job", notices, qaMaxWaits)
	}
}

func TestAnApprovalGivenInTheLastIntervalBeforeTheCapIsSeen(t *testing.T) {
	qa := &fakeQAJobs{statuses: []string{"awaiting_approval", "succeeded"}}
	r, st, _, e2e := e2eFixture(t, qa, "QA-case: A-1")
	now := time.Now()
	r.now = func() time.Time { return now }
	r.approvalWait = time.Hour
	ctx := context.Background()
	r.Tick(ctx) // start
	now = now.Add(time.Minute)
	r.Tick(ctx)                  // waits
	now = now.Add(2 * time.Hour) // past the cap; the job has since succeeded
	r.Tick(ctx)
	if got := prCards(t, st, "p-1")[e2e].Status; got != StatusDone {
		t.Errorf("the card is %s; the finished job must be seen before the cap is applied", got)
	}
}

func TestTheApprovalWaitHasAnUpperBound(t *testing.T) {
	t.Setenv("GAOS_QA_APPROVAL_WAIT_HOURS", "999999999999")
	if got := qaApprovalWaitFromEnv(); got != 720*time.Hour {
		t.Errorf("got %v, want the 720 h bound", got)
	}
}

func TestTheQAToolsReportCannotOverrideAnOperatorsMove(t *testing.T) {
	ctx := context.Background()
	st := NewMemStore(1)
	planned, _, _, _ := st.PlanPullRequestCards(ctx, "p-1", prEvent(prTypeOpened, "laundry-api", 61))
	card := planned[1] // the e2e card
	if _, err := st.ResolvePRCard(ctx, "p-1", card, StatusBlocked, "operator parked it"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ResolvePRCard(ctx, "p-1", card, StatusRunning, "qa job resumed"); err == nil {
		t.Error("a running report moved a card an operator blocked")
	}
	if got := prCards(t, st, "p-1")[card].Status; got != StatusBlocked {
		t.Errorf("the card is %s, want BLOCKED", got)
	}
	// A finished card never comes back.
	st.ResolvePRCard(ctx, "p-1", planned[0], StatusFailed, "x")
	if _, err := st.ResolvePRCard(ctx, "p-1", planned[0], StatusRunning, "late"); err == nil {
		t.Error("a FAILED card was brought back to RUNNING")
	}
	// No walk passes through a finished state.
	for _, step := range statusPath(StatusAwaitingInput, StatusRunning) {
		if step == StatusDone || step == StatusFailed || step == StatusCanceled {
			t.Errorf("the walk passes through %s", step)
		}
	}
}

func TestEveryWayGAOSGivesUpOnAJobCancelsItAtTheQATool(t *testing.T) {
	ctx := context.Background()
	cases := map[string]func(r *qaJobRunner, now *time.Time, qa *fakeQAJobs){
		"approval timeout": func(r *qaJobRunner, now *time.Time, qa *fakeQAJobs) {
			qa.statuses = []string{"awaiting_approval"}
			r.approvalWait = time.Hour
			r.Tick(ctx)
			*now = now.Add(time.Minute)
			r.Tick(ctx)
			*now = now.Add(2 * time.Hour)
			r.Tick(ctx)
		},
		"work timeout": func(r *qaJobRunner, now *time.Time, qa *fakeQAJobs) {
			qa.statuses = []string{"running"}
			r.Tick(ctx)
			*now = now.Add(r.timeout + time.Minute)
			r.Tick(ctx)
		},
	}
	for name, run := range cases {
		qa := &fakeQAJobs{}
		r, st, _, e2e := e2eFixture(t, qa, "QA-case: A-1")
		now := time.Now()
		r.now = func() time.Time { return now }
		run(r, &now, qa)
		if len(qa.canceled) != 1 {
			t.Errorf("%s: the QA tool was asked to cancel %d times, want once", name, len(qa.canceled))
		}
		if got := prCards(t, st, "p-1")[e2e].Status; got != StatusFailed {
			t.Errorf("%s: the card is %s, want FAILED", name, got)
		}
	}
	// A flapping job is canceled too.
	flap := make([]string, 0, 40)
	for i := 0; i < 20; i++ {
		flap = append(flap, "awaiting_approval", "running")
	}
	qa := &fakeQAJobs{statuses: flap}
	r, _, _, _ := e2eFixture(t, qa, "QA-case: A-1")
	for i := 0; i < 30; i++ {
		r.Tick(ctx)
	}
	if len(qa.canceled) != 1 {
		t.Errorf("flapping: canceled %d times, want once", len(qa.canceled))
	}
}

func TestAnOperatorCancelingTheCardCancelsTheQAJob(t *testing.T) {
	ctx := context.Background()
	qa := &fakeQAJobs{statuses: []string{"running"}}
	r, st, _, e2e := e2eFixture(t, qa, "QA-case: A-1")
	r.Tick(ctx) // starts
	st.mu.Lock()
	st.taskByIDLocked(e2e).Status = StatusCanceled // the operator's board move
	st.mu.Unlock()
	r.Tick(ctx)
	if len(qa.canceled) != 1 || qa.canceled[0] != "job-1" {
		t.Fatalf("canceled %v, want the card's job once", qa.canceled)
	}
	r.Tick(ctx)
	if len(qa.canceled) != 1 {
		t.Errorf("a canceled job was canceled again (%d)", len(qa.canceled))
	}
}

func TestAFailedCancelNeverKeepsTheCardFromEnding(t *testing.T) {
	qa := &fakeQAJobs{statuses: []string{"running"}, cancelErr: errors.New("the QA tool is down")}
	r, st, _, e2e := e2eFixture(t, qa, "QA-case: A-1")
	now := time.Now()
	r.now = func() time.Time { return now }
	r.Tick(context.Background())
	now = now.Add(r.timeout + time.Minute)
	r.Tick(context.Background())
	if got := prCards(t, st, "p-1")[e2e].Status; got != StatusFailed {
		t.Errorf("the card is %s; a failed cancel must not keep it open", got)
	}
}

func TestTheCancelCallSpeaksTheAgreedRoute(t *testing.T) {
	var gotPath string
	status := 200
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.Method + " " + r.URL.Path
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"job_id":"j-9","status":"failed","canceled":true}`))
	}))
	defer srv.Close()
	o := newQaOutbound(srv.URL, "tok-tok-tok-tok-tok-tok-tok-tok-tok-tok", nil)
	if o == nil {
		t.Skip("the test URL is not an allowed outbound origin")
	}
	if err := o.cancelJob(context.Background(), "j-9"); err != nil || gotPath != "POST /api/v1/gaos/jobs/j-9/cancel" {
		t.Fatalf("cancel: %v %q", err, gotPath)
	}
	status = 404
	if err := o.cancelJob(context.Background(), "gone"); err != nil {
		t.Errorf("a job the QA tool no longer knows is not an error: %v", err)
	}
	status = 500
	if err := o.cancelJob(context.Background(), "j-9"); err == nil {
		t.Error("a 500 was taken for a cancel")
	}
}
