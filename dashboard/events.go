package main

// The event catalog, closed on purpose.
//
// artifacts/v1/gaos-arcadedb-model.html §08 lists these names and says "keep
// this list closed". A const block is how you keep a list closed in Go: a typo
// in an emit() call is a compile error rather than a new event type nobody
// planned for, silently appearing in the stream that the whole replay model is
// folded from.
//
// The architecture doc's first invariant is that an event which was never
// written is a state change that never happened. The corollary is that an event
// written under an invented name is a state change nothing downstream can read.
//
// The names are grouped exactly as the model document groups them. Names marked
// "needs the review engine" or "needs the budget guard" are declared and not
// emitted: they belong to modules that do not exist yet. They are declared
// anyway so that the catalog in code is the catalog in the document, and so a
// reviewer can see at a glance what is still missing. Emitting one of them from
// sim.go would be inventing evidence, and the audit trail is the last place to
// do that.
//
// The whole execution group came off that list on 2026-08-22. RunStarted,
// Heartbeat and RunExited when T-050 and T-052 gave them a supervisor that
// sends them; TaskOrphaned when T-053 gave it the one thing that can observe an
// orphan, which is a RESTART -- no live supervisor ever sees one, by definition,
// because an orphan is a run whose supervisor is already gone.
//
// Every name that IS emitted has a case in fold.go. That is BR-05 in practice:
// the catalog is closed because a new event type is a schema change to the fold,
// so the constant and the reducer case land in the same change or neither does.

type EventType string

// master
const (
	ProductCreated    EventType = "ProductCreated"
	RecordUpdated     EventType = "RecordUpdated"
	KnowledgeInjected EventType = "KnowledgeInjected"
	ReadinessEvaluate EventType = "ReadinessEvaluated"
	ReadinessDropped  EventType = "ReadinessDropped"

	// D-058: a Blueprint version was published. Lands with its fold case and
	// the data model document §15, per BR-05.
	BlueprintPublished EventType = "BlueprintPublished"

	// M14.4: the product's workspace was created on the worker, and a
	// published version was written into it. Same BR-05 rule.
	WorkspaceProvisioned EventType = "WorkspaceProvisioned"
	BlueprintSynced      EventType = "BlueprintSynced"
)

// planning
const (
	WorkflowCreated   EventType = "WorkflowCreated"
	TaskPlanned       EventType = "TaskPlanned"
	DependencyRewired EventType = "DependencyRewired" // needs the planner's task graph -- M3.2
	WorkflowCanceled  EventType = "WorkflowCanceled"

	// M14.6, D-061: a card moved on the board -- a status change the state
	// machine allowed (Rule names the action), or a new position inside its
	// column. Neither moves a desk: the board is a projection of the task
	// graph, and reusing the desk-moving events would make a replayed floor
	// disagree with the live one.
	TaskMoved     EventType = "TaskMoved"
	TaskReordered EventType = "TaskReordered"
)

// execution
const (
	TaskDispatched EventType = "TaskDispatched"
	RunStarted     EventType = "RunStarted"
	Heartbeat      EventType = "Heartbeat"
	RunExited      EventType = "RunExited"
	TaskBlocked    EventType = "TaskBlocked"
	TaskResumed    EventType = "TaskResumed"
	TaskOrphaned   EventType = "TaskOrphaned"

	// M14.5, D-056: a turn ran in a new session (Rule: first | escalation |
	// lost | rotation), and a session the task meant to resume was gone (410).
	SessionOpened EventType = "SessionOpened"
	SessionLost   EventType = "SessionLost"

	// M15, D-064: a turn ended on a question for the operator, and the
	// operator answered it. The payload's Question carries both halves.
	QuestionAsked    EventType = "QuestionAsked"
	QuestionAnswered EventType = "QuestionAnswered"
)

// judgment
const (
	ReviewCompleted EventType = "ReviewCompleted"
	RetryRequested  EventType = "RetryRequested"
	ModelEscalated  EventType = "ModelEscalated"
	SafetyVeto      EventType = "SafetyVeto"     // needs the real review engine -- M5.2
	GuardExhausted  EventType = "GuardExhausted" // needs the six guards -- M5.5
)

// gates
const (
	ApprovalRequested EventType = "ApprovalRequested"
	ApprovalGranted   EventType = "ApprovalGranted"
	ApprovalRejected  EventType = "ApprovalRejected"
)

// collaboration
const (
	MeetingStarted   EventType = "MeetingStarted"
	ProposalRecorded EventType = "ProposalRecorded"
	DecisionRecorded EventType = "DecisionRecorded"
	MeetingClosed    EventType = "MeetingClosed"
)

// memory
const (
	KnowledgeExtracted EventType = "KnowledgeExtracted"
	KnowledgeReused    EventType = "KnowledgeReused"    // needs recall at planning time -- M7.3
	DecisionSuperseded EventType = "DecisionSuperseded" // needs contradiction detection -- M7.3
)

// budget
//
// The three names D-037 adds to a closed catalog, plus TaskResumed above, which
// the decision also names and which already existed. BR-05 says a new event type
// is a schema change to the fold, so these land with their reducer cases in
// fold.go, the `deferred` task state in model.go and the DDL that permits it --
// together, or not at all. The guard that emits them is M10.3; the catalog entry
// comes first so nothing has to reopen a closed list later.
const (
	BudgetThresholdReached EventType = "BudgetThresholdReached" // needs the budget guard -- M10.3
	BudgetCapReached       EventType = "BudgetCapReached"       // needs the budget guard -- M10.3
	TaskDeferred           EventType = "TaskDeferred"           // needs the budget guard -- M10.3
)

// egress
//
// The two names L-5 requires: "Redactions and refusals are events
// (ContextRedacted, a refused dispatch with its reason) and appear in the
// timeline like any other state change." The reason they are declared here
// rather than at a call site is BR-05 -- a new event type is a schema change to
// the fold, so the constant, the model document's catalog table and the reducer
// case land in one change or in none.
//
// DispatchRefused, not EgressRefused, and the choice is D-052. The refusal is
// about the DISPATCH; why it did not happen travels in the payload as Rule.
// BR-35's budget cap refuses a dispatch for an entirely different reason and
// gets to reuse this name with a different Rule, rather than forcing a closed
// catalog open a second time -- which is the exact trap D-037 had to be written
// to get out of.
//
// Unlike the budget block below these are emitted today: MemStore.DispatchTask
// writes both, which is what makes T-127 true rather than declared.
const (
	ContextRedacted EventType = "ContextRedacted"
	DispatchRefused EventType = "DispatchRefused"
)

// Names the prototype emitted that are not in the model document's catalog.
// They are UI lifecycle, not graph state, and they are kept separate so nobody
// mistakes them for domain events when the ArcadeDB store lands.
const (
	FloorOpened       EventType = "FloorOpened"
	TaskCompleted     EventType = "TaskCompleted"
	TaskCanceled      EventType = "TaskCanceled"
	WorkflowCompleted EventType = "WorkflowCompleted"
)

// Event kinds drive the colour of a timeline pill and nothing else. They are
// presentation, deliberately separate from EventType, so that changing how an
// event looks never means renaming the event.
const (
	KindInfo = "info"
	KindDone = "done"
	KindWarn = "warn"
	KindFail = "fail"
	KindKnow = "know"
	KindMeet = "meet"
)
