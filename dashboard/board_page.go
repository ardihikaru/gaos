package main

// The Board, a page at a time (D-095, docs/designs/urls-and-board.md §4, §5).
//
// Board() returns every card of a product and decorates every one. That was
// right while a board held a workflow's worth of tasks; Done never empties, and
// at 2,000 cards one refresh cost 109 ms and 1.8 MB. BoardPage is the same
// projection cut to what a screen shows: the cards that match a filter, thirty
// to a column until the operator asks for more (or ten table rows a page),
// decorated only once they have been chosen.
//
// Two types carry a request's wishes, and the split is the point:
//
//   - BoardQuery is the VIEW -- the filter, board or table, the sort, and the
//     table's page (D-097). It lives in the address, so a link reproduces it.
//   - BoardWindow is HOW MUCH of the board view -- the depth of each column, the
//     search text. It never reaches an address.

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	boardStep      = 30   // cards a column shows at first, and more per "Show more"
	boardMaxShown  = 300  // the most cards one column ever holds
	boardMaxSearch = 80   // runes of search text
	boardMaxPairs  = 256  // bytes of a shown= or rev= value
	boardMaxState  = 1024 // bytes of the X-Board-State header
)

// The two request headers the Board's own script sets (static/live.js). The
// state is the form's fields and the window, URL-encoded; the search text is on
// its own and never in a URL, because the edge logs request lines.
const (
	boardStateHeader  = "X-Board-State"
	boardSearchHeader = "X-Board-Search"
)

// BoardQuery is the view. Every field is comparable and Encode writes all of
// it, so parsing what Encode wrote gives the same value back.
type BoardQuery struct {
	View      string // "" (the board) | "table"
	Role      string
	Story     string // "plan", or a story id
	Label     string
	Severity  string // a level, or "medium+" (D-102)
	Workflow  string
	Waiting   bool   // only cards waiting on a person
	HideEmpty bool   // hide columns with no matching card
	Sort, Dir string // table only; "" = the default
	Page      int    // table only; 0 = the first page (D-097)
}

// BoardWindow is how much of the view one request wants. Never encoded.
type BoardWindow struct {
	Text  string         // the search box
	Shown map[string]int // board: cards per column; a missing column gets boardStep
}

// BoardFacets is what each select offers, before filtering, sorted.
type BoardFacets struct {
	Roles, Labels, Workflows []string   // the values present on any task of the product
	Severities               []string   // the severity levels present, in ladder order (D-102)
	Stories                  []BoardRow // "plan" first, then every story of the product
}

// BoardPageColumn is one column of a page.
type BoardPageColumn struct {
	BoardColumn
	Matched int // cards of this column that match the filter
	Shown   int // cards returned
	Cards   []BoardCard
}

// BoardPage is one page of a product's board.
type BoardPage struct {
	ProductID string
	Query     BoardQuery        // the query, with every value the product does not offer cleared
	Text      string            // the search text applied
	Columns   []BoardPageColumn // always all nine, in board order
	Rows      []BoardCard       // table view only
	RowsShown int               // table view: len(Rows), the rows of this page
	Matched   int               // cards matching the filter
	All       int               // all cards of the product
	Facets    BoardFacets
}

var boardSorts = map[string]bool{"id": true, "role": true, "story": true, "updated": true, "severity": true}

// severityMediumPlus is the filter value for medium, high and critical (D-102).
const severityMediumPlus = "medium+"

// severityMatches is the Severity filter.
func severityMatches(want string, labels []string) bool {
	got := severityOf(labels)
	if want == severityMediumPlus {
		return prSeverityRank(got) >= prSeverityRank("medium")
	}
	return got == want
}

func isColumnID(id string) bool {
	for _, c := range boardColumns {
		if c.ID == id {
			return true
		}
	}
	return false
}

// ---------- the query: parse and encode ----------

// parsePairs reads "column:value,column:value" -- shown= and rev=. A value over
// boardMaxPairs bytes is ignored whole; a key that is not a column id is dropped.
func parsePairs(v string) map[string]string {
	if v == "" || len(v) > boardMaxPairs {
		return nil
	}
	out := map[string]string{}
	parts := strings.SplitN(v, ",", len(boardColumns)+1)
	if len(parts) > len(boardColumns) {
		parts = parts[:len(boardColumns)]
	}
	for _, p := range parts {
		k, val, ok := strings.Cut(p, ":")
		if ok && isColumnID(k) {
			out[k] = val
		}
	}
	return out
}

// boardParams is where a request's Board parameters come from: the URL, and for
// a key the URL does not carry at all, the X-Board-State header. A key present
// in the URL wins even when empty -- that is how a link clears a filter. The
// request's BODY is never read: POST …/label has a body field named "label",
// and it is not the filter.
type boardParams struct {
	url, state url.Values
}

func boardParamsOf(r *http.Request) boardParams {
	p := boardParams{url: r.URL.Query()}
	if h := r.Header.Get(boardStateHeader); h != "" && len(h) <= boardMaxState {
		p.state, _ = url.ParseQuery(h)
	}
	return p
}

func (p boardParams) get(key string) string {
	if _, ok := p.url[key]; ok {
		return p.url.Get(key)
	}
	return p.state.Get(key)
}

// value is a BoardQuery value: dropped when it is longer than an address may carry.
func (p boardParams) value(key string) string {
	v := p.get(key)
	if len(v) > maxHrefValue {
		return ""
	}
	return v
}

// parseBoardQuery reads the view and the window from a request. It is
// syntactic: it knows no product, so a role nobody has is cleared later, by
// BoardPage, against the product's own facets.
func parseBoardQuery(r *http.Request) (BoardQuery, BoardWindow) {
	p := boardParamsOf(r)
	q := BoardQuery{
		Role: p.value("role"), Story: p.value("story"), Label: p.value("label"), Severity: p.value("severity"), Workflow: p.value("wf"),
		Waiting: p.get("waiting") == "1", HideEmpty: p.get("empty") == "hide",
	}
	if p.get("view") == "table" {
		q.View = "table"
		q.Page = pageParam(p.get("page"))
	}
	if s := p.get("sort"); boardSorts[s] {
		q.Sort = s
		if d := p.get("dir"); d == "asc" || d == "desc" {
			q.Dir = d
		}
	}

	w := BoardWindow{Shown: map[string]int{}}
	for col, v := range parsePairs(p.get("shown")) {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			w.Shown[col] = min(n, boardMaxShown)
		}
	}
	// "more" deepens one column, and the result is clamped again.
	if more := p.get("more"); isColumnID(more) {
		w.Shown[more] = min(max(w.Shown[more], boardStep)+boardStep, boardMaxShown)
	}
	// The search text arrives in a header and nowhere else. A q= in the URL is
	// not read: if one ever got there, it would also be in the access log.
	if h := r.Header.Get(boardSearchHeader); h != "" {
		if t, err := url.QueryUnescape(h); err == nil {
			w.Text = clipRunes(strings.TrimSpace(t), boardMaxSearch)
		}
	}
	return q, w
}

func clipRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// Encode writes the view as a query string, defaults omitted; "" for the
// default view. The window is never in it.
func (q BoardQuery) Encode() string {
	v := url.Values{}
	if q.View == "table" {
		v.Set("view", "table")
	}
	if q.Role != "" {
		v.Set("role", q.Role)
	}
	if q.Story != "" {
		v.Set("story", q.Story)
	}
	if q.Label != "" {
		v.Set("label", q.Label)
	}
	if q.Severity != "" {
		v.Set("severity", q.Severity)
	}
	if q.Workflow != "" {
		v.Set("wf", q.Workflow)
	}
	if q.Waiting {
		v.Set("waiting", "1")
	}
	if q.HideEmpty {
		v.Set("empty", "hide")
	}
	if q.Sort != "" {
		v.Set("sort", q.Sort)
		if q.Dir != "" {
			v.Set("dir", q.Dir)
		}
	}
	if q.View == "table" && q.Page > 1 {
		v.Set("page", strconv.Itoa(q.Page))
	}
	return v.Encode()
}

// revOf reads the column hashes the browser sent: what it already shows.
func revOf(r *http.Request) map[string]string {
	out := map[string]string{}
	for col, v := range parsePairs(boardParamsOf(r).get("rev")) {
		if len(v) == 16 && strings.Trim(v, "0123456789abcdef") == "" {
			out[col] = v
		}
	}
	return out
}

// ---------- the read model ----------

// productTasksLocked is one product's tasks by id. Caller holds s.mu.
func (s *MemStore) productTasksLocked(productID string) map[string]*TaskRecord {
	byID := map[string]*TaskRecord{}
	for i := range s.tasks {
		if s.tasks[i].DeskID == productID {
			byID[s.tasks[i].ID] = &s.tasks[i]
		}
	}
	return byID
}

// lastChange is a task's last state change: the event's Seq, which orders, and
// its time, which the table shows.
type lastChange struct {
	Seq int
	At  int64
}

// lastChangesLocked is, per task, its last state change: the last event the
// timing index holds for it -- its planning, a move, a question or an answer.
// A label change is not one. Read straight off the index, without the copy
// taskEventsLocked makes. Caller holds s.mu.
func (s *MemStore) lastChangesLocked(tasks []*TaskRecord) map[string]lastChange {
	s.timingMu.Lock()
	defer s.timingMu.Unlock()
	s.extendIndexLocked()
	out := make(map[string]lastChange, len(tasks))
	for _, t := range tasks {
		if idx := s.timingIdx[t.ID]; len(idx) > 0 {
			e := &s.events[idx[len(idx)-1]]
			out[t.ID] = lastChange{Seq: e.Seq, At: e.At}
		}
	}
	return out
}

// lastChangeOf is a task's last state change, from the map when the task was
// among those it was read for, and from the index otherwise.
func (s *MemStore) lastChangeOf(last map[string]lastChange, t *TaskRecord) lastChange {
	if v, ok := last[t.ID]; ok {
		return v
	}
	return s.lastChangesLocked([]*TaskRecord{t})[t.ID]
}

// waitsOnAPerson is the "Waiting on a person" filter: the two statuses in
// which a task cannot go on until someone acts. It is wider than the "Needs
// you" column, which is AWAITING_INPUT alone, and so it has its own name.
func waitsOnAPerson(t *TaskRecord) bool {
	return t.Status == StatusAwaitingInput || t.Status == StatusReview
}

func (q BoardQuery) matches(t *TaskRecord, text string) bool {
	if q.Role != "" && t.Role != q.Role {
		return false
	}
	if q.Story != "" && laneOf(*t) != q.Story {
		return false
	}
	if q.Workflow != "" && t.WorkflowID != q.Workflow {
		return false
	}
	if q.Waiting && !waitsOnAPerson(t) {
		return false
	}
	if q.Label != "" {
		has := false
		for _, l := range t.Labels {
			has = has || l == q.Label
		}
		if !has {
			return false
		}
	}
	if q.Severity != "" && !severityMatches(q.Severity, t.Labels) {
		return false
	}
	if text != "" {
		if !strings.Contains(strings.ToLower(t.Title), text) && !strings.Contains(strings.ToLower(t.ID), text) &&
			!strings.Contains(strings.ToLower(t.DedupKey), text) {
			return false
		}
	}
	return true
}

// clean clears every field of q whose value the product does not offer, so a
// filter is never active without a control that shows it, and an address is
// never written with a value that means nothing.
func (q BoardQuery) clean(f BoardFacets) BoardQuery {
	in := func(vals []string, v string) bool {
		for _, x := range vals {
			if x == v {
				return true
			}
		}
		return false
	}
	if !in(f.Roles, q.Role) {
		q.Role = ""
	}
	if !in(f.Labels, q.Label) || strings.HasPrefix(q.Label, labelFamilySeverity+":") {
		q.Label = ""
	}
	if q.Severity != severityMediumPlus && !in(f.Severities, q.Severity) {
		q.Severity = ""
	}
	if !in(f.Workflows, q.Workflow) {
		q.Workflow = ""
	}
	story := false
	for _, s := range f.Stories {
		story = story || s.ID == q.Story
	}
	if !story {
		q.Story = ""
	}
	if q.View != "table" {
		q.View, q.Sort, q.Dir, q.Page = "", "", "", 0
	}
	if !boardSorts[q.Sort] {
		q.Sort, q.Dir = "", ""
	}
	return q
}

// cardOf is a task as a card, undecorated.
func cardOf(t *TaskRecord, m *Master, last lastChange) BoardCard {
	return BoardCard{ID: t.ID, Label: t.Title, Column: columnOf(t.Status), Status: t.Status, Role: t.Role,
		Rank: t.Rank, Labels: t.Labels, Severity: severityOf(t.Labels), Chips: withoutSeverity(t.Labels),
		Vendor: m.Vendors.VendorFor(t.Role), Moves: legalMoves(*t),
		Story: t.Story, Workflow: t.WorkflowID, Updated: last.Seq, UpdatedAt: last.At}
}

// decorateCardsLocked puts on a set of cards what Board puts on all of them:
// the QA roll-up, the timing, the conversation and the gate. Caller holds s.mu.
func (s *MemStore) decorateCardsLocked(cards []BoardCard, byID map[string]*TaskRecord, m *Master, productID string) {
	if len(cards) == 0 {
		return
	}
	tmp := Board{Cards: cards}
	s.decorateBoardQa(&tmp, m)
	// A card's events are read once and used twice, by the timing and by the
	// conversation; each read is a copy.
	memo := make(map[string][]Event, len(cards))
	eventsOf := func(id string) []Event {
		if e, ok := memo[id]; ok {
			return e
		}
		e := s.taskEventsLocked(id)
		memo[id] = e
		return e
	}
	s.decorateCardsTiming(cards, byID, productID, time.Now().Unix(), eventsOf)
	s.decorateCardsConversationWith(cards, byID, eventsOf)
	s.decorateCardsGate(cards, byID)
}

// BoardPage is a product's board cut to one page. Read-only.
func (s *MemStore) BoardPage(ctx context.Context, productID string, q BoardQuery, w BoardWindow) (BoardPage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m := s.masterPtr(productID)
	if m == nil {
		return BoardPage{}, ErrNotFound
	}

	// 1. One walk: the product's tasks, and what each select can offer.
	var tasks []*TaskRecord
	roles, labels, wfs := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for i := range s.tasks {
		t := &s.tasks[i]
		if t.DeskID != productID {
			continue
		}
		tasks = append(tasks, t)
		if t.Role != "" {
			roles[t.Role] = true
		}
		if t.WorkflowID != "" {
			wfs[t.WorkflowID] = true
		}
		for _, l := range t.Labels {
			labels[l] = true
		}
	}
	var sevs []string
	for _, lv := range prSeverities {
		if labels[labelFamilySeverity+":"+lv] {
			sevs = append(sevs, lv)
		}
		delete(labels, labelFamilySeverity+":"+lv) // the Label select never lists them
	}
	facets := BoardFacets{Roles: sortedKeys(roles), Labels: sortedKeys(labels), Workflows: sortedKeys(wfs),
		Severities: sevs, Stories: []BoardRow{{ID: PlanLane, Label: "Plan"}}}
	for _, st := range m.Stories {
		facets.Stories = append(facets.Stories, BoardRow{ID: st.ID, Label: st.ID + " · " + st.Want})
	}

	// 2. The query the product can actually answer.
	q = q.clean(facets)
	p := BoardPage{ProductID: productID, Query: q, Text: w.Text, All: len(tasks), Facets: facets}
	text := strings.ToLower(w.Text)

	// 3. Filter into the columns' buckets, on the task's own fields.
	buckets := map[string][]*TaskRecord{}
	for _, t := range tasks {
		if q.matches(t, text) {
			col := columnOf(t.Status)
			buckets[col] = append(buckets[col], t)
			p.Matched++
		}
	}

	// 4. Order each bucket: by rank -- but Done and Canceled newest first, by
	// the task's last state change, so the column opens on what just finished.
	// The last change is read for the tasks it orders or labels: those two
	// columns' matches, and, in the table, every match.
	var ordered []*TaskRecord
	if q.View == "table" {
		for _, b := range buckets {
			ordered = append(ordered, b...)
		}
	} else {
		ordered = append(append(ordered, buckets[ColDone]...), buckets[ColCanceled]...)
	}
	last := s.lastChangesLocked(ordered)
	for col, b := range buckets {
		newest := col == ColDone || col == ColCanceled
		sort.SliceStable(b, func(i, j int) bool {
			if newest && last[b[i].ID].Seq != last[b[j].ID].Seq {
				return last[b[i].ID].Seq > last[b[j].ID].Seq
			}
			if b[i].Rank != b[j].Rank {
				return b[i].Rank < b[j].Rank
			}
			return b[i].ID < b[j].ID
		})
	}

	// 5. Cut, then 6. build and decorate only what was kept.
	var kept []BoardCard
	if q.View == "table" {
		var rows []*TaskRecord
		for _, c := range boardColumns {
			rows = append(rows, buckets[c.ID]...)
			p.Columns = append(p.Columns, BoardPageColumn{BoardColumn: c, Matched: len(buckets[c.ID])})
		}
		sortTableRows(rows, q, last)
		// One page of the table (D-097). The clamped page goes back into the
		// query, so the address the response writes is a page that exists.
		pg, lo, hi := clampPage(len(rows), q.Page, tablePageSize)
		if pg <= 1 {
			pg = 0
		}
		p.Query.Page = pg
		rows = rows[lo:hi]
		byID := make(map[string]*TaskRecord, len(rows))
		for _, t := range rows {
			kept = append(kept, cardOf(t, m, last[t.ID]))
			byID[t.ID] = t
		}
		s.decorateCardsLocked(kept, byID, m, productID)
		p.Rows, p.RowsShown = kept, len(kept)
		if p.Rows == nil {
			p.Rows = []BoardCard{}
		}
		return p, nil
	}
	type span struct{ from, to int }
	spans := map[string]span{}
	byID := map[string]*TaskRecord{}
	for _, c := range boardColumns {
		b := buckets[c.ID]
		n := w.Shown[c.ID]
		if n <= 0 {
			n = boardStep
		}
		b = b[:min(n, boardMaxShown, len(b))]
		from := len(kept)
		for _, t := range b {
			kept = append(kept, cardOf(t, m, s.lastChangeOf(last, t)))
			byID[t.ID] = t
		}
		spans[c.ID] = span{from, len(kept)}
	}
	s.decorateCardsLocked(kept, byID, m, productID)
	for _, c := range boardColumns {
		sp := spans[c.ID]
		cards := kept[sp.from:sp.to:sp.to]
		if len(cards) == 0 {
			cards = []BoardCard{}
		}
		p.Columns = append(p.Columns, BoardPageColumn{BoardColumn: c, Matched: len(buckets[c.ID]), Shown: len(cards), Cards: cards})
	}
	return p, nil
}

// sortTableRows orders the table. rows arrive in board order -- column, then
// each column's own order -- which is the default ("status"). updated is
// newest first unless told otherwise; everything else ascends.
func sortTableRows(rows []*TaskRecord, q BoardQuery, last map[string]lastChange) {
	if q.Sort == "" {
		return
	}
	if q.Sort == "severity" {
		// Worst first unless told otherwise; a card with no severity is last
		// in both directions, so it never fills the top of the table.
		asc := q.Dir == "asc"
		sort.SliceStable(rows, func(i, j int) bool {
			a, b := prSeverityRank(severityOf(rows[i].Labels)), prSeverityRank(severityOf(rows[j].Labels))
			if (a < 0) != (b < 0) {
				return b < 0
			}
			if a != b {
				if asc {
					return a < b
				}
				return a > b
			}
			return rows[i].ID < rows[j].ID
		})
		return
	}
	desc := q.Dir == "desc" || (q.Dir == "" && q.Sort == "updated")
	less := func(a, b *TaskRecord) bool {
		switch q.Sort {
		case "role":
			if a.Role != b.Role {
				return a.Role < b.Role
			}
		case "story":
			if la, lb := laneOf(*a), laneOf(*b); la != lb {
				return la < lb
			}
		case "updated":
			if last[a.ID].Seq != last[b.ID].Seq {
				return last[a.ID].Seq < last[b.ID].Seq
			}
		}
		return a.ID < b.ID
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if desc {
			return less(rows[j], rows[i])
		}
		return less(rows[i], rows[j])
	})
}

// BoardCard is one card, decorated as the board decorates it. The three
// handlers that answer with a single card -- a drag's JSON answer, a
// send-back's, the timing sheet's title -- used to build the whole board for it.
func (s *MemStore) BoardCard(ctx context.Context, productID, taskID string) (BoardCard, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m := s.masterPtr(productID)
	if m == nil {
		return BoardCard{}, ErrNotFound
	}
	t := s.cardLocked(productID, taskID)
	if t == nil {
		return BoardCard{}, ErrNotFound
	}
	byID := map[string]*TaskRecord{t.ID: t}
	last := s.lastChangesLocked([]*TaskRecord{t})
	cards := []BoardCard{cardOf(t, m, last[t.ID])}
	cards[0].Row = laneOf(*t) // one card is still a card of the unpaged board
	s.decorateCardsLocked(cards, byID, m, productID)
	return cards[0], nil
}

// decorateCardsTiming puts the timing summary on the given cards only -- not,
// as decorateBoardTiming does, on every task of the product. Caller holds s.mu.
func (s *MemStore) decorateCardsTiming(cards []BoardCard, byID map[string]*TaskRecord, productID string, now int64, eventsOf func(string) []Event) {
	want := make(map[string]bool, len(cards))
	for i := range cards {
		want[cards[i].ID] = true
	}
	runs := map[string][]TaskRunRecord{}
	for _, r := range s.runs {
		if want[r.TaskID] && (r.DeskID == productID || r.DeskID == "") {
			runs[r.TaskID] = append(runs[r.TaskID], r)
		}
	}
	mode := runnerMode()
	for i := range cards {
		t := byID[cards[i].ID]
		if t == nil {
			continue
		}
		sum := cardTiming(*t, eventsOf(t.ID), runs[t.ID], now, mode).CardTimingSummary
		cards[i].Timing = &sum
		s.timingCalls.Add(1)
	}
}

// EventCount is how many events a product has: the Timeline tab's number.
// buildBO used to copy the whole stream on every product tab to print it.
func (s *MemStore) EventCount(ctx context.Context, productID string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := 0
	for i := range s.events {
		if s.events[i].DeskID == productID {
			n++
		}
	}
	return n, nil
}

// ---------- what the browser already has ----------

// colRev and cardRev are what a column's hash is taken over: everything the
// card template prints that does not depend on the clock. A running card's
// lead time and its seconds change by themselves; the browser re-ticks the one
// it shows (static/live.js), and a hash that included them would re-send every
// column with an open card on every hint. A field the card template prints
// must be here, or a change to it would not reach the screen.
type colRev struct {
	Hidden  bool
	Matched int
	Cards   []cardRev
}

type cardRev struct {
	ID, Title, Status, Role, Vendor, Story, Workflow, Reason string
	Rank                                                     int
	Labels, Moves                                            []string
	Qa                                                       *BoardQa
	Conversation                                             *BoardConversation
	SendBack                                                 *BoardSendBack
	TimingKnown, TimingComplete, TimingFinished              bool
	CreatedAt                                                int64
	Attempts                                                 int
	LeadText, TitleText                                      string // only once the clock has stopped
}

func revCard(c BoardCard) cardRev {
	r := cardRev{ID: c.ID, Title: c.Label, Status: c.Status, Role: c.Role, Vendor: c.Vendor, Story: c.Story,
		Workflow: c.Workflow, Reason: c.Reason, Rank: c.Rank, Labels: c.Labels, Moves: c.Moves,
		Qa: c.Qa, Conversation: c.Conversation, SendBack: c.SendBack}
	if t := c.Timing; t != nil {
		r.TimingKnown, r.TimingComplete, r.TimingFinished = t.Known, t.Complete, t.Finished
		r.CreatedAt, r.Attempts = t.CreatedAt, t.Attempts
		if t.Finished {
			r.LeadText, r.TitleText = t.LeadText, t.TitleText
		}
	}
	return r
}

// columnRev is the 16 hex digits a column element carries in data-rev.
func columnRev(c BoardPageColumn, hidden bool) string {
	in := colRev{Hidden: hidden, Matched: c.Matched, Cards: make([]cardRev, len(c.Cards))}
	for i, card := range c.Cards {
		in.Cards[i] = revCard(card)
	}
	b, _ := json.Marshal(in)
	h := fnv.New64a()
	h.Write(b)
	return fmt.Sprintf("%016x", h.Sum64())
}
