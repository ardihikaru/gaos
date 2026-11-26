# Board and list UX: pagination, a New-task entry, the AI help panel, the inject button, a locked wizard

**Status:** Approved (2026-11-27, by the operator, after review rounds 1–2)
**Author:** the Elephant (Claude, with the operator)
**Date:** 2026-11-27
**Branch:** `feat/board-list-ux`
**Reviews:** `reviews/board-and-list-ux-review-1.md`, `reviews/board-and-list-ux-security-review-1.md`, `reviews/board-and-list-ux-review-2.md`

Paths are relative to `dashboard/` unless they start with `pustaka/`, `docs/` or
`deploy/`. Line numbers are at commit `562ea29`. The two fixes listed under
"Already landed" changed only `templates/backoffice.html` (one class and title
attribute per cell, so no line moved), `templates/workflow.html`,
`static/app.css` (lines were added after `:1757` and `:1201`, so later
`app.css` lines move) and `handlers_cw.go:170`.

**Terms.**
- **Data table**: a table of records inside a product tab or a global
  operations view. These page at **10**: the Board table, Pull requests,
  Auto-fix, Timeline and QA by epic.
- **List page**: one of the five tenant-wide record pages. These page at **5**:
  Stories, Diagrams, Knowledge, Agents and Archive (both of Archive's tables).
- **`X-Board-State`**: the request header the Board's script (`static/live.js`)
  sends with every Board request, carrying the view and the window.
- **`X-GAOS-Refresh`**: the header that marks a request as a live refresh, not a
  click (`static/live.js:179-182`). The Board refreshes when the server hints
  that its tasks changed, debounced (`static/live.js:201-234`). It does not
  poll. Pull requests and Auto-fix poll every 30 s.
- **The design gate**: a feature's implementation task waits, Blocked, until its
  design task has been reviewed (`designgate.go:455-460`).
- **The Blueprint**: the rule files GAOS writes into a product's workspace.
  **Drift** is a hand edit to them. BR-39 blocks dispatch until they are
  synced again.
- **BR-38**: one live agent turn per product at a time. **D-080**: the runner's
  off, manual and on modes.
- **`/break-ui`**: the repository's worst-case-data review of a UI, run by
  hand.

## 1. The problem, as the operator reported it

On a product's Board (`/app/p/<slug>/board`), five things are wrong or missing:

1. **The table has no pages.**
   - The Board table shows 100 rows, then "Show 100 more", up to 1000
     (`board_page.go:36-37`; foot at `templates/backoffice.html:547`).
   - Pull requests and Auto-fix cap at 200 rows (`prview.go:20,340-342`;
     `autofix_view.go:32,554-556`).
   - The Timeline (`templates/backoffice.html:570`) and the product QA table
     (`templates/qa.html:36`) render everything.

   The operator wants 10 rows per page on every data table.
2. **There is no way to start work from the Board.**
   - The Board refuses to create a card: `handlers_board.go:188-199` returns 409
     with "cards are planned from the product's stories, not created on the
     board".
   - Tasks are created only by the Create workflow wizard. Its button sits in
     the product title row (`templates/backoffice.html:210`), and nothing there
     says this is how a task reaches the Board.
3. **Nothing explains how the AI takes a task.** An operator cannot tell when a
   new task will start, what a card in the Blocked column means, or how to
   answer the agent or unblock a card.
4. **"Inject into graph" looks broken on the Board.** The button
   (`templates/backoffice.html:209`) is on every tab.
   - With ArcadeDB it writes the record (requirements, stories, criteria,
     diagrams, decisions and the Blueprint) as vertices
     (`store_arcade.go:534-549`, `injectSubgraph` `:1275`,
     `store_arcade_blueprint.go:277`).
   - With the in-memory store it emits `KnowledgeInjected` and re-evaluates
     readiness (`store_mem.go:1976-1998`).
   - Today's planner reads the in-memory projection, not those vertices
     (`store_arcade.go:239-241`), so a click changes nothing visible on the
     Board.

   The BRD intends otherwise. A-6 (`pustaka/docs/product/brd.html:773`,
   *Designed*) says "a record that is edited but not injected is a lie the
   planner will believe". That gap is known, and this design does not close it
   (§6).
5. **The wizard does not know where it was opened.** From a product page it
   pre-selects the product (`handlers_cw.go:19`; `buildCW`, `:124`), but it
   still offers every product as a choice.

The five tenant-wide list pages also render everything on one page:
`/app/stories`, `/app/diagrams`, `/app/knowledge`, `/app/agents` and
`/app/archive` (`handlers_bo.go:242-305`; `handlers_conversation.go:38-60`). The
operator wants 5 items per page there.

### Already landed (bug fixes, outside this design)

These were fixes, so they went straight to implementation on this branch:

- **The gap between ID and Title in the Board table.**
  - Cause: `.af-table td` is `white-space:nowrap` with no width cap
    (`static/app.css:1757`). A PR or defect card's id is long
    (`prflow.go:142-147`, `defecttriage.go:102`), so one such row widened the
    ID column for every row.
  - Fix: the Board ID cell, the Board Workflow cell and the Auto-fix Card cell
    carry `class="cell-id"` (`max-width:16ch`, ellipsis) and a `title` holding
    the full value.
- **The wizard's footer was unstyled.**
  - Cause: the `<footer>` sits inside `<form id="cwForm">`, so
    `.modal > footer` never matched. The answer modal has the same structure
    and fixed it (`static/app.css:1194-1201`).
  - Fix: the step-1 hint moved under the Product label as `.field-help`, the
    hidden Back button is no longer rendered on step 1, and "N tasks · M
    approval gate" pluralises.

## 2. The decisions the operator already made

| Question | Decision |
|---|---|
| What does "New task" do? | Opens the **existing** Create workflow wizard with the product locked. There is no ad-hoc card creation, and the 409 at `handlers_board.go:188` stays. |
| What happens to Inject? | It shows on the record tabs only, relabelled "Sync record to graph", with an explanation. |
| How do the grouped list pages page? | By **item**, not by product group. A product heading shows only when one of its items is on the page. Archive's two tables page independently. |
| Is `/app/agents` paged even though it is a fixed roster? | Yes. The operator named it. |

## 3. The design

### 3.1 One pager (`pager.go`, new)

```go
const (
    tablePageSize = 10
    listPageSize  = 5
    maxPage       = 100000 // an input bound, not a product limit
)

type PageLink struct {
    N         int
    Get, Href string // fragment address (hx-get) and page address (href)
    On, Gap   bool   // the current page; "…" in place of two or more skipped pages
}

type Pager struct {
    Page, Size, Total, Pages int  // Page is 1-based and clamped; Pages >= 1
    From, To                 int  // for display: 1-based, inclusive; 0,0 when Total == 0
    Prev, Next               *PageLink // nil on the first / last page
    Links                    []PageLink
    Sync                     string // hx-sync value for every link; "" for none
    Label                    string // the nav's aria-label; "Pages" when empty
}

// pageParam reads a page key. Absent, not an integer, or <= 1 gives 0
// ("no page"; the normal form). Above maxPage gives maxPage.
func pageParam(v string) int

// clampPage turns a requested page (0 = first) into the real page and the
// 0-based, half-open slice bounds xs[lo:hi]. Pure; the store may call it.
func clampPage(total, page, size int) (p, lo, hi int)

// paginate calls clampPage and builds the links through at, which returns the
// fragment and page address of page n. at is never nil.
func paginate(total, page, size int, at func(n int) (get, href string)) Pager

// sliceFor returns xs[p.From-1 : p.To], or nil when p.Total == 0.
func sliceFor[T any](xs []T, p Pager) []T
```

- **Every page key is read through `pageParam` and nowhere else.** That covers
  `page`, `apage` and `ppage`, and the PR and Auto-fix `page`. Every href
  writer writes the page *after* the clamp, as `page=N` only when `N > 1`. A
  page that is absent or 1 is not written.
- **The link window** holds page 1, the last page, and the current page ±1. When
  exactly one page falls between two shown pages, that page is shown as a
  number. When two or more fall between, a single `Gap` replaces them.
  - 7 pages, on page 4: `1 2 3 4 5 6 7`.
  - 40 pages, on page 20: `1 … 19 20 21 … 40`.
  - 40 pages, on page 3: `1 2 3 4 … 40`.
- **The `pager` template** lives in `templates/backoffice.html`.
  - When `Pages <= 1`, it renders only `<p class="pager-line">Showing N</p>`, and
    nothing at all when `Total == 0`.
  - Otherwise it renders
    `<nav class="pager" aria-label="{{or .Label "Pages"}}"><span class="pager-line">Showing a–b of N</span> …links… </nav>`.
  - Each link is `<a href="{{.Href}}" hx-get="{{.Get}}" hx-target="#sink" hx-swap="innerHTML"{{with $.Sync}} hx-sync="{{.}}"{{end}}>`.
    The current page is `<span aria-current="page">`.
  - Previous and Next on the first and last page render as
    `<span class="pager-off">`, not as links.
- **CSS** goes in `static/app.css`: `.pager`, `.pager-line`, `.pager a`,
  `.pager [aria-current]`, `.pager .gap`, `.pager-off`. It uses existing tokens
  only (`--line`, `--ink-2`, `--ink-3`, `--brass`, `--panel-2`, `--r-s`,
  `--font-mono`). There is no new colour, so no theme variable is added.

### 3.2 Data tables, 10 per page

#### 3.2.1 The Board table

The Board keeps its view in the address (`BoardQuery.Encode`,
`board_page.go:218-248`) and its window outside it (`BoardWindow`, `:64-69`,
"Never encoded"; D-095 (2)). This design moves the table's depth from the window
into the view. The table is now a page, and a page is shareable. **D-097**
records this change to D-095 (§3.7).

- **Fields.**
  - `BoardQuery` gains `Page int`. 0 means no page, which is page 1.
  - `parseBoardQuery` (`board_page.go:167`) sets it, for the table view only,
    from `pageParam(p.get("page"))`.
  - `Encode` writes `page=N` when `View == "table"` and `Page > 1`.
  - `BoardQuery.clean` (defined at `board_page.go:349`) keeps it.
  - `TestTheBoardQueryRoundTrips` keeps passing: absent and `page=1` both parse
    to 0, and 0 is never written.
- **Removed.** These go: `BoardWindow.Rows`; `tableStep` and `tableMaxRows`
  (`board_page.go:36-37`); the `rows=` read and the `more=rows` case
  (`:189-195`); `BoardView.RowsMore` and `RowsCapped` (`board_view.go:139-141`);
  the "Show 100 more" foot (`templates/backoffice.html:547`). The comment at
  `board_page.go:12-17` that describes the window is updated to say the table
  is paged.
- **The cut.** `MemStore.BoardPage` (`board_page.go:415`; the cut is at
  `:498-502`) sorts as today. It then calls
  `clampPage(len(rows), q.Page, tablePageSize)`, keeps `rows[from:to]`, and
  writes the clamped page back into `p.Query.Page` (0 when it is 1). The store
  builds no links. `BoardPage.RowsShown` stays as the number of rows kept.
- **The pager.** `boardView` (`board_view.go:84`) builds
  `bv.Pager = paginate(p.Matched, p.Query.Page, tablePageSize, at)`, with
  `Sync = "#boardFilters:replace"`, the same value the existing "Show more"
  carries (`templates/backoffice.html:514`), and with:
  - `at(n).get = frag + "/board/live?page=" + n`
  - `at(n).href = page(q with Page=n)`, where `page` is the existing builder at
    `board_view.go:107-112`.

  The foot at `templates/backoffice.html:547` becomes
  `{{template "pager" .Pager}}`, and `#boardTable`'s `data-rows` becomes
  `data-page="{{.Page.Query.Page}}"`.
- **Which requests keep the page.**
  - `static/live.js`'s `boardState()` (`:137-157`) replaces
    `p.set("rows", table.dataset.rows)` with
    `p.set("page", table.dataset.page)`. Like `rows` today, the page then
    travels on **every** Board request: the live refresh, a move, an answer, a
    retry. `renderBoardAfter` (`board_view.go:256-271`) rebuilds the same page,
    as D-095 (4) requires.
  - **Exception 1, a user's filter change.** In the `htmx:configRequest`
    handler (`static/live.js:168-183`), after `boardState()` is called at `:174`
    and **before** the `refreshing` reset at `:179`: when
    `elt === boardForm() && !refreshing`, the `page` key is deleted from the
    header value before it is set. The live refresh is the same form with
    `refreshing` set, so it keeps the page. The search box submits through the
    same form (`templates/backoffice.html:443`), so a search also counts as a
    filter change and returns to page 1.
  - **Exception 2, links that change the view.** These carry an empty `page=`,
    appended **last**, in their fragment URL: the sort `Get` (`board_view.go:170`), `BoardGet`,
    `TableGet` and `ResetGet` (`:117-119`). A URL key wins over the header even
    when it is empty (`boardParams.get`, `board_page.go:148-153`), so the result
    is page 1. Their page addresses are built from `q` with `Page = 0`, so they
    carry no `page`.
  - A pager link's URL has `page=N`, which wins over the header.

#### 3.2.2 Pull requests and Auto-fix

| | Pull requests | Auto-fix |
|---|---|---|
| Filter type | `PRFilter` (`prview.go:36`) gains `Page int` | `AFFilter` (`autofix_view.go:157`) gains `Page int` |
| Parsed in | `prPage` (`prview.go:187-208`): `f.Page = pageParam(q.Get("page"))` | `autoFixPage` (`autofix_view.go:304-327`): the same |
| Written by | `prHref` (`prview.go:125`): `page` when `Page > 1` | `afHref` (`autofix_view.go:672`): the same |
| The cut | `prview.go:340-342` becomes `clampPage` + slice; `prMaxRows` deleted | `autofix_view.go:554-556` the same; `afMaxRows` deleted |
| After the cut | `f.Page` set to the clamped page; then `p.Filter`, `p.PollURL` (`:209-210`) are set from it. Row `Href`s move out of the per-product loop (`:281,308`) into a loop over the page's rows, after the cut | `f.Page` clamped; `p.Filter = f` moves from `:552` to after the clamp; row `Href`s are already built after the cut (`:557-560`) |
| Pager `Sync` / `Label` | `#prsLive:replace` (the poll's container, `templates/backoffice.html:1203`) / "Pull request pages" | `#afLive:replace` (`:1001`) / "Auto-fix pages" |
| Pager `at(n)` | `(prHref(f with Page=n), prPageHref(f with Page=n))` | `(afHref(…), afPageHref(…))` |
| "Showing the first 200" note | removed (`templates/backoffice.html:1258`) | removed (`:1106`) |

**Links that keep the page.** These are a row's open link and a drawer's close
link:
- PR row `Href` (`prview.go:281,308`)
- PR drawer `CloseHref` (`prview.go:379`)
- Auto-fix row `Href` (`autofix_view.go:559`)
- Auto-fix drawer `CloseHref` (`autofix_view.go:767`)

Each is built with `Page: f.Page`. So opening a drawer on page 2 keeps page 2
under it.

**Links that drop the page.** These set `Page = 0`:
- PR state tabs (`prview.go:324-326`, `g := f` followed by `g.Page = 0`)
- PR kind and product options
- PR reset (`:212`, already a literal with no page)
- Auto-fix state tabs (`autofix_view.go:564,570`, literals)
- Auto-fix product and difficulty options

**The 30-second polls** (`GET /ui/bo/prs/live`, `GET /ui/bo/autofix/live`;
`main.go:532,534`) use `PollURL`, so a poll keeps the page.

PR rows stay grouped by product, as today (`prview.go:343+`). The groups are
built from the page's rows, just as they are built from the first 200 today.

#### 3.2.3 Timeline and QA (product tabs)

- **Timeline** (`handlers_bo.go:357-364`). After `d.Timeline` is built, it is
  sliced with `pageParam(r.URL.Query().Get("page"))` and `tablePageSize`.
  `ProductDetail` gains `Pager Pager`. Its `at(n)` is
  `("products/<id>?tab=timeline&page=n", "p/<slug>/timeline?page=n")`.
- **QA.** `buildQAPane(ctx, m)` (`handlers_qa.go:92`) is **unchanged**, so its
  test callers (`qaclient_test.go:309,321`) are untouched. `QAPane`
  (`handlers_qa.go:25`) gains `Pager Pager`. The call site
  (`handlers_bo.go:347-348`) paginates the result:
  `d.QA.Pager = paginate(len(d.QA.Epics), pageParam(r.URL.Query().Get("page")), tablePageSize, at)`
  with `at(n) = ("products/<id>?tab=qa&page=n", "p/<slug>/qa?page=n")`, using
  `d.Slug`. It then sets `d.QA.Epics = sliceFor(d.QA.Epics, d.QA.Pager)`. The
  `By epic` heading's count (`templates/qa.html:34`) becomes
  `{{.Pager.Total}}`.
- **The address.** `address()`'s Detail branch (`urls.go:258-273`) appends
  `?page=N` for `section == "timeline"` from `d.Pager.Page`, and for `qa` from
  `d.QA.Pager.Page`, when it is greater than 1.
- **What this does not fix.** The Timeline still copies the whole event stream
  per request (§6).

#### 3.2.4 Not paginated, on purpose

- **Integrations tables** (`templates/backoffice.html:854-958`): key/value
  configuration with a handful of fixed rows.
- **The timing modal's Attempts** (`templates/needs.html:198`): one card's
  attempts, inside a modal.
- **The kanban view**: columns, not a table. It keeps its per-column "Show
  more".

### 3.3 List pages, 5 per page

All five are built in `buildBO` (`handlers_bo.go:197-377`), which both
`handleBOView` (`handlers_bo.go:151`) and `handlePage` (`urls.go:366-373`) call
with the request. No route changes.

- **The fields.** `BOView` gains `Page`, `APage` and `PPage`, all `int`, each
  read with `pageParam` and then **overwritten with the clamped page** after
  `paginate` (0 when it is 1). It also gains `Pager` (rollup and agents only).
  Archive's two pagers live on `ArchivePage` as `ArchivedPager` and
  `PrunedPager`, labelled "Archived pages" and "Pruned pages". The address never
  copies the request's query.
  - `address()`'s default branch (`urls.go:278-285`) builds the query from those
    three integers only: `page`, `apage` and `ppage`, each written when it is
    greater than 1. A stray parameter such as `q=` never reaches `bo.Href`.
  - The pager's links are built the same way:
    `at(n) = ("ui/bo/<view>?page=n", "<view>?page=n")`. Archive's two pagers
    also carry the other table's key.
- **Stories, Diagrams and Knowledge** (`handlers_bo.go:284-305`). The groups are
  built as today. Then, with `p := paginate(total, bo.Page, listPageSize, at)`,
  where `total` is the sum of the groups' items, an **offset window** walks the
  groups in order. Each group keeps the part of its items that overlaps
  `[from, to)`, and a group left with nothing is dropped. `bo-rollup`
  (`templates/backoffice.html:748-773`) gets `{{template "pager" .Pager}}`
  after the groups.
- **Agents** (`handlers_bo.go:264-282`). The rows are built as today, then
  sliced. `bo-agents` gets the pager.
- **Archive** (`archivePage`, `handlers_conversation.go:38-60`). It becomes
  `archivePage(v ArchiveView, apage, ppage int)`.
  - The Archived list is cut by `apage` and the Pruned list by `ppage`, each 5
    per page, with its own pager.
  - **The prune form stays bound to cards the operator has seen.** D-082 binds
    the typed PRUNE "to the listed cards" (`pustaka/docs/product/decision-log.html:1692`).
    So the form no longer takes its cards from the page's rows. It takes them
    from a new `ArchivePage.PruneCards []ArchivedRowView`, set before the cut,
    which holds **at most `pruneBatch = 100`** cards, the oldest archived first.
  - The confirmation form (hidden until the prune button is pressed,
    `templates/backoffice.html:811-817`) **lists exactly those cards**, as a
    plain list of product, card id and title, in a scroll box with
    `max-height:16rem`. It carries one hidden `card` input per listed card.
  - The opening button reads "Prune all archived…" when
    `len(PruneCards) == total`, and "Prune the 100 oldest…" otherwise. The
    submit reads "Prune N conversations".
  - The operator therefore sees every card the typed word covers, outside the
    pager, and the payload and the consent are both bounded. A larger archive
    is pruned in batches. D-097 records this amendment to D-082 (§3.7).
- **The nav counts** (`bo.Counts`, `templates/backoffice.html:91-95`, computed
  at `handlers_bo.go:232-239`) stay the totals.

### 3.4 The Board: New task, and how the AI works

The `pane-board` header (`templates/backoffice.html:410-414`) becomes:

```html
<div class="board-head">
  <h4>Board — the product's tasks by status</h4>
  <button class="btn btn-primary" hx-get="ui/cw?product={{.ProductID}}&lock=1" hx-target="#sink" hx-swap="innerHTML">New task</button>
</div>
<details class="board-help">
  <summary>How the AI picks up tasks</summary>
  <ol> …the steps below… </ol>
</details>
<p class="board-lede">…unchanged…</p>
```

The `<summary>` shows an `i` glyph drawn by CSS (`.board-help > summary::before`)
before its visible text. It has no `aria-label`, so its accessible name is the
visible text.

`BoardView` gains `Runner string`. `buildBoardView` (`board_view.go:193-203`)
sets it from `s.adapter` (`main.go:113`) and `runnerMode()` (`runner.go:52-60`):

| `Runner` | When |
|---|---|
| `"off"` | `runnerMode() == RunnerOff` |
| `"noadapter"` | the mode is on or manual and `s.adapter == nil`. The control loop starts only when `adapter != nil && runnerMode() != RunnerOff` (`main.go:306`) |
| `"manual"` | `RunnerManual` with an adapter |
| `"on"` | `RunnerOn` with an adapter |

The steps. Each code anchor is listed here, and also as a comment beside the
step in the template:

1. **Plan.** *New task* turns the stories you pick into tasks. Most start in
   **Backlog**. Under the design gate, an implementation task starts in
   **Blocked** until its design is reviewed
   (`store_mem.go:2150-2240`, `:2282-2309`).
2. **Backlog → Ready.** This step depends on `Runner`:
   - *on*, *manual*: "GAOS moves a task to Ready by itself once its
     prerequisites are done, the product is ready enough, an agent can take it
     and the budget allows. Until then the card waits in Backlog. You cannot
     drag Backlog ↔ Ready." (`scheduler.go:59-73,281`; `board.go:209-229`.)
   - *off*, *noadapter*: "Nothing is readied on this deployment: tasks stay in
     Backlog." (`main.go:306`; D-080.)
3. **Ready → Working.** This step also depends on `Runner`:
   - *on*: "An agent starts the task automatically within a few seconds, one
     task at a time per product." (`runner.go:486`; BR-38.)
   - *manual*: "Nothing starts by itself. Drag the card from Ready to Working,
     or press its `→ working` button, to start it." (`board.go:456-560`;
     D-080; the button label is the column id, `templates/backoffice.html:509,540`.)
   - *off*: "The runner is off on this deployment. An operator must set
     GAOS_RUNNER to start work."
   - *noadapter*: "No AI vendor is configured on this deployment, so no task can
     start."
4. **Needs you.** "The agent asked you a question. Press the Needs you badge and
   answer. The task returns to Ready with your answer. If nobody answers in
   time, the question expires and the task moves to Blocked."
   (`questions.go:195-220,245`.)
5. **Blocked.** "The card says why. Then:"
   - **Held by the design gate** (the reason names the design): "wait. It
     unblocks when the design's review resolves." (`board.go:358-363`.)
   - **A review blocked by its own verdict**: "press `Revise design` to send
     the design back to its author." (`board.go:366-370`.)
   - **Workspace drift** (BR-39): "sync the Blueprint. The block clears by
     itself." (`workspace.go:384-405`.)
   - **Deferred** (over budget): "it cannot be moved. It is released when the
     budget allows." (`board.go:151,212-213`.)
   - **Any other reason**: "fix the cause, then press `→ ready` (or drag the
     card to Ready)." (`MoveUnblock`, `board.go:208-209,390-395`.)
6. **Review.** "An implementation task waits for you: press `→ done` to approve
   it, or `→ ready` to send it back for another attempt. A design or review task
   is decided by the gate, not by you. A Failed card goes back with `→ ready`."
   (`board.go:209-229,376-381`.)

The `<details>` is closed by default, and its open state is not remembered. The
empty-Board line (`templates/backoffice.html:415`) becomes: "No tasks yet. Press
New task to plan this product's stories into tasks."

### 3.5 The title row: Inject and Create workflow

At `templates/backoffice.html:208-211`:

- **Inject.**
  - It renders only when `.Tab` is `brd`, `stories`, `diagrams`, `knowledge` or
    `blueprint`, the tabs whose content injection writes. `.Tab` is never
    empty here: `handlers_bo.go:179` defaults it to `brd`.
  - Its label becomes "Sync record to graph", with one attribute, true for
    both stores:
    `title="Projects this record — requirements, stories, criteria, diagrams,
    decisions and the Blueprint — into the graph database (ArcadeDB). Without
    one, it records the injection and rescores readiness."`
    (`store_arcade.go:534-549`; `store_mem.go:1976-1998`.)
  - The title makes no claim about planning, either way (A-6).
  - The route (`POST /products/{id}/inject`, `main.go:543`) and `handleInject`
    (`handlers_bo.go:488-498`) are unchanged.
- **Create workflow** renders on every tab except `board`, where the Board's own
  "New task" replaces it. It now also sends `&lock=1`.

### 3.6 The locked wizard

- **Reading the lock.** `handleCWOpen` (`handlers_cw.go:17-29`) and
  `handleCWStep` (`:32`) read `lock`. Before calling `buildCW`, both check
  `product` against `Products()`:
  - When the product is unknown, they clear it and the lock, so the unlocked
    picker renders. Today an unknown product fails inside `buildCW`
    (`handlers_cw.go:128-132`).
  - Otherwise, `buildCW` takes a `locked bool` and sets `CWView.Locked` and
    `CWView.LockedName`, which is the selected `Pick`'s `Name` (`view.go:381`).

  `WorkflowDraft` and the store interface are unchanged.
- **Round-trip.** `cw-inner` (`templates/workflow.html:22-30`) carries the lock
  as `<input type="hidden" name="lock" value="1">`.
- **Locked heading and step 1.**
  - The modal heading (`templates/workflow.html:6`) reads
    "New tasks for {{product name}}".
  - Step 1 replaces the `.picker` with a read-only block:
    `<div class="cw-locked"><b>{{name}}</b><span>{{stories}} stories · {{coverage}}% covered</span></div>`.
  - The step-1 hint `#cwProductHelp` (the `aria-describedby` target) reads, when
    locked: "GAOS plans one or more tasks from the stories you pick next, using
    this product's record." This replaces `.Hint`. The `aria-describedby` moves
    to the `.cw-locked` block.
  - The block's values come from the selected `Pick` (`Name`, `Stories`,
    `Coverage`).
  - When locked, step 3's submit (`NextLabel`, `handlers_cw.go:169`) reads
    "Create tasks".
  - `.cw-locked b` and `#cwTitle` have `overflow-wrap:anywhere`.
- **Steps 2 and 3** are unchanged.
- **Unlocked entry.** The topbar entry (`templates/chrome.html:40-41`) sends no
  `lock`.
- **The lock is presentation, not a control.** `lock` and `product` are values
  the client sends, and nothing on the server should trust them. Every
  signed-in account is a full operator today, and `handleWorkflowCreate` runs
  `requireBO` (`handlers_cw.go:68`). If OQ-37 (back-office roles,
  `pustaka/docs/product/open-questions.html:907`) introduces per-product
  scoping, that change must add a server-side check in `handleWorkflowCreate`
  and `handleCWStep`. It must not rely on this lock.

### 3.7 D-097

A new decision-log entry. It amends two earlier decisions:

- **D-095 (2):** the Board table's page is part of the Board's address. The
  table is paged at 10, and the accepted limit "the table at most 1,000 rows"
  is withdrawn.
- **D-082:** the typed PRUNE stays bound to the listed cards. The cards are
  listed inside the confirmation, outside the Archive's pager, and one prune
  takes at most the 100 oldest archived cards.

## 4. The change list

Code, in phase order (§9):

| # | File | Change |
|---|---|---|
| 1 | `pager.go` (new) | `Pager`, `PageLink`, `pageParam`, `clampPage`, `paginate`, `sliceFor`, the constants |
| 2 | `board_page.go` | `BoardQuery.Page`, `Encode`, `parseBoardQuery`, `clean`; remove `tableStep`, `tableMaxRows`, `BoardWindow.Rows`, `more=rows`; the cut; the comment at `:12-17` |
| 3 | `board_view.go` | `Pager`, `Runner`; remove `RowsMore`/`RowsCapped`; empty `page=` on sort, toggle and reset |
| 4 | `static/live.js` | `rows` → `page`; drop it on a user filter change |
| 5 | `prview.go` | `PRFilter.Page`; `prPage`, `prHref`, the cut, the row, close and tab links; delete `prMaxRows` |
| 6 | `autofix_view.go` | `AFFilter.Page`; `autoFixPage`, `afHref`, the cut, the row, close and tab links; delete `afMaxRows` |
| 7 | `handlers_bo.go` | Timeline slice; QA call site; Stories/Diagrams/Knowledge offset window; Agents slice; `BOView` page fields |
| 8 | `handlers_qa.go` | `QAPane.Pager` (the type at `:25`); `buildQAPane` unchanged |
| 9 | `handlers_conversation.go` | `ArchivePage` (`:21`) gains `PruneCards`, `ArchivedPager`, `PrunedPager`; `archivePage(v, apage, ppage)`; `pruneBatch` |
| 10 | `urls.go` | `address()`: the default branch from typed ints; the Detail branch for `timeline` and `qa` |
| 11 | `view.go` | `BOView.Page/APage/PPage/Pager`; `ProductDetail.Pager`; `CWView.Locked/LockedName` |
| 12 | `handlers_cw.go` | read `lock`; clear an unknown product and the lock; `buildCW(…, locked)`; `NextLabel` "Create tasks" when locked |
| 13 | `templates/backoffice.html` | `pager`; Board foot and `data-page`; `pane-board` header and `<details>`; title row; rollup, agents, archive (pagers, prune list), timeline, PR and Auto-fix pagers; remove both "first 200" notes |
| 14 | `templates/qa.html` | pager; heading count |
| 15 | `templates/workflow.html` | `lock` input; locked heading and step 1 |
| 16 | `static/app.css` | `.pager*`, `.board-head`, `.board-help*`, `.cw-locked`, `.cv-prune-list` (the bounded prune list) |
| 17 | `templates/page.html` | `?v=0.58.0` → `?v=0.59.0` on `app.css`, `app.js`, `live.js` (`:48,50,51`); `external_inbox_test.go:898-912` requires all three to equal `toc.js`'s semver |

Tests and paperwork:

| # | File | Change |
|---|---|---|
| 18 | `pager_test.go` (new), `board_page_test.go`, `prview_test.go`, `autofix_view_test.go`, `urls_test.go`, `conversation_test.go`, `main_test.go` | §5. `qaclient_test.go` is not touched |
| 19 | `pustaka/docs/product/qa-test-plan.html` | 10 new rows; 6 changed rows and their prose; counts 752 → 762 in the lede and the register intro |
| 20 | `pustaka/docs/assets/toc/product.js` | the `qa-test-plan` desc and register text (762; Go tests 616 → 626); the decision-log desc (Ninety-five → Ninety-six) |
| 21 | `.claude/rules/qa-test-plan.md` | "Ninety-five entries" → "Ninety-six entries"; "highest in use" TC-752 → TC-762, next free TC-763; the `dashboard (go test)` range row gains TC-753…TC-762; "752 rows… 592 in `dashboard/`… 616 Go tests" becomes 762, 602 and 626; "seven hundred and fifty-two" becomes "seven hundred and sixty-two"; "six hundred and sixteen" becomes "six hundred and twenty-six" |
| 22 | `pustaka/docs/product/decision-log.html` | D-097; "Ninety-five entries" → "Ninety-six entries" |
| 23 | `pustaka/docs/changelog.html`, `pustaka/docs/assets/toc.js` | the v0.59.0 entry citing D-097; `product.semver` 0.58.0 → 0.59.0 |
| 24 | `deploy/check-workspace.sh` | the pinned literals: `:114-118` "six hundred and sixteen"/`616` → "six hundred and twenty-six"/`626`; `:129-130` → "seven hundred and sixty-two"; `:131` → "Ninety-six entries". This is count maintenance, the same as every count change. The script's register rows (TC-357, TC-358, TC-380, TC-381, TC-437, TC-510, TC-751) quote no count, so their prose does not change |
| 25 | `README.md` | `:12` "Product version 0.59.0"; `:137` "626 Go tests: 602 in `dashboard/`"; `:139` "762 registered test cases" (compared by `check-workspace.sh:147-149`) |

No event type, task status, schema statement, route, Blueprint file, or
store-interface signature changes. `MemStore.BoardPage` reads one more field of
the `BoardQuery` it already receives.

## 5. Tests

All published prose uses placeholder products (`p-1`, "the sample product"),
never a real instance name (D-085).

**New.** One register row each, TC-753 to TC-762:

| TC | File | Test | Asserts |
|---|---|---|---|
| TC-753 | `pager_test.go` | `TestPaginateClampsAndWindows` | `pageParam`: "" → 0, "1" → 0, "x" → 0, "-3" → 0, "999999999" → `maxPage`. `paginate(0,0,10)`: Pages 1, From/To 0/0. 25 items: page 0 → 1, page 99 → 3, page 3 is 21–25. Windows: 7 pages on 4 → `1 2 3 4 5 6 7`; 40 on 20 → `1 … 19 20 21 … 40`; 40 on 3 → `1 2 3 4 … 40`. Prev is nil on page 1, Next is nil on the last |
| TC-754 | `board_page_test.go` | `TestTheBoardTablePagesByTen` | 23 tasks, via `GET /products/p-1/board/live?view=table&page=9` with `HX-Current-URL` on the Board's own address. Page 1 has 10 rows and "Showing 1–10 of 23"; `page=3` has 3; `page=9` clamps to 3 and `HX-Replace-Url` carries `page=3`; a sort link carries an empty `page=`; every pager `<a>` has an `href` with no `ui/` or `products/` prefix |
| TC-755 | `board_page_test.go` | `TestTheBoardPageClampsAfterAMove` | 11 tasks in the table, `X-Board-State: view=table&page=2` (one row on page 2). A move of that row under a `status` filter that drops it answers page 1 with `data-page="0"` and no `pager` links. The JS half, dropping the page on a user filter change, is a manual check (§9) |
| TC-756 | `urls_test.go` | `TestTheListPagesPageByFive` | On the default test server, with the total N read from the nav count: `GET /ui/bo/stories` shows `min(5,N)` `.story` cards; `?page=99` clamps to `ceil(N/5)` and its `HX-Replace-Url` (sent with `HX-Current-URL` on `stories`) carries that page; a product heading appears only with an item on the page. Agents: 13 rows → 3 pages. Every pager `<a>` has an `href` with no `ui/` prefix. `ui/bo/stories?page=2&q=secret` gives an address with `page=2` and no `q` |
| TC-757 | `conversation_test.go` | `TestTheArchivePagesEachTableAndListsWhatPruneTakes` | 7 archived and 6 pruned, via `GET /ui/bo/archive?ppage=2`: Archived shows 5 rows. The prune form names all 7 card ids, carries 7 `card` inputs and reads "Prune 7 conversations". The Archived pager's page-2 link carries `ppage=2`. With 101 archived, the form lists 100 (the oldest) and the opening button reads "Prune the 100 oldest…" |
| TC-758 | `prview_test.go` | `TestThePRAndAutoFixTablesPageByTen` | 12 rows each. Page 1 has 10. On page 2, the poll URL, every row `href` and the drawer close link carry `page=2`, and the state tabs carry none. `page=-1` and `page=abc` render page 1. `page=99` renders page 2, and every row `href` carries `page=2` |
| TC-759 | `main_test.go` | `TestTheWizardLocksTheProduct` | `ui/cw?product=p-1&lock=1` renders no `data-pick`, a `cw-locked` block and "New tasks for". The `lock` input round-trips through step 2 and back, and step 3's submit reads "Create tasks". Without `lock`, and with `product=nope&lock=1`, the picker renders with status 200 |
| TC-760 | `board_page_test.go` | `TestTheBoardExplainsTheRunnerAndOffersNewTask` | The Board has `New task` with `lock=1`. `board-help` holds the step-3 text for each of off, noadapter, manual and on, chosen by `GAOS_RUNNER` and the presence of the adapter |
| TC-761 | `main_test.go` | `TestSyncRecordShowsOnlyOnRecordTabs` | Via `GET /products/p-1?tab=…`, inside the `bo-title` header only: `Sync record to graph` is present on `brd` and `blueprint` and absent on `board`, `timeline` and `qa`. `hx-get="ui/cw?product=p-1&amp;lock=1"` with the text `Create workflow` is absent on `board` |
| TC-762 | `main_test.go` | `TestTheTimelineAndQAPanesPageByTen` | 14 events: Timeline page 1 has 10 `ev-row` and page 2 has 4, and the page address is `p/<slug>/timeline?page=2`. QA likewise, with the heading count showing the total |

**Changed.** Each keeps its ID. Its row and prose are updated in the same
commit:

| TC | Test | Change |
|---|---|---|
| TC-630 | `TestAutoFixViewWorstCase` (`autofix_view_test.go:374`) | 502 rows: asserts 10 rendered and a pager, not the 200 cap |
| TC-710 | `TestPRViewRowsAndFilters` (`prview_test.go:58`) | the `prMaxRows` block (`:124-138`) asserts paging instead of "Showing the first 200" |
| TC-739 | `TestBoardPageDecoratesOnlyThePage` (`board_page_test.go`, `:373`) | the bound becomes `tablePageSize` |
| TC-741 | `TestTheBoardQueryRoundTrips` (`board_page_test.go:413`) | gains `{View:"table", Page:3}`; `w.Rows` is removed from the window assertion (`:430`); `rows` stays in the list of keys that are never encoded (`:442-444`) |
| TC-746 | `TestABoardMutationKeepsTheView` | the state header `rows=100` (`:762,803`) becomes `page=2`; asserts the move answers page 2 and `data-page="2"`; the `BoardGet` regex at `:798` gains the trailing `&amp;page=` |
| TC-747 | `TestTheTableSortsAndCaps` → renamed `TestTheTableSortsAndPages` (`:813`) | the 100/1000 assertions (`:830-849`) become page-of-10 assertions; the sort-link regex at `:852` gains the trailing `&amp;page=`; the other sort assertions stay |

| TC-750 | `TestTheBoardTabSkipsTheEventCopyAndTheDiagrams` | found during implementation: the Timeline tab now holds one page of events, so the assertion becomes "one read, one page, and the pager counting all" |

Counts: register 752 → 762, Go tests 616 → 626, dashboard 592 → 602.

## 6. Non-goals and accepted limits

- **Ad-hoc card creation.** The 409 stays (§2).
- **A server-side wizard lock.** Revisit with OQ-37 (§3.6).
- **Closing the A-6 gap.** This design does not make the planner read the
  graph, and does not make injection automatic. The button stays and is only
  relabelled.
- **Showing `lastInjectedAt`.** It exists only in ArcadeDB
  (`store_arcade.go:1353`).
- **Database paging.** Reads are served from the in-memory projection
  (`store_arcade.go:218-232`). The Timeline still copies the full event stream
  and filters it per request (`handlers_bo.go:358-364`), as today. Paging cuts
  what is rendered, not what is read.
- **Products list and Inbox** (`handlers_bo.go:248-250,308-324`). The operator
  did not ask for them.
- **Integrations, the timing modal, the kanban columns** (§3.2.4).
- **A mutation on Archive** (Restore, Prune) re-renders page 1 of both tables.
  This is accepted.
- **Back does not step through pages.** A pager link keeps the path and changes
  only the query, so the address is replaced, not pushed (`urls.go:341-347`).
  Filters behave the same way.
- **Pruning more than 100 at once.** A larger archive is pruned in batches
  (§3.3).
- **Remembering a page per operator.** The page lives in the URL only.

## 7. Alternatives considered

- **Keep "Show 100 more" and page only the other tables.** Rejected. The
  operator asked for every table, and reported the Board's.
- **Client-side paging.** Rejected. Every table is a server fragment, so
  client-side paging would ship every row and duplicate the sort in JS.
- **Page the grouped lists by product.** Rejected by the operator.
- **Prune only the visible page** (one reading of D-082). Rejected. The button
  says "all", and an operator would have to prune page by page.
- **A real single-task modal.** Rejected by the operator. It would need a store
  method, an event type, and a change to the rule that tasks come from a plan
  (the 409, `handlers_board.go:188-199`).

## 8. Risks

| Risk | Mitigation |
|---|---|
| A move or refresh jumps to page 1 | `page` goes on every Board request (§3.2.1). Tested by TC-746 and TC-755 |
| A filter change keeps a stale page | `live.js` drops it on a user filter change, and the clamp still applies. The JS is a manual check in §9 |
| Prune deletes something the operator did not see | Every card is listed in the confirmation (§3.3). TC-757 |
| The help text drifts from the code | Every step's anchor is a comment beside it in the template. Runner variants are pinned by TC-760 |
| A stale refresh overwrites a Board pager click | Board pager links carry `hx-sync="#boardFilters:replace"` |
| Long product names in the locked summary | `overflow-wrap:anywhere`. The §9 `/break-ui` pass |
| Offset paging over live data: a moved card leaves its page; a new PR shifts every PR page by one on the next poll; after a move shrinks the page count, the Board's address keeps the old page until the next refresh | Accepted. The clamp keeps every response valid, and the next refresh or navigation corrects the address |
| A PR or Auto-fix poll in flight overwrites a pager click | Their pager links carry `hx-sync` on the poll's container (§3.2.2) |

## 9. Phases and exit conditions

Each phase leaves `cd dashboard && go vet ./... && go build ./...` passing. A
phase that deletes a constant rewrites its tests in the same phase.

| Phase | Scope (§4 rows) | Exit |
|---|---|---|
| P1 | 1, and TC-753 | `go test -run TestPaginateClampsAndWindows ./...` |
| P2 | 2, 3, 4, part of 13, 16, and TC-754/755/760 plus changed TC-739/741/746/747 | `go test -run 'TestTheBoard\|TestBoardPage\|TestABoardMutation\|TestTheTableSorts' ./...` |
| P3 | 5, 6, part of 13, and TC-758 plus changed TC-630/710 | `go test -run 'TestPRView\|TestAutoFixView\|TestThePRAndAutoFix' ./...` |
| P4 | 7 (Timeline, QA call site), 8, 10 (Detail), 11 (`ProductDetail.Pager`), 13 (Timeline pager), 14, and TC-762 | `go test -run TestTheTimelineAndQAPanes ./...` |
| P5 | 7 (lists), 9, 10 (default), 11 (`BOView` fields), 13 (rollup, agents, archive, prune list), 16 (prune list box), and TC-756/757 | `go test -run 'TestTheListPages\|TestTheArchive\|TestEveryHtmxLink' ./...` |
| P6 | 11 (`CWView`), 12, 15, 13 (title row), 16 (`.cw-locked`), and TC-759/761 | `go test -run 'TestTheWizard\|TestSyncRecord' ./...` |
| P7 | 17, 19–25 | all of the below |

**Final gate:**

```bash
cd dashboard && go vet ./... && go test ./...         # ok
cd pustaka && go test ./... && go run . check ./docs  # ok, no errors
./deploy/check-stacks.sh                              # every line ok
./deploy/check-workspace.sh                           # every line ok (counts, D-085, TC-751)
```

**Manual check:**

1. Run the dashboard locally.
2. On the Board table, page 2: change the Role filter, and type in the search
   box. Each should land on page 1. Go back to page 2 and, from a second tab,
   move a card on that product to provoke a hinted refresh. The first tab should
   stay on page 2. Press a `→` move button. The page should be kept.
3. Open the wizard from the Board and confirm the product is locked.
4. Open Archive with 7 or more archived conversations and confirm that the prune
   confirmation lists all of them.
5. Run `/break-ui` on the pager, the help panel and the locked summary.
