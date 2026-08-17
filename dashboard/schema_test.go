package main

import (
	"fmt"
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

// The counts are the model document's own, from its §01 lede: "25 vertex types,
// 3 document types, 32 edge types". GaosVertex and GaosEdge are supertypes that
// everything else EXTENDS and are deliberately outside those numbers.
const (
	wantVertexTypes   = 25
	wantDocumentTypes = 3
	wantEdgeTypes     = 32
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
