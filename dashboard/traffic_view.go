package main

// traffic_view.go is the back office's Traffic page (D-107, design
// docs/designs/traffic-monitor.md §10): what GAOS called and who called it,
// read from the traffic store, plus the in-memory summary of every direct
// source since the dashboard started. It changes nothing. Masking happens in
// the page model, before anything is rendered: no text, title, link, poll URL
// or filter value carries a public address a viewer may not see in full.

import (
	"errors"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	trafficReadEvery = 15 * time.Second // the read is cached this long, failed or not
	trafficInRows    = 50               // the inbound pairs shown
	trafficClip      = 48               // runes, before an ellipsis
)

// ---------- the read ----------

// tRec is one parsed record of any stream.
type tRec struct {
	Kind    string // the stream: "out:<label>" or "in:…"
	T       time.Time
	M       string
	S       int
	D       int64
	QB, RB  int64
	H, U, E string
	G       int
	X       string
	K, R    string
	Src     string
	Via     string
	C, Pid  string
}

func (r tRec) out() bool { return strings.HasPrefix(r.Kind, "out:") }

func (r tRec) platform() string { return strings.TrimPrefix(r.Kind, "out:") }

// failed is the Failed rule (§10.2): s ≥ 500, no response, or a TaskMgr call
// whose outcome the client counts as a failure.
func (r tRec) failed() bool {
	return r.S >= 500 || r.S <= 0 || (r.platform() == "taskmgr" && r.out() && tmFailed(r.X))
}

func (r tRec) is4xx() bool { return r.S >= 400 && r.S < 500 }

// trafficReader is the page's connection and its cache, shared by every viewer.
type trafficReader struct {
	mu      sync.Mutex
	conn    *respConn
	at      time.Time
	recs    []tRec
	err     error
	reads   int // round trips to the store, for the tests
	counted map[string]int
}

var errTrafficOff = errors.New("traffic recording is off")

// read answers every kept record of the last 48 hours, newest first per
// stream, and per stream how many it holds. One pipelined XREVRANGE per
// stream, at most once per 15 s for every viewer; a failed read is cached too.
func (t *trafficRecorder) read(now time.Time) ([]tRec, map[string]int, error) {
	if !t.on() {
		return nil, nil, errTrafficOff
	}
	rd := &t.rd
	rd.mu.Lock()
	defer rd.mu.Unlock()
	if !rd.at.IsZero() && !now.Before(rd.at) && now.Sub(rd.at) < trafficReadEvery {
		return rd.recs, rd.counted, rd.err
	}
	rd.at, rd.reads = now, rd.reads+1
	rd.recs, rd.counted, rd.err = nil, nil, nil
	if rd.conn == nil {
		c, err := dialRESP(t.addr, trafficUser, t.pass, trafficDial, trafficOpDeadline)
		if err != nil {
			rd.err = err
			return nil, nil, err
		}
		rd.conn = c
	}
	streams := trafficStreams()
	cmds := make([][]string, 0, len(streams))
	for _, s := range streams {
		cmds = append(cmds, []string{"XREVRANGE", s.key(), "+", "-", "COUNT", strconv.Itoa(s.Cap)})
	}
	rs, err := rd.conn.pipeline(cmds)
	if err != nil {
		rd.conn = nil
		rd.err = err
		return nil, nil, err
	}
	cutoff := now.Add(-trafficWindow)
	counted := map[string]int{}
	var out []tRec
	for i, v := range rs {
		if v.Err() != nil {
			rd.err = v.Err()
			return nil, nil, rd.err
		}
		for _, e := range v.arr {
			r, ok := parseTrafficEntry(streams[i].Kind, e)
			if !ok || r.T.Before(cutoff) {
				continue
			}
			counted[r.Kind]++
			out = append(out, r)
		}
	}
	rd.recs, rd.counted = out, counted
	return out, counted, nil
}

// parseTrafficEntry reads one stream entry strictly: an id and an even list of
// known fields with bounded values. Anything else is skipped.
func parseTrafficEntry(kind string, e respValue) (tRec, bool) {
	if e.kind != '*' || len(e.arr) != 2 || e.arr[1].kind != '*' || len(e.arr[1].arr)%2 != 0 || len(e.arr[1].arr) > 40 {
		return tRec{}, false
	}
	r := tRec{Kind: kind, QB: -1, RB: -1, G: -1}
	num := func(s string) (int64, bool) {
		n, err := strconv.ParseInt(s, 10, 64)
		return n, err == nil
	}
	f := e.arr[1].arr
	haveT := false
	for i := 0; i < len(f); i += 2 {
		if f[i].kind != '$' || f[i+1].kind != '$' || len(f[i+1].str) > 200 {
			return tRec{}, false
		}
		k, v := f[i].str, f[i+1].str
		var ok = true
		var n int64
		switch k {
		case "t":
			n, ok = num(v)
			r.T, haveT = time.UnixMilli(n), ok
		case "m":
			r.M = trafficMethod(v)
		case "s":
			n, ok = num(v)
			r.S = int(n)
		case "d":
			r.D, ok = num(v)
		case "qb":
			r.QB, ok = num(v)
		case "rb":
			r.RB, ok = num(v)
		case "g":
			n, ok = num(v)
			r.G = int(n)
		case "h":
			r.H = clipBytes(v, 100)
		case "u":
			r.U = clipBytes(v, 160)
		case "e":
			r.E = v
		case "x":
			r.X = v
		case "k":
			r.K = v
		case "r":
			r.R = v
		case "src":
			r.Src = clipBytes(v, 45)
		case "via":
			r.Via = v
		case "c":
			r.C = clipBytes(v, 64)
		case "pid":
			r.Pid = clipBytes(v, 16)
		}
		if !ok {
			return tRec{}, false
		}
	}
	return r, haveT
}

// ---------- masking ----------

// trafficPublic reports whether an address is public: not loopback, private,
// link-local, unspecified or in 100.64.0.0/10.
func trafficPublic(a netip.Addr) bool {
	a = a.Unmap()
	if a.IsLoopback() || a.IsPrivate() || a.IsLinkLocalUnicast() || a.IsUnspecified() || a.IsLinkLocalMulticast() {
		return false
	}
	return !netip.MustParsePrefix("100.64.0.0/10").Contains(a)
}

// trafficMask is a source as the viewer may see it: a public address in full
// to an auto-fix admin, else as its /24 (IPv4) or /48 (IPv6); every other
// source in full. A value that is neither an address nor a prefix is "unknown".
func trafficMask(src string, admin bool) string {
	var a netip.Addr
	if p, err := netip.ParsePrefix(src); err == nil {
		a = p.Addr()
		if !trafficPublic(a) || admin {
			return p.Masked().String()
		}
	} else if x, err := netip.ParseAddr(src); err == nil {
		a = x.Unmap()
		if !trafficPublic(a) || admin {
			return a.String()
		}
	} else {
		return "unknown"
	}
	bits := 48
	if a.Is4() {
		bits = 24
	}
	p, _ := a.Prefix(bits)
	return p.String()
}

// ---------- the page model ----------

// TrafficFilter is the page's filters, validated.
type TrafficFilter struct {
	Dir, Platform, Src, Status string
	Page                       int
}

// TrafficPage is the whole page.
type TrafficPage struct {
	OOB       bool
	PollURL   string
	State     string // off | unavailable | empty | filtered | normal
	OffReason string
	Checked   string // the status line's "read <ago>"
	Notes     []string
	Dropped   int
	Tiles     []AFTile
	Since     string // the boot time, absolute
	SinceAgo  string
	Sources   []TrafficSource
	Platforms []TrafficPlatform
	Inbound   []TrafficPair
	InMore    int
	Kept      []TrafficKept
	DirectMin string // the shortest in:direct:other kept-since: the review deadline
	Calls     []TrafficCall
	CallTotal int
	Pager     Pager
	Filter    TrafficFilter
	DirTabs   []AFTab
	PlatOpts  []AFOpt
	StatOpts  []AFOpt
	ResetHref string
	Filtered  bool
}

// TrafficSource is one row of the direct-source summary.
type TrafficSource struct {
	Src, SrcFull, Href           string
	First, FirstAt, Last, LastAt string
	Count                        int
	Class                        string
	Principal                    bool
	last                         time.Time
}

// TrafficPlatform is one row of the outbound table.
type TrafficPlatform struct {
	Label, Name, Host, HostFull, Href string
	Calls, Fourxx, Failed             int
	P50, P95                          string
	Last, LastAt, Kept, KeptAt        string
	Cap                               bool
	None                              bool
}

// TrafficPair is one row of the inbound table: a source and a route.
type TrafficPair struct {
	Src, SrcFull, Href, Via string
	Direct                  bool
	Client, Route           string
	Calls, Refused, Failed  int
	P95, Last, LastAt       string
	last                    time.Time
}

// TrafficKept is one inbound stream's kept span.
type TrafficKept struct {
	Name         string
	Count        int
	Since, AtAbs string
	Cap          bool
}

// TrafficCall is one row of the recent calls.
type TrafficCall struct {
	When, WhenAt   string
	Out            bool
	Who, WhoFull   string
	Call, CallFull string
	Status, Chip   string
	Time, Bytes    string
}

var trafficStatusOpts = []AFOpt{{"all", "Any status", false}, {"ok", "Answered", false}, {"4xx", "Refused (4xx)", false}, {"fail", "Failed", false}}

// trafficHref is the view's own address, relative to the page's base.
func trafficHref(f TrafficFilter, live ...string) string {
	v := url.Values{}
	if f.Dir != "" && f.Dir != "all" {
		v.Set("dir", f.Dir)
	}
	if f.Platform != "" {
		v.Set("platform", f.Platform)
	}
	if f.Src != "" {
		v.Set("src", f.Src)
	}
	if f.Status != "" && f.Status != "all" {
		v.Set("status", f.Status)
	}
	if f.Page > 1 {
		v.Set("page", strconv.Itoa(f.Page))
	}
	path := "ui/bo/traffic"
	if len(live) > 0 {
		path += "/live"
	}
	if len(v) == 0 {
		return path
	}
	return path + "?" + v.Encode()
}

// trafficFilter validates the query. An invalid value is dropped, never echoed;
// a source is compared after the viewer's masking, so a full public address a
// viewer may not see becomes its prefix here too.
func trafficFilter(q url.Values, admin bool) TrafficFilter {
	f := TrafficFilter{Dir: "all", Status: "all", Page: pageParam(q.Get("page"))}
	switch q.Get("dir") {
	case "out", "in":
		f.Dir = q.Get("dir")
	}
	if trafficPlatformKnown(q.Get("platform")) {
		f.Platform = q.Get("platform")
	}
	if s := strings.TrimSpace(q.Get("src")); s != "" && len(s) <= 64 {
		if m := trafficMask(s, admin); m != "unknown" {
			f.Src = m
		}
	}
	for _, o := range trafficStatusOpts {
		if q.Get("status") == o.Value {
			f.Status = o.Value
		}
	}
	switch {
	case f.Platform != "":
		f.Src, f.Dir = "", "out"
	case f.Src != "":
		f.Dir = "in"
	}
	return f
}

func trafficPctl(ds []int64, p float64) string {
	if len(ds) == 0 {
		return ""
	}
	s := append([]int64(nil), ds...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	// nearest rank
	k := int(p*float64(len(s))+0.999999) - 1
	k = min(max(k, 0), len(s)-1)
	return trafficMs(s[k])
}

func trafficMs(d int64) string {
	if d >= 10000 {
		return thousands(int(d/1000)) + " s"
	}
	return thousands(int(d)) + " ms"
}

func trafficBytes(n int64) string {
	switch {
	case n < 0:
		return "—"
	case n < 1024:
		return thousands(int(n)) + " B"
	case n < 1<<20:
		return strconv.FormatInt(n/1024, 10) + " KiB"
	}
	return strconv.FormatInt(n>>20, 10) + " MiB"
}

func trafficAbs(t time.Time) string { return t.UTC().Format("2006-01-02 15:04:05 UTC") }

func trafficAgo(t, now time.Time) string { return relSpan(now.Sub(t)) + " ago" }

// trafficPage builds the page for one viewer. rec may be nil (no recorder).
func trafficPage(rec *trafficRecorder, q url.Values, now time.Time, admin bool) *TrafficPage {
	p := &TrafficPage{Filter: trafficFilter(q, admin)}
	f := p.Filter
	mask := func(s string) string { return trafficMask(s, admin) }

	// The direct-source summary: shown in every state.
	boot := started
	var rows []directSource
	if rec != nil {
		boot = rec.started
		rows = rec.sum.rows()
	}
	p.Since, p.SinceAgo = trafficAbs(boot), trafficAgo(boot, now)
	for _, r := range rows {
		m := mask(r.Src)
		p.Sources = append(p.Sources, TrafficSource{Src: clipEllipsis(m, trafficClip), SrcFull: m,
			Href:  trafficHref(TrafficFilter{Src: m, Dir: "in"}),
			First: trafficAgo(r.First, now), FirstAt: trafficAbs(r.First), Last: trafficAgo(r.Last, now), LastAt: trafficAbs(r.Last),
			Count: int(r.Count), Class: r.LastClass, Principal: r.Principal, last: r.Last})
	}
	sort.SliceStable(p.Sources, func(i, j int) bool { return p.Sources[i].last.After(p.Sources[j].last) })
	unknown := unknownDirectSources(rows, mask)

	if rec != nil {
		if n := rec.dropped.Load(); n > 0 {
			p.Dropped = int(n)
			p.Notes = append(p.Notes, thousands(int(n))+" calls since "+p.Since+" were not recorded: the queue was full or the store did not answer.")
		}
		if n := rec.droppedAnon.Load(); n > 0 {
			p.Notes = append(p.Notes, thousands(int(n))+" anonymous API calls were not recorded: an anonymous flood is not allowed to crowd out the rest.")
		}
		rec.mu.Lock()
		fw := rec.firstWriteAt
		rec.mu.Unlock()
		if !fw.IsZero() && fw.After(rec.started.Add(time.Minute)) {
			p.Notes = append(p.Notes, "The traffic store may have been emptied at "+trafficAbs(fw)+": records before it are gone.")
		}
		if n := rec.sum.untracked.Load(); n > 0 {
			p.Notes = append(p.Notes, thousands(int(n))+" direct sources are not tracked: the summary is full.")
		}
	}

	p.PollURL = trafficHref(f, "live")
	p.ResetHref = trafficHref(TrafficFilter{})
	p.tabs(f)

	if !rec.on() {
		p.State = "off"
		p.OffReason = "no traffic recorder is installed"
		if rec != nil {
			p.OffReason = rec.why
		}
		p.Tiles = trafficTiles(0, 0, 0, unknown)
		return p
	}
	recs, counted, err := rec.read(now)
	p.Checked = "read " + trafficAgo(rec.readAt(), now)
	if err != nil {
		p.State = "unavailable"
		p.Tiles = trafficTiles(0, 0, 0, unknown)
		return p
	}

	// Outbound by platform.
	outCalls, outFailed, inAPI := 0, 0, 0
	byPlat := map[string][]tRec{}
	byIn := map[string][]tRec{}
	for _, r := range recs {
		if r.out() {
			byPlat[r.platform()] = append(byPlat[r.platform()], r)
			outCalls++
			if r.failed() {
				outFailed++
			}
		} else {
			byIn[r.Kind] = append(byIn[r.Kind], r)
			if r.K == kAPI {
				inAPI++
			}
		}
	}
	p.Tiles = trafficTiles(outCalls, outFailed, inAPI, unknown)
	for _, pl := range trafficPlatforms {
		rs := byPlat[pl.Label]
		row := TrafficPlatform{Label: pl.Label, Name: pl.Name, Href: trafficHref(TrafficFilter{Platform: pl.Label, Dir: "out"})}
		if len(rs) == 0 {
			row.None = true
			p.Platforms = append(p.Platforms, row)
			continue
		}
		var ds []int64
		oldest, newest := rs[0], rs[0]
		for _, r := range rs {
			row.Calls++
			if r.is4xx() {
				row.Fourxx++
			}
			if r.failed() {
				row.Failed++
			}
			if r.S > 0 {
				ds = append(ds, r.D)
			}
			if r.T.Before(oldest.T) {
				oldest = r
			}
			if r.T.After(newest.T) {
				newest = r
			}
		}
		row.HostFull = newest.H
		row.Host = clipEllipsis(newest.H, trafficClip)
		row.P50, row.P95 = trafficPctl(ds, 0.5), trafficPctl(ds, 0.95)
		row.Last, row.LastAt = trafficAgo(newest.T, now), trafficAbs(newest.T)
		row.Kept, row.KeptAt = trafficAgo(oldest.T, now), trafficAbs(oldest.T)
		row.Cap = counted[rs[0].Kind] >= pl.Cap
		p.Platforms = append(p.Platforms, row)
	}

	// Inbound by (masked source, route or class).
	pairs := map[string]*TrafficPair{}
	clients := map[string]map[string]bool{}
	durs := map[string][]int64{}
	for _, r := range recs {
		if r.out() {
			continue
		}
		src := mask(r.Src)
		route := r.R
		if r.K != kAPI || route == "" {
			route = "[" + r.K + "]"
		}
		key := src + "\x00" + route
		pr := pairs[key]
		if pr == nil {
			pr = &TrafficPair{Src: clipEllipsis(src, trafficClip), SrcFull: src, Via: r.Via, Direct: r.Via != "edge",
				Route: clipEllipsis(route, trafficClip), Href: trafficHref(TrafficFilter{Src: src, Dir: "in"})}
			pairs[key] = pr
			clients[key] = map[string]bool{}
		}
		pr.Calls++
		if r.is4xx() {
			pr.Refused++
		}
		if r.S >= 500 || r.S <= 0 {
			pr.Failed++
		}
		if r.K == kAPI {
			c := r.C
			if c == "" {
				c = "unauthenticated"
			}
			clients[key][c] = true
		}
		durs[key] = append(durs[key], r.D)
		if r.T.After(pr.last) {
			pr.last = r.T
		}
	}
	var list []TrafficPair
	for key, pr := range pairs {
		var cs []string
		for c := range clients[key] {
			cs = append(cs, c)
		}
		sort.Strings(cs)
		switch {
		case len(cs) == 0:
			pr.Client = "—"
		case len(cs) > 2:
			pr.Client = clipEllipsis(strings.Join(cs[:2], ", "), trafficClip) + " +" + strconv.Itoa(len(cs)-2)
		default:
			pr.Client = clipEllipsis(strings.Join(cs, ", "), trafficClip)
		}
		pr.P95 = trafficPctl(durs[key], 0.95)
		pr.Last, pr.LastAt = trafficAgo(pr.last, now), trafficAbs(pr.last)
		list = append(list, *pr)
	}
	sort.SliceStable(list, func(i, j int) bool {
		if !list[i].last.Equal(list[j].last) {
			return list[i].last.After(list[j].last)
		}
		return list[i].SrcFull+list[i].Route < list[j].SrcFull+list[j].Route
	})
	if len(list) > trafficInRows {
		p.InMore = len(list) - trafficInRows
		list = list[:trafficInRows]
	}
	p.Inbound = list

	// Kept records per inbound stream.
	for _, in := range trafficInbound {
		rs := byIn[in.Kind]
		k := TrafficKept{Name: in.Name, Count: len(rs), Cap: counted[in.Kind] >= in.Cap}
		if len(rs) > 0 {
			oldest := rs[0].T
			for _, r := range rs {
				if r.T.Before(oldest) {
					oldest = r.T
				}
			}
			k.Since, k.AtAbs = trafficAgo(oldest, now), trafficAbs(oldest)
			if in.Kind == inDirectOther {
				p.DirectMin = k.AtAbs
			}
		}
		p.Kept = append(p.Kept, k)
	}

	if len(recs) == 0 {
		p.State = "empty"
		return p
	}

	// Recent calls, filtered and paged.
	var shown []tRec
	for _, r := range recs {
		switch {
		case f.Dir == "out" && !r.out(), f.Dir == "in" && r.out():
			continue
		case f.Platform != "" && r.platform() != f.Platform:
			continue
		case f.Src != "" && (r.out() || mask(r.Src) != f.Src):
			continue
		}
		switch f.Status {
		case "ok":
			if r.failed() || r.S >= 400 {
				continue
			}
		case "4xx":
			if !r.is4xx() {
				continue
			}
		case "fail":
			if !r.failed() {
				continue
			}
		}
		shown = append(shown, r)
	}
	sort.SliceStable(shown, func(i, j int) bool { return shown[i].T.After(shown[j].T) })
	p.Filtered = f.Dir != "all" || f.Platform != "" || f.Src != "" || f.Status != "all"
	p.CallTotal = len(shown)
	p.Pager = paginate(len(shown), f.Page, tablePageSize, func(n int) (string, string) {
		g := f
		g.Page = n
		return trafficHref(g), pageHref(trafficHref(g))
	})
	p.Pager.Sync, p.Pager.Label = "#trafficLive:replace", "Call pages"
	f.Page = p.Pager.Page
	if f.Page <= 1 {
		f.Page = 0
	}
	p.Filter = f
	p.PollURL = trafficHref(f, "live")
	names := map[string]string{}
	for _, pl := range trafficPlatforms {
		names[pl.Label] = pl.Name
	}
	for _, r := range sliceFor(shown, p.Pager) {
		c := TrafficCall{When: trafficAgo(r.T, now), WhenAt: trafficAbs(r.T), Out: r.out(), Time: trafficMs(r.D)}
		if r.out() {
			c.Who, c.WhoFull = names[r.platform()], names[r.platform()]
			full := r.M + " " + r.H + r.U
			c.Call, c.CallFull = clipEllipsis(full, trafficClip), full
			c.Bytes = trafficBytes(r.RB)
		} else {
			who := mask(r.Src)
			if r.C != "" {
				who += " · " + r.C
			}
			c.Who, c.WhoFull = clipEllipsis(who, trafficClip), who
			route := r.R
			if r.K != kAPI || route == "" {
				route = "[" + r.K + "]"
			}
			full := r.M + " " + route
			c.Call, c.CallFull = clipEllipsis(full, trafficClip), full
			c.Bytes = trafficBytes(r.RB)
		}
		c.Status, c.Chip = trafficStatus(r)
		p.Calls = append(p.Calls, c)
	}
	if len(shown) == 0 {
		p.State = "filtered"
		return p
	}
	p.State = "normal"
	return p
}

// trafficStatus is a call's status chip: its code or word, never colour alone.
func trafficStatus(r tRec) (string, string) {
	switch {
	case r.out() && r.platform() == "taskmgr" && r.G >= 0:
		chip := "st-ok"
		if tmFailed(r.X) {
			chip = "st-bad"
		}
		return "gRPC " + strconv.Itoa(r.G), chip
	case r.S <= 0:
		e := r.E
		if e == "" {
			e = "no answer"
		}
		return e, "st-bad"
	case r.S >= 500 || (r.out() && r.platform() == "taskmgr" && tmFailed(r.X)):
		return strconv.Itoa(r.S), "st-bad"
	case r.S >= 400:
		return strconv.Itoa(r.S), "st-warn"
	}
	return strconv.Itoa(r.S), "st-ok"
}

func trafficTiles(outCalls, outFailed, inAPI, unknown int) []AFTile {
	failed := AFTile{Label: "Outbound failed", Value: thousands(outFailed), Note: "5xx, no answer, or a TaskMgr failure", Zero: outFailed == 0}
	if outFailed > 0 {
		failed.Tone = "block"
	}
	unk := AFTile{Label: "Unknown direct sources", Value: thousands(unknown), Note: "since the dashboard started, never authenticated", Zero: unknown == 0}
	if unknown > 0 {
		unk.Tone = "ask"
	}
	return []AFTile{
		{Label: "Outbound calls", Value: thousands(outCalls), Note: "kept, last 48 h", Zero: outCalls == 0},
		failed,
		{Label: "Inbound API calls", Value: thousands(inAPI), Note: "kept, last 48 h", Zero: inAPI == 0},
		unk,
	}
}

// tabs fills the direction tabs and the selects; the forced direction shows.
func (p *TrafficPage) tabs(f TrafficFilter) {
	for _, d := range []struct{ v, l string }{{"all", "All"}, {"out", "Outbound"}, {"in", "Inbound"}} {
		g := TrafficFilter{Dir: d.v, Status: f.Status}
		if d.v == "out" {
			g.Platform = f.Platform
		}
		if d.v == "in" {
			g.Src = f.Src
		}
		p.DirTabs = append(p.DirTabs, AFTab{Value: d.v, Label: d.l, Href: trafficHref(g), On: f.Dir == d.v})
	}
	p.PlatOpts = append(p.PlatOpts, AFOpt{"", "Every platform", f.Platform == ""})
	for _, pl := range trafficPlatforms {
		p.PlatOpts = append(p.PlatOpts, AFOpt{pl.Label, pl.Name, f.Platform == pl.Label})
	}
	for _, o := range trafficStatusOpts {
		o.On = f.Status == o.Value
		p.StatOpts = append(p.StatOpts, o)
	}
}

// readAt is when the cached read was made.
func (t *trafficRecorder) readAt() time.Time {
	t.rd.mu.Lock()
	defer t.rd.mu.Unlock()
	return t.rd.at
}

// ---------- the routes ----------

func (s *server) buildTraffic(r *http.Request, bo *BOView) {
	bo.Title = "Traffic"
	bo.Traffic = trafficPage(trafficRec(), r.URL.Query(), time.Now(), prViewerIsAdmin(r))
	bo.Sub = "Recent calls GAOS made to other platforms, and calls made to GAOS — at most 48 hours, each kind keeping only its newest records — and every direct caller since " + bo.Traffic.Since + "."
}

// handleTrafficLive is GET /ui/bo/traffic/live: the 30 s poll, one
// out-of-band element that replaces #trafficLive.
func (s *server) handleTrafficLive(w http.ResponseWriter, r *http.Request) {
	if !s.requireBO(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	page := trafficPage(trafficRec(), r.URL.Query(), time.Now(), prViewerIsAdmin(r))
	page.OOB = true
	s.rend.Render(w, http.StatusOK, "traffic-live", page)
}
