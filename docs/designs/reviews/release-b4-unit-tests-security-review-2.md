# Security review: release-b4-unit-tests.md (rev 2)

I found 1 High, 2 Medium and 6 Low findings, and nothing Critical. The High one (SEC-1) means the design can't be marked Approved until it is resolved.

## What this proposes

The design adds a per-product setting, `UnitQA`, which is off by default. When it is on, a merged pull request in an auto-fix target repository plans a `unit:qa` card instead of today's inert `unit:test` card.

1. **Starting a job.** The QA-job runner drives the card. When it starts, it reads the merge commit from GitHub. It then reads the QA tool's capabilities and sandbox declaration fresh and checks them against a fixed rule. Then it asks the QA tool either to run the repository's unit tests at that commit (`unit-run`) or to also write missing tests (`unit-create`).
2. **Outcomes.**
   - A job failure (infrastructure) fails the card and files nothing.
   - A test or build failure files a deduplicated `unit` defect, but only past a gate: a fixer is assigned, auto-fix is on, the repository is a target, and the daily caps allow it.
   - A pass closes the repository's open unit defects.
3. **The hold.** The defect's triage card is born BLOCKED with `ReasonUnitHeld`. The store refuses every move out of BLOCKED unless the session's account is an auto-fix admin, and records who made the move. A cancel by an admin also closes the defect.
4. **Tests the QA tool wrote.** In create mode, the QA tool opens a draft pull request on `qa-unit/<sha12>-<job8>` under a separate GitHub identity. GAOS records it as a `qa-unit` Pull requests row. At poll time GAOS checks it against GitHub (draft, author, head prefix, files inside `TestGlobs`) and only sends a notice when something is wrong. Pull requests that match a `qa-unit` row plan no review, e2e or unit cards.
5. **Settings.** The setting is edited by admins only, on a new route `POST /products/{id}/autofix/unit`. The design adds no new event types.

## How the existing system works, per this document

- Brain Town forwards `pr.merged`, and `prFlow.handle` plans a `unit:test` card for role `unit-test`. Nothing runs tests, and a FAILED card only sends a notice.
- The QA-job runner already drives e2e cards. It calls `startAutomation`, polls `/gaos/jobs/{id}`, moves the card to Needs you while an approval is pending, and records QA pull requests as rows.
- Defects follow the D-091 path: triage card, then triager, then fixer, then a draft `gaos/fix-*` pull request.
- Auto-fix admins are the accounts listed in the mode-600 file `GAOS_AUTOFIX_ADMINS`.
- A Blueprint sync's drift release does not touch the block reasons listed in `operatorHolds`.

## Trust boundaries walked

| Boundary | Authenticates | Authorises | Validates | If the far side fails |
|---|---|---|---|---|
| operator browser → back office | HMAC-signed session cookie (`dashboard/auth.go:329-359`); the gate stores the session (`auth.go:568-586`); cross-origin writes refused (`auth.go:595-612`) | `autoFixAdmin` for the new route and the release (`handlers_autofix.go:17-37`) | form refusals (design :95-102) | fails closed |
| GAOS → QA tool (start, poll, capabilities) | existing `qaOutbound` credential (not opened) | none on GAOS's side | `sandboxOK` checks shape and age (design :176-187); `unit` result screened and capped (design :203) | job failure → FAILED, no defect |
| QA tool report → GAOS store | same | **the report decides card status, defect filing, defect closing and `qa-unit` row creation** | partial; see SEC-1 and SEC-3 | |
| GAOS → GitHub | existing reader token | read only | merge commit and `shaRe`; poll-time checks | stays PENDING, BLOCKED after 10 tries |
| QA unit identity → GitHub | QA tool's own token | ruleset `qa-unit/*`, no workflows permission (an assumption, design :367) | not verifiable from GAOS | |

**The rubric's starting assumption is out of date.** The rubric says `signedIn()` accepts any cookie. In the current tree it verifies the MAC before parsing (`auth.go:337-341`) and checks the account fingerprint (`auth.go:354-356`). So "only an auto-fix admin may release" can actually be enforced. The store-level guard with a JSON-hidden `Actor` field (design :271) is the right place for it. I also confirmed the design's list of ways out of BLOCKED:
- The board offers only unblock to Ready and cancel from BLOCKED (`board.go:234-239`).
- Drift release respects `operatorHolds` (`workspace.go:395`).
- The design-gate release touches implement tasks only (`designgate.go:795`).
- The external-API cancel is limited to the client's own `qa-run` cards (`api_cards.go:435-440`).

## Findings

### SEC-1 A QA-tool `pr_url` can claim any target-repository pull request as `qa-unit`, and the claim switches off review planning for it - Severity: High
- Component: §8.1 row write, §8.3 own-PR skip, `upsertPullRequestLocked`
- Evidence:
  - The row is written from the QA tool's `pr_url`, and the repository only has to be in `TargetRepos`, not equal the card's repository (design :310, :312).
  - The skip fires on the row's mere existence, whatever the check found (design :327).
  - The design's own threat statement says the row is checked "against GitHub, not against the QA tool's report" (design :36).
  - `TargetRepos` overlaps the review repositories, unlike `AutomationRepos` for `qa` rows (`dashboard/pullrequests.go:97-102`).
  - Today's upsert does not refuse an existing row of another kind; it fills empty fields (`pullrequests.go:129-157`). That contradicts design :312 ("refused … a row of another kind already holds the key"), and §12 lists no change that adds such a refusal.
  - A draft pull request gets no review row, because the draft return comes before `reviewRowLocked` (`prflow.go:232`, `:290`).
- Attack path: a compromised or buggy QA tool reports a `pr_url` for a person's draft pull request, or one GAOS has not seen yet, in any target repository. GAOS writes a `qa-unit` row first. When that pull request becomes ready or is merged, `ownUnitPRLocked` plans no pr-reviewer, e2e or unit card. The code merges without GAOS's review, and the only signal is a later `unit:files` notice. The same applies to a legitimate `qa-unit` pull request that is later amended (SEC-2): its merge skips review.
- Remediation:
  - Before writing the row, require the `pr_url` repository to equal the card's `owner/repo`.
  - Read the pull request from GitHub at write time and require all of these: head exactly `qa-unit/<sha12>-<job8>` for this job and this merge commit, author equal to `UnitQA.Login`, draft, and not from a fork.
  - Make `ownUnitPRLocked` skip only when `QANote == "checked"` at the current head SHA. Otherwise plan the normal review card.
  - Add the kind-collision refusal to `upsertPullRequestLocked`, put it in §12, and add a test case for each.
- Effort: small

### SEC-2 The poll-time check runs once, pins no head commit and ignores later pushes - Severity: Medium
- Component: §8.2, `pollRow`
- Evidence:
  - The check runs only while `QANote` is empty and sets `"checked"` once (design :316-323).
  - `pollRow` keeps refreshing `HeadSHA` but nothing re-runs the check (`dashboard/fixprpoll.go:365-366`).
  - The review-fix check that §8.2 copies pins the head commit and refuses forks (`dashboard/reviewdefect.go:229-232`); §8.2 does neither.
  - Only the pull request's author is compared, not who pushes to it.
  - The check is detective only: CI has already run on push before GAOS looks, and GAOS never closes the pull request (design :323).
- Attack path: after GAOS marks the pull request checked, more commits are pushed to the `qa-unit/*` branch, either by the unit identity or by any account with write access (the ruleset limits the identity, not other writers). The commits add files outside `TestGlobs`. The row still says "checked", and a person who trusts it merges. Combined with SEC-1, that merge also skips review.
- Remediation:
  - Store the head SHA the check ran against (`checked@<sha>`) and re-run the check whenever the head changes.
  - Refuse forks, as `reviewdefect.go:231` does.
  - Re-check on `pr.merged` of a `qa-unit` row and send a notice on any violation.
  - Show "unchecked" in `prview.go` whenever the stored and current heads differ.
- Effort: small

### SEC-3 A late `passed` result closes defects found at a newer commit and cancels their held cards - Severity: Medium
- Component: §7.4 "Passed", §6 outcome table
- Evidence: a `passed` answer closes every non-final unit defect of the repository and calls `cancelDefectCardsLocked` (design :54, :208, :286). No comparison against the defect's `RunRef` or merge order is specified.
- Attack path: merges A and then B start two concurrent unit jobs. B fails and files a held defect. A's slower job then answers `passed` and closes B's defect, and the machine cancels the triage card the admin was meant to decide on. A QA tool that misreports `passed` does the same. This silently undoes the hold the design exists to provide.
- Remediation: close only defects whose recorded merge commit equals the passed commit or is an ancestor of it (GitHub compare API). Otherwise only update `LastSeen`. Keep the full 40-hex commit on the defect (see SEC-4).
- Effort: small

### SEC-4 The job's link to its commit is lost on restart - Severity: Low
- Component: §5.1, runner adoption
- Evidence:
  - The design says "A job adopted after a restart needs no commit" (design :138).
  - The reason string carries only 12 hex characters (design :134).
  - Adoption rebuilds `qaTracked` from the reason alone, with no `sha` (`dashboard/qajobs.go:355-361`).
  - Yet `PlanUnitDefect`, `RunRef`, `buildUnitContext` "at the merge commit" and the `passed` closure all need the commit (design :227, :250, :292).
- Attack path: after a restart, a defect is filed with an empty or shortened commit. Its context is built at the wrong ref, and the SEC-3 ancestry rule cannot be applied reliably.
- Remediation: on adoption, re-read the merged pull request from the card's key as §5.1 does, or write the full 40-hex SHA into the reason.
- Effort: quick-win

### SEC-5 "The runner is its only driver" holds for the scheduler, not for a manual board dispatch - Severity: Low
- Component: §4.2
- Evidence:
  - The design claims `labelsAllowDispatch` refuses `unit:qa` (design :108).
  - That function is consulted only in `dashboard/scheduler.go:286` and `dashboard/apidispatch.go:47`.
  - The board's Ready → Working move goes to `dispatch` with no label check (`dashboard/board.go:226-227`, `:466-467`, `:479-503`).
  - Both `unit:qa` block reasons tell the operator to "move this card to Ready" (design :136, :187).
- Attack path: a signed-in user drags a ready `unit:qa` card to Working. A `unit-test` agent turn is spent while the runner also starts the QA job, so two actors run on one card.
- Remediation: refuse dispatch in `dispatch()` (or in `legalMove`) for labels that `labelsAllowDispatch` refuses, or at least for `unit:qa`, and add a test case.
- Effort: quick-win

### SEC-6 Values read from GitHub go into an event and a WhatsApp notice without a stated cap or screen - Severity: Low
- Component: §8.2 notes
- Evidence: `files outside TestGlobs: <first path>` and `author <login>` are written to `QANote`, to the `RecordUpdated` event text and to the `unit:files` notice (design :323). No cap, character filter or `screenBody` step is named, unlike §6 for test names (design :203).
- Attack path: a file name in the pull request carries a very long string, control or bidi characters, or a credential-shaped name. It lands in the append-only event stream and in a group chat message.
- Remediation:
  - Cap the path and filter its characters (a rule like `prRefOK`) and run `screenBody` on it.
  - When the failing reason is `credentialShaped`, write "a credential-shaped path" instead of the name.
  - Validate the login against `^[A-Za-z0-9-]{1,39}$` before writing it.
- Effort: quick-win

### SEC-7 Reopened defects are not counted against the daily unit cap - Severity: Low
- Component: §7.2 step 4, §7.4
- Evidence:
  - The cap counts defects whose `FirstSeen` is today (design :262).
  - A reopen keeps the record, so `FirstSeen` is unchanged, while it plans a new held card and sends `unit:failure` (design :269, :288).
- Attack path: a flapping test that alternates fail and pass on every merge reopens the same defect each time. Each reopen produces a held card and a group notice, with no cap.
- Remediation: count reopens (a `Gen` increase today) toward `UnitCap()`, or send at most one `unit:failure` notice per defect per UTC day.
- Effort: quick-win

### SEC-8 The page-load sandbox read is a live outbound call on every view - Severity: Low
- Component: §4.1 page
- Evidence:
  - The section shows "the last attestation read on page load" (design :104).
  - `unitSandbox` never caches (design :163).
  - The Auto-fix view has a 30 s live poll (`dashboard/main.go:554`).
  - The fresh window also accepts a declaration up to 7 days old (design :185), so "fresh" describes the transport, not the declaration.
- Attack path: every open Auto-fix tab, through the live poll, makes uncached calls to the QA tool, so viewers multiply load on that origin. The wording can also mislead an admin about how current the declaration is.
- Remediation:
  - Read the declaration on the full page render only, never in `/ui/bo/autofix/live`, and keep the runner's read uncached.
  - Show `attestedAt`'s age next to the 7-day limit, or narrow the window (for example to 24 h) to match the word "fresh".
- Effort: quick-win

### SEC-9 The docs that will be published do not say what must stay out of them - Severity: Low
- Component: §12 docs rows
- Evidence: `pustaka/docs/integrations/pr-agents.html` and `agents.html` are to describe the unit flow (design :397). The published site already exposes topology.
- Attack path: a published page that names the `UnitQA.Login`, the admin-file location, or example target repositories from a real product adds to an attacker's map.
- Remediation: say in §12 that the pages describe the mechanism with the invented `acme/*` example only, and publish no login, admin-file path or product repository.
- Effort: quick-win

Counts: Critical=0 High=1 Medium=2 Low=6
