package main

// traffic.go records GAOS's outbound calls to external platforms and the calls
// made to it, into capped Redis streams that only the Traffic page reads (D-107,
// design docs/designs/traffic-monitor.md). A record is not an event (BR-66): it
// records no change, nothing decides anything from it, and it may be dropped,
// evicted, trimmed or erased.
//
// Recording never blocks and never fails a call: the hot path builds one record
// and offers it to a bounded queue with a non-blocking send; one writer
// goroutine drains the queue into the store. No record holds a header, a body, a
// query string, a fragment, userinfo, a token, a password, an error's text or a
// configured base path.

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// The store's subnet and the edge hop. trafficSubnet is the traffic network of
// deploy/stacks/compose.prod.yaml; GAOS_REDIS_ADDR must be an address inside
// it. edgeHop is the one entry of Traefik's trust list (deploy/stacks/traefik.yml),
// which check-stacks.sh asserts equal.
var trafficSubnet = netip.MustParsePrefix("172.30.40.0/29")

const edgeHop = "10.10.0.1"

// The constants of §5 and §9. The caps and the window are not settings.
const (
	trafficWindow     = 48 * time.Hour
	trafficQueueLen   = 4096
	trafficBatch      = 128
	trafficFlushEvery = 250 * time.Millisecond
	trafficDial       = 500 * time.Millisecond
	trafficOpDeadline = 2 * time.Second
	trafficTrimEvery  = 60 * time.Second
	trafficBackoff1   = time.Second
	trafficBackoffCap = 30 * time.Second
	trafficSummaryCap = 512
	trafficUser       = "gaos"
)

// trafficPlatform is one outbound client's closed label (§7.1).
type trafficPlatform struct {
	Label, Name string
	Cap         int
}

// trafficPlatforms is the closed set, in the page's order.
var trafficPlatforms = []trafficPlatform{
	{"qa-tool", "QA tool", 5000},
	{"ai-manager", "AI manager", 5000},
	{"brain-town", "Brain Town", 1000},
	{"brain-town-reader", "Brain Town reader", 1000},
	{"github", "GitHub", 3000},
	{"github-write", "GitHub (auto-fix)", 1000},
	{"whatsapp", "WhatsApp gateway", 1000},
	{"taskmgr", "TaskMgr", 1000},
}

// The inbound streams (§5.1, §8.1).
const (
	inDirectAuth  = "in:direct:auth"
	inDirectOther = "in:direct:other"
	inAuth        = "in:auth"
	inAnon        = "in:anon"
)

// trafficInbound is the four inbound streams, in the page's order, with their
// caps and names.
var trafficInbound = []struct {
	Kind, Name string
	Cap        int
}{
	{inDirectAuth, "Direct, authenticated", 2000},
	{inDirectOther, "Direct, other", 2000},
	{inAuth, "Authenticated", 2000},
	{inAnon, "Anonymous", 1000},
}

const trafficKeyPrefix = "gaos:traffic:"

// trafficStream is one of the twelve streams.
type trafficStream struct {
	Kind string // "out:<label>" or "in:…"
	Cap  int
}

func (s trafficStream) key() string { return trafficKeyPrefix + s.Kind }

// trafficStreams lists the twelve streams: eight outbound, four inbound.
func trafficStreams() []trafficStream {
	var out []trafficStream
	for _, p := range trafficPlatforms {
		out = append(out, trafficStream{"out:" + p.Label, p.Cap})
	}
	for _, in := range trafficInbound {
		out = append(out, trafficStream{in.Kind, in.Cap})
	}
	return out
}

func trafficCap(kind string) int {
	for _, s := range trafficStreams() {
		if s.Kind == kind {
			return s.Cap
		}
	}
	return 0
}

func trafficPlatformKnown(label string) bool {
	for _, p := range trafficPlatforms {
		if p.Label == label {
			return true
		}
	}
	return false
}

// ---------- the settings ----------

// trafficSettings validates GAOS_REDIS_ADDR and GAOS_REDIS_AUTH_FILE (§4.3). It
// answers the address and the password, or why recording is off. The reason
// never carries the password.
func trafficSettings(addr, authFile string) (string, string, string) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "", "", "GAOS_REDIS_ADDR is not set"
	}
	ap, err := netip.ParseAddrPort(addr)
	if err != nil {
		return "", "", "GAOS_REDIS_ADDR is not an IP address with a numeric port"
	}
	if !trafficSubnet.Contains(ap.Addr()) {
		return "", "", "GAOS_REDIS_ADDR is outside " + trafficSubnet.String()
	}
	authFile = strings.TrimSpace(authFile)
	if authFile == "" {
		return "", "", "GAOS_REDIS_AUTH_FILE is not set"
	}
	st, err := os.Stat(authFile)
	if err != nil || !st.Mode().IsRegular() {
		return "", "", "the traffic store's password file cannot be read"
	}
	if st.Mode().Perm()&0o077 != 0 {
		return "", "", "the traffic store's password file is readable by group or others; it must be mode 0600"
	}
	b, err := os.ReadFile(authFile)
	if err != nil {
		return "", "", "the traffic store's password file cannot be read"
	}
	pass := strings.TrimSpace(string(b))
	if pass == "" {
		return "", "", "the traffic store's password file is empty"
	}
	return ap.String(), pass, ""
}

// ---------- the recorder ----------

// trafficRecord is one record: its stream and its fields, in order.
type trafficRecord struct {
	stream string
	fields []string // name, value, name, value, …
}

// trafficRecorder is the one recorder of the process. Its handle is the
// package-level pointer below; the server gets no field.
type trafficRecorder struct {
	why  string // non-empty: recording is off, and why
	addr string
	pass string

	q       chan trafficRecord
	started time.Time
	now     func() time.Time
	logf    func(string, ...any)

	written, dropped, droppedAnon atomic.Int64

	mu           sync.Mutex
	firstWriteAt time.Time // the first successful write after the latest (re)connect
	lastErrAt    time.Time
	down         bool  // the store is not answering; one log line per transition
	downDropped  int64 // dropped while down, for the "answers again" line

	sum *directSummary

	// noWriter leaves the writer unstarted (the test hook of §9.2).
	noWriter bool

	// rd is the page's reader and its cache (traffic_view.go).
	rd trafficReader
}

// trafficPtr is the one handle (§7.2): set in serve(), read by the gate,
// taskmgrClient.do, trafficWrap and the page.
var trafficPtr atomic.Pointer[trafficRecorder]

func trafficRec() *trafficRecorder { return trafficPtr.Load() }

// newTrafficRecorder builds a recorder that records to addr with pass. An empty
// why turns it on.
func newTrafficRecorder(addr, pass, why string) *trafficRecorder {
	return &trafficRecorder{why: why, addr: addr, pass: pass,
		q: make(chan trafficRecord, trafficQueueLen), started: time.Now(), now: time.Now,
		logf: log.Printf, sum: newDirectSummary(trafficSummaryCap)}
}

// trafficFromEnv reads the two settings and says once what it found. It never
// prints a recorded value or the password.
func trafficFromEnv() *trafficRecorder {
	addr, pass, why := trafficSettings(os.Getenv("GAOS_REDIS_ADDR"), os.Getenv("GAOS_REDIS_AUTH_FILE"))
	t := newTrafficRecorder(addr, pass, why)
	if why != "" {
		log.Printf("traffic: off (%s)", why)
	} else {
		log.Printf("traffic: recording to %s, %d h", addr, int(trafficWindow.Hours()))
	}
	return t
}

// on reports whether records are written.
func (t *trafficRecorder) on() bool { return t != nil && t.why == "" }

// start runs the writer on ctx unless recording is off or the test hook holds it.
func (t *trafficRecorder) start(ctx context.Context) {
	if !t.on() || t.noWriter {
		return
	}
	go t.run(ctx)
}

// enqueue offers one record. It never blocks: a full queue drops and counts. A
// record bound for in:anon is offered only while the queue is below ¾ full, so
// an anonymous flood cannot crowd out the others (§8.5).
func (t *trafficRecorder) enqueue(r trafficRecord) {
	if r.stream == inAnon && len(t.q) >= trafficQueueLen*3/4 {
		t.droppedAnon.Add(1)
		return
	}
	select {
	case t.q <- r:
	default:
		if r.stream == inAnon {
			t.droppedAnon.Add(1)
		} else {
			t.dropped.Add(1)
		}
	}
}

// trafficMethod is the closed method set (§5.2).
func trafficMethod(m string) string {
	switch m {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS":
		return m
	}
	return "OTHER"
}

func clipBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func itoa64(n int64) string { return strconv.FormatInt(n, 10) }

// ---------- outbound ----------

// trafficOut is one outbound call's record fields, read from the request and
// the response only as §7.3 lists.
type trafficOut struct {
	label   string
	start   time.Time
	dur     time.Duration
	method  string
	host    string
	path    string // templated
	qb, rb  int64
	status  int
	errCls  string
	grpc    int    // taskmgr only: the raw code, -1 when none
	outcome string // taskmgr only
}

func (t *trafficRecorder) out(o trafficOut) {
	if !t.on() {
		return
	}
	f := []string{
		"t", itoa64(o.start.UnixMilli()),
		"m", trafficMethod(o.method),
		"s", strconv.Itoa(o.status),
		"d", itoa64(o.dur.Milliseconds()),
		"qb", itoa64(o.qb),
		"rb", itoa64(o.rb),
		"h", clipBytes(strings.ToLower(o.host), 100),
		"u", o.path,
		"e", o.errCls,
	}
	if o.label == "taskmgr" {
		f = append(f, "g", strconv.Itoa(o.grpc), "x", o.outcome)
	}
	t.enqueue(trafficRecord{stream: "out:" + o.label, fields: f})
}

// tmFailed is the rule for a failed TaskMgr call (§7.6): the outcomes the
// client itself treats as failures. notfound and invalid are answers.
func tmFailed(outcome string) bool { return outcome == "auth" || outcome == "unavailable" }

// recordingTransport records one outbound record per round trip. The request,
// the response and the error go back unchanged; the body is not wrapped, so a
// streaming response is one record at its headers.
type recordingTransport struct {
	rt       http.RoundTripper
	label    string
	basePath string
	rec      *trafficRecorder
}

func (rt *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()
	resp, err := rt.rt.RoundTrip(req)
	o := trafficOut{label: rt.label, start: start, dur: time.Since(start), method: req.Method,
		host: req.URL.Host, path: trafficPath(rt.basePath, req.URL.EscapedPath()),
		qb: req.ContentLength, rb: -1}
	if req.ContentLength == 0 && req.Body != nil && req.Body != http.NoBody {
		o.qb = -1
	}
	if err != nil {
		o.errCls = trafficErrClass(err)
	} else if resp != nil {
		o.status, o.rb = resp.StatusCode, resp.ContentLength
	}
	rt.rec.out(o)
	return resp, err
}

// CloseIdleConnections reaches the wrapped transport, as http.Client expects.
func (rt *recordingTransport) CloseIdleConnections() {
	if c, ok := rt.rt.(interface{ CloseIdleConnections() }); ok {
		c.CloseIdleConnections()
	}
}

// trafficWrap wraps an outbound client's transport. With recording off it
// returns rt unchanged (nil stays nil: http.DefaultTransport). A label outside
// the closed set is a programming error and panics at construction.
func trafficWrap(label, base string, rt http.RoundTripper) http.RoundTripper {
	if !trafficPlatformKnown(label) {
		panic("traffic: unknown platform label " + strconv.Quote(label))
	}
	rec := trafficRec()
	if !rec.on() {
		return rt
	}
	if rt == nil {
		rt = http.DefaultTransport
	}
	return &recordingTransport{rt: rt, label: label, basePath: trafficBasePath(base), rec: rec}
}

// trafficBasePath is a configured base URL's path, without a trailing slash.
func trafficBasePath(base string) string {
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil {
		return ""
	}
	return strings.TrimRight(u.EscapedPath(), "/")
}

// trafficPath templates an outbound path (§7.4): relative to the base, its
// free-form tails replaced, every remaining segment kept only when it is a
// short word.
func trafficPath(basePath, p string) string {
	prefix := ""
	if basePath != "" {
		switch {
		case p == basePath:
			p = ""
		case strings.HasPrefix(p, basePath+"/"):
			p = p[len(basePath):]
		default:
			return "{other}"
		}
		prefix = "{base}"
	}
	p = strings.TrimPrefix(p, "/")
	var segs []string
	if p != "" {
		for _, seg := range strings.Split(p, "/") {
			if len(segs) == 12 {
				break
			}
			switch seg {
			case "branches":
				segs = append(segs, seg, "{ref}")
			case "contents":
				segs = append(segs, seg, "{path}")
			default:
				segs = append(segs, trafficSegment(seg))
				continue
			}
			break
		}
	}
	out := prefix + "/" + strings.Join(segs, "/")
	if prefix != "" && len(segs) == 0 {
		out = prefix
	}
	return clipBytes(out, 160)
}

// trafficSegment keeps a segment that starts with a letter, is at most 40 bytes
// of letters, digits, '.', '_' and '-', and holds at most three digits; all
// digits become {n}; anything else {id}. An empty segment stays empty.
func trafficSegment(s string) string {
	if s == "" {
		return ""
	}
	digits := 0
	allDigits := true
	ok := len(s) <= 40
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
			digits++
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '.', c == '_', c == '-':
			allDigits = false
		default:
			allDigits, ok = false, false
		}
	}
	if allDigits {
		return "{n}"
	}
	first := s[0]
	if ok && digits <= 3 && (first >= 'a' && first <= 'z' || first >= 'A' && first <= 'Z') {
		return s
	}
	return "{id}"
}

// trafficErrClass is an outbound error's class (§7.5), in order. The error's text is
// never stored.
func trafficErrClass(err error) string {
	if err == nil {
		return ""
	}
	var ne net.Error
	var dns *net.DNSError
	var cv *tls.CertificateVerificationError
	var ua x509.UnknownAuthorityError
	var hn x509.HostnameError
	var ci x509.CertificateInvalidError
	var rh tls.RecordHeaderError
	var al tls.AlertError
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &ne) && ne.Timeout():
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.As(err, &dns):
		return "dns"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "refused"
	case errors.Is(err, syscall.ECONNRESET), errors.Is(err, syscall.EPIPE), errors.Is(err, io.EOF),
		errors.Is(err, io.ErrUnexpectedEOF):
		return "reset"
	case errors.As(err, &cv), errors.As(err, &ua), errors.As(err, &hn), errors.As(err, &ci),
		errors.As(err, &rh), errors.As(err, &al):
		return "tls"
	}
	return "other"
}

// ---------- inbound ----------

// trafficTagKey holds the request's *trafficTag in its context.
type trafficTagKey struct{}

// trafficTag is what the handlers tell the recorder: the principal that
// authenticated, set only after authentication succeeded.
type trafficTag struct {
	mu        sync.Mutex
	principal string
}

// trafficPrincipal names the request's principal (§8.4). A name is cut to 64
// bytes. It reads nothing from the request but its context.
func trafficPrincipal(r *http.Request, name string) {
	tag, _ := r.Context().Value(trafficTagKey{}).(*trafficTag)
	if tag == nil || name == "" {
		return
	}
	tag.mu.Lock()
	tag.principal = clipBytes(name, 64)
	tag.mu.Unlock()
}

func (tag *trafficTag) get() string {
	if tag == nil {
		return ""
	}
	tag.mu.Lock()
	defer tag.mu.Unlock()
	return tag.principal
}

// trafficVia is "edge" when the last X-Forwarded-For entry -- the hop that
// delivered the request to Traefik -- is the edge hop; otherwise "direct". A
// request with no X-Forwarded-For did not pass through Traefik, so it is direct.
func trafficVia(r *http.Request) string {
	xff := r.Header.Values("X-Forwarded-For")
	if len(xff) == 0 {
		return "direct"
	}
	hops := strings.Split(strings.Join(xff, ","), ",")
	if a, ok := parseSourceAddr(hops[len(hops)-1]); ok && a == netip.MustParseAddr(edgeHop) {
		return "edge"
	}
	return "direct"
}

// The route classes (§8.1).
const (
	kAPI        = "api"
	kBackOffice = "backoffice"
	kPublic     = "public"
	kHealthz    = "healthz"
	kStatic     = "static"
	kUnmatched  = "unmatched"
)

// trafficClass is the route class of a request whose matched pattern is pat.
func trafficClass(pat, path string, bearer map[string]bool) string {
	switch {
	case bearer[pat], pat == "" && strings.HasPrefix(path, "/v1/"):
		return kAPI
	case pat == "GET /healthz":
		return kHealthz
	case pat == "GET /static/":
		return kStatic
	case publicPatterns[pat]:
		return kPublic
	case pat == "":
		return kUnmatched
	}
	return kBackOffice
}

// trafficIn is one inbound request, as the gate saw it.
type trafficIn struct {
	start     time.Time
	dur       time.Duration
	method    string
	status    int
	qb, rb    int64
	class     string
	pattern   string // the matched pattern, "" when none
	src, via  string
	principal string
	pid       string
}

var pidRe = func(s string) bool {
	if len(s) < 3 || len(s) > 16 || !strings.HasPrefix(s, "p-") {
		return false
	}
	for i := 2; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// in records one inbound request (§8.1) and updates the direct-source summary.
func (t *trafficRecorder) in(x trafficIn) {
	if t == nil {
		return
	}
	direct := x.via != "edge"
	if direct {
		t.sum.see(x.src, x.class, x.principal != "", x.start)
	}
	if !t.on() {
		return
	}
	api := x.class == kAPI
	var stream string
	switch {
	case direct && api && x.principal != "":
		stream = inDirectAuth
	case direct:
		stream = inDirectOther
	case api && x.principal != "":
		stream = inAuth
	case api:
		stream = inAnon
	default:
		return // through the edge and not an API call: nothing
	}
	f := []string{
		"t", itoa64(x.start.UnixMilli()),
		"m", trafficMethod(x.method),
		"s", strconv.Itoa(x.status),
		"d", itoa64(x.dur.Milliseconds()),
	}
	if api {
		route := "(no route)"
		if x.pattern != "" {
			route = x.pattern
			if i := strings.IndexByte(route, ' '); i >= 0 {
				route = route[i+1:]
			}
		}
		pid := ""
		if pidRe(x.pid) {
			pid = x.pid
		}
		f = append(f, "qb", itoa64(x.qb), "rb", itoa64(x.rb), "k", x.class, "r", route,
			"src", clipBytes(x.src, 45), "via", x.via, "c", clipBytes(x.principal, 64), "pid", pid)
	} else {
		f = append(f, "k", x.class, "src", clipBytes(x.src, 45), "via", x.via)
	}
	t.enqueue(trafficRecord{stream: stream, fields: f})
}

// trafficWriter records the response's status and size for the inbound record
// (§8.6). It unwraps to the real writer, so http.NewResponseController reaches
// it (SetReadDeadline, Flush).
type trafficWriter struct {
	http.ResponseWriter
	status int
	n      int64
}

func (w *trafficWriter) WriteHeader(code int) {
	if w.status == 0 && code >= 200 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *trafficWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.n += int64(n)
	return n, err
}

// Flush reaches the real writer when it can flush.
func (w *trafficWriter) Flush() {
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *trafficWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// code is the recorded status: the first WriteHeader, 200 for a Write first,
// and 200 when the handler wrote nothing.
func (w *trafficWriter) code() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

// ---------- the direct-source summary ----------

// directSource is one row of the summary (§8.7).
type directSource struct {
	Src         string
	First, Last time.Time
	Count       int64
	LastClass   string
	Principal   bool // a principal was set at least once
}

// directSummary is every direct source since the dashboard started, at most
// cap of them. Eviction in the store never touches it.
type directSummary struct {
	mu        sync.Mutex
	cap       int
	m         map[string]*directSource
	untracked atomic.Int64
}

func newDirectSummary(cap int) *directSummary {
	return &directSummary{cap: cap, m: map[string]*directSource{}}
}

func (s *directSummary) see(src, class string, principal bool, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.m[src]
	if e == nil {
		if len(s.m) >= s.cap {
			s.untracked.Add(1)
			return
		}
		e = &directSource{Src: src, First: at}
		s.m[src] = e
	}
	e.Count++
	if at.After(e.Last) {
		e.Last = at
	}
	e.LastClass = class
	e.Principal = e.Principal || principal
}

// rows is a copy of the summary.
func (s *directSummary) rows() []directSource {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]directSource, 0, len(s.m))
	for _, e := range s.m {
		out = append(out, *e)
	}
	return out
}

// ---------- the writer ----------

// run is the writer (§9.2): batches of at most 128 records or 250 ms, a trim of
// every stream once at start and every 60 s, a dial with AUTH when needed, and
// a backoff from 1 s doubling to 30 s after a failed dial, while the queue keeps
// draining and counting. On shutdown, queued records are discarded.
func (t *trafficRecorder) run(ctx context.Context) {
	w := &trafficWriterLoop{t: t, backoff: trafficBackoff1}
	defer w.conn.Close()
	flush := time.NewTicker(trafficFlushEvery)
	defer flush.Stop()
	trim := time.NewTicker(trafficTrimEvery)
	defer trim.Stop()
	w.trim()
	batch := make([]trafficRecord, 0, trafficBatch)
	for {
		select {
		case <-ctx.Done():
			return
		case r := <-t.q:
			batch = append(batch, r)
			if len(batch) == trafficBatch {
				w.write(batch)
				batch = batch[:0]
			}
		case <-flush.C:
			if len(batch) > 0 {
				w.write(batch)
				batch = batch[:0]
			}
		case <-trim.C:
			w.trim()
		}
	}
}

// trafficWriterLoop is the writer's connection state.
type trafficWriterLoop struct {
	t       *trafficRecorder
	conn    *respConn
	retryAt time.Time
	backoff time.Duration
	fresh   bool // connected, and nothing written on this connection yet
}

// connect dials when there is no connection and the backoff allows it.
func (w *trafficWriterLoop) connect() bool {
	if w.conn != nil {
		return true
	}
	t := w.t
	if now := t.now(); now.Before(w.retryAt) {
		return false
	}
	c, err := dialRESP(t.addr, trafficUser, t.pass, trafficDial, trafficOpDeadline)
	if err != nil {
		w.failed(err)
		w.retryAt = t.now().Add(w.backoff)
		w.backoff = min(2*w.backoff, trafficBackoffCap)
		return false
	}
	w.conn, w.backoff, w.retryAt, w.fresh = c, trafficBackoff1, time.Time{}, true
	return true
}

// failed notes a failure: the first one after a working spell logs one line.
func (w *trafficWriterLoop) failed(err error) {
	t := w.t
	t.mu.Lock()
	t.lastErrAt = t.now()
	was := t.down
	t.down = true
	t.mu.Unlock()
	if !was {
		cls := trafficErrClass(err)
		if errors.Is(err, errRESPAuth) {
			t.logf("%s", errRESPAuth.Error())
			cls = "auth"
		}
		t.logf("traffic: the store is not answering (%s); records are dropped until it answers", cls)
	}
}

// wrote notes a successful write: the first after a (re)connect sets
// firstWriteAt; the first after a failure logs one line.
func (w *trafficWriterLoop) wrote() {
	t := w.t
	t.mu.Lock()
	if w.fresh {
		t.firstWriteAt = t.now()
		w.fresh = false
	}
	was := t.down
	n := t.downDropped
	t.down, t.downDropped = false, 0
	t.mu.Unlock()
	if was {
		t.logf("traffic: the store answers again; %d records were dropped meanwhile", n)
	}
}

func (w *trafficWriterLoop) drop(n int) {
	t := w.t
	t.dropped.Add(int64(n))
	t.mu.Lock()
	if t.down {
		t.downDropped += int64(n)
	}
	t.mu.Unlock()
}

// write pipelines one XADD per record. An error reply (OOM, NOPERM) drops that
// record; an I/O error closes the connection and drops what was not answered.
func (w *trafficWriterLoop) write(batch []trafficRecord) {
	if !w.connect() {
		w.drop(len(batch))
		return
	}
	cmds := make([][]string, 0, len(batch))
	for _, r := range batch {
		cmd := []string{"XADD", trafficKeyPrefix + r.stream, "MAXLEN", "~", strconv.Itoa(trafficCap(r.stream)), "*"}
		cmds = append(cmds, append(cmd, r.fields...))
	}
	rs, err := w.conn.pipeline(cmds)
	if err != nil {
		w.conn = nil
		w.drop(len(batch))
		w.failed(err)
		return
	}
	ok := 0
	for _, v := range rs {
		if v.Err() != nil {
			w.t.dropped.Add(1)
			continue
		}
		ok++
	}
	w.t.written.Add(int64(ok))
	if ok > 0 {
		w.wrote()
	}
}

// trim sends one XTRIM … MINID ~ <now − 48 h>-0 per stream. A successful trim
// counts as a write for firstWriteAt (implementation note: an idle dashboard
// must not read as an emptied store).
func (w *trafficWriterLoop) trim() {
	if !w.connect() {
		return
	}
	minID := strconv.FormatInt(w.t.now().Add(-trafficWindow).UnixMilli(), 10) + "-0"
	var cmds [][]string
	for _, s := range trafficStreams() {
		cmds = append(cmds, []string{"XTRIM", s.key(), "MINID", "~", minID})
	}
	rs, err := w.conn.pipeline(cmds)
	if err != nil {
		w.conn = nil
		w.failed(err)
		return
	}
	for _, v := range rs {
		if v.Err() != nil {
			return
		}
	}
	w.wrote()
}

// trafficStoreHost is the host of GAOS_REDIS_ADDR, which the QA client and the
// Brain Town client refuse as a destination (§13.4).
func trafficStoreHost() string {
	if h, _, err := net.SplitHostPort(strings.TrimSpace(os.Getenv("GAOS_REDIS_ADDR"))); err == nil {
		return h
	}
	return ""
}

// unknownDirectSources counts the direct sources that never authenticated, on
// the values mask shows: two sources one mask hides as one count once (§10.2).
func unknownDirectSources(rows []directSource, mask func(string) string) int {
	seen := map[string]bool{}
	for _, r := range rows {
		if !r.Principal {
			seen[mask(r.Src)] = true
		}
	}
	return len(seen)
}
