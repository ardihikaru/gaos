# Goldfish comprehension review: `docs/designs/public-repo-hardening.md` (round 1)

## Paraphrase

### What this proposes

1. **Redact the tree (§3.1, already drafted on the branch).** The tracked docs and deploy files still name four details: the edge's public IPv4, the edge's SSH login user, the WireGuard UDP port, and the private VM's two LAN addresses. Each one is replaced by its role. Commands now look the address up at run time: `$EDGE_IP` comes from `dig`, and `$LAN_IP` is supplied by the operator.
2. **Add a tracked IPv4 guard (§3.2, TC-722).** This is a new section 8 in `deploy/check-workspace.sh`. It scans the tracked files under `pustaka/docs`, `deploy` and `docs`, plus `README.md`. It fails on any IPv4 literal outside the private, loopback, CGNAT, link-local, documentation and multicast ranges, and it reports the file and line without the value.
3. **Catch a missing comma between ToC entries (§3.3, TC-723).** This applies an upstream pustaka patch. `pustaka check` gains a regex that rejects a `}` and a `{` on consecutive lines with no comma between them. The two ToC part files broken that way since v0.37.0 are fixed.
4. **Rate-limit the docs (§3.4).** The edge nginx gets two `limit_req` zones keyed by client address, one for the docs API and one for docs pages. The prod `docs` container gets memory and CPU caps. The live edge file has drifted from the tracked copy, so the operator edits it by hand and `nginx -t` gates the reload.
5. **Ride-along fixes (§3.5).** The stale "back-office login checks nothing" prose is corrected, and a flaky dashboard test gets a longer deadline.
6. **Rewrite history (§3.6, D-094, answering OQ-36).** This runs after v0.56.0 ships. The operator mirror-clones the repo and runs `git filter-repo`. It rewrites the private strings and the four infra values, and it drops the seed files that held a customer's catalog. The operator checks the result, force-pushes, moves the prod checkouts and the workstation clones onto the new history, and asks GitHub Support to purge its caches.
7. **Registers and release (§3.7).** TC rows are added, the counts change, and v0.56.0 is released.

### How the existing system works, per this document

- One public edge machine runs nginx and terminates TLS. A private VM runs every container and is reachable only through a WireGuard tunnel.
- D-085 took one operator's private names out of the tree. A git-ignored regex list enforces this through section 7 of `check-workspace.sh`. Every commit before v0.45.0 still contains those names.
- The repository has been public since it was created, even though OQ-36 and D-085 say it is private.
- `pustaka check` reads the ToC registry with regexes, so it accepted JavaScript the browser rejects.
- The edge has no request limits and the docs container has no resource limits. The docs have been public since D-093.
- The flaky test fails because a 1 ms deadline also bounds the HTTP call to the fake gateway. When that call is slow, the error is treated as a transient gateway failure, so the run is finished instead of orphaned.

I could paraphrase every section, so there is no comprehension Blocker from that step.

## Findings

### The rollback mirror is never created - Severity: Blocker
- Affected scope: §3.6 steps 1–3 and "Rollback"
- Finding: Step 1 makes one mirror, `gaos-rewrite.git`, and step 3 runs `git filter-repo` inside it. The Rollback paragraph says "The mirror taken in step 1 is kept untouched as `gaos-pre-rewrite.git`". No step creates a second, untouched copy. The step-1 mirror is the one that gets rewritten. As written, the only rollback for a destructive, force-pushed operation does not exist.
- Recommendation: Add an explicit step before step 3: a second `git clone --mirror` into `gaos-pre-rewrite.git` (or a `cp -a` of the fresh mirror). Then say step 3 runs in `gaos-rewrite.git` only. Also say how to check that the backup is intact before step 5, for example by comparing `git for-each-ref` output with GitHub.

### The push step assumes a remote that filter-repo removes - Severity: Major
- Affected scope: §3.6 steps 3 and 5
- Finding: By default, `git filter-repo` deletes the `origin` remote after it rewrites a repository, precisely so that nobody force-pushes by accident. Step 5 (`git push --force --all`, `--tags`) assumes that remote is still there. This is an unstated prerequisite. There is a second wording problem: the line "GitHub rejects the read-only `refs/pull/*`" describes a `--mirror` push, but `--all` and `--tags` never push `refs/pull/*`, so the warning is attached to the wrong command.
- Recommendation: Add "re-add `origin` (`git remote add origin …`)" before step 5. Move the `refs/pull/*` note to the Rollback paragraph, which is where `--mirror` is used.

### D-094 and OQ-36 publish a completed rewrite before it happens - Severity: Major
- Affected scope: §3.6 "Precondition", the drafted D-094 text, the OQ-36 row change, §7 phases 2 and 5
- Finding: The precondition requires v0.56.0 to be merged, tagged and pushed *before* the rewrite. v0.56.0 ships D-094, written in the past tense ("replaced … dropped … were force-pushed, and every checkout was moved"), and turns OQ-36 into "Answered … rewritten". If the rewrite is delayed, fails at step 4 or is rolled back, the published decision log states something false. The document does not say what happens to D-094 in that case.
- Recommendation: Do one of two things. Either ship D-094 as the decision ("history will be rewritten") with an outcome line filled in after phase 5 in a follow-up commit, or move the register change after the rewrite. Also state what D-094 says if the rollback is used.

### "Both prods" / "both prod VMs" is undefined and contradicts the glossary - Severity: Major
- Affected scope: §3.6 step 6, §7 phases 3 and 5, §0 glossary line 15
- Finding: The glossary defines one private VM that "runs every container". Step 6 says "Prod VMs. On both", and §7 says "Deploy both prods" and "both prods report v0.56.0". The document never says what the second prod is: the second instance from §3.4, a second VM, or something else. It also never says where its checkout lives. Step 7 warns that pushing from any old checkout resurrects the old history, so a reader who cannot list every checkout cannot carry out the step safely.
- Recommendation: Add a glossary entry that names both prod deployments by role and says which machine and checkout path each uses. Also define "the second instance", which `deploy/edge/README.md:164-168` describes. Then list every checkout step 6 and step 7 must move.

### The guard fails on this design document once it is committed - Severity: Major
- Affected scope: §3.2 "Why these paths" (design line 116) and "Checks" (design line 120); `deploy/check-workspace.sh:372-389`
- Finding: Section 8 scans every tracked file under `docs/`, and that includes `docs/designs/`. Design lines 116 and 120 each quote routable (non-private, non-documentation) IPv4 literals as examples. They are preceded by a backtick or a space, so the guard's lookbehind does not skip them. The design is untracked today (`?? docs/designs/public-repo-hardening.md`), which is the only reason "the branch passes". Once it is committed, as the design flow requires, section 8 fails. So do any review reports that quote those lines. §7 phase 1 ("all passed, including section 8") cannot hold after the merge.
- Recommendation: Replace the example addresses in §3.2 with values from the RFC 5737 documentation ranges, or describe them in words. Add a note to the review instructions that reports must not quote them either.

### The guard ignores untracked files, unlike section 7, and the negative check depends on an unstated `git add` - Severity: Major
- Affected scope: §3.2 "What it scans" and "Checks"; `deploy/check-workspace.sh:372` compared with `:323` and `:339`
- Finding: Section 8 lists files with `git ls-files`, which returns tracked files only. Section 7 deliberately uses `--others --exclude-standard` and `git grep --untracked`. A new doc with a public IP therefore passes section 8 until it is staged. A by-hand run before `git add` is exactly when the guard should catch it. The negative check ("a planted `docs/zz-probe.md` … fails") only works if the probe was staged, and the document does not say so.
- Recommendation: Either scan `git ls-files --cached --others --exclude-standard`, as section 7 does, or state the limitation and the reason for it. Also write the `git add` step into the negative check so that someone can reproduce it.

### The answered-question count is wrong - Severity: Major
- Affected scope: §3.6 final paragraph, §3.7 "Twenty-one → twenty-two answered questions"
- Finding: The register already has 22 rows marked `Answered →` and 18 marked `Open` (`open-questions.html`, register table), and its lede already says "Twenty-two are answered" (`open-questions.html:18`). Only the ToC (`toc/product.js:78,110`) says twenty-one. Answering OQ-36 makes 23 answered and 17 open, so the planned "twenty-two" in the ToC is still wrong. The lede (`open-questions.html:18-22`) is not on the change list. It also already says "seventeen still open", which disagrees with its own 18 open rows.
- Recommendation: Recount from the register rows and write the arithmetic down (22 + 1 = 23 answered, 18 − 1 = 17 open). Change `toc/product.js:78,110` to twenty-three and update the lede in `open-questions.html`. Consider adding the answered and open counts to the `check-workspace.sh` prose checks.

### The docs API requests count against both zones; the document implies one - Severity: Nit
- Affected scope: §3.4 "Edge maps and zones" and "Why those numbers"; `deploy/edge/gaos.conf:53-54`
- Finding: The page map's pattern `^/docs(/|$)` also matches `/docs/__pustaka/…`, so every API request is counted against both `gaos_docs_api` and `gaos_docs_page`. The burst arithmetic treats them as separate budgets. §1.4 also names `/index.json` as if it were a root path, when it is served under `/__pustaka/` (`pustaka/main.go:500`).
- Recommendation: Say outright that API calls also count toward the page zone, and include them in the burst calculation. Write the full path `/docs/__pustaka/index.json`.

### The edge verification loop does not hit `/app/healthz` - Severity: Nit
- Affected scope: §7 phase 4
- Finding: The loop requests only `/docs/__pustaka/info`. The line "`/app/healthz` in the same loop never does" describes a request the loop never makes.
- Recommendation: Write the second command out, or alternate the two URLs inside the loop.

### Stale "checks nothing" prose remains in a file this change edits - Severity: Nit
- Affected scope: §1.5, §3.5; `deploy/STACKS.md:263`
- Finding: `deploy/STACKS.md:263` still says the back-office login checks nothing. §1.5's list of locations leaves it out, even though §3.1 edits this same file.
- Recommendation: Add it to §1.5 and §3.5, or say why it stays.

### D-085 is misquoted - Severity: Nit
- Affected scope: §1.1
- Finding: Only OQ-36 says "the repository is private today" (`open-questions.html:888-889`). D-085 (`decision-log.html:1804-1805`) says "the repository is not made public until it is answered", which is a commitment, not a statement of fact.
- Recommendation: Quote each source accurately. The finding still stands: D-085 assumed a state that did not exist.

### Evidence in §1.1 has no anchor - Severity: Nit
- Affected scope: §1.1 (`gh api` visibility, the first `PublicEvent`), §3.6 Precondition ("the 14 local worktrees")
- Finding: The design contract (`docs/designs/README.md` rule 2) requires a `file:line` or an explicit "assumption" label. The repository visibility, the event date and the worktree count have neither.
- Recommendation: Label them as point-in-time observations with the date and command, or save the output to a file and cite it.

### Line references for the flake have drifted - Severity: Nit
- Affected scope: §1.6
- Finding: `poll` is at `adapter_aimanager.go:241-270`, `answered` at `:274-285`, and `collectTurn` at `turns.go:222-249`. The document cites `:240-268`, `:273-283` and `turns.go:213-246`. The root-cause chain is also missing one link: what turns a context timeout into `ErrGatewayTransient`.
- Recommendation: Correct the ranges and cite the place where `Client.Result` maps a deadline error to `ErrGatewayTransient`.

### The sibling helper keeps the same race - Severity: Nit
- Affected scope: §3.5 flaky test; `dashboard/events_test.go:493`
- Finding: `driveRecovery` still bounds the poll with a 50 ms deadline, so the root cause in §1.6 applies there too. "Passed 40/40" lowers the odds but does not remove the race.
- Recommendation: Apply the same fix, or say why 50 ms is safe.

### "Upstream", "the fork" and "pustaka" are not defined - Severity: Nit
- Affected scope: §3.3, §5 R1, N5
- Finding: The document uses "upstream pustaka", "the fork" and a pustaka version (v0.10.1) without saying what pustaka is or how it relates to this repo. The repo itself calls it "vendored" (`pustaka/docs/assets/toc.js:9-12`), and `toc.js` carries a separate site `version` (`:18`) that is easy to confuse with the engine version.
- Recommendation: Add a glossary entry: what pustaka is, where upstream lives, and whether "fork" and "vendored" mean the same thing here.

### The script that does the rewrite is not reviewable - Severity: Nit
- Affected scope: §3.6 opening, §6 "Not tracked"
- Finding: The destructive step runs a script that "lives in the agent's scratchpad", a place the document never defines. The script's content, including the filename callback and the conversion from grep ERE to the Python regexes `filter-repo` expects, is not in the design. No reviewer can check it.
- Recommendation: Put the script in the design with patterns and values as runtime inputs only. If that is not possible, say who reviews it and where it is kept until the rollback window closes. Also state that the forbidden-strings patterns must be valid Python regexes, not only `grep -E` patterns.

### The verification cannot prove the catalog itself is gone - Severity: Nit
- Affected scope: G2 second bullet, §3.6 step 4
- Finding: Step 4 searches for the private strings and the private path names. It cannot detect catalog *content* that sits at a path not containing a private name, or content that was copied elsewhere and has had its names reworded. G2 says the catalog is "gone from history, not just reworded".
- Recommendation: Add a check that searches the rewritten history for a few unique catalog fragments. These would be given to the script at run time, never written into the repository.

### The hostname exemption may leak the redacted SSH user - Severity: Nit
- Affected scope: §2 N2 (design line 81), glossary line 17
- Finding: The glossary treats the edge's SSH user as a detail to redact. N2 keeps two hostnames, and one of them looks like it contains a personal identifier. From the document I cannot tell whether that identifier is the same as, or reveals, the redacted SSH user.
- Recommendation: State that neither kept alias equals or contains the redacted login user. If one does, redact it too.

### G2 overclaims what is reachable on GitHub - Severity: Nit
- Affected scope: §2 G2 compared with N4 and §3.6 step 8
- Finding: G2 says "no reachable commit, tag or branch on GitHub" will match afterwards. N4 and step 8 admit that `refs/pull/*` and fetch-by-SHA stay reachable until GitHub runs garbage collection. OQ-36 (`open-questions.html:894`) also says open pull requests break, and the design does not mention them.
- Recommendation: Scope G2 to branches and tags. Add a line on open pull requests: list them, and say they are closed or recreated.

Solid: §3.3, the comma check, matches `pustaka/main.go:620-622` and `:1381-1388` as described. §3.7's Go-test arithmetic (564 + 24 = 588) and the README line numbers are correct.

Relevant files:
- /home/ardi/devel/projects/ardihikaru/my-projects/gaos/docs/designs/public-repo-hardening.md
- /home/ardi/devel/projects/ardihikaru/my-projects/gaos/deploy/check-workspace.sh
- /home/ardi/devel/projects/ardihikaru/my-projects/gaos/deploy/edge/gaos.conf
- /home/ardi/devel/projects/ardihikaru/my-projects/gaos/pustaka/docs/product/open-questions.html
- /home/ardi/devel/projects/ardihikaru/my-projects/gaos/pustaka/docs/product/decision-log.html
- /home/ardi/devel/projects/ardihikaru/my-projects/gaos/deploy/STACKS.md
- /home/ardi/devel/projects/ardihikaru/my-projects/gaos/dashboard/events_test.go

Counts: B=1 M=6 N=12
