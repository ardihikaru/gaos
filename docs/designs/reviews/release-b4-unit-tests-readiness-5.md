I reviewed `/home/ardi/devel/projects/ardihikaru/my-projects/gaos-unit-design/docs/designs/release-b4-unit-tests.md` (rev 5) and checked its claims against the code. It is not ready to implement: one Blocker and ten Majors are open. The Blocker is that the design sends several card moves through `ResolvePRCard`, and that function refuses them today.

# Readiness review: `docs/designs/release-b4-unit-tests.md` (rev 5)

Lens: Tech Lead, zero-decision rule. I checked the design against the repository at `/home/ardi/devel/projects/ardihikaru/my-projects/gaos-unit-design`.

**Sections that hold up:**
- **Identifiers:** D-105, BR-64, TC-850, schema `21` and OQ-38 are the correct next values. The decision log ends at D-104, the BRD at BR-63, the QA plan at TC-849, and the highest schema file is `20-release-b.sql`.
- **Counts:** "Labels 19 → 20" matches `labels.go:119-140`. "TC-850 … TC-879: 30 cases" matches the §9 table (4+6+5+6+4+3+2).
- **Change list:** every file it lists as modified exists.
- **Rest of the design:** §7.1, §7.5, §8.2 (as a check list) and §11 are solid.

### `ResolvePRCard` cannot make three of the moves the design sends through it - Severity: Blocker
- Affected scope: §5.3 "Backoff"; §5.4 "Act once, across a restart"; §4.2 "Switch transitions"
- Finding: `resolveFrom` (`prflow.go:600-606`) and `taskTransitions` (`taskstate.go:86-96`) do not allow these moves:
  1. **§5.3:** a passing attestation moves the card "BLOCKED → READY (`ResolvePRCard`, rule `unit-attest`)". `resolveFrom` has no `StatusReady` target, so `ResolvePRCard` refuses it (`prflow.go:613-615`). The function also takes no rule argument; it always uses `RulePRResolved` (`prflow.go:612, 626`).
  2. **§5.4:** the runner writes `unit:<h8|pass|refused>` into the AWAITING_INPUT reason "in the same poll". `resolveFrom[StatusAwaitingInput]` is `{StatusRunning}` only, so a reason rewrite on an AWAITING_INPUT card is refused. §6 says the result does arrive while `awaiting_approval`, so the act-once guarantee that TC-868 asserts fails in exactly that case.
  3. **§4.2:** `SetUnitQA` ends BLOCKED `unit:qa` cards "DONE". `taskTransitions[StatusBlocked]` has no DONE (`taskstate.go:92`), and `resolveFrom[StatusDone]` excludes BLOCKED.

  The obvious fix, widening `resolveFrom`, breaks that table's stated guarantee for every caller: "a card an operator blocked … stays where they put it" (`prflow.go:595-596`). `ReasonUnitHeld` triage cards carry a `DedupKey`, so they would become movable too. The implementer has to invent the mechanism, and the easy choice is unsafe.
- Recommendation:
  - Name one new store method, for example `ResolveUnitQACard(ctx, product, id, to, reason, rule)`. Allow only `unit:qa` cards, only `BLOCKED(ReasonUnitAttest) → READY`, `AWAITING_INPUT → AWAITING_INPUT` (reason only), and `PENDING|READY|BLOCKED → DONE|CANCELED` for switch-off.
  - State the switch-off status. BLOCKED → DONE is impossible, so use CANCELED, or walk BLOCKED → READY first.
  - State that every runner reason for a unit card is built with the full §5.4 grammar: start, wait, resumed and finished.
  - Add the method to §12 and to the TC-865/TC-868 assertions.

### The unit open cap does not limit concurrent runs - Severity: Major
- Affected scope: §4.2 "Own open cap"; §14 Risks
- Finding: `openUnitQACardsLocked` counts only RUNNING and AWAITING_INPUT, and only at planning. `Tick` starts every PENDING and READY QA card it sees, with no limit (`qajobs.go:252-262`). Ten merges planned while nothing runs all start in the same tick, so `GAOS_UNIT_QA_OPEN=3` limits nothing. Meanwhile a merge that arrives while three are running is dropped for good.
- Recommendation: enforce the cap at runner start (count tracked unit jobs per product and leave extra cards PENDING), or count PENDING and READY in the planner cap. Pick one, state which, and test it in TC-878.

### The unit-cap notice has no sender and no card - Severity: Major
- Affected scope: §4.2 last sentence; §5.5 row 4
- Finding:
  - §5.5 says "the runner, on the `unit:qa` card … when the unit cap dropped a merge". A dropped merge has no card.
  - The drop happens inside the store (`planUnitQASpecLocked`), and the store cannot notify. Round 3 #6 already established that for the held-card notice.
  - The runner never learns about planner drops. Nobody is named as owning `qaUnitCapNoticeAt`.
- Recommendation: name the sender. For example, `prFlow.handle` and `apiTaskCreate` read `Result.Skipped` containing `unit-qa-cap` and call `nt.Notify` on a synthetic `TaskRecord{DeskID, DedupKey}`, with `qaUnitCapNoticeAt` held on `prFlow`. State what the notice is keyed on, since there is no card id for `· card <id>`.

### The attestation backoff cannot be driven by the runner as described - Severity: Major
- Affected scope: §5.3 "Backoff"
- Finding:
  - `Tick` skips BLOCKED cards (`qajobs.go:257-262`), and the design adds no BLOCKED branch.
  - "24 hours after the card was blocked (… `lastChangesLocked`)": `lastChangesLocked` is an internal `MemStore` helper that needs the lock held (`board_page.go:298`). It is not on `Store`, and the runner only has `Store`.
  - `qaUnitAttestRetry` lives in memory, so after a restart nothing says whether retries resume or the card stays BLOCKED forever.
- Recommendation:
  - Add an explicit Tick branch for `StatusBlocked && BlockReason == ReasonUnitAttest`.
  - Name a `Store`-level source for the block time, for example a new `CardLastChange(ctx, product, id)`, or a timestamp written into the reason (`… since:<epoch>`).
  - State the restart behaviour.

### The branch-rule check has two homes and no stated ref - Severity: Major
- Affected scope: §8.2 check 0; §4.1 refusal tables; §9 phases 1, 6, 7
- Finding:
  - §8.2 says "`SetUnitQA` with `Create` on reads the same for each target repository and refuses". `SetUnitQA` is a store method (§4.1, phase 1); it cannot call GitHub under the store lock and has no GitHub client.
  - §4.1 lists exactly three store refusals and four handler refusals, and neither list contains this one. TC-877, a route test in phase 7, asserts it.
  - No phase builds `branchRuleTypes`: phase 6 lists only `pullCommits`.
  - At save time no `qa-unit/<sha12>-<job8>` branch exists, and the ref to query is not given.
- Recommendation: move the check into the handler table of §4.1 as a fifth row. Name the probe ref, for example `qa-unit/probe`. Add `branchRuleTypes` to phase 6's build list.

### Board move trimming needs the viewer, but it runs inside viewer-less store reads - Severity: Major
- Affected scope: §7.3 "The board offers only what the store allows"; §12 `sendback.go`, `handlers_board.go`
- Finding: `decorateCardsGate` is called inside `MemStore.Board` (`board.go:270`) and `decorateCardsLocked`/`BoardPage` (`board_page.go:431`). Both are `Store` methods with no viewer parameter (`store.go:127, 132`). "Resolved in the handler that renders the board and passed in" means one of two things:
  - change two `Store` signatures and `ArcadeStore`, which §12 does not list; or
  - post-filter `Moves` in the handler, in which case `decorateCardsGate` does not change.
- Recommendation: choose. Handler-side post-filtering keeps `Store` unchanged. Name the handler or handlers that render the board and the JSON board API, and fix the §12 rows to match.

### The §8.5 inbox refusal depends on facts GAOS does not have - Severity: Major
- Affected scope: §8.5; §12 `inbox_worker.go`
- Finding:
  - "The QA tool's `externalId` names `qa-job-<id>`" is an invented convention. It is not anchored and not in §11's contract.
  - `inboxWorker` has only `store` and `out`. It cannot see the runner's tracked jobs, so "a tracked … `unit:qa` card" is undecidable from there.
  - Delivery has only three outcomes: `DeliveryDelivered`, retry and `DeliveryGaveUp` (`inbox_worker.go:137-146`). "Marked refused" needs a new outcome and store support, and §12 lists neither.
- Recommendation:
  - Add the `externalId` format to §11 as a requirement.
  - Define the lookup: scan `PRCards` for `unit:qa` cards whose `jobIDFromReason` matches.
  - Name the new delivery outcome constant and the store method that records it, and add both to §12.

### `jr.unit = ghc` creates a typed nil when GitHub is off - Severity: Major
- Affected scope: §5.1 (`jr.unit = ghc`, "or `r.unit == nil`")
- Finding: `ghc` is a `*ghClient` that is nil when the GitHub config is off (`main.go:383-397`), and every existing use is guarded by `if ghc != nil`. Assigning a nil pointer to the `unitSource` interface makes `r.unit == nil` false. The "stays PENDING" row is then never taken, and `pullRequest` dereferences a nil client.
- Recommendation: specify `if ghc != nil { jr.unit = ghc }`, and add a GitHub-off case to TC-864.

### The API refusals: default `dispatch`, and where the age check runs - Severity: Major
- Affected scope: §4.2 external-tasks table, the `apiTaskCreate` row; TC-856
- Finding:
  - A missing `dispatch` is read as `true` (`api_tasks.go:57`: `b.Dispatch == nil || *b.Dispatch`). Every mapped `unit-test` request that omits `dispatch` would therefore get 422 `unit-qa-no-dispatch`. That breaks existing clients, and the design does not say whether it is intended.
  - "Before the lookup … a mapped request whose pull request merged more than 7 days ago answers 422 `unit-qa-too-old`": `merged_at` is only known from the GitHub read at `api_tasks.go:212`, which runs after the lookup and only when a card will be created. One engineer adds a GitHub read before the lookup; another checks after it, so an old merge can still adopt an existing card.
- Recommendation:
  - State that a missing `dispatch` means `false` for mapped `unit-test` requests, or that it is refused. Say which, and publish it.
  - State where `unit-qa-too-old` runs (after `prepare`, cards created only), and that adoption is not age-checked.

### Phase exits cannot pass, and the target counts are not stated - Severity: Major
- Affected scope: §9 preamble ("each phase ends with … `./deploy/check-workspace.sh` … green"); §12 last row ("counts and ToC lines")
- Finding:
  - `check-workspace.sh` pins the Go test count (712, `:113-119`), checks that QA rows equal the highest TC (`:105-111`), and checks prose counts in the QA plan, ToC, decision log, BRD and README (`:129-148`).
  - Phases 1–6 add `Test*` functions, but the registers, ToC and README change only in phase 7. Phases 1–6 therefore cannot exit green.
  - The new values are never stated: the Go test count, "eight hundred and seventy-nine", the decision log's "One hundred and three entries" plus one, "Sixty-four rules", and the ToC's decision count.
- Recommendation: either update the counted registers in each phase (with QA rows marked Planned), or exempt `check-workspace.sh` from phases 1–6 and say so. Write every target prose value into §12.

### §10 allocates a different TC range than §9 - Severity: Major
- Affected scope: §10 "QA test plan section `#go-unit-qa`, TC-850…TC-877"; §9 "TC-850 … TC-879: 30 cases"
- Finding: the register line leaves out TC-878 (`TestUnitCapAndPRCapOrder`) and TC-879 (`TestInboxNeverRelaysAUnitApproval`). Those are allocated in §9 but carried by no register row.
- Recommendation: change §10 to TC-850…TC-879 (30).

### The triage table cites the wrong TC - Severity: Nit
- Affected scope: §18, row "`unit-test` role must be assigned …"
- Finding: the row cites TC-853. TC-853 is `TestQAUnitKindCollisionIsRefused`; the `gateUnassigned` exemption is asserted in TC-854.
- Recommendation: cite TC-854.

### `main.go` anchors drifted after the TaskMgr merge - Severity: Nit
- Affected scope: §4.1, §5.1, §7.5, §8.2
- Finding:
  - `newQAJobRunner` is at `main.go:440`, not 425.
  - The `qaOut` guard is at 439, not 424.
  - `POST /products/{id}/autofix` is at 594, not 577.
  - `srv.codeSrc = ghc` is at 390, not 388.
  - `/ui/bo/autofix/live` is at 570, not 554.
  - The poller's `alert` wiring is at 459-475, not 444-456.
- Recommendation: re-anchor.

### Phase build lists and the change list leave items unplaced - Severity: Nit
- Affected scope: §9, §12
- Finding:
  - `PREvent.HeadSHA` and the `prFlow.handle` change are needed by TC-857 in phase 2, but no phase builds them.
  - `unitApprovalBound` is tested by TC-879 in phase 7 but is in no phase's build list.
  - `planUnitQASpecLocked` and `openUnitQACardsLocked`'s env parser are not named in a §12 row.
  - The constants `ReasonUnitHeld`, `ReasonUnitAttest` and `ReasonUnitNoCommit` have no file.
- Recommendation: add each item to its phase and its §12 file row.

### The outcome table has an undecided cell - Severity: Nit
- Affected scope: §6 outcome table
- Finding: `unit.result = failed` with zero kept entries (all screened out) is decided only for `status = succeeded`. It is not decided for `failed` or `awaiting_approval`.
- Recommendation: make the "no kept entry" row "any" status, with FAILED `qa-unit-unreadable`, or state the other cells.

### "New test files of §9" is a description, not a path list - Severity: Nit
- Affected scope: §12 row "new test files of §9"
- Finding: a write allowlist needs paths.
- Recommendation: list `dashboard/unitqa_test.go`, `dashboard/unitdefect_test.go`, `dashboard/unitqa_runner_test.go` and `dashboard/unitqa_row_test.go`.

### Retry counter reset after a manual release is unstated - Severity: Nit
- Affected scope: §5.1 and §5.3 tables (`qaUnitCommitTries`, `qaUnitAttestTries`)
- Finding: nothing says whether a person moving a `ReasonUnitNoCommit` or `ReasonUnitAttest` card to Ready resets the in-memory counter. If it does not, the first transient failure blocks the card again.
- Recommendation: state "reset when the card leaves BLOCKED", and cover it in TC-864/TC-865.

Counts: B=1 M=10 N=6
