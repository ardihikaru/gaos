# Goldfish review (zoom-out lens): `release-b4-unit-tests.md` rev 1

**Paraphrase.** There is a product switch, `UnitQA`, and it is off by default. When it is on, a merged pull request plans a `unit:qa` card instead of the agent-driven `unit:test` card. The card goes to the existing QA-job runner, which asks the QA tool to run, or also write, unit tests at the merge commit. A failed job ends the card FAILED and files nothing. Failing tests file a unit defect, deduplicated by failing-test set and capped per day. Its triage card is born BLOCKED and only an auto-fix admin may release it. After release the existing D-091 auto-fix path runs. Tests the QA tool writes come back as a `qa-unit` pull-request row, which GAOS checks against `TestGlobs`.

**Architecture.** The overall shape is right. It reuses the QA-job runner, born-BLOCKED defect cards, `buildReviewContext` and `SetDefectContext` rather than building new machinery. Most of the components D-103 §5 requires are present. The problems are some missing or contradicted links in the data flow, and two ways into the system that the design never considers.

---

### The merge commit has no home on the card, so the runner cannot send `sha` - Severity: Blocker
- Affected scope: §3 "Start commit", §2 step 1, §4.2
- Simplicity rationale: §3 routes the merge commit through `ghPR.MergeCommitSHA`, then the re-read in `prFlow.handle`, then `PREvent.Commit` (`braintown_consumer.go:105`). But the event is not stored, and the card key `<owner>/<repo>#<n>:unit-qa` has no SHA in it. `PREvent.Key` puts the commit in the key only when `PRNumber == 0` (`braintown_consumer.go:123-126`). `qaJobRunner.start` builds its request from `c.DedupKey`, `c.Labels` and `c.BlockReason` only (`qajobs.go:350-370`). So the SHA reaches the planner and is then lost, and the runner has nothing to send. Two engineers would solve this differently: put the SHA in the key, add a new task field, or re-read GitHub in the runner. Rule 6 of the design contract forbids leaving that open. The event plumbing is also a detour.
- Recommendation: Pick one. The smallest is to have the runner read `merge_commit_sha` at start through the `prSource` it already holds (`qajobs.go:173`, used by `caseCodeFor` at `qajobs.go:286-288`). Keep the `ghPR.MergeCommitSHA` field and drop the `braintown_consumer.go` change. If the read fails or the SHA is absent, block the card with a reason. This also puts the "no merge commit" decision in one place, the runner, instead of the planner.

### `recordQARow` skips every job whose case is `*`, so no `qa-unit` row is ever written - Severity: Major
- Affected scope: §4.2 ("`CaseCode` is `"*"` for unit modes") against §7 ("written by `recordQARow`")
- Simplicity rationale: `recordQARow` returns early with `t.recorded = true` when `t.caseCode == "*"` (`qajobs.go:519-521`). It also hard-codes `Kind: PRKindQA` (`qajobs.go:523`). The `upsertPullRequestLocked` allow-list only accepts `review` and `qa` (`pullrequests.go:111`). As written, the §7 check never runs. `pullrequests.go` is not in the §10 change list.
- Recommendation: In §7, say that `recordQARow` branches on a unit mode before the `"*"` test and writes `PRKindQAUnit`. Name `pullrequests.go:97,111` as changed, and add `pullrequests.go` to §10.

### The QA tool's own test pull request goes back through Brain Town - Severity: Major
- Affected scope: §2 steps 3 and 7, §7, §12
- Simplicity rationale: The `qa-unit/<sha12>` draft is opened in the application repository, for example `acme/route-api`, which is in `Master.Repos`. `prFlow` skips it only while it is a draft (`prflow.go:232`, `prflow.go:473-474`). After a person marks it ready, or merges it:
  - it plans a `pr-review` card and an `e2e:automation` card (`prflow.go:83-87`);
  - its merge plans another `unit:qa` run (`prflow.go:88-91`);
  - a review row and a `qa-unit` row compete for the same key, and `upsertPullRequestLocked` never changes an existing row's kind (`pullrequests.go:104-107`), so the row written first wins.

  GAOS already solved this for its own fix pull requests with `ownReviewFixLocked` (`prflow.go:251`, `prflow.go:343-353`). The design does not use it.
- Recommendation: Add one rule. A pull request whose head is `qa-unit/*`, or that matches a `qa-unit` row, is the product's own and plans no review, e2e or unit card. This is the same pattern as `ownReviewFixLocked`. Decide which row kind wins when both exist.

### The second planner of unit cards, the external-tasks API, is not addressed - Severity: Major
- Affected scope: §1 row 1, §3, §12 ("With `UnitQA` off, nothing changes" covers only the off state)
- Simplicity rationale: `POST /v1/products/{id}/tasks` with kind `unit-test` plans an agent-driven `unit:test` card through `apiJob` → `prJobUnitTest` (`api_cards.go:89`, `api_cards.go:126-134`, `api_tasks.go:226`). The design says the switch "chose the QA tool" (§3). But with `UnitQA` on, an API client still gets the old agent card, and because the keys differ it can sit next to the new `unit:qa` card.
- Recommendation: State what the API's `unit-test` kind does when `UnitQA` is on. The two clean options are: map it to `unit:qa` with the same key, or refuse it with a named code. Add `api_cards.go` and `api_tasks.go` to §10.

### `PlanUnitDefect` has no refusal set, so released cards can be stranded - Severity: Major
- Affected scope: §6.1, §6.3, §12 ("unit tests for repositories outside `TargetRepos`" is a non-goal)
- Simplicity rationale:
  - The model the design copies, `PlanReviewDefect`, refuses first through `reviewPlanRefusalLocked`: fixer assigned, auto-fix on, repository a fix target and not an automation repository, day cap, and the global `defectMaxNewKeys` (`reviewdefect.go:100-118`, `reviewdefect.go:155`).
  - §6.1 copies the record but not this gate.
  - `unit:qa` is planned for any repository in `Master.Repos` (`prflow.go:229`), not only `TargetRepos`, which contradicts the non-goal.
  - With auto-fix off, an admin can release the triage card and nothing will ever claim it. `autoClaimLocked` refuses when `!m.AutoFix.Enabled` (`autofix.go:164-166`), and `labelsAllowDispatch` refuses every `defect:*` card (`labels.go:182-183`). The card sits in Ready indefinitely.
  - Unit defects also silently use up the QA defects' shared 50-per-day budget (`defects.go:59`, `defects.go:325-333`), because `newKeysTodayLocked` counts every kind.
- Recommendation: Add a `unitPlanRefusalLocked` modelled on `reviewPlanRefusalLocked`: auto-fix on, the triager role assigned, `Repo` in `TargetRepos` and not in `AutomationRepos`, the unit cap, and `defectMaxNewKeys`. Also gate `unit:qa` planning on `Repo ∈ TargetRepos`, so the non-goal holds by design and not by accident.

### The unit defect has no end of life: passes, regressions and overlapping sets - Severity: Major
- Affected scope: §6.1 dedup key and "the same failing set seen again updates `LastSeen` and plans nothing"
- Simplicity rationale: The existing QA path closes a defect when its case passes (`xpass` → `DefectStateFixed`, `defects.go:432-442`). It also reopens a final record when the case fails again (`defects.go:469-478`). §6.1 defines neither:
  - A later `passed` result leaves every open unit defect of that repository open. Its triage card stays held, or gets triaged, for a bug that is already fixed.
  - The same set failing after the record closed is matched by the dedup key and "plans nothing", so a regression is silently lost.
  - Set `{A,B}` and then set `{A}` are two defects that triage the same test twice.
- Recommendation: Keep the per-set key, which D-103 §5 requires, and add three rules borrowed from `ReportDefects`:
  - a `passed` result for a repository closes its open unit defects and cancels their unstarted cards (`cancelDefectCardsLocked`, `defects.go:363`);
  - a final record seen again reopens with `Gen+1`;
  - state explicitly that overlapping sets are separate defects, and say that this is accepted.

### Admin-only release depends on two changes the change list does not name - Severity: Major
- Affected scope: §6.3 (guard in `handleMoveCard`; "attributed to the admin in its event"), §10
- Simplicity rationale:
  - `MoveCard` takes a `CardMove` with no actor (`handlers_board.go:62-72`, `board.go:330`). `moveTaskLocked(taskID, to, action)` records no person. D-103's own text calls a board release "an unattributed board move" (decision-log D-103).
  - So "attributed to the admin" needs a new actor on `CardMove` and on the event. `board.go` is not in §10.
  - The store already keeps reason-based unblock refusals inside `MoveCard`, for example `ReasonDesignBlocked` at `board.go:363-377`. Putting the new guard in the handler means reading the card before the store call, which leaves a window, and it splits the "who may unblock" rules across two layers.
  - §6.3 also does not say whether a non-admin may Cancel a held card.
- Recommendation: Put the check next to `board.go:363-377`. Pass an `Actor`/`Admin` on `CardMove` from `handleMoveCard` and record the actor on the move event. Add `board.go` to §10. State the Cancel rule.

### The `autoClaimLocked` guard is dead code - Severity: Nit
- Affected scope: §6.3, first bullet
- Simplicity rationale: The auto-fix loop considers only READY cards (`autofix.go:311`). An unblock clears `BlockReason` (`board.go:395-401`). So a card whose reason is `ReasonUnitHeld` is always BLOCKED, never READY, and the guard can never fire.
- Recommendation: Drop it, or keep it and label it plainly as defence in depth. Either way, its test cannot fail.

### `UnitQAConfig.AttestedAt` and `AttestedBy` are stored but never read - Severity: Nit
- Affected scope: §3 struct, §4.3
- Simplicity rationale: The runner re-checks the live attestation at every start (§4.3), so nothing reads the stored `AttestedAt`. `AttestedBy` repeats the actor that `RecordUpdated` already carries (§3). The claim that the runner "re-reads the capabilities at each start" is also not literally true:
  - `capabilities` is cached for 10 minutes and keeps the last known set on an error (`qaclient.go:57-95`);
  - it returns `map[string]bool`, so carrying the `sandbox` object changes the `qaJobClient` interface (`qajobs.go:146`) and its test fake (`qajobs_test.go:28`).
- Recommendation: Remove both fields, or name the reader of each. Say "within the 10-minute capability cache" and list the interface change.

### The daily cap is an environment variable, unlike the existing per-product caps - Severity: Nit
- Affected scope: §6.1 `GAOS_UNIT_DEFECTS_PER_DAY`
- Simplicity rationale: The precedent, `ReviewFixCap`, is a field on `AutoFix` and is edited on the same Auto-fix page that §3 already extends (`model.go:463-468`). An environment variable is one more place for an operator to look, and it applies to every product at once.
- Recommendation: Make it `AutoFix.UnitDefectCap`, with 0 meaning the default of 5 and a maximum of 20, mirroring `ReviewCap()`. Or say why it is per deployment.

### The change list is incomplete - Severity: Nit
- Affected scope: §10 against §§3, 6.2, 7
- Simplicity rationale: §10 names 17 files besides the tests: `model.go`, `store.go`, `store_arcade.go`, `labels.go`, `prflow.go`, `braintown_consumer.go`, `qajobs.go`, `qaclient.go`, `unitdefect.go`, `defects.go`, `autofix.go`, `handlers_board.go`, `handlers_autofix.go`, `autofix_view.go`, `templates/backoffice.html`, `schema/21-unit-defects.sql` and `artifacts/v1/gaos-arcadedb-model.html`. The text also changes files that are not listed:
  - `github.go`, for `ghPR.MergeCommitSHA` (§3);
  - `pullrequests.go`, for the kind constant, `prRepos` and the allow-list (§7);
  - `board.go`, if the release is attributed (§6.3);
  - the code that builds the runner, which must give it a `codeSource` for `buildReviewContext` (`reviewdefect.go:319`) — it holds only a `prSource` today (`qajobs.go:173`);
  - the register pages (`decision-log.html`, `brd.html`, `qa-test-plan.html`), for D-104, BR-63 and TC-839 onward.

  That is about 20 code files plus 3 register pages. The design states no target count, so nothing fails, but rule 3 of the contract is not met.

  The other §8 numbers hold. Labels: 19 entries in `labelSet` (`labels.go:95-115`) plus 1 makes 20. Schema 21 is next after `20-release-b.sql`. TC-839 is next after TC-838 (`mergedrun_test.go:257`). D-104 and BR-63 are unused.
- Recommendation: Add the missing files to §10.

### A real alternative is missing: the QA tool reports the failures through the existing defect intake - Severity: Nit
- Affected scope: §11
- Simplicity rationale: The QA tool already pushes defects to GAOS through `ReportDefects`, which has its own validation, deduplication, the 50-per-day cap and the `NeedContext` two-step (`defects.go:384-505`). The design chooses polling and parsing in the runner, which is defensible because the job is bound to a repository and a SHA. But the obvious reuse option is not stated or rejected.
- Recommendation: Add one row to §11. A sufficient reason would be that `ReportDefects` requires a known QA case (`defects.go:427-430`) and has no repository/SHA binding.

### Wrong cross-references - Severity: Nit
- Affected scope: §2 step 1 ("§4.2 fills it"), §2 step 2 (`startAutomation`, `qajobs.go:63`)
- Simplicity rationale: §3 fills the commit, not §4.2. `startAutomation` is at `qajobs.go:95`; line 63 is in `autoMergeOutcome`.
- Recommendation: Fix the two references.

**Sections that hold up:**
- The outcome table in §5 is sound and matches `final()` (`qajobs.go:69-77`).
- Reusing born-BLOCKED planning through `planDefectCardLocked` (`defecttriage.go:78`, `defecttriage.go:125-132`) is the right call. BLOCKED is not touched by `readyDefectCards` (`defecttriage.go:798`).
- The §11 rejection of "hold the card in Needs you" is fair.
- The design stays at the level of interfaces; I found no altitude drops worth flagging.

Counts: B=1 M=6 N=6
