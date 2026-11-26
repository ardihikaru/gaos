# Zoom-out review: `docs/designs/public-repo-hardening.md` (v1)

**Paraphrase.** The repository has been public since 2026-08-02. The register says it is private, so that claim is wrong. Old commits still hold the private instance strings and a customer catalog, and until this branch the tree also held the edge's public IP, its SSH user, the WireGuard port and two LAN addresses. The design does seven things:
- removes those values from the tree;
- adds an IPv4 guard to `check-workspace.sh`;
- syncs the upstream registry comma check;
- adds nginx rate limits and container limits for the public docs;
- fixes outdated back-office text;
- fixes one flaky test;
- has the operator rewrite all git history with `git filter-repo` (D-094).

**What I checked in the code:**
- `pustaka/main.go:622,1385-1387` and `pustaka/main_test.go:1145,1165` match §3.3.
- `main_test.go` has 24 `func Test`, and `dashboard/` has 564 in 75 files. 564 + 24 = 588, which matches §3.7. Adding TC-722 and TC-723 takes the cases from 721 to 723, which also matches.
- `toc.js:22` is 0.55.0.
- `open-questions.html:888` says "private today".
- Section 8 of `check-workspace.sh` (`:364-394`), the edge maps (`gaos.conf:53-56,95-97,140-142`) and the test fix (`taskrun_test.go:335-340`) all match the design.
- §3.3, §3.5 and the arithmetic in §3.7 hold up, except for one missed count site (Nit 2).

---

### The rewrite replaces short infra values as plain text in every file of every commit - Severity: Blocker
- Affected scope: §3.6 steps 2–4; G2
- Simplicity rationale: `--replace-text` swaps a literal everywhere it appears in every file of history, not only in the files that leaked it. Two of the values are short tokens: a port number, and an SSH user that is probably an ordinary name or handle.
  - A port-sized number can turn up inside any hex digest: `go.sum`, the SHA-256 in a `SOURCE.md` (`check-workspace.sh:269-275` checks one), lockfiles, test fixtures.
  - A user name can turn up inside module paths, repository URLs and author text.
  - The rewrite would silently corrupt old commits so they no longer build or verify.
  - The step-4 check (`grep` count is 0) would **pass** on that corrupted history, so nothing catches it before the force-push.
  - The design never decides how far each replacement reaches, which breaks the README contract rule 6 ("nothing is left undecided").
- Recommendation: limit each infra replacement to its context, for example:
  - `user@` / `User ` for the SSH user;
  - `ListenPort`, `/udp` and the firewall line for the port;
  - or `--paths` restricted to the six or seven files §1.2 names.

  Add a step 4b: after the rewrite, `go build` / `go test` an old tag from the rewritten mirror, and diff `git log --all --stat` before and after, to show that no other files changed. Better still, see Major 5: leave the infra values out of the rewrite.

### The rollback copy is never made - Severity: Major
- Affected scope: §3.6 step 1 versus "Rollback"
- Simplicity rationale: step 1 makes one mirror (`gaos-rewrite.git`), and step 3 rewrites it in place. "Rollback" relies on "the mirror taken in step 1 … kept untouched as `gaos-pre-rewrite.git`", but no step creates that second copy. As written, after step 3 nothing is left to roll back to except GitHub, and step 5 overwrites that.
- Recommendation: add step 1b, `git clone --mirror … gaos-pre-rewrite.git` (or `cp -a`), done before step 3, and run `git -C gaos-pre-rewrite.git count-objects` to show it exists. Also say where it is stored: it holds the private material.

### D-094 says the rewrite happened before it does - Severity: Major
- Affected scope: §3.6 (D-094 text, OQ-36 row), §6 rows 11 and 15, §7 phases 2 and 5
- Simplicity rationale: the precondition is that v0.56.0 is merged and tagged. That release already carries D-094 ("history was rewritten … every checkout was moved") and OQ-36 marked "Answered". The rewrite only runs in phase 5. If the operator stops or rolls back, the published decision log is false, and the record can only be fixed by another release.
- Recommendation: v0.56.0 should record only what is true at that point. For example, D-094 as "decided: rewrite; executed: pending", or OQ-36 left Open with the ruling noted. Make the "executed" entry a small follow-up after phase 5 passes on a fresh clone.

### The guard skips any IPv4 that follows a colon, which is how the edge README's probe writes it - Severity: Major
- Affected scope: §3.2 "What it scans"; `deploy/check-workspace.sh:375`
- Simplicity rationale: the pattern excludes a literal preceded by `:` so that image tags are skipped. But curl's `--resolve host:443:<addr>` is exactly that shape, and the redacted probe is still in that form (`deploy/edge/README.md:136`, `:185`). So are `extra_hosts` entries and `host:addr` pairs. If someone pastes a real address back into the one probe the design was written to fix, the guard says nothing. The negative control (a bare address on its own line) cannot catch this.
- Recommendation: skip by context, not by any colon. Skip only when the text before the colon is an image reference (`[a-z0-9/._-]+:` with no port number before it), or exclude `image:` lines and Dockerfile `FROM` lines. Add a second negative probe line: `--resolve x:443:<documentation-range addr>`. Note that addresses in documentation ranges pass the guard, so the probe needs a routable test value kept outside the repository, for example passed through an environment variable.

### G1 says a guard covers the infra details, but it covers one of five values - Severity: Major
- Affected scope: §2 G1, §3.2, §4 alternative 4, §0 glossary
- Simplicity rationale: the guard lets RFC 1918, so it lets the two LAN addresses (which the glossary itself calls sensitive) back in. It cannot see the SSH user or the WireGuard port at all. A guard for the remaining values already exists: section 7 (`check-workspace.sh:281-362`) matches any regex in the git-ignored `deploy/local/forbidden-strings`. §4 rejects the private list only for the *public* IP.
- Recommendation: keep section 8 for routable addresses. Add the user, the port in context (e.g. `<port>/udp`) and the two LAN addresses to `forbidden-strings`; that is the existing mechanism and needs no new code. Say so in G1, and count it in §7 phase 1 ("section 7 with the list").

### The root problem for the infra values is exposure; rotating them fixes it, rewriting history does not - Severity: Major
- Affected scope: §1.2, §2 G2, §3.6 step 2, §4
- Simplicity rationale:
  - §1.2 admits the public IP is in DNS.
  - The values have been public for about four months, and D-094's own residual says clones, forks and cached objects keep them.
  - Scrubbing them from history adds the riskiest part of the rewrite (Blocker 1) but takes nothing back.
  - The fix that actually removes the "target list" is to change the WireGuard `ListenPort` (and the SSH user, if it matters). That is a one-line config change on the edge plus one peer.
  - The customer catalog and the private strings do justify the rewrite. The infra values do not.
- Recommendation: limit the rewrite to the private-strings patterns and the seed paths. Treat the infra values with the tree redaction (done), the guards (Majors 4 and 5) and a port rotation, recorded as a step or a non-goal with a reason. That removes `EDGE_IP`, `EDGE_USER`, `WG_PORT` and `LAN_IP` from step 2 entirely.

### There is no step to stop the exposure now, and "make the repository private" is never weighed - Severity: Major
- Affected scope: §3.6 precondition and steps; §4
- Simplicity rationale:
  - The catalog stays publicly readable from the moment of discovery through review, merge, tag, deploy and rewrite (phases 2–5).
  - Making the repository private is one setting. It would contain the leak at once and would also make the force-push window safe.
  - OQ-36 (`open-questions.html:892-899`) listed three options. §4 drops "stay private" without comment.
  - §4 rejects the "fresh repository" option only with "the operator chose". R2 shows the rewrite breaks commit hashes (SHA references) anyway, so "loses the public history" is a weaker reason than it looks.
- Recommendation: add step 0, "set visibility private until phase 5 is verified on a fresh clone", or reject it in §4 with a reason (for example, D-093's public docs depend on the repository being public). Restate the fresh-repository rejection on its merits.

### "Four infra values" but five are listed - Severity: Nit
- Affected scope: §0 glossary (IP, user, port, two LAN addresses), §3.1 last line, §3.6 step 2 and D-094
- Simplicity rationale: two LAN addresses are mapped to one token, `LAN_IP`. That is either five literals or a choice to merge them; the document does not say which, and the step-4 "→ 0" check depends on it.
- Recommendation: say "five values, four tokens", or list them by role.

### One "twenty-one answered" count is missing from §3.7 - Severity: Nit
- Affected scope: §3.6 last paragraph, §3.7 "Counts"
- Simplicity rationale: `pustaka/docs/product/brd.html:1671` also says "twenty-one answered". §3.7 names only `toc/product.js:78,110`.
- Recommendation: add `brd.html:1671` to the count sites. `brd.html` is already in change-list row 13.

### Forks are left out of the residual - Severity: Nit
- Affected scope: N4, D-094 "Residual"
- Simplicity rationale: OQ-36 (`open-questions.html:895`) names forks keeping the old history. The design mentions only clones and caches.
- Recommendation: add "public forks keep the old history; GitHub Support cannot purge a fork it does not own" to the residual, along with the fork count at rewrite time.

### The comma check fixes one kind of syntax error; a general check costs the same - Severity: Nit
- Affected scope: §1.3, §3.3, §7 phase 1
- Simplicity rationale: the root problem is that nothing executes or parses the ToC JavaScript (`toc*.js`), so a broken sidebar went unnoticed for 18 releases. `reNoComma` catches only this exact shape. Phase 1 already runs `node --check` by hand. Reusing the upstream patch is correct.
- Recommendation: also add `node --check` over `pustaka/docs/assets/toc*.js` as a line in `check-workspace.sh`, skipping with an "info" message when `node` is missing, so any syntax error fails the run. Otherwise record why not.

### The guard scope is narrower than what the repository publishes - Severity: Nit
- Affected scope: §3.2 "What it scans" / "Why these paths"
- Simplicity rationale:
  - The guard reads `git ls-files` only, while section 7 also scans untracked, non-ignored files (`check-workspace.sh:323`). A new design that has not yet been `git add`ed, like this one, is not scanned.
  - Root files other than `README.md`, `.agents/`, `.claude/` and `dashboard/templates` are public and are not scanned either.
- Recommendation: scan all tracked and untracked, non-ignored files. Exclude the cases the design can actually justify (`*_test.go`, `*.svg`) instead of listing four directories.

### The phase-4 rate-limit check may not reach 5 r/s - Severity: Nit
- Affected scope: §7 phase 4
- Simplicity rationale: 120 sequential `curl` processes each open a new TLS connection to a remote edge. The resulting rate may stay under 5 r/s plus a burst of 20, so the expected `429` may never appear.
- Recommendation: run the requests concurrently (`xargs -P 20`), or reuse one connection (`curl` with many URLs).

### The live edge file still differs from the tracked one - Severity: Nit
- Affected scope: §3.4 "Live edge", R4
- Simplicity rationale: hand-editing the live file is the right move today. But the next person who copies the tracked file over the live one drops the second instance's blocks.
- Recommendation: add one line that records the drift as a follow-up, or as an open question if the operator wants one.

No findings on §3.3 (an upstream patch, reused rather than rewritten), the flaky-test fix (§1.6 / §3.5, root cause anchored and the fix is one line), the back-office text fixes, the container limits, or the count arithmetic in §3.7, apart from Nit 2.

Counts: B=1 M=6 N=7
