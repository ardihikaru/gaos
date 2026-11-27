# Security review: release-b4-unit-tests (rev 5)

## What this proposes

Today a merged pull request plans a `unit:test` card that no agent can actually run. The design adds a per-product switch, `UnitQA`, which is off by default. With it on, a merge in a fix-target repository plans a planner-only `unit:qa` card instead. Brain Town and the external-tasks API share one key for it.

- **The run.** The QA-job runner reads the merge commit, `merged_at` and the base from GitHub. It checks a fresh sandbox attestation from the QA tool, then asks the QA tool for `unit-run` or `unit-create`.
- **Failures.** Test or build failures of the existing suite file a "unit" defect. The defect is deduplicated, capped and keyed by repository, base and failing set. Its triage card starts BLOCKED (`ReasonUnitHeld`), and only an auto-fix admin can release it, cancel it or close the defect. The store enforces this and records who did it.
- **Passes.** A later pass of the same test names on the same base closes the defect.
- **Generated tests.** In create mode the QA tool writes tests and waits for a named approver in the QA tool. It then opens a draft pull request on `qa-unit/<sha12>-<job8>` with its own GitHub identity. GAOS records that pull request only after reading it from GitHub, and re-checks it at every new head: branch rule, draft, author, fork, commit authors, file globs. GAOS does not review these pull requests.
- **Live-turn lease.** Cards the QA-job runner drives (`qaToolDriven`) no longer hold the BR-38 lease, and `dispatch` refuses every QA-tool label.
- **Inbox.** GAOS refuses to pass on Inbox answers bound to a unit job.
- **Rule changes.** BR-38, BR-44, BR-54 and D-091 (8) are amended. The agents approve this under the operator's delegation.

## How the existing system works, per this document

- `pr.merged` plans a `unit:test` card for an agent with no checkout. A FAILED card only sends a notice.
- The QA-job runner (`qajobs.go`) drives `e2e:*` cards through `/gaos/automation` and `/gaos/jobs/{id}`, and keeps the job id in the card's reason (`qa-job:`).
- The D-091 auto-fix path goes triage card → triager verdict → fixer → draft `gaos/fix-*` pull request.
- The back office identifies users by a gate-stored session (`sessionOf`), and auto-fix admins are listed in a mode-600 file (`autoFixAdmin`).
- The scheduler and board dispatch apply a one-live-turn-per-product lease to every DISPATCHED or RUNNING task.
- The inbox worker sends answered external questions to the QA tool.

A correction to the rubric, checked against the code: the back office does authenticate. `authority.verify` checks an HMAC before parsing anything, plus expiry and an account fingerprint (`dashboard/auth.go:328-359`). The gate requires a session on every non-public, non-bearer route and stores it on the request (`dashboard/auth.go:568-586`). So "only an auto-fix admin may do X" can be implemented. The findings below take that as given.

## Trust boundaries touched

| From → To | Authenticated by | Authorised by | Validated by | If the far side fails |
|---|---|---|---|---|
| operator browser → back office (move, close, label, `/autofix/unit`) | HMAC session cookie (`auth.go:375-396`) | `autoFixAdmin` for release, cancel, close and settings; **nothing for labels** (SEC-1) | the store guard in `MoveCard` and `CloseDefect` | 401 or 409 |
| GAOS → QA tool (capabilities, automation, jobs, inbox push) | the outbound token | — | `sandboxOK` checks shape and age only | retry, then BLOCKED or FAILED |
| QA tool → GAOS (inbox poll, bearer API) | client token (`api_inbox.go:20-24`) | `ownsItem` | `apiInboxID` | — (SEC-2) |
| GAOS → GitHub (PR read, commits, rules, files) | GAOS GitHub token | — | the §8.2 checks | read error: no note, back off |
| QA unit identity → GitHub `qa-unit/*` → repository CI | the identity's token | the ruleset (unverified, SEC-5) | GAOS checks afterwards, detective only | the statement in §8.4 |
| GAOS fixer → GitHub `gaos/fix-*` into `UnitBase` | GAOS write token | `AllowedBases`, not re-checked (SEC-3) | — | — |

## Findings

### SEC-1 An operator-settable e2e label forges `qaToolDriven` and breaks the BR-38 lease - Severity: High
- Component: live-turn lease, labels, QA-job runner
- Evidence:
  - `docs/designs/release-b4-unit-tests.md:166-173` keys the lease exemption on `qaToolLabel(labels)` plus a `qa-job:` reason.
  - `dashboard/labels.go:67-68` and `dashboard/prflow.go:549-557`: only the `e2e-kind` family is planner-only. `e2e:run` and `e2e:automation` can be set by any signed-in account. `setTaskLabelLocked` does not check the card's status (`prflow.go:558-588`), and `handleSetLabel` checks only for a session (`handlers_board.go:206-219`).
  - `dashboard/qajobs.go:252-262`: `Tick` calls `start` on any PR card that has an e2e label and is PENDING, READY, **RUNNING** or AWAITING_INPUT. `PRCards` returns every card with a `DedupKey`, defect cards included (`prflow.go:718-720`).
  - `dashboard/qajobs.go:393` then writes a `qa-job:` reason.
- Attack path: Any signed-in back-office account, not only an admin, adds `e2e:run` to a card that is RUNNING a model turn, for example a `defect:triage`, `defect:easy` or `pr-review:auto` card.
  - On its next Tick the QA-job runner treats the card as a lost e2e job. It starts a real QA run (`caseCode "*"`) and writes `qa-job:` into the reason.
  - `qaToolDriven` now holds, so `liveIn` and `liveTurnLocked` skip the card while its model turn is still live. A second model turn can be dispatched into the same product.
  - That defeats the claim at design line 173 that "no two model turns ever run at once".
  - The same label added to a released, READY held triage card makes `dispatch` refuse it for good, and makes the runner start an e2e job for a defect card.
  - Before this design the lease still counted such a card. The exemption is what turns a label change into a lease bypass. I was torn between Medium and High; High because it defeats a BR-38 control from a non-admin account.
- Remediation:
  - Make the `e2e` family planner-only, like `defect`, `source` and `e2e-kind`. At minimum, refuse adding a `qaToolLabel` to a card that holds another job label or is not PENDING.
  - Key `qaToolDriven` on something only the planner sets, such as the card's job in its `DedupKey` (`:e2e-*`, `:unit-qa`, `qa-run:`, staging keys), rather than on a mutable label.
  - Make the runner's `start` refuse a RUNNING card whose reason does not already carry `qa-job:`.
  - Extend TC-858: add `e2e:run` to a RUNNING `defect:triage` card, and the lease must still hold.
- Effort: small

### SEC-2 The "never relay a unit approval" control covers only the push, and relies on an id the QA tool chooses - Severity: Medium
- Component: inbox (§8.5)
- Evidence:
  - `docs/designs/release-b4-unit-tests.md:481` adds the refusal only in `inbox_worker.go` `send`.
  - `dashboard/api_inbox.go:17,24`: "this GET is what keeps working when the push does not". The poll path is registered at `dashboard/api_v1.go:439`, and `apiInboxGet` returns the recorded decision (`dashboard/api_inbox.go:222-248`).
  - The answer itself is still accepted through `POST /products/{id}/inbox/{externalId}/answer` (`dashboard/main.go:611`).
  - The binding depends on "the QA tool's `externalId` names `qa-job-<id>`" (design line 481), a naming the QA tool controls (`api_inbox.go:37`).
- Attack path:
  - If the QA tool raises a `unit-create` approval as a GAOS Inbox question, any signed-in GAOS account can answer it. The QA tool can then read the answer through the poll endpoint even though the push was refused.
  - A QA tool that names the item any other way skips the refusal entirely.
  - This matters because §11's own rule (that the QA tool never honours a GAOS answer) is the only remaining control, and the design states "GAOS enforces it too".
- Remediation:
  - Refuse at answer time. Make `handleExternalAnswer` and `apiInboxAnswer` reject an item bound to a unit job, so no decision is ever recorded.
  - Make `inboxState` never report one.
  - Bind items to unit jobs through a GAOS-side record (the job ids GAOS started, from `qaTracked` or the card reason) and a QA-tool-independent field, not through the `externalId` format.
  - Extend TC-879 to cover the answer route and the GET poll.
- Effort: small

### SEC-3 A unit defect's fix base is never re-checked against the current allowed bases - Severity: Medium
- Component: auto-fix, base selection (§7.5)
- Evidence:
  - `docs/designs/release-b4-unit-tests.md:407` says the fixer uses `UnitBase`.
  - `docs/designs/release-b4-unit-tests.md:412` says the claim loop's base check (`autofix.go:350-361`) is "not taken" for unit defects.
  - The base is validated once, when the run starts (design line 183).
  - In the existing code, revoking a retarget base "refuses it for every later attempt" (`dashboard/autofix.go:348-355`), but only when `BaseOverride` is set. `defectFixer.run` takes the base name without re-checking it (`dashboard/autofixpr.go:235-243`).
- Attack path:
  - An admin removes a branch from `AllowedBases`, or changes `BaseBranch`, for example after finding it sensitive or retired.
  - Unit defects filed earlier against that base keep `UnitBase`. When released, and on a second attempt after `Gen++` (design line 411), the fixer still opens a model-written `gaos/fix-*` pull request against the revoked base.
  - The control works for retargets and silently does not work for unit fixes.
- Remediation:
  - In the claim loop and in `defectFixer.run`, require `UnitBase` to be the current effective base or in `AllowedBases`. Otherwise block with `BlockBaseNotAllowed`, as the retarget path does.
  - Change the §7.5 table row to say so, and add a case to TC-872.
- Effort: quick-win

### SEC-4 The new BLOCKED → READY machine path is not specified, and the hold's path table does not cover the FAILED exit - Severity: Medium
- Component: held card (§7.3), attestation backoff (§5.3)
- Evidence:
  - `docs/designs/release-b4-unit-tests.md:243` moves a `ReasonUnitAttest` card "BLOCKED → READY (`ResolvePRCard`, rule `unit-attest`)".
  - `ResolvePRCard` accepts only RUNNING, AWAITING_INPUT, BLOCKED, DONE and FAILED, and has no rule parameter (`dashboard/prflow.go:600-614`, `:626`).
  - `resolveFrom[StatusFailed]` already includes BLOCKED (`dashboard/prflow.go:605`), and any operator may then retry Failed → Ready (`dashboard/board.go:232-233`).
  - The "every path out of BLOCKED" table (design lines 376-387) lists neither path.
- Attack path:
  - The obvious way to build line 243 is to add `StatusReady: {StatusBlocked}` to `resolveFrom`. That would let every `ResolvePRCard` caller (fixer, reviewer, test planner, QA runner) move a `ReasonUnitHeld` card to READY, where the auto-fix claim picks it up (`autofix.go:311`), with no admin involved.
  - Separately, any present or future `ResolvePRCard(…, StatusFailed, …)` on a held card takes it out of BLOCKED, and a non-admin's Retry then releases it.
  - The hold's guarantee rests on no such caller existing, which the design does not state.
- Remediation:
  - Use a dedicated store method that moves only `unit:qa` cards whose reason equals `ReasonUnitAttest` to READY.
  - In `resolveCardLocked`, refuse any target for a card whose `BlockReason == ReasonUnitHeld`, apart from the `PassUnitDefects` cancel.
  - Add both rows to the §7.3 table, and add a TC: `ResolvePRCard` FAILED or READY on a held card is refused.
- Effort: small

### SEC-5 Check 0 proves a branch rule exists, not who may push to `qa-unit/*` - Severity: Medium
- Component: `qa-unit` pull request re-check (§8.2), identity (§11)
- Evidence:
  - `docs/designs/release-b4-unit-tests.md:444` passes check 0 when `rules/branches/<head ref>` "lists a rule of type `update`".
  - Line 449 makes commit authorship advisory, and calls check 0 "the control".
  - Line 522 requires the ruleset to let only the unit identity update `qa-unit/*`.
- Attack path:
  - The unit identity has to push past the same update rule, so it must be exempt from it. The design does not say that the read it relies on shows which actors are exempt, or that it checks the rule is enforced rather than in evaluation.
  - Any other exempt actor (a repository admin, another app or token) can push to an open `qa-unit/*` branch with forged commit emails, and checks 0-6 can all still pass.
  - The result is then marked `checked@<head>`, its e2e card is skipped (§8.3), and a person may merge it on the strength of that label.
- Remediation:
  - State what check 0 does and does not prove, and add it to the D-105 residuals.
  - Extend the `CIConfirmed` / §8.4 statement so the operator confirms that the unit identity is the only actor exempt from the rule.
  - Where the GAOS token can read the ruleset, verify that it is enforced and that its exempt actors are exactly the unit identity. Otherwise show the result on the page as "unverified", not `checked`.
- Effort: small

### SEC-6 The check-0 URL is built from an unchecked head ref before check 3 checks it - Severity: Low
- Component: `(*ghClient).branchRuleTypes`
- Evidence:
  - `docs/designs/release-b4-unit-tests.md:442-448`: check 0 reads `rules/branches/<head ref>` before check 3 tests that the ref starts with `qa-unit/`.
  - Line 436 keeps refused rows, whose head ref was never confirmed, and re-checks them.
- Attack path: The QA tool reports a `pr_url` for a pull request in the same repository whose branch name contains URL-significant characters. The authenticated GitHub read is then made against a different path than intended, and its answer is taken as the branch-rule verdict.
- Remediation: Run check 3 before check 0. Require the ref to match `^qa-unit/[0-9a-f]{12}-[A-Za-z0-9]{8}$` before any request, and escape each path segment.
- Effort: quick-win

### SEC-7 Where the branch-rule refusal in `SetUnitQA` runs is undecided - Severity: Low
- Component: settings (§4.1, §8.2)
- Evidence:
  - `docs/designs/release-b4-unit-tests.md:105` lists the store's three refusals, none of which needs GitHub.
  - Line 444 says "`SetUnitQA` with `Create` on reads the same for each target repository".
  - The handler's refusal table (lines 109-114) has no branch-rule row. The store has no GitHub client, and the design itself keeps file and network reads out from under the store lock (line 370).
- Attack path: Two implementers will diverge, and the refusal can end up in neither place. Create mode could then be switched on for a repository with no rule, until the first poll flags it after CI has already run.
- Remediation: Move the refusal into the §4.1 handler table as a fresh read per target repository, with its text, and cover it in TC-877. Remove "`SetUnitQA` … reads" from line 444.
- Effort: quick-win

### SEC-8 GitHub-derived paths go into WhatsApp group notices - Severity: Low
- Component: notices (§5.5)
- Evidence:
  - `docs/designs/release-b4-unit-tests.md:274` sends `a unit-test pull request failed its check: <verdict>`.
  - The verdicts include `workflow file: <path>` and `files outside TestGlobs: <path>` (line 465).
  - D-088 keeps repository names out of notices (line 263).
- Attack path: File paths chosen by whoever pushes to the branch reach an off-platform group chat. They are screened, but they can still reveal internal structure, or carry text meant for the people in that chat.
- Remediation: Send only the verdict class in the notice (`workflow file`, `files outside TestGlobs`). Keep the path on the Pull requests page.
- Effort: quick-win

### SEC-9 Route anchors have drifted after the TaskMgr merge - Severity: Low
- Component: cross-references (§4.1, §7.3)
- Evidence:
  - `docs/designs/release-b4-unit-tests.md:107` cites `main.go:577` for `POST /products/{id}/autofix`, and line 372 cites `main.go:578` for the close route.
  - They are now at `dashboard/main.go:594` and `:595`.
  - `main.go:577` is now `PUT /products/{id}/brd`.
- Attack path: An implementer following the anchor puts the new admin-only route next to the wrong handler group. This is low risk, because the gate covers every non-public route (`auth.go:516-524`).
- Remediation: Correct the anchors.
- Effort: quick-win

Counts: Critical=0 High=1 Medium=4 Low=4
