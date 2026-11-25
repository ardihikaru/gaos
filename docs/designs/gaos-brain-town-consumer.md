# The Brain Town consumer client — GAOS reads the town's pull-request events

> **Status:** In Review
> **Author:** the GAOS team
> **Reviewers:** the five `gaos-goldfish-*` agents (`docs/rules/gaos-design-review.md`)
> **Policy:** `docs/rules/gaos-design-review.md`
> **Reviews:** `docs/designs/reviews/gaos-brain-town-consumer-review-1.md` (comprehension),
> `docs/designs/reviews/gaos-brain-town-consumer-review-2.md` (critic),
> `docs/designs/reviews/gaos-brain-town-consumer-security-review-1.md` (security)

## 1. Problem

GAOS must react to GitHub pull-request activity on a product's main repository: open a
review task when a pull request appears, open a test task when it merges. Brain Town
already receives the GitHub webhook and already turns it into `pr.opened` and `pr.merged`
town events, but GAOS cannot see them:

- GAOS has **no** GitHub webhook receiver (`dashboard/` has no `X-Hub-Signature` handler).
- Brain Town's public stream (`GET /events`) is **masked**: a hidden product appears under
  its alias and every task title is removed. For a private repository GAOS would receive
  an alias and no title, which is not enough to act on.
- Brain Town's **admin** stream carries the stored envelope but needs a back-office
  session cookie, and GAOS must not hold an operator's admin session.

Brain Town must therefore expose a path GAOS can read: a scoped, revocable, machine-only
stream that carries the **stored** envelope (the real repository name, the real title) and
nothing more. Brain Town's side of that contract is `docs/integrations/gaos-consumer.md`
in the Brain Town repository (`ardihikaru/brain-town`); this document is GAOS's half.

## 2. The contract GAOS depends on

Everything in this section is **Brain Town's** contract, not GAOS's. It is cited here so
the reader knows what GAOS is allowed to assume; if Brain Town's document changes, this
document changes with it.

| Fact | Where it comes from |
| --- | --- |
| The path is `GET /consumer/events`. | Brain Town `docs/integrations/gaos-consumer.md`, "Request (from the consumer)". |
| The credential is `Authorization: Bearer bt_c_<40 hex characters>`. | Same document, the same section. |
| The frame is `id:` (the sequence number), `event:` (the town event type), `data:` (JSON). | Same document, "The frame". |
| The JSON members are `seq`, `id`, `type`, `source`, `at`, `productKey`, `orgLogin`, `payload`. | Same document, "The frame". |
| `productKey` is the **bare repository name**; `orgLogin` is the owner login. | Same document, "The frame". |
| The payload is the stored envelope, unmasked: `product`, `org`, `task`, `data`. | Same document, "The frame". |
| The server sends `retry: 3000` and a heartbeat every 15 s. | Same document, "Request (from the consumer)". |
| `Last-Event-ID` replays everything after it; `?replay=N` starts a fresh subscriber. | Same document, "Request (from the consumer)". |
| A `401` means the token is missing, unknown, revoked or rotated. | Same document, "Responses". |
| The source payload is never stored, so there is no diff, no PR body and no login. | Same document, "What a consumer cannot get". |
| The consumer's scope is the operator's declaration; Brain Town filters by it. | Same document, "Scope". |

**The two sides ship in order.** Brain Town's `/consumer/events` must exist and be
deployed before GAOS's reader is switched on. A GAOS release that ships the reader
without the Brain Town path leaves the reader off with the reason "the stream answers
404", which the Integrations view shows.

**One Brain Town scope per GAOS instance.** GAOS runs a second instance with its own
database (`deploy/stacks/compose.prod.yaml:77`), and the two instances never share a
graph. If both instances pointed at one scope, each would receive every event, neither
would share the other's de-duplication set, and the same pull request would open two
cards — silently. Each GAOS instance therefore registers **its own** scope in Brain
Town, and the scope's name says which instance it belongs to.

## 3. What GAOS will and will not do

**Will:** run one long-lived reader against Brain Town's consumer stream, keep the events
it asked for, and hand each of them to the pull-request flow described in
`docs/designs/gaos-pr-agent-flows.md`.

**Will not:** hold a Brain Town admin credential, hold a GitHub token to read the webhook,
or receive anything the consumer's scope does not name.

## 4. The configuration

Every setting follows GAOS's rule that a credential is a **file** named by an environment
variable, never a value in the environment (`CLAUDE.md`, "Every credential is a file").

| Variable | Meaning |
| --- | --- |
| `BRAIN_TOWN_CONSUMER_ENABLED` | `true` or `1` turns the reader on. Anything else: no listener, no goroutine, no request. |
| `BRAIN_TOWN_CONSUMER_URL` | The Brain Town base URL, for example `https://town.example.org`. https only; **no path**, no userinfo, no query, no fragment; redirects are refused; the host must be the one this setting names and must not be an IP literal or a loopback, private or link-local address. |
| `BRAIN_TOWN_CONSUMER_TOKEN_FILE` | A file, mode 600, holding one `bt_c_…` token. |
| `BRAIN_TOWN_CONSUMER_CURSOR_FILE` | A file, mode 600, holding the last sequence number the reader handled. |
| `BRAIN_TOWN_CONSUMER_TYPES` | Comma-separated town event types to accept. Default `pr.opened,pr.merged`. |
| `BRAIN_TOWN_CONSUMER_KINDS` | Comma-separated event kinds. Default `github`. Sent as the `?kind=` parameter, comma-joined; the reader also filters on it. |

A setting that cannot be used **never stops GAOS**: the reader stays off, the boot log
says why, and the Integrations view shows the reason. This is the same rule the outbound
Brain Town feed already follows (`dashboard/braintown.go:90-127`, and the "An unusable
setting never stops GAOS" sentence in `pustaka/docs/integrations/brain-town.html`).

### 4.1 The token file

The rules are the **stricter** of GAOS's two, not the looser:

- The file must be **private** (mode 600, or stricter). A file others can read is
  **refused**: the reader stays off and the reason says "the token file is not private".
  This copies `dashboard/api_v1.go:133`, which rejects a client token file whose mode is
  not private, and BR-52 (`pustaka/docs/product/brd.html:1346`): a client whose token
  file others can read "is not loaded".
  It is deliberately **not** `readBrainTownToken` (`dashboard/braintown.go:132-148`), which
  only warns through `warnIfOpen` (`dashboard/braintown.go:133`) and uses the token anyway.
- The token must be 16 to 256 printable ASCII characters with no whitespace.
- The token **must start with `bt_c_`**. A `bt_live_` token — Brain Town's **input**
  credential — is refused, because a reader must never hold a sender's token. A `gaos_`
  token, or any token equal to one of GAOS's own API tokens, is refused too, copying
  `dashboard/braintown.go:121-124` (the outbound feed's own guard, recorded as SEC-9 in
  `docs/designs/reviews/brain-town-emitter-security-review-1.md:18`).
- **The file's path is never logged.** The reason says what is wrong with the file, not
  where the file is.

`bt_c_` joins the workspace's secret list (`dashboard/egress.go:361`) and the egress
detectors, so a consumer token pasted into a prompt is caught like any other secret.

### 4.2 The cursor file

The cursor is the last sequence number the reader **handed over**. It is written to
`BRAIN_TOWN_CONSUMER_CURSOR_FILE` (mode 600) after the hand-over, and read at boot. It
is what makes §9's restart promise true: a reader that starts with no cursor asks for
the newest events instead of replaying the whole retention, and says so in the
Integrations view.

## 5. The reader

One file: `dashboard/braintown_consumer.go`.

### 5.1 The loop

One goroutine, started only when the reader is configured and enabled. It opens
`GET {BRAIN_TOWN_CONSUMER_URL}/consumer/events?kind={Kinds}` with:

- `Authorization: Bearer <token>`,
- `Accept: text/event-stream`,
- `Last-Event-ID: <the cursor>` on every reconnect.

**Timeouts.** There is **no whole-request timeout** on the stream — a timeout that fits a
POST would tear the stream down every few seconds. The connect timeout is 10 s. A
**heartbeat watchdog of 45 s** (three times the 15 s heartbeat) detects a half-open
connection: when it fires, the reader reconnects. The heartbeat itself is ignored as
data; only a `data:` line is an event.

**Reconnecting.** The waits before the 2nd, 3rd and 4th attempt are **1 s, 5 s and 30 s**
(`dashboard/braintown.go:57`); after the fourth consecutive failure the reader holds at
the 30 s wait, mirroring `btDegradeAt` (`dashboard/braintown.go:513-516`), so a server
that accepts and immediately closes cannot make GAOS hammer it. The server's `retry:
3000` is a hint; **the reader's own backoff is the authority**.

**Status classes.**

| Answer | What the reader does |
| --- | --- |
| `200` | the stream. It is read until it ends. |
| `401` | the token is unknown, revoked or rotated. The reader **stops** and says so; the token file is **re-read on the next attempt**, so a rotated token is picked up without a GAOS restart. It does not retry in a loop: a revoked token will not come back by itself. |
| `403` | the scope was revoked. The reader **stops** and says so. |
| `404` | Brain Town has no `/consumer/events` yet. The reader **stops** and says "the stream answers 404" — the expected state before Brain Town ships the path. |
| `429` | the rate limit. The reader retries with the backoff and honours `Retry-After` when the answer carries one. |
| `5xx` | Brain Town is unwell. The reader retries with the backoff. |

**The wait sequence.** The waits before the 2nd, 3rd and 4th attempt are 1 s, 5 s and
30 s; **after the fourth consecutive failure the reader holds at the 30 s wait**, so a
server that accepts and immediately closes cannot make GAOS hammer it.

### 5.2 The frame

A frame is at most **4 KiB**, mirroring `btBodyLimit` (`dashboard/braintown.go:51`). An
over-long frame is skipped and counted. Every field is capped: the title at **200
characters** (`btMaxTitle`, `dashboard/braintown.go:49`), the owner and the repository at
**200 characters** each. A title longer than that is truncated; a title with control
characters in it has them stripped; and the title is screened with the egress detectors
(`dashboard/egress.go:347`) before it reaches a task title, because a pull-request title
is attacker-authored text and the outbound feed republishes it.

The frame is decoded into:

```go
// PREvent is one pull-request event the reader accepted.
type PREvent struct {
    Kind     string    // the source kind, from "source"
    Type     string    // "pr.opened", "pr.merged"
    EventID  string    // the town's event id, for de-duplication
    Seq      int64     // the town's sequence number
    Org      string    // the owner login, from "orgLogin"
    Repo     string    // the bare repository name, from "productKey"
    PRNumber int       // the pull request number, from payload.data.pr
    Title    string    // the pull request title, from payload.task.title
    Draft    bool      // from payload.data.draft
    At       time.Time
}
```

### 5.3 The double filter

The reader accepts a frame only when **both** hold:

1. the frame's `source` is in `BRAIN_TOWN_CONSUMER_KINDS` and its `type` is in
   `BRAIN_TOWN_CONSUMER_TYPES`; and
2. **`{orgLogin}/{productKey}` is one of the repositories of a GAOS product.**

The repositories are the product's own: `Master.Repos` (`dashboard/model.go:388`), whose
entries are `owner/repo` pairs (`dashboard/fixtures.go:167`, `deploy/seed/example/product.json:8`).
The match is therefore **owner-qualified**: a repository of the same name under another
owner does not match, and a bare name never matches. The owner's login and the
repository's name come from the frame, but they are only ever used to *look up* a
product — the repository GAOS then acts on is the one the **product** declares
(`docs/designs/gaos-pr-agent-flows.md` §7).

A frame that matches no product is skipped and counted as `foreign`. It is not an error:
the scope in Brain Town is the operator's declaration, and a scope that is broader than
GAOS's products is exactly what this filter is for.

### 5.4 The channel

The reader hands each accepted event to the flow through a **bounded, non-blocking**
channel of capacity 256. The channel's **only** reader is
`btConsumer.Drain(ctx, handle)` — a method on the reader, in the reader's own file —
which calls the flow's handler for each event and returns when the handler returns
false. `Drain` runs on its own goroutine, so neither side blocks the other. A full channel does **not** drop the event: the reader keeps it
and retries, because the stream is edge-triggered and a dropped `pr.merged` would be gone
forever (BR-02, `pustaka/docs/product/brd.html:1226`: "An event that was never written is
a state change that never happened"). The reader therefore **advances the cursor only
after the hand-over succeeded**, so an event that could not be handed over is replayed on
the next reconnect. When the channel has been full for more than a minute the reader
gives up on that event, rewinds the cursor to the last event it handed over, and lets the
server replay it — the same recovery the cursor exists for.

Nothing blocks a GAOS action. The reader runs on its own goroutine.

### 5.5 What it stores

The last sequence number it handed over, in the cursor file (§4.2). The events
themselves are not stored by GAOS: they become tasks, and a task is the record. The
de-duplication set lives in the **flow**, on the card, so a restart rebuilds it from the
graph (`docs/designs/gaos-pr-agent-flows.md` §6.2).

### 5.6 The Integrations view

`dashboard/integrations.go` gains a row, built by the same `buildIntegrations` function
(`dashboard/integrations.go:70`) that already builds the Brain Town outbound row:

| Field | Value |
| --- | --- |
| State | `enabled`, `disabled`, or `unusable: <reason>` |
| Host | the scheme and host of `BRAIN_TOWN_CONSUMER_URL` — never the path, the query or any userinfo (`urlHost`, `dashboard/integrations.go:39-60`) |
| Last event | when the last event was handed over |
| Frames | how many events were handed over |
| Skipped | how many were skipped as foreign or oversized |
| Reconnects | how many times the reader reconnected |

No token, no token hint, no path, no scope, no cursor value. The same rule the whole
view already follows: "It is read-only and it renders no secret"
(`dashboard/integrations.go:18-20`).

The counters are read **under the same mutex** the reader writes them with, exactly as
`Status()` does (`dashboard/braintown.go:599-606`).

## 6. The change list

| File | Change |
| --- | --- |
| `dashboard/braintown_consumer.go` | **New.** The configuration, the reader loop, the SSE frame parser, the double filter, the cursor, the status. |
| `dashboard/braintown_consumer_test.go` | **New.** The configuration rules, the frame parser, the double filter, the cursor, the reconnect. |
| `dashboard/view.go` | `IntegrationsPage` gains a `BrainTownConsumer` row (`dashboard/view.go:193-199`); `BrainTownRow` gains the consumer's fields (`dashboard/view.go:372-380`). |
| `dashboard/integrations.go` | The Integrations view gains the row (`dashboard/integrations.go:70`). |
| `dashboard/integrations_test.go` | The row's fields and its "no secret" rule. |
| `dashboard/templates/backoffice.html` | The row's markup (`dashboard/templates/backoffice.html:730-739`). |
| `dashboard/main.go` | The reader is constructed and started when it is configured. |
| `deploy/stacks/compose.prod.yaml` | The six settings, next to the existing `BRAIN_TOWN_*` block (`compose.prod.yaml:140-149`), and two secret mounts beside the existing one (`compose.prod.yaml:158`): `./prod.bt-consumer-token:/run/secrets/bt-consumer-token:ro` and `./prod.bt-consumer-cursor:/run/secrets/bt-consumer-cursor:ro`. |
| `deploy/stacks/dashboard.env.example` | The same settings, as examples. |
| `deploy/check-stacks.sh` | The assertion that the two token files are files and never environment values, beside the existing one (`deploy/check-stacks.sh:142-156`). |
| `dashboard/egress.go` | `bt_c_` joins the workspace secret list (`dashboard/egress.go:361`). |
| `docs/designs/gaos-brain-town-consumer.md` | **New.** This document. |
| `docs/designs/reviews/gaos-brain-town-consumer-review-1.md` | **New.** The comprehension report. |
| `docs/designs/reviews/gaos-brain-town-consumer-review-2.md` | **New.** The critic report. |
| `docs/designs/reviews/gaos-brain-town-consumer-security-review-1.md` | **New.** The security report. |
| `docs/designs/reviews/gaos-brain-town-consumer-review-3.md` | **New.** The readiness report. |
| `pustaka/docs/integrations/brain-town-consumer.html` | **New.** The published page. It uses **existing CSS classes only** — the closed docs vocabulary is never opened by a page (`.claude/rules/rule-index.md:42-44`), and `pustaka/ai/AUTHORING_SPEC.md` plus the sibling page `pustaka/docs/integrations/brain-town.html` are its template. |
| `pustaka/docs/assets/toc/integrations.js` | The page's registration, beside the sibling entries (`pustaka/docs/assets/toc/integrations.js:54`). |
| `pustaka/docs/product/qa-test-plan.html` | One row per new test, in the same change (`.claude/rules/qa-test-plan.md:19-25`). |
| `pustaka/docs/product/brd.html`, `decision-log.html`, `open-questions.html` | The counts the register rule keeps in step (`.claude/rules/qa-test-plan.md:86-90`). |
| `pustaka/docs/changelog.html`, `pustaka/docs/assets/toc.js` | The release hygiene pair (`.claude/rules/qa-test-plan.md:97-100`). |

**Counts.** The table has **21 rows**: 2 new Go files (the reader and its test), 6
changed Go files, 6 new documents (this design, the four review reports and the
published page) and 7 changed files that are not Go. The arithmetic: 2 + 6 +
6 + 7 = 21.

## 7. Alternatives

### 7.1 Brain Town pushes to GAOS — rejected

Brain Town could POST each matching event to a GAOS endpoint. It is the mirror image of
the feed GAOS already pushes, and it would give GAOS the events without a reader.

Rejected because it inverts the direction the operator asked for: GAOS is the party that
**declares what it needs**, and a push makes the pusher decide what the receiver gets.
With a pull, one operator action — registering a scope in Brain Town's back office —
registers the interest, and the scope is visible in Brain Town's own admin surface where
it can be audited and revoked. A push would also need Brain Town to hold a GAOS
credential and to retry a delivery, which is the machinery GAOS already owns on its own
side.

### 7.2 GAOS reads the admin stream — rejected

`GET /admin/events` carries the stored envelope. It needs a back-office session cookie,
which is an operator's credential: it expires, it is shared with a human, and GAOS holding
one would give GAOS the whole admin API. A consumer token is scoped to one read path and
revocable on its own.

### 7.3 GAOS reads the public stream and maps the aliases — rejected

The public stream is masked, so GAOS would receive an alias and no title. GAOS could keep
a table of alias → repository, but the aliases are the operator's display names, they
change, and a mapping table in GAOS is a second source of truth for something Brain Town
already knows. The consumer path asks for the repository by name and receives the name.

### 7.4 GAOS receives a GitHub webhook directly — rejected

It would duplicate Brain Town's receiver: the signature check, the delivery-id dedupe, the
per-repo event list, the mapping of every GitHub action to a town event. Brain Town has all
of it. GAOS's own receiver would have to agree with it event for event, forever.

### 7.5 The reader holds the de-duplication set — rejected

The reader could keep the seen event ids and drop a repeat itself. Rejected because the
set would be lost on a restart and would have to be rebuilt from a source that no longer
has the events; the card already carries the key, so the **flow** owns the set and the
graph is the durable record.

## 8. Failure and recovery

| Failure | What happens |
| --- | --- |
| Brain Town is unreachable | The reader reconnects with the waits 1 s, 5 s, 30 s, then holds at 30 s. No GAOS action is delayed. The Integrations view shows the reconnect count. |
| The token is wrong or revoked | The stream answers `401`. The reader stops and says so, and re-reads the token file on the next attempt, so a rotated token is picked up without a restart. It does **not** retry in a loop: a revoked token will not come back by itself. |
| Brain Town has no `/consumer/events` yet | `404`. The reader reports "the stream answers 404" and stops. This is the expected state before Brain Town ships the path. |
| The scope matches nothing | The reader connects and receives nothing. The Integrations view shows zero frames. A configuration mistake, visible in the view. |
| GAOS restarts | The reader reads the cursor and replays what it missed. The flow's de-duplication makes a replayed event a no-op. A reader with no cursor asks for the newest events instead and says so. |
| The flow is slow | The channel fills. The reader does **not** advance the cursor, so the event is replayed on the next reconnect. After a minute it rewinds the cursor and lets the server replay it. |
| The token file is loose or missing | The reader stays off and the reason says why. GAOS starts normally. |
| The cursor file is missing | The reader starts from the newest events and says so. It does not replay the whole retention. |

## 9. Security

- The token is a **file**, mode 600, and it is never logged, never rendered and never
  written to an event. The file's path is never logged either.
- The token must be a `bt_c_` token. `bt_live_` (a sender's credential), `gaos_` and
  GAOS's own API tokens are refused.
- The reader refuses a token file others can read.
- The URL is https, has no path, no userinfo, no query and no fragment; redirects are
  refused; the host must be the one the setting names and must not be an IP literal or a
  loopback, private or link-local address. This is stricter than the outbound feed's
  three-hostname denylist (`dashboard/braintown.go:85-88`).
- The reader holds no GAOS credential and asks GAOS for nothing.
- The pull-request title is attacker-authored: it is capped, control-stripped and screened
  with the egress detectors before it reaches a task title.
- A frame is at most 4 KiB and every field is capped, so a hostile frame cannot become a
  memory problem inside the dashboard process.
- The scope in Brain Town is the operator's declaration of what GAOS needs; GAOS filters
  again by its own products, and the repository GAOS acts on is the product's, never the
  event's.

## 10. Non-goals

- GAOS does not write to Brain Town. The `BRAIN_TOWN_*` **outbound** feed stays as it is.
- GAOS does not fetch the pull request's diff, body or author here. A GitHub client
  (`docs/designs/gaos-pr-agent-flows.md` §7) does that, with its own token.
- GAOS does not create tasks here. The reader only hands events over; the flow creates
  tasks.
- GAOS does not register the consumer in Brain Town. The operator does that, in Brain
  Town's back office, and hands GAOS the token.
- GAOS does not read the GitHub webhook. That is Brain Town's job.

## 11. Done when

- [ ] With the settings off, or the URL, the token file or the cursor file unusable, GAOS
      opens no connection and says why in the Integrations view.
- [ ] A `bt_c_` token opens the stream; a `bt_live_`, a `gaos_` and a GAOS API token are
      all refused.
- [ ] A token file others can read is refused; a private one is accepted.
- [ ] A `pr.opened` for a repository GAOS owns reaches the flow, and a `pr.opened` for a
      repository GAOS does not own does not — including a repository of the same name
      under another owner.
- [ ] A `pr.merged` reaches the flow.
- [ ] A disconnect replays the missed events, and the flow's de-duplication makes the
      replay a no-op.
- [ ] An event that could not be handed over is replayed rather than lost.
- [ ] The Integrations view shows the reader's state, host, counts and reconnect count —
      and no token, no path, no scope, no cursor value.
- [ ] The token never appears in a log line, a page or an event, and neither does the
      token file's path.
- [ ] `cd dashboard && go test -race ./...` and
      `cd pustaka && go test ./... && go run . check ./docs` are green,
      `./deploy/check-stacks.sh` and `./deploy/check-workspace.sh` are green, and the new
      test cases are registered in `pustaka/docs/product/qa-test-plan.html`.
