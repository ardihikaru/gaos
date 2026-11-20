# Comprehension review: `card-timing.md` (v1)

## Paraphrase

**What this proposes**

1. Add a wall-clock field `Event.At` (unix seconds). `MemStore.emit` stamps every new event with `time.Now()`. The ArcadeDB payload wire gets an `at` key so the value survives a restart. When the boot load finds a row with no `at`, it falls back to the row's existing `ts` column, which it does not read today. There is no new event type, no schema change, and nothing is rewritten.
2. Add a pure function `cardTiming` (new `dashboard/timing.go`). It replays one task's `TaskPlanned` and `TaskMoved` events into a chronological list of status segments: status, previous status, rule, entered and left times, seconds. It then puts each segment's seconds into exactly one of five buckets: AI working, waiting on operator, queued, blocked, backlog. "Idle" is lead time minus AI time. It also lists each run attempt's duration, tokens and cost. A second function, `workflowTiming`, rolls the cards of one workflow up into workflow totals.
3. Add `CardTiming` to the `Store` interface. `ArcadeStore` forwards it to its in-memory projection. Every `Board` read attaches a timing summary to each card and a `workflows` list to the board.
4. Add a back-office JSON route, `GET /products/{id}/board/cards/{taskId}/timing`, that returns the full history.
5. In the UI, put a lead-time chip on every card, on both the server-rendered board and the sisflow board. The chip opens an htmx "timing sheet" modal built on the answer sheet's pattern: a totals strip, a proportion bar, a timeline, an attempts table and a workflow line. The answer sheet's JS opener is generalised to serve both.
6. Add ten new register rows (TC-466 to TC-475), D-081, a v0.41.0 changelog entry, and the matching count updates.

**How the existing system works, per this document**

- Every status change goes through `moveTaskLocked` and writes a `TaskMoved` event carrying `Task`, `Rule`, `Reason="FROM→TO"` and `To`. `TaskPlanned` creates tasks in `PENDING`.
- In production `Event.T` is always 0, because only the test simulator advances the clock. ArcadeDB stores `ts = sysdate()` per event row, but the boot load never reads it back.
- Event rows are written close to the moment of the move: in the request's own transaction, or in the runner's `persistBackground` right after the move.
- Run records carry app-clock `StartedAt` and `EndedAt`.
- `GAOS_RUNNER` is `off`, `on` or `manual`. Production runs `manual`.

I could paraphrase every section, so there is no comprehension Blocker. I checked most of the `file:line` claims against the code and they hold, apart from small offsets (for example `openAnswer` starts at `app.js:153`, not 155; the board routes are at `main.go:375-380`).

**Isolation note:** while I was reading the rubric, the harness attached the contents of `.claude/CLAUDE.md` to my context on its own. I did not open that file and did not use it in this review.

---

### The flooring rule can make the bucket sum exceed the lead, so idle can go negative - Severity: Major
- Affected scope: §3.2 step 2, step 6 ("Invariant … relaxed to `≤`"), §8 R2
- Finding: When the clock steps backwards, the document floors the backwards segment at 0 and says the invariant relaxes to `sum ≤ lead`. The opposite happens. Take events at `At` = 100, 50, 200. The lead is 100. The segment from 100 to 50 floors to 0, but the next segment, from 50 to 200, counts 150. The sum is 150, which is more than the lead. If that extra time lands in `aiSeconds`, then `idleSeconds = leadSeconds − aiSeconds` goes negative. The card-level `idleSeconds` is not floored; only the workflow's is, with `max(0, …)` in §3.3. TC-469 asserts "floors the segment at 0" and does not check the relaxed invariant.
- Recommendation: State the actual bound, or clamp it. One option is to compute each `enteredAt` as the running maximum of `At`, which keeps the partition exact. Floor the card's `idleSeconds` at 0 too, and have TC-469 assert the chosen bound.

### Summing `costUsd` across attempts drops the basis that BR-31 requires to survive aggregation - Severity: Major
- Affected scope: §3.2 step 8 ("`costUsd` total and token totals are summed"), §3.5 card `timing.costUsd`, header ("BRD rule claimed: none … obeys BR-04 and BR-05")
- Finding: BR-31 (`pustaka/docs/product/brd.html:1283`) says every cost carries its basis, `measured` or `estimated`, and the distinction survives every aggregation. `TaskRunRecord.CostBasis` can be measured, estimated or unavailable (`dashboard/model.go:779`). The card summary sums the attempts into one `costUsd` with no basis. The document checks itself against BR-04 and BR-05 only, and never against BR-31.
- Recommendation: Either carry the cost per basis in the summary (for example `costUsd` split into measured and estimated parts, or a basis field set to `mixed` with the split), or drop the total and show only the per-attempt figures. Name BR-31 in the header.

### The card's history and the card's current status can disagree, and the document does not say which one wins - Severity: Major
- Affected scope: §1 ("every move is a `TaskMoved`"), §3.2 steps 1–3, §3.5 `timing.status` / `finished`, G7
- Finding: `TaskMoved` with `To` has existed only since T-164, which shipped in v0.30.0 (`pustaka/docs/changelog.html:546-556`). A task planned or moved before then has gaps in its event history. Such a task can be `DONE` in `TaskRecord` while `cardTiming` shows an open `PENDING` or `READY` segment whose lead keeps growing to `now`. The same thing happens for any status write that bypasses `moveTaskLocked`, and §1 asserts there is none without anchoring that claim for all time. The document also never says where `timing.status` and `finished` come from: the last segment, or `TaskRecord.Status`. G7 promises timings for all historical data on the live instance, which hides this case.
- Recommendation: Say where `status` and `finished` come from. Define what happens when the last segment's status differs from `TaskRecord.Status` (for example: treat the timing as unknown, or mark the history incomplete). Narrow G7 to "events since v0.30.0" and add a test for the mismatch.

### `queued` mislabels READY time under `GAOS_RUNNER=off` - Severity: Major
- Affected scope: §3.2 step 6 bucket table, row `queuedSeconds` ("the scheduler will dispatch it"); §0 "Runner mode"; N5 / L2
- Finding: The function receives only `manual bool`, so READY is treated as queued under both `on` and `off`. Under `off` the scheduler loop does not run (`dashboard/runner.go:40`) and an operator dispatch may not start a turn (`runnerMaySpend`, `runner.go:64-67`). Nothing will ever dispatch the card, yet the sheet reports it as queued for the scheduler.
- Recommendation: Pass the runner mode itself rather than a boolean, and give READY-under-`off` its own meaning (for example "runner off", or count it as blocked). Alternatively, state plainly that `off` is classified as queued and explain why that is acceptable.

### D-081 is claimed but its content is never stated - Severity: Major
- Affected scope: header "Decision number claimed: D-081"; §4 row 21
- Finding: The document reserves D-081 and changes the decision-log count, but never says what D-081 decides. Candidates are the `At` field, using `ts` as the fallback, the bucket taxonomy, or wall-clock rather than business hours. An implementer would have to write the decision text themselves. That breaks rule 6 of `docs/designs/README.md` ("nothing is left undecided").
- Recommendation: Write D-081's title and decision sentence in the document, plus the alternatives it rejects (§7 can be referenced).

### The fallback opening segment is underspecified - Severity: Nit
- Affected scope: §3.2 step 1, §3.6 ("`rule` is `"plan"` on the opening segment when it came from `TaskPlanned`")
- Finding: When there is no `TaskPlanned`, the opening segment's rule is not defined. It is also unclear whether the FROM segment is opened and closed at the same `At` (zero seconds) before the TO segment starts.
- Recommendation: Say so explicitly. For example: "opening segment `{status: FROM, rule: "", enteredAt = leftAt = e.At, seconds: 0}`, then step 2 applies to the same event."

### `workflowTiming` has no `now` argument, though its lead "runs to now" - Severity: Nit
- Affected scope: §3.3 signature and prose
- Finding: `func workflowTiming(id string, cards []CardTiming)` cannot read `now`, but the text says an open workflow's lead runs to `now`. The reader has to guess that it takes the cards' `asOf`.
- Recommendation: Add a `now int64` parameter, or say "lead runs to the cards' shared `asOf`".

### Types and view-models are named but not defined - Severity: Nit
- Affected scope: §3.2 `CardTiming`; §3.4 `CardTimingSummary`, `WorkflowTiming`; §4 row 11 `View.Timing *TimingView`
- Finding: Only JSON examples stand in for these struct definitions. `TimingView` is not described at all. The sheet's header needs the product name and the task title (§3.7 item 1), but `CardTiming` carries neither, so it is unclear where the handler gets them.
- Recommendation: List the Go fields of each type. State that `handleTimingSheet` also reads the task title and the product name, and from which store call.

### The bucket vocabulary in the segment JSON is not listed - Severity: Nit
- Affected scope: §3.6 `"bucket": "backlog"` / `"waitingOperator"`
- Finding: The summary keys are `*Seconds`. The segment field `bucket` uses short names, but only two of the five are ever shown, and the value for terminal segments is not given.
- Recommendation: List all the `bucket` values: `ai`, `waitingOperator`, `queued`, `blocked`, `backlog`, plus the terminal value (`""` or `"none"`).

### The timing modal markup omits its hidden condition - Severity: Nit
- Affected scope: §3.7 "The timing sheet"
- Finding: The quoted root `<div class="modal-back modal-anim" id="timingModal" hx-swap-oob="true">` has no `{{if not .Timing}}hidden{{end}}`. The model it copies has that condition (`dashboard/templates/needs.html:70`), and the document's own text says the sheet is "empty, hidden" until opened.
- Recommendation: Show the `hidden` condition in the snippet.

### The BLOCKED bucket's meaning is narrower than the transitions that reach it - Severity: Nit
- Affected scope: §3.2 bucket table, `blockedSeconds` row ("the runner, the gate or the budget guard holds it")
- Finding: An expired question also lands a task in BLOCKED (`dashboard/taskstate.go:90-93`, D-066), and BLOCKED leaves through the operator's `unblock` (`dashboard/board.go:177-178`). Part of the "blocked" time is really waiting on the operator.
- Recommendation: Widen the stated meaning, or note it as an accepted limit in §6.

### Cross-product filtering relies on an unstated uniqueness assumption - Severity: Nit
- Affected scope: §3.2 signature (no `productID`), TC-470 ("moves of … another product … change nothing")
- Finding: `cardTiming` filters only by `taskID`. That is correct only because task IDs come from a store-wide workflow sequence (`dashboard/store_mem.go:2073`), and the document never says so.
- Recommendation: State the assumption that task IDs are unique across products, or pass `productID` and filter by `DeskID` the way `foldTasks` does (`dashboard/taskfold.go:33`).

The other sections are solid: §1's account of how `ts` is persisted (`dashboard/store_arcade.go:443-480`, `store_mem.go:216`), the claim that `emit` is the single place events are stamped, the route and auth claims (`dashboard/auth.go:523-563`), §5's register arithmetic, and §9's rollback.

Counts: B=0 M=5 N=7
