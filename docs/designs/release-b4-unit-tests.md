# Release B part 4 — unit tests through the QA tool, a failure held for a person

> **Status:** In Review (rev 1). The goldfish round has not run yet (see the end of this header).
> **Policy:** `docs/rules/gaos-design-review.md`
> **Related:** `docs/designs/release-b-e2e-kinds-merge-unit-tests.md` (D-103; its §5 lists the requirements this design answers), `docs/designs/agent-seats-and-pr-agents.md` (D-102, the review defect this reuses), `docs/designs/gaos-defect-auto-fix.md` (D-091, the triage and fix path).
> **Paths.** A bare name (`qajobs.go`) is in `dashboard/`. `QA:` names a route or field of the QA tool that does **not exist yet**; §9 is the contract a later qa-automation-tools design must build.
> **Example.** The invented product **Route Planner** (`p-9`), repository `acme/route-api` (`be`), automation repository `acme/route-e2e`.
> **Review note.** This revision was written by a worker that could not start reviewers; the five lenses of the policy must run on it before it is approved.

## 0. Why, and what the operator decided

Today a merged pull request plans one `unit:test` card for role `unit-test` (`prflow.go:88-91`). The scheduler hands it to an agent like any card (`labelsAllowDispatch` allows `unit:test`, `labels.go:179-188`); the agent has no checkout and runs nothing, and a FAILED card only sends a notice (`main.go:474-475`). Nothing runs the repository's tests.

D-103 (5) recorded the operator's decision: **the QA tool runs the unit tests, and may write missing ones; a create opens a draft pull request through a separate identity limited to `qa-unit/*`; a test failure becomes a defect a person releases before any agent works it.** This design is GAOS's half.

## 1. What changes, in one table

| # | What | Where |
|---|---|---|
| 1 | A product switch **`UnitQA`** (off by default): with it on, a merge plans a **`unit:qa`** card instead of `unit:test` | `model.go`, `prflow.go` |
| 2 | The QA-job runner drives `unit:qa` cards like e2e cards, with its own mode, start commit and timeout | `qajobs.go` |
| 3 | A job's **test failures** become a **unit defect** (kind `unit`), deduplicated and capped; a **job failure** never does | new `unitdefect.go`, `defects.go`, schema 21 |
| 4 | The unit defect's triage card is born **BLOCKED** and only an auto-fix admin releases it | `unitdefect.go`, `handlers_board.go` |
| 5 | Tests the QA tool wrote arrive as a **`qa-unit`** pull-request row; GAOS checks their paths against `TestGlobs` | `pullrequests.go`, `qajobs.go` |
| 6 | Registers: D-104, BR-63, amendments to BR-44, BR-54, D-091 (8) | §8 |

With `UnitQA` off, nothing changes: `unit:test` behaves as today.

## 2. Data flow

```
Brain Town ─pr.merged─▶ GAOS prFlow ─plans─▶ unit:qa card (PENDING)
                                                │
QA-job runner Tick ─POST QA:/gaos/automation {mode: unit-run|unit-create, repo, sha}─▶ QA tool
                                                │                                     │ sandbox runs the tests at sha
                                                │◀──GET /gaos/jobs/{id} (poll)────────┤ (create: writes tests, waits for a person,
                                                │                                     │  opens a DRAFT PR on qa-unit/<sha12>)
             job failed (infra) ─▶ card FAILED "qa-job-failed"; no defect
             job succeeded, unit.result passed ─▶ card DONE
             job succeeded, unit.result failed ─▶ card DONE "n tests failed" + unit defect (new or seen again)
                                                                  │
                                       triage card BLOCKED "held for a person" (Needs you is not used)
                                                                  │  an auto-fix admin moves it to Ready
                                       D-091 path: triage ─▶ easy ─▶ fixer ─▶ draft fix PR (gaos/fix-*)
             create: row kind qa-unit (owner/repo#n, files) ─▶ paths ⊆ TestGlobs? else note + notice
```

Numbered, who calls whom:

1. **Brain Town → GAOS.** A `pr.merged` event reaches `prFlow.handle` (`prflow.go`), which plans the merge's cards through `PlanPullRequestCards` (`prflow.go:206`). With `UnitQA` on (and the role `unit-test` assigned, `gateUnassigned`, D-102), the unit card is `unit:qa`, key `<owner>/<repo>#<n>:unit-qa`, title `Unit tests for <repo>#<n>`. The merge commit comes from the event (`PREvent.Commit` stays empty for `pr.merged` today; §4.2 fills it).
2. **GAOS runner → QA tool.** `qaJobRunner.Tick` (`qajobs.go:241`) picks the card up (`isE2ECard` grows a `unit:qa` branch, `qajobs.go:230`) and calls `startAutomation` (`qajobs.go:63`) with `mode: unit-run` or `unit-create`, `repo`, `sha`. The capability check (§4.3) runs first.
3. **QA tool.** It checks out `repo` at `sha` into a sandbox, runs the repository's unit tests, and — in `unit-create` — writes tests for changed source files that have none, waits for a person's approval in the QA tool, then pushes `qa-unit/<sha12>` and opens a **draft** pull request with its unit identity.
4. **GAOS runner polls** `GET /gaos/jobs/{id}` (`qajobs.go` `poll`). An approval wait moves the card to Needs you exactly as an e2e create does (BR-44's runner exception, extended in §8).
5. **The answer decides** (§5): a job failure fails the card; a test failure files a unit defect through `PlanUnitDefect` and ends the card Done; a pass ends it Done.
6. **A person releases** the defect's triage card (§6). From there the D-091 path runs unchanged: triage, verdict, fixer, draft fix pull request.
7. **A created test pull request** is recorded as a Pull requests row of kind `qa-unit` and checked against `TestGlobs` (§7).

## 3. The switch and the card

`Master` gains `UnitQA UnitQAConfig`:

```go
type UnitQAConfig struct {
    Enabled     bool   // plan unit:qa instead of unit:test
    Create      bool   // ask for unit-create (write missing tests), not only unit-run
    CIConfirmed bool   // the operator confirms CI runs no secrets on qa-unit/* branches
    AttestedAt  int64  // when GAOS last read a sandbox attestation (§4.3)
    AttestedBy  string // the admin who turned it on
}
```

Stored on the Product document as `unitQA` (schema-less, as `repoKinds`, `store_arcade.go`). Edited on the **Auto-fix** page by an auto-fix admin (`autoFixAdmin`, `handlers_autofix.go:17`) through a new store method `SetUnitQA(ctx, productID, cfg, actor) (Result, error)` (`MemStore`, `ArcadeStore` via `mutate`, `readOnly` refuses), which emits `RecordUpdated` with the actor. `Enabled` can be saved true only when the handler has just read the QA tool's capabilities and they list `unit` with a valid attestation (§4.3); `Create` additionally needs `CIConfirmed`.

**The label.** A new family `unit` already holds `unit:test`; `unit:qa` joins it (labels 19 → 20). `labelsAllowDispatch` refuses `unit:qa` (`labels.go:179-188`), so the scheduler never hands it to an agent: the runner is its only driver, as for `e2e:*`.

**Start commit.** The card names the merge commit, not a branch: `startAutomation` sends `sha`, the merged pull request's `merge_commit_sha`. `prFlow.handle` already re-reads a merged pull request from GitHub to set `MergeChecked` (`prflow.go:455-460`); `ghPR` (`github.go:44-58`) gains a field `MergeCommitSHA` (JSON `merge_commit_sha`), the re-read stores it on the event's `Commit` (`braintown_consumer.go:105`, today set only for staging) when it matches `shaRe`, and the key's suffix is `unit-qa`. With no merge commit (GitHub unread), no `unit:qa` card is planned and the reason is logged; the old `unit:test` card is not planned either (the switch chose the QA tool).

**Timeout.** `GAOS_QA_UNIT_TIMEOUT_MIN`, default 30, max 120, replaces `qaJobTimeout` (40 min, `qajobs.go:194`) for a `unit:qa` card's active time; the approval wait keeps its own budget.

## 4. The QA tool request

### 4.1 Modes

| Mode | Sent when | QA tool does |
|---|---|---|
| `unit-run` | `UnitQA.Enabled`, `!Create` | runs the tests at `sha` |
| `unit-create` | `UnitQA.Enabled && Create && CIConfirmed` | runs them, writes missing tests, waits for approval, opens a draft PR |

### 4.2 The body

`qaStartIn` (`qajobs.go`) gains `Repo string` (`owner/repo`) and `SHA string` (40 hex, `shaRe`), both `omitempty`; `CaseCode` is `"*"` for unit modes. A body is built only from the card's key and the product record — never from text of the pull request.

### 4.3 Capability and sandbox attestation

`capabilities` (`qaclient.go`) already caches the QA tool's list. Two new entries are read: `unit` and `unit-create`, and the answer gains an object `QA:sandbox`:

```json
{"capabilities":["kind","unit","unit-create"],
 "sandbox":{"isolation":"container-per-run","network":"allowlist","secrets":"none","source":"read-only","timeLimitSec":1800,"attestedAt":1795800000}}
```

GAOS accepts the attestation only when every field has exactly these values (a stricter one is fine: a smaller `timeLimitSec`) and `attestedAt` is less than 7 days old. **The planner knows the capability** by the product record, not by a network call: `UnitQA.AttestedAt` is written when an admin saves; the runner re-reads the capabilities at each start and, when `unit` is missing or the attestation fails, moves the card to **BLOCKED** with `the QA tool no longer attests a unit sandbox; an admin re-saves Unit tests on the Auto-fix page` and starts nothing.

## 5. Test failures are not job failures

`qaJob` (`qajobs.go:32-43`) gains `Unit *qaUnitResult`:

```go
type qaUnitResult struct {
    Result  string        `json:"result"`  // passed | failed | no-tests
    Failing []qaUnitTest  `json:"failing"` // at most 50
    Files   []string      `json:"files"`   // unit-create: the files its pull request adds
}
type qaUnitTest struct{ Test, File string } // a test's full name and its file
```

| QA job | `unit.result` | Card | Defect |
|---|---|---|---|
| `failed` (could not check out, sandbox error, timeout) | any | FAILED `qa-job-failed` | none |
| `succeeded` | `passed` or `no-tests` | DONE `unit tests passed` / `no unit tests` | none |
| `succeeded` | `failed` | DONE `n unit test(s) failed; defect <ref>` | `PlanUnitDefect` |
| `succeeded` | absent or unknown | FAILED `qa-unit-unreadable` | none |

Every `Test` and `File` is screened (`screenDefectText`, `defects.go:288`, as `PlanReviewDefect` does), capped (200 and 300 characters), and a `File` must be a repository-relative path (no `..`, no leading `/`).

## 6. The unit defect

### 6.1 Record

`PlanUnitDefect(ctx, productID string, in UnitDefectIn) (Result, error)` in new `unitdefect.go` (MemStore, ArcadeStore via `mutate`, `readOnly`), modelled on `PlanReviewDefect` (`reviewdefect.go:143-196`):

- `Kind` **`unit`** (`DefectKindUnit`, `defects.go:37-42`); `schema/21-unit-defects.sql` widens `Defect.kind` to `xfail|fail|xpass|review|unit` (the pattern of `schema/19-review-defects.sql:8`).
- `AutomationType` `"be"`: a unit test is application code; `AllowedGlobs` and `TargetRepos` still decide where a fix may write (the decision D-102 (4) made for review defects; amended in D-091 (8), §8).
- `Repo` the merged repository, `TargetRepos` `[Repo]`, `Files` the failing tests' files, `Excerpt` the failing test names, `Difficulty` empty (triage decides), `State` new.
- **Dedup key** `unit:<productId>:<owner>/<repo>:<h>`, `h` = the first 16 hex of sha256 over the sorted, newline-joined failing test names. The same failing set seen again updates `LastSeen` and plans nothing; a different set is a different defect.
- **Daily cap** `GAOS_UNIT_DEFECTS_PER_DAY` (default 5, max 20) new unit defects per product per UTC day; past it the card's reason says `defect cap reached` and nothing is filed.

### 6.2 Source slice

The triage packet needs code. `buildUnitContext` reuses `buildReviewContext(ctx, src, repo, sha, paths)` (`reviewdefect.go:319`) at the merge commit with `paths` = the failing test files plus the merged pull request's changed files (`pullFiles`, `prreview.go:226`), each kept only when it matches `AllowedGlobs` or `TestGlobs`. It is built by the runner outside the store lock and stored with `SetDefectContext` (`defects.go:612`), the same two-step the QA defect path uses.

### 6.3 Held for a person

The triage card is planned through `planDefectCardLocked(d, "triage", StatusBlocked, ReasonUnitHeld, LabelDefectTriage)` (`defecttriage.go:78`), `ReasonUnitHeld = "unit tests failed; an auto-fix admin releases this card"`. Two guards keep it held:

- `autoClaimLocked` (`autofix.go:162`) refuses a card whose defect is `unit` while its `BlockReason` is `ReasonUnitHeld`.
- `handleMoveCard` (`handlers_board.go:57`) answers 403 when the card's reason is `ReasonUnitHeld` and the session is not `autoFixAdmin` — today any signed-in account may move any card. The move itself is the ordinary `MoveUnblock` (`board.go:330`), attributed to the admin in its event.

After release the D-091 path runs unchanged, including `fixEligible` (`autofixpr.go:76`) and its caps.

## 7. Tests the QA tool wrote

The created pull request is a Pull requests row of new kind **`qa-unit`** (`PRKindQAUnit`, `pullrequests.go:20-21`), written by `recordQARow` (`qajobs.go:506`) when the job's `pr_url` is in a repository of `TargetRepos` (`prRepos` gains the kind, `pullrequests.go:97`). GAOS checks `unit.files`:

- every file must match `TestGlobs` (`matchAny`, as `autofixcheck.go:276`); else the row's `QANote` is `files outside TestGlobs`, a notice `unit:files` goes out, and GAOS records it with `RecordUpdated`. GAOS does not close the pull request (it never closes one).
- the row's head branch must start `qa-unit/`; else the same note.

The QA tool's unit identity (§9) has no `workflows` permission, so it cannot add or change `.github/workflows/*`; `UnitQA.CIConfirmed` is the operator's statement that CI runs no secrets on `qa-unit/*` — the same confirmation `AutoFix.CIConfirmed` gives for `gaos/fix-*` (`model.go:449-450`, `autofix.go:149`), for a second prefix.

## 8. Registers

- **D-104**: parts 1–7; the switch is off by default; a job failure never files a defect; the held card and its admin-only release; the attestation rule; the residuals (the sandbox is the QA tool's claim, checked for shape and age only; a created test runs in the repository's CI).
- **BR-63**: "A unit test failure reported by the QA tool files at most one unit defect per repository and failing set, capped per day; its triage card is held until an auto-fix admin releases it. A failure of the QA job itself files nothing."
- **BR-44** amended: the runner's Needs-you exception covers `unit:qa` cards waiting for an approval of created tests.
- **BR-54** amended: a QA job result may file a unit defect and plan its held triage card; it still never moves an existing card.
- **D-091 (8)** amended: a unit defect counts as backend by decision, as a review defect does.
- Labels 19 → 20; settings `GAOS_QA_UNIT_TIMEOUT_MIN`, `GAOS_UNIT_DEFECTS_PER_DAY`; schema `21-unit-defects.sql`; product `unitQA`.
- No new event type: `TaskPlanned`, `TaskMoved`, `RecordUpdated` carry every change (the catalog stays closed, BR-05).

## 9. The QA tool contract (assumptions for its own design)

| Need | Shape |
|---|---|
| Capabilities | `unit`, `unit-create`, and the `sandbox` object of §4.3 on `GET /api/v1/gaos/capabilities` |
| Start | `POST /api/v1/gaos/automation` with `mode` `unit-run`/`unit-create`, `caseCode` `*`, `repo`, `sha` |
| Status | `unit` object of §5 on `GET /api/v1/gaos/jobs/{id}`; `status` `failed` only for a job failure |
| Sandbox | a container per run, no access to the QA tool's host, database or credentials, network only to an operator registry list, read-only source, a time limit |
| Identity | a separate GitHub token for unit pull requests: fine-grained, contents write, pull requests write, **no workflows**; a ruleset lets it update only `qa-unit/*` |
| Pull request | always a draft, branch `qa-unit/<sha12>`, only test files; never merged by the QA tool |

## 10. Change list and tests

| File | Change |
|---|---|
| `model.go`, `store.go`, `store_arcade.go` | `UnitQAConfig`; `SetUnitQA`, `PlanUnitDefect` on `Store`; persistence |
| `labels.go` | `unit:qa`; `labelsAllowDispatch` refuses it |
| `prflow.go`, `braintown_consumer.go` | `unit:qa` instead of `unit:test` when on; merge commit on the event |
| `qajobs.go`, `qaclient.go` | unit modes; `Repo`/`SHA`; timeout; `qaUnitResult`; attestation; outcome table; `qa-unit` row and file check |
| new `unitdefect.go`, `defects.go`, `autofix.go`, `handlers_board.go` | the defect, its context, the hold, the admin-only release |
| `handlers_autofix.go`, `autofix_view.go`, `templates/backoffice.html` | the Unit tests section of the Auto-fix page |
| `schema/21-unit-defects.sql`, `artifacts/v1/gaos-arcadedb-model.html` | the kind |
| tests (TC-839 onward) | switch off = today; card shape and refusal by the scheduler; capability missing → BLOCKED; attestation rules; the outcome table row by row; dedup and cap; context paths filtered; held card refused by auto-claim and by a non-admin move, released by an admin; `qa-unit` row and TestGlobs note; persistence on ArcadeDB |

## 11. Alternatives

| Alternative | Why not |
|---|---|
| GAOS runs the tests | GAOS runs no code (BR-42 and the workspace allowlist); the QA tool already has an executor |
| A failure plans a fix card directly | the operator chose a person first (D-103 (5)) |
| Hold the card in Needs you | Needs you is an answer to a question (BR-44); a held defect is a decision to start work, which BLOCKED and a move already mean |
| The e2e `qa-auto` identity writes unit tests | it may update `qa-auto/*` and, with D-103, have auto-merge armed; a second, narrower identity keeps unit pull requests out of that |

## 12. Non-goals

GAOS merging anything; GAOS running code; changing the e2e path; unit tests for repositories outside `TargetRepos`.
