# Zoom-out review: agent seats per product, and the three pull-request agents

**Document:** `/home/ardi/devel/projects/ardihikaru/my-projects/gaos-agent-seats/docs/designs/agent-seats-and-pr-agents.md` (Draft rev 1)

**Paraphrase.** Today the pull-request flows run for every product that owns a repository. The design makes them opt-in: an admin "seats" agents in a product, and a flow runs only when a seated agent holds its role. The reviewer's severity becomes a filterable label. Medium or worse sends GitHub `REQUEST_CHANGES`. A low finding becomes a "review defect" that the existing auto-fix pipeline turns into a draft fix pull request against the reviewed branch. The e2e builder gets two kinds (backend-only and full-stack), chosen per repository. On a staging deploy, GAOS creates missing automation, merges the QA tool's spec pull request, and then runs. A failed unit-test run plans a fix card that stays blocked until a person releases it.

**What holds up:**
- Severity as a label family (§4.1) reuses the existing label machinery well; I checked `prflow.go:404-467`.
- Storing seats on the Product record (§3.1) is sound, and the rejected alternative is stated fairly.
- The §13 label count is right: `labels.go:45-56` has 10 entries, plus 5 + 2 + 1 gives **18**.

---

### §6 review fixes can never open a pull request: `fixEligible` refuses them - Severity: Blocker
- Affected scope: §6.2 table, §6.3 condition 4, §10 row 11
- Simplicity rationale: the design reuses the auto-fix pipeline, but only exempts the dispatch-time base check (`autofix.go:302-307`). The pull-request gate is a separate check, `fixEligible` (`dashboard/autofixpr.go:76-103`), and it runs again after the fixer's turn (`autofixpr.go:161`). Two of its rules refuse every review defect:
  - `case d.AutomationType != "be"` (`autofixpr.go:82`). The §6.2 table never sets `AutomationType`, and the column is mandatory with `regexp 'be|e2e'` (`dashboard/schema/17-defects.sql:18`).
  - `case d.BaseOverride != "" && !containsFold(af.AllowedBases, d.BaseOverride)` (`autofixpr.go:90`). §6.3 condition 4 requires the base to be *outside* `AllowedBases`, so this rule always fires.

  The result is that every low review ends in Review with no pull request. D-091 (8) also records "backend only" as a decision (`pustaka/docs/product/decision-log.html:1972-1973`), and §9 does not amend it. That matters for the worked example's frontend repository, `acme/route-web`.
- Recommendation: put the review-defect exemption in `fixEligible` itself, as one named branch for `Kind == "review"`, next to the `autoFixPass` exemption. State the `AutomationType` value a review defect carries. Add an amendment of D-091 (8) to §9. Extend `TestReviewFixBaseChecks` to run through `afterRun`, not only through dispatch.

### §7.3 spec merge: the default path stalls every staging run, and the card has no way out - Severity: Blocker
- Affected scope: §7.2 step 3, §7.3 steps 3-4 and the last paragraph, §9
- Simplicity rationale:
  - **The "off" setting is not today's behaviour.** Today a successful `create` job ends Done (`dashboard/qajobs.go:334-336`). With `AutoMergeSpecs` off (the default), §7.3 sends every successful create card to Needs you (`AWAITING_INPUT`). The §7.2 rule then holds the `e2e:run` card until that card is final, so no staging run happens until someone cancels a card. "Today's behaviour plus a card that says so" is not what the steps produce.
  - **The card has no exit.** BR-44 allows the QA runner to use `AWAITING_INPUT` only "while the QA tool waits for a person". It may leave only by the job resuming, failing, or a cancel (`pustaka/docs/product/brd.html:1304`). The QA job here is already final, so none of those exits applies. If a person merges the pull request by hand, nothing moves the card.
  - **The state does not survive a restart.** The runner keeps its state in memory. On a restart, a RUNNING card that names a job is re-tracked (`qajobs.go:279-281`). The next poll sees the final job and marks the card Done (`qajobs.go:336`), skipping the merge or its refusal. The 30-minute wait has no persisted state.
  - **Recorded decisions are not amended.** §9 amends neither D-091 (2), "GAOS never marks a pull request ready, approves or merges it" (`decision-log.html:1961-1962`), nor BR-44.

  §8.2, by contrast, refuses to widen BR-44 on purpose, so the document is inconsistent with itself.
- Recommendation: when merging is off, or the merge is refused, end the create card Done or Failed with a `merge-<class>` reason and keep the Needs-you column out of it. Let the run proceed on any final dependency, which §7.2 already allows. If a waiting state is wanted, persist it on the card's reason (the `qa-job:` prefix pattern, `qajobs.go:257-272`) and name an exit for a hand merge. Add the D-091 (2) amendment to §9, and BR-44 too if Needs you stays.

### The seat gate's early return skips the merge-time cancellation and the Pull requests row - Severity: Major
- Affected scope: §3.4 "When every spec is dropped the call returns `ErrPRSkipped`", §6.3 last paragraph
- Simplicity rationale: on `pr.merged`, `prCardsFor` returns only the unit-test spec (`prflow.go:86-89`). A product with a seated reviewer but no seated unit-test agent would drop every spec and return before two things run:
  - the cancel loop for unstarted review and e2e cards (`prflow.go:239-249`), which §6.3 also extends to the review-fix card;
  - `reviewRowLocked` (`prflow.go:253`).

  So a merged pull request leaves its unstarted reviewer card and review-fix card alive, and the fixer can work on a branch that has already merged.
- Recommendation: filter the specs into `fresh` and return `unseated` only after the cancel loop and the row write. The call should return `ErrPRSkipped` only when nothing was planned *and* nothing was cancelled.

### `REQUEST_CHANGES` has no re-review or dismissal, and "re-opens on a new push" cannot happen - Severity: Major
- Affected scope: §5 table, §6.2 `Key` row
- Simplicity rationale: GAOS acts only on `opened` and `merged` (`prflow.go:352-353`; routing table `prflow.go:80-95`). It never sees a new push, so:
  - a "changes requested" review from GAOS's account stays on the pull request after the author fixes it, and nothing re-reviews or dismisses it;
  - §6.2's "a second low review of the same pull request (a new push) re-opens it" has no event that triggers it.

  This is the most visible GitHub-side effect in the design, and its lifecycle is not stated.
- Recommendation: state the lifecycle plainly. Either the operator dismisses or re-requests by hand (a non-goal plus release-note text), or add push handling as an explicit, separate change. Delete the "re-opens on a new push" clause, or name the event that triggers it.

### §7.2 adds a QA-tool route when GAOS already holds the case catalog, and invents a second kind vocabulary - Severity: Major
- Affected scope: §7.1 `RepoKinds` values, §7.2 step 1, §10 row 13
- Simplicity rationale:
  - **The new route duplicates data GAOS already has.** GAOS already syncs the QA catalog. `QaCase` has `Level` (`API|GQL|WEB|E2E`), `Decision` and `Implemented` (`dashboard/qacatalog.go:81-118`), and a store read already exists (`qacatalog_store.go:60`). "Planned cases with no automation" is `Decision == planned && !Implemented`, and the level already separates API from browser cases. The new route `GET /api/v1/gaos/cases?...&automated=false` copies that and adds a cross-repository dependency that blocks phase 5.
  - **The kind vocabulary is new.** GAOS's existing automation-type vocabulary is `be|e2e` (`schema/17-defects.sql:18`, `defects.go:206`). The design introduces `be|fullstack` instead.
  - **Create cards would block on arrival.** `caseCodeFor` reads the case code for a `create` card from a pull request's `QA-case:` line (`qajobs.go:232-255`). A staging create card has no pull request, so each one would be blocked at `qajobs.go:286`. Neither the design nor the change list mentions this.
  - **BR-54 is not addressed.** BR-54 says QA data never adds cards beyond the epic card (`brd.html:1349`). Planning cards from the case list needs an amendment either way.
- Recommendation:
  - Read uncovered cases from the local catalog instead of a new QA-tool route.
  - Reuse `be|e2e` as the kind values.
  - Carry the case code in the create card itself and teach `caseCodeFor` to read it. Add that change to §10.
  - Add a BR-54 amendment to §9.

### Seats only switch roles on; the design's link between a seat and the agent that runs is false - Severity: Major
- Affected scope: §3.4 "the scheduler seats the first agent of that role (`agentForStage`)", §12 last bullet, §3.3 editor
- Simplicity rationale:
  - **The premise is wrong.** `agentForStage` picks by the desk's *stage* (`stageRole`, `fixtures.go:80-106`), not by the card's role (`store_mem.go:1073-1076`). A pull-request reviewer card can show "Working on" for a backend agent while the seated reviewer never appears there.
  - **A seat is a role switch.** Each gated role has one fixture agent, except `qa` (`fixtures.go:35-49`), so seating an agent is just turning its role on.
  - **The editor offers seats that do nothing.** It lists all 13 agents, yet §12 says seats for other roles are read by nothing.
  - **The `fixer` seat is inconsistent.** It gates only review defects; the D-091 defect fixes ignore it, and `triager` is not mentioned at all. That contradicts the goal statement, "Seating is what turns a flow on".
- Recommendation:
  - Fix the §3.4 premise.
  - Limit the editor to the gated roles (`pr-reviewer`, `qa`, `unit-test`, `fixer`).
  - State in one sentence that a seat is a per-role switch and does not choose who runs.
  - Either gate the D-091 fixer or triager flow on the same seats, or say explicitly that it is exempt.

### The upgrade seating still switches flows off silently for products that gain repositories later - Severity: Major
- Affected scope: §3.4 "Upgrade"
- Simplicity rationale: a product with no repositories at upgrade time is marked `seatsMigrated: true` with no seats. So is every product created afterwards, by default. When repositories are added later, every PR flow and staging callback is skipped with `unseated` (409). Nothing tells the operator, which is the very silent switch-off the upgrade step exists to prevent. The permanent `seatsMigrated` flag only exists because of this one-time step.
- Recommendation: decide the default once and say it. Either a product with repositories and no seats is a visible state on the Agents page and the Auto-fix form ("no flows run: seat agents"), or seating happens when `Repos` first becomes non-empty. Drop `seatsMigrated` if the second option makes it unnecessary.

### `RepoKinds` sits on `Master` but is edited through the Auto-fix form; the change list and the cap citation are off - Severity: Nit
- Affected scope: §7.1, §3.2 "capped at 16 KiB … as `handlers_autofix.go:120`", §10
- Simplicity rationale:
  - `SetAutoFix` writes the `AutoFix` struct (`handlers_autofix.go:125-137`). A `Master` field saved from that same form needs a second write path. `AutomationRepos` already lives in `AutoFix` (D-092).
  - The cited cap is `64<<10` (64 KiB), not 16 KiB (`handlers_autofix.go:120`).
  - `handlers_autofix.go` (`autoFixFromForm`) is missing from §10.
- Recommendation: make it `AutoFix.RepoKinds`, which leaves 2 new Product properties, not 3. Correct the citation, and add `handlers_autofix.go` to §10.

### A new pager helper where the code already uses an inline closure - Severity: Nit
- Affected scope: §3.3 "Paging keeps the filter"
- Simplicity rationale: the filtered lists already page with an inline closure over their filter (`prview.go:342-346`, `board_view.go:142`). Adding `listPageAtQ` to `pager.go` creates a second pattern for one caller.
- Recommendation: use an inline closure in the `agents` case.

### The fix-card dedup key is misquoted - Severity: Nit
- Affected scope: §6.2 "dedup key `defect:<key>:fix`"
- Simplicity rationale: the real key is `defect:<key>:<gen>:fix` (`defecttriage.go:44-46`), and the generation is what §6.2's re-open relies on.
- Recommendation: quote `defectCardKey(key, gen, "fix")`.

### Template helper names in §4.2, and a second select that duplicates the Label select - Severity: Nit
- Affected scope: §4.2 Kanban and Filter bullets
- Simplicity rationale: helper names (`sevLabel`, `otherLabels` in `view.go`) are below design altitude. The new Severity select differs from the existing Label select (`board_view.go:95`) only by `medium+`.
- Recommendation: describe the rendering at component level. Consider a single `medium+` option or toggle added to the existing Label filter instead of a new query key and select.

### §8.2 says only an agent can put a card in Needs you, which is not true - Severity: Nit
- Affected scope: §8.2 "only an agent's question may put a card there (`board.go:149`…)"
- Simplicity rationale: BR-44 already makes an exception for the QA runner (`brd.html:1304`; `qajobs.go:353`). `legalMove` (`board.go:214-215`) restricts only what an operator may drop there.
- Recommendation: correct the sentence. It bears on the §7.3 Blocker above.

---

### Arithmetic
- **Labels:** 10 + 8 = **18**. Correct.
- **New DDL statements:** 1. Correct, provided review defects use an existing `automationType` value.
- **New Product properties:** 3 as written; **2** if `RepoKinds` moves into `AutoFix`.
- **Change list:** 20 rows, but at least three changes are missing:
  - `handlers_autofix.go` (the form fields);
  - `caseCodeFor` in `qajobs.go` (named under row 13's file, but not as a change);
  - `fixEligible` in `autofixpr.go` (row 11 names the file, not the function).

Counts: B=2 M=5 N=5
