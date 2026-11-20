# Critic review 1: runner-manual (design v1)

Reviewer: `gaos-goldfish-critic`, design path and rubric path list only.
Isolation: the harness attached `.claude/CLAUDE.md` unasked; not opened with a tool, not used. No canary in this report.

Counter-thesis: under `on`, READY is a column a task passes through in milliseconds; under `manual` it is a parking column with no time limit. Three things assume READY is brief: the scheduler re-selecting READY every pass, the scheduler being the only place readiness and budget are re-checked, and the answer-resume window.

### The manual `RunOnce` can move a live turn back to READY - Severity: Blocker
- Affected scope: §3.2 (the sketch, "every pass after the first is a no-op"); TC-441
- Finding: `Schedule` runs on a snapshot (`store_mem.go:2154-2156`) and `decision.Ready` includes READY tasks (`scheduler.go:270`), so the parked card is re-selected every 2 s. If `dispatchNow` moves it to DISPATCHED/RUNNING between the snapshot and `moveTaskByID(…, StatusReady, MoveSchedule)`, the move is not a no-op: DISPATCHED→READY and RUNNING→READY are legal (`taskstate.go:82-83`). The BR-38 lease disappears (`scheduler.go:220`), the card can be dispatched again, `superviseRun`'s READY→RUNNING and the exit READY→REVIEW are refused (`taskstate.go:81`, dropped at `runner.go:274`), and the outcome is lost from the status. On prod `ArcadeStore.MoveCard` holds the Arcade lock (`store_arcade_blueprint.go:36-37`) but `RunOnce` does not (`store_arcade.go:249-251`).
- Recommendation: move only if the current status is PENDING, under the projection lock; add a negative test that interleaves a dispatch; correct the "no-op" claim.

### Under `manual`, the board dispatch is the only way to spend, and it skips the scheduler's readiness, capability and budget checks - Severity: Major
- Affected scope: §3.2, §3.3, G3, N5
- Finding: `dispatchNow` checks switch, BR-38, BR-39, deps, egress (`board.go:350-383`), not readiness (BR-07, `brd.html:1236`), capability or budget (BR-35, `brd.html:1285`). Under `on` those run right before the loop dispatches (`scheduler.go:278-311`); under `manual` once, at readying. Budget is a stub today (`store_mem.go:2141-2160`); when M10.3 lands `manual` spends through a path the guard never sees.
- Recommendation: re-run the scheduler's verdict for that task in `dispatchNow`, or extract a shared gate; at minimum record BR-07/BR-35 on this path in D-080 and BR-56.

### L2 contradicts BR-40 and the adapter code - Severity: Major
- Affected scope: §3.5, §6 L2
- Finding: the answer exemption holds only until question expiry (`questions.go:261-268`); after it the session rotates and the answer route is skipped (`adapter_aimanager.go:203`). When ai-manager has dropped the session, the fallback `Submit` carries the same dead `SessionID`; a 410 becomes `SigSessionLost` (`adapter_aimanager.go:263-264`) and the task goes FAILED (`runner.go:254-255`); BR-40 (`brd.html:1297`). The real cost of a long park is one failed turn plus a retry.
- Recommendation: rewrite L2; accept the cost or clear `SessionID` when the question has expired; add a TC-443 variant.

### A parked READY card cannot be demoted, and it blocks other products - Severity: Major
- Affected scope: §6 L1; G1; §1
- Finding: a readied card leaves READY only by dispatch (spends) or cancel (terminal, `board.go:165-166`); ready→backlog is refused (`:167-168`) and would be re-readied in 2 s anyway. L1 admits a parked card holds a single-agent role's seat across products (`fixtures.go:25-35`).
- Recommendation: add a hold mechanism (needs a decision), or state the limit as L4 and move A3 to a dated open question.

### TC-124/TC-125's case count is already wrong, and the change list does not fix it - Severity: Nit
- Affected scope: §4 row 11
- Finding: the register says "forty-two contract cases" (`qa-test-plan.html:215-216`); `storeContract` has 44 (`store_conformance_test.go:42-85`); this adds a 45th.
- Recommendation: name "forty-five".

### The conformance case cannot run "through the `Store` interface" - Severity: Nit
- Affected scope: §5
- Finding: neither `RunOnce` nor `SetAdapter` is on `Store`; type assertions are needed (as `main.go:252-254`).
- Recommendation: say so.

### The register rows do not fit the register's format - Severity: Nit
- Affected scope: §5
- Finding: one Kind per row; TC-446 bundles two check lines (precedent TC-435/436), giving 447.
- Recommendation: one Kind per row; split TC-446; redo arithmetic.

### The test file for TC-445 is left undecided - Severity: Nit
- Affected scope: §4 row 7
- Finding: "or" violates design rule 6.
- Recommendation: choose one file.

### Two doc references are missing or slightly off - Severity: Nit
- Affected scope: §4; header
- Finding: `brd.html:324` describes runner behaviour and is not listed; the D-079 row starts at `decision-log.html:1606`.
- Recommendation: add and fix.

### The mode is shown misleadingly when no adapter is configured - Severity: Nit
- Affected scope: §3.3, §3.6
- Finding: with manual and no adapter the board says "the runner is off"; `/healthz` reports `manual` though the loop does not start.
- Recommendation: own refusal text for `ad == nil`; describe the healthz key as meaningful only with an adapter.

### The `hasnt` check is narrow - Severity: Nit
- Affected scope: §3.7
- Finding: fixed-string grep (`check-stacks.sh:25`) misses `on` unquoted and `"${GAOS_RUNNER:-on}"`.
- Recommendation: assert exactly one `GAOS_RUNNER:` line equal to the pass-through.

Counts: B=1 M=3 N=7
