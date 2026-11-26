# pustaka-loading-and-motion — review round 1, triage

**Design reviewed:** v1. **Lenses:**
- comprehension: B0 M5 N5
- zoom-out: B0 M4 N6
- critic: B1 M5 N9
- readiness: B1 M6 N7
- security: Critical 1, High 2, Medium 3, Low 4

**Isolation:**
- Canaries were planted in the seven standing files before the run. The files were restored with `git checkout` after it.
- No canary value reached any report (`grep -c EGM-MARKER` = 0 on each saved report).
- Positive control: a `general-purpose` agent confirmed the `.claude/CLAUDE.md` sentence was in its context, and quoted the canary it read from `.claude/rules/rule-index.md`.
- Three lenses reported the harness attaching `.claude/rules/qa-test-plan.md` without being asked. That file is the rubric, as expected.

Findings that several lenses raised are merged into one row and attributed to all of them.

| Finding | Lens | Severity | Disposition | Note |
|---|---|---|---|---|
| Change list misses `check-workspace.sh`, `README.md`, the QA rule file; §7 would fail | critic, readiness, zoom-out, comprehension | Blocker / Major | PROPOSE FIX | §3.8, §6 rows 15-17 |
| Injected page keeps the file's `Last-Modified`, so 304 serves a cached copy without the snippet | zoom-out, comprehension, critic, readiness | Major | PROPOSE FIX | §3.4 `servedModTime`. Fixed upstream as v0.9.1 (`1ddec13`); TC-721 gains a revalidation case |
| Two sources of truth (copy vs. spec), upstream line numbers, unpinned upstream | zoom-out, comprehension, critic, readiness | Major | PROPOSE FIX | §3.1 pins `1ddec13` as authoritative, A1/A2 labelled, fork lines re-cited |
| `.pk-progress` / `.pk-enter` CSS unspecified; reduced-motion behaviour of the bar | comprehension, readiness, critic | Major | PROPOSE FIX | §3.2 gives exact rules and reduced-motion behaviour |
| G1 "nothing moves" is false (`.page-meta`, anchors, pn, foot); §7 measures the wrong thing | critic | Major | PROPOSE FIX | G1 narrowed to the column and the header strip; §7 metric defined |
| G4 contradicts its own numbers; motion bar not readable by reviewers | critic, comprehension, zoom-out | Major | PROPOSE FIX | Rules quoted inline; `.rv` 280 ms; exemptions stated |
| Motion has no defect behind it; split it out | zoom-out | Major | DECLINE | The operator asked for it explicitly. It lives in the same two files, and the sync is a whole-file copy (§1.2) |
| Public switch under-specified: dev tier, stale prose, credentials left live | critic, readiness, security SEC-8, SEC-9 | Major / Low | PROPOSE FIX | §3.6: credentials commented out; N7 (dev retired); `STACKS.md`, `environments.html` and compose comment rewritten |
| R4 overstates D-085 / section 7 | comprehension, critic, security SEC-2 | Major / High | PROPOSE FIX | R4 restated; §7 runs section 7 with the list present |
| SEC-1: public docs publish network topology | security | Critical | ESCALATE | The repository is already public (`gh repo view` → PUBLIC, 2026-11-26), so the same lines are already on GitHub. Redaction of `infra/*` is a separate change for the operator. Recorded in D-093 Residual |
| SEC-3: OQ-36 gate bypassed | security, comprehension | High | ESCALATE | The repository was made public outside the register. Answering OQ-36 is the operator's call; §1.3 says so |
| SEC-4: stale "back office checks nothing" prose | security | Medium | ESCALATE | Separate docs correction |
| SEC-5: anonymous resource use, no limits, per-request buffering | security, critic (R2) | Medium / Nit | ESCALATE | N6. R2 restated with the real cost |
| SEC-6: same-origin inline script; no invariant | security | Medium | PROPOSE FIX | §3.4 invariant; TC-721 asserts the snippet is identical under a hostile Host / `X-Forwarded-Prefix` |
| SEC-7: `reAssetBase` unspecified | security, readiness | Low / Major | PROPOSE FIX | Pattern `(?:\.\./)*` in §3.4; negative test |
| SEC-10 and zoom-out: design names prod VMs and checkout paths | security, zoom-out | Low / Nit | PROPOSE FIX | v2 says "both prod VMs" only |
| No phases, no rollback | readiness | Major | PROPOSE FIX | §7 phases 1-4 with exit commands and rollback |
| Register, decision-log and changelog content unspecified | readiness | Major | PROPOSE FIX | §3.5 row verbatim, `h3` id and position; §3.7 D-093 text |
| Upstream login page loses its entrance (`.rv`) | critic | Nit | PROPOSE FIX | Fixed upstream in v0.9.1; `_login.html` sets `ready` (§3.1) |
| Wrong citations: 13→8 selectors, line numbers, "18 auth tests", "decisions" vs "entries", `PUSTAKA_AUTH` truthy values | all | Nit | PROPOSE FIX | Corrected |
| N1 contradicts the edited register pages | readiness, critic | Nit | PROPOSE FIX | N1 reworded |
| Fallback 300 ms vs 350 ms | comprehension | Nit | PROPOSE FIX | §1.2 |
| `.shell` first-child assumption unchecked | comprehension | Nit | CONTEST | The readiness lens checked all 33 pages and every one opens with `.shell`. Recorded in §0 |
| Undefined deploy terms | comprehension | Nit | PROPOSE FIX | §7 names the command and the order |
| Press transition ambiguous for `.btn` / `.pn a` | readiness | Nit | PROPOSE FIX | §3.2: their transitions are unchanged |
| How the file is read | readiness | Nit | PROPOSE FIX | `io.ReadAll(f)` |
| Device-mockup claim (`.devices` exists) | readiness | Nit | PROPOSE FIX | §3.1 names the excluded rules exactly (`.dev`, `.tagpin`) |
| Progress bar lifecycle under overlapping swaps | critic | Nit | PROPOSE FIX | §3.3.4 |
| Placeholders permanent if the runtime fails | critic | Nit | PROPOSE FIX | R5, accepted |
| No crawler stance | critic | Nit | ESCALATE | N6 |
| Middle-ground server-rendered chrome | critic | Nit | PROPOSE FIX | §4 |
| CSS-only dark default not considered | zoom-out | Nit | PROPOSE FIX | §4 |
| Drop the font preload | zoom-out | Nit | DECLINE | §4 |
| Public switch bundled with the UI release | zoom-out | Nit | DECLINE | The operator asked for one release. Phases 3-4 still order the switch after the code is green |

No Blocker survives v2. A second round is not required by the policy. The design goes to the operator for approval.
