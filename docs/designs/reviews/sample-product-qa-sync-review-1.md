# sample-product-qa-sync — review round 1

Design reviewed: `docs/designs/sample-product-qa-sync.md` (draft v1). Five lenses, run in
parallel with the document path and the rubric paths only.

| Lens | Counts |
|---|---|
| zoom-out | B=1 M=5 N=3 |
| comprehension | B=1 M=6 N=10 |
| security | Critical=0 High=4 Medium=7 Low=5 |
| readiness | B=2 M=11 N=4 |
| critic | B=3 M=11 N=7 |

**Limits of this round, stated.** The canary probe and the positive control
(`docs/rules/gaos-design-review.md`, flow step 3) were **not** run; the lenses
ran as agents with the rubric paths only, and no report quotes author context,
but that is not proof. The operator was not asked: the task said "don't ask me",
so approval is the author's under that standing instruction.

## Triage

Dispositions: PROPOSE FIX (fixed in v2), CONTEST, ESCALATE, DECLINE.

| Finding (lens) | Severity | Disposition | Note |
|---|---|---|---|
| Reflection walks gated tasks past the design gate, overrides other BLOCKED reasons, writes READY, invents DISPATCHED/RUNNING (all five lenses; SEC-3) | Blocker | PROPOSE FIX | Reflection removed entirely (N5, Alt E, BR-54). QA data never writes the task graph. |
| Bootstrap walks `t-1`/`t-2` to DONE with no artifact (critic, readiness, comprehension; SEC-4) | Blocker / High | PROPOSE FIX | Seed does not clear the gate; design/review start PENDING (§4.9). |
| Decision id D-071 already exists (critic, readiness, comprehension, zoom-out; SEC-15) | Blocker | PROPOSE FIX | D-072…D-076; log count seventy-six. |
| `/api/v1` is stripped by Traefik, so the mux path is `/v1` (readiness) | Blocker | PROPOSE FIX | §4.7 states mux `/v1`, public `/api/v1`, test 399. |
| "Operator approves DONE" is unenforced (SEC-2) | High | PROPOSE FIX | Moot: nothing moves a card now. Existing unauthenticated `/tasks/{id}/approve` recorded as a known gap (§8). |
| BO sync routes reachable with a forged cookie (SEC-1) | High | DECLINE (mitigated) | The user asked for sync buttons. Mitigation: 30 s cooldown, one at a time, one configured origin, inert until the QA tools exist, cannot touch tasks. Stated in §8; real fix is BR-48. |
| Outbound half repeats the inbound half (zoom-out) | Major | DECLINE | The buttons are an explicit requirement ("sync button in both ways"). Kept, trimmed, inert by default. |
| Two seed paths; customer data in the binary; licence (zoom-out, critic) | Major | PROPOSE FIX | Generic `POST /v1/seed/product`; one case path (`catalog` + `seed:true`); sample-product data under `deploy/seed/sample-product/`, outside the binary (N8). |
| Two writers of the automation half; "conflicts impossible" false (critic, comprehension; SEC-13) | Major | PROPOSE FIX | Stale-write refusal by `runAt`; claim reworded (§4.1, BR-51). |
| Push-rejected cases dropped silently (critic) | Major | PROPOSE FIX | `Push.Rejected` (≤20) + `RejectedN`, shown on the tab (§4.10). |
| REVIEW/DONE epics hide later failures (critic) | Major | PROPOSE FIX | `regressed` list in summary and tab; cards untouched. |
| API depends on replay cursor anyone can set (critic; SEC-5) | Major / Medium | PROPOSE FIX | API calls `s.store` directly; BO sync keeps `s.mut` (TC-401). |
| Batch bound rests on a false 60 s fact (critic, comprehension, readiness) | Major | PROPOSE FIX | Bound is lock time; 200 items; edge facts cited (`gaos.conf:147,160`). |
| Schema underspecified; counts 29/38; edge index (readiness, critic) | Major | PROPOSE FIX | Full DDL in §4.3. |
| Test arithmetic missing (all) | Major | PROPOSE FIX | §7 table, TC-391…TC-408, totals 408 / 290 / 268. |
| Seed bootstrap calls unsupported by the store (priority, role, idempotency, epic task ids) (readiness) | Major | PROPOSE FIX | `ProductDraft.Priority`; role fixed; `Qa.Modules` key→story map is the idempotency record; no task ids needed. |
| Bootstrap default content not written (readiness) | Major | PROPOSE FIX | Content lives in `product.json`; SHAs recorded in the seed header. |
| Readiness floor skipped by the seed (critic) | Major | PROPOSE FIX | Step 7: 422 `not-ready`; test 406. |
| Desk fix has no decision (critic; SEC-11) | Major | PROPOSE FIX | D-076; effects stated (§4.8). |
| `story` means two things (comprehension) | Major | PROPOSE FIX | `userStory` vs GAOS `Story`; §4.9 maps modules. |
| Undefined terms (comprehension) | Major | PROPOSE FIX | §0 glossary. |
| Seed test cannot check an external repo (critic, readiness) | Major | PROPOSE FIX | Test 407 asserts fixed counts against the committed file; generator is diffed in P3. |
| Cannot verify seed.sh/generator/docs/deploy (readiness) | Major | PROPOSE FIX | §10 exit commands; TC-408; §6 verification. |
| No phases (readiness) | Major | PROPOSE FIX | §10. |
| Admin token over the public edge; token handling (SEC-6, SEC-7) | Medium | PROPOSE FIX | seed.sh on the VM via the tunnel listener, header via mode-600 file, `*_FILE` secrets, admin file removable. |
| QA token not product-scoped (SEC-8) | Medium | PROPOSE FIX | `GAOS_API_QA_PRODUCTS`, fail closed, 404 for others. |
| Third-party text into agent prompts; secrets in seed (SEC-9, SEC-10) | Medium | PROPOSE FIX | Generator sanitises and fails on secret patterns; test screens with the egress detector. |
| Cursor/URL hygiene and output encoding (SEC-12) | Low | PROPOSE FIX | ≤128 printable cursor, `url.Values`, auto-escape only. |
| Token edge cases: one set, equal tokens (SEC-14) | Low | PROPOSE FIX | §4.7 rules; test 397. |
| Failure throttle defends nothing (zoom-out) | Nit | PROPOSE FIX | Cut. |
| `autoRev` unused (zoom-out) | Nit | PROPOSE FIX | Dropped. |
| Stale line references; env-var count; CSS contradiction; event-count wording; DEFERRED; table precedence (several) | Nit | PROPOSE FIX | Corrected or moot in v2. |
| Deny-list enforced by instruction only (SEC-16) | Low | ESCALATE | Outside this change; noted for the workspace owner. |
| Unauthenticated `/tasks/{id}/approve`, `GET /replay/…` (SEC-2, SEC-5) | High / Medium | ESCALATE | Existing gaps; recorded in §8; not widened here. |
| Milestone/task ids (readiness) | Nit | PROPOSE FIX | M18, T-189…T-194. |

---

# Round 2 (critic and security lenses, against draft v2)

Critic: B=2 M=5 N=11. Security: Critical=1 High=3 Medium=5 Low=4.

| Finding | Severity | Disposition | Note |
|---|---|---|---|
| QA writes save the replayed past floor (critic) | Blocker | PROPOSE FIX | A QA write returns an empty `Floor`; `persist` skips the desk loops; the seed uses `s.mut`; TC-401 asserts live desks unchanged (§4.4). |
| T-189…T-191 already used (critic) | Blocker | PROPOSE FIX | M18 = T-192…T-196; `implementation-plan.html` added to §5. |
| `POST /workflows` and task routes have no cookie check; the desk fix widens them (SEC-1) | Critical | PROPOSE FIX | `requireBO` on those five routes (§4.8, §8); test in TC-396. |
| Stale rule bypassable, one bad `runAt` locks a case (critic) | Major | PROPOSE FIX | `runAt` required with `result`; `future` > now+300 s; equal = unchanged; admin `force` (§4.1). |
| Desk fix: no event, replay drops it, `p-10` desk order (critic) | Major | PROPOSE FIX | Wording corrected; load order re-sorted; tests in TC-396; gap recorded in D-076. |
| Lock-time budget ignores fixed cost, measured too early (critic) | Major | PROPOSE FIX | QA writes carry no floor; measurement moved to P3 exit (§4.5). |
| Missing token file → Docker creates a directory; removal needs restart (critic) | Major | PROPOSE FIX | Files must exist, empty = unset; `check-stacks.sh` asserts regular files (§4.7, §6). |
| Scope by name can be squatted (critic, SEC-5) | Major / Medium | PROPOSE FIX | Ids only; seed refuses to adopt an unseeded product. |
| Token files not gitignored/checked/deny-listed (SEC-2) | High | PROPOSE FIX | `.gitignore`, `check-stacks.sh`, deny-list (§5). |
| Outbound ignores product scope; pull feed has no product (SEC-3) | High | PROPOSE FIX | `qa-out-of-scope`, `product=` echoed, process-wide lock. |
| Customer map in a redistributable repo (SEC-4) | High | DECLINE (recorded) | The repo is private (`gh repo view`: PRIVATE). D-074 records the owner's approval and the condition to remove the files before it could be made public. |
| Seed sanitising only client-side (SEC-6) | Medium | PROPOSE FIX | Server-side screen and strip (§4.9). |
| No limit on authenticated writes (SEC-7) | Medium | PROPOSE FIX | One in-flight write per token, auth before body (§4.5). |
| Seeded plan removes every gate (SEC-8) | Medium | PROPOSE FIX | Release task `Gate:true`. |
| Token format invisible to detectors (SEC-9) | Medium | PROPOSE FIX | `gaos_` prefix, pattern in `egress.go` and `check-workspace.sh`. |
| `/v1` also reachable under `/app` (SEC-10) | Low | PROPOSE FIX | 404 when `X-Forwarded-Prefix` is set. |
| Remote strings unvalidated (SEC-11); token on command line (SEC-12); cleartext to any RFC1918 (SEC-13) | Low | PROPOSE FIX | Patterns; header from a mode-600 file; `http` only on `10.10.0.0/24` and loopback. |
| Eleven nits (method count, wording, count sites, module `pri`, empty heading, …) | Nit | PROPOSE FIX | All applied in v3. |

Surviving Blockers after v3: none known. A third round was not run; the
fixes are mechanical and each has a named test. That is a judgement, stated.
