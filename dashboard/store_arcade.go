package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
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

	// What the boot's schema apply did. ApplySchema returns these "so the boot
	// log can say which of the two happened without the operator having to
	// guess", and for a while this constructor discarded both -- so the only
	// evidence a schema change had landed was that the process was still
	// running, which is an inference rather than a reading.
	schemaApplied, schemaPresent int

	// written is what the graph holds: a fingerprint per record ("Type/id"), as
	// of the last transaction that committed it. persist writes a record only
	// when its fingerprint has moved. staged is the current transaction's half,
	// folded into written on a successful commit and dropped on any failure, so
	// a write that never landed is written again by the next persist.
	//
	// Before this, every persist rewrote every desk, task, run and context slice
	// in the floor -- an edge probe per citation per slice -- on every event. On
	// a slow database that is minutes of statements under mu, and every handler
	// queued behind it (v0.40.2). Both maps are guarded by mu.
	written map[string]string
	staged  map[string]string

	// tap is the commit feed's callback (SetCommitTap); guarded by mu.
	tap func(Event)
}

// SchemaCounts is what the last apply did: statements newly applied, and
// statements already present.
//
// Read once at boot by serve(). Not on the Store interface: it is a fact about
// how this implementation started, not something a handler may ask.
func (s *ArcadeStore) SchemaCounts() (applied, present int) {
	return s.schemaApplied, s.schemaPresent
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

	applied, present, err := arc.ApplySchema(ctx)
	if err != nil {
		return nil, fmt.Errorf("schema: %w", err)
	}
	s.schemaApplied, s.schemaPresent = applied, present
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
	} else {
		s.markLoaded()
		s.backfillSlugs(ctx)
	}
	installHook(s.proj)
	return s, nil
}

func (s *ArcadeStore) Kind() string { return "arcadedb" }

// backfillSlugs gives a slug to every loaded product that has no usable one
// (D-094): products that predate slugs, and a stored slug that is malformed or
// a duplicate. Each is an ordinary mutation -- the product and its
// RecordUpdated event in one transaction (BR-02) -- and fails as one does: the
// projection keeps what the database did not take, and the next boot, finding
// no stored slug, assigns the same one again. A boot never stops for it.
func (s *ArcadeStore) backfillSlugs(ctx context.Context) {
	s.proj.mu.RLock()
	products := append([]Product(nil), s.proj.products...)
	s.proj.mu.RUnlock()
	for _, i := range backfillSlugs(products) {
		id := products[i].ID
		if _, err := s.mutate(ctx, func() (Result, error) { return s.proj.AssignSlug(ctx, id) }); err != nil {
			logSlugBackfill(id, err)
		}
	}
}

// SetCommitTap installs the callback persist calls, after tx.Commit, for every event it wrote.
func (s *ArcadeStore) SetCommitTap(f func(Event)) {
	s.mu.Lock()
	s.tap = f
	s.mu.Unlock()
}

// TaskInfo is the projection's.
func (s *ArcadeStore) TaskInfo(ctx context.Context, id string) (TaskRecord, bool) {
	return s.proj.TaskInfo(ctx, id)
}

// Live is the projection's broker: every event the projection emits is one
// the graph is about to receive, so the hint and the record leave together.
func (s *ArcadeStore) Live() *Broker { return s.proj.Live() }

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
func (s *ArcadeStore) TaskContext(ctx context.Context, i int) (ContextView, error) {
	return s.proj.TaskContext(ctx, i)
}
func (s *ArcadeStore) Runs(ctx context.Context, taskID string) ([]TaskRunRecord, error) {
	return s.proj.Runs(ctx, taskID)
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

func (s *ArcadeStore) Replay(ctx context.Context, i int) (Result, error) {
	return s.proj.Replay(ctx, i)
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
//
// The write is bounded by backgroundPersistBudget, counted from the moment the
// lock is held. Each statement already has the HTTP client's thirty seconds,
// but a transaction is many statements, and a database that answers each one
// slowly would otherwise hold mu -- and every handler waiting on it -- for as
// long as the sum. A write that runs out of budget is a failed write under the
// contract above: its events are lost and logged, and the records it carried
// stay unstaged (see written), so the next persist writes them again.
func (s *ArcadeStore) persistBackground(ctx context.Context, r Result) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, backgroundPersistBudget)
	defer cancel()
	return s.persist(ctx, r)
}

// backgroundPersistBudget is how long one background write may hold the store
// lock. A var so a test can shorten it.
var backgroundPersistBudget = 60 * time.Second

// rollbackBudget bounds the rollback a failed persist sends. The persist's own
// context may be the thing that expired, and a rollback sent on it would fail
// before it left; one sent with no bound could hold mu for the client's thirty
// seconds against the same slow database.
const rollbackBudget = 5 * time.Second

// fingerprint is a record's content as a digest. A record that will not
// marshal gets no fingerprint, which reads as "changed" and is written every
// time, as everything was before the cache.
func fingerprint(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// unchanged reports whether the graph already holds v under key, and returns
// v's fingerprint for stage.
func (s *ArcadeStore) unchanged(key string, v any) (string, bool) {
	fp := fingerprint(v)
	return fp, fp != "" && s.written[key] == fp
}

// stage records that this transaction wrote key at fp. Outside a staging set
// (staged nil) it does nothing.
func (s *ArcadeStore) stage(key, fp string) {
	if fp != "" && s.staged != nil {
		s.staged[key] = fp
	}
}

// beginStage opens the staging set for one transaction; commitStage folds it
// into written once the transaction has committed. A persist that leaves
// without calling commitStage discards it.
func (s *ArcadeStore) beginStage() { s.staged = map[string]string{} }

func (s *ArcadeStore) commitStage() {
	if s.written == nil {
		s.written = map[string]string{}
	}
	for k, v := range s.staged {
		s.written[k] = v
	}
	s.staged = nil
}

// markLoaded records every desk, task, run, slice and handoff the projection
// holds as already in the graph. Called once load() has rebuilt the projection
// FROM the graph, so the first write after a restart is the change it carries
// rather than the whole world again -- which, on the slow database this cache
// exists for, is the write that never finishes.
func (s *ArcadeStore) markLoaded() {
	s.beginStage()
	for _, d := range s.proj.floor().Desks {
		s.stage("Workflow/"+d.WorkflowID, fingerprint(d))
		for _, t := range s.proj.tasksSnapshot(d.ID) {
			s.stage("Task/"+t.ID, fingerprint(t))
		}
		runs, slices := s.proj.runsSnapshot(d.ID)
		for _, r := range runs {
			s.stage("TaskRun/"+r.ID, fingerprint(r))
		}
		for dg, rec := range slices {
			s.stage("ContextSlice/"+dg, fingerprint(rec))
		}
		for _, a := range s.proj.artifactsSnapshot(d.ID) {
			s.stage("Artifact/"+a.ID, fingerprint(a))
		}
	}
	s.commitStage()
}

// soft is optional() for a record whose fingerprint is about to be staged: a
// refusal it swallows marks the record unclean, so the edge is tried again on
// the next persist rather than forgotten behind a fingerprint that says the
// record is written.
func (s *ArcadeStore) soft(err error, clean *bool) error {
	if err != nil && s.optional(err) == nil {
		*clean = false
		return nil
	}
	return err
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
func (s *ArcadeStore) SetTaskLabel(ctx context.Context, productID, taskID, label, actor string) (Result, error) {
	return s.mutate(ctx, func() (Result, error) { return s.proj.SetTaskLabel(ctx, productID, taskID, label, actor) })
}
func (s *ArcadeStore) APICardLookup(ctx context.Context, productID string, in APICardIn) (TaskRecord, APILookup, error) {
	return s.proj.APICardLookup(ctx, productID, in)
}
func (s *ArcadeStore) APICardGet(ctx context.Context, productID, taskID, externalID, client string) (TaskRecord, error) {
	return s.proj.APICardGet(ctx, productID, taskID, externalID, client)
}
func (s *ArcadeStore) AdoptAPICard(ctx context.Context, productID, taskID string, in APICardIn) (TaskRecord, Result, error) {
	var t TaskRecord
	r, err := s.mutate(ctx, func() (Result, error) {
		var res Result
		var e error
		t, res, e = s.proj.AdoptAPICard(ctx, productID, taskID, in)
		return res, e
	})
	return t, r, err
}
func (s *ArcadeStore) PlanAPICard(ctx context.Context, productID string, in APICardIn, now int64) (TaskRecord, bool, Result, error) {
	var t TaskRecord
	var created bool
	r, err := s.mutate(ctx, func() (Result, error) {
		var res Result
		var e error
		t, created, res, e = s.proj.PlanAPICard(ctx, productID, in, now)
		return res, e
	})
	return t, created, r, err
}
func (s *ArcadeStore) APICardCancel(ctx context.Context, productID, taskID, client string) (Result, error) {
	return s.mutate(ctx, func() (Result, error) { return s.proj.APICardCancel(ctx, productID, taskID, client) })
}
func (s *ArcadeStore) HoldReview(ctx context.Context, productID, taskID string) (Result, error) {
	return s.mutate(ctx, func() (Result, error) { return s.proj.HoldReview(ctx, productID, taskID) })
}
func (s *ArcadeStore) FinishMergedReviews(ctx context.Context) (Result, error) {
	return s.mutate(ctx, func() (Result, error) { return s.proj.FinishMergedReviews(ctx) })
}
func (s *ArcadeStore) ResolvePRCard(ctx context.Context, productID, taskID, to, reason string) (Result, error) {
	return s.mutate(ctx, func() (Result, error) { return s.proj.ResolvePRCard(ctx, productID, taskID, to, reason) })
}
// SetPRHooks installs the pull-request flow's seams on the projection.
func (s *ArcadeStore) SetPRHooks(parts func(TaskRecord) []PacketPart, after func(TaskRecord, string)) {
	s.proj.SetPRHooks(parts, after)
}
func (s *ArcadeStore) ListContacts(ctx context.Context) ([]Contact, error) {
	return s.proj.ListContacts(ctx)
}
func (s *ArcadeStore) UpsertContact(ctx context.Context, c Contact, actor string) (Result, error) {
	return s.mutate(ctx, func() (Result, error) { return s.proj.UpsertContact(ctx, c, actor) })
}
func (s *ArcadeStore) DeleteContact(ctx context.Context, productID, login, actor string) (Result, error) {
	return s.mutate(ctx, func() (Result, error) { return s.proj.DeleteContact(ctx, productID, login, actor) })
}
func (s *ArcadeStore) RecordNotification(ctx context.Context, productID, alias, kind, outcome string) (Result, error) {
	return s.mutate(ctx, func() (Result, error) { return s.proj.RecordNotification(ctx, productID, alias, kind, outcome) })
}
func (s *ArcadeStore) ReportDefects(ctx context.Context, productID string, in DefectReportIn) (DefectReportOut, Result, error) {
	var out DefectReportOut
	var res Result
	_, err := s.mutate(ctx, func() (Result, error) {
		var e error
		out, res, e = s.proj.ReportDefects(ctx, productID, in)
		return res, e
	})
	return out, res, err
}
func (s *ArcadeStore) Defects(ctx context.Context, productID string) ([]Defect, error) {
	return s.proj.Defects(ctx, productID)
}
func (s *ArcadeStore) DefectByKey(ctx context.Context, productID, key string) (Defect, error) {
	return s.proj.DefectByKey(ctx, productID, key)
}
func (s *ArcadeStore) SetDefectContext(ctx context.Context, productID, key string, c DefectContext) (Result, error) {
	return s.mutate(ctx, func() (Result, error) { return s.proj.SetDefectContext(ctx, productID, key, c) })
}
func (s *ArcadeStore) ResolveTriage(ctx context.Context, productID, taskID string, v TriageVerdict) (Result, error) {
	return s.mutate(ctx, func() (Result, error) { return s.proj.ResolveTriage(ctx, productID, taskID, v) })
}
func (s *ArcadeStore) CloseDefect(ctx context.Context, productID, ref, actor string) (Result, error) {
	return s.mutate(ctx, func() (Result, error) { return s.proj.CloseDefect(ctx, productID, ref, actor) })
}
func (s *ArcadeStore) FixPRs(ctx context.Context, productID string) ([]FixPR, error) {
	return s.proj.FixPRs(ctx, productID)
}
func (s *ArcadeStore) UpsertFixPR(ctx context.Context, f FixPR) (Result, error) {
	return s.mutate(ctx, func() (Result, error) { return s.proj.UpsertFixPR(ctx, f) })
}
func (s *ArcadeStore) AutoFixOf(ctx context.Context, productID string) (AutoFix, error) {
	return s.proj.AutoFixOf(ctx, productID)
}
func (s *ArcadeStore) SetAutoFix(ctx context.Context, productID string, a AutoFix, actor, tokenKind string) (Result, error) {
	return s.mutate(ctx, func() (Result, error) { return s.proj.SetAutoFix(ctx, productID, a, actor, tokenKind) })
}
func (s *ArcadeStore) PRCards(ctx context.Context, productID string) ([]TaskRecord, error) {
	return s.proj.PRCards(ctx, productID)
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
// PlanPullRequestCards plans the pull-request cards under the same lock as
// every other mutation, so the graph and the desk agree on what exists.
func (s *ArcadeStore) PlanPullRequestCards(ctx context.Context, productID string, ev PREvent) ([]string, []string, Result, error) {
	var planned, existing []string
	var res Result
	_, err := s.mutate(ctx, func() (Result, error) {
		var e error
		planned, existing, res, e = s.proj.PlanPullRequestCards(ctx, productID, ev)
		return res, e
	})
	return planned, existing, res, err
}

func (s *ArcadeStore) SavePolicy(ctx context.Context, id string, p Policy) (Result, error) {
	return s.mutate(ctx, func() (Result, error) { return s.proj.SavePolicy(ctx, id, p) })
}
func (s *ArcadeStore) AddKnowledgeSource(ctx context.Context, id string, k KnowledgeSource) (Result, error) {
	return s.mutate(ctx, func() (Result, error) { return s.proj.AddKnowledgeSource(ctx, id, k) })
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
	s.beginStage()
	defer func() {
		s.staged = nil // a commit has already folded it in; any other exit discards it
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackBudget)
		defer cancel()
		tx.Rollback(rctx)
	}()

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
		if err := s.writeRuns(ctx, tx, d.ID); err != nil {
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
	for _, c := range r.QaCases {
		if err := s.writeQaCase(ctx, tx, c); err != nil {
			return err
		}
	}
	for _, op := range r.Contacts {
		if err := s.writeContact(ctx, tx, op); err != nil {
			return err
		}
	}
	for _, d := range r.Defects {
		if err := s.writeDefect(ctx, tx, d); err != nil {
			return err
		}
	}
	for _, f := range r.FixPRs {
		if err := s.writeFixPR(ctx, tx, f); err != nil {
			return err
		}
	}
	for _, p := range r.PullRequests {
		if err := s.writePullRequest(ctx, tx, p); err != nil {
			return err
		}
	}
	for _, k := range r.PullRequestDeletes {
		if err := s.deletePullRequest(ctx, tx, k); err != nil {
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
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	s.commitStage()
	// The commit feed (braintown.go): only an event the graph now holds reaches it. Runs
	// under mu, so the callback must not block.
	if s.tap != nil {
		for _, e := range r.NewEvents {
			s.tap(e)
		}
	}
	return nil
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
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	// The seed is the whole world, so the graph now holds everything the
	// projection does -- the same position load() leaves it in.
	s.markLoaded()
	return nil
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
		"name": p.Name, "repo": p.Repo, "goal": p.Goal, "slug": p.Slug,
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
	policies, err := jsonList(m.Blueprint.Policies)
	if err != nil {
		return err
	}
	sources, err := jsonList(m.Blueprint.Sources)
	if err != nil {
		return err
	}
	workspace, err := jsonMap(m.Workspace)
	if err != nil {
		return err
	}
	vendors, err := jsonMap(m.Vendors)
	if err != nil {
		return err
	}
	qaState, err := jsonMap(m.Qa)
	if err != nil {
		return err
	}
	autoFix, err := jsonMap(m.AutoFix)
	if err != nil {
		return err
	}
	props := map[string]any{
		"code": m.Code, "owner": m.Owner, "priority": m.Priority,
		"brd": brd, "stories": stories, "diagrams": diagrams,
		"decisions": decisions, "repos": m.Repos,
		"policies": policies, "knowledgeSources": sources, // the Blueprint draft, D-058
		"workspace":    workspace, // the product's ai-manager workspace and what it holds, M14.4
		"vendorPolicy": vendors,   // which AI vendors run its tasks, M16
		"qaState":      qaState,   // the QA catalog's modules, epics, head and sync state, D-072
	}
	props["autoFix"] = autoFix // the defect auto-fix policy, D-091
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
	return s.writeBlueprintVersion(ctx, tx, m)
}

// writeDesk persists one desk as the Workflow it is, plus the Task it is
// currently on.
//
// model.go states the mapping this implements: "Desk is a Workflow plus its
// current Task and TaskRun". The declared Workflow properties carry the parts
// the scheduler will read; the rest of the desk -- the stage ribbon, the log
// tail, the score bars -- rides as a document, because it is a rendering of
// state rather than state the scheduler branches on.
//
// A desk whose content has not moved since the graph last took it is skipped
// (see ArcadeStore.written): the Workflow, its current Task and both edges are
// all functions of d.
func (s *ArcadeStore) writeDesk(ctx context.Context, tx *ArcadeTx, d Desk) error {
	key := "Workflow/" + d.WorkflowID
	fp, same := s.unchanged(key, d)
	if same {
		return nil
	}
	if err := s.writeDeskRecord(ctx, tx, d); err != nil {
		return err
	}
	s.stage(key, fp)
	return nil
}

func (s *ArcadeStore) writeDeskRecord(ctx context.Context, tx *ArcadeTx, d Desk) error {
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
// It writes only the tasks that changed since the graph last took them (see
// ArcadeStore.written). It used to rewrite every task of every desk on every
// mutation, which this comment once called "a follow-up"; in v0.40.2 that
// follow-up was a board move waiting minutes behind a background write.
func (s *ArcadeStore) writeTasks(ctx context.Context, tx *ArcadeTx, deskID string) error {
	for _, t := range s.proj.tasksSnapshot(deskID) {
		key := "Task/" + t.ID
		fp, same := s.unchanged(key, t)
		if same {
			continue
		}
		s.stage(key, fp) // discarded with the transaction if anything below fails
		if err := s.upsert(ctx, tx, "Task", t.ID, map[string]any{
			"title": t.Title, "status": t.Status, "role": t.Role,
			"priority": t.Priority, "modelHint": t.Model,
			"retryCount": t.Retry, "escalationCount": t.Esc,
			"approvalRequired": t.Gate, "blockReason": t.BlockReason,
			"workflowId": t.WorkflowID, "deskId": t.DeskID,
			"story": t.Story, "ordinal": t.Ordinal, "rank": t.Rank,
			// The kind is what the design gate branches on (M17, D-069). Without
			// it a restart erases every task's kind, and the gate is inert on a
			// restored graph: the review task takes the implement branch and is
			// handed the author's brief, handoff and operator answer.
			"kind": t.Kind,
			// The labels, as one JSON array (D-088). The vocabulary is closed,
			// so what is written is always a subset of dashboard/labels.go.
			"labels":   labelsJSON(t.Labels),
			"dedupKey": t.DedupKey,
			// D-091: the automatic dispatch's Unix second, counted by the caps.
			"autoAt": t.AutoAt,
			// D-099: the external-tasks API's owner, read grants, creation second
			// and the unattended-start switch.
			"apiOwner": t.APIOwner, "apiRefs": refsJSON(t.APIRefs), "apiAt": t.APIAt,
			"autoDispatch": t.AutoDispatch,
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

// writeRuns persists one desk's attempts and the context each was given.
// T-054 and T-055, requirements D-5 and D-6.
//
// Three edges, and their directions are the model document's rather than
// convenience:
//
//	Task -HAS_RUN->        TaskRun         one row per attempt
//	TaskRun -ASSEMBLED->   ContextSlice    what THIS attempt was given
//	ContextSlice -USES_CONTEXT-> source    why each node was in the prompt
//
// The last one hangs off the slice and not off the task, which the data model
// calls out as easy to get backwards: attach it to the task and attempt two
// overwrites the evidence of what attempt one received, which is the one thing
// D-6 exists to prevent.
//
// A citation with no NodeID gets no edge. Repository files are the case -- until
// M1.2 indexes a repository there is no vertex for a file to point at, and an
// edge to a vertex that does not exist is refused at commit, taking the whole
// transaction with it. The citation is still stored on the slice record, so the
// fact that code was sent survives even though the graph cannot yet say which.
func (s *ArcadeStore) writeRuns(ctx context.Context, tx *ArcadeTx, deskID string) error {
	runs, slices := s.proj.runsSnapshot(deskID)

	// Handoff documents first, because a slice's USES_CONTEXT edge below needs
	// its endpoint to exist. T-064.
	//
	// NO edge from the Product, deliberately. D-10 says the document "reaches the
	// next agent only through USES_CONTEXT on that agent's slice" -- a
	// HAS_ARTIFACT edge beside it would be a second route to the same document,
	// and the requirement is about there being exactly one.
	//
	// Each record below is written only when it changed since the graph last
	// took it (see ArcadeStore.written). Runs and slices accumulate without
	// bound, and rewriting all of them -- with an existence probe per citation
	// edge -- on every event is what held the store lock for minutes in v0.40.1.
	// A record whose optional edge was refused is not staged, so the edge is
	// tried again next time, as it always was.
	for _, a := range s.proj.artifactsSnapshot(deskID) {
		key := "Artifact/" + a.ID
		fp, same := s.unchanged(key, a)
		if same {
			continue
		}
		s.stage(key, fp)
		if err := s.upsert(ctx, tx, "Artifact", a.ID, map[string]any{
			"kind": a.Kind, "title": a.Title, "body": a.Body, "mimeType": "text/markdown",
			"taskId": a.TaskID, "deskId": a.DeskID, "runId": a.RunID, "attempt": a.Attempt,
		}); err != nil {
			return err
		}
	}

	// Slices next, and in digest order: an ASSEMBLED edge below needs its
	// endpoint to exist, and a map iterated in Go's order would write the same
	// transaction differently on every call.
	digests := make([]string, 0, len(slices))
	for d := range slices {
		digests = append(digests, d)
	}
	sort.Strings(digests)
	for _, d := range digests {
		rec := slices[d]
		key := "ContextSlice/" + d
		fp, same := s.unchanged(key, rec)
		if same {
			continue
		}
		clean := true
		// The citations go on the vertex AND, where they can, on edges.
		//
		// Not redundancy. The edge is the traversable form and the one §12's
		// queries walk, but an edge needs both endpoints to exist, and a
		// citation commonly names a vertex that does not: a repository file has
		// none until M1.2 indexes one, and a requirement has none until the
		// record is injected. Dropping the citation in that case would lose the
		// provenance of most of a slice for a reason that has nothing to do with
		// the slice. The property is the RECORD -- complete, exactly what was
		// frozen -- and load() reads it; the edges are the index over it.
		cites, err := jsonList(rec.Cites)
		if err != nil {
			return err
		}
		if err := s.upsert(ctx, tx, "ContextSlice", rec.Digest, map[string]any{
			"assembledAt": unixStamp(rec.AssembledAt), "tokenCount": rec.TokenCount,
			"nodeCount": rec.NodeCount, "digest": rec.Digest, "rendered": rec.Rendered,
			"taskId": rec.TaskID, "deskId": rec.DeskID, "attempt": rec.Attempt,
			"cites": cites,
		}); err != nil {
			return err
		}
		for _, c := range rec.Cites {
			if c.NodeID == "" || c.NodeType == "" {
				continue
			}
			if err := s.soft(s.edgeProps(ctx, tx, "USES_CONTEXT",
				"ContextSlice", rec.Digest, c.NodeType, c.NodeID,
				map[string]any{"role": c.Role, "tokens": c.Tokens}), &clean); err != nil {
				return err
			}
		}
		if clean {
			s.stage(key, fp)
		}
	}

	for _, r := range runs {
		key := "TaskRun/" + r.ID
		fp, same := s.unchanged(key, r)
		if same {
			continue
		}
		clean := true
		props := map[string]any{
			"attempt": r.Attempt, "status": r.Status, "model": r.Model,
			"startedAt": unixStamp(r.StartedAt), "invocation": r.Invocation,
			"exitCode": r.ExitCode, "tokensIn": r.TokensIn, "tokensOut": r.TokensOut,
			"failureSignature": r.FailureSignature, "logRef": r.LogRef,
			"taskId": r.TaskID, "deskId": r.DeskID,
		}
		if r.EndedAt > 0 {
			props["endedAt"] = unixStamp(r.EndedAt)
		}
		// The gateway's evidence (11-blueprint.sql), written only when present so
		// a CLI run's row carries nothing it never had.
		if r.RequestID != "" {
			props["requestId"] = r.RequestID
		}
		if r.SessionID != "" {
			props["sessionId"] = r.SessionID
		}
		if r.Provider != "" {
			props["provider"] = r.Provider // M16, 13-vendors.sql
		}
		if r.Workspace != "" {
			props["workspace"] = r.Workspace // M17.5, 15-reviewer-workspace.sql
		}
		if r.BlueprintVersion > 0 {
			props["blueprintVersion"] = r.BlueprintVersion
		}
		// Cost and its basis travel together or not at all. BR-31 forbids
		// aggregating a measured figure with a guessed one, and a costUsd
		// written beside the DDL's default basis of "estimated" would be a
		// guess this code never made.
		if r.CostBasis != "" {
			props["costUsd"], props["costBasis"] = r.CostUSD, r.CostBasis
		}
		if err := s.upsert(ctx, tx, "TaskRun", r.ID, props); err != nil {
			return err
		}
		// The task may be a desk display id rather than a planned Task vertex --
		// the operator-triggered dispatch route has no graph task behind it -- so
		// the edge is optional in exactly the way injectSubgraph's are.
		if r.BlueprintVersion > 0 {
			// BR-39: the rules this attempt ran under, as an edge an auditor can walk.
			if err := s.soft(s.edge(ctx, tx, "PINNED_TO", "TaskRun", r.ID,
				"BlueprintVersion", blueprintVersionID(r.DeskID, r.BlueprintVersion)), &clean); err != nil {
				return err
			}
		}
		if err := s.soft(s.edge(ctx, tx, "HAS_RUN", "Task", r.TaskID, "TaskRun", r.ID), &clean); err != nil {
			return err
		}
		if r.SliceDigest != "" {
			if err := s.soft(s.edge(ctx, tx, "ASSEMBLED",
				"TaskRun", r.ID, "ContextSlice", r.SliceDigest), &clean); err != nil {
				return err
			}
		}
		if clean {
			s.stage(key, fp)
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
	Text  string `json:"text"`
	Kind  string `json:"kind"`
	Clock int    `json:"clock"`
	// At is Event.At (D-081). Absent on rows written before v0.41.0, which
	// read their time from the row's own ts instead -- see load().
	At    int64        `json:"at,omitempty"`
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
	p, err := jsonMap(eventPayloadWire{Text: e.Text, Kind: e.Kind, Clock: e.T, At: e.At, Delta: e.Payload})
	if err != nil {
		return err
	}
	return tx.Command(ctx,
		`INSERT INTO Event SET id = :id, tenantId = :t, seq = :seq, type = :ty,
		 entityId = :eid, entityType = 'Workflow', actor = :actor,
		 ts = sysdate(), payload = :p`,
		map[string]any{
			"id": fmt.Sprintf("%s-ev-%d", s.tenant, e.Seq), "t": s.tenant,
			"seq": e.Seq, "ty": string(e.Type), "eid": e.DeskID, "p": p,
			// D-082: the signed-in account when an event carries one, bound,
			// never formatted into the statement.
			"actor": actorName(e.Payload.Actor),
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
	if err := s.injectBlueprint(ctx, tx, m); err != nil {
		return err
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
	return s.edgeProps(ctx, tx, label, fromType, fromID, toType, toID, nil)
}

// edgeProps is edge with properties on the edge itself.
//
// USES_CONTEXT is the reason it exists: role is mandatory on that type, so an
// edge written without one is refused, and role is the entire point of the edge
// -- "why was this node in the prompt" is the question, and an unlabelled edge
// answers only "it was".
//
// Properties are set on CREATE only. An edge that already exists is left alone,
// for the same reason the existence probe below is there rather than a retry:
// re-writing an audit edge would let a later pass restate what an earlier
// attempt was given.
func (s *ArcadeStore) edgeProps(ctx context.Context, tx *ArcadeTx, label, fromType, fromID, toType, toID string, props map[string]any) error {
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
	sets := []string{"createdAt = sysdate()"}
	i := 0
	for k, v := range props {
		p := fmt.Sprintf("e%d", i)
		sets = append(sets, k+" = :"+p)
		params[p] = v
		i++
	}
	sort.Strings(sets) // one statement per edge, byte-identical between runs
	sql := fmt.Sprintf(
		`CREATE EDGE %s FROM (SELECT FROM %s WHERE id = :a AND tenantId = :t)
		 TO (SELECT FROM %s WHERE id = :b AND tenantId = :t) SET %s`,
		label, fromType, toType, strings.Join(sets, ", "))
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
		Slug string `json:"slug"`

		Code      string          `json:"code"`
		Owner     string          `json:"owner"`
		Priority  string          `json:"priority"`
		Stage     string          `json:"stage"`
		Repos     []string        `json:"repos"`
		BRD       json.RawMessage `json:"brd"`
		Stories   json.RawMessage `json:"stories"`
		Diagrams  json.RawMessage `json:"diagrams"`
		Decisions json.RawMessage `json:"decisions"`
		Policies  json.RawMessage `json:"policies"`
		Sources   json.RawMessage `json:"knowledgeSources"`
		Workspace json.RawMessage `json:"workspace"`
		Vendors   json.RawMessage `json:"vendorPolicy"`
		QaState   json.RawMessage `json:"qaState"`
		AutoFix   json.RawMessage `json:"autoFix"`
	}
	if err := s.arc.Query(ctx,
		`SELECT id, name, repo, goal, slug, code, owner, priority, stage, repos,
		        brd, stories, diagrams, decisions, policies, knowledgeSources, workspace, vendorPolicy, qaState, autoFix
		 FROM Product WHERE tenantId = :t ORDER BY id`,
		map[string]any{"t": s.tenant}, &prows); err != nil {
		return false, err
	}
	if len(prows) == 0 {
		return false, nil
	}

	versions, err := s.loadBlueprintVersions(ctx)
	if err != nil {
		return false, err
	}
	m := newProjection()
	for _, r := range prows {
		m.products = append(m.products, Product{ID: r.ID, Name: r.Name, Repo: r.Repo, Goal: r.Goal, Slug: r.Slug})
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
		if err := decodeInto(r.Policies, &mr.Blueprint.Policies); err != nil {
			return false, err
		}
		if err := decodeInto(r.Sources, &mr.Blueprint.Sources); err != nil {
			return false, err
		}
		mr.Blueprint.Versions = versions[r.ID]
		if len(r.Workspace) > 0 && string(r.Workspace) != "null" {
			if err := decodeInto(r.Workspace, &mr.Workspace); err != nil {
				return false, err
			}
		}
		if len(r.Vendors) > 0 && string(r.Vendors) != "null" {
			if err := decodeInto(r.Vendors, &mr.Vendors); err != nil {
				return false, err
			}
		}
		if len(r.QaState) > 0 && string(r.QaState) != "null" {
			if err := decodeInto(r.QaState, &mr.Qa); err != nil {
				return false, err
			}
		}
		if len(r.AutoFix) > 0 && string(r.AutoFix) != "null" {
			if err := decodeInto(r.AutoFix, &mr.AutoFix); err != nil {
				return false, err
			}
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
	// ORDER BY productId sorts as a string, so p-10 would land between p-1 and
	// p-2 and shift the seeded desks' indices after a restart. Re-sort by
	// (id length, id): the same order for p-1..p-9, and p-10 after p-9.
	sort.SliceStable(wrows, func(i, j int) bool {
		a, b := wrows[i].ProductID, wrows[j].ProductID
		if len(a) != len(b) {
			return len(a) < len(b)
		}
		return a < b // the same order sortDeskIDs gives
	})
	// One desk per product, seated on its LATEST workflow (v0.40.5). Every
	// workflow ever created on a product is its own Workflow vertex, and this
	// loop used to seat a desk for each: after a restart the sample product had two, and
	// deskIdxByID found the older one first -- so the second workflow's tasks
	// were dispatched from the first one's desk and briefed with its goal. The
	// running MemStore never has two (CreateWorkflow re-seats the product's one
	// desk), so this puts back what was there before the restart. Every row
	// still gives its goal and its spend: the goal is what a task of an earlier
	// workflow is briefed with, and the spend is the product's total.
	latest := map[string]string{}
	for _, r := range wrows {
		if strings.TrimSpace(r.ID) != "" {
			if m.workflowGoals == nil {
				m.workflowGoals = map[string]string{}
			}
			m.workflowGoals[r.ID] = r.Goal
		}
		if n, ok := workflowNumber(r.ID); ok && n > m.workflowSeq {
			m.workflowSeq = n
		}
		cur, seen := latest[r.ProductID]
		if !seen {
			latest[r.ProductID] = r.ID
			continue
		}
		n, okN := workflowNumber(r.ID)
		c, okC := workflowNumber(cur)
		if okN && (!okC || n > c) {
			latest[r.ProductID] = r.ID
		}
	}
	for _, r := range wrows {
		if latest[r.ProductID] != r.ID {
			m.spend += r.Spent
			continue
		}
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
		Rank       int    `json:"rank"`
		Gate       bool   `json:"approvalRequired"`
		Retry      int    `json:"retryCount"`
		Esc        int    `json:"escalationCount"`
		Block      string `json:"blockReason"`
		Kind       string `json:"kind"`
		Labels     string `json:"labels"`
		DedupKey   string `json:"dedupKey"`
		AutoAt     int64  `json:"autoAt"`
		APIOwner   string `json:"apiOwner"`
		APIRefs    string `json:"apiRefs"`
		APIAt      int64  `json:"apiAt"`
		AutoDisp   bool   `json:"autoDispatch"`
	}
	if err := s.arc.Query(ctx,
		`SELECT id, workflowId, deskId, title, role, story, modelHint, status,
		        priority, ordinal, rank, approvalRequired, retryCount, escalationCount, blockReason, kind, labels, dedupKey, autoAt,
		        apiOwner, apiRefs, apiAt, autoDispatch
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
			Status: r.Status, Priority: r.Priority, Ordinal: r.Ordinal, Rank: r.Rank,
			Gate: r.Gate, Retry: r.Retry, Esc: r.Esc, BlockReason: r.Block,
			// The kind is normalised on the way in: an absent kind is an
			// implement task, and an unknown one is refused rather than guessed.
			Kind: taskKind(r.Kind),
		}
		// The labels are one JSON array on the property (D-088). A record
		// written before the property existed carries none, and that is not an
		// error: a card with no labels is a card that predates the vocabulary.
		if r.Labels != "" {
			_ = json.Unmarshal([]byte(r.Labels), &t.Labels)
		}
		t.DedupKey = r.DedupKey
		t.AutoAt = r.AutoAt
		t.APIOwner, t.APIAt, t.AutoDispatch = r.APIOwner, r.APIAt, r.AutoDisp
		if r.APIRefs != "" {
			_ = json.Unmarshal([]byte(r.APIRefs), &t.APIRefs)
		}
		for _, d := range deps {
			t.Deps = append(t.Deps, d.ID)
		}
		m.tasks = append(m.tasks, t)
	}

	// The attempts and the context each was given. T-054 and T-055, and the
	// same argument as the task graph above: neither is folded from the stream,
	// so if this could not put them back they would be memory pretending to be
	// a record -- and the thing they are a record OF is what a model was sent,
	// which is the one question BR-01's "reconstructible" promise cannot be
	// allowed to fail on.
	//
	// Slices first, because a run names one and the projection resolves the
	// name on read.
	var slrows []struct {
		Digest      string          `json:"digest"`
		TaskID      string          `json:"taskId"`
		DeskID      string          `json:"deskId"`
		Attempt     int             `json:"attempt"`
		AssembledAt string          `json:"assembledAt"`
		TokenCount  int             `json:"tokenCount"`
		NodeCount   int             `json:"nodeCount"`
		Rendered    string          `json:"rendered"`
		Cites       json.RawMessage `json:"cites"`
	}
	if err := s.arc.Query(ctx,
		`SELECT digest, taskId, deskId, attempt, assembledAt, tokenCount, nodeCount,
		        rendered, cites
		 FROM ContextSlice WHERE tenantId = :t ORDER BY digest`,
		map[string]any{"t": s.tenant}, &slrows); err != nil {
		return false, err
	}
	for _, r := range slrows {
		rec := SliceRecord{
			Digest: r.Digest, TaskID: r.TaskID, DeskID: r.DeskID, Attempt: r.Attempt,
			AssembledAt: parseStamp(r.AssembledAt), TokenCount: r.TokenCount,
			NodeCount: r.NodeCount, Rendered: r.Rendered,
		}
		// From the property, not from out('USES_CONTEXT'). The edges only exist
		// for citations whose target vertex does -- see writeRuns -- so reading
		// them back would silently return a subset of the provenance and call it
		// the whole of it, which is a worse failure than having none.
		if err := decodeInto(r.Cites, &rec.Cites); err != nil {
			return false, err
		}
		m.slices[rec.Digest] = rec
	}

	// The handoff documents, T-064. Filtered by kind rather than read whole:
	// Artifact also carries the record's diagrams, which injectSubgraph writes
	// and which are a different thing entirely.
	var arows []struct {
		ID        string `json:"id"`
		Kind      string `json:"kind"`
		Title     string `json:"title"`
		Body      string `json:"body"`
		TaskID    string `json:"taskId"`
		DeskID    string `json:"deskId"`
		RunID     string `json:"runId"`
		Attempt   int    `json:"attempt"`
		CreatedAt string `json:"createdAt"`
	}
	// The handoff documents AND the design gate's own two kinds (M17).
	//
	// Loading only the handoffs was enough while the handoff was the only
	// document a task produced. The design artifact and the review record are
	// documents too, and a restart that dropped them would leave the reviewer
	// with no design to read and the gate with no record of what it found --
	// and BR-01 says the state lives in the graph.
	//
	// Three queries rather than one with an IN: the single-kind shape is the
	// one the rest of this file uses, and a bound list is not a shape the rest
	// of this file has ever asked the database for.
	for _, kind := range []string{ArtifactHandoff, ArtifactDesign, ArtifactReview} {
		if err := s.arc.Query(ctx,
			`SELECT id, kind, title, body, taskId, deskId, runId, attempt, createdAt
			 FROM Artifact WHERE tenantId = :t AND kind = :k ORDER BY id`,
			map[string]any{"t": s.tenant, "k": kind}, &arows); err != nil {
			return false, err
		}
		for _, r := range arows {
			m.artifacts[r.ID] = ArtifactRecord{
				ID: r.ID, Kind: r.Kind, Title: r.Title, Body: r.Body,
				TaskID: r.TaskID, DeskID: r.DeskID, RunID: r.RunID, Attempt: r.Attempt,
				// The vertex's own createdAt, which is the write time rather than the
				// moment the document was produced. Close enough for the only thing
				// that reads it -- "which of these is the latest" -- and honest about
				// which clock it came from.
				CreatedAt: parseStamp(r.CreatedAt),
			}
		}
	}

	var rrows []struct {
		ID          string  `json:"id"`
		TaskID      string  `json:"taskId"`
		DeskID      string  `json:"deskId"`
		Attempt     int     `json:"attempt"`
		Status      string  `json:"status"`
		Model       string  `json:"model"`
		Invocation  string  `json:"invocation"`
		StartedAt   string  `json:"startedAt"`
		EndedAt     string  `json:"endedAt"`
		ExitCode    int     `json:"exitCode"`
		TokensIn    int64   `json:"tokensIn"`
		TokensOut   int64   `json:"tokensOut"`
		CostUSD     float64 `json:"costUsd"`
		CostBasis   string  `json:"costBasis"`
		Signature   string  `json:"failureSignature"`
		LogRef      string  `json:"logRef"`
		SliceDigest string  `json:"sliceDigest"`
		RequestID   string  `json:"requestId"`
		SessionID   string  `json:"sessionId"`
		Pinned      int     `json:"blueprintVersion"`
		Provider    string  `json:"provider"`
		Workspace   string  `json:"workspace"`
	}
	if err := s.arc.Query(ctx,
		`SELECT id, taskId, deskId, attempt, status, model, invocation, startedAt, endedAt,
		        exitCode, tokensIn, tokensOut, costUsd, costBasis, failureSignature, logRef,
		        requestId, sessionId, blueprintVersion, provider, workspace,
		        first(out('ASSEMBLED').digest) AS sliceDigest
		 FROM TaskRun WHERE tenantId = :t ORDER BY id`,
		map[string]any{"t": s.tenant}, &rrows); err != nil {
		return false, err
	}
	for _, r := range rrows {
		m.runs = append(m.runs, TaskRunRecord{
			ID: r.ID, TaskID: r.TaskID, DeskID: r.DeskID, Attempt: r.Attempt,
			Status: r.Status, Model: r.Model, Invocation: r.Invocation,
			StartedAt: parseStamp(r.StartedAt), EndedAt: parseStamp(r.EndedAt),
			ExitCode: r.ExitCode, TokensIn: r.TokensIn, TokensOut: r.TokensOut,
			CostUSD: r.CostUSD, CostBasis: r.CostBasis,
			FailureSignature: r.Signature, LogRef: r.LogRef, SliceDigest: r.SliceDigest,
			RequestID: r.RequestID, SessionID: r.SessionID, BlueprintVersion: r.Pinned,
			Provider: r.Provider, Workspace: r.Workspace,
		})
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
		TS      string          `json:"ts"`
		Payload json.RawMessage `json:"payload"`
	}
	// D-081: a row written before v0.41.0 has no "at" in its payload, and its
	// time is the row's ts -- the database's clock, printed in the database
	// server's zone with no zone attached. dbOffset is that zone, measured
	// once here, so an old row lands on the same clock as a new one.
	dbOffset := s.dbZoneOffset(ctx)
	if err := s.arc.Query(ctx,
		`SELECT seq, type, entityId, ts, payload FROM Event
		 WHERE tenantId = :t ORDER BY seq`,
		map[string]any{"t": s.tenant}, &erows); err != nil {
		return false, err
	}
	for _, r := range erows {
		var pw eventPayloadWire
		if err := decodeInto(r.Payload, &pw); err != nil {
			return false, err
		}
		at := pw.At
		if at == 0 {
			if ts := parseStamp(r.TS); ts > 0 {
				at = ts + dbOffset
			}
		}
		e := Event{Seq: r.Seq, Type: EventType(r.Type), DeskID: r.Entity,
			Text: pw.Text, Kind: pw.Kind, T: pw.Clock, At: at, Payload: pw.Delta}
		if needsNotice(e) {
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

	// The loaded projection is the live store from here on, so it gets the
	// broker -- the one already handed to listeners, if there was one.
	if s.proj != nil && s.proj.live != nil {
		m.live = s.proj.live
	} else {
		m.live = NewBroker()
	}
	cases, err := s.loadQaCases(ctx)
	if err != nil {
		return false, err
	}
	m.restoreQa(cases)
	contacts, err := s.loadContacts(ctx)
	if err != nil {
		return false, err
	}
	m.restoreContacts(contacts)
	defects, err := s.loadDefects(ctx)
	if err != nil {
		return false, err
	}
	fixprs, err := s.loadFixPRs(ctx)
	if err != nil {
		return false, err
	}
	m.restoreDefects(defects, fixprs)
	prs, err := s.loadPullRequests(ctx)
	if err != nil {
		return false, err
	}
	m.restorePullRequests(prs)
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

// parseStamp is unixStamp backwards, for the load path. An unparseable or empty
// stamp is zero rather than an error: it means "not yet", which is what an
// attempt that has not ended has, and failing a whole boot over one is a worse
// answer than a run whose end time reads as unset.
// dbZoneOffset is how far the database's printed clock is from the app's,
// rounded to a zone (tzOffset). Zero when the probe fails: an old event's time
// is display, never state, and a load must not fail over it.
func (s *ArcadeStore) dbZoneOffset(ctx context.Context) int64 {
	var rows []struct {
		Now string `json:"now"`
	}
	if err := s.arc.Query(ctx, `SELECT sysdate() AS now`, nil, &rows); err != nil || len(rows) == 0 {
		return 0
	}
	db := parseStamp(rows[0].Now)
	if db == 0 {
		return 0
	}
	return tzOffset(db, time.Now().Unix())
}

func parseStamp(s string) int64 {
	if s == "" {
		return 0
	}
	for _, layout := range []string{"2006-01-02 15:04:05", time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Unix()
		}
	}
	return 0
}

// unixStamp renders one of the domain's unix-second fields as the DATETIME the
// DDL declares. Zero means "not yet" -- an attempt that has not ended -- and
// becomes an empty string the caller is expected to leave out of the write
// rather than a stamp claiming 1970.
func unixStamp(sec int64) string {
	if sec <= 0 {
		return ""
	}
	return time.Unix(sec, 0).UTC().Format("2006-01-02 15:04:05")
}

// jsonMap and jsonList move a Go value through JSON so it reaches ArcadeDB as a
// document rather than as a string holding JSON. The difference matters on the
// way back: a document is queryable, a string is opaque.
// labelsJSON is the labels array on the task's property (D-088). An empty
// list is an empty string, not "null": the column is optional and a card
// written before the vocabulary existed carries none.
// refsJSON is the API read grants as one JSON array (D-099); "" for none.
func refsJSON(refs []string) string { return labelsJSON(refs) }

func labelsJSON(labels []string) string {
	if len(labels) == 0 {
		return ""
	}
	b, err := json.Marshal(labels)
	if err != nil {
		return ""
	}
	return string(b)
}

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
