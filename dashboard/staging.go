package main

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/netip"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The staging callback (D-088, docs/designs/gaos-pr-agent-flows.md §8.2).
//
// Jenkins calls GAOS when the staging deploy has finished:
//
//	POST /api/v1/products/{id}/staging
//	Authorization: Basic base64(user:pass)
//	{"repo":"owner/repo","branch":"staging","commit":"<sha>","environment":"staging"}
//
// BR-52 and BR-55 say the machine API answers only a bearer token; Jenkins'
// credential step speaks HTTP Basic, so D-088 amends both for this one route.
// The credential is per PRODUCT: a credential valid for product A presented for
// product B answers 404, as a bearer client's product check does.

// stagingAuth is one Basic route's credentials, read once at boot: the staging
// callback's (GAOS_STAGING_BASIC_AUTH_FILE) or the unit-results route's
// (GAOS_UNIT_RESULTS_BASIC_AUTH_FILE, D-106). Each has its own lines and its own
// failure map, so a credential of one never opens the other and a lock on one
// never locks the other.
type stagingAuth struct {
	sums  map[string][sha256.Size]byte // product id -> sha256("user:password")
	why   string                       // non-empty: the file was set and unusable
	route string                       // "staging" | "unit-results": the lock keys and the log lines

	mu       sync.Mutex
	fails    map[string]stagingFails
	lastSeen map[string]time.Time // product id -> the last accepted callback
	now      func() time.Time
	logf     func(string, ...any)
}

type stagingFails struct {
	n     int
	until time.Time
	first time.Time
	quiet int // refusals not logged since the key's first failure or its lock (D-106 §5.4)
}

// The failure throttle: stagingMaxFails failures inside stagingWindow lock the
// source out for stagingLock. The map holds at most stagingMaxKeys keys: when
// it is full a new key is not inserted and the request is throttled (D-106).
const (
	stagingMaxFails = 5
	stagingWindow   = 5 * time.Minute
	stagingLock     = 5 * time.Minute
	stagingMaxKeys  = 4096
)

// The routes' names, which are also the lock keys' route component.
const (
	routeStaging     = "staging"
	routeUnitResults = "unit-results"
)

// The minimum password lengths: the staging file keeps D-088's 12, the
// unit-results file wants a generated secret (openssl rand -base64 32).
const (
	stagingMinPass     = 12
	unitResultsMinPass = 32
)

// stagingAuthFromEnv reads GAOS_STAGING_BASIC_AUTH_FILE. Unset means the
// callback is off, which is not an error.
func stagingAuthFromEnv() *stagingAuth {
	return basicAuthFromEnv("GAOS_STAGING_BASIC_AUTH_FILE", routeStaging, "staging", stagingMinPass)
}

// unitResultsAuthFromEnv reads GAOS_UNIT_RESULTS_BASIC_AUTH_FILE (D-106): the
// same format and parser as the staging file, its own lines, a 32-character
// minimum. Unset means the route is off.
func unitResultsAuthFromEnv() *stagingAuth {
	return basicAuthFromEnv("GAOS_UNIT_RESULTS_BASIC_AUTH_FILE", routeUnitResults, "unit-results", unitResultsMinPass)
}

// basicAuthFromEnv reads one Basic route's credential file, named by the
// environment variable env: lines of productId:user:password, mode 0600. A
// refused file is named in why without its content.
func basicAuthFromEnv(env, route, what string, minPass int) *stagingAuth {
	a := &stagingAuth{sums: map[string][sha256.Size]byte{}, fails: map[string]stagingFails{},
		lastSeen: map[string]time.Time{}, now: time.Now, route: route, logf: log.Printf}
	path := strings.TrimSpace(os.Getenv(env))
	if path == "" {
		return a
	}
	st, err := os.Stat(path)
	if err != nil || !st.Mode().IsRegular() {
		a.why = "the " + what + " credential file cannot be read"
		return a
	}
	if st.Mode().Perm()&0o077 != 0 {
		a.why = "the " + what + " credential file is readable by group or others; it must be mode 0600"
		return a
	}
	b, err := os.ReadFile(path)
	if err != nil {
		a.why = "the " + what + " credential file cannot be read"
		return a
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 || parts[0] == "" || parts[1] == "" || len(parts[2]) < minPass {
			a.why = "the " + what + " credential file wants lines of productId:user:password with a password of at least " +
				strconv.Itoa(minPass) + " characters"
			a.sums = map[string][sha256.Size]byte{}
			return a
		}
		a.sums[parts[0]] = sha256.Sum256([]byte(parts[1] + ":" + parts[2]))
	}
	// An empty file is "off": the compose mount needs the file to exist, and an
	// empty one is how an operator leaves the callback unused.
	return a
}

func (a *stagingAuth) enabled() bool { return a != nil && a.why == "" && len(a.sums) > 0 }

// has reports whether the route is on and holds a line for the product.
func (a *stagingAuth) has(productID string) bool {
	if !a.enabled() {
		return false
	}
	_, ok := a.sums[productID]
	return ok
}

// match answers which products the presented credential belongs to. Every
// stored sum is compared in constant time, with no early exit.
func (a *stagingAuth) match(user, pass string) map[string]bool {
	got := sha256.Sum256([]byte(user + ":" + pass))
	out := map[string]bool{}
	for id, sum := range a.sums {
		if subtle.ConstantTimeCompare(got[:], sum[:]) == 1 {
			out[id] = true
		}
	}
	return out
}

// pruneLocked drops the keys whose window and lock have both passed, logging
// the summary of a key that refused requests it did not log.
func (a *stagingAuth) pruneLocked(now time.Time) {
	for k, v := range a.fails {
		if now.Sub(v.first) > stagingWindow && !v.until.After(now) {
			a.summaryLocked(k, v)
			delete(a.fails, k)
		}
	}
}

// summaryLocked writes the one line of a lock window's unlogged refusals.
func (a *stagingAuth) summaryLocked(key string, f stagingFails) {
	if f.quiet > 0 && a.logf != nil {
		src, _, _ := strings.Cut(key, "|")
		a.logf("%s: %d refused from %s in the last 5 m", a.route, f.quiet, src)
	}
}

// locked reports whether a key is locked out, and for how long. A refusal
// while locked is counted, not logged.
func (a *stagingAuth) locked(key string) (time.Duration, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	f, ok := a.fails[key]
	if !ok {
		return 0, false
	}
	if left := f.until.Sub(a.now()); left > 0 {
		f.quiet++
		a.fails[key] = f
		return left, true
	}
	return 0, false
}

// failResult is what fail decided: full means the map was full and the key new,
// so nothing was counted and the request is throttled; log means this failure
// is the key's first or its transition into a lock, and gets a line.
type failResult struct {
	full, log, locked bool
}

// fail records a failure for a key. Expired keys are dropped first; when the
// map is still full a new key is not inserted (the caller answers 429), while
// an existing key still counts.
func (a *stagingAuth) fail(key string) failResult {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	f, ok := a.fails[key]
	if !ok && len(a.fails) >= stagingMaxKeys {
		a.pruneLocked(now)
		if len(a.fails) >= stagingMaxKeys {
			return failResult{full: true}
		}
	}
	var r failResult
	if f.first.IsZero() || (now.Sub(f.first) > stagingWindow && !f.until.After(now)) {
		a.summaryLocked(key, f)
		f = stagingFails{first: now}
		r.log = true
	}
	f.n++
	if f.n >= stagingMaxFails && !f.until.After(now) {
		f.until = now.Add(stagingLock)
		r.log, r.locked = true, true
	}
	if !r.log {
		f.quiet++
	}
	a.fails[key] = f
	return r
}

// ok clears one key after a success: only the credential's own product key
// (src|route|p), never src|route, which expires on its own window (D-106).
func (a *stagingAuth) ok(key string) {
	a.mu.Lock()
	delete(a.fails, key)
	a.mu.Unlock()
}

// failCount is how many keys the map holds (a test reads it).
func (a *stagingAuth) failCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.fails)
}

func (a *stagingAuth) seen(product string) {
	a.mu.Lock()
	a.lastSeen[product] = a.now()
	a.mu.Unlock()
}

// LastCallback is when a product's last staging callback was accepted.
func (a *stagingAuth) LastCallback(product string) (time.Time, bool) {
	if a == nil {
		return time.Time{}, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	t, ok := a.lastSeen[product]
	return t, ok
}

// jenkinsSource is the address a Basic route's failure is counted against
// (D-106 §5.4, a fix to D-088's gate): the X-Real-IP header the edge sets from
// $remote_addr when it parses as an address; else the rightmost X-Forwarded-For
// hop that is not loopback, private or link-local (the hop the edge appended,
// never the caller's first); else the peer. An IPv4 address is kept whole, an
// IPv6 address becomes its /64, as the edge's own limiter keys it, so a caller
// holding a /64 cannot rotate into fresh buckets.
func jenkinsSource(r *http.Request) string {
	if a, ok := parseSourceAddr(r.Header.Get("X-Real-IP")); ok {
		return normSource(a)
	}
	if xff := r.Header.Values("X-Forwarded-For"); len(xff) > 0 {
		hops := strings.Split(strings.Join(xff, ","), ",")
		for i := len(hops) - 1; i >= 0; i-- {
			a, ok := parseSourceAddr(hops[i])
			if !ok || a.IsLoopback() || a.IsPrivate() || a.IsLinkLocalUnicast() || a.IsUnspecified() {
				continue
			}
			return normSource(a)
		}
	}
	if ap, err := netip.ParseAddrPort(r.RemoteAddr); err == nil {
		return normSource(ap.Addr())
	}
	if a, ok := parseSourceAddr(r.RemoteAddr); ok {
		return normSource(a)
	}
	return "unknown"
}

func parseSourceAddr(s string) (netip.Addr, bool) {
	a, err := netip.ParseAddr(strings.TrimSpace(s))
	if err != nil {
		return netip.Addr{}, false
	}
	return a.Unmap().WithZone(""), true
}

func normSource(a netip.Addr) string {
	if a.Is4() {
		return a.String()
	}
	p, err := a.Prefix(64)
	if err != nil {
		return a.String()
	}
	return p.String()
}

// jenkinsGate is the prelude of the two Basic routes, the staging callback and
// the unit results (D-088, D-106 §5.4), in the order of rev 17:
//
//  1. X-Forwarded-Prefix set and not /api: 404 (BR-52);
//  2. the route is off: 503 with off as the code;
//  3. src|route locked: 429 (no product is known yet; the path id is never consulted);
//  4. no credential, or one that matches nothing: 401, a failure against src|route;
//  5. the credential's product p: src|route|p locked is 429; p not the path id
//     is 404, a failure against src|route|p (never the path's id);
//  6. otherwise only src|route|p is cleared.
//
// A full failure map answers 429 for a key it cannot hold. It answers the path's
// product id when the caller may proceed.
func (s *server) jenkinsGate(w http.ResponseWriter, r *http.Request, a *stagingAuth, off string) (string, bool) {
	if p := r.Header.Get("X-Forwarded-Prefix"); p != "" && p != "/api" {
		apiErr(w, http.StatusNotFound, "not-found", "no such route")
		return "", false
	}
	if !a.enabled() {
		msg := "the staging callback is not configured"
		if a != nil && a.route == routeUnitResults {
			msg = "the unit-results route is not configured"
		}
		apiErr(w, http.StatusServiceUnavailable, off, msg)
		return "", false
	}
	src := jenkinsSource(r)
	routeKey := src + "|" + a.route
	throttled := func(left time.Duration) {
		w.Header().Set("Retry-After", strconv.Itoa(int(left.Seconds())+1))
		apiErr(w, http.StatusTooManyRequests, "throttled", "too many failed attempts")
	}
	counted := func(key string, status int, code string) bool {
		fr := a.fail(key)
		if fr.full {
			throttled(stagingLock - time.Second)
			return false
		}
		if fr.log && a.logf != nil {
			line := fmt.Sprintf("%s: - - from %s %d %s", a.route, src, status, code)
			if fr.locked {
				line += " (locked for 5 m)"
			}
			a.logf("%s", line)
		}
		return true
	}
	if left, locked := a.locked(routeKey); locked {
		throttled(left)
		return "", false
	}
	user, pass, ok := r.BasicAuth()
	owners := map[string]bool{}
	if ok {
		owners = a.match(user, pass)
	}
	if !ok || len(owners) == 0 {
		if counted(routeKey, http.StatusUnauthorized, "unauthorized") {
			w.Header().Set("WWW-Authenticate", `Basic realm="gaos"`)
			apiErr(w, http.StatusUnauthorized, "unauthorized", "a credential is required")
		}
		return "", false
	}
	id := r.PathValue("id")
	p := id
	if !owners[id] {
		// The credential's own product: the smallest, so one credential is one key.
		ids := make([]string, 0, len(owners))
		for o := range owners {
			ids = append(ids, o)
		}
		sort.Strings(ids)
		p = ids[0]
	}
	prodKey := routeKey + "|" + p
	if left, locked := a.locked(prodKey); locked {
		throttled(left)
		return "", false
	}
	if p != id {
		// Valid, but for another product: the answer must not say which exist.
		if counted(prodKey, http.StatusNotFound, "not-found") {
			apiErr(w, http.StatusNotFound, "not-found", "no such product")
		}
		return "", false
	}
	a.ok(prodKey)
	trafficPrincipal(r, "basic:"+a.route) // D-107 §8.4: after authentication only
	return id, true
}

// A full commit id, 40 hex (sha1) or 64 (sha256): a short one would key two
// spellings of one commit as two cards, and two commits sharing a short prefix
// as one.
var shaRe = regexp.MustCompile(`^(?:[0-9a-fA-F]{40}|[0-9a-fA-F]{64})$`)

type stagingBody struct {
	Repo        string `json:"repo"`
	Branch      string `json:"branch"`
	Commit      string `json:"commit"`
	Environment string `json:"environment"`
}

// apiStaging is POST /v1/products/{id}/staging.
func (s *server) apiStaging(w http.ResponseWriter, r *http.Request) {
	a := s.staging
	id, ok := s.jenkinsGate(w, r, a, "staging-off")
	if !ok {
		return
	}

	var b stagingBody
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&b); err != nil {
		apiErr(w, http.StatusUnprocessableEntity, "rejected", "the body must be {repo, branch, commit, environment}")
		return
	}
	owner, repo, okp := ghOwnerRepo(b.Repo)
	if !okp || !shaRe.MatchString(b.Commit) || (b.Environment != "" && b.Environment != "staging") {
		apiErr(w, http.StatusUnprocessableEntity, "rejected", "repo must be owner/repo, commit a full 40- or 64-hex sha, environment staging")
		return
	}
	masters, err := s.store.Masters(r.Context())
	if err != nil {
		apiStoreErr(w, err)
		return
	}
	owned := false
	for _, m := range masters {
		if m.ID == id && productOwns(m.Repos, owner, repo) {
			owned = true
		}
	}
	if !owned {
		apiErr(w, http.StatusUnprocessableEntity, "rejected", "the product does not own that repository")
		return
	}
	ev := PREvent{Type: prTypeStaging, Org: owner, Repo: repo, Commit: strings.ToLower(b.Commit)}
	// The run card and, on a product that asked for them, the create cards (D-103 §3.1).
	planned, existing, creates, res, err := s.store.PlanStagingCards(context.Background(), id, ev)
	switch {
	case err == nil && len(planned) == 0 && len(existing) == 0 && strings.HasPrefix(res.Skipped, skippedUnassigned):
		// A configuration state, not a transient one: 422, never the retryable 409 (D-102 §3.4).
		apiErr(w, http.StatusUnprocessableEntity, "unassigned", "no qa agent is assigned to this product")
		return
	case errors.Is(err, ErrPRSkipped):
		apiErr(w, http.StatusConflict, "capped", "the product holds too many open pull-request cards")
		return
	case err != nil:
		apiStoreErr(w, err)
		return
	}
	a.seen(id)
	switch {
	case len(planned) > 0:
		log.Printf("staging: planned %s on %s (%d create(s))", planned[0], id, creates)
		apiJSON(w, http.StatusAccepted, map[string]any{"accepted": true, "taskId": planned[0], "creates": creates})
	case len(existing) > 0:
		apiJSON(w, http.StatusOK, map[string]any{"accepted": true, "duplicate": true, "taskId": existing[0], "creates": 0})
	case res.Capped:
		// The run was refused by the cap; its creates (own cap) were kept (D-103 §3.1).
		apiJSON(w, http.StatusConflict, map[string]any{"error": "capped",
			"reason": "the product holds too many open pull-request cards", "creates": creates})
	default:
		apiErr(w, http.StatusInternalServerError, "internal", "nothing was planned")
	}
}
