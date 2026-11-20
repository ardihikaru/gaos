# Zoom-out review: `card-timing.md` (v1)

**Isolation note.** Two things the harness did need recording. It inserted the contents of the worktree's `.claude/CLAUDE.md` into my context without my asking. I did not open that file and did not use it for any finding below. Separately, I read `.claude/rules/frontend.md`, `docs/rules/gaos-design-review.md` and `docs/designs/README.md` because the task listed them in the rubric. The canary check should treat this run as possibly exposed.

**Checks that held.** I confirmed these claims against the code:
- `Event` has no wall-clock field (`dashboard/model.go:193-201`).
- `emit` stamps `T: s.clock` (`dashboard/store_mem.go:444`), and only `dashboard/sim_test.go:86` advances the clock.
- The boot load leaves `ts` out of its query (`dashboard/store_arcade.go:1745`).
- Every status change goes through `moveTask` (`dashboard/taskstate.go:145`), and its only caller is `dashboard/runner.go:437`. So the claim that "every move is a TaskMoved" holds.
- Unknown card fields pass through sisflow (`dashboard/static/vendor/sisflow/board/core/shapes.js:70`).

**Arithmetic.** 465 + 10 = 475 register rows. Go tests go from 341 to 351, and dashboard tests from 319 to 329. The next free ID is TC-476. The decision log goes from seventy-nine to eighty entries. All of these are correct, and the "four places" for the count are confirmed (`pustaka/docs/product/qa-test-plan.html:19,79`, `pustaka/docs/assets/toc/product.js:162,168`). The change list touches 26 distinct files: 19 under `dashboard/` and 7 in docs and rules. The design states no file-count target, so there is nothing to check that against.

**Sections that are sound.** The root problem (§1) is diagnosed correctly. Computing timing from the existing event stream instead of adding an event type or a column (§7) is the right, smallest-footprint call. The algorithm and buckets in §3.2 are proportionate to the request.

---

### The per-card JSON route has no consumer - Severity: Major
- Affected scope: G5 (second half), §3.6, change list rows 8 and 10 (one of the two lines), TC-473 and TC-474 (the JSON-route assertions)
- Simplicity rationale: Nothing reads `GET /products/{id}/board/cards/{taskId}/timing`. `board.js` only fetches `…/board` (`dashboard/static/board.js:112-121`). The timing sheet is server-rendered HTML from `GET /ui/timing/{id}/{taskId}` (§3.7), which can call `Store.CardTiming` directly. The JSON route is a second back-office route for the same data, with its own handler, 404 shape and tests. It also widens the attack surface the security lens has to examine. That lens must run anyway, because the design adds back-office routes (`docs/rules/gaos-design-review.md:90-95`).
- Recommendation: Drop the JSON route and `handleCardTiming`, and keep only the HTML sheet route. Add the JSON route when a client exists that needs it. The tests shrink: TC-473 covers one route and TC-474 drops its `segments` assertion.

### Workflow totals and `Board.Workflows` go beyond the request - Severity: Major
- Affected scope: G4, §3.3, §3.5 (the `workflows` array), §3.4 (`CardTiming.Workflow`), §3.7 item 6, TC-472
- Simplicity rationale: The product owner asked for timing "in each card". Nothing on the board would read `workflows` (`board.js` never looks at it). Its only consumer is one footer line in the sheet. Despite that, it adds a second pure function, a second JSON type, a field on `Board`, a field on `CardTiming`, an argument that depends on BR-38 (the Σ union) to be correct, and one register row. That is a speculative aggregate.
- Recommendation: Remove G4, `workflowTiming`, `Board.Workflows` and TC-472. If the operator does want the workflow line, compute it only in the sheet handler from the cards' summaries, which `Board()` already carries, and leave it off the board JSON. With this finding and the previous one applied, the count becomes 474 rows, 350 Go tests, 328 dashboard tests, and next free TC-475.

### An open card's chip freezes while nothing moves, though a ticking mechanism already exists - Severity: Major
- Affected scope: §3.7 "Live refresh", §3.7 lead-time chip, §3.4 `asOf`
- Simplicity rationale: Chips refresh only on task hints (`board.js:142-154`). In production the runner is `manual` (D-080, §6 L2), and a card waiting in READY or AWAITING_INPUT produces no hints. Its chip therefore keeps showing the lead time from the last board load. The idle time the product owner asked about is exactly what stops counting. The console already solves this. `static/live.js:164-191` re-ticks `time[data-since]` every 30 s and on `htmx:afterSettle`, using `span()`, which mirrors `relSpan` (`dashboard/handlers_bo.go:589-599`).
- Recommendation: Render an open card's chip as a `<time>` carrying `createdAt`. Give live.js's existing `tick()` a label-agnostic variant (it currently hard-codes the "waiting " prefix at `live.js:180`). Do not accept the stale value as a limit.

### `fmtDur` duplicates the existing `relSpan`/`span` formatter - Severity: Nit
- Affected scope: §3.7 "Durations are formatted in Go (`fmtDur` …)", change list row 6
- Simplicity rationale: `relSpan` (`dashboard/handlers_bo.go:589`) and its client twin `span` (`dashboard/static/live.js:168`) are the console's duration format (`<1m`, `12m`, `5h`, `9d`). The design adds a third format (`45s`, `1h 05m`, `2d 3h`) without mentioning the existing pair. After this change the inbox would say `5h` and the board `5h 00m`, and a ticking chip would need a third JS twin.
- Recommendation: Name `relSpan` in the design. Either reuse it for the chip, keeping `fmtDur` only for the finer timeline rows, or extend `relSpan` and `span` together. Say which and why.

### The design forbids charts but adds one - Severity: Nit
- Affected scope: N6 ("No charts") versus §3.7 item 3 (proportion bar)
- Simplicity rationale: The bar is a chart, and it is `aria-hidden` because it repeats the totals strip directly above it. If it stays, its widths need a `template.CSS` value. The design says "no `template.HTML` is built" but says nothing about CSS. The existing escape-at-construction helper for this is `pctStyle` (`dashboard/view.go:25`, used at `dashboard/handlers_bo.go:293`). The single-fill `.cov-bar` (`dashboard/static/app.css:914-915`) already exists as a visual precedent.
- Recommendation: Drop the bar. That keeps N6 true and leaves one less CSS block. If the bar stays, change N6 and state that the widths go through `pctStyle`.

### `byStatus` has no reader - Severity: Nit
- Affected scope: §3.2 step 7
- Simplicity rationale: The stated purpose is "for a reader who disagrees with the buckets". No UI element, test or JSON example uses it.
- Recommendation: Remove it. The `segments` list already carries per-status seconds.

### Renaming `openAnswer`/`showAnswer` is unnecessary churn - Severity: Nit
- Affected scope: §3.7 "app.js"
- Simplicity rationale: `showAnswer` already works for a modal with no form. `syncAnswer` returns when no `#answerForm` exists (`dashboard/static/app.js:190-191`), and focus falls back to `.modal` (`app.js:181-183`). Renaming both functions and editing the answer-only branches puts the Needs-you flow at risk for no gain.
- Recommendation: Keep the names. Add `[data-timing-open]` to the delegate selector (`app.js:40`), with a branch that sets `trigger` and calls `htmx.ajax`. Widen the two id checks to include `timingModal`: `afterSwap` (`app.js:312`) and `returnFocus` in `closeModal` (`app.js:126`).

### Two stored times for one fact - Severity: Nit
- Affected scope: §3.1 (the `at` payload-wire field plus `ts`, and the rule for which one wins)
- Simplicity rationale: The reason for adding `at` is that it shares a clock with `TaskRun.StartedAt`. But TaskRun stores its app-clock time in a DATETIME column through `unixStamp` (`dashboard/store_arcade.go:1901`) and reads it back with `parseStamp` (`store_arcade.go:1708`). Writing the event's `ts` the same way, from `e.At` instead of `sysdate()`, gives one stored time and no wire field. Reloading would always read `ts`, with no choice between two values. The design does not weigh this option.
- Recommendation: Add it to §7. Either adopt it, or state why `ts` must stay the database's own clock (for example, if another reader relies on that).

Counts: B=0 M=3 N=5
