package main

import (
	"context"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The Pull requests page (D-092, docs/designs/gaos-pr-overview.md §5): every open pull request that
// relates to a GAOS task, per product. Fix pull requests come from FixPR, review and QA-tool pull
// requests from the PullRequest rows. Nothing here calls GitHub: the poller keeps the rows fresh
// and this view only reads. Every string is plain text; GitHub links are rebuilt from the
// validated owner/repo and number, never from a stored URL.

// prNoFollowDays is the age after which a fix record is no longer polled (fixPRPollLifetime).
const prNoFollowDays = 30

var prStateTabs = []struct{ Value, Label string }{
	{"open", "Open"}, {"merged", "Merged"}, {"closed", "Closed"}, {"all", "All"},
}

var prKindTabs = []struct{ Value, Label string }{
	{"", "Every kind"}, {"fix", "Auto-fix"}, {PRKindReview, "Review"}, {PRKindQA, "QA tool"},
}

var prKindLabels = map[string]string{"fix": "Auto-fix", PRKindReview: "Review", PRKindQA: "QA tool"}

// PRFilter is the page's narrowing, validated against what exists.
type PRFilter struct {
	Product, State, Kind, Open string
	Page                       int // 0 = the first page (D-097)
}

// PRRelation is a link to the pull request a retarget replaced or was replaced by.
type PRRelation struct {
	Label, Ref, URL string
}

// PRRow is one pull request.
type PRRow struct {
	Key                string // fix:<FixPR.ID> or pr:<row key>
	Kind, KindLabel    string
	State, StateLabel  string // State is the chip's class: draft, ready, merged or closed
	Product, ProductID string
	Repo               string
	Number             int
	URL                string
	Title              string
	Head, Base         string
	TaskID, TaskTitle  string
	TaskStatus         string
	TimingHref         string
	Defect             string
	QACase, QALink     string
	QAJob              string
	Age, AgeTitle      string
	Relations          []PRRelation
	Follow             string // "", "not followed since <date>" or "no longer followed"
	Href               string // opens the drawer
	Open               bool
	At                 int64 // when it was opened, for the sort
	// D-100: the row's bundle and whether the missing-code alert was sent.
	Bundle     string
	NoTaskCode bool
}

// PRTaskLink is a task in the drawer.
type PRTaskLink struct {
	Role, ID, Title, Status, TimingHref string
}

// PRDrawer is the detail of one pull request.
type PRDrawer struct {
	PRRow
	Tasks     []PRTaskLink
	Opened    string
	Checked   string
	CloseHref string
	Override  string // the defect's base override, when a retarget set one
	Facts     []PRFact
	Retarget  *PRRetarget
}

// PRRetarget is the drawer's "change the base branch" block of a fix pull request (design §6.1): the
// bases the select offers, the form's address, and, when the button is off, the one sentence that says
// why and how to turn it on (the first row of the design's table that applies).
type PRRetarget struct {
	PostURL string
	Bases   []string
	Off     string
}

// PRFact is a label and a value in the drawer.
type PRFact struct{ K, V string }

// PRGroup is a product's rows.
type PRGroup struct {
	ProductID, Product string
	Rows               []PRRow
}

// PRPage is what the template prints.
type PRPage struct {
	OOB       bool
	PollURL   string
	GitHub    AFGitHub
	Filter    PRFilter
	StateTabs []AFTab
	KindOpts  []AFOpt
	ProdOpts  []AFOpt
	Groups    []PRGroup
	RowsTotal int
	RowsShown int
	Pager     Pager  // ten rows a page (D-097)
	Empty     string // "" | "none" | "filtered"
	ResetHref string
	Note      string
	Drawer    *PRDrawer
}

// prHref is the view's own address, relative to the page's base.
func prHref(f PRFilter, live ...string) string {
	v := url.Values{}
	if f.Product != "" {
		v.Set("product", f.Product)
	}
	if f.State != "" && f.State != "open" {
		v.Set("state", f.State)
	}
	if f.Kind != "" {
		v.Set("kind", f.Kind)
	}
	if f.Open != "" {
		v.Set("open", f.Open)
	}
	if f.Page > 1 {
		v.Set("page", strconv.Itoa(f.Page))
	}
	path := "ui/bo/prs"
	if len(live) > 0 {
		path += "/live"
	}
	if len(v) == 0 {
		return path
	}
	return path + "?" + v.Encode()
}

// prGitHubURL rebuilds a pull request's address from its validated parts; "" when they are not valid.
func prGitHubURL(repo string, n int) string {
	if n <= 0 || !defectRepo.MatchString(repo) {
		return ""
	}
	return "https://github.com/" + repo + "/pull/" + strconv.Itoa(n)
}

// prChipState maps a record's state to the chip's class and label.
func prChipState(state string) (class, label string) {
	switch state {
	case FixPRDraft:
		return "draft", "Draft"
	case FixPRMerged:
		return "merged", "Merged"
	case FixPRClosed:
		return "closed", "Closed"
	}
	return "ready", "Open"
}

// prBucket is the tab a state belongs to.
func prBucket(state string) string {
	switch state {
	case FixPRMerged:
		return "merged"
	case FixPRClosed:
		return "closed"
	}
	return "open"
}

func (s *server) buildPRs(r *http.Request, bo *BOView) {
	bo.Title = "Pull requests"
	bo.Sub = "Every open pull request that relates to a task in GAOS, per product: the draft fixes GAOS opens, the pull requests it reviews, and the ones the QA tool opens. GAOS never merges or marks one ready."
	bo.PRs = s.prPage(r.Context(), r.URL.Query(), time.Now(), prViewerIsAdmin(r))
}

func (s *server) prPage(ctx context.Context, q url.Values, now time.Time, admin bool) *PRPage {
	p := &PRPage{}
	products, _ := s.store.Products(ctx)
	known := map[string]string{}
	for _, pr := range products {
		known[pr.ID] = pr.Name
	}
	f := PRFilter{State: "open"}
	if _, ok := known[q.Get("product")]; ok {
		f.Product = q.Get("product")
	}
	for _, t := range prStateTabs {
		if q.Get("state") == t.Value {
			f.State = t.Value
		}
	}
	for _, k := range prKindTabs {
		if k.Value != "" && q.Get("kind") == k.Value {
			f.Kind = k.Value
		}
	}
	f.Open = q.Get("open")
	f.Page = pageParam(q.Get("page"))
	p.GitHub = s.afGitHub(now)
	p.ResetHref = prHref(PRFilter{State: "open"})

	var all []PRRow
	var noAutomation bool
	var drawer *PRDrawer
	for _, pr := range products {
		if f.Product != "" && pr.ID != f.Product {
			continue
		}
		fixes, _ := s.store.FixPRs(ctx, pr.ID)
		rows, _ := s.store.PullRequests(ctx, pr.ID)
		defects, _ := s.store.Defects(ctx, pr.ID)
		cards, _ := s.store.PRCards(ctx, pr.ID)
		af, _ := s.store.AutoFixOf(ctx, pr.ID)
		own := masterRepos(s, ctx, pr.ID)
		if len(af.AutomationRepos) == 0 {
			noAutomation = true
		}
		byDefect := map[string]Defect{}
		for _, d := range defects {
			byDefect[d.Key] = d
		}
		byCard := map[string]TaskRecord{}
		for _, c := range cards {
			byCard[c.ID] = c
		}
		byFix := map[string]FixPR{}
		for _, fx := range fixes {
			byFix[fx.ID] = fx
		}
		link := func(row *PRRow, taskID string) {
			if taskID == "" {
				return
			}
			row.TaskID = taskID
			row.TimingHref = timingURL(pr.ID, taskID)
			if c, ok := byCard[taskID]; ok {
				row.TaskTitle, row.TaskStatus = afLine(c.Title, 90), c.Status
			}
		}
		for _, fx := range fixes {
			if fx.Number <= 0 || fx.State == FixPRPreparing {
				continue
			}
			cls, lab := prChipState(fx.State)
			row := PRRow{Key: "fix:" + fx.ID, Kind: "fix", KindLabel: prKindLabels["fix"], State: cls, StateLabel: lab,
				Product: pr.Name, ProductID: pr.ID, Repo: fx.Repo, Number: fx.Number, URL: prGitHubURL(fx.Repo, fx.Number)}
			if prRefOK(fx.Branch) {
				row.Head = fx.Branch
			}
			if prRefOK(fx.Base) {
				row.Base = fx.Base
			}
			if d, ok := byDefect[fx.DefectKey]; ok {
				row.Defect, row.Title = d.Ref(), afLine(d.Title, 140)
				if row.Base == "" && d.Kind == DefectKindUnit && prRefOK(d.UnitBase) {
					row.Base = d.UnitBase // the branch whose tests failed (D-105)
				}
			}
			if row.Title == "" {
				row.Title = fx.DefectKey
			}
			link(&row, fx.TaskID)
			row.Age, row.AgeTitle, row.At = prAge(now, fx.CreatedAt), afClock(fx.CreatedAt), fx.CreatedAt
			if (fx.State == FixPRDraft || fx.State == FixPRReady) && now.Unix()-fx.CreatedAt > prNoFollowDays*86400 {
				row.Follow = "not followed since " + afClock(fx.CreatedAt+prNoFollowDays*86400)
			}
			for _, rel := range []struct{ label, id string }{{"Retargeted from", fx.RetargetedFrom}, {"Superseded by", fx.RetargetedTo}} {
				if o, ok := byFix[rel.id]; ok && o.Number > 0 {
					row.Relations = append(row.Relations, PRRelation{Label: rel.label, Ref: o.Repo + "#" + strconv.Itoa(o.Number), URL: prGitHubURL(o.Repo, o.Number)})
				}
			}
			row.Href = prHref(PRFilter{Product: f.Product, State: f.State, Kind: f.Kind, Open: row.Key})
			row.Open = f.Open == row.Key
			if row.Open {
				drawer = prDrawerOf(row, pr.ID, fx.CreatedAt, 0, byDefect[fx.DefectKey], byCard, f, now)
				drawer.Retarget = prRetargetOf(pr.ID, fx, byDefect[fx.DefectKey], byCard, af, fixes, admin)
			}
			all = append(all, row)
		}
		for _, pq := range rows {
			cls, lab := prChipState(pq.State)
			row := PRRow{Key: "pr:" + pq.Key, Kind: pq.Kind, KindLabel: prKindLabels[pq.Kind], State: cls, StateLabel: lab,
				Product: pr.Name, ProductID: pr.ID, Repo: pq.Repo, Number: pq.Number, URL: prGitHubURL(pq.Repo, pq.Number),
				Title: afLine(pq.Title, 140), Head: pq.HeadRef, Base: pq.BaseRef, QACase: afLine(pq.QACase, 40), QAJob: afLine(pq.QAJob, 60),
				Bundle: afLine(pq.BundleKey, 80), NoTaskCode: pq.Alerted == alertNoTaskCode}
			if strings.HasPrefix(pq.QALink, "https://") {
				row.QALink = pq.QALink
			}
			if row.Title == "" {
				row.Title = pq.Repo + "#" + strconv.Itoa(pq.Number)
			}
			link(&row, pq.TaskID)
			row.Age, row.AgeTitle, row.At = prAge(now, pq.OpenedAt), afClock(pq.OpenedAt), pq.OpenedAt
			switch {
			case !prFinal(pq.State) && !productOwns(prRepos(&Master{AutoFix: af, Repos: own}, pq.Kind), ownerOf(pq.Repo), repoOf(pq.Repo)):
				row.Follow = "no longer followed"
			case !prFinal(pq.State) && pq.CheckedAt > 0 && now.Unix()-pq.CheckedAt > prNoFollowDays*86400:
				row.Follow = "not followed since " + afClock(pq.CheckedAt)
			}
			row.Href = prHref(PRFilter{Product: f.Product, State: f.State, Kind: f.Kind, Open: row.Key})
			row.Open = f.Open == row.Key
			if row.Open {
				drawer = prDrawerOf(row, pr.ID, pq.OpenedAt, pq.CheckedAt, Defect{}, byCard, f, now)
			}
			all = append(all, row)
		}
	}

	// counts per tab, over the rows in scope but before the state and kind filters
	counts := map[string]int{}
	for _, row := range all {
		counts[prBucket(rowState(row))]++
		counts["all"]++
	}
	for _, t := range prStateTabs {
		g := f
		g.State, g.Open, g.Page = t.Value, "", 0
		p.StateTabs = append(p.StateTabs, AFTab{Value: t.Value, Label: t.Label, Href: prHref(g), Count: counts[t.Value], On: f.State == t.Value})
	}
	var shown []PRRow
	for _, row := range all {
		if f.State != "all" && prBucket(rowState(row)) != f.State {
			continue
		}
		if f.Kind != "" && row.Kind != f.Kind {
			continue
		}
		shown = append(shown, row)
	}
	sort.SliceStable(shown, func(i, j int) bool { return shown[i].At > shown[j].At })
	p.RowsTotal = len(shown)
	// One page (D-097). The page is clamped first, and everything that writes
	// an address -- the filter, the poll, each row's link -- writes the clamped one.
	p.Pager = paginate(len(shown), f.Page, tablePageSize, func(n int) (string, string) {
		g := f
		g.Open, g.Page = "", n
		return prHref(g), prPageHref(g)
	})
	p.Pager.Sync, p.Pager.Label = "#prsLive:replace", "Pull request pages"
	f.Page = p.Pager.Page
	if f.Page <= 1 {
		f.Page = 0
	}
	p.Filter = f
	p.PollURL = prHref(f, "live")
	shown = sliceFor(shown, p.Pager)
	for i := range shown {
		shown[i].Href = prHref(PRFilter{Product: f.Product, State: f.State, Kind: f.Kind, Open: shown[i].Key, Page: f.Page})
	}
	p.RowsShown = len(shown)
	byProd := map[string]*PRGroup{}
	for _, pr := range products {
		if f.Product != "" && pr.ID != f.Product {
			continue
		}
		g := PRGroup{ProductID: pr.ID, Product: pr.Name}
		byProd[pr.ID] = &g
	}
	for _, row := range shown {
		if g, ok := byProd[row.ProductID]; ok {
			g.Rows = append(g.Rows, row)
		}
	}
	for _, pr := range products {
		if g, ok := byProd[pr.ID]; ok && len(g.Rows) > 0 {
			p.Groups = append(p.Groups, *g)
		}
	}
	switch {
	case len(all) == 0:
		p.Empty = "none"
	case len(shown) == 0:
		p.Empty = "filtered"
	}
	if noAutomation && (f.Kind == "" || f.Kind == PRKindQA) {
		p.Note = "QA-tool pull requests are listed once a product's automation repositories are set (the automationRepos field of POST /products/{id}/autofix)."
	}
	for _, k := range prKindTabs {
		p.KindOpts = append(p.KindOpts, AFOpt{Value: k.Value, Label: k.Label, On: f.Kind == k.Value})
	}
	p.ProdOpts = append(p.ProdOpts, AFOpt{Value: "", Label: "Every product", On: f.Product == ""})
	for _, pr := range products {
		p.ProdOpts = append(p.ProdOpts, AFOpt{Value: pr.ID, Label: afLine(pr.Name, 40), On: f.Product == pr.ID})
	}
	if drawer != nil {
		drawer.CloseHref = prHref(PRFilter{Product: f.Product, State: f.State, Kind: f.Kind, Page: f.Page})
		p.Drawer = drawer
	}
	return p
}

// rowState recovers the record state a chip class stands for.
func rowState(r PRRow) string {
	switch r.State {
	case "merged":
		return FixPRMerged
	case "closed":
		return FixPRClosed
	}
	return PRStateOpen
}

func prAge(now time.Time, at int64) string {
	if at <= 0 {
		return ""
	}
	return relSpan(now.Sub(time.Unix(at, 0)))
}

func ownerOf(repo string) string {
	o, _, _ := ghOwnerRepo(repo)
	return o
}

func repoOf(repo string) string {
	_, r, _ := ghOwnerRepo(repo)
	return r
}

// masterRepos are the product's own repositories (empty when the product is unknown).
func masterRepos(s *server, ctx context.Context, id string) []string {
	m, err := s.store.Master(ctx, id)
	if err != nil {
		return nil
	}
	return m.Repos
}

func prDrawerOf(row PRRow, productID string, opened, checked int64, d Defect, byCard map[string]TaskRecord, f PRFilter, now time.Time) *PRDrawer {
	dr := &PRDrawer{PRRow: row, Opened: afClock(opened)}
	if checked > 0 {
		dr.Checked = afClock(checked)
	}
	add := func(role, id string) {
		if id == "" {
			return
		}
		for _, t := range dr.Tasks {
			if t.ID == id {
				return
			}
		}
		t := PRTaskLink{Role: role, ID: id, TimingHref: timingURL(productID, id)}
		if c, ok := byCard[id]; ok {
			t.Title, t.Status = afLine(c.Title, 100), c.Status
		}
		dr.Tasks = append(dr.Tasks, t)
	}
	add("Card", row.TaskID)
	if d.Key != "" {
		add("Fix card", d.FixTask)
		add("Triage card", d.TriageTask)
		dr.Override = d.BaseOverride
	}
	dr.Facts = append(dr.Facts, PRFact{"Kind", row.KindLabel})
	if row.Head != "" {
		dr.Facts = append(dr.Facts, PRFact{"Branch", row.Head})
	}
	if row.Base != "" {
		dr.Facts = append(dr.Facts, PRFact{"Base", row.Base})
	}
	if row.Defect != "" {
		dr.Facts = append(dr.Facts, PRFact{"Defect", row.Defect})
	}
	if row.QACase != "" {
		dr.Facts = append(dr.Facts, PRFact{"QA case", row.QACase})
	}
	return dr
}

// handlePRsLive is GET /ui/bo/prs/live: the 30 s poll. It answers the list as one out-of-band
// element that replaces #prsLive in place, from the same builder as the page. The drawer is not part
// of it (design §5.2).
func (s *server) handlePRsLive(w http.ResponseWriter, r *http.Request) {
	if !s.requireBO(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	q := r.URL.Query()
	q.Del("open")
	page := s.prPage(r.Context(), q, time.Now(), prViewerIsAdmin(r))
	page.OOB = true
	s.rend.Render(w, http.StatusOK, "prs-live", page)
}

// prViewerIsAdmin says whether the signed-in account is on the auto-fix admin list.
func prViewerIsAdmin(r *http.Request) bool {
	ss, ok := sessionOf(r)
	return ok && ss.Email != "" && autoFixAdmin(ss.Email)
}

// prRetargetOf builds the retarget block of a fix pull request's drawer. Off is the FIRST row of the
// design's table (§6.1) that applies, in that order; "" means the button is on. It makes no GitHub
// call: the bases offered are the allowed ones minus the current base, and the route checks that the
// chosen one exists.
func prRetargetOf(productID string, fx FixPR, d Defect, byCard map[string]TaskRecord, af AutoFix, all []FixPR, admin bool) *PRRetarget {
	rt := &PRRetarget{PostURL: "products/" + url.PathEscape(productID) + "/prs/" + url.PathEscape(fx.ID) + "/retarget"}
	for _, b := range af.AllowedBases {
		if !strings.EqualFold(b, fx.Base) && baseBranchRe.MatchString(b) {
			rt.Bases = append(rt.Bases, b)
		}
	}
	chain := 0
	for _, o := range all {
		if o.DefectKey == fx.DefectKey && o.RetargetedFrom != "" {
			chain++
		}
	}
	card, hasCard := byCard[d.FixTask]
	switch {
	case !admin:
		rt.Off = "Only accounts on the auto-fix admin list can change a base branch. Ask an admin, or ask to be added."
	case !strings.HasPrefix(fx.Branch, fixRefPrefix):
		rt.Off = "GAOS only changes the base of pull requests it opened (branch gaos/fix-…)."
	case d.Kind == DefectKindUnit:
		rt.Off = unitNoRetarget
	case fx.State != FixPRDraft && fx.State != FixPRReady:
		rt.Off = "This pull request is closed. Nothing to change."
	case fx.Base == "":
		rt.Off = "The base branch of this pull request is not known yet. GAOS reads it within 5 minutes; reopen this panel then."
	case fx.RetargetedTo != "":
		rt.Off = "This pull request was already retargeted (see the newer one). Open that one to retarget it again."
	case hasCard && (liveFixCard(&card) || (d.BaseOverride != "" && (card.Status == StatusBlocked || card.Status == StatusFailed))):
		rt.Off = "A task is already working on this defect (task " + card.ID + ", status " + strings.ToLower(card.Status) + "). If it is blocked, unblock it on the board or close this pull request."
	case chain >= retargetMaxChain:
		rt.Off = "This defect was retargeted three times. Change the product's base branch setting instead."
	case len(rt.Bases) == 0:
		rt.Off = "No other base branch is allowed. An admin sets allowed bases with POST /products/{id}/autofix (field allowedBases); leaving the field out clears the list."
	}
	return rt
}
