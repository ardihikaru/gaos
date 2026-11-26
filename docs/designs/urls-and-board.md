# Real URLs for the back office, and a Board that pages, filters, switches to a table and refreshes in place

> **Status:** Approved (rev 4; the operator approved both phases on 2026-11-26 after three review rounds — round 3 raised no Blocker and no Critical or High. The reports are `docs/designs/reviews/urls-and-board-{comprehension,zoomout,critic,readiness}-{1,2,3}.md` and `urls-and-board-security-review-{1,2,3}.md`; the triage of each round is `urls-and-board-review-{1,2,3}.md`.)
> **Policy:** `docs/rules/gaos-design-review.md`
> **Related:** `docs/designs/card-timing.md` (D-081, the last change to the Board), `docs/designs/gaos-pr-overview.md` (D-092, whose filter bar, table and live region this design reuses), `docs/designs/bo-login.md` (the sign-in this design must not weaken)
> **Paths.** A bare name (`board.go`) is in `dashboard/`; so are `templates/`, `static/` and `schema/`. A path that starts with `docs/`, `pustaka/`, `deploy/` or `.claude/`, or `README.md`, is relative to the repository root. Line numbers are those of the branch `feat/urls` at the commit that adds this file. That branch is cut from `fix/board-js-syntax` (pull request #45, v0.54.1); **phase 1 starts after #45 is merged to `main`**, and every baseline count below (TC-721, 587 Go tests) is the count after it.
> **Terms.** *Back office* is the signed-in part of the dashboard. *Sheet* is its outer element, `#boSheet` (`templates/backoffice.html:46`). *Fragment* is a piece of HTML a route returns for htmx to swap in; *page* is a whole HTML document. *OOB* (out of band) is htmx's `hx-swap-oob`: an element in a response that replaces the element with the same id wherever it is. *Hint* is one message on the live stream; it names what changed and carries no HTML (`broker.go:32`). *Card* and *task* are the same thing, a `TaskRecord`. *sisflow* is the vendored client-side board library under `static/vendor/sisflow/`; *the server board* is the plain HTML board the server renders (`.board-ssr`, `templates/backoffice.html:401`). *Prefix* is the path Traefik strips before the dashboard sees a request, `/app` in every deployed tier (`deploy/traefik/dynamic.yml:34`, `:69`). *The fold* is the function that rebuilds state from the event stream (`fold.go`); *the projection* is the in-memory store the ArcadeDB store keeps and answers reads from (`store_arcade_blueprint.go:34`). *The timing index* is the per-task list of event positions the card timing reads (`timing.go:404`). *A run record* is one attempt of a task (`TaskRunRecord`, `model.go:886`). *A send-back* returns a blocked design to its author (D-084). *The edge* is Traefik, the proxy in front of the dashboard. *The chrome* is the top bar's counts, which `RenderWithChrome` re-sends with a response (`render.go:102`). *A desk* is the store's older name for a product: an event's `DeskID` is a product id. A card's *title* is the field `BoardCard.Label` (`board.go:42`); its *labels* are the chips in `BoardCard.Labels`, and the `label` filter means those. The nine *column ids* are `backlog`, `ready`, `working`, `needs-you`, `review`, `blocked`, `failed`, `done` and `canceled` (`board.go:119-127`). *The operator* is the product owner who approves this design; decisions marked "decided by the operator" were taken in the planning conversation on 2026-11-26 and are recorded by the decision-log entries this design adds (D-093, D-094).
> **Example.** The product in every example is an invented one named "Route Planner", id `p-6`, slug `route-planner`.

## 1. Problem

### 1.1 The address never changes

Every click in the back office is an htmx `GET` that targets a hidden `#sink`, and the answer is the whole sheet marked `hx-swap-oob` (`templates/backoffice.html:46`, `:140`). Nothing pushes a history entry: there is no `hx-push-url`, no `HX-Push-Url` header and no `pushState` anywhere in `templates/`, `static/` (outside the vendored htmx) or the Go code. An operator who opens Route Planner, then Stories, then Board, still reads `/app/` in the address bar. A link to "the Board of Route Planner" cannot be copied, the Back button leaves the dashboard, and a reload returns to the product list.

Only one route returns a page: `GET /{$}` (`main.go:499`, `handleIndex`, `handlers_tasks.go:17`). The product route `GET /products/{id}?tab=…` (`main.go:526`) and the view route `GET /ui/bo/{view}` (`main.go:519`) return fragments only.

### 1.2 The Board renders everything, in narrow columns, and replaces the whole sheet on every change

- **No limit.** `MemStore.Board` (`board.go:242`) returns one card per task of the product; the template prints all of them (`templates/backoffice.html:402-422`). The Done column holds every finished task for the life of the product.
- **Narrow.** The board is a grid of nine columns with a 140px minimum (`templates/backoffice.html:401`), inside a two-column layout whose second column is a 300px aside (`static/app.css:855`), inside a 1400px cap (`static/app.css:812`). It already scrolls sideways.
- **No filter, one view.**
- **A change replaces everything.** On a `task` hint, `static/live.js:97` (`onTask`) re-fetches `products/{id}?tab=board`. The server runs `buildBO` (`handlers_bo.go:188`), which copies the whole event stream to count the Timeline (`handlers_bo.go:331`), renders every sequence diagram (`handlers_bo.go:341`), builds the full board, renders the sheet, and the browser replaces `#boSheet`: header, navigation, title, board and aside. `keepPlace` (`static/live.js:63`) restores the vertical scroll of the sheet and nothing else, so the board jumps back to its first column.
- **Two decorators are quadratic.** `decorateBoardConversation` (`conversation.go:621`) calls `cardLocked` (`conversation.go:208`) per card, and `decorateBoardGate` (`sendback.go:375`) calls `taskByIDLocked` (`designgate.go:545`) per card; both helpers scan every task.

Measured on `main` at `f71aa46` (AMD Ryzen 7 5800H, in-memory store, a synthetic product: 90 % of cards Done, six `TaskMoved` events per card, one run record on every second card; one request = one live refresh of the Board tab):

| Cards | `Board()` time | `Board()` allocated | Refresh time | Refresh allocated | Response size (uncompressed) |
|---:|---:|---:|---:|---:|---:|
| 200 | 2.0 ms | 3.6 MB | 10.6 ms | 8.5 MB | 191 KB |
| 2,000 | 25 ms | 36 MB | 109 ms | 94 MB | 1.83 MB |
| 10,000 | 361 ms | 182 MB | 823 ms | 480 MB | 9.10 MB |

A card costs about 0.9 KB of HTML (1.83 MB / 2,000). `Board()` grows faster than linearly (×14 for ×5 cards between the last two rows), which is the quadratic term above.

### 1.3 Two boards exist, and until v0.54.1 only one of them loaded

The Board tab ships the server board and, hidden beside it, a host for sisflow (`templates/backoffice.html:400`). `static/app.js:409` (`mountBoard`) imports `static/board.js`, which mounts sisflow and hides the server board. From v0.44.0 to v0.54.0 that module did not parse, so every operator saw the server board. v0.54.1 repairs it (pull request #45, `TestStaticScriptsHaveBalancedBrackets`). **Decided by the operator: both boards stay** — sisflow when the module loads, the server board as the fallback. Every Board feature in this design therefore states what it does on each.

## 2. Goals and non-goals

**Goals**

1. Every back-office view and every product section has its own URL, `/app/p/<slug>/<section>` for a product. Loading that URL shows that view; Back, Forward and reload work; a link can be copied.
2. The Board shows at most 30 cards per column until the operator asks for more.
3. Board columns are 300px wide and the board uses the full width of the window.
4. The Board can be filtered, and switched to a table.
5. A live change replaces only the columns that changed, keeps every scroll position, costs the server and the browser a small amount of work that does not grow with the number of finished cards, and briefly marks the card that changed. ("Make it smoother when anything changes live" was the operator's request; the mark is how a change is seen without anything moving. D-094 records it.)
6. The chosen filters and view are part of the URL. A background refresh or a mutation never rewrites them. One exception is stated in §4.1: a filter value the product no longer offers is ignored on screen at once, and leaves the address at the operator's next filter change or reload.

**Non-goals**

- Renaming a product. No code changes `Product.Name` after `CreateProduct` (`store_mem.go:1815`); a slug is therefore assigned once and never changes, and no redirect from an old slug is built.
- A new event type, task status, label or role, or a new docs CSS class. The three closed vocabularies — the event catalog, the task-status vocabulary and the docs component vocabulary — stay closed.
- Animating a card from one column to another, or any DOM morphing library.
- A table view inside sisflow, sisflow's own scroll-to-load paging, grouping in the table, saved views, multi-select filters, or paging the Timeline tab.
- Editing a file under `static/vendor/`.
- A change to the machine API under `/v1`, or to the JSON `GET /products/{id}/board` returns when called without the new `paged` parameter.
- Deploying. Each phase ends at a merged pull request.

**Phase 1 can be approved and shipped alone.** It contains nothing of the Board's: no Board type, parameter or test. Phase 2 needs phase 1.

## 3. URLs (phase 1, v0.55.0, D-093)

### 3.1 The routes

All paths below are what the dashboard sees; the browser's address has the prefix in front (`/app/p/route-planner/board`).

| Path | Shows | New |
|---|---|---|
| `/` | The product list (`GET /products` is already the fragment route, `main.go:524`) | no |
| `/inbox`, `/stories`, `/diagrams`, `/knowledge`, `/agents`, `/archive`, `/integrations`, `/autofix`, `/prs` | That back-office view — the same nine view ids `buildBO` switches on (`handlers_bo.go:221-286`) | 9 |
| `/p/{ref}/{section}` | One product section. `{section}` is one of the nine tab ids the sheet already uses (`templates/backoffice.html:71-79`): `brd`, `stories`, `diagrams`, `knowledge`, `board`, `qa`, `timeline`, `blueprint`, `vendors`. | 1 |
| `/p/{ref}` | `302` to `/p/<slug>/brd`; an unknown `ref` is the `404` page below | 1 |

Eleven new patterns: ten render a page and one redirects. There is no catch-all, so an unknown path stays a 404 from the mux.

**Page routes return pages only; fragment routes return fragments only.** The existing fragment routes (`/products/{id}`, `/ui/bo/{view}`, `/ui/bo`) are unchanged and are still what htmx calls. No URL answers in two shapes, so no response needs `Vary: HX-Request` and a browser can never show a cached fragment as a page.

Every page route sends `Cache-Control: no-store`.

**The query string of a page** is read by the view through a parser, never copied as a whole: §3.4 lists the parameters each view keeps. The global views keep the existing `product=<id>` parameter (`inbox?product=p-6`); it stays an id, because it is a filter value inside a view and the fragment routes, the inbox chips and `prHref` already speak ids.

**Resolving `{ref}`**, in this order:

1. A product whose `Slug` equals `ref` → render (`200`).
2. A product whose `ID` equals `ref` → `302` to the same path with the slug. `{section}` is checked first: an unknown section is the `404` below, never a redirect. The `Location` is `sitePrefix(r)` (`render.go:192`) + `/p/<slug>/<section>` + the page's query rebuilt from its parsed parameters (§3.4). Nothing is copied from `r.URL.RawQuery`; a parameter the view does not know is not in the `Location`.
3. Otherwise → `404`, rendered as the page with the product list and the toast "No such product."

An unknown `{section}` is the same `404` with the toast "No such section."

### 3.2 The slug

`Product` (`model.go:41`) gains one field, `Slug string`.

- **Assigned once**, in `CreateProduct` (`store_mem.go:1815`), by a new pure function `newSlug(name, id string, products []Product) string` in the new file `urls.go`:
  1. `base` = the existing `slug(name)` (`store_mem.go:2462`), cut to 48 bytes, trailing `-` removed. `slug("Route Planner")` is `route-planner`; `slug("RoutePlanner")` is `routeplanner`.
  2. If `base` is empty, `base` = `id`.
  3. A candidate is *taken* if another product has it as `Slug`, or if it has the shape of a product id (`p-` followed only by digits) and is not this product's own id.
  4. Return the first of `base`, `base-2`, `base-3`, … that is not taken.
- **Existing products** get a slug the same way, in ascending numeric id order, from `backfillSlugs(products []Product) (changed []int)`, which skips a product that already has one. It runs in two places:
  - `NewMemStore`, after the fixtures are copied (`store_mem.go:325`). The fixtures are the store's genesis; nothing is emitted.
  - `NewArcadeStoreFor`, only in the branch where `load` found a database (`store_arcade.go:163-165`, after `markLoaded`) and before `installHook`; the other branch builds a `NewMemStore`, which has already back-filled, and persists all of it. A new method `MemStore.AssignSlug(ctx, productID string) (Result, error)` sets one product's slug, emits the existing event `RecordUpdated` (`events.go:38`; kind `KindInfo`, desk = the product id, text `<product name>: slug assigned`, the form `UpdateBRD`'s event has) and returns a `Result` whose `Master` is that product's record. The ArcadeDB store runs it through its ordinary `mutate` (`store_arcade.go:533`), once per product `backfillSlugs` names; `persist` writes the event and, because `Result.Master` is set, the `Product` vertex with `writeProduct` (`store_arcade.go:583-590`), in one transaction. That is a state change with its event, as BR-02 requires (`pustaka/docs/product/brd.html:1226`), and no new event type. If a write fails it fails as any mutation's write does today (§7); the error is logged with the product's id and the boot continues.
- **Stored** as the property `slug` on the `Product` vertex: `writeProduct` (`store_arcade.go:763`) adds it to its map and the load query (`store_arcade.go:1496`) selects it. `repo` and `goal` are already written this way with no schema declaration (no `Product.repo` or `Product.goal` exists in `schema/*.sql`), so **there is no schema file and no migration**.
- **The fold is untouched.** The product record is rebuilt by `load`, not by the fold: `ProductCreated` and `RecordUpdated` are in the fold's no-op list (`fold.go:442`). A product created after this change gets its slug inside `CreateProduct`, under the `ProductCreated` event that mutation already emits (`store_mem.go:1845`).
- **Unique** per tenant by construction: every assignment runs under the store's write lock and checks every other product.

**A stored slug is validated on load** (§6): one that is malformed or a duplicate is treated as absent and assigned again.

**Why stored and not derived.** No handler deletes a product, but products are removed from the database by hand, which is why `nextProductID` exists (`store_mem.go:417`). If two products share a name and the older one is removed, a slug derived at boot would move the survivor from `x-2` to `x`: the address of a product that still exists would change under its bookmarks. A stored slug does not move while its product exists. It is not reserved after the product is gone — §12 records that limit.

`ProductCard` and `ProductDetail` (`view.go`) each gain `Slug`, so templates can print page links.

### 3.3 How a deep URL becomes a page

`handlePage(view string)` serves the nine view routes and `handleProductPage` serves `/p/{ref}/{section}`. Both do what `handleIndex` does (`handlers_tasks.go:17`): build the view with `buildBO` and render the `page` template. They differ from it only in the arguments to `buildBO`.

The page's title is `BOView.PageTitle` (§3.4), printed by `templates/page.html:6`:

| Shown | Title |
|---|---|
| The product list | `GAOS · Products` (today's) |
| A global view | `<Label> · GAOS`, where `<Label>` is the navigation label (`templates/backoffice.html:87-96`): `Inbox`, `Stories`, `Diagrams`, `Knowledge`, `Agents`, `Archive`, `Integrations`, `Auto-fix`, `Pull requests` |
| A product section | `<Label> · <product name> · GAOS`, where `<Label>` is the tab label (`templates/backoffice.html:71-79`): `Requirements`, `Stories`, `Diagrams`, `Knowledge`, `Board`, `QA`, `Timeline`, `Blueprint`, `AI vendors` |

### 3.4 How navigation changes the address

The server sets a response header, so no link needs a new attribute.

**`Href` and `PageTitle`.** `BOView` (`view.go:160`) gains two fields, both set by `buildBO` in every branch:

- `PageTitle string` — §3.3's table.
- `Href string` — the canonical address of what was built, relative, with no leading slash, made of two parts:
  - the **path**, which follows from the view, the product and the tab: `""` for the product list, `inbox`, `prs`, `p/route-planner/board`. The slug goes through `url.PathEscape`.
  - the **query**, rebuilt with `url.Values.Encode()` from the parameters that view parses out of the request being answered (`r.URL.Query()`, never the body), and from nothing else:

    | View | Parameters kept |
    |---|---|
    | `inbox` | `product` |
    | `prs` | the fields of `PRFilter` (`prview.go:36`): `product`, `state`, `kind`, `open`. `Href` is `prPageHref(filter)` (§3.6) for the filter the view parsed, so a default such as `state=open` is left out exactly as `prHref` leaves it out (`prview.go:130`). |
    | `autofix` | the fields of `AFFilter` (`autofix_view.go:157`): `product`, `state`, `difficulty`, `open`. `Href` is `afPageHref(filter)`. |
    | every other view, and every product section | none in phase 1. Phase 2 adds the Board's (§4.2). |

    `tab` and every unknown parameter are dropped, and a kept value longer than 128 bytes is dropped. Examples: `inbox?product=p-6`, `prs?open=fix%3A12`.
  - When the view is a product and the product does not exist (`Detail == nil`), `Href` is `"-"`, which `pushURL` treats as "send nothing".

**One call site.** A new method `(s *server) sheet(w, r, v *View, view, productID, tab string)` sets `v.BO = s.buildBO(…)`, calls `noStoreView(w, v.BO.View)` (`integrations.go:27` — today only two of these call sites apply it) and then `pushURL(w, r, v.BO, false)`. Every handler that renders the sheet into an htmx response calls `sheet` instead of assigning `buildBO`'s result:

| Call today | File and line |
|---|---|
| after sign-in | `handlers_bo.go:96` |
| `handleBOSheet`, `handleBOView`, `handleProductList`, `handleProductDetail` | `handlers_bo.go:137`, `:148`, `:158`, `:170` |
| `renderBOAfter` (every product mutation) | `handlers_bo.go:518` |
| `renderBOView` (the retarget and contact handlers) | `handlers_bo.go:1028` |
| creating a workflow, which opens the Board | `handlers_cw.go:87` |
| archive, restore and prune a conversation | `handlers_conversation.go:121`, `:123`, `:156` |
| answering a question from the inbox | `handlers_questions.go:152` |

`TestOnlyTheAllowedFunctionsCallBuildBO` reads the source and fails if `buildBO(` is called from any function other than these five: `sheet`, `handleIndex` (`handlers_tasks.go:17`), the new `handlePage` and `handleProductPage`, and `handleReadiness` (`handlers_bo.go:474`, a leaf fragment that is not the sheet).

**`pushURL(w, r, bo, replaceOnly bool)`** sends nothing unless the request has `HX-Request`, and nothing when `bo.Href` is `"-"`. With `replaceOnly` false — every call from `sheet` — it also sends nothing when the request has the header `X-GAOS-Refresh` (below). With `replaceOnly` true — the Board's own route, §4.3 — only the second row of the table applies and the first is "none". Otherwise it compares `bo.Href` with the request's `HX-Current-URL` header. Both sides are reduced the same way before comparing: prefix removed, path cleaned, query parsed and re-encoded with `url.Values.Encode()` (so parameter order and escaping do not matter). A missing or unparsable header counts as path `/` with no query.

| Request | Path | Query | Header |
|---|---|---|---|
| any | differs | — | `HX-Push-Url` (a navigation, or a mutation that opens another view) |
| `GET` | same | differs | `HX-Replace-Url` (a filter or a drawer: the address follows, the history does not grow) |
| `GET` | same | same | none |
| not `GET` | same | — | none — a mutation never rewrites the query of the page it was made on |

The header value is `sitePrefix(r) + "/" + bo.Href`. `HX-Current-URL` is used only for this comparison, never as a redirect target and never in a response body.

**A background refresh never touches the address.** `static/live.js` issues its own requests (`get`, `:40-44`) when a hint arrives. It adds the request header `X-GAOS-Refresh: 1` to each, and a sheet rendered for such a request sets no address header. Two existing faults in `live.js` would otherwise become visible in the address bar, and are fixed in the same change:

- The refresh is decided when the hint arrives and sent 200 ms later (`soon`, `:48-51`). The timer's function re-reads `#boMain` and does nothing if the view, product or tab it was scheduled for is no longer on screen.
- A refresh held back by `busy()` is kept in `deferred` and replayed after the next swap (`:76-84`, `:160-162`) — often the operator's own navigation away. `deferred` now records the view, product and tab it belongs to, and is discarded instead of replayed when `#boMain` shows something else.

**The title.** htmx applies a `<title>` only when it is a top-level node of the response. A new template `bo-title` prints `<title>{{if .BO}}{{.BO.PageTitle}}{{else}}GAOS · Products{{end}}</title>`. It is the first line of the four templates that carry the sheet to an htmx request — `bo-main-oob` and `bo-main-close-talk` (`templates/backoffice.html:140`, `:143`), `sheets` (`:147`, so signing out sets `GAOS · Products`) and `cw-closed` (`templates/workflow.html:122`) — and `handleBOSheet` (`handlers_bo.go:139`) renders `bo-main-oob` instead of the bare `bo-sheet`. `templates/page.html:6` prints the same expression.

### 3.5 Back and Forward

`templates/page.html:12` already has a `<meta name="htmx-config">`; htmx reads only one, so the two new keys are **added to that element**: `"historyCacheSize":0,"refreshOnHistoryMiss":true`. Both exist in the vendored htmx 2.0.4.

With them, Back or Forward makes the browser load that URL from the server, which renders the page for it (§3.3). htmx keeps no snapshot of back-office HTML in `localStorage`, which would otherwise contradict the `no-store` this application sets on its sensitive views (`integrations.go:27`).

Verified in a spike on this branch (headless Chrome, then reverted): product list → product → Stories → Board gave `/`, `/p/p-1/brd`, `/p/p-1/stories`, `/p/p-1/board`; three Backs and one Forward each showed the right view; a direct load and a reload of `/p/p-1/board` showed the board with its sixteen cards; `localStorage` stayed empty. A second `htmx-config` element placed after the first was ignored — hence "added to that element".

The cost is one full page render per Back or Forward.

### 3.6 Links a browser understands

The navigation is `<button hx-get>` today (`templates/backoffice.html:59`, `:71-79`, `:87-96`, `:160`), which cannot be opened in a new tab. Each becomes `<a href="{page url}" hx-get="{fragment url}" hx-target="#sink" hx-swap="innerHTML">`. The CSS selectors `.bo-menu button` become `.bo-menu a` (`static/app.css:789-806`) with `text-decoration:none`; `.bo-crumb-up` and `.mcard` get the same treatment.

- **A modified click must reach the browser.** The vendored htmx cancels the default action of a click on any `<a href>` that has `hx-get`, and exempts Ctrl/Meta-click only for *boosted* links; an `hx-trigger` filter does not help, because htmx cancels before it evaluates the filter. So `static/app.js` gains one listener on `document`, in the **capture** phase: for a `click` whose target is inside `a[href][hx-get]` and that has `ctrlKey`, `metaKey`, `shiftKey` or `altKey` set, or `button !== 0`, it calls `stopPropagation()`. htmx's listener on the link never runs, and the browser opens the `href` as it would for any link. Checked in the phase-1 exit.
- **Every `href` beside an `hx-get` is a page URL.** The eleven existing `<a … hx-get>` elements have an `href` that points at a fragment route today, so "open in new tab" shows a bare fragment:
  - Ten are Pull requests and Auto-fix rows, tabs, drawers and reset links (`templates/backoffice.html:936-993`, `:1085-1143`). `prview.go` and `autofix_view.go` gain `prPageHref(f PRFilter) string` and `afPageHref(f AFFilter) string`, each the existing function's result with the leading `ui/bo/` removed. The templates print view-model fields, not function calls, so every struct field that holds one of those links today gains a sibling set in the same place: `PageHref` beside `Href` (rows and state tabs), `ResetPageHref` beside `ResetHref`, `ClosePageHref` beside `CloseHref` (`prview.go:65`, `:81`, `:119`; `autofix_view.go:90`, `:163`, `:186`, `:208`). The templates print the new field in `href` and keep the old one in `hx-get`.
  - One is the Auto-fix table's product link (`templates/backoffice.html:904`, `href="products/{{.ID}}"`). Its `href` becomes `p/{{.ID}}/brd` — the id form, which redirects to the slug (§3.1) — so its row needs no new field.
  - `TestEveryHtmxLinkHasAPageHref` renders every view and fails on an `<a … hx-get>` whose `href` starts with `ui/` or `products`.

`<base href="{{.Prefix}}/">` (`templates/page.html:25`) is an absolute path, so every relative URL in every template resolves the same at any depth. No template contains `href="#…"`.

### 3.7 Signed out on a deep URL

BR-55 says every route is refused without a verified session unless it is on the fixed public list or is a bearer route (`pustaka/docs/product/brd.html:1349`), and `TestEveryRouteIsClassified` holds every other route to a `401` (`auth_test.go:321-334`). **That stays true.** A page route is a back-office route and is refused with `401` when signed out. What changes is the *body* of the refusal for a browser that asked for a page.

Today a signed-out request with `HX-Request` gets `401` with the sign-in sheet (`requireBO`, `handlers_bo.go:117`), and one without gets `401` JSON (`auth.go:558`). A person who opens a bookmarked `/app/p/route-planner/board` signed out sees JSON.

- `auth.go` gains `pagePatterns`, the eleven patterns of §3.1. It is a set of back-office patterns, not a new class: `publicPatterns` is not changed.
- In the gate's signed-out branch (`auth.go:552-560`), a request whose pattern is in `pagePatterns` and that has no `HX-Request` is answered with status **`401`**, `Cache-Control: no-store`, and the `page` template with the sign-in sheet open and no back-office data — the document `GET /{$}` renders signed out. A browser renders a `401` HTML body like any other. With `HX-Request` the answer is today's (`requireBO`).
- The gate answers before the mux is called, so the handler never runs and the product is never looked up: `/p/route-planner/board` and `/p/no-such/board` return byte-identical responses.
- The Content-Security-Policy applies to these routes as to every non-bearer route (`auth.go:543-547`), and the gate sets one more header beside it on every non-bearer route: **`Referrer-Policy: same-origin`**, because the address now carries a product's name (§6). The cross-origin check does nothing for these routes: it only refuses non-`GET` requests (`auth.go:570-573`). A cross-site top-level navigation to a page route carries the session cookie (`SameSite=Lax`, `auth.go:493`) by design, as it does to `/` today; these routes are read-only.
- **After sign-in**, `handleLogin` (`handlers_bo.go:56`) looks at `HX-Current-URL`. If it is missing, or its path with the prefix removed is `/`, it answers as today. Otherwise it sets the cookies and answers `204` with the header `HX-Refresh: true`; the browser reloads the URL it is on, now signed in. The header value is a constant. No URL supplied by the client is written into any response.
- **After sign-out** the address is left alone; a reload shows the sign-in form for it.
- **A deep link selects, it never acts.** `prs?open=…` and `autofix?open=…` can now open a view with one record's drawer showing. No parameter of any page route fills a form field or submits anything: every action in a drawer is still a form the operator fills and submits, and that request is a non-`GET` the cross-origin check covers.

## 4. The Board (phase 2, v0.56.0, D-094)

`feat/board` is cut from `main` after phase 1 is merged; phase 2 needs phase 1's `sheet`, `Href` and routes.

### 4.1 The read model

Two types, in the new file `board_page.go`. The first is the state that lives in the URL; the second is what one request adds and never reaches the URL.

```go
// BoardQuery is the view. Every field is comparable, and Encode writes all of it.
type BoardQuery struct {
    View      string // "" (the board) | "table"
    Role, Story, Label, Workflow string // "" = any
    Waiting   bool   // only cards waiting on a person
    HideEmpty bool   // hide columns with no matching card
    Sort, Dir string // table only; "" = the default
}

// BoardWindow is how much of the view one request wants. Never encoded.
type BoardWindow struct {
    Text  string         // the search box; arrives in a header, see §4.2. Printed back as the box's value.
    Shown map[string]int // board: cards per column. Missing = 30. Clamped to 1..300.
    Rows  int            // table: rows. 0 = 100. Clamped to 1..1000.
}

type BoardPage struct {
    ProductID string
    Query     BoardQuery        // the query, with every value the product does not offer cleared
    Columns   []BoardPageColumn // always all nine, in board order
    Rows      []BoardCard       // table view only
    Matched, All int            // cards matching the filter / all cards of the product
    Facets    BoardFacets
}
type BoardPageColumn struct {
    BoardColumn
    Matched, Shown int          // matching cards in this column / returned
    Cards []BoardCard
}
// BoardFacets is what each select offers, before filtering, sorted.
type BoardFacets struct {
    Roles, Labels, Workflows []string // the values present on any task of the product
    Stories []BoardRow                // "plan" first, then every story of the product, with or without a task
}
```

Two new store methods, declared beside `Board` in `store.go:127`:

```go
BoardPage(ctx context.Context, productID string, q BoardQuery, w BoardWindow) (BoardPage, error)
BoardCard(ctx context.Context, productID, taskID string) (BoardCard, error) // one decorated card
```

`MemStore.BoardPage` works under the read lock:

1. Walk `s.tasks` once. For each task of the product: put it in an id → `*TaskRecord` map and collect the facets.
2. Clear every field of `q` whose value the product does not offer: `Role`, `Story`, `Label` and `Workflow` must be in the facets; `View`, `Sort` and `Dir` must be in §4.2's lists. The result is `BoardPage.Query`. It is what the screen shows and what an address header is built from, so a filter is never active without a control that shows it: a select is shown whenever its facet has at least one value, and the search box always shows `BoardWindow.Text`. The value leaves the address when the next address header is sent — the operator's next filter change, view switch or reload — because a refresh and a mutation do not rewrite the address (§3.4).
3. Walk the product's tasks again, test each against the filter using the task's own fields only, and append the matching ones to their column's bucket.
4. Sort each bucket: by `Rank`, except **Done and Canceled, newest first** — by the task's **last state change**: `s.events[i].Seq`, where `i` is the last position in `s.timingIdx[id]`, read under `timingMu` after `extendIndexLocked` has brought the index up to date (`timing.go:432`) and without copying the events as `taskEventsLocked` does (`timing.go:404`). That index holds a task's planning, moves, questions and answers (`timing.go:436-453`); a label change is not a state change and does not move a card up. Ties break on `Rank`, then id.
5. Cut each bucket to `w.Shown[col]`. For the table, concatenate the buckets in board order, apply `Sort`/`Dir` (§4.6) and cut to `w.Rows`.
6. Build `BoardCard`s **only for the cards kept**, and decorate only those: QA, timing, conversation, gate. The conversation and gate decorators take the id map instead of calling `cardLocked` and `taskByIDLocked`; the timing decorator computes the kept cards only, instead of every task of the product (`timing.go:459`).

`BoardCard` (`board.go:40`) gains three fields, set by `BoardPage` and `BoardCard` only and tagged `omitempty`, so the document `Board` returns does not change: `Story string` (`json:"story,omitempty"`, the story id, empty for the plan), `Workflow string` (`json:"workflow,omitempty"`) and `Updated int` (`json:"updated,omitempty"`, the `Seq` of the task's last state change, step 4). Its existing `Row` field gains `omitempty` (`board.go:44`); `Board` never leaves it empty (`laneOf`, `board.go:156`), so its document does not change either.

`BoardPage` returns no workflow timing totals (`Board.Workflows`, `board.go:107`): nothing on either board shows them, and they need the timing of every task.

`MemStore.BoardCard` builds and decorates the one card. Three handlers that today build the whole board to read one card use it instead: the JSON answer to a move (`handlers_board.go:82`), the send-back answer (`handlers_board.go:152`) and the timing sheet's title (`handlers_bo.go:911`). On sisflow every drag is one of these.

`ArcadeStore` delegates both methods to its projection, as `Board` does (`store_arcade_blueprint.go:34`). `Store.Board` keeps its signature and its result; its two quadratic decorators get the id map too.

### 4.2 The parameters

`parseBoardQuery(r *http.Request) (BoardQuery, BoardWindow)` reads each parameter below from `r.URL.Query()` and, **for a key the URL does not carry at all**, from the request header `X-Board-State`, whose value is a URL-encoded query string of the same keys (at most 1,024 bytes, else ignored whole). A key present in the URL wins even when its value is empty: that is how a link clears a filter. It **never reads a request body** — a `POST …/label` has a body field called `label`, which is not the filter. It is syntactic: it has no product, so step 2 of §4.1 does the checking against the product. A `BoardQuery` value longer than 128 bytes is dropped — the bound §3.4 uses; `shown` and `rev` have their own rule below. `sort=status` and `dir` on its own parse to the default, `""`, so one view has one address. `BoardQuery.Encode() string` writes the first table, defaults omitted; parsing its output returns the same `BoardQuery` (`TestTheBoardQueryRoundTrips` compares with `==`).

**In the URL** (`BoardQuery`) — phase 2 adds this row to §3.4's table for a product's `board` section:

| Parameter | Values | Meaning |
|---|---|---|
| `view` | `table` | The table instead of the board |
| `role` | a role at least one task of the product has | |
| `story` | `plan`, or a story id of the product | `plan` = tasks with no story (`board.go:156`) |
| `label` | a label at least one task of the product has | |
| `wf` | a workflow id of the product | |
| `waiting` | `1` | Status `AWAITING_INPUT` or `REVIEW`: the two statuses in which a task waits for a person. The control is labelled **"Waiting on a person"**. It is deliberately not called "Needs you", which on this board is the name of one column and one badge and means `AWAITING_INPUT` only (`board.go:137`). |
| `empty` | `hide` | Hide columns with no matching card |
| `sort`, `dir` | §4.6 | Table only |

**In a request only** — `Encode` never writes these and no response header ever carries them. Values taken from `X-Board-State` pass the same rules and the same step 2 before anything is built from them:

| Carried in | Name | Values | Meaning |
|---|---|---|---|
| request header | `X-Board-Search` | text, percent-encoded by the browser; decoded and cut to 80 runes | Case-insensitive substring of the card's title, id or dedup key. **A header, not a query parameter**, because the edge's access log records the request line with its query string (`deploy/traefik/traefik.yml:39`, `deploy/stacks/traefik.yml:54`, both `accessLog: {}`) and does not record request headers (§6). |
| query or `X-Board-State` | `shown` | at most nine `column:n` pairs, comma-separated; a value over 256 bytes after decoding is ignored whole (nine full pairs are about 100 bytes); split with a limit of ten parts; a key that is not a column id is dropped; `n` that is not a number or is below 1 is dropped, so that column gets the default 30; above 300 it is 300 | Cards wanted per column |
| query or `X-Board-State` | `rows` | integer; not a number or below 1 = absent; above 1,000 it is 1,000 | Table rows wanted |
| query | `more` | a column id, or `rows` | Add 30 to that column's `shown`, or 100 to `rows`, **then clamp again** |
| query or `X-Board-State` | `rev` | as `shown`, with `column:hash` (nine full pairs are about 220 bytes); hash = exactly 16 hex digits, anything else dropped | What the browser has (§5.1) |
| query | `surface` | `sisflow` | Answer with the count line only (§4.8) |

Not offered as filters, with the reason: task kind and vendor (both follow from the role), model and priority (shown to the operator nowhere).

### 4.3 The routes

| Route | Returns |
|---|---|
| `GET /products/{id}/board/live?…` (new) | HTML. Board view: one OOB element per column whose content differs from what the browser has (§5.1). Table view: the table body. Both: the count line (§4.5) and the Timeline count of the product bar, each as an OOB element. Does not call `buildBO`. `Cache-Control: no-store`. Address: `pushURL` with `replaceOnly` true and `Href` = `p/<slug>/board` plus `?` and `BoardPage.Query.Encode()` when that is not empty — it sends `HX-Replace-Url` when `HX-Current-URL`'s path is this Board and the query differs, and nothing otherwise; **never `HX-Push-Url`**. This holds for a request marked `X-GAOS-Refresh` too: a refresh that replaced the operator's own in-flight filter request (§4.5) carries the same form values, so the address still follows the filter, and the path check already covers an operator who has left the Board. |
| `GET /products/{id}/board` (existing, `main.go:537`) | JSON. With `paged=1`: the filter, `X-Board-Search` and `shown` apply, each column holds at most its `shown`, and the document gains `counts` (matching cards per column), `matched` and `all`, has `rows: []`, no `row` on any card, and `workflows: []`, and is sent `no-store`. Without `paged`: exactly today's document — the filter parameters are ignored. |

One new route. It is a back-office route (session required), `GET`, read-only, and is added to `readOnlyGET` in `auth_test.go:258`; the JSON route is already there.

There is no "next page" route. "Show more" is `board/live` with `more=<column>`: one column's content changes, so one column is in the response and it is replaced whole, with its new footer. Nothing is appended by offset, so a card that arrives in between cannot shift an offset and appear twice.

### 4.4 Width and layout

- On the Board tab the aside (`templates/backoffice.html:219`) is not rendered and the layout is one column. The aside's three blocks stay on the other eight tabs.
- `.bo-main[data-tab="board"]{max-width:none}` — the attribute is already there (`templates/backoffice.html:122`).
- One CSS variable, `--board-col:300px`, sets the width of `.board-col` and, from `static/app.css`, of `.sisflow-board-host .sfb-column` (280px in the vendored sheet, `static/vendor/sisflow/sisflow-board.css:90`). No vendored file changes.
- The server board gets the bounded height the sisflow host has (`static/app.css:1216`). Each column scrolls inside it. Its header stays at the top and shows `shown of matched` (`30 of 412`). Its footer holds the "Show more" button while `shown < matched` and `shown < 300`; at 300 it reads "Showing the first 300 of N. Filter, or use the table."
- **All nine column elements are always in the page**, each with a stable id (`col-<id>`) — also when the product has no task at all, in which case the line "No tasks yet. Create a workflow…" (`templates/backoffice.html:425`) is shown above them. With `empty=hide`, a column with no matching card carries the `hidden` attribute; it is still there for an OOB swap to replace when a card arrives.
- The inline styles of `pane-board` (`templates/backoffice.html:391-428`) move to classes in `static/app.css`. No new colour except `--fresh` (§5.4), defined in both themes.

### 4.5 The filter bar, the count line and the view switch

Rendered by the server at the top of `pane-board`, above both boards, from parts the Pull requests page already has (`static/app.css:1616`, `templates/backoffice.html:1056-1135`):

- **`form.af-form#boardFilters`**, `hx-get="products/<id>/board/live"`, `hx-target="#sink"`, `hx-sync="this:replace"`. Named controls: selects `role`, `story`, `label` and `wf` built from `Facets`, the checkboxes `waiting` ("Waiting on a person") and `empty` ("Hide empty columns"), and three hidden inputs holding the current `view`, `sort` and `dir`. It submits on `change`, on `input` in the search box after a 300 ms pause, and on the custom event `refresh`.
- **The search box, `input#boardSearch`, has no `name`.** Neither htmx nor a native form submit can serialise a control without a name, so the text cannot reach a URL even when no script of ours runs. Its value is printed by the server from `BoardWindow.Text`, so a re-rendered bar still shows what the board is narrowed by.
- **`nav.af-seg`** with two links, **Board** and **Table**: `href` = the page URL `p/<slug>/board?view=` or `…?view=table`; `hx-get` = the fragment route `products/<id>?tab=board&view=` or `…&view=table`. **A link names every parameter it sets, with an empty value when it clears one**, and the URL wins over the header (§4.2): Board carries `view=`; the table's header links (§4.6) carry `sort` and `dir`; `a.af-reset` "Clear filters" and the count line's button carry `role=&story=&label=&wf=&waiting=&empty=` and the attribute `data-board-reset`, on which the hook sends no search header. All of them go to the fragment route and re-render the sheet, so the links, the hidden inputs and the search box are always current; the path is the same and the query differs, so the address is replaced, not pushed (§3.4).
- **The count line**, `#boardCount`, under the bar, replaced by every `board/live` response: `412 cards`, or `37 of 412 cards match`. When nothing matches: "No cards match these filters." and a "Clear filters" button. While sisflow is mounted it also lists the nine columns, each as `label shown of matched` with a "Show more" button under the same conditions as §4.4 (§4.8).

**One hook keeps the server informed, and it never edits a URL or a body.** `static/live.js` registers an `htmx:configRequest` listener. While the Board tab of a product is on screen, for every htmx request whose path is `products/<that product>` or starts with `products/<that product>/` — the form, a "Show more", a view, sort or reset link, a move button, a drag-and-drop, a send-back, a label, and every other mutation made from this tab — it sets two request headers:

1. `X-Board-State`: a URL-encoded string of the form's nine named controls (`view`, `role`, `story`, `label`, `wf`, `waiting`, `empty`, `sort`, `dir` — an explicit list in the script, not "whatever the form holds") plus `shown` and `rev` (or `rows`, in the table) read from the DOM at that moment;
2. `X-Board-Search`: `encodeURIComponent` of `#boardSearch`'s value, when it is not empty and the element that issued the request has no `data-board-reset`.

`buildBO` builds the Board tab from `parseBoardQuery(r)` whatever the request's method. So a mutation that still answers with the whole sheet (§5.1) re-renders the same filter, view, depth and search text, and the address — which a mutation never rewrites — keeps matching the screen. A request that arrives with neither the header nor the parameters (no JavaScript, or a test) is answered at the default depth, unfiltered, with every column.

**One request at a time, and the last one wins.** Every `board/live` request is issued by `#boardFilters` or by an element that names it in `hx-sync="#boardFilters:replace"` (the "Show more" buttons). A live refresh does not issue its own request: `live.js` sets a module flag and triggers the form's `refresh` event; the hook, on the next request it sees from the form, reads the flag, clears it and adds `X-GAOS-Refresh`. Nothing else sets the flag — a "Show more" on sisflow triggers `refresh` without it. htmx's `replace` aborts the request in flight when a newer one starts, so a filter change and a refresh cannot be applied out of order.

### 4.6 The table

Server-rendered only, in the existing `.tm-scroll.af-scroll > table.af-table` frame, with one `<tbody id="boardRows">`. When `view=table`, neither board nor the sisflow host is in the page, so sisflow does not mount.

| Column | Content |
|---|---|
| ID | Task id; opens the card's timing sheet, as the timing chip does today |
| Title | Title, then the label chips |
| Status | The exact status; the "Needs you" badge on `AWAITING_INPUT`, as on a card |
| Role | Role and the vendor badge |
| Story | Story id, or "Plan" |
| Workflow | Workflow id |
| Attempts | The conversation chip, when there is one |
| Lead time | The timing chip |
| Actions | One button per legal move — the same `PUT …/move` the cards use |

- `sort` = `status` (board order, then the column's own order of §4.1; the default), `id`, `role`, `story`, `updated` (the `Updated` field; newest first when `dir` is absent). `dir` = `asc` | `desc`. Lead time is not sortable: sorting on it would compute timing for every matching card, which §4.1 exists to avoid.
- There is no grouping. Status, story and workflow are columns that sort, and filters.
- 100 rows; a footer "Showing 100 of N" with a "Show 100 more" button (`more=rows`) while fewer than N and fewer than 1,000 are shown; at 1,000 it reads "Showing the first 1,000 of N. Filter to see the rest." Every `board/live` response replaces the whole `<tbody>` and the footer, so the order is always the server's.
- Column headers are links that set `sort` and `dir` (§4.5), with `aria-sort`.

### 4.7 What disappears from the screen

Two things, both on the Board tab only:

- **The aside** (graph projection, story coverage, planner readiness) — §4.4. It remains on the other eight tabs.
- **Story lanes in sisflow.** A lane multiplies the cells by nine for every story, and the lanes are what grew the board to 13,000px for forty stories (`main_test.go:755-762`). The server board never drew them. The story becomes a chip on the card (from `BoardCard.Story`), the `story` filter, and a sortable column in the table. For sisflow this means the paged document has `rows: []` and no `row` on any card (§4.3); `MoveCard` already accepts an empty row. sisflow draws one lane for cards with no row and `board.js` labels it "Plan" today (`static/board.js:192-195`, `:222`); with every card in that lane the label would be wrong, so `board.js` sets that label to an empty string and `static/app.css` hides the single lane's header inside `.sisflow-board-host` (checked in the exit). The unpaged JSON keeps its rows, so `TestTheBoardHasItsColumnsEveryTaskOnceAndALanePerStory` (`board_test.go:62`) stands unchanged. sisflow was not on screen from v0.44.0 until v0.54.1.

### 4.8 The same features on sisflow

sisflow keeps what it adds — its drag and drop and its card faces — and takes its data, its depth and its totals from the server, exactly as the server board does. **It does not use the library's own paging.**

| Feature | On sisflow |
|---|---|
| Loading | `static/board.js` keeps, **per product id in module state** (not per host element), a per-column depth map (30 each the first time) and the board's scroll position. It has one function, `reload()`: fetch `GET …/board?paged=1` with the same two headers the hook sets (`X-Board-State` built from the same explicit list of nine keys plus `shown` from the depth map, and `X-Board-Search`), so its URL carries no filter and no search text; then hand the document to the board (§5.2). |
| After a sheet swap | A mutation that re-renders the sheet (§5.1) creates a new host element, and `mountBoard` mounts sisflow on it again (`static/app.js:441-442`). Because depth and scroll are keyed by product, the new mount loads at the depths the old one had and restores `.sfb-body`'s scroll after its first render. |
| Filters | When `board.js` mounts it sets a hidden `surface=sisflow` input in the form, so the form's `board/live` request is answered with the count line only. `board.js` listens for `htmx:afterRequest` on the form and calls `reload()`. |
| Search | sisflow's own search box is hidden with CSS: it filters only the cards already loaded, which would disagree with the server's count. The filter bar's search is the search. |
| Show more | The count line's per-column buttons (§4.5). `board.js` handles the click itself: it adds 30 to that column in its depth map (to at most 300) and triggers the form's `refresh`, which redraws the count line and, through `htmx:afterRequest`, calls `reload()`. On sisflow the hook's `shown` comes from the depth map (`board.js` exposes it on the host element as `data-shown`), not from column elements. |
| The library's paging | Stays off. The `loadPage` that v0.54.1 added (`static/board.js:211`) keeps answering "nothing more": the vendored library emits a page-loaded event that nothing in it listens to, never sets its own `exhausted` flag (`static/vendor/sisflow/board/view/virtual-list.js:25`), and stops a column for good after a page that adds no card. This design does not build on it. |
| Totals | In the count line, which the server renders. sisflow's own badge keeps showing how many cards are loaded. |
| Width | The CSS variable of §4.4. |
| Table | Not in sisflow; the table is the server's (§4.6). |

## 5. Live updates and their cost (phase 2)

### 5.1 The server board: only what changed

- Each column element carries `data-shown`, the number of cards in it, and `data-rev`: 16 hex digits, the FNV-1a 64-bit hash of the JSON encoding of an explicit struct — not of the card as the API serialises it:

  ```go
  type colRev struct {
      Hidden  bool
      Matched int
      Cards   []cardRev
  }
  type cardRev struct {
      ID, Title, Status, Role, Vendor, Story, Workflow, Reason string
      Rank     int
      Labels, Moves []string
      Qa           *BoardQa
      Conversation *BoardConversation
      SendBack     *BoardSendBack
      // From Timing, only what does not depend on the clock:
      TimingKnown, TimingComplete, TimingFinished bool
      CreatedAt int64
      Attempts  int
      LeadText, TitleText string // set only when TimingFinished; empty while the clock runs
  }
  ```

  `CardTimingSummary`'s `AsOf` and its seven `…Seconds` fields (`timing.go:43-50`) are computed from the current time for an unfinished card and are **not** in the hash; neither are the two texts until `Finished` is true. The browser already re-ticks the running lead time from `data-elapsed-since` (`static/live.js:183-186`). A field the card template prints must be in `cardRev`; `TestBoardLiveSendsOnlyTheColumnsThatChanged` changes each printed field in turn and expects the column to be sent.
- On a `task` hint for the product on screen, `static/live.js` triggers the form's `refresh` (§4.5); the hook adds the filter, `shown` and `rev`.
- The server builds the page at those depths and writes into the response only the columns whose hash differs from the one sent. A move changes two columns; ticking "Hide empty columns" changes the empty ones; a hint about another product's card changes none.
- The board's own element, `.board-ssr`, is never replaced by a `board/live` response, so its horizontal scroll survives without any code. For each column that is replaced, `live.js` saves and restores that column's `scrollTop`.
- **A move** made on this board answers the same way. `handleMoveCard`'s htmx branch (`handlers_board.go:50`, the `renderBOAfter` call at `:79`) builds the `board/live` response from `parseBoardQuery(r)` — the hook sent the filter, `shown` and `rev` in `X-Board-State` — and writes it with the toast and the chrome (`RenderWithChrome`, which refreshes the "needs you" chip), instead of the whole sheet. In the table view it writes the table body. It sets no address header: it is not a `GET` (§3.4).
- **Every other mutation made from the Board tab still answers with the whole sheet**, because each of them also changes something outside the columns: send-back (`handlers_board.go:149`) and set-label (`:226`), archive or restore a conversation (`handlers_conversation.go:121`), answering a question from the board (`handlers_questions.go:86`), closing a defect (`handlers_autofix.go:143`, `:164`). With the hook and `parseBoardQuery`, that sheet is rendered with the same filter, view and depth. And `live.js` keeps the scroll across any replacement of `#boSheet` that stays on the same product's Board: an `htmx:beforeSwap` listener records `.board-ssr`'s `scrollLeft` and each column's `scrollTop`, and `htmx:afterSwap` restores them. (This replaces `keepPlace`'s narrower version, `static/live.js:63`. sisflow keeps its own, §4.8.)

### 5.2 sisflow: reload the document, keep the board

Today every hint and every save destroys the board and builds a new one (`static/board.js:184`, `:217`, `:233`). Both become `reload()` (§4.8), which hands the document to the board's store; the store redraws once and keeps the scroller. The 250 ms `setTimeout(create)` after a save goes away. A hint triggers the form's `refresh` like on the server board, and the count line's arrival triggers `reload()`.

The store is reached as `board._store` (`static/vendor/sisflow/board/index.js:121`), an underscore member of the vendored library at the version in the tree. `board.js` checks that `board._store && board._store.load` exists; if not, it falls back to today's destroy-and-create.

### 5.3 Fewer requests

- Hints are collected for 250 ms after the last one, and for at most 1 s after the first, before the form's `refresh` is triggered. `hx-sync` (§4.5) keeps one request in flight.
- While `document.hidden`, a hint only marks the board dirty. When the tab is shown again, one refresh is triggered. The stream itself stays open (`static/live.js:198`).
- `busy()` (`static/live.js:57`) holds a refresh while a dialog is open or an input inside the sheet has focus. Two changes: an input inside `#boardFilters` no longer counts, because the filter bar is not replaced by a refresh; and a drag in progress on the server board now counts (new — today only `board.js` checks for one, `static/board.js:234`).

### 5.4 Showing that something changed

**The browser decides; the server never emits the class.**

- Server board: before a column is replaced by a response to a request marked `X-GAOS-Refresh` (the request's headers are on the swap event, `evt.detail.requestConfig.headers`), `live.js` records the `data-card` ids in it. After the swap, a card whose id was not in that column before gets the class `is-fresh`. Nothing is marked after a filter change, a "Show more" or the first render.
- sisflow: `board.js`'s `htmx:afterRequest` listener reads the same header from the finished request and passes it to `reload()`, which, when it is set, compares each card's column with the document it is replacing and sets the card's `css` field to `is-fresh`, joined by a space to `is-needs-you` when the card has both (`static/board.js:203` sets the latter today).

`is-fresh` is a 3px bar on the card's left edge, drawn on `::after`, that appears at once and fades out (opacity only, 150 ms, `--ease-out`) after 1.5 s. The colour is a new variable, `--fresh`, in both themes. Under `prefers-reduced-motion` the bar appears and is removed without a fade. Cards do not move, grow or slide.

### 5.5 Server work that the Board tab does not need

- `buildBO` copies the event stream on every product tab only to print a number: the Timeline tab's label is `len .Timeline` (`templates/backoffice.html:77`), and `Store.Events` is the only event read on the interface (`store.go:70`). A new method, `EventCount(ctx context.Context, productID string) (int, error)`, is added to `Store`: `MemStore` counts the events of that product under the read lock without copying — a walk of the tenant's event stream that allocates nothing, so its time grows with the stream, not with the board; `BenchmarkBoardLive` runs with the synthetic product's 12,000 to 60,000 events to hold it to §5.6. `ArcadeStore` delegates to its projection. `ProductDetail` gains `TimelineN int`; the label prints it inside `<span class="n" id="navTimelineN">`, which is the element `board/live` re-sends (§4.3); and `Timeline` is filled only for `tab=timeline`.
- Sequence diagrams are rendered only for `tab=diagrams` (`handlers_bo.go:338-344`).

Both apply to every product tab, not only the Board.

### 5.6 Targets

`board_bench_test.go` holds five benchmarks over the synthetic product of §1.2, each at 200, 2,000 and 10,000 cards:

| Benchmark | Measures |
|---|---|
| `BenchmarkBoard` | `Store.Board`, the unpaged document |
| `BenchmarkBoardPage` | `Store.BoardPage`, default window |
| `BenchmarkBoardCard` | `Store.BoardCard`, one card |
| `BenchmarkBoardTab` | `GET /products/p-1?tab=board` through the router: time, allocation, response bytes |
| `BenchmarkBoardLive` | `GET /products/p-1/board/live` with the current `rev`s after one move: time, allocation, response bytes |

| Measure | Today | Target | From |
|---|---|---|---|
| Cards in the first render, any board size | all | ≤ 270 (9 × 30) | `TestTheBoardTabRendersNoMoreThanItsLimit` |
| `BoardPage`, 2,000 cards | 25 ms (`Board`) | ≤ 3 ms | `BenchmarkBoardPage/2000` |
| `BoardPage`, 10,000 cards | 361 ms (`Board`) | ≤ 15 ms | `BenchmarkBoardPage/10000` |
| The read behind a drag's answer, 2,000 cards | 25 ms (`Board`) | ≤ 0.5 ms | `BenchmarkBoardCard/2000` |
| Opening the Board tab, 2,000 cards: time / allocated / response | 109 ms / 94 MB / 1.83 MB | ≤ 15 ms / ≤ 10 MB / ≤ 300 KB (270 cards × 0.9 KB, plus the sheet) | `BenchmarkBoardTab/2000` |
| One live refresh after a move, 2,000 cards: time / allocated / response | 109 ms / 94 MB / 1.83 MB | ≤ 10 ms / ≤ 8 MB / ≤ 60 KB (2 columns × 30 cards × 0.9 KB = 54 KB) | `BenchmarkBoardLive/2000` |
| Elements under `#boMain`, 2,000 cards | measured before the templates change | ≤ 4,000 (270 cards × about 12 elements a card, `templates/backoffice.html:406-417`, = 3,240, plus the bar, the nine columns and the title row) | browser, below |
| Detached DOM nodes after 50 live refreshes | measured before the templates change | 0 | browser, below |

Browser measures, in Chrome DevTools on a product seeded with 2,000 Done cards: `document.querySelectorAll('#boMain *').length` in the console; then move one card back and forth from a second window 50 times, take a heap snapshot, and filter it for "Detached".

**A missed target blocks the phase's exit** unless the operator accepts the measured number, in which case the number and the acceptance are written into §12.

## 6. Security

- **Signed out, every new route is refused with `401`** (§3.7), as BR-55 requires. The page routes return the same sign-in document `GET /{$}` returns signed out, built in the gate without calling the handler, so it reveals no product, slug or count.
- **`TestEveryRouteIsClassified` (`auth_test.go:290`) changes in five ways.** Its assertion that a signed-out request without `HX-Request` gets `401` **JSON** (`auth_test.go:332-333`) is narrowed to patterns outside `pagePatterns`. For every pattern in `pagePatterns`, with no cookie and with a forged cookie: the status is `401` with and without `HX-Request`; without it, the body equals the signed-out `GET /{$}` body byte for byte and `Cache-Control` is `no-store`; and a counting stub in place of the page handlers sees zero calls. `pagePatterns` must be a subset of the registered patterns and share no member with `publicPatterns` or the bearer set. Every non-bearer route must send `Referrer-Policy: same-origin`. The eleven page patterns are added to `readOnlyGET` (`auth_test.go:258`) in phase 1 and the Board's new route in phase 2, each with its reason.
- **What reaches a response header.** `HX-Push-Url`, `HX-Replace-Url` and `Location` are built from `sitePrefix(r)`, which already rejects anything that is not a plain absolute path (`render.go:192-209`); from a path made of constants, a validated section id and a slug; and from a query that is **client-supplied values under an allow-list of keys** (§3.4, §4.2), each bounded in length and re-encoded with `url.Values.Encode()`. They are escaped and same-origin; they are not free of client input, and this design does not claim so. `HX-Current-URL` is compared, never echoed. `{section}` is validated before a redirect is written. `TestTheProductIdRedirectsToTheSlug` sends an unknown parameter and an unknown section and asserts neither is in a `Location`.
- **A stored slug is validated when it is loaded.** The address-safety of a slug must not rest on how it was written: products are edited in the database by hand. `load` and `NewMemStore` accept a stored slug only if it matches `^[a-z0-9]+(-[a-z0-9]+)*$`, is at most 64 bytes, is not shaped like another product's id, and is not held by a product with a lower numeric id; otherwise it is treated as absent and `backfillSlugs` assigns a new one (logged by id only).
- **Every new parameter is bounded, whether it arrives in the URL or in `X-Board-State`** — §4.2's tables give each rule, and the two headers are the only ones the Board reads; none of them reaches a response unless it matched a known id, and filter values the product does not offer are cleared before use (§4.1 step 2). The search text is used in `strings.Contains` and printed through `html/template`.
- **What becomes visible outside the session.** Until now a product appeared in a request line only as its id. With this design the following are in the address, and therefore in the edge's access log, in the browser's history and (for the name) in the tab title: **the slug, which is the product's name in lower case**; the Board's filters (`view`, `role`, `story`, `label`, `wf`, `waiting`, `empty`, `sort`, `dir`); and, on the global views, `product`, `state`, `kind`, `difficulty` and `open` — which the fragment routes' request lines already carry today. The id form of a URL redirects to the slug, so it does not avoid this. The name in the URL is the operator's decision; D-093 records the full list above as accepted. These addresses are logged twice over: as the request line of a page load, and as the `Referer` of every later request the page makes — the edge's default log format records `Referer` and `User-Agent` beside the request line. Where that log is kept and for how long is the deployment's existing arrangement and is not changed here. **The search text is the one thing kept out**: it travels in the request header `X-Board-Search` (§4.2), which that format does not record, and never in a URL, on both boards. The control is structural, not a script's good behaviour: the search box has no `name`, so no form submit can carry it (§4.5), and no handler reads a `q` parameter. `TestTheSearchTextNeverTravelsInAURL` asserts both on the rendered Board tab and on requests: the form has no control named `q`; the search input has no `name` attribute; and a request with `?q=x` and no header is answered unfiltered.
- **No back-office HTML is stored in the browser** (§3.5), and every response that carries the sheet for the archive, integrations, auto-fix or pull-request view is `no-store`, now from the one place that renders it (§3.4), as are `board/live` and the paged JSON.
- The Content-Security-Policy is unchanged; `base-uri 'self'` still holds because `<base>` is unchanged.

## 7. Failure and recovery

| What fails | What the operator sees |
|---|---|
| A `board/live` request fails or times out | The board stays as it was; the next hint, or the stream reconnecting, tries again. The existing "live updates paused" dot covers a lost stream (`static/live.js:34`). |
| "Show more" fails | The button stays; the existing error toast is shown. |
| `board.js` does not load | The server board, with every feature of §4. |
| sisflow's `reload()` fetch fails | The board stays as it was and the status line under the bar says so (`say`, `static/board.js:174`); the next hint or filter change tries again. |
| The slug back-fill cannot write to the database at boot | A log line with the product's id. It fails the way any mutation's write fails today (`mutate`, `store_arcade.go:533`): the process keeps the slug and its event in memory, the database has neither, and the next boot — which finds no stored slug — assigns the same one and writes both. The boot continues. |
| A request arrives without the hook's headers | Every column, at the default depth, unfiltered (§4.5). |
| A refresh computed before a move commits arrives after the move's own answer | The two columns briefly show the older state; the move's own hint triggers a refresh that corrects them. Accepted: the move's `PUT` is not queued behind the form. |

## 8. Alternatives considered

| Alternative | Why not |
|---|---|
| Put `hx-push-url` on every link | Some thirty attributes in the templates, and it cannot cover a mutation that changes the view — creating a workflow opens the Board (`handlers_cw.go:87`). `sheet` is one function with a test that it is the only door. |
| One URL for both shapes (`/products/p-6?tab=board` returns a page or a fragment by `HX-Request`) | Needs `Vary`, risks a cached fragment shown as a page on Back, and `GET /products/{id}/board` is already the JSON route. |
| The product id in the URL (`/p/p-6/board`), or the id with the name attached (`/p/p-6-route-planner/board`) | Both were put to the operator with the bare name, and the operator chose the bare name. The id-with-name form needs no stored field, no back-fill and no collision rule, and that cost was weighed: the stored slug is what §3.2 adds for a shorter, cleaner address. The id form still resolves, by redirect. |
| Derive the slug from the name at boot instead of storing it | Of two products with one name, the survivor's derived slug would change when the other is removed by hand (§3.2). |
| Answer a signed-out page request with `200` and the sign-in form | BR-55 refuses every non-public route; `401` with the same body gives the browser the same form and keeps the rule and its test. |
| htmx's history cache (the default) | Stores back-office HTML in `localStorage`, and restores a snapshot that includes whatever was in `#sink`. |
| Build the Board features on one board only | Decided by the operator: both stay. |
| sisflow's own scroll-to-load paging | The vendored library's paging cannot be told a column is finished and stalls after a page that adds nothing (§4.8); it cannot be edited. A button and a deeper reload use one mechanism for both boards. |
| "Next page" routes that append by offset | A card arriving in between shifts the offset and the appended page repeats a card, with duplicate element ids. |
| Send all nine columns on every hint, with no hash | It keeps the scroll positions too, and needs no `rev`. Its size is not the argument: about 243 KB before compression, and repeated card markup compresses well. The argument is the browser: 270 card subtrees thrown away and re-created per hint, per open window, with their listeners and their running clocks, during exactly the bursts of runner events the operator watches — which is the "smoother" the operator asked for. The cost on the other side is one struct, one hash and one attribute (§5.1). Raised by the zoom-out lens in two rounds and declined both times; it is put to the operator at approval. |
| Hash the rendered HTML of a column | The lead-time text changes every minute, so every column with an open card would be re-sent on every hint. |
| A DOM-morphing library (idiomorph) | A new vendored library, which needs the operator's approval; per-column replacement is enough at 30 cards a column. |
| Carry the Board's state on the URL of every request, mutations included | The hook would rewrite the URL of every `PUT` and `POST` made from the tab, and a body field named `label` would collide with the filter. Two headers leave every URL and body alone. |
| The search text as a query parameter, or the access log configured to drop queries | The first writes it to the log. The second is a change to the edge of every tier for one field, and a later change to the log format would undo it silently; a header needs neither. |

## 9. Change list

**Phase 1 — `feat/urls`**

| File | Change |
|---|---|
| `urls.go` (new) | `newSlug`, `backfillSlugs`, `validSlug`, `resolveProduct`, `sheet`, `pushURL`, `handlePage`, `handleProductPage`, `pageTitle` |
| `urls_test.go` (new) | The ten tests of §10.1 |
| `model.go`, `view.go` | `Product.Slug`; `BOView.Href`, `BOView.PageTitle`; `ProductCard.Slug`, `ProductDetail.Slug` |
| `store_mem.go`, `store_arcade.go` | Assign in `CreateProduct`; `MemStore.AssignSlug`; the back-fill at construction; write, read and validate `slug` |
| `main.go`, `auth.go` | Eleven routes; `pagePatterns`; the gate's signed-out page body; `Referrer-Policy` |
| `handlers_bo.go`, `handlers_cw.go`, `handlers_conversation.go`, `handlers_questions.go` | `sheet` in place of `buildBO` (§3.4's table); `Href` and `PageTitle` in `buildBO`; `handleBOSheet` renders `bo-main-oob`; `handleLogin`'s refresh |
| `handlers_tasks.go` | `handleIndex` is unchanged; listed because the title it renders now comes from `BOView.PageTitle` through `templates/page.html` |
| `prview.go`, `autofix_view.go` | `prPageHref`, `afPageHref`; the `PageHref`, `ResetPageHref` and `ClosePageHref` fields (§3.6) |
| `templates/page.html`, `templates/backoffice.html`, `templates/workflow.html` | The two htmx keys, the title expression, `bo-title`, links instead of buttons, page `href`s |
| `static/app.css`, `static/app.js`, `static/live.js` | Link styles; the modified-click listener; `X-GAOS-Refresh`, the timer's re-check and the scoped `deferred` |
| `autofix_view_test.go` | Four assertions that quote the old markup change to the new: `:143` (the Auto-fix navigation item is an `<a>`), `:166`, `:237` and `:240` (`href`s are `autofix…` page URLs, `hx-get`s unchanged) |
| `main_test.go`, `auth_test.go`, `store_conformance_test.go` | §10.1 |
| `deploy/rebuild-restart.sh` | One check after the base-href check (`:396-398`): signed out, `/app/p/x/board` answers `401` and its body has `<base href="/app/">` — the prefix survives the strip on a deep path |
| Registers | `pustaka/docs/product/qa-test-plan.html`, `pustaka/docs/assets/toc/product.js`, `pustaka/docs/assets/toc.js`, `pustaka/docs/changelog.html`, `pustaka/docs/product/decision-log.html` (D-093), `README.md`, `deploy/check-workspace.sh`, `.claude/rules/qa-test-plan.md`, the cache-busters in `templates/page.html` |

**Phase 2 — `feat/board`**

| File | Change |
|---|---|
| `board_page.go` (new) | `BoardQuery` with `Encode`, `BoardWindow`, `parseBoardQuery`, `BoardPage`, `BoardFacets`, `MemStore.BoardPage`, `MemStore.BoardCard`, the column hash, the `board/live` handler |
| `board_page_test.go`, `board_bench_test.go` (new) | §10.2, §5.6 |
| `board.go`, `conversation.go`, `sendback.go`, `timing.go` | `BoardCard`'s three fields and `Row`'s `omitempty`; the id map in the two decorators; timing for a given set of cards |
| `store.go`, `store_mem.go`, `store_arcade.go`, `store_arcade_blueprint.go` | `BoardPage`, `BoardCard` and `EventCount` on the interface and both stores |
| `handlers_board.go`, `handlers_bo.go`, `main.go`, `urls.go` | The JSON's `paged` form; the move's board answer; `BoardCard` at the three single-card reads; `buildBO` uses `parseBoardQuery`, `BoardPage` and `EventCount`, renders diagrams only on their tab, and adds the Board's query to `Href`; one route |
| `view.go` | `ProductDetail.BoardPage`, `ProductDetail.TimelineN`, the filter-bar view model |
| `templates/backoffice.html` | `pane-board` rewritten as separate templates: bar, count line, board, column, card, table; `navTimelineN` |
| `static/app.css`, `static/app.js`, `static/live.js`, `static/board.js` | §4.4, §4.5, §4.8, §5 |
| `auth_test.go`, `main_test.go`, `store_conformance_test.go` | The route in `readOnlyGET` and the route tables; three cases in the conformance table |
| Registers | As phase 1, with D-094 |

Five existing tests render the Board tab and assert on it: `board_test.go:65` (the string `board-card`), `conversation_test.go:125` and `:147` (`id="cv-<id>"`), `needs_test.go:165` (the "Needs you" badge), `timing_test.go:284` (the timing chip) and `design_iterate_test.go:525` (the send-back button). The card template keeps every class, id and `data-` attribute it has today (`templates/backoffice.html:406-417`), and each of those tests looks at a product with fewer than thirty cards in a column, so **they are expected to pass unchanged**. If one does not, it is fixed in the same commit, its register row's prose is updated, and the pull request lists it.

## 10. Phases, tests and exits

One `func Test…` is one register row, and so is one check in a deploy script (`.claude/rules/qa-test-plan.md`). The highest id in use after v0.54.1 is TC-721; there are 587 Go tests and ninety-one decision-log entries.

### 10.1 Phase 1

**Ten new Go tests (TC-722 … TC-731) and one deploy check (TC-732): register 721 → 732; Go tests 587 → 597; decision log ninety-one → ninety-two.**

| Test | Asserts |
|---|---|
| `TestASlugIsAssignedOnceAndIsUnique` | Two products named alike get `x` and `x-2`; an empty name's slug is the id; a name that slugs to `p-7` does not shadow product `p-7`; the back-fill leaves a valid slug alone, is deterministic, and replaces a stored slug that is malformed or a duplicate |
| `TestTheProductIdRedirectsToTheSlug` | `/p/p-1/stories?junk=1` → `302`; the `Location` has the prefix and the slug and not `junk`; an unknown section is `404`, not a redirect |
| `TestADeepURLRendersThePage` | Each of the ten page patterns returns a full document whose `#boMain` has the right `data-view` — and, for a product section, `data-product` and `data-tab` — with `no-store` and the title of §3.3; `/p/{ref}` answers `302`, and `404` for an unknown ref |
| `TestASignedOutDeepURLShowsOnlyTheLogin` | `401`; the body for an existing slug equals the body for a missing one and the signed-out `/` body; it contains no product name |
| `TestSignInOnADeepURLRefreshesInPlace` | With `HX-Current-URL` on `/p/x/board`: `204`, `HX-Refresh: true`, the session cookie set. On `/`, and with the header missing: today's answer |
| `TestNavigationPushesReplacesOrLeavesTheAddress` | The four rows of §3.4's table, with and without a prefix; a different parameter order or escaping in `HX-Current-URL` is "same"; a request with `X-GAOS-Refresh` sets no header even when the path differs; a missing product sets none; every sheet response starts with a non-empty `<title>`, sign-out included; every sheet response for the archive, integrations, auto-fix and pull-request views is `no-store`, the retarget and conversation answers included |
| `TestOnlyTheAllowedFunctionsCallBuildBO` | The source has `buildBO(` only inside the five functions §3.4 names |
| `TestEveryHtmxLinkHasAPageHref` | No `<a … hx-get>` in any rendered view has an `href` starting with `ui/` or `products` |
| `TestAnUnknownProductOrSectionIsNotFound` | `404`, a full page, the toast |
| `TestTheBrowserKeepsNoHistoryCache` | `templates/page.html` has exactly one `htmx-config` element and it holds both keys |

Changed, keeping their ids: `TestEveryRouteIsClassified` (§6), `TestEveryRouteAnswers` (the eleven routes), `TestBaseHrefFollowsForwardedPrefix` (a deep path), the four Auto-fix view tests that hold the assertions listed in §9, and one new case in the store conformance table — a slug survives a reopen (covered by TC-124 and TC-125). The failed-write path of the back-fill has no test of its own: it is `mutate`'s, unchanged, and injecting a write failure would need a fault hook the ArcadeDB store does not have.

*Exit:* the commands of §10.3 pass; and in a browser, with and without the `/app` prefix: sign out, open `/p/<slug>/board`, see the sign-in form, sign in, land on the Board; click through every tab and every global view and read the address and the tab title; create a workflow and read `/p/<slug>/board`; open a Pull requests drawer and read `open=` in the address without a new history entry; navigate from the Board to Stories while a card is moving in a second window, and stay on Stories with its address; Back to the first entry and Forward again; reload; Ctrl-click and middle-click a tab and get a new tab on the right page; open `/p/p-1/board` and read the slug in the address.

### 10.2 Phase 2

**Fifteen new Go tests (TC-733 … TC-747): register 732 → 747; Go tests 597 → 612; decision log ninety-two → ninety-three.** Benchmarks are not tests and get no row.

The server half lands first within the branch: `board_page.go`, its tests and the benchmarks. *Checkpoint:* the eight server-half tests — the first six rows below, `TestTheBoardQueryRoundTrips` and `TestTheBoardJSONPagesOnlyWhenAsked` — pass with `go test -run 'BoardPage|BoardFilters|DoneIsNewest|BoardCardIs|BoardQueryRound|BoardJSONPages' .`, and `BenchmarkBoardPage` and `BenchmarkBoardCard` meet §5.6, before any template changes.

| Test | Asserts |
|---|---|
| `TestBoardPageCapsEachColumnAndCountsTheRest` | 30 by default; `Matched` is the column's full matching count; `shown` above 300 is 300; one that is below 1 or not a number gives that column the default; a `shown` value over 256 bytes is ignored whole; a full nine-pair `rev` is accepted |
| `TestBoardPageDeeperWindowsContainShallowerOnes` | A column at `shown` 60 starts with the same 30 cards as at 30 |
| `TestBoardFiltersNarrowCardsAndCounts` | Each filter of §4.2 alone and two together; the search header, printed back as the box's value; a parameter in the URL wins over `X-Board-State`, also when empty; a value the product does not offer — a label in the vocabulary that no task has, included — is cleared from `BoardPage.Query`; a body field named `label` is not read as the filter |
| `TestDoneIsNewestFirst` | Done and Canceled by last event; the others by rank |
| `TestBoardPageDecoratesOnlyThePage` | The number of card timings computed equals the cards returned, at 30 and at 3,000 tasks |
| `TestBoardCardIsTheSameCardTheBoardHolds` | `BoardCard` equals that card in `Board`, apart from the three new fields; an unknown id is `ErrNotFound` |
| `TestTheBoardTabRendersNoMoreThanItsLimit` | The Board tab of a 500-card product has 30 cards in Done, `30 of N` in its header and a "Show more"; a product with no task has nine columns and the "No tasks yet" line |
| `TestShowMoreDeepensOneColumn` | `board/live` with `more=done`: one column in the response, 60 cards, `data-shown="60"`; at 300 the button is gone and the footer says so |
| `TestBoardLiveSendsOnlyTheColumnsThatChanged` | With the current `rev`s: no column, also after the clock moves a minute. After one move: exactly two, and the Timeline count. Toggling `empty=hide` sends the empty columns, `hidden` or not. Without `rev`: all nine |
| `TestBoardLiveNeverPushesTheAddress` | On the Board with a different query: `HX-Replace-Url`, with `?` only when the query is not empty — also for a request marked `X-GAOS-Refresh`. With `HX-Current-URL` on another tab: no header. `no-store` |
| `TestABoardMutationKeepsTheView` | An htmx `PUT …/move` with the filter, `shown` and `rev` in `X-Board-State`: two columns, no `id="boSheet"`, no address header; with `view=table`, the table body. A `POST …/label` from a filtered, searched Board: the whole sheet, with the same filter applied and the search box holding the text. From the table, the Board link (`view=`) shows the board; "Clear filters" clears every filter and the search |
| `TestTheTableSortsAndCaps` | Order per `sort`/`dir`, 100 rows, `more=rows` gives 200, the footer at 1,000 |
| `TestTheBoardQueryRoundTrips` | Parsing `q.Encode()` returns `q`; defaults encode to an empty string; `shown`, `rows`, `rev`, `more` and `surface` are never in `Encode()` or in an address header |
| `TestTheSearchTextNeverTravelsInAURL` | §6: no control named `q`, a search input without `name`, and `?q=x` without the header answered unfiltered |
| `TestTheBoardJSONPagesOnlyWhenAsked` | With `paged=1`: per-column `shown`, `counts`, `matched`, `all`, `rows: []`, no `row` on any card, `workflows: []`, `no-store`, the filter from the URL or from `X-Board-State`. Without it: today's document, byte for byte, filter parameters ignored |

Changed, keeping their ids: `TestEveryRouteIsClassified`, `TestEveryRouteAnswers`, `TestTheBoardHostHasABoundedHeight` (the server board's height too), `TestNavigationPushesReplacesOrLeavesTheAddress` (the Board's query in `Href`), and three cases in the store conformance table (`BoardPage`, `BoardCard`, `EventCount`; covered by TC-124 and TC-125).

*Exit:* the commands of §10.3 pass; every target of §5.6 is met or accepted; and in a browser, on a product seeded with 2,000 Done cards, **once with sisflow mounted and once with `static/board.js` blocked**: Done shows 30 and "Show more" adds 30; each filter narrows the board, the count line and the address; the search narrows the board, does not appear in the address, and does not appear in any request URL in the Network panel; change a filter and then switch to the table and back, and the filter holds; "Clear filters" clears the selects and the search box; sisflow shows no "Plan" lane header; the table sorts and pages; a reload keeps the filter and the view; set a label and send a design back from a filtered, scrolled Board and keep the filter and both scroll positions; a move made in a second window changes two columns in the first, which keeps both scroll positions and marks the card; a hidden tab makes no request and one on return; both themes; keyboard only; reduced motion. Then the `review-animations` and `break-ui` passes the frontend rule requires (`.claude/rules/frontend.md`).

### 10.3 Commands

```bash
cd dashboard && go vet ./... && go test -race ./...
cd dashboard && go test -run xxx -bench 'Board' -benchmem     # phase 2
cd pustaka && go run . check ./docs && go test ./...
./deploy/check-stacks.sh && ./deploy/check-workspace.sh
```

## 11. Risks

| Risk | Answer |
|---|---|
| htmx does not reload on a history entry it did not create | The spike of §3.5 covered the first entry and a direct load. If a browser differs, three lines in `static/app.js` reload on `popstate`. |
| The capture-phase listener does not keep htmx from a modified click | It is in the phase-1 exit. The fallback is a `data-href` attribute and `window.open` from the same listener. |
| `board._store` is not a public member of sisflow | Feature-checked, with today's behaviour as the fallback (§5.2). |
| Operators relied on story lanes or the aside on the Board | §4.7; the aside is one click away on any other tab. |
| The product name is now in URLs and logs | Stated in §6 and recorded in D-093. |
| Thirty cards hide work in the active columns | The limit applies per column and the header shows `30 of N`; only Done and Canceled are expected to exceed it. |
| The phase is large | Two pull requests; phase 2 has a server-half checkpoint (§10.2). |

## 12. Open questions and accepted limits

None open. Accepted limits:

- **A slug does not move while its product exists; it is not reserved after the product is gone.** If a product is removed from the database by hand and a new product is later given the same name, the new one gets the old slug, and an old bookmark opens the new product. Product ids have the same property when the highest-numbered product is the one removed (`nextProductID`, `store_mem.go:417`). Reserving retired slugs would need a tombstone record for an operation that has no handler; it belongs to the design that adds product deletion.
- **A column shows at most 300 cards and the table at most 1,000 rows.** Beyond that the footer says so and points at the filters.

## 13. Approval

Approved by the operator on 2026-11-26, both phases. Three decisions were put to the operator with the approval and answered:

1. **The product's name in the address** (§6): accepted. D-093 records that the slug, and with it the product's name, is visible in the edge's access log, the browser's history and the tab title.
2. **Only the columns that changed** (§5.1, §8): chosen over re-sending all nine columns on every hint.
3. **A slug is not reserved after its product is removed by hand** (§12): shown with the approval and not objected to.

## 14. As built

### 14.1 Phase 1 (v0.55.0)

Built as §3 says, with these differences, each smaller than what the section describes:

- **Page links in the Pull requests and Auto-fix views** (§3.6) are printed by a template function, `pagehref`, applied to the existing `Href`, `ResetHref` and `CloseHref` fields, instead of nine new sibling fields. `prPageHref` and `afPageHref` exist and are what `BOView.Href` is built from.
- **A sixth function calls `buildBO`**: `pageNotFound`, which renders the product list behind the `404` page. `TestOnlyTheAllowedFunctionsCallBuildBO` allows six.
- **`TestPRViewRowsAndFilters`** (`prview_test.go:74`) also asserted the old navigation markup (`>Pull requests</button>`) and changed with the four Auto-fix assertions §9 lists.
- **The middle click.** Besides the capture-phase `click` listener, `static/app.js` stops `auxclick` on the same links.
- **The back-fill on ArcadeDB** is `ArcadeStore.backfillSlugs`, which calls `MemStore.AssignSlug` through `mutate` for each product the pure `backfillSlugs` names.
- **One more test than §10.1 lists: `TestTheSlugBackfillIsWrittenOnce` (TC-733).** §10.1 left the ArcadeDB back-fill to the conformance table's "a slug survives a reopen", which never exercises a product *without* a slug — the case every existing database is in at the upgrade. The test damages three stored slugs in a scratch ArcadeDB, reopens the store and asserts the slugs, the three `RecordUpdated` events and a third boot that writes nothing. So phase 1 is eleven Go tests and one deploy check: **TC-722 … TC-733, register 721 → 733, Go tests 587 → 598**. Phase 2's ids move by one: **TC-734 … TC-748, register 733 → 748, Go tests 598 → 613**.
- **`routes()` raced with itself under the test harness** (two requests at once in `TestRetargetIsRecheckedUnderTheLock`), on `main` as well; `go test -race` failed about one run in two. Fixed separately (pull request #46) and merged into this branch.

Verified in headless Chrome against a local in-memory dashboard, without a prefix: a deep URL opened signed out shows the sign-in form and, after sign-in, the Board at the same address; nine tabs and views each set their address and title; three Backs, a Forward and a reload land on the right view; the id form redirects to the slug; creating a workflow from the Stories tab moves the address to the Board; a Ctrl-click on a tab opens a second tab on its page address and leaves the first where it was; `localStorage` stays empty. The store half was run against a scratch ArcadeDB (`deploy/schema/scratch.sh up`): the conformance suite and TC-733 pass. Not verified in a browser: the same walk behind the `/app` prefix (covered by the Go tests' prefixed cases and by TC-732 at deploy time), and a Pull requests drawer, which needs pull-request data.

### 14.2 Phase 2 (v0.56.0)

Built as §4 and §5 say, with these differences:

- **Sixteen tests, not fifteen: TC-734 … TC-749, register 733 → 749, Go tests 598 → 614.** `TestTheBoardTabSkipsTheEventCopyAndTheDiagrams` (TC-749) holds §5.5, which §10.2's table had lost between revisions.
- **The view model and the Board's route are in a new file, `board_view.go`**, not in `view.go` and `board_page.go`: `BoardView`, `boardView`, `handleBoardLive`, `renderBoardAfter`, the paged JSON. `board_page.go` is the store half and the parser.
- **The table has a tenth column, "Changed"** — when the card last changed state, ticking — and it is the header that sorts by `updated`. §4.6 offered that sort and gave it no column. `BoardCard` gains `UpdatedAt` beside `Updated` for it.
- **The view links are re-sent with every response of the Board's route** (`#boardViews`, out of band), so a page address copied from "Board" or "Table" after a filter change carries that filter.
- **sisflow's virtual list is switched off** (`virtual: {threshold: 300}`). With it on, a reload that left a column with fewer cards than before made an observer of the old, virtualised list render a card that was gone: `TypeError: Cannot read properties of null (reading 'priority')` in `card-view.js`, found in the browser. A column never holds more than 300 cards, so nothing needs it.
- **The hidden server board is emptied once sisflow has mounted.** It had done its job, and a hidden copy of every card was some 2,000 elements. The next render of the sheet brings it back.
- **sisflow puts its scroller back after every reload**, and after a remount retries until the cards are laid out; the library's store resets it.

**Measured** (`go test -run xxx -bench Board -benchmem`, the synthetic product of §1.2, the same machine):

| Measure | Before | Target | After |
|---|---|---|---|
| `Board`, 10,000 cards | 361 ms | — | 75 ms (the two quadratic decorators) |
| `BoardPage`, 2,000 / 10,000 cards | 25 ms / 361 ms (`Board`) | ≤ 3 ms / ≤ 15 ms | 2.9 ms / 10.2 ms |
| `BoardCard`, 2,000 cards | 25 ms (`Board`) | ≤ 0.5 ms | 0.07 ms |
| Opening the Board tab, 2,000 cards | 109 ms / 94 MB / 1.83 MB | ≤ 15 ms / ≤ 10 MB / ≤ 300 KB | 11.9 ms / 7.3 MB / 247 KB |
| One live refresh after a move, 2,000 cards | 109 ms / 94 MB / 1.83 MB | ≤ 10 ms / ≤ 8 MB / ≤ 60 KB | 4.8 ms / 5.2 MB / 46 KB |
| Elements under `#boMain`, 2,000 cards | not measured before the change | ≤ 4,000 | 2,103 on the server board; 3,845 with sisflow mounted and Done opened to 60 |
| DOM nodes and JS heap after 40 live refreshes | not measured before the change | no growth | server board 6,649 → 5,978 nodes, 1.2 → 1.3 MB; sisflow 8,200 → 8,199 nodes, 2.1 → 2.4 MB |

Every target of §5.6 is met. The last two rows were not measured on the old board, so they have no "before"; the last one was read from Chrome's `Performance.getMetrics` after a forced garbage collection (all live DOM nodes, detached ones included), not from a heap snapshot filtered for "Detached".

**Verified in headless Chrome**, on the synthetic 2,000-card product, once with `static/board.js` blocked and once with sisflow mounted: Done shows 30 of 1,800 and "Show more" makes it 60; a role filter narrows the cards, the count line and the address; the search narrows the board, is absent from the address and from every request URL; a filter, then Table, then "Clear filters" clears the selects and the search box and keeps the table; the table sorts by ID and shows 100 more; Board again and a reload keep the view. A move made outside the page replaces two of the nine columns in one request, keeps the board's sideways scroll and Done's own, and marks the card, which fades; five moves at once are one refresh; with the tab hidden no request is made, and one on return; a label set from a filtered, scrolled board keeps the filter, the address and both scroll positions — on sisflow also its depth. The light theme and a 420px-wide screen were looked at, with a 150-character unbroken title and non-Latin text in the first cards: nothing overflows.

**Not verified**: a real pointer drag on either board (the move route and its answer are tested; the drag itself is the same code as before); a keyboard-only pass; `prefers-reduced-motion` in a browser (the rule is in `app.css`); the `/app` prefix in a browser. The `review-animations` and `break-ui` skills were not run as such — the motion here is one opacity fade written to the frontend agent's rules, and the worst-case check was the manual one above.
