I reviewed §0–§14 of `release-b4-unit-tests.md` (rev 12), checking its claims against the code at `156214c`. The main issues are a wrong poller cadence, a merge-time field type that would break an existing read, undefined store methods and schema, and a workflow template that aborts on a common case.

**Confirmed correct:**
- The next-free identifiers are right: D-105, BR-64, TC-851 and schema 21.
- The test arithmetic adds up: 2+2+5+3+3+1 = 16; there are 688 `func Test` in `dashboard/` today, so 688 → 704.
- `checkFix` really has 17 test call sites in `fixpr_test.go`.
- Every existing file in the change list exists, and the new ones are marked new.
- Most anchors match the code.

### The poller runs every 30 seconds, not every 2 minutes, so the cost bound is wrong - Severity: Major
- Affected scope: §3 step 3, §5.1 "Cost", §14 "Rate limits"
- Finding: The design says "Every poller pass (2 minutes)" and "40 GitHub reads per 2-minute pass". In the code, `fixPRPollTick = 30 * time.Second` (`dashboard/fixprpoll.go:23`) and `Run` calls `Pass` on every tick (`fixprpoll.go:154-165`). With 10 watches per pass, four passes fit in 2 minutes. That allows 40 watches per 2 minutes and up to 160 reads, four times the stated bound. The design also says a rate limit "waits for the reset (`do` already sleeps on it, `github.go:375-391`)". In fact `do` sleeps at most 10 s and gives up after 3 attempts (`github.go:376-383`), so an exhausted limit becomes a failed read and counts toward the 20-try limit.
- Recommendation: State the real 30 s tick. Restate the per-pass and per-minute read bound from it (or set the per-pass budget so the stated bound holds). Correct the rate-limit sentence: a 429/403-limited read counts as a failed read and backs off.

### `ghPR.MergedAt` has no Go type, and the obvious one breaks every merged-PR read - Severity: Major
- Affected scope: §3 step 1, §4.2 `UnitWatch.MergedAt int64`, §12 `dashboard/github.go`
- Finding: GitHub returns `merged_at` as an ISO-8601 string. The design adds `ghPR.MergedAt` without a type, and the target field `UnitWatch.MergedAt` is `int64`. An implementer who writes `MergedAt int64 \`json:"merged_at"\`` makes `json.Unmarshal` fail on every merged pull request. `pullRequest` would then return an error, and D-102's `MergeChecked` (`prflow.go:456-460`) would stop being set for every product. Separately, §3 says `ghPR` "gains … `Base.Ref`", but `Base.Ref` already exists (`github.go:66-67`).
- Recommendation: Specify `MergedAt *time.Time \`json:"merged_at"\`` (or a string parsed with `time.RFC3339`), and state the conversion to epoch seconds plus what happens when parsing fails (plant nothing, log `unit-watch-skipped bad-merged-at`). Drop `Base.Ref` from the "gains" list.

### Needed store methods and `Result` fields are not named or typed - Severity: Major
- Affected scope: §4.2, §5.1, §5.3, §12 `store.go`, `unitchecks.go`
- Finding: The design names write methods (`PlantUnitWatch`, `DelayUnitWatch`, `FinishUnitWatch`) but none of the methods its own steps need:
  - **Listing watches:** `unitPass` iterates "each pending watch", and the page shows "the last watch's outcome" per repository. No `Store` reader is named for either.
  - **Pruning watches:** "the watch retention" in `unitchecks.go` has no method name. It also has no place in `p.retain` (`fixprpoll.go:225-244`), which returns early when `ExpireExcerpts` fails.
  - **Notice state:** `UnitNoticeSent` must be set after the send, and "re-sends on the next pass" needs a scan of defects with `UnitNoticeSent == false`. Neither the setter nor the scan is named.
  - **One-time and daily notices:** the "marked passed (once)" notice and the "once per product per UTC day" `no-ci` notice have no stated state (persisted or in memory).
  - **Missing types:** `UnitPassIn` is used in `FinishUnitWatch`'s signature and never defined. `jobURL` is passed to `passUnitDefectsLocked` but stored nowhere.
  - **New-or-reopened signal:** the caller must build context "when the defect is new or reopened", but nothing says how `FinishUnitWatch` reports that.
  - **Persistence:** `ArcadeStore` must write watches "in the mutation's transaction", but `Result` gains no `UnitWatches` field to carry them.
- Recommendation: Add these to §5.1 and §12, each with a signature and its `readOnly` behaviour:
  - `UnitWatches(ctx, productID) ([]UnitWatch, error)`
  - `PruneUnitWatches(ctx, before int64) (Result, error)`, with where it runs in `retain`
  - `MarkUnitNotice(ctx, productID, key, which string) (Result, error)`
  - `Result.UnitWatches []UnitWatch`
  - the `UnitPassIn` struct
  - the return contract of `FinishUnitWatch` (for example `Result.Defects[0]` plus a `Reopened`/`New` flag)
  - where the daily `no-ci` mark lives

### The `UnitWatch` DDL is "with its properties" - Severity: Major
- Affected scope: §4.2, §12 row `dashboard/schema/21-unit-checks.sql`
- Finding: The `Defect` additions are listed with types, but the new vertex type is not. Its parent type, property names, types, `mandatory`/`notnull`, and the `regexp` for `state`/`outcome` are all missing. The sibling `18-pull-requests.sql:9-33` shows the level of detail the project uses. Two engineers would write different property names, and the boot loader must agree with them.
- Recommendation: Write out the DDL, for example: `CREATE VERTEX TYPE UnitWatch IF NOT EXISTS EXTENDS GaosVertex;` then `productId`, `repo`, `sha`, `base` STRING; `pr`, `tries` INTEGER; `mergedAt`, `firstSeen`, `nextAt` LONG; `state` regexp `pending|done`; `outcome` regexp; `defectKey` STRING; then the index.

### `dashboard/store_mem.go` is missing from the change list - Severity: Major
- Affected scope: §4.2 ("`MemStore.unitWatches map[string]UnitWatch`"), §12
- Finding: The `MemStore` struct and its constructor are in `dashboard/store_mem.go:43` and `:307` (see `fixPRs` at `:216`). Adding the map and initialising it touches that file, which no change-list row names. A write allowlist built from §12 would block the change.
- Recommendation: Add `dashboard/store_mem.go`: the `unitWatches` field and its initialisation in the constructor.

### The workflow template aborts under GitHub's default `bash -eo pipefail` - Severity: Major
- Affected scope: §11 YAML, the `go test` step
- Finding: With `shell: bash`, GitHub runs `bash --noprofile --norc -eo pipefail {0}`. After `set -e`, this line runs inside the `while read` pipeline subshell:

  ```
  file=$(grep -rl --include='*_test.go' "func ${test%%/*}(" "$dir" 2>/dev/null | head -n 1)
  ```

  It exits non-zero when `grep` finds no match (for example a method-based suite, or a `func TestX (t` spacing), or when `grep` gets SIGPIPE from `head`. That aborts the step before any later `::error` lines and before `::notice title=unit-tests-total`. The conclusion is still `failure`, so GAOS sees no kept test annotation and files the `~build` defect instead of naming the tests (§5.2). The template also assumes `go.mod` is at the repository root (`go-version-file: go.mod`, and paths relative to `go list -m`).
- Recommendation: Add `|| true` inside the substitution, or run the parsing block after `set +o pipefail`. Add a TC-861 sub-case (or a script check) using a test name `grep` cannot find. State the root-`go.mod` assumption in §11.

### The merge-time GitHub failure path is not fully decided - Severity: Major
- Affected scope: §4.2, second and first paragraphs
- Finding: On a transient re-read error for a merge, the event is not retried, nothing is planted, and the agent card is dropped anyway. The merge is never checked. The design says the skip is "counted on the page", but no field, store method or view model holds that count. Also undecided: what happens when `repoDefault` fails while `BaseBranch` is empty (plant or skip?). And "once per repository per pass" has no meaning in an event handler: `prFlow.handle` has no pass.
- Recommendation: Name where the skip count lives (for example an in-memory counter on `prFlow` exposed to `autofix_view.go`, reset at boot), or plant the watch with a `skipped:<class>` outcome so the page reads it from the store. State that a `repoDefault` error plants nothing and logs `unit-watch-skipped default-<class>`. Replace "per pass" with "per call, cached 10 minutes".

### A reopened unit defect can be released but can never produce a pull request - Severity: Major
- Affected scope: §7.4 "Seen again, final", §14 "Flaky tests"
- Finding: `reopenUnitDefectLocked` sets `Regression true`. `fixEligible` refuses every regression (`autofixpr.go:84-85`, "the defect is a regression"), and `autoClaimLocked` refuses to dispatch it (`autofix.go:175-177`). After an admin releases a reopened defect, triage runs, and the fixer (dispatched by hand) always ends in Review without a PR. §14's "a recurrence reopens as a regression a person releases" suggests a fix path that does not exist. The design must either accept this or exempt unit defects.
- Recommendation: State it explicitly: a reopened unit defect is person-fixed only, and its fixer ends in Review. Or add a `fixEligible` exemption for `Kind == unit` with its own test in TC-865. Record the choice in D-105.

### "An admin's cancel also closes the defect" has an undecided scope - Severity: Major
- Affected scope: §7.3 "Store guard"
- Finding: The sentence can mean the held triage card only, or any card of a unit defect, including a released fixer card. In the second reading, an admin cancelling a stuck fixer card closes the defect, so the next failing merge reopens it as a regression. That leads straight into the finding above. Two implementations would differ in defect state.
- Recommendation: State which cards' admin cancel closes the defect (for example: only a card BLOCKED `ReasonUnitHeld`; an admin cancel of any other unit-defect card leaves the defect in its state). Add that case to TC-863.

### Several changes have no named test - Severity: Major
- Affected scope: §9 phases 4 and 5
- Finding: These changes appear in "Builds" but no TC name covers them:
  - the notices: the held-card notice with its `UnitNoticeSent` re-send, the marked-passed notice sent once, the closed-by-pass notice, and the daily `no-ci` notice;
  - the watch pruning after 30 days;
  - the `ExpireExcerpts` change that empties `UnitTests`;
  - `buildUnitContext` and the `buildAndStoreDefectContext` early return;
  - `trimUnitMoves` on the paged board.

  TC-861 lists only outcomes.
- Recommendation: Add the notice and retention assertions to named cases (for example extend TC-861 with "notices", and add TC-857 sub-cases "retention" and "context"), or allocate new TC IDs and update the 16-case arithmetic.

### The unit fixer's daily cap is not stated - Severity: Nit
- Affected scope: §7.2, §7.5
- Finding: `autoClaimLocked` counts review fixes against `ReviewCap` and every other fix against `FixCap` (`autofix.go:187-199`). The design does not say whether unit fixes share `FixCap` with QA defects, as the code would do by default.
- Recommendation: Add one sentence: "unit fixes count against `AutoFix.FixCap`."

### The `fixBranch` generation suffix is not stated - Severity: Nit
- Affected scope: §7.5 "Base"
- Finding: The existing `fixBranch` appends `-<gen>` when `Gen > 1` (`autofixpr.go:37-39`). The new form `unit-<h8>-<repo/base slug>` "cut to 60" does not say whether the suffix is kept, or whether the cut happens before it.
- Recommendation: Specify `unit-<h8>-<slug>` cut to 60, then `-<gen>` when `Gen > 1`, as today.

### The forward count strings are not all stated - Severity: Nit
- Affected scope: §9 note, §10, §12 last row
- Finding: Only "Sixty-four rules" is given. `deploy/check-workspace.sh:129-133` asserts "eight hundred and fifty" in `qa-test-plan.html` and in `toc/product.js`, and "One hundred and three entries" in the decision log. `brd.html:1522` also says "Sixty-three in all". The design does not state the new strings. In addition, §9 says each phase writes "its QA-plan rows and counts", but the counted files (`check-workspace.sh`, `README.md`, `toc/product.js`) are listed only under phase 6.
- Recommendation: List each after-value: "eight hundred and sixty-six" ×2, "One hundred and four entries", "Sixty-four rules", `brd.html:1522` "Sixty-four in all", and the Go-test prose 729/704. State which phase edits the count files.

### Two anchors point at the wrong lines - Severity: Nit
- Affected scope: §7.3 table row `ResolvePRCard`
- Finding: `prflow.go:600-606` is the `resolveFrom` table. `resolveCardLocked` is at `prflow.go:638`.
- Recommendation: Cite `prflow.go:638`.

### Where `BoardCard.UnitDefect` gets set is not stated - Severity: Nit
- Affected scope: §7.3 "Board"
- Finding: The design says the flag is "set at projection", but `boardOf` is pure over tasks and has no defects (`board.go:278-307`). It would have to be a read-time decoration like `decorateCardsGate` (`board.go:270`), and the paged board needs the same.
- Recommendation: Name the decorator (for example `decorateCardsUnit` in `MemStore.Board` and `BoardPage`).

Sections that hold up: the problem statement, the decision and dropped-scope lists (§0), the glossary, the outcome table (§6), the dedup, ordering and gate table (§7.2), the alternatives (§13), the non-goals (§14), and the register identifiers (§10).

Counts: B=0 M=10 N=5
