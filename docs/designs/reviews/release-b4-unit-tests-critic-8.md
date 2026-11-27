# Design review: release-b4-unit-tests.md (rev 8), Goldfish critic lens

**Document:** `/home/ardi/devel/projects/ardihikaru/my-projects/gaos-unit-design/docs/designs/release-b4-unit-tests.md`

**Overall:** No blockers, but 10 Major findings and 6 Nits. Most Majors are cheap wording or mechanism fixes. Two need a decision rather than an edit: the mixed-case key regression (finding 1) and the RUNNING cards that stay stuck forever (finding 4).

**Strongest counter-argument to the design's thesis.** The design says it "ships dormant" and that "with `UnitQA.Enabled` false, nothing changes, with three stated exceptions". Neither is true. With the switch off it already changes:
- the live-turn lease for every e2e card;
- boot reconciliation for every e2e card;
- `Tick` start eligibility;
- the API keys of every PR kind (case folding);
- what `ghClient.do` returns.

So a feature that does nothing until another team builds §11 already carries several live behaviour changes, and one of them (the case folding) is a regression. Findings 1, 4 and 9 below.

---

### 1. Brain Town keys are not lower-cased; lower-casing API keys breaks adoption for every PR kind - Severity: Major
- **Affected scope:** §4.2, external-tasks table, the row "`apiEvent` / `apiKey`, every PR kind"; §12 api_cards row.
- **Finding and rationale:**
  - The design says "Brain Town keys are lower-cased the same way". They are not.
    - `btConsumer.handle` builds `PREvent{Org: org, Repo: repo}` from `trimField(f.OrgLogin…)` and `trimField(f.ProductKey…)` (`dashboard/braintown_consumer.go:533-534, 548-551`).
    - `trimField` only drops control characters (`braintown_consumer.go:696-706`).
    - `PREvent.Key` concatenates the fields as they are (`braintown_consumer.go:122-127`).
    - No code path lower-cases `ev.Org` or `ev.Repo`. A grep for that finds only test fixtures.
  - `taskByDedupKeyLocked` is an exact match (`dashboard/prflow.go:168-175`).
  - Effect: on any org whose GitHub login has capitals, an API request after this change no longer finds the Brain Town card.
    - For `pr-review` that means two review cards and two model turns on the same PR. Today adoption works because both sides keep GitHub's casing.
    - For `unit-test` it means two `unit:qa` cards and two sandbox runs.
  - This is a regression with the switch off, and it is missing from §2's exception list.
- **Recommendation:** Pick one rule and apply it to both producers:
  - either lower-case `Org`/`Repo` in `btConsumer.handle`, and migrate or dual-look-up existing keys;
  - or drop the API lower-casing and use a case-insensitive key lookup (`strings.EqualFold`) in `planUnitQASpecLocked` and `apiLookupLocked`.

  Then remove the false sentence and list whichever choice is made in §2.

### 2. An older merge's failure, finishing after a newer pass, reopens or creates a defect that is already fixed - Severity: Major
- **Affected scope:** §7.4 "Seen again, final"; §7.2 dedup; §5.6 (up to `GAOS_UNIT_QA_OPEN` concurrent jobs).
- **Finding and rationale:**
  - Round 2 guarded one direction: an older pass may not close a newer failure (`UnitMergedAt` older than `mergedAt`).
  - The reverse is unguarded. Up to 3 jobs per product run at once, and jobs of different lengths finish out of order.
  - Example: merge A (failing) is still running while merge B (later, passing) finishes and pass-closes the defect. Then A's failure arrives. §7.4 runs the gate and `reopenUnitDefectLocked`, which produces a held regression card and a group notice for a defect fixed at a newer merge.
  - The same thing happens for a *new* key whose failing set was already fixed upstream.
  - The pass-close records `RunRef = <sha12>@<job>` but not the passing merge's `merged_at`, so nothing is available to compare against.
- **Recommendation:**
  - Store the passing merge's time on the defect at pass-close (e.g. `UnitPassedAt`).
  - Refuse a reopen when `in.MergedAt <= UnitPassedAt`. Refuse a new key when a newer run on the same repo and base has already reported its watched pairs passed, or at least when a newer `UnitMergedAt` result exists for the repo and base.
  - Add the case to TC-860 or TC-861.

### 3. BR-64, D-105, §5.5 and §1 still say a pass cancels or closes a held defect; rev 8's §7.4 says it never does - Severity: Major
- **Affected scope:** §10 BR-64 text; §10 D-105 ("no machine move of a held card but the pass cancel"); §5.5 last sentence ("the only machine move of a held card is `cancelDefectCardsLocked` from `PassUnitDefects`"); §1 "Held card" ("a later pass of the same tests cancels it"); §2 row 4.
- **Finding and rationale:**
  - §7.3's last row and §7.4 (rev 8) say a held defect is only *marked*, and an admin confirms the close.
  - BR-64, the business rule this design writes into `brd.html`, says "When every failing test of a defect passes at a later merge on the same base, the defect closes".
  - So the new rule contradicts its own mechanism. An implementer following §5.5 would keep the cancel; one following §7.4 would not. That is two engineers, two results (README rule 6).
- **Recommendation:**
  - Reword BR-64: "…closes, except that a defect whose triage card is still held is marked and closed by an auto-fix admin".
  - Fix D-105, §5.5, §1 and §2 row 4 to match §7.4.

### 4. Runner-driven RUNNING cards can never end when the runner is not running; the e2e cancel path is lost - Severity: Major
- **Affected scope:** §4.4 "Boot reconciliation"; §5.5 admin cancel; §5.6 stale cards; §4.1 switch-off.
- **Finding and rationale:**
  - Today `Reconcile` requeues any RUNNING card with no open turn to READY (`dashboard/runner.go:705-718`). That is the only exit for an e2e card whose runner is gone.
  - Rev 8 makes `Reconcile` skip `qaToolDriven` cards. If GAOS boots with the QA client unusable (`main.go:439-440` never starts the runner), every RUNNING `unit:qa` *and* e2e card stays RUNNING forever:
    - `endStaleUnitQACards` sweeps PENDING only;
    - switch-off leaves RUNNING cards "to finish";
    - `legalMove` refuses cancel from Working (`dashboard/board.go:236-237`).
  - The admin cancel route covers `:unit-qa` keys only: `ResolveUnitQACard` refuses any other key with "Not a unit-test card." So a stranded e2e card has no exit at all. That is a regression to e2e behaviour, and it is not in §14's list of e2e exceptions.
  - A stranded RUNNING unit card also counts against `GAOS_UNIT_QA_OPEN`.
- **Recommendation:**
  - Either skip in `Reconcile` only when the runner will run (pass `qaOut.usable()` in), or have the `else` branch of `main.go` also end RUNNING `qaToolDriven` cards older than their timeout (FAILED `qa-runner-absent`).
  - Make the admin cancel accept every QA-runner key.

### 5. The `planned:<epoch>` clock has no writer in the change list or the phases - Severity: Major
- **Affected scope:** §5.4 ("A PENDING `unit:qa` card's reason is `planned:<epoch>`, written by the planner"); §5.6 `endStaleUnitQACards`; §9 phase 2; §12 `prflow.go` and `api_cards.go` rows.
- **Finding and rationale:**
  - Cards are created by `planCardsLocked` from a `cardItem`. `cardItem` has no reason field (`dashboard/api_cards.go:21-28`).
  - Neither planner path (`planPullRequestCardsLocked`, `PlanAPICard`) is listed as writing a PENDING `BlockReason`.
  - Phase 2's build list does not name it, and phase 4 builds the sweep that depends on it.
  - Without it, the 24-hour end for "GitHub off" and "no runner" (round 7's Major) never fires.
- **Recommendation:** Add `cardItem.Reason` (or set the reason after planning in `planUnitQASpecLocked`) to phase 2 and to the `prflow.go` and `api_cards.go` rows. Have TC-854 assert the reason.

### 6. The 24-hour PENDING sweep cancels a healthy backlog, contradicting "nothing is dropped" - Severity: Major
- **Affected scope:** §5.6 "Stale cards"; §14 "Many merges at once" ("Cards queue PENDING … nothing is dropped"); §13 row "Cap unit runs at planning and drop the rest".
- **Finding and rationale:**
  - `endStaleUnitQACards` cancels *every* PENDING `unit:qa` card older than 24 hours, whatever the cause.
  - A card that is PENDING only because the per-product cap is full is a queued merge, not a stale one. With the cap at 3 and the 30-minute timeout, a busy product (or a QA tool that runs jobs slowly) loses merges silently, apart from one group notice per product.
  - That is the exact drop §13 rejected rev 5 for.
  - The sweep's stated purpose (GitHub off, no runner) is narrower than its rule.
  - Separately, READY cards (after a person's unblock, a retry pass or a drift release) are never swept, so the "bounded" claim does not cover them.
- **Recommendation:**
  - Sweep only when the cause holds (`r.unit == nil`, or the runner is absent), or measure age from the last start attempt rather than from planning.
  - Include READY cards in the no-runner sweep.
  - Correct §14.

### 7. The per-tick GitHub bound does not bound failure results; e2e jobs can be timed out by unit work - Severity: Major
- **Affected scope:** §5.6 "The bound, counted"; §14 "Runner latency".
- **Finding and rationale:**
  - Starts are capped at 2 per tick. Failure results are not: "plus each failure result arriving in that tick (each at most 1 + `defectMaxFetch`)".
  - `defectMaxFetch` is 12 (`dashboard/defecttriage.go:143`), so each result can cost up to 13 GitHub reads.
  - Each read can sleep up to 20 s on a rate limit: two waits of at most 10 s (`dashboard/github.go:375-391`).
  - Several products × `GAOS_UNIT_QA_OPEN` (up to 20) jobs can finish in one poll. Under a rate limit, that is many minutes inside one `poll()`.
  - All of that time is charged to every tracked job's `active` (`dashboard/qajobs.go:408-413`) against the 40-minute e2e budget (`qajobs.go:194`, `:465-468`).
  - The design's "2 minutes, well inside 40" holds only for starts.
- **Recommendation:** Pick one:
  - cap context builds per tick too (defer the rest with `Context.Built` false; adoption already re-runs them);
  - exclude the time spent in unit work from e2e `active`;
  - or move `buildUnitContext` off the loop.

  Then state the real bound.

### 8. The held-and-passed notice repeats on every later merge - Severity: Major
- **Affected scope:** §7.4 (held branch, `UnitHeldPassed`); §5.7 last row; §5.7 closing sentence.
- **Finding and rationale:**
  - A held defect is marked but stays open. Its `UnitMergedAt` stays older than every later merge, so every later passing run on that base marks it again:
    - it appends another `watched tests passed at …` line to `Excerpt` (capped at 4 KiB, so this eventually pushes out the failing tests the admin reads);
    - it sends another group notice.
  - §5.7 claims unit notices are bounded by the daily cap, but this one is bounded only by the merge rate for as long as the admin does not act. The design bounded refusal notices per day (§5.7 row 2); this one has no bound.
- **Recommendation:**
  - Add `UnitPassMarked` (or reuse `UnitNoticeSent` semantics). Mark and notify once.
  - Replace the Excerpt line rather than appending.
  - Add the case to TC-861.

### 9. "Nothing changes with the switch off" lists 3 exceptions; there are at least 6 - Severity: Major
- **Affected scope:** §2 closing paragraph; §0 "ships dormant"; §14 non-goals ("changing the e2e path (except …)").
- **Finding and rationale:** These changes are live with `UnitQA.Enabled` false but are absent from §2:
  - `Reconcile` no longer requeues e2e cards (§4.4);
  - `Tick` starts only QA-runner keys, so an operator-labelled `e2e:run` card no longer runs;
  - `liveAgentSlots` frees agent seats for RUNNING e2e cards;
  - every API PR key is lower-cased (finding 1);
  - `ghClient.do` now fills `ghError.Retry` for every caller;
  - `apiDispatchAnswer` is keyed differently.

  An operator approving a "dormant" release on the strength of §2 underestimates what it changes.
- **Recommendation:** List every switch-off behaviour change in §2 and in §14's e2e exceptions, each with its TC.

### 10. The §11 contract does not say `unit` is final when it first appears - Severity: Major
- **Affected scope:** §6 "'Any' means the result is acted on at once"; §3 step 5 ("The first poll that carries `unit` decides"); §11 Status row.
- **Finding and rationale:**
  - GAOS acts once, on the first poll carrying `unit`, and then sets `unitDone`.
  - §11 never says the QA tool may publish `unit` only when the suite has finished. A QA tool that streams progress (a growing `failing`, or `watched` filled in incrementally) would make GAOS:
    - hash a partial set into the defect identity;
    - pass-close on a partial `watched`;
    - ignore the real result.
  - This is a contract assumption that decides defect identity, and it is unstated.
- **Recommendation:** Add to §11: "`unit` is absent until the run's result is complete and never changes once present". Have GAOS refuse a `unit` whose content differs from an earlier poll (FAILED `qa-unit-unreadable`).

### 11. The `qaJobReason` wrapper cannot keep callers "unchanged" - Severity: Nit
- **Affected scope:** §5.4; §12 "existing tests that change" (`stagingcreate_test.go:138` "is unchanged (it keeps `qaJobReason3`)").
- **Finding and rationale:**
  - Changing `qaJobReason` to take a struct breaks every current call. Today `qaJobReason(id, code, text)` is called at:
    - `dashboard/qajobs.go:393, 429, 448, 456`;
    - `dashboard/qajobs_test.go:269, 282, 283`;
    - `dashboard/stagingcreate_test.go:138`.
  - A wrapper with a new name (`qaJobReason3`) means each of those lines must change. §12 says they do not.
- **Recommendation:** Keep `qaJobReason(id, case, text)` as it is and name the new one `qaUnitReason(qaReason)`, or list the eight call sites as changed.

### 12. Some change-list claims do not match the repository - Severity: Nit
- **Affected scope:** §12 "existing tests that change"; §12 store row.
- **Finding and rationale:**
  - There are no `fixEligible` callers in `autofix_test.go` or `fixpr_test.go`. The only callers are `dashboard/autofixpr.go:166` and `:490`.
  - The upsert column list is at `dashboard/store_arcade_defects.go:45-56`, not `:51-54`.
- **Recommendation:** Drop the test-caller claim and correct the anchor.

### 13. The board never offers the admin cancel of a RUNNING `unit:qa` card; `MoveCard` cannot call the locking method - Severity: Nit
- **Affected scope:** §5.5 last table row; §7.3 "The board offers only what the store allows".
- **Finding and rationale:**
  - `legalMoves` omits `canceled` from Working (`dashboard/board.go:236-237, 244-255`), so the board UI never shows the move.
  - The handler-side trim only removes moves; it never adds one.
  - `MoveCard` holds `s.mu` (`board.go:331`), so it must call `resolveUnitQACardLocked`, not `ResolveUnitQACard`.
- **Recommendation:** Add `canceled` to a RUNNING `:unit-qa` card's `Moves` for an admin viewer in the same post-store step, and name the locked call.

### 14. Which screen `Message` passes through is unnamed - Severity: Nit
- **Affected scope:** §6 ("`Message` is screened and capped at 2 KiB … rendered inside the same fenced section").
- **Finding and rationale:**
  - `Test` and `File` go through `stripBR53`, which removes backticks and line breaks (`dashboard/defects.go:251-263`).
  - For `Message`, the text says only "screened". If `stripBR53` is skipped to keep the build output multi-line, a backtick run in `Message` closes the fence in the packet.
- **Recommendation:** Name the function. Either use `screenDefectText` (one line, backticks stripped), or keep the lines but strip backticks and angle brackets.

### 15. The success metrics are not computable as stated - Severity: Nit
- **Affected scope:** §0 "Success metrics, read from the event log".
- **Finding and rationale:**
  - "A unit result within 60 minutes of the merge" needs the merge time and the event that marks a result.
  - The final DONE `TaskMoved` does not tell "nothing ran" (base not a unit base) apart from a real result unless the reason is in the payload. The in-place marker rewrite emits no event (§5.5).
- **Recommendation:** Name the events and fields: e.g. `TaskPlanned` time to `TaskMoved` DONE whose reason starts `unit tests`/`<n> unit test(s)`, with merges whose base is not a unit base excluded.

### 16. The delegation anchor is created by the design it authorises - Severity: Nit
- **Affected scope:** §0 "Delegation"; §10 D-105.
- **Finding and rationale:**
  - The policy has the operator approve (`docs/rules/gaos-design-review.md:34, 89-90`).
  - The design's authority to approve itself and amend four BRs is a quote that will be written to the decision log in phase 1, i.e. after approval. At review time no repository artifact carries it.
- **Recommendation:** Record the delegation (date, channel, words) in a repository file now, before the status changes, and cite that path and line.

---

**Checked and correct:**
- **Counts:** 3+4+5+7+5+1 = 25, TC-851…TC-875. Go tests 713→738 per phase, consistent with `deploy/check-workspace.sh:115` (712 before v0.66.1). The highest-TC column and `brd.html:1218` / `:1522` "Sixty-three" are right.
- **Schema:** 21 is the next file, and the `Defect.kind` REGEXP follows `schema/19-review-defects.sql:8`.
- **Code anchors I spot-checked are right:** `prflow.go:88-91, 600-606`; `labels.go:179-188`; `board.go:236-237, 452, 479`; `scheduler.go:231`; `runner.go:696, 705-718`; `github.go:309-316, 375-391`; `qajobs.go:350, 355-361, 465, 499`; `sendback.go:153-159`; `workspace.go:384-400`; `defects.go:347, 590-608`; `defecttriage.go:78, 142-143, 262`; `main.go:390, 439-440, 570, 594-595`; `handlers_autofix.go:17, 62, 102, 116, 158`; the 17 `checkFix` calls in `fixpr_test.go`.
- **Business rules:** BR-32 screening at dispatch is addressed. BR-05 stays closed (no new event type). BR-56 is respected (no new dispatch path). BR-38, BR-54 and BR-61 amendments are drafted; the BR-54 amendment matches §7.4, unlike BR-64 (finding 3).

Counts: B=0 M=10 N=6
