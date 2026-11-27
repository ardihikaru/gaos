# GAOS external tasks API

An outside platform can ask GAOS for work without signing in to the back office: a pull-request review, unit
tests for a merged pull request, an e2e automation, or an e2e run. GAOS plans the card on the product's
pull-request lane, starts the agent when the product allows it, and the platform polls the card until it is
done (D-099). The same text is published at `integrations/external-tasks-api.html`.

Placeholders: the host is `gaos.example.org`, the product `p-2`, the repository `acme/backend` and the client
`plat-a`. The token is written `$GAOS_TOKEN`.

## What it is for

| `kind` | What GAOS does | Worked by |
|---|---|---|
| `pr-review` | Reviews an open pull request and posts the review as a comment. | the PR reviewer agent |
| `unit-test` | Writes unit tests for a merged pull request. | the unit-test agent |
| `automation-create` | Asks the QA tool to write the e2e automation a pull request names. | the QA tool |
| `automation-run` | Asks the QA tool to run one automated case, or every one (`*`). | the QA tool |

A card for a pull request is the same card Brain Town's event would plan. If it already exists, the API answers
that card instead of planning a second one.

## Ask the operator for a client

A client is a name, a token and a list of products, set up by the GAOS operator. Ask for a client with the
`tasks` capability on your products. Every call sends the token as a bearer:

```bash
export GAOS=https://gaos.example.org/api/v1
export GAOS_TOKEN="$(cat ~/.config/plat-a/gaos.token)"
```

## Create a PR review

```bash
curl -sS -X POST "$GAOS/products/p-2/tasks" \
  -H "Authorization: Bearer $GAOS_TOKEN" -H "Content-Type: application/json" \
  -d '{"kind":"pr-review","externalId":"plat-4711","pr":"acme/backend#42","review":"auto","dispatch":true}'
```

The route is `POST /api/v1/products/{id}/tasks`. The body is read strictly (at most 16 KiB; unknown fields are
refused):

| Field | Rule |
|---|---|
| `kind` | `pr-review`, `unit-test`, `automation-create` or `automation-run`. |
| `externalId` | Required: your own id, 1 to 64 of letters, digits, `. _ -`. Sending it again returns the same card. |
| `pr` | `owner/repo#n`. Required for `pr-review`, `unit-test` and `automation-create`; refused for `automation-run`. The repository must be one of the product's. |
| `review` | `auto` (default) or `manual`; `pr-review` only. `manual` is never dispatched. |
| `caseCode` | Required for `automation-run`: `*` or a case code such as `TC-OUTREG-004`. |
| `dispatch` | Default `true`. It matters for `pr-review` and `unit-test` only. |

The answer is `201` for a new card, or `200` with `"created":false` for a card that already existed. Adopting an
existing card changes nothing on it.

```json
{"taskId":"wf-pr-p-2-acme-backend-42-pr-reviewer","created":true,"status":"PENDING","column":"backlog",
 "labels":["pr-review:auto","source:api"],"dispatch":"auto"}
```

## Poll it

```bash
curl -sS "$GAOS/products/p-2/tasks/wf-pr-p-2-acme-backend-42-pr-reviewer" -H "Authorization: Bearer $GAOS_TOKEN"
curl -sS "$GAOS/products/p-2/tasks?externalId=plat-4711" -H "Authorization: Bearer $GAOS_TOKEN"
```

The routes are `GET /api/v1/products/{id}/tasks/{taskId}` and `GET /api/v1/products/{id}/tasks?externalId=`.
Both answer the object above plus `updatedAt` and `reason`: `held`, `blocked`, `failed`, or empty. You see only
the cards you asked for; any other id is `404`.

## The other kinds

```bash
curl -sS -X POST "$GAOS/products/p-2/tasks" -H "Authorization: Bearer $GAOS_TOKEN" -H "Content-Type: application/json" \
  -d '{"kind":"unit-test","externalId":"plat-4712","pr":"acme/backend#42"}'
curl -sS -X POST "$GAOS/products/p-2/tasks" -H "Authorization: Bearer $GAOS_TOKEN" -H "Content-Type: application/json" \
  -d '{"kind":"automation-create","externalId":"plat-4713","pr":"acme/backend#42"}'
curl -sS -X POST "$GAOS/products/p-2/tasks" -H "Authorization: Bearer $GAOS_TOKEN" -H "Content-Type: application/json" \
  -d '{"kind":"automation-run","externalId":"plat-4714","caseCode":"TC-OUTREG-004"}'
curl -sS -X POST "$GAOS/products/p-2/tasks/wf-pr-p-2-qa-run-1/cancel" -H "Authorization: Bearer $GAOS_TOKEN"
```

A `pr-review` needs an open pull request, a `unit-test` a merged one. Drafts, forks and strangers' pull requests
are refused. Cancel, `POST /api/v1/products/{id}/tasks/{taskId}/cancel`, works only on an `automation-run` card
you created.

## What `dispatch` answers

| Card | Runner on | Runner manual, product listed for autorun | Runner manual, not listed |
|---|---|---|---|
| agent kind, `dispatch` true | `scheduler` | `auto` | `operator` |
| agent kind, `dispatch` false | `scheduler` | `operator` | `operator` |
| `review: manual` | `manual-review` | `manual-review` | `manual-review` |
| `automation-*` | `qa-tool` | `qa-tool` | `qa-tool` |

With the runner off, every request answers `409 runner-off`.

## Errors

Every error is `{"error":"<code>","reason":"<sentence>"}`.

| Status | Code | When |
|---|---|---|
| 401 | `unauthorized` | No bearer token, or an unknown one. |
| 403 | `forbidden` | No `tasks` capability, or the admin token on a create or a cancel. |
| 404 | `not-found` | A product outside your list, or a card you did not ask for. |
| 409 | `runner-off` | GAOS runs nothing at the moment. |
| 409 | `externalId-reused` | The same `externalId` with a different body. |
| 409 | `too-many-refs` | Eight clients already follow this card. |
| 409 | `skipped` | A draft, a fork, a stranger's pull request, a review on one not open, or unit tests on one not merged. |
| 409 | `cap` | You already have the most open cards allowed. |
| 409 | `daily-cap` | You created the most cards allowed today (UTC). |
| 409 | `run-in-flight` | The product already has an e2e run open. |
| 422 | `invalid-kind`, `invalid-field`, `repo-not-owned` | The body breaks a rule above. |
| 429 | `write-in-flight` | Your previous write has not finished; retry after `Retry-After`. |
| 503 | `api-disabled`, `github-off`, `qa-off` | The API, GitHub or the QA tool is not configured. |

## Limits

- One write at a time per client; a second answers `429`.
- At most `GAOS_API_MAX_OPEN` (default 5) unfinished cards per client.
- At most `GAOS_API_DAILY` (default 20) cards created per client per UTC day, cancelled ones included.
- At most one open `automation-run` per product.
- Under the manual runner, GAOS starts cards itself only on products listed in `GAOS_API_AUTORUN_PRODUCTS`, at
  most `GAOS_API_AUTORUN_DAILY` (default 20) a day.

## Troubleshooting

- **401 with a token the operator gave you:** the client line was skipped at boot; the operator reads the boot log.
- **403:** the client has no `tasks` capability.
- **The card stays in Ready:** `dispatch` said `operator`; ask the operator to list the product or move the card.
- **`reason: held`:** the review needs a person.
- **A replay answers 200:** that is the idempotency of `externalId`.
