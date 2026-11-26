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
	// SaveVendorPolicy replaces which AI vendors run the product's tasks
	// (M16, D-067). It takes effect at each task's next new session.
	SaveVendorPolicy(ctx context.Context, productID string, p VendorPolicy) (Result, error)
	AddKnowledgeSource(ctx context.Context, productID string, k KnowledgeSource) (Result, error)
	PublishBlueprint(ctx context.Context, productID string) (Result, error)
	// SyncBlueprint writes the latest published version into the product's
	// workspace, provisioning it first if needed (M14.4). Rejected without a
	// workspace gateway, without a published version, or when the egress
	// pass refuses the files.
	SyncBlueprint(ctx context.Context, productID string) (Result, error)

	// PlanPullRequestCards plans the cards a pull-request event needs (D-088).
	// It answers the ids of the cards it planned and the ids of the ones that
	// were already there -- a reader replays, and a replay must not open a
	// second card. The events ride in the Result.
	PlanPullRequestCards(ctx context.Context, productID string, ev PREvent) (planned []string, existing []string, res Result, err error)

	// The board (M14.6, board.go): a projection of the product's tasks, and a
	// move on it -- a request the task state machine may refuse (BR-43).
	Board(ctx context.Context, productID string) (Board, error)
	// BoardPage is the board cut to one page (D-094, board_page.go): the cards
	// that match q, w's depth per column, decorated only once chosen. BoardCard
	// is one card, decorated the same way. EventCount is how many events a
	// product has, for the Timeline tab's number, without a copy of the stream.
	BoardPage(ctx context.Context, productID string, q BoardQuery, w BoardWindow) (BoardPage, error)
	BoardCard(ctx context.Context, productID, taskID string) (BoardCard, error)
	EventCount(ctx context.Context, productID string) (int, error)
	// CardTiming is one card's timing history: its status segments, the
	// totals that partition its lead time, its attempts (D-081,
	// docs/designs/card-timing.md). ErrNotFound for an unknown card.
	CardTiming(ctx context.Context, productID, taskID string) (CardTiming, error)

	// A card's conversation, its archive and its prune (D-082,
	// docs/designs/conversations.md). Conversation and ArchivedConversations
	// read; ArchiveConversation and PruneConversations write.
	Conversation(ctx context.Context, productID, taskID string) (Conversation, error)
	ArchiveConversation(ctx context.Context, productID, taskID, actor string, archive bool) (Result, error)
	ArchivedConversations(ctx context.Context) (ArchiveView, error)
	PruneConversations(ctx context.Context, actor string, cards []CardRef) (Result, error)
	MoveCard(ctx context.Context, productID, taskID string, m CardMove) (Result, error)
	// SetTaskLabel changes a card's label and emits TaskLabeled (D-088).
	SetTaskLabel(ctx context.Context, productID, taskID, label, actor string) (Result, error)
	// PRCards are a product's pull-request cards (those carrying a dedup key), as
	// copies: the QA-job runner reads their status, labels and reason (D-088).
	PRCards(ctx context.Context, productID string) ([]TaskRecord, error)
	// Defects and auto-fix (D-091, defects.go, defecttriage.go). Every write is
	// one transaction: the record, its cards and its events together.
	ReportDefects(ctx context.Context, productID string, in DefectReportIn) (DefectReportOut, Result, error)
	Defects(ctx context.Context, productID string) ([]Defect, error)
	DefectByKey(ctx context.Context, productID, key string) (Defect, error)
	SetDefectContext(ctx context.Context, productID, key string, c DefectContext) (Result, error)
	ResolveTriage(ctx context.Context, productID, taskID string, v TriageVerdict) (Result, error)
	CloseDefect(ctx context.Context, productID, ref, actor string) (Result, error)
	FixPRs(ctx context.Context, productID string) ([]FixPR, error)
	UpsertFixPR(ctx context.Context, f FixPR) (Result, error)
	// AutoDispatch dispatches one READY defect card behind the product's switch and
	// caps (autofix.go); OpenFixPR records a draft pull request and ends its card.
	AutoDispatch(ctx context.Context, productID, taskID string) (Result, error)
	OpenFixPR(ctx context.Context, productID, taskID string, f FixPR) (Result, error)
	// AdvanceFixPR and ExpireExcerpts are the poller's writes (fixprpoll.go).
	AdvanceFixPR(ctx context.Context, productID string, f FixPR) (Result, error)
	// The Pull requests page's rows (pullrequests.go, D-092): review and QA-tool pull requests.
	PullRequests(ctx context.Context, productID string) ([]PullRequest, error)
	UpsertPullRequest(ctx context.Context, p PullRequest) (Result, error)
	AdvancePullRequest(ctx context.Context, productID, key string, f PRFields) (Result, error)
	PrunePullRequests(ctx context.Context, finalBefore int64) (Result, error)
	// BackfillReviewRows writes the rows of review cards that already exist (once, at boot).
	BackfillReviewRows(ctx context.Context) (Result, error)
	// RetargetFixPR changes the base of an open fix pull request by raising the defect's generation
	// and planning a fix card against the new base (retarget.go, D-092).
	RetargetFixPR(ctx context.Context, productID, fixPRID, base, actor, tokenKind string) (Result, error)
	ExpireExcerpts(ctx context.Context, now int64) (Result, error)
	AutoFixOf(ctx context.Context, productID string) (AutoFix, error)
	SetAutoFix(ctx context.Context, productID string, a AutoFix, actor, tokenKind string) (Result, error)
	// The contact registry and the one event a notification leaves (D-088).
	ListContacts(ctx context.Context) ([]Contact, error)
	UpsertContact(ctx context.Context, c Contact, actor string) (Result, error)
	DeleteContact(ctx context.Context, productID, login, actor string) (Result, error)
	RecordNotification(ctx context.Context, productID, alias, kind, outcome string) (Result, error)
	// ResolvePRCard moves a pull-request card to DONE or FAILED with a reason
	// (D-088): the outcome of the review post or of a QA-tool job.
	ResolvePRCard(ctx context.Context, productID, taskID, to, reason string) (Result, error)
	// SendBackDesign sends the design a review blocked back to its author
	// (D-084); actor is the signed-in account. SendBackSheet is the same
	// check, read-only, for the sheet. ErrNotFound for a card of no product.
	SendBackDesign(ctx context.Context, productID, reviewTaskID, actor string, triage []FindingTriage) (Result, error)
	SendBackSheet(ctx context.Context, productID, reviewTaskID string) (SendBackView, error)
	Inject(ctx context.Context, productID string) (Result, error)

	// "Needs you" (M15, questions.go): the agents waiting on the operator, one
	// product's or ("") all, and the answer that puts a task back in the queue.
	OpenQuestions(ctx context.Context, productID string) ([]Question, error)
	AnswerQuestion(ctx context.Context, productID, taskID string, choice int, text string, by AnswerSource) (Result, error)
	// RecentAnswers is the inbox's "Answered recently" list (D-083), newest first.
	RecentAnswers(ctx context.Context, productID string, limit int) ([]RecentAnswer, error)
	// ExpireQuestions closes the questions that outlived their session
	// (D-066); main.go sweeps it once a minute.
	ExpireQuestions(ctx context.Context, now int64) (Result, error)

	// The QA tool's questions (external_inbox.go, D-078): items of the inbox
	// that belong to no task. Their Results carry no Floor. AskExternal is
	// idempotent on (product, externalId) and reports whether it created the
	// item; ExternalPending and RecordExternalDelivery are the answer
	// webhook's half (inbox_worker.go).
	AskExternal(ctx context.Context, productID string, in ExternalAsk) (ExternalItem, bool, Result, error)
	ExternalGet(ctx context.Context, productID, externalID string) (ExternalItem, error)
	AnswerExternal(ctx context.Context, productID, externalID string, in ExternalAnswer) (Result, error)
	ExternalPending(ctx context.Context) ([]ExternalItem, error)
	RecordExternalDelivery(ctx context.Context, productID, externalID string, attempt int, outcome, reason string, at int64) (Result, error)

	// The QA catalog (qacatalog.go, D-072). The three writes carry NO floor in
	// their Result: the catalog is not part of the floor, and a write that
	// carried one would save a replayed past over the live desks (BR-20).
	QaCases(ctx context.Context, productID string, q QaQuery) (QaPage, error)
	QaSummary(ctx context.Context, productID string) (QaSummary, error)
	QaImport(ctx context.Context, productID string, in QaImportIn) (QaImportOut, Result, error)
	QaApplyResults(ctx context.Context, productID string, in QaResultsIn) (QaResultsOut, Result, error)
	// QaCreate is the QA tools' create-only door (design §4.12): new cases, new
	// epics and, for a new epic, one BLOCKED card.
	QaCreate(ctx context.Context, productID string, in QaCreateIn) (QaCreateOut, Result, error)
	// QaRecordSync records what one direction of the outbound sync did.
	QaRecordSync(ctx context.Context, productID, dir string, side QaSyncSide) (Result, error)

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

	// QaCases are the catalog cases this mutation changed; ArcadeStore writes
	// them in the same transaction as the events.
	QaCases []QaCase

	// Contacts are registry changes ArcadeStore writes in the same transaction
	// (D-088). They never ride an event.
	Contacts []ContactOp

	// Defects and FixPRs are the records a defect mutation changed (D-091);
	// ArcadeStore writes them in the same transaction.
	Defects []Defect
	FixPRs  []FixPR

	// PullRequests are the rows a mutation changed and PullRequestDeletes the keys it removed
	// (pullrequests.go); ArcadeStore writes them in the same transaction.
	PullRequests       []PullRequest
	PullRequestDeletes []string
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
func (readOnly) ArchiveConversation(context.Context, string, string, string, bool) (Result, error) {
	return Result{}, reject(replayRefusal)
}
func (readOnly) PruneConversations(context.Context, string, []CardRef) (Result, error) {
	return Result{}, reject(replayRefusal)
}
func (readOnly) SetTaskLabel(context.Context, string, string, string, string) (Result, error) {
	return Result{}, reject(replayRefusal)
}
func (readOnly) ResolvePRCard(context.Context, string, string, string, string) (Result, error) {
	return Result{}, reject(replayRefusal)
}
func (readOnly) UpsertContact(context.Context, Contact, string) (Result, error) {
	return Result{}, reject(replayRefusal)
}
func (readOnly) DeleteContact(context.Context, string, string, string) (Result, error) {
	return Result{}, reject(replayRefusal)
}
func (readOnly) RecordNotification(context.Context, string, string, string, string) (Result, error) {
	return Result{}, reject(replayRefusal)
}
func (readOnly) MoveCard(context.Context, string, string, CardMove) (Result, error) {
	return Result{}, reject(replayRefusal)
}
func (readOnly) SendBackDesign(context.Context, string, string, string, []FindingTriage) (Result, error) {
	return Result{}, reject(replayRefusal)
}
func (readOnly) AnswerQuestion(context.Context, string, string, int, string, AnswerSource) (Result, error) {
	return Result{}, reject(replayRefusal)
}
func (readOnly) ExpireQuestions(context.Context, int64) (Result, error) {
	return Result{}, reject(replayRefusal)
}
func (readOnly) AskExternal(context.Context, string, ExternalAsk) (ExternalItem, bool, Result, error) {
	return ExternalItem{}, false, Result{}, reject(replayRefusal)
}
func (readOnly) AnswerExternal(context.Context, string, string, ExternalAnswer) (Result, error) {
	return Result{}, reject(replayRefusal)
}
func (readOnly) RecordExternalDelivery(context.Context, string, string, int, string, string, int64) (Result, error) {
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

func (readOnly) QaImport(context.Context, string, QaImportIn) (QaImportOut, Result, error) {
	return QaImportOut{}, Result{}, reject(replayRefusal)
}
func (readOnly) QaApplyResults(context.Context, string, QaResultsIn) (QaResultsOut, Result, error) {
	return QaResultsOut{}, Result{}, reject(replayRefusal)
}
func (readOnly) QaRecordSync(context.Context, string, string, QaSyncSide) (Result, error) {
	return Result{}, reject(replayRefusal)
}
func (readOnly) QaCreate(context.Context, string, QaCreateIn) (QaCreateOut, Result, error) {
	return QaCreateOut{}, Result{}, reject(replayRefusal)
}

func (readOnly) ReportDefects(context.Context, string, DefectReportIn) (DefectReportOut, Result, error) {
	return DefectReportOut{}, Result{}, reject(replayRefusal)
}
func (readOnly) SetDefectContext(context.Context, string, string, DefectContext) (Result, error) {
	return Result{}, reject(replayRefusal)
}
func (readOnly) ResolveTriage(context.Context, string, string, TriageVerdict) (Result, error) {
	return Result{}, reject(replayRefusal)
}
func (readOnly) CloseDefect(context.Context, string, string, string) (Result, error) {
	return Result{}, reject(replayRefusal)
}
func (readOnly) UpsertFixPR(context.Context, FixPR) (Result, error) {
	return Result{}, reject(replayRefusal)
}
func (readOnly) SetAutoFix(context.Context, string, AutoFix, string, string) (Result, error) {
	return Result{}, reject(replayRefusal)
}
