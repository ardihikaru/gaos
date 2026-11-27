package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// The back office's Auto-fix view (D-091, design §9.2): the product owner's
// window onto autonomous work. Read-only. It is built from the records GAOS
// already holds -- Defect, FixPR, the defect cards and the product's AutoFix --
// and it renders:
//
//   - no token and no secret: only the KIND of the write token;
//   - no excerpt of failure output: Defect.Excerpt is never read here;
//   - no HTML from defect text: every string reaches html/template as a string
//     (the escaping is the template's), and is first stripped of control and
//     bidirectional characters and screened by the egress detectors.
//
// The view is addressed by its query: product, state, difficulty and open (a
// pull-request record, shown in the drawer). It works as plain links and a plain
// GET form; htmx only keeps the sheet in place.

// afPollSeconds is the htmx poll of design §9.2.
const afPollSeconds = 30

// afStateOrder is the table's states, the active ones first.
var afStateOrder = []struct{ Value, Label string }{
	{FixPRPreparing, "Preparing"},
	{FixPRDraft, "Draft"},
	{FixPRReady, "Ready"},
	{FixPRMerged, "Merged"},
	{FixPRClosed, "Closed"},
}

var afDifficulties = []string{"easy", "medium", "complex"}

// afBlockWords says in words what each BLOCKED reason of a defect card needs.
// A reason not in this table is shown generically: the view never prints a
// block reason it does not know.
var afBlockWords = map[string]string{
	"autofix-not-easy":          "the verdict is not an easy backend fix, so a person moves the card",
	"autofix-no-repo":           "no repository of this product matches the defect",
	"autofix-limits":            "the fix went over GAOS's size limits (3 files, 40 code lines, 120 test lines)",
	"autofix-egress":            "held back: the packet matched a secret detector",
	"autofix-ci-secrets":        "a workflow would hand secrets to a gaos/fix-* branch",
	"autofix-branch-exists":     "the fix branch already exists and is not GAOS's",
	"autofix-base-moved":        "a file changed on the default branch after it was read",
	"autofix-unreadable":        "the agent's reply could not be read",
	"autofix-pr-closed":         "the pull request was closed unmerged; a person decides what next",
	"autofix-search-failed":     "GitHub code search failed; retry the card",
	"autofix-draft-unsupported": "GitHub refused a draft pull request on this plan",
}

var afReasonCode = regexp.MustCompile(`^autofix-[a-z-]{1,40}$`)

// ---------------------------------------------------------------- view model

// AutoFixPage is what the template prints. Every string in it is plain text.
type AutoFixPage struct {
	// OOB is set by the poll route: the fragment replaces #afLive in place.
	OOB     bool
	PollURL string

	Token  AFToken
	GitHub AFGitHub
	Tiles  []AFTile
	Disp   AFDispatch

	Products     []AFProduct
	AnyOff       bool
	ProductsMore int
	Waiting      []AFWait
	WaitPager    Pager // five cards a page (D-108 design §7.1)

	// Sections is the nav of the three sections (design §7.2); Section is the active one
	// ("" = Fix pull requests). Dups are the duplicate findings (§5); Admin is whether the
	// viewer may close one.
	Sections []AFTab
	Section  string
	Dups     []AFDup
	Admin    bool

	Filter      AFFilter
	StateTabs   []AFTab
	ProductOpts []AFOpt
	DiffOpts    []AFOpt
	ResetHref   string

	Rows      []AFRow
	RowsTotal int
	Pager     Pager // ten rows a page (D-097)
	// Empty is "" or one of: none (no defect yet), nopr (defects, no pull
	// request yet), filter (the filters match nothing).
	Empty string
	// Pending counts defects a person or the loop has not moved yet.
	Defects int

	Drawer *AFDrawer

	// Units are the "Unit tests (CI)" sections, one per product in scope (D-105 §4.1);
	// UnitSaved names the product whose section was just saved (it opens with "Saved").
	Units     []AFUnit
	UnitSaved string
}

// AFUnit is one product's unit-check setting and its repositories' last watches.
type AFUnit struct {
	ProductID, Name, PostURL string
	Enabled                  bool
	Workflow, CheckName      string // the configured values ("" = the default)
	WorkflowPath, Check      string // the values in force
	Bases                    string // one per line
	DailyCap, Cap            int
	Repos                    []AFUnitRepo
	Open                     bool

	// D-106: the result source ("github-actions" or "jenkins"), Jenkins for the
	// template's branches, whether the product holds a unit-results credential,
	// and the unit bases the checklist names: BaseList as text (cut at 200),
	// BaseItems the same names for the template to print, each in <code> unless
	// it is the "default branch" phrase.
	Source       string
	Jenkins      bool
	JenkinsReady bool
	BaseList     string
	BaseItems    []AFUnitBase
}

// AFUnitBase is one unit base in the checklist; Code is false for the phrase
// that stands for the repository's default branch.
type AFUnitBase struct {
	Name string
	Code bool
}

// AFUnitRepo is a fix-target repository's last watch. Warn: the last three ended no-ci.
// Outcome is the stored token (shown as a title); OutcomeLabel says it in words.
// RunURL is the last result's Jenkins build; RunLink when its origin is
// GAOS_JENKINS_ORIGIN, else it is shown as text (D-106 §4.3).
type AFUnitRepo struct {
	Repo, Outcome, OutcomeLabel, At string
	Warn                            bool
	RunURL                          string
	RunLink                         bool
}

// unitDefaultPhrase stands for the effective base when AutoFix.BaseBranch is empty.
const unitDefaultPhrase = "the repository's default branch"

// unitBaseList is the checklist's unit bases (§4.3): the auto-fix base, or the
// default-branch phrase, then each configured extra base not equal to it, cut
// at 200 characters of text.
func unitBaseList(m Master) (string, []AFUnitBase) {
	items := []AFUnitBase{{Name: unitDefaultPhrase}}
	if b := m.AutoFix.BaseBranch; b != "" {
		items = []AFUnitBase{{Name: b, Code: true}}
	}
	for _, b := range m.UnitChecks.Bases {
		if b != m.AutoFix.BaseBranch {
			items = append(items, AFUnitBase{Name: b, Code: true})
		}
	}
	var names []string
	var kept []AFUnitBase
	n := 0
	for _, it := range items {
		name := afLine(it.Name, 200)
		if n > 0 && n+2+len(name) > 200 {
			kept = append(kept, AFUnitBase{Name: "…"})
			names = append(names, "…")
			break
		}
		n += len(name) + 2
		kept = append(kept, AFUnitBase{Name: name, Code: it.Code})
		names = append(names, name)
	}
	return afLine(strings.Join(names, ", "), 200), kept
}

// unitRunLink reports whether a stored build URL may be a link: its scheme and
// host equal the configured Jenkins origin (D-106 §5.4). No origin, no link.
func unitRunLink(raw, origin string) bool {
	if raw == "" || origin == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Scheme+"://"+u.Host, origin)
}

// unitOutcomeLabel says a watch outcome (§6) the way an operator reads it; the
// source changes the words of no-ci.
func unitOutcomeLabel(o, source string) string {
	if o == UnitNoCI && source == unitSourceJenkins {
		return "no result from Jenkins; nothing filed"
	}
	switch {
	case o == UnitPassed:
		return "passed"
	case o == UnitFailed:
		return "failed: defect filed, held for an auto-fix admin"
	case o == UnitDedup:
		return "failed again: the same open defect"
	case o == UnitUpdated:
		return "failed: the open defect was updated"
	case o == UnitNoCI:
		return "no check run found; nothing filed"
	case o == UnitInconclusive:
		return "inconclusive; nothing filed"
	case strings.HasPrefix(o, "refused:"):
		return "failed, not filed (" + strings.TrimPrefix(o, "refused:") + ")"
	case strings.HasPrefix(o, "skipped:"):
		return "not watched (" + strings.TrimPrefix(o, "skipped:") + ")"
	}
	return o
}

// AFToken is the kind of GitHub write token in use. Never the token.
type AFToken struct {
	Kind string // fine-grained | classic (broad) | not configured
	Tone string // ok | warn | off
	Hint string
}

// AFGitHub is whether GitHub is answering: the client's state and the poller's last pass.
type AFGitHub struct {
	Down    bool
	Message string // one sentence, "" when all is well
	Checked string // "checked 3m ago" or ""
}

// AFTile is one number.
type AFTile struct {
	Label string
	Value string
	Note  string
	Tone  string // "", run, ask, done, block
	Zero  bool
}

// AFMeter is used of a cap.
type AFMeter struct {
	Used, Cap int
	Pct       float64
	Full      bool
}

// AFDispatch is the "automatic dispatches today" tile.
type AFDispatch struct {
	Fix, Triage AFMeter
}

// AFProduct is one product's auto-fix state.
type AFProduct struct {
	ID, Name    string
	State       string // on | off | stuck
	Reason      string // why it is off or stuck
	Fix, Triage AFMeter
	Drafts      int
}

// AFWait is a defect card that is not moving, and why.
type AFWait struct {
	Ref, Title, Card, Product, ProductID string
	Job                                  string // triage | fix
	Person                               bool   // BLOCKED: a person is the next actor
	Why                                  string
}

// AFFilter is the query.
type AFFilter struct {
	Product, State, Difficulty, Open string
	Page                             int    // 0 = the first page (D-097)
	WPage                            int    // the Cards waiting page, 0 = the first (D-108)
	Section                          string // "" (Fix pull requests) | products | waiting
}

// The page's sections (design §7.2). The empty value is Fix pull requests and is never written.
const (
	afSectionProducts = "products"
	afSectionWaiting  = "waiting"
)

// afSectionOf reads the section key; an open drawer always shows Fix pull requests.
func afSectionOf(q url.Values) string {
	if q.Get("open") != "" {
		return ""
	}
	switch v := q.Get("section"); v {
	case afSectionProducts, afSectionWaiting:
		return v
	}
	return ""
}

// AFTab is a state link with its count.
type AFTab struct {
	Value, Label, Href string
	Count              int
	On                 bool
}

// AFOpt is an option of a select.
type AFOpt struct {
	Value, Label string
	On           bool
}

// AFRow is one pull-request record.
type AFRow struct {
	ID, State, StateLabel string
	Ref, Title            string
	Diff                  string
	Product, ProductID    string
	Repo                  string
	Number                int
	URL                   string
	Card                  string
	Age, AgeTitle         string
	WA                    bool
	Href                  string
	Open                  bool
	// D-108: the head branch and the base, why the pull request exists, and whether a
	// retarget replaced it (an S finding).
	Branch, Base string
	Trigger      string
	Replaced     bool
}

// AFDrawer is the detail panel of one pull request.
type AFDrawer struct {
	ID, State, StateLabel string
	Ref, Title, Diff      string
	Product, ProductID    string
	Repo                  string
	Number                int
	URL                   string
	Card, Branch, Class   string
	Base                  string
	Trigger, TriggerHref  string
	TriggerGet            string
	Opened, OpenedTitle   string
	Updated, UpdatedTitle string
	WA                    bool
	Before, After         string
	Files                 []AFFile
	FilesNote             string
	Limits                []AFLimit
	Caps                  []AFLimit
	Timeline              []AFEvent
	CloseHref             string
}

// AFFile is a path the fix changes. Never its content.
type AFFile struct {
	Path  string
	Test  bool
	New   bool
	Lines int
}

// AFLimit is a number against its limit.
type AFLimit struct {
	Label     string
	Used, Max int
	Pct       float64
	Over      bool
	Note      string
}

// AFEvent is a line of the timeline.
type AFEvent struct {
	Kind, Text, Rel, At string
}

// ---------------------------------------------------------------- text safety

// afText makes a record's text safe to print: control characters, bidirectional
// controls and zero-width characters are dropped, a screened secret is withheld,
// and the result is cut to max runes. The template escapes the rest.
func afText(s string, max int) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\n' || r == '\t':
			b.WriteRune(r)
		case unicode.IsControl(r) || afInvisible(r):
		default:
			b.WriteRune(r)
		}
	}
	out := b.String()
	if screenBody(out) {
		return "[withheld: matched a secret detector]"
	}
	if r := []rune(out); len(r) > max {
		return string(r[:max]) + "…"
	}
	return out
}

// afInvisible are the characters that change what a line reads as without
// showing: bidirectional controls, the zero-width space, the word joiner, the
// byte-order mark and the line and paragraph separators. The zero-width joiner
// and non-joiner stay: an emoji sequence and Persian or Indic text need them.
func afInvisible(r rune) bool {
	switch {
	case r == '\u200b', r == '\u2060', r == '\ufeff', r == '\u061c', r == '\u200e', r == '\u200f', r == '\u2028', r == '\u2029':
		return true
	case r >= '\u202a' && r <= '\u202e', r >= '\u2066' && r <= '\u2069':
		return true
	}
	return false
}

// afLine is afText for one line.
func afLine(s string, max int) string {
	return clipEllipsis(strings.Join(strings.Fields(afText(s, max*4)), " "), max)
}

func afClock(at int64) string {
	if at <= 0 {
		return ""
	}
	return time.Unix(at, 0).UTC().Format("2006-01-02 15:04 UTC")
}

func tokenKindLabel(kind string) string {
	if kind == "" {
		return "not configured"
	}
	return kind
}

// ---------------------------------------------------------------- the builder

func (s *server) buildAutoFix(r *http.Request, bo *BOView) {
	ctx := r.Context()
	bo.Title = "Auto-fix"
	bo.Sub = "Defects the QA tools report, and the draft pull requests GAOS opens for the easy ones. Nothing is merged here: a person always has the last word on GitHub."
	bo.AutoFix = s.autoFixPage(ctx, r.URL.Query(), time.Now(), prViewerIsAdmin(r))
}

func (s *server) autoFixPage(ctx context.Context, q url.Values, now time.Time, admin bool) *AutoFixPage {
	p := &AutoFixPage{Admin: admin}
	products, _ := s.store.Products(ctx)

	// ---- the filter, validated against what exists ----
	known := map[string]string{}
	for _, pr := range products {
		known[pr.ID] = pr.Name
	}
	f := AFFilter{}
	if _, ok := known[q.Get("product")]; ok {
		f.Product = q.Get("product")
	}
	for _, st := range afStateOrder {
		if q.Get("state") == st.Value {
			f.State = st.Value
		}
	}
	for _, d := range afDifficulties {
		if q.Get("difficulty") == d {
			f.Difficulty = d
		}
	}
	f.Open = q.Get("open")
	f.Page = pageParam(q.Get("page"))
	f.WPage = pageParam(q.Get("wpage"))
	f.Section = afSectionOf(q)

	// ---- the records of every product in scope ----
	type scoped struct {
		pr     Product
		af     AutoFix
		defs   []Defect
		fixes  []FixPR
		cards  []TaskRecord
		fixUse int
		trgUse int
	}
	today := autoFixNow() / 86400
	var in []scoped
	for _, pr := range products {
		if f.Product != "" && pr.ID != f.Product {
			continue
		}
		sc := scoped{pr: pr}
		sc.af, _ = s.store.AutoFixOf(ctx, pr.ID)
		sc.defs, _ = s.store.Defects(ctx, pr.ID)
		sc.fixes, _ = s.store.FixPRs(ctx, pr.ID)
		sc.cards, _ = s.store.PRCards(ctx, pr.ID)
		for _, t := range sc.cards {
			if t.AutoAt == 0 || t.AutoAt/86400 != today {
				continue
			}
			if _, _, job, ok := parseDefectCardKey(t.DedupKey); ok {
				switch job {
				case defectJobFix:
					sc.fixUse++
				case defectJobTriage:
					sc.trgUse++
				}
			}
		}
		in = append(in, sc)
	}
	defByKey := map[string]Defect{}
	meterOf := func(used, limit int) AFMeter {
		m := AFMeter{Used: used, Cap: limit, Full: limit > 0 && used >= limit}
		if limit > 0 {
			m.Pct = float64(used) * 100 / float64(limit)
			if m.Pct > 100 {
				m.Pct = 100
			}
		}
		return m
	}

	// ---- duplicates (D-108 design §5): per product, over every record ----
	replaced := map[string]bool{}
	for _, sc := range in {
		defs := map[string]Defect{}
		for _, d := range sc.defs {
			defs[d.Key] = d
		}
		for _, dup := range afDuplicates(sc.fixes, defs) {
			if dup.Rule == dupSuperseded {
				replaced[dup.Close] = true
			}
			p.Dups = append(p.Dups, dup)
		}
	}

	// ---- tiles ----
	var drafts, ready, merged, awaiting, blocked, held int
	var fixUsed, fixCap, trgUsed, trgCap, defects int
	weekAgo := now.Unix() - 7*24*3600
	for _, sc := range in {
		for _, d := range sc.defs {
			defByKey[d.Key] = d
			defects++
			if d.State == DefectStateOpen {
				awaiting++
			}
		}
		for _, fx := range sc.fixes {
			switch fx.State {
			case FixPRDraft:
				if !replaced[fx.ID] {
					drafts++
				}
			case FixPRReady:
				ready++
			case FixPRMerged:
				if fx.UpdatedAt >= weekAgo {
					merged++
				}
			}
		}
		// A product that is off has no cap to spend: only the enabled ones add to the total.
		fixUsed, trgUsed = fixUsed+sc.fixUse, trgUsed+sc.trgUse
		if sc.af.Enabled {
			fc, tc := sc.af.Caps()
			fixCap, trgCap = fixCap+fc, trgCap+tc
		}
	}
	p.Defects = defects

	// ---- products and waiting cards ----
	for _, sc := range in {
		fc, tc := sc.af.Caps()
		ap := AFProduct{ID: sc.pr.ID, Name: afLine(sc.pr.Name, 80), Fix: meterOf(sc.fixUse, fc), Triage: meterOf(sc.trgUse, tc)}
		off := afOffReason(sc.af, s.writeTokenKind)
		switch {
		case sc.af.Enabled && off == "":
			ap.State = "on"
		case sc.af.Enabled:
			ap.State, ap.Reason = "stuck", off
		default:
			ap.State, ap.Reason = "off", off
		}
		for _, fx := range sc.fixes {
			if fx.State == FixPRDraft {
				ap.Drafts++
			}
		}
		p.Products = append(p.Products, ap)
		p.AnyOff = p.AnyOff || ap.State != "on"

		live := ""
		for _, t := range sc.cards {
			if t.Status == StatusDispatched || t.Status == StatusRunning {
				live = t.ID
				break
			}
		}
		for _, t := range sc.cards {
			key, _, job, ok := parseDefectCardKey(t.DedupKey)
			if !ok || (t.Status != StatusReady && t.Status != StatusBlocked) {
				continue
			}
			w := AFWait{Card: afLine(t.ID, 24), Product: ap.Name, ProductID: sc.pr.ID, Job: job}
			if d, ok := defByKey[key]; ok {
				w.Ref, w.Title = afLine(d.Ref(), 40), afLine(d.Title, 140)
			} else {
				w.Ref = "defect"
			}
			switch {
			case t.Status == StatusBlocked:
				w.Person = true
				if words, ok := afBlockWords[t.BlockReason]; ok {
					w.Why = "Needs a person: " + words
				} else {
					w.Why = "Needs a person: open the card for the reason"
				}
				blocked++
			case !hasLabel(t.Labels, LabelDefectTriage) && !hasLabel(t.Labels, LabelDefectEasy):
				w.Person = true
				w.Why = "Needs a person: only easy fixes and triage are dispatched automatically"
				blocked++
			case ap.State != "on":
				w.Why = "Auto-fix is off for this product"
				if ap.Reason != "" {
					w.Why += " — " + ap.Reason
				}
				held++
			case job == defectJobFix && ap.Fix.Full:
				w.Why = fmt.Sprintf("Fix cap %d/%d until 00:00 UTC", ap.Fix.Used, ap.Fix.Cap)
				held++
			case job == defectJobTriage && ap.Triage.Full:
				w.Why = fmt.Sprintf("Triage cap %d/%d until 00:00 UTC", ap.Triage.Used, ap.Triage.Cap)
				held++
			case live != "" && live != t.ID:
				w.Why = "Waiting for the live turn on " + afLine(live, 24)
				held++
			default:
				w.Why = "Next in line: the dispatch loop looks every 15 s"
				held++
			}
			p.Waiting = append(p.Waiting, w)
		}
	}
	sort.SliceStable(p.Waiting, func(i, j int) bool { return p.Waiting[i].Person && !p.Waiting[j].Person })
	// Five a page (design §7.1), clamped and written back before any address is built.
	p.WaitPager = paginate(len(p.Waiting), f.WPage, listPageSize, func(n int) (string, string) {
		g := f
		g.Open, g.WPage = "", n
		return afHref(g), afPageHref(g)
	})
	p.WaitPager.Sync, p.WaitPager.Label = "#afLive:replace", "Waiting cards pages"
	f.WPage = p.WaitPager.Page
	if f.WPage <= 1 {
		f.WPage = 0
	}
	p.Waiting = sliceFor(p.Waiting, p.WaitPager)
	// the ones that are working first; the order the store gave otherwise
	sort.SliceStable(p.Products, func(i, j int) bool { return p.Products[i].State == "on" && p.Products[j].State != "on" })
	if len(p.Products) > 30 {
		p.ProductsMore, p.Products = len(p.Products)-30, p.Products[:30]
	}

	p.Tiles = []AFTile{
		{Label: "Open drafts", Value: thousands(drafts), Tone: "run", Zero: drafts == 0, Note: draftNote(len(replaced))},
		{Label: "Ready for review", Value: thousands(ready), Tone: "ask", Zero: ready == 0, Note: "a person is next"},
		{Label: "Merged, 7 days", Value: thousands(merged), Tone: "done", Zero: merged == 0, Note: "fixes that landed"},
		{Label: "Awaiting triage", Value: thousands(awaiting), Zero: awaiting == 0, Note: "defects not yet judged"},
		{Label: "Waiting for a person", Value: thousands(blocked), Tone: "block", Zero: blocked == 0, Note: waitNote(held)},
	}
	p.Disp = AFDispatch{Fix: meterOf(fixUsed, fixCap), Triage: meterOf(trgUsed, trgCap)}

	// ---- the table: every pull request of the scope, then the filters ----
	var all []AFRow
	byID := map[string]FixPR{}
	prodOf := map[string]string{}
	for _, sc := range in {
		for _, fx := range sc.fixes {
			byID[fx.ID] = fx
			prodOf[fx.ID] = sc.pr.ID
		}
	}
	for _, sc := range in {
		for _, fx := range sc.fixes {
			r := afRow(fx, defByKey[fx.DefectKey], afLine(sc.pr.Name, 80), sc.pr.ID, now)
			r.Trigger, _ = afTrigger(fx, defByKey[fx.DefectKey], byID)
			r.Replaced = replaced[fx.ID]
			all = append(all, r)
		}
	}
	rank := func(st string) int {
		for i, s := range afStateOrder {
			if s.Value == st {
				return i
			}
		}
		return len(afStateOrder)
	}
	created := func(r AFRow) int64 { return byID[r.ID].CreatedAt }
	sort.SliceStable(all, func(i, j int) bool {
		ai, aj := rank(all[i].State) < 3, rank(all[j].State) < 3
		if ai != aj {
			return ai
		}
		if created(all[i]) != created(all[j]) {
			return created(all[i]) > created(all[j])
		}
		return all[i].ID < all[j].ID
	})

	// the tabs count what the other filters leave
	counts := map[string]int{}
	total := 0
	var shown []AFRow
	for _, r := range all {
		if f.Difficulty != "" && r.Diff != f.Difficulty {
			continue
		}
		counts[r.State]++
		total++
		if f.State == "" || r.State == f.State {
			shown = append(shown, r)
		}
	}
	p.RowsTotal = len(shown)
	// One page (D-097), clamped before anything writes an address.
	p.Pager = paginate(len(shown), f.Page, tablePageSize, func(n int) (string, string) {
		g := f
		g.Open, g.Page = "", n
		return afHref(g), afPageHref(g)
	})
	p.Pager.Sync, p.Pager.Label = "#afLive:replace", "Auto-fix pages"
	f.Page = p.Pager.Page
	if f.Page <= 1 {
		f.Page = 0
	}
	p.Filter = f
	shown = sliceFor(shown, p.Pager)
	for i := range shown {
		shown[i].Open = f.Open != "" && shown[i].ID == f.Open
		shown[i].Href = afHref(AFFilter{Product: f.Product, State: f.State, Difficulty: f.Difficulty, Open: shown[i].ID, Page: f.Page, WPage: f.WPage})
	}
	p.Rows = shown

	p.StateTabs = append(p.StateTabs, AFTab{Value: "", Label: "All", Count: total, On: f.State == "",
		Href: afHref(AFFilter{Product: f.Product, Difficulty: f.Difficulty, WPage: f.WPage, Section: f.Section})})
	for _, st := range afStateOrder {
		if st.Value == FixPRPreparing && counts[st.Value] == 0 && f.State != st.Value {
			continue // a state with nothing in it and nobody asking is noise
		}
		p.StateTabs = append(p.StateTabs, AFTab{Value: st.Value, Label: st.Label, Count: counts[st.Value], On: f.State == st.Value,
			Href: afHref(AFFilter{Product: f.Product, State: st.Value, Difficulty: f.Difficulty, WPage: f.WPage, Section: f.Section})})
	}
	// The sections (design §7.2): a link to another one keeps the product and the difficulty.
	for _, sec := range []struct {
		v, label string
		n        int
	}{{"", "Fix pull requests", p.RowsTotal}, {afSectionWaiting, "Cards waiting", p.WaitPager.Total}, {afSectionProducts, "Auto-fix by product", len(p.Products) + p.ProductsMore}} {
		p.Sections = append(p.Sections, AFTab{Value: sec.v, Label: sec.label, Count: sec.n, On: f.Section == sec.v,
			Href: afHref(AFFilter{Product: f.Product, Difficulty: f.Difficulty, Section: sec.v})})
	}
	p.Section = f.Section
	for i := range p.Dups {
		dp := &p.Dups[i]
		dp.CloseGet = afHref(AFFilter{Product: f.Product, Open: dp.Close})
		dp.KeepGet = afHref(AFFilter{Product: f.Product, Open: dp.Keep})
		dp.CloseHref, dp.KeepHref = pageHref(dp.CloseGet), pageHref(dp.KeepGet)
		dp.PostURL = "products/" + url.PathEscape(dp.ProductID) + "/prs/" + url.PathEscape(dp.Close) + "/close"
		dp.Confirm = "Close " + dp.CloseRef + " on GitHub? Its branch is kept; the defect is not changed."
	}
	p.ProductOpts = []AFOpt{{Value: "", Label: "All products", On: f.Product == ""}}
	for _, pr := range products {
		p.ProductOpts = append(p.ProductOpts, AFOpt{Value: pr.ID, Label: afLine(pr.Name, 60), On: f.Product == pr.ID})
	}
	p.DiffOpts = []AFOpt{{Value: "", Label: "Any difficulty", On: f.Difficulty == ""}}
	for _, d := range afDifficulties {
		p.DiffOpts = append(p.DiffOpts, AFOpt{Value: d, Label: d, On: f.Difficulty == d})
	}
	p.ResetHref = afHref(AFFilter{Section: f.Section})
	p.PollURL = afHref(f, "live")

	switch {
	case len(p.Rows) > 0:
	case defects == 0 && len(all) == 0:
		p.Empty = "none"
	case len(all) == 0:
		p.Empty = "nopr"
	default:
		p.Empty = "filter"
	}

	// ---- GitHub and the token ----
	p.Token = afToken(s.writeTokenKind)
	p.GitHub = s.afGitHub(now)

	// ---- the unit checks (D-105 §4.1) ----
	for _, sc := range in {
		m, err := s.store.Master(ctx, sc.pr.ID)
		if err != nil {
			continue
		}
		ws, _ := s.store.UnitWatches(ctx, sc.pr.ID)
		p.Units = append(p.Units, afUnit(sc.pr, m, ws, s.unitResults.has(sc.pr.ID), s.jenkinsOrigin))
	}

	// ---- the drawer ----
	if fx, ok := byID[f.Open]; ok {
		for _, sc := range in {
			if sc.pr.ID == prodOf[fx.ID] {
				p.Drawer = s.afDrawer(ctx, fx, defByKey[fx.DefectKey], sc.pr, sc.af, meterOf(sc.fixUse, mustCap(sc.af, true)), meterOf(sc.trgUse, mustCap(sc.af, false)), now, f)
				var openID string
				p.Drawer.Trigger, openID = afTrigger(fx, defByKey[fx.DefectKey], byID)
				if openID != "" {
					g := f
					g.Open = openID
					p.Drawer.TriggerGet = afHref(g)
					p.Drawer.TriggerHref = pageHref(p.Drawer.TriggerGet)
				}
			}
		}
	}
	return p
}

func mustCap(a AutoFix, fix bool) int {
	f, t := a.Caps()
	if fix {
		return f
	}
	return t
}

func draftNote(replaced int) string {
	if replaced == 0 {
		return "waiting for a reviewer"
	}
	return fmt.Sprintf("waiting for a reviewer · %d replaced", replaced)
}

func waitNote(held int) string {
	switch held {
	case 0:
		return "nothing else is held"
	case 1:
		return "1 more held by a switch, cap or live turn"
	}
	return fmt.Sprintf("%d more held by a switch, cap or live turn", held)
}

// afOffReason is why a product's auto-fix is not dispatching, "" when it is
// ready: the §6.1 preconditions, in words, plus where to change them.
func afOffReason(a AutoFix, tokenKind string) string {
	if err := autoFixPreconditions(a, tokenKind); err != nil {
		return strings.TrimSuffix(err.Error(), ".") + "."
	}
	if !a.Enabled {
		return "An operator has not switched it on."
	}
	return ""
}

func afToken(kind string) AFToken {
	t := AFToken{Kind: tokenKindLabel(kind)}
	switch kind {
	case tokenKindFine:
		t.Tone, t.Hint = "ok", "A fine-grained token limited to the application repositories: the recommended credential."
	case tokenKindClassic:
		t.Tone, t.Hint = "warn", "A classic token reaches every repository its account can. A fine-grained token is recommended."
	default:
		t.Tone, t.Hint = "off", "GAOS_GITHUB_WRITE_TOKEN_FILE is not set: auto-fix cannot be turned on."
	}
	return t
}

func (s *server) afGitHub(now time.Time) AFGitHub {
	g := AFGitHub{}
	if s.fixPoll != nil {
		if st := s.fixPoll.Status(); !st.LastPoll.IsZero() {
			g.Checked = "checked " + relSpan(now.Sub(st.LastPoll)) + " ago"
			if st.LastError != "" {
				g.Down, g.Message = true, "GitHub is not answering ("+afLine(st.LastError, 24)+"). Pull-request states are as of the last good read and may be stale."
			}
		}
	}
	if s.ghState != "on" && !g.Down {
		why := s.ghState
		if why == "" {
			why = "off"
		}
		g.Down, g.Message = true, "The GitHub client is "+afLine(why, 120)+". Pull-request states will not move until it is back."
	}
	return g
}

// afHref is the view's own address, relative to the page's base.
func afHref(f AFFilter, live ...string) string {
	v := url.Values{}
	if f.Product != "" {
		v.Set("product", f.Product)
	}
	if f.State != "" {
		v.Set("state", f.State)
	}
	if f.Difficulty != "" {
		v.Set("difficulty", f.Difficulty)
	}
	if f.Open != "" {
		v.Set("open", f.Open)
	}
	if f.Page > 1 {
		v.Set("page", strconv.Itoa(f.Page))
	}
	if f.WPage > 1 {
		v.Set("wpage", strconv.Itoa(f.WPage))
	}
	if f.Section != "" {
		v.Set("section", f.Section)
	}
	path := "ui/bo/autofix"
	if len(live) > 0 {
		path += "/live"
	}
	if len(v) == 0 {
		return path
	}
	return path + "?" + v.Encode()
}

func afStateLabel(st string) string {
	for _, s := range afStateOrder {
		if s.Value == st {
			return s.Label
		}
	}
	return "Unknown"
}

func afRow(fx FixPR, d Defect, product, productID string, now time.Time) AFRow {
	state := fx.State
	if afStateLabel(state) == "Unknown" {
		state = "unknown"
	}
	r := AFRow{ID: fx.ID, State: state, StateLabel: afStateLabel(fx.State), Product: product, ProductID: productID,
		Card: afLine(fx.TaskID, 24), WA: fx.WaSent, Diff: afDiff(fx.Difficulty, d.Difficulty),
		Branch: afLine(fx.Branch, 90), Base: afLine(fx.Base, 60)}
	if d.Key != "" {
		r.Ref, r.Title = afLine(d.Ref(), 40), afLine(d.Title, 200)
	} else {
		r.Ref, r.Title = afLine(fx.DefectKey, 40), "(the defect record is gone)"
	}
	if defectRepo.MatchString(fx.Repo) && fx.Number > 0 {
		r.Repo, r.Number = fx.Repo, fx.Number
		r.URL = afPRURL(fx)
	}
	if fx.CreatedAt > 0 {
		r.Age = relSpan(now.Sub(time.Unix(fx.CreatedAt, 0)))
		r.AgeTitle = "opened " + afClock(fx.CreatedAt)
	}
	return r
}

func afDiff(a, b string) string {
	for _, d := range []string{a, b} {
		for _, k := range afDifficulties {
			if d == k {
				return d
			}
		}
	}
	return ""
}

// afPRURL is the pull request's address: the record's own when it is an https
// URL, else the one built from the repository and the number.
func afPRURL(fx FixPR) string {
	if u, err := url.Parse(fx.URL); err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil {
		return u.String()
	}
	return fmt.Sprintf("https://github.com/%s/pull/%d", fx.Repo, fx.Number)
}

// afParseReply reads the stored reply: the parsed JSON, or the raw block.
func afParseReply(raw string) (FixReply, bool) {
	var r FixReply
	if json.Unmarshal([]byte(raw), &r) == nil && len(r.Edits)+len(r.NewFiles) > 0 {
		return r, true
	}
	if r, err := parseFix(raw); err == nil {
		return r, true
	}
	return FixReply{}, false
}

func (s *server) afDrawer(ctx context.Context, fx FixPR, d Defect, pr Product, a AutoFix, fixM, trgM AFMeter, now time.Time, f AFFilter) *AFDrawer {
	row := afRow(fx, d, afLine(pr.Name, 80), pr.ID, now)
	dr := &AFDrawer{ID: fx.ID, State: row.State, StateLabel: row.StateLabel, Ref: row.Ref, Title: row.Title, Diff: row.Diff,
		Product: row.Product, ProductID: pr.ID, Repo: row.Repo, Number: row.Number, URL: row.URL, Card: row.Card,
		Branch: afLine(fx.Branch, 90), Base: row.Base, Class: afLine(d.Class, 40), WA: fx.WaSent,
		Opened: row.Age, OpenedTitle: afClock(fx.CreatedAt),
		Before: afText(fx.Before, 1024), After: afText(fx.After, 1024),
		CloseHref: afHref(AFFilter{Product: f.Product, State: f.State, Difficulty: f.Difficulty, Page: f.Page})}
	if dr.Opened != "" {
		dr.Opened += " ago"
	}
	if fx.UpdatedAt > 0 {
		dr.Updated = relSpan(now.Sub(time.Unix(fx.UpdatedAt, 0))) + " ago"
		dr.UpdatedTitle = afClock(fx.UpdatedAt)
	}

	// the changed files, from the stored reply: paths and line counts only
	codeLines, testLines := 0, 0
	if rep, ok := afParseReply(fx.Reply); ok {
		perPath := map[string]*AFFile{}
		var order []string
		add := func(path string, lines int, isNew bool) {
			if !fixPathOK(path) {
				return
			}
			e := perPath[path]
			if e == nil {
				e = &AFFile{Path: path, New: isNew, Test: isNew || matchAny(a.TestGlobs, path)}
				perPath[path] = e
				order = append(order, path)
			}
			e.Lines += lines
		}
		for _, e := range rep.Edits {
			add(e.Path, myersDistance(fixLines(e.Find), fixLines(e.Replace)), false)
		}
		for _, n := range rep.NewFiles {
			add(n.Path, len(fixLines(n.Content)), true)
		}
		for _, p := range order {
			e := *perPath[p]
			if e.Test {
				testLines += e.Lines
			} else {
				codeLines += e.Lines
			}
			dr.Files = append(dr.Files, e)
		}
		dr.Limits = []AFLimit{
			afLimit("Files", len(dr.Files), fixMaxFiles, ""),
			afLimit("Code lines", codeLines, fixMaxNonTest, "added + removed"),
			afLimit("Test lines", testLines, fixMaxTest, "added + removed"),
		}
	} else {
		for _, p := range d.Files {
			if fixPathOK(p) {
				dr.Files = append(dr.Files, AFFile{Path: p})
			}
		}
		dr.FilesNote = "The stored reply could not be read, so line counts are not shown."
	}
	dr.Caps = []AFLimit{
		afLimit("Fix dispatches today", fixM.Used, fixM.Cap, "resets 00:00 UTC"),
		afLimit("Triage dispatches today", trgM.Used, trgM.Cap, "resets 00:00 UTC"),
	}

	// the timeline: this record's events, oldest first
	if evs, err := s.store.Events(ctx); err == nil && d.Key != "" {
		ref := regexp.MustCompile(`\b` + regexp.QuoteMeta(d.Ref()) + `\b`)
		var mine []Event
		for _, e := range evs {
			switch e.Type {
			case DefectReported, DefectTriaged, DefectStateChanged, FixPROpened, FixPRStateChanged:
			default:
				continue
			}
			if e.DeskID != pr.ID {
				continue
			}
			if e.Payload.Task == d.Key || (e.Payload.Task == "" && ref.MatchString(e.Text)) {
				mine = append(mine, e)
			}
		}
		if len(mine) > 30 {
			mine = mine[len(mine)-30:]
		}
		for _, e := range mine {
			ev := AFEvent{Kind: afEventLabel(e.Type), Text: afLine(e.Text, 160)}
			if e.At > 0 {
				ev.Rel, ev.At = relSpan(now.Sub(time.Unix(e.At, 0)))+" ago", afClock(e.At)
			}
			dr.Timeline = append(dr.Timeline, ev)
		}
	}
	return dr
}

func afLimit(label string, used, max int, note string) AFLimit {
	l := AFLimit{Label: label, Used: used, Max: max, Note: note, Over: max > 0 && used > max}
	if max > 0 {
		l.Pct = float64(used) * 100 / float64(max)
		if l.Pct > 100 {
			l.Pct = 100
		}
	}
	return l
}

func afEventLabel(t EventType) string {
	switch t {
	case DefectReported:
		return "Reported"
	case DefectTriaged:
		return "Triaged"
	case DefectStateChanged:
		return "Defect"
	case FixPROpened:
		return "Opened"
	case FixPRStateChanged:
		return "Pull request"
	}
	return string(t)
}

// ---------------------------------------------------------------- the poll route

// fixPollSource is the poller's status, as the view reads it.
type fixPollSource interface{ Status() FixPollStatus }

// handleAutoFixLive is GET /ui/bo/autofix/live: the 30 s poll. It answers the
// tiles, the waiting list and the table as one out-of-band element that
// replaces #afLive in place, from the same builder as the page.
func (s *server) handleAutoFixLive(w http.ResponseWriter, r *http.Request) {
	if !s.requireBO(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	page := s.autoFixPage(r.Context(), r.URL.Query(), time.Now(), prViewerIsAdmin(r))
	page.OOB = true
	s.rend.Render(w, http.StatusOK, "af-live", page)
}

// afUnit builds one product's "Unit tests (CI)" section: the setting and, per
// fix-target repository, the last watch that ended (skipped ones included).
// jenkinsReady says the product holds a unit-results credential; origin is
// GAOS_JENKINS_ORIGIN, the only origin a build link may point at.
func afUnit(pr Product, m Master, ws []UnitWatch, jenkinsReady bool, origin string) AFUnit {
	u := m.UnitChecks
	out := AFUnit{ProductID: pr.ID, Name: afLine(pr.Name, 80), PostURL: "products/" + url.PathEscape(pr.ID) + "/autofix/unit",
		Enabled: u.Enabled, Workflow: u.Workflow, CheckName: u.CheckName, WorkflowPath: u.WorkflowPath(), Check: u.Name(),
		Bases: strings.Join(u.Bases, "\n"), DailyCap: u.DailyCap, Cap: u.Cap(),
		Source: u.SourceName(), Jenkins: u.SourceName() == unitSourceJenkins, JenkinsReady: jenkinsReady}
	out.BaseList, out.BaseItems = unitBaseList(m)
	for _, repo := range m.AutoFix.TargetRepos {
		if !unitFixTarget(m, repo) {
			continue
		}
		var done []UnitWatch
		for _, w := range ws {
			if w.State == UnitWatchDone && strings.EqualFold(w.Repo, repo) {
				done = append(done, w)
			}
		}
		sort.Slice(done, func(i, j int) bool { return done[i].DoneAt > done[j].DoneAt })
		r := AFUnitRepo{Repo: afLine(repo, 120), OutcomeLabel: "no merge watched yet"}
		if len(done) > 0 {
			r.Outcome, r.OutcomeLabel, r.At = done[0].Outcome, unitOutcomeLabel(done[0].Outcome, out.Source), afClock(done[0].DoneAt)
			if done[0].RunURL != "" {
				r.RunLink = unitRunLink(done[0].RunURL, origin)
				r.RunURL = done[0].RunURL
				if !r.RunLink {
					r.RunURL = afLine(done[0].RunURL, 120)
				}
			}
		}
		if len(done) >= 3 {
			r.Warn = done[0].Outcome == UnitNoCI && done[1].Outcome == UnitNoCI && done[2].Outcome == UnitNoCI
		}
		out.Repos = append(out.Repos, r)
	}
	return out
}

// ---------------------------------------------------------------- duplicates (D-108)

// AFDup is one duplicate finding: the open record to close, the one kept, the rule
// (superseded | same-defect) and the sentence a person reads. Every string is plain text.
type AFDup struct {
	Rule, RuleLabel     string
	Close, Keep         string // record ids
	CloseRef, KeepRef   string // "owner/repo#n"
	CloseHref, KeepHref string
	CloseGet, KeepGet   string
	Sentence            string
	PostURL, Confirm    string
	ProductID           string
}

const (
	dupSuperseded = "superseded"
	dupSameDefect = "same-defect"
)

func fixOpen(state string) bool { return state == FixPRDraft || state == FixPRReady }

func fixRef(f FixPR) string {
	if f.Number > 0 {
		return afLine(f.Repo, 120) + "#" + strconv.Itoa(f.Number)
	}
	return afLine(f.ID, 60)
}

// afDuplicates is the duplicate check of one product's records (design §5.1). It takes every
// record and names only open ones to close. Rule S: an open record a retarget replaced, whose
// successor is open or merged. Rule D: two or more open records of one defect; the defect's own
// record is kept when it is open, else the newest. A record is the close side of one finding at most.
func afDuplicates(fixes []FixPR, defs map[string]Defect) []AFDup {
	byID := make(map[string]FixPR, len(fixes))
	for _, f := range fixes {
		byID[f.ID] = f
	}
	newer := func(a, b FixPR) bool { // a after b
		if a.CreatedAt != b.CreatedAt {
			return a.CreatedAt > b.CreatedAt
		}
		return a.Number > b.Number
	}
	var out []AFDup
	closed := map[string]bool{}
	mk := func(rule string, c, k FixPR, sentence string) AFDup {
		label := "Superseded"
		if rule == dupSameDefect {
			label = "Same defect"
		}
		return AFDup{Rule: rule, RuleLabel: label, Close: c.ID, Keep: k.ID, CloseRef: fixRef(c), KeepRef: fixRef(k),
			Sentence: afLine(sentence, 200), ProductID: c.ProductID}
	}
	for _, x := range fixes {
		if !fixOpen(x.State) || x.RetargetedTo == "" {
			continue
		}
		y, ok := byID[x.RetargetedTo]
		if !ok || !(fixOpen(y.State) || y.State == FixPRMerged) {
			continue
		}
		closed[x.ID] = true
		out = append(out, mk(dupSuperseded, x, y, fmt.Sprintf("Replaced by %s (base %s) after a retarget.", fixRef(y), baseOrUnknown(y.Base))))
	}
	groups := map[string][]FixPR{}
	var keys []string
	for _, f := range fixes {
		if !fixOpen(f.State) || closed[f.ID] || f.DefectKey == "" {
			continue
		}
		if _, ok := groups[f.DefectKey]; !ok {
			keys = append(keys, f.DefectKey)
		}
		groups[f.DefectKey] = append(groups[f.DefectKey], f)
	}
	for _, k := range keys {
		g := groups[k]
		if len(g) < 2 {
			continue
		}
		keep := g[0]
		for _, f := range g[1:] {
			if newer(f, keep) {
				keep = f
			}
		}
		if live, ok := byID[defs[k].FixPR]; ok && defs[k].FixPR != "" && fixOpen(live.State) && !closed[live.ID] {
			keep = live
		}
		for _, f := range g {
			if f.ID != keep.ID {
				out = append(out, mk(dupSameDefect, f, keep, "Another open pull request fixes the same defect: "+fixRef(keep)+"."))
			}
		}
	}
	created := func(id string) FixPR { return byID[id] }
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Rule != out[j].Rule {
			return out[i].Rule == dupSuperseded
		}
		a, b := created(out[i].Close), created(out[j].Close)
		if a.CreatedAt != b.CreatedAt {
			return a.CreatedAt < b.CreatedAt
		}
		return a.Number < b.Number
	})
	return out
}

// afTrigger says why a fix pull request exists (design §6.2): the first case that applies.
// openID names an earlier record the text refers to, "" when there is none.
func afTrigger(fx FixPR, d Defect, byID map[string]FixPR) (text, openID string) {
	v := func(s string) string { return afLine(s, 60) }
	if o, ok := byID[fx.RetargetedFrom]; ok && fx.RetargetedFrom != "" {
		return afLine(fmt.Sprintf("Retarget of %s (base %s)", fixRef(o), v(baseOrUnknown(o.Base))), 160), o.ID
	}
	switch {
	case d.Key == "":
		return "The defect record is gone", ""
	case d.Kind == DefectKindReview:
		return afLine(fmt.Sprintf("Review finding on %s#%d (%s)", v(d.Repo), reviewNumber(d), v(d.ReviewBranch)), 160), ""
	case d.Kind == DefectKindUnit:
		sha := d.UnitSHA
		if len(sha) > 7 {
			sha = sha[:7]
		}
		return afLine(fmt.Sprintf("Unit tests failed on %s after merge %s", v(d.UnitBase), v(sha)), 160), ""
	}
	if d.Gen > 1 {
		var prev FixPR
		found := false
		for _, o := range byID {
			if o.DefectKey == fx.DefectKey && o.ID != fx.ID && fixPRFinal(o.State) && o.CreatedAt < fx.CreatedAt &&
				(!found || o.CreatedAt > prev.CreatedAt) {
				prev, found = o, true
			}
		}
		if found {
			return afLine(fmt.Sprintf("Second attempt after %s was %s", fixRef(prev), prev.State), 160), prev.ID
		}
	}
	return afLine(fmt.Sprintf("QA report %s (%s)", v(d.CaseID), v(d.Kind)), 160), ""
}
