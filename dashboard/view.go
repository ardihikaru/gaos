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

	Stats Stats
	Toast string

	BO *BOView
	CW *CWView

	// Needs is how many agents are waiting on the operator (M15): the topbar
	// chip and the back-office nav count. Zero when signed out -- the number is
	// back-office data, and a signed-out page has no inbox to open.
	Needs int
	// Answer is the answer sheet, when one is open (M15).
	Answer *AnswerView

	// ShowLogin and a non-nil BO are what decide whether each sheet is visible.
	// The server owns that, not the client: "am I signed in" has exactly one
	// answer and it is not stored in the DOM.
	ShowLogin bool
	SignedIn  bool
	Tenant    string
	LoginErr  string
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
	Board *Board
	// Timeline is the product's events, newest first (T-168). Counted on
	// every tab for the tab label; the list is rendered on its own tab only.
	Timeline   []Event
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
	// Vendors is set for the AI vendors tab only (M16).
	Vendors *VendorsPane
	// QA is set for the QA tab only (D-072, handlers_qa.go).
	QA *QAPane
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
	View    string // products | stories | diagrams | knowledge | agents | inbox
	Counts  BOCounts
	List    []ProductCard
	Detail  *ProductDetail
	Rollup  []RollupGroup
	Title   string
	Sub     string
	Agents  []AgentRow
	NewOpen bool

	// The inbox (M15). Filter is the product the list is narrowed to, "" for
	// all; Filters are the products that have someone waiting, for the chips.
	Inbox      []InboxRow
	Filter     string
	FilterName string
	Filters    []InboxFilter
}

// ---------- "Needs you" (M15) ----------

// InboxRow is one agent waiting on the operator.
type InboxRow struct {
	ProductID   string
	ProductName string
	TaskID      string
	TaskTitle   string
	RoleLabel   string
	RoleStyle   template.CSS
	Lead        string // the question's first line, clipped
	Question    string // the whole question, for the row's title
	Waiting     string // "waiting 12m"
	AskedISO    string // datetime attribute; "" when the time was not recorded
	AskedAbs    string // the absolute time, for title
	Expires     string // "expires in 29d": the question closes with its session (D-066)
	ExpiresISO  string
	Urgent      bool   // under urgentBefore left
	OpenURL     string // GET for the answer sheet
	DomID       string
}

type InboxFilter struct {
	ID, Name string
	N        int
	On       bool
}

// AnswerView is the answer sheet: one question and the four ways to answer it.
type AnswerView struct {
	ProductID   string
	ProductName string
	TaskID      string
	TaskTitle   string
	RoleLabel   string
	Waiting     string
	AskedISO    string
	AskedAbs    string
	Expires     string
	ExpiresISO  string
	Urgent      bool
	Lead        string // the title: the question's first line
	Rest        string // the rest of the question, if it ran on
	Context     string
	ContextLong bool
	ContextLead string
	Options     []AnswerOption
	// Default is the choice preselected: the recommendation, or 4 when the
	// agent offered nothing to recommend.
	Default int
	PostURL string
}

type AnswerOption struct {
	N           int
	Label       string
	Description string
	Recommended bool
	Checked     bool
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
