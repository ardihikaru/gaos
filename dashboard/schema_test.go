package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Guards on schema/*.sql -- the DDL extracted from
// artifacts/v1/gaos-arcadedb-model.html and reconciled against ArcadeDB 26.7.2
// (schema/RECONCILIATION.md).
//
// None of these tests need a database. They are cheap invariants over the text,
// and each one guards something that was actually wrong at some point rather
// than something that merely could be:
//
//   - the type counts drifting from the three numbers the model document states
//     in its own lede, which is how a schema and its documentation separate;
//   - an edge type losing the uniqueness index that D-045 substituted for the
//     UNIQUE keyword, which would be silent -- duplicate edges do not raise,
//     they accumulate;
//   - a statement without IF NOT EXISTS, which breaks the idempotence
//     deploy/schema/apply.sh promises and this project relies on.
//
// The live behaviour they stand in for was measured on 2026-08-16 and is
// written up in schema/RECONCILIATION.md, including how to reproduce it.

const schemaDir = "schema"

// The counts are the model document's own: its §01 lede says "25 vertex types,
// 3 document types, 32 edge types", and §15 (D-058, 11-blueprint.sql) adds
// Policy, KnowledgeSource and BlueprintVersion and five edges -- 28 and 37.
// D-072 (16-qa-catalog.sql) adds QaCase and HAS_QA_CASE -- 29 and 38.
// D-088 adds Contact (30) and D-091 (17-defects.sql) adds Defect and FixPR (32).
// D-092 (18-pull-requests.sql) adds PullRequest (33).
// GaosVertex and GaosEdge are supertypes that everything else EXTENDS and are
// deliberately outside those numbers.
const (
	wantVertexTypes   = 33
	wantDocumentTypes = 3
	wantEdgeTypes     = 38
)

var (
	reVertex = regexp.MustCompile(`(?m)^CREATE VERTEX TYPE\s+(\w+)`)
	reDoc    = regexp.MustCompile(`(?m)^CREATE DOCUMENT TYPE\s+(\w+)`)
	reEdge   = regexp.MustCompile(`(?m)^CREATE EDGE TYPE\s+(\w+)`)
	reAnyTyp = regexp.MustCompile(`(?m)^CREATE (?:VERTEX|DOCUMENT|EDGE) TYPE\s+\w+[^;]*;`)
	reIndex  = regexp.MustCompile("(?m)^CREATE INDEX[^;]*?ON\\s+(\\w+)\\s*\\(([^)]*)\\)\\s*(\\w+)\\s*;")
)

// schemaSQL returns every .sql file's contents with comments stripped, so a
// name that appears only in a comment cannot satisfy an assertion. That trap is
// real here: the files are heavily commented, and several comments quote the
// very statements they explain.
func schemaSQL(t *testing.T) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(schemaDir)
	if err != nil {
		t.Fatalf("read %s: %v", schemaDir, err)
	}
	out := map[string]string{}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(schemaDir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		var kept []string
		for _, line := range strings.Split(string(b), "\n") {
			if i := strings.Index(line, "--"); i >= 0 {
				line = line[:i]
			}
			kept = append(kept, line)
		}
		out[e.Name()] = strings.Join(kept, "\n")
	}
	if len(out) == 0 {
		t.Fatalf("no .sql files in %s; the schema is the thing under test", schemaDir)
	}
	return out
}

func namesOf(re *regexp.Regexp, files map[string]string) []string {
	var got []string
	for _, sql := range files {
		for _, m := range re.FindAllStringSubmatch(sql, -1) {
			got = append(got, m[1])
		}
	}
	sort.Strings(got)
	return got
}

// The three numbers the model document states about itself. A schema that
// declares 24 vertex types while the document says 25 is a documentation bug or
// a missing type, and there is no way to tell which from the outside -- so fail
// and make somebody look.
func TestSchemaDeclaresTheDocumentedTypeCounts(t *testing.T) {
	files := schemaSQL(t)

	// The supertypes are the spine every concrete type extends. They are not
	// part of the catalog's counts and their absence would break everything.
	for _, super := range []string{"GaosVertex", "GaosEdge"} {
		found := false
		for _, sql := range files {
			if strings.Contains(sql, "TYPE "+super+" ") {
				found = true
			}
		}
		if !found {
			t.Fatalf("supertype %s is not declared; every concrete type EXTENDS it", super)
		}
	}

	vertices := namesOf(reVertex, files)
	docs := namesOf(reDoc, files)
	edges := namesOf(reEdge, files)

	concrete := func(names []string, super string) []string {
		var out []string
		for _, n := range names {
			if n != super {
				out = append(out, n)
			}
		}
		return out
	}
	vertices = concrete(vertices, "GaosVertex")
	edges = concrete(edges, "GaosEdge")

	for _, c := range []struct {
		kind string
		got  []string
		want int
	}{
		{"vertex", vertices, wantVertexTypes},
		{"document", docs, wantDocumentTypes},
		{"edge", edges, wantEdgeTypes},
	} {
		if len(c.got) != c.want {
			t.Errorf("%s types: got %d, want %d (the model document's own count) -- %v",
				c.kind, len(c.got), c.want, c.got)
		}
	}

	// A duplicated declaration applies fine and then confuses every reader
	// counting by hand, which is what the numbers above are for.
	seen := map[string]bool{}
	for _, n := range append(append(append([]string{}, vertices...), docs...), edges...) {
		if seen[n] {
			t.Errorf("type %s is declared more than once", n)
		}
		seen[n] = true
	}
}

// D-045: ArcadeDB 26.7.2 rejects `CREATE EDGE TYPE ... UNIQUE`, so the
// "at most one edge per ordered pair" constraint that the model's edge catalog
// marks on every row is carried by a unique index on the endpoints instead.
//
// This is the reconciliation most likely to rot, and rot quietly: adding an
// edge type and forgetting its index does not fail anything. Duplicate edges
// simply start accumulating, and the first symptom is a traversal returning a
// row twice, a long way from here.
func TestEveryEdgeTypeCarriesItsUniquenessIndex(t *testing.T) {
	files := schemaSQL(t)

	edges := map[string]bool{}
	for _, n := range namesOf(reEdge, files) {
		if n != "GaosEdge" {
			edges[n] = false
		}
	}

	for _, sql := range files {
		for _, m := range reIndex.FindAllStringSubmatch(sql, -1) {
			typ, props, kind := m[1], m[2], strings.ToUpper(strings.TrimSpace(m[3]))
			if _, isEdge := edges[typ]; !isEdge {
				continue
			}
			norm := strings.NewReplacer("`", "", " ", "").Replace(props)
			if norm == "@out,@in" && kind == "UNIQUE" {
				edges[typ] = true
			}
		}
	}

	var missing []string
	for name, ok := range edges {
		if !ok {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("%d edge types have no UNIQUE index on (`@out`, `@in`), so duplicate "+
			"edges between the same pair will accumulate silently: %v -- see "+
			"schema/RECONCILIATION.md deviation 1", len(missing), missing)
	}

	// The keyword form must not come back: it is a hard parse error on the
	// pinned version and takes 50 statements down with it.
	for name, sql := range files {
		for _, line := range strings.Split(sql, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "CREATE EDGE TYPE") &&
				strings.Contains(strings.ToUpper(line), "UNIQUE") {
				t.Errorf("%s: `CREATE EDGE TYPE ... UNIQUE` is not valid on ArcadeDB 26.7.2 "+
					"and fails the whole file: %s", name, strings.TrimSpace(line))
			}
		}
	}
}

// deploy/schema/apply.sh promises a second run is a no-op, and that promise is
// what makes the script safe to point at dev or prod. It rests entirely on
// every declaration carrying IF NOT EXISTS.
func TestEverySchemaDeclarationIsIdempotent(t *testing.T) {
	files := schemaSQL(t)

	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)

	total := 0
	for _, name := range names {
		for _, stmt := range reAnyTyp.FindAllString(files[name], -1) {
			total++
			if !strings.Contains(strings.ToUpper(stmt), "IF NOT EXISTS") {
				t.Errorf("%s: declaration without IF NOT EXISTS breaks re-application: %s",
					name, strings.Join(strings.Fields(stmt), " "))
			}
		}
	}
	if total == 0 {
		t.Fatal("no type declarations found; the regexp or the schema moved")
	}

	// 10-storage.sql is ALTER-only and therefore has no declarations to carry
	// the clause. It is idempotent for a different reason -- setting the same
	// value twice is the same assignment twice -- and asserting its emptiness
	// here keeps that distinction from being forgotten and "fixed".
	if got := len(reAnyTyp.FindAllString(files["10-storage.sql"], -1)); got != 0 {
		t.Errorf("10-storage.sql declares %d types; it is meant to be ALTER-only, and "+
			"an ALTER carries no IF NOT EXISTS to protect it", got)
	}
}

// A guard on the guards: if the schema directory is renamed or emptied, the
// three tests above would pass vacuously on an empty map without this.
func TestSchemaDirectoryHasEveryNumberedFile(t *testing.T) {
	files := schemaSQL(t)
	for i := 1; i <= 10; i++ {
		prefix := fmt.Sprintf("%02d-", i)
		found := false
		for name := range files {
			if strings.HasPrefix(name, prefix) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no schema file numbered %s; apply.sh runs them in filename order "+
				"and a gap means a dependency was skipped", prefix)
		}
	}
}

// TestTheTaskStatusVocabularyMatchesTheDDL holds taskstate.go and
// 04-execution.sql to each other, in both directions.
//
// Both directions is the whole point, and it is what the DEFERRED test below
// does not do. That one asks whether one name is present, which catches a DDL
// missing a state Go can reach -- but not a Go constant the DDL would refuse,
// which fails at commit time in production and in no test at all. A status
// vocabulary is a closed set on both sides or it is two vocabularies.
func TestTheTaskStatusVocabularyMatchesTheDDL(t *testing.T) {
	sql := schemaSQL(t)["04-execution.sql"]
	if sql == "" {
		t.Fatal("04-execution.sql is empty or missing")
	}
	m := regexp.MustCompile(`Task\.status[^;]*regexp '([^']+)'`).FindStringSubmatch(sql)
	if m == nil {
		t.Fatal("Task.status declares no regexp; its vocabulary is unconstrained")
	}

	inDDL := map[string]bool{}
	for _, v := range strings.Split(m[1], "|") {
		inDDL[v] = true
	}
	inGo := map[string]bool{}
	for _, v := range taskStatuses() {
		inGo[v] = true
	}

	for v := range inGo {
		if !inDDL[v] {
			t.Errorf("Task.status permits %q; Go declares %q — a status the DDL refuses is a "+
				"row the scheduler cannot write, and it fails at commit rather than here", m[1], v)
		}
	}
	for v := range inDDL {
		if !inGo[v] {
			t.Errorf("the DDL permits %q and taskstate.go declares no transitions for it; a "+
				"status nothing can produce or leave is a state the machine cannot reach", v)
		}
	}
}

// TestTheDeferredStateAndItsCompanionsLandedTogether is T-015 held to its own
// terms. D-037 adds three event names, one task state and two TaskRun
// properties, and the BRD is explicit that "none of it should be done
// piecemeal": the catalog is closed, so a name without the state it describes,
// or a state the DDL will refuse to store, is worse than neither.
//
// So this asserts the whole set in one test on purpose. Splitting it into five
// would let four pass while one was missing, which is precisely the partial
// landing the requirement forbids.
func TestTheDeferredStateAndItsCompanionsLandedTogether(t *testing.T) {
	sql := schemaSQL(t)["04-execution.sql"]
	if sql == "" {
		t.Fatal("04-execution.sql is empty or missing")
	}

	// The task state. DEFERRED has to be inside Task.status's vocabulary, or
	// the scheduler that produces it writes a row the database refuses --
	// RECONCILIATION.md measured that regexp as a real boundary.
	status := regexp.MustCompile(`Task\.status[^;]*regexp '([^']+)'`).FindStringSubmatch(sql)
	if status == nil {
		t.Fatal("Task.status declares no regexp; its vocabulary is unconstrained")
	}
	found := false
	for _, v := range strings.Split(status[1], "|") {
		if v == "DEFERRED" {
			found = true
		}
	}
	if !found {
		t.Errorf("Task.status permits %q, which has no DEFERRED -- model.go declares "+
			"StateDeferred and state.go declares its transitions, so a task that "+
			"reaches it could not be written down", status[1])
	}

	// The two properties. invocation is D-13 and BR-33: the mode is recorded so
	// the prohibition on branching can be checked rather than trusted. costBasis
	// is BR-31 and BR-37: a cost figure without its basis is a number nobody can
	// audit, and an unpriceable one is reported unavailable, never zero.
	for _, want := range []struct{ prop, vocab string }{
		{"TaskRun.invocation", "cli|api"},
		{"TaskRun.costBasis", "measured|estimated|unavailable"},
	} {
		re := regexp.MustCompile(regexp.QuoteMeta(want.prop) + `[^;]*regexp '([^']+)'`)
		m := re.FindStringSubmatch(sql)
		if m == nil {
			t.Errorf("%s is not declared with a regexp; D-037 lands it with its vocabulary "+
				"or not at all", want.prop)
			continue
		}
		if m[1] != want.vocab {
			t.Errorf("%s permits %q, want %q", want.prop, m[1], want.vocab)
		}
	}
}

// A least-privilege user cannot create a database -- only root may run server
// commands -- and the deployment on clinodb is built that way on purpose:
// gaos_dev and gaos_prod each hold rights on their own database alone. The
// first version of ensureDatabase treated that refusal as fatal, and the dev
// dashboard crash-looped on its first boot against the shared server.
func TestALeastPrivilegeUserBootsAgainstADatabaseItCannotCreate(t *testing.T) {
	readable := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/server":
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":"Only root user is authorized to execute server commands (create database gaos_dev)"}`))
		case strings.HasPrefix(r.URL.Path, "/api/v1/query/gaos_dev") && readable:
			w.Write([]byte(`{"result":[{"one":1}]}`))
		default:
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":"Forbidden"}`))
		}
	}))
	defer srv.Close()
	a := &Arcade{URL: srv.URL, DB: "gaos_dev", User: "gaos_dev", Password: "x", client: srv.Client(), stmtClient: srv.Client()}
	if err := a.ensureDatabase(t.Context()); err != nil {
		t.Fatalf("a user who may read gaos_dev but not create it was refused at boot: %v", err)
	}
	readable = false
	if err := a.ensureDatabase(t.Context()); err == nil || !strings.Contains(err.Error(), "cannot read it either") {
		t.Fatalf("a database this user can neither create nor read booted with %v; want the refusal naming both", err)
	}
}
