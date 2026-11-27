# Zoom-out review: Release B (e2e kinds, create-then-run, guarded spec merges, unit tests through the QA tool)

Reviewed: `/home/ardi/devel/projects/ardihikaru/my-projects/gaos-release-b/docs/designs/release-b-e2e-kinds-merge-unit-tests.md` (Draft rev 1)

**Paraphrase.** Release B adds four phases on top of D-102's assignments:
1. Each repository is tagged with an e2e kind, and that kind is sent to the QA tool.
2. A staging deploy plans "create" cards for catalog cases that have no automation yet, then plans the run.
3. GAOS merges the QA tool's approved spec pull request when a list of guards passes.
4. Unit tests run through a new QA-tool executor mode, and a failure becomes a held `unit:fix` card.

**Claims I checked against the code and they hold:**
- labels 17 → 20 (`labels.go:87-105`)
- `qajobs.go:64`, `:204-227`, `:274`, `:279-281`, `:332-336`
- `staging.go:196`
- `qacatalog.go:81-105`
- `ghTokenKind` at `githubwrite.go:123`
- `operatorHolds` at `sendback.go:153`
- `ReadinessBand` at `model.go:729`
- `unmetDeps` at `scheduler.go:379`
- `pullFiles` exists (`github.go:228`)
- D-102 (5) exists and lists the amendments the design makes (`decision-log.html:2144`)

---

### The "safe default" kind drops API coverage on every unconfigured repository - Severity: Blocker
- Affected scope: §2.1 (the "no entry is `fullstack`" sentence and the table), §2.3 (`run` with `*` runs only the kind's levels)
- Simplicity rationale: The premise is that "the larger test is the safe default". But the two level sets in the table do not overlap: `be` = {API, GQL} and `fullstack` = {WEB, E2E}. So `fullstack` is not larger than `be`.
  - Today a staging run asks for `*` and runs every automated case (`qajobs.go:238-240`).
  - Once the QA tool lists `kind`, every repository without an entry gets `kind: fullstack`, so its API and GQL cases quietly stop running on staging.
  - That is a regression nobody configured. The B1 exit test only checks the `be` side, so it would not catch it.
  - It also contradicts the kind's own name: "frontend plus backend" should include the backend levels.
- Recommendation: Make `fullstack` = {API, GQL, WEB, E2E} for `run`, and keep it limited to `tests/web/` only for `create`. Or simpler still: a repository with no entry sends no `kind`, which is exactly today's behaviour. Add a B1 exit check that a repository with no entry still runs API cases.

### The create-card dedup key is per repository, but the case belongs to the product - Severity: Major
- Affected scope: §3.1 steps 2-3
- Simplicity rationale: Step 2 de-duplicates open create cards across the whole product ("any repository"). Step 3 keys them per repository (`staging-create/<owner>/<repo>:<caseID>`). Two problems follow:
  - **Duplicate specs.** With merge off (the default), a create card ends Done as soon as its pull request is open (§3.3). The case stays `!Implemented` until a person merges and the catalog syncs. A deploy of a second repository of the same kind then gets a new key and creates the same case again. The QA tool pushes `qa-auto/<code-lower>` and never forces it (§1), so the second job collides with the first branch, or opens a second spec.
  - **No retry.** `taskByDedupKeyLocked` (`prflow.go:168-175`) matches a card in any status. So a create that failed (`qa-job-failed`, `qa-approval-timeout`) blocks that repository and case for ever, and the design never says how it gets retried.
- Recommendation: Key by product and case (`staging-create/<product>:<caseID>`). Write one rule for each terminal outcome:
  - a failed card may be re-planned after a cool-down, or by a person;
  - a card that ended Done with its pull request open blocks re-creation until the catalog shows `Implemented`.

### The run's dependencies and hold are mostly dead machinery; an existing single-case run would do the job - Severity: Major
- Affected scope: §3.1 step 4 (`Deps`), §3.2 (`GAOS_E2E_RUN_HOLD`), §0 ("create it first, merge it, and run it")
- Simplicity rationale: The run holds for at most 30 minutes after it is planned. The numbers show when that hold can ever help:
  - **Merge off (the default):** a create ends as soon as its pull request is open, and the spec is not on the default branch. The hold delays the run and adds no case to it.
  - **Merge on:** the create needs generation and repair, then a person's approval (up to 24 hours), then checks (up to 30 more minutes, `GAOS_E2E_MERGE_WAIT`). The merged spec only reaches the run if all of that fits inside 30 minutes of the deploy.

  So the stated goal, create then run, is almost never met. The cost is a new rule in `Tick`, a new setting, and `Deps` on cards that never reach the scheduler. The B2 exit test does not check that the run includes the new spec, so the gap would go unnoticed.
- Recommendation: Plan the create cards and the run independently, with no `Deps` and no hold. When a spec merges in B3, plan a single-case run of that case. The key family for that already exists: `qaRunPrefix` (`api_cards.go:151-153`) and `qaRunCode` (`qajobs.go:442`). Either drop `GAOS_E2E_RUN_HOLD`, or state the scenario it actually serves.

### The merge wait runs into the runner's 40-minute active timeout - Severity: Major
- Affected scope: §4.4
- Simplicity rationale: `poll` adds time to `t.active` whenever the job is not in the approval wait (`qajobs.go:314-318`). It fails the card with `qa-job-timeout` once `active > 40m` (`qajobs.go:155-158`, `:371-375`).
  - The design keeps the same tracked job "Running … merging" for up to 30 minutes after the job succeeds.
  - A create whose generation and repair took more than 10 active minutes would therefore end Failed in the middle of the merge.
  - That breaks the §4.4 promise that "every outcome is Done". The design does not say whether the merge phase is tracked by the job map or by something else.
- Recommendation: When the job succeeds, remove it from `r.jobs`, as today. Then drive the merge from the card's own `merging since <unix>` reason, in a separate pass that has its own timer. That removes the interaction and still survives a restart.

### No alternatives section, and the cheaper merge option is not weighed - Severity: Major
- Affected scope: the whole document (README rule 5), §4, §8
- Simplicity rationale: B3 adds a new write power to GAOS. It needs a new token file and a new client, and it amends the BRD out-of-scope line, the scope boundary and D-091 (2).
  - §8 itself argues that a lying QA tool "would need to name … exactly what it is allowed to produce". In other words, the guards only confirm what the QA tool already controls.
  - An option that keeps GAOS's boundary intact is never stated: the QA tool, which already holds the push credential and opens the pull request, merges its own approved spec. It could do this with `gh pr merge --auto --squash` behind branch protection that requires checks in the automation repository.
  - That moves no rule and adds no GAOS token.
  - D-102 (5) proposed "GAOS merging", but a proposal is not an evaluated choice.
- Recommendation: Add an Alternatives section with at least three options:
  - QA-tool self-merge, or GitHub auto-merge on the automation repository;
  - the GAOS-merge design as written;
  - "a person merges", which is today's default.

  For each, give the reason for choosing or rejecting it, and the rule amendments it needs. Then let the operator choose.

### `unit:fix` rebuilds the defect pipeline - Severity: Major
- Affected scope: §5.2, the "Run failed" bullet and the new label
- Simplicity rationale: A failing test from the QA tool, turned into a held fix card with the failure as context, is what D-091 intake already does:
  - `ReportDefects` (`defects.go:382-384`);
  - text that is stripped, capped and screened (`stripBR53`, `screenDefectText`, `defects.go:248-298`);
  - a triage card, caps and state tracking;
  - a GAOS-originated defect kind already has a precedent: `DefectKindReview` (`defects.go:40-42`).

  The design instead adds a new label, a new block reason, a new `operatorHolds` entry, a new board-filter special case, a new notice and an `Artifact` context path. It also never says that the QA tool's failure summary, which is untrusted text, is screened before an agent reads it.
- Recommendation: Report a unit failure as a defect of a new kind, `unit`. Its key is the repository plus the SHA, since there is no QA case ID. Then reuse the triage, the caps and the screening. If the defect path really does not fit, say why in the Alternatives section. In any case, require the same strip and screen for the context part.

### A per-product switch is decided at dispatch time, inside a labels-only function - Severity: Major
- Affected scope: §5.2, first bullet
- Simplicity rationale: `labelsAllowDispatch(labels []string)` (`labels.go:167`) takes no product. It is called from `scheduler.go:286` and `apidispatch.go:47`.
  - Making it "refuse `unit:test` on that product" changes the signature and threads `Master` into both callers.
  - It also means a card's driver (an agent or the QA runner) changes if an admin flips `UnitTestsViaQA` while the card is open. Both drivers could then touch the same card.
- Recommendation: Decide at planning time. When the switch is on and the `unit` capability is present, plan the card with a label that is already non-dispatchable, or add one QA-driven unit label to the closed set in `labelsAllowDispatch`. `isE2ECard` then becomes `isQACard`, and a card keeps one driver for its whole life.

### The mode choice is undecided for a mixed pull request - Severity: Major
- Affected scope: §5.2, "Which mode"
- Simplicity rationale: The rule reads "if a changed source file has a test file beside it … `run`, else `create`". It does not say what happens when some changed files have tests and others do not. Two engineers would build this differently (README rule 6).
- Recommendation: Always `run`. Add `create` only for the changed source files that have no test beside them, or state plainly that one covered file means `run`.

### No change list, so the size cannot be computed - Severity: Major
- Affected scope: §6, §7 (README rules 3-4)
- Simplicity rationale: The design names files only in passing:
  - code: `githubmerge.go` (new), `qajobs.go`, `staging.go`, `prflow.go`, `labels.go`, `api_cards.go`, `sendback.go`, `board_page.go`, `model.go`, the Agents handler, the Arcade persistence for `repoKinds`, and the `AutoFix` form;
  - documentation and registers: the BRD, the decision log, the test plan, and `agents.html`.

  "TC-819 onward, one per `func Test…`" gives no number. I count at least 13 files from the text, but I cannot derive a test count or a per-phase size. A four-release plan cannot be signed off without them.
- Recommendation: Add a per-phase change table (file, change, new tests), with the TC range for each phase.

### The kind vocabulary forks one that already exists - Severity: Nit
- Affected scope: §2.1, §2.3
- Simplicity rationale: The QA tool already reports `automationType` as `be|e2e`: `defects.go:214`, and the DDL regexp in `schema/17-defects.sql:18`. The design invents `be|fullstack` for the same idea.
- Recommendation: Use `be|e2e`, or record why it is different.

### `merge-facts` does not need a capability, and 10-minute polling is premature - Severity: Nit
- Affected scope: §2.3, §4.2
- Simplicity rationale: Whether `pushed_sha`, `pr_author` and `spec_path` are present in the job status already tells GAOS whether a merge can be attempted. If they are empty, no merge. A background refresh loop is not needed either: the capability can be read lazily when a job starts, with a short cache.
- Recommendation: Drop `merge-facts`. Read capabilities when a job starts, cached for about 10 minutes.

### The kind label and `RepoKinds`: which one decides - Severity: Nit
- Affected scope: §2.2, §2.3
- Simplicity rationale: The design does not say whether the `kind` sent when the job starts comes from the card's label or from the current `RepoKinds`. They disagree if an admin edits a kind between planning and start.
- Recommendation: State that the label decides, because it was set at planning time.

### Generalise the "Waiting on a person" filter instead of special-casing `unit:fix` - Severity: Nit
- Affected scope: §5.2
- Simplicity rationale: `waitsOnAPerson` (`board_page.go:324-326`) also leaves out Blocked `ReasonQaEpicAdded` cards, which are held by a person in the same way.
- Recommendation: Use `Status == Blocked && operatorHolds(BlockReason)`. That one change covers both.

### Wrong anchors - Severity: Nit
- Affected scope: §3.2, §4.1
- Simplicity rationale:
  - "e2e cards never reach the scheduler (`labels.go:110-114`)": those lines are `labelOrder`. The gate is `labels.go:163-176`.
  - "performing the merge" is at `brd.html:1459-1460`, not `1456-1457`.
  - The scope-boundary text runs from `brd.html:1382` to `1388`.
- Recommendation: Fix the line numbers.

### B3 depends on the automation repository having CI - Severity: Nit
- Affected scope: §4.3 guard 5
- Simplicity rationale: "At least one check run" means an automation repository without CI never merges anything.
- Recommendation: State this as a prerequisite in §7 B3 and in §10.

---

Solid as written: the QA-tool assumptions are labelled clearly (§1). The B3 guard list is at the right level of detail for a new write power. The label and settings arithmetic is correct (17 → 19 → 20).

Counts: B=1 M=8 N=6
