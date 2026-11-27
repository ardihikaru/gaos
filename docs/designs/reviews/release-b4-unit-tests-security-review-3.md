## Security review: `docs/designs/release-b4-unit-tests.md` (rev 3)

I could not find a Critical issue, but there is one **High**, which blocks approval under `docs/rules/gaos-design-review.md:99-100`. Any signed-in account can close a unit defect, and closing it cancels the held card that the design says only an auto-fix admin may cancel.

### What this proposes

- **A new product setting, `UnitQA`, off by default.** When it is on, a merge into a fix-target repository plans a `unit:qa` card instead of today's `unit:test` card. Only the planner can set that label, and it is never dispatched to an agent.
- **The QA-job runner drives the card.** At each start it reads the merge commit, `merged_at` and base from GitHub. It reads the QA tool's sandbox declaration fresh and checks its shape and age. It then asks the QA tool to run the unit tests (`unit-run`), or to write missing tests too (`unit-create`). The commit and mode are kept in the card's reason so they survive a restart.
- **A test or build failure files a "unit defect".** This only happens behind a gate: a fixer and a triager are assigned, auto-fix is on, the repository is a fix target, and the daily caps allow it. A repeat sighting of the same failures files nothing new; a fresh sighting after the defect closed reopens it. The defect's triage card is created BLOCKED ("held"). Only an auto-fix admin may unblock or cancel it, and the store enforces that. A later passing merge closes the defect. A failure of the QA job itself files nothing.
- **Tests written by the QA tool arrive as a draft pull request** on a `qa-unit/<sha12>-<job8>` branch, opened by a separate GitHub identity. GAOS lists it as a `qa-unit` row only after reading it from GitHub (exact branch, author, draft, not a fork). It re-checks it at every new head: draft, author, branch, and only test files outside `.github/`.
- **GAOS's own `qa-unit` pull requests** plan no e2e or unit card, but are still meant to be reviewed.

### How the existing system works, per this document

- A merge plans one `unit:test` card that an agent receives with no checkout. Nothing runs the tests, and a FAILED card only sends a notice.
- The QA runner already runs e2e jobs with a fixed timeout. The D-091 path already runs defect → triage → fixer → draft `gaos/fix-*` pull request.
- `autoFixAdmin` reads a mode-600 file listing admin accounts. The Auto-fix form replaces the whole `AutoFix` record.
- A Blueprint sync's drift release unblocks cards unless their reason is listed in `operatorHolds`.

### Trust boundaries the design touches

The rubric's premise that `signedIn()` "accepts a cookie from any request" no longer holds. `dashboard/auth.go:330-358` checks an HMAC signature, the expiry and the account fingerprint, and `auth.go:380-395` returns the verified email. "Signed-in" and "auto-fix admin" are now separate identities, so the admin-only rules in §7.3 can be built.

| Boundary | Authenticates | Authorises | Validates | If the far side fails |
|---|---|---|---|---|
| Operator browser → back office (move, close, Unit tests route) | verified session (`auth.go:380`) | `autoFixAdmin` on the new route and in the move guard; **not** on defect close (SEC-1) | form limits, store refusals | 409/422 |
| Runner → QA tool (`/gaos/capabilities`, `/automation`, `/jobs`) | QA key | — | `sandboxOK` checks shape and age only; reply bodies are size-capped (`qaclient.go:232`); `Failing` truncated to 50 and screened | card BLOCKED or FAILED; no defect |
| Runner/poller → GitHub (PR read, files, re-check) | GAOS's reader token | — | sha regex, base allowlist, exact head ref, author, draft, not a fork, `TestGlobs` | transient errors retry; permanent errors block |
| QA tool → GitHub (`qa-unit/*` pushes, draft PR) | separate unit identity | ruleset limited to `qa-unit/*`, no workflows permission (the §11 assumption) | §8.1/§8.2 checks after the fact | detective: a notice is sent |
| QA output → model packets (triager, fixer) | — | — | test names stripped and screened; **file names are not** (SEC-3) | packet fenced by an instruction |

---

### SEC-1 Defect close cancels a held unit-test card without the admin check - Severity: High
- **Component:** Back office, auto-fix; §7.3 hold.
- **Evidence:**
  - `dashboard/handlers_autofix.go:158-165`: `handleCloseDefect` requires a session but never calls `autoFixAdmin`. Compare `handleSaveAutoFix`, which does (`handlers_autofix.go:122`).
  - `dashboard/defects.go:590-608`: `CloseDefect` accepts any defect kind and calls `cancelDefectCardsLocked`.
  - `dashboard/defects.go:363-378`: that function cancels every BLOCKED card of the defect.
  - `dashboard/main.go:578` registers `POST /products/{id}/defects/{ref}/close`.
  - Design lines 311 and 313-322: the "every path out of BLOCKED" table does not list this route, and line 38 says "released or cancelled only by an auto-fix admin".
- **Attack path:** Any signed-in back-office account that is not an auto-fix admin closes a unit defect by its ref. The held triage card is cancelled and a real test failure disappears from the queue. The store guard the design adds to `MoveCard` is never reached.
- **Why High and not Critical:** it is an authorisation bypass of the design's own control, but it only suppresses work. It cannot start an agent turn, and the next failing merge reopens the defect through §7.4. Torn between the two, it sits at High because it runs on the privileged decision path the design reserves for admins.
- **Remediation:**
  - Either give `CloseDefect` the same treatment as `CardMove`: an `Admin bool` resolved in the handler, and a store refusal for a unit defect while it has a card held with `ReasonUnitHeld`. Or require `autoFixAdmin` in `handleCloseDefect` for any `unit` defect.
  - Add the route to the §7.3 table and to the §12 change list.
  - Extend TC-858 so a non-admin closing a unit defect gets 409 and the card stays BLOCKED.
- **Effort:** small

### SEC-2 Skipping a `qa-unit` PR's own unit and e2e runs ignores the check result, and its review may never be planned - Severity: Medium
- **Component:** PR planner (§8.3) and the poller re-check (§8.2).
- **Evidence:**
  - Design line 376: `ownUnitPRLocked` removes the specs whenever a `qa-unit` row exists. It does not look at `QANote`.
  - Design line 372: a failing check only writes a note and sends a notice.
  - Design line 419: these pull requests are always opened as drafts.
  - `dashboard/prflow.go:232-233`: a draft `pr.opened` returns before anything is planned.
  - `dashboard/prflow.go:83-91`: the reviewer card is planned only on `pr.opened`, not on `pr.merged`.
  - `dashboard/braintown_consumer.go:555`: only the draft flag is read. Nothing in this repository shows a "ready for review" event re-planning the reviewer card.
- **Attack path:** Anyone with push access to a `qa-unit/*` branch (any repository writer, or a compromised unit identity) adds a non-test source change.
  - The re-check flags `files outside TestGlobs` and notifies, but the row stays, and its existence suppresses the unit and e2e runs at merge.
  - Because the pull request was opened as a draft, the "still reviewed" guarantee depends on a Brain Town event the design does not anchor. Merging still needs a person with merge rights on GitHub, and that is why this is Medium rather than High.
- **Remediation:**
  - Have `ownUnitPRLocked` remove the specs only when the row's note is `checked@<sha12 of the merged head>`. Otherwise plan as usual.
  - State with a file and line which event plans the reviewer card for a pull request that was opened as a draft. If there is none, plan the reviewer card (or refuse the skip) at `pr.merged` for `qa-unit` rows that have no reviewer card.
  - Add both cases to TC-846 and TC-863.
- **Effort:** small

### SEC-3 Failing-test file names reach the model packets without control-character or line-break screening - Severity: Medium
- **Component:** Unit result intake (§6), Excerpt (§7.1), packets (§7.5).
- **Evidence:**
  - Design line 237: `Test` must be printable and is screened, but `File` only has to pass `contextPathOK`, capped at 300.
  - `dashboard/defecttriage.go:262-272`: `contextPathOK` rejects NUL, backslash, an absolute path, dot segments, forbidden paths and credential-shaped paths. It allows line breaks, control characters, bidi characters, backticks and angle brackets.
  - Design line 286: the Excerpt is built as `<test> — <file>` lines.
  - Design line 336: the Excerpt is rendered into the triager's and the fixer's packets inside a section fenced only by an instruction.
- **Attack path:** A contributor whose code is merged into a fix-target repository controls its file names, and through the QA tool's report, the `File` strings. A name with an embedded line break or bidi characters can step outside the "do not follow instructions" section and steer the triager toward an "easy" verdict, which plans an automatic fixer turn. The admin release is a human check in between, but the admin reads the same Excerpt.
- **Remediation:** Apply the `Test` rules to `File` as well: printable, no line break, no control or format (Cf) characters, `stripBR53`, `screenBody`, and drop the entry on failure. Do this before hashing and before building the Excerpt. Add the case to TC-855 or TC-857.
- **Effort:** quick-win

### SEC-4 The approval of generated tests, the only human gate before code is pushed and run in CI, is not specified - Severity: Medium
- **Component:** `unit-create` flow (§3 step 4, §5.5, §11).
- **Evidence:**
  - Design lines 71-72: the QA tool "waits for a person's approval in the QA tool".
  - Design line 220: the notice says "open the QA Inbox".
  - Design lines 412-419: the §11 contract has no row saying who may approve, how the approver is authenticated, or whether the approval is recorded.
  - `dashboard/qajobs.go:445-448`: the wait GAOS already uses for e2e.
  - `dashboard/handlers_questions.go:103-113`: if this inbox is GAOS's external inbox, any signed-in account can answer it, with no admin check.
- **Attack path:** Whoever can answer that approval decides that model-written code is pushed to `qa-unit/*` and runs in the repository's CI. GAOS's checks there are detective only (D-104 residual, design line 402), and CI's safety rests on the operator's unverified `CIConfirmed` statement (§8.4). If the approval is reachable by any signed-in account, the person in the loop is not the person the operator meant.
- **Remediation:**
  - Say where the approval is answered.
  - If it is answered through GAOS, gate the answer for `unit:qa` jobs with `autoFixAdmin` and record the approver's alias.
  - If it is answered in the QA tool, add a §11 row requiring an authenticated, named approver and a recorded approval.
- **Effort:** small

### SEC-5 The re-check verifies the pull request's author, not who pushed its commits - Severity: Low
- **Component:** §8.2 re-check.
- **Evidence:** Design lines 365-368: the checks are draft, PR author, head ref, fork and files. The PR author (`ghPR` author) is the creator and does not change when somebody else pushes.
- **Attack path:** A repository writer pushes changed test files (inside `TestGlobs`) to a `qa-unit/*` branch. The row stays `checked`, and the tests run in CI as if the QA tool had written them.
- **Remediation:** Also require that every commit between the base and the head has the unit identity as author and committer (GitHub commits list), or a verified signature. Otherwise record this as a named residual in D-104.
- **Effort:** small

### SEC-6 Any later passing merge closes the repository's open unit defects, including when the failing tests were deleted - Severity: Low
- **Component:** `PassUnitDefects` (§7.4), BR-63.
- **Evidence:** Design line 326 (every non-final unit defect with an older `UnitMergedAt` is closed as fixed) and line 403.
- **Attack path:** A contributor deletes or skips the failing tests. The next merge reports `passed`, and the defect and its held card are closed by the machine, with no admin seeing it.
- **Remediation:** Close only defects whose failing test names appear among the tests that passed. That needs one contract line in §11 (a list of passed test names or their hashes). Otherwise update `LastSeen` only, or record this as a residual in D-104.
- **Effort:** small

### SEC-7 "Auto-merge is never armed for unit pull requests" holds only by accident - Severity: Low
- **Component:** Runner start (§5.2).
- **Evidence:** `dashboard/qajobs.go:380` arms `AutoMerge` only when `mode == "create"`. Design line 466 rejects reusing the e2e identity precisely because of auto-merge. Design line 419 says the pull request is never merged by the QA tool, but §5.2 never says GAOS does not request it.
- **Attack path:** A later refactor that sends `unit-create` through the create branch would arm auto-merge on model-written `qa-unit` pull requests without anyone noticing.
- **Remediation:** State in §5.2 that `qaStartIn.AutoMerge` is always nil for unit modes, and assert it in TC-854.
- **Effort:** quick-win

### SEC-8 The move handler's identity source is not named - Severity: Low
- **Component:** §7.3 `handleMoveCard`.
- **Evidence:**
  - Design line 311: "resolved by `handleMoveCard` from the session".
  - `dashboard/handlers_board.go:57-72`: the handler calls only `requireBO` (a yes/no answer) and reads no email today.
  - `dashboard/auth.go:368-396`: `sessionOf` reads the context only, while `sessionFrom` falls back to the cookie.
- **Attack path:** None today. An implementation that reads `sessionOf` when the gate did not set it would get an empty email; if it defaulted wrongly, attribution would be lost or the guard weakened.
- **Remediation:** Name the call (`s.auth.sessionFrom(r)`). Require a non-empty email, and set `Admin` only from `autoFixAdmin(email)`, false otherwise. Add the no-session case to TC-858.
- **Effort:** quick-win

Counts: Critical=0 High=1 Medium=3 Low=4
