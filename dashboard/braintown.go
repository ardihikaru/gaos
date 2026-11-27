package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
)

// The Brain Town emitter (D-086, docs/designs/brain-town-emitter.md).
//
// Brain Town is a public, animated town that shows work as it happens: each GAOS
// product is a hall, each task a person. GAOS pushes one POST per committed state
// change to a receiver the operator names. The feed is off by default; when off there
// is no tap, no goroutine and no request.
//
// The source is a post-commit tap (commitFeed): ArcadeStore calls it after tx.Commit,
// MemStore from emit. The tap only does a non-blocking channel send, because it runs
// under a store lock. One sender goroutine maps, redacts and delivers, in order.
//
// A hidden product is redacted BEFORE it leaves (§3.3): the receiver still gets
// visibility "hidden", but the name is a fixed alias and each title is the task id.
//
// Nothing here reads Payload.Actor, Event.Text, Payload.Reason's prose or a task's
// BlockReason for a body: they can hold an operator's account name or free text.

const (
	btTimeout   = 3 * time.Second
	btQueue     = 2000
	btMaxTitle  = 200
	btMaxTasks  = 50000
	btBodyLimit = 4 << 10
	btAlias     = "Private product"
	btDegradeAt = 3
)

// btWaits is the wait before the 2nd, 3rd and 4th attempt.
var btWaits = []time.Duration{time.Second, 5 * time.Second, 30 * time.Second}

// commitFeed is the optional half of a store that reports committed events.
type commitFeed interface{ SetCommitTap(func(Event)) }

// taskLookup is the optional half of a store that can name one task.
type taskLookup interface {
	TaskInfo(ctx context.Context, id string) (TaskRecord, bool)
}

// ---------------------------------------------------------------- configuration

type btConfig struct {
	enabled bool
	url     *url.URL
	token   string
	public  map[string]bool
	why     string // non-empty: enabled was asked for but the feed is unusable
}

// btConfigFromEnv reads the four BRAIN_TOWN_* settings.
func btConfigFromEnv(api *apiConfig) btConfig {
	host := func(env string) string {
		if u, err := url.Parse(os.Getenv(env)); err == nil {
			return u.Hostname()
		}
		return ""
	}
	return newBTConfig(os.Getenv("BRAIN_TOWN_ENABLED"), os.Getenv("BRAIN_TOWN_URL"),
		os.Getenv("BRAIN_TOWN_TOKEN_FILE"), os.Getenv("BRAIN_TOWN_PUBLIC_PRODUCTS"),
		[]string{host("GAOS_ARCADE_URL"), host("GAOS_AIM_URL"), host("GAOS_QA_URL"), trafficStoreHost()}, api)
}

func newBTConfig(enabled, rawURL, tokenFile, public string, deny []string, api *apiConfig) btConfig {
	c := btConfig{public: map[string]bool{}}
	switch strings.ToLower(strings.TrimSpace(enabled)) {
	case "true", "1":
		c.enabled = true
	default:
		return c
	}
	for _, id := range strings.Split(public, ",") {
		if id = strings.ToLower(strings.TrimSpace(id)); id != "" {
			c.public[id] = true
		}
	}
	switch {
	case strings.TrimSpace(rawURL) == "":
		c.why = "BRAIN_TOWN_URL is not set"
		return c
	case strings.TrimSpace(tokenFile) == "":
		c.why = "BRAIN_TOWN_TOKEN_FILE is not set"
		return c
	}
	u, why := validOutURL(rawURL, "BRAIN_TOWN_URL", true, deny)
	if why != "" {
		c.why = why
		return c
	}
	tok, why := readBrainTownToken(tokenFile)
	if why != "" {
		c.why = why
		return c
	}
	if strings.HasPrefix(tok, apiTokenPrefix) || api.holdsToken(tok) {
		c.why = "BRAIN_TOWN_TOKEN_FILE holds a GAOS credential; it must be the receiver's token"
		return c
	}
	c.url, c.token = u, tok
	return c
}

// readBrainTownToken reads the receiver's bearer token: 16 to 256 printable ASCII
// characters, no whitespace. An unreadable or malformed file disables the feed; the
// reason names the setting, never the content.
func readBrainTownToken(path string) (string, string) {
	warnIfOpen(path, log.Printf)
	b, err := os.ReadFile(path)
	if err != nil {
		return "", "BRAIN_TOWN_TOKEN_FILE cannot be read"
	}
	t := strings.TrimSuffix(strings.TrimSuffix(string(b), "\n"), "\r")
	if len(t) < 16 || len(t) > 256 {
		return "", "BRAIN_TOWN_TOKEN_FILE holds no token of 16 to 256 characters"
	}
	for _, r := range t {
		if r > unicode.MaxASCII || !unicode.IsPrint(r) || unicode.IsSpace(r) {
			return "", "BRAIN_TOWN_TOKEN_FILE holds no printable token"
		}
	}
	return t, ""
}

// holdsToken reports whether tok is one of GAOS's own API tokens (nil-safe).
func (c *apiConfig) holdsToken(tok string) bool {
	if c == nil || tok == "" {
		return false
	}
	sum := sumOf(tok)
	for _, h := range []struct {
		on  bool
		sum [32]byte
	}{{c.hasAdmin, c.adminSum}, {c.hasQA, c.qaSum}, {c.hasOut, c.outSum}} {
		if h.on && subtle.ConstantTimeCompare(sum[:], h.sum[:]) == 1 {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------- body

type btProduct struct {
	Key        string `json:"key"`
	Name       string `json:"name"`
	Visibility string `json:"visibility"`
}

type btTask struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Kind   string `json:"kind"`
	Column string `json:"column"`
}

type btOut struct {
	ID      string         `json:"id"`
	Type    string         `json:"type"`
	At      string         `json:"at"`
	Product btProduct      `json:"product"`
	Task    *btTask        `json:"task,omitempty"`
	Data    map[string]any `json:"data,omitempty"`

	productID string // not sent; for resync on a lost delivery
}

const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// btID is a ULID-shaped id: 10 characters of Event.At in milliseconds, then 16 from
// SHA-256(bootKey, seq, index). Deterministic within a process, so a retry carries the
// same id; it reveals no GAOS sequence number.
func btID(bootKey [16]byte, atMs int64, seq, index int) string {
	var out [26]byte
	ms := uint64(atMs)
	for i := 9; i >= 0; i-- {
		out[i] = crockford[ms&31]
		ms >>= 5
	}
	h := sha256.New()
	h.Write(bootKey[:])
	var n [16]byte
	binary.BigEndian.PutUint64(n[:8], uint64(seq))
	binary.BigEndian.PutUint64(n[8:], uint64(index))
	h.Write(n[:])
	sum := h.Sum(nil)
	var acc uint64
	bits := 0
	j := 10
	for _, b := range sum[:10] {
		acc = acc<<8 | uint64(b)
		bits += 8
		for bits >= 5 && j < 26 {
			bits -= 5
			out[j] = crockford[(acc>>uint(bits))&31]
			j++
		}
	}
	return string(out[:])
}

// btTitle is what leaves for a task title.
func btTitle(public bool, id, title string) string {
	if !public {
		return id
	}
	var sb strings.Builder
	n := 0
	for _, r := range title {
		if unicode.IsControl(r) {
			continue
		}
		sb.WriteRune(r)
		if n++; n >= btMaxTitle {
			break
		}
	}
	t := strings.TrimSpace(sb.String())
	if t == "" {
		return id
	}
	for _, d := range detectors {
		if d.re.MatchString(t) || d.re.MatchString(title) {
			return id
		}
	}
	return t
}

// ---------------------------------------------------------------- worker

type brainTown struct {
	cfg  btConfig
	hc   *http.Client
	logf func(string, ...any)

	products func(context.Context) ([]Product, error)
	task     func(context.Context, string) (TaskRecord, bool)

	in      chan Event
	bootKey [16]byte
	waits   []time.Duration
	sleep   func(context.Context, time.Duration)

	mu       sync.Mutex
	prods    map[string]string // product id -> "name|visibility" last sent
	tasks    map[string]bool   // task ids announced
	last     string
	dropped  int64
	failed   int64
	sent     int64
	degraded int
}

func newBrainTown(cfg btConfig, st Store) *brainTown {
	b := &brainTown{cfg: cfg, logf: log.Printf, in: make(chan Event, btQueue),
		waits: btWaits, prods: map[string]string{}, tasks: map[string]bool{},
		sleep: func(ctx context.Context, d time.Duration) {
			t := time.NewTimer(d)
			defer t.Stop()
			select {
			case <-ctx.Done():
			case <-t.C:
			}
		}}
	_, _ = rand.Read(b.bootKey[:])
	base := ""
	if cfg.url != nil {
		base = cfg.url.String()
	}
	b.hc = &http.Client{Timeout: btTimeout, Transport: trafficWrap("brain-town", base, nil),
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("redirects are refused")
		}}
	b.products = st.Products
	if tl, ok := st.(taskLookup); ok {
		b.task = tl.TaskInfo
	}
	return b
}

// Tap is the commit feed's callback. It runs under a store lock and never blocks.
func (b *brainTown) Tap(e Event) {
	switch e.Type {
	case ProductCreated, TaskPlanned, TaskMoved:
	default:
		return
	}
	select {
	case b.in <- e:
	default:
		atomic.AddInt64(&b.dropped, 1)
		b.mu.Lock()
		delete(b.prods, e.DeskID)
		if e.Payload.Task != "" {
			delete(b.tasks, e.Payload.Task)
		}
		for _, t := range e.Payload.Tasks {
			delete(b.tasks, t.ID)
		}
		b.mu.Unlock()
	}
}

// Run is the single sender loop.
func (b *brainTown) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case e := <-b.in:
			b.handle(ctx, e)
		}
	}
}

func (b *brainTown) handle(ctx context.Context, e Event) {
	for _, o := range b.mapEvent(ctx, e) {
		if ctx.Err() != nil {
			return
		}
		b.deliver(ctx, o)
	}
}

func atTime(e Event) time.Time {
	if e.At > 0 {
		return time.Unix(e.At, 0).UTC()
	}
	return time.Now().UTC()
}

// productOut returns a product.upserted when the product is new to the receiver or its
// name or visibility moved, plus the product object for the events that follow.
func (b *brainTown) productOut(ctx context.Context, pid string, e Event, idx *int) (btProduct, []btOut, bool) {
	prods, err := b.products(ctx)
	if err != nil {
		return btProduct{}, nil, false
	}
	var p *Product
	for i := range prods {
		if prods[i].ID == pid {
			p = &prods[i]
			break
		}
	}
	if p == nil {
		return btProduct{}, nil, false
	}
	key := strings.ToLower(p.ID)
	public := b.cfg.public[key]
	bp := btProduct{Key: key, Name: btAlias, Visibility: "hidden"}
	if public {
		bp.Name, bp.Visibility = p.Name, "public"
	}
	sig := bp.Name + "|" + bp.Visibility
	b.mu.Lock()
	changed := b.prods[pid] != sig
	b.prods[pid] = sig
	b.mu.Unlock()
	var outs []btOut
	if changed {
		outs = append(outs, b.mk(e, idx, "product.upserted", bp, nil, nil, pid))
	}
	return bp, outs, true
}

func (b *brainTown) mk(e Event, idx *int, typ string, p btProduct, t *btTask, data map[string]any, pid string) btOut {
	o := btOut{ID: btID(b.bootKey, atTime(e).UnixMilli(), e.Seq, *idx), Type: typ,
		At: atTime(e).Format(time.RFC3339), Product: p, Task: t, Data: data, productID: pid}
	*idx++
	return o
}

// mapEvent turns one committed GAOS event into zero or more receiver events (§3.2).
func (b *brainTown) mapEvent(ctx context.Context, e Event) []btOut {
	idx := 0
	switch e.Type {
	case ProductCreated:
		_, outs, _ := b.productOut(ctx, e.DeskID, e, &idx)
		return outs
	case TaskPlanned:
		bp, outs, ok := b.productOut(ctx, e.DeskID, e, &idx)
		if !ok {
			return nil
		}
		public := bp.Visibility == "public"
		for _, pt := range e.Payload.Tasks {
			t := &btTask{ID: pt.ID, Title: btTitle(public, pt.ID, pt.Title), Kind: kindOrDefault(pt.Kind), Column: ColBacklog}
			b.markTask(pt.ID)
			outs = append(outs, b.mk(e, &idx, "task.created", bp, t, nil, e.DeskID))
		}
		return outs
	case TaskMoved:
		return b.mapMoved(ctx, e, &idx)
	}
	return nil
}

func kindOrDefault(k string) string {
	if k == "" {
		return TaskKindImplement
	}
	return k
}

func (b *brainTown) markTask(id string) {
	b.mu.Lock()
	if len(b.tasks) >= btMaxTasks {
		b.tasks = map[string]bool{} // bounded; everything is re-announced on its next event
	}
	b.tasks[id] = true
	b.mu.Unlock()
}

func (b *brainTown) mapMoved(ctx context.Context, e Event, idx *int) []btOut {
	tid, to := e.Payload.Task, e.Payload.To
	if tid == "" || to == "" {
		return nil
	}
	bp, outs, ok := b.productOut(ctx, e.DeskID, e, idx)
	if !ok {
		return nil
	}
	from := to
	if i := strings.Index(e.Payload.Reason, "→"); i > 0 {
		from = e.Payload.Reason[:i]
	}
	var rec TaskRecord
	if b.task != nil {
		rec, _ = b.task(ctx, tid)
	}
	public := bp.Visibility == "public"
	title := rec.Title
	mkTask := func(col string) *btTask {
		return &btTask{ID: tid, Title: btTitle(public, tid, title), Kind: kindOrDefault(rec.Kind), Column: col}
	}
	b.mu.Lock()
	seen := b.tasks[tid]
	b.mu.Unlock()
	if !seen {
		b.markTask(tid)
		outs = append(outs, b.mk(e, idx, "task.created", bp, mkTask(columnOf(from)), nil, e.DeskID))
	}
	fc, tc := columnOf(from), columnOf(to)
	data := map[string]any{"from": fc, "to": tc}
	if tc == ColWorking && rec.Role != "" {
		data["agent"] = rec.Role
	}
	outs = append(outs, b.mk(e, idx, "task.moved", bp, mkTask(tc), data, e.DeskID))
	add := func(typ string, d map[string]any) {
		outs = append(outs, b.mk(e, idx, typ, bp, mkTask(tc), d, e.DeskID))
	}
	rule := e.Payload.Rule
	if rule == MoveStart {
		d := map[string]any{}
		if rec.Role != "" {
			d["agent"] = rec.Role
		}
		add("task.claimed", d)
	}
	switch to {
	case StatusDone:
		add("task.done", nil)
	case StatusFailed:
		add("task.failed", map[string]any{"reason": "failed"})
	case StatusCanceled:
		add("task.failed", map[string]any{"reason": "canceled"})
	}
	if rec.Kind == TaskKindReview {
		switch {
		case to == StatusReady:
			add("gate.requested", nil)
		case to == StatusDone && rule == RuleReviewCleared:
			add("gate.decided", map[string]any{"decision": "approved"})
		case rule == RuleDesignBlocked:
			add("gate.decided", map[string]any{"decision": "rejected"})
		}
	}
	return outs
}

// ---------------------------------------------------------------- delivery

// deliver sends one event with the retry rules, in order, and records the outcome.
func (b *brainTown) deliver(ctx context.Context, o btOut) {
	body, err := json.Marshal(o)
	if err != nil {
		return
	}
	b.mu.Lock()
	attempts := 1 + len(b.waits)
	if b.degraded >= btDegradeAt {
		attempts = 1
	}
	b.mu.Unlock()

	status := ""
	for a := 0; a < attempts; a++ {
		if a > 0 {
			b.sleep(ctx, b.waits[a-1])
			if ctx.Err() != nil {
				return
			}
		}
		code, class := b.post(ctx, o.ID, body)
		switch {
		case code >= 200 && code < 300, code == http.StatusConflict:
			b.mu.Lock()
			b.degraded = 0
			b.sent++
			b.last = strconv.Itoa(code) + " · " + time.Now().UTC().Format(time.RFC3339)
			b.mu.Unlock()
			return
		case code == 0:
			status = class
		default:
			status = strconv.Itoa(code)
		}
		if code != 0 && code != http.StatusTooManyRequests && code < 500 {
			break // a 4xx other than 429 is never retried
		}
	}
	b.logf("brain town: event %s %s status %s", o.ID, o.Type, status)
	b.mu.Lock()
	b.degraded++
	b.failed++
	b.last = status + " · " + time.Now().UTC().Format(time.RFC3339)
	delete(b.prods, o.productID) // resync: the next event re-announces what the receiver may lack
	if o.Task != nil {
		delete(b.tasks, o.Task.ID)
	}
	b.mu.Unlock()
}

// post makes one request. It returns the HTTP status, or 0 and an error class; it never
// returns a Go error, whose text can carry the URL.
func (b *brainTown) post(ctx context.Context, id string, body []byte) (int, string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.cfg.url.String(), bytes.NewReader(body))
	if err != nil {
		return 0, "other"
	}
	req.Header.Set("Authorization", "Bearer "+b.cfg.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", id)
	resp, err := b.hc.Do(req)
	if err != nil {
		return 0, errClass(err)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, btBodyLimit))
	_ = resp.Body.Close()
	return resp.StatusCode, ""
}

func errClass(err error) string {
	var ne net.Error
	var dns *net.DNSError
	var ua x509.UnknownAuthorityError
	var hn x509.HostnameError
	var ci x509.CertificateInvalidError
	var rh tls.RecordHeaderError
	switch {
	case errors.As(err, &dns):
		return "dns"
	case errors.As(err, &ua), errors.As(err, &hn), errors.As(err, &ci), errors.As(err, &rh):
		return "tls"
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &ne) && ne.Timeout():
		return "timeout"
	case strings.Contains(err.Error(), "refused"):
		return "refused"
	}
	return "other"
}

// ---------------------------------------------------------------- the Integrations row

// Status is the back office's Brain Town row. Host, state, counts; never a token, a path
// or a title.
func (b *brainTown) Status() BrainTownRow {
	if b == nil {
		return BrainTownRow{State: "disabled"}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return BrainTownRow{State: "enabled", Host: urlHost(b.cfg.url.String()), Queue: len(b.in),
		Last: b.last, Failed: int(b.failed), Dropped: int(atomic.LoadInt64(&b.dropped)), Sent: int(b.sent),
		Failing: b.degraded > 0}
}

// btState is the row for a feed that is off or unusable.
func btState(c btConfig) BrainTownRow {
	switch {
	case !c.enabled:
		return BrainTownRow{State: "disabled"}
	case c.why != "":
		return BrainTownRow{State: "unusable: " + c.why}
	}
	return BrainTownRow{State: "enabled"}
}

var _ = fmt.Sprintf
