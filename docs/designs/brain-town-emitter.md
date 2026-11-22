# Brain Town emitter — GAOS tells a public town when work changes

**Status:** Approved (round 1 triaged, approved by delegation — see `reviews/brain-town-emitter-review-1.md`)
**Version target:** v0.46.0 · **Decision:** D-086 · **Register rows:** TC-513 … TC-523 (see §4)

## 1. Problem

Brain Town is a public, animated, third-party web service ("the receiver", reached at an
operator-supplied URL such as `https://town.example.org/ingest/gaos/<integration-id>`; the real URL
lives only in the deploying shell, never in this repository — D-085): each GAOS product is a hall, each
task a person who queues, waits at the design gate, is worked by an agent and leaves when done. The
receiver never calls GAOS; GAOS **pushes** one HTTP POST per state change. GAOS has no such outbound
feed today; its only outbound calls go to the QA tools (`dashboard/qaclient.go:46`,
`dashboard/inbox_worker.go`).

### Receiver contract (copied here so this design stands alone; it comes from the integration brief)

- Request: `POST <url>`, `Authorization: Bearer <token>`, `Content-Type: application/json`,
  `Idempotency-Key: <event id>`.
- Body: `{id, type, at, product{key,name,visibility}, task{id,title,kind,column}, data}`; `task` is omitted
  for `product.upserted`. `id` is unique per event and stable across retries (ULID or UUID); `at` is UTC
  RFC 3339; `visibility` is `public` or `hidden`; for `hidden` the receiver shows an alias and never shows
  task titles.
- Types and `data`: `product.upserted`; `task.created`; `task.moved {from,to}`; `gate.requested`;
  `gate.decided {decision: approved|rejected}`; `task.claimed {agent}`; `task.qa {passed,failed,total}`;
  `task.done`; `task.failed {reason}`.
- Responses: `202` accepted; `401` bad token; `409` already accepted (= success); `422` invalid body;
  `429` slow down.
- Delivery rules: after commit, from a background worker; 3 s timeout; retries 1 s, 5 s, 30 s on network
  error, `429`, `5xx`, none on other `4xx`; order kept per task; one log line per failed delivery (id +
  status); the token never in a log or error.
- `column` values the receiver accepts are free strings; GAOS sends its board column ids (`backlog`,
  `ready`, `working`, `needs-you`, `review`, `blocked`, `failed`, `done`, `canceled`; `board.go:114-124`).
  *(Assumption: the receiver does not validate `column` against a fixed list. Stated in the operator
  summary so the receiver's author can confirm.)*

**Deliberate deviation from the brief.** The brief says "send the real values; hiding happens on the
Brain Town side". Security review SEC-1 (Critical) showed that makes "hidden" a label on data that has
already left GAOS. Here a hidden product is redacted **before** it leaves (§3.3); the receiver still gets
`visibility: hidden` and everything it needs to draw the person.

## 2. What exists (anchors)

- Every state change is an `Event` (`dashboard/model.go:193`), emitted by `MemStore.emit`
  (`store_mem.go:436`) and by `appendPreparedLocked` (`conversation.go:728`); both append to `s.events`
  and `Publish` to the broker. Subscribers receive the **full** event including `Payload`;
  `Store.Events()` strips payloads (`store_mem.go:687-696`), so it cannot drive a mapper.
- On `ArcadeStore`, `emit` runs **before** persist and a failed persist does not remove the event from the
  projection (`store_arcade.go:443-455`). So neither the broker nor `Events()` can say "committed".
  `persist(ctx, r)` is the single function both write paths use — `mutate` (`store_arcade.go:443`) and
  `persistBackground` (`store_arcade.go:253`) — and after `tx.Commit` it holds `r.NewEvents`, complete with
  payloads (`store_arcade.go:~527-530`).
- Task status changes go through `moveTaskWithLocked` (`runner.go:445`): `TaskMoved` with
  `Payload{Task, Rule, Reason "FROM→TO", To, Actor}` and `DeskID` = product id (`runner.go:462-466`).
  Rules: `schedule`, `start` (`board.go:180-181`) and gate rules `design-delivered`, `review-cleared`,
  `design-blocked`, `review-failed` (`designgate.go:462-465`). `review-cleared` is **also** the rule on each
  implementation task the gate releases (`designgate.go:807`); `review-failed` also covers "design
  missing" and an unparseable review, which D-068 clause 7 says are not verdicts (`designgate.go:735-753`).
- Task creation is `TaskPlanned`, `Payload.Tasks []PlannedTask{ID, Title, Kind, …}` (`model.go:999`).
  Product creation is `ProductCreated` (`store_mem.go:1765`). QA results are one `RecordUpdated` per batch
  naming no task (`qacatalog_store.go:254`).
- `columnOf(status)` maps a status to a board column (`board.go:142`). `TaskRecord` has `Title, Role, Kind,
  Status, WorkflowID` (`model.go:725`); a missing kind means `implement` (`model.go:734-738`).
- `serve()` runs boot reconcile (`main.go:240`), the scheduler (`:261`) and the question sweep (`:283`)
  before the inbox worker (`:305`).
- Secrets are files named by an env var; `readTokenPath` (`api_v1.go:110`) accepts only `gaos_` tokens, and
  `newAPIConfig` refuses equal tokens (`api_v1.go:139-143`). The outbound URL rule is `validQaURL`
  (`qaclient.go:63`). `urlHost` (`integrations.go:39`) renders a host only.
- Closed vocabularies (`.claude/rules/rule-index.md:38-44`): no event type is added by this change.

## 3. Design

### 3.1 Source: a post-commit feed

New optional interface in `braintown.go`:

```go
type commitFeed interface{ SetCommitTap(func(Event)) }
```

- `ArcadeStore.SetCommitTap` stores the callback; `persist` calls it for each `r.NewEvents` element
  **after** `tx.Commit` and `commitStage()` succeed, so a failed persist never reaches it. It runs under the
  store lock, therefore the callback only does a non-blocking channel send (cap 2000); on a full channel it
  increments a counter and returns. Both write paths use `persist`, so one call site covers request and
  background writes.
- `MemStore.SetCommitTap` stores the callback; `emit` and `appendPreparedLocked` call it where they
  `Publish`. A memory store has no commit, so emit is commit. `ArcadeStore` does **not** register on its
  projection, so an event is never tapped twice.
- The tap is registered in `serve()` **before** boot reconcile (`main.go:240`), so boot-time events are
  seen. The worker goroutine starts next to the inbox worker (`main.go:305`) and drains the channel.
- Enabled only when `BRAIN_TOWN_ENABLED` is true **and** configuration is valid. Otherwise no tap, no
  goroutine, no request. No `Events()` call is made per event.

No schema change, so there is **no migration**. State is in memory: events not yet sent when GAOS stops are
lost (non-goal).

### 3.2 Mapper — `mapEvent(Event, taskInfo) []out`

A closed list of fields is read from a GAOS event: `Type, DeskID, At, Seq`, and
`Payload.{Task, To, Rule, Tasks[].{ID,Title,Kind}}`. **`Payload.Actor`, `Event.Text`, `Reason` and
`BlockReason` are never read** (they can hold an operator's account name or free text).

| GAOS event | Condition | Brain Town event(s) | `data` |
|---|---|---|---|
| `ProductCreated` | — | `product.upserted` (see first sight) | — |
| `TaskPlanned` | per `PlannedTask` | `task.created`, column `backlog` | — |
| `TaskMoved` | always | `task.moved` | `from`, `to` = board columns (`columnOf` of the move's two statuses, parsed from `Reason`'s "FROM→TO"); `agent` = `TaskRecord.Role` only when `to` is `working`, else omitted |
| `TaskMoved` | `Rule == "start"` | also `task.claimed` | `agent` = `TaskRecord.Role` (GAOS has no agent id; §6 R2) |
| `TaskMoved` | `To == DONE` | also `task.done` | — |
| `TaskMoved` | `To == FAILED` | also `task.failed` | `reason: "failed"` |
| `TaskMoved` | `To == CANCELED` | also `task.failed` | `reason: "canceled"` |
| `TaskMoved` | moved task `Kind == review` and `To == READY` | also `gate.requested` | — |
| `TaskMoved` | moved task `Kind == review`, `To == DONE`, `Rule == "review-cleared"` | also `gate.decided` | `decision: approved` |
| `TaskMoved` | moved task `Kind == review`, `Rule == "design-blocked"` | also `gate.decided` | `decision: rejected` |
| `TaskMoved` | `Rule == "review-failed"` (and `design-screened`, `review-screened`) | `task.moved` only — a failed review is not a verdict (D-068 cl. 7) | — |
| `RecordUpdated` "QA results applied" | — | **not mapped** (names no task; §6 R3) | — |

Every rule not named maps to `task.moved` only. A send-back (`DONE → READY`, D-084) therefore produces
`task.moved`, and a later `DONE` produces a second `task.done`: intended, and stated to the receiver's
author in the operator summary.

**First sight.** An in-memory set of announced product ids (with last-sent name and visibility) and of
announced task ids (cap 50 000; cleared entry-by-entry when an event is dropped, below). Before any event
that mentions a product not in the set, or whose name/visibility differs from the last one sent, the mapper
emits `product.upserted`; before any `TaskMoved` for a task not in the set it emits a synthetic
`task.created` carrying the task's **current** `Title/Kind` from `TaskInfo` and column = the move's *from*
column. This closes the "tasks created before this boot never exist in the town" gap and covers renames
(`Products()` is read fresh per mapped event).

### 3.3 Identity, enrichment, redaction

- **`product.key`** = the product id lower-cased (`p-6` → `p-6`), stable and unique (rename cannot orphan a
  hall; two same-named products cannot collide).
- **Visibility** = `public` only if the product id is listed in `BRAIN_TOWN_PUBLIC_PRODUCTS`
  (comma-separated, trimmed); otherwise `hidden`.
- **Redaction** — hidden product: `name` = `"Private product"`, `task.title` = the task id. Public
  product: real `name`; `task.title` is the real title with control characters removed and truncated to 200
  runes; if the egress secret detectors (`dashboard/egress.go`, the same ones BR-32 uses before model
  dispatch) fire on it, the title is replaced by the task id. `kind` and `column` are sent for both.
- **`TaskInfo`**: optional interface `taskLookup{ TaskInfo(ctx, id string) (TaskRecord, bool) }`,
  implemented by `MemStore` (takes `s.mu.RLock()`, scans `s.tasks`) and delegated by `ArcadeStore` to
  `s.proj`. Not on the `Store` interface, so the `readOnly` wrapper and the store conformance suite are
  untouched. It supplies `Role` and `Kind` of the moved task and, for first sight, `Title`. The column of a
  `task.moved` always comes from the event (`To`), never from the lookup. Values from the lookup are "as of
  send time"; `Kind` and `Title` do not change.
- **Event `id`**: ULID-shaped, 26 Crockford-base32 characters: 10 characters encoding `Event.At` in
  milliseconds (`At*1000`; if `At == 0`, the send time), then 16 characters from the first 10 bytes of
  `SHA-256(bootKey ‖ seq ‖ index)` base32-encoded, where `bootKey` is 16 random bytes made once at boot
  (`crypto/rand`) and `index` is the position of the output within its GAOS event. Deterministic within a
  process, so every retry carries the same `id` and `Idempotency-Key`; it does not reveal GAOS's sequence
  numbers (SEC-12). Worked example in the tests: `At=1700000000`, fixed `bootKey`, `seq=7`, `index=0` →
  a fixed 26-character string asserted in TC-518.
- `at` = `Event.At` as RFC 3339 UTC (0 → send time).

### 3.4 Delivery

A single sender goroutine drains the channel and delivers **in order** (per-task order holds; cross-task
order too). For each output: `POST` with `http.Client{Timeout: 3s, CheckRedirect: refuse}`, TLS verification
never disabled; on a network error, `429` or `5xx` wait 1 s, 5 s, 30 s and retry (four attempts at most:
worst case 36 s of waiting + 4 × 3 s = **48 s** per event); `2xx` and `409` are success; any other status is
dropped. The response body is read to at most 4 KiB, discarded and closed, never logged.
**Degraded mode:** after 3 consecutive events that exhausted their retries the sender makes a single
attempt per event (no waits) until one succeeds, so a long outage drains the backlog instead of stalling on
it. **Overflow:** a full channel drops the new event; any dropped or abandoned event also removes its task
(and product) from the announced sets, so the next event for it is preceded by a fresh `task.created` /
`product.upserted`. A broker-style "N missed" count is the number of drops since the last success, logged
once on recovery.
Nothing GAOS does waits on the worker: the tap is a non-blocking send.

**Log line:** `brain town: event <id> <type> status <code|class>` where class is one of
`timeout|refused|dns|tls|other`. Never the URL, the token, a body, a title, or a Go error string.

### 3.5 Configuration

| Variable | Default | Meaning |
|---|---|---|
| `BRAIN_TOWN_ENABLED` | `false` | `true` or `1` enables; anything else disables. |
| `BRAIN_TOWN_URL` | — | Full ingest URL. **https only**; no userinfo, query or fragment; redirects refused. The shared checker is extracted from `validQaURL` as `validOutURL(raw, label, scheme, denyHosts)`; every existing `validQaURL` message stays byte-for-byte (the label is `GAOS_QA_URL`). Brain Town passes deny hosts `[host of GAOS_ARCADE_URL, host of GAOS_AIM_URL, host of GAOS_QA_URL]`. |
| `BRAIN_TOWN_TOKEN_FILE` | — | Path to the file holding the bearer token. **No environment-variable token** (every GAOS secret is a mounted file). Read by a new `readBrainTownToken` (`braintown.go`): `warnIfOpen`, trim one trailing newline, require 16–256 printable ASCII characters without whitespace. An unreadable or invalid file **disables** the feature with a reason; there is no fallback. |
| `BRAIN_TOWN_PUBLIC_PRODUCTS` | empty | Product ids sent as `public`. |

The token is refused (feature disabled, reason shown) if it starts with `gaos_` or equals any configured
GAOS API token, so a GAOS credential cannot be sent to the receiver by a wrong path (SEC-9). `bt_live_` is
added to the secret detectors in `egress.go` and to the `SECRET` pattern in `deploy/check-workspace.sh`.

**Deploy wiring.** `deploy/stacks/compose.prod.yaml` passes `BRAIN_TOWN_ENABLED`, `BRAIN_TOWN_URL`,
`BRAIN_TOWN_PUBLIC_PRODUCTS` as `"${NAME:-}"` (from the deploy shell, repeated on every redeploy, like
`GAOS_QA_ORIGIN`) and `BRAIN_TOWN_TOKEN_FILE: /run/secrets/prod.bt-token`, with the gitignored host file
`deploy/stacks/prod.bt-token` mounted read-only like the other `prod.*` secrets. An **empty** file means
"no token" and the feature stays off. `deploy/check-stacks.sh` gains assertions: the mount exists, and no
stack file carries a `BRAIN_TOWN_TOKEN:` key (TC-523).

### 3.6 Surface

`IntegrationsPage` (`view.go`) gains `BrainTown BrainTownRow{State string; Host string; Queue int; Last
string; Failed int; Dropped int}`: `State` is `disabled`, `enabled` or the reason it is unusable; `Host` is
`urlHost(...)`; `Last` is `"<code|class> · <age>"`; no token, no path, no title. `integrations.go` fills it
from the worker's atomic counters; `templates/backoffice.html` renders it with existing component classes
(the frontend rule `.claude/rules/frontend.md` applies; no new CSS class).

## 4. Change list, phases, verification

Each phase ends buildable and tested; run its command before the next.

| Phase | # | File | Change | Exit command |
|---|---|---|---|---|
| P1 | 1 | `dashboard/qaclient.go` | extract `validOutURL`; `validQaURL` calls it | `cd dashboard && go test ./...` (count unchanged) |
| P1 | 2 | `dashboard/store_mem.go`, `dashboard/store_arcade.go` | `SetCommitTap`, `TaskInfo` on both | same |
| P2 | 3 | `dashboard/braintown.go` (new) | config, mapper, redaction, id, sender, `commitFeed`, `taskLookup` | `go test ./...` |
| P2 | 4 | `dashboard/braintown_test.go` (new) | TC-513 … TC-522 | `go test -run BrainTown ./...` |
| P2 | 5 | `dashboard/egress.go` | add the `bt_live_` detector | `go test ./...` |
| P3 | 6 | `dashboard/main.go` | register tap before reconcile; start worker; `server` field | `go test ./...` |
| P3 | 7 | `dashboard/integrations.go`, `dashboard/view.go`, `dashboard/templates/backoffice.html` | Brain Town row | `go test ./...` |
| P4 | 8 | `deploy/stacks/compose.prod.yaml`, `deploy/check-stacks.sh`, `.gitignore` | env + mount + assertions | `./deploy/check-stacks.sh` |
| P4 | 9 | `deploy/check-workspace.sh` | `bt_live_` in `SECRET`; new pinned counts (below) | `./deploy/check-workspace.sh` |
| P5 | 10 | `README.md`, `pustaka/docs/infra/environments.html` | the settings; counts | `cd pustaka && go run . check ./docs` |
| P5 | 11 | `pustaka/docs/integrations/brain-town.html`, `pustaka/docs/integrations/overview.html`, `pustaka/docs/assets/toc/integrations.js` | docs page + link | same |
| P5 | 12 | `pustaka/docs/product/brd.html` | §8.12.1 inventory row: the Brain Town feed, off by default | same |
| P5 | 13 | `pustaka/docs/changelog.html`, `pustaka/docs/assets/toc.js` | v0.46.0 (minor) | same |
| P5 | 14 | `pustaka/docs/product/decision-log.html`, `pustaka/docs/product/qa-test-plan.html`, `pustaka/docs/assets/toc/product.js`, `.claude/rules/qa-test-plan.md` | D-086, TC rows and scenarios, counts | `./deploy/check-workspace.sh` |
| P0 | 15 | `docs/designs/brain-town-emitter.md`, `docs/designs/reviews/brain-town-emitter-review-1.md`, `docs/designs/reviews/brain-town-emitter-security-review-1.md` | this design and its reports | — |

**Tests** (each its own top-level `func`; TC numbers in order):

| TC | Function | Asserts |
|---|---|---|
| 513 | `TestBrainTownDisabledSendsNothing` | enabled false → zero requests after a task is planned and moved; no tap registered |
| 514 | `TestBrainTownConfigIsValidated` | http URL, userinfo, query, deny host, `gaos_` token, equal-to-API-token, short token each → disabled with a reason; valid → enabled |
| 515 | `TestBrainTownMapsEveryEventType` | table over every row of §3.2: types, `data`, `reason` ∈ {failed, canceled}, gate only for review tasks, `review-failed` → no gate event, no body contains `@` or the actor |
| 516 | `TestBrainTownRedactsHiddenProducts` | default hidden: name `Private product`, title = task id, `visibility: hidden`; allow-listed: real name/title, 200-rune cap, secret-looking title → task id; key = product id |
| 517 | `TestBrainTownLifecycleOrderAndFirstSight` | create → every column → gate → done arrives in order; a move for an unseen task is preceded by `task.created`; a send-back yields a second `task.done` |
| 518 | `TestBrainTownRetriesKeepTheSameID` | `Idempotency-Key == body.id` on every attempt; fixed worked-example id; 5xx/429/network retried at 1/5/30 s (injected clock); 400 not retried; 409 success; 202 success |
| 519 | `TestBrainTownSlowReceiverNeverBlocksTheStore` | receiver sleeps 10 s; `MoveCard`/`emit` returns in < 100 ms; channel overflow drops and counts |
| 520 | `TestBrainTownTokenNeverLeaks` | captured log and every returned error contain neither the token nor the URL path |
| 521 | `TestBrainTownIntegrationsRowHasNoSecret` | rendered row has host/state/counts only |
| 522 | `TestBrainTownTapFiresOnlyAfterCommit` | `ArcadeStore` against `testArcade` with a failing commit: tap not called; with success: called once per event; MemStore: called on emit |
| 523 | `deploy/check-stacks.sh` (automation) | mount present; no `BRAIN_TOWN_TOKEN:` key in any stack file |

**Arithmetic.** New register rows: TC-513 … TC-523 = **11**. 512 + 11 = **523** rows; highest TC-523; next
free TC-524. New Go tests: 513–522 = **10**. 386 + 10 = **396** Go tests; dashboard 364 + 10 = **374**
(pustaka stays 22; 374 + 22 = 396). Decision log: D-086 added; `decision-log.html:18` says "Eighty-four
entries" although D-085 exists, so it already lags by one — after this change it reads **eighty-six** and
`check-workspace.sh` pins the same string. BRD rule count and open-question count are unchanged (no new BR,
no new OQ; the BRD change is one inventory cell). Strings to change, each with before → after: "three
hundred and eighty-six" → "three hundred and ninety-six" and `386` → `396` (`check-workspace.sh`,
README, `qa-test-plan.html`, `product.js`); "five hundred and twelve" → "five hundred and twenty-three";
`364` → `374`; "highest in use TC-512 / next free TC-513" → "TC-523 / TC-524"; README "Product version
0.45.0" → "0.46.0".

**D-086 (draft).** *Decision:* GAOS may push committed task and product state changes to an operator-chosen
public receiver, off by default; hidden products are redacted at the source; delivery is best-effort,
in-memory and never on a task's path. *Rejected:* sending real titles for hidden products (receiver-side
hiding); tailing `Events()` with a persisted cursor; a broker subscription (pre-commit). *Evidence:*
TC-513 … TC-523, `pustaka/docs/integrations/brain-town.html`.

## 5. Alternatives

- **Tail `Store.Events()` with a persisted cursor** — rejected: it strips payloads (`store_mem.go:692`) and
  a cursor needs a schema change for a feed that is best-effort by brief.
- **Subscribe to the broker and "settle"** — rejected after review: `ArcadeStore` emits before commit and
  never rolls the projection back, so neither a delay nor an `Events()` check can detect a failed commit.
- **Send real titles for hidden products** (the brief's wording) — rejected: SEC-1.
- **Send nothing for hidden products** — rejected: the receiver draws every product as a hall and the
  operator wants hidden halls shown with an alias, which the redacted event supports.
- **Default `public`, deny-list `hidden`** — rejected: one forgotten entry leaks; default-hidden fails closed.

## 6. Risks

- **R2** GAOS has no agent id; `agent` is the task role (e.g. `engineer`).
- **R3** `task.qa` is not emitted (the QA event names no task). One of the brief's nine types is therefore
  declared unmet, by operator decision, until GAOS ties a result to a task.
- **R4** events queued at shutdown, or dropped on overflow, are not replayed; first-sight events repair the
  receiver's view on the next event of an affected task.
- **R5** public products' real titles go to a third party once the operator enables the feed and lists the
  product; the docs page says so in its first paragraph.
- **R6** the tap runs under the store lock; it is a non-blocking send, covered by TC-519.

## 7. Non-goals

No inbound calls. No persisted cursor or replay. No new event type, task status or docs CSS class. No
`visibility` column. No `task.qa`. No change to the QA sync or the answer webhook behaviour. No environment
variable carrying the token.
