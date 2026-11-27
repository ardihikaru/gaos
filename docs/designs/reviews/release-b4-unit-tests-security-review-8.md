# Security review: release-b4-unit-tests.md (rev 8), round 8

## Paraphrase

### What this proposes

The design adds a per-product switch, `UnitQA`, which is off by default. When it is on, a merged pull request in an auto-fix target repository plans a `unit:qa` card. Brain Town and the external-tasks API both plan it under one key. No agent works this card. The QA-job runner drives it:

1. It reads the merge commit, `merged_at` and base from GitHub.
2. It reads the QA tool's sandbox self-attestation fresh and checks its shape and age.
3. It sends `unit-run` with the repository, the sha and a `watch` list.
4. It polls for a `unit` result.

What happens next depends on the result:
- **Test or build failure:** a deduplicated unit defect is filed, subject to a daily cap. Its triage card is born BLOCKED with `ReasonUnitHeld`. Only an auto-fix admin can release it, cancel it or close the defect. The store enforces this, and every release, cancel or close is attributed to that admin.
- **A later pass of the watched `(test, file)` pairs:** this closes a defect that is not held. A held defect is only marked as passed.
- **Failure of the QA job itself:** this files nothing.

Cards with a planner-written QA-runner key and a `qa-job:` reason no longer hold the product's live-turn lease or an agent seat. This amends BR-38. Boot reconciliation skips them, and board dispatch refuses them. A released unit defect follows the existing D-091 path, with three changes:
- It uses its own packet.
- The fix may not edit the failing test files.
- The fix opens against the recorded `UnitBase`, re-checked when it opens.

The feature ships dormant until the QA tool serves the §11 contract. Writing missing tests is moved to `release-b4b`.

### How the existing system works, per this document

- `pr.merged` plans a `unit:test` agent card that runs nothing (`prflow.go:88-91`).
- The QA-job runner already drives e2e cards through `startAutomation` and `jobStatus` on `qaOutbound`.
- Defect triage and fix follow D-091: a triage card, a verdict, a fixer, then a draft `gaos/fix-*` pull request.
- `ResolvePRCard` keeps operator-blocked cards where the operator put them.
- Board moves go through `MoveCard`/`legalMove`.
- Auto-fix configuration is limited to the mode-600 `GAOS_AUTOFIX_ADMINS` list.
- Drift release leaves `operatorHolds` reasons alone.
- BR-32 screens every dispatch packet.

I could paraphrase every section. No comprehension finding.

## Trust boundaries touched

| From → To | Authenticates | Authorises | Validates | Far side fails |
|---|---|---|---|---|
| Operator browser → back office (new route, move/close guards) | HMAC-signed session cookie, verified against the account fingerprint (`dashboard/auth.go:329-358`), placed on the request by the gate (`auth.go:568-586`) | `autoFixAdmin` file, fail-closed (`handlers_autofix.go:17-37`) | `crossOriginOK` (`auth.go:595-612`), SameSite=Lax | 401 / 403 / 409 |
| GAOS → QA tool (capabilities, `unit-run`, poll) | the QA client's configuration | none (QA is a peer) | `sandboxOK` shape and age; result screening (§6) | PENDING, then BLOCKED, retried for 24 h |
| QA tool → GAOS (job answer) | the same channel | BR-54 as amended | 2 MiB cap, truncate to 50, `screenDefectText`, `contextPathOK` | `qa-unit-unreadable` |
| GAOS → GitHub (PR read, files, default branch) | read token | — | `sha40Re`, `baseBranchRe` | BLOCKED with retry |
| External-tasks API → planner (`unit-test`) | bearer | client caps (`apiDaily` 20) | merged, at most 7 days old | 422 / 503 |
| Agent packet → provider | gateway | — | BR-32 at dispatch | refused |

A note on the rubric's premise: the brief says `signedIn()` accepts any cookie. On this checkout that is no longer true. `signedIn` → `sessionFrom` → `verify` checks an HMAC and an account fingerprint (`dashboard/auth.go:339`, `:354-356`). So "only an auto-fix admin may…" can be implemented, and the design implements it through `sessionOf` + `autoFixAdmin`.

I found no Critical or High finding. In particular I checked these possible bypasses of the hold, and none works:
- **Defect intake `xpass`:** it cannot address a unit defect, because `DefectID` must match `^DEF-\d{1,6}$` (`defects.go:68`, `:210`).
- **`readyDefectCards`:** it only moves PENDING cards (`defecttriage.go:798`).
- **The scheduler:** `readyPending` also only moves PENDING cards (`runner.go:606`).
- **`questions.go` answer:** only acts on AWAITING_INPUT.
- **The TaskMgr link:** it makes no card moves.
- **`CardMove`:** its only callers are the handler and ArcadeStore (`handlers_board.go:72`, `store_arcade_blueprint.go:175-177`).
- **Restart:** the hold survives it, because `blockReason` is persisted on the Task row (`store_arcade.go:1096`, `:1878`).

---

### SEC-1 The trust-rule amendments are approved under a delegation whose anchor does not exist yet - Severity: Medium
- Component: governance of the trust model (BR-38, BR-54, BR-61)
- Evidence: `docs/designs/release-b4-unit-tests.md:17` and `:502` ("written in phase 1"). `pustaka/docs/product/decision-log.html` has no match for `D-105` or the quoted words. `docs/rules/gaos-design-review.md:89-90` says implementation starts only after the operator approves. BR-38 as it stands is at `pustaka/docs/product/brd.html:1298`.
- Attack path: no attacker is needed. The design relaxes two trust rules on the agents' own approval: one live turn per product (BR-38), and "a QA result never moves a task" (BR-54, `qajobs.go:27`). The only evidence of consent is a quotation inside the design under review, and the register entry that would anchor it is written after approval. A reviewer agent cannot treat an agent-relayed quote as operator consent, so this control is unverified as written.
- Remediation: before the status becomes Approved, the step-8 summary should list the BR-38 and BR-54 amendments by number and get the operator's explicit acknowledgement of them. That acknowledgement, or the verbatim delegation with its date and channel, should be saved in the round-8 report or the triage table, which is committed. That way the anchor exists before any code changes. D-105 can still be written in phase 1.
- Effort: quick-win

### SEC-2 The runner loop's GitHub cost per failure result has no bound per tick, and it re-fetches on every duplicate hit - Severity: Medium
- Component: QA-job runner availability; the shared GitHub read token
- Evidence: `docs/designs/release-b4-unit-tests.md:294` (adds "each failure result arriving in that tick" without capping how many). `:348` step 3 re-runs `buildUnitContext` whenever `!Defect.Context.Built`, including on duplicate hits. `:444` ("When there is no source or the build fails, the defect is still filed … with no stored context"). `dashboard/defecttriage.go:143` (`defectMaxFetch = 12`). The runner is one loop (`dashboard/qajobs.go:404-468`), and active time counts against e2e jobs (`qajobs.go:406-413`).
- Attack path: many failing results can arrive in one tick. That happens through volume: merges across products × `GAOS_UNIT_QA_OPEN` (up to 20), API `unit-test` requests, or a QA tool that answers `failed`. Each result costs up to 13 GitHub reads, and each read may sleep up to 20 s on a rate limit. If a context build failed once, every later run of the same failing set fetches again, and the daily defect cap does not limit this because duplicate hits are not counted. The result is that in-flight e2e jobs time out, and the read token's quota (`main.go:383-391`), shared with the PR reviewer and the fix poller, runs out.
- Remediation:
  - Cap context builds at 1 per tick, across all products.
  - Record a per-defect `ContextTriedAt` and rebuild at most once an hour.
  - Or run `buildUnitContext` on its own goroutine outside the poll loop.
  - Restate the §5.6 bound to include this cap, and add the case to TC-866.
- Effort: small

### SEC-3 Brain Town keys are not lower-cased, so the claimed one card per merge does not hold for mixed-case owners - Severity: Low
- Component: planner deduplication across sources
- Evidence: `docs/designs/release-b4-unit-tests.md:143` ("Brain Town keys are lower-cased the same way"). `dashboard/braintown_consumer.go:533-550` builds `PREvent` from `OrgLogin`/`ProductKey` with only `trimField`. `dashboard/braintown_consumer.go:122-127` builds the key without lower-casing it.
- Attack path: GitHub reports a mixed-case owner such as `Acme`. Brain Town's card is `Acme/Route-API#5:unit-qa`, while an API request maps to `acme/route-api#5:unit-qa`. That gives two cards, two sandbox runs and two act-once markers for one merge. Defect deduplication still holds because `UnitDefectIn.Repo` is lower-cased. This is the remaining part of round-7 SEC-3.
- Remediation: lower-case `org`/`repo` in the consumer before building `PREvent`, or make `planUnitQASpecLocked` compare keys case-insensitively. TC-856 should include a mixed-case Brain Town frame. Correct the claim at line 143.
- Effort: quick-win

### SEC-4 The QA job id can contain spaces and leads a space-delimited reason that now carries `sha:`, `base:`, `merged:` and `unit:` - Severity: Low
- Component: reason grammar and adoption (§5.4)
- Evidence: `dashboard/qajobs.go:104` validates the job id with `validCursor`, which is `^[\x20-\x7e]*$` (`dashboard/qacatalog.go:49`, `:418`), so spaces are allowed. `dashboard/qajobs.go:303-307` and `:339-347` show that the job id is the first field of the reason. Design `:264` and `:267` say `reasonField` reads `sha:`, `base:`, `merged:` and `unit:` from the fields.
- Attack path: a QA tool that is faulty or compromised can return a job id that embeds `sha:` / `base:` / `unit:` tokens. On adoption after a restart, `reasonField` may read the injected values instead of GAOS's own. That can set `unitDone` from an injected `unit:` token, or file the defect under a different (still valid) unit base. The QA tool already controls the result, so the extra power is small, but the grammar should not let a peer write GAOS-owned fields.
- Remediation:
  - For unit cards, refuse a job id that does not match `^[A-Za-z0-9._-]{1,64}$` (`qa-unit-unreadable`).
  - Have `reasonField` refuse a reason in which any key appears twice, or appears before `case:`.
  - Add the case to TC-868.
- Effort: quick-win

### SEC-5 Numeric inputs from GitHub and the QA tool are not clamped - Severity: Low
- Component: retry timing; defect title
- Evidence: `docs/designs/release-b4-unit-tests.md:188` (rev 8 has `do` fill `ghError.Retry` from `Retry-After` / `X-RateLimit-Reset`) and `:288` ("not before it"). `:321` and `:379` take `Total` from the QA tool straight into `Title`, with no range given.
- Attack path: an absurd `X-RateLimit-Reset` value, or a proxy rewriting it, stops the 15-minute retry until the 24-hour end. A negative or huge `Total` renders into titles and notices.
- Remediation:
  - Clamp `ghError.Retry` to no more than 1 hour from now, and ignore past values.
  - Clamp `Total` to `[len(Failing), 100000]`.
  - Assert both in TC-865 and TC-869.
- Effort: quick-win

### SEC-6 The "every path out of BLOCKED" table gives a wrong reason for send-back and leaves out the defect intake - Severity: Low
- Component: §7.3 hold enumeration
- Evidence: Design `:433` says a held card is not an implement task. In fact the triage card is `TaskKindImplement` (`dashboard/defecttriage.go:110`). The real guard is the review kind plus `ReasonDesignBlocked` (`dashboard/sendback.go:168-172`). The table also omits `ReportDefects` `xpass` (`dashboard/defects.go:432-440`), which cancels defect cards by `DefectID`. That path is safe only because `defectIDRe` is `^DEF-\d{1,6}$` (`defects.go:68`, `:210`).
- Attack path: none today. The risk is that a later change follows the stated reasoning. For example, if the intake's id rule is widened, an `xpass` could cancel a held card with no admin. The design would not flag it, because the path is not listed.
- Remediation: correct the send-back row to cite `sendback.go:168-172`. Add a row for `ReportDefects` (`xpass`/`xfail` supersede) that cites `defectIDRe` and the `qaCaseID` match. Add an assertion to TC-870 that an `xpass` naming a unit key is rejected.
- Effort: quick-win

### SEC-7 The new admin route has no stated body cap and no unauthenticated branch - Severity: Low
- Component: `POST /products/{id}/autofix/unit`
- Evidence: Design `:105` specifies the 403 and 422 responses but no request size limit and no 401. The model it copies applies `http.MaxBytesReader(…, 64<<10)` and returns 401 with no session (`dashboard/handlers_autofix.go:117-121`, `:127`).
- Attack path: a signed-in non-admin's oversized form is parsed before the admin check, if the implementer orders it that way. A missing-session branch may also be left out. Both are low impact, because the gate and the CSRF check still apply (`auth.go:564-586`).
- Remediation: state the order: 401 with no session, then 403 for a non-admin, then `MaxBytesReader` 64 KiB, then `ParseForm`. Assert it in TC-875.
- Effort: quick-win

### SEC-8 The scheduler's dispatch path relies on a label invariant, not the planner-set key - Severity: Low
- Component: BR-38 as amended
- Evidence: Design `:117` puts the QA-runner-key refusal only in `MemStore.dispatch` (`dashboard/board.go:479`). The scheduler dispatches through `runner.go:522-563` and does not call it, so it relies on `labelsAllowDispatch` (`labels.go:179-188`). Today the invariant holds only because `setTaskLabelLocked` swaps labels within a family and never removes one (`prflow.go:567-580`). Rev 6 moved away from label-keyed controls for exactly this reason (design `:164`, `:587`).
- Attack path: a future change that lets a label be removed, or a QA-runner-keyed card planned without a non-dispatchable label (`staging-create/`, `qa-merged/`), would let the scheduler start a model turn on a card whose `qa-job:` reason makes it lease-exempt. That breaks "no two model turns at once".
- Remediation: in `Schedule`, treat `TaskNode.QADriven`, or a new `TaskNode.QAKey` set from `qaToolKey`, as never dispatchable, so the scheduler and the board refuse on the same key-based rule. Add the case to TC-857.
- Effort: small

---

Counts: Critical=0 High=0 Medium=2 Low=6
