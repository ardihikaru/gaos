# Readiness review: `docs/designs/agent-seats-and-pr-agents.md` (rev 2)

Scope: `/home/ardi/devel/projects/ardihikaru/my-projects/gaos-agent-seats/docs/designs/agent-seats-and-pr-agents.md`, checked against the repository at `main` (bf0e863).

**What checks out:**
- **Identifiers.** D-099 follows D-098 (`decision-log.html:2118`). BR-59 follows BR-58 (`brd.html:1328`). TC-767 follows TC-766 (`qa-test-plan.html:857`). v0.61.0 follows v0.60.0 (`changelog.html:39`). `schema/19-*` follows `18-pull-requests.sql`, and the `//go:embed schema/*.sql` glob (`schema_embed.go:22`) picks a new schema file up.
- **Anchors.** The anchors I checked resolve to what the design says they do.

### Review fix card key uses generation 0, which ArcadeDB refuses - Severity: Blocker
- Affected scope: §6.2 "The fix card" (`defectCardKey(key, 0, "fix")`, `defect:<key>:0:fix`); the §6.2 field table.
- Finding: The fix card's key always takes its generation from the record (`planDefectCardLocked`, `defecttriage.go:83`: `defectCardKey(d.Key, d.Gen, job)`). `fixEligible` also requires `d.Gen == gen` (`autofixpr.go:86`). So a `:0:fix` key means `Defect.Gen == 0`. But `Defect.gen` is `INTEGER (mandatory true, min 1)` (`schema/17-defects.sql:29`), so the record would be refused at commit on ArcadeDB. The field table never sets `Gen`, and `FirstSeen`, `LastSeen` (both mandatory) and `ProductID` are missing from it as well. An engineer who sets `Gen=1` gets `:1:fix`, which contradicts the design and any test written from it. This is the "passes in memory, fails on ArcadeDB" class of bug that §11 itself warns about.
- Recommendation: Add `Gen = 1`, `FirstSeen = LastSeen = now`, `ProductID` and `Regression = false` to the §6.2 table. Change the key to `defect:<key>:1:fix`. Have TC-778 assert the ArcadeDB round trip.

### Release and count files missing from the change list, so the last check fails by construction - Severity: Blocker
- Affected scope: §9 "Release", §10 row 21, §11 command 4, §14 phase 3.
- Finding: `deploy/check-workspace.sh` checks fixed strings and counts:
  - `"six hundred and thirty"` together with `go_tests == 630` (`:114-115`);
  - `"seven hundred and sixty-six"` in `qa-test-plan.html` and in `pustaka/docs/assets/toc/product.js` (`:129-130`);
  - `"Fifty-eight rules"` in `brd.html` (`:133`);
  - the decision count (`:131`).

  The version also appears in `pustaka/docs/assets/toc.js` (the `semver` read at `:138`) and in `dashboard/templates/page.html`. None of `deploy/check-workspace.sh`, `pustaka/docs/assets/toc/product.js`, `pustaka/docs/assets/toc.js` or `templates/page.html` is in §10. The new values are not stated either: 646 Go tests (622 + 24), 782 cases, "Fifty-nine rules", the new decision count, and the README's 630/606 and 766 at `README.md:137-139`. Under a write allowlist the implementer cannot make command 4 pass.
- Recommendation: Add these files to §10 row 21. State every new count and string as a value, not as "updated".

### The gate's sentinel loses the Pull requests row on ArcadeDB, and the merge-row premise is wrong - Severity: Major
- Affected scope: §3.4 "Where the gate sits"; TC-773.
- Finding:
  1. `reviewRowLocked` writes a row only for `pr.opened` (`pullrequests.go:306`). "A merged pull request still … writes its Pull requests row" is therefore false.
  2. On `pr.opened` with both `pr-reviewer` and `qa` off, the sentinel is returned *after* `reviewRowLocked` has already upserted the row into the MemStore projection. `ArcadeStore.mutate` throws the Result away on any error (`store_arcade.go:556-558`). The row then exists in memory but not in the database, and disappears on restart.
  3. Lines 253 and 257 have the existing early return between them (`:254-256`). The design does not say whether the gate goes before or after it. If it goes after, a fully gated `fresh` falls through, plans nothing and returns no sentinel.
- Recommendation:
  - Put the gate at `:253½`, before the early return.
  - Return `ErrPRUnassigned` with `Result{PullRequests: rows}` and have the ArcadeStore wrapper persist that Result, or return no error and carry an `Unassigned` flag instead.
  - Rewrite TC-773 as "opened, every role off: the row survives a restart".

### Closing the defect after a refusal cancels the blocked card; the `pr-merged` close has no API - Severity: Major
- Affected scope: §6.4 "Any refusal blocks the card … and closes the defect with `CloseDefect`"; the "If the reviewed pull request merges first" bullets; §16.
- Finding:
  1. `CloseDefect` calls `cancelDefectCardsLocked` (`defects.go:598`). That function does not skip `BLOCKED` (`defects.go:363-365`), and `BLOCKED → CANCELED` is a legal move (`taskstate.go:92`). Blocking and then closing leaves the card CANCELED with rule `defect-closed`, and the refusal reason that §16 promises is lost.
  2. `CloseDefect` always uses `RuleDefectClosed` and takes no rule parameter, so "closed with rule `pr-merged`" cannot be done through it. Inside `PlanPullRequestCards` it would have to call `setDefectStateLocked` directly. The Result would also need `Defects: […]`, or ArcadeDB never sees the close.
- Recommendation:
  - Specify a store method (or a `rule` argument) that closes the defect **without** cancelling the blocked fix card.
  - State the order: block first, then close.
  - For the merge path, state that `PlanPullRequestCards` calls `setDefectStateLocked(d, closed, RulePRMerged, "")` and adds the record to `Result.Defects`.

### `reviewFixRefusal` cannot check workflows or the default branch with the inputs it has - Severity: Major
- Affected scope: §6.4, the predicate signature and its check table.
- Finding:
  1. The `review-workflows` check ("no changed file … under `.github/`") has no file list in `(d Defect, pr ghPR, defaultBranch string)`. `d.Files` cannot stand in for it: it is filtered by `contextPathOK`, which rejects `.github/**` through `builtinForbiddenGlobs` (`autofixglob.go:11`, `defecttriage.go:271`), and it is capped at 6 files. A check over `d.Files` can never fire.
  2. Where `defaultBranch` comes from is not decided. `ghPR` has no `default_branch` (`github.go:62-67`). `ghWriter.head` hides it when an override is passed (`githubwrite.go:198-200`). `AutoFix.BaseBranch` may be empty. Each choice behaves differently.
  3. The design does not say which client makes the fresh reads at points 2 and 3. `defectFixer.gh` is a `*ghWriter`, and §10 row 8 names only the `autoFixPass` reader.
- Recommendation: Change the signature to `reviewFixRefusal(d, pr, files []ghFile, defaultBranch string)`. Say that `files` comes from a fresh `pullFiles` read and `defaultBranch` from `GET /repos/{o}/{r}` `default_branch`, both with the write token through `ghWriter.c`. Add the `defectFixer` wiring to §10.

### Where the review defect's source slice and pinned head come from is undefined - Severity: Major
- Affected scope: §6.2 (`ReviewDefectIn`, "with the values it gathered"; `Context.Repos[0].Head`), §6.3.
- Finding:
  1. `ReviewDefectIn` has no field list.
  2. A store method holds the lock and makes no GitHub calls, so the context has to be built *before* `PlanReviewDefect`, from a `Defect` that is not stored yet. The design does not say who builds it, or with which `codeSource` and which token.
  3. The review branch of `buildDefectContext` reads "at the pinned head SHA", but the pinned head is defined as `Context.Repos[0].Head`, which is that same function's output. Today the function gets heads from `defaultHead(baseBranch)` (`defecttriage.go:308`).
  4. "Head SHA at review time" is ambiguous. It could be the SHA whose diff was cached at `pr.opened`, or the SHA from `afterRun`'s fresh read after posting. These differ whenever there was a push in between.
  5. Failures on this path have no defined outcome or card reason: the read fails, `pullFiles` fails, `PlanReviewDefect` errors, `head.ref` fails `baseBranchRe`, or `AssignedSet` is false.
- Recommendation:
  - Give `ReviewDefectIn` as a struct.
  - State the call order: fresh `pullRequest` and `pullFiles` reads, then `buildDefectContext(…, pinnedHead)` with an explicit head parameter, then `PlanReviewDefect`.
  - Pin to the SHA that was reviewed: store `head.sha` in the cached `prContext` at `fetchContext`.
  - Give the reason string for each failure branch in the §5 table.

### The own-pull-request rule has no defined behaviour when either login is unknown - Severity: Major
- Affected scope: §5 "The own-pull-request rule".
- Finding: The author's login lives in `prContextCache`. The cache is in memory and is lost on restart (`prreview.go:141-143`), so after a restart the comparison has nothing to compare. Nothing says what happens if `GET /user` fails at start (refuse to start, retry, or always `COMMENT`). Nothing says whether `afterRun`'s fresh read, which carries `user.login`, overrides the cached value. Two engineers would pick different fallbacks, and one of those fallbacks sends a `REQUEST_CHANGES` that GitHub refuses (422).
- Recommendation: Decide all three. For example: the fresh read in `afterRun` is authoritative; an unknown author or an unknown own login means `COMMENT` with reason `… (author unknown: comment only)`; a `GET /user` failure is logged and retried on each review. Add these cases to TC-777.

### The scheduler never sees defect cards, so the fixer assignment cannot steer them - Severity: Major
- Affected scope: §6.2 "born PENDING, and moved by the scheduler"; §3.5; §1 Goal; TC-774; §16.
- Finding: The scheduler skips every `defect:*` card (`scheduler.go:286`, `labels.go:110-114`). `readyDefectCards` readies them instead (`defecttriage.go:789-807`), and `AutoDispatch` and the board's `dispatch` name no agent (`board.go:528`). The new `freeAgent` rule therefore never applies to a review-fix card. The only place an assigned fixer could show up is `liveAgentSlots`. §1's "the scheduler gives that product's cards only to assigned agents" is not true for fixer work.
- Recommendation: Correct the premise. State that for defect cards the assignment acts only at planning (the §3.4 gate) and in `liveAgentSlots` (display), or add an explicit assigned-agent check to `autoClaimLocked`. Scope TC-774 to match.

### The Agents page has no way to read `liveAgentSlots` - Severity: Major
- Affected scope: §3.5 last paragraphs, §3.6, §13 "New store methods 2".
- Finding: `liveAgentSlots` is an unexported helper called under the MemStore lock (`store_mem.go:2402`). `buildBO` only holds a `Store`, and no `Store` method returns the live slots. The design does not choose between a new store read (which makes the method count 3) and recomputing in the handler from some task listing.
- Recommendation: Name the method, for example `LiveAgents(ctx) ([]AgentSlot, error)` on the three stores, and update §13. Or name the existing read the handler will use. Also state `liveAgentSlots`' new signature (it needs the `Assigned` map) and `freeAgent`'s.

### Test files are not named, and phase exits can pass on a skipped test - Severity: Major
- Affected scope: §10 row 20 ("tests (§11)"), §11, §14.
- Finding:
  1. No `_test.go` path is given for any of TC-767 to TC-782, yet every row in the QA register names its file (for example `qa-test-plan.html:4467`).
  2. Phase 1's exit includes TC-768 (the ArcadeDB round trip), but it runs only `go test ./dashboard/...`. Without `GAOS_ARCADE_TEST_URL` the ArcadeDB half is skipped (`store_conformance_test.go:1620-1621`), so the exit condition is met by a skip.
  3. Phase 2's exit runs no full suite.
  4. Phase 3 "Needs … the schema file", but the schema file is itself phase 3 work.
- Recommendation:
  - Give each TC a file path in §11 and list those files in §10.
  - Make every phase's exit "`go test ./dashboard/...` green, plus the ArcadeDB suite on the scratch database for any TC that touches ArcadeStore".
  - Put `schema/19-review-defects.sql` first within phase 3.

### Severity column's default sort direction not stated - Severity: Nit
- Affected scope: §4.2 Table.
- Finding: `boardView` sorts a column ascending by default unless told otherwise (`board_view.go:154-157`). Ascending on `prSeverityRank` puts `none` first.
- Recommendation: State the natural direction, presumably `desc` (critical first).

### Two places build `BoardCard` - Severity: Nit
- Affected scope: §4.2 Kanban ("`board.go`, the card's `Labels`").
- Finding: `BoardCard` is built in `board.go:286` and in `cardOf` at `board_page.go:381`, and both feed JSON (`json:"labels"`).
- Recommendation: Say which builders strip the severity label and set `Severity`, and whether the JSON `labels` keeps it.

### Reset with boxes still ticked - Severity: Nit
- Affected scope: §3.3, §3.6 item 5.
- Finding: With `action=reset`, the editor's checked boxes still post `agent` values, but `SetAssigned` requires an empty list when `set == false`.
- Recommendation: Say that the route drops `agent` values on reset.

### The test to update does not exist under that name - Severity: Nit
- Affected scope: §11 "The existing `TestLabelVocabulary`".
- Finding: The test is `TestTheLabelVocabularyIsClosed` (`labels_test.go:105`), and it does not count labels.
- Recommendation: Use the real name and say what assertion changes. `TestDefectLabelFamilyIsPlannerOnly` may also deserve a severity counterpart.

### Where the FixPR-merge skip sits is not stated - Severity: Nit
- Affected scope: §3.4 "GAOS's own fix pull requests".
- Finding: The design does not say whether the skip is in `prFlow.handle` or in `PlanPullRequestCards`, whether it runs before or after the cancel loop, or what it returns.
- Recommendation: Name the place and the return value, for example an `ErrPRSkipped` of class `own-fix`.

### The validated product's field and query order are not named - Severity: Nit
- Affected scope: §3.6, "`address()` default branch".
- Finding: The view field that carries the validated product is not named, and the order of `product` and `page` in the query is not given.
- Recommendation: Name the field and give the order, for example `agents?product=<id>&page=<n>`.

### Which file holds which function - Severity: Nit
- Affected scope: §10 row 15.
- Finding: The row lists `defects.go` and new `reviewdefect.go` but does not say which of the two holds `PlanReviewDefect` and which holds `reviewFixRefusal`.
- Recommendation: Assign each function to one file.

Counts: B=2 M=8 N=7
