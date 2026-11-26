# Security review: public-repo-hardening (round 1)

## Paraphrase

### What this proposes

The repository has been public on GitHub since it was created. The register said it was private. Because of that, every commit before v0.45.0 is still publicly readable, and those commits hold one operator's private instance material, including a customer's real test catalog. The design does six things:

1. **Redact the tree.** Tracked docs and deploy files stop naming the edge's public IPv4, the edge SSH user, the WireGuard port and the private VM's LAN addresses. Commands find the address at run time with `dig` or from an operator variable.
2. **Add an IPv4 guard.** `deploy/check-workspace.sh` section 8 (TC-722) fails on any routable IPv4 literal in `pustaka/docs`, `deploy`, `docs` and `README.md`.
3. **Add a registry comma check.** An upstream pustaka patch (TC-723) makes `pustaka check` reject a ToC part that has a missing comma. The two broken parts are fixed.
4. **Add rate limits for the public docs.** Two nginx `limit_req` zones are keyed by client address and apply only to `/docs` paths. The prod docs container gets memory and CPU limits.
5. **Fix registers and a test.** Stale back-office prose is corrected (login is real since D-079), and a flaky test's deadline is widened.
6. **Rewrite history (D-094 answers OQ-36).** The operator runs `git filter-repo` from a script written by the agent. It replaces the private strings and the four infra values in every blob and message, and drops the seed files that held the catalog. Then the operator verifies with grep, force-pushes, moves every checkout, and asks GitHub Support to purge its caches. The rollback is a pre-rewrite mirror.

### How the existing system works, per this document

- **Network.** One public edge runs nginx, terminates TLS, and proxies over WireGuard to a private VM that runs every container.
- **Docs.** The docs have been anonymous since D-093. Pages, `/__pustaka/search` and `index.json` are unauthenticated. Before this change there were no rate limits and no container limits.
- **Private strings.** These are git-ignored extended regexes in `deploy/local/forbidden-strings`, enforced by `check-workspace.sh` section 7 (D-085). The agent is denied read access to that file.
- **Back office.** It has real authentication since D-079, but no roles (OQ-37). I confirmed this: `dashboard/auth.go:361-373` checks a real session, and `dashboard/auth.go:53` shows the login throttle. The boundary table I was given says the cookie "checks nothing"; that is out of date, and the design's §1.5 is correct.
- **Repository.** Pull-request refs and cached archives on GitHub are read-only to the operator.

## Trust boundaries touched

| Boundary | Authn | Authz | Validation | Far-side failure |
|---|---|---|---|---|
| Public internet → edge (`/docs`) | none (public by D-093) | none | limits by client address (new) | the VM shares CPU and memory with every product; bounded only for prod docs |
| Edge → VM tunnel | WireGuard keys | port map at `deploy/edge/gaos.conf:61-66` | `444` for an unknown Host | not changed |
| Repository → public (GitHub) | none: public | none | section 7 and section 8 guards on the tree; nothing on history until the rewrite | old objects stay reachable through forks, PR refs and caches |
| Operator workstation → GitHub (force-push) | operator SSH key | repository admin | grep counts in the mirror | the rollback copy is the only recovery |

## Findings

### SEC-1 The customer catalog stays publicly exposed for the whole release sequence, with no interim containment - Severity: Critical
- Component: repository visibility / §3.6 ordering
- Evidence: `docs/designs/public-repo-hardening.md:23-28` (public since creation; catalog in history), `:178` (the rewrite waits until v0.56.0 is merged, tagged and pushed), `:193` and `:205` (old SHAs stay fetchable), `:233-234` (the operator rejected "accept the history" because the catalog would stay public); `pustaka/docs/product/open-questions.html:895-896` (forks keep the old history; PR refs and release archives keep old objects).
- Attack path: Anyone on the internet can browse or clone the pre-v0.45.0 tags today and read a customer's real test catalog. The design leaves that open until a release, two prod deploys, an edge change and the rewrite are all done. Even after the rewrite it stays open through forks, `refs/pull/*` and PR diff views until GitHub Support acts. The design names no interim step that stops the exposure now. It also has no step to find out who already holds a copy, and no step to tell the data owner.
- Remediation:
  - Make the repository private now, as step 0, before any other phase. Make it public again only after the rewrite is verified and the support purge is confirmed.
  - Before step 0, list existing forks. A visibility change does not pull back public forks, so each fork owner, or GitHub Support, has to be asked to remove them.
  - Add an incident step to D-094: what the catalog contains, how long it was exposed, and whether the customer must be told.
  - Change §4 to record "temporary private" as the containment step, not as a rejected alternative.
  - I rated Critical over High because the rubric puts any data exposure at Critical, and this one is live.
- Effort: quick-win

### SEC-2 The rollback copy is never created, so the destructive rewrite has no backup - Severity: High
- Component: §3.6 history rewrite
- Evidence: `docs/designs/public-repo-hardening.md:180` (step 1 makes one mirror, `gaos-rewrite.git`), `:186` (step 3 runs `filter-repo` on it, which rewrites in place), `:195` ("the mirror taken in step 1 is kept untouched as `gaos-pre-rewrite.git`"; no step makes this copy), `:191-192` (steps 6-7 force-move every other checkout to the new history).
- Attack path: The operator follows the steps in order. The only copy of the old history is rewritten in step 3. After the force-push and step 7, the remaining local copies are reset too, so a bad replacement cannot be undone. The plan also says nothing about where the backup lives, who can read it, or when it is deleted. It holds exactly the material being purged, so it must not sit in a synced or shared folder.
- Remediation:
  - Add step 1b: `git clone --mirror` a second time, or `cp -a`, into `gaos-pre-rewrite.git`. Record `git for-each-ref` of both mirrors and check they are identical before step 3.
  - Name the backup's location: outside any synced folder and outside the agent scratchpad, with mode 700.
  - Set a deletion date, for example after the GitHub purge is confirmed, and add that deletion to D-094.
- Effort: quick-win

### SEC-3 The verification cannot prove the catalog is gone - Severity: High
- Component: §3.6 steps 3-4
- Evidence: `docs/designs/public-repo-hardening.md:187` (removal is by path: only `deploy/seed/` directories whose name matches a pattern), `:189` (verification is a grep of `git log --all -p` and of path names for the patterns), `:70` (G2: "the seed files … are gone from history").
- Attack path: A catalog file that was ever copied or renamed outside `deploy/seed/<matching dir>` survives the rewrite. Examples are a fixture, a design attachment, or a file renamed before the directory got its name. The catalog's rows need not contain any private pattern, so the grep shows 0 and the customer data stays public. Other gaps:
  - `git log -p` does not show binary blob contents.
  - `git log` does not show annotated tag messages.
  - `filter-repo` `regex:` rules use Python `re`, while the check uses POSIX ERE (`grep -E`), so the two dialects can match differently.
- Remediation:
  - Before the rewrite, record the blob ID of every file ever stored under the seed directories: `git rev-list --all --objects`, filtered by those paths, written to a mode-600 file. Remove those blobs with `--strip-blobs-with-ids` as well as the path callback.
  - After the rewrite, check that `git cat-file -e` fails for every recorded ID in the mirror and in a fresh clone.
  - Also grep `git for-each-ref --format='%(contents)' refs/tags`.
  - Make the step-4 check run the same regex engine as the rewrite.
  - Add these checks to the D-094 evidence.
- Effort: small

### SEC-4 The IPv4 guard misses addresses after a `:` and the second address on a line - Severity: Medium
- Component: `deploy/check-workspace.sh` section 8 (TC-722)
- Evidence: `deploy/check-workspace.sh:375`; `docs/designs/public-repo-hardening.md:104` and `:120` (the only negative test is one address alone on a line).
- Attack path: This is a gap in the guard, not something an outsider can exploit directly. A future commit can bring a routable address back into public files in two ways the guard does not see:
  - **After a `:`.** The leading character class rejects any literal that follows `:`. That covers `host:IP` forms such as compose `extra_hosts`, `--add-host name:IP` and `::ffff:IP`.
  - **A second address on a line.** Each `grep -o` match uses up the separator after it, and `^` does not match in the middle of a line. So an address that follows another literal plus one separator is never matched, for example a private address, a space, then a public address.
- Remediation:
  - Extract every candidate with a bare pattern, for example `grep -noE '([0-9]{1,3}\.){3}[0-9]{1,3}'`, or use a lookbehind (`grep -P '(?<![0-9.])…'`).
  - Skip image tags by context instead of by the character before the match, for example a `name:tag` token whose name contains `/` or a letter.
  - Add negative fixtures for `host:IP`, `::ffff:IP` and "private public" on one line to TC-722.
- Effort: quick-win

### SEC-5 The guard's scope is narrower than G1, and the SSH user and WireGuard port have no guard - Severity: Medium
- Component: G1 / section 8 scope
- Evidence: `docs/designs/public-repo-hardening.md:67` (G1: "No tracked file names the infra details"), `:100` (a one-time `git grep`), `:104` and `:116` (the guard covers four paths, and only IPv4); `deploy/check-workspace.sh:372`.
- Attack path: Tracked, public files outside the four paths are never checked. These include `CLAUDE.md`, `AGENTS.md`, `GEMINI.md`, `.claude/**`, `dashboard/`, `landing-page/` and other root files. I could not check `.claude/` and the memory files because they are off limits to me, so I cannot confirm the branch-wide `git grep` claim for them. And no automated check at all stops the SSH user or the WireGuard port from coming back. Any of these can return in a later commit and be published.
- Remediation:
  - Run section 8 over all tracked files, with a named allowlist of fixture files (the `dashboard/*_test.go` resolver fixtures, `landing-page/` SVGs).
  - Add the edge user, the port and the public address as patterns in the git-ignored `deploy/local/forbidden-strings`. Section 7 already scans every tracked path and prints no values, so this closes the gap at no cost.
  - Restate G1 as what the guard actually enforces.
- Effort: quick-win

### SEC-6 IPv6 address rotation gets around the docs rate limit, and there is no overall ceiling - Severity: Medium
- Component: edge rate limits (§3.4)
- Evidence: `deploy/edge/gaos.conf:53-56` (keyed on `$binary_remote_addr`), `:70` and `:123` (`listen [::]:443`), `deploy/edge/gaos.conf:91-97` (no `limit_conn`); `docs/designs/public-repo-hardening.md:72` (G4).
- Attack path: An IPv6 client usually holds at least a /64 and can send each request from a fresh address. Each address gets its own bucket, so the per-client limit does not bind. The design's stated goal, "one client cannot monopolise the docs", is then held up only by the prod container limit (and dev has none; see SEC-7). Connections are not limited either.
- Remediation:
  - Key IPv6 clients by /64. One way is a `map $remote_addr` that turns an IPv6 address into its first four groups and leaves IPv4 unchanged, used as the zone key.
  - Add `limit_conn` per key.
  - Add an aggregate zone with a constant key for `/docs/__pustaka/` as an overall ceiling.
  - Record the residual risk in D-094.
- Effort: small

### SEC-7 The dev docs container is public but has no resource limits on the shared VM - Severity: Medium
- Component: `deploy/stacks/compose.dev.yaml`
- Evidence: `deploy/stacks/compose.dev.yaml:100-106` (the `docs:` service has no `mem_limit` and no `cpus`); `deploy/stacks/compose.dev.yaml:9-12` ("Anything else that diverges is a bug"); `deploy/edge/gaos.conf:121-151` (dev is served publicly); `docs/designs/public-repo-hardening.md:51` ("The VM runs every product") and `:149` (limits added to prod only).
- Attack path: The dev host is publicly reachable through the edge. A flood that gets past the edge (see SEC-6) can make the unbounded dev docs container use the VM's CPU and memory, which takes down prod and every neighbour product on that VM.
- Remediation: Add the same `mem_limit` and `cpus` to `compose.dev.yaml`'s `docs:` service. Add a `check-stacks.sh` assertion that both files limit the docs service.
- Effort: quick-win

### SEC-8 Plain-text replacement of a short username and a port number damages unrelated content - Severity: Low
- Component: §3.6 step 2
- Evidence: `docs/designs/public-repo-hardening.md:183` (the infra values become literal `--replace-text` rules), `:189` (verification requires a grep count of 0, which pushes toward replacing every substring).
- Attack path: This is an integrity risk, not a confidentiality one. A short username or a port number replaced as a plain substring in every blob of history can change unrelated content: module paths, other hostnames that N2 keeps, numeric data, checksums in `go.sum`, test fixtures. Old tags would then no longer build or verify. Combined with SEC-2, this cannot be reversed.
- Remediation:
  - Use `regex:` rules with boundaries for the user (`(?<![A-Za-z0-9_-])…(?![A-Za-z0-9_-])`) and for the port (`(?<![0-9])…(?![0-9])`).
  - Run `git filter-repo --analyze` and a dry-run diff of how many blobs each rule changes before the push.
  - Make the step-4 check look for context-bounded matches, not raw substring counts.
- Effort: small

### SEC-9 The rewrite's inputs are handled loosely, and the agent-written script is run without a review step - Severity: Low
- Component: §3.6 operator procedure
- Evidence: `docs/designs/public-repo-hardening.md:176` (the agent writes the script; the operator runs it), `:185` (infra literals passed as command-line arguments), `:181-183` (`replacements.txt` holds every pattern and value), `:269` (the script lives in the agent's scratchpad).
- Attack path: Command-line arguments end up in shell history and in the process list. `replacements.txt` and the scratch mirrors are plain-text copies of everything being purged, and no deletion step is named. The agent cannot read the list it is writing a script for, and the script ends in a force-push of every ref. So nobody checks the script against the real inputs before it runs.
- Remediation:
  - Read the values from a mode-600 file, or from a prompt with `read -s`, never from the command line.
  - Delete `replacements.txt` and the scratch mirrors with `shred` when done, apart from the SEC-2 backup.
  - Require the operator to read the script, and to run a `--dry-run` and the step-4 checks, before any `push --force`.
  - Have the script stop if any check is non-zero.
- Effort: quick-win

### SEC-10 N5 scanned the upstream pustaka history for the infra details only, not the private strings - Severity: Low
- Component: N5 scope
- Evidence: `docs/designs/public-repo-hardening.md:84`; `:124` (upstream patches flow between the repositories).
- Attack path: The upstream repository shares code and sync commits with this one. If its history holds the sample product's name, domain or organisation, the purge here does not cover it.
- Remediation: Run the same `grep -ciE -f forbidden-strings` over `git log --all -p` of the upstream repository, and state the result in N5 and in D-094.
- Effort: quick-win

### SEC-11 Redacting the IP does little, and the published topology should be recorded as an accepted risk - Severity: Low
- Component: published infra pages (D-093 made them anonymous)
- Evidence: `docs/designs/public-repo-hardening.md:37` and `:90` (the address is one `dig` away), `:77-82` (N1-N3 keep the tunnel subnet, peer addresses, hostnames and sizes); `pustaka/docs/infra/overview.html:66-85` (the tunnel table and the full firewall rule set); `pustaka/docs/infra/environments.html:165` (which env file holds the database credentials); `deploy/edge/gaos.conf:58-66` (the backend port map).
- Attack path: An anonymous reader still gets a complete map of the deployment: which firewall ports are open, the tunnel addressing, the env-file paths that hold credentials, and the port scheme. The design presents G1 as reducing exposure, but the main fact it removes, the address, is public through DNS.
- Remediation: State in D-094 that topology disclosure is an accepted residual risk and what it covers. Consider removing the credential-file inventory from the public pages, or moving those pages behind pustaka's login layer, which still exists (`deploy/stacks/compose.prod.yaml:214-217`).
- Effort: quick-win

### SEC-12 The live edge config has drifted and now depends on zones defined in another file - Severity: Low
- Component: edge deploy
- Evidence: `docs/designs/public-repo-hardening.md:151-158` (the live file differs from the tracked one and is edited by hand), `deploy/edge/instance.conf.example:54-57` (it uses zones defined only in `gaos.conf`), `deploy/edge/gaos.conf:9-15` (one `nginx -t` failure takes down every product on the box).
- Attack path: Once the live file has been edited by hand, the tracked config no longer describes production, so a later review approves something different from what runs. The second-instance file now fails `nginx -t` if `gaos.conf` is renamed or removed, and that failure takes down every neighbour product.
- Remediation:
  - After the edit, diff the live file against the tracked one and either bring them back in line or record the remaining difference.
  - Move the docs zones and maps into a small `gaos-limits.conf` that both files depend on, and document it in `deploy/edge/README.md`.
- Effort: small

Counts: Critical=1 High=2 Medium=4 Low=5
