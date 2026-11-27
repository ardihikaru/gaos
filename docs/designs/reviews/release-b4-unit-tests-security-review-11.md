# Security review: `release-b4-unit-tests.md`, rev 12 (§0–§14)

## Paraphrase

### What this proposes
GAOS stops planning an agent `unit:test` card that never runs anything. When a pull request merges into a "unit base" of a fix-target repository and the product's new `UnitChecks` setting is on, GAOS stores a `UnitWatch` for the merge commit. On each 2-minute pass, the existing fix-PR poller reads the check runs on that commit through the GitHub reader it already holds. It keeps only runs from the `github-actions` app whose name is exactly the configured one (default `unit-tests`), and takes the run with the latest `completed_at`.

The outcomes:
- **`success`** closes the open unit defects on that repository and base. A defect whose triage card is still held is only marked.
- **`failure`** reads the run's annotations. Each failing test becomes a screened `{test, file}` pair, and the pairs file one unit defect per repository, base and failing set. If no test annotation survives screening, a `~build` defect is filed instead.
- **No check after 6 h** ends `no-ci`. Any other conclusion ends `inconclusive`. Neither counts as a pass.

A new or reopened unit defect's triage card is born BLOCKED with `ReasonUnitHeld`. Only an account in `GAOS_AUTOFIX_ADMINS` may unblock it, cancel it, or close the defect. This is enforced in `MemStore.MoveCard`, `CloseDefect`, `resolveCardLocked` and `operatorHolds`.

After release, the D-091 path runs: triage, then a draft `gaos/fix-*` pull request against `UnitBase`. A `fixGuard` forbids that fix from editing the failing tests' files or any test file in the packet, but new test files are allowed. The `~build` defect gets no guard.

GAOS executes nothing. The repository owner adds the push workflow of §11, which annotates the failing tests. The feature ships switched off.

### How the existing system works, per this document
- A merge plans one `unit:test` agent card that has no checkout. A FAILED card only sends a notice.
- `prFlow.handle` already re-reads a merged PR from GitHub. The D-091 defect path (triage card, fixer card, draft PR, `checkFix`) and the D-102 review-defect hold already exist.
- Auto-fix admins are the accounts in a mode-600 file.
- The poller already backs off and waits on GitHub's rate limits.
- The scheduler, e2e cards, boot reconciliation and the external-tasks API do not change.

## Trust boundaries the design touches

| From → To | Authenticates | Authorises | Validates | Far side fails |
|---|---|---|---|---|
| operator browser → dashboard (new `POST /products/{id}/autofix/unit`, the guarded card moves, close) | An HMAC-signed session cookie, checked before parsing (`dashboard/auth.go:329-359`, `:380-396`). The rubric's premise that the cookie "checks nothing" is stale: `signedIn` and `sessionOf` return only verified sessions, so the "auto-fix admin" identity does exist. | `autoFixAdmin` (`dashboard/handlers_autofix.go:17-37`), fail-closed. Cross-site writes are refused by `crossOriginOK` (`dashboard/auth.go:564`, `:595-605`). `CardMove.Admin` is tagged `json:"-"`, so a request body cannot set it. | 64 KiB body cap, form validation (§4.1) | 401, 403 or 422 |
| dashboard → GitHub (check runs, annotations, file contents) | The existing reader token (`dashboard/main.go:387-390`, `:459`) | — | App slug and name filter (§5.1); the annotation regex, `contextPathOK`, `stripBR53`, `screenDefectText` (§5.2) | `no-ci` or `inconclusive`, never a pass |
| GitHub check data → model provider (packets) | — | The admin release | Fenced heading, BR-32 screen at dispatch (§7.5) | Screen refusal |
| model output → repository (draft fix PR) | The write token | Admin release, `fixEligible`, `fixGuard` | `checkFix` (`dashboard/autofixcheck.go:275-344`) | Fix refused |

I checked the other ways a card leaves BLOCKED: `readyDefectCards` (PENDING only, `dashboard/defecttriage.go:793-806`), `readyPending` (`dashboard/runner.go:602-613`), `gateReleaseLocked` (implement tasks only, `dashboard/designgate.go:793-816`), `APICardCancel` (`qa-run` cards only, `dashboard/api_cards.go:431-448`), `AnswerQuestion` (needs an open question) and QA intake. QA intake cannot reach a unit defect, because `qaCaseID` is `^TC-[A-Z]+-[0-9]{3}$` (`dashboard/qacatalog.go:44`). None of these paths bypasses the hold, and the §7.3 table is complete as far as I found.

## Findings

### SEC-1 "Only the github-actions app's check run is trusted" can be forged by any workflow in the repository - Severity: Medium
- Component: the poller's check-run read (§5.1) and the trust rationale (§13)
- Evidence: `docs/designs/release-b4-unit-tests.md:123` (filter is only `app.slug == "github-actions"` plus `name`), `:126` (the run with the latest `completed_at` wins), `:352` (statuses rejected because "any token with `repo:status` can post one; only the `github-actions` app's check run is trusted"), `:281` (only the owner's own workflow is restricted to `contents: read`).
- Attack path: Any workflow job in the target repository that holds a `GITHUB_TOKEN` with `checks: write` can create a check run on any commit SHA. That run is attributed to the `github-actions` app and can carry any name, any conclusion and any annotations. "Any workflow job" includes:
  - jobs where the repository's default workflow permissions are read-write;
  - jobs that use a third-party action;
  - same-repository `pull_request` jobs on a branch, such as `gaos/fix-*`, whose test code GAOS's own fixer wrote (OQ-40).

  So a repository writer, a compromised action, or code running in such a job can do two things:
  - Post a later `success` named `unit-tests` on a merge commit. Under rule 4 it supersedes the real failure, and §7.4 then closes every released unit defect on that base and cancels their fix cards. This breaks §0 rule 4 ("a missing or inconclusive check is never a pass").
  - Post a forged `failure` whose annotations are attacker-chosen, so held defects are filed with attacker-shaped packets, bounded by the caps.

  §13's reason for preferring check runs over statuses does not hold for check runs either.
- Remediation:
  - Bind the result to the owner's workflow, not to the app. Add `UnitChecks.Workflow` (for example `.github/workflows/unit-tests.yml`).
  - Resolve the run with `GET /repos/{o}/{r}/actions/runs?head_sha=<sha>&event=push` and require `path == Workflow`, `event == push` and `head_branch == Base`.
  - Accept only a check run whose `check_suite.id` equals that run's `check_suite_id`.
  - If several runs match, take the highest `run_attempt` of that single workflow run, not the latest `completed_at` across suites.
  - Correct §13's rationale.
  - In §11, add that the repository's default workflow permissions should be read-only and that no other workflow should grant `checks: write`.
- Effort: small

### SEC-2 The fix guard does not stop a unit fix from neutralising the failing tests - Severity: Medium
- Component: `fixGuard` (§7.5) and closing on a pass (§7.4)
- Evidence: `docs/designs/release-b4-unit-tests.md:244` ("new test files stay allowed"), `:234` (any green check closes the defect). `dashboard/autofixcheck.go:313-327`: a new file only has to match `TestGlobs` and not already exist.
- Attack path: The fixer's input contains text from the code under test, which is untrusted (§5.2). The fixer can be steered into a fix that weakens the tests without touching any protected file. It does this by adding a new `_test.go` file in the same package that defines `TestMain`, or an `init` that changes package-level test state, skips or exits early. It can also edit a non-test helper in the packet that the tests depend on. The guard accepts this; once a person merges the draft, the next green check closes the defect. §14 accepts "deleted or skipped tests" as a residual, but the guard is presented as the control against exactly this ("The fix may not weaken the failing tests"), and it does not provide it.
- Remediation: Under `Unit`, refuse a new test file in any directory that holds a `Protected` file, or refuse any new or edited Go file in those packages that declares `TestMain` or `init`. Make the PR body list "test files added" as a reviewer checklist item. Optionally, close on a pass only when the passing run's `unit-tests-ran` notice names the protected tests. Otherwise, say in D-105 that the guard prevents only direct edits.
- Effort: small

### SEC-3 Any failure can be downgraded to the unguarded `~build` defect, and the shipped template does it to every root-package test - Severity: Medium
- Component: annotation intake (§5.2), the `~build` exemption (§7.5) and the workflow template (§11)
- Evidence:
  - `docs/designs/release-b4-unit-tests.md:139-141`: withheld entries are dropped, and "no kept test annotation" becomes `~build`.
  - `:244`: `{}` (no guard) for `~build`.
  - `:314-316`: `dir=.` for the module root, then `grep -rl … "$dir"`, which prints paths prefixed `./`.
  - `dashboard/defecttriage.go:266-268`: `contextPathOK` refuses a `.` segment.
- Attack path: With the design's own workflow, every failing test in the module's root package arrives with a `./…_test.go` path and is withheld. A failure that lies only in the root package is therefore filed as `~build`, with the guard off: the fixer may then edit the failing test itself. Separately, the code under test, or a forged run (SEC-1), can make every annotation fail screening on purpose, with an off-pattern title, a bad path or a secret-shaped string. That disables the guard for any failure and also routes free-text `output.summary` into the packet instead of the constrained test names.
- Remediation:
  - In §11, normalise the path (strip a leading `./`).
  - In §5.2, file `~build` only when the failure had **zero** failure-level annotations. When annotations existed but all were withheld, end the watch `refused:withheld` (or file a guarded unit defect).
  - For a `~build` fix, still refuse edits to existing files matching `TestGlobs` unless an admin confirms when releasing.
  - Add TC rows for the root package and for the all-withheld case.
- Effort: quick-win

### SEC-4 `checkName` goes into a query string with no stated encoding - Severity: Low
- Component: `checkRuns` (§5.1)
- Evidence: `docs/designs/release-b4-unit-tests.md:74` ("1-100 printable chars"), `:123` (`?check_name=<name>&filter=latest…`)
- Attack path: An auto-fix admin, or a mistake, saves a name containing `&`, `#` or a space. The request's parameters change (for example `filter` is overridden or truncated), and the client-side exact-name filter becomes the only control.
- Remediation: State `url.QueryEscape(name)` in §5.1, or restrict `CheckName` to `^[A-Za-z0-9 ._-]{1,100}$`. Add a TC-860 case.
- Effort: quick-win

### SEC-5 The template's private-module token would be readable by the code under test - Severity: Low
- Component: workflow template (§11)
- Evidence: `docs/designs/release-b4-unit-tests.md:281` ("the owner adds `GOPRIVATE` and a read-only token secret"), `:302-306` (one step runs `go test`)
- Attack path: Credentials set in the environment or in git config before `go test` can be read by any test, `init` or dependency the merge brings in. The design calls that code untrusted (§0.1), and a malicious merge could exfiltrate the token.
- Remediation: In §11 and on the published page, fetch modules in a separate step that holds the token (`go mod download`), remove the credential (unset the git `insteadOf` or `GOPRIVATE` auth), and run `go test` in a later step that has no secret in its environment.
- Effort: quick-win

### SEC-6 The pinned action SHAs cannot be checked from the repository and may be imposter commits - Severity: Low
- Component: workflow template (§11), published docs
- Evidence: `docs/designs/release-b4-unit-tests.md:296`, `:299`, `:322` ("looked up on 2026-11-27")
- Attack path: GitHub resolves `uses: owner/repo@<sha>` even when the commit exists only in a fork of that repository. A wrong or planted SHA in a published template that owners copy would run someone else's code in their CI. This claim is unverifiable from inside the repository.
- Remediation: Add to §11 how to verify that each SHA is reachable from the named tag in the upstream repository, and require the owner to do it before merging. Record the verification in D-105.
- Effort: quick-win

### SEC-7 Watch budget and ordering let one product's merge volume turn real failures into `no-ci` - Severity: Low
- Component: `unitPass` scheduling (§5.1, §4.2)
- Evidence: `docs/designs/release-b4-unit-tests.md:115` (200 pending per product), `:121` (10 watches per pass across all products, oldest `NextAt` first), `:124-125` (the 6 h limit counts from `MergedAt`, not from the last read), `:129` (`do` sleeps on a rate-limit reset inside the pass)
- Attack path: Anyone who can merge frequently into one product's unit base can fill the shared 10-per-pass budget. Other products' watches then age past 6 h unread and end `no-ci`, so their failures file nothing. A rate-limit sleep also stalls the fix-PR poller. No false pass results; this is loss of availability of the control.
- Remediation:
  - Take the watches per product in round-robin.
  - Measure the 6 h limit as time since the first unanswered read, or end an unread watch `inconclusive` instead of `no-ci`.
  - Keep the rate-limit wait out of the shared pass.
- Effort: small

Counts: Critical=0 High=0 Medium=3 Low=4
