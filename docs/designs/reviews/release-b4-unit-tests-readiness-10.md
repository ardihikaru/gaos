# Readiness review: `docs/designs/release-b4-unit-tests.md` (rev 10)

Reviewed with the Tech Lead lens. I checked the design against the code on `main` (`ba7026a`) under `/home/ardi/devel/projects/ardihikaru/my-projects/gaos-unit-design`.

These parts hold up. The register identifiers are the next free values: D-104 is the highest decision, BR-63 the highest rule and TC-850 the highest case, so D-105, BR-64 and TC-851…TC-875 are correct. The TC arithmetic (3+4+5+7+5+1 = 25) and the per-phase Go test counts (713 → 738, and 688 → 713 in `dashboard/`) follow from the phase table. The `Reconcile` callers (four test files, `main.go:292-295`, `store_arcade.go:393-394`), the 17 `checkFix` calls in `fixpr_test.go`, the two `fixEligible` callers, the `CloseDefect` callers, the schema baseline (20 → 21) and the delegation record at `docs/designs/reviews/release-b4-unit-tests-delegation.md:14` all match the code. The rev-10 boot-path rewrite (§4.4) can be built as written.

### Metric 1's outcome rules can never appear in the event log - Severity: Major
- Affected scope: §0 "Success metrics"; §5.1 table (DONE "not a unit-test base" via `ResolvePRCard`); §6 poll order step (5) "`ResolvePRCard` DONE"
- Finding: §0 measures latency "to the `TaskMoved` event of its final move, whose payload `Rule` carries the outcome (`unit-passed`, `unit-failed`, `unit-no-tests` or `unit-not-base`)". Every final DONE move in the design goes through `ResolvePRCard`. That function takes no rule argument and stamps every hop with the fixed rule `RulePRResolved = "pr-flow"` (`prflow.go:592`, `:626`). The four rule names appear only in §0. No function sets them, no table row allocates them and no TC asserts them. As written, metric 1 cannot be computed and `unit-not-base` cards cannot be excluded.
- Recommendation: Pick one mechanism. Either give `ResolveUnitQACard` a RUNNING/PENDING/READY → DONE row taking `rule` (`unit-passed` and the rest) and send every unit DONE move through it, or derive the metric from the final reason text and say so. Add the assertion to TC-867.

### `labelsAllowDispatch(labels, key)` cannot be called where it is used, and its callers are not in the change list - Severity: Major
- Affected scope: §4.2 (the "`labelsAllowDispatch(labels, key)` gains the key" sentence); §12 `dashboard/labels.go` row; §12 "existing tests that change"
- Finding: The pure scheduler calls `labelsAllowDispatch(t.Labels)` on a `TaskNode` (`scheduler.go:286`). §4.4 gives `TaskNode` a `QAKey bool`, not a key, so a `key string` parameter cannot be passed there. Two engineers would solve this differently: a bool parameter, a second function, or a key added to `TaskNode`. The other callers are also missing from §12: `apidispatch.go:47` (`apiClaimLocked`) and the one-argument test calls at `labels_test.go:167`, `autofix_test.go:225` and `prflow_test.go:252`, `:257`. Only the `prflow_test.go` test is listed, and only for gaining `LabelUnitQA`.
- Recommendation: Fix one signature, for example `labelsAllowDispatch(labels []string, runnerKey bool)`. State that `Schedule` passes `t.QAKey` and `apiClaimLocked` passes `qaToolKey(t.DedupKey)`. Add `dashboard/apidispatch.go`, `dashboard/labels_test.go` and `dashboard/autofix_test.go` to §12, with the new expected values.

### `trimUnitMoves` lacks the data to decide, and its call sites are in files outside the change list - Severity: Major
- Affected scope: §7.3 "The board offers only what the store allows"; §12 `handlers_board.go, board_view.go, handlers_autofix.go` row
- Finding:
  - **Missing data.** `trimUnitMoves(r, cards []BoardCard)` must recognise a `:unit-qa` key, "a card of a `unit` defect", and a RUNNING QA-runner key. `BoardCard` (`board.go:40-90`) carries no `DedupKey` and no defect kind. Only `Reason` is set, and only on BLOCKED cards (`sendback.go:385-386`). The design also says "`decorateCardsGate` and the `Store` board methods do not change". So the function cannot make its decisions from its input, except for the held reason and the `unit:qa` label.
  - **Call sites outside §12.** `renderBOAfter` lives in `handlers_bo.go:597` and `handleProductPage` in `urls.go:401`. Neither file is in §12.
  - **Three of the six HTML paths go through one function.** `handleBoardLive` and `renderBoardAfter` (`board_view.go:259`, `:278`) both go through `buildBoardView` (`board_view.go:208`), and `handleBoardPaged` reads `BoardPage` directly. The design does not choose between hooking each caller and hooking these two.
- Recommendation: Add the fields the trim needs to `BoardCard`, set at projection time (for example `QAKey bool`, `UnitDefect bool`, `json:"-"`), or let the trim take a store lookup. Name the exact insertion points, for example `buildBoardView` plus `handleBoard` and `handleBoardPaged`. Add `dashboard/handlers_bo.go` and/or `dashboard/urls.go` (or `board.go` for the new fields) to §12.

### The two-pass context cannot be built on `buildReviewContext` as it stands - Severity: Major
- Affected scope: §7.5 first paragraph; §9 phase 4 ("`buildUnitContext`") vs §9 preamble and §7.5 ("the whole split is built in phase 5"); §12
- Finding:
  - **No per-call limits.** `buildReviewContext` (`reviewdefect.go:319-347`) applies its own fixed limits: `defectMaxFiles` (6) and a running total against `defectMaxFileBytes` (64 KiB). It has no per-call file or byte budget. Pass 1 ("up to 6 files and 32 KiB") and pass 2 ("into the slots pass 1 left (at most 3) and the bytes left") therefore cannot be expressed through it without changing its signature, and `reviewdefect.go` is not in §12.
  - **Fetch budget not split.** The 12-fetch cap (`defectMaxFetch`) is stated as a total, but nothing says how it divides between the two passes.
  - **Phase 4 contents unstated.** Phase 4 lists `buildUnitContext` while phase 5 builds "the two-pass packet". What the phase-4 version contains is left to the implementer.
- Recommendation: Specify `buildReviewContext(ctx, src, repo, sha, paths, maxFiles, maxBytes int)`, or a new unit-only reader in `unitdefect.go`. State the fetch split, for example pass 1 at most 8 reads and pass 2 the rest. Add the file to §12. Say exactly what phase 4's `buildUnitContext` does (for example a single pass with no `Reference`), or move it wholly to phase 5.

### `PassUnitDefects` cannot know whether to write the marker - Severity: Major
- Affected scope: §6 poll order step (1) ("writing no marker"); §7.4 "It writes the act-once marker on `taskID` … except on the failure path"
- Finding: The signature is `PassUnitDefects(ctx, productID, repo, base, mergedAt, watched, buildOK, taskID, jobID, sha)`. `buildOK` is true for both `passed` and `failed` ("any result but `build-failed`"), so the store cannot tell the failure path from the pass path. Engineers will diverge: an empty `taskID`, a new flag, or always writing `unit:pass` and letting `PlanUnitDefect` overwrite it. The last option leaves a crash window in which `unit:pass` is the marker of a failed run, and adoption then reads it as "pass".
- Recommendation: State the rule. For example: "on the failure path the runner passes `taskID = ""` and no marker is written", or add `mark bool`. Cover the crash-between-steps case in TC-861 or TC-868.

### Distinct unit defects can share one `gaos/fix-*` branch - Severity: Major
- Affected scope: §7.1 `CaseID` (`unit:<owner>/<repo>~<base>~<h>`); §7.5 site table
- Finding: `fixBranch` (`autofixpr.go:29-41`) slugs `d.Ref()` (the `CaseID`) and cuts it to 60 characters. The unit `CaseID` puts the hash last, so a long owner, repository or base truncates part or all of the hash. Two unit defects of the same repository and base then get the same branch, for example `gaos/fix-unit-acme-corporation-route-planner-api-service-release-2026`. The build defect (`~build`) collides the same way. `fixBranch` is not among the "twelve non-test sites", and no TC covers it.
- Recommendation: Add a `fixBranch` row to §7.5. For a `unit` defect, the slug should keep the h8 (for example `unit-<h8>-<truncated repo/base>`, or append `-<h8>` after truncation). Add a TC-873 case with a 60+-character repository/base pair.

### Several changes have no stated verification - Severity: Major
- Affected scope: §12 `fixprpoll.go` row; §4.1 Auto-fix section; §12 `prview.go`, `github.go`, `qaclient.go` rows
- Finding: No TC asserts these changes:
  - **`ExpireExcerpts`.** The changed condition (`(d.Excerpt != "" || d.UnitTests != "")`, emptying both) changes retention for every defect kind. TC-861 only tests the refill after retention.
  - **The Auto-fix section's display rules.** The 60-second display-only cache `unitSandboxDisplay`, "full page render only, never in `/ui/bo/autofix/live`", the attestation fields and age, and the "runner is not running" warning.
  - **The unit *Base* display on `prview.go`.**
  - **`pullFilesFirstPage`.** Exactly one page.
  - **`ghError.Limited` / `unitReadTransient`.** TC-865 covers only `Retry`.
- Recommendation: Add named assertions. Suggested homes:
  - TC-862: retention empties `UnitTests` but not `UnitFiles`, and leaves a non-unit defect as today.
  - TC-875: the section renders only on the full page, with the warning text.
  - TC-873: the *Base* cell.
  - TC-863: one request for `pullFilesFirstPage`, and the `Limited` path.

### `Reconcile(ctx, false)`'s FAILED move has no persistence path on ArcadeDB - Severity: Nit
- Affected scope: §4.4 "Boot reconciliation"
- Finding: `ArcadeStore.Reconcile` is deliberately not wrapped in `mutate` (`store_arcade.go:385-395`). Its requeue loop's events are returned, not `publish`ed (`runner.go:705-721`). The design does not say how the new `qa-runner-absent` moves reach the database.
- Recommendation: State it. Either `Reconcile` calls `s.publish(...)` for the new moves after unlocking, or the design accepts the next floor write and says so. Mark TC-868 (A) if it should run against ArcadeDB.

### Identifiers listed inconsistently in §12 - Severity: Nit
- Affected scope: §12 rows `store.go…`, `qaclient.go`, `qajobs.go`, `unitqa.go`
- Finding:
  - **`Result` fields.** The list names `UnitPlanned`, `UnitMarker` and `UnitHeldPassed`, but omits `UnitMarkerValue` (§5.4, §7.1) and `UnitReleasedClosed` (§7.4).
  - **`qaWatched`.** Listed under both `qaclient.go` and `qajobs.go`.
  - **`sha40Re`.** Listed under both `unitqa.go` and `qajobs.go`.

  A duplicate declaration does not compile.
- Recommendation: One home per symbol. Complete the `Result` field list.

### "Attribution of every `MoveCard` move" is in two phases - Severity: Nit
- Affected scope: §9 phase 2 and phase 5 "Builds"
- Finding: Both phases list it. Phase 2's TC-857 asserts it.
- Recommendation: Keep it in phase 2 only, with the `CardMove.Actor` field and the `handleMoveCard` session read.

### `UnitFiles` retention is described two ways - Severity: Nit
- Affected scope: §7.1 `UnitFiles` row ("**not** emptied by retention") vs §7.2 dedup rule (b) ("when retention emptied `UnitTests`, `Excerpt` or `UnitFiles`")
- Finding: The two sections contradict each other, and §12's `ExpireExcerpts` row empties only `Excerpt` and `UnitTests`.
- Recommendation: Drop `UnitFiles` from §7.2 (b).

### §2 row 6 omits the BR-48 clarification - Severity: Nit
- Affected scope: §2 table row 6
- Finding: The row lists "a clarification of BR-44" only. §0, §10 and the delegation record also list BR-48.
- Recommendation: Add BR-48 to row 6.

### A dedup hit after switch-off contradicts §4.1's promise - Severity: Nit
- Affected scope: §4.1 last sentences; §7.2 step 0 and the "Dedup runs first" paragraph
- Finding: §4.1 says a job that reports after switch-off is ended by "the gate's step 0 … with the switch-off text". But dedup runs before the steps and "no refusal is checked". A failing set that matches an open defect therefore ends DONE `… defect <h8>`, not `unit tests switched off; nothing filed`.
- Recommendation: Either check step 0 before dedup, or reword §4.1 to say a dedup hit after switch-off still updates the open defect.

### A READY `unit:qa` card with an empty reason has no clock - Severity: Nit
- Affected scope: §5.3 ("a drift release may move it to Ready"); §5.5 (a person's unblock clears `BlockReason`); §5.6 stamp and sweep rules
- Finding: A drift release or a board unblock leaves a READY card with an empty reason. The stamp row covers PENDING only. The sweep reads `planned:` or `unit retry since:`, and anything else is "left alone and logged". With GitHub off, that card never ends.
- Recommendation: Extend the `unit-stamp` row to READY with an empty reason, or have the sweep treat an empty READY reason as "stamp now".

### Which mapping the API lookup uses is unstated - Severity: Nit
- Affected scope: §4.2 API table, rows `apiLookupLocked` and `PlanAPICard`
- Finding: `apiLookupLocked` "call[s] `unitQACardLocked`'s two-key … lookup for a mapped request". It does not say whether "mapped" means the handler's `APICardIn.UnitMapped` or the lookup's own `unitQAFor` read under the lock. Only `PlanAPICard` gets the 409 `unit-qa-changed` rule.
- Recommendation: State that the lookup uses `in.UnitMapped`, or that it re-evaluates and answers `unit-qa-changed` on a mismatch too.

### Where the admin cancel of a RUNNING card goes in `MoveCard` - Severity: Nit
- Affected scope: §5.5 last table row
- Finding: `legalMove` refuses Working → Canceled (`board.go:236-237`), and `MoveCard` returns on that refusal at `board.go:351-355`, before any later check. The design says `MoveCard` "calls `resolveUnitQACardLocked(…, admin)` for every `qaToolDriven` card" but not where.
- Recommendation: Say the branch goes before the `legalMove` call: when `mv.Column == ColCanceled && qaToolDriven(&t)`, check `mv.Admin`, then route.

Counts: B=0 M=7 N=9
