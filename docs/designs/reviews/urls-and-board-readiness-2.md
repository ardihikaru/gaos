<!-- Round 2, 2026-11-26. Lens: readiness. Target: docs/designs/urls-and-board.md (rev 2). Verbatim reviewer output. -->

# Readiness review: `urls-and-board.md` (rev 2)

I checked the document against the checkout at `/home/ardi/devel/projects/ardihikaru/my-projects/gaos/.claude/worktrees/feat-urls`.

**What holds up:**
- **Call sites.** The `buildBO` call-site table in §3.4 matches the source line for line.
- **Identifiers.** TC-722…731, TC-732 and TC-733…745 follow from the 721 rows of `qa-test-plan.html`. D-093 and D-094 are the next free decision numbers after D-092.
- **Counts.** 587 → 597 → 610 Go tests and 91 → 92 → 93 decision-log entries add up. The ten phase-1 tests and thirteen phase-2 tests match their tables.
- **References.** The eleven `<a … hx-get>` elements, the route lines in `main.go`, `readOnlyGET` at `auth_test.go:258` and the slug and Arcade anchors are where the document says.

The findings follow.

### Phase-2 tests that must change are missing from the change list - Severity: Major
- Affected scope: §9 (both phases), §10.1 and §10.2 "Changed, keeping their ids"
- Finding: Phase 1 turns the navigation buttons into links and changes the Auto-fix `href`s to page URLs (§3.6). Four assertions in `dashboard/autofix_view_test.go` match the old markup exactly: `:143` (`<button aria-current="page"   hx-get="ui/bo/autofix"`), `:166` (`<a href="ui/bo/autofix?state=merged" …`), `:237` (`href="ui/bo/autofix"`) and `:240` (`data-af-close href="ui/bo/autofix?state=draft"`). All four will fail. The file is not in the phase-1 change list, and its tests are not in the "Changed, keeping their ids" list. Under `.claude/rules/qa-test-plan.md` that list drives the register rows that must be updated. Phase 2 rewrites `pane-board`, drops the aside on the Board tab and caps columns at 30. Five existing tests render `?tab=board` and assert its content: `board_test.go:64`, `conversation_test.go:124`/`:146`, `design_iterate_test.go:525`, `needs_test.go:165` and `timing_test.go:284`. The document does not say whether any of them is affected. A write allowlist taken from §9 would refuse these edits.
- Recommendation: Add `autofix_view_test.go` to the phase-1 list, give the new expected strings, and add its tests to "Changed, keeping their ids". For phase 2, check the five Board-tab tests and either list each one with its new assertion or say outright that none changes.

### Where `View.PageTitle` is set on htmx responses, and what sign-out prints - Severity: Major
- Affected scope: §3.3, §3.4 "The title"
- Finding: `PageTitle` is a field on `View`, but `sheet` returns a `*BOView` and sets nothing on `v`. The document never says who fills `v.PageTitle` at each of the roughly thirteen call sites. It also says nothing about `handleLogout` (`handlers_bo.go:105-114`). That handler renders `sheets` with `v.BO = nil`, and `sheets` is one of the four templates that will start with `bo-title`, so it prints `<title></title>` and blanks the tab title. Separately, `handleBOSheet` (`GET /ui/bo`, the topbar button, `templates/chrome.html:33`) renders `bo-sheet`. `bo-sheet` is not one of the four templates, so after that button the address changes and the title does not. Different implementers will fill the title in different places.
- Recommendation: Have `sheet` take `v *View` and set `v.BO` and `v.PageTitle = pageTitle(bo)`. Give a fixed title for a nil `BO` (`GAOS · Products`). Add `handleBOSheet`'s template to the title list, or explain why it is left out. Add "sign out keeps a non-empty title" to `TestNavigationPushesReplacesOrLeavesTheAddress`.

### Phase-1 `parseBoardQuery` and `BoardQuery` are not specified - Severity: Major
- Affected scope: §3.4 (the board row), §3.8, §4.1, §4.2, §9 (`urls.go` versus `board_page.go`)
- Finding: Phase 1 is said to "parse and re-encode" the Board parameters into `Href`, `HX-Replace-Url` and the redirect `Location`. Three things about it are not stated:
  - The phase-1 signature. §4.2 says it returns `(BoardQuery, BoardWindow)`, but `BoardWindow` only arrives in phase 2.
  - Whether phase 1 checks `label`, `sort` and `dir` against their closed lists. They can be checked without a product.
  - Any length bound on `role`, `story` and `wf`. Until phase 2's facet step clears them, an arbitrary value goes back into a response header.

  The type's home also conflicts: §4.1 puts `BoardQuery` in `board_page.go`, while §9 puts it in `urls.go` and only moves `parseBoardQuery`. Phase 1 has no test of its own for this code; the round-trip test is TC-743, in phase 2.
- Recommendation: Give the exact phase-1 signature and per-parameter rules, for example: `view` only `table`; `label`, `sort` and `dir` checked against their lists; `role`, `story` and `wf` matched against `[A-Za-z0-9._:-]{1,64}` or dropped. Name one file for `BoardQuery`. Move `TestTheBoardQueryRoundTrips` into phase 1, or add a phase-1 case to the navigation test.

### "Hide empty columns" never takes effect, because the column hash ignores it - Severity: Major
- Affected scope: §4.4 (`hidden` with `empty=hide`), §5.1 (`data-rev`), TC-740
- Finding: A column is re-sent only when its hash changes. The hash covers `Total` and the cards. Ticking "Hide empty columns" changes neither for an empty column, and the hook sends `rev` with the filter's `board/live` request. So no column is re-sent, and none gets its `hidden` attribute. Unticking the box fails the same way. The test only covers the case where a hidden column gains a card.
- Recommendation: Put the column's hidden flag (or `Query.HideEmpty`) into the hashed input, or say that a request from `#boardFilters` carries no `rev`. Add "toggling `empty=hide` hides or shows the empty columns" to TC-740.

### The Board/Table and sort links go stale after a filter change - Severity: Major
- Affected scope: §4.5 (`nav.af-seg`, "the bar is outside every element a live response replaces"), §4.6 (column-header sort links; only `<tbody>` is replaced)
- Finding: The server writes the Board/Table links "with the current filter" into their `hx-get`, and the header sort links carry the filter too. After that, neither the bar nor `<thead>` is ever replaced. The hook only adds fields to paths that start with `products/<id>/board`, and the view switch calls `products/<id>?tab=board…`. So after a filter change, switching to the table or re-sorting uses the old filter and loses the change. The exit check "a reload keeps the filter and the view" would not catch this.
- Recommendation: Choose one fix: make the hook also cover `products/<id>?tab=board` requests; send the view switch, the headers and the counts as OOB elements on every `board/live` response; or build those URLs client-side from `#boardFilters`. Add a test that changes a filter and then switches view.

### Other Board mutations, and a `label` name clash - Severity: Major
- Affected scope: §5.1 (only `handleMoveCard` changes), §4.5 (the hook), §9 phase 2 (`handleMoveCard` only)
- Finding: `handleSendBack` (`handlers_board.go:149`) and `handleSetLabel` (`:226`) are also htmx mutations on the Board tab. Both still call `renderBOAfter(…, "board")`, which replaces the whole sheet, the filter bar included. The document does not say:
  - whether these two move to the `board/live` answer;
  - where `buildBO`'s `BoardPage` reads its query from on a POST (`r.URL.Query()` or `r.Form`).

  The hook also adds filter fields "if the request does not already have them". `POST …/label` carries its own form field `label` (`handlers_board.go:212`). That field would be read back as the `label` filter, so the re-rendered board would be filtered by the label just set.
- Recommendation: Give the answer for send-back and set-label (the `board/live` answer, like a move). Say that `parseBoardQuery` reads `r.URL.Query()` only, with the hook putting the window and filter into the query string even on PUT and POST, or rename the filter parameter. Add one case to TC-741.

### The vendored sisflow does not "record `exhausted`" - Severity: Major
- Affected scope: §4.8 "Show more", §7 (the stale-offset row)
- Finding: `static/vendor/sisflow/board/index.js:166` only emits `board:page-loaded` with `exhausted`. Nothing under `board/view/` listens for it, and `state.exhausted` is never set (`virtual-list.js:25`, `:43`). A column stops asking only because `state.loading` stays true until the cell's card count changes (`virtual-list.js:29`). So when `loadPage` drops every card of a page as a duplicate, that column stops loading for good, until a `reload()` happens to change its count. The §7 recovery ("the next `reload()` asks for the column at its real depth") does not unstick it if the count is unchanged. The design is built on a claim about vendored code that is not true, and the vendored code cannot be edited.
- Recommendation: Correct the claim. Specify what `loadPage` does when every card it got is a duplicate: for example, fetch again from `offset + received` until it has a new card or `exhausted` is true. Add a phase-2 exit step that scrolls a column whose page is all duplicates.

### `TestEveryRouteIsClassified` must relax an existing assertion, not only gain new ones - Severity: Nit
- Affected scope: §6, second bullet
- Finding: `auth_test.go:332` requires every back-office route to answer a signed-out request without `HX-Request` with `401` JSON. Page routes will answer with HTML, so that loop must skip `pagePatterns`. The document says the test "keeps its invariant and gains four assertions".
- Recommendation: Say that the JSON check at `:332` excludes `pagePatterns`.

### The allowed `buildBO(` callers are not one exact set - Severity: Nit
- Affected scope: §3.4 "Three callers keep `buildBO` … fails if a fourth appears"
- Finding: The list names three page handlers plus the readiness block, which is four, and `sheet` itself calls `buildBO`, which makes five. "Three" and "a fourth" do not give the test a set to check.
- Recommendation: List the five allowed functions by name.

### The Auto-fix product link has no stated page `href` - Severity: Nit
- Affected scope: §3.6, second bullet
- Finding: `templates/backoffice.html:904` is `<a href="products/{{.ID}}" hx-get=…>`. It is not produced by `afHref`, so `afPageHref` does not cover it, and its row struct has no `Slug`. One implementer may print `p/{{.ID}}` (a redirect); another may add a slug field.
- Recommendation: Say which, and name the struct and file if a field is added.

### `HX-Push-Url` when the product does not exist - Severity: Nit
- Affected scope: §3.4 (`sheet`, `Href`), `handlers_bo.go:170-173`
- Finding: `sheet` calls `pushURL` before `handleProductDetail` checks `Detail == nil`. With no product there is no slug, so `Href` is not defined, yet the 404 still carries a push header.
- Recommendation: Say that `Href` is empty and `pushURL` sends nothing when `Detail` is nil for a product view.

### How the refresh URL merges `location.search` - Severity: Nit
- Affected scope: §3.4 "A background refresh sends the page's own query"
- Finding: `live.js:93` and `:105` already contain `?`. Plain concatenation produces `??` or duplicate keys.
- Recommendation: Say "merged with `URLSearchParams`, the refresh URL's own keys win".

### Does a move answer carry `HX-Replace-Url`? - Severity: Nit
- Affected scope: §3.4 row 4 ("a mutation never rewrites the query") versus §5.1 (the move "writes the `board/live` response"), and §4.3 (`board/live` sets `HX-Replace-Url`)
- Recommendation: Say whether the move's answer includes `HX-Replace-Url`.

### The Arcade back-fill event is not fully described - Severity: Nit
- Affected scope: §3.2, `NewArcadeStoreFor` bullet
- Finding: The event kind, its `DeskID`, whether the event goes through `mutate` (`store_arcade.go:533`) before `installHook`, and the MemStore method that does it are not named. Existing `RecordUpdated` texts are prefixed with the product name (`blueprint_store.go:37`).
- Recommendation: Name the method, the event kind, and the full text.

### The base branch of `feat/board` is not stated - Severity: Nit
- Affected scope: §2 ("can be approved separately"), §9, §10.2
- Finding: Phase 2 relies on phase 1's `Href`, `BoardQuery` and `sheet`, but no branch point is given.
- Recommendation: Say "`feat/board` is cut from `main` after phase 1 is merged."

Counts: B=0 M=7 N=7
