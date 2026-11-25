# gaos-brain-town-consumer — review 1 (comprehension)

> **Reviewer:** the zero-context comprehension goldfish
> **Document reviewed:** `docs/designs/gaos-brain-town-consumer.md`
> **Rubric:** `.claude/rules/rule-index.md`, `.claude/rules/qa-test-plan.md`,
> `.claude/rules/agent-workspace.md`, plus the domain sources the rule index names.

## What this proposes

GAOS cannot see GitHub pull-request activity, so it will open a *reader*: one long-lived
goroutine that pulls Server-Sent Events from Brain Town's new consumer endpoint
(`GET {BRAIN_TOWN_CONSUMER_URL}/consumer/events`) with a scoped bearer token held in a
file, and hands each accepted event to a separate pull-request flow. The reader is
configured by five `BRAIN_TOWN_CONSUMER_*` settings (enabled, base URL, token file,
accepted town event types, accepted event kinds); anything unusable leaves the reader off
and is reported in the Integrations view rather than stopping GAOS. It filters twice: once
by accepted type/kind, then again by GAOS's own products, so an over-broad Brain Town scope
cannot make GAOS act on a foreign repository. It keeps only the last sequence number,
reconnects with 1 s / 5 s / 30 s backoff, resumes with `Last-Event-ID`, and drops events
into a bounded non-blocking channel (capacity 256). It hands a small `PREvent` struct
(kind, type, event id, seq, org, repo, PR number, title, draft, timestamp) to the flow.
Four alternatives are rejected (push, admin stream, alias-mapping the public stream, a
direct GitHub webhook). Eleven files are listed as changing.

## How the existing system works, per this document

GAOS already pushes committed state to Brain Town through `dashboard/braintown.go`, off by
default, with a file-held token, https-only URL rules, a host denylist, and a masked/hidden
product payload. The public stream (`GET /events`) hides hidden products behind an alias and
strips titles; the admin stream (`GET /admin/events`) carries the stored envelope but needs
an operator session cookie. Brain Town already ingests the GitHub webhook and emits
`pr.opened` / `pr.merged`; GAOS has no webhook receiver and no visibility of those events.
The proposed consumer path is the middle ground: a scoped, revocable, machine-only stream
carrying the stored envelope.

## Findings

### The hand-over design does not exist - Severity: Blocker
- Affected scope: §2 ("the pull-request flow described in `docs/designs/gaos-pr-agent-flows.md`"), §5, §8, §10
- What is wrong: The flow that consumes `PREvent`, de-duplicates it, and owns the GitHub client is defined by a document that is not in the repository. The reader's payload contract, the replay-is-a-no-op guarantee (§8) and the "a separate GitHub client" (§5) therefore have no specification.
- Evidence: `docs/designs/` contains no `gaos-pr-agent-flows.md`; `grep -rn "gaos-pr-agent-flows"` matches only this document (lines 32, 223).
- Fix: Land or link the flow design and state its interface here (channel type, `PREvent` consumption, dedupe key — `EventID` or `Seq`?), or restate the contract inline in this document.

### The double filter has no defined rule - Severity: Blocker
- Affected scope: §2 ("a scope that is too broad still cannot make GAOS act on a repository it does not own"), §4.1 ("A frame whose `productKey` is not one of GAOS's own product repositories"), §11
- What is wrong: The filter is stated two incompatible ways. `productKey` is never defined; the `PREvent` struct (lines 118–132) carries no product key at all but `Org`/`Repo`; and a product key is not a repository.
- Evidence: Town `product.key` is the lower-cased product id (`docs/designs/brain-town-emitter.md:132`), and a GAOS product carries `Owner` + `Repo` (`dashboard/model.go:1015-1019`). No GAOS field holds a `productKey`.
- Fix: State the rule in one sentence — which envelope field is compared to which GAOS field (`Org`/`Repo` against `Product.Owner`/`Product.Repo`?) — and say what a mismatch does.

### The SSE payload's wire format is never given - Severity: Major
- Affected scope: §4.1 (`data:` is the stored envelope as JSON), §5
- What is wrong: §5 gives Go field names (`Org`, `Repo`, `PRNumber`) but never the JSON field names, the envelope's `id`/`productKey` members, or the `data` shape. Two engineers would write two parsers.
- Evidence: The outbound side specifies its body exactly (`docs/designs/brain-town-emitter.md:20`); `docs/designs/README.md:20` requires every claim to carry a file and a line.
- Fix: Quote the frame contract (`id:`, `event:`, `data:` and the JSON members) from Brain Town's `docs/integrations/gaos-consumer.md`, with the section reference.

### The resume point is not persisted, yet the restart case depends on it - Severity: Major
- Affected scope: §4.2 ("keeps the last sequence number it handled and nothing else") vs §8 ("GAOS restarts … replays what it missed")
- What is wrong: An in-memory sequence number is gone after a restart, so §8's restart row and §11's replay check cannot hold without persistence. §4.2 rules persistence out implicitly.
- Evidence: The existing emitter keeps only in-process state (`dashboard/braintown.go:270-277`); no store write is described.
- Fix: Say where the last sequence number lives (file, store row, or nowhere) and reconcile §4.2 and §8.

### Mode 600 is described as enforced; the cited function only warns - Severity: Major
- Affected scope: §3 ("The token file's rules copy `readBrainTownToken` … the file must not be readable by others"), §9
- What is wrong: `readBrainTownToken` does not refuse a group/other-readable file; it logs a warning and uses the token anyway.
- Evidence: `dashboard/braintown.go:132-148` calls `warnIfOpen` (`dashboard/braintown.go:133`), which only logs (`dashboard/auth.go:249-252`). The repo's stricter rule for a machine token file is refusal: `dashboard/api_v1.go:133` rejects a file whose mode is not private. `BR-52` (`pustaka/docs/product/brd.html:1346`) says a client whose token file others can read "is not loaded".
- Fix: State which behaviour the reader has — refuse or warn — and, if refuse, cite `dashboard/api_v1.go:133` as the rule it copies.

### The reader never refuses GAOS's own credential, and the `bt_c_` rule is ambiguous - Severity: Major
- Affected scope: §3, §9
- What is wrong: Only a `bt_live_` token is refused. The document never says whether `bt_c_` is *required* or merely that `bt_live_` is refused, and never says a GAOS token is refused — the exact leak the outbound feed already guards against.
- Evidence: The outbound refuses a GAOS credential at `dashboard/braintown.go:121-124` (`apiTokenPrefix = "gaos_"`, `dashboard/api_v1.go:47`; `holdsToken`, `dashboard/braintown.go:151-165`); the risk was recorded as SEC-9 (`docs/designs/reviews/brain-town-emitter-security-review-1.md:18`). `bt_c_` appears nowhere in the repository except this document's line 48; `bt_live_` is a known prefix (`dashboard/egress.go:361`, `deploy/check-workspace.sh:203`).
- Fix: State "the token must start with `bt_c_`" and "a `gaos_` token or one equal to GAOS's own is refused", and add the new prefix to the workspace secret list and the egress detectors.

### The change list is missing files the design itself requires - Severity: Major
- Affected scope: §6, §11
- What is wrong: The list omits the QA register §11 mandates, the ToC entry the new published page needs, the back-office template that renders the Integrations row, the release-hygiene pair, and the prod secret mount.
- Evidence: `pustaka/main.go:1340` rejects a page on disk that is not registered in `assets/toc.js` or a part file (the sibling entry is `pustaka/docs/assets/toc/integrations.js:54`); the row is rendered by `dashboard/templates/backoffice.html:730-739`; `.claude/rules/qa-test-plan.md:19-25` requires `pustaka/docs/product/qa-test-plan.html` in the same change, and `:97-100` requires `pustaka/docs/changelog.html` plus `product.semver` in `pustaka/docs/assets/toc.js`; the prod token file is a mounted secret (`deploy/stacks/compose.prod.yaml:149-158`) asserted by `deploy/check-stacks.sh:154-156`.
- Fix: Add those files to §6, including a `./prod.bt-consumer-token:/run/secrets/bt-consumer-token:ro` mount and its stack-check assertion.

### The counts contradict the table and themselves - Severity: Major
- Affected scope: §6 ("Counts. 11 files: 3 new Go files, 4 changed Go files … The Go files: 2 new … 3 changed")
- What is wrong: One paragraph states two different arithmetic results. The table has 2 new and 3 changed Go files, 5 new non-Go files and 6 changed — and the files listed in the paragraph above are missing too.
- Evidence: §6 table lines 142-152; `docs/designs/README.md:21-22` requires the counts to follow from the change list and the arithmetic to be written down.
- Fix: Recount after the change list is complete, and write the arithmetic in one place.

### `BRAIN_TOWN_CONSUMER_KINDS` is defined but never used - Severity: Major
- Affected scope: §3 (table row "Comma-separated event kinds. Default `github`"), §4.1 ("`?kind=github`")
- What is wrong: The loop's request is written with a literal `?kind=github`, so the setting has no stated effect; an operator changing it gets the old behaviour.
- Evidence: §4.1 line 75 versus §3 line 50.
- Fix: Write "`?kind=` is the configured list, comma-joined" and say how several kinds are encoded in one query parameter.

### The Brain Town-side claims are unanchored - Severity: Major
- Affected scope: §1, §4.1, §5, §7.2, §7.4, §8
- What is wrong: "retry: 3000 and a heartbeat every 15 s", the `401` behaviour, the masking rules, "the GitHub payload is never stored in Brain Town" and `backend/internal/github/` are asserted without a file and a line, and `backend/internal/github/` is a path in another repository that is not identified.
- Evidence: `docs/designs/README.md:20` — a claim that cannot be anchored must be labelled as an assumption; the Brain Town repository has no `backend/` directory.
- Fix: Cite `docs/integrations/gaos-consumer.md` (Brain Town) section by section, and label the rest as assumptions.

### Citation drift on the "no secret" rule - Severity: Nit
- Affected scope: §4.3 (`dashboard/integrations.go:21-23`)
- What is wrong: The quoted sentence "It is read-only and it renders no secret" sits at lines 18–20, not 21–23.
- Evidence: `dashboard/integrations.go:18-20`.
- Fix: Correct the line range.

### The examples name what appears to be an operator's private repository - Severity: Nit

<!-- D-085: the private product name this finding quotes has been reworded; nothing else in the finding changed. -->
- Affected scope: §1 (the sentence about the product's repositories being private), §5 (the two example names)
- What is wrong: The design publishes a product name and a GitHub org/repository in a repository whose docs site is public; the repo's own convention is generic examples (`docs/designs/brain-town-emitter.md:20` uses `p-6`).
- Evidence: `docs/designs/README.md`; `D-085` (`pustaka/docs/product/decision-log.html:1787`) treats an operator's product and domain as private strings.
- Fix: Use generic org/repo names in examples.

### The base URL may carry a path the reader then appends to - Severity: Nit
- Affected scope: §3 (URL rules), §4.1 (`{BRAIN_TOWN_CONSUMER_URL}/consumer/events`)
- What is wrong: The rules forbid userinfo, query and fragment but not a path, so a base URL with a path yields a wrong endpoint.
- Evidence: `validOutURL` refuses userinfo/query/fragment only (`dashboard/qaclient.go:76-80`); the design cites it at §9.
- Fix: Say the base URL carries no path, as `urlHost` shows only scheme and host (`dashboard/integrations.go:39-60`).

---

`Verdict: fix first` — Counts: B=2 M=8 N=3

## Triage

| Finding | Severity | Disposition | Note |
|---|---|---|---|
| The hand-over design does not exist | Blocker | PROPOSE FIX | `docs/designs/gaos-pr-agent-flows.md` was drafted after this review started and is now in the tree; the consumer design names it and states the hand-over contract inline (§4.2, §5). |
| The double filter has no defined rule | Blocker | PROPOSE FIX | The rule is now one sentence: the frame's `orgLogin` + `productKey` are matched against the product's `Repos` entries, which are `owner/repo` pairs (`dashboard/model.go:388`); a mismatch is skipped and counted. `productKey` is named as Brain Town's bare repository name. |
| The SSE payload's wire format is never given | Major | PROPOSE FIX | The frame contract is quoted from Brain Town's `docs/integrations/gaos-consumer.md`, "The frame" section, with the JSON member names. |
| The resume point is not persisted | Major | PROPOSE FIX | The cursor is a file, `BRAIN_TOWN_CONSUMER_CURSOR_FILE`, mode 600, holding the last sequence number handled. §4.2 and §8 are reconciled. |
| Mode 600 is described as enforced; the cited function only warns | Major | PROPOSE FIX | The reader **refuses** a token file others can read, copying `dashboard/api_v1.go:133` and BR-52. The warning-only `readBrainTownToken` is named as the *other* rule, for the outbound feed. |
| The reader never refuses GAOS's own credential | Major | PROPOSE FIX | The token must start with `bt_c_`; `bt_live_`, `gaos_`, and any token equal to one of GAOS's own are refused. `bt_c_` joins the workspace secret list and the egress detectors. |
| The change list is missing files | Major | PROPOSE FIX | The view structs, the back-office template, the docs ToC part, the QA register, the changelog pair, the prod secret mount and the stack assertion are all in §6 now. |
| The counts contradict the table and themselves | Major | PROPOSE FIX | Recounted; the arithmetic is written in one place. |
| `BRAIN_TOWN_CONSUMER_KINDS` is defined but never used | Major | PROPOSE FIX | `?kind=` is the configured list, comma-joined; one kind is the common case. |
| The Brain Town-side claims are unanchored | Major | PROPOSE FIX | Every Brain Town claim now cites `docs/integrations/gaos-consumer.md` in the Brain Town repository; what stays unanchored is labelled an assumption. |
| Citation drift on the "no secret" rule | Nit | PROPOSE FIX | Corrected to `dashboard/integrations.go:18-20`. |
| The examples name an operator's private repository | Nit | PROPOSE FIX | The examples now use generic names (`acme-org`, `sample-service`), per D-085. |
| The base URL may carry a path | Nit | PROPOSE FIX | The base URL carries no path; the rule is stated with the URL rules. |
