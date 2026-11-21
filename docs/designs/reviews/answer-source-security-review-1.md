# Security review: `docs/designs/answer-source.md` v1 (D-083, BR-57)

The design has one Critical and one High finding, so under `docs/rules/gaos-design-review.md:97` it cannot go to "Approved" until both are resolved.

- **Critical (SEC-1):** inbox item ids are shared by every client in a product, and every answer is pushed to the QA tool. Once a second client exists, it can plant and approve an item under an id the QA tool uses, and that approval is sent to the QA tool as its own.
- **High (SEC-2):** a registry client's questions appear to the operator labelled "QA tool". The design records who *answered* an item, but never who *asked* it.

## Trust boundaries touched

| Boundary | Authn | Authz | Validation | Far side fails |
|---|---|---|---|---|
| QA tool or registry client → edge (`/api`, stripped) → dashboard | bearer token in a file, hashed, constant-time compare | scope ladder, product list, creator rule (new) | strict decode up to 1 MiB, secret screen, actor alphabet | 503 / 401 / 403 / 404 / 429, fail closed |
| Dashboard → QA tool webhook | outbound QA bearer | **none per client.** One destination for every item (SEC-1) | none | retry, then give up; GET is the fallback |
| Operator browser → back office | verified session (BR-55) plus the cross-origin check | any signed-in account | the store's answer checks | — |
| Dashboard → ArcadeDB | the database user | — | `Event.actor` now holds `<client>/<username>` or an email | unchanged |
| Repository → host | file permissions | — | `warnIfOpen` on the clients file only (SEC-8) | a bad line is skipped |

Abuse cases checked: replay (first-wins and the idempotent retry), forged request (needs a token), oversized payload (1 MiB cap, 30-second body deadline), path traversal (`apiInboxID` regex, `api_inbox.go:33`), injection through `actor` (alphabet and template escaping), IDOR (creator rule, with holes: SEC-1), secret in a log (no token in the registry log line), scope-ladder escalation (SEC-4).

---

### SEC-1 Inbox ids are shared across clients and every answer goes to the QA tool, so one client can forge, hijack or receive another's decisions - Severity: Critical
- Component: the store's item identity, the ask route, the answer webhook worker
- Evidence: `dashboard/external_inbox.go:88` (the key is product + externalId, with no asker); `dashboard/external_inbox.go:171-172`; `dashboard/api_inbox.go:170-179`; `dashboard/inbox_worker.go:122` and `:161-164`; `docs/designs/answer-source.md:173`, `:215`, `:288`, `:295`.
- Attack path: a holder of a registry client's token sharing a product with the QA tool can (1) forge an approval by asking under an id in the QA tool's id space and answering it, the worker delivering it to the QA webhook; (2) block a QA approval by claiming an id first; (3) learn whether another client's item exists and its state through the ask route's 200; (4) receive, in the other direction, the QA tool gets the operator's answers to a registry client's items.
- Remediation: (a) make an item's identity (product, asker, externalId), or refuse an ask whose id exists under another asker; (b) the worker delivers only items asked by `qa-tools`; registry clients get no push until the registry carries a per-client webhook URL and token; (c) change BR-57's "delivered to the asking tool"; (d) tests: a cross-client id collision on ask, and a registry client's answered item is not delivered to `GAOS_QA_URL`.
- Effort: small

### SEC-2 A registry client's questions appear to the operator as the QA tool's - Severity: High
- Component: the ask path and the back-office inbox and answer sheet
- Evidence: `dashboard/external_inbox.go:28`, `:193`, `:196`; `dashboard/handlers_bo.go:726-731`; `docs/designs/answer-source.md:149`, `:243-249`.
- Attack path: a less-trusted client posts an approval request; it appears in "Needs you" tagged **QA tool**; the operator approves on the trust they give the QA tool.
- Remediation: set `Source` and the asked-event text from the asking client; "QA tool" only for `qa-tools`. Show the asker on the inbox row and the answer sheet. Test that a registry client's item is never labelled "QA tool".
- Effort: quick-win

### SEC-3 The operator's back-office email is sent to machine clients - Severity: Medium
- Component: the GET body's `source`, and the 409 reason sentence
- Evidence: `docs/designs/answer-source.md:77`, `:197`, `:213`, `:220`, `:94`; `pustaka/docs/product/brd.html:1348`.
- Attack path: any client token holder, or any user of the QA tool's `/history` page that shows the 409 reason or `source`, learns the operator account's login identifier for a public sign-in form — half a credential pair, and personal data held by an outside system.
- Remediation: on the machine API, return the `gaos` kind's actor as a non-identifying value (`operator`). Keep the email only in the event and the back office. Write that rule into BR-57 and assert the API body never contains the session email.
- Effort: quick-win

### SEC-4 The new scope can be implemented fail-open, and no negative test covers registry-client product scoping - Severity: Medium
- Component: `apiProduct`, the scope ladder, TC-493
- Evidence: `dashboard/api_v1.go:206` and `:300` (product filtering keyed on `scope == scopeQA`); `docs/designs/answer-source.md:112`, `:121`, `:137`, `:261`.
- Attack path: if `scopeClient` is added but an equality check like line 206 is left, a registry client passes `apiProduct` for every product.
- Remediation: state the product check as "admin, or `client != nil && client.products[id]`; anything else is 404". Extend TC-493: 404 on an unlisted product, `-` reaches nothing, 403 on `GET /v1/products`, `/qa/*` and `/v1/seed/product`.
- Effort: quick-win

### SEC-5 The per-product open-item cap is shared, so one client can lock out another - Severity: Medium
- Component: `AskExternal`
- Evidence: `dashboard/external_inbox.go:32`, `:175-183`; `docs/designs/answer-source.md:47`.
- Attack path: a registry client fills a shared product with 200 open items; the QA tool's asks are refused.
- Remediation: count the cap per (product, asker), and record that in BR-57.
- Effort: quick-win

### SEC-6 A registry client's kind and origin can copy the QA tool's - Severity: Low
- Component: registry parsing and the source phrase
- Evidence: `docs/designs/answer-source.md:133-134`, `:87`.
- Attack path: a careless registry line with kind `qa-tool` and origin `qa-tool.sample-product.com` produces answers that read exactly like the QA tool's. Needs host access.
- Remediation: reserve kind `qa-tool`. Reject an origin equal to the resolved QA origin or to `GAOS_BO_LABEL`.
- Effort: quick-win

### SEC-7 "Origin" is a configured label, not where the request came from - Severity: Low
- Component: the source phrase and the R1 attribution claim
- Evidence: `docs/designs/answer-source.md:78`, `:90`, `:327`.
- Attack path: an answer made with a leaked QA token from anywhere is recorded as "via QA tool (qa-tool.sample-product.com)".
- Remediation: say in BR-57 and the API guide that `origin` is the client's registered host, not an observed one. Optionally log the request's peer address in the server log when the route writes an answer.
- Effort: quick-win

### SEC-8 Registry token files are not permission-checked - Severity: Low
- Component: `apiFromEnv` and the registry loader
- Evidence: `docs/designs/answer-source.md:139`, `:329`; `dashboard/auth.go:249-253`; `dashboard/api_v1.go:77-96`.
- Attack path: a group- or world-readable token file gives any local user a working client token.
- Remediation: call `warnIfOpen` on every token file the loader reads, built-in ones included.
- Effort: quick-win

### SEC-9 `actor.display` is stored indefinitely but nothing reads it - Severity: Low
- Component: `AnswerSource.Display`
- Evidence: `docs/designs/answer-source.md:61`, `:188`, `:213`.
- Attack path: personal data with no consumer, kept in an append-only stream.
- Remediation: drop `display`, or name its reader and add it to the BR-57 retention wording.
- Effort: quick-win

### SEC-10 BR-52's "equal tokens disable the API" contradicts the new registry rule - Severity: Low
- Component: the registers
- Evidence: `pustaka/docs/product/brd.html:1345`; `docs/designs/answer-source.md:141`, `:289`.
- Attack path: none directly; two conflicting fail-closed statements.
- Remediation: reword BR-52's clause to "equal admin, QA or outbound tokens disable the API; a registry client whose token equals any principal's is not loaded".
- Effort: quick-win

Counts: Critical=1 High=1 Medium=3 Low=5
