package main

import (
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
	"sync/atomic"
	"time"
)

// The machine API (D-072, BR-52, BR-53). Bearer-token authenticated, fail
// closed, product-scoped for the QA token. It exists because the back office's
// cookie gate accepts any request that sets `gaos_bo=1` (handlers_bo.go) and so
// cannot protect a route a machine calls.
//
// PATHS. Traefik routes PathPrefix(/api) to this server and STRIPS it
// (deploy/traefik/dynamic.yml), so a public https://<host>/api/v1/products
// arrives here as /v1/products. The mux registers /v1/...; the public base is
// /api/v1. A request that arrives through /app (X-Forwarded-Prefix set) is a
// 404, so an edge control keyed to /api cannot be walked around.
//
// The results and catalog handlers call s.store directly, not s.mut(r): a QA
// write carries no floor (qacatalog_store.go), so a parked replay has no reason
// to block it and the API does not depend on a cursor an unauthenticated GET
// can set. The seed handler, which touches the floor, does use s.mut (seed.go).

type apiScope int

const (
	scopeNone apiScope = iota
	scopeQA
	scopeAdmin
)

const (
	apiMaxBody     = 1 << 20 // 1 MiB
	apiTokenPrefix = "gaos_"
	apiTokenMinLen = 37 // the prefix plus 32 characters
)

// apiConfig is read once at boot. It holds digests, never the tokens.
type apiConfig struct {
	adminSum, qaSum [sha256.Size]byte
	hasAdmin, hasQA bool
	qaProducts      map[string]bool
	why             string // why the API is disabled, "" when it is not

	// One write in flight per principal: each write holds the store's one lock
	// for its whole transaction, so a looping client must not queue behind
	// itself.
	adminBusy, qaBusy atomic.Bool
}

func validToken(t string) bool {
	if len(t) < apiTokenMinLen || !strings.HasPrefix(t, apiTokenPrefix) {
		return false
	}
	// The same alphabet as the egress detector (gaos_[0-9A-Za-z]{32,}), so a
	// token this accepts is one the secret screens can see.
	for _, r := range t[len(apiTokenPrefix):] {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return false
		}
	}
	return true
}

// readTokenFile reads one secret file. An empty file is the way to disable a
// token; an unreadable or malformed one counts as unset and is logged by name,
// never by content.
func readTokenFile(envName string) string {
	path := os.Getenv(envName)
	if path == "" {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		log.Printf("api: %s: %v (treated as unset)", envName, err)
		return ""
	}
	t := strings.TrimSuffix(strings.TrimSuffix(string(b), "\n"), "\r")
	if t == "" {
		return ""
	}
	if !validToken(t) {
		log.Printf("api: %s does not hold a token of the form %s<32+ chars> (treated as unset)", envName, apiTokenPrefix)
		return ""
	}
	return t
}

// newAPIConfig builds the configuration from the three tokens and the QA
// product list. Two equal tokens disable the whole API: the QA tools holding
// admin rights, or replaying GAOS's own outbound credential back at it, is the
// failure this refuses.
func newAPIConfig(admin, qa, out, products string) *apiConfig {
	c := &apiConfig{qaProducts: map[string]bool{}}
	for _, id := range strings.Split(products, ",") {
		if id = strings.TrimSpace(id); id != "" {
			c.qaProducts[id] = true
		}
	}
	eq := func(a, b string) bool { return a != "" && a == b }
	if eq(admin, qa) || eq(admin, out) || eq(qa, out) {
		c.why = "two of the admin, QA and outbound tokens are equal"
		return c
	}
	if admin != "" {
		c.adminSum, c.hasAdmin = sha256.Sum256([]byte(admin)), true
	}
	if qa != "" {
		c.qaSum, c.hasQA = sha256.Sum256([]byte(qa)), true
	}
	if !c.hasAdmin && !c.hasQA {
		c.why = "no API token is configured"
	}
	return c
}

func apiFromEnv() *apiConfig {
	c := newAPIConfig(readTokenFile("GAOS_API_ADMIN_TOKEN_FILE"), readTokenFile("GAOS_API_QA_TOKEN_FILE"),
		readTokenFile("GAOS_QA_TOKEN_FILE"), os.Getenv("GAOS_API_QA_PRODUCTS"))
	log.Printf("api: %s", c.Describe())
	return c
}

// Describe is the boot line. It names what is configured and never a token.
func (c *apiConfig) Describe() string {
	if c.why != "" {
		return "disabled (" + c.why + ")"
	}
	return "enabled (admin=" + strconv.FormatBool(c.hasAdmin) + " qa=" + strconv.FormatBool(c.hasQA) +
		" qa-products=" + strconv.Itoa(len(c.qaProducts)) + ")"
}

// authorize says who the bearer is. The comparison hashes both sides first so
// its time does not depend on how much of a guess was right.
func (c *apiConfig) authorize(r *http.Request) apiScope {
	h := r.Header.Get("Authorization")
	const p = "Bearer "
	if len(h) <= len(p) || !strings.EqualFold(h[:len(p)], p) {
		return scopeNone
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(h[len(p):])))
	if c.hasAdmin && subtle.ConstantTimeCompare(sum[:], c.adminSum[:]) == 1 {
		return scopeAdmin
	}
	if c.hasQA && subtle.ConstantTimeCompare(sum[:], c.qaSum[:]) == 1 {
		return scopeQA
	}
	return scopeNone
}

// ---------- answers ----------

func apiJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func apiErr(w http.ResponseWriter, status int, code, reason string) {
	apiJSON(w, status, map[string]string{"error": code, "reason": reason})
}

// apiGate answers for the request or reports the caller's scope. need is the
// least scope the route accepts.
func (s *server) apiGate(w http.ResponseWriter, r *http.Request, need apiScope) (apiScope, bool) {
	// Traefik's stripPrefix sets X-Forwarded-Prefix to the prefix it removed: /api
	// is THE public path of this API and must pass; /app (Mission Control's mount)
	// and anything else is refused, so an edge control keyed to /api cannot be
	// walked around through another mount.
	if p := r.Header.Get("X-Forwarded-Prefix"); p != "" && p != "/api" {
		apiErr(w, http.StatusNotFound, "not-found", "no such route")
		return scopeNone, false
	}
	if s.api == nil || s.api.why != "" {
		apiErr(w, http.StatusServiceUnavailable, "api-disabled", "the machine API is not configured")
		return scopeNone, false
	}
	got := s.api.authorize(r)
	switch {
	case got == scopeNone:
		w.Header().Set("WWW-Authenticate", `Bearer realm="gaos"`)
		apiErr(w, http.StatusUnauthorized, "unauthorized", "a bearer token is required")
		return scopeNone, false
	case got < need:
		apiErr(w, http.StatusForbidden, "forbidden", "this token may not call this route")
		return scopeNone, false
	}
	return got, true
}

// apiProduct resolves {id} for the caller. The QA token reaches only the
// products it is scoped to, and any other id is a 404, not a 403: the answer
// must not say which products exist. Unset scope reaches nothing.
func (s *server) apiProduct(w http.ResponseWriter, r *http.Request, scope apiScope) (string, bool) {
	id := r.PathValue("id")
	if scope == scopeQA && !s.api.qaProducts[id] {
		apiErr(w, http.StatusNotFound, "not-found", "no such product")
		return "", false
	}
	ps, err := s.store.Products(r.Context())
	if err == nil {
		for _, p := range ps {
			if p.ID == id {
				return id, true
			}
		}
	}
	apiErr(w, http.StatusNotFound, "not-found", "no such product")
	return "", false
}

// beginWrite takes the principal's single write slot. Authentication has
// already happened, so an unauthenticated caller cannot hold it, and nothing is
// read from the body before it is taken.
func (s *server) beginWrite(w http.ResponseWriter, scope apiScope) (release func(), ok bool) {
	flag := &s.api.qaBusy
	if scope == scopeAdmin {
		flag = &s.api.adminBusy
	}
	if !flag.CompareAndSwap(false, true) {
		w.Header().Set("Retry-After", "2")
		apiErr(w, http.StatusTooManyRequests, "write-in-flight", "another write by this token is still running")
		return nil, false
	}
	return func() { flag.Store(false) }, true
}

// apiDecode reads one strictly-typed JSON body of at most apiMaxBody.
func apiDecode(w http.ResponseWriter, r *http.Request, v any) bool {
	// A slow client must not hold the principal's one write slot: the body has
	// 30 seconds (the server sets only a header timeout).
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(30 * time.Second))
	err := decodeStrict(http.MaxBytesReader(w, r.Body, apiMaxBody), v)
	if err == nil {
		return true
	}
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		apiErr(w, http.StatusRequestEntityTooLarge, "too-large", "the body exceeds 1 MiB")
		return false
	}
	apiErr(w, http.StatusBadRequest, "bad-json", "the body is not the JSON this route takes: "+qaTrim(err.Error(), 120))
	return false
}

func apiStoreErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		apiErr(w, http.StatusNotFound, "not-found", "no such product")
	case errors.Is(err, ErrRejected):
		apiErr(w, http.StatusUnprocessableEntity, "rejected", err.Error())
	default:
		log.Printf("api: store error: %v", err)
		apiErr(w, http.StatusInternalServerError, "internal", "the store could not complete the request")
	}
}

// ---------- routes ----------

func (s *server) registerAPI(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/products", s.apiProducts)
	mux.HandleFunc("GET /v1/products/{id}/qa/summary", s.apiQaSummary)
	mux.HandleFunc("GET /v1/products/{id}/qa/cases", s.apiQaCases)
	mux.HandleFunc("POST /v1/products/{id}/qa/cases", s.apiQaCreate)
	mux.HandleFunc("POST /v1/products/{id}/qa/results", s.apiQaResults)
	mux.HandleFunc("POST /v1/products/{id}/qa/catalog", s.apiQaCatalog)
	mux.HandleFunc("POST /v1/seed/product", s.apiSeedProduct)
}

type apiProductOut struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Repo string `json:"repo"`
}

func (s *server) apiProducts(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.apiGate(w, r, scopeQA)
	if !ok {
		return
	}
	ps, err := s.store.Products(r.Context())
	if err != nil {
		apiStoreErr(w, err)
		return
	}
	out := []apiProductOut{}
	for _, p := range ps {
		if scope == scopeQA && !s.api.qaProducts[p.ID] {
			continue
		}
		out = append(out, apiProductOut{ID: p.ID, Name: p.Name, Repo: p.Repo})
	}
	apiJSON(w, http.StatusOK, map[string]any{"products": out})
}

func (s *server) apiQaSummary(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.apiGate(w, r, scopeQA)
	if !ok {
		return
	}
	id, ok := s.apiProduct(w, r, scope)
	if !ok {
		return
	}
	sum, err := s.store.QaSummary(r.Context(), id)
	if err != nil {
		apiStoreErr(w, err)
		return
	}
	apiJSON(w, http.StatusOK, sum)
}

var apiEpicParam = regexp.MustCompile(`^[A-Z]{2,8}$`)

func (s *server) apiQaCases(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.apiGate(w, r, scopeQA)
	if !ok {
		return
	}
	id, ok := s.apiProduct(w, r, scope)
	if !ok {
		return
	}
	q := r.URL.Query()
	query := QaQuery{Limit: 100}
	if v := q.Get("after"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			apiErr(w, http.StatusBadRequest, "bad-query", "after is a non-negative integer")
			return
		}
		query.After = n
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > qaMaxItems {
			apiErr(w, http.StatusBadRequest, "bad-query", "limit is 1 to "+strconv.Itoa(qaMaxItems))
			return
		}
		query.Limit = n
	}
	if v := q.Get("epic"); v != "" {
		if !apiEpicParam.MatchString(v) {
			apiErr(w, http.StatusBadRequest, "bad-query", "epic is 2 to 8 capital letters")
			return
		}
		query.Epic = v
	}
	page, err := s.store.QaCases(r.Context(), id, query)
	if err != nil {
		apiStoreErr(w, err)
		return
	}
	apiJSON(w, http.StatusOK, page)
}

type apiResultsBody struct {
	Source string         `json:"source"`
	Force  bool           `json:"force"`
	Items  []QaResultItem `json:"items"`
}

func (s *server) apiQaResults(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.apiGate(w, r, scopeQA)
	if !ok {
		return
	}
	id, ok := s.apiProduct(w, r, scope)
	if !ok {
		return
	}
	release, ok := s.beginWrite(w, scope)
	if !ok {
		return
	}
	defer release()
	var body apiResultsBody
	if !apiDecode(w, r, &body) {
		return
	}
	if len(body.Items) > qaMaxItems {
		apiErr(w, http.StatusRequestEntityTooLarge, "too-large", "a batch holds at most "+strconv.Itoa(qaMaxItems)+" items")
		return
	}
	if byteLen(body.Source) > 80 || !qaPrintable.MatchString(body.Source) {
		apiErr(w, http.StatusBadRequest, "bad-source", "source is short printable text")
		return
	}
	if body.Force && scope != scopeAdmin {
		apiErr(w, http.StatusForbidden, "forbidden", "force needs the admin token")
		return
	}
	out, _, err := s.store.QaApplyResults(r.Context(), id, QaResultsIn{Source: body.Source, Force: body.Force, Items: body.Items})
	if err != nil {
		apiStoreErr(w, err)
		return
	}
	apiJSON(w, http.StatusOK, out)
}

type apiCatalogBody struct {
	Seed    bool       `json:"seed"`
	Modules []QaModule `json:"modules"`
	Epics   []QaEpic   `json:"epics"`
	Cases   []QaCase   `json:"cases"`
}

func (s *server) apiQaCatalog(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.apiGate(w, r, scopeAdmin)
	if !ok {
		return
	}
	id, ok := s.apiProduct(w, r, scope)
	if !ok {
		return
	}
	release, ok := s.beginWrite(w, scope)
	if !ok {
		return
	}
	defer release()
	var body apiCatalogBody
	if !apiDecode(w, r, &body) {
		return
	}
	if len(body.Modules)+len(body.Epics)+len(body.Cases) > qaMaxItems {
		apiErr(w, http.StatusRequestEntityTooLarge, "too-large", "a batch holds at most "+strconv.Itoa(qaMaxItems)+" items")
		return
	}
	out, _, err := s.store.QaImport(r.Context(), id, QaImportIn{Seed: body.Seed, Modules: body.Modules, Epics: body.Epics, Cases: body.Cases})
	if err != nil {
		apiStoreErr(w, err)
		return
	}
	apiJSON(w, http.StatusOK, out)
}

// apiQaCreate is the QA tools' create-only door (design §4.12). Every string of
// the request is screened for secrets before anything is written; the store
// cleans the text it keeps.
func (s *server) apiQaCreate(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.apiGate(w, r, scopeQA)
	if !ok {
		return
	}
	id, ok := s.apiProduct(w, r, scope)
	if !ok {
		return
	}
	release, ok := s.beginWrite(w, scope)
	if !ok {
		return
	}
	defer release()
	var body QaCreateIn
	if !apiDecode(w, r, &body) {
		return
	}
	if len(body.Cases) > qaMaxItems || len(body.Epics) > qaMaxItems {
		apiErr(w, http.StatusRequestEntityTooLarge, "too-large", "a batch holds at most "+strconv.Itoa(qaMaxItems)+" cases")
		return
	}
	if byteLen(body.Source) > 80 || !qaPrintable.MatchString(body.Source) {
		apiErr(w, http.StatusBadRequest, "bad-source", "source is short printable text")
		return
	}
	raw, _ := json.Marshal(body)
	var tree any
	_ = json.Unmarshal(raw, &tree)
	if hit := secretInStrings(tree); hit != "" {
		apiErr(w, http.StatusBadRequest, "secret-in-body", "the body matches the secret detector "+hit+"; nothing was written")
		return
	}
	out, _, err := s.store.QaCreate(r.Context(), id, body)
	if err != nil {
		apiStoreErr(w, err)
		return
	}
	apiJSON(w, http.StatusOK, out)
}
