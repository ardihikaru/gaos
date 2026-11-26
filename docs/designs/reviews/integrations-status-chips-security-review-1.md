<!-- Round 1, lens: security. Reviewer read only the design and the rubric (zero context). Canaries planted in the seven standing files of the worktree and removed after the round; no canary value in this report. -->

# Security review: `docs/designs/integrations-status-chips.md`

## Paraphrase

### What this proposes
- The back office's Integrations view (`/app/integrations`) gets a coloured status chip on each of its six integration headings: ai-manager, Brain Town, Brain Town reader, Pull-request flow, QA tools and Machine API. Each heading also gets a Mandatory or Optional chip. ai-manager, QA tools and Machine API are Mandatory, and that is fixed in code.
- A new pure Go function, `integHeads`, builds every chip from state fields the page already has. It uses one closed table: exact match or prefix match turns each state into a fixed Label and a Tone of `on`, `off` or `warn`. The Pull-request block also gets three chips per row (GitHub, Staging, WhatsApp), and its heading chip is derived from those three.
- The `· <state>` text after each heading is removed. The full state text still prints in the rows below.
- Each heading gets an info button that opens a native HTML popover with fixed English text: what the integration does, and what happens when it is connected or disconnected. It adds no JavaScript and only CSS. Four new tests (TC-753 to TC-756) are added, plus register, changelog and version updates.

### How the existing system works, per this document
- `buildIntegrations` (`dashboard/integrations.go`) fills an `IntegrationsPage` with states from each subsystem. Some states are fixed words and some carry free-text reasons: Brain Town `unusable: <why>`, GitHub `off (<why>)`, Machine API `enabled (admin=… )` or `disabled (<why>)`, and WhatsApp's composite label.
- The view is a back-office route behind the session gate, and the page already shows hosts and topology but never secrets (D-085).
- The Brain Town reader's cursor file is mounted read-only, so the cursor cannot be saved. The design says this is escalated and not fixed here.

Note for the reader: this review's brief says `signedIn()` accepts a cookie from any request. That is out of date in this worktree. `dashboard/auth.go:328-396` checks the MAC, the expiry, that the account exists and the fingerprint. `gate` (`dashboard/auth.go:541-590`) denies by default, and `/ui/bo/{view}` is not in `publicPatterns` (`auth.go:516-524`). The design's §7 claim that the view sits behind the session holds. No trust boundary is widened: there is no new route, origin, outbound call, file write or interpolated field.

## Trust boundaries touched

| Boundary | Authenticates | Authorises | Validates | Change |
|---|---|---|---|---|
| operator browser to dashboard | signed session cookie (`auth.go:375-396`) | the `gate` default-deny (`auth.go:568-587`) | `html/template` escaping; CSP `backOfficeCSP` (`auth.go:539`) | new static markup, fixed labels, a `Tone` inside a class attribute (escaped by `html/template`, `render.go:9`) |
| everything else | — | — | — | not touched |

Abuse cases checked and found not to apply:
- **Injection through a free-text field:** labels and tones are fixed words. The reason strings were already rendered before this change.
- **Secret leak:** no new field reaches the page.
- **IDOR, replay, oversized payload, path traversal:** no new input.
- **Committed credential:** none.
- **Tunnel denial of service:** no new route.

The findings below are about the security signals the operator reads, which this design changes, and about one escalated item it describes wrongly.

---

### SEC-1 The green "Enabled" chip covers WhatsApp's deliberate "unencrypted" warning - Severity: Medium
- Component: the status mapping (§2.2, `PR.WAChip`) and the `PRHead` roll-up
- Evidence: `docs/designs/integrations-status-chips.md:89-92` maps any WhatsApp state starting with `"enabled"` to `Enabled · on` (green). `dashboard/wa.go:250-258` builds that state from parts so that "unencrypted" is never hidden by another state. Its comment says "an operator must always be able to see the link is clear". It produces `enabled, over http to any address (unencrypted)` when `publicHTTP` is set.
- Attack path: an operator sets `GAOS_WA_ALLOW_HTTP=any` (`deploy/stacks/compose.prod.yaml:168-170`). WhatsApp notices and the gateway's Basic credential (`compose.prod.yaml:165`) then travel in cleartext to a public address. Today the heading text puts that warning in front of the operator. After this change the most visible signal is a green "Enabled" chip, and a green-feeding "Partly connected" roll-up on the PR heading. The warning moves to a row below the chip. A deliberate security warning drops from primary to secondary, and a network-path observer can read the credential and the notices.
  - Why Medium: the text is still on the page, so the warning is weakened, not removed. If the operator sees it as a regression of a deliberate control (`wa.go:250-251`), treat it as High.
- Remediation: in §2.2, map a WhatsApp state that contains `unencrypted` to `warn`, labelled for example "Unencrypted", whatever its prefix. Make `PRHead` treat a `warn` sub-chip as never green. Add a negative case to TC-753: an `enabled, over http … (unencrypted)` state is never `on`. Also apply the rule to any future state that carries a security qualifier, by matching on the qualifier and not only the prefix.
- Effort: quick-win

### SEC-2 The Machine API popover says every machine call is refused when it is off, but the staging callback stays open - Severity: Medium
- Component: the popover text `ip-api` (§2.3)
- Evidence:
  - `docs/designs/integrations-status-chips.md:136` says that when it is disconnected, "Every machine call is refused; only the back office works."
  - `dashboard/api_v1.go:308-311` refuses `/v1` bearer routes when `s.api` is nil or disabled. But `POST /v1/products/{id}/staging` (`dashboard/api_v1.go:415-417`) goes to `apiStaging` (`dashboard/staging.go:196-222`). That handler checks only `s.staging.enabled()` and its own Basic credential, never `s.api`.
  - The design's own `ip-btc` text (`integrations-status-chips.md:133`) says "The staging callback still works."
- Attack path: an operator who wants to close GAOS's inbound machine surface turns off the Machine API. The UI tells them no machine call is accepted. A Basic-authenticated, internet-reachable write route (`/api/v1/products/{id}/staging`) is still live, so they believe the surface is closed when it is not. The two popovers also contradict each other.
- Remediation: change the `ip-api` disconnected text to name the exception, for example: "Every bearer-token call is refused. The staging callback has its own credential and is controlled by the Pull-request flow's staging setting." Add an assertion to TC-756 that the `ip-api` text names the staging exception, so the claim cannot drift from `staging.go`.
- Effort: quick-win

### SEC-3 The escalated Brain Town reader cursor item describes the replay risk backwards - Severity: Medium
- Component: §5 Non-goals (an escalated deployment defect)
- Evidence:
  - `docs/designs/integrations-status-chips.md:222-228` says a restart "starts the reader fresh (events during the gap are not replayed)".
  - `deploy/stacks/compose.prod.yaml:150-153` says the cursor file exists so that "a restart resumes where it stopped instead of replaying the whole retention". That points the other way: with no usable cursor, the stream may replay.
  - `dashboard/braintown_consumer.go:381-383` re-reads the cursor file on every connection and sends its value as `Last-Event-ID`. `writeCursor` (`braintown_consumer.go:622-631`) ignores its errors, so a read-only file keeps whatever value it was given at deploy time. Every reconnect, not only a restart, then asks for every event since that fixed point.
  - I could not open the file to see its value (`prod.bt-consumer-cursor` is mounted from the secrets set), so which behaviour happens in production is unverified.
- Attack path: if the stale value is greater than 0, or the receiver replays retention when there is no ID, every reconnect replays pull-request opened and merged events into the PR flow. `prflow.go:395` logs and drops duplicates, which reduces the impact, but I did not verify that this dedup survives a restart or covers WhatsApp notices. A broken replay control would be filed as "loses events", and the escalation would be prioritised on that wrong premise.
- Remediation: rewrite the §5 bullet to describe both outcomes and how they depend on the file's value:
  - an empty file means events in the gap are lost;
  - a stale value greater than 0 means events since that value are replayed on every reconnect;
  - the receiver's no-ID behaviour is unverified.

  Cite `braintown_consumer.go:381` and `compose.prod.yaml:152-153`, and point to the PR flow's dedup (`prflow.go:395`) as the current mitigation. The escalated design should also log `writeCursor` failures, which are discarded today.
- Effort: small (in this design, wording only; the fix belongs to the escalated design)

### SEC-4 "Connected" chips stay green while deliveries are failing - Severity: Low
- Component: §2.2, `AIMHead` and `QAHead`
- Evidence:
  - `docs/designs/integrations-status-chips.md:75,80` gives `Connected · on` when `AIM.Configured` or `QA.Outbound` is true. `dashboard/integrations.go:90-91` sets `Configured` from the adapter's type alone, and `integrations.go:145-148` sets `Outbound` from the configuration alone. Neither reflects whether the far side is reachable.
  - The existing fixture shows the QA tools answering HTTP 500 (`dashboard/integrations_test.go:63-66,122`) on a view that this design would show as green "Connected".
- Attack path: no attacker needed. The far side fails (an item is given up, or the gateway is unreachable) while the most visible signal says healthy. That delays the operator's response to a broken integrity path (answers not reaching the QA tools, `integrations.go:132-135`).
- Remediation: either rename the label to "Configured", or map `QA.GaveUp > 0` or a recorded last-attempt error to `warn`. Add that case to TC-753.
- Effort: quick-win

### SEC-5 The Brain Town popover reassures the operator about hidden products without saying what still leaves - Severity: Low
- Component: popover text `ip-bt` (§2.3)
- Evidence: `docs/designs/integrations-status-chips.md:132` says a non-public product is "sent hidden, under an alias and with the task id as its title". `dashboard/braintown.go:370-372` also sends the lower-cased product id as `Key`, and `braintown.go:409,456` sends each task's kind and column, plus event times.
- Attack path: from the popover, an operator can conclude a hidden product is fully anonymised to a public service (`braintown.go:31`). The id, the task ids and the activity pattern are still linkable. This is low impact because ids are opaque (`docs/designs/brain-town-emitter.md:132`).
- Remediation: change the text to say the product id, task ids, kinds, columns and times are still sent, and only the name and titles are replaced.
- Effort: quick-win

### SEC-6 TC-756's secret check cannot fail, and no test fixes the chip labels to the closed set - Severity: Low
- Component: §3, the tests
- Evidence:
  - `docs/designs/integrations-status-chips.md:188` asserts that no fixture secret appears in a popover, but the design's own rule is that popovers interpolate no field (`:125-126`), so the check cannot fail.
  - The security-relevant invariant is that `Label` is never a reason string, host or configured value (`:45-46`). TC-753 (`:185`) checks the mapping rows, but nothing asserts that a rendered chip's text belongs to the closed label set.
- Attack path: a later edit that sets `Label` from `State` (for example to "show the reason") would pass every test. The page already shows hosts, so the new risk is a free-text reason or a future configuration value turning into chip text.
- Remediation: in TC-754, collect every `<span class="chip st-…">` text in the section and assert it is one of {Connected, Off, Enabled, Disabled, Unusable, Outbound off, On, Partly connected}. Keep or drop the popover secret check, but do not count it as coverage.
- Effort: quick-win

### SEC-7 Several citations are off by a line or a path - Severity: Low
- Component: document anchors
- Evidence:
  - `buildIntegrations` is cited at `integrations.go:69` but starts at `dashboard/integrations.go:70`.
  - The existing test is cited at `integrations_test.go:103` but is at `dashboard/integrations_test.go:104`.
  - `compose.prod.yaml:191` is really `deploy/stacks/compose.prod.yaml:191`.
  - §7 cites `dashboard/auth.go` with no line; the gate is `auth.go:541-590`.
- Attack path: none directly. A later reviewer cannot check the security claims in §7 and §5 from the anchors given.
- Remediation: correct the four anchors, and cite `auth.go:516-524` and `auth.go:568-587` for the default-deny claim.
- Effort: quick-win

Counts: Critical=0 High=0 Medium=3 Low=4
