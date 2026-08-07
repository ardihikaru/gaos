package main

// The fixtures, ported verbatim from the prototype's const block
// (artifacts/v1/gaos-mission-control.html lines 1288-1338 and 2050-2153).
//
// They are seed data, not test data: MemStore starts from a copy of them and
// mutates that copy. Nothing here is written to at runtime -- if a handler ever
// needs to change one of these values, it is reading the wrong thing.
//
// When store_arcade.go lands, this file becomes the bootstrap fixture: the same
// records, written into the graph once, and MemStore stops being the default.

var roleOrder = []string{"architect", "backend", "frontend", "qa", "security", "devops", "planner"}

var roles = map[string]Role{
	"architect": {"architect", "System Architect", "SA", "var(--role-architect)", "architecture", "claude-opus-5"},
	"backend":   {"backend", "Backend Developer", "BE", "var(--role-backend)", "backend", "claude-sonnet-5"},
	"frontend":  {"frontend", "Frontend Developer", "FE", "var(--role-frontend)", "backend", "claude-sonnet-5"},
	"qa":        {"qa", "QA Reviewer", "QA", "var(--role-qa)", "qa", "claude-haiku-4.5"},
	"security":  {"security", "Security Reviewer", "SR", "var(--role-security)", "architecture", "claude-opus-5"},
	"devops":    {"devops", "DevOps", "DO", "var(--role-devops)", "release", "claude-sonnet-5"},
	"planner":   {"planner", "Planner", "PL", "var(--role-planner)", "knowledge", "claude-sonnet-5"},
}

var agents = []Agent{
	{"a-1", "Aster", "architect"},
	{"a-2", "Bolt", "backend"},
	{"a-3", "Cedar", "backend"},
	{"a-4", "Dune", "frontend"},
	{"a-5", "Ember", "qa"},
	{"a-6", "Flint", "qa"},
	{"a-7", "Grove", "security"},
	{"a-8", "Halo", "devops"},
	{"a-9", "Iris", "planner"},
}

func agentByID(id string) *Agent {
	for i := range agents {
		if agents[i].ID == id {
			return &agents[i]
		}
	}
	return nil
}

var rooms = []Room{
	{"architecture", "Architecture", 3, "Design, ADRs, trade-offs"},
	{"backend", "Backend", 4, "API, domain, migrations"},
	{"qa", "QA", 3, "Tests, regressions, scores"},
	{"release", "Release", 2, "Approvals, rollout"},
	{"knowledge", "Knowledge", 2, "Lessons, replay, ADR history"},
	{"meeting", "Meeting room", 5, "Multi-agent decisions"},
}

var stages = []string{"planning", "architecture", "coding", "testing", "review", "deployment"}

// screenMode picks what the little monitor on a desk card draws. The three
// modes are CSS states in app.css (.screen[data-mode]); there is no fourth.
var screenMode = map[string]string{
	"planning": "diagram", "architecture": "diagram", "coding": "terminal",
	"testing": "tests", "review": "terminal", "deployment": "terminal",
}

// stageRole is which role the scheduler seats when a workflow advances a stage.
// In the real thing this is the routing table from architecture §05; here it is
// the one-liner the prototype used.
var stageRole = map[string]string{
	"planning": "planner", "architecture": "architect", "coding": "backend",
	"testing": "qa", "review": "security", "deployment": "devops",
}

var taskPool = map[string][]string{
	"planning":     {"Break the goal into tasks", "Map impacted domain areas", "Estimate risk and cost"},
	"architecture": {"Draft the service boundaries", "Write the ADR", "Check the security surface"},
	"coding":       {"Add the pricing migration", "Update the invoice service", "Build the membership form", "Wire the discount rules"},
	"testing":      {"Write integration tests", "Run the regression suite", "Check rounding edge cases"},
	"review":       {"Score the diff", "Review the migration plan", "Verify the approval gate"},
	"deployment":   {"Prepare the rollout", "Run the smoke checks", "Record the lesson"},
}

// logPool is what a runner "prints". Cosmetic, but it is the only thing on the
// screen that makes a stage legible at a glance, so it stays stage-keyed.
var logPool = map[string][]string{
	"coding":       {"reading context slice (8 nodes)", "patch applied to pricing.go", "go build ./... ok", "gofmt clean"},
	"testing":      {"go test ./internal/pricing -run Round", "3 cases passed", "1 case failed: half-even vs half-up"},
	"planning":     {"traversing repository graph", "4 impacted domain areas found", "split into 6 atomic tasks"},
	"architecture": {"loading ADR-011, ADR-013", "boundary check: pricing ↔ invoicing", "risk: medium"},
	"review":       {"scoring diff against expected_output", "reviewer verdict pending", "checking approval policy"},
	"deployment":   {"building release artifact", "smoke checks queued", "rollback plan recorded"},
}

// scoreDims are the review weights from architecture §06:
//
//	final = 0.45*correctness + 0.20*completeness + 0.15*safety
//	      + 0.10*quality + 0.10*efficiency
//
// The order is printed under the bars in the inspector, so it is fixed.
var scoreDims = []ScorePart{
	{Key: "correctness", Weight: 0.45},
	{Key: "completeness", Weight: 0.20},
	{Key: "safety", Weight: 0.15},
	{Key: "quality", Weight: 0.10},
	{Key: "efficiency", Weight: 0.10},
}

var seedProducts = []Product{
	{"p-1", "Laundry Membership", "loop/laundry-api", "Add membership pricing and discounts"},
	{"p-2", "Invoice Engine", "loop/billing-core", "Rework invoice rounding"},
	{"p-3", "Fleet Dispatch", "loop/dispatch", "Route optimisation v2"},
	{"p-4", "Partner Portal", "loop/portal-web", "Self-serve onboarding"},
	{"p-5", "Ops Warehouse", "loop/warehouse", "Stock reconciliation job"},
}

// seedDeskState is the starting position of the floor: which desks are busy,
// how far along, what they have already spent. Index-aligned with seedProducts.
var seedDeskState = []struct {
	State    string
	StageIdx int
	Progress float64
	Cost     float64
	Agents   []string
}{
	{StateActive, 0, 12, 0.42, []string{"a-1"}},
	{StateActive, 2, 58, 1.86, []string{"a-2", "a-3"}},
	{StateActive, 1, 34, 0.91, []string{"a-1"}},
	{StateActive, 3, 71, 2.35, []string{"a-5"}},
	{StateIdle, 0, 0, 0, nil},
}

var meetingScript = []MeetingLine{
	{"Facilitator", "Agenda locked: rounding drift in the discount path."},
	{"Architect", "Proposal A keeps rounding in the invoice aggregate."},
	{"Backend", "Proposal B moves it into the pricing service."},
	{"Security", "B widens the audit surface. A is easier to prove."},
	{"QA", "Regression from ADR-011 already covers A."},
	{"Recorder", "Consensus on A. Writing ADR-014 and two follow-up tasks."},
}

// seedMasters is the product master. Five records at four different levels of
// completeness on purpose -- p-1 is a full record, p-5 is nearly empty -- so the
// readiness panel and the coverage bar have something honest to say.
var seedMasters = []Master{
	{
		ID: "p-1", Code: "LM", Owner: "team-a", Priority: "high", Stage: "live",
		Repos: []string{"loop/laundry-api", "loop/laundry-web"},
		BRD: BRD{
			Problem: "Repeat customers pay the same as walk-ins, so there is no reason to come back. Staff hand out ad-hoc discounts that never reach the invoice.",
			Goals: []string{
				"Sell a paid membership with a fixed monthly fee",
				"Apply the member rate automatically at checkout",
				"Show members what they saved on every invoice",
			},
			Scope: []string{
				"Membership plans and pricing",
				"Member discount in the invoice engine",
				"Member badge on the customer record",
				"Revenue report split by member and non-member",
			},
			OutOfScope: []string{"Loyalty points", "Referral rewards", "Family or corporate accounts"},
			Rules: []string{
				"A customer holds at most one active membership",
				"Discount applies to service lines only, never to delivery fees",
				"Rounding is half-up to the smallest currency unit",
				"An expired membership must never silently renew",
			},
			Metrics: []string{
				"30% of active customers on a plan within two quarters",
				"Member repeat rate above 60%",
				"Zero rounding disputes after launch",
			},
		},
		Stories: []Story{
			{"US-01", "must", "returning customer", "subscribe to a membership plan", "I pay less on every order",
				[]string{"Plans are listed with price and benefits", "Payment failure leaves no partial membership", "Confirmation shows the start and renewal date"}, 3},
			{"US-02", "must", "cashier", "see the member rate applied automatically", "I never calculate a discount by hand",
				[]string{"Member price replaces list price at checkout", "The invoice line shows the original price and the saving", "Non-service lines keep the list price"}, 2},
			{"US-03", "should", "owner", "see revenue split by member and non-member", "I can tell whether the plan pays for itself",
				[]string{"Report filters by date range", "Discount total is shown separately from revenue"}, 1},
			{"US-04", "could", "member", "pause my plan for a month", "I am not charged while I travel",
				[]string{"A paused plan does not renew", "Pausing is limited to once per quarter"}, 0},
		},
		Diagrams: []Diagram{
			{"DG-01", "sequence", "Member checkout",
				"Cashier -> OrderAPI: submit order\nOrderAPI -> Membership: look up active plan\nMembership --> OrderAPI: plan + rate\nOrderAPI -> Pricing: apply member rate\nPricing --> OrderAPI: priced lines\nOrderAPI -> Invoice: issue invoice\nInvoice --> Cashier: total + saving"},
			{"DG-02", "erd", "Membership data model",
				"Customer 1---0..1 Membership\nMembership *---1 Plan\nPlan 1---* PriceRule\nInvoice *---1 Customer\nInvoiceLine *---1 Invoice\nInvoiceLine 0..1---1 PriceRule   (which rule set the price)"},
		},
		Decisions: []Decision{
			{"ADR-011", "Money is stored as integer minor units", 0.94},
			{"ADR-014", "Rounding lives in the pricing service, not the invoice aggregate", 0.86},
		},
	},
	{
		ID: "p-2", Code: "IE", Owner: "team-a", Priority: "high", Stage: "live",
		Repos: []string{"loop/billing-core"},
		BRD: BRD{
			Problem: "Invoice totals drift by one minor unit when several discounted lines are combined, and finance reconciles the difference by hand every month.",
			Goals: []string{
				"One rounding rule across every price path",
				"Reproduce any historical invoice exactly",
				"Remove the manual reconciliation step",
			},
			Scope:      []string{"Rounding in the pricing service", "Backfill check against last year's invoices", "Regression suite for combined discounts"},
			OutOfScope: []string{"Tax engine rewrite", "Multi-currency invoices"},
			Rules: []string{
				"Round once, at the line level, never on the total",
				"Half-up, and the same rule in reports and invoices",
				"Any change to a rule must be versioned and dated",
			},
			Metrics: []string{"Zero reconciliation entries for two closing cycles", "100% match on a 12-month replay"},
		},
		Stories: []Story{
			{"US-01", "must", "finance officer", "invoice totals that match the sum of the lines", "I stop adjusting the ledger by hand",
				[]string{"Line totals sum exactly to the invoice total", "A replay of last year's invoices matches to the minor unit"}, 4},
			{"US-02", "must", "engineer", "one rounding helper used everywhere", "reports and invoices cannot disagree",
				[]string{"Only one rounding function is exported", "A lint rule fails a direct float division on money"}, 2},
		},
		Diagrams: []Diagram{
			{"DG-01", "flow", "Where rounding may happen",
				"list price\n   |\n   v\ndiscount rule ---> rounded HERE (line level, half-up)\n   |\n   v\nline total ------> summed, never rounded again\n   |\n   v\ninvoice total"},
		},
		Decisions: []Decision{{"ADR-011", "Money is stored as integer minor units", 0.94}},
	},
	{
		ID: "p-3", Code: "FD", Owner: "team-b", Priority: "medium", Stage: "live",
		Repos: []string{"loop/dispatch"},
		BRD: BRD{
			Problem:    "Routes are assigned in the order orders arrive, so drivers cross the same district several times a day.",
			Goals:      []string{"Group nearby pickups into one run", "Respect the promised pickup window", "Let a dispatcher override any suggestion"},
			Scope:      []string{"Route grouping", "Driver capacity", "Dispatcher override log"},
			OutOfScope: []string{"Live traffic data", "Driver payroll"},
			Rules: []string{
				"A run never exceeds the vehicle capacity",
				"A promised window is never broken to save distance",
				"Every override records who made it and why",
			},
			Metrics: []string{"15% less distance per completed order", "No increase in late pickups"},
		},
		Stories: []Story{
			{"US-01", "must", "dispatcher", "suggested runs grouped by district", "I stop building routes by hand",
				[]string{"Suggestions respect capacity and windows", "I can accept, edit, or reject a suggestion"}, 2},
			{"US-02", "should", "driver", "my run in pickup order", "I do not backtrack",
				[]string{"Stops are ordered", "Changes push a notification"}, 0},
		},
		Diagrams: []Diagram{
			{"DG-01", "sequence", "Run assignment",
				"Dispatcher -> Dispatch: request suggestions\nDispatch -> Router: group open orders\nRouter --> Dispatch: candidate runs\nDispatch --> Dispatcher: runs + reasons\nDispatcher -> Dispatch: accept run\nDispatch -> Driver: assign stops"},
		},
	},
	{
		ID: "p-4", Code: "PP", Owner: "team-b", Priority: "medium", Stage: "live",
		Repos: []string{"loop/portal-web"},
		BRD: BRD{
			Problem:    "Every new partner is onboarded over email, so it takes about nine days and nobody can tell where a request is stuck.",
			Goals:      []string{"Partners register themselves", "Documents are checked in one queue", "Both sides can see the current step"},
			Scope:      []string{"Registration", "Document upload and review", "Status tracking"},
			OutOfScope: []string{"Contract e-signature", "Commission setup"},
			Rules: []string{
				"A partner cannot trade before approval",
				"Rejected documents state a reason",
				"Personal data is redacted from logs",
			},
			Metrics: []string{"Onboarding under two days", "80% of partners finish without support"},
		},
		Stories: []Story{
			{"US-01", "must", "prospective partner", "register and upload my documents", "I do not wait on email",
				[]string{"Progress is saved between sessions", "Rejections say what to fix"}, 3},
		},
	},
	{
		ID: "p-5", Code: "OW", Owner: "team-c", Priority: "low", Stage: "draft",
		Repos: []string{"loop/warehouse"},
		BRD: BRD{
			Problem:    "Counted stock and recorded stock disagree, and nobody knows when the gap appeared.",
			Goals:      []string{"Reconcile stock nightly", "Explain every variance"},
			Scope:      []string{"Nightly reconciliation job", "Variance report"},
			OutOfScope: []string{"Purchase ordering"},
			Rules:      []string{"A variance is never auto-corrected without a reason code"},
			Metrics:    []string{"Variance under 0.5% of item value"},
		},
	},
}
