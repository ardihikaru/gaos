package main

// The domain types. Every one of them is a projection of something the ArcadeDB
// model document already names -- Desk is a Workflow plus its current Task and
// TaskRun, Master is a Product plus its Requirements, Issues, Artifacts and
// Decisions. The names here follow the UI because this pass renders the UI; the
// mapping back to vertex types is in artifacts/v1/gaos-arcadedb-model.html §14.
//
// Nothing in this file has a pointer field except Meeting. Deep-copying a Desk
// is what makes replay possible (see MemStore.emit), and every pointer in a
// value that gets snapshotted is an aliasing bug waiting for a concurrent
// writer.

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
}

// ---------- the floor ----------

// Desk states. Each one is a distinct CSS selector in app.css (data-state), so
// adding a state means adding a rule; there is no default styling.
const (
	StateActive    = "active"
	StateReview    = "review"
	StateBlocked   = "blocked"
	StateCompleted = "completed"
	StateIdle      = "idle"
	StateMeeting   = "meeting"
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

// ---------- the event stream ----------

// Event carries a full snapshot of the floor at the moment it was written.
//
// That is expensive and it is on purpose for this pass: replay is
// `state(t) = fold(reduce, events)` in the architecture document, and a fold
// needs either a pure reducer over every event type or a snapshot to read
// straight back. Snapshots are the honest version of "we have not written the
// reducer yet" -- the alternative is a reducer that silently disagrees with the
// simulation and produces a replay nobody can trust.
//
// Capped at maxEvents, same as the prototype. When the ArcadeDB store lands the
// snapshot moves to the Snapshot document type (one per 500 events) and this
// field becomes a scope id.
type Event struct {
	Seq      int
	Type     EventType
	DeskID   string
	Text     string
	Kind     string
	T        int
	Snapshot []Desk
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
}

func (m Master) Clone() Master {
	c := m
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

// ReadinessItem is one line of the planner-readiness checklist. The full
// 100-point rubric from the architecture document §07 is pass 2; this is the
// prototype's seven-item boolean version, which is what the UI draws.
type ReadinessItem struct {
	OK    bool
	Label string
}

func (m Master) Readiness() []ReadinessItem {
	everyStoryHasAC := len(m.Stories) > 0
	for _, s := range m.Stories {
		if len(s.AC) == 0 {
			everyStoryHasAC = false
			break
		}
	}
	return []ReadinessItem{
		{len(m.BRD.Problem) > 40, "Problem statement"},
		{len(m.BRD.Goals) > 0, "Goals"},
		{len(m.BRD.Rules) > 0, "Domain rules"},
		{len(m.Stories) > 0, "At least one story"},
		{everyStoryHasAC, "Every story has criteria"},
		{len(m.Diagrams) > 0, "A diagram to reason from"},
		{len(m.Repos) > 0, "Repository linked"},
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
	ReplayAt   int
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

// PlanTask is one row of the plan preview. Dep is a task id, not a pointer,
// because DEPENDS_ON in the graph points at an id too.
type PlanTask struct {
	ID    string
	Title string
	Role  string
	Story string
	Dep   string
	Model string
	Gate  bool
}

type ProductDraft struct {
	Name  string
	Owner string
	Repo  string
	Goal  string
}
