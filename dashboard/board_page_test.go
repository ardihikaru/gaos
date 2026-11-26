package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
)

// The tests of D-095: the Board a page at a time (board_page.go,
// docs/designs/urls-and-board.md §4, §5, §10.2).

// bigBoard is a product with n synthetic cards on top of the fixtures': nine
// in ten Done, the rest spread over the other columns; three roles, two
// stories and the plan, a label on every fourth, a workflow per twenty, six
// moves each and a run on every second. It is what §1.2 of the design measured.
func bigBoard(tb testing.TB, n int) (*server, *MemStore) {
	tb.Helper()
	rend, err := NewRenderer(false, ".")
	if err != nil {
		tb.Fatal(err)
	}
	st := NewMemStore(1)
	srv := &server{store: st, rend: rend, arc: &Arcade{DB: "gaos"}, auth: newTestAuthority()}
	other := []string{StatusPending, StatusReady, StatusRunning, StatusAwaitingInput, StatusReview, StatusBlocked, StatusFailed, StatusCanceled}
	roles := []string{"backend", "frontend", "qa"}
	stories := []string{"US-01", "US-02", ""}
	st.mu.Lock()
	defer st.mu.Unlock()
	for i := 0; i < n; i++ {
		status := StatusDone
		if i%10 == 0 {
			status = other[(i/10)%len(other)]
		}
		t := TaskRecord{ID: fmt.Sprintf("bt-%05d", i), DeskID: "p-1", Title: fmt.Sprintf("Synthetic task %05d with a title of ordinary length", i),
			Status: status, Role: roles[i%3], Story: stories[i%3], Rank: i, WorkflowID: fmt.Sprintf("wf-%03d", i/20)}
		if i%4 == 0 {
			t.Labels = []string{LabelUnitTest}
		}
		if i%50 == 7 {
			t.DedupKey = fmt.Sprintf("loop/laundry-api#%d:review", i)
		}
		st.tasks = append(st.tasks, t)
		for k := 0; k < 6; k++ {
			st.emit(TaskMoved, "p-1", "moved", KindInfo, EventPayload{Task: t.ID, To: status})
		}
		if i%2 == 0 {
			st.runs = append(st.runs, TaskRunRecord{TaskID: t.ID, DeskID: "p-1"})
		}
	}
	return srv, st
}

func colOf(p BoardPage, id string) BoardPageColumn {
	for _, c := range p.Columns {
		if c.ID == id {
			return c
		}
	}
	return BoardPageColumn{}
}

func cardIDs(cards []BoardCard) []string {
	out := make([]string, len(cards))
	for i, c := range cards {
		out[i] = c.ID
	}
	return out
}

// boardReq is a request as the Board's script makes one: the view and window
// in the URL, or in the X-Board-State header, and the search text in its own.
func boardReq(rawURL, state, search string) *http.Request {
	r := httptest.NewRequest("GET", rawURL, nil)
	if state != "" {
		r.Header.Set(boardStateHeader, state)
	}
	if search != "" {
		r.Header.Set(boardSearchHeader, url.QueryEscape(search))
	}
	return r
}

func TestBoardPageCapsEachColumnAndCountsTheRest(t *testing.T) {
	// Its own store and server, so it runs beside the others: under the race
	// detector a rendered board is slow, and the suite has ten minutes.
	t.Parallel()
	_, st := bigBoard(t, 2000)
	ctx := context.Background()
	full, _ := st.Board(ctx, "p-1")
	inCol := map[string]int{}
	for _, c := range full.Cards {
		inCol[c.Column]++
	}

	p, err := st.BoardPage(ctx, "p-1", BoardQuery{}, BoardWindow{})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Columns) != len(boardColumns) || p.All != len(full.Cards) || p.Matched != len(full.Cards) {
		t.Fatalf("the page has %d columns, All %d and Matched %d; want %d columns and %d for both, unfiltered",
			len(p.Columns), p.All, p.Matched, len(boardColumns), len(full.Cards))
	}
	total := 0
	for _, c := range p.Columns {
		want := min(inCol[c.ID], boardStep)
		if c.Matched != inCol[c.ID] || c.Shown != want || len(c.Cards) != want {
			t.Errorf("column %s: Matched %d, Shown %d, %d card(s); want Matched %d (the column's full count) and %d shown",
				c.ID, c.Matched, c.Shown, len(c.Cards), inCol[c.ID], want)
		}
		total += len(c.Cards)
	}
	if total > len(boardColumns)*boardStep {
		t.Errorf("the default page holds %d cards; it must hold at most %d", total, len(boardColumns)*boardStep)
	}

	for _, c := range []struct {
		name, shown string
		want        int
	}{
		{"a deeper column", "done:90", 90},
		{"above the cap", "done:5000", boardMaxShown},
		{"below one: ignored, the default", "done:0", boardStep},
		{"negative: ignored, the default", "done:-5", boardStep},
		{"not a number: ignored, the default", "done:lots", boardStep},
		{"not a column: dropped", "nope:90", boardStep},
		{"over 256 bytes: ignored whole", "done:90," + strings.Repeat("x", 260), boardStep},
	} {
		_, w := parseBoardQuery(boardReq("/products/p-1/board/live?shown="+url.QueryEscape(c.shown), "", ""))
		got, _ := st.BoardPage(ctx, "p-1", BoardQuery{}, w)
		if n := colOf(got, ColDone).Shown; n != c.want {
			t.Errorf("shown=%s (%s): Done shows %d card(s), want %d", clip(c.shown, 30), c.name, n, c.want)
		}
	}
	// "more" deepens one column and is clamped again.
	_, w := parseBoardQuery(boardReq("/x?shown=done:290&more=done", "", ""))
	if w.Shown[ColDone] != boardMaxShown {
		t.Errorf("more=done at 290 gives %d, want the cap %d", w.Shown[ColDone], boardMaxShown)
	}
	// A full nine-column rev is well inside the limit and is read whole.
	var pairs []string
	for _, c := range boardColumns {
		pairs = append(pairs, c.ID+":0123456789abcdef")
	}
	if got := revOf(boardReq("/x?rev="+strings.Join(pairs, ","), "", "")); len(got) != len(boardColumns) {
		t.Errorf("a full rev of %d columns was read as %d; it must be accepted whole", len(boardColumns), len(got))
	}
	if got := revOf(boardReq("/x?rev=done:XYZ,ready:0123456789abcdef", "", "")); len(got) != 1 {
		t.Errorf("a rev with a malformed hash kept %d pair(s), want only the well-formed one", len(got))
	}
	if _, err := st.BoardPage(ctx, "nope", BoardQuery{}, BoardWindow{}); err != ErrNotFound {
		t.Errorf("BoardPage of an unknown product: %v, want ErrNotFound", err)
	}
}

func TestBoardPageDeeperWindowsContainShallowerOnes(t *testing.T) {
	// Its own store and server, so it runs beside the others: under the race
	// detector a rendered board is slow, and the suite has ten minutes.
	t.Parallel()
	_, st := bigBoard(t, 600)
	ctx := context.Background()
	p30, _ := st.BoardPage(ctx, "p-1", BoardQuery{}, BoardWindow{})
	p60, _ := st.BoardPage(ctx, "p-1", BoardQuery{}, BoardWindow{Shown: map[string]int{ColDone: 60}})
	a, b := cardIDs(colOf(p30, ColDone).Cards), cardIDs(colOf(p60, ColDone).Cards)
	if len(a) != 30 || len(b) != 60 || !reflect.DeepEqual(a, b[:30]) {
		t.Errorf("Done at depth 60 does not start with the 30 cards it shows at depth 30: \"Show more\" would reshuffle the column")
	}
	if !reflect.DeepEqual(cardIDs(colOf(p30, ColReady).Cards), cardIDs(colOf(p60, ColReady).Cards)) {
		t.Errorf("deepening Done changed another column")
	}
	seen := map[string]bool{}
	for _, id := range b {
		if seen[id] {
			t.Errorf("card %s is in Done twice", id)
		}
		seen[id] = true
	}
}

func TestBoardFiltersNarrowCardsAndCounts(t *testing.T) {
	// Its own store and server, so it runs beside the others: under the race
	// detector a rendered board is slow, and the suite has ten minutes.
	t.Parallel()
	_, st := bigBoard(t, 400)
	ctx := context.Background()
	st.mu.RLock()
	var mine []TaskRecord
	for _, tk := range st.tasks {
		if tk.DeskID == "p-1" {
			mine = append(mine, tk)
		}
	}
	st.mu.RUnlock()
	count := func(ok func(TaskRecord) bool) int {
		n := 0
		for _, tk := range mine {
			if ok(tk) {
				n++
			}
		}
		return n
	}
	page := func(rawURL, state, search string) BoardPage {
		t.Helper()
		q, w := parseBoardQuery(boardReq(rawURL, state, search))
		p, err := st.BoardPage(ctx, "p-1", q, w)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	hasLabel := func(tk TaskRecord, l string) bool {
		for _, x := range tk.Labels {
			if x == l {
				return true
			}
		}
		return false
	}
	for _, c := range []struct {
		name, url string
		want      int
	}{
		{"role", "/x?role=qa", count(func(tk TaskRecord) bool { return tk.Role == "qa" })},
		{"story", "/x?story=US-02", count(func(tk TaskRecord) bool { return tk.Story == "US-02" })},
		{"the plan", "/x?story=plan", count(func(tk TaskRecord) bool { return tk.Story == "" })},
		{"label", "/x?label=" + LabelUnitTest, count(func(tk TaskRecord) bool { return hasLabel(tk, LabelUnitTest) })},
		{"workflow", "/x?wf=wf-003", count(func(tk TaskRecord) bool { return tk.WorkflowID == "wf-003" })},
		{"waiting on a person", "/x?waiting=1", count(func(tk TaskRecord) bool { return tk.Status == StatusAwaitingInput || tk.Status == StatusReview })},
		{"two together", "/x?role=backend&story=US-01", count(func(tk TaskRecord) bool { return tk.Role == "backend" && tk.Story == "US-01" })},
	} {
		p := page(c.url, "", "")
		sum := 0
		for _, col := range p.Columns {
			sum += col.Matched
		}
		if p.Matched != c.want || sum != c.want || c.want == 0 || c.want == p.All {
			t.Errorf("filter %s (%s): Matched %d, the columns add up to %d; want %d, a real narrowing of %d",
				c.name, c.url, p.Matched, sum, c.want, p.All)
		}
	}
	// The search text comes from its header, matches the title, the id and the
	// dedup key, and is handed back for the box to show.
	if p := page("/x", "", "BT-00007"); p.Matched != 1 || p.Text != "BT-00007" {
		t.Errorf("searching an id found %d card(s) and hands back %q; want one card and the text for the search box", p.Matched, p.Text)
	}
	if p := page("/x", "", "laundry-api#7:"); p.Matched != 1 {
		t.Errorf("searching a pull-request key found %d card(s), want 1", p.Matched)
	}
	if p := page("/x?q=bt-00007", "", ""); p.Matched != p.All || p.Text != "" {
		t.Errorf("a q= in the URL narrowed the board to %d of %d: the search text is read from its header only", p.Matched, p.All)
	}
	if _, w := parseBoardQuery(boardReq("/x", "", strings.Repeat("é", 200))); len([]rune(w.Text)) != boardMaxSearch {
		t.Errorf("a 200-rune search text was cut to %d runes, want %d", len([]rune(w.Text)), boardMaxSearch)
	}

	// The header carries the state for a request whose URL does not; the URL
	// wins for a key it names, also when it names it empty.
	if p := page("/x", "role=qa&story=US-01", ""); p.Query.Role != "qa" || p.Query.Story != "US-01" {
		t.Errorf("the state header was read as role %q, story %q; want qa and US-01", p.Query.Role, p.Query.Story)
	}
	if p := page("/x?role=backend", "role=qa&story=US-01", ""); p.Query.Role != "backend" || p.Query.Story != "US-01" {
		t.Errorf("with role in the URL and in the header the page has role %q, story %q; the URL wins, the rest comes from the header", p.Query.Role, p.Query.Story)
	}
	if p := page("/x?role=&view=", "role=qa&view=table", ""); p.Query.Role != "" || p.Query.View != "" {
		t.Errorf("an empty role= and view= in the URL left role %q and view %q; an empty value in the URL clears what the header says", p.Query.Role, p.Query.View)
	}
	// The body is never read: POST …/label carries a field named label.
	body := httptest.NewRequest("POST", "/products/p-1/board/cards/x/label", strings.NewReader("label="+LabelUnitTest+"&role=qa"))
	body.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if q, _ := parseBoardQuery(body); q != (BoardQuery{}) {
		t.Errorf("a form body was read as the filter %+v; the Board's parameters come from the URL and its header only", q)
	}

	// A value the product does not offer is cleared -- a label of the closed
	// vocabulary that no task has, included -- so no filter is active unseen.
	p := page("/x?role=nobody&story=US-99&label="+LabelE2ERun+"&wf=wf-999&sort=bogus&dir=desc", "", "")
	if p.Query != (BoardQuery{}) || p.Matched != p.All {
		t.Errorf("values the product does not offer left the query %+v and %d of %d cards; all of them must be cleared", p.Query, p.Matched, p.All)
	}
	if p := page("/x?sort=id&dir=desc", "", ""); p.Query.Sort != "" {
		t.Errorf("a sort on the board view was kept (%q); sorting is the table's", p.Query.Sort)
	}
	// The facets are the product's, before filtering.
	f := page("/x?role=qa", "", "").Facets
	if !reflect.DeepEqual(f.Roles, []string{"backend", "frontend", "qa"}) && len(f.Roles) < 3 {
		t.Errorf("the role facet under a role filter is %v; facets are taken before filtering", f.Roles)
	}
	if len(f.Stories) < 3 || f.Stories[0].ID != PlanLane {
		t.Errorf("the story facet is %v; want the plan first, then the product's stories", f.Stories)
	}
}

func TestDoneIsNewestFirst(t *testing.T) {
	// Its own store and server, so it runs beside the others: under the race
	// detector a rendered board is slow, and the suite has ten minutes.
	t.Parallel()
	_, st := bigBoard(t, 200)
	ctx := context.Background()
	// The oldest Done card is moved again: it is now the last to have changed.
	st.mu.Lock()
	st.emit(TaskMoved, "p-1", "moved", KindInfo, EventPayload{Task: "bt-00001", To: StatusDone})
	st.mu.Unlock()
	p, _ := st.BoardPage(ctx, "p-1", BoardQuery{}, BoardWindow{Shown: map[string]int{ColDone: 300}})
	done := colOf(p, ColDone).Cards
	if len(done) < 30 || done[0].ID != "bt-00001" {
		t.Fatalf("Done opens on %q; want bt-00001, the card that changed last", done[0].ID)
	}
	for i := 1; i < len(done); i++ {
		if done[i-1].Updated < done[i].Updated {
			t.Fatalf("Done is not newest first: %s (last change %d) is above %s (%d)", done[i-1].ID, done[i-1].Updated, done[i].ID, done[i].Updated)
		}
	}
	// A label is not a state change: it does not lift a card.
	before := done[5].ID
	st.mu.Lock()
	st.emit(TaskLabeled, "p-1", "labeled", KindInfo, EventPayload{Task: done[20].ID})
	st.mu.Unlock()
	p2, _ := st.BoardPage(ctx, "p-1", BoardQuery{}, BoardWindow{Shown: map[string]int{ColDone: 300}})
	if got := colOf(p2, ColDone).Cards[5].ID; got != before {
		t.Errorf("labelling a card reordered Done (%s moved to where %s was); only a state change does", got, before)
	}
	// Every other column is in rank order.
	for _, c := range p.Columns {
		if c.ID == ColDone || c.ID == ColCanceled {
			continue
		}
		for i := 1; i < len(c.Cards); i++ {
			if c.Cards[i-1].Rank > c.Cards[i].Rank {
				t.Errorf("column %s is not in rank order: %s (rank %d) is above %s (rank %d)", c.ID, c.Cards[i-1].ID, c.Cards[i-1].Rank, c.Cards[i].ID, c.Cards[i].Rank)
			}
		}
	}
}

func TestBoardPageDecoratesOnlyThePage(t *testing.T) {
	// Its own store and server, so it runs beside the others: under the race
	// detector a rendered board is slow, and the suite has ten minutes.
	t.Parallel()
	for _, n := range []int{30, 3000} {
		_, st := bigBoard(t, n)
		ctx := context.Background()
		st.timingCalls.Store(0)
		p, err := st.BoardPage(ctx, "p-1", BoardQuery{}, BoardWindow{})
		if err != nil {
			t.Fatal(err)
		}
		returned := 0
		for _, c := range p.Columns {
			returned += len(c.Cards)
			for _, card := range c.Cards {
				if card.Timing == nil {
					t.Fatalf("with %d tasks, card %s was returned without its timing", n, card.ID)
				}
			}
		}
		if got := int(st.timingCalls.Load()); got != returned {
			t.Errorf("with %d tasks the page returned %d card(s) and computed %d timing(s); it must compute one per card returned and none for the rest",
				n, returned, got)
		}
		st.timingCalls.Store(0)
		tp, _ := st.BoardPage(ctx, "p-1", BoardQuery{View: "table"}, BoardWindow{})
		if got := int(st.timingCalls.Load()); got != len(tp.Rows) || len(tp.Rows) > tableStep {
			t.Errorf("with %d tasks the table returned %d row(s) and computed %d timing(s); want at most %d rows and one timing each", n, len(tp.Rows), got, tableStep)
		}
	}
}

func TestBoardCardIsTheSameCardTheBoardHolds(t *testing.T) {
	// Its own store and server, so it runs beside the others: under the race
	// detector a rendered board is slow, and the suite has ten minutes.
	t.Parallel()
	_, st := bigBoard(t, 120)
	ctx := context.Background()
	full, _ := st.Board(ctx, "p-1")
	checked := 0
	for _, want := range full.Cards {
		if checked >= 40 {
			break
		}
		checked++
		got, err := st.BoardCard(ctx, "p-1", want.ID)
		if err != nil {
			t.Fatalf("BoardCard(%s): %v", want.ID, err)
		}
		// The three fields only the paged reads set; and the timing's clock.
		got.Story, got.Workflow, got.Updated, got.UpdatedAt = "", "", 0, 0
		if got.Timing != nil && want.Timing != nil {
			got.Timing.AsOf = want.Timing.AsOf
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("BoardCard(%s) is not the card Board holds:\n got  %+v\n want %+v", want.ID, got, want)
		}
	}
	if _, err := st.BoardCard(ctx, "p-1", "no-such-card"); err != ErrNotFound {
		t.Errorf("BoardCard of an unknown card: %v, want ErrNotFound", err)
	}
	if _, err := st.BoardCard(ctx, "p-2", full.Cards[0].ID); err != ErrNotFound {
		t.Errorf("BoardCard of another product's card: %v, want ErrNotFound", err)
	}
}

func TestTheBoardQueryRoundTrips(t *testing.T) {
	// Its own store and server, so it runs beside the others: under the race
	// detector a rendered board is slow, and the suite has ten minutes.
	t.Parallel()
	for _, q := range []BoardQuery{
		{},
		{View: "table"},
		{Role: "qa", Story: "US-01", Label: LabelUnitTest, Workflow: "wf-001", Waiting: true, HideEmpty: true},
		{View: "table", Sort: "updated", Dir: "asc"},
		{View: "table", Sort: "id"},
		{Role: "a b&c=d", Story: "plan"},
	} {
		enc := q.Encode()
		got, w := parseBoardQuery(boardReq("/x?"+enc, "", ""))
		if got != q {
			t.Errorf("parsing %q gives %+v, want %+v", enc, got, q)
		}
		if len(w.Shown) != 0 || w.Rows != 0 || w.Text != "" {
			t.Errorf("parsing %q produced a window %+v; the view carries none", enc, w)
		}
	}
	if enc := (BoardQuery{}).Encode(); enc != "" {
		t.Errorf("the default view encodes to %q, want an empty string", enc)
	}
	// The default sort has one spelling, so one view has one address.
	if q, _ := parseBoardQuery(boardReq("/x?view=table&sort=status&dir=desc", "", "")); q != (BoardQuery{View: "table"}) {
		t.Errorf("sort=status parsed to %+v; it is the default and is written as nothing", q)
	}
	// The window never reaches an address.
	q, _ := parseBoardQuery(boardReq("/x?role=qa&shown=done:90&rows=300&rev=done:0123456789abcdef&more=done&surface=sisflow&q=secret", "", "secret"))
	enc := q.Encode()
	for _, key := range []string{"shown", "rows", "rev", "more", "surface", "q=", "secret"} {
		if strings.Contains(enc, key) {
			t.Errorf("the encoded view %q carries %q; the window and the search text are never written into an address", enc, key)
		}
	}
	if len(strings.Repeat("x", maxHrefValue+1)) > maxHrefValue {
		if q, _ := parseBoardQuery(boardReq("/x?role="+strings.Repeat("x", maxHrefValue+1), "", "")); q.Role != "" {
			t.Errorf("a role of %d bytes was kept; a value longer than %d is dropped", maxHrefValue+1, maxHrefValue)
		}
	}
}

// ---------- the Board over HTTP ----------

// boardHX is a request as the Board's script makes one.
func boardHX(t *testing.T, s *server, method, path string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, nil)
	r.Header.Set("HX-Request", "true")
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	for _, c := range boCookies() {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	handlerOf(s).ServeHTTP(w, r)
	return w
}

// handlerOf is a server's handler, built once per server: these tests make
// many requests each, and routes() registers well over a hundred patterns.
var handlers sync.Map

func handlerOf(s *server) http.Handler {
	if h, ok := handlers.Load(s); ok {
		return h.(http.Handler)
	}
	h := s.routes()
	handlers.Store(s, h)
	return h
}

var colTag = regexp.MustCompile(`<div class="board-col" id="col-([a-z-]+)" data-col="[a-z-]+" data-shown="(\d+)" data-rev="([0-9a-f]{16})"([^>]*)>`)

// columnsIn is the columns a response carries: id -> [shown, rev, the rest of the tag].
func columnsIn(body string) map[string][3]string {
	out := map[string][3]string{}
	for _, m := range colTag.FindAllStringSubmatch(body, -1) {
		out[m[1]] = [3]string{m[2], m[3], m[4]}
	}
	return out
}

// stateOf is the X-Board-State a browser showing these columns would send.
func stateOf(cols map[string][3]string, extra string) string {
	var shown, rev []string
	for _, c := range boardColumns {
		if v, ok := cols[c.ID]; ok {
			shown = append(shown, c.ID+":"+v[0])
			rev = append(rev, c.ID+":"+v[1])
		}
	}
	v := url.Values{"shown": {strings.Join(shown, ",")}, "rev": {strings.Join(rev, ",")}}
	s := v.Encode()
	if extra != "" {
		s += "&" + extra
	}
	return s
}

func TestTheBoardTabRendersNoMoreThanItsLimit(t *testing.T) {
	// Its own store and server, so it runs beside the others: under the race
	// detector a rendered board is slow, and the suite has ten minutes.
	t.Parallel()
	srv, _ := bigBoard(t, 500)
	body := boardHX(t, srv, "GET", "/products/p-1?tab=board", nil).Body.String()
	cols := columnsIn(body)
	if len(cols) != len(boardColumns) {
		t.Fatalf("the Board tab has %d columns, want all %d", len(cols), len(boardColumns))
	}
	if n := strings.Count(body, `class="board-card`) - strings.Count(body, `class="board-card-`); n > len(boardColumns)*boardStep {
		t.Errorf("the Board tab of a 500-card product renders %d cards; it must render at most %d", n, len(boardColumns)*boardStep)
	}
	done := body[strings.Index(body, `id="col-done"`):]
	done = done[:strings.Index(done, `id="col-canceled"`)]
	if cols[ColDone][0] != "30" || !strings.Contains(done, `<span class="board-col-n">30 of 450</span>`) || !strings.Contains(done, `board/live?more=done`) {
		t.Errorf("Done shows %s card(s); want 30, the header \"30 of 450\" and a \"Show more\" that asks for more=done", cols[ColDone][0])
	}
	if strings.Contains(body, `<aside class="inject">`) || !strings.Contains(body, `class="panel-grid is-wide"`) {
		t.Errorf("the Board tab still renders the side panel, or is not the wide layout")
	}
	if other := boardHX(t, srv, "GET", "/products/p-1?tab=stories", nil).Body.String(); !strings.Contains(other, `<aside class="inject">`) {
		t.Errorf("the Stories tab lost the side panel; only the Board gives it up")
	}
	// A product with no task still has its nine columns for a first card to
	// land in, and says there is nothing yet.
	empty := boardHX(t, srv, "GET", "/products/p-5?tab=board", nil).Body.String()
	if len(columnsIn(empty)) != len(boardColumns) || !strings.Contains(empty, "No tasks yet.") || strings.Contains(empty, `id="board-host"`) {
		t.Errorf("a product with no task renders %d column(s); want all nine, the \"No tasks yet.\" line and no client-board host", len(columnsIn(empty)))
	}
}

func TestShowMoreDeepensOneColumn(t *testing.T) {
	// Its own store and server, so it runs beside the others: under the race
	// detector a rendered board is slow, and the suite has ten minutes.
	t.Parallel()
	srv, _ := bigBoard(t, 500)
	first := columnsIn(boardHX(t, srv, "GET", "/products/p-1?tab=board", nil).Body.String())
	w := boardHX(t, srv, "GET", "/products/p-1/board/live?more=done", map[string]string{boardStateHeader: stateOf(first, "")})
	got := columnsIn(w.Body.String())
	if len(got) != 1 || got[ColDone][0] != "60" || !strings.Contains(got[ColDone][2], `hx-swap-oob="true"`) {
		t.Fatalf("more=done answered with %d column(s), Done at %s; want Done alone, 60 cards, out of band", len(got), got[ColDone][0])
	}
	if !strings.Contains(w.Body.String(), `<span class="board-col-n">60 of 450</span>`) {
		t.Errorf("the deepened column does not say \"60 of 450\"")
	}
	// At the cap the button is gone and the column says why.
	at := map[string][3]string{}
	for k, v := range first {
		at[k] = v
	}
	at[ColDone] = [3]string{"300", first[ColDone][1], ""}
	capped := boardHX(t, srv, "GET", "/products/p-1/board/live?more=done", map[string]string{boardStateHeader: stateOf(at, "")}).Body.String()
	if c := columnsIn(capped); c[ColDone][0] != "300" || strings.Contains(capped, "more=done") || !strings.Contains(capped, "Showing the first 300 of 450. Filter, or use the table.") {
		t.Errorf("at the cap Done shows %s card(s); want 300, no \"Show more\", and the line that says it is the first 300 of 450", c[ColDone][0])
	}
}

func TestBoardLiveSendsOnlyTheColumnsThatChanged(t *testing.T) {
	// Its own store and server, so it runs beside the others: under the race
	// detector a rendered board is slow, and the suite has ten minutes.
	t.Parallel()
	srv, st := bigBoard(t, 400)
	first := columnsIn(boardHX(t, srv, "GET", "/products/p-1?tab=board", nil).Body.String())
	live := func(state string) (map[string][3]string, string) {
		t.Helper()
		w := boardHX(t, srv, "GET", "/products/p-1/board/live", map[string]string{boardStateHeader: state})
		if w.Code != 200 {
			t.Fatalf("board/live answered %d", w.Code)
		}
		return columnsIn(w.Body.String()), w.Body.String()
	}
	if got, _ := live(stateOf(first, "")); len(got) != 0 {
		t.Fatalf("with what the browser already shows, board/live sent %d column(s); it must send none", len(got))
	}
	// A running card's lead time and its seconds change with the clock. They
	// are the browser's to tick: none of them is in a column's hash, so no
	// column is re-sent because a minute has passed. A finished card's final
	// lead time is in it.
	page, _ := st.BoardPage(context.Background(), "p-1", BoardQuery{}, BoardWindow{})
	working := colOf(page, ColWorking)
	if len(working.Cards) == 0 || working.Cards[0].Timing == nil || working.Cards[0].Timing.Finished {
		t.Fatalf("the fixture has no running card with a timing in Working")
	}
	before := columnRev(working, false)
	later := *working.Cards[0].Timing
	later.AsOf += 90
	later.LeadSeconds += 90
	later.AISeconds += 90
	later.IdleSeconds += 90
	later.QueuedSeconds += 90
	later.LeadText, later.TitleText = "2m", "lead 2m"
	working.Cards[0].Timing = &later
	if columnRev(working, false) != before {
		t.Errorf("a running card's clock (AsOf, the seconds, the lead-time text) changed its column's hash: every column with an open card would be re-sent on every hint")
	}
	done := colOf(page, ColDone)
	doneBefore := columnRev(done, false)
	fin := *done.Cards[0].Timing
	fin.Finished, fin.LeadText = true, "3d"
	done.Cards[0].Timing = &fin
	if columnRev(done, false) == doneBefore {
		t.Errorf("a finished card's lead time is not in its column's hash")
	}
	if columnRev(done, true) == columnRev(done, false) {
		t.Errorf("hiding a column does not change its hash")
	}
	// Without a rev the browser has nothing: all nine.
	if got, _ := live(""); len(got) != len(boardColumns) {
		t.Errorf("without a rev, board/live sent %d column(s), want all %d", len(got), len(boardColumns))
	}
	// One move: the column the card left and the one it reached, and the
	// Timeline tab's number.
	if _, err := st.MoveCard(context.Background(), "p-1", "bt-00000", CardMove{Column: ColCanceled, Index: -1}); err != nil {
		t.Fatalf("move: %v", err)
	}
	got, body := live(stateOf(first, ""))
	if len(got) != 2 || got[ColBacklog][0] == "" || got[ColCanceled][0] == "" {
		t.Errorf("after one move board/live sent %d column(s) (%v); want exactly Backlog and Canceled", len(got), got)
	}
	if !strings.Contains(body, `id="navTimelineN" hx-swap-oob="true"`) || !strings.Contains(body, `id="boardCount" hx-swap-oob="true"`) {
		t.Errorf("the live response lacks the Timeline count or the count line, out of band")
	}
	// "Hide empty columns" changes a column that holds no card: it is sent,
	// hidden, and sent again when it is shown.
	narrow := "role=qa&story=US-02&wf=wf-000"
	shownAll, _ := live(narrow)
	hidden, _ := live(stateOf(shownAll, narrow+"&empty=hide"))
	n := 0
	for id, c := range hidden {
		if !strings.Contains(c[2], " hidden") || c[0] != "0" {
			t.Errorf("turning \"Hide empty columns\" on sent %s, which is not an empty, hidden column", id)
		}
		n++
	}
	if n == 0 {
		t.Fatalf("turning \"Hide empty columns\" on sent no column; an empty column's hash must change with it")
	}
	if back, _ := live(stateOf(hidden, narrow)); len(back) == 0 {
		t.Errorf("turning \"Hide empty columns\" off again sent nothing; the hidden columns stay hidden")
	}
	// Every field the card prints is in the hash: a label added to a card
	// re-sends its column.
	base, _ := live("")
	st.mu.Lock()
	for i := range st.tasks {
		if st.tasks[i].ID == "bt-00010" {
			st.tasks[i].Labels = append(st.tasks[i].Labels, LabelE2ERun)
		}
	}
	st.mu.Unlock()
	if got, _ := live(stateOf(base, "")); len(got) != 1 {
		t.Errorf("a label added to one card re-sent %d column(s), want its column alone", len(got))
	}
	// The client board is on screen: the count line, and no column.
	if got, body := live("surface=sisflow"); len(got) != 0 || !strings.Contains(body, `data-board-more="done"`) {
		t.Errorf("with surface=sisflow board/live sent %d column(s); want none, and the count line with its \"Show more\"", len(got))
	}
}

func TestBoardLiveNeverPushesTheAddress(t *testing.T) {
	// Its own store and server, so it runs beside the others: under the race
	// detector a rendered board is slow, and the suite has ten minutes.
	t.Parallel()
	srv, st := bigBoard(t, 200)
	slug := slugOfProduct(t, st, "p-1")
	const host = "https://gaos.example"
	for _, c := range []struct {
		name, path, current string
		hdr                 map[string]string
		replace             string
	}{
		{"a filter on the Board", "/products/p-1/board/live?role=qa", host + "/app/p/" + slug + "/board", nil, "/app/p/" + slug + "/board?role=qa"},
		{"the last filter cleared", "/products/p-1/board/live?role=", host + "/app/p/" + slug + "/board?role=qa", nil, "/app/p/" + slug + "/board"},
		{"the same view", "/products/p-1/board/live?role=qa", host + "/app/p/" + slug + "/board?role=qa", nil, ""},
		{"a refresh, the address behind the filter", "/products/p-1/board/live?role=qa", host + "/app/p/" + slug + "/board", map[string]string{refreshHeader: "1"}, "/app/p/" + slug + "/board?role=qa"},
		{"the operator has left the Board", "/products/p-1/board/live?role=qa", host + "/app/p/" + slug + "/stories", nil, ""},
		{"the operator is on another product", "/products/p-1/board/live?role=qa", host + "/app/p/invoice-engine/board", nil, ""},
		{"a value the product does not offer", "/products/p-1/board/live?role=nobody", host + "/app/p/" + slug + "/board", nil, ""},
	} {
		hdr := map[string]string{"X-Forwarded-Prefix": "/app", "HX-Current-URL": c.current}
		for k, v := range c.hdr {
			hdr[k] = v
		}
		w := boardHX(t, srv, "GET", c.path, hdr)
		if w.Header().Get("HX-Push-Url") != "" || w.Header().Get("HX-Replace-Url") != c.replace {
			t.Errorf("%s: board/live set HX-Push-Url %q and HX-Replace-Url %q; want no push and replace %q",
				c.name, w.Header().Get("HX-Push-Url"), w.Header().Get("HX-Replace-Url"), c.replace)
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: board/live sends Cache-Control %q, want no-store", c.name, w.Header().Get("Cache-Control"))
		}
	}
	// The window and the search text never reach the address.
	w := boardHX(t, srv, "GET", "/products/p-1/board/live?role=qa&more=done&surface=sisflow", map[string]string{
		"HX-Current-URL": host + "/p/" + slug + "/board", boardStateHeader: "shown=done:60&rev=done:0123456789abcdef", boardSearchHeader: "secret"})
	if got := w.Header().Get("HX-Replace-Url"); got != "/p/"+slug+"/board?role=qa" {
		t.Errorf("the address after a request with a window and a search is %q; want only the view, /p/%s/board?role=qa", got, slug)
	}
	if w := boardHX(t, srv, "GET", "/products/nope/board/live", nil); w.Code != http.StatusNotFound {
		t.Errorf("board/live of an unknown product answered %d, want 404", w.Code)
	}
}

func TestABoardMutationKeepsTheView(t *testing.T) {
	// Its own store and server, so it runs beside the others: under the race
	// detector a rendered board is slow, and the suite has ten minutes.
	t.Parallel()
	srv, st := bigBoard(t, 400)
	slug := slugOfProduct(t, st, "p-1")
	at := "https://gaos.example/p/" + slug + "/board?role=backend"
	first := columnsIn(boardHX(t, srv, "GET", "/products/p-1?tab=board&role=backend", nil).Body.String())
	state := stateOf(first, "role=backend")

	// A move: the two columns, under the filter, and not the sheet.
	mv := func(card, col string, hdr map[string]string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("PUT", "/products/p-1/board/cards/"+card+"/move", strings.NewReader("column="+col))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("HX-Request", "true")
		r.Header.Set("HX-Current-URL", at)
		for k, v := range hdr {
			r.Header.Set(k, v)
		}
		for _, c := range boCookies() {
			r.AddCookie(c)
		}
		w := httptest.NewRecorder()
		handlerOf(srv).ServeHTTP(w, r)
		return w
	}
	w := mv("bt-00000", ColCanceled, map[string]string{boardStateHeader: state})
	body := w.Body.String()
	got := columnsIn(body)
	if w.Code != 200 || len(got) != 2 || got[ColBacklog][0] == "" || got[ColCanceled][0] == "" {
		t.Fatalf("a move answered %d with %d column(s) (%v); want 200 and exactly Backlog and Canceled", w.Code, len(got), got)
	}
	if strings.Contains(body, `id="boSheet"`) {
		t.Errorf("a move was answered with the whole sheet; the Board answers a move with its changed columns")
	}
	if strings.Contains(body, "bt-00001") || !strings.Contains(body, `id="toast"`) {
		t.Errorf("the move's answer shows a card of another role, or carries no toast: the filter the browser holds must apply, and the chrome must ride along")
	}
	if w.Header().Get("HX-Push-Url")+w.Header().Get("HX-Replace-Url") != "" {
		t.Errorf("a move set an address header (%q, %q); a mutation never rewrites the address", w.Header().Get("HX-Push-Url"), w.Header().Get("HX-Replace-Url"))
	}
	// In the table, a move answers with the table.
	tw := mv("bt-00030", ColCanceled, map[string]string{boardStateHeader: "view=table&role=backend&rows=100"})
	if tb := tw.Body.String(); !strings.Contains(tb, `id="boardTable" data-rows="`) || strings.Contains(tb, `id="boSheet"`) || len(columnsIn(tb)) != 0 {
		t.Errorf("a move made from the table was not answered with the table")
	}

	// A mutation that still answers with the sheet re-renders the same view:
	// the filter, the depth and the search text. The body's own field named
	// "label" is the label to set, not the filter.
	r := httptest.NewRequest("POST", "/products/p-1/board/cards/bt-00003/label", strings.NewReader("label="+LabelE2ERun))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("HX-Request", "true")
	r.Header.Set("HX-Current-URL", at)
	r.Header.Set(boardStateHeader, "role=backend&shown=done:60")
	r.Header.Set(boardSearchHeader, url.QueryEscape("task 00"))
	for _, c := range boCookies() {
		r.AddCookie(c)
	}
	lw := httptest.NewRecorder()
	handlerOf(srv).ServeHTTP(lw, r)
	sheet := lw.Body.String()
	if lw.Code != 200 || !strings.Contains(sheet, `id="boSheet"`) {
		t.Fatalf("setting a label answered %d without the sheet: %s", lw.Code, clip(sheet, 200))
	}
	if !strings.Contains(sheet, `<option value="backend" selected>`) || strings.Contains(sheet, `<option value="`+LabelE2ERun+`" selected>`) {
		t.Errorf("the sheet after a label does not keep the role filter, or took the body's label for the label filter")
	}
	if !strings.Contains(sheet, `id="boardSearch"`) || !strings.Contains(sheet, `value="task 00"`) {
		t.Errorf("the sheet after a label does not show the search text the board is narrowed by")
	}
	if lw.Header().Get("HX-Replace-Url") != "" || lw.Header().Get("HX-Push-Url") != "" {
		t.Errorf("a label set an address header; a mutation that stays on the Board leaves the address alone")
	}

	// Leaving the table, and clearing the filters: each link names what it
	// sets, empty included, and the URL wins over the state the script sends.
	tbl := boardHX(t, srv, "GET", "/products/p-1?tab=board&view=table", map[string]string{boardStateHeader: "role=backend"}).Body.String()
	boardGet := regexp.MustCompile(`hx-get="(products/p-1\?tab=board&amp;view=)"`).FindStringSubmatch(tbl)
	reset := regexp.MustCompile(`data-board-reset href="[^"]*" hx-get="([^"]+)"`).FindStringSubmatch(tbl)
	if boardGet == nil || reset == nil {
		t.Fatalf("the table view lacks the Board link (view=) or the \"Clear filters\" link")
	}
	stale := map[string]string{boardStateHeader: "view=table&role=backend&rows=100"}
	if b := boardHX(t, srv, "GET", "/"+html.UnescapeString(boardGet[1]), stale).Body.String(); !strings.Contains(b, `class="board-ssr"`) || strings.Contains(b, `id="boardTable"`) {
		t.Errorf("the Board link, sent with a state that says view=table, did not show the board: an empty view= in the URL must win")
	}
	cleared := boardHX(t, srv, "GET", "/"+html.UnescapeString(reset[1]), stale).Body.String()
	if strings.Contains(cleared, `<option value="backend" selected>`) || !strings.Contains(cleared, `id="boardTable"`) {
		t.Errorf("\"Clear filters\" left the role filter on, or left the table: it clears the filters and keeps the view")
	}
}

func TestTheTableSortsAndCaps(t *testing.T) {
	// Its own store and server, so it runs beside the others: under the race
	// detector a rendered board is slow, and the suite has ten minutes.
	t.Parallel()
	srv, _ := bigBoard(t, 1400)
	rowIDs := func(body string) []string {
		var ids []string
		for _, m := range regexp.MustCompile(`<tr class="board-row[^"]*" data-card="([^"]+)"`).FindAllStringSubmatch(body, -1) {
			ids = append(ids, m[1])
		}
		return ids
	}
	get := func(q, state string) string {
		t.Helper()
		return boardHX(t, srv, "GET", "/products/p-1/board/live?view=table"+q, map[string]string{boardStateHeader: state}).Body.String()
	}
	def := get("", "")
	if ids := rowIDs(def); len(ids) != tableStep || !strings.Contains(def, "Showing 100 of ") || !strings.Contains(def, "more=rows") {
		t.Fatalf("the table shows %d row(s); want %d, \"Showing 100 of N\" and a \"Show 100 more\"", len(rowIDs(def)), tableStep)
	}
	if strings.Contains(def, `class="board-ssr"`) || strings.Contains(def, `id="board-host"`) {
		t.Errorf("the table view also renders a board")
	}
	byID := rowIDs(get("&sort=id", ""))
	if !sort.StringsAreSorted(byID) || !strings.Contains(get("&sort=id", ""), `aria-sort="ascending"`) {
		t.Errorf("sort=id is not ascending by id, or its header does not say so")
	}
	desc := rowIDs(get("&sort=id&dir=desc", ""))
	if len(desc) == 0 || desc[0] <= desc[len(desc)-1] {
		t.Errorf("sort=id&dir=desc is not descending")
	}
	if more := rowIDs(get("&more=rows", "rows=100")); len(more) != 2*tableStep {
		t.Errorf("more=rows at 100 shows %d row(s), want %d", len(more), 2*tableStep)
	}
	capped := get("&more=rows", "rows=1000")
	if ids := rowIDs(capped); len(ids) != tableMaxRows || strings.Contains(capped, "more=rows") || !strings.Contains(capped, "the first 1,000. Filter to see the rest.") {
		t.Errorf("at the cap the table shows %d row(s); want %d, no \"Show 100 more\", and the line that says so", len(rowIDs(capped)), tableMaxRows)
	}
	// The header links carry the page address and the fragment address.
	if !regexp.MustCompile(`<a href="p/laundry-membership/board\?[^"]*sort=id[^"]*" hx-get="products/p-1\?tab=board&amp;view=table&amp;sort=id&amp;dir=asc"`).MatchString(def) {
		t.Errorf("the ID header is not a link with a page address in href and its own sort in hx-get")
	}
}

func TestTheSearchTextNeverTravelsInAURL(t *testing.T) {
	// Its own store and server, so it runs beside the others: under the race
	// detector a rendered board is slow, and the suite has ten minutes.
	t.Parallel()
	srv, _ := bigBoard(t, 200)
	body := boardHX(t, srv, "GET", "/products/p-1?tab=board", nil).Body.String()
	form := body[strings.Index(body, `id="boardFilters"`):]
	form = form[:strings.Index(form, "</form>")]
	if strings.Contains(form, `name="q"`) {
		t.Fatalf("the filter form has a control named q; htmx and the browser would both write it into a URL")
	}
	box := regexp.MustCompile(`<input type="search" id="boardSearch"[^>]*>`).FindString(form)
	if box == "" || strings.Contains(box, "name=") {
		t.Errorf("the search box is %q; it must be in the form and have no name, so that nothing can serialise it", box)
	}
	if !strings.Contains(form, `onsubmit="return false"`) {
		t.Errorf("the filter form can be submitted natively; Enter in the search box must not navigate")
	}
	// A q= in a URL is not read by any Board route.
	all := columnsIn(boardHX(t, srv, "GET", "/products/p-1/board/live", nil).Body.String())
	for _, path := range []string{"/products/p-1/board/live?q=bt-00007", "/products/p-1?tab=board&q=bt-00007"} {
		got := columnsIn(boardHX(t, srv, "GET", path, nil).Body.String())
		for id, c := range all {
			if got[id][0] != c[0] || got[id][1] != c[1] {
				t.Errorf("GET %s narrowed the board (column %s); the search text is read from its header only", path, id)
			}
		}
	}
	narrowed := boardHX(t, srv, "GET", "/products/p-1/board/live", map[string]string{boardSearchHeader: "bt-00007"}).Body.String()
	if !strings.Contains(narrowed, "1 of ") {
		t.Errorf("the search header did not narrow the board to one card")
	}
	// The script sends it in the header and builds no URL from it.
	for _, f := range []string{"static/live.js", "static/board.js"} {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if regexp.MustCompile(`[?&]q=|["']q["']`).Match(src) {
			t.Errorf("%s names a q parameter; the search text has no parameter", f)
		}
	}
	if src, _ := os.ReadFile("static/live.js"); !strings.Contains(string(src), `e.detail.headers["X-Board-Search"]`) {
		t.Errorf("static/live.js does not set the X-Board-Search header")
	}
}

func TestTheBoardJSONPagesOnlyWhenAsked(t *testing.T) {
	// Its own store and server, so it runs beside the others: under the race
	// detector a rendered board is slow, and the suite has ten minutes.
	t.Parallel()
	srv, st := bigBoard(t, 400)
	get := func(path string, hdr map[string]string) (int, string, http.Header) {
		t.Helper()
		r := httptest.NewRequest("GET", path, nil)
		for k, v := range hdr {
			r.Header.Set(k, v)
		}
		for _, c := range boCookies() {
			r.AddCookie(c)
		}
		w := httptest.NewRecorder()
		handlerOf(srv).ServeHTTP(w, r)
		return w.Code, w.Body.String(), w.Header()
	}
	// Without paged=1: the document it has always been, whatever else is asked.
	full, _ := st.Board(context.Background(), "p-1")
	_, plain, _ := get("/products/p-1/board", nil)
	_, filtered, _ := get("/products/p-1/board?role=qa&shown=done:5", map[string]string{boardStateHeader: "role=qa", boardSearchHeader: "bt-00007"})
	var a, b Board
	if err := json.Unmarshal([]byte(plain), &a); err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal([]byte(filtered), &b)
	if len(a.Cards) != len(full.Cards) || len(b.Cards) != len(full.Cards) || len(a.Rows) < 2 || a.Cards[0].Row == "" {
		t.Errorf("the unpaged document has %d card(s) (%d under a filter) and %d lane(s); want all %d both times, with their lanes", len(a.Cards), len(b.Cards), len(a.Rows), len(full.Cards))
	}
	if strings.Contains(plain, `"counts"`) || strings.Contains(plain, `"story"`) || strings.Contains(plain, `"updated"`) {
		t.Errorf("the unpaged document gained a field (counts, story or updated); it must not change")
	}

	code, body, hdr := get("/products/p-1/board?paged=1", nil)
	var doc pagedBoard
	if err := json.Unmarshal([]byte(body), &doc); err != nil || code != 200 {
		t.Fatalf("paged=1 answered %d: %v", code, err)
	}
	perCol := map[string]int{}
	for _, c := range doc.Cards {
		perCol[c.Column]++
		if c.Row != "" {
			t.Fatalf("a card of the paged document has the lane %q; it has no lanes", c.Row)
		}
	}
	if perCol[ColDone] != boardStep || doc.Counts[ColDone] != 360 || doc.Matched != doc.All || doc.All != len(full.Cards) || len(doc.Columns) != len(boardColumns) {
		t.Errorf("the paged document has %d Done card(s), counts.done %d, matched %d, all %d; want %d, 360, and both totals %d", perCol[ColDone], doc.Counts[ColDone], doc.Matched, doc.All, boardStep, len(full.Cards))
	}
	if !strings.Contains(body, `"rows":[]`) || !strings.Contains(body, `"workflows":[]`) || strings.Contains(body, `"row":`) {
		t.Errorf("the paged document must carry rows: [], workflows: [] and no row on any card")
	}
	if hdr.Get("Cache-Control") != "no-store" {
		t.Errorf("the paged document sends Cache-Control %q, want no-store", hdr.Get("Cache-Control"))
	}
	// The view and the window: from the header, or from the URL.
	for name, c := range map[string]struct {
		path string
		hdr  map[string]string
	}{
		"the header": {"/products/p-1/board?paged=1", map[string]string{boardStateHeader: "role=qa&shown=done:60"}},
		"the URL":    {"/products/p-1/board?paged=1&role=qa&shown=done:60", nil},
	} {
		_, body, _ := get(c.path, c.hdr)
		var d pagedBoard
		_ = json.Unmarshal([]byte(body), &d)
		n := 0
		for _, card := range d.Cards {
			if card.Role != "qa" {
				t.Fatalf("with role=qa in %s the paged document holds a %s card", name, card.Role)
			}
			if card.Column == ColDone {
				n++
			}
		}
		if n != 60 || d.Matched >= d.All {
			t.Errorf("with role=qa and shown=done:60 in %s: %d Done card(s), matched %d of %d; want 60 and a real narrowing", name, n, d.Matched, d.All)
		}
	}
	_, searched, _ := get("/products/p-1/board?paged=1", map[string]string{boardSearchHeader: "bt-00007"})
	var sd pagedBoard
	_ = json.Unmarshal([]byte(searched), &sd)
	if sd.Matched != 1 {
		t.Errorf("the search header matched %d card(s) in the paged document, want 1", sd.Matched)
	}
	if code, _, _ := get("/products/nope/board?paged=1", nil); code != http.StatusNotFound {
		t.Errorf("the paged document of an unknown product answered %d, want 404", code)
	}
}

// evCountingStore counts the reads of the whole event stream.
type evCountingStore struct {
	Store
	events int
}

func (c *evCountingStore) Events(ctx context.Context) ([]Event, error) {
	c.events++
	return c.Store.Events(ctx)
}

func TestTheBoardTabSkipsTheEventCopyAndTheDiagrams(t *testing.T) {
	// Its own store and server, so it runs beside the others: under the race
	// detector a rendered board is slow, and the suite has ten minutes.
	t.Parallel()
	srv, st := bigBoard(t, 100)
	counter := &evCountingStore{Store: st}
	srv.store = counter
	build := func(tab string) *ProductDetail {
		t.Helper()
		r := httptest.NewRequest("GET", "/products/p-1?tab="+tab, nil)
		bo := srv.buildBO(r, "products", "p-1", tab)
		if bo.Detail == nil {
			t.Fatalf("no detail for tab %s", tab)
		}
		return bo.Detail
	}
	for _, tab := range []string{"board", "brd", "stories", "qa"} {
		counter.events = 0
		d := build(tab)
		if counter.events != 0 || d.Timeline != nil {
			t.Errorf("the %s tab read the whole event stream %d time(s) and holds %d timeline event(s); only the Timeline tab loads it", tab, counter.events, len(d.Timeline))
		}
		if d.TimelineN < 600 {
			t.Errorf("the %s tab's Timeline count is %d; the number is shown on every tab", tab, d.TimelineN)
		}
		for _, g := range d.Diagrams {
			if g.SVG != "" {
				t.Errorf("the %s tab drew the sequence diagram %s; it is drawn for the Diagrams tab only", tab, g.ID)
			}
		}
	}
	counter.events = 0
	if d := build("timeline"); counter.events != 1 || len(d.Timeline) != d.TimelineN {
		t.Errorf("the Timeline tab read the stream %d time(s) and holds %d of %d event(s); want one read and all of them", counter.events, len(d.Timeline), d.TimelineN)
	}
	drawn := 0
	for _, g := range build("diagrams").Diagrams {
		if g.SVG != "" {
			drawn++
		}
	}
	if drawn == 0 {
		t.Errorf("the Diagrams tab drew no sequence diagram")
	}
	// The label still shows the number on the Board tab.
	body := boardHX(t, srv, "GET", "/products/p-1?tab=board", nil).Body.String()
	if !regexp.MustCompile(`Timeline <span class="n" id="navTimelineN">[\d,]+</span>`).MatchString(body) {
		t.Errorf("the Board tab's menu does not carry the Timeline count")
	}
}
