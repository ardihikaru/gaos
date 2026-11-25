# gaos-brain-town-consumer — review 2 (critic)

> **Reviewer:** the zero-context critic goldfish
> **Document reviewed:** `docs/designs/gaos-brain-town-consumer.md`
> **Rubric:** `.claude/rules/rule-index.md`, `.claude/rules/qa-test-plan.md`,
> `.claude/rules/agent-workspace.md`, plus the domain sources the rule index names.

## Findings

### The flow design does not exist - Severity: Blocker
- Affected scope: §2; §10; §8; §11
- What is wrong: The reader's only consumer, the de-duplication mechanism, the "replays what it missed" promise and the GitHub client all live in `docs/designs/gaos-pr-agent-flows.md`, which is not in the repository.
- Evidence: `docs/designs/` holds 16 design files; no `gaos-pr-agent-flows.md` exists, and the only two references to it in the whole tree are lines 32 and 223 of this design.
- Fix: Ship the flow design first (or fold its handover contract into this one): the channel's consumer, the dedupe key and its durability, and whether the flow blocks. Otherwise §11's done-when criteria cannot be met.

### Restart replay is contradicted by the storage rule - Severity: Major
- Affected scope: §4.2 vs §4.1 and §8
- What is wrong: §4.2 says the reader "keeps the last sequence number it handled and nothing else", so after a restart there is no cursor — the reader either resumes from "now" (losing exactly the window §8 promises to replay) or re-reads from zero (replaying the whole retention).
- Fix: State where the cursor lives (a file, following the token-file convention), what Brain Town's replay window is, and when the cursor advances.

### A dropped event is permanent loss, not a dropped snapshot - Severity: Major
- Affected scope: §4.1; §8
- What is wrong: The design copies the outbound feed's drop-on-full-queue rule, where a drop is recoverable because the next event re-announces state; here the stream is edge-triggered, so a dropped `pr.merged` is gone forever and leaves only a counter.
- Evidence: the copied rule is `dashboard/braintown.go:303-322`; BR-02, `pustaka/docs/product/brd.html:1226`: "An event that was never written is a state change that never happened."
- Fix: Specify the recovery: rewind the cursor to the last delivered event and re-read (the server replays from `Last-Event-ID`), or block with a deadline, or persist. Name which.

### The double filter's key is undefined - Severity: Major
- Affected scope: §4.1 vs §5
- What is wrong: §4.1 filters on `productKey`, but `PREvent` carries no product key, only `Org`/`Repo`; the design never says which GAOS structure holds the owned repositories nor how `org/repo` is matched against it.
- Evidence: GAOS's product record has `Repo string` — `dashboard/model.go:41-46`.
- Fix: Name the field, the match rule (owner-qualified or not), and the behaviour for an unregistered repository (skip silently, or count it).

### `BRAIN_TOWN_CONSUMER_KINDS` is a dead setting - Severity: Major
- Affected scope: §3 vs §4.1
- What is wrong: The setting is a comma-separated list with default `github`, but the request hard-codes `?kind=github` and no client-side kind filter is specified, so the operator's value changes nothing.
- Fix: Send `?kind=` from the configured list (and state how the list is encoded), or drop the setting and say so.

### The change list is not complete - Severity: Major
- Affected scope: §6
- What is wrong: At least six files the implementation or the release needs are absent, contrary to the design contract's completeness rule.
- Evidence: `dashboard/view.go:193-199` and `dashboard/view.go:372-380` (the `IntegrationsPage` struct and `BrainTownRow` need new fields); `dashboard/templates/backoffice.html:730-739` (the row markup); `pustaka/docs/assets/toc/integrations.js:54` (page registration); `pustaka/docs/product/qa-test-plan.html` (one row per `func Test…`, `.claude/rules/qa-test-plan.md:129-135`); `pustaka/docs/assets/toc/product.js` plus the count sync in `brd.html`, `decision-log.html`, `open-questions.html` (`.claude/rules/qa-test-plan.md:86-90`); `pustaka/docs/changelog.html` + `pustaka/docs/assets/toc.js` (release hygiene, `.claude/rules/qa-test-plan.md:97-100`).
- Fix: List every file, with the arithmetic written down.

### The consumer token has no secret mount - Severity: Major
- Affected scope: §6
- What is wrong: The change adds `BRAIN_TOWN_CONSUMER_TOKEN_FILE` to the prod compose file but not the bind mount that gives the file content, so the reader would find nothing and stay silently off.
- Evidence: the existing pattern is `BRAIN_TOWN_TOKEN_FILE: "/run/secrets/bt-token"` at `deploy/stacks/compose.prod.yaml:149` with the mount at `deploy/stacks/compose.prod.yaml:158`.
- Fix: Name the host file, the mount line, and the mode (600), and say the reader's state is `unusable` when the mount is missing.

### A 401 stops the reader forever - Severity: Major
- Affected scope: §8
- What is wrong: "The reader stops" has no recovery path, and no distinction is drawn between a revoked token and a transient 401 from a proxy; the token is read at boot, so rotation needs a GAOS restart.
- Evidence: tokens are read once at boot — `deploy/stacks/compose.prod.yaml:117`; the outbound client does classify status codes — `dashboard/braintown.go:540-542` and `574-593`.
- Fix: Specify the status-class policy (401/403 stop; 429/5xx retry), a bounded retry for the 401 case, and how a rotated token is picked up (re-read the file on the next attempt, or document the restart requirement).

### A half-open connection is never detected - Severity: Major
- Affected scope: §4.1
- What is wrong: The heartbeat is asserted but no rule says what happens when it stops while the TCP connection stays open — the classic failure of a long-lived SSE flow behind a NAT. The reader would hang forever, never reconnect, and no error would surface.
- Fix: State a read deadline (for example 2× the heartbeat), the reconnect on deadline, and that the heartbeat itself is ignored as data.

### No frame-size or field-length limit - Severity: Major
- Affected scope: §4.1; §5
- What is wrong: The SSE frame is read whole into memory and its JSON decoded, with no maximum size and no cap on `Title`/`Org`/`Repo`, so a large or hostile frame is a memory problem inside the dashboard process.
- Evidence: the existing outbound client caps the body at 4 KB (`dashboard/braintown.go:51`, `btBodyLimit`) and the title at 200 (`dashboard/braintown.go:49`, `btMaxTitle`).
- Fix: Give the reader a max frame size and max field lengths, mirroring `btBodyLimit`/`btMaxTitle`, and say what happens to an oversized frame (drop + count).

### The token-file mode rule is overstated - Severity: Major
- Affected scope: §3
- What is wrong: The design presents "the file must not be readable by others" as part of the rules it copies from `readBrainTownToken`; that function only logs a warning and does not refuse, so an implementer copying it gets a warning, not the stated rule.
- Evidence: `dashboard/braintown.go:133` calls `warnIfOpen`, which only logs — `dashboard/auth.go:249-253`.
- Fix: Say plainly that GAOS warns and does not refuse, or state that the consumer reader refuses a group/other-readable file (and name the check).

### Brain Town's contract is unanchored and unscheduled - Severity: Major
- Affected scope: §1; §4.1; §5; §7.4
- What is wrong: `/consumer/events`, `?kind=`, `retry: 3000`, the 15 s heartbeat, the admin stream's cookie, "the GitHub payload is never stored in Brain Town" and `backend/internal/github/` are all load-bearing and none carries a file and line or an explicit assumption label; there is also no statement of which side is built first.
- Evidence: `docs/designs/README.md:22-23`; `backend/internal/github/` does not exist in this repository.
- Fix: Mark each external claim as an assumption with the Brain Town contract it depends on, quote the contract's version or date, and add a sequencing note: GAOS ships after Brain Town's `/consumer/events` exists.

### Two instances, one scope - Severity: Major
- Affected scope: §2; §4.1
- What is wrong: GAOS runs a second instance with its own database, so if both point at one Brain Town scope each gets every event, and neither the flow's dedupe nor the reader's cursor is shared — duplicate PR tasks, silently.
- Evidence: `deploy/stacks/compose.prod.yaml:77` — "The second instance (rnd-v3-vpn) sets GAOS_ARCADE_DB to its own database, so the two never share a graph."
- Fix: Require one scope per GAOS instance, or specify that the consumer stream is a shared-queue per scope.

### The counts contradict themselves - Severity: Nit
- Affected scope: §6
- What is wrong: "3 new Go files, 4 changed Go files" contradicts the next sentence's "2 new … 3 changed", and the two deploy files are counted nowhere; the total of 11 is right by accident.
- Evidence: `docs/designs/README.md:26-27` requires the arithmetic.
- Fix: Rewrite as 2 new + 3 changed Go files, 2 deploy files, 1 design, 1+ review reports, 1 new docs page, 1 changed docs page — and recompute the total.

### Three citations point at the wrong lines - Severity: Nit
- Affected scope: §4.3
- What is wrong: "It is read-only and it renders no secret" is at line 18, not 21-23; `urlHost` spans 39-60, not 41-57; `buildIntegrations` is declared at line 70, not 69.
- Evidence: `dashboard/integrations.go:18`, `dashboard/integrations.go:39-60`, `dashboard/integrations.go:70`.
- Fix: Correct the three ranges.

### The reconnect cycle and the server's `retry` are unreconciled - Severity: Nit
- Affected scope: §4.1; §8
- What is wrong: The waits are "1 s, 5 s, 30 s" with no statement of what follows, and the server's `retry: 3000` is never reconciled with the client's own first wait.
- Evidence: the copied constant is documented as "the wait before the 2nd, 3rd and 4th attempt" — `dashboard/braintown.go:56-57`.
- Fix: Say the cycle repeats at 30 s after the third failure, and whether the server's `retry` overrides it.

### The counters need a concurrency rule - Severity: Nit
- Affected scope: §4.3
- What is wrong: The frames, dropped and reconnect counters are written by the reader goroutine and read by the HTTP handler, with no statement of how that is made safe.
- Evidence: the existing pattern takes a mutex — `dashboard/braintown.go:599-606`.
- Fix: Say the counters are read under the same lock, as `Status()` does.

---

Verdict: fix first — 1 Blocker, 12 Majors, 4 Nits.

## Triage

| Finding | Severity | Disposition | Note |
|---|---|---|---|
| The flow design does not exist | Blocker | PROPOSE FIX | `docs/designs/gaos-pr-agent-flows.md` is now in the tree, and §5 of this design states the hand-over contract inline: the channel is `chan PREvent` with capacity 256, the reader owns no de-duplication, and the flow's key is `<repo>#<pr>:<role>`. |
| Restart replay is contradicted by the storage rule | Major | PROPOSE FIX | The cursor is a file, `BRAIN_TOWN_CONSUMER_CURSOR_FILE`, mode 600. §4.2 and §8 now agree. |
| A dropped event is permanent loss | Major | PROPOSE FIX | The cursor advances **only** after the flow has accepted the event, so a full channel leaves the cursor where it was and the next reconnect replays the event. |
| The double filter's key is undefined | Major | PROPOSE FIX | The rule is one sentence: the frame's `orgLogin` + `productKey` must equal one of the product's `Repos` entries, which are `owner/repo` pairs. A frame that matches no product is skipped and counted. |
| `BRAIN_TOWN_CONSUMER_KINDS` is a dead setting | Major | PROPOSE FIX | `?kind=` is the configured list, comma-joined. |
| The change list is not complete | Major | PROPOSE FIX | Every file the review named is in §6, and the arithmetic is written in one place. |
| The consumer token has no secret mount | Major | PROPOSE FIX | The mount, its mode and the "unusable when the mount is missing" state are in §6. |
| A 401 stops the reader forever | Major | PROPOSE FIX | The status-class policy is stated: `401`/`403` stop the reader and the token file is re-read on the next attempt, so a rotated token is picked up without a restart. `429`/`5xx` retry. |
| A half-open connection is never detected | Major | PROPOSE FIX | A 45 s heartbeat watchdog (3 × the 15 s heartbeat) reconnects; the heartbeat is ignored as data. |
| No frame-size or field-length limit | Major | PROPOSE FIX | 4 KiB per frame and 200 characters per field, mirroring `btBodyLimit`/`btMaxTitle`. |
| The token-file mode rule is overstated | Major | PROPOSE FIX | The reader **refuses** a loose mode, citing `dashboard/api_v1.go:133`. |
| Brain Town's contract is unanchored and unscheduled | Major | PROPOSE FIX | Every Brain Town claim cites `docs/integrations/gaos-consumer.md` in the Brain Town repository; the sequencing note says GAOS ships after Brain Town's path exists. |
| Two instances, one scope | Major | PROPOSE FIX | One Brain Town scope per GAOS instance is required; the scope names the instance. |
| The counts contradict themselves | Nit | PROPOSE FIX | Recounted. |
| Three citations point at the wrong lines | Nit | PROPOSE FIX | Corrected. |
| The reconnect cycle and the server's `retry` are unreconciled | Nit | PROPOSE FIX | The cycle repeats at 30 s; the server's `retry` is a hint the reader does not follow, because the reader's own backoff is the authority. |
| The counters need a concurrency rule | Nit | PROPOSE FIX | The counters are read under the same mutex, as `Status()` does. |
