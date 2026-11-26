<!-- Round 2, 2026-11-26. Lens: security-review. Target: docs/designs/urls-and-board.md (rev 2). Verbatim reviewer output. -->

## Paraphrase

### What this proposes

There are two phases, and each can be approved on its own.

**Phase 1 (D-093)** gives every back-office view and product section its own URL:
- Nine global view paths, `/p/{ref}/{section}` for a product section, and `/p/{ref}` which redirects to the requirements (`brd`) section.
- Products get a stored slug, assigned once from the name in `CreateProduct`. Existing products are back-filled at boot. On ArcadeDB the back-fill is written as a `RecordUpdated` event.
- A single `sheet()` function wraps `buildBO` and sets `HX-Push-Url` or `HX-Replace-Url`. It builds them from a server-computed `Href`: the path is fixed strings plus the slug, and the query is re-encoded from parameters the view has already parsed.
- htmx's history cache is turned off.
- Navigation buttons become `<a href hx-get>`, and a capture-phase listener lets Ctrl-click and middle-click open a real new tab.
- When signed out, the new page patterns still answer `401`, but a request without `HX-Request` gets the sign-in page instead of JSON. That page is built in the gate, before any handler runs.
- After sign-in on a deep URL, the server answers `204` with a constant `HX-Refresh: true`.

**Phase 2 (D-094)** pages the Board:
- 30 cards per column, columns 300px wide, and filters (role, story, label, workflow, "needs you", hide empty) that live in the URL.
- A search text `q` and a set of window parameters (`shown`, `rows`, `more`, `rev`, `surface`) that are request-only and "never written to the address".
- A table view.
- Two new read-only GET routes, `board/live` (HTML) and `board/cards` (JSON), and a `paged=1` mode on the existing board JSON.
- Live updates replace only the columns whose FNV hash changed, and cheaper server work per tab (`EventCount`, diagrams rendered only on their own tab).

### How the existing system works, per this document

- Every back-office click is an htmx GET to a fragment route. The whole sheet comes back out-of-band, the address never changes, and only `GET /{$}` returns a full page.
- The gate in `auth.go` classifies each pattern as public, bearer or back-office. Back-office routes without a verified session get `401`: the sign-in sheet for htmx requests, JSON otherwise. The CSP applies to every non-bearer route. The cross-origin check refuses only non-GET requests. The session cookie is `SameSite=Lax`.
- `sitePrefix` validates `X-Forwarded-Prefix`.
- The Board renders every card, two decorators are quadratic, and every live hint re-renders and replaces the whole sheet.
- Traefik writes an access log at the edge.

Every section could be paraphrased, so there is no comprehension-level finding.

## Trust boundaries touched

| Boundary | Authenticates | Authorises | Validates | Far side fails |
|---|---|---|---|---|
| Browser → dashboard (new page routes, `board/live`, `board/cards`, `paged=1`) | Signed, verified session cookie (`auth.go:366-381`). The rubric says `signedIn()` "accepts a cookie from any request". That is out of date for this checkout: `sessionFrom` verifies a signature. | Any signed-in operator, no per-product authorisation. That matches today and the design adds no IDOR beyond it. | Parsers, clamps, facet clearing (§4.1 step 2, §4.2) | Signed out: `401` from the gate, handler never runs (§3.7) |
| Dashboard → ArcadeDB (slug write and read) | Database user | Grants | **Not validated on load** (SEC-2) | Back-fill logs and retries next boot (§3.2) |
| Public edge → VM (Traefik access log) | Tunnel | — | — | Request lines, including query strings, are persisted (SEC-1) |

The design adds no new endpoints beyond those listed, no new origin, no outbound call, and no change to the reviewer's reach. I found nothing in the design's stored outputs that would carry a secret.

## Findings

### SEC-1 The search text `q` is written to the edge access log, contradicting the design's own control - Severity: High
- **Component:** Board phase 2. The request-only `q` parameter, Traefik `accessLog`.
- **Evidence:**
  - The design says `q` is not in the address and "must not reach a log": `docs/designs/urls-and-board.md:448`, `:479`, `:292`.
  - Yet `q` is sent as a GET query parameter in three places:
    - the filter form requests `board/live` (a GET route, `:309`, `:332`), and the hook adds the `#boardFilters` fields, `q` included, to every board request (`:336`);
    - sisflow loads `GET …/board?paged=1&<filter>` built from `FormData` of the same form (`:371`);
    - `board/cards` is fetched "plus the filter" (`:373`).
  - The edge logs every request in its default format, which includes the full request line with its query: `deploy/traefik/traefik.yml:39`, `deploy/stacks/traefik.yml:54`.
- **Attack path:**
  1. An operator pastes sensitive free text into the Board search box. The design itself assumes this can happen (`:448`).
  2. Each debounced keystroke (`:332`) and each later live refresh (`:384`, which re-adds the form fields) writes that text in clear into the persisted Traefik access log.
  3. Anyone who can read the edge logs or log backups, or who receives them while troubleshooting, can read it.
- **Why High rather than Medium:** this is not a missing nicety. It is a stated data-exposure control that the design asserts and that the specified mechanism defeats. `TestTheBoardQueryRoundTrips` (`:558`) checks only `Encode()` and `HX-Replace-Url`, so it would pass while the leak happens.
- **Remediation:**
  - Carry `q` outside the request line. The `htmx:configRequest` hook moves it into a request header (for example `X-Board-Search`), `board.js` sets the same header on its `fetch` calls, and `parseBoardQuery` reads `q` from that header only. Traefik's access log drops request headers by default.
  - Alternatively, declare in §6 and D-094 that `q` reaches the access log, and remove the "must not reach a log" claim and the §8 row.
  - Add a test asserting that no client-side code path puts `q` into a URL. For example, the hook strips `q` from `evt.detail.parameters` for GET requests, and a source test checks `board.js`.
- **Effort:** small

### SEC-2 The slug's safe character set holds only by construction; a stored slug is never re-validated before it reaches `Location` and `HX-Push-Url` - Severity: Medium
- **Component:** `Product.Slug` storage, `resolveProduct`, `pushURL`, the redirect.
- **Evidence:**
  - The header-safety argument rests on the slug being `[a-z0-9-]` (`docs/designs/urls-and-board.md:446`).
  - The slug is written to and read from the ArcadeDB `Product` vertex with no schema declaration (`:104`). The load query is `store_arcade.go:1496`, as the design cites it.
  - Products are edited in the database by hand (`:108`, `store_mem.go:417`).
  - `slug()` produces the safe set only when it is called (`dashboard/store_mem.go:2462-2478`). `backfillSlugs` skips any product that already has a slug (`:101`), so a bad stored value is never corrected.
  - Uniqueness is guaranteed only "by construction" at assignment time (`:106`).
- **Attack path:**
  1. A hand edit, a restore, or a future code path stores a slug containing `/`, `?`, `#`, `..` or uppercase, or two products end up with the same slug.
  2. That value is concatenated into `Location`, `HX-Push-Url` and `HX-Replace-Url`.
  3. The result is path confusion, an injected query, or a bookmark opening the wrong product. A duplicate resolves to whichever product is matched first (`:86`).
  4. This is not an open redirect: the header always begins with the validated prefix plus `/p/`. It is still a broken integrity assumption on a database-to-dashboard boundary.
- **Remediation:**
  - In `load`, and in `resolveProduct`'s index, accept a stored slug only if it matches `^[a-z0-9]+(-[a-z0-9]+)*$`, is at most 48 bytes plus the suffix, does not have the `p-<digits>` id shape, and is unique.
  - Otherwise treat it as absent so `backfillSlugs` reassigns it, with a logged id only.
  - Build header paths with `url.PathEscape(slug)` as defence in depth.
  - Add a conformance case: an invalid or duplicate stored slug is replaced on reopen.
- **Effort:** small

### SEC-3 Window parameters: `offset` has no upper bound or parse rule, `more` is applied after the clamp, and the pair lists are bounded only after splitting - Severity: Low
- **Component:** `board/cards`, `board/live`, `parseBoardQuery`.
- **Evidence:** `docs/designs/urls-and-board.md:297-300`, `:310`, `:447`.
  - `offset` is "clamped to ≥ 0" only.
  - `more` "adds 30 … before building", but the 1..300 clamp is stated for `parseBoardQuery`, which runs earlier.
  - `shown` and `rev` are "at most nine pairs", but the design does not say how a longer value is cut.
- **Attack path:**
  - A signed-in client, or a buggy client, sends a huge or non-numeric `offset`. Unless the behaviour is specified, an `offset+limit` overflow can panic the handler. `net/http` recovers it, but logs a stack trace.
  - Repeated `more` pushes a column past 300.
  - A multi-kilobyte `rev` or `shown` is fully split and allocated before the nine-pair limit applies.
  - The impact is bounded by the session requirement.
- **Remediation:**
  - Specify the parse rules: a non-numeric `offset` counts as 0, `offset` is clamped to at most the column total, and slicing uses `min`.
  - Re-clamp after `more`.
  - Parse the pair lists with `strings.SplitN(v, ",", 10)` and reject a value over a fixed byte length.
  - Add these cases to `TestTheBoardJSONPagesAndFilters` and `TestShowMoreDeepensOneColumn`.
- **Effort:** quick-win

### SEC-4 No `Referrer-Policy` on the new page routes, whose address now carries the product name and filters - Severity: Low
- **Component:** Page routes, the CSP header set in `auth.go`.
- **Evidence:**
  - The slug (the product name) and the filter query are now in the address (`docs/designs/urls-and-board.md:448`).
  - The gate sets only the CSP (`dashboard/auth.go:525`, `:545`).
  - Existing external links carry `rel="noopener noreferrer"` (`dashboard/templates/backoffice.html:965`, `:1112`, `dashboard/markdown.go:465`), so protection is per link and depends on every future link remembering it.
- **Attack path:** A future external link, or an external resource added without `noreferrer`, sends the full back-office URL, including the product name, to a third party in the `Referer` header.
- **Remediation:** Set `Referrer-Policy: same-origin` (or `no-referrer`) next to the CSP for every non-bearer route. Add an assertion to `TestEveryRouteIsClassified`'s new checks (`:445`).
- **Effort:** quick-win

### SEC-5 §6 omits the `Referer` and request-line logging of filter values and the PR/Auto-fix `open=` parameter - Severity: Low
- **Component:** §6, "What becomes visible outside the session".
- **Evidence:**
  - §6 discloses that the slug reaches the edge access log (`docs/designs/urls-and-board.md:448`).
  - The URL-resident filters also reach that log, as do the existing `prs`/`autofix` `open=` drawer ids (`:136-137`, `:141`). This happens both as request lines on page loads and refreshes, and, if the deployed format includes it, as the `Referer` of every htmx request.
  - Edge configuration: `deploy/traefik/traefik.yml:39`, `deploy/stacks/traefik.yml:54`.
- **Attack path:** A reader of the edge log reconstructs which products, roles, stories and pull requests an operator viewed and when. The values are low-sensitivity, but D-093 should record the full set it accepts.
- **Remediation:** Extend §6 and D-093 to list everything now in the address and the log (slug, `BoardQuery` fields, `product`, `state`, `kind`, `open`), so the acceptance is explicit.
- **Effort:** quick-win

## Checks that passed

These are recorded so they are not re-raised:
- **No secret leakage:** `HX-Current-URL` is only compared, never echoed (`:166`, `:203`).
- **Prefix validation:** `sitePrefix` rejects absolute and scheme URLs (`dashboard/render.go:192-209`).
- **Signed-out handling:** the signed-out page body is built in the gate, before the mux runs (`dashboard/auth.go:551-560`, design `:201`).
- **Framing:** `frame-ancestors 'none'` still covers the page routes.
- **Methods:** the new routes are GET and read-only. The design states this for the Board routes, and `buildBO`'s view branches read from the store. Mutations keep the cross-origin check.
- **History cache:** htmx's history cache is disabled, so no back-office HTML lands in `localStorage` (`:174-176`).

Counts: Critical=0 High=1 Medium=1 Low=3
