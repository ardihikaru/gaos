# Readiness review: design-iterate (v3)

**Lens:** gaos-goldfish-readiness (Tech Lead, zero-decision rule)
**Document:** `/home/ardi/devel/projects/ardihikaru/my-projects/gaos/.claude/worktrees/agent-ac38e26c8cafe21d1/docs/designs/design-iterate.md`

**Isolation note:** the harness put `.claude/CLAUDE.md` into my context without my asking. I did not open it with a tool, I did not use it, and nothing below comes from it.

**Checked against the code and found accurate:**
- `moveTaskLocked` (`runner.go:430-444`); the order in `finishRun` (closeRun at `:210`, then `gateVerdict` at `:294`, then the two moves).
- The line layout of `renderReviewArtifact` (`designgate.go:594-612`); the line numbers in `taskTransitions`, `legalMove` and `MoveCard`; `cardTiming`.
- The `readOnlyGET` table, the route gate and `crossOriginOK`.
- Payload persistence through `jsonMap`.
- §13's count arithmetic against `qa-test-plan.md`, `check-workspace.sh:112-131`, `decision-log.html:18`, `brd.html:1217` and `toc/product.js:129,162,168`.
- The TC-208 row and prose (`qa-test-plan.html:299,1718`).

The change list (§7), the routes (§3.8), the board decoration (§3.9) and the merge-order table (§13) are solid.

### A design refused for quoting the review does not get the refusal in its next packet - Severity: Blocker
- Affected scope: §3.4 (the paragraph on the earlier refused attempt), §3.5 measure 2, §3.6a (identical revision), TC-499
- Finding: The design says the refusal sentence reaches the next attempt through the existing failure lesson. The code does not work that way:
  - `failure-previous.md` is built from `latestHandoffFor`, which only returns `ArtifactHandoff` records (`store_mem.go:1290`). Its body is `failureLesson(a.RunID, sig, a.Body)` with `a.Body`, the handoff artifact's body (`store_mem.go:1454-1459`).
  - For a reply that parsed, the handoff artifact holds the agent's own `gaos-handoff` block (`adapter_aimanager.go:292-293`, `runner.go:229-236`). It never holds a gate refusal. The gate's `detail` only reaches the `RunExited` caption (`runner.go:298-300`).
  - So the next packet would show the designer's own handoff under a heading that calls it "the error it returned". TC-499's check that "failure-previous.md names the finding numbers and not their text" cannot pass.
  - Setting the signature has no stated mechanism either. `closeRun` copies `att.Failure` into `FailureSignature` (`store_mem.go:1176`) before `gateVerdict` runs, and `gateOutcome` has no signature field (`designgate.go:663-670`).
  - The identical-revision refusal in §3.6a (`design-missing`) has the same two problems.

  Two engineers would have to invent two different ways to carry the refusal.
- Recommendation: Specify both mechanisms.
  1. Add `sig string` to `gateOutcome`. `finishRun` writes it to the run's `FailureSignature` after the gate's move.
  2. Say how the refusal sentence reaches the packet. Either record a dedicated artifact (for example `<run>-refusal` of kind `handoff`, created only when there is no handoff, which needs an ordering rule against the agent's own handoff), or add a packet part in the §3.4 block that is read from the newest gate-refused run of this design.

  Then make TC-499 assert the exact body.

### The verbatim check is underspecified and will refuse legitimate revisions - Severity: Major
- Affected scope: §3.5 measure 2, R2, TC-499
- Finding: Three things are left open, and one problem is built in.
  - **Which text from a finding is compared.** It could be the note only, or the whole `severity  scope  note` line. A scope such as `docs/designs/x.md:12` is a likely false hit.
  - **When the 24-rune threshold is measured.** It is unclear whether it applies before or after normalisation. Normalisation removes every rune that is not a letter or digit, so spaces go too, and "collapse whitespace to one space" does nothing.
  - **What "findings <n, …>" names.** A matched triage note, and a finding from an earlier round's artifact, have no single numbering.
  - **The order of checks.** The design branch has a screen, the verbatim check and the identical-revision check. The order decides which signature a revision gets.
  - **The built-in problem.** Review notes often quote the design's own wording. Normalised and compared as a substring, a revision that keeps a sentence a reviewer quoted gets `FAILED`, and a retry cannot fix it. That includes a nit the operator marked `DECLINE`. An identical revision would hit this check before the identical check.
- Recommendation:
  - State the compared field (note only), when the threshold is measured (after normalisation), the exact normalisation steps in order, the numbering in the detail (for example `round <r> finding <n>` / `triage <n>`), and the check order.
  - Then exclude from the comparison any note substring that also occurs, normalised, in the previous design artifact, so text the reviewer quoted from the design is never a hit.
  - Add a TC-499 case for that exclusion.

### P1 does not compile on its own - Severity: Major
- Affected scope: §12, P1 (rows 1, 2, 11); §3.3 write step 5
- Finding: P1 includes row 2 (`runner.go`: `moveTaskWithLocked`). That function copies `extra.Actor` and `extra.Triage` and appends `" by <actor>"`. Those fields come from row 4 (`model.go`), which is in P2. P1's exit (`go vet ./... && go test ./...`) cannot be green.
- Recommendation: Move row 4 (`EventPayload.Actor`, `.Triage`, `FindingTriage`, `Clone`) into P1, or move `moveTaskWithLocked` to P2 and keep only the guard in P1.

### P3's manual check cannot be set up as written - Severity: Major
- Affected scope: §12, P3 exit
- Finding: The exit asks for `GAOS_STORE=memory GAOS_RUNNER=manual go run .` with "a product driven to a design-blocked review by the test helpers' steps". The helpers (`gateFixtureNew`, `designReply`, `reviewReply`) live in `_test.go` files and are not in the binary. The only runtime adapter is `aimanager` (`modelconfig.go:129-142`); with no adapter, dispatch is refused (`board.go:369-371`). An implementer has no stated way to reach a design-blocked review locally, so each one will invent a different setup, or skip the check.
- Recommendation: Name a reproducible setup. Options:
  - a test that renders the server board and the sheet to files for inspection;
  - a seed step through existing routes against a stub gateway, giving the stub's location and how it is started;
  - limiting the manual check to what a TC-497 rendering already gives.

  State the exact command.

### "No Go string carries a D-number" is not true - Severity: Nit
- Affected scope: header line 6; §3.8 (`readOnlyGET` entry); §3.2 (`Actor` comment)
- Finding: The new `readOnlyGET` value `"renders the send-back sheet (D-083)"` is a Go string literal, and the `Actor` doc comment names D-083. Under the other merge order, both change.
- Recommendation: Add both to §13's renumber-at-rebase list, or drop the D-number from the code.

### The RunExited caption calls a design refusal "the review was refused" - Severity: Nit
- Affected scope: §3.5 measure 2, §3.6a; `runner.go:298-300`
- Finding: Any `gateOutcome.detail` is appended as `"; the review was refused: "`. The new design-branch details would be captioned as a review refusal.
- Recommendation: State the caption for a design-branch detail (for example `"; the design was refused: "`), and have TC-499 assert it.

### The board still offers approve on design and review cards - Severity: Nit
- Affected scope: §3.6 Approve, §3.9 `decorateBoardGate`
- Finding: `MoveCard` now refuses `approve` for design and review tasks. But `legalMoves` still lists `done` for them in Review, so the board shows a `→ done` button that always gets a 409.
- Recommendation: Have `decorateBoardGate` also remove `ColDone` from `Moves` for design and review cards in `REVIEW`. Extend `foldedBoard`'s restore rule to match.

### The cap of 3 is hard-coded in several places - Severity: Nit
- Affected scope: §3.3 toast, §3.4 `renderTriage`, §3.9 (`BoardSendBack.Open`, the template's `(… of 3)`, the sheet's `revision <Sent+1> of 3`)
- Finding: `maxDesignSendBacks` and `SendBackView.Cap` exist, but the strings and the template write the literal `3`, and `BoardSendBack` has no cap field.
- Recommendation: Read every one of them from `maxDesignSendBacks` (add `Cap` to `BoardSendBack`). Otherwise say plainly that the literal is intended and is covered by a test.

Counts: B=1 M=3 N=4
