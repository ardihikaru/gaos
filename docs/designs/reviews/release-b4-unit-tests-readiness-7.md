# Readiness review: `docs/designs/release-b4-unit-tests.md` (rev 7)

Lens: Tech Lead, zero-decision rule. I read the whole design and the two rubric files, and checked the code anchors that matter in `dashboard/`.

**What holds up:**
- **Next-free identifiers.** D-104 is the highest decision, the log reads "One hundred and three entries", TC-849 is the highest case, BR-63 is the highest rule, the newest schema file is `20-release-b.sql`, and `toc.js` is at 0.66.0. So D-105, TC-850…874, BR-64, schema 21 and v0.67.0 are all correct.
- **Counts.** 25 TCs is 3+4+5+7+5+1, and the Go-test progression 712 → 737 follows from them.
- **Phase exits.** Each one is concrete and observable.

### Adoption keeps `sha` but loses `base` and `mergedAt` - Severity: Major
- Affected scope: §5.4 "Adoption"; §5.1 (`qaTracked.sha`, `.mergedAt`, `.base`); §6 poll order; §7.1 `CaseID`; §7.4 `PassUnitDefects`
- Finding: The RUNNING reason carries only `sha:`, `mode:` and `unit:` (§5.4 grammar). When `sha:` is valid, adoption fills `sha` and `unit` from the reason. It re-reads GitHub only "when the reason lacks a valid `sha:`". So after an ordinary restart, `qaTracked.base` and `qaTracked.mergedAt` are empty. Then:
  - `PlanUnitDefect` builds `CaseID` `unit:<owner>/<repo>@@<h>` and stores `UnitBase=""`. The fix re-check (§7.5) will always refuse that base.
  - `PassUnitDefects(base="", mergedAt=0)` matches no defect, so a defect never closes.
- Recommendation: Do one of two things:
  - Add `base:` and `merged:` fields to the `qaReason` struct and the reason grammar.
  - Or state that adoption of a `unit:qa` card always re-reads the pull request as in §5.1, whether or not `sha:` is present.

  Either way, extend TC-867 to assert that the adopted job files the defect under the merged base.

### Phase order: phase 1 and phase 3 use code from later phases - Severity: Major
- Affected scope: §9 lead paragraph ("Each phase uses only its own and earlier phases' code"); §5.5 body step (2); §5.4; §12 `unitdefect.go` row
- Finding: The claim that each phase uses only earlier code fails in two places:
  - **Phase 1 uses a phase 3 constant.** `ResolveUnitQACard` is built in phase 1 and explicitly refuses `BlockReason == ReasonUnitHeld`. But `ReasonUnitHeld` is defined in new `unitdefect.go` (§12), which is phase 3.
  - **Phase 3 uses phase 4 helpers.** `PlanUnitDefect` and `PassUnitDefects` must write the marker reason `qa-job:<id> case:* sha:… mode:unit-run unit:<h8> <text>` (§5.4). Building or parsing that string needs `qaReason`, `qaJobReason(r qaReason)` and `reasonField`, which are phase 4 (§9 phase 4 "`qaReason`").
- Recommendation:
  - Move `ReasonUnitHeld` to `unitqa.go` in phase 1, or move the explicit held check to phase 3.
  - Move `qaReason`, `reasonField` and `sha40Re` to phase 1 (or 3), and update the §9 table and the §12 file rows to match.
  - Remove `ResolveUnitQACard` from the phase 4 "Builds" list, where it still appears even though rev 7 moved it to phase 1.

### The store-side marker rewrite is not specified - Severity: Major
- Affected scope: §5.4 "Act once, across a restart"; §5.5 table row RUNNING → RUNNING; §6 poll order "A store error … retries on the next poll"
- Finding: The store gets only `taskID` and must "rewrite that card's RUNNING reason with `unit:<h8>`". Three things are left open:
  - **How the string is built.** Does the store parse the current reason and re-emit it through `qaReason`? Where does the `unit:` token go relative to the free text? Does it replace an existing `unit:` token?
  - **What happens if the card is not RUNNING.** `resolveUnitQACardLocked` refuses RUNNING → RUNNING on a card that is no longer RUNNING. Does `PlanUnitDefect` then abort the whole transaction, so no defect is filed, or file the defect without the marker? If it aborts, the §6 rule "retries on the next poll" loops for the rest of the job.
  - **Whether the `unit:refused` path writes `Floor`.** It returns `Result{Skipped: …}`, and the design does not say whether `Floor` is set there.
- Recommendation:
  - State the exact algorithm: parse the reason with `reasonField`, set the `Unit` field, rebuild with `qaJobReason`, keep `Text`.
  - State the behaviour when the card is not RUNNING, for example: "the defect is still filed; the marker is skipped; the runner sets `unitDone` from the returned result".
  - Add both cases to TC-860.

### The 24-hour "no runner" cancel has no clock and no call site - Severity: Major
- Affected scope: §5.6 "No runner"; §5.5 table rule `unit-no-runner`; §12 `main.go` and `qajobs.go` rows
- Finding: The rule says "at boot GAOS ends every `unit:qa` card PENDING for more than 24 hours". There are two gaps:
  - **No timestamp.** `TaskRecord` (`dashboard/model.go:851-899`) has no creation or planned timestamp, so "PENDING for more than 24 hours" cannot be computed from the card.
  - **No call site.** The cancel is listed under `qajobs.go` ("the no-runner cancel"). But in this case the runner is never constructed (`main.go:439-444`), and the `main.go` row of §12 does not list a boot call. Nothing says which function runs, where in `main.go`, or before or after `Reconcile` (`main.go:292-302`).
- Recommendation:
  - Name the age source, for example the `TaskPlanned` event time read from the event log, or a new `TaskRecord.PlannedAt` with its schema and persistence impact.
  - Name the function and its call site in `main.go` (the `else` branch of `if srv.qaOut != nil`).
  - Add the change to the §12 `main.go` row and give it a TC assertion.

### The `checkFix` unit rule has no inputs and no defined comparison - Severity: Major
- Affected scope: §7.5 "A unit fix may not weaken the failing tests"; §12 `autofixcheck.go` row; §12 "existing tests that change"
- Finding: `checkFix(r FixReply, packet, af AutoFix, lookup)` (`dashboard/autofixcheck.go:275`) receives no `Defect`. So the rule's inputs and logic are all undecided:
  - **Where the kind and file list come from.** The design says "the files of its `Excerpt`". But `Excerpt` is display text, screened, cut to 4 KiB and ended with `… and <k> more`, so failing files beyond the cut are lost. `UnitTests` stores names only.
  - **What "deletes or modifies any line" means.** Fixes are search/replace `Edits`, not diffs, so this needs a definition in that model.
  - **Where the check lives.** If `checkFix`'s signature changes, about 20 calls in `dashboard/fixpr_test.go` change, and that file is not in the change list. If instead the check goes in the caller (`autofixpr.go:253`), it is a different file.
- Recommendation:
  - Store the kept failing files on the defect (for example `UnitFiles`, schema 21) rather than parsing `Excerpt`.
  - State that any `Edits` entry whose `Path` is in that set is refused.
  - Pick the location: either a new parameter (and list `fixpr_test.go`) or a check before `checkFix` at `autofixpr.go:253`.

### The `UnitBase` re-check cannot see `UnitQA.Bases` or the effective base - Severity: Major
- Affected scope: §7.5 base-site table rows `autofixpr.go:90` and `autofix.go:350-361`
- Finding: The two sites cannot make the check as written:
  - **The gate has no `Master`.** The pre-open gate is `fixEligible(d Defect, taskID string, gen int, af AutoFix, tokenKind string)` (`dashboard/autofixpr.go:76`). It is a pure function with only `AutoFix`, but `UnitQA.Bases` is on `Master.UnitQA`.
  - **The effective base needs a GitHub read.** "The current effective base" requires `repoDefault` when `BaseBranch` is empty (§1). The claim loop (`autofix.go:350`) does not do that read today, and `fixEligible` cannot.
- Recommendation:
  - Specify the new `fixEligible` signature (for example it takes `UnitQAConfig` and a resolved `effectiveBase string`).
  - Say where `effectiveBase` is read in each caller, and what a failed default-branch read does (block, or retry).
  - List the changed `fixEligible` test callers.

### "The five defect properties" do not match the six in schema 21 - Severity: Major
- Affected scope: §12 row `store.go, store_arcade.go, store_arcade_defects.go` ("persist `unitQA` and the five defect properties") against §12 row `21-unit-defects.sql` (six `CREATE PROPERTY`) and §7.1
- Finding: Schema 21 creates `unitSha`, `unitMergedAt`, `unitReopenedAt`, `unitBase`, `unitTests` and `unitNoticeSent`, which is six. The Arcade defect upsert and load use an explicit column list (`dashboard/store_arcade_defects.go:51-54`, `:79-99`). An implementer who follows "five" will most likely drop `unitNoticeSent`, which was added in rev 7. That loses the notice re-send across a restart that §5.4 depends on. TC-861 does not name the field.
- Recommendation: Change the count to "six", name the six fields, and have TC-861 assert that `UnitNoticeSent` round-trips through ArcadeDB.

### The `qaJobReason` signature change breaks an unlisted test file - Severity: Major
- Affected scope: §5.4 "`qaJobReason` takes a struct"; §12 "existing tests that change"
- Finding: `qaJobReason(id, caseCode, text string)` is called in `dashboard/stagingcreate_test.go:138` as well as `qajobs_test.go`. The change list names only `qajobs_test.go`. With a write allowlist taken from §12, phase 4 cannot compile.
- Recommendation: Add `dashboard/stagingcreate_test.go` (the `qaJobReason` call) to "existing tests that change". Alternatively, keep a three-argument wrapper and say so.

### Phase placement of the env parsers is ambiguous - Severity: Nit
- Affected scope: §9 phase 4 ("timeout; … the start cap") against phase 6 ("`unitQATimeoutFromEnv`, `unitQAOpenFromEnv` in compose and check-stacks")
- Finding: TC-865 and phase 4's exit ("four PENDING cards start at most three") need the default of 3 from `unitQAOpenFromEnv`. Phase 6 can be read as building the parsers there.
- Recommendation: State that the parsers are built in phase 4, and that phase 6 adds only compose and `check-stacks.sh`.

### The method that blocks a `unit:qa` card is not named - Severity: Nit
- Affected scope: §5.1 table rows → BLOCKED `ReasonUnitNoCommit`; §5.3 → BLOCKED `ReasonUnitAttest`; §5.5 table
- Finding: `ResolveUnitQACard`'s "allows exactly" table has no → BLOCKED row, so the implementer has to infer `ResolvePRCard` (`resolveFrom[StatusBlocked]` allows PENDING and READY, `prflow.go:603`). The `since:` suffix format is also only implied.
- Recommendation: Name `ResolvePRCard(…, StatusBlocked, ReasonUnitNoCommit+" since:<epoch>")` explicitly.

### Two different cancel texts when the switch is off at start - Severity: Nit
- Affected scope: §5.1 first table row against §5.2
- Finding: `unitQAFor` includes `Enabled`. So a start with the switch off matches both "repository no longer a fix target" (rule `unit-not-target`) and "unit tests switched off" (rule `unit-off`), and the order is not stated.
- Recommendation: Check `Enabled` first and emit the §5.2 text with rule `unit-off`.

### Where `qa-off` applies on adoption is not stated - Severity: Nit
- Affected scope: §4.2 API table row "`apiTaskCreate`, preconditions"
- Finding: Today `qa-off` is checked only on the create path (`api_tasks.go:234`, after the lookup). The design does not say whether a mapped `unit-test` request that would adopt an existing card gets 503 or 200 when the QA tool is off.
- Recommendation: State the position: before the lookup, or create-only.

### Register and count lines missing from the change list - Severity: Nit
- Affected scope: §12 target prose values and file rows; §9
- Finding:
  - `brd.html:1522` also says "Sixty-three in all".
  - `README.md:143` gives the dashboard/pustaka split ("687 in `dashboard/`"), which must become 712.
  - The decision-log row omits the D-091 (8) amendment.
  - `Result.UnitHeldCanceled` (§7.4) is not in the `store.go` row.
  - No phase is named for the changelog entry and the `product.semver` bump.
- Recommendation: Add each of these to §12 and the count table, and assign the changelog and semver bump to phase 6.

Counts: B=0 M=8 N=5
