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
// "pass 2" are not emitted yet -- they belong to the runner, the scheduler and
// the real review engine, none of which exist in this pass. They are declared
// anyway so that the catalog in code is the catalog in the document, and so a
// reviewer can see at a glance what is still missing.

type EventType string

// master
const (
	ProductCreated    EventType = "ProductCreated"
	RecordUpdated     EventType = "RecordUpdated"
	KnowledgeInjected EventType = "KnowledgeInjected"
	ReadinessEvaluate EventType = "ReadinessEvaluated" // pass 2
	ReadinessDropped  EventType = "ReadinessDropped"   // pass 2
)

// planning
const (
	WorkflowCreated   EventType = "WorkflowCreated"
	TaskPlanned       EventType = "TaskPlanned"
	DependencyRewired EventType = "DependencyRewired" // pass 2
	WorkflowCanceled  EventType = "WorkflowCanceled"  // pass 2
)

// execution
const (
	TaskDispatched EventType = "TaskDispatched"
	RunStarted     EventType = "RunStarted" // pass 2
	Heartbeat      EventType = "Heartbeat"  // pass 2
	RunExited      EventType = "RunExited"  // pass 2
	TaskBlocked    EventType = "TaskBlocked"
	TaskResumed    EventType = "TaskResumed"
	TaskOrphaned   EventType = "TaskOrphaned" // pass 2
)

// judgment
const (
	ReviewCompleted EventType = "ReviewCompleted"
	RetryRequested  EventType = "RetryRequested"
	ModelEscalated  EventType = "ModelEscalated"
	SafetyVeto      EventType = "SafetyVeto"     // pass 2
	GuardExhausted  EventType = "GuardExhausted" // pass 2
)

// gates
const (
	ApprovalRequested EventType = "ApprovalRequested"
	ApprovalGranted   EventType = "ApprovalGranted"
	ApprovalRejected  EventType = "ApprovalRejected" // pass 2
)

// collaboration
const (
	MeetingStarted   EventType = "MeetingStarted"
	ProposalRecorded EventType = "ProposalRecorded" // pass 2
	DecisionRecorded EventType = "DecisionRecorded"
	MeetingClosed    EventType = "MeetingClosed"
)

// memory
const (
	KnowledgeExtracted EventType = "KnowledgeExtracted"
	KnowledgeReused    EventType = "KnowledgeReused"    // pass 2
	DecisionSuperseded EventType = "DecisionSuperseded" // pass 2
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
