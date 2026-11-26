package main

import (
	"net/url"
	"strconv"
)

// One pager for every list the back office pages (docs/designs/board-and-list-ux.md,
// D-097). A data table shows tablePageSize rows a page, a tenant-wide list page
// listPageSize items. The page lives in the address as page=N (archive also has
// apage= and ppage=), is read through pageParam and nowhere else, and is
// written back only after clampPage -- so an address never names a page that
// does not exist.
const (
	tablePageSize = 10
	listPageSize  = 5
	maxPage       = 100000 // an input bound, not a product limit
)

// PageLink is one link of a pager: a page number, or the gap between two.
type PageLink struct {
	N         int
	Get, Href string // the fragment address (hx-get) and the page address (href)
	On, Gap   bool   // the current page; "…" in place of two or more skipped pages
}

// Pager is what the "pager" template prints.
type Pager struct {
	Page, Size, Total, Pages int        // Page is 1-based and clamped; Pages >= 1
	From, To                 int        // for display: 1-based, inclusive; 0,0 when Total == 0
	Prev, Next               *PageLink  // nil on the first / last page
	Links                    []PageLink // the window: first, last, current ±1, and gaps
	Sync                     string     // hx-sync for every link; "" for none
	Label                    string     // the nav's aria-label; "Pages" when empty
}

// pageParam reads a page key. Absent, not an integer, or <= 1 gives 0 -- "no
// page", the normal form, which is page 1 and is never written. Above maxPage
// gives maxPage.
func pageParam(v string) int {
	n, err := strconv.Atoi(v)
	if err != nil || n <= 1 {
		return 0
	}
	return min(n, maxPage)
}

// clampPage turns a requested page (0 = the first) into the real page and the
// 0-based, half-open bounds xs[lo:hi] of what that page shows.
func clampPage(total, page, size int) (p, lo, hi int) {
	pages := max(1, (total+size-1)/size)
	p = min(max(page, 1), pages)
	lo = min((p-1)*size, total)
	hi = min(lo+size, total)
	return p, lo, hi
}

// paginate clamps the page and builds the links through at, which gives the
// fragment and the page address of page n.
func paginate(total, page, size int, at func(n int) (get, href string)) Pager {
	p, lo, hi := clampPage(total, page, size)
	pg := Pager{Page: p, Size: size, Total: total, Pages: max(1, (total+size-1)/size)}
	if total > 0 {
		pg.From, pg.To = lo+1, hi
	}
	link := func(n int) PageLink {
		g, h := at(n)
		return PageLink{N: n, Get: g, Href: h, On: n == p}
	}
	if p > 1 {
		l := link(p - 1)
		pg.Prev = &l
	}
	if p < pg.Pages {
		l := link(p + 1)
		pg.Next = &l
	}
	// The window: page 1, the last page, the current ±1. One skipped page is
	// shown as its number (a "…" would hide no more than the number itself);
	// two or more become one gap.
	show := map[int]bool{1: true, pg.Pages: true, p - 1: true, p: true, p + 1: true}
	prev := 0
	for n := 1; n <= pg.Pages; n++ {
		if !show[n] && !(show[n-1] && show[n+1] && n-1 >= 1 && n+1 <= pg.Pages) {
			continue
		}
		if prev != 0 && n-prev > 1 {
			pg.Links = append(pg.Links, PageLink{Gap: true})
		}
		pg.Links = append(pg.Links, link(n))
		prev = n
	}
	return pg
}

// sliceFor returns the part of xs that p shows.
func sliceFor[T any](xs []T, p Pager) []T {
	if p.Total == 0 || p.From < 1 || p.To > len(xs) {
		return nil
	}
	return xs[p.From-1 : p.To]
}

// pageQ is "page=N" for a page worth writing (N > 1), else "".
func pageQ(key string, n int) string {
	if n <= 1 {
		return ""
	}
	return key + "=" + strconv.Itoa(n)
}

// productPageAt is the pager's addresses for a product tab: the fragment the
// sheet loads, and the page address of the same tab.
func productPageAt(id, slug, tab string) func(n int) (string, string) {
	return func(n int) (string, string) {
		q := pageQ("page", n)
		get := "products/" + url.PathEscape(id) + "?tab=" + tab
		href := productPath(slug, tab)
		if q != "" {
			get += "&" + q
			href += "?" + q
		}
		return get, href
	}
}

// listPageAt is the pager's addresses for a tenant-wide list page.
func listPageAt(view string) func(n int) (string, string) {
	return func(n int) (string, string) {
		if q := pageQ("page", n); q != "" {
			return "ui/bo/" + view + "?" + q, view + "?" + q
		}
		return "ui/bo/" + view, view
	}
}

// windowOf is xs[a:b] clipped to xs: a rollup group holds one kind of item,
// so two of its three slices are empty and clip to nothing.
func windowOf[T any](xs []T, a, b int) []T {
	if a >= len(xs) {
		return nil
	}
	return xs[a:min(b, len(xs))]
}
