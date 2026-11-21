# Security review: design-iterate (v2)

The design is sound at its boundaries. Both new routes are behind the session gate, the operator's email stays out of every prompt, and the triage text is checked before it is stored. One High remains: an ordinary approve on the revised design card can send the re-review back to the old design. The rest are one acknowledged weak control, one acknowledged exposure, and hardening.

## Paraphrase

### What this proposes
When a design review blocks a design, a signed-in back-office operator can send the design back to its author. The design task goes from `DONE` back to `READY`, which becomes the only allowed exit from `DONE`, for design tasks only. The review goes `BLOCKED → PENDING`. Both moves use the new rule `send-back`, and both events record the operator's email. The design's event also carries an optional triage of each finding: one of four dispositions plus a note of up to 300 characters. The revision's packet holds the previous design, the review artifact and the rendered triage, plus an instruction not to quote any of them. When the revision is delivered, the review becomes runnable again through its dependency edge and runs on a fresh session. That decision is made from the event stream. If the revised design copies a finding or a triage note of 24 characters or more word for word, it is refused. Unblocking a design-blocked review is refused, and so is releasing it on drift. A design can be sent back at most three times. A review card gets a "Revise design" button and a sheet, with one GET route and one POST route. Nothing changes in the schema or the event types.

### How the existing system works, per this document
The design gate gives each story three tasks: design, then review, then implementation, linked by dependency edges. `gateVerdict` records a design artifact and moves the design to `DONE`. It records a review artifact and either clears the review or blocks it with `design-blocked`. `DONE` has no exits. Unblocking a review re-runs it against the same design, which amounts to a reroll. A drift release unblocks every `BLOCKED` task. A review's first attempt opens a fresh session; other attempts resume. Back-office routes need a session and a same-origin request. Since D-079 / BR-55 the session is a verified, signed cookie (`dashboard/auth.go:360-382`), so the operator identity this design relies on does exist. The rubric's claim that `signedIn()` accepts any cookie is out of date. Any signed-in account can move any card (one trust domain).

## Trust boundaries touched

| Boundary | Authenticates | Authorises | Validates | Far side fails |
|---|---|---|---|---|
| operator browser → dashboard (GET sheet, POST send-back) | verified BO cookie, `gate` (`auth.go:523-555`) | none per account (N6) | same-origin check on the POST (`auth.go:560-577`), 256 KiB body cap, triage rules, egress screen | refusal before anything is written (append-only) |
| dashboard → gateway (designer packet) | gateway key | gateway key | egress pass on the packet; triage screened before it is stored | revision FAILED or BLOCKED; review waits |
| designer → re-reviewer (through the design document) | none | none | exact-substring check only | paraphrase passes (stated) |
| author workspace → reviewer workspace | none | none | write boundary only (BR-47) | inherited |
| dashboard → ArcadeDB | DB user | grants | none | one transaction through `mutate` |

## Findings

### SEC-1 Approving a revision that produced no design re-runs the review on the old design - Severity: High
- Component: design gate / board approve / re-review packet
- Evidence:
  - Design claims: `docs/designs/design-iterate.md:53` (G6, "the reroll is closed on every route"), `:260-276` (§3.6), `:339` (BR-57: "never re-run on the same design").
  - If a design turn hands off without a parseable design block, `gateVerdict` steps aside (`dashboard/designgate.go:685-690`). The runner then puts the task in `REVIEW` (`dashboard/runner.go:275-284`, `adapter_aimanager.go:305-308`: a valid handoff sets no failure).
  - `legalMove` allows `REVIEW → DONE` as `approve` for any kind (`dashboard/board.go:177-178`). `MoveCard` checks only implementation tasks (`board.go:288-301`, `designgate.go:860-863`).
  - The review packet and `gateVerdict`'s review branch take the newest design artifact without checking which revision it came from (`store_mem.go:1491-1500`, `designgate.go:722`, `artifactOfKindFor` at `designgate.go:573-586`).
- Attack path:
  1. After a send-back, the designer replies with a handoff but no valid design block. A steered or lazy model can do this.
  2. The design lands in `REVIEW`. An operator approves it as they would any review card. The move is actor-less, and nothing on the card shows that no artifact was recorded.
  3. The design is now `DONE`, so the scheduler promotes the review. Under `GAOS_RUNNER=on` it also dispatches it.
  4. A fresh reviewer judges the previous artifact, which is the very design that was blocked.

  That is the reroll D-083 says it closes, and a clearing roll releases the implementation. In the first round this path ends in `design-missing`, because no artifact exists yet. Only a revision has an older artifact to fall back on. The verbatim check (§3.5) is also skipped, because it runs only when a design block parses.
- Remediation:
  - In `gateVerdict`'s review branch, require a design artifact written after the newest `send-back` `TaskMoved` of the design. Compare the artifact's run with the send-back's `Seq`/`At`, or require the number of design artifacts to exceed `sent`. Otherwise block with `design-missing`. Make the review packet build fail closed the same way.
  - Also refuse `approve` in `MoveCard` for design and review tasks. D-069 clause 6 makes the gate the only writer of their `DONE`.
  - Add both cases to TC-499.
- Effort: small
- Why High and not Critical: it needs a privileged operator action and a reviewer roll that happens to clear, and no authentication is bypassed. It still opens a gate-bypass path on a privileged route, and the design says that path is closed.

### SEC-2 The verbatim "quotes the review" check is narrower than the threat - Severity: Medium
- Component: `gateVerdict` design branch (§3.5 item 2)
- Evidence:
  - `docs/designs/design-iterate.md:255` compares only the last send-back's triage and the newest review artifact, by exact substring, for notes of 24 characters or more.
  - `:237` says the designer resumes its session, which holds the findings and triage of every earlier round.
  - `:256` states that a paraphrase passes.
- Attack path:
  - In round 2 or 3, the designer copies findings or triage notes from round 1 word for word. They are in its resumed session but not in `last` or the newest artifact. Changing whitespace or case is enough to beat the check too.
  - That text then reaches the fresh reviewer, which BR-47/BR-49 is meant to prevent. This lets the author lobby the reviewer with the text the earlier reviews produced.
- Remediation:
  - Check against every triage of every `send-back` event of the design and every review artifact of the review task.
  - Normalise both sides before comparing: casefold, collapse whitespace, strip punctuation.
  - Also check finding scopes.
  - Add a round-2 case to TC-499 that quotes a round-1 note.
  - Keep the stated limit.
- Effort: small

### SEC-3 The read boundary between workspaces now guards operator triage, and nothing new is added for it - Severity: Medium
- Component: author/reviewer workspace isolation
- Evidence:
  - `docs/designs/design-iterate.md:258` (SEC-3 "inherited limit") and `:428` (R4).
  - `pustaka/docs/product/brd.html:1326`: a write boundary, not a read boundary.
  - `brd.html:1328`: BR-49 withholds "the operator's answer", and a triage note is the same class of text.
- Attack path: A re-reviewer with file tools, possibly steered by text in the revised design, reads files the designer wrote in the sibling author folder. If the designer saved findings or triage to disk during its revision, the re-reviewer reads the author's side of the desk. Before this change that folder held no operator triage.
- Remediation:
  - State, and test, that the packet parts `findings.md` and `design.md` are prompt content only and are never written into the author's folder.
  - Assert that the BR-49 reviewer workspace reset runs before a re-review turn, not only before a first attempt.
  - Record the remaining read risk against an open question rather than only in prose.
- Effort: small

### SEC-4 The send-back sheet handler drops the `requireBO` check its sibling sheets carry - Severity: Low
- Component: `handleSendBackSheet`
- Evidence:
  - `docs/designs/design-iterate.md:292` specifies the handler without `requireBO`.
  - The answer and timing sheets both call `s.requireBO` first (`dashboard/handlers_bo.go:758, 785`).
- Attack path: Today the gate protects the route. If the route is ever misclassified (for example added to `publicPatterns`), the sheet would list review findings to a signed-out caller with no second check.
- Remediation: Begin the handler with `if !s.requireBO(w, r) { return }`, as the sibling sheets do.
- Effort: quick-win

### SEC-5 Triage notes skip BR-53's stripping on the strength of "the operator is trusted" - Severity: Low
- Component: `normalizeTriage` → designer prompt
- Evidence: `docs/designs/design-iterate.md:155` (control characters, line breaks and fences refused; backticks and angle brackets allowed), `:426` (R2 rationale), `:65` (N6: any signed-in account), `brd.html:1346` (BR-53).
- Attack path: Any signed-in account can put pseudo-markup or instruction-shaped text into a model prompt, framed as "the operator's triage". The risk is low inside one trust domain, but the trust rests on the account, not on a person.
- Remediation:
  - Either strip backticks and angle brackets as BR-53 does, or keep the exemption and record it in BR-57's text.
  - Wrap the rendered notes in a clearly delimited, quoted block.
- Effort: quick-win

### SEC-6 The test for the POST's same-origin defence covers only one header case - Severity: Low
- Component: `POST .../send-back` CSRF
- Evidence:
  - `dashboard/auth.go:557-577`: a POST with neither `Sec-Fetch-Site` nor `Origin` passes.
  - `auth.go:493`: the cookie is `SameSite=Lax`.
  - `docs/designs/design-iterate.md:378`: TC-497 asserts only `Sec-Fetch-Site: cross-site`.
- Attack path: An old or unusual client that sends neither header leaves SameSite=Lax as the only CSRF defence for a state-changing, append-only write. This is inherited, but this design adds the route.
- Remediation: Extend TC-497 with a foreign `Origin` → 403 and a `same-site` → 403, so both branches are tested for this route.
- Effort: quick-win

### SEC-7 The operator's email is written permanently into event captions - Severity: Low
- Component: event `Text` / audit
- Evidence: `docs/designs/design-iterate.md:162` (caption gains ` by <actor>`), `:427` (R3, permanent per BR-04), `dashboard/store_arcade.go:1062` (`Text` is persisted in the payload).
- Attack path: Not an attack. Personal data enters an append-only store and cannot be removed, and it appears in every timeline render and every export of the event log, not only in the payload that holds the audit fact.
- Remediation:
  - Keep `Actor` in the payload, which is the audit record.
  - Leave the caption free of the email, or show a stable account label.
  - State the retention decision in D-083.
- Effort: quick-win

### SEC-8 Full scans of the event stream on board reads - Severity: Low
- Component: `sendBacksLocked`, `reviewSentBackSinceDispatchLocked`, `decorateBoardGate`
- Evidence:
  - `docs/designs/design-iterate.md:139, 248, 286`: one scan of `s.events` per call.
  - `dashboard/store_arcade.go:1736-1740`: the stream is unbounded.
  - Live hints make clients refetch the board (`dashboard/broker.go:15-19`).
- Attack path: A signed-in client, or many open tabs, hits the board repeatedly. Each render scans the whole stream under the read lock once per design-blocked review card, which slows writers that need `s.mu`. The impact is bounded by authentication.
- Remediation: Keep a per-design send-back count and last-send-back `Seq`, updated in `emit` and rebuilt on load. Read them in O(1).
- Effort: small

### Not raised as findings
- The reviewer deny-list in `.claude/rules/agent-workspace.md` is enforced by instruction, not by tooling. This design does not change it.
- Path traversal and IDOR are covered: the product is checked at check step 1 (`docs/designs/design-iterate.md:134`) and a card of another product is a 404 (`:292`).
- The live stream sends hints, not payloads (`dashboard/broker.go:15-19`), and the task-mutation JSON responses strip payloads (`dashboard/handlers_tasks.go:80-86`), so the triage and the actor do not leak there.

Counts: Critical=0 High=1 Medium=2 Low=5
