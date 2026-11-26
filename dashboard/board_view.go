package main

// The Board tab's view model and its own route (D-094,
// docs/designs/urls-and-board.md §4.3–§4.6, §5.1).
//
// boardView turns a BoardPage into what the templates print: the filter bar's
// options, the columns with what the browser needs to ask for the next change
// (data-shown, data-rev), the table, the links. handleBoardLive is the route
// the bar, "Show more" and a live refresh all call; it answers with the columns
// that differ from what the browser already has, and nothing else.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

// BoardColView is one column as the template prints it.
type BoardColView struct {
	BoardPageColumn
	ProductID string
	Rev       string // data-rev: what the browser sends back to say "I have this"
	Hidden    bool   // "Hide empty columns" is on and nothing here matches
	More      bool   // there are more matching cards, and room for them
	Capped    bool   // the column is at its cap and there are still more
	Send      bool   // a live response carries this column
}

// BoardSortLink is a table header.
type BoardSortLink struct {
	Key, Label string
	Get, Href  string // the fragment address and the page address
	Aria       string // aria-sort: ascending | descending | none
}

// BoardView is the Board tab.
type BoardView struct {
	ProductID, Slug string
	Page            BoardPage
	Cols            []BoardColView
	Table           bool
	Rows            []BoardCard
	RowsMore        bool // more rows match, and the table may still grow
	RowsCapped      bool
	Sorts           []BoardSortLink

	// The bar.
	Text                                   string
	RoleOpts, StoryOpts, LabelOpts, WfOpts []AFOpt
	Filtered                               bool // a filter or a search narrows the board
	LiveURL                                string
	BoardGet, BoardHref                    string
	TableGet, TableHref                    string
	ResetGet, ResetHref                    string
	CountText                              string
	TimelineN                              int

	// Live is set for a response of the Board's own route: its parts are
	// out-of-band elements. In the sheet they are plain, because the sheet is
	// itself out of band and one never nests in another.
	Live bool
	// Sisflow is set when the client board is on screen: the response carries
	// the count line and no column.
	Sisflow bool
}

// tableHeads are the table's columns that sort, in order.
var tableHeads = []struct{ Key, Label string }{
	{"id", "ID"}, {"", "Title"}, {"status", "Status"}, {"role", "Role"}, {"story", "Story"},
	{"", "Workflow"}, {"", "Attempts"}, {"", "Lead time"}, {"updated", "Changed"}, {"", "Actions"},
}

func boardOpts(all string, values []string, label func(string) string, on string) []AFOpt {
	out := []AFOpt{{Value: "", Label: all, On: on == ""}}
	for _, v := range values {
		out = append(out, AFOpt{Value: v, Label: label(v), On: v == on})
	}
	return out
}

// boardView builds the Board tab from one page.
func boardView(p BoardPage, slug string, timelineN int) *BoardView {
	q := p.Query
	bv := &BoardView{ProductID: p.ProductID, Slug: slug, Page: p, Table: q.View == "table", Text: p.Text, TimelineN: timelineN,
		Filtered: p.Text != "" || q.Role != "" || q.Story != "" || q.Label != "" || q.Workflow != "" || q.Waiting}
	same := func(v string) string { return v }
	bv.RoleOpts = boardOpts("Every role", p.Facets.Roles, func(v string) string {
		if r, ok := roles[v]; ok {
			return r.Label
		}
		return v
	}, q.Role)
	bv.LabelOpts = boardOpts("Every label", p.Facets.Labels, same, q.Label)
	bv.WfOpts = boardOpts("Every workflow", p.Facets.Workflows, same, q.Workflow)
	bv.StoryOpts = []AFOpt{{Value: "", Label: "Every story", On: q.Story == ""}}
	for _, st := range p.Facets.Stories {
		bv.StoryOpts = append(bv.StoryOpts, AFOpt{Value: st.ID, Label: clipEllipsis(st.Label, 48), On: st.ID == q.Story})
	}

	// The addresses. A page address reproduces the whole view; a fragment
	// address names only what the link changes -- and names it even when the
	// change is "nothing", because the URL wins over the state header and an
	// empty value is how a link clears one.
	frag := "products/" + url.PathEscape(p.ProductID)
	page := func(v BoardQuery) string {
		if enc := v.Encode(); enc != "" {
			return productPath(slug, "board") + "?" + enc
		}
		return productPath(slug, "board")
	}
	bv.LiveURL = frag + "/board/live"
	asBoard, asTable := q, q
	asBoard.View, asBoard.Sort, asBoard.Dir = "", "", ""
	asTable.View = "table"
	bv.BoardGet, bv.BoardHref = frag+"?tab=board&view=", page(asBoard)
	bv.TableGet, bv.TableHref = frag+"?tab=board&view=table", page(asTable)
	bv.ResetGet = frag + "?tab=board&role=&story=&label=&wf=&waiting=&empty="
	bv.ResetHref = page(BoardQuery{View: q.View, Sort: q.Sort, Dir: q.Dir})

	if p.Matched == p.All {
		bv.CountText = thousands(p.All) + " card" + pluralS(p.All)
	} else {
		bv.CountText = thousands(p.Matched) + " of " + thousands(p.All) + " card" + pluralS(p.All) + " match"
	}

	for _, c := range p.Columns {
		hidden := q.HideEmpty && c.Matched == 0
		cv := BoardColView{BoardPageColumn: c, ProductID: p.ProductID, Hidden: hidden,
			More:   c.Shown < c.Matched && c.Shown < boardMaxShown,
			Capped: c.Shown >= boardMaxShown && c.Matched > c.Shown}
		if !bv.Table {
			cv.Rev = columnRev(c, hidden)
		}
		bv.Cols = append(bv.Cols, cv)
	}
	if bv.Table {
		bv.Rows = p.Rows
		bv.RowsMore = p.RowsShown < p.Matched && p.RowsShown < tableMaxRows
		bv.RowsCapped = p.RowsShown >= tableMaxRows && p.Matched > p.RowsShown
		for _, h := range tableHeads {
			l := BoardSortLink{Key: h.Key, Label: h.Label, Aria: "none"}
			if h.Key != "" {
				sorted := q.Sort == h.Key || (h.Key == "status" && q.Sort == "")
				// Clicking the sorted column turns it round; any other starts
				// in its own natural direction.
				natural := "asc"
				if h.Key == "updated" {
					natural = "desc"
				}
				cur := q.Dir
				if cur == "" {
					cur = natural
				}
				next := natural
				if sorted && cur == natural {
					next = map[string]string{"asc": "desc", "desc": "asc"}[natural]
				}
				if sorted {
					l.Aria = map[string]string{"asc": "ascending", "desc": "descending"}[cur]
				}
				to := q
				to.Sort, to.Dir = h.Key, next
				if h.Key == "status" {
					// The default order has one spelling: no sort at all.
					to.Sort, to.Dir = "", ""
					l.Aria = map[bool]string{true: "ascending", false: "none"}[sorted]
				}
				l.Get = frag + "?tab=board&view=table&sort=" + url.QueryEscape(to.Sort) + "&dir=" + url.QueryEscape(to.Dir)
				l.Href = page(to)
			}
			bv.Sorts = append(bv.Sorts, l)
		}
	}
	return bv
}

// productSlug is a product's slug, "" when there is no such product.
func (s *server) productSlug(ctx context.Context, id string) (string, bool) {
	ps, _ := s.store.Products(ctx)
	for _, p := range ps {
		if p.ID == id {
			return p.Slug, true
		}
	}
	return "", false
}

// buildBoardView reads a product's board for a request: the view and the window
// the request carries -- whatever its method, from its URL and its state header,
// never its body -- as one page.
func (s *server) buildBoardView(r *http.Request, productID, slug string) (*BoardView, error) {
	q, w := parseBoardQuery(r)
	page, err := s.store.BoardPage(r.Context(), productID, q, w)
	if err != nil {
		return nil, err
	}
	n, _ := s.store.EventCount(r.Context(), productID)
	bv := boardView(page, slug, n)
	bv.Sisflow = boardParamsOf(r).get("surface") == "sisflow"
	return bv, nil
}

// boardLive fills in what makes a view a live response: which columns go out.
// A column is sent when the browser's hash for it differs, or it sent none.
func boardLive(bv *BoardView, r *http.Request) *BoardView {
	bv.Live = true
	have := revOf(r)
	for i := range bv.Cols {
		c := &bv.Cols[i]
		c.Send = !bv.Table && !bv.Sisflow && have[c.ID] != c.Rev
	}
	return bv
}

// boardAddress tells the browser the Board's address after a response of its
// own route: replaced when the view changed, never pushed -- this route follows
// the filter and must not navigate, least of all an operator who has left.
func boardAddress(w http.ResponseWriter, r *http.Request, bv *BoardView) {
	href := productPath(bv.Slug, "board")
	if enc := bv.Page.Query.Encode(); enc != "" {
		href += "?" + enc
	}
	pushURL(w, r, &BOView{Href: href}, true)
}

// handleBoardLive is GET /products/{id}/board/live: the Board's own route. The
// filter bar, "Show more", a sort and a live refresh all arrive here, each with
// what the browser shows (X-Board-State) and what it is searching for
// (X-Board-Search); the answer is the columns that differ, the count line, and
// the Timeline's number.
func (s *server) handleBoardLive(w http.ResponseWriter, r *http.Request) {
	if !s.requireBO(w, r) {
		return
	}
	id := r.PathValue("id")
	slug, ok := s.productSlug(r.Context(), id)
	if !ok {
		s.fail(w, r, ErrNotFound)
		return
	}
	bv, err := s.buildBoardView(r, id, slug)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	boardAddress(w, r, boardLive(bv, r))
	s.rend.Render(w, http.StatusOK, "board-live", bv)
}

// renderBoardAfter answers a mutation made on the Board with the Board, not the
// sheet: the columns that changed (or the table), the toast and the chrome. It
// sets no address header -- a mutation never rewrites the address.
func (s *server) renderBoardAfter(w http.ResponseWriter, r *http.Request, res Result, productID string) {
	slug, ok := s.productSlug(r.Context(), productID)
	if !ok {
		s.fail(w, r, ErrNotFound)
		return
	}
	bv, err := s.buildBoardView(r, productID, slug)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	v := s.applyResult(r, res)
	v.BoardLive = boardLive(bv, r)
	w.Header().Set("Cache-Control", "no-store")
	s.rend.RenderWithChrome(w, "board-live-view", v)
}

// pagedBoard is GET /products/{id}/board?paged=1: the document the client board
// loads. The same page the server board renders, in the shape the unpaged
// document has, with no lanes and with what a client needs to say "N of M".
type pagedBoard struct {
	ProductID string           `json:"productId"`
	Columns   []BoardColumn    `json:"columns"`
	Rows      []BoardRow       `json:"rows"`
	Cards     []BoardCard      `json:"cards"`
	Workflows []WorkflowTiming `json:"workflows"`
	Counts    map[string]int   `json:"counts"` // matching cards per column
	Matched   int              `json:"matched"`
	All       int              `json:"all"`
}

func (s *server) handleBoardPaged(w http.ResponseWriter, r *http.Request, id string) {
	q, win := parseBoardQuery(r)
	q.View, q.Sort, q.Dir = "", "", "" // the client board is the board
	p, err := s.store.BoardPage(r.Context(), id, q, win)
	if errors.Is(err, ErrNotFound) {
		writeBoardJSON(w, http.StatusNotFound, map[string]string{"error": "no such product"})
		return
	}
	if err != nil {
		writeBoardJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	doc := pagedBoard{ProductID: id, Columns: boardColumns, Rows: []BoardRow{}, Cards: []BoardCard{},
		Workflows: []WorkflowTiming{}, Counts: map[string]int{}, Matched: p.Matched, All: p.All}
	for _, c := range p.Columns {
		doc.Counts[c.ID] = c.Matched
		doc.Cards = append(doc.Cards, c.Cards...)
	}
	w.Header().Set("Cache-Control", "no-store")
	writeBoardJSON(w, http.StatusOK, doc)
}

// boardFooter is a column's last line once it is at its cap.
func boardFooter(shown, matched int) string {
	return fmt.Sprintf("Showing the first %s of %s. Filter, or use the table.", thousands(shown), thousands(matched))
}
