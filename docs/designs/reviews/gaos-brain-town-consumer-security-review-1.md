# gaos-brain-town-consumer — security review 1

> **Reviewer:** the zero-context security goldfish
> **Document reviewed:** `docs/designs/gaos-brain-town-consumer.md`
> **Rubric:** `.claude/rules/rule-index.md`, `.claude/rules/qa-test-plan.md`,
> `.claude/rules/agent-workspace.md`, plus the domain sources the rule index names.

## Findings

### Wrong-kind tokens are accepted - Severity: High
- Affected scope: §3 "The configuration" (`BRAIN_TOWN_CONSUMER_TOKEN_FILE`); §9 "Security"
- What is wrong: The token rule is "copy `readBrainTownToken`" plus a `bt_live_` refusal, but that function checks only length and printability — any 16–256 printable string is accepted and sent as `Authorization: Bearer` to a third-party host. GAOS's own credentials and its GitHub token are never refused.
- Evidence: `dashboard/braintown.go:132-148` (length/printability only, no prefix); `dashboard/braintown.go:121-124` with `dashboard/api_v1.go:47` (the outbound feed refuses `gaos_` and any GAOS API token); `dashboard/braintown_test.go:150-151`
- Fix: Require the `bt_c_` prefix positively and refuse `bt_live_`, `gaos_` and `github_pat_` shapes; register a negative case per shape.

### The mode-600 rule is stated but not enforced by the code it copies - Severity: Medium
- Affected scope: §3 ("the file must not be readable by others"); §9 ("The token is a **file**, mode 600")
- What is wrong: `readBrainTownToken` only *warns* about a loose mode; it never refuses, so a reader that copies it runs with a group- or world-readable consumer token. The warning also logs the token file's path, which §4.3 forbids in the view and §9 does not mention for logs.
- Evidence: `dashboard/braintown.go:133` (`warnIfOpen`, warn-only, and it prints the path); `dashboard/auth.go:249-253`; `pustaka/docs/integrations/brain-town.html:37` (the published rule claims mode 600)
- Fix: Say whether a loose mode refuses the reader (off, reason shown) or only warns, and say the file path is never logged.

### SSRF defence is three exact hostnames, not an allow-list - Severity: Medium
- Affected scope: §9 ("refuses to connect to the database's, the ai-manager's or the QA tools' own hosts")
- What is wrong: The denylist is built from three *hostnames* and matched with an exact case-insensitive compare; there is no refusal of IP literals, loopback, private or link-local addresses, so `https://127.0.0.1:8080/`, `https://10.10.0.2:18301/` (GAOS's own Traefik) or a metadata address all pass and receive the bearer.
- Evidence: `dashboard/braintown.go:85-88`; `dashboard/qaclient.go:81-86` (exact hostname match) and `dashboard/qaclient.go:87-101` (the private-range rule applies only when `httpsOnly` is false); `deploy/stacks/compose.prod.yaml:76`
- Fix: Require an allow-list of the one permitted Brain Town host, refuse IP literals and loopback/private/link-local ranges, and keep the redirect refusal explicit.

### The untrusted title is unbounded and unscreened - Severity: Medium
- Affected scope: §5 "The event it hands over" (`Title string`); §1 ("the real title")
- What is wrong: The PR title is attacker-authored text and the design caps nothing: no length limit, no control-character strip, no secret-detector pass. It lands in a task title, and the outbound feed republishes a public product's title after only the detector check, so a crafted title reaches the public town.
- Evidence: `dashboard/braintown.go:49` and `228-253` (the outbound side caps at 200 chars, strips control characters, runs `detectors`); `dashboard/egress.go:347-369`; `dashboard/schema/04-execution.sql:26` (`Task.title` has no length limit)
- Fix: Cap and control-strip the inbound title, screen it with the egress detectors, and mark it untrusted in the task it creates.

### No bound on one frame - Severity: Medium
- Affected scope: §4.1 (the SSE parser)
- What is wrong: The queue is bounded (capacity 256) but a single frame is not; a hostile or compromised Brain Town can deliver one arbitrarily large `data:` line into the reader's own buffer.
- Evidence: `dashboard/braintown.go:51` and `570` (the outbound feed bounds the wire at 4 KiB); `dashboard/braintown.go:308-323` (the queue bound the design mirrors)
- Fix: State a maximum frame size, mirroring `btBodyLimit`, and say an over-long frame is skipped and counted.

### Timeout and heartbeat behaviour unspecified - Severity: Medium
- Affected scope: §4.1 ("retry: 3000 and a heartbeat every 15 s")
- What is wrong: The design never says what the reader does with the heartbeat. The outbound feed's client carries a whole-request `Timeout` of 3 s — right for a POST, fatal for a stream (reconnect every 3 s) — and if no timeout is used, a half-open connection stalls the reader forever with no reconnect.
- Evidence: `dashboard/braintown.go:47` and `292-294`; `dashboard/qaclient.go:106-111`
- Fix: Say there is no client-level timeout on the stream, state the connect timeout, and state the heartbeat watchdog that triggers a reconnect.

### The reconnect floor is unbounded - Severity: Medium
- Affected scope: §4.1 and §8 ("reconnects with the waits 1 s, 5 s, 30 s")
- What is wrong: A server that accepts and immediately closes yields one request per second, forever, plus a log line each time; the outbound feed bounds itself by degrading to a single attempt after repeated failures, and the design states no ceiling.
- Evidence: `dashboard/braintown.go:513-516` and `519` (`btDegradeAt`, 4 attempts then degrade); `dashboard/braintown.go:57` (the wait list the design reuses)
- Fix: State that after N consecutive failures the reader holds at the 30 s wait, and that the 401 case stops it (as §8 already says).

### The double filter is keyed on a bare productKey - Severity: Medium
- Affected scope: §4.1 ("a frame whose `productKey` is not one of GAOS's own product repositories")
- What is wrong: GAOS's repositories are `owner/repo` pairs, while the filter key is a bare key and the struct hands over an unverified `Org`. A same-named repository under another owner passes a name-only filter, and the `Org` the flow may use comes from the remote.
- Evidence: `dashboard/model.go:388` (`Repos []string`); `dashboard/fixtures.go:167` (`"loop/laundry-api"`); `deploy/seed/example/product.json:8` (`"example-org/sample-shop-tests"`)
- Fix: Resolve the frame to a GAOS product by the product's declared owner/repo pair and state that the frame's `Org`/`Repo` are re-derived or not trusted.

### Restart replay contradicts the stated state - Severity: Medium
- Affected scope: §4.2 ("keeps the last sequence number it handled and nothing else") vs §4.1 and §8 ("a restart replays what it missed")
- What is wrong: Nothing in the change list persists the sequence number or the seen event ids, so after a restart the reader has no `Last-Event-ID` and re-reads from the server's default; §8 attributes de-duplication to the flow while §5 puts the `EventID` key on the reader, which stores nothing.
- Evidence: a document statement, not a repository claim.
- Fix: Say where the sequence number and the de-duplication set live across a restart, or drop the replay claim and bound the re-read.

### Change list is incomplete and two citations are wrong - Severity: Low
- Affected scope: §6 "The change list" (§4.3, §9 citations)
- What is wrong: The change list omits the QA register and the count text it moves, and it says "3 new Go files, 4 changed Go files" then "2 new … 3 changed" for the same files. The redirect rule is attributed to `validOutURL`, and the "read-only and renders no secret" sentence is cited three lines off.
- Evidence: `.claude/rules/qa-test-plan.md:19-24` and `72-90` (register in the same change, four counts); `docs/designs/README.md:26-27` (the arithmetic must follow the list); `dashboard/qaclient.go:71-102` and `dashboard/braintown.go:292-294` (redirect refusal lives in the client); `dashboard/integrations.go:18`; `deploy/check-stacks.sh:142-156` and `deploy/stacks/compose.prod.yaml:149-158` (the token volume mount the new setting needs)
- Fix: Add the register, the count text, the stack assertions and the token volume mount to the change list, correct the counts, and cite the redirect rule and the quoted sentence accurately.

## Inferences

- The inbound direction is sound in principle: the request carries no GAOS product id or
  credential other than the bearer, redirects are refused, and the flow's own rule
  (repository from the product, never from the event) bounds what a forged frame can cause.
- The gaps are mostly *stated-rule vs implemented-rule* gaps: the design asserts controls
  (mode 600, token kind, host safety) that the mirrored code implements more weakly or not
  at all.

## Unknowns / risks

- Brain Town's side of the contract lives in a separate repository and was not read here;
  the design's assumptions about the envelope shape, the scope semantics and the `bt_c_`
  prefix are therefore unverifiable from this repository and should be stated as
  assumptions in the document.
- The consumer stream's real retention and replay window are unknown, so the cost of the
  replay gap cannot be bounded from here.

---

Verdict: fix first — Counts: Critical 0, High 1, Medium 8, Low 1 (a High finding is a
Blocker under `docs/rules/gaos-design-review.md:99-100`).

## Triage

| Finding | Severity | Disposition | Note |
|---|---|---|---|
| Wrong-kind tokens are accepted | High | PROPOSE FIX | The token must start with `bt_c_`; `bt_live_`, `gaos_`, `github_pat_` and any token equal to one of GAOS's own are refused. A negative test per shape is in the change list. |
| The mode-600 rule is stated but not enforced | Medium | PROPOSE FIX | The reader **refuses** a token file others can read (`dashboard/api_v1.go:133`, BR-52). The file path is never logged; the reason says "the token file is not private". |
| SSRF defence is three hostnames, not an allow-list | Medium | PROPOSE FIX | The reader allows **one** host, named by the setting, and refuses IP literals and loopback, private and link-local ranges. Redirects stay refused. |
| The untrusted title is unbounded and unscreened | Medium | PROPOSE FIX | The title is capped at 200 characters, control-stripped and screened with the egress detectors before it reaches a task title. |
| No bound on one frame | Medium | PROPOSE FIX | A frame is at most 4 KiB, mirroring `btBodyLimit` (`dashboard/braintown.go:51`). An over-long frame is skipped and counted. |
| Timeout and heartbeat behaviour unspecified | Medium | PROPOSE FIX | No whole-request timeout on the stream; a 10 s connect timeout; a heartbeat watchdog of 45 s (3 × the 15 s heartbeat) that reconnects. |
| The reconnect floor is unbounded | Medium | PROPOSE FIX | After four consecutive failures the reader holds at the 30 s wait, mirroring `btDegradeAt`. A `401` stops it. |
| The double filter is keyed on a bare productKey | Medium | PROPOSE FIX | The frame's `orgLogin` + `productKey` are matched against the product's `Repos` entries, which are `owner/repo` pairs; a bare name never matches. |
| Restart replay contradicts the stated state | Medium | PROPOSE FIX | The cursor is a file; §4.2 and §8 are reconciled. |
| Change list is incomplete and two citations are wrong | Low | PROPOSE FIX | All the named files are in §6; the redirect refusal is cited to the client; the quoted sentence is cited to `dashboard/integrations.go:18`. |
