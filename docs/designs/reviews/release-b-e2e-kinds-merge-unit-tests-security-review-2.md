## What this proposes

The document adds four parts, each shipped as its own phase.

- **B1, repository kinds.** Each product repository can carry a kind, `be` or `fullstack`, or no kind at all. An admin sets it through `POST /agents/assign` using `kind.<owner/repo>` fields. Keys are accepted only for the product's own repositories, and values only from `be|fullstack|""`. When a repository has a kind, the planner adds a planner-only label, `e2e-kind:*`, to its e2e cards. When the QA tool's new `GET /api/v1/gaos/capabilities` route lists `kind`, GAOS sends `kind` on `POST /api/v1/gaos/automation`, taking it from the label and not from the current setting. The QA tool then filters the cases it runs by level and limits generation to that kind's folder. No kind means today's behaviour.
- **B2, creates on a staging deploy.** Inside one lock, the authenticated staging callback plans today's run card and, for a repository with a kind, create cards for the cases that are planned but not yet implemented. Each create card is keyed per product and case with a retry counter. They have their own caps (2 per callback, 4 open), and a failed one may retry after 7 days. A create card ends Done once its spec pull request is open. BR-54 is amended.
- **B3, merging the spec.** After a person approves a spec, the QA tool itself enables GitHub auto-merge on the spec pull request. It does this only if the default branch requires at least one status check and the repository allows auto-merge. GAOS gets no merge power. When the fix-PR poller sees a `qa` Pull requests row become merged, GAOS plans one single-case run card (`qa-merged/...`).
- **B4, unit tests.** The QA tool gains a sandboxed `unit_run`, `POST /api/v1/gaos/unit`, and a separate GitHub identity restricted by rulesets to `qa-unit/*`. GAOS turns this on per product with `UnitTestsViaQA`, which also needs `UnitSandboxConfirmed` and the `unit` capability. A merged pull request then plans a `unit:qa` card, sent with the merge commit SHA and a bounded `create_for` list. Created tests open as a draft pull request after a person approves them. A failed run becomes a `unit` Defect, screened and held Blocked (`ReasonUnitHeld`) until a person moves it to Ready. From there the existing D-091 triage and auto-fix pipeline takes over.

## How the existing system works, per this document

- The staging callback uses HTTP Basic per product and plans one `e2e:run` card that runs every case.
- A `pr.opened` event plans an `e2e:automation` card, with the case taken from a `QA-case:` line.
- The QA job runner starts and polls jobs. It puts a card in Needs you while the QA tool waits for an approval (BR-44), ends the card Done on success, and fails a job that stays active longer than 40 minutes.
- A job's spec pull request becomes a `qa` Pull requests row, and the fix-PR poller refreshes that row.
- GAOS keeps a synced copy of the QA catalog.
- `unit:test` goes to an agent whose workspace holds no product code.
- D-091 turns a defect into triage, then into a draft fix pull request, with screening, caps and the write token.
- The QA tool, as stated assumptions:
  - It opens spec pull requests under its `GH_TOKEN`, and nothing merges them.
  - It has no unit runner.
  - Its only executor runs on the app host against staging, with an outbound allowlist.

I could paraphrase every section, so there are no comprehension findings.

## Trust boundaries touched

| From → To | Authenticates | Authorises | Validates | Far side fails |
|---|---|---|---|---|
| Operator browser → `/agents/assign` (kinds) | Signed session (`dashboard/auth.go:375-396`) | `autoFixAdmin`, fail-closed mode-600 list (`dashboard/handlers_assign.go:102`, `dashboard/handlers_autofix.go:17-37`) | 16 KiB cap (`dashboard/handlers_assign.go:106`); kind keys and values (design:55) | 400 |
| Operator browser → board move (releases the unit hold and the create retry) | Signed session | **None beyond sign-in** (`dashboard/handlers_board.go:57-60`) | `CardMove` carries no actor (`dashboard/board.go:127-131`) | — |
| Deploy hook → staging callback | Basic auth per product | Product owns the repository | Strict body | Caps |
| GAOS → QA tool (capabilities, automation, unit) | Bearer | Self-asserted capabilities, cached 10 min, stale kept on error (design:63) | — | Card fails |
| QA tool → GAOS (job results, `pr_url`, unit failures) | Same channel | `recordQARow` accepts only listed automation repositories (`dashboard/qajobs.go:409-428`) | Unit excerpt screened (design:117); `Files` not validated | — |
| QA tool → GitHub (auto-merge on the automation repository) | QA `GH_TOKEN` | Branch protection: "at least one required check" (design:89) | Nothing binds the merge to the approved content | Left for a person |
| QA unit identity → product repository (`qa-unit/*`) | Separate App or token | Rulesets (design:104) | Unverified by GAOS | — |
| Merged spec → QA executor (via the GAOS `qa-merged` run) | — | Automatic (design:95) | — | — |

## Findings

### SEC-1 Auto-merge is not bound to the content the person approved - Severity: High
- Component: B3, the QA tool's auto-merge contract, and GAOS's automatic `qa-merged` run
- Evidence: design:15, design:89, design:91, design:95, design:174; today a person performs the merge (design:40).
- Attack path:
  - The QA tool enables `--auto` once, after the approval (design:89). GitHub keeps auto-merge enabled when an account with write access pushes further commits to the head branch.
  - So any collaborator, or any holder of the QA `GH_TOKEN`, can push to `qa-auto/<code>` after the approval. Once the required check passes, GitHub merges content that nobody approved.
  - GAOS then plans and runs that case automatically (design:95). The unapproved spec executes on the QA executor against staging.
  - In rev 1 the human merge was the one check independent of the QA tool. Rev 2 removes it, and §8 (design:174) presents the approval as the gate without saying that the approval and the merge are not tied to the same commit.
- Remediation:
  - Make the contract in §4.1 state that the QA tool enables auto-merge pinned to the approved head (`--match-head-commit <pushed_sha>`), and disables it if the head moves.
  - Add an operator precondition beside §13 item 2: a ruleset that lets only the QA bot identity update `qa-auto/*`.
  - On the GAOS side, have the QA job report `pushed_sha`. Plan the `qa-merged` run only when the merged row's head SHA equals it. Otherwise plan nothing and say so in the row or a notice.
- Effort: small

### SEC-2 "At least one required status check" can be satisfied by the same token that opens the pull request - Severity: Medium
- Component: B3 gate, automation-repository branch protection
- Evidence: design:89, design:91 ("the repository's required checks are the real gates"), design:213 (open item 2)
- Attack path:
  - A required check that is not bound to a source can be satisfied by any commit status posted by an account with status write. That includes the QA tool's own `GH_TOKEN`.
  - A trivially passing check, or one the QA tool posts itself, also meets "at least one".
  - Either way, the "real gate" can collapse into the QA tool vouching for itself.
- Remediation:
  - State that the required check must be pinned to a specific GitHub App or Actions source (rulesets' required check with an integration source).
  - State that it must run the spec's lint, typecheck and dry run.
  - State that the QA tool's identity must have no bypass or admin rights.
  - Have the QA tool verify these conditions and report them, not just "protected with one check".
- Effort: quick-win

### SEC-3 The unit sandbox gate is a one-time checkbox plus a self-asserted capability that can go stale - Severity: Medium
- Component: B4 switch (`UnitTestsViaQA`, `UnitSandboxConfirmed`), capability cache
- Evidence: design:63 (on an error the last known set is kept, cached 10 min), design:105 ("configured", not "restricted"), design:109, design:110 ("the card keeps one driver for its life"), design:175
- Attack path:
  - `unit` means the sandbox and identity are *configured*. It does not attest isolation or the ruleset restriction.
  - The operator's confirmation is a boolean. It records nothing about what was confirmed and never expires.
  - If the QA tool's sandbox is removed or degraded, two things keep work flowing to a host that may now run product code unsandboxed:
    - a capability error keeps the stale positive set;
    - already planned `unit:qa` cards keep their driver.
  - Every merged pull request's code and dependencies then execute there.
- Remediation:
  - Have the capability answer carry a structured attestation: the sandbox profile id or version, and the unit identity's login and ruleset id. Show it when the operator confirms, and record the confirming account and that attestation in `AutoFixConfigured`. Clear the confirmation when the attestation changes.
  - For `unit`, treat a capability error as the capability being absent.
  - At `start`, re-check the switch, the confirmation and the capability for `unit:qa`. Fail closed with a class such as `unit-not-capable` instead of honouring the planning-time decision.
- Effort: small

### SEC-4 Model-written test code in the product repository's CI is not constrained on paths or secrets - Severity: Medium
- Component: B4 create mode, the `qa-unit/*` draft pull request
- Evidence: design:104 ("test files only", rulesets restricted to `refs/heads/qa-unit/*`), design:112, design:176
- Attack path:
  - The QA tool's model writes tests after reading product source. That source can contain text written to steer a model.
  - The draft pull request comes from a same-repository branch, so the product's `pull_request` workflows run on it, usually with repository secrets.
  - Branch rulesets limit *which ref* the identity can write. They do not limit *which files*.
  - "Test files only" is enforced only by the QA tool's own model output, and GAOS does not check it.
  - A generated test, or a file outside the test globs such as a workflow, can exfiltrate CI secrets. The only thing in the way is the person's approval of a diff.
- Remediation:
  - State that the unit identity has no `workflows` permission.
  - State that a push ruleset, where available, restricts it to the test globs, or that the QA tool refuses any changed path outside `AutoFix.TestGlobs` before pushing.
  - When GAOS records the unit pull request, check its files (`pullFiles`) against `TestGlobs` and flag or close the card on a mismatch.
  - Record the operator precondition that workflows on `qa-unit/*` require approval or run without secrets.
- Effort: small

### SEC-5 "A person decides" on the unit hold and the create retry is any signed-in account, and is unattributed - Severity: Medium
- Component: B4 `ReasonUnitHeld` release; B2 create retry release
- Evidence: design:74 ("a person may release it sooner by moving the card to Ready"), design:117, design:177; `dashboard/handlers_board.go:57-60` (`requireBO` only); `dashboard/board.go:127-131` (`CardMove` has no actor)
- Attack path:
  - Releasing the held unit triage card sends the defect excerpt, which comes from product code's test output, to a model.
  - The release can also lead to a draft fix pull request under the GAOS write token.
  - Configuring auto-fix is restricted to `GAOS_AUTOFIX_ADMINS`, but this release is not. Any signed-in back-office account, or one whose session was stolen, can release it, and the record does not say who did.
  - The design's "a person releases" (design:177) implies an identity check that does not exist. The `qa-epic-added` hold (BR-54, `pustaka/docs/product/brd.html:1350`) has the same gap, but this design adds a hold whose release leads to write-token output.
- Remediation:
  - Restrict moving a card out of a `ReasonUnitHeld` hold to `autoFixAdmin`. The check belongs in `MoveCard`, keyed off `operatorHolds`.
  - Record the session's account on that move (in the event or the reason).
  - Say so in §5.3 and §8.
- Effort: small

### SEC-6 Unit failure `Files` from the QA tool are not validated - Severity: Low
- Component: B4 `PlanUnitDefect`
- Evidence: design:117 (`Files` the failing tests' files); design:178 validates only `create_for`. `Defect.Files` is shown in the back office (`dashboard/autofix_view.go:826-828`) and goes into the fix text (`dashboard/autofix.go:403`).
- Attack path: Paths come from the sandbox output, which product code influences. Without bounds, they are unscreened free text that ends up in a stored record, the UI and agent-facing text.
- Remediation:
  - Give `Files` the same rule as `create_for`: at most N entries, bounded length, relative, no `..`, no control characters.
  - Pass them through `screenDefectText` together with the excerpt.
- Effort: quick-win

### SEC-7 The `qa-merged` run is QA data planning a card, but BR-54's amendment names only the staging creates - Severity: Low
- Component: Registers
- Evidence: design:79, design:95, design:121; BR-54 (`pustaka/docs/product/brd.html:1350`)
- Attack path: A row recorded from a QA job's `pr_url` turning merged now plans an automatically executed run card. The amended text quoted at design:79 covers only the staging callback. A later reader of BR-54 would not know that a QA-sourced row can create work that runs.
- Remediation:
  - Extend the D-103 amendment text to the `qa-merged` run.
  - Record `merged_by` and the merge SHA on the row and in the run card's reason, so the automated merge can be attributed.
- Effort: quick-win

### SEC-8 The design names a private automation repository - Severity: Low
- Component: Design document (tracked, and its content flows into `agents.html`)
- Evidence: design:213, against D-085 (`pustaka/docs/product/decision-log.html:1787-1790`: no tracked file names one operator's private instance); design:127 (`agents.html` is rewritten from this plan)
- Attack path: An operator-specific repository name ends up in an open-source tracked file. From there it can reach the published docs site, adding to the topology the site already exposes.
- Remediation:
  - Reword it to the worked example (`acme/route-e2e`) or to "the operator's automation repository".
  - Confirm that `check-workspace.sh`'s guard covers `docs/designs/`.
- Effort: quick-win

Counts: Critical=0 High=1 Medium=4 Low=3
