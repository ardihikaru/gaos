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
}

func (f *fakeQAJobs) startAutomation(_ context.Context, product, code, mode string) (string, int, error) {
	f.started = append(f.started, product+"|"+code+"|"+mode)
	if f.startErr != nil || f.startStatus != 0 {
		return "", f.startStatus, errors.New("x")
	}
	return "job-1", 202, nil
}
func (f *fakeQAJobs) jobStatus(context.Context, string) (qaJob, int, error) {
	s := f.statuses[min(f.polls, len(f.statuses)-1)]
	f.polls++
	return qaJob{Status: s}, 200, nil
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
	id, code, err := o.startAutomation(context.Background(), "p-1", "A-1", "create")
	if err != nil || id != "j-9" || code != 202 || gotPath != "POST /api/v1/gaos/automation" || !strings.Contains(gotBody, `"caseCode":"A-1"`) {
		t.Fatalf("start: %q %d %v %q %q", id, code, err, gotPath, gotBody)
	}
	j, _, err := o.jobStatus(context.Background(), "j-9")
	if err != nil || j.Status != "succeeded" || gotPath != "GET /api/v1/gaos/jobs/j-9" {
		t.Fatalf("status: %+v %v %q", j, err, gotPath)
	}
}
