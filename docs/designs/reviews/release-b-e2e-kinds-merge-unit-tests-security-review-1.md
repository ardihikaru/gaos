# Security review: Release B (e2e kinds, create-then-run, guarded spec merges, unit tests through the QA tool)

Design: `/home/ardi/devel/projects/ardihikaru/my-projects/gaos-release-b/docs/designs/release-b-e2e-kinds-merge-unit-tests.md` (Draft rev 1)

## Paraphrase

### What this proposes

Four parts, each its own phase and release:

- **B1: e2e kinds by repository.** Each product repository gets a kind, `be` (backend only) or `fullstack`. A repository with no kind is `fullstack`. Admins set the kind on the Agents page through `POST /agents/assign`. A new planner-only label family, `e2e-kind`, goes on every e2e card. GAOS reads a new capability route on the QA tool. When the tool lists `kind`, GAOS sends `kind` on `POST /api/v1/gaos/automation`, and the tool then limits generation folders or which cases it runs.
- **B2: create then run on a staging deploy.** The authenticated staging callback reads GAOS's synced QA catalog. For each planned, unimplemented case of the deployed repository's kind, it plans up to N create cards, deduplicated per repository and case and capped per product. It then plans the run card, which depends on those create cards. The run waits for them for at most 30 minutes. With merge off, a create card ends Done once its spec pull request is open. BR-54 is amended.
- **B3: GAOS merges spec pull requests.** This is a new write power, for automation repositories only. It uses a new fine-grained token file, `GAOS_GITHUB_MERGE_TOKEN_FILE`. GAOS merges only when all of these hold: a per-product switch is on; the readiness band is `working` or `complete`; the repository is in the automation list; the pull request is on a `qa-auto/` branch from the same repository; its author equals `pr_author` as the QA tool reports it; its head equals `pushed_sha` as the QA tool reports it; it changes exactly one spec file at the QA tool's reported `spec_path`; all checks are green and `mergeable_state` is `clean`. The merge is a squash pinned to `pushed_sha`. Every outcome ends Done. The decision is D-103, the rule BR-62.
- **B4: unit tests through the QA tool.** The QA tool gains a `unit_run` executor type that clones a product repository and runs a configured command (for example `go test ./...`). It also gains `POST /api/v1/gaos/unit` with modes `run` and `create`. In `create`, the tool's model writes tests, a person approves them, and the tool opens a draft pull request on a `qa-unit/` branch of the **product** repository. On the GAOS side, `AutoFix.UnitTestsViaQA` sends `unit:test` cards to the QA job runner. A failed run plans a Blocked `unit:fix` card that carries the failure summary as context; a person releases it to an agent.

### How the existing system works, per this document

- The staging callback is HTTP Basic per product and plans one `e2e:run` for every case.
- `pr.opened` plans `e2e:automation`, with the case taken from a `QA-case:` line.
- The QA job runner starts and polls jobs, uses Needs you for QA approval waits (BR-44), and ends cards Done on success.
- GAOS holds a synced copy of the QA catalog.
- The `unit:test` card goes to an agent whose workspace holds no product code, and GAOS runs no code.
- The QA tool (stated as assumptions):
  - It commits one spec, pushes `qa-auto/<code>` and opens a pull request under its `GH_TOKEN`.
  - It records no SHA, and nothing merges these pull requests.
  - Runs happen in a Playwright executor on the app host, against staging only, with an outbound allowlist.
  - It has no unit-test runner.

All sections could be paraphrased; there are no comprehension findings.

## Trust boundaries touched

| From → To | Authenticates | Authorises | Validates | Far side fails |
|---|---|---|---|---|
| Operator browser → back office (`/agents/assign`, auto-fix form) | Signed, expiring cookie, checked against accounts (`dashboard/auth.go:375-396`). The rubric's premise that `signedIn()` accepts any cookie no longer holds in this tree. | `autoFixAdmin`, a mode-600 list that fails closed (`dashboard/handlers_autofix.go:17-37`, `dashboard/handlers_assign.go:102`) | 16 KiB body cap (`dashboard/handlers_assign.go:106`). How `kind.*` keys are validated is unstated. | 4xx |
| Deploy hook → staging callback | Basic auth per product, throttled (`dashboard/staging.go:206-230`) | Product owns the repository (`dashboard/staging.go:249-257`) | Strict body, 4 KiB (`dashboard/staging.go:233-243`) | Caps |
| GAOS → QA tool (capabilities, automation, unit, jobs) | Bearer | None on the GAOS side. Capabilities are self-asserted. | Lenient decode (`dashboard/qajobs.go:30-42`) | Card fails |
| QA tool → GAOS (job facts `pr_url`, `pushed_sha`, `pr_author`, `spec_path`, failure summary) | Same channel | **Trusted as facts for a merge** | Partial (SEC-1, SEC-5) | — |
| GAOS → GitHub (merge token) | New fine-grained token | Token scope plus `AutomationRepos` | Guards 1–6 | Done, "a person merges it" |
| QA executor → product code / package registries (B4) | — | — | Deferred to another design (SEC-3) | — |
| QA `GH_TOKEN` → product repositories (B4 create) | QA tool's account | "`qa-unit/` only", which is unimplementable as stated (SEC-4) | — | — |

## Findings

### SEC-1 The merge's author and SHA guards are self-referential; nothing outside the QA tool vouches for the approval - Severity: High
- Component: B3 merge guards (`ghMerger`)
- Evidence: design:101-103, design:112-113, design:168. Today a person performs every merge (`pustaka/docs/product/decision-log.html:1961-1962`).
- Attack path: Every fact the guards compare against comes from the QA tool's job answer:
  - `pr_url`, `pr_author`, `pushed_sha` and `spec_path` all come from that answer.
  - "Its `user.login` equals the job's `pr_author`" therefore only checks that the QA tool's answer agrees with itself.
  - Any account that can push a `qa-auto/*` branch to the automation repository can produce a pull request that passes guards 2–5. That includes any collaborator, or a human sharing the `GH_TOKEN` account. The QA tool then only has to return that URL, through a bug, a mix-up between jobs, or a compromise.
  - Nothing ties `spec_path` to the case the card was created for.

  Today the human merge is the only check that does not depend on the QA tool. This design removes it. §8's argument ("exactly what it is allowed to produce") assumes the QA tool is honest and bug-free.
- Remediation:
  - Pin the expected author in GAOS rather than taking it from the job: a per-product or env setting such as the QA bot's login. Require the pull request's `user.login` **and** the head commit's committer to equal it.
  - Require that the branch is exactly `qa-auto/<lower(caseCode)>` of the card's tracked case.
  - Require that the spec file's basename is `<caseCode>.spec.ts`.
  - Require that the head is exactly one commit ahead of base, via compare `ahead_by == 1`.
  - Recommend a repository ruleset that restricts `qa-auto/*` refs to the QA bot identity, and state it as an operator precondition.
- Effort: small

### SEC-2 Nothing stops a product repository from being listed as an automation repository, so "never merge into a product repository" rests on token scope alone - Severity: High
- Component: B3 guard 2; AutoFix validation
- Evidence:
  - design:112 (the only repository check is membership in `AutoFix.AutomationRepos`); design:174 (non-goal)
  - `dashboard/autofix.go:88-90` validates only shape and count, and performs no disjointness check against `Master.Repos` or `TargetRepos`.
  - `dashboard/reviewdefect.go:106` shows the overlap is possible and is handled elsewhere.
- Attack path: An admin can list `acme/route-api` under automation repositories, by mistake or after a compromised admin session. Together with a merge token that an operator scoped too widely (GAOS cannot check the scope, design:109), GAOS will squash-merge a `qa-auto/` pull request into a product repository. The non-goal is then enforced only by a convention.
- Remediation:
  - `ValidateAutoFix` refuses an `AutomationRepos` entry that is in `Master.Repos` or `TargetRepos`.
  - `ghMerger` re-checks the same condition at merge time.
  - At boot, for each product repository, GAOS reads `GET /repos/{owner}/{repo}` with the merge token. If `permissions.push` is true, GAOS disables merging and explains why.
- Effort: quick-win

### SEC-3 Unit runs execute product code and its dependencies on the QA app host, and the sandbox is deferred while GAOS's gate is a self-asserted capability - Severity: High
- Component: B4 `unit_run`; GAOS switch `UnitTestsViaQA`
- Evidence:
  - design:38 (the executor runs "on the app host")
  - design:134 (the allowlist is widened by "the package registries the command names"; the only stated property is "no QA-tool secret in the environment")
  - design:169 ("The QA tool's own design must state the sandbox; GAOS refuses … unless the tool lists the `unit` capability")
- Attack path: Code runs on the host that holds the QA tool's `GH_TOKEN`, its database and its API:
  - a merged change;
  - a dependency's install or test hook;
  - with `GAOS_PR_ALLOW_FORKS=1`, a fork commit.

  An empty environment does not stop that code from reading host files, reaching local services or a container socket, or sending data out through a registry host that the command itself chose. The `unit` capability says only that the route exists. It says nothing about isolation.

  The design also leaves the SHA unspecified (design:134-135): it could be the head of a fork rather than the merge commit on the base repository.
- Remediation:
  - Write the sandbox requirements into this design as preconditions for D-104:
    - a separate container or VM per run;
    - no host mounts or container socket;
    - no route to the QA tool's API or database, or to the metadata service;
    - a fixed registry allowlist that the operator configures, not one derived from the command;
    - no credentials, read-only source, CPU and memory limits.
  - Gate enabling on a distinct attested capability (for example `unit-sandbox-v1`) **plus** an operator confirmation recorded in `AutoFixConfigured`, as `CIConfirmed` is today.
  - Specify that `sha` is the merge commit on the base repository.
- Effort: structural

### SEC-4 "Write access limited to `qa-unit/` branches" cannot be enforced by a token, and it would put product-repository write next to SEC-3's code execution - Severity: High
- Component: B4 create mode; QA `GH_TOKEN`
- Evidence: design:136 ("Its write access is limited to `qa-unit/` branches"); design:170; design:182 (open question 2). §8 does not treat this widening as a risk.
- Attack path:
  - GitHub token permissions are granted per repository, not per branch. A `GH_TOKEN` with `contents: write` on product repositories can push to any branch that is not protected, including default branches when protection is absent.
  - That token lives on the host where SEC-3 runs untrusted code. Stealing it gives an attacker direct writes to product code, which goes around every GAOS review flow.
- Remediation:
  - Use a separate identity for unit pull requests (a GitHub App or a dedicated token), never the e2e `GH_TOKEN`.
  - Restrict it with repository rulesets that allow only `refs/heads/qa-unit/*`, and require default-branch protection as a stated precondition.
  - Keep the credential off the executor host.
  - Until all of that exists, make the run-only fallback of open question 2 (design:182) the default.
- Effort: structural

### SEC-5 The `unit:fix` context is unscreened, unbounded output from product code that enters an agent packet - Severity: Medium
- Component: B4 `unit:fix` card context
- Evidence: design:143 ("the failure summary as its context part … stored as an `Artifact` of kind `context` … read by the packet hook"); BR-32 (`pustaka/docs/product/brd.html:1285`); BR-25 (`pustaka/docs/product/brd.html:1271`)
- Attack path:
  - Test output is produced by product code and its dependencies, and it can carry text written to steer the agent (prompt injection).
  - It can also carry secrets that the test environment printed.
  - It reaches a model provider when a person releases the card. The design gives no size cap, no screening when it is stored, no external-storage rule and no data fencing.
- Remediation:
  - Cap the summary (for example 8 KiB) and strip control characters.
  - Run the secret screen (`screenBody`) when it is stored, and refuse or redact on a hit.
  - State that the packet's egress pass covers context artifacts.
  - Store large output by reference (BR-25).
  - Render it in the packet as quoted, untrusted data.
- Effort: small

### SEC-6 A new write power with no audit event - Severity: Medium
- Component: B3 merge outcome; event catalog
- Evidence: design:122-128 (the outcomes are a card reason and a WhatsApp notice on refusal only); BR-05 (`pustaka/docs/product/brd.html:1230`); BR-28 (`pustaka/docs/product/brd.html:1281`)
- Attack path: A merge performed by GAOS leaves only a card's free-text reason. There is no folded event recording the repository, pull request, SHA, guard results or token kind. A wrong or malicious merge cannot be reconstructed or attributed from the record.
- Remediation:
  - Name an event for "spec merged" and one for "merge refused (guard)", with an explicit fold change. BR-05 treats a new type as a schema change.
  - Alternatively, state which existing event carries them.
  - List the change in §6.
- Effort: small

### SEC-7 The interaction with branch protection, and the merge token's permission ceiling, are unstated - Severity: Medium
- Component: B3 token and repository settings
- Evidence: design:109 (permissions listed: `contents: write`, `pull_requests: write`, `checks: read`); design:115 (it also reads `…/status`, which needs commit-status read)
- Attack path: If an automation repository requires a review, the merge fails as a `github-<class>` error. The easy fix for operators is to drop the review requirement or grant the token's account bypass or administration rights. That turns GAOS's guards into the only control and widens the token. The design neither forbids nor detects this.
- Remediation:
  - State that branch protection stays unchanged and that GAOS's merge must satisfy it.
  - Forbid `administration` and bypass rights, and refuse at boot when `permissions.admin` is true.
  - Add `statuses: read` to the documented permissions.
- Effort: quick-win

### SEC-8 Rules about who may merge and who may hold a card are contradicted without being amended - Severity: Medium
- Component: Registers (§4.1, §6)
- Evidence:
  - BR-61 says "GAOS never … merges" (`pustaka/docs/product/brd.html:1307`) and is not in the amendment list (design:93, design:149).
  - BR-44's exception covers only "an e2e card" (`pustaka/docs/product/brd.html:1304`), yet design:142 puts `unit:test` into Needs you while calling it "the existing BR-44 path".
  - BR-19 (`pustaka/docs/product/brd.html:1258`) is not addressed.
- Attack path: Authorisation rules that silently disagree with the code leave a later reviewer or implementer with no authoritative limit. Someone could read "GAOS never merges" as still true and leave out a control, or widen Needs you to other card kinds by analogy.
- Remediation:
  - Amend BR-61 and BR-44 explicitly in D-103 and D-104.
  - State how BR-19 applies: a spec pull request is never a gated task, and why.
- Effort: quick-win

### SEC-9 The merge token's deployment and handling are missing from the change list; non-fine-grained tokens are not refused - Severity: Low
- Component: B3 configuration
- Evidence:
  - design:109 says "a classic token is refused (`ghTokenKind`)", but `dashboard/githubwrite.go:123-131` returns `""` for any other prefix, so "refuse classic" is not the same as "accept fine-grained only".
  - The existing write token's mount is at `deploy/stacks/compose.prod.yaml:193`; there is no equivalent for the merge token in the design.
- Attack path: An app token or another token kind could pass a check that refuses only classic tokens. Without a stated rule, the merge token could be mounted next to the other tokens or read by other paths.
- Remediation:
  - Accept only `tokenKindFine`.
  - Add the compose secret mount and the mode-0600 load through `ghConfigFromVar` (`dashboard/github.go:116-131`).
  - State "never in a workspace, packet or log; read only by `ghMerger`".
- Effort: quick-win

### SEC-10 The spec-path rule is wider than what the QA tool produces - Severity: Low
- Component: B3 guard 4
- Evidence: design:114 allows `api/graphql`, while design:36 and design:48 say GraphQL is never generated. `[^/]+` for the module allows dot-segments. The path is not tied to the card's kind (design:46-49) or its case.
- Attack path: Every allowance beyond what the QA tool emits is room for a pull request that was not generated to pass guard 4.
- Remediation: Use exactly the QA tool's own pattern, filtered by the card's `e2e-kind`. Refuse `.` and `..` segments. Bind the basename to the case code (see SEC-1).
- Effort: quick-win

### SEC-11 Input validation for `kind.<owner/repo>` form keys and the unit `files[]` list is unstated - Severity: Low
- Component: B1 form; B4 request
- Evidence: design:51; design:135, design:141
- Attack path:
  - Unknown repository keys could be stored in the schema-less `repoKinds` property.
  - Pull-request file paths, which contributors control, go unbounded into a request that the QA tool feeds to a model prompt.
- Remediation:
  - Accept `kind.*` keys only for repositories in `Master.Repos`, and only with the values `be` or `fullstack`; refuse everything else.
  - Cap `files[]` (count and length), require the paths to be relative with no `..`, and screen them.
- Effort: quick-win

### SEC-12 Which readiness score gates the merge is unspecified - Severity: Low
- Component: B3 guard 1
- Evidence: design:111 (`ReadinessBand(score)`); BR-09 and BR-10 (`pustaka/docs/product/brd.html:1241-1242`)
- Attack path: Implemented on the raw score, the guard would allow a merge after drift has dropped the effective band to minimum, which goes against BR-08 (`pustaka/docs/product/brd.html:1240`).
- Remediation: State that the guard uses the effective score, read fresh at merge time.
- Effort: quick-win

Counts: Critical=0 High=4 Medium=4 Low=4
