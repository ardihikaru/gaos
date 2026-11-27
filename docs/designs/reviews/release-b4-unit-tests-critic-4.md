# Goldfish review of `release-b4-unit-tests.md` (rev 4): critic, structural and style lens

The design does not hold up. Its fix for BR-38 would let model turns run outside the lease. Its register IDs are already used on `main`. Several mechanisms quietly drop the unit runs the design promises.

## Blockers

### The live-turn exemption also exempts the triager and fixer turns - Severity: Blocker
- **Affected scope:** §4.4; §2 row 2; §10 (BR-38 amendment); §12 (`scheduler.go`, `board.go`); TC-847.
- **Finding and rationale:**
  - §4.4 says `liveIn` (`scheduler.go:231-239`) and `liveTurnLocked` (`board.go:452-459`) should ignore any task whose labels `labelsAllowDispatch` refuses. It describes that set as "`e2e:*`, `unit:qa`".
  - That is not the set. `labelsAllowDispatch` (`labels.go:179-188`) also refuses `pr-review:manual`, `pr-review:needs-manual` and every `defect:*` label (`labels.go:182-183`).
  - Defect triage and fix cards are model turns. The auto-fix loop dispatches them (`defecttriage.go:789-792`, `autofix.go:162-181`).
  - So the rule as written removes the lease from exactly the triager and fixer turns §4.4 says it protects. Two model turns could then run in one product at once, which breaks even the amended "one live **model** turn".
  - A card relabelled `pr-review:manual` while RUNNING would also drop its lease mid-turn.
  - TC-847 only covers QA-tool cards, so it would not catch this.
- **Recommendation:** Base the exemption on the cards the QA-job runner drives, not on `labelsAllowDispatch`. For example, exempt only when `isE2ECard(labels)` holds (`e2e:automation`, `e2e:run`, `unit:qa`, `qajobs.go:230-238`). Correct §4.4's description of the refused set. Add a TC asserting that a RUNNING `defect:triage` or `defect:easy` card still holds the lease.

### D-104, BR-63 and TC-839…TC-849 are already taken on `main` - Severity: Blocker
- **Affected scope:** §9 (TC map), §10 (all register rows), §12 register files, §5.5 / §12 `notify.go` and `contacts.go` edits.
- **Finding and rationale:** This worktree is behind `main`. On `main` these IDs already belong to the third-party TaskMgr feature:
  - D-104: `gaos/pustaka/docs/product/decision-log.html:2154-2157`, pointing at `docs/designs/gaos-third-party-taskmgr.md`.
  - BR-63: `gaos/pustaka/docs/product/brd.html:1309`.
  - TC-839…TC-849: `gaos/pustaka/docs/product/qa-test-plan.html:930-940`.
  - OQ-41, OQ-44 and OQ-45 already cite D-104 (`open-questions.html:127-131`).

  Code the design edits has also moved on `main`:
  - `loginFor` now has a TaskMgr branch (`gaos/dashboard/notify.go:40`).
  - `ValidContact`'s text is now built by `notifyKindList()`, and `notifyKinds` has D-104 entries (`gaos/dashboard/contacts.go:67-70, 101`).

  Every register row, every TC number and the §5.5/§12 notice edits would collide or apply to code that no longer exists. The worktree's own registers stop at D-103 and BR-62 (`gaos-unit-design/pustaka/docs/product/decision-log.html:2148`, `brd.html:1308`), which is why this was missed.
- **Recommendation:** Rebase onto `main` and give this design the next free IDs: D-105, BR-64, TC-850 onward, and re-check `notifyKinds`/`loginFor`. Then update §9's TC map, the "28 cases" line, §10 and the §17 note.

## Majors

### The pull-request cap still refuses `unit:qa` cards - Severity: Major
- **Affected scope:** §4.2 "Own open cap"; §14 Risks ("never toward `GAOS_PR_MAX_OPEN`").
- **Finding and rationale:**
  - Excluding `unit:qa` from `openPRCardsLocked` only changes what is counted.
  - The gate at `prflow.go:298` still refuses the whole event when `len(fresh) > 0 && openPRCardsLocked >= prMaxOpen()`.
  - A `pr.merged` event's only fresh spec is the `unit:qa` one (`prflow.go:88-91`). So when review cards fill `GAOS_PR_MAX_OPEN`, every merge's unit run is dropped (`ErrPRSkipped: cap`, `prflow.go:300`).
  - The cursor then moves past the event (`prflow.go:488-489, 508`), so the merge is never tested.
- **Recommendation:** Say that the `prMaxOpen` gate applies only when the fresh specs include something other than `unit:qa`. Add that case to TC-843.

### The unit open cap silently drops merges, and blocked cards use it up - Severity: Major
- **Affected scope:** §4.2 (`GAOS_UNIT_QA_OPEN`); §5.1/§5.3 BLOCKED outcomes; §4.2 switch transitions; §14 Risks ("nothing is silently dropped").
- **Finding and rationale:**
  - `openUnitQACardsLocked` counts every non-final `unit:qa` card, BLOCKED ones included.
  - A QA-tool outage of about three minutes blocks each starting card with `ReasonUnitAttest` (§5.3).
  - Three such cards (the default is 3) and every later merge is dropped with `Skipped = "unit-qa-cap"`. The only trace is a log line.
  - Unit testing stays off until a person clears the cards one by one. That contradicts the risk section's "nothing is silently dropped".
  - Cards left BLOCKED after a switch-off are only ended if they are PENDING (§4.2), so they still use up the cap when the switch comes back on.
- **Recommendation:** Pick one of these:
  - count only RUNNING and AWAITING_INPUT cards toward the cap;
  - or end attestation-blocked cards automatically on a backoff;
  - or send a `unit:failure` notice for each dropped merge.

  Also say what happens to READY and BLOCKED `unit:qa` cards at switch-off.

### The `unit-test` role must be assigned to an agent that never runs - Severity: Major
- **Affected scope:** §4.2 ("The `unit-test` role is still gated by assignment"); §4.1 page; §7.2 gate.
- **Finding and rationale:**
  - On a configured product, `gateUnassigned` (`prflow.go:294, 361-380`) drops any spec whose role does not run.
  - The `unit:qa` spec keeps `Role: "unit-test"`. So unless the operator assigns an agent seat to `unit-test`, no `unit:qa` card is ever planned, even though no agent will work it.
  - Nothing on the Unit tests section warns about this, and §7.2's gate checks fixer and triager but not `unit-test`.
  - An operator who reads "the QA tool runs unit tests" and unassigns `unit-test` turns the feature off without knowing.
- **Recommendation:** Do one of two things:
  - exempt `unit:qa` specs from `gateUnassigned`, since the runner checks the QA tool at start;
  - or have the handler refuse `enabled` while `unit-test` is unassigned, and show that on the page.

  Either way, add a TC.

### The API path skips the cross-switch check and the unit cap - Severity: Major
- **Affected scope:** §4.2 "One card per merge across the switch", "Own open cap", the external-tasks API table; TC-843, TC-845.
- **Finding and rationale:**
  - `PlanAPICard` (`api_cards.go:320-389`) plans through `planCardsLocked` and never calls `planPullRequestCardsLocked`.
  - The two-key check and `openUnitQACardsLocked` exist only in `planPullRequestCardsLocked`.
  - `apiLookupLocked` looks up only `apiKey(m, …)`, which is `:unit-qa` once the switch is on.
  - So if Brain Town planned `:unit-test` before the switch, an API request after it creates a second card for the same merge. The `GAOS_UNIT_QA_OPEN` cap never applies to API cards.
- **Recommendation:** Apply the two-key lookup and the unit cap inside `PlanAPICard`, or in a helper both planners call. Extend TC-845 with an API request after the switch flips.

### Unit defects are not tied to their base, so a pass on one branch closes another branch's defect - Severity: Major
- **Affected scope:** §7.1 (`CaseID`, `Key`), §7.2 dedup, §7.4 `PassUnitDefects`, §5.1 `AllowedBases`.
- **Finding and rationale:**
  - The key is `unit:<owner>/<repo>@<h>`, with no base.
  - `PassUnitDefects` selects defects by product and repository only.
  - So a later merge into an allowed release branch that passes the same tests marks `main`'s defect fixed and cancels its held card, while `main` still fails.
  - The other way round, the same failing set on two bases deduplicates into one defect whose `UnitBase` jumps to "the newer of the two merges". One base's fix is then never opened.
- **Recommendation:** Put the base in the identity, as `unit:<owner>/<repo>@<base>@<h>`. Add `base` to `PassUnitDefects`, filter on `UnitBase == base`, and test it in TC-851.

### Keeping only 2000 passed test names makes "a pass closes the defect" unreliable - Severity: Major
- **Affected scope:** §6 (`Passed` capped at 2000), §7.4, BR-63's last sentence.
- **Finding and rationale:**
  - `Passed` is cut to 2000 names "before screening and hashing", and no order is given.
  - A suite larger than 2000 tests is common. For such a suite, a defect's names will often not be in the kept list, so the defect never closes on a pass.
  - BR-63 states closing as a rule, but the mechanism can't deliver it for large suites.
- **Recommendation:** Check membership against the full list before truncating, or define a sorted truncation and say the limit. Better: have the QA tool answer for the specific names (send `UnitTests`, receive per-name status). State the limit in BR-63 or D-105's residuals.

### "Checked at the current head" compares against a stale stored head - Severity: Major
- **Affected scope:** §8.2, §8.3 `ownUnitPRLocked`, §8.2 "*unchecked*".
- **Finding and rationale:**
  - `HeadSHA` only moves when the poller runs `AdvancePullRequest`.
  - The poller visits a row every 5 minutes for the first 7 days, then hourly (`fixprpoll.go:24-26, 332-335`), and stops after 90 days (`pullrequests.go:30`, `fixprpoll.go:328-331`).
  - A push followed by a quick merge reaches `ownUnitPRLocked` through Brain Town before the poller has seen the new head. The note `checked@<old>` still matches the stored old `HeadSHA`, so the merge is treated as checked and its unit run is skipped.
  - The page shows nothing as *unchecked* for the same reason.
  - After 90 days, nothing is re-checked at all, which contradicts "at every new head".
- **Recommendation:** In `ownUnitPRLocked`, compare the note with the head GitHub reports at merge time. `prFlow.handle` already reads the PR (`prflow.go:456-461`); pass `got.Head.SHA` on the event. State the 90-day limit, or exempt `qa-unit` rows from it.

### BR-63's "generated tests never file a defect" stops being true once they are merged - Severity: Major
- **Affected scope:** §2 row 3, §6, §8.3, §10 BR-63 and D-104.
- **Finding and rationale:**
  - Once a `qa-unit` pull request merges, its tests are part of the "pre-existing suite" at the next merge's commit.
  - §8.3 also skips the unit run at the `qa-unit` merge itself. So the first run that executes the generated tests is the next unrelated merge, and a broken generated test files a defect against that merge's author.
  - The register text promises the opposite.
- **Recommendation:** Run `unit-run` at the `qa-unit` merge (drop the unit spec from `ownUnitPRLocked`; skip only e2e). Reword BR-63 to "within the job that generated them".

### The runner's GitHub interface cannot do what §5.1 asks - Severity: Major
- **Affected scope:** §5.1 (effective base, 403 rate-limit retry), §12 (`github.go`, `prflow.go`, `qajobs.go` rows).
- **Finding and rationale:**
  - The runner's `src` is `prSource` (`qajobs.go:173`, set from `flow.gh` at `main.go:425`). That interface has only `pullRequest` and `pullFiles` (`prflow.go:394-397`).
  - `repoDefault` exists only on `*ghClient` (`github.go:414`).
  - The rate-limited 403 is detected inside `get` (`github.go:375`), but `ghError` keeps only `Status`, `Retry` and `Draft` (`github.go:309-316`). The runner cannot tell a rate-limited 403 from a permanent one, and `lookupTransient` treats every 403 as permanent (`prflow.go:430-436`).
  - None of these interface changes is in §12.
- **Recommendation:**
  - Add `repoDefault` (and `pullCommits`, if the runner needs it) to `prSource` or to a new runner interface, and update the fakes.
  - Add `Limited bool` to `ghError`, set where `limited` is computed, and read it in a `unitTransient`.
  - List all three in §12.

### An approval timeout after a reported result has no legal path to DONE - Severity: Major
- **Affected scope:** §6 ("An approval timeout or a later job failure after that keeps the reported result … not FAILED"), §10 BR-44 amendment.
- **Finding and rationale:**
  - Today the approval timeout, the active timeout and the flapping limit all call `r.fail` and end FAILED (`qajobs.go:439-443, 459-469`).
  - `resolveFrom[StatusDone]` does not include AWAITING_INPUT (`prflow.go:604`). Today's success path gets there only through a RUNNING hop (`qajobs.go:429`).
  - BR-44 allows a runner-placed card to leave AWAITING_INPUT only by "the job resuming (RUNNING), failing, or a cancel" (`brd.html:1304`). The §10 amendment only extends who may put a card there, not how it leaves.
  - The design does not name which of the three timeout and flapping branches change, or how the card moves.
- **Recommendation:**
  - Name the three branches.
  - Say that with `unitDone` set, the runner cancels the job and moves the card AWAITING_INPUT → RUNNING → DONE with the §6 text.
  - Amend BR-44's exit list.
  - Add the case to TC-856.

### "The operator delegated approval" has no anchor, yet the design amends four business rules - Severity: Major
- **Affected scope:** §0 ("The operator delegated the approval of this design to the agents' review (2026-11-27)"); §10.
- **Finding and rationale:**
  - Rule 2 of `docs/designs/README.md:22-23` requires every claim to carry a file and line, or be labelled an assumption. This claim does neither.
  - The policy makes approval the operator's (`gaos-design-review.md:34, 89-90`).
  - Meanwhile the design amends BR-38 (a core invariant from D-18/D-060), BR-44, BR-54 and D-091 (8). The BR-38 change is also the Blocker above.
- **Recommendation:** Anchor the delegation (a decision-log entry or a saved message). Otherwise, put the BR-38 and BR-54 amendments to the operator explicitly at step 8.

## Nits

### The notice kind count is wrong - Severity: Nit
- **Affected scope:** §5.5 "lists all seven kinds"; §12 `contacts.go` row.
- **Finding and rationale:** `notifyKinds` already has seven entries (`contacts.go:67-69`). Adding `unit:approval` and `unit:files` makes nine. On `main` the text is generated instead (`gaos/dashboard/contacts.go:101`).
- **Recommendation:** Say "every kind in `notifyKinds`", or reuse `notifyKindList()` after the rebase.

### The handler identity source differs from the sibling handlers - Severity: Nit
- **Affected scope:** §7.3 (`s.auth.sessionFrom(r)`).
- **Finding and rationale:** `handleSaveAutoFix` and `handleCloseDefect` use `sessionOf(r)` (`handlers_autofix.go:117, 159`). `sessionFrom` (`auth.go:380-395`) falls back to the cookie. Using two identity readers for one admin decision invites drift.
- **Recommendation:** Use `sessionOf(r)` in all three handlers, or say why `handleMoveCard` needs the cookie fallback.

### `pullCommits` is not "on `prSource`" for the poller - Severity: Nit
- **Affected scope:** §8.2 check 4.
- **Finding and rationale:** The poller holds a `*ghClient` (`fixprpoll.go:139`), not a `prSource`.
- **Recommendation:** Say `(*ghClient).pullCommits`, and add it to `prSource` only if the runner needs it.

### §13 says the API change breaks no published contract, but adds a 422 - Severity: Nit
- **Affected scope:** §13 last row; §4.2 `unit-qa-no-dispatch`.
- **Finding and rationale:** A client that sends `dispatch: true` today gets a new 422 on switched-on products.
- **Recommendation:** Say "changes the published contract by one refusal code, documented in §12".

### BR-42 is cited for "GAOS runs no code" - Severity: Nit
- **Affected scope:** §13 first row.
- **Finding and rationale:** BR-42 covers workspace naming and the instruction-file allowlist (`brd.html:1302`), not code execution.
- **Recommendation:** Cite the rule that actually says this, or label it as an assumption.

### Pull requests with more than 250 commits get no verdict - Severity: Nit
- **Affected scope:** §8.2 check 4 ("at most 250").
- **Finding and rationale:** The design does not say what happens when the commit list is truncated.
- **Recommendation:** Add the verdict `commit list incomplete` when the count reaches 250 or differs from `commits`.

## What holds up
- The paths out of BLOCKED in the §7.3 table check out against the code:
  - `legalMove`: `board.go:234-239`
  - drift release: `workspace.go:395`
  - `readyDefectCards` touches PENDING only: `defecttriage.go:798`
  - the QA `xpass` route cannot reach a unit key because of `defectIDRe`: `defects.go:68, 210, 433`
- The TC arithmetic in §9 is correct (4+5+5+6+4+3+1 = 28), and so is the label count of 19 → 20 (`labels.go:119-140`).
- The anchors I spot-checked are accurate: `prflow.go:88-91, 225, 232, 456-461`, `qajobs.go:355-361, 380, 465, 519-521`, `autofix.go:175-177, 187-198`, `autofixpr.go:235-238`, `main.go:474-475, 554, 577-578`.

Counts: B=2 M=11 N=6
