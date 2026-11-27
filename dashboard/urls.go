package main

// Real URLs for the back office (D-094, docs/designs/urls-and-board.md §3).
//
// Until v0.56.0 every navigation was an htmx GET into #sink and the address
// stayed on the mount point. Two things change that, and both live here:
//
//   - PAGE ROUTES. /inbox, /prs, … and /p/{ref}/{section} render the whole
//     document for one view, so a deep link, a reload and Back all land on it.
//     They return pages only; the fragment routes (products/{id}, ui/bo/{view})
//     still return fragments only, so no URL answers in two shapes.
//   - THE ADDRESS HEADER. buildBO works out the canonical address of what it
//     built (BOView.Href), and sheet() -- the one door every handler that
//     renders the sheet goes through -- tells htmx to push or replace it.
//
// A product is named in a URL by its slug, which is stored (Product.Slug).

import (
	"log"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// pageViews are the global views that have a page route, with the label the
// navigation gives each (templates/backoffice.html, the back-office menu).
// "products" is not here: the product list is the home page.
var pageViews = []struct{ ID, Label string }{
	{"inbox", "Inbox"}, {"stories", "Stories"}, {"diagrams", "Diagrams"},
	{"knowledge", "Knowledge"}, {"agents", "Agents"}, {"archive", "Archive"},
	{"integrations", "Integrations"}, {"autofix", "Auto-fix"}, {"prs", "Pull requests"},
}

// productSections are a product's tabs, with their labels (the product menu).
var productSections = []struct{ ID, Label string }{
	{"brd", "Requirements"}, {"stories", "Stories"}, {"diagrams", "Diagrams"},
	{"knowledge", "Knowledge"}, {"board", "Board"}, {"qa", "QA"},
	{"timeline", "Timeline"}, {"blueprint", "Blueprint"}, {"vendors", "AI vendors"},
}

const (
	defaultPageTitle = "GAOS · Products"
	// noHref is BOView.Href for a view that has no address: a product view of
	// a product that does not exist. pushURL sends nothing for it.
	noHref = "-"
	// maxHrefValue bounds a query value that is written back into an address.
	maxHrefValue = 128
	// maxSlug is the longest slug accepted from the database. newSlug cuts the
	// name to 48 bytes; the rest is room for a "-<n>" suffix.
	maxSlug = 64
)

func viewLabel(id string) (string, bool) {
	for _, v := range pageViews {
		if v.ID == id {
			return v.Label, true
		}
	}
	return "", false
}

func sectionLabel(id string) (string, bool) {
	for _, s := range productSections {
		if s.ID == id {
			return s.Label, true
		}
	}
	return "", false
}

// pagePatterns are the page routes, as the mux knows them. They are back-office
// routes like any other (BR-55): signed out they are refused with 401. The
// gate only gives that refusal a body a browser can use -- the sign-in page --
// instead of JSON, because these are the URLs a person bookmarks.
var pagePatterns = func() map[string]bool {
	m := map[string]bool{"GET /p/{ref}": true, "GET /p/{ref}/{section}": true}
	for _, v := range pageViews {
		m["GET /"+v.ID] = true
	}
	return m
}()

// ---------- the slug ----------

var (
	slugShape      = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	productIDShape = regexp.MustCompile(`^p-[0-9]+$`)
)

// productNum is the number in a p-<n> id, for ordering; ok is false for an id
// of another shape.
func productNum(id string) (int, bool) {
	rest, ok := strings.CutPrefix(id, "p-")
	if !ok || rest == "" {
		return 0, false
	}
	n, err := strconv.Atoi(rest)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// slugTaken says whether a candidate may not be given to product id: another
// product holds it, or it looks like a product id that is not this product's
// own -- /p/p-7/… must always mean product p-7.
func slugTaken(cand, id string, products []Product) bool {
	if productIDShape.MatchString(cand) && cand != id {
		return true
	}
	for _, p := range products {
		if p.ID != id && p.Slug == cand {
			return true
		}
	}
	return false
}

// newSlug is the slug a product gets: its name through slug(), cut to 48
// bytes, or its id when the name has nothing a URL can carry; then the first
// of base, base-2, base-3, … that is free. Pure.
func newSlug(name, id string, products []Product) string {
	base := slug(name)
	if len(base) > 48 {
		base = strings.TrimRight(base[:48], "-")
	}
	if base == "" {
		base = id
	}
	if !slugTaken(base, id, products) {
		return base
	}
	for n := 2; ; n++ {
		if cand := base + "-" + strconv.Itoa(n); !slugTaken(cand, id, products) {
			return cand
		}
	}
}

// validSlug is the shape a stored slug must have before it is trusted in an
// address. The database is edited by hand; how a slug was written proves
// nothing about what is in it now.
func validSlug(s string) bool {
	return s != "" && len(s) <= maxSlug && slugShape.MatchString(s)
}

// productOrder is the products' indexes in ascending numeric id order, ids of
// another shape last and by name. The back-fill walks it so that the older of
// two namesakes keeps the bare slug.
func productOrder(products []Product) []int {
	idx := make([]int, len(products))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool {
		na, oka := productNum(products[idx[a]].ID)
		nb, okb := productNum(products[idx[b]].ID)
		switch {
		case oka && okb:
			return na < nb
		case oka != okb:
			return oka
		}
		return products[idx[a]].ID < products[idx[b]].ID
	})
	return idx
}

// backfillSlugs gives a slug to every product that has no usable one and
// returns the indexes it changed, in the order it changed them. A stored slug
// is kept only if it is well formed, is not shaped like another product's id,
// and no product with a lower id holds it; anything else is assigned again.
// Idempotent, and deterministic for the same products.
func backfillSlugs(products []Product) []int {
	order := productOrder(products)
	held := map[string]bool{}
	var redo []int
	for _, i := range order {
		p := &products[i]
		ok := validSlug(p.Slug) && !held[p.Slug] && !(productIDShape.MatchString(p.Slug) && p.Slug != p.ID)
		if !ok {
			p.Slug = ""
			redo = append(redo, i)
			continue
		}
		held[p.Slug] = true
	}
	for _, i := range redo {
		products[i].Slug = newSlug(products[i].Name, products[i].ID, products)
	}
	return redo
}

// resolveProduct finds the product a URL names: by slug, or by id. bySlug says
// which, because the id form is answered with a redirect to the slug form.
func resolveProduct(products []Product, ref string) (p Product, bySlug, ok bool) {
	for _, c := range products {
		if c.Slug != "" && c.Slug == ref {
			return c, true, true
		}
	}
	for _, c := range products {
		if c.ID == ref {
			return c, false, true
		}
	}
	return Product{}, false, false
}

// productPath is a product section's page address, relative to the base.
func productPath(slug, section string) string {
	return "p/" + url.PathEscape(slug) + "/" + section
}

// ---------- the address of a view ----------

// pageHref turns a fragment address into the page address of the same view:
// ui/bo/prs?state=merged -> prs?state=merged. Templates print it in href
// beside the fragment address in hx-get, so "open in new tab" opens a page.
func pageHref(fragment string) string {
	return strings.TrimPrefix(fragment, "ui/bo/")
}

func prPageHref(f PRFilter) string { return pageHref(prHref(f)) }
func afPageHref(f AFFilter) string { return pageHref(afHref(f)) }

func boundValue(v string) string {
	if len(v) > maxHrefValue {
		return ""
	}
	return v
}

// address sets bo.Href and bo.PageTitle from what buildBO built. The path
// follows from the view, the product and the tab; the query is rebuilt from
// what the view parsed, never copied from the request.
func (s *server) address(bo *BOView, products []Product, productID string) {
	bo.PageTitle = defaultPageTitle
	switch {
	case bo.View == "inbox":
		bo.Href = "inbox"
		for _, p := range products {
			if p.ID == bo.Filter && bo.Filter != "" {
				bo.Href += "?" + url.Values{"product": {p.ID}}.Encode()
			}
		}
	case bo.View == "prs" && bo.PRs != nil:
		f := bo.PRs.Filter
		f.Open = boundValue(f.Open)
		bo.Href = prPageHref(f)
	case bo.View == "agents":
		// The product filter is the validated one (D-102); the page comes last.
		bo.Href = agentsHref("agents", bo.Assign.Product, bo.Page)
	case bo.View == "autofix" && bo.AutoFix != nil:
		f := bo.AutoFix.Filter
		f.Open = boundValue(f.Open)
		bo.Href = afPageHref(f)
	case bo.Detail != nil:
		section := bo.Detail.Tab
		label, ok := sectionLabel(section)
		if !ok {
			section, label = "brd", "Requirements"
		}
		bo.Href = productPath(bo.Detail.Slug, section)
		// The Board's view is part of its address (D-095): the filter, board
		// or table, the sort. Never the window, never the search text.
		if bv := bo.Detail.BoardView; bv != nil && section == "board" {
			if enc := bv.Page.Query.Encode(); enc != "" {
				bo.Href += "?" + enc
			}
		}
		// The Timeline's and the QA table's page (D-097), clamped, and only
		// when it is not the first.
		switch {
		case section == "timeline" && bo.Detail.Pager.Page > 1:
			bo.Href += "?" + pageQ("page", bo.Detail.Pager.Page)
		case section == "qa" && bo.Detail.QA != nil && bo.Detail.QA.Pager.Page > 1:
			bo.Href += "?" + pageQ("page", bo.Detail.QA.Pager.Page)
		}
		bo.PageTitle = label + " · " + bo.Detail.Name + " · GAOS"
		return
	case bo.View == "products" && productID != "":
		// A product view of a product that is not there: no address.
		bo.Href = noHref
		return
	default:
		if _, ok := viewLabel(bo.View); ok {
			bo.Href = bo.View
			// The list pages' pages (D-097): built from the clamped integers
			// the view kept, never from the request's query.
			var q []string
			for _, kv := range []struct {
				k string
				n int
			}{{"apage", bo.APage}, {"page", bo.Page}, {"ppage", bo.PPage}} {
				if s := pageQ(kv.k, kv.n); s != "" {
					q = append(q, s)
				}
			}
			if len(q) > 0 {
				bo.Href += "?" + strings.Join(q, "&")
			}
		} else {
			bo.Href = "" // the product list is the home page
			return
		}
	}
	if label, ok := viewLabel(bo.View); ok {
		bo.PageTitle = label + " · GAOS"
	}
}

// splitAddress reduces an address to a comparable path and query: the path
// without its leading and trailing slash, the query parsed and re-encoded so
// that parameter order and escaping do not make two equal addresses differ.
func splitAddress(path, rawQuery string) (string, string) {
	q, _ := url.ParseQuery(rawQuery)
	return strings.Trim(path, "/"), q.Encode()
}

// currentAddress is where the browser says it is (HX-Current-URL), with the
// mount prefix removed. A missing or unparsable header is the home page.
func currentAddress(r *http.Request) (path, query string) {
	u, err := url.Parse(r.Header.Get("HX-Current-URL"))
	if err != nil {
		return "", ""
	}
	p := u.Path
	if pre := sitePrefix(r); pre != "" {
		if rest, ok := strings.CutPrefix(p, pre); ok && (rest == "" || rest[0] == '/') {
			p = rest
		}
	}
	return splitAddress(p, u.RawQuery)
}

// refreshHeader marks a request live.js made on its own, after a hint. Such a
// request never moves the address: the operator did not ask for it.
const refreshHeader = "X-GAOS-Refresh"

// pushURL tells htmx what the address should be after this response:
//
//	path differs               -> HX-Push-Url     (a navigation)
//	GET, same path, query not  -> HX-Replace-Url  (a filter, a drawer)
//	anything else              -> nothing
//
// A mutation never rewrites the query of the page it was made on. With
// replaceOnly the first row is "nothing" too: that is the Board's own route,
// which follows its filter and must never navigate. HX-Current-URL is only
// compared here; it is never written into a response.
func pushURL(w http.ResponseWriter, r *http.Request, bo *BOView, replaceOnly bool) {
	if bo == nil || bo.Href == noHref || r.Header.Get("HX-Request") == "" {
		return
	}
	if !replaceOnly && r.Header.Get(refreshHeader) != "" {
		return
	}
	hrefPath, hrefQuery, _ := strings.Cut(bo.Href, "?")
	wantPath, wantQuery := splitAddress(hrefPath, hrefQuery)
	curPath, curQuery := currentAddress(r)

	value := sitePrefix(r) + "/" + bo.Href
	switch {
	case wantPath != curPath:
		if !replaceOnly {
			w.Header().Set("HX-Push-Url", value)
		}
	case r.Method == http.MethodGet && wantQuery != curQuery:
		w.Header().Set("HX-Replace-Url", value)
	}
}

// sheet builds the back-office state for a response that renders the sheet,
// and sets what goes with every such response: no-store for the sensitive
// views, and the address. It is the only way a handler should fill v.BO for an
// htmx response; TestOnlyTheAllowedFunctionsCallBuildBO holds the list of
// functions that may call buildBO directly.
func (s *server) sheet(w http.ResponseWriter, r *http.Request, v *View, view, productID, tab string) {
	v.BO = s.buildBO(r, view, productID, tab)
	noStoreView(w, v.BO.View)
	pushURL(w, r, v.BO, false)
}

// ---------- the page routes ----------

// handlePage renders the whole document for one global view. The gate has
// already refused a signed-out caller.
func (s *server) handlePage(view string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		v, _ := s.newView(r)
		v.BO = s.buildBO(r, view, r.URL.Query().Get("product"), "")
		w.Header().Set("Cache-Control", "no-store")
		s.rend.Render(w, http.StatusOK, "page", v)
	}
}

// handleProductPage is GET /p/{ref}/{section} and GET /p/{ref}.
func (s *server) handleProductPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	section := r.PathValue("section")
	bare := section == "" // GET /p/{ref}
	if bare {
		section = "brd"
	}
	// The section first: an unknown one is never written into a Location.
	if _, ok := sectionLabel(section); !ok {
		s.pageNotFound(w, r, "No such section.")
		return
	}
	products, _ := s.store.Products(r.Context())
	p, bySlug, ok := resolveProduct(products, r.PathValue("ref"))
	if !ok {
		s.pageNotFound(w, r, "No such product.")
		return
	}
	if !bySlug || bare {
		http.Redirect(w, r, sitePrefix(r)+"/"+productPath(p.Slug, section), http.StatusFound)
		return
	}
	v, _ := s.newView(r)
	v.BO = s.buildBO(r, "products", p.ID, section)
	if v.BO.Detail == nil {
		s.pageNotFound(w, r, "No such product.")
		return
	}
	s.rend.Render(w, http.StatusOK, "page", v)
}

// pageNotFound is a 404 a person can read: the product list, and why.
func (s *server) pageNotFound(w http.ResponseWriter, r *http.Request, why string) {
	v, _ := s.newView(r)
	v.BO = s.buildBO(r, "products", "", "brd")
	v.Toast = why
	s.rend.Render(w, http.StatusNotFound, "page", v)
}

// signedOutPage is the gate's answer to a signed-out browser that asked for a
// page route: still a refusal (401, BR-55), with the sign-in page as its body.
// It is the document GET / renders signed out, and it is built without calling
// the handler, so it cannot say whether the product in the URL exists.
func (s *server) signedOutPage(w http.ResponseWriter, r *http.Request) {
	v, _ := s.newView(r)
	v.SignedIn = false
	v.ShowLogin = true
	v.BO = nil
	w.Header().Set("Cache-Control", "no-store")
	s.rend.Render(w, http.StatusUnauthorized, "page", v)
}

// loginLandsElsewhere says whether a sign-in was made from a page other than
// the home page, in which case the browser is told to reload where it is
// rather than being handed the product list.
func loginLandsElsewhere(r *http.Request) bool {
	if r.Header.Get("HX-Current-URL") == "" {
		return false
	}
	path, _ := currentAddress(r)
	return path != ""
}

// logSlugBackfill reports a back-fill that could not be written. By id only: a
// product's name is not for a log line.
func logSlugBackfill(id string, err error) {
	log.Printf("slug: product %s: back-fill not written (%v); it is assigned again at the next boot", id, err)
}
