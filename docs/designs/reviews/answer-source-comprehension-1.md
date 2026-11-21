# Design review: `answer-source` v1 (comprehension lens)

Reviewed `docs/designs/answer-source.md` against `docs/rules/gaos-design-review.md`, `docs/designs/README.md`, the `.claude/rules/` rubric files and the BRD, decision log and open-questions register.

**Isolation notice.** While I worked, the harness put the contents of `.claude/CLAUDE.md` into my context. I did not ask for it or open it. I did not use it for any finding, and I am not quoting any of it. Please count this run's isolation as compromised for your canary check.

---

## Paraphrase

### What this proposes
1. **A source record (`AnswerSource`).** Every answer gets a `{kind, client, origin, actor}` record, plus a stored `display`. It is saved on the existing answer events (`QuestionAnswered`, and `ExternalQuestionAnswered` when the item was answered). The asking event (`ExternalQuestionAsked`) gains an `askedBy` field naming the client that asked. Only `actor` comes from the client; the server works out the rest. A fixed phrase turns the record into text, for example "via QA tool (host) by user". `Event.actor` stores the email for a back-office answer and `client/username` for a machine answer.
2. **A client registry.** The QA token becomes a named client, `qa-tools`. An optional file named by `GAOS_API_CLIENTS_FILE` adds more clients, one per line: name, kind, origin, token file, products. A new `scopeClient` level sits below QA and reaches only the three inbox routes. A bad line, or a line whose token equals another principal's, is skipped instead of turning off the whole API.
3. **Store changes.** `AnswerExternal` takes a struct, applies a "you can only answer what you asked" rule, returns typed errors for "already answered" and "expired" together with the item, and records the source. `AnswerQuestion` takes a source. A new read, `RecentAnswers`, is added.
4. **A new machine route.** `POST /v1/products/{id}/inbox/{externalId}/answer` runs a fixed sequence of checks. It refuses the admin token, screens for secrets, validates the actor, treats an identical retry as a no-op, and returns 409 or 410 with the GET body attached.
5. **`source` on GET.** The GET body gains a `source` field, and GET also enforces the "only what you asked" rule.
6. **`GAOS_QA_ORIGIN`.** This sets the QA tool's public host name. Without it, the host of `GAOS_QA_URL` is used when it is a name rather than an IP. It is wired into prod compose and `check-stacks.sh`.
7. **Back-office changes.** Answers record the session email and the deployment label. Refusals name who answered first. The inbox gets an "Answered recently" list, the timeline text ends with the phrase, and the conversation sheet says who answered.
8. **Tests, docs and registers.** Ten test cases (TC-490 to TC-499), D-083, the new BR-57 and an added sentence for BR-52.

### How the existing system works, per this document
- Back-office users are the only ones who answer inbox items. The machine API can ask a question and poll for the result, but cannot answer.
- Neither answer event says who answered. The `Event.actor` column is always "operator".
- The API knows two principals, admin and QA, matched by comparing token hashes in constant time. The QA token is limited to certain products, and each principal can have one write in flight.
- If two of the admin, QA and outbound tokens are equal, the whole API is turned off.
- After an answer, a worker sends the decision to the QA tool's webhook. The QA tool keeps the first answer it receives for each `externalId`.

I could paraphrase every section. Most `file:line` anchors I checked are right or within a line or two. Two are slightly off:
- `buildInbox` ends at `handlers_bo.go:757`, not 765.
- The D-082 row cell is at `decision-log.html:1684`, not 1683.

---

## Findings

### "Source" already means something else in the same struct the design changes - Severity: Major
- Affected scope: §0 Glossary "Source"; §3.1 `QuestionPayload`; §3.5 GET `source`; §3.7 "the QA tool tag"
- Finding: The glossary says "Source" means where an answer came from. But the struct the design extends, `QuestionPayload`, already has `Source string json:"source"` holding the literal `"QA tool"` (`dashboard/questions.go:70`, set from `ExternalSource` at `external_inbox.go:28,193`). `InboxRow.Source` reuses it, and `needs.html:31,40,113,119,120` uses `{{if .Source}}` to decide whether a row or sheet belongs to the QA tool. §3.7's "the **QA tool** tag" is exactly this old field, and the document does not say so. After this change, the same payload has `source: "QA tool"` (meaning "this is an external item") next to `answeredBy: {…}` (meaning "who answered"). Meanwhile the API's GET body gets a key `source` with the second meaning. An implementer adding a `Source` field to `RecentRow`, or reading "source" in the persisted payload, can easily mix the two up.
- Recommendation: Add a glossary entry for the existing `QuestionPayload.Source`/`InboxRow.Source`, say it is unchanged, and name it differently in the prose (for example, the "origin tag"). Say that the GET body's `source` is the `AnswerSource` and is unrelated to the payload's `source`. Consider naming the API key `answeredBy` to match the payload.

### Who detects the "identical retry", and how, is undecided - Severity: Major
- Affected scope: §3.3 item 2 step 2; §3.4 Responses table rows 2–3; BR-57 text
- Finding: In the store, every already-answered item returns `ErrAlreadyAnswered` (§3.3 step 2), with no exception for the same client. But §3.4 says an identical retry by the same client is a 200 that writes nothing. The document never says whether the store or the handler does the comparison. It also never says which fields are compared, or in what form (client via `AnsweredBy.Client`? text after `inboxText` or after `strings.TrimSpace`? `actor.display`?). Two engineers would build different behaviour for a retry that differs only in `display`, or in whitespace inside the text.
- Recommendation: State where the comparison runs. For example: "the handler, on `ErrAlreadyAnswered`, compares `item.AnsweredBy.Client == client.Name && item.Choice == in.Choice && item.Text == cleaned && item.AnsweredBy.Actor == username`; `display` is ignored." Add a test row for the edge cases this rules in or out.

### Two clients in one product share one `externalId` space - Severity: Major
- Affected scope: §3.3 item 1 (`AskExternal`); §3.4 404 row; §3.5 creator rule; §4 "only items you asked"
- Finding: An item is identified by `(product, externalId)` (`external_inbox.go:24-25,171-173`). Asking with an existing id returns the existing item and writes nothing. The design scopes reading and answering to the creating client, but not asking. So a registry client asking with an id the QA tool already used gets back a 200 with `created:false` and the other client's item status (leaking that it exists and its state); its own question is silently never recorded; its follow-up GET or answer is a 404.
- Recommendation: Decide the rule and write it down: key items by `(product, askedBy, externalId)`, or on ask return 409 or 404 when the existing item's `AskedBy` differs from the caller. Add the chosen case to TC-492 or TC-493.

### `apiGate` "unchanged in shape" contradicts the handlers needing the client - Severity: Nit
- Affected scope: §3.2 "The scope ladder", `apiProduct`/`beginWrite` paragraph; §3.4 steps 2, 3, 5, 9
- Finding: §3.2 says `authorize` returns `(apiScope, *apiClient)` but `apiGate` is "unchanged in shape". Today `apiGate` returns only `(apiScope, bool)` (`api_v1.go:175`). But `apiProduct` needs `client.products`, `beginWrite` needs `client.busy`, and step 9 needs `client.Name`, `Kind` and `Origin`.
- Recommendation: Say that `apiGate` returns the client too (and give its new signature), or say where else the client comes from.

### The phrase for a missing source produces broken sentences - Severity: Nit
- Affected scope: §3.1 phrase table; §3.3 step 2 sentence; §3.7 Recent list
- Finding: When the source is nil, the phrase is `source not recorded`. In the store's template it reads "That question was already answered source not recorded." In the recent list's `answered <phrase>`, it reads "answered source not recorded". Pre-change answered items will hit the 409/refusal path immediately after deploy.
- Recommendation: Give the nil case its own sentence for each surface, e.g. "That question was already answered (source not recorded)." and "answered · source not recorded".

### A registry `kind` of `qa-tool` would display as "via QA tool" - Severity: Nit
- Affected scope: §3.2 registry rules; §3.1 phrase table
- Finding: Only `gaos` is reserved as a kind. A registry client given `kind qa-tool` gets the phrase "via QA tool (…)".
- Recommendation: Either reserve `qa-tool` as a kind, or say that it is deliberately allowed and why.

### `Display` is stored but has no reader - Severity: Nit
- Affected scope: §3.1 `AnswerSource.Display`; §3.4 step 8; §3.5
- Finding: `display` is validated, screened and stored, but is not returned by the API, not part of the phrase, and not named on any back-office surface.
- Recommendation: Name the surface that shows it, or drop the field.

### Undefined deployment terms - Severity: Nit
- Affected scope: §3.6 ("the tunnel", "rnd-v3-vpn", "the redeploy line")
- Finding: project-specific terms never defined or linked; `rebuild-restart.sh:102` is a comment, so "the redeploy line" probably means an operator's shell command outside the repository.
- Recommendation: Link to `deploy/STACKS.md` for the tunnel and the host. Say exactly where the operator adds `GAOS_QA_ORIGIN=…`.

### `RecentAnswers` interface plumbing not listed - Severity: Nit
- Affected scope: §3.3 item 4
- Finding: Item 4 does not say that `RecentAnswers` joins the interface, or what `readOnly` does with it. `readOnly` has an `AnswerExternal` stub at `store.go:313`, which must also change signature.
- Recommendation: Add one line covering the interface, the Arcade delegation and `readOnly` for the new read, and the `readOnly` `AnswerExternal` signature change.

### BR-52's existing "equal tokens disable the API" sentence now needs a qualifier - Severity: Nit
- Affected scope: §3.10 BR-52 addition
- Finding: `brd.html:1345` says flatly that "equal tokens … disable the API". The appended sentence makes registry clients an exception but leaves the blanket statement unqualified.
- Recommendation: Reword to "equal admin, QA or outbound tokens disable the API".

The rest holds up: goals and non-goals, the alternatives, the accepted limits, the counts (489→499 and 365→374), and keeping the operator's email out of the packet via the unchanged `questionBody`.

Counts: B=0 M=3 N=7
