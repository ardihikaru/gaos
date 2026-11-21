# Answer from anywhere: the QA tool answers its own inbox items, and every answer records where it came from

**Status:** Approved (v3). Round 1 reviewed v1 (`reviews/answer-source-*-1.md`, triage `reviews/answer-source-review-1.md`); round 2 reviewed v2 with the four lenses that had raised a Blocker or a Critical (`reviews/answer-source-*-2.md`, triage `reviews/answer-source-review-2.md`). Round 2 raised no Blocker and no Critical or High; v3 applies its fixes. Approval in §11.
**Feature:** `answer-source`
**Version target:** the next free minor after `main` at merge time (v0.43.0 if nothing else lands first).
**Decision number claimed:** D-083 (§3.10). D-082 is the newest row (`pustaka/docs/product/decision-log.html:1683-1684`).
**Registers this changes:** a new **BR-57** in BRD §9.8 `#rules-qa-sync` (`pustaka/docs/product/brd.html:1333`). **BR-52** reworded (§3.10). **BR-48**'s "events carry no actor" narrowed (§3.10). **BR-05** is *not* opened: no event type is added (§3.3).
**Rules this must obey:** BR-02 (every change emits an event), BR-04 (append-only), BR-05 (closed catalog), BR-52 (the machine API answers only a bearer read from a file and fails closed, `brd.html:1345`), BR-53 (every text field of a request is screened, `brd.html:1346`), BR-55 (a back-office session is verified, `brd.html:1348`), D-078 (the QA tool's inbox, `docs/designs/qa-inbox-api.md`).
**Security lens:** required — a new machine route, and a mapping from a bearer token to a client identity.

## 0. Glossary

- **External item** — a question a machine client put into GAOS's "Needs you" inbox through `POST /v1/products/{id}/inbox` (D-078). Its record is three events: `ExternalQuestionAsked`, `ExternalQuestionAnswered` (an answer, or the expiry), `ExternalAnswerDelivery` (`dashboard/external_inbox.go:20-22`). Its projection is `externalItemsLocked` (`dashboard/external_inbox.go:92-130`). Its identity is (product, externalId) (`external_inbox.go:88`), and this design keeps that.
- **Task question** — an AI agent's question; its task waits in `AWAITING_INPUT` (`dashboard/questions.go:11-23`). Answered only in the back office, event `QuestionAnswered` (`questions.go:190-211`).
- **QA tool** — the `qa-automation-tools` service, public host `qa.example.org` (the product owner's brief; GAOS reaches it on the WireGuard tunnel at an IP, `deploy/rebuild-restart.sh:102`, `pustaka/docs/infra/environments.html:205`). It calls GAOS with the QA bearer (`GAOS_API_QA_TOKEN_FILE`, product-scoped by `GAOS_API_QA_PRODUCTS`, `dashboard/api_v1.go:126-131`). GAOS calls it back at `GAOS_QA_URL` with a different bearer (`GAOS_QA_TOKEN_FILE`, `dashboard/qaclient.go:93-103`).
- **The answer webhook** — after an external item is answered or expires, GAOS POSTs the decision to `{GAOS_QA_URL}/api/v1/gaos/inbox-answer` (`dashboard/inbox_worker.go:12-33`). The QA tool de-duplicates on `externalId`: first answer wins (`docs/designs/qa-inbox-api.md` §3.6, §7).
- **Back office (BO)** — the signed-in operator console; the session is verified per request (`dashboard/auth.go:527-567`), its email is read with `actorOf` (`dashboard/handlers_conversation.go:80`). **`GAOS_BO_LABEL`** is the deployment's label (`auth.go:259`), passed from the deploy shell (`deploy/stacks/compose.prod.yaml:130`); the operator sets it to the host name (`docs/designs/bo-login.md:214`; `gaos.example.org` on the second prod is the operator's setting, not a repository value); it defaults to `gaos` outside prod (`auth.go:271-276`). It is held as `s.auth.label`.
- **Client** — a machine that calls the API with its own bearer token and has a name, a kind and an origin. Today there is one: `qa-tools`.
- **Origin tag** — the existing `QuestionPayload.Source` field (JSON `source`, `questions.go:70`), set to `"QA tool"` on every external item (`external_inbox.go:28,193`) and printed as the tag on the inbox row and the answer sheet (`InboxRow.Source`, `dashboard/view.go:188`; `dashboard/templates/needs.html:31,75`). It says **who asked**. It is unchanged in meaning; this design sets it per client (§3.3). In this document "origin tag" always means this field.
- **Answer source** — the new `AnswerSource` (§3.1): **who answered**. It is stored as `answeredBy` in the payload and returned as `source` in the machine API's bodies. The two are different JSON documents (the event payload and the API response), so the payload's `source` (origin tag) and the API's `source` (answer source) never meet in one object.

## 1. Problem

The product owner's request (relayed by the orchestrating agent on 2026-11-21 — a conversation message, not a repository file, labelled here as such): when something waits for approval in the QA tool, its `/history` page gets a button to answer it, and the answer goes to GAOS. GAOS needs a REST route for that, and GAOS must log **where each answer came from**: GAOS itself, the QA tool (with its domain), or another platform later (a Hikaru app, the Hikaru Android app). The same brief fixes the route, its body and responses (§3.4), and asks that clients be "a small registry (config: client name, kind, origin/domain, token file), so a future hikaru-app or hikaru-android client is added by configuration plus a token file, not code."

Today:

1. The only way to answer an external item is the back office: `POST /products/{id}/inbox/{externalId}/answer` behind the session gate (`dashboard/handlers_questions.go:103-136`, registered `dashboard/main.go:390`). The machine API has only ask and poll (`dashboard/api_v1.go:277-278`).
2. No answer records its source. `ExternalQuestionAnswered` carries choice, text and time (`external_inbox.go:242-245`); `QuestionAnswered` likewise (`questions.go:204-207`).
3. The machine API knows two principals, `scopeQA` and `scopeAdmin` (`api_v1.go:33-39`). It has no notion of a named client.

## 2. Goals and non-goals

**Goals**

- G1. A machine route that lets a client answer an external item **it asked**, with the same validation as the back office and the same two-pass secret screening as the ask route.
- G2. Every answer — back office or machine — records an answer source `{kind, client, origin, actor}` on its existing event, derived on the server; only `actor` comes from the client, and it is recorded as reported.
- G3. Clients are a small **registry**: a future **inbox-only** client (it asks, polls and answers its own items) is added by a configuration line and a token file, with no code change. Everything the inbox does per client — the origin tag, the open-item cap, the id ownership, the webhook — follows the asking client (§3.3), so a second client is safe on day one. The Hikaru apps will also need §8's route for task questions; the registry is the identity half of that, not all of it.
- G4. The answer source is shown: in a new "Answered recently" list under the inbox (the brief asks for it in "the Needs-you inbox history"), in the timeline, in the answer sheet's refusal, and in the card's conversation sheet. An old event without one shows **"source not recorded"**.
- G5. Answering through the API behaves like answering in the back office in every other respect: the same `ExternalQuestionAnswered`, and — for the QA tool's items — the same webhook delivery.

**Non-goals**

- N1. No new event type, task status or schema statement (§3.3).
- N2. No machine route for **task** questions. It is the next extension, for the Hikaru apps (§8).
- N3. The webhook body is unchanged (`inbox_worker.go:39-47`). The QA tool reads the source with GET (§7).
- N4. The ask route's body, limits and 201/200 semantics for its own items are unchanged (`dashboard/api_inbox.go:128-180`). Two things change: an id another client already uses is refused (§3.3), and the open-item cap counts per asker.
- N5. No per-client rate limit beyond the existing one-write-in-flight per principal (`api_v1.go:225-236`).
- N6. No deploy, no VM change. Setting `GAOS_QA_ORIGIN` on the redeploy line is an operator step after merge (§3.6).
- N7. No per-client webhook. A registry client polls GET; only the QA tool's items are pushed (§3.3).

## 3. Design

### 3.1 The answer source

New file `dashboard/answer_source.go`:

```go
type AnswerSource struct {
    Kind   string `json:"kind"`   // gaos | qa-tool | <a registry client's kind>
    Client string `json:"client"` // back-office | qa-tools | <registry name>
    Origin string `json:"origin"` // a host name or the BO label; "" when not configured
    Actor  string `json:"actor"`  // BO: the session email; a client: actor.username as reported
}
```

`QuestionPayload` (`questions.go:53-74`) gains two `omitempty` fields:

- `AnsweredBy *AnswerSource json:"answeredBy,omitempty"` — set on `QuestionAnswered` and on `ExternalQuestionAnswered` with status `answered`, **only when the caller passed a source with a non-empty `Kind`** (one rule for both answer paths). Nil on an expiry and on every event written before this change.
- `AskedBy string json:"askedBy,omitempty"` — set on `ExternalQuestionAsked`: the asking client's name. **The store normalises** an empty `ExternalAsk.AskedBy` to `qa-tools` and an empty `ExternalAsk.Label` to `ExternalSource` (`"QA tool"`) before any comparison, cap count or emit, so every existing caller and test that leaves them empty behaves exactly as today (`external_inbox_test.go:188-197,515` pass unchanged). The admin token, which reaches the ask route today (`docs/designs/qa-inbox-api.md` §3.4), asks **as `qa-tools`**: its items are the QA tool's, are pushed to the QA tool, and the QA tool can read and answer them — exactly today's behaviour. **An item asked before this change has no `askedBy` and is read as `qa-tools`**, which is exactly who could have asked it (the QA token or the admin token acting as it).

The helper `effectiveAsker(x) string` returns `AskedBy`, or `qa-tools` when empty; every rule below uses it.

`EventPayload.Clone` (`dashboard/model.go:291-322`) copies `AnsweredBy` deeply (a pointer inside `Question`).

The fixed values:

| Where the answer came from | kind | client | origin | actor |
|---|---|---|---|---|
| The back office | `gaos` | `back-office` | `s.auth.label` (`GAOS_BO_LABEL`; `gaos` when unset outside prod), as configured | the session's email (`actorOf`) |
| The QA token | `qa-tool` | `qa-tools` | §3.6 | `actor.username` from the body |
| A registry client | its `kind` | its `name` | its `origin` | `actor.username` from the body |

**Origin is a registered value, not an observed one.** It names the client's configured host; an answer made with a leaked QA token from anywhere still reads "via QA tool (qa.example.org)". The source proves which token answered, nothing about the network. When the route writes an answer it logs, to the server log only, `api: inbox answer <product>/<externalId> by <client> peer=<RemoteAddr> xff="<X-Forwarded-For as received, inboxText-cleaned, clipped to 200>"`. Every `/api` call arrives through Traefik (`dashboard/api_v1.go:22-26`), so `peer` is the proxy; Traefik keeps a received `X-Forwarded-For` only from the WireGuard tunnel and appends its own peer (`deploy/stacks/traefik.yml:18-42`), so the right-most `xff` entry is the hop that reached Traefik and earlier entries are only as trustworthy as the edge that wrote them. It is a lead for an investigation, not proof.

**The phrase**, `(*AnswerSource).Phrase() string` (nil-safe):

| Source | Phrase |
|---|---|
| nil, or `Kind == ""` | `""` (empty: the caller prints its own "source not recorded" sentence, below) |
| client `back-office` (kind `gaos`) | `in GAOS (<origin>) by <actor>` |
| client `qa-tools` | `via QA tool (<origin>) by <actor>` |
| any other client | `via <client> (<origin>) by <actor>` |

The label keys on the **client name**, not the kind, so no registry line can print "via QA tool" (the kind `qa-tool` is also reserved, §3.2). The parenthesis is left out when the origin is empty, and ` by <actor>` when the actor is empty.

**Sentences per surface** (the only places the phrase is printed):

| Surface | With a source | Without one |
|---|---|---|
| Event text suffix | ` — <phrase>` | no suffix |
| Refusal (409 reason, toast) | `That question was already answered <phrase>.` | `That question was already answered (source not recorded).` |
| "Answered recently" row | `answered <phrase>` | `answered · source not recorded` |
| Conversation sheet (after `<b>Answered</b> <answer> ·`) | `<phrase>` | `source not recorded` |

**The machine API never returns a back-office email.** In every API body (`source` and the 409 `reason`) an answer source with kind `gaos` is returned with `actor: "operator"`; the event and the back office keep the email. A client's own reported username is returned as stored. (The operator's sign-in identifier is half a credential pair for a public form, BR-55, and the QA tool shows these values on its `/history` page.) The function is `apiSourceOf(*AnswerSource) *apiSource`.

The event's `Payload.Actor` (`model.go:271-274`) is set on both answer events, because it is what fills the queryable `Event.actor` column (`store_arcade.go:1075`, `dashboard/schema/07-events.sql:26`), which was `operator` for every answer before (`actorName`, `dashboard/conversation.go:199-204`). Value: the email for `gaos`; `<client>/<username>` for a client — the `<client>/` prefix marks an identity GAOS did not verify (D-083 says so). `Payload.Actor`'s comment is widened to name the two answer events; the conversation fold reads it only for its own two types (`conversation.go:179,192`).

The answer source is **never** written into an agent's packet: the packet's answer part carries only choice and text (`dashboard/store_mem.go:1445-1451`, `questionBody`, `questions.go:285-311`), both unchanged.

### 3.2 The client registry

`dashboard/api_v1.go`:

```go
type apiClient struct {
    Name, Kind, Origin string
    sum      [sha256.Size]byte
    products map[string]bool  // for qa-tools: the SAME map as apiConfig.qaProducts
    busy     *atomic.Bool     // for qa-tools: &apiConfig.qaBusy; for each registry client its OWN atomic.Bool, allocated by parseClients
    scope    apiScope         // scopeQA for qa-tools, scopeClient for a registry client
    push     bool             // true only for qa-tools: its items are delivered by the webhook
}
```

`apiConfig` (`api_v1.go:47-58`) **keeps** `adminSum, qaSum, hasAdmin, hasQA, qaProducts, adminBusy, qaBusy` and gains `outSum [sha256.Size]byte`, `hasOut bool` and `clients []*apiClient`. `newAPIConfig` computes all three digests **before** it decides `why` (today it returns early on equal tokens, `api_v1.go:109-113`, and never keeps the outbound digest, `:102-123`). The `qa-tools` client aliases `qaProducts` and `qaBusy`, so `handlers_qa.go:86` and the tests that toggle `srv.api.qaBusy` (`api_v1_test.go:111,118`, `external_inbox_test.go:139,143`) are unchanged.

Signatures:

- `newAPIConfig(admin, qa, out, products string) *apiConfig` — **unchanged signature**. When `qa` is set and no built-in tokens are equal it appends the `qa-tools` client with `Origin ""`. The nineteen existing test calls (`api_v1_test.go` 11, `qacatalog_create_test.go` 5, `seed_test.go` 2, `qaclient_test.go` 1) are unchanged.
- `(c *apiConfig) setQAOrigin(o string)` — sets the `qa-tools` client's origin.
- `parseClients(r io.Reader, readToken func(path string) string, logf func(string, ...any)) []*apiClient` — parses the registry file (rules below) without the collision check, and gives each client its own `atomic.Bool`.
- `(c *apiConfig) addClients(cs []*apiClient, reservedOrigins []string, logf func(string, ...any))` — **does nothing when `why` names equal built-in tokens** (that `why` is final). Otherwise it drops a client whose token digest equals `adminSum`, `qaSum` or `outSum` (each only when set) or an earlier client's; whose origin equals a reserved origin (ASCII case-insensitive, and only when both are non-empty); or whose products overlap `c.qaProducts` (until items are keyed by asker, §4, a registry client may not share a product with the QA tool, so it can never take one of the QA tool's ids). Each drop logs `api: clients file: <name>: <why> (skipped)`. It clears `why` only when `why` is `no API token is configured` and at least one client was added.
- `apiFromEnv`, in this order: `c := newAPIConfig(...)`; `o := qaOrigin(os.Getenv("GAOS_QA_ORIGIN"), os.Getenv("GAOS_QA_URL"), log.Printf)`; `c.setQAOrigin(o)`; when `GAOS_API_CLIENTS_FILE` is set, `c.addClients(parseClients(file, readTokenPathStrict, log.Printf), []string{o, os.Getenv("GAOS_BO_LABEL")}, log.Printf)`; then the boot line.
- `readTokenFile(envName)` is kept (tests at `api_v1_test.go:135,140` call it) and becomes a wrapper over `readTokenPath(path, label string) string`, which also calls `warnIfOpen` (`auth.go:249-253`) — so every built-in token file is permission-checked (a warning, as for the back-office files). Registry lines use `readTokenPathStrict`, which **refuses** a group- or world-readable token file (returns `""`, so the line is skipped): the registry is new and nothing depends on it, so it fails closed.
- `(c *apiConfig) authorize(r) (apiScope, *apiClient)` — hashes the bearer once and compares it in constant time against the admin digest, then every client's. Admin returns `(scopeAdmin, nil)`.
- `(s *server) apiGate(w, r, need apiScope) (apiScope, *apiClient, bool)` — every call site gains the client value.
- `(s *server) apiProduct(w, r, scope apiScope, cl *apiClient) (string, bool)` — **admin reaches any product; otherwise only `cl != nil && cl.products[id]`; anything else is 404.** No branch compares `scope == scopeQA`.
- `(s *server) beginWrite(w, scope apiScope, cl *apiClient)` — takes `cl.busy` for a client, `adminBusy` for admin.
- `GET /v1/products` (`api_v1.go:288-306`) filters by `cl.products` for any non-admin caller.
- `Describe()` becomes `enabled (admin=<bool> qa=<bool> qa-products=<n> clients=<n>)`, where `clients` counts every client including `qa-tools`.

**The scope ladder** becomes `scopeNone < scopeClient < scopeQA < scopeAdmin` (`api_v1.go:35-39`). `apiGate`'s comparison `got < need` (`:194`) is unchanged: a `scopeClient` caller on a QA route is 403.

| Route | Least scope, before | Least scope, after |
|---|---|---|
| `GET /v1/products`, the five `/qa/...` routes, `POST /v1/seed/product` | QA / admin | unchanged (a registry client is 403) |
| `POST /v1/products/{id}/inbox` | QA | **client** |
| `GET /v1/products/{id}/inbox/{externalId}` | QA | **client** |
| `POST /v1/products/{id}/inbox/{externalId}/answer` (new) | — | **client**; the admin token is 403 (§3.4) |

**Where the clients come from** (`apiFromEnv`, `:126-131`):

1. `qa-tools` from the existing variables (unchanged), kind `qa-tool`, origin per §3.6, scope `scopeQA`, `push: true`.
2. A registry file named by **`GAOS_API_CLIENTS_FILE`** (unset = no further clients), read once at boot, `warnIfOpen` applied. One client per line, `#` starts a comment, five whitespace-separated fields:

   ```
   # name          kind            origin                 token file                           products
   hikaru-app      hikaru-app      app.hikaru.example     /run/secrets/api-client-hikaru-app   p-6,p-7
   ```

   - `name` and `kind` match `^[a-z][a-z0-9-]{1,31}$`. Reserved names: `qa-tools`, `back-office`, `admin`, `gaos`, `qa-tool`, `operator`. Reserved kinds: `gaos`, `qa-tool`. Names are unique. **A name is never reused for a different party**: items belong to the name (`askedBy`), so a retired name stays retired (D-083 and the API guide say so).
   - `origin` matches `^[A-Za-z0-9]([A-Za-z0-9.-]{0,251}[A-Za-z0-9])?(:[0-9]{1,5})?$`, or `-` for none.
   - `token file` is read by `readTokenPath` (`gaos_` plus at least 32 of `[0-9A-Za-z]`; empty or malformed = the line is skipped).
   - `products` is a comma list of product ids, or `-` (reaches nothing). A product the QA token reaches is not allowed (above).
   - The host-side token file is `deploy/stacks/prod.api-client-<name>` (inside the gitignored `deploy/stacks/*.api-*`, `.gitignore:9`), mounted at `/run/secrets/api-client-<name>`.
   - Registry clients get `scopeClient` and `push: false`.
   - A malformed line logs `api: clients file line <n>: <why> (skipped)` — line number and reason, never a token. Restart to change.

**Distinct tokens.** BR-52's rule for the three built-in tokens is unchanged and final: two of admin, QA and outbound equal disable the whole API (`api_v1.go:109-113`), and no registry line re-enables it. A registry client whose token equals any principal's is **not loaded**; the rest stays up. With no built-in token configured but a registry client loaded, the API is enabled for that client.

### 3.3 The store

No new event type. Every change is on `MemStore`; `ArcadeStore` delegates through `s.mutate`/`s.proj` as today (`dashboard/store_arcade_blueprint.go:165-189`); `mutate` returns `(Result, error)` (`store_arcade.go:443`), so a method that also returns an item captures it in the closure, as `AskExternal` already does (`store_arcade_blueprint.go:172-183`). `readOnly` (`dashboard/store.go:252-317`) refuses the writes with `replayRefusal`; the new read passes through the embedded store.

1. **Ask** — `ExternalAsk` (`external_inbox.go:48-55`) gains `AskedBy string` and `Label string` (the origin tag: `"QA tool"` for `qa-tools`, else the client's name). `AskExternal` (`:165-199`):
   - an existing (product, externalId) whose `effectiveAsker` equals `in.AskedBy` → returned, nothing written (today's idempotency);
   - an existing one asked by another client → `ErrIDTaken` (a rejection, `That externalId is already used by another client; choose another.`); the handler answers 409 `id-taken` with no item state. This reveals only that the id is in use;
   - the open-item cap (`externalMaxOpen`, 200) counts open items **per (product, asker)**, so one client cannot fill another's inbox;
   - the event's text becomes `<Label> asks: <lead>` and `Source` (the origin tag) is `Label`; the toast `<Label>'s question is in Needs you.`; the cap refusal (`:182`) `The inbox already holds 200 open questions from <Label> for this product; answer some first.`
2. **Answer** — `AnswerExternal` changes signature:

   ```go
   type ExternalAnswer struct {
       Choice int
       Text   string
       By     AnswerSource // By.Kind == "gaos": the back office, any item; otherwise only an item By.Client asked
   }
   AnswerExternal(ctx, productID, externalID string, in ExternalAnswer) (Result, error)

   // AnswerClosed is the refusal for an item that is no longer open. It satisfies
   // errors.Is(err, ErrRejected), so existing handler mappings keep working, and
   // carries the item for the API's 409/410 bodies.
   type AnswerClosed struct { Item ExternalItem; Expired bool; reason string }
   ```

   Under the store's write lock, in this order:
   1. Unknown item, or `By.Kind != "gaos"` and `effectiveAsker(item) != By.Client` → `ErrNotFound`.
   2. Already answered → `*AnswerClosed{Item, Expired:false}` with the refusal sentence of §3.1.
   3. Expired, or `lapsed` (`:135-137`) → `*AnswerClosed{Item, Expired:true}`, `That question expired before it was answered.` The returned item has the same rewrite `ExternalGet` applies (`:209-211`): status `expired`, `answeredAt = deadline`.
   4. `validAnswer`, the offered-option check and the 4000-character limit, unchanged (`external_inbox.go:232-241`) → plain `reject(...)`.
   5. Emit `ExternalQuestionAnswered` with `AnsweredBy` (§3.1 rule), `Payload.Actor`, and text `<item.Source> question answered — <answerSummary>` plus the §3.1 suffix. The toast is `Answer recorded — the QA tool is told.` for a `qa-tools` item and `Answer recorded — <item.Source> reads it on its next poll.` otherwise.

   The signature keeps `(Result, error)`, so test call sites change only their arguments; the API handler reads the item with `ExternalGet` after a success (an answered item is final, so nothing can change it in between) and from `*AnswerClosed` on a refusal.

   The projection's answered case (`external_inbox.go:113`) also copies `AnsweredBy`. The expiry text (`:259`) becomes `<item.Source> question expired unanswered`; the sweep's toast (`questions.go:249`) becomes `%d inbox question(s) expired.`
3. **Delivery** — `ExternalPending` (`external_inbox.go:268-278`) returns only items whose `effectiveAsker` is `qa-tools`. That filter is the whole mechanism: no new delivery state (`Delivery.State` is read nowhere else, `:273`). The worker (`inbox_worker.go`) is otherwise untouched: the QA tool receives its own items' decisions from every source, and never another client's.
4. **Task answer** — `AnswerQuestion` (`questions.go:190-211`) gains a last parameter `by AnswerSource`; `AnsweredBy` and `Payload.Actor` per §3.1; the event text gains the §3.1 suffix.
5. **New read** — `RecentAnswers(ctx, productID string, limit int) ([]RecentAnswer, error)`:

   ```go
   type RecentAnswer struct {
       Question            // the row as the inbox speaks it (External, ProductID, TaskID, Title/TaskTitle, Options, Choice, Text, AnsweredAt, AnsweredBy, Source)
       Expired bool        // an external item that expired unanswered
   }
   ```

   Answered task questions (`questionsLocked`, `questions.go:101-120`, `Answered` rows) and external items whose stream status is `answered` or `expired`, newest `AnsweredAt` first, at most `limit`, one product or (`""`) all. A lapsed item the sweep has not yet written stays out (it is in neither list for at most the one-minute sweep, `questions.go:40`). In the `Store` interface beside `OpenQuestions` (`store.go:140`); ArcadeStore reads `s.proj`; `readOnly` passes it through (a read).

### 3.4 The machine route

`POST /v1/products/{id}/inbox/{externalId}/answer`, public `/api/v1/products/{id}/inbox/{externalId}/answer`, registered in `registerAPI` (`api_v1.go:270-280`), so `main.go:406-407` marks it a bearer route and the session gate does not apply. Handler `apiInboxAnswer` in `dashboard/api_inbox.go`.

Order of checks:

1. `apiGate(scopeClient)` — 503 disabled, 401 no/wrong token, 404 through a non-`/api` prefix.
2. The admin token (`cl == nil`) → **403** `{"error":"forbidden","reason":"the admin token is not a client; answer in the back office"}`.
3. `apiProduct` — 404 `not-found` outside the client's products or unknown.
4. `externalId` must match `apiInboxID` (`api_inbox.go:33`) — else 404 `not-found`, `no such inbox item`.
5. `beginWrite` — 429 with `Retry-After: 2` while this client's previous write is in flight. For the QA tool this slot is shared with its `/qa/results` and `/qa/cases` pushes (`api_v1.go:225-236`); §7 tells it to retry.
6. `apiDecode` into `{choice int, text string, actor {username string, display string}}` — strict, 1 MiB (`api_v1.go:239-254`); an unknown field is 400 `bad-json`. `display` is accepted because the brief's body has it, validated, screened, and **not stored**: no surface reads it.
7. **Secret screen**, the ask route's two passes (`api_inbox.go:146-169`): `secretInStrings` over the raw body; then `inboxText` on `text`, `actor.username`, `actor.display`; screen the cleaned strings again. A hit is 400 `secret-in-body`, nothing written.
8. Actor: cleaned `username` must match `^[A-Za-z0-9._@+-]{1,64}$`, else 400 `bad-actor`, reason `actor.username is 1 to 64 of A-Z a-z 0-9 . _ @ + -`. It must not be `operator` (any case) nor a back-office account's email (`s.auth.accounts[canonEmail(username)]`), else 400 `bad-actor`, reason `actor.username may not name the operator`: a client cannot pose as an answer given in GAOS. A client sends a handle, not an email (§7). Cleaned `display`, whitespace collapsed, at most 64 characters, else 400 `bad-actor`, reason `actor.display is at most 64 characters`.
9. Text: cleaned and trimmed `text` over 4000 characters (`runesOver(text, externalTextMax)`) → 422 `bad-text`, reason `An answer is at most 4000 characters.`
10. `store.AnswerExternal(ctx, id, ext, ExternalAnswer{Choice, Text: cleaned, By: {cl.Kind, cl.Name, cl.Origin, username}})`, then:

| Store outcome | Status | Body |
|---|---|---|
| success | 200 | the GET body (§3.5) of the item now answered, read with `ExternalGet` |
| `*AnswerClosed`, `!Expired`, and `Item.AnsweredBy` has `Client == cl.Name`, `Actor == username`, and `Item.Choice == choice`, `Item.Text == cleaned text` | 200 | the GET body of the **first** answer; nothing is written (an idempotent retry; `display` is not compared) |
| `*AnswerClosed`, `!Expired`, otherwise | 409 | `{"error":"already_answered","reason":<the API refusal sentence>, ...GET body}` |
| `*AnswerClosed`, `Expired` | 410 | `{"error":"expired","reason":"That question expired before it was answered.", ...GET body}` |
| `ErrNotFound` | 404 | `{"error":"not-found","reason":"no such inbox item"}` |
| any other `ErrRejected` | 422 | `{"error":"bad-choice","reason":<the store's sentence>}` |
| anything else | 500 | `apiStoreErr` |

The API refusal sentence is the §3.1 refusal sentence built from `apiSourceOf` (so a BO answer reads "in GAOS (gaos.example.org) by operator"). The 409/410 bodies are the GET body marshalled to a map plus `error` and `reason`; the GET body has neither key.

After a 200 that wrote, the handler calls `s.inbox.Kick()` (as `handlers_questions.go:130`) and logs the line of §3.1.

The route writes through `s.store`, not `s.mut(r)`, like every machine route (`api_v1.go:28-31`).

### 3.5 The GET body gains `source`; GET is owner-only

`apiInboxState` (`api_inbox.go:184-193`) gains `Source *apiSource json:"source"` — **always present**: `null` while open, on an expiry, and on an item answered before this change; `{kind, client, origin, actor}` (via `apiSourceOf`) once answered.

`apiInboxGet` (`api_inbox.go:195-219`) applies the owner rule: a client reads only items whose `effectiveAsker` is its name, else 404 `no such inbox item`; the admin token reads any. The QA tool's view is unchanged, because every item it could see is `qa-tools` by §3.1 (admin-asked ones included).

```json
{ "externalId": "run-42:case-7", "status": "answered", "choice": 1, "optionLabel": "Approve", "text": "",
  "askedAt": 1763000000, "answeredAt": 1763000900, "expiresAt": 1763604800,
  "source": { "kind": "qa-tool", "client": "qa-tools", "origin": "qa.example.org", "actor": "ardi" } }
```

The 200 answer body is this same shape. It carries `askedAt` and `expiresAt` in addition to the fields the brief listed, so a client parses one shape for GET, 200, 409 and 410.

### 3.6 `GAOS_QA_ORIGIN`

The `qa-tools` client's origin, resolved once at boot by `qaOrigin(env, qaURL string, logf) string`:

1. `GAOS_QA_ORIGIN`, when it matches the origin pattern of §3.2. Otherwise logged `api: GAOS_QA_ORIGIN is not a host name (ignored)` and treated as unset.
2. Else `url.Parse(GAOS_QA_URL).Hostname()` (**without the port**) when it parses, is non-empty and `net.ParseIP` rejects it (a name, not an IP literal). `http://qa.example:8140` gives `qa.example`.
3. Else `""`: the phrase prints `via QA tool by ardi`.

Production runs `GAOS_QA_URL=http://10.10.0.9:8140` (the tunnel IP, `deploy/rebuild-restart.sh:102`), so production needs `GAOS_QA_ORIGIN=qa.example.org`. `deploy/stacks/compose.prod.yaml` passes it through after `GAOS_QA_URL` (`:134-135`):

```yaml
      # The QA tool's public host name, recorded on every answer it sends (D-083).
      # Unset = the host of GAOS_QA_URL when that is a name; the tunnel URL is an IP.
      GAOS_QA_ORIGIN: "${GAOS_QA_ORIGIN:-}"
```

`deploy/check-stacks.sh`, after the `GAOS_BO_LABEL` check (`:142`), gains `has $PROD 'GAOS_QA_ORIGIN: "${GAOS_QA_ORIGIN:-}"' "prod passes the QA tool's origin from the deploy shell"`. The comment at `deploy/rebuild-restart.sh:102` names the variable. **After merge**, the operator adds `GAOS_QA_ORIGIN=qa.example.org` to the redeploy command they run on the second prod host; until then that deployment records an empty origin ("via QA tool by ardi").

`GAOS_API_CLIENTS_FILE` is not wired into compose in this change: no registry client exists. §7b says what a future client adds.

### 3.7 The back office

- **The answer.** `handleExternalAnswer` (`handlers_questions.go:103-136`) and `handleAnswer` (`:55-93`) build `AnswerSource{Kind:"gaos", Client:"back-office", Origin: s.auth.label, Actor: actorOf(r)}` (a helper `boSource(r)`) and pass it to the store. Their status mapping is unchanged: `*AnswerClosed` is an `ErrRejected`, so it stays 409 / a toast.
- **The refusal names the first answer.** The toast is the store's sentence: "That question was already answered via QA tool (qa.example.org) by ardi." `handleExternalSheet` (`dashboard/handlers_bo.go:825-850`), when the item is not open, reads it with `ExternalGet` and toasts the same refusal sentence (or the expiry sentence) instead of today's `That QA tool question is not waiting on an answer any more.` (`:844`); an unknown id gets `That question is not waiting on an answer any more.`
- **Labels per client.** The inbox row's and the sheet's tag already print `.Source` (the origin tag, `needs.html:31,75`), which is now the asking client's label. A view field `Asker` on `InboxRow` and `AnswerView` holds `the QA tool` for a `qa-tools` item, else the client's name; `Pushed bool` says whether the asker receives the webhook. The exact strings:

  | Where | Today | After |
  |---|---|---|
  | `needs.html:40` expiry title | `Closes at its deadline; after that the QA tool is told it expired` | `Closes at its deadline; after that {{.Asker}} {{if .Pushed}}is told{{else}}sees{{end}} that it expired` |
  | `needs.html:113` placeholder | `Tell the QA tool what to do instead` | `Tell {{.Asker}} what to do instead` |
  | `needs.html:119` label | `Note to the QA tool` | `Note to {{.Asker}}` |
  | `handlers_bo.go:731` fallback | `(the QA tool sent no question text)` | `(<Asker> sent no question text)` |
- **"Answered recently".** `buildInbox` (`handlers_bo.go:703-757`) fills `bo.Recent []RecentRow` from `RecentAnswers(filter, 20)`. `RecentRow` (in `dashboard/view.go`, beside `InboxRow`): `ProductName, Lead, TaskID, Tag, Title, Outcome, By, WhenISO, When string`. `Tag` is the origin tag for an external item, `""` for a task question (then `TaskID` shows). `Outcome` is the option label, `own words: <text clipped to 80>`, or `expired unanswered`. `By` is the §3.1 row sentence (empty for an expired item). `When` is `relSpan(now - answeredAt) + " ago"` (`handlers_bo.go:600-610`). `needs.html` renders it under the open list and under the empty state, heading `Answered recently`, as an `<ol class="inbox-recent">`; rows are not buttons. All fields are plain strings, escaped by `html/template`.
- **The timeline** prints `Event.Text` (`dashboard/templates/backoffice.html:425-438`, the `.Text` cell at `:436`), which now ends in the phrase. Old events keep their old text.
- **The card's conversation sheet.** `AttemptView` (`conversation.go:657-664`) gains `AnsweredBy string`, filled at `:705-713` with the conversation-sheet sentence of §3.1 (the bare phrase, or `source not recorded`). `needs.html:239` reads `<b>Answered</b> <answer> · <AnsweredBy> <time>` instead of `<b>You answered</b>`. The brief asks for the source "on the answer sheet / card"; today every new row there says "in GAOS", and it says more once §8 lands.
- New CSS in `dashboard/static/app.css` for `.inbox-recent` uses existing variables only.

### 3.8 Tests

New top-level tests. Numbers provisional (§6). The phase that writes each is in §10.

| TC | File · test | Asserts |
|---|---|---|
| TC-490 | `dashboard/answer_source_test.go` · `TestAPIInboxAnswerFromTheQATool` | 200 with `source` `{qa-tool, qa-tools, qa.example, ardi}`; the event carries `AnsweredBy` and `Payload.Actor` `qa-tools/ardi`; `ExternalPending` holds the item; GET shows the same `source`; a `display` is not stored; the server log line carries the `xff` value received |
| TC-491 | same file · `TestAPIInboxAnswerFirstWins` | an identical retry is 200 with the first `answeredAt` and writes no event; a different choice, or a different username, is 409 `already_answered` with the first source; a BO answer first then the API answer is 409 whose `source.actor` is `operator` and whose body never contains the session email; a BO answer after an API answer is refused with "already answered via QA tool" |
| TC-492 | same file · `TestAPIInboxAnswerRefusals` | 401 no token; 403 admin token; 404 product out of scope; 404 unknown id; 410 expired (and lapsed-but-unswept, body `status: expired`); 422 `bad-choice` (5, an unoffered 3, 4 without words); 422 `bad-text`; 400 `bad-actor` (missing, 65 characters, a space, `operator`, a back-office account's email); 400 `secret-in-body` in text, in the username and in a split-by-bidi-mark secret; 400 `bad-json` for an unknown field; 429 while the write slot is held |
| TC-493 | same file · `TestAPIClientRegistry` | a good line loads with its own write slot; skipped and logged, the rest loading: a reserved name, a reserved kind `qa-tool`, a duplicate name, a bad origin, an origin equal to the QA origin in another case, a group-readable token file, a missing token file, a token equal to the QA token, a token equal to the outbound token, a product the QA token reaches; a line with origin `-` loads when the QA origin is unset; equal admin and QA tokens plus a valid line still answer 503 everywhere; with no built-in token, a valid line enables the API. A registry client: 403 on `GET /v1/products`, `/qa/summary` and `/v1/seed/product`; 404 on a product not in its list; `-` reaches nothing; asks and answers its own item with its kind and origin; 404 on GET and answer of an item it did not ask; a second registry client asking its id gets 409 `id-taken`; its item is labelled with its name, never "QA tool"; its answered item is not in `ExternalPending`; 429 when its own slot is held and not when the QA tool's is; the open cap counts per asker |
| TC-494 | same file · `TestQAOriginResolution` | `GAOS_QA_ORIGIN=qa.example.org` wins; `bad host!` is ignored; `http://qa.example:8140` gives `qa.example`; `http://10.10.0.9:8140` gives `""`; unparsable gives `""` |
| TC-495 | same file · `TestBackOfficeAnswersRecordTheirSource` | through the BO handlers signed in as the test account with label `gaos.example.org`, an external answer and a task answer carry `{gaos, back-office, gaos.example.org, <email>}`; event text ends `in GAOS (gaos.example.org) by <email>`; `Payload.Actor` is the email; the next packet's answer part does not contain the email |
| TC-496 | same file · `TestAnswerSourcePhrase` | every row of the phrase table and the sentence table of §3.1, empty origin, empty actor |
| TC-497 | `dashboard/needs_test.go` · `TestInboxShowsRecentAnswers` | the rendered inbox lists a QA-tool answer and a BO answer with their sentences, a pre-change event as `answered · source not recorded`, an expired item as `expired unanswered`, newest first, at most 20, filtered by product; a `<script>` title is escaped |
| TC-498 | `dashboard/conversation_test.go` · `TestConversationSheetSaysWhoAnswered` | the sheet shows `Answered … · in GAOS (…) by …` for a new answer and `source not recorded` for an old one |
| TC-499 | `deploy/check-stacks.sh` | `ok   prod passes the QA tool's origin from the deploy shell`; FAIL when the line is missing |

The store conformance case `caseExternalInbox` (`dashboard/external_inbox_test.go:759-806`, registered `store_conformance_test.go:44`) extends: `AskedBy` and `AnsweredBy` survive a round trip on both stores, and `RecentAnswers` returns the answered item. It stays one case under TC-124/TC-125.

**Existing tests that change, and why:** every match of `AnswerExternal(` in `dashboard/*_test.go` (`external_inbox_test.go:33,176,359,369,399,440,450,480-481,582-583,625,643,690,704,777-783,822`, `store_conformance_test.go:846-848`) passes an `ExternalAnswer` instead of `choice, text`; every match of `AnswerQuestion(` (among them `events_test.go:455`, `runner_manual_test.go:308`, `questions_test.go`, `store_conformance_test.go`) gains `AnswerSource{}`. With no source passed there is no suffix, so text assertions hold. `external_inbox_test.go:188-197,515` pass unchanged (normalisation, §3.1). `newAPIConfig`, `readTokenFile` and `qaBusy` keep their names and meaning. Tests asserting the strings §3.3 and §3.7 change (`QA tool asks:`, the not-waiting sentence, the expiry text) are updated to the new strings, which are identical for a `qa-tools` item except the not-waiting sentence and the sweep toast.

### 3.9 Files

| File | Change |
|---|---|
| `dashboard/answer_source.go` (new) | `AnswerSource`, `Phrase`, the sentence helpers, `apiSource`, `apiSourceOf`, `qaOrigin`, `parseClients`, the origin pattern |
| `dashboard/api_v1.go` | `apiClient`, `scopeClient`, `outSum`, `authorize`, `apiGate`, `apiProduct`, `beginWrite`, `readTokenPath`, `readTokenPathStrict`, `addClients`, `setQAOrigin`, `apiFromEnv`, `Describe`, route registration, every call site of the gate in this file |
| `dashboard/seed.go` | `apiSeedProduct`: the new `apiGate`/`beginWrite` signatures (`:182,186`) |
| `dashboard/api_inbox.go` | ask: `AskedBy`/`Label`, 409 `id-taken`; GET: owner rule and `source`; the new `apiInboxAnswer` and its log line |
| `dashboard/external_inbox.go` | `ExternalAsk`, `ExternalAnswer`, `AnswerClosed`, `ErrIDTaken`, `effectiveAsker`, normalisation, per-asker cap, `AnswerExternal`, `ExternalPending`, the projection copying `AnsweredBy`, the expiry text, `RecentAnswers` (external half) |
| `dashboard/questions.go` | two payload fields, `AnswerQuestion(by)`, the sweep toast, `RecentAnswers` (task half) |
| `dashboard/model.go` | `Clone` copies `AnsweredBy`; the `Actor` comment |
| `dashboard/store.go` | interface: `AnswerQuestion`, `AnswerExternal`, `RecentAnswers`; `readOnly` stubs |
| `dashboard/store_arcade_blueprint.go` | the three wrappers |
| `dashboard/handlers_questions.go` | `boSource`, both handlers |
| `dashboard/handlers_bo.go` | `buildInbox` recent list and `Asker`, `handleExternalSheet` refusal |
| `dashboard/conversation.go` | `AttemptView.AnsweredBy` |
| `dashboard/view.go` | `RecentRow`, `BOView.Recent`, `Asker`/`Pushed` on `InboxRow` and `AnswerView` |
| `dashboard/templates/needs.html`, `dashboard/static/app.css` | the recent list, the label wording, the conversation line |
| `dashboard/answer_source_test.go` (new), `needs_test.go`, `conversation_test.go`, `external_inbox_test.go`, `events_test.go`, `questions_test.go`, `store_conformance_test.go`, `runner_manual_test.go` | §3.8 |
| `deploy/stacks/compose.prod.yaml`, `deploy/check-stacks.sh`, `deploy/rebuild-restart.sh` (comment) | §3.6 |
| `deploy/check-workspace.sh` | the count literals (§6) |
| `.claude/rules/qa-test-plan.md` | highest TC, totals, range map, count literals (§6) |
| `pustaka/docs/guide/qa-sync-api.html` | a new `<h2 id="inbox">The inbox</h2>` after `#call-seed` (`:214`): the ask, GET and answer routes (the first two were never documented here), the response table, `source`, the client registry and its file format, the never-reused name rule, `GAOS_QA_ORIGIN`, and §7's contract |
| `pustaka/docs/assets/toc/guide.js` | the `qa-sync-api` entry (`:86-92`): an `inbox` section, the desc and tags |
| `pustaka/docs/infra/environments.html` | rows for `GAOS_QA_ORIGIN` and `GAOS_API_CLIENTS_FILE` after `GAOS_QA_URL` (`:205`) |
| `pustaka/docs/product/qa-test-plan.html` | ten rows, ten `h3`s, the counts |
| `pustaka/docs/product/brd.html` | BR-57, BR-52, BR-48, "Fifty-six rules" → "Fifty-seven rules" |
| `pustaka/docs/product/decision-log.html` | D-083, "Eighty-one entries" → "Eighty-two entries" |
| `pustaka/docs/product/open-questions.html` | OQ-11 (`:519`) and OQ-34 (`:811-813`): one clause each saying answers carry an actor since D-083 (the questions stay open; no count changes) |
| `pustaka/docs/assets/toc/product.js` | the mirrored counts |
| `pustaka/docs/changelog.html`, `pustaka/docs/assets/toc.js` | release note, `product.semver` |
| `artifacts/v1/gaos-arcadedb-model.html` | payload table: `askedBy`, `answeredBy` on the question events |
| `docs/designs/qa-inbox-api.md` | a pointer to this design under §3.4 |

`README.md` and `deploy/STACKS.md` list no API variable today, so the variables are documented where the others are (`environments.html`).

### 3.10 The registers

- **D-083.** "An answer records its source, and a machine client answers only its own items." Context: the QA tool's `/history` page needs an answer button; Hikaru apps will follow. Decision: §3.1–§3.5. Consequences: `Event.actor` names the answerer (the `<client>/` prefix marks an identity GAOS did not verify); `answeredBy` on the two answer events; a client is a registry entry and its name is never reused; ids, the open cap, the origin tag and the webhook are per asking client, and a registry client may not share a product with the QA tool until items are keyed by asker; the machine API never returns a back-office email. BR-57 sits in §9.8 because every rule in it is about the machine API and its inbox; its one back-office clause (the source of a BO answer) is the other half of the same record. Alternatives: §4. References: BR-57, BR-52, BR-48, OQ-11, OQ-34, TC-490…TC-499.
- **BR-57** (§9.8): "Every answer records where it came from — the back office with its deployment label and the signed-in account, or a machine client with its registered name, kind and origin — derived on the server. The origin is the client's registered host, not an observed address. Only the client's actor handle is taken from the request; it is recorded and shown as the client reported it, kept with the event for as long as the event (indefinitely, BR-04), and it may not name the operator. The machine API returns a back-office answer's actor as `operator`, never the account. A machine client asks, reads and answers only its own inbox items; an id another client uses is refused, the open-item cap counts per client, and a client name is never reused. The first answer wins; an identical retry by the same client is accepted again and writes nothing. The QA tool receives every decision on its own items, whatever answered them; the webhook is a notification and carries no source, so a decision counts as the operator's only when its `source.kind` is `gaos`. Any other client polls. *(D-083, D-078)*"
- **BR-52**: the clause "equal tokens, or a token that is short or lacks the `gaos_` prefix, disable the API" becomes "two equal admin, QA or outbound tokens, or a token that is short or lacks the `gaos_` prefix, disable the API; a client from the clients file whose token equals any principal's, or whose token file others can read, is not loaded, and such a client reaches only the inbox routes of its own products", and the citation `(D-072, D-074, D-075)` gains D-083.
- **BR-48**: "events carry no actor" becomes "the clearance event carries no actor (conversation events do since D-082 and answers since D-083)".

## 4. Alternatives

- **Trust a `source` object in the body.** Rejected: any holder of the QA token could write "in GAOS by the operator". The server knows which token was presented.
- **A new event type.** Rejected: BR-05; the fold, the worker and the projection would each need a second case for the same state change. A payload field is additive.
- **Let any client answer any item in its products.** Rejected: a second client could close the QA tool's decisions.
- **Defer the registry until the first real client** (zoom-out and critic, rounds 1 and 2). Rejected by the operator's approval (§11): the brief asks for it explicitly (§1), and the parts that made it unsafe in v1 — shared ids, a shared cap, the QA label and the QA webhook for every item — are fixed here per asker, so a client added later by configuration cannot reach the QA tool's items. What a later Hikaru app still needs is a push (N7) and task questions (§8); both are additive. The cost of keeping it is listed in `reviews/answer-source-zoomout-2.md`.
- **Key items by (product, asker, externalId).** Rejected for now: the back-office routes (`/products/{id}/inbox/{externalId}/answer`, `/ui/answer-ext/...`) and the webhook body address an item by externalId alone; a composite key changes all of them. Instead a registry client may not share a product with the QA tool, so the QA tool's ids cannot be taken; between two registry clients, 409 `id-taken` applies (L4).
- **Registry in a Go table, or in an environment variable.** Rejected: the brief asks for configuration plus a token file; a file is commentable and keeps paths out of the process environment.
- **Disable the whole API when a registry token collides.** Rejected for registry lines: a mistyped future client must not take the QA sync down. The built-in rule stands and is final.
- **Source in the webhook body.** Rejected for now (N3): the contract the QA side implements against fixes the webhook body; GET, the 200 and BR-57's rule on `source.kind` cover it.
- **Return the operator's email to clients** (the brief's example shows it). Rejected (security round 1): the email is the sign-in identifier. The event and the back office keep it; the API says `operator`.

## 5. Accepted limits

- L1. `actor` is whatever the client says: validated to a narrow alphabet, screened, escaped, shown as reported, and never `operator` or an account email. The client is authenticated; the person is not.
- L2. The first-answer rule holds for answers that reach GAOS. An approval the QA tool records and never sends is outside GAOS; §7 tells it not to.
- L3. "Answered recently" is a projection over the whole stream, like `OpenQuestions` (`questions.go:161-182`).
- L4. Two registry clients sharing a product share its id space: the second to ask an id gets 409 `id-taken`, which tells it only that the id is used. A client whose ids are predictable can be blocked by another client in the same product; the QA tool cannot, because no registry client may share its products.
- L5. The server-log address is a lead, not proof (§3.1).

## 6. Counts

Ten new register rows, provisionally TC-490…TC-499: nine `dashboard (go test)` rows and one `check-stacks.sh` row. Register 489 → 499 rows ("four hundred and eighty-nine" → "four hundred and ninety-nine"); `dashboard (go test)` 343 → 352; Go tests 365 → 374 ("three hundred and sixty-five" → "three hundred and seventy-four"). BRD "Fifty-six rules" → "Fifty-seven rules". Decision log "Eighty-one entries" → "Eighty-two entries" (D-082 made it eighty-one on the same basis, `docs/designs/conversations.md` header; the page counts entries, not `<tr id>` rows). Open questions: no count changes. These literals live in `qa-test-plan.html` (lede, register intro), `toc/product.js` (`qa-test-plan` desc and register text, the BRD and decision-log entries), `check-workspace.sh:112-113,127-131`, and `.claude/rules/qa-test-plan.md`.

The concurrent branch `feat/design-iterate` may claim numbers first. On rebase the rows are renumbered to the next free block, the D-number and BR-number to the next free ones, and every count is recomputed from the register and the pages, not from this paragraph.

## 7. What the QA tool must match

1. Answer with `POST /api/v1/products/{id}/inbox/{externalId}/answer`, its own bearer, body `{"choice", "text", "actor": {"username", "display"}}`. `username` is a handle, not an email, and not `operator`.
2. **Record an answer as given only on 200.** On 409 `already_answered` or 410 `expired`, adopt the item in the response body (its `status`, `choice`, `text`, `source`): that is the decision of record. On 429, retry after `Retry-After` (the slot is shared with its result and case pushes).
3. The webhook still arrives for every decision on its items, including its own answers, and **may arrive before the 200**. It carries the same decision and no source; take `source` from the 200 or from GET, not from whichever record came first. A decision is the operator's only when `source.kind` is `gaos`.
4. On the ask route, 409 `id-taken` means the id belongs to another client: choose another id. It is never transient. (It cannot happen to the QA tool while no registry client shares its products.)
5. Read `source` on GET for display. `actor` is `operator` for an answer given in GAOS.

### 7b. What a future client does

1. Generate a token (`gaos_` + 32 or more alphanumerics) into `deploy/stacks/prod.api-client-<name>` on the host, mode 600 (the name keeps it inside the ignored `*.api-*` pattern).
2. Add a line to the clients file: `hikaru-android hikaru-android android.hikaru.example /run/secrets/api-client-hikaru-android p-8` — a product the QA token does not reach, and a name never used before.
3. In `compose.prod.yaml`, mount the clients file and `prod.api-client-<name>` read-only at `/run/secrets/api-client-<name>` and set `GAOS_API_CLIENTS_FILE`. Restart.
4. The client asks, polls GET (no push, N7) and answers its own items. Its items are tagged with its name; its answers read "via hikaru-android (android.hikaru.example) by <user>".

## 8. Next extension (not in this change)

Answering **task** questions over the machine API — what the Hikaru apps need to approve an agent's question from a phone. It needs a decision this design does not make: which client may answer which task's question (tasks have no asking client). `AnswerQuestion` already takes an `AnswerSource`, so the route is the remaining work. A per-client webhook (N7) and items keyed by asker (§4) are the others. This question is escalated as a follow-up in the release report rather than opened as an OQ here.

## 9. Risks

- R1. A leaked QA token now also answers the QA tool's own approvals. It could already create and read them; the source records the client; the server log records the forwarded chain as a lead (L5).
- R2. Stored XSS via `actor` — restricted alphabet, `html/template` escaping, no `template.HTML` built from it.
- R3. A readable token or clients file — a registry token file others can read is refused; built-in token files and the clients file are warned about (`warnIfOpen`).

## 10. Phases

Each exit command must print `ok` for the package and must **not** print `no tests to run`.

1. **Store and source.** `answer_source.go` (types, phrase, sentences), `external_inbox.go`, `questions.go`, `model.go`, `store.go`, `store_arcade_blueprint.go`, the two production callers in `handlers_questions.go:65,113` (passing `boSource(r)`), every test call site, the conformance case, and TC-496. Exit: `cd dashboard && go vet ./... && go test -run 'TestAnswerSourcePhrase|TestStoreConformance' ./... && go test ./...`.
2. **Clients and the route.** `api_v1.go`, `seed.go`, `api_inbox.go`, `qaOrigin`, `parseClients`, TC-490…TC-494. Exit: `go test -run 'TestAPIInboxAnswerFromTheQATool|TestAPIInboxAnswerFirstWins|TestAPIInboxAnswerRefusals|TestAPIClientRegistry|TestQAOriginResolution' ./...`, then `go test ./...`.
3. **Back office.** `handlers_bo.go`, `view.go`, `needs.html`, `app.css`, `conversation.go`, TC-495, TC-497, TC-498. Exit: `go test -run 'TestBackOfficeAnswersRecordTheirSource|TestInboxShowsRecentAnswers|TestConversationSheetSaysWhoAnswered|TestTheQAToolsQuestionIsAnsweredFromTheInbox' ./...`, then `go test ./...`. Then render the inbox once in a headless browser against the in-memory store: the "Answered recently" list shows a QA-tool row (`answered via QA tool (…) by …`) and a back-office row (`answered in GAOS (…) by …`), and opening an item the QA tool already answered toasts `That question was already answered via QA tool …`.
4. **Deploy and docs.** compose, `check-stacks.sh` (TC-499), the pustaka pages, the registers, the counts, the rule file, the changelog. Exit: `cd pustaka && go run . check ./docs && go test ./...`; `./deploy/check-stacks.sh` and `./deploy/check-workspace.sh` with no `FAIL`; `cd dashboard && go test -race ./...`, and the ArcadeDB half with `GAOS_ARCADE_TEST_URL` set.

## 11. Approval

The product owner's brief, relayed by the orchestrating agent on 2026-11-21 (a conversation message, not a repository file — labelled here as such), fixes the route, the body, the responses, the source kinds and the client registry, and delegates approval of this design to the orchestrating agent under the product owner's standing instruction ("work among your agents, never as my confirmation"), as recorded in `conversations.md` §11. The operator decision zoom-out round 2 asked to be made explicitly — keep the client registry (option a) or defer it (option b) — is made here: **(a), keep it**, because the brief names it and v3 makes a configured client unable to reach the QA tool's items; its cost is accepted as listed in `reviews/answer-source-zoomout-2.md`. Two deviations from the brief are deliberate and are reported to the orchestrator: the API returns a back-office answer's actor as `operator`, not the email (security round 1); and `actor.display` is accepted and not stored. Approved 2026-11-21, v3, by the implementing agent on behalf of the orchestrating agent, under that delegation; the product owner was not asked.

## 12. As built (v0.43.0)

Built as designed, with these differences:

- The store's owner check skips a source with no kind as well as kind `gaos` (`external_inbox.go`, `AnswerExternal`): an internal caller that records no source (the tests, the drive helpers) is the back office's equivalent. Every machine caller sets a kind, so the rule for clients is unchanged.
- The labels for the answer sheet and the inbox row come from `askerLabel` in `handlers_bo.go`; the store's `labelOf` gives the origin tag, `QA tool` for an item that predates it.
- TC-495 signs in with the test suite's label (`gaos-test`), not `gaos.example.org`; the assertion is the same.
- D-078 has no row in the decision log (its design says the row is added when its review passes), so BR-57 cites it without a link.

Verified: `go test -race ./...` on both stores (ArcadeDB on the scratch image, `GAOS_ARCADE_TEST_URL`), `pustaka check`, the pustaka tests, `check-workspace.sh` and `check-stacks.sh` (TC-499 also seen to FAIL with the line removed). Driven against a local in-memory dashboard: the QA token answers and gets its source back; a back-office answer then makes the QA tool's second answer a 409 naming `in GAOS (gaos.example.org) by operator`; opening an item the QA tool answered toasts `already answered via QA tool (qa.example.org) by ardi`; the inbox renders "Answered recently" with both sources at 1280 and 390 pixels wide. Basis: round 1's Blockers, Critical and High are fixed (`reviews/answer-source-review-1.md`); round 2 raised no Blocker and no Critical or High, and its findings are applied (`reviews/answer-source-review-2.md`); no Blocker survives, so no third round (policy step 7).
