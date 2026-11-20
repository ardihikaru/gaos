# Zoom-out review: `docs/designs/conversations.md` (v2)

**Isolation note:** I never opened `.claude/CLAUDE.md`. The harness injected its contents into my context anyway, along with older copies of two rule files from the main checkout. I used none of that: every count and claim below was checked against the worktree's own files.

**Paraphrase:** Each card gets a sheet showing every attempt's prompt, reply, question and answer, with markdown rendered safely on the server. Once a card is done or canceled, its conversation can be archived (a new event type) and restored. The Archive page can prune all archived conversations. Prune empties the stored bodies, deletes the gateway sessions and records a certificate with a SHA-256 per deleted body. Events, run records, context slices and handoffs survive.

**What I verified and found solid:**
- **Arithmetic.** TC-478 to TC-489 are 12 new top-level tests, so 477 + 12 = 489 and 353 + 12 = 365. "Eighty" is at `decision-log.html:18`, so D-082 makes eighty-one. The `.claude/rules/qa-test-plan.md` counts and `check-workspace.sh` are both in row #17 of the change list.
- **Session scope.** `sessionForLocked` (`dashboard/turns.go:102-142`) only resumes runs of the same task. Deleting a card's sessions cannot reach another card.
- **Reuse is real.**
  - The per-task event index is `timingEventsLocked` (`dashboard/timing.go:386`).
  - The type-asserted adapter capability has precedent: `s.adapter.(WorkspaceGateway)` at `dashboard/reviewer_workspace.go:46`, and `MemStore` holds the adapter (`store_mem.go:236`).
  - The `admin()` and `onVendor()` helpers exist (`aimanager_admin.go:45,76`).
  - `Result` already carries same-transaction side writes (`QaCases`, `store.go:181-183`).
- **The hand-written markdown renderer is justified.** The module is stdlib-only by policy (`dashboard/go.mod:1`) and no renderer exists in the repo. `seq.go:37-69` is the only HTML builder, and it uses the same escape-first pattern. A client-side library would move HTML injection into the browser.
- **Archive as two event types plus `convIdx`** is the smallest replayable form. The rejected alternatives in §6 are fairly stated.

---

### Prune certifies two of its three body kinds as deleted while their content stays readable - Severity: Major
- **Affected scope:** §3.7 "Deleted" item 1, `PruneRecord.Bodies`, §5 L1/L2, §3.12 D-082 text.
- **Simplicity rationale:**
  - **Prompt.** The prompt artifact is `RenderPrompt(c)` (`dashboard/adapter_aimanager.go:112-122`). That is `instructionOf` (`:334-345`) applied to the task, design, rules, context and repo parts, plus fixed contract boilerplate. `ContextSlice.rendered` (`store_mem.go:1093-1109`) stores those same part bodies, and §3.7 keeps it, still served by `GET /tasks/{id}/context`.
  - **Question and answer.** `QuestionAsked`/`QuestionAnswered` payloads keep the full question, context, options and answer text (`questions.go:57-63`), not "a summary" as L1 says. They are append-only.
  - **Net effect.** Hashing, emptying and certifying the prompt and question bodies removes no content that was not already in a kept record. Only the reply (minus its handoff block, which is kept) is actually removed. A certificate listing SHA-256 values as "deleted" for text that is still served is the failure L-8 exists to prevent (`brd.html:1186`: what survives must be "disclosed rather than discovered"). Round 1's fix (`reviews/conversations-review-1.md:51`) named the survivors but did not change what is certified.
- **Recommendation:** The operator chooses one:
  - **(a) Smaller.** Prune deletes reply bodies and sessions only. Drop `prompt` and `question` from the delete set and from `Bodies`. The sheet's pruned mode can keep showing the prompt. This removes code and makes the certificate true.
  - **(b) Keep the deletion.** `Kept` and the confirm form must say, verbatim, "the prompt's content, in the context slice" and "the full question and the operator's full answer, in the event stream". L1 must stop calling the answer a summary.

  Either way, D-082's "the prompt, reply and question bodies become empty" must describe what is actually gone.

### The existing timing sheet as the conversation's home was not considered - Severity: Nit
- **Affected scope:** §3.4 "Chip" and "Sheet", §6, change list rows #8, #11, #12.
- **Simplicity rationale:** `timing-modal` already opens per card with the same eyebrow (`needs.html:146`) and an Attempts table (`needs.html:174-181`). §3.4's attempt header repeats the same fields. The design adds a second chip on every card, a new `BoardCard` field, a sisflow chip, a second modal and two more JS id checks, without weighing "a Conversation section or link per attempt in the timing sheet".
- **Recommendation:** Add this to §6 with a one-line reason for rejecting it (for example, "archive must remove something from the card"), or adopt it and drop the chip.

### Deleting gateway sessions is not weighed against leaving them to expire - Severity: Nit
- **Affected scope:** §3.7 step 2 and `SessionJanitor`; §6; R3 and R4.
- **Simplicity rationale:** Session deletion accounts for about half of the prune design:
  - the select/delete/commit split and its 60 s budget;
  - three error sentinels and TC-487;
  - R3, R4 and the use of the over-broad admin key (OQ-29).

  The design itself says sessions "expire in 30 days anyway". It may well be the right call, since the gateway transcript holds tool calls and file contents that GAOS never stores. But §6 never says so, so a reader cannot see why the cost is worth it.
- **Recommendation:** Add one alternative to §6: "leave sessions to the 30-day expiry — rejected because the gateway's transcript holds more than GAOS's bodies, and 'permanently deletes' must cover it".

### Reuse `ErrGatewayTransient`; `admin()` puts the session id into its error strings - Severity: Nit
- **Affected scope:** §3.7 step 2; change-list row #6 ("three sentinel errors").
- **Simplicity rationale:**
  - `ErrGatewayTransient` already exists (`dashboard/aimanager.go:33`), so only `ErrSessionBusy` and `ErrNoAdminKey` are new.
  - `admin()` builds its no-key and unreachable errors with the method and path in the text (`aimanager_admin.go:47,65`). For this call the path contains the session id, which would contradict "errors without the path or id".
- **Recommendation:** Change row #6 to "two new sentinels; reuse `ErrGatewayTransient`". State that `DeleteSession` maps `admin()`'s errors to the sentinels with `errors.Is` and does not wrap them.

### Change list omits the new `Result` field - Severity: Nit
- **Affected scope:** change-list row #4 versus row #5b (`Result.PrunedArtifacts`).
- **Simplicity rationale:** `Result` is defined in `dashboard/store.go:175-184`. Row #4 lists only the Store methods and the `readOnly` refusals.
- **Recommendation:** Add "`Result.PrunedArtifacts` (beside `QaCases`)" to row #4.

### One database round-trip per missing body - Severity: Nit
- **Affected scope:** §3.2 ArcadeStore `Conversation`.
- **Simplicity rationale:** One query per missing id means 2N round-trips each time a sheet opens after a restart. A single `WHERE tenantId = :t AND id IN :ids` is the same amount of code, not an optimisation.
- **Recommendation:** Specify one `IN` query per sheet.

### Line anchors have drifted - Severity: Nit
- **Affected scope:** header line 6; §3.4 JS paragraph.
- **Simplicity rationale:**
  - D-081 is at `decision-log.html:1656`, not `:1655`.
  - `openTiming` is declared at `static/app.js:170`; `:175` is its `htmx.ajax` call.
  - The `afterSwap` id check is at `:324`, not `:312`.
  - The `closeModal` id check is at `:127`, not `:126`.
- **Recommendation:** Re-anchor these four references.

Counts: B=0 M=1 N=6
