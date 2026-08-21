package main

import (
	"context"
	"errors"
	"fmt"
)

// Store is the entire surface the handlers use. Two implementations: MemStore
// in store_mem.go and ArcadeStore in store_arcade.go, run against one
// conformance suite so the seam cannot rot.
//
// This file was written to say "THIS FILE DOES NOT CHANGE", and it did not --
// the whole ArcadeDB store landed against it untouched, which was the point of
// declaring the seam before it was needed. It changes here, once, for T-099:
// architecture §08 requires replay to go through a read-only handle rather than
// a flag, and a read-only handle is a thing the seam has to be able to express.
// readOnly below is that handle.
//
// Two rules the ArcadeDB implementation will need, and which are why the
// signatures look the way they do:
//
//   - Every method takes a context. That store talks HTTP to
//     http://arcadedb-master:2480 and must honour a deadline; retrofitting
//     contexts through a dozen handlers later is the kind of change that gets
//     half-done.
//
//   - Every mutation returns Result, not error alone. Architecture §09: "every
//     mutation returns either the new state or a rejection reason, never a bare
//     200." Result carries the changed state AND the events it emitted, so a
//     handler can render the fragment and its out-of-band chrome from one call
//     instead of reading back what it just wrote.
type Store interface {
	// Ready backs /healthz. MemStore always succeeds; ArcadeStore will issue
	// GET /api/v1/ready against the database and this handler stays untouched.
	Ready(ctx context.Context) error
	Kind() string // "memory" | "arcadedb"

	// ---- reads ----
	Snapshot(ctx context.Context) (Floor, error)
	Desk(ctx context.Context, idx int) (Desk, int, error)
	Agents(ctx context.Context) ([]Agent, error)
	Products(ctx context.Context) ([]Product, error)
	Master(ctx context.Context, productID string) (Master, error)
	Masters(ctx context.Context) ([]Master, error)
	ContextSlice(ctx context.Context, deskIdx int) ([]string, error)
	Events(ctx context.Context) ([]Event, error)

	// Focus moves the inspector without changing any domain state.
	Focus(ctx context.Context, idx int) (Result, error)

	// Replay folds the floor back to the state at an event. The argument is an
	// event Seq; passing -1 returns to live.
	//
	// A Seq rather than an offset into Events() because the stream is unbounded
	// and append-only (BR-04) -- an offset names a different moment as soon as
	// anything is prepended to the caller's view of the slice, and the pill the
	// operator clicked has to stay the pill they get.
	Replay(ctx context.Context, eventSeq int) (Result, error)

	// ---- floor mutations ----
	ApproveTask(ctx context.Context, idx int) (Result, error)
	RetryTask(ctx context.Context, idx int) (Result, error)
	CancelTask(ctx context.Context, idx int) (Result, error)
	CallReview(ctx context.Context, idx int) (Result, error)
	CloseMeeting(ctx context.Context) (Result, error)
	ClearNotifications(ctx context.Context) (Result, error)
	SetPaused(ctx context.Context, paused bool) (Result, error)
	SetSpeed(ctx context.Context, speed int) (Result, error)

	// ---- product master ----
	CreateProduct(ctx context.Context, d ProductDraft) (Result, error)
	UpdateBRD(ctx context.Context, productID string, b BRD) (Result, error)
	AddStory(ctx context.Context, productID string, s Story) (Result, error)
	AddDiagram(ctx context.Context, productID string, g Diagram) (Result, error)
	AddDecision(ctx context.Context, productID string, a Decision) (Result, error)
	Inject(ctx context.Context, productID string) (Result, error)

	// ---- planning ----
	// PlanWorkflow is pure: it reads the master and returns a task graph without
	// writing anything, so the wizard can preview a plan the user may discard.
	PlanWorkflow(ctx context.Context, d WorkflowDraft) ([]PlanTask, error)
	CreateWorkflow(ctx context.Context, d WorkflowDraft, plan []PlanTask) (Result, error)
}

// Result is what a mutation gives back: enough to render the changed fragment
// and the out-of-band chrome in one response.
//
// NewEvents is only the events THIS call emitted, not the whole stream. The
// timeline appends them with hx-swap-oob="beforeend"; re-sending all 160 pills
// on every tick would both waste the bandwidth and reset the track's scroll
// position, which is how you lose the place you were looking at.
type Result struct {
	Floor     Floor
	Master    *Master
	NewEvents []Event
	Toast     string
}

// Sentinel errors. ErrRejected is the "rejection reason" half of the §09 rule:
// a mutation that will not happen says why, in a form the handler can turn into
// a toast rather than a 500.
var (
	ErrNotFound = errors.New("not found")
	ErrRejected = errors.New("rejected")
)

// reject builds an ErrRejected carrying a human-readable reason.
func reject(reason string) error {
	return &rejection{reason: reason}
}

type rejection struct{ reason string }

func (r *rejection) Error() string { return r.reason }
func (r *rejection) Is(target error) bool {
	return target == ErrRejected
}

// NotReadyError is BR-07's refusal: the planner declines a record below
// PlanFloor and hands back what is missing. It satisfies ErrRejected, so every
// handler that already turns a rejection into a toast keeps working unchanged --
// but it carries the list too, because a refusal that does not say what is
// missing is a dead end (B-3), and the caller should not have to re-score the
// record to find out.
type NotReadyError struct {
	ProductID string
	Score     int
	Band      string
	Missing   []string
}

func (e *NotReadyError) Error() string {
	return fmt.Sprintf("readiness %d (%s) is below the planning floor of %d — %d signal(s) missing",
		e.Score, e.Band, PlanFloor, len(e.Missing))
}
func (e *NotReadyError) Is(target error) bool { return target == ErrRejected }

// ---------- the read-only handle ----------

// readOnly wraps a Store and refuses every mutation on it. T-099, and BR-20.
//
// Architecture §08 is unusually specific about why this exists rather than a
// boolean: "route replay through a read-only store handle rather than a flag
// someone can forget to check -- a flag will eventually be forgotten and will
// overwrite live state with a historical one."
//
// So this is not a check, it is a type. A handler holding a readOnly cannot
// approve a task by forgetting anything, because the method it would call is not
// the store's. Reads pass straight through, including Replay itself: moving the
// cursor is how you look at a different moment, and looking is what this handle
// is for. Focus passes through for the same reason -- the inspector follows the
// scrub, and neither writes an event.
//
// The wrapped store is still the live one. readOnly narrows what a caller can
// do with it; it does not copy or freeze anything, which is what makes it cheap
// enough to hand out per request.
type readOnly struct{ Store }

// replayRefusal is the one sentence every refused mutation gives back. Written
// once so the reason an operator sees is the same wherever they hit it.
const replayRefusal = "The floor is in replay. Return to live before changing anything."

func (readOnly) ApproveTask(context.Context, int) (Result, error) {
	return Result{}, reject(replayRefusal)
}
func (readOnly) RetryTask(context.Context, int) (Result, error) {
	return Result{}, reject(replayRefusal)
}
func (readOnly) CancelTask(context.Context, int) (Result, error) {
	return Result{}, reject(replayRefusal)
}
func (readOnly) CallReview(context.Context, int) (Result, error) {
	return Result{}, reject(replayRefusal)
}
func (readOnly) CloseMeeting(context.Context) (Result, error) { return Result{}, reject(replayRefusal) }
func (readOnly) SetPaused(context.Context, bool) (Result, error) {
	return Result{}, reject(replayRefusal)
}
func (readOnly) SetSpeed(context.Context, int) (Result, error) {
	return Result{}, reject(replayRefusal)
}

func (readOnly) ClearNotifications(context.Context) (Result, error) {
	return Result{}, reject(replayRefusal)
}
func (readOnly) CreateProduct(context.Context, ProductDraft) (Result, error) {
	return Result{}, reject(replayRefusal)
}
func (readOnly) UpdateBRD(context.Context, string, BRD) (Result, error) {
	return Result{}, reject(replayRefusal)
}
func (readOnly) AddStory(context.Context, string, Story) (Result, error) {
	return Result{}, reject(replayRefusal)
}
func (readOnly) AddDiagram(context.Context, string, Diagram) (Result, error) {
	return Result{}, reject(replayRefusal)
}
func (readOnly) AddDecision(context.Context, string, Decision) (Result, error) {
	return Result{}, reject(replayRefusal)
}
func (readOnly) Inject(context.Context, string) (Result, error) {
	return Result{}, reject(replayRefusal)
}
func (readOnly) CreateWorkflow(context.Context, WorkflowDraft, []PlanTask) (Result, error) {
	return Result{}, reject(replayRefusal)
}
