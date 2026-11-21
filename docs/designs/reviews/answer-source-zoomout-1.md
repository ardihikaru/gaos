# Design review: `answer-source` v1 (zoom-out lens)

Document: `docs/designs/answer-source.md`

**Paraphrase.** The QA tool should be able to answer the inbox items it created, over a new bearer-token route. Every answer, whether from the back office or a machine, records `{kind, client, origin, actor}` on the existing answer event. The design also adds a file-based registry of further machine clients, a new "Answered recently" list in the back office, and a `GAOS_QA_ORIGIN` setting.

**What I checked in the code.** The core is right-sized. No new event type is needed. `ExternalPending` (`dashboard/external_inbox.go:268-278`) already queues any answered item for the webhook. The first-wins check already exists (`external_inbox.go:226-231`). The secret screen can be reused as it is (`dashboard/api_inbox.go:146-169`).

**Arithmetic checked.** The register currently ends at TC-489 (`pustaka/docs/product/qa-test-plan.html:580`). The page and the ToC say 489 cases and 365 unit tests (`qa-test-plan.html:19,79`, `pustaka/docs/assets/toc/product.js:162,168`). TC-490…TC-499 is ten rows, nine of them Go tests. That gives 489 + 10 = 499 rows and 365 + 9 = 374 Go tests, so §6 adds up. One stale figure in the rubric: `.claude/rules/qa-test-plan.md` still says TC-477 / 353. That file is outside this design, but it will mislead whoever updates the counts.

---

### The client registry gives answers to the wrong tool and mixes up item identity - Severity: Blocker
- Affected scope: §3.2 (the registry), §3.3 step 1 (`AskedBy`), §3.10 BR-57, §7
- Finding: The design lets a registry client ask and answer inbox items, but the inbox underneath still assumes there is only one client, the QA tool:
  - **Webhook goes to the wrong place.** The webhook worker sends every answered or expired item to `GAOS_QA_URL` (`dashboard/inbox_worker.go:12-19`, `:32`), with no notion of which client asked. An item a `hikaru-app` client asked would have its decision, product id and text POSTed to the QA tool. BR-57's sentence "every answer, from any source, is delivered to the asking tool" is therefore false as designed.
  - **Two clients share one id space.** An item is identified by (product, externalId) alone (`external_inbox.go:88`, `:171-172`). If a second client asks with an id the QA tool already used, `AskExternal` returns the QA tool's existing item and its status. The new GET creator rule then answers 404 for that same item, so the ask and the GET disagree.
  - **Labels are hard-wired.** Every item is stamped `Source: "QA tool"` (`external_inbox.go:28`, `:193`), and the back office branches on that field (`dashboard/templates/needs.html:31,113,119`). The event text says "QA tool asks:" (`external_inbox.go:196`). A registry client's items would show as "QA tool" everywhere. The design never mentions the existing `QuestionPayload.Source` field (`dashboard/questions.go:70`), which already names the asker.
  - **No client needs it yet.** §3.6 says "no registry client exists", and §8 says the Hikaru apps need *task*-question answering, which this registry does not provide. So the registry serves no current client, and the one it was designed for does not need what it offers.
- Recommendation: Remove the registry from this change. That means dropping G3, `GAOS_API_CLIENTS_FILE`, `scopeClient`, the per-client `busy` flag, the parsing rules, TC-493, the BR-52 sentence and §7. Keep `AnswerSource` with `kind`/`client`/`origin` as data, so a later client needs no event migration. With only the QA tool and the admin token as callers, `AskedBy`/`AsClient` and the GET creator rule reduce to "the QA token answers, admin is 403", so drop them and L1 as well. When a second client really exists, design the registry together with per-client webhook routing, per-client id namespacing and per-client labels; all three are needed and none is here. The counts become nine rows: 499 → 498, and Go tests stay at 374.

### "Answered recently" is a new view the request did not ask for - Severity: Major
- Affected scope: G4, §3.3 item 4 (`RecentAnswers`), §3.7 "Answered recently", TC-497
- Finding: The request is to record where each answer came from. Two existing places already show that once the phrase is added. The timeline prints `Event.Text`, which §3.3 extends with the phrase (`dashboard/templates/backoffice.html:436`). The refusal toast names the first answer. The new list adds a store-interface method (on both stores), a template section, CSS and a test, all to show what the timeline already shows. L4 also admits it adds another full-stream scan on every inbox render.
- Recommendation: Ship the phrase in the event text, the refusal sentence and the conversation sheet. Defer "Answered recently" until an operator asks for it. If the operator wants it now, have the operator decide that explicitly rather than folding it into G4.

### `Display` is stored but never read - Severity: Nit
- Affected scope: §3.1 `AnswerSource.Display`, §3.4 steps 6–8
- Finding: `actor.display` is accepted, cleaned, screened, length-checked and stored. No API response returns it and no back-office surface in §3.7 renders it. It is client-reported data kept for a reader that does not exist.
- Recommendation: Drop `display` from the body and the struct. With the strict decoder, a client that sends it gets a 400, which is the honest answer. Add the field when a surface needs it.

### Answer events carry the actor twice - Severity: Nit
- Affected scope: §3.1, the paragraph on `Payload.Actor`
- Finding: The answerer is stored in `AnsweredBy.Actor` and again in `Payload.Actor` / the `Event.actor` column (as `<client>/<username>`). Today `actorName` (`dashboard/conversation.go:199-204`) reads that field only for conversation events. Two copies of one fact can drift apart.
- Recommendation: Either state why the column must also hold the actor (for example, a query on the indexed column), or leave `Payload.Actor` unset on answer events and read `AnsweredBy` alone.

### The `GAOS_QA_URL` fallback is never used in production - Severity: Nit
- Affected scope: §3.6 steps 2–3, TC-494
- Finding: Production's `GAOS_QA_URL` is an IP address (§3.6), so production must set `GAOS_QA_ORIGIN` anyway. Parsing the URL and detecting an IP literal only helps setups that do not exist.
- Recommendation: Use `GAOS_QA_ORIGIN` if it is set and valid, otherwise `""`. That cuts TC-494 to two cases.

---

Solid sections: the rejection of a client-supplied `source` (§4), the choice of a payload field over a new event type (§3.3, §4), keeping the email out of `questionBody` (§3.1), and leaving the webhook body unchanged (N3).

Counts: B=1 M=1 N=3
