# Goldfish critic review: `docs/designs/public-repo-hardening.md` (v1)

Reviewed against `docs/rules/gaos-design-review.md`, `docs/designs/README.md`, `brd.html`, `decision-log.html` and `open-questions.html`. I checked the claims against the working tree of `fix/public-docs-hardening`. I did not open `.claude/` or `deploy/local/`. Addresses, usernames and ports are cited by `file:line` only.

**Strongest argument against the design.** The rewrite cannot undo four months of public exposure. Forks, clones, archives and GitHub's own PR views all keep the old history. The edge IP is published in DNS, and the design admits it (§1.2). So the part of this change that actually reduces risk is the guard and the tree redaction. For the leaked infrastructure details, the remedy is to change them: move the WireGuard port, restrict SSH. For the customer catalog, it is to tell the data owner. The design does neither. Instead it spends its risk budget on a destructive, global text replacement of short tokens across all history.

---

### The history rewrite replaces short infra tokens everywhere, and its check cannot pass - Severity: Blocker
- Affected scope: §3.6 steps 2–4; §7 phase 5
- Finding and rationale: Step 2 replaces the four infra values as literal text in every blob and message of every commit. A WireGuard port is a short run of digits, and an SSH login can be a common word or part of another identifier. The replacement will also change:
  - unrelated numbers and hex SHAs in the changelog, which cites short SHAs;
  - hashes in `go.sum`, test data and SVG path data;
  - possibly binary assets, such as the many `.woff2` fonts under `pustaka/docs/assets/fonts/` and `dashboard/static/fonts/`.

  Nothing checks for this collateral damage. Step 4's check cannot reach 0 for a numeric value. `git log --all -p` prints `@@` hunk headers and `index <sha>..<sha>` lines, and those can contain the port's digits whatever the rewrite does. If a check cannot pass, people stop trusting it or skip it. Step 4 also never checks that the rewritten `v0.56.0` tree is identical to the tree that was tested and deployed. Prod is then checked out to that rewritten tag (step 6).
- Recommendation:
  - Make each infra rule a regex with word boundaries. Use `regex:\b<value>\b`, and for the IPs use the full dotted quad with the dots escaped.
  - Or drop the infra values from the rewrite, because the IP is public in DNS. Rotate the port and the SSH user instead.
  - Add a mandatory check: `git diff --stat <old v0.56.0> <new v0.56.0>` (and `git rev-parse <tag>^{tree}` for each tag) must be empty, or contain only the intended paths.
  - Run step 4's grep over `git log --all -p --format=%B --no-prefix -U0`, after removing the `@@` and `index` lines, or over `git grep` against every commit's tree.

### The rollback does not exist, and step 5 has nowhere to push - Severity: Blocker
- Affected scope: §3.6 step 1, step 3, step 5, "Rollback"
- Finding and rationale:
  - **No backup.** Step 1 makes one mirror, and step 3 runs `git filter-repo` on it. "Rollback" then says the mirror from step 1 "is kept untouched as `gaos-pre-rewrite.git`". It cannot be both. As written, no untouched copy exists, and the operation is a destructive force-push of all branches and tags.
  - **No remote.** `git filter-repo` removes the `origin` remote after a rewrite. That is a deliberate safety feature. Step 5's `git push --force --all` therefore has no target.
- Recommendation:
  - Step 1: take two mirrors, `gaos-pre-rewrite.git` (read-only, never touched) and `gaos-rewrite.git`. Record `git for-each-ref` from the backup.
  - Before step 5: add `git remote add origin git@github.com:…`.
  - Make the rollback a tested command that names the backup mirror.

### History is checked only with a list built from the tree, and the secret scan covered the tree only - Severity: Major
- Affected scope: §1.2 "What is not in the tree"; §3.6 steps 2 and 4
- Finding and rationale:
  - The credential-shape scan in §1.2 covered the **tree**. The repository has been public since it was created (§1.1). A key committed and later deleted is still in history, and the design never looks for one.
  - The private-string patterns are the D-085 list. That list was built against the tree at v0.45.0 (`decision-log.html:1795-1799`; `open-source-docs.md:30` counts at one commit). It will miss names that appear only in deleted files, in older commits or in commit messages.
  - Step 4 checks with the same list it rewrote with, so it is circular.
- Recommendation:
  - Before deciding what to rewrite, run a history-wide secret scanner over all refs (for example gitleaks or trufflehog in `--all` mode) and list every path that ever existed under `deploy/seed/` (`git log --all --name-only`). Rotate any credential found.
  - Record the result in the design as the input to step 2.

### Exposure outside git is not addressed, and leaked details are not changed - Severity: Major
- Affected scope: §2 N4, §3.6 step 8, D-094 "Residual", §5
- Finding and rationale: N4 covers only `refs/pull/*` and cached archives. Untouched by the rewrite:
  - PR titles, bodies and review comments, which are separate from the git objects;
  - GitHub Release notes and assets;
  - forks, which keep the old network;
  - third-party archives.

  OQ-36's own option text names forks (`open-questions.html:895`). The design never checks whether forks exist. A customer's real catalog has been public for about four months. The design does not mention telling the data owner. The SSH user and the WireGuard port stay the same, so the "target list" in §1.2 stays valid for anyone who already copied it.
- Recommendation:
  - Add a precondition: `gh api repos/<repo>/forks` returns an empty list, or the fork owners are contacted.
  - Add a step to scan and edit PR, issue and release text.
  - Add a hardening step: change the WireGuard `ListenPort`, confirm SSH is key-only and restrict it by source address.
  - Add an explicit operator decision on whether to notify the catalog's owner.
  - Add these to §5.

### The IPv4 guard has several false-negative paths - Severity: Major
- Affected scope: §3.2; `deploy/check-workspace.sh:372-389`
- Finding and rationale:
  1. **Committed files only.** Section 8 lists files with `git ls-files`, so a new file is invisible until it is committed. Section 7 uses `--others --exclude-standard` for exactly this reason (`check-workspace.sh:323,339`). This design document is itself untracked on the branch, so the guard does not scan it today. The negative control in §3.2 ("a planted `docs/zz-probe.md` … fails") cannot have worked unless the probe was `git add`ed, and the design does not say so.
  2. **The `:` exclusion is too broad.** It skips the `--resolve host:443:<ip>` form that the tracked docs use (`deploy/edge/README.md:136,185`), and compose's `extra_hosts: "name:<ip>"`. Putting a literal back on the line where the edge IP was most likely written passes the guard.
  3. **Two addresses one character apart.** `grep -o` consumes the trailing separator of the first match. When a second address follows after a single space or comma, the pattern no longer has the leading character it needs, so the second address is never reported.
  4. **Scope.** The guard covers `pustaka/docs`, `deploy`, `docs` and `README.md`. It omits `artifacts/` (architecture HTML), `CONTRIBUTING.md`, `pustaka/README.md`, `pustaka/ai/`, `.claude/` and `.agents/`, all of which are prose.
- Recommendation:
  - Scan `git ls-files --cached --others --exclude-standard`.
  - Replace the `:` exclusion with a narrow image-tag rule: an address preceded by `:` and immediately after `[a-z0-9/_-]+`, with no host-port shape.
  - Use a lookaround-capable matcher (`grep -P` or awk over every token) so neighbouring addresses are each checked.
  - Use a deny-by-default path set: everything except `dashboard/*_test.go` fixtures and `landing-page/**/*.svg`.
  - Add the `--resolve` form and the neighbouring-address case to TC-722's negative half.

### The answered-question count is wrong, and a stale copy is missed - Severity: Major
- Affected scope: §3.6 last paragraph, §3.7 "Twenty-one → twenty-two"
- Finding and rationale:
  - The register already has 22 rows with `<td>Answered` (open-questions.html, counted).
  - The lede already says "Twenty-two are answered" (`open-questions.html:18`).
  - After D-094 the count is **twenty-three**.
  - `toc/product.js:78,110` ("twenty-one") is already stale.
  - `brd.html:1671` ("thirty-six in all, twenty-one answered") is also stale, and §3.7 does not mention it.

  As written, the plan leaves the three places disagreeing.
- Recommendation: Change the target to twenty-three in `open-questions.html:18`, `toc/product.js:78,110` and `brd.html:1671`. Fix the BRD's "thirty-six in all" while you are there. Consider adding a `prose_check` for the answered count to `check-workspace.sh`.

### The branch changes files the design does not list, and was built before approval - Severity: Major
- Affected scope: §0 "Process note"; §6 change list
- Finding and rationale:
  - The working tree modifies `.claude/CLAUDE.md`, `.claude/rules/agent-workspace.md`, `.claude/rules/frontend.md`, `.claude/rules/rule-index.md`, `AGENTS.md` and `GEMINI.md`. None of them appears in §6. Only `.claude/rules/qa-test-plan.md` is listed.
  - Changing a rule, `CLAUDE.md`, `AGENTS.md` or `GEMINI.md` is itself a design trigger (`gaos-design-review.md:47-48`).
  - Files outside `docs/designs/` were changed before the design existed (`gaos-design-review.md:36,40`; `docs/designs/README.md:3-4`).

  The process note discloses the drafting but not these files. The review can therefore be asked to approve changes it was never shown.
- Recommendation: Either list every modified path in §6 with its purpose, or move the unrelated edits to their own branch and design. State plainly which drafted changes this review is approving.

### D-094 records the rewrite as done in a release that ships before the rewrite - Severity: Major
- Affected scope: §3.6 Precondition, the D-094 text, OQ-36's new `h3`
- Finding and rationale:
  - The precondition merges and tags v0.56.0 *before* the rewrite.
  - v0.56.0 already carries D-094 in the past tense ("history was rewritten", "All branches and tags were force-pushed").
  - It also marks OQ-36 "Answered … rewritten" (dated 2026-11-26).

  If the operator aborts or rolls back, the register states something false. D-094's evidence ("this design and TC-722") is the IPv4 guard, which proves nothing about the rewrite.
- Recommendation: Record D-094 as the *decision to rewrite*, with the procedure, and leave OQ-36 "Decided, pending execution". Mark it Answered in a follow-up commit that is created after the rewrite, from the rewritten history. Cite the step 4 and §7 phase 5 checks as evidence, or a TC that asserts zero matches on a fresh clone.

### Moving the prod checkouts ignores how prod is actually deployed - Severity: Major
- Affected scope: §3.6 step 6
- Finding and rationale:
  - `rebuild-restart.sh` builds from `.worktrees/<tag>`, created at the tag's SHA (`deploy/rebuild-restart.sh:144-160`).
  - Step 6 runs `git checkout --force v0.56.0` in the main checkout and checks `git describe`. That proves nothing about which tree the running stack was built from.
  - The old `.worktrees/*` stay pinned to SHAs from before the rewrite.
  - OQ-36's own cost statement says the VM checkouts are **re-cloned** (`open-questions.html:894`), and the design changes that without saying why.
- Recommendation: On each VM:
  1. Fetch.
  2. Remove `.worktrees/*` and run `git worktree prune`.
  3. Redeploy `v0.56.0` through `rebuild-restart.sh`, so its provenance check reports the new SHA.

  Otherwise, re-clone as OQ-36 states. Verify with the script's own "worktree at" line, not `git describe`.

### The live edge change is a manual edit to an untracked file, with little verification - Severity: Major
- Affected scope: §3.4 "Live edge"; §7 phase 4
- Finding and rationale:
  - **The drift is accepted, not fixed.** The live `gaos.conf` holds the second instance's blocks. That contradicts D-085's layout, a separate `conf.d/gaos-<label>.conf` (`deploy/edge/gaos.conf:154-157`, `instance.conf.example:1-5`). The design edits around this. The tracked file stays non-authoritative, so a later "copy the tracked file" deploy will remove the second instance.
  - **No record of the edit.** Step 3 ("every `location /` that proxies to a GAOS backend") is a judgement call, and no diff of the result is kept.
  - **Phase 4 checks one host only.** It does not check dev or the second instance.
  - **The loop may not trigger.** 120 sequential `curl` calls, each a new TLS connection, may never exceed 5 r/s from a client with high latency.
  - **`/app/healthz` is not in the loop.** The text says it is checked "in the same loop", but the command as written never requests it.
- Recommendation:
  - Save `diff -u gaos.conf.bak-<date> gaos.conf` to the operator's record.
  - Either reconcile the live file with the D-085 layout, or record the drift as a risk with an owner.
  - Run the probe in parallel (`xargs -P 20`).
  - Add the `/app/healthz` request to the loop.
  - Repeat the probe for the dev host and the second instance.

### "Four infra values", but the glossary defines five - Severity: Nit
- Affected scope: §0 "The infra details", §3.6 step 2, D-094
- Finding and rationale: The glossary lists the edge IP, the SSH user, the WireGuard port and **two** LAN addresses, which is five values. Step 2 maps "the four infra values" to four tokens, and step 4 checks "the four".
- Recommendation: Say "five values (two map to `LAN_IP`)" and pass all five as arguments.

### "179 matching lines in all" is a count at one commit - Severity: Nit
- Affected scope: §1.1
- Finding and rationale: `open-source-docs.md:30` counts 179 lines with `git grep` at one commit, `c407f11`. It is not a total across history, so "in all" overstates what was measured.
- Recommendation: Say "179 lines in the tree at `c407f11`; the history-wide figure is not measured".

### G3 overclaims what the comma check catches - Severity: Nit
- Affected scope: §2 G3, §3.3
- Finding and rationale: `reNoComma` (`pustaka/main.go:622`) matches only `}` at the end of one line followed by `{` on the next. A missing comma inside a line (`} {`) or between other tokens (`]`/`[`, between strings) still passes. The check catches the shape that broke the parts, not "a missing comma".
- Recommendation: Narrow G3 to "a missing comma between two entries on adjacent lines", or keep `node --check` from §7 as a registered check.

### API requests also count against the page zone - Severity: Nit
- Affected scope: §3.4 "Why those numbers"
- Finding and rationale: The page map `~^/docs(/|$)` (`deploy/edge/gaos.conf:54`) also matches `/docs/__pustaka/…`, so every API request uses both budgets. The sizing argument ignores this. The zones are also shared by prod, dev and the second instance, keyed only by client address.
- Recommendation: Exclude `__pustaka` from the page key, or state that both buckets are charged and size the limits for it.

### The line reference for the Traefik trust note is stale - Severity: Nit
- Affected scope: §4, "Rate limits in Traefik"
- Finding and rationale: The design cites `gaos.conf:86-94`. After the inserted maps, that note is at `deploy/edge/gaos.conf:101-111`.
- Recommendation: Update the reference.

### The dev docs container gets no resource limits - Severity: Nit
- Affected scope: §3.4 "Container limits"
- Finding and rationale: §1.4 argues that "the VM runs every product". Only `compose.prod.yaml:225-226` gains limits. The dev `docs:` (`compose.dev.yaml:100`) runs on the same VM and is also public through the edge.
- Recommendation: Add the same limits to the dev file, or say why dev is exempt.

### Pattern syntax may change between grep and filter-repo - Severity: Nit
- Affected scope: §3.6 step 2
- Finding and rationale: The forbidden-strings lines are POSIX EREs, matched by `grep -iE` (`check-workspace.sh:318-319`). `regex:` in filter-repo is Python `re`. POSIX classes and `\<` `\>` mean different things or fail there.
- Recommendation: Have the script compile each pattern with Python first, and stop if any pattern fails or differs.

### Another stale Go-test count in the same ToC file - Severity: Nit
- Affected scope: §3.7 Counts
- Finding and rationale: `toc/product.js:170` still says "two hundred and eighty-two dashboard and twenty-two pustaka unit tests". §3.7 edits line 168 of the same entry and leaves 170.
- Recommendation: Fix it in the same change, or say why it is left alone.

### `dig +short | head -1` can return a CNAME - Severity: Nit
- Affected scope: §3.1 (`deploy/edge/README.md:135`, `pustaka/docs/infra/overview.html:349`)
- Finding and rationale: If either domain is ever a CNAME, the first line `dig +short` prints is a hostname, not an address.
- Recommendation: Use `dig +short A <host> | grep -E '^[0-9.]+$' | head -1`.

---

**Sections that hold up:**
- §1.6, the root cause of the flaky test: `adapter_aimanager.go:249-283` and `turns.go:233` confirm it, and the test is at `taskrun_test.go:332-340`.
- §3.3, the comma check: `main.go:1381-1388` and `main_test.go:1145-1167` match the quoted messages. The count of 24 `func Test` is confirmed.
- The comma fixes are in place at `guide.js:81` and `product.js:175`.
- §1.5: "checks nothing" now appears only in `changelog.html:2350`, which is a release record.

Counts: B=2 M=8 N=8
