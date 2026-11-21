# open-source-docs — review round 2, triage

Design reviewed: `docs/designs/open-source-docs.md` (v2). Three lenses — critic, readiness, security — the ones that raised round 1's Blockers and Critical, run in parallel with the document path and the rubric path list only. Reports: `open-source-docs-critic-2.md`, `open-source-docs-readiness-2.md`, `open-source-docs-security-review-2.md`.

Totals: critic B=0 M=7 N=8; readiness B=0 M=6 N=7; security Critical=0 High=1 Medium=4 Low=6.

## Isolation

- The round-1 canaries stayed planted for this round. No canary value reached any report.
- All three lenses again reported `.claude/CLAUDE.md` attached unasked when they read a file under `.claude/` (round 1's SEC-3, escalated). None used it.
- Canaries removed after this round.

## Triage

| Finding | Lens | Severity | Disposition | Note |
|---|---|---|---|---|
| SEC-1 deny rules do not reach the main checkout's list | security | High | PROPOSE FIX | `Read(//**/deploy/local/**)` — an absolute glob — and the rule text "in this checkout and in any other checkout, the main one included"; verified in phase 1 by a Read from an agent worktree of the main checkout's list, result appended below |
| Deny rules miss the canonical location; narrower than the rule | critic | Major | PROPOSE FIX (anchoring, scope) / DECLINE (move the list to `~/.config`) | The absolute glob covers every checkout and the whole of `deploy/local/`, so rule and setting agree. The brief asks for "git-ignored files and folders" in the repository; a list in `~/.config` is outside that and outside the tree the operator copies |
| Phase 1 exit count vs the guard's contract | readiness, critic | Major | PROPOSE FIX | baseline 179 content FAIL lines + 7 path FAIL lines; §1.1 "matching lines", "seven tracked paths" |
| Masking fails open | readiness, critic, SEC-3 | Major / Medium | PROPOSE FIX | no masking: a path that matches is shown as `path #<k> (<8 hex SHA-1>)`, tested with the same `grep -i -E -f` |
| TC-510 branches unverified | readiness | Major | PROPOSE FIX | phase 1 runs each branch once via `GAOS_FORBIDDEN_STRINGS` and pipes every run's output through the list |
| BRD OQ range and total | readiness | Major | PROPOSE FIX | `brd.html:1658,1670-1671` in §3.7 and §7 |
| Title/Sub unspecified; TC-512 vacuous | readiness, critic | Major | PROPOSE FIX | exact title and sub; `<section id="boIntegrations">`; TC-511 asserts `Integrations</h2>` and checks facts inside the section; TC-512's unsigned requests use the TC-511 fixture |
| Archive no-store untested | readiness | Major | PROPOSE FIX | TC-511 asserts it on both archive routes |
| Edge template: default server, map, transition | critic, SEC-5 | Major / Medium | PROPOSE FIX | `gaos-<label>.conf`; explicit Host 444; no `map`, no `default_server`; `nginx -T` conflict and default checks; `curl --resolve` |
| Append-only exception not in the policy | critic, readiness | Major / Nit | PROPOSE FIX | the sentence in `docs/rules/gaos-design-review.md:71`; D-085 names the round-1 redaction as its first use; SEC-11's mechanical limit in phase 3 |
| List not written before implementation | critic | Major | PROPOSE FIX | §10 step 0; phase 1's first command |
| SEC-2 private name in `deploy/local/` names | security | Medium | PROPOSE FIX | neutral labels (`instance-2`, `private`) |
| SEC-4 untracked files not scanned | security | Medium | PROPOSE FIX | `git grep --untracked`; `--others --exclude-standard` for paths |
| SEC-6 hand-off location | security | Low | PROPOSE FIX | only in the session, or under `deploy/local/`; never under `docs/` |
| SEC-7 first-readable fails open | security | Low | PROPOSE FIX | first non-empty; a set-but-unusable env variable fails; the source named |
| SEC-8 binaries | security | Low | PROPOSE FIX (one-off) | `strings` over tracked images in phase 3; no script change |
| SEC-9 `seed.sh` cleartext default | security | Low | PROPOSE FIX | `--base` defaults to `http://127.0.0.1:8083/api/v1`; instances pass it explicitly |
| SEC-10 structural hint in the glossary | security | Low | PROPOSE FIX | reworded. The VM name under N6 stays: the owner's own infrastructure names are outside the brief |
| SEC-11 exception without a mechanical limit | security | Low | PROPOSE FIX | phase 3: every removed line of an earlier report matched a pattern |
| Steps in two phases; ignore first | readiness, critic | Nit | PROPOSE FIX | step 1 is "the ignore rule is already in place" |
| `bootChecked` in `run` | readiness, critic | Nit | PROPOSE FIX | `serve`, set in the literal at `main.go:294` |
| Signatures | readiness | Nit | PROPOSE FIX | stated (`buildIntegrations` fills the `BOView`, like `buildInbox`) |
| TC-511 external item | readiness | Nit | PROPOSE FIX | asked with `tokQ` on `p-1` through the route; answered through the store; `RunOnce` |
| `/base` fixed needle | readiness | Nit | PROPOSE FIX | path sentinel |
| qa-sync anchor; template check | readiness | Nit | PROPOSE FIX | `#inbox-clients`; phase 3 greps |
| Line anchors | critic | Nit | PROPOSE FIX | re-anchored where the text still cites them |
| Delivery reason wording | critic | Nit | PROPOSE FIX | "GAOS's sentence plus at most 100 characters of a transport error" |
| A6 clone target | critic | Nit | PROPOSE FIX | `deploy/local/private/` |
| Historical seed path wording | critic | Nit | PROPOSE FIX | "the sample product's seed directory (moved to `deploy/local/`, D-085)" |
| Vendor query | critic | Nit | PROPOSE FIX | in the per-call template |
| README env subset | critic | Nit | PROPOSE FIX | listed; the table links to the full list |
| TC-511 common words | critic | Nit | PROPOSE FIX | facts checked inside the section |
| Docs link assumption | critic | Nit | PROPOSE FIX | stated in the template comment |

No Blocker and no Critical or High is open after these fixes; round 2's High is closed by its own remediation, verified in phase 1 (below). The design goes to **Approved (v3)** by delegation (§11).

## Verification of SEC-1 (phase 1)

`.claude/settings.json` gained `Read(//**/deploy/local/**)` and the rule text in `.claude/rules/agent-workspace.md` was extended. A zero-context agent spawned from the implementing session was then told to Read the worktree's `deploy/local/forbidden-strings` with the Read tool: it reported **READ** (six lines; it quoted none). The implementing session loaded its permissions when it started, before the rule existed, so this run cannot show whether the rule refuses. **The verification is open**, not passed: the operator repeats the probe from a session started after the merge (and from an agent worktree against the main checkout's list). If it is still read, the fallback is the critic's alternative — keep the list outside every checkout — and the finding is reopened.
