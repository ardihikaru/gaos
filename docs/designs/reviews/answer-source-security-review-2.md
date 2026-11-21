# Security review: `docs/designs/answer-source.md` v2 (D-083, BR-57), round 2

## Verdict

**Approvable on security grounds.** No Critical or High finding is open (`docs/rules/gaos-design-review.md:97`). Round 1's SEC-1 (Critical) and SEC-2 (High) are fixed in v2. Three Medium findings are new and should be fixed before release; six Low follow.

## Note on the rubric

The rubric assumes `signedIn()` accepts any cookie. No longer true: `dashboard/auth.go:361-378` checks the MAC and `auth.go:527-562` refuses every unlisted route without a session. `actorOf` (`handlers_conversation.go:80-83`) is sound.

## Trust boundaries touched

| Boundary | Authenticates | Authorises | Validates | If the far side fails |
|---|---|---|---|---|
| Client → Traefik (`/api`, stripped) → dashboard | bearer in a file, hashed, constant-time | scope ladder, product list, owner rule | strict decode ≤ 1 MiB, two secret passes, `actor` alphabet | 503/401/403/404/429, fail closed |
| Dashboard → QA tool webhook | outbound bearer | only `qa-tools` items are sent | — | retry, give up; GET is the fallback |
| Operator browser → back office | verified session + cross-origin check | any signed-in account | store's answer checks | — |
| Dashboard → ArcadeDB | database user | — | `Event.actor` holds an email or `<client>/<username>` | unchanged |
| Repository → host | file permissions | — | `warnIfOpen` (warning only) | bad registry line skipped |

Handled: replay, IDOR (404 for unknown and others' items), path traversal (`api_inbox.go:33`), script injection via `actor`, oversized body, secrets in logs, unauthenticated pages (`/ui/needs` renders only the chip, `chrome.html:93`; `GET /events` needs a session).

| Round 1 | v2 |
|---|---|
| SEC-1 | fixed, §3.3 items 1–3 |
| SEC-2 | fixed, §3.3 item 1, §3.7 |
| SEC-3 | fixed, `apiSourceOf` |
| SEC-4 | fixed, §3.2 `apiProduct` |
| SEC-5 | fixed, per-asker cap |
| SEC-6 | partly; see SEC-5 below |
| SEC-7 | wording fixed; the log line does not work (SEC-2 below) |
| SEC-8 | partly; see SEC-6 below |
| SEC-9 | fixed, not stored |
| SEC-10 | fixed, §3.10 |

---

### SEC-1 The token-collision check cannot be built from what `apiConfig` holds, and the "enabled for a registry client" rule can reopen an API the built-in collision rule closed - Severity: Medium
- Component: `newAPIConfig`, `addClients`, BR-52 fail-closed
- Evidence: `dashboard/api_v1.go:109-113` (returns before the digests are set on a collision); `:102-124` (no outbound digest stored); `docs/designs/answer-source.md:130`, `:133`, `:168`; `pustaka/docs/product/brd.html:1345`.
- Attack path: (1) a registry token file holding GAOS's outbound token loads (no outbound digest to compare); the QA tool, which receives that token on every webhook call, can then call GAOS as that client. (2) Two built-in tokens equal: `hasAdmin`/`hasQA` stay false, and line 168 read literally re-enables the API for a registry client.
- Remediation: store the outbound digest; a built-in collision `why` is final; TC-493: a registry token equal to the outbound token is skipped, and a built-in collision plus a valid registry client still gives 503.
- Effort: quick-win

### SEC-2 The address in the new log line is always Traefik's, so the R1 mitigation does not work - Severity: Medium
- Component: the answer route's server-log line, R1
- Evidence: `docs/designs/answer-source.md:84`, `:398`; `deploy/stacks/compose.prod.yaml:32-42`; `dashboard/api_v1.go:22-26`.
- Attack path: a leaked-token holder answers from anywhere; every log line names Traefik's internal address.
- Remediation: log the client address Traefik forwards (the entry Traefik appended, not the leftmost; Traefik honours `X-Forwarded-For` only from trusted peers, `deploy/stacks/traefik.yml:25-36`), labelled as forwarded; or remove the claim. Test it.
- Effort: small

### SEC-3 The webhook carries no answer source, yet now delivers the QA tool's own answers alongside the operator's - Severity: Medium
- Component: the webhook and §7
- Evidence: `dashboard/inbox_worker.go:38-47`; `docs/designs/answer-source.md:46`, `:202`, `:367`, `:382`.
- Attack path: before, a GAOS-pushed decision could only be the operator's. Now a QA-tool user (unverified name) or a QA-token holder can answer "Approve" and the webhook is byte-identical to an operator's approval. QA-tool logic that treats a GAOS-delivered decision as operator sign-off loses that guarantee silently.
- Remediation: add `source` to the webhook body, or have §7 and BR-57 say a decision is the operator's only when `source.kind` is `gaos`, the webhook is a notification, and a gating decision is confirmed by GET.
- Effort: quick-win

### SEC-4 An item's owner is a reusable client name - Severity: Low
- Component: `effectiveAsker`, the registry
- Evidence: `docs/designs/answer-source.md:72`, `:166`, `:197`, `:252`
- Attack path: a removed registry name given later to another party with a new token reads and answers the old client's items.
- Remediation: state that a client name is never reused, or record a token fingerprint in `askedBy`.
- Effort: quick-win

### SEC-5 Reserved names and actors do not stop look-alikes of GAOS or the operator - Severity: Low
- Component: registry parsing, the phrase and the label
- Evidence: `docs/designs/answer-source.md:161`, `:91-95`, `:228`, `:105`
- Attack path: a registry client named `gaos` or `qa-tool` reads "via gaos (…)"/"qa-tool asks:"; any client can report the username `operator` or the operator's email, so `/history` shows "via QA tool by operator" next to a real "in GAOS by operator".
- Remediation: reserve the names `gaos`, `qa-tool`, `operator`; refuse a client username that is `operator` or equals a back-office account email.
- Effort: quick-win

### SEC-6 Registry token files are only warned about, not refused - Severity: Low
- Component: `readTokenPath`, the clients file
- Evidence: `dashboard/auth.go:249-253`; `docs/designs/answer-source.md:134`, `:154`, `:400`
- Attack path: a group- or world-readable token file for a new client still loads.
- Remediation: fail closed for registry lines: skip a line whose token file is group- or world-readable; say so in R3 and TC-493.
- Effort: quick-win

### SEC-7 The host-side name of a registry token file is not tied to the gitignored pattern - Severity: Low
- Component: §7b, deploy layout
- Evidence: `.gitignore:9` (`deploy/stacks/*.api-*`); `docs/designs/answer-source.md:387-389`; `deploy/stacks/compose.prod.yaml:139-141`
- Attack path: an operator names the file outside the ignore pattern and commits a live token.
- Remediation: fix the host name as `deploy/stacks/prod.api-client-<name>`, mounted at `/run/secrets/api-client-<name>`; when first wired, assert in `check-stacks.sh` that every registry mount source matches `*.api-*`.
- Effort: quick-win

### SEC-8 The write slot for registry clients is never allocated in the spec - Severity: Low
- Component: `apiClient.busy`, `beginWrite`
- Evidence: `docs/designs/answer-source.md:120`, `:138`; `dashboard/api_v1.go:225-236`
- Attack path: a nil `busy` panics the request; a shared flag lets one client 429 another.
- Remediation: `parseClients` gives each registry client its own `atomic.Bool`; TC-493: 429 on its own held slot, not blocked by the QA tool's.
- Effort: quick-win

### SEC-9 A client-reported username may be an email and is kept forever - Severity: Low
- Component: `AnswerSource.Actor`, `Event.actor`, event text
- Evidence: `docs/designs/answer-source.md:107`, `:228`, `:367`; `dashboard/schema/07-events.sql:26`
- Attack path: a QA-tool user's email copied into an append-only stream, a queryable column and the timeline, with no retention rule.
- Remediation: §7 and BR-57 say a client sends a handle, not an email; BR-57 records the actor is retained indefinitely as personal data.
- Effort: quick-win

Counts: Critical=0 High=0 Medium=3 Low=6
