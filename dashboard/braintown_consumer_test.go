package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The consumer reader: the configuration rules, the frame parser, the double
// filter and the cursor.

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func writeFile(t *testing.T, path, body string, mode os.FileMode) string {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestConsumerURLRules(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		why  string // a non-empty value means the URL was refused
	}{
		{"empty", "", "is not set"},
		{"not a url", "::::", "is not a URL"},
		{"plain http", "http://town.example.org", "is https only"},
		{"with credentials", "https://user:pw@town.example.org", "may not carry credentials"},
		{"with a query", "https://town.example.org?a=1", "no query or fragment"},
		{"with a path", "https://town.example.org/town", "with no path"},
		{"an ip address", "https://127.0.0.1", "names an address"},
		{"loopback", "https://localhost", "loopback"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, why := validConsumerURL(c.raw)
			if c.why == "" {
				if why != "" {
					t.Fatalf("want the URL accepted, got %q", why)
				}
				return
			}
			if !strings.Contains(why, c.why) {
				t.Fatalf("want a reason containing %q, got %q", c.why, why)
			}
		})
	}
	if _, why := validConsumerURL("https://town.example.org"); why != "" {
		t.Fatalf("a good URL must be accepted: %s", why)
	}
}

func TestConsumerTokenRules(t *testing.T) {
	dir := t.TempDir()
	api := &apiConfig{}

	// A reader token is accepted.
	good := writeFile(t, filepath.Join(dir, "good"), "bt_c_"+strings.Repeat("a", 48)+"\n", 0o600)
	if tok, why := readConsumerToken(good, api); why != "" || tok == "" {
		t.Fatalf("a reader token must be accepted: %q %q", tok, why)
	}

	// A file others can read is refused, not warned about.
	open := writeFile(t, filepath.Join(dir, "open"), "bt_c_"+strings.Repeat("a", 48)+"\n", 0o644)
	if _, why := readConsumerToken(open, api); !strings.Contains(why, "readable by group or others") {
		t.Fatalf("a loose file must be refused, got %q", why)
	}

	// The wrong kind of token is refused, each with its own reason.
	for _, c := range []struct{ tok, want string }{
		{"bt_live_" + strings.Repeat("a", 48), "input"},
		{"gaos_" + strings.Repeat("a", 48), "GAOS credential"},
		{strings.Repeat("a", 48), "must start with"},
	} {
		p := writeFile(t, filepath.Join(dir, "k"), c.tok+"\n", 0o600)
		if _, why := readConsumerToken(p, api); !strings.Contains(why, c.want) {
			t.Fatalf("token %q: want a reason containing %q, got %q", c.tok[:12], c.want, why)
		}
	}

	// A missing file is refused, not a panic.
	if _, why := readConsumerToken(filepath.Join(dir, "nope"), api); why == "" {
		t.Fatal("a missing file must be refused")
	}
}

func TestConsumerScopeFilter(t *testing.T) {
	// The seeded product p-1 declares the repositories "loop/laundry-api" and
	// "loop/laundry-web" (dashboard/fixtures.go:167).
	st := NewMemStore(1)
	c := newBTConsumer(btConsumerConfig{enabled: true, cursor: filepath.Join(t.TempDir(), "cursor")}, st)

	if !c.ownsRepo("loop/laundry-api") {
		t.Error("the product's own repository must match")
	}
	if !c.ownsRepo("LOOP/LAUNDRY-API") {
		t.Error("the match is case-insensitive")
	}
	if c.ownsRepo("other-org/laundry-api") {
		t.Error("the same repository name under another owner must not match")
	}
	if c.ownsRepo("laundry-api") {
		t.Error("a bare repository name must not match")
	}
}

func TestConsumerFrameIsParsedAndBounded(t *testing.T) {
	st := NewMemStore(1)
	c := newBTConsumer(btConsumerConfig{enabled: true, cursor: filepath.Join(t.TempDir(), "cursor"), kinds: map[string]bool{"github": true}, types: map[string]bool{"pr.opened": true}}, st)

	frame := `{"seq":42,"id":"gh-1","type":"pr.opened","source":"github","at":"2026-11-24T13:41:31Z",` +
		`"productKey":"laundry-api","orgLogin":"loop",` +
		`"payload":{"id":"gh-1","type":"pr.opened","at":"2026-11-24T13:41:31Z","org":"acme-org",` +
		`"product":{"key":"sample-service","name":"sample-service","visibility":"hidden"},` +
		`"task":{"id":"4931","kind":"pr","title":"Fix export"},"data":{"pr":4931,"draft":false}}}`
	var id int64
	c.handle(frame, "pr.opened", &id)
	if len(c.out) != 1 {
		t.Fatalf("the frame must be handed over, got %d", len(c.out))
	}
	ev := <-c.out
	if ev.Type != "pr.opened" || ev.Repo != "laundry-api" || ev.Org != "loop" {
		t.Errorf("the frame was read wrong: %+v", ev)
	}
	if ev.PRNumber != 4931 || ev.Title != "Fix export" {
		t.Errorf("the pull request was read wrong: %+v", ev)
	}
	if ev.Key("pr-reviewer") != "loop/laundry-api#4931:pr-reviewer" {
		t.Errorf("the key is %q", ev.Key("pr-reviewer"))
	}
}

func TestConsumerRejectsTheWrongKindAndType(t *testing.T) {
	st := NewMemStore(1)
	c := newBTConsumer(btConsumerConfig{enabled: true, cursor: filepath.Join(t.TempDir(), "cursor"), kinds: map[string]bool{"github": true}, types: map[string]bool{"pr.opened": true}}, st)
	var id int64
	c.handle(`{"seq":1,"type":"pr.merged","source":"github","productKey":"r","orgLogin":"o","payload":{}}`, "pr.merged", &id)
	c.handle(`{"seq":2,"type":"pr.opened","source":"qa","productKey":"r","orgLogin":"o","payload":{}}`, "pr.opened", &id)
	if len(c.out) != 0 {
		t.Fatalf("a frame outside the kinds or types must not be handed over, got %d", len(c.out))
	}
}

func TestConsumerRejectsAnUnownedRepository(t *testing.T) {
	st := NewMemStore(1)
	c := newBTConsumer(btConsumerConfig{enabled: true, cursor: filepath.Join(t.TempDir(), "cursor"), kinds: map[string]bool{"github": true}}, st)
	var id int64
	c.handle(`{"seq":1,"type":"pr.opened","source":"github","productKey":"elsewhere","orgLogin":"other-org","payload":{}}`, "pr.opened", &id)
	if len(c.out) != 0 {
		t.Fatalf("a repository GAOS does not own must not be handed over, got %d", len(c.out))
	}
}

func TestConsumerTitleIsCappedAndScreened(t *testing.T) {
	long := strings.Repeat("x", 400)
	if got := cleanConsumerTitle(long); len(got) != btConsumerTitleMax {
		t.Errorf("a long title must be capped, got %d", len(got))
	}
	if got := cleanConsumerTitle("fix\x00 the\nthing"); strings.ContainsRune(got, 0) || strings.Contains(got, "\n") {
		t.Errorf("control characters must be stripped, got %q", got)
	}
	if got := cleanConsumerTitle("token ghp_abcdefghijklmnopqrstuvwxyz0123456789"); got != "" {
		t.Errorf("a title that looks like a credential must be dropped, got %q", got)
	}
}

func TestConsumerCursorRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cursor")
	c := newBTConsumer(btConsumerConfig{enabled: true, cursor: path}, nil)
	if got := c.readCursor(); got != 0 {
		t.Errorf("a missing cursor is 0, got %d", got)
	}
	c.writeCursor(17)
	if got := c.readCursor(); got != 17 {
		t.Errorf("the cursor must round-trip, got %d", got)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Errorf("the cursor file must be mode 0600, got %o", st.Mode().Perm())
	}
}

func TestConsumerReadsTheStream(t *testing.T) {
	st := NewMemStore(1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer bt_c_test" {
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("retry: 3000\n\n"))
		w.Write([]byte("id: 5\nevent: pr.opened\ndata: {\"seq\":5,\"id\":\"gh-5\",\"type\":\"pr.opened\",\"source\":\"github\"," +
			"\"productKey\":\"laundry-api\",\"orgLogin\":\"loop\",\"payload\":{\"data\":{\"pr\":7},\"task\":{\"title\":\"T\"}}}\n\n"))
		w.(http.Flusher).Flush()
	}))
	defer srv.Close()

	dir := t.TempDir()
	cfg := btConsumerConfig{
		enabled: true, url: mustURL(t, srv.URL), token: "bt_c_test",
		cursor: filepath.Join(dir, "cursor"),
		kinds:  map[string]bool{"github": true}, types: map[string]bool{"pr.opened": true},
	}
	c := newBTConsumer(cfg, st)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx)

	select {
	case ev := <-c.out:
		if ev.Repo != "laundry-api" || ev.PRNumber != 7 {
			t.Errorf("the event was read wrong: %+v", ev)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no event within five seconds")
	}
}

func TestConsumerStatusCarriesNoSecret(t *testing.T) {
	st := NewMemStore(1)
	c := newBTConsumer(btConsumerConfig{enabled: true, cursor: filepath.Join(t.TempDir(), "cursor"), token: "bt_c_secret", url: mustURL(t, "https://town.example.org")}, st)
	row := c.Status()
	if strings.Contains(row.Host, "bt_c") || strings.Contains(row.Last, "bt_c") {
		t.Errorf("the row must carry no token: %+v", row)
	}
	if row.State != "enabled" {
		t.Errorf("the state is %q", row.State)
	}
	var off *btConsumer
	if off.Status().State != "disabled" {
		t.Error("a nil reader is disabled")
	}
}
