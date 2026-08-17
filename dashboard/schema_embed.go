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
// It returns the number applied and the number already present, so the boot log
// can say which of the two happened without the operator having to guess.
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
