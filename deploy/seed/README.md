# deploy/seed — seeding a product from a QA catalog

`POST /api/v1/seed/product` (admin token, `dashboard/seed.go`) builds a product
in one call: the product, its BRD, one user story per module, decisions,
diagrams, Blueprint policies and knowledge sources, a published Blueprint, and a
workflow with one *Automate* card per test epic behind the design gate. The test
catalog then goes in through `POST /api/v1/products/{id}/qa/catalog`. The seed
is generic and idempotent: a second run changes nothing, and it never clears the
design gate (D-074).

`seed.sh` does both calls and checks the result:

```bash
# against the local stack (docker compose up -d), with an admin token file
deploy/seed/seed.sh --dir deploy/seed/example --token-file /path/to/admin-token

# dry run: print the seed's totals, send nothing
deploy/seed/seed.sh --dir deploy/seed/example --dry-run

# a deployed instance: run on its VM, against its tunnel listener
deploy/seed/seed.sh --dir deploy/local/seed/<label> --base http://<tunnel-ip>:18301/api/v1
```

`--dir` is required. `--base` defaults to the local stack,
`http://127.0.0.1:8083/api/v1`; the script sends the bearer only over https, or
over http to a loopback or private address, and never follows a redirect.

## `example/` — an invented product

*Sample Shop* is a fictional web shop: 3 modules, 8 features, 4 epics (one with no
module, so it lands in the Plan lane), 12 cases (9 planned, 5 of them
implemented; one blocked, one manual-only, one won't-do). Nothing in it describes
a real system. `dashboard/seed_test.go` seeds it in every test run.

A real product's seed is private: keep it under the git-ignored
`deploy/local/seed/<label>/` (see `deploy/local/README.md`), never here.

## `product.json`

Every field of `ProductSeed` (`dashboard/seed.go`):

| Field | Type | Meaning |
|---|---|---|
| `source` | object | informational: what generated the file |
| `name`, `owner`, `repo`, `goal`, `priority` | string | the product record; `name` is how a re-run finds the product |
| `brd` | object | `problem` (string), `goals`, `scope`, `outOfScope`, `rules`, `metrics` (string lists) |
| `modules` | list | `{key, title, pri, features[]}` — `key` is `A01`…`D99`, `pri` is `must` or `should`; one user story each |
| `diagrams` | list | `{kind, title, src}` — `flow`, `sequence` or `erd` |
| `decisions` | list | `{id, title, confidence}` |
| `policies` | list | `{key, kind, title, body, order}` — the Blueprint policies |
| `sources` | list | `{url, ref, purpose, note}` — knowledge sources, `ref` a 40-hex commit |
| `epics` | list | `{code, title, service, source, module}` — `code` is 2-8 capitals; one card each; `module` may be empty |
| `plan` | object | `{goal, priority}` — the workflow's goal |

Text that reaches an agent's turn is cleaned (backticks, angle brackets and line
breaks go) and screened for secrets; a seed carrying a secret is refused whole.
A record below the readiness floor is refused with what is missing.

## `cases.json`

`{"source": {...}, "cases": [...]}`. One row per case, the fields of `QaCase`
(`dashboard/qacatalog.go`):

| Field | Rule |
|---|---|
| `caseId` | `TC-<EPIC>-NNN`, unique, never reused |
| `epic` | an epic `code` from `product.json` |
| `module`, `features` | a module key and its feature ids (`A01.02`), or empty |
| `userStory` | `US-<EPIC>-NN`, or empty |
| `scenario` | one line, at most 600 bytes |
| `polarity` | `+` or `-` |
| `level` | `API`, `GQL`, `WEB` or `E2E` |
| `pri` | `P0`…`P3` |
| `decision` | `planned`, `blocked`, `manual-only` or `wont` |
| `reason` | `category:detail` for anything but `planned` (`defect`, `not-needed`, `blocked`, `unsafe`, `manual-only`, `deferred`, `other`) |
| `implemented`, `spec` | the automation facts: a planned case with a script names its spec path |

## Building a seed from your own QA catalog

Map your catalog onto the two files: one module per area of the product map, one
epic per catalog file or test suite, one case row per catalogued case. Keep the
generator next to the private seed it writes, under `deploy/local/`. Check the
result with `seed.sh --dir <dir> --dry-run` before seeding.
