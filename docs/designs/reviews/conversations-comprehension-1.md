# Review: `docs/designs/conversations.md` v1 (comprehension lens)

Process note: the harness put the contents of `.claude/CLAUDE.md` into my context without my asking, which the reviewer deny-list forbids. I did not use it for any finding and do not quote it. You should treat this run's isolation as broken and check the canary for that file.

## Paraphrase

**What this proposes**
1. A new read model, `Conversation`, for each card. It has one turn per attempt, holding the prompt and reply bodies from the `<run>-prompt` and `<run>-reply` artifacts, plus the question from `QuestionAsked` and the operator's answer from `QuestionAnswered`. `Store.Conversation` builds it from memory. ArcadeStore also reads missing prompt and reply bodies from the database, because those kinds are not loaded at boot.
2. A stdlib-only markdown renderer that escapes everything first and only emits HTML tags written in Go source. Links must be http or https. There are no images, and bodies over 512 KiB render as plain text.
3. A chip on each card on both boards, showing "{n} turn(s)". It opens a back-office sheet listing every attempt: the prompt (collapsed), the reply, the question and the answer. The sheet has an Archive button.
4. Two new event types, which opens the closed catalog: `ConversationArchived` (archive or restore, with the actor) and `ConversationPruned` (a tombstone holding counts only). Both are no-ops in the fold. Archive is refused while a turn is running, when the card is already in that state, or when it has no run.
5. A new back-office view, Archive, listing archived cards with Restore and Open, plus a "Prune all archived" form that requires typing `PRUNE`.
6. Prune has two parts. It nulls the prompt, reply and question bodies of archived cards in memory and in ArcadeDB. It also deletes the gateway sessions those cards' runs used, through a new `SessionJanitor` capability, but only for tasks that are DONE or CANCELED. It writes one tombstone event per card and keeps every event, run record, artifact metadata row, and every handoff, design and review artifact.
7. Routes, tests TC-478 to TC-488, the register and count updates, decision D-082, and release v0.42.0.

**How the existing system works, per this document**
- Every attempt is one `TaskRunRecord`, which carries its gateway session id and provider.
- After each attempt the runner stores the prompt and reply artifacts (plus handoff and question when they exist). Nothing reads the prompt, reply or question artifacts back, and after a restart only handoff, design and review artifacts are loaded into memory.
- An answer resumes the session that asked the question. A retry resumes the task's session until the session rotates.
- The gateway's admin API can delete a session (409 while busy, 404 when already gone) but cannot read a transcript. Sessions expire after 30 days.
- Back-office routes require a signed-in session, and a cross-origin non-GET request is refused. A replay handle refuses every mutation.

## Verification summary

Most anchors check out:
- `runner.go:217-249`
- `store_arcade.go:893-905` and `:1654-1667`
- `10-storage.sql:48`
- `questions.go:53-74` and `:261`
- `auth.go:349-358`, `:523-555` and `:557-575`
- `main.go:112`
- `store.go:246-300`
- `aimanager_admin.go:45` and `:76`
- `workspace.go:99-112`
- `fold.go:378-382`
- `events_test.go:17-45`
- `handlers_bo.go:141-149` and `:186`
- `backoffice.html:92`
- `app.js:54-62`
- `store_mem.go:1281`
- the data-model document at `:768` and `:1409`
- `timing.go:386`, which is safe to extend because `cardTiming` filters by event type at `timing.go:161-164`

The counts are also correct: 477 tests become 488, 353 Go tests become 364, and the decision log goes from eighty to eighty-one entries.

---

### Prune deletes records that BR-41 and J-6 say are kept forever - Severity: Blocker
- Affected scope: header "BRD rules this must obey" (BR-41); §3.7 "Deleted" 1; §3.11 D-082; N1
- Finding: The design lists BR-41 as a rule it obeys and leans on it in N1 ("those are the record"). But BR-41 (`pustaka/docs/product/brd.html:1300`) says the prompt and reply "are stored by GAOS for every attempt, and remain readable after the session expires". BRD J-6 (`brd.html:985`) keeps "everything else indefinitely, because the audit trail is the product." Prune permanently nulls exactly the prompt and reply bodies BR-41 protects. The D-082 "Rejected" list mentions BR-04 and BR-22 but never BR-41 or J-6. A reader is told the design obeys a rule that its main feature breaks.
- Recommendation: Say plainly that prune is an exception to BR-41 and J-6, which needs the operator's approval. D-082 should amend BR-41 (for example, "…until the operator prunes an archived conversation; the tombstone remains"), and the BRD row and its count should be added to the change list. Alternatively, explain why nulling the bodies is compatible with BR-41. Do not list BR-41 as a rule this design obeys without saying either.

### The pruned state hides new attempts and leaves no way to re-archive - Severity: Blocker
- Affected scope: §3.2 `ConversationState.Pruned`; §3.4 "An archived or pruned card shows no chip… A pruned one shows the tombstone line"; §3.5 `conversationStateOf`; §5 L2
- Finding: The rules contradict each other.
  - The struct comment defines `Pruned` as "a ConversationPruned exists after the last archive".
  - §3.5 says that after a later re-archive "the pruned flag stays".
  - Prune is allowed on unfinished tasks (only their sessions are kept), so a pruned card can run again. Between prune and any re-archive, `Pruned` is true, so the card shows no chip and the sheet shows only the tombstone line. The new attempts are invisible, and there is no Archive button, so L2's "they archive it again" cannot happen through the UI.
  - State is tracked per card, but whether a body was pruned is really per attempt. `PruneRecord.Runs` already lists the pruned runs, but the sheet does not use it.
- Recommendation: Define the state machine explicitly. One option: "pruned" applies only to the runs in `PruneRecord.Runs`; the chip and sheet appear again when the card has attempts newer than the last prune; those turns render normally and the pruned turns show the tombstone. Give one definition of `Pruned` and use it everywhere.

### The prune order paragraph is self-revising and its failure claim is wrong - Severity: Major
- Affected scope: §3.7 "Order"
- Finding:
  - The paragraph lists steps (a) to (d), then partly withdraws them with "so (b) runs **after** a re-check: (a′)…". The final sequence is never stated in one place, and the clause "acceptable loss only if it was still archived" cannot be parsed.
  - "Read lock", "write lock" and "the store lock" are never tied to a specific lock. There are two: MemStore's `s.mu`, and ArcadeStore's own `s.mu`, which `mutate` holds (`store_arcade.go:443-455`).
  - "A failure leaves nothing half-said" is not true. If (c) or (d) fails, for example a `persist` error in (d), the bodies are already null in the database and the sessions are already deleted, but no tombstone exists.
- Recommendation: Write the order as one numbered list: (1) mark the cards as pruning, (2) collect, (3) null the bodies, (4) delete the sessions, (5) emit the tombstones, (6) clear the marks. Name the lock held at each step. State what happens on a failure at each step, including that a failure after (3) leaves bodies gone with no tombstone, and whether a retry or a startup reconciliation writes the tombstone.

### Archive is allowed on tasks that will run again - Severity: Major
- Affected scope: §3.5 ArchiveConversation refusals; §3.7 "Deleted" 2
- Finding: Archive is refused only for `DISPATCHED`/`RUNNING`, because "the reply that turn writes would land in an archived conversation unseen". The same thing happens with a `READY`, `PENDING`, `BLOCKED`, `FAILED` or `AWAITING_INPUT` task (`taskstate.go:79-95`): it can run later and write into an archived, hidden conversation. Those bodies are also eligible for prune. The rule does not match its own reason.
- Recommendation: Do one of the following. Allow archive only for DONE or CANCELED tasks, which matches the session rule in §3.7. Or have a new attempt automatically clear the archive. Or state openly that a non-terminal card can be archived and what happens to its later attempts.

### The Archive page's "Open" link contradicts the sheet - Severity: Major
- Affected scope: §3.6 "an **Open** link to the sheet in read mode"; §3.4 "An archived conversation opened by URL shows only 'This conversation is archived…'"
- Finding: "Read mode" is not defined anywhere. §3.4 says opening an archived conversation shows only a restore notice, so the Open link on the Archive page shows no conversation. The operator cannot read an archived conversation before choosing to prune it.
- Recommendation: Define read mode, for example a `?archived=1` query that renders the turns without the Archive button. Otherwise remove the Open link and say archived conversations cannot be read until they are restored.

### Gateway behaviour rests on a document outside the repository - Severity: Major
- Affected scope: §0 Gateway; §1.3; §3.5 "a session id lets its holder continue"; §3.7 step 2; L3
- Finding: The design relies on several gateway behaviours: `DELETE /admin/sessions/{id}`, 409 while busy, 404 when gone, `?provider=antigravity`, the 30-day bucket expiry, and that a session id lets its holder continue the conversation. All of them are cited to ai-manager's `docs/claude-sessions.md` "read on `rnd-v2-vpn`" and to `internal/server/handlers_sessions.go:11`, and neither file is in this repository. There is also an open point the design does not address. `onVendor` (`aimanager_admin.go:72-75`) documents that `?provider=` makes ai-manager send a workspace call to every account in the vendor's pool. The design does not say whether a session delete with `?provider=` reaches the one account that holds the session, or what happens when accounts answer differently (some 404, some 2xx).
- Recommendation: Label these as unverified assumptions about an external system, or quote the relevant contract in an appendix with a version or commit. State how a pooled vendor's delete works and how mixed answers from the pool are counted.

### "Turn" means two things - Severity: Nit
- Affected scope: §3.2 `ConversationTurn`; §3.4 chip "{n} turn(s)"; §3.5 "A turn is running"
- Finding: "Turn" means an attempt in the data model and the chip, and a live model call in the refusal message and §3.7. `Talk`/`BoardTalk` and "conversation" are also used for the same thing.
- Recommendation: Pick one word for an attempt's record, preferably "attempt", and keep "turn" for the live call. Rename `Talk` to `Conversation`, or explain why it differs.

### Some terms are not defined in this document - Severity: Nit
- Affected scope: §3.2 "the projection's answer"; §3.4 "the sisflow card"; §3.7 `SessionLost`
- Finding: "Projection" is defined only in `card-timing.md` §0, which this glossary cites only for card and attempt. "Sisflow" is never explained; it is the vendored board (`board.go:20`).
- Recommendation: Add both to §0 with an anchor each.

### Some line anchors have drifted - Severity: Nit
- Affected scope: §0 and §3.1, §3.5
- Finding:
  - `TaskRunRecord` is at `model.go:766-802`, not 740-797.
  - `SessionID` and `Provider` are at `model.go:795-798`, not 785-794.
  - `EventPayload` is at `model.go:215`, not 211.
  - `sessionForLocked` is defined at `turns.go:102`. `runner.go:595` is one of its call sites.
  - `renderBOAfter` is defined at `handlers_bo.go:494`. `handlers_board.go:78` is a call site.
  - `readOnlyGET` is a test map in `auth_test.go:258`; the design does not say which file it is in.
- Recommendation: Update the anchors.

### The JS opener change leaves out the click selector - Severity: Nit
- Affected scope: §3.4 "JS"
- Finding: The design says the delegate branch reads `t.dataset.timingOpen || t.dataset.sheetOpen`. The click handler only reaches that branch for elements matched by the `closest(...)` selector at `app.js:40`, and `[data-sheet-open]` is not in it. `openTiming` (`app.js:175`) also reads `t.dataset.timingOpen` directly.
- Recommendation: List the selector change and the `openTiming` URL source as part of the JS change.

### The escaping function differs from the one the frontend rule names - Severity: Nit
- Affected scope: §3.3
- Finding: `.claude/rules/frontend.md` asks for an explicit `template.HTMLEscapeString` where a `template.HTML` value is built. The design uses `html.EscapeString` instead.
- Recommendation: Use `template.HTMLEscapeString`, or explain in one sentence why `html.EscapeString` meets the rule.

### The decision-log count in the ToC is missing from the change list - Severity: Nit
- Affected scope: §4 rows 15 and 16
- Finding: `qa-test-plan.md` says the decision-log count "Eighty entries" is mirrored in `pustaka/docs/assets/toc/product.js`. Row 16 lists only `decision-log.html`.
- Recommendation: Add the `product.js` decision-log count to row 16.

### L5 is vague - Severity: Nit
- Affected scope: §5 L5
- Finding: "The in-memory dev store has none when unauthenticated paths are used in tests" does not say what `Actor` holds in that case: an empty string or a placeholder.
- Recommendation: State the value.

Sections that are solid: §3.3, the markdown renderer's escaping rules, is clear and complete. §3.8 routes and guards matches `gate`/`crossOriginOK` (`auth.go:523-577`). §3.10 and the count arithmetic are consistent with the register.

Counts: B=2 M=4 N=7
