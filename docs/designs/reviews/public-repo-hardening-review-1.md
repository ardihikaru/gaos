# public-repo-hardening — review round 1, triage

**Design reviewed:** v1.

**Lenses:**

| Lens | Findings |
|---|---|
| comprehension | B1 M6 N12 |
| zoom-out | B1 M6 N7 |
| critic | B2 M8 N8 |
| readiness | B2 M7 N9 |
| security | Critical 1, High 2, Medium 4, Low 5 |

**Isolation.**
- Canaries were planted in the seven standing files before the run and restored with `git checkout` after it.
- No canary value appears in any saved report.
- The positive control for this repository's harness was established in round 1 of `pustaka-loading-and-motion` the same day, with the same agent definitions.
- Two lenses (readiness and critic) listed the canaried standing files as "modified on the branch". Those modifications are the canaries, not changes. v2's process note says so.

**Redaction.** No report quotes an infra value: the reviewers were told to cite by `file:line`, and a scan of the five reports for dotted quads finds none.

**How the triage is recorded.** Findings raised by several lenses are merged into one row and attributed to each lens that raised them.

| Finding | Lens | Severity | Disposition | Note |
|---|---|---|---|---|
| The rollback mirror is never created | comprehension, readiness, critic, security SEC-2, zoom-out | Blocker / High / Major | PROPOSE FIX | §3.6 `plan` takes `pre.git` (never touched) and `work.git`, and compares their refs before the rewrite. The script was tested. |
| Short infra tokens are replaced in every blob, and the check cannot pass | zoom-out, readiness, critic, security SEC-8 | Blocker / Low | PROPOSE FIX | Bounded regex rules are used for the addresses and the user. The port is no longer rewritten; it is rotated. The tip tree must be identical before and after. Verification strips diff headers. Tested with look-alike neighbours. |
| `filter-repo` removes `origin`, so the push has no target | comprehension, critic | Major | PROPOSE FIX | `push` re-adds `origin` explicitly. |
| D-094 and OQ-36 are recorded as done before the rewrite runs | zoom-out, comprehension, readiness, critic | Major | PROPOSE FIX | D-094 records the decision, with the outcome pending. A follow-up commit after step 6 fills it in. |
| Customer catalog stays exposed through the whole release, with no containment | security SEC-1, zoom-out | Critical / Major | PROPOSE FIX | Step 0a makes the repository private until a fresh clone verifies clean. The operator decides. Step 0b covers the data-owner decision. |
| Verification cannot prove the catalog is gone (copies, tag messages, regex dialect) | security SEC-3, comprehension | High / Nit | PROPOSE FIX | Seed blobs are stripped by ID and their absence is checked. Tag messages are scanned. Verification uses the same Python engine. |
| History never scanned for credentials; the list is built from the tree | critic | Major | PROPOSE FIX | A full-history credential-shape scan was run: 0 hits (§1.2). Seed blobs are taken from all of history, not from the tree. |
| Exposure outside git (forks, PR text, releases); infra values never rotated; data owner | critic, security SEC-1 | Major | PROPOSE FIX | Forks: 0 (§1.1). The port is rotated and SSH checked to be key-only (§3.6). Data owner is step 0b. PR/issue/release text: ESCALATE to the operator (no releases, no open PRs). |
| The IPv4 guard misses `:`-prefixed, adjacent and sentence-final addresses | zoom-out, readiness, critic, security SEC-4 | Major / Medium | PROPOSE FIX | `grep -P` lookarounds; image lines are blanked by context. All three negative probes fail as expected, and the image-tag line passes. |
| The guard ignores untracked files; its scope is too narrow | comprehension, critic, zoom-out, security SEC-5 | Major / Nit / Medium | PROPOSE FIX | It scans tracked and untracked-not-ignored files, deny by default, with a reasoned skip list. |
| G1 overclaims: the user, port and LAN addresses have no guard | zoom-out, readiness, security SEC-5 | Major / Nit | PROPOSE FIX | G1 is restated. The three are added to the private list, which section 7 enforces (operator). |
| The guard fails on this design document (example addresses) | comprehension | Major | PROPOSE FIX | The examples are reworded. Section 8 passes with the design present. |
| Answered-question count is wrong; the lede and `brd.html:1671` were missed | comprehension, readiness, critic, zoom-out | Major / Nit | PROPOSE FIX | 22 → 23 counted from the rows. The lede, the ToC and the BRD are listed. |
| The prod step ignores `.worktrees/<tag>` | readiness, critic, comprehension | Major | PROPOSE FIX | Step 4 removes the worktrees, prunes, runs gc and redeploys. It checks the script's `worktree at` SHA. |
| "Both prods" undefined | comprehension | Major | PROPOSE FIX | Glossary: instances 1 and 2 by role, with no private names. |
| Live edge drift is left in place | readiness, critic, security SEC-12, zoom-out | Major / Low / Nit | PROPOSE FIX (record) + ESCALATE (split) | v2 records the drift (N6), keeps a `diff -u` and gates on `nginx -t`. The file split is escalated. |
| Script not reviewable and kept in a volatile place; values passed on the command line | readiness, comprehension, security SEC-9 | Major / Low | PROPOSE FIX | The operator keeps it at `~/gaos-rewrite-history.sh` and reads it. Values come from a mode-600 file. `cleanup` shreds the rules. The procedure is in §3.6. |
| The branch modifies files not in the change list (`.claude/*`, `AGENTS.md`, `GEMINI.md`) | readiness, critic | Major | CONTEST | Those are the round's isolation canaries, restored after the run (`git status` clean for them). v2's process note says so. |
| The design was drafted after files changed | critic | Major | DECLINE | Disclosed in the process note. Nothing is merged or deployed before approval, and the review gates both. |
| Rate limits: IPv6 rotation, no global ceiling, no `limit_conn` | security SEC-6 | Medium | PROPOSE FIX | /64 key, a global API zone and `limit_conn` 20. `nginx -t` passes. |
| Dev docs container unbounded | security SEC-7, readiness, critic | Medium / Nit | PROPOSE FIX | Same limits in `compose.dev.yaml`. |
| API calls charged to both zones; wrong `/index.json` path | comprehension, readiness, critic | Nit | PROPOSE FIX | The page key excludes `__pustaka`. Paths corrected. |
| The phase-4 probe may not trigger 429 and never hits `/app/healthz` | zoom-out, comprehension, readiness, critic | Nit | PROPOSE FIX | `xargs -P 20`, with separate `/app/healthz` and cold-page checks. |
| The comma check catches one shape; a general syntax check exists | zoom-out, critic | Nit | PROPOSE FIX | `node --check` line in `check-workspace.sh`; G3 narrowed. |
| Phase 3 proves the Guide group only | readiness | Nit | PROPOSE FIX | Also asserts "Open questions" from `toc/product.js`. |
| `README.md:12` and `toc/product.js:170` missing from the counts | readiness, critic | Nit | PROPOSE FIX | Listed in §3.7. |
| `driveRecovery` keeps the same race | comprehension | Nit | PROPOSE FIX | `time.Minute` plus a start 9000 s ago. Passed 20/20. |
| "Four infra values" but five are listed | zoom-out, critic | Nit | PROPOSE FIX | "Five values". |
| "179 in all" overstates | critic | Nit | PROPOSE FIX | "179 tree lines at `c407f11`". |
| D-085 misquoted; observations unanchored; stale line refs; paths not repo-relative | comprehension, readiness | Nit | PROPOSE FIX | Corrected. |
| pustaka / fork / upstream undefined | comprehension | Nit | PROPOSE FIX | Glossary. |
| Hostname alias may contain the SSH user | comprehension | Nit | PROPOSE FIX | N2 states that it does not. |
| `dig +short | head -1` can return a CNAME | critic | Nit | PROPOSE FIX | `dig +short A … | grep -E '^[0-9.]+$'` in §3.1. The two edited commands are updated in the implementation. |
| Upstream history not scanned for private strings | security SEC-10 | Low | PROPOSE FIX | Step 0d, run by the operator (the agent cannot read the list). |
| Published topology is an accepted residual | security SEC-11 | Low | ESCALATE | The operator decides whether `infra/*` stays public. D-094's residual names it. |
| The rewrite is riskier than rotation for the infra values | zoom-out | Major | DECLINE (partly) | The addresses and the user are rewritten safely with bounded rules, and the tip-tree check proves it. The port is rotated, as proposed. |
| "Temporary private" not weighed; fresh-repository rejection is weak | zoom-out, security | Major | PROPOSE FIX | Step 0a. §4 restates the fresh-repository trade-off. |
| SEC-12 shared zones depend on `gaos.conf` | security | Low | ESCALATE | Part of the N6 file split. |

No Blocker survives v2. The policy does not require a second round. The design goes to the operator, with the decisions that are theirs: 0a, 0b, the private-list additions, the port rotation, and the `infra/*` residual.
