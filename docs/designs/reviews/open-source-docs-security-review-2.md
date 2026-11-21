# open-source-docs — security lens, round 2

Design reviewed: `docs/designs/open-source-docs.md` (v2). Lens: `gaos-goldfish-security`. Saved with light condensation of the prose; every finding, evidence, severity and remediation is kept. The harness flagged instruction-shaped text in the raw output; none was acted on.

**Scope notes.** Reading `.claude/rules/qa-test-plan.md` and `.claude/rules/agent-workspace.md` attached `.claude/CLAUDE.md` to my context unasked (round 1's SEC-3 again, escalated there); nothing here relies on it. I read `deploy/edge/gaos.conf:1-136` to check line references and cite private lines by number only. `signedIn()` checks the cookie's signature (`dashboard/auth.go:361-382`, BR-55) and `gate` (`auth.go:551-560`) puts every back-office route behind it. `/docs/` sits behind pustaka's login layer (`deploy/stacks/compose.prod.yaml:164-168`), so the new pages are not public.

## Paraphrase

1. Reword every private string with a fixed vocabulary; two designs renamed; register IDs and history stay; history is OQ-36 and the repository stays private until it is answered.
2. Move the customer's seed, the second instance's edge blocks and overrides, and the list into git-ignored `deploy/local/`, denied to reviewers; an invented seed and a placeholder edge template replace them; `seed.sh` becomes generic with a required `--dir`.
3. Section 7 of `check-workspace.sh` reads the list from an env variable, `deploy/local/` or the main worktree's copy; fails on any match in a tracked file's content or path, masking the path; fails if the list or anything else under `deploy/local/` is tracked. The README checks compare with computed values.
4. Rewrite the README, set the description and topics (still private), three login-gated docs pages.
5. A read-only *Integrations* view on the two view routes, `no-store`, visible to every account; never a token, digest, credential, URL path or query, or gateway error; TC-511 and TC-512 with random sentinels.

## Trust boundaries

| Boundary | Authenticated by | Authorised by | Validated by | If the far side fails |
|---|---|---|---|---|
| Browser → `/ui/bo/integrations`, `/ui/bo?view=integrations` | signed session cookie (`auth.go:366-382`) | any account (D-085 records it) | `html/template`; `urlHost`; `noStoreView` | 401, or the login sheet |
| Reviewer agent → `deploy/local/` (worktree and main checkout) | — | `.claude/settings.json` Read deny rules plus instruction | — | the private list enters a report |
| Repository → operator's terminal (guard output) | — | — | masking (§3.3) | the private string is printed |
| Public edge → second instance (`instance.conf.example`) | its own certificate | nothing: the Host check is removed | — | answers unknown Hosts if it becomes the default server |
| `seed.sh` → GAOS API | admin bearer token | admin | — | the token is sent in cleartext to the default address |

No new weakness in the view itself: `describeAdapter` carries no URL (`adapter_aimanager.go:51-52`); the delivery reason is GAOS's own text (`inbox_worker.go:169`, `qaclient.go:174-179`); `VendorInfo` carries no error text (`vendors.go:255-260`).

## Findings

### SEC-1 The Read deny rules are not shown to cover the main checkout's list, the only copy an agent worktree can reach - Severity: High
- Component: reviewer tooling deny-list
- Evidence: design `:132` (`**/deploy/local/*/**`, `**/deploy/local/forbidden-strings`); `:179` (the list lives in the main checkout, an ancestor of `.claude/worktrees/<agent>/`); `.claude/settings.json:3-8`; `.claude/rules/agent-workspace.md:109`.
- Attack path: a reviewer in a worktree follows the documented fallback to the main checkout's list and reads it into a committed report. Nothing shows that a pattern with no `//` prefix matches outside the session's directory. High: the file is a complete inventory of the strings this change removes.
- Remediation: an absolute-anchored deny (`Read(//**/deploy/local/**)`) or an explicit `//<main-checkout>/deploy/local/**` in untracked local settings; a positive control from an agent worktree that must be refused, recorded in the triage.
- Effort: quick-win

### SEC-2 The private name is kept in directory and file names under `deploy/local/` - Severity: Medium
- Evidence: design `:125-127`, `:138`, `:149`, `:167`, `:168`, `:173`.
- Attack path: Read rules guard contents; a directory listing, `git status --ignored`, a transcript, `seed.sh`'s `FAIL … in <dir>` or the edge host's `conf.d` listing shows the path, which is the private string.
- Remediation: neutral names (`seed/private/`, `edge/instance-2.conf`, `instances/instance-2.env`, `conf.d/gaos-instance-2.conf`); the README says so.
- Effort: quick-win

### SEC-3 Path masking uses a third regex engine with different case handling, and fails open - Severity: Medium
- Evidence: design `:184-186` (grep `-i -E` matches; awk masks against the lower-cased path; the patterns are not lower-cased).
- Attack path: an upper-case literal or an interval expression matches in grep but not in awk; the FAIL line prints the path unmasked.
- Remediation: lower-case the patterns too; re-test the masked string with `grep -i -E -f`, and if it still matches print `<path withheld>` and a short hash; TC-510 negatives with an upper-case and an interval pattern.
- Effort: small

### SEC-4 The guard does not scan untracked files, where a fresh report or page sits - Severity: Medium
- Evidence: design `:184` (`git grep` covers tracked files only), `:185`, `:103`.
- Attack path: a private string written into a new, untracked report passes the guard before `git add`.
- Remediation: `git grep --untracked`; `git ls-files --cached --others --exclude-standard` for paths; a TC-510 case with an untracked match.
- Effort: quick-win

### SEC-5 The second-instance edge template has no Host check and no file-name constraint - Severity: Medium
- Evidence: design `:166`, `:167`; `deploy/edge/gaos.conf:9-13` (a duplicate `map` stops nginx for every product), `:17-19` (no `default_server`; the first file alphabetically is the implicit default), `:46-47`.
- Attack path: a file name that sorts first makes its `:443` block the default — unknown SNI gets the instance's certificate (naming the private domain) and any Host is proxied; a copied `map` fails `nginx -t`.
- Remediation: installed name `gaos-<label>.conf`; an explicit 444 for any Host other than its `server_name`; no `map`; a README check that `nginx -T | grep default_server` is unchanged.
- Effort: small

### SEC-6 The hand-off carries private paths and values, but where it goes is not specified - Severity: Low
- Evidence: design `:173`, `:281`, `:395`.
- Remediation: delivered only in the session, or saved under `deploy/local/`, never under `docs/`.
- Effort: quick-win

### SEC-7 Choosing the list by "first readable" fails open to a skip - Severity: Low
- Evidence: design `:179-180`.
- Attack path: an empty `GAOS_FORBIDDEN_STRINGS` file or a stale empty local list is taken; the run skips and the main list is never consulted.
- Remediation: the first non-empty list after filtering; an env variable set but unreadable or empty fails; name the source in the `ok` and skip lines.
- Effort: quick-win

### SEC-8 Tracked binary files are not scanned - Severity: Low
- Evidence: design `:184` (`-I`); tracked images under `landing-page/img/`.
- Remediation: an `info` line with the number skipped; a one-off scan of image metadata before OQ-36 is answered.
- Effort: quick-win

### SEC-9 The generic `seed.sh` sends the admin token in cleartext to the owner's tunnel address by default - Severity: Low
- Evidence: design `:147`, `:150`, `:155`.
- Attack path: an outside user runs `--dir` without `--base`; the bearer goes in cleartext to whatever answers at that private address on their network.
- Remediation: `--base` required, or defaulting to the local stack; refuse plain http except loopback or tunnel, as `validQaURL` does (`dashboard/qaclient.go:79-90`); update TC-411.
- Effort: small

### SEC-10 The design still describes the organisation's name by its structure - Severity: Low
- Evidence: design `:13`; the second instance's VM stays named in `deploy/stacks/compose.prod.yaml:151-152` under N6.
- Remediation: the glossary says only "the organisation's name"; decide whether the VM name under N6 is acceptable.
- Effort: quick-win

### SEC-11 The exception to append-only reports has no mechanical limit - Severity: Low
- Evidence: design `:101`, `:271`.
- Remediation: in phase 3's exit, every removed line of the earlier review reports matched a pattern (`git diff -U0` piped through `grep -i -E -f`).
- Effort: small

Counts: Critical=0 High=1 Medium=4 Low=6
