# Zoom-out review: `docs/designs/conversations.md` (Draft v1)

The problem statement is clear. The data model reuses what already exists: the prompt, reply and question artifacts at `dashboard/runner.go:220-249`, the question events, and `runsFor` at `dashboard/store_mem.go:1207`. Several claims check out:
- **New markdown renderer:** a search found no markdown renderer in the repository (no `goldmark` or `blackfriday`, no markdown function), so `markdown.go` is not a reinvention.
- **Counts:** 477 + 11 = 488 and 353 + 11 = 364 match `deploy/check-workspace.sh:112,127` and `pustaka/docs/assets/toc/product.js:162,168`. TC-478 to TC-488 is 11 tests.
- **Decision number:** D-081 is the newest row and the log says "Eighty entries" (`decision-log.html:18,1655`).
- **Change list:** four `Store` methods and four routes, as counted.
- **Gateway call:** `admin()` and `onVendor` exist (`dashboard/aimanager_admin.go:45,76`) and the optional-capability pattern exists (`dashboard/workspace.go:99`). Reusing them is correct.

The problems are in one premise and in how much machinery §3.5 and §3.7 add on top of it.

### Prune contradicts BR-41 and D-012, but the design says it obeys them - Severity: Blocker
- Affected scope: header "BR rules this must obey" (BR-41); §2 G5; §3.7 "Deleted" item 1; §3.11 D-082
- Simplicity rationale: BR-41 (`pustaka/docs/product/brd.html:1300`) says the prompt and the reply are stored for every attempt "and remain readable after the session expires". D-012 (`pustaka/docs/product/decision-log.html:116`) keeps everything except events, samples and logs "indefinitely — because the audit trail is the product". Prune removes exactly those bodies. The document cites BR-41 as a rule it obeys and as the reason for N1. It never says it is amending a business rule and an earlier decision. Bug-for-bug, this is the same kind of change as opening BR-05: a change to a register of record dressed as a UI feature. D-028 (external auditor, `open-questions.html:89`) is also affected and is not mentioned.
- Recommendation: D-082 should say plainly that it amends BR-41 and narrows D-012: operator-pruned prompt, reply and question bodies are the one exception to "indefinitely". Add the BRD row edit and the D-012 cross-reference to the change list. Put the BR-41 amendment in §8 as a question for the operator, not "None blocking".

### Archive any card, not just finished ones, and it costs a whole layer of states - Severity: Major
- Affected scope: §3.5 refusal list; §3.7 item 2 "only when the task is finished"; `SessionsKept`; L2
- Simplicity rationale: archiving is refused only while a task is DISPATCHED or RUNNING. So an AWAITING_INPUT, FAILED or blocked card can be archived and pruned, and then run again. That one choice causes:
  - the "a turn will write into an archived conversation" problem, which applies to every non-terminal status, not just RUNNING;
  - the kept-session branch and the `SessionsKept` count;
  - L2 (archive again after the task finishes);
  - the pruned-then-archived-again state (§3.5, last paragraph);
  - the chip contradiction in the next finding.
- Recommendation: allow archive only when `terminal(status)` is true. That helper already exists at `dashboard/timing.go:138`. Then every archived card has a finished session, `SessionsKept` and L2 go away, and the refusal list becomes one line.

### A pruned card that runs again loses its chip for good - Severity: Major
- Affected scope: §3.4 "An archived or pruned card shows no chip"; §3.5 `conversationStateOf` ("the pruned flag stays")
- Simplicity rationale: `Pruned` never clears. A card that runs again after a prune has new, unpruned attempts, but no chip to open them. G1 then fails for that card.
- Recommendation: if the previous finding is adopted, this case cannot happen. Otherwise, don't make `Pruned` a card state. Show the chip whenever the card has runs and is not archived. Show the tombstone only inside the sheet and on the Archive page.

### The prune ordering rewrites itself halfway through and still leaves a half-done state - Severity: Major
- Affected scope: §3.7 "Order" paragraph
- Simplicity rationale: the paragraph gives steps (a) to (d), then adds a step (a′) and a transient in-memory `pruning` mark to fix a race it has just described. Step (b) commits the body nulling in ArcadeDB before step (d) writes the tombstone. If (d) fails, the bodies are gone and there is no tombstone, which contradicts "leaves nothing half-said". It is also a new kind of lock-released state that only this feature has.
- Recommendation: use two steps.
  1. Outside the lock, delete the gateway sessions of the selected finished cards (best effort, with counts).
  2. Under the write lock, re-check which cards are still archived. Then in one `mutate`, null their bodies and add their tombstones.

  A card restored between the two steps keeps its bodies. It loses only a finished task's session, which BR-41 itself calls transport. No `pruning` mark is needed.

### Deleting gateway sessions is the costliest part, and its scope is assumed rather than decided - Severity: Major
- Affected scope: §2 G5; §3.7 item 2; `SessionJanitor`; TC-484 and TC-487; the security-lens trigger; L3
- Simplicity rationale: §3.1 itself says a session "is not a conversation the operator can see". The sessions also expire on their own after 30 days. This one goal brings in:
  - a new outbound destructive admin call and a new interface;
  - three tombstone counters and a 15 s timeout per call;
  - the dependency on the admin key;
  - two of the eleven tests;
  - most of the ordering problem above.

  There is also an unchecked assumption. `onVendor` is documented as fanning a call out to "every account of that vendor's pool" (`dashboard/aimanager_admin.go:72-75`), and that was written for workspaces. Whether `DELETE /admin/sessions/{id}?provider=antigravity` behaves the same way is not stated.
- Recommendation: make this an explicit operator choice in §8. The product owner's "permanently deletes" may or may not cover the gateway copy. If it is kept, check and record the fan-out behaviour. If it is cut, prune becomes a store-only operation, and L3 already documents the 30-day expiry as the backstop.

### The Archive page's "Open" link goes to a sheet that shows nothing - Severity: Major
- Affected scope: §3.6 "an **Open** link to the sheet in read mode" vs §3.4 "An archived conversation opened by URL shows only 'This conversation is archived…'"
- Simplicity rationale: prune cannot be undone. As written, the operator cannot read what they are about to delete, because the only route to it shows a placeholder.
- Recommendation: an archived conversation's sheet should render the attempts read-only, with no Archive button and a Restore button instead. Only a pruned sheet should show just the tombstone.

### A new opener attribute and URL helper are not needed - Severity: Nit
- Affected scope: §3.4 "JS" paragraph; change list #10 `sheetURL`
- Simplicity rationale: the click delegate at `dashboard/static/app.js:40` already opens any `[data-timing-open]` URL as a sheet. `timingURL` (`dashboard/timing.go:374`, registered at `dashboard/render.go:142`) already shows the pattern. A second attribute, the `||` branch and a generic `sheetURL` add nothing. The paragraph is also implementation detail, not architecture.
- Recommendation: add a `conversationURL` beside `timingURL`, and either reuse the existing attribute or rename it once to something neutral. Leave the JS details to the implementation.

### Reusing the timing index makes its name and comment wrong - Severity: Nit
- Affected scope: §3.2 MemStore bullet; change list #7
- Simplicity rationale: reusing the index is right. `cardTiming` ignores unrelated event types (`dashboard/timing.go:161-165`), so timing is safe. But `timingEventsLocked` and its comment (`dashboard/timing.go:380-385`) say "TaskPlanned … and TaskMoved", which stops being true once four more types are indexed.
- Recommendation: say the function is renamed to something like `taskEventsLocked`, or its comment widened, in the same change.

### `Rule` is overloaded to mean archive or restore - Severity: Nit
- Affected scope: §3.5 table, `ConversationArchived` payload
- Simplicity rationale: `EventPayload.Rule` is documented as the egress refusal's machine-readable rule (`dashboard/model.go:252-255`). §6 rejects `RecordUpdated` because overloading it would give it "a second meaning", then does the same thing with `Rule`.
- Recommendation: use `To` (`archived` or `active`), or name the overload in the field's comment. Either way, apply the §6 argument consistently.

### Wrong citation for `renderBOAfter` - Severity: Nit
- Affected scope: §3.8 "`renderBOAfter`, `handlers_board.go:78`"
- Simplicity rationale: the function is defined at `dashboard/handlers_bo.go:494`.
- Recommendation: cite the definition.

Counts: B=1 M=5 N=4
