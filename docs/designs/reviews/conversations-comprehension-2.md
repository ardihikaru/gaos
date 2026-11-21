# Goldfish comprehension review: `docs/designs/conversations.md` (v2)

**Isolation note.** The harness put the contents of `.claude/CLAUDE.md` into my context; I did not open that file. I did not use it for any finding. It also put the main checkout's copies of `qa-test-plan.md` and `frontend.md` into my context. Those are outside this worktree, and the `qa-test-plan.md` copy is older (it says TC-465). I judged only against the worktree copies, which say TC-477 and 353 Go tests.

## Paraphrase

### What this proposes
1. **A conversation sheet for each card.** It shows every attempt in order: the prompt GAOS sent (collapsed), the AI's reply, the question the AI asked, and the operator's answer. It is built by a pure function, `conversationOf`, from the task's run records, the prompt/reply artifacts, and the question events. After a restart, missing prompt and reply bodies are read from ArcadeDB one id at a time.
2. **A markdown renderer** using only the standard library. It escapes all text first, then writes only allowlisted tags, accepts https links only, runs in linear time, and renders a body over 512 KiB as one `<pre>`. One fixed example and a 2,000-input fuzz test check it.
3. **A chip on each board card** (`n attempt(s)`) that opens the sheet through the timing sheet's existing modal code. The sheet has three modes: active, archived and pruned.
4. **Two new event types**, `ConversationArchived` (`To` = archived or active) and `ConversationPruned` (carrying a `PruneRecord` certificate). They are added to the closed catalog in one commit, together with the fold (no-op), the catalog test, `drive`, the data-model document and `graph.html`. The event's `actor` column is now filled from the signed-in account.
5. **Archive and restore** for cards that are DONE or CANCELED only. A pruned card cannot be archived or restored.
6. **An Archive page in the back office.** It lists archived conversations with Open and Restore, and has a "Prune all archived" form. The form needs the word PRUNE typed in and is bound to the cards it listed. A table of certificates follows.
7. **Prune** runs in three steps: select the cards under a read lock; delete their gateway sessions with no lock held; then, in one transaction, re-check, compute digests, empty the prompt, reply and question bodies, and write one certificate per card. Events, run records, context slices, artifact rows, handoffs, designs and reviews are kept.
8. **Register changes**: BR-41 is amended; D-082 is added and cross-referenced from D-012 and J-6; TC-478 to TC-489 are added (12 new tests, so 489 register rows and 365 Go tests).

### How the existing system works, per this document
- After each attempt the runner stores `<run>-prompt` and `<run>-reply` artifacts, plus `-handoff` and `-question` when they apply. Nothing reads prompt, reply or question artifacts today.
- ArcadeStore answers reads from an in-memory MemStore projection. At boot it loads only handoff, design and review artifacts, so prompts and replies exist only in the database after a restart.
- A card's attempts share one gateway session. DONE and CANCELED cannot move to any other status.
- `QuestionAsked` and `QuestionAnswered` events carry the question and answer; their `Text` is a summary shown on the Timeline.
- The gateway (ai-manager) has an admin `DELETE /admin/sessions/{id}` endpoint and no way to read a transcript.
- Every back-office route needs a session. Mutating requests from another site are refused, the cookie is SameSite=Lax, and the replay handle refuses mutations.
- BR-41 currently says prompts and replies stay readable forever.

The `file:line` anchors I checked are accurate, give or take a line: runner, questions, timing, taskstate, turns, store_arcade, fold, broker, app.js, auth, events_test, model, go.mod, brd and decision-log. The glossary is solid.

## Findings

### Prune says it deletes the conversation, but most of its text survives elsewhere - Severity: Major
- **Affected scope:** §2 G5, §3.7 "Survives", §3.12 D-082, §5 L1/L2, header "Operator ruling", §3.4 pruned mode.
- **Finding:** The document never tells the reader that the kept records contain the same text that prune "deletes".
  - **Questions and answers.** `QuestionAsked` and `QuestionAnswered` events carry the full question, context, options and the operator's full answer (`dashboard/questions.go:53-64`, `:204-207`). The event's `Text` holds the answer word for word when the operator typed their own answer (`questions.go:278`). So L1's phrase "a summary of the answer" understates what is kept. Emptying the `<run>-question` artifact removes a copy of text the event stream still holds in full.
  - **Prompts.** The prompt is `RenderPrompt` over the cleared packet (`dashboard/adapter_aimanager.go:112-121`, `:159`). The context slice that prune keeps (`store_mem.go:1109`) is that same cleared packet, minus the fixed contracts.
  - **What actually goes.** In practice only the reply and the gateway sessions are removed.
  - **L-8.** The certificate cites L-8, which says what survives is "the event skeleton and its business ids" (`brd.html:1186`). This is not a skeleton.
  - **The operator's words.** The product owner asked for something that "permanently deletes them". From this document a reader cannot tell that the product owner knows how little is removed.
- **Recommendation:**
  - State plainly, in §3.7, on the confirm form and in D-082, that the question and answer text survives in full in the event stream, and the prompt's content survives in the context slice.
  - Say whether the product owner accepted that, with an anchor.
  - Either drop the claim to satisfy L-8's "skeleton" wording or explain why it still holds.
  - Fix L1's "summary".

### OQ-17 is described as open, but the register shows it answered - Severity: Major
- **Affected scope:** header "Rules this must obey", §3.7 "Storage, honestly", §3.12, §5 L4, §8.
- **Finding:**
  - §8 says OQ-17 "stays open", and D-082 "records the gap against OQ-17". The register shows OQ-17 as "Answered → D-043" (`pustaka/docs/product/open-questions.html:104`, `:531-541`).
  - D-043 committed to checking, on 26.7.2, that an `EXTERNAL` payload can be destroyed separately from its record. That check is exactly what this design says is "not verified".
  - So the gap is an unmet commitment in D-043, not an open question, and the document points the reader at the wrong register entry.
  - Backups are not mentioned. L-9 (`brd.html:1187`) says backups carry the same deletion duties as the data they hold.
- **Recommendation:**
  - Re-anchor the gap to D-043, and say whether D-043's check was ever run.
  - Drop "stays open" for OQ-17.
  - Add one line on backups to "Storage, honestly".

### The prune commit step does not say how ArcadeStore gets the old bodies after a restart - Severity: Major
- **Affected scope:** §3.7 "Order", steps 1 and 3; §4 row 5b.
- **Finding:**
  - After a restart, prompt, reply and question bodies are only in the database (`store_arcade.go:1654-1667`).
  - Step 3 says "compute digests". Each body's SHA-256 and length must go into the certificate, but nothing says where the bodies are read from. It does not say which step reads them, whether the read happens inside `mutate` while the store lock is held, or how a missing artifact is handled (for example, a prompt that was never recorded because it was empty, `runner.go:221`).
  - `Result.PrunedArtifacts` appears only in change-list row 5b. It is never defined, and §3.7 describes the database `UPDATE`s without naming the mechanism.
- **Recommendation:**
  - Name the step that reads the bodies not held in memory, and the lock it holds while doing so.
  - Define `Result.PrunedArtifacts` (type, and who fills it) in §3.7.
  - Say what the certificate records for an artifact that does not exist.

### The approval delegation and the operator's ruling have no anchor - Severity: Major
- **Affected scope:** header line 9, §13.
- **Finding:**
  - The document says approval is delegated to "the orchestrating agent under the product owner's standing instruction of 2026-11-20/21 (§13)". §13 contains only "Pending round 2".
  - Neither that instruction nor the quoted ruling has a file or line. `docs/designs/README.md` rule 2 says an unanchored claim must be labelled as an assumption.
  - `docs/rules/gaos-design-review.md` (flow step 8) gives approval to the operator. This feature deletes data permanently.
- **Recommendation:** Anchor or quote the standing instruction and the ruling in §13, or label both as assumptions and route approval to the operator.

### The refusal list says every refusal is a 422, but the first is a 404 - Severity: Nit
- **Affected scope:** §3.5 refusal list; TC-486.
- **Finding:** The list says "each a `reject` (toast, 422)", but refusal 1 is `ErrNotFound`, which TC-486 asserts as a 404.
- **Recommendation:** Say "2–5 are `reject` (422); 1 is `ErrNotFound` (404)".

### Several names are undefined or do not match the code - Severity: Nit
- **Affected scope:** §3.4, §3.7, §3.8, §5 L3, §2 G3.
- **Finding:**
  - `.Talk` (`View.Talk`) and `CardRef` are used but never defined.
  - "The bucket expires it in 30 days" assumes the reader knows the gateway's session store.
  - In the routes table, `requireBO` reads like a guard, but it is a helper that handlers call. The guard is `gate` (`auth.go:523-555`).
  - `sessionOf(r).Email` does not match `sessionOf`'s two return values (`auth.go:355`).
  - "An archived conversation leaves the card" is ambiguous; it means the chip is hidden.
- **Recommendation:** Define `Talk` and `CardRef` where they are first used, name the session store, put `gate` in the routes table, and reword G3.

### The pruned mode of the sheet cannot be reached - Severity: Nit
- **Affected scope:** §3.4 pruned mode, §3.6 certificates table.
- **Finding:** A pruned card has no chip, and the certificates table has no Open link. Nothing in the UI opens the sheet in pruned mode.
- **Recommendation:** Add Open to the certificates table, or say that pruned mode can only be reached by URL.

### The prune toast is not specified for skipped cards - Severity: Nit
- **Affected scope:** §3.7 "Deleted", first paragraph.
- **Finding:** Cards that are no longer archived are "counted in the toast", but the toast text is not given. The outcome when every listed card is skipped is not stated either.
- **Recommendation:** Give the exact toast text, and the outcome when zero cards are pruned.

### Code blocks inside list items are not covered by the markdown rules - Severity: Nit
- **Affected scope:** §3.3 block rules.
- **Finding:** Model replies often put a code block inside a list item. A fence indented 3 spaces or fewer opens a fence and ends the list. A fence indented 4 or more becomes item text joined with `<br>`, so the fence markers show as literal text. Neither outcome is stated as intended.
- **Recommendation:** State the intended behaviour and add it to TC-478.

### Section numbers jump from 10 to 13 - Severity: Nit
- **Affected scope:** §10 to §13; §7.
- **Finding:** The document skips §11 and §12. §7 contains only a pointer ("§3.9"). The header cites §13 for content §13 does not contain.
- **Recommendation:** Renumber, or say the sections were intentionally left empty.

Counts: B=0 M=4 N=6
