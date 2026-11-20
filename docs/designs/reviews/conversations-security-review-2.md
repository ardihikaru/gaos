# Security review: `conversations`, design v2 (round 2)

**Isolation note for the author.** The harness injected the full text of `.claude/CLAUDE.md` into this session as a system reminder, along with three rubric files. I never opened `CLAUDE.md`. I did not use its content as evidence and I do not quote it here. Isolation in this run was instructional only, so check your canary results against it.

**The identity premise is out of date.** My brief says `signedIn()` accepts a cookie from any request. It does not. `signedIn` (`dashboard/auth.go:361-382`) verifies the cookie's HMAC, its expiry and the account fingerprint (`auth.go:315-345`). The `gate` binds the verified session to every back-office route (`auth.go:523-555`). So "the certificate names the account" can be implemented.

## What this proposes

v0.42.0 adds a conversation sheet to each card. For every attempt it shows the prompt GAOS sent and the reply, rendered by a new stdlib-only markdown renderer that escapes first. It also shows any question the agent asked and the operator's answer. Two new events are added to the closed catalog (BR-05):

- `ConversationArchived` lets a finished (DONE or CANCELED) card's conversation be archived or restored.
- `ConversationPruned` records a prune.

A back-office Archive page lists archived cards. It also has **Prune all archived**, which needs a typed `PRUNE` and is bound to the set of cards the page listed. Prune:

1. deletes the gateway session of each card (`DELETE /admin/sessions/{id}` with the over-broad admin key, OQ-29), outside any lock;
2. then, in one transaction, empties the `prompt`, `reply` and `question` artifact bodies and emits one `ConversationPruned` certificate per card. The certificate holds the actor, the runs, each body's SHA-256 and length, the session counts and a fixed "kept" list.

Events, run records, context slices, artifact rows, and the handoff, design and review artifacts all survive. BR-41 is amended, and D-012 and J-6 gain an exception.

## How the existing system works, per this document

- After every attempt the runner stores the prompt and reply artifacts, plus the question artifact when the turn asked one. Nothing reads them, and after a restart only the handoff, design and review artifacts are loaded into memory.
- A card's attempts share one gateway session per task, until the session rotates.
- Back-office routes are session-gated. A non-GET request that is cross-site or has a foreign `Origin` is refused, and the cookie is `SameSite=Lax`. In replay mode, `s.mut` refuses every mutation.
- The gateway admin API can delete a session. It answers 409 while a turn is in flight and 404 when the session is gone, and it has no endpoint that reads a transcript.

## Trust boundaries touched

| Boundary | Authenticates | Authorises | Validates | If the far side fails |
|---|---|---|---|---|
| Operator browser to dashboard (sheet, archive, restore, prune) | HMAC session cookie (`auth.go:315-382`) | Any signed-in account (L5) | `crossOriginOK` (`auth.go:560-577`), `SameSite=Lax` (`auth.go:493`), typed `PRUNE`, bound card set, re-check under the lock | Toast via `s.fail` (`main.go:499-509`) |
| Dashboard to ai-manager admin (new DELETE) | Admin bearer (`aimanager_admin.go:62`) | The same key, which can delete *any* caller's session (`open-questions.html:733-738`) | Status mapping only; no check on the id's format | Counted as failed; prune continues |
| Dashboard to ArcadeDB (first in-place `UPDATE Artifact`) | Database user | Unverifiable from the repository | Bound `:t` and `:id` | One transaction |
| Model output to operator browser | — | — | `renderMarkdown` invariant (TC-489) | No CSP or anti-framing behind it |

I also checked these, and found no new exposure:

- The public routes (`auth.go:502-510`) and the bearer API (`api_v1.go:271-279`) do not return the new payloads.
- Task ids are qualified per workflow (`store_mem.go:2083-2091`), so a `CardRef` cannot collide across products.
- `GET /events` and `GET /tasks/{id}/context` stay session-gated (`main.go:347,354`).

## Findings

### SEC-1 Prune promises a deletion it does not perform: most of the conversation's content survives in GAOS and in the gateway - Severity: Medium
- Component: §3.7 "Deleted / Survives", the pruned banner in §3.4, L1, D-082
- Evidence:
  - The prompt is `RenderPrompt(c)` built from the cleared packet's parts (`dashboard/adapter_aimanager.go:112-121,159,188`). The kept context slice stores every one of those part bodies (`dashboard/store_mem.go:1093-1109`) and is served by `GET /tasks/{id}/context` (`dashboard/main.go:354`).
  - `QuestionAsked` and `QuestionAnswered` carry the full question, context, options and the operator's full answer text (`dashboard/questions.go:53-64,204-207`; `dashboard/runner.go:321-323`). The design calls this "the answer's summary" (`docs/designs/conversations.md:198,297`).
  - On the gateway side, ai-manager's temp bucket keeps the question record for 30 days (`dashboard/questions.go:25-31`; `dashboard/aimanager_questions.go:12-17`). Turn results stay addressable by request id (`dashboard/turns.go:80-83`). `DELETE /admin/sessions/{id}` is not shown to remove either.
  - The UI still says "The prompt and reply of this attempt were deleted" (`docs/designs/conversations.md:140`).
- Attack path: a model can open workspace files beyond what egress sends (OQ-24), so a reply can echo something sensitive. An operator prunes to remove it and gets a certificate saying the bodies are gone. The content stays readable to every back-office account through the context route, the event stream and any handoff, design or review artifact that repeats it. It also stays in the gateway's stores. L-8 requires that what deliberately survived is disclosed, not discovered (`pustaka/docs/product/brd.html:1186`). OQ-17's own rule is that a deletion promise resting on an unverified behaviour is not a promise (`pustaka/docs/product/open-questions.html:527-530`).
- Remediation:
  - Reword the banner, the confirm form and D-082 to "deletes the stored prompt, reply and question documents". State in the same place that the packet content survives in the context slice, and that the full question and answer survive in the event stream.
  - Correct L1 from "summary" to "the full answer".
  - Add the gateway's question-bucket and request-result copies to `PruneRecord.Kept` and to the external contract in §0, with their retention.
  - If the product owner's intent was "the text is gone", escalate a decision on also clearing `QuestionPayload` text and the slice instead.
- Effort: small

### SEC-2 Model-controlled content and a bulk destructive action share a back office with no CSP and no anti-framing header - Severity: Medium
- Component: back-office response headers, N5 and R5
- Evidence:
  - `docs/designs/conversations.md:49,327` says "escalated" but names no OQ row or owner.
  - No `Content-Security-Policy`, `frame-ancestors` or `X-Frame-Options` exists anywhere in `dashboard/` or `deploy/nginx/`. The only nginx headers are Cache-Control (`deploy/nginx/landing.conf:22-34`).
  - The repository already ships a strict policy for the landing page (`landing-page/index.html:10`).
- Attack path: content steered by repository or knowledge text through a model reply is now rendered as HTML inside a back-office session that can prune every archived conversation. Today the renderer and its invariant test (TC-489) are the only layer. One regression in `markdown.go`, or in any later `template.HTML` builder (`.claude/rules/frontend.md:58-62`), runs with full operator authority. There is also no framing control. `SameSite=Lax` blunts cross-site framing, but a same-site sibling origin is not blunted.
- Remediation:
  - Ship the parts that need no inline-script audit in this change: `frame-ancestors 'none'` (or `X-Frame-Options: DENY`), `object-src 'none'`, `base-uri 'none'` and `form-action 'self'` on back-office responses.
  - Record the remaining `script-src` work as a named OQ row in the same commit, so "escalated" points at something.
- Effort: small

### SEC-3 Sessions are deleted before the re-check, so a skipped or failed commit leaves an uncertified destructive call - Severity: Medium
- Component: §3.7 order, steps 1 to 3
- Evidence:
  - `docs/designs/conversations.md:209-213`: step 2 deletes sessions with no lock held. Step 3 then re-checks and *skips* a card that was restored or pruned meanwhile, and the design says step 3's failure leaves "no certificate".
  - L-8 requires that a deletion produces a certificate (`pustaka/docs/product/brd.html:1186`).
  - N1 says the gateway transcript, tool calls included, has no other copy (`docs/designs/conversations.md:45`).
- Attack path: operator A prunes while operator B restores one of the listed cards, or the transaction fails. The full raw transcript of that card is destroyed with the over-broad admin key, and no event records it. Its sheet shows an active or archived conversation whose session is silently gone. I rated this Medium rather than Low because it is an irreversible outbound deletion with no audit record. The 30-day expiry limits the impact but does not make the deletion recorded.
- Remediation (either one):
  - Mark the selected cards "pruning" in memory at step 1, and have `ArchiveConversation` refuse a restore of a card in that set until step 3 ends. Then also emit a `ConversationPruned` (session counts only, zero bodies) for any card whose sessions were touched but whose commit was skipped.
  - Commit the tombstone first and delete the sessions after it.

  Add the restore-in-between case to TC-484 with the expected event.
- Effort: small

### SEC-4 The session id is not validated before a destructive admin call made with an over-broad key - Severity: Low
- Component: `AIMClient.DeleteSession` (§3.7)
- Evidence:
  - Session ids come from gateway replies and are persisted in `TaskRun` (`dashboard/adapter_aimanager.go:289`; `dashboard/store_arcade.go:1715`).
  - The design escapes them with `url.PathEscape` only (`docs/designs/conversations.md:223`), which leaves `.` and `..` segments intact.
  - The key can delete any caller's sessions (`pustaka/docs/product/open-questions.html:733-738`).
  - `admin()` writes the path into its error text (`dashboard/aimanager_admin.go:47,59,65`).
- Attack path: a malformed or tampered run record, for example through a database write or a gateway bug, makes GAOS send a DELETE with the admin bearer to a path other than one named session. A wrapped `admin()` error would also carry the id into a toast.
- Remediation: before calling, refuse an empty id, `.`, `..` and any id that does not match the gateway's id format, and count it as failed. State that `DeleteSession` maps `admin()` errors to the three sentinels and never wraps them. TC-487 already asserts no id in the error. Add the malformed-id case to it.
- Effort: quick-win

### SEC-5 The actor value must reach `Event.actor` as a bound parameter - Severity: Low
- Component: `writeEvent` change (§3.5)
- Evidence: today the column is the SQL literal `'operator'` inside the statement text (`dashboard/store_arcade.go:1066-1069`). The design says to write `Payload.Actor` "when set, else 'operator'" (`docs/designs/conversations.md:155`) without saying how.
- Attack path: the natural edit is to replace the literal with a formatted string. The value is a login email, canonicalised at sign-in (`auth.go:400`), so this is hardening and not a live path. It is still a free-text value going into SQL.
- Remediation: specify `actor = :actor` with `"actor": def(e.Payload.Actor, "operator")` in the bound map, and add the assertion to TC-488.
- Effort: quick-win

### SEC-6 How link hosts are displayed is underspecified - Severity: Low
- Component: `renderMarkdown` link rule
- Evidence: `docs/designs/conversations.md:109`. Only `href` is said to be escaped. The `(host)` span's escaping, IDN form and bidi handling are not stated, and the case of a link inside link text is not covered.
- Attack path: a model-written link uses a look-alike Unicode host, or a right-to-left override, so that it displays as the operator's own back-office host. The host label then reinforces the deception instead of exposing it.
- Remediation: render `u.Hostname()` converted to ASCII (punycode) and passed through `HTMLEscapeString`. Strip bidi and zero-width control characters from link text and host. Render `[` inside link text literally. Add these cases to TC-479.
- Effort: quick-win

### SEC-7 The rule "a terminal card never runs again" is enforced at the board, not at the store - Severity: Low
- Component: §3.1 and the archive precondition
- Evidence:
  - The transition table allows DISPATCHED and RUNNING to move to CANCELED (`dashboard/taskstate.go:82-83`). Only the board's `legalMove` refuses cancelling a live turn (`dashboard/board.go:185-186`).
  - `finishRun` writes the prompt and reply artifacts whatever the task's status (`dashboard/runner.go:220-228`), and boot recovery adopts in-flight turns (`dashboard/turns.go:171-178`).
- Attack path: any present or future path that cancels a card mid-turn lets that turn's bodies land after a prune. They would be stored indefinitely, hidden by the pruned sheet's header-only mode, and contradicted by a certificate saying the card is pruned.
- Remediation: `ArchiveConversation` also refuses when any run of the card is not closed (STARTED or RUNNING). Add that case to TC-482.
- Effort: quick-win

### SEC-8 OQ-17's status in the design disagrees with the register - Severity: Low
- Component: §3.7 "Storage, honestly", §8, D-082
- Evidence:
  - The design says OQ-17 "stays open" and calls `EXTERNAL` reclamation unverified (`docs/designs/conversations.md:205,319`).
  - The register records OQ-17 as Answered by D-043, and says that `EXTERNAL` payload destroyability is verified in the scratch pass (`pustaka/docs/product/open-questions.html:531-541`).
  - `dashboard/schema/RECONCILIATION.md:241` records only that `EXTERNAL` was applied, not that destroyability was tested.
- Attack path: no attack. The deletion guarantee rests on a register claim that the design and the reconciliation record contradict, so an auditor cannot tell which one is true.
- Remediation: D-082 should either cite the D-043 verification result, or state that D-043's destroyability check was never recorded and reopen or narrow OQ-17 in the same commit.
- Effort: quick-win

Counts: Critical=0 High=0 Medium=3 Low=5
