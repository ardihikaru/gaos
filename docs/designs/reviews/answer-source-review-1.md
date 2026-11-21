# Review record 1: answer-source (design v1 → v2)

Design: `docs/designs/answer-source.md`. Round 1 ran five goldfish lenses on v1 in parallel, each given the design path and the rubric path list only: `answer-source-comprehension-1.md`, `answer-source-zoomout-1.md`, `answer-source-critic-1.md`, `answer-source-readiness-1.md`, `answer-source-security-review-1.md`. The security lens ran because the change adds a machine route and maps a bearer token to a client identity.

| Lens | Counts |
|---|---|
| comprehension | B=0 M=3 N=7 |
| zoom-out | B=1 M=1 N=3 |
| critic | B=1 M=6 N=9 |
| readiness | B=1 M=8 N=4 |
| security | Critical=1 High=1 Medium=3 Low=5 |

## Isolation

- Canaries planted in the seven standing files (`.claude/CLAUDE.md`, `AGENTS.md`, `GEMINI.md`, and the four `.claude/rules/` files) before the run, removed after it (`git checkout`); `grep -rln "EGM-MARKER-[A-Za-z]" .` finds nothing. No canary appears in any report.
- Positive control: an agent with no exclusion read `.claude/rules/frontend.md` and returned its planted canary line. The probe can detect a leak.
- Limit, stated: four lenses reported that the harness attached `.claude/CLAUDE.md` (and a stale main-checkout copy of `.claude/rules/qa-test-plan.md`) unasked; none opened or used it. As in earlier records, isolation for that file is instruction-only.

## Triage

| Finding | Lens | Severity | Disposition | Note |
|---|---|---|---|---|
| The client registry gives answers to the wrong tool and mixes up item identity | zoom-out | Blocker | PROPOSE FIX | v2 §3.3: webhook only for `qa-tools` items (`DeliveryPoll` for others); id owned per asker (409 `id-taken`); origin tag per client; open cap per asker. Removal of the registry itself contested below |
| Registry clients' answers go to the QA tool's webhook and are labelled "QA tool" | critic | Blocker | PROPOSE FIX | same; option (b) partly: labels per client, no push for registry clients (N7) |
| The answer webhook sends every client's answers to the QA tool | readiness | Blocker | PROPOSE FIX | same; admin-asked items are `qa-tools` items (§3.1), so they stay pushed and readable by the QA tool |
| SEC-1 Inbox ids shared across clients and every answer goes to the QA tool | security | Critical | PROPOSE FIX | same; TC-493 gains the collision and no-push assertions; BR-57 reworded |
| SEC-2 A registry client's questions appear as the QA tool's | security | High | PROPOSE FIX | §3.3 item 1 `Label`; §3.7 labels per client; phrase keys on client name; TC-493 |
| (zoom-out recommendation) remove the registry entirely | zoom-out | Blocker | CONTEST | The brief asks for the registry explicitly (§1 quotes it). Checkable: §1. What made it unsafe is fixed per asker; §4 records why deferral loses |
| The registry is speculative surface | critic | Major | CONTEST | same; §1 now states the requirement and its source, §4 the deferral alternative |
| "Answered recently" is a new view the request did not ask for | zoom-out | Major | CONTEST | The brief asks to show the source "in the Needs-you inbox history" (§2 G4); no such list existed, so it is the requested surface. Checkable: G4 |
| "Source" already means something else | comprehension | Major | PROPOSE FIX | §0: "origin tag" (existing `Source`) vs "answer source"; payload key `answeredBy`, API key `source`, never in one object |
| Who detects the identical retry, and how, is undecided | comprehension | Major | PROPOSE FIX | §3.4 table: the handler, fields named, `display` ignored |
| Two clients share one externalId space | comprehension | Major | PROPOSE FIX | §3.3 item 1, 409 `id-taken` |
| The operator's email leaves GAOS through the API | critic | Major | PROPOSE FIX | §3.1: API returns `operator` for kind `gaos`; BR-57; TC-491 |
| SEC-3 email sent to machine clients | security | Medium | PROPOSE FIX | same |
| The first-answer contract with the QA tool is unspecified | critic | Major | PROPOSE FIX | §7: record only on 200, adopt the body on 409/410, retry on 429 |
| Count drift and missing register updates | critic | Major | PROPOSE FIX | §6: exact literals, the decision-log basis, the rule file and `check-workspace.sh` |
| The decision-log count does not follow from the page | readiness | Major | PROPOSE FIX | same: "Eighty-one entries" → "Eighty-two entries", basis stated |
| No consolidated change list | critic | Major | PROPOSE FIX | §3.9 Files |
| The change list is incomplete and names the wrong files | readiness | Major | PROPOSE FIX | §3.9; `environments.html` instead of README/STACKS; `handlers_qa.go:86` unchanged because `qaProducts` is kept |
| The apiConfig refactor is not specified; §3.8's promise is false | readiness | Major | PROPOSE FIX | §3.2 signatures; `qaProducts`/`qaBusy`/`newAPIConfig`/`readTokenFile` kept; §3.8 lists the tests that change |
| How the store's refusals map to API statuses is left open | readiness | Major | PROPOSE FIX | §3.3 `AnswerClosed` (errors.As), §3.4 text pre-check and outcome table |
| The own-items rule conflicts with the ask route | readiness | Major | PROPOSE FIX | §3.3 item 1; N4 reworded; cap per asker |
| The host of GAOS_QA_URL is ambiguous about the port | readiness | Major | PROPOSE FIX | §3.6 `Hostname()`, TC-494 inputs and outputs |
| No phases, exit conditions or verification commands | readiness | Major | PROPOSE FIX | §10 |
| qa-sync-api.html has no inbox section | readiness | Major | PROPOSE FIX | §3.9: new `#inbox` section documenting all three routes |
| SEC-4 the new scope can be implemented fail-open | security | Medium | PROPOSE FIX | §3.2 `apiProduct` rule stated without a scope equality; TC-493 negatives |
| SEC-5 the open-item cap is shared | security | Medium | PROPOSE FIX | §3.3 per (product, asker) |
| SEC-6 kind and origin can copy the QA tool's | security | Low | PROPOSE FIX | kind `qa-tool` reserved; origin equal to QA origin or BO label skipped |
| SEC-7 origin is a label, not an observed address | security | Low | PROPOSE FIX | §3.1 paragraph, BR-57, the server-log line |
| SEC-8 registry token files not permission-checked | security | Low | PROPOSE FIX | `readTokenPath` calls `warnIfOpen` for every token file |
| SEC-9 / Display stored but never read (zoom-out, comprehension, readiness, critic) | several | Low / Nit | PROPOSE FIX | accepted (the brief's body has it), validated, screened, not stored |
| SEC-10 / BR-52 sentence needs a qualifier (comprehension) | security, comprehension | Low / Nit | PROPOSE FIX | §3.10 BR-52 reworded |
| apiGate unchanged in shape contradicts handlers | comprehension | Nit | PROPOSE FIX | §3.2 new signature |
| The phrase for a missing source produces broken sentences | comprehension | Nit | PROPOSE FIX | §3.1 sentence table |
| A registry kind qa-tool displays as "via QA tool" | comprehension, readiness, critic | Nit | PROPOSE FIX | reserved; phrase keys on client name |
| Undefined deployment terms | comprehension | Nit | PROPOSE FIX | §0 QA tool entry, §3.6 |
| RecentAnswers interface plumbing not listed | comprehension | Nit | PROPOSE FIX | §3.3 item 5 |
| Answer events carry the actor twice | zoom-out | Nit | CONTEST | The `Event.actor` column is the queryable one (`store_arcade.go:1075`); `answeredBy` is the structured record. §3.1 states why |
| The GAOS_QA_URL fallback is never used in production | zoom-out | Nit | DECLINE | The brief asks for that default; it costs one function and one test |
| AnsweredBy nil rule differs between paths | critic | Nit | PROPOSE FIX | §3.1: one rule |
| 409/410 bodies must apply the lapsed adjustment | critic | Nit | PROPOSE FIX | §3.3 step 3 |
| "source" means two things | critic | Nit | PROPOSE FIX | §0 |
| L1 contradicts the existing design | critic | Nit | PROPOSE FIX | admin asks as `qa-tools`; L1 removed |
| Answers compete with sync pushes for the write slot | critic | Nit | PROPOSE FIX | §3.4 step 5, §7 |
| Event.actor mixes verified and reported identities | critic | Nit | PROPOSE FIX | §3.1, D-083 |
| Register wording stale or unregistered (BR-48, BR-57 section, OQ for §8) | critic | Nit | PROPOSE FIX / ESCALATE | BR-48 narrowed, BR-57 in §9.8; the §8 question is escalated as a follow-up rather than opened as an OQ in this change |
| GAOS_BO_LABEL as an origin is unanchored | critic | Nit | PROPOSE FIX / DECLINE | anchored (`compose.prod.yaml:130`); not pattern-checked: it is operator configuration, printed escaped, and returned to clients only as the origin of a GAOS answer |
| Event text and Display when the source is empty | readiness | Nit | PROPOSE FIX | §3.1 sentence table |
| Small anchoring and naming gaps | readiness | Nit | PROPOSE FIX | `AttemptView`, `:757`, `RecentRow` fields, §9.8, lapsed rule |
| Redeploy step versus N6 | readiness | Nit | PROPOSE FIX | N6 and §3.6: an operator step after merge |

A Blocker and a Critical were open in round 1, so round 2 runs (policy step 7).
