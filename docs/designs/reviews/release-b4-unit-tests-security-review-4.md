# Security review: release-b4-unit-tests.md (rev 4)

The design needs one fix before it can be approved. Its change to the live-turn lease (§4.4) is keyed on the wrong predicate. As written, it would release BR-38 for defect cards, and those cards do run agent turns (SEC-1, High). The other findings are Medium or Low.

## What this proposes

- A product setting `UnitQA`, off by default, stored on its own record outside `AutoFix`. With it on, a merge into a fix-target repository plans a planner-only `unit:qa` card instead of `unit:test`. Brain Town and the external-tasks API produce the same card under the same key.
- At the start of each job, the QA-job runner reads the merge commit, `merged_at` and base from GitHub. It reads the QA tool's capabilities and sandbox attestation fresh, checks them by shape and age, and asks the QA tool for `unit-run` or `unit-create`.
- Failures in the repository's existing tests file a deduplicated, capped `unit` defect behind a gate. Its triage card is born BLOCKED with `ReasonUnitHeld`. Only an auto-fix admin, resolved in the handler and enforced in the store, can release it, cancel it or close the defect, and the action is attributed.
- A later pass of the same test names at a newer merge closes the defect. Job failures and generated tests never file one.
- `unit-create` may open a draft pull request on `qa-unit/<sha12>-<job8>` through a separate GitHub identity. GAOS records it as a `qa-unit` row only after a GitHub read. It re-checks draft state, author, fork, commit authors and file globs at every new head. GAOS never reviews these pull requests (a stated residual).
- QA-tool-driven cards stop holding the product's live-turn lease (BR-38 amended). `unit:qa` cards get their own open cap.

## How the existing system works, per this document

- A merge plans a `unit:test` card that an agent picks up with no checkout, so nothing runs.
- The QA-job runner drives `e2e:*` cards against the QA tool's `/gaos/automation` and `/gaos/jobs` routes.
- Defects go through the D-091 path: triage card, triager verdict, fixer card, draft `gaos/fix-*` pull request.
- Auto-fix admins are listed in a mode-600 file read by `autoFixAdmin`.
- The scheduler and the board's dispatch both count any DISPATCHED or RUNNING task as the product's one live turn.
- A Blueprint sync releases BLOCKED cards unless `operatorHolds` names the reason.

## Trust boundaries touched

| From → To | Authenticates | Authorises | Validates | Far side fails |
|---|---|---|---|---|
| Operator browser → back office (move, close, new `/autofix/unit`) | Signed session cookie: HMAC, expiry, password fingerprint (`dashboard/auth.go:330-359`); gate plus cross-origin check (`auth.go:541-612`). The rubric's "cookie that checks nothing" is out of date. | `autoFixAdmin` file (`handlers_autofix.go:17-37`); there are no other roles (`pustaka/docs/product/brd.html:157`) | Store guard in `MoveCard` / `CloseDefect`; `Admin` is `json:"-"` | 401 / 403 / 409 |
| GAOS → QA tool (capabilities, automation, jobs) | `GAOS_QA_TOKEN_FILE` bearer (not opened) | — | `sandboxOK`; screening of names and paths; 2 MiB body cap (`qaclient.go:40,232`) | Retry, then BLOCKED (§5.1, §5.3) |
| GAOS → GitHub (PR, commits, files) | GAOS's GitHub reader (not opened) | — | 40-hex sha, author, draft, fork, commit authors, globs | No note; backoff |
| QA tool → GitHub (`qa-unit/*`) | Unit identity token (§11, assumed) | A ruleset GAOS does not verify | GAOS checks after the fact (detective) | — |
| External-tasks API → planner | Bearer | Product owns the repo (`prflow.go:229`), fix target | Merged PR required (`api_tasks.go:226-228`) | 422 |

## Findings

### SEC-1 The lease skip releases BR-38 for agent-run defect, manual-review and hand-dispatched e2e cards - Severity: High
- Component: scheduler and board live-turn lease (§4.4)
- Evidence:
  - Design `docs/designs/release-b4-unit-tests.md:160` says both lease sites "ignore a task whose labels `labelsAllowDispatch` refuses" and claims such a card "runs no model".
  - `dashboard/labels.go:179-188` refuses more than QA-runner labels: it also refuses `defect:triage`, `defect:easy`, `defect:medium`, `defect:complex`, `pr-review:manual` and `pr-review:needs-manual`.
  - Defect cards are dispatched to model agents through `AutoDispatch` (`dashboard/autofix.go:222-225`, `MoveAutoFix`). That goes through `dispatch`, whose only lease check is `liveTurnLocked` (`dashboard/board.go:500-503`).
  - Board dispatch of any READY card, `e2e:*` and `pr-review:manual` included, applies no label check (`dashboard/board.go:356-358, 466-503`). The design escalated this gap (§16 row 36, §14).
- Attack path: once a triager or fixer turn is DISPATCHED or RUNNING, neither the scheduler nor the auto-fix loop sees a live turn. A second fixer, a triager or any ordinary card of the same product can then start at the same time, which breaks BR-38's one live turn. No attacker is needed: the auto-fix loop does this by itself when two defect cards are READY. A signed-in operator who hand-dispatches an `e2e:*` or `pr-review:manual` card also starts a model turn that holds no lease. The design makes the escalated manual-dispatch gap worse, so it is no longer outside this design's scope.
- Remediation:
  - Key the skip on "driven by the QA-job runner", not on `labelsAllowDispatch`. Use a new `qaDriven(labels)` that is true only for `e2e:automation`, `e2e:run` and `unit:qa`.
  - Better, also require the card's reason to carry the `qa-job:` prefix, so a hand-dispatched e2e card still counts.
  - In this design, close the escalated gap: `dispatch` refuses every `qaDriven` label, not only `unit:qa`.
  - Extend TC-847: a RUNNING `defect:triage` card still blocks a second dispatch, and a RUNNING `unit:qa` card does not.
  - Fix the false sentence at line 160.
- Effort: small

### SEC-2 Approval of generated tests is enforced only on the QA tool's side; GAOS still relays Inbox answers - Severity: Medium
- Component: approval of `unit-create` (§3 step 4, §11 "Approval of generated tests")
- Evidence: Design lines 77 and 490 say an answer relayed through GAOS "never approves". But GAOS's answer webhook relays any answered QA-tool question to the QA tool (`dashboard/inbox_worker.go:12-33`). Every account can answer, because there are no roles (`pustaka/docs/product/brd.html:157`; OQ-37).
- Attack path: suppose the QA tool, through a bug or a later change, raises its `unit-create` approval as a GAOS Inbox question, as e2e approvals do (`qajobs.go:448`). Then any signed-in account answers it, GAOS delivers the answer, and generated code is pushed. The "named person" control is then only the other system's promise, and GAOS has no test for it.
- Remediation: add a GAOS-side refusal. External-question intake rejects, or the inbox worker never delivers, a question bound to a job of a `unit:qa` card (match on job id or card). Add a TC for it. State in §11 that GAOS enforces this as well as relying on it.
- Effort: small

### SEC-3 An automated code-writing identity's pull requests have no GAOS review, accepted under a delegated approval - Severity: Medium
- Component: §8.3, D-104 residuals
- Evidence:
  - Design line 13: "The operator delegated the approval of this design to the agents' review."
  - Lines 447 and 473: GAOS does not review `qa-unit` pull requests.
  - Line 441: a failing verdict only sends a notice, and GAOS never closes the pull request.
  - Line 415: a pull request refused at write time gets no row, so it is never re-checked.
  - `docs/rules/gaos-design-review.md:89-90`: the operator is shown every blocker and residual before approval.
- Attack path: a compromised or misbehaving QA tool, or anyone able to push to `qa-unit/*`, opens a pull request that changes test-glob files. Test code runs in CI and in later sandbox runs. The only controls are the QA-tool approver and whoever merges. If the write-time check refuses the pull request (wrong author, not a draft), GAOS sends one notice and stops watching that exact suspicious case.
- Remediation:
  - Take the "no review" residual to the operator explicitly, rather than closing it under the delegated agent approval.
  - Keep a row for a pull request refused at write time, marked `refused: <why>`, and keep re-checking it.
  - Show a failing `qa-unit` verdict as a standing alert on the Pull requests and Auto-fix pages, not only as a one-off notice.
- Effort: small

### SEC-4 Check 4 trusts GitHub's email-based commit attribution, and the real control (the ruleset) is unverified - Severity: Medium
- Component: §8.2 check 4, §11 Identity
- Evidence:
  - Design line 424 requires every commit's GitHub author and committer to be `UnitQA.Login`, reading "at most 250" commits.
  - Line 492 places the push restriction in a ruleset that GAOS never reads.
  - Line 436: the verdict list has no outcome for more than 250 commits.
- Attack path: GitHub maps a commit's author and committer to a login by the email the pusher writes into the commit. Anyone who can push to the branch can therefore make a commit appear to be by the unit identity. If the ruleset is missing or misconfigured, check 4 passes on commits the identity never made. A pull request with more than 250 commits leaves the later ones unchecked.
- Remediation:
  - Require `commit.verification.verified == true`, with the signer bound to the unit identity. Or have GAOS read the branch rules for `qa-unit/*` when `SetUnitQA` saves `create`, and again at each check.
  - Add a verdict `too many commits` when the pull request's `commits` count is over 250.
  - State in §8.2 that check 4 alone is advisory.
- Effort: small

### SEC-5 What happens to a name or build output the screen withholds is unspecified - Severity: Low
- Component: §6 intake screening, §7.5 packets
- Evidence:
  - Design line 276 runs each `Test` and `File` through `screenDefectText`. That function returns an empty string with `withheld` set; it does not drop the entry (`dashboard/defects.go:288-298`).
  - Line 380 puts only "failing tests and files" under the fenced, do-not-follow heading. The `build-failed` `Message` reaches `Excerpt` (line 325) with no stated fencing.
- Attack path: if a credential-shaped name is withheld, it may be kept as an empty entry. That skews the set hash and dedup, and it may count toward "≥ 1 kept entry". QA-tool-supplied build output is model input in the triager's packet with no stated fence.
- Remediation:
  - An entry whose `Test` or `File` is withheld is dropped and counted only in `Total`. The same rule applies to `Passed` and `UnitTests`.
  - A withheld `Message` becomes a fixed text, `build output withheld`.
  - Render `Message` inside the same fenced section.
  - Add these cases to TC-858.
- Effort: quick-win

### SEC-6 `ownUnitPRLocked` trusts a stored head that can lag the merged head - Severity: Low
- Component: §8.3
- Evidence: Design line 445 matches `checked@<sha12 of the row's current HeadSHA>`. `HeadSHA` is written only by the poller (line 419). A push followed by a merge between two polls leaves the old head stored.
- Attack path: an unchecked last commit is merged, and its merge plans no e2e or unit run, because the stale note still reads `checked`.
- Remediation: compare the note with the head of the merged pull request, read at the merge-time re-check or taken from the event. When that head is unknown, plan as usual.
- Effort: quick-win

### SEC-7 The external-tasks API can now start sandbox runs on any historical merge - Severity: Low
- Component: §4.2 API mapping
- Evidence: Design lines 130-139 map an API `unit-test` request to `unit:qa`. `dashboard/api_tasks.go:226-228` requires only that the pull request is merged.
- Attack path: a bearer client names old merged pull requests of a fix-target repository. Each one now runs code in the QA sandbox, which is new, and can file a held defect with an old `UnitBase` and send group notices. This is bounded by `GAOS_UNIT_QA_OPEN` and the daily cap, but it is a new effect of an existing credential.
- Remediation: refuse a mapped request whose `merged_at` is older than a stated window, for example 7 days, with a 422 code. Or record this in D-104 as accepted.
- Effort: quick-win

### SEC-8 The board still offers Ready and Cancel on a held card to non-admins - Severity: Low
- Component: §7.3 board rendering
- Evidence: `dashboard/sendback.go:378-399` already strips moves the store would refuse for design-blocked cards. Design §7.3 (line 351) adds the store refusal but no matching trim of the offered moves.
- Attack path: none is a bypass, because the store refuses. A non-admin sees moves that answer 409, which invites retries and confusion on a privileged control.
- Remediation: in `decorateCardsGate`, remove `ready` and `canceled` from `c.Moves` for a `ReasonUnitHeld` card when the viewer is not an admin, resolved at render time. State this in §7.3.
- Effort: quick-win

## Checked and clean

- `CardMove.Actor` and `Admin` are `json:"-"`, so a request body cannot forge them.
- An `xpass` report cannot close a unit defect, because `defectIDRe` (`dashboard/defects.go:68`) cannot match a `unit:` key.
- `BlockReason` persists through ArcadeStore (`store_arcade.go:1084`), so the hold survives a restart.
- `operatorHolds` covers the drift release (`workspace.go:395`).
- No secret or matched text appears in this report.

Counts: Critical=0 High=1 Medium=3 Low=4
