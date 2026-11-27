# Readiness review: `docs/designs/release-b4-unit-tests.md` (rev 1)

I read the design, the policy (`docs/rules/gaos-design-review.md`), `docs/designs/README.md`, the three domain registers, and the code the design anchors to under `dashboard/`, `deploy/` and `pustaka/`. I did not open anything under `.claude/`.

**What checks out:**
- **Identifiers.** D-104 is the next free decision (the log ends at D-103, `decision-log.html:2148`). BR-63 is the next free rule (BR-62 is at `brd.html:1308`). TC-839 is the next free test case (TC-838 is the highest in `qa-test-plan.html` and `mergedrun_test.go`). D-091 (8) exists.
- **Label count.** Going from 19 to 20 labels matches `labelSet` and `labelOrder` (`labels.go:95-140`).
- **Anchors.** Most anchors resolve: `qajobs.go:32,63,194,230,241,506`, `autofix.go:162`, `defecttriage.go:78`, `reviewdefect.go:143,319`, `defects.go:37,288,612`, `autofixpr.go:76`, `handlers_autofix.go:17`, `board.go:330`, `pullrequests.go:20-21,97`.

---

### No phases, no exit conditions, no verification commands - Severity: Blocker
- Affected scope: §10 (whole document)
- Finding: The work is one table with no phases. Nothing says what order to build things in. The schema has to come before the ArcadeDB persistence, the label before the planner, and the store method before the runner, but none of that is written down. No step has a command and an expected result. The tests are one cell: "tests (TC-839 onward)". It names no test file, gives no count, and does not map one TC ID to each behaviour. Two engineers would allocate TC IDs differently, and the register rows in `qa-test-plan.html` cannot be written ahead of time.
- Recommendation: Split the work into phases. For example: (1) schema, label and model; (2) planner and merge commit; (3) runner and outcome table; (4) unit defect, context and hold; (5) the `qa-unit` row; (6) the Auto-fix page; (7) registers. For each phase, list the exact `*_test.go` files, the TC IDs it owns (TC-839..TC-8xx, one per behaviour named in §10) and its exit command (e.g. `cd dashboard && go test ./... -run 'TestUnitQA'`, plus `deploy/check-stacks.sh`) with the expected result.

### The `qa-unit` pull-request row cannot be written as specified - Severity: Blocker
- Affected scope: §7, §10 row "`qajobs.go`…`qa-unit` row", `schema/21-unit-defects.sql`
- Finding: Four things in the current code stop the row from being written, and the design mentions none of them.
  1. `recordQARow` returns early without writing a row when `t.caseCode == "*"` (`qajobs.go:519`). §4.2 makes `CaseCode` `"*"` for every unit mode, so no `qa-unit` row is ever written.
  2. `upsertPullRequestLocked` refuses any kind other than `review` or `qa` (`pullrequests.go:111`).
  3. The QA-only field fill runs only for `cur.Kind == PRKindQA` (`pullrequests.go:148`).
  4. `schema/18-pull-requests.sql:13` constrains `PullRequest.kind` to the regexp `'review|qa'`. §6.1 widens only `Defect.kind`, so ArcadeDB would refuse the row.

  The design also never says what `Kind` value `recordQARow` writes, or where `HeadRef` comes from for the "must start `qa-unit/`" check. The job answer carries no branch, and the row's `HeadRef` is filled by a later GitHub poll.
- Recommendation: Spell out the `recordQARow` branch for unit cards: which kind it writes, and that it skips the `"*"` guard. Name the change to `pullrequests.go:111` and `:148`. Add `ALTER PROPERTY PullRequest.kind REGEXP 'review|qa|qa-unit'` to `21-unit-defects.sql`, with the full statement text. State when the `TestGlobs` and branch checks run: at row write, or at the first `AdvancePullRequest` that has `HeadRef`.

### A Blueprint sync releases the "held for a person" card - Severity: Blocker
- Affected scope: §6.3
- Finding: When a sync happens, `releaseDriftLocked` (`workspace.go:384-400`) moves every BLOCKED card of the desk to READY unless `operatorHolds(reason)` is true. `operatorHolds` (`sendback.go:153-158`) lists only `ReasonDesignBlocked`, `ReasonDesignUnreviewed` and `ReasonQaEpicAdded`. So a held unit triage card would be released by a machine, with no admin involved. That breaks the operator's D-103 (5) decision.
  - The `autoClaimLocked` guard does not help. `moveTaskLocked` does not clear `BlockReason`, so the card lands in READY still carrying `ReasonUnitHeld`. Auto-claim then refuses it forever, and no unblock move is possible because the card is no longer BLOCKED.
  - The same applies to the §4.3 capability BLOCKED reason, if that one is also meant to be a hold.
- Recommendation: Add `ReasonUnitHeld` to `operatorHolds`, and put `sendback.go` and `workspace.go` in the change list. Add a test: a drift release leaves a held unit triage card BLOCKED.

### Defect state "new" does not exist - Severity: Major
- Affected scope: §6.1 "`State` new"
- Finding: The defect states are `open|triaged|fixing|fixed|closed` (`defects.go:28-32`), and the DDL regexp enforces them (`schema/17-defects.sql:36`). A `new` state would be refused on ArcadeDB.
- Recommendation: Use `DefectStateOpen`. Name the events emitted: `DefectReported` and, unlike `PlanReviewDefect`, no `DefectTriaged`.

### The unit defect record is under-specified - Severity: Major
- Affected scope: §6.1
- Finding: Two engineers would build different records here.
  - **Missing fields.** `CaseID` is mandatory (`17-defects.sql:15`). The design also leaves out `Title`, `Class`, `Gen`, `FirstSeen` and the `Excerpt` cap, all of which `PlanReviewDefect` sets (`reviewdefect.go:164-174`).
  - **A failing set seen again on a closed or fixed defect.** QA defects reopen with `Gen++` (`defects.go:544-547`). Review defects never reopen (`reviewdefect.go:160`). The design says only "updates `LastSeen` and plans nothing".
  - **`result: failed` with an empty `failing` list** (a build failure, say). The hash of an empty set and empty `Files` are left undefined.
  - **Gating.** It is unstated whether `PlanUnitDefect` checks `AutoFix.Enabled`, `TargetRepos` membership (§12 says outside repos are a non-goal), triager assignment, or the global `defectMaxNewKeys` (`defects.go:59`).
  - **How the cap is counted.** `GAOS_UNIT_DEFECTS_PER_DAY` does not say how it counts (by `FirstSeen` day and `Kind == unit`?).
- Recommendation: Give the full `Defect` literal and a `UnitDefectIn` struct. Pick one rule for reopening. Decide the empty-set case: treat it as `qa-unit-unreadable`, or as a single "build failed" defect. List the refusal conditions in order, as `reviewPlanRefusalLocked` does (`reviewdefect.go:100-117`).

### Where the switch lives in the planner is undecided; a transient GitHub error silently drops the card - Severity: Major
- Affected scope: §2 step 1, §3 "Start commit"
- Finding:
  - **No hook point.** `prCardsFor(ev)` (`prflow.go:80`) has no access to `Master`. The design does not say whether it gains a `Master` parameter or whether a `withUnitQA` step joins `withRepoKind` (`prflow.go:225`).
  - **No job constant.** It does not name the job constant that produces the `:unit-qa` key suffix (e.g. `prJobUnitQA = "unit-qa"`).
  - **"No commit" check undecided.** It does not say whether the check lives in `handle` (a log line) or in the store (`Result.Skipped`).
  - **Silent drop on a GitHub hiccup.** The merged re-read swallows errors (`prflow.go:456-461`) and returns `flowDone`. Under the new rule, one 5xx means the merge permanently gets no unit card, and the cursor passes the event. The opened path retries transient errors (`prflow.go:466-468`). The design does not say which behaviour the merged path gets when `UnitQA` is on.
  - **Two minor gaps.** It does not say whether `Commit` is set before `MergeChecked` gating. `shaRe` accepts 64 hex (`staging.go:186`), but §4.2 says "40 hex".
- Recommendation: Name the function signature change and the constant. Say that with `UnitQA.Enabled` a `lookupTransient` error on the merge re-read returns `flowRetry`, and a permanent error logs `skip unit-qa-no-commit`. Fix the "40 hex" wording.

### The runner changes leave mode, case code, timeout and notices open - Severity: Major
- Affected scope: §2 step 2, §3 "Timeout", §4.1, §5
- Finding:
  - **Mode undecided.** `isE2ECard(labels)` (`qajobs.go:230`) only sees labels, but the mode depends on `UnitQA.Create`. The design does not say whether the mode is fixed at plan time (in the key or a label) or read from the product at start. It also does not say what a PENDING `unit:qa` card does when the switch is turned off.
  - **Wrong path in `caseCodeFor`.** With mode `unit-run`/`unit-create`, `caseCodeFor` (`qajobs.go:279-295`) falls through to PR-body parsing and BLOCKs the card with "no QA-case line". The design states `CaseCode "*"` but not the branch that produces it.
  - **Timeout mechanism unnamed.** The runner has one `r.timeout` (`qajobs.go:178`). A per-card timeout needs a field on `qaTracked` or a lookup, and neither is named. The env parsing (clamping, logging) is not given in the shape of `qaApprovalWaitFromEnv`.
  - **e2e wording on unit cards.** `fail` sends notice `e2e:failure` with text "e2e …" (`qajobs.go:499`). The approval wait sends `e2e:approval` and "approve the spec" (`qajobs.go:448-451`). `doneReason` has no unit branch. The design does not say which notice kinds and texts a unit card uses.
  - **DONE before or after filing?** The outcome table does not say whether `PlanUnitDefect` runs before the DONE move, or what happens when it returns a store error: retry on the next poll, or DONE anyway.
- Recommendation: Add a unit branch to `isE2ECard`/`caseCodeFor`/`doneReason`, with the exact strings. Add `qaTracked.timeout` set at `start`. Name notice kinds such as `unit:failure` and `unit:approval`. Write the poll order: build context, `PlanUnitDefect`, `SetDefectContext`, `ResolvePRCard` DONE, and what each failure does.

### The capability and attestation read has no shape - Severity: Major
- Affected scope: §3, §4.3
- Finding:
  - **No API for the sandbox object.** `capabilities()` returns `map[string]bool` (`qaclient.go:67`) and is part of `qaJobClient` (`qajobs.go:146`). No method, type or interface change is given for the `sandbox` object.
  - **Not fresh reads.** The cache lasts 10 minutes, and an error keeps the last set (`qaclient.go:57-58, 94`). So "the handler has just read" and "re-reads the capabilities at each start" are not fresh reads, and the design does not say whether to bypass the cache.
  - **"Stricter" is open.** "A stricter one is fine" is defined only for `timeLimitSec`. Is `network: "none"` accepted? Is any `timeLimitSec` ≤ 1800 accepted? How does it relate to `GAOS_QA_UNIT_TIMEOUT_MIN`, which may be up to 120 minutes?
  - **`AttestedAt` has two meanings.** It is "when GAOS last read" in the struct and "written when an admin saves" in the prose, and neither is the QA tool's `attestedAt`.
  - **The capability-BLOCKED card has no way out.** Re-saving the page does not move it, and `Tick` ignores BLOCKED (`qajobs.go:258`).
- Recommendation: Define `type qaSandbox struct{…}` and `func (o *qaOutbound) sandbox(ctx, fresh bool) (qaSandbox, bool)`. Write the acceptance predicate field by field. Fix what `AttestedAt` stores. State how the BLOCKED card is released: a person moves it to Ready, or the save releases it.

### Admin-only release: the actor has no path, and the auto-claim guard never fires - Severity: Major
- Affected scope: §6.3
- Finding:
  - **No actor path.** "Attributed to the admin in its event" has no route through the code. `MoveCard(ctx, productID, taskID, mv)` (`board.go:330`) takes no actor, `CardMove` has no actor field, and `moveTaskLocked` passes an empty `EventPayload` (`runner.go:445`). The signature change, and `store_arcade_blueprint.go:175-177`, are not in the change list.
  - **Check-then-move race.** A handler-level 403 means reading the card's `BlockReason` before the move. The data source for that read is unnamed, and the read is racy. A store-level check, like the design-gate check at `board.go:363-377`, would be decidable.
  - **Unclear scope of the 403.** The design does not say whether it also covers cancel and same-column reorder.
  - **Dead guard.** The `autoClaimLocked` guard is dead code: auto-fix only dispatches READY cards (`autofix.go:311`), and an unblock clears `BlockReason` (`board.go:397-401`).
- Recommendation: Put the refusal in `MemStore.MoveCard`, next to the design-gate check, with the actor passed in. Name the new signature, or a `MoveCardAs`. Say which moves are refused. Drop the auto-claim guard, or justify it by a named path.

### Context building needs a source the runner does not have - Severity: Major
- Affected scope: §6.2
- Finding:
  - **No code source.** `buildReviewContext` needs a `codeSource` (`fileAt`, `reviewdefect.go:330`). The runner holds only `prSource` (`qajobs.go:173`). The design does not say which client or token is injected, or the change to `newQAJobRunner`/`main.go`. `main.go` is not in the change list.
  - **No failure path.** When the build fails, `autoFixPass` later calls `buildAndStoreDefectContext` (`autofix.go:319`, `defecttriage.go:428-436`). That rebuilds the slice from `AutoFix.BaseBranch` through the QA-defect search, not at the merge commit. The design does not say whether that fallback is accepted, or whether the triage card is blocked with `BlockSearchFailed`.
  - **Ordering and caps.** Path ordering and caps (`reviewFilesFor`-style ordering, `defectMaxFiles`) are unstated.
- Recommendation: Name the injected source and the wiring line in `main.go`. Decide the failure path. Give the path list order.

### `unit:qa` shares an operator-settable family with `unit:test` - Severity: Major
- Affected scope: §3 "The label"
- Finding: `setTaskLabelLocked` refuses only the planner-only families `defect`, `source` and `e2e-kind` (`prflow.go:549-557`). Within a family it swaps the label (`prflow.go:567-580`). So an operator can relabel a `unit:qa` card to `unit:test`, and the scheduler then dispatches it to an agent with no checkout. The reverse swap gives the runner a card with no merge commit. The design is silent on this.
- Recommendation: Decide whether the planner alone sets the `unit` family, or only `unit:qa`. Add the refusal and a test for it.

### The change list misses files the design itself changes - Severity: Major
- Affected scope: §10
- Finding: These are changed by the design's own text but missing from the change list:
  - `pullrequests.go`: §7 changes `PRKindQAUnit`, `prRepos` and the kind check.
  - `github.go`: `ghPR.MergeCommitSHA`.
  - `main.go`: the runner timeout and source wiring.
  - `board.go` and `store_arcade_blueprint.go`: the actor on unblock.
  - `sendback.go` and `workspace.go`: the hold.
  - `schema/18`: or its widening, stated in 21.
  - `deploy/stacks/compose.prod.yaml` and `deploy/check-stacks.sh`. Every GAOS setting is passed and asserted there (e.g. `GAOS_QA_APPROVAL_WAIT_HOURS`, `compose.prod.yaml:184`, `check-stacks.sh:221`), so the two new settings never reach production otherwise.
  - The registers: `pustaka/docs/product/decision-log.html`, `brd.html`, `qa-test-plan.html`, `pustaka/docs/integrations/pr-agents.html` (it documents `unit:test`) and `pustaka/docs/changelog.html`.

  Also, `artifacts/v1/gaos-arcadedb-model.html` is at the repository root, not in `dashboard/`, so it breaks the header's "bare name is in `dashboard/`" convention. "tests" is a description, not a path.
- Recommendation: List every file by its repository-relative path, with one change per row.

### The new route and form for the Auto-fix page are not specified - Severity: Major
- Affected scope: §3, §10 row "`handlers_autofix.go`…"
- Finding: The design does not say whether `SetUnitQA` is saved through the existing `POST /products/{id}/autofix` (`main.go:577`) or a new route. It gives no form field names, no refusal messages (missing capability, or `Create` without `CIConfirmed`), and nothing about what the section shows (the attestation fields, the age). Two engineers would build two different pages and APIs.
- Recommendation: Give the route, the method, the field names, the exact refusal texts and the rendered fields.

### Anchors and wording - Severity: Nit
- Affected scope: §2, §3, §6.2
- Finding:
  - `PlanPullRequestCards` is at `prflow.go:208`, not 206.
  - The `pullFiles` implementation is `github.go:228`. `prreview.go:226` is the interface line.
  - "A new family `unit` already holds `unit:test`; `unit:qa` joins it" reads as if the family were new, but no `labelFamilyUnit` constant exists.
  - `AttestedBy`'s comment ("the admin who turned it on") does not match its name.
  - `unit:qa` cards count toward `GAOS_PR_MAX_OPEN` (`prflow.go:177-197`). That is probably intended, but it is not stated.
- Recommendation: Correct the line numbers, reword the family sentence, and say whether the PR cap applies.

Counts: B=3 M=10 N=1
