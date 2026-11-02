package main

import (
	"context"
	"embed"
	"fmt"
	"sort"
	"strings"
)

// The schema, carried inside the binary.
//
// schema/RECONCILIATION.md says why the .sql files live in this module rather
// than beside deploy/schema/apply.sh, and it is a constraint rather than a
// preference: the deployed dashboard runs on distroless with no shell, so the
// script cannot run there, and //go:embed cannot reach outside its own module.
// One copy, two readers -- the script for a scratch database from a
// workstation, and this for dev and prod.
//
// RECONCILIATION.md wrote that promise against T-007. This is it.
//
//go:embed schema/*.sql
var schemaFS embed.FS

// ApplySchema creates the database if it is missing and applies every numbered
// file in filename order.
//
// IDEMPOTENT, and that property is the point rather than a bonus. Every
// statement carries IF NOT EXISTS, so a second run is a no-op; ArcadeDB still
// reports some re-declarations as errors, which is why AlreadyExists is a skip
// and not a failure. A migration you are afraid to re-run is a migration nobody
// runs -- and this one runs on every boot of every dashboard in both tiers.
//
// It returns two counts, and what they MEAN is narrower than their names
// suggest -- measured on 26.7.2, and worth stating because the first reader of
// this comment believed the flattering version.
//
//	present  statements the server rejected with AlreadyExists
//	applied  every other statement that succeeded
//
// `applied` is NOT "newly created". Most of the DDL is written IF NOT EXISTS,
// and the server honours that silently rather than raising AlreadyExists -- so a
// re-run against a fully populated database still reports 128 applied and 196
// present, not 0 and 324. The pair tells an operator how large the embedded DDL
// is and that none of it failed; it does not tell them whether anything changed.
// Nothing in this repository can answer that from the counts alone, and a log
// line implying otherwise would be worse than no log line.
func (a *Arcade) ApplySchema(ctx context.Context) (applied, present int, err error) {
	if !a.Configured() {
		return 0, 0, ErrUnconfigured
	}
	if err := a.ensureDatabase(ctx); err != nil {
		return 0, 0, err
	}

	files, err := schemaFiles()
	if err != nil {
		return 0, 0, err
	}
	for _, name := range files {
		raw, err := schemaFS.ReadFile("schema/" + name)
		if err != nil {
			return applied, present, fmt.Errorf("read embedded %s: %w", name, err)
		}
		for _, stmt := range splitStatements(string(raw)) {
			if err := a.Command(ctx, stmt, nil, nil); err != nil {
				if ae, ok := err.(*ArcadeError); ok && ae.AlreadyExists() {
					present++
					continue
				}
				return applied, present, fmt.Errorf("%s: %w", name, err)
			}
			applied++
		}
	}
	return applied, present, nil
}

// ensureDatabase creates the database. `create database` is a SERVER command,
// not a database one, so it goes to a different endpoint -- and an existing
// database answers 400 rather than 200, which is the normal case on every boot
// after the first and is not a failure.
func (a *Arcade) ensureDatabase(ctx context.Context) error {
	resp, body, err := a.send(ctx, "/api/v1/server", "",
		[]byte(fmt.Sprintf(`{"command":%q}`, "create database "+a.DB)))
	if err != nil {
		return err
	}
	if resp.StatusCode == 200 {
		return nil
	}
	ae := decodeArcadeError(resp.StatusCode, "command", "create database "+a.DB, body)
	if ae.AlreadyExists() {
		return nil
	}
	// A least-privilege user -- gaos_dev / gaos_prod on the shared server, whose
	// rights are on their own database alone -- may not run server commands at
	// all, so creation is refused even when the database is right there. That
	// is the deployment working as intended, not a failure: the database is
	// created once by the server's operator. Prove it exists by reading it as
	// this user, and boot; if even that fails, say which of the two it is.
	if ae.RootOnly() {
		var one []map[string]any
		if qerr := a.Query(ctx, "SELECT 1 AS one", nil, &one); qerr != nil {
			return fmt.Errorf("database %s: this user may not create it (%s), and cannot read it either: %w", a.DB, ae.Err, qerr)
		}
		return nil
	}
	return ae
}

// schemaFiles lists the embedded .sql files in filename order. The order is
// load-bearing -- supertypes before the types that EXTEND them, types before
// the indexes over them -- which is what TestSchemaDirectoryHasEveryNumberedFile
// guards against a gap in.
func schemaFiles() ([]string, error) {
	entries, err := schemaFS.ReadDir("schema")
	if err != nil {
		return nil, fmt.Errorf("read embedded schema: %w", err)
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil, fmt.Errorf("no .sql files embedded; the //go:embed pattern moved")
	}
	return out, nil
}

// splitStatements is the Go half of the sed pipeline in deploy/schema/apply.sh,
// and deliberately makes the same three moves in the same order: strip --
// comments, fold the multi-line CREATE PROPERTY declarations back together, and
// split on the terminator. Whitespace is collapsed because the wire format is
// JSON and SQL does not care.
//
// The two readers of these files must agree about what a statement is, or the
// script and the binary would disagree about whether the schema applied.
func splitStatements(sql string) []string {
	var b strings.Builder
	for _, line := range strings.Split(sql, "\n") {
		if i := strings.Index(line, "--"); i >= 0 {
			line = line[:i]
		}
		b.WriteString(line)
		b.WriteByte(' ')
	}
	var out []string
	for _, stmt := range strings.Split(b.String(), ";") {
		if s := strings.Join(strings.Fields(stmt), " "); s != "" {
			out = append(out, s)
		}
	}
	return out
}
