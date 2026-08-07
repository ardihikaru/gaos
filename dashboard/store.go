package main

import (
	"context"
	"errors"
)

// Store is the entire surface the handlers use. MemStore is the only
// implementation in this pass; store_arcade.go in the next one speaks the
// ArcadeDB HTTP API and THIS FILE DOES NOT CHANGE. That is the whole point of
// having it -- the seam is declared before it is needed, while the shape is
// still cheap to get right.
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

	// Replay folds the floor back to the state at an event. Passing -1 returns
	// to live. Architecture §08: replay must go through a read-only handle, so
	// this returns a Floor and never mutates anything a live read can see.
	Replay(ctx context.Context, eventIdx int) (Result, error)

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
