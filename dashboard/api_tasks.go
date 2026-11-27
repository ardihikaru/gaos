package main

// The external-tasks API's HTTP half (D-099, docs/designs/gaos-external-tasks-and-pr-bundles.md
// §5): a client with the tasks capability plans a pull-request review, a unit-test build, an e2e
// automation or an e2e run, follows it, and cancels a run it started. The store half is
// api_cards.go; the unattended start is apidispatch.go.

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
)

// apiTaskBody is POST /v1/products/{id}/tasks. There is no title: every title is derived
// (BR-59).
type apiTaskBody struct {
	Kind       string `json:"kind"`
	ExternalID string `json:"externalId"`
	PR         string `json:"pr"`
	Review     string `json:"review"`
	CaseCode   string `json:"caseCode"`
	Dispatch   *bool  `json:"dispatch"`
}

// apiTaskOut is the answer of every task route.
type apiTaskOut struct {
	TaskID    string   `json:"taskId"`
	Created   bool     `json:"created"`
	Status    string   `json:"status"`
	Column    string   `json:"column"`
	Labels    []string `json:"labels"`
	Dispatch  string   `json:"dispatch"`
	Reason    *string  `json:"reason,omitempty"`
	UpdatedAt *int64   `json:"updatedAt,omitempty"`
}

const apiTaskMaxBody = 16 << 10

// parsePRRef reads owner/repo#n.
func parsePRRef(ref string) (owner, repo string, n int, ok bool) {
	hash := strings.LastIndex(ref, "#")
	if hash < 0 {
		return "", "", 0, false
	}
	o, r, found := ghOwnerRepo(ref[:hash])
	num, err := strconv.Atoi(ref[hash+1:])
	if !found || err != nil || num <= 0 || o == "" || r == "" || strings.ContainsAny(o+r, " :@") {
		return "", "", 0, false
	}
	return o, r, num, true
}

// validateTaskBody turns a body into a request, or the 422 it answers.
func validateTaskBody(b apiTaskBody, client string) (APICardIn, error) {
	in := APICardIn{Client: client, ExternalID: b.ExternalID, Kind: b.Kind, Dispatch: b.Dispatch == nil || *b.Dispatch}
	bad := func(reason string) (APICardIn, error) { return APICardIn{}, refuse(422, "invalid-field", reason) }
	switch b.Kind {
	case APIKindPRReview, APIKindUnitTest, APIKindAutoCreate, APIKindAutoRun:
	default:
		return APICardIn{}, refuse(422, "invalid-kind", "kind is one of pr-review, unit-test, automation-create, automation-run")
	}
	if !apiExternalIDPattern.MatchString(b.ExternalID) {
		return bad("externalId is 1 to 64 of A-Z a-z 0-9 . _ - and starts with a letter or a digit")
	}
	switch b.Review {
	case "", "auto":
	case "manual":
		if b.Kind != APIKindPRReview {
			return bad("review applies to pr-review only")
		}
		in.Manual = true
	default:
		return bad("review is auto or manual")
	}
	if b.Kind == APIKindAutoRun {
		if b.PR != "" {
			return bad("automation-run takes no pr")
		}
		if b.CaseCode != "*" && !apiCaseCodePattern.MatchString(b.CaseCode) {
			return bad("caseCode is * or a case code such as TC-OUTREG-004")
		}
		in.CaseCode = b.CaseCode
		return in, nil
	}
	if b.CaseCode != "" {
		return bad("caseCode applies to automation-run only")
	}
	o, r, n, ok := parsePRRef(b.PR)
	if !ok {
		return bad("pr is owner/repo#number")
	}
	in.Owner, in.Repo, in.Number = o, r, n
	return in, nil
}

// apiDispatchAnswer is §5.4's table, computed from the card as it stands.
func apiDispatchAnswer(t TaskRecord) string {
	switch {
	case hasLabel(t.Labels, LabelPRReviewManual):
		return "manual-review"
	case hasLabel(t.Labels, LabelE2EAutomation) || hasLabel(t.Labels, LabelE2ERun):
		return "qa-tool"
	case runnerMode() == RunnerOn:
		return "scheduler"
	case t.AutoDispatch && autorunProducts()[t.DeskID]:
		return "auto"
	}
	return "operator"
}

func apiTaskAnswer(t TaskRecord, created, detail bool) apiTaskOut {
	labels := t.Labels
	if labels == nil {
		labels = []string{}
	}
	out := apiTaskOut{TaskID: t.ID, Created: created, Status: t.Status,
		Column: strings.ToLower(columnOf(t.Status)), Labels: labels, Dispatch: apiDispatchAnswer(t)}
	if detail {
		r := apiPublicReason(t)
		out.Reason = &r
		u := t.LastBeatAt
		if u == 0 {
			u = t.APIAt
		}
		out.UpdatedAt = &u
	}
	return out
}

// apiCardFail answers a store refusal.
func apiCardFail(w http.ResponseWriter, err error) {
	var ce *apiCardErr
	switch {
	case errors.As(err, &ce):
		apiErr(w, ce.Status, ce.Code, ce.Reason)
	case errors.Is(err, ErrNotFound):
		apiErr(w, http.StatusNotFound, "not-found", "no such task")
	default:
		apiStoreErr(w, err)
	}
}

// apiTaskGate is the gate of every task route: the client scope, the tasks capability, then the
// product. writes refuses the admin token: tasks are planned by a named client (SEC-8 (r2)).
func (s *server) apiTaskGate(w http.ResponseWriter, r *http.Request, write bool) (string, *apiClient, apiScope, bool) {
	scope, cl, ok := s.apiGate(w, r, scopeClient)
	if !ok || !needCap(w, scope, cl, CapTasks) {
		return "", nil, scopeNone, false
	}
	if write && cl == nil {
		apiErr(w, http.StatusForbidden, "forbidden", "tasks are planned by a named client")
		return "", nil, scopeNone, false
	}
	id, ok := s.apiProduct(w, r, scope, cl)
	return id, cl, scope, ok
}

// apiTaskCreate is POST /v1/products/{id}/tasks, in the order of design §5.3.
func (s *server) apiTaskCreate(w http.ResponseWriter, r *http.Request) {
	id, cl, scope, ok := s.apiTaskGate(w, r, true)
	if !ok {
		return
	}
	release, ok := s.beginWrite(w, scope, cl)
	if !ok {
		return
	}
	defer release()
	var b apiTaskBody
	if !apiDecodeLimit(w, r, &b, apiTaskMaxBody) {
		return
	}
	in, err := validateTaskBody(b, cl.Name)
	if err != nil {
		apiCardFail(w, err)
		return
	}
	if apiPRKind(in.Kind) {
		m, err := s.store.Master(r.Context(), id)
		if err != nil || !productOwns(m.Repos, in.Owner, in.Repo) {
			apiErr(w, http.StatusUnprocessableEntity, "repo-not-owned", "the product does not follow "+in.Owner+"/"+in.Repo)
			return
		}
	}
	if runnerMode() == RunnerOff {
		apiErr(w, http.StatusConflict, "runner-off", "the runner is off (GAOS_RUNNER); nothing is planned")
		return
	}
	ctx := r.Context()
	t, found, err := s.store.APICardLookup(ctx, id, in)
	if err != nil {
		apiCardFail(w, err)
		return
	}
	switch found {
	case APIOwn:
		apiJSON(w, http.StatusOK, apiTaskAnswer(t, false, false))
		return
	case APIAdopt:
		t, _, err := s.store.AdoptAPICard(ctx, id, t.ID, in)
		if err != nil {
			apiCardFail(w, err)
			return
		}
		apiJSON(w, http.StatusOK, apiTaskAnswer(t, false, false))
		return
	}
	// Preconditions, only when a card will be created (§5.3 step 6).
	if apiPRKind(in.Kind) {
		pr, skip, err := s.flow.prepare(ctx, in.Owner, in.Repo, in.Number)
		switch {
		case errors.Is(err, errGitHubOff):
			apiErr(w, http.StatusServiceUnavailable, "github-off", "the GitHub client is off; GAOS cannot check the pull request")
			return
		case err != nil:
			apiErr(w, http.StatusConflict, "skipped", "the pull request could not be read ("+ghErrorClass(err)+")")
			return
		case skip != "":
			apiErr(w, http.StatusConflict, "skipped", "the pull request is a "+skip)
			return
		case in.Kind == APIKindPRReview && (pr.Merged || pr.State != "open"):
			apiErr(w, http.StatusConflict, "skipped", "not open")
			return
		case in.Kind == APIKindUnitTest && !pr.Merged:
			apiErr(w, http.StatusConflict, "skipped", "not merged")
			return
		}
		if in.Kind == APIKindPRReview && s.flow.ctx != nil {
			s.flow.fetchContext(ctx, apiEvent(in), pr)
		}
	}
	if (in.Kind == APIKindAutoCreate || in.Kind == APIKindAutoRun) && s.qaOut == nil {
		apiErr(w, http.StatusServiceUnavailable, "qa-off", "GAOS has no QA tool to run the automation (GAOS_QA_URL)")
		return
	}
	t, created, _, err := s.store.PlanAPICard(ctx, id, in, nowUnix())
	if err != nil {
		apiCardFail(w, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
		if in.Manual && s.nt != nil {
			s.apiManualNotice(t, cl.Name)
		}
	}
	apiJSON(w, status, apiTaskAnswer(t, created, false))
}

// apiTaskGet is GET /v1/products/{id}/tasks/{taskId} and GET /v1/products/{id}/tasks?externalId=.
func (s *server) apiTaskGet(w http.ResponseWriter, r *http.Request) {
	id, cl, _, ok := s.apiTaskGate(w, r, false)
	if !ok {
		return
	}
	client := ""
	if cl != nil {
		client = cl.Name
	}
	taskID, ext := r.PathValue("taskId"), ""
	if taskID == "" {
		ext = r.URL.Query().Get("externalId")
		if !apiExternalIDPattern.MatchString(ext) || client == "" {
			apiErr(w, http.StatusNotFound, "not-found", "no such task")
			return
		}
	}
	t, err := s.store.APICardGet(r.Context(), id, taskID, ext, client)
	if err != nil {
		apiCardFail(w, err)
		return
	}
	apiJSON(w, http.StatusOK, apiTaskAnswer(t, false, true))
}

// apiTaskCancel is POST /v1/products/{id}/tasks/{taskId}/cancel.
func (s *server) apiTaskCancel(w http.ResponseWriter, r *http.Request) {
	id, cl, scope, ok := s.apiTaskGate(w, r, true)
	if !ok {
		return
	}
	release, ok := s.beginWrite(w, scope, cl)
	if !ok {
		return
	}
	defer release()
	if _, err := s.store.APICardCancel(r.Context(), id, r.PathValue("taskId"), cl.Name); err != nil {
		apiCardFail(w, err)
		return
	}
	t, err := s.store.APICardGet(r.Context(), id, r.PathValue("taskId"), "", cl.Name)
	if err != nil {
		apiCardFail(w, err)
		return
	}
	apiJSON(w, http.StatusOK, apiTaskAnswer(t, false, true))
}

// apiManualNotice announces a client's manual-review request to the group, naming the client: an
// outside caller never chooses who is messaged (D-101). The card's key is cleared on the copy so
// the notifier looks up no author.
func (s *server) apiManualNotice(t TaskRecord, client string) {
	group := t
	group.DedupKey = ""
	go s.nt.Notify(context.Background(), group, KindReviewManual, "Client "+client+" asked for a manual review")
}
