package main

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The Traffic page (traffic_view.go, D-107 §10). TC-898 … TC-901. The store is
// a fake with stream semantics; records are put in it as the writer would.

// trafficFixture is a recorder reading from a fake store, its writer
// unstarted so the page's own requests cannot change what it reads.
func trafficFixture(t *testing.T) (*trafficRecorder, *fakeStreams, *fakeRedis) {
	t.Helper()
	fs := newFakeStreams()
	f := newFakeRedis(t, fs.reply)
	rec := useTraffic(t, f.addr(), false)
	return rec, fs, f
}

// rereads drops the page's 15 s cache.
func rereads(rec *trafficRecorder) {
	rec.rd.mu.Lock()
	rec.rd.at = time.Time{}
	rec.rd.mu.Unlock()
}

func ms(t time.Time) string { return strconv.FormatInt(t.UnixMilli(), 10) }

func addOut(fs *fakeStreams, label string, at time.Time, s, d int, extra ...string) {
	f := []string{"t", ms(at), "m", "GET", "s", strconv.Itoa(s), "d", strconv.Itoa(d), "qb", "0", "rb", "12",
		"h", label + ".example", "u", "/v1/x", "e", ""}
	fs.add("gaos:traffic:out:"+label, append(f, extra...)...)
}

func addIn(fs *fakeStreams, kind string, at time.Time, src, via, k, r, c string, s int) {
	f := []string{"t", ms(at), "m", "GET", "s", strconv.Itoa(s), "d", "5"}
	if k == "api" {
		f = append(f, "qb", "0", "rb", "10", "k", k, "r", r, "src", src, "via", via, "c", c, "pid", "")
	} else {
		f = append(f, "k", k, "src", src, "via", via)
	}
	fs.add("gaos:traffic:"+kind, f...)
}

func platformRow(p *TrafficPage, label string) TrafficPlatform {
	for _, r := range p.Platforms {
		if r.Label == label {
			return r
		}
	}
	return TrafficPlatform{}
}

// ---------------------------------------------------------------- TC-898

func TestTrafficSummaries(t *testing.T) {
	rec, fs, _ := trafficFixture(t)
	now := time.Now()
	// GitHub: ten answered calls of 10 … 100 ms, one 404, one 502, one with no answer.
	for i := 1; i <= 10; i++ {
		addOut(fs, "github", now.Add(-time.Duration(20-i)*time.Minute), 200, i*10)
	}
	addOut(fs, "github", now.Add(-3*time.Minute), 404, 5)
	addOut(fs, "github", now.Add(-2*time.Minute), 502, 5)
	addOut(fs, "github", now.Add(-time.Minute), 0, 5, "e", "timeout")
	// TaskMgr: 200 with gRPC 16 (failed), 5 (an answer).
	addOut(fs, "taskmgr", now.Add(-time.Minute), 200, 3, "g", "16", "x", "auth")
	addOut(fs, "taskmgr", now.Add(-time.Minute), 200, 3, "g", "5", "x", "notfound")
	// Older than 48 h: not counted.
	addOut(fs, "qa-tool", now.Add(-49*time.Hour), 200, 1)
	// WhatsApp at its cap of 1000, Brain Town one under its cap.
	for i := 0; i < 1000; i++ {
		addOut(fs, "whatsapp", now.Add(-time.Duration(1000-i)*time.Second), 200, 1)
	}
	for i := 0; i < 999; i++ {
		addOut(fs, "brain-town", now.Add(-time.Duration(999-i)*time.Second), 200, 1)
	}
	// Inbound: 60 sources, one route each, newest last.
	for i := 0; i < 60; i++ {
		addIn(fs, "in:auth", now.Add(-time.Duration(60-i)*time.Second), fmt.Sprintf("198.51.100.%d", i+1), "edge", "api", "/v1/products", "qa-tools", 200)
	}
	addIn(fs, "in:direct:other", now.Add(-time.Hour), "10.10.0.9", "direct", "backoffice", "", "", 401)

	p := trafficPage(rec, url.Values{}, now, true)
	if p.State != "normal" {
		t.Fatalf("the page is %q, want normal", p.State)
	}
	var order []string
	for _, r := range p.Platforms {
		order = append(order, r.Label)
	}
	if strings.Join(order, " ") != "qa-tool ai-manager brain-town brain-town-reader github github-write whatsapp taskmgr" {
		t.Errorf("the platforms are %v, want the eight in order", order)
	}
	gh := platformRow(p, "github")
	if gh.Calls != 13 || gh.Fourxx != 1 || gh.Failed != 2 || gh.P50 != "40 ms" || gh.P95 != "100 ms" || gh.Last != "1m ago" {
		t.Errorf("GitHub is %+v; want 13 calls, 1 4xx, 2 failed, p50 40 ms, p95 100 ms over answered calls (nearest rank), last 1m ago", gh)
	}
	if gh.KeptAt != trafficAbs(time.UnixMilli(now.Add(-19*time.Minute).UnixMilli())) || gh.Cap {
		t.Errorf("GitHub is kept since %s (cap %v), want the oldest record's time and no cap", gh.KeptAt, gh.Cap)
	}
	if tm := platformRow(p, "taskmgr"); tm.Calls != 2 || tm.Failed != 1 {
		t.Errorf("TaskMgr is %+v; want 2 calls, 1 failed (gRPC 16), the not-found an answer", tm)
	}
	if qa := platformRow(p, "qa-tool"); !qa.None {
		t.Errorf("QA tool is %+v; a record older than 48 h must not be counted", qa)
	}
	if wa := platformRow(p, "whatsapp"); !wa.Cap || wa.Calls != 1000 {
		t.Errorf("WhatsApp is %+v; at its cap it must be marked cap", wa)
	}
	if bt := platformRow(p, "brain-town"); bt.Cap || bt.KeptAt != trafficAbs(time.UnixMilli(now.Add(-999*time.Second).UnixMilli())) {
		t.Errorf("Brain Town is %+v; one under its cap it is not cap, and kept since its oldest", bt)
	}
	if len(p.Inbound) != 50 || p.InMore != 11 || p.Inbound[0].SrcFull != "198.51.100.60" {
		t.Errorf("the inbound table has %d rows, %d more, first %q; want 50, 11 more, newest first", len(p.Inbound), p.InMore, p.Inbound[0].SrcFull)
	}
	var kept []string
	for _, k := range p.Kept {
		kept = append(kept, fmt.Sprintf("%s=%d", k.Name, k.Count))
	}
	if strings.Join(kept, ",") != "Direct, authenticated=0,Direct, other=1,Authenticated=60,Anonymous=0" || p.DirectMin == "" {
		t.Errorf("the kept records are %v (direct deadline %q), want each inbound stream with its count", kept, p.DirectMin)
	}
	tiles := map[string]AFTile{}
	for _, tl := range p.Tiles {
		tiles[tl.Label] = tl
	}
	if tiles["Outbound calls"].Value != "2,014" || tiles["Outbound failed"].Value != "3" || tiles["Outbound failed"].Tone != "block" ||
		tiles["Inbound API calls"].Value != "60" {
		t.Errorf("the tiles are %+v", p.Tiles)
	}

	// The emptied-store note: only when the first write came a minute after start.
	for _, c := range []struct {
		after time.Duration
		want  bool
	}{{30 * time.Second, false}, {2 * time.Minute, true}} {
		rec.mu.Lock()
		rec.firstWriteAt = rec.started.Add(c.after)
		rec.mu.Unlock()
		p = trafficPage(rec, url.Values{}, now, true)
		has := strings.Contains(strings.Join(p.Notes, "|"), "The traffic store may have been emptied at ")
		if has != c.want {
			t.Errorf("first write %v after start: the emptied note is %v, want %v", c.after, has, c.want)
		}
	}

	// The summary: since boot, and the untracked note past 512 sources.
	for i := 0; i < 513; i++ {
		rec.sum.see(fmt.Sprintf("10.20.%d.%d", i/250, i%250+1), kBackOffice, i == 0, now.Add(time.Duration(i)*time.Millisecond))
	}
	p = trafficPage(rec, url.Values{}, now, true)
	if len(p.Sources) != 512 || p.Since != trafficAbs(rec.started) || p.Sources[0].SrcFull != "10.20.2.12" {
		t.Errorf("the summary shows %d sources since %q, first %q; want 512 since boot, newest first", len(p.Sources), p.Since, p.Sources[0].SrcFull)
	}
	if !strings.Contains(strings.Join(p.Notes, "|"), "1 direct sources are not tracked: the summary is full.") {
		t.Errorf("the notes are %q; want the untracked note at 513 sources", p.Notes)
	}
	if tl := p.Tiles[3]; tl.Label != "Unknown direct sources" || tl.Value != "511" || tl.Tone != "ask" {
		t.Errorf("the unknown-sources tile is %+v, want 511 in tone ask", tl)
	}
}

// ---------------------------------------------------------------- TC-899

func TestTrafficFiltersAndPages(t *testing.T) {
	rec, fs, _ := trafficFixture(t)
	now := time.Now()
	for i := 0; i < 25; i++ {
		addOut(fs, "github", now.Add(-time.Duration(i)*time.Second), 200, 1)
	}
	addOut(fs, "qa-tool", now, 404, 1)
	addOut(fs, "qa-tool", now, 503, 1)
	addIn(fs, "in:direct:other", now, "10.10.0.9", "direct", "healthz", "", "", 200)
	addIn(fs, "in:anon", now, "203.0.113.9", "edge", "api", "/v1/products", "", 401)

	page := func(q string, admin bool) *TrafficPage {
		t.Helper()
		v, _ := url.ParseQuery(q)
		return trafficPage(rec, v, now, admin)
	}
	cases := []struct {
		q     string
		total int
		dir   string
	}{
		{"", 29, "all"},
		{"dir=out", 27, "out"},
		{"dir=in", 2, "in"},
		{"platform=qa-tool", 2, "out"},
		{"platform=qa-tool&dir=in", 2, "out"},
		{"src=10.10.0.9", 1, "in"},
		{"src=10.10.0.9&dir=out", 1, "in"},
		{"src=10.10.0.9&platform=github", 25, "out"},
		{"status=ok", 26, "all"},
		{"status=4xx", 2, "all"},
		{"status=fail", 1, "all"},
		{"dir=sideways&platform=pastebin&status=maybe&src=not-an-address", 29, "all"},
	}
	for _, c := range cases {
		p := page(c.q, false)
		if p.CallTotal != c.total || p.Filter.Dir != c.dir {
			t.Errorf("%q shows %d calls in direction %q, want %d in %q", c.q, p.CallTotal, p.Filter.Dir, c.total, c.dir)
		}
		on := ""
		for _, tab := range p.DirTabs {
			if tab.On {
				on = tab.Value
			}
		}
		if on != c.dir {
			t.Errorf("%q: the tab on is %q, want the forced %q", c.q, on, c.dir)
		}
	}
	if p := page("src=10.10.0.9&platform=github", false); p.Filter.Src != "" {
		t.Errorf("with both set the source %q was kept; it must be dropped", p.Filter.Src)
	}
	// A non-admin's source filter is masked: the full public address is not echoed.
	p := page("src=203.0.113.9", false)
	if p.Filter.Src != "203.0.113.0/24" || p.CallTotal != 1 || strings.Contains(p.PollURL, "203.0.113.9") {
		t.Errorf("a non-admin's src=203.0.113.9 is %q (%d calls, poll %q); want the /24, one call, no full address", p.Filter.Src, p.CallTotal, p.PollURL)
	}
	// Invalid values are ignored and never echoed.
	p = page("dir=%3Cscript%3E&platform=%3Cb%3E&status=%22x", false)
	for _, s := range []string{p.PollURL, p.ResetHref, p.Pager.Next.Href} {
		if strings.Contains(s, "script") || strings.Contains(s, "%3Cb") || strings.Contains(s, "x%22") {
			t.Errorf("an invalid value was echoed in %q", s)
		}
	}
	// Ten a page; the page and the filters ride in the poll URL.
	p = page("platform=github&page=3", false)
	if len(p.Calls) != 5 || p.Pager.Page != 3 || p.Pager.Pages != 3 {
		t.Errorf("page 3 of 25 GitHub calls shows %d rows, page %d of %d; want 5, 3 of 3", len(p.Calls), p.Pager.Page, p.Pager.Pages)
	}
	if p.PollURL != "ui/bo/traffic/live?dir=out&page=3&platform=github" {
		t.Errorf("the poll URL is %q, want the filters and the page", p.PollURL)
	}
	if p = page("page=1", false); len(p.Calls) != 10 {
		t.Errorf("the first page shows %d rows, want 10", len(p.Calls))
	}
	if p = page("platform=github&page=99", false); p.Pager.Page != 3 {
		t.Errorf("page 99 was clamped to %d, want 3", p.Pager.Page)
	}
}

// ---------------------------------------------------------------- TC-900

func TestTrafficPageStates(t *testing.T) {
	s, _ := newTestServer(t)
	cs := boCookies()
	render := func(path string) (int, string, http.Header) {
		t.Helper()
		w := pageGet(t, s, path, nil, cs...)
		return w.Code, w.Body.String(), w.Header()
	}

	// Off: the reason, the docs, and the summary.
	off := newTrafficRecorder("", "", "GAOS_REDIS_ADDR is not set")
	off.logf = func(string, ...any) {}
	prev := trafficPtr.Load()
	trafficPtr.Store(off)
	t.Cleanup(func() { trafficPtr.Store(prev) })
	off.sum.see("10.10.0.9", kHealthz, false, time.Now())
	code, body, hdr := render("/traffic")
	if code != 200 || !strings.Contains(body, "Traffic recording is off: GAOS_REDIS_ADDR is not set.") ||
		!strings.Contains(body, "This deployment has no traffic store configured.") || !strings.Contains(body, "10.10.0.9") {
		t.Errorf("the Off page (%d) lacks its reason, its docs line or the summary", code)
	}
	if hdr.Get("Cache-Control") != "no-store" {
		t.Errorf("GET /traffic sends Cache-Control %q, want no-store", hdr.Get("Cache-Control"))
	}

	// Unavailable: the store refuses; the summary still shows; no retry in 15 s.
	f := newFakeRedis(t, nil)
	f.close()
	rec := useTraffic(t, f.addr(), false)
	rec.sum.see("10.10.0.9", kHealthz, false, time.Now())
	_, body, _ = render("/traffic")
	if !strings.Contains(body, `role="alert"`) || !strings.Contains(body, "The traffic store is not answering.") ||
		!strings.Contains(body, "GAOS keeps working; calls made meanwhile are not recorded.") || !strings.Contains(body, "10.10.0.9") {
		t.Errorf("the Unavailable page lacks its alert or the summary")
	}
	_, _, _ = render("/traffic")
	if rec.rd.reads != 1 {
		t.Errorf("a failed read was tried %d times within 15 s, want once", rec.rd.reads)
	}

	// Empty, Filtered empty, Normal.
	rec, fs, fr := trafficFixture(t)
	_, body, _ = render("/traffic")
	if !strings.Contains(body, "No call recorded in the last 48 hours.") || !strings.Contains(body, "No calls kept") {
		t.Errorf("the Empty page lacks its text")
	}
	addOut(fs, "github", time.Now(), 200, 3)
	rereads(rec)
	_, body, _ = render("/traffic?platform=qa-tool")
	if !strings.Contains(body, "No call matches these filters.") || !strings.Contains(body, "Clear them") {
		t.Errorf("the Filtered empty page lacks its text")
	}
	_, body, _ = render("/traffic")
	if !strings.Contains(body, `id="trafficLive"`) || !strings.Contains(body, "github.example/v1/x") || !strings.Contains(body, "Outbound by platform") {
		t.Errorf("the Normal page lacks the call")
	}
	w := pageGet(t, s, "/ui/bo/traffic/live", map[string]string{"HX-Request": "true"}, cs...)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `hx-swap-oob="true"`) || w.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("the poll answered %d (%q); want the live block out of band, no-store", w.Code, w.Header().Get("Cache-Control"))
	}

	// Signed out: refused, and the store is not touched.
	dials := fr.connCount()
	rereads(rec)
	for _, p := range []string{"/traffic", "/ui/bo/traffic", "/ui/bo/traffic/live"} {
		if w := pageGet(t, s, p, map[string]string{"HX-Request": "true"}); w.Code != http.StatusUnauthorized {
			t.Errorf("signed out, %s answered %d, want 401", p, w.Code)
		}
		if w := pageGet(t, s, p, nil); w.Code != http.StatusUnauthorized {
			t.Errorf("signed out, %s answered %d, want 401", p, w.Code)
		}
	}
	if fr.connCount() != dials {
		t.Errorf("a signed-out request touched the store")
	}
	rec.rd.mu.Lock()
	untouched := rec.rd.at.IsZero()
	rec.rd.mu.Unlock()
	if !untouched {
		t.Errorf("a signed-out request read the store")
	}
}

// ---------------------------------------------------------------- TC-901

func TestTrafficPageEscapesAndMasksAddresses(t *testing.T) {
	s, _ := newTestServer(t)
	cs := boCookies()
	rec, fs, _ := trafficFixture(t)
	now := time.Now()
	evil := []string{"<script>alert(1)</script>", `"><img onerror=alert(1)>`, strings.Repeat("Ω", 1024)}
	for _, e := range evil {
		addOut(fs, "github", now, 200, 1, "h", e, "u", e)
		addIn(fs, "in:auth", now, "198.51.100.7", "edge", "api", e, e, 200)
	}
	addIn(fs, "in:direct:other", now, "203.0.113.9", "direct", "backoffice", "", "", 401)
	addIn(fs, "in:direct:other", now, "2001:db8:1:2::/64", "direct", "public", "", "", 200)
	rec.sum.see("203.0.113.9", kBackOffice, false, now)
	rec.sum.see("203.0.113.77", kBackOffice, false, now)
	rec.sum.see("2001:db8:1:2::/64", kPublic, false, now)
	rec.sum.see("10.10.0.9", kHealthz, false, now)
	rec.sum.see("100.64.1.2", kHealthz, false, now)

	// The tiles first: the page requests below are direct callers too.
	rereads(rec)
	if p := trafficPage(rec, url.Values{}, now, false); p.Tiles[3].Value != "4" {
		t.Errorf("for a non-admin the unknown-sources tile is %s, want 4 (two public sources in one /24 count once)", p.Tiles[3].Value)
	}
	rereads(rec)
	if p := trafficPage(rec, url.Values{}, now, true); p.Tiles[3].Value != "5" {
		t.Errorf("for an admin the unknown-sources tile is %s, want 5", p.Tiles[3].Value)
	}
	get := func(path string) string {
		t.Helper()
		rereads(rec)
		w := pageGet(t, s, path, nil, cs...)
		if w.Code != 200 {
			t.Fatalf("GET %s answered %d", path, w.Code)
		}
		return w.Body.String()
	}
	t.Setenv("GAOS_AUTOFIX_ADMINS", "")
	for _, path := range []string{"/traffic", "/traffic?src=203.0.113.9", "/traffic?dir=in"} {
		body := get(path)
		for _, e := range evil[:2] {
			if strings.Contains(body, e) {
				t.Errorf("%s renders %q unescaped", path, e)
			}
		}
		if strings.Contains(body, strings.Repeat("Ω", 100)) {
			t.Errorf("%s renders a 2 KiB value unclipped", path)
		}
		for _, full := range []string{"203.0.113.9", "203.0.113.77", "2001:db8:1:2::"} {
			if strings.Contains(body, full) {
				t.Errorf("a non-admin's %s carries the full public address %s", path, full)
			}
		}
		for _, masked := range []string{"203.0.113.0/24", "2001:db8:1::/48", "10.10.0.9", "100.64.1.2"} {
			if !strings.Contains(body, masked) {
				t.Errorf("a non-admin's %s lacks %s", path, masked)
			}
		}
	}
	autoFixAdminsFile(t, 0o600, testEmail)
	body := get("/traffic")
	for _, full := range []string{"203.0.113.9", "203.0.113.77", "2001:db8:1:2::/64"} {
		if !strings.Contains(body, full) {
			t.Errorf("an admin's page lacks the full address %s", full)
		}
	}
}
