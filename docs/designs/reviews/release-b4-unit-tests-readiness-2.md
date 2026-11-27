# Readiness review: `docs/designs/release-b4-unit-tests.md` (rev 2)

**Lens:** Tech Lead readiness. The test is whether two competent engineers could build this and get materially different results.

**What I read:**
- The design, `docs/rules/gaos-design-review.md` and `docs/designs/README.md`.
- The registers: `pustaka/docs/product/{brd,decision-log,qa-test-plan}.html`.
- The code the design anchors to under `dashboard/`.
- I did not open anything under `.claude/`.

**What checked out:**
- **Identifiers are next-free and used consistently.**
  - D-104: the last decision is D-103, at `decision-log.html:2148`.
  - BR-63: the last rule is BR-62, at `brd.html:1308`.
  - TC-839 to TC-863: the last test case is TC-838, at `qa-test-plan.html:929`.
  - Section `#go-unit-qa` is not already in use.
  - Schema file `21-unit-defects.sql`: the last one is `20-release-b.sql`.
- **The test case count adds up.** The phases hold 3+4+6+6+2+3+1 = 25 test cases, which matches TC-839 to TC-863.
- **Labels go from 19 to 20,** and `labels.go:95-115` does list 19.
- **Anchors that hold:** `prflow.go:88-91`, `labels.go:179-188`, `qajobs.go:350`, `model.go:516`, `defecttriage.go:78`, `sendback.go:153-159`, `staging.go:186` (40 or 64 hex), `prreview.go:196` and `pullrequests.go:97-113`.
- **The schema change is correct:** `18-pull-requests.sql:13` and `19-review-defects.sql:8` hold exactly the regexps that §12 widens.
- **Every path in the change list exists.**

---

### A job picked up after a restart loses sha, timeout and mode - Severity: Blocker
- Affected scope: §4.3, §5.1 (last sentence), §5.2, §6, §7.1, §8.1; `qajobs.go:355-361`
- Finding: When the runner restarts, `start` re-adopts a job from the card's reason and builds `qaTracked{jobID, card, lastPoll, waiting, caseCode}`. It returns before any of the new setup runs. The design says "A job adopted after a restart needs no commit: it is already started." That is false, because the poll path needs several values the adoption never sets:
  - **Timeout.** `t.timeout` stays 0. §4.3 says "the poller compares active time with `t.timeout`", so every adopted job fails `qa-job-timeout` on its first poll. That includes today's e2e jobs, so this is a regression outside the unit path.
  - **Merge commit.** The full sha is needed for:
    - `UnitDefectIn.SHA`;
    - `RunRef`;
    - `buildUnitContext` "at the merge commit";
    - the `QASha` of the `qa-unit` row;
    - the done reasons `… at <sha12>`.

    The reason only carries `sha:<12 hex>`, and §5.2's wait reason drops `case:`, so how the fields are ordered against `caseFromReason`, which reads field 1, is not decided.
  - **Mode.** The `unit-create` versus `unit-run` mode is needed by `recordQARow`'s unit branch ("when … the job is a `unit-create`"). `qaTracked` gets no mode field (§12 lists only `timeout/sha`).
- Recommendation: Specify the adoption branch for every card:
  - set `timeout` (to `r.timeout` for non-unit cards; recompute it, or persist it, for unit cards);
  - re-read the full merge commit from GitHub, or persist all 40 hex;
  - persist the mode, for example as `mode:unit-create` in the reason;
  - state the exact reason grammar (`qa-job:<id> case:* sha:<40> mode:<m> <text>`) and the parser change.

  Add an adoption case to TC-846 or TC-849, and a regression test that an adopted e2e job is not timed out at once.

### The triage packet is not the packet the design changes - Severity: Major
- Affected scope: §7.5 (second paragraph and the branch table, row `autofix.go:397`), §12 `dashboard/autofix.go`
- Finding: The design says "The triage packet for a unit defect is a new `renderUnitDefectPacket` (beside `renderReviewDefectPacket`, `autofix.go:397`)". In fact:
  - `autofix.go:383-399` is `fixerPartsLocked`, the fixer's packet.
  - The triage packet is `defectPartsLocked` at `defecttriage.go:456-471`, and it always calls `renderDefectPacket`. That function prints QA fields: `Case`, `Spec`, `Operations`, `Level`.
  - Review defects never needed a triage branch, because they are born `triaged` (`reviewdefect.go:172`). Unit defects are the first non-QA kind to go through triage.

  As written, the triager sees the QA-shaped packet and the fixer loses the "triage verdict" section. An engineer could reasonably change either function, or both.
- Recommendation: Name both branches:
  - `defectPartsLocked` (triage) renders `renderUnitDefectPacket`;
  - `fixerPartsLocked` keeps the QA path, with the verdict.

  Add `defecttriage.go` (the packet) to §12's row. Add a test that asserts the fenced heading and the absence of QA fields in the triage packet.

### Which base branch the fix targets is undecided - Severity: Major
- Affected scope: §4.2 (planner conditions), §7.5 table (rows `autofix.go:194`, `autofixpr.go:239`, `retarget.go:81`)
- Finding: The design contradicts itself:
  - The fix "targets `AutoFix.BaseBranch`" (`autofixpr.go:235`).
  - Retargeting is refused because "A unit-test fix follows its merge commit".
  - The context is read at the merge commit (§7.5).

  Nothing requires the merged pull request's base to be `AutoFix.BaseBranch`, or to be in `AllowedBases`. A merge into a feature branch would file a defect whose fix is opened against a branch where the tests may not fail, built from files read at a different commit.
- Recommendation: Pick one rule:
  - either `withUnitQA` also requires the merged base to be `BaseBranch` or in `AllowedBases`, and the fix targets that base (with the reason in D-104);
  - or set `BaseOverride` to the merged base.

  Then reword the retarget refusal to match, and cover it in TC-842 or TC-852.

### Saving the main Auto-fix form wipes `UnitDefectCap` - Severity: Major
- Affected scope: §4.1 ("`UnitDefectCap` travels inside the existing `autoFix` document"); `handlers_autofix.go:60-99`, `defects.go:692`
- Finding: `autoFixFromForm` builds the whole `AutoFix` record ("the form replaces the whole record"), and `SetAutoFix` assigns `m.AutoFix = a`. Every save of the existing form therefore resets `UnitDefectCap` to 0, which means the default of 5, and the change does not appear in either event.
- Recommendation: Do one of the following, and add a test for it:
  - state that `SetAutoFix` keeps the current `UnitDefectCap`;
  - or move the field into `UnitQAConfig`, which `SetUnitQA` alone writes.

### The phases depend on later phases - Severity: Major
- Affected scope: §9 table and phase exits
- Finding: Several phases cannot be built or tested in the stated order:
  - **Phase 1** tests `TestUnitQAIsSavedAndRefused`. But the refusals in §4.1 need `unitSandbox` and `sandboxOK` (phase 3), and the route and handler (phase 7).
  - **Phase 2** builds `ownUnitPRLocked`, which matches `qa-unit` rows. `PRKindQAUnit` only arrives in phase 6.
  - **Phase 3**'s outcome table (TC-849) ends in `PlanUnitDefect` (phase 4).
  - **Phase 3**'s exit ("files one held card") needs phases 4 and 5.
  - **Phase 7**'s only test is admin-only (TC-863), so the six refusal texts in §4.1 have no test that is clearly theirs.

  Each phase is supposed to leave a buildable, testable tree. These do not.
- Recommendation: Do two things:
  1. Re-order the phases or move items between them:
     - put the `PRKindQAUnit` constant in phase 1;
     - test the `SetUnitQA` store method only in phase 1, and test the handler's refusals in phase 7 with their own TC;
     - in phase 3, either stub `PlanUnitDefect` or move the failure rows of the outcome table to phase 4.
  2. Restate each phase exit so it uses only that phase's code.

### The notifier changes are not specified and the files are missing from the change list - Severity: Major
- Affected scope: §5.2 (notices), §7.3, §8.2, §10 last bullet, §12
- Finding: Notice kinds are not inert strings:
  - `notify.go:40` sends `e2e:approval` to the group. Every other kind goes to the pull request's committer (`pullCommitAuthors`). `unit:approval` would therefore go to the committer, contrary to its purpose ("open the QA Inbox").
  - `contacts.go:67` holds a closed `notifyKinds` vocabulary. `unit:failure` is already in it; `unit:files` is not.
  - The text of the `unit:files` notice is never given.
  - Neither `notify.go` nor `contacts.go` is in §12.
- Recommendation: Decide where each of the three kinds goes:
  - `unit:approval` goes to the group, like `e2e:approval`;
  - `unit:files` should probably go to the group;
  - state whether `unit:files` and `unit:approval` join `notifyKinds`.

  Then give the `unit:files` text, add both files to §12, and add a test.

### The store method that closes defects on a pass is not named - Severity: Major
- Affected scope: §6 (row `passed`), §7.4 "Passed", §12 `dashboard/defects.go` ("passed-closes rule")
- Finding: The runner runs outside the store, so it needs a `Store` method to set the repository's unit defects to `fixed` and cancel their cards. The design does not give:
  - the method's name or signature;
  - its `ArcadeStore` or `readOnly` forms;
  - its events (`DefectStateChanged` through `setDefectStateLocked`?);
  - what happens when its store call fails.

  §6's poll order covers only the failure answer.
- Recommendation: Specify the method, for example `PassUnitDefects(ctx, productID, repo, sha string) (Result, error)`, with its forms, its events and its `Result.Defects`. Extend the poll order to the `passed` answer, so a store error retries before the card is closed.

### The `qa-unit` row is checked once; later pushes are never re-checked - Severity: Major
- Affected scope: §8.2
- Finding: The check runs only "for a `qa-unit` row whose `QANote` is empty". After `checked`, nothing re-reads it, so a later push to `qa-unit/*` that adds non-test files, or marks the PR ready, is never caught. Some of what the check needs is also not specified:
  - `pollRow` has `HeadSHA` (`fixprpoll.go:365`), but `ghPRState` has no `ChangedFiles` (`fixprpoll.go:54-55`), which step 4 needs.
  - No store write for `QANote` is named: `PRFields` has no note field (`pullrequests.go:71-80`).
- Recommendation: Do three things:
  - re-run the check whenever `HeadSHA` changes, keeping the checked sha with the note;
  - add `ChangedFiles` (and `Draft`, if needed) to `ghPRState`;
  - name the store method that writes `QANote`, or extend `PRFields` and `AdvancePullRequest`.

  Cover the re-check in TC-861.

### The `unit` defect branch in `buildAndStoreDefectContext` is undecided - Severity: Major
- Affected scope: §7.5 first paragraph; `defecttriage.go:428-436`
- Finding: The design says "`buildAndStoreDefectContext` … refuses a `unit` defect … so a released triage card with no context is BLOCKED `BlockSearchFailed`". It does not say where the refusal sits. The function returns nil early when `src == nil`, so a refusal placed after that check lets the triage card run with "No source files could be loaded". It is also unclear whether the refusal returns an error, which is what blocks the card through the existing path at line 445. There is no TC for any of this.
- Recommendation: State the exact order:
  1. if `d.Kind == unit` and the context is not built, resolve the triage card BLOCKED `BlockSearchFailed` and return an error, before the `src == nil` return;
  2. otherwise continue as today.

  Add it to TC-855.

### `UnitQA` off still changes today's labels - Severity: Nit
- Affected scope: §2 last line, §4.2 first paragraph
- Finding: Making the `unit` family planner-only (`prflow.go:549-557`) also stops an operator from setting the existing `unit:test` label, whether or not `UnitQA` is on. That contradicts "With `UnitQA.Enabled` false, nothing changes".
- Recommendation: State the side effect in §2 and in D-104, or refuse only `unit:qa`. Also name the constant (`LabelUnitQA`) and its place in `labelOrder`.

### The planner hook cannot evaluate its own third condition - Severity: Nit
- Affected scope: §4.2 third bullet, §8.3
- Finding: `withUnitQA(m Master, ev, specs)` is a pure function, so it cannot check "the pull request is not the product's own", which needs `s.pullRequests`. `ownUnitPRLocked`'s place inside `planPullRequestCardsLocked` is not given: before or after `reviewRowLocked` at line 290, and the merge-cancel loop at line 256. The order of `withUnitQA` against `withRepoKind` at line 225 is not given either.
- Recommendation: Drop the third bullet from `withUnitQA`. Give the exact position of the `ownUnitPRLocked` early return, for example "beside line 251, for both event types", and the order of the two calls.

### The handler refers to `flow.gh` and uses 400, unlike its sibling - Severity: Nit
- Affected scope: §4.1 refusal table, §7.5 (`jr.code, _ = flow.gh.(codeSource)`)
- Finding: A handler cannot see `flow`; the server has `s.codeSrc` (`main.go:388`). Also, `autoFixError` answers 422 for a rejection (`handlers_autofix.go:111`), while §4.1 says 400.
- Recommendation: Use `s.codeSrc == nil` and `jr.code = srv.codeSrc`, and pick one status code.

### The API refusal's stated reason is partly wrong - Severity: Nit
- Affected scope: §4.2 "The external-tasks API"
- Finding: The design says "the API path has no merged pull request to read a commit from". But `api_tasks.go:211-228` does read the pull request and requires `pr.Merged`. The second reason given, duplicate keys, stands.
- Recommendation: Keep only the duplicate-key reason. Say whether the refusal sits in the handler or in `PlanAPICard`, which is atomic against a concurrent `SetUnitQA`.

### How `PlanUnitDefect` reports a refusal is not stated - Severity: Nit
- Affected scope: §7.1 signature, §6 refusal sentence
- Finding: It is unstated how a refusal comes back (`Result.Skipped`, as `PlanReviewDefect` does?) and how the runner reads the `<why>` from it. It is also unstated that the admin's cancel in `MoveCard` must return the closed defect in `Result.Defects` so `ArcadeStore` persists it.
- Recommendation: State both.

### The change list uses a glob and vague doc entries - Severity: Nit
- Affected scope: §12 last row
- Finding: `pustaka/docs/assets/toc/*.js` is a glob over hand-written files; there are 8 in the directory, plus `toc.js`. "The unit-test docs" does not say what changes in `pr-agents.html` or `agents.html`.
- Recommendation: List the exact toc files (probably `toc/product.js` and `toc/integrations.js`) and one line of content per page.

---

**Sections that are sound:**
- §5.3's attestation predicate is fully decidable.
- §7.1's record table is fully decidable.
- §7.2's gate order is fully decidable.
- §7.3's table of the ways a card leaves BLOCKED matches the code (`legalMove`, `board.go:213-241`, only allows unblock and cancel from BLOCKED).
- §11's contract is clear.

Counts: B=1 M=8 N=6
