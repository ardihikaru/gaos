package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// The ArcadeDB leg: reachability, and the query path store_arcade.go runs on.
//
// /healthz tells you whether the database is reachable from inside the
// container, on the network the compose file put it on, with the credentials
// the env file supplied. That is the check worth having first, because the
// three ways this goes wrong -- wrong external network name, wrong container
// alias, wrong password -- all produce a stack that starts cleanly and looks
// completely healthy.
//
// The endpoint is GET {url}/api/v1/ready. MEASURED against both live instances
// on 2026-08-07: it answers 204 No Content, NOT 200. A check written against
// 200 reports every healthy database as unreachable, and you go looking at the
// network for an hour.
//
// Instances on this host:
//
//	dev  http://arcadedb-master-dev:2480   network master-compose-ai-dev-net
//	prod http://arcadedb-master:2480       network master-compose-ai-net
//
// Unset GAOS_ARCADE_URL means "not configured", which is the local stack and a
// perfectly valid state -- see handleHealth for why that stays a 200.
type Arcade struct {
	URL      string
	DB       string
	User     string
	Password string
	client   *http.Client

	// A second client, because the two jobs want different patience. The probe
	// backs /healthz and must fail fast -- a health endpoint that hangs for
	// thirty seconds is worse than one that says "unreachable". A statement
	// carrying a whole master record has no such constraint, and the schema
	// applier sends 314 of them on a cold database.
	stmtClient *http.Client
}

func ArcadeFromEnv() *Arcade {
	return &Arcade{
		URL:        strings.TrimRight(os.Getenv("GAOS_ARCADE_URL"), "/"),
		DB:         envOr("GAOS_ARCADE_DB", "gaos"),
		User:       os.Getenv("GAOS_ARCADE_USER"),
		Password:   os.Getenv("GAOS_ARCADE_PASSWORD"),
		client:     &http.Client{Timeout: 4 * time.Second},
		stmtClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (a *Arcade) Configured() bool { return a != nil && a.URL != "" }

// Describe is for the boot log. It never prints the password, and it never
// prints the user either -- a boot line is the easiest thing in the world to
// paste into an issue.
func (a *Arcade) Describe() string {
	if !a.Configured() {
		return "unconfigured"
	}
	return a.URL + "/" + a.DB
}

// Probe returns the database name on success, or "unreachable" with the reason.
func (a *Arcade) Probe(ctx context.Context) (string, error) {
	if !a.Configured() {
		return "unconfigured", nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL+"/api/v1/ready", nil)
	if err != nil {
		return "unreachable", err
	}
	if a.User != "" {
		req.SetBasicAuth(a.User, a.Password)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return "unreachable", err
	}
	defer resp.Body.Close()

	// 204 is the healthy answer. 200 is accepted too, so that a future ArcadeDB
	// that starts sending a body does not read as an outage.
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return "unreachable", fmt.Errorf("ready probe returned %d", resp.StatusCode)
	}
	return a.DB, nil
}

// ---------------------------------------------------------------- the query path
//
// Everything below is T-007. Until it existed the only thing in this repository
// that knew how to talk SQL to ArcadeDB was deploy/schema/apply.sh, in shell,
// and the dashboard could tell you the database was alive without being able to
// ask it anything.
//
// Three endpoints, all POST, all taking the same envelope:
//
//	/api/v1/query/{db}    read-only; refuses a statement that would write
//	/api/v1/command/{db}  the write path
//	/api/v1/begin|commit|rollback/{db}   transactions, keyed by a session header
//
// MEASURED against arcadedata/arcadedb:26.7.2 on 2026-08-17, because the model
// document's own warning about the dialect moving between releases applies to
// the wire format too:
//
//   - a successful body is {"user":"...","result":[ ... ]}, and `result` is
//     present even for a statement that returns nothing;
//   - a failure is HTTP 500 with {"error":...,"exception":...,"detail":...},
//     and `detail` is the only field worth putting in front of a human --
//     "Type with name 'NoSuchType' was not found" rather than "Error on
//     transaction commit";
//   - an aggregate over no rows comes back as an EMPTY result array, not as a
//     row holding null. seqNext depends on this and says so again there;
//   - begin answers 204 with the session id in the `arcadedb-session-id`
//     header, which every statement in that transaction must echo back.

// ErrUnconfigured is what every statement returns when GAOS_ARCADE_URL is unset.
// Unconfigured is a legitimate state -- it is the local stack, and handleHealth
// reports it as healthy -- so it must be distinguishable from a database that
// is configured and refusing.
var ErrUnconfigured = errors.New("arcadedb is not configured")

// ArcadeError carries the server's own account of the failure. The HTTP status
// alone is useless here: every SQL error is a 500, so the status tells you the
// statement failed and Detail tells you why.
type ArcadeError struct {
	Status    int
	Op        string // "query" | "command" | "begin" | "commit" | "rollback"
	Err       string
	Exception string
	Detail    string
	SQL       string
}

func (e *ArcadeError) Error() string {
	msg := e.Detail
	if msg == "" {
		msg = e.Err
	}
	if msg == "" {
		msg = fmt.Sprintf("HTTP %d", e.Status)
	}
	if e.SQL != "" {
		return fmt.Sprintf("arcadedb %s: %s (%s)", e.Op, msg, firstN(e.SQL, 90))
	}
	return fmt.Sprintf("arcadedb %s: %s", e.Op, msg)
}

// AlreadyExists reports whether the failure is a re-declaration. The schema
// applier needs this and only this: every statement carries IF NOT EXISTS, but
// ArcadeDB still reports some re-declarations as errors, and a re-run that
// reported them would make the idempotence this project relies on unverifiable.
// deploy/schema/apply.sh matches the same substring for the same reason.
func (e *ArcadeError) AlreadyExists() bool {
	return strings.Contains(strings.ToLower(e.Detail+" "+e.Err), "already exist")
}

// Query runs a read. Results are decoded from the envelope's `result` array
// into out, which should be a pointer to a slice of structs or maps.
//
// Params are sent as parameters, not interpolated. apply.sh gets away with
// refusing quotes outright because DDL contains none; a product goal or a BRD
// problem statement certainly does, and a store that built SQL by concatenation
// would be one apostrophe away from a syntax error and rather worse than that.
func (a *Arcade) Query(ctx context.Context, sql string, params map[string]any, out any) error {
	return a.statement(ctx, "query", "", sql, params, out)
}

// Command runs a write. Same envelope, different endpoint: /api/v1/query
// refuses a statement that mutates, which is a useful accident -- a read path
// that tries to write fails loudly at the transport rather than quietly in the
// graph.
func (a *Arcade) Command(ctx context.Context, sql string, params map[string]any, out any) error {
	return a.statement(ctx, "command", "", sql, params, out)
}

// ArcadeTx is one transaction. Every statement on it carries the session header,
// and a Commit that never happens leaves the writes invisible -- which is why
// every caller defers Rollback and lets the successful Commit make it a no-op.
type ArcadeTx struct {
	a       *Arcade
	session string
	done    bool
}

// Begin opens a transaction. The whole reason it exists is the event write: a
// mutation allocates a sequence number, writes the event, writes the snapshot
// and updates the vertex, and a crash between any two of those would leave a
// state change that emitted no event -- exactly what BR-02 forbids.
func (a *Arcade) Begin(ctx context.Context) (*ArcadeTx, error) {
	if !a.Configured() {
		return nil, ErrUnconfigured
	}
	resp, body, err := a.send(ctx, "/api/v1/begin/"+a.DB, "", nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return nil, decodeArcadeError(resp.StatusCode, "begin", "", body)
	}
	sid := resp.Header.Get("arcadedb-session-id")
	if sid == "" {
		return nil, &ArcadeError{Status: resp.StatusCode, Op: "begin",
			Detail: "no arcadedb-session-id header; the transaction has no handle"}
	}
	return &ArcadeTx{a: a, session: sid}, nil
}

func (t *ArcadeTx) Query(ctx context.Context, sql string, params map[string]any, out any) error {
	return t.a.statement(ctx, "query", t.session, sql, params, out)
}

func (t *ArcadeTx) Command(ctx context.Context, sql string, params map[string]any, out any) error {
	return t.a.statement(ctx, "command", t.session, sql, params, out)
}

func (t *ArcadeTx) Commit(ctx context.Context) error {
	if t.done {
		return nil
	}
	t.done = true
	return t.a.control(ctx, "commit", t.session)
}

// Rollback is safe to defer after a Commit: the first of the two to run marks
// the transaction done and the second returns nil.
func (t *ArcadeTx) Rollback(ctx context.Context) {
	if t.done {
		return
	}
	t.done = true
	_ = t.a.control(ctx, "rollback", t.session)
}

func (a *Arcade) control(ctx context.Context, op, session string) error {
	resp, body, err := a.send(ctx, "/api/v1/"+op+"/"+a.DB, session, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return decodeArcadeError(resp.StatusCode, op, "", body)
	}
	return nil
}

// statement is the one place a SQL envelope is built, so the shape is right
// once rather than at every call site.
func (a *Arcade) statement(ctx context.Context, op, session, sql string, params map[string]any, out any) error {
	if !a.Configured() {
		return ErrUnconfigured
	}
	payload := map[string]any{"language": "sql", "command": sql}
	if len(params) > 0 {
		payload["params"] = params
	}
	enc, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("arcadedb %s: encode: %w", op, err)
	}

	resp, body, err := a.send(ctx, "/api/v1/"+op+"/"+a.DB, session, enc)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return decodeArcadeError(resp.StatusCode, op, sql, body)
	}
	if out == nil {
		return nil
	}
	var env struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("arcadedb %s: decode envelope: %w", op, err)
	}
	if len(env.Result) == 0 {
		return nil
	}
	if err := json.Unmarshal(env.Result, out); err != nil {
		return fmt.Errorf("arcadedb %s: decode result: %w", op, err)
	}
	return nil
}

// send performs the request and reads the whole body. The body is read even on
// a failure status, because on this server the body IS the error message.
func (a *Arcade) send(ctx context.Context, path, session string, body []byte) (*http.Response, []byte, error) {
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.URL+path, rdr)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if session != "" {
		req.Header.Set("arcadedb-session-id", session)
	}
	if a.User != "" {
		req.SetBasicAuth(a.User, a.Password)
	}
	client := a.stmtClient
	if client == nil {
		client = a.client
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	// Bounded: a runaway result set should not be able to exhaust the
	// dashboard's memory through an error path nobody is watching.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, nil, err
	}
	return resp, raw, nil
}

func decodeArcadeError(status int, op, sql string, body []byte) *ArcadeError {
	e := &ArcadeError{Status: status, Op: op, SQL: sql}
	var parsed struct {
		Error     string `json:"error"`
		Exception string `json:"exception"`
		Detail    string `json:"detail"`
	}
	if err := json.Unmarshal(body, &parsed); err == nil {
		e.Err, e.Exception, e.Detail = parsed.Error, parsed.Exception, parsed.Detail
	}
	if e.Err == "" && e.Detail == "" {
		e.Detail = strings.TrimSpace(firstN(string(body), 200))
	}
	return e
}
