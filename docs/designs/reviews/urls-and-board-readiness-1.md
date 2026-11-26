<!-- Round 1, 2026-11-26. Lens: readiness. Target: docs/designs/urls-and-board.md (first draft). Verbatim reviewer output, except that a private product name the first draft used as its example is redacted (D-085). -->

I've added one isolation note at the end, outside the findings.

# Readiness review: `docs/designs/urls-and-board.md`

Lens: gaos-goldfish-readiness (can two engineers implement this and get the same result?).

These parts are solid: the slug algorithm (§3.2), the counter maths, and the claimed file and line anchors. Nearly every anchor I checked exists and says what the design claims; the one slip is in the last Nit. On the counter maths: TC-721 is the highest ID in use, and Go tests go 587 → 595 → 608. There are 8 tests in phase 1 and 13 in phase 2. The decision log goes from 91 to 93 entries, with D-093 and D-094 next free after D-092.

### pushURL call sites miss the handlers that change the view, including the one §8 uses to justify the header - Severity: Major
- Affected scope: §3.4 bullet 3, §8 row 1, §9 phase 1 change list
- Finding: §3.4 calls `pushURL` from four fragment handlers and `renderBOAfter` only. Other handlers also build a sheet for a different view:
  - `handlers_cw.go:87`: creating a workflow opens the Board. This is the case §8 gives as the reason for using a header instead of `hx-push-url`.
  - `handlers_conversation.go:121`, `:123`, `:156`: archive opens the Board; restore and prune open the Archive.
  - `handlers_questions.go:152`: opens the Inbox.
  - `renderBOView`, `handlers_bo.go:1026`: used by the contact handlers.

  None of these files is in the change list, so the address bar will show the wrong view after these actions. §3.4 also adds the `<title>` to `bo-main-oob` only. The other templates that carry the sheet (`bo-main-close-talk`, `cw-closed`, `sheets`) would leave the document title stale.
- Recommendation: List every `buildBO` caller that renders a sheet and state, for each one, whether it calls `pushURL`. Add `handlers_cw.go`, `handlers_conversation.go` and `handlers_questions.go` to the phase 1 change list. Say which templates carry the `<title>`.

### The fragment request's query is used as the page's query, so refreshes and mutations rewrite the address - Severity: Major
- Affected scope: §3.4 (the same-path, same-query rule), §3.8, §4.3 `board/live`, §4.5 bullet 3
- Finding: `Href` is built inside `buildBO` from the fragment request. The live refresh requests `products/{id}?tab=board` with no Board parameters (`static/live.js:105`). Mutations through `renderBOAfter` (`handlers_bo.go:516-519`) only know `productID` and `tab`. So on `/p/[private product name, redacted]/board?view=table` (accepted in phase 1, per §3.8), every live refresh or Board mutation hits "same path, different query". The server then sends `HX-Replace-Url` and removes the operator's filter from the address. This contradicts goal 6 and the premise in §3.4 that a live refresh has the same query.

  §3.8 says the Board parameters are "passed through" but does not say which parameters `Href` keeps (all of them, or only §4.2's) or that `tab` is dropped. §4.3's `board/live` does not call `buildBO`, so it has no `BOView.Href` to pass to `pushURL(w, r, bo)`.
- Recommendation: Define where `Href`'s query comes from. One option: the fragment routes forward the Board query, and `live.js` and the mutation forms send it. Another: `pushURL` never replaces the address on responses that are not navigations. Name the allowlist of parameters `Href` carries. Give `board/live` its own way to compute the replace URL, for example `pushURLFor(w, r, href string)`.

### "Count the Timeline without the copy" names no mechanism - Severity: Major
- Affected scope: §5.5 bullet 1, §9 phase 2 (`store.go`, `view.go`), `TestTheBoardTabSkipsTheEventCopyAndTheDiagrams`
- Finding: The template prints `{{num (len .Timeline)}}` (`templates/backoffice.html:77`), and the only event source on `Store` is `Events()`, which copies. The test requires no `Events` call on `tab=board`, so a new store method (a per-product event count) and a new `ProductDetail` count field are needed. Neither is named, typed or in the change list. Two implementers would add different methods, or one would drop the count.
- Recommendation: Specify the method, for example `EventCount(ctx, productID) (int, error)` on `Store`, with Mem and Arcade implementations. Name the view field and the template change. Add them to §9.

### The sisflow "Show more" mechanism conflicts with the vendored provider wiring - Severity: Major
- Affected scope: §4.8 "Show more" row, §5.2, §4.3 JSON row
- Finding: `attachProvider` already handles `board:load-more` (`static/vendor/sisflow/board/index.js:162-170`). It calls `provider.loadPage` and then `store.ingest`. §4.8 instead says `board.js` fetches `…/board/cards`, merges into its own copy and reloads the document. These are two different designs, and the design does not say which hook `board.js` uses or what `loadPage` returns.

  §5.2's `reload()` fetches "with the current filter and depths". The JSON route in §4.3 takes only a single `limit=N`, with no per-column depth. A reload would therefore cut every column that was scrolled deeper back to 30 cards.
- Recommendation: Choose one path. For example: the custom `loadPage` fetches `…/board/cards?<query>&column=&offset=&limit=30` and returns `{cards, exhausted}` for the library to ingest. State how `board.js` tracks per-column depth. Add a per-column depth parameter to the JSON route (for example the same `shown=col:n,…` as `board/live`), and add it to `TestTheBoardJSONPagesAndFilters`.

### Stale-offset recovery (§7) assumes de-duplication that neither board has - Severity: Major
- Affected scope: §7 row 4, §4.3 `board/col/{col}`
- Finding: §7 says "both boards ignore a card id they already hold". This is not true today:
  - sisflow's `store.ingest` calls `ops.assertFreeId` and throws on a duplicate id (`static/vendor/sisflow/board/core/store.js:151`).
  - The server board appends with `hx-swap-oob="beforeend:…"`, which cannot skip ids, and it would create duplicate element ids (`tm-<id>`, `cv-<id>`, `nb-<id>`).

  No file in the change list is given this job.
- Recommendation: Say who removes duplicates and how. For example: `board.js` filters out known ids before returning from `loadPage`, and `live.js` removes later duplicates of `[data-card]` on `htmx:oobAfterSwap`. Add a test or a stated manual check for each.

### `BoardQuery`'s parse, validation and encode boundaries are undecided, and its round-trip test cannot compile - Severity: Major
- Affected scope: §4.1 `BoardQuery`, §4.2, §6 "Filter values are data", `TestTheBoardQueryRoundTrips`
- Finding:
  - `parseBoardQuery(url.Values)` takes no product context, but §6 says it keeps only values "in the set the product offers". It cannot know that set.
  - The design does not say which function parses `shown`, `offset`, `rev` and the hidden `surface=sisflow` field, or whether `Encode()` writes `Shown`, `Offset` and `Limit`. §4.2 says depth is not in the URL.
  - `parseBoardQuery(q.Encode()) == q` does not compile, because `BoardQuery` contains a `map[string]int`. It cannot round-trip either, if `Encode` leaves out `Shown`.
- Recommendation: Split the URL state (filter, view, sort, dir, group) from the request state (shown, offset, limit, rev, surface), or state exactly which fields `Encode` covers. Validate against facets in `BoardPage` and say whether a rejected value is also removed from the replaced URL. Rewrite the test as `reflect.DeepEqual` on the URL-state part.

### `BoardCard` lacks the fields the filters, the table and the story chip need - Severity: Major
- Affected scope: §4.2 (`wf`, `story`), §4.6 (Story and Workflow columns, `sort=updated`, `group=workflow`), §4.7, §9 phase 2 `board.go`
- Finding: `BoardCard` (`board.go:40-74`) has no workflow id and no last-event or updated value. Its story is carried only in `Row`, and §4.7 removes `Row` from every sisflow card while still requiring a story chip on the card. The table columns, the grouping and the sisflow story chip therefore need new JSON fields. Their names and JSON tags are undecided. That also matters because the unlimited JSON must stay byte-identical.
- Recommendation: Name the new fields, for example `Story string \`json:"story,omitempty"\``, `Workflow`, and `Updated int64`. Say whether they appear only in the paged JSON (to protect the byte-identical guarantee). Add them to the `board.go` row of §9.

### Table grouping and paging do not fit together - Severity: Major
- Affected scope: §4.3 `board/live` (table) and `board/rows`, §4.6 bullets 2-3
- Finding: §4.6 renders "one `<tbody>` per group, headed by the group's name and its count". §4.3 returns and appends to "the table body", singular. The design does not say:
  - where `board/rows?offset=100` appends rows that continue the last group or start a new one;
  - whether group-header counts are totals or rows shown;
  - whether "Showing 100 of N" counts rows or groups.
- Recommendation: Specify the swap targets: an id per group `tbody`, and the rule for "append to the existing group or create a new `tbody`". Specify the group count semantics. Extend `TestTheTableSortsGroupsAndCaps` to page across a group boundary.

### Hiding empty columns conflicts with per-column OOB replacement - Severity: Major
- Affected scope: §4.2 `empty=hide`, §5.1
- Finding: With `empty=hide`, a column that gains its first matching card has no element in the DOM for an OOB swap to replace. htmx drops the swap and logs `oobErrorNoTarget`, so the card never appears. The reverse case, a column becoming empty, is also unspecified.
- Recommendation: Always render all nine column elements and hide empty ones with a class or `hidden` inside the column fragment, so the OOB target always exists. Add this case to `TestBoardLiveSendsOnlyTheColumnsThatChanged`.

### Who marks a card "fresh" is undecided - Severity: Major
- Affected scope: §5.4
- Finding: "A card that is new in a column after a live response gets `is-fresh`" can be built on the server or in the client. The server is stateless per request and cannot know what the browser held before. Yet "sisflow gets the same class through the card's `css` field" suggests the server JSON sets it. Two engineers would build different things, and one of them cannot work.
- Recommendation: State that the client decides. `live.js` diffs `[data-card]` ids per replaced column before and after the swap. `board.js` diffs ids across `reload()` and sets `css` before `store.load`. The server never emits `is-fresh`.

### Phase 2's exit condition contradicts §5.6, and its measures are unnamed - Severity: Major
- Affected scope: §5.6 (last line), §10 phase 2 *Exit*, §10.3 second command
- Finding:
  - The exit requires "the benchmark meets §5.6". §5.6 says a missed target is "reported in §11 … not adjusted", which suggests the phase can still exit.
  - The benchmark functions in `board_bench_test.go` are not named, and the "one live refresh" target has no stated benchmark.
  - The DOM element count and detached-node targets rely on "a script in the pull request description" that is not written.
  - Phase 2 also has an internal checkpoint ("server half lands before its templates") with no exit condition of its own.
- Recommendation: Decide whether a missed target blocks the exit. Name each `Benchmark…` and the target each one checks. Put the browser measurement script in the repository, or state its steps exactly. Give the server-half checkpoint its own exit (for example `go test -run 'BoardPage|BoardQuery'` passes).

### The new `rebuild-restart.sh` check has no register row - Severity: Major
- Affected scope: §9 phase 1 `deploy/rebuild-restart.sh` row, §10 phase 1 counts
- Finding: The QA register gives each `rebuild-restart.sh` check its own TC row (TC-017…TC-047 and later rows), and a change to that script triggers a register update. Phase 1 adds a check (`/app/inbox` and `/app/p/x/board` answer 200), but the eight new IDs (TC-722…729) are all Go tests. The register total "721 → 729" therefore leaves this check without a row.
- Recommendation: Either allocate TC-730 for the deploy check and shift phase 2 to TC-731…743 (updating every count), or say which existing `rebuild-restart.sh` row it extends and how that row's scenario changes.

### `TestADeepURLRendersThePage` asserts the impossible for `/p/{ref}` - Severity: Nit
- Affected scope: §10 phase 1 table, §3.1
- Finding: "Each of the eleven patterns returns a full document" cannot hold for `/p/{ref}`, which answers 302 (§3.1). Separately, `TestEveryRouteIsClassified` (`auth_test.go:316-319`) requires every non-public GET to be in `readOnlyGET`. Phase 1 does not say that the eleven page patterns are added there; §4.3 says so only for phase 2's routes.
- Recommendation: Say "the ten page patterns, and the redirect pattern answers 302". State that the eleven patterns go into `readOnlyGET` in phase 1.

### The "Needs you" filter needs send-back state for tasks that will be cut - Severity: Nit
- Affected scope: §4.2 `needs`, §4.1 step 1
- Finding: "BLOCKED with an open design send-back" needs `gateDesignDepLocked` and `sendBacksLocked` (`sendback.go:387-389`). This is decorator logic, but filtering happens in step 1, before the cut, and §4.1 says only kept cards are decorated.
- Recommendation: State that step 1 evaluates the send-back predicate (and only that) for blocked design-review tasks, using the id map.

### JSON route behaviour with a filter and no `limit`, and the dropped `Workflows`, are unstated - Severity: Nit
- Affected scope: §4.3 JSON row, §4.1 `BoardPage`
- Finding: The design defines the response with `limit`, and without `limit` and without a filter. It does not define the response with a filter and no `limit`. `BoardPage` also has no field for `Board.Workflows` (`board.go:107`), so the paged JSON's `workflows` key is undecided.
- Recommendation: State both behaviours.

### Href examples and the "page form" of prHref/afHref are loose - Severity: Nit
- Affected scope: §3.4 bullet 1, §9 phase 1 `prview.go` and `autofix_view.go`
- Finding: The example `prs?state=open&open=fix%3A12` cannot come from `prHref`, which leaves out `state=open` as the default (`prview.go:130`). "The page form of `prHref` and `afHref`" is not defined: it could be a new function, a new parameter, or row `href`s that change.
- Recommendation: Correct the example and give the signature of the page form.

### Smaller undefined shapes - Severity: Nit
- Affected scope: §4.1 `BoardFacets`, §3.3 `pageTitle`, §4.4 vendored CSS line
- Finding:
  - `BoardFacets` is used but never defined.
  - `pageTitle` has one example only. The strings for global views and for tabs such as "AI vendors" are open.
  - The vendored `width: 280px` is at `sisflow-board.css:90`, not `:89`.
- Recommendation: Give the `BoardFacets` struct, a title table, and correct the line.

---

**Isolation note (not a finding):** The checkout lives under `.claude/worktrees/` in the parent repository. Reading the design and code there made the harness attach three rule files from the parent's `.claude/rules/`: `agent-workspace.md`, `qa-test-plan.md` and `frontend.md`. No `CLAUDE.md`, `AGENTS.md` or `GEMINI.md` was attached or opened. I used the attached `qa-test-plan.md` only to confirm the register conventions. Its stale "TC-720" was ignored in favour of the checkout's register, which shows TC-721.

Counts: B=0 M=12 N=5
