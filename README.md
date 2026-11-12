# GAOS

A graph-native agent operating system. The thesis is in one line: **the graph is
the primitive, not the prompt.** State lives in vertices and edges rather than a
conversation, every change emits an event, and the orchestrator holds nothing —
kill it mid-run and it rebuilds from the graph.

**Product version 0.36.1** (`pustaka/docs/assets/toc.js`). The graph, the event
store, readiness, the planner, the scheduler and the attempt record are built, and
GAOS has made real model calls. What judges a result — review, scoring, the retry
guards — is not built yet. Every page and every file says which is which.

**GAOS is product and task management.** *ai-claude-cli works the task with
context, ai-manager shapes it with a workspace, GAOS shapes it with the product, the
task details and the product's Blueprint.* Since v0.29.0 GAOS runs no model tool
itself: every attempt is one turn through ai-manager, in the product's own workspace,
under a versioned Blueprint, tracked on a kanban per product. Start with
`pustaka/docs/architecture/gateway.html` and `pustaka/docs/product/blueprint.html`;
the decisions are D-055…D-062. What is left of milestone M14 is the task fold that
lets the board replay, and vendoring the sisflow board once OQ-25 allows it.

```
artifacts/v1/   the design — three self-contained documents, the source of truth
dashboard/      the product master · Go + HTMX · stdlib only · ArcadeDB or in-memory store
pustaka/        the documentation site (vendored engine, GAOS content)
landing-page/   the marketing page (vendored, static)
deploy/         Dockerfiles, Traefik, the tunnelled stacks, the edge config
docs/           working documents — the design gate's rules, designs and articles
compose.yaml    the local stack — one port, no infrastructure
```

## Run it locally

```bash
docker compose up -d
open http://localhost:8083/
```

| path | what |
|---|---|
| `/` | landing page — owns the origin's 404 |
| `/app/` | the dashboard — the product list, each product's Blueprint, Board and Timeline |
| `/docs/` | the documentation |
| `/api/healthz` | health, including ArcadeDB reachability |

Port 8083 rather than 8080, because 8080 is already taken on the development
box. Override with `GAOS_EDGE_PORT`.

## Deployed

Product slot **03**. Both tiers run on the private VM behind the WireGuard
tunnel; `clino` is the public edge and runs no containers.

| | dev | prod |
|---|---|---|
| | `dev.gaos.mfardiansyah.id` | `gaos.mfardiansyah.id` |
| | `10.10.0.2:19301` | `10.10.0.2:18301` |

Read **`deploy/STACKS.md`** before touching either, and
**`deploy/edge/README.md`** before touching nginx on `clino` — three of the rules
there have already cost somebody an outage on that box.

```bash
./deploy/check-stacks.sh                                  # invariants a running stack hides
./deploy/rebuild-restart.sh dev  deploy                   # dev first, always
./deploy/rebuild-restart.sh prod deploy --tag v0.13.0     # prod builds from a tag, never the checkout
```

## What is designed, and what is built

| | state |
|---|---|
| System architecture, control loops, scoring, readiness rubric | **designed** — `artifacts/v1/gaos-architecture.html` |
| Graph schema: 25 vertex types, 32 edge types, event store | **built** — `dashboard/schema/`, applied to dev and prod, reconciled against ArcadeDB 26.7.2 |
| Product master UI | **built** — Go + HTMX; the product list is the home page, each product with its record, Blueprint, Board and Timeline. The engineering floor was retired in v0.29.0 |
| dev + prod stacks, edge config, health probe | **built** — verified end to end over the tunnel |
| Docs site and landing page | **built** |
| ArcadeDB schema applied, graph queries | **built** — `dashboard/store_arcade.go` against the `store.go` seam; one conformance suite covers both stores |
| Append-only event store, the fold, replay | **built** — `dashboard/fold.go`; the stream is unbounded, the reducer is pure/total/order-stable, replay folds from the nearest snapshot through a read-only handle |
| Task state machine | **built** — `dashboard/state.go`; illegal transitions are refused at the boundary |
| Scheduler, supervisor, boot recovery | **built** — `dashboard/scheduler.go`, `runner.go`, `turns.go`; one live turn per product (the graph lease), recovery by request id |
| Attempt record, frozen context slice, reply contract and handoff | **built** — `dashboard/model.go`, `handoff.go`, `handoff_reply.go`; the handoff travels in the reply (D-057) |
| ai-manager gateway adapter | **built** — `dashboard/adapter_aimanager.go`, `GAOS_MODEL_MODE=aimanager`; the upstream provisioning API is live |
| Blueprint per product | **built** — policies, knowledge sources, versions, the back-office tab, synced into the product's workspace |
| One workspace and one live turn per product | **built** — drift gate before dispatch, graph lease in the scheduler (M14.4) |
| Sessions per task, request-id recovery | **built** — `dashboard/turns.go`; one real turn passed through `live-aim-run.sh` |
| Kanban per product | **built** — the back office Board tab and `dashboard/board.go`; moves are requests the state machine may refuse |
| Reviews, scoring, retry guards | **not built** |
| Authentication | **not built** — the back-office login sets a cookie and checks nothing |

The event catalog in `dashboard/events.go` is the closed list from the model
document, so the vocabulary cannot drift while the rest is written. Twelve of its
names are declared and not yet emitted; they belong to the runner, the review
engine, the planner's task graph and the budget guard, and a test pins the list
to those modules so the gap cannot quietly grow or shrink.

## Tests

```bash
cd dashboard && go test ./...          # 251 tests, race-clean, no dependencies
cd pustaka   && go test ./... && go run . check ./docs
./deploy/check-stacks.sh
./deploy/check-workspace.sh
```

The two Go packages hold 273 tests between them — 251 in `dashboard/` and 22 in
`pustaka/`.

Every test case — implemented or still a forward specification — is registered in
`pustaka/docs/product/qa-test-plan.html` (390 cases, 45 of them retired and struck). There is no CI: the product
owner runs them (D-025), and a test that is not on that page is one nobody knows
to run.

`check-workspace.sh` is the agent workspace's own invariants: that the paths the
project rules name exist, that no skill is an orphan, that every count agrees
with its register, that no rule is forked between two files, and that no
isolation canary or secret was left behind. It is advisory — the product owner
runs it — and it is the gate that stops the drift from coming back.

The dashboard tests deliberately target what fails *silently*: a computed style
losing its `template.CSS` type (`html/template` emits the literal `ZgotmplZ` and
the page still renders), a mount-point prefix that stops arriving, a snapshot
that aliases the live projection, and a back-office route that quietly never had an
auth check.

## Licence

**Apache-2.0** — `LICENSE`. Clone it, run it, change it, ship it. There is no
licence key, no trial, and no feature that switches off; this repository is a
complete system on its own, and no paid component may ever be required to run it.

The name is not covered by the licence: build on GAOS and give your fork its own
name (`NOTICE`). Contributions take a DCO sign-off rather than a CLA —
`CONTRIBUTING.md`.

What *is* commercial — optional private submodules, installation, support — and
what will never be sold, is in the docs under
`pustaka/docs/product/faq.html`. You always pay your own model provider; GAOS is
never in that billing path.
