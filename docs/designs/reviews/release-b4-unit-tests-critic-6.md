# Goldfish critic report: `release-b4-unit-tests.md` (rev 6)

**Summary.** 1 Blocker, 10 Majors, 9 Nits. The Blocker is the restart story: the design assumes a running unit card stays RUNNING across a restart, but boot reconciliation moves it to READY.

**Counter-argument to the thesis.** Rev 6 says that keying everything on the planner-set card key, plus a store-side hold, makes `unit-run` safe and restart-proof. The code does not support that yet:

- At every boot, reconciliation moves the runner's own cards to READY, so nothing a RUNNING reason carries survives a restart in the status the design relies on.
- The job's mode is still read from labels an operator can set.
- The scheduler's lease is computed on a struct that has no key and no reason to read.
- Several "bounded" costs (the runner loop's GitHub reads, the refusal notices) are not bounded.

---

### Boot reconciliation moves every runner-driven RUNNING card to READY, so §5.4's restart design rests on a wrong premise - Severity: Blocker
- Affected scope: §5.4 (adoption, act-once marker), §4.1 (switch-off "a RUNNING job is left to finish"), §5.5 (`ResolveUnitQACard` RUNNING → RUNNING), §5.6 (counting "RUNNING unit jobs"), TC-867
- Finding and rationale:
  - `MemStore.Reconcile` (`dashboard/runner.go:705-718`) moves every DISPATCHED or RUNNING task with no open gateway turn to READY. It runs at every boot, before the QA-job runner starts (`dashboard/main.go:292-302`, then `:439-443`).
  - A QA-tool card never has a gateway turn, so after each restart every RUNNING `unit:qa` (and e2e) card is READY. Its `qa-job:` reason stays, because `moveTaskLocked` does not clear `BlockReason`. Adoption still finds the job id (`qajobs.go:355`).
  - What breaks:
    - The act-once marker is written only through `ResolveUnitQACard` RUNNING → RUNNING (§5.5 table). On a READY card that move is refused. So after a restart the marker can never be written for a job that had not yet acted, and the guarantee "a restart never acts twice" fails at the next restart.
    - Switch-off cancels READY `unit:qa` cards (§4.1), which now includes cards whose job is still running. That contradicts "a RUNNING job is left to finish".
    - "fewer RUNNING unit jobs tracked by the runner" (§5.6) is ambiguous once the tracked job sits on a READY card.
  - The design never mentions `Reconcile`, and `runner.go` is not in §12.
- Recommendation: Do one of the following, and state it in §5.4:
  - (a) `Reconcile` skips cards for which `qaToolDriven` holds (`runner.go` added to §12, with a TC); or
  - (b) `ResolveUnitQACard` gains READY-with-`qa-job:` → RUNNING, adoption moves the card back to RUNNING first, and the switch-off cancel skips cards with a `qa-job:` reason.

  Then restate TC-867 against a real `Reconcile` call.

### The scheduler's lease cannot evaluate `qaToolDriven`: `TaskNode` has no key or reason - Severity: Major
- Affected scope: §4.4, §12 rows for `scheduler.go` / `board.go`
- Finding and rationale:
  - `Schedule` computes `liveIn` over `[]TaskNode` (`dashboard/scheduler.go:231-239`).
  - `TaskNode` carries ID, workflow, desk, title, role, model, status, deps, priority, ordinal, gate and labels only (`scheduler.go:90-105`). It has no `DedupKey` and no `BlockReason`.
  - The nodes are built in `schedulerInputLocked` (`dashboard/store_mem.go:2377-2385`). That file is not in the change list.
  - `qaToolDriven(t *TaskRecord)` cannot be called where the design says it is called.
- Recommendation: Add `DedupKey` and `BlockReason` (or a precomputed `QADriven bool`) to `TaskNode`, populate it in `store_mem.go:2378`, list `store_mem.go` in §12, and keep `Schedule` pure.

### The job mode still comes from labels, so an operator's e2e label turns a unit card into an e2e create or run - Severity: Major
- Affected scope: §5.2 (`isE2ECard` returns `"unit"`), §4.2 (planner-only refusal covers only the `unit` family), TC-863
- Finding and rationale:
  - `isE2ECard` checks `e2e:automation`, then `e2e:run` (`qajobs.go:230-238`).
  - The `e2e` family stays operator-settable (`prflow.go:549-557` refuses only the defect, source and e2e-kind families), and adding an `e2e:*` label does not remove `unit:qa`, because it is a different family (`prflow.go:567-580`).
  - So `e2e:automation` added to a `unit:qa` card makes the runner start a `create` job on a QA-runner key. `Tick` accepts it, because the key qualifies. `AutoMerge` is armed for `mode == "create"` (`qajobs.go:380`).
  - This contradicts "AutoMerge is always nil for a unit card" and rev 6's own principle (key, not label).
- Recommendation: Derive the mode from the key: `:unit-qa` → `unit`, checked before any label. Refuse `e2e:*` labels on a card whose key ends `:unit-qa`. Extend TC-851 and TC-863.

### The act-once marker is a separate transaction from the defect write; a crash between them re-acts and loses the held-card notice - Severity: Major
- Affected scope: §6 poll order (steps 2→5), §5.4, §7.1 (`UnitDefectIn.TaskID` is unused)
- Finding and rationale:
  - `PlanUnitDefect` (step 2) and the marker (step 5) are two store calls with GitHub reads between them (step 3).
  - A crash after step 2 leaves `unitDone` false. After a restart:
    - If an admin closed the defect in the meantime, the next poll reopens it, which is exactly what TC-867 says cannot happen.
    - Otherwise the dedup hit returns `UnitPlanned` false, so the held-card notice (step 4) is never sent.
  - The design also does not say that `ResolveUnitQACard` returns `Result.Floor`. Without it ArcadeDB does not write the Task row (`prflow.go:334-335` documents this bug class), so the marker may not be durable at all.
- Recommendation: Write the marker inside `PlanUnitDefect` and `PassUnitDefects`, using the `TaskID` the input already carries. Return `Floor`. Mark "notice sent" on the defect so that a dedup hit with the notice unsent re-sends it.

### The runner loop's GitHub cost is understated, and the BLOCKED retry branch is uncapped - Severity: Major
- Affected scope: §5.6, §14 "Runner latency", §7.5 `buildUnitContext`
- Finding and rationale: §5.6 claims "at most 3 GitHub reads" per start. The code adds more:
  - `buildReviewContext` makes one `fileAt` read per candidate path until 6 files land (`reviewdefect.go:326-342`). It does not apply `defectMaxFetch` (`defecttriage.go:143`). The paths are up to 50 failing files plus the merged pull request's files.
  - `pullFiles` reads up to 30 pages (`github.go:225-241`).
  - Each GitHub call can sleep up to 20 s on rate limits (`github.go:376-391`).
  - Every `unit:failure` notice on the `unit:qa` card calls `pullCommitAuthors` (`notify.go:96`).
  - The §5.5 retry branch has no per-tick limit, and "after a restart … the first Tick retries at once" every BLOCKED card.

  All of this runs in the single loop, and the time counts against e2e active time (`qajobs.go:406-413`).
- Recommendation: Cap `buildUnitContext` reads (apply `defectMaxFetch`, and limit PR files to page 1). Count the retry branch inside the 2-per-tick budget. Restate the bound in §5.6 with the real arithmetic.

### `since:<epoch>` is not specified to survive BLOCKED → READY → BLOCKED, so the 24-hour end is unenforceable - Severity: Major
- Affected scope: §5.5, §5.4
- Finding and rationale:
  - The reason "is kept by every later block of the same card", but the card passes through READY in between.
  - The design does not say what reason `ResolveUnitQACard` writes on BLOCKED → READY. `resolveCardLocked` overwrites `BlockReason` with its argument (`prflow.go:650`).
  - A board unblock clears it (`board.go:397-401`), and so does any drift release that rewrites it.
  - A card that alternates between the NoCommit and Attest failures can therefore retry forever, or restart its 24-hour clock.
- Recommendation: Keep `since` on the READY reason, rewritten by `ResolveUnitQACard`, or keep it in a durable field. Say whether a person's unblock resets it, and test that alternating failures still end at 24 hours.

### A RUNNING `unit:qa` card still marks a `unit-test` agent busy, starving other products - Severity: Major
- Affected scope: §4.4, §4.2 (role `unit-test` kept)
- Finding and rationale:
  - `liveAgentSlots` marks one agent of the task's role busy for every DISPATCHED or RUNNING task (`store_mem.go:2410-2416`). A busy agent is free only for its own desk (`scheduler.go:115-118`, `:310`).
  - With up to `GAOS_UNIT_QA_OPEN` 30-minute unit runs per product, a one-agent `unit-test` roster blocks every other product's `unit:test` card ("every unit-test agent is busy").
  - The design exempts the lease but not the seat, and §14 is silent on this.
- Recommendation: Skip `qaToolDriven` tasks in `liveAgentSlots` too, add this to §12 and TC-856, or give `unit:qa` a role no agent holds.

### Refusal notices are unbounded, and the switch can be turned on in a state where every run is refused - Severity: Major
- Affected scope: §5.7 row 2, §4.1 handler refusals, §7.2
- Finding and rationale:
  - §5.7 bounds only the held-card notice. The "no defect filed (<why>)" notice goes out on every refused failing run.
  - The Unit tests save requires only the QA attestation and a GitHub reader. It does not require `AutoFix.Enabled`, a fixer, a triager or any `TargetRepos`.
  - So a product can have `UnitQA` on, auto-fix off, and a notice on every merge with a failing test, indefinitely.
- Recommendation: Either refuse enabling unless §7.2 conditions 1–4 hold (and cancel queued cards when they stop holding), or send the refusal notice at most once per product, reason and UTC day.

### The external-tasks API misreports a `unit:qa` card and applies the wrong preconditions - Severity: Major
- Affected scope: §4.2 API mapping table, §12 `api_tasks.go` row, published contract
- Finding and rationale:
  - `apiDispatchAnswer` (`api_tasks.go:99-111`) returns `qa-tool` only for e2e labels. A `unit:qa` card answers `scheduler` or `operator`, which is false in the published answer.
  - `runner-off` (`api_tasks.go:187`) refuses a request that would spend no model turn.
  - The `qa-off` 503 (`api_tasks.go:234`) is not applied to a mapped `unit-test` request, so the API plans a card that stays PENDING forever.
- Recommendation: Add `unit:qa` to `apiDispatchAnswer` (`qa-tool`). Exempt mapped requests from `runner-off`. Apply `qa-off` to them. Publish all three, and add them to TC-855.

### A unit-defect fix may pass by weakening the failing test; no rule and no risk entry covers it - Severity: Major
- Affected scope: §7.5, §14 Risks
- Finding and rationale:
  - The context deliberately loads the failing test files (`TestGlobs`).
  - `checkFix` allows edits to existing test files and counts them as `TestLines` (`autofixcheck.go:275-330`).
  - For a unit defect, the cheapest "fix" an agent can make is to edit the assertion. The human merge gate is the only defence, and the design does not name the risk.
- Recommendation: For `kind == unit`, refuse edits that delete or modify lines in the files of the failing tests (new test files stay allowed). At minimum, list the risk in §14 and flag test edits in the fix PR body.

### Overloading `AllowedBases` couples "run unit tests here" with "fixes may be retargeted here" - Severity: Major
- Affected scope: §5.1 "The base rule", D-105, `model.go:462-465`
- Finding and rationale:
  - `AllowedBases` means retarget targets (`model.go:462`, `autofixpr.go:90`, `retarget.go`).
  - To run unit tests on a non-default integration branch, an admin must also allow every fix PR to be retargeted there. Removing a branch to stop retargets silently stops its unit runs and blocks open unit fixes (`BlockBaseNotAllowed`).
  - No alternative is weighed in §13.
- Recommendation: Add a separate `UnitQA.Bases` (at most 8, validated like `AllowedBases`) and re-check `UnitBase` against it plus the effective base. Or record the coupling as a rejected alternative with a reason.

### `ResolveUnitQACard`'s key-suffix check also accepts a merged-spec run key - Severity: Nit
- Affected scope: §5.5
- Finding and rationale: A `qa-merged/…:<caseCode>` key (`mergedrun.go:34`) ends `:unit-qa` if a case code is `unit-qa`.
- Recommendation: Require that `prKeyParts` succeeds and that the suffix is `:unit-qa`.

### "Names no repository" is not what the lines achieve: card ids carry it - Severity: Nit
- Affected scope: §5.7
- Finding and rationale:
  - Lines end `· card <id>`. PR card ids embed the sanitized `org-repo` (`prflow.go:144-149`).
  - Defect card ids embed the defect key (`defecttriage.go:102`), which for a unit defect includes repo, base and hash.
  - The reason given for avoiding `Ref()` therefore does not hold.
- Recommendation: State that ids carry the sanitized repository as they do today, or stop justifying h8 by it.

### §3 and §6 disagree about `PassUnitDefects` on `build-failed` - Severity: Nit
- Affected scope: §3 diagram vs §6 outcome table and poll order
- Recommendation: Pick one and align both. Calling it with `buildOK=false` is harmless.

### `ghError` already has `Retry` - Severity: Nit
- Affected scope: §5.1
- Finding and rationale: `ghError.Retry time.Duration` already exists (`github.go:311`), so the new `RetryAt` duplicates it.
- Recommendation: Reuse `Retry`, or say why `RetryAt` replaces it.

### Anchor drift - Severity: Nit
- Affected scope: §4.1
- Finding and rationale: `ReviewCap()` is at `model.go:520`, not `:516`.
- Recommendation: Re-anchor.

### Inconsistent setting names - Severity: Nit
- Affected scope: §4.3, §5.6, §10
- Finding and rationale: `GAOS_QA_UNIT_TIMEOUT_MIN` and `GAOS_UNIT_QA_OPEN` put the same words in opposite orders.
- Recommendation: Use one prefix for both.

### `UnitTests` is untrusted text with no retention rule - Severity: Nit
- Affected scope: §7.1, schema 21
- Finding and rationale: The 30-day retention empties `excerpt` (`schema/17-defects.sql:6-7`), but not the new `unitTests` property, which holds test names from the code under test.
- Recommendation: Include `unitTests` in the retention job, or state why it is kept.

### The delegation clause should not read as waiving the security gate - Severity: Nit
- Affected scope: §0 "Delegation"
- Finding and rationale: The policy forbids "Approved" while a Critical or High security finding is open (`docs/rules/gaos-design-review.md:99-100`).
- Recommendation: Say that the delegation does not waive that gate or step 8's summary.

### Start fairness and PENDING accumulation - Severity: Nit
- Affected scope: §5.6, §14
- Finding and rationale: The 2 starts per tick go to products in iteration order, so earlier products can starve later ones. PENDING cards with no runner (GAOS_QA_URL unset) never end.
- Recommendation: Rotate the starting product. Give PENDING cards with no runner a stated end.

---

These sections hold up:
- **§9 / §12 arithmetic:** the TC ranges (25 = 3+4+5+7+5+1) and the Go test counts (712 → 737) are consistent. The current values check out at `deploy/check-workspace.sh:115,129,131`.
- **BR-05:** confirmed that no new event type is added (`events.go:38,149`).
- **Held-card store guard:** sound for `MoveCard` and `CloseDefect`. These are their only non-test callers (`handlers_board.go:72`, `handlers_autofix.go:165`).

Counts: B=1 M=10 N=9
