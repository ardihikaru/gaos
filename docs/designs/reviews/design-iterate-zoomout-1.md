The design fixes the real problem: today a design whose review finds a Blocker stalls its workflow for good. Making the operator send the design back, capped at three times and with no override, is a sound response, and I found no Blockers. Three Majors propose smaller or sturdier mechanisms that the codebase already supports.

# Zoom-out review: design-iterate

Document reviewed: `/home/ardi/devel/projects/ardihikaru/my-projects/gaos/.claude/worktrees/agent-ac38e26c8cafe21d1/docs/designs/design-iterate.md`

## Summary

- **Problem (§1):** solid. I checked every claim against the code and docs: `designgate.go:708`, `:745-746`, `:790-792`, `:860-873`, `taskstate.go:87`, `board.go:171-172`, and `design-gate.html:178-180`. The stall is real and the problem is stated at the right level.
- **Non-goals and the override rejection (N1, A5):** argued well and fit the project's approach.
- **Sessions (§3.5):** the reasoning for giving the reviewer a fresh session is sound and matches what D-069 says in `turns.go:46-50`.
- **Card timing fix (§3.1):** the smallest correct change. The partition sums at `timing.go:248-259` would otherwise lose a `DONE` segment that is later left.
- **Arithmetic (§5):** it adds up.
  - The rule file says 477 register rows and 353 Go tests, 331 of them in `dashboard/`.
  - `feat/conversations` adds 12 (TC-478…489): 489 rows, 365 tests, 343 in `dashboard/`.
  - This design adds 10: 499 rows, 375 tests, 353 in `dashboard/`. All match the document.
  - TC-208's "Fifteen" is right: the illegal list at `taskstate_test.go:37-51` has 15 rows, and removing `{DONE, READY}` at :47 leaves fourteen.
  - The change list has 19 rows covering 35 files. The document states no file-count target, so there is nothing to check that against.

## Findings

### The re-review hook rebuilds what the scheduler already does - Severity: Major
- Affected scope: §3.2 (`ReasonDesignRevising`, `RuleDesignRevised`), §3.3 step 8, §3.6 "On delivery" (`reReviewLocked` and the new hook in `finishRun`), the `design-revising` half of §3.6's reroll case. Change list rows 2 and 3.
- Simplicity rationale: the design invents a new waiting state, "BLOCKED with reason `design-revising`", plus a hook in `finishRun` that wakes the review when the design is delivered again. The task graph already does exactly this for the first delivery:
  - `BLOCKED → PENDING` is already a legal move (`taskstate.go:85`).
  - The scheduler promotes a `PENDING` task once all its dependencies are `DONE` (`scheduler.go:278`, `readyPending` at `runner.go:564-576`).
  - The review depends on the design through its `DEPENDS_ON` link (`store_mem.go:1987`).

  So if the send-back moves the review `BLOCKED → PENDING` (rule `iterate`), the review waits on the re-opened design and the existing scheduler moves it to Ready when the revision is delivered. No new hook, rule or reason is needed. This option is not among the alternatives in §6.
- Recommendation: in `IterateDesign`:
  - move the design `DONE → READY`;
  - move the review `BLOCKED → PENDING`;
  - set the implementation's reason to `design-unreviewed` (this matches what `gateStateLocked` would report for a pending review, `designgate.go:513-514`).

  Then drop `ReasonDesignRevising`, `RuleDesignRevised`, `reReviewLocked` and the `finishRun` change. The reroll guard only needs to cover `design-blocked`, because `legalMove` already refuses backlog to ready (`board.go:173-174`). The concurrency case F1 still holds: a second send-back finds the review `PENDING` and is refused at step 3. `designRevisedSinceLocked` is still needed for the fresh session. Confirm in one line that `readyPending` also runs under `GAOS_RUNNER=manual` and `off`. §9 step 4 already relies on that.

### Per-finding structured triage is more machinery than the problem needs - Severity: Major
- Affected scope: G2, §3.2 (`FindingTriage`, `EventPayload.Triage`), §3.3 step 7, §3.4 (`reviewFindingsOf`, `renderTriage`), §3.8 (the per-finding `d<N>` / `n<N>` form, the `N = 1..100` range), TC-490's round-trip part, F8, R2.
- Simplicity rationale: G2 calls the triage optional, yet it is the main source of new code:
  - a new payload type;
  - a parser that reads back markdown the gate itself rendered (`designgate.go:608-610`), only so findings can be addressed by position;
  - a seven-part validation step;
  - a triage renderer;
  - a form with one row per finding;
  - most of the security surface (R2).

  The designer already receives the full findings in `findings.md`. And under `docs/rules/gaos-design-review.md`, step 5, triage is the author's job, not the operator's. Writing one disposition per finding is what the revising designer should do itself. The one thing A5(c) needs from the operator is a way to say "CONTEST: this is fine", and a free-text note covers that.
- Recommendation: replace the `Triage []FindingTriage` payload field with a single optional `Note` string. Keep the same limits (300 runes, one line, no reserved code-fence tag, egress-screened). Render it under the review body in `findings.md`, and tell the designer to triage each finding with the four dispositions in its revised document. This removes `FindingTriage`, `reviewFindingsOf`, `renderTriage`, most of step 7, the per-row form and F8. The sheet becomes a confirmation with one text field. If production later shows that per-finding triage is needed, add it then.

### The revision packet leaves out the design being revised - Severity: Major
- Affected scope: §3.4 (the design-task `findings.md` block), G3, §3.5 "The designer".
- Simplicity rationale: the revision only sees its own previous document if the session resumes. §3.5 admits a session rotated for age (`turns.go:139-141`), a lost session (`turns.go:118-120`) or a change of vendor (`turns.go:123-124`) opens a fresh one. In those cases the designer gets findings about a document it cannot see.

  The code to include the design already exists. The implementation packet adds the design artifact at `store_mem.go:1536-1541`, using `designArtifactFor` (`designgate.go:564-566`). The design's own newest artifact is one call away.
- Recommendation: in the same design-only block, also add `.gaos/packet/design.md` (kind `KindDesign`) with `designArtifactFor(taskID)`, reusing the implementer's code. Add the check to TC-491. G3 then becomes "same session where possible", and losing the session stops being a quiet loss of quality.

### Two guards where one has all the information - Severity: Nit
- Affected scope: §3.1 items 1 and 2; change list rows 1 and 2.
- Simplicity rationale: §3.1 says `moveTask` knows the task and `moveTaskLocked` knows the rule. But `moveTaskLocked` holds the task too (`t` at `runner.go:432`), so it can check kind and rule together. The only other caller that reaches `moveTask` is `canMoveTask` from the board (`board.go:297`), and `legalMove` has already refused any move out of Done before that point (`board.go:171-172`).
- Recommendation: use one guard in `moveTaskLocked`: leaving `DONE` is allowed only when the task is a design and the rule is `iterate`. Either drop the `moveTask` kind check or state that it is purely defensive.

### `lastIterateLocked` and `iterationsLocked` do the same event scan - Severity: Nit
- Affected scope: §3.3 step 6, §3.4, change list row 3.
- Simplicity rationale: both walk `s.events` for `TaskMoved` with rule `iterate` on the same design. One returns the count, the other returns the newest event plus the count.
- Recommendation: use one helper that returns the newest event and the count, called from both places.

### `BoardIterate` carries derived and constant fields - Severity: Nit
- Affected scope: §3.9 `BoardIterate`.
- Simplicity rationale: `Open` is just `N < Cap`, and `Cap` is the same constant on every card.
- Recommendation: keep `Design` and `N`, and either derive `Open` on the client or keep it and drop `Cap`. Minor either way.

### `writeEvent` writes an `actor` column that is never read back, and depends on an unmerged branch - Severity: Nit
- Affected scope: §3.2, last paragraph; change list row 7.
- Simplicity rationale: the boot load reads `seq, type, entityId, ts, payload` and not `actor` (`store_arcade.go:1753-1756`). Since `Actor` is already stored in the payload, binding the column records the same fact twice. The coupling also rests on `conversations.md:160`, which is on another branch and which I could not check from this worktree.
- Recommendation: carry `Actor` in the payload only. Leave the column binding to whichever design actually reads the column, so the two branches do not have to agree on a shared line.

### The "reviewer's handoff" claim is broader than the code - Severity: Nit
- Affected scope: §3.4, last paragraph.
- Simplicity rationale: `latestHandoffFor` returns the newest handoff of any task in the product (`store_mem.go:1286-1302`). That is not necessarily the reviewer's. In a product with several stories it may come from an unrelated task.
- Recommendation: say "the product's newest handoff, which may be the reviewer's", or drop the sentence. Nothing in the design depends on it.

### `BoardCard.Reason` on every blocked card is a separate improvement - Severity: Nit
- Affected scope: G8, §3.9, the `.card-reason` CSS.
- Simplicity rationale: showing the block reason on every blocked and deferred card is useful, but the send-back needs only the `Iterate` decoration.
- Recommendation: keep it if wanted, but present it in G8 as a separate, independent change so it can be judged on its own.

### TC-497's test file is missing from the change list - Severity: Nit
- Affected scope: §5 TC-497 "(`handlers` test)"; §7 row 15.
- Simplicity rationale: row 15 lists the test files, but not the file where the route test lives.
- Recommendation: name the file, for example `design_iterate_test.go`, if that is where it goes.

Counts: B=0 M=3 N=7
