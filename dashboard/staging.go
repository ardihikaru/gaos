package main

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"regexp"
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

// stagingAuth is the callback's credentials, read once at boot.
type stagingAuth struct {
	sums map[string][sha256.Size]byte // product id -> sha256("user:password")
	why  string                       // non-empty: the file was set and unusable

	mu       sync.Mutex
	fails    map[string]stagingFails
	lastSeen map[string]time.Time // product id -> the last accepted callback
	now      func() time.Time
}

type stagingFails struct {
	n     int
	until time.Time
	first time.Time
}

// The failure throttle: stagingMaxFails failures inside stagingWindow lock the
// source out for stagingLock.
const (
	stagingMaxFails = 5
	stagingWindow   = 5 * time.Minute
	stagingLock     = 5 * time.Minute
)

// stagingAuthFromEnv reads GAOS_STAGING_BASIC_AUTH_FILE. Unset means the
// callback is off, which is not an error.
func stagingAuthFromEnv() *stagingAuth {
	a := &stagingAuth{sums: map[string][sha256.Size]byte{}, fails: map[string]stagingFails{},
		lastSeen: map[string]time.Time{}, now: time.Now}
	path := strings.TrimSpace(os.Getenv("GAOS_STAGING_BASIC_AUTH_FILE"))
	if path == "" {
		return a
	}
	st, err := os.Stat(path)
	if err != nil || !st.Mode().IsRegular() {
		a.why = "the staging credential file cannot be read"
		return a
	}
	if st.Mode().Perm()&0o077 != 0 {
		a.why = "the staging credential file is readable by group or others; it must be mode 0600"
		return a
	}
	b, err := os.ReadFile(path)
	if err != nil {
		a.why = "the staging credential file cannot be read"
		return a
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 || parts[0] == "" || parts[1] == "" || len(parts[2]) < 12 {
			a.why = "the staging credential file wants lines of productId:user:password with a password of at least 12 characters"
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

// locked reports whether a source is locked out, and for how long.
func (a *stagingAuth) locked(src string) (time.Duration, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	f := a.fails[src]
	if left := f.until.Sub(a.now()); left > 0 {
		return left, true
	}
	return 0, false
}

// fail records a failure for a source.
func (a *stagingAuth) fail(src string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	f := a.fails[src]
	if f.first.IsZero() || now.Sub(f.first) > stagingWindow {
		f = stagingFails{first: now}
	}
	f.n++
	if f.n >= stagingMaxFails {
		f.until = now.Add(stagingLock)
	}
	a.fails[src] = f
	if len(a.fails) > 4096 { // bounded: forget the oldest sources
		for k, v := range a.fails {
			if now.Sub(v.first) > stagingWindow && v.until.Before(now) {
				delete(a.fails, k)
			}
		}
	}
}

func (a *stagingAuth) ok(src string) {
	a.mu.Lock()
	delete(a.fails, src)
	a.mu.Unlock()
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

// stagingSource is the address a failure is counted against: the peer, and the
// first X-Forwarded-For hop when the edge supplies one.
func stagingSource(r *http.Request) string {
	src := r.RemoteAddr
	if i := strings.LastIndex(src, ":"); i > 0 {
		src = src[:i]
	}
	if x := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-For"), ",")[0]); x != "" {
		src += "|" + x
	}
	return src
}

var shaRe = regexp.MustCompile(`^[0-9a-fA-F]{7,64}$`)

type stagingBody struct {
	Repo        string `json:"repo"`
	Branch      string `json:"branch"`
	Commit      string `json:"commit"`
	Environment string `json:"environment"`
}

// apiStaging is POST /v1/products/{id}/staging.
func (s *server) apiStaging(w http.ResponseWriter, r *http.Request) {
	if p := r.Header.Get("X-Forwarded-Prefix"); p != "" && p != "/api" {
		apiErr(w, http.StatusNotFound, "not-found", "no such route")
		return
	}
	a := s.staging
	if !a.enabled() {
		apiErr(w, http.StatusServiceUnavailable, "staging-off", "the staging callback is not configured")
		return
	}
	src := stagingSource(r)
	if left, locked := a.locked(src); locked {
		w.Header().Set("Retry-After", strconv.Itoa(int(left.Seconds())+1))
		apiErr(w, http.StatusTooManyRequests, "throttled", "too many failed attempts")
		return
	}
	user, pass, ok := r.BasicAuth()
	owners := map[string]bool{}
	if ok {
		owners = a.match(user, pass)
	}
	if !ok || len(owners) == 0 {
		a.fail(src)
		w.Header().Set("WWW-Authenticate", `Basic realm="gaos"`)
		apiErr(w, http.StatusUnauthorized, "unauthorized", "a credential is required")
		return
	}
	id := r.PathValue("id")
	if !owners[id] {
		// Valid, but for another product: the answer must not say which exist.
		a.fail(src)
		apiErr(w, http.StatusNotFound, "not-found", "no such product")
		return
	}
	a.ok(src)

	var b stagingBody
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&b); err != nil {
		apiErr(w, http.StatusUnprocessableEntity, "rejected", "the body must be {repo, branch, commit, environment}")
		return
	}
	owner, repo, okp := ghOwnerRepo(b.Repo)
	if !okp || !shaRe.MatchString(b.Commit) || (b.Environment != "" && b.Environment != "staging") {
		apiErr(w, http.StatusUnprocessableEntity, "rejected", "repo must be owner/repo, commit a hex sha, environment staging")
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
	planned, existing, _, err := s.store.PlanPullRequestCards(context.Background(), id, ev)
	switch {
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
		log.Printf("staging: planned %s on %s", planned[0], id)
		apiJSON(w, http.StatusAccepted, map[string]any{"accepted": true, "taskId": planned[0]})
	case len(existing) > 0:
		apiJSON(w, http.StatusOK, map[string]any{"accepted": true, "duplicate": true, "taskId": existing[0]})
	default:
		apiErr(w, http.StatusInternalServerError, "internal", "nothing was planned")
	}
}
