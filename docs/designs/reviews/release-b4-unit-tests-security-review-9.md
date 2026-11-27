# Security review: release-b4-unit-tests (rev 9)

**Bottom line:** no Critical or High findings, so the policy's security gate does not block approval. There are three Medium and three Low findings, and the Medium ones should be fixed in the design before it moves to Approved. The rubric says the back office authenticates nobody. That is out of date: the session cookie is HMAC-verified, so this design's admin rules can be built today.

## Paraphrase

### What this proposes

- **A product switch, off by default.** It is called `UnitQA`, can be turned on only by an auto-fix admin, and needs a fresh sandbox attestation from the QA tool. When it is on, a merge into a fix-target repository plans a `unit:qa` card instead of today's `unit:test` card. Brain Town and the external-tasks API share one card per merge.
- **The QA-job runner drives that card.**
  - It reads the merge commit and base from GitHub itself.
  - It checks a fresh, self-declared sandbox attestation (container per run, no secrets, read-only source, checkout outside the container, network none or an allowlist, at most 7 days old).
  - It asks the QA tool to run the repository's existing tests (`unit-run`) at that commit, with a watch list of the tests of open unit defects.
  - Concurrency is capped per product and per tick. Blocked starts retry for 24 hours.
- **A test or build failure files a unit defect** (deduplicated, capped, gated on auto-fix being on with a fixer and triager assigned). Its triage card is born BLOCKED `ReasonUnitHeld`. Only an auto-fix admin can release it, cancel it or close its defect, and this is enforced in the store and attributed. A failure of the job itself files nothing.
- **A later pass of a defect's watched `(test, file)` pairs** closes it. If its card is still held, the defect is only marked, once, and an admin closes it. Older failures never reopen what a newer pass covered.
- **QA-runner cards are recognised by their planner-written key** and hold no live model turn (BR-38 amended) and no agent seat, and are never dispatched to an agent.
- **Boot reconciliation** leaves these cards RUNNING when the QA client is usable. When it is not, they end FAILED `qa-runner-absent`.
- **An admin may cancel a RUNNING QA card**, e2e or unit.
- **A unit fix** may not edit the failing tests' files and opens against the recorded `UnitBase`, re-checked when the fix opens.
- **Writing missing tests is out of scope** (moved to `release-b4b`).
- **Approval** is delegated to the agents under a recorded operator instruction. The security gate and the final summary to the operator still apply.

### How the existing system works, per this document

- Today a merge plans `unit:test` for an agent with no checkout. Nothing runs, and a FAILED card only sends a notice.
- The QA-job runner already drives e2e cards through `POST /gaos/automation` and polling. Boot `Reconcile` requeues every DISPATCHED or RUNNING card that has no gateway turn to READY.
- The live-turn lease counts every DISPATCHED or RUNNING card.
- Labels can be set by any signed-in account. Auto-fix settings, assignments and the closing of held defects are limited to the mode-600 `GAOS_AUTOFIX_ADMINS` list.
- `ResolvePRCard` moves PR cards within a fixed table.
- Defects follow the D-091 path: triage, then fixer, then a draft `gaos/fix-*` pull request checked by `checkFix`.

All sections could be paraphrased; there is no comprehension finding.

## Trust boundaries walked

| From → To | Authenticates | Authorises | Validates | If the far side fails |
|---|---|---|---|---|
| operator browser → dashboard (new route, board moves, close defect) | HMAC-signed session cookie checked before parsing (`dashboard/auth.go:329-359`); the gate requires it on every non-public route (`dashboard/auth.go:541-590`); cross-origin refusal (`auth.go:595-612`) | `autoFixAdmin` file check (`handlers_autofix.go:17-37`), in the handler, passed into the store as `CardMove.Admin` (`json:"-"`) | 64 KiB cap, store refusals | 401 / 403 / 409 |
| dashboard → QA tool | bearer token; https, or http only inside the tunnel (`qaclient.go:97-140`) | — | attestation shape (§5.3), job-id pattern, `unit` must not change between polls, the runner's `missing` rule, name screening (§6) | PENDING, then BLOCKED with retry, then FAILED |
| dashboard → GitHub (read token) | read token | `unitQAFor` (fix target) checked **before** any read (design :204) | 40-hex SHA, base rule (`autofix.go:122`) | retry, then BLOCKED |
| code under test → triager/fixer packet | — | the held card (a person decides first) | `stripBR53`, `screenDefectText`, fencing, BR-32 at dispatch (design :349, :470) | the dispatch is refused |
| dashboard → ArcadeDB | DB user | — | schema 21 | — |

**Rubric note.** The rubric's premise that `signedIn()` accepts any cookie no longer holds: `auth.go:375-396` verifies the MAC, expiry and account fingerprint. The "only an auto-fix admin" rule is therefore buildable today. The design does not widen the public edge, adds no new outbound origin (the QA URL is unchanged), and publishes only `acme/*` examples (design :596).

## Findings

### SEC-1 The no-runner boot branch drops a running e2e job's id, so a retry starts a second job - Severity: Medium
- Component: boot reconciliation, the e2e QA runner (change (c) of §2, live from the release)
- Evidence:
  - The design ends every RUNNING `qaToolDriven` card, e2e included, FAILED `qa-runner-absent` when `!qaOut.usable()` (design :194, :307, :574). Final reasons carry no `qa-job:` prefix (design :276).
  - Today `Reconcile` requeues to READY without touching `BlockReason` (`dashboard/runner.go:705-718`), and `start` adopts any card whose reason names its job, "never started again: a second `create` job could open a second pull request" (`dashboard/qajobs.go:352-362`).
  - A board retry FAILED → READY is open to any signed-in account (`dashboard/board.go:232-233`) and does not restore the reason.
- Attack path: a restart with `GAOS_QA_URL` missing or misconfigured, whether a mistake or a deliberate edit by someone with host access, while an e2e `create` job is running. The QA tool keeps running the job with nobody polling or cancelling it. If auto-merge was armed (`qajobs.go:380-383`), it can still open and merge a spec pull request that GAOS never records (`recordQARow` never runs). When the URL is fixed, any signed-in account's retry starts a fresh job, which means a second spec pull request in the automation repository. This silently gives up a safeguard the code exists to keep.
- Remediation (pick one):
  - Keep the job id: end the card with a reason that still carries it (for example BLOCKED `qa-job:<id> … qa-runner-absent`, which `start` adopts once the runner returns).
  - Or limit the FAILED end to `:unit-qa` cards, which write nothing, and keep today's requeue for e2e cards.

  Either way, add a TC-868 assertion that a retried `qa-runner-absent` e2e card does not call `startAutomation` a second time.
- Effort: small

### SEC-2 Any signed-in account can stop unit runs and orphan released unit defects, with no record of who did it - Severity: Medium
- Component: board moves on `unit:qa` cards and on released unit defect cards
- Evidence:
  - The design limits the cancel of a RUNNING QA card (design :293) and moves of a held card (design :432) to admins.
  - It leaves the board cancel of PENDING, READY and BLOCKED `unit:qa` cards open to everyone (`legalMove`, `dashboard/board.go:238-239`).
  - `MoveCard` records no actor for these moves (`board.go:394`, `moveTaskLocked(taskID, to, action)`). Only the held-card path gets `Actor` (design :432).
  - After an admin releases a held card, any account can cancel the triage or fixer card. The defect stays open with no card, and every later identical failure is deduplicated into it with "plans nothing" (design :426).
  - OQ-37 (`pustaka/docs/product/open-questions.html:912-918`) accepts open operator actions only because "every change names its alias". This one does not.
- Attack path: a signed-in non-admin (or a stolen session) cancels the `unit:qa` card of a merge before it starts, or cancels a released triage card. The failing tests never become a held defect, or the defect sits open with no work and absorbs every repeat. No event names who did it. This undermines the BR-64 promise that a failure is held for an admin.
- Remediation:
  - Fill `CardMove.Actor` and pass `actorAlias` on **every** `MoveCard` move, not only held-card moves.
  - Apply the admin check to cancelling any `:unit-qa` card and any card of a `unit` defect, not only RUNNING or held ones. Or, when a non-held unit triage or fixer card is cancelled, re-hold the defect rather than leaving it open with no card.
  - Add both to TC-870.
- Effort: small

### SEC-3 The delegation record and the operator's final summary do not match the design's amendments - Severity: Medium
- Component: governance of the trust-model amendments (BR-38) that are approved without the operator
- Evidence:
  - The delegation file's summary list (`docs/designs/reviews/release-b4-unit-tests-delegation.md:29-35`) describes D-091 (8) as "a unit defect's triage card is born BLOCKED and held". The design's D-091 (8) amendment is "a unit defect counts as backend by decision" (design :528).
  - The list omits the new rule BR-64 (design :523), the OQ-38 amendment (:529) and the BR-44 clarification (:525).
  - It does not say that the BR-38 relaxation, the admin cancel and the `qa-runner-absent` end apply to **every product's e2e cards from release day**, even while unit runs stay dormant (design :51-61).
  - §0 calls the quoted text "its words" (design :17) but quotes only part of line 14 of the record.
  - The §22 triage says §0 cites `:17` (design :895), while §0 and §10 cite `:14`.
- Attack path: there is no attacker. The only human control left over a delegated approval is the step-8 summary (design :17; `docs/rules/gaos-design-review.md:89-90`). If it is built from the delegation file, the operator gets a wrong description of one amendment and no mention of a live change to the trust model (BR-38, `pustaka/docs/product/brd.html:1298`). The operator cannot revisit what they were never shown accurately.
- Remediation:
  - Generate the delegation file's amendment list from §10, word for word: BR-64, BR-38, BR-54, BR-61, D-091 (8), OQ-38 and the BR-44 clarification.
  - Add a line saying that the §2 (a–g) changes, including e2e cards leaving the lease, are live from the release.
  - Either quote line 14 in full or mark the §0 and §10 quotes as excerpts.
  - Correct `:17` to `:14` in §22.
- Effort: quick-win

### SEC-4 Some board renders are left out of the held-card move trim - Severity: Low
- Component: the back-office UI
- Evidence:
  - The trim list is `handleBoard`, `handleBoardPaged`, `handleBoardLive` and `renderBoardAfter` (design :436).
  - Board sheets are also rendered by `renderBOAfter(…, "board")` after a label change, a send-back, an auto-fix save and a defect close (`dashboard/handlers_board.go:157`, `:244`; `dashboard/handlers_autofix.go:150`, `:171`), and by the product page route `GET /p/{ref}/{section}` (`dashboard/main.go:548-549`).
- Attack path: a non-admin is offered Ready and Cancel on a held card and gets a 409. The store guard holds, so this is a misleading UI, not a bypass.
- Remediation: put the trim in the one function every board projection goes through (or list `renderBOAfter` and `handleProductPage`), and assert in TC-870 that none of them offers the moves.
- Effort: quick-win

### SEC-5 A unit fix must add a model-written test file whose CI runs on a same-repository branch; OQ-40 is not cited - Severity: Low
- Component: the D-091 fixer for unit defects
- Evidence:
  - `checkFix` refuses any fix that adds or edits no test file (`dashboard/autofixcheck.go:354-360`). This design refuses edits to test files in the packet for unit defects (design :472), so **every** unit fix must add a new test file written by the model.
  - The fixer's packet carries test names, file names, build output and source taken from the code under test (design :468).
  - The fix branch lives in the application repository, and its CI exposure is the open question OQ-40 (`pustaka/docs/product/open-questions.html:126`).
  - §14 (design :628) mentions shadowing but not OQ-40.
- Attack path: text in merged code steers the fixer to write a test that runs in CI on a `gaos/fix-*` branch. The added privilege over the merged code itself is small (that code already runs in CI), and the admin release, `CIConfirmed` and the draft-only pull request still apply. The residual is simply not stated.
- Remediation: add OQ-40 to the D-105 residuals and to §14, stating that a unit fix always adds a model-written test file on a same-repository branch, guarded by `CIConfirmed` and the admin release.
- Effort: quick-win

### SEC-6 A pass closes a released unit defect and cancels its cards without any notice - Severity: Low
- Component: `PassUnitDefects` for defects that are not held
- Evidence:
  - For a defect whose card is not held, a passing report calls `cancelDefectCardsLocked` and marks the defect fixed (design :458). That cancels its PENDING or READY triage or fixer cards (`dashboard/defects.go:363-379`).
  - §5.7 lists a notice only for **held** defects (design :322).
- Attack path: a wrong, buggy or compromised QA report undoes an admin's release (the cancelled cards are the work the admin started). Only the event stream records it, as `job <id> at <sha12>`. There is no notice, so nobody is told to look.
- Remediation: send one group `unit:failure` notice, with fixed text such as "defect <h8>: its failing tests passed at <sha12>; its cards were cancelled", when a pass closes a defect whose triage card had been released. Assert it in TC-861.
- Effort: quick-win

Counts: Critical=0 High=0 Medium=3 Low=3
