The rev 4 design is not ready to implement. I found 2 Blockers, 9 Majors and 5 Nits. The two Blockers:

1. Its decision, rule and test-case numbers are already used on `main`.
2. Its live-turn rule would also free the triager and fixer cards, which do run model turns.

# Readiness review: `docs/designs/release-b4-unit-tests.md` (rev 4)

Reviewed in `/home/ardi/devel/projects/ardihikaru/my-projects/gaos-unit-design`. I checked the anchors against that worktree. I checked the registers against both that worktree and the main checkout at `/home/ardi/devel/projects/ardihikaru/my-projects/gaos`, which is on `main` at `ba7026a`. I did not open anything under `.claude/`.

### The design's identifiers are already taken on `main` (D-104, BR-63, TC-839…TC-849), and its base is stale - Severity: Blocker
- Affected scope: §2 row 7, §9 (lines 459–467), §10 (lines 473–480), §12 register rows, §17's closing line
- Finding: The design worktree has no D-104, and its test plan ends at TC-838. On `main`, PR #63 (TaskMgr) already uses these numbers:
  - **D-104:** `pustaka/docs/product/decision-log.html:2154` is the TaskMgr third-party decision.
  - **BR-63:** `brd.html:1309` is "A third-party platform is bound to one product at most…".
  - **TC-839…TC-849:** `qa-test-plan.html:930-940`, and section `#go-taskmgr` at `:4666`, are TaskMgr cases.

  So 11 of this design's 28 test cases, its decision number and its new BR all collide. The same merge also changed files the design anchors into. On `main`, `notify.go:40` is now TaskMgr routing, and `loginFor`'s `e2e:approval` branch has moved to `:86`. `contacts.go:67-77` now builds the refusal text from `notifyKindList()` and gained D-104 kinds. As written, §5.5's anchors and the "`ValidContact` text at `contacts.go:93`" change point at code that no longer exists in that form.
- Recommendation: Rebase the design onto `main` and take the next free numbers there (D-105, BR-64, TC-850 onward). Carry them through §2, §4.2, §8.3, §9, §10, §12 and the TC map. Re-anchor every `notify.go` and `contacts.go` claim against `main`. Add a step that makes each phase re-check the next-free values before writing the registers.

### The BR-38 lease change would also exempt the triager and fixer cards, which run model turns - Severity: Blocker
- Affected scope: §4.4 (line 160), §2 last paragraph, §10 BR-38 amendment, TC-847
- Finding: §4.4 says `liveIn` and `liveTurnLocked` "ignore a task whose labels `labelsAllowDispatch` refuses". That function also refuses `defect:triage`, `defect:easy`, `defect:medium` and `defect:complex` (`labels.go:182-183`, confirmed by `labels_test.go:162-169`). Those cards are dispatched as real model turns by the auto-fix loop: `dispatch(…, MoveAutoFix, claim)` in `board.go:479`, which checks `liveTurnLocked` at `board.go:500` and `:551`. `readyDefectCards` (`defecttriage.go:789-792`) says so.

  Under §4.4, a RUNNING triager or fixer would hold no lease. A second defect turn, or any scheduler turn, could then start in the same product. That breaks BR-38 for exactly the cards this design creates. The text's "(`e2e:*`, `unit:qa`)" lists only part of what the predicate covers.
- Recommendation: Use a dedicated predicate, `qaToolDriven(labels)`, true for exactly `e2e:automation`, `e2e:run` and `unit:qa`. Use it in both `liveIn` and `liveTurnLocked`, and name it in §12 (`labels.go`). Extend TC-847 to assert two things: a RUNNING `defect:triage` card still blocks a dispatch, and a RUNNING `unit:qa` card does not.

### The GitHub reads the runner and the poller need are not on the interfaces they use - Severity: Major
- Affected scope: §1 "Effective base", §5.1 (line 166), §8.2 check 4 (line 424), §12 `github.go` row
- Finding: The runner's `src` is a `prSource` (`qajobs.go:173`, `prflow.go:394-397`). That interface has only `pullRequest` and `pullFiles`, but §5.1 needs `repoDefault` (`github.go:414`, on `*ghClient`). §8.2 adds "a new `pullCommits` on `prSource`", but the poller does not use `prSource`: `fixPRPoller.gh` is a concrete `*ghClient` (`fixprpoll.go:110`). Three things are left for the implementer to decide:
  - whether to widen `prSource`, which forces new methods on `fakePRSource` (`prflow_test.go:203`), `countingPR` (`api_tasks_test.go:41`), `bundleGH` and `fakeReader`, or add a separate runner-only interface;
  - how TC-864 fakes `pullCommits` behind a concrete `*ghClient`;
  - whether to reuse the existing `pullCommitAuthors` (`github.go:246`), which reads authors only, single page, `per_page=100`.
- Recommendation: Name one interface per consumer. For example, `qaJobRunner` gets a `unitSource interface { pullRequest; pullFiles; repoDefault }` wired from `ghc` in `main.go`, and `pullCommits(ctx, o, r, n) ([]ghCommit, error)` (author and committer login, paged to 250) goes on `*ghClient` only. List every test fake that must change in §12.

### A rate-limited 403 cannot be told apart from a permanent 403 - Severity: Major
- Affected scope: §5.1 outcome table, row 3 (line 172)
- Finding: The design retries a "403 rate-limit answer (`X-RateLimit-Remaining: 0` or `Retry-After`)" and blocks on other 403s. `ghClient.do` detects the rate limit internally (`github.go:375`), but after its retries it returns `&ghError{Status: 403}` with no flag and `Retry` unset (`github.go:371-377`). `lookupTransient` treats 403 as permanent (`prflow.go:430-434`). The caller has nothing to branch on.
- Recommendation: Add `Limited bool` to `ghError`, set at `github.go:375`, and a `unitReadTransient(err)` that returns true for it. Put the change in the §12 `github.go` row and cover it in TC-853.

### Planning a `unit:qa` card: the existing PR cap and the `Skipped` field are undecided - Severity: Major
- Affected scope: §4.2 "Own open cap" (line 128), §14 Risks
- Finding: `planPullRequestCardsLocked` refuses the whole event when `len(fresh) > 0 && openPRCardsLocked >= prMaxOpen()` (`prflow.go:298-300`). The design removes `unit:qa` cards from the *count*. It does not say whether a fresh `unit:qa` spec is still subject to this *check*. If it is, a product at its PR-review cap refuses every merge's unit card, which contradicts §14's "never toward `GAOS_PR_MAX_OPEN`".

  Separately, `Result.Skipped = "unit-qa-cap"` and `gateUnassigned`'s `skipped` (`prflow.go:294`) write the same string field, so which value wins is undecided. "The other specs of the event plan as usual" is also empty in practice: `pr.merged` has only the unit spec (`prflow.go:88-91`).
- Recommendation: State the order:
  1. `gateUnassigned`
  2. the unit-QA cap, applied to the `unit:qa` spec only
  3. the PR cap, applied to the remaining non-`unit:qa` specs only

  State how two skip reasons combine (for example, joined with `;`). Add a TC-843 case: PR cap full, unit cap free, and the unit card is planned.

### The API path skips both the "one card per merge" check and the unit cap - Severity: Major
- Affected scope: §4.2 "One card per merge across the switch" (line 126), "Own open cap", the API table (lines 132–139)
- Finding: The two-key lookup and `openUnitQACardsLocked` are specified only inside `planPullRequestCardsLocked`. `PlanAPICard` takes its own path: `apiLookupLocked` looks up a single key (`api_cards.go:247-248`), then the client caps, then `planCardsLocked` (`api_cards.go:320-377`). Two consequences:
  - After the switch is turned on, an API `unit-test` request for a merge that already has a `:unit-test` card misses and plans a second card under `:unit-qa`.
  - API-planned `unit:qa` cards ignore `GAOS_UNIT_QA_OPEN`.
- Recommendation: Specify that `apiLookupLocked` also tries the other key of the pair for a mapped `unit-test`, and that `PlanAPICard` applies `openUnitQACardsLocked` with a stated refusal code (for example 409 `unit-qa-cap`). Add both to TC-845.

### The unit result is acted on once only in memory; a restart re-acts and can reopen a defect an admin closed - Severity: Major
- Affected scope: §6 (lines 274, 288–290), §5.4 adoption (line 242), §12 `qajobs.go` row
- Finding: `qaTracked.unitDone` lives in memory. Adoption after a restart builds a fresh `qaTracked` (`qajobs.go:355-361`, extended by §5.4), and the reason grammar (§5.4) has no field for "already acted".

  A `unit-create` job can wait up to 24 h for approval while still reporting `unit`. Suppose an admin cancels the held card in that window, which closes the defect (§7.3), and then GAOS restarts. The next poll calls `PlanUnitDefect` again. The record is now final, so §7.4 reopens it as a regression, plans a new held card and uses up a slot of the daily cap.

  There are two related gaps:
  - The DONE text ("`<total> unit test(s) failed; defect <h8>`") has to be produced at job end, possibly many polls later, but no `qaTracked` field holds the total, the h8 or the refusal reason.
  - The active-timeout branch (`qajobs.go:465-468`, `r.fail(…"qa-job-timeout")`) is neither an "approval timeout" nor a "job failure", so whether it keeps the reported result is undecided.
- Recommendation:
  - Add `unit:<h8|pass|refused>` to the RUNNING and AWAITING_INPUT reason grammar, written in the same poll as the store call. Make adoption read it and set `unitDone`.
  - Name the `qaTracked` fields (`unitText string`) that hold the final text.
  - Say explicitly that all three ends (approval timeout, active timeout, job failure) after `unitDone` produce the stored DONE text.
  - Add the restart-after-close case to TC-857.

### The admin's cancel writes the email address into a persisted event - Severity: Major
- Affected scope: §7.3 (line 351)
- Finding: The design sets `CardMove.Actor = ss.Email` and calls `setDefectStateLocked(d, DefectStateClosed, "unit-release", mv.Actor)`. `setDefectStateLocked` puts `actor` into both `EventPayload.Actor` and the event text (`defects.go:353-357`). `CloseDefect` passes `actorAlias(actor)` (`defects.go:607`), and `actorAlias` exists so that "the address itself never reaches an event" (`prflow.go:522-523`). The move event in the same paragraph uses `actorAlias(mv.Actor)`, so the two calls in one paragraph disagree.
- Recommendation: Pass `actorAlias(mv.Actor)` to `setDefectStateLocked`. Make TC-859 assert that no event text or payload contains `@`.

### Truncating `Passed` to 2000 names has no stated order, so a defect can never close - Severity: Major
- Affected scope: §6 (lines 268, 276), §7.4 "Passed"
- Finding: GAOS keeps "at most 2000" `Passed` names, but only `Failing` has a stated sort order. In a suite of more than 2000 tests, a previously failing test can land beyond the cut. `PassUnitDefects` then never sees it, and the defect stays open forever. Which 2000 names are kept differs between implementations.
- Recommendation: Have `PassUnitDefects` test membership against the full `Passed` list before any cap (the body is already bounded at 2 MiB, `qaclient.go:40`). Alternatively, state that truncation keeps the defects' `UnitTests` names first. Add a case of more than 2000 names to TC-851 or TC-858.

### Existing test files that must change are missing from the change list - Severity: Major
- Affected scope: §12 (lines 503–525)
- Finding: §12 lists only `qajobs_test.go` and the new test files. With a write allowlist, these existing tests would not compile or would fail:
  - **`CloseDefect` callers:** the new `admin bool` argument breaks `defects_test.go:461,464,479`, `retarget_test.go:482` and `store_conformance_test.go:962`. The last one is the read-only conformance table, which also needs rows for `SetUnitQA`, `PlanUnitDefect`, `PassUnitDefects` and `NoteQAUnitRow`.
  - **`prSource` fakes:** if the interface is widened (see the GitHub-reads finding), the fakes in `prflow_test.go`, `api_tasks_test.go`, `bundles_test.go` and `reviewseverity_test.go` must change.
  - **`prflow_test.go:250-261`:** `TestTheSchedulerNeverDispatchesAManualOrQAToolCard` should gain `LabelUnitQA`.
- Recommendation: List each of these files in §12 with its exact change.

### A transient GitHub error on the final poll loses the `qa-unit` row and sends no notice - Severity: Major
- Affected scope: §8.1 (line 415)
- Finding: "A transient GitHub error leaves the job unrecorded and retries on the next poll." But `poll` calls `recordQARow` and then, on a final status, deletes the job in the same pass (`qajobs.go:425-434`). If `pr_url` first appears with `succeeded`, which is the normal order for a create, a transient read has no next poll. The draft pull request then exists on GitHub with no row, no §8.2 checks and no `unit:files` notice.
- Recommendation: For a unit job, keep it tracked past `final()` until the row is recorded or refused, with a bounded number of tries (for example 10) followed by the `unit:files` notice "could not be read". Add the case to TC-863.

### "Lists all seven kinds" is the wrong count - Severity: Nit
- Affected scope: §5.5 (line 246), §12 `notify.go`/`contacts.go` row
- Finding: The worktree's `notifyKinds` already has 7 entries (`contacts.go:67-69`), so adding `unit:approval` and `unit:files` makes 9. On `main` the text is generated by `notifyKindList()`, so no hand-written text exists to change.
- Recommendation: Drop the count. After the rebase, say the text follows `notifyKindList()`.

### `buildAndStoreDefectContext`'s "first statement" cannot read `d` - Severity: Nit
- Affected scope: §7.5 (line 378)
- Finding: The function takes `key`, and `d` is loaded by `st.DefectByKey` (`defecttriage.go:424`).
- Recommendation: Say "immediately after the `DefectByKey` read, before the `Built` check".

### §8.2 does not say what happens past 250 commits or how to page - Severity: Nit
- Affected scope: §8.2 check 4 (line 424)
- Finding: Pagination (`per_page`, how many pages) and the verdict for a pull request with more than 250 commits are both unstated.
- Recommendation: State `per_page=100`, at most 3 pages, and a verdict `too many commits` when there are more than 250. Add it to the verdict table and to TC-864.

### TC-846 in phase 2 needs a `checked@…` note that only phase 6 can write - Severity: Nit
- Affected scope: §9 phase 2 (line 460), and the rule "each phase uses only its own and earlier phases' code" (line 455)
- Finding: `NoteQAUnitRow` arrives in phase 6, so the phase-2 test would have to seed the MemStore map directly, which the plan does not say.
- Recommendation: Either move `NoteQAUnitRow` to phase 1, or state that TC-846 seeds `QANote` and `HeadSHA` through the fixture.

### When the `unit-qa-no-dispatch` 422 applies is ambiguous - Severity: Nit
- Affected scope: §4.2 API table, `apiTaskCreate` row (line 139)
- Finding: "Before planning" leaves open whether a `dispatch:true` request that finds an existing card (APIOwn or APIAdopt) is also refused.
- Recommendation: Say "before the lookup, for every mapped `unit-test` request with `dispatch:true`".

Sections that hold up: the §5.2 mode table, the §5.3 attestation predicate, the §7.1 record table, §7.3's list of every path out of BLOCKED, the §7.5 table of `BaseOverride` sites, and the §9 TC arithmetic (4+5+5+6+4+3+1 = 28). The verification commands, the §12 paths and the 19 → 20 label count all check out.

Counts: B=2 M=9 N=5
