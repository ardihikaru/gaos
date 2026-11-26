package main

import (
	"fmt"
	"html/template"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// The back office: the product master, and the only part of the UI that writes
// records rather than watching them.
//
// Authentication is real (auth.go, D-079, BR-55): a signed, expiring session
// cookie issued after a password check against a PBKDF2 hash file. The mux gate
// (auth.go: gate) refuses every route that is not on its fixed public list or a
// bearer route when no valid session is presented, and requireBO below is the
// second line of defence in each handler.

const (
	boCookie     = "gaos_bo"
	tenantCookie = "gaos_tenant"
)

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

// tenantLabel is what the header shows: the tenant (cosmetic, from its cookie)
// and, when signed in, the email of the VERIFIED session.
func (s *server) tenantLabel(r *http.Request) string {
	t := tenantOf(r)
	if ss, ok := s.auth.sessionFrom(r); ok {
		return t + " · " + ss.Email
	}
	return t
}

func (s *server) handleLogin(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	tenant := r.FormValue("tenant")
	if tenant == "" {
		tenant = "loop-eng"
	}

	// The order is the design's (docs/designs/bo-login.md 3.6a): the origin
	// check ran in the gate; this is "not configured", the shape check, the
	// account lock, a verification slot, then the password.
	res := s.auth.login(r.FormValue("email"), r.FormValue("password"))
	if res.Email == "" {
		if res.RetryAfter > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(res.RetryAfter.Seconds()))))
		}
		v, _ := s.newView(r)
		v.ShowLogin = true
		v.LoginErr = res.Msg
		v.LoginEmail = strings.TrimSpace(r.FormValue("email"))
		s.rend.Render(w, res.Status, "login-sheet", v)
		return
	}

	// Secure is deliberately NOT scheme-derived. Both deployed tiers sit behind
	// real TLS at the edge and see plain HTTP inside the tunnel, so deriving
	// the flag from r.TLS would drop it in production. It is env-driven, and it
	// defaults on -- the same rule cubiq applies to its admin cookie.
	http.SetCookie(w, s.auth.cookie(res.Email))
	http.SetCookie(w, &http.Cookie{
		Name: tenantCookie, Value: url.QueryEscape(tenant), Path: "/",
		SameSite: http.SameSiteLaxMode, Secure: secureCookies(),
	})

	// Signed in from a deep URL (D-094): the browser reloads the page it is
	// on, now with its session. The header's value is a constant -- nothing
	// the client sent decides where it goes.
	if loginLandsElsewhere(r) {
		w.Header().Set("HX-Refresh", "true")
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Both sheets in one response: the login sheet closes as the back office
	// opens, and neither transition depends on the client remembering which
	// one it was looking at.
	v, _ := s.newView(r)
	v.SignedIn = true
	v.ShowLogin = false
	v.Tenant = tenant + " · " + res.Email
	s.sheet(w, r, v, "products", "", "brd")
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
	if s.auth.signedIn(r) {
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
	if !s.auth.signedIn(r) {
		v.ShowLogin = true
		s.rend.Render(w, http.StatusOK, "login-sheet", v)
		return
	}
	q := r.URL.Query()
	s.sheet(w, r, v, def(q.Get("view"), "products"), q.Get("product"), def(q.Get("tab"), "brd"))
	// bo-main-oob is bo-sheet with the title in front of it.
	s.rend.Render(w, http.StatusOK, "bo-main-oob", v)
}

func (s *server) handleBOView(w http.ResponseWriter, r *http.Request) {
	if !s.requireBO(w, r) {
		return
	}
	q := r.URL.Query()
	v, _ := s.newView(r)
	// sheet sets no-store for the sensitive views (D-082, D-085) and the address.
	s.sheet(w, r, v, r.PathValue("view"), q.Get("product"), def(q.Get("tab"), "brd"))
	s.rend.Render(w, http.StatusOK, "bo-main-oob", v)
}

func (s *server) handleProductList(w http.ResponseWriter, r *http.Request) {
	if !s.requireBO(w, r) {
		return
	}
	v, _ := s.newView(r)
	s.sheet(w, r, v, "products", "", "brd")
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
	s.sheet(w, r, v, "products", r.PathValue("id"), def(r.URL.Query().Get("tab"), "brd"))
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
	// Every return below leaves through this: the address and the title of
	// what was built (urls.go, D-094).
	defer func() { s.address(bo, products, productID) }()
	slugOf := func(id string) string {
		for _, p := range products {
			if p.ID == id {
				return p.Slug
			}
		}
		return id
	}
	for _, m := range masters {
		bo.Counts.Products++
		bo.Counts.Stories += len(m.Stories)
		bo.Counts.Diagrams += len(m.Diagrams)
		bo.Counts.Knowledge += len(m.Decisions)
	}
	bo.Counts.Agents = len(agents)
	archived, _ := s.store.ArchivedConversations(r.Context())
	bo.Counts.Archived = len(archived.Archived)

	switch view {
	case "archive":
		bo.Title = "Archive"
		bo.Sub = "Conversations archived from finished cards. Open one to read it, restore it to its card, or prune them all."
		q := r.URL.Query()
		bo.Archive = archivePage(archived, pageParam(q.Get("apage")), pageParam(q.Get("ppage")))
		bo.APage, bo.PPage = bo.Archive.ArchivedPager.Page, bo.Archive.PrunedPager.Page
		return bo

	case "inbox":
		s.buildInbox(r, bo, productID, name)
		return bo

	case "integrations":
		s.buildIntegrations(r.Context(), bo)
		return bo

	case "autofix":
		s.buildAutoFix(r, bo)
		return bo

	case "prs":
		s.buildPRs(r, bo)
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
		bo.Pager = paginate(len(bo.Agents), pageParam(r.URL.Query().Get("page")), listPageSize, listPageAt(view))
		bo.Agents = sliceFor(bo.Agents, bo.Pager)
		bo.Page = bo.Pager.Page
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
		// Paged by item, not by product (D-097): an offset window walks the
		// groups in order, each keeps what overlaps it, and a group left with
		// nothing is not shown.
		total := 0
		for _, g := range bo.Rollup {
			total += len(g.Stories) + len(g.Diagrams) + len(g.Decisions)
		}
		bo.Pager = paginate(total, pageParam(r.URL.Query().Get("page")), listPageSize, listPageAt(view))
		bo.Page = bo.Pager.Page
		lo, hi := bo.Pager.From-1, bo.Pager.To
		var kept []RollupGroup
		off := 0
		for _, g := range bo.Rollup {
			n := len(g.Stories) + len(g.Diagrams) + len(g.Decisions)
			a, b := max(lo-off, 0), min(hi-off, n)
			off += n
			if a >= b {
				continue
			}
			g.Stories, g.Diagrams, g.Decisions = windowOf(g.Stories, a, b), windowOf(g.Diagrams, a, b), windowOf(g.Decisions, a, b)
			kept = append(kept, g)
		}
		bo.Rollup = kept
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
				ID: m.ID, Slug: slugOf(m.ID), Code: m.Code, Name: name(m.ID), Repo: repo, Owner: m.Owner,
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
			ID: m.ID, Slug: slugOf(m.ID), Name: name(m.ID), Goal: goal(m.ID), Owner: m.Owner,
			Repos: def(strings.Join(m.Repos, ", "), "no repository linked"),
			Tab:   tab, Master: m, Projection: m.Project(),
			Coverage: m.Coverage(), CovStyle: pctStyle("width", float64(m.Coverage())),
			WithTasks: m.StoriesWithTasks(), Readiness: m.Readiness(),
			Score: m.ReadinessScore(), Band: ReadinessBand(m.ReadinessScore()),
			CanPlan: m.ReadinessScore() >= PlanFloor,
		}
		// The Board is one page of the product's cards, in the view the
		// request carries -- whatever its method (D-095).
		if tab == "board" {
			if bv, err := s.buildBoardView(r, m.ID, d.Slug); err == nil {
				d.BoardView = bv
			}
		}
		if tab == "qa" {
			d.QA = s.buildQAPane(r.Context(), m)
			if d.QA != nil {
				d.QA.Pager = paginate(len(d.QA.Epics), pageParam(r.URL.Query().Get("page")), tablePageSize, productPageAt(m.ID, d.Slug, "qa"))
				d.QA.Pager.Label = "Epic pages"
				d.QA.Epics = sliceFor(d.QA.Epics, d.QA.Pager)
			}
		}
		if tab == "vendors" {
			d.Vendors = buildVendorsPane(r.Context(), m, s.adapter)
		}
		// The Timeline tab's label is a number on every tab; the events
		// themselves are loaded for its own tab only. Until D-095 the whole
		// stream was copied on every product request for that count.
		d.TimelineN, _ = s.store.EventCount(r.Context(), m.ID)
		if tab == "timeline" {
			if evs, err := s.store.Events(r.Context()); err == nil {
				for i := len(evs) - 1; i >= 0; i-- {
					if evs[i].DeskID == m.ID {
						d.Timeline = append(d.Timeline, evs[i])
					}
				}
			}
			d.Pager = paginate(len(d.Timeline), pageParam(r.URL.Query().Get("page")), tablePageSize, productPageAt(m.ID, d.Slug, "timeline"))
			d.Pager.Label = "Timeline pages"
			d.Timeline = sliceFor(d.Timeline, d.Pager)
		}
		// A sequence diagram is drawn for the tab that shows it.
		for _, g := range m.Diagrams {
			dv := DiagramView{Diagram: g}
			if g.Kind == "sequence" && tab == "diagrams" {
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
	s.sheet(w, r, v, "products", productID, tab)
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
	return expiryAt(questionExpiresAt(asked), now)
}

// expiryAt is expiryFor for a deadline already known: the QA tool's items carry
// their own (D-078).
func expiryAt(exp int64, now time.Time) (rel, iso string, urgent bool) {
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
	if !s.auth.signedIn(r) {
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

// externalSheetURL is the GET that opens the answer sheet of a QA tool item.
func externalSheetURL(productID, externalID, filter string) string {
	q := url.Values{"from": {"inbox"}}
	if filter != "" {
		q.Set("product", filter)
	}
	return "ui/answer-ext/" + url.PathEscape(productID) + "/" + url.PathEscape(externalID) + "?" + q.Encode()
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
		if q.External {
			// D-078: no task, no role; the source tag and the title stand in.
			exp, expISO, urgent := expiryAt(q.ExpiresAt, now)
			asker, pushed := askerLabel(q.QuestionPayload)
			bo.Inbox = append(bo.Inbox, InboxRow{
				ProductID: q.ProductID, ProductName: name(q.ProductID),
				Source: labelOf(q.QuestionPayload), Asker: asker, Pushed: pushed, TaskTitle: q.Title,
				Lead: clipEllipsis(def(lead, "("+asker+" sent no question text)"), 160), Question: q.Question,
				Waiting: rel, AskedISO: iso, AskedAbs: abs,
				Expires: exp, ExpiresISO: expISO, Urgent: urgent,
				OpenURL: externalSheetURL(q.ProductID, q.ExternalID, filter),
				DomID:   "inbox-" + q.ProductID + "-ext-" + q.ExternalID,
			})
			continue
		}
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
	bo.Recent = s.recentRows(r, filter, name, now)
	// One product's chips are noise; they appear when there is a choice to
	// make, or a filter to undo.
	if len(bo.Filters) < 2 && filter == "" {
		bo.Filters = nil
	}
}

// recentLimit is how many answers "Answered recently" shows.
const recentLimit = 20

// askerLabel is how the back office names an inbox item's asker, and whether
// that asker is told by webhook (only the QA tool is, D-083).
func askerLabel(q QuestionPayload) (string, bool) {
	if effectiveAsker(q) == ClientQATools {
		return "the QA tool", true
	}
	return labelOf(q), false
}

// recentRows is "Answered recently" (D-083): every answer with where it came
// from, newest first.
func (s *server) recentRows(r *http.Request, filter string, name func(string) string, now time.Time) []RecentRow {
	rs, err := s.store.RecentAnswers(r.Context(), filter, recentLimit)
	if err != nil {
		return nil
	}
	out := make([]RecentRow, 0, len(rs))
	for _, a := range rs {
		lead, _ := splitLead(a.Question.Question)
		row := RecentRow{ProductName: name(a.ProductID), Lead: clipEllipsis(def(lead, "(no question text)"), 140)}
		if a.External {
			row.Tag, row.Title = labelOf(a.QuestionPayload), a.Title
		} else {
			row.TaskID, row.Title = a.TaskID, a.TaskTitle
		}
		switch {
		case a.Expired:
			row.Outcome = "expired unanswered"
		case a.Choice >= 1 && a.Choice < ChoiceOther && a.Choice <= len(a.Options):
			row.Outcome = a.Options[a.Choice-1].Label
		default:
			row.Outcome = "own words: " + clipEllipsis(a.Text, 80)
		}
		if !a.Expired {
			row.By = recentSentence(a.AnsweredBy)
		}
		if a.AnsweredAt > 0 {
			t := time.Unix(a.AnsweredAt, 0).UTC()
			d := now.Sub(t)
			if d < 0 {
				d = 0
			}
			row.WhenISO, row.When = t.Format(time.RFC3339), relSpan(d)+" ago"
		}
		out = append(out, row)
	}
	return out
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

// SendBackSheetView is the send-back sheet (templates/needs.html,
// send-back-modal): the store's view and the product's name.
type SendBackSheetView struct {
	ProductID, ProductName string
	SendBackView
}

// handleSendBackSheet is GET /ui/send-back/{id}/{taskId}: the sheet that sends
// a design-blocked review's design back to its author (D-084). A card of no
// product, or of another product, is a 404 and the toast.
func (s *server) handleSendBackSheet(w http.ResponseWriter, r *http.Request) {
	if !s.requireBO(w, r) {
		return
	}
	id, taskID := r.PathValue("id"), r.PathValue("taskId")
	sv, err := s.store.SendBackSheet(r.Context(), id, taskID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	name := id
	if ps, err := s.store.Products(r.Context()); err == nil {
		for _, p := range ps {
			if p.ID == id {
				name = p.Name
			}
		}
	}
	v, _ := s.newView(r)
	v.SendBack = &SendBackSheetView{ProductID: id, ProductName: name, SendBackView: sv}
	s.rend.Render(w, http.StatusOK, "send-back-modal", v)
}

// handleTimingSheet is GET /ui/timing/{id}/{taskId}: a card's timing sheet
// (D-081). An unknown card is a 404 and the toast, as the answer sheet's is.
func (s *server) handleTimingSheet(w http.ResponseWriter, r *http.Request) {
	if !s.requireBO(w, r) {
		return
	}
	id, taskID := r.PathValue("id"), r.PathValue("taskId")
	ct, err := s.store.CardTiming(r.Context(), id, taskID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	name, title := id, taskID
	if ps, err := s.store.Products(r.Context()); err == nil {
		for _, p := range ps {
			if p.ID == id {
				name = p.Name
			}
		}
	}
	// One card, not the board: the sheet only wants its title (D-095).
	if c, err := s.store.BoardCard(r.Context(), id, taskID); err == nil {
		title = c.Label
	}
	v, _ := s.newView(r)
	v.Timing = buildTimingView(name, title, ct)
	s.rend.Render(w, http.StatusOK, "timing-modal", v)
}

// handleExternalSheet is GET /ui/answer-ext/{id}/{externalId}: the answer sheet
// for one of the QA tool's questions (D-078).
func (s *server) handleExternalSheet(w http.ResponseWriter, r *http.Request) {
	if !s.requireBO(w, r) {
		return
	}
	id, ext := r.PathValue("id"), r.PathValue("externalId")
	qs, err := s.store.OpenQuestions(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var q *Question
	for i := range qs {
		if qs[i].External && qs[i].ExternalID == ext {
			q = &qs[i]
		}
	}
	if q == nil {
		// D-083: say who got there first, when somebody did.
		why := "That question is not waiting on an answer any more."
		if x, err := s.store.ExternalGet(r.Context(), id, ext); err == nil {
			switch x.Status {
			case ExtAnswered:
				why = answeredSentence(x.AnsweredBy)
			case ExtExpired:
				why = "That question expired before it was answered."
			}
		}
		s.fail(w, r, reject(why))
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
	if q.External {
		exp, expISO, urgent = expiryAt(q.ExpiresAt, time.Now())
	}
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
			Description: o.Description, Recommended: i == 0 && !q.External, Checked: i == 0 && !q.External})
	}
	if len(a.Options) > 0 {
		a.Default = ChoiceRecommended
	}
	if q.External {
		// D-078: the QA tool recommends nothing, and an Approve preselected by
		// default is a decision nobody made. Nothing is checked; the operator picks.
		a.Source, a.TaskID, a.TaskTitle, a.RoleLabel, a.Default = labelOf(q.QuestionPayload), "", q.Title, "", 0
		a.Asker, a.Pushed = askerLabel(q.QuestionPayload)
	}
	pq := url.Values{}
	if f := r.URL.Query().Get("product"); f != "" {
		pq.Set("product", f)
	}
	if r.URL.Query().Get("from") == "board" {
		pq.Set("from", "board")
	}
	a.PostURL = "products/" + url.PathEscape(q.ProductID) + "/tasks/" + url.PathEscape(q.TaskID) + "/answer"
	if q.External {
		a.PostURL = "products/" + url.PathEscape(q.ProductID) + "/inbox/" + url.PathEscape(q.ExternalID) + "/answer"
	}
	if len(pq) > 0 {
		a.PostURL += "?" + pq.Encode()
	}
	return a
}

// The contact registry's back-office routes (D-088). A form post each, behind
// the session; the actor is the session's account, and the log line names its
// alias and the contact's alias, never a destination.

func (s *server) renderBOView(w http.ResponseWriter, r *http.Request, res Result, view string) {
	v := s.applyResult(r, res)
	s.sheet(w, r, v, view, "", "")
	s.rend.RenderWithChrome(w, "bo-main-oob", v)
}

func (s *server) handleContactSave(w http.ResponseWriter, r *http.Request) {
	ss, ok := sessionOf(r)
	if !ok || ss.Email == "" {
		s.requireBO(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	_ = r.ParseForm()
	var on []string
	for _, k := range strings.FieldsFunc(r.PostFormValue("notifyOn"), func(c rune) bool { return c == ',' || c == ' ' }) {
		on = append(on, k)
	}
	res, err := s.mut(r).UpsertContact(r.Context(), Contact{
		ProductID: strings.TrimSpace(r.PostFormValue("productId")), GitHubLogin: r.PostFormValue("githubLogin"),
		DisplayName: r.PostFormValue("displayName"), Channel: "whatsapp",
		Destination: strings.TrimSpace(r.PostFormValue("destination")), NotifyOn: on,
	}, ss.Email)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.renderBOView(w, r, res, "integrations")
}

func (s *server) handleContactDelete(w http.ResponseWriter, r *http.Request) {
	ss, ok := sessionOf(r)
	if !ok || ss.Email == "" {
		s.requireBO(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	_ = r.ParseForm()
	res, err := s.mut(r).DeleteContact(r.Context(), r.PostFormValue("productId"), r.PostFormValue("githubLogin"), ss.Email)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.renderBOView(w, r, res, "integrations")
}
