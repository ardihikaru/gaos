package main

import "sort"

// The fold. This is T-011, T-097 and T-098, and it is the file the whole replay
// model rests on.
//
// artifacts/v1/gaos-architecture.html §08 states it exactly:
//
//	state(t) = fold(reduce, events.filter(e => e.ts <= t), snapshot_before(t) ?? empty)
//
//	reduce(state, event) must be:
//	  pure          same inputs, same output, no clock, no random
//	  total         an unknown event type is skipped, never fatal
//	  order-stable  ties broken by (ts, seq) so replays are identical
//
// All three are load-bearing and all three are tested.
//
//   - pure: nothing in this file reads the clock, draws a random number, takes a
//     lock or touches the network. Every value the reducer needs arrives in the
//     event. That is why EventPayload exists.
//   - total: the default case returns the state unchanged. A stream written by a
//     newer build, carrying a name this build has never heard of, folds to
//     something slightly stale rather than to an error. The alternative is an
//     audit trail that stops working the moment the catalog grows, which is the
//     opposite of what an audit trail is for.
//   - order-stable: fold sorts by (T, Seq) before it starts. Two events in the
//     same clock tick have distinct Seq, so the order is total and a replay of
//     the same stream is the same replay every time.
//
// The other half of §08 is why snapshots are not the source: "if the UI can only
// replay from stored snapshots, then any moment between two snapshots is
// unreachable and any bug in snapshot writing is invisible." So snapshots here
// are only ever a starting point handed to fold, never an answer -- and
// TestFoldNeedsNoSnapshotAtAll deletes every one of them and demands the same
// result (BR-21, T-100).

// FloorState is what the reducer folds to: the desks, the clock they were last
// touched at, and which desk is holding a meeting.
//
// Deliberately not in here: spend, notification count and the room occupancy
// meters. Those are live chrome, computed in MemStore.stats from the live floor,
// and the rail has always described the present even while the carousel shows
// the past -- a replayed floor with a replayed room meter would look like the
// present and be the past. Keeping them out of FloorState is what makes that
// choice explicit rather than accidental.
type FloorState struct {
	Desks      []Desk
	Clock      int
	MeetingIdx int
}

// emptyFloor is §08's `?? empty`: the state before anything happened. Genesis
// is reached by folding FloorOpened onto it, which is the one event that carries
// a floor rather than a delta.
func emptyFloor() FloorState {
	return FloorState{MeetingIdx: -1}
}

// Clone deep-copies a folded state. Callers get their own desks or they get a
// slice the next fold will write through.
func (f FloorState) Clone() FloorState {
	c := f
	c.Desks = cloneDesks(f.Desks)
	return c
}

// fold applies every event with Seq <= uptoSeq to base, in (T, Seq) order.
//
// base is a snapshot, or emptyFloor(). Passing a snapshot is an optimisation and
// must never change the answer: fold(snapshotAt(n), events, m) and
// fold(emptyFloor(), events, m) are required to agree, which is T-012 and the
// fold acceptance test the BRD has been waiting on since §4.1 was written.
//
// Pass allEvents for uptoSeq to fold the whole stream.
const allEvents = 1<<62 - 1

func fold(base FloorState, events []Event, uptoSeq int) FloorState {
	st := base.Clone()

	// Sort a copy. The caller's slice is the event stream, and the stream is
	// append-only in the strongest sense: nothing here may reorder it in place.
	ordered := make([]Event, 0, len(events))
	for _, e := range events {
		if e.Seq <= uptoSeq {
			ordered = append(ordered, e)
		}
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].T != ordered[j].T {
			return ordered[i].T < ordered[j].T
		}
		return ordered[i].Seq < ordered[j].Seq
	})

	for _, e := range ordered {
		st = reduce(st, e)
	}
	return st
}

// reduce is the reducer. One case per event type that changes the floor, and a
// default that changes nothing.
//
// Read the cases as the definition of what each event MEANS. The state a desk
// lands in is decided here, from the event type -- the payload only supplies
// values the reducer could not have known, like the score a review produced or
// the task title a planner picked. That split is the point: an emitter that
// writes the wrong payload produces a wrong value, but an emitter cannot invent
// a transition, because it does not get to choose one.
func reduce(st FloorState, e Event) FloorState {
	p := e.Payload

	// FloorOpened is genesis and is handled before the desk lookup, because it
	// is the event that creates the desks every other case looks up.
	if e.Type == FloorOpened {
		st.Desks = cloneDesks(p.Genesis)
		st.Clock = e.T
		st.MeetingIdx = -1
		return st
	}

	if e.T > st.Clock {
		st.Clock = e.T
	}

	idx := -1
	for i := range st.Desks {
		if st.Desks[i].ID == e.DeskID {
			idx = i
			break
		}
	}
	if idx < 0 {
		// An event about something that is not a desk -- every master-record
		// event is one, because ProductCreated names a product id and the floor
		// only ever showed the seeded six. Not an error, and not a floor change.
		return st
	}
	d := st.Desks[idx].Clone()

	switch e.Type {

	// ---- planning ----

	case WorkflowCreated:
		// A new objective on an existing desk: the wizard's CreateWorkflow, or
		// the simulator picking up the next goal after a completed workflow.
		d.State = StateActive
		d.WorkflowID = p.WorkflowID
		d.TaskSeq = p.TaskSeq
		d.StageIdx = p.StageIdx
		d.Progress = p.Progress
		d.Task = p.Task
		d.Agents = append([]string(nil), p.Agents...)
		d.Retry, d.Esc = p.Retry, p.Esc
		d.Cost = p.Cost
		d.Scored, d.Score, d.ScoreParts = false, 0, nil
		d.BlockReason = ""
		d.ApprovalRequired = false
		d.Meeting = nil
		if p.Goal != "" {
			d.Goal = p.Goal
		}

	case TaskPlanned:
		// Tasks live in the graph, not on the desk. Nothing on the floor moves,
		// and the payload's Tasks list is deliberately not read here.
		//
		// T-042 made that a real decision rather than a convenience. The task
		// graph is authoritative persisted state, and this reducer is not how it
		// is rebuilt -- ArcadeStore.load is, exactly as it is for the master
		// record, whose ProductCreated and RecordUpdated sit in the no-op list
		// further down for the same reason. BR-01 asks that memory be
		// reconstructible, not that it be reconstructible by folding; the test
		// that keeps this honest is TestTheTaskGraphSurvivesARestart.
		//
		// FloorState is also the wrong container for it. The comment at the top
		// of this file already names what stays out and why, and a graph in here
		// would additionally ride inside every snapshot rung, turning "six
		// desks" into "six desks plus every task ever planned".
		//
		// The consequence, worth stating because it will surprise somebody:
		// scrubbing the timeline shows the PRESENT graph beside a PAST floor.
		// That is the same trade this file already makes for the room meters and
		// the spend rail. It matters for T-109 -- the five blocking reasons the
		// inspector renders must come from the live scheduler, never from a
		// replayed floor.

	case WorkflowCanceled:
		d.State = StateIdle
		d.Agents = nil
		d.Progress = 0
		d.ApprovalRequired = false

	// ---- execution ----

	case TaskDispatched:
		d.State = StateActive
		d.StageIdx = p.StageIdx
		d.TaskSeq = p.TaskSeq
		d.Progress = p.Progress
		d.Task = p.Task
		d.Agents = append([]string(nil), p.Agents...)
		d.Retry = p.Retry
		d.Scored, d.Score, d.ScoreParts = false, 0, nil
		d.BlockReason = ""

	case TaskBlocked:
		d.State = StateBlocked
		d.BlockReason = e.Text
		d.Progress = p.Progress
		d.Cost = p.Cost

	case TaskResumed:
		d.State = StateActive
		d.BlockReason = ""

	case TaskDeferred:
		// D-037's throttled zone. BR-34: not a failure, so the retry and
		// escalation counters are pointedly not touched here.
		d.State = StateDeferred
		d.BlockReason = e.Text

	// ---- egress ----

	case ContextRedacted:
		// The slice was cut down and the dispatch went ahead anyway. Nothing
		// about the TASK changed, so nothing about the desk changes: this is a
		// fact about what was SENT, not about what is happening.
		//
		// It still reaches the operator, through the timeline pill and through
		// p.Logs at the bottom of this function, which is exactly what L-5 asks
		// for -- "an invisible filter is indistinguishable from a broken one".
		//
		// Its own case rather than a line in the no-op list below, because the
		// reason it moves nothing is specific to it and worth reading here.

	case DispatchRefused:
		// BR-32: the dispatch did not happen, so the desk must not look like it
		// did. Blocked is what this is -- not running, and waiting on something
		// a human has to clear -- and it is the state stats() counts into
		// Attention, so the refusal reaches the bell with no extra wiring.
		//
		// Pointedly NOT deferred: BR-34 says a deferral is not a failure and
		// consumes no retry, while an egress refusal is a failure that needs a
		// person to change a policy or a slice. Pointedly not idle either: an
		// idle desk carrying a reason is a contradiction the inspector renders
		// as an empty desk, which is the invisible-filter failure again.
		//
		// This is why state.go now permits idle -> blocked. See the comment on
		// the transition table: until BR-32 existed, blocked could only be
		// reached from work that was already running.
		d.State = StateBlocked
		d.BlockReason = e.Text
		d.Progress = 0
		d.Agents = nil

	case TaskCompleted:
		d.State = StateReview
		d.Progress = 100
		d.Cost = p.Cost

	case TaskCanceled:
		d.State = StateIdle
		d.Agents = nil
		d.Progress = 0
		d.ApprovalRequired = false

	// ---- judgment ----

	case ReviewCompleted:
		d.Scored = true
		d.Score = p.Score
		d.ScoreParts = append([]ScorePart(nil), p.Parts...)
		d.Cost = p.Cost

	case RetryRequested:
		d.State = StateActive
		d.Retry = p.Retry
		d.Progress = p.Progress
		d.Task = p.Task
		d.Scored, d.Score, d.ScoreParts = false, 0, nil

	case ModelEscalated:
		d.State = StateActive
		d.Esc = p.Esc
		d.Retry = p.Retry
		d.Progress = p.Progress
		d.Task = p.Task
		d.Scored, d.Score, d.ScoreParts = false, 0, nil
		d.Meeting = nil

	// ---- gates ----

	case ApprovalRequested:
		d.ApprovalRequired = true

	case ApprovalGranted:
		d.ApprovalRequired = false

	case ApprovalRejected:
		// The operator refused the gate. The gate comes off because it has been
		// answered, and the desk goes idle because the answer was no.
		d.ApprovalRequired = false
		d.State = StateIdle
		d.Agents = nil
		d.Progress = 0

	// ---- collaboration ----

	case MeetingStarted:
		d.State = StateMeeting
		if p.Meeting != nil {
			m := *p.Meeting
			m.Participants = append([]string(nil), p.Meeting.Participants...)
			m.Log = append([]MeetingLine(nil), p.Meeting.Log...)
			d.Meeting = &m
		}
		st.MeetingIdx = idx

	case ProposalRecorded, DecisionRecorded:
		if p.Meeting != nil {
			m := *p.Meeting
			m.Participants = append([]string(nil), p.Meeting.Participants...)
			m.Log = append([]MeetingLine(nil), p.Meeting.Log...)
			d.Meeting = &m
		}

	case MeetingClosed:
		d.Meeting = nil
		if st.MeetingIdx == idx {
			st.MeetingIdx = -1
		}

	// ---- lifecycle ----

	case WorkflowCompleted:
		d.State = StateCompleted
		d.Progress = 100
		d.TaskSeq = p.TaskSeq
		d.Cost = p.Cost

	case KnowledgeExtracted, KnowledgeInjected, KnowledgeReused,
		ProductCreated, RecordUpdated, ReadinessEvaluate, ReadinessDropped,
		DecisionSuperseded, DependencyRewired,
		BudgetThresholdReached, BudgetCapReached:
		// Recorded on the timeline, folded into the master record or the tenant
		// rather than into a desk. Named explicitly rather than left to the
		// default so that "this event does not move the floor" is a decision
		// somebody made, not an omission.

	default:
		// Total. An unknown name -- a stream from a newer build, or a type this
		// build has not learned to fold yet -- leaves the state alone. Note the
		// desk clone above is simply discarded in this branch.
		return st
	}

	// Log lines ride with the event that follows them, so a replayed desk shows
	// the log it had at that moment. Same 40-line trim as MemStore.log, applied
	// here too because the reducer has to agree with the writer about what the
	// desk contains.
	if len(p.Logs) > 0 {
		d.Logs = append(d.Logs, p.Logs...)
		if len(d.Logs) > maxDeskLogs {
			d.Logs = d.Logs[len(d.Logs)-maxDeskLogs:]
		}
	}

	st.Desks[idx] = d
	return st
}
