# Review 2: `gaos-external-tasks-and-pr-bundles.md` (revision 2)

Round 2 ran all five lenses against revision 2 with zero context, set up exactly as in round 1: the design
path, the rubric paths, and nothing opened under `.claude/`.

- **Canaries.** Planted again in the seven standing files. They were removed with `git checkout`, and the
  value grep (`deploy/check-workspace.sh:183`) finds nothing.
- **Isolation.** The round-1 probe (a `general-purpose` agent answered YES, a goldfish answered NO before and
  after a `docs/` read) is not repeated, because the reviewer definitions and the harness are unchanged.
- **Security.** The security lens is summarised in this file, under its own heading.

Round 1's Blockers are confirmed closed:

- the BR-56 bypass, now an explicit opt-in with a claim;
- the cursor owner, now a directory owned by the deploy user;
- the role, now in the regexps and with a seat.

The new findings are in the parts revision 2 rewrote.

## Comprehension: B=2 M=7 N=7

- **[B] The QA tool loses the inbox.** Its client gets an empty capability set, while the inbox handlers now
  require `inbox` (`api_inbox.go:148,223,294`; BR-57).
- **[B] A review that is RUNNING at merge is never finished.** `pollRow` stops at a final row
  (`fixprpoll.go:297`), so there is no "next poll".
- [M] Filtered frames written by the reader goroutine can move the cursor past uncommitted events. A failed
  `Masters` read filters everything (`braintown_consumer.go:559-568`).
- [M] `flowRetry` has no bound, so one bad event stops intake.
- [M] The first `apiRefs` entry cannot tell a created card from an adopted one, and eviction moves
  ownership.
- [M] The POST handler leaves cases open:
  - an `externalId` reused for a different request;
  - a natural key planned during the GitHub call;
  - `run-in-flight` checked outside the lock;
  - the helper's merge and cap behaviour for API callers.
- [M] The "joined" time has no field, because `CheckedAt` is overwritten (`fixprpoll.go:341`).
- [M] Alert (b) fires on the secondaries of a `pr:` bundle, and its text is false.
- [M] A retried test plan makes near-duplicate drafts, because the titles change.
- [N] The release dependencies; anchors (`columnOf` is at `board.go:157`); the reason for the overlap rule
  (id collision, `api_v1.go:196-197`); SEC-n and BR-59 cited undefined; the checklist prerequisite; the
  test-planner card id; `pr-review` on a merged PR.

## Zoom-out: B=0 M=4 N=6

- [M] The shared planner cannot serve three callers that build fake `PREvent`s. It emits
  `StagingDeployed`/`PullRequestOpened`, runs the moot cancel, and its `prCardID` breaks.
- [M] Ownership is encoded as a position in a capped list. Use an immutable `apiOwner`.
- [M] The two defect fixes wait on approval of a four-release feature. Ship them on their own.
- [M] One team's template is hard-coded for every product, with no switch. Add a per-product opt-in, and
  use one alert channel.
- [N] Autorun is an env var while the Master has a pattern for this. Bundle cards are gated by an "API"
  variable without saying so. `FinishMergedReview` is called under the lock. There is already a body
  parser (`qajobs.go:245-253`). Storing `testNotes` is not needed. The release dependency and test
  placement. Change-list gaps (`GAOS_API_MAX_OPEN`, where `bundlePass` lives).

## Critic: B=3 M=13 N=6

- **[B] The QA tool loses the inbox.**
- **[B] Ownership in `apiRefs` lets a client get around both caps** through adoption and eviction.
- **[B] `testPlannerParts` makes network and store calls under the store's read lock.** The hook runs in
  `freezePacketFor` (`store_mem.go:1639-1641`, `runner.go:624-630`).
- [M] A REVIEW reached after the merge is never finished.
- [M] The cursor has two writers and no monotonic rule, and the channel is not flushed on retry.
- [M] `flowRetry` becomes a poison pill. Retrying a store error does nothing, because `mutate` already
  applied the change to the projection (`store_arcade.go:552-563`).
- [M] `check-stacks.sh` fails on both new directories (`:118-129`, `:142-153`, `:158`).
- [M] API and bundle planning emits domain events that did not happen (`prflow.go:289-299`).
- [M] The release order contradicts the change list (`NotifyPR`, `api_tasks.go`, TC-781).
- [M] Bundle membership depends on rows that may never exist (a PR the cap refused) or may be pruned (after
  30 days, `pullrequests.go:33`).
- [M] The alert is wrong for the secondaries of a bundle with no code.
- [M] Retrying the test planner duplicates cases.
- [M] The new notice kinds cannot be opted into. `NotifyOn` is closed (`contacts.go:66-85`).
- [M] The lookup-to-plan race, and the `dispatch` answer for an adopted card.
- [M] Docs made wrong are missing from the list: `qa-sync-api.html:237`, `environments.html:208`,
  `README.md:108` and `backoffice.html:1100`.
- [M] The v0.62.0 rollback ignores the new role.
- [N] Anchors. The `secret-held` write path. The scope of the daily cap's count. The `off` column. The PR
  page template is not named. Where `runner-off` sits in the order.

## Readiness: B=3 M=12 N=7

- **[B] The QA tool loses the inbox.**
- **[B] The cursor can still lose events** (two writers, the channel not flushed on retry, no mechanism).
- **[B] The test-planner packet calls GitHub and the store while the lock is held.**
- [M] RUNNING at merge is never finished.
- [M] `FinishMergedReview` is called from inside the locked function, and the `capped` signature is not
  given.
- [M] `planPRCardsLocked` has no contract: keys, ids, titles, side events.
- [M] `apiRefs[0]` cannot identify the creator.
- [M] The lookup, adopt and read paths have no store methods, there is a race, and the admin token is
  undefined.
- [M] Adoption ignores the caller's `review` and `dispatch`.
- [M] The daily autorun count does not say which cards it counts, or whether bundle cards are eligible.
- [M] The bundle-field writers and the "joined" time are not defined, and a late PR never adopts.
- [M] The poller has no notifier and no comment poster (`main.go:436`, `fixprpoll.go:121,156`).
- [M] `check-stacks.sh` fails on the two directories.
- [M] `flowRetry` has no retry bound.
- [M] The v0.60.1 persistence fix is tested only in memory.
- [N] The `NotifyPR` register. The `secret-held` store path. The §5.4 details. The route count (4, not 3).
  Change-list globs. Persisting the agent seat. Deploy follow-ups.

## Security: Critical=0 High=2 Medium=6 Low=6

- **[High] SEC-1:** spend has no product or daily bound under `on`. A client can walk every past PR
  (`pr-review` is not refused on closed or merged PRs).
- **[High] SEC-2:** a permanently failing GitHub lookup (404, 403) becomes a poison event that halts the
  reader. The lookup runs before the association check (`prflow.go:365-381`).
- [Med] SEC-3: the eviction and adoption ownership conflict.
- [Med] SEC-4: adoption before the gates lets a client read any card's `reason`, including `secret-held`.
- [Med] SEC-5: `qa-run/` keys carry no client.
- [Med] SEC-6: `automation-*` ignores `GAOS_RUNNER=off`, and create-cancel loops are unbounded.
- [Med] SEC-7: the QA token loses the inbox (`external_inbox_test.go:109-116`).
- [Med] SEC-8: the admin token as a task principal is undefined.
- [Low] SEC-9: the body is decoded before `beginWrite` (`api_v1.go:348-350`).
- [Low] SEC-10: false `PullRequestOpened`/`StagingDeployed` events.
- [Low] SEC-11: untrusted template text picks the bundle; a link can stall it.
- [Low] SEC-12: the deploy checks use `-f` on directories; the clients-file mode; revoking by restart.
- [Low] SEC-13: what `review: manual` does on adoption is undecided.
- [Low] SEC-14: the public tutorial publishes secret-mount paths.

## Triage

Severities are the reviewers' and are not changed. Where several lenses raised one defect, the row names them
all.

| Finding | Severity | Disposition | Note (revision 3) |
|---|---|---|---|
| The QA tool loses the inbox (comp, critic, readiness, SEC-7) | Blocker | PROPOSE FIX | The QA-tools client gets `{inbox}`. TC-772 asserts that the QA token still reaches the three inbox routes. |
| Ownership in `apiRefs` (critic B; comp, zoom-out, readiness M; SEC-3) | Blocker | PROPOSE FIX | `Task.apiOwner` is immutable and set only on create. `apiRefs` holds read grants only: one per client, at most 8, and the owner is never evicted. Caps, the exclusion and cancel all key on `apiOwner`. |
| Packet built under the store lock (critic, readiness) | Blocker | PROPOSE FIX | `bundlePass` builds the context **outside** the lock into `bundleCtxCache`, keyed by card key. The hook only reads it. `apiClaimLocked` refuses a test-planner card whose cache entry is missing, and the next `bundlePass` refills it. |
| RUNNING at merge never finished (comp B; critic, readiness M) | Blocker | PROPOSE FIX | `finishMergedReviews` runs on every poller tick over REVIEW `pr-reviewer` cards whose row is `merged`, independent of `pollRow`. A locked helper is shared with the merge branch. |
| Cursor: two writers, no flush, no mechanism (readiness B; comp, critic M) | Blocker | PROPOSE FIX | `Drain` is the only writer. Filtered and skipped frames travel the channel as no-op markers. The write is monotonic. On retry, `Drain` closes `c.restart`, and the reader ends the session and drops queued frames by session generation. |
| `flowRetry` is a poison pill, and a store retry is useless (comp, critic, readiness, SEC-2) | Major / High | PROPOSE FIX | Retry only on a transient GitHub error (network, 5xx, 429) or a failed `Masters` read. 404/403/422 and store errors are `flowDone` and logged, with the poller as backstop. Five attempts per seq, with backoff kept across sessions; then the event is skipped and the Integrations row shows `gave up seq n`. |
| The shared planner emits false events (zoom-out, critic, readiness, SEC-10) | Major / Low | PROPOSE FIX | `planCardsLocked` takes prepared items (key, id, title, spec) and owns only dedup, cap, ordinal, cards and `TaskPlanned`. The PR lifecycle events, the moot moves and the row stay in `PlanPullRequestCards`. The id schemes are given. |
| The defect fixes wait on approval (zoom-out) | Major | PROPOSE FIX | §3 is marked as fixes that may ship before the rest is approved. They need no decision. |
| Template hard-coded, no switch (zoom-out) | Major | PROPOSE FIX (partly DECLINE) | A per-product opt-in, `GAOS_PR_TEMPLATE_PRODUCTS`. Both alert channels stay, because the operator asked for an alert and they use WhatsApp. Each fires once per PR. |
| Lookup and plan race; adoption semantics; admin token; mismatched replay (comp, readiness, critic, SEC-8, SEC-13) | Major / Med / Low | PROPOSE FIX | `PlanAPICard` repeats the lookup under its lock. Adoption never changes the label or `autoDispatch`, sends no notice, and answers the card as it stands. The admin token may GET but not POST or cancel (403). A replay with a different body is `409 externalId-reused`. |
| Joined time; field writers; late PRs; rows missing or pruned (comp, readiness, critic) | Major | PROPOSE FIX | `bundledAt` and the `links` JSON are stored on the row. `ApplyBundleLinks` does own-code, adopt and supersede in one transaction, on every body read and when a row is created. `bundlePass` creates a `review` row for a linked PR in a followed repository. Rows of an unplanned bundle are not pruned. |
| Alert (b) on secondaries (comp, critic) | Major | PROPOSE FIX | (b) is exempt for any row with a `bundleKey`, and its grace is 60 minutes. The residual race is listed in §10. |
| Retry duplicates drafts (comp, critic, readiness) | Major | PROPOSE FIX | The draft code is `L-G<8 hex of sha256(product|bundleKey)>-<index>`, and the QA tool upserts it, so a retry overwrites the drafts. |
| New notice kinds cannot be opted into (critic) | Major | PROPOSE FIX | `notifyKinds` (`contacts.go:67`) gains `review:manual`, `review:needs-manual` and `pr:no-task-code`, and the contact form follows. |
| `check-stacks.sh` fails on the directories (critic, readiness, SEC-12) | Major / Low | PROPOSE FIX | The loops exempt the two names and require `-d`, mode 700 and the owner; the tokens inside are mode 600. `:158` is replaced, and a `hasnt` is added for the old mount. The clients file is refused when others can read it. |
| Poller wiring (readiness) | Major | PROPOSE FIX | `newFixPRPoller(st, gh, nt, post)`. |
| Release order and `NotifyPR` (all) | Major / Nit | PROPOSE FIX | `NotifyPR` is under D-100, in v0.62.0. v0.63.0 depends on v0.61.0 and v0.62.0. The assertion moves to TC-794. |
| Rollback ignores the role (critic) | Major | PROPOSE FIX | The role, its regexps and the seat ship in v0.61.0, so a v0.62.0 rollback lands on code that knows the role. |
| Persistence fix tested in memory only (readiness) | Major | PROPOSE FIX | The new TC-771 is an Arcade conformance case. |
| Daily-cap count scope; bundle eligibility (readiness, critic) | Major / Nit | PROPOSE FIX | The count covers `autoDispatch` cards on `wf-pr-<p>` with `AutoAt` today. Bundle cards are eligible only on listed products, and the checklist says so. |
| Docs made wrong (critic) | Major | PROPOSE FIX | Added to the change list. |
| SEC-1 spend unbounded under `on` | High | PROPOSE FIX | `pr-review` only for an open PR. `GAOS_API_DAILY` (default 20) limits created cards per client per UTC day, counted on `apiAt`, in every mode. |
| SEC-4 `reason` leak | Medium | PROPOSE FIX | `reason` is mapped to a closed public set: `held`, `failed`, `blocked` or `""`. |
| SEC-5 `qa-run/` key without the client | Medium | PROPOSE FIX | The client is in the key, and `qa-run/` keys are never adopted. |
| SEC-6 `automation-*` ignores `off`; loops | Medium | PROPOSE FIX | `409 runner-off` under `off`. Created cards (cancelled ones included) count toward `GAOS_API_DAILY`. |
| SEC-9 decode before the slot | Low | PROPOSE FIX | The gate, the capability, the product, then `beginWrite`, then decode. |
| SEC-11 template picks the bundle | Low | ESCALATE | Members are non-strangers. The page shows `waiting on <ref>`. An operator "detach" action is OQ-43. |
| SEC-14 mount paths published | Low | PROPOSE FIX | The public page names no container path; the operator part lives in `deploy/README` text. TC-784 also fails on a `gaos_` token literal and on product ids other than `p-2`. |
| `secret-held` store path (critic, readiness) | Nit | PROPOSE FIX | `HoldReview` sets the label and the reason in one event. |
| Autorun is env, not Master (zoom-out) | Nit | DECLINE | It matches the existing per-product env lists (`GAOS_WA_PRODUCTS`, `GAOS_API_QA_PRODUCTS`). A Master form adds a schema and a UI for a list the operator sets once. |
| Existing body parser (zoom-out) | Nit | PROPOSE FIX | `parsePRTemplate` also returns `QACase`, and `caseCodeFor` calls it. |
| `testNotes` stored (zoom-out) | Nit | PROPOSE FIX | Not stored. `bundlePass` reads the bodies when it builds the context. |
| Anchors; the `off` column; route count; globs; the overlap reason; SEC-n and BR glosses; the card id; the agent seat; deploy follow-ups (all) | Nit | PROPOSE FIX | Applied. The seat needs no DB row: the scheduler seats from the in-memory fixture (`scheduler.go:400-414`). |
