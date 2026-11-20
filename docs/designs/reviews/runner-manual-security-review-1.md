# Security review 1: runner-manual (design v1)

Reviewer: `gaos-goldfish-security`, given the design path and the rubric path list only.
Isolation: the reviewer reports the harness attached `.claude/CLAUDE.md` to its context unasked; it did not open it and used none of it. No canary string appears in this report.

The reviewer notes the rubric is out of date on one point: since D-079 the board move route needs a real signed session (`dashboard/auth.go:315-345`, gate `:502-555`, cross-origin refusal `:560-577`); reviewed against current code.

### SEC-1 The BR-38 lease in `dispatchNow` can be raced, and under `manual` that route is the only way to spend - Severity: High
- Component: `dashboard/board.go` `dispatchNow`; BR-38
- Evidence: `dashboard/board.go:348-359` checks for a live turn under `s.mu`; the lock is released at `:376`, `clearFor` runs unlocked at `:379`, and DISPATCHED is written only after re-locking at `:384-385`. The design states "at most one turn per product at a time" (`runner-manual.md:123`), and retry/redo/unblock/answer make two ready cards in one product a normal state.
- Attack path: two board requests for two ready cards of one product inside the `clearFor` window both pass the check, both reach DISPATCHED, both start a turn: two live turns in one product and double spend. A double-submit of the same card also passes twice — `moveTaskLocked`'s boolean is ignored (`:385`) and the goroutine starts anyway (`:388`). The race exists under `on` today; the design makes this the sole spend path.
- Remediation: make the lease atomic — re-check under the lock right before the move, use the boolean from `moveTaskLocked`, and do not start `superviseRun` when the move did not happen. Add a TC with two concurrent `MoveCard` calls asserting exactly one chat post.
- Effort: small

### SEC-2 The permission to spend becomes ambient shell state at deploy time, and the design leaves out two other places the value can come from - Severity: Medium
- Component: `deploy/stacks/compose.prod.yaml`, `deploy/rebuild-restart.sh`
- Evidence: the pass-through `GAOS_RUNNER: "${GAOS_RUNNER:-}"` (`runner-manual.md:154-163`) is filled from the deploying shell and from a `.env` in the project directory; the service also loads `env_file: [./prod.dashboard.env]` (`compose.prod.yaml:64`), which an `environment:` key overrides; the script pins no `--env-file` (`rebuild-restart.sh:109`). Whether `prod.dashboard.env` sets `GAOS_RUNNER` is unverifiable (credential file, not opened).
- Attack path: a shell that still exports `GAOS_RUNNER=on`, or a stray `.env`, brings prod up with `on` on a routine redeploy, and the scheduler dispatches the 47 sample-product tasks unattended. If the env file sets the runner, the new key silently overrides it to empty. Nothing records the deployed mode except live `/healthz`.
- Remediation: pick one documented source. If the pass-through stays: print the resolved mode before `up`; refuse `on` without a confirmation flag; pin `--env-file` or check no `.env` exists; compare `/healthz` after deploy. Confirm whether `prod.dashboard.env` sets `GAOS_RUNNER`.
- Effort: small

### SEC-3 No account is recorded on an operator-chosen spend - Severity: Medium
- Component: event stream; `dashboard/runner.go:401-412`
- Evidence: `TaskMoved` carries a rule and no actor (`runner.go:411-412`); D-079 records "events still carry no actor" (`decision-log.html:1621`); the session email is on the request context (`auth.go:551`, `:355`).
- Attack path: under `manual` every spend is a human decision any account can make (no roles, OQ-11 open); a disputed spend cannot be traced.
- Remediation: put the session email on the `dispatch` `TaskMoved` payload, or record the limit as L4 with OQ-11, and say in BR-56 that "operator" means any signed-in account.
- Effort: small

### SEC-4 The design keeps a weaker spend path it could close for little cost - Severity: Medium
- Component: `POST /tasks/{id}/dispatch`, `dashboard/store_mem.go:1028-1032`
- Evidence: the route applies no dependency check and no BR-38 lease and no UI calls it (`runner-manual.md:27,129`); still registered (`main.go:346`); under `on` it starts a turn.
- Attack path: under `on`, a signed-in client posts directly and starts a turn without the lease or the dependency gate. Inert under `manual`.
- Remediation: make the condition `false` or route it through `dispatchNow`; or record it as a known open spend path with an OQ instead of a non-goal.
- Effort: quick-win

### SEC-5 The public `/healthz` now tells anyone whether spending is enabled - Severity: Low
- Component: `dashboard/main.go:432-460`; `auth.go:504`
- Evidence: `"runner"` added to both bodies (`runner-manual.md:148`); `/healthz` is public at `/api/healthz`.
- Attack path: an observer learns whether a stolen session could spend. Little beyond `"adapter"`.
- Remediation: accept and record, or report the mode only to a signed-in caller.
- Effort: quick-win

### SEC-6 Rolling back to "an older tag" can bring back the forgeable cookie - Severity: Low
- Component: §9
- Evidence: `runner-manual.md:242`; D-079 consequences (`decision-log.html:1624-1625`).
- Attack path: following §9 to a pre-D-079 tag reopens every back-office route.
- Remediation: say "the previous tag (v0.39.x), never a tag before D-079", or drop the older-tag option.
- Effort: quick-win

### SEC-7 Reviewer isolation is not enforced by tooling, and the deny-list was crossed in this session - Severity: Medium
- Component: the design gate's reviewer controls
- Evidence: the deny-list is instructions only (`.claude/rules/agent-workspace.md:99-114`); the harness put `.claude/CLAUDE.md` into the reviewer's context unasked; the globs miss a bare `.env`.
- Attack path: not an attack on the dashboard; a harness that injects files could carry a secret into a committed report.
- Remediation: enforce the deny-list with permission rules; add `.env` and `**/*.env`; have `check-workspace.sh` scan reports for canaries and `.claude/CLAUDE.md` content; update the reviewer rubric about `signedIn()`.
- Effort: small

### SEC-8 The `check-stacks.sh` negative check only catches one literal string - Severity: Low
- Component: `deploy/check-stacks.sh` (proposed)
- Evidence: `hasnt $PROD 'GAOS_RUNNER: "on"'` misses unquoted `on`, `"1"`, `"true"`, `"yes"` (`runner.go:39`).
- Attack path: a later edit hard-codes the runner on in another spelling and the check passes.
- Remediation: assert the file has exactly one `GAOS_RUNNER:` line and that it equals the pinned pass-through.
- Effort: quick-win

Counts: Critical=0 High=1 Medium=4 Low=3
