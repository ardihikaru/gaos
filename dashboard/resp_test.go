package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// The RESP2 client (resp.go, D-107 §6) and the fake store the traffic tests
// share. TC-884, TC-885.

// ---------------------------------------------------------------- a fake store

// fakeRedis is a loopback server that parses RESP2 commands, records them and
// answers from a script. reply answers one command with raw RESP; "" closes the
// connection instead.
type fakeRedis struct {
	ln    net.Listener
	mu    sync.Mutex
	cmds  [][]string
	conns int
	reply func(cmd []string) string
	open  []net.Conn
}

// fakeDefault answers AUTH with OK, XADD with an id, XTRIM with 0 and XREVRANGE
// with nothing.
func fakeDefault(cmd []string) string {
	switch strings.ToUpper(cmd[0]) {
	case "AUTH":
		return "+OK\r\n"
	case "XADD":
		return "$3\r\n1-0\r\n"
	case "XTRIM":
		return ":0\r\n"
	case "XREVRANGE":
		return "*0\r\n"
	}
	return "-ERR unknown command\r\n"
}

func newFakeRedis(t *testing.T, reply func([]string) string) *fakeRedis {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if reply == nil {
		reply = fakeDefault
	}
	f := &fakeRedis{ln: ln, reply: reply}
	go f.serve()
	t.Cleanup(f.close)
	return f
}

func (f *fakeRedis) addr() string { return f.ln.Addr().String() }

func (f *fakeRedis) close() {
	_ = f.ln.Close()
	f.mu.Lock()
	for _, c := range f.open {
		_ = c.Close()
	}
	f.mu.Unlock()
}

// dropConns closes every open connection, as a restarted store would.
func (f *fakeRedis) dropConns() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.open {
		_ = c.Close()
	}
	f.open = nil
}

func (f *fakeRedis) setReply(fn func([]string) string) {
	f.mu.Lock()
	f.reply = fn
	f.mu.Unlock()
}

func (f *fakeRedis) commands() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][]string(nil), f.cmds...)
}

func (f *fakeRedis) connCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.conns
}

func (f *fakeRedis) serve() {
	for {
		c, err := f.ln.Accept()
		if err != nil {
			return
		}
		f.mu.Lock()
		f.conns++
		f.open = append(f.open, c)
		f.mu.Unlock()
		go f.handle(c)
	}
}

func (f *fakeRedis) handle(c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	for {
		cmd, err := readFakeCommand(r)
		if err != nil {
			return
		}
		f.mu.Lock()
		f.cmds = append(f.cmds, cmd)
		reply := f.reply
		f.mu.Unlock()
		out := reply(cmd)
		if out == "" {
			return
		}
		if _, err := io.WriteString(c, out); err != nil {
			return
		}
	}
}

func readFakeCommand(r *bufio.Reader) ([]string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(line, "*") {
		return nil, errors.New("not an array")
	}
	n, err := strconv.Atoi(strings.TrimSpace(line[1:]))
	if err != nil {
		return nil, err
	}
	cmd := make([]string, 0, n)
	for i := 0; i < n; i++ {
		h, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		l, err := strconv.Atoi(strings.TrimSpace(h[1:]))
		if err != nil {
			return nil, err
		}
		b := make([]byte, l+2)
		if _, err := io.ReadFull(r, b); err != nil {
			return nil, err
		}
		cmd = append(cmd, string(b[:l]))
	}
	return cmd, nil
}

// ---------------------------------------------------------------- TC-884

func TestRESPRoundTripsTheTrafficCommands(t *testing.T) {
	enc := []struct {
		args []string
		want string
	}{
		{[]string{"AUTH", "gaos", "pw"}, "*3\r\n$4\r\nAUTH\r\n$4\r\ngaos\r\n$2\r\npw\r\n"},
		{[]string{"XADD", "gaos:traffic:out:github", "MAXLEN", "~", "3000", "*", "t", "1"},
			"*8\r\n$4\r\nXADD\r\n$23\r\ngaos:traffic:out:github\r\n$6\r\nMAXLEN\r\n$1\r\n~\r\n$4\r\n3000\r\n$1\r\n*\r\n$1\r\nt\r\n$1\r\n1\r\n"},
		{[]string{"XTRIM", "gaos:traffic:in:anon", "MINID", "~", "1700000000000-0"},
			"*5\r\n$5\r\nXTRIM\r\n$20\r\ngaos:traffic:in:anon\r\n$5\r\nMINID\r\n$1\r\n~\r\n$15\r\n1700000000000-0\r\n"},
		{[]string{"XREVRANGE", "gaos:traffic:in:auth", "+", "-", "COUNT", "2000"},
			"*6\r\n$9\r\nXREVRANGE\r\n$20\r\ngaos:traffic:in:auth\r\n$1\r\n+\r\n$1\r\n-\r\n$5\r\nCOUNT\r\n$4\r\n2000\r\n"},
		{[]string{"XADD", "k", "*", "u", ""}, "*5\r\n$4\r\nXADD\r\n$1\r\nk\r\n$1\r\n*\r\n$1\r\nu\r\n$0\r\n\r\n"},
	}
	for _, c := range enc {
		if got := string(appendCommand(nil, c.args)); got != c.want {
			t.Errorf("%v encodes as %q, want %q", c.args, got, c.want)
		}
	}

	parse := []struct {
		raw  string
		want respValue
	}{
		{"+OK\r\n", respValue{kind: '+', str: "OK"}},
		{"-OOM command not allowed\r\n", respValue{kind: '-', str: "OOM command not allowed"}},
		{":42\r\n", respValue{kind: ':', num: 42}},
		{":-1\r\n", respValue{kind: ':', num: -1}},
		{"$5\r\nhe\r\no\r\n", respValue{kind: '$', str: "he\r\no"}},
		{"$0\r\n\r\n", respValue{kind: '$', str: ""}},
		{"$-1\r\n", respValue{kind: '$', null: true}},
		{"*-1\r\n", respValue{kind: '*', null: true}},
	}
	for _, c := range parse {
		got, err := readReply(bufio.NewReader(strings.NewReader(c.raw)), 0)
		if err != nil || fmt.Sprint(got) != fmt.Sprint(c.want) {
			t.Errorf("%q parses as %+v %v, want %+v", c.raw, got, err, c.want)
		}
	}
	if v, _ := readReply(bufio.NewReader(strings.NewReader("-NOPERM no\r\n")), 0); v.Err() == nil || v.Err().Error() != "NOPERM no" {
		t.Errorf("an error reply's Err is %v, want NOPERM no", v.Err())
	}
	// An XREVRANGE answer: entries of an id and a field array.
	stream := "*2\r\n*2\r\n$3\r\n2-0\r\n*4\r\n$1\r\nt\r\n$1\r\n2\r\n$1\r\nm\r\n$3\r\nGET\r\n*2\r\n$3\r\n1-0\r\n*2\r\n$1\r\nt\r\n$1\r\n1\r\n"
	v, err := readReply(bufio.NewReader(strings.NewReader(stream)), 0)
	if err != nil || len(v.arr) != 2 || v.arr[0].arr[0].str != "2-0" || len(v.arr[0].arr[1].arr) != 4 || v.arr[1].arr[1].arr[1].str != "1" {
		t.Fatalf("an XREVRANGE reply parses as %+v %v, want two entries with their fields", v, err)
	}

	// A pipeline of 128 over a real socket: 128 replies, in order.
	n := 0
	var mu sync.Mutex
	f := newFakeRedis(t, func(cmd []string) string {
		if cmd[0] == "AUTH" {
			return "+OK\r\n"
		}
		mu.Lock()
		defer mu.Unlock()
		n++
		return ":" + strconv.Itoa(n) + "\r\n"
	})
	rc, err := dialRESP(f.addr(), "gaos", "pw", time.Second, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer rc.Close()
	var cmds [][]string
	for i := 0; i < 128; i++ {
		cmds = append(cmds, []string{"XADD", "gaos:traffic:in:anon", "MAXLEN", "~", "1000", "*", "i", strconv.Itoa(i)})
	}
	rs, err := rc.pipeline(cmds)
	if err != nil || len(rs) != 128 {
		t.Fatalf("a pipeline of 128 answered %d replies, %v", len(rs), err)
	}
	for i, r := range rs {
		if r.num != int64(i+1) {
			t.Fatalf("reply %d is %d; the replies are out of order", i, r.num)
		}
	}
	got := f.commands()
	if got[0][0] != "AUTH" || got[0][1] != "gaos" || got[0][2] != "pw" || len(got) != 129 || got[128][7] != "127" {
		t.Errorf("the store saw %d commands starting %v; want AUTH gaos then the 128 in order", len(got), got[0][:2])
	}
}

// ---------------------------------------------------------------- TC-885

func TestRESPRefusesMalformedOrOversizedReplies(t *testing.T) {
	bad := map[string]string{
		"a line over 1 KiB":           "+" + strings.Repeat("a", 1100) + "\r\n",
		"a bulk string over 64 KiB":   "$65537\r\n",
		"an array over 50 000":        "*50001\r\n",
		"nesting over 3":              "*1\r\n*1\r\n*1\r\n*1\r\n:1\r\n",
		"a negative length":           "$-2\r\n",
		"a negative array length":     "*-5\r\n",
		"a non-numeric length":        "$abc\r\n",
		"a non-numeric integer":       ":x1\r\n",
		"a truncated bulk":            "$5\r\nab",
		"a bulk without its CRLF":     "$2\r\nabcd",
		"a truncated array":           "*3\r\n:1\r\n",
		"an unknown type byte":        "!oops\r\n",
		"a line without CR":           "+OK\n",
		"an empty line":               "\r\n",
		"a huge declared bulk":        "$2147483647\r\n",
		"a huge declared array":       "*2147483647\r\n",
		"an over-long declared array": "*99999999999999999999\r\n",
	}
	for name, raw := range bad {
		if _, err := readReply(bufio.NewReader(strings.NewReader(raw)), 0); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	// Nothing is allocated from a declared length before the bound is checked.
	for _, raw := range []string{"$2147483647\r\n", "*2147483647\r\n", "*50000\r\n"} {
		var m0, m1 runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&m0)
		_, _ = readReply(bufio.NewReader(strings.NewReader(raw)), 0)
		runtime.ReadMemStats(&m1)
		if d := m1.TotalAlloc - m0.TotalAlloc; d > 64<<10 {
			t.Errorf("%q allocated %d bytes before failing; nothing may be allocated from a declared length", raw, d)
		}
	}
	// An error reply is cut to 80 bytes.
	v, _ := readReply(bufio.NewReader(strings.NewReader("-"+strings.Repeat("E", 300)+"\r\n")), 0)
	if len(v.Err().Error()) != 80 {
		t.Errorf("an error reply became a %d-byte error, want 80", len(v.Err().Error()))
	}

	// Over a socket: a malformed reply closes the connection.
	f := newFakeRedis(t, func(cmd []string) string {
		if cmd[0] == "AUTH" {
			return "+OK\r\n"
		}
		return "$99999999\r\n"
	})
	rc, err := dialRESP(f.addr(), "gaos", "pw", time.Second, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rc.pipeline([][]string{{"XTRIM", "k", "MINID", "~", "0-0"}}); err == nil {
		t.Fatal("an over-bound reply was accepted")
	}
	if _, err := rc.c.Write([]byte("*1\r\n$4\r\nPING\r\n")); err == nil {
		t.Error("the connection is still open after a malformed reply; it must be closed")
	}

	// A refused AUTH: one fixed error, no password in it, the connection closed.
	const secret = "s3cret-traffic-password-for-tests"
	f2 := newFakeRedis(t, func(cmd []string) string {
		return "-WRONGPASS invalid username-password pair or user is disabled. " + cmd[len(cmd)-1] + "\r\n"
	})
	rc2, err := dialRESP(f2.addr(), "gaos", secret, time.Second, 2*time.Second)
	if rc2 != nil || !errors.Is(err, errRESPAuth) {
		t.Fatalf("a refused AUTH answered %v %v, want errRESPAuth", rc2, err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("the AUTH error %q carries the password", err)
	}
}

// fakeStreams is a fake store with stream semantics: XADD appends and trims the
// key to its MAXLEN exactly, XREVRANGE answers newest first. It is the reply
// script of a fakeRedis.
type fakeStreams struct {
	mu   sync.Mutex
	seq  int64
	keys map[string][]fakeEntry
}

type fakeEntry struct {
	id     string
	fields []string
}

func newFakeStreams() *fakeStreams { return &fakeStreams{keys: map[string][]fakeEntry{}} }

func (fs *fakeStreams) reply(cmd []string) string {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	switch strings.ToUpper(cmd[0]) {
	case "AUTH":
		return "+OK\r\n"
	case "XADD":
		// XADD key MAXLEN ~ cap * field value …
		key := cmd[1]
		capN, _ := strconv.Atoi(cmd[4])
		fs.seq++
		id := strconv.FormatInt(fs.seq, 10) + "-0"
		es := append(fs.keys[key], fakeEntry{id: id, fields: append([]string(nil), cmd[6:]...)})
		if capN > 0 && len(es) > capN {
			es = append([]fakeEntry(nil), es[len(es)-capN:]...)
		}
		fs.keys[key] = es
		return "$" + strconv.Itoa(len(id)) + "\r\n" + id + "\r\n"
	case "XTRIM":
		return ":0\r\n"
	case "XREVRANGE":
		n := 1 << 30
		if len(cmd) >= 6 {
			n, _ = strconv.Atoi(cmd[5])
		}
		es := fs.keys[cmd[1]]
		var b strings.Builder
		k := min(n, len(es))
		b.WriteString("*" + strconv.Itoa(k) + "\r\n")
		for i := len(es) - 1; i >= len(es)-k; i-- {
			e := es[i]
			b.WriteString("*2\r\n$" + strconv.Itoa(len(e.id)) + "\r\n" + e.id + "\r\n")
			b.WriteString("*" + strconv.Itoa(len(e.fields)) + "\r\n")
			for _, f := range e.fields {
				b.WriteString("$" + strconv.Itoa(len(f)) + "\r\n" + f + "\r\n")
			}
		}
		return b.String()
	}
	return "-ERR unknown command\r\n"
}

// add stores one entry directly, as a record the writer would have written.
func (fs *fakeStreams) add(key string, fields ...string) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.seq++
	fs.keys[key] = append(fs.keys[key], fakeEntry{id: strconv.FormatInt(fs.seq, 10) + "-0", fields: fields})
}

func (fs *fakeStreams) entries(key string) []fakeEntry {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return append([]fakeEntry(nil), fs.keys[key]...)
}
