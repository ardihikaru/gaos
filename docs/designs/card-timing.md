# Card timing: how long a task spent in each status, and how much of it nobody was working

**Status:** Approved (v2). Round 1 reviewed v1 with five lenses (`reviews/card-timing-{comprehension,zoomout,critic,readiness}-1.md`, `reviews/card-timing-security-review-1.md`); the triage is in `reviews/card-timing-review-1.md` and every `PROPOSE FIX` is applied below. No Blocker was raised; the one High security finding (SEC-1) is fixed in this version (§3.6). Approved by the orchestrating agent under the product owner's standing instruction of 2026-11-20/21 ("work among your agents, never as my confirmation") — approval by delegation, not by the product owner in person (§11).
**Feature:** `card-timing`
**Version target:** v0.41.0
**Decision number claimed:** D-081 (text in §3.9). D-080 is the newest row (`pustaka/docs/product/decision-log.html:1631-1632`); D-078 is claimed by the QA-inbox design and has no row, so the log holds seventy-nine entries (`decision-log.html:18`) and D-081 makes eighty.
**BRD rules this must obey:** BR-04 (append-only), BR-05 (closed catalog), BR-31 (every cost carries its basis through every aggregation, `pustaka/docs/product/brd.html:1283`), BR-38 (one live turn per product). No rule is added or changed.
**Security lens:** required — the change adds two back-office routes (`docs/rules/gaos-design-review.md`, the security criterion). It ran in round 1.

## 0. Glossary

- **Card / task** — one row of the task graph (`TaskRecord`, `dashboard/model.go:685-714`), drawn as one card on a product's board (`BoardCard`, `dashboard/board.go:39-58`). Task ids come from a store-wide workflow counter (`dashboard/store_mem.go:2073`), so they are unique across products; the timing still filters by product (§3.2).
- **Status** — closed vocabulary of eleven values (`dashboard/taskstate.go:32-46`). Legal changes: `taskTransitions` (`taskstate.go:79-95`). `DONE` and `CANCELED` have no exits (`taskstate.go:87,89`).
- **Move** — a status change. Since T-164 (v0.30.0, `pustaka/docs/changelog.html:546-556`) every status change writes one `TaskMoved` (`dashboard/events.go:60-66`) through `moveTaskLocked` (`dashboard/runner.go:430-444`), the only caller of `moveTask` (`taskstate.go:145`). Payload: `Task`, `Rule` (the action, `dashboard/board.go:130-152`), `Reason = "FROM→TO"`, `To`.
- **Plan event** — `TaskPlanned` (`events.go:56`), creating each listed task in `PENDING` (`dashboard/taskfold.go:34-53`).
- **Attempt / run** — one `TaskRunRecord` per attempt (`model.go:740-797`), `StartedAt`/`EndedAt` in unix seconds on the app clock (`runner.go:122,142`, `store_mem.go:1140-1141,1164`).
- **Runner mode** — `GAOS_RUNNER`: `off`, `on`, `manual` (`runner.go:49-60`). Under `manual` (D-080) only an operator's board dispatch moves `READY → DISPATCHED`; under `off` nothing dispatches (`runner.go:64-67`).
- **Projection** — `ArcadeStore` answers reads from an in-memory `MemStore` (`dashboard/store_arcade_blueprint.go:33-35`), rebuilt from the database on boot (`dashboard/store_arcade.go:1640-1800`).
- **Back office (BO)** — every route is session-gated unless listed public or bearer (`dashboard/auth.go:523-555`); `TestEveryRouteIsClassified` proves it for every registered pattern (`dashboard/auth_test.go:284-330`).

## 1. Problem

The product owner asked: *in each card, show the history of the elapsed time as the task moves from one status to another, and the total time to perform the task end to end until it is finished — including idle time when no AI is working on it at all.*

The board shows only a card's current status (`dashboard/templates/backoffice.html:395-405`, `dashboard/static/board.js:77-84`). The moves are recorded, but **the in-memory event has no wall-clock time**:

1. `Event.T` is `s.clock` (`store_mem.go:444`), a simulation counter (`store_mem.go:52`) only the test simulator advances (`dashboard/sim_test.go:86`). In production every event has `T = 0`.
2. The ArcadeDB row has a time: `writeEvent` stores `ts = sysdate()` (`store_arcade.go:1063-1066`; `Event.ts DATETIME mandatory`, `dashboard/schema/07-events.sql:28`). The boot load selects `seq, type, entityId, payload` only (`store_arcade.go:1745-1746`), so it is never read back.
3. `ts` is close to the move: a request's events are written in the request's transaction (`mutate` → `persist`, `store_arcade.go:443-480`); a runner's right after, by `persistBackground` (`store_arcade.go:251-256`). A background write that fails is **lost**, not retried — its records are written again later, its events are not (`store_arcade.go:236-249`).

## 2. Goals and non-goals

**Goals**

- G1. Per card: a chronological list of **status segments** — status, the status it came from, the move rule that entered it, entered-at, left-at, seconds in it.
- G2. Per card, totals that **partition** the lead time exactly (§3.2): AI working, waiting on the operator, queued, blocked, backlog; plus **idle = lead − AI working** ("no AI working on it").
- G3. Per attempt: duration, status, model, provider, tokens in/out, cost **with its basis** (BR-31).
- G4. Per workflow: lead, AI working, idle, cards, cards done (requested as "workflow-level totals too if cheap").
- G5. Board JSON: a `timing` summary on every card and a `workflows` list. A per-card JSON route with the full history (requested: "a per-card JSON route if a detail route is cheaper" — the segments are too heavy for every board read).
- G6. Board UI: a lead-time chip on every card, ticking while the card is open, that opens a **timing sheet**: totals, timeline, attempts, workflow line.
- G7. Cards whose history is complete — every card planned on v0.30.0 or later, minus any lost background write — get timings on the live instance with no migration (§3.1). A card whose history is incomplete says so (§3.2 step 5).

**Non-goals**

- N1. No new event type (BR-05), task status, schema statement, Go dependency or vendored JS.
- N2. No change to the fold (`foldTasks`), the floor reducer, or `Event.T`.
- N3. No edit of any stored event (BR-04).
- N4. No business-hours calendar: elapsed is wall-clock.
- N5. No historical runner mode: `READY` is classified by the current `GAOS_RUNNER` (L2).
- N6. No charts. Numbers and a list.
- N7. No per-account product scoping (SEC-4; see §3.6).
- N8. No deploy and no VM change.

## 3. Design

### 3.1 A wall-clock time on every event — a field, not an event type

`Event` (`model.go:193-201`) gains `At int64` (unix seconds, app clock; 0 = unknown). No JSON tag: like every other `Event` field it appears in `GET /events` as `"At"` (`dashboard/handlers_tasks.go:36-43`) — an additive change to that response, stated here.

- `MemStore.emit` (`store_mem.go:444`) sets `At: time.Now().Unix()`.
- `eventPayloadWire` (`store_arcade.go:1038-1043`) gains `At int64 \`json:"at,omitempty"\``; `writeEvent` (`store_arcade.go:1057-1059`) fills it from `e.At`. `ts = sysdate()` stays: it is the database's own write time and is what the `Event (tenantId, ts)` index covers (`pustaka/docs/architecture/graph.html:169`); overwriting it with the app's clock would change a column's meaning for one reader.
- The boot load (`store_arcade.go:1739-1767`) selects `ts` too. `At = pw.At` when non-zero, else `parseStamp(ts) + dbOffset` (`parseStamp`, `store_arcade.go:1889-1899`).
- **`dbOffset`** reconciles the database clock with the app clock for old rows. Once per load: `SELECT sysdate() AS now` and `tzOffset(parseStamp(now), time.Now().Unix())`, a pure helper that returns the difference **rounded to the nearest 900 s** (a zone offset, never a seconds skew). On the scratch image the container runs UTC and `ts` reads as UTC (measured: `ts` `"2026-11-20 22:41:58.994"` at `date -u` 22:41:58), so the offset there is 0; a server in another zone gets its offset removed. If the probe query fails, `dbOffset = 0` and the load proceeds (a timing is display, not state). `TaskRun.startedAt` does **not** share this question — it is written by the app as UTC (`unixStamp`, `store_arcade.go:1905`); `Artifact.createdAt` does, and keeps its current behaviour (out of scope).

BR-05 closes event **types** (`events.go:29-31`); a payload-wire key is how `Clock` was added (`store_arcade.go:1041`). BR-04 forbids rewriting a stored event; nothing is rewritten.

### 3.2 The pure function

New file `dashboard/timing.go`:

```go
func cardTiming(t TaskRecord, events []Event, runs []TaskRunRecord, now int64, mode string) CardTiming
```

Pure: reads its arguments, modifies none. `t` supplies the id, the product (`t.DeskID`) and the **current status**. Events are visited in `Seq` order (the fold's order, `taskfold.go:30-31`); only events with `DeskID == t.DeskID` count.

1. **Effective time.** Each counted event's time is `eff = max(e.At, previous eff)` — a running maximum, so a clock that steps back never produces a negative or overlapping interval and the partition stays exact.
2. **Opening.** The first `TaskPlanned` whose `Payload.Tasks` lists `t.ID`: `createdAt = eff`, first segment `{status: PENDING, from: "", rule: "plan", enteredAt: createdAt}`. If there is none, the first `TaskMoved` naming the task opens the history: segment `{status: <FROM half of Reason>, from: "", rule: "", enteredAt: eff, leftAt: eff, seconds: 0}`, `createdAt = eff`, and the move itself is then applied by step 3; the timing is marked `complete = false` with reason `"no plan event for this card"`.
3. **Each `TaskMoved` with `Payload.Task == t.ID` and `Payload.To != ""`:** close the open segment (`leftAt = eff`, `seconds = leftAt − enteredAt`), open `{status: To, from: <previous status>, rule: Payload.Rule, enteredAt: eff}`. A move to the current status is skipped. All other event types are skipped.
4. **The last segment.** Status `DONE` or `CANCELED`: finished, `finishedAt = enteredAt`, `seconds = 0`, `open = false`. Otherwise `open = true`, `leftAt = 0`, `seconds = max(0, now − enteredAt)`.
5. **Known and complete.**
   - `known = false` if any counted event has `At == 0`; then every number is 0, segments keep status/from/rule, and `reason = "no time recorded for this card's history"`.
   - `complete = false` if the history's last status differs from `t.Status`, with `reason = "the history ends in <X> but the card is <Y>; a move was not recorded"` (pre-v0.30.0 cards, a lost background write — §1.3). An incomplete timing is still computed from what exists, but `status` is always `t.Status`, and the chip and the sheet print the reason instead of a lead.
   - No events at all for the task: `known = false`, `reason = "no history for this card"`.
6. **Lead** = `(finishedAt or now) − createdAt` (0 when not known). When the card is open and `now < enteredAt` (clock behind), the open segment is 0 and lead is clamped to the sum.
7. **Buckets.** Each segment's seconds go to exactly one bucket by status and `mode`:

   | `bucket` value | JSON total | Statuses | Why |
   |---|---|---|---|
   | `ai` | `aiSeconds` | `DISPATCHED`, `RUNNING` | the Working column (`board.go:102`): a turn is in flight |
   | `operator` | `waitingOperatorSeconds` | `AWAITING_INPUT`, `REVIEW`, `FAILED`; `READY` when mode is `manual` or `off` | only an operator acts next: answer, approve/redo, retry, dispatch (`board.go:155-187`) — or, under `off`, turn the runner on |
   | `queued` | `queuedSeconds` | `READY` when mode is `on` | the scheduler dispatches it |
   | `blocked` | `blockedSeconds` | `BLOCKED`, `DEFERRED` | the runner, the design gate, an expired question (D-066, `taskstate.go:90-93`) or the budget guard holds it; leaving it may still need the operator's unblock (L7) |
   | `backlog` | `backlogSeconds` | `PENDING` | dependencies not met |
   | `done` | — | `DONE`, `CANCELED` | terminal, always 0 |

   **Invariant (tested, no exceptions):** when `known`, `ai + operator + queued + blocked + backlog == leadSeconds`. `idleSeconds = max(0, leadSeconds − aiSeconds)`.
8. **Attempts** (§3.4 for the type): one `AttemptTiming` per run of this task, sorted by `Attempt`. `seconds = EndedAt − StartedAt` when ended; `now − StartedAt` when `EndedAt == 0` and status is `STARTED` or `RUNNING`; else 0. Floored at 0. Card totals: `attempts`, `tokensIn`, `tokensOut`, and cost **per basis**: `costMeasuredUsd`, `costEstimatedUsd`, and `costUnavailable` (count of attempts whose basis is unavailable) — never one blended total (BR-31; bases from `model.go:779`).

**AI working is the Working-column time, not Σ run durations.** The run record is written just before the move to `RUNNING` (`runner.go:122-142` vs `runner.go:157`); the two differ by seconds. Segments keep the partition exact; per-attempt durations are shown beside it, not added into it.

### 3.3 Workflow totals

```go
func workflowTiming(id string, cards []CardTiming, now int64) WorkflowTiming
```

Over one workflow's cards (`TaskRecord.WorkflowID`): `startedAt = min(createdAt)`; `finished` when every card is finished, then `finishedAt = max(finishedAt)`; `leadSeconds = (finishedAt or now) − startedAt`; `aiSeconds = Σ aiSeconds`; `idleSeconds = max(0, lead − ai)`; `cards`; `done` (status `DONE`). Σ is a union because a product has at most one live turn (BR-38, enforced at `board.go:380-383,412-416`) and a workflow is one product's. Any card not `known` or not `complete` makes the workflow `known = false`.

### 3.4 Types (Go, with JSON tags)

```go
type CardTimingSummary struct {
	Known       bool   `json:"known"`
	Complete    bool   `json:"complete"`
	Reason      string `json:"reason,omitempty"`
	Status      string `json:"status"`
	Finished    bool   `json:"finished"`
	CreatedAt   int64  `json:"createdAt"`
	FinishedAt  int64  `json:"finishedAt,omitempty"`
	AsOf        int64  `json:"asOf"`
	LeadSeconds int64  `json:"leadSeconds"`
	AISeconds   int64  `json:"aiSeconds"`
	IdleSeconds int64  `json:"idleSeconds"`
	WaitingOperatorSeconds int64 `json:"waitingOperatorSeconds"`
	QueuedSeconds  int64 `json:"queuedSeconds"`
	BlockedSeconds int64 `json:"blockedSeconds"`
	BacklogSeconds int64 `json:"backlogSeconds"`
	Attempts         int     `json:"attempts"`
	TokensIn         int64   `json:"tokensIn"`
	TokensOut        int64   `json:"tokensOut"`
	CostMeasuredUSD  float64 `json:"costMeasuredUsd"`
	CostEstimatedUSD float64 `json:"costEstimatedUsd"`
	CostUnavailable  int     `json:"costUnavailable"`
	LeadText  string `json:"leadText"` // relSpan(lead), or "—" when not known/complete
	TitleText string `json:"title"`    // "Lead 1h · AI 22m · idle 38m — open the timing history", or the reason
}

type TimingSegment struct {
	Status    string `json:"status"`
	From      string `json:"from"`
	Rule      string `json:"rule"`
	EnteredAt int64  `json:"enteredAt"`
	LeftAt    int64  `json:"leftAt"` // 0 while open
	Seconds   int64  `json:"seconds"`
	Open      bool   `json:"open"`
	Bucket    string `json:"bucket"` // ai | operator | queued | blocked | backlog | done
}

// AttemptTiming is an allowlist, built field by field from TaskRunRecord.
type AttemptTiming struct {
	Attempt   int     `json:"attempt"`
	Status    string  `json:"status"`
	Model     string  `json:"model"`
	Provider  string  `json:"provider"`
	StartedAt int64   `json:"startedAt"`
	EndedAt   int64   `json:"endedAt,omitempty"`
	Seconds   int64   `json:"seconds"`
	TokensIn  int64   `json:"tokensIn"`
	TokensOut int64   `json:"tokensOut"`
	CostUSD   float64 `json:"costUsd"`
	CostBasis string  `json:"costBasis"`
}

type CardTiming struct {
	CardTimingSummary
	TaskID   string          `json:"taskId"`
	Segments []TimingSegment `json:"segments"`
	Runs     []AttemptTiming `json:"runs"`
	Workflow *WorkflowTiming `json:"workflow,omitempty"`
}

type WorkflowTiming struct {
	ID          string `json:"id"`
	Known       bool   `json:"known"`
	Cards       int    `json:"cards"`
	Done        int    `json:"done"`
	Finished    bool   `json:"finished"`
	StartedAt   int64  `json:"startedAt"`
	FinishedAt  int64  `json:"finishedAt,omitempty"`
	LeadSeconds int64  `json:"leadSeconds"`
	AISeconds   int64  `json:"aiSeconds"`
	IdleSeconds int64  `json:"idleSeconds"`
}
```

`BoardCard` gains `Timing *CardTimingSummary \`json:"timing,omitempty"\``; `Board` gains `Workflows []WorkflowTiming \`json:"workflows"\`` (never null). The key is `timing`, not `tasks`: sisflow passes unknown card fields through (`dashboard/static/vendor/sisflow/board/core/shapes.js:67-70`) and overwrites `tasks` (`shapes.js:82`).

### 3.5 Store seam and cost

`Store` (`dashboard/store.go:49`) gains `CardTiming(ctx, productID, taskID string) (CardTiming, error)`. `MemStore.CardTiming` takes `s.mu.RLock()`, finds the task (`DeskID == productID`) else `ErrNotFound`, and computes the card, its runs and its workflow; `ArcadeStore.CardTiming` forwards to `s.proj` (as `Board` does, `store_arcade_blueprint.go:33-35`); `readOnly` embeds `Store` (`store.go:240`) and needs nothing.

`MemStore.Board` (`board.go:201-212`) reads `now` once and, after `boardOf` and `decorateBoardQa`, calls `func (s *MemStore) decorateBoardTiming(b *Board, productID string, now int64)` (a method, in `timing.go`, beside the pure functions). `boardOf` stays pure (TC-308 compares through it).

**Cost (SEC-3).** The event stream is unbounded and spans every product. The timing never rescans it: `MemStore` keeps a **per-task index** `timingIdx map[string][]int` (positions in `s.events` of `TaskPlanned` events listing the task and `TaskMoved` events naming it) with a high-water mark `timingN`, under its own `sync.Mutex`. A read (already under `s.mu.RLock`, so `s.events` cannot change) locks the index, extends it from `timingN` to `len(s.events)` and unlocks — amortised O(new events), whoever appended them (`emit`, `store_mem.go:448`, or the Arcade load, `store_arcade.go:1760`). If `len(s.events) < timingN` the index is rebuilt. Runs are grouped by task in one pass over `s.runs` per board read (O(runs)). A board read is then O(cards × their own events + runs). The sizes on prod are not measured from here (assumption).

### 3.6 The per-card JSON route

`GET /products/{id}/board/cards/{taskId}/timing` → 200 with `CardTiming`. Unknown product or task → 404 `{"error":"no such card"}`. Any other store error → 500 with the fixed body `{"error":"timing unavailable"}` and the cause logged server-side (`log.Printf("card timing %s/%s: %v", id, taskID, err)`) — not `err.Error()` in the body (SEC-5).

- **Exposure (SEC-1).** Per-attempt model, provider, tokens and cost are **new on the HTTP surface**: no handler serves runs today (`Store.Runs`'s callers are the stores, `store.go:68`). They go out only through `AttemptTiming` (§3.4), an explicit allowlist built field by field. `TaskRunRecord`'s `SessionID`, `RequestID`, `LogRef`, `Workspace`, `SliceDigest`, `FailureSignature`, `Invocation`, `BlueprintVersion` and `ExitCode` are **never** copied into it, into the summary, or into the sheet; TC-472 and TC-475 assert it with a seeded session and request id.
- **Auth.** Registered with `handle(...)` in `routes()` (`dashboard/main.go:375-380`): back-office class, session verified by `gate` (`auth.go:541-551`, `sessionFrom` `auth.go:361-382`); the handler also calls `s.requireBOJSON` (`handlers_tasks.go:151-157`). Both new GET patterns are added to `readOnlyGET` in `dashboard/auth_test.go:258-272`, so `TestEveryRouteIsClassified` proves they are read-only and answer 401 signed-out and with a forged cookie. GET only: no CSRF surface.
- **Scope (SEC-4).** Single trust domain: every back-office account sees every product, as for `GET …/board` today (`handlers_board.go:32-36`). No open question covers per-account scoping; a design that adds it must include these two routes.

### 3.7 The board UI

**The chip.** Every card gets one `<button type="button" class="tm-chip mono" data-timing-open="…">`:
- URL: server board — built in Go as `"ui/timing/" + url.PathEscape(productID) + "/" + url.PathEscape(taskID)` (the answer sheet's pattern, `handlers_bo.go:678`), passed to the template as a template func `timingURL`; sisflow — `"ui/timing/" + encodeURIComponent(product) + "/" + encodeURIComponent(card.id)` (as `board.js:28`). SEC-6.
- Text: `timing.leadText` (`relSpan`, `handlers_bo.go:589-599`: `<1m`, `12m`, `5h`, `9d` — the console's existing format). `title` and `aria-label`: `timing.title`.
- **Ticking.** An open, known, complete card's chip holds `<time data-elapsed-since="{createdAt RFC3339 UTC}">{leadText}</time>`. `static/live.js`'s existing `tick()` (`live.js:176-191`, every 30 s and on `htmx:afterSettle`) gains one selector: `time[data-elapsed-since]` → `span(now − t)` with **no prefix** (the existing `data-since` keeps its "waiting " prefix untouched). `span` is the JS twin of `relSpan` (`live.js:164-175`), so a server render and a tick agree. A finished card's chip is static. No live dot.
- sisflow card: built with `textContent` only (as `qaBadge`, `board.js:52-75`), appended to the card face, and stops `Enter`/`Space`/`F2`/`Delete`/`Backspace` propagation as `needsBadge` does (`board.js:30-34`). The chip is a button, so sisflow's drag ignores it (`vendor/sisflow/board/dnd/pointer.js:7`).

**The sheet.** `GET /ui/timing/{id}/{taskId}` (back-office; `s.requireBO`) calls `Store.CardTiming`, the product name (`Store.Products`) and the task title (the `Store.Board` card label), builds `TimingView`, and renders template `timing-modal`. Unknown card: `s.fail(w, r, ErrNotFound)` (`main.go:496-506`: 404 and the toast "That record is gone."). The template, in `needs.html` beside `answer-modal`:

```html
{{define "timing-modal"}}
<div class="modal-back modal-anim" id="timingModal" {{if not .Timing}}hidden{{end}} hx-swap-oob="true">
  {{with .Timing}}
  <div class="modal timing" tabindex="-1" role="dialog" aria-modal="true" aria-labelledby="timingTitle">
  … header (eyebrow "{product} · {taskId} · {status}", h3#timingTitle = task title, close button data-close-modal="timingModal") …
  … body: totals, created/finished line, timeline, attempts, workflow …
  </div>
  {{end}}
</div>
{{end}}
```

`page.html` includes it beside `answer-modal` (`templates/page.html:68-69`), so with `.Timing == nil` it renders an empty hidden `div`.

```go
type TimingView struct {
	ProductName, TaskID, TaskTitle, Status string
	Known, Complete bool
	Reason string
	Totals []TimingTotal  // Lead, AI working, Idle — no AI working on it, Waiting on you, Queued, Blocked, Backlog
	Created, Finished string // "2026-11-20 16:10 UTC"; Finished "" while open
	AsOf string
	Segments []TimingRow
	Attempts []AttemptRow
	Workflow string // "wf-8: 8/8 cards done · lead 1h 55m · AI 41m · idle 1h 14m"; "" when not known
	Cost string     // "$1.84 measured · $0.20 estimated · 1 unavailable", parts with zero omitted
}
type TimingTotal struct{ Label, Value string; Zero bool }
type TimingRow struct{ Status, From, Rule, Entered, EnteredISO, Elapsed string; Open bool }
type AttemptRow struct{ Attempt int; Status, Model, Provider, Duration, Tokens, Cost string }
```

Durations in the sheet use `fmtDur` — finer than `relSpan` on purpose, because a timeline row of "<1m" next to another tells nothing: `0s`, `45s`, `12m 05s` under an hour, `1h 05m` under two days, `2d 3h` beyond. Absolute times are `2006-01-02 15:04 UTC`; `EnteredISO` is RFC 3339 for `<time datetime>`. All text goes through `html/template` escaping; no `template.HTML` or `template.CSS` is built. The proportion bar of v1 is dropped (N6).

Content, top to bottom: the totals (a `dl` grid; zero ones dimmed with `.tm-zero`); "Created … · finished … | still open, measured at …"; the reason, if any; the timeline (`ol`, one `li` per segment: status, "from X by rule", entered, elapsed; the open one says "now — {elapsed} so far"); the attempts table, or "No attempt has run yet."; the cost line; the workflow line.

**app.js.** The answer sheet's code stays byte-identical except two id checks. A new branch in the click delegate (`app.js:40`, selector gains `[data-timing-open]`) sets `trigger` exactly as `openAnswer` does (`app.js:153-164`) and calls `htmx.ajax("GET", url, {target:"#sink", swap:"innerHTML"})`. `afterSwap` (`app.js:309-312`) calls the existing `showAnswer(target)` for `timingModal` too — it already handles a modal with no form (`syncAnswer` returns without `#answerForm`, `app.js:190-191`; focus falls back to `.modal`, `app.js:181-183`). `closeModal` (`app.js:116-127`) returns focus for `timingModal` as for `answerModal`. Esc, the Tab trap and the `.modal-anim` enter/exit are inherited; no new motion.

**CSS.** A `tm-` block in `static/app.css` (`.tm-chip`, `.tm-totals`, `.tm-zero`, `.tm-line`, `.modal.timing`) using existing variables only (`--ink-2`, `--ink-3`, `--line`, `--panel-2`, `--run`, `--font-mono`, `--r-s`); no new colour.

### 3.8 Phases, each with its exit check

| Phase | Changes (§4 #) | Exit |
|---|---|---|
| P1 event time | 1–3 | `go test ./...` green; with `GAOS_ARCADE_TEST_URL` set, `go test -run 'Conformance|EventTime' ./...` green |
| P2 timing and seam | 4–7, 18 (pure tests), 19 | `go test -race ./...` green |
| P3 routes and UI | 8–17, 24 | `go vet ./... && go test -race ./...` green; the board rendered locally with the in-memory store shows the chip and the sheet |
| P4 registers and release | 20–23 | `cd pustaka && go run . check ./docs && go test ./...`; `./deploy/check-workspace.sh`; `./deploy/check-stacks.sh`; `grep -rn "four hundred and" …` shows one number |

The tests and the register rows land in the same commit (`.claude/rules/qa-test-plan.md`).

### 3.9 D-081 (the decision row)

**Title:** *Card timing is derived from the event stream: every event carries the app's wall-clock time, and older events read the database's `ts`.*
**Decision:** `Event` gains `At` (unix seconds) on the payload wire (`at`); rows written before v0.41.0 read `Event.ts` corrected by the database's zone offset. A card's timing is a pure function of its `TaskPlanned`/`TaskMoved` events and its runs: status segments that partition the lead time into AI working (`DISPATCHED`+`RUNNING`), waiting on the operator, queued, blocked and backlog; idle is lead minus AI working; costs stay per basis (BR-31). Wall-clock, not business hours. **Rejected:** a new event type or a `Task.enteredAt` column (BR-05, a second source of truth); reusing `Event.T` (the simulation clock replay carries); AI time as Σ run durations (does not partition the lead). Source: `docs/designs/card-timing.md`.

## 4. Change list

| # | File | Change |
|---|---|---|
| 1 | `dashboard/model.go` | `Event.At int64` |
| 2 | `dashboard/store_mem.go` | `emit` sets `At`; `timingIdx`/`timingN`/its mutex on `MemStore`; `MemStore.CardTiming` |
| 3 | `dashboard/store_arcade.go` | wire `at`; `writeEvent` writes it; load selects `ts`, probes `sysdate()`, sets `At` |
| 4 | `dashboard/store_arcade_blueprint.go` | `ArcadeStore.CardTiming` forwards |
| 5 | `dashboard/store.go` | `CardTiming` on `Store` |
| 6 | `dashboard/timing.go` (new) | types; `cardTiming`, `workflowTiming`, `tzOffset`, `fmtDur`; the index; `decorateBoardTiming`; `buildTimingView` |
| 7 | `dashboard/board.go` | `BoardCard.Timing`, `Board.Workflows`; `Board()` calls `decorateBoardTiming` |
| 8 | `dashboard/handlers_board.go` | `handleCardTiming` |
| 9 | `dashboard/handlers_bo.go` | `handleTimingSheet` |
| 10 | `dashboard/main.go` | two `handle(...)` lines |
| 11 | `dashboard/view.go`, `dashboard/render.go` | `View.Timing *TimingView`; template func `timingURL` |
| 12 | `dashboard/templates/needs.html` | `timing-modal` |
| 13 | `dashboard/templates/page.html` | include `timing-modal` |
| 14 | `dashboard/templates/backoffice.html` | the chip on the server board |
| 15 | `dashboard/static/board.js` | the chip on the sisflow card |
| 16 | `dashboard/static/app.js` | open / show / focus-return for `timingModal` |
| 17 | `dashboard/static/app.css` | the `tm-` block |
| 18 | `dashboard/timing_test.go` (new) | §5 |
| 19 | `dashboard/store_conformance_test.go` | `storeContract` case `CardTimingOfAPlannedTask` |
| 20 | `pustaka/docs/product/qa-test-plan.html`, `pustaka/docs/assets/toc/product.js`, `.claude/rules/qa-test-plan.md` | rows, `h3`s, counts, TC-124/125 prose, the rule's highest ID, totals and range map (and its stale "four hundred and sixty" sentence) |
| 21 | `pustaka/docs/product/decision-log.html`, `pustaka/docs/assets/toc/product.js` | D-081 (§3.9); seventy-nine → eighty |
| 22 | `pustaka/docs/architecture/graph.html` | the `Event` paragraph: the `at` key, `ts` as fallback, the board's timing read |
| 23 | `pustaka/docs/changelog.html`, `pustaka/docs/assets/toc.js` | v0.41.0 note; `product.semver` 0.41.0 |
| 24 | `dashboard/auth_test.go`, `dashboard/static/live.js` | two `readOnlyGET` entries; the `data-elapsed-since` tick |

Changelog sentence: *"Every card on the board now shows how long it has taken, and its timing sheet shows each status it passed through, how long it stayed, which move took it there, the attempts with their tokens and cost, and how much of the time no AI was working on it (D-081)."*

## 5. Tests and the register

New `func Test…` in `dashboard/timing_test.go`, one register row and one `h3` each (quoting its `t.Fatalf`), TC-466 onward:

| TC | Test | Kind | Asserts |
|---|---|---|---|
| TC-466 | `TestCardTimingFollowsEveryMove` | Positive | PENDING→READY→DISPATCHED→RUNNING→AWAITING_INPUT→READY→DISPATCHED→RUNNING→REVIEW→DONE at fixed times: ten segments, exact from/rule/enteredAt/leftAt/seconds, finished, lead |
| TC-467 | `TestCardTimingBucketsPartitionTheLeadTime` | Positive | the five buckets sum to the lead; idle = lead − AI; READY is `operator` under manual and off, `queued` under on |
| TC-468 | `TestCardTimingAnOpenCardRunsToNow` | Positive | the last segment is open and measured to `now`; lead = now − created |
| TC-469 | `TestCardTimingWithoutTimesIsUnknownAndAClockStepCannotBreakThePartition` | Negative | an event with `At == 0` → `known=false`, numbers 0; a backwards step (`At` 0,100,50,200) keeps the partition exact and idle ≥ 0 |
| TC-470 | `TestCardTimingIgnoresOtherTasksAndProducts` | Negative | another task's moves, the same id in another product, and non-move events change nothing |
| TC-471 | `TestCardTimingThatDisagreesWithTheCardIsIncomplete` | Negative | history ends READY, card is DONE → `complete=false`, the reason names both; status is the card's; no plan event → `complete=false` |
| TC-472 | `TestCardTimingAttemptsCarryOnlyTheAllowedFields` | Positive / Negative | per-attempt seconds, tokens, cost and basis; per-basis cost totals; a running attempt measured to now; the JSON holds no session id, request id, log ref or workspace |
| TC-473 | `TestWorkflowTimingTotals` | Positive | min created, max finished only when all are finished, Σ AI, idle; an unknown card makes the workflow unknown |
| TC-474 | `TestTheTimingRoutesAnswer404ForAnUnknownCard` | Negative | JSON route → 404 `{"error":"no such card"}`; the sheet → 404 with the toast "That record is gone." |
| TC-475 | `TestTheBoardAndTheSheetCarryTheTiming` | Positive / Negative | board JSON: `timing` on every card and `workflows`; the JSON route: `segments`; the sheet: `id="timingModal"`, a row per segment, the chip with `data-timing-open`; neither carries a seeded session or request id |
| TC-476 | `TestTheDatabaseZoneOffsetRoundsToAQuarterHour` | Positive / Negative | `tzOffset` maps a 7 h difference plus 40 s of skew to 25 200, and 40 s alone to 0 |
| TC-477 | `TestEventTimeSurvivesTheGraphRoundTrip` | Positive | ArcadeDB (skips without `GAOS_ARCADE_TEST_URL`): `At` reads back equal after a reopen; a row stored with no `at` reads its `ts` |

One `storeContract` case `CardTimingOfAPlannedTask` (a task from `CreateWorkflow`: `known`, `complete`, `createdAt > 0`, first segment `PENDING`/`plan`; `ErrNotFound` for an unknown id), covered by TC-124/TC-125 whose prose names it. `TestEveryRouteIsClassified` gains two table entries, no new assertion; its row does not change.

Counts: 465 + 12 = **477** ("four hundred and seventy-seven", four places); Go tests 341 + 12 = **353** ("three hundred and fifty-three"), `dashboard/` 319 → 331; next free TC-478.

## 6. Accepted limits

- L1. Wall-clock (N4): a card left overnight shows the night as waiting on you. That is what was asked.
- L2. `READY` follows the current runner mode, and under `manual` a READY card in a product with a live turn is counted `operator` although BR-38 is what holds it.
- L3. A background write that fails loses its events (§1.3); the card then reads `complete = false` with the reason — it is not hidden.
- L4. Cards from before v0.30.0 have no `To` on their moves and read `complete = false`.
- L5. Timing is computed on read, not stored (D-001: no second source of truth).
- L6. A dashboard crash during a turn leaves the card `RUNNING` until boot recovery requeues it (`turns.go:237`); the downtime counts as AI working.
- L7. `BLOCKED` covers both machine holds and states the operator must unblock (an expired question); it is one bucket.
- L8. When events move to cold archive after 90 days (`pustaka/docs/architecture/graph.html:213`, not built), timings of older cards will read `known = false`.
- L9. The sheet is a snapshot; the chip ticks.

## 7. Alternatives considered

- **A `TaskTimed` event or a `Task.enteredAt` column.** Rejected: BR-05, a schema statement, a second source of truth.
- **Use `Event.T`.** Rejected: the simulation clock carried in snapshots (`store_arcade.go:1041,1102`).
- **AI time = Σ run durations.** Rejected (§3.2).
- **Write `ts` from `e.At` instead of a wire key.** Rejected: it changes what an existing column means, and old rows still need the offset; the wire key costs one field.
- **Timeline inline on the card.** Rejected: 140 px cards (`backoffice.html:390`), and sisflow owns the face.
- **No JSON detail route** (zoom-out, round 1). Contested: the request asks for the history as JSON per card, and the segments are too heavy for every board read.
- **No workflow totals** (zoom-out, round 1). Contested: the request asks for them; they cost one function over data already computed.

## 8. Risks

- R1. Read cost: bounded by the index (§3.5).
- R2. Clock steps: the running maximum keeps the partition exact (§3.2 step 1).
- R3. A strict JSON client of the board: the only client is `board.js`; the fields are additive.
- R4. app.js: the answer sheet's functions are unchanged apart from two id checks; the manual check in the TC-475 prose is "open a timing sheet, Esc, focus returns to the chip; then open and send an answer".

## 9. Rollout

v0.41.0. No migration. Rollback = revert; an older build ignores the `at` key (`eventPayloadWire` decoding ignores unknown keys).

## 10. Open questions

None blocking. The live check (p-6 `wf-8`, 8 DONE cards, 2026-11-20 16:10–18:05 UTC) is the operator's after deploy; this change deploys nothing.

## 11. Approval

Approved 2026-11-21, v2, by the orchestrating agent under the product owner's standing instruction of 2026-11-20/21 to work among the agents without asking for confirmation (approval by delegation). Basis: round 1 raised no Blocker; the one High security finding (SEC-1, run records on the HTTP surface) is fixed by the `AttemptTiming` allowlist and two negative assertions; every Major is fixed or contested with a checkable reason in `reviews/card-timing-review-1.md`. No second round: no Blocker survives (policy step 7).
