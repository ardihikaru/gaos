<!-- Round 3, 2026-11-26. Lens: readiness. Target: docs/designs/urls-and-board.md (rev 3). Verbatim reviewer output. -->

# Readiness review: urls-and-board.md (rev 3)

Reviewed `/home/ardi/devel/projects/ardihikaru/my-projects/gaos/.claude/worktrees/feat-urls/docs/designs/urls-and-board.md` against `docs/rules/gaos-design-review.md` and `docs/designs/README.md` in that checkout. I checked the design's code claims against the checkout.

**Isolation note.** The checkout sits under the outer repository's `.claude/worktrees/`. Because of that, the harness attached three outer rule files to my context without my asking: `.claude/rules/agent-workspace.md`, `.claude/rules/qa-test-plan.md` and `.claude/rules/frontend.md`. No `CLAUDE.md`, `AGENTS.md` or `GEMINI.md` was opened or attached. I did not open the checkout's `.claude/` or `docs/designs/reviews/`.

**Claims that check out:**
- **Where `buildBO` is called.** The 14 call sites (`handlers_bo.go:96,137,148,158,170,508,518,1028`, `handlers_conversation.go:121,123,156`, `handlers_cw.go:87`, `handlers_questions.go:152`, `handlers_tasks.go:20`) match §3.4's table plus the five functions allowed to call it.
- **Existing links.** There are exactly eleven `<a … hx-get>` elements, at the lines §3.6 gives.
- **Baseline counts.** 565 dashboard plus 22 pustaka gives the 587 Go tests. TC-721 is the highest case id. The decision log has "Ninety-one entries", ending at D-092.
- **Column ids.** The nine ids fit inside the `rev` 256-byte cap (about 220 bytes).
- **Routes.** None of the new route paths clashes with an existing one, and the gate can see a request's matched pattern before the handler runs (`auth.go:529`).
- **Phase 1.** Its routes, slug handling and sign-in behaviour are otherwise decidable.

### The column hash still changes every request for any card that is not finished - Severity: Major
- Affected scope: §5.1 first bullet; §10.2 `TestBoardLiveSendsOnlyTheColumnsThatChanged`; §5.6 `BenchmarkBoardLive`
- Finding: The hash covers the JSON of every card with only `Timing.LeadText` and `Timing.TitleText` blanked. But `BoardCard.Timing` is a `*CardTimingSummary` (`board.go:63`). That type also carries `AsOf`, `LeadSeconds`, `AISeconds`, `IdleSeconds`, `WaitingOperatorSeconds`, `QueuedSeconds` and `BacklogSeconds` (`timing.go:43-50`), and for an open segment these are worked out from `now` (`timing.go:231-232`, `:256-271`). So any column with an unfinished card gets a new hash on every request. The test's "no column, also after the clock moves a minute" cannot pass, and the ≤ 60 KB live-refresh target does not follow. The design also never says which field means "the clock is still running".
- Recommendation: Hash a defined projection, not the raw JSON. Either name every `CardTimingSummary` field that is zeroed when `!Timing.Finished` (`AsOf` and every `*Seconds` field, plus the two text fields), or hash an explicit struct listing the fields that go in. Name `Finished` as the "clock running" test.

### The hook adds the form's current fields back, so "Board" and "Clear filters" undo themselves - Severity: Major
- Affected scope: §4.5 hook step 1; the `nav.af-seg` bullet; `a.af-reset`; the count line's "Clear filters" button
- Finding: Step 1 adds "each field of `#boardFilters` that the request does not itself carry". The form has hidden `view`, `sort` and `dir` inputs. In the table view, the **Board** link carries no `view` ("without `view=table`"), so the hook adds `view=table` from the hidden input and the operator cannot leave the table. "Clear filters" carries no filter parameters, so the hook adds every current filter back and nothing clears. Also, for the form's own `GET`, htmx keeps fields in `evt.detail.parameters` and appends them to the path after the event (vendored `static/htmx.min.js`: `n=C.path … R+=an(w)`). "Removes `q` from the URL" and "added to the request's URL" therefore leave open whether to edit `path` or `parameters`, and one choice puts `q` into the URL or duplicates parameters.
- Recommendation: Give the switch, sort and reset links an opt-out (for example a `data-board-own-query` attribute), or have them carry every parameter explicitly, including empty `view=`. State that step 3 deletes `q` from `evt.detail.parameters`, and that step 1 only touches `evt.detail.path` for requests that are not issued by the form itself. Add both cases to `TestABoardMutationKeepsTheView` or a browser exit step.

### The 64-byte value limit conflicts with `shown` and `rev` - Severity: Major
- Affected scope: §4.2 ("A value longer than 64 bytes is dropped") against the `shown` and `rev` rows (256-byte limit)
- Finding: `parseBoardQuery` "reads everything below" and drops any value over 64 bytes. With the nine column ids (`board.go:119-127`, 59 bytes of ids), a full `shown` value is up to about 103 bytes and a full `rev` about 220 bytes. If the 64-byte rule applies to them, `rev` is always dropped and every refresh sends all nine columns. If it does not, the text does not say so. Two engineers will build different parsers.
- Recommendation: Say that the 64-byte limit applies to the `BoardQuery` parameters only, and that `shown` and `rev` are governed by their own 256-byte rule.

### The paged JSON form has no test, and the checkpoint regex selects a test that does not exist - Severity: Major
- Affected scope: §4.3 (`GET /products/{id}/board` with `paged=1`); §10.2 checkpoint; §10.2 test table
- Finding: None of the fourteen new tests checks the paged document: `counts`, `matched`, `all`, `rows: []`, no `row` on any card, `workflows: []`. None checks that the unpaged document ignores filter parameters. sisflow depends on both. The checkpoint command `go test -run 'BoardPage|BoardQuery|BoardJSON|BoardCard'` names `BoardJSON`, but no test of that name exists. The same regex also misses two server-half tests: `TestBoardFiltersNarrowCardsAndCounts` and `TestDoneIsNewestFirst`.
- Recommendation: Add a test (for example `TestTheBoardJSONPagesOnlyWhenAsked`) as TC-747 and carry it through the counts (register 746→747, Go tests 611→612). Fix the checkpoint regex so it selects every server-half test by name.

### A negative `shown`: dropped or clamped - Severity: Nit
- Affected scope: §4.2 `shown` row; §10.2 `TestBoardPageCapsEachColumnAndCountsTheRest`
- Finding: §4.2 says a value that is not a number is dropped and anything else is clamped to 1..300, so `-5` becomes 1. The test says "a negative … one is ignored", so `-5` falls back to the default 30.
- Recommendation: Pick one and make both places say it.

### `AssignSlug` and `AssignSlugs` - Severity: Nit
- Affected scope: §3.2 (line 103) against §9 phase 1 (line 510)
- Finding: The method is named `AssignSlug(ctx, productID)` in §3.2 and `AssignSlugs` in the change list.
- Recommendation: Use `AssignSlug` in both places.

### Where the back-fill runs in `NewArcadeStoreFor` - Severity: Nit
- Affected scope: §3.2, second bullet
- Finding: "after `load` and `markLoaded`" applies only to the seeded branch (`store_arcade.go:163-165`). The unseeded branch builds `NewMemStore(seed)`, which already back-fills, and then calls `persistAll`.
- Recommendation: Say the `AssignSlug` loop runs only in the `else` branch, before `installHook`.

### The timing index holds event positions, not `Seq` values - Severity: Nit
- Affected scope: §4.1 step 4
- Finding: `s.timingIdx[id]` holds positions in `s.events` (`timing.go:404-434`), not `Seq` values. The index is also rebuilt lazily when it is stale (`:433`).
- Recommendation: Say the value is `s.events[last(s.timingIdx[id])].Seq`, read after the same staleness check `taskEventsLocked` does.

### An open item left to the server-half checkpoint can be settled now - Severity: Nit
- Affected scope: §11, third row
- Finding: The fallback to request headers is "decided at the server-half checkpoint". That checkpoint has no JavaScript in it, so it cannot decide this. The vendored htmx already uses the changed `C.path` for every verb, so the fallback is not needed.
- Recommendation: Drop the conditional and state that the URL form is the design.

### `reload()` sends the whole form through `FormData` - Severity: Nit
- Affected scope: §4.8, Loading row
- Finding: `FormData` of `#boardFilters` includes `q` and the hidden `surface` input. The design does not say to strip them from the `…/board?paged=1` URL. `TestTheSearchTextNeverTravelsInAURL` would catch `q` only after the fact.
- Recommendation: State that `reload()` deletes `q` and `surface` before it builds the query.

### How the hook recognises a live refresh - Severity: Nit
- Affected scope: §4.5, "One request at a time"; §4.8, Show more row
- Finding: "Sets `X-GAOS-Refresh` in the hook for that one request" does not say how the hook recognises that request. sisflow's "Show more" also fires `refresh`, so it would be marked as a background refresh as well.
- Recommendation: Name the test (for example `evt.detail.triggeringEvent.type === 'refresh' && triggeringEvent.detail.live`), and give the two callers different event details.

### Page links for the Pull requests and Auto-fix rows need view-model fields - Severity: Nit
- Affected scope: §3.6, second bullet; §9 (`prview.go`, `autofix_view.go`)
- Finding: The templates print struct fields (`.Href`, `.ResetHref`, `.CloseHref`, and `.Href` on the state tabs; `prview.go:65,81,119`, `autofix_view.go:90,163,186,208`). They do not call `prHref` or `afHref`. "The templates print them in `href`" therefore needs new fields that the design never names.
- Recommendation: List the new fields (for example `PageHref`, `ResetPageHref`, `ClosePageHref`) on each struct, and say where they are set.

### Default values in the query for `prs` and `autofix` - Severity: Nit
- Affected scope: §3.4, `Href` query table
- Finding: The design does not say whether a default such as `state=open` is written. `prHref` leaves it out (`prview.go:130`). If `Href` keeps it, every visit gets a needless `HX-Replace-Url`.
- Recommendation: State that `Href`'s query is the query of `prPageHref`/`afPageHref` for the parsed filter, defaults left out.

### The failed-write conformance case has no stated mechanism - Severity: Nit
- Affected scope: §10.1, "Changed" paragraph ("a failed back-fill write is retried at the next open")
- Finding: The conformance table also runs against `MemStore`, which has no write to fail. The design does not say how the failure is injected.
- Recommendation: Name the injection point (for example a failing `persist` stub in the Arcade test fixture), or move the case to an Arcade-only test.

### `Matched` asserted as "true" - Severity: Nit
- Affected scope: §10.2 `TestBoardPageCapsEachColumnAndCountsTheRest`
- Finding: `Matched` is an `int`, so "`Matched` true" states no value to check.
- Recommendation: State the expected number, for example `Matched` = the column's full count.

Counts: B=0 M=4 N=11
