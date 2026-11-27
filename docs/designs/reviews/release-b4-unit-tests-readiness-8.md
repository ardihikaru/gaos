# Readiness review: `docs/designs/release-b4-unit-tests.md` (rev 8)

Reviewer lens: Tech Lead, zero-decision rule. I read the design, the rubric (`docs/rules/gaos-design-review.md`, `docs/designs/README.md`), the three domain registers and the code under `dashboard/`. I opened nothing under `.claude/`. Where the brief told me to, I treated v0.66.1 (`qaOutbound.usable()`, TC-850) as already on main.

**Checked and solid:**
- **Identifiers.** D-105 follows D-104, the last decision-log entry (`decision-log.html:2154`). BR-64 follows BR-63 (`brd.html:1309`). TC-851 to TC-875 start right after TC-850.
- **Counts.** 25 cases (3+4+5+7+5+1). Go tests 713 → 716 → 720 → 725 → 732 → 737 → 738. The `dashboard/` total starts at 688 (687 in `README.md:143`, plus TC-850). Labels go 19 → 20 (`labels.go:119-140`). The "Sixty-three" texts are at `brd.html:1218` and `:1522`.
- **Phase verification commands.** Every phase has an exit command and an observable exit condition.

---

### API keys are lower-cased on the false premise that Brain Town keys are - Severity: Blocker
- Affected scope: §4.2 table, row "`apiEvent` / `apiKey`, every PR kind" (line 143); TC-856.
- Finding: The design lower-cases owner and repo in every API PR key "(Brain Town keys are lower-cased the same way)". They are not:
  - `btConsumer` builds `PREvent{Org: org, Repo: repo}` from `trimField(f.OrgLogin…)` / `trimField(f.ProductKey…)` with no case fold (`braintown_consumer.go:533-551`).
  - `PREvent.Key` does not fold case either (`braintown_consumer.go:122-125`).
  - `prflow.go` has no `ToLower` on the key.

  Followed literally, the change gives an API request for a repository with any capital letter a different key (and card id, `prCardID`) from Brain Town's card. That breaks adoption for **every** PR kind (`pr-review`, `unit-test`, `auto-create`), so the reviewer card is planned twice and runs two model turns. It also defeats the "equal to Brain Town's" guarantee this design relies on.
- Recommendation: Pick one rule and write it down:
  - (a) fold the key in both planners (Brain Town and API), plus a stated migration or compatible lookup for existing mixed-case keys; or
  - (b) keep keys as received and make `apiLookupLocked` / `planUnitQASpecLocked` compare keys case-insensitively.

  Fix the premise text, and add a mixed-case Brain Town card to TC-856.

### `qaJobReason` changes signature, but its callers are said to stay unchanged - Severity: Major
- Affected scope: §5.4 (lines 251-259); §9 phase 1 (line 485); §12 row "existing tests that change" (`stagingcreate_test.go:138 is unchanged (it keeps qaJobReason3)`).
- Finding: `qaJobReason(id, caseCode, text string)` is defined at `qajobs.go:303`. It is called at `qajobs.go:393, 429, 448, 456`, `qajobs_test.go:269, 282, 283` and `stagingcreate_test.go:138`. If `qaJobReason` now takes a `qaReason` struct, all eight calls stop compiling unless they are renamed to `qaJobReason3`. So "the wrapper keeps today's callers unchanged" is false, and so is "stagingcreate_test.go:138 is unchanged".
  - Phase 1 also puts `qaJobReason` in `unitqa.go`, but `qajobs.go:303` is not listed for removal, so the symbol would be declared twice.
- Recommendation: Choose one:
  - keep the name `qaJobReason` for the 3-argument form and call the struct form `qaJobReasonOf(qaReason)`, so nothing changes; or
  - list every call that is renamed and the removal of `qajobs.go:303-308` in phase 1.

  Correct the §12 row to match.

### The type of `watch` / `watched` is contradictory, and the store cannot see what TC-861 asserts - Severity: Major
- Affected scope: §5.2 (`qaStartIn` gains `Watch []string`); §6 and §11 (`watch` = up to 500 `{test, file}` pairs); §7.4 `PassUnitDefects(…, watched map[string]string, …)`; TC-861.
- Finding:
  - `Watch []string` cannot carry the `{test, file}` objects the §11 contract specifies.
  - `watched map[string]string` leaves the key encoding undecided (`test\tfile`? test only?).
  - The rule "`count` ≠ 1, a different file, or absent → `missing`" has no stated home, runner or store. TC-861 is a store test (phase 3) that asserts the count and file behaviour, but a `map[string]string` of statuses carries no count. Two engineers would build different wire bodies and put the rule in different places.
- Recommendation:
  - Define `type qaWatchPair struct{ Test string \`json:"test"\`; File string \`json:"file"\` }` and `qaStartIn.Watch []qaWatchPair \`json:"watch,omitempty"\``.
  - State that the runner applies the `missing` rule and passes `watched map[qaWatchPair]string` (or a stated `test+"\t"+file` key).
  - Move the count and file assertions to TC-867 (runner), and keep TC-861 on what the store receives.

### Marking a held defect as passed repeats on every later passing run - Severity: Major
- Affected scope: §7.4 "Passed" bullet (line 438); §5.7 last notice row.
- Finding: For a held defect, `PassUnitDefects` appends `watched tests passed at <sha12> (job <jobID>)` to `Excerpt` and returns its h8 in `UnitHeldPassed`, and the runner sends a group notice. Nothing records that the defect was already marked: `UnitMergedAt` is not advanced on this path, and there is no flag. So every later merge on that base whose watched tests pass appends another line and sends another notice until an admin acts. How the append fits within `defectMaxExcerpt` (4 KiB) is not stated either.
- Recommendation: Add a stored marker, for example `UnitPassedAt int64` (schema 21) or advancing `UnitMergedAt`. Mark and notify only when it is unset. Say the line replaces an earlier one, and state the cut. Add a second pass to TC-861 that asserts one notice and one line.

### `endStaleUnitQACards` in `Tick` cancels cards that are only waiting for the concurrency cap - Severity: Major
- Affected scope: §5.6 "Stale cards" (line 296), against §4.2 "no merge is dropped" and §14 "nothing is dropped".
- Finding: The `Tick` call site ends **every** PENDING `unit:qa` card whose `planned:` is over 24 h old. The parenthesis "(covers GitHub off, `r.unit == nil`)" suggests the intent is narrower, but the rule as written also cancels cards queued behind `GAOS_UNIT_QA_OPEN` or the 2-per-tick budget during a merge burst or with long jobs. One engineer would sweep unconditionally, another only when `r.unit == nil`.
- Recommendation: State the condition exactly. For example, the `Tick` sweep runs only when `r.unit == nil`, and the `main.go` else-branch sweep covers no runner. Alternatively, accept the drop and change "nothing is dropped" to say so. Add the queued case to TC-866.

### Nothing names who writes `planned:<epoch>` - Severity: Major
- Affected scope: §5.4 ("A PENDING `unit:qa` card's reason is `planned:<epoch>`, written by the planner"); §12 rows for `prflow.go`, `api_cards.go` and `unitqa.go`.
- Finding:
  - No function or file is given. Brain Town planning and `PlanAPICard` both go through `planCardsLocked` (`api_cards.go:377`), and neither the `prflow.go` nor the `api_cards.go` change row mentions setting `BlockReason`.
  - It is not stated what `endStaleUnitQACards` does with a PENDING `unit:qa` card that has no parseable `planned:` (for example a card planned before this release, or after a switch flip).
- Recommendation: Name the site, for example `planUnitQASpecLocked` sets `BlockReason = "planned:"+now` on the new card for both callers, phase 2. Add it to the `unitqa.go` row. State the rule for a missing or invalid stamp (for example, treat the card's `TaskPlanned` time as the stamp, or never sweep it).

### `ResolveUnitQACard`'s admin-only cancel has no admin parameter and is built three phases before its caller - Severity: Major
- Affected scope: §5.5 signature (line 277) and table row "RUNNING (an auto-fix admin only; `Admin` passed as in §7.3)"; §9 phase 1 ("with the admin cancel of a RUNNING card").
- Finding:
  - `ResolveUnitQACard(ctx, productID, taskID, to, reason, rule string)` takes no admin flag, and `CardMove.Admin` only arrives in phase 5. So it is undecided how `resolveUnitQACardLocked` learns the caller is an admin, and whether the public `Store` method must refuse RUNNING → CANCELED outright. If it does not, any runner-side caller can stop a running job.
  - It is also not said whether `MoveCard` routes only `:unit-qa` cards or every `qaToolDriven` card. Step (1) refuses e2e keys with "Not a unit-test card.".
- Recommendation:
  - Give the locked function `admin bool`, and have the public method always pass `false`, so it refuses RUNNING → CANCELED.
  - Move the admin-cancel row to phase 5, beside the `MoveCard` guard.
  - Say that `MoveCard` routes only cards whose key ends `:unit-qa` and leaves e2e cards as today.

### The `UnitBase` re-check has no stated source for `m.UnitQA`, for the effective base, or for its scope - Severity: Major
- Affected scope: §7.5 table rows `autofixpr.go:90` and `autofix.go:350-361`.
- Finding:
  - Both `fixEligible` callers (`autofixpr.go:166`, `:490`) hold only `af` from `p.st.AutoFixOf`, not a `Master`, so "pass `m.UnitQA`" needs a read the design does not name.
  - "Resolve `effectiveBase` (`repoDefault` when empty)" does not say which client. `defectFixer.gh` is a `*ghWriter` (`githubwrite.go:178`); the claim loop has a `codeSource` `src`, which has no `repoDefault` in its contract.
  - It is not said whether the read happens only for `unit` defects. Read for every defect, it adds a GitHub call and a new failure mode ("the repository's default branch could not be read") to every e2e and review fix.
- Recommendation: State all three:
  - the read is `configuredProduct(ctx, st, productID)` or `st.Master`;
  - the client is `p.gh.repoDefault` in `autofixpr.go`, and a named interface assertion on `src` in the claim loop;
  - the base is resolved **only when `d.Kind == DefectKindUnit`**.

  Add "an e2e fix with an empty `BaseBranch` makes no default-branch read" to TC-873.

### The 30-day retention lives in a file the change list does not name - Severity: Major
- Affected scope: §12 row `dashboard/defects.go` ("the 30-day retention empties `UnitTests`"); §7.1 `UnitTests` row.
- Finding: Retention is `MemStore.ExpireExcerpts` at `fixprpoll.go:490-502`, not in `defects.go`, and `fixprpoll.go` is not in the change list. A write allowlist built from §12 would refuse the edit. The function also only runs its loop body when `d.Excerpt != ""`. A `@build` defect with an empty `Message` keeps a non-empty `UnitTests` and is never emptied.
- Recommendation: Add `dashboard/fixprpoll.go` (`ExpireExcerpts`) to §12. State the condition as `(d.Excerpt != "" || d.UnitTests != "") && now-d.LastSeen ≥ 30 days`.

### `unitQA` will not be read back from ArcadeDB unless the explicit Product select names it - Severity: Major
- Affected scope: §4.1 ("stored on the Product document as `unitQA` (schema-less, like `repoKinds`)"); §12 row for `store_arcade.go`.
- Finding: `arcadeProductSelect` (`store_arcade.go:2329-2335`) is an explicit column list, and its own comment says a schema-less property "must be named here or it is never read back". There is also the decode struct (`store_arcade.go:1636`) and the write map (`:960`). §12 names the explicit lists for the seven Defect properties but not these three Product sites.
- Recommendation: Name `arcadeProductSelect`, the decode struct field `UnitQA UnitQAConfig \`json:"unitQA"\`` and the `props["unitQA"]` write in the `store_arcade.go` row (phase 1). TC-853 already round-trips it.

### Restricting `Tick` to QA-runner keys is a fourth unstated change when the switch is off, and it strands cards - Severity: Major
- Affected scope: §2 last paragraph ("three stated exceptions"); §4.4 ("`Tick` … starts or adopts only cards whose key is a QA-runner key"); §14.
- Finding: Today `Tick` drives any card with an `e2e:*` label (`qajobs.go:252-262`). After the change, a card an operator labelled `e2e:run` on a non-runner key is neither started by the runner nor dispatched (`labelsAllowDispatch` refuses `e2e:*`, `labels.go:182`). It stays PENDING or READY for ever. This is not among §2's three exceptions, is not in §14, and no test covers it.
- Recommendation: List it as a fourth exception and decide the outcome. Either refuse `e2e:*` labels in `setTaskLabelLocked` on cards whose key is not a QA-runner key, or say the card stays stranded and the board shows why. Add a case to TC-857.

### §12 lists `fixEligible` test callers that do not exist - Severity: Nit
- Affected scope: §12 row "existing tests that change" ("the `fixEligible` callers in `dashboard/autofix_test.go`/`fixpr_test.go`").
- Finding: No test calls `fixEligible`. The only callers are `autofixpr.go:166` and `:490`.
- Recommendation: Drop that clause. The 17 `checkFix` calls in `fixpr_test.go` are correct.

### `MarkUnitNotice` is missing from the read-only conformance list - Severity: Nit
- Affected scope: §5.4 (says `MarkUnitNotice` is "in the conformance table"); §12 `store_conformance_test.go` row (lists four methods).
- Finding: §12 lists four methods for the read-only table and leaves out `MarkUnitNotice`, which §5.4 says is in it.
- Recommendation: Add `MarkUnitNotice` to the §12 list.

### The retry's re-check uses a fixed rule name - Severity: Nit
- Affected scope: §5.5 "The retry" ("false → CANCELED, rule `unit-not-target`").
- Finding: §5.1 says the failing condition's own rule and text are used (`unit-off`, `unit-not-ready` or `unit-not-target`). §5.5 fixes `unit-not-target` instead.
- Recommendation: Say "with the failing condition's rule and text, as §5.1".

### The same outcome gets two final texts - Severity: Nit
- Affected scope: §7.2 step 0; §6 last paragraph; §5.4 adoption `unitText`.
- Finding:
  - Gate step 0 ends the card `unit tests switched off`, while §6 says every refusal ends `<total> unit test(s) failed; no defect filed (<why>)`.
  - After adoption, a `unit:build` marker gives `unit tests failed; defect build`, where the live text is `the build failed; defect build`.
- Recommendation: Pick one text per outcome and use it in all three places.

### `endStaleUnitQACards` cannot send the notice it is listed as sending - Severity: Nit
- Affected scope: §5.6 signature `endStaleUnitQACards(ctx, st Store, now int64) Result`; §5.7 row 5.
- Finding: The function takes no notifier, so it cannot send its "one notice per product". It is also not said which card that one notice names.
- Recommendation: Add an `nt *notifier` parameter. Say the notice names the oldest cancelled card.

### Some symbols' files are not stated consistently - Severity: Nit
- Affected scope: §6 `qaJob.Unit` / `qaUnitResult`; §5.4 "(in `unitqa.go` and `unitdefect.go`)".
- Finding:
  - `qaJob.Unit`, `qaUnitResult` and their json tags (`json:"unit"`) are not in the `qajobs.go` change row.
  - §5.4 puts the reason constants in two files, while §9 and §12 put `ReasonUnitHeld` in `unitqa.go`.
- Recommendation: Add the first to the `qajobs.go` row. Say all three constants live in `unitqa.go`.

### Accepting the client's own card under either key covers only one direction of the switch - Severity: Nit
- Affected scope: §4.2 row "`apiLookupLocked`, own-card branch".
- Finding: The either-key acceptance applies only to a *mapped* request. After the switch is turned off, a client retrying against its own `:unit-qa` card is not mapped, so it still gets `externalId-reused` (`api_cards.go:256-257`). That contradicts "(no `externalId-reused` across a switch flip)".
- Recommendation: Apply the either-key acceptance to every `unit-test` request.

### What the runner does with a `:unit-qa` card in phases 2 and 3 is not stated - Severity: Nit
- Affected scope: §9 phase 2 ("the runner's key-only `Tick`") against phase 4 (unit mode).
- Finding: Between phases 2 and 4, a `:unit-qa` card is a QA-runner key with no unit mode. What `Tick` does with it is not stated (skip it? start it as e2e?).
- Recommendation: Say `Tick` skips `:unit-qa` keys until phase 4 adds the unit branch.

### Phase 4's exit condition leaves the tick count open - Severity: Nit
- Affected scope: §9 phase exits, item 4 ("four PENDING cards start at most three").
- Finding: With 2 starts per tick, a test that runs one tick sees two cards start, not three. The tick count needed is not stated.
- Recommendation: Say "after two ticks, three are RUNNING and one stays PENDING".

Counts: B=1 M=10 N=9
