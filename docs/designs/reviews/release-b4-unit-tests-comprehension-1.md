# Goldfish comprehension review: `release-b4-unit-tests.md` (rev 1)

Design reviewed: `/home/ardi/devel/projects/ardihikaru/my-projects/gaos-unit-design/docs/designs/release-b4-unit-tests.md`

## Paraphrase

### What this proposes

1. **A per-product switch, `UnitQA`, off by default.** When it is on, a merged pull request plans a `unit:qa` card instead of today's `unit:test` card. The card is keyed `<owner>/<repo>#<n>:unit-qa` and still needs an assigned `unit-test` agent.
2. **Who edits the switch.** An auto-fix admin turns it on from the Auto-fix page, through a new store method `SetUnitQA`. `Enabled` can only be saved after GAOS has just read the QA tool's capabilities, and those must list `unit` with a valid sandbox attestation. `Create` (letting the QA tool write tests) also needs `CIConfirmed`.
3. **Who runs the card.** The scheduler never dispatches `unit:qa`. The QA-job runner drives it, the same way it drives e2e cards:
   - It sends `mode: unit-run` or `unit-create`, `repo`, the merge commit `sha`, and `caseCode "*"`.
   - The merge commit comes from a new `MergeCommitSHA` field, read during the GitHub re-read of a merged pull request. If there is no merge commit, no card is planned.
   - The card's active-time timeout is a new setting, `GAOS_QA_UNIT_TIMEOUT_MIN`.
4. **Attestation at every start.** Before each start the runner checks the `unit` capability and the sandbox attestation. The attestation must carry exactly the listed values and be less than 7 days old. If not, the card goes to BLOCKED.
5. **The job answer gains a `unit` object** (passed, failed or no-tests; up to 50 failing tests; the files a create added). Outcomes:
   - The job itself fails: the card is FAILED and no defect is filed.
   - The tests fail: the card is DONE and a unit defect is filed.
   - The tests pass: the card is DONE.
   - The result is missing or unknown: the card is FAILED `qa-unit-unreadable`.
6. **The unit defect.** It is a new defect kind `unit` (schema 21), filed by `PlanUnitDefect`:
   - It is backend by decision.
   - Its dedup key is the repository plus a hash of the failing test names.
   - New unit defects are capped per day.
   - Its source slice is built with `buildReviewContext` at the merge commit.
7. **The triage card is born BLOCKED** with `ReasonUnitHeld`. Two things are meant to keep it there: auto-claim refuses it, and the board move handler refuses anyone who is not an auto-fix admin. Once an admin releases it, the existing D-091 path (triage, fix, draft fix PR) runs unchanged.
8. **Tests the QA tool wrote** become a Pull requests row of kind `qa-unit`. GAOS checks the reported files against `TestGlobs` and the branch against the `qa-unit/` prefix. If either check fails, it adds a note and sends a notice; it never closes the pull request.
9. **Register changes:** D-104 and BR-63 are new; BR-44, BR-54 and D-091 (8) are amended; two new settings, schema 21, and the label count goes from 19 to 20. §9 is the contract the QA tool must build.

### How the existing system works, per this document

- A merge plans one `unit:test` card for the `unit-test` role. The scheduler gives it to an agent that has no checkout and runs nothing. A FAILED card only sends a notice. Nothing actually runs the tests.
- The QA-job runner already drives `e2e:*` cards. It starts a job, polls it, moves the card to Needs you during an approval wait, and has a 40-minute active timeout.
- `PREvent.Commit` is set only for staging events. `prFlow.handle` already re-reads a merged pull request from GitHub to set `MergeChecked`.
- Review defects (D-102) are the model to copy: kind `review`, `be` by decision, a source slice from `buildReviewContext`, and `SetDefectContext` called outside the lock.
- Any signed-in account can move any card today.
- `AutoFix.CIConfirmed` covers `gaos/fix-*` branches.

I could paraphrase every section. The findings below are about wrong premises, missing decisions, and anchors that do not hold up against the repository.

## Findings

### Nothing says where the merge commit is stored on the card - Severity: Blocker
- Affected scope: §3 "Start commit", §4.2, §2 step 2
- Finding: The runner must send `sha`, and §4.2 says the request body "is built only from the card's key and the product record". But the key is `<owner>/<repo>#<n>:unit-qa`, which holds no commit. `TaskRecord` (`model.go:846-891`) has no commit field. `qaJobRunner.start` copies only `ID`, `DeskID`, `DedupKey` and `Labels` into the tracked card (`qajobs.go:351`). §3 writes the merge commit onto `PREvent.Commit`, but the event is not persisted on the card. So the sha the runner needs exists nowhere it can read, including after a restart, when the job is re-adopted from `BlockReason`. Two engineers would invent different stores for it: the key, the reason, a new task field, or a GitHub re-read at start time.
- Recommendation: Decide where the sha lives. One option is a key suffix like `#<n>@<sha>:unit-qa`; another is a new persisted `TaskRecord` field added to `store_arcade.go`. Say how the runner reads it and validates it with `shaRe`, and correct the sentence in §4.2.

### A Blueprint sync releases the held triage card automatically - Severity: Blocker
- Affected scope: §6.3 "Two guards keep it held"
- Finding: There is a third path out of BLOCKED that the document does not list. `releaseDriftLocked` (`workspace.go:384-400`) moves every BLOCKED task on the desk to READY unless `operatorHolds(reason)` is true. `operatorHolds` (`sendback.go:153-159`) only knows `ReasonDesignBlocked`, `ReasonDesignUnreviewed` and `ReasonQaEpicAdded`. So any drift release after a Blueprint sync would release a `ReasonUnitHeld` card without an admin. That breaks the core promise of D-103 (5), "a person releases before any agent works it". Also, `moveTaskLocked` does not clear `BlockReason` here, so a released card would sit in READY with a stale held reason. What the two guards do in that state is undefined.
- Recommendation: Add `ReasonUnitHeld` to `operatorHolds` (`sendback.go`), add `sendback.go` to §10, and add a test (a drift release leaves the held card BLOCKED). Better still, list every path out of BLOCKED and say which guard covers each one.

### The `qa-unit` row is never written: the row path drops `caseCode "*"` - Severity: Major
- Affected scope: §4.2, §7
- Finding: §4.2 sets `CaseCode` to `"*"` for unit modes. `recordQARow` marks a job `recorded` and returns without writing a row when `t.caseCode == "*"` (`qajobs.go:519-521`). §7 says `recordQARow` writes the `qa-unit` row, but never says this guard changes.
- Recommendation: State that `recordQARow` branches on the unit label before the `"*"` guard, and add a test for it.

### The unit modes do not fit `isE2ECard` / `caseCodeFor` / `doneReason` - Severity: Major
- Affected scope: §2 step 2, §4.1, §4.2
- Finding: `isE2ECard` returns a mode string that the rest of the runner branches on:
  - `caseCodeFor` returns `"*"` only for `mode == "run"`. Any other mode parses the pull request body for a `QA-case:` line, and BLOCKs the card when it finds none (`qajobs.go:279-295, 363-368`).
  - `doneReason` treats `mode == "create"` specially (`qajobs.go:552`).
  - `fail` and the approval wait send `e2e:failure` / `e2e:approval` notices with "spec" wording (`qajobs.go:448-451, 499`).

  The document says only that `isE2ECard` "grows a `unit:qa` branch". It does not say which mode string it returns. `Create` cannot be fixed on the label, because it is a product setting read at start time. It also does not say how the case-code lookup, the done reason and the notices change.
- Recommendation: Specify the return value for `unit:qa` and that the unit mode is resolved from `UnitQA` at start. Give the case-code short-circuit, the notice kinds (for example `unit:failure`, `unit:approval`) and the wait reason text for unit cards.

### The `PullRequest.kind` schema and the upsert both refuse `qa-unit` - Severity: Major
- Affected scope: §7, §8 schema line, §10
- Finding:
  - `schema/18-pull-requests.sql:13` restricts `PullRequest.kind` to `regexp 'review|qa'`. Schema 21, as described, only widens `Defect.kind`.
  - `upsertPullRequestLocked` rejects any kind other than review or qa (`pullrequests.go:111-113`).
  - It fills the QA fields only for `PRKindQA` (`pullrequests.go:148-153`).
  - `prview.go:27,30` has the kind labels and filter used by the Pull requests page.

  `pullrequests.go` appears in §1 but not in the §10 change list, and `prview.go` appears in neither. This breaks README rule 3 (the change list must be complete).
- Recommendation: Widen `PullRequest.kind` in `schema/21`. Name the changes to `upsertPullRequestLocked` and `prRepos`, and to the `prview.go` labels and filter. Add `pullrequests.go` and `prview.go` to §10.

### It is unclear which repository list a `qa-unit` row is checked against - Severity: Major
- Affected scope: §7 first paragraph
- Finding: "when the job's `pr_url` is in a repository of `TargetRepos` (`prRepos` gains the kind)" does not say which list is meant. `TargetRepos` is both a defect field (`defects.go:83`) and an `AutoFix` field (`model.go:457`), and the AutoFix list may be empty. `prRepos` today returns either `m.Repos` or `AutoFix.AutomationRepos` (`pullrequests.go:97-102`). The created test pull request is in the application repository. If the intended list is `AutoFix.TargetRepos` and it is empty, no row is ever recorded. §12 also says "repositories outside `TargetRepos`", which reads like a third meaning.
- Recommendation: Name the exact list `prRepos(m, PRKindQAUnit)` returns (`m.Repos`, or `m.AutoFix.TargetRepos`) and use that one name in §7 and §12.

### The file and branch checks trust the QA tool's own report, at a moment the branch is unknown - Severity: Major
- Affected scope: §7 bullets
- Finding:
  - The `TestGlobs` check reads `unit.files`, the QA tool's self-report. GAOS can list the pull request's real files (`pullFiles`, `github.go:228`) and does so for review fixes, but this design does not.
  - The head-branch check needs `HeadRef`. The row is written at poll time without it, and `HeadRef` is filled later by the GitHub poller (`PRFields`, `pullrequests.go:70-80`). So "the row's head branch must start `qa-unit/`" has no defined moment at which it runs.
  - "GAOS records it with `RecordUpdated`" does not say what is recorded.
- Recommendation: Say whether the file check uses GitHub's file list, which would be stronger. Say when the branch check runs (for example, in the poller once `HeadRef` is known) and what the `RecordUpdated` payload contains.

### What happens when the same failing set returns after the defect is fixed or closed - Severity: Major
- Affected scope: §6.1 dedup key
- Finding: The dedup key carries no commit or generation. "The same failing set seen again updates `LastSeen` and plans nothing" would hide a real regression once the earlier defect is final (`defectFinal`, `defects.go:322`). `PlanReviewDefect` says explicitly "one per pull request, no re-open" (`reviewdefect.go:160`). This design does not say whether that rule applies.
- Recommendation: Decide: either the defect is reopened with a new `Gen` and a new held triage card, or the duplicate is suppressed forever and that is listed as a residual. Add the matching test.

### The preconditions of `PlanUnitDefect` are not stated - Severity: Major
- Affected scope: §6.1, §6.3
- Finding: The model, `PlanReviewDefect`, refuses through `reviewPlanRefusalLocked` (`reviewdefect.go:97-118`) when:
  - no fixer is assigned;
  - auto-fix is off;
  - the repository is not a fix target, or is an automation repository;
  - the shared `defectMaxNewKeys` cap (50, `defects.go:59`) is reached.

  This design names only its own daily cap. It does not say whether a unit defect is filed when auto-fix is off. If it is, the released triage card can never be dispatched: `autoClaimLocked` refuses when `AutoFix.Enabled` is false (`autofix.go:164`) and `labelsAllowDispatch` refuses `defect:triage`. It also leaves unstated whether the two daily caps are checked before or after dedup, and which `CaseID`, `Class` and `Title` the record carries.
- Recommendation: List the refusal conditions, the order of dedup, cap and the shared cap, and the record's remaining fields. Say what the operator sees when filing is refused.

### A failed GitHub re-read drops the unit card with no retry - Severity: Major
- Affected scope: §3 "Start commit"
- Finding: On `pr.merged`, `handle` ignores errors from the re-read (`prflow.go:456-460`) and never returns `flowRetry` for it. With the new rule ("no merge commit, no `unit:qa` card, and no `unit:test` either"), one transient GitHub error or a missing GitHub client (`f.gh == nil`) means the merge gets no unit card, permanently, and only a log line records it.
- Recommendation: Say whether a transient re-read error retries (as `pr.opened` does at `prflow.go:466-468`). Say whether `SetUnitQA` refuses `Enabled` when there is no GitHub reader. Say whether the skip is shown to the operator, not only logged.

### Capability, attestation and mode rules are underspecified - Severity: Major
- Affected scope: §3, §4.1, §4.3
- Finding:
  - `capabilities` returns `map[string]bool` and has a TTL cache (`qaclient.go:67-92`), so it cannot carry the `sandbox` object. Neither the new accessor nor the `qaJobClient` interface change (`qajobs.go:142-147`) is specified, and "re-reads the capabilities at each start" conflicts with the cache.
  - "The planner knows the capability by the product record" does not say whether the planner checks how old `AttestedAt` is.
  - `AttestedAt` is described as "when GAOS last read a sandbox attestation", while the QA tool's `attestedAt` is a different timestamp. Two senses of one name.
  - "Every field has exactly these values (a stricter one is fine)" contradicts itself for every field except `timeLimitSec`.
  - The §4.1 table leaves two cases uncovered: the tool lists `unit` but not `unit-create`, and `Create` is true while `CIConfirmed` is false.
- Recommendation: Define the new client method and its cache rule, rename one of the two timestamps, and make the attestation rule one exact predicate. Complete the mode table for every combination of `Enabled`, `Create`, `CIConfirmed` and the `unit-create` capability.

### A BLOCKED attestation card has an unstated release step - Severity: Major
- Affected scope: §4.3 BLOCKED message
- Finding: The message tells the admin to re-save Unit tests on the Auto-fix page. But `Tick` starts only PENDING, READY, RUNNING and AWAITING_INPUT cards (`qajobs.go:257-262`). Re-saving the switch does not move the card, so someone must also move it to Ready, and the message does not say so. The same drift-release path as above would also release it without any re-save.
- Recommendation: Say how the card leaves BLOCKED: a board move after the re-save, or an automatic release when the attestation is valid again. Make the message name that step.

### The admin-only release guard is in the handler, not the store - Severity: Nit
- Affected scope: §6.3 second bullet
- Finding: The comparable guards, the design-gate hold and D-084, live in `MemStore.MoveCard` (`board.go:363-377`). Putting this one only in `handleMoveCard` leaves any other caller of `MoveCard` uncovered. It also answers 403 where the store's refusals answer 409.
- Recommendation: Say why the handler is chosen, or put the check in `MoveCard` with the actor passed in.

### Inaccurate line anchors - Severity: Nit
- Affected scope: §2, §5, §6.1, §6.3
- Finding:
  - `startAutomation` is at `qajobs.go:95`, not `:63`.
  - `qaJob` spans `qajobs.go:32-46`, not `32-43`.
  - `PlanPullRequestCards` is at `prflow.go:208`, not `206`.
  - `pullFiles` at `prreview.go:226` is the interface method; the implementation is `github.go:228`.
  - `MoveUnblock` is defined at `board.go:185`; `board.go:330` is `MoveCard`.
- Recommendation: Correct the anchors.

### The "label family" wording is confusing - Severity: Nit
- Affected scope: §3 "The label"
- Finding: "A new family `unit` already holds `unit:test`" contradicts itself. No `labelFamilyUnit` constant exists in `labels.go`, and the parent design (§5) asked for "a `unit:qa` card with its own family".
- Recommendation: Say whether a `labelFamilyUnit` constant is added, and whether it is planner-only like `defect`, `source` and `e2e-kind`.

### Project terms used without definition - Severity: Nit
- Affected scope: §0, §2, §6.3
- Finding: Brain Town, "Needs you" (the AWAITING_INPUT column), "the D-091 path", the Auto-fix page and the Pull requests row are used with no definition or link in this document.
- Recommendation: Add a short glossary, or link each term on first use.

### The excerpt cap truncates the failing list without saying so - Severity: Nit
- Affected scope: §5, §6.1
- Finding: 50 failing names of up to 200 characters each is up to about 10 KB. `screenDefectText` caps the excerpt at `defectMaxExcerpt` (4 KB, `defects.go:61`), so the excerpt may silently lose failing names.
- Recommendation: Say that the excerpt is truncated and how (for example, the first N names plus "and k more").

### The amendment wording sits loosely against BR-54 - Severity: Nit
- Affected scope: §8 BR-54 bullet
- Finding: BR-54 (`brd.html:1351`) says no QA result changes "the status or block reason of any existing task". Here, `unit.result` decides the `unit:qa` card's final status (FAILED `qa-unit-unreadable`) and its reason text. The proposed amendment says only "it still never moves an existing card".
- Recommendation: Word the amendment to cover the runner closing its own `unit:qa` card from `unit.result`, as the e2e job-status exception does.

### Solid sections
- §0, §11 and §12 are clear. The registers check out: D-104 and BR-63 are unused, the next test number TC-839 is free, and D-103 (5) matches the decision log and the parent design's §5.

Counts: B=2 M=11 N=6
