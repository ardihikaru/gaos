# Release B — e2e repository kinds, missing automation created on staging, approved specs that merge by themselves and then run

> **Status:** Implemented in v0.65.0 (rev 4; the operator approved it on 2026-11-27; B1–B3 ship as one release). Rounds: 1 (five lenses), 2 and 3 (readiness, critic, security); reports `docs/designs/reviews/release-b-e2e-kinds-merge-unit-tests-*-{1,2,3}.md`; triage §14 (round 1), §15 (round 2), §16 (round 3). The unit-test part was moved to its own design after round 2 (§5).
> **Policy:** `docs/rules/gaos-design-review.md`
> **Related:** `docs/designs/agent-seats-and-pr-agents.md` (D-102, "Release A"), `docs/designs/gaos-pr-agent-flows.md` (D-088), `docs/designs/gaos-pr-overview.md` (D-092), `docs/designs/qa-catalog-sync.md` (D-072), `docs/designs/gaos-external-tasks-and-pr-bundles.md` (D-099…D-101).
> **Paths.** A bare name (`qajobs.go`) is in `dashboard/`. A path starting `docs/`, `pustaka/`, `deploy/`, `artifacts/` or `README.md` is from the repository root. `QA:` paths are in the repository `qa-automation-tools` at `main` `dcdb06d` (2026-11-27); `QA:AB/` is `QA:ai-backend/internal/`. Every `QA:` claim is an **assumption** the author checked on 2026-11-27; the QA tool's changes get their own design there (§9). This document fixes the GAOS side and the contract.
> **Terms.** *Repository kind* (`repoKind`): `be` or `fullstack`, a property of one product repository. *Create job* / *run job*: the QA tool's `mode: create|run` jobs. *Spec pull request*: the pull request the QA tool opens on an automation repository after a person approves a generated spec. *Case code*: a QA case id. Worked example: the invented product **Route Planner** (`p-9`), repositories `acme/route-api` (`be`) and `acme/route-web` (`fullstack`), automation repository `acme/route-e2e`.

## 0. Why, the parts, and the operator's decisions

The operator asked for three agents on one product (D-102 §0). Release A built the reviewer and assignment. This release builds the **e2e automation builder** in two kinds — backend only, and frontend plus backend — so that when a repository is deployed to staging, GAOS runs the automation, has the missing automation created, and runs it once it has merged.

Operator decisions (2026-11-27), recorded in D-103:

1. **The QA tool, not GAOS, merges an approved spec**, by turning on GitHub auto-merge pinned to the commit a person approved, under a required check. GAOS never merges and holds no merge token.
2. **A repository with no kind sends no kind** (today's behaviour).
3. **Unit tests** go through the QA tool (run and create), a failure becoming a defect held for a person — **decided, deferred** to `docs/designs/release-b4-unit-tests.md` (§5).

| Part | What | QA-tool change | Phase / release |
|---|---|---|---|
| 1 | Repository kinds | capabilities route; `kind` on `create`/`run` | B1 / v0.65.0 |
| 2 | Missing automation created on a staging deploy | none | B2 / v0.66.0 |
| 3 | An approved spec merges by itself; GAOS then runs it | pinned auto-merge, `pushed_sha`, a call to GAOS before enabling | B3 / v0.67.0 |

## 1. What exists today

**GAOS.**
- The staging callback (`staging.go:196`, HTTP Basic per product) plans one `e2e:run` card; its job runs every automated case (`caseCodeFor` answers `*`, `qajobs.go:238-240`).
- A `pr.opened` plans an `e2e:automation` card; its case comes from a `QA-case:` line (`qajobs.go:241-254`).
- The QA job runner (`Tick` `qajobs.go:204`, `start` `:274`, `poll` `:310`) starts and polls jobs; each reason is written by `qaJobReason(id, text)` (`:260`); it holds an e2e card in Needs you while the QA tool waits for approval (BR-44), ends it Done on success (`:332-336`), and fails a job active over 40 minutes (`:157`).
- `recordQARow` (`qajobs.go:412`) turns a job's spec pull request into a Pull requests row of kind `qa` with its case (`PullRequest`, `pullrequests.go:36-63`; `schema/18-pull-requests.sql`). The fix-PR poller refreshes such rows (`pollRow`, `fixprpoll.go:319`) until they are final.
- GAOS keeps the QA catalog (`QaCase{Level, Decision, Implemented}`, `qacatalog.go:81-108`). The QA tool pushes results, including `implemented`, through `QaApplyResults` (`qacatalog_store.go:254`, the flag at `:300`).
- `ReadinessBand` (`model.go:729`): `complete` ≥ 90, `working` ≥ 70, `minimum` ≥ 45, else `insufficient`. Effective score = `ReadinessScore()` while no drift penalty exists (`model.go:603-608`).

**The QA tool** (assumptions).
- Levels `API`, `GQL`, `WEB`, `E2E`; one generator writing `tests/web/…` or `tests/api/rest/…`; GraphQL specs cannot be generated (`QA:AB/services/caseauto/validate.go:66-68`).
- After a person approves, it commits one file, pushes `qa-auto/<code-lower>` (never forced), opens a pull request under its `GH_TOKEN` (`QA:AB/services/caseauto/machine.go:650-678`). Nothing merges it. A `run` needs the case's `spec_paths` in its catalog, which a sync after the merge fills (`QA:AB/api/case_automation.go:238`; `QA:AB/api/gaos_automation.go:266-268`).

## 2. Part 1 — Repository kinds (B1)

### 2.1 What a kind means

| Kind | A `run` of every case runs | A `create` is for |
|---|---|---|
| none | every automated case (today) | today's behaviour |
| `be` (backend only) | `API`, `GQL` cases | `API` cases, written under `tests/api/rest/` |
| `fullstack` (frontend and backend) | every case | `WEB`, `E2E` cases, written under `tests/web/` |

`Master` gains `RepoKinds map[string]string` (lower-cased `owner/repo` → `be|fullstack`), stored as the Product document property `repoKinds` (schema-less; no DDL). **No entry = no kind**: no kind label, no `kind` sent.

**Editing.** A new store method `SetRepoKinds(ctx, productID string, kinds map[string]string, staging, autoMerge bool, actor string) (Result, error)` (`MemStore`; `ArcadeStore` through `mutate`; `readOnly` refuses; added to `Store`). The map **replaces** the product's kinds; only keys of `Master.Repos` (lower-cased) and values `be|fullstack` are accepted, anything else is refused (400 at the route). The two booleans set `StagingCreates` (§3.1) and `AutoMergeSpecs` (§4.1). It emits `RecordUpdated` with `Payload.Actor` and the text `<product>: repository kinds by <alias>: acme/route-api=be acme/route-web=fullstack; staging creates on; spec auto-merge off`. It does **not** touch assignment: the Agents page editor posts `action=kinds` (fields `kind.<owner/repo>`, `stagingCreates`, `autoMergeSpecs`) to `POST /agents/assign`, a branch that calls only `SetRepoKinds`. Same admins (`GAOS_AUTOFIX_ADMINS`).

### 2.2 The label

A new planner-only family **`e2e-kind`**: `e2e-kind:be`, `e2e-kind:fullstack` (labels 17 → 19), placed before `source` in `labelOrder`; `SetTaskLabel` refuses the family (as `source`, `prflow.go:543-548`). It is set on:
- the staging **run** card (`prCardsFor`, `prflow.go:80`, gains the master: `prCardsFor(ev, m)`; callers `PlanPullRequestCards` and `apiSpec`, whose signature becomes `apiSpec(m Master, in APICardIn)`, called from `PlanAPICard`, `api_cards.go:368`), with the kind of `lower(ev.Org + "/" + ev.Repo)`;
- the staging **create** cards (§3), with the kind their case's level maps to (§3.1 step 4).

It is **not** set on `pr.opened` `e2e:automation` cards (their case is named by a person in the pull request; the `QA-case:` path is unchanged) nor on API `automation-run` cards (no repository). **The label decides** what is sent: `start` reads it, never `RepoKinds`.

### 2.3 Telling the QA tool

- **Capabilities.** The QA tool gains `GET /api/v1/gaos/capabilities` → `{"capabilities":["kind","auto-merge"]}` on the existing bearer. `qaOutbound` (`qaclient.go`) gains `capabilities(ctx)`, read when a job starts, cached 10 minutes; on an error the last known set is kept; a 404 is the empty set.
- **The start request** becomes a struct: `startAutomation(ctx, in qaStartIn)` with `qaStartIn{Product, CaseCode, Mode, Kind string; AutoMerge *bool}`, marshalled with `omitempty` (the `qaJobClient` interface, `qajobs.go:112-116`, and `fakeQAJobs` in `qajobs_test.go` change with it). `Kind` is set only when the card has a kind label **and** the tool lists `kind`; `AutoMerge` only when the tool lists `auto-merge` (§4.1). A tool without the capability therefore receives today's body.
- When a kind label could not be sent, the card's **final** reason ends `(kind not sent)`.

## 3. Part 2 — Missing automation created on a staging deploy (B2)

### 3.1 What the callback plans

A per-product switch **`StagingCreates`** (Product document property `stagingCreates`, default false, §2.1) turns this part on. With it off, the callback plans exactly what it plans today.

`apiStaging` calls a new store method `PlanStagingCards(ctx, productID string, ev PREvent) (planned, existing []string, creates int, res Result, err error)` (`MemStore` in new `stagingcreate.go`; `ArcadeStore` through `mutate`; `readOnly`), which does, **under one lock**:

1. **The run card**, as `PlanPullRequestCards` plans it today (key per commit, `gateUnassigned` for role `qa`, then `GAOS_PR_MAX_OPEN`), with the kind label. A capped run is **not an error**: it returns `err == nil` with `res.Capped = true` (`Result.Capped`, `store.go`), so the creates below persist (`ArcadeStore.mutate` drops the Result on an error, `store_arcade.go:632-635`).
2. **Only when** the product has `StagingCreates`, the repository has a kind, **and** the run card was planned new (a replay of the commit plans no creates): the uncovered cases. `QaCase` has no repository, so the selection is **product-wide by level**: `Decision == "planned"`, `!Implemented`, the case code matching **`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`** (no key delimiter; a case that does not match is skipped and logged), and `Level` in the **create levels** — `be`: `API`; `fullstack`: `WEB`, `E2E`, and also `API` when the product has no `be` repository. Oldest `CatRev` first.
3. A case is skipped when **any** of these holds (times from the row's `OpenedAt`, its new `FinalAt` (§4.3), or the card's last state change from the timing index, `lastChangesLocked` in `board_page.go`):
   - an open card of the product is creating it: a `staging-create/` card of that case, or an `e2e:automation` card whose recorded case (§3.2) is that case. *Accepted race:* a `pr.opened` card that has not started has no recorded case yet; if both run, the QA tool refuses the second push of `qa-auto/<code>` and that card ends Failed with its reason;
   - any `qa` Pull requests row of the product with that case (whatever card made it) is **open** and was opened less than 30 days ago;
   - such a row is **merged** less than 14 days ago and the catalog does not yet show `Implemented`;
   - such a row was **closed** unmerged, or a `staging-create/` card of the case ended Failed or Canceled, less than 7 days ago.
4. **Create cards**, up to `min(GAOS_E2E_CREATE_MAX, GAOS_E2E_CREATE_OPEN − open creates)` (defaults 2 and 4; `CREATE_MAX` ≤ 10):
   - key **`staging-create/<productId>:<caseCode>#<n>`** (*n* = 1 + earlier `staging-create/` cards of the case); id `e2e-create-<sanitized productId-caseCode>-<n>` through `freeCardID`; title `E2E automation of <caseCode> (staging deploy of <owner>/<repo>)` through `prTitle`;
   - labels `e2e:automation` + the kind of the case's **level**: `API` → `e2e-kind:be`, `WEB`/`E2E` → `e2e-kind:fullstack` (so an `API` case planned from a `fullstack` deploy asks for a backend spec);
   - role `qa`; outside `GAOS_PR_MAX_OPEN` (`openPRCardsLocked`, `prflow.go:178-195`, skips `staging-create/`); `prKeyParts` (`prreview.go:198`) excludes `staging-create/`.

`caseCodeFor` reads the case from a `staging-create/` key, in a branch **before** the `mode == "run"` shortcut (`qajobs.go:235-240`), splitting at the first `:` after the prefix and the last `#`. Events: the cards' `TaskPlanned` (and `StagingDeployed` as today); no new event type.

**Answers:** 202 `{accepted, taskId, creates}`; 200 `{accepted, duplicate, taskId, creates: 0}`; 409 `{error:"capped", creates}` (`res.Capped`); 422 `unassigned` (D-102).

**BR-54** is amended (D-103): "…and, on a product that turned it on, the staging callback may plan create cards for the product's uncovered cases of the deployed repository's kind, capped; and a QA spec pull request's merge may plan one run of its case (§4.2)." OQ-42 (an outside client creating from a case code) stays open; D-103 answers the staging surface only.

### 3.2 The case is recorded, and what a create ends as

`qaTracked` gains the case code, and `qaJobReason(id, case, text string)` writes `qa-job:<id> case:<code> <text>` on **every** intermediate reason (started, waiting for approval, resumed, finished). Adoption after a restart (`start`, `qajobs.go:279-281`) reads `case:` from the reason, else recomputes it with `caseCodeFor` from a `staging-create/` key; `recordQARow` therefore always has the case.

The create job is `succeeded` once its spec pull request is open. The card ends **Done** with `case:<code> spec PR open (<link>); <how it merges>`, where `<link>` is rebuilt from the parsed owner/repo/number of a product-owned automation repository (`parseGitHubPRURL`, used in `recordQARow`) — never the QA tool's text — and `<how it merges>` comes from the job's `auto_merge` (§4.1): `enabled` → `it merges when its required check passes`; `refused:<why>` → `a person merges it (auto-merge refused: <why>)`, `<why>` bounded to 80 characters and screened; `off` or absent → `a person merges it`. A `(kind not sent)` suffix goes after it.

## 4. Part 3 — An approved spec merges by itself, then runs (B3)

### 4.1 The QA tool's side, and GAOS's answer to it

**Product switch.** `AutoMergeSpecs` (Product document property `autoMergeSpecs`, default **false**, §2.1, recorded with the actor).

**On `create`**, GAOS sends `AutoMerge` (only to a tool listing `auto-merge`): `true` when the product has `AutoMergeSpecs` and `ReadinessBand(m.ReadinessScore())` is `working` or `complete`; otherwise `false`.

**Before enabling**, the QA tool asks GAOS again — a new machine route `GET /api/v1/products/{id}/spec-auto-merge` on the QA token (registered through `registerAPI`, `api_v1.go:402`) answering `{allowed: bool}` by the same rule, read at that moment. No answer, or `false`, means no auto-merge. A band that dropped, or a switch turned off, while a spec waited for approval therefore stops the merge.

**The QA tool enables auto-merge only when all hold**, verifies each, and reports which failed:
- the request had `auto_merge: true` and the route answers `allowed`; a spec whose create carried no `auto_merge: true` (including one started from the QA tool's own interface) never auto-merges;
- a ruleset lets **only the QA tool's identity** update `refs/heads/qa-auto/*`;
- the default branch requires a status check **from a fixed source** (an App- or Actions-bound required check, not any commit status) that runs the spec's lint, typecheck, a **secret scan**, and a dry run with **no repository secrets** and network only to the staging allowlist (OQ-40 is related);
- its identity has no bypass or admin right on the repository.

It then runs `gh pr merge --auto --squash --match-head-commit <pushed_sha>`. The job status of a create gains `pushed_sha` (present whenever `pr_url` is) and `auto_merge` (`enabled` | `refused:<token>` | `off`).

**Scope line.** The BRD's out-of-scope line "GAOS indexes and proposes; the gate is where its authority stops" (`pustaka/docs/product/brd.html:1459-1460`) gains, under D-103: "GAOS may allow the QA tool to auto-merge its own approved spec pull requests in automation repositories, for a product that turned it on, in the Working and Complete bands; GAOS itself never merges." **BR-08** is extended to that auto-merge (Minimum and Insufficient never). The BRD's B-2 band text (`brd.html:806`) gains: "a spec a person approved in the QA tool counts as attended." BR-61 and D-091 (2) stay: GAOS never merges.

### 4.2 GAOS runs the merged case

**Recording.** `recordQARow` keeps polling until it has both `pr_url` and a `pushed_sha` matching `shaRe` (`staging.go:186`), then stores `qaSha` on the row; `upsertPullRequestLocked`'s fill list (`pullrequests.go:130-145`) gains `QASha`.

**At the merge.** `ghPRState` (`fixprpoll.go:39`) gains `HeadSHA`; `PRFields` (`pullrequests.go:66-73`) gains `HeadSHA`; `pollRow` passes it. `AdvancePullRequest` now also sets the row's `FinalAt` when it becomes final, and, for a `qa` row turning **merged**, calls `planMergedRunLocked`:
- the row has a case, its `qaSha` is set and equals the merged head (`HeadSHA`); otherwise nothing is planned, the row's `QANote` (new property) is `merged head is not the approved commit`, a **security notice** of kind `e2e:failure` names the product and the pull request number, and `RecordUpdated` records it;
- the product's `qa` role runs (`gateUnassigned`), else nothing is planned and `QANote` is `qa role unassigned`;
- the case is **already `Implemented`** in the catalog; otherwise `QANote` is `waiting for the catalog` and planning waits (below).

**When the catalog catches up.** `QaApplyResults` (`qacatalog_store.go:254`), where a case's `Implemented` turns true (`:300`), calls `planMergedRunLocked` for every merged `qa` row of that case whose `QANote` is `waiting for the catalog` and whose `FinalAt` is less than 14 days ago.

**The card.** Key `qa-merged/<productId>:<owner>/<repo>#<prNumber>:<caseCode>`; id `e2e-merged-<sanitized …>` through `freeCardID`; title `E2E run of <caseCode> after its spec merged`; label `e2e:run` only (a single-case run ignores kind); role `qa`; **never capped**; `prKeyParts` excludes `qa-merged/`; `caseCodeFor` reads the case (after the last `:`), before the `run` shortcut. The job is `mode: run, caseCode: <case>`. The dedup key makes a repeat a no-op. `AdvancePullRequest` and `QaApplyResults` thereby plan cards and emit `TaskPlanned`; their Results carry them (Mem and Arcade).

### 4.3 Schema

`schema/20-release-b.sql`: `CREATE PROPERTY PullRequest.qaSha STRING;`, `CREATE PROPERTY PullRequest.qaNote STRING;`, `CREATE PROPERTY PullRequest.finalAt LONG;` — read and written by `store_arcade_pullrequests.go`; `artifacts/v1/gaos-arcadedb-model.html` carries the same statements.

## 5. Unit tests: decided, deferred to their own design

The operator decided (D-103 records it): unit tests through the QA tool, run and create, the create opening a draft pull request through a separate identity limited to `qa-unit/*`, a failure becoming a defect held for a person. Round 2 showed it needs work this design should not carry: a unit defect's own source slice (the review defect's `buildReviewContext`, `reviewdefect.go:319`, is the precedent), dedup per repository and failing-test set with a daily cap, a contract separating test failures from job failures, a `unit:qa` card with its own family, start branch, timeout and row kind, the planner knowing the capability, a sandbox attestation, the identity without `workflows` permission, a path check against `TestGlobs`, `CIConfirmed` for `qa-unit/*`, admin-only release of the held card, and amendments to BR-44, BR-54 and D-091 (8). Those are the requirements of `docs/designs/release-b4-unit-tests.md`. Until it ships, `unit:test` behaves as today.

## 6. Registers

- **D-103** (next free): parts 1–3 as above; decisions 1–3 of §0 (3 as decided-deferred); supersedes D-102 (5)'s GAOS-merge proposal; amends **BR-54**, the **BRD scope line**, **BR-08** and **B-2** (§4.1); OQ-42 answered for the staging surface only; records the residuals of §8.
- **BR-62**: "GAOS sends a repository kind only when the operator set one. GAOS never merges; the QA tool may auto-merge an approved spec of a product that turned it on, in the Working and Complete bands, pinned to the approved commit, under a fixed-source required check; GAOS runs the case after such a merge only when the merged head is the approved commit."
- Labels 17 → 19. Settings: `GAOS_E2E_CREATE_MAX` (2), `GAOS_E2E_CREATE_OPEN` (4); product `repoKinds`, `stagingCreates`, `autoMergeSpecs`. Schema `schema/20-release-b.sql`. Route `GET /api/v1/products/{id}/spec-auto-merge`.
- **Per phase:** B1 — D-103 (decisions; part 1) and BR-62's first sentence; B2 — the BR-54 create clause; B3 — the rest of D-103, BR-62, BR-08, B-2 and the scope line. The QA test plan section `#go-release-b`, TC-819 onward.
- Pages: `pustaka/docs/product/{decision-log,brd,open-questions,qa-test-plan}.html`, `pustaka/docs/integrations/{agents,pr-agents}.html`, `pustaka/docs/changelog.html`, `pustaka/docs/assets/toc.js`, `pustaka/docs/assets/toc/product.js`, `README.md`, `deploy/check-workspace.sh`, `.claude/rules/qa-test-plan.md` (the highest TC and the counts).

## 7. Change list and tests, per phase

**B1 — repository kinds** (v0.65.0)

| File | Change |
|---|---|
| `model.go` | `Master.RepoKinds`, `StagingCreates`, `AutoMergeSpecs`; `Clone` |
| `store.go`, `store_arcade.go` | `SetRepoKinds` (interface, `readOnly`); persist/read `repoKinds`, `stagingCreates`, `autoMergeSpecs` |
| new `repokinds.go`, `handlers_assign.go`, `view.go`, `templates/backoffice.html` | `SetRepoKinds`; `action=kinds`; the editor's selects and two switches |
| `labels.go`, `prflow.go`, `api_cards.go` | `e2e-kind` family; `prCardsFor(ev, m)`; `apiSpec(m, in)`; refusal |
| `qaclient.go`, `qajobs.go`, `qajobs_test.go` | `capabilities`; `qaStartIn`; `qaJobReason(id, case, text)`; `qaTracked.caseCode`; `fakeQAJobs` |
| `repokinds_test.go` (new) | TC-819 `TestRepoKindsAreSavedAndValidated` (keys, values, replace, event text, assignment untouched), TC-820 `TestRepoKindsSurviveARestart` (ArcadeDB) |
| `prflow_test.go` | TC-821 `TestRunCardsCarryTheRepositoryKind` (staging run yes; `pr.opened` and API run no; operator cannot set it) |
| `qajobs_test.go` | TC-822 `TestKindAndAutoMergeAreSentOnlyWithTheCapability` (cache, error keeps last set, 404 empty, omitempty body), TC-823 `TestNoKindIsTodaysBehaviour`, TC-824 `TestEveryReasonCarriesTheCase` (adoption while waiting for approval) |

**B2 — creates on staging** (v0.66.0)

| File | Change |
|---|---|
| new `stagingcreate.go`, `store.go`, `store_arcade.go` | `PlanStagingCards` |
| `staging.go` | calls it; the answers |
| `prflow.go`, `prreview.go` | `openPRCardsLocked` and `prKeyParts` skip `staging-create/` |
| `qajobs.go` | `caseCodeFor` reads `staging-create/`; the Done reasons (§3.2) |
| `stagingcreate_test.go` (new) | TC-825 `TestStagingPlansCreatesForUncoveredCases` (switch off = today; levels per kind; `API` on a fullstack-only product → `e2e-kind:be`; bad case code skipped; replay plans none), TC-826 `TestCreateBlockingRules` (open card, recorded case, open row 30 d, merged 14 d, closed and failed 7 d), TC-827 `TestCreatesHaveTheirOwnCap`, TC-828 `TestStagingAnswersCarryCreates` (202, 200, 409 with creates, 422), TC-829 `TestCreatesPersistWhenTheRunIsCapped` (ArcadeDB) |
| `qajobs_test.go` | TC-830 `TestCreateDoneReasons` (enabled, refused, off; rebuilt link; kind suffix) |

**B3 — merge, then run** (v0.67.0; the QA tool's change first)

| File | Change |
|---|---|
| `schema/20-release-b.sql`, `artifacts/v1/gaos-arcadedb-model.html` | three properties |
| `pullrequests.go`, `store_arcade_pullrequests.go`, `fixprpoll.go` | `QASha`, `QANote`, `FinalAt`, `PRFields.HeadSHA`, `ghPRState.HeadSHA`; `planMergedRunLocked`; `prKeyParts` skips `qa-merged/` |
| `qacatalog_store.go` | `QaApplyResults` calls `planMergedRunLocked` |
| `qajobs.go`, `qaclient.go` | `AutoMerge` on create; `pushed_sha`/`auto_merge` read and validated; recording until both are present; `caseCodeFor` reads `qa-merged/` |
| `api_v1.go`, new `specautomerge.go` | `GET /api/v1/products/{id}/spec-auto-merge` |
| `fixprpoll_test.go` | TC-831 `TestAMergedApprovedSpecRunsItsCaseOnce`, TC-832 `TestAMergedUnapprovedHeadIsReportedAndDoesNotRun`, TC-833 `TestAMergedSpecWaitsForTheCatalog` (then `QaApplyResults` plans it), TC-834 `TestAMergedSpecWithoutQARoleDoesNotRun` |
| `qajobs_test.go` | TC-835 `TestAutoMergeFollowsSwitchAndBand`, TC-836 `TestQaShaIsRecordedEvenWhenTheURLComesFirst` |
| `api_v1_test.go` | TC-837 `TestSpecAutoMergeRoute` (QA token only; allowed by switch and band; refused otherwise) |
| `store_conformance_test.go` | TC-838 `TestPullRequestReleaseBFieldsSurviveARestart` (ArcadeDB) |

Every phase also updates its §6 pages and counts, and exits on: `(cd dashboard && go test ./... && go test -race ./...)` green; `deploy/schema/scratch.sh up` then the ArcadeDB suite with `-v` and no `--- SKIP`; `./deploy/check-workspace.sh` and `(cd pustaka && go run . check ./docs)` green. GAOS-observable exits: **B1** a `be` repository's staging run request carries `"kind":"be"`, an unconfigured one none; **B2** two uncovered `API` cases plan two creates and an immediate second deploy none; **B3** a `qa` row turning merged at its approved head, its case implemented, plans one run of that case; at another head, none and a notice.

## 8. Security

- **GAOS gains no write to GitHub.** It gains one read-only machine route the QA tool asks before auto-merging.
- **The staging credential's authority widens**: on a product that turned `StagingCreates` on, it can start generation jobs (capped, only on a new commit, never on a replay). D-103 records this.
- **Auto-merge** is pinned to the approved commit, behind a ruleset only the QA identity can write through, a fixed-source required check with a secret scan and a secret-free dry run, no bypass, the product switch and the band, re-asked at enable time. An unapproved merged head is reported (notice, event, row note) and GAOS does not run its case; the next run of every case still runs whatever the default branch holds — that residual is recorded in D-103 and is what the ruleset prevents.
- **Inputs from the QA tool**: `pushed_sha` must match `shaRe`; `auto_merge` must be one of the three forms; a link in a reason is rebuilt by GAOS. Case codes in keys match a delimiter-free pattern.
- **Releasing a Failed create early** is any signed-in account's board move (unattributed, as every board move today); it is bounded by the approval in the QA tool. Recorded in D-103.
- **The published docs page** describes the mechanism with the invented example only (D-085).

## 9. The QA tool's work, and the order

Pull requests in `qa-automation-tools`, each with its own design there: (1) `capabilities` and `kind` (B1 sends `kind` only once it is listed; B1's labels ship first); (2) `auto_merge` on create, the call to `spec-auto-merge`, the verified preconditions, the pinned auto-merge, `pushed_sha` and `auto_merge` on the job status (before B3). GAOS phases ship independently; a missing capability leaves today's behaviour.

## 10. Alternatives considered

| Part | Alternative | Why not |
|---|---|---|
| 1 | A product-wide kind | Route Planner has a backend and a web repository; one kind would run the wrong half on half its deploys |
| 1 | Default `fullstack` | Changes what runs for every product the day the QA tool learns `kind`; "no kind = today" changes nothing |
| 2 | Hold the run until creates finish | A new spec reaches the default branch only after an approval and a merge (hours); §4.2 runs the case after its merge instead |
| 2 | Creates on whenever a kind is set | Widens the staging credential's authority silently; a separate switch makes it a decision |
| 3 | GAOS merges with its own token behind guards | The guards re-check what the QA tool reports; a new token and four amendments for little assurance |
| 3 | Auto-merge enabled once, unpinned | A push after the approval would merge unseen |
| 3 | Run the case at the merge, before the catalog sync | The QA tool runs a case only from its synced `spec_paths`; the run would fail |

## 11. Non-goals

- GAOS merging or closing any pull request.
- Generating GraphQL specs.
- Changing the `QA-case:` path of `pr.opened` automation cards.
- Unit tests (§5).

## 12. Done when

- [ ] B1: a `be` repository's staging run sends `"kind":"be"`; an unconfigured one sends none.
- [ ] B2: with `StagingCreates` on, a deploy plans creates for uncovered cases of the repository's kind, never twice for a case, retrying a failure after a week.
- [ ] B3: an approved spec with a passing required check merges by itself, and once the catalog shows it, GAOS runs that case once.

## 13. Open for the operator

1. `GAOS_E2E_CREATE_MAX` = 2 and `GAOS_E2E_CREATE_OPEN` = 4 as defaults.
2. B3 needs, on the operator's automation repository: a ruleset restricting `qa-auto/*` to the QA identity, and a fixed-source required check. Without them, the QA tool leaves every spec for a person (B3 still runs the case after a person's merge).

## 14. Round-1 triage

(Kept as the record; pointers refer to rev 2's sections.)

| Finding (lenses) | Severity | Disposition | Note |
|---|---|---|---|
| GAOS merging contradicts BR-61; BR-08 (crit, comp, read, SEC-8) | Blocker / Major / Medium | PROPOSE FIX | the QA tool merges via pinned auto-merge (§4.1) |
| GAOS-side merge guards (SEC-1, 2, 6, 7, 9, 10, 12) | High … Low | PROPOSE FIX | moot: GAOS has no merge power |
| Merge wait vs timeout; restart (zoom, crit, read) | Major | PROPOSE FIX | moot |
| No change list / tests (all) | Blocker / Major | PROPOSE FIX | §7 |
| `fullstack` default drops API cases (zoom, crit) | Blocker / Major | PROPOSE FIX | no kind = today (§2.1) |
| `unit:fix` has no route to code; sandbox; identity; unscreened text (all, SEC-3/4/5) | Blocker / High / Medium | ESCALATE | part 4's own design (§5) |
| Create-then-run never runs what it created (zoom, crit, comp) | Major | PROPOSE FIX | run after merge (§4.2) |
| Create key, cap, lock, GQL, API-run kind, alternatives, anchors, terms (all) | Major / Nit | PROPOSE FIX | §2–§3, §10 |
| Kind vocabulary `be|e2e` (zoom) | Nit | DECLINE | `e2e` already means "a spec" there |

## 15. Round-2 triage

| Finding (lenses) | Severity | Disposition | Note |
|---|---|---|---|
| Part 4 blockers and requirements (read, crit, SEC-3/4/5/6) | Blocker … Low | ESCALATE | §5 |
| Auto-merge not bound to the approved commit (SEC-1) | High | PROPOSE FIX | pinned; `qaSha` check (§4.1, §4.2) |
| Required check satisfiable by the QA token (SEC-2) | Medium | PROPOSE FIX | fixed-source check, verified (§4.1) |
| BR-08, BR-54 for the merge run (read, crit, SEC-7) | Major / Low | PROPOSE FIX | §4.1, §6 |
| `qa-merged/` cap; adoption loses the case; duplicates; product-wide levels; change list; anchors (read, crit) | Major / Nit | PROPOSE FIX | §3, §4.2, §7 |
| Private repository named (SEC-8) | Low | PROPOSE FIX | reworded |

## 16. Round-3 triage

Round 3: readiness B0 M10 N7; critic B1 M13 N8; security High 1, Medium 4, Low 3.

| Finding (lenses) | Severity | Disposition | Note |
|---|---|---|---|
| `API` creates on a fullstack-only product contradict the kind's folder (read, crit) | Blocker / Major | PROPOSE FIX | the create's kind follows the case's level (§3.1 step 4) |
| Post-merge race: a push after approval merges; mismatch has no record (SEC-1) | High | PROPOSE FIX | `qa-auto/*` ruleset verified by the QA tool; `QANote`, notice, event; residual recorded (§4.1, §4.2, §8) |
| Working band vs B-2 (SEC-2) | Medium | PROPOSE FIX | B-2 note: an approved spec counts as attended (§4.1) |
| Stale `auto_merge` flag; QA-UI specs (SEC-3, crit) | Medium / Major | PROPOSE FIX | `spec-auto-merge` route asked at enable time; default off (§4.1) |
| Staging credential widened; replay (SEC-4) | Medium | PROPOSE FIX | `StagingCreates` switch; creates only on a new commit (§3.1, §8) |
| Required check scope (SEC-5) | Medium | PROPOSE FIX | secret scan, secret-free dry run (§4.1) |
| QA-supplied values (SEC-6, crit) | Low / Major | PROPOSE FIX | `shaRe`, enumerated `auto_merge`, rebuilt link (§3.2, §4.2) |
| Early release unattributed (SEC-7) | Low | DECLINE | bounded by the approval; recorded in D-103 (§8) |
| Docs and §8 (SEC-8) | Low | PROPOSE FIX | §8 |
| No time base for the windows (read, crit) | Major | PROPOSE FIX | `OpenedAt`, `FinalAt`, last state change (§3.1 step 3, §4.3) |
| Capped run drops the creates in ArcadeDB (read, crit) | Major | PROPOSE FIX | `res.Capped`, no error (§3.1 step 1) |
| Row "reason" and `HeadSHA` have no carrier (read, crit) | Major | PROPOSE FIX | `QANote`, `PRFields.HeadSHA` (§4.2, §4.3) |
| Titles and ids (read) | Major | PROPOSE FIX | §3.1 step 4, §4.2 |
| Done reason from capability, URL from the QA tool (read, crit) | Major | PROPOSE FIX | from the job's `auto_merge`; rebuilt link (§3.2) |
| `auto_merge` sent without capability (read, crit) | Major | PROPOSE FIX | `qaStartIn`, omitempty (§2.3) |
| `case:` overwritten; pending PR cards (read, crit) | Major | PROPOSE FIX | `qaJobReason(id, case, text)`; accepted race stated (§3.1, §3.2) |
| PR-planned rows don't block (read, crit) | Major | PROPOSE FIX | blocking by any `qa` row of the case (§3.1) |
| `SetAssigned` signature, reset semantics; saving a kind turns gating on (read, crit) | Major | PROPOSE FIX | separate `SetRepoKinds`, `action=kinds` (§2.1) |
| Auto-merge has no switch; band drop (crit) | Major | PROPOSE FIX | `AutoMergeSpecs` default off; re-asked at enable time |
| "Never merges" understates GAOS's role (crit) | Major | PROPOSE FIX | the scope line is amended (§4.1) |
| Kind label on `pr.opened` creates (crit) | Major | PROPOSE FIX | not set there (§2.2) |
| Case ids with delimiters (read, crit) | Major / Nit | PROPOSE FIX | pattern; parse rule (§3.1) |
| Run before the catalog sync (crit) | Major | PROPOSE FIX | waits for `Implemented` via `QaApplyResults` (§4.2) |
| `caseCodeFor` order; `qa-merged/` key collision; effective band; anchors; phase registers; part-4 leaks; events; observable Done-when; §3.3 pointer; B2 lists a B3 key; §14 scope wording (read, crit) | Nit | PROPOSE FIX | applied |
