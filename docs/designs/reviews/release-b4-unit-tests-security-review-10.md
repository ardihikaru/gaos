# Security review: `release-b4-unit-tests.md` (rev 10)

## Paraphrase

### What this proposes

The design adds a product switch, `UnitQA`, which is off by default. When it is on, a merged pull request in a fix-target repository plans a `unit:qa` card instead of today's `unit:test` agent card. Brain Town and the external-tasks API both plan it under one key, compared case-insensitively, and a newer merge supersedes an older pending card for the same repository.

The QA-job runner drives the card:
- At start it reads the merge commit, `merged_at` and base from GitHub, and requires the base to be a configured unit base.
- It reads a fresh sandbox attestation from the QA tool and checks its shape and age.
- It sends `unit-run` with a `watch` list of the `(test, file)` pairs of open unit defects.
- It polls for a result that must not change once it appears.

A failing test or build files a deduplicated, capped `unit` defect. Its triage card is born BLOCKED with `ReasonUnitHeld`, and only an auto-fix admin can release it, cancel it or close its defect. When all of a defect's watched pairs pass at a newer merge on the same base, the defect closes; if its card is still held, it is only marked for an admin.

After release, the existing D-091 triage and fix path runs with two guards:
- `checkFix` refuses edits to the failing tests' files, to reference files and to test-glob files in the packet. New test files are still allowed.
- The fix opens against `UnitBase`, re-checked when it opens.

Some changes apply from release day whatever the switch says:
- Cards with a QA-runner key (e2e cards included) leave the BR-38 live-turn lease and are never dispatched to an agent.
- Boot `Reconcile` no longer requeues those cards.
- `e2e:*` labels are refused on cards that do not have a QA-runner key.
- An auto-fix admin may cancel a running QA-tool card.
- Every board move records the mover's alias.

The trust rules change too: new BR-64; amendments to BR-38, BR-54, BR-61, D-091 (8) and OQ-38; clarifications of BR-44 and BR-48. All of this is approved under a recorded operator delegation that does not waive the security gate. The design ships dormant until the QA tool serves the §11 contract.

### How the existing system works, per this document

- Today a merge plans one `unit:test` card that an agent receives, but nothing runs the tests (`prflow.go:88-91`, `labels.go:179-188`).
- The QA-job runner drives e2e cards selected by label (`qajobs.go:241-263`). It keeps the job id in the card's reason and adopts the job after a restart (`qajobs.go:350-362`).
- The lease counts every DISPATCHED or RUNNING card (`board.go:452`, `scheduler.go:226-238`).
- Boot `Reconcile` requeues DISPATCHED or RUNNING cards (`main.go:292-302`).
- `legalMove` refuses cancelling a Working card (`board.go:236-237`).
- `CloseDefect` cancels a defect's cards and closes it for any signed-in account (`defects.go:590-608`).
- Auto-fix admins are listed in a mode-600 file (`handlers_autofix.go:17-37`).

## Trust boundaries

| Boundary | Authenticates | Authorises | Validates | If the far side fails |
|---|---|---|---|---|
| Operator browser → back office | A per-account HMAC-signed cookie, checked against the account fingerprint (`auth.go:329-358`). The gate requires it on every back-office route (`auth.go:568-587`). | `autoFixAdmin` for the new route, the hold, cancel and close (`handlers_autofix.go:17`). | Cross-origin refusal (`auth.go:595-611`); 64 KiB body cap on the new route (§4.1). | — |
| GAOS → QA tool | GAOS's QA client (`qaclient.go`) | — | `sandboxOK` checks the shape and age of the attestation (§5.3); the job-id regex, finality hash, `count`/file rule and screening of names (§5.2, §6) | Start retries, then BLOCKED, then FAILED after 24 h (§5.5). With no usable runner, `:unit-qa` cards end FAILED and e2e cards stay RUNNING (§4.4). |
| GAOS → GitHub | GAOS's GitHub client | — | `sha40Re`, `baseBranchRe`, no `..` (§5.1); the retry wait is clamped (§5.1) | PENDING, then BLOCKED with retries |
| External-tasks API → GAOS | A bearer token naming the client (`api_tasks.go:147-158`) | `productOwns`, `CapTasks` | `parsePRRef` refuses `:`, space and `@` (`api_tasks.go:49`); `unit-qa-too-old`; `unit-qa-no-dispatch` | — |
| GAOS → model (triage/fix packets) | — | Admin release of the held card | BR-32 screening at dispatch (§7.5); `buildReviewContext` screens each file (`reviewdefect.go:337`) | — |

The rubric's premise that "`signedIn()` accepts a cookie from any request" is stale. The cookie is verified, so "only an auto-fix admin may do X" can be implemented. Authorisation is still global, though: an auto-fix admin acts on every product.

## Findings

### SEC-1 Any signed-in account can still close a released unit defect and cancel its cards through `CloseDefect` - Severity: Medium
- **Component:** the defect close route; the §7.3 admin-only rule.
- **Evidence:**
  - `docs/designs/release-b4-unit-tests.md:453` makes cancelling "any card of a `unit` defect (triage or fixer, held or released)" admin-only, so that "a non-admin cannot stop a queued unit run or orphan a released defect".
  - `docs/designs/release-b4-unit-tests.md:455` guards `CloseDefect` only "for a `unit` defect with a card BLOCKED `ReasonUnitHeld`"; "every other defect closes as today".
  - Today's close cancels every card of the defect: `dashboard/defects.go:606-607`.
  - The route is open to any session: `dashboard/handlers_autofix.go:158-165`.
- **Attack path:** A signed-in account that is not an auto-fix admin posts to `POST /products/{id}/defects/{ref}/close` for a released unit defect. `cancelDefectCardsLocked` cancels the triage or fixer card an admin released, and the defect is closed. This is exactly the result §7.3 forbids on the board, reached through a different route. It repeats round 9 SEC-2, which was closed only for `MoveCard`.
- **Remediation:**
  - In `CloseDefect`, require `admin` for every `unit` defect, not only held ones. At minimum, require it whenever the defect has a non-final card.
  - State this in §7.3 and in the "Every path out of BLOCKED" table.
  - Extend TC-870 with a non-admin close of a released unit defect: expect 409, and the cards stay where they are.
- **Effort:** quick-win

### SEC-2 The watched-test contract has no "skipped" class, and a model-written new test file can skip the failing tests, which then auto-closes the defect - Severity: Medium
- **Component:** the §11 contract; `PassUnitDefects`; `checkFix`.
- **Evidence:**
  - `docs/designs/release-b4-unit-tests.md:356` limits the per-pair status to `passed | failed | missing`; `:563` does not say what a skipped, disabled or expected-failure test reports.
  - `docs/designs/release-b4-unit-tests.md:493` keeps new test files allowed in a unit fix. `dashboard/autofixcheck.go:313-327` accepts any new file that matches `TestGlobs` and does not exist yet, which can include test setup or config files that disable tests.
  - `docs/designs/release-b4-unit-tests.md:479` closes a non-held defect as `fixed` and cancels its cards once every watched pair is `passed`.
  - `docs/designs/release-b4-unit-tests.md:655` records only that a new file "could shadow a test", not that it closes the defect.
- **Attack path:** The fixer model's input includes failing-test text and source files written by the code under test (§7.5 calls this untrusted data). That input can steer the model to add a test-glob file that skips the failing tests. If a person merges the draft and the QA tool reports a skipped watched test as `passed`, `PassUnitDefects` records the defect `fixed` with a passing `RunRef`, although the tests never ran. The same holds for any later merge that disables them. The defect record, which is the audit trail the hold protects, then says something false.
- **Remediation:**
  - Make §11 require that a watched pair which did not execute (skipped, disabled, expected-failure, filtered) is reported as a status other than `passed`, for example `skipped`.
  - Make the runner treat `skipped` as `missing`.
  - Optionally, have `checkFix` for a unit defect refuse new files matching a small list of well-known test-setup and config names, or flag them in the fix pull request body.
  - Add a TC-861 case: a `skipped` pair never closes a defect.
- **Effort:** small

### SEC-3 `qaToolKey`'s prefix checks match Brain Town keys whose GitHub owner is named `qa-run`, `staging-create` or `qa-merged` - Severity: Low
- **Component:** the lease exemption and dispatch refusal (§4.4).
- **Evidence:**
  - `docs/designs/release-b4-unit-tests.md:187-192` uses `strings.HasPrefix(key, qaRunPrefix|stagingCreatePrefix|qaMergedPrefix)`.
  - Brain Town keys start with the owner: `dashboard/braintown_consumer.go:126` (`Org + "/" + Repo + "#" …`).
  - The prefixes are `dashboard/api_cards.go:153`, `dashboard/stagingcreate.go:23` and `dashboard/mergedrun.go:18`. All three are valid GitHub organisation names.
- **Attack path:** This is not exploitable without the operator following such an organisation. If a product does follow one, every pull-request card of that owner becomes a "QA-runner key": `dispatch` and `Schedule` refuse it and its `e2e:*` handling changes. The design's claim that this key is "only the planner writes" (§4.4) depends on a namespace being disjoint, and that disjointness is not enforced. It also behaves differently by casing (`QA-Run/…` does not match).
- **Remediation:** Make the prefix branch of `qaToolKey` also require that the key contains no `#` (runner prefix keys never do, `dashboard/api_cards.go:151-152`), or check `prKeyParts`-shaped keys first. Add the case to TC-857.
- **Effort:** quick-win

### SEC-4 The alias that attributes the admin's release, cancel and close is not unique per account - Severity: Low
- **Component:** BR-48 attribution of privileged moves.
- **Evidence:**
  - `dashboard/prflow.go:524-529`: `actorAlias` keeps only the sanitized local part of the e-mail address.
  - `docs/designs/release-b4-unit-tests.md:453` and `:547` rely on `actorAlias(mv.Actor)` as the record of who released a held unit defect.
  - `dashboard/auth.go` has no check against alias collisions between accounts.
- **Attack path:** Two accounts with the same local part on different domains (one of them an auto-fix admin) produce the same alias. The event log cannot show which of them released or closed a held defect, so the audit trail of the one human gate in this design is ambiguous.
- **Remediation:** Refuse, at account load, two accounts whose `actorAlias` collides. Alternatively, record a stable, non-address account id beside the alias in `EventPayload`. State the chosen rule in §7.3.
- **Effort:** small

### SEC-5 An admin's cancel of a running QA-tool card cannot cancel the QA job when no runner is running - Severity: Low
- **Component:** the admin cancel row (§5.5) and the no-runner boot branch (§4.4).
- **Evidence:**
  - `docs/designs/release-b4-unit-tests.md:204` leaves RUNNING e2e cards RUNNING when the QA client is not usable.
  - `docs/designs/release-b4-unit-tests.md:308` says "the runner then calls `cancelJob` on the QA job".
  - The runner exists only when `srv.qaOut.usable()` (`dashboard/main.go:439-444`).
- **Attack path:** There is no attacker; the problem is a missing control. With no usable runner, an admin cancels a RUNNING e2e `create` card. The card ends CANCELED, but the QA job keeps running unwatched and may still open its spec pull request. A CANCELED card is never adopted, so nothing in GAOS ties that pull request to the card.
- **Remediation:** When the runner is not running, either refuse the admin cancel of a RUNNING QA-tool card with a stated reason, or say in the confirmation and in D-105 that the job must be cancelled on the QA tool's side. Add the case to TC-870.
- **Effort:** quick-win

## Other checks (no finding)

- **Published docs:** they use `acme/*` only and name no admin-file path (§12).
- **Stored output:** this report quotes no secret-bearing file.
- **Reviewer reach:** the design adds no reviewer. The change list edits a count in `.claude/rules/qa-test-plan.md`, which widens nothing.
- **Network surface:** the new route sits behind the gate's session and cross-origin checks. The only new outbound calls go to origins GAOS already uses (GitHub and the QA tool).
- **Delegation record:** it exists and matches §10 (`docs/designs/reviews/release-b4-unit-tests-delegation.md:14`, `:30-38`).

Counts: Critical=0 High=0 Medium=2 Low=3
