# GAOS

A graph-native agent operating system. The thesis is in one line: **the graph is
the primitive, not the prompt.** State lives in vertices and edges rather than a
conversation, every change emits an event, and the orchestrator holds nothing —
kill it mid-run and it rebuilds from the graph.

**Product version 0.26.0** (`pustaka/docs/assets/toc.js`). The graph, the event
store, readiness, the planner, the scheduler and the attempt record are built, and
GAOS has made real model calls. What judges a result — review, scoring, the retry
guards — is not built yet. Every page and every file says which is which.

**The direction changed on 2026-11-02, and the code has not caught up.** GAOS is
becoming product and task management: *ai-claude-cli works the task with context,
ai-manager shapes it with a workspace, GAOS shapes it with the product, the task
details and the product's Blueprint.* The dashboard still spawns `claude` itself
until milestone M14 replaces that with an ai-manager client, a versioned Blueprint
per product and a kanban per product. Start with `pustaka/docs/architecture/gateway.html`
and `pustaka/docs/product/blueprint.html`; the decisions are D-055…D-062.

```
artifacts/v1/   the design — three self-contained documents, the source of truth
dashboard/      Mission Control · Go + HTMX · stdlib only · ArcadeDB or in-memory store
pustaka/        the documentation site (vendored engine, GAOS content)
landing-page/   the marketing page (vendored, static)
deploy/         Dockerfiles, Traefik, the two tunnelled stacks, the edge config
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
| `/app/` | Mission Control |
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
| Mission Control UI | **built** — ported from the prototype to Go + HTMX |
| dev + prod stacks, edge config, health probe | **built** — verified end to end over the tunnel |
| Docs site and landing page | **built** |
| ArcadeDB schema applied, graph queries | **built** — `dashboard/store_arcade.go` against the `store.go` seam; one conformance suite covers both stores |
| Append-only event store, the fold, replay | **built** — `dashboard/fold.go`; the stream is unbounded, the reducer is pure/total/order-stable, replay folds from the nearest snapshot through a read-only handle |
| Task state machine | **built** — `dashboard/state.go`; illegal transitions are refused at the boundary |
| Scheduler, runner, lease, boot reconciliation | **built** — `dashboard/scheduler.go`, `runner.go`, `lease.go`, `reconcile.go`; to be replaced by the ai-manager gateway (M14) |
| Attempt record, frozen context slice, packet and handoff | **built** — `dashboard/taskrun.go`, `packet.go`, `handoff.go`; a real Claude Code run completed on 2026-08-23 |
| ai-manager gateway adapter | **built** — `dashboard/adapter_aimanager.go`, `GAOS_MODEL_MODE=aimanager`; the upstream provisioning API is live |
| Blueprint per product | **built** — policies, knowledge sources, versions, the back-office tab, synced into the product's workspace |
| One workspace and one live turn per product | **built** — drift gate before dispatch, graph lease in the scheduler (M14.4) |
| Sessions per task, request-id recovery, kanban per product | **designed** — M14.5–M14.6 |
| Reviews, scoring, retry guards | **not built** — `dashboard/sim.go` still animates the floor |
| Authentication | **not built** — the back-office login sets a cookie and checks nothing |

The event catalog in `dashboard/events.go` is the closed list from the model
document, so the vocabulary cannot drift while the rest is written. Twelve of its
names are declared and not yet emitted; they belong to the runner, the review
engine, the planner's task graph and the budget guard, and a test pins the list
to those modules so the gap cannot quietly grow or shrink.

## Tests

```bash
cd dashboard && go test ./...          # 201 tests, race-clean, no dependencies
cd pustaka   && go test ./... && go run . check ./docs
./deploy/check-stacks.sh
```

Every test case — implemented or still a forward specification — is registered in
`pustaka/docs/product/qa-test-plan.html` (312 cases). There is no CI: the product
owner runs them (D-025), and a test that is not on that page is one nobody knows
to run.

The dashboard tests deliberately target what fails *silently*: a computed style
losing its `template.CSS` type (`html/template` emits the literal `ZgotmplZ` and
the page still renders), a mount-point prefix that stops arriving, a snapshot
that aliases the live floor, and a back-office route that quietly never had an
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
