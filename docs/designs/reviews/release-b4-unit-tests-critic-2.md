# Goldfish review: `docs/designs/release-b4-unit-tests.md` (rev 2)

**Overall.** The rev-2 design fixes most of round 1. It still has one wrong premise: that a job adopted after a restart needs nothing more. It also has a cluster of mechanisms anchored on the wrong function or on state that does not exist: the triage packet branch, the card "description", the QANote writer and the poller's notifier. Three things contradict the registers or the code: the BR-54 amendment, the D-088 notice rule and the reason the API refusal gives. And settings drift: saving the existing Auto-fix form silently resets the new cap.

**Confirmed.** Most anchors check out (minor line drift is noted under Nits):
- The labels count really is 19 → 20 (`labels.go:95-115`).
- The identifiers are free: D-104 (the log ends at D-103, `decision-log.html:2148`), BR-63 (the BRD ends at BR-62, `brd.html:1308`) and TC-839 onward (the plan ends at TC-838, `qa-test-plan.html:929`).
- The TC map adds up to 25.
- The schema regexps are what the design alters (`schema/19-review-defects.sql:8`, `schema/18-pull-requests.sql:13`).
- `PullRequest.qaNote` already exists (`schema/20-release-b.sql:7`).

---

### Restart adoption loses the timeout, the commit and the mode - Severity: Blocker
- Affected scope: §4.3, §5.1 last sentence, §5.2, §6, §8.1
- Finding and rationale: §5.1 says *"A job adopted after a restart needs no commit: it is already started."* That is wrong.
  - **Adoption skips every new setting.** The adoption path in `qaJobRunner.start` returns early (`qajobs.go:355-361`), before any of the new start-time settings are made. That leaves:
    - `qaTracked.timeout` at zero;
    - `qaTracked.sha` empty (the reason carries only 12 hex);
    - the mode (`unit-run` or `unit-create`) unknown.
  - **Adopted jobs time out at once.** §4.3 makes the poller compare `active` against `t.timeout` instead of `r.timeout` (`qajobs.go:465`). Every adopted job, the existing e2e jobs included, would hit `qa-job-timeout` on its first poll after a restart.
  - **The outcome path still needs the full commit.** `PlanUnitDefect` takes `SHA`. `buildUnitContext` reads at the merge commit. The `qa-unit` row stores `QASha: <merge sha>`. A 12-hex prefix fails `shaRe` (`staging.go:186`, which accepts 40 or 64 hex only).
  - **The create branch cannot be decided.** `recordQARow`'s unit branch runs only "when the job is a `unit-create`", which adoption cannot tell.
- Recommendation:
  - In the adoption branch, set `timeout` (the unit or e2e default).
  - Re-read `merge_commit_sha` from GitHub. The key carries `owner/repo#n`, so the same §5.1 read works.
  - Persist the mode in the reason (for example `mode:unit-create`), as the case already is.
  - Add an adoption case to TC-846 or TC-849, and an e2e adoption regression test for the timeout.

### The unit packet is wired into the fixer, not the triager - Severity: Major
- Affected scope: §7.5 (the packet paragraph and the `autofix.go:397` row); §12 `dashboard/autofix.go`
- Finding and rationale:
  - **The anchor is the fixer's packet.** `autofix.go:397` is inside `fixerPartsLocked` (`autofix.go:383-411`).
  - **The triager's packet is untouched.** It is `defectPartsLocked` → `renderDefectPacket` (`defecttriage.go:456-470`), which the change list does not touch. Built as written, the triager gets the QA packet (empty `Spec` and `Operations`, test names under "Failure excerpt"), not the fenced unit packet that round-1 SEC-9 asked for.
  - **The fixer loses the verdict.** The review branch returns before the verdict section is appended (`autofix.go:397-403`). Copying it for `unit` drops the triage verdict from the fixer's packet, and review defects have no triage but unit defects do.
- Recommendation: Put the `unit` branch in `defectPartsLocked` (the triager). Keep the fixer on `renderDefectPacket` plus the verdict, or give it a unit variant that keeps the verdict. Add `defecttriage.go` (`defectPartsLocked`) to §12, and add a TC that inspects both packets.

### The BR-54 amendment forbids what §7.4 does - Severity: Major
- Affected scope: §10 BR-54; §7.3 last table row; §7.4 "Passed"
- Finding and rationale: The amendment ends *"no other existing card is moved."* §7.4 does exactly that on a `passed` answer: it sets defects to `fixed` and calls `cancelDefectCardsLocked`. That cancels every unstarted card of those defects, including READY fixer cards and held triage cards (`defects.go:363-380`). BR-54's current text already says *"a QA result still never moves an existing card"* (`brd.html:1351`). Round-1 finding 17 was fixed by rewording, and the rewording opens a new contradiction.
- Recommendation: Either state the exception in BR-54 ("…and a `passed` result cancels the unstarted cards of that repository's open unit defects"), or drop the cancel from §7.4. Then record which one D-104 chose.

### An older commit's pass can close a newer failure - Severity: Major
- Affected scope: §7.4 "Passed"; §3 step 6
- Finding and rationale: `Tick` starts every PENDING `unit:qa` card, so jobs for merges M1 and M2 run at the same time (`qajobs.go:252-262`). Suppose M2 fails first and files a defect. Then M1's job, at the older commit, reports `passed`. Every non-final unit defect of the repository is set to `fixed`, and its held triage card is cancelled. A real regression is silently closed by a stale result. Nothing compares commits or merge times.
- Recommendation: Close only defects whose `RunRef` commit is older than the passing commit. Record `merged_at` from the §5.1 read on the defect and compare. Or, more simply, close only when the passing job's merge is newer than the defect's `LastSeen` sighting. Add a TC for out-of-order results.

### Saving the existing Auto-fix form resets `UnitDefectCap` - Severity: Major
- Affected scope: §4.1 (`UnitDefectCap` "travels inside the existing `autoFix` document")
- Finding and rationale: `autoFixFromForm` builds a fresh `AutoFix`, and the code says *"the form replaces the whole record"* (`handlers_autofix.go:60-99`). The main form has no `unitDefectCap` field. Every save of `POST /products/{id}/autofix` (`main.go:577`) therefore resets the cap to 0, the default of 5, with no notice. The design edits the field through a different route and never mentions this.
- Recommendation: Move the cap into `UnitQAConfig` (the record the unit route owns). Or have `SetAutoFix` keep `UnitDefectCap`, and add a TC for that.

### Notice texts and routing break D-088, and two kinds are not in the vocabulary - Severity: Major
- Affected scope: §5.2 Notices; §7.3; §8.2 (`unit:files`)
- Finding and rationale:
  - **The texts name a repository.** All three texts contain `<repo>#<n>`. D-088's rule, restated in code, is *"Like every line (D-088), it names no repository"* (`notify.go:95`). Every line ends with "· card <id>" (`notify.go:82`).
  - **Approval notices go to the wrong person.** `loginFor` sends only `e2e:approval` to the group (`notify.go:40`). `unit:approval` would go to the merge's committer instead of whoever approves in the QA Inbox.
  - **Contacts cannot subscribe to the new kinds.** `notifyKinds` is a closed vocabulary with no `unit:approval` or `unit:files` (`contacts.go:67-69`, `contacts.go:92-93`).
  - **The change list misses both files.** `notify.go` and `contacts.go` are absent.
- Recommendation: Rewrite the texts without the repository. Route `unit:approval` to the group. Add both kinds to `notifyKinds` (or say they are never subscribable). Add both files to §12.

### The poll-time check has no store writer and no notifier - Severity: Major
- Affected scope: §8.2; §12 (`fixprpoll.go`, `main.go`, `store.go`)
- Finding and rationale:
  - **No store method writes the note.** The poller writes only `PRFields` through `AdvancePullRequest`, which has no `QANote` (`pullrequests.go:70-80`, `214-252`). The only `QANote` writer is the merged-run path (`mergedrun.go:63`). §8.2 needs a new store method that sets the note and emits `RecordUpdated` in one transaction; the `Store` interface, `ArcadeStore` and `readOnly` are not in §12.
  - **The poller has no notifier.** `fixPRPoller` has no `nt` (`fixprpoll.go:108-126`); `main.go:444-456` gives it only `alert`. `unit:files` therefore cannot go out.
  - **The poller has no card.** The row has a `TaskID`, but `Notify` takes a `TaskRecord`.
- Recommendation: Name the new method (for example `NoteQAUnitRow(ctx, productID, key, note, actor)`). Give the poller a notifier in `main.go`, or use `NotifyPR` (`notify.go:97`). List both in §12.

### Reopening a unit defect is "adapted" without saying how - Severity: Major
- Affected scope: §7.4 "Seen again, final"; §7.1 `Files`, `Repo`
- Finding and rationale: `reopenDefectLocked` (`defects.go:544-559`) has three effects the design does not decide:
  - **It sets `d.Regression = true`.** `autoClaimLocked` then refuses any automatic fix (`autofix.go:175-177`).
  - **It clears `Files` and `Repo`.** §7.1 fills both from the failing set.
  - **It clears `Context` and rescreens from `it.Excerpt`.** `it` is a QA `DefectItem` that a unit sighting does not have.

  Separately, `Defect.files` is documented as *"the verdict's files"* (`schema/17-defects.sql:34`). §7.1 puts the failing test files there before triage, giving the field two meanings. Two engineers would build two different reopens.
- Recommendation:
  - Write a `reopenUnitDefectLocked` with an explicit field list.
  - Decide whether a unit regression may auto-fix.
  - Keep the failing files in a field of their own, or in the excerpt, not `Files`.

### The held card has no "description" to show the failing tests - Severity: Major
- Affected scope: §7.3 ("The card's description lists the kept failing names and files")
- Finding and rationale: `TaskRecord` has no description field (`model.go:846-890`). `planDefectCardLocked` takes only a title, a reason, an actor and a rule (`defecttriage.go:76-78`). The admin's informed release, which is the point of the hold, depends on state that does not exist. Neither the schema nor the change list says where it would live.
- Recommendation: Say where the admin reads the names. The defect's row on the Auto-fix page (`autofix_view.go`) already holds `Excerpt`: link the held card to it. Otherwise add a field, its schema statement and its persistence to §12.

### The API refusal's reason is false, and the published API docs are not updated - Severity: Major
- Affected scope: §4.2 "The external-tasks API"; §12
- Finding and rationale: The design says *"the API path has no merged pull request to read a commit from."*
  - **The API does have a merged pull request.** It refuses `unit-test` unless `pr.Merged` (`api_tasks.go:226-228`).
  - **It uses the same key as Brain Town.** It builds a `pr.merged` event, so the runner's §5.1 read would work unchanged (`api_cards.go:141-148`).
  - **The new 422 code is a breaking change to a published contract.** The published 422 codes are `invalid-kind`, `invalid-field` and `repo-not-owned` (`pustaka/docs/downloads/gaos-external-tasks-api.md:115`). The kind is documented at lines 16, 46 and 84. Neither that file nor `pustaka/docs/integrations/external-tasks-api.html` is in §12.
- Recommendation: Either map an API `unit-test` to `unit:qa` on a UnitQA product (same key, one run), or keep the refusal with a true reason. Either way, add both docs pages to §12.

### Phase exits depend on later phases - Severity: Major
- Affected scope: §9
- Finding and rationale:
  - **Phase 3's exit needs later phases.** It is *"a fake QA tool answering `failed` with two tests files one held card"*, which needs `PlanUnitDefect` (phase 4) and the hold (phase 5).
  - **Phase 1's test needs phase 7.** TC-839 `TestUnitQAIsSavedAndRefused` sits in phase 1, but four of the six refusals in §4.1 (the capability, attestation and GitHub-reader checks) are in the handler, and the route lands in phase 7.
  - **No phase can end green as written.**
- Recommendation: In phase 3, stop at "the runner calls a stub `PlanUnitDefect` with the right input". Move the held-card exit to phase 5. Split TC-839 into the store's checks (phase 1) and the handler's (phase 7, folded into TC-863).

### The `qa-unit` file check runs once and never again - Severity: Major
- Affected scope: §8.2 ("for a `qa-unit` row whose `QANote` is empty")
- Finding and rationale: After the first pass sets `QANote = "checked"`, the check never runs again. The unit identity can still push to `qa-unit/*` (§11), and the QA tool may push again after a re-approval. A later push that adds `src/app.go` or `.github/workflows/x.yml` is never seen. That defeats the check that §8.4 and §11 lean on. The poller already reads `HeadSHA` (`fixprpoll.go:365`).
- Recommendation: Store the head SHA that was checked (in `QASha` or a new field). Re-run the check whenever `gs.HeadSHA` changes. Add that case to TC-861.

### The gate does not check that a triager is assigned - Severity: Major
- Affected scope: §7.2 step 1
- Finding and rationale: The gate copies `reviewPlanRefusalLocked`'s fixer-only check (`reviewdefect.go:100`). A review defect is born triaged with a fixer card (`reviewdefect.go:172-183`); a unit defect is born with a **triager** card (`defecttriage.go:98`). On a product with a fixer and no triager, the defect is filed, uses up the cap and is held, and once released no agent works it.
- Recommendation: Add `!roleAssigned(*m, "triager")` → `no triager assigned` to the gate, and to TC-853.

### The create timeout is derived from a single sandbox run - Severity: Major
- Affected scope: §4.3
- Finding and rationale: `timeLimitSec` is the per-run limit of the sandbox (§11). A `unit-create` job runs the tests, writes tests and usually runs again before its approval wait, all on the active clock. `min(GAOS_QA_UNIT_TIMEOUT_MIN, timeLimitSec + 5 min)` will fail legitimate creates as `qa-job-timeout`, which files no defect. The text also says it is parsed "like `qaApprovalWaitFromEnv`", but that function has no minimum clamp (`qajobs.go:198-208`).
- Recommendation: Give creates their own budget (for example `2 × timeLimitSec + 10 min`, capped at the env value), or count only time before the first wait. Write the clamp out explicitly.

### Refusing to build context for a unit defect is a dead end - Severity: Major
- Affected scope: §7.5 first paragraph
- Finding and rationale:
  - **Moving the card to Ready just blocks it again.** A released card with no stored context goes BLOCKED `BlockSearchFailed`, and nothing can ever build it.
  - **The refusal does not always run.** `buildAndStoreDefectContext` returns `nil` before any kind check when `src == nil` or the context is already built (`defecttriage.go:428-430`), so the outcome depends on where the check is placed.
  - **The existing triager copes with no context.** It says *"No source files could be loaded. Answer medium…"* (`defecttriage.go:504-505`).
- Recommendation: Return `nil` for `unit` (no search) and let the triager run with what it has. Or have the autofix pass call `buildUnitContext` for unit defects. State which.

---

### Line anchors have drifted - Severity: Nit
- Affected scope: throughout
- Finding and rationale: The design cites `prKeyParts` at `prreview.go:198`; it is at `:196`. Other drift: `pollRow` is at `fixprpoll.go:320`, `AutoFix` at `model.go:436-469`, `buildAndStoreDefectContext` at `defecttriage.go:422-451`, and `caseCodeFor` starts at `qajobs.go:269`.
- Recommendation: Correct the anchors.

### "Two fields" on AutoFix, but one is listed - Severity: Nit
- Affected scope: §4.1 first sentence
- Finding and rationale: The text says `AutoFix` "gains two fields"; the block adds only `UnitDefectCap`.
- Recommendation: Say "one field".

### The status code differs from the existing route - Severity: Nit
- Affected scope: §4.1 refusals
- Finding and rationale: The design specifies 400; the sibling route answers 422 through `autoFixError` (`handlers_autofix.go:111`).
- Recommendation: Use 422.

### "Dispatch" is not a move out of BLOCKED - Severity: Nit
- Affected scope: §7.3 guard and its table
- Finding and rationale: `legalMove` allows only unblock and cancel from BLOCKED (`board.go:234-239`).
- Recommendation: Drop "dispatch".

### The planner-only `unit` family changes today's behaviour - Severity: Nit
- Affected scope: §4.2 vs §2
- Finding and rationale: An operator can set `unit:test` by hand today (`prflow.go:549-557`). Making the whole `unit` family planner-only removes that with UnitQA off, contradicting "nothing changes".
- Recommendation: Say so, or guard `unit:qa` only.

### `autoFixAdmin` would read a file under the store lock - Severity: Nit
- Affected scope: §7.3 guard
- Finding and rationale: `autoFixAdmin` stats and reads a file (`handlers_autofix.go:17-36`). Calling it inside `MemStore.MoveCard` does that I/O under `s.mu`.
- Recommendation: Resolve it in the handler and pass `Admin bool` (`json:"-"`) on `CardMove`.

### `QANote` gets a second meaning - Severity: Nit
- Affected scope: §8.2
- Finding and rationale: For `qa` rows the note means "the merged case did not run" (`mergedrun.go:63`, `105`). §8.2 reuses the same field for a file-check verdict.
- Recommendation: Say how the Pull requests page tells the two apart.

### Caps and the capability requirement are left unstated - Severity: Nit
- Affected scope: §7.2, §0
- Finding and rationale:
  - Unit fixes fall under `FixCap` because only review fixes have their own cap (`autofix.go:187-198`); the design does not say so.
  - The sibling design's requirement of "the planner knowing the capability" (`release-b-e2e-kinds-merge-unit-tests.md:138`) is replaced by a check at start and is not called out as a departure.
- Recommendation: State both.

### Which card the held-defect notice is sent on - Severity: Nit
- Affected scope: §5.2 vs §7.3
- Finding and rationale: The design does not say whether one notice or two go out (from the runner, and when the triage card is planned), or on which card. `loginFor` routes the two cards differently (`notify.go:40`).
- Recommendation: Name the one notice and its card.

### Some new behaviour has no TC - Severity: Nit
- Affected scope: §9
- Finding and rationale: No TC covers `renderUnitDefectPacket`, the notice kinds and texts, or an admin's cancel closing the defect.
- Recommendation: Add TCs, or fold these into TC-854 and TC-858 by name.

Counts: B=1 M=14 N=10
