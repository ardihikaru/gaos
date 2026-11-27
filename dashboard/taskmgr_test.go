package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// TaskMgr as a product-bound third-party platform (D-104): the codec, the client, the settings,
// the breaker and the people directory. TC-839 to TC-843.

// ---------------------------------------------------------------- a fake TaskMgr

// fakeTM is TaskMgr's gRPC API as far as GAOS calls it, over h2c on loopback.
type fakeTM struct {
	mu       sync.Mutex
	paths    []string
	headers  []http.Header
	users    []tmPerson
	tasks    map[string]tmTaskRow
	comments []string // "code|text"
	force    string   // a grpc-status every call answers, trailers-only
	forceMsg string
	mode     string // "", "trailers-only-ok", "big", "two-frames", "slow"
	fail     error  // a page of ListUsers answers 14 when set
}

func (f *fakeTM) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.paths)
}

func (f *fakeTM) set(fn func(f *fakeTM)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

func encTMUser(p tmPerson) []byte {
	var w pbWriter
	w.str(1, p.ID)
	w.str(2, p.Username)
	w.str(3, p.Name)
	w.str(4, p.Role)
	w.boolean(11, p.Active)
	w.str(14, p.WhatsApp)
	w.boolean(15, p.WAActive)
	w.str(16, p.GitHub)
	w.boolean(19, p.Checked)
	w.str(99, "a field GAOS does not know")
	return w.b
}

func encTMTask(t tmTaskRow) []byte {
	var qa, w pbWriter
	qa.str(1, t.QAID)
	qa.str(2, "qa-login")
	w.str(1, t.ID)
	w.str(5, t.Status)
	w.str(10, t.ProductCode)
	w.bytes(21, qa.b)
	w.boolean(37, t.Deleted)
	return w.b
}

func grpcFrame(msg []byte) []byte {
	b := make([]byte, 5, 5+len(msg))
	binary.BigEndian.PutUint32(b[1:], uint32(len(msg)))
	return append(b, msg...)
}

func writeGRPC(w http.ResponseWriter, msg []byte, frames int) {
	w.Header().Set("Content-Type", "application/grpc")
	w.Header().Set("Trailer", "Grpc-Status, Grpc-Message")
	w.WriteHeader(http.StatusOK)
	for i := 0; i < frames; i++ {
		_, _ = w.Write(grpcFrame(msg))
	}
	w.Header().Set("Grpc-Status", "0")
}

func writeGRPCStatus(w http.ResponseWriter, code, msg string) {
	w.Header().Set("Content-Type", "application/grpc")
	w.Header().Set("Grpc-Status", code)
	w.Header().Set("Grpc-Message", msg)
	w.WriteHeader(http.StatusOK)
}

func (f *fakeTM) handler(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.paths = append(f.paths, r.URL.Path)
	f.headers = append(f.headers, r.Header.Clone())
	force, fmsg, mode, fail := f.force, f.forceMsg, f.mode, f.fail
	f.mu.Unlock()
	if mode == "slow" {
		time.Sleep(300 * time.Millisecond)
	}
	if force != "" {
		writeGRPCStatus(w, force, fmsg)
		return
	}
	if len(body) < 5 {
		writeGRPCStatus(w, "3", "no frame")
		return
	}
	req, err := pbParse(body[5:])
	if err != nil {
		writeGRPCStatus(w, "3", "bad message")
		return
	}
	var out pbWriter
	switch r.URL.Path {
	case "/taskmgr.v1.UserService/ListUsers":
		if fail != nil {
			writeGRPCStatus(w, "14", "down")
			return
		}
		pg, _ := req.msg(4)
		page, size := int(pg.uint(1)), int(pg.uint(2))
		f.mu.Lock()
		users := append([]tmPerson(nil), f.users...)
		f.mu.Unlock()
		for i := (page - 1) * size; i < len(users) && i < page*size; i++ {
			out.bytes(1, encTMUser(users[i]))
		}
		var info pbWriter
		info.uvarint(1, uint64(page))
		info.uvarint(3, uint64(len(users)))
		out.bytes(2, info.b)
	case "/taskmgr.v1.TaskService/GetTask":
		f.mu.Lock()
		t, ok := f.tasks[req.str(1)]
		f.mu.Unlock()
		if !ok {
			writeGRPCStatus(w, "5", "task not found")
			return
		}
		out.bytes(1, encTMTask(t))
	case "/taskmgr.v1.TaskService/AddComment":
		f.mu.Lock()
		f.comments = append(f.comments, req.str(1)+"|"+req.str(2))
		f.mu.Unlock()
		out.str(4, req.str(2))
	default:
		writeGRPCStatus(w, "12", "unimplemented")
		return
	}
	switch mode {
	case "big":
		writeGRPC(w, bytes.Repeat([]byte{0}, 2<<20), 1)
	case "two-frames":
		writeGRPC(w, out.b, 2)
	case "trailers-only-ok":
		writeGRPCStatus(w, "0", "")
	default:
		writeGRPC(w, out.b, 1)
	}
}

// startFakeTM serves f over h2c on a loopback port.
func startFakeTM(t *testing.T, f *fakeTM) string {
	t.Helper()
	var p http.Protocols
	p.SetUnencryptedHTTP2(true)
	srv := &http.Server{Protocols: &p, Handler: http.HandlerFunc(f.handler)}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return ln.Addr().String()
}

const tmTestSecret = "s3cret-for-tests"

// tmTestEnv points the settings at addr with a fresh credential file, and answers its path.
func tmTestEnv(t *testing.T, addr string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "taskmgr-auth")
	if err := os.WriteFile(p, []byte("gaos:"+tmTestSecret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GAOS_TASKMGR_GRPC_ADDR", addr)
	t.Setenv("GAOS_TASKMGR_AUTH_FILE", p)
	t.Setenv("GAOS_TASKMGR_CA_FILE", "")
	return p
}

// tmTestLink is a link to a fresh fake TaskMgr.
func tmTestLink(t *testing.T, st Store, f *fakeTM) *taskmgrLink {
	t.Helper()
	tmTestEnv(t, startFakeTM(t, f))
	l := newTaskMgrLink(st, taskmgrConfigFromEnv())
	if l.cl == nil {
		t.Fatalf("fixture: the TaskMgr client is off: %+v", l.cfg.why)
	}
	return l
}

// ---------------------------------------------------------------- TC-839

func TestProtowireRoundTrip(t *testing.T) {
	var inner, w pbWriter
	inner.str(1, "nested")
	inner.uvarint(2, 300)
	w.uvarint(1, 1<<40)
	w.str(2, "héllo")
	w.bytes(3, inner.b)
	w.boolean(4, true)
	w.str(2, "last wins")
	// Unknown fields of every wire type: fixed64, fixed32, a group with a nested group, varint, bytes.
	w.tag(20, pbI64)
	w.b = append(w.b, 1, 2, 3, 4, 5, 6, 7, 8)
	w.tag(21, pbI32)
	w.b = append(w.b, 1, 2, 3, 4)
	w.tag(22, pbSGroup)
	w.uvarint(1, 7)
	w.tag(23, pbSGroup)
	w.str(1, "deep")
	w.tag(23, pbEGroup)
	w.tag(22, pbEGroup)
	w.uvarint(24, 9)
	w.str(25, "unknown")
	w.str(5, "after the unknown fields")

	m, err := pbParse(w.b)
	if err != nil {
		t.Fatalf("a message with unknown fields of every wire type did not parse: %v", err)
	}
	if m.uint(1) != 1<<40 || !m.boolean(4) || m.str(2) != "last wins" || m.str(5) != "after the unknown fields" {
		t.Errorf("the known fields read back wrong: %d %t %q %q", m.uint(1), m.boolean(4), m.str(2), m.str(5))
	}
	sub, err := m.msg(3)
	if err != nil || sub.str(1) != "nested" || sub.uint(2) != 300 {
		t.Errorf("the nested message read back as %v %v", sub, err)
	}
	if len(m[20]) != 0 || len(m[21]) != 0 || len(m[22]) != 0 {
		t.Error("a fixed-width field or a group was returned instead of skipped")
	}
	if missing, err := m.msg(30); err != nil || len(missing) != 0 || m.str(31) != "" || m.uint(32) != 0 {
		t.Error("an absent field must read as its zero value")
	}
	for name, bad := range map[string][]byte{
		"a truncated varint":     {0x08, 0x80},
		"a length past the end":  {0x12, 0x05, 'a'},
		"a field number of zero": {0x00, 0x01},
		"an unclosed group":      {0xb3, 0x01, 0x08, 0x01},
		"a mismatched group end": {0xb3, 0x01, 0xbc, 0x01},
		"a truncated fixed64":    {0xa1, 0x01, 1, 2},
	} {
		if _, err := pbParse(bad); err == nil {
			t.Errorf("%s parsed without an error", name)
		}
	}
}

// ---------------------------------------------------------------- TC-840

func TestTaskMgrClientCalls(t *testing.T) {
	ctx := context.Background()
	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	f := &fakeTM{tasks: map[string]tmTaskRow{"DEMO-FEATURE-00001": {ID: "DEMO-FEATURE-00001", Status: "IN_PROGRESS", ProductCode: "DEMO", QAID: "u-qa"}},
		users: []tmPerson{{ID: "u-1", Username: "dev.one", Name: "Dev One", Role: "Developer", Active: true, WhatsApp: "6281234567890", WAActive: true, GitHub: "dev-one", Checked: true}}}
	l := tmTestLink(t, nil, f)
	cl := l.cl

	task, err := cl.getTask(ctx, "DEMO-FEATURE-00001", tmPollTimeout)
	if err != nil || task.ProductCode != "DEMO" || task.QAID != "u-qa" || task.Status != "IN_PROGRESS" || task.Deleted {
		t.Fatalf("GetTask answered %+v %v, want the task with its product and QA", task, err)
	}
	h := f.headers[0]
	if f.paths[0] != "/taskmgr.v1.TaskService/GetTask" || h.Get("Content-Type") != "application/grpc" ||
		h.Get("Te") != "trailers" || h.Get("Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("gaos:"+tmTestSecret)) {
		t.Errorf("the call went to %s with headers %v; want the gRPC path, application/grpc, te: trailers and the Basic credential", f.paths[0], h)
	}
	ps, total, err := cl.listUsers(ctx, 1)
	if err != nil || total != 1 || len(ps) != 1 || ps[0].WhatsApp != "6281234567890" || !ps[0].Checked || ps[0].GitHub != "dev-one" || ps[0].Role != "Developer" {
		t.Errorf("ListUsers answered %+v total %d %v", ps, total, err)
	}
	if err := cl.addComment(ctx, "DEMO-FEATURE-00001", "a note"); err != nil || len(f.comments) != 1 || f.comments[0] != "DEMO-FEATURE-00001|a note" {
		t.Errorf("AddComment sent %v %v", f.comments, err)
	}
	if _, err := cl.getTask(ctx, "DEMO-FEATURE-09999", tmPollTimeout); !errors.Is(err, errTMNotFound) {
		t.Errorf("an unknown task answered %v, want errTMNotFound", err)
	}

	// Each status class, on a fresh client so the breaker of one case does not hide the next.
	sentinel := "grpc-message-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	for _, c := range []struct {
		force, mode string
		want        error
	}{
		{"5", "", errTMNotFound}, {"7", "", errTMAuth}, {"8", "", errTMAuth}, {"16", "", errTMAuth},
		{"3", "", errTMInvalid}, {"14", "", errTMUnavailable}, {"13", "", errTMUnavailable},
		{"", "trailers-only-ok", errTMUnavailable}, {"", "big", errTMUnavailable}, {"", "two-frames", errTMUnavailable},
	} {
		f.set(func(f *fakeTM) { f.force, f.forceMsg, f.mode = c.force, sentinel, c.mode })
		fresh := newTaskMgrClient(l.cfg)
		_, err := fresh.getTask(ctx, "DEMO-FEATURE-00001", tmPollTimeout)
		if !errors.Is(err, c.want) {
			t.Errorf("status %q mode %q answered %v, want %v", c.force, c.mode, err, c.want)
		}
		if err != nil && strings.Contains(err.Error(), sentinel) {
			t.Errorf("an error carries TaskMgr's grpc-message: %v", err)
		}
	}
	f.set(func(f *fakeTM) { f.force, f.mode = "", "slow" })
	if _, err := newTaskMgrClient(l.cfg).getTask(ctx, "DEMO-FEATURE-00001", 50*time.Millisecond); !errors.Is(err, errTMUnavailable) {
		t.Errorf("a call past its deadline answered %v, want errTMUnavailable", err)
	}
	if strings.Contains(logs.String(), sentinel) || strings.Contains(logs.String(), tmTestSecret) {
		t.Errorf("the log carries TaskMgr's message or the credential: %s", logs.String())
	}
}

// ---------------------------------------------------------------- TC-841

func TestTaskMgrConfig(t *testing.T) {
	auth := tmTestEnv(t, "")
	if c := taskmgrConfigFromEnv(); c.on() || c.why != "" || newTaskMgrClient(c) != nil {
		t.Errorf("no address must be off with no reason, got %+v", c.why)
	}
	for addr, want := range map[string]string{
		"10.0.0.5:9095":          "TLS only",
		"taskmgr.example.org:95": "IP literal",
		"10.0.0.5":               "ip:port",
		"10.0.0.5:99999":         "ip:port",
	} {
		t.Setenv("GAOS_TASKMGR_GRPC_ADDR", addr)
		if c := taskmgrConfigFromEnv(); c.on() || !strings.Contains(c.why, want) {
			t.Errorf("%s gave %q, want a refusal naming %q", addr, c.why, want)
		}
	}
	t.Setenv("GAOS_TASKMGR_GRPC_ADDR", "127.0.0.1:9095")
	if err := os.Chmod(auth, 0o644); err != nil {
		t.Fatal(err)
	}
	if c := taskmgrConfigFromEnv(); c.on() || !strings.Contains(c.why, "0600") {
		t.Errorf("a credential file readable by others gave %q, want a refusal", c.why)
	}
	_ = os.Chmod(auth, 0o600)
	_ = os.WriteFile(auth, []byte("\n"), 0o600)
	if c := taskmgrConfigFromEnv(); c.on() || c.why != "" || c.off == "" {
		t.Errorf("an empty credential file must be off, not unusable: why %q off %q", c.why, c.off)
	}
	_ = os.WriteFile(auth, []byte("no-colon\n"), 0o600)
	if c := taskmgrConfigFromEnv(); c.on() || !strings.Contains(c.why, "user:password") {
		t.Errorf("a credential with no colon gave %q", c.why)
	}
	t.Setenv("GAOS_TASKMGR_AUTH_FILE", "")
	if c := taskmgrConfigFromEnv(); c.on() || !strings.Contains(c.why, "GAOS_TASKMGR_AUTH_FILE") {
		t.Errorf("no credential file gave %q", c.why)
	}
	_ = os.WriteFile(auth, []byte("gaos:"+tmTestSecret), 0o600)
	t.Setenv("GAOS_TASKMGR_AUTH_FILE", auth)

	// TLS to a pinned certificate: a loopback server with a CA file is reached over TLS 1.3 and h2.
	f := &fakeTM{tasks: map[string]tmTaskRow{"DEMO-FEATURE-00001": {ID: "DEMO-FEATURE-00001", ProductCode: "DEMO"}}}
	ts := httptest.NewUnstartedServer(http.HandlerFunc(f.handler))
	ts.EnableHTTP2 = true
	ts.StartTLS()
	t.Cleanup(ts.Close)
	ca := filepath.Join(t.TempDir(), "ca.pem")
	_ = os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ts.Certificate().Raw}), 0o644)
	t.Setenv("GAOS_TASKMGR_CA_FILE", ca)
	t.Setenv("GAOS_TASKMGR_GRPC_ADDR", strings.TrimPrefix(ts.URL, "https://"))
	c := taskmgrConfigFromEnv()
	cl := newTaskMgrClient(c)
	if cl == nil || c.plain || !strings.HasPrefix(cl.base, "https://") {
		t.Fatalf("a CA file must give a TLS client: %+v", c.why)
	}
	if task, err := cl.getTask(context.Background(), "DEMO-FEATURE-00001", tmCallTimeout); err != nil || task.ProductCode != "DEMO" {
		t.Errorf("the call over TLS to the pinned certificate answered %+v %v", task, err)
	}
	_ = os.WriteFile(ca, []byte("not a certificate"), 0o644)
	if c := taskmgrConfigFromEnv(); c.on() || !strings.Contains(c.why, "PEM") {
		t.Errorf("a CA file with no certificate gave %q", c.why)
	}
	_ = os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ts.Certificate().Raw}), 0o644)
	t.Setenv("GAOS_TASKMGR_GRPC_ADDR", "10.0.0.5:9095")
	if c := taskmgrConfigFromEnv(); !c.on() || c.plain {
		t.Errorf("a private address with a CA file must be on over TLS: %q", c.why)
	}
	// Only the configured address is dialled.
	tr := cl.hc.Transport.(*http.Transport)
	if _, err := tr.DialContext(context.Background(), "tcp", "127.0.0.2:"+cl.cfg.port); err == nil {
		t.Error("the transport dialled an address other than the configured one")
	}
	if _, err := tr.DialContext(context.Background(), "tcp", "127.0.0.1:1"); err == nil {
		t.Error("the transport dialled another port")
	}
}

// ---------------------------------------------------------------- TC-842

func TestTaskMgrBreaker(t *testing.T) {
	ctx := context.Background()
	f := &fakeTM{force: "16"}
	addr := startFakeTM(t, f)
	auth := tmTestEnv(t, addr)
	cl := newTaskMgrClient(taskmgrConfigFromEnv())
	now := time.Unix(1_000_000, 0)
	cl.now = func() time.Time { return now }

	if _, err := cl.getTask(ctx, "X-1", tmPollTimeout); !errors.Is(err, errTMAuth) {
		t.Fatalf("a refused credential answered %v", err)
	}
	if _, err := cl.getTask(ctx, "X-1", tmPollTimeout); !errors.Is(err, errTMPaused) || f.calls() != 1 || cl.breaker() != "auth" {
		t.Errorf("after an auth failure the next call answered %v after %d call(s), breaker %q; want paused with no call", err, f.calls(), cl.breaker())
	}
	f.set(func(f *fakeTM) { f.force = "" })
	_ = os.WriteFile(auth, []byte("gaos:rotated"), 0o600)
	later := time.Now().Add(time.Minute)
	_ = os.Chtimes(auth, later, later)
	if _, err := cl.getTask(ctx, "X-1", tmPollTimeout); !errors.Is(err, errTMNotFound) || f.calls() != 2 {
		t.Errorf("a changed credential file must close the latch: %v after %d call(s)", err, f.calls())
	}
	if h := f.headers[1].Get("Authorization"); h != "Basic "+base64.StdEncoding.EncodeToString([]byte("gaos:rotated")) {
		t.Errorf("the call after the change did not use the new credential: %s", h)
	}
	f.set(func(f *fakeTM) { f.force = "8" })
	_, _ = cl.getTask(ctx, "X-1", tmPollTimeout)
	now = now.Add(59 * time.Minute)
	if cl.allow() {
		t.Error("the auth latch opened before an hour passed")
	}
	now = now.Add(time.Minute)
	if !cl.allow() {
		t.Error("the auth latch is still closed after an hour")
	}

	// Unavailable: three in a row open it for a minute, then each failure doubles the wait.
	f.set(func(f *fakeTM) { f.force = "14" })
	for i := 0; i < 3; i++ {
		_, _ = cl.getTask(ctx, "X-1", tmPollTimeout)
	}
	n := f.calls()
	if _, err := cl.getTask(ctx, "X-1", tmPollTimeout); !errors.Is(err, errTMPaused) || f.calls() != n || cl.breaker() != "unavailable" {
		t.Errorf("three unavailable answers must pause the calls: %v, %d call(s)", err, f.calls()-n)
	}
	now = now.Add(time.Minute)
	if _, err := cl.getTask(ctx, "X-1", tmPollTimeout); !errors.Is(err, errTMUnavailable) || f.calls() != n+1 {
		t.Errorf("after a minute one call must go through: %v", err)
	}
	now = now.Add(time.Minute)
	if cl.allow() {
		t.Error("after a fourth failure the wait did not double to two minutes")
	}
	now = now.Add(time.Minute)
	if !cl.allow() {
		t.Error("the doubled wait did not end after two minutes")
	}
	for i := 0; i < 10; i++ {
		now = now.Add(time.Hour)
		_, _ = cl.getTask(ctx, "X-1", tmPollTimeout)
	}
	if cl.backoff != tmBackoffCap {
		t.Errorf("the backoff grew to %s, want the 30-minute cap", cl.backoff)
	}
	f.set(func(f *fakeTM) { f.force = "5" })
	now = now.Add(time.Hour)
	if _, err := cl.getTask(ctx, "X-1", tmPollTimeout); !errors.Is(err, errTMNotFound) || cl.breaker() != "" || cl.backoff != 0 {
		t.Errorf("an answer must close the breaker: %v, breaker %q", err, cl.breaker())
	}
}

// ---------------------------------------------------------------- TC-843

func TestTaskMgrPeopleDirectory(t *testing.T) {
	ctx := context.Background()
	f := &fakeTM{}
	for i := 0; i < 150; i++ {
		f.users = append(f.users, tmPerson{ID: "u-" + itoa(i), Username: "user" + itoa(i), Name: "User " + itoa(i), Role: "Developer", Active: true})
	}
	f.users[0] = tmPerson{ID: "u-0", Username: "dev.one", Name: "Dev One", Role: "Developer", Active: true, WhatsApp: "6281234567890", WAActive: true, GitHub: "Dev-One", Checked: true}
	f.users[1] = tmPerson{ID: "u-1", Username: "qa.one", Name: "QA One", Role: "QA", Active: true, WhatsApp: "6289876543210", WAActive: false, GitHub: "qa-one", Checked: true}
	f.users[2] = tmPerson{ID: "u-2", Username: "twin.a", Active: true, GitHub: "twin", Checked: true, WhatsApp: "0812345", WAActive: true}
	f.users[3] = tmPerson{ID: "u-3", Username: "twin.b", Active: true, GitHub: "TWIN", Checked: true}
	f.users[4] = tmPerson{ID: "u-4", Username: "unchecked", Active: true, GitHub: "unchecked", Checked: false, WhatsApp: "6280000000001", WAActive: true}
	f.users[149] = tmPerson{ID: "u-149", Username: "gone", Active: false, WhatsApp: "6280000000002", WAActive: true}
	l := tmTestLink(t, nil, f)
	d := l.people
	if err := d.refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if got := f.calls(); got != 2 {
		t.Errorf("150 people took %d ListUsers call(s), want 2 pages of 100", got)
	}
	s := d.snapshot()
	if s.Counts != (tmCounts{People: 149, Linked: 4, Usable: 2, Conflicts: 1}) {
		t.Errorf("the counts are %+v, want 149 people, 4 GitHub-linked, 2 usable numbers, 1 login conflict", s.Counts)
	}
	if p, ok := d.login("dev-one"); !ok || p.ID != "u-0" || !p.usable() || p.jid() != "6281234567890@s.whatsapp.net" {
		t.Errorf("a checked login, matched without case, must find its person and number: %+v %t", p, ok)
	}
	if p, ok := d.login("qa-one"); !ok || p.usable() {
		t.Error("a person with WhatsApp switched off must be found but not usable")
	}
	if _, ok := d.login("twin"); ok {
		t.Error("a login two checked people hold must route to nobody")
	}
	if _, ok := d.login("unchecked"); ok {
		t.Error("an unchecked GitHub login must not route")
	}
	if _, ok := d.person("u-149"); ok {
		t.Error("an inactive person is in the directory")
	}
	if p, ok := d.person("u-1"); !ok || p.Role != "QA" {
		t.Error("a person must be found by id")
	}

	// A failed refresh keeps the directory, marked stale.
	f.set(func(f *fakeTM) { f.fail = errors.New("down") })
	if err := d.refresh(ctx); err == nil {
		t.Fatal("a failed page did not fail the refresh")
	}
	if s := d.snapshot(); !s.Loaded || s.LastErr != "unavailable" || s.Counts.People != 149 {
		t.Errorf("a failed refresh must keep the directory with its error class: %+v", s.Counts)
	}
	if _, ok := d.login("dev-one"); !ok {
		t.Error("a failed refresh dropped the directory")
	}

	// Run with no enabled binding empties it, and calls nothing.
	f.set(func(f *fakeTM) { f.fail = nil })
	before := f.calls()
	d.enabled = func(context.Context) bool { return false }
	ctx2, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { d.Run(ctx2); close(done) }()
	deadline := time.Now().Add(2 * time.Second)
	for d.snapshot().Loaded && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	if s := d.snapshot(); s.Loaded || len(s.People) != 0 || f.calls() != before {
		t.Errorf("with no binding the directory must be empty and nothing called: %d people, %d call(s)", len(s.People), f.calls()-before)
	}
}
