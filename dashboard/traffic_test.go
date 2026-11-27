package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
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

// The traffic recorder (traffic.go, D-107). TC-886 … TC-897. Every test that
// turns recording on sets the package pointer, is not parallel, and restores
// the pointer when it ends.

// useTraffic installs a recorder that records to addr. With writer false the
// writer is left unstarted (the test hook of §9.2) and the queue is the only
// consumer.
func useTraffic(t *testing.T, addr string, writer bool) *trafficRecorder {
	t.Helper()
	rec := newTrafficRecorder(addr, "pw", "")
	rec.noWriter = !writer
	rec.logf = func(string, ...any) {}
	prev := trafficPtr.Load()
	trafficPtr.Store(rec)
	t.Cleanup(func() { trafficPtr.Store(prev) })
	if writer {
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		rec.start(ctx)
	}
	return rec
}

// noTraffic installs no recorder, as a deployment with no setting.
func noTraffic(t *testing.T) {
	t.Helper()
	prev := trafficPtr.Load()
	trafficPtr.Store(nil)
	t.Cleanup(func() { trafficPtr.Store(prev) })
}

// drain takes every queued record.
func drain(rec *trafficRecorder) []trafficRecord {
	var out []trafficRecord
	for {
		select {
		case r := <-rec.q:
			out = append(out, r)
		default:
			return out
		}
	}
}

func fieldsOf(r trafficRecord) map[string]string {
	m := map[string]string{}
	for i := 0; i+1 < len(r.fields); i += 2 {
		m[r.fields[i]] = r.fields[i+1]
	}
	return m
}

func fieldNames(r trafficRecord) string {
	var names []string
	for i := 0; i < len(r.fields); i += 2 {
		names = append(names, r.fields[i])
	}
	return strings.Join(names, " ")
}

// ---------------------------------------------------------------- TC-886

func TestTrafficSettings(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "auth")
	if err := os.WriteFile(good, []byte("0123456789abcdef\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	addr, pass, why := trafficSettings("172.30.14.2:6379", good)
	if why != "" || addr != "172.30.14.2:6379" || pass != "0123456789abcdef" {
		t.Fatalf("a valid address and a mode-600 file answered %q %q, want on with the trimmed password", addr, why)
	}
	for _, bad := range []string{"", "redis:6379", "192.0.2.4", "203.0.113.5:6379", "10.0.0.5:6379", "172.30.14.2:x", "172.30.14.9:6379"} {
		if a, p, why := trafficSettings(bad, good); why == "" || a != "" || p != "" {
			t.Errorf("GAOS_REDIS_ADDR=%q turned recording on; want off with the reason", bad)
		} else if strings.Contains(why, "0123456789abcdef") {
			t.Errorf("the reason %q carries the password", why)
		}
	}
	empty := filepath.Join(dir, "empty")
	open := filepath.Join(dir, "open")
	_ = os.WriteFile(empty, []byte("\n"), 0o600)
	_ = os.WriteFile(open, []byte("secret\n"), 0o644)
	if err := os.Chmod(open, 0o644); err != nil {
		t.Fatal(err)
	}
	for name, f := range map[string]string{"unset": "", "missing": filepath.Join(dir, "nope"), "empty": empty, "mode 644": open, "a directory": dir} {
		if _, _, why := trafficSettings("172.30.14.2:6379", f); why == "" {
			t.Errorf("a password file that is %s turned recording on", name)
		}
	}
	// The boot line names the address and the window, never the password.
	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	t.Setenv("GAOS_REDIS_ADDR", "172.30.14.2:6379")
	t.Setenv("GAOS_REDIS_AUTH_FILE", good)
	if rec := trafficFromEnv(); !rec.on() {
		t.Fatalf("trafficFromEnv is off: %s", rec.why)
	}
	t.Setenv("GAOS_REDIS_ADDR", "")
	if rec := trafficFromEnv(); rec.on() || rec.why != "GAOS_REDIS_ADDR is not set" {
		t.Fatalf("trafficFromEnv with no address answered %q", rec.why)
	}
	if out := logs.String(); !strings.Contains(out, "traffic: recording to 172.30.14.2:6379, 48 h") ||
		!strings.Contains(out, "traffic: off (GAOS_REDIS_ADDR is not set)") || strings.Contains(out, "0123456789abcdef") {
		t.Errorf("the boot lines are %q; want the address and 48 h, the off reason, and never the password", out)
	}
	// The caps and the window are the §5 constants.
	want := map[string]int{"out:qa-tool": 5000, "out:ai-manager": 5000, "out:github": 3000, "out:github-write": 1000,
		"out:whatsapp": 1000, "out:brain-town": 1000, "out:brain-town-reader": 1000, "out:taskmgr": 1000,
		"in:direct:auth": 2000, "in:direct:other": 2000, "in:auth": 2000, "in:anon": 1000}
	total := 0
	for _, s := range trafficStreams() {
		if want[s.Kind] != s.Cap || !strings.HasPrefix(s.key(), "gaos:traffic:") {
			t.Errorf("stream %s has cap %d, want %d", s.key(), s.Cap, want[s.Kind])
		}
		total += s.Cap
	}
	if len(trafficStreams()) != 12 || total != 25000 || trafficWindow != 48*time.Hour || trafficQueueLen != 4096 {
		t.Errorf("%d streams holding %d records, a %v window and a %d queue; want 12, 25000, 48h, 4096",
			len(trafficStreams()), total, trafficWindow, trafficQueueLen)
	}
}

// ---------------------------------------------------------------- TC-887

func TestOutboundPathIsTemplated(t *testing.T) {
	hex40 := "0123456789abcdef0123456789abcdef01234567"
	cases := []struct{ base, path, want string }{
		{"https://wa.example/inst-Kq7xTzLm", "/inst-Kq7xTzLm/send/message", "{base}/send/message"},
		{"https://wa.example/inst-Kq7xTzLm/", "/inst-Kq7xTzLm", "{base}"},
		{"https://wa.example/inst-Kq7xTzLm", "/other-instance/send", "{other}"},
		{"https://wa.example/inst-Kq7xTzLm", "/inst-Kq7xTzLmX/send", "{other}"},
		{"https://api.github.com", "/repos/acme/route-api/pulls/12/files", "/repos/acme/route-api/pulls/{n}/files"},
		{"https://api.github.com", "/repos/acme/route-api/branches/feature/jane-ticket-12", "/repos/acme/route-api/branches/{ref}"},
		{"https://api.github.com", "/repos/acme/route-api/contents/internal/billing/rates.go", "/repos/acme/route-api/contents/{path}"},
		{"https://api.github.com", "/repos/acme/route-api/contents/.github/workflows", "/repos/acme/route-api/contents/{path}"},
		{"https://api.github.com", "/repos/acme/route-api/commits/" + hex40, "/repos/acme/route-api/commits/{id}"},
		{"", "/repos/acme%2Fsecret/x", "/repos/{id}/x"},
		{"", "/users/jane.doe@example.com/profile", "/users/{id}/profile"},
		{"", "/send/6281234567890", "/send/{n}"},
		{"", "/send/+6281234567890", "/send/{id}"},
		{"", "/v1/products/p-12/qa/cases", "/v1/products/p-12/qa/cases"},
		{"", "/v1/products/p-1234/qa/cases", "/v1/products/{id}/qa/cases"},
		{"", "/taskmgr.v1.TaskService/GetTask", "/taskmgr.v1.TaskService/GetTask"},
		{"", "/a/" + strings.Repeat("x", 41), "/a/{id}"},
		{"", "/abc1234", "/{id}"},
		{"", "", "/"},
		{"", "/", "/"},
	}
	for _, c := range cases {
		if got := trafficPath(trafficBasePath(c.base), c.path); got != c.want {
			t.Errorf("base %q path %q templated as %q, want %q", c.base, c.path, got, c.want)
		}
	}
	// 13 segments keep 12; a 300-byte path is cut to 160.
	segs := strings.Split("a/b/c/d/e/f/g/h/i/j/k/l/m", "/")
	if got := trafficPath("", "/"+strings.Join(segs, "/")); got != "/"+strings.Join(segs[:12], "/") {
		t.Errorf("13 segments templated as %q, want the first 12", got)
	}
	long := "/" + strings.Repeat("abcdefghij/", 30)
	if got := trafficPath("", long); len(got) > 160 {
		t.Errorf("a 300-byte path templated to %d bytes, want at most 160", len(got))
	}
	// Never a query or a fragment: the path is read with EscapedPath.
	u, _ := url.Parse("https://h/v1/x?token=abc#frag")
	if got := trafficPath("", u.EscapedPath()); got != "/v1/x" {
		t.Errorf("a URL with a query and a fragment templated as %q", got)
	}
}

// ---------------------------------------------------------------- TC-888

func TestRecordingTransportKeepsNoSecret(t *testing.T) {
	const (
		s1 = "bearer-secret-one"
		s2 = "userinfo-secret-two"
		s3 = "query-secret-three"
		s4 = "fragment-secret-four"
		s5 = "body-secret-five"
		s6 = "response-secret-six"
		u  = "user-name-u"
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		w.Header().Set("X-Back", s6)
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, "answer "+s6)
	}))
	defer srv.Close()
	rec := useTraffic(t, "127.0.0.1:1", false)
	base := "https://h/inst-Kq7xTzLm"
	hc := &http.Client{Transport: trafficWrap("whatsapp", base, nil)}
	target := strings.Replace(srv.URL, "http://", "http://"+u+":"+s2+"@", 1) + "/inst-Kq7xTzLm/send/message?token=" + s3 + "#" + s4
	req, _ := http.NewRequest(http.MethodPost, target, strings.NewReader(s5))
	req.Header.Set("Authorization", "Bearer "+s1)
	resp, err := hc.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated || string(body) != "answer "+s6 || resp.Header.Get("X-Back") != s6 {
		t.Errorf("the caller got %d %q %q; the response must reach it unchanged", resp.StatusCode, body, resp.Header.Get("X-Back"))
	}
	rs := drain(rec)
	if len(rs) != 1 || rs[0].stream != "out:whatsapp" {
		t.Fatalf("the call made %d records (%v), want one on out:whatsapp", len(rs), rs)
	}
	if got := fieldNames(rs[0]); got != "t m s d qb rb h u e" {
		t.Errorf("the record's fields are %q, want exactly t m s d qb rb h u e", got)
	}
	for _, v := range rs[0].fields {
		for _, s := range []string{s1, s2, s3, s4, s5, s6, u, "inst-Kq7xTzLm"} {
			if strings.Contains(v, s) {
				t.Errorf("a stored field %q holds %q", v, s)
			}
		}
	}
	f := fieldsOf(rs[0])
	if f["u"] != "{base}/send/message" || f["m"] != "POST" || f["s"] != "201" || f["qb"] != "16" || f["e"] != "" {
		t.Errorf("the record is %v; want {base}/send/message, POST, 201, qb 16, no error class", f)
	}
}

// ---------------------------------------------------------------- TC-889

// tmTrafficServer is an h2c TaskMgr that answers by mode.
func tmTrafficServer(t *testing.T, mode *string, mu *sync.Mutex) string {
	t.Helper()
	var p http.Protocols
	p.SetUnencryptedHTTP2(true)
	srv := &http.Server{Protocols: &p, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		mu.Lock()
		m := *mode
		mu.Unlock()
		w.Header().Set("Content-Type", "application/grpc")
		switch m {
		case "trailer-16":
			w.Header().Set("Trailer", "Grpc-Status")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(grpcFrame(nil))
			w.Header().Set("Grpc-Status", "16")
		case "big":
			w.Header().Set("Trailer", "Grpc-Status")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(grpcFrame(bytes.Repeat([]byte{0}, 2<<20)))
			w.Header().Set("Grpc-Status", "0")
		default: // a trailers-only code
			w.Header().Set("Grpc-Status", m)
			w.WriteHeader(http.StatusOK)
		}
	})}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return ln.Addr().String()
}

func TestRecordingTransportClassifiesErrors(t *testing.T) {
	rec := useTraffic(t, "127.0.0.1:1", false)
	get := func(hc *http.Client, ctx context.Context, u string) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if resp, err := hc.Do(req); err == nil {
			resp.Body.Close()
		}
	}
	one := func(what, wantClass string) {
		t.Helper()
		rs := drain(rec)
		if len(rs) != 1 {
			t.Fatalf("%s made %d records, want one", what, len(rs))
		}
		f := fieldsOf(rs[0])
		if f["e"] != wantClass || f["s"] != "0" {
			t.Errorf("%s recorded class %q status %s, want %q and 0", what, f["e"], f["s"], wantClass)
		}
		for _, v := range rs[0].fields {
			if strings.Contains(v, " ") || strings.Contains(v, "dial") {
				t.Errorf("%s stored %q; no error text may be stored", what, v)
			}
		}
	}
	hc := &http.Client{Transport: trafficWrap("qa-tool", "", &http.Transport{})}

	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer slow.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	get(hc, ctx, slow.URL)
	cancel()
	one("a timeout", "timeout")

	ctx, cancel = context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	get(hc, ctx, slow.URL)
	one("a canceled call", "canceled")

	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	dead := ln.Addr().String()
	ln.Close()
	get(hc, context.Background(), "http://"+dead+"/x")
	one("a refused connection", "refused")

	tlsSrv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer tlsSrv.Close()
	tlsSrv.Config.ErrorLog = log.New(io.Discard, "", 0)
	get(hc, context.Background(), tlsSrv.URL)
	one("an unknown CA", "tls")

	rst, _ := net.Listen("tcp", "127.0.0.1:0")
	defer rst.Close()
	go func() {
		for {
			c, err := rst.Accept()
			if err != nil {
				return
			}
			buf := make([]byte, 4096)
			_, _ = c.Read(buf)
			_ = c.(*net.TCPConn).SetLinger(0)
			c.Close()
		}
	}()
	get(hc, context.Background(), "http://"+rst.Addr().String()+"/x")
	one("a reset connection", "reset")

	if c := trafficErrClass(errors.New("something else")); c != "other" {
		t.Errorf("an unknown error is class %q, want other", c)
	}
	if c := trafficErrClass(&net.DNSError{Err: "no such host", Name: "x.invalid"}); c != "dns" {
		t.Errorf("a DNS error is class %q, want dns", c)
	}

	// TaskMgr: one record per call through taskmgrClient.do, with its gRPC
	// outcome.
	var mu sync.Mutex
	mode := "16"
	tmTestEnv(t, tmTrafficServer(t, &mode, &mu))
	cl := newTaskMgrClient(taskmgrConfigFromEnv())
	if cl == nil {
		t.Fatal("fixture: the TaskMgr client is off")
	}
	tmCase := func(m, wantS, wantG, wantX string, failed bool) {
		t.Helper()
		mu.Lock()
		mode = m
		mu.Unlock()
		_, _ = cl.do(context.Background(), "/taskmgr.v1.TaskService/GetTask", []byte{}, 2*time.Second, "gaos", tmTestSecret)
		rs := drain(rec)
		if len(rs) != 1 || rs[0].stream != "out:taskmgr" {
			t.Fatalf("mode %s made %d records, want exactly one on out:taskmgr", m, len(rs))
		}
		f := fieldsOf(rs[0])
		if f["s"] != wantS || f["g"] != wantG || f["x"] != wantX || f["u"] != "/taskmgr.v1.TaskService/GetTask" || f["m"] != "POST" {
			t.Errorf("mode %s recorded s=%s g=%s x=%s u=%s, want s=%s g=%s x=%s", m, f["s"], f["g"], f["x"], f["u"], wantS, wantG, wantX)
		}
		if got := tmFailed(f["x"]); got != failed {
			t.Errorf("mode %s: failed is %v, want %v", m, got, failed)
		}
		for _, v := range rs[0].fields {
			if strings.Contains(v, tmTestSecret) {
				t.Errorf("a TaskMgr record holds the credential")
			}
		}
	}
	tmCase("trailer-16", "200", "16", "auth", true)
	tmCase("5", "200", "5", "notfound", false)
	tmCase("3", "200", "3", "invalid", false)
	tmCase("big", "200", "-1", "unavailable", true)
	tmTestEnv(t, dead)
	down := newTaskMgrClient(taskmgrConfigFromEnv())
	_, _ = down.do(context.Background(), "/taskmgr.v1.TaskService/GetTask", nil, time.Second, "gaos", tmTestSecret)
	rs := drain(rec)
	if len(rs) != 1 || fieldsOf(rs[0])["s"] != "0" || fieldsOf(rs[0])["x"] != "unavailable" || fieldsOf(rs[0])["e"] != "refused" {
		t.Errorf("a transport error recorded %v, want one record s=0 x=unavailable e=refused", rs)
	}
}

// ---------------------------------------------------------------- TC-890

func TestTrafficRecordingNeverBlocks(t *testing.T) {
	rec := useTraffic(t, "127.0.0.1:1", false)
	done := make(chan struct{})
	go func() {
		for i := 0; i < trafficQueueLen+1000; i++ {
			rec.out(trafficOut{label: "github", start: time.Now(), method: "GET", host: "h", path: "/"})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("recording blocked: 5096 records did not enqueue within 5 s")
	}
	if got := rec.dropped.Load(); got != 1000 {
		t.Errorf("dropped is %d after 4096 + 1000 records, want exactly 1000", got)
	}

	// A started writer and a store that refuses connections: every call succeeds.
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	dead := ln.Addr().String()
	ln.Close()
	rec2 := useTraffic(t, dead, true)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "ok") }))
	defer srv.Close()
	hc := &http.Client{Transport: trafficWrap("brain-town", srv.URL, nil), Timeout: 5 * time.Second}
	for i := 0; i < 300; i++ {
		resp, err := hc.Get(srv.URL + "/v1/x")
		if err != nil || resp.StatusCode != 200 {
			t.Fatalf("call %d failed with the store down: %v", i, err)
		}
		resp.Body.Close()
	}
	if rec2.written.Load() != 0 {
		t.Errorf("records were written to a store that refuses connections")
	}
}

// ---------------------------------------------------------------- TC-891

func TestTrafficWriterTrimsByLengthAndAge(t *testing.T) {
	f := newFakeRedis(t, func(cmd []string) string {
		if cmd[0] == "XADD" {
			for i := 6; i+1 < len(cmd); i += 2 {
				switch {
				case cmd[i] == "h" && cmd[i+1] == "oom":
					return "-OOM command not allowed when used memory > 'maxmemory'.\r\n"
				case cmd[i] == "h" && cmd[i+1] == "noperm":
					return "-NOPERM User gaos has no permissions to access one of the keys\r\n"
				}
			}
		}
		return fakeDefault(cmd)
	})
	rec := newTrafficRecorder(f.addr(), "pw", "")
	var mu sync.Mutex
	var logs []string
	rec.logf = func(format string, a ...any) {
		mu.Lock()
		logs = append(logs, fmt.Sprintf(format, a...))
		mu.Unlock()
	}
	clock := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	rec.now = func() time.Time { return clock }
	w := &trafficWriterLoop{t: rec, backoff: trafficBackoff1}

	// The trim: twelve XTRIM … MINID ~ <now − 48 h>-0, after AUTH.
	w.trim()
	cmds := f.commands()
	if len(cmds) != 13 || cmds[0][0] != "AUTH" || cmds[0][1] != "gaos" {
		t.Fatalf("the trim sent %d commands starting %v, want AUTH gaos then twelve XTRIM", len(cmds), cmds[0])
	}
	minID := fmt.Sprintf("%d-0", clock.Add(-48*time.Hour).UnixMilli())
	seen := map[string]bool{}
	for _, c := range cmds[1:] {
		if c[0] != "XTRIM" || c[2] != "MINID" || c[3] != "~" || c[4] != minID {
			t.Errorf("a trim is %v, want XTRIM <key> MINID ~ %s", c, minID)
		}
		seen[c[1]] = true
	}
	if len(seen) != 12 {
		t.Errorf("the trim covered %d streams, want 12", len(seen))
	}
	if !rec.firstWriteAt.Equal(clock) {
		t.Errorf("firstWriteAt is %v after the first write, want %v", rec.firstWriteAt, clock)
	}

	// Each XADD goes to its stream with its cap; OOM and NOPERM drop that record.
	batch := []trafficRecord{
		{stream: "out:github", fields: []string{"t", "1", "h", "api.github.com"}},
		{stream: "in:anon", fields: []string{"t", "1"}},
		{stream: "out:qa-tool", fields: []string{"t", "1", "h", "oom"}},
		{stream: "out:taskmgr", fields: []string{"t", "1", "h", "noperm"}},
	}
	w.write(batch)
	adds := f.commands()[13:]
	if len(adds) != 4 || strings.Join(adds[0][:6], " ") != "XADD gaos:traffic:out:github MAXLEN ~ 3000 *" ||
		strings.Join(adds[1][:6], " ") != "XADD gaos:traffic:in:anon MAXLEN ~ 1000 *" ||
		strings.Join(adds[2][:6], " ") != "XADD gaos:traffic:out:qa-tool MAXLEN ~ 5000 *" {
		t.Fatalf("the batch sent %v; want one XADD per record to its stream with its cap", adds)
	}
	if rec.written.Load() != 2 || rec.dropped.Load() != 2 {
		t.Errorf("written %d dropped %d, want 2 and 2 (OOM and NOPERM drop their record)", rec.written.Load(), rec.dropped.Load())
	}
	if f.connCount() != 1 {
		t.Errorf("%d connections, want one", f.connCount())
	}

	// A lost connection: the batch is dropped, one line is logged; the next
	// write redials, signs in first, moves firstWriteAt and logs the recovery.
	f.dropConns()
	time.Sleep(20 * time.Millisecond)
	clock = clock.Add(time.Minute)
	w.write(batch[:1])
	clock = clock.Add(time.Minute)
	w.write(batch[:1])
	if f.connCount() != 2 {
		t.Errorf("%d connections after the loss, want a redial", f.connCount())
	}
	cmds = f.commands()
	var authAt []int
	for i, c := range cmds {
		if c[0] == "AUTH" {
			authAt = append(authAt, i)
		}
	}
	if len(authAt) != 2 || cmds[authAt[1]+1][0] != "XADD" {
		t.Errorf("AUTH was sent at %v; want it first on each connection", authAt)
	}
	if !rec.firstWriteAt.Equal(clock) {
		t.Errorf("firstWriteAt is %v, want the first write after the redial (%v)", rec.firstWriteAt, clock)
	}
	// A refused dial backs off from 1 s, doubling.
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	rec.addr = ln.Addr().String()
	ln.Close()
	w.conn.Close()
	w.conn = nil
	w.write(batch[:1])
	if w.backoff != 2*time.Second || !w.retryAt.Equal(clock.Add(time.Second)) {
		t.Errorf("after a refused dial the backoff is %v until %v, want 2s and +1s", w.backoff, w.retryAt)
	}
	w.write(batch[:1]) // within the backoff: no dial, counted
	clock = clock.Add(time.Second)
	w.write(batch[:1])
	if w.backoff != 4*time.Second {
		t.Errorf("after a second refused dial the backoff is %v, want 4s", w.backoff)
	}
	for i := 0; i < 10; i++ {
		clock = clock.Add(time.Minute)
		w.write(batch[:1])
	}
	if w.backoff != 30*time.Second {
		t.Errorf("the backoff grew to %v, want it capped at 30s", w.backoff)
	}
	rec.addr = f.addr()
	clock = clock.Add(time.Minute)
	w.write(batch[:1])
	if w.conn == nil || w.backoff != time.Second {
		t.Errorf("the writer did not reconnect and reset its backoff (%v)", w.backoff)
	}
	mu.Lock()
	defer mu.Unlock()
	var down, up int
	for _, l := range logs {
		switch {
		case strings.HasPrefix(l, "traffic: the store is not answering ("):
			down++
		case strings.HasPrefix(l, "traffic: the store answers again; "):
			up++
		}
	}
	if down != 2 || up != 2 {
		t.Errorf("logged %d outage and %d recovery lines over two outages, want 2 and 2: %q", down, up, logs)
	}

	// The writer itself: records in the queue reach the store.
	f2 := newFakeRedis(t, nil)
	rec2 := useTraffic(t, f2.addr(), true)
	rec2.out(trafficOut{label: "github", start: time.Now(), method: "GET", host: "h", path: "/"})
	deadline := time.Now().Add(3 * time.Second)
	for rec2.written.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if rec2.written.Load() != 1 {
		t.Errorf("the running writer wrote %d records, want 1", rec2.written.Load())
	}
}

// ---------------------------------------------------------------- TC-892

func TestEveryExternalClientIsRecorded(t *testing.T) {
	st := NewMemStore(1)
	bu, _ := url.Parse("https://bt.example/feed")
	cu, _ := url.Parse("https://bt.example/reader")
	type built struct {
		name string
		rt   func() http.RoundTripper
	}
	clients := []built{
		{"qa-tool", func() http.RoundTripper {
			return newQaOutbound("https://qa.example/base", "gaos_"+strings.Repeat("q", 32), nil).hc.Transport
		}},
		{"ai-manager", func() http.RoundTripper { return (&AIMClient{Base: "https://aim.example"}).client().Transport }},
		{"brain-town", func() http.RoundTripper { return newBrainTown(btConfig{enabled: true, url: bu}, st).hc.Transport }},
		{"brain-town-reader", func() http.RoundTripper {
			return newBTConsumer(btConsumerConfig{enabled: true, url: cu}, st).hc.Transport
		}},
		{"github", func() http.RoundTripper {
			return newGHClient(ghConfig{token: "ghp_" + strings.Repeat("g", 36), base: "https://api.github.com"}).hc.Transport
		}},
		{"github-write", func() http.RoundTripper {
			return newGHWriter(ghConfig{token: "ghp_" + strings.Repeat("w", 36), base: "https://api.github.com"}).c.hc.Transport
		}},
		{"whatsapp", func() http.RoundTripper { return newWANotifier(waConfig{base: "https://wa.example/inst-x"}).hc.Transport }},
	}
	wantBase := map[string]string{"qa-tool": "/base", "brain-town": "/feed", "brain-town-reader": "/reader", "whatsapp": "/inst-x"}

	// Off: today's transports.
	noTraffic(t)
	off := map[string]http.RoundTripper{}
	for _, c := range clients {
		rt := c.rt()
		if _, ok := rt.(*recordingTransport); ok {
			t.Errorf("%s is wrapped with recording off", c.name)
		}
		off[c.name] = rt
	}
	if off["qa-tool"] != nil || off["ai-manager"] != nil || off["brain-town"] != nil {
		t.Errorf("with recording off the QA, AI-manager and Brain Town clients have %T %T %T, want the default (nil)",
			off["qa-tool"], off["ai-manager"], off["brain-town"])
	}
	if _, ok := off["github"].(*http.Transport); !ok {
		t.Errorf("with recording off the GitHub client has %T, want *http.Transport", off["github"])
	}

	// On: every one carries a recording transport with its label and base.
	useTraffic(t, "127.0.0.1:1", false)
	for _, c := range clients {
		rt, ok := c.rt().(*recordingTransport)
		if !ok {
			t.Errorf("%s is not recorded", c.name)
			continue
		}
		if rt.label != c.name || rt.basePath != wantBase[c.name] {
			t.Errorf("%s records as %q with base %q, want %q and %q", c.name, rt.label, rt.basePath, c.name, wantBase[c.name])
		}
		if off[c.name] != nil && fmt.Sprintf("%T", rt.rt) != fmt.Sprintf("%T", off[c.name]) {
			t.Errorf("%s wraps %T, want today's %T", c.name, rt.rt, off[c.name])
		}
	}
	injected := &http.Client{}
	if got := (&AIMClient{Base: "https://aim.example", HTTP: injected}).client(); got != injected {
		t.Error("an injected AI-manager client was not returned as is")
	}
	tmTestEnv(t, "127.0.0.1:1")
	if cl := newTaskMgrClient(taskmgrConfigFromEnv()); cl == nil {
		t.Fatal("fixture: the TaskMgr client is off")
	} else if _, ok := cl.hc.Transport.(*http.Transport); !ok {
		t.Errorf("TaskMgr's transport is %T, want the plain *http.Transport (it records in do)", cl.hc.Transport)
	}
	a := ArcadeFromEnv()
	if _, ok := a.client.Transport.(*http.Transport); !ok {
		t.Errorf("the Arcade client's transport is %T; ArcadeDB is not recorded", a.client.Transport)
	}
	if _, ok := a.stmtClient.Transport.(*http.Transport); !ok {
		t.Errorf("the Arcade statement client's transport is %T; ArcadeDB is not recorded", a.stmtClient.Transport)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("an unknown platform label did not panic")
			}
		}()
		trafficWrap("pastebin", "", nil)
	}()
}

// ---------------------------------------------------------------- TC-893

func TestInboundRecordsRouteSourceAndClient(t *testing.T) {
	srv, _, pids := apiFixture(t, bothTokens)
	urFile := filepath.Join(t.TempDir(), "unit-results")
	if err := os.WriteFile(urFile, []byte(pids[0]+":jenkins-p1:"+urPass1+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GAOS_UNIT_RESULTS_BASIC_AUTH_FILE", urFile)
	srv.unitResults = unitResultsAuthFromEnv()
	srv.unitResults.logf = func(string, ...any) {}
	rec := useTraffic(t, "127.0.0.1:1", false)
	h := srv.routes()
	const secretQ = "query-secret-value"
	do := func(method, path string, hdr ...string) (*httptest.ResponseRecorder, []trafficRecord) {
		t.Helper()
		req := httptest.NewRequest(method, path, nil)
		req.RemoteAddr = "172.30.12.5:5555"
		for i := 0; i+1 < len(hdr); i += 2 {
			req.Header.Add(hdr[i], hdr[i+1])
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w, drain(rec)
	}
	one := func(what string, rs []trafficRecord, stream string) map[string]string {
		t.Helper()
		if len(rs) != 1 || rs[0].stream != stream {
			t.Fatalf("%s made %d records %v, want one on %s", what, len(rs), rs, stream)
		}
		for _, v := range rs[0].fields {
			for _, s := range []string{tokQA, urPass1, secretQ, "nope"} {
				if strings.Contains(v, s) {
					t.Errorf("%s stored %q", what, v)
				}
			}
		}
		return fieldsOf(rs[0])
	}
	summary := pids[0]
	edge := []string{"X-Forwarded-For", "203.0.113.9, 10.10.0.1", "X-Real-IP", "203.0.113.9"}

	_, rs := do("GET", "/v1/products/"+summary+"/qa/summary", append([]string{"Authorization", "Bearer " + tokQA}, edge...)...)
	f := one("a QA call through the edge", rs, inAuth)
	if f["r"] != "/v1/products/{id}/qa/summary" || f["c"] != "qa-tools" || f["pid"] != summary || f["via"] != "edge" ||
		f["src"] != "203.0.113.9" || f["k"] != "api" || fieldNames(rs[0]) != "t m s d qb rb k r src via c pid" {
		t.Errorf("a QA call through the edge recorded %v; want the pattern, qa-tools, the product, edge", f)
	}
	_, rs = do("GET", "/v1/products/"+summary+"/qa/summary", "Authorization", "Bearer "+tokQA,
		"X-Forwarded-For", "203.0.113.9", "X-Forwarded-For", "10.10.0.1", "X-Real-IP", "203.0.113.9")
	if f := one("two X-Forwarded-For lines", rs, inAuth); f["via"] != "edge" || f["c"] != "qa-tools" {
		t.Errorf("two X-Forwarded-For lines recorded %v, want the same as one", f)
	}
	// Traefik replaces an untrusted peer's chain with the peer and sets X-Real-IP to it (A6).
	_, rs = do("GET", "/v1/products", "Authorization", "Bearer "+tokQA, "X-Forwarded-For", "10.10.0.9", "X-Real-IP", "10.10.0.9")
	if f := one("a QA call from a tunnel peer", rs, inDirectAuth); f["via"] != "direct" || f["src"] != "10.10.0.9" {
		t.Errorf("a direct QA call recorded %v, want direct from 10.10.0.9", f)
	}
	_, rs = do("GET", "/v1/products", "Authorization", "Bearer "+tokQA)
	if f := one("a QA call with no X-Forwarded-For", rs, inDirectAuth); f["via"] != "direct" || f["src"] != "172.30.12.5" {
		t.Errorf("a call with no X-Forwarded-For recorded %v, want direct from the peer", f)
	}
	w, rs := do("GET", "/v1/products", append([]string{"Authorization", "Bearer gaos_" + strings.Repeat("z", 64)}, edge...)...)
	if f := one("a wrong token through the edge", rs, inAnon); w.Code != 401 || f["s"] != "401" || f["c"] != "" {
		t.Errorf("a wrong token answered %d and recorded %v, want 401 with no principal", w.Code, f)
	}
	req := httptest.NewRequest("POST", "/v1/products/"+summary+"/unit-results", strings.NewReader("{}"))
	req.SetBasicAuth("jenkins-p1", urPass1)
	req.RemoteAddr = "172.30.12.5:5555"
	h.ServeHTTP(httptest.NewRecorder(), req)
	if f := one("a Basic unit-results call", drain(rec), inDirectAuth); f["c"] != "basic:unit-results" || f["pid"] != summary {
		t.Errorf("a Basic unit-results call recorded %v, want basic:unit-results", f)
	}
	w, rs = do("GET", "/v1/nope?x="+secretQ, edge...)
	if f := one("an unmatched /v1/ path", rs, inAnon); w.Code != 404 || f["r"] != "(no route)" || f["s"] != "404" || f["k"] != "api" {
		t.Errorf("/v1/nope answered %d and recorded %v, want (no route) 404", w.Code, f)
	}
	w, rs = do("DELETE", "/v1/products", edge...)
	if f := one("a wrong method on /v1/products", rs, inAnon); w.Code != 405 || f["r"] != "(no route)" || f["s"] != "405" {
		t.Errorf("DELETE /v1/products answered %d and recorded %v, want (no route) 405", w.Code, f)
	}

	reduced := "t m s d k src via"
	w, rs = do("GET", "/inbox")
	if f := one("a direct signed-out page", rs, inDirectOther); f["k"] != "backoffice" || f["s"] != "401" || fieldNames(rs[0]) != reduced {
		t.Errorf("a direct signed-out GET /inbox (%d) recorded %v; want a reduced record, backoffice, 401", w.Code, rs[0].fields)
	}
	w, rs = do("GET", "/ui/bo/inbox", "HX-Request", "true")
	if f := one("a direct signed-out htmx request", rs, inDirectOther); f["s"] != fmt.Sprint(w.Code) || w.Code != 401 {
		t.Errorf("a direct signed-out htmx request answered %d and recorded %v, want the requireBO status", w.Code, f)
	}
	w, rs = do("POST", "/contacts", "Sec-Fetch-Site", "cross-site")
	if f := one("a direct cross-origin POST", rs, inDirectOther); w.Code != 403 || f["s"] != "403" {
		t.Errorf("a direct cross-origin POST answered %d and recorded %v, want 403", w.Code, f)
	}
	_, rs = do("GET", "/healthz")
	if f := one("a direct health probe", rs, inDirectOther); f["k"] != "healthz" || f["s"] != "200" {
		t.Errorf("a direct GET /healthz recorded %v, want healthz 200", f)
	}
	_, rs = do("GET", "/static/app.css")
	if f := one("a direct static file", rs, inDirectOther); f["k"] != "static" {
		t.Errorf("a direct static file recorded %v, want static", f)
	}
	for _, p := range []string{"/inbox", "/healthz", "/ui/bo/inbox", "/static/app.css"} {
		if _, rs := do("GET", p, edge...); len(rs) != 0 {
			t.Errorf("GET %s through the edge made %d records, want none", p, len(rs))
		}
	}
	srcs := map[string]directSource{}
	for _, r := range rec.sum.rows() {
		srcs[r.Src] = r
	}
	if len(srcs) != 2 || !srcs["10.10.0.9"].Principal || !srcs["172.30.12.5"].Principal || srcs["172.30.12.5"].Count != 7 {
		t.Errorf("the summary holds %v; want 10.10.0.9 and 172.30.12.5, both with a principal, every direct request counted", srcs)
	}
	if _, ok := srcs["203.0.113.9"]; ok {
		t.Error("an edge caller is in the direct-source summary")
	}
}

// ---------------------------------------------------------------- TC-894

func TestInboundRecorderKeepsTheResponseController(t *testing.T) {
	srv, _ := newTestServer(t)
	rec := useTraffic(t, "127.0.0.1:1", false)
	mux := http.NewServeMux()
	var deadlineErr, flushErr error
	mux.HandleFunc("POST /v1/body", func(w http.ResponseWriter, r *http.Request) {
		deadlineErr = http.NewResponseController(w).SetReadDeadline(time.Now().Add(time.Second))
		var v map[string]any
		if apiDecodeLimit(w, r, &v, 16) {
			w.WriteHeader(204)
		}
	})
	mux.HandleFunc("GET /v1/flush", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "part")
		flushErr = http.NewResponseController(w).Flush()
	})
	mux.HandleFunc("GET /v1/nothing", func(http.ResponseWriter, *http.Request) {})
	mux.HandleFunc("GET /v1/panic", func(http.ResponseWriter, *http.Request) { panic("boom") })
	bearer := map[string]bool{"POST /v1/body": true, "GET /v1/flush": true, "GET /v1/nothing": true, "GET /v1/panic": true}
	ts := httptest.NewUnstartedServer(srv.gate(mux, bearer))
	ts.Config.ErrorLog = log.New(io.Discard, "", 0)
	ts.Start()
	defer ts.Close()
	// status waits for the request's one record: it is sent after the handler
	// returns, so what the handler wrote is visible once it arrives.
	status := func(what string) string {
		t.Helper()
		select {
		case r := <-rec.q:
			if rs := drain(rec); len(rs) != 0 {
				t.Fatalf("%s made %d records, want one", what, 1+len(rs))
			}
			return fieldsOf(r)["s"]
		case <-time.After(5 * time.Second):
			t.Fatalf("%s made %d records, want one", what, 0)
		}
		return ""
	}
	resp, err := http.Post(ts.URL+"/v1/body", "application/json", strings.NewReader(`{"a":"b"}`))
	_ = status("a small body")
	if err != nil || resp.StatusCode != 204 || deadlineErr != nil {
		t.Fatalf("a small body answered %v %v; SetReadDeadline through the wrapper: %v", resp, err, deadlineErr)
	}
	resp.Body.Close()
	resp, err = http.Post(ts.URL+"/v1/body", "application/json", strings.NewReader(`{"a":"`+strings.Repeat("x", 64)+`"}`))
	if err != nil || resp.StatusCode != 413 {
		t.Fatalf("an over-limit body answered %v %v, want 413", resp, err)
	}
	resp.Body.Close()
	if s := status("an over-limit body"); s != "413" {
		t.Errorf("an over-limit body recorded %s, want 413", s)
	}
	resp, err = http.Get(ts.URL + "/v1/flush")
	_ = status("a flush")
	if err != nil || flushErr != nil {
		t.Fatalf("Flush through the wrapper: %v %v", err, flushErr)
	}
	resp.Body.Close()
	resp, err = http.Get(ts.URL + "/v1/nothing")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("a handler that writes nothing answered %v %v", resp, err)
	}
	resp.Body.Close()
	if s := status("a handler that writes nothing"); s != "200" {
		t.Errorf("a handler that writes nothing recorded %s, want 200", s)
	}
	fresh := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}
	if _, err := fresh.Get(ts.URL + "/v1/panic"); err == nil {
		t.Error("a panicking handler's panic did not reach net/http (the connection was answered)")
	}
	if s := status("a panic"); s != "500" {
		t.Errorf("a panic recorded %s, want 500", s)
	}
}

// waitDrained waits until the writer has taken every queued record.
func waitDrained(t *testing.T, rec *trafficRecorder) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for len(rec.q) > 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(400 * time.Millisecond) // the last batch's flush
}

// ---------------------------------------------------------------- TC-895

func TestAnAnonymousFloodEvictsNoDirectOrAuthenticatedRecord(t *testing.T) {
	srv, _, _ := apiFixture(t, bothTokens)
	fs := newFakeStreams()
	f := newFakeRedis(t, fs.reply)
	rec := useTraffic(t, f.addr(), true)
	h := srv.routes()
	call := func(src, token string, edge bool) {
		req := httptest.NewRequest("GET", "/v1/products", nil)
		req.RemoteAddr = "172.30.12.5:5555"
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if edge {
			req.Header.Set("X-Forwarded-For", src+", 10.10.0.1")
			req.Header.Set("X-Real-IP", src)
		} else {
			req.Header.Set("X-Forwarded-For", src)
		}
		h.ServeHTTP(httptest.NewRecorder(), req)
	}
	call("10.10.0.9", tokQA, false)
	call("198.51.100.4", tokQA, true)
	for i := 0; i < 10000; i++ {
		call(fmt.Sprintf("203.0.%d.%d", 113+(i%5000)/250, 1+(i%5000)%250), "", true)
	}
	waitDrained(t, rec)
	if n := len(fs.entries("gaos:traffic:in:direct:auth")); n != 1 {
		t.Errorf("in:direct:auth holds %d records after the flood, want its one", n)
	}
	if n := len(fs.entries("gaos:traffic:in:auth")); n != 1 {
		t.Errorf("in:auth holds %d records after the flood, want its one", n)
	}
	if n := len(fs.entries("gaos:traffic:in:anon")); n != 1000 {
		t.Errorf("in:anon holds %d records after 10 000 anonymous calls, want its cap 1000", n)
	}

	// The writer unstarted: anonymous records stop at ¾ of the queue, and an
	// outbound record still enqueues.
	rec2 := useTraffic(t, "127.0.0.1:1", false)
	for i := 0; i < 4000; i++ {
		rec2.in(trafficIn{start: time.Now(), method: "GET", status: 401, class: kAPI, src: "203.0.113.1", via: "edge"})
	}
	if len(rec2.q) != trafficQueueLen*3/4 || rec2.droppedAnon.Load() != 4000-trafficQueueLen*3/4 {
		t.Errorf("4000 anonymous records left %d queued and %d dropped, want %d and %d",
			len(rec2.q), rec2.droppedAnon.Load(), trafficQueueLen*3/4, 4000-trafficQueueLen*3/4)
	}
	rec2.out(trafficOut{label: "qa-tool", start: time.Now(), method: "GET"})
	if len(rec2.q) != trafficQueueLen*3/4+1 || rec2.dropped.Load() != 0 {
		t.Errorf("an outbound record after the flood: %d queued, %d dropped; it must still enqueue", len(rec2.q), rec2.dropped.Load())
	}
}

// ---------------------------------------------------------------- TC-896

func TestAnAuthenticatedDirectBurstEvictsNoOtherDirectRecord(t *testing.T) {
	srv, _, _ := apiFixture(t, bothTokens)
	fs := newFakeStreams()
	f := newFakeRedis(t, fs.reply)
	rec := useTraffic(t, f.addr(), true)
	h := srv.routes()
	call := func(src, token string) {
		req := httptest.NewRequest("GET", "/v1/products", nil)
		req.RemoteAddr = src + ":5555"
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		h.ServeHTTP(httptest.NewRecorder(), req)
	}
	call("10.10.0.77", "")
	for i := 0; i < 10000; i++ {
		call("10.10.0.2", tokQA)
		if i%1000 == 999 {
			waitDrained(t, rec)
		}
	}
	waitDrained(t, rec)
	other := fs.entries("gaos:traffic:in:direct:other")
	if len(other) != 1 || !strings.Contains(strings.Join(other[0].fields, " "), "10.10.0.77") {
		t.Errorf("in:direct:other holds %v after the burst, want the one unauthenticated record", other)
	}
	if n := len(fs.entries("gaos:traffic:in:direct:auth")); n != 2000 {
		t.Errorf("in:direct:auth holds %d after 10 000 authenticated calls, want its cap 2000", n)
	}
	rows := rec.sum.rows()
	if len(rows) != 2 {
		t.Fatalf("the summary holds %d sources, want both", len(rows))
	}
	if n := unknownDirectSources(rows, func(s string) string { return s }); n != 1 {
		t.Errorf("the summary counts %d unknown direct sources, want 1", n)
	}
}

// ---------------------------------------------------------------- TC-897

func TestOutboundURLsRefuseTheTrafficStore(t *testing.T) {
	tok := filepath.Join(t.TempDir(), "qa-token")
	if err := os.WriteFile(tok, []byte("gaos_"+strings.Repeat("t", 40)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	btTok := filepath.Join(t.TempDir(), "bt-token")
	if err := os.WriteFile(btTok, []byte("bt_live_"+strings.Repeat("b", 32)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GAOS_QA_TOKEN_FILE", tok)
	t.Setenv("GAOS_QA_URL", "https://172.30.14.2:6379/")
	t.Setenv("BRAIN_TOWN_ENABLED", "true")
	t.Setenv("BRAIN_TOWN_URL", "https://172.30.14.2:6379/events")
	t.Setenv("BRAIN_TOWN_TOKEN_FILE", btTok)
	const reason = "names a host GAOS already uses for its gateway or database"

	t.Setenv("GAOS_REDIS_ADDR", "")
	if o := qaOutboundFromEnv(); strings.Contains(o.why, reason) {
		t.Fatalf("fixture: the QA URL is refused without the store set: %s", o.why)
	}
	t.Setenv("GAOS_QA_URL", "") // the Brain Town deny-list holds the QA host too
	if c := btConfigFromEnv(nil); strings.Contains(c.why, reason) {
		t.Fatalf("fixture: the Brain Town URL is refused without the store set: %s", c.why)
	}
	t.Setenv("GAOS_REDIS_ADDR", "172.30.14.2:6379")
	t.Setenv("GAOS_QA_URL", "https://172.30.14.2:6379/")
	if o := qaOutboundFromEnv(); !strings.Contains(o.why, reason) {
		t.Errorf("a QA URL on the traffic store's host answered %q, want refused: %s", o.why, reason)
	}
	t.Setenv("GAOS_QA_URL", "")
	if c := btConfigFromEnv(nil); !strings.Contains(c.why, reason) {
		t.Errorf("a Brain Town URL on the traffic store's host answered %q, want refused: %s", c.why, reason)
	}
}
