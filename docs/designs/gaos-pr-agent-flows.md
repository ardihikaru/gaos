# The pull-request agents — GAOS reacts to GitHub pull-request activity

> **Status:** Draft
> **Author:** the GAOS team
> **Reviewers:** the five `gaos-goldfish-*` agents (`docs/rules/gaos-design-review.md`)
> **Policy:** `docs/rules/gaos-design-review.md`
> **Related:** `docs/designs/gaos-brain-town-consumer.md` (where the events come from)

## 1. Problem

A product's main repository receives pull requests. Somebody has to review them, and
somebody has to prove they still work. Today both are manual, and the person who opened
the pull request is the only one who knows anything happened.

GAOS already holds the plan for a product: its requirements, its stories, its Blueprint
of policies, and a board of tasks. It also already runs agents: a task is dispatched to
a worker that works in the product's own workspace, with the Blueprint's rules in force.
What GAOS lacks is the **trigger** and the **agents** for pull-request work.

The trigger exists in Brain Town, which already receives the GitHub webhook and already
turns it into `pr.opened` and `pr.merged` town events. The design in
`docs/designs/gaos-brain-town-consumer.md` is how GAOS reads them.

This document is the rest: four agents, the routing that starts them, the labels that
say which kind a task is, and the three ways GAOS talks to the outside world when
something needs a human — a GitHub review comment, the QA tool, and WhatsApp.

## 2. What GAOS will do

For a product whose main repository is under GAOS's care:

| GitHub event | GAOS does |
| --- | --- |
| A pull request is **opened** | Opens a **PR reviewer** card and an **e2e automation builder** card. |
| A pull request is **merged** | Opens a **unit test builder and runner** card. |
| The **staging deploy** finished | Opens an **e2e automation test runner** card. |

The four agents:

| Agent | Role key | What it is for |
| --- | --- | --- |
| PR reviewer | `pr-reviewer` | Reads the diff against the product's coding policy and recommends changes. |
| E2E automation script builder | `e2e-builder` | Writes the Playwright spec for a new automation. |
| E2E automation test runner | `e2e-runner` | Asks the QA tool to run an existing spec against staging. |
| Unit test builder and runner | `unit-test` | Writes and runs the unit tests for the changed code. |

## 3. The task label

Every task this flow creates carries a label, so an operator can see at a glance whether
a card is waiting on a person or on an agent.

| Label | Meaning | Default | WhatsApp |
| --- | --- | --- | --- |
| `pr-review:auto` | an agent reviews the pull request | **yes** | on a medium-or-worse finding |
| `pr-review:manual` | a human reviews the pull request | no | no |
| `pr-review:needs-manual` | the agent ran and a human must take over | no | yes |
| `e2e:automation` | the e2e script builder | no | on failure |
| `e2e:run` | the e2e test runner | no | on failure |
| `unit:test` | the unit test builder and runner | no | on failure |

**The vocabulary is closed.** A label that is not in this table is refused at write time
(`422`), exactly as the task-status vocabulary is refused by a DDL regexp
(`dashboard/schema/04-execution.sql:28-30`). The vocabulary lives in one file,
`dashboard/labels.go`, and one test asserts that the Go set and the DDL agree.

The default for a pull-request review is `pr-review:auto`: an agent reviews unless a
human says otherwise. An operator changes a card's label on the board, the way they
change any other card.

**Labels are not event types.** They are task data, so they ride on the task and are
rendered on the card. The **event catalog** is a different closed vocabulary
(`dashboard/events.go:30-32`) and this design does not touch it — except for the four new
event types in §9, which are recorded there and in the fold together.

## 4. The four roles

`dashboard/fixtures.go:15` holds the single `roles` map that **both** stores read
(`store_arcade.go:581`, `view.go:368`, `vendors.go:376`). Four entries are added:

| Role key | Label | Room | Model |
| --- | --- | --- | --- |
| `pr-reviewer` | PR Reviewer | `review` | `claude-opus-5` |
| `e2e-builder` | E2E Script Builder | `qa` | `claude-sonnet-5` |
| `e2e-runner` | E2E Test Runner | `qa` | `claude-haiku-4.5` |
| `unit-test` | Unit Test Builder | `qa` | `claude-sonnet-5` |

The model choice follows the repository's own rule: reasoning over a diff and judging
severity is the strongest tier; writing code is the middle tier; orchestrating a run that
already exists is the cheapest tier.

**`Task.role` is a closed vocabulary.** `dashboard/schema/04-execution.sql:29-30` declares
it with the regexp `planner|architect|backend|frontend|qa|security|devops`, and a test
asserts the Go constant set and the DDL agree in both directions. The change is:

1. the DDL regexp gains the four keys,
2. `dashboard/fixtures.go` gains the four `Role` entries,
3. a new test asserts the two agree.

The vendor-policy validator (`dashboard/vendors.go:126`) accepts a role once the map has
it; no change is needed there.

## 5. The four policies

A role is a seat; a **policy** is the standing rule the seat works under. GAOS renders
a product's Blueprint into its workspace's rules files
(`dashboard/blueprint.go:171-232`), and the rendered file is what the agent reads.

Four new policy kinds, each with its own rendered file:

| Kind | Rendered file | What it says |
| --- | --- | --- |
| `pr-review` | `.claude/rules/62-pr-review.md` | Which repo-local reviewer agent to use, the severity ladder, and where to post. |
| `e2e-automation` | `.claude/rules/63-e2e-automation.md` | Which repo-local agent writes the spec, in which checkout, and what "what to test" means. |
| `e2e-run` | `.claude/rules/64-e2e-run.md` | Which repo-local agent orchestrates the QA tool run. |
| `unit-test` | `.claude/rules/66-unit-test.md` | Which repo-local agent writes and runs the unit tests, and where the JUnit file lands. |

`policyFile()` (`dashboard/blueprint.go:152-165`) maps a kind to a path, and
`rulesFile` (`dashboard/blueprint.go:115`) is the regexp that refuses a path a workspace
may not hold. **Both change**: a new kind without a new `rulesFile` entry would be
rendered into a path the workspace refuses.

The policies are **seed policies**: GAOS ships them, and a product adopts them. They name
the repo-local agent, for example: "your reviewer agent is `.claude/agents/pr-reviewer.md`
in the product's main checkout; read it, then follow it". The repo-local agent files
themselves live in the product's repository, not in GAOS.

## 6. The routing

`dashboard/prflow.go` is new. It takes a `PREvent` (the struct the consumer client
hands over) and creates tasks.

### 6.1 The table

| Event | Card | Kind | Role | Labels | Depends on |
| --- | --- | --- | --- | --- | --- |
| `pr.opened` | one per pull request | `review` | `pr-reviewer` | `pr-review:auto` | — |
| `pr.opened` | one per pull request | `implement` | `e2e-builder` | `e2e:automation` | the review card |
| `pr.merged` | one per pull request | `implement` | `unit-test` | `unit:test` | — |
| staging deployed | one per product and deploy | `implement` | `e2e-runner` | `e2e:run` | — |

The review card is `kind: review`, so the design gate applies to it
(`dashboard/designgate.go:43-44`): a review that finds a problem sends the work back
rather than closing it.

### 6.2 The de-duplication key

A pull request event arrives twice when the consumer reconnects and replays. The key is

```
<repository>#<pull request number>:<role key>
```

for example `sample-service#4931:pr-reviewer`. The key is written **onto the card**, so a
restart rebuilds the set from the graph rather than from memory. The in-memory set is a
bounded map (5 000 entries, oldest evicted) that the graph rebuilds at boot.

A second event with a key that exists is a no-op, and says so.

### 6.3 Where the cards live

A card is a **task on a workflow**, exactly as the "Automate: …" card the QA catalog
creates (`dashboard/qacatalog_create.go:186-221`). The store grows one method,
`PlanPullRequestCards`, which:

1. creates the workflow and the tasks,
2. emits `TaskPlanned` for each,
3. emits `PullRequestOpened` or `PullRequestMerged` once for the pair,
4. returns the task ids.

The ArcadeDB store delegates it to its projection under the same lock as every other
mutation (`store_arcade.go:403-405` shows the pattern).

### 6.4 What "pr.merged" does when there is no "pr.opened"

A reconnect can replay a merge without the open, if the window fell between them. The
merged handler must work on its own: it creates the unit-test card and does not look for
the review card.

## 7. The GitHub client

`dashboard/github.go` is new. GAOS has no GitHub client today.

| Call | Purpose |
| --- | --- |
| `GET /repos/{owner}/{repo}/pulls/{number}` | the title, the body, the author, the draft flag. |
| `GET /repos/{owner}/{repo}/pulls/{number}/files` | the diff. |
| `POST /repos/{owner}/{repo}/pulls/{number}/reviews` | the review comment. |

**The credential is a file**, `GAOS_GITHUB_TOKEN_FILE`, mode 600, holding a token with
`pull_requests: write` on the product's repositories. The token is read once at boot and
kept in memory; it is never logged, never rendered and never written to an event, and
neither is the file's path.

The token's **kind** is refused, not just its length: a `bt_live_` token (Brain Town's
input credential), a `bt_c_` token (Brain Town's reader credential) and a `gaos_` token
are all refused, and so is any token equal to one of GAOS's own API tokens. A GitHub
credential must never be a Brain Town credential, a reader credential or a GAOS
credential. The same reasoning as `dashboard/braintown.go:121-124`, which refuses a GAOS
credential on the outbound feed.

A token file others can read is **refused**, copying `dashboard/api_v1.go:133` and
BR-52 (`pustaka/docs/product/brd.html:1346`).

**The repository comes from the product, never from the event.** The event carries the
owner login and the repository name; the product carries the repository list
(`Master.Repos`, `dashboard/model.go:388`), whose entries are `owner/repo` pairs. The
reader resolves the frame to a product by that pair
(`docs/designs/gaos-brain-town-consumer.md` §5.3), and the client is called with the pair
the **product** declares, so a forged event cannot make GAOS read a repository the product
does not own.

**The review body** is written by the agent, and the agent's own handoff block carries
it (the same contract every other agent in GAOS already follows,
`dashboard/adapter_aimanager.go:137-139`). GAOS posts it as `event: COMMENT` — a review
comment, never an approval. A `201` from GitHub means the comment was posted; a `422`
means the token may not write to that repository, and the card records the failure and
moves to **Failed**.

### 7.1 The untrusted input

The pull request's title, body and diff are **untrusted text**. They travel into the
agent as **context parts**, not as a prompt, and the pre-dispatch egress pass
(`dashboard/egress.go:470`, `Screen`) runs over the packet as it always does. The secret
detectors (`dashboard/egress.go:347`) run over the diff, so a token pasted into a comment
never reaches the model.

The **title** is the field that ends up in a task title, and a task title is republished
by the outbound Brain Town feed. It is therefore capped at 200 characters, stripped of
control characters and screened with the egress detectors **before** it reaches the card —
the same three steps the outbound feed applies to a title it publishes
(`dashboard/braintown.go:228-253`).

### 7.2 "What to test"

The brief makes the pull-request author's statement of what to test **mandatory**. The
author writes it in the pull request body, under the headings the repository's template
fixes (`## Apa saja yang sudah dites oleh developer?` and
`## Apa saja yang perlu dites oleh QA?`).

The e2e builder reads that section. **If it is empty or absent, the agent does not
guess**: it asks one question with three options through GAOS's own question mechanism
(`dashboard/questions.go`), and the card waits in **Needs you**
(`StatusAwaitingInput`, `dashboard/taskstate.go:46`). The same rule the QA catalog's own
questions already follow.

## 8. The three outside calls

### 8.1 The QA tool

The QA tool's automation and run routes are session-authenticated
(`qa-automation-tools/ai-backend/internal/api/README.md`); GAOS holds only the
`/api/v1/gaos/*` bearer family. The QA tool gains two bearer routes under the **existing**
`GAOS_INBOUND_TOKEN` guard:

| Route | Body | Answer |
| --- | --- | --- |
| `POST /api/v1/gaos/automation` | `{product, caseCode, mode:"create"\|"run"}` | `202 {job_id}` |
| `GET /api/v1/gaos/jobs/{id}` | — | `{status, phase, job_type, error?, report?}` |

They are a thin authentication and DTO shim over the QA tool's existing
`caseauto.Machine`, which already does generate → write → run → repair ≤3 → approve →
push → PR **in the automation repository**. No new automation logic.

GAOS calls them through the existing outbound client (`dashboard/qaclient.go`), which
already holds `GAOS_QA_URL` and `GAOS_QA_TOKEN_FILE` and already refuses redirects.

The **results** come back by the path that already exists: the QA tool pushes
`POST /api/v1/products/{id}/qa/results` to GAOS (`dashboard/api_v1.go:401`). No change.

### 8.2 The staging callback

Jenkins calls GAOS after the staging deploy, over a new machine route with **HTTP Basic**
auth:

```
POST /api/v1/products/{id}/staging
Authorization: Basic base64(user:pass)
{"repo":"acme-org/sample-service","branch":"staging","commit":"<sha>","environment":"staging"}
```

- **The credential** is a file, `GAOS_STAGING_BASIC_AUTH_FILE`, mode 600, holding
  `user:password`. GAOS compares it with `crypto/subtle` over a SHA-256 of the presented
  credential. A failure is `401` with `WWW-Authenticate: Basic realm="gaos"`. The
  credential is never logged and never rendered.
- **The scope**: the product id in the path is authoritative; the `repo` in the body must
  be one of that product's own repositories or the answer is `422`.
- **The effect**: creates the `e2e-runner` card, or unblocks the one that is waiting,
  and emits `StagingDeployed`.
- `202 {accepted:true, taskId}`; `409` when a staging card is already open for that
  product.

This route sits **outside** the bearer group (`dashboard/api_v1.go:397-406` puts the
machine API under the bearer gate) because the Basic credential **is** the machine
identity here. It is still inside the product-scope check.

### 8.3 WhatsApp

`dashboard/wa.go` is new. The gateway is GoWA v8 at
`http://devkedungdoro.aplikasi.web.id:7001`:

- `POST {API_URL}/send/message`, `Authorization: Basic base64(user:pass)`, body
  `{"phone":"<group jid>","message":"<text>"}`.
- The answer is `{"code":"SUCCESS","message":…,"results":{"message_id":…,"status":…}}`.
  **A `200` whose `code` is not `SUCCESS` is a failure**, not a success.
- The `phone` is a `@g.us` group JID, passed verbatim.
- `GET /user/my/groups` lists the device's groups, so GAOS resolves the group **by name**
  ("JACK SPARROW") and falls back to a configured JID.

**Settings**: `GAOS_WA_ENABLED`, `GAOS_WA_URL`, `GAOS_WA_BASIC_AUTH_FILE` (mode 600),
`GAOS_WA_GROUP_FILE` (mode 600), `GAOS_WA_GROUP_NAME` (default `JACK SPARROW`).

**The semantics**: a `200` with a `message_id` means **accepted by WhatsApp, not
delivered**. GAOS never claims "delivered".

**Never** a group id, a number or a message body in a log line. The log line is
`wa: notify <alias> sent|failed <class>`.

**Never** a retry storm into a chat group: one attempt plus a bounded retry for
transport errors only. A `400` and a `429` (WhatsApp's anti-spam lockout) are never
retried.

### 8.4 The contact registry

`dashboard/contacts.go` is new. It maps a GitHub account to a WhatsApp destination:

| Field | Meaning |
| --- | --- |
| `productId` | `p-6`, or empty for every product. |
| `githubLogin` | the GitHub account, for example `ardi`. |
| `displayName` | the person's name. |
| `channel` | `whatsapp`. |
| `destination` | the group JID or the number. |
| `notifyOn` | which findings reach this person: `pr-review:medium`, `e2e:failure`, `unit:failure`. |

**Where it lives: GAOS only.** Never in Brain Town, never in the consumer stream, never
in an event. A phone number is personal data, and the event bus feeds a public town.

The registry is **not** part of `Master` (`dashboard/model.go:382-411`), so it is never
rendered into the Blueprint and never reaches a workspace file. The back-office list
shows the destination masked (`…@g.us`); the Integrations view shows counts only.

**The resolution order**: a medium-or-worse finding goes to the pull request's author's
contact; a test failure goes to the last committer's contact; when no contact matches,
the message goes to the "JACK SPARROW" group.

## 9. The new event types

The event catalog is closed (`dashboard/events.go:30-32`), so each new type lands in the
catalog **and** in the fold (`dashboard/fold.go:111`) in the same change.

| Event type | Emitted when |
| --- | --- |
| `PullRequestOpened` | a `pr.opened` event arrived and its cards were planned. |
| `PullRequestMerged` | a `pr.merged` event arrived and its card was planned. |
| `StagingDeployed` | the staging callback landed and its card was planned. |
| `ContactNotified` | a WhatsApp message was sent, with the target's **alias**, never the number. |

`ContactNotified` carries the alias and the message kind, never the destination and
never the body.

## 10. The change list

| File | Change |
| --- | --- |
| `dashboard/labels.go` | **New.** The closed label vocabulary and its validation. |
| `dashboard/labels_test.go` | **New.** The vocabulary refuses an unknown label. |
| `dashboard/model.go` | `TaskRecord.Labels`; the contact record on `Master`. |
| `dashboard/schema/04-execution.sql` | `Task.labels`; the `Task.role` regexp gains the four keys. |
| `dashboard/schema_test.go` | The Go role set and the DDL regexp agree. |
| `dashboard/fixtures.go` | The four roles. |
| `dashboard/blueprint.go` | The four policy kinds; `policyFile()` and `rulesFile`. |
| `dashboard/blueprint_test.go` | The new kinds render to their own files. |
| `dashboard/events.go` | The four new event types. |
| `dashboard/fold.go` | The four new fold cases. |
| `dashboard/prflow.go` | **New.** The routing, the de-duplication key, the card creation. |
| `dashboard/prflow_test.go` | **New.** The routing table, the de-duplication, the merged-without-opened case. |
| `dashboard/github.go` | **New.** The GitHub client: read the pull request, read the diff, post the review. |
| `dashboard/github_test.go` | **New.** The client against a fake server. |
| `dashboard/wa.go` | **New.** The WhatsApp notifier. |
| `dashboard/wa_test.go` | **New.** The `code != "SUCCESS"` guard, the 429 that is not retried. |
| `dashboard/contacts.go` | **New.** The contact registry and its resolution. |
| `dashboard/contacts_test.go` **New.** The resolution order and the masking. |
| `dashboard/staging.go` | **New.** The Basic-auth staging callback. |
| `dashboard/staging_test.go` | **New.** 401, 422, 202. |
| `dashboard/store.go` | The store's `PlanPullRequestCards` and the contact methods. |
| `dashboard/store_mem.go` | The MemStore's implementation. |
| `dashboard/store_arcade.go` | The ArcadeDB delegation. |
| `dashboard/api_v1.go` | The staging callback route. |
| `dashboard/integrations.go` | The WhatsApp row and the contact count. |
| `dashboard/board.go` | The label chips on a card (`dashboard/board.go:267`). |
| `dashboard/view.go` | The board card's label field (`dashboard/view.go:291-312`). |
| `dashboard/templates/backoffice.html` | The chips' markup, beside the card (`dashboard/templates/backoffice.html:730-739`). |
| `dashboard/seed.go` | The four seed policies, beside the seeded policies (`dashboard/seed.go:394`). |
| `deploy/stacks/compose.prod.yaml` | The new settings and the GitHub token's secret mount, beside the existing `BRAIN_TOWN_TOKEN_FILE` mount (`compose.prod.yaml:149-158`). |
| `deploy/stacks/dashboard.env.example` | The same settings, as examples. |
| `deploy/check-stacks.sh` | The assertion that the GitHub token is a file and never an environment value, beside the existing one (`deploy/check-stacks.sh:142-156`). |
| `docs/designs/gaos-pr-agent-flows.md` | **New.** This document. |
| `docs/designs/reviews/gaos-pr-agent-flows-review-*.md` | **New.** The goldfish reports. |
| `pustaka/docs/integrations/pr-agents.html` | **New.** The published page. |
| `pustaka/docs/assets/toc/integrations.js` | The page's registration, beside the sibling entries (`pustaka/docs/assets/toc/integrations.js:54`). |
| `pustaka/docs/product/qa-test-plan.html` | One row per new test, in the same change (`.claude/rules/qa-test-plan.md:19-25`). |
| `pustaka/docs/product/brd.html`, `decision-log.html`, `open-questions.html` | The counts the register rule keeps in step (`.claude/rules/qa-test-plan.md:86-90`). |
| `pustaka/docs/changelog.html`, `pustaka/docs/assets/toc.js` | The release hygiene pair (`.claude/rules/qa-test-plan.md:97-100`). |

**Counts.** The table has **39 rows**: 12 new Go files (an implementation and its test,
seven pairs), 17 changed Go files, 3 new documents and 7 changed files that are
not Go. The arithmetic: 12 + 17 + 3 + 7 = 39.

## 11. Alternatives

### 11.1 One agent instead of four — rejected

A single "pull request agent" would read the diff, write the tests and write the spec.
Rejected because the four have different **models**, different **workspaces** and
different **failure owners**: the reviewer reasons (strongest tier), the builder writes
code (middle tier), the runner only orchestrates (cheapest tier), and the unit-test
agent writes and runs. One agent would force the cheapest of them on the strongest, or
the strongest on the cheapest, and the runner's work would be paid for twice.

### 11.2 GAOS posts the review comment instead of the agent — rejected

GAOS could hold the findings and post them. Rejected because the findings are the
agent's, in the agent's own words, and a machine re-writing them loses the reason and
the file reference. The agent posts its own review; GAOS only carries the card.

### 11.3 The WhatsApp message carries the findings — rejected

The message would be long and would leak code into a chat. The message carries one line
and the card id; the findings live in the card.

### 11.4 The staging callback pushes to GAOS — rejected

Jenkins could push the "staging is ready" event to GAOS over the bearer API. Rejected
because the bearer API has one identity per integration, and a CI job is not a
platform: it has no product scope, no token lifecycle and no back-office row. The
callback's Basic credential is scoped to **one** product's staging deploy, which is
exactly what it should be.

## 12. Failure and recovery

| Failure | What happens |
| --- | --- |
| Brain Town is down | No events, no cards. Nothing else is affected. The consumer reconnects. |
| The GitHub token is missing or may not write | The review card moves to **Failed** with the reason. The card is not silently closed. |
| The QA tool is down | The e2e cards stay open; the card's reason says so. The e2e runner is the smallest role and its failure is the least expensive. |
| WhatsApp is down | The card still records the finding and moves to **Failed** so a human sees it. The message is not retried into the group. |
| Jenkins never calls back | No staging card. The Integrations view shows the last staging callback per product, so the silence is visible. |
| GAOS restarts mid-flow | The consumer replays from the last sequence it handled; the de-duplication key makes the replay a no-op. |

## 13. Security

- Every credential is a **file**, mode 600, named by an environment variable. None is an
  environment value, a log line, a page or an event.
- The GitHub token's scope is one product's repositories; the repository pair comes from
  the product, never from the event.
- The WhatsApp credential never reaches a log line, and the group id and the message body
  never do either.
- The staging callback compares its credential in constant time and never logs it.
- The contact registry is the one place personal data lives. It is never written to an
  event, never pushed to Brain Town and never included in a task title.
- The pull request body and the diff are untrusted: they travel as context parts and the
  egress pass screens them.

## 14. Non-goals

- Any product other than the one this release is written for. The design must not hard-code a product.
- A GitHub review **approval** (`event: APPROVE`). Only `COMMENT`.
- An agent opening a pull request. The operator reviews the diff and opens it, as the
  product's own workspace policy already says.
- A fifth agent. Four is what the brief asks for.
- Turning the GAOS → Brain Town feed on. It stays as it is.

## 15. Done when

- [ ] A `pr.opened` event for a repository GAOS owns opens a review card and an e2e
      builder card, both labelled, and a second copy of the same event opens nothing.
- [ ] A `pr.merged` event opens a unit-test card, with no review card present.
- [ ] A card's labels render on the board and an unknown label is refused.
- [ ] The four roles are accepted by the store and the DDL agrees with the Go set.
- [ ] The four policies render to their own rules files.
- [ ] The review agent's findings reach the pull request as a review comment, and a
      medium-or-worse finding reaches the contact or the group.
- [ ] The staging callback answers 401 without the credential, 422 for a repository the
      product does not own, and 202 with a card.
- [ ] A WhatsApp answer whose `code` is not `SUCCESS` is treated as a failure, and a 429
      is not retried.
- [ ] The contact registry is never rendered into the Blueprint and never appears in an
      event.
- [ ] `cd dashboard && go test -race ./...`, `cd pustaka && go test ./... && go run . check ./docs`,
      `./deploy/check-stacks.sh` and `./deploy/check-workspace.sh` are green, and every new
      test case is registered in `pustaka/docs/product/qa-test-plan.html`.
