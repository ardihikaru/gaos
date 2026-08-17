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
// "needs the runner" or "needs the review engine" are declared and not emitted:
// they belong to M4 and M5, neither of which exists. They are declared anyway so
// that the catalog in code is the catalog in the document, and so a reviewer can
// see at a glance what is still missing. Emitting one of them from sim.go would
// be inventing evidence -- a Heartbeat nothing sent, a RunExited from a process
// that never started -- and the audit trail is the last place to do that.
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
)

// planning
const (
	WorkflowCreated   EventType = "WorkflowCreated"
	TaskPlanned       EventType = "TaskPlanned"
	DependencyRewired EventType = "DependencyRewired" // needs the planner's task graph -- M3.2
	WorkflowCanceled  EventType = "WorkflowCanceled"
)

// execution
const (
	TaskDispatched EventType = "TaskDispatched"
	RunStarted     EventType = "RunStarted" // needs the runner -- M4.2
	Heartbeat      EventType = "Heartbeat"  // needs the runner -- M4.2
	RunExited      EventType = "RunExited"  // needs the runner -- M4.2
	TaskBlocked    EventType = "TaskBlocked"
	TaskResumed    EventType = "TaskResumed"
	TaskOrphaned   EventType = "TaskOrphaned" // needs the lease -- M4.2
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
