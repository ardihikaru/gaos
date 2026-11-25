# The pull-request flows — GAOS reacts to GitHub pull-request activity

> **Status:** Implemented (revision 3). Round 1 of the review is `docs/designs/reviews/gaos-pr-agent-flows-consolidated-1.md`; the operator
> approved implementing revision 2 without a second review round, and revision 3 records where the
> implementation differs (each difference is marked **As built**).
> **Author:** the GAOS team
> **Reviewers:** the five `gaos-goldfish-*` agents (`docs/rules/gaos-design-review.md`)
> **Policy:** `docs/rules/gaos-design-review.md`
> **Related:** `docs/designs/gaos-brain-town-consumer.md` (where the events come from)
> **Decision:** D-088 (allocated by this design; see §16)
> **Baseline:** every "today" claim in this document describes `main` at `ba20daf`. The worktree
> holds code written before this review; it is a spike, kept only where it matches this revision.

## 1. Problem

A product's main repository receives pull requests. Somebody has to review them and somebody has
to prove they still work. Today both are manual, and only the author knows anything happened.

GAOS already holds a product's plan, its Blueprint and a board of tasks, and already dispatches
agents into the product's workspace. It lacks the **trigger** and the **work** for pull-request
events. The trigger exists: Brain Town receives the GitHub webhook (and verifies its signature)
and emits `pr.opened` and `pr.merged` town events, which `docs/designs/gaos-brain-town-consumer.md`
makes GAOS read. This document is the rest: the routing, two agents, two QA-tool jobs, a label
vocabulary, a contact registry, and three outside calls (a GitHub review comment, the QA tool,
WhatsApp).

## 2. What GAOS will do

For a product whose main repository is under GAOS's care:

| Trigger | GAOS opens | Worked by |
| --- | --- | --- |
| A pull request is **opened** (not a draft, not from a fork) | a **PR review** card | the `pr-reviewer` agent |
| A pull request is **opened** (same) | an **e2e automation** card | the QA tool (§8.1) |
| A pull request is **merged** | a **unit test** card | the `unit-test` agent |
| The **staging deploy** finished | an **e2e run** card | the QA tool (§8.1) |

Two of the four are agents; two are QA-tool jobs that GAOS drives with no model turn, because the
QA tool already does the work (§8.1) and a model would only repeat it.

### 2.1 Glossary

- **Handoff block** — the fenced reply block an agent ends its turn with; `contractsFor`
  (`dashboard/adapter_aimanager.go`) says which block a task kind must return.
- **Context part** — a labelled piece of text GAOS attaches to a packet as data, never as a prompt.
- **Needs you** — `StatusAwaitingInput` (`dashboard/taskstate.go`).
- **The QA tool** — the sibling `qa-automation-tools` service GAOS already calls through
  `dashboard/qaclient.go`.
- **GoWA** — a self-hosted WhatsApp web gateway (`github.com/aldinokemal/go-whatsapp-web-multidevice`).

## 3. The task labels

A task carries a **list** of labels, one per family, so a card shows at a glance whether it waits on
a person or an agent.

| Label | Family | Meaning | Set by |
| --- | --- | --- | --- |
| `pr-review:auto` | review | an agent reviews the pull request | routing (default) |
| `pr-review:manual` | review | a human reviews it; **the scheduler never dispatches the card** | an operator |
| `pr-review:needs-manual` | review | the agent ran, or was refused (§7.1), and a human must take over | GAOS, when the reply is `severity: high`/`critical`, or the egress pass refused the packet |
| `e2e:automation` | e2e | the e2e automation job | routing |
| `e2e:run` | e2e | the e2e run job | routing |
| `unit:test` | unit | the unit test card | routing |

Rules: a card holds at most one label per family; `pr-review:*` labels exclude each other; a label
that is not in the table is refused with `422`. The vocabulary lives in `dashboard/labels.go`.

**Enforcement.** `Task.labels` is a `LIST OF STRING`, which a DDL regexp cannot constrain, so the
DDL carries no regexp for it. Two tests hold the set: `TestTheTaskLabelVocabularyIsClosed`
(`labels_test.go`) asserts the table above equals `labels.go`'s set and that every other string is
refused; the role/DDL test (§4) is a separate matter.

**Labels ride on the task.** They are part of the `TaskPlanned` payload (a new `labels` field), so
a replay keeps them. An operator changes a label with `POST /products/{id}/board/cards/{taskId}/label`, which emits
`TaskLabeled` (§9) naming the signed-in account's alias. Every signed-in back-office account is an
"operator" until the back office has roles (D-085); that is recorded as an open question.

The **event catalog** and the **task-status** vocabulary stay closed apart from the five event
types in §9.

## 4. The roles

`roles` in `dashboard/fixtures.go` is the single map both stores read (`store_arcade.go`,
`view.go`, `vendors.go`), and `roleOrder` (same file) is the display order. Two entries are added
to both:

| Role key | Label | Room | Model |
| --- | --- | --- | --- |
| `pr-reviewer` | PR Reviewer | `architecture` | `claude-opus-5` |
| `unit-test` | Unit Test Builder | `qa` | `claude-sonnet-5` |

`review` is a stage, not a room (`fixtures.go` rooms: `architecture, backend, qa, release,
knowledge, meeting`), so the reviewer sits in `architecture`, with `security`. The model tiers
follow `docs/rules/` practice: judging a diff is the strongest tier, writing code the middle one.

**Agents.** The scheduler blocks a task with "no agent holds the role" when none does
(`dashboard/scheduler.go`). `agents` in `fixtures.go` gains one agent per new role, and the ArcadeDB
bootstrap reads that list, so the cards are dispatchable. The done-when bar is "dispatchable", not
"accepted by the store".

**`Task.role` is a closed vocabulary.** `dashboard/schema/04-execution.sql` constrains it with a
regexp that gains the two keys. Today `TestTheClosedVocabulariesMatchTheDDL`
(`dashboard/designgate_test.go`) checks the closed vocabularies against the DDL in both
directions as subtests; the role/DDL agreement is held by `TestTheTaskRoleVocabularyMatchesTheDDL` in
`labels_test.go` (**As built:** a new top-level test beside the label vocabulary test, not a subtest, because it
also checks `roleOrder`). The DDL is **extracted verbatim** from `artifacts/v1/gaos-arcadedb-model.html`
(`04-execution.sql`, header), so the edit is made in that document and the file is re-extracted.

The e2e cards use the existing role `qa`: the QA tool is the worker, so no new role and no policy
is needed for them.

## 5. The two policies

A Blueprint policy is a standing rule rendered into the workspace. Two **seed policies** are
shipped, each of kind **`custom`**: `policyFile()` already sends a custom policy to
`.claude/rules/65-<key>.md`, and `rulesFile` is the generic pattern
`^\.claude/rules/[a-z0-9][a-z0-9._-]{0,80}\.md$`, which already admits these paths. So
`dashboard/blueprint.go` does **not** change: no new kind, no `rulesFile` edit.

| Policy key | Rendered file | What it says |
| --- | --- | --- |
| `pr-review` | `.claude/rules/65-pr-review.md` | the severity ladder (`none, low, medium, high, critical`), the reply block (§7), what to review |
| `unit-test` | `.claude/rules/65-unit-test.md` | how to write and run the unit tests for the changed code, where the JUnit file lands |

**The reviewer's instructions are Blueprint content, not a repository file.** An earlier draft told
the agent to read `.claude/agents/pr-reviewer.md` from the product's checkout. That file is outside
GAOS's allowlist (BR-42), is never hashed or drift-gated, and, if read from a pull request's
head, lets the author rewrite their own reviewer. The policy therefore carries the reviewer's
instructions itself and names no repository file. The pull request is checked out **detached at
its head** into `<workspace>/pr/<number>` as read-only context for the agent; the agent's own
instructions never come from that tree.

## 6. The routing

`dashboard/prflow.go` takes a `PREvent` (`dashboard/braintown_consumer.go`) and plans cards.

### 6.1 The table

| Event | Card | Kind | Role | Labels | Initial status | Depends on |
| --- | --- | --- | --- | --- | --- | --- |
| `pr.opened` | PR review | `implement` | `pr-reviewer` | `pr-review:auto` | `PENDING` | — |
| `pr.opened` | e2e automation | `implement` | `qa` | `e2e:automation` | `PENDING` (the scheduler promotes it), run by the QA-tool job runner (§8.1) | — |
| `pr.merged` | unit test | `implement` | `unit-test` | `unit:test` | `PENDING` | — |
| staging deployed | e2e run | `implement` | `qa` | `e2e:run` | `READY`, run by the QA-tool job runner | — |

**The PR review is `kind: implement`, not `kind: review`.** In GAOS a `review` task is the reviewer
of a written design: it must depend on a delivered design task (`dashboard/graph.go` plan
validator), must return a `gaos-review` verdict on that design (`dashboard/designgate.go`,
`review-missing`), and BR-46/BR-47 bind it. A pull request has no design to send back, so the
design gate does not apply to these cards, and BR-46 does not require them to wait on a review.
The reviewer returns its own reply block, `gaos-pr-review`, defined in §7.

**Triggers that open no card:** a draft pull request, and a pull request whose head repository
differs from its base repository (a fork). Both are refused by default and logged as
`prflow: skip <class>`; a product setting `prAllowForks` may open the second. A **cap** of
`prMaxOpen` (default 5) open PR cards per product refuses the sixth with a log line, so a burst of
pull requests cannot starve the product's own work. One live turn per product still holds
(BR-38); PR cards queue behind it like any other card.

**Why not the BR-54 `BLOCKED`. (As built: cards are born `PENDING`, which the scheduler promotes to `READY`.)** BR-54 makes a QA-token card born `BLOCKED` because the
QA token is a third party's. This trigger is authenticated by the town's signature verification,
the product's own repository pair, the draft/fork filter and the cap, and the label
`pr-review:auto` is the operator's standing consent. D-088 records that exception; an operator who
wants BR-54's behaviour sets `pr-review:manual`, which the scheduler never dispatches.

### 6.2 The de-duplication key

```
<owner>/<repository>#<pull request number>:<role or job>
```

for example `acme-org/sample-service#4931:pr-reviewer`. The owner is part of the key, so
`acme/api#4` and `other/api#4` never collide. A staging deploy has no pull request, so its key is

```
<owner>/<repository>@<commit sha>:e2e-run
```

The key is stored on the card, in a new `Task.dedupKey STRING` property with a unique index, and
`PREvent.Key` (`braintown_consumer.go`) is the **only** source of the pull-request form, so two
formats cannot exist. **As built:** there is no separate cache. The store holds every card, so the key is looked up on the cards
themselves (`taskByDedupKeyLocked`) and an eviction cannot make a duplicate; `Task.dedupKey` is indexed
`NOTUNIQUE` (a unique index would have to tolerate a null on every other task).
A second event with an existing key is a no-op and logs `prflow: duplicate`.

### 6.3 Where the cards live

The cards attach to the product's **PR workflow**, `wf-pr-<product>`, created once and never set as
the desk's `WorkflowID` (`dashboard/store_mem.go`, plan path), so a pull request never resets the
product's own plan, progress or score. `PlanPullRequestCards` (a store method) creates the tasks
with their labels, emits `TaskPlanned` for each and one of `PullRequestOpened`/`PullRequestMerged`
**once per event** (§9), and returns the ids. The ArcadeDB store delegates under the same lock as
every other mutation (`store_arcade.go`).

The plan goes through `validatePlan` like any other; because the PR cards are `implement` tasks
with no review dependency, BR-46 is satisfied by D-088's carve-out (they are not product plan
work), recorded in the decision and checked by a test.

### 6.4 `pr.merged` without `pr.opened`

A reconnect can replay a merge without its open. The merged handler works alone: it creates the
unit-test card and never looks for the review card. A pull request **closed unmerged** cancels its
open review and e2e cards (status `CANCELLED`); a merge does the same for a review that has not
started. New commits to an open pull request (`synchronize`) are **not** handled in this release;
the reviewer reviews the head at dispatch time, and that is listed under §14.

### 6.5 Crash safety

The consumer advances its cursor after hand-over to a bounded channel, not after commit
(`gaos-brain-town-consumer.md` §5), so an event handed over but not yet planned when the process dies is
lost. **As built, this is a known gap, not closed here**: closing it needs an acknowledgement channel in the
consumer, which is that design's to change. The drain plans each event as soon as it arrives, so the window
is short; §12 says so.

## 7. The GitHub client

`dashboard/github.go` is new; GAOS has no GitHub client today.

| Call | Purpose |
| --- | --- |
| `GET /repos/{owner}/{repo}/pulls/{number}` | title, body, author, author association, draft flag, head and base repository |
| `GET /repos/{owner}/{repo}/pulls/{number}/files` | the diff, **paginated** (`per_page=100`, capped at 30 pages, a file without `patch` is listed by name only) |
| `GET /repos/{owner}/{repo}/pulls/{number}/commits` | the last committer's login, for notification routing |
| `POST /repos/{owner}/{repo}/pulls/{number}/reviews` | the review comment |

**The credential is a file**, `GAOS_GITHUB_TOKEN_FILE`, mode 600, holding a token with
`pull_requests: write` on the product's repositories; read once at boot, held in memory, never
logged, rendered, written to an event or placed in a workspace or a packet. Only the dashboard
process holds it and only `github.go` calls GitHub. A test greps the rendered Blueprint files and
a built packet for the token. A token file others can read is refused (`api_v1.go` token loader,
BR-52). The token's kind is refused: `bt_live_`, `bt_c_` and `gaos_` prefixes and any value equal to
one of GAOS's own tokens, as `braintown.go` refuses a GAOS credential on the outbound feed.

**The client refuses redirects** (as `qaclient.go` does) and reads a GitHub rate-limit response
(`403` with `X-RateLimit-Remaining: 0`, or `429`) as a retryable failure with the `Retry-After` it
names, bounded to two attempts.

**The repository comes from the product, never from the event.** The product carries
`Master.Repos` (`dashboard/model.go`), `owner/repo` pairs; the reader resolves the frame to a
product by that pair, and the client is called with the **product's** pair. The trust anchor is
that Brain Town verified GitHub's webhook signature before it emitted the town event; this design
depends on that and states it.

**Who posts the review: GAOS.** The agent ends its turn with a `gaos-pr-review` block:

```
severity: none | low | medium | high | critical
body: <the review text, at most 6 000 characters>
```

`dashboard/adapter_aimanager.go` (`contractsFor`) learns the block for the `pr-reviewer` role; GAOS
parses it, runs the **body through the egress detectors** (BR-32) before posting, and posts it as
`event: COMMENT`, never an approval. If the detectors fire, nothing is posted, the card goes to
**Needs you** and takes the label `pr-review:needs-manual`. An agent never holds the token.

**GitHub's answers.** `201` means posted. `401`, `403` and `404` mean the token may not write to
that repository; `422` means GitHub refused the review (for example the pull request is closed).
Each moves the card to **Failed** with a class that names the cause, not the response body.

### 7.1 The untrusted input

The pull request's title, body and diff are **untrusted text**. They travel as **context parts**, and
the pre-dispatch egress pass (`Screen`, `dashboard/egress.go`) runs over the packet. **A detector
hit refuses the whole dispatch** (it does not strip the secret), so a pull request that contains a
test credential or a PEM header gets no automated review: the card goes to **Needs you** with the
label `pr-review:needs-manual` and a reason, and the operator is notified (§8.3). A diff over the
size limit is refused the same way. Planting a string to suppress review therefore produces a
visible, manual card, not silence.

The title ends up in a card title that the outbound Brain Town feed republishes, so it is capped at
200 characters, stripped of control characters and screened before it reaches the card (the same
three steps as `braintown.go`'s outbound title). If the screen fails, the card's title is the task
id, as `btTitle` does.

**As built:** a refused dispatch moves the card to `BLOCKED` with the egress reason on the stream
(`DispatchRefused`), as it does for any task; it does not add `pr-review:needs-manual` or send a
notification. A person sees the blocked card and the reason.

### 7.2 "What to test" and the `caseCode`

The pull-request author states what to test in the body, under the headings the product's template
fixes. **As built:** no heading is read, so there is no `prTestHeadings` setting; the `QA-case:` line is the only
convention. The e2e automation job needs a **QA case code**: the author names it on a
line `QA-case: <code>` in the body. If the line is absent, the card does not guess. **As built:** the questions mechanism is agent-initiated, so
the job runner moves the card to `BLOCKED` with the reason "no QA-case line in the pull request description;
add `QA-case: <code>` there and move the card to Ready", and picks it up again when a person releases it.

## 8. The three outside calls

### 8.1 The QA tool, and who writes the spec

**The QA tool writes the spec.** Its `caseauto.Machine` already does generate → write → run →
repair (up to 3) → approve → push → PR **in the automation repository**; GAOS adds no automation
logic and **no model turn**. The `e2e:automation` and `e2e:run` cards are therefore not dispatched
to an agent. A GAOS-side **job runner** (`dashboard/qajobs.go`, new) drives them:

1. call `POST /api/v1/gaos/automation` with `{product, caseCode, mode:"create"|"run"}` →
   `202 {job_id}`;
2. poll `GET /api/v1/gaos/jobs/{id}` every 15 seconds, at most 40 minutes, until `status` is
   final: `succeeded` → card `DONE`; `failed` or `error` → `FAILED`; timeout → `FAILED`
   with reason `qa-job-timeout`.

The scheduler skips a task carrying `e2e:automation` or `e2e:run` (`dashboard/scheduler.go`), so the
job runner is its only driver. The job holds no product live turn, so BR-38 is untouched.

**The QA tool opens a pull request in the automation repository.** §14's "an agent opening a pull
request" non-goal is about the *product's* repository; this release states the exception for the
QA tool's own automation repository, which is the tool's existing behaviour behind its own
credential. GAOS holds no push right.

**The e2e run's scope.** A staging deploy carries no case, so `mode:"run"` is called with
`caseCode:"*"`: the QA tool runs every automated case of the product against staging.

**This is a change in the QA tool's repository** (`qa-automation-tools`), under its existing
`GAOS_INBOUND_TOKEN` guard: a thin authentication and DTO shim over `caseauto.Machine`. It is a
**dependency**, designed and reviewed in that repository. Until it exists, the job runner marks the
card `FAILED` with reason `qa-routes-missing` on a `404`, and the e2e cards are the only part of
this release that waits for it. `dashboard/qaclient.go` gains the two calls.

The **results** come back by the existing `POST /api/v1/products/{id}/qa/results`
(`dashboard/api_v1.go`); BR-54 means a QA result never moves a task, so the job's own `status` is
what closes the card.

### 8.2 The staging callback

Jenkins calls GAOS after the staging deploy:

```
POST /api/v1/products/{id}/staging
Authorization: Basic base64(user:pass)
{"repo":"acme-org/sample-service","branch":"staging","commit":"<sha>","environment":"staging"}
```

**This route is not bearer.** BR-52 and BR-55 say the machine API answers only a bearer token and
every other route must be bearer or on the fixed public list. Jenkins' credential step speaks HTTP
Basic, so D-088 amends both rules for **this one route**: it is added to the fixed list with its own
gate. No other route changes.

- **The credential is per product.** `GAOS_STAGING_BASIC_AUTH_FILE` (mode 600) holds lines
  `productId:user:password`. GAOS compares a SHA-256 of the presented credential with
  `crypto/subtle` against the line for the **path's** product; a credential that is valid for
  product A but presented for product B is `404`, as the bearer clients' product check answers
  (`apiProduct`, `api_v1.go`). A failure is `401` with `WWW-Authenticate: Basic realm="gaos"`.
  The credential is never logged or rendered.
- **Throttling.** Failures are throttled per source address with `Retry-After`, using the
  back-office login's throttle (`auth.go`).
- **The scope**: the `repo` in the body must be one of the product's own repositories, or `422`.
- **The effect**: creates the `e2e-run` card; emits `StagingDeployed`. De-duplication uses the key
  `owner/repo@sha:e2e-run` (§6.2), so a replay of the same commit is a `200` no-op, and a new
  commit while a card is open is a new card; at the cap the answer is `409`.
- `202 {accepted:true, taskId}`.
- The mount sits beside the bearer group (`api_v1.go` route registration), still inside the
  product-scope check.

### 8.3 WhatsApp

`dashboard/wa.go` is new. The gateway is a GoWA v8 instance the operator runs.

- `POST {API_URL}/send/message`, `Authorization: Basic …`, body `{"phone":"<group jid>","message":"<text>"}`.
- The answer is `{"code":"SUCCESS","message":…,"results":{"message_id":…,"status":…}}`.
  **A `200` whose `code` is not `SUCCESS` is a failure.**
- **The group JID is authoritative**: it comes from `GAOS_WA_GROUP_FILE` (mode 600, a `@g.us` JID).
  `GET /user/my/groups` is called **once at boot**, only to check that the pinned JID still
  exists; if it does not, notifications are disabled and the Integrations view says so (fail
  closed). There is no name lookup, because group names are not unique and anyone can set one.
- **Settings:** `GAOS_WA_URL` (**no default**; unset means disabled; `https` only, refused
  otherwise), `GAOS_WA_BASIC_AUTH_FILE` (mode 600), `GAOS_WA_GROUP_FILE` (mode 600). There is no
  `GAOS_WA_ENABLED`.
- **Semantics:** a `200` with a `message_id` means *accepted by WhatsApp*, never "delivered".
- **Logs:** never a group id, a number or a message body. The log line is
  `wa: notify <alias> sent|failed <class>`, where the **alias** is the contact's `displayName`
  slug (§8.4) or `group` for the fallback.
- **Retry:** one attempt plus **one** retry after 2 seconds, for transport errors only. A `400` and
  a `429` are never retried.
- **A failed notification never fails the work.** The finding is already on the card; a failed
  send **As built:** the failure is one `ContactNotified` event with `outcome: failed`; nothing is written on the card
  and its status is unchanged. §12 follows.

**When a notification is sent.** A review whose reply is `severity: medium`, `high` or `critical`,
or a refused dispatch (§7.1); an e2e or unit job that ends `FAILED`. The message is one line and the
card id (findings live on the card).

### 8.4 The contact registry

`dashboard/contacts.go` maps a GitHub account to a destination. It is **not** part of `Master`
(`dashboard/model.go`), so it is never rendered into a Blueprint, a packet or a workspace file, and
the §10 row for `model.go` does not touch it.

| Field | Meaning |
| --- | --- |
| `productId` | `p-6`-style id, or empty for every product |
| `githubLogin` | the GitHub account |
| `displayName` | the person's name; its slug is the **alias** |
| `channel` | `whatsapp` |
| `destination` | the group JID or number |
| `notifyOn` | any of `review:medium`, `review:high`, `e2e:failure`, `unit:failure` (a value includes the worse severities) |

**Storage.** A `Contact` vertex type in `04-execution.sql` (extracted from the model document), with
a unique index on `(productId, githubLogin)`, read and written through the store methods
`ListContacts`, `UpsertContact` and `DeleteContact` (`store.go`, `store_mem.go`,
`store_arcade.go`). The contact is never in an event: `ContactNotified` carries the alias, the
message kind and the outcome only.

**Write path (as built).** `POST /contacts` and `POST /contacts/delete`, behind the session, rendered by a
section at the foot of the Integrations view in `dashboard/templates/backoffice.html`; the Integrations rows
above it carry counts, and the list shows each destination masked. A contact change adds **no event**
(the registry is deliberately outside the event stream, so a number can never reach the town); it
is recorded in the application log as `contact: upsert|delete <alias> by <actor-alias>`, never the
destination. The back-office list shows the destination masked (`…@g.us`); the Integrations view
shows counts only.

**Resolution order.** A review finding goes to the pull request's **author's** contact; a test
failure goes to the **last committer's** contact (from `…/pulls/{n}/commits`, §7; for a staging
deploy with no pull request, the commit's author, from `GET /repos/{o}/{r}/commits/{sha}`); when no
contact matches, the message goes to the pinned group.

## 9. The new event types

The event catalog is closed (BR-05). Each type lands in **the data-model document, `events.go`, the
fold and the catalog test** together:

| Event type | Emitted when |
| --- | --- |
| `PullRequestOpened` | a `pr.opened` event arrived and its cards were planned |
| `PullRequestMerged` | a `pr.merged` event arrived and its card was planned |
| `StagingDeployed` | the staging callback landed and its card was planned |
| `ContactNotified` | a WhatsApp message was attempted: alias, message kind, outcome — never the destination, never the body |
| `TaskLabeled` | an operator changed a card's label: the task id, the old and new label, the actor's alias |

`TaskLabeled` is a fifth type that the operator's "keep all four" did not name; it is required
because a label change is a state change and BR-02 says a state change with no event never
happened. If the operator prefers, labels can be made immutable after planning instead, which
removes the type and the `manual` switch.

The data-model document is `artifacts/v1/gaos-arcadedb-model.html` (§08 catalog); the DDL is
re-extracted from it. `dashboard/events_test.go` keeps its own hand-written copy of the catalog on
purpose, so the five names are added there, and `pustaka/docs/architecture/graph.html` is updated
as earlier event additions were.

## 10. The change list

**As built**, counted by file against `ba20daf`: **62 files**, 20 new and 42 changed. The review reports in
`docs/designs/reviews/` are the record, not part of the change.

**New (20).** Nineteen Go files: nine implementation/test pairs and one store file.

| Pair | Holds |
| --- | --- |
| `labels.go`, `labels_test.go` | the closed label vocabulary, one label per family, `labelsAllowDispatch` |
| `prflow.go`, `prflow_test.go` | the routing table, filters, cap, key, `PlanPullRequestCards`, `SetTaskLabel`, `ResolvePRCard` |
| `prreview.go`, `prreview_test.go` | the `gaos-pr-review` block, the screen, the context cache, the post |
| `github.go`, `github_test.go` | the client: pages, classes, rate limit, redirect refusal, commit authors |
| `staging.go`, `staging_test.go` | the Basic callback, per-product credentials, the throttle |
| `wa.go`, `wa_test.go` | the notifier: https only, pinned group, one retry |
| `contacts.go`, `contacts_test.go` | the registry, resolution, masking, the back-office routes' tests |
| `notify.go`, `notify_test.go` | the notification service |
| `qajobs.go`, `qajobs_test.go` | the job runner and the two QA calls |

Plus `dashboard/store_arcade_contacts.go` (the `Contact` vertex's write and load), and the published page
`pustaka/docs/integrations/pr-agents.html`.

**Changed (42).** 21 Go: `adapter_aimanager`, `api_v1`, `board`, `braintown_consumer`,
`braintown_consumer_test`, `events`, `events_test`, `fixtures`, `fold`, `handlers_board`, `handlers_bo`,
`integrations`, `main`, `model`, `runner`, `scheduler`, `schema_test`, `store`, `store_arcade`, `store_mem`,
`view`. 21 not Go: `schema/04-execution.sql`, `artifacts/v1/gaos-arcadedb-model.html`, `backoffice.html`,
`app.css`, `deploy/seed/example/product.json`, `compose.prod.yaml`, `check-stacks.sh`, `check-workspace.sh`,
`.gitignore`, `README.md`, `.claude/rules/qa-test-plan.md`, this document, and nine pages of `pustaka/docs`
(`graph.html`, `integrations.js`, `toc.js`, `product.js`, `changelog.html`, `brd.html`, `decision-log.html`,
`open-questions.html`, `qa-test-plan.html`).

**Not changed, against revision 2:** `blueprint.go` (§5), `qaclient.go` (the two QA calls live in `qajobs.go` on
`qaOutbound`), `seed.go` (the seed policies are in `deploy/seed/example/product.json`), `dashboard.env.example`
(every new credential is a file, and that file holds only environment credentials), `designgate_test.go`.

Arithmetic: 19 + 1 new, 21 + 21 changed: 20 + 42 = **62**.

## 11. Phases

Each phase ends when its command is green; later phases depend on earlier ones.

| Phase | Content | Exit command |
| --- | --- | --- |
| 1 | Vocabularies: labels, roles, agents, model document, DDL re-extraction, the five events and fold, catalog tests | `cd dashboard && go test -run 'Vocabular\|Catalog\|Fold' ./...` |
| 2 | `PlanPullRequestCards`, the store methods, `prflow.go`, the cap and filters, the `prflow` wiring to the consumer | `go test -run 'PRFlow\|Plan' ./...` |
| 3 | The GitHub client, the `gaos-pr-review` contract, the review post | `go test -run 'GitHub\|PRReview' ./...` |
| 4 | The staging route and the throttle | `go test -run 'Staging' ./...` |
| 5 | WhatsApp, the contacts and the back-office section | `go test -run 'WA\|Contact' ./...` |
| 6 | The job runner and `qaclient.go` (blocked on the QA-tool routes for a live check; unit tests use a fake) | `go test -run 'QAJob' ./...` |
| 7 | Deploy files, docs, register counts | `go test -race ./...`, `cd pustaka && go test ./... && go run . check ./docs`, `./deploy/check-stacks.sh`, `./deploy/check-workspace.sh` |

## 12. Failure and recovery

| Failure | What happens |
| --- | --- |
| Brain Town is down | No events, no cards. Nothing else is affected. The consumer reconnects. |
| The GitHub token is missing, or may not write | The review card moves to **Failed** with the class. It is not closed silently. |
| The QA tool is down, or lacks the routes | The e2e card is **Failed** with `qa-job-timeout` or `qa-routes-missing`. |
| WhatsApp is down | The card's status is **unchanged**; the failed send is recorded on the card and in `ContactNotified`. Nothing is retried into the group beyond §8.3's one retry. |
| Jenkins never calls back | No staging card. The Integrations view shows the last callback per product, so the silence is visible. |
| GAOS restarts mid-flow | The consumer replays from its last cursor and the `dedupKey` makes the replay a no-op. An event handed over but not yet planned when the process dies is lost (§6.5). A PR context fetched but not yet dispatched is lost too, and the packet says so. |
| The egress pass refuses a pull request | The card is `BLOCKED` with the egress reason on the stream (§7.1, as built). |

## 13. Security

- Every credential is a **file**, mode 600, named by an environment variable; none is an environment
  value, a log line, a page or an event. There are four: GitHub, staging, WhatsApp auth, WhatsApp
  group.
- The GitHub token never reaches an agent, a packet or a workspace file; the review body is screened
  before it is posted.
- The reviewer's instructions are Blueprint content (§5), never a file from the pull request.
- The WhatsApp URL is `https` only, has no default, and names no private host in this repository
  (D-085); the group JID is pinned, never found by name.
- The staging credential is per product, compared in constant time, throttled, and replay-safe by
  commit.
- The contact registry is the one place personal data lives; it is never in an event, a title, a
  packet or the Blueprint.
- The pull-request title, body and diff are untrusted; a draft or a fork opens nothing, and a
  per-product cap bounds the spend.
- Every label and contact change names the actor's alias.

## 14. Non-goals

- Any product other than the one this release is written for; no product is hard-coded.
- A GitHub review **approval** (`event: APPROVE`). Only `COMMENT`.
- An agent opening a pull request in the **product's** repository. (The QA tool's own automation
  pull request is the stated exception, §8.1.)
- A model turn for the e2e work.
- Reviewing again on new commits to an open pull request (`synchronize`).
- A fifth agent.
- Turning the GAOS → Brain Town feed on.
- Roles in the back office (an open question).

## 15. Done when

- [x] A `pr.opened` event for a repository GAOS owns opens a review card and an e2e card, both
      labelled; a second copy opens nothing; a draft or fork opens nothing.
- [x] A `pr.merged` event opens a unit-test card with no review card present.
- [x] A card's labels render, an unknown label is refused, a label change emits `TaskLabeled`, and
      `pr-review:manual` is never dispatched.
- [x] The two roles are dispatchable, and the DDL and the Go set agree.
- [x] The two seed policies render to their own files and the Blueprint carries no contact field.
- [x] A review finding reaches the pull request as a `COMMENT`, screened first; a medium-or-worse
      finding reaches the contact or the pinned group.
- [x] The staging callback answers `401` without the credential, `404` for another product's
      credential, `422` for a repository the product does not own and `202` with a card.
- [x] A WhatsApp answer whose `code` is not `SUCCESS` is a failure, a `429` is not retried, and a
      failed send does not fail the card.
- [x] The seven phases' exit commands are green and every new test is in
      `pustaka/docs/product/qa-test-plan.html` (TC-534 to TC-599).

## 16. Registers

- **D-088** (this design): PR cards are born `READY` under `pr-review:auto`; the PR review is an
  `implement` task outside BR-46/BR-47; BR-52/BR-55 are amended for the one staging route; the QA
  tool writes the e2e specs.
- **Test cases** are allocated from **TC-534**, one per new `func Test…` and per new
  `check-stacks.sh` assertion; the next free number is then written back into
  `.claude/rules/qa-test-plan.md`.
- **Open questions** gained: back-office accounts have no roles; the QA-tool routes are a dependency
  in another repository.
- The release version is the next minor after `v0.46.0`.

## 17. Alternatives

- **One agent for all four jobs** — rejected: the reviewer reasons (strongest tier), the unit-test
  agent writes code (middle tier), and the e2e jobs need no model at all.
- **The agent posts its own review** — rejected: it would put a write-scoped token into a workspace
  that reads untrusted text. GAOS posts the screened body.
- **The WhatsApp message carries the findings** — rejected: it would leak code into a chat. One line
  and the card id.
- **The staging callback over the bearer API** — rejected for this release because Jenkins' credential
  step is Basic and a bearer client would need a token lifecycle Jenkins does not hold; the cost is
  the D-088 amendment of BR-52/BR-55 for one route. A bearer client in the clients file remains the
  simpler path if Jenkins can send one, and is recorded as the follow-up.
