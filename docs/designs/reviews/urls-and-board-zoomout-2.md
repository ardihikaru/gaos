<!-- Round 2, 2026-11-26. Lens: zoomout. Target: docs/designs/urls-and-board.md (rev 2). Verbatim reviewer output. -->

# Zoom-out review: Real URLs for the back office, and a Board that pages, filters, switches to a table and refreshes in place

Reviewed: `/home/ardi/devel/projects/ardihikaru/my-projects/gaos/.claude/worktrees/feat-urls/docs/designs/urls-and-board.md` (rev 2). I checked the claims against the `feat-urls` checkout.

**Arithmetic.**
- **`buildBO(` call sites:** the code has 14. Twelve go through `sheet`; two keep `buildBO` (`handlers_tasks.go:20` and `handlers_bo.go:508`). The two new page handlers make 16, which matches §3.4.
- **Routes:** 9 view routes + `/p/{ref}/{section}` + `/p/{ref}` = 11, which matches §3.1.
- **Phase 1 tests:** 10 tests, TC-722 to TC-731, plus one deploy check (TC-732). The register goes 721 → 732, and Go tests 587 → 597. Correct.
- **Phase 2 tests:** 13 tests, TC-733 to TC-745. Go tests 597 → 610. Correct.
- **§5.6 size targets:** 270 cards × 0.9 KB ≈ 243 KB, under the 300 KB cap. A live refresh is 2 × 30 × 0.9 = 54 KB, under 60 KB. Correct.
- **One thing I could not check:** the 721/587 baseline. It assumes #45 is merged. The rule file currently in the tree still says TC-720 and 586.

**Reuse checks that held up.**
- The existing `slug()` is reused (`store_mem.go:2462`).
- `prPageHref`/`afPageHref` are built on `prHref`/`afHref` (`prview.go:125`, `autofix_view.go:672`). That is the right reuse.
- The filter bar reuses the Pull requests page's parts.
- §5.5 (`EventCount`, and rendering diagrams only on their own tab) targets the real root cause at `handlers_bo.go:331-344` and is minimal.
- The `sheet` funnel for push/replace (§3.4) is a sound single entry point.
- Giving both decorators the id map fixes the quadratic term at its source.

---

### The most complex live-diff work is spent on the fallback board - Severity: Major
- Affected scope: §5.1, the `rev`/`data-rev` parts of §4.2 and §4.3, §5.4 (server-board half), §5.6 (`BenchmarkBoardLive`, the detached-nodes measure), §10.2 (`TestBoardLiveSendsOnlyTheColumnsThatChanged`, `TestAMoveAnswersWithTheBoardNotTheSheet`)
- Simplicity rationale:
  - §1.3 says sisflow is what operators see whenever the module loads. `static/board.js:248-249` shows the sisflow host and hides `.board-ssr`. So the server board is the fallback, and the table is server-rendered separately.
  - The heaviest new parts all serve that fallback:
    - FNV-1a column hashes, with running-clock text blanked so the hash stays stable;
    - the `rev` request parameter and its validation;
    - sending only the columns whose hash changed;
    - a new board-shaped answer from `handleMoveCard`;
    - client-side `is-fresh` bookkeeping;
    - a benchmark and a heap-snapshot exit measure.
  - The 30-per-column cap already bounds the cost: about 270 cards, about 243 KB.
  - §8 rejects "send the whole board region" because it "loses each column's scroll position". But §5.1 already saves and restores `scrollTop` for every column it replaces, and the same code covers nine columns. So that alternative was not stated fairly: always sending all nine column elements by OOB, without the diff, keeps every scroll position and was never considered.
- Recommendation:
  - On a hint, have `board/live` return all nine column elements (OOB, at the `shown` depths) plus the count line. Keep `.board-ssr` and per-column scroll restore.
  - Drop `rev`, the hash, `data-rev` and the "only changed columns" test.
  - If the fallback ever becomes the main surface, add the diff then, with a benchmark that justifies it.

### Sisflow's hot path still pays for the whole board - Severity: Major
- Affected scope: §1.2 (root problem), §4.1 last paragraph ("`Store.Board` keeps its signature and its result"), §5.6
- Simplicity rationale:
  - Sisflow is the main surface, and every drag on it is a JSON `PUT`. That path still calls the unpaged `s.store.Board(...)` just to return one card (`dashboard/handlers_board.go:82`).
  - Two other handlers do the same:
    - the send-back answer (`dashboard/handlers_board.go:152`);
    - the timing sheet, which looks up a title (`dashboard/handlers_bo.go:911`).
  - After this design, those three calls still build and decorate every card, including timing for every task. That is about 25 ms / 36 MB at 2,000 cards, and 361 ms at 10,000 (§1.2 table). The id-map fix only removes the quadratic term.
  - None of §5.6's benchmarks measures this path.
  - The design pages the read but leaves the most frequent write answer on the old cost, so the root problem is only half fixed.
- Recommendation:
  - Answer these three call sites with a single-card read. Either use `BoardPage` restricted to one task id, or use a one-card lookup next to the existing `TaskInfo` (`dashboard/store_mem.go:2498`).
  - Add the move-answer path to `BenchmarkBoardPage` or to its own benchmark.
  - If this is out of scope, write it into §12 as an accepted limit.

### Phase 1 already ships phase 2's URL contract - Severity: Major
- Affected scope: §2 ("Phase 1 … depends on nothing in phase 2"), §3.4 Href table row "a product's `board` section", §3.8, §9 `urls.go` row, §9 phase-2 `board_page.go` row ("`parseBoardQuery` moves here")
- Simplicity rationale:
  - Phase 1 adds `BoardQuery`, `Encode` and a syntactic `parseBoardQuery` that nothing acts on. That freezes phase 2's parameter names (`view`, `role`, `wf`, `needs`, `empty`, `sort`, `dir`) before phase 2 is approved, even though the two phases are meant to be approved separately.
  - The round-trip test `TestTheBoardQueryRoundTrips` is in phase 2. So phase 1 ships `Encode` with nothing testing it, and phase 2 then moves the parser to another file.
  - If phase 2 is rejected or reshaped, phase 1 leaves behind dead code and a published URL vocabulary.
- Recommendation:
  - In phase 1, the board section keeps no parameters ("none", like every other section).
  - Phase 2 adds the `BoardQuery` row to the Href table, the parser and its round-trip test, all in `board_page.go`. Nothing has to move.

### An id-bearing slug was never considered - Severity: Major
- Affected scope: §3.2, §8 rows "Use the product id in the URL" and "Derive the slug from the name at boot"
- Simplicity rationale:
  - Storing the slug requires all of the following:
    - a new `Product` field and a vertex property;
    - `newSlug` with collision suffixes and an "id-shaped" exclusion;
    - `backfillSlugs` in two store constructors;
    - boot-time database writes that emit `RecordUpdated` events on first deploy;
    - a write-failure retry rule;
    - a conformance case.
  - §8 offers only two other options: a pure id, or a pure name-derived slug that can move. A third option is a URL that carries the id with the name attached, such as `/p/6-route-planner/board` or `/p/route-planner-p6/board`, resolved by the id and redirected when the name part is wrong. It needs:
    - no new stored field, no back-fill and no boot writes;
    - no collision logic: it is unique by construction, and it cannot move when a namesake is deleted.
  - It still meets the operator's goal that "the URL should say which product it is". Leaving it out makes the stored-slug choice look forced when it is a choice.
- Recommendation:
  - Add this option to §8 with a fair reason for rejecting it, or adopt it.
  - If the operator still wants a bare name, keep §3.2. The record should then show the cost was weighed against this cheaper option.

### Two request-only parameters change the response mode - Severity: Nit
- Affected scope: §4.2 request-only table (`more`, `surface`), §4.8 Filters row
- Simplicity rationale:
  - `more=<column>` repeats information `shown` already carries: the client knows the current depth and can send `shown=done:60` itself.
  - `surface=sisflow`, set through a hidden form input, quietly switches `board/live` into a "count line only" mode. It is a mode flag smuggled through a filter form.
- Recommendation:
  - Drop `more` and let the "Show more" link carry the deeper `shown` value.
  - For sisflow, either render the count line from the paged JSON, which already has `counts`, `matched` and `total`, or point the form at a count-line endpoint while sisflow is mounted.

### Too much implementation detail in places - Severity: Nit
- Affected scope: §5.3 (250 ms / 1 s debounce), §5.4 (3px bar, 150 ms fade, 1.5 s delay, `::after`), §4.1 steps 1–6, §4.5 (300 ms input pause)
- Simplicity rationale: These are tuning values and internal loop steps, not architecture. They make the design longer and invite review of numbers that the frontend pass (`review-animations`) will decide anyway.
- Recommendation: Keep the contract ("hints are coalesced; one request in flight", "a fresh card is marked briefly, opacity only, and respects reduced-motion"). Move the exact values to the implementation and its frontend review.

Counts: B=0 M=4 N=2
