<!-- Round 3, 2026-11-26. Lens: security-review. Target: docs/designs/urls-and-board.md (rev 3). Verbatim reviewer output. -->

# Security review: urls-and-board.md (rev 3)

Reviewed: `/home/ardi/devel/projects/ardihikaru/my-projects/gaos/.claude/worktrees/feat-urls/docs/designs/urls-and-board.md`

**Isolation note.** The design lives under the outer repository's `.claude/worktrees/`, so I opened it there as instructed. Because of that path, the harness also attached three rule files from `.claude/rules/`: `agent-workspace.md`, `qa-test-plan.md` and `frontend.md`. No `CLAUDE.md` was attached and I opened none. I did not open the checkout's own `.claude/`, `CLAUDE.md`, `AGENTS.md`, `GEMINI.md`, `docs/designs/reviews/` or any credential file.

## What this proposes

**Phase 1 (D-093)**
- Eleven new back-office `GET` routes, so that every view and every product section has an address of its own:
  - nine global views;
  - `/p/{ref}/{section}`;
  - `/p/{ref}`, which redirects (302) to the slug.
- A product gets a stored slug, made from its name. It is assigned once and checked against a regular expression when it is loaded. Products that have no slug yet get one at boot, and each assignment emits the existing `RecordUpdated` event.
- A single function, `sheet`, sets `HX-Push-Url` or `HX-Replace-Url`. It builds them from:
  - a prefix that has already been validated;
  - fixed path parts;
  - an allow-listed set of query keys, each length-bounded and re-encoded.
- htmx's history cache is switched off.
- Navigation buttons become `<a href>` links.
- A signed-out browser that asks for a page gets a `401` whose body is the sign-in page.
- After sign-in on a deep URL, the server answers `204` with `HX-Refresh: true`.
- `Referrer-Policy: same-origin` is added to every non-bearer route.

**Phase 2 (D-094)**
- A paged and filtered Board read model.
- One new route, `GET /products/{id}/board/live`. It returns only the columns that changed, detected with an FNV hash per column.
- A table view.
- A `paged=1` form of the JSON board.
- The search text travels in an `X-Board-Search` request header, never in the URL.
- A `live.js` hook copies the filter, `shown` and `rev` onto the URL of every htmx request made from the Board tab, mutations included.

## How the existing system works, per this document

- Every back-office route except a fixed public list and the bearer routes needs a verified session.
- The gate (`auth.go:527-565`) answers a signed-out caller with `401`: the sign-in sheet for htmx requests, JSON for anything else.
- The cross-origin check refuses only non-`GET` requests (`auth.go:570-587`).
- The session cookie is `HttpOnly` and `SameSite=Lax` (`auth.go:489-494`).
- The Content-Security-Policy forbids framing (`auth.go:525`).
- Today all navigation targets `#sink`, and the address never changes.
- The edge's access log records request lines, including query strings (`deploy/traefik/traefik.yml:39`).

**Rubric premise checked.** The rubric says `signedIn()` accepts any cookie. That is out of date in this checkout. `signedIn` → `sessionFrom` checks the cookie's signature and expiry (`dashboard/auth.go:361-382`), and `TestEveryRouteIsClassified` tests a forged cookie (`dashboard/auth_test.go:310-325`). The design's use of "the operator" refers to an identity that does exist, so I raise no finding on it.

## Trust boundaries the design touches

| Boundary | Authenticates | Authorises | Validates | Far side fails |
|---|---|---|---|---|
| Operator browser → dashboard: page routes | Signed session cookie (`auth.go:366-382`) | Session present; the design adds no per-product rule | `{section}` against a fixed set; `{ref}` against the stored slug or id; query keys allow-listed and bounded | Signed out: `401` with the sign-in page, built in the gate before any handler runs |
| Operator browser → dashboard: `board/live`, `paged=1` | Same | Same | §4.2 bounds; values the product does not offer are cleared | Board left as it was (§7) |
| Dashboard → browser: response headers (`HX-Push-Url`, `HX-Replace-Url`, `Location`, `HX-Refresh`) | — | — | `sitePrefix` (`render.go:192-209`), the slug pattern, `url.Values.Encode` | — |
| Public edge → VM | Tunnel | — | — | The access log now also holds product names and filters |
| Dashboard → ArcadeDB | Database user | Grants | Stored slug checked on load (§6) | Slug and event kept in memory, retried at next boot (§7) |

I checked these and found no problem:
- **Replay or forged requests:** the new routes are `GET` and read-only. `buildBO` makes only reads (`handlers_bo.go:189-331`), and the PR and Auto-fix views make no outbound calls (no matches for an HTTP client in `prview.go` or `autofix_view.go`).
- **Open redirect, header injection, path traversal:** response headers are built from a validated prefix, a slug that matches `^[a-z0-9]+(-[a-z0-9]+)*$`, and re-encoded allow-listed values. `HX-Current-URL` is only compared, never echoed. The value of `HX-Refresh` is fixed.
- **Oversized input:** `shown` is capped at 256 bytes and 1..300 per column; `rows` at 1..1000; search text at 80 runes.
- **Enumeration:** a signed-out response is identical whether or not the product exists.
- **Framing:** prevented by `frame-ancestors 'none'`.
- **Route collisions:** none of the new patterns clashes with an existing route in `dashboard/main.go`.

---

### SEC-1 The "search text never in a URL" control depends on `live.js` running, and fails open - Severity: Medium
- Component: Phase 2 filter bar (`#boardFilters`), `static/live.js` hook, `static/board.js` `reload()`
- Evidence:
  - The search box is a named form field (`name q`) inside a form that uses `hx-get` (`docs/designs/urls-and-board.md:338`).
  - Only the `live.js` hook takes `q` off the URL, and only "while the Board tab of a product is on screen" (`urls-and-board.md:342-346`).
  - `sisflow`'s `reload()` reads the form with `FormData` (`urls-and-board.md:386`), which includes `q`.
  - The only test reads the static source files (`urls-and-board.md:468`, `:585`).
  - htmx, `app.js` and `live.js` are separate `defer` scripts (`dashboard/templates/page.html:45-47`), so htmx can run while `live.js` does not.
  - The design itself records that a static module failed to parse for ten releases (`urls-and-board.md:38`).
  - The edge access log records the request line (`deploy/traefik/traefik.yml:39`, `deploy/stacks/traefik.yml:54`).
- Attack path: no attacker is needed. The text an operator types into the search box can reach the edge access log, browser history and every later request's `Referer` in three ways:
  - **`live.js` fails to parse or throws.** htmx then puts the form's named fields, `q` included, in the `GET` query string.
  - **A timing gap.** Pressing Enter in the search box before htmx has initialised triggers a native form submit to the current address with `?q=…`. That is a full navigation, so the text lands in the address bar and the history, and becomes the page URL.
  - **The hook's "on screen" check misjudges the tab state.**

  The design states this as a guarantee (`urls-and-board.md:468`: "The search text is the one thing kept out"), but nothing in the server or the markup enforces it.
- Remediation:
  - Give the search input **no `name`** (or place it outside the form) so that neither htmx nor a native submit can serialise it.
  - Add `hx-params="not q"` as a second guard.
  - Add a form `onsubmit` that prevents the native submit.
  - Have the hook read the box by id and set only the header.
  - Add a rendered-template test that the search input has no `name` and that the form cannot submit natively, alongside the existing source-reading test.
- Effort: quick-win

### SEC-2 `sheet` becomes the only route to the sheet but does not apply the no-store rule - Severity: Low
- Component: Phase 1 `sheet()`; phase 2 `board/live` and the `paged=1` JSON
- Evidence:
  - `noStoreView` marks archive, integrations, autofix and prs as `no-store` (`dashboard/integrations.go:24-31`). Only `handleBOSheet` and `handleBOView` call it (`dashboard/handlers_bo.go:138`, `:149`).
  - The design makes `sheet` the single place that sets the address (`urls-and-board.md:147-159`). Its call sites include `renderBOView` (retarget, which renders the PR view) and archive/restore of a conversation (`urls-and-board.md:154-156`). The design says nothing about `Cache-Control` for any of them, for `board/live` (`:318`) or for the paged JSON (`:319`).
- Attack path: on a shared or managed machine, a back-office response holding archived-conversation or topology data (D-082, D-085) can be kept in the HTTP cache when it is rendered by a call site other than the two that apply `noStoreView`. Most of these responses answer `POST` requests, which browsers rarely cache, so the exposure is small. But the design has just built a single point of control and is not using it for this rule.
- Remediation:
  - Call `noStoreView(w, v.BO.View)` inside `sheet`.
  - State that `board/live` and the `paged=1` JSON send `Cache-Control: no-store`.
  - Extend `TestOnlyTheAllowedFunctionsCallBuildBO`, or a sibling test, to assert `no-store` on every sheet response for the four sensitive views.
- Effort: quick-win

### SEC-3 Not every place the new addresses appear is listed - Severity: Low
- Component: §4.2 and §6 statements about the edge log, and D-093's record of what was accepted
- Evidence:
  - The design says the access log "does not record request headers" (`urls-and-board.md:305`).
  - `accessLog: {}` is Traefik's default format (`deploy/traefik/traefik.yml:39`), which, going by Traefik's documented defaults, records the `Referer` and `User-Agent` headers.
  - Every htmx request sent from a page carries the page address in `Referer` (same-origin, so `Referrer-Policy: same-origin` still sends it; `urls-and-board.md:210`).
  - The log's destination and who can read it are not stated anywhere in the design (`urls-and-board.md:468`, `:609`).
- Attack path: the slug (the product's name) and the Board filters are written to the access log twice: once as the request line of the page load, and again as the `Referer` of every fragment, live-refresh and static-asset request made from that page. Anyone who can read the edge logs sees them, which multiplies what D-093 accepts. `X-Board-Search` itself is still not logged, so the search text stays out as long as SEC-1 holds.
- Remediation:
  - Correct the sentence: the log records `Referer`, and therefore the page address, on every request.
  - Add "as `Referer` on every subsequent request" to the list D-093 accepts.
  - Name where the edge access log is written and its retention, or state that this is out of scope.
- Effort: quick-win

### SEC-4 A cross-site link can open a view with a drawer already open beside its action buttons - Severity: Low
- Component: Phase 1 page routes for `prs` and `autofix`, with the `open` parameter
- Evidence:
  - `open` is kept in the page address for `prs` and `autofix` (`urls-and-board.md:140-141`).
  - A cross-site top-level navigation carries the session cookie, "by design" (`urls-and-board.md:210`; `SameSite=Lax`, `dashboard/auth.go:493`).
  - The most recent feature added a state-changing control to a fix PR, changing its base branch (commit `64d16b2`, D-092 v0.54.0).
- Attack path: a third party sends a signed-in operator a link like `…/app/prs?open=fix:N`. The operator lands on a real GAOS page with a chosen record's drawer open, next to its action controls. The click itself is the operator's own, so the request is same-origin and the origin check will not stop it. This is social engineering, not a bypass, and framing is already blocked. It is new because, until now, no URL could pre-open a drawer.
- Remediation:
  - State in §6 that a deep link may pre-select a record but never pre-fills or arms a mutation.
  - State that every irreversible action reachable from a drawer (retarget, prune, close a defect) needs an explicit confirmation step. Cite the existing confirmation, or add one.
- Effort: small

### SEC-5 A fallback that would read the filter from request headers is left undecided - Severity: Low
- Component: §11 risk row on the `htmx:configRequest` hook; `parseBoardQuery`
- Evidence:
  - §4.2 says `parseBoardQuery` reads only `r.URL.Query()` plus one header (`urls-and-board.md:286`).
  - §11 says that, if the hook cannot rewrite a `PUT`'s path, "the hook sets the same values as request headers and `parseBoardQuery` reads headers first; decided at the server-half checkpoint" (`urls-and-board.md:606`).
- Attack path: the input surface of an authenticated mutation path could change after approval without review. The header names, their length bounds, their precedence over the query, and whether the address builder (§3.4) or `Encode` would then read headers are all unspecified. No exploit follows from this, but it is an unreviewed second input channel.
- Remediation:
  - Either name the headers now, with the same §4.2 bounds, and state that `Href` and `Encode` never take a value from a header,
  - or require the checkpoint's outcome to come back for a security re-review before any template depends on it.
- Effort: quick-win

Counts: Critical=0 High=0 Medium=1 Low=4
