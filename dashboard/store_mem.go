package main

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"
	"sync"
)

// maxEvents caps the stream, same as the prototype. The cap is the reason
// replay is bounded: scrub past the oldest retained event and there is nothing
// to fold back to. When the Event document type lands this becomes a retention
// policy (90 days full fidelity) rather than a ring buffer.
const maxEvents = 160

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
	events   []Event
	seq      int

	spend  float64
	notifs int

	focusIdx   int
	replayAt   int
	meetingIdx int

	paused bool
	speed  int
	simOn  bool
}

// NewMemStore builds a store from the fixtures. The seed is a parameter so the
// tests can pin the simulation: every branch in sim.go is a probability, and a
// test that cannot reproduce a sequence cannot assert on one.
func NewMemStore(seed uint64) *MemStore {
	s := &MemStore{
		rng:        rand.New(rand.NewPCG(seed, seed^0x9E3779B97F4A7C15)),
		replayAt:   liveMode,
		meetingIdx: -1,
		speed:      1,
	}

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

	// The boot sequence from the prototype: one FloorOpened event, then a
	// runner line on every seated desk, so the inspector is not empty on the
	// first paint.
	s.emit(FloorOpened, s.desks[0].ID,
		fmt.Sprintf("Mission Control attached to %d workflows", len(s.desks)), KindInfo)
	for i := range s.desks {
		if s.desks[i].State != StateIdle {
			s.log(&s.desks[i], fmt.Sprintf("runner attached · pid %d", 11000+s.rng.IntN(900)), "t")
		}
	}
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

func (s *MemStore) log(d *Desk, line, cls string) {
	if cls == "" {
		cls = "t"
	}
	d.Logs = append(d.Logs, LogLine{Line: line, Cls: cls, T: s.clock})
	if len(d.Logs) > 40 {
		d.Logs = d.Logs[len(d.Logs)-40:]
	}
}

// emit writes an event AND the full floor snapshot it describes.
//
// This is the SSE seam. When the broker lands in pass 2 it publishes from
// exactly here, and nothing else changes: handlers already render from the
// events this returns rather than re-reading the store.
func (s *MemStore) emit(t EventType, deskID, text, kind string) Event {
	s.seq++
	e := Event{
		Seq: s.seq, Type: t, DeskID: deskID, Text: text, Kind: kind,
		T: s.clock, Snapshot: cloneDesks(s.desks),
	}
	if kind == KindWarn || kind == KindFail || t == ApprovalRequested {
		s.notifs++
	}
	s.events = append(s.events, e)
	if len(s.events) > maxEvents {
		drop := len(s.events) - maxEvents
		s.events = s.events[drop:]
		// The replay cursor is an index into this slice. Shifting the window
		// without shifting the cursor silently moves the operator to a
		// different moment than the one they clicked.
		if s.replayAt != liveMode {
			s.replayAt -= drop
			if s.replayAt < 0 {
				s.replayAt = 0
			}
		}
	}
	return e
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
		Replay:   s.replayAt != liveMode,
		ReplayAt: s.replayAt,
		Paused:   s.paused, Speed: s.speed, SimRunning: s.simOn,
	}
}

// floor is one consistent read. Which desks you see depends on the replay
// cursor: live reads the live slice, replay reads the snapshot frozen into the
// event. The rail and the topbar always describe LIVE state -- a replayed floor
// with a replayed room meter would look like the present and be the past.
func (s *MemStore) floor() Floor {
	view := s.desks
	if s.replayAt != liveMode && s.replayAt < len(s.events) {
		view = s.events[s.replayAt].Snapshot
	}
	f := Floor{Desks: cloneDesks(view), Stats: s.stats(), FocusIdx: s.focusIdx}
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
	view := s.desks
	if s.replayAt != liveMode && s.replayAt < len(s.events) {
		view = s.events[s.replayAt].Snapshot
	}
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

func (s *MemStore) Events(ctx context.Context) ([]Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	// Snapshots are deliberately not copied out: the timeline renders only
	// type, kind, text and time, and copying 160 floors per poll is the kind of
	// cost that only shows up under load.
	out := make([]Event, len(s.events))
	for i, e := range s.events {
		e.Snapshot = nil
		out[i] = e
	}
	return out, nil
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

func (s *MemStore) Replay(ctx context.Context, eventIdx int) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if eventIdx < 0 {
		s.replayAt = liveMode
		return s.result(""), nil
	}
	if eventIdx >= len(s.events) {
		return Result{}, ErrNotFound
	}
	s.replayAt = eventIdx
	if hit := s.deskIdxByID(s.events[eventIdx].DeskID); hit >= 0 {
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
	e := s.emit(ApprovalGranted, d.ID, "Merge gate released on "+taskIDOf(*d), KindDone)
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
	d.Retry++
	d.State = StateActive
	d.Progress = 6
	d.Scored, d.Score, d.ScoreParts = false, 0, nil
	s.log(d, "manual retry · context widened by one hop", "y")
	e := s.emit(RetryRequested, d.ID, "Operator retry on "+taskIDOf(*d), KindWarn)
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
	d.State = StateIdle
	d.Agents = nil
	d.Progress = 0
	d.ApprovalRequired = false
	s.log(d, "task canceled · workspace released", "r")
	e := s.emit(TaskCanceled, d.ID, "Operator canceled "+taskIDOf(*d), KindFail)
	return s.result("Task canceled. The workspace lease is released.", e), nil
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
	if s.replayAt != liveMode {
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
	d.State = StateMeeting
	d.Meeting = &Meeting{Reason: reason, Participants: participants, Conf: 38}
	s.meetingIdx = idx
	return []Event{s.emit(MeetingStarted, d.ID, reason, KindMeet)}
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
	ev = append(ev, s.emit(DecisionRecorded, d.ID, "ADR-014 accepted · 2 action items created", KindKnow))
	d.State = StateActive
	d.Esc++
	d.Retry = 0
	d.Progress = 8
	d.Scored, d.Score, d.ScoreParts = false, 0, nil
	d.Task = "Apply ADR-014: move rounding into the pricing service"
	d.Meeting = nil
	s.log(d, "meeting decision applied · model upgraded to claude-opus-5", "p")
	ev = append(ev, s.emit(ModelEscalated, d.ID, "Retry running on a stronger model after the review", KindKnow))
	ev = append(ev, s.emit(MeetingClosed, d.ID, "Design review closed", KindMeet))
	return ev
}

// advance moves a workflow to the next stage, or completes it. Assumes the lock
// is held. This is the only place a desk changes stage, which is what keeps the
// stage ribbon and the seated role from disagreeing.
func (s *MemStore) advance(d *Desk) []Event {
	d.TaskSeq++
	if d.StageIdx >= len(stages)-1 {
		d.State = StateCompleted
		d.Progress = 100
		s.log(d, "knowledge node written · linked to task, files, ADR", "p")
		return []Event{
			s.emit(KnowledgeExtracted, d.ID, "Lesson stored for "+d.Name, KindKnow),
			s.emit(WorkflowCompleted, d.ID, d.Name+" shipped", KindDone),
		}
	}
	d.StageIdx++
	d.Progress = 4
	d.State = StateActive
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
	return []Event{s.emit(TaskDispatched, d.ID, stageOf(*d)+": "+d.Task, KindInfo)}
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
	e := s.emit(ProductCreated, id, name+" record created", KindInfo)
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
	e := s.emit(RecordUpdated, id, s.productName(id)+": requirements updated", KindInfo)
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
	e := s.emit(RecordUpdated, id, s.productName(id)+": "+st.ID+" added", KindInfo)
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
	e := s.emit(RecordUpdated, id, s.productName(id)+": "+g.ID+" added", KindInfo)
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
	e := s.emit(RecordUpdated, id, s.productName(id)+": "+a.ID+" recorded", KindKnow)
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
	e := s.emit(KnowledgeInjected, id, text, KindKnow)
	if idx := s.deskIdxByID(id); idx >= 0 {
		s.log(&s.desks[idx], fmt.Sprintf("master record injected · %d vertices, %d edges", pr.Vertices, pr.Edges), "p")
	}
	r := s.result(fmt.Sprintf("Injected %d vertices and %d edges into the graph.", pr.Vertices, pr.Edges), e)
	c := m.Clone()
	r.Master = &c
	return r, nil
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
	dk.State = StateActive
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
		s.emit(WorkflowCreated, dk.ID, d.Goal, KindInfo),
		s.emit(TaskPlanned, dk.ID, fmt.Sprintf("%d tasks planned from the product master", len(plan)), KindKnow),
	}
	s.focusIdx = idx
	s.replayAt = liveMode
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
