# GAOS

**A graph-native agent operating system: product and task management for AI
coding agents.** GAOS holds each product — its requirements, user stories,
decisions, diagrams and a versioned *Blueprint* of policies — as a graph, plans
the work as tasks on a kanban, and sends each task to an AI agent one turn at a
time, with the product's context, through a gateway. The thesis is in one line:
**the graph is the primitive, not the prompt.** State lives in vertices, edges and
an append-only event stream, not in a conversation; kill the orchestrator
mid-run and it rebuilds from the graph.

**Product version 0.52.5** (`pustaka/docs/assets/toc.js`). Apache-2.0.

Three layers, each owning one thing: **GAOS** shapes the work with the product,
the task and the Blueprint; **ai-manager** runs every model turn in the
product's own provisioned workspace; a **worker** such as ai-claude-cli works
the task there. GAOS runs no model tool itself (D-055).

## Features

Each links to the page that describes it, in the docs site under `pustaka/docs/`.

- **Products and readiness** — a record per product (BRD, stories, decisions,
  diagrams, knowledge) scored for readiness; a record below the floor cannot be
  planned. `product/brd.html`
- **The Blueprint** — versioned policies per product, published and synced into
  the product's workspace as the agent's rules; drift is refused before a
  dispatch. `product/blueprint.html`
- **Kanban and the manual runner** — one board per product; a move is a request
  the task state machine may refuse. `GAOS_RUNNER=manual` makes the operator's
  board move the only thing that spends. `architecture/gateway.html`
- **The design gate** — a planned story gets a design task, a zero-context
  review and only then an implementation task; a design the review blocks goes
  back to its author and is reviewed again. `architecture/design-gate.html`
- **Card timing and conversations** — how long each card waited, worked and was
  blocked; every card's AI conversation, with archive and prune.
- **The operator's inbox** — an agent's question, or an external client's, waits
  in *Needs you*; every answer records where it came from.
- **QA sync** — a test catalog per product, a bearer-token REST API for the QA
  tools, two sync buttons, an answer webhook, and a registry of further machine
  clients. `guide/qa-sync-api.html`
- **Back-office sign-in** — real accounts, signed sessions, every route
  session-gated unless listed public. `guide/bo-sign-in.html`
- **An Integrations view** — every connection's live state, read-only, never a
  secret.

Not built yet, and said so everywhere: the scoring engine, the retry guards and
the budget guard. The design gate's review is built (D-068, D-084).

## Architecture

```
QA tools / clients ──bearer──▶  GAOS machine API (/api/v1)
                                   │
                                   ▼
                   GAOS — graph + event stream (ArcadeDB or in-memory),
                   board, scheduler, back office, inbox
                                   │  Basic (/v1) · admin bearer (/admin)
                                   ▼
                              ai-manager  ──▶  worker in the product's workspace
```

```
dashboard/      GAOS itself · Go stdlib + HTMX · ArcadeDB or in-memory store
pustaka/        the documentation site (vendored engine, GAOS content)
landing-page/   the marketing page (static)
deploy/         Dockerfiles, Traefik, stacks, edge config, checks, seed tooling
docs/           working documents — the design gate's rules, designs, reviews
artifacts/v1/   the original design documents
compose.yaml    the local stack — one port, no infrastructure
```

## Quick start

```bash
docker compose up -d
open http://localhost:8083/
```

| path | what |
|---|---|
| `/` | landing page — owns the origin's 404 |
| `/app/` | the dashboard — products, Blueprint, Board, Timeline, back office |
| `/docs/` | the documentation |
| `/api/healthz` | health, including ArcadeDB reachability |

Port 8083 by default; override with `GAOS_EDGE_PORT`. With no database the
dashboard runs on its in-memory store and reports `"database":"unconfigured"`.
To load an invented sample product, see `deploy/seed/README.md`.

## Configuration

The common variables. Every credential is a **file** (mode 600) named by a
variable, never a value in the environment. The full list, with the production
values' shape, is `pustaka/docs/infra/environments.html`.

| Variable | Meaning |
|---|---|
| `GAOS_ADDR` | listen address (`:8080` in the stacks) |
| `GAOS_STORE` | `arcadedb` to use ArcadeDB; otherwise in-memory |
| `GAOS_ARCADE_URL`, `GAOS_ARCADE_DB`, `GAOS_ARCADE_USER`, `GAOS_ARCADE_PASSWORD`, `GAOS_ARCADE_TENANT` | the ArcadeDB connection |
| `GAOS_MODEL_MODE` | `aimanager` to enable turns; `off` by default |
| `GAOS_AIM_URL`, `GAOS_AIM_CHAT_AUTH_FILE`, `GAOS_AIM_ADMIN_KEY_FILE` | ai-manager's base, its chat login, its admin key |
| `GAOS_AIM_TENANT`, `GAOS_AIM_POLL_MS`, `GAOS_AIM_TURN_DEADLINE`, `GAOS_AIM_SESSION_MAX_AGE`, `GAOS_MODEL` | turn tuning |
| `GAOS_RUNNER` | `off` (default), `manual` (only a board move spends) or `on` (the scheduler dispatches) |
| `GAOS_API_ADMIN_TOKEN_FILE`, `GAOS_API_QA_TOKEN_FILE` | the machine API's admin and QA tokens |
| `GAOS_API_QA_PRODUCTS` | product ids the QA token may reach |
| `GAOS_API_CLIENTS_FILE` | further machine clients, one per line |
| `GAOS_QA_URL`, `GAOS_QA_TOKEN_FILE`, `GAOS_QA_ORIGIN` | where GAOS calls the QA tools, its token, their host name |
| `GAOS_BO_USERS_FILE`, `GAOS_BO_SESSION_KEY_FILE`, `GAOS_BO_LABEL`, `GAOS_COOKIE_SECURE` | back-office sign-in |
| `BRAIN_TOWN_ENABLED`, `BRAIN_TOWN_URL`, `BRAIN_TOWN_TOKEN_FILE`, `BRAIN_TOWN_PUBLIC_PRODUCTS` | the optional Brain Town feed: off by default; the receiver's ingest URL, a file holding its bearer token, and the product ids sent as public (all others are sent hidden and redacted) |
| `GAOS_EGRESS_*` | the pre-dispatch egress screen: size caps, path allow/deny lists, redaction |

## Integrations

- **How integration works** — every connection, who starts it, how it is
  authenticated, and a question's and a test result's round trip:
  `pustaka/docs/integrations/overview.html`
- **Webhooks and external clients** — register a client, the inbox API, the
  answer webhook, troubleshooting, and tutorials for the QA tools, ai-manager
  and a new client: `pustaka/docs/integrations/webhooks-and-clients.html`
- **Brain Town feed** — the optional push of task and product changes to a
  public receiver, its three settings and what a hidden product leaks:
  `pustaka/docs/integrations/brain-town.html`
- **ai-manager REST reference** — all eight calls GAOS makes:
  `pustaka/docs/integrations/ai-manager-api.html`

## Tests

```bash
cd dashboard && go test -race ./...    # set GAOS_ARCADE_TEST_URL (deploy/schema/scratch.sh up) for the ArcadeDB half
cd pustaka   && go test ./... && go run . check ./docs
./deploy/check-stacks.sh
./deploy/check-workspace.sh
```

561 Go tests: 539 in `dashboard/` and 22 in `pustaka/`. Every test case —
implemented, retired or a forward specification — is in the register,
`pustaka/docs/product/qa-test-plan.html`: 695 registered test cases. There is
no CI: the product owner runs the checks (D-025), and a test that is not on that
page is one nobody knows to run. `check-workspace.sh` keeps the agent
workspace honest — paths, orphan skills, every count against its register, rule
forks, leftover canaries, secrets in reports — and fails if a tracked file names
an operator's private instance (D-085).

## Deployment

Production builds from a tag on a private VM behind a WireGuard tunnel; a small
public edge runs nginx only. Read `deploy/STACKS.md` before touching a stack and
`deploy/edge/README.md` before touching the edge. One operator's own material — a
second instance's overrides, a private product seed — lives in the git-ignored
`deploy/local/` (`deploy/local/README.md`), never in the repository.

## Contributing

Feature-sized changes start as a design in `docs/designs/` reviewed by
zero-context reviewers before any code (`docs/rules/gaos-design-review.md`).
Contributions take a DCO sign-off rather than a CLA — `CONTRIBUTING.md`.

## Licence

**Apache-2.0** — `LICENSE`. Clone it, run it, change it, ship it. There is no
licence key, no trial, and no feature that switches off; this repository is a
complete system on its own, and no paid component may ever be required to run it.

The name is not covered by the licence: build on GAOS and give your fork its own
name (`NOTICE`). What *is* commercial — optional private submodules,
installation, support — and what will never be sold is in
`pustaka/docs/product/faq.html`. You always pay your own model provider; GAOS is
never in that billing path.
