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

	// adapter is nil unless GAOS_MODEL_MODE names one. Held for /healthz and
	// the boot log only -- the dispatch path reaches it through the store,
	// because a dispatch emits events and only a store may.
	adapter Adapter
}

// describeAdapter is what /healthz and the boot line report.
func (s *server) describeAdapter() string {
	if s.adapter == nil {
		return "unconfigured"
	}
	return fmt.Sprintf("%s (%s)", s.adapter.Name(), s.adapter.Mode())
}

// mut is the handle a mutating handler must use. While the floor is parked in
// the past it is a readOnly, and a readOnly refuses.
//
// This is T-099 at the point of use. Architecture §08 asks for replay to be
// routed through a read-only handle "rather than a flag someone can forget to
// check"; the way to make that true is for the handle a handler reaches for to
// be a different handle, chosen once, here. A new mutating route added next year
// gets the protection by calling s.mut() like its neighbours, and gets no
// protection at all by calling s.store -- which is why every one of them calls
// this and reads do not.
//
// It costs one Snapshot read per mutation. That read was already happening in
// every one of these handlers' render path, and correctness that depends on
// nobody forgetting is not correctness.
func (s *server) mut(r *http.Request) Store {
	f, err := s.store.Snapshot(r.Context())
	if err != nil || !f.Stats.Replay {
		return s.store
	}
	return readOnly{s.store}
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
	// What the schema apply did, said out loud once.
	//
	// HERE and not in the constructor, which is the whole reason this is three
	// lines rather than one. NewArcadeStoreFor runs per store, and the
	// conformance suite builds one per case against its own tenant -- logging
	// there would print forty-odd lines of schema arithmetic into every test run
	// and teach everyone to skim past it. serve() runs once per process.
	//
	// The TOTAL first, because that is the number that answers a real question:
	// does the binary now running carry the DDL you think it does. It moved from
	// 323 to 324 when M4.5 added ContextSlice's identity index, and an operator
	// reading 324 here knows the deploy carried it.
	//
	// The breakdown is deliberately worded "accepted", not "applied". See
	// ApplySchema: a re-run against a fully populated database still reports 128
	// accepted, because most of the DDL is IF NOT EXISTS and the server honours
	// that silently instead of raising AlreadyExists. These counts cannot say
	// whether anything CHANGED, and a line that implied they could would send
	// somebody looking for a migration that never happened.
	if sc, ok := store.(interface{ SchemaCounts() (int, int) }); ok {
		accepted, present := sc.SchemaCounts()
		log.Printf("schema: %d statements from the embedded DDL, none failed (%d accepted, %d already declared)",
			accepted+present, accepted, present)
	}
	// The model gateway. T-057, and D-049's standing obligation: "the pin is
	// worthless without the boot smoke test §7.6.3 already promises."
	//
	// selectAdapter FAILS THE BOOT rather than falling back, for exactly the
	// reason selectStore does above. An UNCONFIGURED adapter is nil and is a
	// valid state -- that is the local stack, and handleHealth has always
	// treated an unconfigured dependency as healthy. A CONFIGURED adapter whose
	// binary is missing, or whose version has drifted out of the pin, is a
	// dashboard that would accept a dispatch and produce evidence nobody can
	// trust. That is worse than a dashboard that did not start, because it
	// looks completely healthy while doing it.
	adapter, err := selectAdapter(ModelConfigFromEnv())
	if err != nil {
		return err
	}
	if adapter != nil {
		if err := adapter.Smoke(ctx); err != nil {
			return fmt.Errorf("boot smoke test: %w", err)
		}
		if sa, ok := store.(interface{ SetAdapter(Adapter) }); ok {
			sa.SetAdapter(adapter)
		}
		// Said out loud at boot, because the two switches are easy to conflate
		// and the difference between them is money. An adapter that passed its
		// smoke test CAN talk to a provider; GAOS_RUNNER is what says it MAY. An
		// operator who set one and meant both would otherwise find out from a
		// dispatch that quietly did nothing.
		if runnerEnabled() {
			log.Printf("runner on: a cleared dispatch spawns %s and spends against it", adapter.Name())
		} else {
			log.Printf("runner off (GAOS_RUNNER): a dispatch screens and records, and stops there")
		}
	}

	// Boot reconciliation. T-053, and D-4 is unusually plain about its standing:
	// "this is the whole of recovery; there is no other path."
	//
	// HERE, and not in a goroutine, and not after the listener. Because execution
	// does not die with orchestration (D-2), a restart finds tasks that claim to
	// be running -- and a request served before this ran would see a floor that
	// is lying about what is in flight.
	//
	// Running before the listener is also what makes the read-only replay handle
	// irrelevant on this path: no operator can be in replay yet, because no
	// operator can have reached the process.
	//
	// A failure FAILS THE BOOT, for the same reason selectStore and
	// selectAdapter do. A dashboard that could not work out what was running,
	// then served a floor as if nothing were, looks completely healthy while a
	// detached process edits a repository nobody is watching.
	if rc, ok := store.(interface {
		Reconcile(context.Context) (Result, error)
	}); ok {
		res, err := rc.Reconcile(ctx)
		if err != nil {
			return fmt.Errorf("boot reconciliation: %w", err)
		}
		if n := len(res.NewEvents); n > 0 {
			log.Printf("boot reconciliation: %d claimed run(s) adopted, requeued or orphaned", n)
		}
	}

	// The control loop. One pass asks the scheduler what may start and starts it;
	// the pass itself is where the clock is read, so Schedule stays a pure
	// function of what it is handed (D-1).
	//
	// Behind both switches, same as the dispatch route: an adapter says a
	// provider CAN be reached, GAOS_RUNNER says it MAY be. Without them the loop
	// would still produce a correct decision and nothing to do with it, so it
	// simply does not run.
	if adapter != nil && runnerEnabled() {
		if r, ok := store.(interface {
			RunOnce(context.Context, Adapter) ScheduleDecision
		}); ok {
			go func() {
				t := time.NewTicker(schedulerTick)
				defer t.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-t.C:
						r.RunOnce(ctx, adapter)
					}
				}
			}()
		}
	}

	srv := &server{store: store, rend: rend, arc: arc, adapter: adapter}

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
	log.Printf("gaos dashboard listening on %s (%s, sim=%v, store=%s, arcade=%s, adapter=%s)",
		addr, mode, sim, store.Kind(), srv.arc.Describe(), srv.describeAdapter())

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
	mux.HandleFunc("POST /tasks/{id}/dispatch", s.handleDispatch)
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
		"adapter": s.describeAdapter(),
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
		ctx, _ := s.store.TaskContext(r.Context(), f.FocusIdx)
		v.Insp = NewInspectorView(f.Desks[f.FocusIdx], f.FocusIdx, ctx)
	}

	// Addressed by Seq, not by position. The stream is unbounded now (BR-04), so
	// a pill's position is a function of how much has happened since -- and a
	// timeline whose links renumber themselves under the operator is a timeline
	// that sends them to the wrong moment.
	evs, _ := s.store.Events(r.Context())
	for _, e := range evs {
		v.Events = append(v.Events, NewEventView(e, e.Seq, f.Stats.Replay && f.Stats.ReplayAt == e.Seq))
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
