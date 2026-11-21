# Back-office login: a session nobody can forge

Status: **draft v4** (2026-11-15), after review rounds 1 (5 reviewers), 2 and 3. Author: the Elephant.
Closes the blocking precondition in BR-48 and answers OQ-11 (authentication approach) for the back office.

## 1. Problem

`signedIn()` (`dashboard/handlers_bo.go:31`) accepts any request carrying `gaos_bo=1`; `POST /auth/login` only checks
that the email contains `@`. Worse, review round 1 found that **gating by calling `signedIn` per handler is itself
incomplete**: `GET /events`, `GET /replay/{workflow}` (a GET that parks the whole store and
freezes every write, BR-20), `GET /tasks/{id}/context` (the model's exact context), `GET /products/{id}/readiness`,
`GET /ui/cw` and `POST /ui/cw/step/{n}` never check anything. A real cookie check would leave them open.

So the fix has two halves: make the session real, and make the gate **default-deny at the mux** instead of
opt-in per handler.

## 2. Goal and non-goals

Goal: a request reaches a non-public route only with a cookie the server issued after a correct password, not yet
expired, signed with a key an attacker lacks; any route nobody classified is refused.

Non-goals: roles and per-product authorisation (all operators are equal, OQ-34 stays open and is *unblocked* by this,
not answered); SSO (OQ-19); password reset or change UI; the bearer API `/v1` (own tokens, D-072); the tenant
selector (still cosmetic); audit identity on events (follow-up, touches the closed event catalog BR-05).

## 3. Design

### 3.1 Reuse: lift pustaka's auth, with named changes

`pustaka/main.go` already has the session token (`sign`/`verify`, 730-765), credential fingerprint (712-716), the
throttle with sweep (1188-1254), `clientIP` ignoring `X-Forwarded-For` (1173) and the login Origin check
(1082-1085). The dashboard is a separate Go module, so this is a **copy** into `dashboard/auth.go`, with exactly
these changes: (a) the cookie names the account; (b) the credential is a per-account PBKDF2 hash instead of one
plaintext pair; (c) the throttle is per account and never evicts a locked entry (3.4); (d) a deployment label is
bound into the MAC. Verification keeps pustaka's order: **MAC first, parse after**, so attacker-shaped bytes never
reach field logic.

### 3.2 Accounts: a file

`GAOS_BO_USERS_FILE` names a text file, one account per line: `email:pbkdf2-sha256$<iterations>$<salt b64>$<hash b64>`.

- Blank lines and `#` lines ignored. Email lower-cased, must contain `@`, at most 254 bytes, must not contain `:`
  or `.` is fine; `|` and control characters are rejected (they would break the MAC input). A malformed line is
  logged by line number, never by content, and skipped; a duplicate email keeps the first.
- Hash: PBKDF2-HMAC-SHA256 (Go 1.24 stdlib `crypto/pbkdf2`, so the module stays dependency-free), 16-byte random
  salt, 32-byte output, **600 000 iterations** for new hashes. A line is accepted with 100 000 to 5 000 000
  iterations, so a hostile file cannot turn login into a CPU sink.
- Read once at start. Editing the file takes effect at the next restart.
- Both files are mode **0600** and **sensitive** (the hash allows offline guessing; with the key it lets
  an attacker forge any session). The process logs a warning (does not refuse) when either is group- or
  world-readable; the container runs as the deploy user's uid (`compose.prod.yaml:128-133`) so 0600 works, and
  refusing would turn a permission slip into an outage.
- `dashboard hash-password`: dispatched in `main()` **before** `flag.Parse` when `os.Args[1]` is that word. It
  reads the password from stdin (one line, never argv), prints one `pbkdf2-sha256$...` string, nothing else. On a
  VM: `docker run --rm -i --entrypoint /dashboard <image> hash-password < pwfile` (the image is distroless; the
  Dockerfile's ENTRYPOINT is `/dashboard --addr :8080 --prod`, the override supplies the subcommand).

### 3.3 Fail closed, and the 422

With no valid account (variable unset, file unreadable, or every line rejected) nobody can sign in: `POST
/auth/login` answers **422** with the login sheet and the text "Back-office sign-in is not configured on this
server." (422, not 503: htmx swaps only 2xx-4xx, `templates/page.html:12`, so a 503 would show nothing). There is no
open mode; the tests and the scratch stack set a users file like everyone else. Rollout therefore writes the files
**before** the deploy (section 6).

### 3.4 The session cookie and the throttle

`gaos_bo = v1.<b64url(email)>.<expiry unix>.<fp>.<b64url(mac)>`; `fp` is the first 16 hex characters of
`SHA-256(stored hash string)`; `mac = HMAC-SHA256(key, "v1" "\x00" label "\x00" email "\x00" expiry "\x00" fp)`
(NUL separators; fields cannot contain NUL) where `label` is `GAOS_BO_LABEL` (default `gaos`, set to the host on
each deployment so a cookie, and a copied key file, do not cross deployments).

- Lifetime 12 h absolute, no renewal; at most 512 bytes. `HttpOnly`, `Secure` (`GAOS_COOKIE_SECURE`, as today),
  `SameSite=Lax`, `Path=/`, `Max-Age` = lifetime.
- Order: length, split at the last dot, `hmac.Equal` on the MAC, then fields, expiry in the future, account still
  present, `fp` equal. Any failure is simply "signed out".
- Key: `GAOS_BO_SESSION_KEY_FILE`, **raw bytes, whitespace-trimmed, no decoding, at least 32 bytes** (generate with
  `head -c 48 /dev/urandom | base64 -w0 > prod.bo-session-key`). Unset, unreadable or short: a warning is logged and
  a random per-process key is used (sessions die at restart, which is the safe fallback, not a weak key).
- Revocation: stateless. A password change in the file, a removed account, or a different key invalidates sessions
  **after the dashboard restarts with the edited file** (the fingerprint matters only when the key file persists
  across restarts; without it a restart already ends every session). Logout clears the cookie but cannot
  invalidate a stolen copy before expiry. Stated on the docs page.

Throttle (copied shape, changed). Two tables, because a known account must never be starved by traffic aimed
at made-up emails:

- **Known accounts** (an entry per email in the users file; at most the file's size, never refused for capacity,
  never evicted). Five failures within a 15-minute window that starts at the first failure lock the account for
  15 minutes: further attempts answer 422 "Too many attempts. Try again in N minutes." (N = ceil minutes, plus
  `Retry-After` in whole seconds, ceil) **without checking the password**. A success deletes the entry.
- **Unknown emails**: one shared table, at most 4 096 keys, evict-oldest (safe: they can never succeed). They
  never show a lock message; the answer is always the ordinary "That email and password do not match." after the
  dummy-hash verification.
- No per-address lock (behind Traefik every client shares an address; round 1).
- CPU bound: two semaphores, **4 slots for known-account verifications and 2 for unknown-email ones**, so an
  unknown-email flood cannot occupy a known account's slots. A request that cannot get a slot within 2 s answers
  422 "The server is busy. Try again in a moment."

Accepted limits: someone who knows an email can lock that account for 15 minutes (denial, not intrusion), and the
lock message tells them the account exists. Unknown emails are indistinguishable from wrong passwords.

### 3.5 The gate: default-deny at the mux

`routes()` registers through two helpers that also record the pattern: `s.handle(pattern, h)` (back office and
public) and `s.handleBearer(pattern, h)` (used by `registerAPI`, `api_v1.go:271-280`, which today registers on the
mux directly and is therefore edited). `routes()` resets `s.patterns`/`s.bearer` first (`do()` in tests calls it
per request). It returns `s.gate(mux)`. **`r.Pattern` is empty outside the mux**, so the gate calls
`_, pat := mux.Handler(r)` to find the matched pattern; an empty `pat` (404/405) is passed to the mux, which
answers 404/405 as today (nothing is revealed and nothing is public). Classes, by **exact** membership:

| Class | Patterns | Signed out |
|---|---|---|
| **public** (fixed list in `auth.go`) | `GET /{$}`, `GET /healthz`, `GET /static/`, `POST /auth/login`, `POST /auth/logout`, `GET /ui/needs`, `GET /ui/bo` | handler runs; the UI ones already answer a login sheet / zero count |
| **bearer** | exactly the patterns in `s.bearer` | handler runs (own token check, unchanged); exempt from 3.6 |
| **back office** | every other pattern, including `GET /events`, `GET /tasks/{id}/context`, `GET /products/{id}/readiness`, `GET /ui/cw`, `POST /ui/cw/step/{n}`, `POST /replay/{workflow}` and **any route added later** | **401**: the login sheet for an htmx request (`HX-Request`), JSON `{"error":"sign in to the back office"}` otherwise |

`GET /replay/{workflow}` changes global state, so it becomes **`POST /replay/{workflow}`** (no template, static
file or test calls the HTTP route; tests use `Store.Replay`, and `TestEveryRouteAnswers` `main_test.go:76-77`
sends `GET /replay/floor` expecting 200: it now expects 405 for the GET and 401 for the POST with no cookie, its TC
row prose updated). The gate stores the verified session (email, expiry) in the request context for handlers.
`GET /events/stream` already calls `requireBO` (`broker.go:115`); it additionally closes itself at the session
expiry from that context. Existing per-handler `requireBO`/`requireBOJSON` calls stay (defence in depth);
`requireBOJSON` becomes a method on `*server`. The free `signedIn(r)` is **deleted**, replaced by
`s.auth.signedIn(r)`; call sites: `handlers_bo.go:112,125,619`, `handlers_tasks.go:152`, `main.go:449`.

Tenant label: `tenantOf` returns the tenant name from `gaos_tenant` (which now carries only the tenant,
`"loop-eng"`); the header shows `<tenant> · <verified session email>`. `TestTenantCookieSurvivesNonASCII`
(`main_test.go:261`) therefore asserts the tenant cookie round-trips a non-ASCII tenant and the label carries the
session email.

### 3.6 Cross-origin requests

`SameSite=Lax` does not protect same-origin script (the docs pages share the dashboard's origin behind Traefik) or
login/logout (no prior cookie). The gate therefore also refuses, with 403, any non-GET request, **including
`/auth/login` and `/auth/logout`**, that carries `Sec-Fetch-Site` other than `same-origin`/`none`, or an `Origin`
whose host differs from `Host` (pustaka's `sameOrigin`). `/v1` (bearer) patterns are exempt. A request with neither header (curl, tests) passes the
origin check but still needs the session. State-changing routes are therefore POST/PUT/DELETE only, and a test
fails if a GET pattern outside the fixed read-only list is registered.

### 3.6a The login handler's order

`POST /auth/login` does, in this order, stopping at the first that answers: (1) origin check (403, 3.6);
(2) no accounts configured (422 "Back-office sign-in is not configured on this server."); (3) `canonEmail`
fails or password empty (422 "Enter the email and password you use for this server.", counts nowhere);
(4) known account locked (422 lock text, no verification, no failure counted); (5) take a semaphore slot (known or
unknown pool; 422 busy text on timeout, counts nowhere); (6) verify against the stored hash (or the dummy);
failure: known account counts a failure, unknown email records in the unknown table, both answer "That email and
password do not match." (422); success: delete the account's entry, set the cookies, answer as today. Only step 6
counts failures. `TestLoginRejectsANonEmail` (`main_test.go:247`) keeps its assertion with the new step-3 text.

### 3.7 Login form

Remove `value="ardi@loop.eng"` and `value="demo"` and the "No password is checked yet" note
(`templates/backoffice.html:29,33,39-43`); replace with one line: "Accounts are set by whoever runs this server."
The stale "placeholder / forgeable cookie" comments in `handlers_bo.go:15-24`, `handlers_qa.go:15`, `api_v1.go:20`
and `handlers_tasks.go:146-150` are rewritten. Frontend rule: `.claude/rules/frontend.md` applies to the template.

## 4. Changes, by phase (paths from the repository root; exit command for each)

| Phase | Files | Exit |
|---|---|---|
| 1 auth core | new `dashboard/auth.go` (token, `canonEmail`, users parser, PBKDF2, two-table throttle, semaphores, gate, origin check), new `dashboard/auth_test.go` | `cd dashboard && go test -run 'TestAuth\|TestSession\|TestThrottle\|TestUsers\|TestHash' ./...` green |
| 2 wiring | `dashboard/main.go` (`handle`/`handleBearer`, `gate`, subcommand dispatch before `flag.Parse`, `s.auth`, line 449), `dashboard/api_v1.go` (`registerAPI` via `handleBearer`; comment at :20), `dashboard/handlers_bo.go` (login order 3.6a, cookies, `tenantOf`, call sites 112/125/619, header comment), `dashboard/handlers_tasks.go` (`replay` POST, `requireBOJSON` method, comment 146-150), `dashboard/broker.go` (close at expiry), `dashboard/handlers_qa.go:15` (comment), `dashboard/main_test.go` (`newTestServer` builds a test authority from a fixed key + account; `boCookies()` signs with it via a package-level test authority; `TestEveryRouteAnswers`, `TestLoginRejectsANonEmail`, `TestTenantCookieSurvivesNonASCII` updated per 3.5/3.6a), `dashboard/qaclient_test.go:235` and `dashboard/qacatalog_test.go:353` (use `boCookies()`) | `cd dashboard && go test ./...` green |
| 3 template and docs | `dashboard/templates/backoffice.html` (3.7), new `pustaka/docs/guide/bo-sign-in.html`, `pustaka/docs/assets/toc/guide.js` (register it), `pustaka/docs/product/brd.html` (BR-48 rewritten; new BR-55; count 54→55), `pustaka/docs/product/decision-log.html` (new D-078 row quoting `docs/designs/qa-inbox-api.md`, new D-079; "Seventy-seven"→"Seventy-nine"), `pustaka/docs/product/open-questions.html` (OQ-11 answered by D-079 and moved; "twenty-one answered"→"twenty-two" and the matching open count; OQ-34 "why open" text), `pustaka/docs/delivery/implementation-plan.html` (the BR-48 line on M17; T-089), `pustaka/docs/product/brd.html` F-6/R-02 rows that cite OQ-11, `pustaka/docs/product/qa-test-plan.html`, `pustaka/docs/assets/toc/product.js` (counts), `pustaka/docs/assets/toc.js` (`product.semver` 0.38.0→**0.39.0**), `pustaka/docs/changelog.html` | `cd pustaka && go run . check ./docs` clean; `go test ./...` green |
| 4 deploy | `.gitignore` (`deploy/stacks/*.bo-*`), `deploy/rebuild-restart.sh` (link `*.bo-*` into the tag worktree; **refuse** with a `bad` line when `prod.bo-users` or `prod.bo-session-key` is missing, next to the API-token check at :161-170), `deploy/stacks/compose.prod.yaml` (two read-only file mounts; `GAOS_BO_USERS_FILE`, `GAOS_BO_SESSION_KEY_FILE`, `GAOS_BO_LABEL: ${GAOS_BO_LABEL:?set to the host}`), `deploy/stacks/dashboard.env.example`, `deploy/check-stacks.sh` (both files untracked regular files; label required), `deploy/check-workspace.sh` (path list :27 gains `*.bo-*`; secret pattern :198 gains `pbkdf2-sha256\$[0-9]+\$` and a 40+ char base64 run), `.claude/rules/agent-workspace.md` (deny-list gains `*.bo-*`, `*.api-*`), the five `.claude/agents/gaos-goldfish-*.md` (their Off limits lines gain the same two), new `.claude/settings.json` (Read deny for `**/*.bo-*`, `**/*.api-*`, `**/*.env`) | `./deploy/check-stacks.sh` and `./deploy/check-workspace.sh` print no FAIL |
| 5 rollout | section 6 | live checks in section 6 |

## 5. Tests and the register

Twelve Go tests and four script assertions, TC-423…TC-438. (As built, TC-435 is the `*.bo-*` untracked/regular-file check, TC-436 the compose label, TC-437 the secret scan, TC-438 the missing-file refusal. While verifying TC-437 the scan's existing regex was found to have an unbalanced parenthesis, so it had never matched anything; it is fixed in this change.) Counts: register 422→**438**; Go tests 304→**316**
(dashboard 282→294); BR 54→55; decisions 77→**78** (D-079 only: D-078 belongs to the QA-inbox design and is recorded when its review passes);
OQ-11 is **narrowed** by D-079, not answered (per-tenant scoping, an identity provider and an actor on events stay open),
and the open-questions lede's stale "twenty-one answered" becomes twenty-two (OQ-33 was answered earlier).

1. `TestAForgedBackOfficeCookieIsSignedOut` (TC-423) — `gaos_bo=1` on a back-office route gives the login sheet.
2. `TestASignedInCookieOpensTheBackOffice` (TC-424) — positive; a wrong password and an unknown email answer the
   same text.
3. `TestACookieThatWasTamperedWithIsRefused` (TC-425) — expired, edited email, edited expiry, truncated MAC,
   oversized, wrong label.
4. `TestAPasswordChangeEndsOldSessions` (TC-426) — an authority built from an edited file refuses the old cookie.
5. `TestWithNoAccountsNobodySignsIn` (TC-427) — 422 with the "not configured" text; `gaos_bo=1` still fails.
6. `TestFiveFailuresLockTheAccount` (TC-428) — 422 even for the right password; success resets; other accounts
   unaffected; email variants (case, spaces) share one counter; a flood of unknown emails neither locks a known
   account nor fills its slots; a locked entry is never evicted.
7. `TestTheUsersFileParser` (TC-429) — comments, blanks, duplicate, malformed, `:` and control characters in an
   email, iterations out of range.
8. `TestHashPasswordRoundTrip` (TC-430) — hash then verify, plus a fixed PBKDF2 vector.
9. `TestEveryRouteIsClassified` (TC-431) — public ∪ bearer ∪ back office equals `s.patterns` ∪ `s.bearer` with no
   overlap; the public set equals the 3.5 list; every other pattern answers 401 with no cookie and with
   `gaos_bo=1`; the empty-pattern case (an unknown path) is not public; the GET patterns not in a hand-written
   read-only list (each entry with its reason) fail the test.
10. `TestACrossOriginPostIsRefused` (TC-432) — `Origin` mismatch and `Sec-Fetch-Site: cross-site` on `/auth/login`,
    `/auth/logout` and a back-office PUT answer 403; same-origin and header-less pass; `/v1` is exempt.
11. `TestTheSessionKeyFile` (TC-433) — trimmed, under 32 bytes falls back to a random key (and logs), unreadable falls back, exactly 32 accepted.
12. `TestTheEventStreamClosesAtSessionExpiry` (TC-434) — in `broker_test.go`, with a cookie whose expiry is 1 s away.
13. `check-stacks.sh` (TC-435) — `prod.bo-users` / `prod.bo-session-key` untracked regular files; (TC-436) `compose.prod.yaml` passes `GAOS_BO_LABEL`.
14. `check-workspace.sh` (TC-437) — a planted `pbkdf2-sha256$600000$…` line in the report store is caught.
15. `rebuild-restart.sh` (TC-438) — a missing `prod.bo-users` makes the tag deploy refuse.

Each row quotes its exact `t.Fatalf` or script `ok`/`FAIL` string, per `.claude/rules/qa-test-plan.md`.

## 6. Rollout and rollback

Two deployments: `gaos.example.org` on rnd-v3-vpn (live) and the original prod on vm-rnd-ardi (not deployed
now; done when asked).

1. On the VM, **the agent never sees the password**. A single ssh command generates it with
   `head -c 18 /dev/urandom | base64` straight into `~/gaos-bo-password.txt` in the deploy user's home (outside the
   repository, mode 0600), pipes that file into the `docker run … hash-password` command (3.2), and writes
   `deploy/stacks/prod.bo-users` (`<operator email>:<hash>`) and `deploy/stacks/prod.bo-session-key` (0600,
   gitignored). Only the file path is reported. The operator reads the password from that file in their own
   terminal, stores it, and deletes the file.
2. Deploy the tag with `GAOS_BO_LABEL=<host>` added to the deploy line.
3. Verify live: `Cookie: gaos_bo=1` to `/app/ui/bo/products` gives the login sheet, `/app/events` gives 401,
   a real login works in a real browser (Playwright, with a throwaway second account added only for the test and
   removed after), the QA tab buttons work after login, `/api/v1` unchanged, a wrong password answers the
   "do not match" text.
4. Rollback: redeploy the previous tag. **That tag has the forgeable cookie**; during any incident rollback keep an
   edge layer in front (Traefik basic auth is a valid extra layer, not a substitute) and prefer fixing forward.

## 7. Alternatives considered

- **Per-handler checks only** (the first draft): rejected, six routes (`/events`, `/replay`, `/tasks/{id}/context`,
  `/products/{id}/readiness`, `/ui/cw`, `/ui/cw/step/{n}`) proved the class of bug.
- **Basic auth at the edge as the only gate:** different edges per deployment, no identity inside GAOS, untestable here.
- **A `User` vertex in ArcadeDB:** bootstrap problem, UI, event-catalog change (BR-05). The file is recoverable by
  anyone with shell access.
- **One credential (`GAOS_BO_USER` + password file), as the zoom-out reviewer proposed:** smaller, and noted. The
  accounts file costs one short parser, and gives the session an email now, which the audit-identity follow-up and
  OQ-34's "who may" need. Kept deliberately; the parser is test 7.
- **Server-side session store:** instant revocation, but a store to persist and coordinate; the restart +
  fingerprint + 12 h bound cover a single-process dashboard.
- **bcrypt/argon2:** not stdlib.
- **Sliding expiry:** a stolen cookie would live forever.

## 8. Risks and accepted limits

- BR-48's clearance now needs "some operator signed in", with no attribution on the event (no audit identity, as
  stated in 2); this is recorded as the first follow-up and BR-48's text says so.
- Same-origin script on the docs pages could still act as a signed-in operator with a valid cookie; the origin check
  stops cross-origin only. No check today rejects an external `<script src>` in a docs page and no CSP is set;
  this is **unmitigated** here and recorded as a follow-up (a `pustaka check` rule and `script-src 'self'`).
- `/healthz` still returns the raw database probe error to anonymous callers (`main.go:422-426`); out of scope here,
  recorded as a follow-up.
- Stateless sessions cannot be revoked individually (3.4). One shared hash file; use generated passwords.

## 9. Open questions

None blocking. D-040 lists per-tenant scoping beside authentication as a precondition for the product to host more
than one tenant; this change is the authentication half only, per-tenant scoping stays a stated non-goal and
OQ-11's row records that split. The operator delegated the choices.

## 10. Decisions from round 3 (these supersede any earlier wording)

- **The hash comes from the image, built first.** On the VM, from the tag's checkout:
  `docker build -f deploy/Dockerfile.dashboard -t gaos-bo-hash:<tag> .` then
  `docker run --rm -i --entrypoint /dashboard gaos-bo-hash:<tag> hash-password < <password file>`. This needs no
  `bo-*` file, so it works before the deploy script's refusal. The subcommand dispatches before `fs.Parse`
  (`main.go:40`).
- **Label:** compose passes `GAOS_BO_LABEL: ${GAOS_BO_LABEL:-}` (no `:?`, so `status`/`logs` keep working); with
  `--prod` and an empty label sign-in is **disabled** (fail closed, same 422 text as "not configured") and a boot
  line says why. TC-436 asserts the compose file passes the variable.
- **`check-workspace.sh` and the docs counts** move with this change: the script's count lines (112-116,
  127-131, 141-144) and `README.md` (lines 8, 100, 106, 110) are updated to the new values (Go tests 316, dashboard
  294, register 438, "Seventy-nine entries", "Fifty-five rules", product version 0.39.0), and
  `.claude/rules/qa-test-plan.md` (next free TC-439, the area map, the counts paragraph). The OQ lede at
  `open-questions.html:17-22` is corrected to the true open count while answering OQ-11.
- **Secret scan:** only `pbkdf2-sha256\$[0-9]+\$` is added (a bare base64 run matches git SHAs already in committed
  reports). The deny-list gains `*.bo-*` only (`*.api-*` is already there); `check-workspace.sh:27` is a comment edit.
- **Test authority:** package-level constants (a fixed key, one account, a hash line at the minimum 100 000
  iterations); `newTestServer` builds a **fresh** authority from them per test, so throttle state never leaks;
  `boCookies()` signs with the same constants. `TestEveryRouteAnswers` gets a per-row option to send no cookie.
  The dummy hash for unknown emails uses the production 600 000 iterations. The unknown-email table's only job is
  bounding memory of repeated unknown attempts for the busy/CPU accounting; it records nothing visible.
- **Gate details:** a value from `mux.Handler` that is not a registered pattern (trailing-slash or path-clean
  redirect) is treated as back office (a signed-out caller gets 401, never the redirect); TC-431 covers it. The
  read-only GET list in TC-431 is the set of GET patterns in `main.go:318-371` that read only; the test file
  writes each with its reason; bearer GETs are in the bearer class.
- `.claude/settings.json` Read denies are kept (`**/*.bo-*`, `**/*.api-*`, `**/*.env`).

## 11. As merged

While merging `main` (PR #9, v0.38.1) a test of the QA-inbox work, `TestAWebhookWhoseRecordFailsIsNotResentEveryTick`, was found without a register row; it is registered as TC-439, so the totals are 439 rows, 317 Go tests (295 in `dashboard/`).
