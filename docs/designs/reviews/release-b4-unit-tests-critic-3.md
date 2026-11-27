# Goldfish review: `docs/designs/release-b4-unit-tests.md` (rev 3)

**Lens:** hostile design critic, structural reviewer and style reviewer combined.
**Inputs read:** the design, `docs/rules/gaos-design-review.md`, `docs/designs/README.md`, the BRD (BR-05, BR-38, BR-44, BR-54, BR-62), D-103 in the decision log, sibling design §5, and the code under `dashboard/`.

## Verified and solid (one line each)

- **Register allocation:** BR-62 is the highest BR (`brd.html:1308`). D-103 is the last decision (`decision-log.html:2147`). TC-838 is the highest TC (`qa-test-plan.html:929`). So D-104, BR-63 and TC-839…TC-865 (27 cases, matching the per-phase sum 4+4+5+6+4+3+1) are free and consistent.
- **Label count:** there are 19 labels today (`labels.go:119-140`), so 19 → 20 is right. `labelsAllowDispatch` allows `unit:test` (`labels.go:179-188`).
- **Schema:** `21` is the next free number. The current regexps match the ALTERs (`schema/19-review-defects.sql:8`, `schema/18-pull-requests.sql:13`).
- **Anchors spot-checked and correct:** `prflow.go:88-91,225,232,343,430-436,541-557`, `qajobs.go:142-147,173,178,230,269,350-361,465,506,519-521,552`, `board.go:127-131,213-242,330,363-377,479`, `runner.go:457`, `sendback.go:153-159`, `workspace.go:384-400`, `defecttriage.go:44-68,78,142,262,422-451,456-471,505`, `defects.go:59,61,126,288,347,363,612`, `reviewdefect.go:97-118,143-194,319`, `github.go:44-58,228`, `staging.go:186`, `main.go:388,425,554,577`, `handlers_autofix.go:17,60-99,111`.

---

### qa-unit pull requests are never reviewed; the stated SEC-1 mitigation is false - Severity: Blocker
- **Affected scope:** §2 row 6, §8.3, §13 row "Skip review…", D-104 in §10, TC-846, Phase 2 exit.
- **Finding and rationale:** §11 requires the QA tool's pull request to be "always a draft". In the code, a draft `pr.opened` returns at `prflow.go:232-233` (`ErrPRSkipped: draft`). That happens before `ownUnitPRLocked` (§8.3 places it "right after the draft return"), and before any spec is planned. A `pr.merged` event carries no reviewer spec at all (`prflow.go:88-91`). Brain Town forwards only `pr.opened` and `pr.merged` (`braintown_consumer.go:97,175`), and nothing in the repository handles `ready_for_review`. So:
  - a `qa-unit` pull request gets no reviewer card, ever;
  - the `pr.opened` half of `ownUnitPRLocked` is dead code;
  - "a `qa-unit` pull request is reviewed like any other" is untrue;
  - TC-846 and the Phase 2 exit ("a `qa-unit` row's pull request still gets its reviewer card") cannot pass without a synthetic event that never occurs.

  The §13 rejection of "skip review" depends on this false premise.
- **Recommendation:** Decide one of two things, and record it in D-104.
  - (a) GAOS does not review `qa-unit` pull requests. State it as a residual, and drop the "still reviewed" claims, TC-846 and the Phase 2 clause.
  - (b) Add a trigger that does review them, for example a poller-planned reviewer card when the row leaves draft, with its own key and cap. Put it in §8.2 and §12.

### An empty `AutoFix.BaseBranch` (the default) breaks the base rule and every fix - Severity: Blocker
- **Affected scope:** §5.1 outcome table, §7.1 `BaseOverride` row, §7.5 `autofixpr.go:236-239` row.
- **Finding and rationale:** `BaseBranch` empty means "the repository's default branch" (`model.go:451-453`). §5.1 compares `base.ref` to `AutoFix.BaseBranch` literally. On a product with the default config, a merge into `main` fails the comparison and ends DONE "not an auto-fix base; nothing ran", unless `main` happens to be listed in `AllowedBases`. If an implementer fixes that case by reading it as "differs from BaseBranch", §7.1 sets `BaseOverride = "main"` on every defect. `autofixPass` then blocks every fixer card with `BlockBaseNotAllowed` whenever the override is not in `AllowedBases` (`autofix.go:350-355`). Two engineers will implement this differently.
- **Recommendation:** Define the effective base as `BaseBranch`, or, when it is empty, the repository's default branch, read from GitHub (`ghPR.Base.Repo` default branch, or `repoDefault`, `reviewdefect.go:256`). Compare against that. Set the override only when the merged base differs from the effective base.

### `BaseOverride` is overloaded with retarget semantics it already carries - Severity: Major
- **Affected scope:** §7.1 `BaseOverride` row, §7.4 reopen, §7.5 branch table.
- **Finding and rationale:** `BaseOverride` is documented and used as "the base a retarget set" (`defects.go:105-108`). Code paths that would misfire on a unit defect:
  - **Retarget looks pending.** `fixPRReplacedLocked` treats `BaseOverride != "" && FixPR == cur.ID && live fix card` as a pending retarget (`pullrequests.go:320-331`). So a unit fix PR closed while its card is live takes the "leave the defect alone" branch (`fixprpoll.go:448-449`). A merge takes the superseded-block branch (`fixprpoll.go:439-447`).
  - **The override is cleared too early.** A fix PR closed unmerged clears the override (`fixprpoll.go:465`), so the person-only second attempt opens against `BaseBranch`. That violates §5.1's "the fix opens where the tests failed".
  - **The fixer packet gets a wrong section.** `fixerPartsLocked` appends a "previous fix on the old base" section (`autofix.go:405-409`).
  - **Other readers.** `retarget.go:87` and `prview.go:533` also read it as a retarget.

  The §7.5 branch table lists only `autofixpr.go:236-239`.
- **Recommendation:** Add a dedicated `UnitBase` property (schema 21, `store_arcade_defects.go`). Read it in `autofixpr.go:235-238` and in `buildUnitContext`. Leave `BaseOverride` to retargets. Add the extra branches to the §7.5 table.

### `CloseDefect` lets any signed-in user cancel the held card - Severity: Major
- **Affected scope:** §7.3 "Every path out of BLOCKED" table, BR-63, D-104 ("admin-only, attributed release and cancel").
- **Finding and rationale:** `POST /products/{id}/defects/{ref}/close` (`handlers_autofix.go:156-175`) only checks for a session; there is no `autoFixAdmin` check. `CloseDefect` calls `cancelDefectCardsLocked` (`defects.go:606`). That function cancels BLOCKED cards (`defects.go:371-377`), including a `ReasonUnitHeld` triage card. The table claims to list every path out of BLOCKED, and it misses this one. The admin-only cancel rule is bypassed.
- **Recommendation:** Add the row. Either refuse `CloseDefect` on a unit defect with a held card unless the caller is an auto-fix admin (resolve `Admin` in the handler, as for `CardMove`), or state it as an accepted residual in D-104 and BR-63. Add the case to TC-858.

### The external-tasks API mapping does not work as described - Severity: Major
- **Affected scope:** §4.2 "The external-tasks API", §12 `api_cards.go` row, §13 last row, TC-845.
- **Finding and rationale:**
  - **No Master.** `apiSpec(productID, in)` has no `Master` (`api_cards.go:171`), so it cannot call `withUnitQA(m, …)`.
  - **The rewritten spec is never returned.** `apiSpec` keeps only the spec whose `Job == apiJob(kind)`, which is `prJobUnitTest` (`api_cards.go:182-183`). A spec rewritten to `prJobUnitQA` is never chosen.
  - **Wrong key, so duplicate runs.** The key comes from `apiKey`, which is `apiEvent(in).Key(apiJob(kind))`, so it ends in `:unit-test` (`api_cards.go:159-168`). Lookup and adoption (`api_cards.go:248,266`) therefore never find Brain Town's `:unit-qa` card. "Same key, one run, whichever comes first" is false, and duplicate runs follow.
  - **Endless refused dispatch.** `apiModelKind` marks `unit-test` as a model turn (`api_cards.go:126`), so `AutoDispatch` is set (`api_cards.go:372`). `apiAutoPass` then retries a dispatch that `dispatch` refuses on every pass (`apidispatch.go:115-128`).
- **Recommendation:** Specify the changes to `apiSpec` (signature, takes `Master`), `apiJob`, `apiKey` (Master-aware) and `apiModelKind` (false for a `unit:qa` mapping). Add them to §12, and make TC-845 assert key equality with the Brain Town card.

### A running unit job holds the product's single live turn (BR-38) - Severity: Major
- **Affected scope:** §4.3, §5, D-104; not addressed anywhere.
- **Finding and rationale:** The scheduler's graph lease counts any RUNNING task of the product (`scheduler.go:226-238`). So does the board dispatch (`board.go:450-458`, `liveTurnLocked`). The runner marks the `unit:qa` card RUNNING (`qajobs.go:393`) for up to 60 minutes (§4.3). With `UnitQA` on, every merge into a fix target therefore stops all agent work on the product, including the triager and fixer of the unit defect itself, for the whole job. e2e cards already do this, but they are not planned on every merge. The cost is not stated, and neither is the trade-off.
- **Recommendation:** Decide and record it in D-104. Either accept the stall, with the expected per-merge cost, or exclude QA-tool-driven cards from the lease. The second option needs an amendment to BR-38 ("one live **turn**"), because a QA job is not a model turn.

### Stuck unit cards count toward `GAOS_PR_MAX_OPEN` and can starve PR review - Severity: Major
- **Affected scope:** §4.2 last sentence of the planner paragraph, §5.1, §5.3, §4.1.
- **Finding and rationale:**
  - **Stuck cards count.** `openPRCardsLocked` counts every non-final PR card (`prflow.go:177-197`), and the default cap is 5 (`prflow.go:54-62`). BLOCKED `ReasonUnitAttest`, BLOCKED `ReasonUnitNoCommit` and AWAITING_INPUT create waits (up to 24 h) all count.
  - **One lapse blocks every merge.** §5.3 blocks on *any* error of the fresh capability read (no retry, unlike §5.1's 10 tries). A QA-tool restart, or an attestation older than 7 days, blocks each merge's card.
  - **Rate limits block too.** GitHub secondary rate limits answer 403, which `lookupTransient` treats as permanent (`prflow.go:430-436`).
  - **Review starves.** After five such cards, `PlanPullRequestCards` refuses new reviewer and e2e cards with `cap` (`prflow.go:298-300`), so one QA-tool problem stops PR review.
  - **No runner at all.** If `GAOS_QA_URL` is later removed, the runner never starts (`main.go:424`) and `unit:qa` cards stay PENDING forever.
- **Recommendation:**
  - Exclude `unit:qa` cards from `openPRCardsLocked`, as staging creates are excluded (`prflow.go:186-187`), and give them their own cap.
  - Treat a transient capability-read error like §5.1: stay PENDING and retry.
  - State what happens when the runner is absent.

### `unit-create` result semantics are undecided - Severity: Major
- **Affected scope:** §6 outcome table, §11 "Status" row, §3 step 4.
- **Finding and rationale:** In a create, the QA tool writes tests, waits for approval, runs "usually again", then reports one `unit` object. The contract does not say whether `result`/`failing` covers the repository's existing tests at `sha`, the generated tests, or both.
  - If generated tests fail, GAOS files a defect, and a fixer edits application code to satisfy tests that exist only on an unmerged `qa-unit/*` branch.
  - The failure of the existing tests reaches GAOS only after the approval wait, which can take up to 24 h (`qajobs.go:198-207`).
  - An approval timeout (`qajobs.go:459-463`) turns real test failures into a job failure, which files nothing.
- **Recommendation:** In §11, require `unit` to report the pre-existing suite at `sha` only. Either have the QA tool report it before the approval wait, for example as a run job followed by a separate create, or state that generated-test failures never file a defect.

### "The store sends" the held-card notice, and its once-per-day rule has no state - Severity: Major
- **Affected scope:** §5.5 third row, §7.3 last sentence.
- **Finding and rationale:** `MemStore` holds no notifier (`store_mem.go:43-87`), and no store method sends WhatsApp today. Notices come from components that hold a `*notifier` (`qajobs.go:175`, `main.go:414-475`). No field on `Defect` or the task records the day of the last notice, so "at most one per defect per UTC day" cannot be enforced or survive a restart.
- **Recommendation:** Have the runner send the notice when `PlanUnitDefect` returns a newly planned held card, for example through a `Result` flag or the returned triage-card id. Then the once-per-day rule follows from "new or reopened only", which the daily cap already bounds. Otherwise, name the persisted field.

### The held-card notice text names the repository - Severity: Major
- **Affected scope:** §5.5 table, third `unit:failure` row; §5.5 first sentence.
- **Finding and rationale:** `Defect.Ref()` returns `CaseID` when `DefectID` is empty (`defects.go:126-131`). For a unit defect that is `unit:<owner>/<repo>@<h>` (§7.1). So `…waits for an auto-fix admin (<ref>)` names the repository, which contradicts the design's own "Every line names no repository (D-088; `notify.go:95`)".
- **Recommendation:** Use a repository-free reference in notices, such as `<h>` or the card id only. TC-861 should assert the absence of `owner/repo`.

### The poll order overwrites the context of an open, in-flight defect - Severity: Major
- **Affected scope:** §6 "Poll order" step 3, against §7.2 dedup ("plans nothing").
- **Finding and rationale:** On every failure answer the runner builds a context and calls `SetDefectContext`, which overwrites unconditionally (`defects.go:612-623`). That includes a dedup of an open defect whose triage verdict or fixer turn was pinned to an earlier context. The QA path guards this with `Context.Built` (`defecttriage.go:428`). The runner also spends up to 7 GitHub reads per failure before it knows whether `PlanUnitDefect` refused or deduplicated.
- **Recommendation:** Call `PlanUnitDefect` first. Build and store the context only when it planned a new or reopened record (a `Result` marker), or when `!Context.Built`.

### The "unchecked" state needs a row head that is not stored - Severity: Major
- **Affected scope:** §8.2 last sentences, §12 `prview.go` and schema rows.
- **Finding and rationale:** `PullRequest` has no head-SHA field (`pullrequests.go:36-68`). `AdvancePullRequest` drops `PRFields.HeadSHA` (`pullrequests.go:214-252`). "The Pull requests page shows *unchecked* when the row's current head differs from the note's" therefore cannot be computed at render time.
- **Recommendation:** Add `PullRequest.headSha` (schema 21, ArcadeDB read and write, `AdvancePullRequest`) to §8.2 and §12. Alternatively, drop the *unchecked* display.

### §0 claims a single departure; there is a second - Severity: Nit
- **Affected scope:** §0 "One departure…", §4.2.
- **Finding and rationale:** The sibling design's §5 asks for "a `unit:qa` card with its own family". This design keeps `unit:qa` in the `unit` family (`labelFamily`, `prflow.go:515-520`) and adds a bespoke refusal.
- **Recommendation:** Name it as a second departure, with its reason.

### The glossary overstates the hold - Severity: Nit
- **Affected scope:** §1 "Held card".
- **Finding and rationale:** It says "only an auto-fix admin moves it out", but §7.3 also lists the machine cancel through `PassUnitDefects` (and, per the finding above, `CloseDefect`).
- **Recommendation:** Say "…or a later pass cancels it".

### BR-54 amendment says "unstarted cards" - Severity: Nit
- **Affected scope:** §10, BR-54 amended.
- **Finding and rationale:** `cancelDefectCardsLocked` skips only DONE, FAILED, CANCELED, DISPATCHED and RUNNING (`defects.go:371-374`). It also cancels REVIEW, AWAITING_INPUT and BLOCKED cards, which are not "unstarted".
- **Recommendation:** Say "cancels the cards of those defects that hold no live turn".

### "Fixes the regression" describes a regression that does not exist today - Severity: Nit
- **Affected scope:** §5.4 Adoption.
- **Finding and rationale:** The timeout is runner-wide today (`qajobs.go:178,465`), so adopted e2e jobs do not time out at once. The risk only appears once the per-job `t.timeout` is introduced.
- **Recommendation:** Say "so the new per-job timeout is never zero on an adopted job".

### The change list hedges a file and misses one - Severity: Nit
- **Affected scope:** §12.
- **Finding and rationale:**
  - "`store_arcade_defects.go` (or where `writeDefect` lives)" is a hedge. The file exists and writes `baseOverride` (`store_arcade_defects.go:54`).
  - `contacts.go:93`'s error text enumerates the notice kinds and needs updating.
- **Recommendation:** Name the file without the hedge, and add the `contacts.go:93` text change.

### Dedup of `build-failed` keeps a stale excerpt - Severity: Nit
- **Affected scope:** §7.2 dedup.
- **Finding and rationale:** Only `LastSeen`, `RunRef`, `UnitSHA` and `UnitMergedAt` are refreshed. A new build failure under the single `@build` key shows the admin the old build output.
- **Recommendation:** Refresh `Excerpt` on dedup, or say that it is not refreshed.

### Flaky tests and other risks are not discussed - Severity: Nit
- **Affected scope:** whole design (no risks section).
- **Finding and rationale:** A flaky test opens a defect, a later pass closes it, and a recurrence reopens it as `Regression` (never auto-fixed). This churn, bounded only by the cap of 5 a day, is the most likely failure mode in practice and is not named.
- **Recommendation:** Add a short risks list. Include flakiness, the BR-38 stall and the cap interaction.

### Replay across a switch flip can plan two cards for one merge - Severity: Nit
- **Affected scope:** §4.2 switch transitions.
- **Finding and rationale:** The `:unit-test` and `:unit-qa` keys differ (`prflow.go:241`). A Brain Town replay of a merge after `UnitQA` is turned on plans a `unit:qa` card beside an existing `unit:test` card.
- **Recommendation:** Check both keys, or state that this is accepted.

### Wording - Severity: Nit
- **Affected scope:** §8.2 and §5.1.
- **Finding and rationale:**
  - §8.2: "`ghPRState` gains `ChangedFiles int`, `Author` (already `Author`)…" contradicts itself.
  - §5.1: the in-memory `qaUnitCommitTries` resets on restart, and 10 tries are about 2.5 minutes at a 15 s tick (`qajobs.go:193`).
- **Recommendation:** Drop `Author` from the list. State the reset and the effective time budget.

Counts: B=2 M=11 N=9
