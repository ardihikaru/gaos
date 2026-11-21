package main

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sort"
	"strings"
	"sync"
	"time"
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

	// The card timing's per-task index into events (timing.go, D-081):
	// positions of the TaskPlanned and TaskMoved events bearing on each task,
	// extended from timingN on read. Its own mutex, because a reader holding
	// mu.RLock extends it.
	timingMu  sync.Mutex
	timingIdx map[string][]int
	convIdx   []int // D-082: every ConversationArchived / ConversationPruned
	timingN   int

	// live is the broker emit publishes to (D-065). Set on the live store
	// only: a projection built to fold or replay has none, so the past is
	// never re-announced as if it were happening now.
	live *Broker

	rng *rand.Rand

	clock    int
	desks    []Desk
	products []Product
	masters  []Master

	// tasks is the planned graph -- one TaskRecord per Task vertex, with
	// TaskRecord.Deps standing in for the DEPENDS_ON edges. T-042.
	//
	// A slice rather than a map, and that is not an oversight about lookup cost.
	// The scheduler is required to be a pure function whose answer does not vary
	// between two replicas (D-1), so every collection it reads has to have an
	// order that is a property of the data rather than of the runtime. A slice
	// has one by construction; a map would need a sort.Strings(keys) at every
	// site and would be wrong the first time somebody forgot one.
	//
	// This is NOT folded. The graph is authoritative persisted state, restored
	// by ArcadeStore.load exactly as the master record is, and the TaskPlanned
	// case in fold.go says so at more length.
	tasks []TaskRecord

	// runs is one TaskRunRecord per ATTEMPT -- T-054, D-5, BR-22 -- and slices
	// are the frozen ContextSlices they were given, keyed by digest (T-055, D-6).
	//
	// A slice and a map, and the asymmetry is deliberate. Attempts have an order
	// that is a property of the data (a task's attempt 1 precedes its attempt 2)
	// and things that read them care about it; slices have no order at all, only
	// an identity, and two attempts given the same context share one record
	// rather than storing it twice. The digest IS that identity -- it comes from
	// the egress pass, over the surviving parts in path order.
	//
	// Neither is folded, for the same reason the task graph is not: they are
	// persisted records restored by ArcadeStore.load, not projections of the
	// stream. RunStarted and RunExited describe them; they are not made of them.
	runs   []TaskRunRecord
	slices map[string]SliceRecord

	// artifacts are the handoff documents attempts produced. T-064.
	//
	// Keyed by id and not by task, because the id IS runID-handoff and the
	// interesting lookup is "the latest one on this desk" rather than "all of
	// them" -- the next attempt is cited one document, not a pile.
	artifacts map[string]ArtifactRecord

	// qa is the QA catalog: product -> case id -> case. Not folded, restored
	// by ArcadeStore.load (qacatalog.go).
	qa map[string]map[string]QaCase

	// workflowSeq numbers workflows. Monotonic, and never derived from
	// len(desks): the desks are the six seeded products and their count does not
	// change, so `wf-len(desks)+1` handed every workflow ever created the same
	// id. Harmless while a workflow was only a desk field; not harmless at all
	// once Task vertices hang off Workflow by HAS_TASK and Workflow (tenantId,
	// id) is UNIQUE.
	workflowSeq int

	// workflowGoals is each workflow's own goal, by workflow id (v0.40.5). The
	// desk is one per product and carries only the LATEST workflow's goal, so a
	// task of an earlier workflow on the same product briefed from d.Goal was
	// told somebody else's objective. Restored by ArcadeStore.load from every
	// Workflow vertex's goal, not only the seated one's.
	workflowGoals map[string]string

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

	// The egress gate and the model gateway. T-126 and T-057.
	//
	// The policy lives on the store rather than on the adapter because §7.6.1
	// says the egress block is not per-mode: "the pre-dispatch check in L-4
	// runs identically whether a subprocess or an HTTP request is about to
	// carry the slice out." An adapter that owned its own policy could differ
	// from the other one, which is the whole thing that rule forbids.
	//
	// adapter is nil in every test and in every deployment that has not
	// configured one, and DispatchTask is written so that nil means "screen the
	// packet, record the verdict, and stop there". That is not a degraded mode:
	// with no runner and no lease (M4.2), actually spawning the tool would emit
	// RunStarted and RunExited with nothing supervising them, which events.go
	// calls inventing evidence.
	egress  EgressPolicy
	target  Target
	adapter Adapter

	// onWrite is how a BACKGROUND writer's changes reach durable storage.
	//
	// Every other mutation on this store is called by a handler, and ArcadeStore
	// wraps those in mutate() which persists whatever they produced. The runner
	// is the first writer that is nobody's request: it emits RunStarted minutes
	// before it emits RunExited, from a goroutine no handler is waiting on, so
	// there is no mutate() around it and its events would live and die in this
	// projection.
	//
	// nil in a memory deployment, where the projection IS the store and there is
	// nothing to flush to. ArcadeStore sets it. Called outside the lock, because
	// it writes to a database.
	onWrite func(Result)

	// repoParts is where repository files would enter a packet: M1.2's index,
	// or a Blueprint knowledge source once OQ-28 says how one is read. nil in
	// every deployment, so a packet carries the record and the brief alone.
	// GAOS reads no folder on this host since M14.7 (BR-42), and the seam is
	// kept so the allowlist half of the egress pass still has a caller.
	repoParts func(deskID string) []PacketPart
}

// setEgressForTest overrides the deployment's egress policy.
//
// Unexported and named for what it is, because there is deliberately no Store
// method for this: §7.6.1 makes the egress block a deployment decision read
// once at boot, and a caller that could vary it per request would be a per-task
// override of exactly the kind that section refuses to offer. The conformance
// suite needs one to prove a refusal happens, so it gets a door with a sign on
// it rather than a hole in the seam.
func (s *MemStore) setEgressForTest(p EgressPolicy, tgt Target) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.egress, s.target = p, tgt
}

// SetAdapter installs the model gateway. Called once, from serve(), after the
// boot smoke test has passed -- never per request, because §7.6.1 makes the
// mode a deployment decision and a per-task override "would make two attempts
// at the same task incomparable".
// SetOnWrite installs the flush hook. See MemStore.onWrite.
func (s *MemStore) SetOnWrite(f func(Result)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onWrite = f
}

// publish hands a background writer's events to durable storage, if there is
// any. Takes the lock only to read the floor and the hook, and calls the hook
// outside it: persisting is a network round trip, and holding the floor's lock
// across one would stall every reader on it.
func (s *MemStore) publish(ev ...Event) {
	if len(ev) == 0 {
		return
	}
	s.mu.RLock()
	f, floor := s.onWrite, s.floor()
	s.mu.RUnlock()
	if f != nil {
		f(Result{Floor: floor, NewEvents: ev})
	}
}

func (s *MemStore) SetAdapter(a Adapter) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.adapter = a
	if a != nil {
		s.target = a.Target()
	}
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
		slices:      map[string]SliceRecord{},
		artifacts:   map[string]ArtifactRecord{},
		qa:          map[string]map[string]QaCase{},

		// The documented defaults of §7.6.1, so a store built without a
		// configuration still screens. An egress policy that defaulted to
		// nothing would make BR-32 true only for deployments that remembered
		// to configure it, which is the same as not being true.
		egress: ModelConfigFromEnv().Egress,
	}
}

// Live is the store's broker, for GET /events/stream.
func (s *MemStore) Live() *Broker { return s.live }

// projectionSeed is arbitrary and fixed. See newProjection.
const projectionSeed uint64 = 20260819

func NewMemStore(seed uint64) *MemStore {
	s := newProjection()
	s.live = NewBroker()
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
	// The seeded desks took wf-1..wf-N, so the counter starts where they left
	// off rather than at zero. ArcadeStore.load restores it the same way, from
	// the ids actually in the graph.
	s.workflowSeq = len(s.desks)

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

// taskIDOf builds the DISPLAY id of the task a desk is currently showing.
//
// "wf-2" + seq 2 -> "t-213". The shape is the prototype's, and the name it
// produces is not a planned task's: since T-042 a planned Task carries an id of
// the form "wf-9-t-1", qualified by qualify() so two plans cannot collide under
// Task (tenantId, id) UNIQUE. Three id spaces now exist where a reader will
// assume one -- this display id, the plan-local "t-1" PlanWorkflow hands the
// wizard, and the graph id. This one is the display id. It is not a key to
// anything the scheduler reads, and the day the graph drives the desk it should
// be retired rather than reconciled.
func taskIDOf(d Desk) string {
	n := strings.TrimPrefix(d.WorkflowID, "wf-")
	return fmt.Sprintf("t-%s%d", n, d.TaskSeq+11)
}

// workflowNumber reads the counter back out of a workflow id. Returns false for
// anything that is not "wf-<digits>", so an id from some other scheme cannot
// silently reset the sequence to zero.
func workflowNumber(id string) (int, bool) {
	rest, ok := strings.CutPrefix(id, "wf-")
	if !ok || rest == "" {
		return 0, false
	}
	n := 0
	for _, c := range rest {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
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
	e := Event{Seq: s.seq, Type: t, DeskID: deskID, Text: text, Kind: kind, T: s.clock, At: time.Now().Unix(), Payload: p.Clone()}
	if needsNotice(e) {
		s.notifs++
	}
	s.events = append(s.events, e)
	s.live.Publish(e) // never blocks; see broker.go

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

// TaskContext answers §09's "the exact slice sent to the model". T-056.
//
// Two answers, and the caller is told which one it got. If the desk has an
// attempt behind it, the frozen ContextSlice that attempt was given is the
// answer -- evidence, in D-6's sense, unchanged by anything edited since. If
// nothing has run, there is no evidence and the live assembly is returned with
// Frozen false, because a projection of today's record is a reasonable preview
// and a terrible audit trail, and the difference has to be visible rather than
// inferred.
func (s *MemStore) TaskContext(ctx context.Context, idx int) (ContextView, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if idx < 0 || idx >= len(s.desks) {
		return ContextView{}, ErrNotFound
	}
	d := s.desks[idx]

	if rec, attempt, ok := s.frozenFor(d.ID); ok {
		return ContextView{
			Chips:       chipsFor(rec),
			Frozen:      true,
			Digest:      rec.Digest,
			AssembledAt: rec.AssembledAt,
			Attempt:     attempt,
			Cites:       rec.Cites,
		}, nil
	}
	return ContextView{Chips: s.liveSlice(d)}, nil
}

// frozenFor finds the slice this desk was last given, and the attempt number to
// report with it.
//
// Two sources, in that order. An attempt's own SliceDigest is the better answer
// because it names what a process was actually handed. Failing that, the most
// recently frozen slice for the desk still counts: a dispatch that cleared the
// egress pass with no runner configured froze a real slice and never spawned
// anything, and calling that "live" would hide a thing that genuinely happened.
//
// What it will NOT do is open a TaskRun for that case. There was no attempt --
// no process, no cost, no exit -- and events.go's rule about inventing evidence
// applies to records as much as to events.
//
// Assumes the lock is held.
func (s *MemStore) frozenFor(deskID string) (SliceRecord, int, bool) {
	if run, ok := s.latestRunForDesk(deskID); ok {
		if rec, ok := s.slices[run.SliceDigest]; ok {
			return rec.Clone(), run.Attempt, true
		}
	}
	var (
		best  SliceRecord
		found bool
	)
	for _, rec := range s.slices {
		if rec.DeskID != deskID {
			continue
		}
		// Ties broken by digest, not by map order: two slices frozen in the same
		// second would otherwise make this answer depend on Go's map iteration,
		// and an audit route that returns a different answer per call is worse
		// than one that returns the wrong answer consistently.
		if !found || rec.AssembledAt > best.AssembledAt ||
			(rec.AssembledAt == best.AssembledAt && rec.Digest > best.Digest) {
			best, found = rec, true
		}
	}
	return best.Clone(), best.Attempt, found
}

// chipsFor renders a frozen slice as the inspector's chips.
//
// From the citations, not from the master: the whole value of a frozen slice is
// that it does not consult the record again. A story deleted after the attempt
// ran still shows here, because it was there when the model was given it.
func chipsFor(rec SliceRecord) []string {
	out := []string{"task:" + rec.TaskID, fmt.Sprintf("digest:%s", rec.Digest)}
	byRole := map[string]int{}
	var order []string
	for _, c := range rec.Cites {
		if _, seen := byRole[c.Role]; !seen {
			order = append(order, c.Role)
		}
		byRole[c.Role]++
	}
	sort.Strings(order)
	for _, role := range order {
		out = append(out, fmt.Sprintf("%s(%d)", role, byRole[role]))
	}
	return append(out, fmt.Sprintf("parts(%d)", rec.NodeCount),
		fmt.Sprintf("~%d tokens", rec.TokenCount))
}

// liveSlice is the set of graph nodes that WOULD be rendered into the prompt,
// assembled from the product master and never invented -- that is the claim the
// inspector card makes, so it has to be true. Assumes the lock is held.
func (s *MemStore) liveSlice(d Desk) []string {
	if leadAgent(d) == nil {
		return []string{"planner output only"}
	}
	out := []string{"task:" + taskIDOf(d), "dependency subgraph (3)"}
	m := s.masterPtr(d.ID)
	if m == nil {
		return append(out, "repo:"+d.Repo, "policy:no-secrets-in-prompt", "diff:workspace HEAD")
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
	return append(out, "policy:no-secrets-in-prompt", "diff:workspace HEAD")
}

// Runs is a task's attempts, oldest first. The read side of T-054: BR-22's
// "each attempt is its own record" is only true if something can ask for them.
func (s *MemStore) Runs(ctx context.Context, taskID string) ([]TaskRunRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.runsFor(taskID), nil
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

// DispatchTask freezes a packet, screens it, and dispatches. T-126, T-127,
// and the caller T-057's adapter was missing.
//
// This is the operator-triggered half of the §7.6 execution flow, steps 2-5.
// There is no scheduler behind it, no lease and no heartbeat -- those are T-048
// and T-050-053. What it buys is that the egress pass runs against the real
// store and its events land on the real timeline, rather than only inside a
// test. L-5 asks for the redaction and the refusal to "appear in the timeline
// like any other state change", and an event with no emitter does not appear
// anywhere.
//
// WHAT THIS DELIBERATELY DOES NOT DO: it does not emit RunStarted or RunExited.
// Those two names are in events.go's "needs the runner -- M4.2" list and in
// events_test.go's owed map. Writing them without a lease and a heartbeat
// behind them would be a RunExited from a process nothing is supervising, which
// the header of events.go calls inventing evidence -- and the audit trail is
// the last place to do that.
//
// A REFUSAL RETURNS (Result, nil), NOT AN ERROR, and that is the one surprising
// line in the method. A refusal is a RECORDED STATE CHANGE, not a rejected
// mutation: it blocks the desk and writes two events. If it came back as
// ErrRejected the handler would take the s.fail path, applyResult would never
// run, and the events this task exists to produce would be in the stream and
// absent from the response -- visible only after the next full render. L-5 says
// they appear on the timeline; that is a Result, not an error.
func (s *MemStore) DispatchTask(ctx context.Context, idx int) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if idx < 0 || idx >= len(s.desks) {
		return Result{}, ErrNotFound
	}
	d := &s.desks[idx]

	// Only a desk with nothing in flight may be dispatched. Re-dispatching a
	// running task would produce a second attempt against one TaskRun, which
	// BR-22 is precisely about not doing.
	if d.State != StateIdle && d.State != StateBlocked {
		return Result{}, reject("This desk already has a task in flight.")
	}

	attempt := s.nextAttemptLocked(taskIDOf(*d))
	pkt := s.freezePacket(*d, attempt)
	pkt.Vendor, pkt.Model = s.vendorForLocked(d.ID, pkt.Role)
	if ta, ok := s.adapter.(TurnAdapter); ok {
		pkt.SessionID, pkt.SessionReason = s.sessionForLocked(taskIDOf(*d), ta, pkt.Vendor, pkt.Model, time.Now().Unix())
	}
	cleared, v, err := Screen(s.egress, s.target, pkt)
	// BR-39, the operator's route: drift refuses exactly as the scheduler's
	// does, through the same refusal branch below, so the timeline cannot tell
	// which route met it.
	if _, drift := s.blueprintGate(d.ID); drift != nil {
		cleared, v, err = Cleared{}, *drift, &EgressRefusal{Rule: drift.Rule, Reason: drift.Reason}
	}

	var ev []Event

	// The redaction event is emitted whether or not the dispatch then proceeds.
	// A slice that was cut down and then refused was still cut down, and L-5
	// wants both facts on the record rather than only the more dramatic one.
	if len(v.Redactions) > 0 {
		for _, r := range v.Redactions {
			s.log(d, fmt.Sprintf("egress: dropped %s — %s", r.Path, r.Why), "y")
		}
		ev = append(ev, s.emit(ContextRedacted, d.ID,
			fmt.Sprintf("Context slice cut by %d part(s) before dispatch", len(v.Redactions)),
			KindWarn, EventPayload{
				Redactions: len(v.Redactions), Bytes: v.Bytes, Digest: v.Digest,
			}))
	}

	if err != nil {
		if serr := s.setState(d, StateBlocked); serr != nil {
			return Result{}, serr
		}
		d.BlockReason = v.Reason
		d.Progress = 0
		d.Agents = nil
		s.log(d, "egress: dispatch refused — "+v.Reason, "r")
		ev = append(ev, s.emit(DispatchRefused, d.ID, v.Reason, KindFail, EventPayload{
			Reason: v.Reason, Rule: v.Rule, Bytes: v.Bytes, Digest: v.Digest,
			Redactions: len(v.Redactions),
		}))
		return s.result("Dispatch refused. "+v.Reason, ev...), nil
	}

	if serr := s.setState(d, StateActive); serr != nil {
		return Result{}, serr
	}
	d.BlockReason = ""
	d.Progress = 2
	d.Task = s.pickTask(*d)
	d.Scored, d.Score, d.ScoreParts = false, 0, nil
	if len(d.Agents) == 0 {
		if a := agentForStage(d.StageIdx); a != nil {
			d.Agents = []string{a.ID}
		}
	}
	// The slice is frozen HERE, before anything is dispatched and whether or not
	// a runner is configured. D-6 is about what was assembled and cleared, not
	// about what a provider did with it afterwards; a dispatch that then failed
	// to spawn still has an answer to "what would it have been given", and the
	// operator-triggered route with no adapter behind it is exactly that case.
	slice := s.recordSlice(cleared, taskIDOf(*d), d.ID, 1)

	s.log(d, fmt.Sprintf("packet frozen · %d parts · %d bytes · digest %s",
		slice.NodeCount, cleared.Bytes(), cleared.Digest()), "g")

	p := deskPayload(*d)
	p.Bytes, p.Digest = cleared.Bytes(), cleared.Digest()
	ev = append(ev, s.emit(TaskDispatched, d.ID,
		"Dispatched "+taskIDOf(*d)+" · slice cleared", KindInfo, p))

	// And now, if a runner is configured, something actually runs.
	//
	// This is the gap store_mem.go's header described for a release: "adapter is
	// nil in every test and in every deployment that has not configured one, and
	// DispatchTask is written so that nil means screen the packet, record the
	// verdict, and stop there ... with no runner and no lease (M4.2), actually
	// spawning the tool would emit RunStarted and RunExited with nothing
	// supervising them." There is a lease now, and a supervisor.
	//
	// Two conditions, not one. An adapter exists so the boot smoke test can run;
	// GAOS_RUNNER is what says money may be spent. See runnerMode. Only `on`:
	// under `manual` the board's dispatch is the one route that spends (D-080),
	// and this desk route has neither its dependency check nor its BR-38 claim.
	//
	// A goroutine, because superviseRun blocks for as long as the work takes and
	// this method is holding the floor's lock. The goroutine's first act that
	// needs the lock will simply wait for this to return.
	//
	// context.WithoutCancel because ctx here is the HTTP request's, which is
	// cancelled the moment the response is written -- a run tied to it would die
	// as the operator's browser finished painting the toast.
	if s.adapter != nil && runnerMode() == RunnerOn {
		ad, deskID, cl := s.adapter, d.ID, cleared
		run := context.WithoutCancel(ctx)
		go func() { _, _ = s.superviseRun(run, ad, deskID, cl, attempt) }()
	}

	return s.result("Dispatched. The slice cleared the egress pass.", ev...), nil
}

// freezePacket assembles the task packet of §7.6, in memory.
//
// Materialising it as FILES in the leased workspace is still T-061 and still
// does not exist -- Packet.Workspace is the path an adapter would run in, and
// this store does not write into it. What does exist now is the record of what
// was assembled: recordSlice stores the cleared packet as a ContextSlice keyed
// by digest (T-055), so the answer to "what was attempt two given" comes out of
// the store rather than out of a second guess at the same question.
//
// Assumes the lock is held.
func (s *MemStore) freezePacket(d Desk, attempt int) Packet {
	return s.freezePacketFor(d, taskIDOf(d), attempt)
}

// recordSlice freezes what the egress pass cleared, as one ContextSlice per
// digest. T-055, requirement D-6, rule BR-23.
//
// Keyed by digest and not by attempt, and that is the requirement read
// carefully rather than a shortcut. D-6 wants the slice frozen PER ATTEMPT; two
// attempts given byte-identical context were given one slice, and storing it
// twice would make H-4's "compare two runs of the same task" a diff of two
// copies of the same string instead of a pointer comparison. The attempt keeps
// its own reference either way, on TaskRunRecord.SliceDigest, so nothing is
// lost -- and the first attempt's assembly time is the one kept, because that is
// when this context came into existence.
//
// Rendered is the parts joined, in the SAME path order the digest was computed
// over. Any other order would render a slice whose text disagreed with its own
// identity.
//
// Assumes the lock is held.
func (s *MemStore) recordSlice(c Cleared, taskID, deskID string, attempt int) SliceRecord {
	if s.slices == nil {
		s.slices = map[string]SliceRecord{}
	}
	if existing, ok := s.slices[c.Digest()]; ok {
		return existing.Clone()
	}

	parts := c.Packet().Parts
	sort.Slice(parts, func(i, j int) bool { return parts[i].Path < parts[j].Path })
	var b strings.Builder
	for _, p := range parts {
		fmt.Fprintf(&b, "==> %s (%s)\n%s\n", p.Path, p.Kind, p.Body)
	}

	rec := SliceRecord{
		Digest:      c.Digest(),
		TaskID:      taskID,
		DeskID:      deskID,
		Attempt:     attempt,
		AssembledAt: time.Now().Unix(),
		TokenCount:  tokenEstimate(b.String()),
		NodeCount:   len(parts),
		Bytes:       c.Bytes(),
		Rendered:    b.String(),
		Cites:       c.Cites(),
	}
	s.slices[rec.Digest] = rec
	return rec.Clone()
}

// startRun opens the record for one attempt, before anything has spawned.
//
// Before, and not after, because the record is what a restart reads. A run that
// crashed between the spawn and its first event has to leave something behind
// that says an attempt was made -- otherwise boot reconciliation sees a task in
// DISPATCHED with no attempt against it and cannot tell a lost run from one that
// never began. STARTED is that fact; superviseRun moves it to RUNNING once a
// process actually exists.
//
// Assumes the lock is held.
func (s *MemStore) startRun(r TaskRunRecord) TaskRunRecord {
	for i := range s.runs {
		if s.runs[i].ID == r.ID {
			// An adopted run, or a retried write. The attempt is identified by
			// (task, attempt) and re-opening it must not fork the record: BR-22
			// says each attempt is ONE record, and two rows for one attempt would
			// double every cost aggregated over them.
			s.runs[i].Status = r.Status
			return s.runs[i].Clone()
		}
	}
	s.runs = append(s.runs, r)
	return r.Clone()
}

// markRun applies a status to one attempt and nothing else. Used by boot
// reconciliation, where the only thing known about the run is what happened to
// it. Assumes the lock is held.
func (s *MemStore) markRun(runID, status string) {
	for i := range s.runs {
		if s.runs[i].ID == runID {
			s.runs[i].Status = status
			if s.runs[i].EndedAt == 0 && status != RunStatusRunning {
				s.runs[i].EndedAt = time.Now().Unix()
			}
			return
		}
	}
}

// closeRun writes what the attempt cost and what it left behind.
//
// Everything here comes off the Attempt, which is the mode-independent shape the
// adapter seam produces -- including CostBasis. It is NOT defaulted to
// "estimated" when the tool reported nothing: BR-31 exists so that a number
// nobody measured cannot be aggregated with one somebody did, and inventing the
// basis is a more thorough way of breaking that rule than omitting the cost.
//
// Assumes the lock is held.
func (s *MemStore) closeRun(runID string, att Attempt, runErr error) {
	for i := range s.runs {
		if s.runs[i].ID != runID {
			continue
		}
		r := &s.runs[i]
		r.Status = RunStatusExited
		r.EndedAt = time.Now().Unix()
		r.ExitCode = att.ExitCode
		r.TokensIn, r.TokensOut = att.TokensIn, att.TokensOut
		r.CostUSD, r.CostBasis = att.CostUSD, att.CostBasis
		r.FailureSignature = att.Failure
		if att.LogRef != "" {
			r.LogRef = att.LogRef
		}
		if att.Model != "" {
			r.Model = att.Model
		}
		if att.Invocation != "" {
			r.Invocation = att.Invocation
		}
		if att.RequestID != "" {
			r.RequestID = att.RequestID
		}
		if att.SessionID != "" {
			r.SessionID = att.SessionID
		}
		if runErr != nil {
			// The process never produced an exit status. ExitCrashed already
			// says so through ExitCode -1; the signature says what to do about
			// it, and a run that could not start is not a failure of the work.
			r.ExitCode = -1
			if r.FailureSignature == "" {
				r.FailureSignature = "run-not-started"
			}
		}
		return
	}
}

// runsFor returns one task's attempts, oldest first. Copies, for the reason
// tasksFor returns copies. Assumes the lock is held.
func (s *MemStore) runsFor(taskID string) []TaskRunRecord {
	var out []TaskRunRecord
	for _, r := range s.runs {
		if r.TaskID == taskID {
			out = append(out, r.Clone())
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Attempt < out[j].Attempt })
	return out
}

// latestRunForDesk is what the inspector needs: the most recent attempt on the
// desk being looked at, whatever task it belonged to. Assumes the lock is held.
func (s *MemStore) latestRunForDesk(deskID string) (TaskRunRecord, bool) {
	var (
		best  TaskRunRecord
		found bool
	)
	for _, r := range s.runs {
		if r.DeskID != deskID {
			continue
		}
		if !found || r.StartedAt > best.StartedAt ||
			(r.StartedAt == best.StartedAt && r.Attempt > best.Attempt) {
			best, found = r, true
		}
	}
	return best.Clone(), found
}

// roleFor is the role the handoff document must name. The seated agent's, and
// where no agent is seated, the planned task's -- a role GAOS invented would be
// a document that describes work nobody was assigned. Empty is a legal answer;
// handoffContract fills in a default rather than shipping an empty mandatory
// field to a model. Assumes the lock is held.
func (s *MemStore) roleFor(d Desk) string {
	if a := leadAgent(d); a != nil {
		return a.Role
	}
	for _, t := range s.tasks {
		if t.DeskID == d.ID && t.Status != StatusDone && t.Role != "" {
			return t.Role
		}
	}
	return ""
}

// recordHandoff stores what one attempt handed over. T-064. Assumes the lock is
// held.
func (s *MemStore) recordHandoff(a ArtifactRecord) {
	if a.ID == "" || strings.TrimSpace(a.Body) == "" {
		// A document nobody wrote is not an artifact. BR-30 already recorded the
		// absence as a failure signature on the attempt; inventing an empty
		// Artifact beside it would put a second, softer version of the same fact
		// in the graph.
		return
	}
	if s.artifacts == nil {
		s.artifacts = map[string]ArtifactRecord{}
	}
	s.artifacts[a.ID] = a
}

// latestHandoffFor finds the document the previous attempt on this desk left.
//
// Scoped to the desk rather than the task on purpose. A workflow's desk moves
// through stages -- architecture, then coding, then review -- and D-10's "the
// next agent" is the next one to sit at that desk, which is usually a different
// task and always a different role. Restricting it to the same task id would
// hand a retry its own previous attempt and hand a new stage nothing, which is
// the opposite of what §7.6's chain describes.
//
// Attempts at or after `attempt` are excluded so a packet frozen twice for the
// same attempt does not cite itself. Assumes the lock is held.
func (s *MemStore) latestHandoffFor(deskID, taskID string, attempt int) (ArtifactRecord, bool) {
	var (
		best  ArtifactRecord
		found bool
	)
	for _, a := range s.artifacts {
		// Handoffs only. The prompt and the reply are artifacts too (BR-41),
		// and the newest artifact of an attempt is its reply -- citing that
		// as the lesson hands the next agent a transcript, not a handoff.
		if a.DeskID != deskID || a.Kind != ArtifactHandoff {
			continue
		}
		if a.TaskID == taskID && a.Attempt >= attempt {
			continue
		}
		// Ties by id, never by map order: a packet whose contents depended on Go's
		// map iteration would produce a different digest on every freeze, and the
		// digest is what two runs are compared by.
		if !found || a.CreatedAt > best.CreatedAt ||
			(a.CreatedAt == best.CreatedAt && a.ID > best.ID) {
			best, found = a, true
		}
	}
	return best, found
}

// failureOfRunLocked is the failure signature of the run, or "" when it did
// not fail (or is not known). Assumes the lock is held.
func (s *MemStore) failureOfRunLocked(runID string) string {
	for _, r := range s.runs {
		if r.ID == runID {
			return r.FailureSignature
		}
	}
	return ""
}

// failureLesson is how a failed attempt's stored text reaches the next agent:
// said to be an error before it is shown.
func failureLesson(runID, sig, body string) string {
	return fmt.Sprintf("The previous attempt (run %s) failed with `%s`. What follows is the error it returned, not a handoff from an agent.\n\n%s", runID, sig, body)
}

// freezePacketFor is freezePacket with the task id supplied.
//
// The operator-triggered route has no graph task behind it and passes the
// display id; the scheduler-driven route passes the id of the Task vertex it
// decided to run. The difference matters downstream: the runner updates the
// TaskRecord it was named, and a packet carrying a display id simply has no
// record to update -- which is correct rather than lossy, because there is no
// planned task behind an operator poking a desk.
// The attempt number is threaded through because the packet has to name the run
// the document must be written against, and a model told to "use the run id"
// will eventually use a different spelling of it. It also means two attempts at
// one task have different packets and therefore different digests, which is
// correct rather than inconvenient: attempt two really was given a different
// instruction.
func (s *MemStore) freezePacketFor(d Desk, taskID string, attempt int) Packet {
	// The task's kind decides the shape of this packet (M17, D-069). A review
	// gets a packet of its own -- the design and the rubric, and nothing else.
	kind := TaskKindImplement
	role := s.roleFor(d)
	// The brief is the dispatched task's own when a planned task stands behind
	// the id. d.Task is the desk's display line -- CreateWorkflow sets it to the
	// plan's FIRST task -- so briefing from it told every later task of the
	// graph (the architect, the implementer, the verifier) that it was the
	// planner. The role follows the task for the same reason: the seated agent
	// is the planner for the whole workflow.
	brief := d.Task
	// The objective is the goal of the dispatched task's OWN workflow (v0.40.5).
	// d.Goal is the desk's, and the desk is one per product: a second workflow
	// on the product overwrites it, so reading it told a task of either
	// workflow the other one's objective.
	goal := d.Goal
	if t := s.taskByIDLocked(taskID); t != nil {
		if g := s.workflowGoals[t.WorkflowID]; strings.TrimSpace(g) != "" {
			goal = g
		}
		kind = taskKind(t.Kind)
		if t.Role != "" {
			role = t.Role
		}
		brief = s.taskBriefLocked(d.ID, *t)
	}
	pkt := Packet{
		TaskID: taskID,
		RunID:  runIDFor(taskID, attempt),
		Role:   role,
		DeskID: d.ID,
		Kind:   kind,
	}
	// A review runs in the product's own reviewer workspace (M17.5, D-071): a
	// second root that holds the published Blueprint and none of the author's
	// files. The kind is known here, so the id is derived once, and the store
	// and the adapter cannot disagree about where the turn runs.
	if kind == TaskKindReview {
		pkt.WorkspaceID = s.reviewerWorkspaceID(d.ID)
	}
	add := func(path, kind, body string) {
		if strings.TrimSpace(body) != "" {
			pkt.Parts = append(pkt.Parts, PacketPart{Path: path, Kind: kind, Body: body})
		}
	}

	// cite records WHY a node is in this packet, against the part carrying it.
	//
	// Built here rather than derived later because here is the only place that
	// knows: once the record has been flattened into markdown, "which requirement
	// is that bullet" is a parsing problem, and USES_CONTEXT is supposed to be
	// the answer to it rather than another thing that needs one.
	cite := func(nodeType, nodeID, role, path, body string) {
		pkt.Cites = append(pkt.Cites, Citation{
			NodeType: nodeType, NodeID: nodeID, Role: role,
			Path: path, Tokens: tokenEstimate(body),
		})
	}

	// The deliverable is named at the TOP as well as in the contract at the
	// bottom, and the repetition is measured rather than nervous: on the first
	// real call this repository made, an agent read the whole packet, did the
	// work well, and never wrote the handoff -- the contract had been the last
	// section of a long input and read as background by the time the work was
	// done. Two mentions and a standing system prompt, because an attempt whose
	// work GAOS cannot see is an attempt that did not happen (BR-30).
	// A review sees none of this. The brief, the goal and the contract are the
	// author's side of the desk, and D-069 clause 3 withholds them from the
	// reviewer: what is withheld is the author's session state.
	if kind != TaskKindReview {
		add(".gaos/packet/task.md", KindTask,
			"# "+taskID+"\n\n"+brief+"\n\n## Objective\n"+goal+
				"\n\n## Definition of done\nThe work above, AND the handoff: one gaos-handoff block at the end "+
				"of your reply — see .gaos/packet/handoff.md for its exact shape. "+
				"A reply that does the work and carries no block is recorded as failed.\n")
	}

	// The contract, T-062. In the packet rather than bolted onto the instruction
	// at spawn time, and that placement is the requirement rather than a
	// preference: what the run was ASKED for is as much part of the evidence as
	// what it was given, so it belongs inside the frozen slice and inside the
	// digest. A replay can then prove both halves.
	add(".gaos/packet/handoff.md", KindContract,
		contractsFor(kind, taskID, runIDFor(taskID, attempt), role))

	// What the previous attempt handed over, T-064. It reaches this agent HERE
	// and nowhere else -- as a cited part of its own frozen slice, not by
	// reading a file somebody left in the workspace. D-10: the document "reaches
	// the next agent only through USES_CONTEXT on that agent's slice", which is
	// what makes it possible to ask afterwards what attempt two was told about
	// attempt one.
	//
	// A failed turn's error text is stored as its attempt's handoff, because it
	// is still the best evidence of what happened. It is labelled as a failure
	// here, under its own name, so the next agent cannot mistake an error for
	// work somebody handed over.
	// The operator's answer to the question the last attempt asked (M15). In the
	// slice, cited, like the handoff below -- and on the packet as well, so the
	// gateway adapter can send it through the answer route that resumes the
	// session which asked.
	// The operator's answer and the previous stage's handoff are the author's
	// session state, and a reviewer gets neither (D-069 clause 3). The handoff
	// chain is desk-scoped on purpose (latestHandoffFor), so without this the
	// reviewer would inherit the author's handoff by default.
	if kind != TaskKindReview {
		if q, ok := s.answerForLocked(taskID, attempt); ok {
			const path = ".gaos/packet/context/answer.md"
			body := questionBody(q.QuestionPayload)
			add(path, KindContext, body)
			cite("Artifact", q.RunID+"-question", CiteLesson, path, body)
			if q.Attempt == attempt-1 {
				pkt.Answer = &PacketAnswer{QuestionID: q.ID, Choice: q.Choice, Text: q.Text}
			}
		}

		if a, ok := s.latestHandoffFor(d.ID, taskID, attempt); ok {
			path, body := ".gaos/packet/context/handoff-previous.md", a.Body
			if sig := s.failureOfRunLocked(a.RunID); sig != "" {
				path = ".gaos/packet/context/failure-previous.md"
				body = failureLesson(a.RunID, sig, a.Body)
			}
			add(path, KindContext, body)
			cite("Artifact", a.ID, CiteLesson, path, body)
		}
	}

	m := s.masterPtr(d.ID)
	if m == nil {
		return pkt
	}

	const rulesPath = ".gaos/packet/rules.md"
	var rules strings.Builder
	if m.BRD.Problem != "" {
		fmt.Fprintf(&rules, "## Problem\n%s\n\n", m.BRD.Problem)
	}
	for i, r := range m.BRD.Rules {
		fmt.Fprintf(&rules, "- %s\n", r)
		cite("Requirement", fmt.Sprintf("%s-rule-%d", m.ID, i+1), CiteRule, rulesPath, r)
	}
	for i, o := range m.BRD.OutOfScope {
		fmt.Fprintf(&rules, "- OUT OF SCOPE: %s\n", o)
		cite("Requirement", fmt.Sprintf("%s-out_of_scope-%d", m.ID, i+1), CiteBoundary, rulesPath, o)
	}
	add(rulesPath, KindRules, rules.String())

	// THE REVIEW PACKET. The design and the rubric, and nothing else.
	//
	// This is D-069 clause 3 made mechanical: the reviewer's packet carries the
	// design artifact and the domain rules, and the author's brief, goal,
	// contract, handoff, failure lesson, operator answer, stories and
	// acceptance criteria are all withheld.
	if kind == TaskKindReview {
		const designPath = ".gaos/packet/design.md"
		if t := s.taskByIDLocked(taskID); t != nil {
			if designID := s.gateDesignDepLocked(t); designID != "" {
				if art, ok := s.designArtifactFor(designID); ok {
					add(designPath, KindDesign, art.Body)
					cite("Artifact", art.ID, CiteDesign, designPath, art.Body)
				}
			}
		}
		return pkt
	}

	const recordPath = ".gaos/packet/context/record.md"
	var ctxb strings.Builder
	for _, st := range m.Stories {
		fmt.Fprintf(&ctxb, "### %s (%s) — as %s, I want %s so %s\n", st.ID, st.Pri, st.Role, st.Want, st.So)
		cite("Issue", m.ID+"-"+st.ID, CiteStory, recordPath, st.Want+st.So)
		for i, c := range st.AC {
			fmt.Fprintf(&ctxb, "- %s\n", c)
			cite("AcceptanceCriterion", fmt.Sprintf("%s-%s-ac-%d", m.ID, st.ID, i+1), CiteCriterion, recordPath, c)
		}
	}
	for _, a := range m.Decisions {
		fmt.Fprintf(&ctxb, "### %s — %s (confidence %.2f)\n", a.ID, a.Title, a.Confidence)
		cite("Decision", m.ID+"-"+a.ID, CiteDecision, recordPath, a.Title)
	}
	add(recordPath, KindContext, ctxb.String())

	// The implementation task under the gate gets the design it is
	// implementing and the findings that were made about it, so that the
	// implementer sees the review that rejected it (D-069 clause 6).
	//
	// Both are found by the DEPENDS_ON edge: the design is the artifact of the
	// design task the review depends on, and the findings are the artifact of
	// the review task this task depends on. No new field on the task carries
	// either, because the edge is the gate.
	if kind == TaskKindImplement {
		if t := s.taskByIDLocked(taskID); t != nil {
			if reviewID := s.gateReviewDepLocked(t); reviewID != "" {
				if art, ok := s.reviewArtifactFor(reviewID); ok {
					const path = ".gaos/packet/context/findings.md"
					add(path, KindContext, art.Body)
					cite("Artifact", art.ID, CiteLesson, path, art.Body)
				}
				if designID := s.gateDesignDepLocked(s.taskByIDLocked(reviewID)); designID != "" {
					if art, ok := s.designArtifactFor(designID); ok {
						const path = ".gaos/packet/design.md"
						add(path, KindDesign, art.Body)
						cite("Artifact", art.ID, CiteDesign, path, art.Body)
					}
				}
			}
		}
	}

	// The repository half. Every part is still screened against the
	// allowlist, and sorted so the digest does not depend on who supplied it.
	if s.repoParts != nil {
		repo := s.repoParts(d.ID)
		sort.Slice(repo, func(i, j int) bool { return repo[i].Path < repo[j].Path })
		pkt.Parts = append(pkt.Parts, repo...)
		for _, part := range repo {
			// No vertex id: nothing indexes a file yet, and ArcadeStore writes
			// an edge only for a citation that names a vertex.
			cite("", "", CiteCode, part.Path, part.Body)
		}
	}
	return pkt
}

// taskBriefLocked is the body of task.md for a planned task: what THIS task
// is -- its title, kind and role -- and, when it serves a story, the story and
// its acceptance criteria. Deterministic in its inputs, so two freezes of one
// attempt still produce one digest. Caller holds the lock.
func (s *MemStore) taskBriefLocked(deskID string, t TaskRecord) string {
	var b strings.Builder
	b.WriteString(t.Title)
	fmt.Fprintf(&b, "\n\n- Kind: %s\n", taskKind(t.Kind))
	if t.Role != "" {
		fmt.Fprintf(&b, "- Role: %s\n", t.Role)
	}
	if t.Story == "" {
		return strings.TrimRight(b.String(), "\n")
	}
	if m := s.masterPtr(deskID); m != nil {
		for _, st := range m.Stories {
			if st.ID != t.Story {
				continue
			}
			fmt.Fprintf(&b, "\n## Story %s\nAs %s, I want %s so %s\n", st.ID, st.Role, st.Want, st.So)
			if len(st.AC) > 0 {
				b.WriteString("\n## Acceptance criteria\n")
				for _, c := range st.AC {
					fmt.Fprintf(&b, "- %s\n", c)
				}
			}
			return strings.TrimRight(b.String(), "\n")
		}
	}
	fmt.Fprintf(&b, "- Story: %s\n", t.Story)
	return strings.TrimRight(b.String(), "\n")
}

// tokenEstimate is USES_CONTEXT.tokens, and the name is the whole disclaimer.
//
// Four bytes per token is the usual English rule of thumb and it is wrong for
// code, wrong for Indonesian and wrong for any tokeniser in particular. A real
// count needs the provider's vocabulary, which is a provider shape above the
// adapter seam and BR-33 forbids it; the DDL wants a number for tuning the
// assembler, not for billing. The one that bills is TaskRun.tokensIn, which is
// measured from the tool's own report and carries a basis (BR-31).
func tokenEstimate(body string) int { return (len(body) + 3) / 4 }

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
	return s.result("Task canceled. The product is free for its next turn.", ev...), nil
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

	// Deterministic seating, via the same helper DispatchTask uses.
	//
	// This used to be cand[s.rng.IntN(len(cand))], picking among the agents
	// holding the stage's role. That was survivable while one process owned the
	// floor. It stops being survivable at D-1, which requires two replicas
	// computing from the same graph to reach the same answer: a reseat drawn
	// from a private rng makes the two disagree about who is busy, and every
	// capability decision downstream inherits the disagreement. fixtures.go says
	// the same thing about agentForStage in its own comment -- "a dispatch that
	// seated a different agent on every replay would make the fold disagree with
	// the run" -- and this was the one path still doing it the other way.
	if a := agentForStage(d.StageIdx); a != nil {
		d.Agents = []string{a.ID}
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
	prio := "medium"
	switch dr.Priority {
	case "low", "high":
		prio = dr.Priority
	}
	s.products = append(s.products, Product{ID: id, Name: name, Repo: repo, Goal: goal})
	s.masters = append(s.masters, Master{
		ID: id, Code: strings.ToUpper(firstN(name, 2)), Owner: owner,
		Priority: prio, Stage: "draft", Repos: []string{repo},
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
// away.
//
// T-042 changed the shape from a line to a graph. It used to hand every task
// the previous one as its single prerequisite, which made "acyclic" true the
// way a chain is trivially acyclic and made DEPENDS_ON a decoration. What it
// produces now:
//
//	planner → architect
//	architect → implement(story)      one per picked story, all parallel
//	implement(story) → verify(story)
//	{verify(story) …} → security      every story's verifier, fanning in
//	security → devops (gated)
//
// The fan-out is what lets two stories be worked at once; the fan-in is what
// makes Deps a slice rather than a field. Neither is expressible in a chain,
// and the security task's prerequisite count is now a function of the record
// rather than a constant.
//
// The ids are plan-local -- t-1, t-2 -- because this function does not know
// which workflow it is planning for and must not, or it would have to read the
// counter and stop being pure. CreateWorkflow qualifies them; see qualify().
func (s *MemStore) PlanWorkflow(ctx context.Context, d WorkflowDraft) ([]PlanTask, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m := s.masterPtr(d.ProductID)
	if m == nil {
		return nil, ErrNotFound
	}
	// BR-07. Below the floor there is no plan, only a list. Refusing here rather
	// than in the handler is what makes the rule hold for every caller: the
	// wizard, the API and ArcadeStore, which reaches this same method through
	// the projection and so cannot drift from it.
	if score := m.ReadinessScore(); score < PlanFloor {
		return nil, &NotReadyError{
			ProductID: m.ID,
			Score:     score,
			Band:      ReadinessBand(score),
			Missing:   m.ReadinessMissing(),
		}
	}
	picked := map[string]bool{}
	for _, id := range d.Stories {
		picked[id] = true
	}

	var plan []PlanTask
	// add is the plan builder. The kind says what the task IS: an ordinary
	// implementation, a design, or a review. M17's gate is the DEPENDS_ON
	// edge between them, so the kind and the edge are written together here.
	add := func(title, role, story string, deps []string, gate bool, kind string) string {
		id := fmt.Sprintf("t-%d", len(plan)+1)
		model := roles[role].Model
		// The reviewer's model is set outright, and NOT through the gate flag:
		// the gate flag means "a human releases this" and is persisted as
		// approvalRequired (store_arcade.go:667). A review that cannot follow
		// the design proves nothing, so a review runs on opus whatever the role
		// would have used.
		if gate || kind == TaskKindReview {
			model = "claude-opus-5"
		}
		plan = append(plan, PlanTask{
			ID: id, Title: title, Role: role, Story: story,
			Deps: deps, Model: model, Gate: gate, Kind: kind,
		})
		return id
	}
	after := func(ids ...string) []string { return ids }

	p1 := add("Read the record and split the goal into tasks", "planner", "", nil, false, TaskKindImplement)
	arch := add("Confirm boundaries against the diagrams and ADRs", "architect", "", after(p1), false, TaskKindImplement)

	// One design/review/implement/verify chain per picked story, each chain
	// hanging off the architect rather than off the previous chain. That is the
	// fan-out: two stories are two independent branches, and the scheduler is
	// free to run both the moment the architect is done.
	//
	// The IMPLEMENT task depends on the REVIEW task, and that edge is the gate
	// (D-069 clause 7): the scheduler holds a task whose prerequisite is not
	// DONE, and the gate is what makes the review DONE.
	var verifiers []string
	for _, st := range m.Stories {
		if !picked[st.ID] {
			continue
		}
		des := add("Design: "+st.Want, "architect", st.ID, after(arch), false, TaskKindDesign)
		rev := add("Review the design: "+st.Want, "qa", st.ID, after(des), false, TaskKindReview)
		impl := add("Implement "+st.Want, "backend", st.ID, after(rev), false, TaskKindImplement)
		ac := "acceptance criteria for " + st.ID
		if len(st.AC) > 0 {
			ac = st.AC[0]
		}
		verifiers = append(verifiers, add("Verify: "+ac, "qa", st.ID, after(impl), false, TaskKindImplement))
	}
	if len(verifiers) == 0 {
		des := add("Design the objective", "architect", "", after(arch), false, TaskKindDesign)
		rev := add("Review the design", "qa", "", after(des), false, TaskKindReview)
		impl := add("Implement the objective", "backend", "", after(rev), false, TaskKindImplement)
		verifiers = append(verifiers, add("Write integration tests", "qa", "", after(impl), false, TaskKindImplement))
	}

	// And the fan-in. Security waits on every verifier there is, so its
	// prerequisite count is a property of the record rather than a number
	// written here.
	sec := add("Review the security and pricing edge cases", "security", "", verifiers, false, TaskKindImplement)
	add("Prepare the rollout and record the lesson", "devops", "", after(sec), d.Gate, TaskKindImplement)
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
	// T-042's teeth, and they are here rather than in PlanWorkflow on purpose:
	// the planner cannot build a cycle, so checking its own output proves
	// nothing. This method takes the plan as an argument, and the Store seam is
	// public -- an API caller may hand it a graph that loops, names a
	// prerequisite outside the plan, or repeats an id.
	//
	// Every refusal in this method happens before the first write. A plan
	// rejected halfway would leave the story coverage bar moved for a workflow
	// that does not exist.
	if err := validatePlan(plan); err != nil {
		return Result{}, err
	}
	idx := s.deskIdxByID(d.ProductID)
	if idx < 0 {
		// D-076: a product made in the back office has no desk, because desks
		// were only ever built for the seeded products. Seat an idle one; the
		// code below activates it. No event records it and the fold skips events
		// for desks it never seated (fold.go), so a parked floor does not show
		// it -- the first runtime desk, and a known gap rather than a hidden one.
		var p *Product
		for i := range s.products {
			if s.products[i].ID == d.ProductID {
				p = &s.products[i]
			}
		}
		if p == nil {
			return Result{}, ErrNotFound
		}
		// In (id length, id) order, the order ArcadeStore.load restores, so a
		// restart does not shift the index of any desk (the desk routes are
		// keyed by it). The seeded desks p-1..p-5 sort first and are untouched.
		nd := Desk{ID: p.ID, Name: p.Name, Repo: p.Repo, Goal: p.Goal, State: StateIdle}
		idx = len(s.desks)
		for i, d := range s.desks {
			if len(d.ID) > len(nd.ID) || (len(d.ID) == len(nd.ID) && d.ID > nd.ID) {
				idx = i
				break
			}
		}
		s.desks = append(s.desks, Desk{})
		copy(s.desks[idx+1:], s.desks[idx:])
		s.desks[idx] = nd
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

	dk := &s.desks[idx]
	s.workflowSeq++
	wf := fmt.Sprintf("wf-%d", s.workflowSeq)

	// The plan becomes the graph. Ids are qualified here because two plans both
	// numbering from t-1 collide under Task (tenantId, id) UNIQUE, and the
	// second one would upsert over the first taking its DEPENDS_ON edges along.
	//
	// Every task lands PENDING, including the ones with no prerequisites. READY
	// is the scheduler's word (D-1, T-048) and writing it here would be the
	// planner making a scheduling decision it has no inputs for -- no lease, no
	// agent capacity, no budget zone. PENDING is also the DDL's own default.
	graph := qualify(wf, plan)
	for i, t := range graph {
		s.tasks = append(s.tasks, TaskRecord{
			ID: t.ID, WorkflowID: wf, DeskID: d.ProductID,
			Title: t.Title, Role: t.Role, Story: t.Story, Model: t.Model,
			Kind:   taskKind(t.Kind),
			Status: StatusPending, Deps: append([]string(nil), t.Deps...),
			Priority: taskPriority(d.Priority), Ordinal: i, Gate: t.Gate,
		})
	}
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
	if s.workflowGoals == nil {
		s.workflowGoals = map[string]string{}
	}
	s.workflowGoals[wf] = d.Goal
	dk.Meeting = nil

	s.log(dk, fmt.Sprintf("planner read the master record · %d stories, %d rules", len(m.Stories), len(m.BRD.Rules)), "p")
	s.log(dk, "workspace "+strings.Replace(wf, "wf", "ws", 1)+" leased", "t")
	// TaskPlanned carries the graph it planned. The reducer does not read it and
	// the floor does not move -- see the case in fold.go -- but "what did the
	// planner produce, at that moment" stops being answerable from the present
	// graph the first time a dependency is rewired, and BR-04 makes the stream
	// the record rather than the current state.
	planned := deskPayload(*dk)
	for _, t := range graph {
		planned.Tasks = append(planned.Tasks, PlannedTask{
			ID: t.ID, Title: t.Title, Role: t.Role,
			Deps:  append([]string(nil), t.Deps...),
			Story: t.Story, Model: t.Model, Gate: t.Gate, Priority: taskPriority(d.Priority),
			Kind: taskKind(t.Kind),
		})
	}
	ev := []Event{
		s.emit(WorkflowCreated, dk.ID, d.Goal, KindInfo, deskPayload(*dk)),
		s.emit(TaskPlanned, dk.ID, fmt.Sprintf("%d tasks planned from the product master", len(plan)), KindKnow, planned),
	}

	// M17: an implementation task under the gate is BLOCKED from the moment the
	// graph is written, and the reason says why.
	//
	// The DEPENDS_ON edge is what HOLDS it -- the scheduler refuses a task
	// whose prerequisite is not DONE (scheduler.go:363-376). This is what SAYS
	// so: without it the operator sees a card that is merely waiting, and the
	// waiting is not the ordinary kind, because what it waits for is a review
	// nobody has been dispatched to write yet.
	for i := range s.tasks {
		t := &s.tasks[i]
		// Only the tasks of THIS workflow. The loop used to walk every task of
		// every product, so planning one product re-blocked another's gated
		// task -- in flight or not -- and overwrote its BlockReason (found by
		// the QA-sync design review, round 3).
		if t.WorkflowID != wf {
			continue
		}
		if taskKind(t.Kind) != TaskKindImplement {
			continue
		}
		if s.gateReviewDepLocked(t) == "" {
			continue
		}
		t.BlockReason = ReasonDesignUnreviewed
		if m, ok := s.moveTaskLocked(t.ID, StatusBlocked, RuleGateUnwired); ok {
			ev = append(ev, m)
		}
	}
	s.focusIdx = idx
	s.replayAtSeq = liveMode
	return s.result("Workflow created. The desk is live on the floor.", ev...), nil
}

// taskPriority maps the wizard's three words onto Task.priority, which
// 04-execution.sql declares INTEGER with min 0 and max 100 and a default of 50.
// Three points on a scale of a hundred rather than a scale of three, because
// the column is the one that exists and T-044's routing will want room between
// the words later.
func taskPriority(word string) int {
	switch word {
	case "high":
		return 75
	case "low":
		return 25
	default:
		return 50
	}
}

// tasksFor returns the graph for one desk, in plan order. Assumes the lock is
// held, same as every other private helper here.
//
// Copies, not references: TaskRecord carries a slice, and handing a caller the
// live one is the aliasing bug the top of this file is written about.
func (s *MemStore) tasksFor(deskID string) []TaskRecord {
	var out []TaskRecord
	for _, t := range s.tasks {
		if t.DeskID == deskID {
			out = append(out, t.Clone())
		}
	}
	return out
}

// schedulerInput is the impure shell around Schedule. T-048.
//
// Everything the decision may read is gathered here, under the lock, and handed
// down as a value -- including the clock, which is why now is a parameter
// rather than a time.Now() call inside. The caller reads the clock ONCE per
// pass and that read is the only one on the whole path; two tasks in one
// decision compared against two different "now"s would be a scheduler that
// disagreed with itself, never mind with a replica.
//
// Budget is BudgetNormal unconditionally, and will be until M10.3 exists. That
// is honest rather than optimistic: there is no guard, so there is no zone to
// report, and inventing a throttled one here would put a refusal in the
// timeline that no limit produced.
//
// The lease is the graph lease (BR-38): one DISPATCHED or RUNNING task holds
// its product, and the scheduler reads it from the tasks themselves.
func (s *MemStore) schedulerInput(now int64) SchedulerInput {
	in := s.schedulerInputLocked(now)

	return in
}

func (s *MemStore) schedulerInputLocked(now int64) SchedulerInput {
	s.mu.RLock()
	defer s.mu.RUnlock()

	in := SchedulerInput{
		Readiness: make(map[string]int, len(s.masters)),
		Budget:    BudgetNormal,
		Now:       now,
	}
	for _, t := range s.tasks {
		in.Tasks = append(in.Tasks, TaskNode{
			ID: t.ID, WorkflowID: t.WorkflowID, DeskID: t.DeskID,
			Title: t.Title, Role: t.Role, Model: t.Model, Status: t.Status,
			Deps:     append([]string(nil), t.Deps...),
			Priority: t.Priority, Ordinal: t.Ordinal, Gate: t.Gate,
		})
	}
	for i := range s.masters {
		in.Readiness[s.masters[i].ID] = s.masters[i].ReadinessScore()
	}

	// Busy is a fact about the graph, not about the floor: an agent is busy
	// when it carries a live turn, and a live turn is a task DISPATCHED or
	// RUNNING. Each one holds one agent of its role, named with the live
	// task's desk (AgentSlot.Desk).
	//
	// It used to be read from the desk seats, and that stopped every card on
	// prod (v0.40.1): a seat is the floor's display, not a run. The seeded
	// desks seat a-1, the only architect, at p-1 and p-3 and stay active with
	// nothing running them; CreateWorkflow seats a-9, the only planner, on
	// every desk it activates. An agent seated at two live desks read as busy
	// everywhere, so every architect and planner task waited on a run that did
	// not exist and that nothing would ever finish.
	in.Agents = liveAgentSlots(s.tasks, agents)
	return in
}

// liveAgentSlots marks one agent busy per live task, deterministically: live
// tasks in id order, each taking the first agent of its role (in id order) not
// already taken. A live task whose role has no free agent left takes nothing --
// the capacity is already spent, and the graph lease still holds its product.
func liveAgentSlots(tasks []TaskRecord, roster []Agent) []AgentSlot {
	var live []TaskRecord
	for _, t := range tasks {
		if t.Status == StatusDispatched || t.Status == StatusRunning {
			live = append(live, t)
		}
	}
	sort.Slice(live, func(i, j int) bool { return live[i].ID < live[j].ID })
	slots := make([]AgentSlot, 0, len(roster))
	for _, a := range roster {
		slots = append(slots, AgentSlot{ID: a.ID, Role: a.Role})
	}
	order := make([]int, len(slots))
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(i, j int) bool { return slots[order[i]].ID < slots[order[j]].ID })
	for _, t := range live {
		for _, i := range order {
			if slots[i].Role == t.Role && !slots[i].Busy {
				slots[i].Busy, slots[i].Desk = true, t.DeskID
				break
			}
		}
	}
	return slots
}

// tasksSnapshot is tasksFor with the lock taken, for callers reaching into the
// projection from outside its own helpers. ArcadeStore.persist is the one, and
// it reaches in the same way foldAt already lets it.
func (s *MemStore) tasksSnapshot(deskID string) []TaskRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tasksFor(deskID)
}

// runsSnapshot is every attempt on one desk, with the slice each was given.
//
// The slices come back beside the runs rather than being fetched one at a time
// because two attempts commonly share one, and the persist path writes both in
// a single transaction: a TaskRun whose ASSEMBLED edge points at a ContextSlice
// that failed to write would be an attempt naming context nobody can read.
func (s *MemStore) runsSnapshot(deskID string) ([]TaskRunRecord, map[string]SliceRecord) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var runs []TaskRunRecord
	slices := map[string]SliceRecord{}
	for _, r := range s.runs {
		if r.DeskID != deskID {
			continue
		}
		runs = append(runs, r.Clone())
		if rec, ok := s.slices[r.SliceDigest]; ok {
			slices[r.SliceDigest] = rec.Clone()
		}
	}
	// Every slice frozen for this desk, including ones no attempt references.
	// A dispatch that cleared with no runner configured froze a real slice and
	// spawned nothing; leaving it out of the write would mean the projection
	// could answer /tasks/{id}/context and a restarted process could not, which
	// is BR-01's cache-must-be-reconstructible rule failing quietly.
	for digest, rec := range s.slices {
		if rec.DeskID == deskID {
			slices[digest] = rec.Clone()
		}
	}
	sort.SliceStable(runs, func(i, j int) bool { return runs[i].ID < runs[j].ID })
	return runs, slices
}

// artifactsSnapshot is one desk's handoff documents, in id order. T-064.
func (s *MemStore) artifactsSnapshot(deskID string) []ArtifactRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var out []ArtifactRecord
	for _, a := range s.artifacts {
		if a.DeskID == deskID {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
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
