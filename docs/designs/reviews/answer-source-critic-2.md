# Design review: `answer-source` v2 (critic lens, round 2)

**Isolation note.** No deny-listed file opened. The harness attached a stale copy of `.claude/rules/qa-test-plan.md` (TC-477); the worktree file (TC-489, `.claude/rules/qa-test-plan.md:50,129`) was used.

**The case against the design.** v2 fixes round 1's main defect: every rule follows the asking client. The weak spots are the safety claims in the design's own words: "the server log records the address" logs the proxy, and BR-52's equal-token rule can fail open once a clients file loads. And the registry, with no user, already changes the contract for the one real client (a new 409 on ask) without telling it in §7.

### The logged "remote address" is Traefik's, so R1's mitigation does not exist - Severity: Major
- Affected scope: §3.1, §9 R1, BR-57
- Finding: every `/api` request comes through Traefik (`dashboard/api_v1.go:22-26`, `deploy/traefik/dynamic.yml:17`), so `r.RemoteAddr` is the proxy. Nothing reads `X-Forwarded-For` (only `X-Forwarded-Prefix`, `api_v1.go:180`, `render.go:189`). Traefik trusts it only from `trustedIPs` (`deploy/stacks/traefik.yml:21,40`).
- Recommendation: log the right-most hop Traefik appended, trustworthy because Traefik discards the header from untrusted peers (`deploy/STACKS.md:242`); or drop the claim and record the limit. Assert it in TC-492.

### BR-52's fail-closed rule is undefined once a clients file is present - Severity: Major
- Affected scope: §3.2, §3.10
- Finding: on equal built-in tokens `newAPIConfig` returns early with `why` set and no digests (`api_v1.go:109-113`), and never stores the outbound digest (`:102-123`). "If `hasAdmin` and `hasQA` are both false but a registry client loaded, the API is enabled" is exactly that state: a misconfiguration that disables the QA sync is re-opened by any client line. Whether `qa-tools` is appended before or after the early return is open.
- Recommendation: keep all three digests (new `outSum`) before deciding `why`; `addClients` clears `why` only when it is "no API token is configured"; TC-493 negative: admin == QA plus a valid registry line gives 503 everywhere.

### `id-taken` lets a registry client block the QA tool's ids, and the QA tool is not told about the new 409 - Severity: Major
- Affected scope: §3.3 item 1, §4, §5 L4, §7
- Finding: QA ids are predictable (`run-42:case-7`, `docs/designs/qa-inbox-api.md:106`); a registry client in the same product can take one first and the QA tool gets 409 on a question it must ask (today 201/200, `api_inbox.go:175-179`) — a denial of service §5/§9 do not list. §7 covers only the answer route.
- Recommendation: record the limit with its precondition (overlapping products), or refuse at load a registry client whose products overlap the QA tool's; add a §7 item: `id-taken` means choose another id, never transient.

### The projection never carries `AnsweredBy` onto the item - Severity: Nit
- Affected scope: §3.3, §3.5
- Finding: `externalItemsLocked` copies only four fields from the answer event (`external_inbox.go:112-115`).
- Recommendation: say the fold copies `AnsweredBy` (`:113`).

### "QA tool" is still hard-coded in places a registry client's item reaches - Severity: Nit
- Affected scope: G3, §3.3, §3.7
- Finding: `external_inbox.go:259` (expiry text), `questions.go:249` (expiry toast), `external_inbox.go:182` (cap refusal), `:198` (ask toast), `handlers_bo.go:731` (no-text fallback), `handlers_bo.go:844` (not-waiting sentence).
- Recommendation: list the six and build each from `Label`/`Asker`.

### `dashboard/seed.go` is missing from the change list - Severity: Nit
- Affected scope: §3.9, §3.2
- Finding: `seed.go:182` (`apiGate`), `seed.go:186` (`beginWrite`).
- Recommendation: add it.

### Phase 1's exit condition cannot be met as scoped - Severity: Nit
- Affected scope: §10
- Finding: `handlers_questions.go:65,113` are deferred to phase 3.
- Recommendation: move them into phase 1.

### Small count and anchor errors - Severity: Nit
- Affected scope: §3.2, §0
- Finding: 19 `newAPIConfig` test calls, not twenty; `compose.prod.yaml:130` passes a variable, not `gaos.example.org` — the only anchor is `docs/designs/bo-login.md:214`.
- Recommendation: correct the count; label the production value as the operator's deploy-line setting.

### An unset origin makes `addClients` drop every client that has no origin - Severity: Nit
- Affected scope: §3.2
- Finding: QA origin `""` until set; a line with `-` matches and is dropped; upper case slips past an exact match.
- Recommendation: compare only non-empty origins, ASCII case-insensitive; both cases in TC-493.

### The registers still say "events carry no actor" - Severity: Nit
- Affected scope: §3.10, §3.9
- Finding: OQ-11 (`open-questions.html:519`) and OQ-34 (`:811-813`) say the same; BR-48's new wording omits D-082's conversation actor (`model.go:271-274`); BR-57 governs BO task answers too yet sits in §9.8 (`brd.html:1333`).
- Recommendation: narrow OQ-11/OQ-34 or say why not; cite D-082 and D-083 in BR-48; justify §9.8 in D-083.

### The webhook can arrive before the 200 - Severity: Nit
- Affected scope: §3.4, §7
- Finding: `Kick` is non-blocking (`inbox_worker.go:72-80`); the worker may POST before the QA tool reads its 200; the webhook carries no `source`.
- Recommendation: §7 says the webhook may come first with the same decision; read `source` from the 200 or GET.

### The conversation line repeats "answered" - Severity: Nit
- Affected scope: §3.7, §3.1
- Recommendation: bare phrase on this surface.

**Solid:** the anchors checked hold; `AnswerClosed` follows `Is(ErrRejected)` (`store.go:207-212`, `egress.go:326`); BR-05 closed; no packet carries the source (`store_mem.go:1445-1451`); counts follow (`check-workspace.sh:112-131`, `decision-log.html:18,1683`).

Counts: B=0 M=3 N=9
