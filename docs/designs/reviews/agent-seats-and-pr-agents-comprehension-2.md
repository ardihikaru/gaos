# Goldfish comprehension review: `docs/designs/agent-seats-and-pr-agents.md` (rev 2)

## Paraphrase

### What this proposes

This is Release A. Release B (QA-tool e2e kinds, staging automation, auto-merge of spec pull requests) is only recorded as decisions in D-099.

1. **Assignment.**
   - `Master` gains `Assigned` (agent ids) and `AssignedSet` (true once an admin has saved). Both are stored as schema-less Product properties.
   - A new store method `SetAssigned` emits `RecordUpdated` with the actor's alias.
   - A new admin-only route `POST /agents/assign` takes `save` or `reset`. Admin means listed in `GAOS_AUTOFIX_ADMINS`.
   - A product that was never configured behaves as today. Nothing migrates.
2. **Gate.**
   - For a configured product, a PR-flow card (`pr-reviewer`, `qa`, `unit-test`, and `fixer` for review defects) is planned only if an assigned agent holds that role.
   - The gate sits in `PlanPullRequestCards` after the merge-cancel loop and the row write, and before the cap check.
   - If the gate alone empties the plan, the call returns `ErrPRUnassigned`, and staging answers 409 `unassigned`.
   - A merge of GAOS's own fix pull request plans nothing.
3. **Scheduler.**
   - `freeAgent` only considers a product's assigned agents of the role, when at least one exists. Otherwise it falls back to every agent of the role.
   - `liveAgentSlots` uses the same rule, and the Agents page's "Working on" column reads from it.
4. **Agents page.** A product filter, product chips per agent, and an admin editor with Save and Reset.
5. **Severity.**
   - A new `severity:*` label family (five labels), set by the reviewer right after parsing.
   - A coloured chip on the kanban, Severity and Labels table columns, a severity sort, and a Severity filter that includes `medium+`.
6. **GitHub event.**
   - Medium and worse send `REQUEST_CHANGES`; everything else sends `COMMENT`.
   - If the review account's own login (`GET /user`) is the pull request's author, GAOS sends `COMMENT`.
   - GAOS reviews once and never dismisses. Bodies sent with `REQUEST_CHANGES` end with a line telling a maintainer to dismiss the review.
7. **Review defect.**
   - A low review on a configured product that has an assigned fixer becomes a `Defect{Kind: review}`. It is born triaged and easy, carries `be`, and has `BaseOverride` set to the pull request's head branch, with the head SHA pinned.
   - Its source slice is the pull request's changed files, read at the pinned SHA.
   - It is fixed through the D-091 auto-fix path.
   - The predicate `reviewFixRefusal` replaces the `AllowedBases` check. It runs at dispatch, before eligibility, and before `createPR`, and refuses on: pull request closed, branch changed, head moved, fork, default branch, or `.github/` changes.
   - The draft pull request targets the reviewed branch, and GAOS comments a link on the reviewed pull request.
8. **Registers and delivery.** D-099, BR-59, data-model updates, TC-767…782, a docs page, and v0.61.0, delivered in three phases.

### How the existing system works, per this document

- The Agents page lists the 13 fixture agents with "seated · desk", and has no product filter.
- The scheduler hands a card to the first free agent of the role, across all products.
- Every product that owns a repository gets review, e2e and unit-test cards.
- Severity is parsed, but it only decides the card's end state. High and critical stay in Review as needs-manual.
- `postReview` always sends `COMMENT`.
- The D-091 pipeline turns triaged easy defects into fix cards. Those cards are dispatched under caps when `AutoFix.Enabled` is on, and produce a draft pull request after `fixEligible`, `checkFix`, the CI-secret check and the egress check.
- Labels are a closed vocabulary of 10, one per family, persisted, replayed, evented, rendered and filterable.

Most `file:line` anchors I checked are accurate (prflow.go, prreview.go, labels.go, scheduler.go, autofix.go, autofixpr.go, defects.go, github.go, staging.go, handlers_autofix.go, handlers_bo.go, store_arcade.go, model.go, brd.html). §4 (severity) and §3.3 (route and store method) are clear and solid.

## Findings

### Fix-card generation 0 conflicts with the Defect schema and with fixEligible - Severity: Blocker
- Affected scope: §6.2, "The fix card" (`defectCardKey(key, 0, "fix")`, i.e. `defect:<key>:0:fix`) and the field table, which has no `Gen` row.
- Finding:
  - `schema/17-defects.sql:29` declares `Defect.gen INTEGER (mandatory true, min 1)`. Every new defect is created with `Gen: 1` (`defects.go:522`). The existing tests key first-generation cards with gen 1 (`defecttriage_test.go:389`, `fixpr_test.go:93`).
  - If the record stores `Gen: 0`, ArcadeDB refuses it. In-memory tests would pass, which is exactly the v0.52.0 lesson §11 cites.
  - If the record stores `Gen: 1`, `fixEligible` refuses every review fix with "the card is of an older generation" (`autofixpr.go:86`), because the card key says 0.
  - Either way the path in §6 cannot work as written, and the table leaves `Gen` undecided.
- Recommendation: Set `Gen = 1` explicitly in the §6.2 table and use `defectCardKey(key, 1, "fix")`. Add an ArcadeDB assertion on `gen` to TC-778.

### The Defect field table omits fields that are required to persist or to pass the gates - Severity: Major
- Affected scope: §6.2, the `Defect` field table.
- Finding:
  - `ProductID`, `FirstSeen` and `LastSeen` are missing. All three are mandatory columns (`schema/17-defects.sql:13,26-27`).
  - `FixTask` is missing. `autoClaimLocked` requires `d.FixTask == t.ID` (`autofix.go:164`), `fixEligible` requires it too (`autofixpr.go:78`), and `fixerPartsLocked` finds the record through it (`autofix.go:341`). The triage path sets it at `defecttriage.go:117`.
  - `Regression` (must be false), `Level`, `Backend`, `Spec` and `TriageTask` (must stay empty) are not stated.
  - "Planned as the triage path plans one" does not say which of those assignments carry over.
- Recommendation: List every `Defect` field with its value, or say "zero" explicitly, and name `FixTask = <the planned card id>`.

### The scheduler's assigned-agent rule never reaches defect cards; §6.2 says the scheduler moves them - Severity: Major
- Affected scope: §3.5, §6.2 ("born PENDING, and moved by the scheduler"), §16 bullet 4.
- Finding:
  - `defect:*` cards are never dispatched by the scheduler, in any mode (`labels.go:33-34`, `labelsAllowDispatch` at `labels.go:110-118`, D-091 (4) at `decision-log.html:1968`).
  - `readyDefectCards` moves them from PENDING to READY with no agent check (`defecttriage.go:793-806`). `autoFixPass` then dispatches them through `AutoDispatch`, which runs `MemStore.dispatch` (`board.go:474`). §3.5 itself says that route "names no agent".
  - So `freeAgent`'s new rule never applies to a review fix card, or to any D-091 fixer card.
  - The "assigned fixer" therefore only gates planning. §16's "a product's cards go only to its assigned agents of the role" is false for fixer cards.
  - The only effect on those cards is that `liveAgentSlots` attributes them to an assigned fixer for display.
- Recommendation: Correct §6.2: `readyDefectCards` moves the card and `autoFixPass` dispatches it. In §3.5, state that the assignment rule binds the scheduler's `freeAgent` only, and that for defect cards assignment is a planning gate plus display attribution. Narrow §16 and BR-59 to match.

### BR-59 and the Goal say "only assigned agents", but §3.5 falls back to every agent - Severity: Major
- Affected scope: §1 Goal, §3.5, §3.6 item 2, §9 BR-59.
- Finding:
  - §3.5 falls back to every agent of a role when the product has no assigned agent of that role. This happens for architect, planner, triager, and for gated roles on cards planned before assignment.
  - BR-59 says the product "gives its cards only to assigned agents", and so does the Goal. The rule being written into the BRD contradicts the design.
  - §3.6 lists "only its assigned agents" for a configured product, so the page hides the fallback agents that will actually take that product's other roles.
- Recommendation: Reword BR-59 and the Goal, for example: "…gives a card of a role to assigned agents of that role when any is assigned; otherwise to any agent of the role." Say on the Agents page that unassigned roles fall back to every agent.

### reviewFixRefusal cannot evaluate two of its own checks from its signature - Severity: Major
- Affected scope: §6.4, the predicate signature and the check table; §6.4 point 1 ("gains a `pullRequest` reader").
- Finding:
  - `reviewFixRefusal(d Defect, pr ghPR, defaultBranch string)` has no list of changed files. `ghPR` carries none (`github.go:43-68`).
  - `d.Files` cannot stand in for it: it is filtered by `contextPathOK` (`defecttriage.go:262-272`, which applies `pathForbidden`) and capped at 6. A `.github/` change could be dropped from it.
  - The `review-workflows` check therefore needs a fresh `pullFiles` read at each of the three points, and only a `pullRequest` reader is added to `autoFixPass`.
  - `defaultBranch` has no stated source. It could be the repository's default branch (an extra GitHub call) or `AutoFix.BaseBranch`. These give different results.
- Recommendation: Add a `files []string` parameter (or `[]ghFile`) and a `pullFiles` read at all three points. Name the source of `defaultBranch` and the call that fetches it. List both in §10 rows 8 and 17.

### The "pinned head" is taken after the review, not from the diff the reviewer read - Severity: Major
- Affected scope: §6.2 (`Context.Repos[0].Head` "at review time"), §6.2 first paragraph (afterRun reads with `pullRequest`), §15.
- Finding:
  - The reviewer's diff is captured at `pr.opened` by `fetchContext` (`prflow.go:582-590`), which stores no SHA.
  - `afterRun` reads the pull request again after posting. If the author pushed in between, the pinned SHA, the source slice and the fix are all based on code the reviewer never saw, and every later check passes.
  - This weakens the SEC-2 pin that §15 relies on.
- Recommendation: Store `pr.Head.SHA` in `prContext` at `fetchContext`. Plan the review defect only if the fresh read still matches it; otherwise plan nothing, with a logged reason. State what happens on a cache miss.

### The own-pull-request rule depends on an in-memory cache and a start-up call with no failure behaviour - Severity: Major
- Affected scope: §5, "The own-pull-request rule".
- Finding:
  - The author login is said to live in the review-context cache. That cache is bounded to 64 entries and lost on restart (`prreview.go:141-150`).
  - On a miss, the comparison cannot be made, and nothing says whether GAOS sends `COMMENT` or `REQUEST_CHANGES`. The wrong choice means a 422, and the card ends Failed `github-<class>`.
  - `GET /user` failing at start is also undefined: retry, disable `REQUEST_CHANGES`, or refuse to start.
  - The same rule is labelled an assumption about GitHub's behaviour, which is acceptable, but the fallback must be deterministic.
- Recommendation: Read `user.login` from the fresh `pullRequest` call `afterRun` already gains. Say that if either login is unknown, GAOS sends `COMMENT`, with the reason text `… (author unknown: comment only)`.

### A refusal at the third check leaves the pushed fix branch behind - Severity: Major
- Affected scope: §6.4, third check point and "Any refusal blocks the card…".
- Finding:
  - The third check runs after the commit and the ref, so `gaos/fix-*` already exists on GitHub (`adoptOrCreate`, `autofixpr.go:312`).
  - The design closes the `FixPR` record but says nothing about deleting the branch. The existing `createPR` error path does delete a branch it created (`autofixpr.go:322-327`).
  - A branch left behind keeps running CI on a base that includes the pull request's own workflows. `review-workflows` exists to stop exactly that.
- Recommendation: Say that a refusal at point 3 deletes the branch when `created` is true, as the existing error path does, and add that to TC-780.

### D-088's "posts the review as a comment" is amended but not listed - Severity: Major
- Affected scope: §9, D-099 (3) and (4).
- Finding:
  - D-088 decides that "GAOS (never the agent) posts the screened review as a comment" (`decision-log.html:1866`). The code comment at `github.go:270-271` relies on it.
  - D-099 lists amendments to D-091 (8) and D-092 (6) only. Changing to `REQUEST_CHANGES` is an amendment of D-088 and is not recorded as one.
- Recommendation: Add "amends D-088: medium and worse post `REQUEST_CHANGES`" to D-099. Update the `postReview` comment in change row 14.

### Gate placement versus the early return, and whether the row survives ErrPRUnassigned - Severity: Major
- Affected scope: §3.4, "Where the gate sits".
- Finding:
  - The early return `len(fresh)==0 && len(events)==0` (`prflow.go:254-256`) sits between `reviewRowLocked` (:253) and the cap check (:257).
  - If the gate runs after line 254, an empty `fresh` reaches planning. If it runs before, the sentinel must replace that return.
  - Existing error returns hand back `Result{}` (`:258`), so for a `pr.opened` with both roles off it is unclear whether the Pull requests row is persisted. §3.4 promises the row only for the merge case.
- Recommendation: State that the gate sits between :253 and :254. State that `ErrPRUnassigned` is returned with `Result{PullRequests: rows}` and that callers persist that result, or state that no row is written. Cover the chosen behaviour in TC-772.

### Reusing BaseOverride makes a review defect look like a retarget - Severity: Major
- Affected scope: §6.2 (`BaseOverride` row), §6.4.
- Finding:
  - `BaseOverride` is documented as "the base a retarget set" (`defects.go:102-105`), and several readers treat a non-empty value as a retarget:
    - the Pull requests page shows it as an Override (`prview.go:461`, `:529`);
    - `buildAndStoreDefectContext` blocks the fix card rather than the triage card (`defecttriage.go:433-443`);
    - `retarget.go:84`.
  - An operator can retarget a review defect (`retarget.go:119`). That sets `BaseOverride` to an allowed base, and the defect is then refused as `review-base-changed`.
  - None of these interactions is addressed.
- Recommendation: Either refuse retargeting for `Kind == review` and state how the Pull requests page shows the override, or use a separate field (for example `ReviewBranch`). List each `BaseOverride` reader with its review-kind behaviour.

### Operators can change severity, but the UI path to do so is removed - Severity: Nit
- Affected scope: §4.1 last paragraph against §4.2 Filter, third bullet.
- Finding: §4.1 says an operator may correct the severity "through the existing label route". §4.2 removes `severity:*` from the label select and offers no other way to set it.
- Recommendation: Name the control that sets it, for example the card drawer's label menu, or say that correction is route-only.

### Where the GAOS fix-PR merge check runs is not stated - Severity: Nit
- Affected scope: §3.4, last paragraph.
- Finding: It does not say whether the `FixPRs` lookup runs in `prFlow.handle` or inside the locked `PlanPullRequestCards`, where calling a store method would re-lock. It also does not say whether it comes before or after the cancel loop.
- Recommendation: Name the function and its position.

### The cited code meaning of "seat" is mis-paraphrased - Severity: Nit
- Affected scope: Header, "Terms".
- Finding: The document says "in the code a seat is an agent on a desk for a run". The cited comment says the opposite: "a seat is the floor's display, not a run" (`store_mem.go:2395-2396`).
- Recommendation: Quote the comment as written.

### The reviewer's packet wiring lacks the codeSource that §6.3 needs - Severity: Nit
- Affected scope: §6.2 first paragraph, §6.3, §10 row 8.
- Finding: `afterRun` is given a reader with `pullRequest` and `pullFiles` only. §6.3 reads files with `fileAt` and runs `buildDefectContext`, which takes a `codeSource`. That injection is not listed.
- Recommendation: Add `codeSource` to the `prReviewer` wiring in §10 row 8.

### Assignment rides an event that the fold ignores - Severity: Nit
- Affected scope: §3.3.
- Finding: `RecordUpdated` is on the fold's no-op list (`fold.go:442`), so assignment survives only through the Product document. The document does not say what a MemStore-only deployment or a replay shows.
- Recommendation: State in one line that assignment is not rebuilt from events and that a MemStore-only run loses it on restart (or that no such deployment exists).

Counts: B=1 M=10 N=5
