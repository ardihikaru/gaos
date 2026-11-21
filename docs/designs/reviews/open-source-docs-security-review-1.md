# open-source-docs — security lens, round 1

Design reviewed: `docs/designs/open-source-docs.md` (v1). Lens: `gaos-goldfish-security`. Saved with light condensation of the prose; every finding, evidence, severity and remediation is kept. The harness flagged instruction-shaped text in the raw output; none was acted on.

**Scope notes.** When I read `.claude/rules/agent-workspace.md`, the harness attached the contents of `.claude/CLAUDE.md` to the result, although this agent sets `omitClaudeMd: true`. I did not open that file and nothing here relies on it (SEC-3). While checking line numbers I read the current edge config, which holds private strings; this report never writes them.

## Paraphrase

1. Scrub the tracked files (179 matches in 39 files, plus two file names) with a fixed vocabulary; register IDs and git history stay.
2. Move the customer's seed, the second instance's edge block and redeploy values, and a forbidden-strings list to a new git-ignored `deploy/local/`; an invented "Sample Shop" replaces the seed; `seed.sh` becomes generic with `--dir`.
3. A new section 7 in `deploy/check-workspace.sh` fails when a tracked file's content or path matches a pattern in the ignored list; it skips when the list is absent.
4. Rewrite the README, set the GitHub description and topics, add three docs pages.
5. A read-only back-office *Integrations* view on `GET /ui/bo/{view}` that never renders a token, a digest, a credential, URL userinfo, a path or a query (TC-511, TC-512).

## Trust boundaries the design touches

| Boundary | Authenticated by | Authorised by | Validated by | If the far side fails |
|---|---|---|---|---|
| Browser → back office (`/ui/bo/integrations`, and `/ui/bo?view=`) | MAC-verified session cookie (`auth.go:361-382`, BR-55) | any account in the users file; no roles | `html/template` escaping | 401 or the login sheet |
| Repository → public (history, metadata, docs) | — | repository visibility | the new guard (advisory) | the data stays in history |
| Repository → host or edge (`deploy/local/edge/*.conf`) | host file permissions | — | nothing (`check-stacks.sh` has no edge check) | silent loss of certificate renewal |
| Reviewer agents → repository files | — | `.claude/settings.json:3-8` plus instructions | — | a private string ends up in an append-only report |

## Findings

### SEC-1 The customer's real catalog stays retrievable from history while the change publicises the repository - Severity: Critical
- Component: repository, git history, GitHub metadata
- Evidence: design `:32` (the seed is the customer's real catalog), `:69` (N1), `:143` and `:368` (point at the commit and paths that hold the material), `:203` (description and 12 topics), `:290` (D-085 "history is not rewritten"), `:364` (§9).
- Attack path: anyone who can read the repository can fetch `c407f11` or any tag up to v0.44.0 and recover the catalog, the instance domain and the second instance's tunnel address and port; after this change G1 reads as done while topics make the repository easier to find and the design names the commit to look in. I could not tell from the repository whether it is public today; if it is, this is a live exposure. Critical only if the repository is reachable by people outside the operator.
- Remediation: make the visibility decision a precondition of §3.4 (no topics or description until the owner decides on a rewrite, or keeps the repository private and asks the data's owner); restore the private files from a private backup, not `c407f11:`; remove the commit-and-path pointers at `:143` and `:368` from tracked text; have D-085 state plainly that the data is still in history, if true; if rewritten, add forks, cached PR refs, release archives and the VM's `/.worktrees/` to §9.
- Effort: structural

### SEC-2 Reviewer agents can read `deploy/local/`, including the list of private strings - Severity: High
- Component: reviewer deny-list and agent tooling
- Evidence: `.claude/rules/agent-workspace.md:101-109` (no `deploy/local/`); `.claude/settings.json:3-8` (denies only `Read` of `*.bo-*`, `*.api-*`, `*.aim-*`, `*.env`); `.claude/agents/gaos-goldfish-security.md:4` (`Read, Grep, Glob`); design `:121-124`.
- Attack path: a reviewer checking a claim opens `deploy/local/forbidden-strings` or a private `cases.json` and quotes it into a committed, append-only report; the guard catches it only on the owner's checkout, by hand, after the fact.
- Remediation: add `deploy/local/**` to the deny-list and to `.claude/settings.json` (and the patterns the rule lists but the tooling omits); confirm the deny binds Grep and Glob; list both files in §7; extend `check-workspace.sh` so the section-7 scan covers `docs/designs/reviews/`.
- Effort: quick-win

### SEC-3 Reviewer isolation is not enforced: an off-limits memory file entered this review's context - Severity: Medium
- Component: reviewer isolation
- Evidence: `.claude/agents/gaos-goldfish-security.md:6` sets `omitClaudeMd: true`; reading a file under `.claude/rules/` attached `.claude/CLAUDE.md`, a file on the deny-list (`.claude/rules/agent-workspace.md:104-105`); `.claude/settings.json:3-8` has no rule to stop it.
- Attack path: no attacker; the "zero context" claim does not hold for any reviewer that reads the rubric under `.claude/`.
- Remediation: record that isolation is instructional only; move the rubric out of `.claude/` or serve it from a path that does not trigger directory memory; keep the canary check as the verification.
- Effort: small

### SEC-4 The guard fails open in three ways - Severity: Medium
- Component: `deploy/check-workspace.sh` §7 (TC-510)
- Evidence: design `:173`, `:161`, `:190`, `:300`.
- Attack path: (1) `xargs -0 grep … -f <(printf …)` hands every grep run the same pipe; past the first batch the pattern file is empty and the run prints `ok`; (2) a missing list skips with exit 0, and ignored files are not copied into `git worktree add` checkouts — the guard skips exactly where changes are authored; (3) it reads working-tree files, not the index.
- Remediation: a `mktemp` pattern file (mode 600, `trap`) and `git grep --cached -I -i -n -E -f "$tmp"`, exit 2 = FAIL; find the list through the main worktree (`git rev-parse --path-format=absolute --git-common-dir`/..) or `GAOS_FORBIDDEN_STRINGS`; offer a pre-commit or pre-push hook that fails when the list is missing; add a TC-510 case with more files than one `xargs` batch.
- Effort: small

### SEC-5 The path FAIL line prints the private string - Severity: Low
- Component: `check-workspace.sh` §7
- Evidence: design `:175` prints `$p`, the tracked path containing the private string; contradicts `:185`.
- Remediation: print the path with the match masked, or an index plus a hash; update TC-510's string.
- Effort: quick-win

### SEC-6 The second instance's edge block leaves review and can lose its certificate-renewal block - Severity: Medium
- Component: edge
- Evidence: design `:137`; `deploy/edge/gaos.conf:21-23` (rule 4), `:28-41`, `:71`, `:76`, `:94-97`; `deploy/check-stacks.sh` has no edge check.
- Attack path: a private file without an `:80` ACME block fails renewal in about 60 days; TLS versions, body limit and X-Forwarded-For handling are copied by hand with no review.
- Remediation: a tracked placeholder template (`deploy/edge/instance.conf.example`) with the `:80` ACME block, TLS settings, `client_max_body_size` and header handling; `deploy/local/README.md` requires it; a §3.2.4 step: dry-run renewal before reinstalling `gaos.conf`.
- Effort: small

### SEC-7 `Cache-Control: no-store` can be bypassed through the public sheet route - Severity: Low
- Evidence: design `:230`; `dashboard/main.go:359`, `dashboard/handlers_bo.go:129-138`; `templates/backoffice.html:45,109,123`; the same gap exists for `archive` (`handlers_bo.go:148-150`).
- Remediation: set the header from the view name in both handlers; add the sheet path to TC-512, signed out and signed in, and assert the header.
- Effort: quick-win

### SEC-8 TC-511's "no secret" assertions are weak and miss the free-text fields - Severity: Medium
- Evidence: design `:284` (short needles `login`, `pw`, `/x`, `x=1`); `:249`, `:257` render `ProviderErr` and the delivery reason, which carry upstream text (`dashboard/aimanager.go:203-224`, `inbox_worker.go:138-139`, `qaclient.go:174-179`); `:278` `urlHost` trusts `url.Parse` — a password with an unescaped `/`, `?` or `#` ends the host early and the parsed host can include the login.
- Remediation: random-hex sentinels for every token, login, password, path and query; fake endpoints return errors containing a sentinel, asserted absent; `urlHost` returns `""` unless the scheme is http/https and `Hostname()` parses as a host name or IP; a malformed-URL case.
- Effort: small

### SEC-9 Every back-office account gains a view of the integration topology - Severity: Low
- Evidence: `dashboard/auth.go:168-286` (no roles); BR-55; design `:243-271`.
- Remediation: record in D-085 that the view is visible to every account.
- Effort: quick-win (to document)

### SEC-10 New published pages are not covered by the secret scan - Severity: Low
- Evidence: design `:215`, `:216`, `:220`; `deploy/check-workspace.sh:199-210` (scans only `docs/designs/reviews/`); `dashboard/api_v1.go:83-95` (`validToken`).
- Remediation: placeholders that fail `validToken`; tokens minted with a CSPRNG; `gaos.example.org` / `qa.example.org` with no tunnel addresses; extend the secret scan to `pustaka/docs/`.
- Effort: quick-win

### SEC-11 The design itself carries identifying detail the patterns cannot catch - Severity: Medium
- Evidence: design `:358` (R1 names the customer's sector); `:32` (the catalog's sizes and case count); `:133` says such counts do not belong in a tracked test.
- Remediation: reword R1 without a sector; drop the sizes and the count from §1.1; apply the same rule to this feature's review reports.
- Effort: quick-win

Counts: Critical=1 High=1 Medium=5 Low=4
