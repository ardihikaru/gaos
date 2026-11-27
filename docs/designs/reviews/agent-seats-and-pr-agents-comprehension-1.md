# Goldfish comprehension review: `docs/designs/agent-seats-and-pr-agents.md` (rev 1)

## Paraphrase

### What this proposes

1. **Seats.** A product record (`Master`) gets a sorted list of agent ids, `Seats`. One agent can be seated in several products. Seats are stored on the Product document, so no DDL changes. An admin-only route, `POST /agents/seats`, changes them and emits `RecordUpdated`. The Agents page gets a product filter (kept across pages), seat chips per agent and an editor for admins. The existing "seated · desk" text is renamed "Working on".
2. **Seats gate the flows.** `PlanPullRequestCards` drops every card spec whose role has no seated agent in the product. If every spec is dropped, it returns `ErrPRSkipped` with the class `unseated`; the staging callback answers 409. On first start, a one-time upgrade seats the first agent of `pr-reviewer`, `qa`, `unit-test` and `fixer` in every product that has repositories, so nothing switches off silently.
3. **Severity as a label.** A new `severity:*` label family (five values) is written by the reviewer right after it parses its reply. The board shows it as a coloured chip, the table gets Severity and Labels columns, and a Severity filter offers `medium+`.
4. **Asking GitHub for changes.** Medium and worse post `REQUEST_CHANGES`; none and low post `COMMENT`. If GitHub answers 422 because the account opened the pull request, the review is posted again once as `COMMENT`.
5. **Low reviews are fixed by an agent.** A low review becomes a `Defect` of kind `review`, already triaged `easy`, and gets a fix card from the existing auto-fix pipeline. The fix's draft pull request targets the head branch of the reviewed pull request. That branch is exempt from `AllowedBases` under four conditions checked at dispatch.
6. **Two e2e kinds, and GAOS merging specs.** A per-repository kind (`be` or `fullstack`) becomes an `e2e-kind:*` label and is sent to the QA tool. On a staging deploy, GAOS asks the QA tool which cases have no automation yet and plans up to N create cards. The run card depends on them. When `AutoMergeSpecs` is on, GAOS squash-merges the QA tool's spec pull request with the head SHA pinned, but only if every check is green.
7. **Unit tests.** The unit-test agent replies in a fenced block (`created`, `passed` or `failed`). A `failed` reply plans a Blocked `unit:fix` card that a person releases. The "Waiting on a person" filter is widened to list it.
8. **Registers.** D-099, BR-59 and BR-60, data-model rows, QA-plan rows and the docs page.

### How the existing system works, per this document

- Agents are 13 fixtures and have no tie to a product. The Agents page shows only the desk an agent is busy at.
- The PR flows (D-088) plan cards for any product that owns the repository. The reviewer always posts one `COMMENT`, and the severity only decides the card's end state.
- There is one e2e label for building automation. A staging deploy plans one `run` card for every case (`*`). A person merges the QA tool's spec pull request.
- A failed unit-test run only sends a WhatsApp notice.
- Labels form a closed, one-per-family vocabulary that is persisted, replayed and filterable.
- The auto-fix pipeline turns triaged `easy` defects into draft fix pull requests, within caps and the `AllowedBases` rules.
- "Needs you" (`AWAITING_INPUT`) is entered only by an agent's question. BR-44 is cited for this; the document does not mention BR-44's QA-runner exception.

Sections 3, 4, 5 and 8 paraphrase cleanly. In §6 and §7, parts of the described mechanism do not match the code they cite; details follow.

## Findings

### GAOS performing a merge contradicts the BRD scope, and the design does not say it amends it - Severity: Blocker
- Affected scope: §7.3, §9 (BR-60), §12, §15
- Finding: The BRD lists **"Owning the tenant's repository or performing the merge. GAOS indexes and proposes; the gate is where its authority stops"** under Out of scope (`pustaka/docs/product/brd.html:1456-1457`). The scope boundary says the same: "the merge is gated rather than performed" (`brd.html:1382-1384`). BR-08 (`brd.html:1240`) says the *Minimum* band "forbids auto-merge". §7.3 has GAOS call `PUT …/merge`. §9 adds BR-60 but does not mention the out-of-scope line, the boundary or BR-08. §12 treats "merging a QA spec pull request" as in scope without saying which ruling allows it. The plan therefore rests on a premise the authoritative source rejects, and the decision is not recorded.
- Recommendation: State in §7.3 and §9 that D-099 amends the BRD's out-of-scope item and §10.2 for automation repositories only. Say whether a spec pull request is a "gated task" under BR-19/F-2. Say how `AutoMergeSpecs` interacts with the product's band (BR-08), for example "refused while the band is Minimum". Otherwise drop the merge and keep the Needs-you path.

### The merge-wait and Needs-you states break the QA job runner's restart logic and widen BR-44 without saying so - Severity: Blocker
- Affected scope: §7.3 steps 3–4; compare §8.2 "which would widen BR-44"
- Finding: I checked this against `qajobs.go`:
  - `poll` removes a job from `r.jobs` when it is final and marks the card Done straight away (`qajobs.go:332-336`). The design does not say where the "merging / waiting for checks up to 30 min" state lives, or what the card's status and reason are during that wait.
  - `Tick` restarts any e2e card in `AWAITING_INPUT` (or RUNNING, PENDING, READY) that has no tracked job (`qajobs.go:220-225`). `start` re-adopts a job only if the reason begins with `qa-job:<id>` (`qajobs.go:279-281`). Otherwise it reads the case code and calls `startAutomation` again.
  - A card moved to Needs you with the reason `merge-<class>` (as §7.3 specifies) is therefore started again on the next tick. That opens a **second create job and a second spec pull request**, which is exactly what the comment at `qajobs.go:276-278` exists to prevent.
  - BR-44 (`brd.html:1304`) allows the QA runner into `AWAITING_INPUT` only "while the QA tool waits for a person", and the card leaves "by the job resuming, failing, or a cancel". A finished job cannot resume, so the only exit is a cancel (`board.go:216-217`).
  - §8.2 explicitly avoids widening BR-44; §7.3 widens it silently. The two sections contradict each other.
- Recommendation: Name the card status and the exact reason format for each merge sub-state, keeping the `qa-job:<id>` prefix or adding a separate marker that `Tick` honours. Say how a waiting merge survives a restart. Either amend BR-44 explicitly and define how a person releases the card (for example "the person merged it by hand: move to Done"), or use Blocked instead of Needs you, as §8.2 does.

### With merge off (the default), every staging run with uncovered cases is held indefinitely - Severity: Major
- Affected scope: §7.2 step 3, §7.3 final paragraph
- Finding: `AutoMergeSpecs` defaults to false, and then "step 3's Needs-you path is what happens". So every create card ends in `AWAITING_INPUT`, which is not final. The §7.2 rule then keeps the `e2e:run` card from starting until a person cancels every create card. Today a successful create job ends Done (`qajobs.go:336`). Calling this "today's behaviour plus a card that says so" is wrong: today's staging run would also stop happening.
- Recommendation: Define the merge-off outcome. Either the create card ends Done (as today) with a "spec PR awaits a person" reason and the run proceeds, or the hold is intended and should be stated as a behaviour change with a stated exit.

### Staging create cards have no source for their case code - Severity: Major
- Affected scope: §7.2 step 2
- Finding: The runner gets a create card's case from the pull request's `QA-case:` line, found by parsing a PR-keyed dedup key (`qajobs.go:236-254`, `prKeyParts`). The new create cards are keyed `<owner>/<repo>@<sha>:e2e-create:<caseCode>` and have no pull request. `caseCodeFor` would return "", and `start` would block the card with "no QA-case line in the pull request description" (`qajobs.go:284-288`). The change list (§10 row 13) does not mention `caseCodeFor`.
- Recommendation: Say that `caseCodeFor` reads the case code from the `e2e-create:` dedup key (or from a card field), and add it to §10 and §11.

### The seat gate returns before the merge-cancel loop, which §6.3 depends on - Severity: Major
- Affected scope: §3.4, §6.3 "If the reviewed pull request merges…"
- Finding: The gate runs "after `prCardsFor` and before the de-duplication loop" and returns `ErrPRSkipped` when every spec is dropped. On `pr.merged` the only spec is `unit-test` (`prflow.go:86-89`). If `unit-test` is unseated, the call returns before the cancel loop (`prflow.go:239-249`) and before the Pull requests row is written (`prflow.go:253`). Unstarted review and e2e cards of a merged pull request are then never canceled. The review-defect fix card that §6.3 adds to that list is not canceled either, so a fixer may still push to a branch that has already merged.
- Recommendation: Specify that the merge-cancel loop and the row update run before, or regardless of, the seat gate, and add a test for "unit-test unseated, PR merges".

### The `unseated` 409 cannot be told apart from `capped` the way §3.4 describes - Severity: Major
- Affected scope: §3.4 "a new case beside `capped`, `staging.go` after line 282"
- Finding: `staging.go` ends at line 279. Its single case `errors.Is(err, ErrPRSkipped)` answers `capped` for every skip class (`staging.go:262-264`). A second `errors.Is` case "beside" it cannot match a different class, because both wrap the same sentinel. Two engineers would build this differently: a string match on the message, a new sentinel, or a typed error.
- Recommendation: Name the mechanism, for example `var ErrPRUnseated = fmt.Errorf("%w: unseated", ErrPRSkipped)` checked before the generic case, and correct the anchor.

### A "second low review on a new push" cannot happen, and the reopen path it relies on does the opposite of what §6.2 needs - Severity: Major
- Affected scope: §6.2, `Key` row
- Finding: Two problems:
  - Review cards are keyed per pull request (`braintown_consumer.go:115`), and Brain Town reports only `pr.opened` and `pr.merged` (`prflow.go:352`). Nothing plans a second review after a push, so the premise has no trigger.
  - If something did trigger it, `reopenDefectLocked` (`defects.go:533-553`) is the *regression* path. It clears `BaseOverride`, `Difficulty`, `Class`, `Repo` and `Files`, sets `Regression`, and plans a new **triage** card. Its doc comment says the fix card "will never be automatic". That contradicts "already triaged `easy` … no triage card is planned" and the head-branch base.
- Recommendation: Drop the reopen sentence and state "one review defect per pull request", or define a dedicated re-plan path for review defects and the trigger that calls it.

### Review defects omit a mandatory DDL property - Severity: Major
- Affected scope: §6.2 field table; §2 item 7; §13 "New DDL statements: 1"
- Finding: `Defect.automationType` is `mandatory true, regexp 'be|e2e'` (`schema/17-defects.sql:18`). The field table does not set `AutomationType` (`defects.go:78`), so an ArcadeDB commit of a review defect would be refused. This is the "passes in memory, fails on ArcadeDB" class of bug that §11 itself warns about. Neither value fits a code review, so either a value is chosen or a second regexp widens, which changes the §13 arithmetic.
- Recommendation: Add `AutomationType` to the table with a stated value, or widen that regexp in `19-agent-seats.sql` and update §2 and §13.

### A standing `REQUEST_CHANGES` has no lifecycle - Severity: Major
- Affected scope: §5 table, §15
- Finding: After a medium review the card ends Done, but the GitHub review stays "changes requested". GAOS never approves (§5) and never re-reviews (see the per-PR key finding above). The design does not say who dismisses the review or what happens after the author fixes the code. Assumption to verify: on repositories that require approving reviews, an outstanding change request from a writer blocks the merge until it is dismissed. In that case each medium finding becomes a permanent block that a person must find on GitHub.
- Recommendation: State the expected exit: a person dismisses it, or GAOS dismisses it on some event. Say so in the release notes and add it to the §16 checklist.

### New PR-keyed cards count against `GAOS_PR_MAX_OPEN` - Severity: Major
- Affected scope: §8.2 (the `unit:fix` card), §7.2 (create cards)
- Finding: `openPRCardsLocked` counts every unfinished card that has a dedup key, except keys starting with `defect:` (`prflow.go:175-191`). The default cap is 5 (`prflow.go:49`).
  - A Blocked `unit:fix` card waits for a person indefinitely and holds a slot.
  - A staging callback adds up to `GAOS_E2E_CREATE_MAX` (≤10) create cards plus the run card, which can exceed the cap by itself.
  - The design does not say whether the cap applies to these cards, or where create cards are planned relative to the cap check.
- Recommendation: State, per new card type, whether it counts toward the cap and where it is planned (inside `PlanPullRequestCards` or after it). Consider exempting `unit:fix`.

### Upgrade seating leaves later products undefined - Severity: Major
- Affected scope: §3.4 "Upgrade"
- Finding: The migration runs once per product, marked by `seatsMigrated`. It does not say:
  - what a product created after the upgrade gets (it starts with zero seats, so the flows are silently off);
  - whether a product with no repos at upgrade time is marked migrated, and so gets nothing when repos are added later;
  - how `MemStore` (no persistence) behaves on each start.
  
  This is the "silent switch-off" the section says it avoids. README rule 6 applies: two engineers would build this differently.
- Recommendation: State the default for new products and for products that gain repositories later, and when `seatsMigrated` is written for a product with no repos.

### How the review fixer's source slice is built is not stated - Severity: Major
- Affected scope: §6.1, §6.3
- Finding: For a fix card with `BaseOverride`, `autoFixPass` builds the source slice with `buildAndStoreDefectContext` (`autofix.go:308-313`, `defecttriage.go:422-451`). That path was written for QA defects. The design says "the pipeline does the rest" but does not say whether `buildDefectContext` builds a context from `Files` alone when `Operations` and `Spec` are empty. If it cannot, the card is blocked `BlockSearchFailed`. I could not verify this from the cited lines.
- Recommendation: State what the fixer's packet contains for a review defect and which function builds it. Add the case to `TestLowReviewPlansAReviewDefectAndFixCard`.

### No fallback when the QA tool lacks the new routes - Severity: Major
- Affected scope: §7.1 last sentence, §7.2 step 1
- Finding: §7.1 keeps today's behaviour when `RepoKinds` is empty. §7.2 calls the new `GET /api/v1/gaos/cases` route on every staging callback, with no stated behaviour on 404 or a timeout. It also does not say whether that outbound call blocks the HTTP callback. Phase 5 waits for the QA-tool change, but nothing stops an older QA tool from running against the new GAOS.
- Recommendation: State that on 404 or any error GAOS plans only the run card (today's behaviour), and say whether the call is synchronous in the handler.

### Seats do not decide who runs, and the cited picker is the wrong function - Severity: Major
- Affected scope: §3.4 last paragraph, §12 first bullet
- Finding: The scheduler picks a PR card's agent with `freeAgent` (`scheduler.go:399-410`), which takes the first free agent of the role. `agentForStage` (`fixtures.go:80`) is for workflow stages. So the agent that runs is "first free", not "first", and need not be seated. An operator who seats Flint (`a-6`, qa) can see Ember (`a-5`), who is not seated, under "Working on" for that product. That contradicts the meaning §3.1 gives a seat. The non-goal is stated, but its consequence for the Agents page is not.
- Recommendation: Correct the anchor and the wording. Either state on the page that a seat is "the role is on for this product", not "this agent works here", or restrict `freeAgent` to seated agents.

### Several line anchors are wrong - Severity: Nit
- Affected scope: §3.4, §4.1, §5, §1
- Finding: These anchors do not match the code:
  - `prFlow.handle` logs the skip at `prflow.go:385-386`, not 388-389.
  - `staging.go` has no line 282.
  - `parsePRReview` is called at `prreview.go:234`, not 236.
  - The end-state logic is at `prreview.go:256-261`, not 251-256.
  - `needs-manual` is set at `prreview.go:256-258`, not 251-253.
  - `listPageAt` is at `pager.go:128`.
- Recommendation: Re-anchor them.

### "Seat" and "seated" are used in three senses - Severity: Nit
- Affected scope: Terms, §3.3
- Finding: The design defines *seat* as product membership. The code and UI already use "seated" for "assigned to a desk for a run" (`handlers_bo.go:277`, `scheduler.go:398`, `store_mem.go:635-646` "Seated" count). The Agents page subtitle says "Roles the scheduler can seat" (`handlers_bo.go:267`). The design renames only the row text.
- Recommendation: Also list the subtitle and the `Seated` stat in §3.3, or choose a different word, such as "staffed".

### The fix card's dedup key is misquoted - Severity: Nit
- Affected scope: §6.2, after the table
- Finding: Defect card keys are `defect:<key>:<gen>:<job>` (`defecttriage.go:49-68`), not `defect:<key>:fix`.
- Recommendation: Quote the real format.

### The Severity sort needs an entry in `boardSorts` - Severity: Nit
- Affected scope: §4.2 Table
- Finding: Sorting is allowed only for keys listed in `boardSorts` (`board_page.go:96`). §4.2 adds only the `tableHeads` entry.
- Recommendation: Mention `boardSorts`.

### The §5 table omits the held and failed paths - Severity: Nit
- Affected scope: §5
- Finding: The table does not cover a body held by `screenBody` (`prreview.go:240-245`) or a failed post. It does not say how the 422 "own pull request" case is told apart from other 422s, or where `request-changes-refused` lands when the card stays in Review.
- Recommendation: Add those rows and the detection rule.

### The kind label and the kind field can disagree - Severity: Nit
- Affected scope: §7.1
- Finding: A repository with no entry gets the `fullstack` label, yet GAOS omits `kind` when `RepoKinds` is empty. The card would say `fullstack` while the QA tool received no kind.
- Recommendation: Say whether the label is also omitted in that case.

### When the unit-test notice fires is unclear - Severity: Nit
- Affected scope: §8.2 "(3)"
- Finding: Today the notice fires on `StatusFailed` (`main.go:452`). Under the new table a `failed` reply ends Done. The design does not say whether the notice now fires on the reply, on run failure, or on both.
- Recommendation: State the trigger.

### "The review card's comment thread on the reviewed pull request" mixes card and pull request - Severity: Nit
- Affected scope: §6.3
- Finding: A card has no comment thread on GitHub.
- Recommendation: Say "a comment on the reviewed pull request".

These parts held up: §4.1's case for using a label instead of a new field, the §4.2 board and filter design, the §3.2 route and admin rule (checked against `handlers_autofix.go:110-120` and `main.go:554-555`), and the §13 label arithmetic (10 + 8 = 18, matching `labels.go:45-56`).

Counts: B=2 M=12 N=8
