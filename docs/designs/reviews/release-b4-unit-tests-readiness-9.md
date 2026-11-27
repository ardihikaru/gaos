# Readiness review: `docs/designs/release-b4-unit-tests.md` (rev 9)

The design is not ready to implement. The boot path for cards whose QA client is unusable cannot be built as written (one Blocker), and nine decisions or file-list gaps would give materially different builds (nine Majors). Most of the rest is solid; the Nits are mostly text and cross-reference slips.

**Checked and solid:**
- **Identifiers:** D-105 follows D-104 (`decision-log.html:2153`). TC-851 is free after TC-850 (`qa-test-plan.html:941`).
- **Counts:** 25 cases (3+4+5+7+5+1). Go tests 713 → 738, and 688 → 713 in `dashboard/` (`README.md:143`, `deploy/check-workspace.sh:114-130`). Nine defect properties are named and listed.
- **Call sites:** the 17 `checkFix` calls in `fixpr_test.go` are right, and so are the `fixEligible` callers (`autofixpr.go:166`, `:490`).
- **Delegation anchor:** it exists at `docs/designs/reviews/release-b4-unit-tests-delegation.md:14`.
- **Phases:** every phase has an exit command and an observable exit condition.

---

### The boot path for "no usable QA client" cannot be built as written - Severity: Blocker
- Affected scope: §4.4 "Boot reconciliation"; §2 row c; §5.6 "Stale cards"; §12 rows `dashboard/runner.go`, `dashboard/main.go`, `dashboard/store_arcade.go`; TC-868.
- Finding: Four separate problems:
  - **`srv` does not exist yet.** `Reconcile` runs at `main.go:292-302`, but `srv` (and `qaOut: qaOutboundFromEnv()`) is created at `main.go:368-369`. "Passed `srv.qaOut.usable()` from `main.go:292-302`" will not compile. One engineer moves `Reconcile` below the server setup, which may be after the control loop starts at `:304`. Another reads the env a second time.
  - **The cards are no longer RUNNING when the else branch looks.** With `qaRunner == false`, the design only says what happens when it is true. Today's loop (`runner.go:705-718`) then moves every RUNNING card with no open turn to READY. That includes every `qaToolDriven` card, with a `TaskResumed` event. So the else branch at `main.go:439-444` (which is new; there is no `else` today) finds nothing RUNNING to end "FAILED `qa-runner-absent`". The e2e cards end up READY with no runner, which is exactly the stranding the design says it prevents.
  - **No store call is named for that move.** For an e2e card it would presumably be `ResolvePRCard` RUNNING → FAILED, but no rule name is given.
  - **The boot hook can silently stop running.** `main.go:292-294` finds `Reconcile` through an anonymous interface, `Reconcile(context.Context) (Result, error)`. If the signature changes and that interface literal does not, the type assertion fails quietly and boot reconciliation never runs again. `ArcadeStore.Reconcile` (`store_arcade.go:393-394`) must change too; neither is in §12. The test callers (`events_test.go:511`, `taskrun_test.go:357`, `turns_test.go:70`, `:92`) appear only as "the Reconcile callers in tests".
- Recommendation:
  - Decide where the FAILED move happens. Either `Reconcile(ctx, false)` itself ends `qaToolDriven` RUNNING cards FAILED `qa-runner-absent` (name the store call and rule), or `Reconcile` skips them in both modes and the else branch ends them.
  - Compute `usable` before line 292, for example `qaOut := qaOutboundFromEnv()` passed into the `server` literal later.
  - List the anonymous interface at `main.go:292-294`, `store_arcade.go:393` and the four test call sites in §12.
  - State the phase that builds the FAILED path; today `Reconcile` is in phase 2 and TC-868 is in phase 4.

### Two existing tests break and are not in the change list; `apiDispatchAnswer`'s label branch is undecided - Severity: Major
- Affected scope: §2 rows d and f; §4.2 `apiDispatchAnswer` row; §12 "existing tests that change".
- Finding:
  - `board_page_test.go:771` sets `e2e:run` on seed card `bt-00003`, which has no QA-runner key, and expects 200 with the sheet (`:783`). The new refusal "an `e2e:*` label on any card whose key is not a QA-runner key" makes this test fail.
  - `api_tasks_test.go:486-487` (`TestAPIDispatchAnswer`) expects `qa-tool` for cards with an e2e label and no `DedupKey`. §2(f) says the answer comes "by the card's key, not its label". §4.2 only adds the key rule and does not say whether the label case (`api_tasks.go:103`) is removed.
  - Neither file is in §12, so a write allowlist built from §12 refuses the fix.
- Recommendation: State that `apiDispatchAnswer` drops the label case, or keeps it. Add `dashboard/board_page_test.go` (choose another label, or give the card a runner key) and `dashboard/api_tasks_test.go` (the two cases change) to §12, with the new expected values.

### `planUnitQASpecLocked` has no stated contract - Severity: Major
- Affected scope: §4.2 "One card per merge across the switch"; §9 phase 2.
- Finding:
  - It is described as a lookup called *before* planning that "answers the existing card". It is also said to write `planned:<epoch>` "right after `planCardsLocked` returns it". One function cannot do both unless it also calls `planCardsLocked`, which in `planPullRequestCardsLocked` (`prflow.go:309-314`) takes a batch of items.
  - The signature `(m, productID, owner, repo, n)` has no return type and no `now`. `PlanPullRequestCards` has no clock argument; `PlanAPICard` has `now`.
  - It is not said whether it replaces the exact-key loop at `prflow.go:240-246` for the unit spec.
- Recommendation: Split it into two helpers with full signatures, for example:
  - `unitQACardLocked(productID, owner, repo string, n int) *TaskRecord`, the two-key `EqualFold` lookup that replaces the `:240-246` check for the unit spec;
  - `stampPlannedLocked(id string, now int64)`, called after `planCardsLocked` by both planners.

  Name the clock each planner uses.

### Stamping a card that has no `planned:` has no store write path - Severity: Major
- Affected scope: §5.6 last paragraph; §5.5 table.
- Finding:
  - `endStaleUnitQACards(ctx, st Store, nt, now)` must stamp a PENDING or READY card that lacks `planned:` with the current time. No `Store` method writes a reason on a PENDING or READY card: `ResolveUnitQACard`'s table has no PENDING→PENDING or READY→READY row, and `ResolvePRCard` has no such move.
  - A READY card after a retry carries `unit retry since:<epoch>`, not `planned:`. The sweep would therefore stamp over it, losing the 24-hour `since` clock, and the design does not say whether that is intended.
- Recommendation: Add a row such as "PENDING/READY → same; `BlockReason` = `planned:<now>` (rule `unit-stamp`, no event)". Alternatively, have the sweep read `since:` as the clock for READY retry cards. State which.

### "`pullFiles` page 1 only" is not what the existing method does; the 19-call bound is wrong - Severity: Major
- Affected scope: §5.6 "The bound, counted"; §7.5; §5.1 `unitSource`; §14 runner latency.
- Finding: `unitSource.pullFiles` is "satisfied by `*ghClient`". But `ghClient.pullFiles` (`github.go:228-242`) pages up to `ghMaxPages = 30` (`github.go:225`). One context build can therefore make up to 42 calls (30 pages + 12 file reads), not 13. The tick bound becomes 48 calls, not 19, and the "about 6½ minutes" worst case grows accordingly. "Page 1 only" needs a new method or a parameter, and neither is named.
- Recommendation: Add `pullFilesFirstPage(ctx, owner, repo, n)` to `github.go` (in the §12 row) and to `unitSource`. Alternatively, restate the bound with 30 pages.

### The packet slot split and the "read-only reference part" cannot be built by reusing `buildReviewContext` - Severity: Major
- Affected scope: §7.5 first paragraph; §9 phases 4 and 5.
- Finding:
  - `buildReviewContext` (`reviewdefect.go:319-347`) fills files in path order up to `defectMaxFiles`, under one byte budget (`total+len > defectMaxFileBytes`). It cannot reserve 3 slots and half the bytes for non-test files.
  - `DefectContext` / `DefectContextFile` have no field marking a "read-only reference part". `checkFix` refuses edits by `protected` and by `TestGlobs`, not by part.
  - Possible builds: two calls with separate budgets, a new builder, or a new field. Each gives different packets and a different schema.
  - The work also appears in two phases: `buildUnitContext` in phase 4, "the packet slot split" in phase 5.
- Recommendation:
  - Specify the algorithm. For example, call `buildReviewContext` once for non-test paths with max 6 files and the full budget, then once for test paths with `6 − n` files (at most 3) and the remaining budget, then concatenate.
  - Say how the reference part is represented, for example a separate heading in `renderUnitDefectPacket` driven by `UnitFiles`, with no new field.
  - Build it in one phase.

### `checkFix` cannot tell that a build-failure defect is a unit defect - Severity: Major
- Affected scope: §7.5 "A unit fix may not weaken the failing tests".
- Finding: The signature only gains `protected []string`, and "for a unit defect" is the trigger for refusing `TestGlobs` packet edits. A `build-failed` defect has no failing tests, so `UnitFiles` is empty and the split gives nil. That is indistinguishable from the `nil` passed for other kinds. One engineer adds a flag; another treats a non-nil empty slice as "unit". The second breaks silently through `strings.Split` or serialisation.
- Recommendation: Add an explicit `unit bool` parameter, or pass a struct `fixGuard{Unit bool; Protected []string}`. Add a build-defect case to TC-873.

### The refusal marker grammar and its final text are contradictory - Severity: Major
- Affected scope: §5.4 reason grammar `[unit:<h8|pass|refused>]` against adoption ("`<why>` read from the marker `unit:refused-<rule>`"); §6 last paragraph; §7.2 step 0; §4.1 switch-off.
- Finding:
  - The marker is both `unit:refused` and `unit:refused-<rule>`. `<why>` values contain spaces ("no fixer assigned"), so they cannot sit inside a space-delimited token. No mapping from rule slug to text is given.
  - The final texts disagree. The live path gives `<total> unit test(s) failed; no defect filed (<why>)`. Adoption gives `unit tests failed; no defect filed (<why>)`, with no total. Gate step 0 gives `unit tests switched off`.
  - So "one text per outcome" (claimed fixed in round 8) still does not hold.
- Recommendation: Define the slug set, for example `off`, `no-fixer`, `no-triager`, `autofix-off`, `not-target`, `unit-cap`, `defect-cap` and `older`, with a table to the `<why>` text. Write `unit:refused-<slug>`. Store `Total` in the marker or drop it from the refusal text. Use one switch-off text in all three places.

### The unit defect's identity puts `@` into events, against TC-870 - Severity: Major
- Affected scope: §7.1 `CaseID` = `unit:<owner>/<repo>@<base>@<h>`; TC-870 "no `@` in any event".
- Finding: A unit defect has no `DefectID`, so `d.Ref()` returns the `CaseID` (`defects.go:126-131`). `DefectReported` text is `"defect reported: "+d.Ref()` (`defects.go:533-535`), and `setDefectStateLocked` text and payload carry `d.Ref()` / `d.Key` (`defects.go:353-354`). So every unit defect event contains `@`, and TC-870 as stated cannot pass. One engineer changes the separator; another narrows the assertion to actor fields.
- Recommendation: Pick a separator that is not `@` (for example `unit:<owner>/<repo>~<base>~<h>`, after checking `baseBranchRe` refuses `~`). Alternatively, restate TC-870 as "no e-mail address or local part in any event's actor or text".

### The delegation record tells the operator the wrong D-091 (8) amendment - Severity: Major
- Affected scope: §0 "Delegation"; §10 D-091 (8); `docs/designs/reviews/release-b4-unit-tests-delegation.md:33-34`.
- Finding:
  - D-091 (8) is the "backend only" eligibility rule (`decision-log.html:1972-1973`), and §10 amends it to "a unit defect counts as backend". The delegation file's step-8 list instead says "D-091 (8): a unit defect's triage card is born BLOCKED and held for an auto-fix admin".
  - The file the approval relies on therefore misdescribes an amendment the operator is meant to be able to reverse.
  - It also leaves out the BR-44 clarification.
- Recommendation: Correct the delegation file's D-091 (8) line to the §10 wording before the status changes, and add the BR-44 clarification line.

### §2 row c names the wrong test - Severity: Nit
- Affected scope: §2 table row c.
- Finding: The row cites TC-867 (the outcome table). §4.4 and §9 put the `Reconcile` boot branches in TC-868.
- Recommendation: Change it to TC-868.

### §22 cites a different anchor line from §0 and D-105 - Severity: Nit
- Affected scope: §22 row "Delegation anchor".
- Finding: It says "§0 and D-105 cite `:17`", but they cite `:14`, which is the correct line.
- Recommendation: Change it to `:14`.

### The D-105 text states the reconciliation rule twice, once unconditionally - Severity: Nit
- Affected scope: §10 D-105 bullet.
- Finding: "boot reconciliation leaves runner-driven cards RUNNING" appears once without a condition, then again "only when the QA client is usable". The implementer copying it into the register gets a self-contradiction.
- Recommendation: Delete the first, unconditional clause.

### `qaTracked` has no field for the marker, so the "finished" rewrite drops it - Severity: Nit
- Affected scope: §5.4; §12 `qajobs.go` row.
- Finding: Adoption sets `unit` on `qaTracked`, and every rewrite builds the reason from `qaTracked`. The §12 field list (`timeout/sha/mergedAt/base/unitDone/unitText`) has no marker field. On the live path, nothing copies the store's marker into `qaTracked`. So the "finished" rewrite at `qajobs.go:429` would drop `unit:`.
- Recommendation: Add `qaTracked.unit string`, set from the `PlanUnitDefect` / `PassUnitDefects` result.

### There is no back-office control for "an admin confirms the close" - Severity: Nit
- Affected scope: §5.7 last row; §7.3; §7.4.
- Finding: The Auto-fix page has no close-defect control (`autofix_view.go`, `templates/backoffice.html`). The only route is `POST /products/{id}/defects/{ref}/close`, and a unit `ref` is a `CaseID` containing `/`. The workable path is the admin's board cancel, which closes the defect (§7.3), but the notice does not say so.
- Recommendation: Have the notice and §7.4 say "cancel the held card on the board". Alternatively, add a close link that URL-escapes the ref.

### "Mapped" is decided twice, in the handler and in the store - Severity: Nit
- Affected scope: §4.2 API table rows `runner-off`/`qa-off` and `dispatch` default.
- Finding: The handler's exemptions read `s.store.Master` (`api_tasks.go:181`). The store's `apiJob` reads `masterPtr` under the lock. If the switch flips between the two, an unmapped `unit:test` card can be planned past `runner-off`, with `dispatch` false.
- Recommendation: Have `PlanAPICard` refuse when its own mapping differs from the handler's (pass the handler's decision in `APICardIn`).

Counts: B=1 M=9 N=6
