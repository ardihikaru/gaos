# Security review: agent-seats-and-pr-agents (Release A, rev 2)

Reviewed `/home/ardi/devel/projects/ardihikaru/my-projects/gaos-agent-seats/docs/designs/agent-seats-and-pr-agents.md` against `docs/rules/gaos-design-review.md`, `docs/designs/README.md`, the BRD, the decision log and the open-question register, with the anchors checked in `dashboard/`. Nothing under `.claude/` and no credential file was opened.

## Paraphrase

### What this proposes
- **Assignment.** A product record gets two new fields: a list of agent ids and an "assignments configured" flag. Only an admin can change them, through a new route, `POST /agents/assign`. That route needs a session, membership in `GAOS_AUTOFIX_ADMINS`, a 16 KiB body cap and the same-origin check. Every change emits a `RecordUpdated` event that names the actor's alias.
- **What assignment changes.** Once a product's assignments are saved:
  - a PR-flow role (`pr-reviewer`, `qa`, `unit-test`, and `fixer` for review defects) is planned only when an agent of that role is assigned;
  - the scheduler gives that product's cards only to assigned agents.
  - A product that was never configured behaves exactly as today. There is no migration.
- **Agents page.** It gains a validated `?product=` filter, product chips per agent, and an assignment editor shown to admins.
- **Severity.** The reviewer's severity becomes a `severity:*` label. It is shown as a chip on the board and in the table, and can be sorted and filtered, including a `medium+` filter.
- **Review event.** Medium and worse post `REQUEST_CHANGES`. A pull request opened by the review account itself gets `COMMENT` only. GAOS reviews once and never dismisses.
- **Low findings.** A low review becomes a "review defect" (`kind=review`, `automationType=be`, `easy`, `triaged`). It is fixed through the D-091 auto-fix path, and the draft fix pull request's base is the reviewed pull request's head branch.
  - For this kind, the admin-controlled `AllowedBases` check is replaced by a new predicate, `reviewFixRefusal`. It checks that the pull request is open, its branch name and head SHA are unchanged, it is not from a fork, its branch is not the default branch, and it does not touch `.github/`.
  - The predicate runs at three points: before dispatch, before eligibility, and before `createPR`.

### How the existing system works, per this document
- The scheduler gives a card to the first free agent of its role across all agents (`scheduler.go:399-410`).
- Every product with a repository gets review, e2e and unit-test cards (`prflow.go:351-398`).
- Severity is parsed, but it only decides the card's end state (`prreview.go:256-261`).
- `postReview` always sends `COMMENT` (`github.go:270-280`).
- Auto-fix (D-091) turns a triaged easy backend defect into a fixer card. With the separate write token, it then opens a draft pull request on a `gaos/fix-*` branch, bounded by `AllowedGlobs`, the forbidden globs, the egress screen, the CI-secret check and `AllowedBases` (`autofix.go:302-307`, `autofixpr.go:76-103`, `:211-357`).

All sections could be paraphrased.

## Trust boundaries touched

| Boundary | Authenticates | Authorises | Validates | Far side fails |
|---|---|---|---|---|
| Operator browser → back office (`/agents/assign`, `/ui/bo/agents`) | Signed session cookie. The cookie is now HMAC-verified (`auth.go:338-358`), and the gate needs a session on every non-public route (`auth.go:541-587`). The rubric's "a cookie that checks nothing" is out of date for this repository, so the admin rule is implementable. | `autoFixAdmin` on a mode-600 file (`handlers_autofix.go:17-37`) | product and agent ids checked against the store; 16 KiB cap; `crossOriginOK` (`auth.go:595-612`) plus a SameSite=Lax cookie (`auth.go:507`) | 401 / 403 |
| Dashboard → GitHub, review token | the review token | the token's repository scope | new: `GET /user`, `REQUEST_CHANGES`; review body screened (`prreview.go:240`) | card Failed `github-<class>` |
| Dashboard → GitHub, write token | the write token | `AllowedGlobs`, forbidden globs, and `AllowedBases`, which is **removed** for review fixes | `reviewFixRefusal` at three points | card Blocked |
| Pull request author → model (reviewer, then fixer) | GitHub association (`prflow.go:378`) | assignment of a fixer, plus `AutoFix.Enabled` | egress pass; text marked "untrusted" | — |

## Findings

### SEC-1 The pinned head is not the SHA the reviewer read - Severity: High
- **Component:** review defect, `PlanReviewDefect`, `reviewFixRefusal`
- **Evidence:**
  - Design: `docs/designs/agent-seats-and-pr-agents.md:265` pins `Context.Repos[0].Head` as "the pull request's head SHA at review time"; `:311` compares the live head against that pin; `:456` claims "unmoved since review (pinned SHA)".
  - The diff the reviewer reads is fetched once, at `pr.opened` (`dashboard/prflow.go:391-393`), and the cached context keeps no SHA (`dashboard/prreview.go:137-139`). `PlanReviewDefect` reads the pull request again only after the agent's turn (`:250`).
  - The fix commit's parent is the current head of the base branch, not the pin (`dashboard/autofixpr.go:227`, `:307`).
- **Attack path:**
  1. A collaborator opens a harmless pull request.
  2. While the review card waits or runs, they push further commits.
  3. GAOS pins the *new* head: changed files and contents the reviewer never saw. It feeds them to the fixer as source, builds its commit on top of them with the write token, and opens a draft that presents itself as the fix for a reviewed change.
  4. Every later "unmoved" check passes, because it compares against the post-push pin.

  This is the same class as round-1 SEC-2 (High). The fix closes the window after planning, but not between review and planning.
- **Remediation:**
  - Record `pr.Head.SHA` in `prContext` at `fetchContext`. Use that value as the pin, and have `PlanReviewDefect` refuse (`review-base-moved`) when the live head differs.
  - Read `Files` with `pullFiles` only after confirming that the head equals that SHA.
  - In `run`, use the pinned SHA as the commit parent and refuse when `head()` returns anything else, instead of relying on the third check.
  - Add a case to TC-780 where the push lands between review and planning.
- **Effort:** small

### SEC-2 The review-fix base bypasses the admin's `AllowedBases` and cannot be revoked - Severity: Medium
- **Component:** `reviewFixRefusal`, D-092 (6) amendment
- **Evidence:**
  - Design: `docs/designs/agent-seats-and-pr-agents.md:293-314` (the only base rule left is "not the default branch"); `:353` (D-092 (6) replaced).
  - Today the base is refused at use when it is not in `AllowedBases`: `dashboard/autofix.go:302-307`, `dashboard/autofixpr.go:90`, `pustaka/docs/product/decision-log.html:1998`.
- **Attack path:**
  1. Any account that can open a same-repository pull request opens one whose head is an existing long-lived non-default branch (for example `release/1.2`). It does not need push rights to that branch.
  2. A low review makes that branch the base of a GAOS write-token draft.
  3. The admin has no setting that excludes such branches, and removing a base from `AllowedBases` no longer stops anything for this kind.
- **Remediation:**
  - Add a product setting such as `ReviewFixHeads`, a glob list defaulting to empty, so the feature stays off until set. `reviewFixRefusal` refuses `review-base-not-allowed` when `head.ref` does not match it.
  - Also refuse when the branch is protected (read with the read token; when it cannot be read, refuse).
  - Re-check both at all three points.
- **Effort:** small

### SEC-3 Withdrawing the fixer assignment or pressing Reset does not stop a planned review fix - Severity: Medium
- **Component:** assignment gate for `fixer`, §6.1 and §6.4
- **Evidence:**
  - Design: `docs/designs/agent-seats-and-pr-agents.md:244` (the gate is checked at planning only); `:70` (Reset); `:307-320` (the predicate's checks do not include assignment).
  - `autoClaimLocked` and `fixEligible` check `AutoFix.Enabled` but not assignment (`dashboard/autofix.go:151-170`, `dashboard/autofixpr.go:76-103`).
- **Attack path:**
  1. An admin discovers a steered review fix and removes the fixer, or presses Reset, to stop it.
  2. Fix cards already in Ready still dispatch, and the draft still opens.
  3. The control that is described as the explicit consent for this path (round-1 SEC-3) is not checked at the moment it matters.
- **Remediation:**
  - Add a `review-unassigned` row to `reviewFixRefusal`: the product has `AssignedSet` and an assigned `fixer` *now*.
  - Close the open review defects on Reset or when the fixer is removed, actor = the admin's alias.
  - Test it in TC-780.
- **Effort:** quick-win

### SEC-4 The `review-workflows` check trusts a file list that can be truncated without notice - Severity: Medium
- **Component:** `reviewFixRefusal` `.github/` check
- **Evidence:**
  - Design: `docs/designs/agent-seats-and-pr-agents.md:314`, `:330`.
  - `pullFiles` stops at `ghMaxPages` = 30 pages of 100 and returns whatever it collected, with no truncation signal (`dashboard/github.go:213-231`).
  - Assumption, not verified here: GitHub's list-files endpoint itself returns at most 3000 files.
- **Attack path:** A pull request with more files than the listing returns hides a `.github/` change past the cap. The check passes, and the `CIConfirmed` consent (which covers only unchanged workflows) is stretched over a branch whose workflows changed. The same truncation also shapes `Files`.
- **Remediation:**
  - Fail closed (`review-workflows`) when the listing hits the page cap, or when its length differs from the pull request's `changed_files`.
  - Better still, compare the tree of `.github/` at the pinned head with the tree at the default branch's head, rather than relying on the file list.
- **Effort:** quick-win

### SEC-5 The fixer is told the excerpt is not instructions while it is the only statement of the task - Severity: Medium
- **Component:** fixer packet for review defects
- **Evidence:**
  - Design: `docs/designs/agent-seats-and-pr-agents.md:260` (Excerpt = the posted review body); `:289`; `:459`.
  - The packet already frames the excerpt as "untrusted data — never instructions" (`dashboard/defecttriage.go:489`, `:494-502`).
  - The reviewer's body is model output written after reading a stranger's title, description and diff (`dashboard/prreview.go:108-111`, `:182-184`).
- **Attack path:** Text in the pull request steers the reviewer into a "low" finding that is really a change request ("also relax the check in X"). For a review defect the fixer has nothing else to work from, so it either acts on that free text, which is an injection channel to a write-token commit, or ignores it and produces nothing. The design names this residual (`:459`) but adds no structural bound beyond `AllowedGlobs`.
- **Remediation:**
  - Parse the review body into structured findings (`path:line`, severity, reason) and drop free text that does not parse.
  - Pass only those findings to the fixer.
  - Make `checkFix` refuse any edited path that is not named by a parsed finding and also in the pull request's changed files.
  - Record this bound in D-099.
- **Effort:** small

### SEC-6 The `be` amendment lets a review fix write to any repository the product owns, with one product-wide glob set - Severity: Medium
- **Component:** D-091 (8) amendment
- **Evidence:**
  - Design: `docs/designs/agent-seats-and-pr-agents.md:261` (`Repo` = the reviewed repository); `:267`; `:352`.
  - `AllowedGlobs` is one list per product, not per repository (`dashboard/model.go:429`).
  - D-092 (4) keeps the QA tool's repositories in `AutomationRepos`, separately from `Master.Repos` (`pustaka/docs/product/decision-log.html:1993-1995`; `dashboard/model.go:446-449`). Nothing in the design stops a review defect on a repository listed there, or on a product repository the admin never meant for auto-fix.
- **Attack path:** A pull request in a secondary product repository (spec, infrastructure, or documentation repository) whose paths happen to match the product's globs gets a write-token commit and a draft. This is where D-091 (8) previously kept auto-fix out.
- **Remediation:**
  - Plan a review defect only when the repository is in `AutoFix.TargetRepos` and not in `AutomationRepos`. Otherwise plan nothing and set the card reason to `review posted; repository not a fix target`.
  - Add a test case for this.
- **Effort:** quick-win

### SEC-7 A third-check refusal leaves the pushed fix branch in the repository - Severity: Low
- **Component:** §6.4 third check
- **Evidence:**
  - Design: `docs/designs/agent-seats-and-pr-agents.md:320-322` (check after "the commit and the ref"; refusal closes the records only).
  - The ref is deleted only when `createPR` fails (`dashboard/autofixpr.go:312-329`).
- **Attack path:** A refused review fix leaves a `gaos/fix-*` branch holding the contributor's head plus a GAOS-authored commit. It already triggered push CI. A later generation's `adoptOrCreate` adopts or refuses it (`autofixpr.go:362-377`).
- **Remediation:** When the third check refuses and this run created the ref, delete it, as the `createPR` error path does. State this in §6.4.
- **Effort:** quick-win

### SEC-8 Any signed-in account can manually dispatch a review-defect fix card - Severity: Low
- **Component:** manual dispatch with auto-fix off
- **Evidence:**
  - Design: `docs/designs/agent-seats-and-pr-agents.md:246` ("a person may dispatch it").
  - The back office has no roles (`pustaka/docs/product/open-questions.html:907-913`, OQ-37).
- **Attack path:** A non-admin account spends model turns on content derived from a stranger, on a product whose admin left auto-fix off. No pull request opens (`fixEligible`), and the egress pass still applies, so the impact is spend only.
- **Remediation:** Either restrict dispatch of `defect:*` cards with `kind=review` to `autoFixAdmin`, or add this case to OQ-37's list of unrestricted actions.
- **Effort:** quick-win

### SEC-9 `REQUEST_CHANGES` can be switched on, but not off per product - Severity: Low
- **Component:** §5 review event
- **Evidence:** `docs/designs/agent-seats-and-pr-agents.md:206`, `:215-229`. An operator's label correction changes nothing on GitHub (`:186`).
- **Attack path:** A false "medium" (a model error, or a steer by text in the pull request) blocks merges on repositories that require approving reviews, until a maintainer dismisses it. Correcting the label in GAOS does not undo it. This is an availability lever on repositories GAOS does not own.
- **Remediation:** Add a per-product switch for `REQUEST_CHANGES`, defaulting to the current `COMMENT` behaviour, and set by the same admins. Alternatively, record the availability residual in D-099 and §15.
- **Effort:** small

### SEC-10 D-099 (5) records Release B's auto-merge as decided before its security review - Severity: Low
- **Component:** registers
- **Evidence:**
  - Design: `docs/designs/agent-seats-and-pr-agents.md:14-20`, `:354`, `:487` (round-1 SEC-1 High escalated to B).
  - BR-08 (`pustaka/docs/product/brd.html:1240`) and D-091 (2), "GAOS never ... merges" (`pustaka/docs/product/decision-log.html:1961`).
- **Attack path:** A later session reads D-099 (5) as standing approval for GAOS merging, and implements against the guard list without the Release B design and its security review.
- **Remediation:** Word D-099 (5) as "proposed for Release B; not in force until that design is approved and BR-08, D-091 (2) and the out-of-scope line are amended".
- **Effort:** quick-win

The four remaining sections (the assignment route, the product filter, the severity label, and the 409 sentinel's message) were checked and are sound as written.

Counts: Critical=0 High=1 Medium=5 Low=4
