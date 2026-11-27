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
	"sync"
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
	if len(os.Args) > 1 && os.Args[1] == "hash-password" {
		if err := runHashPassword(os.Stdin, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "hash-password:", err)
			os.Exit(1)
		}
		return
	}
	fs := flag.NewFlagSet("dashboard", flag.ExitOnError)
	addr := fs.String("addr", envOr("GAOS_ADDR", ":8080"), "listen address")
	prod := fs.Bool("prod", false, "parse templates once and skip the filesystem watcher")
	seed := fs.Uint64("seed", 20260807, "fixture seed; pin it to reproduce a store")
	root := fs.String("root", ".", "in dev mode, read templates and static from here")
	if err := fs.Parse(os.Args[1:]); err != nil {
		log.Fatal(err)
	}

	if err := serve(*addr, !*prod, *seed, *root); err != nil {
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
	// staging is the Jenkins callback's credentials (staging.go); never nil.
	staging *stagingAuth
	// ghState is the GitHub client's row on the Integrations view.
	ghState string
	// codeSrc reads application source for a defect's packet (D-091); nil when
	// the GitHub client is off.
	codeSrc codeSource
	// branches answers "does this branch exist" for the retarget route (D-092); nil when GitHub is off.
	branches branchChecker
	// writeTokenKind is the kind of the auto-fix write token in use ("" = none).
	writeTokenKind string
	// fixPoll is the pull-request poller, for the Auto-fix view's GitHub status; nil when off.
	fixPoll fixPollSource
	// wa is the WhatsApp notifier (wa.go); nil when off.
	wa   *waNotifier
	auth *authority // back-office sessions (auth.go); nil signs nobody in
	// patterns and bearer are what routes() registered, for the route-table test.
	// routesMu guards them: production calls routes() once, but the tests build
	// a handler per request, and two of those in flight wrote these together.
	routesMu sync.Mutex
	patterns []string
	bearer   map[string]bool
	store    Store
	rend     *Renderer
	arc      *Arcade

	// api is the machine API's configuration (api_v1.go); nil in tests that do
	// not exercise it, which is the same as disabled.
	api *apiConfig

	// qaOut is the client for GAOS calling the QA tools (qaclient.go); nil or
	// unconfigured means the sync buttons are disabled.
	qaOut *qaOutbound
	// flow is the pull-request flow; the task API prepares a pull request
	// through it and fills its context cache (D-099).
	flow *prFlow
	// nt sends the manual-review notices of an operator's relabel (D-101); nil sends nothing.
	nt *notifier
	// tm is the TaskMgr link (D-104, taskmgr.go); nil in tests that do not set it.
	tm *taskmgrLink

	// inbox delivers the operator's answers to the QA tool's questions
	// (inbox_worker.go); nil in tests that do not exercise it.
	inbox *inboxWorker

	// brain is the Brain Town emitter (braintown.go); nil when off. brainOff is the row shown then.
	brain    *brainTown
	brainOff BrainTownRow
	// btConsumer is the Brain Town reader (braintown_consumer.go); nil when off.
	btConsumer    *btConsumer
	btConsumerOff BrainTownRow

	// adapter is nil unless GAOS_MODEL_MODE names one. Held for /healthz and
	// the boot log only -- the dispatch path reaches it through the store,
	// because a dispatch emits events and only a store may.
	adapter Adapter

	// bootChecked is when the adapter's boot smoke test (GET /v1/providers)
	// passed; zero with no adapter. Shown by the Integrations view (D-085).
	bootChecked time.Time
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

func serve(addr string, dev bool, seed uint64, root string) error {
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
	var bootChecked time.Time // D-085: when the boot provider check passed
	adapter, err := selectAdapter(ModelConfigFromEnv())
	if err != nil {
		return err
	}
	if adapter != nil {
		if err := adapter.Smoke(ctx); err != nil {
			return fmt.Errorf("boot smoke test: %w", err)
		}
		bootChecked = time.Now()
		if sa, ok := store.(interface{ SetAdapter(Adapter) }); ok {
			sa.SetAdapter(adapter)
		}
		// Said out loud at boot, because the two switches are easy to conflate
		// and the difference between them is money. An adapter that passed its
		// smoke test CAN talk to a provider; GAOS_RUNNER is what says it MAY. An
		// operator who set one and meant both would otherwise find out from a
		// dispatch that quietly did nothing.
		switch runnerMode() {
		case RunnerOn:
			log.Printf("runner on: a cleared dispatch spawns %s and spends against it", adapter.Name())
		case RunnerManual:
			log.Printf("runner manual (GAOS_RUNNER): the scheduler readies work; only a board move ready -> working spawns %s and spends against it", adapter.Name())
		default:
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
	// D-086: the Brain Town emitter. Off by default. The commit tap is registered BEFORE
	// the reconcile below so boot-time events are seen; the sender starts with the inbox worker.
	api := apiFromEnv()
	btCfg := btConfigFromEnv(api)
	var brain *brainTown
	brainOff := btState(btCfg)
	if btCfg.enabled && btCfg.why != "" {
		log.Printf("brain town: off (%s)", btCfg.why)
	}
	if btCfg.enabled && btCfg.why == "" {
		if cf, ok := store.(commitFeed); ok {
			brain = newBrainTown(btCfg, store)
			cf.SetCommitTap(brain.Tap)
			log.Printf("brain town: sending to %s", urlHost(btCfg.url.String()))
		} else {
			brainOff = BrainTownRow{State: "unusable: this store has no commit feed"}
		}
	}

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
	// simply does not run. Under GAOS_RUNNER=manual it runs, and only readies
	// work (D-080): RunOnce never dispatches in that mode.
	if adapter != nil && runnerMode() != RunnerOff {
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

	// D-066: a question closes with the session that asked it. Swept once a
	// minute whatever the runner switch says -- expiring spends nothing, and a
	// task left waiting on a question nobody can answer any more is a lie.
	go func() {
		t := time.NewTicker(questionSweep)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-t.C:
				if res, err := store.ExpireQuestions(ctx, now.Unix()); err != nil {
					log.Printf("question sweep: %v", err)
				} else if n := len(res.NewEvents); n > 0 {
					log.Printf("question sweep: %d question(s) expired with their session", n)
				}
			}
		}
	}()

	// The Brain Town reader: one goroutine that pulls the town's pull-request
	// events. It is separate from the emitter above, which pushes GAOS's own
	// changes out.
	var btCons *btConsumer
	btConsOff := BrainTownRow{State: "disabled"}
	if cc := btConsumerFromEnv(api); !cc.enabled {
		log.Printf("brain town: consumer off")
	} else if cc.why != "" {
		btConsOff = BrainTownRow{State: "unusable: " + cc.why}
		log.Printf("brain town: consumer unusable (%s)", cc.why)
	} else {
		btCons = newBTConsumer(cc, store)
		btConsOff = BrainTownRow{State: "enabled"}
		log.Printf("brain town: reading from %s", urlHost(cc.url.String()))
	}

	srv := &server{store: store, rend: rend, arc: arc, adapter: adapter, api: api, brain: brain, brainOff: brainOff,
		btConsumer: btCons, btConsumerOff: btConsOff, qaOut: qaOutboundFromEnv(),
		auth: authFromEnv(!dev), bootChecked: bootChecked, staging: stagingAuthFromEnv()}

	// D-078: the answer webhook. One worker; with the outbound unconfigured it
	// idles and the QA tool polls GET /v1/products/{id}/inbox/{externalId}.
	srv.inbox = newInboxWorker(store, srv.qaOut)
	go srv.inbox.Run(ctx)
	if brain != nil {
		go brain.Run(ctx)
	}
	// The pull-request flow's outside calls (D-088): GitHub, WhatsApp and the
	// seams the runner reads. They do not depend on the reader being on: the
	// staging callback uses them too.
	gcfg := ghConfigFromEnv()
	ghc := newGHClient(gcfg)
	flow := &prFlow{st: store, ctx: &prContextCache{}, logf: log.Printf}
	srv.flow = flow
	var authors authorSource
	if ghc != nil {
		flow.gh = ghc
		authors = ghc
		srv.codeSrc = ghc
		srv.branches = ghc
		srv.ghState = "on"
		log.Printf("pull requests: GitHub client on")
	} else {
		srv.ghState = "off (" + gcfg.why + ")"
		log.Printf("pull requests: GitHub client off (%s); no fork check, no review post", gcfg.why)
	}
	// The auto-fix's write token: its own file, its own client (D-091).
	wtcfg := ghWriteConfigFromEnv()
	ghw := newGHWriter(wtcfg)
	if ghw != nil {
		srv.writeTokenKind = ghTokenKind(wtcfg.token)
		log.Printf("auto-fix: write token on (%s)", srv.writeTokenKind)
	} else {
		log.Printf("auto-fix: write token off (%s)", wtcfg.why)
	}
	wcfg := waConfigFromEnv()
	wa := newWANotifier(wcfg)
	switch {
	case wa != nil:
		wa.VerifyGroup(ctx)
		log.Printf("whatsapp: notifier on (%s)", urlHost(wcfg.base))
	case wcfg.why != "":
		log.Printf("whatsapp: notifier off (%s)", wcfg.why)
	}
	tmcfg := taskmgrConfigFromEnv()
	tm := newTaskMgrLink(store, tmcfg)
	switch {
	case tm.cl != nil:
		tm.people.logf = log.Printf
		go tm.people.Run(ctx)
		log.Printf("taskmgr: client on (%s)", tmcfg.addr)
	case tmcfg.why != "":
		log.Printf("taskmgr: client off (%s)", tmcfg.why)
	case tmcfg.off != "":
		log.Printf("taskmgr: client off (%s)", tmcfg.off)
	}
	srv.tm = tm
	nt := &notifier{st: store, wa: wa, gh: authors, logf: log.Printf, tm: tm}
	srv.nt = nt
	srv.wa = wa
	// Pull requests already open when this release starts get their rows (design §4.3); idempotent.
	if res, err := store.BackfillReviewRows(ctx); err != nil {
		log.Printf("pull requests: the review rows could not be back-filled: %v", err)
	} else if n := len(res.PullRequests); n > 0 {
		log.Printf("pull requests: %d review row(s) back-filled", n)
	}
	// The QA tool writes and runs the e2e specs; GAOS only asks and polls.
	if srv.qaOut != nil {
		jr := newQAJobRunner(store, srv.qaOut, flow.ctx, nt, flow.gh)
		jr.logf = log.Printf
		go jr.Run(ctx)
	}
	switch {
	case srv.staging.enabled():
		log.Printf("staging: callback on for %d product(s)", len(srv.staging.sums))
	case srv.staging.why != "":
		log.Printf("staging: callback off (%s)", srv.staging.why)
	}
	if h, ok := store.(interface {
		SetPRHooks(func(TaskRecord) []PacketPart, func(TaskRecord, string))
	}); ok {
		rv := &prReviewer{st: store, logf: log.Printf, notify: nt.Notify}
		dt := &defectTriager{st: store, logf: log.Printf, notify: nt.Notify}
		fx := &defectFixer{st: store, gh: ghw, tokenKind: srv.writeTokenKind, notify: nt.Notify, logf: log.Printf}
		go autoFixLoop(ctx, store, srv.codeSrc)
		go fx.resumeFixPRs(ctx)
		poller := newFixPRPoller(store, ghc)
		srv.fixPoll = poller
		if ghc != nil {
			rv.gh = ghc
			rv.rd = ghc     // D-102: the fresh read before posting, and the review fix's reads
			fx.review = ghc // the "draft fix" line, with the review token
		}
		tp := &testPlanner{st: store, logf: log.Printf, notify: nt.Notify, tm: tm, direct: nt.notifyTo}
		poller.tm = tm
		if srv.qaOut != nil {
			tp.qa = srv.qaOut
		}
		poller.alert = prAlerter{nt: nt}
		if ghc != nil {
			poller.alert.post = ghc
		}
		go poller.Run(ctx) // after tm and alert are set: the poll goroutine reads both
		rv.cache, rv.src = flow.ctx, srv.codeSrc
		rv.lowFix = rv.planLowFix
		h.SetPRHooks(
			func(t TaskRecord) []PacketPart { return append(prPacketParts(flow.ctx, t), testPlannerParts(t)...) },
			func(t TaskRecord, reply string) {
				switch {
				case t.Role == "pr-reviewer":
					rv.afterRun(context.Background(), t, reply)
				case t.Role == "triager":
					dt.afterRun(context.Background(), t, reply)
				case t.Role == "fixer":
					fx.afterRun(context.Background(), t, reply)
				case t.Role == "test-planner":
					tp.afterRun(context.Background(), t, reply)
				case t.Role == "unit-test" && t.DedupKey != "" && t.Status == StatusFailed:
					nt.Notify(context.Background(), t, "unit:failure", "unit tests failed")
				}
			})
	}

	if btCons != nil {
		go btCons.Run(ctx)
		// The reader's consumer: the pull-request flow. It runs on its own
		// goroutine and never blocks the reader.
		go btCons.Drain(ctx, flow.handle)
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
	log.Printf("gaos dashboard listening on %s (%s, store=%s, arcade=%s, adapter=%s)",
		addr, mode, store.Kind(), srv.arc.Describe(), srv.describeAdapter())

	if err := hs.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	var patterns []string
	bearer := map[string]bool{}
	handle := func(p string, f http.HandlerFunc) {
		patterns = append(patterns, p)
		mux.HandleFunc(p, f)
	}

	// Go 1.22 patterns: method and wildcards in the pattern itself, so there is
	// no router library and no switch on r.Method inside a handler.
	handle("GET /{$}", s.handleIndex)
	// The page routes (D-094, urls.go): one whole document per back-office view
	// and per product section, so a deep link, a reload and Back land on it.
	// They are back-office routes; signed out, the gate answers them itself.
	for _, v := range pageViews {
		handle("GET /"+v.ID, s.handlePage(v.ID))
	}
	handle("GET /p/{ref}", s.handleProductPage)
	handle("GET /p/{ref}/{section}", s.handleProductPage)
	handle("GET /healthz", s.handleHealth)
	patterns = append(patterns, "GET /static/")
	mux.Handle("GET /static/", s.staticHandler())

	// --- the documented surface (architecture §09). Named now so nothing has
	// to be renamed when the graph is behind it. ---
	handle("GET /events", s.handleEvents)
	handle("GET /events/stream", s.handleStream) // D-065: hints, not HTML
	handle("POST /replay/{workflow}", s.handleReplay)
	handle("POST /tasks/{id}/dispatch", s.handleDispatch)
	handle("POST /tasks/{id}/approve", s.handleApprove)
	handle("POST /tasks/{id}/retry", s.handleRetry)
	handle("POST /tasks/{id}/cancel", s.handleCancel)
	handle("GET /tasks/{id}/context", s.handleTaskContext)

	// --- back office ---
	handle("POST /auth/login", s.handleLogin)
	handle("POST /auth/logout", s.handleLogout)
	handle("GET /ui/bo", s.handleBOSheet)
	handle("GET /ui/bo/{view}", s.handleBOView)
	handle("GET /ui/bo/autofix/live", s.handleAutoFixLive)                 // the Auto-fix view's 30 s poll
	handle("POST /products/{id}/prs/{fixPRID}/retarget", s.handleRetarget) // D-092
	handle("GET /ui/bo/prs/live", s.handlePRsLive)                         // the Pull requests view's 30 s poll (D-092)

	handle("GET /products", s.handleProductList)
	handle("POST /products", s.handleProductCreate)
	handle("GET /products/{id}", s.handleProductDetail)
	handle("PUT /products/{id}/brd", s.handleBRDSave)
	handle("POST /products/{id}/stories", s.handleAddStory)
	handle("POST /products/{id}/diagrams", s.handleAddDiagram)
	handle("POST /products/{id}/knowledge", s.handleAddDecision)
	handle("POST /products/{id}/inject", s.handleInject)
	handle("POST /products/{id}/blueprint/policies", s.handleSavePolicy)
	handle("POST /products/{id}/blueprint/sources", s.handleAddSource)
	handle("POST /products/{id}/blueprint/publish", s.handlePublishBlueprint)
	handle("POST /products/{id}/blueprint/sync", s.handleSyncBlueprint)
	handle("PUT /products/{id}/vendors", s.handleSaveVendors)
	handle("GET /products/{id}/board", s.handleBoard)
	handle("GET /products/{id}/board/live", s.handleBoardLive)                   // the Board's own route (D-095)
	handle("GET /products/{id}/board/cards/{taskId}/timing", s.handleCardTiming) // D-081
	handle("PUT /products/{id}/board/cards/{taskId}/move", s.handleMoveCard)
	handle("POST /products/{id}/board/cards/{taskId}/send-back", s.handleSendBack) // D-084
	handle("POST /products/{id}/board/cards/{taskId}/label", s.handleSetLabel)     // D-088
	handle("POST /products/{id}/third-party", s.handleSaveThirdParty)              // D-104
	handle("POST /products/{id}/autofix", s.handleSaveAutoFix)                     // D-091
	handle("POST /products/{id}/defects/{ref}/close", s.handleCloseDefect)         // D-091
	handle("POST /agents/assign", s.handleAssign)                                  // D-102
	handle("POST /contacts", s.handleContactSave)                                  // D-088
	handle("POST /contacts/delete", s.handleContactDelete)                         // D-088
	handle("POST /products/{id}/board/cards", s.handleCardRefused)
	handle("PUT /products/{id}/board/cards/{taskId}", s.handleCardRefused)
	handle("DELETE /products/{id}/board/cards/{taskId}", s.handleCardRefused)
	handle("GET /products/{id}/readiness", s.handleReadiness)

	// --- "Needs you" (M15): agents waiting on the operator, and the answer ---
	handle("GET /questions", s.handleQuestions)
	handle("GET /products/{id}/questions", s.handleQuestions)
	handle("POST /products/{id}/tasks/{taskId}/answer", s.handleAnswer)
	handle("GET /ui/needs", s.handleNeeds)                      // the chip and the nav count
	handle("GET /ui/answer/{id}/{taskId}", s.handleAnswerSheet) // the answer sheet
	// D-078: the QA tool's questions, answered through the same sheet.
	handle("POST /products/{id}/inbox/{externalId}/answer", s.handleExternalAnswer)
	handle("GET /ui/answer-ext/{id}/{externalId}", s.handleExternalSheet)
	// D-081: a card's timing sheet.
	handle("GET /ui/timing/{id}/{taskId}", s.handleTimingSheet)
	// D-082: a card's conversation, its archive and its prune.
	handle("GET /ui/conversation/{id}/{taskId}", s.handleConversationSheet)
	handle("POST /products/{id}/board/cards/{taskId}/conversation/archive", s.handleArchiveConversation(true))
	handle("POST /products/{id}/board/cards/{taskId}/conversation/restore", s.handleArchiveConversation(false))
	handle("POST /conversations/prune", s.handlePrune)
	// D-084: the send-back sheet of a design-blocked review.
	handle("GET /ui/send-back/{id}/{taskId}", s.handleSendBackSheet)

	// --- create workflow ---
	handle("GET /ui/cw", s.handleCWOpen)
	handle("POST /ui/cw/step/{n}", s.handleCWStep)
	handle("POST /workflows", s.handleWorkflowCreate)

	// The machine API (api_v1.go). Traefik strips /api, so these are /v1/...
	s.registerAPI(func(p string, f http.HandlerFunc) {
		bearer[p] = true
		mux.HandleFunc(p, f)
	})

	// The QA sync buttons (handlers_qa.go): the back office's own routes.
	handle("POST /products/{id}/qa/sync/pull", s.handleQaPull)
	handle("POST /products/{id}/qa/sync/push", s.handleQaPush)

	s.routesMu.Lock()
	s.patterns, s.bearer = patterns, bearer
	s.routesMu.Unlock()
	return s.gate(mux, bearer)
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
		// The configured switch (D-080); whether anything can be sent at all is
		// the adapter key beside it.
		"runner": runnerMode(),
		"uptime": time.Since(started).Round(time.Second).String(),
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
// The floor it used to carry -- desks, rooms, the inspector, the timeline
// strip -- was retired in M14.7; what is left is the chrome.
func (s *server) newView(r *http.Request) (*View, Floor) {
	f, _ := s.store.Snapshot(r.Context())
	v := &View{
		Prefix:   sitePrefix(r),
		Stats:    f.Stats,
		SignedIn: s.auth.signedIn(r),
		Tenant:   s.tenantLabel(r),
		Needs:    s.needsCount(r),
	}
	return v, f
}

// applyResult folds a mutation's Result into a fresh view: its toast.
func (s *server) applyResult(r *http.Request, res Result) *View {
	v, _ := s.newView(r)
	v.Toast = res.Toast
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
