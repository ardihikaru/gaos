# Security review: `card-timing`

**Reviewed:** `docs/designs/card-timing.md` (v1)

**Isolation breach:** the harness put the contents of `.claude/CLAUDE.md` into my context, which is on the deny-list. It also inserted `.claude/rules/agent-workspace.md` from the main checkout, a path outside this worktree. I did not open either one, and nothing below relies on them. I did not see an isolation canary. The details are in SEC-7.

## What this proposes

Every event gets a wall-clock time field, `Event.At`. It is set when the event is emitted and saved in the existing event payload JSON as `at`. Older rows don't have it, so for them the time comes from the existing `Event.ts` column when the store loads at startup. A pure function, `cardTiming`, walks one task's plan and move events to build a list of status segments. It sorts the time into five buckets that add up to the lead time (AI working, waiting on the operator, queued, blocked, backlog) and defines idle as lead time minus AI time. It also reads each attempt's run records for duration, model, provider, tokens and cost. Per-workflow totals add these up.

User-facing changes:
- The board JSON gets a `timing` summary on every card and a `workflows` list.
- New back-office route `GET /products/{id}/board/cards/{taskId}/timing` returns the full history as JSON.
- New back-office route `GET /ui/timing/{id}/{taskId}` renders a timing sheet.
- Each card gets a lead-time chip that opens the sheet.

There is no new event type, task status, schema statement or deployment.

## How the existing system works, per this document

- Every status change writes a `TaskMoved` event through `moveTaskLocked`.
- The in-memory `Event.T` is a simulation counter, so in production it is always 0.
- ArcadeDB stores `ts = sysdate()` on every event row, but the startup load never reads it.
- `ArcadeStore` answers every read from an in-memory `MemStore` projection.
- Back-office routes are session-gated by `gate`, and handlers also call `requireBO`.
- A GET request passes the cross-origin check.
- Run records hold `StartedAt`/`EndedAt` in app-clock seconds.

## Trust boundaries touched

The only boundary touched is **operator browser → back-office dashboard**. The design adds two GET routes and new fields in the board JSON.

The rubric says `signedIn()` accepts any cookie. **That is out of date.**
- `sessionFrom` verifies the cookie (`dashboard/auth.go:361-382`).
- `TestEveryRouteIsClassified` checks that a forged cookie gets a 401 on every back-office route (`dashboard/auth_test.go:304-318`).
- So the design's claim that the routes are gated and need a session holds, and is enforced by tooling.

The other boundaries are not touched:
- **ArcadeDB:** one more column (`ts`) is read, using the same database user, and nothing new is written.
- **Gateway:** no change.
- **Edge / tunnel:** no change.
- **Published docs:** `graph.html` gets a data-model line and no hostnames or topology.

## Findings

### SEC-1 The per-attempt run data is new HTTP exposure, and the record it comes from carries session and request ids - Severity: High
- Component: `GET …/board/cards/{taskId}/timing`, `GET /ui/timing/…`, `CardTiming.runs`
- Evidence: the design says the route "opens no new data class" (`docs/designs/card-timing.md:177`). In fact, no handler calls `Store.Runs` today: its only callers are the store itself (`dashboard/store.go:68`, `dashboard/store_arcade.go:202-203`, `dashboard/store_mem.go:854-858`). The board card has no cost or token fields (`dashboard/board.go`, no match), and no template shows them (`dashboard/templates/`, no match). `TaskRunRecord` has no JSON tags and holds `SessionID`, `RequestID`, `LogRef`, `Workspace`, `FailureSignature` and `SliceDigest` (`dashboard/model.go:762-798`).
- Attack path: if the implementer embeds or returns `TaskRunRecord` (or `runsFor` output) directly, every signed-in browser receives the gateway session id and request id. BR-41 treats the session as transport, and a retry resumes a session by that id (`model.go:785-791`). Those ids, plus the log location and workspace name, would then reach the browser, its cache and any HTML-rendered sheet. The design lists the fields it wants (`card-timing.md:106,173`) but never says the type is a separate allowlisted DTO, and its security argument rests on a false premise. I rated this High rather than Medium: the exposure is on a privileged path, and the only thing preventing it is how the list of fields happens to be written.
- Remediation:
  - Correct §3.6: per-attempt model, provider, tokens and cost are new on the HTTP surface.
  - Define a named `AttemptTiming` struct with explicit `json` tags, built field by field from `TaskRunRecord`.
  - State in the design that `SessionID`, `RequestID`, `LogRef`, `Workspace`, `SliceDigest`, `FailureSignature` and `Invocation` are never copied into it.
  - Extend TC-474 with a negative assertion that the JSON and the sheet HTML contain none of those keys or a seeded session/request id value.
- Effort: small

### SEC-2 The read-only GET classifier is not in the change list - Severity: Medium
- Component: route classification
- Evidence: `TestEveryRouteIsClassified` fails any GET pattern that is not in `readOnlyGET` (`dashboard/auth_test.go:258-272,310-313`). The change list (`card-timing.md:200-226`) does not name `dashboard/auth_test.go`. TC-473 (`card-timing.md:241`) re-asserts the 401 behaviour that this test already proves for every route.
- Attack path: no direct attack. The control that actually keeps the new GETs session-gated and free of side effects is never named. That leaves an implementer free to "fix" the failing test in a way that weakens the list, for example by loosening the check.
- Remediation:
  - Add both patterns to `readOnlyGET`, each with its reason, and list `dashboard/auth_test.go` in §4.
  - Cite `TestEveryRouteIsClassified` in §3.6 as the enforcing control.
  - Keep TC-473 only for what is new: the HTMX login-sheet path and the fixed 404 body.
- Effort: quick-win

### SEC-3 Unbounded per-read work under the store's read lock - Severity: Medium
- Component: `MemStore.Board` / `decorateBoardTiming`, `MemStore.CardTiming`
- Evidence: `Board` holds `s.mu.RLock()` for the whole projection (`dashboard/board.go:201-210`). The design adds a full pass over the event stream inside that lock on every board read and every timing request (`card-timing.md:127,131-133`). It also states the stream is unbounded (`card-timing.md:133`; `store_arcade.go:1737-1741`). The board re-reads on every task hint (`card-timing.md:198`), and writers need `s.mu.Lock()` (`dashboard/store_mem.go`, 16 sites).
- Attack path: any signed-in session, or just several open board tabs, can issue repeated board and timing GETs. Nothing rate-limits them (`card-timing.md:177`). Each read holds the read lock for O(events), which delays the runner's and the operator's writes. Over time this becomes an availability problem for the runner. Unauthenticated callers are stopped at `gate`, so the exposure is limited to authenticated users.
- Remediation: do one of the following, and add a test with a stated time budget at around 10⁵ events.
  - Maintain the per-task index of `TaskPlanned`/`TaskMoved` events incrementally in `emit`. This is the design's own R1 mitigation, done now.
  - Or copy the product's events under the lock and compute outside it.
- Effort: small

### SEC-4 No per-product authorisation; the new routes inherit "any signed-in account sees every product" - Severity: Low
- Component: `handleCardTiming`, `handleTimingSheet`
- Evidence: `handleBoard` checks only that a session exists, not the account or the product (`dashboard/handlers_board.go:32-36`). There can be several accounts (`dashboard/auth.go:340`). The design says the same `requireBO` pattern will be used (`card-timing.md:177,183`).
- Attack path: any valid account can enumerate `{id}/{taskId}` and read any product's timing, costs and models. This is an IDOR across products. It already exists for the board and is not widened beyond SEC-1's new fields.
- Remediation: state the assumption in §3.6 ("single trust domain: every BO account sees every product") and add a cross-reference to whichever open question covers per-account scoping, so the next design that adds scoping finds these routes.
- Effort: quick-win

### SEC-5 The error path copied from `handleBoard` echoes internal error text - Severity: Low
- Component: `handleCardTiming`
- Evidence: `handleBoard` writes `err.Error()` into the 500 response (`dashboard/handlers_board.go:41-43`). `handleEvents` does the same (`dashboard/handlers_tasks.go:31-33`). The design says the new handler works "like `handleBoard`" (`card-timing.md:177`) and specifies only the 404 body.
- Attack path: if the store errors, any database or driver message is sent to the browser, which tells a signed-in user about internal details.
- Remediation: specify a fixed 500 body (for example `{"error":"timing unavailable"}`), log the cause on the server, and do not log the request's cookies.
- Effort: quick-win

### SEC-6 The chip's URL encoding is unspecified - Severity: Low
- Component: `backoffice.html` chip, `board.js` chip
- Evidence: `data-timing-open="ui/timing/{id}/{taskId}"` (`card-timing.md:181`). The existing answer-sheet URL builder path-escapes both segments (`dashboard/handlers_bo.go:678`). The design does not say the new builder does the same.
- Attack path: task ids are generated by the server today, so I found no current exploit. A future id that contains `/`, `?` or `..` would send the sheet request to a different route or product.
- Remediation: build the URL with `url.PathEscape` on the server and `encodeURIComponent` in `board.js`. Say so in §3.7.
- Effort: quick-win

### SEC-7 The reviewer deny-list is enforced only by instruction, and was bypassed by the harness in this session - Severity: Medium
- Component: the design-review process (reviewer isolation)
- Evidence: the deny-list (`.claude/rules/agent-workspace.md:99-109`) is written as instructions to the agent. In this session the harness inserted the contents of `.claude/CLAUDE.md` into my context, plus a copy of `agent-workspace.md` from a path outside the worktree, without my reading either. There was no secret in either file.
- Attack path: if the harness auto-loaded a credential-bearing file the same way, a reviewer could quote it into the committed, append-only report. Nothing in the tooling prevents that, and `check-workspace.sh` only checks the report store after the fact.
- Remediation:
  - Enforce the deny-list in tooling: harness permission deny rules for the listed globs, applied to the reviewer agents, and confirm the frontmatter isolation setting is honoured by the harness in use.
  - Keep the after-the-fact secret scan in `check-workspace.sh`.
  - Record this occurrence in the review's triage.
- Effort: small

Counts: Critical=0 High=1 Medium=3 Low=3
