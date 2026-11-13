package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// The generic product seed (D-074, design §4.9).
//
// POST /v1/seed/product takes a product SEED DOCUMENT and builds the product
// from it through the store calls an operator would make by hand: the product,
// its BRD, a story per module, diagrams, decisions, Blueprint policies and
// sources, a published Blueprint, the QA epics and a gated workflow. GAOS ships
// no product's content; sample-product's is deploy/seed/sample-product/product.json.
//
// IDEMPOTENT: each step is skipped when it is already true, so a re-run never
// overwrites what an operator edited since. NOT A SHORTCUT AROUND THE GATE: the
// workflow it plans carries the design gate (BR-46), and the seed does not
// clear it -- the design and review tasks start PENDING and the epic tasks
// gate-BLOCKED, exactly as a plan made in the wizard would. No task is moved,
// no run, artifact or DONE is invented.

type SeedModule struct {
	Key      string   `json:"key"`
	Title    string   `json:"title"`
	Pri      string   `json:"pri"`
	Features []string `json:"features"`
}

type SeedDiagram struct {
	Kind  string `json:"kind"`
	Title string `json:"title"`
	Src   string `json:"src"`
}

type SeedDecision struct {
	ID         string  `json:"id"`
	Title      string  `json:"title"`
	Confidence float64 `json:"confidence"`
}

type SeedPolicy struct {
	Key   string `json:"key"`
	Kind  string `json:"kind"`
	Title string `json:"title"`
	Body  string `json:"body"`
	Order int    `json:"order"`
}

type SeedSource struct {
	URL     string `json:"url"`
	Ref     string `json:"ref"`
	Purpose string `json:"purpose"`
	Note    string `json:"note"`
}

type SeedBRD struct {
	Problem    string   `json:"problem"`
	Goals      []string `json:"goals"`
	Scope      []string `json:"scope"`
	OutOfScope []string `json:"outOfScope"`
	Rules      []string `json:"rules"`
	Metrics    []string `json:"metrics"`
}

type ProductSeed struct {
	Source    json.RawMessage `json:"source"` // the generator's header; informational
	Name      string          `json:"name"`
	Owner     string          `json:"owner"`
	Repo      string          `json:"repo"`
	Goal      string          `json:"goal"`
	Priority  string          `json:"priority"`
	BRD       SeedBRD         `json:"brd"`
	Modules   []SeedModule    `json:"modules"`
	Diagrams  []SeedDiagram   `json:"diagrams"`
	Decisions []SeedDecision  `json:"decisions"`
	Policies  []SeedPolicy    `json:"policies"`
	Sources   []SeedSource    `json:"sources"`
	Epics     []QaEpic        `json:"epics"`
	Plan      struct {
		Goal     string `json:"goal"`
		Priority string `json:"priority"`
	} `json:"plan"`
}

// cleanSeedText makes one line of seed text safe to put in front of an agent:
// story, AC, feature and case text reaches every turn through 20-stories.md.
// Backticks, angle brackets and line breaks go, and a leading '#' (a heading)
// is stripped. Policy bodies and diagram sources are markdown or DSL by nature
// and are screened, not rewritten.
func cleanSeedText(s string) string {
	r := strings.NewReplacer("`", "'", "<", "", ">", "", "\r", " ", "\n", " ", "\t", " ")
	s = strings.Join(strings.Fields(r.Replace(s)), " ")
	return strings.TrimSpace(strings.TrimLeft(s, "# "))
}

func cleanAll(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = cleanSeedText(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// secretIn names the first secret detector the text trips, or "".
func secretIn(text string) string {
	for _, d := range detectors {
		if d.class == ClassSecrets || d.class == ClassCredentials {
			if d.re.MatchString(text) {
				return d.name
			}
		}
	}
	return ""
}

// secretInStrings walks a decoded JSON value and names the first detector any
// string in it trips.
func secretInStrings(v any) string {
	switch x := v.(type) {
	case string:
		return secretIn(x)
	case []any:
		for _, e := range x {
			if hit := secretInStrings(e); hit != "" {
				return hit
			}
		}
	case map[string]any:
		for _, e := range x {
			if hit := secretInStrings(e); hit != "" {
				return hit
			}
		}
	}
	return ""
}

func brdEmpty(b BRD) bool {
	return b.Problem == "" && len(b.Goals)+len(b.Scope)+len(b.OutOfScope)+len(b.Rules)+len(b.Metrics) == 0
}

type seedOut struct {
	ProductID        string `json:"productId"`
	Product          string `json:"product"` // created | adopted
	Stories          int    `json:"stories"`
	Epics            int    `json:"epics"`
	Policies         int    `json:"policies"`
	BlueprintVersion int    `json:"blueprintVersion"`
	Tasks            int    `json:"tasks"`
	Score            int    `json:"readinessScore"`
	Band             string `json:"readinessBand"`
}

type seedErr struct {
	status int
	code   string
	reason string
}

// seedErrFrom maps a store error to the seed's answer. A refusal while parked
// in replay is a 409; any other refusal is a 422 carrying its sentence.
func seedErrFrom(err error) *seedErr {
	switch {
	case errors.Is(err, ErrNotFound):
		return &seedErr{http.StatusNotFound, "not-found", "no such product"}
	case err != nil && strings.Contains(err.Error(), replayRefusal):
		return &seedErr{http.StatusConflict, "replay", "the floor is in replay; return to live first"}
	case errors.Is(err, ErrRejected):
		return &seedErr{http.StatusUnprocessableEntity, "rejected", err.Error()}
	}
	return &seedErr{http.StatusInternalServerError, "internal", "the store could not complete the seed"}
}

func (s *server) apiSeedProduct(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.apiGate(w, r, scopeAdmin)
	if !ok {
		return
	}
	release, ok := s.beginWrite(w, scope)
	if !ok {
		return
	}
	defer release()
	var seed ProductSeed
	if !apiDecode(w, r, &seed) {
		return
	}
	// The seed touches the floor (CreateWorkflow), which replay must refuse.
	if f, err := s.store.Snapshot(r.Context()); err == nil && f.Stats.Replay {
		apiErr(w, http.StatusConflict, "replay", "the floor is in replay; return to live first")
		return
	}
	out, se := s.runSeed(r, seed)
	if se != nil {
		apiErr(w, se.status, se.code, se.reason)
		return
	}
	apiJSON(w, http.StatusOK, out)
}

// runSeed does the work. It answers (out, 0, ...) on success, or the status and
// error code to send.
func (s *server) runSeed(r *http.Request, seed ProductSeed) (seedOut, *seedErr) {
	ctx := r.Context()
	st := s.mut(r)
	bad := func(code, why string) (seedOut, *seedErr) {
		return seedOut{}, &seedErr{http.StatusBadRequest, code, why}
	}

	// The server-side screen: not the generator's convenience but the control.
	// Every string value is screened ON ITS OWN: some detectors are anchored to a
	// line start, which a marshalled blob (newlines escaped) would never present.
	raw, _ := json.Marshal(seed)
	var tree any
	_ = json.Unmarshal(raw, &tree)
	if hit := secretInStrings(tree); hit != "" {
		return bad("secret-in-seed", "the seed matches the secret detector "+hit+"; nothing was written")
	}
	if strings.TrimSpace(seed.Name) == "" || strings.TrimSpace(seed.Plan.Goal) == "" {
		return bad("bad-seed", "a seed needs a name and a plan goal")
	}
	if len(seed.Epics) == 0 {
		return bad("bad-seed", "a seed needs at least one epic to plan")
	}
	moduleKeys := map[string]bool{}
	for _, m := range seed.Modules {
		if validateModule(QaModule{Key: m.Key, Title: m.Title}) != "" {
			return bad("bad-seed", "module "+m.Key+" is not a valid module")
		}
		moduleKeys[m.Key] = true
	}
	for _, e := range seed.Epics {
		if why := validateEpic(e); why != "" {
			return bad("bad-seed", "epic "+e.Code+": "+why)
		}
		if e.Module != "" && !moduleKeys[e.Module] {
			return bad("bad-seed", "epic "+e.Code+" names module "+e.Module+", which the seed does not define")
		}
	}

	// 1. the product -- never adopted silently.
	out := seedOut{}
	masters, _ := s.store.Masters(ctx)
	products, _ := s.store.Products(ctx)
	var matches []string
	for _, p := range products {
		if strings.EqualFold(strings.TrimSpace(p.Name), strings.TrimSpace(seed.Name)) {
			matches = append(matches, p.ID)
		}
	}
	var pid string
	switch len(matches) {
	case 0:
		res, err := st.CreateProduct(ctx, ProductDraft{Name: seed.Name, Owner: seed.Owner, Repo: seed.Repo,
			Goal: cleanSeedText(seed.Goal), Priority: seed.Priority})
		if err != nil {
			return seedOut{}, seedErrFrom(err)
		}
		pid, out.Product = res.Master.ID, "created"
	case 1:
		pid = matches[0]
		for _, m := range masters {
			if m.ID != pid {
				continue
			}
			board, _ := s.store.Board(ctx, pid)
			pristine := len(m.Stories) == 0 && len(m.Blueprint.Policies) == 0 && len(m.Blueprint.Sources) == 0 &&
				brdEmpty(m.BRD) && len(board.Cards) == 0
			if len(m.Qa.Modules) == 0 && !pristine {
				return seedOut{}, &seedErr{http.StatusConflict, "product-exists-unseeded",
					"a product of that name exists and was not made by this seed; rename it or remove it first"}
			}
		}
		out.Product = "adopted"
	default:
		return seedOut{}, &seedErr{http.StatusConflict, "product-exists-unseeded", "more than one product has that name; refusing to choose"}
	}
	out.ProductID = pid
	cur := func() Master {
		m, _ := s.store.Master(ctx, pid)
		return m
	}

	// 2. the BRD, only when the stored one is entirely empty.
	if m := cur(); brdEmpty(m.BRD) {
		if _, err := st.UpdateBRD(ctx, pid, BRD{
			Problem: cleanSeedText(seed.BRD.Problem), Goals: cleanAll(seed.BRD.Goals), Scope: cleanAll(seed.BRD.Scope),
			OutOfScope: cleanAll(seed.BRD.OutOfScope), Rules: cleanAll(seed.BRD.Rules), Metrics: cleanAll(seed.BRD.Metrics),
		}); err != nil {
			return seedOut{}, seedErrFrom(err)
		}
	}

	// 3. a story per module. A story is recognised by its Want, so a run that
	// died between AddStory and recording the module reuses the story.
	storyOf := map[string]string{}
	for _, mm := range cur().Qa.Modules {
		storyOf[mm.Key] = mm.Story
	}
	byWant := map[string]string{}
	for _, sy := range cur().Stories {
		byWant[sy.Want] = sy.ID
	}
	var mods []QaModule
	for _, m := range seed.Modules {
		title := cleanSeedText(m.Title)
		want := cleanSeedText(fmt.Sprintf("automated regression coverage for %s — %s", m.Key, title))
		id := storyOf[m.Key]
		if id == "" {
			id = byWant[want]
		}
		if id == "" {
			ac := cleanAll(m.Features)
			pri := m.Pri
			if pri != "must" && pri != "should" && pri != "could" {
				pri = "should"
			}
			if pri == "must" && len(ac) < 2 {
				// Readiness gives its points only to a must story with two
				// criteria (model.go), so a thin module does not claim "must".
				pri = "should"
			}
			res, err := st.AddStory(ctx, pid, Story{Pri: pri, Role: "QA engineer", Want: want,
				So: "a release cannot break " + title + " unnoticed", AC: ac})
			if err != nil {
				return seedOut{}, seedErrFrom(err)
			}
			id = res.Master.Stories[len(res.Master.Stories)-1].ID
			out.Stories++
		}
		mods = append(mods, QaModule{Key: m.Key, Title: title, Story: id})
		storyOf[m.Key] = id
	}

	// 4. diagrams and decisions, skipped by title.
	haveDiagram := map[string]bool{}
	for _, d := range cur().Diagrams {
		haveDiagram[d.Title] = true
	}
	for _, d := range seed.Diagrams {
		if t := cleanSeedText(d.Title); !haveDiagram[t] {
			if _, err := st.AddDiagram(ctx, pid, Diagram{Kind: d.Kind, Title: t, Src: d.Src}); err != nil {
				return seedOut{}, seedErrFrom(err)
			}
		}
	}
	haveDecision := map[string]bool{}
	for _, d := range cur().Decisions {
		haveDecision[d.Title] = true
	}
	for _, d := range seed.Decisions {
		if t := cleanSeedText(d.Title); !haveDecision[t] {
			if _, err := st.AddDecision(ctx, pid, Decision{ID: d.ID, Title: t, Confidence: d.Confidence}); err != nil {
				return seedOut{}, seedErrFrom(err)
			}
		}
	}

	// 5. the Blueprint's policies (only a key not yet present) and sources.
	havePolicy := map[string]bool{}
	for _, p := range cur().Blueprint.Policies {
		havePolicy[p.Key] = true
	}
	for _, p := range seed.Policies {
		if havePolicy[p.Key] {
			continue
		}
		if _, err := st.SavePolicy(ctx, pid, Policy{Key: p.Key, Kind: p.Kind, Title: cleanSeedText(p.Title), Body: p.Body, Order: p.Order}); err != nil {
			return seedOut{}, seedErrFrom(err)
		}
		out.Policies++
	}
	for _, k := range seed.Sources {
		if _, err := st.AddKnowledgeSource(ctx, pid, KnowledgeSource{URL: k.URL, Ref: k.Ref, Purpose: k.Purpose, Note: cleanSeedText(k.Note)}); err != nil && !errors.Is(err, ErrRejected) {
			return seedOut{}, seedErrFrom(err)
		}
	}

	// 6. publish the Blueprint when none exists yet.
	if len(cur().Blueprint.Versions) == 0 {
		if _, err := st.PublishBlueprint(ctx, pid); err != nil {
			return seedOut{}, seedErrFrom(err)
		}
	}
	if v, ok := cur().Blueprint.Latest(); ok {
		out.BlueprintVersion = v.N
	}

	// 7. BR-07: CreateWorkflow does not check readiness itself.
	m := cur()
	out.Score, out.Band = m.ReadinessScore(), ReadinessBand(m.ReadinessScore())
	if out.Score < PlanFloor {
		return seedOut{}, &seedErr{http.StatusUnprocessableEntity, "not-ready",
			fmt.Sprintf("readiness %d is below the planning floor of %d: %s", out.Score, PlanFloor, strings.Join(m.ReadinessMissing(), " "))}
	}

	// 8. the modules and epics, with the story ids the stories now have.
	epics := append([]QaEpic(nil), seed.Epics...)
	sort.Slice(epics, func(i, j int) bool { return epics[i].Code < epics[j].Code })
	for i := range epics {
		epics[i].Title = cleanSeedText(epics[i].Title)
		epics[i].Story = storyOf[epics[i].Module]
	}
	if _, _, err := st.QaImport(ctx, pid, QaImportIn{Modules: mods, Epics: epics}); err != nil {
		return seedOut{}, seedErrFrom(err)
	}
	out.Epics = len(epics)

	// 9. the workflow, only when the product has no tasks.
	if board, _ := s.store.Board(ctx, pid); len(board.Cards) == 0 {
		plan, picked := seedPlan(epics)
		d := WorkflowDraft{ProductID: pid, Goal: cleanSeedText(seed.Plan.Goal), Priority: seed.Plan.Priority, Stories: picked}
		if d.Priority == "" {
			d.Priority = "high"
		}
		if _, err := st.CreateWorkflow(ctx, d, plan); err != nil {
			return seedOut{}, seedErrFrom(err)
		}
	}
	// 10. each epic learns its card, so the board can show its live counts.
	board, _ := s.store.Board(ctx, pid)
	cardOf := map[string]string{}
	for _, c := range board.Cards {
		cardOf[c.Label] = c.ID
	}
	for i := range epics {
		epics[i].Task = cardOf["Automate: "+epics[i].Title]
	}
	if _, _, err := st.QaImport(ctx, pid, QaImportIn{Epics: epics}); err != nil {
		return seedOut{}, seedErrFrom(err)
	}
	out.Tasks = len(board.Cards)
	return out, nil
}

// seedPlan is the gated plan for a set of QA epics: a design, its review, one
// implement task per epic that waits on the review, and a release task that
// waits on every epic and is released by a human. The ids are plan-local.
func seedPlan(epics []QaEpic) ([]PlanTask, []string) {
	plan := []PlanTask{
		{ID: "t-1", Title: "Design: test strategy and catalog", Role: "architect", Model: roles["architect"].Model, Kind: TaskKindDesign},
		{ID: "t-2", Title: "Review the test strategy", Role: "qa", Model: "claude-opus-5", Kind: TaskKindReview, Deps: []string{"t-1"}},
	}
	var epicTasks, picked []string
	seen := map[string]bool{}
	for _, e := range epics {
		id := fmt.Sprintf("t-%d", len(plan)+1)
		plan = append(plan, PlanTask{ID: id, Title: "Automate: " + e.Title, Role: "qa", Story: e.Story,
			Model: roles["qa"].Model, Kind: TaskKindImplement, Deps: []string{"t-2"}})
		epicTasks = append(epicTasks, id)
		if e.Story != "" && !seen[e.Story] {
			seen[e.Story] = true
			picked = append(picked, e.Story)
		}
	}
	plan = append(plan, PlanTask{ID: fmt.Sprintf("t-%d", len(plan)+1), Title: "Release: staging regression run and report",
		Role: "qa", Model: roles["qa"].Model, Kind: TaskKindImplement, Gate: true, Deps: epicTasks})
	return plan, picked
}
