# Goldfish critic review: release-b4-unit-tests.md (rev 9)

The problem statement, scope cut, alternatives, non-goals and register numbering hold up. Most anchors are right: `prflow.go:88-91`, `labels.go:179-188`, `main.go:490-491`, `qajobs.go:303/350/355-361/465`, `board.go:236-237/452/479`, `store_mem.go:2410-2436`, `defects.go:59/61/68/288/590-608`, `defecttriage.go:78/142/143`, `reviewdefect.go:97-118/319`, `store_arcade.go:960/2329-2335`, the BR-38/44/54/61 text in `brd.html:1298-1352`, and D-091 (8) at `decision-log.html:1972`. The counts add up: 713+25=738, 688+25=713, 19 labels become 20, 9 schema properties, TCs 3+4+5+7+5+1=25.

The weak point is boot recovery. One more thing runs through several findings: the design says a person reads the held defect and decides, but the mark and the metrics that support that decision are not reliable.

### Boot reconciliation cannot be wired as written, and its no-runner branch never fires - Severity: Blocker
- Affected scope: §4.4 "Boot reconciliation", §3 step 5, §2 row (c), §12 `main.go` and `runner.go` rows, TC-868
- Finding and rationale:
  1. **Wrong order.** The design passes `srv.qaOut.usable()` into `Reconcile` "from `main.go:292-302`". But `srv`, and with it `qaOut: qaOutboundFromEnv()`, is only built at `main.go:368-369`, after the `Reconcile` call at `main.go:295`. The value does not exist at that point.
  2. **Moving the call is not free.** Moving `Reconcile` below line 368 puts it after the scheduler loop starts (`main.go:313-330`). The scheduler could then act on stale DISPATCHED/RUNNING cards before recovery.
  3. **The no-runner branch is dead.** With `qaRunner == false`, the stated semantics are "today's requeue". `Reconcile` moves every RUNNING card with no open turn to READY (`runner.go:705-717`), and `moveTaskLocked` keeps the `qa-job:` reason (`runner.go:457-478`). When the new `else` branch later looks for RUNNING `qaToolDriven` cards to end them FAILED `qa-runner-absent`, there are none.
  4. **Consequences.** TC-868's "unusable → RUNNING QA-runner cards end FAILED" cannot pass. Those e2e cards stay READY with no runner, which is the stranding the design says it removes. Unit cards are then "stamped" by the sweep, which overwrites their `qa-job:` reason (§5.6).
- Recommendation:
  - Compute `qaUsable := qaOutboundFromEnv()…usable()` before `main.go:292` and reuse that client for `srv`, or hoist the construction.
  - Make `Reconcile` **always** skip `qaToolDriven` cards.
  - Put the FAILED `qa-runner-absent` end either inside `Reconcile` when `!qaRunner`, or in the `else` branch, but over cards that `Reconcile` left untouched.
  - Restate TC-868 to match.

### The `Reconcile` signature change can silently switch boot recovery off in production - Severity: Major
- Affected scope: §4.4, §12 `runner.go` row
- Finding and rationale: `main.go:292-294` finds `Reconcile` through an anonymous interface assertion, `Reconcile(context.Context) (Result, error)`. Production uses `ArcadeStore.Reconcile` (`store_arcade.go:393-394`), which forwards to the projection, and §12 does not list that change. If `MemStore`'s method or the interface literal changes without `ArcadeStore` (or the other way round), the assertion answers `ok=false`. Boot reconciliation is then skipped with no error. `MemStore` tests would still pass (`turns_test.go:70`, `taskrun_test.go:357`, `events_test.go:511`).
- Recommendation: Name the interface literal at `main.go:292-294` and `ArcadeStore.Reconcile` in §12. Add a compile-time assertion such as `var _ interface{ Reconcile(context.Context, bool) (Result, error) } = (*ArcadeStore)(nil)`.

### Success metric 1 cannot be computed from the event log - Severity: Major
- Affected scope: §0 "Success metrics"
- Finding and rationale: The metric picks `TaskMoved` DONE events "whose reason starts `unit tests passed`, `no unit tests`…" and excludes those ending `merged into a branch…`. `TaskMoved` does not carry the card's `BlockReason`. Its payload `Reason` is `from + "→" + to` (`runner.go:474`), and its text is `"%s: %s (%s → %s)"` (`runner.go:470`). Round 8 flagged the metric as not computable, and rev 9's fix ("reason prefixes named") points at data the event does not have.
- Recommendation: Either add the outcome to the payload of the final unit move without adding an event type (BR-05), for example `Rule: "unit-passed|unit-failed|unit-no-tests|unit-not-base"`, or measure from a store read of card reasons and say so.

### A held defect marked "passed" keeps the mark after it fails again - Severity: Major
- Affected scope: §7.2 dedup, §7.4 "Passed" (held branch), §5.7 last row
- Finding and rationale:
  1. A held defect whose watched tests pass at M2 gets `UnitPassedAt = M2` and the head line `watched tests passed at <sha12>`, once.
  2. If merge M3 > M2 fails the same set, dedup updates only `LastSeen`, `RunRef`, `UnitSHA`, `UnitMergedAt` (§7.2). `UnitPassedAt` and the "passed" line stay.
  3. The admin sees "its failing tests passed" and closes a defect that is failing at the newest merge.
  4. The next failure then reopens it as a regression, which is never auto-fixable (`autofix.go:175-177`, D-091 (8) "not a regression").
  5. A later real pass does not notify again either, because `UnitPassedAt` is no longer zero.
- Recommendation: On a dedup hit with `in.MergedAt > d.UnitPassedAt`, reset `UnitPassedAt = 0` and remove the head mark line, so a later pass marks and notifies afresh. Add this to TC-861.

### The packet slot split cannot be built by "reusing `buildReviewContext`", and the "read-only reference part" has no representation - Severity: Major
- Affected scope: §7.5 first paragraph, "A unit fix may not weaken the failing tests"
- Finding and rationale:
  - **Slots.** `buildReviewContext` takes paths in order, stops at `defectMaxFiles`, and shares one 64 KiB `total` across files (`reviewdefect.go:326-341`). With "the distinct failing files first", test files take the first slots and the byte budget. "At least 3 slots and half of the byte budget for non-test files" is not something this function can do.
  - **Reference part.** `DefectContextFile` is `{Repo, Path, SHA, Text}` (`defecttriage.go:156-161`), and `checkFix` addresses the packet as `map[string]DefectContextFile` by path (`autofixcheck.go:275, 329`). No field marks a file as reference-only, and the stored context has no second part. Two engineers would build different storage and rendering.
- Recommendation: Specify a two-pass build: non-test paths with ≤ 3 slots / ≤ 32 KiB reserved, then test paths into what remains. Add a field such as `DefectContextFile.Reference bool`, persisted in the context JSON, that `renderUnitDefectPacket` renders separately and `checkFix` refuses. Or state that `protected` plus `TestGlobs` is the only enforcement and drop the "separate part" wording.

### After retention, an open unit defect can never be pass-closed, contradicting BR-64 - Severity: Major
- Affected scope: §7.1 `UnitTests`, §7.2 dedup, §7.4, §12 `fixprpoll.go` row, BR-64 text in §10
- Finding and rationale:
  1. `ExpireExcerpts` will empty `UnitTests` 30 days after `LastSeen` (§12; today it is `fixprpoll.go:490-502`).
  2. If the failure comes back on the **open** record, dedup refreshes neither `UnitTests` nor `Excerpt` (§7.2).
  3. Empty `UnitTests` means "never pass-closed" (§7.4). The defect then stays open for good, outside the watch list, with an empty excerpt for the admin to read.
  4. BR-64 says "When every failing test of a defect passes at a later merge on the same base, the defect closes."
- Recommendation: On dedup, refill `UnitTests` and `Excerpt` from `in` when they are empty, or treat an expired open defect as final so the next failure reopens it.

### The delegation record and the design disagree on what the operator is told - Severity: Major
- Affected scope: §0 "Delegation", §10 D-091 (8), `docs/designs/reviews/release-b4-unit-tests-delegation.md:33-34`
- Finding and rationale:
  - **D-091 (8).** The record that feeds the step-8 summary says the amendment is "a unit defect's triage card is born BLOCKED and held". §10 says it is "a unit defect counts as backend by decision". The operator would be asked to keep or reverse an amendment the design does not make. The record also omits the BR-44 clarification that D-105 carries.
  - **Policy gap.** The policy has no delegation clause: "The operator … approve the design" (`gaos-design-review.md:34`, `:89-90`). Four BR amendments go through on an agent-written record of an instruction.
- Recommendation: Make the record's amendment list match §10 word for word. Have the step-8 summary quote the actual BR-38/54/61 and D-091 (8) texts from §10, so the operator confirms the amendments themselves, not paraphrases.

### The unit-test queue has no upper bound, and "queueing costs nothing" is not justified - Severity: Major
- Affected scope: §4.2 "No planning cap", §5.6, §13 row "Cap unit runs at planning", §14 "Many merges at once"
- Finding and rationale:
  - **No bound while a runner runs.** Every fix-target merge plans a PENDING card. Cards are swept only when GitHub or the runner is absent. Nothing bounds the backlog while a runner runs: 3 concurrent runs, up to 60 minutes each, at most 2 starts per tick.
  - **Per-tick cost grows with history.** Every 15 s, `Tick` clones **every** keyed card of every product (`PRCards`, `prflow.go:714-724`). `cardCanceled` clones them all again for **each** tracked job (`qajobs.go:482-489`). Up to 20 unit jobs per product are allowed, and DONE unit cards accumulate on every merge, so the cost per tick grows with history and with `GAOS_UNIT_QA_OPEN`.
  - **Stale commits.** A queued card runs a merge commit that may be days old. The out-of-order machinery (§7.2) exists largely because of this.
  - **Missing alternative.** "Coalesce: run only the newest queued merge per repository and base, and DONE the superseded ones" is not considered in §13.
- Recommendation: Add a per-product queue bound, or coalesce per (repository, base): cancel an older PENDING card as `superseded by a newer merge; nothing ran` when a newer one is planned. Put the coalescing alternative in §13. Replace "costs nothing" with the per-tick read cost stated as a risk.

### The new `e2e:*` refusal strands cards that already carry such a label - Severity: Major
- Affected scope: §4.2, §2 row (d) ("so no card is stranded"), §4.4 key-only `Tick`
- Finding and rationale: Today any card with `e2e:automation` or `e2e:run` is started by `Tick` (`qajobs.go:252-262`) and refused by the scheduler (`labels.go:182`). After this release, `Tick` drives only QA-runner keys, and "labels already on such cards stay and do nothing". A live card with an operator-added `e2e:run` on a non-runner key (for example a `:unit-test` card) is then neither started nor dispatched, and it is not cancelled. The claim "no card is stranded" is false for existing data, and this change applies with the switch off.
- Recommendation: Add a one-off boot pass, or a refusal note, that clears `e2e:*` from non-runner-key cards. Alternatively, make `labelsAllowDispatch` ignore `e2e:*` on non-runner keys so those cards go back to agent dispatch. State which one.

### The triage table cites the wrong delegation line - Severity: Nit
- Affected scope: §22 row "Delegation anchor"
- Finding and rationale: The row says §0 and D-105 cite `:17`. They cite `:14`, which is correct: the verbatim words are at `delegation.md:14`.
- Recommendation: Change `:17` to `:14`.

### §2 row (c) names the wrong test - Severity: Nit
- Affected scope: §2 row (c)
- Finding and rationale: Row (c) names TC-867 (`TestUnitOutcomeTable`). The `Reconcile` branches are tested by TC-868 (§4.4, §9).
- Recommendation: Change it to TC-868.

### "The `else` branch (`main.go:439-444`)" does not exist yet - Severity: Nit
- Affected scope: §4.4, §5.6
- Finding and rationale: `main.go:439-444` has no `else`. The branch is new. Also, `endStaleUnitQACards` needs `nt`, which is built at `main.go:429`, so it cannot run "at boot after `Reconcile`" (line 295). It must run at or after line 444.
- Recommendation: Say "a new `else` branch after `main.go:444`" and drop "after Reconcile".

### `usable()` is described incompletely - Severity: Nit
- Affected scope: Header "Anchors", §4.1
- Finding and rationale: The design says `usable()` checks non-nil and empty `why`. It also checks `o.base != nil` (`qaclient.go:393`).
- Recommendation: Quote the full predicate.

### TC-870's "no `@` in any event" contradicts the unit CaseID - Severity: Nit
- Affected scope: §7.1 `CaseID`, §9 TC-870
- Finding and rationale: `CaseID = unit:<owner>/<repo>@<base>@<h>`, and `Defect.Ref()` returns `CaseID` when there is no `DefectID` (`defects.go:126-131`). Every `DefectReported` and `DefectStateChanged` text, built with `d.Ref()` at `defects.go:354`, therefore contains `@`. The same goes for the triage card title (`defecttriage.go:103`).
- Recommendation: Narrow the assertion to "no e-mail address", or pick a separator other than `@` for the CaseID.

### A changed `unit` result after `unitDone` has two answers - Severity: Nit
- Affected scope: §6 "A result is final", §5.4 last sentence
- Finding and rationale: A later poll whose `unit` differs ends the card FAILED `qa-unit-unreadable`. But "after `unitDone` … a job failure end[s] the card DONE with `unitText`". A defect may already be filed at that point. Which rule wins is not stated.
- Recommendation: State the precedence, for example: after `unitDone`, a changed `unit` cancels the job and ends the card DONE with `unitText` plus a log line.

### Wrong anchor for the status walk - Severity: Nit
- Affected scope: §5.5 body step (4)
- Finding and rationale: "as `MoveCard` does (`board.go:390-394`)" is wrong. `MoveCard` makes one `moveTaskLocked`. The `statusPath` walk is in `resolveCardLocked` (`prflow.go:646-658`).
- Recommendation: Re-anchor to `prflow.go:646-658`.

### The operator's UI/UX instruction is not addressed - Severity: Nit
- Affected scope: §4.1 "Unit tests" section of the Auto-fix page
- Finding and rationale: The delegation (`delegation.md:14`) also says "anything about UI/UX, adopt this `https://emilkowal.ski/skill`". The design adds a back-office section and never mentions this.
- Recommendation: State whether and how the new section follows that instruction, or record why it does not apply.

### BR-48 is now stale - Severity: Nit
- Affected scope: §10
- Finding and rationale: BR-48 says "the other board moves are not attributed yet" (`brd.html:1331`). This design attributes held-card release and cancel (`unit-release`, actor alias), and BR-48 is not amended.
- Recommendation: Add a one-line BR-48 amendment, or record it as a clarification in D-105.

### The sweep's "stamp" overwrites reasons the runner depends on - Severity: Nit
- Affected scope: §5.6 "A `unit:qa` card with no parseable `planned:` … is stamped"
- Finding and rationale: A READY card with `unit retry since:<epoch>`, a READY card with a requeued `qa-job:` reason, or a drift-released card still carrying the BLOCKED text (`workspace.go:395-397` keeps the reason) has no `planned:`. Stamping it replaces the `since:` clock or the job id.
- Recommendation: Stamp only PENDING cards whose reason is empty, or add `planned:` as one more field and keep the existing reason.

Counts: B=1 M=8 N=10
