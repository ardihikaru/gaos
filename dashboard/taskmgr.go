package main

// TaskMgr, the team's task manager, as a product-bound third-party platform (D-104,
// docs/designs/gaos-third-party-taskmgr.md §3.3-§6).
//
// GAOS calls four methods of TaskMgr's gRPC API: UserService/ListUsers, TaskService/GetTask and
// TaskService/AddComment (ListComments is granted but unused). The client is hand-written over
// net/http's HTTP/2 because the dashboard is stdlib-only: gRPC is a POST of one length-prefixed
// protobuf frame, answered by one frame and a grpc-status trailer.
//
// The channel is TLS 1.3 to a pinned self-signed certificate, to the configured IP only: the VPN
// relays through a hub, so a plaintext call would expose the credential and the staff numbers
// there. Plaintext h2c is allowed to a loopback address only, which is what the tests use.
//
// BR-63: the people directory lives in this process's memory. A number is never stored, logged,
// put in a packet, sent to Brain Town or written into an event.

import (
	"bytes"
	"container/list"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	tmPollTimeout    = 3 * time.Second  // a lookup on the GitHub poll path
	tmCallTimeout    = 10 * time.Second // the directory and the write-back
	tmMaxAnswer      = 1 << 20          // one answer, whole
	tmRefreshEvery   = 15 * time.Minute
	tmCheckFresh     = 30 * time.Minute
	tmCheckEntries   = 512
	tmAuthLatch      = time.Hour
	tmOpenAfter      = 3 // consecutive unavailable answers
	tmBackoffFirst   = time.Minute
	tmBackoffCap     = 30 * time.Minute
	tmPageSize       = 100
	tmMaxPages       = 20
	tmCommentRunes   = 1500 // TaskMgr's AddComment limit
	tmCommentURLs    = 10
	tmCommentCodes   = 20
	alertUnknownTask = "unknown-task-code"
)

// The error classes of a call. None carries TaskMgr's own message (grpc-message is never read).
var (
	errTMNotFound    = errors.New("taskmgr: not found")
	errTMAuth        = errors.New("taskmgr: the credential was refused")
	errTMInvalid     = errors.New("taskmgr: the request was refused as invalid")
	errTMUnavailable = errors.New("taskmgr: unavailable")
	errTMPaused      = errors.New("taskmgr: calls are paused after failures")
)

// tmErrorClass is a log- and page-safe name for an error.
func tmErrorClass(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, errTMNotFound):
		return "not-found"
	case errors.Is(err, errTMAuth):
		return "auth"
	case errors.Is(err, errTMInvalid):
		return "invalid"
	case errors.Is(err, errTMPaused):
		return "paused"
	}
	return "unavailable"
}

// ---------------------------------------------------------------- settings (§3.3)

type taskmgrConfig struct {
	addr     string // ip:port; "" is off
	ip       net.IP
	port     string
	plain    bool // loopback without a CA: h2c, tests only
	pool     *x509.CertPool
	authPath string
	user     string
	pass     string
	off      string // why it is off although an address is set (an empty credential file)
	why      string // non-empty: set but unusable
}

func (c taskmgrConfig) on() bool { return c.addr != "" && c.why == "" && c.off == "" }

// readTaskMgrAuth reads the credential file: a regular file, no group or other bits, one line
// user:password. An empty file answers ("", "", "") and means off.
func readTaskMgrAuth(path string) (user, pass, why string) {
	st, err := os.Stat(path)
	if err != nil || !st.Mode().IsRegular() {
		return "", "", "the TaskMgr credential file cannot be read"
	}
	if st.Mode().Perm()&0o077 != 0 {
		return "", "", "the TaskMgr credential file is readable by group or others; it must be mode 0600"
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", "", "the TaskMgr credential file cannot be read"
	}
	line := strings.TrimSpace(string(b))
	if line == "" {
		return "", "", ""
	}
	u, p, ok := strings.Cut(line, ":")
	if !ok || u == "" || p == "" || strings.ContainsAny(line, "\r\n") {
		return "", "", "the TaskMgr credential file holds no user:password line"
	}
	return u, p, ""
}

func fileMod(path string) time.Time {
	if st, err := os.Stat(path); err == nil {
		return st.ModTime()
	}
	return time.Time{}
}

func taskmgrConfigFromEnv() taskmgrConfig {
	c := taskmgrConfig{addr: strings.TrimSpace(os.Getenv("GAOS_TASKMGR_GRPC_ADDR"))}
	if c.addr == "" {
		return c
	}
	host, port, err := net.SplitHostPort(c.addr)
	if n, perr := strconv.Atoi(port); err != nil || perr != nil || n <= 0 || n > 65535 {
		c.why = "GAOS_TASKMGR_GRPC_ADDR must be ip:port"
		return c
	}
	c.ip, c.port = net.ParseIP(host), port
	if c.ip == nil {
		c.why = "GAOS_TASKMGR_GRPC_ADDR must be an IP literal, not a host name"
		return c
	}
	if ca := strings.TrimSpace(os.Getenv("GAOS_TASKMGR_CA_FILE")); ca != "" {
		pem, err := os.ReadFile(ca)
		if err != nil {
			c.why = "the TaskMgr certificate file cannot be read"
			return c
		}
		c.pool = x509.NewCertPool()
		if !c.pool.AppendCertsFromPEM(pem) {
			c.why = "the TaskMgr certificate file holds no PEM certificate"
			return c
		}
	} else if c.ip.IsLoopback() {
		c.plain = true
	} else {
		c.why = "TaskMgr is reached over TLS only: set GAOS_TASKMGR_CA_FILE"
		return c
	}
	c.authPath = strings.TrimSpace(os.Getenv("GAOS_TASKMGR_AUTH_FILE"))
	if c.authPath == "" {
		c.why = "GAOS_TASKMGR_AUTH_FILE is not set"
		return c
	}
	u, p, why := readTaskMgrAuth(c.authPath)
	switch {
	case why != "":
		c.why = why
	case u == "":
		c.off = "the TaskMgr credential file is empty"
	default:
		c.user, c.pass = u, p
	}
	return c
}

// ---------------------------------------------------------------- the client (§4)

type taskmgrClient struct {
	cfg  taskmgrConfig
	hc   *http.Client
	base string
	now  func() time.Time

	mu        sync.Mutex
	user      string
	pass      string
	authOpen  bool
	authAt    time.Time
	authMod   time.Time
	credMod   time.Time // the credential file's time when user/pass were loaded
	fails     int
	backoff   time.Duration
	openUntil time.Time
	last      string // the last failure's class
}

// newTaskMgrClient builds the client; nil when the settings are off or unusable.
func newTaskMgrClient(cfg taskmgrConfig) *taskmgrClient {
	if !cfg.on() {
		return nil
	}
	want := net.JoinHostPort(cfg.ip.String(), cfg.port)
	d := &net.Dialer{Timeout: tmCallTimeout}
	tr := &http.Transport{
		// Only the configured address is ever dialled.
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil || !net.ParseIP(host).Equal(cfg.ip) || port != cfg.port {
				return nil, errors.New("taskmgr: refused to dial an address other than the configured one")
			}
			return d.DialContext(ctx, network, want)
		},
		MaxResponseHeaderBytes: 64 << 10,
	}
	var p http.Protocols
	scheme := "https"
	if cfg.plain {
		p.SetUnencryptedHTTP2(true)
		scheme = "http"
	} else {
		p.SetHTTP2(true)
		tr.TLSClientConfig = &tls.Config{RootCAs: cfg.pool, ServerName: cfg.ip.String(), MinVersion: tls.VersionTLS13}
	}
	tr.Protocols = &p
	return &taskmgrClient{cfg: cfg, base: scheme + "://" + want, now: time.Now, user: cfg.user, pass: cfg.pass,
		credMod: fileMod(cfg.authPath),
		hc: &http.Client{Transport: tr, CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("taskmgr: redirects are refused")
		}}}
}

// allow is the circuit breaker (§4.3): closed, or open on auth until the credential file
// changes or an hour passes, or open on unavailability for the backoff.
func (c *taskmgrClient) allow() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if !c.authClearLocked(now) {
		return false
	}
	return !now.Before(c.openUntil)
}

// authClearLocked closes the auth latch once the credential file differs from the one that
// failed, or the latch has run an hour; it reloads the credential then. It reports whether the
// latch is closed. Callers hold c.mu.
func (c *taskmgrClient) authClearLocked(now time.Time) bool {
	if !c.authOpen {
		return true
	}
	mod := fileMod(c.cfg.authPath)
	if mod.Equal(c.authMod) && now.Sub(c.authAt) < tmAuthLatch {
		return false
	}
	if u, p, why := readTaskMgrAuth(c.cfg.authPath); why == "" && u != "" {
		c.user, c.pass, c.credMod = u, p, mod
	}
	c.authOpen = false
	return true
}

func (c *taskmgrClient) record(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	switch {
	case err == nil || errors.Is(err, errTMNotFound) || errors.Is(err, errTMInvalid):
		c.fails, c.backoff, c.last = 0, 0, ""
	case errors.Is(err, errTMAuth):
		// The latch keys on the file the failing credential came from, so a file already
		// rewritten before the failure reloads on the next check.
		c.authOpen, c.authAt, c.authMod, c.last = true, now, c.credMod, "auth"
	default:
		c.fails++
		c.last = "unavailable"
		if c.fails >= tmOpenAfter {
			if c.backoff == 0 {
				c.backoff = tmBackoffFirst
			} else {
				c.backoff = min(2*c.backoff, tmBackoffCap)
			}
			c.openUntil = now.Add(c.backoff)
		}
	}
}

// breaker names the breaker's state: "" closed, "auth" or "unavailable" open.
func (c *taskmgrClient) breaker() string {
	if c == nil {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	switch {
	case !c.authClearLocked(now):
		return "auth"
	case now.Before(c.openUntil):
		return "unavailable"
	}
	return ""
}

// call is one unary call: one frame out, one frame back, the status from the trailers.
func (c *taskmgrClient) call(ctx context.Context, path string, req []byte, timeout time.Duration) (pbMsg, error) {
	if c == nil {
		return nil, errTMUnavailable
	}
	if !c.allow() {
		return nil, errTMPaused
	}
	c.mu.Lock()
	user, pass := c.user, c.pass
	c.mu.Unlock()
	m, err := c.do(ctx, path, req, timeout, user, pass)
	c.record(err)
	return m, err
}

// tmOutcome is a call's outcome for its traffic record (D-107 §7.6): failed
// means auth or unavailable, the codes the breaker counts as failures.
func tmOutcome(err error) string {
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, errTMNotFound):
		return "notfound"
	case errors.Is(err, errTMInvalid):
		return "invalid"
	case errors.Is(err, errTMAuth):
		return "auth"
	}
	return "unavailable"
}

func (c *taskmgrClient) do(ctx context.Context, path string, req []byte, timeout time.Duration, user, pass string) (m pbMsg, err error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	frame := make([]byte, 5, 5+len(req))
	binary.BigEndian.PutUint32(frame[1:], uint32(len(req)))
	frame = append(frame, req...)
	// D-107 §7.6: one traffic record on every return path, with the raw gRPC code
	// read (-1 when none) and the outcome mapped from the error do returns.
	var trace *trafficOut
	if rec := trafficRec(); rec.on() {
		trace = &trafficOut{label: "taskmgr", start: time.Now(), method: http.MethodPost, grpc: -1, rb: -1,
			qb: int64(len(frame))}
		if u, perr := url.Parse(c.base + path); perr == nil {
			trace.host, trace.path = u.Host, trafficPath("", u.EscapedPath())
		}
		defer func() {
			trace.dur, trace.outcome = time.Since(trace.start), tmOutcome(err)
			rec.out(*trace)
		}()
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(frame))
	if err != nil {
		return nil, errTMUnavailable
	}
	r.Header.Set("content-type", "application/grpc")
	r.Header.Set("te", "trailers")
	r.Header.Set("authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(user+":"+pass)))
	resp, err := c.hc.Do(r)
	if err != nil {
		if trace != nil {
			trace.errCls = trafficErrClass(err)
		}
		return nil, errTMUnavailable
	}
	defer resp.Body.Close()
	if trace != nil {
		trace.status, trace.rb = resp.StatusCode, resp.ContentLength
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, tmMaxAnswer+1))
	if err != nil || len(body) > tmMaxAnswer || resp.StatusCode != http.StatusOK {
		return nil, errTMUnavailable
	}
	code := resp.Trailer.Get("grpc-status")
	if code == "" {
		code = resp.Header.Get("grpc-status") // a trailers-only answer
	}
	if n, cerr := strconv.Atoi(code); cerr == nil && trace != nil && n >= 0 {
		trace.grpc = n
	}
	switch code {
	case "0":
	case "5":
		return nil, errTMNotFound
	case "7", "8", "16":
		return nil, errTMAuth
	case "3":
		return nil, errTMInvalid
	default:
		return nil, errTMUnavailable
	}
	// Exactly one uncompressed frame, or nothing is decoded.
	if len(body) < 5 || body[0] != 0 || int(binary.BigEndian.Uint32(body[1:5])) != len(body)-5 {
		return nil, errTMUnavailable
	}
	m, err = pbParse(body[5:])
	if err != nil {
		return nil, errTMUnavailable
	}
	return m, nil
}

// tmPerson is a cached TaskMgr user.
type tmPerson struct {
	ID, Username, Name, Role, GitHub, WhatsApp string
	Active, WAActive, Checked                  bool
}

var tmNumberPattern = regexp.MustCompile(`^[1-9][0-9]{6,14}$`)

// usable reports whether a WhatsApp line may be sent to the person.
func (p tmPerson) usable() bool { return p.WAActive && tmNumberPattern.MatchString(p.WhatsApp) }

func (p tmPerson) jid() string { return p.WhatsApp + "@s.whatsapp.net" }

// listUsers reads one page of active users and the total.
func (c *taskmgrClient) listUsers(ctx context.Context, page int) ([]tmPerson, int, error) {
	var pg, w pbWriter
	pg.uvarint(1, uint64(page))
	pg.uvarint(2, tmPageSize)
	w.boolean(2, true)
	w.bytes(4, pg.b)
	m, err := c.call(ctx, "/taskmgr.v1.UserService/ListUsers", w.b, tmCallTimeout)
	if err != nil {
		return nil, 0, err
	}
	us, err := m.msgs(1)
	if err != nil {
		return nil, 0, errTMUnavailable
	}
	info, err := m.msg(2)
	if err != nil {
		return nil, 0, errTMUnavailable
	}
	out := make([]tmPerson, 0, len(us))
	for _, u := range us {
		out = append(out, tmPerson{ID: u.str(1), Username: u.str(2), Name: u.str(3), Role: u.str(4),
			Active: u.boolean(11), WhatsApp: u.str(14), WAActive: u.boolean(15), GitHub: u.str(16), Checked: u.boolean(19)})
	}
	return out, int(info.uint(3)), nil
}

// tmTaskRow is the part of a TaskMgr task GAOS reads.
type tmTaskRow struct {
	ID, Status, ProductCode, QAID string
	Deleted                       bool
}

func (c *taskmgrClient) getTask(ctx context.Context, code string, timeout time.Duration) (tmTaskRow, error) {
	var w pbWriter
	w.str(1, code)
	m, err := c.call(ctx, "/taskmgr.v1.TaskService/GetTask", w.b, timeout)
	if err != nil {
		return tmTaskRow{}, err
	}
	t, err := m.msg(1)
	if err != nil {
		return tmTaskRow{}, errTMUnavailable
	}
	qa, err := t.msg(21)
	if err != nil {
		return tmTaskRow{}, errTMUnavailable
	}
	return tmTaskRow{ID: t.str(1), Status: t.str(5), ProductCode: t.str(10), QAID: qa.str(1), Deleted: t.boolean(37)}, nil
}

func (c *taskmgrClient) addComment(ctx context.Context, code, text string) error {
	var w pbWriter
	w.str(1, code)
	w.str(2, text)
	_, err := c.call(ctx, "/taskmgr.v1.TaskService/AddComment", w.b, tmCallTimeout)
	return err
}

// ---------------------------------------------------------------- the directory (§6)

type tmCounts struct{ People, Linked, Usable, Conflicts int }

type taskmgrPeople struct {
	cl      *taskmgrClient
	enabled func(ctx context.Context) bool
	now     func() time.Time
	logf    func(string, ...any)
	kick    chan struct{}

	mu      sync.RWMutex
	byID    map[string]tmPerson
	byLogin map[string]tmPerson
	counts  tmCounts
	lastOK  time.Time
	lastErr string
	loaded  bool
}

func newTaskMgrPeople(cl *taskmgrClient, enabled func(context.Context) bool) *taskmgrPeople {
	return &taskmgrPeople{cl: cl, enabled: enabled, now: time.Now, logf: func(string, ...any) {}, kick: make(chan struct{}, 1)}
}

// refresh reads every page. A failure keeps the directory it had.
func (d *taskmgrPeople) refresh(ctx context.Context) error {
	var all []tmPerson
	for page := 1; page <= tmMaxPages; page++ {
		ps, total, err := d.cl.listUsers(ctx, page)
		if err != nil {
			d.mu.Lock()
			d.lastErr = tmErrorClass(err)
			d.mu.Unlock()
			return err
		}
		all = append(all, ps...)
		if len(ps) == 0 || len(all) >= total {
			break
		}
	}
	byID, byLogin := map[string]tmPerson{}, map[string]tmPerson{}
	holders := map[string]int{}
	var n tmCounts
	for _, p := range all {
		if !p.Active || p.ID == "" {
			continue
		}
		byID[p.ID] = p
		n.People++
		if p.usable() {
			n.Usable++
		}
		if l := strings.ToLower(strings.TrimSpace(p.GitHub)); p.Checked && l != "" {
			n.Linked++
			holders[l]++
			byLogin[l] = p
		}
	}
	for l, k := range holders {
		if k > 1 {
			delete(byLogin, l) // a login two people hold routes to nobody
			n.Conflicts++
		}
	}
	d.mu.Lock()
	d.byID, d.byLogin, d.counts, d.lastOK, d.lastErr, d.loaded = byID, byLogin, n, d.now(), "", true
	d.mu.Unlock()
	return nil
}

// clear forgets every person: no binding is enabled.
func (d *taskmgrPeople) clear() {
	d.mu.Lock()
	d.byID, d.byLogin, d.counts, d.lastOK, d.lastErr, d.loaded = nil, nil, tmCounts{}, time.Time{}, "", false
	d.mu.Unlock()
}

// Kick asks Run for a pass now (a binding changed).
func (d *taskmgrPeople) Kick() {
	if d == nil {
		return
	}
	select {
	case d.kick <- struct{}{}:
	default:
	}
}

// Run refreshes every 15 minutes while a binding is enabled.
func (d *taskmgrPeople) Run(ctx context.Context) {
	t := time.NewTicker(tmRefreshEvery)
	defer t.Stop()
	for {
		if d.enabled(ctx) {
			if err := d.refresh(ctx); err != nil {
				d.logf("taskmgr: the people directory could not be refreshed: %s", tmErrorClass(err))
			}
		} else {
			d.clear()
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-d.kick:
		}
	}
}

func (d *taskmgrPeople) person(id string) (tmPerson, bool) {
	if d == nil {
		return tmPerson{}, false
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	p, ok := d.byID[id]
	return p, ok
}

func (d *taskmgrPeople) login(l string) (tmPerson, bool) {
	if d == nil {
		return tmPerson{}, false
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	p, ok := d.byLogin[strings.ToLower(strings.TrimSpace(l))]
	return p, ok
}

// tmSnapshot is the directory as the Integrations view reads it.
type tmSnapshot struct {
	Counts  tmCounts
	LastOK  time.Time
	LastErr string
	Loaded  bool
	People  []tmPerson
}

func (d *taskmgrPeople) snapshot() tmSnapshot {
	if d == nil {
		return tmSnapshot{}
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	s := tmSnapshot{Counts: d.counts, LastOK: d.lastOK, LastErr: d.lastErr, Loaded: d.loaded}
	for _, p := range d.byID {
		s.People = append(s.People, p)
	}
	sort.Slice(s.People, func(i, j int) bool { return s.People[i].Username < s.People[j].Username })
	return s
}

// ---------------------------------------------------------------- the validation cache (§5.1)

type taskCheck struct {
	Result string // "ok" | "unknown"
	QAID   string
	At     time.Time
}

type taskChecks struct {
	mu  sync.Mutex
	max int
	ll  *list.List
	m   map[string]*list.Element
}

type taskCheckEntry struct {
	key string
	v   taskCheck
}

func newTaskChecks(max int) *taskChecks {
	return &taskChecks{max: max, ll: list.New(), m: map[string]*list.Element{}}
}

func taskCheckKey(productCode, code string) string { return productCode + "|" + code }

func (c *taskChecks) get(key string, now time.Time) (taskCheck, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[key]
	if !ok {
		return taskCheck{}, false
	}
	v := e.Value.(*taskCheckEntry).v
	if now.Sub(v.At) >= tmCheckFresh {
		c.ll.Remove(e)
		delete(c.m, key)
		return taskCheck{}, false
	}
	c.ll.MoveToFront(e)
	return v, true
}

func (c *taskChecks) put(key string, v taskCheck) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.m[key]; ok {
		e.Value.(*taskCheckEntry).v = v
		c.ll.MoveToFront(e)
		return
	}
	c.m[key] = c.ll.PushFront(&taskCheckEntry{key: key, v: v})
	for c.ll.Len() > c.max {
		old := c.ll.Back()
		c.ll.Remove(old)
		delete(c.m, old.Value.(*taskCheckEntry).key)
	}
}

func (c *taskChecks) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ll.Init()
	c.m = map[string]*list.Element{}
}

func (c *taskChecks) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ll.Len()
}

// ---------------------------------------------------------------- the link

// taskmgrLink is everything the uses share: the settings, the client, the directory, the cache.
type taskmgrLink struct {
	st     Store
	cfg    taskmgrConfig
	cl     *taskmgrClient // nil: the settings are off or unusable
	people *taskmgrPeople
	checks *taskChecks
	now    func() time.Time
}

func newTaskMgrLink(st Store, cfg taskmgrConfig) *taskmgrLink {
	l := &taskmgrLink{st: st, cfg: cfg, cl: newTaskMgrClient(cfg), checks: newTaskChecks(tmCheckEntries), now: time.Now}
	l.people = newTaskMgrPeople(l.cl, func(ctx context.Context) bool { _, _, ok := l.anyBinding(ctx); return ok })
	return l
}

// binding is the product's enabled TaskMgr binding.
func (l *taskmgrLink) binding(m Master) (ThirdPartyLink, bool) {
	b, ok := thirdPartyOf(m, ThirdPartyTaskMgr)
	return b, ok && b.Enabled
}

// anyBinding is the instance's enabled binding, if one exists.
func (l *taskmgrLink) anyBinding(ctx context.Context) (Master, ThirdPartyLink, bool) {
	if l == nil || l.st == nil {
		return Master{}, ThirdPartyLink{}, false
	}
	ps, err := l.st.Products(ctx)
	if err != nil {
		return Master{}, ThirdPartyLink{}, false
	}
	for _, p := range ps {
		if m, err := l.st.Master(ctx, p.ID); err == nil {
			if b, ok := l.binding(m); ok {
				return m, b, true
			}
		}
	}
	return Master{}, ThirdPartyLink{}, false
}

// ready is taskmgrReady (§2.1): usable settings, a closed breaker, an enabled binding, and the
// product listed in GAOS_PR_TEMPLATE_PRODUCTS.
func (l *taskmgrLink) ready(m Master) (ThirdPartyLink, bool) {
	if l == nil || l.cl == nil || l.cl.breaker() != "" {
		return ThirdPartyLink{}, false
	}
	b, ok := l.binding(m)
	if !ok || !templateProducts()[m.ID] {
		return ThirdPartyLink{}, false
	}
	return b, true
}

// check looks a code up: from the cache unless fresh, else one GetTask. An error leaves the code
// unchecked; it is never assumed valid.
func (l *taskmgrLink) check(ctx context.Context, b ThirdPartyLink, code string, fresh bool) (taskCheck, error) {
	key := taskCheckKey(b.ProductCode, code)
	now := l.now()
	if !fresh {
		if v, ok := l.checks.get(key, now); ok {
			return v, nil
		}
	}
	timeout := tmPollTimeout
	if fresh {
		timeout = tmCallTimeout
	}
	t, err := l.cl.getTask(ctx, code, timeout)
	var v taskCheck
	switch {
	case errors.Is(err, errTMNotFound):
		v = taskCheck{Result: "unknown", At: now}
	case err != nil:
		return taskCheck{}, err
	case t.Deleted || strings.EqualFold(t.Status, "DELETED") || strings.EqualFold(t.Status, "ARCHIVED") ||
		t.ProductCode != b.ProductCode:
		v = taskCheck{Result: "unknown", At: now}
	default:
		v = taskCheck{Result: "ok", QAID: t.QAID, At: now}
	}
	l.checks.put(key, v)
	return v, nil
}
