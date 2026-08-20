package main

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"
	"sync"
)

// maxDeskLogs is how many inspector lines a desk keeps. This one is a display
// window and says so: the log is a tail, the stream is the record. fold.go
// applies the same trim, because the reducer has to agree with the writer about
// what a desk contains.
const maxDeskLogs = 40

// snapshotEvery is the snapshot ladder. Every nth event, the folded floor is
// stored so a replay can start near where it is going instead of at genesis.
//
// BR-21: this is an optimisation and never a source of truth. The proof is
// TestFoldNeedsNoSnapshotAtAll, which throws the whole ladder away and demands
// the identical floor -- if that test ever needs the ladder to pass, the ladder
// has become the record and the event stream has stopped being it.
const snapshotEvery = 500

// liveMode is the sentinel for "not replaying". -1 rather than a *int because
// this field is read on every render and a nil check per read is noise.
const liveMode = -1

// MemStore is the pass-1 store: the fixtures, in memory, behind one lock.
//
// The lock is not optional even without the simulation goroutine -- net/http
// serves every request on its own goroutine, so two operators clicking Approve
// at the same instant already race. The simulation just makes it obvious.
//
// Every exported method takes the lock, does its work against the private
// helpers (which assume the lock is HELD and never take it themselves), and
// returns deep copies. Returning a Desk by reference would hand a caller a
// pointer the simulation is about to write to.
type MemStore struct {
	mu sync.RWMutex

	rng *rand.Rand

	clock    int
	desks    []Desk
	products []Product
	masters  []Master

	// events is the record. Append-only and unbounded -- BR-04. There used to be
	// a 160-entry ring here with the replay cursor shifted whenever the window
	// moved, which made the oldest history unreachable and the cursor a lie. The
	// retention policy that eventually bounds this is by age (90 days full
	// fidelity, D-012), enforced where the events actually live, not by throwing
	// away the beginning of the stream in memory.
	events []Event
	seq    int

	// snaps is the ladder: folded floors at roughly every snapshotEvery-th
	// event, purely so a scrub does not fold from genesis every time. Never read
	// as an answer -- see snapshotEvery.
	snaps []floorSnapshot

	// pending holds log lines written since the last emit, per desk id. emit
	// drains them into the event it is writing, so the fold can reproduce the
	// inspector log rather than approximating it.
	pending map[string][]LogLine

	spend  float64
	notifs int

	focusIdx int
	// replayAtSeq is an event Seq, not an index into events. That distinction is
	// the whole of T-013's tail: an index into a slice that could be trimmed had
	// to be shifted whenever the window moved, and a Seq never moves.
	replayAtSeq int
	meetingIdx  int

	paused bool
	speed  int
	simOn  bool
}

// NewMemStore builds a store from the fixtures. The seed is a parameter so the
// tests can pin the simulation: every branch in sim.go is a probability, and a
// test that cannot reproduce a sequence cannot assert on one.
// newProjection builds an empty MemStore with every field a method on it may
// reach already set, and no fixtures in it.
//
// ArcadeStore.load() used to write this literal itself, and left rng nil
// because nothing in the load path touches it. Nothing in the LOAD path does --
// but advance() does, through pickTask, and advance() is reachable from
// ApproveTask. So a dashboard restarted against a populated graph would segfault
// the first time an operator released a merge gate, and no test caught it
// because the conformance suite seeds through NewMemStore, which does set rng.
//
// One construction path is the fix. A projection is a MemStore with no seed
// data, not a MemStore with no fields.
func newProjection() *MemStore {
	return &MemStore{
		// Fixed seed: a projection's randomness only ever picks a display task
		// title and reseats an agent, and a deterministic pick is easier to
		// replay than an arbitrary one.
		rng:         rand.New(rand.NewPCG(projectionSeed, projectionSeed^0x9E3779B97F4A7C15)),
		replayAtSeq: liveMode,
		meetingIdx:  -1,
		speed:       1,
		pending:     map[string][]LogLine{},
	}
}

// projectionSeed is arbitrary and fixed. See newProjection.
const projectionSeed uint64 = 20260819

func NewMemStore(seed uint64) *MemStore {
	s := newProjection()
	s.rng = rand.New(rand.NewPCG(seed, seed^0x9E3779B97F4A7C15))

	s.products = append([]Product(nil), seedProducts...)
	for _, m := range seedMasters {
		s.masters = append(s.masters, m.Clone())
	}

	for i, p := range seedProducts {
		st := seedDeskState[i]
		d := Desk{
			ID: p.ID, Name: p.Name, Repo: p.Repo, Goal: p.Goal,
			WorkflowID: fmt.Sprintf("wf-%d", i+1),
			State:      st.State,
			StageIdx:   st.StageIdx,
			Progress:   st.Progress,
			Cost:       st.Cost,
			Agents:     append([]string(nil), st.Agents...),
		}
		d.Task = s.pickTask(d)
		s.desks = append(s.desks, d)
	}

	// The boot sequence from the prototype: a runner line on every seated desk
	// so the inspector is not empty on the first paint, then one FloorOpened
	// event carrying the whole seeded floor.
	//
	// The order is reversed from the prototype's and that is the point.
	// FloorOpened is genesis -- §08's `?? empty` -- and it is the only event
	// that carries a floor rather than a delta, because the fixtures are a state
	// no event produced. Writing it last means it carries the boot log lines
	// too, so fold(emptyFloor(), events) reproduces this floor exactly.
	for i := range s.desks {
		if s.desks[i].State != StateIdle {
			s.log(&s.desks[i], fmt.Sprintf("runner attached · pid %d", 11000+s.rng.IntN(900)), "t")
		}
	}
	s.emit(FloorOpened, s.desks[0].ID,
		fmt.Sprintf("Mission Control attached to %d workflows", len(s.desks)), KindInfo,
		EventPayload{Genesis: cloneDesks(s.desks)})
	return s
}

func (s *MemStore) Kind() string                    { return "memory" }
func (s *MemStore) Ready(ctx context.Context) error { return nil }

// ---------- helpers, all assuming the lock is held ----------

func (s *MemStore) pickTask(d Desk) string {
	pool := taskPool[stages[d.StageIdx]]
	return pool[s.rng.IntN(len(pool))]
}

func taskIDOf(d Desk) string {
	// "wf-2" + seq 2 -> "t-213". The shape is the prototype's; it is a display
	// id, and the real Task.id comes from the graph.
	n := strings.TrimPrefix(d.WorkflowID, "wf-")
	return fmt.Sprintf("t-%s%d", n, d.TaskSeq+11)
}

func leadAgent(d Desk) *Agent {
	if len(d.Agents) == 0 {
		return nil
	}
	return agentByID(d.Agents[0])
}

func stageOf(d Desk) string {
	if d.StageIdx < 0 || d.StageIdx >= len(stages) {
		return stages[0]
	}
	return stages[d.StageIdx]
}

// log writes an inspector line and remembers it for the next event.
//
// The desk gets it immediately, because the live inspector reads the desk. The
// pending buffer gets it too, so the next emit on this desk can carry it into
// the stream -- otherwise a replayed desk would show the log it has now instead
// of the log it had then, which is the sort of small dishonesty that makes an
// audit trail worthless.
func (s *MemStore) log(d *Desk, line, cls string) {
	if cls == "" {
		cls = "t"
	}
	l := LogLine{Line: line, Cls: cls, T: s.clock}
	d.Logs = append(d.Logs, l)
	if len(d.Logs) > maxDeskLogs {
		d.Logs = d.Logs[len(d.Logs)-maxDeskLogs:]
	}

	// The buffer is trimmed to the same window the desk keeps, and for the same
	// reason plus one. The reason: a desk shows forty lines, so carrying more
	// than forty into an event is carrying something nobody can read. The extra
	// one: a desk that logs busily between events would otherwise hand every
	// subsequent event a longer list than the last, which turns a linear stream
	// into a quadratic one -- and it does so silently, because nothing about the
	// output looks wrong.
	p := append(s.pending[d.ID], l)
	if len(p) > maxDeskLogs {
		p = p[len(p)-maxDeskLogs:]
	}
	s.pending[d.ID] = p
}

// emit appends one event to the record.
//
// It used to attach a deep copy of the entire floor. Now it attaches p, the
// delta fold.go's reducer needs for this event type, and the log lines written
// since the last event on this desk. What the event MEANS is the reducer's
// business; what happened is this function's.
//
// This is the SSE seam. When the broker lands it publishes from exactly here,
// and nothing else changes: handlers already render from the events this returns
// rather than re-reading the store.
func (s *MemStore) emit(t EventType, deskID, text, kind string, p EventPayload) Event {
	s.seq++

	if t == FloorOpened {
		// Genesis carries the floor, log lines and all. Anything buffered before
		// it is already inside that floor, and letting it ride along as well
		// would append every boot line twice on the first fold.
		clear(s.pending)
	} else if lines := s.pending[deskID]; len(lines) > 0 {
		p.Logs = append(p.Logs, lines...)
		delete(s.pending, deskID)
	}

	// Clone once, here, rather than at twenty-odd call sites. A payload that
	// shares a slice or a *Meeting with the live floor is an event that changes
	// after it was written, which BR-04 says cannot happen and which would be
	// invisible until a replay showed the wrong past.
	e := Event{Seq: s.seq, Type: t, DeskID: deskID, Text: text, Kind: kind, T: s.clock, Payload: p.Clone()}
	if kind == KindWarn || kind == KindFail || t == ApprovalRequested {
		s.notifs++
	}
	s.events = append(s.events, e)

	// The ladder, not the record. See snapshotEvery.
	if e.Seq%snapshotEvery == 0 {
		s.snaps = append(s.snaps, floorSnapshot{UptoSeq: e.Seq, State: s.foldTo(e.Seq)})
	}
	return e
}

// floorSnapshot is one rung. UptoSeq says exactly which events are already
// folded into State -- the same contract the Snapshot document type declares in
// 07-events.sql, and the reason a rung can be discarded without ambiguity.
type floorSnapshot struct {
	UptoSeq int
	State   FloorState
}

// deskPayload fills the fields every desk-shaped event carries. Each emit call
// then overrides or ignores whichever ones its reducer case actually reads --
// keeping this in one place is what stops the writer and fold.go from drifting
// apart field by field.
func deskPayload(d Desk) EventPayload {
	return EventPayload{
		Task: d.Task, Goal: d.Goal, WorkflowID: d.WorkflowID,
		Agents:   append([]string(nil), d.Agents...),
		StageIdx: d.StageIdx, TaskSeq: d.TaskSeq,
		Progress: d.Progress, Score: d.Score,
		Parts: append([]ScorePart(nil), d.ScoreParts...),
		Retry: d.Retry, Esc: d.Esc, Cost: d.Cost,
	}
}

// foldTo folds the stream up to seq, starting from the nearest snapshot at or
// before it. T-097. Assumes the lock is held.
//
// The snapshot only ever decides where the fold STARTS. Hand it emptyFloor()
// instead and the answer is required to be identical, which is what T-100 says
// and what the fold acceptance test measures.
func (s *MemStore) foldTo(seq int) FloorState {
	base := emptyFloor()
	from := 0
	for _, snap := range s.snaps {
		if snap.UptoSeq > seq {
			break
		}
		base = snap.State
		// Everything at or below the rung is already inside it. Advance past
		// those rather than folding them a second time -- the reducer is
		// idempotent on absolute values, but doing the work twice is still work.
		for from < len(s.events) && s.events[from].Seq <= snap.UptoSeq {
			from++
		}
	}
	return fold(base, s.events[from:], seq)
}

// occupancy folds SEATED_IN: how many agents are in a room right now.
func (s *MemStore) occupancy(roomID string) int {
	n := 0
	if roomID == "meeting" {
		for _, d := range s.desks {
			if d.State == StateMeeting {
				n += len(d.Agents)
			}
		}
		return n
	}
	for _, d := range s.desks {
		if d.State == StateIdle || d.State == StateMeeting {
			continue
		}
		for _, aid := range d.Agents {
			a := agentByID(aid)
			if a != nil && roles[a.Role].Room == roomID {
				n++
			}
		}
	}
	return n
}

func (s *MemStore) masterPtr(id string) *Master {
	for i := range s.masters {
		if s.masters[i].ID == id {
			return &s.masters[i]
		}
	}
	return nil
}

func (s *MemStore) deskIdxByID(id string) int {
	for i, d := range s.desks {
		if d.ID == id {
			return i
		}
	}
	return -1
}

func (s *MemStore) productName(id string) string {
	for _, p := range s.products {
		if p.ID == id {
			return p.Name
		}
	}
	return id
}

func (s *MemStore) productGoal(id string) string {
	for _, p := range s.products {
		if p.ID == id {
			return p.Goal
		}
	}
	return ""
}

func (s *MemStore) stats() Stats {
	seated, attention := 0, 0
	for _, d := range s.desks {
		if d.State != StateIdle {
			seated += len(d.Agents)
		}
		if d.State == StateBlocked || d.ApprovalRequired {
			attention++
		}
	}
	return Stats{
		Tenant: "loop-eng", Health: "healthy",
		Spend: s.spend, Seated: seated, AgentCount: len(agents),
		Attention: attention, Notifs: s.notifs,
		EventCount: len(s.events), DeskCount: len(s.desks),
		Clock:    s.clock,
		Replay:   s.replayAtSeq != liveMode,
		ReplayAt: s.replayAtSeq,
		Paused:   s.paused, Speed: s.speed, SimRunning: s.simOn,
	}
}

// view is the desks a read should see: the live slice, or the folded floor if
// the cursor is parked in the past. Assumes the lock is held.
//
// This is where replay stopped being a stored recording. It folds -- from the
// nearest snapshot forward, per architecture §08 -- so any moment in the stream
// is reachable, including moments no snapshot was ever written for.
func (s *MemStore) view() []Desk {
	if s.replayAtSeq == liveMode {
		return s.desks
	}
	return s.foldTo(s.replayAtSeq).Desks
}

// floor is one consistent read. Which desks you see depends on the replay
// cursor. The rail and the topbar always describe LIVE state -- a replayed floor
// with a replayed room meter would look like the present and be the past.
func (s *MemStore) floor() Floor {
	f := Floor{Desks: cloneDesks(s.view()), Stats: s.stats(), FocusIdx: s.focusIdx}
	for _, r := range rooms {
		occ := s.occupancy(r.ID)
		f.Rooms = append(f.Rooms, RoomState{Room: r, Occupancy: occ, Full: occ >= r.Capacity})
	}
	if s.meetingIdx >= 0 && s.meetingIdx < len(s.desks) {
		d := s.desks[s.meetingIdx]
		if d.Meeting != nil {
			m := *d.Meeting
			m.Participants = append([]string(nil), d.Meeting.Participants...)
			m.Log = append([]MeetingLine(nil), d.Meeting.Log...)
			f.Meeting = &m
			f.MeetDesk = d.Name
		}
	}
	if f.FocusIdx >= len(f.Desks) {
		f.FocusIdx = 0
	}
	return f
}

func (s *MemStore) result(toast string, ev ...Event) Result {
	return Result{Floor: s.floor(), NewEvents: ev, Toast: toast}
}

// ---------- reads ----------

func (s *MemStore) Snapshot(ctx context.Context) (Floor, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.floor(), nil
}

func (s *MemStore) Desk(ctx context.Context, idx int) (Desk, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	view := s.view()
	if idx < 0 || idx >= len(view) {
		return Desk{}, 0, ErrNotFound
	}
	return view[idx].Clone(), len(view), nil
}

func (s *MemStore) Agents(ctx context.Context) ([]Agent, error) {
	return append([]Agent(nil), agents...), nil
}

func (s *MemStore) Products(ctx context.Context) ([]Product, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Product(nil), s.products...), nil
}

func (s *MemStore) Master(ctx context.Context, id string) (Master, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m := s.masterPtr(id)
	if m == nil {
		return Master{}, ErrNotFound
	}
	return m.Clone(), nil
}

func (s *MemStore) Masters(ctx context.Context) ([]Master, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Master, len(s.masters))
	for i, m := range s.masters {
		out[i] = m.Clone()
	}
	return out, nil
}

// Events returns the whole record, oldest first. Not a window: BR-04, and the
// reason Replay can reach any moment rather than the last 160.
//
// Payloads are stripped on the way out. The timeline renders type, kind, text
// and time and nothing else, so a caller that only draws pills has no use for
// the deltas -- and a payload that escaped this method would be a handle onto
// the stream's own slices. Replay folds inside the store, where the real
// payloads are, which is also what keeps the folded floor unreachable from a
// caller holding events.
func (s *MemStore) Events(ctx context.Context) ([]Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Event, len(s.events))
	for i, e := range s.events {
		e.Payload = EventPayload{}
		out[i] = e
	}
	return out, nil
}

// foldAt is foldTo, taking the lock. ArcadeStore calls it while writing a
// snapshot rung, which happens outside any read the projection is already
// serving.
func (s *MemStore) foldAt(seq int) FloorState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.foldTo(seq)
}

// rawEvents is Events without the stripping: the stream with its payloads
// intact. Not on the Store interface and not meant to be -- ArcadeStore needs it
// because it persists the deltas, and a caller outside this package has no
// business holding one.
func (s *MemStore) rawEvents() []Event {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Event, len(s.events))
	for i, e := range s.events {
		e.Payload = e.Payload.Clone()
		out[i] = e
	}
	return out
}

// ContextSlice is the exact set of graph nodes that would be rendered into the
// model's prompt. It is assembled from the product master, never invented --
// that is the claim the inspector card makes, so it has to be true.
func (s *MemStore) ContextSlice(ctx context.Context, idx int) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if idx < 0 || idx >= len(s.desks) {
		return nil, ErrNotFound
	}
	d := s.desks[idx]
	if leadAgent(d) == nil {
		return []string{"planner output only"}, nil
	}
	out := []string{"task:" + taskIDOf(d), "dependency subgraph (3)"}
	m := s.masterPtr(d.ID)
	if m == nil {
		return append(out, "repo:"+d.Repo, "policy:no-secrets-in-prompt", "diff:workspace HEAD"), nil
	}
	if m.BRD.Problem != "" {
		out = append(out, "brd:problem")
	}
	if n := len(m.BRD.Rules); n > 0 {
		out = append(out, fmt.Sprintf("rules(%d)", n))
	}
	if n := len(m.BRD.OutOfScope); n > 0 {
		out = append(out, fmt.Sprintf("out-of-scope(%d)", n))
	}
	shown := 0
	for _, st := range m.Stories {
		if st.Tasks > 0 && shown < 2 {
			out = append(out, "story:"+st.ID)
			shown++
		}
	}
	for i, g := range m.Diagrams {
		if i >= 2 {
			break
		}
		out = append(out, "diagram:"+g.ID)
	}
	for i, a := range m.Decisions {
		if i >= 2 {
			break
		}
		out = append(out, "adr:"+a.ID)
	}
	if len(m.Repos) > 0 {
		out = append(out, "repo:"+m.Repos[0])
	}
	return append(out, "policy:no-secrets-in-prompt", "diff:workspace HEAD"), nil
}

// ---------- navigation ----------

func (s *MemStore) Focus(ctx context.Context, idx int) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.desks) == 0 {
		return s.result(""), nil
	}
	if idx < 0 {
		idx = 0
	}
	if idx >= len(s.desks) {
		idx = len(s.desks) - 1
	}
	s.focusIdx = idx
	return s.result(""), nil
}

func (s *MemStore) Replay(ctx context.Context, eventSeq int) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if eventSeq < 0 {
		s.replayAtSeq = liveMode
		return s.result(""), nil
	}
	at := -1
	for i, e := range s.events {
		if e.Seq == eventSeq {
			at = i
			break
		}
	}
	if at < 0 {
		return Result{}, ErrNotFound
	}
	s.replayAtSeq = eventSeq
	if hit := s.deskIdxByID(s.events[at].DeskID); hit >= 0 {
		s.focusIdx = hit
	}
	return s.result(""), nil
}

func (s *MemStore) ClearNotifications(ctx context.Context) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notifs = 0
	for i, d := range s.desks {
		if d.ApprovalRequired || d.State == StateBlocked {
			s.focusIdx = i
			break
		}
	}
	return s.result(""), nil
}

func (s *MemStore) SetPaused(ctx context.Context, paused bool) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.paused = paused
	return s.result(""), nil
}

func (s *MemStore) SetSpeed(ctx context.Context, speed int) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if speed != 1 && speed != 2 && speed != 4 {
		return Result{}, reject("speed must be 1, 2 or 4")
	}
	s.speed = speed
	return s.result(""), nil
}

// ---------- floor mutations ----------

func (s *MemStore) ApproveTask(ctx context.Context, idx int) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if idx < 0 || idx >= len(s.desks) {
		return Result{}, ErrNotFound
	}
	d := &s.desks[idx]
	if !d.ApprovalRequired {
		return Result{}, reject("Nothing is holding the merge gate on this desk.")
	}
	d.ApprovalRequired = false
	s.log(d, "approval granted by operator", "g")
	e := s.emit(ApprovalGranted, d.ID, "Merge gate released on "+taskIDOf(*d), KindDone, deskPayload(*d))
	ev := []Event{e}
	ev = append(ev, s.advance(d)...)
	return s.result("Approved. The gate is released.", ev...), nil
}

func (s *MemStore) RetryTask(ctx context.Context, idx int) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if idx < 0 || idx >= len(s.desks) {
		return Result{}, ErrNotFound
	}
	d := &s.desks[idx]
	if d.State == StateIdle {
		return Result{}, reject("No task is running on this desk.")
	}
	// Architecture §06: max_retry_count is 3 per task. The guardrail is
	// enforced here rather than in the UI, because the UI is not the only
	// caller this method will ever have.
	if d.Retry >= 3 {
		return Result{}, reject("Retry guard exhausted (3). This needs a design review, not another attempt.")
	}
	if err := s.setState(d, StateActive); err != nil {
		return Result{}, err
	}
	d.Retry++
	d.Progress = 6
	d.Scored, d.Score, d.ScoreParts = false, 0, nil
	s.log(d, "manual retry · context widened by one hop", "y")
	e := s.emit(RetryRequested, d.ID, "Operator retry on "+taskIDOf(*d), KindWarn, deskPayload(*d))
	return s.result("Retrying with a wider context slice.", e), nil
}

func (s *MemStore) CancelTask(ctx context.Context, idx int) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if idx < 0 || idx >= len(s.desks) {
		return Result{}, ErrNotFound
	}
	d := &s.desks[idx]
	if d.State == StateIdle {
		return Result{}, reject("This desk is already idle.")
	}

	// Cancelling a desk that was holding a merge gate is a rejected approval,
	// and the catalog has a name for that. Emitting it here rather than folding
	// it into TaskCanceled is what lets an auditor tell "nobody ever answered
	// the gate" from "somebody answered it with no" -- which is exactly the
	// question an approval record exists to settle (BR-19).
	var ev []Event
	gated := d.ApprovalRequired
	task := taskIDOf(*d)

	s.mustSetState(d, StateIdle)
	d.Agents = nil
	d.Progress = 0
	d.ApprovalRequired = false
	s.log(d, "task canceled · workspace released", "r")

	if gated {
		ev = append(ev, s.emit(ApprovalRejected, d.ID, "Merge gate refused on "+task, KindFail, deskPayload(*d)))
	}
	ev = append(ev, s.emit(TaskCanceled, d.ID, "Operator canceled "+task, KindFail, deskPayload(*d)))
	if d.StageIdx >= len(stages)-1 {
		// The last stage was the workflow. Cancelling it cancels the workflow,
		// which is a different fact from cancelling one task inside a running
		// one, and D-020's closed catalog already has the name for it.
		ev = append(ev, s.emit(WorkflowCanceled, d.ID, "Workflow "+d.WorkflowID+" canceled", KindFail, deskPayload(*d)))
	}
	return s.result("Task canceled. The workspace lease is released.", ev...), nil
}

func (s *MemStore) CallReview(ctx context.Context, idx int) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if idx < 0 || idx >= len(s.desks) {
		return Result{}, ErrNotFound
	}
	if s.meetingIdx >= 0 {
		return Result{}, reject("A review is already in session.")
	}
	if s.replayAtSeq != liveMode {
		return Result{}, reject("The floor is in replay. Return to live first.")
	}
	d := &s.desks[idx]
	if d.State == StateIdle || d.State == StateMeeting {
		return Result{}, reject("There is no running task to review here.")
	}
	ev := s.startMeeting(idx, "Operator called a review on "+taskIDOf(*d))
	return s.result("Review called. The room is filling.", ev...), nil
}

func (s *MemStore) CloseMeeting(ctx context.Context) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.meetingIdx < 0 {
		return Result{}, reject("No review is in session.")
	}
	ev := s.closeMeeting()
	return s.result("Decision recorded. ADR-014 written.", ev...), nil
}

// startMeeting seats up to five agents: the architect, security and QA always,
// plus whoever is already on the desk. Assumes the lock is held.
func (s *MemStore) startMeeting(idx int, reason string) []Event {
	d := &s.desks[idx]
	seen := map[string]bool{}
	var participants []string
	for _, id := range append([]string{"a-1", "a-7", "a-5"}, d.Agents...) {
		if !seen[id] && len(participants) < 5 {
			seen[id] = true
			participants = append(participants, id)
		}
	}
	s.mustSetState(d, StateMeeting)
	d.Meeting = &Meeting{Reason: reason, Participants: participants, Conf: 38}
	s.meetingIdx = idx
	p := deskPayload(*d)
	p.Meeting = d.Meeting
	return []Event{s.emit(MeetingStarted, d.ID, reason, KindMeet, p)}
}

// closeMeeting applies the decision: an ADR is written, the model is escalated
// one tier, and the retry counter resets because the task is now a different
// task. Assumes the lock is held.
func (s *MemStore) closeMeeting() []Event {
	idx := s.meetingIdx
	s.meetingIdx = -1
	if idx < 0 || idx >= len(s.desks) {
		return nil
	}
	d := &s.desks[idx]
	var ev []Event

	// The room's own record, before the decision is applied: the proposal that
	// was on the table and then the decision taken on it. ProposalRecorded and
	// DecisionRecorded are two different facts and the catalog names them
	// separately -- a meeting that recorded a decision nobody proposed is not a
	// meeting anybody can audit.
	meet := deskPayload(*d)
	meet.Meeting = d.Meeting
	ev = append(ev, s.emit(ProposalRecorded, d.ID, "Proposal on the table · move rounding into the pricing service", KindMeet, meet))
	ev = append(ev, s.emit(DecisionRecorded, d.ID, "ADR-014 accepted · 2 action items created", KindKnow, meet))

	s.mustSetState(d, StateActive)
	d.Esc++
	d.Retry = 0
	d.Progress = 8
	d.Scored, d.Score, d.ScoreParts = false, 0, nil
	d.Task = "Apply ADR-014: move rounding into the pricing service"
	d.Meeting = nil
	s.log(d, "meeting decision applied · model upgraded to claude-opus-5", "p")
	ev = append(ev, s.emit(ModelEscalated, d.ID, "Retry running on a stronger model after the review", KindKnow, deskPayload(*d)))
	ev = append(ev, s.emit(MeetingClosed, d.ID, "Design review closed", KindMeet, deskPayload(*d)))
	return ev
}

// advance moves a workflow to the next stage, or completes it. Assumes the lock
// is held. This is the only place a desk changes stage, which is what keeps the
// stage ribbon and the seated role from disagreeing.
func (s *MemStore) advance(d *Desk) []Event {
	d.TaskSeq++
	if d.StageIdx >= len(stages)-1 {
		s.mustSetState(d, StateCompleted)
		d.Progress = 100
		s.log(d, "knowledge node written · linked to task, files, ADR", "p")
		return []Event{
			s.emit(KnowledgeExtracted, d.ID, "Lesson stored for "+d.Name, KindKnow, deskPayload(*d)),
			s.emit(WorkflowCompleted, d.ID, d.Name+" shipped", KindDone, deskPayload(*d)),
		}
	}
	d.StageIdx++
	d.Progress = 4
	s.mustSetState(d, StateActive)
	d.Scored, d.Score, d.ScoreParts = false, 0, nil
	d.Retry = 0
	d.Task = s.pickTask(*d)

	want := stageRole[stageOf(*d)]
	var cand []string
	for _, a := range agents {
		if a.Role == want {
			cand = append(cand, a.ID)
		}
	}
	if len(cand) > 0 {
		d.Agents = []string{cand[s.rng.IntN(len(cand))]}
	}
	s.log(d, "stage → "+stageOf(*d)+" · agent reseated", "t")
	return []Event{s.emit(TaskDispatched, d.ID, stageOf(*d)+": "+d.Task, KindInfo, deskPayload(*d))}
}

// ---------- product master ----------

func (s *MemStore) CreateProduct(ctx context.Context, dr ProductDraft) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	name := strings.TrimSpace(dr.Name)
	if name == "" {
		return Result{}, reject("Give the product a name.")
	}
	repo := strings.TrimSpace(dr.Repo)
	if repo == "" {
		repo = "loop/" + slug(name)
	}
	goal := strings.TrimSpace(dr.Goal)
	if goal == "" {
		goal = "No objective set yet"
	}
	owner := strings.TrimSpace(dr.Owner)
	if owner == "" {
		owner = "unassigned"
	}
	id := fmt.Sprintf("p-%d", len(s.products)+1)
	s.products = append(s.products, Product{ID: id, Name: name, Repo: repo, Goal: goal})
	s.masters = append(s.masters, Master{
		ID: id, Code: strings.ToUpper(firstN(name, 2)), Owner: owner,
		Priority: "medium", Stage: "draft", Repos: []string{repo},
	})
	e := s.emit(ProductCreated, id, name+" record created", KindInfo, EventPayload{})
	r := s.result("Product record created. Fill the requirements before planning work.", e)
	m := s.masterPtr(id).Clone()
	r.Master = &m
	return r, nil
}

func (s *MemStore) UpdateBRD(ctx context.Context, id string, b BRD) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.masterPtr(id)
	if m == nil {
		return Result{}, ErrNotFound
	}
	m.BRD = b
	e := s.emit(RecordUpdated, id, s.productName(id)+": requirements updated", KindInfo, EventPayload{})
	r := s.result("Requirements saved.", e)
	c := m.Clone()
	r.Master = &c
	return r, nil
}

func (s *MemStore) AddStory(ctx context.Context, id string, st Story) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.masterPtr(id)
	if m == nil {
		return Result{}, ErrNotFound
	}
	if strings.TrimSpace(st.Role) == "" || strings.TrimSpace(st.Want) == "" {
		return Result{}, reject("A story needs at least a role and a want.")
	}
	if st.So == "" {
		st.So = "the outcome is worth building"
	}
	if st.Pri == "" {
		st.Pri = "must"
	}
	st.ID = fmt.Sprintf("US-%02d", len(m.Stories)+1)
	st.Tasks = 0
	m.Stories = append(m.Stories, st)
	e := s.emit(RecordUpdated, id, s.productName(id)+": "+st.ID+" added", KindInfo, EventPayload{})
	r := s.result("Story added. It becomes an Issue vertex on the next injection.", e)
	c := m.Clone()
	r.Master = &c
	return r, nil
}

func (s *MemStore) AddDiagram(ctx context.Context, id string, g Diagram) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.masterPtr(id)
	if m == nil {
		return Result{}, ErrNotFound
	}
	if strings.TrimSpace(g.Title) == "" || strings.TrimSpace(g.Src) == "" {
		return Result{}, reject("A diagram needs a title and a source.")
	}
	if g.Kind != "sequence" && g.Kind != "erd" && g.Kind != "flow" {
		g.Kind = "sequence"
	}
	g.ID = fmt.Sprintf("DG-%02d", len(m.Diagrams)+1)
	m.Diagrams = append(m.Diagrams, g)
	e := s.emit(RecordUpdated, id, s.productName(id)+": "+g.ID+" added", KindInfo, EventPayload{})
	r := s.result("Diagram added.", e)
	c := m.Clone()
	r.Master = &c
	return r, nil
}

func (s *MemStore) AddDecision(ctx context.Context, id string, a Decision) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.masterPtr(id)
	if m == nil {
		return Result{}, ErrNotFound
	}
	if strings.TrimSpace(a.Title) == "" {
		return Result{}, reject("Write the decision first.")
	}
	if a.ID == "" {
		a.ID = fmt.Sprintf("ADR-%d", len(m.Decisions)+20)
	}
	if a.Confidence <= 0 || a.Confidence > 1 {
		a.Confidence = 0.8
	}
	m.Decisions = append(m.Decisions, a)
	e := s.emit(RecordUpdated, id, s.productName(id)+": "+a.ID+" recorded", KindKnow, EventPayload{})
	r := s.result("Decision recorded.", e)
	c := m.Clone()
	r.Master = &c
	return r, nil
}

// Inject writes the master subgraph. In this pass it emits the event and
// reports the projection; the write itself lands with store_arcade.go. The
// numbers reported here are the same ones the back office promised, because
// both call Master.Project.
func (s *MemStore) Inject(ctx context.Context, id string) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.masterPtr(id)
	if m == nil {
		return Result{}, ErrNotFound
	}
	pr := m.Project()
	text := fmt.Sprintf("%s: %d vertices, %d edges written", s.productName(id), pr.Vertices, pr.Edges)
	ev := []Event{s.emit(KnowledgeInjected, id, text, KindKnow, EventPayload{})}
	if idx := s.deskIdxByID(id); idx >= 0 {
		s.log(&s.desks[idx], fmt.Sprintf("master record injected · %d vertices, %d edges", pr.Vertices, pr.Edges), "p")
	}
	ev = append(ev, s.evaluateReadiness(id, *m)...)

	r := s.result(fmt.Sprintf("Injected %d vertices and %d edges into the graph.", pr.Vertices, pr.Edges), ev...)
	c := m.Clone()
	r.Master = &c
	return r, nil
}

// evaluateReadiness scores the record and says so, and says so twice when the
// score has fallen into a stricter band. Assumes the lock is held.
//
// BR-10: "downward band crossing emits ReadinessDropped and the stricter band
// applies immediately." The applying is M2.4; the reporting is here, because a
// band crossing that is never announced is a silent recalculation, which is the
// thing the requirement exists to forbid.
//
// The previous score is read back out of the event stream rather than kept in a
// field. That is not cleverness for its own sake -- a field would be state the
// orchestrator owns, which BR-01 says it may not, and it would be lost on the
// restart that ArcadeStore rebuilds from the graph. The stream survives both.
func (s *MemStore) evaluateReadiness(id string, m Master) []Event {
	score := m.ReadinessScore()
	band := ReadinessBand(score)

	prev, had := -1, false
	for i := len(s.events) - 1; i >= 0; i-- {
		if s.events[i].Type == ReadinessEvaluate && s.events[i].DeskID == id {
			prev, had = s.events[i].Payload.Score, true
			break
		}
	}

	ev := []Event{s.emit(ReadinessEvaluate, id,
		fmt.Sprintf("%s: readiness %d · %s", s.productName(id), score, band),
		KindKnow, EventPayload{Score: score})}

	if had && ReadinessBand(prev) != band && score < prev {
		ev = append(ev, s.emit(ReadinessDropped, id,
			fmt.Sprintf("%s: readiness fell from %s to %s (%d → %d)",
				s.productName(id), ReadinessBand(prev), band, prev, score),
			KindWarn, EventPayload{Score: score}))
	}
	return ev
}

// ---------- planning ----------

// PlanWorkflow is pure. It reads the master and returns a task graph without
// writing anything, so the wizard can show a plan the operator is free to throw
// away. The shape -- planner, architect, then a build/verify pair per story,
// then security, then a gated devops task -- is the prototype's buildPlan().
func (s *MemStore) PlanWorkflow(ctx context.Context, d WorkflowDraft) ([]PlanTask, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m := s.masterPtr(d.ProductID)
	if m == nil {
		return nil, ErrNotFound
	}
	picked := map[string]bool{}
	for _, id := range d.Stories {
		picked[id] = true
	}

	var plan []PlanTask
	add := func(title, role, story, dep string, gate bool) string {
		id := fmt.Sprintf("t-%d", len(plan)+1)
		model := roles[role].Model
		if gate {
			model = "claude-opus-5"
		}
		plan = append(plan, PlanTask{ID: id, Title: title, Role: role, Story: story, Dep: dep, Model: model, Gate: gate})
		return id
	}

	p1 := add("Read the record and split the goal into tasks", "planner", "", "", false)
	last := add("Confirm boundaries against the diagrams and ADRs", "architect", "", p1, false)

	any := false
	for _, st := range m.Stories {
		if !picked[st.ID] {
			continue
		}
		any = true
		impl := add("Implement "+st.Want, "backend", st.ID, last, false)
		ac := "acceptance criteria for " + st.ID
		if len(st.AC) > 0 {
			ac = st.AC[0]
		}
		last = add("Verify: "+ac, "qa", st.ID, impl, false)
	}
	if !any {
		impl := add("Implement the objective", "backend", "", last, false)
		last = add("Write integration tests", "qa", "", impl, false)
	}
	sec := add("Review the security and pricing edge cases", "security", "", last, false)
	add("Prepare the rollout and record the lesson", "devops", "", sec, d.Gate)
	return plan, nil
}

func (s *MemStore) CreateWorkflow(ctx context.Context, d WorkflowDraft, plan []PlanTask) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.masterPtr(d.ProductID)
	if m == nil {
		return Result{}, ErrNotFound
	}
	if strings.TrimSpace(d.Goal) == "" {
		return Result{}, reject("Write the objective in one sentence.")
	}
	if len(plan) == 0 {
		return Result{}, reject("The plan is empty. Go back and build it.")
	}

	// The picked stories now have planned tasks, which is what moves the
	// coverage bar. Two per story: implement + verify.
	picked := map[string]bool{}
	for _, id := range d.Stories {
		picked[id] = true
	}
	for i := range m.Stories {
		if picked[m.Stories[i].ID] {
			m.Stories[i].Tasks += 2
		}
	}

	idx := s.deskIdxByID(d.ProductID)
	if idx < 0 {
		return Result{}, ErrNotFound
	}
	dk := &s.desks[idx]
	wf := fmt.Sprintf("wf-%d", len(s.desks)+1)
	dk.WorkflowID = wf
	dk.TaskSeq = 0
	if err := s.setState(dk, StateActive); err != nil {
		return Result{}, err
	}
	dk.StageIdx = 0
	dk.Progress = 2
	dk.Scored, dk.Score, dk.ScoreParts = false, 0, nil
	dk.Retry, dk.Esc, dk.Cost = 0, 0, 0
	dk.BlockReason = ""
	dk.ApprovalRequired = false
	dk.Agents = []string{"a-9"}
	dk.Task = plan[0].Title
	dk.Goal = d.Goal
	dk.Meeting = nil

	s.log(dk, fmt.Sprintf("planner read the master record · %d stories, %d rules", len(m.Stories), len(m.BRD.Rules)), "p")
	s.log(dk, "workspace "+strings.Replace(wf, "wf", "ws", 1)+" leased", "t")
	ev := []Event{
		s.emit(WorkflowCreated, dk.ID, d.Goal, KindInfo, deskPayload(*dk)),
		s.emit(TaskPlanned, dk.ID, fmt.Sprintf("%d tasks planned from the product master", len(plan)), KindKnow, deskPayload(*dk)),
	}
	s.focusIdx = idx
	s.replayAtSeq = liveMode
	return s.result("Workflow created. The desk is live on the floor.", ev...), nil
}

// ---------- small helpers ----------

func slug(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		default:
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func firstN(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n])
}
