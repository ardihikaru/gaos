# Review 1: `gaos-external-tasks-and-pr-bundles.md` (revision 1)

Four lenses ran in parallel, each with zero context: comprehension, zoom-out, critic and readiness. Each got
the design path and the rubric paths (`docs/rules/gaos-design-review.md`, `docs/designs/README.md`) and was
told to open nothing under `.claude/`. The security lens is filed in
`gaos-external-tasks-and-pr-bundles-security-review-1.md`.

## How the round was run

- **Canaries.** One `EGM-MARKER-…` line was planted in each of the seven standing files. They were removed
  after the run with `git checkout`. The value grep (`deploy/check-workspace.sh:183`) finds nothing, and no
  canary appears in any report.
- **Isolation probe.**
  - A `general-purpose` agent, asked whether its context held the sentence starting "Four invariants, and
    where to read the rest", answered **YES**. This is the positive control.
  - A `gaos-goldfish-zoom-out` agent answered **NO**, and still **NO** after reading
    `docs/designs/README.md`.

## Comprehension: B=1 M=11 N=3

- **[B] API auto-dispatch breaks BR-56 under `manual`.**
  - `runnerMaySpend` is true under `manual` (`runner.go:64-67`).
  - BR-56 (`brd.html:1306`) reserves that mode's turns for an operator.
  - The auto-fix path it copies has a per-product opt-in (`autofix.go:151-155`); this one has none.
- [M] `dispatch:false` does nothing for e2e kinds, because the QA runner starts them anyway
  (`qajobs.go:216-224`).
- [M] `caseCode` through `validCursor` lets caller text, including `:@#`, into keys and titles. This goes
  against BR-59.
- [M] `caseCodeFor` returns `*` for every run before it reads the key (`qajobs.go:233-235`).
- [M] B5 does not make merge events lossless. `handle` returns true on every failure
  (`prflow.go:355-388`), and a frame dropped on a full channel is passed over.
- [M] The test-planner card has no rule for the open-card cap.
- [M] Bundle states contradict Retry (`failed` is final). Idempotency keyed on `(bundleKey, index)` drops a
  retried run's new cases. Late joiners are not covered, and the key differs between §6.5 and §7.
- [M] Overlapping bundles can plan two test-planner cards, and "a member of both" cannot be represented.
- [M] The alert and the bundle notices have no card, and `Notify` needs one (`notify.go:70-81`). No pass
  evaluates the alert for a PR in no bundle.
- [M] B4 rides on a merge branch whose cap refusal drops its events (`prflow.go:239-258`). A card that
  reaches REVIEW after the merge is never revisited.
- [M] The PR template is neither shown nor linked.
- [N] Undefined terms. Small inaccuracies (titles, the "bounded" scan, the "second call", the pustaka test's
  location, the silent gap between releases A and C). The test-planner packet wiring is not placed.

## Zoom-out: B=0 M=7 N=5

- [M] The new dispatch loop clones auto-fix, and its only effect is to bypass `manual`. Drop it, or make the
  decision explicit and reuse `autoFixPass`.
- [M] `PlanAPICard` is a second planner and has already drifted: it does not write the `PullRequest` row
  (`prflow.go:253,302`). Share one locked helper over `prCardSpec`.
- [M] The `qa-case:` kinds are speculative, and the change the design lists does not make them work. Defer
  them.
- [M] The `PRBundle` vertex and rowless polling exist only for unfollowed repositories. Compute the bundle
  from the rows that share `bundleKey`, and use the card key as the marker.
- [M] A PR in two bundles cannot be stored in one `bundleKey`. Drop multi-membership.
- [M] Two fixes (B4, B5) wait on a feature release that waits on another repository. Ship them first as a
  patch.
- [M] The alerts assume a notifier path that does not exist. `loginFor` sends non-`review:` kinds to the
  committer (`notify.go:43-51`).
- [N] Files missing from the change list. The "bounded" claim. Titles are not the ones `prCardsFor` uses
  (bare repo, `prflow.go:79`). A wrong anchor (`pullrequests.go:94` vs `:98`). The tutorial is kept twice.

## Critic: B=3 M=13 N=3

- **[B] Under `manual`, `apiDispatchLoop` starts paid turns with no operator, against BR-56.** BR-56 is not
  amended.
- **[B] The cursor volume is owned by 65532, but prod runs as `GAOS_UID`** (`compose.prod.yaml:198-203`).
  Distroless has no shell.
- **[B] The `test-planner` role has no agent seat.** `freeAgent` (`scheduler.go:310-315`) keeps the card
  PENDING forever.
- [M] BR-52 and the clients-file QA-overlap drop (`api_v1.go:222-226`) contradict the `tasks` capability.
- [M] Dedup keys are global (`prflow.go:166-173`), so `qa-case:` and `bundle:` keys collide across products.
- [M] The `caseCode` and key grammar does not parse as described. `prKeyParts` succeeds on `qa-case:x/y#3`
  and on `bundle:pr:acme/backend#42:…`.
- [M] `dispatch` has no label gate, and `APIDispatch` names no claim. The `BlockEgress` branch is
  hard-wired to `MoveAutoFix` (`board.go:532`).
- [M] The `dispatch` answer gives a false contract under `on`. `readyPending` runs only under `manual`
  (`runner.go:502-513`).
- [M] On a merge with a full cap, B4's REVIEW → DONE is thrown away.
- [M] The cursor fix does not deliver "cannot lose a merge event".
- [M] A `broken` bundle can never reopen (`pollRow` stops at `closed`, `fixprpoll.go:297-299`), and nothing
  expires a bundle.
- [M] The QA tool's idempotency on `(bundleKey, index)` is wrong for a retry that reruns the model.
- [M] Switching to `review:needs-manual` drops severity opt-ins (`contacts.go:205-208`).
- [M] Several file changes are missing from the change list: `pullrequests.go`, the PR page template,
  `fixtures.go` agents, `ghPRState.Body`, `webhooks-and-clients.html`, the compose comment and
  `Dockerfile.pustaka`.
- [M] No sweep is named for the alert on a PR that is in no bundle.
- [N] `Notify` needs a card. Anchors have drifted. Several claims need correcting, and there is no
  risks-and-rollback section.

## Readiness: B=1 M=12 N=7

- **[B] The `test-planner` role cannot be stored or seated.**
  - `Task.role` and `Agent.role` are closed regexps (`schema/04-execution.sql:29-30`, `05-capacity.sql:13-14`)
    with ALTERs (`17-defects.sql:79-80`).
  - `labels_test.go:28-62` pins them.
  - No agent seat is added.
- [M] The change list is not a usable allowlist. It uses globs, and it misses
  `schema_test.go:45 wantVertexTypes`, `artifacts/v1/gaos-arcadedb-model.html`, `webhooks-and-clients.html`
  and the test files.
- [M] A `tasks` client cannot share a product with the QA token (`api_v1.go:222-226`).
- [M] The `inbox` capability is undefined: does a `tasks`-only client reach the inbox?
- [M] `qa-case:` and `bundle:` keys collide with the existing key parsers, and the run shortcut comes first.
- [M] A merged PR at the cap is applied in memory but never persisted (`store_arcade.go:556-558`). It covers
  Brain Town only, and RUNNING cards are never revisited.
- [M] Saving the cursor after the commit does not make merge events lossless.
- [M] Some notices have no card, and the author is not who gets them.
- [M] The test-planner lifecycle contradicts itself (Retry, the cap).
- [M] The API route and the test-planner packet are not wired. `server` has no flow, and the packet hook
  is a single function (`main.go:443`).
- [M] Bundle polling and key changes are left open. A `pr:` bundle that later gains a code gets a second
  planner.
- [M] The test-plan enum values are not stated. GAOS uses `API|GQL|WEB|E2E` and `P0..P3`
  (`schema/16-qa-catalog.sql:22-23`). The QA route is not probed before the tag.
- [M] Several changes have no verification: GET, the Integrations rows, merge `AdvancePullRequest`, the API
  manual notice, the missing-template alert, and `check-stacks.sh`. There are no exit conditions.
- [N] Anchors. Titles. Forward text not given. The distroless `mkdir`. Forward test-count strings. The
  B-on-A dependency. Egress and `dispatch:false` details.

## Triage

One row per distinct finding. Where several lenses raised the same defect, the row names them all. Severities
are the reviewers' and are not changed.

| Finding | Severity | Disposition | Note |
|---|---|---|---|
| API auto-dispatch bypasses BR-56 under `manual` (comp, critic, zoom-out) | Blocker | PROPOSE FIX | rev 2 §4.5: an explicit BR-56 amendment (D-099). Auto-run happens only for products listed in `GAOS_API_AUTORUN_PRODUCTS`, under a daily cap `GAOS_API_AUTORUN_DAILY` (default 20). It reuses `autoFixLoop`'s ticker and the one `dispatch` with an `apiClaimLocked` claim. There is no second loop. |
| Cursor volume owner ≠ `GAOS_UID` (critic, SEC-12) | Blocker | PROPOSE FIX | rev 2 §3 (patch v0.60.1). A bind **directory** `deploy/stacks/prod.bt-state/`, mode 700, owned by the deploy user and created by hand like the other prod files. `rebuild-restart.sh` refuses a missing one and links it like `*.bt-*`. `check-stacks.sh` asserts the rw dir mount. The write is tested live. |
| `test-planner` role not storable or seatable (critic, readiness) | Blocker | PROPOSE FIX | rev 2 §7.5. Both CREATE regexps, a new ALTER pair in `19-pr-bundles.sql`, `labels_test.go` updated, an agent seat `a-14`, and a PENDING → READY test through `Schedule`. |
| B4/B5 wait on the feature (zoom-out) | Major | PROPOSE FIX | rev 2 ships them first as patch v0.60.1 (§3). |
| B5 does not make events lossless (comp, critic, readiness) | Major | PROPOSE FIX | rev 2 §3.2. `handle` returns `retry` on transient store or GitHub errors, and the cursor stays. A full channel ends the session (reconnect from the cursor) instead of dropping. The poller is named as the source of truth for merge state, and the "cannot lose" claim is restated. |
| B4 events lost at the cap; late REVIEW cards never revisited (comp, critic, readiness) | Major | PROPOSE FIX | rev 2 §3.1. The moot moves are committed even when the unit-test card is refused. The poller's merged row also finishes REVIEW reviewer cards (`FinishMergedReview`). |
| SEC-4 secret-held card auto-finished | Medium | PROPOSE FIX | rev 2 §3.1. `BlockReason=secret-held` is never auto-finished. |
| Second planner drifts (no row; titles) (zoom-out, comp, critic) | Major | PROPOSE FIX | rev 2 §5.2. The API builds a `PREvent` and calls one shared locked helper (`planPRCardsLocked`), so it writes the row and uses the `prCardsFor` titles. |
| `qa-case:` kinds speculative or broken (zoom-out, comp, critic, readiness, SEC-7, SEC-8) | Major | PROPOSE FIX (partly ESCALATE) | `automation-create` by `caseCode` is **deferred** (OQ-42). `automation-run` stays, because the operator asked for "run automation tests". It gets a strict code pattern, a product-scoped key with no `#`, the branch placed before the `*` shortcut, `prKeyParts` refusing non-PR prefixes, and one open run per product. |
| `dispatch` contract per runner mode; flag on e2e kinds (comp, critic) | Major | PROPOSE FIX | rev 2 §5.4 gives the answer table per mode. `dispatch` is ignored for e2e kinds, and the answer is `qa-tool`. |
| No label gate and no claim; `BlockEgress` hard-wired (critic) | Major | PROPOSE FIX | `apiClaimLocked` re-checks the product list, the cap, `AutoDispatch`, `labelsAllowDispatch` and the lane. The `BlockEgress` branch extends to `MoveAPI`. |
| BR-52 and the QA-overlap drop vs `tasks` (critic, readiness) | Major | PROPOSE FIX | BR-52 amended. A client without `inbox` is exempt from the overlap drop. |
| `inbox` capability undefined (readiness) | Major | PROPOSE FIX | The inbox routes check `needCap("inbox")`, with a test. |
| Global dedup collision (critic) | Major | PROPOSE FIX | Non-PR keys carry the product id, and the lookup is product-scoped. |
| SEC-1 natural-key takeover | High | PROPOSE FIX | The API cannot cancel natural-key cards. `unit-test` requires a merged PR. |
| SEC-2 cap DoS | High | PROPOSE FIX | A per-client cap `GAOS_API_MAX_OPEN` (default 5). API-asked cards are excluded from `openPRCardsLocked`. |
| SEC-3 gate only on `pr-review` | High | PROPOSE FIX | `prepare` runs for every `pr` kind, and `503 github-off` applies to all of them. |
| SEC-5 stranger PRs in bundles | Medium | PROPOSE FIX | Parsing and joins happen only for PRs that passed the gates, which are exactly the rows. |
| SEC-6 unfollowed members leak | Medium | PROPOSE FIX | Rowless members are never polled. They are listed as `not followed`, ref only. |
| SEC-7 code charsets | Medium | PROPOSE FIX | `TaskCode` `^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`. `externalId` has no `:@`. A strict case-code pattern. |
| SEC-8 unlimited runs | Medium | PROPOSE FIX | One open `automation-run` per product. `*` is limited to one open run per product. |
| SEC-9 fence and link trust | Medium | PROPOSE FIX | A per-packet random fence, the DATA clause, and `link` codes validated against `QaCases`. |
| SEC-10 lookup before fetch | Medium | PROPOSE FIX | Stated in §5.3. |
| SEC-11 GET scope, cancel prefix | Medium | PROPOSE FIX | GET and cancel only for cards whose `apiRefs` holds the caller (exact name). `apiRefs` is a list. |
| SEC-12 deploy wiring | Medium | PROPOSE FIX | The `prod.api-clients/` dir is mounted ro, with `GAOS_API_CLIENTS_FILE` and `check-stacks.sh` rows. |
| SEC-13 "read token" | Low | PROPOSE FIX | Reworded. |
| SEC-14 API manual notice | Low | PROPOSE FIX | Group only, naming the client. |
| SEC-15 published text | Low | PROPOSE FIX | Placeholders, plus a pustaka test. The event message names no client. |
| `PRBundle` vertex and rowless polling unnecessary (zoom-out) | Major | PROPOSE FIX | The vertex is dropped. A bundle is the set of rows sharing `bundleKey`. The planner card key is the marker, and the outcome lives on the card. |
| Multi-membership and overlapping bundles (zoom-out, comp) | Major | PROPOSE FIX | One key per row, with the adoption and supersede rules in §7.2. |
| `broken` never reopens; no expiry (critic) | Major | PROPOSE FIX | There is no `broken` state. A closed member is excluded. A bundle completes when every non-closed member is merged and at least one is. Rows already expire at 90 days. |
| Test-planner cap, Retry and idempotency (comp, critic, readiness) | Major | PROPOSE FIX | Bundle cards have their own limit (2 open per product); the bundle waits and retries the next pass. Retry reruns the model, and the QA tool keys on a hash of each item's content. |
| Notices without a card; wrong recipient (zoom-out, comp, critic, readiness) | Major | PROPOSE FIX | A new `NotifyPR`. `loginFor` maps `pr:` kinds to the author. |
| Alert sweep not named (comp, critic) | Major | PROPOSE FIX | `pollRow`'s hook over review rows that are ≥10 min old, not draft, with `alerted` empty and `templateSeen` set (new rows only). |
| needs-manual drops severity opt-ins (critic) | Major | PROPOSE FIX | GAOS's own relabel keeps `review:<severity>` with new text. Only a secret hold uses `review:needs-manual`. |
| Template not shown (comp) | Major | PROPOSE FIX | Appendix A, with generic repository names (D-085). |
| Wiring of flow and packet (readiness) | Major | PROPOSE FIX | `server.flow`. The packet hook appends the test-planner parts. |
| Enum values and pre-tag probe (readiness) | Major | PROPOSE FIX | The QA tool's values are `+ -`, `API GQL WEB E2E`, `P0..P3`. A pre-tag curl expects `401`. |
| Missing verification and exit checklists (readiness) | Major | PROPOSE FIX | Tests and commands added; each release gets an exit checklist. |
| Change list not an allowlist (critic, readiness, zoom-out) | Major | PROPOSE FIX | Exact paths, the test files and the listed extras. |
| Tutorial kept twice (zoom-out) | Nit | CONTEST | The operator asked for a downloadable markdown tutorial. It stays, with the drift test. |
| Glossary, anchors, forward strings, risks section (all) | Nit | PROPOSE FIX | Applied in rev 2. |
