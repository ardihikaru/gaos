package main

// The external-tasks API's store half (D-099, docs/designs/gaos-external-tasks-and-pr-bundles.md
// §5): the shared card planner, the API's planning, adoption, reading and cancel. The HTTP half is
// api_tasks.go.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// cardItem is one card a caller has already worked out: key, id, title and spec. planCardsLocked
// owns only what every planner shares (design §5.2).
type cardItem struct {
	Key, ID, Title string
	Spec           prCardSpec
	Owner          string // apiOwner: "" for Brain Town and bundles
	Ref            string // the first apiRefs entry, "" for none
	AutoDispatch   bool
	At             int64 // apiAt
}

// taskByDedupKeyInLocked finds the card of one product that carries key.
func (s *MemStore) taskByDedupKeyInLocked(productID, key string) *TaskRecord {
	for i := range s.tasks {
		if s.tasks[i].DeskID == productID && s.tasks[i].DedupKey == key {
			return &s.tasks[i]
		}
	}
	return nil
}

// planCardsLocked plans items on wf-pr-<product>: the product-scoped dedup, the ordinal, the
// cards and one TaskPlanned whose message is msg. It checks no cap, emits no pull-request
// lifecycle event, moves no card and writes no row: each caller does its own.
func (s *MemStore) planCardsLocked(m *Master, d Desk, items []cardItem, msg string) (planned, existing []string, events []Event) {
	productID := m.ID
	wf := prWorkflowID(productID)
	ordinal := 0
	for i := range s.tasks {
		if s.tasks[i].DeskID == productID && s.tasks[i].WorkflowID == wf {
			ordinal++
		}
	}
	var plannedTasks []PlannedTask
	for _, it := range items {
		if t := s.taskByDedupKeyInLocked(productID, it.Key); t != nil {
			existing = append(existing, t.ID)
			continue
		}
		id := s.freeCardID(it.ID)
		ordinal++
		task := TaskRecord{
			ID: id, WorkflowID: wf, DeskID: productID,
			Title: prTitle(it.Title, id), Role: it.Spec.Role, Model: roles[it.Spec.Role].Model,
			Kind: TaskKindImplement, Status: StatusPending, Labels: normalizeLabels(it.Spec.Labels),
			DedupKey: it.Key, Priority: 50, Ordinal: ordinal,
			APIOwner: it.Owner, APIAt: it.At, AutoDispatch: it.AutoDispatch,
		}
		if it.Ref != "" {
			task.APIRefs = []string{it.Ref}
		}
		s.tasks = append(s.tasks, task)
		planned = append(planned, id)
		plannedTasks = append(plannedTasks, PlannedTask{ID: id, Title: task.Title, Role: task.Role,
			Model: task.Model, Priority: task.Priority, Kind: task.Kind, Labels: task.Labels, DedupKey: it.Key})
	}
	if len(planned) > 0 {
		p := deskPayload(d)
		p.WorkflowID = wf
		p.Tasks = plannedTasks
		events = append(events, s.emit(TaskPlanned, productID, msg, KindKnow, p))
	}
	return planned, existing, events
}

// ---------------------------------------------------------------- the API's request

// The kinds of POST /v1/products/{id}/tasks (design §5.1).
const (
	APIKindPRReview   = "pr-review"
	APIKindUnitTest   = "unit-test"
	APIKindAutoCreate = "automation-create"
	APIKindAutoRun    = "automation-run"
)

var (
	apiExternalIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	apiCaseCodePattern   = regexp.MustCompile(`^[A-Z][A-Z0-9]{0,15}(-[A-Z0-9]{1,16}){1,4}$`)
)

// APICardIn is one validated request. Owner, Repo and Number are set for the pull-request kinds,
// CaseCode for automation-run.
type APICardIn struct {
	Client, ExternalID, Kind string
	Owner, Repo              string
	Number                   int
	Manual                   bool // review: manual
	CaseCode                 string
	Dispatch                 bool
}

// apiCardErr is a refusal with the API's status and code (design §5.4).
type apiCardErr struct {
	Status       int
	Code, Reason string
}

func (e *apiCardErr) Error() string { return e.Code + ": " + e.Reason }

func refuse(status int, code, reason string) error { return &apiCardErr{status, code, reason} }

// apiPRKind reports whether the kind takes a pull request.
func apiPRKind(kind string) bool {
	return kind == APIKindPRReview || kind == APIKindUnitTest || kind == APIKindAutoCreate
}

// apiModelKind reports whether the kind is a model turn (dispatch applies) rather than a QA-tool job.
func apiModelKind(kind string) bool { return kind == APIKindPRReview || kind == APIKindUnitTest }

// apiJob is the pull-request flow's job for a kind.
func apiJob(kind string) string {
	switch kind {
	case APIKindPRReview:
		return prJobReviewer
	case APIKindUnitTest:
		return prJobUnitTest
	case APIKindAutoCreate:
		return prJobE2EBuild
	}
	return prJobE2ERun
}

// apiEvent is the PREvent a pull-request kind stands for, so its key, id and spec are Brain
// Town's own (design §5.2).
func apiEvent(in APICardIn) PREvent {
	typ := prTypeOpened
	if in.Kind == APIKindUnitTest {
		typ = prTypeMerged
	}
	return PREvent{Kind: "api", Type: typ, Org: in.Owner, Repo: in.Repo, PRNumber: in.Number}
}

// qaRunPrefix starts an automation-run card's key: it carries the product and the client, and no
// "#", so prKeyParts never reads it as a pull request.
const qaRunPrefix = "qa-run/"

// bundlePrefix starts a bundle's test-planner key (D-100).
const bundlePrefix = "bundle/"

// apiKey is the card key a request maps to.
func apiKey(productID string, in APICardIn) string {
	if in.Kind == APIKindAutoRun {
		code := in.CaseCode
		if code == "*" {
			code = "ALL"
		}
		return qaRunPrefix + productID + "/" + in.Client + "/" + code + "/" + in.ExternalID + ":" + prJobE2ERun
	}
	return apiEvent(in).Key(apiJob(in.Kind))
}

// apiSpec is the routing table's spec for a kind (design §5.2), and the card's title and id.
func apiSpec(productID string, in APICardIn) (prCardSpec, string, string) {
	wf := prWorkflowID(productID)
	if in.Kind == APIKindAutoRun {
		title := "E2E run of " + in.CaseCode
		if in.CaseCode == "*" {
			title = "E2E run of every automated case"
		}
		return prCardSpec{Job: prJobE2ERun, Role: "qa", Labels: []string{LabelE2ERun}, Title: title},
			title, wf + "-qa-run-1"
	}
	ev := apiEvent(in)
	for _, spec := range prCardsFor(ev) {
		if spec.Job == apiJob(in.Kind) {
			if in.Kind == APIKindPRReview && in.Manual {
				spec.Labels = []string{LabelPRReviewManual}
			}
			return spec, spec.Title, prCardID(wf, ev, spec.Job)
		}
	}
	return prCardSpec{}, "", ""
}

// apiRefOf is the read grant a request records.
func apiRefOf(in APICardIn) string { return in.Client + ":" + in.ExternalID }

// refClient is the client part of a ref: up to the first ":".
func refClient(ref string) string {
	if i := strings.Index(ref, ":"); i >= 0 {
		return ref[:i]
	}
	return ref
}

// hasRefOf reports whether the card grants client (compared whole).
func hasRefOf(t *TaskRecord, client string) bool {
	for _, r := range t.APIRefs {
		if refClient(r) == client {
			return true
		}
	}
	return false
}

const apiMaxRefs = 8

// envPosInt reads a positive integer setting, or def.
func envPosInt(name string, def int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name))); err == nil && n > 0 {
		return n
	}
	return def
}

func apiMaxOpen() int { return envPosInt("GAOS_API_MAX_OPEN", 5) }
func apiDaily() int   { return envPosInt("GAOS_API_DAILY", 20) }

// finished reports a terminal status.
func finished(status string) bool {
	return status == StatusDone || status == StatusFailed || status == StatusCanceled
}

// utcDay is the UTC day of a Unix second.
func utcDay(at int64) int64 { return at / 86400 }

// ---------------------------------------------------------------- lookup and adoption

// APILookup is what the lookup found (design §5.3 step 5).
type APILookup int

const (
	APIMiss  APILookup = iota // nothing: a card will be created
	APIOwn                    // the client's own card for this externalId
	APIAdopt                  // a card with the natural key: adopt it
)

// apiLookupLocked is the design's step 5, product-scoped.
func (s *MemStore) apiLookupLocked(productID string, in APICardIn) (*TaskRecord, APILookup, error) {
	key, ref := apiKey(productID, in), apiRefOf(in)
	for i := range s.tasks {
		t := &s.tasks[i]
		if t.DeskID != productID {
			continue
		}
		for _, r := range t.APIRefs {
			if r == ref {
				if t.DedupKey != key {
					return nil, APIMiss, refuse(409, "externalId-reused", "this externalId already names another request")
				}
				return t, APIOwn, nil
			}
		}
	}
	if strings.HasPrefix(key, qaRunPrefix) {
		return nil, APIMiss, nil // a run carries its client: never adopted
	}
	if t := s.taskByDedupKeyInLocked(productID, key); t != nil {
		return t, APIAdopt, nil
	}
	return nil, APIMiss, nil
}

// APICardLookup is the lookup without a write: no GitHub call is made before it (design §5.3).
func (s *MemStore) APICardLookup(ctx context.Context, productID string, in APICardIn) (TaskRecord, APILookup, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, kind, err := s.apiLookupLocked(productID, in)
	if err != nil || t == nil {
		return TaskRecord{}, kind, err
	}
	return t.Clone(), kind, nil
}

// adoptLocked appends the request's ref to t: one ref per client, at most eight, the owner's
// never evicted. It changes nothing else about the card (design §5.3).
func (s *MemStore) adoptLocked(t *TaskRecord, in APICardIn) (bool, error) {
	if hasRefOf(t, in.Client) {
		return false, nil
	}
	if len(t.APIRefs) >= apiMaxRefs {
		return false, refuse(409, "too-many-refs", "eight clients already follow this card")
	}
	t.APIRefs = append(t.APIRefs, apiRefOf(in))
	return true, nil
}

// AdoptAPICard records a client's read grant on a natural-key card. It is a read grant, not a
// state change of the plan, so it emits no event (design §5.6); the task row is rewritten.
func (s *MemStore) AdoptAPICard(ctx context.Context, productID, taskID string, in APICardIn) (TaskRecord, Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.taskByIDLocked(taskID)
	if t == nil || t.DeskID != productID {
		return TaskRecord{}, Result{}, ErrNotFound
	}
	changed, err := s.adoptLocked(t, in)
	if err != nil {
		return TaskRecord{}, Result{}, err
	}
	if !changed {
		return t.Clone(), Result{}, nil
	}
	return t.Clone(), Result{Floor: s.floor()}, nil
}

// ---------------------------------------------------------------- planning

// PlanAPICard plans one card for a client (design §5.3 step 7). It repeats the lookup under the
// write lock -- Brain Town or another client may have planned the key during the GitHub call --
// then the three limits, then the card. created is false when it adopted or found one.
func (s *MemStore) PlanAPICard(ctx context.Context, productID string, in APICardIn, now int64) (TaskRecord, bool, Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.masterPtr(productID)
	idx := s.deskIdxByID(productID)
	if m == nil || idx < 0 {
		return TaskRecord{}, false, Result{}, ErrNotFound
	}
	t, kind, err := s.apiLookupLocked(productID, in)
	if err != nil {
		return TaskRecord{}, false, Result{}, err
	}
	switch kind {
	case APIOwn:
		return t.Clone(), false, Result{}, nil
	case APIAdopt:
		changed, err := s.adoptLocked(t, in)
		if err != nil {
			return TaskRecord{}, false, Result{}, err
		}
		if changed {
			return t.Clone(), false, Result{Floor: s.floor()}, nil
		}
		return t.Clone(), false, Result{}, nil
	}
	open, today, runs := 0, 0, 0
	for i := range s.tasks {
		x := &s.tasks[i]
		if x.APIOwner == in.Client && x.DeskID == productID {
			if !finished(x.Status) {
				open++
			}
			if utcDay(x.APIAt) == utcDay(now) {
				today++
			}
		}
		if x.DeskID == productID && strings.HasPrefix(x.DedupKey, qaRunPrefix) && !finished(x.Status) {
			runs++
		}
	}
	switch {
	case open >= apiMaxOpen():
		return TaskRecord{}, false, Result{}, refuse(409, "cap", fmt.Sprintf("this client has %d open cards (GAOS_API_MAX_OPEN)", open))
	case today >= apiDaily():
		return TaskRecord{}, false, Result{}, refuse(409, "daily-cap", fmt.Sprintf("this client planned %d cards today (GAOS_API_DAILY)", today))
	case in.Kind == APIKindAutoRun && runs > 0:
		return TaskRecord{}, false, Result{}, refuse(409, "run-in-flight", "an automation run is already open on this product")
	}
	spec, title, id := apiSpec(productID, in)
	item := cardItem{Key: apiKey(productID, in), ID: id, Title: title, Spec: spec,
		Owner: in.Client, Ref: apiRefOf(in), At: now, AutoDispatch: in.Dispatch && apiModelKind(in.Kind)}
	where := in.Owner + "/" + in.Repo
	if in.Kind == APIKindAutoRun {
		where = productID
	}
	planned, _, events := s.planCardsLocked(m, s.desks[idx], []cardItem{item}, "1 task(s) planned by the API on "+where)
	if len(planned) == 0 {
		return TaskRecord{}, false, Result{}, refuse(409, "skipped", "the card could not be planned")
	}
	var rows []PullRequest
	if apiPRKind(in.Kind) {
		rows = s.reviewRowLocked(m, apiEvent(in))
	}
	c := m.Clone()
	card := s.taskByIDLocked(planned[0]).Clone()
	return card, true, Result{Master: &c, NewEvents: events, PullRequests: rows, Floor: s.floor(),
		Toast: "1 task planned by the API."}, nil
}

// ---------------------------------------------------------------- reading and cancel

// APICardGet answers one card for the caller: by id, or by externalId when taskID is "". A client
// sees only a card whose refs name it; the admin token (client "") any wf-pr card (design §5.4).
func (s *MemStore) APICardGet(ctx context.Context, productID, taskID, externalID, client string) (TaskRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	wf := prWorkflowID(productID)
	for i := range s.tasks {
		t := &s.tasks[i]
		if t.DeskID != productID || t.WorkflowID != wf {
			continue
		}
		if taskID != "" && t.ID != taskID {
			continue
		}
		if externalID != "" {
			want := client + ":" + externalID
			found := false
			for _, r := range t.APIRefs {
				found = found || r == want
			}
			if !found {
				continue
			}
		}
		if client != "" && !hasRefOf(t, client) {
			continue
		}
		return t.Clone(), nil
	}
	return TaskRecord{}, ErrNotFound
}

// RuleAPICancel names the actor of a cancel through the API.
const RuleAPICancel = "api-cancel"

// APICardCancel cancels a card the client created whose key is an automation run: a natural-key
// card belongs to Brain Town and the operator, and a cancelled natural key would swallow the
// real event (design §5.4). The QA job runner then cancels the job (qajobs.go cardCanceled).
func (s *MemStore) APICardCancel(ctx context.Context, productID, taskID, client string) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.taskByIDLocked(taskID)
	if t == nil || t.DeskID != productID || t.APIOwner == "" || t.APIOwner != client {
		return Result{}, ErrNotFound
	}
	if !strings.HasPrefix(t.DedupKey, qaRunPrefix) {
		return Result{}, refuse(409, "not-cancelable", "only an automation run this client created can be canceled here")
	}
	if finished(t.Status) {
		return Result{}, refuse(409, "finished", "the card is "+t.Status)
	}
	mv, ok := s.moveTaskLocked(t.ID, StatusCanceled, RuleAPICancel)
	if !ok {
		return Result{}, refuse(409, "not-cancelable", "the card cannot be canceled from "+t.Status)
	}
	return Result{NewEvents: []Event{mv}, Floor: s.floor()}, nil
}

// apiPublicReason maps a card's reason to the closed public set (design §5.4): a secret hold, a
// QA job id or a GitHub error class never leaves GAOS.
func apiPublicReason(t TaskRecord) string {
	switch {
	case t.BlockReason == RuleSecretHeld:
		return "held"
	case t.Status == StatusBlocked:
		return "blocked"
	case t.Status == StatusFailed:
		return "failed"
	}
	return ""
}

// nowUnix is the API's clock; tests replace it.
var nowUnix = func() int64 { return time.Now().Unix() }

// shortHash is the first n hex of sha256(s).
func shortHash(s string, n int) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])[:n]
}
