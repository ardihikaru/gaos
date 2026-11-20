# Zoom-out review 1: runner-manual (design v1)

Reviewer: `gaos-goldfish-zoom-out`, design path and rubric path list only.
Isolation: the harness attached `.claude/CLAUDE.md` to the reviewer's context unasked; not opened, not used. No canary in this report.

Overall: the core is the right size — one env value, one condition in each of five places, one branch in `RunOnce`, reusing `MoveSchedule`, `moveTaskByID` (no-op on same status, `runner.go:408-410`) and `dispatchNow`. Verified: five callers, `envOr` empty → default (`main.go:56-61`), `Schedule` re-selects READY (`scheduler.go:270`), per-pass product claim (`:320`, `:350`), 295 + 22 = 317 tests. Budget is hard-coded `BudgetNormal` (`store_mem.go:2160`), so manual loses no budget check `on` has.

### The change list leaves out the version literals that check-workspace enforces - Severity: Major
- Affected scope: §4 rows 18-20; §5 exit commands
- Finding: `README.md:8` ("Product version 0.39.0") and `deploy/check-workspace.sh:144` (which checks that literal) are not in the change list; following it leaves README at 0.39.0 and the exit command would not catch it. Counts otherwise match §5.
- Recommendation: add `README.md:8` to row 19 and `deploy/check-workspace.sh:144` to row 20.

### The compose pass-through also makes `on` a shell variable in prod, and the design never says so - Severity: Major
- Affected scope: §3.7, §8, §9
- Finding: today prod cannot spend without editing a committed file (`compose.prod.yaml:97-98`). The pass-through makes `on` as easy as `manual` — one word on the redeploy line — and through `on` the 47 HISv3 cards would dispatch unasked. The `hasnt` check guards only a hard-coded literal. Neither §8 nor D-080 mention the widening.
- Recommendation: state it as an accepted consequence in §8 and D-080, or keep `on` out of reach of the pass-through. One sentence in the decision is the smallest fix.

### Alternative A3 is rejected for a cost it doesn't have - Severity: Nit
- Affected scope: §7 A3, §6 L1, N4
- Finding: A3 needs no new `Schedule` input: the manual branch of `RunOnce` can drop READY tasks from the input `schedulerInput` already builds. A parked card then stops holding its product claim and seat; dependents read it as "not in the graph" and stay blocked (`scheduler.go:366-372`); the decision is discarded by the loop (`main.go:263`). L1 matters for the very case in §1 (one agent per role for architect/security/devops/planner, `fixtures.go:25-35`).
- Recommendation: restate A3's cost as a ~3-line filter in `RunOnce` and let the operator choose.

### Three tests check the same five-line branch - Severity: Nit
- Affected scope: §5 TC-441, TC-445, the conformance case; §4 rows 5-7
- Finding: TC-441 and the conformance case assert the same; TC-445 tests that a `schedule` `TaskMoved` survives a restart, which `on` already writes (`runner.go:481`); row 7 leaves the file undecided.
- Recommendation: fold TC-441's second-pass check into the conformance case; drop TC-445 unless no existing test covers it.

### G6 says "byte-for-byte unchanged", but `off` and `on` both change slightly - Severity: Nit
- Affected scope: §2 G6, §3.1, §3.3
- Finding: the refusal text under `off` changes; trimming widens `on`.
- Recommendation: "`off` and `on` spend exactly as today; only the refusal wording and whitespace parsing change."

### Three exported constants and three accessors for five call sites - Severity: Nit
- Affected scope: §3.1
- Finding: `runnerDispatches` has one caller; other sites compare `runnerMode()` directly.
- Recommendation: keep `runnerMode()` and `runnerMaySpend()`; write `runnerMode() == RunnerOn` at `DispatchTask`.

### The decision count assumes D-078 lands after this change - Severity: Nit
- Affected scope: header; §4 row 13; §5
- Finding: if `qa-inbox-api.md`'s D-078 merges first the literal is "Eighty entries".
- Recommendation: write the count as current + 1 at merge; add the ordering to R3.

Counts: B=0 M=2 N=4
