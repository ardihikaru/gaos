# Design review: `docs/designs/conversations.md` v1 (critic lens)

**On isolation:** while I worked, the harness put the contents of `.claude/CLAUDE.md` into my context without my asking (main-checkout copies of three rule files also arrived that way). I did not open `CLAUDE.md` with a tool and did not use its contents for any finding below; the rule files were read from the worktree. Because the text did reach my context, the canary probe for this run should be treated as not proving isolation.

**Verdict:** the design cannot go ahead as written. Prune deletes the per-attempt record that BR-41 requires to stay readable, and the design claims to obey BR-41 at the same time. The "exactly what goes and what stays" section misses two places where the deleted content survives. The prune ordering can leave bodies deleted with no record of who deleted them.

### Prune deletes what BR-41 says must stay readable, while the header claims to obey BR-41 - Severity: Blocker
- Affected scope: header line 7, §2 N1, §3.7 item 1, §3.11, §4.
- Finding and rationale: BR-41 (`pustaka/docs/product/brd.html:1300`) says the prompt and reply "are stored by GAOS for every attempt, and remain readable after the session expires". The runner writes these artifacts for that reason (`dashboard/runner.go:218-220`, the comment cites BR-41). §3.7 sets exactly those bodies to null, and §0 says GAOS has no other copy (the gateway exposes no transcript read). So once pruned, the record is gone for good. Yet the design lists BR-41 among the rules it obeys and uses it against the raw-transcript alternative (N1). The BRD already has the right authority for deletion: L-7 (`brd.html:1185`: destroy the `EXTERNAL` payload, keep the event references) and L-8 (`brd.html:1186`: deletion produces a certificate of what was deleted and what deliberately survived). OQ-14 (`open-questions.html:272`) also describes this deletion approach. The design cites none of them. As written, D-082 contradicts a business rule without amending it, and no register records that prune is an exception.
- Recommendation: D-082 should amend BR-41 explicitly, for example "…readable until an operator prunes them (L-7)", or the design should state why L-7 overrides it. Cite L-7, L-8 and D-034 in the header. Add `pustaka/docs/product/brd.html` to the change list (the BR-41 amendment note). Make the tombstone satisfy L-8 by listing what survived, not only counts.

### The prompt and the operator's answer survive prune in places §3.7 does not list - Severity: Major
- Affected scope: §3.7 "Kept", §5 L1, §2 G5.
- Finding and rationale:
  - **The question and answer stay visible.** L1 says prune "removes them from every page". It does not. `QuestionAsked` puts the question into `Event.Text` (`runner.go:322`, `"%s asks: %s"`), `QuestionAnswered` puts the answer summary there (`questions.go:206`), and the product Timeline prints `Event.Text` (`dashboard/templates/backoffice.html:430`).
  - **"What GAOS sent" stays stored and served.** The packet is also frozen per attempt as `ContextSlice.rendered` (`dashboard/model.go:843-868`) and served by `GET /tasks/{id}/context` (`dashboard/main.go:354`). That packet includes `context/answer.md` and `context/handoff-previous.md`. §3.7 lists neither in "Deleted" nor in "Kept".
- Recommendation: list `ContextSlice.rendered` and event `Text` explicitly under "Kept", or add the slice to what prune deletes (L-7 allows destroying an `EXTERNAL` payload). Correct L1 so it does not claim the question and answer vanish from every page. Show these surviving copies in the confirmation form, since L-8 requires that what survives is disclosed rather than discovered.

### The prune order can delete bodies without writing the tombstone - Severity: Major
- Affected scope: §3.7 "Order", G5.
- Finding and rationale:
  - **A crash leaves deleted bodies with no record.** Step (b) commits `UPDATE … SET body = null` in its own transaction, outside `mutate` (`dashboard/store_arcade.go:443-455`). The `ConversationPruned` event is only written in step (d), after up to N×15 s of gateway calls. A crash or a failed persist between (b) and (d) leaves the bodies gone with no tombstone, no actor and no time. That breaks BR-02 ("every change emits an event", `brd.html:1226`) and L-8, and it contradicts the design's own "a failure leaves nothing half-said".
  - **The mark does not survive a restart.** The in-memory `pruning` mark from (a′) is lost on restart.
  - **The text is hard to follow.** The paragraph states (a)–(d) and then revises the order inside the same sentence ("so (b) runs **after** a re-check: (a′)…").
- Recommendation: delete the gateway sessions first, since they are expendable and expire on their own. Then, in one `mutate`, set the bodies to null and append the tombstones, so both commit or neither does. Rewrite the order as one numbered list with no revisions inside it.

### Prune nulls bodies by task id, so it can delete attempts that ran after the card was archived - Severity: Major
- Affected scope: §3.5 (archive allowed for any non-running status), §3.7 step (b) `UPDATE … WHERE taskId = :task`.
- Finding and rationale: archive is refused only while the task is `DISPATCHED` or `RUNNING`. An archived `READY`, `FAILED` or `AWAITING_INPUT` card can still be dispatched afterwards (`FAILED → READY`, `dashboard/taskstate.go:88`). Nothing stops the scheduler from running an archived card, so the hazard §3.5 refuses archive to avoid ("the reply that turn writes would land in an archived conversation unseen") happens anyway. On top of that, an `UPDATE` keyed by `taskId` nulls any attempt written after step (a) collected the run ids. That attempt is not in the tombstone's `Runs`, so the tombstone would undercount what was deleted.
- Recommendation: null bodies by the artifact ids collected in (a), never by `taskId`. Then choose one of two rules: either allow archive only for terminal tasks (`DONE`/`CANCELED`, which `taskstate.go:87,89` makes terminal), or have every new run on an archived card clear the archive with an event.

### The operator cannot read what they are about to destroy, and the confirmation is not tied to what they saw - Severity: Major
- Affected scope: §3.4 last paragraph, §3.6 "Open link … in read mode", §3.7 confirm.
- Finding and rationale:
  - **The Open link shows nothing.** §3.6 promises an "Open link to the sheet in read mode". But §3.4 says an archived conversation opened by URL shows only "This conversation is archived — restore it from the Archive page." Read mode does not exist, so the only way to inspect an archived conversation before an irreversible prune is to restore it first.
  - **The typed `PRUNE` is not bound to a set.** It applies to every archived card in every product. A card archived by anyone after the Archive page was rendered is pruned without ever having been shown.
- Recommendation: define read mode (the full sheet with a banner and no Archive button). Have the prune form carry the archived set it displayed (task ids, or the highest event seq at render time), and prune only that set.

### Card states after a prune contradict L2 and G1 - Severity: Major
- Affected scope: §3.4 chip rule, §3.5 `conversationStateOf`, §5 L2.
- Finding and rationale: after a prune, `Pruned` stays true and "An archived or pruned card shows no chip". A card that runs again (a non-terminal task that was archived) therefore never shows its new attempts again, which contradicts G1. L2 tells the operator to "archive it again", but the only Archive button is in the sheet, the sheet is reached only from the chip, and a pruned sheet "shows the tombstone line". The archive-again path cannot be reached from the UI.
- Recommendation: limit `Pruned` to the runs listed in the tombstone. Show the chip whenever the card has runs that are not pruned. Render pruned turns as "deleted" (as L-7 requires) beside the live turns.

### The design writes deleted-body states three ways and calls one of them fine - Severity: Major
- Affected scope: §3.7 (b) and (d), TC-488, §5 L4.
- Finding and rationale:
  - **L4 contradicts the register.** It says whether ArcadeDB reclaims the external storage "is the database's business". OQ-17 (`open-questions.html:527-530`) says the opposite: "a deletion promise resting on an unverified storage behaviour is not a promise", and L-7 relies on that behaviour.
  - **Null and empty string both appear.** Step (d) empties in-memory bodies. The persist loop then re-upserts them as `""` (`store_arcade.go:893-905`, the fingerprint changes), so a body loaded since boot ends up `""` while the others stay `null`. TC-488 asserts null.
- Recommendation: either verify that external storage is reclaimed on ArcadeDB 26.7.2 (D-043) and add a test for it, or downgrade the promise in G4, G5 and D-082 to "unreferenced, reclaimed by the database". Pick one deleted representation, write it in both stores, and assert that.

### The per-card artifact query has no index - Severity: Major
- Affected scope: §3.2 ArcadeStore, §3.7 (b).
- Finding and rationale: the only index on `Artifact` is `(tenantId, id) UNIQUE` (`dashboard/schema/09-indexes.sql:42`). `WHERE tenantId AND taskId AND kind IN [...]` therefore scans every artifact in the type, and artifacts grow without bound. That scan runs on every sheet open after a restart, and once per card inside the prune transaction. N4 forbids a new schema statement, so the design rules out the fix.
- Recommendation: query by id instead. The ids are deterministic (`<run>-prompt`, `<run>-reply`, `<run>-question`, `runner.go:223,246`) and the run ids are known, so the indexed `id IN [...]` works. Write that query in the design.

### The markdown renderer's linear-time claim is not backed by anything - Severity: Major
- Affected scope: §3.3, TC-479.
- Finding and rationale: §3.3 treats model text as attacker input, then asserts the renderer is linear because it uses only `strings` scanning. Emphasis and link matching that scans forward for a closing delimiter is quadratic on input like 500 KiB of unmatched `*` or `[`. A 511 KiB body passes under the 512 KiB cap and is parsed, inside a request handler with no deadline. TC-479 only tests the 600 KiB case, which skips parsing entirely.
- Recommendation: specify how delimiters are matched (one pass with a bounded delimiter stack, or a closer searched only within the same line). Add a pathological 500 KiB input with a time bound to TC-479.

### Wider use of an over-broad admin key, built on assumptions about a repository outside this one - Severity: Major
- Affected scope: §0 gateway bullet, §3.7 item 2, §7.
- Finding and rationale: OQ-29 is open (`open-questions.html:733-738`). The admin bearer "lists, deactivates and deletes *every* caller's sessions". This design is the first to use it for destructive session calls, driven from a browser form, and neither §7 nor the open questions mention OQ-29. The 409/404 behaviour and the antigravity `?provider=` behaviour of `DELETE /admin/sessions/{id}` come from a document on another host ("read on `rnd-v2-vpn`"), which no reviewer can check. `onVendor` fans a call out to every account in a vendor's pool (`dashboard/aimanager_admin.go:72-75`), and how session deletes behave across a pool is not stated.
- Recommendation: label the gateway behaviour as an assumption, under the README rule 2. Cite OQ-29 in §7 as a risk this change amplifies. TC-487 should pin the exact URL and the pool behaviour.

### The "No session id in an event" rationale is false - Severity: Nit
- Affected scope: §3.5 paragraph after the table.
- Finding and rationale: `SessionOpened` and `SessionLost` already carry the full session id (`dashboard/turns.go:152-160`; data model `artifacts/v1/gaos-arcadedb-model.html:1400-1401`). Keeping it out of the tombstone protects nothing.
- Recommendation: keep counts only for brevity, but drop the security reasoning, or say the stream already holds the ids.

### `Actor` duplicates the existing `Event.actor` column - Severity: Nit
- Affected scope: §3.5 `EventPayload.Actor`.
- Finding and rationale: the Event vertex already declares `actor` (`dashboard/schema/07-events.sql:26`). `writeEvent` hard-codes it to `'operator'` (`store_arcade.go:1068`).
- Recommendation: set the column, or explain why the payload carries the actor instead.

### Wrong or outdated line references - Severity: Nit
- Affected scope: §0, §3.1, §3.8, §3.7.
- Finding and rationale:
  - `TaskRunRecord` is at `model.go:766-802`, not 740-797; `SessionID` and `Provider` are at 795 and 798.
  - `sessionForLocked` is defined at `turns.go:102`; `runner.go:595` is only a call site.
  - `renderBOAfter` is defined at `handlers_bo.go:494`; `handlers_board.go:78` is a call.
  - `AIMClient.admin` takes a `ctx` (`aimanager_admin.go:45`), which the §3.7 call omits.
  - BR-25 is about `EXTERNAL` storage (`brd.html:1270`), not "no prompt body on a run record".
- Recommendation: correct all five.

### The change list misses `broker.go`, so other tabs do not update - Severity: Nit
- Affected scope: §4.
- Finding and rationale: `sseName` (`dashboard/broker.go:46-54`) sends the new events as `event`, not `task`, so a board open in another tab keeps the chip after an archive.
- Recommendation: add `broker.go` to the change list, or state that other tabs refresh only on the next task event.

### Markdown rules that are ambiguous or will mangle code-heavy replies - Severity: Nit
- Affected scope: §3.3 table.
- Finding and rationale:
  - `_i_` without an intraword rule turns `snake_case_name` into italics.
  - "2+ spaces … relative to the parent's content column" can be read two ways.
  - GFM tables are neither supported nor listed as unsupported.
  - `html.EscapeString` is used where the frontend rule names `template.HTMLEscapeString`.
- Recommendation: forbid intraword `_` emphasis, define indentation with one worked example, say tables render as text, and name the escaping function the rule requires.

### Counts and identifiers check out - Severity: Nit
- Affected scope: §3.10, header.
- Finding and rationale: TC-477 is the highest in use and 353 Go tests exist (`.claude/rules/qa-test-plan.md:50,129`), so 488, 364 and next-free TC-489 follow. "Eighty entries" and `d-081` as the newest row are confirmed (`decision-log.html:18,1655`). The four routes and four `Store` methods match the change list. No change needed.
- Recommendation: none.

Sections with nothing to add: the route guards (`auth.go:523-577` behaves as described), the decision to scope archive to the card (§3.1), and the fold no-op placement beside `fold.go:378-382`.

Counts: B=1 M=10 N=5
