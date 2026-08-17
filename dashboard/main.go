package main

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// GAOS Mission Control -- the operator surface for a graph-native agent OS.
//
// This is pass 1: the full UI from artifacts/v1/gaos-mission-control.html,
// server-rendered from Go templates and swapped by HTMX, reading an in-memory
// store seeded from the prototype's fixtures. There is no ArcadeDB query in
// here yet, on purpose. The architecture document's own build order (§10) puts
// Mission Control at step 9 and warns: "Resist starting with the visual layer
// -- it is the most rewarding to build and the least load-bearing." What this
// pass buys is not the visual layer, it is a deployable unit: a container that
// goes through the tunnel, the edge and the strip, and stays up.
//
// House style is pustaka's, next door: stdlib only, no router library,
// http.NewServeMux, flag.NewFlagSet, distroless image. Read pustaka/main.go
// before adding a dependency here.

var started = time.Now()

func main() {
	fs := flag.NewFlagSet("dashboard", flag.ExitOnError)
	addr := fs.String("addr", envOr("GAOS_ADDR", ":8080"), "listen address")
	prod := fs.Bool("prod", false, "parse templates once and skip the filesystem watcher")
	sim := fs.Bool("sim", true, "run the floor simulation")
	seed := fs.Uint64("seed", 20260807, "simulation seed; pin it to reproduce a run")
	root := fs.String("root", ".", "in dev mode, read templates and static from here")
	if err := fs.Parse(os.Args[1:]); err != nil {
		log.Fatal(err)
	}

	if err := serve(*addr, !*prod, *sim, *seed, *root); err != nil {
		log.Fatal(err)
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

type server struct {
	store Store
	rend  *Renderer
	arc   *Arcade
}

// selectStore picks the implementation. GAOS_STORE=arcadedb is opt-in and
// explicit: the presence of a database URL is not consent to store anything in
// it, because /healthz has been probing a configured-but-unused ArcadeDB since
// this dashboard was first deployed.
//
// It FAILS THE BOOT rather than falling back. A dashboard asked for the graph
// store that quietly served fixtures instead would look completely healthy
// while losing every write, which is the exact failure mode the comment at the
// top of arcade.go was written about.
func selectStore(ctx context.Context, arc *Arcade, seed uint64) (Store, error) {
	if envOr("GAOS_STORE", "memory") != "arcadedb" {
		return NewMemStore(seed), nil
	}
	if !arc.Configured() {
		return nil, fmt.Errorf("GAOS_STORE=arcadedb but GAOS_ARCADE_URL is unset")
	}
	st, err := NewArcadeStore(ctx, arc, seed)
	if err != nil {
		return nil, fmt.Errorf("arcadedb store: %w", err)
	}
	return st, nil
}

func serve(addr string, dev, sim bool, seed uint64, root string) error {
	rend, err := NewRenderer(dev, root)
	if err != nil {
		return fmt.Errorf("templates: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	arc := ArcadeFromEnv()
	store, err := selectStore(ctx, arc, seed)
	if err != nil {
		return err
	}
	srv := &server{store: store, rend: rend, arc: arc}

	// The simulation belongs to MemStore and is not on the Store interface, so
	// it is asked for rather than assumed. ArcadeStore holds real state and has
	// nothing to simulate -- a floor that moved on its own would be writing
	// events nobody asked for, into a stream BR-04 says is the record.
	if sim {
		if r, ok := store.(interface{ RunSim(context.Context) }); ok {
			go r.RunSim(ctx)
		} else {
			sim = false
		}
	}

	h := gzipMiddleware(srv.routes())
	hs := &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		sc, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = hs.Shutdown(sc)
	}()

	mode := "dev"
	if !dev {
		mode = "prod"
	}
	log.Printf("gaos dashboard listening on %s (%s, sim=%v, store=%s, arcade=%s)",
		addr, mode, sim, store.Kind(), srv.arc.Describe())

	if err := hs.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()

	// Go 1.22 patterns: method and wildcards in the pattern itself, so there is
	// no router library and no switch on r.Method inside a handler.
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.Handle("GET /static/", s.staticHandler())

	// --- floor ---
	mux.HandleFunc("GET /ui/floor", s.handleFloor)
	mux.HandleFunc("GET /ui/rail", s.handleRail)
	mux.HandleFunc("GET /ui/inspector", s.handleInspector)
	mux.HandleFunc("GET /ui/desk/{idx}", s.handleSelectDesk)
	mux.HandleFunc("GET /ui/meeting", s.handleMeeting)
	mux.HandleFunc("GET /ui/timeline", s.handleTimeline)

	// --- the documented surface (architecture §09). Named now so nothing has
	// to be renamed when the graph is behind it. ---
	mux.HandleFunc("GET /events", s.handleEvents)
	mux.HandleFunc("GET /replay/{workflow}", s.handleReplay)
	mux.HandleFunc("POST /tasks/{id}/approve", s.handleApprove)
	mux.HandleFunc("POST /tasks/{id}/retry", s.handleRetry)
	mux.HandleFunc("POST /tasks/{id}/cancel", s.handleCancel)
	mux.HandleFunc("GET /tasks/{id}/context", s.handleTaskContext)

	// --- floor controls ---
	mux.HandleFunc("POST /ui/live", s.handleGoLive)
	mux.HandleFunc("POST /ui/pause", s.handlePause)
	mux.HandleFunc("POST /ui/speed/{n}", s.handleSpeed)
	mux.HandleFunc("POST /ui/bell", s.handleBell)
	mux.HandleFunc("POST /ui/desk/{idx}/review", s.handleCallReview)
	mux.HandleFunc("POST /ui/meeting/close", s.handleCloseMeeting)

	// --- back office ---
	mux.HandleFunc("POST /auth/login", s.handleLogin)
	mux.HandleFunc("POST /auth/logout", s.handleLogout)
	mux.HandleFunc("GET /ui/bo", s.handleBOSheet)
	mux.HandleFunc("GET /ui/bo/{view}", s.handleBOView)

	mux.HandleFunc("GET /products", s.handleProductList)
	mux.HandleFunc("POST /products", s.handleProductCreate)
	mux.HandleFunc("GET /products/{id}", s.handleProductDetail)
	mux.HandleFunc("PUT /products/{id}/brd", s.handleBRDSave)
	mux.HandleFunc("POST /products/{id}/stories", s.handleAddStory)
	mux.HandleFunc("POST /products/{id}/diagrams", s.handleAddDiagram)
	mux.HandleFunc("POST /products/{id}/knowledge", s.handleAddDecision)
	mux.HandleFunc("POST /products/{id}/inject", s.handleInject)
	mux.HandleFunc("GET /products/{id}/readiness", s.handleReadiness)

	// --- create workflow ---
	mux.HandleFunc("GET /ui/cw", s.handleCWOpen)
	mux.HandleFunc("POST /ui/cw/step/{n}", s.handleCWStep)
	mux.HandleFunc("POST /workflows", s.handleWorkflowCreate)

	return mux
}

// staticHandler serves the embedded assets. Fingerprinting is pass 2; for now
// the CSS, the fonts and htmx get a week of immutable caching because they only
// change when the image changes, and the image is the deploy unit.
func (s *server) staticHandler() http.Handler {
	sub, err := fs.Sub(assets, "static")
	if err != nil {
		log.Fatalf("static: %v", err)
	}
	fileSrv := http.FileServerFS(sub)
	return http.StripPrefix("/static/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=604800")
		fileSrv.ServeHTTP(w, r)
	}))
}

// handleHealth is what every deployment check curls.
//
// Mounted behind Traefik's /api strip it answers at
// https://gaos.mfardiansyah.id/api/healthz -- see deploy/traefik/dynamic.yml.
//
// THE PROBE MUST NOT GATE STARTUP. cubiq's `server` exits when its database is
// missing, which is right for a service that cannot answer a single request
// without one. This dashboard renders fixtures; with ArcadeDB down it is
// degraded, not broken, and a crash loop would take the whole UI away for a
// database it does not read yet.
func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	w.Header().Set("Cache-Control", "no-store")
	body := map[string]any{
		"ok":      true,
		"service": "gaos-dashboard",
		"store":   s.store.Kind(),
		"uptime":  time.Since(started).Round(time.Second).String(),
	}

	db, err := s.arc.Probe(ctx)
	body["database"] = db
	if err != nil {
		body["error"] = err.Error()
	}
	// A configured-but-unreachable database is a 503: the deploy check is
	// meant to catch a wrong network name or a wrong password, and those are
	// exactly the failures that otherwise look healthy. An UNCONFIGURED
	// database is a 200 -- that is the local stack, and it is a valid state.
	if err != nil && s.arc.Configured() {
		body["ok"] = false
		jsonStatus(w, http.StatusServiceUnavailable, body)
		return
	}
	jsonOut(w, body)
}

// ---------- shared view assembly ----------

// newView builds the render envelope from one consistent read of the store.
func (s *server) newView(r *http.Request) (*View, Floor) {
	f, _ := s.store.Snapshot(r.Context())
	v := &View{
		Prefix:   sitePrefix(r),
		Stats:    f.Stats,
		FocusIdx: f.FocusIdx,
		SignedIn: signedIn(r),
		Tenant:   tenantOf(r),
	}
	for i, d := range f.Desks {
		v.Floor = append(v.Floor, NewDeskView(d, i, f.FocusIdx))
		v.Products = append(v.Products, ProdRowView{
			Idx: i, Name: d.Name, Score: scoreText(d),
			DotStyle: cssVar("background", stateColor(d.State)),
			Current:  i == f.FocusIdx,
		})
	}
	for _, rm := range f.Rooms {
		v.Rooms = append(v.Rooms, NewRoomView(rm))
	}
	v.Meeting = NewMeetingView(f.Meeting, f.MeetDesk)

	if f.FocusIdx < len(f.Desks) {
		ctx, _ := s.store.ContextSlice(r.Context(), f.FocusIdx)
		v.Insp = NewInspectorView(f.Desks[f.FocusIdx], f.FocusIdx, ctx)
	}

	evs, _ := s.store.Events(r.Context())
	for i, e := range evs {
		v.Events = append(v.Events, NewEventView(e, i, f.Stats.Replay && f.Stats.ReplayAt == i))
	}
	return v, f
}

func scoreText(d Desk) string {
	if !d.Scored {
		return "—"
	}
	return fmt.Sprint(d.Score)
}

// applyResult folds a mutation's Result into a fresh view: the new events go
// into NewEvents for the beforeend swap, and the toast into the chrome.
func (s *server) applyResult(r *http.Request, res Result) *View {
	v, _ := s.newView(r)
	v.Toast = res.Toast
	base := len(v.Events) - len(res.NewEvents)
	for i := range res.NewEvents {
		idx := base + i
		if idx < 0 || idx >= len(v.Events) {
			continue
		}
		v.NewEvents = append(v.NewEvents, v.Events[idx])
	}
	return v
}

// fail turns a store rejection into a toast rather than a stack trace. The
// architecture document's rule is that a mutation returns the new state or a
// reason -- this is the reason half, rendered.
func (s *server) fail(w http.ResponseWriter, r *http.Request, err error) {
	v, _ := s.newView(r)
	v.Toast = err.Error()
	code := http.StatusUnprocessableEntity
	if err == ErrNotFound {
		code = http.StatusNotFound
		v.Toast = "That record is gone."
	}
	w.WriteHeader(code)
	s.rend.RenderWithChrome(w, "empty", v)
}

// ---------- tiny request helpers ----------

func atoiDefault(s string, def int) int {
	n, sign, any := 0, 1, false
	for i, c := range s {
		if i == 0 && c == '-' {
			sign = -1
			continue
		}
		if c < '0' || c > '9' {
			return def
		}
		n = n*10 + int(c-'0')
		any = true
	}
	if !any {
		return def
	}
	return n * sign
}

func splitLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(l); t != "" {
			out = append(out, t)
		}
	}
	return out
}
