# Design review: `answer-source` v1 (critic lens)

**Isolation note for the author.** I did not open any file on the deny-list. The harness did push the contents of `.claude/CLAUDE.md` into my context anyway, along with an older copy of `.claude/rules/qa-test-plan.md` that says TC-477, where the worktree copy says TC-489. I used neither for any finding. Your canary probe should treat this run as exposed to `.claude/CLAUDE.md`.

**The case against the design.** The design's main claim is that a client registry makes the inbox multi-client "by a configuration line". But the parts under the registry are still QA-tool-only: the webhook destination, the `Source` label, the event text, the `externalId` namespace. So the first registry client would mislabel its items, leak its answers to the QA tool, and share an id namespace with the QA tool. The smallest design that delivers the request is the QA-tool answer route plus recording the source.

### Registry clients' answers go to the QA tool's webhook and are labelled "QA tool" - Severity: Blocker
- Affected scope: §3.2, §3.3 "The webhook worker is untouched", §3.10 BR-57, TC-493.
- Finding and rationale: the worker sends every pending item to `{GAOS_QA_URL}/api/v1/gaos/inbox-answer` with the QA outbound token (`dashboard/inbox_worker.go:12-19,122-164`); `ExternalPending` does not filter by asker (`dashboard/external_inbox.go:268-278`). A registry client's item and the operator's free-text answer are POSTed to the QA tool, which will reject or ignore it; after 5 attempts it becomes `gave-up`, a warn event. BR-57 promises "delivered to the asking tool" with no URL or token in the registry line. Every item is stamped `Source: ExternalSource` ("QA tool", `external_inbox.go:28,193`) and the event text says "QA tool asks:"/"QA tool question answered" (`:196,244`). The same applies to admin-asked items after this change.
- Recommendation: (a) drop the registry; or (b) per-client delivery target (validated URL plus outbound token), worker routes by `AskedBy`, `Source` and the event text use the client's name; say what happens to an item whose asker has no delivery target.

### The `externalId` namespace is shared across clients - Severity: Major
- Affected scope: §3.2, §3.3 step 1, §3.5.
- Finding and rationale: identity is (product, externalId) (`external_inbox.go:24-25,88`); asking a taken id returns `created:false` and its status (`:171-172`, `api_inbox.go:175-179`). A registry client can check whether the QA tool has an id, or claim one first so the QA tool's later GET and answer are 404. Same when the admin token asks first.
- Recommendation: identity (product, asker, externalId), or apply the creator rule to ask (409 `id-taken`). Add a test.

### The registry is speculative surface - Severity: Major
- Affected scope: §2 G3, §3.2, §3.6, §4 "Registry in code — Rejected by the brief", §8.
- Finding and rationale: the request is a button on the QA tool's `/history` page; the named future clients need task-question answering, which is deferred. The registry adds a fourth scope, a parser, a weaker collision rule and a BR-52 amendment with zero users. "Rejected by the brief" points at a document the reader does not have (README rule 1).
- Recommendation: state the config-only requirement in §1 with its source, or defer the registry; if kept, explain why "add it with the first real client" loses.

### The operator's email leaves GAOS through the machine API - Severity: Major
- Affected scope: §3.1 table, §3.4 409 `reason`, §3.5 GET `source.actor`.
- Finding and rationale: GET returns `source.actor` = the operator's email to the QA tool for every back-office answer; the 409 reason carries it too. New exposure; append-only storage. `display` is unused personal data kept forever.
- Recommendation: decide explicitly (D-083 consequence plus a §5 limit): e.g. return `actor` only for the calling client's answers and "operator" for back-office answers. Drop `Display` or show it.

### The first-answer contract with the QA tool is unspecified - Severity: Major
- Affected scope: §0, §3.4 409/200, §5 L3, N3.
- Finding and rationale: the QA tool keeps the first answer it sees per `externalId` (`docs/designs/qa-inbox-api.md:115,151`). If it records its user's approval locally and then gets 409, the back-office answer arriving by webhook is dropped as a duplicate and the two systems disagree permanently.
- Recommendation: a "What the QA tool must match" section: record an answer only on 200; on 409 or 410 adopt the item in the response body.

### Count drift and missing register updates - Severity: Major
- Affected scope: §6, §3.9.
- Finding and rationale: "Eighty-one entries" (`decision-log.html:18`, `toc/product.js:129`) with D-082 present; only 59 `<tr id="d-` rows on the page — the basis is not stated. `.claude/rules/qa-test-plan.md` (highest TC, totals, range map, decision-log count) is missing from §3.9 and `check-workspace.sh` checks it.
- Recommendation: give the real figure with its basis; add the rule file.

### No consolidated change list - Severity: Major
- Affected scope: whole document; README rule 3.
- Finding and rationale: missing: the `Store` interface and `readOnly` stubs (`dashboard/store.go:150-152,310-317`), `RecentAnswers` on ArcadeStore, the view types, how a handler reaches `s.auth.label`, the tests that break (`api_v1_test.go:111,118`, `external_inbox_test.go:139,143` use `qaBusy`; `api_v1_test.go:135,140` call `readTokenFile` by env name). Contradicts §3.8's "existing tests change only by passing `AnswerSource{}`".
- Recommendation: add a change list naming every file; correct §3.8.

### `AnsweredBy` nil rule differs between the two answer paths - Severity: Nit
- Affected scope: §3.3 steps 2.5 and 3.
- Finding: `AnswerQuestion` stores nil when `by.Kind == ""`, `AnswerExternal` always stores `&in.By`.
- Recommendation: one rule for both.

### Reserve kind `qa-tool` - Severity: Nit
- Affected scope: §3.2.
- Recommendation: reserve `qa-tool` as well.

### 409/410 bodies must apply the `lapsed` adjustment - Severity: Nit
- Affected scope: §3.3 step 3, §3.4.
- Finding: `ExternalGet` rewrites an overdue open item as expired (`external_inbox.go:209-211`); the store's returned item may not, so the 410 body could say `status:"open"`.
- Recommendation: say the returned item goes through the same rewrite.

### "source" means two things - Severity: Nit
- Affected scope: §0, §3.5.
- Finding: `QuestionPayload.Source` is already JSON `source` holding "QA tool" (`questions.go:70`).
- Recommendation: rename one, or add a glossary note.

### L1 contradicts the existing design - Severity: Nit
- Affected scope: §5 L1, §3.5.
- Finding: `docs/designs/qa-inbox-api.md:59` documents that the admin token reaches ask and GET; after this change the QA tool also loses GET on items the admin asks.
- Recommendation: restate L1 and the §3.5 claim accurately.

### Answers compete with sync pushes for the one write slot - Severity: Nit
- Affected scope: §3.4 step 5, N5.
- Finding: the QA token's single in-flight slot (`api_v1.go:225-236`) is shared by a human clicking "answer" and bulk `/qa/results` pushes.
- Recommendation: say so, and tell the QA tool to retry on 429 after `Retry-After`.

### `Event.actor` now mixes verified and client-reported identities - Severity: Nit
- Affected scope: §3.1 `Payload.Actor`.
- Finding: the column is documented as "agent id, user id, or 'system'" (`dashboard/schema/07-events.sql:26`).
- Recommendation: record in D-083 that the `<client>/` prefix marks an identity nobody verified.

### Register wording left stale or unregistered - Severity: Nit
- Affected scope: §3.10, §8.
- Finding: BR-48 still says "events carry no actor" (`brd.html:1327`); BR-57's section is not named; §8's undecided question is not registered in `open-questions.html`.
- Recommendation: update BR-48, place BR-57 (§9.8), open an OQ for §8.

### `GAOS_BO_LABEL` used as an origin is unanchored - Severity: Nit
- Affected scope: §0, §3.1.
- Finding: the label is free text that binds sessions (`auth.go:259,308`), not checked against the hostname pattern, and would now be sent to clients.
- Recommendation: anchor the prod value, or check it against the origin pattern.

**Solid:** the §3.1 line references, `api_v1.go`, `api_inbox.go`, `external_inbox.go`, `main.go:390,406-407`, the store-wrapper pattern, the TC arithmetic, and BR-05 staying closed.

Counts: B=1 M=6 N=9
