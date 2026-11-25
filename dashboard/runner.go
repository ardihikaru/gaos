package main

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// The runner. T-050 and T-052, re-based on the gateway in M14.7 (D-060).
//
// One supervising goroutine per attempt. It records the TaskRun with its
// request id, submits the turn through the adapter, beats while it polls, and
// records what came back. Nothing runs on this host: the work happens in the
// product's workspace on the worker, and what survives an orchestrator restart
// is the request id, which recovery polls (turns.go).
//
// # What the heartbeat actually attests to
//
// The supervisor, NOT the model. When an orchestrator dies the beat goes stale
// while the turn continues upstream -- which is why recovery asks the gateway
// rather than reading the beat, and why a request the gateway no longer knows
// holds its product until the turn must have ended (D-060).
//
// # Two cadences, not one
//
// The beat is a field write every heartbeatInterval. The Heartbeat EVENT is
// written every heartbeatEventEvery, plus always the first beat of a run and
// always the last before it exits. D-053 has the argument.

// The runner switch, GAOS_RUNNER, deliberately separate from whether an
// adapter is configured.
//
// An operator who sets GAOS_MODEL_MODE=aimanager to make the boot smoke test run has
// not thereby authorised spending money on model calls. Two different
// decisions, two different variables, and the default on this one is off.
//
// Three modes (D-080, BR-56):
//
//   - off: nothing is ever sent, and the scheduler loop does not run.
//   - on: the scheduler dispatches everything it judges ready, unasked.
//   - manual: the scheduler moves work to READY and no further; a turn is sent
//     only when an operator moves a ready card to working on the board.
//
// Anything unrecognised is off: a typo must fail closed, never open.
const (
	RunnerOff    = "off"
	RunnerOn     = "on"
	RunnerManual = "manual"
)

func runnerMode() string {
	switch strings.ToLower(strings.TrimSpace(envOr("GAOS_RUNNER", RunnerOff))) {
	case "on", "1", "true", "yes":
		return RunnerOn
	case "manual":
		return RunnerManual
	}
	return RunnerOff
}

// runnerMaySpend says whether an operator's board dispatch may start a turn.
// The scheduler's own permission is runnerMode() == RunnerOn, and only that.
func runnerMaySpend() bool {
	m := runnerMode()
	return m == RunnerOn || m == RunnerManual
}

// The heartbeat's two cadences (D-053): the supervisor beats every
// heartbeatInterval seconds while it polls a turn, and writes a Heartbeat
// event only every heartbeatEventEvery -- a run must not put one pill per beat
// into a stream that is append-only and rendered in full.
const (
	heartbeatInterval   = 30
	heartbeatEventEvery = 300
)

// beatEmitsEvent is the pure half of the heartbeat: given when the last event
// was written, say whether this beat writes another.
//
// Pure and separate so the cadence can be asserted against a fake clock rather
// than by waiting five minutes. The first and last beats of a run always emit,
// because "alive at T" and "silent since T" are the only two questions a replay
// asks of a heartbeat, and both need an endpoint.
func beatEmitsEvent(lastEventAt, now int64, first, final bool) bool {
	if first || final {
		return true
	}
	return now-lastEventAt >= heartbeatEventEvery
}

// runIDFor names a run. Derived rather than random: the scheduler is a pure
// function and the reconciler compares this against the handoff document's
// `run` field, so a name that needed a random source would be a name two
// replicas could not both produce.
func runIDFor(taskID string, attempt int) string {
	return fmt.Sprintf("%s-a%d", taskID, attempt)
}

// superviseRun is the whole of one run's life. Blocking: the caller decides
// whether it wants a goroutine.
//
// Every failure before the spawn returns without emitting RunStarted. That is
// deliberate and events.go is explicit about why -- emitting a RunStarted for a
// process that never started, or a Heartbeat nobody sent, would put invented
// evidence in the one record the whole audit story rests on.
func (s *MemStore) superviseRun(ctx context.Context, ad Adapter, deskID string, c Cleared, attempt int) (Attempt, error) {
	// Every attempt is a turn (D-060, M14.7). There is no folder on this host
	// to lease, no run record to write and no packet to put on disk: the
	// workspace that matters is the product's, on the worker, one live turn per
	// product is the graph lease the scheduler reads (BR-38), and what survives
	// a restart is the request id recorded below.
	ta, isTurn := ad.(TurnAdapter)
	if !isTurn {
		return Attempt{}, fmt.Errorf("supervise: %s does not run turns; the CLI path was retired in M14.7", ad.Name())
	}
	taskID := c.Packet().TaskID
	if taskID == "" {
		return Attempt{}, fmt.Errorf("supervise: the cleared packet names no task")
	}
	runID := runIDFor(taskID, attempt)
	now := time.Now().Unix()

	// Recorded BEFORE the turn is sent: it is the only thing a restarted
	// orchestrator can recover the turn by (D-060).
	requestID := ta.RequestIDFor(runID)

	// The graph's record of this attempt, and the slice it is about to be given.
	// T-054 and T-055.
	//
	// Both before the submit, so a run that dies after it has left a record
	// recovery can find the turn by. The slice is recorded rather than
	// re-derived because by the time
	// anyone asks what attempt two was given, the record it was assembled from
	// will have moved on.
	s.mu.Lock()
	slice := s.recordSlice(c, taskID, deskID, attempt)
	pinned, _ := s.blueprintGate(deskID) // BR-39: the version this attempt runs under
	s.startRun(TaskRunRecord{
		ID: runID, TaskID: taskID, DeskID: deskID, Attempt: attempt,
		Status: RunStatusStarted, Model: ad.Name(), Invocation: ad.Mode(), Provider: runProvider(ad, c.Packet()),
		StartedAt: now, SliceDigest: slice.Digest, BlueprintVersion: pinned, RequestID: requestID,
		Workspace: c.Packet().WorkspaceID,
		// No CostBasis yet, and deliberately not "estimated". A run that has
		// spent nothing has no basis to record; BR-31 is about never aggregating
		// a measured number with a guessed one, and a default here would seed
		// every abandoned attempt with a guess.
	})
	s.mu.Unlock()

	// DISPATCHED -> RUNNING. The distinction exists for exactly one reader: boot
	// reconciliation, which D-4 scopes to "every task claiming to be dispatched
	// or running". A task that had been decided on but never reached a process
	// and one that has a live child are different recoveries, and collapsing
	// them would mean either killing work that was fine or requeuing work that
	// never started.
	s.moveTaskByID(taskID, StatusRunning, MoveStart)
	s.mu.Lock()
	s.markRun(runID, RunStatusRunning)
	s.mu.Unlock()

	s.emitLocked(RunStarted, deskID,
		fmt.Sprintf("run %s started, attempt %d · turn %s", runID, attempt, requestID),
		KindInfo, EventPayload{Digest: c.Digest(), Bytes: c.Bytes()})

	// The beat. It writes the field every heartbeatInterval and an event
	// far less often -- D-053.
	done := make(chan struct{})
	go s.beat(deskID, runID, now, done)

	att, err := ad.Dispatch(ctx, c)
	close(done)
	if err == nil {
		s.sessionEvents(deskID, c, att)
	}

	if err != nil {
		s.finishRun(deskID, taskID, runID, Attempt{Digest: c.Digest()}, err)
		return Attempt{}, err
	}

	// The last beat before the exit always writes, so a replay has an endpoint
	// for "silent since T" rather than having to infer one from the RunExited.
	s.beatOnce(deskID, runID, time.Now().Unix(), 0, false, true)

	s.finishRun(deskID, taskID, runID, att, nil)
	s.checkAfterTurn(ctx, deskID, runID, att)
	return att, nil
}

// finishRun applies what the run changed and emits RunExited, under one lock.
//
// One lock and one function because the two must not be able to disagree. The
// event carries the desk's running total (D-048: absolute, not a delta, so a
// fold that replays it twice is idempotent) and the desk carries the same
// number -- a reducer setting a value the live path never set would make
// TestFoldFromGenesisMatchesTheLiveFloor describe a floor that never existed.
//
// What it does NOT do is decide whether the work succeeded. RunExited says a
// process ended and what it cost; whether the output is any good is the review
// engine's, M5. D-050 forbids reading that from an exit, and this is the place
// it would be tempting to.
func (s *MemStore) finishRun(deskID, taskID, runID string, att Attempt, runErr error) {
	s.mu.Lock()

	// The attempt's own record, closed under the same lock as the event that
	// describes it. Two writers could otherwise leave a TaskRun saying one thing
	// and a RunExited saying another, and there would be no way to tell which of
	// them the auditor should believe.
	s.closeRun(runID, att, runErr)

	// What it handed over, stored where the next agent can be cited it. T-064.
	//
	// Under the same lock for the same reason, and keyed by the run so two
	// attempts leave two documents. The workspace copy is not the record: the
	// lease is released moments from now and the next attempt truncates the file
	// before it starts.
	// The turn's transcript, per attempt (BR-41): the session that carried it
	// expires, and what was asked and answered is the record, not transport.
	for kind, body := range map[string]string{ArtifactPrompt: att.Prompt, ArtifactReply: att.Reply} {
		if body != "" {
			s.recordHandoff(ArtifactRecord{
				ID: runID + "-" + kind, Kind: kind, Title: kind + " of " + runID,
				TaskID: taskID, DeskID: deskID, RunID: runID,
				Attempt: attemptOfRun(s.runs, runID), Body: body, CreatedAt: time.Now().Unix(),
			})
		}
	}
	if att.HandoffRaw != "" {
		s.recordHandoff(ArtifactRecord{
			ID: runID + "-handoff", Kind: ArtifactHandoff,
			Title:  fmt.Sprintf("handoff from %s", runID),
			TaskID: taskID, DeskID: deskID, RunID: runID,
			Attempt: attemptOfRun(s.runs, runID), Body: att.HandoffRaw,
			CreatedAt: time.Now().Unix(),
		})
	}

	// M15: a turn that ended on a question. Its record is the question
	// artifact and the QuestionAsked event below; the task waits on the
	// operator in AWAITING_INPUT.
	var asked *QuestionPayload
	if runErr == nil && att.Outcome() == OutcomeAsked {
		asked = questionFromReply(att.Question, runID, attemptOfRun(s.runs, runID))
		s.recordHandoff(ArtifactRecord{
			ID: runID + "-question", Kind: ArtifactQuestion, Title: "question from " + runID,
			TaskID: taskID, DeskID: deskID, RunID: runID,
			Attempt: asked.Attempt, Body: questionBody(*asked), CreatedAt: time.Now().Unix(),
		})
	}

	p := EventPayload{Digest: att.Digest}
	text := fmt.Sprintf("run %s exited %s, outcome %s", runID, att.ExitClass, att.Outcome())
	kind := exitKind(att)

	switch {
	case runErr != nil:
		p.Reason = runErr.Error()
		text = fmt.Sprintf("run %s could not start: %v", runID, runErr)
		kind = KindFail
	case att.Failure != "":
		// BR-30: no handoff document is a failed attempt with a signature, and
		// the desk must not be left looking like nothing happened.
		p.Reason = att.Failure
		kind = KindFail
	}

	// Where the task goes next, and pointedly not "done".
	//
	// A run that ended is a run that produced something to judge, not a verdict.
	// D-050 forbids reading the outcome from the exit, and BR-13 scores every
	// attempt, so the furthest this may move a task is REVIEW -- M5 decides the
	// rest. The two exceptions are failures of the invocation rather than of the
	// work: no handoff at all (BR-30), and a run that never started.
	to := StatusReview
	rule := MoveExit
	switch {
	case runErr != nil, att.Failure != "":
		to = StatusFailed
	case asked != nil:
		to = StatusAwaitingInput
	case att.Outcome() == HandoffBlocked:
		to = StatusBlocked
	}
	// M17: a DESIGN or a REVIEW task's terminal status is the gate's verdict,
	// not the runner's default. The gate is the only thing in the product that
	// writes DONE for a task that no human approved (D-069 clause 6).
	//
	// The gate's move comes AFTER the runner's, and that order is the state
	// machine's: there is no RUNNING -> DONE edge, so the task goes to REVIEW
	// first and the gate moves it on from there. Two moves, two events, and the
	// timeline says what happened.
	var gate *gateOutcome
	if g, ok := s.gateVerdict(taskID, runID, att); ok {
		gate = &g
		// A review the parser refused says why on the run's own line, so the
		// operator who retries it knows what the reviewer must change.
		if g.detail != "" {
			text += "; the review was refused: " + g.detail
		}
		if g.note != "" {
			text += "; " + g.note
		}
	}
	var out []Event
	if m, ok := s.moveTaskLocked(taskID, to, rule); ok {
		out = append(out, m)
	}
	gateTo := ""
	if gate != nil {
		gateTo = gate.to
		if gate.reason != "" {
			if t := s.taskByIDLocked(taskID); t != nil {
				t.BlockReason = gate.reason
			}
		}
		if m, ok := s.moveTaskLocked(taskID, gate.to, gate.rule); ok {
			out = append(out, m)
		}
	}
	// M17: the gate releases the implementation task when the review clears,
	// and rewrites its reason when the review blocks.
	out = append(out, s.gateReleaseLocked(taskID, gateTo)...)
	if asked != nil {
		out = append(out, s.emit(QuestionAsked, deskID, fmt.Sprintf("%s asks: %s", taskID, asked.Question),
			KindWarn, EventPayload{Task: taskID, Question: asked}))
	}

	if i := s.deskIdxByID(deskID); i >= 0 {
		d := &s.desks[i]
		if att.CostUSD > 0 {
			d.Cost += att.CostUSD
			s.spend += att.CostUSD
		}
		p.Cost = d.Cost
		if p.Reason != "" && d.State != StateBlocked {
			// setState rather than an assignment: idle -> blocked and
			// active -> blocked are both legal, and a run that ended badly on a
			// desk in some third state is a bug worth hearing about rather than
			// forcing through.
			if err := s.setState(d, StateBlocked); err == nil {
				d.BlockReason = p.Reason
			}
		}
	}
	out = append(out, s.emit(RunExited, deskID, text, kind, p))
	var hookTask *TaskRecord
	if s.afterRun != nil {
		if t := s.taskByIDLocked(taskID); t != nil {
			c := t.Clone()
			hookTask = &c
		}
	}
	after := s.afterRun
	s.mu.Unlock()

	// Outside the lock on purpose: publish is a database round trip, and one
	// slow write must not hold the floor closed against every reader.
	s.publish(out...)
	// The pull-request flow reads the reply once the run is recorded (D-088).
	if hookTask != nil && after != nil {
		go after(*hookTask, att.Reply)
	}
}

// beat runs until the run is over. Its own goroutine because the adapter call
// blocks for as long as the work takes.
func (s *MemStore) beat(deskID, runID string, startedAt int64, done <-chan struct{}) {
	tick := time.NewTicker(heartbeatInterval * time.Second)
	defer tick.Stop()

	lastEvent := startedAt
	first := true
	for {
		select {
		case <-done:
			return
		case <-tick.C:
			now := time.Now().Unix()
			if s.beatOnce(deskID, runID, now, lastEvent, first, false) {
				lastEvent = now
			}
			first = false
		}
	}
}

// beatOnce writes the beat and, when the cadence says so, the event. Returns
// whether it wrote an event.
//
// Nothing here can fail the run: a beat is evidence that the supervisor is
// alive, never a condition the turn depends on -- a
// supervisor that tore its own work down on a failed renew would race the very
// process that is trying to arbitrate.
func (s *MemStore) beatOnce(deskID, runID string, now, lastEvent int64, first, final bool) bool {
	if !beatEmitsEvent(lastEvent, now, first, final) {
		return false
	}
	s.emitLocked(Heartbeat, deskID, "run "+runID+" is alive", KindInfo, EventPayload{})
	return true
}

// emitLocked takes the store lock and writes one event. Every other emit() call
// in the package is made by something that already holds the lock; the runner
// is the first writer that does not, because it spends most of its life
// blocked on a subprocess and holding the floor's lock for twenty minutes would
// stop every reader on it.
func (s *MemStore) emitLocked(t EventType, deskID, text, kind string, p EventPayload) {
	s.mu.Lock()
	e := s.emit(t, deskID, text, kind, p)
	s.mu.Unlock()
	s.publish(e)
}

// moveTaskByID takes the lock and moves one task. moveTaskLocked assumes it is
// already held, same split as every other pair in the store.
//
// A task id that names nothing is NOT an error. The operator-triggered dispatch
// route freezes a packet with a display id (see freezePacketFor) and has no
// planned task behind it at all, so "no record to move" is the ordinary case
// rather than a failure -- and turning it into one would mean the runner
// refused to supervise anything an operator asked for by hand.
//
// A refused TRANSITION is different, and is dropped on the floor here on
// purpose: moveTask already returns the reason, the caller is a supervisor with
// nowhere to put it, and the state machine's job is to refuse the write rather
// than to unwind the run around it.
func (s *MemStore) moveTaskByID(id, to, rule string) {
	s.mu.Lock()
	e, ok := s.moveTaskLocked(id, to, rule)
	s.mu.Unlock()
	if ok {
		s.publish(e)
	}
}

// moveTaskLocked moves one task and writes the TaskMoved that says so (T-164).
//
// Every status change is an event naming the TASK, whoever made it -- the
// board, the scheduler, the runner, recovery, a drift release. Rule names that
// actor and To carries the status it reached, so taskfold.go can rebuild every
// card from the stream without reading a caption. A move to the status the task
// already has changes nothing and writes nothing. The caller publishes the
// returned event once it has let go of the lock.
func (s *MemStore) moveTaskLocked(id, to, rule string) (Event, bool) {
	return s.moveTaskWithLocked(id, to, rule, EventPayload{})
}

// moveTaskWithLocked is moveTaskLocked with the send-back's extra payload: the
// account that made the move and the operator's triage (D-084). The caption
// names the account only when there is one, so every other move's caption is
// unchanged byte for byte.
//
// It is also where the one exit from DONE is kept narrow: a move out of DONE
// is refused unless it is a design task sent back. moveTask knows the task;
// only this function knows the rule.
func (s *MemStore) moveTaskWithLocked(id, to, rule string, extra EventPayload) (Event, bool) {
	for i := range s.tasks {
		t := &s.tasks[i]
		if t.ID != id {
			continue
		}
		from := t.Status
		if from == StatusDone && (rule != MoveSendBack || taskKind(t.Kind) != TaskKindDesign) {
			return Event{}, false
		}
		if from == to || moveTask(t, to) != nil {
			return Event{}, false
		}
		text := fmt.Sprintf("%s: %s (%s → %s)", id, rule, from, to)
		if extra.Actor != "" {
			text += " by " + extra.Actor
		}
		p := EventPayload{Task: id, Rule: rule, Reason: from + "→" + to, To: to, Actor: extra.Actor}
		if len(extra.Triage) > 0 {
			p.Triage = append([]FindingTriage(nil), extra.Triage...)
		}
		return s.emit(TaskMoved, t.DeskID, text, KindInfo, p), true
	}
	return Event{}, false
}

// schedulerTick is how often the control loop asks. Seconds rather than
// milliseconds: a pass reads the graph, stats every leased workspace and starts
// processes, and nothing on this floor becomes ready faster than a run finishes.
const schedulerTick = 2 * time.Second

// RunOnce is one pass of the control loop: ask the scheduler what may start,
// and start it.
//
// The clock is read HERE and nowhere below. Schedule is a pure function of what
// it is handed (D-1), and a second read further down would let two tasks in one
// decision be judged against two different nows.
//
// Everything it starts runs in its own goroutine, because superviseRun blocks
// for as long as the work takes and a loop that waited would dispatch one task
// per twenty minutes.
func (s *MemStore) RunOnce(ctx context.Context, ad Adapter) ScheduleDecision {
	now := time.Now().Unix()
	mode := runnerMode()
	if ad != nil && mode == RunnerManual {
		// D-080: the scheduler's verdict is recorded as READY and acted on by
		// nobody but the operator. READY tasks are left out of the input so a
		// card parked in ready holds neither its product's claim nor its role's
		// seat in this pass -- otherwise the first ready card would keep every
		// other card of its product, and of other products sharing a one-agent
		// role, in backlog for as long as the operator left it there.
		in := s.schedulerInput(now)
		in.Tasks = withoutReady(in.Tasks)
		decision := Schedule(in)
		s.readyPending(decision)
		return decision
	}
	decision := Schedule(s.schedulerInput(now))
	if ad == nil || mode != RunnerOn {
		// The decision still stands and is still worth having -- T-109 renders
		// the blocked half whether or not anything may run. What does not happen
		// is the spending.
		return decision
	}
	for _, d := range decision.Ready {
		s.mu.RLock()
		attempt := s.nextAttemptLocked(d.TaskID)
		s.mu.RUnlock()
		// BR-39 before the egress pass: a product whose workspace does not hold
		// its published Blueprint is refused as drift, with the same event an
		// egress refusal writes, and stays blocked until a sync releases it.
		s.mu.RLock()
		_, drift := s.blueprintGate(d.DeskID)
		s.mu.RUnlock()
		if drift != nil {
			s.refuseDispatch(d, *drift)
			continue
		}
		c, v, err := s.clearFor(ctx, d, attempt)
		if err != nil {
			// A refusal on this path used to be a `continue`, and that was a bug
			// found by running it: the scheduler would decline to dispatch, over
			// and over, every two seconds, leaving nothing in the timeline and
			// nothing on the desk. L-5 does not have a scheduler exemption -- "a
			// refused dispatch with its reason appears in the timeline like any
			// other state change" -- and a refusal only the logs know about is
			// indistinguishable from a scheduler that has stopped working.
			//
			// The task goes BLOCKED rather than back to PENDING. An egress refusal
			// is a policy problem an operator has to fix; retrying it on the next
			// tick would spin forever against a packet that cannot change on its
			// own.
			s.refuseDispatch(d, v)
			continue
		}
		if c.empty() {
			// The desk went away between the scheduler's decision and this. Not
			// a refusal and not worth an event: nothing was screened, so there is
			// no state change to describe.
			continue
		}
		// PENDING -> READY -> DISPATCHED, both legal and both meaningful. READY
		// is the scheduler's verdict; DISPATCHED is this loop acting on it, and
		// the gap between them is what boot reconciliation reads.
		s.moveTaskByID(d.TaskID, StatusReady, MoveSchedule)
		s.moveTaskByID(d.TaskID, StatusDispatched, MoveSchedule)

		dispatch, task := d, d.TaskID
		go func() {
			if _, err := s.superviseRun(context.WithoutCancel(ctx), ad, dispatch.DeskID, c, attempt); err != nil {
				// The run never started. Put the task back rather than leaving
				// it DISPATCHED forever, which would make the next boot
				// reconcile a run that does not exist.
				s.moveTaskByID(task, StatusReady, MoveNotStarted)
			}
		}()
	}
	return decision
}

// withoutReady drops READY tasks from a scheduler input (manual mode). A task
// that depends on one reads it as not in the graph and stays blocked, which is
// right: READY is not DONE. The lease reads DISPATCHED and RUNNING only, so it
// is untouched.
func withoutReady(tasks []TaskNode) []TaskNode {
	var out []TaskNode
	for _, t := range tasks {
		if t.Status != StatusReady {
			out = append(out, t)
		}
	}
	return out
}

// readyPending moves each scheduled task PENDING -> READY, and only from
// PENDING, with the status re-read under the write lock. The decision was
// computed from a snapshot, and DISPATCHED -> READY is a legal transition: an
// unconditional move would pull a card the operator dispatched in between back
// to READY while its turn is live, releasing the BR-38 lease.
//
// The moves of one pass are published as ONE write, and a pass that moved
// nothing publishes nothing. Under the ArcadeDB store every publish is a
// transaction held under the store lock, and one per card -- one per product,
// every pass -- multiplied the time a board move waited behind the scheduler.
func (s *MemStore) readyPending(d ScheduleDecision) {
	var moved []Event
	for _, x := range d.Ready {
		s.mu.Lock()
		if t := s.taskPtr(x.TaskID); t != nil && t.Status == StatusPending {
			if e, ok := s.moveTaskLocked(x.TaskID, StatusReady, MoveSchedule); ok {
				moved = append(moved, e)
			}
		}
		s.mu.Unlock()
	}
	s.publish(moved...)
}

// clearFor freezes and screens the packet for one scheduled task. BR-32: the
// only way to a provider is through Screen, and the only thing that returns a
// Cleared is Screen.
// The verdict comes back as well as the packet, and the error separately from
// the boolean, because the two failures are different facts: a desk that has
// gone away is nothing to report, and a packet the egress pass refused is a
// state change L-5 requires on the timeline.
func (s *MemStore) clearFor(ctx context.Context, d Dispatch, attempt int) (Cleared, EgressVerdict, error) {
	s.mu.RLock()
	idx := s.deskIdxByID(d.DeskID)
	if idx < 0 {
		s.mu.RUnlock()
		return Cleared{}, EgressVerdict{}, nil
	}
	pkt := s.freezePacketFor(s.desks[idx], d.TaskID, attempt)
	pkt.Vendor, pkt.Model = s.vendorForLocked(d.DeskID, pkt.Role)
	if ta, ok := s.adapter.(TurnAdapter); ok {
		pkt.SessionID, pkt.SessionReason = s.sessionForLocked(d.TaskID, ta, pkt.Vendor, pkt.Model, time.Now().Unix())
	}
	pol, tgt := s.egress, s.target
	s.mu.RUnlock()

	c, v, err := Screen(pol, tgt, pkt)
	if err != nil {
		return Cleared{}, v, err
	}
	if c.empty() {
		return Cleared{}, v, nil
	}
	// A review runs in the product's reviewer workspace, and the folder is
	// reset before the turn: it holds the standing files and nothing else
	// (M17.5, D-071). A refusal here is a refusal before anything is spent.
	if pkt.Kind == TaskKindReview {
		if verdict := s.prepareReviewWorkspace(ctx, d.DeskID); verdict != nil {
			return Cleared{}, *verdict, fmt.Errorf("%s", verdict.Reason)
		}
	}
	return c, v, nil
}

// refuseDispatch records a scheduled dispatch the egress pass refused.
//
// The same two events MemStore.DispatchTask writes on the operator's route, for
// the reason BR-33 gives about adapters and L-5 gives about refusals: which
// route reached the pass is not something the timeline should be able to tell.
func (s *MemStore) refuseDispatch(d Dispatch, v EgressVerdict) {
	s.mu.Lock()
	var ev []Event
	if i := s.deskIdxByID(d.DeskID); i >= 0 {
		dk := &s.desks[i]
		if len(v.Redactions) > 0 {
			ev = append(ev, s.emit(ContextRedacted, dk.ID,
				fmt.Sprintf("Context slice cut by %d part(s) before dispatch", len(v.Redactions)),
				KindWarn, EventPayload{
					Redactions: len(v.Redactions), Bytes: v.Bytes, Digest: v.Digest,
				}))
		}
		if err := s.setState(dk, StateBlocked); err == nil {
			dk.BlockReason = v.Reason
		}
		s.log(dk, "egress: dispatch refused — "+v.Reason, "r")
		ev = append(ev, s.emit(DispatchRefused, dk.ID, v.Reason, KindFail, EventPayload{
			Reason: v.Reason, Rule: v.Rule, Bytes: v.Bytes, Digest: v.Digest,
			Redactions: len(v.Redactions),
		}))
	}
	s.mu.Unlock()

	s.moveTaskByID(d.TaskID, StatusBlocked, MoveRefused)
	s.publish(ev...)
}

// ---------------------------------------------------------------- recovery

// Reconcile is boot recovery (D-060). Every gateway run still STARTED or
// RUNNING is polled by the request id it recorded before it was sent, and
// finished into the same attempt a supervised run gets (turns.go). A task
// that claims to be running with no run behind it -- it never reached a
// turn -- is put back to READY, because there is nothing anywhere that could
// still be doing its work.
func (s *MemStore) Reconcile(ctx context.Context) (Result, error) {
	s.mu.RLock()
	ad := s.adapter
	s.mu.RUnlock()
	var ev []Event
	taken := map[string]bool{}
	if ta, ok := ad.(TurnAdapter); ok {
		taken, ev = s.recoverTurns(ctx, ad, ta)
	}
	s.mu.Lock()
	for i := range s.tasks {
		t := &s.tasks[i]
		if taken[t.ID] || (t.Status != StatusDispatched && t.Status != StatusRunning) || s.hasOpenTurnLocked(t.ID) {
			continue
		}
		if m, ok := s.moveTaskLocked(t.ID, StatusReady, RuleRequeued); ok {
			ev = append(ev, m, s.emit(TaskResumed, t.DeskID, t.ID+" requeued: it claimed to be running and no turn was ever recorded",
				KindWarn, EventPayload{Rule: RuleRequeued}))
			if j := s.deskIdxByID(t.DeskID); j >= 0 {
				_ = s.setState(&s.desks[j], StateIdle)
			}
		}
	}
	r := Result{Floor: s.floor(), NewEvents: ev}
	s.mu.Unlock()
	return r, nil
}

// hasOpenTurnLocked reports whether task has a run still in flight with a
// request id -- one recovery is already polling. Caller holds s.mu.
func (s *MemStore) hasOpenTurnLocked(taskID string) bool {
	for _, r := range s.runs {
		if r.TaskID == taskID && r.RequestID != "" && (r.Status == RunStatusStarted || r.Status == RunStatusRunning) {
			return true
		}
	}
	return false
}

// attemptOfRun reads an attempt number off the record rather than re-deriving it
// from the run id's suffix. The id's shape is runIDFor's business and parsing it
// back would make that format load-bearing in a second place.
func attemptOfRun(runs []TaskRunRecord, runID string) int {
	for _, r := range runs {
		if r.ID == runID {
			return r.Attempt
		}
	}
	return 0
}

// setRunStatus records what recovery did to an attempt, taking the lock itself.
//
// Separate from markRun so that the reconciliation loop, which holds no lock,
// does not have to grow one around every branch. A run id that names nothing is
// not an error: a task can claim to be dispatched with no turn recorded at
// all, which is precisely the claim the requeue branch exists for.
func (s *MemStore) setRunStatus(runID, status string) {
	if runID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.markRun(runID, status)
}

func exitKind(a Attempt) string {
	switch a.Outcome() {
	case HandoffComplete:
		return KindDone
	case HandoffBlocked:
		return KindWarn
	default:
		return KindFail
	}
}
