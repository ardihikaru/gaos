# Security review: `docs/designs/release-b4-unit-tests.md` (rev 1)

## Paraphrase

### What this proposes

A per-product switch, `UnitQA`, which is off by default. When it is on, a merged pull request plans a `unit:qa` card instead of today's `unit:test` card. The scheduler never dispatches that card. The QA-job runner drives it: it asks the QA tool for a `unit-run` or `unit-create` job pinned to the merge commit (`repo`, `sha`), then polls the job.

The outcomes split two ways:
- **The job itself fails** (checkout, sandbox, timeout): the card goes FAILED and no defect is filed.
- **Tests fail:** GAOS files a deduplicated, daily-capped `unit` defect. Its triage card is born BLOCKED with `ReasonUnitHeld`. Only an auto-fix admin may release it, after which the existing D-091 path runs (triage, fixer, draft fix PR).

In `unit-create` mode, the QA tool writes the missing tests and waits for a person's approval in the QA tool. It then opens a draft PR on `qa-unit/<sha12>` using a separate GitHub identity that has no `workflows` permission. GAOS records that PR as a `qa-unit` row and checks the reported files against `TestGlobs`. A mismatch only produces a note and a notice; GAOS never closes the PR.

The switch is saved on the Auto-fix page by an auto-fix admin. It can only be turned on when the QA tool lists the `unit` capability and returns a sandbox attestation of an exact shape that is less than 7 days old. `Create` also needs `CIConfirmed`.

### How the existing system works, per this document

- Today a merge plans `unit:test`. An agent with no checkout "runs" it, and a FAILED card only sends a notice.
- E2e cards are already driven by `qaJobRunner`, never by the scheduler.
- Review defects (`PlanReviewDefect`) are the model for a defect born from a non-QA source. They use `AutomationType "be"`, a source slice from `buildReviewContext`, and D-091's caps and `fixEligible`.
- Auto-fix admins are the accounts listed in the mode-600 file `GAOS_AUTOFIX_ADMINS`.
- Every signed-in account may move any card today.

**Note on the rubric:** its boundary table says the back-office cookie checks nothing. That is out of date in this tree. `authority.verify` checks an HMAC signature, an expiry and a per-account fingerprint (`dashboard/auth.go:330-358`), and `signedIn` and `sessionFrom` use it (`dashboard/auth.go:375-396`). So "only an auto-fix admin" can be implemented: there is a real identity (`dashboard/handlers_autofix.go:17-37`).

## Trust boundaries the design touches

| From | To | Authenticates | Authorises | Validates | If the far side fails |
|---|---|---|---|---|---|
| dashboard (runner) | QA tool | bearer token (`qaclient.go:217`) | QA tool's own | body built from the card key and product record only (§4.2); answer capped at 2 MiB (`qaclient.go:232-238`); `unit` fields screened and capped (§5) | job failure gives FAILED with no defect (§5); capability errors keep the last known set (`qaclient.go:94`) |
| QA tool | GitHub (unit identity) | fine-grained token (§9) | ruleset limited to `qa-unit/*`, no `workflows` | GAOS checks are after the fact and use QA-tool-reported data (§7) | GAOS notes it and does not close the PR |
| operator browser | back office (Auto-fix page, board move) | HMAC session cookie | `autoFixAdmin` file | form parsing | n/a |
| dashboard | GitHub (read) | GAOS read token | n/a | `shaRe` on `merge_commit_sha` (§3) | no card is planned |
| dashboard | model provider (triage and fixer packets) | gateway | n/a | egress screen in `buildReviewContext` (`reviewdefect.go:337`) | n/a |
| dashboard | GitHub (write, fixer) | GAOS write token | `fixEligible` | `d.TargetRepos` | n/a |

The design adds no new endpoint, origin or event type (§8). The new security-relevant surface is two QA-tool response fields that become stored data and model input, a new mover-sensitive block reason, and a new source of fixable defects.

## Findings

### SEC-1 A unit defect is fixable in any merged repository, not only in the auto-fix targets - Severity: High
- Component: `unitdefect.go` (new), `fixEligible`, planner gate
- Evidence:
  - The design sets `TargetRepos` to `[Repo]` for whichever repository merged (`docs/designs/release-b4-unit-tests.md:134`).
  - The planner gate checks only the switch and the `unit-test` role (`docs/designs/release-b4-unit-tests.md:50`).
  - `fixEligible` trusts the defect's own `TargetRepos`, not `AutoFix.TargetRepos` (`dashboard/autofixpr.go:97-102`).
  - The precedent this design copies refuses a repository outside `af.TargetRepos`, or inside `af.AutomationRepos`, before planning (`dashboard/reviewdefect.go:106-107`).
  - §12 lists "repositories outside `TargetRepos`" as a non-goal (`docs/designs/release-b4-unit-tests.md:205`), but nothing in §3 or §6 enforces it.
- Attack path:
  - A merge in any repository the product follows (`m.Repos`), including an automation repository such as `acme/route-e2e`, files a unit defect whose own record says the repository is a kept target.
  - Once someone releases the triage card and triage says easy, the fixer can open a pull request with the GAOS write token in a repository the operator never listed as a fix target.
  - Test names and failing files from that repository also reach the triage model.
  - This is a missing authorisation check on the most privileged write path GAOS has.
  - The write token's own repository scope may limit the damage. That scope is in a credential file I did not open, so it is unverifiable here.
- Remediation:
  - Plan a `unit:qa` card, and file a unit defect, only when `Repo` is in `AutoFix.TargetRepos` and not in `AutoFix.AutomationRepos` (the `reviewPlanRefusalLocked` rule).
  - Otherwise the card ends DONE with a "not a fix target" reason.
  - Set `d.TargetRepos` to the intersection with `af.TargetRepos`, not to `[Repo]`.
  - Add a test: a merge in a repository outside the targets, or in an automation repository, files no fixable defect.
- Effort: quick-win

### SEC-2 A Blueprint sync releases the held triage card without an admin - Severity: High
- Component: hold enforcement (§6.3), `releaseDriftLocked`, `operatorHolds`
- Evidence:
  - The design names only two guards: the auto-claim and the handler 403 (`docs/designs/release-b4-unit-tests.md:144-147`).
  - `releaseDriftLocked` moves every BLOCKED task of the desk to READY unless `operatorHolds(BlockReason)` (`dashboard/workspace.go:384-399`).
  - `operatorHolds` lists only the design-gate and QA-epic reasons (`dashboard/sendback.go:153-158`).
  - The decision log already fixed exactly this hole for QA-added cards (`pustaka/docs/product/decision-log.html:1778`).
  - `sendback.go` is not in the change list (`docs/designs/release-b4-unit-tests.md:183-192`).
- Attack path:
  - A drift block (BR-39) on the product's workspace, followed by any sync, moves the held card to READY with no admin involved. This breaks BR-63 as proposed.
  - The reason is not cleared on that path (only `board.go:397-401` clears it), so the auto-claim guard still refuses the card.
  - The operator's manual dispatch passes no claim, though (`dashboard/board.go:466-467`). Whether the new 403 also covers a Ready-to-Working drag is not stated.
  - So any signed-in account can then start triage on the card. That is the authorisation bypass the hold exists to prevent.
- Remediation:
  - Add `ReasonUnitHeld` to `operatorHolds`.
  - Put `sendback.go` in the change list.
  - Add a test that a drift release leaves a unit-held card BLOCKED.
  - Have §6.3 list every path that moves a BLOCKED card (board move, drift release, design-gate release, send-back, recovery) and say which ones the hold covers.
- Effort: quick-win

### SEC-3 The admin-only release sits in the handler, and the claimed attribution cannot be implemented - Severity: Medium
- Component: `handleMoveCard`, `MemStore.MoveCard`
- Evidence:
  - The design checks `autoFixAdmin` in the handler and claims the move is "attributed to the admin in its event" (`docs/designs/release-b4-unit-tests.md:147`).
  - `MoveCard(ctx, productID, taskID, mv)` carries no actor (`dashboard/board.go:330`).
  - Every existing hold lives inside the store's `MoveCard`, under the lock (`dashboard/board.go:360-377`).
  - An actor reaches `TaskMoved` only through `moveTaskWithLocked`'s `extra.Actor` (`dashboard/runner.go:457-474`).
  - D-103 records that board releases are "an unattributed board move" (`pustaka/docs/product/decision-log.html:2150`).
  - `board.go` and `store.go` (for the `MoveCard` signature) are missing from the change list (`docs/designs/release-b4-unit-tests.md:189`).
- Attack path:
  - The handler reads the reason and then calls the store. Any other or future caller of `MoveCard` skips the check.
  - The release event would not name the admin, so the audit trail for "an admin released this" does not exist.
- Remediation:
  - Move the check into `MoveCard`: pass the session's account, refuse `MoveUnblock` and `MoveDispatch` on a `ReasonUnitHeld` card unless `autoFixAdmin(actor)`, and emit through `moveTaskWithLocked` with `Actor`.
  - List `board.go`, `store.go`, `store_arcade_blueprint.go` and `readOnly` in the change list.
- Effort: small

### SEC-4 Any signed-in account can cancel the held card, and dedup then suppresses that failure permanently - Severity: Medium
- Component: §6.1 dedup, §6.3 hold
- Evidence:
  - Cancel is legal from BLOCKED (`dashboard/board.go:238-239`, `dashboard/taskstate.go:92`).
  - The 403 covers only the move out to Ready (`docs/designs/release-b4-unit-tests.md:147`).
  - "The same failing set seen again updates `LastSeen` and plans nothing" (`docs/designs/release-b4-unit-tests.md:135`).
- Attack path: a non-admin account cancels the triage card. The defect is never re-planned for that failing set, so a real failure is silenced without anyone with auto-fix authority deciding it.
- Remediation:
  - Apply the admin check to `MoveCancel` on a held card too.
  - Alternatively, state that a canceled held card lets the next sighting of the same set re-plan it, and decide whether that re-plan counts against the daily cap.
- Effort: quick-win

### SEC-5 The unit source slice drops the `contextPathOK` filter the precedent applies - Severity: Medium
- Component: `buildUnitContext` (§6.2), §5 file validation
- Evidence:
  - Paths are kept "only when it matches `AllowedGlobs` or `TestGlobs`", and a `File` is checked only for `..` and a leading `/` (`docs/designs/release-b4-unit-tests.md:124,140`).
  - The review precedent filters with `contextPathOK` (`dashboard/reviewdefect.go:302`), which also refuses empty segments, `.`, backslashes, NUL, forbidden paths and credential-shaped names (`dashboard/defecttriage.go:262-272`).
- Attack path:
  - `Failing[].File` comes from the QA tool, and its content comes from the code under test.
  - With broad `AllowedGlobs`, a reported path to a credential-shaped file is fetched and offered to the triage model.
  - Only the content scan in `buildReviewContext` (`dashboard/reviewdefect.go:337`) stands between that file and the provider. A secret format the scanner does not recognise passes.
- Remediation: require `contextPathOK(p)` in addition to the glob match, for both the failing files and the changed files, and test it with a credential-shaped path.
- Effort: quick-win

### SEC-6 The `qa-unit` row and its `TestGlobs` check are never reached for a unit job - Severity: Medium
- Component: `recordQARow`
- Evidence:
  - §4.2 sets `CaseCode` to `"*"` for unit modes (`docs/designs/release-b4-unit-tests.md:91`).
  - `recordQARow` marks the job recorded and returns without writing a row when `t.caseCode == "*"` (`dashboard/qajobs.go:519-522`).
  - The design does not mention changing this (`docs/designs/release-b4-unit-tests.md:153`).
  - `upsertPullRequestLocked` also refuses any kind other than review or QA (`dashboard/pullrequests.go:111-113`).
- Attack path: no direct attack. The only GAOS-side control on what the unit identity pushed (paths within `TestGlobs`, branch prefix) silently never runs, and a test only on a happy-path fake might not notice.
- Remediation:
  - State the branch in `recordQARow`: a unit card writes a `PRKindQAUnit` row before the `"*"` early return.
  - Widen the kind check in `upsertPullRequestLocked` to accept `PRKindQAUnit`.
  - Add a test that a `unit-create` job with `pr_url` produces the row and the check.
- Effort: quick-win

### SEC-7 The path and branch check trusts the QA tool's own report instead of GitHub - Severity: Medium
- Component: §7 file and branch check, §9 identity
- Evidence:
  - The check reads `unit.files` from the job answer (`docs/designs/release-b4-unit-tests.md:113,153-156`).
  - GAOS already holds a GitHub reader, `pullFiles` (`dashboard/github.go:227-228`), and the poller fills `HeadRef` and `Author` from GitHub (`dashboard/pullrequests.go:70-80`).
  - The review flow reads files fresh and compares the count (`dashboard/reviewdefect.go:237`).
- Attack path:
  - This control exists to catch a misbehaving or compromised QA tool. A tool that writes a non-test file, or pushes from a branch other than `qa-unit/*`, also controls the `files` list it reports, so the check passes.
  - A QA tool can also name any pull request in a target repository as its `pr_url` (for example a human's), and GAOS records it as `qa-unit`.
- Remediation:
  - Run the check against `pullFiles` and the GitHub head ref (with `len(files) == changed_files`).
  - Require the PR author to equal a configured unit-identity login.
  - Require the PR to be a draft.
  - Treat a mismatch with the QA-reported list as its own note.
- Effort: small

### SEC-8 The sandbox attestation is self-asserted, its age is set by the attester, and caching contradicts "re-reads at each start" - Severity: Medium
- Component: §4.3, `qaOutbound.capabilities`
- Evidence:
  - Every field of the attestation, including `attestedAt`, comes from the QA tool, and acceptance is "less than 7 days old" (`docs/designs/release-b4-unit-tests.md:99-102`).
  - D-104 admits the check is "for shape and age only" (`docs/designs/release-b4-unit-tests.md:162`).
  - `capabilities` serves a cached set for `qaCapsTTL`, and on error keeps the last known set (`dashboard/qaclient.go:70-71,94`). Yet the design says the runner "re-reads the capabilities at each start" (`docs/designs/release-b4-unit-tests.md:102`).
  - The cache returns `map[string]bool` with no room for the `sandbox` object.
- Attack path:
  - Any QA tool, including a misconfigured one, passes by echoing the fixed shape with the current time.
  - A future `attestedAt` also passes the age test as written.
  - When the QA tool stops answering, the stale set keeps starting jobs that run repository code.
  - The control gives the operator an assurance it does not provide.
- Remediation:
  - Reject an `attestedAt` in the future (beyond a small skew).
  - Cache the attestation with its own timestamp and treat a read error as "no attestation" for unit modes.
  - Say plainly in §4.3 and on the Auto-fix page that this is the QA tool's declaration, and that the real control is the QA tool's own deployment review.
  - Record that as an open question rather than a gate.
- Effort: small

### SEC-9 Failing-test names and files are data written by the code under test and go into model packets - Severity: Low
- Component: §5 screening, §6.1 `Excerpt`, triage and fixer packets
- Evidence:
  - The `Test` and `File` values are screened for secrets and capped, nothing more (`docs/designs/release-b4-unit-tests.md:124`).
  - They become the defect's `Excerpt` and `Files` (`docs/designs/release-b4-unit-tests.md:134`), which the triage card reads.
- Attack path:
  - A repository contributor can choose test names that carry instructions to the triage or fixer model.
  - The person-release gate is the mitigation, but the design does not say the release view shows the admin what will be sent.
- Remediation:
  - Show the failing names and files on the held card.
  - Mark them as untrusted data in the triage packet.
  - Restrict `Test` to printable characters with no newlines.
  - Apply the same screen to the `unit:files` notice text.
- Effort: quick-win

### SEC-10 `CIConfirmed` for `qa-unit/*` does not cover the workflow token - Severity: Low
- Component: §3 `UnitQAConfig.CIConfirmed`, §7
- Evidence: the confirmation reads "CI runs no secrets on `qa-unit/*`" (`docs/designs/release-b4-unit-tests.md:66,158`), mirroring `AutoFix.CIConfirmed` (`dashboard/autofix.go:149-150`).
- Attack path:
  - Model-written tests run in CI on a same-repository branch.
  - If the repository's default workflow permissions are write, the run's `GITHUB_TOKEN` is a credential, and an operator may not count it as a "secret".
  - Workflows triggered by `pull_request_target` are a second gap.
- Remediation: word the confirmation to include read-only workflow-token permissions and no `pull_request_target` workflows on `qa-unit/*`, and say the same in §9.
- Effort: quick-win

### SEC-11 The unit defect cap does not share the product's global new-key limit, and the 50-item bound is only a comment - Severity: Low
- Component: §6.1 cap, §5 `Failing`
- Evidence:
  - The review defects count against `defectMaxNewKeys` as well as their own cap (`dashboard/reviewdefect.go:115`).
  - The unit cap is described alone (`docs/designs/release-b4-unit-tests.md:136`).
  - "At most 50" appears only as a struct comment (`docs/designs/release-b4-unit-tests.md:111`).
- Attack path: churning failing sets (each set is a new key) adds up to 20 defects a day on top of the other defect sources. A QA answer with thousands of entries, still inside the 2 MiB cap, makes GAOS screen and hash all of them.
- Remediation: count unit defects in `newKeysTodayLocked`, and have GAOS truncate `Failing` to 50 before screening and hashing (or treat more than 50 as `qa-unit-unreadable`).
- Effort: quick-win

## Summary

Two High findings block approval under the policy's security criterion:
- **SEC-1:** a unit defect is not limited to the auto-fix target repositories.
- **SEC-2:** a Blueprint sync releases the held triage card without an admin.

Both are quick fixes with a direct precedent in the code (`dashboard/reviewdefect.go:106`, `dashboard/sendback.go:153`). Of the Medium findings:
- SEC-3 and SEC-4 complete the "admin-only release" claim.
- SEC-6 is a functional gap that silently disables §7's only control.
- SEC-5, SEC-7 and SEC-8 are defences that rest on what the QA tool says about itself.

Files consulted: `/home/ardi/devel/projects/ardihikaru/my-projects/gaos-unit-design/docs/designs/release-b4-unit-tests.md`, `/home/ardi/devel/projects/ardihikaru/my-projects/gaos-unit-design/dashboard/{auth.go,board.go,handlers_board.go,handlers_autofix.go,qajobs.go,qaclient.go,reviewdefect.go,autofix.go,autofixpr.go,workspace.go,sendback.go,defecttriage.go,pullrequests.go,labels.go,runner.go,api_cards.go,handlers_tasks.go}`, `/home/ardi/devel/projects/ardihikaru/my-projects/gaos-unit-design/pustaka/docs/product/decision-log.html`, `/home/ardi/devel/projects/ardihikaru/my-projects/gaos-unit-design/docs/designs/release-b-e2e-kinds-merge-unit-tests.md`. No credential file and nothing under `.claude/` was opened.

Counts: Critical=0 High=2 Medium=6 Low=3
