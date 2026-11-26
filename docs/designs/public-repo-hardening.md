# Public repository hardening: infra redaction, an IPv4 guard, docs rate limits, the registry comma check, and the history rewrite (OQ-36)

**Status:** Approved (v2). The operator approved it on 2026-11-26 ("Approve"), with two decisions: the repository stays public during the rewrite (step 0a declined), and the operator notifies the catalog's owner (step 0b). Round 1 reviewed v1 with five lenses, `reviews/public-repo-hardening-*-1.md`; the triage is in `reviews/public-repo-hardening-review-1.md`.
**Feature:** `public-repo-hardening`
**Version target:** v0.58.0 (main is v0.57.0, `pustaka/docs/assets/toc.js:22`).
**Renumbered** after a parallel release took D-094, D-095, v0.56.0, v0.57.0 and TC-722…TC-750: this change now claims D-096, TC-751, TC-752 and v0.58.0. The round-1 reports and triage keep the old numbers as written.
**Numbers claimed:** **D-096** (the decision on OQ-36), **TC-751**, **TC-752**, **v0.58.0**. No BR is added or amended.
**BRD rules this must obey:** none of the §9 rules govern the docs, the edge or git history. BR-05 is untouched because no event is written.
**Security lens:** required.

**Process note.** The tree changes in §3.1–§3.5 were drafted on branch `fix/public-docs-hardening` while the problem was being measured. Nothing is merged or deployed. This review gates the merge, the deploy and the rewrite. During each review round the standing files (`.claude/CLAUDE.md`, `.claude/rules/*`, `AGENTS.md`, `GEMINI.md`) carry isolation canaries. Those are the review mechanism, not changes, and they are restored with `git checkout` after the round.

## 0. Glossary

- **The edge** is the one public machine. It runs nginx and terminates TLS for every product on this deployment (`deploy/edge/README.md:3-6`).
- **The private VM** runs every container. It is reachable only through a WireGuard tunnel from the edge (`pustaka/docs/infra/overview.html:61-75`).
- **The two prods.** GAOS runs two production instances, each a stack on its own VM, deployed by `deploy/rebuild-restart.sh prod deploy --tag <tag>` from a git checkout on that VM. The script builds from a detached worktree at `.worktrees/<tag>` (`deploy/rebuild-restart.sh:144-160`).
  - **Instance 1** is the original prod, on the private VM above.
  - **Instance 2** is the second instance. Its own overrides live in the git-ignored `deploy/local/instances/` (D-085), and its edge blocks follow `deploy/edge/instance.conf.example`.
  - This document names no host, path or domain of instance 2.
- **pustaka** is the documentation engine. `pustaka/` is a vendored fork of the upstream repository `github.com/ardihikaru/pustaka`, which is public (`deploy/Dockerfile.pustaka:3-6`). Upstream fixes reach the fork as patches. The fork carries its own site version in `toc.js`, which is unrelated to the engine's.
- **Private strings** are one operator's instance names: a sample product, its organisation and its domain. They are kept as extended regexes in the git-ignored `deploy/local/forbidden-strings` and enforced by `deploy/check-workspace.sh` section 7 (D-085). This document never writes them.
- **The infra details** are five values: the edge's public IPv4, the edge's SSH login user, the WireGuard UDP port, and the private VM's two LAN addresses. This document never writes them.

## 1. Problem

### 1.1 The repository has been public since it was created

Observed on 2026-11-26 with `gh api repos/ardihikaru/gaos`: `"visibility":"public"`. The repository's first event, from `gh api repos/ardihikaru/gaos/events`, is a `PublicEvent` at its creation (2026-08-02). It has no forks and no open pull requests.

- OQ-36 states "the repository is private today" (`open-questions.html:888-889`). That is false.
- D-085 commits that "the repository is not made public until [OQ-36] is answered" (`decision-log.html:1804-1805`), and assumed a state that did not exist.

**What was exposed.** D-085 removed the private strings from the *tree* in v0.45.0. Every commit before that still carries them, and has been publicly readable for about four months. That includes:
- five files under a seed directory named after the sample product, among them a customer's real test catalog;
- two design files whose paths carry the name.

At commit `c407f11`, 179 tree lines matched (`docs/designs/open-source-docs.md` §1.1). The figure across all of history was not measured.

### 1.2 The tree publishes the infra details

Before §3.1, the infra details appeared in tracked files:
- the public IP, in 14 lines across 6 files: `deploy/STACKS.md`, `deploy/edge/README.md`, both compose headers, `pustaka/docs/infra/overview.html` and `neighbours.html`;
- the SSH user and the WireGuard port, in `overview.html`;
- the LAN addresses, in `overview.html`, `environments.html` and `deploy/STACKS.md`.

The public IP alone is not secret: both product domains resolve to it. Together with the SSH user and the open UDP port, though, these values give a stranger a list of targets.

**No credential was ever committed.** On 2026-11-26 a scan of every added line in `git log --all -p` looked for credential shapes:
- private-key blocks;
- GitHub tokens (`ghp_`, `github_pat_`);
- `sk-` and `sk-ant-` keys;
- AWS keys;
- Slack tokens;
- WireGuard `PrivateKey`;
- JWTs.

After excluding `*_test.go` fixtures and the documented AWS example key, it found 0 hits. A scan for assignments to `password`, `secret`, `api key` or `token` with a literal value of 16 or more characters found only a regex constant in `dashboard/autofixcheck.go`.

### 1.3 Two ToC parts have been invalid JavaScript since v0.37.0

In `pustaka/docs/assets/toc/guide.js:81` and `toc/product.js:175`, a `}` on one line is followed by `{` on the next with no comma between them. This came in with `b4dcba6`. `node --check` rejects both files. The browser threw on each one, so the Guide and Product groups were missing from every page's sidebar and from client-side search for 18 releases. Every page logged `Unexpected token '{'`. `pustaka check` passed the whole time, because the engine reads the registry with regexes (`pustaka/main.go`, `loadRegistry`).

### 1.4 The public docs have no limits

- **Edge:** `deploy/edge/gaos.conf` sets only `client_max_body_size`. It has no `limit_req` and no `limit_conn`.
- **Container:** neither compose file's `docs:` service sets `mem_limit` or `cpus`.
- **Exposure:** since D-093 the docs, `/docs/__pustaka/search` and `/docs/__pustaka/index.json` are anonymous. One VM runs every product.

### 1.5 Stale claims about the back office

Several places say the back-office login "checks nothing":
- `brd.html:157,187,335,1426`
- `faq.html:294`
- `qa-collaboration.html:253`
- `toc/product.js:50`
- `deploy/STACKS.md:263`

D-079 (v0.39.0) made the login real.

### 1.6 A flaky test blocks deploys

`TestBootReconciliationClosesTheAttemptItRecovered` (`dashboard/taskrun_test.go:332`) failed 2 of 12 isolated runs. It stopped the v0.55.0 deploy once.

The cause is `a.Deadline = time.Millisecond`. That deadline bounds the whole poll (`dashboard/adapter_aimanager.go:241-270`), including the HTTP call that brings back the fake gateway's 404. When the call outlives 1 ms, `AIMClient.do` reports the context error as `ErrGatewayTransient` ("unreachable", `dashboard/aimanager.go:160`). `answered` (`adapter_aimanager.go:274-285`) maps that to `SigGatewayFailed` instead of `SigRequestLost`. `collectTurn` (`dashboard/turns.go:222-249`) then finishes the run instead of orphaning it.

## 2. Goals and non-goals

**Goals**

- G1. **No routable IPv4 in the repository's text, and no infra details in the tree.**
  - No tracked or new text file contains a routable IPv4. A guard enforces this with no private list (§3.2).
  - No tracked file contains any of the five infra details. The SSH user, the port and the LAN addresses are added to the git-ignored private list, so section 7 also enforces those three (§3.2).
- G2. **OQ-36 is decided: rewrite.** After the rewrite, no branch or tag on GitHub, and no annotated tag message, matches:
  - the private strings;
  - the edge IP, the LAN addresses or the SSH user (all bounded matches);
  - any blob that was ever stored under the private seed directory.

  The WireGuard port is excluded from the rewrite and rotated instead (§3.6). Objects GitHub keeps outside branches and tags are N4.
- G3. **The comma check.** `pustaka check` fails on two ToC entries on adjacent lines with no comma between them, and both broken parts are fixed. A `node --check` line in `check-workspace.sh` catches any other syntax error in a ToC part.
- G4. **Per-client and global limits.** One client (an IPv4 address or an IPv6 /64) cannot monopolise the docs. The edge enforces per-client and global limits, and the containers have resource limits.
- G5. **The register is true.** It states the facts about the back office, the repository's visibility and the flaky test.

**Non-goals**

- N1. **Tunnel and Docker addresses stay.** `10.10.0.0/24` and `172.30.0.0/16` are RFC 1918, unreachable from outside, and used as functional values by the deploy scripts.
- N2. **Hostnames stay.** `clino` and `vm-rnd-ardi` are local SSH aliases. Neither equals the redacted SSH user, and neither contains it.
- N3. **Hardware sizes and software versions stay** in the infra pages.
- N4. **GitHub's server-side copies cannot be purged from here.** This covers objects reachable through `refs/pull/*`, cached archives and fetch-by-SHA. The operator asks GitHub Support after the push. The repository has no forks.
- N5. **The upstream pustaka history is not rewritten.** A scan of its whole history for the infra details found nothing. The private-strings scan of upstream is step 0d below, run by the operator because the agent cannot read the list.
- N6. **The live edge file's drift is recorded, not repaired.** The live file carries instance 2's blocks inside `gaos.conf` instead of a separate file (§3.4). Splitting it is a separate edge change.

## 3. Design

### 3.1 Redaction of the tree (done on the branch)

Each value is replaced by its role. Commands resolve the edge at run time with `EDGE_IP=$(dig +short A gaos.mfardiansyah.id | grep -E '^[0-9.]+$' | head -1)`, or take `$LAN_IP` from the operator.

| File | Change |
|---|---|
| `pustaka/docs/infra/overview.html` | The SSH row loses the user ("a local alias; key-only"). The public-address row becomes "one public IPv4 … whatever the DNS A records resolve to", and the LAN row "two LAN interfaces". The WireGuard port becomes "the UDP port in `ListenPort` of `/etc/wireguard/wg0.conf`", and the firewall listing shows `<wg-port>/udp`. The flow node becomes "the one public IP", and the `openssl s_client` example uses `$EDGE_IP`. Two prose sentences drop the literals. |
| `pustaka/docs/infra/neighbours.html` | "the edge's one public IP" |
| `pustaka/docs/infra/environments.html`, `deploy/STACKS.md` | The "must be REFUSED" probe uses `$LAN_IP`, and the STACKS diagram labels the edge by role. |
| `deploy/edge/README.md` | Five places: the role description, `$EDGE_IP` in the `--resolve` probe, "both must print the same address". |
| `deploy/stacks/compose.{dev,prod}.yaml` | Header comment: "clino (the public edge, …)" |

`git grep` for the five values returns nothing on the branch.

### 3.2 Guards (TC-751)

**Section 8 of `deploy/check-workspace.sh`: no routable IPv4.**

- **Files scanned.** `git ls-files --cached --others --exclude-standard`, the same set section 7 uses, so a new page is checked before it is committed.
- **Files skipped:**
  - `deploy/local/`;
  - binary assets;
  - `*_test.go`, because the resolver fixtures need real public addresses;
  - `landing-page/`, because its inline SVG path data looks like dotted quads.
- **How a file is read.**
  - Each file goes through `sed`, which blanks image references (`image:` values and `FROM` arguments), so four-part version tags are not read as addresses.
  - It is then searched with `grep -noP '(?<![0-9.])(?:[0-9]{1,3}\.){3}[0-9]{1,3}(?![0-9]|\.[0-9])'`.
  - The lookarounds make two addresses on one line, an address after a `:` and an address at the end of a sentence each match.
- **What passes.** An address is allowed if it is in RFC 1918, loopback, `0/8`, CGNAT `100.64/10`, link-local, an RFC 5737 range, or multicast and above.
- **Output.**
  - On a failure it prints `a public IPv4 address in a tracked or new file: <file>:<line>`, with no value.
  - When the tree is clean it prints `ok no public IPv4 address in any tracked or new file`.

**ToC syntax.** A second line in section 8 runs `node --check` on every `pustaka/docs/assets/toc.js` and `toc/*.js`.
- On a failure it prints `a ToC part is not valid JavaScript: <file>`.
- When every part is valid it prints `ok every ToC part is valid JavaScript`.
- Without `node` it prints `info skip ToC syntax: node not installed`.

**The private list.** The operator adds three patterns to `deploy/local/forbidden-strings`:
- the SSH user, word-bounded;
- the WireGuard port as `<port>/udp` and `ListenPort *= *<port>`;
- the two LAN addresses, with the dots escaped.

Section 7 then enforces them over every path and content. The agent cannot read or write that file.

**Checks.**
- Positive: the branch passes.
- Negative: an untracked `docs/zz-probe.md` with three lines makes section 8 fail on lines 1–3:
  1. a routable (public) address at the end of a sentence;
  2. `--resolve a:443:` followed by such an address;
  3. a private address, then a space, then such an address.

  A fourth line, `image: alpine/socat:` followed by a four-part version, does not fail. Both were verified on the branch.

### 3.3 The registry comma check (TC-752)

This is upstream commit `1da1a90` (pustaka v0.10.1), applied as a patch.
- `check()` adds `reNoComma = \}[ \t]*\r?\n[ \t]*\{`, which catches two entries on adjacent lines with no comma between them.
- Each match fails as `<part> — line N: two entries with no comma between them — the browser drops this whole file`.
- The test is `TestCheckRejectsAMissingCommaBetweenEntries`, with these messages:
  - `the valid fixture failed check with %d problem(s)`;
  - `a registry with no comma between two entries passed check`.
- The fork has no `mustRead` helper, so the test reads the fixture with `os.ReadFile`.

Run against the pre-fix `toc/guide.js`, `check` prints `assets/toc/guide.js — line 81: …`. After the fix it prints `✓ 33 pages valid`.

### 3.4 Docs rate limits and container limits

**Edge maps and zones.** These are added to `deploy/edge/gaos.conf` after the upgrade map. Every key is empty outside `/docs`, and nginx does not count a request with an empty key.

| Name | What it does |
|---|---|
| `$gaos_client_key` | An IPv6 address's first four groups (its /64) when they are spelled out in full; otherwise `$binary_remote_addr` |
| `gaos_docs_api` | `/docs/__pustaka/*`, per client: 10 MB, 5 r/s, burst 20 |
| `gaos_docs_all` | `/docs/__pustaka/*` across all clients (constant key): 1 MB, 50 r/s, burst 100 |
| `gaos_docs_page` | `/docs` and `/docs/*`, excluding `__pustaka`, per client: 10 MB, 20 r/s, burst 60. Each request is charged to one zone only |
| `gaos_docs_conn` | at most 20 concurrent connections to `/docs` per client |

**Location limits.** Both `location /` blocks (prod, dev) and `instance.conf.example` apply all four, with `limit_req_status 429` and `limit_conn_status 429`. Zone names are global on the edge, so the instance file reuses them.

**Sizing.** A page view is about 15 requests: HTML, two stylesheets, two scripts, fonts and eight ToC parts. Navigation adds one partial per page.

**Validation.** `nginx -t` on the tracked file, with the TLS lines removed, passes in `nginx:1.27-alpine`.

**Containers.** In both compose files, the `docs:` service gains `mem_limit: 256m` and `cpus: 0.5`. Idle use on instance 1 is 6.4 MiB. Dev is retired, but it would be public if it ever ran.

**The live edge.** The live `gaos.conf` differs from the tracked file and also carries instance 2's blocks (N6). Deploying means editing that live file in place:
1. Back it up as `gaos.conf.bak-<date>`.
2. Insert the maps and zones after the upgrade map.
3. Add the location lines to every `location /` whose `proxy_pass` targets a GAOS backend (instance 1 prod and dev, instance 2).
4. Run `nginx -t`, then reload.
5. Keep `diff -u` of the backup against the new file in the operator's record.

If `nginx -t` fails, restore the backup.

### 3.5 Fixes that ride along

- **The two missing commas (§1.3).**
- **The stale back-office prose (§1.5).**
  - The BRD's "Authentication" row becomes "Partly built": real sign-in (D-079), no roles (OQ-37).
  - R-02 becomes "no authorisation", narrowed by D-079.
  - The faq, qa-collaboration and ToC sentences follow.
  - `STACKS.md:263` becomes "checked nothing before v0.39.0 (D-079)".
  - The decision-log rows that describe the past (`decision-log.html:1538,1608,1624`) stay as history.
- **The flaky test (§1.6).**
  - `a.Deadline = time.Minute`, with a comment.
  - The hold after "lost" has already expired, because the run started 9000 s ago.
  - 60 of 60 runs pass.
  - `driveRecovery` (`dashboard/events_test.go:493`) has the same shape with 50 ms. It gets the same treatment, a deadline of `time.Minute`, and its run start is moved back by the same 9000 s so that the requeue still lands.

### 3.6 OQ-36: the history rewrite (D-096)

**The operator's ruling (2026-11-26): rewrite.**

**What is and is not rewritten.**
- Rewritten:
  - every content line, path and commit message matching a private pattern;
  - the edge IP and the two LAN addresses, as dotted quads bounded by `(?<![0-9.])…(?![0-9]|\.[0-9])`;
  - the SSH user, bounded by `(?<![A-Za-z0-9_.-])…(?![A-Za-z0-9_-])`;
  - annotated tag messages.
- Stripped by blob ID, wherever the content was later copied: every blob ever stored under `deploy/seed/<a directory matching a private pattern>/`.
- Not rewritten: the WireGuard port, because a short number cannot be replaced safely across every blob (hashes, `go.sum`, fonts).

**What the operator does at the edge instead.**
- Rotate `ListenPort`: edit `wg0.conf` on the edge and `Endpoint` on the peer, then move the `ufw` rule.
- Confirm that SSH is key-only (`PasswordAuthentication no`).

**The script.** The agent wrote `rewrite-history.sh`. The operator keeps it at `~/gaos-rewrite-history.sh` until the rollback window closes, and reads it before running it.
- It takes no value on the command line. It reads the private list from `deploy/local/forbidden-strings` and the infra values from a mode-600 file, `~/.gaos-infra-values`.
- It prints only counts and indexes. It has three modes:
  - `plan`:
    - takes two mirrors, `pre.git` (never touched) and `work.git`, and checks that their refs are identical;
    - compiles every pattern as a Python regex, and stops if one fails;
    - builds the rules;
    - lists the seed blob IDs;
    - runs `git filter-repo --strip-blobs-with-ids --replace-text --replace-message --filename-callback` on `work.git`;
    - verifies, using the same regex engine. Every one of these must hold:
      - 0 private lines across all patches (diff headers removed), messages, tag messages and paths;
      - 0 infra matches;
      - every stripped blob ID absent;
      - the newest release tag's tree identical before and after.

      Otherwise it stops.
  - `push`: re-adds `origin` (filter-repo removes it), asks for the literal word `REWRITE`, then runs `git push --force --all` and `--tags`.
  - `cleanup`: `shred`s the rules and the blob list, and removes `work.git`.
- It was tested on a throwaway repository with a private name, a copied catalog blob, an annotated tag and look-alike neighbours (`<user>x9`, `<ip>0`). The name, the copy and the tag message were rewritten or stripped, and the neighbours were untouched.

**Steps.**

| # | Step | Who |
|---|---|---|
| 0a | **Containment now.** Make the repository private until step 6 is verified on a fresh clone. Run `gh repo edit ardihikaru/gaos --visibility private --accept-visibility-change-consequences`. The repository has no forks to detach. GitHub Pages and the docs site do not depend on its visibility. | Operator decides |
| 0b | **The data owner.** The operator decides whether the customer whose catalog was exposed must be told. The decision is recorded in D-096's outcome. | Operator |
| 0c | **No other writers.** Every other session and worktree has pushed or abandoned its work. The 14 local worktrees are listed with `git worktree list`. | Operator |
| 0d | **Upstream check.** In an upstream pustaka clone, `git log --all -p | grep -ciE -f deploy/local/forbidden-strings` returns 0. | Operator |
| 1 | v0.58.0 is merged, tagged, pushed and deployed. | Agent |
| 2 | `GAOS_CHECK_TAG=v0.58.0 bash ~/gaos-rewrite-history.sh plan` prints `CLEAN`. | Operator |
| 3 | `bash ~/gaos-rewrite-history.sh push` | Operator |
| 4 | **Each prod VM.** Run `git fetch --force --tags origin`, `git checkout --force v0.58.0`, then `git worktree list` followed by `git worktree remove --force` for every `.worktrees/*`, then `git worktree prune && git gc --prune=now`. Then redeploy: `deploy/rebuild-restart.sh prod deploy --tag v0.58.0`, with instance 2's override file for instance 2. Its line `worktree at v0.58.0 (<sha>)` must show the new SHA, which is `git rev-parse v0.58.0` after the fetch. | Agent |
| 5 | **Workstation.** For the main clone and each worktree, `git fetch --force`, then reset each live branch to its rewritten remote or delete it. Nothing is pushed from a pre-rewrite clone. | Agent |
| 6 | **Verification on a fresh clone.** The `plan` verification, run against a new `git clone --mirror`, must report 0. | Operator |
| 7 | GitHub Support is asked to run garbage collection and purge cached views. Once they confirm, `cleanup` runs and `pre.git` is deleted. | Operator |
| 8 | Make the repository public again (if 0a was taken). | Operator |

**Rollback before step 7.** Run `cd ~/.gaos-rewrite/pre.git && git push --force --all origin-url && git push --force --tags origin-url`. Then redo steps 4–5.

**D-096 in v0.58.0 records the decision, not its execution.**

> **The repository is public, and its history will be rewritten to remove one operator's instance material and the infra details. OQ-36 is decided.**
>
> Context: the repository has been public since it was created (2026-08-02), not private as OQ-36 stated.
>
> Decision:
> - rewrite with `git filter-repo` per `docs/designs/public-repo-hardening.md` §3.6: private strings, the edge and LAN addresses and the SSH user (bounded); the seed blobs stripped by ID; all branches and tags force-pushed; every checkout moved;
> - the WireGuard port is rotated instead of rewritten;
> - the tree is guarded by TC-751 and the private list.
>
> Residual: old objects stay fetchable by SHA until GitHub collects them; anyone who cloned before the rewrite keeps the old history.
>
> Outcome: *pending — filled in by the follow-up commit after step 6.*

**OQ-36's register row** becomes `Decided → D-096 (execution pending)`, and its `h3` gains one line saying so.

**The follow-up commit**, made after step 6 on the rewritten history:
- sets the row to `Answered → D-096`;
- fills in D-096's outcome: the date, the verification counts, the containment and data-owner decisions, and the port rotation.

**Counts.** The register holds 22 `Answered` rows and 18 `Open` rows (counted). The lede at `open-questions.html:18` says "Twenty-two are answered … Of the seventeen still open". The "seventeen" is already wrong today: there are 18. With OQ-36 decided:
- v0.58.0 counts it as answered: 23 answered, 17 open.
- The lede becomes "Twenty-three are answered", and its "seventeen still open" becomes true.
- `toc/product.js:78,110` ("twenty-one", which is stale) becomes "twenty-three".
- `brd.html:1671`, "thirty-six in all, twenty-one answered" (stale, written before OQ-37–40), becomes "forty in all, twenty-three answered".

### 3.7 Registers, counts and release

**New register rows.**
- TC-751: `check-workspace.sh`, Negative. "No public IPv4 address in any tracked or new file, and every ToC part is valid JavaScript". Its `h3` carries the positive and negative probes from §3.2.
- TC-752: `pustaka (go test)`, Negative. `TestCheckRejectsAMissingCommaBetweenEntries`.

**Counts.**

| Count | From → to | Where |
|---|---|---|
| Cases | 721 → 723 | `qa-test-plan.html:19,79`, `toc/product.js:162,168`, `check-workspace.sh:129-130`, `README.md:139` |
| Go tests | 587 → 588 (564 + 24) | `toc/product.js:168`, `check-workspace.sh:114-118`, `README.md:137` |
| Unit-test count | stale | `toc/product.js:170` ("two hundred and eighty-two … twenty-two") becomes "five hundred and sixty-four dashboard and twenty-four pustaka" |
| Decisions | "Ninety-two entries" → "Ninety-three" | `decision-log.html:18`, `check-workspace.sh:131`, and "decisions" in `toc/product.js:129` |
| Answered questions | as §3.6 | as §3.6 |

**QA rule file.** `.claude/rules/qa-test-plan.md`:
- highest TC-752, next TC-724;
- the counts;
- the `check-workspace.sh` row gains TC-751 and the `pustaka (go test)` row gains TC-752;
- totals: "723 rows … 564 in `dashboard/` and 24 in `pustaka/` — 588 Go tests".

This file is the one standing file this change edits, and only its counts and map rows.

**Release.**
- Changelog v0.58.0.
- semver 0.56.0, at `toc.js:22` and `README.md:12`.
- Dashboard `?v=` bump (`templates/page.html`).

## 4. Alternatives rejected

- **A fresh public repository, keeping this one private.**
  - It keeps the old history private and costs no force-push.
  - But every link to this repository, every clone's remote and the deploy checkouts would move to a new name.
  - The rewrite fixes the same problem in place.
  - The operator chose the rewrite.
- **Accepting the history as it is.** A customer's catalog would stay public (the operator rejected this).
- **Leaving the infra values out of the rewrite entirely** (zoom-out).
  - The addresses and the user are unique, bounded tokens. Rewriting them is safe, and the tip-tree check proves no collateral damage.
  - Only the port is unsafe, and it is rotated instead.
- **Renaming the tunnel addresses** (N1).
- **A private-list entry for the public IP instead of a guard.** The guard needs no list and catches any routable address.
- **Rate limits in Traefik.** The edge sees the real client address. Traefik would key on `X-Forwarded-For`, whose trust has to be exactly right (`deploy/edge/gaos.conf:101-111`).

## 5. Risks

- R1. **A concurrent session pushes the old history back.** Another session released upstream pustaka today. Steps 0c and 5 cover this, but both depend on the operator.
- R2. **Rewritten SHAs leave dangling references** in docs, the changelog and agent memories. Tag names survive, and the docs cite tags and paths far more than SHAs.
- R3. **A legitimate reader hits a 429.** Bursts of 60 (page) and 20 (API) with `nodelay` make this unlikely, and a 429 costs one retry.
- R4. **A hand edit of the drifted live edge file** (N6). It is gated by `nginx -t` and recorded with `diff -u`.
- R5. **The guard flags a future SVG or fixture** outside the skipped paths. It names the file and line, and the fix is to add it to the skip list with a reason.
- R6. **Compressed IPv6 addresses fall back to per-address keys** (§3.4). The global zone bounds the total.

## 6. Change list

| # | File | § |
|---|---|---|
| 1 | `pustaka/docs/infra/overview.html`, `neighbours.html`, `environments.html` | 3.1 |
| 2 | `deploy/STACKS.md` | 3.1, 3.5 |
| 3 | `deploy/edge/README.md` | 3.1 |
| 4 | `deploy/stacks/compose.dev.yaml`, `compose.prod.yaml` | 3.1, 3.4 |
| 5 | `deploy/check-workspace.sh` | 3.2, 3.7 |
| 6 | `pustaka/main.go`, `pustaka/main_test.go` | 3.3 |
| 7 | `pustaka/docs/assets/toc/guide.js` | 3.5 |
| 8 | `pustaka/docs/assets/toc/product.js` | 3.5, 3.6, 3.7 |
| 9 | `deploy/edge/gaos.conf`, `deploy/edge/instance.conf.example` | 3.4 |
| 10 | `pustaka/docs/product/brd.html`, `faq.html`, `qa-collaboration.html` | 3.5, 3.6 |
| 11 | `dashboard/taskrun_test.go`, `dashboard/events_test.go` | 3.5 |
| 12 | `pustaka/docs/product/decision-log.html`, `open-questions.html` | 3.6 |
| 13 | `pustaka/docs/product/qa-test-plan.html`, `.claude/rules/qa-test-plan.md`, `README.md` | 3.7 |
| 14 | `pustaka/docs/changelog.html`, `pustaka/docs/assets/toc.js`, `dashboard/templates/page.html` | 3.7 |

Not tracked:
- the live edge file (§3.4);
- the private list and the infra values file (§3.2, §3.6);
- the rewrite script (§3.6).

## 7. Phases and verification

1. **Tree.** All must pass:
   - `cd pustaka && go test ./...` reports ok, and `grep -c '^func Test' main_test.go` = 24;
   - `go run . check ./docs` reports `✓ 33 pages valid`;
   - `cd dashboard && go test ./...` reports ok;
   - `go test -run 'TestBootReconciliationClosesTheAttemptItRecovered' -count=60` reports ok;
   - `deploy/check-workspace.sh` passes in full, with sections 7 (the list present) and 8, and the §3.2 negative probe fails as stated;
   - `deploy/check-stacks.sh` passes.
2. **Release.** PR, merge, tag `v0.58.0`.
3. **Deploy both prods.** On each instance, a headless Chrome run of `/docs/guide/getting-started.html` must:
   - report no `pageerror`;
   - show `.sidebar` containing "The QA sync API" (from `toc/guide.js`);
   - show `.sidebar` containing "Open questions" (from `toc/product.js`).
4. **Edge.** Apply §3.4 on the edge. Then run, for each prod host:
   - `seq 200 | xargs -P 20 -I{} curl -s -o /dev/null -w '%{http_code}\n' https://<host>/docs/__pustaka/info | sort | uniq -c`: some responses are `429`;
   - `seq 200 | xargs -P 20 -I{} curl -s -o /dev/null -w '%{http_code}\n' https://<host>/app/healthz | sort | uniq -c`: all `200`;
   - one cold page load: no `429`.
5. **Rewrite.** Steps 0–8 of §3.6. Then the follow-up commit (D-096's outcome).
