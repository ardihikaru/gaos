# Review 1: `gaos-third-party-taskmgr.md` (revision 1)

Five lenses ran in parallel, each with zero context: comprehension, zoom-out, critic, readiness and security. Each
got the design path and the rubric paths (`docs/rules/gaos-design-review.md`, `docs/designs/README.md`) and was
told to open nothing under `.claude/`.

- **Canaries.** One `EGM-MARKER-…` line was planted in each of the seven standing files. They were removed with
  `git checkout`, and the value grep finds nothing.
- **Isolation.** The isolation probe of the previous design round (same session, same reviewer definitions)
  stands.

Counts:

| Lens | Blocker | Major | Nit |
|---|---|---|---|
| comprehension | 1 | 8 | 8 |
| zoom-out | 0 | 6 | 5 |
| critic | 0 | 15 | 8 |
| readiness | 0 | 12 | 5 |
| security | Critical 0, High 4 | Medium 6 | Low 4 |

## Findings, merged across lenses

1. **[B] The alert copies `maybeAlert`'s guard, which makes it unreachable** (comprehension B; zoom-out,
   critic, readiness M). `bundles.go:477` returns on any `BundleKey`, and every coded row has one. The design
   also says "`alerted` empty", so a row already alerted `no-task-code` is never told its new code is wrong.
2. **[M] `NotifyOn` opt-out bypassed** by the TaskMgr steps (comprehension, critic, readiness; SEC-5).
3. **[M] `e2e:*` → QA overrides the deliberate group routing** of `e2e:approval` (`notify.go:36-40`).
   `bundle:failure` is unplaced, and `qc` is fetched but unused (critic, readiness).
4. **[M] No behaviour after `errAuth`;** TaskMgr's lockout returns code 8, and GAOS would lock itself out
   (comprehension, critic, readiness; SEC-7). Also no backoff for `errUnavailable`: a TaskMgr outage would stall
   the serial poll loop for 10 s per row (critic).
5. **[M] "Live" omits `GAOS_PR_TEMPLATE_PRODUCTS`** (`fixprpoll.go:374`), and "live" has two meanings
   (comprehension, zoom-out, critic, readiness).
6. **[M] Write-back:**
   - it ignores the validation result and can comment on another product's task (critic, readiness; SEC-4 High);
   - it exceeds TaskMgr's 1500-rune limit (`task_comments.go:17,68`);
   - it says "every PR has merged" when some may be closed (comprehension, critic);
   - its marker guard covers no real retry path, the 100-comment cap misses old markers, and anyone can forge the
     marker (zoom-out; SEC-12).
7. **[M] Deleted or archived tasks validate `ok`** (`task_service.go:67-70`); code case is not stated
   (comprehension, critic).
8. **[M] The cache value type contradicts itself.** It has no product key, no size cap and no clear on rebind
   (readiness, critic; SEC-14).
9. **[M] `NotifyPR` has no way to find the role holder,** and the notifier and test-planner fields are unnamed
   (readiness, critic).
10. **[M] The people table breaks the registry's "counts only" rule** (`contacts.go:26-27`), and BR-62 is
    contradicted by the stored `tm:` alias (zoom-out, critic, comprehension; SEC-6).
11. **[M] No Alternatives and no Risks section.** TaskMgr REST is not weighed (zoom-out, critic).
12. **[M] The change list misses files:** `events_test.go`, `integrations_test.go`, `store_conformance_test.go`,
    `graph.html`, `pull-requests.html`, the test files and `.gitignore`. No phases (readiness, critic).
13. **[M] The binding form names no product,** no conflict sentinel exists, and unbinding is undefined
    (readiness).
14. **[M] The general binding model with routes and an event is speculative,** where an env list is the
    precedent (zoom-out).
15. **[High] SEC-1:** `GRPCAdmin` is a full TaskMgr admin credential.
16. **[High] SEC-2:** `prod.taskmgr-auth` escapes every secret guard (`.gitignore`, `check-stacks.sh`, the
    tag-worktree link loop).
17. **[High] SEC-3:** plaintext gRPC is relayed through the WireGuard hub.
18. **[Med] SEC-8:** unvalidated QA case codes and TaskMgr product codes go into comments.
19. **[Med] SEC-9:** private instance, product and address strings in the design (D-085).
20. **[Med] SEC-10:** the reviewer deny-list does not cover `prod.*` secrets.
21. **[Low] SEC-11:** duplicate GitHub logins. **SEC-13:** two alert texts leak task existence.
22. **[N] Anchors and wording:** `backoffice.html:908` → `:945`; `jsonList` not `jsonMap`; "Third-party" is not
    a status label; `§6.3` is empty; counters undefined; the varint code can reuse `encoding/binary`.

## Triage

| # | Finding | Severity | Disposition | Revision 2 |
|---|---|---|---|---|
| 1 | Alert guard | Blocker | PROPOSE FIX | Exact predicate given. No `BundleKey` check. `Alerted ∈ {"", no-task-code}`. Editing the code resets nothing. |
| 2 | `NotifyOn` bypass | Major | PROPOSE FIX | A contact found but filtered out ends at the group. |
| 3 | Role routing | Major | PROPOSE FIX | Step 3 applies only to `bundle:planned` (→ QA). Everything else stays login-based. `e2e:approval` and `bundle:failure` go to the group. `qc` is dropped. |
| 4 | Auth and outage | Major | PROPOSE FIX | A circuit breaker. `errAuth` and code 8 latch off until the credential file's mtime changes or 1 h passes. `errUnavailable` backs off from 1 to 30 min. The poll-path deadline is 3 s. |
| 5 | Live gates | Major | PROPOSE FIX | Two predicates, `taskmgrReady` and `taskmgrNotifies`. Template products is part of "ready". |
| 6 | Write-back | Major / High | PROPOSE FIX | A fresh `GetTask` that must answer `ok` with the bound product. Wording: "m merged, c closed". A 1500-rune cap with "+k more". Best-effort once per DONE. `ListComments` and the marker are dropped. |
| 7 | Deleted tasks, case | Major | PROPOSE FIX | `is_deleted` (37) and the status read. Deleted or archived counts as `unknown`. Codes are matched as written (TaskMgr's ids are case-sensitive). |
| 8 | Cache shape | Major | PROPOSE FIX | One `taskCheck` struct keyed by (product code, code). 512 entries LRU, cleared on rebind. `ok` is re-checked after 30 min. |
| 9 | `NotifyPR` | Major | PROPOSE FIX | Unchanged: no role step on that path. The notifier gains `people *taskmgrPeople`. |
| 10 | People table, BR-62 | Major / Med | PROPOSE FIX | Counts for everyone; the table only for `autoFixAdmin` accounts. BR-62 narrowed to phone numbers. The alias is `tm:` + `sanitizeIDPart(username)`. |
| 11 | Alternatives, risks | Major | PROPOSE FIX | Sections added. REST lacks numbers and logins (`master_users_rest_api.go:12-26`), so gRPC is chosen. |
| 12 | Change list, phases | Major | PROPOSE FIX | Exact files and phases. |
| 13 | Form product, sentinel | Major | PROPOSE FIX | A product select over the products; `ErrConflict` → 409; disable rather than delete. |
| 14 | Env vs binding | Major | CONTEST | The operator chose per-product configuration in GAOS. The second gate (template products) is now explicit in "ready". |
| 15 | SEC-1 `GRPCAdmin` | High | PROPOSE FIX | The operator chose a narrow TaskMgr grant. TaskMgr adds `GRPCIntegration` (four methods, product-scoped), its own design `taskmgr-grpc-integration-grant.md`. GAOS refuses nothing extra but is documented to need only that grant. |
| 16 | SEC-2 file name | High | PROPOSE FIX | `prod.pr-taskmgr-auth` at `/run/secrets/pr-taskmgr-auth`, so every guard applies. |
| 17 | SEC-3 hub | High | PROPOSE FIX | TLS on the TaskMgr listener with a pinned self-signed certificate (`GAOS_TASKMGR_CA_FILE`), stdlib TLS with ALPN h2. Plaintext is refused unless the address is loopback. |
| 18 | SEC-8 strings | Med | PROPOSE FIX | Case codes are filtered with `apiCaseCodePattern`, and a product code with the code pattern. |
| 19 | SEC-9 D-085 | Med | PROPOSE FIX | Placeholders everywhere (`<taskmgr-addr>`, `p-N`, `<CODE>`). |
| 20 | SEC-10 deny-list | Med | ESCALATE | A rule-file change (`.claude/rules/agent-workspace.md`), out of this design. Recorded as OQ-45. |
| 21 | SEC-11, SEC-13 | Low | PROPOSE FIX | Duplicate checked logins are dropped from `byLogin`. One alert text for both cases. |
| 22 | Nits | Nit | PROPOSE FIX | Applied. |
