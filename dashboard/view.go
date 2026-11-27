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
	// BoardLive is set when a mutation made on the Board is answered with the
	// Board's changed columns instead of the whole sheet (D-095).
	BoardLive *BoardView

	// Needs is how many agents are waiting on the operator (M15): the topbar
	// chip and the back-office nav count. Zero when signed out -- the number is
	// back-office data, and a signed-out page has no inbox to open.
	Needs int
	// Answer is the answer sheet, when one is open (M15).
	Answer *AnswerView
	// Timing is the open timing sheet (D-081, timing.go), or nil.
	Timing *TimingView
	// Talk is the open conversation sheet (D-082, conversation.go), or nil.
	Talk *ConversationView
	// SendBack is the open send-back sheet (D-084, sendback.go), or nil.
	SendBack *SendBackSheetView

	// ShowLogin and a non-nil BO are what decide whether each sheet is visible.
	// The server owns that, not the client: "am I signed in" has exactly one
	// answer and it is not stored in the DOM.
	ShowLogin  bool
	SignedIn   bool
	Tenant     string
	LoginErr   string
	LoginEmail string // what was typed, so a wrong password does not clear the email field
}

// ---------- back office ----------

type BOCounts struct {
	Products, Stories, Diagrams, Knowledge, Agents int
	Archived                                       int // D-082: archived conversations
}

type ProductCard struct {
	ID       string
	Slug     string
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
	// BoardView is set for the board tab only: one page of the board, and
	// its filter bar (D-095, board_view.go).
	BoardView *BoardView
	// TimelineN is how many events the product has: the Timeline tab's
	// number, on every tab, without loading the events (D-095).
	TimelineN int
	// Timeline is the product's events, newest first (T-168). Counted on
	// every tab for the tab label; the list is rendered on its own tab only.
	Timeline   []Event
	Pager      Pager // the Timeline tab's pages (D-097)
	ID         string
	Slug       string // the product's name in a URL (D-094)
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
	// Assigned are the products the agent is assigned to (D-102), by name.
	Assigned []AgentProduct
}

// AgentProduct is one product an agent is assigned to.
type AgentProduct struct{ ID, Name string }

// AgentAssign is the Agents page's product filter and assignment editor (D-102).
type AgentAssign struct {
	Product     string // the validated ?product=, or ""
	ProductName string
	Configured  bool // the product's AssignedSet
	Admin       bool // the signed-in account may edit (GAOS_AUTOFIX_ADMINS)
	None        bool // configured with no agent: no PR flow runs
	Open        bool // the editor is shown open (after a save)
	Opts        []AFOpt
	Picks       []AgentPick // the editor: every agent, gated roles first
	// Release B (D-103 §2.1): the product's repositories with their kinds, and the two switches.
	Repos          []RepoKindPick
	StagingCreates bool
	AutoMergeSpecs bool
	AutoMergeBand  bool // the product's band allows auto-merge today (working or complete)
	Band           string // the product's readiness band today, for the auto-merge hint
	KindsSet       int    // how many of Repos have a kind, for the closed summary
	KindsOpen      bool   // the kinds editor is shown open (after a kinds save)
}

// RepoKindPick is one repository row of the kinds editor.
type RepoKindPick struct {
	Repo, Kind string
}

// AgentPick is one checkbox of the assignment editor.
type AgentPick struct {
	ID, Name, Label string
	On, Gated       bool
}

type RollupGroup struct {
	ProductID string
	Name      string
	Stories   []Story
	Diagrams  []Diagram
	Decisions []Decision
}

type BOView struct {
	// Href is the canonical address of what this view shows, relative to the
	// page's base and without a leading slash ("" is the product list,
	// "p/route-planner/board", "prs?state=merged"); "-" when the view has none.
	// PageTitle is the document title. Both are set by buildBO (urls.go, D-094).
	Href      string
	PageTitle string

	View    string // products | stories | diagrams | knowledge | agents | inbox
	Counts  BOCounts
	List    []ProductCard
	Detail  *ProductDetail
	Rollup  []RollupGroup
	Title   string
	Sub     string
	Agents  []AgentRow
	Assign  AgentAssign
	NewOpen bool

	// The list pages' pages (D-097): read with pageParam, then overwritten
	// with the clamped page, which is all address() ever writes. Pager is the
	// rollup's or the agents'; Archive's two live on ArchivePage.
	Page, APage, PPage int
	Pager              Pager

	// The inbox (M15). Filter is the product the list is narrowed to, "" for
	// all; Filters are the products that have someone waiting, for the chips.
	Inbox      []InboxRow
	Filter     string
	FilterName string
	Filters    []InboxFilter
	// Recent is "Answered recently" (D-083): who answered what, newest first.
	Recent []RecentRow

	// The Archive page (D-082).
	Archive *ArchivePage

	// The Integrations page (D-085): read-only, never a secret.
	Integrations *IntegrationsPage

	// The Auto-fix page (D-091, design §9.2): read-only, never a token or an excerpt.
	AutoFix *AutoFixPage

	// The Pull requests page (D-092, design §5): read-only, no GitHub call.
	PRs *PRPage
}

// ---------- Integrations (D-085) ----------

// IntegrationsPage is what GAOS is connected to and whether each connection
// works. Every value is a host, a mode, a count, a time or a yes/no: no token,
// digest, credential, URL path or query, and no text an external system wrote.
type IntegrationsPage struct {
	AIM       AIMStatus
	QA        QAStatus
	API       APIStatus
	Clients   []ClientRow
	BrainTown BrainTownRow
	// BrainTownConsumer is the Brain Town *reader* (D-087): GAOS pulling the
	// town's pull-request events. It is the opposite direction of BrainTown
	// above, which is GAOS pushing its own changes out.
	BrainTownConsumer BrainTownRow
	// PR is the pull-request flow (D-088): the GitHub client, the staging
	// callback, WhatsApp and the contact registry. Counts, hosts, modes and
	// times; the contact list carries the destination masked, never whole.
	PR PRFlowRow
	// TM is TaskMgr, the product-bound third-party platform (D-104): counts for everyone, the
	// people table for an auto-fix admin only, every number masked.
	TM TaskMgrRow

	// The status chip on each integration's heading (D-098), filled by
	// integChips from the fields above. See docs/designs/integrations-status-chips.md.
	AIMChip, BrainTownChip, BrainTownConsumerChip, PRChip, QAChip, APIChip, TMChip StatusChip
}

// StatusChip is one coloured state chip on the Integrations view. Tone is
// exactly one of "ok", "warn", "bad"; the template prints it as the CSS class
// "st-<tone>". Label is one word or phrase from a closed set (integChips) --
// never a reason string, a host or anything taken from configuration.
type StatusChip struct{ Label, Tone string }

// PRFlowRow is the pull-request flow on the Integrations view.
type PRFlowRow struct {
	GitHub       string // on | off (<reason>)
	WriteToken   string // the auto-fix write token's KIND: fine-grained | classic (broad) | not configured
	StagingState string
	StagingLast  []StagingSeen
	WA           waStatus
	Contacts     []ContactRow

	// The per-row chips (D-098), filled by integChips.
	GitHubChip, StagingChip, WAChip StatusChip
}

// StagingSeen is the last accepted staging callback of a product.
type StagingSeen struct{ Product, At string }

// ContactRow is one contact, masked.
type ContactRow struct {
	ProductID, Login, Name, Destination, NotifyOn string
}

// AIMStatus is the ai-manager gateway.
type AIMStatus struct {
	Configured   bool
	Adapter      string // describeAdapter: "ai-manager (gateway)" or "unconfigured"
	Host         string // scheme://host[:port] of the adapter's base; "" when refused or unset
	Runner       string // off | manual | on
	BootCheck    string // "passed at boot, 3h ago" or "not run (no gateway)"
	Providers    []string
	ProviderNote string // why there is no list, or the cache's age
	LastTurn     string // "RunExited · Product · 4m ago"; "" when no turn has run
}

// QAStatus is the QA tools: the outbound sync and the answer webhook.
type QAStatus struct {
	Outbound     bool
	OutboundHost string
	OutboundWhy  string
	Origin       string
	LastDelivery string // "4m ago · QA tool answer: attempt 2 retry"; "" when none
	Pending      int
	PendingNote  string // the newest waiting item's last attempt and GAOS's reason
	GaveUp       int
	// GaveUpRecent counts the gave-up events of the last 24 hours. Only the
	// chip uses it, so the chip turns green again a day after the last one.
	GaveUpRecent int
	// Failing: the newest attempted waiting item's last attempt failed.
	Failing bool
}

// APIStatus is the machine API's own configuration.
type APIStatus struct {
	State               string
	Admin, QA, Outbound bool
	QAProducts          []string
	// The external-tasks API (D-099): how many clients hold the tasks capability,
	// and the products whose API cards start unattended under manual.
	TaskClients int
	Autorun     []string
}

// ClientRow is one machine client. There is no token field on purpose.
type ClientRow struct {
	Name, Kind, Origin, Scope string
	Products                  []string
}

// ---------- "Needs you" (M15) ----------

// InboxRow is one agent waiting on the operator.
type InboxRow struct {
	ProductID   string
	ProductName string
	TaskID      string
	TaskTitle   string
	Source      string // the asker's tag ("QA tool", or a client's name) for an item that belongs to no task (D-078); "" for a task's question
	Asker       string // "the QA tool", or the client's name (D-083)
	Pushed      bool   // the asker is told by webhook; any other client polls
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

// RecentRow is one line of "Answered recently" (D-083). Plain strings: the
// actor is a client's reported handle, so html/template escapes all of it.
type RecentRow struct {
	ProductName string
	Lead        string // the question's first line, clipped
	TaskID      string // a task question's task; "" for an inbox item
	Tag         string // an inbox item's origin tag ("QA tool", a client's name)
	Title       string
	Outcome     string // the option's label, "own words: …", or "expired unanswered"
	By          string // "answered via QA tool (…) by ardi"; "" for an expiry
	WhenISO     string
	When        string // "5m ago"
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
	Source      string // as on InboxRow
	Asker       string // as on InboxRow
	Pushed      bool
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
	// Locked is set when the wizard was opened from a product (lock=1) and that
	// product exists: step 1 shows it instead of the picker. Presentation
	// only -- lock and product are the client's values (design §3.6, OQ-37).
	Locked     bool
	LockedName string
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

// BrainTownRow is the Brain Town feed on the Integrations view (D-086). State, host and
// counts only: no token, no URL path, no title.
type BrainTownRow struct {
	State   string // disabled | enabled | unusable: <reason>
	Host    string
	Queue   int
	Last    string // "202 · 2026-11-22T10:00:00Z" or "timeout · …"
	Sent    int
	Failed  int
	Dropped int
	// Failing: the last attempt failed. It clears on the next success. Only
	// the yes/no reaches the page, never the error's text.
	Failing bool
	// Cursor is the reader's cursor state: "saved <seq>" or "not saved", plus
	// "· gave up <seq>" after the retry bound (design §3.2). Empty for the feed.
	Cursor string
}

// TaskMgrRow is the TaskMgr block on the Integrations view (D-104 §7).
type TaskMgrRow struct {
	State       string // on · off (<why>) · unusable: <why>
	Host        string // the configured ip:port
	Product     string // the product with the enabled binding; "" none
	Code        string // its TaskMgr product code
	InTemplates bool   // the product is in GAOS_PR_TEMPLATE_PRODUCTS
	Breaker     string // "" closed, "auth" or "unavailable" open
	Refreshed   string // "4m ago"; "" never
	Stale       bool
	LastErr     string // an error class, never TaskMgr's message
	Counts      tmCounts
	Admin       bool
	People      []TMPersonRow // filled for an admin only
	Products    []TMBindRow
}

// TMPersonRow is one TaskMgr person, the number masked.
type TMPersonRow struct{ Name, Role, Login, Number string }

// TMBindRow is one product's binding, for the admin form.
type TMBindRow struct {
	ID, Name, Code string
	Enabled        bool
}
