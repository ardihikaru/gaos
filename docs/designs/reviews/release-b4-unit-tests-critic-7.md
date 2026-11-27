# Design review: `docs/designs/release-b4-unit-tests.md` (rev 7), critic, structural and style lenses

**Paraphrase.** When a merge lands in a fix-target repository and `UnitQA` is on, GAOS plans a `unit:qa` card instead of `unit:test`. The QA-job runner reads the merge commit and base from GitHub and checks a fresh sandbox attestation. It then has the QA tool run the repository's existing tests. A test or build failure files a deduplicated, capped unit defect. That defect's triage card is held BLOCKED until an auto-fix admin releases it. A later merge where the watched tests pass closes the defect.

**Strongest case against the design.** It rests on two things that do not hold in production. First, it treats "the QA tool is not configured" as `s.qaOut == nil`, which never happens in the shipped binary. Second, it assumes the runner's memory, the excerpt and the six-file packet carry enough state to act safely, and several paths lose that state. Beneath both, the whole feature waits on a QA-tool contract that does not exist yet (OQ-38 is open).

---

### The "no QA tool" premise is false in production, and an unconfigured QA tool crashes the runner - Severity: Blocker
- **Affected scope:** §4.1 (the runner warning), §4.2 (the API `qa-off` row), §5.6 ("No runner"), §5.1 (the "GitHub off" row), §14 (risks).
- **Finding and rationale:**
  - **`srv.qaOut` is never nil.** It is set from `qaOutboundFromEnv()` (`dashboard/main.go:369`). That function always returns a non-nil `*qaOutbound` and records the problem in `why` (`dashboard/qaclient.go:143-164`, `:166-181`).
  - **So the runner always starts.** `if srv.qaOut != nil` at `dashboard/main.go:439` is always true.
  - **Three behaviours never fire.** The "runner is not running" warning, the boot-time no-runner cancel and the `qa-off` refusal for mapped requests (`dashboard/api_tasks.go:234` checks only `== nil`) are dead code.
  - **The crash.** When `why != ""`, `o.base` is nil and `qaOutbound.do` dereferences it at once (`u := *o.base`, `dashboard/qaclient.go:202`). Say an admin enables `UnitQA` and `GAOS_QA_URL` is later unset, or its token file becomes invalid. The first unit start then calls `unitSandbox` → `do`, and the runner goroutine panics. That takes the process down on every boot.
  - **"PENDING for more than 24 hours" cannot be computed.** `TaskRecord` has no creation or status timestamp (`dashboard/model.go:851-896`; only `APIAt`, for API cards).
  - **GitHub off has no end.** With `r.unit == nil`, cards "stay PENDING, untouched" forever.
- **Recommendation:**
  - Define one predicate, `qaUsable := s.qaOut != nil && s.qaOut.why == ""`. Use it in §4.1, §4.2, §5.6 and the `main.go` wiring.
  - Make `unitSandbox` (and `do` for unit calls) refuse when `why != ""`, rather than dereference.
  - Name a timestamp for the 24-hour rule (for example `since:` written at planning, or a new field), and say which component runs the cancel and when (not only at boot).
  - Give GitHub-off cards the same bounded end.
  - Add a TC with `GAOS_QA_URL` unset and `UnitQA` on.

### Adoption after a restart loses `base` and `mergedAt` - Severity: Major
- **Affected scope:** §5.4 (reason grammar, adoption), §6 (poll order), §7.1, §7.4.
- **Finding and rationale:**
  - **What survives a restart.** The RUNNING reason carries only `qa-job`, `case`, `sha`, `mode` and `unit`. Adoption restores only `sha` and `unit`; it re-reads `merged_at` and the base only "when the reason lacks a valid `sha:`".
  - **What goes wrong.** A job adopted with a valid `sha:` and no result yet then calls `PassUnitDefects(…, base="", mergedAt=0, …)`, which matches nothing. It also calls `PlanUnitDefect` with `Base=""`.
  - **The defect it files.** The CaseID becomes `unit:<owner>/<repo>@@<h>` and `UnitBase` is empty. The pre-open gate (`autofixpr.go:90`, §7.5) then refuses its fix forever.
  - **How often.** Any deploy during a job of up to 60 minutes hits this.
- **Recommendation:** Add `base:` and `merged:` to the RUNNING reason, validated by `reasonField`. Alternatively, always re-read the PR on adoption of a unit card that has no `unit:` marker. Extend TC-867 to cover it.

### The source packet is filled first with files the fixer may not edit - Severity: Major
- **Affected scope:** §7.5 (`buildUnitContext` path order; "a unit fix may not weaken the failing tests").
- **Finding and rationale:**
  - **The order.** `paths` puts the failing test files first, and the packet keeps at most `defectMaxFiles` = 6 (`dashboard/defecttriage.go:142`). There is also one shared byte budget (`dashboard/reviewdefect.go:334`).
  - **Why that breaks fixing.** `checkFix` refuses an edit to any file not in the packet (`dashboard/autofixcheck.go:329-331`), and the new rule refuses edits to the failing tests' files.
  - **The outcome.** When the failures span about six test files, or the test files use up the byte budget, the packet holds only files the fixer is forbidden to touch. No fix is possible, and the defect still costs an admin release and a triage turn.
- **Recommendation:** Reserve packet slots and bytes for non-test files (the PR's changed files). Alternatively, put the test files in a read-only "reference" part that does not count against the editable set. State the split and test it in TC-872.

### The `checkFix` unit rule has no defect to read and the wrong data source - Severity: Major
- **Affected scope:** §7.5 ("checkFix … the files of its `Excerpt`"), §12 ("existing tests that change").
- **Finding and rationale:**
  - **No defect parameter.** `checkFix(r FixReply, packet …, af AutoFix, lookup fixLookup)` takes no `Defect` and no kind (`dashboard/autofixcheck.go:275`). Its one caller is `dashboard/autofixpr.go:253`, and about 17 calls sit in `dashboard/fixpr_test.go:125-320`. That test file is not listed.
  - **The Excerpt is a lossy source for the file list:**
    - it is cut at 4 KiB (`defectMaxExcerpt`, `dashboard/defects.go:61`);
    - retention empties it after 30 days (`schema/17-defects.sql:6-7`);
    - it is free text in the form `<test> — <file>`, which a file name containing ` — ` breaks;
    - it can be withheld.
  - **The result.** In each of those cases the guard quietly turns off.
- **Recommendation:** Store the failing files in their own field (for example `UnitFiles`, schema 21, which retention does not empty). Pass a `protected []string` (or the defect) into `checkFix`, and list `fixpr_test.go` and the new signature in §12.

### An emptied `UnitTests` closes the defect on any later run - Severity: Major
- **Affected scope:** §7.1 (`UnitTests` "emptied by the 30-day retention … no longer watched"), §7.4 (`PassUnitDefects`).
- **Finding and rationale:**
  - **The rule as written.** A defect closes when "every one of whose `UnitTests` names is `passed`". For an empty set that is vacuously true.
  - **What follows.** Once retention empties `UnitTests`, the next run of any result closes the defect. It also cancels its held card, which BR-54's amendment allows only "when every failing test … passed".
  - **Two readings.** The prose says the defect is "no longer watched", but the predicate does not exclude it. Two engineers could implement this differently.
- **Recommendation:** State that a non-`@build` defect with empty `UnitTests` is never pass-closed (or is closed by the retention itself, attributed). Add a TC case for it.

### A crash after the marker loses the context and the held-card notice; the store method is unnamed - Severity: Major
- **Affected scope:** §5.4 ("Act once"), §6 poll order steps 3–4, §12 store row.
- **Finding and rationale:**
  - **The gap.** The marker is written inside `PlanUnitDefect` (step 2). Context build (step 3) and the notice (step 4) come after it.
  - **After a restart.** Adoption reads `unit:<h8>` and sets `unitDone = true`, so this job never repeats steps 3–4. A new held defect then has no context. `buildAndStoreDefectContext` now returns early for unit defects (§7.5), so it never gets one, and it has no notice until some later failing run deduplicates it.
  - **Why it matters.** A held card nobody hears about can sit for ever. The design's "a crash can never leave a filed defect without its marker" is true, but the guarantee that matters is the notice.
  - **The unnamed method.** The store call that sets `UnitNoticeSent` has no name. It is not in `Store`, `readOnly`, the conformance table or the §12 store row.
- **Recommendation:**
  - Name the method (for example `MarkUnitNotice(ctx, productID, key)`) and list it on `Store` and `readOnly`.
  - On adoption with a `unit:<h8>` marker, re-run steps 3–4 when `!Context.Built` or `!UnitNoticeSent`.
  - Alternatively, have a periodic sweep send pending held-card notices.

### The "never on in a refused state" invariant is not enforced after saving - Severity: Major
- **Affected scope:** §4.1 (`SetUnitQA` refusals: "so the switch is never on in a state where every failing run is refused"), §4.2 (`unitQAFor`), §7.2.
- **Finding and rationale:**
  - **Checked once only.** `SetUnitQA` checks conditions 1–4 only when it is saved.
  - **The settings can change afterwards.** The Auto-fix form replaces the whole `AutoFix` record (`dashboard/handlers_autofix.go:62-99`, `:144`) and can turn `AutoFix.Enabled` off. Assignments can drop the fixer or the triager. Neither touches `UnitQA`.
  - **The cost.** `unitQAFor` does not check `AutoFix.Enabled` or the assignments, so every merge still spends a sandbox run whose failure is refused by §7.2.
  - **After switch-off.** A RUNNING job "left to finish" can still file a held defect, because §7.2 does not check `UnitQA.Enabled`.
- **Recommendation:** Either add `AutoFix.Enabled` and both role checks to `unitQAFor` (so cards end CANCELED with a reason), or have `SetAutoFix` and the assignment save refuse or turn off `UnitQA`. Also say whether a job that finishes after switch-off may file a defect.

### The operator delegation that approves five rule amendments has no anchor - Severity: Major
- **Affected scope:** §0 ("Delegation"), §10 (D-105 "Accepted under the operator's delegation").
- **Finding and rationale:**
  - **What the policy says.** The policy gives approval to the operator and says implementation starts only after approval (`docs/rules/gaos-design-review.md:34`, `:89-90`).
  - **What the design rests on.** The design lets the agents' own review approve the scope cut and the BR-38, BR-44, BR-54, BR-61 and D-091 (8) amendments, on the strength of a quote.
  - **The only evidence.** That quote appears only in this design and `docs/designs/release-b4b-unit-create.md`; no register, issue or commit records it. This breaks README rule 2 ("Every claim carries a file and a line").
- **Recommendation:** Record the delegation where the operator can be held to it (a decision-log entry or the triage record, with date and channel). Alternatively, mark the amendments as pending operator approval in step 8's summary, and do not mark them accepted in D-105.

### No success metric, and the feature waits on an unbuilt contract - Severity: Major
- **Affected scope:** §0 ("the part that answers 'nothing runs the tests'"), §11, §9 phase exits.
- **Finding and rationale:**
  - **The contract does not exist.** Every phase exit is a GAOS unit test against a fake QA tool. The QA-tool routes are themselves unbuilt: OQ-38 is open at `pustaka/docs/product/open-questions.html:124`, and the design widens it (§10).
  - **So nothing will run in production.** The handler refuses to enable without an attested `unit` capability, so after phase 6 nothing runs any tests.
  - **No measure of success.** No metric says when the problem is solved: for example "share of fix-target merges with a unit result within N minutes" or "held defects released or closed within N days".
- **Recommendation:** Add a success-metric line and a dependency/ordering statement: what ships dormant, what qa-automation-tools must deliver, and how the operator verifies the first real run.

---

### Defect property count drifts - Severity: Nit
- **Affected scope:** §12, `store.go` row ("the five defect properties") vs the `21-unit-defects.sql` row.
- **Finding and rationale:** The schema row creates six properties: `unitSha`, `unitMergedAt`, `unitReopenedAt`, `unitBase`, `unitTests`, `unitNoticeSent`.
- **Recommendation:** Say "six".

### Stale anchors - Severity: Nit
- **Affected scope:** §5.6, §5.2, §4.4.
- **Finding and rationale:**
  - "40-minute budget (`qajobs.go:157`)": line 157 is `lastPoll`; the constant is at `dashboard/qajobs.go:194`.
  - `doneReason` is at `dashboard/qajobs.go:550`, not 552.
  - `Reconcile` starts at `dashboard/runner.go:696`; 705-718 is its loop.
  - `liveIn` is a local map (`dashboard/scheduler.go:231`), not a function.
- **Recommendation:** Re-anchor.

### `ghError.Retry` is never filled today - Severity: Nit
- **Affected scope:** §5.1 ("the wait reuses the existing `ghError.Retry` … filled from `Retry-After` or `X-RateLimit-Reset`").
- **Finding and rationale:** `do` builds `&ghError{Status: …}` and never sets `Retry` (`dashboard/github.go:371-378`). Nothing reads `X-RateLimit-Reset` anywhere.
- **Recommendation:** List filling it in `do` as a change in the §12 `github.go` row.

### Notices on the `unit:qa` card make GitHub calls the bound leaves out - Severity: Nit
- **Affected scope:** §5.6 ("The bound, counted"), §5.7.
- **Finding and rationale:** The job-failed, refusal and retry-ended notices are sent on a PR-keyed card. `loginFor` calls `pullCommitAuthors` for them (`dashboard/notify.go:89-99`), inside the runner loop.
- **Recommendation:** Count these calls, or send all unit notices to the group.

### The board trim misses one render path; the JSON branch is already 409 - Severity: Nit
- **Affected scope:** §7.3.
- **Finding and rationale:** `renderBoardAfter` (`dashboard/board_view.go:272`), which answers every HTMX move, is not among the trimmed handlers. The JSON branch already answers 409 through `refuseCard` (`dashboard/handlers_board.go:31-33`, `:82`); only the HTMX branch (`:74-76`) needs a change.
- **Recommendation:** Add `renderBoardAfter` and reword the 409 sentence.

### README count not in the target prose table - Severity: Nit
- **Affected scope:** §12 target values.
- **Finding and rationale:** `README.md:143` also says "687 in `dashboard/`".
- **Recommendation:** Add that row (687 → 712).

### "Answers medium" is an instruction, not a guarantee - Severity: Nit
- **Affected scope:** §7.5 ("the triager's packet … answers medium … so a person decides").
- **Finding and rationale:** `dashboard/defecttriage.go:505` reads "Answer medium unless the report alone settles it". The model can still answer easy.
- **Recommendation:** Say "is told to answer medium".

### `RecordUpdated` reused for a task reason rewrite - Severity: Nit
- **Affected scope:** §5.5 ("`RecordUpdated` for the in-place rewrite").
- **Finding and rationale:** `RecordUpdated` is the master-record event (`dashboard/fold.go:172-175`). Today's RUNNING → RUNNING rewrite through `ResolvePRCard` emits nothing (`dashboard/prflow.go:601`, `:667-668`).
- **Recommendation:** Either emit nothing, as today, or state why a master-record event names a task.

### Phase 4 re-lists `ResolveUnitQACard` - Severity: Nit
- **Affected scope:** §9 phase 4 "Builds".
- **Finding and rationale:** Rev 7 moved it to phase 1.
- **Recommendation:** Drop it from phase 4.

### The BR-44 amendment is redundant - Severity: Nit
- **Affected scope:** §10.
- **Finding and rationale:** BR-44's only exception is already "an e2e card" (`pustaka/docs/product/brd.html:1304`).
- **Recommendation:** Drop the amendment, or reword it as a clarification.

### `buildReviewContext` has no fetch cap - Severity: Nit
- **Affected scope:** §7.5 ("At most `defectMaxFetch` … reads").
- **Finding and rationale:** It reads every path until six are kept (`dashboard/reviewdefect.go:326-336`).
- **Recommendation:** State that `buildUnitContext` cuts `paths` to `defectMaxFetch` before the call.

### A RUNNING `unit:qa` card cannot be stopped by a person - Severity: Nit
- **Affected scope:** §4.4, §5.5.
- **Finding and rationale:** `legalMove` refuses cancelling from Working: "a live turn cannot be stopped" (`dashboard/board.go:236-237`). Yet this card holds no turn.
- **Recommendation:** State that it is left to its timeout, or allow an admin cancel.

---

**Sections that hold up:**
- **Planner and API key equality (§4.2):** checked against `dashboard/api_cards.go:159-168` and `dashboard/braintown_consumer.go:122-127`.
- **Label count 19 → 20:** `dashboard/labels.go:95-115`.
- **Held-card path table (§7.3):** confirmed against `dashboard/workspace.go:395`, `dashboard/sendback.go:153-159`, `dashboard/defects.go:363-380` and `dashboard/designgate.go:793-816`.
- **TC arithmetic (§9, §12):** 25 cases and 712 → 737 tests are consistent; TC-850 onward is free in the register.

Counts: B=1 M=8 N=12
