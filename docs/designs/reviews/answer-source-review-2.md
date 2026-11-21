# Review record 2: answer-source (design v2 → v3)

Design: `docs/designs/answer-source.md`. Round 2 ran the four lenses that had raised a Blocker or a Critical in round 1, on v2, in parallel, each given the design path, the round-1 record and the rubric path list only: `answer-source-zoomout-2.md`, `answer-source-critic-2.md`, `answer-source-readiness-2.md`, `answer-source-security-review-2.md`. Comprehension raised no Blocker in round 1 and was not re-run; its findings were all applied in v2.

| Lens | Counts |
|---|---|
| zoom-out | B=0 M=1 N=4 |
| critic | B=0 M=3 N=9 |
| readiness | B=0 M=5 N=7 |
| security | Critical=0 High=0 Medium=3 Low=6 |

No Blocker and no Critical or High: the design may be approved (policy step 7 and the security criterion). v3 applies the fixes below.

## Isolation

- Canaries planted again in the seven standing files before the round and removed after it (`git checkout`); `grep -rln "EGM-MARKER-[A-Za-z]" .` finds nothing. No canary in any report.
- The round-1 positive control stands for the same harness and the same files.
- Limit, stated: the readiness and critic lenses report that the harness attached `.claude/CLAUDE.md` and a stale main-checkout `.claude/rules/qa-test-plan.md` unasked; readiness also read one non-secret source file from the main checkout by mistake and then used the worktree copy. Isolation for those files is instruction-only.

## Triage

| Finding | Lens | Severity | Disposition | Note |
|---|---|---|---|---|
| G3's "no code change" promise is false for the clients it names; the registry got more expensive | zoom-out | Major | PROPOSE FIX | G3 now says "inbox-only client"; §11 records the operator decision (a) with the cost accepted |
| DeliveryPoll and the ExternalPending filter do the same job twice | zoom-out | Nit | PROPOSE FIX | `DeliveryPoll` dropped |
| ExternalAnswer.AsClient repeats By | zoom-out | Nit | PROPOSE FIX | `AsClient` dropped; owner check from `By.Kind`/`By.Client` |
| The conversation-sheet line can only say "in GAOS" | zoom-out | Nit | CONTEST | The brief asks for the source "on the answer sheet / card" (§11 quotes the brief's scope); §3.7 says why it is kept |
| Changing the store's return types breaks ~20 call sites; §3.8 anchors wrong | zoom-out, readiness | Nit | PROPOSE FIX | `AnswerExternal` keeps `(Result, error)`; anchors replaced; 19 |
| The logged remote address is Traefik's (critic) / SEC-2 | critic, security | Major / Medium | PROPOSE FIX | §3.1 logs `peer` and the `xff` chain as received, says what each is worth; L5; TC-490 |
| BR-52's fail-closed rule undefined with a clients file (critic) / SEC-1 | critic, security | Major / Medium | PROPOSE FIX | `outSum`; built-in collision `why` final; `addClients` clears only "no API token is configured"; TC-493 negatives |
| addClients cannot do what it promises with its inputs | readiness | Major | PROPOSE FIX | same, plus call order in `apiFromEnv`, reserved origins from the raw env, non-empty case-insensitive comparison |
| id-taken lets a registry client block the QA tool's ids; §7 silent on it | critic | Major | PROPOSE FIX | a registry client may not share a product with the QA token; L4 for registry-to-registry; §7 item 4 |
| SEC-3 the webhook carries no source yet now delivers the QA tool's own answers | security | Medium | PROPOSE FIX | BR-57 and §7: the webhook is a notification; operator's only when `source.kind` is `gaos`; take `source` from the 200 or GET |
| Empty AskedBy and Label not normalised | readiness | Major | PROPOSE FIX | §3.1 normalisation; `external_inbox_test.go:188-197,515` unchanged |
| Phase 1 cannot reach its exit | readiness, critic | Major / Nit | PROPOSE FIX | the two `handlers_questions.go` call sites move into phase 1 |
| seed.go missing from the change list | readiness, critic | Major / Nit | PROPOSE FIX | added to §3.9 and phase 2 |
| No phase owns the new tests | readiness | Major | PROPOSE FIX | §10 assigns every TC and names exact functions; "no tests to run" forbidden |
| SEC-4 an owner is a reusable client name | security | Low | PROPOSE FIX | names never reused (§3.2, D-083, BR-57, API guide) |
| SEC-5 look-alikes of GAOS or the operator | security | Low | PROPOSE FIX | reserved names `gaos`, `qa-tool`, `operator`; username `operator` or an account email refused |
| SEC-6 registry token files only warned about | security | Low | PROPOSE FIX | `readTokenPathStrict` refuses a readable file; R3 |
| SEC-7 host-side token file name not tied to the ignore pattern | security | Low | PROPOSE FIX | `deploy/stacks/prod.api-client-<name>` in §3.2 and §7b; the `check-stacks.sh` assertion is ESCALATED to the change that first wires a registry client into compose (none is wired here) |
| SEC-8 registry write slot never allocated | security | Low | PROPOSE FIX | `parseClients` allocates one per client; TC-493 |
| SEC-9 a reported username may be an email, kept forever | security | Low | PROPOSE FIX | §7: a handle, not an email; BR-57 states the retention |
| The projection never carries AnsweredBy | critic, readiness | Nit | PROPOSE FIX | §3.3 item 2 names `:113` |
| "QA tool" hard-coded where a registry item reaches | critic, readiness | Nit | PROPOSE FIX | §3.3 and §3.7 list every string with its new form |
| Small count and anchor errors (19 calls; prod label value) | critic | Nit | PROPOSE FIX | corrected; the label value is labelled as the operator's setting |
| An unset origin drops every origin-less client | critic | Nit | PROPOSE FIX | compare only non-empty origins, case-insensitive; TC-493 |
| The registers still say "events carry no actor" | critic | Nit | PROPOSE FIX | BR-48 cites D-082 and D-083; OQ-11 and OQ-34 gain a clause; D-083 says why §9.8 |
| The webhook can arrive before the 200 | critic | Nit | PROPOSE FIX | §7 item 3 |
| The conversation line repeats "answered" | critic, readiness | Nit | PROPOSE FIX | bare phrase on that surface (§3.1 table) |
| BR-52 "before" text does not exist on the page | readiness | Nit | PROPOSE FIX | §3.10 quotes the exact clause and adds D-083 to the citation |
| The new #inbox h2 has no ToC entry | readiness | Nit | PROPOSE FIX | `toc/guide.js` in §3.9 |
| Phase 3's browser check has no pass criterion | readiness | Nit | PROPOSE FIX | §10 phase 3 names what must be seen |

Approved as v3 (§11 of the design).
