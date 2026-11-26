package main

// The domain types. Every one of them is a projection of something the ArcadeDB
// model document already names -- Desk is a Workflow plus its current Task and
// TaskRun, Master is a Product plus its Requirements, Issues, Artifacts and
// Decisions. The names here follow the UI because this pass renders the UI; the
// mapping back to vertex types is in artifacts/v1/gaos-arcadedb-model.html §14.
//
// Nothing in this file has a pointer field except Meeting. Deep-copying a Desk
// is what keeps a read from aliasing the live floor (see fold.go and
// MemStore.floor), and every pointer in a value that crosses the store's lock
// is an aliasing bug waiting for a concurrent writer.

// ---------- reference data ----------

// Role is a seat on the floor. Color is a CSS custom-property reference, not a
// hex value: the palette lives in app.css and flips with the theme, so a colour
// resolved in Go would freeze one theme's answer into the markup.
type Role struct {
	Key   string
	Label string
	Short string
	Color string // "var(--role-backend)"
	Room  string
	Model string
}

type Agent struct {
	ID   string
	Name string
	Role string
}

type Room struct {
	ID       string
	Name     string
	Capacity int
	Note     string
}

type Product struct {
	ID   string
	Name string
	Repo string
	Goal string
	// Slug names the product in a URL (D-094): /p/<slug>/board. Assigned once,
	// from the name, when the product is created; never changed after that,
	// because nothing renames a product. See urls.go.
	Slug string
}

// ---------- the floor ----------

// Desk states. Each one is a distinct CSS selector in app.css (data-state), so
// adding a state means adding a rule; there is no default styling.
//
// deferred is the state D-037 adds: a task the budget guard would not dispatch,
// which keeps its place in the graph and starts again when the window moves.
// BR-34 is the reason it is a state of its own rather than "blocked" -- a
// deferral is not a failure, consumes no retry and no failure signature, and a
// desk showing "blocked" would say the opposite. The guard that produces it is
// M10.3; the state and its transitions land first, because state.go must be
// able to refuse an illegal move into it before anything can make one.
const (
	StateActive    = "active"
	StateReview    = "review"
	StateBlocked   = "blocked"
	StateCompleted = "completed"
	StateIdle      = "idle"
	StateMeeting   = "meeting"
	StateDeferred  = "deferred"
)

type LogLine struct {
	Line string
	Cls  string // t | g | y | r | p -- severity classes in app.css
	T    int
}

// ScorePart is one dimension of a review verdict. It is a slice rather than a
// map because the order is load-bearing: the weights 0.45 / 0.20 / 0.15 / 0.10
// / 0.10 are printed under the bars and must line up with them.
type ScorePart struct {
	Key    string
	Weight float64
	Value  float64
}

type Meeting struct {
	Reason       string
	Participants []string
	Step         int
	Conf         float64
	Ticks        int
	Log          []MeetingLine
}

type MeetingLine struct {
	Who  string
	Says string
}

// Desk is one workflow, seen from the floor. In graph terms: a Workflow vertex,
// its current Task, that task's most recent TaskRun, and the Review attached to
// it. The UI never shows more than one task per workflow at a time, which is
// why they are flattened here rather than modelled as a collection.
type Desk struct {
	ID   string
	Name string
	Repo string
	Goal string

	WorkflowID string
	TaskSeq    int
	State      string
	StageIdx   int
	Progress   float64

	Scored     bool
	Score      int
	ScoreParts []ScorePart

	Retry int
	Esc   int
	Cost  float64

	BlockReason      string
	ApprovalRequired bool

	Agents []string
	Task   string
	Logs   []LogLine

	Meeting *Meeting
}

// Clone deep-copies a desk. Used for every event snapshot and for every read
// that escapes the store's lock. The three slices and the one pointer are the
// whole reason this exists -- a shallow copy shares them with the simulation
// goroutine, and the replay you scrub back to would keep mutating under you.
func (d Desk) Clone() Desk {
	c := d
	c.Agents = append([]string(nil), d.Agents...)
	c.Logs = append([]LogLine(nil), d.Logs...)
	c.ScoreParts = append([]ScorePart(nil), d.ScoreParts...)
	if d.Meeting != nil {
		m := *d.Meeting
		m.Participants = append([]string(nil), d.Meeting.Participants...)
		m.Log = append([]MeetingLine(nil), d.Meeting.Log...)
		c.Meeting = &m
	}
	return c
}

func cloneDesks(in []Desk) []Desk {
	out := make([]Desk, len(in))
	for i, d := range in {
		out[i] = d.Clone()
	}
	return out
}

// clonePlannedTasks deep-copies the audit list a TaskPlanned event carries. Nil
// in, nil out: an event of any other type must not grow an empty slice on its
// way through Clone, or two payloads that are the same fact stop comparing
// equal and the purity tests start arguing about representation.
func clonePlannedTasks(in []PlannedTask) []PlannedTask {
	if in == nil {
		return nil
	}
	out := make([]PlannedTask, len(in))
	for i, t := range in {
		out[i] = t
		out[i].Deps = append([]string(nil), t.Deps...)
	}
	return out
}

// ---------- the event stream ----------

// Event is one thing that happened, and the delta it left behind.
//
// It used to carry a deep copy of the entire floor. That was the honest version
// of "we have not written the reducer yet", and it is gone: replay is
// `state(t) = fold(reduce, events)` in architecture §08, and fold.go now has
// the reducer that makes it true. What an event carries instead is Payload --
// the values the reducer needs for THIS event type and nothing else. The state
// transition itself belongs to the reducer, not to whoever wrote the event.
//
// Seq is the per-tenant monotonic counter and the fold's tie-breaker: two
// events in the same clock tick must still fold in a deterministic order, which
// is what "order-stable" means in §08.
//
// DeskID is a business id, never a physical RID -- BR-03. A restore into
// different buckets renumbers every RID, and a history keyed by one would be
// unreadable afterwards.
type Event struct {
	Seq    int
	Type   EventType
	DeskID string
	Text   string
	Kind   string
	T      int
	// At is the wall-clock time the event was emitted, unix seconds, on the
	// app's clock (D-081). T above is the simulation clock and is 0 in
	// production; At is what the card timing measures with. 0 = unknown.
	At      int64
	Payload EventPayload
}

// EventPayload is the delta. Every field is read by the reducer cases for a
// named handful of event types and ignored by all the others -- fold.go is the
// only file that may read one, and reduce() is the only function that decides
// what a given type does with it.
//
// Absolute values, not increments. `Retry: 2` rather than "add one", because a
// fold that replays an increment twice is wrong in a way that is very hard to
// see, and an absolute value folded twice is simply idempotent.
type EventPayload struct {
	// Blueprint. BlueprintPublished carries the version number here, and the
	// digest identifying what it contains in Digest below.
	Version int
	// Session is the gateway session SessionOpened / SessionLost name.
	Session string
	// To is the task status a TaskMoved reached (T-164). Reason carries the
	// same move as "FROM→TO" for a reader; the task fold reads this.
	To string

	// Desk fields. Which of these a reducer case reads is fixed per event type.
	Task       string
	Goal       string
	WorkflowID string
	Agents     []string
	StageIdx   int
	TaskSeq    int
	Progress   float64
	Score      int
	Parts      []ScorePart
	Retry      int
	Esc        int
	Cost       float64 // the desk's running total after this event, not a delta
	Meeting    *Meeting

	// Logs are the inspector lines written since the previous event on this
	// desk. They travel with the event so a replayed desk shows the log it
	// actually had, rather than the log it has now.
	Logs []LogLine

	// Egress. Read by the ContextRedacted and DispatchRefused cases in fold.go
	// and by nothing else.
	//
	// Reason duplicates Event.Text on purpose. Text is the pill's sentence and
	// may be re-worded whenever the UI is; Reason is the record. A future reader
	// asking "why was this refused" must not be reading a caption.
	//
	// Rule is the machine-readable half -- secret | local-only | size |
	// empty-slice | path-escape, and budget-cap when M10.3 reuses the event.
	// A reducer that had to parse the sentence would break the first time
	// somebody improved the wording.
	Reason     string
	Rule       string
	Redactions int    // how many parts the pass cut
	Bytes      int    // the size of the slice after redaction
	Digest     string // the cleared packet's digest; ContextSlice.digest in 04-execution.sql

	// Tasks is set by TaskPlanned and by nothing else, and no reducer case reads
	// it. See PlannedTask: the graph is not folded, so this is the audit copy of
	// what the planner produced rather than a delta anything applies.
	Tasks []PlannedTask

	// Question is set by QuestionAsked (the question) and QuestionAnswered
	// (the same question, with the answer filled in), and by nothing else.
	Question *QuestionPayload

	// D-082. Actor is the signed-in account that archived, restored or pruned
	// a conversation; Prune is a prune's certificate. Set by
	// ConversationArchived and ConversationPruned. D-083: Actor is also set on
	// QuestionAnswered and ExternalQuestionAnswered -- the session's email, or
	// "<client>/<handle>" for a machine client's reported, unverified handle
	// (eventActor, answer_source.go). It fills the Event.actor column.
	Actor string
	Prune *PruneRecord
	// Triage is the operator's disposition of a design review's findings
	// (D-084), carried by the design's send-back TaskMoved and by nothing
	// else. The send-back's two TaskMoved events also carry Actor.
	Triage []FindingTriage

	// Genesis is set by FloorOpened and by nothing else. It is the seeded floor
	// -- the `?? empty` half of §08's `snapshot_before(t) ?? empty` -- and it is
	// what lets a fold from an empty state reproduce a floor that was never
	// itself the product of an event.
	Genesis []Desk
}

// FindingTriage is the operator's triage of one finding of a design review
// (D-084): one of the four dispositions of docs/rules/gaos-design-review.md,
// or none, and an optional one-line note to the designer.
type FindingTriage struct {
	N           int    // the finding's 1-based position in the review artifact
	Disposition string // "", "CONTEST", "ESCALATE", "PROPOSE FIX", "DECLINE"
	Note        string // one line, at most maxFindingNote runes
}

// Clone deep-copies a payload. Same argument as Desk.Clone: a payload handed to
// a caller shares its slices with the stream otherwise, and the stream is
// supposed to be immutable.
//
// The five egress fields are absent here because all five are scalars and there
// is nothing for a clone to copy. Adding a slice or a pointer up there means
// adding a line down here, and this sentence is the reminder.
func (p EventPayload) Clone() EventPayload {
	c := p
	c.Agents = append([]string(nil), p.Agents...)
	c.Parts = append([]ScorePart(nil), p.Parts...)
	c.Logs = append([]LogLine(nil), p.Logs...)
	c.Tasks = clonePlannedTasks(p.Tasks)
	if p.Meeting != nil {
		m := *p.Meeting
		m.Participants = append([]string(nil), p.Meeting.Participants...)
		m.Log = append([]MeetingLine(nil), p.Meeting.Log...)
		c.Meeting = &m
	}
	if p.Genesis != nil {
		c.Genesis = cloneDesks(p.Genesis)
	}
	if p.Triage != nil {
		c.Triage = append([]FindingTriage(nil), p.Triage...)
	}
	if p.Question != nil {
		q := *p.Question
		q.Options = append([]QuestionOption(nil), p.Question.Options...)
		if p.Question.AnsweredBy != nil {
			by := *p.Question.AnsweredBy
			q.AnsweredBy = &by
		}
		c.Question = &q
	}
	if p.Prune != nil {
		r := *p.Prune
		r.Runs = append([]string(nil), p.Prune.Runs...)
		r.Bodies = append([]PrunedBody(nil), p.Prune.Bodies...)
		r.Kept = append([]string(nil), p.Prune.Kept...)
		c.Prune = &r
	}
	return c
}

// ---------- the product master ----------

type BRD struct {
	Problem    string
	Goals      []string
	Scope      []string
	OutOfScope []string
	Rules      []string
	Metrics    []string
}

type Story struct {
	ID    string
	Pri   string // must | should | could
	Role  string
	Want  string
	So    string
	AC    []string
	Tasks int
}

type Diagram struct {
	ID    string
	Kind  string // sequence | erd | flow
	Title string
	Src   string
}

type Decision struct {
	ID         string
	Title      string
	Confidence float64
}

// Master is the record a workflow is planned from. One product, one master.
// The planner reads it; the context assembler slices it into each prompt. An
// empty record is why a plan comes out vague, which is the entire argument for
// the readiness rubric in the architecture document §07.
type Master struct {
	ID        string
	Code      string
	Owner     string
	Priority  string
	Stage     string
	Repos     []string
	BRD       BRD
	Stories   []Story
	Diagrams  []Diagram
	Decisions []Decision

	// Blueprint is the product's standing technical knowledge (D-058): the
	// policies and knowledge sources a human drafts here, and the versions
	// published from them. blueprint.go.
	Blueprint Blueprint

	// Workspace is the product's ai-manager workspace and what it holds
	// (M14.4, workspace.go). Zero until the first sync.
	Workspace ProductWorkspace

	// Vendors is which AI vendors run the product's tasks (M16, vendors.go).
	// The zero value is Claude only.
	Vendors VendorPolicy

	// Qa is the product's QA catalog state: its modules, its epics, the
	// revision counter and the last outbound sync (qacatalog.go, D-072). The
	// cases themselves are QaCase vertices, not part of this document.
	Qa QaState

	// AutoFix is the product's defect auto-fix policy (D-091). Structured,
	// because GAOS enforces it; never rendered into the Blueprint.
	AutoFix AutoFix
}

// AutoFix is a product's defect auto-fix configuration (design §6.1). The zero
// value is off, with no allowed paths.
type AutoFix struct {
	// Enabled is the standing consent for automatic dispatch.
	Enabled bool
	// FixCap and TriageCap are automatic dispatches per UTC day (0 = default).
	FixCap, TriageCap int
	// AllowedGlobs are the only paths an easy fix may edit.
	AllowedGlobs []string
	// ForbiddenGlobs are extra paths never touched, on top of the built-ins.
	ForbiddenGlobs []string
	// TestGlobs say what counts as a test file.
	TestGlobs []string
	// PRTemplate is the pull-request body with named slots; empty = default.
	PRTemplate string
	// CIConfirmed: the operator confirms CI runs no secrets on gaos/fix-*.
	CIConfirmed bool
	// BaseBranch is the branch fixes are read from and opened against. Empty = the
	// repository's default branch. The product owner decides; never the agent.
	BaseBranch string
	// TargetRepos are the application repositories a defect fix may be opened
	// in (owner/repo). They are the product's repositories for this feature only:
	// the PR-review flow keeps reading Master.Repos.
	TargetRepos []string
	// AllowedBases are the branches an open fix pull request may be retargeted to (at most 8).
	// Empty = no retarget. AutomationRepos are the repositories where the QA tool opens its
	// pull requests (at most 4); they are NOT Master.Repos, so the review flow never sees them.
	AllowedBases    []string
	AutomationRepos []string
}

// Auto-fix cap defaults and ceilings (design §6.1).
const (
	autoFixFixCapDefault    = 3
	autoFixFixCapMax        = 10
	autoFixTriageCapDefault = 10
	autoFixTriageCapMax     = 50
)

// Caps are the effective daily caps: the defaults for 0, the ceiling for more.
func (a AutoFix) Caps() (fix, triage int) {
	fix, triage = a.FixCap, a.TriageCap
	if fix <= 0 {
		fix = autoFixFixCapDefault
	}
	if fix > autoFixFixCapMax {
		fix = autoFixFixCapMax
	}
	if triage <= 0 {
		triage = autoFixTriageCapDefault
	}
	if triage > autoFixTriageCapMax {
		triage = autoFixTriageCapMax
	}
	return fix, triage
}

// Clone deep-copies the slices.
func (a AutoFix) Clone() AutoFix {
	a.AllowedGlobs = append([]string(nil), a.AllowedGlobs...)
	a.ForbiddenGlobs = append([]string(nil), a.ForbiddenGlobs...)
	a.TestGlobs = append([]string(nil), a.TestGlobs...)
	a.TargetRepos = append([]string(nil), a.TargetRepos...)
	a.AllowedBases = append([]string(nil), a.AllowedBases...)
	a.AutomationRepos = append([]string(nil), a.AutomationRepos...)
	return a
}

func (m Master) Clone() Master {
	c := m
	c.Vendors = m.Vendors.Clone()
	c.Workspace = m.Workspace.Clone()
	c.Repos = append([]string(nil), m.Repos...)
	c.BRD.Goals = append([]string(nil), m.BRD.Goals...)
	c.BRD.Scope = append([]string(nil), m.BRD.Scope...)
	c.BRD.OutOfScope = append([]string(nil), m.BRD.OutOfScope...)
	c.BRD.Rules = append([]string(nil), m.BRD.Rules...)
	c.BRD.Metrics = append([]string(nil), m.BRD.Metrics...)
	c.Stories = make([]Story, len(m.Stories))
	for i, s := range m.Stories {
		s.AC = append([]string(nil), s.AC...)
		c.Stories[i] = s
	}
	c.Diagrams = append([]Diagram(nil), m.Diagrams...)
	c.Decisions = append([]Decision(nil), m.Decisions...)
	c.Blueprint = m.Blueprint.Clone()
	c.Qa = m.Qa.Clone()
	c.AutoFix = m.AutoFix.Clone()
	return c
}

// Projection is what a master record becomes in the graph once injected. The
// arithmetic is the prototype's, kept verbatim so the number the back office
// promises matches the number the injection reports.
type Projection struct {
	Vertices  int
	Edges     int
	Req       int
	Stories   int
	ACs       int
	Diagrams  int
	Decisions int
}

func (m Master) Project() Projection {
	acs := 0
	for _, s := range m.Stories {
		acs += len(s.AC)
	}
	req := len(m.BRD.Goals) + len(m.BRD.Rules)
	p := Projection{
		Req: req, Stories: len(m.Stories), ACs: acs,
		Diagrams: len(m.Diagrams), Decisions: len(m.Decisions),
	}
	p.Vertices = 1 + req + p.Stories + acs + p.Diagrams + p.Decisions + len(m.Repos)
	p.Edges = req + p.Stories*2 + acs + p.Diagrams*2 + p.Decisions + len(m.Repos)
	return p
}

// Coverage is the share of stories that have at least one planned task. A story
// with no task is work nobody has planned yet, which is the one number the back
// office puts a bar under.
func (m Master) Coverage() int {
	if len(m.Stories) == 0 {
		return 0
	}
	n := 0
	for _, s := range m.Stories {
		if s.Tasks > 0 {
			n++
		}
	}
	return int(float64(n) / float64(len(m.Stories)) * 100)
}

func (m Master) StoriesWithTasks() int {
	n := 0
	for _, s := range m.Stories {
		if s.Tasks > 0 {
			n++
		}
	}
	return n
}

// PlanFloor is BR-07's refusal line. Below an effective 45 the planner declines
// to plan and returns what is missing instead. It is the lower edge of the
// "minimum" band, and the two must stay the same number: a planner that refused
// at a different score than the band names would mean the rule and the label
// disagreed about the same record.
//
// BR-09 defines effective = raw - drift_penalty. The penalty is M2.3 and is not
// built -- it needs months since last review, boundary files changed since
// injection, and open contradictions, and the last two need M1.2 and M7. Until
// then the penalty is zero for every record, so effective IS raw and this
// compares against ReadinessScore directly. When drift lands, this comparison is
// the line that has to start subtracting; it is not a second floor to add.
const PlanFloor = 45

// ReadinessItem is one signal of the twelve-signal, hundred-point rubric that
// BRD §8.2 specifies. Points are earned whole or not at all -- a half-written
// domain-rule list is not six points of domain rules, it is a record the
// reviewer cannot score against, and partial credit is exactly how a subjective
// bar gets argued back in.
//
// Missing is B-3's missing list, carried on the item rather than derived later:
// "a refusal that does not say what is missing is a dead end", and the only
// place that knows why a signal failed is the check that failed it.
type ReadinessItem struct {
	OK      bool
	Points  int
	Label   string
	Missing string
}

// Readiness scores the record against BRD §8.2. The twelve weights sum to 100
// and are the document's, not ours.
//
// ONE SIGNAL CANNOT PASS YET, and this is deliberate rather than a bug. §8.2's
// "Verification strategy" asks how correctness is proven -- tests, replay,
// manual check. The graph schema has somewhere to put that answer
// (AcceptanceCriterion.verifiedBy, schema/02-master.sql) and the in-memory
// record does not: Story.AC is a []string of sentences. So the signal is listed,
// scores 0, and says why. The practical ceiling today is 95, which is still
// "complete", and the five points return when M1 widens the record rather than
// when someone decides the rubric is inconvenient. A rubric that quietly drops
// the signal it cannot measure is how the bar moves without anyone voting on it.
//
// Two more conditions are checked in the half the record can express:
// "Repository linked" reads §8.2's "repo indexed; entrypoint identified" and can
// only see that a repo is named, because indexing is M1.2 and nothing writes
// Repository.indexedAt yet. "Stories exist" checks as/want/so form, which is
// mechanical; §8.2's "phrased as an outcome not a task" for Goals is not, and is
// not checked.
func (m Master) Readiness() []ReadinessItem {
	must, covered, wellFormed := 0, 0, 0
	for _, s := range m.Stories {
		if s.Role != "" && s.Want != "" && s.So != "" {
			wellFormed++
		}
		if s.Pri == "must" {
			must++
			if len(s.AC) >= 2 {
				covered++
			}
		}
	}
	hasDiagram := func(kinds ...string) bool {
		for _, d := range m.Diagrams {
			for _, k := range kinds {
				if d.Kind == k {
					return true
				}
			}
		}
		return false
	}

	return []ReadinessItem{
		{len(m.BRD.Problem) >= 40, 8, "Problem statement",
			"Describe the symptom in at least a sentence — under 40 characters is a title, not a problem."},
		{len(m.BRD.Goals) > 0, 8, "Goals",
			"Record at least one goal, phrased as an outcome rather than a task."},
		{len(m.BRD.Rules) >= 3, 12, "Domain rules",
			"Record at least 3 rules a reviewer can score work against."},
		{len(m.BRD.OutOfScope) > 0, 6, "Out of scope",
			"Name at least one explicit exclusion, so the planner knows where to stop."},
		{wellFormed > 0, 10, "Stories exist",
			"Add at least one story in as / want / so form."},
		// Vacuous truth would hand 12 points to a record with no stories at all,
		// which is the opposite of what this signal is for.
		{must > 0 && covered == must, 12, "Criteria coverage",
			"Give every must story at least 2 acceptance criteria."},
		{hasDiagram("sequence", "flow"), 10, "Behaviour diagram",
			"Add a sequence or flow diagram showing the call order."},
		{hasDiagram("erd"), 8, "Data model",
			"Add an ERD recording the entities and their cardinality."},
		{len(m.Repos) > 0, 8, "Repository linked",
			"Link the repository this work lands in."},
		{len(m.Decisions) > 0, 8, "Decisions",
			"Carry at least one ADR, so the prompt inherits a decision instead of re-litigating it."},
		{len(m.BRD.Metrics) > 0, 5, "Success metrics",
			"Record at least one measurable outcome."},
		{false, 5, "Verification strategy",
			"Not yet recordable: the record has no field for how correctness is proven (M1)."},
	}
}

// ReadinessScore sums the points of the signals that pass. The BANDS are §07's
// -- 0-44 insufficient, 45-69 minimum, 70-89 working, 90-100 complete -- and the
// band is what ReadinessDropped reports crossing, so a second set of boundaries
// anywhere would mean the event and the document disagreed about what "dropped"
// meant.
func (m Master) ReadinessScore() int {
	score := 0
	for _, it := range m.Readiness() {
		if it.OK {
			score += it.Points
		}
	}
	return score
}

// ReadinessMissing is the list B-3 publishes and BR-07 returns with a refusal:
// one line per signal not earned, in rubric order, heaviest gaps first only
// insofar as the rubric already orders them.
func (m Master) ReadinessMissing() []string {
	var out []string
	for _, it := range m.Readiness() {
		if !it.OK {
			out = append(out, it.Missing)
		}
	}
	return out
}

// ReadinessBand names the band a score falls in. §07's four.
func ReadinessBand(score int) string {
	switch {
	case score >= 90:
		return "complete"
	case score >= 70:
		return "working"
	case score >= 45:
		return "minimum"
	default:
		return "insufficient"
	}
}

// ---------- aggregates the chrome reads ----------

// Stats is everything the topbar, the bell and the timeline counter need. It is
// one struct because those five numbers are swapped out of band on nearly every
// mutation, and a handler that had to assemble them field by field would
// eventually forget one.
type Stats struct {
	Tenant     string
	Health     string
	Spend      float64
	Seated     int
	AgentCount int
	Attention  int
	Notifs     int
	EventCount int
	DeskCount  int
	Clock      int
	Replay     bool
	ReplayAt   int // an event Seq, not an index into any slice
	SimRunning bool
	Paused     bool
	Speed      int
}

// Floor is a whole consistent read of the store: everything page.html needs,
// taken under one lock so the rail cannot disagree with the carousel.
type Floor struct {
	Desks    []Desk
	Rooms    []RoomState
	Stats    Stats
	FocusIdx int
	Meeting  *Meeting
	MeetDesk string
}

type RoomState struct {
	Room
	Occupancy int
	Full      bool
}

// WorkflowDraft is the create-workflow wizard's state. It round-trips through
// hidden form fields rather than a server-side session: fewer moving parts, it
// survives a restart, and it is the shape HTMX is built for.
type WorkflowDraft struct {
	Step      int
	ProductID string
	Goal      string
	Priority  string
	Gate      bool
	Stories   []string
}

// PlanTask is one row of the plan preview. Deps are task ids, not pointers,
// because DEPENDS_ON in the graph points at an id too.
//
// A slice rather than one id, and that is the whole of T-042. A single Dep can
// only ever express a line, and the plan is not a line: every story's verify
// task fans into one security review, so the security task has as many
// prerequisites as there are stories. The fan-in is the reason the field is
// plural; a chain would not need it.
//
// The ids here are plan-local -- t-1, t-2 -- because PlanWorkflow is pure and
// does not know which workflow it is planning for. CreateWorkflow qualifies
// them on the way to the graph. See TaskRecord.
type PlanTask struct {
	ID    string
	Title string
	Role  string
	Story string
	Deps  []string
	Model string
	Gate  bool
	// Kind is the task's kind (M17, D-069 clause 9). It is a PLAN-TIME
	// field, not something derived from the plan's position: the planner
	// knows which task writes a design and which one reviews it, and the
	// packet builder, the reply contract and the gate all branch on it.
	Kind string
}

// TaskRecord is the Go mirror of the Task vertex in 04-execution.sql. One per
// planned task, and the thing the scheduler traverses.
//
// What is deliberately absent is BR-25: no packet, no context slice, no handoff
// text, no log tail. Heavy payloads are stored EXTERNAL or by reference "never
// inline on a vertex the scheduler traverses", so the heavy half lives in the
// workspace and behind TaskRun.logRef, and this record stays cheap to walk.
//
// LeaseUntil and LastBeatAt are unix seconds rather than time.Time, matching
// Event.T's choice: a value the fold and the reconciler both compare, with no
// monotonic-clock reading riding along inside it.
type TaskRecord struct {
	ID         string
	WorkflowID string
	DeskID     string
	Title      string
	Role       string
	Story      string
	Model      string
	Status     string
	// Kind is the task's kind: implement, design or review (M17, D-069
	// clause 9). It is what the packet builder branches on, what the reply
	// contract is chosen by, and what the gate acts on. A task written before
	// the migration has no kind, and an empty kind is an implement task.
	Kind string
	// Labels say what kind of work this card is (D-088). The vocabulary is
	// closed: dashboard/labels.go refuses anything outside it, and the DDL
	// stores them as one JSON array.
	Labels []string
	// DedupKey is a pull-request card's de-duplication key (D-088):
	// owner/repo#n:role or owner/repo@sha:e2e-run. Empty on every other card.
	DedupKey string
	// AutoAt is the Unix second of the automatic dispatch of a defect card
	// (D-091); the daily caps count it. Zero on every other card.
	AutoAt      int64
	Deps        []string
	Priority    int
	Ordinal     int
	Gate        bool
	Retry       int
	Esc         int
	BlockReason string
	Workspace   string
	LeaseUntil  int64
	LastBeatAt  int64
	Attempt     int
	// Rank orders cards inside a board column (M14.6). Separate from Ordinal
	// on purpose: Ordinal is plan order and the scheduler sorts by it, and a
	// card dragged up a column must not silently change what runs first.
	Rank int
}

// Clone deep-copies a task record. Same argument as Desk.Clone: Deps is a slice,
// and a record handed across the store's lock that shares it is an aliasing bug
// waiting for a concurrent writer.
func (t TaskRecord) Clone() TaskRecord {
	c := t
	c.Deps = append([]string(nil), t.Deps...)
	c.Labels = append([]string(nil), t.Labels...)
	return c
}

// ---------- the attempt, and what it was given ----------

// The TaskRun lifecycle, constrained by 04-execution.sql's regexp on
// TaskRun.status. Constants rather than literals for the reason the event
// catalog is: a status this build invents is a row the DDL refuses at commit,
// and finding that out at commit time is finding it out in production.
const (
	RunStatusStarted  = "STARTED"  // the record is written; the turn is not yet submitted
	RunStatusRunning  = "RUNNING"  // the turn was submitted
	RunStatusExited   = "EXITED"   // it ended, well or badly -- the outcome is not read from here
	RunStatusTimeout  = "TIMEOUT"  // the deadline fired before the turn ended
	RunStatusOrphaned = "ORPHANED" // a restart found its request unknown to the gateway, past its deadline
	RunStatusKilled   = "KILLED"   // kept for old records; nothing writes it since M14.7 (OQ-26: no cancel)
)

// TaskRunRecord is the Go mirror of the TaskRun vertex in 04-execution.sql, one
// per ATTEMPT. D-5 and BR-22: "each attempt is its own record with its own
// model, cost, tokens, exit code and failure signature."
//
// Per attempt, not per task, and that is the whole requirement. A task that
// retries three times against three model tiers has three costs, three exit
// codes and possibly three different failure signatures; collapsing them onto
// the task would leave the retry policy reading an average of things that
// happened separately, and BR-16 routes on whether the signature CHANGED.
//
// Not to be confused with RunRecord in adapter.go, which is what recovery hands
// an adapter. That one is opaque above the adapter; this one is the graph's
// record and holds nothing mode-specific -- BR-33.
//
// Times are unix seconds, matching TaskRecord.LeaseUntil and Event.T: a value
// the fold and the reconciler both compare, with no monotonic clock reading
// riding along inside it.
//
// What is deliberately absent is BR-25: no prompt body, no log text, no packet.
// LogRef names where the output was stored and SliceDigest names the slice; the
// heavy halves live behind those two strings, so the scheduler can traverse a
// task's attempts without dragging a megabyte of transcript through the query.
type TaskRunRecord struct {
	ID      string // runIDFor(taskID, attempt) -- stable, so an adopting process finds the same row
	TaskID  string
	DeskID  string
	Attempt int
	Status  string

	Model      string
	Invocation string // InvocationCLI | InvocationAPI -- RECORDED, never branched on (BR-33)

	StartedAt int64
	EndedAt   int64
	ExitCode  int

	TokensIn  int64
	TokensOut int64
	CostUSD   float64
	CostBasis string // CostMeasured | CostEstimated | CostUnavailable (BR-31)

	FailureSignature string
	LogRef           string
	SliceDigest      string // the ContextSlice this attempt was given: the ASSEMBLED edge

	// Through the gateway (D-060, BR-39): the Blueprint version the attempt
	// pinned (0 = none), the request id recovery polls, and the session the
	// turn ran in. The session is transport (BR-41); recording its id is what
	// lets a retry resume it (D-056, M14.5).
	BlueprintVersion int
	RequestID        string
	SessionID        string
	// Provider is the vendor the attempt was sent to (M16): claude or
	// antigravity. "" on a run recorded before M16, which went to Claude.
	Provider string
	// Workspace is the gateway workspace the turn ran in (M17.5). "" on a run
	// recorded before M17.5, which ran in the product's own workspace.
	Workspace string
}

func (r TaskRunRecord) Clone() TaskRunRecord { return r }

// The USES_CONTEXT.role vocabulary, closed by 08-edges.sql's regexp.
//
// Constants for the same reason as the event catalog (D-020): a role this build
// invents is an edge the DDL refuses, and an audit edge that failed to write is
// an attempt whose context cannot be explained afterwards.
const (
	CiteRequirement = "requirement"
	CiteRule        = "rule"
	CiteBoundary    = "boundary"
	CiteStory       = "story"
	CiteCriterion   = "criterion"
	CiteDiagram     = "diagram"
	CiteDecision    = "decision"
	CiteLesson      = "lesson"
	CiteCode        = "code"
	CiteDiff        = "diff"
	CitePolicy      = "policy"
	// CiteDesign is M17's: the design artifact is cited by the part that
	// carries it, so the audit edge can say why the reviewer was given it.
	// D-069 clause 9 -- the third of the three closed vocabularies that grow.
	CiteDesign = "design"
)

// Citation is one USES_CONTEXT edge: why a node was in the prompt.
//
// Path names the packet part that carried it, and it is not decoration. The
// egress pass drops parts, and a citation whose part was dropped has to go with
// it -- otherwise the slice claims to have sent something the pass refused, and
// the audit edge becomes the least trustworthy thing in the graph.
type Citation struct {
	NodeType string // the vertex type: Requirement, Issue, AcceptanceCriterion, Artifact
	NodeID   string
	Role     string // one of the Cite* constants
	Tokens   int
	Path     string // the packet part that carried it
}

// SliceRecord is the Go mirror of the ContextSlice vertex, frozen per attempt.
// D-6 and BR-23: "the exact context slice sent to a model is frozen and stored
// per attempt."
//
// Rendered is allowed to be large -- ContextSlice.rendered is EXTERNAL in
// 10-storage.sql, so it is not on a page the scheduler walks. It is the whole
// point of the record: GET /tasks/{id}/context has to answer with what the model
// ACTUALLY received, and a slice reassembled later from a record that has since
// been edited answers a different question convincingly.
//
// Digest is the identity. It comes from the egress pass, over the surviving
// parts in path order, so two attempts that were given the same context share a
// digest and the comparison H-4 asks for is a string equality rather than a
// diff.
type SliceRecord struct {
	Digest      string
	TaskID      string
	DeskID      string
	Attempt     int
	AssembledAt int64
	TokenCount  int
	NodeCount   int
	Bytes       int
	Rendered    string
	Cites       []Citation
}

func (s SliceRecord) Clone() SliceRecord {
	c := s
	c.Cites = append([]Citation(nil), s.Cites...)
	return c
}

// ArtifactRecord is the Go mirror of the Artifact vertex, and today it carries
// exactly one thing: the handoff document an attempt produced. T-064, D-10.
//
// Stored rather than left in the workspace because the workspace is a lease. It
// is released when the run ends, reused by the next attempt, and truncated
// before that attempt starts -- so a document only on disk is evidence with an
// expiry date, and the question it answers ("what did the previous agent hand
// over") is asked precisely when it has expired.
//
// Body is the RAW document, not the parsed one. What the next agent should read
// is what the last agent wrote; handing it a re-rendering of our parse of it
// would quietly drop everything the parser ignores, including the free text
// after the front matter, which is where an agent explains itself.
type ArtifactRecord struct {
	ID        string // runID + "-handoff": one document per attempt, addressable
	Kind      string
	Title     string
	TaskID    string
	DeskID    string
	RunID     string
	Attempt   int
	Body      string
	CreatedAt int64
}

// ArtifactHandoff is the Artifact.kind for a handoff document.
const ArtifactHandoff = "handoff"

// The turn's own transcript, stored per attempt because the session that
// carried it expires (BR-41): what was sent, and everything that came back.
const (
	ArtifactPrompt = "prompt"
	ArtifactReply  = "reply"
	// ArtifactQuestion is the question a turn ended on, rendered (M15).
	ArtifactQuestion = "question"
	// ArtifactDesign is the document a design task wrote (M17, D-069 clause 9).
	ArtifactDesign = "design"
	// ArtifactReview is the gate's own record of what a reviewer found (M17,
	// D-069 clause 8). It is an artifact and not a Review: the Review vertex
	// belongs to the M5 score-band engine.
	ArtifactReview = "review"
)

// The eight kinds the DDL has always admitted and the Go side never wrote.
// They are declared here so that the vocabulary can be compared as a SET
// against the DDL's regexp, in both directions: a kind the DDL admits and
// Go does not declare is a name nothing can write, and a kind Go declares and
// the DDL does not admit is a row ArcadeDB refuses at commit.
const (
	ArtifactSequence = "sequence"
	ArtifactERD      = "erd"
	ArtifactFlow     = "flow"
	ArtifactDiff     = "diff"
	ArtifactCommit   = "commit"
	ArtifactReport   = "report"
	ArtifactLog      = "log"
	ArtifactUpload   = "upload"
)

// ContextView is what the inspector and GET /tasks/{id}/context are answered
// with. T-056.
//
// Frozen is the field that matters and the reason this is a struct rather than
// the []string it used to be. A live recomputation and a frozen slice look
// identical once they are chips on a page, and D-6 exists because they are not
// the same thing at all: one is evidence, the other is a guess that agrees with
// today's record. The caller is told which it is holding.
type ContextView struct {
	Chips       []string
	Frozen      bool
	Digest      string
	AssembledAt int64
	Attempt     int
	Cites       []Citation
}

// PlannedTask is the audit shape of one task, carried by TaskPlanned.
//
// The reducer does not read it -- the graph is not in FloorState, see the
// TaskPlanned case in fold.go. It is here because "which tasks the planner
// produced, at that moment" stops being answerable from the present graph the
// first time a dependency is rewired, and BR-04 makes the stream the record.
// An audit field, named as one so it does not read as a field nobody uses.
//
// Since T-164 it also carries what the task fold needs to rebuild a card
// (taskfold.go): its story (the board's lane), model, gate and priority. The
// ordinal is the task's index in the list and the workflow is the event's.
type PlannedTask struct {
	ID       string
	Title    string
	Role     string
	Deps     []string
	Story    string
	Model    string
	Gate     bool
	Priority int
	// Kind rides the event stream so a REPLAYED graph knows which task was a
	// design and which was a review. Without it the fold rebuilds every task
	// as an implement task, and the review branch is dead code on a board
	// that was rebuilt from the stream.
	Kind string
	// Labels and DedupKey ride the stream so a replay keeps them (D-088).
	Labels   []string
	DedupKey string
}

type ProductDraft struct {
	Name  string
	Owner string
	Repo  string
	Goal  string
	// Priority is low, medium or high; empty is medium.
	Priority string
}
