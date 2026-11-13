package main

import (
	"context"
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
)

// The generic product seed and HISv3's committed data (seed.go, D-074).

const seedDir = "../deploy/seed/hisv3/"

func readSeedFile(t *testing.T, name string, v any) []byte {
	t.Helper()
	b, err := os.ReadFile(seedDir + name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return b
}

func seedCall(t *testing.T, srv *server, body []byte) (int, seedOut, string) {
	t.Helper()
	rec := call(t, srv, "POST", "/v1/seed/product", tokAdmin, string(body))
	var out seedOut
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out, rec.Body.String()
}

func TestProductSeedIsIdempotentAndMeetsTheFloor(t *testing.T) {
	srv, st := newTestServer(t)
	srv.api = newAPIConfig(tokAdmin, tokQA, tokOut, "")
	ctx := context.Background()
	var product ProductSeed
	raw := readSeedFile(t, "product.json", &product)

	code, first, body := seedCall(t, srv, raw)
	if code != 200 {
		t.Fatalf("the HISv3 seed answered %d: %s", code, body)
	}
	wantTasks := 2 + len(product.Epics) + 1
	if first.Product != "created" || first.Stories != len(product.Modules) || first.Epics != len(product.Epics) || first.Tasks != wantTasks {
		t.Errorf("first seed = %+v, want created, %d stories, %d epics, %d tasks", first, len(product.Modules), len(product.Epics), wantTasks)
	}
	if first.Score < PlanFloor || first.BlueprintVersion != 1 || first.Policies != len(product.Policies) {
		t.Errorf("first seed = %+v, want a readiness of at least %d, Blueprint version 1 and %d policies", first, PlanFloor, len(product.Policies))
	}

	// The gate is NOT cleared and nothing is invented: design and review wait,
	// epic tasks are gate-blocked, the release task needs a human, none is DONE.
	board, _ := st.Board(ctx, first.ProductID)
	byTitle := map[string]BoardCard{}
	for _, c := range board.Cards {
		byTitle[c.Label] = c
		if c.Status == StatusDone || c.Status == StatusRunning || c.Status == StatusDispatched {
			t.Errorf("%q is %s; the seed must not move or run a task", c.Label, c.Status)
		}
	}
	if byTitle["Design: test strategy and catalog"].Status != StatusPending || byTitle["Review the test strategy"].Status != StatusPending {
		t.Errorf("design and review must start PENDING: %+v / %+v", byTitle["Design: test strategy and catalog"], byTitle["Review the test strategy"])
	}
	blocked, planLane := 0, 0
	for _, c := range board.Cards {
		if strings.HasPrefix(c.Label, "Automate: ") {
			if c.Status != StatusBlocked {
				t.Errorf("%q is %s, want BLOCKED by the design gate", c.Label, c.Status)
			}
			blocked++
			if c.Row == PlanLane {
				planLane++
			}
		}
	}
	if blocked != len(product.Epics) {
		t.Errorf("%d epic cards, want %d", blocked, len(product.Epics))
	}
	if planLane == 0 || planLane == blocked {
		t.Errorf("%d of %d epic cards are in the Plan lane: only the epic with no module should be", planLane, blocked)
	}
	var release *TaskRecord
	for _, tk := range st.tasksSnapshot(first.ProductID) {
		if strings.HasPrefix(tk.Title, "Release:") {
			tk := tk
			release = &tk
		}
	}
	if release == nil || !release.Gate || len(release.Deps) != len(product.Epics) {
		t.Errorf("the release task must be gated and wait on every epic: %+v", release)
	}

	// Operator edits survive a re-run, and a re-run adds nothing.
	m, _ := st.Master(ctx, first.ProductID)
	if _, err := st.UpdateBRD(ctx, first.ProductID, BRD{Problem: "An operator rewrote the problem statement on purpose, at length."}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SavePolicy(ctx, first.ProductID, Policy{Key: "coding", Kind: PolicyCoding, Title: "Edited", Body: "edited by an operator"}); err != nil {
		t.Fatal(err)
	}
	_ = m
	code, second, body := seedCall(t, srv, raw)
	if code != 200 {
		t.Fatalf("the second seed answered %d: %s", code, body)
	}
	if second.ProductID != first.ProductID || second.Product != "adopted" || second.Stories != 0 || second.Policies != 0 || second.Tasks != wantTasks {
		t.Errorf("a re-run must change nothing: %+v", second)
	}
	m2, _ := st.Master(ctx, first.ProductID)
	if !strings.HasPrefix(m2.BRD.Problem, "An operator rewrote") {
		t.Errorf("a re-run overwrote an operator's BRD: %q", m2.BRD.Problem)
	}
	for _, p := range m2.Blueprint.Policies {
		if p.Key == "coding" && p.Title != "Edited" {
			t.Errorf("a re-run overwrote an operator's policy: %+v", p)
		}
	}
	if len(m2.Stories) != len(product.Modules) {
		t.Errorf("%d stories after two runs, want %d", len(m2.Stories), len(product.Modules))
	}
	// Sanitised: no story text carries a backtick or an angle bracket.
	for _, sy := range m2.Stories {
		for _, ln := range append([]string{sy.Want, sy.So}, sy.AC...) {
			if strings.ContainsAny(ln, "`<>\n") {
				t.Errorf("story %s carries unsafe text: %q", sy.ID, ln)
			}
		}
	}
	checkProductSeedRefusals(t)
}

func checkProductSeedRefusals(t *testing.T) {
	t.Helper()
	srv, st := newTestServer(t)
	srv.api = newAPIConfig(tokAdmin, tokQA, tokOut, "")
	ctx := context.Background()
	var product ProductSeed
	readSeedFile(t, "product.json", &product)
	mk := func(mut func(p *ProductSeed)) []byte {
		var p ProductSeed
		b, _ := json.Marshal(product) // a deep copy: the mutators append to slices
		_ = json.Unmarshal(b, &p)
		p.Name = "Refusal Probe"
		mut(&p)
		b, _ = json.Marshal(p)
		return b
	}

	// A QA token may not seed.
	if rec := call(t, srv, "POST", "/v1/seed/product", tokQA, string(mk(func(*ProductSeed) {}))); rec.Code != 403 {
		t.Errorf("a QA token seeded: %d", rec.Code)
	}
	// A secret anywhere in the seed refuses it, and writes nothing.
	before, _ := st.Products(ctx)
	for name, mut := range map[string]func(p *ProductSeed){
		"in a story feature": func(p *ProductSeed) {
			p.Modules[0].Features = append(p.Modules[0].Features, "key AKIAABCDEFGHIJKLMNOP here")
		},
		"in a policy body": func(p *ProductSeed) { p.Policies[0].Body += "\nAPI_TOKEN = abcdef123456789\n" },
		"a gaos token":     func(p *ProductSeed) { p.BRD.Rules = append(p.BRD.Rules, tokAdmin) },
	} {
		code, _, body := seedCall(t, srv, mk(mut))
		if code != 400 || !strings.Contains(body, "secret-in-seed") {
			t.Errorf("secret %s: answered %d %s, want 400 secret-in-seed", name, code, body)
		}
	}
	if after, _ := st.Products(ctx); len(after) != len(before) {
		t.Error("a refused seed created a product")
	}
	// Structure.
	for name, mut := range map[string]func(p *ProductSeed){
		"no name":         func(p *ProductSeed) { p.Name = " " },
		"no epics":        func(p *ProductSeed) { p.Epics = nil },
		"unknown module":  func(p *ProductSeed) { p.Epics[0].Module = "D99" },
		"no plan goal":    func(p *ProductSeed) { p.Plan.Goal = "" },
		"a bad epic code": func(p *ProductSeed) { p.Epics[0].Code = "x" },
	} {
		if code, _, body := seedCall(t, srv, mk(mut)); code != 400 {
			t.Errorf("%s: answered %d %s, want 400", name, code, body)
		}
	}
	// A below-the-floor record is refused with what is missing, before any workflow.
	code, _, body := seedCall(t, srv, mk(func(p *ProductSeed) {
		p.Name = "Thin Probe"
		p.BRD, p.Modules, p.Diagrams, p.Decisions, p.Policies = SeedBRD{}, nil, nil, nil, nil
		for i := range p.Epics {
			p.Epics[i].Module = ""
		}
	}))
	if code != 422 || !strings.Contains(body, "not-ready") {
		t.Errorf("a thin seed answered %d %s, want 422 not-ready", code, body)
	}
	if ps, _ := st.Products(ctx); len(ps) == 0 {
		t.Fatal("no products")
	} else {
		for _, p := range ps {
			if p.Name == "Thin Probe" {
				if b, _ := st.Board(ctx, p.ID); len(b.Cards) != 0 {
					t.Error("a below-the-floor seed planned a workflow anyway")
				}
			}
		}
	}

	// Never adopt silently: a same-named product the seed did not make is a 409.
	if _, err := st.CreateProduct(ctx, ProductDraft{Name: "Squatter"}); err != nil {
		t.Fatal(err)
	}
	ms, _ := st.Masters(ctx)
	sq := ms[len(ms)-1].ID
	if _, err := st.AddStory(ctx, sq, Story{Role: "x", Want: "squat"}); err != nil {
		t.Fatal(err)
	}
	if code, _, body := seedCall(t, srv, mk(func(p *ProductSeed) { p.Name = "squatter" })); code != 409 || !strings.Contains(body, "product-exists-unseeded") {
		t.Errorf("a same-named non-pristine product: answered %d %s, want 409 product-exists-unseeded", code, body)
	}
	// ... and two products of one name is never resolved by guessing.
	_, _ = st.CreateProduct(ctx, ProductDraft{Name: "Twin"})
	_, _ = st.CreateProduct(ctx, ProductDraft{Name: "twin"})
	if code, _, _ := seedCall(t, srv, mk(func(p *ProductSeed) { p.Name = "Twin" })); code != 409 {
		t.Errorf("two products of one name: answered %d, want 409", code)
	}

	// The seed touches the floor, so replay refuses it.
	if _, err := st.Replay(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if code, _, body := seedCall(t, srv, mk(func(p *ProductSeed) { p.Name = "Replay Probe" })); code != 409 || !strings.Contains(body, "replay") {
		t.Errorf("a seed during replay answered %d %s, want 409 replay", code, body)
	}
	_, _ = st.Replay(ctx, -1)
}

func TestTheHISv3SeedIsWellFormed(t *testing.T) {
	var product ProductSeed
	var casesDoc struct {
		Source struct {
			Commit string `json:"commit"`
		} `json:"source"`
		Cases []QaCase `json:"cases"`
	}
	praw := readSeedFile(t, "product.json", &product)
	craw := readSeedFile(t, "cases.json", &casesDoc)

	// The counts are the source repo's at the commit the files record.
	feats := 0
	moduleKeys := map[string]bool{}
	for _, m := range product.Modules {
		feats += len(m.Features)
		moduleKeys[m.Key] = true
		if m.Pri != "must" && m.Pri != "should" {
			t.Errorf("module %s has pri %q", m.Key, m.Pri)
		}
	}
	if len(product.Modules) != 39 || feats != 444 || len(product.Epics) != 47 || len(casesDoc.Cases) != 1700 {
		t.Errorf("seed has %d modules, %d features, %d epics, %d cases; want 39, 444, 47, 1700", len(product.Modules), feats, len(product.Epics), len(casesDoc.Cases))
	}
	sha := regexp.MustCompile(`^[0-9a-f]{40}$`)
	if !sha.MatchString(casesDoc.Source.Commit) {
		t.Errorf("cases.json names no source commit: %q", casesDoc.Source.Commit)
	}
	for _, k := range product.Sources {
		if !sha.MatchString(k.Ref) {
			t.Errorf("knowledge source %s is pinned to %q, want a 40-hex commit", k.URL, k.Ref)
		}
		if validateSource(KnowledgeSource{URL: k.URL, Ref: k.Ref, Purpose: k.Purpose}) != nil {
			t.Errorf("knowledge source %s is not valid", k.URL)
		}
	}
	for _, p := range product.Policies {
		if err := validatePolicy(Policy{Key: p.Key, Kind: p.Kind, Title: p.Title, Body: p.Body}); err != nil {
			t.Errorf("policy %s: %v", p.Key, err)
		}
	}
	epics := map[string]bool{}
	noModule := 0
	for _, e := range product.Epics {
		epics[e.Code] = true
		if why := validateEpic(e); why != "" {
			t.Errorf("epic %s: %s", e.Code, why)
		}
		if e.Module == "" {
			noModule++
		} else if !moduleKeys[e.Module] {
			t.Errorf("epic %s names module %s, which the seed does not define", e.Code, e.Module)
		}
	}
	if noModule != 1 {
		t.Errorf("%d epics have no module, want exactly 1 (the report builder: its stories name no feature)", noModule)
	}
	seen := map[string]bool{}
	implemented, planned := 0, 0
	for _, c := range casesDoc.Cases {
		if why := validatePlanning(c); why != "" {
			t.Errorf("%s: %s", c.CaseID, why)
		}
		if seen[c.CaseID] {
			t.Errorf("duplicate case id %s", c.CaseID)
		}
		seen[c.CaseID] = true
		if !epics[c.Epic] {
			t.Errorf("%s names epic %s, which the seed does not define", c.CaseID, c.Epic)
		}
		if c.Module != "" && !moduleKeys[c.Module] {
			t.Errorf("%s names module %s, which the seed does not define", c.CaseID, c.Module)
		}
		if c.Implemented {
			implemented++
			if c.Decision != DecisionPlanned || c.Spec == "" || validateSpec(c.Spec) != "" {
				t.Errorf("%s is implemented but is %s with spec %q", c.CaseID, c.Decision, c.Spec)
			}
		} else if c.Spec != "" {
			t.Errorf("%s is not implemented but carries spec %q", c.CaseID, c.Spec)
		}
		if c.Decision == DecisionPlanned {
			planned++
		}
		if strings.ContainsAny(c.Scenario, "`<>\n") {
			t.Errorf("%s: unsafe scenario text", c.CaseID)
		}
	}
	if implemented != 494 || planned != 1146 {
		t.Errorf("%d implemented and %d planned; want 494 and 1146", implemented, planned)
	}
	// The egress detector finds nothing in either file, read as a whole and per string.
	for name, raw := range map[string][]byte{"product.json": praw, "cases.json": craw} {
		var tree any
		_ = json.Unmarshal(raw, &tree)
		if hit := secretInStrings(tree); hit != "" {
			t.Errorf("%s trips the secret detector %s", name, hit)
		}
	}
}
