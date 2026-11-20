# Security review: `conversations` (design v1, draft)

**Isolation note for the author.** In this session the harness injected the contents of `.claude/CLAUDE.md` and `.claude/rules/agent-workspace.md` as system reminders, even though I never opened them. I did not use the CLAUDE.md content as evidence, and I do not quote it here. Check your canary results against this run, because isolation was instructional only.

## What this proposes

Version 0.42.0 adds a conversation sheet to each card. For every attempt it shows the prompt GAOS sent, the reply the model gave, and any question asked with the operator's answer. Prompts and replies are rendered as markdown by a new stdlib-only renderer that escapes the text first. An operator can archive or restore a card's conversation, using a new `ConversationArchived` event. A new back-office Archive page lists archived conversations. **Prune all archived** sits behind a typed `PRUNE` confirmation. It nulls the bodies of the `prompt`, `reply` and `question` artifacts of every archived card. It then calls the gateway's admin `DELETE /admin/sessions/{id}` for each session of a finished task. Finally it writes a `ConversationPruned` tombstone per card, holding counts only and no session ids. Events, run records, artifact metadata, handoffs, designs and reviews are kept. Four new back-office routes are added, all session-gated and same-origin. Two event names are added to the closed catalog.

## How the existing system works, per this document

- After every attempt the runner writes prompt, reply, handoff and question artifacts, but nothing reads them, and after a restart they are not loaded.
- Questions and answers also live in the `QuestionAsked` and `QuestionAnswered` events.
- Sessions are per task and are resumed across retries and answers.
- The gateway's admin API, called with the admin bearer key, can delete a session. It returns 409 while a turn is running and 404 when the session is gone. It has no endpoint that reads a transcript.
- Back-office routes go through `gate`: a session cookie (HMAC-verified at `dashboard/auth.go:317-345`) plus `crossOriginOK` for any non-GET request.
- Replay mode refuses every mutation.

## Trust boundaries touched

| Boundary | Authenticates | Authorises | Validates | If the far side fails |
|---|---|---|---|---|
| Browser to dashboard (sheet, archive, restore, prune) | HMAC session cookie (`auth.go:361-382`) | Any signed-in account; no roles | `crossOriginOK` (`auth.go:560-577`); typed `PRUNE`; product/task match | Toast via `s.fail`, which prints `err.Error()` verbatim (`main.go:499-501`) |
| Dashboard to ai-manager admin (new DELETE) | Admin bearer key (`aimanager_admin.go:62`) | Same key | Status-code mapping | Counted as failed; prune continues |
| Dashboard to ArcadeDB (first in-place `UPDATE Artifact`) | DB user | Unverified: the repo has no grant model | Bound `tenantId` and `taskId` | One transaction; error returned |
| Model output to operator's browser | — | — | `renderMarkdown` | — |

## Findings

### SEC-1 Prune destroys the per-attempt record that BR-41 and D-012 say is kept, while the design says it obeys BR-41 - Severity: High
- Component: Prune, governance of the audit trail
- Evidence: `docs/designs/conversations.md:7` (lists BR-41 as obeyed), `:161`, `:229`. Against them: `pustaka/docs/product/brd.html:1300` (BR-41: the prompt and reply "remain readable after the session expires") and `pustaka/docs/product/decision-log.html:116` (D-012: everything except events, samples and logs is kept indefinitely, "because the audit trail is the product").
- Attack path: any signed-in back-office account can permanently erase every archived card's prompt bodies. That erases the only stored evidence of what was sent to a model provider after the egress check (BR-32, `brd.html:1284`). Someone covering up a bad or leaking dispatch only has to archive the card and prune it. The tombstone records that a deletion happened, but not what was deleted. D-082 neither amends BR-41 nor D-012. I rated this High rather than Medium because the deletion cannot be undone and it removes audit evidence.
- Remediation: make D-082 explicitly amend BR-41 and D-012, and have the operator approve the amendment. Keep a SHA-256 digest and byte length of each nulled body, either on the artifact metadata row or in `PruneRecord`, so that a later audit can still check what was sent. List BR-32 and D-012 in the "must obey" line of the design.
- Effort: small

### SEC-2 "Prune all" deletes whatever is archived when the POST arrives, not what the operator saw, and any account can do it - Severity: Medium
- Component: `POST /conversations/prune`, §3.6 and §3.7
- Evidence: `docs/designs/conversations.md:154`, `:158-160`, `:171`. Several accounts are supported (`dashboard/auth.go:340`). The archive and prune state is global, across products (`:149-152`).
- Attack path: operator A opens the Archive page. Meanwhile operator B archives another product's card. A's prune then deletes B's card too, which A never saw (a time-of-check/time-of-use gap). `PRUNE` is a fixed, non-secret string, so it guards against a mis-click but gives no authorisation. There is no fresh authentication before an irreversible bulk delete.
- Remediation: have the form post the exact set of task ids it displayed, or a digest of that set. Prune only cards that are both in that set and still archived under the lock; refuse with a re-render if the set changed. Consider asking for the password again for prune, and record the decision about cross-operator scope in D-082.
- Effort: small

### SEC-3 A gateway error carries the session id and can reach the toast or a log - Severity: Medium
- Component: `SessionJanitor` and `AIMClient.DeleteSession`, error path
- Evidence: `dashboard/aimanager_admin.go:47`, `:59` and `:65` put the request `path` into the error text. For the new call the path is `/admin/sessions/{id}` (`docs/designs/conversations.md:181`). `dashboard/main.go:501` toasts `err.Error()` unchanged. The design forbids session ids only in events (`:133`).
- Attack path: if a gateway failure is wrapped into the error returned by `PruneConversations` or by a handler, or is logged, the id of a kept session reaches the browser or the logs. Kept sessions belong to unfinished tasks. Per the design, whoever holds a session id can continue that conversation (`:133`).
- Remediation: `DeleteSession` returns sentinel errors only (`ErrTurnRunning`, `ErrGatewayTransient`, no-key), with the path stripped. `PruneConversations` never returns or logs per-session errors, only counts. Extend TC-487 and TC-484 to assert that no session id appears in any returned error string or toast.
- Effort: quick-win

### SEC-4 The only XSS defence is a hand-rolled parser that emits `template.HTML`, with no well-formedness invariant and no CSP - Severity: Medium
- Component: `dashboard/markdown.go`, `conversation-modal`
- Evidence: `docs/designs/conversations.md:92-107`, `:272`. The dashboard sets no Content-Security-Policy header: grep finds only Cache-Control headers in `dashboard/*.go`. TC-479 (`:211`) tests a fixed list of inputs.
- Attack path: replies are written by the model and can be steered by anything the model read, such as repository content or knowledge sources. A parser bug in emphasis, list or fence state could emit a tag without its pair, or a closing tag it never opened. The text stays escaped, but the page structure breaks around the modal and `<details>` elements. Any future slip in escaping would run with the operator's full back-office session, which can trigger prune. No second layer of defence exists.
- Remediation: state and test the invariant that the output contains only allowlisted tags, every closing tag matches an opened one, and every `<` outside a literal tag is escaped. Add a stdlib `FuzzRenderMarkdown` test for it. Add a restrictive CSP to back-office responses (`script-src 'self'`, `img-src 'self'`, `object-src 'none'`, `base-uri 'none'`), after checking that the templates have no inline scripts.
- Effort: small

### SEC-5 The prune order can delete data without leaving a tombstone - Severity: Medium
- Component: `PruneConversations` ordering
- Evidence: `docs/designs/conversations.md:171`. Steps (b) null the bodies and (c) delete the sessions before (d) emits `ConversationPruned`. The `pruning` mark is in memory only. The projection upserts bodies from memory based on a fingerprint (`dashboard/store_arcade.go:893-905`).
- Attack path: a crash or restart after (b) or (c) leaves bodies nulled and sessions deleted, but no tombstone. The card still shows as archived, and its sheet is empty for no recorded reason. This is the same audit gap as SEC-1, reached by accident. It contradicts the design's own "a failure leaves nothing half-said".
- Remediation: in ArcadeStore, commit the body `UPDATE` and the `ConversationPruned` event (bodies count, run ids) in one transaction. Then delete the sessions and record their outcome counts afterwards, as a second `ConversationPruned` carrying only the session counts. Do not delete a session before its card's tombstone is committed. Add a crash-between-steps case to TC-488.
- Effort: small

### SEC-6 The sheet returns full prompt packets without `Cache-Control: no-store` - Severity: Low
- Component: `GET /ui/conversation/{id}/{taskId}`
- Evidence: `docs/designs/conversations.md:113-116`. The existing pattern is at `dashboard/main.go:443` and `dashboard/api_v1.go:164`.
- Attack path: prompts carry Blueprint, BRD and knowledge content. A shared or proxied browser cache can keep them after sign-out.
- Remediation: set `Cache-Control: no-store` on the sheet and on the Archive view.
- Effort: quick-win

### SEC-7 Model-written links open any http(s) host from the operator console - Severity: Low
- Component: `renderMarkdown` link rule
- Evidence: `docs/designs/conversations.md:102`
- Attack path: a reply can place a convincing link, for example to a look-alike back-office sign-in page, inside a trusted console. Plain `http` is allowed too.
- Remediation: show the target host next to the link text, or render the URL as text and not as a link. Consider allowing `https` only.
- Effort: quick-win

### SEC-8 The CSRF defence relies on SameSite=Lax for requests that carry no fetch metadata - Severity: Low
- Component: archive, restore and prune POSTs
- Evidence: `dashboard/auth.go:557-577` lets a request through when it has neither `Sec-Fetch-Site` nor `Origin`. The cookie is `SameSite=Lax` (`auth.go:493`). The design's R2 (`docs/designs/conversations.md:273`) names only `crossOriginOK` and the typed confirm.
- Attack path: from an older or non-conforming client, the destructive POST rests entirely on the cookie's SameSite attribute, and the typed confirm can be forged.
- Remediation: name SameSite=Lax in R2 as a required control. Have TC-486 also assert that `Sec-Fetch-Site: cross-site` and a foreign `Origin` are refused on prune.
- Effort: quick-win

### SEC-9 The design contradicts itself on whether an archived conversation can be read - Severity: Low
- Component: §3.4 and §3.6
- Evidence: `docs/designs/conversations.md:120` (archived sheet shows only a notice) against `:154` (an **Open** link "in read mode").
- Attack path: none directly. Archive is not an access control, but implementers may build different behaviour, and a reader may assume archive hides content.
- Remediation: choose one behaviour and state in §3.5 that archive changes visibility, not access.
- Effort: quick-win

### SEC-10 The gateway DELETE behaviour cannot be verified from this repository - Severity: Low
- Component: §0 and §3.7, the gateway contract
- Evidence: `docs/designs/conversations.md:15` cites ai-manager docs "read on `rnd-v2-vpn`". `dashboard/aimanager_admin.go:72-75` says an antigravity call fans out to every account in the pool.
- Attack path: if DELETE fans out across a pool, or "404" means "wrong account" and not "gone", counting 404 as deleted could leave transcripts in place while the tombstone says they are gone.
- Remediation: pin the gateway contract in TC-487 with a fixture for the pool case, and record the gateway version the design was checked against.
- Effort: small

Counts: Critical=0 High=1 Medium=4 Low=5
