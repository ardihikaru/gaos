# Design review: `answer-source` v2 (zoom-out lens, round 2)

**Paraphrase.** The QA tool gets a bearer-token route to answer the inbox items it asked. Every answer, from the back office or a machine, records `{kind, client, origin, actor}` on its existing answer event. Machine clients come from a registry file. Each item now belongs to the client that asked it: that client's label, its own open-item cap, its own ids, and webhook delivery only for the QA tool's items. The source is shown in a new "Answered recently" list, the timeline, refusals and the conversation sheet.

**What I checked in the code.** The scope ladder and `apiGate`'s `got < need` (`dashboard/api_v1.go:33-39,194`); `apiProduct`'s `scope == scopeQA` branch (`:206`); `beginWrite`'s two flags (`:225-236`); the hard-wired `ExternalSource` (`dashboard/external_inbox.go:28,193,196`); the first-wins refusals (`:226-231`); the `Event.actor` column (`dashboard/store_arcade.go:1075`); the three "QA tool" wordings (`dashboard/templates/needs.html:40,113,119-120`); the `handleExternalSheet` refusal (`dashboard/handlers_bo.go:844`).

**Arithmetic checked.** Register ends at TC-489 (`qa-test-plan.html:580`); 489 + 10 = 499 rows, 365 + 9 = 374 Go tests, 343 + 9 = 352 dashboard rows. "Eighty-one entries" with D-082 last (`decision-log.html:18,1684`), so "Eighty-two" follows. One count is wrong: §3.2's "twenty existing test calls" of `newAPIConfig` are 19 (`api_v1_test.go` 11, `qacatalog_create_test.go` 5, `seed_test.go` 2, `qaclient_test.go` 1).

---

### G3's "no code change" promise is false for the two clients it names, and v2 makes the registry more expensive - Severity: Major
- Affected scope: §1, G3, §3.2, §3.3 items 1–3, §3.5, §3.7, N7, §8, §4
- Finding: §1 quotes the brief: a future hikaru-app or hikaru-android is "added by configuration plus a token file, not code". §8 says those apps need to answer task questions, which needs a new route; N7 says they get no push. So neither named client can be onboarded by configuration alone. The round-1 contest kept the registry because the brief asked for it, without weighing this, nor the cost v2 added to make it safe (`AskedBy`/`effectiveAsker`, `ErrIDTaken`, the per-asker cap, `Label`/`Asker`, `DeliveryPoll`, owner-only GET, `scopeClient`, `apiClient`, `parseClients`/`addClients`, the reserved rules; most of TC-493). All serve a client that will not exist on day one.
- Recommendation: correct G3 ("an inbox-only client is added by configuration; the Hikaru apps still need §8's route") and put the choice to the operator in §11: (a) ship the registry as designed, or (b) ship the QA route and `AnswerSource` now and build the registry with §8.

### `DeliveryPoll` and the `ExternalPending` asker filter do the same job twice - Severity: Nit
- Affected scope: §3.3 item 3
- Finding: `Delivery.State` is read in one non-test place, the `ExternalPending` filter (`external_inbox.go:273`). Either mechanism alone suffices.
- Recommendation: keep the `effectiveAsker` check in `ExternalPending`; drop `DeliveryPoll`.

### `ExternalAnswer.AsClient` repeats what `By` already says - Severity: Nit
- Affected scope: §3.3 item 2
- Finding: on the machine path `AsClient == By.Client`; on the BO path `AsClient == ""` and `By.Kind == "gaos"`. Two fields encode one fact.
- Recommendation: derive the owner check from `By` and drop `AsClient`.

### The conversation-sheet line can only ever say "in GAOS by the operator" - Severity: Nit
- Affected scope: G4, §3.7, TC-498
- Finding: N2 keeps task answers back-office only, so every new row reads "in GAOS … by <email>". The brief asks for the source in the inbox history, not on the card.
- Recommendation: defer to §8, or say in G4 why it is wanted now.

### Changing the store's return types breaks about twenty test calls, and §3.8 lists the wrong lines - Severity: Nit
- Affected scope: §3.3 item 2, §3.8, §3.2
- Finding: `AnswerExternal` has 19 call sites in `external_inbox_test.go` (33, 176, 359, 369, 399, 440, 450, 480, 481, 582, 583, 625, 643, 690, 704, 777, 780, 783, 822) and readOnly cases at `store_conformance_test.go:846,848`. `events_test.go` and `runner_manual_test.go` call only `AnswerQuestion`. The extra `ExternalItem` return is what changes every call site; only the API success path needs it.
- Recommendation: keep `(Result, error)` and call `ExternalGet` after a success; fix the anchors; "twenty" → 19.

Solid: `*AnswerClosed` follows the typed-refusal pattern (`dashboard/store.go:214`); `readTokenPath`/`warnIfOpen` reuse; `newAPIConfig`/`readTokenFile`/`qaBusy` kept; no new event type; the `Payload.Actor` rationale; §6 arithmetic.

Counts: B=0 M=1 N=4
