# Goldfish critic review: release-b4-unit-tests.md (rev 10)

**Summary.** Rev 10 fixes the round-9 boot Blocker in a way that can be built. Building `qaOut` before `main.go:292` works: `srv` is built at `main.go:368-369`, and `Reconcile` needs only `usable()`, not `nt`. Making `Reconcile` always skip runner-driven cards keeps the e2e adoption that already exists at `qajobs.go:355-361`. I found no Blocker.

The main weaknesses are elsewhere:
- **A claim with no mechanism.** The success metric depends on move rules that nothing in the design writes.
- **Coalescing.** It is keyed on planning order, not merge order, and it does not cover BLOCKED cards, which is where a queue actually builds up.
- **Unenforced rules.** Some rules hold for one path but not for the paths beside it (`CloseDefect`, attribution).
- **An unbuildable signature.** The scheduler is pure and cannot take the key that `labelsAllowDispatch(labels, key)` needs.

**Anchors I confirmed:**
- `prflow.go:88-91, 240-246, 294, 298-300, 549-557, 600-606, 646-658`
- `labels.go:179-188`
- `main.go:292-302, 368-370, 390, 429, 439-444, 490-491, 570, 594-595`
- `runner.go:457-478, 696-722`
- `store_arcade.go:393-394, 960-964, 1636, 2329`
- `qajobs.go:193-194, 252-262, 303, 350-397, 429, 448, 456, 465-468, 482-499`
- `qaclient.go:40, 67, 393`
- `github.go:309-316, 342-394, 414`
- `board.go:213-242, 330-405, 452, 479`
- `store_mem.go:2378-2436`
- `defects.go:347-379, 590-608`
- `autofix.go:122, 162-218, 311, 350-361`
- `autofixcheck.go:275, 354-360`
- `handlers_autofix.go:17, 102, 116-175`
- Registers: BR-38, BR-44, BR-48, BR-54 and BR-61 at `brd.html:1298-1352`

**Counts I confirmed:**
- 713 + 25 = 738 tests overall, and 688 + 25 = 713 in `dashboard/` (`README.md:143`, `check-workspace.sh:115`).
- 17 `checkFix` calls in `fixpr_test.go`.
- Four test callers of `Reconcile`.
- TCs per phase: 3+4+5+7+5+1 = 25.
- 19 labels become 20.

---

### Success metric 1 still cannot be computed: no unit move carries the outcome rules - Severity: Major
- Affected scope: §0 "Success metrics"; §5.1 table (DONE "not a unit-test base" via `ResolvePRCard`); §5.5 table; §6 poll order step (5)
- Finding and rationale:
  - §0 says a card's final `TaskMoved` carries `Rule` = `unit-passed` / `unit-failed` / `unit-no-tests` / `unit-not-base`.
  - Every final DONE in the design goes through `ResolvePRCard`: §5.1 for not-base, §6 step (5) for the outcomes. That function always passes `RulePRResolved = "pr-flow"` (`prflow.go:592, 626`), and `moveTaskWithLocked` writes the `rule` argument into the payload (`runner.go:474`).
  - The `ResolveUnitQACard` table (§5.5) has no RUNNING → DONE row and none of those four rules.
  - So the round-9 fix ("Metric 1 not computable", §23) exists only in §0. Two engineers would emit different rules, or none.
  - The metric also does not say how the other endings count: `unit-superseded`, `unit-stale`, `unit-runner-absent`, `qa-job-failed` and `qa-unit-retry-ended`. Excluded from the denominator, or counted as misses? During a burst of merges most cards are superseded, so this choice decides the number.
- Recommendation:
  - Add a RUNNING/PENDING/READY → DONE row to `ResolveUnitQACard` that takes the outcome rule, and route §5.1's not-base move and §6 step (5) through it.
  - Name the rule per §6 outcome row.
  - State how every other final rule counts in the metric.
  - Have TC-867 assert the `Rule` on the final `TaskMoved`.

### Coalescing is keyed on planning order and skips BLOCKED/READY cards; the stated bound does not hold - Severity: Major
- Affected scope: §4.2 "Coalescing per repository"; §13 rows "Keep every queued merge PENDING" and "Coalesce per (repository, base)"; §14 "Many merges at once"; the published API text in §4.2
- Finding and rationale:
  1. **Older means planned earlier, not merged earlier.** `merged_at` is read only at start (§5.1). The API may plan a new card for any merge up to 7 days old (`unit-qa-too-old`). An API request for an old PR (#3) whose card was never planned cancels the PENDING card of the newer merge (#7) as "superseded by a newer merge". The repository's newest commit is then never tested. That reverses §4.2's justification ("a newer merge's run covers the repository's current tests").
  2. **The API client's own card is cancelled with no stated contract.** A client-owned `unit:qa` card is superseded CANCELED by the next Brain Town merge. The published API text in §4.2 names three new codes but not this cancel.
  3. **The bound fails in an outage.** "The queue holds at most one PENDING card per repository" (§14) holds only for PENDING. During a GitHub or QA-tool outage:
     - each merge's card becomes BLOCKED after 10 tries (§5.1, §5.3);
     - BLOCKED and READY cards are never superseded, so the next merge's PENDING card supersedes nothing.
     
     A day-long outage builds one BLOCKED card per merge. Those cards retry inside the shared budget of 2 actions per tick. When the outage ends they all run, on commits up to 24 hours old, which is the "days-old commit" problem §13 rejects.
- Recommendation:
  - Supersede by `merged_at`, not by planning order:
    - either do it at start, after the read: a card whose repository has a started or queued card with a newer `merged_at` ends `unit-superseded`;
    - or have the API path read `merged_at` (it already calls `prepare`, `api_tasks.go:212`) and never supersede a newer merge.
  - Extend supersede to BLOCKED retry cards and READY `unit retry` cards. They hold no job.
  - State the API client's view of the cancel in the published contract.

### `labelsAllowDispatch(labels, key)` cannot be called from the pure scheduler, and its callers and tests are missing from the change list - Severity: Major
- Affected scope: §4.2 ("`labelsAllowDispatch(labels, key)` gains the key"); §4.4 `TaskNode`; §12 `labels.go`, `scheduler.go` and existing-test rows
- Finding and rationale:
  - The scheduler's call is `labelsAllowDispatch(t.Labels)` on a `TaskNode` (`scheduler.go:286`). §4.4 says `TaskNode` carries no key; it gains only `QAKey bool`. A key-taking signature cannot be satisfied there.
  - The other production caller, `apidispatch.go:47` (`apiClaimLocked`), is not in §12.
  - Four existing tests call the one-argument form and are not listed: `autofix_test.go:225`, `labels_test.go:167`, `prflow_test.go:252`, `prflow_test.go:257`.
  - Two engineers would choose differently (a bool parameter, a key string, or a second function), and the build breaks until this is decided.
- Recommendation: Make the parameter `qaKey bool`, filled from `TaskNode.QAKey` in the scheduler and from `qaToolKey(t.DedupKey)` in `apiClaimLocked`. Add `apidispatch.go` and the four test files to §12.

### A non-admin can still cancel a released unit defect's cards through `CloseDefect` - Severity: Major
- Affected scope: §7.3 "Closing the defect is guarded the same way" versus "Cancelling … any card of a `unit` defect (triage or fixer, held or released) requires `mv.Admin` … a non-admin cannot … orphan a released defect"; BR-64; TC-870
- Finding and rationale:
  - `handleCloseDefect` needs only a session (`handlers_autofix.go:158-167`). `CloseDefect` cancels every non-running card of the record (`defects.go:606`, `cancelDefectCardsLocked`, `defects.go:363-379`).
  - The design adds the admin check only when a card is BLOCKED `ReasonUnitHeld` ("Every other defect closes as today").
  - So once an admin releases a unit defect, any signed-in account can close it and cancel its READY fixer card. That is the exact action the board guard forbids (round-9 SEC-2), reached by a different route.
- Recommendation: Require `admin` in `CloseDefect` for every `unit` defect, held or released. Have TC-870 assert that a non-admin close of a released unit defect answers 409 and leaves its cards in place.

### "Every board move is attributed" is not what the mechanism does - Severity: Major
- Affected scope: §2 (g); §7.3 ("**Every** `MoveCard` move … records `EventPayload{Actor: actorAlias(mv.Actor)}`"); BR-48 clarification (§10); delegation record line 37; TC-857
- Finding and rationale:
  - **The dispatch move is not covered.** Ready → Working returns early into `dispatchNow` → `dispatch` (`board.go:356-358, 466-467`), which takes no actor. This is the board move that spends money. Reorders (`reorderLocked`, `board.go:409-415`) are not covered either.
  - **The payload rule is ignored.** §7.3 sends `Rule: "unit-release"` inside `EventPayload`, but `moveTaskWithLocked` overwrites `Rule` with its `rule` argument (`runner.go:474`). The held-release rule that metric 2 reads exists only if it is passed as the argument.
  - **An empty actor becomes "x".** `actorAlias("")` is `sanitizeIDPart("")`, which returns `"x"` (`prflow.go:124-141`, `:524-529`). A move with no session (auth off, dev) would be recorded as made by "x". The caption "unchanged byte for byte" promise (`runner.go:449-452`) also breaks for every existing move test.
- Recommendation:
  - Pass the actor through `dispatch` for `MoveDispatch`, or narrow the BR-48 text to "every column move except dispatch".
  - Pass `"unit-release"` as the `rule` argument.
  - Attribute only when `mv.Actor != ""`.
  - List the existing move-caption tests that change.

### A rate-limited tick can time out every unit job before it reports - Severity: Major
- Affected scope: §4.3 budget table; §5.6 "The bound, counted"; §14 "Runner latency"
- Finding and rationale:
  - `poll` charges wall time since the last poll as active time (`qajobs.go:404-413`). The start phase of the previous tick runs inside that interval.
  - §5.6 states a fully rate-limited tick takes about 6½ minutes.
  - A unit job's budget is `min(env, timeLimitSec + 5 min)`, with `env` clamped down to 5 and `timeLimitSec` as low as 60 s. With the default `env` = 30 and an attested 60 s, the budget is 6 minutes.
  - One such tick therefore ends every in-flight unit job FAILED `qa-job-timeout`, cancels it and sends one notice per card. The runner's own GitHub waits cause this. §14 names the effect on e2e jobs and not on unit jobs.
- Recommendation: Set a floor on the unit budget above the worst-case tick (for example `max(…, 10 min)`), or charge only the time since the previous poll's job read, not the start phase. State the result in §14.

### Boot `Reconcile`'s new FAILED move is not persisted with its event on ArcadeDB - Severity: Major
- Affected scope: §4.4 "Boot reconciliation" (`qaRunner` false); §5.4 ("Every return … carries `Floor`"); TC-868 (not marked (A))
- Finding and rationale:
  - `ArcadeStore.Reconcile` returns the projection's `Result` without persisting it (`store_arcade.go:393-394`).
  - The requeue loop at `runner.go:705-721` does not call `publish`; only `recoverTurns` goes through `onWrite`.
  - `main.go:295-301` only logs the result.
  - So the `qa-runner-absent` FAILED move, and its `TaskMoved`, reach the graph only if a later mutation happens to carry the floor. The event itself is never written. The design relies on Floor-in-the-same-commit everywhere else but says nothing about this path, and TC-868 runs on MemStore only.
- Recommendation: After the projection call, have `ArcadeStore.Reconcile` persist the returned `Result` through `persistBackground`. That is outside `mutate`, so it avoids the deadlock described at `store_arcade.go:387-392`. Mark TC-868 (A), with a round-trip of the FAILED status and its event.

### A build defect's fix is refused whenever the build failure is in a test file - Severity: Major
- Affected scope: §7.5 "A unit fix may not weaken the failing tests" (`fixGuard{Unit: true}`, empty `Protected`, `TestGlobs` edits refused); TC-873
- Finding and rationale:
  - `checkFix` requires that some test file is added or edited (`autofixcheck.go:354-360`). For any unit defect the guard refuses edits to `TestGlobs` packet files.
  - A common `build-failed` cause is a test that no longer compiles after an API change. The only correct fix edits that test, and the guard refuses it.
  - Each such defect then spends a triage turn and a fixer turn and ends blocked. There are no failing test names to protect in a build defect, so the guard protects nothing here.
- Recommendation: For the `~build` defect, allow edits to test files and rely on the draft pull request and its human reviewer. Alternatively, state that build defects are never auto-fixed and leave their fixer card for a person. Test whichever choice is made in TC-873.

### `trimUnitMoves` is not on "every board projection" - Severity: Nit
- Affected scope: §7.3 "The board offers only what the store allows"; TC-870
- Finding and rationale: The list misses two paths:
  - `renderBOAfter(…, "board")` at `handlers_questions.go:86`.
  - The `buildBO` → `buildBoardView` path reached by `GET /products/{id}?tab=board`, `/ui/bo?…&tab=board` and `/ui/bo/{view}` (`handlers_bo.go:138-185, 386-389`; `urls.go:382, 394`).
  
  The store guard still refuses the move, so this is a display gap. But the claim "the single place every board projection goes through" is false, and TC-870 asserts it.
- Recommendation: Call the trim once inside `buildBoardView` (`board_view.go:208`), plus `handleBoard` and `handleBoardPaged`, instead of listing callers.

### §2 row 6 omits the BR-48 clarification - Severity: Nit
- Affected scope: §2 row 6 versus §0 and §10
- Finding and rationale: Row 6 lists "a clarification of BR-44". §0, §10 and the delegation record also list BR-48.
- Recommendation: Add BR-48 to row 6.

### §12 misses two `Result` fields - Severity: Nit
- Affected scope: §12 `store.go` row
- Finding and rationale: §5.4 and §7.4 introduce `Result.UnitMarkerValue` and `Result.UnitReleasedClosed`. The row lists only `UnitPlanned`, `UnitMarker` and `UnitHeldPassed`.
- Recommendation: List all five.

### Legacy task routes: four routes, "all three" tested - Severity: Nit
- Affected scope: §7.3 row "Legacy task routes"
- Finding and rationale: The row names `dispatch|approve|retry|cancel` (`main.go:559-562`). It then names three store methods and says TC-870 "calls all three". `approve` is not covered.
- Recommendation: Name the approve handler's store call and include it in TC-870.

### The BR-54 amendment calls a BLOCKED move "final" - Severity: Nit
- Affected scope: §10 BR-54 amendment
- Finding and rationale: The capability answer moves a card to BLOCKED `ReasonUnitAttest` (§5.3), which is not a final status. The amendment says it "may set the final status and reason".
- Recommendation: Write "may set the status and reason of that job's own `unit:qa` card".

### OQ-40 is used as a residual but not amended - Severity: Nit
- Affected scope: §10 D-105 residual "OQ-40"; §14
- Finding and rationale: OQ-40 says "What it blocks: nothing today" (`open-questions.html:942-949`). With this design, every unit fix adds a model-written test file that CI runs on a same-repository branch. That is a new, standing trigger for the question.
- Recommendation: Add OQ-40 to the §10 amendments, noting unit fixes as a new case.

### The UI/UX source and stylesheet are not anchored - Severity: Nit
- Affected scope: §4.1 "UI/UX"
- Finding and rationale:
  - The delegation names `https://emilkowal.ski/skill` (`delegation.md:14`). The design cites `emilkowalski/skills`, `emil-design-eng` with no anchor for that mapping.
  - The pattern exists (`static/app.css:1718-1719`), but `dashboard/static/app.css` is not in §12, and the section needs classes.
- Recommendation: Anchor the mapping, and add `app.css` to §12 or name the existing class the section reuses.

### The Status line is a changelog - Severity: Nit
- Affected scope: Header "Status"
- Finding and rationale: One paragraph lists ten rounds of report names. It repeats §15–§23 and hides the one fact a reader needs: rev 10, awaiting round 10.
- Recommendation: Reduce it to the current revision, the scope cut and a pointer to §15–§23.

---

**Solid in this revision:**
- Register numbering (D-105, BR-64, TC-851…TC-875) and the target-count arithmetic.
- The `Reconcile` hoist, the compile-time assertions and the four test callers.
- The `qaToolKey` and `qaToolDriven` split, keyed on fields only the planner writes.
- The held-card BLOCKED-path table, apart from the gaps above.

Counts: B=0 M=8 N=8
