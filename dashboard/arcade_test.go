package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// Guards on the query path T-007 added, and on the embedded copy of the schema.
//
// None of these need a database. They run against an httptest server that
// speaks ArcadeDB's envelope, which is enough to pin the parts that were got
// wrong at least once during the writing: the response shape, the fact that the
// useful half of an error is in the body rather than the status, and that an
// unconfigured store refuses rather than dialling nothing.
//
// The behaviour they stand in for was measured against arcadedata/arcadedb:
// 26.7.2 on 2026-08-17, and the live version of these assertions is
// TestStoreConformanceArcadeDB.

func testArcade(t *testing.T, h http.HandlerFunc) *Arcade {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &Arcade{
		URL: srv.URL, DB: "gaos",
		client:     srv.Client(),
		stmtClient: srv.Client(),
	}
}

func TestArcadeQueryDecodesTheResultEnvelope(t *testing.T) {
	var gotPath, gotBody string
	a := testArcade(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"user":"root","result":[{"id":"p-1","name":"Quote Engine"}]}`)
	})

	var rows []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	err := a.Query(context.Background(),
		"SELECT id, name FROM Product WHERE tenantId = :t",
		map[string]any{"t": "loop-eng"}, &rows)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if gotPath != "/api/v1/query/gaos" {
		t.Errorf("a read went to %q, want /api/v1/query/gaos -- the command endpoint "+
			"would accept a write from a path that is meant to be read-only", gotPath)
	}
	if len(rows) != 1 || rows[0].Name != "Quote Engine" {
		t.Fatalf("decoded %+v, want one row named Quote Engine", rows)
	}

	// The parameters must travel as parameters. Building SQL by concatenation
	// is one apostrophe in a product goal away from a syntax error, and rather
	// worse than that.
	var sent map[string]any
	if err := json.Unmarshal([]byte(gotBody), &sent); err != nil {
		t.Fatalf("the request body is not JSON: %v", err)
	}
	if sent["language"] != "sql" {
		t.Errorf("language is %v, want \"sql\"", sent["language"])
	}
	params, ok := sent["params"].(map[string]any)
	if !ok || params["t"] != "loop-eng" {
		t.Errorf("params travelled as %v, want a params object carrying t=loop-eng", sent["params"])
	}
	if strings.Contains(sent["command"].(string), "loop-eng") {
		t.Error("the parameter was interpolated into the statement text")
	}
}

func TestArcadeCommandUsesTheWriteEndpoint(t *testing.T) {
	var gotPath string
	a := testArcade(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		io.WriteString(w, `{"user":"root","result":[{"count":1}]}`)
	})
	if err := a.Command(context.Background(), "UPDATE Product SET name = :n UPSERT WHERE id = :i",
		map[string]any{"n": "x", "i": "p-1"}, nil); err != nil {
		t.Fatalf("Command: %v", err)
	}
	if gotPath != "/api/v1/command/gaos" {
		t.Errorf("a write went to %q, want /api/v1/command/gaos", gotPath)
	}
}

// TestArcadeErrorCarriesTheServerDetail is the negative case that matters most.
// Every SQL failure on this server is an HTTP 500, so the status says only that
// something went wrong; `detail` is the half a human can act on.
func TestArcadeErrorCarriesTheServerDetail(t *testing.T) {
	a := testArcade(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, `{"error":"Error on transaction commit",`+
			`"exception":"com.arcadedb.exception.SchemaException",`+
			`"detail":"Type with name 'NoSuchType' was not found"}`)
	})
	err := a.Query(context.Background(), "SELECT nope FROM NoSuchType", nil, &[]any{})
	if err == nil {
		t.Fatal("a 500 was reported as success")
	}
	var ae *ArcadeError
	if !errors.As(err, &ae) {
		t.Fatalf("got %T, want an *ArcadeError carrying the server's own account", err)
	}
	if ae.Status != 500 {
		t.Errorf("status is %d, want 500", ae.Status)
	}
	if ae.Detail != "Type with name 'NoSuchType' was not found" {
		t.Errorf("detail is %q, want the server's detail field", ae.Detail)
	}
	if !strings.Contains(err.Error(), "NoSuchType") {
		t.Errorf("the message reads %q; it must carry the detail rather than "+
			"\"Error on transaction commit\", which names no cause", err.Error())
	}
}

// TestArcadeAlreadyExistsIsNotAFailure pins the property the schema applier
// rests on: IF NOT EXISTS is on every statement, but ArcadeDB still reports
// some re-declarations as errors, and a re-run that treated those as failures
// would make the idempotence this project relies on unverifiable.
func TestArcadeAlreadyExistsIsNotAFailure(t *testing.T) {
	cases := []struct {
		body string
		want bool
	}{
		{`{"error":"Cannot execute command","detail":"Database 'gaos' already exists"}`, true},
		{`{"error":"x","detail":"Type 'Product' already exists"}`, true},
		{`{"error":"x","detail":"Type with name 'NoSuchType' was not found"}`, false},
		{`{"error":"x","detail":""}`, false},
	}
	for _, c := range cases {
		ae := decodeArcadeError(500, "command", "", []byte(c.body))
		if got := ae.AlreadyExists(); got != c.want {
			t.Errorf("AlreadyExists() is %v for %s, want %v", got, c.body, c.want)
		}
	}
}

func TestArcadeUnconfiguredRefusesToQuery(t *testing.T) {
	a := &Arcade{DB: "gaos"} // no URL: the local stack, and a valid state
	ctx := context.Background()

	if err := a.Query(ctx, "SELECT 1", nil, nil); !errors.Is(err, ErrUnconfigured) {
		t.Errorf("Query on an unconfigured store returned %v, want ErrUnconfigured", err)
	}
	if err := a.Command(ctx, "SELECT 1", nil, nil); !errors.Is(err, ErrUnconfigured) {
		t.Errorf("Command on an unconfigured store returned %v, want ErrUnconfigured", err)
	}
	if _, err := a.Begin(ctx); !errors.Is(err, ErrUnconfigured) {
		t.Errorf("Begin on an unconfigured store returned %v, want ErrUnconfigured", err)
	}
	// The probe stays the exception, because handleHealth reports an
	// unconfigured database as healthy rather than as an outage.
	if db, err := a.Probe(ctx); err != nil || db != "unconfigured" {
		t.Errorf("Probe reported (%q, %v), want (\"unconfigured\", nil)", db, err)
	}
}

// TestArcadeTransactionCarriesTheSessionHeader pins the mechanism the event
// write depends on: allocate a sequence number, write the event and write the
// state in one transaction, or risk a state change that emitted no event.
func TestArcadeTransactionCarriesTheSessionHeader(t *testing.T) {
	var sawSession, committed bool
	a := testArcade(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/v1/begin/"):
			w.Header().Set("arcadedb-session-id", "AS-test-session")
			w.WriteHeader(http.StatusNoContent)
		case strings.HasPrefix(r.URL.Path, "/api/v1/commit/"):
			committed = r.Header.Get("arcadedb-session-id") == "AS-test-session"
			w.WriteHeader(http.StatusNoContent)
		default:
			sawSession = r.Header.Get("arcadedb-session-id") == "AS-test-session"
			io.WriteString(w, `{"result":[]}`)
		}
	})

	ctx := context.Background()
	tx, err := a.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := tx.Command(ctx, "INSERT INTO Event SET seq = 1", nil, nil); err != nil {
		t.Fatalf("Command in transaction: %v", err)
	}
	if !sawSession {
		t.Error("a statement inside the transaction carried no arcadedb-session-id; " +
			"it ran outside the transaction it was meant to be in")
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if !committed {
		t.Error("commit carried no session id")
	}
	// Rollback after a successful Commit must be a no-op, because every caller
	// defers it.
	tx.Rollback(ctx)
}

func TestArcadeBeginWithoutASessionHeaderIsAnError(t *testing.T) {
	a := testArcade(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent) // no arcadedb-session-id
	})
	if _, err := a.Begin(context.Background()); err == nil {
		t.Fatal("Begin accepted a response with no session id; every statement after " +
			"it would silently run outside the transaction")
	}
}

func TestArcadeQueryHonoursTheContextDeadline(t *testing.T) {
	// The handler stalls for longer than the caller's deadline but not forever:
	// httptest.Server.Close waits for outstanding requests, and a handler that
	// only ever returns when the client goes away deadlocks the shutdown.
	release := make(chan struct{})
	a := testArcade(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-time.After(3 * time.Second):
		}
	})
	// Registered after testArcade so it runs BEFORE srv.Close: cleanups are
	// last-in-first-out, and the other order is the deadlock this is avoiding.
	t.Cleanup(func() { close(release) })

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := a.Query(ctx, "SELECT 1", nil, nil)
	if err == nil {
		t.Fatal("a query against a server that never answers returned success")
	}
	if elapsed := time.Since(start); elapsed > 4*time.Second {
		t.Errorf("the query took %s to give up; store.go put a context on every "+
			"method so a slow database cannot hold a request open", elapsed)
	}
}

// ---------------------------------------------------------------- the embed

// TestEmbeddedSchemaMatchesTheDirectory is the drift guard. schema_test.go
// checks the .sql files on disk; the binary applies an embedded copy, and a
// //go:embed pattern that stopped matching a file would leave those two
// disagreeing with nothing to notice -- the deployed dashboard would apply a
// schema one file short and report success.
func TestEmbeddedSchemaMatchesTheDirectory(t *testing.T) {
	onDisk, err := filepath.Glob(filepath.Join(schemaDir, "*.sql"))
	if err != nil {
		t.Fatalf("glob %s: %v", schemaDir, err)
	}
	for i := range onDisk {
		onDisk[i] = filepath.Base(onDisk[i])
	}
	sort.Strings(onDisk)

	embedded, err := schemaFiles()
	if err != nil {
		t.Fatalf("schemaFiles: %v", err)
	}
	if len(embedded) != len(onDisk) {
		t.Fatalf("the binary embeds %d schema files and %s holds %d: %v vs %v -- "+
			"the //go:embed pattern and the directory have drifted apart",
			len(embedded), schemaDir, len(onDisk), embedded, onDisk)
	}
	for i := range onDisk {
		if embedded[i] != onDisk[i] {
			t.Fatalf("embedded file %d is %s, on disk it is %s", i, embedded[i], onDisk[i])
		}
		want, err := os.ReadFile(filepath.Join(schemaDir, onDisk[i]))
		if err != nil {
			t.Fatalf("read %s: %v", onDisk[i], err)
		}
		got, err := schemaFS.ReadFile("schema/" + embedded[i])
		if err != nil {
			t.Fatalf("read embedded %s: %v", embedded[i], err)
		}
		if string(got) != string(want) {
			t.Errorf("%s differs between the embedded copy and the file on disk", onDisk[i])
		}
	}
}

// TestSplitStatementsMatchesTheShellApplier guards the agreement between the
// two readers of the DDL. deploy/schema/apply.sh strips -- comments, folds the
// multi-line CREATE PROPERTY declarations back together and splits on the
// terminator; if the Go half counted statements differently, the script and the
// binary would disagree about whether a schema had been applied.
func TestSplitStatementsMatchesTheShellApplier(t *testing.T) {
	got := splitStatements(`
-- a comment, and a fake statement inside it: CREATE VERTEX TYPE Nope;
CREATE VERTEX TYPE Product IF NOT EXISTS EXTENDS GaosVertex;

CREATE PROPERTY Product.readinessBand  STRING  (default 'INSUFFICIENT',
                 regexp 'INSUFFICIENT|MINIMUM|WORKING|COMPLETE');   -- trailing
`)
	want := []string{
		"CREATE VERTEX TYPE Product IF NOT EXISTS EXTENDS GaosVertex",
		"CREATE PROPERTY Product.readinessBand STRING (default 'INSUFFICIENT', " +
			"regexp 'INSUFFICIENT|MINIMUM|WORKING|COMPLETE')",
	}
	if len(got) != len(want) {
		t.Fatalf("split into %d statements, want %d: %q", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("statement %d is\n  %q\nwant\n  %q", i, got[i], want[i])
		}
	}
}

// TestEverySchemaStatementSurvivesTheSplit is the negative half: a statement
// that came out empty, or one that still carries a comment marker, means the
// binary would send something the database cannot parse.
func TestEverySchemaStatementSurvivesTheSplit(t *testing.T) {
	files, err := schemaFiles()
	if err != nil {
		t.Fatalf("schemaFiles: %v", err)
	}
	total := 0
	for _, name := range files {
		raw, err := schemaFS.ReadFile("schema/" + name)
		if err != nil {
			t.Fatalf("read embedded %s: %v", name, err)
		}
		for _, stmt := range splitStatements(string(raw)) {
			total++
			if strings.Contains(stmt, "--") {
				t.Errorf("%s: a statement still carries a comment marker: %s", name, firstN(stmt, 80))
			}
			if strings.TrimSpace(stmt) == "" {
				t.Errorf("%s: an empty statement survived the split", name)
			}
		}
	}
	// 10-storage.sql is ALTER-only and D-046 commented three of its statements
	// out, so the total is well under the 314 apply.sh reports for a fresh
	// database; what matters here is that the split produced work at all.
	if total < 300 {
		t.Errorf("the embedded schema split into %d statements; the DDL is a few "+
			"hundred and a number this low means a file stopped being read", total)
	}
}
