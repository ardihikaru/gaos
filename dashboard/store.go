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
// It changes a second time, on 2026-08-22, for T-126: DispatchTask. The seam
// grows a method rather than the handlers growing a code path, for the reason
// the file was written around in the first place -- a dispatch emits events,
// and only a store may. Both changes are additive, and store_arcade.go needed
// one line for each.
//
// And a third time, on 2026-08-23, for M4.3. This one is not additive:
// ContextSlice([]string) becomes TaskContext(ContextView), and the replacement
// is the requirement rather than a tidy-up. D-6 says the slice sent to a model
// is frozen and stored per attempt; a []string cannot say whether it is the
// frozen one or a fresh guess at the same question, and a caller that cannot
// tell those apart will eventually present the guess as evidence. The return
// type carries Frozen because the distinction is the point. Runs is the other
// half, T-054: attempts are records now, and something has to be able to ask
// for them.
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
	// TaskContext answers GET /tasks/{id}/context: the frozen slice the desk's
	// most recent attempt was given, or -- when nothing has run -- the live
	// assembly, saying so. T-056, requirement D-6.
	TaskContext(ctx context.Context, deskIdx int) (ContextView, error)

	// Runs is one task's attempts, oldest first. T-054, D-5, BR-22.
	Runs(ctx context.Context, taskID string) ([]TaskRunRecord, error)

	Events(ctx context.Context) ([]Event, error)

	// Replay folds the floor back to the state at an event. The argument is an
	// event Seq; passing -1 returns to live.
	//
	// A Seq rather than an offset into Events() because the stream is unbounded
	// and append-only (BR-04) -- an offset names a different moment as soon as
	// anything is prepended to the caller's view of the slice, and the pill the
	// operator clicked has to stay the pill they get.
	Replay(ctx context.Context, eventSeq int) (Result, error)

	// ---- floor mutations ----

	// DispatchTask freezes a task packet, runs it through the pre-dispatch
	// egress pass, and dispatches what survives. T-126, T-127.
	//
	// It is on the seam rather than above it because it emits events, and the
	// store is the only thing that may. A refusal comes back as a Result with
	// a toast and a nil error, not as ErrRejected -- see the implementation for
	// why that is deliberate rather than sloppy.
	DispatchTask(ctx context.Context, idx int) (Result, error)

	ApproveTask(ctx context.Context, idx int) (Result, error)
	RetryTask(ctx context.Context, idx int) (Result, error)
	CancelTask(ctx context.Context, idx int) (Result, error)

	// ---- product master ----
	CreateProduct(ctx context.Context, d ProductDraft) (Result, error)
	UpdateBRD(ctx context.Context, productID string, b BRD) (Result, error)
	AddStory(ctx context.Context, productID string, s Story) (Result, error)
	AddDiagram(ctx context.Context, productID string, g Diagram) (Result, error)
	AddDecision(ctx context.Context, productID string, a Decision) (Result, error)

	// The Blueprint (D-058). SavePolicy adds or replaces a policy by key;
	// AddKnowledgeSource records a pinned repository; PublishBlueprint renders
	// the draft into an immutable version — and does nothing when the render is
	// unchanged, so one digest never names two versions.
	SavePolicy(ctx context.Context, productID string, p Policy) (Result, error)
	AddKnowledgeSource(ctx context.Context, productID string, k KnowledgeSource) (Result, error)
	PublishBlueprint(ctx context.Context, productID string) (Result, error)
	// SyncBlueprint writes the latest published version into the product's
	// workspace, provisioning it first if needed (M14.4). Rejected without a
	// workspace gateway, without a published version, or when the egress
	// pass refuses the files.
	SyncBlueprint(ctx context.Context, productID string) (Result, error)

	// The board (M14.6, board.go): a projection of the product's tasks, and a
	// move on it -- a request the task state machine may refuse (BR-43).
	Board(ctx context.Context, productID string) (Board, error)
	MoveCard(ctx context.Context, productID, taskID string, m CardMove) (Result, error)
	Inject(ctx context.Context, productID string) (Result, error)

	// "Needs you" (M15, questions.go): the agents waiting on the operator, one
	// product's or ("") all, and the answer that puts a task back in the queue.
	OpenQuestions(ctx context.Context, productID string) ([]Question, error)
	AnswerQuestion(ctx context.Context, productID, taskID string, choice int, text string) (Result, error)

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

func (readOnly) DispatchTask(context.Context, int) (Result, error) {
	return Result{}, reject(replayRefusal)
}
func (readOnly) ApproveTask(context.Context, int) (Result, error) {
	return Result{}, reject(replayRefusal)
}
func (readOnly) RetryTask(context.Context, int) (Result, error) {
	return Result{}, reject(replayRefusal)
}
func (readOnly) CancelTask(context.Context, int) (Result, error) {
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
func (readOnly) SavePolicy(context.Context, string, Policy) (Result, error) {
	return Result{}, reject(replayRefusal)
}
func (readOnly) AddKnowledgeSource(context.Context, string, KnowledgeSource) (Result, error) {
	return Result{}, reject(replayRefusal)
}
func (readOnly) PublishBlueprint(context.Context, string) (Result, error) {
	return Result{}, reject(replayRefusal)
}
func (readOnly) MoveCard(context.Context, string, string, CardMove) (Result, error) {
	return Result{}, reject(replayRefusal)
}
func (readOnly) AnswerQuestion(context.Context, string, string, int, string) (Result, error) {
	return Result{}, reject(replayRefusal)
}
func (readOnly) SyncBlueprint(context.Context, string) (Result, error) {
	return Result{}, reject(replayRefusal)
}
func (readOnly) Inject(context.Context, string) (Result, error) {
	return Result{}, reject(replayRefusal)
}
func (readOnly) CreateWorkflow(context.Context, WorkflowDraft, []PlanTask) (Result, error) {
	return Result{}, reject(replayRefusal)
}
