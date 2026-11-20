# Readiness review 1: runner-manual (design v1)

Reviewer: `gaos-goldfish-readiness`, design path and rubric path list only.
Isolation: the harness attached `.claude/CLAUDE.md` unasked; not opened, not used; the reviewer asks that the run be treated as a failed isolation probe for that file. No canary in this report.

Checks out: the five callers (`main.go:208`, `:251`, `runner.go:435`, `board.go:350`, `store_mem.go:1028`); the anchors in §1 and §3.2–§3.5; 295 / 317 / 439; D-079 at `decision-log.html:1605`; BR-55 at `brd.html:1345`.

### The manual `RunOnce` move can pull a just-dispatched task back to READY - Severity: Major
- Affected scope: §3.2
- Finding: the manual branch moves to READY without checking the current status; the status came from a snapshot; DISPATCHED→READY and RUNNING→READY are legal (`taskstate.go:82-83`); `moveTaskLocked` only skips equal status (`runner.go:408`); `dispatchNow` drops the lock for `clearFor` (`board.go:376-385`). A tick that snapshots READY, then a dispatch, then the tick's move writes DISPATCHED→READY while the turn is live: the BR-38 lease is released, a second turn is possible, and later moves are dropped (`runner.go:380-383`).
- Recommendation: under the lock, move only when still PENDING (e.g. a `moveTaskIfLocked(id, from, to, rule)` helper); assert it with a stale decision in a test.

### The compose `environment:` entry overrides any `GAOS_RUNNER` set in the env file - Severity: Major
- Affected scope: §3.7, §9, G6
- Finding: `env_file: [./prod.dashboard.env]` (`compose.prod.yaml:64`) is overridden by an `environment:` key, so a host that sets the runner in the env file is turned off. Manual then depends on putting `GAOS_RUNNER=manual` on every redeploy line; a plain redeploy silently drops to off — fail-closed but undocumented toil, and a choice between two mechanisms the design never makes.
- Recommendation: state as a checked premise that no host env file sets it (grep path only), or choose the env file and drop the pass-through; name the one mechanism and what happens on the next redeploy.

### Change #7 leaves the target file as "A or B", and A does not exist - Severity: Major
- Affected scope: §4 row 7; §5 TC-445
- Finding: `dashboard/store_arcade_test.go` does not exist; the precedent is in `store_conformance_test.go:109`.
- Recommendation: choose `dashboard/store_conformance_test.go`.

### The TC-124/TC-125 prose count is left undecided, and the register is already out of step - Severity: Major
- Affected scope: §4 row 11; §5
- Finding: "forty-two contract cases" (`qa-test-plan.html:215-216`, `:1289`) vs 44 entries (`store_conformance_test.go:42-85`); after this change 45.
- Recommendation: write "forty-five", note it corrects drift, list the lines.

### No phases, exit conditions or per-change verification - Severity: Major
- Affected scope: §4, §5
- Finding: a flat list of 21 changes; no order (the count literals in `check-workspace.sh` must move with the register and README; the register rows land with the tests); no verification for the docs rows, the rule file or the script comment.
- Recommendation: P1 code + tests (`go test -race`), P2 deploy files (`check-stacks.sh` with the new `ok` lines), P3 registers/docs/counts (`pustaka check`, `check-workspace.sh`); a grep per docs row.

### The D-080 row and the published doc rows are named but not written - Severity: Major
- Affected scope: §4 rows 13, 15, 16, 17
- Finding: BR-56 is written; D-080's cells, the two env-table rows, the changelog entry and the scenario `h3` anchor are not; `environments.html:202` needs a per-host default.
- Recommendation: write them verbatim; name the `h3` id.

### Kind "Positive + Negative" is not a register value, and TC-446 bundles two checks - Severity: Nit
- Affected scope: §5
- Recommendation: one Kind per row, both polarities in `data-tags`; split TC-446 (447).

### TC-441 asserts "≥1" where the exact value is known - Severity: Nit
- Affected scope: §5
- Recommendation: assert exactly one move for one planned product.

### G6 says "byte-for-byte unchanged", but `off` changes - Severity: Nit
- Affected scope: §2 G6
- Recommendation: "the spending behaviour of off and on is unchanged", and list the visible differences.

### L2's fallback description does not match the adapter - Severity: Nit
- Affected scope: §6 L2
- Finding: the fallback `Client.Submit` carries the asking session (`adapter_aimanager.go:210`); a gone session is BR-40's `session-lost`.
- Recommendation: restate L2.

### BR-56's position is unstated - Severity: Nit
- Affected scope: §4 row 14
- Recommendation: "append after BR-45 in §9.6", noting allocation order.

### The decision count depends on D-078 not landing first - Severity: Nit
- Affected scope: header; §5
- Recommendation: whichever lands second adds one on `main`.

### The `hasnt` check is narrow - Severity: Nit
- Affected scope: §3.7
- Recommendation: a pattern covering `on` as a literal or default.

Counts: B=0 M=6 N=7
