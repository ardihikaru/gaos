## Security review: `docs/designs/release-b4-unit-tests.md` (rev 6)

The design has one High finding, so under the policy it cannot move to Approved until that is fixed. There are also three Medium and two Low findings.

### What this proposes

- **A per-product switch, `UnitQA`.** It is off by default and only an auto-fix admin can change it. When it is on, a merged pull request in a fix-target repository plans a `unit:qa` card instead of today's `unit:test` card. Brain Town and the external-tasks API both plan it, under the same key.
- **The QA-job runner drives `unit:qa` cards. No model agent works them.** At start the runner:
  - reads the merge commit, `merged_at` and the base from GitHub itself;
  - requires the base to be the effective base or an allowed base;
  - takes a fresh sandbox attestation from the QA tool;
  - sends `unit-run` with `repo`, `sha` and a `watch` list.

  Concurrent runs and starts per tick are capped. A blocked start retries for 24 hours.
- **These cards hold no live model turn (BR-38 amended).** The exemption is keyed on a key only the planner writes plus the runner's `qa-job:` reason. Labels no longer count. `dispatch` refuses every card with a QA-runner key.
- **A test or build failure files a `unit` defect.** Failing tests and files are screened. Filing passes a refusal gate (fixer, triager, auto-fix on, fix target, daily cap, global cap) and is deduplicated per repository, base and failing set. A failure of the job itself files nothing.
- **When every watched test of an open defect passes at a newer merge on the same base, the defect closes.**
- **The defect's triage card is born BLOCKED with `ReasonUnitHeld`.** The checks for this live in the store, not only the UI.
  - Only an auto-fix admin can unblock it, cancel it, or close its defect. These actions are attributed to the admin by alias.
  - `resolveCardLocked`, drift release and `CloseDefect` refuse every other path.
  - The one machine exit is the pass-cancel.
- **After an admin releases it, the existing D-091 triage/fix path runs.** Unit-specific branches use a new `UnitBase`, which is checked again when the fix pull request opens.
- **Writing missing tests (`unit-create`) is out of scope.** It moves to `release-b4b`.

### How the existing system works, per this document

- A merge plans a `unit:test` card for an agent that has no checkout. Nothing runs the tests, and a FAILED card only sends a notice.
- The QA-job runner already drives e2e cards. It picks them by label (`isE2ECard`) and keeps the job id in the card's reason so it can adopt the job after a restart.
- The graph lease counts every DISPATCHED or RUNNING card.
- `ResolvePRCard` has a fixed table of moves.
- Auto-fix admins are listed in a mode-600 file and checked in the handlers.
- Defects reach triage and then a fixer through D-091.
- Back-office identity is a verified session (`sessionOf`).

I verified that last point myself: `auth.go:330-359` checks an HMAC, an expiry and an account fingerprint, and `auth.go:586` stores the verified session on the request. So the rubric's statement that the cookie "checks nothing" is out of date. The admin identity this design relies on does exist. What does not exist is roles (OQ-37, `brd.html:157`): every signed-in account can do everything that is not file-gated.

### Trust boundaries touched

| From → To | Authenticates | Authorises | Validates | Far side fails |
|---|---|---|---|---|
| Operator browser → dashboard (Unit tests route, board moves, defect close) | verified session (`auth.go:586`) | `autoFixAdmin` file in the handler; `Admin` sent to the store as a `json:"-"` field | store guard in `MoveCard` / `CloseDefect` | fails closed: no session means `Admin=false` |
| Dashboard → QA tool (capabilities, start, poll) | bearer to an operator-set URL (`qaclient.go:97-130`) | none on GAOS's side; the QA tool's answer can set the card's outcome, file defects and close defects (BR-54 amended) | `sandboxOK`, result screening, 50-entry cap, `watched` limited to the names sent | PENDING/BLOCKED with retry, then FAILED after 24 h |
| Dashboard → GitHub (PR read, default branch, files, context) | GAOS token | — | `sha40Re`, base rule, `contextPathOK` plus globs | stays PENDING; GitHub off means a no-op |
| External-tasks client → dashboard | client token | `unitQAFor`, client caps, `unit-qa-too-old` | BR-59 patterns | 422 or 409 |
| QA tool sandbox → merged code | — | — | the QA tool's own declaration only | residual (D-105) |

### SEC-1 Operator-settable e2e labels override the job mode of a `unit:qa` card - Severity: High
- **Component:** QA-job runner mode selection; label route.
- **Evidence:**
  - `dashboard/prflow.go:549-557`: `setTaskLabelLocked` refuses only the `defect`, `source` and repository-kind families, so `e2e:automation` and `e2e:run` can be set on any card.
  - `dashboard/qajobs.go:230-237`: `isE2ECard` checks `e2e:automation` first, then `e2e:run`.
  - `dashboard/qajobs.go:380`: `AutoMerge` is armed when `mode == "create"`.
  - Design §5.2 (line 189) says only that `isE2ECard` "returns mode unit for unit:qa", with no precedence.
  - Design §4.2 (line 112) locks only the `unit` family on a `unit:qa` card.
  - §5.2 also makes `caseCodeFor` answer `"*"` for a `unit:qa` card before any other branch.
- **Attack path:**
  1. Any signed-in account (there are no roles, OQ-37) adds `e2e:automation` (or `e2e:run`) to a PENDING `unit:qa` card through `POST …/label`.
  2. The key still ends `:unit-qa`, so the new key-only `Tick` accepts the card.
  3. If the label check comes first, the runner starts a `create` job with case `*`, and can arm auto-merge where the product's band allows it, or it starts an e2e run of every case.

  This lets a non-admin reach the QA tool's write path, which this design says it never uses ("Nothing in this design writes, pushes…", line 15). It is the same class of problem as round-5 SEC-1, a label forging the runner's behaviour, moved from the lease to the mode.
- **Remediation:**
  - Derive the mode from the planner-set key, not from labels: a key ending `:unit-qa` is always `unit-run`, `AutoMerge` is nil and the case is `*`, whatever labels the card carries.
  - In `setTaskLabelLocked`, refuse any `e2e:*` label on a card whose key is a QA-runner key of another kind.
  - State both in §4.2 and §5.2, and add a case to TC-856 or TC-863: a `unit:qa` card with `e2e:automation` added still sends `unit-run` with no `auto_merge`.
- **Effort:** quick-win

### SEC-2 Boot reconciliation requeues RUNNING `unit:qa` cards to READY, which breaks the act-once marker - Severity: Medium
- **Component:** adoption after a restart; `ResolveUnitQACard`.
- **Evidence:**
  - `dashboard/runner.go:706-718`: `Reconcile` moves every DISPATCHED or RUNNING task that has no open turn to READY, and keeps `BlockReason` (`taskstate.go:152-169`).
  - `dashboard/main.go:292-301` runs it at every boot.
  - A `unit:qa` card never has a model turn, so after any restart it is READY with its `qa-job:` reason.
  - Design §5.5 (line 262) allows the marker write only RUNNING → RUNNING.
  - §5.4 (line 251) promises that "a restart never acts twice", in particular that an admin-closed defect is not reopened (TC-867).
  - The design never mentions `Reconcile`.
- **Attack path:**
  1. A job is adopted after one restart; its card is now READY.
  2. It reports a failure. `PlanUnitDefect` runs, but the marker write is refused because the card is not RUNNING. §6 (line 317) specifies retries only for steps (1) and (2).
  3. Meanwhile an admin closes the defect.
  4. A second restart before the job ends gives `unitDone=false`, so the same job acts again and reopens the defect with a new held card.

  An admin's attributed decision is undone by the machine. It is bounded by the daily cap, but it defeats a guarantee the design states.
- **Remediation:** Pick one of these and say which in §5.4 and §5.5:
  - make `Reconcile` skip cards for which `qaToolDriven` holds; or
  - allow READY → READY and READY → RUNNING marker rewrites in `ResolveUnitQACard` for a card whose reason carries `qa-job:`.

  Also state what happens when the step (5) marker write fails: keep the job tracked and retry before any final move. Extend TC-867 to cover a boot that goes through `Reconcile`.
- **Effort:** small

### SEC-3 The fix-target authorisation is decided at planning and never checked again before code runs - Severity: Medium
- **Component:** runner start and the BLOCKED retry.
- **Evidence:**
  - Design §5.2 (line 189): at start only `UnitQA.Enabled` is checked again.
  - `unitQAFor` (TargetRepos and not AutomationRepos, §4.2 line 114-117) is evaluated only by the planner.
  - Cards wait PENDING without limit (§4.2 line 123) and retry BLOCKED for 24 h (§5.5 line 267).
  - The design's own argument against planner-time checks (§0 departure 1, line 20) applies equally here.
- **Attack path:**
  1. An auto-fix admin removes a repository from `AutoFix.TargetRepos`, or moves it to `AutomationRepos`, for example because its recent merges are not trusted.
  2. Already-queued and blocked `unit:qa` cards still send that `repo` and `sha` to the QA tool, which checks out and runs the merged code in its sandbox.
  3. The defect gate (§7.2 step 4) stops the defect, but not the execution.
- **Remediation:**
  - Evaluate `unitQAFor(m, owner, repo)` at every start and every BLOCKED retry.
  - On false, end the card through `ResolveUnitQACard` as CANCELED `repository no longer a fix target; nothing ran`.
  - Add the row to the §5.1 table and a case to TC-862.
- **Effort:** quick-win

### SEC-4 The sandbox contract leaves the checkout credential and the network allowlist undefined - Severity: Medium
- **Component:** QA tool contract (§11) and `sandboxOK` (§5.3).
- **Evidence:**
  - Design line 212-221 accepts `network: allowlist` with no content, and `secrets: none`.
  - Line 478 requires "no access to the QA tool's … credentials", yet line 77 has the QA tool check `repo` out at `sha`. For a private repository that needs a GitHub credential, and the contract does not say where it lives.
- **Attack path:**
  - The code under test is arbitrary merged code, so a contributor's test or build script runs in the sandbox.
  - If the checkout token is left in the clone's config or environment inside the container, or the "allowlist" is wide, that code can read or exfiltrate it.
  - GAOS would still accept the attestation, because it checks only shape and age.
- **Remediation:**
  - In §11, require the checkout to happen outside the run container, with a read-only, single-repository, short-lived credential that is never present in the container's filesystem or environment.
  - Add a matching attested field (for example `checkout: outside`) to `qaSandbox` and `sandboxOK`.
  - Accept `allowlist` only when the attestation lists its hosts, show them on the Auto-fix page, and default `sandboxOK` to `none` unless an admin opts in.
  - Record both items as residuals in D-105.
- **Effort:** small

### SEC-5 The pass-cancel releases a held card on the QA tool's word, and the record does not show which job did it - Severity: Low
- **Component:** `PassUnitDefects`.
- **Evidence:**
  - Design §7.4 (line 400) calls `setDefectStateLocked(d, DefectStateFixed, "unit-passed", "")` with an empty actor.
  - §7.3 (line 396) makes this the only machine exit from the hold.
  - BR-54 amended (line 464).
- **Attack path:** A faulty or compromised QA tool answers `watched: passed` for every name GAOS sent at any newer merge. Every open unit defect of that repository and base closes and its held cards are cancelled, silently. The event names no job or commit, so an auditor cannot tell which run did it.
- **Remediation:**
  - Carry the job id and `sha12` in the payload of the pass-close (for example in `RunRef` and the event text).
  - Send one group notice, `defect <h8> closed: watched tests passed at <sha12>`, whenever a held card is cancelled this way.
  - Assert both in TC-860.
- **Effort:** quick-win

### SEC-6 An uncached outbound read on every full render of the Auto-fix page - Severity: Low
- **Component:** Auto-fix page, Unit tests section.
- **Evidence:**
  - Design §4.1 (line 108) does a fresh attestation read on every full page render, for any signed-in viewer.
  - `dashboard/qaclient.go:38-40` allows 15 s and 2 MiB per call.
  - `unitSandbox` has no cache by design (line 199).
- **Attack path:** Any signed-in account (or a stuck browser) reloading the page keeps the QA tool busy and holds request goroutines for up to 15 s each.
- **Remediation:**
  - Use a short display-only cache (for example 60 s) or the existing cooldown pattern (`qaCooldown`, `qaclient.go:42`) for the page.
  - Keep the uncached read for the save handler and the runner start.
- **Effort:** quick-win

### Checked and holding (no finding)

- **The admin flag cannot come from the request.** `CardMove.Actor` and `CardMove.Admin` are `json:"-"`, and `handleMoveCard` decodes the body into `CardMove` (`handlers_board.go:62-71`).
- **The existing QA defect intake cannot reach a unit defect.**
  - An `xpass` item must carry a `DefectID` matching `^DEF-\d{1,6}$` (`defects.go:68`, `:210`) and a `TC-` case (`qacatalog.go:44`).
  - `unit:` keys and case ids match neither, so `ReportDefects` (`defects.go:432-442`) cannot close a unit defect or cancel its held card.
- **Every card key is written by a planner.** `DedupKey` is set only by planners (`api_cards.go:64,73`, `defecttriage.go:111,122`). The API's `qa-run/` keys always end in `:e2e-run` (`api_cards.go:165`), so a client cannot mint a `:unit-qa` key.
- **Drift release cannot free the held card.** It matches `operatorHolds` by equality (`sendback.go:153-159`, `workspace.go:395`), so `ReasonUnitHeld` (an exact constant) is safe.
- **The API exposes no job ids or GitHub error classes.** `apiPublicReason` maps reasons to a closed set (`api_cards.go:453-463`).
- **QA-tool text cannot carry markup or break the packet's fence.** `stripBR53` removes backticks, angle brackets and control/format characters (`defects.go:251-264`), and the templates use `html/template`.
- **Published content:** only the `acme/*` example names and no admin file path (§12, line 525).

Counts: Critical=0 High=1 Medium=3 Low=2
