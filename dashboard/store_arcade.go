package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// ArcadeStore is the ArcadeDB implementation of Store -- T-006, the file
// store.go has been promising since it was written. store.go DID NOT CHANGE to
// let this land, which was the whole point of declaring the seam early.
//
// # Where the state actually lives
//
// Be precise about this, because the invariant it serves is BR-01 and the
// honest reading matters more than the flattering one.
//
// The graph is authoritative. Every mutation writes to ArcadeDB before it is
// visible as committed, and every mutation writes an Event -- BR-02, an event
// that was never written is a state change that never happened. On boot the
// whole floor is REBUILT from the graph; nothing survives in this process that
// the database could not reproduce.
//
// Reads are served from an in-memory projection of that graph. BR-01 permits
// exactly this and says so in its own words: "The orchestrator holds no
// authoritative state; anything cached in memory must be reconstructible." The
// projection is a read model, not a second source of truth, and the test that
// keeps it honest is the one in the conformance suite that restarts the store
// against the same database and expects the same floor back.
//
// # Why it reuses MemStore's transitions rather than reimplementing them
//
// The domain rules -- what a retry does to the guard counter, which desk a
// meeting seats, when a workflow advances a stage -- are the same rules
// whichever store holds the bytes. Writing them twice would produce two
// dialects of the same domain that agree only until someone edits one, and
// T-008 exists precisely to stop that drift. So the projection IS a MemStore,
// the transitions run there, and this file's job is the part that is genuinely
// different: reading the floor out of the graph, and writing it back.
//
// M0.3 and M0.5 rewrite those transitions into a pure reducer and a real state
// machine. When they do, both stores inherit the change at once, which is the
// property this arrangement is buying.
type ArcadeStore struct {
	arc    *Arcade
	tenant string

	// One lock over "apply the transition, then persist it". The projection has
	// its own lock for its own consistency, but that one cannot span the write
	// to the database, and a second operator reading between the two would see
	// a floor the graph does not yet agree with.
	mu   sync.Mutex
	proj *MemStore
}

// ArcadeStoreDefaultTenant is the tenant every vertex and event is written
// under when GAOS_ARCADE_TENANT is unset.
//
// One tenant per store instance, for now. The schema demands a tenantId on
// every record and indexes Event by (tenantId, seq); the Go domain types carry
// no tenant at all. Reconciling those two properly is D-040's precondition on
// further Mission Control work and is not this pass.
const ArcadeStoreDefaultTenant = "loop-eng"

// NewArcadeStore applies the schema, then either rebuilds the floor from the
// graph or -- on a database that has never held one -- writes the fixtures into
// it once.
//
// fixtures.go predicted its own future here: "When store_arcade.go lands, this
// file becomes the bootstrap fixture: the same records, written into the graph
// once." That is what the seeding branch below is. It is also what makes one
// conformance suite meaningful: both implementations start from the same
// records, so a case that passes against one and fails against the other is a
// real disagreement rather than a difference in seed data.
func NewArcadeStore(ctx context.Context, arc *Arcade, seed uint64) (*ArcadeStore, error) {
	return NewArcadeStoreFor(ctx, arc, seed, envOr("GAOS_ARCADE_TENANT", ArcadeStoreDefaultTenant))
}

// NewArcadeStoreFor is NewArcadeStore with the tenant named rather than read
// from the environment. The conformance suite uses it to give every case its
// own world inside one database -- which is both faster than applying the
// schema per case and a small standing check that the tenant scoping works,
// since a case that leaked would see another case's desks.
func NewArcadeStoreFor(ctx context.Context, arc *Arcade, seed uint64, tenant string) (*ArcadeStore, error) {
	if !arc.Configured() {
		return nil, ErrUnconfigured
	}
	s := &ArcadeStore{arc: arc, tenant: tenant}

	if _, _, err := arc.ApplySchema(ctx); err != nil {
		return nil, fmt.Errorf("schema: %w", err)
	}
	if err := s.ensureTenant(ctx); err != nil {
		return nil, err
	}

	// The flush hook, before anything can write. Every mutation a handler makes
	// is persisted by mutate(); a BACKGROUND writer -- the runner, the
	// reconciler -- has no request behind it and no mutate() around it, so its
	// events would live and die in the projection without this.
	//
	// context.Background rather than a request's, because these writes belong to
	// a run that outlives whatever asked for it. That is the whole of D-2.
	installHook := func(m *MemStore) {
		m.SetOnWrite(func(r Result) {
			if err := s.persistBackground(context.Background(), r); err != nil {
				log.Printf("background write did not reach the graph: %v", err)
			}
		})
	}

	seeded, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	if !seeded {
		s.proj = NewMemStore(seed)
		if err := s.persistAll(ctx); err != nil {
			return nil, fmt.Errorf("seed: %w", err)
		}
	}
	installHook(s.proj)
	return s, nil
}

func (s *ArcadeStore) Kind() string { return "arcadedb" }

// Ready issues the probe against the database, which is exactly what store.go
// said this method would do when the ArcadeDB store landed -- and the reason
// handleHealth needed no change to report it.
func (s *ArcadeStore) Ready(ctx context.Context) error {
	_, err := s.arc.Probe(ctx)
	return err
}

// ---------------------------------------------------------------- reads
//
// Every read is served from the projection, under the projection's own lock.
// They are one-liners on purpose: a read that reached for the database would
// make the page render depend on a network round-trip per fragment, and HTMX
// asks for several fragments per interaction.

func (s *ArcadeStore) Snapshot(ctx context.Context) (Floor, error) { return s.proj.Snapshot(ctx) }
func (s *ArcadeStore) Desk(ctx context.Context, i int) (Desk, int, error) {
	return s.proj.Desk(ctx, i)
}
func (s *ArcadeStore) Agents(ctx context.Context) ([]Agent, error)     { return s.proj.Agents(ctx) }
func (s *ArcadeStore) Products(ctx context.Context) ([]Product, error) { return s.proj.Products(ctx) }
func (s *ArcadeStore) Master(ctx context.Context, id string) (Master, error) {
	return s.proj.Master(ctx, id)
}
func (s *ArcadeStore) Masters(ctx context.Context) ([]Master, error) { return s.proj.Masters(ctx) }
func (s *ArcadeStore) Events(ctx context.Context) ([]Event, error)   { return s.proj.Events(ctx) }
func (s *ArcadeStore) ContextSlice(ctx context.Context, i int) ([]string, error) {
	return s.proj.ContextSlice(ctx, i)
}
func (s *ArcadeStore) PlanWorkflow(ctx context.Context, d WorkflowDraft) ([]PlanTask, error) {
	return s.proj.PlanWorkflow(ctx, d)
}

// ---------------------------------------------------------------- view state
//
// Focus, Replay, pause and speed change nothing in the graph, and there is no
// vertex type for them because they are not domain state -- they are where one
// operator is currently looking. Persisting them would make two operators fight
// over a single cursor.

func (s *ArcadeStore) Focus(ctx context.Context, i int) (Result, error) { return s.proj.Focus(ctx, i) }
func (s *ArcadeStore) Replay(ctx context.Context, i int) (Result, error) {
	return s.proj.Replay(ctx, i)
}
func (s *ArcadeStore) SetPaused(ctx context.Context, p bool) (Result, error) {
	return s.proj.SetPaused(ctx, p)
}
func (s *ArcadeStore) SetSpeed(ctx context.Context, n int) (Result, error) {
	return s.proj.SetSpeed(ctx, n)
}
func (s *ArcadeStore) ClearNotifications(ctx context.Context) (Result, error) {
	return s.proj.ClearNotifications(ctx)
}

// ---------------------------------------------------------------- mutations
//
// All of them are the same two steps: run the transition against the
// projection, and if it was accepted, write what it produced. A rejected
// mutation touches the database not at all, which is what makes reject() cheap
// enough to use as the first line of every one of these.

// SetAdapter forwards to the projection, which is where DispatchTask runs. Not
// on the Store interface: installing the model gateway is a boot-time act by
// serve(), not something a handler may do, and putting it on the seam would
// invite a request-scoped caller.
func (s *ArcadeStore) SetAdapter(a Adapter) { s.proj.SetAdapter(a) }

// persistBackground writes what a runner or the reconciler produced.
//
// It takes the store's own lock, the same one mutate() holds, because the two
// write to the same tables and a runner finishing mid-request would otherwise
// interleave with the handler that request is serving.
//
// A failure is logged rather than returned, and that is a real limitation
// stated rather than hidden: the caller is a goroutine nobody is waiting on, so
// there is nothing to return an error TO. BR-02 says an event that was never
// written is a state change that never happened, and a background write that
// failed is exactly that -- which is why boot reconciliation exists, and why
// the log line is worded so a human reading it knows the graph is behind.
func (s *ArcadeStore) persistBackground(ctx context.Context, r Result) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.persist(ctx, r)
}

// Reconcile is boot recovery. T-053.
//
// NOT wrapped in mutate, and the reason is worth reading before somebody
// "fixes" it: the projection publishes each recovery event through the onWrite
// hook as it goes, and that hook takes this store's lock. mutate holds the same
// lock for the whole callback, so wrapping this would deadlock the boot -- the
// dashboard would come up, print its adapter line, and never listen. That is
// exactly how it was found.
func (s *ArcadeStore) Reconcile(ctx context.Context) (Result, error) {
	return s.proj.Reconcile(ctx)
}

// RunOnce is one pass of the control loop. Not wrapped in mutate: what it
// starts runs in goroutines that finish long after this returns, and each one
// persists itself through the hook SetAdapter installed.
func (s *ArcadeStore) RunOnce(ctx context.Context, ad Adapter) ScheduleDecision {
	return s.proj.RunOnce(ctx, ad)
}

// setEgressForTest forwards to the projection, so the conformance suite proves
// the same refusal against both stores. See MemStore.setEgressForTest.
func (s *ArcadeStore) setEgressForTest(p EgressPolicy, tgt Target) {
	s.proj.setEgressForTest(p, tgt)
}

func (s *ArcadeStore) DispatchTask(ctx context.Context, i int) (Result, error) {
	return s.mutate(ctx, func() (Result, error) { return s.proj.DispatchTask(ctx, i) })
}
func (s *ArcadeStore) ApproveTask(ctx context.Context, i int) (Result, error) {
	return s.mutate(ctx, func() (Result, error) { return s.proj.ApproveTask(ctx, i) })
}
func (s *ArcadeStore) RetryTask(ctx context.Context, i int) (Result, error) {
	return s.mutate(ctx, func() (Result, error) { return s.proj.RetryTask(ctx, i) })
}
func (s *ArcadeStore) CancelTask(ctx context.Context, i int) (Result, error) {
	return s.mutate(ctx, func() (Result, error) { return s.proj.CancelTask(ctx, i) })
}
func (s *ArcadeStore) CallReview(ctx context.Context, i int) (Result, error) {
	return s.mutate(ctx, func() (Result, error) { return s.proj.CallReview(ctx, i) })
}
func (s *ArcadeStore) CloseMeeting(ctx context.Context) (Result, error) {
	return s.mutate(ctx, func() (Result, error) { return s.proj.CloseMeeting(ctx) })
}
func (s *ArcadeStore) CreateProduct(ctx context.Context, d ProductDraft) (Result, error) {
	return s.mutate(ctx, func() (Result, error) { return s.proj.CreateProduct(ctx, d) })
}
func (s *ArcadeStore) UpdateBRD(ctx context.Context, id string, b BRD) (Result, error) {
	return s.mutate(ctx, func() (Result, error) { return s.proj.UpdateBRD(ctx, id, b) })
}
func (s *ArcadeStore) AddStory(ctx context.Context, id string, st Story) (Result, error) {
	return s.mutate(ctx, func() (Result, error) { return s.proj.AddStory(ctx, id, st) })
}
func (s *ArcadeStore) AddDiagram(ctx context.Context, id string, g Diagram) (Result, error) {
	return s.mutate(ctx, func() (Result, error) { return s.proj.AddDiagram(ctx, id, g) })
}
func (s *ArcadeStore) AddDecision(ctx context.Context, id string, a Decision) (Result, error) {
	return s.mutate(ctx, func() (Result, error) { return s.proj.AddDecision(ctx, id, a) })
}
func (s *ArcadeStore) CreateWorkflow(ctx context.Context, d WorkflowDraft, plan []PlanTask) (Result, error) {
	return s.mutate(ctx, func() (Result, error) { return s.proj.CreateWorkflow(ctx, d, plan) })
}

// Inject is the one mutation that does more in this store than in MemStore.
//
// store_mem.go's own comment says so: "In this pass it emits the event and
// reports the projection; the write itself lands with store_arcade.go." This is
// where it lands. The master record is a document while a human edits it --
// D-009, schema-less where humans write -- and injection is the moment it
// becomes a subgraph, which is what AddStory's toast has been promising the
// operator all along: "It becomes an Issue vertex on the next injection."
//
// The counts in the toast still come from Master.Project(), unchanged, so the
// number the back office promised and the number the injection reports are the
// same number in both stores.
func (s *ArcadeStore) Inject(ctx context.Context, id string) (Result, error) {
	return s.mutate(ctx, func() (Result, error) {
		r, err := s.proj.Inject(ctx, id)
		if err != nil {
			return r, err
		}
		m, err := s.proj.Master(ctx, id)
		if err != nil {
			return r, err
		}
		if err := s.injectSubgraph(ctx, m); err != nil {
			return Result{}, err
		}
		return r, nil
	})
}

// mutate runs a transition and persists whatever it produced.
func (s *ArcadeStore) mutate(ctx context.Context, apply func() (Result, error)) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	r, err := apply()
	if err != nil {
		return Result{}, err
	}
	if err := s.persist(ctx, r); err != nil {
		return Result{}, err
	}
	return r, nil
}

// persist writes one mutation's consequences: the events it emitted, the
// snapshot that lets a replay find its way back, and the records it changed.
//
// One transaction, because a sequence number allocated without the event that
// claims it would leave a hole in a stream BR-04 says is append-only, and an
// event written without the state change it describes is worse.
func (s *ArcadeStore) persist(ctx context.Context, r Result) error {
	tx, err := s.arc.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	for _, e := range r.NewEvents {
		if err := s.writeEvent(ctx, tx, e); err != nil {
			return err
		}
	}
	for _, d := range r.Floor.Desks {
		if err := s.writeDesk(ctx, tx, d); err != nil {
			return err
		}
		if err := s.writeTasks(ctx, tx, d.ID); err != nil {
			return err
		}
	}
	if r.Master != nil {
		// The Product vertex first. writeMaster sets code, owner and the record
		// document, but Product.name is mandatory and only the Product carries
		// it -- CreateProduct would otherwise try to upsert a nameless vertex
		// and be refused at commit.
		if p, ok := s.productByID(ctx, r.Master.ID); ok {
			if err := s.writeProduct(ctx, tx, p); err != nil {
				return err
			}
		}
		if err := s.writeMaster(ctx, tx, *r.Master); err != nil {
			return err
		}
	}
	// A snapshot every snapshotEvery events, not one per event.
	//
	// The rung is keyed to the LAST event in the batch rather than to whichever
	// event happened to be the 500th, because r.Floor is the floor after the
	// whole mutation and uptoSeq has to name the event it actually reflects. A
	// rung landing at 502 instead of 500 costs nothing -- uptoSeq is explicit,
	// so the fold knows precisely what is already in there -- whereas a rung
	// claiming 500 and holding 502 would be a snapshot that lies.
	if n := len(r.NewEvents); n > 0 {
		last := r.NewEvents[n-1]
		if last.Seq/snapshotEvery > (r.NewEvents[0].Seq-1)/snapshotEvery {
			if err := s.writeSnapshot(ctx, tx, last.Seq, s.proj.foldAt(last.Seq)); err != nil {
				return err
			}
		}
	}
	return tx.Commit(ctx)
}

// persistAll writes the whole seeded world. Only ever runs once per database.
func (s *ArcadeStore) persistAll(ctx context.Context) error {
	f, err := s.proj.Snapshot(ctx)
	if err != nil {
		return err
	}
	products, _ := s.proj.Products(ctx)
	masters, _ := s.proj.Masters(ctx)
	// rawEvents, not Events: the seam strips payloads on the way out and the
	// payloads are the half a fold needs. Writing the stripped version would
	// store a stream that replays to a blank floor.
	events := s.proj.rawEvents()

	tx, err := s.arc.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	for _, a := range agents {
		// availability, not status: the DDL names it that and constrains it to
		// six values. model is notnull, and the role's model is the one the
		// planner would pick for it.
		if err := s.upsert(ctx, tx, "Agent", a.ID, map[string]any{
			"name": a.Name, "role": a.Role,
			"model": roles[a.Role].Model, "availability": "IDLE",
		}); err != nil {
			return err
		}
	}
	for _, rm := range rooms {
		// The room ids in fixtures.go are exactly Room.kind's declared
		// vocabulary -- architecture, backend, qa, release, knowledge, meeting
		// -- so the id IS the kind here rather than a second thing to keep in
		// step with it.
		if err := s.upsert(ctx, tx, "Room", rm.ID, map[string]any{
			"name": rm.Name, "kind": rm.ID, "capacity": rm.Capacity, "note": rm.Note,
		}); err != nil {
			return err
		}
	}
	for _, p := range products {
		if err := s.writeProduct(ctx, tx, p); err != nil {
			return err
		}
	}
	for _, m := range masters {
		if err := s.writeMaster(ctx, tx, m); err != nil {
			return err
		}
	}
	for _, d := range f.Desks {
		if err := s.writeDesk(ctx, tx, d); err != nil {
			return err
		}
		if err := s.writeTasks(ctx, tx, d.ID); err != nil {
			return err
		}
	}
	for _, e := range events {
		if err := s.writeEvent(ctx, tx, e); err != nil {
			return err
		}
	}
	// No snapshot rung here. The seeded world is one FloorOpened event, and a
	// rung after the first event would save nothing -- the ladder starts paying
	// for itself at snapshotEvery, and persist writes the rungs from there on.
	return tx.Commit(ctx)
}

// ---------------------------------------------------------------- writes

// tasksSnapshot forwards to the projection, the same one-line way every other
// read that needs no graph query does.
//
// Deliberately NOT on the Store interface. store.go's header records that the
// seam has changed exactly twice and why each time was worth it; the task graph
// does not need a third, because the only callers are persist() above and the
// conformance suite, and the suite already has the named-local-interface
// pattern for reaching past the seam on purpose (see caseDispatchRefusedBlocks).
func (s *ArcadeStore) tasksSnapshot(deskID string) []TaskRecord {
	return s.proj.tasksSnapshot(deskID)
}

func (s *ArcadeStore) productByID(ctx context.Context, id string) (Product, bool) {
	ps, err := s.proj.Products(ctx)
	if err != nil {
		return Product{}, false
	}
	for _, p := range ps {
		if p.ID == id {
			return p, true
		}
	}
	return Product{}, false
}

func (s *ArcadeStore) ensureTenant(ctx context.Context) error {
	return s.upsert(ctx, nil, "Tenant", s.tenant, map[string]any{"name": s.tenant})
}

func (s *ArcadeStore) writeProduct(ctx context.Context, tx *ArcadeTx, p Product) error {
	if err := s.upsert(ctx, tx, "Product", p.ID, map[string]any{
		"name": p.Name, "repo": p.Repo, "goal": p.Goal,
	}); err != nil {
		return err
	}
	return s.edge(ctx, tx, "OWNS", "Tenant", s.tenant, "Product", p.ID)
}

// writeMaster stores the record as a document on the Product vertex.
//
// D-009 is the licence for this and names the boundary exactly: schema-full
// where machines read, schema-less where humans write. A BRD under edit is the
// second of those. The declared properties -- name, code, owner, stage -- are
// set as properties because the readiness rubric will read them; the parts a
// human is still typing ride along as nested documents until Inject turns them
// into the vertices §14 maps them to.
func (s *ArcadeStore) writeMaster(ctx context.Context, tx *ArcadeTx, m Master) error {
	brd, err := jsonMap(m.BRD)
	if err != nil {
		return err
	}
	stories, err := jsonList(m.Stories)
	if err != nil {
		return err
	}
	diagrams, err := jsonList(m.Diagrams)
	if err != nil {
		return err
	}
	decisions, err := jsonList(m.Decisions)
	if err != nil {
		return err
	}
	props := map[string]any{
		"code": m.Code, "owner": m.Owner, "priority": m.Priority,
		"brd": brd, "stories": stories, "diagrams": diagrams,
		"decisions": decisions, "repos": m.Repos,
	}
	if m.Stage != "" {
		props["stage"] = m.Stage
	}
	// A Product the master belongs to always exists by the time this runs --
	// CreateProduct appends both in the same call -- but the master is the only
	// carrier of code and owner, so it must not silently create a nameless
	// vertex if that ever stops being true.
	if err := s.upsert(ctx, tx, "Product", m.ID, props); err != nil {
		return err
	}
	for _, repo := range m.Repos {
		if err := s.upsert(ctx, tx, "Repository", repo, map[string]any{
			"name": repo, "url": repo, "defaultBranch": "main",
		}); err != nil {
			return err
		}
		if err := s.edge(ctx, tx, "USES_REPOSITORY", "Product", m.ID, "Repository", repo); err != nil {
			return err
		}
	}
	return nil
}

// writeDesk persists one desk as the Workflow it is, plus the Task it is
// currently on.
//
// model.go states the mapping this implements: "Desk is a Workflow plus its
// current Task and TaskRun". The declared Workflow properties carry the parts
// the scheduler will read; the rest of the desk -- the stage ribbon, the log
// tail, the score bars -- rides as a document, because it is a rendering of
// state rather than state the scheduler branches on.
func (s *ArcadeStore) writeDesk(ctx context.Context, tx *ArcadeTx, d Desk) error {
	logs, err := jsonList(d.Logs)
	if err != nil {
		return err
	}
	parts, err := jsonList(d.ScoreParts)
	if err != nil {
		return err
	}
	props := map[string]any{
		"title": d.Name, "goal": deskGoal(d), "status": workflowStatus(d.State),
		"spentUsd": d.Cost, "productId": d.ID, "deskState": d.State,
		"stageIdx": d.StageIdx, "taskSeq": d.TaskSeq, "progress": d.Progress,
		"scored": d.Scored, "score": d.Score, "scoreParts": parts,
		"retryCount": d.Retry, "escalationCount": d.Esc, "blockReason": d.BlockReason,
		"approvalRequired": d.ApprovalRequired, "agents": d.Agents,
		"taskTitle": d.Task, "logs": logs, "repo": d.Repo,
	}
	if d.Meeting != nil {
		if props["meeting"], err = jsonMap(*d.Meeting); err != nil {
			return err
		}
	} else {
		props["meeting"] = nil
	}
	if err := s.upsert(ctx, tx, "Workflow", d.WorkflowID, props); err != nil {
		return err
	}
	if err := s.edge(ctx, tx, "HAS_WORKFLOW", "Product", d.ID, "Workflow", d.WorkflowID); err != nil {
		return err
	}
	if d.State == StateIdle || strings.TrimSpace(d.Task) == "" {
		return nil
	}
	// The desk's CURRENT task, by its display id -- see taskIDOf, which builds
	// one from the workflow number and the desk's task counter. This is not a
	// planned task and is deliberately distinguishable from one: it carries no
	// workflowId property, and that absence is exactly what load() filters on
	// when it rebuilds the graph. A planned Task comes from writeTasks below.
	taskID := taskIDOf(d)
	if err := s.upsert(ctx, tx, "Task", taskID, map[string]any{
		"title": d.Task, "status": taskStatus(d.State), "role": deskRole(d),
		"retryCount": d.Retry, "escalationCount": d.Esc,
		"approvalRequired": d.ApprovalRequired, "blockReason": d.BlockReason,
	}); err != nil {
		return err
	}
	return s.edge(ctx, tx, "HAS_TASK", "Workflow", d.WorkflowID, "Task", taskID)
}

// writeTasks persists one desk's planned graph: a Task vertex per record, the
// HAS_TASK edge from its Workflow, and a DEPENDS_ON edge per prerequisite.
// T-042.
//
// Direction matters and is not the argument order's to decide by accident.
// schema/08-edges.sql: "Task -DEPENDS_ON-> Task means the OUT side waits for
// the IN side", so the dependent is the FROM and the prerequisite is the TO.
// TestTheTaskGraphSurvivesARestart asserts that rather than trusting it.
//
// workflowId, deskId, story and ordinal ride as undeclared properties. That is
// established practice here rather than a shortcut -- writeDesk already writes
// productId, deskState, stageIdx and taskSeq onto Workflow, none of which the
// DDL declares either. The declared columns are the ones the scheduler filters
// and indexes on; these four are how a row finds its way home.
//
// Known cost, written down rather than discovered: this rewrites every task of
// every desk on every mutation, which is the same profile persist already has
// for desks. At fixture scale it is a few dozen statements. At real scale it
// wants to write only the task that changed, and that is a follow-up.
func (s *ArcadeStore) writeTasks(ctx context.Context, tx *ArcadeTx, deskID string) error {
	for _, t := range s.proj.tasksSnapshot(deskID) {
		if err := s.upsert(ctx, tx, "Task", t.ID, map[string]any{
			"title": t.Title, "status": t.Status, "role": t.Role,
			"priority": t.Priority, "modelHint": t.Model,
			"retryCount": t.Retry, "escalationCount": t.Esc,
			"approvalRequired": t.Gate, "blockReason": t.BlockReason,
			"workflowId": t.WorkflowID, "deskId": t.DeskID,
			"story": t.Story, "ordinal": t.Ordinal,
		}); err != nil {
			return err
		}
		if err := s.edge(ctx, tx, "HAS_TASK", "Workflow", t.WorkflowID, "Task", t.ID); err != nil {
			return err
		}
		for _, dep := range t.Deps {
			if err := s.edge(ctx, tx, "DEPENDS_ON", "Task", t.ID, "Task", dep); err != nil {
				return err
			}
		}
	}
	return nil
}

// eventPayloadWire is Event.payload as it sits in the database: the three
// presentation fields the timeline reads, plus the reducer's delta under its own
// key.
//
// Nested rather than flattened so the two never collide. `text` and `kind` are
// how a pill is drawn; `delta` is what fold.go folds. A future field on one has
// no way to shadow a field on the other.
type eventPayloadWire struct {
	Text  string       `json:"text"`
	Kind  string       `json:"kind"`
	Clock int          `json:"clock"`
	Delta EventPayload `json:"delta"`
}

// writeEvent appends one event. Never updated -- BR-04 -- so this is an INSERT
// rather than an upsert, and the (tenantId, seq) UNIQUE index in
// 09-indexes.sql is what turns a double write into an error instead of a
// silently duplicated moment in the timeline.
//
// entityId is the business id, never the RID: BR-03, because a restore into
// different buckets would break every RID the history had recorded. That is
// T-010, and TestEventsNeverReferenceARID is what keeps it true rather than
// merely intended.
//
// The payload carries the reducer's delta now, not a floor snapshot. Written as
// a document rather than a JSON string -- see jsonMap -- so the delta stays
// queryable on the way back out.
func (s *ArcadeStore) writeEvent(ctx context.Context, tx *ArcadeTx, e Event) error {
	p, err := jsonMap(eventPayloadWire{Text: e.Text, Kind: e.Kind, Clock: e.T, Delta: e.Payload})
	if err != nil {
		return err
	}
	return tx.Command(ctx,
		`INSERT INTO Event SET id = :id, tenantId = :t, seq = :seq, type = :ty,
		 entityId = :eid, entityType = 'Workflow', actor = 'operator',
		 ts = sysdate(), payload = :p`,
		map[string]any{
			"id": fmt.Sprintf("%s-ev-%d", s.tenant, e.Seq), "t": s.tenant,
			"seq": e.Seq, "ty": string(e.Type), "eid": e.DeskID, "p": p,
		}, nil)
}

// writeSnapshot stores a folded floor as a starting point for a later replay.
//
// One per snapshotEvery events, which is the thinning model.go promised "when
// the ArcadeDB store lands": it needed the reducer first, because a gap between
// snapshots is only crossable if something can fold across it, and inventing the
// gap before the reducer existed would have made replay wrong rather than cheap.
// The reducer exists now, so the gap is safe and the per-event floor copy is
// gone.
//
// BR-21 and T-100: this is an optimisation. uptoSeq says exactly which events
// are already folded into it, so a corrupt snapshot can always be discarded in
// favour of a fold from genesis, and the tests prove the answer is the same
// either way.
// It takes a FloorState rather than a []Desk on purpose: a rung has to hold
// everything the reducer would have produced at that seq, clock and open meeting
// included. A rung missing the clock folds forward to a floor whose event times
// are wrong, and it would agree with a fold from genesis on every field anyone
// happened to look at first.
func (s *ArcadeStore) writeSnapshot(ctx context.Context, tx *ArcadeTx, upto int, st FloorState) error {
	desks, err := jsonList(st.Desks)
	if err != nil {
		return err
	}
	return tx.Command(ctx,
		`INSERT INTO Snapshot SET id = :id, tenantId = :t, scope = 'floor',
		 scopeId = :t, uptoSeq = :u, ts = sysdate(), state = :s`,
		map[string]any{
			"id": fmt.Sprintf("%s-snap-%d", s.tenant, upto), "t": s.tenant,
			"u": upto, "s": map[string]any{
				"desks": desks, "clock": st.Clock, "meetingIdx": st.MeetingIdx,
			},
		}, nil)
}

// injectSubgraph materialises the master record as the vertices and edges §14
// maps it to.
//
// One item the schema refuses does not abort the injection. The declared
// constraints are real -- Requirement.text has a minimum length and
// RECONCILIATION.md confirmed the engine enforces it -- and a record with one
// two-word "goal" in it is a data-quality problem, not a reason to refuse to
// inject the other forty things that are fine. What must not happen is a
// partial injection reported as a whole one, which is why the count comes from
// Master.Project() either way and the skips are logged rather than hidden.
func (s *ArcadeStore) injectSubgraph(ctx context.Context, m Master) error {
	tx, err := s.arc.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	kinds := []struct {
		kind string
		text []string
	}{
		{"goal", m.BRD.Goals}, {"rule", m.BRD.Rules},
		{"scope", m.BRD.Scope}, {"out_of_scope", m.BRD.OutOfScope},
	}
	for _, k := range kinds {
		for i, text := range k.text {
			id := fmt.Sprintf("%s-%s-%d", m.ID, k.kind, i+1)
			if err := s.optional(s.upsert(ctx, tx, "Requirement", id, map[string]any{
				"kind": k.kind, "text": text, "sourceRef": m.ID,
			})); err != nil {
				return err
			}
			if err := s.optional(s.edge(ctx, tx, "HAS_REQUIREMENT", "Product", m.ID, "Requirement", id)); err != nil {
				return err
			}
		}
	}
	for _, st := range m.Stories {
		id := m.ID + "-" + st.ID
		if err := s.optional(s.upsert(ctx, tx, "Issue", id, map[string]any{
			"key": st.ID, "role": st.Role, "want": st.Want,
			"benefit": st.So, "priority": st.Pri,
			"status": issueStatus(st.Tasks),
		})); err != nil {
			return err
		}
		if err := s.optional(s.edge(ctx, tx, "HAS_ISSUE", "Product", m.ID, "Issue", id)); err != nil {
			return err
		}
		for i, ac := range st.AC {
			acID := fmt.Sprintf("%s-ac-%d", id, i+1)
			if err := s.optional(s.upsert(ctx, tx, "AcceptanceCriterion", acID, map[string]any{
				"text": ac,
			})); err != nil {
				return err
			}
			if err := s.optional(s.edge(ctx, tx, "HAS_CRITERION", "Issue", id, "AcceptanceCriterion", acID)); err != nil {
				return err
			}
		}
	}
	for _, g := range m.Diagrams {
		id := m.ID + "-" + g.ID
		if err := s.optional(s.upsert(ctx, tx, "Artifact", id, map[string]any{
			"kind": g.Kind, "title": g.Title, "body": g.Src, "mimeType": "text/plain",
		})); err != nil {
			return err
		}
		if err := s.optional(s.edge(ctx, tx, "HAS_ARTIFACT", "Product", m.ID, "Artifact", id)); err != nil {
			return err
		}
	}
	for _, a := range m.Decisions {
		id := m.ID + "-" + a.ID
		// key is the ADR number the operator sees; the vertex id namespaces it
		// under the product so two products can both have an ADR-014.
		if err := s.optional(s.upsert(ctx, tx, "Decision", id, map[string]any{
			"key": a.ID, "title": a.Title, "confidence": a.Confidence, "status": "ACCEPTED",
		})); err != nil {
			return err
		}
		if err := s.optional(s.edge(ctx, tx, "GOVERNS", "Decision", id, "Product", m.ID)); err != nil {
			return err
		}
	}
	if err := s.upsert(ctx, tx, "Product", m.ID, map[string]any{"lastInjectedAt": nowStamp()}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// optional swallows a per-item validation refusal and keeps going. Anything
// else -- a dropped connection, a type that does not exist -- still fails the
// injection, because those are not data-quality problems.
func (s *ArcadeStore) optional(err error) error {
	if err == nil {
		return nil
	}
	var ae *ArcadeError
	if errors.As(err, &ae) && strings.Contains(ae.Exception, "ValidationException") {
		return nil
	}
	return err
}

// upsert writes one vertex by business id. tx may be nil for a standalone write.
func (s *ArcadeStore) upsert(ctx context.Context, tx *ArcadeTx, typ, id string, props map[string]any) error {
	// createdAt is mandatory on GaosVertex and must not move when a record is
	// rewritten, and an UPSERT cannot branch on whether it inserted. ifnull
	// resolves that in one statement: on the insert path the property does not
	// exist yet and the expression is sysdate(); on the update path it is
	// whatever the first write stamped. MEASURED on 26.7.2 -- doing it as a
	// second UPDATE instead fails, because the mandatory-property check runs at
	// commit and the record is already invalid by then.
	sets := []string{
		"tenantId = :__t", "id = :__id",
		"createdAt = ifnull(createdAt, sysdate())", "updatedAt = sysdate()",
	}
	params := map[string]any{"__t": s.tenant, "__id": id}
	i := 0
	for k, v := range props {
		p := fmt.Sprintf("p%d", i)
		sets = append(sets, k+" = :"+p)
		params[p] = v
		i++
	}
	// The WHERE must hit an index or 26.7.2 refuses the UPSERT outright. D-047
	// added the (tenantId, id) index to every type written this way; a type
	// without one fails here rather than writing something unfindable.
	sql := fmt.Sprintf("UPDATE %s SET %s UPSERT WHERE id = :__id AND tenantId = :__t",
		typ, strings.Join(sets, ", "))
	return s.run(ctx, tx, sql, params)
}

// edge creates one edge between two business ids, once.
//
// D-045 replaced the UNIQUE keyword with a unique index on (`@out`, `@in`), and
// that index is what makes this idempotent: a second identical edge raises
// DuplicatedKeyException rather than quietly accumulating, and here that is the
// success case rather than the failure one. BR-24 is why there is one edge type
// per relationship and no type property to filter on.
func (s *ArcadeStore) edge(ctx context.Context, tx *ArcadeTx, label, fromType, fromID, toType, toID string) error {
	// The existence check is not belt-and-braces over the unique index; it is
	// the only thing that works. 26.7.2 has no CREATE EDGE ... IF NOT EXISTS,
	// and inside a transaction the index violation is raised at COMMIT rather
	// than at the statement -- so catching DuplicatedKeyException here would
	// never fire, and the whole mutation would fail on a re-write of an edge
	// that was already correct. MEASURED, after exactly that happened.
	//
	// The traversal form is also the only one that reads the edge: filtering
	// `FROM LABEL WHERE out.id = ...` returns zero for an edge that is
	// demonstrably there.
	var hits []struct {
		N int `json:"n"`
	}
	probe := fmt.Sprintf(
		`SELECT count(*) AS n FROM (SELECT expand(out('%s')) FROM %s
		 WHERE id = :a AND tenantId = :t) WHERE id = :b`, label, fromType)
	params := map[string]any{"a": fromID, "b": toID, "t": s.tenant}
	if err := s.query(ctx, tx, probe, params, &hits); err != nil {
		return err
	}
	if len(hits) > 0 && hits[0].N > 0 {
		return nil
	}
	sql := fmt.Sprintf(
		`CREATE EDGE %s FROM (SELECT FROM %s WHERE id = :a AND tenantId = :t)
		 TO (SELECT FROM %s WHERE id = :b AND tenantId = :t) SET createdAt = sysdate()`,
		label, fromType, toType)
	err := s.run(ctx, tx, sql, params)
	var ae *ArcadeError
	if errors.As(err, &ae) && strings.Contains(ae.Exception, "DuplicatedKeyException") {
		return nil
	}
	return err
}

func (s *ArcadeStore) query(ctx context.Context, tx *ArcadeTx, sql string, params map[string]any, out any) error {
	if tx != nil {
		return tx.Query(ctx, sql, params, out)
	}
	return s.arc.Query(ctx, sql, params, out)
}

func (s *ArcadeStore) run(ctx context.Context, tx *ArcadeTx, sql string, params map[string]any) error {
	if tx != nil {
		return tx.Command(ctx, sql, params, nil)
	}
	return s.arc.Command(ctx, sql, params, nil)
}

// ---------------------------------------------------------------- rebuild
//
// load is the half of BR-01 that makes the projection legitimate. If this could
// not put the floor back, the memory above would be a source of truth wearing a
// cache's clothes.

func (s *ArcadeStore) load(ctx context.Context) (bool, error) {
	var prows []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Repo string `json:"repo"`
		Goal string `json:"goal"`

		Code      string          `json:"code"`
		Owner     string          `json:"owner"`
		Priority  string          `json:"priority"`
		Stage     string          `json:"stage"`
		Repos     []string        `json:"repos"`
		BRD       json.RawMessage `json:"brd"`
		Stories   json.RawMessage `json:"stories"`
		Diagrams  json.RawMessage `json:"diagrams"`
		Decisions json.RawMessage `json:"decisions"`
	}
	if err := s.arc.Query(ctx,
		`SELECT id, name, repo, goal, code, owner, priority, stage, repos,
		        brd, stories, diagrams, decisions
		 FROM Product WHERE tenantId = :t ORDER BY id`,
		map[string]any{"t": s.tenant}, &prows); err != nil {
		return false, err
	}
	if len(prows) == 0 {
		return false, nil
	}

	m := newProjection()
	for _, r := range prows {
		m.products = append(m.products, Product{ID: r.ID, Name: r.Name, Repo: r.Repo, Goal: r.Goal})
		mr := Master{ID: r.ID, Code: r.Code, Owner: r.Owner, Priority: r.Priority,
			Stage: r.Stage, Repos: r.Repos}
		if err := decodeInto(r.BRD, &mr.BRD); err != nil {
			return false, err
		}
		if err := decodeInto(r.Stories, &mr.Stories); err != nil {
			return false, err
		}
		if err := decodeInto(r.Diagrams, &mr.Diagrams); err != nil {
			return false, err
		}
		if err := decodeInto(r.Decisions, &mr.Decisions); err != nil {
			return false, err
		}
		m.masters = append(m.masters, mr)
	}

	var wrows []struct {
		ID        string          `json:"id"`
		ProductID string          `json:"productId"`
		Title     string          `json:"title"`
		Goal      string          `json:"goal"`
		Repo      string          `json:"repo"`
		DeskState string          `json:"deskState"`
		StageIdx  int             `json:"stageIdx"`
		TaskSeq   int             `json:"taskSeq"`
		Progress  float64         `json:"progress"`
		Scored    bool            `json:"scored"`
		Score     int             `json:"score"`
		Parts     json.RawMessage `json:"scoreParts"`
		Retry     int             `json:"retryCount"`
		Esc       int             `json:"escalationCount"`
		Spent     float64         `json:"spentUsd"`
		Block     string          `json:"blockReason"`
		Approval  bool            `json:"approvalRequired"`
		Agents    []string        `json:"agents"`
		TaskTitle string          `json:"taskTitle"`
		Logs      json.RawMessage `json:"logs"`
		Meeting   json.RawMessage `json:"meeting"`
	}
	if err := s.arc.Query(ctx,
		`SELECT id, productId, title, goal, repo, deskState, stageIdx, taskSeq,
		        progress, scored, score, scoreParts, retryCount, escalationCount, spentUsd,
		        blockReason, approvalRequired, agents, taskTitle, logs, meeting
		 FROM Workflow WHERE tenantId = :t ORDER BY productId`,
		map[string]any{"t": s.tenant}, &wrows); err != nil {
		return false, err
	}
	for _, r := range wrows {
		d := Desk{
			ID: r.ProductID, Name: r.Title, Repo: r.Repo, Goal: r.Goal,
			WorkflowID: r.ID, TaskSeq: r.TaskSeq, State: r.DeskState,
			StageIdx: r.StageIdx, Progress: r.Progress, Scored: r.Scored,
			Score: r.Score, Retry: r.Retry, Esc: r.Esc, Cost: r.Spent,
			BlockReason: r.Block, ApprovalRequired: r.Approval,
			Agents: r.Agents, Task: r.TaskTitle,
		}
		if err := decodeInto(r.Parts, &d.ScoreParts); err != nil {
			return false, err
		}
		if err := decodeInto(r.Logs, &d.Logs); err != nil {
			return false, err
		}
		if len(r.Meeting) > 0 && string(r.Meeting) != "null" {
			var mt Meeting
			if err := decodeInto(r.Meeting, &mt); err != nil {
				return false, err
			}
			d.Meeting = &mt
			m.meetingIdx = len(m.desks)
		}
		m.spend += r.Spent
		m.desks = append(m.desks, d)
	}

	// The planned task graph. T-042, and the half of BR-01 that makes the
	// scheduler's input legitimate: the graph is not folded from the stream, so
	// if this could not put it back it would be memory pretending to be a
	// record.
	//
	// workflowId IS NOT NULL is the filter that separates a planned task from
	// the desk's current display task -- writeDesk writes one of those per
	// seated desk with a taskIDOf id and no workflowId, and pulling it in here
	// would put a task in the graph that no plan ever produced.
	//
	// Ordered by workflowId then ordinal, so the slice comes back in plan order.
	// The scheduler is required to reach the same answer in two replicas (D-1),
	// and an input whose order came out of the storage layer's bucket layout
	// would be the first thing to break that.
	var trows []struct {
		ID         string `json:"id"`
		WorkflowID string `json:"workflowId"`
		DeskID     string `json:"deskId"`
		Title      string `json:"title"`
		Role       string `json:"role"`
		Story      string `json:"story"`
		Model      string `json:"modelHint"`
		Status     string `json:"status"`
		Priority   int    `json:"priority"`
		Ordinal    int    `json:"ordinal"`
		Gate       bool   `json:"approvalRequired"`
		Retry      int    `json:"retryCount"`
		Esc        int    `json:"escalationCount"`
		Block      string `json:"blockReason"`
	}
	if err := s.arc.Query(ctx,
		`SELECT id, workflowId, deskId, title, role, story, modelHint, status,
		        priority, ordinal, approvalRequired, retryCount, escalationCount, blockReason
		 FROM Task WHERE tenantId = :t AND workflowId IS NOT NULL
		 ORDER BY workflowId, ordinal`,
		map[string]any{"t": s.tenant}, &trows); err != nil {
		return false, err
	}
	for _, r := range trows {
		// One traversal per task rather than a projection of out('DEPENDS_ON')
		// across the whole type. The expand form is the one this file has
		// already proved works on 26.7.2 -- edge() says filtering an edge any
		// other way "returns zero for an edge that is demonstrably there" -- and
		// load runs once per boot, so N+1 queries buy correctness cheaply.
		var deps []struct {
			ID string `json:"id"`
		}
		if err := s.arc.Query(ctx,
			`SELECT id FROM (SELECT expand(out('DEPENDS_ON')) FROM Task
			 WHERE id = :a AND tenantId = :t) ORDER BY id`,
			map[string]any{"a": r.ID, "t": s.tenant}, &deps); err != nil {
			return false, err
		}
		t := TaskRecord{
			ID: r.ID, WorkflowID: r.WorkflowID, DeskID: r.DeskID,
			Title: r.Title, Role: r.Role, Story: r.Story, Model: r.Model,
			Status: r.Status, Priority: r.Priority, Ordinal: r.Ordinal,
			Gate: r.Gate, Retry: r.Retry, Esc: r.Esc, BlockReason: r.Block,
		}
		for _, d := range deps {
			t.Deps = append(t.Deps, d.ID)
		}
		m.tasks = append(m.tasks, t)
	}

	// The workflow counter, restored from the ids actually in the graph rather
	// than from a count of anything. A restarted dashboard that resumed
	// numbering from len(desks) would hand the next workflow an id some earlier
	// workflow already has, and Workflow (tenantId, id) UNIQUE would let the
	// second one overwrite the first.
	for _, d := range m.desks {
		if n, ok := workflowNumber(d.WorkflowID); ok && n > m.workflowSeq {
			m.workflowSeq = n
		}
	}
	for _, t := range m.tasks {
		if n, ok := workflowNumber(t.WorkflowID); ok && n > m.workflowSeq {
			m.workflowSeq = n
		}
	}

	// The whole stream, oldest first. There used to be a LIMIT here matching the
	// in-memory ring, with a comment saying unbounding it was T-013; that is
	// this change. The record is the record -- Replay now addresses an event Seq
	// rather than an offset into a window, so there is no window left for the
	// two stores to have to agree about.
	var erows []struct {
		Seq     int             `json:"seq"`
		Type    string          `json:"type"`
		Entity  string          `json:"entityId"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := s.arc.Query(ctx,
		`SELECT seq, type, entityId, payload FROM Event
		 WHERE tenantId = :t ORDER BY seq`,
		map[string]any{"t": s.tenant}, &erows); err != nil {
		return false, err
	}
	for _, r := range erows {
		var pw eventPayloadWire
		if err := decodeInto(r.Payload, &pw); err != nil {
			return false, err
		}
		e := Event{Seq: r.Seq, Type: EventType(r.Type), DeskID: r.Entity,
			Text: pw.Text, Kind: pw.Kind, T: pw.Clock, Payload: pw.Delta}
		if e.Kind == KindWarn || e.Kind == KindFail || e.Type == ApprovalRequested {
			m.notifs++
		}
		m.events = append(m.events, e)
		if r.Seq > m.seq {
			m.seq = r.Seq
		}
		if pw.Clock > m.clock {
			m.clock = pw.Clock
		}
	}

	// The snapshot ladder, read back so a scrub on a long-lived database does
	// not fold the whole stream every time. Ordered by uptoSeq because foldTo
	// walks the rungs forward and stops at the first one past the cursor.
	//
	// If this query returned nothing -- an empty Snapshot type, a wiped one,
	// rungs from a build that wrote them differently -- replay still works and
	// still gives the same answer. That is not a happy accident, it is BR-21,
	// and TestFoldNeedsNoSnapshotAtAll is the test that would notice the day it
	// stopped being true.
	var srows []struct {
		UptoSeq int `json:"uptoSeq"`
		State   struct {
			Desks      []Desk `json:"desks"`
			Clock      int    `json:"clock"`
			MeetingIdx int    `json:"meetingIdx"`
		} `json:"state"`
	}
	if err := s.arc.Query(ctx,
		`SELECT uptoSeq, state FROM Snapshot
		 WHERE tenantId = :t AND scope = 'floor' ORDER BY uptoSeq`,
		map[string]any{"t": s.tenant}, &srows); err != nil {
		return false, err
	}
	for _, r := range srows {
		m.snaps = append(m.snaps, floorSnapshot{
			UptoSeq: r.UptoSeq,
			State: FloorState{
				Desks: r.State.Desks, Clock: r.State.Clock,
				MeetingIdx: r.State.MeetingIdx,
			},
		})
	}

	s.proj = m
	return true, nil
}

// ---------------------------------------------------------------- mapping

// workflowStatus maps a desk state onto Workflow.status, whose vocabulary is
// enforced by a regexp in 04-execution.sql rather than by convention. The two
// vocabularies are not the same size -- "review" and "meeting" are both a
// workflow that is running while someone looks at it -- and the desk state is
// kept alongside as deskState so nothing is lost in the narrowing.
func workflowStatus(state string) string {
	switch state {
	case StateBlocked:
		return "BLOCKED"
	case StateCompleted:
		return "COMPLETED"
	case StateIdle:
		return "DRAFT"
	default:
		return "RUNNING"
	}
}

// taskStatus does the same for Task.status, whose regexp RECONCILIATION.md
// measured as a real boundary: status='NONSENSE' is refused with a
// ValidationException.
func taskStatus(state string) string {
	switch state {
	case StateReview, StateMeeting:
		return "REVIEW"
	case StateBlocked:
		return "BLOCKED"
	case StateCompleted:
		return "DONE"
	case StateIdle:
		return "CANCELED"
	default:
		return "RUNNING"
	}
}

// deskRole is Task.role, which the DDL restricts to the seven seats on the
// floor. The stage the desk is on names the role, same as advance() does when
// it reseats an agent.
func deskRole(d Desk) string {
	if r := stageRole[stageOf(d)]; r != "" {
		return r
	}
	return "planner"
}

// deskGoal satisfies Workflow.goal's declared minimum of ten characters without
// inventing a goal. A desk with nothing written on it yet gets the record's own
// words for that, which is what the back office shows in the same situation.
func deskGoal(d Desk) string {
	if len(strings.TrimSpace(d.Goal)) >= 10 {
		return d.Goal
	}
	return "No objective set yet"
}

func issueStatus(tasks int) string {
	if tasks > 0 {
		return "PLANNED"
	}
	return "OPEN"
}

func nowStamp() string { return time.Now().UTC().Format("2006-01-02 15:04:05") }

// jsonMap and jsonList move a Go value through JSON so it reaches ArcadeDB as a
// document rather than as a string holding JSON. The difference matters on the
// way back: a document is queryable, a string is opaque.
func jsonMap(v any) (map[string]any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	return out, json.Unmarshal(b, &out)
}

func jsonList(v any) ([]any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out []any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []any{}
	}
	return out, nil
}

func decodeInto(raw json.RawMessage, out any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return json.Unmarshal(raw, out)
}
