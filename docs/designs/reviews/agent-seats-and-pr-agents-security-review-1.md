# Security review: Agent seats per product, and the three pull-request agents finished

Document reviewed: `/home/ardi/devel/projects/ardihikaru/my-projects/gaos-agent-seats/docs/designs/agent-seats-and-pr-agents.md` (Draft rev 1)

## Paraphrase

### What this proposes

- **Seats.** A product record gets a sorted list of agent ids, `Master.Seats`. An admin-only route, `POST /agents/seats`, changes it and emits `RecordUpdated`. The Agents page gets a product filter. A PR-flow card is planned only when the product seats an agent of the card's role. When nothing is seated, the staging callback answers 409 `unseated`. On its first start, the new version seats the first agent of four roles (`pr-reviewer`, `qa`, `unit-test`, `fixer`) in every product that has repositories.
- **Severity.** Severity becomes a `severity:*` label family. The reviewer sets it, any operator can relabel it, and it is shown as a chip, a table column and a filter (`medium+`).
- **GitHub review event.** Medium and worse post `REQUEST_CHANGES` with the review token. If GitHub refuses with 422, the review is posted again as `COMMENT`.
- **Low findings become review defects.** A low review becomes a `Defect` of kind `review`, pre-triaged `easy`. It goes through the auto-fix pipeline. The base of the draft fix PR is the reviewed PR's head branch, which is exempt from `AllowedBases` under four checks made at dispatch.
- **Two e2e kinds.** The kind (`be` or `fullstack`) is chosen per repository. On a staging deploy, GAOS asks the QA tool which cases have no automation and plans up to 10 `create` cards before the `run` card. With `AutoMergeSpecs` on, GAOS squash-merges the QA tool's spec PR in an automation repository. It uses the write token, requires green check runs and pins the head SHA.
- **Unit tests.** The unit-test agent writes and runs tests and replies with a block. A failed run plans a `BLOCKED` `unit:fix` card that a person releases with the existing unblock move.

### How the existing system works, per this document

- The Agents page lists fixture agents and has no relation to products.
- The PR flows run for every product that owns a repository.
- The reviewer always posts one `COMMENT`. Severity decides only the card's end state and is never stored.
- e2e has one kind. Staging starts one `run` job, and a person merges the QA tool's spec PR.
- A failed unit run only sends a notice.
- The auto-fix pipeline (D-091) turns a triaged defect into a draft PR. It uses a separate write token and is bounded by globs, a CI-secret check, an egress screen, caps and `AllowedBases`.
- Auto-fix settings are restricted to `GAOS_AUTOFIX_ADMINS`. Every back-office POST has same-origin protection.

## Trust boundaries touched

| From | To | Authenticates | Authorises | Validates | If the far side fails |
|---|---|---|---|---|---|
| Operator browser | Dashboard (`POST /agents/seats`, label, unblock, Auto-fix form) | Verified HMAC session (`dashboard/auth.go:328-359`, `:568-586`, BR-55). The rubric's claim that `signedIn()` accepts any cookie is outdated; it now verifies the MAC, expiry and fingerprint (`auth.go:375-396`). | `autoFixAdmin` for seats and the Auto-fix form (`handlers_autofix.go:17-37`). Labels and moves only need a session (`handlers_board.go:205-217`). | Same-origin check (`auth.go:595-612`), body caps | — |
| Dashboard | GitHub (review token) | `GAOS_GITHUB_TOKEN_FILE` | Repository permission | Event value restricted to `COMMENT` or `REQUEST_CHANGES` (design:103) | 422 leads to a `COMMENT` fallback (design:114) |
| Dashboard | GitHub (write token) | `GAOS_GITHUB_WRITE_TOKEN_FILE`, which may be classic (`githubwrite.go:133-142`) | Code checks only: `AutomationRepos`, pinned SHA | Check runs only (design:190) | Needs you (design:192) |
| Staging CI | Dashboard (`/v1/products/{id}/staging`) | Basic auth per product (`staging.go:212-229`) | Ownership of the repository (`staging.go:249-258`) | 4 KiB strict JSON | 409 `capped` or `unseated` |
| Dashboard | QA tool (`GET /cases`, `POST /automation` with `kind`) | Not named by the design | — | Not specified | Not specified |
| Reviewer reply / PR content | Fixer agent prompt | — | — | `screenDefectText` (secret screening only) | — |
| Repository code | Unit-test workspace (executed) | — | — | None named | — |

## Findings

### SEC-1 GAOS gains a merge path that reverses D-091 and pins the wrong SHA - Severity: High
- Component: QA job runner and `ghWriter` (§7.3)
- Evidence: design:188-195 and design:316. `dashboard/githubwrite.go:155-159` says the writer can "merge nothing and update no ref". `pustaka/docs/product/decision-log.html:1961` (D-091: "GAOS never marks a pull request ready, approves or merges it"; the write token is "used for that path only"). `pustaka/docs/product/qa-test-plan.html:768` (TC-677 asserts no merge call). design:236 (D-099 does not say it amends D-091).
- Attack path:
  - **The approval does not cover the merged commit.** A person approves a spec in the QA tool. The design pins the head SHA that GAOS reads at check time (design:190-191), not the SHA that was approved. Anyone with push access to the spec branch, or a compromised QA tool, can push after approval. Once checks go green, GAOS merges content that no person approved into the automation repository's default branch.
  - **The PR's origin is not checked.** The only proof that the PR came from the QA tool is the QA tool's own `pr_url`. Nothing checks the PR's author, the head-branch naming or which files changed. §15 accepts that a QA tool that lies can get any open, green, default-based PR in that repository merged. That bound is weak, because the default branch's workflows then run with the repository's secrets.
  - **A recorded security invariant is reversed silently.** The design does not list the reversal of D-091 and TC-677 as an amendment.
- Remediation:
  - Have the QA tool report the head SHA the person approved. Merge only when the current head equals that SHA.
  - Require the PR author to be the QA tool's GitHub account and the head branch to match the QA tool's prefix.
  - Refuse when any changed file is outside a declared spec path list (and always refuse any change under `.github/`).
  - Record in D-099 that it amends D-091 (2) and the "for that path only" clause. Update TC-677 and the `ghWriter` doc comment, and put the approved-SHA rule into BR-60.
- Effort: small

### SEC-2 The review-fix base exemption is checked at dispatch, but the write is gated elsewhere - Severity: High
- Component: auto-fix pipeline (§6.3)
- Evidence: design:151-158 places the four checks "at dispatch" (`autofix.go:302-307`). The check that gates the write is `fixEligible` (`dashboard/autofixpr.go:76-103`). It refuses any `BaseOverride` outside `AllowedBases` (`:90`) and any defect whose `AutomationType` is not `be` (`:82`). D-091 (8) (`decision-log.html:1972-1973`) says eligibility to open a PR is decided from the record "whoever dispatched the card". The base head is read again at write time (`autofixpr.go:223-227`). design:138 does not set `AutomationType`.
- Attack path: The design does not say how `fixEligible` changes, so one of two things happens:
  - The implementer leaves it alone and the review fix never opens a PR.
  - The implementer relaxes `:82`/`:90` without the four conditions. Then a manually dispatched card, or a card whose PR merged, closed or changed head during the fixer's turn, gets a `gaos/fix-*` branch and a draft PR against a branch the checks were meant to exclude. This is a time-of-check/time-of-use gap on a GitHub write path. It also contradicts D-092 (6), "refused at use" (`decision-log.html:1998`).
- Remediation:
  - Move the four conditions into `fixEligible`/`run`, record-based, and run them again immediately before `createCommit`/`createPR`.
  - Pin the head SHA the reviewer saw when the defect is planned, and refuse (`review-base-changed`) if the branch head has moved.
  - State the `AutomationType` value a review defect carries and the exact new case in `fixEligible`.
  - Add a test that dispatches manually and changes the head in the middle of the turn.
- Effort: small

### SEC-3 The upgrade seating silently turns on new GitHub writes the admins never agreed to - Severity: High
- Component: upgrade (§3.4), review fix (§6), `REQUEST_CHANGES` (§5)
- Evidence: design:79 seats `fixer` and `pr-reviewer` in every product that has repositories. design:151-158 exempts review fixes from `AllowedBases`. design:107-112 adds `REQUEST_CHANGES`. `AllowedBases` is the admin's recorded consent about which bases a fix may target (`dashboard/model.go:445-448`, D-092 (6)).
- Attack path: Take a product whose auto-fix admin enabled auto-fix with `AllowedBases` empty ("no retarget"). On upgrade:
  - It starts receiving automatic draft PRs pushed against arbitrary feature branches.
  - Every seated repository starts getting `REQUEST_CHANGES` reviews.
  - No admin acts and no `AutoFixConfigured` event is emitted.

  The upgrade exists to avoid a silent switch-off, but it causes a silent switch-on of a new write behaviour. This is a missing consent control on a privileged path. I chose High over Medium for that reason.
- Remediation:
  - Do not seat `fixer` in the upgrade; seat only what preserves today's behaviour.
  - Alternatively, gate review fixes behind a separate Auto-fix form switch (for example `ReviewFixes`, default off, recorded by `AutoFixConfigured`).
  - Mention the new `REQUEST_CHANGES` behaviour in the release notes.
- Effort: quick-win

### SEC-4 A seat change does not record its actor, although §15 says it does - Severity: Medium
- Component: `SetSeats` (§3.2)
- Evidence: design:50 says "as the vendor-policy edit does" and that "the event carries no new payload field". `dashboard/vendors.go:198` emits `RecordUpdated` with an empty `EventPayload{}` and no actor in the text. design:318 claims "a seat change is an event naming the actor". Compare `prflow.go:462`, where `SetTaskLabel` records the actor.
- Attack path: An admin, or a stolen admin session, seats the reviewer or fixer and so turns on spending and GitHub writes. The event stream does not record who did it.
- Remediation: Set `Payload.Actor = actorAlias(actor)` and add "by <alias>" to the text, the way `SetTaskLabel` does. Use the actor `gaos` for the upgrade. Assert the actor in `TestSetSeatsRefusesUnknownAgentAndProduct`.
- Effort: quick-win

### SEC-5 The merge token's kind, scope and preconditions are not constrained - Severity: Medium
- Component: write token and `AutoMergeSpecs` (§7.3)
- Evidence: design:195. `githubwrite.go:133-142` accepts a classic token when `GAOS_AUTOFIX_ALLOW_CLASSIC=1`. `autofix_test.go:81-92` shows that a classic token is accepted for auto-fix. `handlers_autofix.go:60-93` rebuilds the whole record from the form, and the design does not say whether `AutoMergeSpecs` requires `Enabled`, `CIConfirmed` or a non-empty `AutomationRepos`.
- Attack path:
  - A classic token belonging to a repository admin can merge past branch protection that does not enforce admins. "GAOS never changes branch protection" is then true, but it does not protect anything.
  - Adding `contents:write` on the automation repository widens the token stored on the host to direct pushes on that repository's default branch.
- Remediation:
  - In `validateAutoFix`, refuse `AutoMergeSpecs` unless the token kind is fine-grained and `AutomationRepos` is non-empty.
  - Say whether `AutoMergeSpecs` depends on `Enabled`.
  - Recommend, or require, a separate fine-grained token limited to the automation repositories for merging.
- Effort: small

### SEC-6 "Green" counts only the first page of check runs and ignores commit statuses - Severity: Medium
- Component: `pullForMerge` (§7.3 step 1)
- Evidence: design:190 reads only `GET /repos/{o}/{r}/commits/{sha}/check-runs`.
- Attack path:
  - The design does not specify paging, so a failing check run beyond GitHub's default page is missed.
  - A failing legacy commit status (the status API) is invisible.
  - A repository with one trivially passing check run and a failing CI status therefore counts as green, and GAOS merges red code.
- Remediation:
  - Read check runs with `per_page=100` and follow every page; fail closed past a fixed page limit.
  - Also read the combined status (`/commits/{sha}/status`) and require `success` when any statuses exist.
  - Additionally require the PR's `mergeable_state` to be `clean`.
  - Add each case to `TestSpecMergeOnlyGreenPinnedAllowed`.
- Effort: small

### SEC-7 QA-tool data now plans cards, and the response is unvalidated and unbounded - Severity: Medium
- Component: staging create-then-run (§7.2)
- Evidence: design:180-182. `prflow.go:202-204` holds the store lock while planning. `prflow.go:257` checks the cap once, before a batch is added. `qajobs.go:249` validates a case code with `validCursor` only on the PR-body path. BR-54 (`brd.html:1349`) and D-091 (3) (`decision-log.html:1962-1963`) limit what QA data may plan. The design does not name the credential for the outbound call.
- Attack path:
  - **Unvalidated input from another origin.** A misbehaving or compromised QA tool returns arbitrary case codes. They go unvalidated into dedup keys and the next outbound start call.
  - **Spend amplification.** A holder of the staging credential posts many distinct SHAs. Each one plans up to 10 `create` jobs (QA-tool LLM spend), and the batch can overshoot the open-card cap.
  - **Lock held across a network call.** If the call is made inside `PlanPullRequestCards`, a slow QA tool holds the store lock and stalls the dashboard.
- Remediation:
  - Validate every case code with `validCursor` and cap the response bytes and item count.
  - Make the call outside the store lock, with a timeout, before planning.
  - Count `create` cards against `prMaxOpen` per card, and add a per-product daily cap.
  - Name the token used for the call.
  - Amend BR-54 explicitly in D-099.
- Effort: small

### SEC-8 Untrusted PR content reaches the fixer, and the CI consent does not cover the author's workflows - Severity: Medium
- Component: review defect (§6.2, §6.3)
- Evidence:
  - design:135: `Excerpt` is the posted review body, which is model output over PR content. `screenDefectText` (`defects.go:280-283`) screens secrets, not instructions.
  - design:158 claims that `CIConfirmed` "covers" the branch. The operator confirmed the repository's CI at its configured base (`autofix.go:138-139`), but the `gaos/fix-*` branch is created from the PR author's head. Its workflows are the author's, checked only by the heuristic in `autofixcheck.go:644-656`.
- Attack path: A collaborator's PR carries text that steers the reviewer to "low" and dictates a fix. The fixer applies it, and GAOS's token commits and pushes it under the `ai-assisted` / `defect-auto-fix` labels. That gives attacker-directed code GAOS provenance, and any branch-push workflow on the author's branch runs as GAOS's account.
- Remediation:
  - Refuse a review fix when the reviewed PR changes any path under `.github/`, or when the head's workflows differ from the default branch's.
  - Mark the excerpt in the packet as untrusted data, not instructions.
  - Record this residual in D-099.
- Effort: small

### SEC-9 Running the unit-test command executes repository code, and the cited anchor does not support it - Severity: Medium
- Component: unit-test builder (§8.1)
- Evidence: design:212 cites "the PR-flows design §4" as already assuming test execution. `docs/designs/gaos-pr-agent-flows.md:81-110` (§4) covers roles only. `:129-131` describes the checkout as "read-only context". BR-32 (`brd.html:1285`) screens only what goes to the provider. R-14 (`brd.html:1439`) says the model runs with full permissions in the workspace.
- Attack path: Merged repository code (test setup, package hooks) and model-written tests run with the agent's permissions on the workspace host. They can reach the network and the workspace's files. The egress pass does not see that traffic, so the attack path runs through a control the design has not named.
- Remediation:
  - Open an open question, or state the sandbox: no network, no credentials in the environment, the filesystem limited to the workspace, and a time limit.
  - Alternatively, have the agent write the tests and have GAOS's CI run them.
  - Fix the citation.
- Effort: structural

### SEC-10 The new label families can be set by any signed-in account - Severity: Low
- Component: label vocabulary (§4.1, §7.1, §8.2)
- Evidence: `prflow.go:434-436` makes only the `defect` family planner-only. `handlers_board.go:205-217` needs only a session. design:168, design:174 (the kind is read from the card's label) and design:226 (`unit:fix` shares the `unit` family with `unit:test`).
- Attack path:
  - Any signed-in account can relabel a card `e2e-kind:be` or `e2e-kind:fullstack` and change which generator the QA tool uses.
  - Relabelling a `unit:test` card as `unit:fix` removes `unit:test` (one label per family). `unitTester.afterRun` then skips the card, and it appears under "Waiting on a person".
- Remediation: Make `e2e-kind` and `unit:fix` planner-only next to `defect`. Keep `severity` operator-settable, recorded through `TaskLabeled`.
- Effort: quick-win

### SEC-11 The fork check for the review-fix base must not follow `prAllowForks()` - Severity: Low
- Component: §6.3 condition 3
- Evidence: design:155 cites `prflow.go:378-380`, but those lines are the author-association check. The fork rule is `prflow.go:375`, and it applies only when `!prAllowForks()`.
- Attack path: If forks are allowed and the implementer copies the existing rule, a fork's `head.ref` names a branch in the fork. The fixer then reads and targets a branch with the same name in the base repository (`autofixpr.go:224-227`).
- Remediation: Make condition 3 always apply (`Head.Repo.FullName == Base.Repo.FullName`) whatever `GAOS_PR_ALLOW_FORKS` says, and correct the citation.
- Effort: quick-win

### SEC-12 The published docs page will describe the merge path - Severity: Low
- Component: `pustaka/docs/integrations/pr-agents.html` (design:241)
- Evidence: design:241. The published site already exposes topology.
- Attack path: The page will describe when GAOS merges, which token permissions it holds and which repositories are automation repositories. That tells a reader which repository and which conditions to target.
- Remediation: Describe the rules generically. Name no automation repository, token kind or environment variable values, and use the invented example.
- Effort: quick-win

Counts: Critical=0 High=3 Medium=6 Low=3
