# Readiness review: `docs/designs/release-b4-unit-tests.md` (rev 6)

I checked the design's claims against the code on `main`. The register identifiers, the counts and most anchors hold. Six Major findings would each make an implementer stop and choose, or would fail a stated exit condition. There are no Blockers.

**What I confirmed:**
- **Identifiers are the next free values.** The decision log ends at D-104 and says "One hundred and three entries" (`decision-log.html:18`, `:2153`). The BRD ends at BR-63 and says "Sixty-three rules" (`brd.html:1218`). The QA plan ends at TC-849 (`qa-test-plan.html:940`). The highest schema file is `20-release-b.sql`, and `schema_embed.go:22` embeds `schema/*.sql`, so `21-unit-defects.sql` is picked up. There are 19 labels (`labels.go:95-115`), so 19 → 20 is right.
- **The counts follow.** TC-850…TC-874 is 25 cases (3+4+5+7+5+1). 712 Go tests + 25 = 737. The baselines match `check-workspace.sh:113-133` and `README.md:143`. The `CloseDefect` callers listed in §12 are the complete set.
- **Key anchors are correct.** `prflow.go:88-91`, `labels.go:179-188`, `main.go:439-440`, `main.go:490-491`, `qajobs.go:350/355-361/380/465/499`, `board.go:330/452/479`, `prflow.go:600-606`, `defecttriage.go:78/142/262/422/456`, `defects.go:59/61/288/347/590/612`, `contacts.go:67`, `staging.go:186` and `open-questions.html:923-931`.
- **`qaToolKey` covers every key the runner drives today.** Brain Town keys end `:e2e-automation` or `:e2e-run` (`braintown_consumer.go:122-127`); the other prefixes are `qa-run/` (`api_cards.go:153`), `staging-create/` (`stagingcreate.go:23`) and `qa-merged/` (`mergedrun.go:18`).
- **Sections with no findings:** §7.1, §7.2, §7.4, §11, §13, §14, and the §9 verification commands.

---

### The scheduler's lease cannot see a card's key or reason - Severity: Major
- Affected scope: §4.4 (`liveIn` skips tasks for which `qaToolDriven` holds); §12 row `dashboard/scheduler.go, dashboard/board.go`
- Finding: `qaToolDriven(t *TaskRecord)` reads `DedupKey` and `BlockReason`. But `liveIn` (`scheduler.go:231-239`) loops over `SchedulerInput.Tasks []TaskNode`, and `TaskNode` (`scheduler.go:90-105`) has neither field. `TaskNode` values are built in `store_mem.go:2378-2384`, and `store_mem.go` is not in the change list. To build this, an implementer must either add `DedupKey` and `BlockReason` to `TaskNode`, or add a precomputed `QADriven bool` filled in by the builder. That changes the pure scheduler's input either way, and the design does not say which. It also does not say whether `liveAgentSlots` (`store_mem.go:2410-2436`), which marks an agent of the card's role busy for every RUNNING task, should skip these cards too.
- Recommendation: Pick one shape, for example `TaskNode.QADriven bool` set at `store_mem.go:2378` from `qaToolDriven(&t)`. Add `dashboard/store_mem.go` to §12, and state the `liveAgentSlots` rule.

### `ResolveUnitQACard` has no stated way to make its moves - Severity: Major
- Affected scope: §5.5 (the table, "`resolveCardLocked` refuses every target … so neither `ResolvePRCard` nor `ResolveUnitQACard` can move a held triage card"); §4.1 switch-off
- Finding: The sentence implies `ResolveUnitQACard` goes through `resolveCardLocked`. That function only allows targets listed in `resolveFrom` (`prflow.go:600-606`, `638-645`). The table has no READY or CANCELED entry, and the design says it is not widened. So BLOCKED → READY and PENDING/READY/BLOCKED → CANCELED cannot go through `resolveCardLocked`.
  - The design does not name the primitive to use instead (`moveTaskLocked` with `statusPath`? `canMoveTask` with `moveTaskLocked`, as `MoveCard` does at `board.go:390-394`?).
  - It does not say whether BLOCKED → READY clears `BlockReason`, as a board unblock does (`board.go:397-401`).
  - It does not say where the "held" refusal sits for this method.

  Two engineers would build this differently.
- Recommendation: Specify the body of `ResolveUnitQACard`:
  - its own from/to table;
  - the move primitive, with `statusPath` and `moveTaskLocked(id, step, rule)`;
  - what happens to `BlockReason` on each move (BLOCKED → READY sets `""`);
  - an explicit `ReasonUnitHeld` check inside it, rather than relying on `resolveCardLocked`.

### Three phases use code that a later phase builds - Severity: Major
- Affected scope: §9 ("Each phase uses only its own and earlier phases' code"), the phase table and the phase exits
- Finding:
  - (a) Phase 1's `SetUnitQA` switch-off cancels "through the same transition as `ResolveUnitQACard`", and its exit asserts the cancel. `ResolveUnitQACard` is a phase 4 item.
  - (b) Phase 2's API age check `unit-qa-too-old` (tested by TC-855) needs `ghPR.MergedAt`. The `github.go` changes are listed in phase 4.
  - (c) Phase 3's `UnitDefectIn.Failing []qaUnitTest` uses the `qaUnitTest` type, which is defined with the `qaJob` changes in `qajobs.go` (§6, phase 4).

  Each phase would fail to compile, or fail its own exit check, in the order written.
- Recommendation: Move the shared CANCELED transition (or all of `ResolveUnitQACard`) into phase 1. Move `ghPR.MergedAt` (and `MergeCommitSHA`) into phase 2. Define `qaUnitTest` in phase 3, in `unitdefect.go`.

### The 409 for a held close and an HTMX move is not what the handlers return - Severity: Major
- Affected scope: §7.3 (`CloseDefect` refusal "409"; `MoveCard` "409"); §9 phase 5 exit ("a non-admin's unblock, cancel and close answer 409")
- Finding: `handleCloseDefect` passes every store error to `autoFixError` (`handlers_autofix.go:165-167`). That function answers 422 for a JSON request and `s.fail` for an HTMX one, and `s.fail` also writes 422 (`handlers_autofix.go:102-112`, `main.go:729-737`). A refused HTMX board move also goes through `s.fail`, so it answers 422 too (`handlers_board.go:74-76`). As written, the phase 5 exit cannot pass for a close, or for a move made from the HTMX board. The implementer has to choose between changing the handlers and changing the expected codes.
- Recommendation: Either state the handler change (for example, map this refusal to 409 in `handleCloseDefect` and in the HTMX branch of `handleMoveCard`, and add it to §12), or change the stated status to 422 for those paths and to 409 only for a JSON board move.

### The API answers the wrong `dispatch` value for a `unit:qa` card - Severity: Major
- Affected scope: §4.2, external-tasks API table; the published contract text
- Finding: `apiDispatchAnswer` (`api_tasks.go:99-111`) answers `qa-tool` only for `e2e:*` labels. For a `unit:qa` card it would answer `scheduler` (with the runner on) or `operator`. The published text says dispatch "is not available" for this card, so the API would contradict its own contract. The function is not in the §4.2 table or in §12.
- Recommendation: Add a row: `apiDispatchAnswer` answers `qa-tool` for `LabelUnitQA`, or better, for `qaToolKey(t.DedupKey)`. Cover it in TC-855.

### The API's own-card check refuses a retry after the switch flips - Severity: Major
- Affected scope: §4.2, `apiLookupLocked` row ("call `planUnitQASpecLocked`'s two-key lookup for a mapped request")
- Finding: Before any key lookup, `apiLookupLocked` finds the client's own card by `ref` and answers 409 `externalId-reused` when `t.DedupKey != key` (`api_cards.go:254-259`). Suppose a client created a `unit-test` task while `UnitQA` was off (key `…:unit-test`), and retries the same `externalId` after it was turned on (key `…:unit-qa`). The retry would get 409 instead of its own card. The reverse case behaves the same. The design specifies the two-key rule for adoption but not for this branch.
- Recommendation: State that for a mapped `unit-test` request the own-card check accepts either key (`:unit-qa` or `:unit-test`) and answers `APIOwn`. Add the case to TC-855.

### Missing and explicit `dispatch` are merged before the handler can tell them apart - Severity: Nit
- Affected scope: §4.2, `apiTaskCreate` row
- Finding: `validateTaskBody` folds the field into one bool (`Dispatch: b.Dispatch == nil || *b.Dispatch`, `api_tasks.go:57`) before the handler reads the master (`api_tasks.go:181`). After that it cannot tell "missing" from `true`. The design does not say where the three-state value is kept.
- Recommendation: Say that `APICardIn` gains `DispatchSet bool` (or keeps `*bool`), and that the mapped check reads it.

### The release version and `toc.js` semver are not set - Severity: Nit
- Affected scope: §12 last row ("the release note")
- Finding: The latest release is v0.66.0 (`changelog.html:39`, `pustaka/docs/assets/toc.js:22`). The design names neither the next version nor `pustaka/docs/assets/toc.js`, which the README semver check reads (`check-workspace.sh:138`).
- Recommendation: State the version (for example v0.67.0, minor) and add `pustaka/docs/assets/toc.js` to §12.

### "The scheduler lease tests" is a description, not a path - Severity: Nit
- Affected scope: §12 "existing tests that change"
- Finding: No file is named; the candidates are `workspace_test.go:150` and `workflow_dispatch_test.go:203/248`. It is also not stated whether the change edits existing functions or adds a new `func Test…`. A new one would break the Go-test target in §12, which `check-workspace.sh:113` counts.
- Recommendation: Name the file and the function, and state "no new test function", or adjust the counts.

### Go test counts need their word form for `product.js` - Severity: Nit
- Affected scope: §12, target prose table
- Finding: `check-workspace.sh:114` greps `product.js` for the words ("seven hundred and twelve"). The table gives only digits for Go tests at each phase.
- Recommendation: Add the word form per phase (for example "seven hundred and fifteen" … "seven hundred and thirty-seven").

### The capability answer's shape and failure cases are not pinned - Severity: Nit
- Affected scope: §5.3, §11
- Finding: Today's answer is `{"capabilities":[…]}` (`qaclient.go:78-80`). The design does not give the JSON key of the `sandbox` object. It also does not say how a non-404 4xx or an undecodable body is classed: retry, or BLOCKED `ReasonUnitAttest`.
- Recommendation: State `{"capabilities":[…],"sandbox":{…}}`, and that any non-5xx answer without a valid `unit` and `sandbox` counts as "an answer without `unit`".

### A few anchors have drifted - Severity: Nit
- Affected scope: §4.1, §3 step 1
- Finding: `ReviewCap` is at `model.go:520`, not 516. `handleSaveAutoFix` starts at `handlers_autofix.go:116`, not 117. `withRepoKind` is defined at `repokinds.go:31`; `prflow.go:225` is only where it is called.
- Recommendation: Re-anchor these.

Counts: B=0 M=6 N=6
