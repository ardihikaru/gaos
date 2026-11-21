# Design review (critic): design-iterate v2

**Document:** `/home/ardi/devel/projects/ardihikaru/my-projects/gaos/.claude/worktrees/agent-ac38e26c8cafe21d1/docs/designs/design-iterate.md`

**Overall:** most of the code references check out. Two defects break the design's core claims: the verdict parser reads the wrong line, and a revision that delivers no design can still be approved, which reruns the review on the old design. I raise the strongest objection first. The design says "nothing clears the gate without a fresh review of a revised design", but it never checks that the design the re-reviewer is handed is new. It assumes that a design task reaching `DONE` means a new artifact exists, and the code does not guarantee that.

### The verdict is on line 3, not line 2, so the anchored reader breaks the whole gate - Severity: Blocker
- **Affected scope:** §0 Glossary ("line 2 `verdict: …`, line 3 `open blockers`"), §3.3 `reviewVerdictOf` and check step 5, §3.11 ("reads a review's verdict from its anchored line"), TC-490.
- **Finding and rationale:** `renderReviewArtifact` writes `"# Design review — %s\n\n"` (`dashboard/designgate.go:596`): a heading and then a blank line. The verdict is therefore line 3 (`:597`) and `open blockers` is line 4 (`:598`). Built as specified, `reviewVerdictOf` returns `""` for every artifact that exists or will exist. That has two effects:
  1. Check step 5 refuses every send-back with "has no recorded verdict that asks for a revision". The feature never fires.
  2. §3.3 also switches `gateStateLocked` (`designgate.go:503-504`) to this reader. Every `DONE` review would then read as `GateFailed`. `gateReleaseLocked` (`:782-793`) would never release an implementation again, including on production, for every workflow and not only for sent-back ones.

  This is a wrong premise in the core mechanism, written as a literal spec.
- **Recommendation:** Specify the format exactly: "the first line after the heading's blank line that begins `verdict: `, and it must be line 3". Better still, find the line as the first non-blank line after line 1 and require it to match `^verdict: (accept|iterate)$`. Fix the §0 description to match. Add a test: `reviewVerdictOf(renderReviewArtifact(...))` round-trips for both verdicts and for a drift line. Also add a regression test that an existing cleared review still releases its implementation after the switch.

### A revision that delivers no design block can be approved, and the review then reruns on the old design - Severity: Blocker
- **Affected scope:** G6, §3.6 ("closed on every route"), F2, BR-57 text ("never re-run on the same design"), §3.4 / §3.5 re-review packet.
- **Finding and rationale:** F2 says a revision with no design block goes `FAILED` "as any attempt (`runner.go:283-285`)". It does not:
  - The runner only moves to `FAILED` on `runErr != nil` or `att.Failure != ""` (`runner.go:278`).
  - `gateVerdict` returns `(_, false)` when `ExtractDesign` fails (`designgate.go:685-690`), and nothing in the tree sets `att.Failure` to `SigDesignMissing`. That constant appears only in the parser and in the contract text (`handoff_reply.go:280`).
  - So a reply with a valid handoff and no `gaos-design` block ends in `REVIEW`. The board offers `approve` (`board.go:177-178`), which moves it to `DONE`.
  - The scheduler then promotes the `PENDING` review (`scheduler.go:371-383`). Its packet reads the newest design artifact (`store_mem.go:1495`), which is still the **old** design, because no new one was recorded.

  That is exactly the gate bypass by repetition that §1 sets out to remove, reopened through a normal board action. A revision that re-delivers a byte-identical document has the same effect.
- **Recommendation:** Add a precondition for the re-review, enforced in `gateVerdict`'s review branch, in `freezePacketFor`, or in both. When the design has `sent > 0`, the newest design artifact must have been created after the newest `send-back` event (compare the artifact's run against the event Seq). If it was not, block the review with `design-missing`. Also refuse a delivered design whose body equals the previous artifact's. Correct F2, and add the "approved empty revision" case to TC-499.

### The drift-release skip list is keyed on stale block reasons and will strand tasks - Severity: Major
- **Affected scope:** §3.6 drift release, §3.9 ("The gate's reasons are always written when the gate blocks and cleared when it releases").
- **Finding and rationale:** The claim in §3.9 holds only for implementation tasks (`designgate.go:784-793`). For design and review tasks, the reason is written at `runner.go:311` and never cleared:
  - The board's unblock does not clear it (`board.go:295-301`).
  - `refuseDispatch` (`runner.go:623-647`) moves the task to `BLOCKED` without writing a task `BlockReason`.

  Example: a design blocked with `design-screened-out` or `design-quotes-review` is unblocked by hand, and its next dispatch is then refused for drift. It sits in `BLOCKED` carrying the stale gate reason. The new `gateReason` check makes `releaseDriftLocked` skip it permanently, and `decorateBoardGate` shows a false `blocked: design-quotes-review`. The same happens to a `review-failed` review that was unblocked and then hit drift.
- **Recommendation:** Pick one:
  - (a) Clear `BlockReason` in `moveTaskLocked` on every move out of `BLOCKED` that does not write a new reason.
  - (b) Have `refuseDispatch` write a drift reason on the task.
  - (c) Narrow the skip list to `design-blocked` and `design-unreviewed`, the only reasons that carry a verdict or a gate hold.

  Either way, remove the §3.9 claim or make it true, and add the stale-reason case to TC-499.

### OQ-16 is the wrong open question - Severity: Major
- **Affected scope:** G9, N7, §3.10 (`open-questions.html` "OQ-16 (`:514-519`)"), D-083 / BR-48 cross-references.
- **Finding and rationale:** Lines 514-519 sit under OQ-11, "Authentication approach" (`pustaka/docs/product/open-questions.html:494`). That is the paragraph ending "…and an actor on every event." OQ-16 is "Is high availability required, and for whom?" (`:827`), which has nothing to do with actors. As written, the docs step would add a "Narrowed by D-083" note to the HA question.
- **Recommendation:** Replace OQ-16 with OQ-11 everywhere (G9, N7, §3.10).

### The actor claims contradict each other across the two merge orders - Severity: Major
- **Affected scope:** N7, §3.2 ("If `feat/conversations` has merged first"), BR-48 and BR-57 text, the OQ narrowing sentences.
- **Finding and rationale:**
  - N7 says `Event.actor` "stays `'operator'`" and "Every other operator move stays actor-less".
  - The amended BR-48 says "other operator moves are not attributed yet", and the OQ notes say "no other event does yet".
  - §3.2 says that under the **expected** order, `feat/conversations` binds the `actor` column to `EventPayload.Actor` for "this operator action". If that is so, the column no longer stays `'operator'`, and other operator actions are attributed.

  The published text would be false under the order the design calls expected. The design defers this to "the author of whichever branch merges second", but the registers are written now.
- **Recommendation:** Write the register text for each merge order, as §13 does for the numbers, or state the actor rules in terms that hold under both. Add the actor wording to the §13 table.

### A design refused for quoting the review gets no feedback, so the loop is paid for and uncapped - Severity: Major
- **Affected scope:** §3.5 item 2, F3.
- **Finding and rationale:** After a `design-quotes-review` refusal, "it runs again in its session with the same packet". The refusal sentence goes only into the run's `detail`, which is timeline text. Nothing reaches the designer:
  - No failure lesson: `failureOfRunLocked` is keyed on a failure signature, and this path sets none.
  - No packet part.
  - The resumed session holds the designer's own reply, not the refusal.

  The likely result is the same quote again. Each unblock costs a design turn, and these attempts do not count against `maxDesignSendBacks`. R5's cost bound therefore does not hold.
- **Recommendation:** On the next freeze after a `design-quotes-review` block, add the refusal sentence and the offending finding numbers (never the text) to `findings.md`, or record a failure signature so `failure-previous.md` carries it. Either count these refusals toward the cap or give them their own small limit. Test it.

### The operator override in the brief is dropped without an explicit operator decision - Severity: Major
- **Affected scope:** §0 "The brief" (lists "an operator override *only if the review agrees it is safe*"), N1, §6 A5, §11.
- **Finding and rationale:** The product owner asked for a conditional override. The design declines it on its own authority. A5's argument is reasonable, but under the rubric the operator resolves trade-offs, and §11 ("Pending round 2") does not raise this as a decision. A reader approving "the design" could miss that a requested capability is absent. BR-48 is also rewritten to remove "Only the operator clears it", which is a product-rule change.
- **Recommendation:** Add a decision item to §11: "Brief item 6 (conditional override) is not delivered; operator to confirm." Note in BR-48's amendment that the operator's clearance power is removed on purpose.

### The template snippet uses `$card` before it is defined - Severity: Nit
- **Affected scope:** §3.9 "Server board".
- **Finding and rationale:** The reason line and the button are placed "under the meta line" (`backoffice.html:398`), but `{{$card := .ID}}` is declared at `:400`. Placed as described, the template fails to parse at boot.
- **Recommendation:** Say "after `{{$card := .ID}}` (`:400`)" instead.

### Count wording in the ToC - Severity: Nit
- **Affected scope:** §13 ("'Eighty-two entries' (… `toc/product.js:129` …)").
- **Finding and rationale:** `pustaka/docs/assets/toc/product.js:129` says "Eighty decisions", not "entries".
- **Recommendation:** Name the exact strings for each file.

### Ambiguous scope of the 24-rune threshold - Severity: Nit
- **Affected scope:** §3.5 item 2.
- **Finding and rationale:** "any triage note of `last` or any finding note … that is at least 24 runes long" can be read as applying the length floor to both kinds of note or to finding notes only. Two implementers would build different checks.
- **Recommendation:** State it explicitly, for example: "every note, triage or finding, of at least 24 runes".

### The route's body format is undefined for JSON clients - Severity: Nit
- **Affected scope:** §3.8 POST, steps 2-3.
- **Finding and rationale:** The triage is read with `r.FormValue("d<N>")`, but step 2 lists a JSON 400 response, and `handleMoveCard` accepts a JSON body (`handlers_board.go:55-63`). It is unclear whether a JSON body is accepted.
- **Recommendation:** Say "form-encoded only; a JSON content type is 415", or define the JSON schema.

### The TC-497 zero-findings worst case cannot be parsed - Severity: Nit
- **Affected scope:** §3.9 last paragraph.
- **Finding and rationale:** "shows the refusal-free sheet with `- none` rows absent and only the footer" mixes the packet's `- none` with the sheet's rows. Also, a `design-blocked` review always has at least one blocker finding, so zero parsed findings means the parser skipped every line.
- **Recommendation:** State what the sheet renders when `Findings` is empty and `Refusal == ""`.

### Booking a left `DONE` segment as "blocked" mislabels time - Severity: Nit
- **Affected scope:** §3.1 Card timing.
- **Finding and rationale:** While the design sits in `DONE`, the time is the review's AI turn plus the operator's wait, not a block. Calling it `BucketBlocked` keeps the partition summing correctly but misattributes the time.
- **Recommendation:** Keep the change, and say in the D-083 consequences that the bucket is a convention for the partition, not a cause.

### Checked and confirmed
These held up when checked against the repository:
- **State machine:** the `taskTransitions` rows (`taskstate.go:79-95`), the `moveTask` callers (only `runner.go:437` and `board.go:297`), `legalMove` refusing moves out of Done (`board.go:171-172`) and the unblock gate (`board.go:288-294`).
- **Sessions:** `sessionForLocked` (`turns.go:102-143`), and the claim that sessions are decided before the dispatch move on both routes (`runner.go:498/525`, `board.go:410/433`).
- **Scheduler and drift:** `unmetDeps` (`scheduler.go:371-383`) and `releaseDriftLocked` (`workspace.go:384-397`).
- **Packets:** the `freezePacketFor` ranges.
- **Storage:** the unbounded event load (`store_arcade.go:1736-1758`) and the single-transaction `mutate` / `persist`.
- **Docs and tests:** `readOnlyGET` (`auth_test.go:258-274`), the `check-workspace.sh` lines 112-131, BR-46 to BR-50 and "Fifty-six rules" (`brd.html:1217`), and the "five rules" lines (`design-gate.html:170,212`, `architecture.js:104`).
- **Arithmetic:** the §13 numbers follow from the 331/22/477 baseline in `.claude/rules/qa-test-plan.md`.

Counts: B=2 M=5 N=6
