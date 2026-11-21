# Design review: design-iterate (v3), hostile critic, structural and style lens

I checked the document's anchors against the repository. Most of them hold: `taskstate.go:79-95`, `runner.go:430-444`, `designgate.go:454-469`, `:503-504`, `:594-612`, `:684-751`, `:807-834`, `:860-873`, `board.go:162-191`, `:255-305`, `:366-414`, `workspace.go:384-397`, `turns.go:102-143`, `store_mem.go:1443-1463`, `:1491-1501`, `:1528-1545`, and `auth.go:523-577`. The §13 arithmetic also follows from the current registers: 477 rows, 353 Go tests of which 331 are in `dashboard/`, 80 decision rows (`decision-log.html:18`), and "Fifty-six rules" (`brd.html:1217`). The findings below are where the mechanism and the code disagree, or where the design claims more than it delivers.

### The verbatim check defeats CONTEST, the disposition the no-override argument rests on - Severity: Major
- Affected scope: §3.5 item 2, §3.4 (the example triage line `CONTEST — "the order is stated in §3.2"`), §6 A5(c), BR-57 text in §3.10
- Finding and rationale: A5(c) withdraws the override because "the send-back already carries CONTEST — the designer may keep the design and argue for it." The verbatim check then refuses any revision that contains a triage note or finding of 24 runes or more. An operator who contests a finding will naturally quote the design ("§3.2 already says '…'"). A reviewer's note can also repeat a phrase from the design. In both cases the quoted text is the design's own text. A designer that keeps the contested text, which is what CONTEST asks for, is refused with `design-quotes-review`, and every retry fails the same way. There is no cap on these retries (§3.5: "not counted toward the send-back cap"). The only ways out are for the designer to delete the sentence the operator defended, or to cancel the story. The refusal also tells the designer to remove "the review's words" when the words are its own.
- Recommendation: Count a note or finding as copied only if its normalised text appears in the revised document and does **not** appear in the design artifact that was sent back. Add a TC-499 case: a CONTEST note that quotes the original design, with the design unchanged at that spot, passes.

### The refused revision's failure lesson does not carry the refusal sentence, and nothing records the signature - Severity: Major
- Affected scope: §3.4 ("whose body is the refusal sentence naming the finding numbers it copied"), §3.5 item 2, §3.6a, TC-499
- Finding and rationale:
  - **The lesson body is the agent's handoff, not the refusal.** `failure-previous.md` is built from `latestHandoffFor`'s artifact as `failureLesson(a.RunID, sig, a.Body)` (`store_mem.go:1454-1459`). `a.Body` is the run's handoff artifact, which comes from `att.HandoffRaw` (`runner.go:229-237`, set in `adapter_aimanager.go:292-293`). A design reply carries a handoff, so the body would be the agent's own handoff. The gate's `detail` reaches only the `RunExited` caption (`runner.go:298-300`), and that caption is prefixed "the review was refused:", which is wrong for a design.
  - **Nothing writes the signature.** `closeRun` sets `FailureSignature = att.Failure` (`store_mem.go:1176`) before `gateVerdict` runs (`runner.go:210` and then `:294`), and `gateOutcome` has no signature field (`designgate.go:663-670`). The design does not say what writes `design-quotes-review` onto the run. Without it, `failureOfRunLocked` returns "", and the next attempt gets the refused reply as an ordinary `handoff-previous.md`.
  - **Effect.** TC-499's assertion that "failure-previous.md names the finding numbers" cannot pass as specified.
- Recommendation:
  - Add a `sig` field to `gateOutcome`, and have `finishRun` write it to the run record under the same lock.
  - Either record the refusal sentence as the run's handoff artifact (replacing the agent's handoff), or add a separate `failure` part. Say which one.
  - Branch the `RunExited` caption by task kind.

### A design over 64 KiB is silently dropped from the revision packet - Severity: Major
- Affected scope: §2 G3 ("a rotated or lost session loses nothing"), §3.4, §3.5 ("the packet carries everything the revision needs either way")
- Finding and rationale: The egress pass drops, rather than refuses, any packet part over `MaxPartBytes` (`egress.go:515-519`), and the default limit is 64 KiB (`modelconfig.go:75`). A design document can easily exceed that; this one is roughly 100 KB. The revision would then be told "Revise the design in .gaos/packet/design.md" for a file it was never given, and nothing on the board says why. The design never mentions the per-part limit. The same limit already threatens the review packet, and the design inherits that without stating it.
- Recommendation: Name the 64 KiB per-part limit in §3.4 and §8. Then pick one of two options:
  - Refuse the send-back (step 4) when the design artifact exceeds `MaxPartBytes`.
  - Make the freeze fail loudly (a recorded refusal) instead of letting the part be redacted.

  Add a test either way.

### "Never re-run on the same design" is beaten by a one-byte edit - Severity: Major
- Affected scope: §3.6a, BR-57 text ("A review blocked by its own verdict is never re-run on the same design"), D-083
- Finding and rationale: The design's main argument is that a re-review of an unchanged design is a reroll that can eventually clear. §3.6a refuses only a byte-identical revision. A designer told to CONTEST everything can add one space or one rationale sentence and get a new fresh-reviewer sample. That gives up to four samples per story (three send-backs plus the original review). The cap bounds the reroll; nothing closes it. Stating it as a binding rule overclaims, just as the paraphrase limit would if BR-57 did not admit it.
- Recommendation: Do one of two things:
  - State the limit in BR-57, D-083 and §8: "a trivially edited design is a new design; the cap, not the identity check, bounds re-sampling".
  - Strengthen the check: compare the normalised text, and require a minimum changed fraction.

### The drift release keeps breaking BR-54 in the function this design edits - Severity: Major
- Affected scope: §3.6 "The drift release" ("Every other reason is released as today: the list is short on purpose")
- Finding and rationale: BR-54 (`brd.html:1347`) says a QA-added card, born `BLOCKED` / `qa-epic-added`, is "released only by an operator's board move". `releaseDriftLocked` releases every `BLOCKED` task (`workspace.go:391-396`), and that includes those cards. The design rewrites this function and gives the old reason for keeping the skip list short: a stale reason would strand a task. Its own §3.6 now clears `BlockReason` on every unblock, so that reason no longer applies.
- Recommendation: Add `qa-epic-added` to `gateHolds`, or rename the helper to something like `operatorHolds`, and add a TC-499 case. If this is out of scope, record it as ESCALATE with a follow-up, but do not claim the list is "on purpose".

### The rollback procedure misses reviews promoted but not yet run - Severity: Major
- Affected scope: §10
- Finding and rationale: Before a rollback, §10 resolves design-blocked reviews and designs between a send-back and a delivery. It does not cover a review whose revision was delivered and which the scheduler already promoted to `READY`, but which has not run. The older build lacks the §3.5 case, so `sessionForLocked` resumes the reviewer's previous session (`turns.go:142`). That session holds its own earlier findings, which breaks BR-49's isolation for exactly the re-review this feature exists for.
- Recommendation: Widen the pre-rollback list. Any review task whose newest `send-back` `TaskMoved` has no later `RUNNING` move must be resolved or canceled first. That is the same predicate as `reviewSentBackSinceRunLocked`.

### The dropped override is a scope cut that the agent approves on its own - Severity: Major
- Affected scope: §0 "The brief", §2 N1, §6 A5, §11
- Finding and rationale:
  - **A brief requirement is dropped.** The brief asks for "an operator override *only if the review agrees it is safe*". The design drops it and also withdraws the operator's direct clearance from BR-48.
  - **The approver is the agent.** §11 names this as the operator's decision, then says approval is delegated to the orchestrating agent. So a product-owner requirement is removed, and a business rule amended, on the authority of the agent that wrote the design.
  - **An argued reading is not stated.** The document never says whether an all-CONTEST send-back that a fresh review then clears *is* the brief's conditional override. That reading would satisfy the brief.
- Recommendation: Do one of two things:
  - State the argued reading in §2 as the way the goal is met, and change N1 from a non-goal to "delivered as G1 + G4".
  - Mark §11 as requiring the product owner's own sign-off for N1 and the BR-48 amendment, not the delegated approval.

### The verbatim check's normalisation contradicts itself - Severity: Nit
- Affected scope: §3.5 item 2
- Finding and rationale:
  - **Self-contradiction.** The text says "every run of whitespace collapsed to one space, every rune that is neither a letter nor a digit removed". A space is neither a letter nor a digit, so the second step removes what the first one kept.
  - **Unspecified threshold.** It is also unstated whether "at least 24 runes" is counted before or after normalisation.
  - **Two implementations can differ.** Two engineers will build different checks with different false-refusal rates.
- Recommendation: Give the exact pipeline in order. Say that the threshold is measured on the normalised text, and include one worked example.

### An identical revision is labelled `design-missing` - Severity: Nit
- Affected scope: §3.6a
- Finding and rationale: `SigDesignMissing` is only ever an error prefix in today's code (`designgate.go:101,132-195`); no run record carries it. Using it for "identical to the design that was sent back" means the next packet says the attempt "failed with `design-missing`" (`store_mem.go:1321`). That misleads the designer. The parenthetical "`gateOutcome{}, false` path is not used" is also hard to parse.
- Recommendation: Add `SigDesignUnchanged = "design-unchanged"` and rewrite the sentence.

### Approve is refused but the board still offers it - Severity: Nit
- Affected scope: §3.6 "Approve", §3.9
- Finding and rationale: `legalMoves` still lists `done` for a design or review card in `REVIEW` (`board.go:177-178`). The client shows a move that the server then refuses. The decoration already removes `ready` for design-blocked reviews.
- Recommendation: Have `decorateBoardGate` also remove `ColDone` from design and review cards in `REVIEW`, and assert it in TC-499.

### The open-question register table is not in the change list - Severity: Nit
- Affected scope: §3.10, §7 row 17
- Finding and rationale: OQ-11's status cell reads "Open — narrowed by D-026, D-079" (`open-questions.html:103`), and OQ-34's row is at `:120`. The design updates only the prose sections (`:494`, `:805`), so the register table would not list D-083.
- Recommendation: Add both table cells to §3.10.

### The revision instruction contradicts itself - Severity: Nit
- Affected scope: §3.4, `renderTriage` closing paragraph
- Finding and rationale: "Do not quote, cite or answer this review" sits next to "Put your reasoning into the design as design rationale". A model will read the second sentence as permission to answer the findings in paraphrase. That is the soft-boundary leak R2 admits, and here the prompt invites it.
- Recommendation: Reword along these lines: "Change the design so it stands on its own; where you keep a choice a finding questioned, state why the choice is right as a property of the design, without referring to any review."

### TC-499 is overloaded and filed in the wrong test file - Severity: Nit
- Affected scope: §5 TC-499
- Finding and rationale: A test in `board_test.go` named "never rerolled" also asserts the verbatim refusal, the failure lesson and the identical-revision refusal. Those are gate and packet behaviour, so a failure there will be looked for in the wrong file.
- Recommendation: Move those assertions into TC-491 or TC-492 in `design_iterate_test.go`, or rename TC-499 to cover what it asserts.

Sections that hold up: §3.3a's Blueprint pin fix, the event-ordered fresh-session rule in §3.5 (verified against `runner.go:139,157`, `turns.go:205`, `runner.go:595`, `board.go:410`), the route classification and same-origin handling in §3.8, the read-time board decoration and the TC-308 handling in §3.9, and the §13 counts.

Counts: B=0 M=7 N=6
