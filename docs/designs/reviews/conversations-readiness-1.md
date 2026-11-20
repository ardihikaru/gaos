# Readiness review: `docs/designs/conversations.md` (Draft v1)

Isolation note: during this session the harness automatically added the contents of `.claude/CLAUDE.md` to my context, along with a copy of `.claude/rules/qa-test-plan.md` from outside this worktree. I did not open either file and did not use them. Every count below comes from the files in this worktree.

**What checked out:**
- **Identifiers:** D-081 is the newest decision row (`decision-log.html:1655`). TC-477 is the highest case in use (`qa-test-plan.html:568`). The arithmetic 477+11=488 and 353+11=364 is correct, and TC-478…TC-488 is exactly 11 IDs.
- **Code anchors:** `runner.go:217-249`, `store_arcade.go:1654-1667`, `fold.go:378`, `auth.go:523-577`, `aimanager_admin.go:45-81`, `workspace.go:99` and `10-storage.sql:48` all match what the design says.
- **Phase P4's count step** really does have to touch `deploy/check-workspace.sh`: the file hard-codes "Eighty entries" (`:129`) and the TC and Go-test counts (`:112-128`).

### Prune contradicts BR-41, which the design says it obeys - Severity: Blocker
- Affected scope: header "BRD rules this must obey"; §2 N1; §3.7 "Deleted" item 1; §3.11
- Finding: BR-41 (`brd.html:1300`) says the prompt and the reply "are stored by GAOS for every attempt, and remain readable after the session expires". Prune nulls exactly those bodies while the run record stays, so BR-41 is broken. The design cites BR-41 as a rule it follows (N1: "those are the record (BR-41)") and never reconciles the two. The only deletion authority in the BRD is L-7 (`brd.html:1185`), and it covers deleting "a tenant, a product or a run", not a card's payloads with the run kept. The design never cites L-7. An implementer cannot tell whether the operator has approved changing a business rule.
- Recommendation: state the BRD change explicitly. Either amend BR-41 (for example, "…unless the operator prunes an archived conversation, D-082") or extend L-7 to cover card-level payload deletion. Add `pustaka/docs/product/brd.html` to the change list, name the amendment in D-082, and say whether the "Fifty-six rules" count changes.

### The §3.7 prune ordering contradicts itself and does not fit ArcadeStore's `mutate` - Severity: Blocker
- Affected scope: §3.7 "Order"
- Finding: the paragraph sets out steps (a)–(d), then patches them with an (a′) that replaces (a)'s read lock with a write lock and a `pruning` mark. Problems:
  - It says a card restored between (a) and (d) "is skipped at (d)", then says the mark makes restore impossible. Both cannot be true.
  - The `pruning` mark is never specified: no field, which store holds it (MemStore, or ArcadeStore's projection), whether archive is also refused, or what clears it on error or panic.
  - Step (b) is "one `UPDATE` per card, in one transaction", but ArcadeStore persists through `mutate` (`store_arcade_blueprint.go:18-29`). The design does not say whether (b) runs inside `mutate`, or how (b) and (d) can be two separate transactions without leaving bodies nulled and no tombstone if (d) fails.
  - Between (b) and (d) the database holds null while memory still holds the body. At (d), clearing the in-memory body changes the artifact's fingerprint, so `mutate` upserts `body: ""` (`store_arcade.go:893-903`) over the null. That contradicts "`body = null`", which TC-488 asserts.
  - Nothing says whether the 15 s gateway calls in (c) run one after another or in parallel, or what bounds the whole HTTP request.
- Recommendation: replace the paragraph with one ordered sequence that names the lock or `mutate` call at each step. Define the mark (a field and who reads it), the refusal text it causes, and the failure behaviour after each step (what is committed and what is rolled back). Choose either null or "" as the final stored body, and make TC-488 assert that value.

### "Pruned" semantics make new attempts invisible, and L2's re-archive step impossible - Severity: Major
- Affected scope: §3.2 `ConversationState.Pruned` comment; §3.4 Chip; §3.5 `conversationStateOf`; §5 L2
- Finding:
  - The struct comment says `Pruned` means "a ConversationPruned exists after the last archive". §3.5 says that after a re-archive "the pruned flag stays". These two definitions conflict.
  - Under either one, a card that runs again after a prune (for example AWAITING_INPUT → answered, which the session rule deliberately allows) has `Pruned=true` and `Archived=false`. The chip is hidden whenever the card is pruned (§3.4), and the sheet shows only the tombstone. The new attempts can never be seen.
  - The Archive button exists only on the sheet, so L2's "they archive it again" cannot be done.
- Recommendation: define the state per attempt, or reset it when a run starts after the prune. Make the chip and sheet show attempts newer than the last prune, and add a test case for it.

### What happens when an archived card runs again is undecided - Severity: Major
- Affected scope: §3.5 refusal list; §3.7
- Finding: archive is refused only in DISPATCHED and RUNNING. A card can be archived in READY, AWAITING_INPUT, FAILED, BLOCKED, REVIEW or DEFERRED, and all of those can lead to another run (`taskstate.go:80-94`). The reason the design gives for refusing archive during a turn ("the reply… would land in an archived conversation unseen") applies equally to the next turn. Prune also does not check whether a card is RUNNING before nulling its bodies, so a run in progress writes its reply after the tombstone.
- Recommendation: choose one rule and write it down: either a new `RunStarted` un-archives the card, or archive is allowed only in DONE, CANCELED or FAILED. Make prune refuse or skip a card that is currently DISPATCHED or RUNNING. Add a negative test for each.

### "Open in read mode" on the Archive page has no defined behaviour - Severity: Major
- Affected scope: §3.6 "an **Open** link to the sheet in read mode"; §3.4 last paragraph
- Finding: §3.4 says an archived conversation opened by URL shows only "This conversation is archived — restore it from the Archive page." §3.6 links every archived row to that sheet "in read mode". Nothing defines read mode: no query parameter and no template branch. Two implementers would build two different things.
- Recommendation: either define read mode (for example `?archived=1`, which renders all attempts with no Archive button) or remove the Open link.

### The JS opener as specified would request `undefined`, and the sheet-close mechanism is unspecified - Severity: Major
- Affected scope: §3.4 "JS"
- Finding:
  - The design says only the click branch reads `t.dataset.timingOpen || t.dataset.sheetOpen`. But `openTiming` itself fetches `t.dataset.timingOpen` (`static/app.js:175`), so for a conversation chip it issues a GET to `undefined`.
  - The click delegate's selector (`app.js:40`) must also gain `[data-sheet-open]`. The design does not mention it.
  - "The sheet closes" after Archive is stated without a mechanism. The only close-on-success hook is filtered to `#answerForm` (`app.js:308-315`). It is not said whether the server sends `conversation-modal` back as an out-of-band swap with `hidden`, or whether a JS hook closes it.
- Recommendation: specify the edits line by line: the selector at `:40`, the branch at `:49`, the URL source in `openTiming` at `:175`, and the close mechanism (with the template or JS line that implements it).

### P2's exit check fails as written: the unemitted-names test is not addressed - Severity: Major
- Affected scope: §3.10 "The catalog test's map gains the two names"; §3.9 P2
- Finding: `TestTheUnemittedNamesAreExactlyTheOnesStillOwedToLaterModules` (`events_test.go:86-119`) fails for any catalog name that is neither emitted by `drive`/`driveRunner`/`driveRecovery` nor listed in `owed`. Adding the two names to `catalog` without extending `drive` (`events_test.go:124`) makes P2's `go test -race ./...` fail. The implementer must then choose between emitting the events in `drive` and listing them as owed.
- Recommendation: state that `drive` archives, restores and prunes one card, so that both names are emitted, and that the change does not alter that test's register row.

### The safe-markdown forward spec has no stated expected output - Severity: Major
- Affected scope: §3.3 table; TC-478, TC-479
- Finding: TC-478 asserts "the exact expected HTML", but the design never states it. Several choices are left open, and each changes the output:
  - whether list-item text is wrapped in `<p>`;
  - how `<br>` behaves inside list items;
  - whether `_` inside words (`snake_case`) becomes emphasis;
  - how `*` is told apart from emphasis at the start of a line;
  - what an `<ol>` starting at `3.` or written as `1)` becomes;
  - whether `#heading` with no space counts as a heading;
  - whether emphasis can nest inside link text;
  - which elements get the `md-` classes.

  The rubric calls this case out: a forward specification whose asserted value is not stated.
- Recommendation: give one fixed input and its exact expected HTML for TC-478, and state each rule above in one line.

### `ArchivedConversations` has no enumeration strategy, against the "never rescanned" rule - Severity: Major
- Affected scope: §3.6; §3.2 timing-index extension
- Finding: the Archive page, and the nav count shown on every back-office view (`buildBO` computes every count per request, `handlers_bo.go:183-214`), need every archived card across all products plus every tombstone. The timing index is keyed by task (`timing.go:386-412`), and the code's own comment says "the stream is unbounded, so it is never rescanned". The design does not say how the archived set is found: a full stream scan per request, a new index, or something else.
- Recommendation: define a per-type index for `ConversationArchived` and `ConversationPruned` (or a maintained set) with a stated high-water-mark update, and say which function computes the nav count.

### "Permanently deletes" depends on storage behaviour the design does not verify - Severity: Major
- Affected scope: §5 L4; G4; TC-488
- Finding: L4 says that whether ArcadeDB reclaims the external page "is the database's business". D-043 (`decision-log.html:149-153`) and OQ-17 (`open-questions.html:527-530`) say the opposite: "a deletion promise resting on unverified storage behaviour is not a promise". The UI promises permanent deletion, but TC-488 checks only that the property reads as null.
- Recommendation: cite L-7 and D-043, and either name the verification that destroying an `EXTERNAL` payload frees its data, or change the UI text and G4 to promise only what was verified.

### Phases break the same-commit rules for the register and the event catalog - Severity: Major
- Affected scope: §3.9
- Finding: P1 and P2 add `func Test…` functions, but the register rows land in P4. The QA rule requires the register update in the same commit as the test. BR-05 requires a new event to land in the data-model document, `events.go` and the fold together, yet P2 changes `events.go` and the fold while the data-model document waits for P4. P2's exit check "both stores" also skips silently without `GAOS_ARCADE_TEST_URL`. P3's "rendered locally" exit is a judgement call, not a check.
- Recommendation: either declare the phases as one commit, or move each phase's register rows and data-model rows into that phase. State that the P2 exit requires `GAOS_ARCADE_TEST_URL` to be set (a skip is not a pass). Replace "rendered locally" with named handler-test assertions.

### `EventPayload.Clone` is not extended for the new pointer - Severity: Nit
- Affected scope: §3.5; change list #3
- Finding: `Clone` (`model.go:282-305`) deep-copies every pointer and slice field, and its own comment says a new pointer needs a matching line. `Prune *PruneRecord`, which contains `Runs []string`, is not mentioned.
- Recommendation: add "deep-copy `Prune` and `Prune.Runs` in `Clone`" to change #3.

### Event Kind, Text and actor placement are unspecified - Severity: Nit
- Affected scope: §3.5 event table
- Finding:
  - The pill sentence (`Text`) and the `Kind` (info or warn; warn feeds the attention filter at `questions.go:95`) are not given for either event.
  - The schema already has `Event.actor` (`schema/07-events.sql:26`), yet the design adds `EventPayload.Actor` without saying why the existing property is not used.
- Recommendation: state the exact Text format and Kind for both events, and record one line on the actor choice.

### `conversationStateOf` signature and the product filter disagree - Severity: Nit
- Affected scope: §3.2 vs §3.5
- Finding: §3.2 calls it with `(events, t)` and §3.5 with `(events, taskID)`. Neither `conversationOf` nor `conversationStateOf` says it filters by `DeskID`, which `cardTiming` does (`timing.go:157`).
- Recommendation: fix one signature, and state the `DeskID == product` filter.

### Test-to-file mapping and the ArcadeStore file split are not given - Severity: Nit
- Affected scope: change list #5 and #13; §3.10
- Finding:
  - TC-487 needs a fake ai-manager, and `dashboard/aimanager_admin_test.go` does not exist. TC-488's file is not named.
  - Change #5 lists two ArcadeStore files without saying which method goes in which.
- Recommendation: add a "file" column to the §3.10 table, and split change #5 by method.

### Drifted line anchors - Severity: Nit
- Affected scope: §0, §3.1, §3.5, §3.8
- Finding:
  - `TaskRunRecord` is at `model.go:766`, not 740.
  - `SessionID` is at `:795`.
  - `EventPayload` is at `:215`, not 211.
  - `sessionForLocked` is defined at `turns.go:102`; `runner.go:595` is a call site.
  - `renderBOAfter` is defined at `handlers_bo.go:494`; `handlers_board.go:78` is a call site.
- Recommendation: correct the anchors.

Counts: B=2 M=10 N=5
