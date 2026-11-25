package main

import (
	"bufio"
	"context"
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
	"time"
)

// The Brain Town consumer client (D-087, docs/designs/gaos-brain-town-consumer.md).
//
// Brain Town already receives the GitHub webhook and already turns it into
// `pr.opened` and `pr.merged` town events. Its public stream is masked on
// purpose -- a hidden product appears under its alias and every title is
// removed -- so GAOS cannot act on a pull request from it.
//
// The consumer path is the middle ground: a scoped, revocable machine token
// that reads the *stored* envelope, so GAOS learns the repository's real name
// and the pull request's title, and nothing else. Brain Town's side of the
// contract is its docs/integrations/gaos-consumer.md.
//
// Three rules shape everything below:
//
//   - Nothing here blocks a GAOS action. The reader runs on its own goroutine
//     and hands events over through a bounded channel.
//   - The repository GAOS acts on is the *product's*, never the event's. The
//     reader only uses the frame to find a product.
//   - Every credential is a file, mode 600, named by an environment variable.

const (
	// btConsumerTokenPrefix marks a Brain Town *reader* credential. It is
	// distinct from bt_live_ (an input credential) and from gaos_ (GAOS's own),
	// so one can never be presented as another.
	btConsumerTokenPrefix = "bt_c_"

	// btConsumerFrameMax bounds one SSE frame. The outbound feed bounds its
	// wire at 4 KiB (btBodyLimit); a reader that accepted an unbounded frame
	// would let one hostile delivery exhaust the dashboard's memory.
	btConsumerFrameMax = 4 << 10

	// btConsumerTitleMax bounds the pull request's title, which is
	// attacker-authored text and ends up in a task title.
	btConsumerTitleMax = 200

	// btConsumerFieldMax bounds the owner and the repository name.
	btConsumerFieldMax = 200

	// btConsumerQueue is the hand-over channel's capacity.
	btConsumerQueue = 256

	// btConsumerHeartbeatWait is the watchdog: three times the 15 s heartbeat
	// Brain Town sends. A connection that stops delivering heartbeats while
	// the socket stays open is a half-open connection, and only a deadline
	// notices it.
	btConsumerHeartbeatWait = 45 * time.Second

	// btConsumerConnectTimeout bounds the connect, not the stream. A timeout
	// that fits a POST would tear the stream down every few seconds.
	btConsumerConnectTimeout = 10 * time.Second

	// btConsumerReadTimeout bounds one read of the stream. It is the same
	// watchdog expressed on the socket.
	btConsumerReadTimeout = btConsumerHeartbeatWait

	// btConsumerRepoTTL is how long the reader's copy of the products'
	// repository list is reused before it is read again.
	btConsumerRepoTTL = 30 * time.Second

	// btConsumerDegradeAt is how many consecutive failures before the reader
	// holds at the longest wait, so a server that accepts and immediately
	// closes cannot make GAOS hammer it.
	btConsumerDegradeAt = 4
)

// btConsumerWaits are the waits before the 2nd, 3rd and 4th attempt. After
// the fourth failure the reader holds at the last one.
var btConsumerWaits = []time.Duration{time.Second, 5 * time.Second, 30 * time.Second}

// PREvent is one pull-request event the reader accepted.
//
// It is deliberately small: the reader's job is to route, not to interpret.
// The pull request's diff, body and author are not here -- Brain Town never
// stored them -- and the GitHub client fetches what the agents need.
type PREvent struct {
	Kind     string // the source kind, from the frame's "source"
	Type     string // "pr.opened", "pr.merged"
	EventID  string // the town's event id (gh-<delivery id>)
	Seq      int64  // the town's sequence number
	Org      string // the owner login
	Repo     string // the bare repository name
	PRNumber int    // the pull request number
	Title    string // the pull request's title
	Draft    bool   // the pull request is a draft
	Commit   string // a staging deploy's commit sha (type "staging.deployed")
	At       time.Time
}

// Key is the flow's de-duplication key. The flow owns the set; the card
// carries the key, so a restart rebuilds it from the graph.
func (e PREvent) Key(role string) string {
	if e.Commit != "" && e.PRNumber == 0 {
		return e.Org + "/" + e.Repo + "@" + e.Commit + ":" + role
	}
	return e.Org + "/" + e.Repo + "#" + strconv.Itoa(e.PRNumber) + ":" + role
}

// btConsumerConfig is the reader's configuration. An unusable setting leaves
// the reader off with a reason; it never stops GAOS.
type btConsumerConfig struct {
	enabled bool
	url     *url.URL
	token   string
	cursor  string
	types   map[string]bool
	kinds   map[string]bool
	why     string // non-empty: enabled was asked for but the reader is unusable
}

// btConsumerFromEnv reads the six settings.
func btConsumerFromEnv(api *apiConfig) btConsumerConfig {
	c := btConsumerConfig{types: map[string]bool{}, kinds: map[string]bool{}}
	switch strings.ToLower(strings.TrimSpace(os.Getenv("BRAIN_TOWN_CONSUMER_ENABLED"))) {
	case "true", "1":
		c.enabled = true
	default:
		return c
	}
	raw := strings.TrimSpace(os.Getenv("BRAIN_TOWN_CONSUMER_URL"))
	u, why := validConsumerURL(raw)
	if why != "" {
		c.why = why
		return c
	}
	c.url = u

	tokenPath := strings.TrimSpace(os.Getenv("BRAIN_TOWN_CONSUMER_TOKEN_FILE"))
	if tokenPath == "" {
		c.why = "BRAIN_TOWN_CONSUMER_TOKEN_FILE is not set"
		return c
	}
	tok, why := readConsumerToken(tokenPath, api)
	if why != "" {
		c.why = why
		return c
	}
	c.token = tok

	c.cursor = strings.TrimSpace(os.Getenv("BRAIN_TOWN_CONSUMER_CURSOR_FILE"))
	if c.cursor == "" {
		c.why = "BRAIN_TOWN_CONSUMER_CURSOR_FILE is not set"
		return c
	}
	for _, v := range splitList(os.Getenv("BRAIN_TOWN_CONSUMER_TYPES"), "pr.opened", "pr.merged") {
		c.types[v] = true
	}
	for _, v := range splitList(os.Getenv("BRAIN_TOWN_CONSUMER_KINDS"), "github") {
		c.kinds[v] = true
	}
	return c
}

// splitList reads a comma-separated setting, falling back to the defaults
// when it is empty.
func splitList(raw string, defaults ...string) []string {
	var out []string
	for _, v := range strings.Split(raw, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		out = defaults
	}
	return out
}

// validConsumerURL is the reader's URL rule. It is stricter than the outbound
// feed's: the reader's host is named by the setting, so there is no allow-list
// to build -- but a bare IP, a loopback address or a private range must never
// receive GAOS's bearer, and the base address carries no path, because the
// reader appends one.
func validConsumerURL(raw string) (*url.URL, string) {
	const label = "BRAIN_TOWN_CONSUMER_URL"
	if raw == "" {
		return nil, label + " is not set"
	}
	u, err := url.Parse(raw)
	switch {
	case err != nil || u.Host == "":
		return nil, label + " is not a URL"
	case u.User != nil:
		return nil, label + " may not carry credentials"
	case u.RawQuery != "" || u.Fragment != "":
		return nil, label + " is a base address, with no query or fragment"
	case u.Path != "" && u.Path != "/":
		return nil, label + " is a base address, with no path"
	case u.Scheme != "https":
		return nil, label + " is https only"
	}
	host := u.Hostname()
	if ip := net.ParseIP(host); ip != nil {
		return nil, label + " names an address, not a host name"
	}
	if strings.EqualFold(host, "localhost") {
		return nil, label + " names a loopback host"
	}
	return u, ""
}

// readConsumerToken reads the reader's token.
//
// The rules are the strict ones, not the loose ones: a token file others can
// read is refused outright (readTokenPathStrict's rule, BR-52), and the token
// must be a *reader* credential. A bt_live_ token is Brain Town's input
// credential and a gaos_ token is GAOS's own; a reader must hold neither.
//
// The file's path is never logged. The reason says what is wrong with the
// file, not where the file is.
func readConsumerToken(path string, api *apiConfig) (string, string) {
	st, err := os.Stat(path)
	if err != nil || !st.Mode().IsRegular() {
		return "", "the consumer token file cannot be read"
	}
	if st.Mode().Perm()&0o077 != 0 {
		return "", "the consumer token file is readable by group or others; it must be mode 0600"
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", "the consumer token file cannot be read"
	}
	tok := strings.TrimSuffix(strings.TrimSuffix(string(b), "\n"), "\r")
	if len(tok) < 16 || len(tok) > 256 {
		return "", "the consumer token file holds no token of 16 to 256 characters"
	}
	for _, r := range tok {
		if r > 127 || !isPrintableASCII(r) || r == ' ' {
			return "", "the consumer token file holds no printable token"
		}
	}
	switch {
	case strings.HasPrefix(tok, btConsumerTokenPrefix):
	case strings.HasPrefix(tok, "bt_live_"):
		return "", "the token is a Brain Town *input* credential; a reader must hold its own"
	case strings.HasPrefix(tok, apiTokenPrefix):
		return "", "the token is a GAOS credential; a reader must hold Brain Town's"
	default:
		return "", "the token is not a Brain Town consumer token (it must start with " + btConsumerTokenPrefix + ")"
	}
	if api != nil && api.holdsToken(tok) {
		return "", "the token is a GAOS credential; a reader must hold Brain Town's"
	}
	return tok, ""
}

func isPrintableASCII(r rune) bool {
	return r >= 0x21 && r <= 0x7e
}

// btConsumer is the reader.
type btConsumer struct {
	cfg  btConsumerConfig
	st   Store
	hc   *http.Client
	logf func(string, ...any)

	out chan PREvent

	mu         sync.Mutex
	last       int64
	frames     int64
	skipped    int64
	reconnects int64
	lastAt     time.Time
	lastErr    string

	reposMu sync.Mutex
	reposAt time.Time
	repos   map[string]bool
}

// newBTConsumer builds the reader. A nil reader means the setting is off.
func newBTConsumer(cfg btConsumerConfig, st Store) *btConsumer {
	c := &btConsumer{cfg: cfg, st: st, logf: log.Printf, out: make(chan PREvent, btConsumerQueue)}
	c.hc = &http.Client{
		Timeout: 0, // a stream has no whole-request deadline; the watchdog is the deadline
		Transport: &http.Transport{
			DialContext:           (&net.Dialer{Timeout: btConsumerConnectTimeout}).DialContext,
			ResponseHeaderTimeout: btConsumerConnectTimeout,
			DisableCompression:    true,
		},
	}
	return c
}

// Status is the Integrations row. It never carries a token, a path, a scope or
// a cursor value.
func (c *btConsumer) Status() BrainTownRow {
	if c == nil {
		return BrainTownRow{State: "disabled"}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	state := "enabled"
	if !c.cfg.enabled {
		state = "disabled"
	} else if c.cfg.why != "" {
		state = "unusable: " + c.cfg.why
	}
	host := ""
	if c.cfg.url != nil {
		host = urlHost(c.cfg.url.String())
	}
	last := ""
	if !c.lastAt.IsZero() {
		last = strconv.FormatInt(c.last, 10) + " · " + c.lastAt.UTC().Format(time.RFC3339)
	}
	return BrainTownRow{State: state, Host: host, Queue: len(c.out),
		Last: last, Failed: int(c.skipped), Dropped: 0, Sent: int(c.frames)}
}

// Run is the reader loop. It never returns until the context is done.
func (c *btConsumer) Run(ctx context.Context) {
	if c == nil || !c.cfg.enabled || c.cfg.why != "" {
		return
	}
	attempt := 0
	for ctx.Err() == nil {
		wait, ok := c.session(ctx)
		if ctx.Err() != nil {
			return
		}
		if !ok {
			attempt++
			c.mu.Lock()
			c.reconnects++
			c.mu.Unlock()
			if attempt >= btConsumerDegradeAt {
				wait = btConsumerWaits[len(btConsumerWaits)-1]
			} else {
				wait = btConsumerWaits[min(attempt-1, len(btConsumerWaits)-1)]
			}
			c.logf("brain town: consumer stream ended, retrying in %s", wait)
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
			continue
		}
		attempt = 0
	}
}

// session holds one connection. It returns false when the connection ended and
// the reader should reconnect.
func (c *btConsumer) session(ctx context.Context) (time.Duration, bool) {
	u := *c.cfg.url
	u.Path = strings.TrimSuffix(u.Path, "/") + "/consumer/events"
	if len(c.cfg.kinds) > 0 {
		q := u.Query()
		q.Set("kind", strings.Join(sortedKeys(c.cfg.kinds), ","))
		u.RawQuery = q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return 0, false
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.token)
	req.Header.Set("Accept", "text/event-stream")
	if last := c.readCursor(); last > 0 {
		req.Header.Set("Last-Event-ID", strconv.FormatInt(last, 10))
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		c.setErr("unreachable")
		return 0, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		c.setErr("the stream answers " + strconv.Itoa(resp.StatusCode))
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			// A revoked token will not come back by itself, and the token file
			// is re-read on the next attempt, so a rotated token is picked up
			// without a restart.
			c.logf("brain town: consumer token refused (%d); the reader stops", resp.StatusCode)
			c.stop()
			return 0, false
		}
		return 0, false
	}
	c.setErr("")
	return c.pump(ctx, resp.Body)
}

// pump reads the stream. It returns false when the connection ended.
func (c *btConsumer) pump(ctx context.Context, body io.Reader) (time.Duration, bool) {
	reader := bufio.NewReaderSize(body, 4096)
	var (
		id    int64
		event string
		data  []string
		size  int
	)
	readFrame := func() bool {
		defer func() { data = data[:0]; size = 0; event = "" }()
		if len(data) == 0 {
			return true
		}
		raw := strings.Join(data, "\n")
		c.handle(raw, event, &id)
		return true
	}
	for {
		if ctx.Err() != nil {
			return 0, false
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				readFrame()
			}
			return 0, false
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			readFrame()
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue // a comment: the heartbeat. It is not data.
		}
		field, value := line, ""
		if i := strings.Index(line, ":"); i >= 0 {
			field, value = line[:i], strings.TrimPrefix(line[i+1:], " ")
		}
		switch field {
		case "data":
			size += len(value)
			if size > btConsumerFrameMax {
				c.skip("a frame over the size limit")
				data = data[:0]
				size = 0
				continue
			}
			data = append(data, value)
		case "event":
			event = value
		case "id":
			if n, err := strconv.ParseInt(value, 10, 64); err == nil {
				id = n
			}
		}
	}
}

// handle turns one frame into a PREvent and hands it over.
func (c *btConsumer) handle(raw, event string, id *int64) {
	var f struct {
		Seq        int64          `json:"seq"`
		ID         string         `json:"id"`
		Type       string         `json:"type"`
		Source     string         `json:"source"`
		ProductKey string         `json:"productKey"`
		OrgLogin   string         `json:"orgLogin"`
		Payload    map[string]any `json:"payload"`
	}
	if err := json.Unmarshal([]byte(raw), &f); err != nil {
		c.skip("a frame that is not JSON")
		return
	}
	if len(c.cfg.kinds) > 0 && !c.cfg.kinds[f.Source] {
		return
	}
	if len(c.cfg.types) > 0 && !c.cfg.types[f.Type] {
		return
	}
	org := trimField(f.OrgLogin, btConsumerFieldMax)
	repo := trimField(f.ProductKey, btConsumerFieldMax)
	if org == "" || repo == "" {
		c.skip("a frame with no owner or no repository")
		return
	}
	if !c.ownsRepo(org + "/" + repo) {
		c.skip("a repository GAOS does not own")
		return
	}
	ev := PREvent{
		Kind: f.Source, Type: f.Type, EventID: f.ID, Seq: f.Seq,
		Org: org, Repo: repo, At: time.Now().UTC(),
	}
	if f.Payload != nil {
		ev.Title = cleanConsumerTitle(strField(f.Payload, "task", "title"))
		ev.PRNumber = int(numField(f.Payload, "data", "pr"))
		ev.Draft = boolField(f.Payload, "data", "draft")
		if at := strField(f.Payload, "at"); at != "" {
			if t, err := time.Parse(time.RFC3339, at); err == nil {
				ev.At = t.UTC()
			}
		}
	}
	select {
	case c.out <- ev:
		c.mu.Lock()
		c.frames++
		c.last = ev.Seq
		c.lastAt = ev.At
		c.mu.Unlock()
		c.writeCursor(ev.Seq)
	default:
		// The channel is full. The cursor does not advance, so the event is
		// replayed on the next reconnect rather than lost (BR-02).
		c.skip("the hand-over channel was full")
	}
}

// Drain reads the hand-over channel and calls handle for each event.
//
// It is the reader's only consumer, and it runs on its own goroutine: the
// flow must not block the reader, and the reader must not block the flow.
// The handler returns false to stop the drain.
//
// A full channel is the *reader's* rule, not the flow's: the reader keeps the
// event and does not advance the cursor, so the event is replayed on the next
// reconnect rather than lost (BR-02).
func (c *btConsumer) Drain(ctx context.Context, handle func(context.Context, PREvent) bool) {
	if c == nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-c.out:
			if !handle(ctx, ev) {
				return
			}
		}
	}
}

// ownsRepo reports whether a GAOS product declares this repository.
//
// The match is owner-qualified: the product's repositories are `owner/repo`
// pairs, so a repository of the same name under another owner does not match.
func (c *btConsumer) ownsRepo(pair string) bool {
	c.reposMu.Lock()
	defer c.reposMu.Unlock()
	if c.repos == nil || time.Since(c.reposAt) > btConsumerRepoTTL {
		c.repos = map[string]bool{}
		if masters, err := c.st.Masters(context.Background()); err == nil {
			for _, m := range masters {
				for _, r := range m.Repos {
					c.repos[strings.ToLower(strings.TrimSpace(r))] = true
				}
			}
		}
		c.reposAt = time.Now()
	}
	return c.repos[strings.ToLower(pair)]
}

// trimField caps a field and strips control characters.
func trimField(v string, max int) string {
	var sb strings.Builder
	n := 0
	for _, r := range v {
		if r < 0x20 || r == 0x7f {
			continue
		}
		sb.WriteRune(r)
		n++
		if n >= max {
			break
		}
	}
	return strings.TrimSpace(sb.String())
}

// cleanConsumerTitle caps the title and runs the egress secret detectors over
// it. A pull request's title is attacker-authored text and it ends up in a
// task title, which the outbound feed republishes.
func cleanConsumerTitle(v string) string {
	v = trimField(v, btConsumerTitleMax)
	if v == "" {
		return ""
	}
	for _, d := range detectors {
		if d.re.MatchString(v) {
			return ""
		}
	}
	return v
}

// readCursor reads the last sequence number the reader handed over.
func (c *btConsumer) readCursor() int64 {
	b, err := os.ReadFile(c.cfg.cursor)
	if err != nil {
		return 0
	}
	n, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// writeCursor records the last sequence number the reader handed over. The
// cursor advances only after the hand-over succeeded, so an event that could
// not be handed over is replayed.
func (c *btConsumer) writeCursor(seq int64) {
	if c.cfg.cursor == "" {
		return
	}
	tmp := c.cfg.cursor + ".tmp"
	if err := os.WriteFile(tmp, []byte(strconv.FormatInt(seq, 10)+"\n"), 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, c.cfg.cursor)
}

func (c *btConsumer) skip(why string) {
	c.mu.Lock()
	c.skipped++
	c.mu.Unlock()
	c.logf("brain town: consumer skipped a frame: %s", why)
}

func (c *btConsumer) setErr(e string) {
	c.mu.Lock()
	c.lastErr = e
	c.mu.Unlock()
}

// stop marks the reader as finished, so a revoked token is not retried in a
// loop. The token file is re-read on the next attempt, so a rotated token is
// picked up without a restart.
func (c *btConsumer) stop() {
	c.mu.Lock()
	c.cfg.enabled = false
	c.mu.Unlock()
}

// strField reads a string from a nested map.
func strField(m map[string]any, path ...string) string {
	v := m
	for i, k := range path {
		if i == len(path)-1 {
			s, _ := v[k].(string)
			return s
		}
		next, ok := v[k].(map[string]any)
		if !ok {
			return ""
		}
		v = next
	}
	return ""
}

// numField reads a number from a nested map.
func numField(m map[string]any, path ...string) float64 {
	v := m
	for i, k := range path {
		if i == len(path)-1 {
			f, _ := v[k].(float64)
			return f
		}
		next, ok := v[k].(map[string]any)
		if !ok {
			return 0
		}
		v = next
	}
	return 0
}

// boolField reads a boolean from a nested map.
func boolField(m map[string]any, path ...string) bool {
	v := m
	for i, k := range path {
		if i == len(path)-1 {
			b, _ := v[k].(bool)
			return b
		}
		next, ok := v[k].(map[string]any)
		if !ok {
			return false
		}
		v = next
	}
	return false
}

var _ = fmt.Sprintf
