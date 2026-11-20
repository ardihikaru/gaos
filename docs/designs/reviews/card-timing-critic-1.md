# Review: `docs/designs/card-timing.md` v1 (gaos-goldfish-critic)

I found no Blockers. There are five Majors, and in three of them the board would show numbers that look reliable but are wrong. The worst is that old cards get timings worked out from an incomplete history.

**Isolation note.** The harness put the contents of `.claude/CLAUDE.md` into my context without my asking. I did not open it, did not use it, and quote none of it here. I did not open `CLAUDE.md`, `AGENTS.md`, `GEMINI.md` or any credential file. The canary probe should take this into account.

**Claims I checked and found correct:**
- The event struct and the clock: `model.go:193-201`, `store_mem.go:444`, `store_mem.go:52`, `sim_test.go:86`.
- Event writing and loading: `ts = sysdate()` at `store_arcade.go:1066`, and the boot query reads no `ts` (`store_arcade.go:1745`).
- Task moves: `moveTaskLocked` (`runner.go:430-444`), the transitions (`taskstate.go:79-95`), the fold (`taskfold.go:26-70`).
- The board and its JSON: `boardOf` is pure (`board.go:217`), `ArcadeStore.Board` forwards (`store_arcade_blueprint.go:33-35`), sisflow keeps unknown card fields and overwrites `tasks` (`shapes.js:67-83`).
- Auth: back-office routes are gated (`auth.go:523-555`) and GETs pass the cross-origin check (`auth.go:560-563`).
- The five colour variables exist in both themes (`app.css:14,21,28,30,37,90-106`).
- The counts: 465 + 10 = 475 test cases, 341 + 10 = 351 Go tests, 319 → 329 in `dashboard/`. 79 decisions (D-001…D-080 without D-078) becomes 80 with D-081.

---

### Old card histories are incomplete, yet are reported as known - Severity: Major
- **Affected scope:** §1, glossary "Move", G7, §3.2 steps 1–4, L3.
- **Finding and rationale:**
  - The design rests on "every status change writes one `TaskMoved`". That has only been true since T-164, which shipped in v0.30.0 (`pustaka/docs/changelog.html:546-556`). Before that, runner, scheduler and recovery moves wrote no `TaskMoved`, and board moves had no `To` field. Step 2 skips events with an empty `To`.
  - So a card planned before v0.30.0 can fold to `PENDING` or `READY` forever while its real status (loaded from the `Task` vertex) is `DONE`. Its whole lead time lands in one bucket, and the card still says `known=true`.
  - L3 also misreads `store_arcade.go:239-252`. A background write that fails or times out is not retried "with a later ts". Its events are **lost**, and only the records are written again. After a restart, the stream is missing that move while `Task.status` already shows it. That is the same gap, and it can happen on any live instance.
  - Step 4's "every production row has ts" protects against neither case.
- **Recommendation:**
  - In `cardTiming`, compare the status the history ends on with `TaskRecord.Status`. If they differ, or if no `TaskPlanned` was found, set `known=false`, or add a `partial` flag. Pass the current status in as an argument so the function stays pure.
  - Add a negative test for "the history disagrees with the live status".
  - Restate L3 to say these events are lost, not delayed.

### A clock step makes the bucket total exceed the lead, not fall short - Severity: Major
- **Affected scope:** §3.2 step 6 (the invariant), R2, TC-469.
- **Finding and rationale:**
  - Rounding a negative interval up to 0 adds time; it does not remove it. Take `At` values 0, 100, 50, 200. The segments come out as 100, 0 and 150, which sum to 250. The lead is 200.
  - So the "relaxed to ≤" invariant is backwards. `idleSeconds = lead − ai` can go negative on a card. The workflow idle is clamped with `max(0, …)` (§3.3); the card idle is not.
  - TC-469 would be written to an assertion that is false.
- **Recommendation:**
  - Make each event's time non-decreasing before measuring: use `max(At, previous effective At)` in `Seq` order. The partition then stays exact with no relaxation, and the invariant has one form.
  - Clamp card `idleSeconds` at 0 anyway.
  - Rewrite TC-469 and R2 to match.

### Old rows and new rows use two different clocks, so "durations survive" is not true - Severity: Major
- **Affected scope:** §3.1 "ts timezone", L4.
- **Finding and rationale:**
  - "The same assumption is already made for … `TaskRun.startedAt`" is false. `startedAt` is written by the app as a UTC string (`unixStamp`, `store_arcade.go:1905`). `ts` is `sysdate()` in the database server's timezone. Only `Artifact.createdAt` (`store_arcade.go:1226,1290`) shares that assumption.
  - The timezone was measured on the scratch image only. Nothing under `deploy/` sets a timezone (searching for `TZ`/`timezone` finds nothing), so production is unmeasured.
  - "Durations are differences and survive" holds only when both ends come from the same clock. It fails in two cases, and both are exactly the cards a v0.41.0 operator looks at:
    - a segment opened by a pre-upgrade event (`ts`) and closed by a post-upgrade one (`at`);
    - every open old segment, measured against `now` from `time.Now()`.
  - Either case is off by the full timezone offset, plus any skew between the database host's clock and the app host's.
- **Recommendation:**
  - At boot, read `SELECT sysdate()` once, compare it with `time.Now()`, and apply that offset to every `ts`-sourced `At`. Or label segments built from `ts` and refuse to mix the two sources in one interval.
  - Correct the sentence about `TaskRun.startedAt`.
  - Measure the production instance before this ships, rather than leaving it as L4.

### The chip on an open card goes stale, and its "live" dot says it is current - Severity: Major
- **Affected scope:** §3.7 "lead-time chip" and "Live refresh".
- **Finding and rationale:**
  - The chip shows a lead computed at board-read time (`asOf`). The board refreshes only on task hints (`board.js:140-154`).
  - The cards that matter most are the ones with no events: a card in `READY` under manual mode, or one in `AWAITING_INPUT`. Their lead freezes at the last refresh, possibly hours old.
  - Meanwhile the design gives open cards a leading `--run` dot "so a ticking card reads as live". Nothing ticks. The sheet is a snapshot too.
  - This is an all-day operator console (`.claude/rules/frontend.md`), so a frozen number with a live marker misleads the operator.
- **Recommendation:** Either:
  - have `board.js` (and a tiny script for the server-rendered board) work out the open lead as `asOf + (Date.now()/1000 − loadTime) − createdAt`, refreshed once a minute; or
  - drop the live dot and render "as of HH:MM".

  Say which one, and add it to the change list.

### Renaming the answer-sheet JavaScript risks the only way to answer an agent, and nothing tests it - Severity: Major
- **Affected scope:** §3.7 "app.js", change #16.
- **Finding and rationale:**
  - The design renames and generalises `openAnswer`/`showAnswer` (`app.js:153-184`) into `openSheet`/`showSheet` to reuse them. That code carries answer-only state: the shared `trigger`, `openedByKey`, `syncAnswer`, radio focus, and the `answerModal` checks at `app.js:126,276,312`.
  - The repository has no JavaScript tests. TC-474 renders the server side only.
  - A regression would break answering agents (M15, BR-44) with nothing to catch it.
- **Recommendation:** Add separate small `openTiming`/`showTiming` functions and leave the answer code byte-identical, generalising only `returnFocus`'s id check. Or name a manual check (open, Esc, focus return, then answer a question) in the test plan's prose.

### `READY` is mis-labelled under `off`, and sometimes under `manual` - Severity: Nit
- **Affected scope:** §3.2 bucket table, N5.
- **Finding and rationale:**
  - `manual bool` puts `off` into the "queued — the scheduler will dispatch it" bucket. Under `off` nothing dispatches (`runner.go:40-45,64-67`).
  - Under `manual`, a `READY` card in a product that already has a live turn cannot be dispatched by the operator either (BR-38, `board.go:380-383`).
- **Recommendation:** Pass the runner mode rather than a bool, and label `off` "not running". Or state both cases as accepted limits.

### Some citations point at the wrong lines - Severity: Nit
- **Affected scope:** §3.3, §3.2 last paragraph.
- **Finding and rationale:**
  - `board.go:271-279` is not where BR-38 is enforced. Those lines are `MoveCard`'s design-gate check and the hand-off to dispatch. The enforcement is at `board.go:380-383,412-416`.
  - The move to `RUNNING` is at `runner.go:157`, not `runner.go:430`, which is `moveTaskLocked` itself.
- **Recommendation:** Fix the line numbers.

### `GET /events` changes shape without a mention - Severity: Nit
- **Affected scope:** §3.1, N1, change list.
- **Finding and rationale:** `Event` has no JSON tags, and `handleEvents` encodes it directly (`handlers_tasks.go:36-43`). Every event in `GET /events` gains an `"At"` key. The change is additive, but it is a public change and is not listed.
- **Recommendation:** Say so in §3.1, and decide on the tag (`json:"at,omitempty"` or `json:"-"`).

### D-081's content is not stated - Severity: Nit
- **Affected scope:** header, change #21.
- **Finding and rationale:** The design claims a decision number but never says what the decision records. Two engineers would write different rows.
- **Recommendation:** Give the one-sentence decision text. For example: "wall-clock `at` on the event payload wire; `ts` is the fallback for older rows."

### The timing sheet's error and empty paths are not specified - Severity: Nit
- **Affected scope:** §3.7 "The timing sheet".
- **Finding and rationale:**
  - The JSON route's 404 is defined. The HTML route's response for an unknown product or task (`s.fail`? a toast? 404?) is not.
  - `page.html` always includes `timing-modal`, so the template must render when `.Timing` is nil. That is not stated.
- **Recommendation:** Specify both. Include the unknown-card case in TC-473.

### The design does not trigger the security lens or fix a stale rule sentence - Severity: Nit
- **Affected scope:** §3.6, change #20.
- **Finding and rationale:**
  - The design adds two back-office routes. That meets the security criterion (`docs/rules/gaos-design-review.md:92-95`), but the design does not say the security lens is due.
  - `.claude/rules/qa-test-plan.md:73-74` still says "currently all four hundred and sixty". That contradicts the same file's 465.
- **Recommendation:** Note that the security review is required. Correct that sentence when updating the counts.

### The performance claim is not measured - Severity: Nit
- **Affected scope:** §3.4 "Cost", R1.
- **Finding and rationale:**
  - "Sub-millisecond at thousands of events" is an assumption. The stream covers every product, including heartbeats.
  - The pass runs under `RLock` on every board read, every move response (`handlers_board.go:80`), and every task hint for every open client.
  - `runsFor` is called per card with a linear scan (`store_mem.go:1199-1208`), which is O(cards × runs).
- **Recommendation:** Label it as an assumption, or measure the event count on production. Index runs by task inside `decorateBoardTiming`.

### Two accepted limits are missing - Severity: Nit
- **Affected scope:** §6.
- **Finding and rationale:**
  - A dashboard crash during a turn leaves the card `RUNNING` until boot recovery requeues it (`turns.go:237`). Downtime is therefore counted as "AI working".
  - The architecture says events are kept at full fidelity for 90 days, then go to cold archive (`pustaka/docs/architecture/graph.html:213`). Timing computed from the stream would break for older cards once that archive lands.
- **Recommendation:** Add both as limits L6 and L7.

---

**Sections that hold up:** BR-04 and BR-05 compliance (§3.1: no new event type, no rewrite) is sound. The alternatives (§7) are real and reasoned. The change list covers every file the design names. The test and decision arithmetic is correct.

Counts: B=0 M=5 N=8
