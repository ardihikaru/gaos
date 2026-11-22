package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// The Brain Town emitter (D-086, docs/designs/brain-town-emitter.md).

const btWorkedExample = "01HF7YAT00X6Z3RRM5ATZ62EBX"
const btTestToken = "bt_live_0123456789abcdefghij"

type btHook struct {
	mu     sync.Mutex
	status []int // statuses to answer, in order; the last repeats
	calls  int
	ids    []string
	keys   []string
	auths  []string
	bodies []btOut
	block  chan struct{}
}

func (h *btHook) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.block != nil {
		<-h.block
	}
	b, _ := io.ReadAll(r.Body)
	var o btOut
	_ = json.Unmarshal(b, &o)
	h.mu.Lock()
	i := h.calls
	h.calls++
	h.ids = append(h.ids, o.ID)
	h.keys = append(h.keys, r.Header.Get("Idempotency-Key"))
	h.auths = append(h.auths, r.Header.Get("Authorization"))
	h.bodies = append(h.bodies, o)
	st := 202
	if len(h.status) > 0 {
		if i >= len(h.status) {
			i = len(h.status) - 1
		}
		st = h.status[i]
	}
	h.mu.Unlock()
	w.WriteHeader(st)
}

func (h *btHook) n() int { h.mu.Lock(); defer h.mu.Unlock(); return h.calls }

func (h *btHook) types() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []string
	for _, b := range h.bodies {
		out = append(out, b.Type)
	}
	return out
}

// btTestWorker builds a worker against a fake receiver with a fixed set of products and tasks.
func btTestWorker(t *testing.T, h *btHook, public ...string) (*brainTown, map[string]TaskRecord, *[]time.Duration, *[]string) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL + "/ingest/gaos/secret-id")
	cfg := btConfig{enabled: true, url: u, token: btTestToken, public: map[string]bool{}}
	for _, p := range public {
		cfg.public[p] = true
	}
	b := newBrainTown(cfg, NewMemStore(1))
	tasks := map[string]TaskRecord{}
	b.products = func(context.Context) ([]Product, error) {
		return []Product{{ID: "p-1", Name: "Alpha"}, {ID: "p-2", Name: "Beta"}}, nil
	}
	b.task = func(_ context.Context, id string) (TaskRecord, bool) { r, ok := tasks[id]; return r, ok }
	var waits []time.Duration
	var logs []string
	b.sleep = func(_ context.Context, d time.Duration) { waits = append(waits, d) }
	b.logf = func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }
	return b, tasks, &waits, &logs
}

func mv(seq int, pid, task, from, to, rule string) Event {
	return Event{Seq: seq, Type: TaskMoved, DeskID: pid, At: 1700000000 + int64(seq),
		Payload: EventPayload{Task: task, To: to, Reason: from + "→" + to, Rule: rule, Actor: "operator@example.org"}}
}

func btTokenFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "bt")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// TC-513
func TestBrainTownDisabledSendsNothing(t *testing.T) {
	h := &btHook{}
	srv := httptest.NewServer(h)
	defer srv.Close()
	cfg := newBTConfig("false", srv.URL, btTokenFile(t, btTestToken), "p-1", nil, nil)
	if cfg.enabled {
		t.Fatalf("BRAIN_TOWN_ENABLED=false produced an enabled feed")
	}
	if got := btState(cfg).State; got != "disabled" {
		t.Fatalf("state = %q, want disabled", got)
	}
	st := NewMemStore(1)
	if _, err := st.CreateProduct(context.Background(), ProductDraft{Name: "Alpha"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if h.n() != 0 {
		t.Fatalf("a disabled feed made %d request(s)", h.n())
	}
}

// TC-514
func TestBrainTownConfigIsValidated(t *testing.T) {
	good := btTokenFile(t, btTestToken+"\n")
	api := &apiConfig{hasAdmin: true, adminSum: sumOf("apiadmin_0123456789abcdefghijkl")}
	cases := []struct {
		name, url, file string
		wantWhy         string
	}{
		{"valid", "https://town.example.org/ingest/gaos/x", good, ""},
		{"http", "http://town.example.org/ingest/gaos/x", good, "https only"},
		{"http loopback is still refused", "http://127.0.0.1:9/x", good, "https only"},
		{"userinfo", "https://u:p@town.example.org/x", good, "credentials"},
		{"query", "https://town.example.org/x?a=1", good, "no query"},
		{"deny host", "https://gateway.example.org/x", good, "already uses"},
		{"no url", "", good, "BRAIN_TOWN_URL is not set"},
		{"no file", "https://town.example.org/x", "", "BRAIN_TOWN_TOKEN_FILE is not set"},
		{"missing file", "https://town.example.org/x", "/nonexistent/bt", "cannot be read"},
		{"short token", "https://town.example.org/x", btTokenFile(t, "short"), "16 to 256"},
		{"whitespace token", "https://town.example.org/x", btTokenFile(t, "bt_live_ with space 1234"), "printable"},
		{"gaos token", "https://town.example.org/x", btTokenFile(t, "gaos_0123456789abcdef0123456789abcdef"), "GAOS credential"},
		{"equals an api token", "https://town.example.org/x", btTokenFile(t, "apiadmin_0123456789abcdefghijkl"), "GAOS credential"},
	}
	for _, c := range cases {
		cfg := newBTConfig("true", c.url, c.file, "", []string{"gateway.example.org"}, api)
		if c.wantWhy == "" {
			if cfg.why != "" || cfg.url == nil || cfg.token != btTestToken {
				t.Errorf("%s: want a usable feed, got why=%q", c.name, cfg.why)
			}
			continue
		}
		if !strings.Contains(cfg.why, c.wantWhy) || cfg.url != nil {
			t.Errorf("%s: why = %q, want it to contain %q", c.name, cfg.why, c.wantWhy)
		}
		if strings.Contains(cfg.why, btTestToken) {
			t.Errorf("%s: the reason carries the token", c.name)
		}
	}
}

// TC-515
func TestBrainTownMapsEveryEventType(t *testing.T) {
	h := &btHook{}
	b, tasks, _, _ := btTestWorker(t, h, "p-1")
	ctx := context.Background()
	tasks["T-1"] = TaskRecord{ID: "T-1", Title: "Add captions", Kind: TaskKindImplement, Role: "engineer"}
	tasks["R-1"] = TaskRecord{ID: "R-1", Title: "Review", Kind: TaskKindReview, Role: "reviewer"}
	tasks["T-2"] = TaskRecord{ID: "T-2", Title: "Other", Role: "engineer"}

	planned := Event{Seq: 1, Type: TaskPlanned, DeskID: "p-1", At: 1700000000,
		Payload: EventPayload{Tasks: []PlannedTask{{ID: "T-1", Title: "Add captions", Kind: ""}}}}
	got := b.mapEvent(ctx, planned)
	if len(got) != 2 || got[0].Type != "product.upserted" || got[1].Type != "task.created" || got[1].Task.Column != "backlog" || got[1].Task.Kind != "implement" {
		t.Fatalf("TaskPlanned mapped to %+v", got)
	}
	if o := b.mapEvent(ctx, Event{Seq: 2, Type: ProductCreated, DeskID: "p-1"}); len(o) != 0 {
		t.Fatalf("an unchanged product produced %d upsert(s) again", len(o))
	}
	types := func(es []btOut) string {
		var s []string
		for _, e := range es {
			s = append(s, e.Type)
		}
		return strings.Join(s, ",")
	}
	cases := []struct {
		ev   Event
		want string
	}{
		{mv(3, "p-1", "T-1", "READY", "DISPATCHED", MoveSchedule), "task.moved"},
		{mv(4, "p-1", "T-1", "DISPATCHED", "RUNNING", MoveStart), "task.moved,task.claimed"},
		{mv(5, "p-1", "T-1", "RUNNING", "DONE", "exit"), "task.moved,task.done"},
		{mv(6, "p-1", "T-1", "DONE", "READY", "send-back"), "task.moved"},
		{mv(7, "p-1", "T-1", "RUNNING", "FAILED", "exit"), "task.moved,task.failed"},
		{mv(8, "p-1", "T-1", "READY", "CANCELED", "cancel"), "task.moved,task.failed"},
		{mv(9, "p-1", "R-1", "PENDING", "READY", "schedule"), "task.created,task.moved,gate.requested"},
		{mv(10, "p-1", "R-1", "RUNNING", "DONE", RuleReviewCleared), "task.moved,task.done,gate.decided"},
		{mv(11, "p-1", "R-1", "RUNNING", "BLOCKED", RuleDesignBlocked), "task.moved,gate.decided"},
		{mv(12, "p-1", "R-1", "RUNNING", "BLOCKED", RuleReviewFailed), "task.moved"},
		{mv(13, "p-1", "T-1", "BLOCKED", "READY", RuleReviewCleared), "task.moved"}, // a released implementation task is no verdict
	}
	for _, c := range cases {
		out := b.mapEvent(ctx, c.ev)
		if g := types(out); g != c.want {
			t.Errorf("seq %d (%s): mapped to %q, want %q", c.ev.Seq, c.ev.Payload.Rule, g, c.want)
		}
		for _, o := range out {
			raw, _ := json.Marshal(o)
			if strings.Contains(string(raw), "@") || strings.Contains(string(raw), "operator") {
				t.Errorf("seq %d: a body carries the actor: %s", c.ev.Seq, raw)
			}
			if o.Type == "task.failed" {
				if r := o.Data["reason"]; r != "failed" && r != "canceled" {
					t.Errorf("seq %d: failed reason = %v, want failed or canceled", c.ev.Seq, r)
				}
			}
		}
	}
	if o := b.mapEvent(ctx, mv(14, "p-1", "T-1", "PENDING", "READY", MoveSchedule)); o[0].Data["agent"] != nil {
		t.Fatalf("agent sent for a move that is not into working: %v", o[0].Data)
	}
	if o := b.mapEvent(ctx, mv(15, "p-1", "T-1", "DISPATCHED", "RUNNING", MoveStart)); o[0].Data["agent"] != "engineer" {
		t.Fatalf("agent on a move into working = %v, want the role", o[0].Data["agent"])
	}
	if o := b.mapEvent(ctx, Event{Seq: 16, Type: RecordUpdated, DeskID: "p-1", Text: "QA results applied"}); len(o) != 0 {
		t.Fatalf("a QA record update was mapped: %+v", o)
	}
}

// TC-516
func TestBrainTownRedactsHiddenProducts(t *testing.T) {
	h := &btHook{}
	b, tasks, _, _ := btTestWorker(t, h, "p-1")
	ctx := context.Background()
	long := strings.Repeat("x", 300)
	tasks["A-1"] = TaskRecord{ID: "A-1", Title: long + "\x07"}
	tasks["A-2"] = TaskRecord{ID: "A-2", Title: "token sk-ant-api03-abcdefghijklmnopqrstuvwxyz"}
	tasks["B-1"] = TaskRecord{ID: "B-1", Title: "Secret plan"}
	pub := b.mapEvent(ctx, mv(1, "p-1", "A-1", "READY", "RUNNING", MoveStart))
	if pub[0].Product.Visibility != "public" || pub[0].Product.Name != "Alpha" || pub[0].Product.Key != "p-1" {
		t.Fatalf("public product sent as %+v", pub[0].Product)
	}
	for _, o := range pub {
		if o.Task != nil && len([]rune(o.Task.Title)) != 200 {
			t.Fatalf("public title length = %d runes, want 200", len([]rune(o.Task.Title)))
		}
	}
	pub2 := b.mapEvent(ctx, mv(2, "p-1", "A-2", "READY", "RUNNING", MoveStart))
	for _, o := range pub2 {
		if o.Task != nil && o.Task.Title != "A-2" {
			t.Fatalf("a title with a credential left as %q, want the task id", o.Task.Title)
		}
	}
	hid := b.mapEvent(ctx, mv(3, "p-2", "B-1", "READY", "RUNNING", MoveStart))
	for _, o := range hid {
		raw, _ := json.Marshal(o)
		if o.Product.Visibility != "hidden" || o.Product.Name != btAlias || o.Product.Key != "p-2" {
			t.Fatalf("hidden product sent as %+v", o.Product)
		}
		if strings.Contains(string(raw), "Beta") || strings.Contains(string(raw), "Secret plan") {
			t.Fatalf("a hidden product's real name or title left GAOS: %s", raw)
		}
		if o.Task != nil && o.Task.Title != "B-1" {
			t.Fatalf("hidden title = %q, want the task id", o.Task.Title)
		}
	}
}

// TC-517
func TestBrainTownLifecycleOrderAndFirstSight(t *testing.T) {
	h := &btHook{}
	b, tasks, _, _ := btTestWorker(t, h, "p-1")
	ctx := context.Background()
	tasks["T-1"] = TaskRecord{ID: "T-1", Title: "Add captions", Role: "engineer"}
	b.handle(ctx, Event{Seq: 1, Type: TaskPlanned, DeskID: "p-1", At: 1700000000,
		Payload: EventPayload{Tasks: []PlannedTask{{ID: "T-1", Title: "Add captions"}}}})
	steps := [][3]string{{"PENDING", "READY", "schedule"}, {"READY", "DISPATCHED", "schedule"},
		{"DISPATCHED", "RUNNING", MoveStart}, {"RUNNING", "REVIEW", "exit"}, {"REVIEW", "DONE", "approve"}, {"DONE", "READY", "send-back"}, {"READY", "DONE", "approve"}}
	for i, s := range steps {
		b.handle(ctx, mv(2+i, "p-1", "T-1", s[0], s[1], s[2]))
	}
	want := "product.upserted,task.created,task.moved,task.moved,task.moved,task.claimed,task.moved,task.moved,task.done,task.moved,task.moved,task.done"
	if got := strings.Join(h.types(), ","); got != want {
		t.Fatalf("lifecycle sent\n %s\nwant\n %s", got, want)
	}
	// a task GAOS knew before this boot: its first move is preceded by a task.created
	b2h := &btHook{}
	b2, tasks2, _, _ := btTestWorker(t, b2h, "p-1")
	tasks2["T-9"] = TaskRecord{ID: "T-9", Title: "Old", Role: "engineer"}
	b2.handle(ctx, mv(1, "p-1", "T-9", "READY", "DISPATCHED", MoveSchedule))
	if got := strings.Join(b2h.types(), ","); got != "product.upserted,task.created,task.moved" {
		t.Fatalf("first sight sent %s, want product.upserted,task.created,task.moved", got)
	}
	if c := b2h.bodies[1].Task.Column; c != "ready" {
		t.Fatalf("synthetic task.created column = %q, want ready (the move's from-column)", c)
	}
}

// TC-518
func TestBrainTownRetriesKeepTheSameID(t *testing.T) {
	if id := btID([16]byte{}, 1700000000000, 7, 0); len(id) != 26 || id != btID([16]byte{}, 1700000000000, 7, 0) || id == btID([16]byte{}, 1700000000000, 7, 1) {
		t.Fatalf("btID = %q: want 26 deterministic characters that differ by index", id)
	}
	if got := btID([16]byte{}, 1700000000000, 7, 0); got != btWorkedExample {
		t.Fatalf("worked-example id = %q, want %q", got, btWorkedExample)
	}
	ev := mv(1, "p-1", "T-1", "READY", "DISPATCHED", MoveSchedule)
	run := func(status []int) (*btHook, []time.Duration, []string) {
		h := &btHook{status: status}
		b, tasks, waits, logs := btTestWorker(t, h, "p-1")
		tasks["T-1"] = TaskRecord{ID: "T-1", Title: "x"}
		for _, o := range b.mapEvent(context.Background(), ev)[1:2] {
			b.deliver(context.Background(), o)
		}
		return h, *waits, *logs
	}
	h, waits, _ := run([]int{503, 429, 202})
	if h.n() != 3 || len(waits) != 2 || waits[0] != time.Second || waits[1] != 5*time.Second {
		t.Fatalf("5xx then 429 then 202: %d calls, waits %v; want 3 calls, waits [1s 5s]", h.n(), waits)
	}
	for i := range h.ids {
		if h.ids[i] != h.ids[0] || h.keys[i] != h.ids[0] || h.auths[i] != "Bearer "+btTestToken {
			t.Fatalf("attempt %d: id %q key %q auth %q; want one id used as Idempotency-Key", i, h.ids[i], h.keys[i], h.auths[i])
		}
	}
	if h, _, logs := run([]int{400}); h.n() != 1 || len(logs) != 1 || !strings.Contains(logs[0], "status 400") {
		t.Fatalf("400: %d calls, logs %v; want one call, one log line", h.n(), logs)
	}
	if h, _, logs := run([]int{409}); h.n() != 1 || len(logs) != 0 {
		t.Fatalf("409: %d calls, logs %v; want success after one call", h.n(), logs)
	}
	if h, w, logs := run([]int{500}); h.n() != 4 || len(w) != 3 || w[2] != 30*time.Second || len(logs) != 1 {
		t.Fatalf("persistent 500: %d calls, waits %v, logs %v; want 4 calls, 1s 5s 30s, one log line", h.n(), w, logs)
	}
}

// TC-519
func TestBrainTownSlowReceiverNeverBlocksTheStore(t *testing.T) {
	h := &btHook{block: make(chan struct{})}
	b, _, _, _ := btTestWorker(t, h, "p-1")
	defer close(h.block)
	st := NewMemStore(1)
	st.SetCommitTap(b.Tap)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go b.Run(ctx)
	start := time.Now()
	if _, err := st.CreateProduct(ctx, ProductDraft{Name: "Alpha"}); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 100*time.Millisecond {
		t.Fatalf("CreateProduct took %v with the receiver hung; want under 100ms", d)
	}
	start = time.Now()
	for i := 0; i < btQueue+500; i++ {
		b.Tap(mv(i+10, "p-1", fmt.Sprintf("T-%d", i), "READY", "DISPATCHED", MoveSchedule))
	}
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Fatalf("%d taps took %v with the receiver hung; want a non-blocking send", btQueue+500, d)
	}
	if b.Status().Dropped == 0 {
		t.Fatalf("overflow dropped nothing; want a counted drop")
	}
}

// TC-520
func TestBrainTownTokenNeverLeaks(t *testing.T) {
	h := &btHook{status: []int{500}}
	b, tasks, _, logs := btTestWorker(t, h, "p-1")
	tasks["T-1"] = TaskRecord{ID: "T-1", Title: "x"}
	for _, o := range b.mapEvent(context.Background(), mv(1, "p-1", "T-1", "READY", "DISPATCHED", MoveSchedule)) {
		b.deliver(context.Background(), o)
	}
	b.cfg.url.Host = "127.0.0.1:1" // refused
	for _, o := range b.mapEvent(context.Background(), mv(2, "p-1", "T-1", "DISPATCHED", "RUNNING", MoveStart)) {
		b.deliver(context.Background(), o)
	}
	if len(*logs) == 0 {
		t.Fatal("no failed delivery was logged")
	}
	all := strings.Join(*logs, "\n") + fmt.Sprintf("%+v", b.Status())
	for _, bad := range []string{btTestToken, "secret-id", "/ingest"} {
		if strings.Contains(all, bad) {
			t.Fatalf("log or status contains %q:\n%s", bad, all)
		}
	}
	if strings.Contains(strings.Join(*logs, "\n"), "127.0.0.1") {
		t.Fatalf("a log line names the receiver's address:\n%s", strings.Join(*logs, "\n"))
	}
	if _, why := readBrainTownToken(btTokenFile(t, "bad token")); strings.Contains(why, "bad token") {
		t.Fatalf("the token reason echoes the file content: %q", why)
	}
}

// TC-521
func TestBrainTownIntegrationsRowHasNoSecret(t *testing.T) {
	h := &btHook{}
	b, _, _, _ := btTestWorker(t, h)
	row := b.Status()
	raw := fmt.Sprintf("%+v", row)
	if row.State != "enabled" || !strings.HasPrefix(row.Host, "http://127.0.0.1:") {
		t.Fatalf("row = %s; want state enabled and a scheme://host", raw)
	}
	for _, bad := range []string{btTestToken, "secret-id", "/ingest"} {
		if strings.Contains(raw, bad) {
			t.Fatalf("row contains %q: %s", bad, raw)
		}
	}
	if s := btState(btConfig{enabled: true, why: "BRAIN_TOWN_URL is not set"}).State; s != "unusable: BRAIN_TOWN_URL is not set" {
		t.Fatalf("unusable state = %q", s)
	}
}

// TC-522
func TestBrainTownTapFiresOnlyAfterCommit(t *testing.T) {
	commit := 204
	arc := testArcade(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/v1/begin/"):
			w.Header().Set("arcadedb-session-id", "s1")
			w.WriteHeader(204)
		case strings.HasPrefix(r.URL.Path, "/api/v1/commit/"):
			w.WriteHeader(commit)
		case strings.HasPrefix(r.URL.Path, "/api/v1/rollback/"):
			w.WriteHeader(204)
		default:
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"result":[]}`)
		}
	})
	as := &ArcadeStore{arc: arc, tenant: "t", proj: NewMemStore(1)}
	var got []int
	as.SetCommitTap(func(e Event) { got = append(got, e.Seq) })
	r := Result{NewEvents: []Event{{Seq: 1, Type: ProductCreated, DeskID: "p-1"}, {Seq: 2, Type: TaskPlanned, DeskID: "p-1"}}}
	commit = 500
	if err := as.persist(context.Background(), r); err == nil {
		t.Fatal("persist succeeded although the commit failed")
	}
	if len(got) != 0 {
		t.Fatalf("tap saw %v after a failed commit; want nothing", got)
	}
	commit = 204
	if err := as.persist(context.Background(), r); err != nil {
		t.Fatalf("persist: %v", err)
	}
	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("tap saw %v after a commit; want [1 2]", got)
	}
	ms := NewMemStore(1)
	var mem int
	ms.SetCommitTap(func(Event) { mem++ })
	if _, err := ms.CreateProduct(context.Background(), ProductDraft{Name: "Alpha"}); err != nil || mem == 0 {
		t.Fatalf("MemStore tap saw %d event(s) (err %v); want emit to be the commit", mem, err)
	}
}
