# Review: `docs/designs/conversations.md` v2 (gaos-goldfish-critic)

I found 1 Blocker, 7 Majors and 4 Nits. The Blocker: prune deletes almost nothing that does not survive elsewhere, yet the design, D-082 and the BR-41 amendment all say it deletes the conversation's text.

**Isolation note:** the harness put `.claude/CLAUDE.md` into my context without my opening it. I did not use it and quote nothing from it. The harness also injected the main checkout's `.claude/rules/qa-test-plan.md`, which says TC-465; I used this worktree's copy (TC-477).

**Claims I confirmed:**
- BR-41 text: `brd.html:1300`.
- L-7 and L-8: `brd.html:1185-1186`.
- J-6: `brd.html:985`.
- D-012: `decision-log.html:116`.
- D-081 and "Eighty entries": `decision-log.html:1655`, `:18`.
- DONE and CANCELED have no exits: `taskstate.go:87,89`.
- `terminal()`: `timing.go:138`.
- Boot load reads only handoff, design and review: `store_arcade.go:1654-1672`.
- Fold default skips unknown types: `fold.go:445-449`.
- Catalog test and `drive`: `events_test.go:17-45`, `:124`.
- Data-model rows: `gaos-arcadedb-model.html:768`, `:1409`.
- Auth, cookie and BO helpers: `auth.go:493`, `:560`; `auth_test.go:258`; `handlers_bo.go:494`.
- SSE name and click selector: `broker.go:46`; `app.js:40,49`.
- Count baselines: 477 cases and 353 Go tests.

---

### Prune deletes almost nothing that does not survive elsewhere, but the design says it "deletes the conversation's text" - Severity: Blocker
- Affected scope: §2 G5; §3.7 "Deleted"/"Survives"; §3.11; §3.12 title; §5 L1; §1 status line ("operator's ruling")
- Finding and rationale: Each deleted body is still held somewhere else:
  - **Prompt.** `RenderPrompt` (`dashboard/adapter_aimanager.go:112-122`, `:334-345`) builds it from the cleared packet's parts plus fixed contract text. `recordSlice` (`dashboard/store_mem.go:1093-1109`) freezes those same parts into `ContextSlice.Rendered`. Prune keeps that slice, and `GET /tasks/{id}/context` still serves it. Deleting the prompt removes no unique content.
  - **Question and answer.** `QuestionAsked` and `QuestionAnswered` carry the full question, context, options and free-text answer in their payload (`dashboard/questions.go:53-64`, `:204-207`). That is not a "summary".
    - `Event.Text` also quotes a free-text answer verbatim through `answerSummary` with `%q` (`questions.go:278`). L1 is wrong to call it "the answer's summary".
    - The conversation never reads the `question` artifact anyway: §3.2 takes the question from events. Emptying that artifact changes nothing anyone can see.
  - **Reply.** Its handoff, design and review blocks survive as artifacts by design.

  In practice, what prune removes is the reply's prose outside those blocks, and the gateway sessions. L-8 exists so that what survives is "disclosed rather than discovered" (`brd.html:1186`). Listing "ContextSlice" in `Kept` does not tell anyone that the prompt is still there word for word. The product owner's ruling was for prune that "permanently deletes them". The BR-41 amendment and D-082 rest on that ruling, but describe a deletion this design does not perform.
- Recommendation: Take this back to the operator with the truth in plain words: "the prompt's content survives in the context slice; the question and your answer survive in full in the event stream; the handoff, design and review blocks survive". Then the operator chooses one of three options, and the D-082 text and the confirm form use the result:
  - (a) Accept that, reword G5, D-082, the BR-41 amendment and the confirm form to "deletes the reply text and the gateway sessions", and drop the `prompt` and `question` artifacts from the deletion scope.
  - (b) Widen the deletion to the slice's `rendered` and keep only its digest. That needs a separate BR-32 ruling.
  - (c) Move question and answer content out of the event payload into an EXTERNAL artifact the event references (L-7's model). That is a catalog-payload change.

### OQ-17 is answered, not open, and D-043 already promised the verification this design calls "not verified" - Severity: Major
- Affected scope: header "Rules this must obey" (OQ-17 row); §3.7 "Storage, honestly"; §5 L4; §8 ("stay open"); §3.12
- Finding and rationale: OQ-17 is "Answered → D-043" (`pustaka/docs/product/open-questions.html:104`, `:531`). D-043 (`decision-log.html:141-155`) says that destroying an EXTERNAL payload separately from its record "is verified in that same pass" before build-order step 1. Either that check was done, and the design should cite its result, or it was not, and D-043 holds a false claim. "Recording the gap against OQ-17" puts a new fact on a closed register row.
  - The design is also silent on L-9 (`brd.html:1187`): "backups inherit … the deletion obligations". It does not mention any ArcadeDB backup, export or WAL that holds the old bodies.
- Recommendation: Cite D-043, and say whether its EXTERNAL-destroy check was ever run on 26.7.2. If it was not, raise a new OQ that narrows D-043 rather than reopening OQ-17. Add L-9 to the certificate's disclosed survivors ("copies in backups or exports taken before the prune").

### Commit-step atomicity holds in the database but not in the projection - Severity: Major
- Affected scope: §3.7 "Order" step 3; "Failure at each step" (3)
- Finding and rationale: `mutate` runs `apply()` first, which changes the MemStore projection: bodies set to `""` and `ConversationPruned` emitted. Only then does it call `persist`. If `persist` fails, it rolls back the database transaction and nothing restores memory (`dashboard/store_arcade.go:443-455`, `:463-474`).
  - The result: the UI shows "Pruned … by …" and a certificate, while the database still holds every body and no certificate. A restart brings the text back.
  - For a deletion certificate, a certificate the database does not hold is the worst outcome. The claim "commits bodies and certificates together or neither" is true only of the database.
- Recommendation: Either compute the prune as data first (digests, ids, event), persist it, and change memory only after commit; or snapshot the affected artifacts and events and restore them when `persist` fails. Add a TC that injects a persist failure and asserts that the sheet is not in pruned mode.

### Sessions can be deleted with no event, and a 404 is counted as a deletion - Severity: Major
- Affected scope: §3.7 steps 2–3 and failure mode (2); `PruneRecord.SessionsDeleted`; §5 L3
- Finding and rationale: BR-02 says "every change emits an event" (`brd.html:1226`). Step 2 deletes gateway sessions before any event exists. If step 3 fails, or if the card was restored between steps 1 and 3 (it is then skipped), those deletions are never recorded anywhere.
  - "2xx or 404 → deleted" lets a misrouted call count as success. Examples: a provider other than claude or antigravity, or a wrong `?provider=` choice (`onVendor`, `dashboard/aimanager_admin.go:76-81`, with `runVendor` defaulting an empty value to claude at `vendors.go:245-250`). The wrong worker answers 404 and the certificate says the session was deleted while it still exists.
- Recommendation:
  - Re-check that the card is still archived immediately before each DELETE.
  - Record a session-deletion attempt even when no body commit follows. Either emit the certificate with a "bodies not deleted" state, or put the deletion outcome in the next successful certificate.
  - Count 404 as "already gone" separately from "deleted", and refuse (count as failed) any vendor the DELETE route is not known to serve.

### "Every session a terminal card names is finished with" is asserted, not enforced - Severity: Major
- Affected scope: §3.1; §9 R3
- Finding and rationale: The session id is whatever the gateway returns (`att.SessionID`, `dashboard/store_mem.go:1190`, `adapter_aimanager.go:289`), not something GAOS assigns. `sessionForLocked` even scans every task's runs for a matching `SessionID` (`dashboard/turns.go:134-137`). Nothing in GAOS guarantees that two tasks never end up in one gateway session.
  - If they ever do, prune deletes a session a live card is about to resume. The gateway's 409 protects only a turn that is running at that moment.
- Recommendation: In step 1, skip (and count as "kept, shared") any session id that is also named by a run of a card outside the prune set. Add a negative TC for it.

### ArcadeStore prune: no source named for digests of unloaded bodies, and an unbounded transaction under the store lock - Severity: Major
- Affected scope: §3.7 step 3 ("compute digests"); §3.2 ArcadeStore read path; change list 5b
- Finding and rationale: After a restart, prompt, reply and question bodies are not in memory (`dashboard/store_arcade.go:1654`). The certificate's SHA-256 and length need the old body. The design never says that step 3 reads each one from the database, inside `mutate`, under `s.mu`. A natural implementation hashes the in-memory `""` and records a false digest for every body.
  - "Prune all archived" is then N SELECTs plus N UPDATEs in one transaction under the global lock, and `mutate` has no budget. Compare `persistBackground`'s 60 s budget (`:253-263`) and the v0.40.1 incident in which the lock was held for minutes (`:887-891`).
  - It is also unspecified whether the sheet's per-artifact reads (§3.2) take `s.mu`.
- Recommendation:
  - Specify the body read: read before taking the lock, then re-check under the lock using the run list.
  - Cap the number of cards per prune, or batch them into one transaction per card.
  - Give the prune transaction an explicit budget.
  - State that the conversation read path does not take `s.mu`.
  - Have TC-488 assert a non-empty-string digest after a reopen.

### Markdown rules break code fenced inside list items, which the product owner asked for by name - Severity: Major
- Affected scope: §3.3 block table (fence row, continuation row)
- Finding and rationale: A fence opens only after ≤3 spaces. A fence under a nested bullet (4 or more spaces) therefore becomes "continuation text joined with `<br>`" with inline rules applied. Inside it, `*`, `_` and backticks would turn into emphasis or code. A fence at 3 spaces under `1.` opens a fence, but nothing says whether it closes the list or nests in the `<li>`.
  - Model replies use fences inside lists all the time. Two engineers would build this differently, and the result fails G2's "readable as written".
- Recommendation: Define fences relative to the enclosing item's content column. A fence indented to an item's content nests as `<pre>` inside that `<li>`, and continuation never applies inline rules inside an open fence. Add that case to TC-478's fixed example.

### Approval by delegation is unanchored for a business-rule amendment and an irreversible deletion - Severity: Major
- Affected scope: header "Operator ruling"; §13
- Finding and rationale: The gate requires the operator to see every Blocker and Major and to approve (`docs/rules/gaos-design-review.md:87-88`). The design cites a "standing instruction of 2026-11-20/21" with no file or line (`docs/designs/README.md` rule 2). That delegation would cover amending BR-41, creating an exception to D-012 and J-6, and a destructive admin-key call.
  - Because of the Blocker above, the product owner's quoted ruling ("permanently deletes them") is not what this design does. An agent cannot approve a trade-off the operator never saw.
- Recommendation: Anchor the delegation to a file. Exclude register amendments and irreversible deletions from delegated approval, and put the BR-41 and D-082 wording in front of the operator directly.

### Section numbering and empty sections - Severity: Nit
- Affected scope: §7, §10→§13
- Finding and rationale: §11 and §12 do not exist. §7 "Phases" only points to §3.9.
- Recommendation: Renumber, and fold §7 into §3.9 or the other way round.

### Change list leaves some edits implied - Severity: Nit
- Affected scope: §4 rows 4, 5b and 17
- Finding and rationale: `Result.PrunedArtifacts` is a field on `Result` (`dashboard/store.go:175`), but row 4 does not list it. `deploy/check-workspace.sh:129` hard-codes "Eighty entries", and the worktree's `.claude/rules/qa-test-plan.md` names the TC and decision counts. Row 17 just says "counts".
- Recommendation: Name each one: `Result` gains `PrunedArtifacts` in `store.go`; "Eighty entries" becomes "Eighty-one entries" at `check-workspace.sh:129` and in `qa-test-plan.md`; TC-477 becomes TC-489 and 478 becomes 490.

### `DeleteSession` via `c.admin` would leak the session id unless the error is replaced - Severity: Nit
- Affected scope: §3.7 `SessionJanitor` paragraph
- Finding and rationale: `c.admin` puts the request path, which contains the session id, into its errors (`dashboard/aimanager_admin.go:47`, `:59`, `:65`). §3.7 calls `c.admin` directly and also promises errors free of the id. TC-487 would catch this, but the design should say it.
- Recommendation: State that `DeleteSession` discards `c.admin`'s error and returns a bare sentinel, never a wrapped one.

### Glossary contradicts an in-repo comment on pool fan-out - Severity: Nit
- Affected scope: §0 "Gateway"
- Finding and rationale: The glossary says the antigravity session store is "not a pool fan-out". `onVendor`'s comment says that `?provider=` "fans the call out to every account of that vendor's pool" (`dashboard/aimanager_admin.go:72-75`). That comment may cover workspace calls only.
- Recommendation: Say explicitly that the fan-out comment covers workspace routes and not `/admin/sessions`, citing the external commit the glossary already names.

Counts: B=1 M=7 N=4
