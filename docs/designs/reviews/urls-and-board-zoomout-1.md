<!-- Round 1, 2026-11-26. Lens: zoomout. Target: docs/designs/urls-and-board.md (first draft). Verbatim reviewer output. -->

# Zoom-out review: `docs/designs/urls-and-board.md`

Lens: Skeptical Architect (gaos-goldfish-zoom-out). I read the design, `docs/rules/gaos-design-review.md`, `docs/designs/README.md`, the code it cites, the QA register and the decision log. I opened nothing under the checkout's `.claude/`. The harness attached three path-scoped rule files from the outer checkout by itself (`agent-workspace.md`, `qa-test-plan.md`, `frontend.md`). No `CLAUDE.md`, `AGENTS.md` or `GEMINI.md` content reached me.

**Claims I confirmed.** All of these say what the design says they say:
- `board.go:242`, `conversation.go:208` and `:621`, `designgate.go:545`, `sendback.go:375`, `timing.go:404`.
- `handlers_bo.go:129-176`, `:188`, `:331`, `:338-344`.
- `static/live.js:57`, `:63`, `:97`, `static/app.js:409`.
- `prview.go:125`, `templates/page.html:12` and `:25`, `store_arcade_blueprint.go:34`, `main.go:537`.
- In the vendored sisflow: `index.js:121`, `rest.js:32`, `virtual-list.js:6`.
- There is no `HX-Push-Url`, `HX-Current-URL` or `pushState` anywhere outside vendored htmx.

**Arithmetic.**
- Phase 1 lists 8 tests (TC-722 to TC-729), so Go tests go from 587 to 595.
- Phase 2 lists 13 tests (TC-730 to TC-742), so 595 to 608.
- The register's highest id today is TC-721 (`qa-test-plan.html:812`), and the decision log says "Ninety-one entries" (`decision-log.html:18`).
- "Eleven patterns" = nine view routes + `/p/{ref}` + `/p/{ref}/{section}`.
- "≤ 270 cards in the first render" = 9 columns × 30.

All of these add up. One target does not: the live-refresh response target of 60 KB fails (see the first Major finding below).

**Sections that hold up.**
- §3.1 and §3.3: page routes and fragment routes are kept apart, and the rejected "one URL for both shapes" option was stated fairly.
- §3.4: a server-side push header from one function is smaller than about thirty `hx-push-url` attributes.
- §3.5: the htmx config change is minimal and was checked in a spike.
- §3.7: the gate answers signed-out page requests itself, with a constant `HX-Refresh` value.
- §5.5: skipping the event copy and the diagram rendering on the Board tab fixes the root cause.

---

### The 60 KB live-refresh target cannot be met with the hash as designed - Severity: Major
- Affected scope: §5.1 (last paragraph), §5.6 (row "One live refresh, 2,000 cards"), §10 Phase 2 `TestBoardLiveSendsOnlyTheColumnsThatChanged`
- Simplicity rationale:
  - Each column's `data-rev` is a hash of its rendered HTML. That HTML includes each open card's lead-time text inside `<time data-elapsed-since>` (`templates/backoffice.html:416`). The client already updates that text itself every 30 s (`static/live.js:183-186`).
  - The lead time is shown for every card that is not finished. So after a minute, the hash changes in every column that holds an open card, which is up to seven of nine columns. §5.1 says it costs "an extra column".
  - From §1.2, a response is about 0.95 KB per card (191 KB / 200 cards). Seven columns × 30 cards × 0.95 KB ≈ 200 KB, against a target of ≤ 60 KB. Only a move inside the same minute stays near 60 KB (2 × 30 × 0.95 ≈ 57 KB).
  - The same cause makes the "after one move: exactly two" test flaky whenever a minute boundary falls between the two renders.
  - The hash mechanism exists to save bytes, but because of this it does not save them.
- Recommendation: Hash the card data rather than the HTML, and leave out the ticking lead-time text, which the client re-ticks anyway. Alternatively, drop `rev` altogether, always send all nine columns (≤ 270 cards, replaced per column, which still keeps each column's scroll), and change the 60 KB target to the number that produces. Either way, the arithmetic behind the target has to be written in §5.6.

### "Show more" and "Show 100 more" add two routes that `board/live` already covers - Severity: Major
- Affected scope: §4.3 (the `board/col/{col}` and `board/rows` rows), §7 (the stale-offset row), §8 (the cursor-paging row), §9 phase 2 (`main.go`, "four routes")
- Simplicity rationale:
  - `board/live` already takes `shown` per column and returns only the columns whose hash changed (§5.1). A "Show more" on Done is the same request with `shown=done:60`: one column changes, and `live.js` already replaces it and restores its `scrollTop`.
  - The separate `board/col` route adds work the design then has to handle:
    - a second response shape (`beforeend:#col-<col>-cards` plus a footer);
    - a `data-rev` that is stale after the append, so the next refresh resends that column anyway;
    - the stale-offset duplicate/skip failure mode that §7 and §8 then have to justify.
  - The table has the same pattern. A `limit` on `board/live` in table view replaces `board/rows`.
  - Offsets are still needed for sisflow's `…/board/cards`, because its provider speaks offsets. The server board does not need them.
- Recommendation: Keep two new routes: `board/live` (HTML, the server board and the table) and `board/cards` (JSON, sisflow). Express "more" as a larger depth on `board/live`. That removes `board/col`, `board/rows`, `TestShowMoreAppendsTheNextCards`' OOB `beforeend` contract, and the stale-offset row in §7 for the server board.

### sisflow paging re-implements what the vendored provider and store already do - Severity: Major
- Affected scope: §4.8 (the "Show more" row), §4.3 (the `board/cards` row)
- Simplicity rationale:
  - The design has `board.js` fetch `…/board/cards`, merge the cards into its own copy of the document, and reload the whole document into the store.
  - sisflow already does this. On `board:load-more` it calls `provider.loadPage(req)`, then `store.ingest({cards}, req)`, then emits `board:page-loaded` with `exhausted` (`static/vendor/sisflow/board/index.js:162-167`).
  - The stock REST provider's `loadPage` requests `{base}/cards?column&row&offset&limit` (`static/vendor/sisflow/board/provider/rest.js:32-33`). `board.js` sets `base` to `products/{id}/board` (`static/board.js:189`), so that URL is exactly the new route of §4.3.
  - Today `board.js` overrides `loadPage` to say "nothing more" (`static/board.js:211-213`). Deleting that override is most of the feature.
  - Replacing the whole document from inside a load-more also skips the `page-loaded` event the virtual list waits on to clear its loading state (`virtual-list.js:25`). The column could stay in "loading".
- Recommendation: In §4.8, say that `board.js` replaces its `loadPage` override with one that calls `rest.loadPage` and appends the current filter query, and that sisflow merges the pages through `store.ingest`. There is no hand-written merge and no full reload on "more".

### The stored, back-filled slug guards against product deletion, which does not exist - Severity: Major
- Affected scope: §3.2 ("Existing products", "Stored"), §7 (rows "slug back-fill cannot write" and "slug belongs to a deleted product"), §8 (row "Derive the slug … on every request"), §9 phase 1 (`store_arcade.go`)
- Simplicity rationale:
  - §8 rejects deriving the slug because "two products with one name would swap slugs when the older one is deleted". There is no product deletion: the only product mutation is `CreateProduct` (`store_mem.go:1815`), and a search finds no `DeleteProduct` or similar.
  - §2 already rules out renames.
  - With names fixed and no deletion, `newSlug` applied in ascending id order is deterministic at every boot. That makes the persisted property unnecessary.
  - Persisting it costs:
    - a write to ArcadeDB in a transaction at every boot;
    - a new way for the boot to fail (§7);
    - a change to `writeProduct` and to the load query;
    - a store-conformance case.
  - The §8 alternative is a strawman: it argues against a deletion feature that is not in the code.
- Recommendation: Compute `Slug` in memory: in `CreateProduct`, and in one `assignSlugs(products)` pass after load in both stores. Never write it. Record in §8 that a stored slug becomes necessary when product deletion or rename is designed, and leave it to that design. This removes the boot-write failure row and the "deleted product" row from §7, and the reopen case from §10.

### The grouped and sorted table with paging is under-specified and speculative - Severity: Major
- Affected scope: §4.2 (`sort`, `dir`, `group`), §4.3 (`board/rows`), §4.6
- Simplicity rationale:
  - The table has 5 sort keys × 2 directions × 4 groupings, plus 100-row paging.
  - With `group≠none` there is "one `<tbody>` per group", but `board/rows` appends "to the table body". Rows 101-200 can continue an existing group's `<tbody>` or start new groups, and the group-header counts must then change. Two engineers would implement this differently.
  - The defaults also repeat each other: `group=status` together with `sort=status` puts the same information on two axes.
  - Goal 4 asks only for "switched to a table". Grouping is not motivated anywhere in §1.
- Recommendation: Ship the table with sort and dir only (no `group`). Status is already a sortable column, and story and workflow are columns and filters. If grouping stays, state that paging refreshes the whole table body through `board/live` with a larger limit (see the second Major finding), so group headers are always recomputed by the server.

### Two independent features in one design document - Severity: Nit
- Affected scope: title, §3 versus §4-§5, §9, §11 ("The phase is large")
- Simplicity rationale: Phase 1 (URLs, D-093, v0.55.0, `feat/urls`) and phase 2 (Board, D-094, v0.56.0, `feat/board`) have separate decisions, branches, test ranges and exits. Phase 1 is "useful alone" (§11). Its only link to phase 2 is passing the query string through (§3.8). One document means phase 1's approval waits on phase 2's review rounds, which are the larger and riskier part.
- Recommendation: Split into `urls.md` and `board.md`, with the board document citing §3.4's push and replace rule. Or state explicitly that phase 1 may be approved on its own.

### The quadratic-decorator fix is a standalone fix being threaded through a new API - Severity: Nit
- Affected scope: §1.2 (last bullet), §4.1 step 4, §9 phase 2 (`board.go`, `timing.go`, `conversation.go`, `sendback.go`, `qacatalog_store.go`)
- Simplicity rationale:
  - Only two decorators look up tasks one at a time: `cardLocked` in `conversation.go:631` and `taskByIDLocked` in `sendback.go:378`.
  - `decorateBoardQa` and `decorateBoardTiming` never look up by id, so passing "the id map" to all four widens four signatures for the benefit of two.
  - A local `id→*TaskRecord` map built at the top of each of the two loops fixes the unlimited JSON today. It is a bug fix under the rubric's own trigger list.
- Recommendation: Land the two local maps as a fix before phase 2 and measure §5.6's baseline after it, so the targets reflect what `BoardPage` itself buys. Change only the two decorators' signatures, if any.

### `mountBoard` "skip a host that is already mounted" already exists - Severity: Nit
- Affected scope: §5.3 (last bullet)
- Simplicity rationale: `board.js` `mount` already returns early for a host it has mounted (`static/board.js:180`, a `WeakMap`), and the browser caches a repeated dynamic `import()` of the same URL in its module map.
- Recommendation: Delete the bullet, or say what it actually saves if a measurement shows a cost.

### The "fresh" marker is polish no goal asks for - Severity: Nit
- Affected scope: §5.4, §4.4 ("No new colour except `--fresh`")
- Simplicity rationale:
  - None of Goals 1-6 asks for a change indicator.
  - It adds a new theme variable in both themes, a motion spec, and a reduced-motion branch.
  - On sisflow it needs `board.js` to diff card ids between documents. §5.4 does not say who computes it.
- Recommendation: Defer it, or state that `live.js` and `board.js` compute "new in column" by comparing card ids before and after, and on which board.

Counts: B=0 M=5 N=4
