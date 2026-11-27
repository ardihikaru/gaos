# Goldfish critic report: release-b4-unit-tests rev 12 (§0–§14)

**Thesis under attack.** Rev 12 says GAOS can trust one named `github-actions` check run on the merge commit, wait for it with a cheap background read, and turn red into a held defect and green into a close. Three of those premises don't hold against the code:

- **The trust boundary.** A check run named `unit-tests` from the `github-actions` app can be created by any workflow token. It does not prove that the §11 workflow ran.
- **The poller's cadence and rate-limit behaviour.** The tick is 30 s, not 2 min, and `do` never waits for the rate-limit reset.
- **Ordering of results across merges.** A newer pass recorded before any defect exists leaves no trace, so an older failure can still file a stale defect.

The design's identifiers and counts are sound. D-105, BR-64, TC-851 and schema 21 are the next free ids. 713 → 729 Go tests and 688 dashboard tests check out exactly. There are 17 `checkFix` calls in `fixpr_test.go`. The `UnitWatch` fields, the defect fields and the shared defect cap are under-specified in ways that two engineers would implement differently.

---

### A forged check run passes as trusted - Severity: Major
- Affected scope: §0 decision 2; §5.1 step 1 and step 4; §13 row "Read commit statuses too"
- Finding and rationale: The design rejects commit statuses because "any token with `repo:status` can post one", and trusts any check run whose `app.slug == "github-actions"` and whose name matches. That trust has the same weakness. A check run created with a workflow's `GITHUB_TOKEN` (`POST /repos/{o}/{r}/check-runs` with `checks: write`) is attributed to the `github-actions` app, and it can carry any name, any `head_sha` and any conclusion. Repositories created before 2023 often still grant read-write `GITHUB_TOKEN` permissions by default, which include `checks: write`. So any of these can post `unit-tests: success` or `failure` on a merge commit:
  - a same-repository `pull_request` workflow run by a collaborator;
  - a third-party action in any workflow;
  - a `workflow_dispatch` run.

  Step 4 picks the run with the latest `completed_at`, so the forgery wins. The impact:
  - a forged pass closes released defects and marks held ones;
  - a forged failure files defects whose test names and paths the attacker chose. Those paths become `UnitFiles`, decide which files go into the context, and set the fix guard.

  None of this is listed in §14.
- Recommendation: Bind the check to the workflow, not only the app. Read `check_suite.id` from the run, then `GET /repos/{o}/{r}/actions/runs?head_sha=<sha>&event=push`. Require the matching run to have `path == ".github/workflows/unit-tests.yml"` (make the path a setting), `event == "push"` and `head_branch == Base`. Name the remaining risk in D-105: a writer of that workflow file can still forge results.

### The poller tick is 30 s, not 2 minutes; the cost and rate-limit claims are wrong - Severity: Major
- Affected scope: §3 step 3 ("Every poller pass (2 minutes)"); §5.1 "Cost"; §5.1 "Waiting"; §14 "Rate limits"
- Finding and rationale: `fixPRPollTick = 30 * time.Second` (`dashboard/fixprpoll.go:23`). The ceiling of "40 reads per 2-minute pass" is really up to 40 reads every 30 s, about 4,800 an hour. That is most of a 5,000/h token budget, which this token shares with the review reads, the fix-PR and row polls, and the bundle pass.

  The design also says "a rate limit waits for the reset (`do` already sleeps on it, `github.go:375-391`)". That is false. `do` retries at most twice and sleeps at most 10 s each time (`github.go:379-385`). It reads `Retry-After` only and never `X-RateLimit-Reset`. A primary-limit exhaustion therefore costs about 20 s of blocking per call inside `Pass`, and then the call fails. The cost line also leaves out the reads made by `buildUnitContext` (up to `defectMaxFetch` per new defect) and the `repoDefault` reads.
- Recommendation: Restate §5.1 and §14 against the 30 s tick. Either cap unit reads per minute rather than per pass, or run `unitPass` only every fourth tick. Fix the rate-limit sentence. Add the context-build and `repoDefault` reads to the cost line.

### An older failure can file a stale defect after a newer pass - Severity: Major
- Affected scope: §7.2 "out of order"; §7.4 "Passed"
- Finding and rationale: The out-of-order rule refuses an older failure only when a defect exists that already has `UnitPassedAt > in.MergedAt`. Take this sequence:
  1. Merge A, then merge B, on the same base.
  2. B's check completes green first (A's run was queued longer). `passUnitDefectsLocked` finds no defect and records nothing.
  3. A's check completes red. It files a new held defect for a failure that B's pass already disproved.

  The persisted watches hold exactly the evidence needed, but the gate never reads them.
- Recommendation: In `FinishUnitWatch`'s failure branch, also refuse as `older` when any `done` watch with outcome `passed` has the same `ProductID`, `Repo` and `Base` and `MergedAt > in.MergedAt`. Add this sequence to TC-858.

### Overlapping failing sets create parallel defects for one breakage - Severity: Major
- Affected scope: §7.1 `h`; §7.2 dedup; §14 "Coarse results"; BR-64 text
- Finding and rationale: The defect identity hashes the kept set: the first 10 `sort -u` lines (§11). Dedup and the out-of-order rule match only an identical key, or a passed defect that covers every pair. Two cases follow:
  - Merges that progressively fix or add failures, such as A,B → A,B,C, or more than 10 failures where one is fixed, each file a new defect with its own held card. The earlier defects stay open until a fully green run.
  - Two released fixers can then work the same base, with the same protected files, at the same time.

  BR-64's "at most one open unit defect per repository, base and failing set" is true by definition, but it doesn't prevent this outcome.
- Recommendation: Decide the intended rule and write it down. One option: a new failing set on a repository and base that already has an open, non-final unit defect updates that defect (merging pairs up to the cap) instead of filing a new one. Another: allow at most one open unit defect per repository and base. Amend BR-64 to match and add a TC.

### "The package's non-test files" cannot be read with the existing code source - Severity: Major
- Affected scope: §7.5 "Context"; §12 change list
- Finding and rationale: `buildUnitContext` is to load "the failing test files first, then their package's non-test files". `codeSource` has only `defaultHead`, `searchCode` and `fileAt` (`dashboard/defecttriage.go:205-211`), and `buildReviewContext` needs explicit paths (`reviewdefect.go:319`). Nothing lists a directory at a commit. The change list adds no tree or contents-listing reader to `github.go` or `codeSource`. As written, the context can only hold the test files, and `checkFix` refuses any edit to a file outside the packet (`autofixcheck.go:329-331`). So the fixer can edit nothing except the protected test files, which the guard also refuses.
- Recommendation: Add `dirAt(ctx, owner, repo, dir, sha)` (`GET /repos/{o}/{r}/contents/{dir}?ref=<sha>`) to `codeSource` and to §12. State its read budget and count it in §5.1's cost.

### The fix guard can be bypassed with a new test file - Severity: Major
- Affected scope: §7.5 "The fix may not weaken the failing tests"
- Finding and rationale: Protected paths and edited `TestGlobs` files are refused, but "new test files stay allowed". In Go, a new `_test.go` in the same package can still neutralise the failing tests:
  - a `TestMain(m)` that calls `os.Exit(0)` without running `m.Run()`;
  - an `init()` that changes shared state;
  - a build-tag trick.

  A later green check then pass-closes the defect, the same path that §14 calls a residual only for "deleted or skipped" tests. The heading promises a guarantee the mechanism doesn't give.
- Recommendation: With `Unit`, refuse a new test file in any directory that holds a protected file, or refuse any new file containing `func TestMain(` or `func init(`. Otherwise reword the heading and add this bypass to D-105's residuals.

### `UnitNoticeSent` has no re-send path, and the daily `no-ci` limiter has no store - Severity: Major
- Affected scope: §5.3; §7.1 `UnitNoticeSent`; §7.4 reopen
- Finding and rationale: The crash-safety rule is that `UnitNoticeSent` "records it so a crash between the store write and the send re-sends on the next pass". But:
  - `unitPass` iterates only pending watches, and the watch is already `done`.
  - No store method sets the flag after a send. None is named in §5 or §12.
  - `reopenUnitDefectLocked` doesn't reset it, so a reopened defect never notifies.
  - The "once" mark notice and the "at most once per product per UTC day" `no-ci` notice have no persisted state, so they repeat after a restart (BR-01).
- Recommendation: Add `MarkUnitNoticeSent(ctx, key)` to the store and §12. Add a scan in `unitPass` of non-final unit defects with `!UnitNoticeSent`. Reset the flag on reopen and on the passed mark. Store the last `no-ci` notice day on the product's `UnitChecks` state or on the watch.

### `UnitWatch` has no finish time, so the metric, the retention and the page cannot be computed - Severity: Major
- Affected scope: §0 "Success… Metrics"; §4.1 "Page"; §4.2 `UnitWatch` and its retention
- Finding and rationale: The metric is "watch ended `passed` or `failed` within 60 minutes of `merged_at`". The retention prunes "30 days after their last read". The page shows "the last watch's outcome and time". `UnitWatch` carries only `MergedAt`, `FirstSeen`, `NextAt` and `Tries`. There is no `DoneAt` and no `LastRead`, so the metric can't be read from the store, as the design claims it can.
- Recommendation: Add `DoneAt int64` (set by `FinishUnitWatch`) and use it for the metric, the retention and the page. Name the retention store method (for example `PruneUnitWatches`) in §4.2 and §12, and add it to the read-only conformance table.

### The `MergedAt` JSON type would break every pull-request read - Severity: Major
- Affected scope: §3 step 1; §12 `dashboard/github.go`
- Finding and rationale: GitHub returns `merged_at` as an RFC 3339 string or `null`. The design leaves the field's type open, and every other `MergedAt` in the design is `int64`. An `int64` field tagged `json:"merged_at"` makes `pullRequest()` fail to decode on every merged pull request. Because `lookupTransient` treats a non-`ghError` as transient (`prflow.go:430-436`), every `pr.opened` would then return `flowRetry` forever (`prflow.go:466-468`). A fake built from the struct would pass the tests while production breaks.
- Recommendation: Specify `MergedAt *time.Time \`json:"merged_at"\`` and `MergeCommitSHA string \`json:"merge_commit_sha"\`` on `ghPR`, converted to Unix seconds when planting. Make TC-853 decode a recorded GitHub JSON body.

### A transient re-read error silently drops the merge's test - Severity: Major
- Affected scope: §4.2 second paragraph; §4.2 "The agent card"
- Finding and rationale: When the feature is on, the agent card is dropped "whatever the base". A GitHub hiccup on the merged re-read then plants nothing and plans nothing. The only trace is a log line and an in-memory counter whose store isn't specified. The merge is never tested and never ends `no-ci`, so it is also missing from the success metric's denominator. The flow already has `flowRetry` for exactly this class (`prflow.go:466-468`), and planning is idempotent.
- Recommendation: On a transient re-read error for a watched repository, return `flowRetry`. Keep `unit-watch-skipped` for permanent errors only, and persist the counter if the page shows it.

### The OQ-40 amendment says CI runs on fix branches; §11's workflow does not - Severity: Major
- Affected scope: §10 OQ-40; §14 "OQ-40"; §8
- Finding and rationale: The amendment says a unit fix's tests on a `gaos/fix-*` branch are run by "CI". The §11 workflow triggers only on `push` to the unit bases, and the example's other workflow is "`pull_request` behind a paths filter" (§0 decision 3). So a unit fix's draft pull request may get no test run before a person merges it, and the first evidence comes only after the merge (§8 admits this). The register text would record a guard that doesn't exist.
- Recommendation: Either add a `pull_request` trigger to §11 (read-only, no secrets, fork-safe) so the draft fix gets a check, or reword OQ-40 and §14 to say the fix's tests first run after the merge.

### Stale or wrong anchors and claims - Severity: Nit
- Affected scope: §3 step 1; §4.2; §4.4; §7.3 table
- Finding and rationale:
  - `ghPR` already has `Base.Ref` (`github.go:66-67`). §3 says it "gains" it, and §12 omits it.
  - `p.retain` is at `fixprpoll.go:225-244`, not `:169-176`.
  - `prflow.go:600-606` is the `resolveFrom` table; `resolveCardLocked` is at `prflow.go:638`.
  - `sha40Re` duplicates the existing `shaRe` (`staging.go:186`).
- Recommendation: Fix the anchors, and reuse `shaRe` or say why a 40-only regex is needed.

### The refusal gate omits `AssignedSet` - Severity: Nit
- Affected scope: §7.2 steps 2–3
- Finding and rationale: The sibling gate checks `!m.AssignedSet || !roleAssigned(...)` (`reviewdefect.go:100`). §7.2 names only `roleAssigned`.
- Recommendation: State the `AssignedSet` behaviour explicitly.

### Unit defects share the QA fix and triage caps - Severity: Nit
- Affected scope: §7.2; §14 "Flaky tests"
- Finding and rationale: `autoClaimLocked` gives only review fixes their own cap (`autofix.go:187-199`). Unit triage and fix cards therefore consume `TriageCap` and `FixCap` alongside QA defects. The design doesn't say whether that is intended.
- Recommendation: State it, or give unit cards their own cap.

### Workflow script details - Severity: Nit
- Affected scope: §11 script
- Finding and rationale:
  - `grep -rl` recurses into sub-packages and can name the wrong file. That wrong file then becomes `UnitFiles` and the context.
  - `${file:-$dir}` can put a directory into `UnitFiles`, and the design doesn't say whether protection matches exactly or by prefix.
  - Under GitHub's `bash -eo pipefail`, a failing `grep` inside `$(…)`, or `printf` hitting SIGPIPE into `head`, aborts the script before the `unit-tests-total` notice.
  - `Example*` and `Fuzz*` failures fail the `^Test` title regex, so they are mis-filed as `~build`, which turns the fix guard off.
  - Each failing subtest also fails its parent, which double-counts `Total`.
- Recommendation: Use a non-recursive `grep -l … "$dir"/*_test.go || true` and wrap the reporting block in `set +e`. Define how `Protected` matches. Widen the title regex or document the misclassification. Count only leaf tests.

### The single-card move response is not trimmed - Severity: Nit
- Affected scope: §7.3 "Board"
- Finding and rationale: `handleMoveCard` answers the JSON client with `s.store.BoardCard(...)` (`handlers_board.go:93`), which `trimUnitMoves` doesn't cover. This is cosmetic only, because the store guard still refuses the move.
- Recommendation: Add that call site to §7.3 and §12.

### "An admin closes it" has no surface - Severity: Nit
- Affected scope: §3 step 7; §7.4
- Finding and rationale: The only close is the route at `main.go:595`, and no page renders a close action today (no other `/close` reference in non-test code). A unit `CaseID` contains `/` and `~`, so a hand-built URL must escape it.
- Recommendation: Say the admin's cancel of the held card is the close path, or add a close button that escapes the ref.

### Watch writes and BR-02 - Severity: Nit
- Affected scope: §4.2; §5.1
- Finding and rationale: `PlantUnitWatch`, `DelayUnitWatch` and the outcomes `refused:*`, `no-ci`, `inconclusive` and `dedup` change stored state and emit no event. BR-02 (brd.html:1227) says every change emits an event. There is precedent for silent cache writes (`ExpireExcerpts`, `fixprpoll.go:488-489`), but the design doesn't claim it.
- Recommendation: State that watch writes are silent bookkeeping like `ExpireExcerpts`, or map the outcomes onto an existing event.

### Smaller gaps in enabling, reading and closing - Severity: Nit
- Affected scope: §4.1 route; §5.1; §7.4
- Finding and rationale:
  - Enabling doesn't check that the Brain Town reader is on. With no `pr.merged` events there are no watches, so the `no-ci` warning never appears either.
  - `unitPass` doesn't re-check, when it reads, that the product still owns the repository, as `pollOne` does (`fixprpoll.go:251-253`).
  - A pass-close leaves the fix's draft pull request open, and nothing says so.
  - A crash between `FinishUnitWatch` and `SetDefectContext` leaves no context and has no retry.
  - An admin cannot mute a known-failing test except by switching the whole feature off.
- Recommendation: Add a refusal when the reader is off and the read-time ownership check. Add one line each on the orphaned pull request and the missing context. Consider a "won't fix" close that suppresses reopening.

### Delegation anchor and UI reference - Severity: Nit
- Affected scope: §0 "Rev 12 decision"; §4.1 "Page"
- Finding and rationale:
  - The C′ mandate ("work among agents to decide the final decision…") is quoted only in the author-edited summary at `reviews/release-b4-unit-tests-delegation.md:28-29`, not in the verbatim block. §0 cites `:14`, which is the first message.
  - The verbatim instruction names `https://emilkowal.ski/skill`, but §4.1 cites `github.com/emilkowalski/skills` `emil-design-eng`.
- Recommendation: Record the second message verbatim with its own anchor, and reconcile the two URLs.

---

Sections that hold up: §9's counts and TC arithmetic, the §10 identifier allocation, the §7.3 store guard against `legalMove` (`board.go:213-242`), and the path table out of BLOCKED.

Counts: B=0 M=12 N=9
