# Readiness review: `agent-seats-and-pr-agents.md` (rev 3)

I checked the design's anchors against the code. Most of them hold. The identifiers are allocated correctly: D-099 is the next free decision (D-098 is the last, with 97 rows), TC-767 is the next free case (TC-766 is the last), BR-59 follows "Fifty-eight rules", and v0.61.0 follows semver `0.60.0`. The dashboard has 606 Go tests, and the six `postReview` call sites exist where §10 row 22 says. The findings below are where the plan still leaves a choice to the implementer, or names the wrong file.

### `checkFix` lives in `autofixcheck.go`, and its signature has no `Defect` - Severity: Major
- Affected scope: §6.3 "`checkFix` (`autofixpr.go:231-234`) gains one rule"; §10 row 16
- Finding: `checkFix` is defined at `dashboard/autofixcheck.go:275` as `checkFix(r FixReply, packet map[string]DefectContextFile, af AutoFix, lookup fixLookup)`. `autofixpr.go:232` only calls it. `autofixcheck.go` is not in the change list. The function also cannot see `d.Files`. Two things are left open:
  - whether the rule goes inside `checkFix` (new parameter, new file) or into `run` after it;
  - whether `r.NewFiles` (new test files, which `checkFix:313-327` allows) are refused, since a new file can never be one of `d.Files`. §6.3 only says "any edited path".
- Recommendation: Add `autofixcheck.go` to §10. Give the new signature, for example `checkFix(..., onlyPaths []string)`, nil for non-review defects. State whether review fixes may add new test files.

### The `Defect` struct and its ArcadeDB persistence are in files the change list leaves out - Severity: Major
- Affected scope: §10 rows 1 and 4; §6.2 "new fields"
- Finding:
  - `Defect` is declared at `dashboard/defects.go:71`, not in `model.go` (row 1).
  - Its write and read are `writeDefect` and `loadDefects` in `dashboard/store_arcade_defects.go:36-119`, which lists every column explicitly. That file is not in §10. Row 4 puts "the Defect's two new properties" in `store_arcade.go`, which has no Defect columns.
  - `Result` is at `store.go:245`, not `model.go`.
  - Without `store_arcade_defects.go`, `reviewBranch` and `reviewHead` never reach the database, and TC-778's round trip fails.
- Recommendation: Move `Defect.ReviewBranch`/`ReviewHead` and `Result.Skipped` to their real files. Add `store_arcade_defects.go` to §10: the upsert map, the row struct, the SELECT list and the struct literal.

### New GitHub seams and wiring are named but not defined - Severity: Major
- Affected scope: §5 "The reviewer gains a `prReader`"; §6.2 steps 2–4; §6.4 check point 1; §10 rows 7 and 13
- Finding:
  - **`prReader` does not exist.** The repository has no `prReader`. The interface with `pullRequest` and `pullFiles` is `prSource` (`prflow.go:323-326`).
  - **No method for `GET /user` or the default branch.** Nothing on the client reads `GET /user`. Nothing returns a repository's `default_branch` through the read token: `defaultHead` (`githubwrite.go:35`) returns only a SHA, and `ghWriter.head` uses the write client.
  - **The reviewer cannot reach the context cache.** `prReviewer` (`prreview.go:217-222`) has no `prContextCache`, yet §6.2 step 2 reads `prContext.HeadSHA` from it.
  - **The reviewer has no `codeSource`.** It has none to pass to `buildReviewContext`.
  - **The wrong function is named.** `autoFixPass` is called only by `autoFixLoop(ctx, st, src)` (`autofix.go:235-243`; `main.go:434`), so it is the loop's signature that changes.
  - **Open choices.** Implementers will differ on the interface name, its method set, which file declares it, and which fields `prReviewer` gains.
- Recommendation:
  - Declare the interface with its exact methods, for example `prReader{ pullRequest; pullFiles; repoDefault(ctx,o,r) (string, error); selfLogin(ctx) (string, error) }`, and the file it lives in.
  - List the new `prReviewer` fields: `rd prReader`, `ctx *prContextCache`, `src codeSource`.
  - State the new `autoFixLoop` and `autoFixPass` signatures and the exact `main.go` wiring (`rv.ctx = flow.ctx`, and so on).

### The §7 draft close uses a function that cannot close a draft, and the poller has no write client - Severity: Major
- Affected scope: §7 bullet 3; §10 rows 7 and 19
- Finding:
  - `closeRecord` (`autofixpr.go:186-191`) is a `defectFixer` method. It only acts on `rec.State == FixPRPreparing`, so it does nothing to a record in Draft.
  - The poller holds only the read-token `*ghClient` (`fixprpoll.go:105-123`; `main.go:436` `newFixPRPoller(store, ghc)`).
  - No `ghWriter` method closes a pull request (methods listed at `githubwrite.go:170-450`).
  - The design does not say which store call moves the FixPR to closed: `AdvanceFixPR`, or `CloseReviewDefect` returning `FixPRs` as well.
- Recommendation:
  - Add a `ghWriter.closePR(ctx, owner, repo, n)` method (PATCH `state=closed`).
  - Inject `*ghWriter` into `fixPRPoller`, with the new `newFixPRPoller` signature and the `main.go` change.
  - Name the store path that records the FixPR as closed in the same transaction as the defect close.
  - Add the comment post (`addComment`) to the poller's dependencies.

### The merge-time close of a review defect is ambiguous about the card's state - Severity: Major
- Affected scope: §7 bullets 1–2
- Finding: Bullet 1 says that on `pr.merged`, the card is canceled "if Pending or Ready", and then says unconditionally that "it moves the record to `closed`". If the record is closed while the fixer card is Running:
  - `fixEligible` (`autofixpr.go:88`) answers "the defect is closed";
  - `afterRun` returns at `:161-163`, and the card ends in Review with no pull request;
  - bullet 2's `review-pr-closed` Blocked outcome never happens.

  If a draft is already open, closing the defect at merge also races bullet 3's `review-base-changed` close. PlanPullRequestCards has no way to know "before dispatch" other than the card's status.
- Recommendation: State that the record is closed only when the card was Pending or Ready (or has no FixPR). In every other case, leave it to check point 2 or the poller.

### The review card's final reason and the order of `ResolvePRCard` are not specified - Severity: Major
- Affected scope: §5 table row `low`; §6.1 suffix list; §6.2 call order
- Finding:
  - Today `afterRun` resolves the card Done with "review posted" right after posting (`prreview.go:260`). A Done card cannot be resolved again: `resolveFrom[StatusDone]` excludes Done (`prflow.go:483`).
  - The design has the reason "gain" a suffix after §6.1/§6.2, but never says the Done resolution moves to after `PlanReviewDefect`.
  - It also never gives the reason when a defect *is* planned. Only the failure suffixes are listed.
  - The own/unknown-author row's `…` placeholder leaves open whether a `none` or `low` review, which posts COMMENT anyway, also gets "(comment only: own or unknown author)".
- Recommendation:
  - State that for `low` on a configured product, `ResolvePRCard(Done, reason)` is called once, after step 5.
  - Give the success text, for example `review posted; review fix planned (<card id>)`.
  - Spell out the exact reason string for each severity in the own/unknown-author row.

### Which files go into `in.Files` is undecided - Severity: Major
- Affected scope: §6.2 step 4 and `ReviewDefectIn.Files` ("passed contextPathOK, at most 6")
- Finding: A pull request may change more than 6 files. The plan does not say which 6 are kept: GitHub's order, the `contextClass` order (`defecttriage.go:278`), or the first 6 that `fileAt` finds. It also does not say whether removed files (`ghFile.Status == "removed"`, where `fileAt` answers not found) are counted. The §6.3 changed-files rule then binds the fixer to that choice, so two implementations allow different edits.
- Recommendation: Specify:
  - the filter (skip `removed`, skip anything `contextPathOK` rejects);
  - the order (for example `contextClass`, then path);
  - that `Files` equals exactly the paths whose blobs landed in `Context.Files`.

### The verification commands fail from the repository root - Severity: Major
- Affected scope: §11 "Commands" 1–3; §14 every phase exit
- Finding: There is no `go.mod` or `go.work` at the root, only `dashboard/go.mod` and `pustaka/go.mod`. So `go test ./dashboard/...` run from the root does not resolve. `README.md:131` uses `cd dashboard && go test -race ./...`. Command 3's "none skipped" cannot be checked without `-v`, because the ArcadeDB tests skip quietly (`store_conformance_test.go:1621`).
- Recommendation: Write the commands as `(cd dashboard && go test ./...)` and the matching `-race` and `-run` forms. For command 3, add `-v` and an explicit check that the output has no `--- SKIP` line.

### `check-workspace.sh` will still fail: pinned prose strings and the README version are not in §9 - Severity: Major
- Affected scope: §9 bullets "Go test count", "README", "Release"; §10 row 25; command 4
- Finding:
  - `deploy/check-workspace.sh` also pins `"seven hundred and sixty-six"` (lines 129–130), `"Ninety-seven entries"` (131) and `"Fifty-eight rules"` (133). §9 changes those registers, but only names lines 114–115 and 118 of the script.
  - `readme_check "README version"` (line 149) needs `README.md:12` to read "Product version 0.61.0". §9 only edits `README.md:137-139`.
  - So command 4 fails by construction. Round 2 raised this as a Blocker and marked it fixed, but it is still incomplete.
- Recommendation: List `check-workspace.sh` lines 129, 130, 131 and 133 with their new strings ("seven hundred and eighty-two", "Ninety-eight entries", "Fifty-nine rules"). Add `README.md:12` to the README bullet.

### The test files are not stated as new or modified, and two named files do not exist - Severity: Major
- Affected scope: §10 row 22 ("test files (§11)"); §11 File column
- Finding:
  - `handlers_bo_test.go` and `autofixpr_test.go` do not exist. The fixer's tests live in `fixpr_test.go`, which has the `fixEnv` harness.
  - `assign_test.go`, `handlers_assign_test.go` and `reviewdefect_test.go` are also new.
  - Row 22 is a description, not a path list, so a write allowlist cannot be built from it.
- Recommendation: Replace row 22 with explicit paths, each marked new or modified. Either point TC-781 at `fixpr_test.go` or mark `autofixpr_test.go` and `handlers_bo_test.go` as new.

### Phase exits depend on later phases, and the split-release registers are undefined - Severity: Major
- Affected scope: §14
- Finding:
  - Phase 1's exit includes TC-769 ("`COMMENT` only, no review defect"). The event choice is phase 2 and review defects are phase 3, so in phase 1 that case is empty or unwritable.
  - TC-774 needs `Defect.Kind == "review"`, which the schema allows only from phase 3.
  - Phases 1 and 2 add tests but do not run command 4, so the workspace check is red between phases.
  - "Phases 1 and 2 may ship alone as v0.61.0" does not say which D-099 items, BR-59 text, TC range, test counts (for example 622 / 646 vs. a partial count) or changelog apply to that split release.
- Recommendation:
  - Move TC-769's `COMMENT` assertion to phase 2 and its review-defect assertion to phase 3, and move TC-774 to phase 3.
  - Add command 4 to every phase, with the per-phase counts written down.
  - Either remove the split option, or give the register values for v0.61.0 (phases 1–2) and v0.62.0 (phase 3).

### The §8 change to the OQ-37 list is in no change row - Severity: Nit
- Affected scope: §8 "Manual dispatch… joins the unrestricted actions listed under OQ-37"; §9; §10
- Finding: That sentence implies an edit to `pustaka/docs/product/open-questions.html:907-916`. Neither §9 nor §10 lists that file. The operator's severity correction (§4.1) is a second new unrestricted action.
- Recommendation: Add `open-questions.html` with the exact sentence to add, or reword §8 so it is clearly a reference only.

### The §13 call arithmetic leaves out the per-review read - Severity: Nit
- Affected scope: §13 "New GitHub calls"; §5 "`afterRun` reads the pull request fresh"
- Finding: §5 adds a fresh pull-request read for every review on a configured product, either always or only when the severity is medium or worse; the design does not say which. §13 counts reads only per low review and per check point. Whether §6.2 step 2 can reuse the §5 read is also not stated.
- Recommendation: State when the §5 read happens and whether step 2 reuses it. Fix the count to match.

### Small undecided details - Severity: Nit
- Affected scope: §3.5, §3.4, §4.2, model changes
- Finding:
  - The subtitle at `handlers_bo.go:267` has a second sentence ("Capability and model routing…"). The design does not say whether it stays.
  - `agentByID` returns nil for an unknown id, and the gate dereferences `.Role`.
  - The position of `severity=` in `BoardQuery.Encode` is not given.
  - `AutoFix.Clone` (`model.go:480-485`) must deep-copy `ReviewFixHeads`, and §3.1 mentions only `Master.Clone`.
  - The refusal code `review-workflows` is also used for an incomplete file list.
- Recommendation: Settle each one in a single line.

Sections that hold up: §4 (the severity label family, the builders, the sort and the filter) is precise and anchored correctly. The §6.2 Defect field table matches `schema/17-defects.sql`, including the `Gen` 1 and `:1:fix` key fix. The registers and identifier allocation in §9 are correct, apart from the `check-workspace.sh` and README gaps above.

Counts: B=0 M=11 N=3
