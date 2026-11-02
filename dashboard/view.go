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
