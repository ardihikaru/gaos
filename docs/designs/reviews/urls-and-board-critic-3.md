<!-- Round 3, 2026-11-26. Lens: critic. Target: docs/designs/urls-and-board.md (rev 3). Verbatim reviewer output. -->

# Goldfish critic report: `urls-and-board.md` (rev 3)

**Isolation note:** The design file is inside the parent repository's `.claude/worktrees/`. When I read it, the harness attached `.claude/rules/agent-workspace.md`, `qa-test-plan.md` and `frontend.md` from the parent checkout. No `CLAUDE.md` was attached or opened. I did not use those rule files as the rubric.

**Verified correct, with no finding:**
- **`buildBO` call sites.** All of them match the §3.4 table. That includes `handleReadiness` at `handlers_bo.go:508`, `handleIndex` at `handlers_tasks.go:20`, `handlers_conversation.go:121/123/156`, `handlers_cw.go:87` and `handlers_questions.go:152`.
- **The gate.** `publicPatterns` and the signed-out JSON branch are as described (`auth.go:502-510`, `:551-560`). `sitePrefix` validates as claimed (`render.go:192-209`).
- **Slugs.** `slug()` produces ASCII only (`store_mem.go:2462-2478`), so the `^[a-z0-9]+(-[a-z0-9]+)*$` check cannot reject its output.
- **Store and fold.** `writeProduct` and the load query are where the design says (`store_arcade.go:763`, `:1494-1496`). `load` uses `newProjection`, not `NewMemStore` (`:1508`), so the back-fill path is correct. `mutate`/`persist` behave as §7 says (`:533-545`). The fold no-op list matches (`fold.go:442`).
- **Other code references.** The eleven `<a … hx-get>` elements exist and there is no `href="#`. No new route collides with an existing one (`main.go:499-583`). The `accessLog: {}` lines are at both cited places.
- **Counts.** The test counts add up: phase 1 has 10 Go tests plus 1 deploy check, which is TC-722 to TC-732. Phase 2 has 14 tests, TC-733 to TC-746.

---

### The search text leaks into the request URL through htmx's own form serialization - Severity: Major
- Affected scope: §4.5 (the form's search box is `name q`, and hook step 3), §4.2 `X-Board-Search`, §6 "The search text is the one thing kept out", §10.2 `TestTheSearchTextNeverTravelsInAURL`
- Finding and rationale: The form is `hx-get`. On a GET, htmx 2.0.4 sends the form's named inputs as `evt.detail.parameters` and appends them to the path *after* `htmx:configRequest`. Step 3 says the hook "removes `q` from the URL". But on the form's own request, `q` is in the parameters, not yet in the URL, so a hook that only edits `evt.detail.path` leaves the search text in the request line. That request line is what `accessLog: {}` records (`deploy/traefik/traefik.yml:39`). The planned test reads `live.js`/`board.js` source to check where `q` is written. It cannot see htmx serializing a named input implicitly. So the single privacy promise of §6 rests on a test that cannot fail for the actual leak.
- Recommendation: Give the search input no `name`, or put `hx-params="not q"` on the form, so htmx never serializes it. Make the hook read the box by id. Replace the source-reading assertion with an end-to-end one: a rendered `#boardFilters` has no form control named `q`. The §10.2 exit already checks the Network panel; keep that.

### The hook re-adds `view` and the filters to the links that are meant to remove them - Severity: Major
- Affected scope: §4.5 hook step 1, the "Board"/"Table" `nav.af-seg`, `a.af-reset` "Clear filters"
- Finding and rationale: Step 1 adds "each field of `#boardFilters` that the request does not itself carry as a query parameter" to every `products/<id>…` request. The **Board** link is `products/<id>?tab=board` without `view`, so the hook adds the hidden `view=table` and the switch back to the board never happens. "Clear filters", whether it is a link or an `hx-get`, gets `role`, `story`, `label`, `wf` and the rest re-added from the still-populated form, so it clears nothing. The design never says how either link opts out.
- Recommendation: Define an opt-out, for example `data-board-keep="sort dir"` or an explicit empty `view=` that counts as "carried". Specify it for the Board link, "Clear filters" and the sort headers. Add both cases to `TestABoardMutationKeepsTheView` or to a new test.

### `board/live`'s address rule contradicts the §3.4 table it cites - Severity: Major
- Affected scope: §4.3 `board/live` row; §3.4 push/replace table; §10.2 `TestBoardLiveNeverPushesTheAddress`
- Finding and rationale: §4.3 says the address follows "the rule of §3.4's table" and also "never `HX-Push-Url`". By §3.4's first row, any request whose path differs from `HX-Current-URL` gets `HX-Push-Url`. That happens when a `board/live` response lands after the operator has gone to another tab. The test then expects no header in that case. So `pushURL` needs a mode the design never defines.
- Recommendation: Give `pushURL` a `replaceOnly` argument, or have `board/live` call a separate function. State the rule: send `HX-Replace-Url` only when the path is the same and the query differs; otherwise send nothing.

### A hint during a filter change aborts the operator's request and the address never follows the filter - Severity: Major
- Affected scope: §4.5 "One request at a time" (`hx-sync="this:replace"`, and `X-GAOS-Refresh` set "for that one request"); Goal 6; §3.4 (`X-GAOS-Refresh` suppresses every header)
- Finding and rationale: The operator picks a role, and the form's GET is in flight. A task hint then triggers the form's `refresh`. `replace` aborts the operator's request and sends the refresh with the same form values, marked `X-GAOS-Refresh`, so `pushURL` sends nothing. The screen is filtered but the address is not. A reload or a copied link drops the filter. Hint bursts come from active runners, which is exactly when operators filter.
- Recommendation: Either let `board/live` send `HX-Replace-Url` even for a refresh when `HX-Current-URL`'s path is this Board, which is safe because the path check already covers "navigated away"; or use `hx-sync` `queue last` for refreshes. Add the race to `TestBoardLiveNeverPushesTheAddress`.

### The search box is never said to be re-rendered, so a filter can be active with no visible control - Severity: Major
- Affected scope: §4.1 `BoardWindow.Text`, §4.1 step 2 ("a filter can never be active without a control that shows it"), §4.5 (whole-sheet mutations "re-render the same filter"), §5.3 ("the filter bar is not replaced by a refresh")
- Finding and rationale: The view links, the sort links and every mutation other than a move replace `#boSheet`, filter bar included. The server applies `X-Board-Search` to the cards. Nothing says the template prints `BoardWindow.Text` back into the box. If it does not, the board stays narrowed by text the operator can no longer see. If it does, the design should say so, because two engineers will choose differently.
- Recommendation: State that the bar's template prints `BoardWindow.Text` as the box's value. Add that assertion to `TestABoardMutationKeepsTheView`.

### sisflow loses its depth and scroll on every whole-sheet response, which contradicts the phase-2 exit - Severity: Major
- Affected scope: §4.8 "Loading" (depth map, 30 at mount), §5.1 last bullet (scroll restore covers `.board-ssr` and `.board-col` only), §10.2 exit ("set a label and send a design back … keep the filter and both scroll positions", run "once with sisflow mounted")
- Finding and rationale: A send-back, a label, an answer or a defect close replaces `#boSheet`, which creates a new `#board-host`. `mountBoard` runs on every `afterSwap`/`oobAfterSwap` (`static/app.js:441-442`), and `mount` keys on the host element (`static/board.js:180`). So sisflow mounts again at depth 30, with its `.sfb-body` scroll at zero. The §5.1 restore does not cover `.sfb-body`. The exit, as written, fails on sisflow.
- Recommendation: Keep the depth map and the `.sfb-body` scroll in module state keyed by product id, not by host element. Restore them on remount. Or say in the design that sisflow resets after such mutations and change the exit to match.

### The paged sisflow board draws every card in one lane labelled "Plan" - Severity: Major
- Affected scope: §4.7 "Story lanes in sisflow", §4.3 paged JSON (`rows: []`, no `row`)
- Finding and rationale: `board.js` sets the catalog key `"lane.none": "Plan"`, and its comment says sisflow "always draws a lane for cards with no row" (`static/board.js:192-195`, `:222`). If no card has a row, every card, story work included, sits under a lane header that says "Plan". The design removes the lanes but leaves the label that now misdescribes the board.
- Recommendation: When `paged=1`, set `lane.none` to an empty or neutral label, or hide the lane header with CSS. Put this in §4.7 and in the §10.2 exit.

### "The task's last event" is not what the timing index holds - Severity: Major
- Affected scope: §4.1 step 4 (Done/Canceled newest first), §4.1 `Updated` ("the `Seq` of the task's last event"), §4.6 `sort=updated`, §10.2 `TestDoneIsNewestFirst`
- Finding and rationale: `timingIdx` indexes only `TaskPlanned`, `TaskMoved`, `QuestionAsked`, `QuestionAnswered` and `ConversationArchived`/`Pruned` (`timing.go:436-453`). `TaskLabeled`, `TaskDispatched` and `TaskCanceled` are not indexed. The index also holds *positions in `s.events`*, not `Seq`. It is extended lazily, so reading it without `extendIndexLocked` (`timing.go:432`) returns stale data. One engineer will read the index tail; another will scan the stream for the true last event. Their orders will differ, and so will their costs.
- Recommendation: Define `Updated` as "`s.events[i].Seq` for the last position in `timingIdx[id]`, after `extendIndexLocked`". Rename it, for example to "last state change", and say a label change does not move it. Or widen the index explicitly, and add the extra cost to §5.6.

### The paged JSON contract has no test, and the phase-2 checkpoint selects tests that do not exist - Severity: Major
- Affected scope: §4.3 JSON row (`paged=1`: `counts`, `matched`, `all`, `rows: []`, no `row`, `workflows: []`; filters ignored without `paged`), §10.2 checkpoint `-run 'BoardPage|BoardQuery|BoardJSON|BoardCard'`
- Finding and rationale: None of the 14 new tests asserts the `paged=1` document, or that the unpaged document ignores filter parameters. sisflow depends entirely on that document. The checkpoint pattern names `BoardJSON`, which matches no listed test. It also skips `TestBoardFiltersNarrowCardsAndCounts` and `TestDoneIsNewestFirst`, which are server-half tests. So "the server half passes" can be declared while filtering is untested.
- Recommendation: Add `TestTheBoardJSONPagesOnlyWhenAsked`. That is a 15th phase-2 test, so the register and the Go-test counts move by one. Make the checkpoint regex match every server-half test by name.

### `AssignSlug` versus `AssignSlugs` - Severity: Nit
- Affected scope: §3.2 vs §9 phase 1 (`store_mem.go` row)
- Finding and rationale: §3.2 defines `MemStore.AssignSlug(ctx, productID)` and §9 lists `MemStore.AssignSlugs`.
- Recommendation: Use one name in both places.

### "A product with a lower id" does not say how ids are ordered - Severity: Nit
- Affected scope: §6 "A stored slug is validated", vs §3.2 "ascending numeric id order"
- Finding and rationale: Compared as strings, `p-10` sorts before `p-9`. Which product keeps a duplicate slug then depends on the comparison.
- Recommendation: Say "lower numeric id" in §6.

### The Timeline count is not bounded - Severity: Nit
- Affected scope: Goal 5 ("small, bounded amount of work"), §5.5 `EventCount`
- Finding and rationale: `EventCount` walks all of `s.events` (`store_mem.go:748-757` shows the store keeps one flat stream). Each `board/live` therefore costs time linear in the tenant's whole history. It is cheap, but it is not bounded.
- Recommendation: Either keep a per-product counter in `emit`, or soften the goal's wording and add a long event history to `BenchmarkBoardLive`.

### Story facets are described two ways - Severity: Nit
- Affected scope: §4.1 `BoardFacets` comment ("values present on ANY task") vs `Stories` ("the product's stories"), and §4.2 `story` ("a story id of the product")
- Finding and rationale: A story that has no task is offered under one reading and not under the other.
- Recommendation: Choose one reading and use it in both places.

### Unknown `{ref}` on `/p/{ref}` is not specified - Severity: Nit
- Affected scope: §3.1 table, `/p/{ref}` row
- Finding and rationale: Only the known-ref `302` is given.
- Recommendation: State that an unknown ref gets the same `404` page as an unknown product.

### `TestADeepURLRendersThePage` checks attributes that global views do not render - Severity: Nit
- Affected scope: §10.1
- Finding and rationale: `data-product` and `data-tab` exist only with `.BO.Detail` (`templates/backoffice.html:122`).
- Recommendation: Say these are asserted for product sections only.

### `sort=status` versus the empty default - Severity: Nit
- Affected scope: §4.1 `Sort` ("" = the default), §4.6 (`status` is "the default"), §10.2 round-trip test
- Finding and rationale: The design does not say whether `sort=status` parses to `""`. If it parses to `"status"`, the `==` round trip passes but two addresses mean the same view.
- Recommendation: Normalise the default to `""` when parsing, and say so.

### "Label" means two things - Severity: Nit
- Affected scope: §4.2 `label` filter; `BoardCard.Label` is the card title (`board.go:42`)
- Finding and rationale: The search covers "title", which is the `Label` field, while the `label` filter means `Labels`.
- Recommendation: Add one line to the Terms block.

### A move's answer can be overwritten by an older refresh - Severity: Nit
- Affected scope: §4.5 / §5.1 (the move's `PUT` is not under `hx-sync="#boardFilters"`)
- Finding and rationale: A refresh computed before the move commits can arrive after the move's answer and briefly show the old columns. The move's own hint then corrects it.
- Recommendation: Say in §7 that this is accepted.

Counts: B=0 M=9 N=8
