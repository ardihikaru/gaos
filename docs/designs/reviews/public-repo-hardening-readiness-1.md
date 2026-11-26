# Readiness review: `docs/designs/public-repo-hardening.md` (v1)

Lens: Tech Lead, zero-decision rule. I checked the claims against the working tree on `fix/public-docs-hardening`. I did not open anything under `.claude/` or `deploy/local/`. One exception: the harness attached two `.claude/rules/` files to my context without me opening them, and the boundary finding below relies on what `.claude/rules/qa-test-plan.md` says.

Sections that are solid:
- The tree-side drafts match the design. Section 8 of `deploy/check-workspace.sh` is at lines 364-394. The `reNoComma` check and its test are at `pustaka/main.go:622,1385-1387` and `pustaka/main_test.go:1165`. The edge maps and zones are at `deploy/edge/gaos.conf:53-56,95-97,140-142`, with the instance lines at `instance.conf.example:54-57`. The container limits are at `compose.prod.yaml:225-226`, and the test fix is at `dashboard/taskrun_test.go:340`.
- The Go test count is right: `pustaka/main_test.go` has 24 tests, and 564 + 24 = 588.

---

### The rollback mirror is never created - Severity: Blocker
- Affected scope: §3.6 step 1, step 3, "Rollback"
- Finding: Step 1 makes one mirror, `gaos-rewrite.git`, and step 3 runs `git filter-repo` inside that same mirror. The Rollback paragraph then says "the mirror taken in step 1 is kept untouched as `gaos-pre-rewrite.git`". No step makes a second, untouched copy. So once step 3 has run, the only pre-rewrite copy on hand has already been rewritten, and the safety net for a destructive force-push does not exist as written. Two engineers would also disagree on whether "kept untouched" means a rename (which leaves nothing to rewrite) or a second clone.
- Recommendation: Add an explicit step 0, `git clone --mirror … gaos-pre-rewrite.git`, and never touch it. Then clone `gaos-rewrite.git` separately and run filter-repo only there. Add a check before step 5 that `gaos-pre-rewrite.git`'s `git show-ref` still matches GitHub's refs from before the rewrite.

### Unanchored literal replacement of the port and the user can corrupt unrelated content, and step 4 cannot confirm it - Severity: Blocker
- Affected scope: §3.6 step 2 ("the four infra values → `EDGE_IP`, `EDGE_USER`, `WG_PORT`, `LAN_IP`"), step 4, Phase 5
- Finding: `--replace-text` does a literal substring match on every blob in every commit unless a rule says `regex:` with boundaries. The WireGuard port is a short digit string. It can occur inside unrelated numbers: other ports, hashes, `go.sum`, test fixtures, SVG path data. The SSH user is a short word. It may be a substring of names that N2 keeps on purpose (the hostname alias), of the repository owner in `git@github.com:…` URLs, or of Go module paths. The design does not say whether each value is a literal or a bounded regex, so two engineers will write different rules. One of them will silently change content at the v0.56.0 tip, which is the tree the prod VMs check out in step 6. Step 4's "grep for the four infra values → 0" has the same problem in reverse: for a bare number, it will either report a non-zero count for unrelated reasons or hide damage the replacement already did.
- Recommendation: Write each rule out: `regex:` with `\b` or context anchors (for example, the port only next to `ListenPort`, `/udp` or `:` after the edge address; the user only in `user@` or `User ` forms). Add a step 4 check: `git diff --stat <old v0.56.0> <new v0.56.0>` must be empty. The tip was already clean, so any difference proves collateral damage. Also run the full test suites on the rewritten v0.56.0 before pushing.

### D-094 and "OQ-36 Answered" ship in v0.56.0 before the rewrite happens - Severity: Major
- Affected scope: §3.6 "Precondition" (v0.56.0 merged, tagged and pushed), the D-094 text, §3.7, §7 Phases 2 and 5
- Finding: D-094 is written in the past tense: "was rewritten", "All branches and tags were force-pushed", "every checkout was moved". It goes out in v0.56.0, and the precondition requires v0.56.0 to be merged and deployed before the rewrite runs. The `check-workspace.sh:365` comment and `gaos.conf:45` also cite D-094. If the rewrite is aborted or rolled back, the published register says something false. That is the same kind of defect G5 sets out to fix. Readers also cannot tell what the decision log should say between Phase 2 and Phase 5.
- Recommendation: Pick one:
  - (a) D-094 records the *decision*, in the future tense ("will be rewritten, run by the operator after v0.56.0"). A follow-up fix records that it was carried out.
  - (b) Move the D-094 and OQ-36 edits into a release after the rewrite.

  State which, and give the exact text.

### Answered-question count is wrong, and the open-questions lede is missing from the update list - Severity: Major
- Affected scope: §3.6 last paragraph, §3.7 "Twenty-one → twenty-two"
- Finding: `open-questions.html:18` already says "Twenty-two are answered", and the register already has 22 `<td>Answered →` rows. Only `toc/product.js:78,110` say "twenty-one", and they are stale. Once OQ-36 is answered, the right figure is twenty-three. Following the design would leave the ToC at twenty-two, one behind the register, and the lede would not be touched at all. `check-workspace.sh` does not check this count, so nothing will catch it.
- Recommendation: Change §3.6 and §3.7 to say twenty-three. List `open-questions.html:18` (and the lede's list of answered questions at `:18-25`) among the files to edit. Optionally, add a `prose_check` that compares the answered count with the `Answered →` row count.

### The branch modifies files outside the change list - Severity: Major
- Affected scope: §6 Change list; §0 Process note ("the tree changes in §3.1–§3.5 were drafted on branch")
- Finding: The git status for this branch shows these files modified: `.claude/CLAUDE.md`, `.claude/rules/agent-workspace.md`, `.claude/rules/frontend.md`, `.claude/rules/rule-index.md`, `AGENTS.md` and `GEMINI.md`. It also shows an untracked `.claude/worktrees/`. None of them appear in §6, and no section describes them. Changing an agent rule, `CLAUDE.md`, `AGENTS.md` or `GEMINI.md` is itself a trigger for a design (`docs/rules/gaos-design-review.md:47-48`). `.claude/rules/qa-test-plan.md` is in §6, but as of this review it still says the highest ID is TC-721, so its current edit is something §3.7 does not describe. A write allowlist built from §6 would reject this branch.
- Recommendation: Either list each of these files with its exact change, its section and how it is verified (for example, if the "repository is private" wording is being corrected in the standing files, say so), or revert them before merge. Also state that `.claude/worktrees/` stays untracked, or add it to the ignore file.

### The rewrite script, which carries all the risk, is neither specified nor kept anywhere durable - Severity: Major
- Affected scope: §3.6 opening paragraph and steps 2-5; §6 "the rewrite script, which lives in the agent's scratchpad"
- Finding: The script that performs the destructive step is not reviewable, and it is stored in a session scratchpad that can be cleared before Phase 5 runs. Several decisions inside it are left to the implementer:
  - **Regex dialect.** The `forbidden-strings` patterns are POSIX extended regexes (`check-workspace.sh:285-286`, used with `grep -E`). filter-repo's `regex:` rules are Python regexes. The conversion (POSIX classes, escaping) is not defined.
  - **Replacement token.** Every pattern, whether it is a product, an organisation or a domain, becomes `sample-product`. Should a domain become a placeholder domain instead? Not decided.
  - **The `origin` remote.** filter-repo normally removes `origin` after it runs, but step 5 runs `git push --force --all` without naming a remote or URL.
  - **The filename callback.** It is described in words only.
- Recommendation: Put the full script in the design, or in a reviewed file the design names. Keep the patterns out of it: it should read them at run time from the ignored list. Specify the ERE-to-Python conversion rule (or require the list to hold only patterns valid in both). Give one replacement token per pattern category. Make step 5 `git push --force --all <url>` (or re-add the remote). Store the script somewhere that survives until Phase 5.

### The prod step does not match how prods are deployed - Severity: Major
- Affected scope: §3.6 step 6; §7 Phase 5 ("both prods report `v0.56.0`")
- Finding: Prods build from detached per-tag worktrees at `.worktrees/<tag>` (`deploy/rebuild-restart.sh:144-160`), not from the main checkout. Step 6 runs `git checkout --force v0.56.0` and `git describe` in a checkout it does not name. After that, `.worktrees/v0.56.0` still sits on the old SHA and keeps the old objects alive. "Both prods report v0.56.0" does not say which command, run in which directory, proves it. "Both" is also not tied to named hosts or paths.
- Recommendation: Name each prod host and repo path. Add `git worktree remove --force .worktrees/v0.56.0 && git worktree prune` (or re-run `rebuild-restart.sh v0.56.0`, which recreates the worktree when the SHA has changed). State the check: the new v0.56.0 SHA equals the `HEAD` of the recreated worktree, and so does the running stack's provenance line.

### The IPv4 guard misses an address at the end of a sentence - Severity: Major
- Affected scope: §3.2 "What it scans"; G1 ("a tracked guard keeps any routable IPv4 out"); `deploy/check-workspace.sh:375`
- Finding: The pattern requires the match to be followed by `[^0-9.]` or end-of-line. An address followed by a full stop, which is the normal way prose ends a sentence, never matches. The guard's main targets, the docs and designs, are exactly where that occurs. The negative test in §3.2 uses a bare address on its own line, so it cannot catch this. G1's "any routable address" is therefore not delivered.
- Recommendation: Allow a trailing `.` when it is not followed by a digit, for example `([^0-9.]|\.([^0-9]|$)|$)`. Add a second negative probe: an address at the end of a sentence. Record both probes in TC-722's `h3`.

### The live edge file has drifted, and the design leaves the drift in place - Severity: Major
- Affected scope: §3.4 "Live edge"; R4
- Finding: According to the design, the live `/etc/nginx/conf.d/gaos.conf` contains the second instance's server blocks. The tracked file and D-085 say a second instance lives in its own `conf.d/gaos-<label>.conf` (`deploy/edge/gaos.conf:154-157`). The plan edits the live file by hand and leaves it different from the tracked one, so the tracked file is still not authoritative. Step 3, "every `location /` that proxies to a GAOS backend", depends on content in the live file that the design does not describe. Two operators could edit different blocks.
- Recommendation: Decide one of two options. (a) Split the live file into `gaos.conf` (identical to the tracked file) plus `gaos-<label>.conf` (from `instance.conf.example`), then `nginx -t` and reload. (b) Keep the hand edit, record the drift as an accepted residual, and add a step that diffs the live file against the tracked one. Either way, add a check: after the edit, the live file's md5 or diff matches the expected file.

### The zones overlap, and the API paths are misnamed - Severity: Nit
- Affected scope: §1.4, §3.4 "Edge maps and zones"
- Finding: The page map `~^/docs(/|$)` (`gaos.conf:54`) also matches `/docs/__pustaka/…`, so API calls count against both zones. §3.4 presents the two zones as separate. §1.4 and §3.4 refer to `/index.json`, but the route is `/__pustaka/index.json` (`pustaka/main.go:500`), so it falls in the API zone.
- Recommendation: State that API requests count against both zones (or exclude `__pustaka` from the page map), and correct the path.

### Phase 4's check is ambiguous and checks only one side - Severity: Nit
- Affected scope: §7 Phase 4
- Finding: The loop requests a single URL, so "`/app/healthz` in the same loop never does" cannot be carried out as written. There is also no positive check that one ordinary page view gets no 429 (R3).
- Recommendation: Give the exact command that interleaves the two URLs. Add: one cold page load (HTML plus its assets) returns no 429.

### Phase 3 proves only the Guide group - Severity: Nit
- Affected scope: §7 Phase 3
- Finding: "The QA sync API" exists only in `toc/guide.js:83`. The Product group, the other broken part, is not checked, and the headless tool is not named.
- Recommendation: Also assert a label that only appears in the Product group, and name the tool and the command.

### Release counts miss README version and stale ToC prose - Severity: Nit
- Affected scope: §3.7
- Finding: `README.md:12` ("Product version …") must change with the semver, because `check-workspace.sh:149` checks it, but §3.7 lists only `README.md:137,139`. `toc/product.js:170` already carries stale unit-test counts and is not mentioned.
- Recommendation: Add `README.md:12`. Fix `product.js:170` or list it explicitly as out of scope.

### Push and verification wording in the rewrite - Severity: Nit
- Affected scope: §3.6 steps 4-5
- Finding: `git push --force --all` pushes only `refs/heads`, so it never touches `refs/pull/*`. The rejection the design expects happens only with `--mirror`, which the Rollback uses. Step 4's `git log --all -p` does not show annotated-tag messages.
- Recommendation: Move the `refs/pull` remark to the Rollback. Add `git for-each-ref refs/tags --format='%(contents)'` to the step 4 grep.

### The dev docs container has no resource limits - Severity: Nit
- Affected scope: §3.4 "Container limits"; G4
- Finding: The edge limits apply to dev as well, but `compose.dev.yaml:100` `docs:` gets no `mem_limit` or `cpus`. Dev is publicly reachable through the edge too.
- Recommendation: Add the same limits to dev, or state why dev is excluded.

### The user and port have no lasting guard - Severity: Nit
- Affected scope: §3.1 last line; §4 fourth alternative
- Finding: A one-off `git grep` is the only check for the SSH user and the WireGuard port. Section 8 catches only addresses.
- Recommendation: Add both to `deploy/local/forbidden-strings`, so section 7 enforces them, and say so.

### The negative probe needs the file to be tracked - Severity: Nit
- Affected scope: §3.2 "Checks"
- Finding: The guard reads `git ls-files`, so a planted `docs/zz-probe.md` is invisible until it is added to the index.
- Recommendation: Write the probe as `git add -N docs/zz-probe.md`, run the check, then `git rm --cached` and delete the file.

### §1.6 paths are not repository-relative - Severity: Nit
- Affected scope: §1.6, §3.5
- Finding: `adapter_aimanager.go`, `turns.go` and `events_test.go` are under `dashboard/`.
- Recommendation: Prefix them with `dashboard/`.

Counts: B=2 M=7 N=9
