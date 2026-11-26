<!-- Round 2, 2026-11-26. Lens: comprehension. Target: docs/designs/urls-and-board.md (rev 2). Verbatim reviewer output. -->

I reviewed `docs/designs/urls-and-board.md` in the `feat-urls` checkout. **There are no Blockers: 8 Majors and 14 Nits.** I could paraphrase every section, and most `file:line` claims check out against the checkout. The Majors are contradictions or gaps that would lead to wrong behaviour if built as written.

**Isolation note.** The design sits under the primary repository's `.claude/worktrees/`, so reading it made the harness attach three rule files from the primary checkout: `.claude/rules/agent-workspace.md`, `qa-test-plan.md` and `frontend.md`. No `CLAUDE.md`, `AGENTS.md` or `GEMINI.md` content was attached or opened. I did not open the checkout's `.claude/` directory or anything under `docs/designs/reviews/`.

## Paraphrase

### What this proposes

**Phase 1 (v0.55.0, D-093): real URLs**
1. Every global back-office view gets its own address: `/inbox`, `/stories`, `/diagrams`, `/knowledge`, `/agents`, `/archive`, `/integrations`, `/autofix` and `/prs`.
2. Every product section gets `/p/{ref}/{section}`, where the section is one of nine tabs. `/p/{ref}` redirects to the `brd` tab.
3. That makes eleven new GET patterns. Each returns a whole page with `no-store`. The existing fragment routes stay as they are and are still what htmx calls.
4. `{ref}` is looked up as a slug first. If it matches a product id instead, the server redirects (302) to the slug form. Anything else is a 404 page with a toast.
5. Products get a stored `Slug`. It is set once at creation from the name, and existing products are back-filled at boot. On ArcadeDB the back-fill writes the product and emits a `RecordUpdated` event; there is no schema change.
6. One wrapper, `sheet()`, replaces `buildBO` at every handler that sends the sheet to htmx. It computes `BOView.Href` (the path plus a re-encoded allow-list of parameters). `pushURL` then sets `HX-Push-Url`, `HX-Replace-Url` or nothing, by comparing with `HX-Current-URL`.
7. Back and Forward reload the page from the server. htmx's history cache is turned off.
8. Navigation buttons become `<a href hx-get>`. A capture-phase listener lets Ctrl/Cmd/middle-click open a real tab.
9. A signed-out request for a page URL gets a 401 HTML sign-in page from the gate. Signing in on a deep URL answers 204 with `HX-Refresh`, so the browser reloads where it was.

**Phase 2 (v0.56.0, D-094): the Board**
1. A new store method, `BoardPage`, does one pass to filter, sort (Done and Canceled newest first), cut each column to 30 and decorate only the cards it keeps.
2. The view lives in the URL as `BoardQuery`. The search text and paging depth are request-only (`BoardWindow`).
3. Two new routes:
   - `board/live` returns only the columns that changed, as OOB elements, decided by a per-column hash the browser sends back as `rev`.
   - `board/cards` serves pages of cards to sisflow.
4. The existing board JSON gains a `paged=1` form.
5. Layout and controls:
   - The Board tab goes full width with 300px columns.
   - A filter bar and a Board/Table switch are added.
   - A server-rendered table sorts and pages.
   - Story lanes are dropped from sisflow.
6. Live behaviour:
   - Hints are batched.
   - Hidden tabs wait.
   - A new card shows a short "fresh" bar.
7. `buildBO` stops copying the event stream and rendering diagrams on tabs that do not need them.
8. Benchmarks set the targets.

### How the existing system works, per this document

- Every back-office click is an htmx GET into a hidden `#sink`. The response is the whole `#boSheet` as an OOB element, and nothing changes the address. Only `GET /{$}` returns a whole page.
- The Board renders every card in nine 140px-minimum columns, beside a 300px aside, inside a 1400px cap.
- A `task` hint re-fetches the product route. That re-runs `buildBO` in full: it copies the event stream, renders every sequence diagram and builds the whole board. The sheet is then replaced and the horizontal scroll is lost.
- Two board decorators scan every task for every card, which is quadratic.
- Two boards exist: sisflow, and a server-rendered fallback. sisflow did not load from v0.44.0 until the fix in v0.54.1.
- The auth gate refuses non-public routes when signed out: 401 with the sign-in sheet for htmx, 401 JSON otherwise.
- The product record is rebuilt by `load`, not by the fold. Product ids are never reused.

## Clarity

The flow from §1 to §13 is easy to follow. §3.2 (the slug), §3.3, §3.5, §5.5 and §7 are solid. The many `file:line` anchors make most claims checkable, and nearly all of them hold.

## Findings

### A removed product's slug can be handed out again, which the design's own reasoning forbids - Severity: Major
- Affected scope: §3.2 "Why stored and not derived"; §7, row "A bookmarked slug belongs to a product that was removed"
- Finding: A candidate slug is "taken" only if *another existing* product holds it. If product `x` is removed from the database by hand and a namesake is created later, the new product gets `x`. A bookmark to `/p/x/board` then opens the wrong product, with no 404. That is the exact hazard §3.2 cites (TC-699, `nextProductID` at `store_mem.go:417`) to justify storing the slug. §7 promises a 404 in this case, which is not what the design builds.
- Recommendation: State that slugs are not reused, and say how: a retired-slug set, or deriving the suffix from the high-water mark the way ids are. Otherwise, explicitly accept reuse and correct §3.2's rationale and §7's row.

### "Phase 1 depends on nothing in phase 2" is false - Severity: Major
- Affected scope: §2 "The two phases can be approved separately"; §3.4 table, Board row; §3.8; §4.1 opening line; §4.2; §9 phase-1 `urls.go` row
- Finding: Phase 1 parses and re-encodes `BoardQuery`, whose fields and parameter vocabulary are defined only in §4.1 and §4.2 (phase 2). The signature given, `parseBoardQuery(url.Values) (BoardQuery, BoardWindow)`, returns `BoardWindow`, a phase-2 type. §4.1 says both types live "in the new file `board_page.go`", but §9 puts `BoardQuery` and `Encode` in `urls.go` in phase 1. If phase 2 is rejected or changed, phase 1 will already have shipped URL parameters (`view`, `role`, `wf`, `needs`, `empty`, `sort`, `dir`) for features that do not exist.
- Recommendation: Write a phase-1 section that defines exactly what phase 1 parses and its signature, and where the type lives. Alternatively, drop Board parameters from phase 1 and have phase 2 add them to the §3.4 table.

### Client-supplied values do reach response headers, contrary to §6 - Severity: Major
- Affected scope: §6, the second and third bullets ("No client-supplied URL or query reaches a response header", "Every new parameter is bounded"); §3.1 resolution of `{ref}`; §3.4; §3.8
- Finding: `Href`'s query is "re-encoded from parsed parameters". In phase 1 the parser is only syntactic, so arbitrary values reach `Location`, `HX-Push-Url` and `HX-Replace-Url`, URL-escaped and unbounded in length. That covers:
  - `role`, `story`, `wf` and `label`;
  - `product` for `/inbox`;
  - `product`, `state`, `kind` and `open` for `/prs`;
  - the same filter fields for `/autofix`.
  §6's bounds list covers only `q`, `shown`, `rows`, `offset`, `limit` and column ids. §3.1 also does not say whether `{section}` is checked before the id-to-slug redirect. If it is not, a client-chosen path segment is written into `Location`.
- Recommendation: Reword §6 to say what is actually true: values are escaped, same-origin, and limited to an allow-list of keys. Add a length bound for every kept value, a syntactic check for each in phase 1, and the rule that `{section}` is validated before any redirect. Add one assertion to `TestTheProductIdRedirectsToTheSlug` for an unknown section.

### The Board/Table switch and sort links carry stale filters - Severity: Major
- Affected scope: §4.5, the first bullet, "The bar is outside every element a live response replaces" and "One hook carries the window"; §4.6, the last bullet
- Finding: The **Board** and **Table** links, and the table's sort-header links, are rendered by the server "with the current filter" at render time. Later filter changes go to `board/live`, which never replaces the bar. The `htmx:configRequest` hook adds form fields only "if the request does not already have them". So after the operator changes `role`, clicking **Table** sends the link's old `role` value, and the filter reverts in the board and in the address. The same applies to sort headers after a filter change.
- Recommendation: Specify one of the following:
  - The switch, sort and reset links carry only their own parameter (`view`, `sort`, `dir`) and the hook supplies every filter field from the form.
  - `board/live` also re-renders those links out of band.
  Add a case to `TestAMoveAnswersWithTheBoardNotTheSheet` or to a new test.

### "Needs you" means two different sets - Severity: Major
- Affected scope: §4.2 `needs` row; §4.5 checkbox "Needs you"; §4.6 Status column; the existing "Needs you" column (`board.go:137`)
- Finding: The "Needs you" column and badge mean `AWAITING_INPUT` only. The new "Needs you" filter means `AWAITING_INPUT` or `REVIEW`, or `BLOCKED` with an open send-back. An operator who ticks "Needs you" will see Review and Blocked cards under a label that elsewhere means "an agent asked you a question". Two engineers could reasonably pick either meaning.
- Recommendation: Give the filter its own label (for example "Waiting on a person"), or make it match the column. State the choice explicitly.

### Column-only answers leave the product bar's counts stale - Severity: Major
- Affected scope: §5.1, the bullets "only the columns whose hash differs" and "A move made on this board answers the same way"
- Finding: Today a move or a refresh resends the whole sheet because "the nav counts sit inside #boSheet" (`templates/backoffice.html:137-139`). In product mode the bar shows the Timeline count, which every `TaskMoved` changes, and §5.5 deliberately keeps that count. Once a move or a live refresh returns only columns and the count line, the Timeline count, and any other bar count, goes stale until the next full navigation. The document also does not say whether the move answer still goes through `RenderWithChrome`, which refreshes the needs chip.
- Recommendation: Either include the bar's counts as an OOB element in `board/live` and the move answer, or accept and state the staleness. Also state that the move answer is rendered with the chrome.

### A move's board answer sets `HX-Replace-Url` on a PUT, contradicting §3.4 - Severity: Major
- Affected scope: §4.3 `board/live` row ("Sets `HX-Replace-Url`… when that differs"); §5.1 ("writes the `board/live` response"); §3.4 last table row; §10.1 `TestNavigationPushesReplacesOrLeavesTheAddress`
- Finding: §3.4 says a non-GET request never rewrites the address, and the phase-1 test asserts that a `PUT …/move` sets no header. In phase 2, the move "writes the `board/live` response", and that response sets `HX-Replace-Url` whenever the cleaned query differs. That can happen, for example, when a value is cleared because the move emptied a facet.
- Recommendation: State that the board/live writer omits the header for non-GET requests, and add that case to a phase-2 test.

### A filter can be active with no visible control - Severity: Major
- Affected scope: §4.1 `BoardFacets` comment ("A select with fewer than two options is not shown") and step 2 (`Label` checked against the closed vocabulary, not the facets)
- Finding: Two paths lead to the same result:
  - `label=X` where no task has label X survives step 2, because it is in the vocabulary. The select, built from the facets, cannot show it, so the board says "No cards match" with no visible reason.
  - A label facet with exactly one value hides its select, yet `label=that` from a URL still narrows the board.
- Recommendation: Clear any value that is not in the facets, for every field including `Label`. Alternatively, always show a select whose parameter is set. Add the case to `TestBoardFiltersNarrowCardsAndCounts`.

### Anchors that point at the wrong symbol - Severity: Nit
- Affected scope: Terms (`#boSheet` at `templates/backoffice.html:45`); §3.4 table (`PRFilter` at `prview.go:125`, `AFFilter` at `autofix_view.go:672`)
- Finding: Line 45 is the `{{define "bo-sheet"}}` line; the element is at :46, which §1.1 cites correctly. `prview.go:125` is `prHref`; `PRFilter` is at `prview.go:36`. `autofix_view.go:672` is `afHref`; `AFFilter` is at `autofix_view.go:157`.
- Recommendation: Correct the three anchors.

### "Three callers keep `buildBO`" lists four call sites - Severity: Nit
- Affected scope: §3.4, the paragraph after the call-site table
- Finding: Two existing sites plus two new page handlers make four `buildBO(` calls. "Fails if a fourth appears" is ambiguous about what is being counted.
- Recommendation: Say exactly which call sites `TestOnlyPagesAndTheReadinessBlockCallBuildBODirectly` allows.

### `busy()` is described as something it does not do today - Severity: Nit
- Affected scope: §5.3, the third bullet
- Finding: `static/live.js:57-61` holds a refresh for an open modal or a focused input inside `#boSheet`; it does not check dragging. The text reads as if the drag check exists. "A focused filter field no longer holds it" also does not say whether other inputs, such as the BRD editor, still hold it.
- Recommendation: Mark the drag check as new, and say that the input check is narrowed to exclude `#boardFilters` only.

### "The one visible removal" is not the only one - Severity: Nit
- Affected scope: §4.7 vs §4.4, the first bullet
- Finding: §4.4 removes the aside (graph projection and its other blocks) from the Board tab. That is also a visible removal.
- Recommendation: List both removals in §4.7.

### "Total" has two meanings, and the example numbers do not add up - Severity: Nit
- Affected scope: §4.1 `BoardPage.Total` vs `BoardPageColumn.Total`; §4.4 "30 of 412"; §4.5 count-line example
- Finding: `BoardPage.Total` means all cards of the product. `BoardPageColumn.Total` means matching cards in one column. The example "412 cards … Backlog 13 · … · Done 412" sums to more than 412.
- Recommendation: Rename one field (for example `Matching`) and fix the example numbers.

### `GET /ui/bo` sets the address but not the title - Severity: Nit
- Affected scope: §3.4 "The title"; the call-site table (`handlers_bo.go:137`)
- Finding: `handleBOSheet` renders `bo-sheet`, not one of the four templates that gain `bo-title`. It is called from `templates/chrome.html:33`. Its response will move the address but leave the title unchanged.
- Recommendation: Say how this route's title is set, or state that it always lands on the product list title.

### The product-name link in the Auto-fix table has no stated fix - Severity: Nit
- Affected scope: §3.6, the second bullet
- Finding: `templates/backoffice.html:904` (`href="products/{{.ID}}"`) is one of the eleven links. Neither `prPageHref` nor `afPageHref` covers it. The test would catch it, but the design never says what its `href` becomes.
- Recommendation: Name its new `href` (`p/<slug>/brd`) and where the slug comes from.

### The Board/Table links are described as pointing at a fragment route - Severity: Nit
- Affected scope: §4.5, the first bullet
- Finding: "`<a href hx-get>` to the product fragment route" contradicts §3.6's rule that every `href` beside an `hx-get` is a page URL.
- Recommendation: Say `href` = `p/<slug>/board?…` and `hx-get` = `products/<id>?tab=board&…`.

### How `HX-Current-URL` is compared is unspecified - Severity: Nit
- Affected scope: §3.4 `pushURL`
- Finding: "Compares … path and query" does not say whether the query is compared as a string or as parsed values. `open=fix:12` and `open=fix%3A12`, or a different parameter order, would count as different and produce spurious `HX-Replace-Url` headers.
- Recommendation: State that both sides are parsed and re-encoded before comparison.

### Project terms used without definition - Severity: Nit
- Affected scope: §3.2 ("the fold", "genesis"), §4.1 ("its projection", "timing index"), §1.2 ("run record"), §4.1 step 3 ("design-blocked", "send-back"), the Related line ("live region")
- Finding: These are specific to this project and are neither defined nor linked in the document.
- Recommendation: Add one line each to Terms, or a link to where each is defined.

### Who sets `PageTitle` for `/` and the signed-out page - Severity: Nit
- Affected scope: §3.3 title table; §3.7; §9 phase-1 change list
- Finding: `handleIndex` (`handlers_tasks.go`) and the gate's signed-out page both render `page`. Neither is shown setting `PageTitle`, and `handlers_tasks.go` is not in the change list. Today the title is a literal at `templates/page.html:6`.
- Recommendation: Say where the default title comes from, and add the file to §9 if it changes.

### Missing `?` in the `HX-Replace-Url` formula - Severity: Nit
- Affected scope: §4.3 `board/live` row
- Finding: `"/p/<slug>/board" + BoardPage.Query.Encode()` has no `?` separator, and gets an empty one when there is no query.
- Recommendation: Write the formula the way §3.4 does.

### The back-fill mutation path is vague - Severity: Nit
- Affected scope: §3.2, the `NewArcadeStoreFor` bullet
- Finding: "One mutation writes the product … and emits `RecordUpdated`" runs before `installHook` (`store_arcade.go:166`). The document does not name the function that persists it or what happens to an in-memory event when the write fails. The event text also departs from the existing `<name>: …` convention (for example `store_mem.go:1860`).
- Recommendation: Name the persist function, and state what happens to the in-memory event if the transaction fails.

### A product with no tasks has no columns to swap into - Severity: Nit
- Affected scope: §4.4 "All nine column elements are always in the page"; `templates/backoffice.html:397,424-425`
- Finding: Today `pane-board` renders "No tasks yet" and no columns when there are no cards. A `board/live` OOB response for the first card would then have no `col-<id>` target.
- Recommendation: State that the columns render even with zero tasks, or that the first card triggers a full refresh.

Counts: B=0 M=8 N=14
