# `GAOS_RUNNER=manual`: the scheduler readies, the operator spends

**Status:** Approved (v2), implemented in v0.40.0 (§12). Round 1 reviewed v1 (`reviews/runner-manual-*-1.md`, five lenses); the triage is in `reviews/runner-manual-review-1.md` and every `PROPOSE FIX` is applied below. No Blocker and no Critical/High security finding survives in v2. Approved by the orchestrating agent under the product owner's standing instruction of 2026-11-20 to proceed without confirmation (approval by delegation; §11).
**Feature:** `runner-manual`
**Version target:** v0.40.0
**Decision number claimed:** D-080. D-079 is the newest row (`pustaka/docs/product/decision-log.html:1605-1606`); D-078 is claimed by the QA-inbox design and not yet in the log (`decision-log.html:1625`). Whichever of the two lands second recomputes the "entries" count on `main` (§8 R3).
**BRD rule claimed:** BR-56 (BR-55 is the newest, `pustaka/docs/product/brd.html:1345`).

## 0. Glossary

- **Product / desk** — one product GAOS manages. A task's `DeskID` holds the product id (`p-6`), so "per desk" and "per product" are the same thing here.
- **Turn** — one model call through **ai-manager**, the only model gateway (D-055). Every attempt at a task is one turn and costs money (`dashboard/runner.go:83-90`).
- **Adapter** — the client that can send a turn. Configured by `GAOS_MODEL_MODE`; it says a turn *can* be sent (`dashboard/main.go:190-201`).
- **Runner switch** — `GAOS_RUNNER`. It says a turn *may* be sent: the permission to spend (`dashboard/runner.go:31-43`).
- **Packet / egress pass** — the frozen context sent with a turn, and the screen (BR-32) every packet passes before it is sent (`clearFor`, `dashboard/runner.go:507`).
- **Drift gate** — BR-39: a product whose workspace does not hold its published Blueprint is refused dispatch (`blueprintGate`).
- **Scheduler** — `Schedule`, a pure function of the task graph returning which tasks may start (`Ready`) and why the rest may not (`Blocked`) (`dashboard/scheduler.go:192`). It applies readiness (BR-07), dependency, capability, budget and lease checks (`:270-338`).
- **Scheduler loop** — the goroutine that calls `RunOnce` every `schedulerTick` = 2 s (`dashboard/main.go:251-268`, `dashboard/runner.go:420`).
- **Board** — the per-product kanban. A drag between columns is a *request* checked by `legalMove` (`dashboard/board.go:155-187`).
- **Task status** — closed vocabulary. Here: `PENDING` (column *backlog*), `READY` (*ready*), `DISPATCHED`/`RUNNING` (*working*), `AWAITING_INPUT` (*needs you*).
- **Answer route** — ai-manager's route that records an operator's answer and resumes the session that asked (M15, `dashboard/adapter_aimanager.go:192-211`).
- **Projection / write hook** — `ArcadeStore` keeps a `MemStore` in memory as its projection; every event the projection publishes is written to ArcadeDB by a hook (`dashboard/store_arcade.go:214-232`).
- **Conformance suite** — `storeContract`, one table of cases run against both stores (`dashboard/store_conformance_test.go:41-102`). **Fake gateway** — the test double for ai-manager (`newFakeAIM`, `dashboard/aimanager_test.go:59`).
- **Board dispatch** — the board's ready → working move, `dispatchNow` (`dashboard/board.go:347`).

## 1. Problem

Today `GAOS_RUNNER` has two values. `runnerEnabled()` returns true for `on`, `1`, `true`, `yes` (any case) and false for everything else, unset included (`dashboard/runner.go:37-43`).

With it **on**, three paths spend money:

1. **The scheduler loop.** Started only when an adapter exists and the runner is on (`dashboard/main.go:251`). Every 2 s `RunOnce` asks `Schedule` what may start and, for every task in `decision.Ready`, clears its packet, moves it `PENDING → READY → DISPATCHED` and starts a turn (`dashboard/runner.go:432-497`; moves `:479-480`, turn `:485-492`).
2. **The board dispatch.** `legalMove` maps ready → working to `MoveDispatch` (`dashboard/board.go:170-171`); `MoveCard` hands it to `dispatchNow` (`:273-277`). `dispatchNow` refuses with "the runner is off (GAOS_RUNNER); dispatching spends money and needs it on" unless an adapter is set and the runner is on (`:349-353`); otherwise it applies BR-38 (one live turn per product, `:354-359`), BR-39 (`:360-363`), the dependency check (`:369-374`) and the egress pass (`:379-383`), then moves the task to DISPATCHED and starts a turn (`:384-392`). It does **not** apply the readiness floor (BR-07), and its BR-38 check is not atomic: the lock is dropped for `clearFor` (`:376-379`) and re-taken for the move (`:384`), and the boolean from `moveTaskLocked` is ignored (`:385`).
3. **The desk route** `POST /tasks/{id}/dispatch` (`dashboard/main.go:346`, `dashboard/handlers_tasks.go:95-99`) → `MemStore.DispatchTask`, which screens a packet and, if the adapter is set and the runner is on, starts a turn (`dashboard/store_mem.go:1028-1032`). No template or script calls it any more (`grep -rn "/dispatch" dashboard/templates dashboard/static/*.js` finds no caller), and it applies no dependency check and no BR-38 lease.

With it **off**, nothing spends and the loop does not start (`dashboard/main.go:251`), so nothing moves a task `PENDING → READY` either; the board refuses backlog → ready ("the scheduler moves work between backlog and ready, by its dependencies", `dashboard/board.go:167-168`).

The live instance `gaos.sample-product.com` holds the sample-product product and a QA sandbox product (`docs/designs/sample-product-qa-sync.md:25`, `:689`; on that instance they are `p-6` and `p-7` — an assumption about live data, not checkable from the repository). sample-product's plan is one design task and, after its review clears, one "Automate: <epic>" task per epic (47). Under `on` the loop would dispatch them one after another. The operator wants to **run one chosen card at a time, by hand**: see what is ready, start that turn, look at the result, decide on the next. `off` leaves every card in backlog and refuses the board move; `on` spends without asking.

## 2. Goals and non-goals

**Goals**

- G1. `GAOS_RUNNER=manual`: **no turn is ever started without an operator's board dispatch**.
- G2. Under `manual`, the loop runs and moves every task the scheduler judges ready `PENDING → READY` (no spend), so the operator has cards to choose from — every eligible card, not only the first in plan order (§3.2).
- G3. Under `manual`, the board dispatch works as under `on`, plus two hardenings that apply to both modes: the readiness floor is re-checked at dispatch, and the BR-38 check is made atomic with the move (§3.3).
- G4. A task put in READY by any route (scheduler, answer, redo, retry, unblock, recovery requeue) stays there under `manual` until the operator moves it.
- G5. Boot recovery and supervision of a turn in flight behave identically under all three modes (they never read the switch, §3.5).
- G6. **The spending behaviour of `off` and `on` is unchanged.** Visible differences, all deliberate: the `off` refusal text names `manual`; a missing adapter gets its own refusal text; `/healthz` gains a `runner` key; surrounding whitespace in the value is trimmed; the board dispatch refuses below the readiness floor and refuses a second concurrent dispatch in one product (both modes — both are BR-07/BR-38 being enforced where they were not).
- G7. The mode is visible: boot log line and `GET /healthz`.

**Non-goals**

- N1. The desk route `POST /tasks/{id}/dispatch` keeps today's `on`-only behaviour (§3.4).
- N2. No new event type, task status, packet kind, CSS class or template.
- N3. No change to `Schedule` (`dashboard/scheduler.go:192`).
- N4. No budget, no confirmation dialog, no rate limit. `manual` is a permission switch.
- N5. No way to demote a READY card back to backlog. Cancel remains the only non-spending exit (§6 L1).
- N6. No actor on events (§6 L4).
- N7. No deploy and no VM change. Enabling the mode is the operator's env change (§9).

## 3. Design

### 3.1 The switch: one mode, one predicate

`dashboard/runner.go` replaces `runnerEnabled()`:

```go
const (
	RunnerOff    = "off"
	RunnerOn     = "on"
	RunnerManual = "manual"
)

// runnerMode reads GAOS_RUNNER: on/1/true/yes (any case, surrounding
// whitespace trimmed) is on; manual is manual; anything else, unset or empty
// included, is off -- a typo fails closed.
func runnerMode() string

// runnerMaySpend: an operator's board dispatch may start a turn (on, manual).
func runnerMaySpend() bool
```

Parsing: `strings.ToLower(strings.TrimSpace(envOr("GAOS_RUNNER", "off")))`. `envOr` returns the default for an empty value (`dashboard/main.go:56-61`).

| Caller | Today | After |
|---|---|---|
| boot log, `dashboard/main.go:208-212` | `runnerEnabled()` | `switch runnerMode()`, §3.6 |
| loop start, `dashboard/main.go:251` | `adapter != nil && runnerEnabled()` | `adapter != nil && runnerMode() != RunnerOff` |
| `RunOnce`, `dashboard/runner.go:435` | `ad == nil \|\| !runnerEnabled()` → return | §3.2 |
| `dispatchNow`, `dashboard/board.go:350` | one refusal | §3.3 |
| `DispatchTask`, `dashboard/store_mem.go:1028` | `s.adapter != nil && runnerEnabled()` | `s.adapter != nil && runnerMode() == RunnerOn` |

### 3.2 `RunOnce` under manual: ready every eligible card, never dispatch

```go
func (s *MemStore) RunOnce(ctx context.Context, ad Adapter) ScheduleDecision {
	now := time.Now().Unix()
	mode := runnerMode()
	if ad != nil && mode == RunnerManual {
		in := s.schedulerInput(now)
		in.Tasks = withoutReady(in.Tasks) // a parked card holds no claim
		decision := Schedule(in)
		s.readyPending(decision)
		return decision
	}
	decision := Schedule(s.schedulerInput(now))
	if ad == nil || mode != RunnerOn {
		return decision
	}
	... // on: unchanged, dashboard/runner.go:441-496
}

// readyPending moves each scheduled task PENDING -> READY, and only from
// PENDING, re-read under the lock: the decision was computed from a snapshot,
// and a task an operator dispatched since must not be pulled back.
func (s *MemStore) readyPending(d ScheduleDecision) {
	for _, x := range d.Ready {
		s.mu.Lock()
		var e Event; ok := false
		if t := s.taskPtr(x.TaskID); t != nil && t.Status == StatusPending {
			e, ok = s.moveTaskLocked(x.TaskID, StatusReady, MoveSchedule)
		}
		s.mu.Unlock()
		if ok { s.publish(e) }
	}
}
```

- **Why drop READY tasks from the input (`withoutReady`).** `Schedule` gives each product one `Ready` entry per pass (`claimed["desk:"+…]`, `dashboard/scheduler.go:315-320, 350`) and each agent seat to one task per pass (`taken`, `:259-264, 347`), and it considers READY tasks as well as PENDING ones (`:270`). Left in, a card parked in ready would be re-selected every pass and hold its product and its role's seat forever, keeping the next card in backlog and, for a role with one agent (`architect`, `security`, `devops`, `planner`; `dashboard/fixtures.go:25-35`), another product's card too. Dropped, a parked card claims nothing, so each pass readies the next eligible card of each product until every eligible card is in ready. A task that depends on a READY task reads it as "not in the graph" and stays blocked (`dashboard/scheduler.go:362-376`), which is correct: READY is not DONE. `liveIn` (BR-38) reads only DISPATCHED/RUNNING (`:220`), so the lease is unaffected. `Schedule` is unchanged (N3); the loop discards the returned decision (`dashboard/main.go:263`).
- **Why only from PENDING, under the lock.** The decision is computed from a snapshot taken under a read lock that is released before `Schedule` runs (`dashboard/store_mem.go:2154-2156`). DISPATCHED→READY and RUNNING→READY are legal transitions (`dashboard/taskstate.go:82-83`), so an unconditional move could pull a card the operator dispatched in between back to READY while its turn is live (round-1 critic Blocker). `readyPending` re-reads the status under the write lock and moves only a task that is still PENDING. `taskPtr` exists (`dashboard/questions.go:154`).
- The move is the existing `TaskMoved` with rule `schedule` (`dashboard/board.go:144`). No new event. Once every eligible card is ready, a pass selects nothing and writes nothing.
- Nothing is frozen, screened or sent; `ad` is used only for the nil check.
- `ArcadeStore.RunOnce` delegates to the projection (`dashboard/store_arcade.go:249-251`); the move reaches ArcadeDB through `publish` → write hook, the path `on`'s moves take today. No ArcadeStore code changes.

### 3.3 The board dispatch

`dispatchNow` (`dashboard/board.go:347-394`) becomes, in order:

1. `ad == nil` → refuse: `no model adapter is configured (GAOS_MODEL_MODE); nothing can be dispatched`.
2. `!runnerMaySpend()` → refuse: `the runner is off (GAOS_RUNNER); dispatching spends money and needs it on or manual`.
3. **New — readiness floor (BR-07):** if `m := s.masterPtr(productID); m != nil && m.ReadinessScore() < PlanFloor` → refuse: `readiness <n> is below the planning floor of <PlanFloor>; nothing is dispatched (BR-07)`. Same comparison `Schedule` makes (`dashboard/scheduler.go:278-281`); `masterPtr` at `dashboard/store_mem.go:523`.
4. BR-38, BR-39, dependencies: unchanged (`:354-374`).
5. `clearFor` (unlocked): unchanged.
6. **New — atomic claim:** re-take the lock; if any other task of the product is now DISPATCHED/RUNNING, or this task is no longer READY, release and refuse with the BR-38 text (`<product> already has a live turn (<id>); one at a time per product (BR-38)`) or `<task> is no longer ready (it is <status>)`. Otherwise move to DISPATCHED; if `moveTaskLocked` returns false, refuse with the same "no longer ready" text and start no turn.
7. Start the supervised turn: unchanged.

Steps 3 and 6 apply under `on` too. A refusal at step 6 has screened a packet and spent nothing; for a review task `clearFor` will have reset the reviewer workspace (`dashboard/runner.go:523-527`), which is what a dispatch does first anyway.

Not re-checked at dispatch: **capability** — in gateway mode the turn runs in the product's workspace and the agent seat is a desk-projection fact, not a resource the turn consumes (accepted, §6 L3); **budget** — `schedulerInputLocked` hard-codes `BudgetNormal` (`dashboard/store_mem.go:2160`), so there is nothing to check today; when the budget guard (M10.3) lands it must also be applied in `dispatchNow` (recorded in D-080's consequences, ESCALATE).

`legalMove` is untouched: backlog → ready stays refused (`dashboard/board.go:167-168`).

### 3.4 The desk route stays `on`-only

`DispatchTask` takes `runnerMode() == RunnerOn` — today's meaning exactly. Under `manual` it screens and records and starts **no** turn, as under `off`. It is not reachable from the UI, it has fewer gates than the board dispatch, and leaving it alone keeps the edit in a file another branch is editing to one condition. That it still spends without BR-38 under `on` is recorded in D-080 as a known path (ESCALATE).

### 3.5 Answers, recovery, supervision

- **Answer.** `AnswerQuestion` moves the task `AWAITING_INPUT → READY`, rule `answer` (`dashboard/questions.go:209`). Under `manual` it waits (§3.2 never moves a READY task). On the board dispatch, `clearFor` → `freezePacketFor` attaches the answer and, since the asking attempt is the one just before, sets `pkt.Answer` (`dashboard/store_mem.go:1407-1413`); the adapter sends the turn through the answer route, which resumes the asking session (`dashboard/adapter_aimanager.go:202-211`). The packet keeps the asking session past the rotation age only while the question has not expired (`answerResumesLocked`, `dashboard/questions.go:261-268`; `dashboard/turns.go:139`). Nothing here reads which route dispatched the task. The cost of a long wait is §6 L2.
- **Boot recovery.** `Reconcile` (`dashboard/runner.go:577-603`) never reads the switch; a task that claimed to be running with no turn goes back to READY, rule `requeued` (`:592`), and under `manual` waits there.
- **Supervision.** `superviseRun` (`dashboard/runner.go:83`) never reads the switch.
- **Question sweep** (`dashboard/main.go:271-286`) runs under every mode, unchanged.

### 3.6 Visibility

Boot log (`dashboard/main.go:208-212`; printed only when an adapter is configured):

| Mode | Line |
|---|---|
| on | `runner on: a cleared dispatch spawns <adapter> and spends against it` (unchanged) |
| manual | `runner manual (GAOS_RUNNER): the scheduler readies work; only a board move ready -> working spawns <adapter> and spends against it` |
| off | `runner off (GAOS_RUNNER): a dispatch screens and records, and stops there` (unchanged) |

`GET /healthz` (`dashboard/main.go:432-460`; served publicly as `/api/healthz` because the edge strips the `/api` prefix, `dashboard/main.go:424-425`) adds `"runner": runnerMode()` to the shared body (`:437-443`), so both the 200 and the 503 carry it. It reports the configured switch; whether anything can be sent is the `"adapter"` key beside it. Disclosure: it tells an anonymous caller which of three modes is set, next to `"adapter"`, which `/healthz` already discloses; accepted (SEC-5, DECLINE).

### 3.7 Deploy files

- `deploy/stacks/compose.prod.yaml:97-98`: the comment "GAOS_RUNNER stays unset" becomes a pass-through, default empty (= off):

  ```yaml
  # THE RUNNER (D-080, BR-56): the permission to spend. Empty/unset = off:
  # nothing is ever dispatched. "manual" = the scheduler readies work and only
  # a board move ready -> working sends a turn, one chosen card at a time.
  # "on" = the scheduler dispatches everything ready, unasked. Read from the
  # deploying shell, so it must be on EVERY redeploy line; a plain redeploy
  # falls back to off. This key overrides a GAOS_RUNNER in prod.dashboard.env.
  GAOS_RUNNER: "${GAOS_RUNNER:-}"
  ```

  The one mechanism is the deploy command line. A compose `environment:` key overrides the same key from `env_file` (`compose.prod.yaml:64`): a host whose `prod.dashboard.env` sets `GAOS_RUNNER` is overridden to empty = off — fail-closed. The committed template `deploy/stacks/dashboard.env.example` does not set it; whether a host's file does is checked by the operator with a path-only grep (§9).
- `deploy/check-stacks.sh`, after the `GAOS_BO_LABEL` check (`:142`), two checks:
  - `has $PROD 'GAOS_RUNNER: "${GAOS_RUNNER:-}"' "prod passes the runner switch from the deploy shell, empty (off) by default"`
  - a count: `[ "$(grep -c 'GAOS_RUNNER:' $PROD)" = 1 ] && ok "prod sets GAOS_RUNNER in exactly one place, the pass-through" || bad "prod sets GAOS_RUNNER in more than one place; the runner must come only from the deploy shell"` — together with the first, any hard-coded value in any spelling fails.
- `deploy/rebuild-restart.sh`: one override line in the comment block (`:95-103`): `#   GAOS_RUNNER=manual                  the permission to spend: unset=off, manual=one board-chosen turn at a time`; and before `dc up -d` (`:310`), for prod: `echo "   GAOS_RUNNER=${GAOS_RUNNER:-} (empty = off)"` so the deploy log records the mode it deployed.
- `deploy/stacks/compose.dev.yaml`: unchanged (dev is retired).

## 4. Change list, by phase

Paths from the repository root. Each phase ends with its exit command.

**P1 — code and Go tests** (exit: `cd dashboard && go test -race ./...` passes with 303 top-level tests; and with `GAOS_ARCADE_TEST_URL` from `deploy/schema/scratch.sh up`, `go test -run 'TestStoreConformance' ./...` passes)

| # | File | Change |
|---|---|---|
| 1 | `dashboard/runner.go` | constants, `runnerMode`, `runnerMaySpend`; remove `runnerEnabled`; `RunOnce`, `withoutReady`, `readyPending` (§3.2) |
| 2 | `dashboard/main.go` | boot log; loop start; `/healthz` `runner` key |
| 3 | `dashboard/board.go` | `dispatchNow` steps 1-3 and 6 (§3.3) |
| 4 | `dashboard/store_mem.go` | `:1028` condition and the comment at `:1019-1020` only |
| 5 | `dashboard/runner_manual_test.go` (new) | eight tests, §5 |
| 6 | `dashboard/store_conformance_test.go` | one `storeContract` case (§5) |

**P2 — deploy files** (exit: `./deploy/check-stacks.sh` passes and prints the two new `ok` lines of §3.7 verbatim)

| 7 | `deploy/stacks/compose.prod.yaml` | §3.7 |
| 8 | `deploy/check-stacks.sh` | §3.7 |
| 9 | `deploy/rebuild-restart.sh` | §3.7 |

**P3 — registers, docs, counts** (exit: `cd pustaka && go test ./... && go run . check ./docs`; `./deploy/check-workspace.sh` with every count line `ok`; `grep -n "0.40.0" pustaka/docs/assets/toc.js README.md`; `grep -n 'id="d-080"' pustaka/docs/product/decision-log.html`; `grep -n "BR-56" pustaka/docs/product/brd.html`; `grep -n "manual" pustaka/docs/architecture/gateway.html pustaka/docs/infra/environments.html`)

| 10 | `pustaka/docs/product/qa-test-plan.html` | rows TC-440…TC-449 (§5); scenario `h3 id="go-runner-manual"` under `#implemented` quoting each test's `t.Fatalf`/`t.Errorf` and each script line; TC-124/TC-125 rows and their prose (`:215-216`, `:1289`) say "forty-five" contract cases (corrects the stale "forty-two"; 44 existing + 1); lede and register intro counts |
| 11 | `pustaka/docs/assets/toc/product.js` | QA count, Go-test count, decision count, BRD rule count |
| 12 | `pustaka/docs/product/decision-log.html` | D-080 row after D-079 (§4.1); "Seventy-eight entries" → "Seventy-nine entries" |
| 13 | `pustaka/docs/product/brd.html` | BR-56 appended after BR-45 in §9.6 `#rules-gateway` (numbers follow allocation order, as BR-55 does in §9.8); "Fifty-five rules" → "Fifty-six rules"; `:324` "When the runner is switched on…" gains one sentence on `manual` |
| 14 | `pustaka/docs/architecture/gateway.html:245` | row: `GAOS_RUNNER` · `off` · `off`: nothing is sent. <code>manual</code>: the scheduler readies work and only a board move ready → working sends a turn (D-080). <code>on</code>: the scheduler dispatches everything ready |
| 15 | `pustaka/docs/infra/environments.html:202` | row: `GAOS_RUNNER` · unset (off) on both prod hosts; `manual` when the owner runs cards by hand; `on` only while acceptance runs · The owner's decision — it is the permission to spend (D-080) |
| 16 | `pustaka/docs/changelog.html` | v0.40.0 entry (minor): Added `GAOS_RUNNER=manual`; Changed the board dispatch re-checks the readiness floor and claims the product atomically; Added `runner` in `/healthz`; Added the compose pass-through and two check-stacks lines |
| 17 | `pustaka/docs/assets/toc.js:22` | `semver: "0.40.0"` |
| 18 | `README.md` | `:8` "Product version 0.40.0"; `:100`, `:106`, `:110` counts |
| 19 | `deploy/check-workspace.sh` | count literals `:112-116`, `:127-131`, `:141-143`, and the version literal `:144` |
| 20 | `.claude/rules/qa-test-plan.md` | highest in use TC-449, counts, range map |

### 4.1 D-080 (decision-log row, four cells)

- Date: 2026-11-20.
- Decision: **`GAOS_RUNNER=manual`: the scheduler readies work and only the operator's board dispatch spends.** Context: `GAOS_RUNNER` was off or on; on, the loop dispatched every ready task on every product, which on the instance carrying sample-product means a design task and then 47 automate tasks unasked; off, nothing ever became ready. Decision: a third value. Under `manual` the loop runs, ready-marks every task the scheduler would start (a parked card claims nothing), and never dispatches; a turn is sent only when an operator moves a ready card to working, through BR-07, BR-38, BR-39, the dependency check and the egress pass. The board dispatch now re-checks the readiness floor and claims its product atomically under both `on` and `manual`. Unrecognised values are off. The desk route stays on-only.
- Consequences: the mode is read from the deploy shell, so `manual` must be on every redeploy line and a plain redeploy falls back to off; `on` is equally one word away, and choosing it is the operator's decision as before. The budget guard, when built, must also gate the board dispatch. Under `on` the unused desk route still spends without BR-38. Events still carry no actor, so which signed-in account dispatched is not recorded.
- Links: `docs/designs/runner-manual.md`; `qa-test-plan.html#go-runner-manual` (TC-440 to TC-449); `brd.html#rules-gateway` (BR-56).

BR-56 text: *A turn is sent only under the runner switch. `off` sends none. `on` lets the scheduler dispatch whatever it judges ready. `manual` lets the scheduler move work to ready and nothing further: a turn is sent only when a signed-in operator moves a ready card to working, and that move applies the readiness floor (BR-07), one live turn per product (BR-38), the drift gate (BR-39), the dependency check and the egress pass. An unrecognised value is `off`. (D-080)*

## 5. Tests and the register

One register row per new `func Test…`; one Kind per row (both polarities in `data-tags` where a test asserts both). IDs continue from TC-439.

| TC | Test / script | Kind | Asserts |
|---|---|---|---|
| TC-440 | `TestRunnerModeReadsGAOS_RUNNER` | Negative | unset, `""`, `off`, `maybe`, `manaul` → off; `on`, `ON`, `1`, `true`, `yes` → on; `manual`, `MANUAL`, `" manual "` → manual; `runnerMaySpend` true only for on and manual |
| TC-441 | `TestAManualRunnerReadiesWorkAndNeverDispatchesIt` | Negative | one planned product, fake gateway, manual: the first `RunOnce` writes exactly one `TaskMoved` rule `schedule` PENDING→READY; repeated passes until one writes nothing; then no task is DISPATCHED/RUNNING, no `TaskRun` exists, zero chat posts; and every task the plain `Schedule` would have readied is READY |
| TC-442 | `TestTheManualReadyStepMovesOnlyAPendingTask` | Negative | `readyPending` fed a stale decision naming a task that is now DISPATCHED leaves it DISPATCHED and writes no event; the same decision for a PENDING task moves it |
| TC-443 | `TestAManualRunnerDispatchesTheCardTheOperatorMoves` | Positive | under manual, a READY card moved to working answers rule `dispatch`, one chat post, and its run exits |
| TC-444 | `TestTheBoardDispatchRefusesWithoutPermission` | Negative | off → exact §3.3 step-2 text; no adapter under manual → step-1 text; readiness below the floor → step-3 text; under manual the desk route `DispatchTask` starts no turn (zero posts); each refusal writes no event |
| TC-445 | `TestTwoBoardDispatchesInOneProductStartOneTurn` | Negative | two concurrent `MoveCard` ready→working on two READY cards of one product: exactly one succeeds, one refused, exactly one chat post |
| TC-446 | `TestAnAnsweredTaskWaitsInReadyUnderAManualRunner` | Positive | asked → answered → READY; `RunOnce` under manual leaves it READY and posts nothing; the board dispatch sends exactly one answer under the next attempt's request id and no `/v1/chat` post |
| TC-447 | `TestHealthzSaysWhichRunnerModeIsOn` | Positive | `/healthz` answers `"runner":"off"` unset, `"manual"` and `"on"` when set |
| TC-448 | `check-stacks.sh` | Positive | prod passes `GAOS_RUNNER: "${GAOS_RUNNER:-}"` |
| TC-449 | `check-stacks.sh` | Negative | prod sets `GAOS_RUNNER:` in exactly one place |

Conformance (both stores; covered by TC-124/TC-125): `AManualRunnerReadiesAndNeverDispatches` — plan a workflow through the `Store` interface (as `casePlanWritesGraph`), then reach `SetAdapter` and `RunOnce` by type assertion (they are not on `Store`, as at `dashboard/main.go:252-254`); `RunOnce` under manual; some task READY, none DISPATCHED, zero posts.

The tests that set `GAOS_RUNNER=on` (`dashboard/board_test.go:134`, `dashboard/designgate_test.go:856`, `dashboard/taskfold_test.go:43`) stay as they are and must pass: G6's proof for `on`.

**Arithmetic.** Rows 439 + 10 = **449** ("four hundred and forty-nine"). Go tests: eight new `func Test…` in `dashboard/` → dashboard 295 + 8 = **303**, total 317 + 8 = **325** ("three hundred and twenty-five"); `pustaka/` 22. Contract cases 44 + 1 = **45**. Decisions 78 + 1 = **79** (if D-078 lands first, 80). Rules 55 + 1 = **56**. Retired rows stay 45.

## 6. Accepted limits

- **L1. A ready card can only leave ready by dispatch or cancel.** The board refuses ready → backlog (`dashboard/board.go:167-168`). With §3.2 a parked card claims nothing, so it blocks no other card; the cost is only that "not now" is expressed by leaving it in ready.
- **L2. A long wait in ready can cost a failed turn.** After the question expires, the packet no longer keeps the asking session (`dashboard/questions.go:261-268`, `dashboard/turns.go:139-140`), the session rotates and the answer route is skipped (`dashboard/adapter_aimanager.go:203`): the turn runs in a new session with the answer in the packet. If instead ai-manager dropped the session before GAOS stops resuming it, the answer route refuses, the fallback ordinary turn carries the same dead session id, the 410 is recorded `session-lost` and the task goes FAILED (`dashboard/adapter_aimanager.go:263-264`, `dashboard/runner.go:254-255`, BR-40); the operator's retry opens a new session with the answer in the packet. One wasted turn at most.
- **L3. Capability is not re-checked at dispatch** (§3.3).
- **L4. The dispatching account is not recorded.** Any signed-in back-office account may dispatch (no roles; OQ-11 open), and events carry no actor (D-079). ESCALATE to the actor follow-up.

## 7. Alternatives considered

- **A1. A per-product pause flag.** Rejected: a stored field, a UI, and a default that still spends for products created later. One env var fails closed.
- **A2. `off`, plus allowing backlog → ready on the board.** Rejected: the board would re-implement the scheduler's checks, and `off` would start spending (violates G6).
- **A3. Ready every eligible card by dropping READY tasks from the scheduler input.** **Adopted** (§3.2): a three-line filter in `RunOnce`, no change to `Schedule`. v1 rejected it for a cost it did not have.
- **A4. Widen the desk route too.** Rejected, §3.4.
- **A5. Keep the mode in `prod.dashboard.env` instead of a compose pass-through.** Rejected: the env file is a host-side secret file the operator edits by hand on each VM; the pass-through puts the choice on the deploy line where the deploy log records it, and the empty default fails closed.
- **A6. Refuse `on` in `rebuild-restart.sh` without a confirmation flag.** Declined: `on` is an existing, documented mode used by acceptance runs; the deploy log line and `/healthz` make the mode visible.

## 8. Risks

- **R1. Empty value.** compose passes `""` when the shell does not set it; `envOr` returns the default for empty (`dashboard/main.go:56-61`); TC-440's `""` row.
- **R2. A typo opens nothing.** `manaul` is off (TC-440); the boot line and the deploy log line show it.
- **R3. Concurrent branch and D-078.** Another branch edits `dashboard/store_mem.go` (`CreateWorkflow`, the packet's `task.md`); this change touches one condition and one comment in `DispatchTask` (`:1019-1028`). If it conflicts, rebase onto `origin/main`. If D-078 lands first, the decision count becomes "Eighty entries".
- **R4. `on` is one word away.** The pass-through makes `on` as easy to set as `manual` (round-1 zoom-out, SEC-2). Accepted and recorded in D-080; the deploy log line and `/healthz` show which was set.

## 9. Rollout and rollback

Ships as tag v0.40.0. Without a deploy-shell change nothing differs from v0.39.0: compose passes empty = off. Before enabling, the operator checks on the host that no env file sets the switch, printing paths only: `grep -l GAOS_RUNNER deploy/stacks/prod.*.env` (expect no output). To enable, add `GAOS_RUNNER=manual` to the redeploy line, e.g. on rnd-v3-vpn: `GAOS_RUNNER=manual GAOS_TUNNEL_IP=10.10.0.9 … ./deploy/rebuild-restart.sh prod deploy --tag v0.40.0`. Verify `/api/healthz` → `"runner":"manual"`. Every later redeploy must carry it again. Rollback: redeploy without the variable (off), or redeploy v0.39.x — never a tag before D-079 (v0.39.0), which brings back the forgeable back-office cookie.

## 10. Open questions

None.

## 11. Approval

Approved 2026-11-20, v2, by the orchestrating agent under the product owner's standing instruction of 2026-11-20 to proceed without confirmation (approval by delegation, not by the product owner in person). Basis: round 1's one Blocker (critic, the manual move pulling a live task back) and the one High security finding (SEC-1, the non-atomic BR-38 check) are both fixed in the design (§3.2, §3.3); every Major is fixed, contested or escalated in the triage (`reviews/runner-manual-review-1.md`). No second round was run: no Blocker survives (policy step 7).

## 12. As built (v0.40.0)

- Implemented as §3–§5. Two deviations, both narrower than the design:
  - The conformance case asserts that every task a manual pass's decision named is READY and that nothing is DISPATCHED/RUNNING and nothing was sent; it does not require a non-empty READY set, because whether the seeded floor has a free seat for the root task is the fixture's business, not the contract's. The non-empty, exact-count assertion lives in the MemStore test (§5's TC-441, fleet idled).
  - The concurrent-dispatch test (§5's TC-445) frees a second task of its prerequisites in test setup, because a freshly planned product has a single root.
- **Renumbered at merge.** `fix/workflow-dispatch` (v0.39.1) landed first and took TC-440…TC-442, so this change's rows are **TC-443…TC-452** (§5's TC-440 → TC-443 … TC-449 → TC-452, same order). The counts move from that base: rows 442 + 10 = 452; Go tests 320 + 8 = 328 (dashboard 298 + 8 = 306); contract cases 44 + 1 = 45; decisions 79 and rules 56 as designed (D-078 had not landed). v0.39.1 also fixed a test-only data race in `dashboard/api_v1_test.go` that this change had found independently; the fix on `main` is theirs.
- Verified: `cd dashboard && go test -race ./...` (306 tests, race-clean); the ArcadeDB conformance half against `deploy/schema/scratch.sh` (the new case passes, not skipped); `cd pustaka && go test ./... && go run . check ./docs`; `./deploy/check-stacks.sh`; `./deploy/check-workspace.sh`.
