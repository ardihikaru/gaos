# Zoom-out review: Agent assignment per product, PR-review severity, and low findings fixed by an agent (rev 2)

Reviewed: `/home/ardi/devel/projects/ardihikaru/my-projects/gaos-agent-seats/docs/designs/agent-seats-and-pr-agents.md`

**Summary.** No blockers. The scope split and most of the reuse claims check out against the code. The main problem is §3.5: the agent-level scheduler rule changes nothing about what runs, and it can block work that a free agent could have taken. Four other Majors cover undecided wiring and one break of the "unconfigured behaves as today" promise.

**Solid, briefly:**
- **§0.** Splitting into Release A and Release B keeps this design to one concern set.
- **§3.1.** Keeping the list on the product record matches where the planner and scheduler read it.
- **§3.4 gate placement.** It holds against `prflow.go:238-258`: the cancel loop and `reviewRowLocked` run before the early return and the cap check.
- **§4.1.** Severity as a label family is the right reuse. `SetTaskLabel` (`prflow.go:424-468`) and `normalizeLabels` (`labels.go:89`) already give one label per family, persistence and an event.
- **Arithmetic I checked:**
  - Labels: 10 today (`labels.go:45-56`) + 5 = 15. Correct.
  - Tests: TC-767 to TC-782 is 16. Correct, and TC-766 is the last one allocated (`qa-test-plan.html:857`).
  - Register ids are free: D-098 is the last decision (`decision-log.html:2118`), BR-58 the last business rule, and v0.60 the last changelog entry.

---

### The assigned-agent scheduler rule (§3.5) changes nothing that runs, and can stall work - Severity: Major
- Affected scope: §3.5, §16 bullet 4, §10 rows 3 and 5, TC-774
- Simplicity rationale:
  - **The chosen agent is never read.** `Dispatch.AgentID` is written at `scheduler.go:369` and read only by tests (`scheduler_test.go:354`, `workflow_dispatch_test.go:47`). The model comes from the role (`roles[spec.Role].Model`, `prflow.go:274`). So "Kite works on Route Planner" changes no prompt, model or workspace. It only changes which name is counted busy.
  - **Most gated work never passes through `freeAgent`:**
    - The scheduler never dispatches a `defect:*` card or an `e2e:*` card (`labels.go:110-119`, D-091 (4) at `decision-log.html:1968`).
    - Defect cards go through `AutoDispatch` → `dispatch` (`board.go:474`, `board.go:528`), which names no agent.
    - So assigning a fixer, or a qa agent for e2e work, affects only the gate (§3.4), never "who takes the work".
  - **Where it does apply, it is mostly a no-op.** It applies to pr-reviewer, unit-test and ordinary workflow cards. pr-reviewer, unit-test and fixer each have exactly one fixture agent (`fixtures.go:45-48`). Only backend (a-2, a-3) and qa (a-5, a-6) have a choice, and BR-38 already allows only one live turn per product (`board.go:445-450`).
  - **It can stall work.** `liveAgentSlots` (`store_mem.go:2406-2436`) guesses which agent holds each live task. Suppose an unconfigured product's live qa task is attributed to a-5, and a-5 is assigned to `p-9`. Then `p-9`'s card is blocked with "every assigned qa agent is busy" while a-6 is free. This is the v0.40.1 failure the comment at `store_mem.go:2395-2401` describes: busy-ness read from something that is not the run.
  - §16 bullet 4 ("A product's cards go only to its assigned agents") cannot be observed for defect cards at all.
- Recommendation:
  - Keep assignment as the per-product gate (§3.4), which is the part that changes behaviour.
  - Drop the `freeAgent` rule, the `liveAgentSlots` change and the "every assigned <role> agent is busy" reason. Move "assigned agents take the work" to §12 Non-goals until `AgentID` means something.
  - If the operator still wants it (round-1 triage says they chose "real assignment"), the document must state:
    - that `AgentID` is unread;
    - that defect and e2e cards bypass the rule;
    - what stops the false-busy stall above.

### "Working on" depends on a store read that does not exist - Severity: Major
- Affected scope: §3.5 paragraph 4, §3.6, §10 row 6, §13 "New store methods 2"
- Simplicity rationale:
  - `liveAgentSlots` is a private helper called only inside `MemStore.schedulerInputLocked` (`store_mem.go:2402`).
  - The Agents handler reaches the store only through the `Store` interface. That interface has no cross-product task or scheduler read (`store.go:58`, `:127-133`).
  - Getting the live slots there needs a third store method on three stores. That method is not in the change list, and the §13 count does not include it.
  - It also fixes an older mismatch: the display seat versus the run, already noted at `store_mem.go:2395`. That mismatch is not one of the five problems in §1.
- Recommendation: Leave the "seated · <desk>" text as it is (`handlers_bo.go:271-281`) and list the Working-on change as a non-goal. Otherwise, name the new store method, add it to §10, and change §13 to 3 store methods.

### "FixPR merges plan nothing" changes behaviour for unconfigured products - Severity: Major
- Affected scope: §3.4 last paragraph, §10 row 12, TC-782, versus §3.2 and TC-769
- Simplicity rationale:
  - The rule matches any `FixPR` record (`defects.go:619`). That includes every D-091 auto-fix draft merged into the product's base branch.
  - Today, merging such a pull request in an owned repository plans a unit-test card (`prflow.go:86-89`).
  - After this change it plans nothing, for every product, configured or not. That contradicts §3.2 ("behaves exactly as today… no migration") and TC-769.
  - The stated reason, "merging … into a feature branch", covers only review fixes.
- Recommendation: Narrow the rule to fix PRs whose defect has `Kind == "review"`. Or drop it: the review fix merges into a feature branch, and that branch's own merge is the change worth testing.

### The own-pull-request rule stores an author that is already stored, and leaves its failures undefined - Severity: Major
- Affected scope: §5 "The own-pull-request rule", §10 rows 8 and 14, §13 "New outbound GitHub calls 1"
- Simplicity rationale:
  - **The author is already stored, in two places:**
    - in the review context, as `prContext.Author` (set from `pr.User.Login` at `prflow.go:589`);
    - in the persisted Pull requests row (`fixprpoll.go:52`, `store_arcade_pullrequests.go:51`).
  - **The design picks the copy a restart loses.** The cache is in-memory and lost on restart (`prreview.go:141-143`). If GAOS restarts between planning and review, the author is empty, the logins do not match, and `REQUEST_CHANGES` is sent to an own pull request.
  - **Two failure cases are undecided:**
    - what happens if `GET /user` fails at start;
    - what happens if the token type cannot call `GET /user`.
- Recommendation:
  - Compare against the persisted row's `Author`, not the cache.
  - State a fallback: COMMENT whenever either login is unknown.
  - Then "never guess a 422" holds without a new store field.

### Building the review defect's source slice is under-wired and does not use existing helpers - Severity: Major
- Affected scope: §6.2 first paragraph and "The fix card", §6.3, §10 rows 8, 15 and 16
- Simplicity rationale:
  - **The reviewer is given the wrong reader.**
    - Building the context at planning time needs a `codeSource`: `defaultHead`, `searchCode` and `fileAt` (`defecttriage.go:205-211`).
    - The reviewer is injected with only a `prReader` (`pullRequest`, `pullFiles`).
    - Who builds the slice, and with which client, is undecided. A store method cannot do the GitHub reads, because contexts are built outside the lock (`defecttriage.go:293`, and `TestDefectContextBuiltOutsideTheLock`).
  - **Existing helpers already do most of the work:**
    - `buildDefectContext` already pins each target repository's head with `defaultHead(…, baseBranch)` (`defecttriage.go:302-311`). With `BaseOverride` = `head.ref`, as `buildAndStoreDefectContext` already does at `:433-436`, it pins the pull request branch.
    - The only new branch needed is "fetch `d.Files` when `Operations` is empty".
    - `newDefectLocked` (`defects.go:515`), `planDefectCardLocked` (`defecttriage.go:78`) and `SetDefectContext` (`defects.go:604`) already do the record, the card and the store.
  - **"Born PENDING, and moved by the scheduler" is wrong.** `readyDefectCards` moves it (`defecttriage.go:789-807`), and the scheduler never dispatches it.
- Recommendation:
  - Inject a `codeSource` (the review `ghClient` already implements `defaultHead` and `fileAt`, `githubwrite.go:35`, `:83`).
  - Have `afterRun` call `buildDefectContext` before `PlanReviewDefect`.
  - Say that `PlanReviewDefect` composes `newDefectLocked` and `planDefectCardLocked`.
  - Correct the readying sentence.

### The fix path re-checks the pull request at three points where two would do - Severity: Nit
- Affected scope: §6.4 "The predicate runs at three points"
- Simplicity rationale: Points 2 and 3 are in the same `afterRun` → `run` call, seconds apart. `run` already reads the base branch head at `autofixpr.go:227`, so the pinned-SHA comparison can use that `head` value without another read.
- Recommendation: Keep the dispatch check, which saves the model turn, and the pre-`createPR` check. Drop point 2, or say what it buys beyond skipping a commit and a ref write.

### §13 undercounts the GitHub calls - Severity: Nit
- Affected scope: §13 "New outbound GitHub calls 1", §6.4 `review-default-branch`
- Simplicity rationale: `ghPR` has no default-branch field (`github.go:42-68`), so `reviewFixRefusal(…, defaultBranch)` needs a repository read at each check point. `autoFixPass` also gains a `pullRequest` call per ready review-fix card. These are existing endpoints with new callers, and the count says only `GET /user`.
- Recommendation: List the calls per site: `GET /user` ×1 at start, plus a pull-request read and a repository read at each check point.

### `AutomationType = be` makes the record say something false - Severity: Nit
- Affected scope: §6.2 "Why `AutomationType` is `be`"
- Simplicity rationale: The field is used to pass `fixEligible` (`autofixpr.go:82`) and `autoClaimLocked` (`autofix.go:165`), not to describe the defect. Any later report or filter on `automationType` will count review findings as backend QA defects.
- Recommendation: Keep it if the operator accepts it. Add a code comment at both checks saying `kind=review` carries `be` by decision D-099 (4), so the next reader does not "fix" it.

### §4.2 includes table changes nobody asked for - Severity: Nit
- Affected scope: §4.2 Table: `{"", "Labels"}` column; "The Title cell no longer repeats the labels"; the `severity:none` chip on every clean review
- Simplicity rationale: §1 asks for visible, filterable severity. Moving labels into a new column changes every row of the table for an unrelated reason. A "none" chip on every clean card adds noise to the kanban.
- Recommendation: Add only the Severity column, and keep the labels in the Title cell. Consider not showing a chip for `none`; the filter can still select it.

### §3.3 and §3.6 drop to template and string detail - Severity: Nit
- Affected scope: §3.6 items 4–5 (`hx-get`, `hx-trigger`, `<noscript>`, checkbox grouping), §3.3 the three event text strings
- Simplicity rationale: These are markup and copy, not architecture. "Same filter form as the Pull requests page (`templates/backoffice.html:1349-1357`)" is enough.
- Recommendation: Replace the markup and the strings with one line pointing at the existing pattern.

### One release where two would lower the risk - Severity: Nit
- Affected scope: §14 "One release, v0.61.0, after phase 3"
- Simplicity rationale: Phases 1 and 2 do not touch the schema. Phase 3 carries the DDL change, the ArcadeDB lesson (§11 step 3) and every write-token path. Holding phases 1–2 behind phase 3 ties the low-risk features to the riskiest part.
- Recommendation: Allow phases 1–2 to ship as their own release if phase 3 slips. Say so in §14.

Counts: B=0 M=5 N=6
