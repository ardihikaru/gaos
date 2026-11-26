package main

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The tests of D-094: real URLs for the back office (urls.go,
// docs/designs/urls-and-board.md §3, §10.1).

// get is a request as a browser makes one for a page: no HX-Request.
func pageGet(t *testing.T, s *server, path string, hdr map[string]string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("GET", path, nil)
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, r)
	return w
}

// hx is an htmx request with the address the browser says it is on.
func hx(t *testing.T, s *server, method, path, current string, hdr map[string]string, form url.Values, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	body := strings.NewReader("")
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	r := httptest.NewRequest(method, path, body)
	if form != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	r.Header.Set("HX-Request", "true")
	if current != "" {
		r.Header.Set("HX-Current-URL", current)
	}
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, r)
	return w
}

func slugOfProduct(t *testing.T, st Store, id string) string {
	t.Helper()
	ps, _ := st.Products(context.Background())
	for _, p := range ps {
		if p.ID == id {
			return p.Slug
		}
	}
	t.Fatalf("no product %s", id)
	return ""
}

func TestASlugIsAssignedOnceAndIsUnique(t *testing.T) {
	st := NewMemStore(1)
	ctx := context.Background()
	slugs := map[string]string{}
	for _, p := range st.products {
		if !validSlug(p.Slug) {
			t.Fatalf("fixture product %s has the slug %q, which is not a valid slug", p.ID, p.Slug)
		}
		if other, dup := slugs[p.Slug]; dup {
			t.Fatalf("products %s and %s share the slug %q", other, p.ID, p.Slug)
		}
		slugs[p.Slug] = p.ID
	}
	if got := slugOfProduct(t, st, "p-1"); got != "laundry-membership" {
		t.Fatalf("the slug of Laundry Membership is %q, want laundry-membership", got)
	}

	mk := func(name string) Product {
		t.Helper()
		res, err := st.CreateProduct(ctx, ProductDraft{Name: name})
		if err != nil {
			t.Fatalf("CreateProduct(%q): %v", name, err)
		}
		ps, _ := st.Products(ctx)
		for _, p := range ps {
			if p.ID == res.Master.ID {
				return p
			}
		}
		t.Fatalf("the product created for %q is not in the store", name)
		return Product{}
	}
	a, b := mk("Route Planner"), mk("Route Planner")
	if a.Slug != "route-planner" || b.Slug != "route-planner-2" {
		t.Errorf("two products named alike have the slugs %q and %q, want route-planner and route-planner-2", a.Slug, b.Slug)
	}
	if c := mk("日本語"); c.Slug != c.ID {
		t.Errorf("a name with nothing a URL can carry has the slug %q, want the product's id %s", c.Slug, c.ID)
	}
	// A name that slugs to another product's id must not shadow it: /p/p-1/…
	// always means product p-1.
	if d := mk("P 1"); d.Slug == "p-1" || !validSlug(d.Slug) {
		t.Errorf("a product named \"P 1\" has the slug %q; it must not take the id of product p-1", d.Slug)
	}
	long := mk(strings.Repeat("abcdefghij ", 12))
	if len(long.Slug) > 48 || strings.HasSuffix(long.Slug, "-") {
		t.Errorf("a long name has the slug %q (%d bytes); want at most 48 and no trailing dash", long.Slug, len(long.Slug))
	}

	// The back-fill leaves a valid slug alone, replaces a malformed one and a
	// duplicate, gives the bare slug to the lower id, and is deterministic.
	in := []Product{
		{ID: "p-10", Name: "Twin", Slug: "twin"},
		{ID: "p-9", Name: "Twin", Slug: "twin"},
		{ID: "p-3", Name: "Kept", Slug: "kept-as-is"},
		{ID: "p-4", Name: "Bad Slug", Slug: "Bad/Slug?x"},
		{ID: "p-5", Name: "Unset"},
		{ID: "p-6", Name: "Squatter", Slug: "p-3"},
	}
	run := func() []Product {
		ps := append([]Product(nil), in...)
		backfillSlugs(ps)
		return ps
	}
	got := run()
	want := map[string]string{"p-9": "twin", "p-10": "twin-2", "p-3": "kept-as-is", "p-4": "bad-slug", "p-5": "unset", "p-6": "squatter"}
	for _, p := range got {
		if p.Slug != want[p.ID] {
			t.Errorf("after the back-fill %s has the slug %q, want %q", p.ID, p.Slug, want[p.ID])
		}
	}
	again := run()
	for i := range got {
		if got[i].Slug != again[i].Slug {
			t.Errorf("the back-fill is not deterministic: %s got %q, then %q", got[i].ID, got[i].Slug, again[i].Slug)
		}
	}
	if changed := backfillSlugs(got); len(changed) != 0 {
		t.Errorf("a second back-fill changed %d product(s); it must leave valid slugs alone", len(changed))
	}

	// AssignSlug is the back-fill as a mutation: the slug, and its event (BR-02).
	st.mu.Lock()
	st.products[0].Slug = ""
	st.mu.Unlock()
	before, _ := st.Events(ctx)
	res, err := st.AssignSlug(ctx, "p-1")
	if err != nil {
		t.Fatalf("AssignSlug: %v", err)
	}
	after, _ := st.Events(ctx)
	if slugOfProduct(t, st, "p-1") != "laundry-membership" || len(after) != len(before)+1 || after[len(after)-1].Type != RecordUpdated || res.Master == nil || res.Master.ID != "p-1" {
		t.Errorf("AssignSlug left the slug %q, %d new event(s), and a result that does not name the product's record; want laundry-membership, one RecordUpdated, the record",
			slugOfProduct(t, st, "p-1"), len(after)-len(before))
	}
}

func TestTheProductIdRedirectsToTheSlug(t *testing.T) {
	s, st := newTestServer(t)
	cs := boCookies()
	slug := slugOfProduct(t, st, "p-1")

	for _, c := range []struct{ prefix, path, want string }{
		{"", "/p/p-1/stories?junk=1&view=table", "/p/" + slug + "/stories"},
		{"/app", "/p/p-1/board", "/app/p/" + slug + "/board"},
		{"", "/p/p-1", "/p/" + slug + "/brd"},
		{"/app", "/p/" + slug, "/app/p/" + slug + "/brd"},
	} {
		hdr := map[string]string{}
		if c.prefix != "" {
			hdr["X-Forwarded-Prefix"] = c.prefix
		}
		w := pageGet(t, s, c.path, hdr, cs...)
		if w.Code != http.StatusFound || w.Header().Get("Location") != c.want {
			t.Errorf("GET %s (prefix %q) answered %d with Location %q, want 302 to %q: the id form redirects to the slug, and nothing of the query it does not know rides along",
				c.path, c.prefix, w.Code, w.Header().Get("Location"), c.want)
		}
	}
	// An unknown section is refused before any redirect is written.
	if w := pageGet(t, s, "/p/p-1/nope", nil, cs...); w.Code != http.StatusNotFound || w.Header().Get("Location") != "" {
		t.Errorf("GET /p/p-1/nope answered %d with Location %q, want 404 and no redirect", w.Code, w.Header().Get("Location"))
	}
}

var boMainTag = regexp.MustCompile(`<div class="bo-main" id="boMain"[^>]*>`)

func TestADeepURLRendersThePage(t *testing.T) {
	s, st := newTestServer(t)
	cs := boCookies()
	slug := slugOfProduct(t, st, "p-1")

	check := func(path, title string, attrs ...string) {
		t.Helper()
		w := pageGet(t, s, path, nil, cs...)
		body := w.Body.String()
		if w.Code != 200 || !strings.HasPrefix(body, "<!DOCTYPE html>") {
			t.Errorf("GET %s answered %d and a body that does not start a document; want 200 and the whole page", path, w.Code)
			return
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("GET %s sends Cache-Control %q, want no-store", path, w.Header().Get("Cache-Control"))
		}
		if !strings.Contains(body, "<title>"+title+"</title>") {
			t.Errorf("GET %s does not carry the title %q", path, title)
		}
		tag := boMainTag.FindString(body)
		for _, a := range attrs {
			if !strings.Contains(tag, a) {
				t.Errorf("GET %s: the back-office pane is %q, want it to carry %s", path, tag, a)
			}
		}
	}
	for _, v := range pageViews {
		check("/"+v.ID, v.Label+" · GAOS", `data-view="`+v.ID+`"`)
	}
	for _, sec := range productSections {
		check("/p/"+slug+"/"+sec.ID, sec.Label+" · Laundry Membership · GAOS", `data-view="products"`, `data-product="p-1"`, `data-tab="`+sec.ID+`"`)
	}
	if n := len(pageViews) + 2; len(pagePatterns) != n || n != 11 {
		t.Errorf("there are %d page patterns, want eleven: nine views, a product section and the redirect", len(pagePatterns))
	}
	if w := pageGet(t, s, "/p/"+slug, nil, cs...); w.Code != http.StatusFound {
		t.Errorf("GET /p/%s answered %d, want 302 to its first section", slug, w.Code)
	}
	if w := pageGet(t, s, "/p/no-such-product", nil, cs...); w.Code != http.StatusNotFound {
		t.Errorf("GET /p/no-such-product answered %d, want 404", w.Code)
	}
	// The inbox keeps its product filter, and only a product that exists.
	if w := pageGet(t, s, "/inbox?product=p-1", nil, cs...); !strings.Contains(boMainTag.FindString(w.Body.String()), `data-filter="p-1"`) {
		t.Errorf("GET /inbox?product=p-1 does not render the inbox narrowed to p-1")
	}
}

func TestASignedOutDeepURLShowsOnlyTheLogin(t *testing.T) {
	s, st := newTestServer(t)
	slug := slugOfProduct(t, st, "p-1")
	home := pageGet(t, s, "/", nil).Body.String()
	if !strings.Contains(home, "Sign in to the back office") {
		t.Fatalf("the signed-out home page does not show the sign-in form")
	}
	real := pageGet(t, s, "/p/"+slug+"/board", nil)
	missing := pageGet(t, s, "/p/no-such-product/board", nil)
	if real.Code != http.StatusUnauthorized || missing.Code != http.StatusUnauthorized {
		t.Fatalf("a signed-out deep URL answered %d (existing product) and %d (missing product), want 401 for both (BR-55)", real.Code, missing.Code)
	}
	if real.Body.String() != missing.Body.String() {
		t.Errorf("the signed-out page differs between an existing product and a missing one: it says which products exist")
	}
	if real.Body.String() != home {
		t.Errorf("the signed-out page of a deep URL is not the signed-out home page")
	}
	if strings.Contains(real.Body.String(), "Laundry Membership") || strings.Contains(real.Body.String(), slug) {
		t.Errorf("the signed-out page names the product")
	}
	if real.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("the signed-out page sends Cache-Control %q, want no-store", real.Header().Get("Cache-Control"))
	}
	// htmx still gets the sign-in sheet, as every back-office route gives it.
	if w := do(t, s, "GET", "/p/"+slug+"/board", nil); w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), `id="loginSheet"`) {
		t.Errorf("an htmx request to a deep URL signed out answered %d without the sign-in sheet, want 401 with it", w.Code)
	}
}

func TestSignInOnADeepURLRefreshesInPlace(t *testing.T) {
	s, _ := newTestServer(t)
	form := url.Values{"email": {testEmail}, "password": {testPassword}, "tenant": {"loop-eng"}}

	deep := hx(t, s, "POST", "/auth/login", "https://gaos.example/app/p/laundry-membership/board", map[string]string{"X-Forwarded-Prefix": "/app"}, form)
	if deep.Code != http.StatusNoContent || deep.Header().Get("HX-Refresh") != "true" || sessionCookie(deep) == nil {
		t.Errorf("signing in on a deep URL answered %d, HX-Refresh %q, session cookie set: %v; want 204, true, set",
			deep.Code, deep.Header().Get("HX-Refresh"), sessionCookie(deep) != nil)
	}
	if strings.Contains(deep.Header().Get("HX-Push-Url")+deep.Header().Get("HX-Redirect")+deep.Header().Get("Location"), "/") {
		t.Errorf("signing in on a deep URL wrote an address into a response header; the reload is the browser's own URL")
	}
	for name, current := range map[string]string{
		"on the home page":    "https://gaos.example/app/",
		"with no current URL": "",
	} {
		w := hx(t, s, "POST", "/auth/login", current, map[string]string{"X-Forwarded-Prefix": "/app"}, form)
		if w.Code != 200 || w.Header().Get("HX-Refresh") != "" || !strings.Contains(w.Body.String(), `id="boSheet"`) || sessionCookie(w) == nil {
			t.Errorf("signing in %s answered %d, HX-Refresh %q; want 200, no refresh, and the back-office sheet", name, w.Code, w.Header().Get("HX-Refresh"))
		}
	}
	// A wrong password on a deep URL is still refused, with no refresh.
	bad := hx(t, s, "POST", "/auth/login", "https://gaos.example/p/x/board", nil, url.Values{"email": {testEmail}, "password": {"wrong"}, "tenant": {"loop-eng"}})
	if bad.Code == http.StatusNoContent || bad.Header().Get("HX-Refresh") != "" || sessionCookie(bad) != nil {
		t.Errorf("a wrong password on a deep URL answered %d with HX-Refresh %q; it must be refused", bad.Code, bad.Header().Get("HX-Refresh"))
	}
}

func TestNavigationPushesReplacesOrLeavesTheAddress(t *testing.T) {
	s, st := newTestServer(t)
	cs := boCookies()
	slug := slugOfProduct(t, st, "p-1")
	const host = "https://gaos.example"

	type want struct{ push, replace string }
	headers := func(w *httptest.ResponseRecorder) want {
		return want{w.Header().Get("HX-Push-Url"), w.Header().Get("HX-Replace-Url")}
	}
	for _, prefix := range []string{"", "/app"} {
		hdr := map[string]string{}
		if prefix != "" {
			hdr["X-Forwarded-Prefix"] = prefix
		}
		at := func(path string) string { return host + prefix + path }
		cases := []struct {
			name, method, path, current string
			want                        want
		}{
			{"a navigation: the path differs", "GET", "/products/p-1?tab=board", at("/"), want{push: prefix + "/p/" + slug + "/board"}},
			{"a navigation to a global view", "GET", "/ui/bo/stories", at("/p/" + slug + "/board"), want{push: prefix + "/stories"}},
			{"a navigation home", "GET", "/products", at("/stories"), want{push: prefix + "/"}},
			{"the same path, another query: a filter", "GET", "/ui/bo/prs?state=merged", at("/prs"), want{replace: prefix + "/prs?state=merged"}},
			{"the same path and query", "GET", "/ui/bo/prs?state=merged", at("/prs?state=merged"), want{}},
			{"the same query in another order and escaping", "GET", "/ui/bo/prs?kind=review&state=merged&open=fix%3A1", at("/prs?open=fix:1&state=merged&kind=review"), want{}},
			{"a default left out is the same address", "GET", "/ui/bo/prs?state=open", at("/prs"), want{}},
			{"a refresh in place", "GET", "/products/p-1?tab=board", at("/p/" + slug + "/board"), want{}},
			{"an unknown parameter is not written back", "GET", "/ui/bo/inbox?product=p-1&junk=1", at("/inbox"), want{replace: prefix + "/inbox?product=p-1"}},
			{"an inbox filter that names no product is dropped", "GET", "/ui/bo/inbox?product=nope", at("/inbox"), want{}},
			{"a mutation that stays where it is", "POST", "/products/p-1/inject?tab=stories", at("/p/" + slug + "/stories?x=1"), want{}},
			{"a mutation that opens another view", "POST", "/products/p-1/inject?tab=stories", at("/p/" + slug + "/brd"), want{push: prefix + "/p/" + slug + "/stories"}},
			{"a product that does not exist", "GET", "/products/nope?tab=board", at("/"), want{}},
			// D-095: the Board's view is part of its address; its window is not.
			{"the Board as a table", "GET", "/products/p-1?tab=board&view=table&sort=id&shown=done:60&more=done", at("/p/" + slug + "/board"), want{replace: prefix + "/p/" + slug + "/board?sort=id&view=table"}},
			{"the Board, a filter nobody offers", "GET", "/products/p-1?tab=board&role=nobody", at("/p/" + slug + "/board"), want{}},
			{"onto the Board with a view", "GET", "/products/p-1?tab=board&view=table", at("/p/" + slug + "/stories"), want{push: prefix + "/p/" + slug + "/board?view=table"}},
		}
		for _, c := range cases {
			w := hx(t, s, c.method, c.path, c.current, hdr, nil, cs...)
			if got := headers(w); got != c.want {
				t.Errorf("prefix %q, %s: %s %s from %s set HX-Push-Url %q and HX-Replace-Url %q, want %q and %q",
					prefix, c.name, c.method, c.path, c.current, got.push, got.replace, c.want.push, c.want.replace)
			}
		}
		// A request live.js made on its own never moves the address, even when
		// the operator has since gone somewhere else.
		refresh := map[string]string{refreshHeader: "1"}
		for k, v := range hdr {
			refresh[k] = v
		}
		if got := headers(hx(t, s, "GET", "/products/p-1?tab=board", at("/stories"), refresh, nil, cs...)); got != (want{}) {
			t.Errorf("prefix %q: a background refresh set HX-Push-Url %q and HX-Replace-Url %q; it must set neither", prefix, got.push, got.replace)
		}
	}
	// Without HX-Request there is no address header at all.
	if w := pageGet(t, s, "/products/p-1?tab=board", nil, cs...); w.Header().Get("HX-Push-Url") != "" {
		t.Errorf("a request that is not htmx's was given HX-Push-Url %q", w.Header().Get("HX-Push-Url"))
	}

	// Every response that carries the sheet starts with a title, and it is
	// never empty -- not after sign-out either.
	titled := regexp.MustCompile(`^<title>[^<]+</title>`)
	for name, w := range map[string]*httptest.ResponseRecorder{
		"a product tab":   hx(t, s, "GET", "/products/p-1?tab=board", host+"/", nil, nil, cs...),
		"a global view":   hx(t, s, "GET", "/ui/bo/agents", host+"/", nil, nil, cs...),
		"the back office": hx(t, s, "GET", "/ui/bo", host+"/stories", nil, nil, cs...),
		"a mutation":      hx(t, s, "POST", "/products/p-1/inject", host+"/", nil, nil, cs...),
		"sign-out":        hx(t, s, "POST", "/auth/logout", host+"/stories", nil, nil, cs...),
	} {
		if !titled.MatchString(strings.TrimSpace(w.Body.String())) {
			t.Errorf("the response to %s does not start with a non-empty <title>; it starts %q", name, clip(strings.TrimSpace(w.Body.String()), 60))
		}
	}
	if w := hx(t, s, "GET", "/products/p-1?tab=board", host+"/", nil, nil, cs...); !strings.HasPrefix(strings.TrimSpace(w.Body.String()), "<title>Board · Laundry Membership · GAOS</title>") {
		t.Errorf("the Board tab's response does not carry its title first")
	}
	// The sensitive views are no-store from every door that renders them.
	for _, path := range []string{"/ui/bo/archive", "/ui/bo/integrations", "/ui/bo/autofix", "/ui/bo/prs", "/ui/bo?view=prs"} {
		if w := hx(t, s, "GET", path, host+"/", nil, nil, cs...); w.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("GET %s sends Cache-Control %q, want no-store", path, w.Header().Get("Cache-Control"))
		}
	}
}

// buildBO is called from one door. Every handler that renders the sheet for
// htmx goes through sheet(), which also sets the address; a handler that
// called buildBO itself would render a view the address bar never learns of.
func TestOnlyTheAllowedFunctionsCallBuildBO(t *testing.T) {
	allowed := map[string]bool{"sheet": true, "handleIndex": true, "handlePage": true, "handleProductPage": true, "pageNotFound": true, "handleReadiness": true}
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool { return !strings.HasSuffix(fi.Name(), "_test.go") }, 0)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, pkg := range pkgs {
		for _, f := range pkg.Files {
			for _, d := range f.Decls {
				fn, ok := d.(*ast.FuncDecl)
				if !ok || fn.Body == nil || fn.Name.Name == "buildBO" {
					continue
				}
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "buildBO" {
						seen[fn.Name.Name] = true
						if !allowed[fn.Name.Name] {
							t.Errorf("%s: %s calls buildBO directly; a handler that renders the sheet must call sheet(), which sets the address",
								fset.Position(call.Pos()), fn.Name.Name)
						}
					}
					return true
				})
			}
		}
	}
	var missing []string
	for name := range allowed {
		if !seen[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("the allowed list names %v, which no longer call buildBO; shorten the list", missing)
	}
}

var htmxLink = regexp.MustCompile(`<a\b[^>]*\bhx-get="[^"]*"[^>]*>`)
var hrefAttr = regexp.MustCompile(`\bhref="([^"]*)"`)

func TestEveryHtmxLinkHasAPageHref(t *testing.T) {
	f := newAFFixture(t)
	s, cs := f.srv, boCookies()
	paths := []string{"/", "/ui/bo", "/products", "/products/p-1", "/ui/bo/autofix", "/ui/bo/autofix?open=" + f.pr1, "/ui/bo/autofix?state=merged&difficulty=easy", "/ui/bo/prs", "/ui/bo/prs?state=merged"}
	for _, v := range pageViews {
		paths = append(paths, "/ui/bo/"+v.ID)
	}
	for _, sec := range productSections {
		paths = append(paths, "/products/p-1?tab="+sec.ID)
	}
	links := 0
	for _, path := range paths {
		body := do(t, s, "GET", path, nil, cs...).Body.String()
		for _, tag := range htmxLink.FindAllString(body, -1) {
			links++
			m := hrefAttr.FindStringSubmatch(tag)
			if m == nil {
				t.Errorf("on %s the link %s has hx-get and no href; a new tab has nowhere to go", path, clip(tag, 120))
				continue
			}
			if strings.HasPrefix(m[1], "ui/") || strings.HasPrefix(m[1], "products") || strings.HasPrefix(m[1], "/") {
				t.Errorf("on %s a link's href is %q, a fragment route or an absolute path; it must be the page address of what hx-get loads", path, m[1])
			}
		}
	}
	if links < 40 {
		t.Fatalf("found %d <a hx-get> links across the views; the navigation should be made of them", links)
	}
	// The navigation is links, not buttons: no button swaps a whole view in.
	nav := do(t, s, "GET", "/products/p-1?tab=board", nil, cs...).Body.String()
	if strings.Contains(nav, `<button hx-get="products/p-1?tab=`) || !strings.Contains(nav, `href="p/laundry-membership/stories" hx-get="products/p-1?tab=stories"`) {
		t.Errorf("the product menu is not made of links that carry the page address and the fragment address")
	}
}

func TestAnUnknownProductOrSectionIsNotFound(t *testing.T) {
	s, st := newTestServer(t)
	cs := boCookies()
	slug := slugOfProduct(t, st, "p-1")
	for path, toast := range map[string]string{
		"/p/no-such-product/board": "No such product.",
		"/p/no-such-product":       "No such product.",
		"/p/" + slug + "/nope":     "No such section.",
	} {
		w := pageGet(t, s, path, nil, cs...)
		body := w.Body.String()
		if w.Code != http.StatusNotFound || !strings.HasPrefix(body, "<!DOCTYPE html>") || !strings.Contains(body, toast) || !strings.Contains(body, `data-view="products"`) {
			t.Errorf("GET %s answered %d; want 404, the whole page with the product list, and the toast %q", path, w.Code, toast)
		}
	}
	if w := pageGet(t, s, "/no/such/page", nil, cs...); w.Code != http.StatusNotFound {
		t.Errorf("a path with no route answered %d, want 404: there is no catch-all", w.Code)
	}
}

func TestTheBrowserKeepsNoHistoryCache(t *testing.T) {
	b, err := os.ReadFile("templates/page.html")
	if err != nil {
		t.Fatal(err)
	}
	metas := regexp.MustCompile(`<meta name="htmx-config" content='([^']*)'>`).FindAllStringSubmatch(string(b), -1)
	if len(metas) != 1 {
		t.Fatalf("templates/page.html has %d htmx-config elements, want exactly one: htmx reads only the first", len(metas))
	}
	for _, key := range []string{`"historyCacheSize":0`, `"refreshOnHistoryMiss":true`, `"responseHandling":`} {
		if !strings.Contains(metas[0][1], key) {
			t.Errorf("the htmx-config element lacks %s: without it htmx keeps back-office HTML in localStorage, or Back shows a stale snapshot", key)
		}
	}
}

// The back-fill against the real graph (D-094): a product stored before slugs
// existed, or with a slug someone damaged by hand, gets one at boot -- written
// with its RecordUpdated event, once -- and a second boot finds it there and
// writes nothing. ArcadeDB only; it skips without GAOS_ARCADE_TEST_URL.
func TestTheSlugBackfillIsWrittenOnce(t *testing.T) {
	arc := arcadeTestTarget(t)
	ctx := context.Background()
	tenant := "conf-slug"

	first := newArcadeTestStore(t, arc, tenant)
	if _, err := first.CreateProduct(ctx, ProductDraft{Name: "Ledger Rewrite"}); err != nil {
		t.Fatalf("create product: %v", err)
	}
	// What an upgrade finds: one product with no slug, one with a slug no URL
	// may carry, and one that took another product's.
	for id, slug := range map[string]any{"p-1": nil, "p-2": "Bad/Slug?x", "p-3": "ledger-rewrite"} {
		if err := arc.Command(ctx, `UPDATE Product SET slug = :s WHERE tenantId = :t AND id = :id`,
			map[string]any{"s": slug, "t": tenant, "id": id}, nil); err != nil {
			t.Fatalf("damage the stored slug of %s: %v", id, err)
		}
	}
	eventsBefore, _ := first.Events(ctx)

	second, err := NewArcadeStoreFor(ctx, arc, 1, tenant)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	slugs := func(st Store) map[string]string {
		out := map[string]string{}
		ps, _ := st.Products(ctx)
		for _, p := range ps {
			out[p.ID] = p.Slug
		}
		return out
	}
	got := slugs(second)
	ps, _ := second.Products(ctx)
	var ledger string
	for _, p := range ps {
		if p.Name == "Ledger Rewrite" {
			ledger = p.ID
		}
	}
	// p-3 held the slug of the newer product; the lower id keeps a slug, so the
	// newer product is the one that is assigned again.
	if got["p-1"] != "laundry-membership" || got["p-2"] != "invoice-engine" || got["p-3"] != "ledger-rewrite" || got[ledger] != "ledger-rewrite-2" {
		t.Errorf("after the boot the slugs are p-1 %q, p-2 %q, p-3 %q and %s %q; want laundry-membership, invoice-engine, ledger-rewrite (kept: the lower id) and ledger-rewrite-2",
			got["p-1"], got["p-2"], got["p-3"], ledger, got[ledger])
	}
	eventsAfter, _ := second.Events(ctx)
	assigned := 0
	for _, e := range eventsAfter[len(eventsBefore):] {
		if e.Type == RecordUpdated && strings.HasSuffix(e.Text, ": slug assigned") {
			assigned++
		}
	}
	if assigned != 3 || len(eventsAfter) != len(eventsBefore)+3 {
		t.Errorf("the back-fill wrote %d event(s), %d of them \"slug assigned\"; want three RecordUpdated, one per product it changed (BR-02)",
			len(eventsAfter)-len(eventsBefore), assigned)
	}

	// A third boot reads what the second wrote: the same slugs, no new event.
	third, err := NewArcadeStoreFor(ctx, arc, 1, tenant)
	if err != nil {
		t.Fatalf("reopen again: %v", err)
	}
	for id, want := range got {
		if s := slugs(third)[id]; s != want {
			t.Errorf("after another boot %s has the slug %q, want %q: the back-fill did not reach the graph", id, s, want)
		}
	}
	if eventsThird, _ := third.Events(ctx); len(eventsThird) != len(eventsAfter) {
		t.Errorf("a boot with nothing to back-fill wrote %d event(s); it must write none", len(eventsThird)-len(eventsAfter))
	}
}

// TC-756: the tenant-wide list pages are paged by five, by item (D-097). The
// page in the address is the clamped one, built from the page alone.
func TestTheListPagesPageByFive(t *testing.T) {
	srv, _ := newTestServer(t)
	at := func(view string) map[string]string {
		return map[string]string{"HX-Current-URL": "https://gaos.example/" + view}
	}
	stories := func(b string) int { return strings.Count(b, `<div class="story `) }
	totalOf := func(b string) int {
		if m := regexp.MustCompile(`Showing \d+–\d+ of (\d+)`).FindStringSubmatch(b); m != nil {
			n, _ := strconv.Atoi(m[1])
			return n
		}
		if m := regexp.MustCompile(`Showing (\d+)</p>`).FindStringSubmatch(b); m != nil {
			n, _ := strconv.Atoi(m[1])
			return n
		}
		return -1
	}
	p1 := boardHX(t, srv, "GET", "/ui/bo/stories", at("stories")).Body.String()
	n := totalOf(p1)
	if n <= 5 || stories(p1) != 5 {
		t.Fatalf("/stories page 1: %d card(s) of %d; the seed must hold more than 5 stories, and a page shows 5", stories(p1), n)
	}
	last := (n + 4) / 5
	far := boardHX(t, srv, "GET", "/ui/bo/stories?page=99", at("stories"))
	if got, want := stories(far.Body.String()), n-(last-1)*5; got != want {
		t.Errorf("/stories?page=99 shows %d card(s); want the last page's %d", got, want)
	}
	if u := far.Header().Get("HX-Replace-Url"); !strings.HasSuffix(u, "/stories?page="+strconv.Itoa(last)) {
		t.Errorf("/stories?page=99 wrote the address %q; want the clamped page %d", u, last)
	}
	// A heading only with an item on the page: every <h4> of a group has a card under it.
	for _, blk := range strings.Split(p1, `<div class="block">`)[1:] {
		if strings.Contains(blk, "Open record") && !strings.Contains(blk, `<div class="story `) {
			t.Errorf("a product heading is shown with no story of it on the page")
		}
	}
	// Every pager link is a page address in href and a fragment in hx-get.
	for _, m := range regexp.MustCompile(`<a class="pager-[a-z]+" href="([^"]+)" hx-get="([^"]+)"`).FindAllStringSubmatch(p1, -1) {
		if strings.HasPrefix(m[1], "ui/") || !strings.HasPrefix(m[1], "stories") || !strings.HasPrefix(m[2], "ui/bo/stories") {
			t.Errorf("pager link href=%q hx-get=%q; want a page address and a fragment", m[1], m[2])
		}
	}
	// The address is built from the page alone: a stray q= never reaches it.
	sq := boardHX(t, srv, "GET", "/ui/bo/stories?page=2&q=secret", at("stories"))
	if u := sq.Header().Get("HX-Replace-Url"); !strings.HasSuffix(u, "/stories?page=2") || strings.Contains(u, "secret") {
		t.Errorf("/stories?page=2&q=secret wrote the address %q; want stories?page=2 and nothing else", u)
	}
	ag := boardHX(t, srv, "GET", "/ui/bo/agents?page=3", at("agents")).Body.String()
	if n := strings.Count(ag, `<div class="proj" style="padding:8px 0">`); n != len(agents)-10 || !strings.Contains(ag, "of "+strconv.Itoa(len(agents))) {
		t.Errorf("/agents?page=3 shows %d agent(s); want %d of %d", n, len(agents)-10, len(agents))
	}
}
