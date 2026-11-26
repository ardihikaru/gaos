<!-- Round 1, 2026-11-26. Lens: security-review. Target: docs/designs/urls-and-board.md (first draft). Verbatim reviewer output. -->

## Paraphrase

### What this proposes
The design has two phases.

**Phase 1 (D-093)** gives every back-office view its own URL, and gives every product section one too: `/p/<slug>/<section>`. The slug is made once from the product name, stored on the Product vertex, and filled in for existing products at boot. There are eleven new page routes. The server tells htmx what to put in the address bar with `HX-Push-Url` or `HX-Replace-Url`, after comparing the canonical `Href` it built with the `HX-Current-URL` the client sent. htmx's history cache is switched off, so Back and Forward reload the page from the server. Navigation buttons become `<a href hx-get>` links. Signed out, a request for a page route gets a `200` sign-in page from the gate, which never calls the handler, so an existing slug and a missing one give the same body. After sign-in on a deep URL, the server answers `204` with `HX-Refresh: true`.

**Phase 2 (D-094)** adds a paged and filtered read model, `BoardPage`: 30 cards per column, at most 300. It adds a query string for filters, view, sort and group, five new read-only GET routes, and a server-side table. Live updates send only the columns whose FNV hash changed. On sisflow, the board's document is reloaded in place instead of the board being rebuilt. The Board tab also stops copying the event stream and stops rendering diagrams.

### How the existing system works, per this document
- Every click is an htmx GET into `#sink` that swaps the whole sheet out of band. Nothing is pushed to history, and only `GET /{$}` returns a full page.
- The gate sorts routes into public, bearer and back office. A signed-out back-office request gets `401` JSON, or the login sheet if it came from htmx.
- `sitePrefix` checks `X-Forwarded-Prefix`.
- The Board renders every card. On every `task` hint the whole sheet is rebuilt and replaced. Two decorators make the board quadratic.
- Two boards exist: sisflow, and the server board as the fallback.

I could paraphrase every section. No comprehension finding.

## Trust boundaries walked

| Boundary | Touched? | Authenticates | Authorises | Validates | Far side fails |
|---|---|---|---|---|---|
| Operator browser to dashboard | Yes: 11 page routes and 5 data routes | A signed session cookie, checked by `sessionFrom` (`dashboard/auth.go:366-378`) | Being signed in. There are no roles. | `crossOriginOK` for non-GET (`auth.go:570-587`), CSP (`auth.go:525`), `parseBoardQuery`, `sitePrefix` | The board stays as it was (§7) |
| Dashboard to ArcadeDB | Yes: the slug back-fill writes at boot | The database user | Its grants | `writeProduct`; the slug is limited to `[a-z0-9-]` (`store_mem.go:2462-2478`) | Boot fails (§7) |
| Public edge to VM | Indirectly: product names and search text go into request paths | Tunnel | — | — | — |
| Dashboard to gateway, gateway to provider, repo to host | No | — | — | — | — |

The rubric says `signedIn()` "accepts a cookie from any request". That is out of date for this checkout. `auth.go:361-378` checks a signed cookie with `a.verify`, so the design's "signed-in operator" identity can be implemented. It is still one role: every signed-in user is "the operator".

Abuse cases with no finding:
- **Open redirect:** `Location` and `HX-Push-Url` use a fixed path plus `sitePrefix`, and the post-login `HX-Refresh` is a constant.
- **Clickjacking:** `frame-ancestors 'none'` covers the page class.
- **CSRF on the table's move buttons:** they are `PUT`, so `crossOriginOK` applies.
- **XSS through `q` or the `<title>`:** both go through `html/template`.
- **Slug enumeration while signed out:** the gate answers before the handler runs.
- **htmx localStorage snapshot:** `historyCacheSize:0`.
- **IDOR:** this design does not create one, because there is a single tenant and no per-product authorisation exists today.

## Findings

### SEC-1 Product names and free-text search move into URLs, and so into the edge access log and browser history - Severity: Medium
- Component: the slug in page URLs (§3.1, §3.2), the `q` filter in the address (§4.2, §4.5), `PageTitle` (§3.3), and the §6 claim "Slugs are not secrets"
- Evidence: `docs/designs/urls-and-board.md:43`, `:92`, `:107`, `:207`, `:246`, `:347`. `deploy/traefik/traefik.yml:39` and `deploy/stacks/traefik.yml:54` turn on Traefik's default access log, which records the full request path and query. `pustaka/docs/product/decision-log.html:1788-1791` (D-085) treats a sample product's name as private material that must never appear in the repository.
- Attack path: Until now a product appeared in request lines only as its id (`/products/p-6?tab=board`). After this change, every navigation, deep-link load, Back/Forward reload and 300 ms search-box `HX-Replace-Url` writes the product's name and whatever the operator typed into the edge access log. The same data goes into browser history, history sync and tab titles. Anyone who can read the host's Traefik logs, or a synced browser profile, learns the product names and the search terms. That includes a token or customer identifier pasted into the search box. §6 says a slug is "visible only to a signed-in operator". That is not true once it is in the request line.
- Remediation:
  - Change §6 to say the slug and `q` are written to the edge access log and to browser history, and record that the operator accepts this, or decide otherwise.
  - Either keep `q` out of the URL (treat it as position state, like `shown`, §4.2:218), or configure the access log to drop or redact the query string for the dashboard router.
  - If product names are private under D-085, offer an id-based URL as the canonical form, or say why the slug is acceptable.
- Effort: small

### SEC-2 The new classPage exemption from the signed-out 401 test is not specified, so the one structural check of the gate can be weakened silently - Severity: Medium
- Component: `auth.go` gate, `pagePatterns`, `TestEveryRouteIsClassified`
- Evidence: `dashboard/auth_test.go:310-336` asserts that every non-public pattern answers `401` signed out, with no cookie and with a forged cookie, both through htmx and as JSON. `dashboard/auth_test.go:316-319` requires every non-public GET to be on `readOnlyGET`. The design changes this test only as "(the page class)" (`docs/designs/urls-and-board.md:425`). It adds only the §4.3 routes to `readOnlyGET` (`:230`), not the eleven page routes. It does not say what the gate does for a signed-out classPage request that carries `HX-Request` (`:143`).
- Attack path: The page class is the first non-public class that answers `200` signed out. If the test is loosened to "skip pagePatterns", nothing proves three things:
  1. A forged cookie on a page route gets the identical sign-in page.
  2. `pagePatterns` stays disjoint from `publicPatterns` and the bearer set. If a pattern lands in both, the gate lets the request through to the handler, which builds `buildBO` data for a signed-out caller.
  3. The handler is never reached signed out.

  A later edit that moves the handler call ahead of the session check, or adds a data-bearing route to `pagePatterns`, would pass the suite.
- Remediation: Spell out the replacement assertions in §10:
  - For every pattern in `pagePatterns`, signed out with no cookie, with a forged cookie, and with and without `HX-Request`, the response is `200`, `no-store`, and byte-equal to the signed-out `GET /{$}` body.
  - A counting handler stub sees zero calls.
  - `pagePatterns` is a subset of the registered patterns and is disjoint from `publicPatterns` and the bearer set.
  - All eleven page routes are on `readOnlyGET`, each with a reason.

  Also state the gate's answer for a signed-out classPage request that carries `HX-Request`.
- Effort: quick-win

### SEC-3 Bounds and allowlists for the new numeric and path parameters are incomplete - Severity: Medium
- Component: `parseBoardQuery` and the §4.3 routes
- Evidence: `docs/designs/urls-and-board.md:170-171` and `:191` cap only `Shown` (300). `:224-228` add `offset=N`, `limit=N` on the JSON route, `limit` on `…/board/cards`, the comma-separated `shown` and `rev` lists, and the path value `{col}`, which is echoed into `hx-swap-oob="beforeend:#col-<col>-cards"` (`:225`). §6 (`:345`) lists the allowlisted parameters, and `col`, `offset`, `limit`, `shown` and `rev` are not among them.
- Attack path: Any signed-in session can send `limit` or `offset` as huge or negative numbers, or `shown`/`rev` lists with arbitrarily many entries. So can a cross-site top-level navigation, because the cookie is `SameSite=Lax` (`dashboard/auth.go:493`) and every GET passes `crossOriginOK` (`auth.go:572-573`). This brings back the per-request cost that §1.2 measures: 823 ms and 480 MB at 10,000 cards. A negative offset is an out-of-range slice index; whether that panics depends on the handler code, which the design does not show. An unvalidated `{col}` becomes part of an htmx target selector in the response.
- Remediation: Say in §4.2 and §6 that:
  - `col` and the keys of `shown` and `rev` must be one of the nine column ids, and anything else is dropped (and `col` returns 404).
  - `offset` is clamped to `>= 0`.
  - `limit` is clamped to `1..300` on both JSON routes.
  - `shown` and `rev` hold at most nine entries.
  - `q` is cut to 80 runes, not bytes.

  Add a negative case to `TestBoardFiltersNarrowCardsAndCounts` or `TestTheBoardJSONPagesAndFilters`.
- Effort: quick-win

### SEC-4 The claim "no client-supplied URL reaches a response" contradicts "query string kept" and "passed unchanged" - Severity: Low
- Component: the `Location` of the id-to-slug redirect, and `Href` for the seven views other than Pull requests and Auto-fix
- Evidence: `docs/designs/urls-and-board.md:82` says "query string kept". `:69` says "the query string is passed to the view unchanged". `:113` defines `Href` but says how it is built only for the PR and Auto-fix views (`prHref`/`afHref`, `dashboard/prview.go:125`, `dashboard/autofix_view.go:672`, both of which build from `url.Values`). §6 (`:344`) states that every part is "a constant, a slug, or a query value written with `url.Values.Encode`".
- Attack path: If the implementer copies `r.URL.RawQuery` into `Location` or into `HX-Push-Url`/`HX-Replace-Url`, unparsed client bytes reach a response header and the address bar. The path is fixed, so this is not an open redirect. It does make the §6 claim false, and it lets a crafted link plant arbitrary query text in the operator's history.
- Remediation: State that `Location` and every `Href` are rebuilt from parsed, allowlisted values (`url.Values` re-encoded, unknown keys dropped), never from `RawQuery`. Assert it in `TestTheProductIdRedirectsToTheSlug` with an unknown parameter that must not survive.
- Effort: quick-win

### SEC-5 §3.7 presents the cross-origin check as a control on page routes, but it does nothing for GET - Severity: Low
- Component: §3.7, §6
- Evidence: `docs/designs/urls-and-board.md:144`. `dashboard/auth.go:571-573` returns `true` for every GET, HEAD and OPTIONS.
- Attack path: None directly, because the page routes are read-only. A reader of the design may believe page routes and the §4.3 data routes are protected against cross-site requests. They are reachable by any cross-site top-level navigation with the Lax cookie, which is what makes SEC-3's cost amplification reachable from outside a session the operator controls.
- Remediation: Rewrite the sentence to say that for these GET routes only the CSP and the session apply, and that cross-site top-level GETs carry the session by design.
- Effort: quick-win

### SEC-6 A boot-time database write becomes a hard dependency for starting the dashboard - Severity: Low
- Component: `backfillSlugs` in `NewArcadeStoreFor`
- Evidence: `docs/designs/urls-and-board.md:96` and `:358`.
- Attack path: This is an availability risk, not an attacker path. One failed write takes the whole back office down on restart: a transient database error, a grant change for the dashboard's database user, or a single product whose write is rejected. That includes the sign-in page. The write is only needed once per product.
- Remediation: Either assign missing slugs in memory, serve them, and retry the write, logging the failure without the product name; or state explicitly that the operator accepts a back office that refuses to start in this case. Add the failure row to the QA register's negative cases.
- Effort: small

Counts: Critical=0 High=0 Medium=3 Low=3
