# Comprehension review 1: runner-manual (design v1)

Reviewer: `gaos-goldfish-comprehension`, design path and rubric path list only.
Isolation: the harness attached `.claude/CLAUDE.md` unasked; ignored; `CLAUDE.md`, `AGENTS.md`, `GEMINI.md` not opened. No canary in this report.

## Paraphrase (summary)

`GAOS_RUNNER` gains `manual`. `runnerMode()` replaces `runnerEnabled()`, trims and lower-cases, and treats anything unrecognised as off; `runnerDispatches()` is true for on only, `runnerMaySpend()` for on and manual. Under manual the loop runs but each pass only moves scheduler-ready tasks PENDING→READY with the existing `schedule` move; no packet, no screen, no turn. The board's ready→working move is allowed under manual with every gate it has. The desk route stays on-only. Answers, recovery, supervision and the sweep are unchanged, and a task put in READY by any route waits there. The mode is shown in the boot log and `/healthz`; prod compose passes the variable through, empty by default. Six Go tests, one check-stacks row, one conformance case, D-080, BR-56, v0.40.0.

Every section could be paraphrased; no comprehension Blocker. The `file:line` claims checked out (five callers; `superviseRun` reached only from the three paths, so G1 holds by construction; BR-55/D-079 newest; D-078 not in the log).

### Readying and dispatching drift apart in time, but only some gates run at dispatch - Severity: Major
- Affected scope: §2 G3, §3.2 third bullet, §3.3, §6
- Finding: `Schedule` applies the readiness floor (BR-07, `scheduler.go:278-281`), capability/seat (`:294-301`) and budget (`:306-311`); `dispatchNow` (`board.go:347-394`) re-checks none. Under `on` readying and dispatching happen in one pass; under `manual` a card readied hours ago can be dispatched after readiness fell below 45, although BR-07 says "Nothing is dispatched". Budget is `BudgetNormal` today (`store_mem.go:2141-2160`).
- Recommendation: (a) add a limit L4 and reword G3/BR-56, or (b) have `dispatchNow` re-check readiness and budget (and reconsider G6).

### TC-446 bundles two checks, and "Positive + Negative" is not a valid Kind - Severity: Major
- Affected scope: §5 table and Arithmetic
- Finding: the Kind is `Positive|Negative` (`.claude/rules/qa-test-plan.md:46-47`); no row uses a combined Kind; the D-079 precedent is two rows (TC-435/TC-436, `qa-test-plan.html:526-527`). Splitting gives 447, which `check-workspace.sh:127-128` and `README.md:110` enforce.
- Recommendation: split TC-446, redo the arithmetic, one Kind per row.

### The change list misses the README and check-workspace version literals - Severity: Major
- Affected scope: §4 rows 18-20
- Finding: `README.md:8` and `deploy/check-workspace.sh:144` hold "Product version 0.39.0" and are not listed (design contract rule 3).
- Recommendation: add both, changing to 0.40.0.

### L1 gets wrong which card holds the product - Severity: Nit
- Affected scope: §6 L1, §3.2
- Finding: the earliest-ordered eligible card claims the product (`scheduler.go:194-203`, `:320`, `:350`), so a lower-ordinal pending card can also be readied; several READY cards per product are possible; "second pass writes zero events" assumes the parked card sorts first.
- Recommendation: say so; scope TC-441's assertion.

### The answer-session exemption expires, which L2 does not mention - Severity: Nit
- Affected scope: §3.5, §6 L2
- Finding: `answerResumesLocked` exempts only while `now < questionExpiresAt(asked)` (`questions.go:261-268`); after that `sessionForLocked` rotates (`turns.go:139-140`) and `submit` skips the answer route (`adapter_aimanager.go:203`).
- Recommendation: name both causes in L2.

### Project-specific terms used but not defined - Severity: Nit
- Affected scope: §0
- Finding: desk (= product id), packet, egress pass, drift gate, ai-manager, answer route, projection, write hook, `storeContract`, fake gateway, M14.7, D-066.
- Recommendation: one-line glossary entries.

### `/healthz` versus `/api/healthz` - Severity: Nit
- Affected scope: §3.6 vs §9
- Finding: the `/api` prefix strip (`main.go:424-425`) is not explained.
- Recommendation: one clause.

### The refusal text is misleading when no adapter is configured - Severity: Nit
- Affected scope: §3.3
- Finding: `ad == nil || !runnerMaySpend()` shares one message; with `manual` and no adapter it says "the runner is off".
- Recommendation: split the message or accept.

### The decision count depends on D-078 landing first or second - Severity: Nit
- Affected scope: header; §4 row 13; §5
- Finding: `check-workspace.sh:129` enforces the literal.
- Recommendation: state the order or recompute on rebase; add to R3.

### Facts about the live instance are not anchored - Severity: Nit
- Affected scope: §1
- Finding: "p-6 … 47 Automate" and "p-7" have no anchor; they agree with `docs/designs/hisv3-qa-sync.md:25`, `:689`.
- Recommendation: cite it or label as an assumption.

Counts: B=0 M=3 N=7
