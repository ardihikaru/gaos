# open-source-docs — review round 1, triage

Design reviewed: `docs/designs/open-source-docs.md` (draft v1). Five lenses, run in parallel with the document path and the rubric path list only: comprehension, zoom-out, critic, readiness, security (required: the design touches tokens, credential configuration and the back-office surface). Reports: `open-source-docs-comprehension-1.md`, `open-source-docs-zoomout-1.md`, `open-source-docs-critic-1.md`, `open-source-docs-readiness-1.md`, `open-source-docs-security-review-1.md`.

Totals: comprehension B=0 M=4 N=8; zoom-out B=0 M=7 N=6; critic B=1 M=10 N=10; readiness B=2 M=7 N=10; security Critical=1 High=1 Medium=5 Low=4.

## Isolation

- Canaries were planted in `.claude/CLAUDE.md`, `AGENTS.md`, `GEMINI.md` and the four `.claude/rules/*.md` files before the run. No canary value reached any report (`grep -rn "EGM-MARKER-" docs/designs/reviews/` finds only the prefix in older triage prose).
- Positive control: one agent with no exclusion was told to read `.claude/rules/rule-index.md` and quote its trailing HTML comments; it quoted the planted rule-index canary. The probe can detect a leak.
- **Limit, stated.** All four general lenses and the security lens reported that the harness attached `.claude/CLAUDE.md` to their context unasked when they read a file under `.claude/`. None used it and no canary reached a report. The probe proves "the canary did not reach a report", not "the file was not seen" — the limit `docs/rules/gaos-design-review.md` already states. SEC-3 is the finding.
- Canaries removed after the run; `./deploy/check-workspace.sh` §5 confirms.

## Triage

| Finding | Lens | Severity | Disposition | Note |
|---|---|---|---|---|
| `git mv` into an ignored dir leaves the seed tracked | readiness, critic, zoom-out, comprehension | Blocker (readiness), Major (others) | PROPOSE FIX | §3.2.2: plain `mv`, `git rm --cached`, `git mv` only for `seed.sh`, then `.gitignore`; acceptance `git ls-files deploy/local` = README only; the guard asserts it (§3.3) |
| No way to verify G1 in the implementing worktree | readiness, critic | Blocker | PROPOSE FIX | §3.3: the list is found via `$GAOS_FORBIDDEN_STRINGS`, `deploy/local/`, or the main worktree's `deploy/local/` (`git rev-parse --git-common-dir`); §12 phase 1 exit: section 7 prints FAIL lines for the baseline, not the skip line; phase 3 exit: `ok … (4 pattern(s))` |
| SEC-1 history retrievable while the change publicises the repository | security | Critical | PROPOSE FIX | The repository is **private** (`gh repo view --json visibility` → `PRIVATE`, 2026-11-21), stated in §1. OQ-36 opened; D-085 records "not public until OQ-36 is answered"; metadata does not change visibility; no tracked pointer into history at the private content (§3.2.4); §9 adds forks, cached refs, archives and the VM worktrees. Closed by its own remediation |
| "History decided in D-085 yet owner's call"; "repository already public" | critic | Major | PROPOSE FIX (OQ-36) / CONTEST (public) | OQ-36 as recommended. The "already public" premise is checkable and false: `gh repo view --json visibility` → `PRIVATE` |
| Root problem deferred without stating visibility | zoom-out | Major | PROPOSE FIX | §1 states visibility; N1 → OQ-36 |
| SEC-2 reviewers can read `deploy/local/` | security | High | PROPOSE FIX | §3.2.1: deny-list in `agent-workspace.md` and two Read deny rules in `.claude/settings.json` (Read rules bind Grep and Glob). The "patterns the rule lists but the tooling omits" are pre-existing and out of this change's scope: ESCALATE that half |
| SEC-3 isolation not enforced | security | Medium | ESCALATE | Recorded above as the stated limit; moving the rubric out of `.claude/` is a change to the design-gate workflow itself, not to this feature |
| SEC-4 the guard fails open | security, critic (xargs, Major) | Medium / Major | PROPOSE FIX | `git grep` over a `mktemp` pattern file (no xargs batching), exit 2 = FAIL, main-worktree fallback. The pre-commit hook: DECLINE — D-025 makes the owner the runner and the repository has no hooks; the fallback closes the case the hook would. Index vs working tree: `git grep` without `--cached` scans the tracked files' working copy, which is what the owner runs before committing; DECLINE `--cached` |
| SEC-5 the path FAIL prints the string | security | Low | PROPOSE FIX | §3.3: paths are printed lower-cased with every match masked `***` |
| SEC-6 edge block loses ACME and review | security, critic, readiness | Medium / Major | PROPOSE FIX | §3.2.3: exact lines; tracked placeholder `deploy/edge/instance.conf.example` with `:80` ACME + redirect and `:443` with no `$gaos_backend` check; `deploy/edge/README.md` "A second instance" with a renewal dry-run |
| SEC-7 / sheet route without `no-store` | security, critic, zoom-out, readiness | Low / Major / Nit | PROPOSE FIX | `noStoreView` in both handlers for `archive` and `integrations`; TC-511 and TC-512 cover the sheet route |
| SEC-8 weak no-secret assertions; free-text fields; `urlHost` | security | Medium | PROPOSE FIX | random-hex sentinels; the provider list never renders a gateway error (non-blocking cache read); last turn renders no event text; the delivery reason comes from a real failed delivery against an `httptest` server whose body sentinel is asserted absent; strict `urlHost` with two refusal cases |
| Rendered error strings (provider error, delivery reason) | readiness | Major | PROPOSE FIX / CONTEST (half) | The provider error is no longer rendered. The delivery reason does **not** carry the URL: `qaOutbound.do` unwraps `url.Error` to its inner error before building the reason (`dashboard/qaclient.go:171-178`) — checkable; TC-511 now drives a real failed delivery and asserts `/base` absent |
| SEC-9 every account sees the topology | security | Low | PROPOSE FIX | recorded in D-085 and §3.6 |
| SEC-10 new pages not covered by the secret scan | security | Low | PROPOSE FIX (placeholders) / DECLINE (scan) | Placeholders `gaos_<32+ random characters>` fail `validToken`; minted with `openssl rand`; no tunnel addresses. Extending the §6 scan to `pustaka/docs/` would amend TC-357/TC-437 for pages this change authors by construction; §12 phase 5 runs a one-off `grep` instead |
| SEC-11 identifying detail in the design | security | Medium | PROPOSE FIX | sizes, case count and sector removed from the design; the round-1 reports redact the sizes and the count |
| TC-511 fixture refused by `validQaURL` | zoom-out, comprehension, critic, readiness | Major | PROPOSE FIX | `httptest` server URL + `/base`; host shown, `/base` asserted absent |
| Source of the AIM host / Configured / the fake | comprehension, readiness, critic | Major / Nit | PROPOSE FIX | §3.6 table: `urlHost(a.Client.Base)` when `s.adapter` is an `*AIMAdapter`; a real `*AIMAdapter` in TC-511 |
| nil `api` / `qaOut` undefined | readiness, comprehension, critic | Major / Nit | PROPOSE FIX | §3.6 table "When unset" column; TC-512 uses `newTestServer`'s nil fields |
| Override file application unstated | comprehension | Major | PROPOSE FIX | §3.2.3: `set -a; . deploy/local/instances/<instance>.env; set +a; ./deploy/rebuild-restart.sh …`; the manual-probe note kept with placeholders |
| `seed.sh` token path and prod default | critic, zoom-out | Major | PROPOSE FIX | `root=$here/../..`; `--dir` required with its FAIL line |
| TC-407 depends on ignored files / under-specified | zoom-out, readiness, critic | Major / Nit | PROPOSE FIX | the example only; exact new messages; count literals removed |
| README counts re-pinned | zoom-out | Major | PROPOSE FIX | `readme_check` compares with `$go_tests`, `$qa_rows` and `product.semver` |
| No phases or exit commands | readiness | Major | PROPOSE FIX | §12 |
| Change list by description | readiness | Major | PROPOSE FIX | §7 lists the 39 files and every new or changed file by path |
| Provider list not cached on failure | critic, comprehension, readiness | Major / Nit | PROPOSE FIX | the view never calls the gateway: `CachedVendorInfo` reads the cache with `TryLock`; the boot check is a recorded time |
| Gave-up count | critic | Major | PROPOSE FIX | from the existing event scan, latest outcome per item |
| OQ-25 grantor erased; misquote | critic, comprehension | Major / Nit | PROPOSE FIX (misquote, record) / ESCALATE (grantor) | The quote is corrected. The brief says never mention the organisation; D-085 records the anonymised grantor as accepted knowingly, and the report raises it to the owner as an open item |
| One release vs two | zoom-out | Major | DECLINE | The brief asks for all four together and the view links to the pages; a split adds a release and a register round (N7) |
| Guard reimplements `git grep` | zoom-out | Nit | PROPOSE FIX | §3.3 is now a contract; `git grep` |
| A6 private repository | zoom-out | Nit | PROPOSE FIX | A6 and `deploy/local/README.md` |
| View-model detail; `Token` constant | zoom-out | Nit | PROPOSE FIX (table, drop `Token` field) | §3.6 is a table; "configured" is rendered for every admitted client |
| BO tutorials duplicate docs | zoom-out | Nit | DECLINE | The brief asks for "a short setup tutorial per integration, linking to the docs pages"; capped at four steps naming variables only |
| Line references drifted | all | Nit | PROPOSE FIX | re-anchored |
| "Eight variables" | comprehension, critic, readiness | Nit | PROPOSE FIX | ten |
| 12/5/9 wording | comprehension, readiness | Nit | PROPOSE FIX | "9 planned, 5 of which implemented; 3 other decisions" |
| TC-406 message changes | comprehension | Nit | PROPOSE FIX | stated |
| Append-only exception not in D-085 | comprehension, readiness, critic | Nit | PROPOSE FIX | D-085 and one sentence in `docs/designs/README.md` |
| Undefined terms | comprehension | Nit | PROPOSE FIX | glossary: ai-claude-cli, ai-manager's own docs; the four paths named |
| `qa-test-plan.md` lines 73-81 | readiness | Nit | PROPOSE FIX | §3.7 |
| Example seed `source`, `--dir` resolution | readiness | Nit | PROPOSE FIX | no commit; resolved against the current directory |
| README "not built" claim | readiness | Nit | PROPOSE FIX | reworded |
| BR-53 cited wrongly | critic, readiness | Nit | PROPOSE FIX | BR-52 and BR-57 |
| `bo-main` branch | critic, readiness | Nit | PROPOSE FIX | named |
| Rollback re-tracks private files | critic | Nit | PROPOSE FIX | §10 forward fix |
| ai-manager claims unreferenced | critic | Nit | PROPOSE FIX | labelled "per ai-manager's docs" |
| `/break-ui` pass not named | critic | Nit | PROPOSE FIX | named with its worst-case data |

Every Blocker and the Critical are closed by `PROPOSE FIX` in v2. Round 2 re-runs the critic, readiness and security lenses on v2.
