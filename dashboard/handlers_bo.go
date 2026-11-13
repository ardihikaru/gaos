package main

import (
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// The back office: the product master, and the only part of the UI that writes
// records rather than watching them.
//
// AUTH IS A PLACEHOLDER AND SAYS SO. The prototype's own note (line 1194) is
// that nothing is checked and any value opens the panel. That is preserved
// exactly: a cookie, no password comparison, no session store. Real auth is
// pass 2 and its shape is already written down next door -- pustaka's
// HMAC-signed cookie, credential fingerprint and per-IP throttle
// (pustaka/main.go:645-1244) is the thing to lift, not a new invention.
//
// What IS enforced now is the boundary shape: every back-office route checks
// the cookie, so when the check becomes real there is exactly one place to
// change and no route that quietly never had one.

const (
	boCookie     = "gaos_bo"
	tenantCookie = "gaos_tenant"
)

func signedIn(r *http.Request) bool {
	c, err := r.Cookie(boCookie)
	return err == nil && c.Value == "1"
}

// tenantOf reads the label shown in the back-office header.
//
// The value is URL-escaped going in and out. It contains a middle dot, and
// net/http drops invalid bytes from a Cookie.Value silently -- you get a
// warning in the log and a truncated header, not an error. Escaping is one line
// and removes the whole class.
func tenantOf(r *http.Request) string {
	c, err := r.Cookie(tenantCookie)
	if err != nil || c.Value == "" {
		return "loop-eng"
	}
	v, err := url.QueryUnescape(c.Value)
	if err != nil {
		return "loop-eng"
	}
	return v
}

func (s *server) handleLogin(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	user := strings.TrimSpace(r.FormValue("email"))
	tenant := r.FormValue("tenant")
	if tenant == "" {
		tenant = "loop-eng"
	}

	if user == "" || !strings.Contains(user, "@") {
		v, _ := s.newView(r)
		v.ShowLogin = true
		v.LoginErr = "Enter the email you use for this tenant."
		s.rend.Render(w, http.StatusUnprocessableEntity, "login-sheet", v)
		return
	}

	// Secure is deliberately NOT scheme-derived. Both deployed tiers sit behind
	// real TLS at the edge and see plain HTTP inside the tunnel, so deriving
	// the flag from r.TLS would drop it in production. It is env-driven, and it
	// defaults on -- the same rule cubiq applies to its admin cookie.
	http.SetCookie(w, &http.Cookie{
		Name: boCookie, Value: "1", Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: secureCookies(),
	})
	http.SetCookie(w, &http.Cookie{
		Name: tenantCookie, Value: url.QueryEscape(tenant + " · " + user), Path: "/",
		SameSite: http.SameSiteLaxMode, Secure: secureCookies(),
	})

	// Both sheets in one response: the login sheet closes as the back office
	// opens, and neither transition depends on the client remembering which
	// one it was looking at.
	v, _ := s.newView(r)
	v.SignedIn = true
	v.ShowLogin = false
	v.Tenant = tenant + " · " + user
	v.BO = s.buildBO(r, "products", "", "brd")
	s.rend.Render(w, http.StatusOK, "sheets", v)
}

func secureCookies() bool {
	// GAOS_COOKIE_SECURE=0 is for a plain-HTTP local stack. Anything else, on.
	return envOr("GAOS_COOKIE_SECURE", "1") != "0"
}

func (s *server) handleLogout(w http.ResponseWriter, r *http.Request) {
	for _, n := range []string{boCookie, tenantCookie} {
		http.SetCookie(w, &http.Cookie{Name: n, Value: "", Path: "/", MaxAge: -1})
	}
	v, _ := s.newView(r)
	v.SignedIn = false
	v.ShowLogin = false
	v.BO = nil
	v.Toast = "Signed out of the back office."
	s.rend.RenderWithChrome(w, "sheets", v)
}

func (s *server) requireBO(w http.ResponseWriter, r *http.Request) bool {
	if signedIn(r) {
		return true
	}
	v, _ := s.newView(r)
	v.ShowLogin = true
	s.rend.Render(w, http.StatusUnauthorized, "login-sheet", v)
	return false
}

// ---------- views ----------

func (s *server) handleBOSheet(w http.ResponseWriter, r *http.Request) {
	v, _ := s.newView(r)
	if !signedIn(r) {
		v.ShowLogin = true
		s.rend.Render(w, http.StatusOK, "login-sheet", v)
		return
	}
	q := r.URL.Query()
	v.BO = s.buildBO(r, def(q.Get("view"), "products"), q.Get("product"), def(q.Get("tab"), "brd"))
	s.rend.Render(w, http.StatusOK, "bo-sheet", v)
}

func (s *server) handleBOView(w http.ResponseWriter, r *http.Request) {
	if !s.requireBO(w, r) {
		return
	}
	q := r.URL.Query()
	v, _ := s.newView(r)
	v.BO = s.buildBO(r, r.PathValue("view"), q.Get("product"), def(q.Get("tab"), "brd"))
	s.rend.Render(w, http.StatusOK, "bo-main-oob", v)
}

func (s *server) handleProductList(w http.ResponseWriter, r *http.Request) {
	if !s.requireBO(w, r) {
		return
	}
	v, _ := s.newView(r)
	v.BO = s.buildBO(r, "products", "", "brd")
	if r.URL.Query().Get("new") == "1" {
		v.BO.NewOpen = true
	}
	s.rend.Render(w, http.StatusOK, "bo-main-oob", v)
}

func (s *server) handleProductDetail(w http.ResponseWriter, r *http.Request) {
	if !s.requireBO(w, r) {
		return
	}
	v, _ := s.newView(r)
	v.BO = s.buildBO(r, "products", r.PathValue("id"), def(r.URL.Query().Get("tab"), "brd"))
	if v.BO.Detail == nil {
		s.fail(w, r, ErrNotFound)
		return
	}
	s.rend.Render(w, http.StatusOK, "bo-main-oob", v)
}

func def(v, d string) string {
	if v == "" {
		return d
	}
	return v
}

// buildBO assembles the whole back-office state for one request. It is one
// function rather than five because the nav counts are shown on every view, and
// a per-view builder would recompute them five slightly different ways.
func (s *server) buildBO(r *http.Request, view, productID, tab string) *BOView {
	masters, _ := s.store.Masters(r.Context())
	products, _ := s.store.Products(r.Context())
	f, _ := s.store.Snapshot(r.Context())

	name := func(id string) string {
		for _, p := range products {
			if p.ID == id {
				return p.Name
			}
		}
		return id
	}
	goal := func(id string) string {
		for _, p := range products {
			if p.ID == id {
				return p.Goal
			}
		}
		return ""
	}

	bo := &BOView{View: view}
	for _, m := range masters {
		bo.Counts.Products++
		bo.Counts.Stories += len(m.Stories)
		bo.Counts.Diagrams += len(m.Diagrams)
		bo.Counts.Knowledge += len(m.Decisions)
	}
	bo.Counts.Agents = len(agents)

	switch view {
	case "inbox":
		s.buildInbox(r, bo, productID, name)
		return bo

	case "agents":
		bo.Title, bo.Sub = "Agents", "Roles the scheduler can seat. Capability and model routing are read from here when a task is dispatched."
		for _, a := range agents {
			role := roles[a.Role]
			row := AgentRow{Style: cssVar("background", role.Color), Short: role.Short, Name: a.Name, Label: role.Label, Seated: "idle"}
			for _, d := range f.Desks {
				if d.State == StateIdle {
					continue
				}
				for _, id := range d.Agents {
					if id == a.ID {
						row.Seated = "seated · " + d.Name
						row.IsOn = true
					}
				}
			}
			bo.Agents = append(bo.Agents, row)
		}
		return bo

	case "stories", "diagrams", "knowledge":
		bo.Title = map[string]string{"stories": "User stories", "diagrams": "Diagrams", "knowledge": "Knowledge"}[view]
		bo.Sub = map[string]string{
			"stories":   "Every story across the tenant, grouped by product. A story with no task is work nobody has planned yet.",
			"diagrams":  "Sequences and models the architect reads before proposing a boundary change.",
			"knowledge": "Decisions and lessons reused by the planner on the next similar goal.",
		}[view]
		for _, m := range masters {
			g := RollupGroup{ProductID: m.ID, Name: name(m.ID)}
			switch view {
			case "stories":
				g.Stories = m.Stories
			case "diagrams":
				g.Diagrams = m.Diagrams
			case "knowledge":
				g.Decisions = m.Decisions
			}
			if len(g.Stories)+len(g.Diagrams)+len(g.Decisions) > 0 {
				bo.Rollup = append(bo.Rollup, g)
			}
		}
		return bo
	}

	// products
	bo.Title, bo.Sub = "Products", "One record per product. Agents read this before planning, so an empty record produces a vague plan."
	if productID == "" {
		for _, m := range masters {
			pr := m.Project()
			repo := "no repo"
			if len(m.Repos) > 0 {
				repo = m.Repos[0]
			}
			bo.List = append(bo.List, ProductCard{
				ID: m.ID, Code: m.Code, Name: name(m.ID), Repo: repo, Owner: m.Owner,
				Problem: clipEllipsis(m.BRD.Problem, 110),
				Stories: len(m.Stories), Diagrams: len(m.Diagrams),
				Vertices: pr.Vertices, Coverage: m.Coverage(),
			})
		}
		return bo
	}

	for _, m := range masters {
		if m.ID != productID {
			continue
		}
		d := &ProductDetail{
			ID: m.ID, Name: name(m.ID), Goal: goal(m.ID), Owner: m.Owner,
			Repos: def(strings.Join(m.Repos, ", "), "no repository linked"),
			Tab:   tab, Master: m, Projection: m.Project(),
			Coverage: m.Coverage(), CovStyle: pctStyle("width", float64(m.Coverage())),
			WithTasks: m.StoriesWithTasks(), Readiness: m.Readiness(),
			Score: m.ReadinessScore(), Band: ReadinessBand(m.ReadinessScore()),
			CanPlan: m.ReadinessScore() >= PlanFloor,
		}
		if tab == "board" {
			if b, err := s.store.Board(r.Context(), m.ID); err == nil {
				d.Board = &b
			}
		}
		if tab == "qa" {
			d.QA = s.buildQAPane(r.Context(), m)
		}
		if tab == "vendors" {
			d.Vendors = buildVendorsPane(r.Context(), m, s.adapter)
		}
		if evs, err := s.store.Events(r.Context()); err == nil {
			for i := len(evs) - 1; i >= 0; i-- {
				if evs[i].DeskID == m.ID {
					d.Timeline = append(d.Timeline, evs[i])
				}
			}
		}
		for _, g := range m.Diagrams {
			dv := DiagramView{Diagram: g}
			if g.Kind == "sequence" {
				dv.SVG = RenderSequence(g.Src)
			}
			d.Diagrams = append(d.Diagrams, dv)
		}
		bo.Detail = d
	}
	return bo
}

func clipEllipsis(s string, n int) string {
	if len([]rune(s)) <= n {
		return s
	}
	return clip(s, n) + "…"
}

// ---------- mutations ----------

// Each one writes through the store, then re-renders the whole back-office main
// pane plus the nav counts out of band. Re-rendering the pane rather than the
// one changed block is deliberate for pass 1: the readiness panel, the coverage
// bar, the projection counts and the tab badges all move when a story is added,
// and four out-of-band fragments is more code and more ways to forget one.

func (s *server) handleProductCreate(w http.ResponseWriter, r *http.Request) {
	if !s.requireBO(w, r) {
		return
	}
	_ = r.ParseForm()
	res, err := s.mut(r).CreateProduct(r.Context(), ProductDraft{
		Name: r.FormValue("name"), Owner: r.FormValue("owner"),
		Repo: r.FormValue("repo"), Goal: r.FormValue("goal"),
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.renderBOAfter(w, r, res, res.Master.ID, "brd")
}

func (s *server) handleBRDSave(w http.ResponseWriter, r *http.Request) {
	if !s.requireBO(w, r) {
		return
	}
	_ = r.ParseForm()
	id := r.PathValue("id")
	res, err := s.mut(r).UpdateBRD(r.Context(), id, BRD{
		Problem:    strings.TrimSpace(r.FormValue("problem")),
		Goals:      splitLines(r.FormValue("goals")),
		Rules:      splitLines(r.FormValue("rules")),
		Scope:      splitLines(r.FormValue("scope")),
		OutOfScope: splitLines(r.FormValue("outofscope")),
		Metrics:    splitLines(r.FormValue("metrics")),
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.renderBOAfter(w, r, res, id, "brd")
}

func (s *server) handleAddStory(w http.ResponseWriter, r *http.Request) {
	if !s.requireBO(w, r) {
		return
	}
	_ = r.ParseForm()
	id := r.PathValue("id")
	res, err := s.mut(r).AddStory(r.Context(), id, Story{
		Pri:  r.FormValue("pri"),
		Role: strings.TrimSpace(r.FormValue("role")),
		Want: strings.TrimSpace(r.FormValue("want")),
		So:   strings.TrimSpace(r.FormValue("so")),
		AC:   splitLines(r.FormValue("ac")),
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.renderBOAfter(w, r, res, id, "stories")
}

func (s *server) handleAddDiagram(w http.ResponseWriter, r *http.Request) {
	if !s.requireBO(w, r) {
		return
	}
	_ = r.ParseForm()
	id := r.PathValue("id")
	res, err := s.mut(r).AddDiagram(r.Context(), id, Diagram{
		Kind:  r.FormValue("kind"),
		Title: strings.TrimSpace(r.FormValue("title")),
		Src:   strings.TrimSpace(r.FormValue("src")),
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.renderBOAfter(w, r, res, id, "diagrams")
}

func (s *server) handleAddDecision(w http.ResponseWriter, r *http.Request) {
	if !s.requireBO(w, r) {
		return
	}
	_ = r.ParseForm()
	id := r.PathValue("id")
	conf := float64(atoiDefault(r.FormValue("confidence"), 80))
	res, err := s.mut(r).AddDecision(r.Context(), id, Decision{
		ID:         strings.TrimSpace(r.FormValue("ref")),
		Title:      strings.TrimSpace(r.FormValue("title")),
		Confidence: conf / 100,
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.renderBOAfter(w, r, res, id, "knowledge")
}

func (s *server) handleInject(w http.ResponseWriter, r *http.Request) {
	if !s.requireBO(w, r) {
		return
	}
	id := r.PathValue("id")
	res, err := s.mut(r).Inject(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.renderBOAfter(w, r, res, id, def(r.URL.Query().Get("tab"), "brd"))
}

// handleReadiness is §09's GET /products/{id}/readiness. It answers JSON to a
// non-HTMX caller because the planner will need it before it will need HTML.
func (s *server) handleReadiness(w http.ResponseWriter, r *http.Request) {
	m, err := s.store.Master(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	items := m.Readiness()
	if r.Header.Get("HX-Request") == "" {
		met := 0
		for _, i := range items {
			if i.OK {
				met++
			}
		}
		// score/band/missing are B-3's half of the rubric and BR-07's half of the
		// refusal, answered by the same endpoint: whoever is about to be refused a
		// plan can ask here first and get the list rather than the rejection.
		score := m.ReadinessScore()
		missing := m.ReadinessMissing()
		if missing == nil {
			missing = []string{}
		}
		jsonOut(w, map[string]any{
			"product": m.ID, "met": met, "of": len(items),
			"score": score, "band": ReadinessBand(score),
			"planFloor": PlanFloor, "canPlan": score >= PlanFloor,
			"missing":  missing,
			"coverage": m.Coverage(), "items": items,
		})
		return
	}
	// readiness-block is a leaf of the product detail, so it renders against
	// the Detail struct, not the whole View. Handing it the envelope produces a
	// silent empty list -- or, with a range over a missing field, a 500.
	bo := s.buildBO(r, "products", m.ID, "brd")
	if bo.Detail == nil {
		s.fail(w, r, ErrNotFound)
		return
	}
	s.rend.Render(w, http.StatusOK, "readiness-block", bo.Detail)
}

func (s *server) renderBOAfter(w http.ResponseWriter, r *http.Request, res Result, productID, tab string) {
	v := s.applyResult(r, res)
	v.BO = s.buildBO(r, "products", productID, tab)
	s.rend.RenderWithChrome(w, "bo-main-oob", v)
}

// The Blueprint tab (D-058, T-153).

func (s *server) handleSavePolicy(w http.ResponseWriter, r *http.Request) {
	if !s.requireBO(w, r) {
		return
	}
	_ = r.ParseForm()
	id := r.PathValue("id")
	res, err := s.mut(r).SavePolicy(r.Context(), id, Policy{
		Key: r.FormValue("key"), Kind: r.FormValue("kind"), Title: r.FormValue("title"),
		Body: r.FormValue("body"), Order: atoiDefault(r.FormValue("order"), 0),
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.renderBOAfter(w, r, res, id, "blueprint")
}

func (s *server) handleAddSource(w http.ResponseWriter, r *http.Request) {
	if !s.requireBO(w, r) {
		return
	}
	_ = r.ParseForm()
	id := r.PathValue("id")
	res, err := s.mut(r).AddKnowledgeSource(r.Context(), id, KnowledgeSource{
		URL: r.FormValue("url"), Ref: r.FormValue("ref"), Purpose: r.FormValue("purpose"), Note: strings.TrimSpace(r.FormValue("note")),
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.renderBOAfter(w, r, res, id, "blueprint")
}

func (s *server) handlePublishBlueprint(w http.ResponseWriter, r *http.Request) {
	if !s.requireBO(w, r) {
		return
	}
	id := r.PathValue("id")
	res, err := s.mut(r).PublishBlueprint(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.renderBOAfter(w, r, res, id, "blueprint")
}

func (s *server) handleSyncBlueprint(w http.ResponseWriter, r *http.Request) {
	if !s.requireBO(w, r) {
		return
	}
	id := r.PathValue("id")
	res, err := s.mut(r).SyncBlueprint(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.renderBOAfter(w, r, res, id, "blueprint")
}

// The AI vendors tab (M16, D-067).

func (s *server) handleSaveVendors(w http.ResponseWriter, r *http.Request) {
	if !s.requireBO(w, r) {
		return
	}
	_ = r.ParseForm()
	id := r.PathValue("id")
	res, err := s.mut(r).SaveVendorPolicy(r.Context(), id, vendorPolicyFromForm(r.PostForm))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.renderBOAfter(w, r, res, id, "vendors")
}

// ---------- "Needs you" (M15) ----------
//
// The inbox and the answer sheet are views of Store.OpenQuestions; nothing here
// writes. The answer itself posts to handleAnswer (handlers_questions.go).

// urgentBefore is when a question's expiry turns urgent: three days before it
// closes with its session (D-066) -- a long weekend is enough to miss it.
const urgentBefore = 3 * 24 * time.Hour

// relSpan is one duration as the inbox prints it: <1m, 12m, 5h (under two
// days), 9d. static/live.js formats the same way when it re-ticks the label,
// so a server render and a client tick never disagree.
func relSpan(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "<1m"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d/time.Hour))
	}
	return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
}

// expiryFor renders when a question closes with its session: "expires in 29d",
// the datetime, and whether it is within urgentBefore. Nothing for a question
// whose ask time was not recorded, which never expires.
func expiryFor(asked int64, now time.Time) (rel, iso string, urgent bool) {
	exp := questionExpiresAt(asked)
	if exp == 0 {
		return "", "", false
	}
	t := time.Unix(exp, 0).UTC()
	left := t.Sub(now)
	if left <= 0 {
		return "expires now", t.Format(time.RFC3339), true
	}
	return "expires in " + relSpan(left), t.Format(time.RFC3339), left < urgentBefore
}

// needsCount is the topbar chip's number. Signed out it is zero: the count is
// back-office data, and there is no inbox to open without a session.
func (s *server) needsCount(r *http.Request) int {
	if !signedIn(r) {
		return 0
	}
	qs, err := s.store.OpenQuestions(r.Context(), "")
	if err != nil {
		return 0
	}
	return len(qs)
}

// waitingFor renders how long a question has waited so far and the absolute
// time for the title. The label is elapsed time, not a limit: static/live.js
// re-ticks it from the datetime every thirty seconds, so "waiting 12m" becomes
// "waiting 13m" without a refresh. A missing ask time says so rather than
// pretending the agent asked in 1970.
func waitingFor(asked int64, now time.Time) (rel, iso, abs string) {
	if asked <= 0 {
		return "waiting", "", "The time this was asked was not recorded."
	}
	t := time.Unix(asked, 0).UTC()
	d := now.Sub(t)
	if d < 0 {
		d = 0
	}
	return "waiting " + relSpan(d), t.Format(time.RFC3339), "Asked " + t.Format("Mon 2 Jan 2006, 15:04 UTC")
}

// splitLead is a question's first non-blank line and whatever follows it.
func splitLead(q string) (lead, rest string) {
	q = strings.TrimSpace(q)
	if i := strings.IndexByte(q, '\n'); i >= 0 {
		return strings.TrimSpace(q[:i]), strings.TrimSpace(q[i+1:])
	}
	return q, ""
}

func roleOf(key string) (string, template.CSS) {
	if r, ok := roles[key]; ok {
		return r.Label, cssVar("background", r.Color)
	}
	if key == "" {
		key = "agent"
	}
	return key, cssVar("background", "var(--ink-3)")
}

// answerSheetURL is the GET that opens the sheet; from says where it was
// opened, so the answer re-renders the view the operator came from.
func answerSheetURL(productID, taskID, from, filter string) string {
	q := url.Values{"from": {from}}
	if filter != "" {
		q.Set("product", filter)
	}
	return "ui/answer/" + url.PathEscape(productID) + "/" + url.PathEscape(taskID) + "?" + q.Encode()
}

// buildInbox fills the inbox view: every waiting agent, oldest first, narrowed
// to one product when filter is set.
func (s *server) buildInbox(r *http.Request, bo *BOView, filter string, name func(string) string) {
	bo.Title = "Needs you"
	bo.Sub = "Agents that stopped to ask you something, oldest first. Each task waits, out of the scheduler's reach, until you answer."
	bo.Filter = filter
	if filter != "" {
		bo.FilterName = name(filter)
	}
	all, _ := s.store.OpenQuestions(r.Context(), "")
	now := time.Now()
	seen := map[string]int{}
	for _, q := range all {
		if i, ok := seen[q.ProductID]; ok {
			bo.Filters[i].N++
		} else {
			seen[q.ProductID] = len(bo.Filters)
			bo.Filters = append(bo.Filters, InboxFilter{ID: q.ProductID, Name: name(q.ProductID), N: 1, On: q.ProductID == filter})
		}
		if filter != "" && q.ProductID != filter {
			continue
		}
		lead, _ := splitLead(q.Question)
		rel, iso, abs := waitingFor(q.AskedAt, now)
		exp, expISO, urgent := expiryFor(q.AskedAt, now)
		label, style := roleOf(q.Role)
		bo.Inbox = append(bo.Inbox, InboxRow{
			ProductID: q.ProductID, ProductName: name(q.ProductID),
			TaskID: q.TaskID, TaskTitle: def(q.TaskTitle, "untitled task"),
			RoleLabel: label, RoleStyle: style,
			Lead: clipEllipsis(def(lead, "(the agent sent no question text)"), 160), Question: q.Question,
			Waiting: rel, AskedISO: iso, AskedAbs: abs,
			Expires: exp, ExpiresISO: expISO, Urgent: urgent,
			OpenURL: answerSheetURL(q.ProductID, q.TaskID, "inbox", filter),
			DomID:   "inbox-" + q.ProductID + "-" + q.TaskID,
		})
	}
	// One product's chips are noise; they appear when there is a choice to
	// make, or a filter to undo.
	if len(bo.Filters) < 2 && filter == "" {
		bo.Filters = nil
	}
}

// handleNeeds is GET /ui/needs: the chip and the nav count, nothing else. The
// live channel asks for it on every question hint, so it must stay this small.
func (s *server) handleNeeds(w http.ResponseWriter, r *http.Request) {
	v, _ := s.newView(r)
	s.rend.Render(w, http.StatusOK, "needs-oob", v)
}

// handleAnswerSheet is GET /ui/answer/{id}/{taskId}: the answer sheet for the
// question a task is waiting on.
func (s *server) handleAnswerSheet(w http.ResponseWriter, r *http.Request) {
	if !s.requireBO(w, r) {
		return
	}
	id, taskID := r.PathValue("id"), r.PathValue("taskId")
	qs, err := s.store.OpenQuestions(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var q *Question
	for i := range qs {
		if qs[i].TaskID == taskID {
			q = &qs[i]
		}
	}
	if q == nil {
		s.fail(w, r, reject(taskID+" is not waiting on an answer any more."))
		return
	}
	v, _ := s.newView(r)
	v.Answer = s.buildAnswer(r, *q)
	s.rend.Render(w, http.StatusOK, "answer-modal", v)
}

func (s *server) buildAnswer(r *http.Request, q Question) *AnswerView {
	name := q.ProductID
	if ps, err := s.store.Products(r.Context()); err == nil {
		for _, p := range ps {
			if p.ID == q.ProductID {
				name = p.Name
			}
		}
	}
	lead, rest := splitLead(q.Question)
	rel, iso, abs := waitingFor(q.AskedAt, time.Now())
	exp, expISO, urgent := expiryFor(q.AskedAt, time.Now())
	label, _ := roleOf(q.Role)
	a := &AnswerView{
		ProductID: q.ProductID, ProductName: name, TaskID: q.TaskID,
		TaskTitle: def(q.TaskTitle, "untitled task"), RoleLabel: label,
		Waiting: rel, AskedISO: iso, AskedAbs: abs,
		Expires: exp, ExpiresISO: expISO, Urgent: urgent,
		Lead: def(lead, "The agent is waiting on you."), Rest: rest,
		Context: strings.TrimSpace(q.Context), Default: ChoiceOther,
	}
	if ctx := []rune(a.Context); len(ctx) > 280 || strings.Count(a.Context, "\n") >= 3 {
		a.ContextLong = true
		first, _ := splitLead(a.Context)
		a.ContextLead = clipEllipsis(first, 110)
	}
	for i, o := range q.Options {
		if i >= ChoiceOther-1 {
			break // 4 is always the operator's own words
		}
		a.Options = append(a.Options, AnswerOption{N: i + 1, Label: def(strings.TrimSpace(o.Label), fmt.Sprintf("Option %d", i+1)),
			Description: o.Description, Recommended: i == 0, Checked: i == 0})
	}
	if len(a.Options) > 0 {
		a.Default = ChoiceRecommended
	}
	pq := url.Values{}
	if f := r.URL.Query().Get("product"); f != "" {
		pq.Set("product", f)
	}
	if r.URL.Query().Get("from") == "board" {
		pq.Set("from", "board")
	}
	a.PostURL = "products/" + url.PathEscape(q.ProductID) + "/tasks/" + url.PathEscape(q.TaskID) + "/answer"
	if len(pq) > 0 {
		a.PostURL += "?" + pq.Encode()
	}
	return a
}
