# Readiness review: `docs/designs/release-b-e2e-kinds-merge-unit-tests.md`

The document is not ready to implement: it has 2 Blockers, both in Part 4 (unit tests). Several of its round-1 fixes are also left half-decided in ways only the code shows.

Design reviewed: `/home/ardi/devel/projects/ardihikaru/my-projects/gaos-release-b/docs/designs/release-b-e2e-kinds-merge-unit-tests.md`. I checked its claims against `dashboard/` and the three registers. Claims about the QA tool (`QA:` paths) are treated as stated assumptions.

**What checks out:**
- **Allocated IDs are correct and next-free.**
  - TC-819…833: `qa-test-plan.html` ends at TC-818.
  - D-103 and D-104: the decision log ends at `d-102`.
  - BR-62: the BRD ends at BR-61.
  - The schema file `schema/20-unit-defects.sql`: the last one is `19-review-defects.sql`.
  - The label count of 17 matches `labels.go:87-105`.
  - The version v0.65.0: the changelog's newest entry is v0.64.0.
- **The §1 anchors resolve:** `qajobs.go:157,193,232-255`, `prflow.go:80,178,206,543`, `fixprpoll.go:319`, `sendback.go:153`, `board_page.go:324`, `defects.go:288`.
- **Notice kind:** `unit:failure` is already allowed (`contacts.go:67`).

### The `unit:qa` card has no carrier for its SHA or `create_for`, and no start or result path in the runner - Severity: Blocker
- Affected scope: §5.2 (lines 110-113); §7 B4 rows for `qajobs.go` and `qaclient.go`; `qajobs.go:204-303`
- Finding:
  - **The runner only sees what the card stores.** The QA runner starts work from a `TaskRecord` (`Tick` → `start(ctx, m.ID, c, mode)`, `qajobs.go:216-224`). A `pr.merged` card's key is `owner/repo#n:unit-test` (`braintown_consumer.go:122-127`). The key has no SHA in it.
  - **The SHA is lost after planning.** §5.2 puts `merge_commit_sha` into `PREvent.Commit` at planning time, but never says where the card keeps it so `POST /gaos/unit {sha}` can send it later. A new `TaskRecord` field (and its ArcadeDB property), the reason prefix, or something else would each be a different implementation.
  - **`create_for` has no stated home.** It is not said whether it is computed at planning time (in `prFlow.handle`, which has `f.gh`) or when the job starts (in the runner, whose `src prSource` only has `pullRequest` and `pullFiles`, `prflow.go:385-388`). Nor is it said where it is stored.
  - **No GitHub call lists a directory.** The "reads the directory at the merge SHA (contents API)" step has no matching client method: `ghClient` has `fileAt` (`githubwrite.go:83`), which reads one file. The B4 `github.go` row only mentions `merge_commit_sha`.
  - **The job's start and result are undefined.**
    - `start()` always calls `caseCodeFor` and then `startAutomation` (`qajobs.go:283-290`). Nothing defines the `unit` branch.
    - `qaJob` has no `{tests, failed, skipped, failures}` fields.
    - Nothing separates "the job failed" from "the tests failed".
    - Nothing decides whether `fail()` still sends `e2e:failure` (`qajobs.go:405`) for a unit card.
- Recommendation: Add a §5.2 subsection "How a `unit:qa` card starts and ends". It should name:
  - the field that keeps the SHA and `create_for` (and its schema line);
  - where `create_for` is computed, with the new `ghClient` method and interface method by name;
  - the `start` branch for a `unit:qa` card;
  - the `qaJob` fields and how `final()` maps to the four outcomes;
  - the notice kind used for an infrastructure failure.

### "D-091 runs unchanged" is false: a unit defect gives the triager no source code - Severity: Blocker
- Affected scope: §5.3 (line 117); §0 decision 2
- Finding:
  - **The context builder searches by `Operations` only.** `buildDefectContext` (`defecttriage.go:294-337`) finds candidate files by running `searchCode` for each entry of `d.Operations`. It never reads `d.Files`, which is the verdict's field (`17-defects.sql:34`).
  - **A unit defect has no `Operations`.** §5.3 sets `Files` to the failing tests' files and sets no `Operations`. The source slice would hold only the repository heads, so the triager reads no code.
  - **So the operator's second decision does not work as written.** "A unit-test failure becomes a defect … through the D-091 pipeline" fails at the context step.
- Recommendation: Decide how a unit defect gets context. Two options:
  - set `Operations` from the failing test names or the files' base names, with a stated rule and cap; or
  - change `buildDefectContext` to read `Files` directly, which adds `defecttriage.go` to B4 and needs a test.

  Then drop "unchanged".

### The `unit` capability cannot be known where the planning decision is made - Severity: Major
- Affected scope: §5.2 "Decided at planning" (line 110); §2.3 (line 63); §2.2 (line 59)
- Finding:
  - **The planner cannot see the capability cache.** The switch between `unit:test` and `unit:qa` is made inside `prCardsFor` / `PlanPullRequestCards`. That is a `MemStore` method under the store lock (`prflow.go:206-218`), with no access to `qaOutbound`.
  - **The cache may be empty at planning time.** §2.3 says capabilities are read "when a job starts". After a restart, or before any job has run, the cache is empty when a `pr.merged` card is planned.
  - **`apiSpec` has no product to pass.** §2.2 says `apiSpec` passes the product to `prCardsFor(ev, m)`, but `apiSpec(productID, in)` (`api_cards.go:171`) has no `Master`.
- Recommendation: State how the capability reaches the planner: a `PREvent` field filled by `prFlow.handle`, or a store parameter. State what an empty or cold cache means (fetch now, or treat as "not listed"). Name `apiSpec`'s new signature and its callers.

### `unit:qa` shares a family with `unit:test`, so a hand edit can switch a card's driver - Severity: Major
- Affected scope: §5.2 (line 110); §2.2 "`SetTaskLabel` refuses the family"
- Finding:
  - **The existing refusals work by family.** `SetTaskLabel` refuses whole families (`prflow.go:540-545`).
  - **Within a family, a new label replaces the old one** (`prflow.go:555-568`).
  - **The rule for the mixed family is not decided.** §5.2 puts `unit:qa` in family `unit` as "planner-only". Refusing the whole family would also block hand-setting `unit:test`, which is allowed today. Refusing only the label still lets an operator set `unit:test` on a `unit:qa` card, which replaces it and turns a QA-driven card into an agent-dispatched one. That contradicts "keeps one driver for its life".
- Recommendation: Either give `unit:qa` its own family, or state the exact rule: refuse setting `unit:qa`, and refuse any `unit:*` change on a card that already holds `unit:qa`. Name the test.

### The change list leaves out files the changes need, and tests have no file paths - Severity: Major
- Affected scope: §7, B1 to B4 tables
- Finding:
  - **`SetAssigned` is also declared in `store.go`.** It is in the `Store` interface (`store.go:201`) and the `readOnly` stub (`store.go:511`). B1 changes its signature but does not list `store.go`.
  - **The editor's view needs `view.go`.** The editor's data lives in `view.go` (`AgentAssign`, `view.go:171`, built in `handlers_assign.go:64-92`), which is not listed.
  - **Reset is not decided for kinds.** The `reset` action (`handlers_assign.go:116-117`) clears the assignments, but the design does not say whether it also clears `RepoKinds`.
  - **Tests have names but no files.** No row says which `*_test.go` gets each case, so a write allowlist cannot be built.
- Recommendation: Add `store.go` and `view.go` to B1. Decide what reset does to kinds. Give each TC its test file path.

### The `qa-merged/` card's cap is undefined, and the cap formula contradicts it - Severity: Major
- Affected scope: §4.2 (line 95); §3.1 step 4 (line 75)
- Finding:
  - **Two statements disagree.** The run-after-merge card is "counted with the creates' cap". Step 4's formula counts only "open creates".
  - **What happens at the cap is not said.** If the cap is full when the poller sees the merge, the run might be skipped forever (the row is already merged, and `pollRow` stops on final rows, `fixprpoll.go:320`), retried, or planned anyway.
- Recommendation: State whether `qa-merged/` cards count toward `GAOS_E2E_CREATE_OPEN`, and what a capped merge does. "Always planned, never capped" is one simple choice. Add a test for it.

### A job adopted after a restart loses its case code, so its merged spec never runs - Severity: Major
- Affected scope: §4.2; §3.2; `qajobs.go:279-281`, `qajobs.go:412-423`
- Finding:
  - **Adoption skips the case code.** A job picked up after a restart is tracked with `caseCode` empty (`qajobs.go:280`).
  - **The row is then written without a case.** `recordQARow` records `QACase: ""`. §4.2 only plans the follow-up run when the row "carries a case code".
  - **The effect:** a staging create in flight during a restart produces a spec pull request that merges and is never run.
- Recommendation: State that adoption recomputes the case code from a `staging-create/` (or `qa-merged/`) key using `caseCodeFor`. Add a restart test.

### The rule that blocks duplicate creates misses two common cases - Severity: Major
- Affected scope: §3.1 step 3 (line 74)
- Finding:
  - **Merged before the catalog catches up.** After a spec merges, the row is final and the card is Done, but `QaCase.Implemented` stays false until the next catalog sync. A deploy in that window passes step 3 and plans a second create for the same case.
  - **Closed without merging.** A spec pull request that a person closes without merging also stops blocking. Every later deploy then plans `#n+1`, with no stated limit.
- Recommendation: Make a Done card with a **merged** row block until the catalog shows `Implemented`. Decide what a **closed** row means: treat it like Failed, with the 7-day wait, or block until a person releases it. Test both.

### A unit test that keeps failing creates a new held defect on every merge - Severity: Major
- Affected scope: §5.3 key `<productId>/unit:<owner>/<repo>@<sha12>`
- Finding:
  - **The defect key is per SHA.** Every merge runs the existing tests (§5.1), so one broken test makes a new defect, and a new Blocked triage card, on each merge to the repository.
  - **Nothing merges or supersedes them.** D-091 has a `supersededBy` field (`17-defects.sql:37`), but the design does not use it.
- Recommendation: Key by repository plus the set of failing tests, or supersede the repository's open unit defect with the new one. State the rule and test it.

### BR-08's Minimum band is declared "unchanged" without settling whether QA-tool auto-merge is allowed in it - Severity: Major
- Affected scope: §0 decision 1; §6 (line 124); `brd.html:1240`
- Finding:
  - **BR-08 forbids auto-merge in the Minimum band.** Its text: "*Minimum* … forbids auto-merge".
  - **The guard that respected it is gone.** D-102 (5)'s proposal had "refused in the Minimum band" (`decision-log.html:2144`). The new path auto-merges spec pull requests for a GAOS product with no band check, yet calls BR-08 unchanged.
  - **Implementers would split here.** One engineer would add a band check (none exists, since GAOS does not control the QA tool); another would ignore it.
- Recommendation: State in D-103 whether BR-08 covers the QA tool's auto-merge. If it does, say how a Minimum-band product stops it: for example, GAOS omits an `auto_merge` flag on `create`.

### Several behaviours have no test - Severity: Major
- Affected scope: §7 test rows against §2.2, §2.3, §3.2, §5.2, §5.3
- Finding: These behaviours map to no named TC:
  - the §3.2 Done reasons, including the `auto-merge` variant;
  - the capability cache (10 minutes; last known set kept on error; a 404 is the empty set);
  - `SetTaskLabel` refusing `e2e-kind:*` and `unit:qa`;
  - the `unit-no-merge-sha` failure;
  - the `not a fix target` branch;
  - the `waitsOnAPerson` filter change;
  - the ArcadeDB round trip of `repoKinds` in B1.
- Recommendation: Map each to a TC, or extend an existing one with named assertions.

### The `(kind not sent …)` text is overwritten on the next status change - Severity: Nit
- Affected scope: §2.3 (line 64)
- Finding: A card's reason is rewritten at each transition (`qajobs.go:299`, `:335-336`, `:362`), so the suffix disappears.
- Recommendation: Say which reason carries the suffix (for example, the final Done or Failed reason), or record it as an event instead.

### The kind label on a `qa-merged/` card has no effect - Severity: Nit
- Affected scope: §4.2
- Finding: A single-case run ignores `kind` (§2.3), so copying the create card's label onto the run card does nothing and needs an extra lookup.
- Recommendation: Drop it, or say it is only for display.

### `PlanStagingCards` has an unexplained `max int` parameter, and the 409 body is not given - Severity: Nit
- Affected scope: §3.1 (lines 70, 77)
- Finding: `max` is not defined. A 409 that "still plans creates" does not say whether its body includes `creates`.
- Recommendation: Define `max`, or remove it. Give the exact 409 body.

### Broadening the "Waiting on a person" filter also catches design-gate holds - Severity: Nit
- Affected scope: §5.3, last sentence
- Finding: `operatorHolds` also covers `ReasonDesignBlocked` and `ReasonDesignUnreviewed` (`sendback.go:155`). The filter change would quietly add those cards too.
- Recommendation: Say so explicitly, or list only the reasons that should be included.

### The `create_for` matching and selection rules are loose - Severity: Nit
- Affected scope: §5.2 `create_for` (line 112)
- Finding:
  - "Starts with the source file's base name" lets `router_test.go` count as covering `route.go`.
  - When more than 10 files qualify, the order that picks the 10 is not stated.
- Recommendation: Define base name as the name without its extension, followed by a separator (`_`, `.`, `-`). State the order used to pick the 10 (for example, the order of `pullFiles`).

### Ambiguous subject and loose anchor in §1 and §5.3 - Severity: Nit
- Affected scope: §5.3 ("It is planned only when…"); §1 bullet 5
- Finding:
  - It is unclear whether "It" means the defect or the triage card, and which card carries `not a fix target`.
  - BR-42 (`brd.html:1302`) does not say "GAOS reads no folder".
- Recommendation: Name the subject. Re-anchor or reword the BR-42 claim.

### Register edits are described, not given as paths - Severity: Nit
- Affected scope: §7, "Registers per phase" (line 170); §3.1 OQ-42 sentence
- Finding:
  - "The changelog and version", "README counts" and "the TC rows" have no repository paths.
  - The files that should be named are `pustaka/docs/changelog.html`, `pustaka/docs/product/{brd,decision-log,open-questions,qa-test-plan}.html` and `pustaka/docs/integrations/pr-agents.html`, plus the version's file.
  - OQ-42's new status (answered or narrowed) is not stated.
- Recommendation: List exact paths per phase and give OQ-42's target status.

Counts: B=2 M=10 N=7
