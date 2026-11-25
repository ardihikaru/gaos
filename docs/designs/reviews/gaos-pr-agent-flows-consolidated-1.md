# gaos-pr-agent-flows — review round 1 (consolidated)

- Document: `docs/designs/gaos-pr-agent-flows.md` (Status: Draft)
- Lenses: comprehension (B3 M13 N7), zoom-out (B2 M10 N4), critic (B7 M11 N6),
  readiness (B3 M15 N2), security (High 5, Medium 7, Low 4).
- Isolation: canaries planted in the seven standing files before the run and removed after it
  (`git checkout`); no canary value in any report. All five lenses reported that the harness
  attached `.claude/CLAUDE.md` to their context unasked; none used it (same as the earlier rounds,
  isolation for that file is instruction-only). **The positive control was not run this round.**
- Process note: the reviewers found implementation already in the worktree
  (`labels.go`, `prflow.go`, `github.go`, `wa.go`, `contacts.go`, roles, DDL, events) written
  before this review. That code is treated as a spike and is adapted to the approved design.

Full lens reports were returned in-session and are summarised here by finding; severities are the
reviewers' and are not changed.

## Findings, deduplicated, with the author's disposition

| # | Finding | Lenses | Severity | Disposition |
|---|---|---|---|---|
| 1 | §7 says GAOS posts the review; §11.2 says the agent does. Decides where the write token lives | all five | Blocker | PROPOSE FIX — GAOS posts; delete §11.2's reading; only `github.go` holds the token; name the handoff field |
| 2 | Contact registry "not on `Master`" (§8.4) vs "on `Master`" (§10); no storage, no write path | comprehension, critic, readiness, security, zoom-out | Blocker | PROPOSE FIX — own vertex type, never on `Master`; or defer (operator decision) |
| 3 | Staging route is HTTP Basic: conflicts with BR-52/BR-55; one global credential, not product-scoped; §11.4 mislabelled | critic, zoom-out, security, comprehension | Blocker | operator decision (bearer client vs amend BR-52) |
| 4 | `kind: review` is the design-gate reviewer (BR-46/47, `review-missing`, plan validator); a PR review is not that | critic, comprehension | Blocker | PROPOSE FIX — PR reviewer is an `implement` task with its own reply contract |
| 5 | External events create dispatchable work: no initial status, cap, budget, draft/fork filter; BR-54 precedent (born BLOCKED) ignored; "creates the workflow" would reset the desk | critic, zoom-out, security | Blocker | PROPOSE FIX — attach to the current workflow, born per BR-54 or recorded decision, per-product cap, skip drafts/forks |
| 6 | No agent holds the four roles, so the scheduler blocks every card | critic | Blocker | PROPOSE FIX — add agents; success bar is "dispatchable" |
| 7 | BR-05: data-model document (`artifacts/v1/gaos-arcadedb-model.html`), `events_test.go`, graph.html missing; DDL is extracted, not hand-edited | critic, readiness, comprehension | Blocker | PROPOSE FIX — add to change list |
| 8 | e2e builder: agent writes the spec, or the QA tool's `caseauto.Machine` does (which opens a PR, against §14); no `caseCode` source; no poller; the runner has no spec to run | all | Blocker/Major | operator decision |
| 9 | No phases, no exit command per change | readiness | Blocker | PROPOSE FIX — add phases |
| 10 | Review body is posted unscreened (prompt injection → Blueprint leaks to a public PR) | security | High | PROPOSE FIX — screen and cap the reply before posting |
| 11 | WhatsApp over plain `http`, private host and group name committed (D-085) | security, critic, readiness, comprehension | High/Medium | PROPOSE FIX — https only, no defaults, placeholders |
| 12 | Seed policies tell the agent to follow a repo-local agent file GAOS does not own (BR-42), ref unspecified | security | High | PROPOSE FIX — pin to the default branch at a recorded SHA, or render as Blueprint |
| 13 | Labels: no event, not in `TaskPlanned`, scheduler ignores `manual`, no route, singular/plural | comprehension, critic, readiness, zoom-out | Major | operator decision (drop for v1, or specify) |
| 14 | `rulesFile` already matches the new paths; only `kinds`, `policyFile`, the message change | zoom-out, comprehension, readiness, critic | Major | PROPOSE FIX |
| 15 | Four new events vs BR-54's "no event name" precedent | zoom-out | Major | DECLINE for `ContactNotified`/staging audit; operator decision for the three PR/staging events |
| 16 | De-dup key drops the owner, eviction, staging has no key, cursor advances before commit (§12 wrong) | critic, security, readiness, comprehension | Major | PROPOSE FIX — `owner/repo#n:role`, graph fallback, correct §12 |
| 17 | Staging: "unblocks the waiting card" never exists; 409 contradicts it; no replay or throttle | all | Major | PROPOSE FIX — create-only; key `owner/repo@sha`; throttle |
| 18 | GitHub: 403/404 vs 422, pagination, rate limit, no redirects; "last committer" has no source | comprehension, critic, readiness, security | Major/Low | PROPOSE FIX |
| 19 | A WhatsApp failure marks a successful review Failed; retry rule conflicts | comprehension, readiness, critic | Major | PROPOSE FIX — record separately, task status unchanged |
| 20 | Egress refuses the whole dispatch on a hit; planting a token suppresses review | critic | Major | PROPOSE FIX — state outcome and the label |
| 21 | Per-product staging credential, author/fork allowlist, back-office actor events | security | High/Medium | PROPOSE FIX |
| 22 | Room `review` does not exist; stale line references; counts (six pairs, 15 Go, 9 non-Go, 42 files); malformed row; missing files (`main.go`, `qaclient.go`, `app.css`, `toc/product.js`, secret mounts); D-088 and TC-534+ unallocated; `.claude/rules/qa-test-plan.md` counts stale | all | Major/Nit | PROPOSE FIX |
| 23 | Role/DDL test exists already (`labels_test.go:21`); extend as a subtest | zoom-out, critic | Nit | PROPOSE FIX |

Operator decisions needed before round 2: #2, #3, #8, #13, #15.
