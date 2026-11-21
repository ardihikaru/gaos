# Design review: `design-iterate` (critic lens)

**Isolation note.** While I was working, the harness put the contents of `.claude/CLAUDE.md` into my context. I did not ask for that file. I have not used it and I quote nothing from it. You should know this before you run the canary check.

**Verified.** These claims hold against the code:
- **`taskTransitions`:** DONE has no exits (`dashboard/taskstate.go:87`).
- **`moveTaskLocked`:** at `dashboard/runner.go:430-444`, with the same-status no-op at :437, not :438.
- **The gate's move order:** the gate moves after the runner's move (`runner.go:303-320`).
- **Gate reasons and verdict lines:** `designgate.go:454-469, 702-708, 739-748, 784-792` match.
- **`VerdictIterate` exists:** `designgate.go:216`.
- **`sessionForLocked`:** at `turns.go:102-143`.
- **The whole stream loads on boot:** `store_arcade.go:1736-1758`, so the cap survives a restart.
- **TC-208's "Fifteen":** `qa-test-plan.html:299, 1718`, and the row being removed is at `taskstate_test.go:47`.
- **The baseline counts:** 477 rows and 331 + 22 Go tests (`.claude/rules/qa-test-plan.md`), so 343 / 365 after conversations is correct.

The state-machine guards (§3.1) and the session rule (§3.5) are sound as written.

---

### The board reason and the reroll guard break the fold-equals-board invariant (TC-308) - Severity: Blocker
- Affected scope: §3.9 ("`Reason` is filled there", in `boardOf`); §3.6 (the new `legalMove` case reads `t.BlockReason`)
- Finding and rationale: `boardOf` is the pure projection that `TestTheTaskFoldReproducesTheBoard` compares, by `reflect.DeepEqual`, against the board rebuilt from events (`dashboard/taskfold_test.go:114-126, 152`). `foldTasks` never restores `BlockReason` (`dashboard/taskfold.go:49-62`). The test's history plans a workflow, and planning sets `design-unreviewed` on every gated implementation task (`store_mem.go:2165`). So:
  - Filling `BoardCard.Reason` inside `boardOf` makes the live board and the folded board disagree straight away, and TC-308 fails.
  - The reroll case in `legalMove` has the same problem for the same reason. `legalMoves` runs inside `boardOf` (`board.go:237`). A live review held with `design-blocked` or `design-revising` offers no `ready`, but its folded twin does.
  - The underlying issue is that the reroll lock (`design-revising`) exists only on the task record, not in the event stream. A replayed board, or Replay mode (BR-20), would show a reroll the live system refuses. That weakens the design's claim that the audit lives in the events.
  
  The change list and §5 say nothing about TC-308 or `foldedBoard`.
- Recommendation: Pick one approach and write it down.
  - **(a)** Fill `Reason` and `Iterate` in read-time decorators, as Timing is (`foldedBoard` already nulls Timing at `taskfold_test.go:119-124`). Put the reroll refusal in `MoveCard` next to the existing `gateHoldsLocked` check (`board.go:285-294`), plus a decorator that removes `ready` from `Moves`.
  - **(b)** Put the reason into the event stream, for example a `BlockReason` field on `TaskMoved` and on the iterate move, and fold it.
  
  Either way, add `taskfold_test.go` to the change list and update TC-308's prose.

### The re-reviewer is lobbied through the revised design itself - Severity: Major
- Affected scope: §3.5 reason 2; N3; §6 A5(c); BR-57's text "sees the new design and the rubric only"
- Finding and rationale: §3.5 refuses to let the re-reviewer read "CONTEST — this is fine", because that is lobbying. But A5(c) depends on exactly that happening: "the designer keeps the design and says why, and a fresh reviewer either agrees or does not". The only route for "says why" is the design document, which goes into the reviewer's packet (`store_mem.go:1491-1500`). A designer given `findings.md` will predictably add a "Changes since review" or "Response to finding 1" section. That passes the previous findings and the operator's triage to the re-reviewer and anchors it — the failure the goldfish model exists to prevent (BR-47 withholds the author's session state). N3 and BR-57 promise an isolation the mechanism does not deliver.
- Recommendation: Decide this explicitly. One option is to have `renderTriage` tell the author to put any rationale into the design as design rationale, without citing the review, the findings or the operator, and to state in BR-57 and D-083 that this is a soft boundary. The other is to accept the channel and remove the "rubric only" promise from N3, BR-57 and TC-493's assertion text.

### `Actor` is set on one operator move only, and its documented meaning is then wrong - Severity: Major
- Affected scope: §3.2 (the `Actor` comment, "Empty for the machine's moves"); G7; R3; §3.10
- Finding and rationale:
  - Only `iterate` fills `Actor`. Every other operator move (approve, redo, retry, unblock, cancel, dispatch through `MoveCard`, `board.go:295-301`) stays empty. So an empty `Actor` does not mean "machine", contrary to the field comment.
  - `writeEvent` keeps the literal `'operator'` for every other row (`store_arcade.go:1068`), including the machine's moves. The schema says the column holds "agent id, user id, or 'system'" (`schema/07-events.sql:26`).
  - The open-question register names "an actor on every event" as still open (`open-questions.html:519`, OQ-11), and OQ-34 waits on exactly this (`open-questions.html:811-813`). The design narrows both, but `open-questions.html` is not in §3.10 or the change list.
- Recommendation: Either pass `sessionOf(r).Email` through `MoveCard` too, so every board move records its actor (small, since the handler has the session), or reword the comment to "set only by the iterate move". In both cases, add narrowing notes to OQ-11 and OQ-34 and add `open-questions.html` to the change list.

### A re-review records the wrong Blueprint drift line in a permanent artifact - Severity: Major
- Affected scope: §3.6 "What happens next … the gate as it is"
- Finding and rationale: `blueprintPinsLocked` takes the review's pin from the first run of the review task it finds, the oldest (`designgate.go:817-822`). Before this change a review almost always had one run. With re-reviews it has several, possibly under newer Blueprints. The `drift:` line that `renderReviewArtifact` writes (`designgate.go:599-602`) would compare the new design's pin with the first review's pin. That line goes into an append-only artifact and into the implementer's `findings.md`.
- Recommendation: Use the pin of the run being judged (`runID`, through `blueprintPinForRunLocked`), add it to the change list for `designgate.go`, and assert it in TC-492 or TC-493.

### The claim that "the revision sees the reviewer's handoff" does not hold in general - Severity: Nit
- Affected scope: §3.4, last paragraph
- Finding and rationale: `latestHandoffFor` returns the newest handoff on the desk (`store_mem.go:1281-1305`). A product with several picked stories fans out into parallel chains (`store_mem.go:1982-1994`), so the newest handoff may belong to another story's task.
- Recommendation: Say "the newest handoff on the desk, often the reviewer's", or drop the sentence.

### The form limits do not match the data they bound - Severity: Nit
- Affected scope: §3.8, POST route
- Finding and rationale:
  - `ParseReview` sets no limit on the number of findings (`designgate.go:311-353`). Triage for finding 101 or later is silently ignored.
  - 100 notes of 300 runes, URL-encoded, can exceed the 64 KiB body cap, and the response to that is not specified.
  - The HTML `maxlength` counts UTF-16 code units, not runes.
- Recommendation: State the response when the cap is exceeded, and either derive N from `len(findings)` or say what happens above 100.

### "Blocked: <reason>" will show stale reasons - Severity: Nit
- Affected scope: §3.9 / G8
- Finding and rationale: A task's `BlockReason` is never cleared on unblock (`board.go:295-301`). A block that writes no reason (the runner's `HandoffBlocked`, `runner.go:282-283`) will show whatever the previous block left, for example `qa-epic-added` (`qacatalog_create.go:222`).
- Recommendation: Show the reason only when the gate or another known writer set it, or clear it on every unblock.

### `board.go` line citations have drifted - Severity: Nit
- Affected scope: §0, §1, §3.1, §3.2, §3.9
- Finding and rationale:
  | Claimed | Actual |
  |---|---|
  | Done refusal :173-174 | `board.go:171-172` |
  | Unblock :181-182 | `board.go:183-184` |
  | Actions :130-139 | `board.go:136-145` |
  | `Board` :209-213 | `board.go:207-218` |
  | `boardOf` :216-245 | `board.go:224-251` |
  | `freezePacketFor` block :1437-1462 | `store_mem.go:1443-1463` |
  | Freeze `runner.go:604` | `runner.go:592` |
- Recommendation: Re-anchor these.

### Some claims rest on a branch this reviewer cannot see - Severity: Nit
- Affected scope: header "Numbers claimed"; §3.2
- Finding and rationale: `docs/designs/conversations.md` is not in this tree, so `conversations.md:160` and lines 245-256 cannot be checked. The shared `Actor` definition and the one-line `writeEvent` merge are assumptions.
- Recommendation: Label them as assumptions, and name who reconciles `Actor` if conversations defines it differently.

### A hedged item in §3.10 - Severity: Nit
- Affected scope: §3.10, "`graph.html` (… if it lists payload keys)"
- Finding and rationale: It does not. `graph.html:132` describes `payload` only as the reducer's delta.
- Recommendation: Remove the item or state the exact change.

### The precedent for adding payload keys is mis-cited - Severity: Nit
- Affected scope: §3.2, "payload keys have been added before (`At`, D-081; `Clock` …)"
- Finding and rationale: `At` and `Clock` are fields of `eventPayloadWire` (`store_arcade.go:1038-1046`), not of `EventPayload`, the delta. The real precedent is fields added to `EventPayload` itself, such as `Question` and `Genesis` (`model.go:269-275`).
- Recommendation: Cite those instead.

### Triage notes get a weaker sanitisation than BR-53 sets for text reaching a prompt - Severity: Nit
- Affected scope: §3.3 step 7; R2
- Finding and rationale: BR-53 strips backticks, angle brackets and line breaks from external text before it can reach an agent's prompt (`brd.html:1346`). Triage notes only refuse reserved fences and line breaks. The operator is trusted, so the risk is low, but the inconsistency should be stated.
- Recommendation: Either apply the BR-53 strip, or say why operator text is exempt.

### Counting send-backs scans the whole event stream on every board render and every freeze - Severity: Nit
- Affected scope: §3.3 step 6; §3.9 `decorateBoardIterate`
- Finding and rationale: `s.events` has no bound (`store_mem.go:456`). Card timing already keeps an incremental index (`timing.go:389-405`).
- Recommendation: Reuse that index or keep a per-task counter.

### The frontend rule is not addressed - Severity: Nit
- Affected scope: §3.9
- Finding and rationale: `.claude/rules/frontend.md:16-28` requires the taste, motion and break-ui passes for a new modal and a new card line. The design names no worst-case data for the sheet (100 findings, 300-rune non-Latin notes, long scopes) and no reduced-motion behaviour.
- Recommendation: Add a line naming the break-ui cases that TC-497 will render.

Counts: B=1 M=3 N=10
