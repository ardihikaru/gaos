package main

import (
	"net/http"
	"net/url"
	"strings"
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
