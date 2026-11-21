# Comprehension review: design-iterate (Goldfish lens)

Document reviewed: `/home/ardi/devel/projects/ardihikaru/my-projects/gaos/.claude/worktrees/agent-ac38e26c8cafe21d1/docs/designs/design-iterate.md`

## Paraphrase

### What this proposes

1. **Let a design task leave `DONE`.** One new edge is added to the task state machine: `DONE → READY`. Two guards keep it narrow. `moveTask` refuses the edge for any task that is not a design task. `moveTaskLocked` refuses any move out of `DONE` whose rule is not `iterate`. Card timing changes too: a `DONE` segment that is later left is counted as "blocked" time, so the timing buckets still add up.
2. **Add new names.** The block reason `design-revising`, the rule `design-revised`, a cap of 3, the board action `MoveIterate = "iterate"`, and two new payload fields on the `TaskMoved` event: `Actor` (who did it) and `Triage` (the operator's response to each finding).
3. **Add a store method, `IterateDesign`.** Given a review that the gate blocked with `design-blocked`, it checks, in order:
   - that the review exists and is a review task;
   - that it is blocked with `design-blocked`;
   - that its design task is `DONE`;
   - that the review artifact's verdict is `iterate`;
   - that the cap has not been reached;
   - that the triage is valid and passes the secret screen.

   It then moves the design `DONE → READY` with rule `iterate`, recording the actor and the triage, and changes the review's block reason to `design-revising`. The implementation task is left as it is.
4. **The design's next packet** gets `findings.md`: the review artifact body plus a rendered triage section (no email address in it).
5. **Sessions.**
   - The designer resumes its own session, with no code change.
   - The re-reviewer gets a fresh session. This is triggered by a new `sessionForLocked` case: the latest design run started after the review's last run.
   - A review retry on the same design still resumes its session.
6. **On delivery of the revised design,** `finishRun` calls `reReviewLocked`. It moves a review that is `BLOCKED/design-revising` to `READY` with rule `design-revised`, and resets the implementation's reason to `design-unreviewed`. A manual "unblock" of a review blocked with `design-blocked` or `design-revising` is refused in `legalMove`, so the review cannot be re-run on an unchanged design.
7. **At most 3 send-backs,** counted from events. After that the only exit is cancelling the work.
8. **UI and docs.**
   - Two back-office routes: a `GET` that renders the triage sheet and a `POST` that performs the send-back.
   - Board cards gain `Reason` and `Iterate` fields, and the UI gets a "Revise design" button plus a block-reason line.
   - Docs: an amended BR-48, a new BR-57, D-083, TC-490…499, and updated counts.

### How the existing system works, per this document

- A story is planned as three tasks, design → review → implement, chained by `DEPENDS_ON` edges.
- `gateVerdict` records design and review artifacts. A delivered design moves `REVIEW → DONE`. A review with any Blocker moves to `BLOCKED/design-blocked`, and its implementation is held with `design-blocked`.
- `DONE` and `CANCELED` have no exits. The board refuses moves out of Done. An "unblock" of the review re-runs it against the same design. The implementation cannot be unblocked by hand while its review is not `DONE`.
- A review's first attempt opens a fresh session; later attempts resume (D-056/D-069).
- The back office is session-gated (BR-55). Today events carry no actor (`writeEvent` hard-codes `'operator'`).
- The result: a design that draws a Blocker stalls its workflow forever (seen on production, wf-9). The only exit is cancelling.

Most of the claims I checked are accurate:
- `taskstate.go:79-95` and `taskstate.go:145-157`
- `runner.go:296-320` and `runner.go:430-444`
- `designgate.go:454-469`, `designgate.go:573-612`, `designgate.go:702-746`, `designgate.go:784-792`, `designgate.go:860-873`
- `turns.go:102-145`
- `timing.go:132-133` and `timing.go:213-252`
- `store_mem.go:1491-1500`, `store_mem.go:1528-1545`, `store_mem.go:1986-1987`
- `auth.go:355-358` and `auth.go:523-575`
- `store_arcade.go:1061-1073` and `store_arcade.go:1736-1771`
- `taskstate_test.go:47` and `qa-test-plan.html:299`
- `brd.html:1217`, `brd.html:1305`, `brd.html:1327`
- `decision-log.html:1655`
- `design-gate.html:178-180`
- `check-workspace.sh:129,131`

The register arithmetic (331+12 → 353, 477+12 → 499) agrees with `.claude/rules/qa-test-plan.md`.

## Findings

### The reroll is not closed: drift release unblocks every BLOCKED task, including design-blocked reviews - Severity: Major
- Affected scope: §2 G6, §3.6 "The reroll is closed", §3.10 BR-57 ("never re-run by hand on the same design"), §4 failure modes.
- Finding: The document closes the reroll only in `legalMove` (the board). But `releaseDriftLocked` (`dashboard/workspace.go:384-397`) moves **every** `BLOCKED` task on the desk to `READY` with rule `drift-release`, whatever its `BlockReason` is, and it never clears that reason. That puts a gate-blocked review back in the queue:
  - **A `design-blocked` review** is re-run on the same design after a Blueprint sync. This is the bypass §1 and A1 reject.
  - **A `design-revising` review** returns to `READY` while its design is `READY`/running. `reReviewLocked` then finds no `BLOCKED/design-revising` review when the revision is delivered, and the implementation's reason stays `design-blocked`.

  The document never mentions this path. G6 says "cannot be unblocked by hand", which is technically true and misleading, because the machine path stays open.
- Recommendation: Either exclude gate reasons (`design-blocked`, `design-revising`, and arguably the other gate reasons) from `releaseDriftLocked`, or state explicitly that the drift release still re-runs a blocked review and why that is acceptable. Add a failure-mode row and a TC assertion for it either way.

### Adding `Reason` to `boardOf` and making `legalMove` read `BlockReason` breaks the fold-equals-board invariant (TC-308) - Severity: Major
- Affected scope: §3.6 (the new `legalMove` case), §3.9 ("`Reason` is filled there", in `boardOf`).
- Finding: `boardOf` is deliberately shared between the live board and the task fold's test, so that the fold reproduces the board (`board.go:220-223`; `taskfold_test.go:114-126`). The fold rebuilds tasks from `TaskPlanned`/`TaskMoved` only, and never sets `BlockReason` (`taskfold.go:51-62`). After this change, a design-blocked review would differ between the two:
  - **Live board:** `Reason: "design-blocked"`, and `Moves` without `ready`.
  - **Folded board:** an empty `Reason`, and `Moves` with `ready`.

  So the invariant breaks the first time a test history contains a gated block. The document does not mention TC-308 or this interaction.
- Recommendation: Say how the invariant is kept. One option is to fill `Reason` in a decorator (as `Timing` is) and strip it in `foldedBoard`. Another is to have `TestTheTaskFoldReproducesTheBoard` cover the reason explicitly. Also accept that `Moves` depends on a field the fold cannot rebuild, and record that as a known divergence. Add the affected test to the §7 change list.

### `Actor` is defined as "every operator move" but set only by `iterate`; the open questions that ask for an actor are not addressed - Severity: Major
- Affected scope: §3.2 (`Actor` comment), §2 G7, §3.10, §8 R3.
- Finding: The field's comment says it is "the signed-in back-office account that made an operator move. Empty for the machine's moves." The design sets it only on the `iterate` move. Approve, unblock, cancel, dispatch and answer are also operator moves, and they would stay actor-less.

  So "Actor empty" has two meanings: a machine move, or an operator move the code never stamped. A reader of the stream cannot tell them apart, and `writeEvent` writes `'operator'` for both.

  The open-question register makes this a domain point, not just a wording one:
  - `open-questions.html:514-519` (OQ-11) lists "an actor on every event" as still open.
  - `open-questions.html:810-813` (OQ-34) says it waits for "an actor recorded on the event".
  - `decision-log.html:1650` (D-080) records that "which signed-in account dispatched is not recorded".

  §3.10 updates neither register, and does not say whether D-083 narrows OQ-11 or OQ-34.
- Recommendation: Do one of two things.
  - Narrow the field's definition to "set by the `iterate` move only", and state that every other operator move remains actor-less (OQ-11/OQ-34 unchanged).
  - Or stamp every board and answer move with the actor, and add OQ-11/OQ-34 "narrowed by D-083" notes to §3.10.

  Either way, name the decision.

### Key specifications are made by reference to a document that is not in this repository - Severity: Major
- Affected scope: header "Numbers claimed"; §3.2 (`conversations.md:160`, "whichever merges second keeps one definition", "the two land as one line"); §5 numbering.
- Finding: `docs/designs/conversations.md` does not exist in this worktree; it is said to be on another branch. Two things are specified only by pointing at it:
  - the meaning of `Actor`;
  - the `writeEvent` binding.

  A reader cannot check whether the two definitions agree, and "whichever merges second keeps one definition" leaves the actual definition undecided. The numbering also depends on it. That part is handled ("each number is 12 lower"), but the Actor semantics are not.
- Recommendation: State the full `Actor` contract and the exact `writeEvent` change in this document, so it stands alone. Then add one sentence on what the second merge must reconcile, and who checks it.

### "iterate" means two things: the review verdict and the new move rule - Severity: Nit
- Affected scope: §3.2 (`MoveIterate = "iterate"`), §3.3 step 5 (`VerdictIterate`), §3.11.
- Finding: `VerdictIterate = "iterate"` already exists as the review's derived verdict (`designgate.go:215-216, 257-262`). The document adds a move rule with the same string and uses both in neighbouring steps (§3.3 steps 5 and 6). It never defines `VerdictIterate`, and never says the two share a string. Searching events or artifacts for "iterate" will match both meanings.
- Recommendation: Add `VerdictIterate` to the glossary and note the shared spelling. Better still, give the move rule a distinct name (for example `send-back`).

### Several `board.go` and `store_mem.go` line references are off - Severity: Nit
- Affected scope: §1, §3.1, §3.2, §3.4, §3.9.
- Finding: These references point at the wrong lines:

  | Document says | Actually at |
  |---|---|
  | Board actions `board.go:130-139` | `board.go:135-145` |
  | Refusal of moves out of Done `board.go:173-174` | `board.go:171-172` (173-174 is the backlog rule) |
  | Unblock `board.go:181-182` | `board.go:183-184` (181-182 is retry) |
  | `decorateBoardTiming` call `board.go:209-213` | `board.go:216` |
  | `boardOf` `board.go:216-245` | `board.go:224-251` |
  | `kind != TaskKindReview` block `store_mem.go:1437-1462` | `store_mem.go:1443-1463` |

- Recommendation: Correct the references.

### `sessionOf(r).Email` and the 401 claim do not match the code - Severity: Nit
- Affected scope: §3.8, TC-497.
- Finding:
  - `sessionOf` returns `(session, bool)` (`auth.go:355`), so `sessionOf(r).Email` is not the call as written.
  - TC-497 says "no session → 401". For an HTMX request the gate calls `requireBO` instead of returning 401 (`auth.go:544-547`).
- Recommendation: Write `ss, _ := sessionOf(r); actor := ss.Email`, or say what happens when `ok` is false. Split the TC-497 expectation into the JSON case (401) and the HTMX case.

### The counter `n` is used for three different counts - Severity: Nit
- Affected scope: §3.3 step 9, §3.4 `renderTriage`, §3.8 sheet, §3.9 `BoardIterate.N`.
- Finding:
  - The sheet shows "`n of 3`", where `n` is send-backs so far.
  - The toast shows "revision `n+1` of 3".
  - The heading "Sent back for revision `<n>` of 3" uses `n` as the count *including* this one.

  A reader has to work out which count each one shows.
- Recommendation: Name the two quantities, for example `sent` (completed send-backs) and `revision` (= sent+1 at send time), and use them consistently.

### The claim that the revision sees "the reviewer's handoff" is not guaranteed - Severity: Nit
- Affected scope: §3.4, last paragraph.
- Finding: `latestHandoffFor` returns the newest handoff on the **desk** (the product), from any task (`store_mem.go:1270-1294`). With parallel stories, that may be another story's task rather than this design's reviewer.
- Recommendation: Say "the newest handoff on the product's desk, which may be the reviewer's", or drop the claim.

### Re-review is triggered by the gate's intention, not by the design's actual status - Severity: Nit
- Affected scope: §3.6, "On delivery".
- Finding: The proposed code is `if gate != nil && gate.rule == RuleDesignDelivered`, which fires even if the design's own `→ DONE` move was refused. `gateReleaseLocked`'s own comment (`designgate.go:764-770`) warns against conditioning on the gate's intention rather than its state.
- Recommendation: Condition on the design task actually being `DONE`, or explain why the move cannot be refused at that point.

### Undefined reference to "the brief" - Severity: Nit
- Affected scope: §2 N1 ("it is the one optional item of the brief").
- Finding: "The brief" (the request that produced this design) is not defined or linked anywhere in the document.
- Recommendation: Link or quote the brief's optional item, or drop the reference.

### The removed test row's reason argues against this design and is not acknowledged - Severity: Nit
- Affected scope: §3.1, §6 A2.
- Finding: The `{DONE, READY}` row being removed carries the reason "the work shipped; re-running it is a new task, not a new status" (`taskstate_test.go:47`). That is A2, the alternative this design rejects. The document reverses a recorded argument without saying so.
- Recommendation: Add one sentence in §3.1 or the D-083 text saying this reverses that argument, and why.

### The test file and count targets are not fully specified - Severity: Nit
- Affected scope: §5, §3.10.
- Finding:
  - Only TC-490 names its file. TC-491 to TC-496 do not, and TC-497 says only "(`handlers` test)".
  - The decision-log count is given only as "+1". The target word is not stated: "Eighty entries" today (`decision-log.html:18`), and it depends on conversations merging first.
- Recommendation: Name each file, and state the exact count wording for both merge orders.

### Operator text standard differs from BR-53 without saying so - Severity: Nit
- Affected scope: §3.3 step 7, §8 R2.
- Finding: BR-53 (`brd.html:1346`) strips backticks, angle brackets and line breaks from any QA text field "before it can reach an agent's prompt". The triage note reaches a prompt but only refuses line breaks, NUL and reserved fences. BR-53 is scoped to the QA sync, so this is not a violation, but a reader will wonder why the two standards differ.
- Recommendation: State that BR-53 does not apply here and why, or adopt the same stripping.

The goals and non-goals (§2), the session reasoning (§3.5), the cap (§3.7), the alternatives (§6) and the rollout (§10) are clear and could be paraphrased without difficulty.

Counts: B=0 M=4 N=10
