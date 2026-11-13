# sample-product as a GAOS product, and a two-way QA sync API

**Status:** Draft v5. Reviewed in three rounds (`reviews/sample-product-qa-sync-review-1.md`: round 1 on v1, round 2 on v2, round 3 on v4's §4.12). Implementation of P1–P4 followed v3, which the author approved on 2026-11-14 under the product owner's standing "don't ask me"; §4.12 (v4/v5) is implemented only after round 3's findings, all applied below.
**Feature:** `sample-product-qa-sync`
**Version target:** v0.37.0

## 0. Glossary (terms this document uses)

- **GAOS** — this repository: a Go dashboard (`dashboard/`), a docs site (`pustaka/`) and deploy files (`deploy/`).
- **Product / master record** — one product the operator manages: BRD, stories, diagrams, decisions, Blueprint. A `Master` (`dashboard/model.go:342`).
- **Desk** — the per-product slot that holds the product's active workflow (`Desk`, `model.go:103`). Only the six seeded products have one today.
- **Workflow / task / board** — a workflow is a planned graph of tasks; the **board** is a read-only projection of the tasks into kanban columns (`dashboard/board.go`). Task status is the closed ten-word vocabulary in `dashboard/taskstate.go`.
- **The design gate (M17, BR-46…BR-50)** — an implementation task is held `BLOCKED` until a design task delivered and a review task cleared (`dashboard/designgate.go`).
- **Event / fold** — every mutation appends an event from the closed catalog (BR-05, `dashboard/events.go`); the *fold* rebuilds the floor from events. Product records are **not** folded (`dashboard/fold.go:130-175`).
- **Replay** — an operator parks the floor at a past event; while parked, ordinary mutations are refused by the read-only store handle (`dashboard/main.go:89-95`, `store.go:216`).
- **Blueprint** — a product's standing rules, published as immutable versions and (separately, by an operator button) synced into an ai-manager workspace.
- **BO** — the back office: the browser UI whose only gate today is a forgeable cookie (`dashboard/handlers_bo.go:31-34`; BR-48's open precondition).
- **QA tools** — the repository `qa-automation-tools`, which will write and run the automated test scripts.
- **Source repo** — `automation-test-sample-product`: the sample-product product map and test catalog (markdown), plus the Playwright specs.

## 1. Problem

sample-product (sample-product's hospital information system) has a source repo holding its
product map (4 parts, 39 modules, 444 features: `docs/modules/*.md`) and a test
catalog (47 epic files, 258 user stories, 1,700 cases, 494 already automated:
`docs/test-catalog/**.md`). The QA tools will concentrate on the scripts.

The product owner wants (1) sample-product added to the GAOS instance at
`gaos.sample-product.com` with a default Blueprint, BRD, stories, kanban cards and
task details; (2) a REST API the QA tools can integrate with later; (3) a
**sync** in both directions, with a sync button for each.

GAOS has no machine API. Every route is for a browser; the one JSON route
(`GET /products/{id}/board`, `handlers_board.go:32`) sits behind the cookie
gate; `GET /tasks/{id}/context` also answers JSON, ungated. A product created in the BO has no desk, and `CreateWorkflow` returns
`ErrNotFound` for it (`store_mem.go:1955-1958`; desks are built only for the
seeded products, `store_mem.go:278-291`), so it can have stories but no tasks
and no board. sample-product needs both.

## 2. Goals and non-goals

**Goals.** G1 a bearer-token REST surface, fail-closed. G2 a QA catalog per
product with one writer per field. G3 sync both ways: GAOS serves the endpoints
the QA tools call, and GAOS calls the QA tools when an operator presses a
button. G4 a repeatable, idempotent seed that gives sample-product a BRD, stories, a
Blueprint, a workflow and a populated board. G5 a read-mostly BO "QA" tab with
the two buttons.

**Non-goals.**

- N1. No change to the QA tools repo. §4.11 is a contract it implements later;
  the outbound half is inert until `GAOS_QA_URL` and its token are set.
- N2. No new event type, task status or docs class (the three closed
  vocabularies stay closed).
- N3. No case editor in the BO. Planning fields of an EXISTING case change only
  through the catalog endpoint (admin) or a seed re-run. (v4: the QA tools may
  CREATE a case, never change one, §4.12.)
- N4. No deletion of cases; `decision: wont` retires one.
- N5. **The catalog never moves a task.** QA data changes no existing task's
  status, ever (BR-54). The one write it may cause to the task graph is to ADD a
  card for a new epic, born exactly as a planned card is (§4.12). Cards follow
  the existing design→review→implement flow and the operator. (Round 1: the reflection walked gated tasks past the gate
  and recorded dispatches that never happened. It is removed, not repaired.
  Whether results should ever move cards is OQ-35.)
- N6. No BO authentication. The new BO routes inherit BR-48's open
  precondition and say so (§8).
- N7. The seed does not run `SyncBlueprint` (a real gateway call); it stays an
  operator button. The seed does not clear the design gate (§4.9).
- N8. No customer-specific code in the binary: sample-product's content is a data file
  under `deploy/seed/sample-product/`, loaded through a generic endpoint.

## 3. Alternatives considered

- **A. Every test case is a Task.** Rejected: `validatePlan` demands a gated
  design→review→implement shape (`graph.go:140-283`), the scheduler and task
  fold traverse every task, and cards are work for an agent while a case row is
  a fact about a script.
- **B. Catalog inside the `Product` record document** (as the BRD is,
  `store_arcade.go:1134-1204`). Rejected: ~700 KB rewritten per edit; D-009
  keeps documents for text a human types.
- **C. A new event type.** Rejected by BR-05; `RecordUpdated` is the precedent
  (`store_mem.go:1677,1703,1725`).
- **D. One shared field, last writer wins.** Rejected: a silent loser.
  Chosen instead: one writer per field (§4.1), plus a stale-write refusal
  inside the automation half.
- **E. Reflect each epic's roll-up onto its task** (draft v1). Rejected after
  review: it overrides other `BLOCKED` reasons, writes `READY` (the
  scheduler's word, `store_mem.go:1980-1983`) and invents `DISPATCHED`/`RUNNING`
  without a run (§N5).
- **F. sample-product compiled into the binary** (draft v1). Rejected after review: a
  customer's catalog and route in a generic Apache-2.0 binary (N8).

## 4. Design

### 4.1 The catalog and who owns which field

A **QaCase** belongs to one product, keyed by its **case id** (`caseId`, e.g.
`TC-AUTH-001`). Fields split in two halves, each with one writer:

| Half | Fields | Writer |
|---|---|---|
| Planning (GAOS) | `caseId`, `epic`, `module`, `features[]`, `userStory`, `scenario`, `polarity`, `level`, `pri`, `decision`, `reason` | catalog endpoint (admin), seed; **and, for a case that does not exist yet, the create-only endpoint (QA scope, §4.12)** |
| Automation (QA tools) | `implemented`, `spec`, `result`, `runAt`, `runRef` | results endpoint (qa/admin), the pull |

Closed vocabularies (copied from the source repo's `scripts/test-catalog.mjs`
at the commit the seed records, §4.9):

- `polarity` `+`|`-`; `level` `API`|`GQL`|`WEB`|`E2E`; `pri` `P0`..`P3`;
  `decision` `planned`|`blocked`|`manual-only`|`wont`; `result` `""`|`pass`|`fail`|`skipped`.
- `reason`: required for `blocked`/`manual-only`/`wont`; optional on `planned`
  (the source catalog's planned rows may carry a note such as `deferred:…`).
  When present it is `<category>:<detail>`, category in
  `defect|not-needed|blocked|unsafe|manual-only|deferred|other`, detail non-empty.
- patterns: `caseId ^TC-[A-Z]+-[0-9]{3}$`; `epic ^[A-Z]{2,8}$`;
  `userStory ^US-[A-Z]+-[0-9]{2}$` or empty; `module ^[A-D][0-9]{2}$` or empty;
  feature `^[A-D][0-9]{2}\.[0-9]{2,3}(\.[0-9]{2})?$`.
- caps (bytes): `scenario` 600, `spec` 240 (relative: no leading `/`, no `..`),
  `runRef` 120 (printable ASCII), `reason` 240; `runAt` unix seconds ≥ 0; ≤ 12 features.

`userStory` is the source repo's user story id. It is **not** a GAOS `Story`
(§4.9 explains the mapping from `module`).

**Effective status** (derived, never stored): `automated` if
`decision == planned && implemented`; else the `decision`. A case with
`implemented` and a decision other than `planned` is **needs decision** and is
listed in the summary; GAOS never flips it.

**Stale and future writes.** Inside the automation half the fields have two
rules. `result`, `runAt` and `runRef` are one **run record**: an item that
writes any of them must carry `runAt`; it is refused `stale` when the stored
`runAt` is greater, `future` when `runAt > now + 300 s` (a millisecond value or
a skewed clock cannot lock a case), and an equal `runAt` with equal fields is
`unchanged`. `implemented` and `spec` describe the script, not a run, and the
last write wins. An item with the **admin** token and `"force": true` skips
`stale` (the repair path for a bad timestamp already stored). This is how the
pull and an inbound `POST …/results` coexist without a silent loser: planning
and automation never conflict (disjoint fields), and inside the automation half
the newest run wins.

**Revisions.** The product's `Qa.CatHead` counts planning changes. A write that
changes a planning field sets that case's `catRev = ++CatHead`. An identical
re-post changes nothing, emits nothing and counts `unchanged`. (Draft v1's
`autoRev` is dropped: nothing reads it.)

### 4.2 Product-level QA state (`Master.Qa`)

```go
type QaState struct {
    Modules []QaModule   // Key "A01", Title, Story (GAOS story id)
    Epics   []QaEpic     // Code, Title, Service, Source (path), Module (key), Story (GAOS story id)
    CatHead int
    Pull    QaSyncSide   // At int64, OK bool, Note string, Items int, Cursor string
    Push    QaSyncSide   // + Rejected []string (≤20 ids), RejectedN int
}
```

`Cursor`: pull = the QA tools' opaque `next`, ≤128 printable ASCII bytes (else
the page is refused); push = the decimal `catRev` last reached. `Note` ≤200
bytes, never a token or a response body. It rides on `Product.qaState` as a
document (no DDL; same as `vendorPolicy`, `schema/13-vendors.sql`).

### 4.3 Storage and the migration

New `dashboard/schema/16-qa-catalog.sql` (every statement `IF NOT EXISTS`; this
is the whole migration, embedded by `schema_embed.go:22` and applied at every
boot by `NewArcadeStoreFor`):

```sql
CREATE VERTEX TYPE QaCase IF NOT EXISTS EXTENDS GaosVertex;
CREATE PROPERTY QaCase.productId   STRING  (mandatory true, notnull true);
CREATE PROPERTY QaCase.caseId      STRING  (mandatory true, notnull true);
CREATE PROPERTY QaCase.epic        STRING  (mandatory true, notnull true);
CREATE PROPERTY QaCase.module      STRING;
CREATE PROPERTY QaCase.features    LIST;
CREATE PROPERTY QaCase.userStory   STRING;
CREATE PROPERTY QaCase.scenario    STRING  (notnull true);
CREATE PROPERTY QaCase.polarity    STRING  (mandatory true);
CREATE PROPERTY QaCase.level       STRING  (mandatory true, regexp 'API|GQL|WEB|E2E');
CREATE PROPERTY QaCase.pri         STRING  (mandatory true, regexp 'P0|P1|P2|P3');
CREATE PROPERTY QaCase.decision    STRING  (mandatory true, regexp 'planned|blocked|manual-only|wont');
CREATE PROPERTY QaCase.reason      STRING;
CREATE PROPERTY QaCase.implemented BOOLEAN;
CREATE PROPERTY QaCase.spec        STRING;
CREATE PROPERTY QaCase.result      STRING  (regexp 'pass|fail|skipped');
CREATE PROPERTY QaCase.runAt       LONG;
CREATE PROPERTY QaCase.runRef      STRING;
CREATE PROPERTY QaCase.catRev      INTEGER (mandatory true, min 0);
CREATE EDGE TYPE HAS_QA_CASE IF NOT EXISTS EXTENDS GaosEdge;   -- Product -> QaCase
CREATE INDEX IF NOT EXISTS ON QaCase (tenantId, id) UNIQUE;
CREATE INDEX IF NOT EXISTS ON QaCase (tenantId, productId, catRev) NOTUNIQUE;
CREATE INDEX IF NOT EXISTS ON HAS_QA_CASE (`@out`, `@in`) UNIQUE;
```

The vertex `id` is `<productId>:<caseId>` (D-047). An empty `result` is stored as
an absent property. `schema_test.go` moves to `wantVertexTypes = 29`,
`wantEdgeTypes = 38` (its own counts, `schema_test.go:41-45`; the model
document `artifacts/v1/gaos-arcadedb-model.html` is updated to match).

`Result` (`store.go:147`) gains `QaCases []QaCase`. A QA write returns the
`Master` (for `Qa`) and the cases; `ArcadeStore.persist` (`store_arcade.go:346`)
upserts them and the edge in the mutation's single transaction.
`ArcadeStore.load` (`store_arcade.go:1134`) reads every `QaCase` back, so a
restart returns the catalog (BR-01). `MemStore` holds
`qa map[productID]map[caseID]QaCase` under `s.mu`.

### 4.4 Store seam

Five methods on `Store` (`store.go`), implemented on `MemStore`, forwarded by
`ArcadeStore` (writes through `mutate`, reads direct):

```go
QaCases(ctx, productID string, q QaQuery) (QaPage, error)
QaSummary(ctx, productID string) (QaSummary, error)
QaImport(ctx, productID string, in QaImportIn) (QaImportOut, Result, error)
QaApplyResults(ctx, productID string, in QaResultsIn) (QaResultsOut, Result, error)
QaRecordSync(ctx, productID, dir string, side QaSyncSide) (Result, error)  // dir "pull"|"push"
```

and `readOnly` (replay) refuses the three writes, so a BO click is refused
during replay. The **`/v1` results and catalog handlers call `s.store`
directly**: because a QA write carries no floor (below), replay has no reason
to block a machine write, and the API no longer depends on a cursor an
unauthenticated GET can set (round 1, SEC-5). **The seed handler uses
`s.mut(r)`**: it calls `CreateWorkflow` and the record mutations, which replay
must refuse; a refusal is 409 `replay`.

**QA writes carry no floor.** Every QA write returns a `Result` whose `Floor`
is the zero value, so `ArcadeStore.persist` skips its desk/task/run loops
(`store_arcade.go:358-368`) and QA writes never save a replayed (parked) past
floor over the live one (BR-20; `store.go:197-201`; `store_mem.go:573-578`).
This also removes the fixed cost of rewriting every desk, task and edge from
each QA write (§4.5). A test asserts both (TC-401).

A write that changes something emits **one `RecordUpdated`** event (plus, for
a write that adds a card, the `TaskPlanned` and `TaskMoved` events of that one
card, §4.12)
(`QaRecordSync` too), naming product, direction and counts. A write that
changes nothing emits nothing. No other event is ever emitted by QA code.

`QaImport` takes optional epics (upsert by `code`) and cases (upsert by
`caseId`) and writes the planning half. Exception, once: with `seed:true`
**and** a `caseId` not yet present, the case is created with the
`implemented`/`spec` it carried; on an existing case the automation half is
never written by an import. `QaApplyResults` takes items
`{caseId, implemented?, spec?, result?, runAt?, runRef?}` (absent = untouched),
writes the automation half only; an unknown `caseId` → rejected
`unknown-case`; stale → rejected `stale`; a planning field in an item is a
decode error (`DisallowUnknownFields`).

Batch semantics: valid items apply, invalid ones come back in `rejected`, the
mutation commits once.

### 4.5 Limits

≤ 200 items and ≤ 1 MiB per request (`http.MaxBytesReader`, read **after**
authentication); over → 413, nothing applied. One write in flight per token; a
second concurrent one answers 429 `Retry-After: 2`. The bound comes from lock
time, not the proxy: `mutate` holds the store's one lock for the whole ArcadeDB
transaction (`store_arcade.go:326-337`). A QA write that adds no card has **no fixed
cost** (no floor, §4.4); one that adds a card returns the live floor and costs
what any ordinary mutation costs (rare: at most 5 per request, §4.12); each new case costs about three statements (upsert, edge probe,
edge), so 200 cases ≈ 600 statements. The *seed's* `CreateWorkflow` is the
opposite: every later ordinary mutation in the system rewrites every desk,
task and edge, so the ~50 sample-product tasks and ~100 dependency edges added by the
seed make every BO write dearer for every product. That cost is measured once
the seed has run (P3 exit) and the batch limit is revised from the number. The
edge allows 300 s and 4 MiB (`deploy/edge/gaos.conf:147,160`) and is not the
limit.

### 4.7 The REST surface

**Paths.** Traefik routes `PathPrefix(/api)` to the dashboard **and strips
`/api`** (`deploy/traefik/dynamic.yml:16-21,58-60`). The Go mux therefore
registers `/v1/...`; the **public** base is `https://gaos.sample-product.com/api/v1`.
`deploy/traefik/dynamic.yml` is unchanged. A test requests the stripped path.

All bodies are JSON objects. Errors: `{"error":"<code>","reason":"<sentence>"}`.

**Auth.** `Authorization: Bearer <token>`. Secrets come from files, matching
the repo's `*_FILE` precedent (`compose.prod.yaml:95-106`):
`GAOS_API_ADMIN_TOKEN_FILE`, `GAOS_API_QA_TOKEN_FILE`. A token must start with
`gaos_` and be ≥ 37 bytes (after trimming one trailing newline) — minted as
`printf 'gaos_%s' "$(openssl rand -hex 32)"` — so GAOS's own secret detectors
can see it (§4.9, `egress.go`, `check-workspace.sh`). Anything else, an empty
file, or a read error (logged) is **unset**. The files must exist (an empty
file is the way to disable a token; the new value is read at boot, so a change
needs a restart). If two of the three tokens (admin, QA, outbound) are equal,
**all API tokens are disabled** and the boot log says why. A request arriving
with a non-empty `X-Forwarded-Prefix` (i.e. through `/app`) is answered 404, so
an edge control keyed to `/api` cannot be bypassed (SEC-10). Rules:

- neither token configured → every `/v1` route answers 503 `api-disabled`;
- otherwise a request with no/wrong token → 401; a valid QA token on an admin
  route → 403; an admin token satisfies every route;
- compare = SHA-256 of both sides + `subtle.ConstantTimeCompare`;
- tokens are never logged or echoed. No failure throttle (256-bit tokens).

**Product scoping.** `GAOS_API_QA_PRODUCTS` is a comma list of product **ids**
(`p-6`; names are not unique and anyone with the cookie can make one). The QA
token reaches only those; any other `{id}` → 404 (not 403); `GET /v1/products`
lists only them; **unset = the QA token reaches nothing** (fail closed). The
admin token reaches every product. The id is known only after the seed, so the
deploy order is: deploy, seed (prints the id), set the variable, restart.

| Method & path (mux) | Scope | Purpose |
|---|---|---|
| `GET /v1/products` | qa | `[{id,name,repo}]` (QA: its products only) |
| `GET /v1/products/{id}/qa/summary` | qa | totals, per-epic roll-up, `regressed` epics (an epic with a `fail`), needs-decision list, sync state, `catHead` |
| `GET /v1/products/{id}/qa/cases?after=<catRev>&limit=<1..200, default 100>&epic=<code>` | qa | cases with `catRev > after`, ascending; `{items,next,catHead}`; `next` = last item's `catRev` when the page was full, else `null` |
| `POST /v1/products/{id}/qa/cases` | qa | create-only, §4.12: `{source,epics?,cases[]}` → `{created,unchanged,rejected[],epicsAdded,cardsAdded,cardsDeferred,catHead}` |
| `POST /v1/products/{id}/qa/results` | qa | `{source,force?,items[]}` → `{applied,unchanged,rejected[],catHead}` |
| `POST /v1/products/{id}/qa/catalog` | admin | `{seed?,epics[],cases[]}` → `{created,updated,unchanged,rejected[],catHead}` |
| `POST /v1/seed/product` | admin | §4.9 |

Status codes: 200; 400 malformed JSON / unknown field / bad query; 401; 403;
404; 409 (`replay`, `product-exists-unseeded` on the seed; `sync-running`,
`cooldown`, `qa-unconfigured`, `qa-out-of-scope` on the BO sync routes);
413; 422 (`not-ready`, with the missing list); 429 (`write-in-flight`); 503.

**Case JSON** (cases responses and catalog requests; the automation
fields `implemented` and `spec` in a catalog request are honoured only under
`seed`; `result`/`runAt`/`runRef` never are):

```json
{"caseId":"TC-AUTH-001","epic":"AUTH","module":"A13","features":["A13.03"],
 "userStory":"US-AUTH-01","scenario":"Given…","polarity":"-","level":"GQL","pri":"P0",
 "decision":"planned","reason":"","status":"automated",
 "implemented":true,"spec":"tests/api/graphql/his-backend/user/login.spec.ts",
 "result":"pass","runAt":1763000000,"runRef":"run-1"}
```

`status` is derived and output-only.

### 4.8 The desk fix (D-076)

When `CreateWorkflow` finds no desk for an existing product, it seats an idle
desk (`Desk{ID,Name,Repo,Goal}`, `State: StateIdle`) and continues. Effects,
stated rather than hidden: (1) any product made in the BO can now get a
workflow, a board and — **if `GAOS_RUNNER` is on and the gate cleared** —
dispatch; prod leaves the runner unset (`compose.prod.yaml:97-98`). (2) the new
desk persists through `persist`'s existing desk loop (`store_arcade.go:358-368`)
with no schema change. (3) **No event records the desk**, and the fold skips
events for unknown desks (`fold.go:129-137`), so a parked (replayed) floor does
not show it. This is the first runtime desk — there was no precedent — and it
is a known gap, recorded in D-076 rather than papered over: reopening BR-05 for
a desk event is out of scope (N2), and the floor UI that read desks was retired
(M14.7). The change makes it safe rather than complete: (4) `ArcadeStore.load`
reads desks `ORDER BY productId` as a string, so `p-10` would sort before `p-2`
and shift seeded desk indices after a restart; the loaded desks are re-sorted
by (id length, id), preserving seed order. (5) the unauthenticated
`POST /workflows` route (`main.go:352`, `handlers_cw.go:67`) and the task
routes `dispatch|approve|retry|cancel` (`main.go:310-313`) get `requireBO`
(§8), because this change would otherwise extend their reach to new products.
Tests: a planned runtime product's tasks are `PENDING`/gate-`BLOCKED` exactly
as a seeded product's; a parked floor with a runtime desk present does not
panic and `Snapshot` answers; desk order after a restart with a product id of
10 or more; `POST /workflows` without a cookie is 401 (all in TC-396).

### 4.9 The product seed (generic) and sample-product's data

`POST /v1/seed/product` (admin) takes a **product seed document**; GAOS ships
no sample-product content. The document:

```json
{"name","owner","repo","goal","priority",
 "brd":{problem,goals[],scope[],outOfScope[],rules[],metrics[]},
 "modules":[{"key":"A01","title","pri","features":["A01.07 · Sub · text [PROD]"]}],
 "diagrams":[{kind,title,src}], "decisions":[{id,title,confidence}],
 "policies":[{key,kind,title,body,order}], "sources":[{url,ref,purpose,note}],
 "epics":[{code,title,service,source,module}],
 "plan":{"goal","priority"}}
```

Steps, in order, each skipped when already true (so re-running never
overwrites what an operator edited), each through an **existing store call**:

1. product: find by exact name (case-insensitive). None → `CreateProduct`. Exactly
   one → adopt it **only if** it carries a seed record (`Qa.Modules` non-empty)
   or is pristine (no stories, no policies, empty BRD, no workflow); otherwise,
   or if the name matches more than one product, answer 409
   `product-exists-unseeded` and change nothing. Never adopt silently; `ProductDraft`
   gains `Priority` (`low|medium|high`, default `medium`) so `high` is settable.
2. `UpdateBRD` only if the stored BRD is entirely empty.
3. stories: one `AddStory` per module not in `Qa.Modules` (role `QA engineer`,
   want `automated regression coverage for <KEY> — <title>`, so `<so>` =
   `a release cannot break <title> unnoticed`, `AC` = the module's feature
   lines, `Pri`: the seed's, downgraded from `must` to `should` when the module has
   fewer than 2 feature lines, because readiness needs ≥ 2 AC on every `must`
   story, `model.go:529`). The returned story id goes into `Qa.Modules`;
   this key→id map is the idempotency record (no change to `Story`).
4. diagrams / decisions: skipped by equal `Title`.
5. policies: `SavePolicy` only for a `key` not yet present. Server-side screen
   (not the client's alone): every text field of the seed — BRD, stories, AC,
   policy bodies — is screened with the egress secret detector (`egress.go`)
   and refused (400 `secret-in-seed`) on a hit; story, AC, feature and case
   text additionally has backticks, angle brackets and line breaks replaced and
   a leading `#` stripped (policy bodies are markdown by nature and are
   screened, not rewritten). Sources:
   `AddKnowledgeSource` (a duplicate is refused and ignored).
6. `PublishBlueprint` if no version exists.
7. readiness: if `Master.ReadinessScore() < PlanFloor` → 422 `not-ready` with
   `ReadinessMissing()` (BR-07 holds for the seed; `CreateWorkflow` does not
   check it itself, `store_mem.go:1930-1958`).
8. epics: `QaImport` epics (with the resolved story id; the card's task id is stored by a second import after step 9).
9. workflow, only if the product's desk has no tasks: `CreateWorkflow` with
   `WorkflowDraft{ProductID, Goal: plan.goal, Priority: plan.priority, Gate:false,
   Stories: <story ids with an epic>}` and this plan, **task `Gate:false` except the Release task, which is `Gate:true`** (a human releases it):
   - `t-1` *Design: test strategy and catalog* — kind `design`, role `architect`;
   - `t-2` *Review the test strategy* — kind `review`, role `qa`, dep `t-1`;
   - one *Automate: `<epic title>`* per epic — kind `implement`, role `qa`,
     story = the epic's story id (empty → the Plan lane), dep `t-2`;
   - *Release: staging regression run and report* — kind `implement`, role
     `qa`, `Gate:true`, deps = every epic task.
   This passes `validateTheGate` (`graph.go:181-283`).

**The design gate is not cleared by the seed.** `t-1`/`t-2` start `PENDING`; the
epic tasks start gate-`BLOCKED` (`store_mem.go:2039-2049`); the operator runs
design and review through the normal gate. Nothing is fabricated: no `DONE`,
no run, no artifact. The board is honest on day one: Backlog holds the design,
review and Release cards, Blocked holds the epic cards. With `GAOS_RUNNER` off (prod)
nothing dispatches; if it is turned on, the gate's own sequence applies and the
epic tasks are real agent work (write the scripts for that epic) priced like
any other.

**sample-product's data** (private repo; D-074 records the product owner's approval and
the condition that the files leave this repo before it ever becomes public) is
`deploy/seed/sample-product/product.json` (defaults: BRD, two
diagrams, three ADRs, five policies — `technical`, `coding`, `security`,
`testing`, and a `custom` `qa-sync` one stating the ownership rule — two
knowledge sources pinned to the SHAs the seed header records, the epics and
modules) and `deploy/seed/sample-product/cases.json` (the cases). Both are generated by
`deploy/seed/sample-product/build-seed.mjs <source-repo-checkout>` (Node ≥ 22, the
source repo's own toolchain; the header of each file records the source commit
SHA). The generator also **fails** if any text matches a secret pattern, and strips
backticks, angle brackets, line breaks and a leading `#` from every text
field it copies (round 1, SEC-9/10: these strings reach agent prompts through
`20-stories.md`). A refresh must be reviewed as a diff. TC-407 screens the
whole file with the existing egress detector (`egress.go:466`).

`deploy/seed/sample-product/seed.sh` runs **on the VM**, against the tunnel listener
(`http://${GAOS_TUNNEL_IP}:18301/api/v1`, not the public edge), reads the admin
token from its file, passes the header through a mode-600 temp file
(`curl -H @file`, so it is never in argv), posts the product seed, then the
cases in slices of 200 with `seed:true` (9 slices), then reads
`/qa/summary` and **asserts the totals are at least the seed's** (§7, TC-411): later QA
writes add cases, epics and script facts, so equality would turn a healthy
re-run into a FAIL. Re-running changes
nothing.

### 4.10 Outbound sync and the BO tab

`dashboard/qaclient.go`. Config read at boot: `GAOS_QA_URL` and
`GAOS_QA_TOKEN_FILE`. The URL is valid if `https`, or `http` with an IP-literal
host in loopback or the WireGuard subnet `10.10.0.0/24` (and not the gateway or
database addresses); no userinfo. Otherwise or if the token is absent/short: **unconfigured**, logged.
The URL is operator config, never request input (no SSRF). Client: 15 s per
call, 60 s per click, every redirect refused, ≤ 2 MiB response, query built with
`url.Values`, the pull cursor length-checked (§4.2).

Two BO routes, behind the cookie gate (§8), both through `s.mut(r)` (refused in
replay):

- Both routes refuse (409 `qa-out-of-scope`) any product not listed in
  `GAOS_API_QA_PRODUCTS`, so a click cannot send another product's catalog to
  the QA origin or apply the QA tools' feed to another product.
- `POST /products/{id}/qa/sync/pull` — pages `GET {QA}/api/v1/gaos/results?product=<id>&after=<cursor>&limit=200`
  (≤ 20 pages; a page whose `product` does not echo the id is refused), applies each page through `QaApplyResults` (stale/unknown
  rejections counted in `Note`), stores the last `next`, records via `QaRecordSync`.
- `POST /products/{id}/qa/sync/push` — sends cases with `catRev` above the
  push cursor in batches of ≤ 200 to `POST {QA}/api/v1/gaos/catalog`, advances
  the cursor per 2xx batch, stops at the first non-2xx; ids in the response's
  `rejected[]` that match `^TC-[A-Z]+-[0-9]{3}$` and exist in the catalog are
  kept in `Push.Rejected` (first 20) and `RejectedN`, shown on
  the tab, and are terminal until their `catRev` changes.
- One sync **in the whole process** at a time (409 `sync-running`) and a **30 s
  cooldown** between starts (global, so iterating product ids does not get
  around it) (409 `cooldown`, `Retry-After`), bounding what a forged cookie
  can make GAOS do. Unconfigured → 409 `qa-unconfigured`; the buttons render
  disabled with that reason as visible text. These locks do not touch the API
  routes: inbound results and a pull coexist via the stale rule.

The tab is the existing pattern: `GET /products/{id}?tab=qa` through `buildBO`
(an `if tab == "qa"` beside `handlers_bo.go:292`), a new `Tab == "qa"` branch in
`templates/backoffice.html:76-83,204`, a new fragment file
`templates/qa.html`, and new selectors prefixed `.qa-` in `static/app.css`.
Content: totals, a per-epic table (coverage, `regressed` marker), the
needs-decision list, last pull/push lines with `RejectedN`, the two buttons.
Every value from the QA tools (`spec`, `runRef`, `Note`) is printed through
`html/template` auto-escaping only — never `template.HTML`.

### 4.11 The contract for the QA tools (implemented there, later)

GAOS **calls**: `GET /api/v1/gaos/results?after=<opaque ≤128>&limit=<≤200>` →
`{"items":[{caseId,implemented?,spec?,result?,runAt?,runRef?}],"next":"<opaque>"|null}`;
`POST /api/v1/gaos/catalog` with `{"product":{id,name,repo},"cases":[§4.7 case, planning fields],"catHead":n}`
→ `{"accepted":n,"rejected":[{"caseId","reason"}]}`; bearer `GAOS_QA_TOKEN`.

GAOS **serves**: the `qa`-scope routes of §4.7 at
`https://gaos.sample-product.com/api/v1`. The QA tools may use either direction or
both; both end in the same two store methods. The published contract page
(`pustaka/docs/guide/qa-sync-api.html`) shows one `curl` per call with
`<TOKEN>` placeholders and no internal addresses.

### 4.12 Cases created on the QA side, and the kanban (v5)

The product owner's addition: in the QA tools' `/products`, a product can be
flagged **integrated with GAOS**; test cases created there then appear in
sample-product's board in GAOS, and are kept up to date, without a human step. The QA
tools' own design lives in that repository (`docs/designs/GAOS_INTEGRATION.md`);
this is the GAOS half, and what the QA token may now do is listed in §8.

**Creation by the QA tools** — `POST /v1/products/{id}/qa/cases` (QA scope; the
admin token also passes, §4.7; product-scoped; one write in flight; ≤ 200 cases;
strict JSON; answers 200), the write twin of the `GET` on that path:

```json
{"source":"qa-automation-tools",
 "epics":[{"code":"AUTH","title":"Authentication","service":"his-backend","source":"docs/test-catalog/his-backend/auth.md","module":"A13"}],
 "cases":[ <§4.7 case: planning fields, plus implemented/spec> ]}
```
→ `{"created":n,"unchanged":n,"rejected":[{"caseId","reason"}],"epicsAdded":n,"cardsAdded":n,"cardsDeferred":"","catHead":n}`.

**Per case, create-only.** A new `caseId` is created if it validates (§4.1) and
passes these extra rules, each a refusal code: `decision-not-allowed` unless
`decision` is `planned` (the QA side may not park or retire a case) and `reason`
is empty; `use-results` if `result`/`runAt`/`runRef` is non-zero; `bad-case-epic`
unless the id's infix equals its `epic` (`TC-AUTH-001` belongs to `AUTH`);
`case-limit` past 5,000 cases for the product. It carries `implemented`/`spec`
(validated). An existing `caseId` with identical planning fields is `unchanged`;
with different ones it is refused `exists-differs` and **not changed** — GAOS
owns the planning half of every case that exists. Nothing is ever deleted.

**Every text field of the request is screened server-side**, like the seed's
(§4.9): the egress secret detector over each string (a hit → 400
`secret-in-body`, nothing written), and backticks, angle brackets, line breaks
and a leading `#` stripped from `scenario` and from the epic's `title`,
`service` and `source`.

**Epics.** A case whose `epic` is not in `Qa.Epics` creates it. The `epics`
array is read **only for a code that does not exist yet**; an entry for an
existing code is ignored (GAOS owns it; never overwritten by this route). An
unknown epic with no entry is titled by its code. Caps (BR-53): at most **5 new
epics per request** (more → `epic-limit` on those cases) and **100 epics per
product** (`epic-limit`).

**Cards — born blocked, never released by the QA side.** For each new epic, if
the desk has a current workflow **and** that workflow has a review task (the one
with the lowest ordinal), and the floor is **not parked**, GAOS adds one card,
"Automate: <title>": kind `implement`, role `qa`, story = the epic's module
story (else the Plan lane), dependent on that review task. It is **always born
`BLOCKED`**, `BlockReason` "added by the QA catalog; plan its design and review
before unblocking", rule `qa-epic-added`, by a **new single-task routine** that
touches only that task (never the all-tasks loop `CreateWorkflow` runs, never
`replayAtSeq`). It is announced by one `TaskPlanned` event carrying that task
and one `TaskMoved` event. Only an operator's board move (`unblock`) releases it;
the scheduler cannot, so a QA token can never create dispatchable agent work
(BR-46). The card does not become a prerequisite of the Release task and does not
bump `Story.Tasks`; both are the operator's to plan. Without a workflow or review,
or while parked, the epic is recorded and `cardsAdded` is 0, `cardsDeferred`
says `no-workflow` or `replay`; there is no backfill, and `qa/summary` lists
`noCard`, the epics with no card, so the tab can say so. An epic never gets a
second card. The card's id is stored on the epic (`QaEpic.Task`).

**`TaskPlanned` now means "a plan, or an addition to one".** The task fold
continues a workflow's ordinals instead of restarting them at 0 (ordinal = the
count of tasks already in that workflow plus the index); a first plan is
unchanged. `taskfold.go` and the data-model document say so.

**The kanban follows the catalog (display only).** `BoardCard` gains an
optional `qa` object for an epic's card — `{epic, cases, automated, fail, pct}` —
filled at read time from the catalog through `QaEpic.Task`. Every case the QA
tools create, and every script fact they report, therefore shows on the card at
once, with nothing written to the task. The seed stores `QaEpic.Task` for the
epics it plans (a second import after step 9).

**Persistence.** A write that adds a card builds `Floor.Desks` from the **live**
desk slice (`s.desks`), never from `s.result`/`s.floor()` (which return the
folded view while parked, `store_mem.go:578-583`), so `persist`'s existing task
loop writes the card and its `DEPENDS_ON` edge. A write that adds no card keeps
the empty floor (§4.4).

**An existing defect, fixed on the way.** `CreateWorkflow`'s gate loop
(`store_mem.go:2066-2078`) walks every task of every product and re-blocks any
implementation task with a review dependency, whether or not the review cleared.
The seed's `CreateWorkflow` would run it. It is restricted to the tasks of the
workflow being created (TC-396 asserts another product's in-flight task keeps
its status and reason); its `replayAtSeq` reset stays (it is the wizard's
documented behaviour).

## 5. Change list (every file)

New: `dashboard/qacatalog.go` (types, vocabularies, validation, effective
status, summary); `dashboard/qacatalog_store.go` (MemStore methods);
`dashboard/store_arcade_qa.go` (forwards, persist, load); `dashboard/schema/16-qa-catalog.sql`;
`dashboard/api_v1.go` (auth, scoping, routes, seed handler);
`dashboard/qaclient.go`; `dashboard/handlers_qa.go` (two BO POSTs + the tab
view-model); `dashboard/templates/qa.html`; `deploy/seed/sample-product/{build-seed.mjs,seed.sh,product.json,cases.json}`;
tests `dashboard/{qacatalog,api_v1,qaclient,seed}_test.go`;
`pustaka/docs/guide/qa-sync-api.html`; `docs/designs/reviews/sample-product-qa-sync-review-1.md`.

Modified: `dashboard/store.go` (seam, `Result.QaCases`, `readOnly`);
`dashboard/model.go` (`Master.Qa`, `ProductDraft.Priority`);
`dashboard/store_mem.go` (desk seat in `CreateWorkflow`; `CreateProduct`
priority); `Qa` in `Master.Clone` (`model.go:368`); `dashboard/store_arcade.go` (persist/load `qaState`,
cases); `dashboard/main.go` (routes, boot config); `dashboard/handlers_bo.go`
(`tab == "qa"`); `dashboard/handlers_cw.go` and `dashboard/handlers_tasks.go`
(`requireBO` on `POST /workflows` and on task `dispatch|approve|retry|cancel`);
`dashboard/egress.go` (a `gaos_<hex>` token pattern); `dashboard/templates/backoffice.html` (tab link + include);
`dashboard/static/app.css` (`.qa-*`); `dashboard/schema_test.go`
(29/38); `dashboard/store_conformance_test.go` (one case: the catalog
survives a restart — no new TC, covered by TC-124/125);
`artifacts/v1/gaos-arcadedb-model.html` (type counts);
`.gitignore` and `deploy/check-stacks.sh` (the pattern `deploy/stacks/prod.api-*`
joins the "no secret is committed" loop; the three files must be regular
files); `.claude/rules/agent-workspace.md` and `docs/rules/gaos-design-review.md`
(the reviewer deny-list gains `*.api-*`); `deploy/stacks/compose.prod.yaml`
(mount `prod.api-admin`, `prod.api-qa`, `prod.api-qa-out` read-only; pass
`GAOS_API_QA_PRODUCTS`, `GAOS_QA_URL`; `*_FILE` paths);
`deploy/stacks/dashboard.env.example` and `deploy/stacks/README` lines for the
new variables; `deploy/check-workspace.sh` and `README.md` (the literal counts it
greps); `.claude/rules/qa-test-plan.md` (its stated counts, range map and the stale
"three hundred and eighty-one" at lines 73-74);
pustaka: `delivery/implementation-plan.html`, `product/qa-test-plan.html`, `decision-log.html`, `brd.html`,
`open-questions.html` (including its "thirty-two in all", `brd.html:1645` and
`assets/toc/product.js:78` count sites), `changelog.html`, `assets/toc.js`, `assets/toc/product.js`,
`assets/toc/guide.js`.

**Variables (five):** `GAOS_API_ADMIN_TOKEN_FILE`, `GAOS_API_QA_TOKEN_FILE`,
`GAOS_API_QA_PRODUCTS`, `GAOS_QA_URL`, `GAOS_QA_TOKEN_FILE`.

**Registers (checked against the files, round 1).** Decisions: **D-072**
ownership split and the REST surface; **D-073** the catalog as its own vertex,
never a task (and N5/BR-54); **D-074** the generic product seed, sample-product data
outside the binary, the gate not cleared; **D-075** the outbound origin and the
two buttons; **D-076** the desk fix. D-071 is taken
(`decision-log.html:1404`). Log: seventy-one → **seventy-six** entries. Rules:
**BR-51** one writer per field, newest run wins inside the automation half;
**BR-52** the API is bearer-authenticated, fail-closed, product-scoped for the
QA token; **BR-53** every batch is bounded, and so are the epics, cards and cases a QA token can add; **BR-54** QA data never moves a task (it may add one card for a new epic,
§4.12). BRD: "Fifty rules" → **fifty-four**. Open questions:
**OQ-33** the QA tools' endpoints are a contract only; **OQ-34** who may edit a
case's decision (needs BR-48's identity); **OQ-35** whether results should ever
move a card. OQ: "thirty-two" → **thirty-five**. Delivery plan
(`pustaka/docs/delivery/implementation-plan.html`; T-189…T-191 are M17.5's):
a new milestone **M18** with **T-192…T-196**, one per phase P1…P5 of §10.

## 6. Deploy and migration

- The migration is `16-qa-catalog.sql`, applied by the new image's boot; purely
  additive. The old image ignores `qaState`, `QaCase` and the extra desk.
- Tag `v0.37.0`; deploy on rnd-v3-vpn with the override line in the
  second-prod notes (checkout `/home/sample-product/devel/projects/gaos`).
- Secrets: the admin and QA tokens are generated on the VM
  (`printf 'gaos_%s' "$(openssl rand -hex 32)"`, mode 600, gitignored
  `deploy/stacks/prod.api-admin|api-qa|api-qa-out`, all three created — the
  outbound one empty — because a missing bind-mount source would be created by
  Docker as a root-owned directory), never printed or put in argv, and not
  committed (`.gitignore`, `check-stacks.sh`). The outbound token file stays empty until the QA
  tools implement §4.11, so the buttons render disabled. To disable the admin
  token after seeding, empty its file and restart the dashboard (tokens are read
  at boot); admin routes then answer 401/503.
- After healthy: `deploy/seed/sample-product/seed.sh` on the VM (tunnel listener).
- Order: deploy → seed (prints the product id) → set `GAOS_API_QA_PRODUCTS=<id>`
  → restart. Verification: `seed.sh` asserts the §7 totals and then repeats the
  summary read through the public edge with the QA token, header from a mode-600
  file; the BO shows sample-product with the tab.

## 7. Tests (one `func Test…` each; ids follow TC-390)

| TC | Test | Asserts |
|---|---|---|
| TC-391 | `TestQaCaseVocabularyIsClosed` | each out-of-vocabulary value, bad pattern, over-cap text, bad reason → rejected |
| TC-392 | `TestEffectiveStatusAndNeedsDecision` | planned+implemented → automated; blocked+implemented → blocked and listed |
| TC-393 | `TestImportAndResultsOwnTheirOwnHalf` | import cannot change automation; results with a planning field → decode error; `seed` creates with `implemented`/`spec` only for a new case |
| TC-394 | `TestAnIdenticalRepostChangesNothing` | no revision moved, no event emitted |
| TC-395 | `TestResultsRejectUnknownStaleAndFuture` | `unknown-case`, `stale`, `future` (> now+300 s), a `result` without `runAt` rejected; equal run = unchanged; `force` works only with admin; valid items applied |
| TC-396 | `TestACreatedProductGetsADeskAndCanBePlanned` | desk seated; tasks `PENDING`/gate-`BLOCKED`; seeded products unchanged; parked floor with a runtime desk answers; desk order survives `p-10`; `POST /workflows` and task routes without a cookie → 401 |
| TC-397 | `TestAPIFailsClosed` | no tokens → 503; missing → 401; QA token on admin route → 403; a token without the `gaos_` prefix, short, empty or unreadable is unset; equal tokens disable the API; a request with `X-Forwarded-Prefix` → 404; a second concurrent write → 429; no token in any body |
| TC-398 | `TestAPIQaTokenIsScopedToItsProducts` | other product → 404; `/v1/products` filtered; `GAOS_API_QA_PRODUCTS` unset → nothing; a name in the list matches nothing |
| TC-399 | `TestAPILimitsAndStrictDecoding` | 201 items → 413; body > 1 MiB → 413; unknown field → 400; stripped `/v1` path works |
| TC-400 | `TestAPICasesCursorCoversEveryCaseOnce` | pages by `catRev`; `next` null on a short page; no case twice or missed |
| TC-401 | `TestQaWritesNeverTouchTheFloorInReplay` | parked floor: `/v1` results/catalog → 200 with an empty `Result.Floor`, live desks and the `Workflow` writes unchanged; the BO sync route and the seed → refused (409 `replay`) |
| TC-402 | `TestQaClientURLValidation` | https ok; http+10.x ok; http+hostname, userinfo refused; cursor > 128 refused |
| TC-403 | `TestPullAppliesPagesAndKeepsTheCursor` | `httptest` QA tools: pages applied, cursor stored, stale counted |
| TC-404 | `TestPushAdvancesPerBatchAndKeepsRejections` | cursor per 2xx batch; stops at 500; rejected ids kept (≤20) |
| TC-405 | `TestSyncRefusesRedirectsOverlapAndCooldown` | redirect not followed; `sync-running` and `cooldown` are process-wide; unconfigured → 409; a product outside `GAOS_API_QA_PRODUCTS` → `qa-out-of-scope`; a pull page that does not echo the product is refused |
| TC-406 | `TestProductSeedIsIdempotentAndMeetsTheFloor` | twice = same; operator-edited BRD/policy untouched; a same-named unseeded product → 409; seed text with a secret → 400 and stripped markup; score ≥ `PlanFloor`; `2 + epics + 1` tasks; Release gated; none `DONE`; refused in replay |
| TC-407 | `TestThesample-productSeedIsWellFormed` | the committed files: 39 modules, 444 features, 47 epics, 1,700 unique cases, 494 implemented; patterns; every epic/module resolves; egress detector finds nothing |
| TC-408 | `TestQaToolsMayCreateButNeverChangeACase` | QA scope (admin also passes); product-scoped; a new valid case is created `planned`; park/retire, reason, run fields, wrong epic infix, `case-limit` refused with their codes; identical existing = `unchanged`; differing existing = `exists-differs` and unchanged; the `epics` entry for an existing code is ignored; a secret in any text → 400 and nothing written; markup stripped; strict JSON; 201 cases → 413 |
| TC-409 | `TestANewEpicGetsOneBlockedCardAndNothingElseMoves` | a new epic adds exactly one `Automate:` card, always `BLOCKED` with the QA reason, with one `TaskPlanned` and one `TaskMoved`; `QaEpic.Task` set; the Plan lane without a module; the review having already cleared does not release it; no existing task changes status or reason, including an in-flight gated task of another product; `CreateWorkflow`'s gate loop touches only its own workflow; a second case in the epic adds no card; no workflow or review → `cardsAdded:0` and `no-workflow`; parked floor → card deferred (`replay`) and the live desks unchanged; > 5 new epics per request and > 100 per product → `epic-limit`; the fold continues ordinals; the card persists (live desk) |
| TC-410 | `TestTheBoardCardCarriesLiveCaseCounts` | `BoardCard.qa` equals the epic's roll-up and moves with every created case and every result; cards of other epics and non-epic cards carry none; the seed stores `QaEpic.Task` for every epic; `qa/summary.noCard` lists an epic with no card |
| TC-411 | `deploy/seed/sample-product/seed.sh` | on the VM: ends `ok  summary matches` or `FAIL <what>` after asserting the totals are at least the seed's |

Totals: register **390 → 411** ("four hundred and eleven"); Go tests
**273 → 293** (20 new, TC-391…TC-410); dashboard **251 → 271**; pustaka 22. The
persistence case in the conformance table needs no row.

## 8. Security considerations

- **New inbound boundary** `/v1`: bearer, two scopes, product-scoped QA token,
  fail-closed, constant-time, files not env values, equal-token lockout (§4.7).
- **New outbound origin** `GAOS_QA_URL` (§4.10): operator config, validated,
  no redirects, bounded, own token.
- **The two BO sync routes sit behind `signedIn`, which accepts any cookie
  `gaos_bo=1`** (`handlers_bo.go:31-34`; `deploy/edge/gaos.conf:135-161` adds
  no edge auth). Anyone who can reach the dashboard can therefore click them.
  Accepted, with bounds: they reach only the one configured origin and only for
  products in `GAOS_API_QA_PRODUCTS`, a process-wide single flight and 30 s
  cooldown cap the rate, they are inert until the QA tools exist (N1), and what
  comes back passes the validation of the API and can add at most blocked
  cards for new epics (BR-54, §4.12). This is the same blast radius as every BO write (BR-48's open
  precondition); closing it is OQ-34's prerequisite, not this change.
- **Existing ungated routes.** `POST /workflows` (`main.go:352`) and the task
  routes `dispatch|approve|retry|cancel` (`main.go:310-313`) take no cookie
  check at all. The desk fix would extend `POST /workflows` to new products, so
  this change adds `requireBO` to it and to those four (§4.8) — this is the
  one fix to existing security behaviour in scope, because it is what makes the
  desk fix safe. Still open and **not** widened here: `GET /replay/{workflow}`
  (`main.go:309`, a state-changing GET, unauthenticated; it no longer affects
  the machine API, §4.4) and `GET /tasks/{id}/context` (read-only).
- **The desk fix widens** what a forged cookie can plan to any BO-made
  product, once those routes are cookie-gated; dispatch also needs
  `GAOS_RUNNER`, which is off in prod (§4.8).
- **The seed is admin-only and idempotent**, never run at boot, refuses to adopt
  an unseeded product of the same name, and screens every text field
  server-side (§4.9). The generator's sanitising is a convenience, not the
  control.
- **Tokens** are files with a `gaos_` prefix so GAOS's detectors can see them
  (`egress.go`, `check-workspace.sh`); the files are gitignored, checked by
  `check-stacks.sh` and on the reviewer deny-list. The reviewer deny-list is
  otherwise still enforced by instruction only (round 1, ESCALATE).
- **What the QA token can do** (v5; read this before rotating or leaking it):
  write automation facts of existing cases; **create** new `planned` cases
  (never change or delete one, never park or retire one); create epics (≤ 5 per
  request, ≤ 100 per product) and, for each, **one `BLOCKED` card** in the
  product's current workflow, with its `TaskPlanned`/`TaskMoved` events, up to
  5,000 cases per product. It cannot move or release any task, change a
  decision, a policy or the BRD, or touch another product. Every text it sends is
  screened and stripped server-side (§4.12). To revoke it: empty
  `deploy/stacks/prod.api-qa` and restart the dashboard.
- The catalog holds test scenarios, not patient data (the source repo forbids
  PII in its catalog).

## 9. Open points carried into the registers

OQ-33, OQ-34, OQ-35 (§5). Licensing/ownership of the sample-product catalog files in
this repository: they are the product owner's own and ride in `deploy/seed/`
exactly as the `gaos.sample-product.com` host overrides already do; recorded under
D-074.

## 10. Phases and exit conditions

| Phase | Work | Exit |
|---|---|---|
| P1 | schema, `qacatalog*.go`, seam, ArcadeStore persist/load, desk fix, tests 391-396 + conformance case | `cd dashboard && go vet ./... && go test ./...` green |
| P2 | `api_v1.go`, routes, tests 397-401 | same, plus a request through the stripped path |
| P3 | seed handler, generator, seed files, tests 406-407, `seed.sh` | same; `node build-seed.mjs <checkout> \| diff` against the committed files is empty |
| P4 | `qaclient.go`, BO routes, tab, tests 402-405 | same; `gaos-frontend` review of the tab (break-ui worst case) |
| P5 | pustaka pages and registers, counts, `check-workspace.sh` | `cd pustaka && go run . check ./docs && go test ./...`; `./deploy/check-workspace.sh` and `./deploy/check-stacks.sh` pass |
