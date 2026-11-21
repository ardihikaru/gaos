# The QA tool asks the operator, and gets the decision back

**Status:** Draft v1. Implemented on the branch `feat/qa-inbox-api` in the same change; the zero-context review (`docs/rules/gaos-design-review.md`) is still owed before it merges. Written after the code, so every claim below is anchored to the code that exists.
**Feature:** `qa-inbox-api`
**Decision:** D-078 (proposed; the number is claimed in the event catalog comment `dashboard/events.go`, and the decision-log row is added when the review passes).

## 0. Glossary

- **Inbox / "Needs you"** — the back-office view listing what waits on the operator (`dashboard/templates/needs.html` template `bo-inbox`, built by `buildInbox`, `dashboard/handlers_bo.go:686`).
- **Task question** — the existing kind of inbox item: an agent's turn ended on a question, its task sits in `AWAITING_INPUT` (`dashboard/questions.go`, M15, D-064).
- **External item** — the new kind: a question the QA tool asks through the machine API. It has no task.
- **QA tool** — the `qa-automation-tools` service. Calls GAOS with the QA-scope bearer token (`GAOS_API_QA_TOKEN_FILE`, product-scoped by `GAOS_API_QA_PRODUCTS`, `dashboard/api_v1.go`); GAOS calls it with `GAOS_QA_URL` + the outbound `GAOS_QA_TOKEN_FILE` token (`dashboard/qaclient.go`).
- **externalId** — the QA tool's own id for the item. With the product id it is the item's identity.

## 1. Problem

The QA tool sometimes needs a human decision that belongs to no GAOS task ("quarantine these three flaky specs?"). GAOS's inbox only lists task questions: `OpenQuestions` returns a question only while its task is `AWAITING_INPUT` (`dashboard/questions.go:126-135`). There is no way for the QA tool to put a question there, and no way for it to learn the answer.

## 2. Goals and non-goals

**Goals.** G1 the QA tool asks a question through the bearer API and polls for the decision. G2 the item appears in the existing inbox, counts toward the chip and nav badge, and is answered in the existing answer sheet. G3 the decision is pushed to the QA tool, durably, without ever blocking the answer. G4 everything survives a restart, because it is events.

**Non-goals.**

- N1. No task, no board card and no desk is created or moved by an external item (BR-20: the Result carries no floor).
- N2. Task-bound question behaviour is untouched: `QuestionAsked`/`QuestionAnswered`, `AnswerQuestion`, `openQuestionLocked`, the `/tasks/{taskId}/answer` route and its tests keep their exact semantics.
- N3. No new secret, env var or URL. The webhook reuses `GAOS_QA_URL` and `GAOS_QA_TOKEN_FILE`.
- N4. No editing or withdrawing an item from the QA side. An item ends by answer or expiry only.
- N5. No new docs component class, task status or database table.

## 3. Design

### 3.1 The item is three events (BR-05)

No synthetic task. An item is the projection of three new event types, added to the closed catalog (`dashboard/events.go`), each with an explicit no-op case in the fold (`dashboard/fold.go`, beside `QuestionAsked`) and a name in the data model document (`artifacts/v1/gaos-arcadedb-model.html` §08, the execution group and the payload table):

| Event | Kind | Payload (`EventPayload.Question`, plus `Rule`/`Reason`) |
|---|---|---|
| `ExternalQuestionAsked` | warn (counts toward notices, `needsNotice`) | `externalId`, `title`, `source` ("QA tool"), `question`, `context`, `options`, `askedAt`, `deadline` |
| `ExternalQuestionAnswered` | info | the same payload plus `status` (`answered` or `expired`), `choice`, `text`, `answeredAt` |
| `ExternalAnswerDelivery` | info, warn on `gave-up` | `externalId`, `attempt`, `attemptAt`; `Rule` = `delivered` \| `retry` \| `gave-up`; `Reason` = why an attempt failed |

`QuestionPayload` (`dashboard/questions.go`) gained six `omitempty` fields (`externalId`, `title`, `source`, `deadline`, `status`, `attemptAt`). Existing task-question events serialise unchanged. The event's `DeskID` (the ArcadeDB `entityId`) is the product id, so no schema or DDL change is needed: `Event.type` is a free string (`dashboard/schema/07-events.sql`) and the payload is a MAP.

The projection is `MemStore.externalItemsLocked` (`dashboard/external_inbox.go`): one `ExternalItem` per (product id, externalId), folded in event order. `ArcadeStore` reads and writes through its projection and persists the Result's events in the mutation's transaction (`dashboard/store_arcade_blueprint.go`); `load()` rebuilds the stream, so nothing else is needed for a restart.

Identity and idempotency: `AskExternal` returns the existing item and writes nothing when (product, externalId) already has an `ExternalQuestionAsked`. The item id returned to the QA tool is `ei-<seq of that event>`, stable across restarts because the seq is persisted.

### 3.2 Reads and the inbox

`OpenQuestions` appends the open external items as `Question` rows (`External: true`, `TaskID: ""`, `TaskTitle: <title>`, `ExpiresAt: <deadline>`) and keeps its oldest-first sort. Everything built on it follows without a change of its own: `needsCount` (the chip and the nav badge), `GET /questions`, the inbox list and its product filter. An item past its deadline is excluded at read time, so it can never be answered in the gap before the sweep writes the expiry.

### 3.3 Expiry

`ExpireQuestions(now)` (swept every minute, `dashboard/main.go`) now also closes every open external item whose `deadline <= now` by writing one `ExternalQuestionAnswered` with `status: expired` and `answeredAt = deadline`. A swept item is never swept again, and an answered item is never expired. A sweep that touches only external items returns a Result with no floor. The deadline is `askedAt + expiresInHours` (default 168, 1 to 720); GET reports an overdue open item as `expired` before the sweep has written it.

### 3.4 The machine API (`dashboard/api_inbox.go`, registered in `registerAPI`)

Both routes use `apiGate(scopeQA)` and `apiProduct`, so the QA token reaches only the products in `GAOS_API_QA_PRODUCTS` (any other id is a 404), and the admin token reaches all.

**`POST /v1/products/{id}/inbox`** (public `/api/v1/products/{id}/inbox`). `beginWrite`, `apiDecode` (strict, 1 MiB), then `secretInStrings` over the whole body, then validation.

```json
{ "externalId": "run-42:case-7",        // 1..80 of [A-Za-z0-9._:-], first char alphanumeric
  "question":   "Quarantine these 3 specs?",   // required, <= 500 characters
  "context":    "…",                      // optional, <= 4000
  "options":    [{"label":"Yes","description":"…"}, {"label":"No"}],   // 2..3; label 1..60, description <= 200; omit for Approve / Reject / Needs changes
  "expiresInHours": 168,                 // optional, 1..720
  "title":      "Quarantine" }           // optional, <= 120
```

Text is cleaned before it is stored: CRLF becomes LF, control characters and bidirectional overrides are dropped, ends are trimmed, titles and labels collapse whitespace. Lengths count characters, not bytes.

| Status | Body | When |
|---|---|---|
| 201 | `{"id":"ei-123","externalId":"…","status":"open","created":true}` | new item |
| 200 | same shape, `"created":false`, `status` = the item's current status | same (product, externalId) again; nothing is written |
| 400 | `{"error": code, "reason": …}` with code `bad-external-id`, `bad-question`, `bad-context`, `bad-title`, `bad-options`, `bad-expiry`, `bad-json`, `secret-in-body` | validation |
| 401 / 404 / 413 / 422 / 429 | as every other door | no/wrong token; out-of-scope or unknown product; body over 1 MiB; 200 open items already on the product; a write by this token in flight |

> **Since v0.43.0** (D-083, `docs/designs/answer-source.md`): a client can also answer an item it asked with `POST /v1/products/{id}/inbox/{externalId}/answer`; every answer records its source; the GET body gains `source`; a client reads only its own items; and the clients are a registry (`GAOS_API_CLIENTS_FILE`). The webhook below carries only the QA tool's own items.

**`GET /v1/products/{id}/inbox/{externalId}`**:

```json
{ "externalId": "run-42:case-7", "status": "answered",   // open | answered | expired
  "choice": 2, "optionLabel": "No", "text": "…",         // absent while open; choice 4 = the operator's own words (optionLabel absent)
  "askedAt": 1763000000, "answeredAt": 1763000900,        // unix seconds; answeredAt absent while open
  "expiresAt": 1763604800 }
```

404 when the id is unknown, malformed, or on a product outside the token's scope.

### 3.5 The back office

- The inbox row (`templates/needs.html`) shows a **QA tool** tag where a task id would be, then the item's title; the role column is omitted. The row opens `GET /ui/answer-ext/{id}/{externalId}` (`handleExternalSheet`), which renders the existing `answer-modal` with `AnswerView.Source` set.
- The sheet offers the QA tool's options and, always, option 4 (own words). **No option is preselected and none is marked Recommended**: the QA tool recommends nothing, and a default "Approve" would be a decision nobody made. `static/app.js` already handles "nothing picked" (submit stays disabled, hint says "Pick one of the four answers").
- The sheet posts to **`POST /products/{id}/inbox/{externalId}/answer`** (`handleExternalAnswer`): same `requireBO` session gate, same body (`{choice, text}` JSON or form), same `s.mut(r)` replay refusal and same 404/409/422 mapping as the task route (`dashboard/handlers_questions.go`). The repo has no CSRF layer; the route is exactly as exposed as the task answer route.
- Choice must be an offered option (1 to the number offered) or 4 with words (at most 4000 characters). A second answer or an answer to an expired item is refused (409 JSON / toast).

### 3.6 The answer webhook (`dashboard/inbox_worker.go`)

When an item is answered or expires, GAOS `POST`s the decision to the QA tool:

```
POST {GAOS_QA_URL}/api/v1/gaos/inbox-answer
Authorization: Bearer <the GAOS_QA_TOKEN_FILE token>
{ "externalId": "run-42:case-7", "productId": "p-3", "status": "answered",   // answered | expired
  "choice": 2, "optionLabel": "No", "text": "",                              // choice 0 and "" when expired
  "answeredAt": 1763000900 }                                                 // unix seconds; the deadline when expired
```

- **Destination and credential are never request input.** It reuses the `qaOutbound` client built once at boot (`newQaOutbound`), which validates the URL with `validQaURL` (https, or http inside loopback / `10.10.0.0/24`; no userinfo, query or fragment; no gateway or database host), refuses redirects, caps the response and never logs the token. Unconfigured or invalid means the worker idles and records nothing; the work waits for the day it is configured.
- **Durable.** The state is the events: the pending set is "answered or expired items whose latest delivery event is neither `delivered` nor `gave-up`". Each attempt appends an `ExternalAnswerDelivery`. A restart resumes exactly there, including the attempt count and the backoff clock (`attemptAt`).
- **Retry.** Up to 5 attempts; the wait after attempt N failing is 30 s, 2 min, 10 min, 1 h. Any non-2xx, a network error or a timeout (15 s) is a failed attempt. After the 5th the item is `gave-up` (a warn-kind event) and is never sent again; `GET` keeps serving the decision.
- **Single worker, off the request path.** One goroutine started in `main.go` ticks every 10 s and is nudged (`Kick`, non-blocking, buffered 1) after an answer. The answer request records the event and returns; a hung or down QA tool cannot slow or fail it. The sweep's expiries are picked up by the tick.
- **At-least-once.** A crash between the POST and its delivery event repeats the POST on restart. The QA tool must treat the webhook as idempotent on `externalId`.

### 3.7 Live updates

`sseName` maps the two question-bearing external events to the `question` SSE name, so an open inbox refreshes on an ask or an answer exactly as it does for a task question (`dashboard/broker.go`). The hint carries no task.

## 4. Change list (the complete set)

| File | Change |
|---|---|
| `dashboard/events.go` | three names |
| `dashboard/fold.go` | one no-op case |
| `dashboard/broker.go` | `sseName` |
| `dashboard/questions.go` | six payload fields, `Question.External`, `needsNotice`, `OpenQuestions` and `ExpireQuestions` include external items |
| `dashboard/external_inbox.go` (new) | projection and the five store methods on `MemStore` |
| `dashboard/store.go`, `store_arcade_blueprint.go` | seam methods, read-only refusals, Arcade wrappers |
| `dashboard/api_inbox.go` (new), `api_v1.go` | the two routes |
| `dashboard/inbox_worker.go` (new), `main.go` | the worker and its wiring, two routes |
| `dashboard/handlers_questions.go`, `handlers_bo.go`, `view.go`, `templates/needs.html`, `static/app.css` | answer route, sheet, row, tag |
| `dashboard/external_inbox_test.go` (new), `events_test.go`, `store_conformance_test.go` | tests |
| `artifacts/v1/gaos-arcadedb-model.html`, `pustaka/docs/**` | catalog doc, register, counts, changelog |

## 5. Alternatives rejected

- **A synthetic task in `AWAITING_INPUT`.** It would reuse `AnswerQuestion` unchanged but invents a task that the scheduler, board, design gate and `ExpireQuestions` (which would block it, not close it) all then have to learn to ignore. N1 and N2 forbid it.
- **A webhook URL in the request.** Rejected outright: it makes GAOS an SSRF client with the QA bearer in hand. `validQaURL` exists to prevent this (`dashboard/qaclient.go:62`).
- **Reusing `QuestionAsked`/`QuestionAnswered` with an empty task.** `questionsLocked` keys by task and every consumer assumes a task exists; the catalog is cheaper to extend than those consumers.
- **A MySQL/Arcade outbox table.** The events already are the durable record; a second store for delivery state could disagree with them.

## 6. Counts

10 new top-level Go tests (`TC-413` to `TC-422` in the QA register) plus one shared case in the store conformance suite (`TheQaToolsInboxItemsRoundTrip`, covered by TC-124 and TC-125). The register goes from 412 to 422 rows, `dashboard (go test)` from 272 to 282, Go tests from 294 to 304. The ArcadeDB half of the conformance case needs `GAOS_ARCADE_TEST_URL` and was not run where this was written.

## 7. What the QA tool must match

1. Send `Authorization: Bearer <QA token>`; ask with the body in §3.4; poll the GET in §3.4.
2. Serve `POST /api/v1/gaos/inbox-answer` with the body in §3.6, accept the GAOS outbound bearer, answer 2xx once it has stored the decision, and be idempotent on `externalId`.
3. Treat `status: expired` as "no human decided in time".
