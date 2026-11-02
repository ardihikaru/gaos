package main

import (
	"fmt"
	"html/template"
	"strings"
)

// View structs: everything the templates need, computed in Go.
//
// TWO RULES, BOTH LEARNED FROM THE PROTOTYPE'S SHAPE.
//
// 1. No arithmetic in templates. paintFloor() and renderInspector() in the
//    prototype compute badges, stage marks, screen modes, test dots, bubble
//    text and role pills before touching the DOM. That work moves here, where
//    it is unit-testable, instead of into {{if}} chains nobody can assert on.
//
// 2. Anything that lands in a style="" attribute is template.CSS, never string.
//    html/template refuses to interpolate a plain string into a style context
//    and emits the literal text ZgotmplZ instead -- silently, with a perfectly
//    valid page that has lost every colour. The prototype does this at a dozen
//    sites (--av, --w, width, background, color), so every one of them gets a
//    typed constructor below.

func pctStyle(prop string, v float64) template.CSS {
	if v < 0 {
		v = 0
	}
	if v > 100 {
		v = 100
	}
	return template.CSS(fmt.Sprintf("%s:%.4g%%", prop, v))
}

// cssVar guards the one thing template.CSS gives up: escaping. Every caller
// passes a value from roles/stateColor, i.e. from this package, but a future
// caller might not -- so anything that is not a bare CSS custom-property
// reference is refused rather than trusted.
func cssVar(prop, v string) template.CSS {
	if !strings.HasPrefix(v, "var(--") || !strings.HasSuffix(v, ")") || strings.ContainsAny(v, ";\"'<>{}") {
		v = "var(--ink-3)"
	}
	return template.CSS(prop + ":" + v)
}

// stateColor maps a desk state to the palette token the legend uses. Keeping it
// in one function is what stops the rail dot, the inspector tag and the desk
// card drifting apart.
func stateColor(state string) string {
	switch state {
	case StateActive, StateReview:
		return "var(--run)"
	case StateCompleted:
		return "var(--done)"
	case StateBlocked:
		return "var(--block)"
	case StateMeeting:
		return "var(--know)"
	default:
		return "var(--ink-3)"
	}
}

// ---------- the render envelope ----------

// View is what every template receives. One struct rather than a per-template
// type because the out-of-band chrome (§ oob-chrome in templates) is appended
// to whatever fragment a mutation returns, and it needs Stats, NewEvents and
// Toast to be reachable from any of them.
type View struct {
	// Prefix is the mount point when Traefik strips one. It is used in exactly
	// one place -- <base href> in page.html -- and every other URL in every
	// template is relative, so this cannot be forgotten in attribute number 41.
	Prefix string

	Stats     Stats
	Toast     string
	NewEvents []EventView

	Floor    []DeskView
	Rooms    []RoomView
	Products []ProdRowView
	Insp     *InspectorView
	Events   []EventView
	Meeting  *MeetingView
	FocusIdx int

	BO *BOView
	CW *CWView

	// ShowLogin and a non-nil BO are what decide whether each sheet is visible.
	// The server owns that, not the client: "am I signed in" has exactly one
	// answer and it is not stored in the DOM.
	ShowLogin bool
	SignedIn  bool
	Tenant    string
	LoginErr  string
}

// ---------- floor ----------

type Badge struct {
	Class string
	Text  string
}

type RolePill struct {
	Style   template.CSS
	Short   string
	Name    string
	Label   string
	Stacked bool
}

type DeskView struct {
	Idx        int
	Focused    bool
	Name       string
	Repo       string
	State      string
	ScreenMode string
	TaskID     string
	TaskText   string
	IsIdle     bool
	Stage      string
	StageMarks []string
	Badges     []Badge
	TestDots   []string
	Bubble     string
	ProgStyle  template.CSS
	AvStyle    template.CSS
	RolePills  []RolePill
}

func NewDeskView(d Desk, idx, focus int) DeskView {
	v := DeskView{
		Idx: idx, Focused: idx == focus,
		Name: d.Name, Repo: d.Repo, State: d.State,
		Stage: stageOf(d), ScreenMode: screenMode[stageOf(d)],
		TaskID:    taskIDOf(d),
		IsIdle:    d.State == StateIdle,
		ProgStyle: pctStyle("width", d.Progress),
	}

	if d.Scored {
		cls := ""
		switch {
		case d.Score >= 85:
			cls = "good"
		case d.Score < 70:
			cls = "low"
		}
		v.Badges = append(v.Badges, Badge{"badge score " + cls, fmt.Sprint(d.Score)})
	}
	if d.Retry > 0 {
		v.Badges = append(v.Badges, Badge{"badge retry", fmt.Sprintf("retry %d", d.Retry)})
	}
	if d.Esc > 0 {
		v.Badges = append(v.Badges, Badge{"badge esc", "escalated"})
	}
	if d.ApprovalRequired {
		v.Badges = append(v.Badges, Badge{"badge approve", "approval"})
	}

	for i := range stages {
		switch {
		case i < d.StageIdx:
			v.StageMarks = append(v.StageMarks, "done")
		case i == d.StageIdx:
			v.StageMarks = append(v.StageMarks, "on")
		default:
			v.StageMarks = append(v.StageMarks, "")
		}
	}

	switch d.State {
	case StateIdle:
		v.TaskText = "No agent seated. The workflow is planned and waiting for capacity."
	case StateBlocked:
		v.TaskText = d.BlockReason
	default:
		v.TaskText = d.Task
	}

	if v.ScreenMode == "tests" {
		const total = 24
		pass := int(float64(total)*min1(d.Progress/100) + 0.5)
		fail := 0
		if d.Scored && d.Score < 70 {
			fail = 2
		}
		for k := 0; k < total; k++ {
			switch {
			case k < pass-fail:
				v.TestDots = append(v.TestDots, "p")
			case k < pass:
				v.TestDots = append(v.TestDots, "f")
			default:
				v.TestDots = append(v.TestDots, "")
			}
		}
	}

	switch d.State {
	case StateBlocked:
		v.Bubble = "waiting…"
	case StateMeeting:
		v.Bubble = "in meeting"
	case StateReview:
		v.Bubble = "scoring diff"
	case StateCompleted:
		v.Bubble = "lesson saved"
	case StateIdle:
		v.Bubble = "chair free"
	default:
		v.Bubble = clip(d.Task, 26)
		if len([]rune(d.Task)) > 26 {
			v.Bubble += "…"
		}
	}

	for k, id := range d.Agents {
		a := agentByID(id)
		if a == nil {
			continue
		}
		r := roles[a.Role]
		v.RolePills = append(v.RolePills, RolePill{
			Style: cssVar("--av", r.Color), Short: r.Short,
			Name: a.Name, Label: r.Label, Stacked: k > 0,
		})
	}

	if lead := leadAgent(d); lead != nil {
		v.AvStyle = cssVar("--av", roles[lead.Role].Color)
	} else {
		v.AvStyle = cssVar("--av", "var(--role-planner)")
	}
	return v
}

func min1(v float64) float64 {
	if v > 1 {
		return 1
	}
	if v < 0 {
		return 0
	}
	return v
}

// ---------- rail ----------

type RoomView struct {
	ID         string
	Name       string
	Occupancy  int
	Capacity   int
	Note       string
	Full       bool
	MeterStyle template.CSS
}

func NewRoomView(r RoomState) RoomView {
	note := r.Note
	if r.Full {
		note = "At capacity — new work queues here"
	}
	return RoomView{
		ID: r.ID, Name: r.Name, Occupancy: r.Occupancy, Capacity: r.Capacity,
		Note: note, Full: r.Full,
		MeterStyle: pctStyle("width", float64(r.Occupancy)/float64(r.Capacity)*100),
	}
}

type ProdRowView struct {
	Idx      int
	Name     string
	Score    string
	DotStyle template.CSS
	Current  bool
}

// ---------- inspector ----------

type ScoreRow struct {
	Key   string
	Width template.CSS
	Value int
}

type LogView struct {
	Cls  string
	Time string
	Line string
}

type InspectorView struct {
	Idx        int
	Name       string
	Goal       string
	State      string
	StateStyle template.CSS
	TaskID     string
	RoleLabel  string
	Model      string
	Retry      int
	Esc        int
	Cost       string
	Trace      template.HTML
	Scores     []ScoreRow
	ScoreText  string
	Ctx        []string

	// CtxFrozen and CtxDigest say WHICH slice the chips above are. T-056: a
	// frozen slice is what an attempt was given and a live one is what an
	// attempt would be given today, and a card that showed them identically
	// would be inviting the reader to treat the second as evidence.
	CtxFrozen bool
	CtxDigest string

	Logs       []LogView
	CanApprove bool

	// CanDispatch mirrors the guard in MemStore.DispatchTask rather than
	// replacing it. The store refuses a desk with work in flight whatever the
	// button says -- the UI is not the only caller this method will ever have,
	// which is the same reason RetryTask enforces its own retry guard.
	CanDispatch bool

	Hint string
}

func NewInspectorView(d Desk, idx int, ctx ContextView) *InspectorView {
	v := &InspectorView{
		Idx: idx, Name: d.Name, Goal: d.Goal, State: d.State,
		StateStyle: cssVar("color", stateColor(d.State)),
		Retry:      d.Retry, Esc: d.Esc,
		Cost:       fmt.Sprintf("$%.2f", d.Cost),
		Ctx:        ctx.Chips,
		CtxFrozen:  ctx.Frozen,
		CtxDigest:  ctx.Digest,
		CanApprove: d.ApprovalRequired,
		// Dispatch is legal only from a desk with nothing running. Blocked is
		// included because a refused dispatch lands there, and the operator
		// fixing the policy and trying again is the whole point of the
		// blocked -> active row BR-32 added to the state machine.
		CanDispatch: d.State == StateIdle || d.State == StateBlocked,
	}

	lead := leadAgent(d)
	v.TaskID = "—"
	if d.State != StateIdle {
		v.TaskID = taskIDOf(d)
	}
	v.RoleLabel, v.Model = "unassigned", "—"
	if lead != nil {
		r := roles[lead.Role]
		v.RoleLabel = r.Label
		v.Model = r.Model
		if d.Esc > 0 {
			v.Model = "claude-opus-5"
		}
		// The trace is a literal graph path. It is built here rather than in
		// the template because it interleaves <u> tags with escaped values, and
		// mixing those two in a template is how an injection gets in.
		v.Trace = template.HTML(fmt.Sprintf(
			`<u>Workflow</u> %s —HAS_TASK→ <u>Task</u> %s<br>`+
				`—EXECUTED_BY→ <u>Agent</u> %s (%s)<br>`+
				`—USES_WORKSPACE→ <u>Workspace</u> ws-%s%d<br>`+
				`—USES_CONTEXT→ <u>Requirement</u> + <u>Issue</u> from the %s master record`,
			template.HTMLEscapeString(d.WorkflowID),
			template.HTMLEscapeString(taskIDOf(d)),
			template.HTMLEscapeString(lead.ID),
			template.HTMLEscapeString(r.Label),
			template.HTMLEscapeString(strings.TrimPrefix(d.WorkflowID, "wf-")), d.Retry,
			template.HTMLEscapeString(d.Name)))
	} else {
		v.Trace = template.HTML(fmt.Sprintf(
			`<u>Workflow</u> %s is PLANNED. No task dispatched yet, so there is nothing to trace.`,
			template.HTMLEscapeString(d.WorkflowID)))
	}

	for _, dim := range scoreDims {
		val := 0.0
		for _, p := range d.ScoreParts {
			if p.Key == dim.Key {
				val = p.Value
			}
		}
		v.Scores = append(v.Scores, ScoreRow{Key: dim.Key, Width: pctStyle("width", val), Value: int(val + 0.5)})
	}
	v.ScoreText = "not scored yet"
	if d.Scored {
		v.ScoreText = fmt.Sprint(d.Score)
	}

	for _, l := range d.Logs {
		if len(v.Logs) >= 24 {
			v.Logs = v.Logs[1:]
		}
		v.Logs = append(v.Logs, LogView{Cls: l.Cls, Time: fmt.Sprintf("%04d", l.T), Line: l.Line})
	}

	switch {
	case d.ApprovalRequired:
		v.Hint = "This task is holding the merge gate. Approve to continue to deployment."
	case d.State == StateBlocked:
		v.Hint = d.BlockReason
	default:
		v.Hint = "Approving releases the merge gate. Retrying keeps the workspace and raises the model tier."
	}
	return v
}

// ---------- timeline ----------

// EventView is one timeline pill. Seq, not an index: it is what the replay link
// carries, and it names the same moment however long the stream grows.
type EventView struct {
	Seq     int
	Type    string
	Kind    string
	Text    string
	Time    string
	Current bool
}

func NewEventView(e Event, seq int, current bool) EventView {
	kind := e.Kind
	if kind == KindInfo {
		kind = ""
	}
	return EventView{Seq: seq, Type: string(e.Type), Kind: kind, Text: e.Text, Time: mmss(e.T), Current: current}
}

func mmss(s int) string { return fmt.Sprintf("%02d:%02d", s/60, s%60) }

// ---------- meeting ----------

type SeatView struct {
	Style template.CSS
	Label string
}

type MeetingView struct {
	Title    string
	Agenda   string
	Conf     int
	BarStyle template.CSS
	Step     string
	Seats    []SeatView
	Log      []MeetingLine
}

func NewMeetingView(m *Meeting, deskName string) *MeetingView {
	if m == nil {
		return nil
	}
	v := &MeetingView{
		Title: "Design review · " + deskName, Agenda: m.Reason,
		Conf: int(m.Conf + 0.5), BarStyle: pctStyle("width", m.Conf),
		Log: m.Log, Step: "Gathering",
	}
	switch {
	case m.Step >= len(meetingScript):
		v.Step = "Decision"
	case m.Step > 0:
		v.Step = "Discussion"
	}
	// Seats are laid out around the table by the seat index; the CSS positions
	// them from --i, so no transform has to be computed here.
	for _, id := range m.Participants {
		a := agentByID(id)
		if a == nil {
			continue
		}
		r := roles[a.Role]
		v.Seats = append(v.Seats, SeatView{Style: cssVar("--av", r.Color), Label: r.Short + " " + a.Name})
	}
	return v
}

// ---------- back office ----------

type BOCounts struct {
	Products, Stories, Diagrams, Knowledge, Agents int
}

type ProductCard struct {
	ID       string
	Code     string
	Name     string
	Repo     string
	Owner    string
	Problem  string
	Stories  int
	Diagrams int
	Vertices int
	Coverage int
}

type ProductDetail struct {
	// Board is set for the board tab only (M14.6).
	Board      *Board
	ID         string
	Name       string
	Goal       string
	Owner      string
	Repos      string
	Tab        string
	Master     Master
	Projection Projection
	Coverage   int
	CovStyle   template.CSS
	WithTasks  int
	Readiness  []ReadinessItem
	// The rubric's number, its band, and whether it clears BR-07's floor. The
	// checklist has always shown which signals pass; without these it could not
	// show what that adds up to, which is the half B-3 actually asks for.
	Score    int
	Band     string
	CanPlan  bool
	Diagrams []DiagramView
}

type DiagramView struct {
	Diagram
	SVG template.HTML
}

type AgentRow struct {
	Style  template.CSS
	Short  string
	Name   string
	Label  string
	Seated string
	IsOn   bool
}

type RollupGroup struct {
	ProductID string
	Name      string
	Stories   []Story
	Diagrams  []Diagram
	Decisions []Decision
}

type BOView struct {
	View    string // products | stories | diagrams | knowledge | agents
	Counts  BOCounts
	List    []ProductCard
	Detail  *ProductDetail
	Rollup  []RollupGroup
	Title   string
	Sub     string
	Agents  []AgentRow
	NewOpen bool
}

// ---------- create workflow ----------

type PickView struct {
	ID       string
	Name     string
	Stories  int
	Coverage int
	Selected bool
}

type StoryPick struct {
	Story
	Selected bool
}

type CWView struct {
	Draft     WorkflowDraft
	Picks     []PickView
	Stories   []StoryPick
	Master    *Master
	Plan      []PlanTask
	PlanGates int
	PlanDeps  int
	Hint      string
	NextLabel string
	Goal      string
	Steps     []CWStep
}

type CWStep struct {
	Label string
	Class string
}

func cwSteps(step int) []CWStep {
	labels := []string{"1 Objective", "2 Scope", "3 Plan"}
	out := make([]CWStep, len(labels))
	for i, l := range labels {
		cls := ""
		switch {
		case step == i+1:
			cls = "on"
		case step > i+1:
			cls = "done"
		}
		out[i] = CWStep{Label: l, Class: cls}
	}
	return out
}

func roleShort(key string) string { return roles[key].Short }
