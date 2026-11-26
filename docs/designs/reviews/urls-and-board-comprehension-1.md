<!-- Round 1, 2026-11-26. Lens: comprehension. Target: docs/designs/urls-and-board.md (first draft). Verbatim reviewer output, except that a private product name the first draft used as its example is redacted (D-085). -->

I read the design from inside the main repository's `.claude/worktrees/` path, because that is where the task put it. While I did, the harness attached three of the main checkout's `.claude/rules/` files: `agent-workspace.md`, `qa-test-plan.md` and `frontend.md`. These are rubric rule files, not memory. No `CLAUDE.md`, `AGENTS.md` or `GEMINI.md` content was attached or opened. I read the rubric and code only from the feat-urls checkout. The canary check should still be run to confirm this.

# Goldfish comprehension review: `docs/designs/urls-and-board.md`

## Paraphrase

### What this proposes

**Phase 1 (v0.55.0, D-093): URLs.**
- Each of the nine back-office views gets its own page route: `/inbox`, `/stories`, `/diagrams`, `/knowledge`, `/agents`, `/archive`, `/integrations`, `/autofix` and `/prs`.
- Each product section gets a route too: `/p/{ref}/{section}`, where the section is one of the nine tab ids. `/p/{ref}` redirects to `/p/<slug>/brd`.
- Page routes return whole documents with `no-store`. The existing fragment routes stay as they are and are still what htmx calls.
- Products gain a stored, unchanging `Slug`. It is assigned in `CreateProduct`, back-filled at store construction, and written as a property with no schema change and no event. A product id in the URL redirects (302) to the slug. An unknown product or section returns a 404 page with a toast.
- Every built back-office view gets a canonical `Href`. A new `pushURL` compares it with `HX-Current-URL` and sends one of three things: no header, `HX-Replace-Url` when only the query differs, or `HX-Push-Url` when the path differs. The response also carries a `<title>`.
- htmx's history cache is turned off (`historyCacheSize:0`, `refreshOnHistoryMiss:true`), so Back and Forward reload the page from the server.
- Navigation buttons become `<a href hx-get>`, so links can be copied or opened in a new tab.
- The auth gate gets a new route class, `classPage`. A signed-out request to a page route gets the generic sign-in page (200), and the product is never looked up. Signing in from a deep URL answers `204` with `HX-Refresh`, so the page reloads in place.

**Phase 2 (v0.56.0, D-094): the Board.**
- A new store read, `BoardPage(productID, BoardQuery)`. It makes one pass over the tasks, filters them, sorts each column (Done and Canceled newest first), keeps 30 cards per column (raised in steps of 30, up to 300), and decorates only the cards it keeps, using an id map so lookups are no longer quadratic.
- URL parameters for filtering, sorting and grouping, with a round-trip encoder.
- Four new HTML and JSON routes: a live per-column diff, column "show more", table rows, and sisflow's card pages. The existing board JSON gains `limit`.
- The board gets full width, 300px columns and no aside. A filter bar and a Board/Table switch are added, plus a paged, sortable, grouped server-rendered table.
- Story lanes are removed from sisflow.
- Live updates replace only the columns whose content hash changed, keep scroll positions, wait for hints to settle, and pause while the tab is hidden. sisflow reloads its store instead of being rebuilt. Fresh cards get a fading marker.
- `buildBO` stops copying events and rendering sequence diagrams on the Board tab.
- Numeric performance targets are set.

### How the existing system works, per this document

- Every back-office interaction is an htmx GET into a hidden `#sink`. The answer is the whole `#boSheet`, sent out of band. Nothing writes to browser history, so the address stays at `/app/`.
- Only `GET /{$}` returns a whole page. `GET /products/{id}` and `GET /ui/bo/{view}` return fragments.
- The board renders every task in nine 140px columns inside a 1400px layout that has a 300px aside.
- A `task` hint refetches the whole product tab. That runs `buildBO`, which copies the event stream and renders diagrams, and then replaces the whole sheet. Only the sheet's vertical scroll is kept.
- Two decorators look up each card with a linear scan, so the board is quadratic in the number of cards.
- There are two boards: sisflow, a client library, and a server-rendered fallback. sisflow was broken from v0.44.0 until v0.54.1.
- The sign-in gate answers a signed-out non-htmx request with 401 JSON.
- Products are rebuilt by `load`, not by the event fold.

The paraphrase was possible for every section. No comprehension Blocker.

## Anchor checks

Most anchors match the code. These all checked out:
- `main.go:499/519/524/526/537`; `handlers_tasks.go:17`; `handlers_bo.go:56/129-176/188/221-286/331/338-344/516`
- `board.go:156/242`; `conversation.go:208/621`; `sendback.go:375`; `designgate.go:545`; `timing.go:404/489`; `qacatalog_store.go:383`
- `store_mem.go:1815/2462`; `store_arcade.go:154/763/1496`; `fold.go:442`; `render.go:192-209`; `auth.go:502-558`; `auth_test.go:258/290`; `board_test.go:62`
- `templates/page.html:6/12/25`; `templates/backoffice.html:46/59/71-79/87-96/122/219/400/401`
- `static/app.css:789-806/812/855/1216/1616`; `static/live.js:57/63/97`; `static/app.js:409`; `static/board.js:184/217/233`; `virtual-list.js:6`; `rest.js:32`; `integrations.go` (no-store at :29)
- The register's highest id is TC-721, and the decision log holds "Ninety-one entries".

The anchors that drifted are listed in the Nits.

## Findings

### A refresh or mutation rebuilds `Href` without the board's query, so the filter is dropped from the address - Severity: Major
- Affected scope: §3.4 (`Href`, `pushURL` called from `renderBOAfter`), §3.8, §5.1
- Finding: `buildBO(r, view, productID, tab)` (`handlers_bo.go:188`) gets only a view, a product and a tab. The document says `Href` carries the board query, for example `p/[private product name, redacted]/board?view=table`. It never says where that query comes from on a request that is not the navigation itself.
  - In phase 1 the live refresh asks for `products/{id}?tab=board` (`static/live.js:105`).
  - Every product mutation goes through `renderBOAfter`.
  - Neither request carries the board query. The `Href` they produce has the same path and no query, and under §3.4's own rule that sends `HX-Replace-Url`. The address loses `?view=table` or the filter on the next hint or mutation, and in phase 2 the re-rendered board loses the filter too.
  - §3.8 says phase 1 "passes through" the parameters but does not say how.
- Recommendation: State where `buildBO` reads the board query, for example `r.URL.Query()` filtered through `parseBoardQuery`. State that every caller sends it: the live refresh, `renderBOAfter` callers, and the mutation forms. Alternatively, have `pushURL` leave the query alone when the new `Href` has none and the path matches. Add the case to `TestNavigationPushesReplacesOrLeavesTheAddress`.

### `board/live` must send `HX-Replace-Url`, but the only mechanism defined needs `buildBO`, which it does not call - Severity: Major
- Affected scope: §4.3 (`board/live` "Does not call `buildBO`"), §4.5 ("The response carries `HX-Replace-Url` … (§3.4's rule)"), §3.4, §4.8 (`surface=sisflow`)
- Finding:
  - §3.4 defines `pushURL(w, r, bo)` over `bo.Href`, which `buildBO` sets. It lists the five places it is called, and `board/live` is not one of them. §4.3 says `board/live` does not call `buildBO`. So how `board/live` builds its `Href` is undefined.
  - The request carries parameters that must not reach the address: `shown`, `rev` (§5.1) and the hidden `surface=sisflow` (§4.8). §4.2's parameter table does not list `surface`, `shown` or `rev`.
  - So it is undecided whether `parseBoardQuery` reads `shown`/`offset`, or whether `Encode()` writes them. This also bears on `TestTheBoardQueryRoundTrips`, because `Shown` is a field of `BoardQuery` but is "not in the URL".
- Recommendation: Specify the `Href` that `board/live` builds: `p/<slug>/board` plus `BoardQuery.Encode()`, where `Encode` writes only the §4.2 parameters. Add `board/live` (and `board/rows` if relevant) to the list of `pushURL` callers. List `surface`, `shown`, `rev` and `offset` as request-only parameters that `Encode` never writes.

### "Hide empty columns" cannot be applied by per-column OOB replacement - Severity: Major
- Affected scope: §4.1 (`Columns … always all nine`), §4.2 (`empty=hide`), §5.1
- Finding: `board/live` replaces one element per column by id. If `empty=hide` leaves a column out of the DOM, a later response cannot swap that column back in, because an OOB swap with no target is dropped. Showing or hiding columns when a filter changes is also a structural change to `.board-ssr`. §5.1 says `.board-ssr` is never replaced. It is not said whether a hidden column is rendered as an empty `hidden` element, or whether a filter change re-renders the whole region. The same gap applies to the filter form's own requests: do they send `rev` and `shown`, and do they reset each column's depth to 30?
- Recommendation: Say that all nine column elements are always rendered and that `empty=hide` sets `hidden` on them, so OOB always has a target. Say whether a filter change sends `rev`/`shown` or resets depth to 30.

### The response to a move made on the server board is undefined - Severity: Major
- Affected scope: §5.1, last bullet; §10 `TestAMoveAnswersWithTheBoardNotTheSheet`
- Finding: "answers with the same response plus the toast" refers to `board/live`'s response, which needs the filter, `shown` and `rev`. The move is `hx-put` with `hx-vals='{"column":…}'` (`templates/backoffice.html:417`) and carries none of them. Without them the server cannot know which columns differ, or how deep each is open. The test only asserts the absence of `boSheet`.
- Recommendation: Specify what the move button sends, for example `hx-include="#boardFilters"` plus the `shown` and `rev` values. Alternatively, say the move answers with exactly the source and target columns at their current depth, and say how that depth is known. Make the test assert it.

### sisflow's per-column depth cannot be expressed by the JSON's single `limit` - Severity: Major
- Affected scope: §4.3 (JSON `limit=N`), §4.8 (Filters, Show more), §5.2 (`reload()` "with the current filter and depths")
- Finding: `reload()` fetches "with the current filter and depths". The JSON route only takes one `limit` that applies to every column, and `shown` is defined only for `board/live`. After the operator scrolls Done to 90, a reload either cuts Done back to 30 or fetches 90 cards in every column. Two engineers would choose differently.
- Recommendation: Give the JSON route the same `shown=col:n,…` parameter, or state the rule `board.js` follows (for example `limit` = the maximum depth, then trim each column on the client).

### The sisflow "Show more" description ignores that sisflow already ingests the page itself - Severity: Major
- Affected scope: §4.8 "Show more"
- Finding: In the vendored library, the `board:load-more` handler calls `provider.loadPage(req)` and then `store.ingest({cards: page.cards}, req)` itself (`static/vendor/sisflow/board/index.js:162-166`). The design says `board.js` answers by fetching `…/board/cards`, merging the cards into its own copy, and reloading the whole document into the store. If `board.js` both returns the cards and reloads, the cards are ingested twice. If it returns `{cards: []}`, how `exhausted` is reported is unclear. The document's own route `…/board/cards` is exactly what the stock REST provider already calls (`rest.js:32-33`).
- Recommendation: Decide one path. Either `loadPage` returns `{cards, exhausted}` and sisflow ingests them, while `board.js` only records the new depth for the next `reload()`. Or `loadPage` reloads and returns nothing to ingest. State which.

### The `needs` filter requires send-back state before the cut, which conflicts with "decorate only the cards kept" - Severity: Major
- Affected scope: §4.1 steps 1 and 4; §4.2 `needs`; §10 `TestBoardPageDecoratesOnlyThePage`
- Finding: `needs` matches "`BLOCKED` with an open design send-back". That state is computed by `decorateBoardGate` through `gateDesignDepLocked` and `sendBacksLocked` (`sendback.go:386-389`), and step 4 runs that decoration only for kept cards. Yet step 1 tests the filter, including `needs`, for every task before the cut. The document does not say that the send-back check is computed in step 1 for blocked design reviews, or what that costs.
- Recommendation: Say that step 1 computes send-back openness only for tasks that are `BLOCKED` with `ReasonDesignBlocked`, and that this is the one predicate evaluated before the cut. Keep the "decorate only the page" test scoped to timing, or say so.

### Counting the Timeline "without the copy" needs a store capability the change list does not have - Severity: Major
- Affected scope: §5.5 first bullet; §9 phase 2 change list
- Finding: The Timeline tab label prints `len .Timeline` on every product tab (`templates/backoffice.html:77`). The `Store` interface offers only `Events(ctx)`, which returns everything (`store.go:70`). The document says the count is taken "without the copy" and does not say how. §9 adds only `BoardPage` to `store.go`, and `TestTheBoardTabSkipsTheEventCopyAndTheDiagrams` asserts that `Events` is not called at all.
- Recommendation: Name the new method, for example `EventCount(ctx, productID)`, with its MemStore and ArcadeStore implementations. Add it to §9. Alternatively, say the count is dropped from the label outside the Timeline tab.

### The `rebuild-restart.sh` assertion has no register row in the counts - Severity: Major
- Affected scope: §9 phase 1 (`deploy/rebuild-restart.sh`), §10 phase 1 counts ("register 721 → 729")
- Finding: Phase 1 adds a check to `deploy/rebuild-restart.sh`: `/app/inbox` and `/app/p/x/board` answer 200. That script's assertions are register rows (the TC-017… range). The phase 1 arithmetic counts only the eight Go tests, so either a row is missing or the check is folded into an existing row without saying so. The design contract says counts must follow from the change list (`docs/designs/README.md` rule 4).
- Recommendation: Either add a TC row for the new `rebuild-restart.sh` check and correct the arithmetic to 721 → 730, or name the existing TC whose scenario it extends.

### "Eleven patterns each return a full document" includes a redirect - Severity: Nit
- Affected scope: §3.1 ("eleven patterns"), §10 `TestADeepURLRendersThePage`
- Finding: `/p/{ref}` is a 302 by §3.1, yet the test says each of the eleven patterns returns a full document with `data-view`/`data-tab`. The §3.1 table also lists `/` as a row, but `/` is not one of the eleven, which makes the count harder to follow.
- Recommendation: Say ten patterns render and `/p/{ref}` redirects. Mark `/` in the table as existing and not counted.

### Line anchors that drifted - Severity: Nit
- Affected scope: §5.1 (`handlers_board.go:79`), §5.2 (`index.js:122`), §4.8 (`index.js:162`)
- Finding:
  - `handleMoveCard` is declared at `handlers_board.go:50`. Line 79 is its `renderBOAfter` call.
  - `_store` is at `static/vendor/sisflow/board/index.js:121`.
  - The `loadPage` call is at `:164`. The listener starts at `:162`.
- Recommendation: Anchor each to the declaration, or say which line the reference means.

### The operator's decisions are not anchored - Severity: Nit
- Affected scope: §1.3 ("The operator decided that both boards stay"), §8 rows "Use the product id…" and "Build the Board features on one board only"
- Finding: These decisions carry no file or line. The decision log ends at D-092. The README requires each claim to carry an anchor, or to be labelled as an assumption.
- Recommendation: Point to where the decision is recorded, or say it will be recorded as part of D-093/D-094.

### `BoardFacets` is used but not defined - Severity: Nit
- Affected scope: §4.1 (`Facets BoardFacets`), §4.5
- Finding: The type's fields are never given. It also never says whether facets are taken from all of the product's tasks or from the filtered set.
- Recommendation: Give the struct and the rule, for example "facets come from all tasks of the product, before filtering".

### "The three closed vocabularies" does not match the list - Severity: Nit
- Affected scope: §2 Non-goals, second bullet
- Finding: The sentence names five things (event type, task status, label, role, docs CSS class) and then speaks of "the three closed vocabularies".
- Recommendation: Name the three, or drop the number.

### "The five routes of §4.3" mixes new and existing routes - Severity: Nit
- Affected scope: §6 second bullet; §4.3 "added to `readOnlyGET`"; §9 "four routes"
- Finding: §4.3 lists four new routes plus the existing `GET /products/{id}/board`, which is presumably already classified.
- Recommendation: Say "the four new routes" and say whether the existing JSON route's entry changes.

### The dependency on PR #45 landing first is unstated - Severity: Nit
- Affected scope: §1.3, §10 baselines (TC-721, 587 Go tests)
- Finding: The baselines assume v0.54.1 (PR #45) has been merged. The measurements are "on `main` at `f71aa46`", which is before that merge.
- Recommendation: State that phase 1 starts after PR #45 is merged to `main`.

### Which side decides that a card is "fresh" is unstated - Severity: Nit
- Affected scope: §5.4
- Finding: "A card that is new in a column after a live response" could be decided by the server or by the client. If the server adds `is-fresh` to the HTML, it changes the column hash of §5.1. For sisflow, the `css` field is set in `board.js` (`static/board.js:203`).
- Recommendation: Say that `live.js` compares the card ids before and after the swap, and that `board.js` does the same before `reload()`. Say the server never emits `is-fresh`.

### `handleLogin` with no `HX-Current-URL` - Severity: Nit
- Affected scope: §3.7 "After sign-in"
- Finding: Only the cases "path is `/`" and "otherwise" are given. A login request without the header falls into "otherwise".
- Recommendation: Say that a missing header answers as today.

### What `BoardPage` and the limited JSON do with workflow timing totals - Severity: Nit
- Affected scope: §4.1, §4.3 JSON with `limit`
- Finding: `Board.Workflows` is computed from every task's timing (`timing.go:489-505`). The document says timing is computed only for kept cards, but never says whether `workflows` is still returned when `limit` is set, or whether it keeps the full-product cost.
- Recommendation: State whether `workflows` is omitted, kept at full cost, or computed some other way.

These sections are solid: §1.1, §3.2 (slug algorithm and storage; checked against `slug()`, `writeProduct`, `fold.go:442` and the two `NewArcadeStoreFor` branches), §3.5, §6 (checked against `sitePrefix` and the gate), §8, §11.

Counts: B=0 M=9 N=9
