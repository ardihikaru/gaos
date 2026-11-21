# Open-source docs: a scrub of private instance material, the integration docs, and an Integrations view in the back office

**Status:** Approved (v3). Round 1 reviewed v1 with five lenses (`reviews/open-source-docs-*-1.md`, triage `reviews/open-source-docs-review-1.md`); round 2 reviewed v2 with the critic, readiness and security lenses (`*-2.md`, triage `open-source-docs-review-2.md`). Every `PROPOSE FIX` of both rounds is applied below. Approval in §11.
**Feature:** `open-source-docs`
**Version target:** v0.45.0 (main is v0.44.0, `pustaka/docs/assets/toc.js:22`).
**Numbers claimed:** **D-085**, **OQ-36**, **TC-510…TC-512**, **v0.45.0**. No BR is added or amended.
**BRD rules this must obey:** BR-04 (append-only stream — nothing here writes an event), BR-05 (closed event catalog — no event type is added), BR-52 (the machine API's bearer tokens: digests only, never rendered or logged), BR-55 (a verified back-office session for every back-office route), BR-57 (every answer records its source — the docs describe it). `pustaka/docs/product/brd.html` §9.
**Security lens:** required — a back-office view that reads the credential configuration (tokens, the gateway's chat login and admin key) and must render none of it; a validator that reads a git-ignored list of private strings; a change to the reviewer deny-list.

## 0. Glossary

- **The sample product** — one product a real deployment of GAOS carries: a customer's test catalog seeded through `POST /v1/seed/product` (`dashboard/seed.go:14-18`). Its name, its organisation and the domain of the instance that hosts it are **private strings**. This document never writes them; it calls them *the sample product's name*, *the organisation* and *the instance domain*.
- **Private strings** — the case-insensitive extended regular expressions a tracked file's content or path must not match. Today: the sample product's name in its three spellings, and the organisation's name. Four patterns.
- **Tracked file** — a path `git ls-files` lists.
- **`deploy/local/`** — the new git-ignored directory for one operator's instance material (§3.2). Only its `README.md` is tracked.
- **The QA tools** — the external service that runs the test scripts (`qa-automation-tools`); GAOS calls it (`dashboard/qaclient.go:20-35`) and it calls GAOS (`dashboard/api_v1.go:18-31`).
- **ai-manager** — the external model gateway every GAOS turn goes through (`dashboard/adapter_aimanager.go`, `GAOS_MODEL_MODE=aimanager`, `dashboard/modelconfig.go:81`).
- **ai-claude-cli** — the worker behind ai-manager that runs a turn with the Claude CLI inside the product's provisioned workspace. GAOS never calls it; it is named so the docs' diagram can show where a turn runs (an assumption about the far side, documented by ai-manager, not by this repository).
- **Registry client** — a machine client named in `GAOS_API_CLIENTS_FILE`, one line `name kind origin token-file products` (`dashboard/answer_source.go:183-187`), admitted by `addClients` (`dashboard/api_v1.go:198-234`), reaching only the inbox routes (`scopeClient`, `api_v1.go:34-43`, routes `api_v1.go:402-404`).
- **The answer webhook** — `POST {GAOS_QA_URL}/api/v1/gaos/inbox-answer` with the outbound token, sent by one worker with 5 attempts and a 30 s / 2 min / 10 min / 1 h backoff (`dashboard/inbox_worker.go:12-35`).
- **Back office (BO)** — the signed-in console. Every route is session-gated unless listed public or bearer (`dashboard/auth.go:527-560`). Two routes render a top-level view: `GET /ui/bo/{view}` (`dashboard/main.go:360`, `handleBOView` at `dashboard/handlers_bo.go:141-152`) and `GET /ui/bo?view=` (`main.go:359`, `handleBOSheet` at `handlers_bo.go:129-139`, listed public at `auth.go:509` and rendering the login sheet when signed out). Both call `buildBO` (`handlers_bo.go:189-337`), which switches on the view name.
- **ai-manager's own docs** — the REST documentation in ai-manager's repository (a sibling project, not vendored here), read read-only for the cross-check in §3.5. Claims about ai-manager's behaviour that come only from there are labelled *per ai-manager's docs*.

## 1. Problem

The product owner's brief, restated: GAOS is open source (Apache-2.0, `LICENSE`); make its repository and its description fit that, document every external integration, and add an *Integrations* view to the back office. The GitHub repository is **private today** (`gh repo view --json visibility` → `PRIVATE`, checked 2026-11-21). Four defects.

### 1.1 Tracked files carry a private instance

`git grep -ciE` with the four patterns on `c407f11` finds **179 matching lines in 39 tracked files**, and **seven tracked paths** whose names contain the product's name (the five files under `deploy/seed/<name>/`, `docs/designs/<name>-qa-sync.md` and `docs/designs/reviews/<name>-qa-sync-review-1.md`). By area (matching lines / files):

| Area | Matches | Files | What |
|---|---|---|---|
| `deploy/seed/<name>/` | 37 | 5 | the seed: `product.json`, `cases.json` (the customer's real test catalog), `defaults.json`, `build-seed.mjs` (parses the customer's catalog repository), `seed.sh` |
| `deploy/` other | 11 | 3 | the edge config's second-instance blocks (`deploy/edge/gaos.conf:31,52-53,137-165`), the redeploy override comment (`deploy/rebuild-restart.sh:95-106`), one compose comment (`deploy/stacks/compose.prod.yaml:138`) |
| `dashboard/` | 13 | 9 | code comments (`seed.go:18`, `qacatalog.go:12`, `store_arcade.go:1459`, `schema/16-qa-catalog.sql:1`), tests (`seed_test.go:12,14,45,267`, `answer_source_test.go:370`, `workflow_goal_test.go:11`, `store_arcade_writes_test.go:14`), `testdata/review-reply-long-note.txt:22,28` |
| `docs/designs/` | 59 | 12 | designs and review reports |
| `pustaka/docs/` | 58 | 9 | the QA sync API page (12, every curl names the instance domain), the QA collaboration page (11, a table of the instance's real URLs), the decision log (11), the QA plan (9), the implementation plan (8), the changelog (3), environments (2), open questions (1), the ToC (1) |
| `artifacts/v1/` | 1 | 1 | a path to the renamed design |

Sum: 37 + 11 + 13 + 59 + 58 + 1 = 179, the figure `git grep -c` reports. The 39 files are listed by path in §7.

### 1.2 The README is stale

`README.md` says "Product version 0.40.1" (`README.md:8`), "Authentication — **not built**" (`README.md:88-89`; sign-in shipped in v0.39.0, D-079), names 308 / 330 tests and 454 cases (`README.md:100-110`; today 384 Go tests, 509 cases), and describes a dev tier retired since 2026-11-03. `deploy/check-workspace.sh:133-144` pins those numbers as fixed strings and never compares them with reality, which is how they went stale while the check stayed green. The GitHub repository has an empty description and no topics.

### 1.3 No integration documentation

The QA tools' contract is one reference page (`pustaka/docs/guide/qa-sync-api.html`); ai-manager's contract is spread across `adapter_aimanager.go`, `aimanager.go`, `aimanager_admin.go`, `aimanager_questions.go`, `workspace.go`, `conversation.go` and `vendors.go`, with no page listing the eight calls GAOS makes (`aimanager.go:170,182,246`, `aimanager_questions.go:71`, `aimanager_admin.go:86,105,125,146`). No page shows how the pieces fit, and there is no tutorial for a new client.

### 1.4 The back office does not show integration state

Whether the QA outbound is configured is visible only as a disabled button on a product's QA tab (`qaclient.go:28-29`); the API's state is a boot log line (`api_v1.go:255-262`); a webhook delivery is a timeline line per product. An operator cannot see in one place what is connected and whether it works.

## 2. Goals and non-goals

**Goals**

- G1. **Zero private strings in tracked files**, content and paths, with meaning and every register ID kept (§3.1); proved by the guard with the list present in the implementing checkout (§3.3, §12 phase 1).
- G2. **Private material moves to `deploy/local/`** (git-ignored); a documented, invented example replaces the seed; a tracked placeholder template replaces the second instance's edge blocks (§3.2).
- G3. **A guard** in `deploy/check-workspace.sh` that fails on a private string in a tracked file, reading the patterns from a git-ignored file so the guard does not itself carry them (§3.3, TC-510).
- G4. **README and repository metadata** rewritten for an open-source reader; every count and the version compared with the registers by `check-workspace.sh` (§3.4).
- G5. **Three docs pages** under a new *Integrations* group (§3.5).
- G6. **A back-office *Integrations* view**, read-only, rendering no secret, on both view routes, with tests that prove it (§3.6, TC-511, TC-512).
- G7. **Registers**: D-085, OQ-36, TC-510…TC-512 with every count, TC-406 / TC-407 / TC-411 amended, the changelog, `product.semver` 0.45.0.

**Non-goals**

- N1. **No history rewrite in this change.** Git history keeps the private strings. Whether to rewrite it is **OQ-36**, opened here for the product owner, with §9 as its options. D-085 records the condition: *the repository is not made public until OQ-36 is answered.*
- N2. **No deploy, and no edit to any VM or to the edge.** The operator puts the private files in place (§3.2.4).
- N3. **No new route, event type, task status, store method or schema statement.** The view rides the two existing view routes.
- N4. **The webhook stays the QA tools'.** A registry client is told nothing by push; it polls `GET …/inbox/{externalId}` (`api_v1.go:403`). The guide documents this as it is; a per-client webhook is future work (§6 A4).
- N5. **No change to what ai-manager is sent.** The reference documents the calls; the gaps it finds (§3.5, page 3) are listed as open items, not fixed.
- N6. **Infrastructure names that are the product owner's own** (`gaos.mfardiansyah.id`, `clino`, the `rnd-*` VM names, the `10.10.0.x` tunnel addresses) stay: they are not private strings under the brief, and `deploy/STACKS.md` and `deploy/edge/README.md` depend on them.
- N7. **One release, not two.** The brief asks for the scrub, the docs and the view together, and the view's tutorials link to the docs pages; splitting adds a release and a register round for no reduction in risk (round 1, zoom-out M1, DECLINE).

## 3. Design

### 3.1 The scrub

Every match is reworded in place. The replacement vocabulary is fixed:

| Private | Replacement |
|---|---|
| the sample product's name (prose) | "the sample product" (first mention on a page: "a sample product") |
| the sample product's name in a test name or a message | `example` / `sample` (renames below) |
| the sample product's old seed directory, in a historical record (changelog, decision log, QA plan, implementation plan) | "the sample product's seed directory (moved to `deploy/local/`, D-085)" — never `deploy/seed/example/`, which holds different, invented content |
| the organisation as an owner, a company or a copyright holder | "the product owner's organisation" |
| the customer's GitHub org/repo | `example-org/qa-catalog` (prose); `automation-test-sample` (inside the testdata reply) |
| the instance domain for GAOS | `gaos.example.org` |
| the instance domain for the QA tools | `qa.example.org` |
| the instance's ArcadeDB database name | `gaos_instance` |

**OQ-25** (`pustaka/docs/product/open-questions.html:675`) names the organisation as the author of the vendored sisflow board. It becomes "It is code from the product owner's organisation." `dashboard/static/vendor/sisflow/NOTICE-sisflow.txt:2` already names only "the sisflow project owner", and D-063 (`decision-log.html:1094-1105`) names no grantor. So after the scrub no tracked file names the copyright holder of the vendored board. D-085 records this as accepted knowingly, on the brief's instruction; the grantor's identity stays in the product owner's own records (round 1, critic M "OQ-25", ESCALATE to the owner as an open item in the report).

**Renames** (`git mv`, references updated in the same change):

- `docs/designs/<name>-qa-sync.md` → `docs/designs/qa-catalog-sync.md`; `docs/designs/reviews/<name>-qa-sync-review-1.md` → `docs/designs/reviews/qa-catalog-sync-review-1.md`. Every reference to the old path (decision log ×5, `dashboard/qacatalog.go:12`, `dashboard/schema/16-qa-catalog.sql:1`, `artifacts/v1/gaos-arcadedb-model.html:1467`, the implementation plan, the QA collaboration page, `docs/designs/runner-manual.md:38`, three review reports) points at the new one.
- `TestThe<Name>SeedIsWellFormed` (`dashboard/seed_test.go:267`) → `TestTheExampleSeedIsWellFormed` (§3.2.2). TC-407 keeps its ID.
- `dashboard/testdata/review-reply-long-note.txt` is a real model reply: its two mentions of the catalog repository become `automation-test-sample`. The test that reads it (`TestARealReviewWithALongNoteSaysWhyItWasRefused`, `dashboard/review_note_test.go:68-80`) asserts only that the note is over `maxFindingNote` and the refusal's wording; the replacement is one character longer, so the note stays over the limit.

**Registers and review reports.** Rewording a private string in a decision, a question, a register row, a changelog line or a review report is allowed for this purpose only: versions, IDs, dates, counts and facts stay; no row or finding is deleted. `docs/designs/README.md:36-37` and `docs/rules/gaos-design-review.md` ("Save the reports … Append-only") make review reports append-only; D-085 records this one exception and its limit (private strings only), and names the redaction of this design's own round-1 reports (sizes of the customer's data) as its first use. The same one sentence goes into **`docs/rules/gaos-design-review.md:71`**, the policy's source of truth, and into `docs/designs/README.md:36` (round 2, critic M, readiness N). The limit is checked mechanically in phase 3 (§12): every line removed from a review report that existed at `c407f11` matched a pattern (round 2, SEC-11).

**Identifying detail the patterns cannot catch.** This document, its reviews and the new pages state no size, count or sector of the customer's data. The count literals in TC-407 (`seed_test.go:289,355`) and its register prose (`qa-test-plan.html:3157-3168`) are the customer's figures and are removed with the generalised test (§3.2.2).

### 3.2 Private material moves to `deploy/local/`

#### 3.2.1 The ignored directory

`.gitignore` gains:

```
# One operator's instance material: private seeds, a second instance's edge
# blocks and redeploy overrides, the forbidden-strings list. Never committed;
# the README beside it is the one tracked file and says what goes where.
/deploy/local/*
!/deploy/local/README.md
```

`deploy/local/README.md` (tracked) documents the layout and names no private string:

```
deploy/local/
  README.md                 tracked: this file
  forbidden-strings         one extended regex per line, case-insensitive; # comments (§3.3)
  seed/<label>/             a private product seed: product.json, cases.json (+ its own generator)
  edge/gaos-<label>.conf    a second instance's nginx blocks, made from deploy/edge/instance.conf.example
  instances/<label>.env     KEY=value overrides for a second instance's redeploy line
  private/                  optional: a clone of a private repository holding the above
```

`<label>` is a neutral label (below). The README also offers `deploy/local/private/` as a clone of a private repository, which gives the material history and a backup (round 1, zoom-out N "A6").

**No agent session may read it.** `.claude/rules/agent-workspace.md`'s deny-list (`:101-109`) gains *"`deploy/local/**` — in this checkout and in any other checkout of the repository, the main one included"*, and `.claude/settings.json` gains `"Read(//**/deploy/local/**)"` — an **absolute** glob (the `//` prefix), so it matches the main checkout's `deploy/local/` from inside an agent worktree, which a pattern anchored at the session's directory would not (round 2, SEC-1 High, critic M). The rule and the setting cover the same set, the tracked README included (it is documentation, readable in the repository's history and on GitHub). Read deny rules also bind Grep and Glob, and they bind every session, the implementer's included. Verification, recorded in the triage: from an agent worktree, a Read of the main checkout's list is refused.

**Neutral names.** Every directory and file under `deploy/local/` is named with a neutral label the operator chooses (`instance-2`, `private`), never the private name: a directory listing, `git status --ignored`, a `FAIL … in <dir>` line or the edge host's `conf.d` listing then carries nothing (round 2, SEC-2). The hand-off uses `deploy/local/seed/instance-2/`, `deploy/local/edge/gaos-instance-2.conf` and `deploy/local/instances/instance-2.env`.

**A private repository.** `deploy/local/` cannot be a clone target (the tracked README makes it non-empty); clone a private repository into `deploy/local/private/` instead, which the rule already ignores (round 2, critic N).

#### 3.2.2 The seed

**The move, exactly**, in this order:

1. the `.gitignore` lines (§3.2.1) are already in place from phase 1 (§12), so nothing moved below can be staged by a `git add -A`;
2. `mkdir -p deploy/local/seed/instance-2` and plain `mv deploy/seed/<name>/{product.json,cases.json,defaults.json,build-seed.mjs} deploy/local/seed/instance-2/` (the files stay on disk under the ignored path);
3. `git rm --cached deploy/seed/<name>/product.json deploy/seed/<name>/cases.json deploy/seed/<name>/defaults.json deploy/seed/<name>/build-seed.mjs`;
4. `git mv deploy/seed/<name>/seed.sh deploy/seed/seed.sh`.

Acceptance: `git ls-files deploy/local` prints exactly `deploy/local/README.md`, and `git ls-files deploy/seed` prints `deploy/seed/README.md`, `deploy/seed/seed.sh`, `deploy/seed/example/product.json`, `deploy/seed/example/cases.json`. The guard also asserts the first (§3.3).

**`deploy/seed/seed.sh`** (generic):

- `root` becomes `$here/../..` (`seed.sh:18` today climbs three levels from `deploy/seed/<name>/`), so the default `--token-file` stays `deploy/stacks/prod.api-admin`.
- `--dir <seed dir>` is **required**; it is resolved against the current directory (like any path argument) and must hold `product.json` and `cases.json`. Without it the script prints `FAIL --dir is required: the seed directory holding product.json and cases.json` and exits 2 — a bare run on the VM seeds nothing (round 1, zoom-out M4, critic M "seed.sh").
- A `--dir` without both files prints `FAIL no product.json and cases.json in <dir>` and exits 2.
- `--base` defaults to the **local stack**, `http://127.0.0.1:8083/api/v1` (loopback, which the script's own URL rule accepts, `seed.sh:42-53`): an outside user who forgets `--base` sends the admin token nowhere but their own machine (round 2, SEC-9). An instance operator passes `--base http://<tunnel-ip>:18301/api/v1` explicitly; the help and `deploy/seed/README.md` say so. The plain-http rule (https, or http to a loopback or private IP) is unchanged.
- Every other `ok` / `FAIL` string is unchanged (TC-411 quotes them). The help line becomes `deploy/seed/seed.sh --dir <seed dir> [--base <url>] [--token-file <path>] [--dry-run]`.

**A new invented example**: `deploy/seed/example/product.json` and `cases.json` for a fictional web shop, *Sample Shop* (`owner: "Example QA"`, `repo: "example-org/sample-shop-tests"`). The `source` block is `{"generator": "hand-written example", "note": "An invented product. Nothing here describes a real system."}` with **no commit**. Shape: 3 modules (8 features); 4 epics, one with no module (TC-406 asserts the Plan lane holds only that one, `seed_test.go:68-84`); 12 cases — 9 with decision `planned`, 5 of those implemented with a spec, and 3 with another decision (`blocked`, `manual-only`, `wont`, each with a `category:detail` reason); 3 decisions; 5 policies (`technical`, `coding`, `security`, `testing`, one `custom`); 1 flow diagram; `sources: []` (a 40-hex ref to a repository that does not exist would be a false claim). The BRD has three entries per list, so the readiness score clears `PlanFloor` (45, `dashboard/model.go:501`); TC-406 asserts it.

**`deploy/seed/README.md`** documents the seed document (every field of `ProductSeed`, `dashboard/seed.go:27-88`, and of a case, `QaCase`, `dashboard/qacatalog.go:81-101`, with the validation patterns of `qacatalog.go:38-48`), how to build one from your own QA catalog (one story per module; one epic per catalog file; one `cases.json` row per case), and how to run `seed.sh --dir`.

**The tests** (`dashboard/seed_test.go`):

- `seedDir` (`:14`) becomes `../deploy/seed/example/`; `readSeedFile` keeps its signature.
- **TC-406** `TestProductSeedIsIdempotentAndMeetsTheFloor` runs on the example. Its first message (`:45`) becomes `the example seed answered %d: %s`; the register prose quotes it.
- **TC-407** is renamed `TestTheExampleSeedIsWellFormed` and checks **the tracked example only** (round 1, zoom-out M5, readiness M "TC-407", critic N): it keeps every structural check and replaces the count literals with the example's. Its messages become `example seed has %d modules, %d features, %d epics, %d cases; want 3, 8, 4, 12`, `%d implemented and %d planned; want 5 and 9` and `%d epics have no module, want exactly 1 (the report pages)`. The `cases.json names no source commit` assertion is removed (the example has none); the knowledge-source loop stays (vacuous on `sources: []`). A private seed is checked by the server when it is seeded (`seed_test.go:154-198` proves the refusals) and by `seed.sh --dir <private> --dry-run`.

#### 3.2.3 The edge blocks and the redeploy overrides

- `deploy/edge/gaos.conf`: delete the instance domain from the `:80` `server_name` on line 31 (one name of three), the map comment and entry on lines 52-53, and the third `:443` server block on lines 137-165. Append a comment: *"A second instance on this edge keeps its own :80 and :443 blocks in a separate file made from deploy/edge/instance.conf.example and installed beside this one; it proxies to its tunnel address directly and adds nothing to the map above."*
- **New, tracked: `deploy/edge/instance.conf.example`** with placeholders only (`instance.example.org`, `<tunnel-ip>:<port>`, lineage `gaos-<label>`): a `:80` block with the ACME location (`root /var/www/acme`) and the 301 to HTTPS — rule 4 of `gaos.conf:21-23` — and a `:443` block with the same TLS settings, `client_max_body_size 4m`, `proxy_set_header` lines and `proxy_read_timeout` as `gaos.conf:137-165`, a literal `proxy_pass http://<tunnel-ip>:<port>;`, and in place of the `$gaos_backend` check (the host is not in the tracked map, so that check would answer 444 to every request) an explicit `if ($host != instance.example.org) { return 444; }` (round 2, SEC-5). It declares **no `map`** — it reuses `$gaos_connection_upgrade` from `gaos.conf`, and a second declaration would stop nginx (rule 1, `gaos.conf:9-13`) — and **no `default_server`**. It is installed as **`/etc/nginx/conf.d/gaos-<label>.conf`**: that name sorts after `clino.conf`, which must stay the implicit `:443` default (rule 3, `gaos.conf:17-19`), and before `gaos.conf`, so it also wins the transition window while both files name the host (round 2, critic M, SEC-5). Round 1, SEC-6, critic M "edge", readiness M "edge".
- `deploy/edge/README.md` gains a short section *"A second instance"*: copy the example to `deploy/local/edge/gaos-<label>.conf`, fill it in, install it as `/etc/nginx/conf.d/gaos-<label>.conf`, `sudo nginx -t`, reload, `sudo certbot renew --dry-run --cert-name gaos-<label>`, and `curl -sI --resolve instance.example.org:443:<edge-ip> https://instance.example.org/api/healthz` **before** reinstalling a new `gaos.conf` that no longer carries the instance; afterwards `sudo nginx -T 2>&1 | grep -c 'conflicting server name'` prints `0` and `sudo nginx -T | grep default_server` shows no change.
- `deploy/rebuild-restart.sh:95-106`: the twelve-line comment becomes a generic one naming the **ten** variables a second instance overrides (`GAOS_TUNNEL_IP`, `GAOS_HOST`, `GAOS_ARCADE_URL`, `GAOS_ARCADE_DB`, `GAOS_UID`, `GAOS_GID`, `GAOS_QA_URL`, `GAOS_API_QA_PRODUCTS`, `GAOS_QA_ORIGIN`, `GAOS_RUNNER`) and giving the exact invocation: `set -a; . deploy/local/instances/<label>.env; set +a; ./deploy/rebuild-restart.sh prod deploy --tag vX.Y.Z`. The manual-probe note (lines 105-106) stays, with placeholders: *"the post-deploy checks that ssh clino fail when run on the second instance's own VM: probe http://$GAOS_TUNNEL_IP:18301/api/healthz and https://$GAOS_HOST/api/healthz by hand."* No behaviour changes: the script never read the comment.
- `deploy/stacks/compose.prod.yaml:138`: the example value becomes `GAOS_QA_ORIGIN=qa.example.org`.

#### 3.2.4 What the operator must put in place

The main checkout still has the private files on disk. A `git pull` of this change **deletes** `deploy/seed/<name>/` there (a pull removes files the merged commit untracks). So, before the pull, in the main checkout and on the VM that serves the second instance: copy `deploy/seed/<name>/` to `deploy/local/seed/instance-2/`; write `deploy/local/edge/gaos-instance-2.conf` from the new example and the instance's values; write `deploy/local/instances/instance-2.env` with the ten overrides; write `deploy/local/forbidden-strings`. The exact source paths and values go in the change's hand-off, which is delivered **only in the session** to the operator (or saved by the operator under `deploy/local/`) and **never under `docs/`** or any tracked path — a tracked pointer into history at the private content would itself be a pointer for anyone who can read the repository (round 1, SEC-1; round 2, SEC-6). The deployed edge file is not touched by this change (N2).

### 3.3 The guard

`deploy/check-workspace.sh` gains section **7**, after the secrets section and before `done`. Its contract:

- **The list.** The first **non-empty** list (after dropping blank lines and `#` comments — an empty pattern matches everything) of: `$GAOS_FORBIDDEN_STRINGS`; `deploy/local/forbidden-strings`; the same path under the **main** worktree, `$(git rev-parse --path-format=absolute --git-common-dir)/../deploy/local/forbidden-strings` — so an agent worktree, which never receives ignored files, still finds the owner's list (round 1, readiness B2, critic B, SEC-4). If `GAOS_FORBIDDEN_STRINGS` is set but unreadable or empty ⇒ `FAIL GAOS_FORBIDDEN_STRINGS names no readable, non-empty list` (round 2, SEC-7). The filtered patterns are written to a `mktemp` file, mode 600, removed by a `trap`.
- **No list anywhere** ⇒ `info` line `skip private strings: no forbidden-strings list (deploy/local/README.md says where it goes)`, exit status unaffected — a fresh clone has nothing private to leak.
- **The list is tracked** (`git ls-files --error-unmatch deploy/local/forbidden-strings` succeeds) ⇒ `FAIL the forbidden-strings list is tracked; it must stay git-ignored`.
- **Anything else under `deploy/local/` is tracked** (`git ls-files deploy/local` prints a path other than `deploy/local/README.md`) ⇒ `FAIL a file under deploy/local/ is tracked: <shown path>`.
- **An invalid pattern** (`grep -E -f "$tmp" /dev/null` exits 2) ⇒ `FAIL the forbidden-strings list holds an invalid pattern`; the scan does not run.
- **Content**: `git grep --untracked -I -i -n -E -f "$tmp"` — the tracked files **and the untracked, not-ignored ones** (a new review report or page written but not yet added), with no `xargs` batching (round 2, SEC-4). Exit 2 ⇒ `FAIL git grep could not scan the working tree`. **One FAIL per matching line**: `FAIL a tracked file names a private instance: <shown path> (line <n>)`. The matched text is never printed — the same rule the script keeps for secrets (`check-workspace.sh:24-27`).
- **Paths**: `git ls-files --cached --others --exclude-standard | grep -i -E -f "$tmp"`. One FAIL per path: `FAIL a tracked path names a private instance: <shown path>`.
- **The shown path** (round 2, SEC-3, critic M, readiness M): a path is printed as-is when it matches no pattern; when it matches one — tested with the same `grep -i -E -f "$tmp"`, so there is one regex engine and one case rule — it is printed as `path #<k> (<8 hex of its SHA-1>)`, where `<k>` is its line number in `git ls-files --cached --others --exclude-standard`. No masking, so nothing can be half-masked; the operator finds the file with `git ls-files --cached --others --exclude-standard | sed -n '<k>p'`.
- **Pass** ⇒ `ok   no tracked file or path names a private instance (<N> pattern(s) from <env|local|main>)`; the skip line names nothing because there is nothing.
- **Binaries** are skipped by `-I`. Image metadata is checked once by hand in phase 3 (§12), not by the script (round 2, SEC-8).
- The script header (`:9-18`) gains item 7.

**Not done:** a pre-commit hook. `D-025` makes the product owner the runner of every check and the repository has no hooks; the fallback to the main worktree's list closes the case the hook would have (SEC-4, DECLINE for the hook).

**`readme_check`** (`check-workspace.sh:133-144`) stops comparing fixed strings. It compares the README with the values the script already computes (round 1, zoom-out M6): the Go-test count `$go_tests` (`:111`) must appear as `<n> Go tests`, the register size `$qa_rows` (`:104`) as `<n> registered test cases`, and `product.semver` read from `pustaka/docs/assets/toc.js` as `Product version <semver>`. Messages: `ok   README Go-test count matches (<n>)`, `FAIL README Go-test count: README.md does not say "<n> Go tests"`, and the same shape for the two others. These three checks belong to TC-357 / TC-358 (the existing README-count checks, `qa-test-plan.html:2769-2790`); their prose is updated.

QA register: **TC-510**, `check-workspace.sh`, positive and negative: the `ok` line, every `FAIL` line above, and the skip line, quoted.

### 3.4 README and repository metadata

`README.md` is rewritten. Sections, in order: what GAOS is (product and task management for AI agents on a graph; GAOS plans and tracks, ai-manager runs each turn, a worker such as ai-claude-cli works the task in a provisioned workspace); features (each with the docs page that describes it: products and readiness, the Blueprint, the kanban and the manual runner, the design gate and its send-back, card timing, conversations, the inbox and answer sources, the QA sync and its client registry, the ai-manager gateway, back-office sign-in); architecture (the three-tier ASCII the overview page draws); quick start (`docker compose up -d`, `http://localhost:8083/`, and the table of the four paths the local stack serves — `/`, `/app/`, `/docs/`, `/api/healthz` — kept from `README.md:44-50`); configuration (an env table: `GAOS_STORE`, `GAOS_ARCADE_*`, `GAOS_MODEL_MODE`, `GAOS_AIM_*`, `GAOS_RUNNER`, `GAOS_API_*_TOKEN_FILE`, `GAOS_API_QA_PRODUCTS`, `GAOS_API_CLIENTS_FILE`, `GAOS_QA_URL`, `GAOS_QA_TOKEN_FILE`, `GAOS_QA_ORIGIN`, `GAOS_BO_*`, `GAOS_ADDR`, `GAOS_COOKIE_SECURE`, `GAOS_MODEL`, `GAOS_EGRESS_*` — every name checked against a `Getenv`/`envOr` in `dashboard/`; the table says it is the common subset and links to `pustaka/docs/infra/environments.html` for the full list); integrations (three lines linking the three new pages); repository layout; tests (the four commands, `386 Go tests` and `512 registered test cases`); deployment (a pointer to `deploy/STACKS.md`: prod only, from a tag); contributing (`CONTRIBUTING.md`, DCO); licence (kept). `Product version 0.45.0`.

"Reviews, scoring, retry guards — not built" (`README.md:88`) becomes "The scoring engine, the retry guards and the budget guard — not built; the design gate's review is built (D-068, D-084)", so the feature list and the status table agree.

**Repository metadata**, an operator step after merge (§10): `gh repo edit ardihikaru/gaos --description "GAOS — a graph-native agent operating system: product and task management for AI coding agents, with a design gate, a kanban, and integrations for an AI gateway and QA tools." --add-topic agent-orchestration,ai-agents,graph-database,arcadedb,golang,htmx,kanban,task-management,llm,claude,devtools,self-hosted`. Verified with `gh repo view --json description,repositoryTopics`. Setting metadata does not change visibility; the repository stays private until OQ-36 is answered (round 1, SEC-1).

### 3.5 Docs pages

A new ToC part `pustaka/docs/assets/toc/integrations.js`, group **"Integrations"**, listed in `toc.js` `parts` after `guide.js`. Three pages in `pustaka/docs/integrations/` (asset prefix `../`), each with a `page-meta` (`data-version-from="0.45.0"`), using only the components of `pustaka/ai/AUTHORING_SPEC.md` §3. Every host is `gaos.example.org`, `qa.example.org` or `aim.example.org`; no tunnel address; every token is written `gaos_<32+ random characters>` (which `validToken`, `api_v1.go:83-95`, refuses, so it cannot be pasted into production) and minted as `printf 'gaos_%s' "$(openssl rand -hex 16)"`.

1. **`integrations/overview.html`** (id `integrations-overview`, "How integration works").
   - A `.flow` diagram: *QA tools / clients* → *GAOS machine API* → *GAOS (graph, scheduler, back office)* → *ai-manager* → *worker in the product's workspace*.
   - A table of the six connections: direction, base URL variable, auth, credential file, who initiates — QA tools → GAOS (`/api/v1/…`, bearer QA token); a registry client → GAOS (inbox routes, its own bearer token); GAOS → QA tools sync (`GET /api/v1/gaos/results`, `POST /api/v1/gaos/catalog`, bearer outbound token); GAOS → QA tools answer webhook; GAOS → ai-manager chat (`/v1/…`, Basic); GAOS → ai-manager admin (`/admin/…`, bearer admin key).
   - Two sequences as `codeblock`s (the convention of `architecture/gateway.html:96`): **a question round trip** and **a results push**.
   - The three tokens and why they must differ (`whyEqualTokens`, `api_v1.go:76-79`); the answer source (kind, client, origin, actor; BR-57).
2. **`integrations/webhooks-and-clients.html`** (id `webhooks-and-clients`, "Webhooks and external clients").
   - Register a client: the clients-file line format and every refusal reason, quoted from `parseClients` (`answer_source.go:187-248`) and `addClients` (`api_v1.go:198-234`); the token file (mode 600, `readTokenPathStrict`); products; origin; reserved names and kinds.
   - The inbox API: ask, poll, answer — one curl each, linking to `guide/qa-sync-api.html#inbox` for the full error table rather than repeating it.
   - The answer webhook: URL, method, headers (`Authorization: Bearer <outbound token>`, `Content-Type`, `Accept`), the body (`inboxAnswerHook`, `inbox_worker.go:37-46`), any 2xx is delivery, redirects refused, 15 s per call (`qaclient.go:38`), 5 attempts with 30 s / 2 min / 10 min / 1 h, then `gave-up`; idempotency (the receiver keys on `productId` + `externalId`; a retry after a lost 2xx re-sends the same body); the URL rule (`validQaURL`, `qaclient.go:58-90`); only the QA tools receive it today (N4).
   - Verify: `/api/healthz`, the boot lines `api: enabled (…)` and `qa sync: outbound to …`, the back-office *Integrations* view.
   - Troubleshooting: a table of symptom → cause, from the refusal strings.
   - Tutorials (as `ctabs`): **(a) the QA tools** — the three tokens, where each lives on each side, `GAOS_API_QA_PRODUCTS`, `GAOS_QA_URL`, `GAOS_QA_ORIGIN`, the order to deploy in; **(b) ai-manager** — the chat login file, the admin key file, `GAOS_AIM_URL`, `GAOS_MODEL_MODE=aimanager`, `GAOS_RUNNER=manual`, workspace provisioning on the first Blueprint sync, the boot provider check; **(c) a new client, e.g. a mobile app** — mint a token, write the file, add the line, restart, ask/poll/answer with curl.
3. **`integrations/ai-manager-api.html`** (id `ai-manager-api`, "ai-manager REST reference").
   - Common: the two helpers (`AIMClient.do`, `aimanager.go:140-165`; `AIMClient.admin`, `aimanager_admin.go:45-70`), the 30 s client timeout (`aimanager.go:131-136`), the status map `statusError` (`aimanager.go:203-225`), `X-Request-ID` (`RequestIDFor`, `adapter_aimanager.go:100-106`), the configuration table (`modelconfig.go:79-108`, `runner.go:52-60`).
   - One `h2` per call — method and path, query (the admin calls append `?provider=<vendor>` for a non-Claude vendor, `onVendor`, `aimanager_admin.go:76-81`), auth, headers, request body, response GAOS decodes, the statuses GAOS distinguishes and what it does, timeouts and polling, and the caller as `file:line`: (1) `POST /v1/chat`; (2) `GET /v1/requests/{id}/result`; (3) `POST /v1/questions/{id}/answer`; (4) `GET /v1/providers`; (5) `POST /admin/workspaces/provision`; (6) `GET /admin/workspaces/{id}/files`; (7) `PUT /admin/workspaces/{id}/files`; (8) `DELETE /admin/sessions/{id}`.
   - "Calls GAOS never makes", and "Known gaps against ai-manager's own documentation", each labelled *per ai-manager's docs*: a 412 on (7) is not retried; a 423 is read as a workspace, not a session; `finish_reason` is decoded and unread; GAOS's 660 s turn deadline is longer than ai-manager's CLI timeout. Documented, not fixed (N5).

**Existing pages.** `guide/qa-sync-api.html`: every URL → `https://gaos.example.org/api/v1`, the origin example → `qa.example.org`; its *Clients and their source* section (`#inbox-clients`, `guide/qa-sync-api.html:233-250`) keeps the source table and replaces the paragraph on adding a client with a link to the new guide. `product/qa-collaboration.html`: the "Setting it up" table's third column becomes "Example value" with placeholders, and its prose names the sample product neutrally. `infra/environments.html`, decision log, QA plan, implementation plan, changelog, open questions, `assets/toc/product.js`: §3.1 wording only.

### 3.6 The back-office *Integrations* view

**Routes and auth.** No new route. `GET /ui/bo/integrations` (`handleBOView`, `requireBO` at `handlers_bo.go:117-125`) and `GET /ui/bo?view=integrations` (`handleBOSheet`, login sheet when signed out) both reach it; `buildBO`'s switch (`handlers_bo.go:222`) gains `case "integrations"`. **`Cache-Control: no-store`** is set by one helper, `noStoreView(w, view)`, called from **both** handlers for `archive` and `integrations` (round 1, SEC-7, zoom-out N, critic M, readiness N) — which also closes the same gap the archive has on the sheet route today.

**Nav, title and dispatch.** One button after *Archive* in the back-office menu (`templates/backoffice.html:93`): `Integrations`, no count. `case "integrations"` sets `bo.Title = "Integrations"` and `bo.Sub = "What GAOS is connected to, and whether each connection works. Read-only; no secret is shown."`; the block renders them as `<h2>{{.Title}}</h2><p>{{.Sub}}</p>` like the archive (`backoffice.html:655`). `bo-main` (`backoffice.html:118-129`) gains `{{else if eq .View "integrations"}}{{template "bo-integrations" .}}` before the product-detail branch. The block's outer element is `<section id="boIntegrations">`, so a test can cut the view out of the sheet.

**Signatures.** `func (s *server) buildIntegrations(ctx context.Context, bo *BOView)` in `dashboard/integrations.go`; `func noStoreView(w http.ResponseWriter, view string)`; `func urlHost(raw string) string`; `func (a *AIMAdapter) CachedVendorInfo() (infos []VendorInfo, at time.Time, busy bool)` in `dashboard/vendors.go` — `busy` when `TryLock` fails, `infos == nil` when never filled; a stale entry (older than 60 s) is shown with its age, not hidden.

**What the view shows, and where each value comes from** (`BOView` gains `Integrations *IntegrationsPage`; the types live in `dashboard/view.go`):

| Block | Fact | Source | When unset |
|---|---|---|---|
| ai-manager | adapter | `s.describeAdapter()` (`main.go:91-96`) | `unconfigured` |
| | host | `urlHost(a.Client.Base)` when `s.adapter` is an `*AIMAdapter` | `—` |
| | runner | `runnerMode()` (`runner.go:52-60`) | `off` |
| | boot provider check | `server.bootChecked`, a new `time.Time`: `serve` captures `time.Now()` in a local right after `adapter.Smoke` succeeds (`main.go:197`) and sets it in the `&server{…}` literal (`main.go:294`) — boot fails otherwise, so a running adapter passed it | `not run (no gateway)` |
| | providers | `a.CachedVendorInfo()`, a new non-blocking read of the existing 60 s cache (`vendors.go:268-276`): `TryLock`; returns the names with `available`/`not available` and the cache age; never calls the gateway | `not asked since boot — the AI vendors tab asks` / `a provider check is in flight` |
| | last turn | newest `RunStarted` or `RunExited` in `s.store.Events` (newest-first scan, as `handlers_bo.go:320-326` already does): its type, its product's name and its age — **not** its text, which can carry gateway error text | `no turn has run` |
| QA tools | outbound | `s.qaOut != nil && s.qaOut.why == ""` | `off` |
| | outbound host | `s.qaOut.base.Scheme + "://" + s.qaOut.base.Host` — the validated base, path dropped | `—` |
| | why off | `s.qaOut.why`; a nil `s.qaOut` renders `GAOS_QA_URL is not set` | |
| | origin | the `qa-tools` client's `Origin` | `—` |
| | last delivery | newest `ExternalAnswerDelivery` event: age, attempt, outcome (`Rule`), item id, reason (`Payload.Reason`: GAOS's own sentence, plus at most 100 characters of a transport error — an address, never a path or a credential — trimmed to 200 by `RecordExternalDelivery`, `external_inbox.go:384`; the HTTP client's URL is stripped before it is built, `qaclient.go:174-179`) | `no delivery has been attempted` |
| | pending | `len(ExternalPending)` (`external_inbox.go:350-367`) | `0` |
| | given up | from the same event scan: items whose **latest** delivery outcome is `gave-up` (round 1, critic M) | `0` |
| Machine API | state | `s.api.Describe()`; a nil `s.api` renders `disabled (no API token is configured)` | |
| | built-in tokens | admin / QA / outbound: `configured` or `not set` (`hasAdmin`, `hasQA`, `hasOut`) | `not set` |
| | QA products | sorted keys of `qaProducts` | `none` |
| Clients | one row per `s.api.clients` entry | name, kind, origin, products (sorted), scope (`qa` for `qa-tools`, `inbox` otherwise), token `configured` (every admitted client has one) | `No machine client is registered.` |

**`urlHost(raw string) string`** returns `scheme://host[:port]` only when `url.Parse` succeeds, the scheme is `http` or `https`, `Hostname()` is an IP or matches `^[A-Za-z0-9]([A-Za-z0-9.-]{0,251}[A-Za-z0-9])?$`, and the port (if any) is digits; otherwise `""` (round 1, SEC-8: a password with an unescaped `/` or `@` makes `url.Parse` put part of the userinfo in the host, and this refuses that).

**Never rendered:** any token, any token digest, the chat login id or secret, the admin key, a URL's userinfo, path or query, a run event's text, an error returned by ai-manager.

**Visible to every account.** The back office has no roles (`auth.go:168-286`, BR-55): every signed-in account sees the gateway host, the QA host and the client registry. D-085 records this (round 1, SEC-9).

**Template** `bo-integrations` in `templates/backoffice.html`, the archive page's pattern (`backoffice.html:654-700`): one `block` per integration with a `<h4>`, a status word, a facts table (`tm-runs cv-table` inside `tm-scroll`), and a setup list of at most four steps naming the variables to set, ending with a link to its docs page (`../docs/integrations/<page>.html`, `target="_blank" rel="noopener"`). The link assumes the docs are served beside the dashboard on the same origin, as the local stack and prod do (`compose.yaml:1-7`); a dashboard run on its own has no `/docs/` and the link 404s — stated in the template comment (round 2, critic N). The brief asks for "a short setup tutorial per integration, linking to the docs pages", so the steps stay; the docs pages hold the detail (round 1, zoom-out N, DECLINE). Only existing CSS classes (`block`, `tm-runs`, `cv-table`, `tm-scroll`, `tm-line`, `mono`, `btn`); no new colour. The frontend rule's `/break-ui` pass runs on the clients table with a 64-character origin, 30 products, a 32-character name and zero clients (`.claude/rules/frontend.md`, round 1, critic N).

**Tests** (`dashboard/integrations_test.go`). Every secret is a **random-hex sentinel** made per run (`sentinel()` = 16 random bytes as hex), so no needle can occur by accident in ordinary markup (round 1, SEC-8).

- **TC-511** `TestTheIntegrationsViewShowsEachIntegrationAndNoSecret`, positive and negative. The server gets: `newAPIConfig(tokA, tokQ, tokO, "p-1,p-2")` with three sentinel tokens (`gaos_` + 32 hex); one registry client (name `mobile-app`, kind `mobile`, origin `app.example.org`, product `p-9`, a fourth sentinel token) added through `addClients`; `setQAOrigin("qa.example.org")`; a QA outbound `newQaOutbound(qaSrv.URL+"/"+pathSentinel, tokO, nil)` where `qaSrv` is an `httptest.Server` (http on loopback, accepted by `validQaURL`) answering 500 with a sentinel in its body; a real `*AIMAdapter` whose `Client.Base` is `https://aim.example.org:8443/<pathSentinel>?q=<querySentinel>`, whose `Client.Creds` holds `Login`, `Secret` and `AdminKey` sentinels, and whose vendor cache is pre-filled with `claude` available; `bootChecked` set; `GAOS_RUNNER=manual` (`t.Setenv`); one external item asked with `tokQ` on `p-1` through `POST /v1/products/p-1/inbox` (so its asker is `qa-tools`, which `ExternalPending` requires, `external_inbox.go:359-360`), answered through the store's `AnswerExternal` with `By.Kind = "gaos"`, then `newInboxWorker(st, out).RunOnce(ctx, now)` against `qaSrv` (so the reason is the real `the QA tools answered HTTP 500`). Signed in, both `GET /ui/bo/integrations` and `GET /ui/bo?view=integrations` answer 200 with `Cache-Control: no-store`, and so do `GET /ui/bo/archive` and `GET /ui/bo?view=archive` (the archive gains the header on the sheet route; round 2, readiness M). Each integrations body contains `Integrations</h2>`, and **within the `boIntegrations` section** (the body cut between `<section id="boIntegrations">` and its `</section>`, so a word elsewhere in the sheet cannot satisfy it; round 2, critic N) `https://aim.example.org:8443`, `manual`, `claude`, the `qaSrv` host, `qa.example.org`, `mobile-app`, `mobile`, `app.example.org`, `p-9`, `p-1`, `retry`, `the QA tools answered HTTP 500` and `configured`; it contains **none of** the four tokens, their hex SHA-256 digests, the five AIM sentinels (login, secret, admin key, path, query), the QA path sentinel, and the QA server's body sentinel. In the same test, `urlHost` refuses a userinfo-bearing URL whose password holds a `/` (`https://<loginSentinel>:a/b@aim.example.org/`) and a `ftp://` URL, returning `""` for both. Messages: `the integrations view answered %d on %s`, `the %s view on %s is not no-store: %q`, `the integrations view does not show %q`, `the integrations view leaked %q`, `urlHost(%q) = %q, want ""`.
- **TC-512** `TestTheIntegrationsViewNeedsASessionAndSaysWhatIsMissing`, negative and positive. Unsigned, against **the TC-511 fixture** (so `mobile-app` is registered and could leak; round 2, readiness M, critic M), `GET /ui/bo/integrations` with and without `HX-Request` answers 401, and `GET /ui/bo?view=integrations` renders the login sheet; none of the three bodies contains `Integrations</h2>`, `mobile-app`, `app.example.org` or the outbound host. With nothing configured (`newTestServer`: nil `api`, `qaOut` and `adapter`, `main_test.go:34`), the signed-in view answers 200 and says `unconfigured`, `GAOS_QA_URL is not set`, `disabled (no API token is configured)`, `No machine client is registered.`, `no delivery has been attempted` and `no turn has run`. Messages: `an unsigned request to %s answered %d`, `an unsigned request to %s rendered the view`, `the unconfigured integrations view does not say %q`.
- The route-table test (`auth_test.go:285-340`) needs no change: no pattern is added.

### 3.7 Registers and release

- **D-085** (decision log): tracked files carry no operator's instance material; it lives in git-ignored `deploy/local/` (reviewers denied); an invented example replaces the seed and a placeholder template the edge blocks; the guard reads its patterns from a git-ignored list and fails on any match; registers and review reports may be reworded for private strings only (an exception to their append-only rule); the vendored board's copyright holder is no longer named in a tracked file, accepted knowingly; the README's counts are checked against the registers; the read-only *Integrations* view, visible to every signed-in account; the repository is not made public until OQ-36 is answered. Count "Eighty-three entries" → "Eighty-four entries" (`decision-log.html:18`), "Eighty-three decisions" → "Eighty-four decisions" (`toc/product.js:129`), `check-workspace.sh:129`.
- **OQ-36** (open questions, *Operations*): "Is git history rewritten before the repository is made public?" — options and costs from §9; blocks: making the repository public. Counts "Thirty-five decisions" / "All thirty-five in one place" → thirty-six (`open-questions.html:17,76`, `toc/product.js:78,110,114`, `check-workspace.sh:130`). **`pustaka/docs/product/brd.html`** carries the register's range and total (`:1658`, `:1670-1671`): the range becomes `OQ-01…OQ-36`, the total thirty-six, the closing sentence names OQ-36 as raised on 2026-11-21; the answered count is unchanged (round 2, readiness M).
- **QA plan**: TC-510…TC-512 added; TC-406, TC-407, TC-411 rows and prose amended (§3.2.2); TC-357 / TC-358 prose for the README checks (§3.3); counts "five hundred and nine" → "five hundred and twelve" (`qa-test-plan.html:19,79`, `toc/product.js:162,168`, `check-workspace.sh:127-128`); Go-test count "three hundred and eighty-four" → "three hundred and eighty-six" (`toc/product.js:168`, `check-workspace.sh:111-117`).
- **`.claude/rules/qa-test-plan.md`**: the highest-ID line ("TC-509; next free TC-510" → "TC-512; next free TC-513"); the counts paragraph (`:73-81`: "four hundred and ninety-nine", "five hundred and nine" ×2, "three hundred and eighty-four") → the new figures; the range table (`check-workspace.sh` gains TC-510; `dashboard (go test)` gains TC-511, TC-512); the totals line ("509 rows … 362 in `dashboard/` … 384 Go tests" → 512, 364, 386).
- **Changelog** v0.45.0 (minor), **`product.semver`** 0.45.0.

## 4. Failure modes

| Failure | Effect | Handling |
|---|---|---|
| The operator pulls before copying the private seed out | the pull deletes `deploy/seed/<name>/` from the main checkout's disk | §3.2.4 says copy first; the hand-off report repeats it |
| The edge file is reinstalled without the instance file | the second instance's domain answers 444 and its renewal stops | `deploy/edge/README.md`'s new section: install the instance file and dry-run its renewal first |
| No list anywhere | the guard skips with a notice | the skip line prints every run; §3.2.4 lists the file; the worktree fallback finds the main checkout's list |
| A pattern line is a bad regex | without a check the scan matches nothing and prints `ok` | §3.3 runs the patterns against `/dev/null` first; exit 2 fails the run |
| ai-manager is down | the view does not wait: it reads the cache non-blocking and never calls the gateway | `CachedVendorInfo` (§3.6) |
| The event stream is large | a full scan per render | the product detail already pays it (`handlers_bo.go:320`) |

## 5. Tests and the register

| ID | Where | What |
|---|---|---|
| TC-510 | `check-workspace.sh` §7 | no private string in a tracked or untracked-not-ignored file or path; nothing under `deploy/local/` tracked but its README; the list not tracked; an invalid pattern fails; an unreadable `GAOS_FORBIDDEN_STRINGS` fails; a private path is shown by index and hash; skip when absent |
| TC-511 | `integrations_test.go` · `TestTheIntegrationsViewShowsEachIntegrationAndNoSecret` | §3.6 |
| TC-512 | `integrations_test.go` · `TestTheIntegrationsViewNeedsASessionAndSaysWhatIsMissing` | §3.6 |
| TC-406 | amended | runs on the example seed; one message reworded |
| TC-407 | amended, renamed | the example only, the example's counts |
| TC-411 | amended | `deploy/seed/seed.sh --dir`, two new FAIL lines |
| TC-357 / TC-358 | amended prose | the README checks compare with computed values |

Arithmetic: 509 + 3 = 512 register rows; 384 + 2 = 386 `func Test…` (TC-407's rename is not an addition); 362 + 2 = 364 in `dashboard/`.

## 6. Alternatives

- A1. **Delete the seed data instead of moving it.** Rejected: the brief says move, and the instance still needs it.
- A2. **Hashed patterns in the guard.** Rejected: whole-token matching misses a name inside a repository slug or a domain; a regex list in an ignored file is simpler and catches substrings.
- A3. **A separate `/ui/integrations` route.** Rejected: a new pattern needs a route-table row and a gate class; the existing view routes have both.
- A4. **A webhook per registry client.** Rejected for now (N4): a per-client URL and token, a validator per destination and a delivery stream keyed by client — a design of its own.
- A5. **Rewrite git history now.** Not decided here: OQ-36 (§9).
- A6. **A private repository cloned into `deploy/local/`.** Allowed, not required: the ignore rule fits it, and the README says so.
- A7. **Two releases (scrub, then view).** Rejected: N7.

## 7. Change list

The 39 files with matches, by path (`<name>` is the private directory name):

| # | File | Change |
|---|---|---|
| 1-5 | `deploy/seed/<name>/product.json`, `cases.json`, `defaults.json`, `build-seed.mjs`, `seed.sh` | the first four untracked and moved to `deploy/local/seed/instance-2/`; `seed.sh` → `deploy/seed/seed.sh` (§3.2.2) |
| 6 | `deploy/rebuild-restart.sh` | the override comment made generic |
| 7 | `deploy/edge/gaos.conf` | lines 31 (one name), 52-53, 137-165; a pointer comment |
| 8 | `deploy/stacks/compose.prod.yaml` | one comment |
| 9 | `dashboard/seed_test.go` | example dir; TC-406 message; TC-407 renamed and re-counted |
| 10 | `dashboard/testdata/review-reply-long-note.txt` | two repository names |
| 11 | `dashboard/workflow_goal_test.go` | one comment |
| 12 | `dashboard/store_arcade_writes_test.go` | one comment |
| 13 | `dashboard/store_arcade.go` | one comment |
| 14 | `dashboard/seed.go` | one comment |
| 15 | `dashboard/schema/16-qa-catalog.sql` | the renamed design path |
| 16 | `dashboard/qacatalog.go` | the renamed design path |
| 17 | `dashboard/answer_source_test.go` | one table row's host → `qa.example.org` |
| 18 | `docs/designs/<name>-qa-sync.md` | renamed `qa-catalog-sync.md`; wording |
| 19 | `docs/designs/reviews/<name>-qa-sync-review-1.md` | renamed `qa-catalog-sync-review-1.md`; wording |
| 20 | `docs/designs/answer-source.md` | wording |
| 21 | `docs/designs/runner-manual.md` | wording, renamed path |
| 22 | `docs/designs/design-iterate.md` | wording |
| 23 | `docs/designs/bo-login.md` | wording |
| 24 | `docs/designs/reviews/answer-source-security-review-1.md` | wording |
| 25 | `docs/designs/reviews/answer-source-critic-2.md` | wording |
| 26 | `docs/designs/reviews/runner-manual-zoomout-1.md` | wording |
| 27 | `docs/designs/reviews/runner-manual-security-review-1.md` | wording |
| 28 | `docs/designs/reviews/runner-manual-review-1.md` | renamed path |
| 29 | `docs/designs/reviews/runner-manual-comprehension-1.md` | renamed path |
| 30 | `pustaka/docs/guide/qa-sync-api.html` | URLs; client paragraph → link |
| 31 | `pustaka/docs/product/qa-collaboration.html` | wording; example-value table |
| 32 | `pustaka/docs/product/decision-log.html` | wording; D-085; count |
| 33 | `pustaka/docs/product/qa-test-plan.html` | wording; TC rows; counts |
| 34 | `pustaka/docs/delivery/implementation-plan.html` | wording |
| 35 | `pustaka/docs/changelog.html` | wording; v0.45.0 |
| 36 | `pustaka/docs/infra/environments.html` | wording |
| 37 | `pustaka/docs/product/open-questions.html` | OQ-25 wording; OQ-36; counts |
| 38 | `pustaka/docs/assets/toc/product.js` | wording; counts |
| 39 | `artifacts/v1/gaos-arcadedb-model.html` | the renamed path |

Files without matches that change or are new:

| File | Change |
|---|---|
| `.gitignore` | `/deploy/local/*`, `!/deploy/local/README.md` |
| `.claude/settings.json` | two Read deny rules |
| `.claude/rules/agent-workspace.md` | deny-list gains `deploy/local/` |
| `.claude/rules/qa-test-plan.md` | counts and ranges |
| `docs/designs/README.md` | the review-report exception, one sentence |
| `docs/rules/gaos-design-review.md` | the same sentence at the append-only rule (`:71`) |
| `pustaka/docs/product/brd.html` | the open-question range and total (`:1658`, `:1670-1671`) |
| `deploy/local/README.md` | new |
| `deploy/seed/README.md` | new |
| `deploy/seed/example/product.json`, `cases.json` | new |
| `deploy/edge/instance.conf.example` | new |
| `deploy/edge/README.md` | "A second instance" |
| `deploy/check-workspace.sh` | §7; counts; README checks |
| `dashboard/integrations.go` | new: `buildIntegrations`, `urlHost`, `noStoreView` |
| `dashboard/integrations_test.go` | new: TC-511, TC-512 |
| `dashboard/vendors.go` | `CachedVendorInfo` |
| `dashboard/main.go` | `server.bootChecked` |
| `dashboard/handlers_bo.go` | `case "integrations"`; `noStoreView` in both handlers |
| `dashboard/view.go` | the view types |
| `dashboard/templates/backoffice.html` | nav button, `bo-main` branch, `bo-integrations` |
| `pustaka/docs/integrations/overview.html`, `webhooks-and-clients.html`, `ai-manager-api.html` | new |
| `pustaka/docs/assets/toc/integrations.js` | new |
| `pustaka/docs/assets/toc.js` | the part; semver |
| `README.md` | rewritten |
| `docs/designs/open-source-docs.md`, `docs/designs/reviews/open-source-docs-*` | this design and its reviews |

## 8. Risks

- R1. **Missed occurrences the patterns cannot see** — a paraphrase or another identifying detail of the customer. Mitigation: the seed's own text leaves the repository whole; the remaining files are scanned for the listed patterns; the owner can add patterns.
- R2. **Edge drift.** The deployed edge file still holds the second instance's blocks; reinstalling the tracked file without the instance file drops them. Mitigation: `deploy/edge/README.md`'s new section and the hand-off report.
- R3. **History.** Every old commit still carries the strings (N1, OQ-36).

## 9. History rewrite — what it would involve (OQ-36's options; not done)

`git filter-repo --replace-text <expressions file>` plus `--path-rename` for the two renamed designs and the seed directory, over all refs. It changes every commit SHA from the first match onward, so every tag (`v0.1.0` … `v0.45.0`) is rewritten and must be force-pushed; every clone and worktree (the main checkout, the agent worktrees, the VM's `/.worktrees/` checkouts that `rebuild-restart.sh --tag` builds from) must be re-cloned; open pull requests are invalidated; forks keep the old history; GitHub keeps old objects reachable from cached pull-request refs and release archives until its support purges them. The alternative is to keep the repository private, or to publish a fresh repository whose first commit is the scrubbed tree.

## 10. Rollout and rollback

0. **Before implementation:** the operator's list exists — `deploy/local/forbidden-strings` in the main checkout, the four patterns, mode 600 (under the standing delegation, the implementing agent writes it there from the brief; it is never committed). Phase 1's first command checks it (round 2, critic M).
1. Merge, tag v0.45.0. No deploy in this change.
2. Operator: §3.2.4 before pulling the main checkout; then `gh repo edit` (§3.4) and verify with `gh repo view`.
3. **Rollback is a forward fix, never a revert of the scrub**: a revert would re-track the private files and the guard would fail. To back out the view or the pages, revert those files only.

## 11. Approval

**Approved (v3)** by the orchestrating agent under the product owner's standing instruction ("work among your agents, never as my confirmation") — approval by delegation, not by the product owner in person. Round 1's two Blockers and its Critical, and round 2's High (the deny rule's anchoring), are closed by their own remediations as the triage files record; no Blocker and no Critical or High finding is open. Two trade-offs a product owner might weigh are recorded rather than buried: the vendored board's copyright holder is no longer named in a tracked file (§3.1, D-085), and git history keeps the private strings until OQ-36 is answered — the repository stays private until then (N1).

## 12. Phases

| Phase | Files | Depends on | Exit (command → expected) |
|---|---|---|---|
| 1. List and guard | `deploy/local/forbidden-strings` (§10 step 0), `.gitignore`, `deploy/local/README.md`, `deploy/check-workspace.sh` §7, `.claude/settings.json`, `.claude/rules/agent-workspace.md` | §10 step 0 | `test -s <main>/deploy/local/forbidden-strings` → true; `./deploy/check-workspace.sh` → **179 content FAIL lines and 7 path FAIL lines** (the baseline at `c407f11`), no skip line. Each other branch run once and quoted into TC-510: `GAOS_FORBIDDEN_STRINGS=<file holding "(">` → `FAIL the forbidden-strings list holds an invalid pattern`; `GAOS_FORBIDDEN_STRINGS=/nonexistent` → `FAIL GAOS_FORBIDDEN_STRINGS names no readable, non-empty list`; in a fresh clone with no list and `GIT_DIR` of its own → the skip line; `git add -f deploy/local/forbidden-strings` then `git reset` → `FAIL the forbidden-strings list is tracked …`; the output of every run piped through `grep -i -E -f <list>` → nothing. From an agent worktree, a Read of the main checkout's list → refused |
| 2. Seed | §3.2.2 steps 1-4, the example, `deploy/seed/README.md`, `seed_test.go` | 1 | `git ls-files deploy/local` → `deploy/local/README.md`; `deploy/seed/seed.sh --dir deploy/seed/example --dry-run` → `ok   dry run: nothing sent`; `cd dashboard && go test -run 'TestProductSeed|TestTheExampleSeed' ./...` → ok |
| 3. Scrub | the remaining files of §7 rows 6-39, the renames, the edge template and README | 2 | `./deploy/check-workspace.sh` → `ok   no tracked file or path names a private instance (4 pattern(s) from …)`; `git diff -U0 c407f11 -- $(git ls-tree -r --name-only c407f11 docs/designs/reviews) \| grep '^-[^-]' \| grep -v -i -E -f <list>` → nothing (every removed report line matched a pattern; SEC-11); `grep -c acme-challenge deploy/edge/instance.conf.example` → 1 and `grep -c gaos_backend deploy/edge/instance.conf.example` → 0; `git ls-files '*.png' '*.jpg' '*.ico' '*.webp' '*.woff2' \| xargs strings \| grep -i -E -f <list>` → nothing (SEC-8) |
| 4. View | `integrations.go`, `vendors.go`, `main.go`, `handlers_bo.go`, `view.go`, `backoffice.html`, `integrations_test.go` | — | `cd dashboard && go vet ./... && go test -race ./...` → ok, both stores |
| 5. Docs and registers | the three pages, ToC, existing pages, D-085, OQ-36, TC rows, counts, changelog, semver, rules file, README | 3, 4 | `cd pustaka && go run . check ./docs && go test ./...` → ok; `./deploy/check-workspace.sh` and `./deploy/check-stacks.sh` → all checks passed; `grep -rE 'gaos_[0-9A-Za-z]{32}' pustaka/docs/integrations/` → nothing |
| 6. Release | branch, PR, merge, tag | 5 | the tag exists on the remote |
