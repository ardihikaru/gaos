# Design review: agent seats per product, and the three pull-request agents finished

**Document:** `/home/ardi/devel/projects/ardihikaru/my-projects/gaos-agent-seats/docs/designs/agent-seats-and-pr-agents.md` (Draft rev 1)

**Overall verdict:** the design is not ready. It reads as complete, but the three pipelines it changes don't do what it says:
- §6: the review defect fails four record gates that already exist, so it can never produce a pull request.
- §7: the staging run can be held indefinitely, by default, through a path that breaks BR-44.
- §8: the "person first" hold can be released by a Blueprint sync.

The strongest counter-argument to the design's own thesis: a "seat" does not choose who works a card, because the scheduler ignores it. It is a per-product on/off switch for each role, presented as many-to-many staffing.

### 1. Spec-merge refusal and the default-off path put a finished job's card in Needs you, and it never leaves - Severity: Blocker
- **Affected scope:** §7.3 steps 3–4, §7.2 step 3, §16 item 7.
- **Finding and rationale:**
  - **BR-44 is broken.** BR-44 (`pustaka/docs/product/brd.html:1304`) lets the QA-job runner put an e2e card in `AWAITING_INPUT` only "while the QA tool waits for a person". The card leaves only "by the job resuming (`RUNNING`), failing, or a cancel". §7.3 puts the card there after the job is final and successful, so the job can never resume.
  - **No exit for the operator.** `legalMove` allows only Cancel out of Needs you (`dashboard/board.go:216-217`). A person who merges the spec by hand leaves a card that waits forever.
  - **The run is held indefinitely.** §7.2 holds the `e2e:run` card until every dependency is "final (Done, Failed or Canceled)". `AWAITING_INPUT` is not final, so the run never starts.
  - **This happens with the defaults.** `AutoMergeSpecs` is off by default, so "step 3's Needs-you path" is what every create card does. Every staging deploy that has an uncovered case loses its e2e run until someone cancels a card.
  - **"Today's behaviour plus a card that says so" is false.** Today the card ends Done (`dashboard/qajobs.go:334-336`) and nothing waits on it.
- **Recommendation:**
  - Do not use `AWAITING_INPUT`. End the create card Done with a reason (for example `spec-pr-open: merge it`) when merging is off. When a merge is refused, end it Failed/Blocked.
  - Or decide whether the run should wait for an unmerged spec at all (it cannot run that spec anyway).
  - If the Needs-you state is truly wanted, amend BR-44 in §9 and define how the card leaves.

### 2. The review defect fails four existing record gates, so it can never be dispatched or open a pull request - Severity: Blocker
- **Affected scope:** §6.2 field table, §6.3, §13 ("New DDL statements: 1"), §16 item 5.
- **Finding and rationale:**
  - **The ArcadeDB write is refused.** `Defect.automationType` is `mandatory true, regexp 'be|e2e'` (`dashboard/schema/17-defects.sql:18`), and the field table sets no `AutomationType`. This is the v0.52.0 trap the design itself cites in §11.
  - **The automatic path refuses it.** `autoClaimLocked` rejects a fix card unless `d.AutomationType == "be"` and `!d.Regression` (`dashboard/autofix.go:164-166`).
  - **The pull-request path re-checks things the design only exempts at dispatch.** §6.3 exempts only `autoFixPass` (`autofix.go:302-307`). But `fixEligible` runs again before any pull request opens:
    - It refuses a `BaseOverride` that is not in `AllowedBases` (`dashboard/autofixpr.go:90`), so the exemption is undone.
    - It refuses `"auto-fix is off"` (`autofixpr.go:92`), so §16's "or a Ready fix card (off)" can never become a pull request.
  - **The four checks run only at dispatch.** A reviewed pull request that merges while the fixer turn is running is not caught. §6.3's cancel covers only Pending/Ready cards (`dashboard/prflow.go:242`). GAOS would then open a draft against a merged or deleted branch.
- **Recommendation:**
  - Set `AutomationType: "be"` explicitly, or widen the regexp. If widened, that is a second DDL statement: update §13.
  - Put the review exemption and the four checks inside `fixEligible` (`autofixpr.go:76-103`), and re-run checks 2–4 there before `createPR`.
  - State what happens with auto-fix off: either the operator path can open the pull request, or §16 drops "(off)".
  - Name `fixEligible` and `autoClaimLocked` in change-list row 11.

### 3. Staging create cards have no case code the runner can read, so every one blocks and the run never starts - Severity: Blocker
- **Affected scope:** §7.2 steps 1–3, change-list row 13.
- **Finding and rationale:**
  - **The runner can't find the case code.** `qaJobRunner.start` gets the case from `caseCodeFor`. For `create` mode, `caseCodeFor` reads a `QA-case:` line from the pull request body, which it finds through `prKeyParts(c.DedupKey)` (`dashboard/qajobs.go:232-254`). The new key `<owner>/<repo>@<sha>:e2e-create:<caseCode>` has no `#`, so `prKeyParts` fails (`dashboard/prreview.go:194-199`).
  - **So every create card blocks.** The code comes back empty and the card goes `BLOCKED` with "no QA-case line in the pull request description" (`qajobs.go:284-288`).
  - **And the run waits on it.** `BLOCKED` is not final under §7.2's rule, so the run waits on it too.
- **Recommendation:** say that `caseCodeFor` reads the case code from the `e2e-create:` key (or from a field) for staging create cards. Add a test where a staging create card starts a `create` job with that code.

### 4. A Blueprint sync releases the "waits for a person" unit-fix card without a person - Severity: Major
- **Affected scope:** §8.2 ("Approval is the existing unblock").
- **Finding and rationale:** `releaseDriftLocked` moves every `BLOCKED` task of the desk to `READY` unless `operatorHolds(reason)` (`dashboard/workspace.go:395-396`). `operatorHolds` knows only three reasons: design-blocked, design-unreviewed and qa-epic-added (`dashboard/sendback.go:153-158`). The text `waits for a person: …` is not one of them. After a drift-blocked desk is synced, the scheduler dispatches the fix with nobody having approved it. That breaks §8's whole guarantee and §16 item 8.
- **Recommendation:** add a constant reason (for example `ReasonUnitFixHeld`) to `operatorHolds`, add `sendback.go` to the change list, and test that a drift release leaves the card Blocked.

### 5. The unit-test agent's tests and fixes go nowhere, and the workspace premise conflicts with BR-42/BR-50 - Severity: Major
- **Affected scope:** §8.1 (assumption), §8.2, §12 ("The unit-test fix opening its own pull request").
- **Finding and rationale:**
  - **The premise is an unanchored assumption.** "The product workspace holds the product's repository at the merge commit" is not backed by any file. BR-42 says GAOS "writes only allowlisted instruction files" into a workspace (`brd.html:1302`), and BR-50 says GAOS "reads no folder" (`brd.html:1330`).
  - **Nothing reaches a repository.** A `created` result means new test files exist only in an agent workspace. A `unit:fix` run "ends in Review" and opens no pull request.
  - **So no one has been told what "approve" means.** What does a person approve, and how does the code reach the repository?
- **Recommendation:** either route test and fix output through the auto-fix pull-request path (a branch and a draft pull request, with the same checks), or state plainly that §8 produces a report only. Then drop "write them" and "fix", and show how the workspace gets the code without breaking BR-42.

### 6. The "second low review re-opens the defect" premise is false, and re-opening would not do what §6.2 says - Severity: Major
- **Affected scope:** §6.2 `Key` row.
- **Finding and rationale:**
  - **A second review never happens.** The reviewer card is de-duplicated per pull request (`owner/repo#n:role`, `dashboard/braintown_consumer.go:111-116`), so a new push gets no second review.
  - **If it did, re-opening misbehaves.** `reopenDefectLocked` (`dashboard/defects.go:536-553`):
    - plans a **triage** card;
    - clears `BaseOverride` (`:540`);
    - sets `Regression = true` (`:541`), which `autoClaimLocked` and `fixEligible` both refuse.
  - **A non-final defect is never re-opened.** Existing code takes the `repeat` path instead (`defects.go:461-464`).
- **Recommendation:** delete the re-open clause, or write a dedicated path that keeps the triaged state and re-sets `BaseOverride`. State what happens when the defect is still open.

### 7. The seat gate's early return skips the merge-time cancel and the review-defect close - Severity: Major
- **Affected scope:** §3.4 (gate placement, "When every spec is dropped…"), §6.3 last paragraph.
- **Finding and rationale:** for `pr.merged`, the only spec is the unit-test card (`prflow.go:86-89`). If `unit-test` is unseated, every spec is dropped and the call returns `unseated` before the cancel loop runs (`prflow.go:238-249`). The pull-request row (`:253`) isn't written either. Review and e2e cards made moot by the merge stay open, and the review defect is never closed with `pr-merged`.
- **Recommendation:** apply the gate only to the planning of `fresh` specs, after the cancel loop and the row update. Test this specific case: a merged pull request with no `unit-test` seat.

### 8. GAOS's REQUEST_CHANGES is never withdrawn, so it can block a merge for good - Severity: Major
- **Affected scope:** §5.
- **Finding and rationale:** where branch protection requires approvals, a "changes requested" review from a writer blocks the merge until that reviewer approves or the review is dismissed. GAOS never approves (§5, §12) and never re-reviews (finding 6). The design gives no dismissal step, so the pull request stays blocked until an admin intervenes. The design also asserts GitHub's 422 behaviour without anchoring it, and the fallback must tell that 422 apart from other 422s (for example a body validation error).
- **Recommendation:** decide how the blocking review is cleared: a dismissal on the operator's label change or on Done, or a documented manual step. Label the GitHub behaviour as an assumption. Make the fallback match the specific error.

### 9. The "Auto-fix settings form" does not exist, and the new fields have no write path - Severity: Major
- **Affected scope:** §7.1 (`RepoKinds`), §7.3 (`AutoMergeSpecs`), change-list rows 6 and 2.
- **Finding and rationale:**
  - **No form exists.** `templates/backoffice.html:1126-1138` is the end of `bo-autofix` and the status bar. The page tells the operator to call `POST /products/{id}/autofix` (`:1173`).
  - **`RepoKinds` has no store path.** It is a `Master` field, but `SetAutoFix` writes only `m.AutoFix` (`dashboard/defects.go:675-696`).
  - **The handler isn't in the change list.** `handlers_autofix.go` (`autoFixFromForm`, `:125`) is missing.
  - **The event wouldn't record the change.** The `AutoFixConfigured` reason format (`defects.go:690-693`) would not include either new value.
- **Recommendation:** either design the form as new UI work (fields, admin rule, validation that each `RepoKinds` key is in `Repos`), or move `RepoKinds` into `AutoFix`. Add `handlers_autofix.go` and the reason format to the change list.

### 10. The unit-test seed policy is not in blueprint.go - Severity: Major
- **Affected scope:** §8.1, change-list row 17.
- **Finding and rationale:**
  - `blueprint.go:164` is the generic `65-<key>.md` path rule. It holds no unit-test text.
  - The seed is in `deploy/seed/example/product.json:138`, as the PR-flows design records (`docs/designs/gaos-pr-agent-flows.md:472`).
  - Row 17 would therefore change the wrong file.
- **Recommendation:** replace row 17 with `deploy/seed/example/product.json`, and keep the release-note instruction.

### 11. The QA-tool interactions have no failure behaviour, no de-duplication across commits, and a long hold on the run - Severity: Major
- **Affected scope:** §7.1 (the `kind` field), §7.2 steps 1–3.
- **Finding and rationale:**
  - **No failure handling.** Nothing says what happens when the new cases query fails (404 before the QA-tool change, 5xx, timeout). It is an outbound call made inside an inbound handler (`staging.go:259-260`).
  - **Duplicate create jobs.** Create cards are keyed per SHA, so every staging deploy re-creates create jobs (and spec pull requests) for the same uncovered cases while earlier ones still wait for approval.
  - **The run waits up to a day.** The run waits for spec approval, up to `GAOS_QA_APPROVAL_WAIT_HOURS` (24 h by default, `qajobs.go:161-170`), against a commit that may already be replaced.
  - **The compatibility switch is tied to the wrong thing.** The omit-the-field rule depends on a product setting rather than on the QA tool's capability. A strict decoder there (GAOS's own staging handler uses `DisallowUnknownFields`, `staging.go:234`) turns into `qa-start-failed`.
- **Recommendation:**
  - On any query error, plan the run alone.
  - Key create cards by repository and case, not by SHA, and skip a case that already has a live create card.
  - Decide whether the run waits for creates at all.
  - Gate the `kind` field on a QA-tool capability, not on `RepoKinds`.

### 12. Held cards count toward the PR-card cap and can starve new reviews - Severity: Major
- **Affected scope:** §7.2, §7.3, §8.2.
- **Finding and rationale:** `openPRCardsLocked` counts every unfinished card with a dedup key that doesn't start with `defect:` (`prflow.go:176-191`). The following all count toward `prMaxOpen`:
  - Blocked `unit:fix` cards;
  - Needs-you create cards;
  - dependency-held run cards.

  Once enough are waiting, new pull requests are refused with `cap`.
- **Recommendation:** exclude person-held cards from the cap, or give them their own cap, and say which.

### 13. BR-54 and BR-08 are not addressed - Severity: Major
- **Affected scope:** §7.2, §7.3, §9.
- **Finding and rationale:**
  - **BR-54** (`brd.html:1349`): "the single write [QA data] may cause to the task graph is to add one card for a new epic". In §7.2, the QA tool's case list decides how many cards are planned and which ones.
  - **BR-08** (`brd.html:1240`): the Minimum band "forbids auto-merge". §7.3 introduces GAOS's first auto-merge with no readiness-band check.
  - §9 lists only BR-59 and BR-60, so neither existing rule is amended or honoured.
- **Recommendation:** add the BR-54 amendment (or argue in the document why the staging callback, not QA data, causes the write). Refuse the spec merge for a product in the Minimum band, or record why BR-08 does not apply.

### 14. The spec merge trusts the QA tool to name any pull request - Severity: Major
- **Affected scope:** §7.3 step 1, §15 first bullet.
- **Finding and rationale:**
  - **Any qualifying pull request can be named.** The checks are open, not draft, default base, same-repo head, green. Any person's pull request in an automation repository passes them if the QA tool names it, and nothing checks that the QA tool opened it. §15 calls this the worst case, but it is unbounded.
  - **Legacy commit statuses are ignored,** so a repository that uses only statuses never merges.
  - **No pagination is stated** for check runs (GitHub returns 30 per page by default).
  - **A permission is missing.** Reading check runs needs `checks:read`, which §7.3 doesn't list.
- **Recommendation:**
  - Pin the merge to the QA tool's own branch prefix and the account that authored the pull request, recorded when the job reports `pr_url`.
  - Read both check runs and combined status, paginate, and list `checks:read`.

### 15. A seat is a role switch, not an assignment, and the upgrade switches on a new flow silently - Severity: Major
- **Affected scope:** §1 Goal, §3.4 ("The gate does not choose which agent runs a card", "Upgrade").
- **Finding and rationale:**
  - **Seats don't choose the worker.** The scheduler takes the first free agent of the role from all agents (`freeAgent`, `dashboard/scheduler.go:399-409`), not from those seated in the product. The citation `agentForStage` is wrong: it seats by stage, not role (`dashboard/fixtures.go:80-91`).
  - **So the agent-level identity is decorative.** Seating Juno in `p-9` means only "the pr-reviewer role is on in `p-9`". The Agents page will show agents "seated" in products that a different agent may serve. The honest alternative, a per-product flow switch, is not considered.
  - **The upgrade turns on new work.** It seats `fixer`, which switches on the review-defect flow of §6 (new pull request comments, new fix cards) in every product with repositories. That flow never existed before. It is the "silent switch" the section says it avoids, in the other direction.
- **Recommendation:**
  - Either make the scheduler (and the auto-fix claim) choose among the product's seated agents, or rename seats as per-product role switches. Add that switch as the rejected (or chosen) alternative.
  - Seat only the roles that have flows today (`pr-reviewer`, `qa`, `unit-test`) in the upgrade.

### 16. The unit-fix card's "summary as its context part" has nowhere to live - Severity: Major
- **Affected scope:** §8.2 (the fix-card paragraph).
- **Finding and rationale:**
  - `TaskRecord` has no context field (`dashboard/model.go:802-841`).
  - The only card packet hooks are defect-specific (`fixerPartsLocked`, `autofix.go:335-360`) and reviewer-specific (`prPacketParts`, `prreview.go:178-189`).
  - Two engineers would store this differently, and it must survive a restart.
- **Recommendation:** name the carrier: a stored artifact, a field plus its DDL, or a small record like `Defect`. Name the packet hook that reads it, and add both to the change list and §13.

### 17. The rejected alternative's reason is false - Severity: Nit
- **Affected scope:** §3.1 "Rejected", §11 `TestSeatsSurviveARestartAndTheAgentBootstrap`.
- **Finding and rationale:** `persistAll` "only ever runs once per database" (`dashboard/store_arcade.go:675`, called only when the database is unseeded, `:158-160`). Bootstrap does not upsert agents on every start.
- **Recommendation:** drop that reason. The second reason (the planner already reads the product record) is enough.

### 18. Line and identifier drift - Severity: Nit
- **Affected scope:** throughout.
- **Finding and rationale:** these citations are off:

  | Cited | Actual |
  |---|---|
  | `prflow.go:388-389` (skip log) | `:385-386` |
  | `prflow.go:378-380` (fork rule) | `:375`; 378-380 is the association check |
  | `staging.go` "after line 282" | file ends at 279; the `capped` case is `:262-264` |
  | `templates/backoffice.html:1275-1282` (PR filter) | `:1349-1357` |
  | `board.go:226-227` (unblock) | `:230-231`; 226-227 is redo |
  | `prreview.go:251-256` | `:256-261` |
  | `autofix.go:322` | `:325` |
  | `pager.go:130` | `:128` |

  In addition:
  - `agents[id].Role` indexes a slice (`fixtures.go:35`); use `agentByID` (`:51`) or `s.store.Agents`, as §3.3 does.
  - `readOnly` methods live in `store.go` (for example `:451`), not only `autofix.go`.
  - The fold treats `RecordUpdated` as a no-op (`fold.go:439-450`); "folded into the master record" overstates it.
- **Recommendation:** correct the anchors and use one source for agents.

### 19. "Seat" already means two other things in the registers - Severity: Nit
- **Affected scope:** §0 Terms, §3.
- **Finding and rationale:**
  - A product is "seated at an idle desk" (`decision-log.html:1551-1555`).
  - Support is priced "not per seat" (`brd.html:1118`).
  - The scheduler "seats" an agent on a desk, which is the meaning §3.3 renames to "Working on".
- **Recommendation:** use a distinct term (for example "staffed" or "enabled roles"), or define the new sense against the other two.

### 20. Change list and arithmetic omissions - Severity: Nit
- **Affected scope:** §10, §13, §3.4.
- **Finding and rationale:**
  - `GAOS_E2E_CREATE_MAX` and `GAOS_E2E_MERGE_WAIT` are in neither the change list (env example and stack files) nor §13.
  - `seatsMigrated` needs a `Master` field that row 1 doesn't list.
  - `Master.Clone` must deep-copy `Seats` and `RepoKinds`.
  - §6.2's excerpt is capped at 4 KiB (`defectMaxExcerpt`, `defects.go:58`), but a review body can be 6000 characters (`prreview.go:23`).
  - Review defects bypass `defectMaxNewKeys` without saying so.
- **Recommendation:** add the rows and state the truncation and the cap.

### 21. Done-when and phasing are inconsistent - Severity: Nit
- **Affected scope:** §16 item 2, §14.
- **Finding and rationale:**
  - Staging plans only `qa` cards, so "unseating the reviewer … the staging callback answers 409" pairs a role with a flow it doesn't touch.
  - §14 says "one release", while phase 5 waits on an external change.
- **Recommendation:** say "unseating qa" for the staging case. State whether the release waits for phase 5 or ships without it.

### 22. GAOS's own fix pull requests feed its flows - Severity: Nit
- **Affected scope:** §6.3.
- **Finding and rationale:** merging GAOS's draft fix into the feature branch makes Brain Town report `pr.merged`. That plans a unit-test card for a merge into a non-default branch (`prflow.go:86-89`). The design doesn't mention this.
- **Recommendation:** skip `gaos/fix-*` heads in `prFlow.handle`, or state that this is intended.

**Sections that hold up:** the label-family choice for severity (§4.1) is sound: `labelSet` is closed in Go only and `Task.labels` is an unconstrained string (`schema/04-execution.sql:34`). The label count, 10 plus 8 = 18, checks out against `labels.go:45-56`. D-099, BR-59 and BR-60 are all free.

Counts: B=3 M=13 N=6
