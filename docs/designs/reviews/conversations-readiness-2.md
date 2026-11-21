# Readiness review: conversations (v2)

Isolation note: I did not open any off-limits file. However, while I read the rubric, the harness injected the full contents of `.claude/CLAUDE.md` into my context as a system reminder. That file is on the deny-list. Nothing below quotes it or relies on it, but the isolation was breached by the harness, so the author's canary check should treat this run as exposed. A second injected copy of `.claude/rules/qa-test-plan.md` came from the main checkout and is stale (it says TC-465). I used the worktree's copy, which says TC-477.

Document: `docs/designs/conversations.md`

**Overall:** the plan is mostly well anchored, with one undecided API shape in the prune. Most `file:line` claims check out, including:
- `timing.go:138`, `:374`, `:386-411`;
- `questions.go:53-74`, `runner.go:217-249`, `runner.go:322`;
- `store_arcade.go:893-905`, `:1061-1074`, `:1654-1667`;
- `fold.go:378-382`, `broker.go:46-54`;
- `auth_test.go:258`, `events_test.go:17-45`, `:124`;
- `store.go:244-304`, `model.go:215`, `:285`, `:795`, `:798`.

The counts are consistent: 477 + 12 = 489 TCs, 353 + 12 = 365 Go tests, and 12 new `func Test…` for TC-478 to TC-489. D-082 and TC-478 onward are not claimed anywhere else in the repo, and the rollback claim holds (`fold.go:445`).

### Prune's store split and the certificate's digests for bodies not in memory are undecided - Severity: Blocker
- Affected scope: §3.7 "Order" steps 1–3; §4 rows 4, 5b; §3.7 `PruneConversations(ctx, actor string, cards []CardRef)`
- Finding: After a restart, no `prompt`, `reply` or `question` body is in memory (`store_arcade.go:1654-1667`), so on a long-running deployment most bodies to be pruned exist only in the database. The certificate (G5, L-8) needs each deleted body's SHA-256 and length. Step 3 says only "compute digests" and never says which query reads those bodies, under which lock, or in which step.
  - Existence is unknown: whether a `<run>-question` artifact exists for a run is unknowable without a query. A blind `UPDATE` on a missing id changes nothing, yet a naive implementation would still certify a deletion.
  - The established ArcadeStore pattern wraps one whole MemStore method in `mutate` (`store_arcade_blueprint.go:27-29`). That would hold the ArcadeStore lock across the 60 s gateway step. The design instead has ArcadeStore run select, janitor and mutate itself (§4 row 5b), which needs MemStore helpers that are never named or typed.
  - `Result.PrunedArtifacts` (§4 row 5b) does not exist. `Result` is defined at `store.go:175-184`; no type is given for the new field, and row 4 does not list adding it.
  - `CardRef` is undefined.
  - Two engineers would produce different certificates: one digests only in-memory bodies, another reads the database in step 1 or inside `mutate`.
- Recommendation: Write down all of the following.
  1. The MemStore split: `pruneSelectLocked(cards) []pruneTarget` and `pruneCommitLocked(actor, targets, bodies map[id]string, sessions outcome) Result`, with signatures.
  2. ArcadeStore step 1b: one `SELECT id, body FROM Artifact WHERE tenantId=:t AND id=:id` per candidate id not in memory. Run it outside `mutate`; only ids that return a row become targets; the digest is computed from that read.
  3. Re-read inside `mutate`, or not, and say which.
  4. `type CardRef struct{ ProductID, TaskID string }`.
  5. `Result.PrunedArtifacts []string` (the ids to `UPDATE`), added in §4 row 4.
  6. A TC-488 assertion that the certificate's SHA-256 for a body read from the database after a reopen equals the digest of the original text.

### What each mutation answers is undecided - Severity: Major
- Affected scope: §3.4 "Closing after archive/restore"; §3.6; §3.7 handler
- Finding: `renderBOAfter` always rebuilds the `"products"` view (`handlers_bo.go:494-498`), and the design does not pass a `tab`. Restore and Prune are pressed on the Archive page, and re-rendering "products" would navigate the operator away from it. The design also does not say:
  - which view or tab the prune response renders;
  - the exact success toast for archive, restore or prune;
  - the wording of the prune toast that "counts" skipped cards (§3.7). TC-484 asserts the skip but has no string to match.
- Recommendation: Name the response per route. For example:
  - archive from the board answers `renderBOAfter(..., productID, "board")`;
  - restore and prune answer a re-render of `buildBO(r, "archive", "", "")` plus the hidden `conversation-modal`.

  Give the exact toast strings, for example "Pruned {n} conversations; {k} skipped (no longer archived)."

### `PruneRecord.Kept` and the confirm-form text have no stated value - Severity: Major
- Affected scope: §3.5 `Kept []string // fixed list, §3.7`; §3.6 "states what is deleted and what survives (§3.7, verbatim)"; TC-483
- Finding: §3.7's bullets mix prose, code identifiers and `file:line` (`latestHandoffFor`, `store_mem.go:1281`). "Verbatim" cannot be meant literally on a UI form, and the strings stored in `Kept` are not stated. TC-483 asserts `Kept`, so this is a forward specification with no asserted value (audit vector).
- Recommendation: Write the exact `Kept` slice, for example `[]string{"events", "run records", "context slices", "artifact rows", "handoff, design and review artifacts", "this certificate"}`, and the exact form paragraph. TC-483 then asserts equality with that slice.

### Markdown: a fence inside a list item is undecided - Severity: Major
- Affected scope: §3.3 block-rules table (fence row, list nesting row, continuation row)
- Finding: The product owner's own example is "nested bullets, code, inline code". In model replies, code most often appears as a fence indented under a numbered item (`1. Run:` / `   ```bash`). The table does not say:
  - whether a fence indented ≤3 columns closes the open list or nests inside the `<li>`;
  - what happens to a fence indented ≥4 columns under a deeper item. The fence rule refuses it, so the continuation rule would join backtick lines with `<br>`.

  Mixed marker types at the same depth (`- a` then `1. b`) are also undecided. TC-489 keeps the output well formed either way, but two implementations will render the most common reply shape differently.
- Recommendation: Add rows to the table:
  1. A fence whose marker is indented at least as far as the open item's text column belongs to that `<li>`; strip that indent and allow any depth.
  2. Otherwise the fence closes all open lists.
  3. A marker of a different list type at the same depth closes the current list and opens a new one.

  Add one fixed example of each to TC-478.

### Archived state on the board JSON is ambiguous - Severity: Nit
- Affected scope: §3.4 Chip; TC-481 "chip leaves board JSON"
- Finding: `BoardConversation` carries `archived` and `pruned`, which implies the field stays present for an archived card. TC-481 says the chip "leaves board JSON", which implies `nil`. The two readings lead to different JSON and a different assertion.
- Recommendation: State one. For example: "`Conversation` is set for every card with a run; the chip renders only when `!archived && !pruned`; TC-481 asserts `archived: true` in the JSON and no `data-sheet-open` in the HTML."

### `ErrGatewayTransient` already exists, and `admin()` errors carry the path - Severity: Nit
- Affected scope: §3.7 step 2; §4 row 6 "three sentinel errors"
- Finding: `ErrGatewayTransient` is already declared at `dashboard/aimanager.go:33`, so declaring it again in `aimanager_admin.go` will not compile. `admin()` returns errors that embed `method path`, and so the session id, at `aimanager_admin.go:47`, `:59` and `:65`. The no-key case is a plain `fmt.Errorf`, not a sentinel.
- Recommendation: Say "two new sentinels (`ErrSessionBusy`, `ErrNoAdminKey`); reuse `ErrGatewayTransient`". Also say that `DeleteSession` checks `c.Creds.AdminKey == ""` itself and maps every `admin()` error to the bare sentinel, never wrapping it.

### Certificate counting edge cases - Severity: Nit
- Affected scope: §3.4 pruned mode; §3.5 `SessionsNamed`
- Finding: The pruned banner prints one "({bytes} bytes, SHA-256 {first 12 hex})" per attempt, but an attempt has two or three bodies. It is also not stated whether runs with an empty `SessionID` count towards `SessionsNamed`.
- Recommendation: Print one line per `PrunedBody` (kind, bytes, hash). Exclude empty session ids from `SessionsNamed`.

### `drive` needs a terminal card with a run, and the design does not say which - Severity: Nit
- Affected scope: §3.5 (the `drive` addition)
- Finding: `drive` (`events_test.go:124-…`) runs turns on `t-drive` through `superviseRun` without a planned `TaskRecord`. Archive refuses a card with no run or a non-terminal status, and `drive` calls `t.Fatalf` on any error.
- Recommendation: Name the card, for example the first task of the workflow planned at `events_test.go:206`, moved to `CANCELED` with a recorded run, or state the setup.

### Rule-file and validator values not written out - Severity: Nit
- Affected scope: §3.10 counts; §4 row 17
- Finding: The design gives the totals but not every string that must change:
  - `.claude/rules/qa-test-plan.md`: "highest TC-477 / next TC-478", "477 rows… 331 in `dashboard/`… 353", and the range-map row `TC-453…TC-477`;
  - `deploy/check-workspace.sh:112-129`: "three hundred and fifty-three", `353`, "four hundred and seventy-seven", "Eighty entries".
- Recommendation: List the new values: TC-489 and TC-490, 489 rows, 343 in `dashboard/`, 365 Go tests, `TC-453…TC-489`, "three hundred and sixty-five", "four hundred and eighty-nine", "Eighty-one entries".

### Stale line anchors - Severity: Nit
- Affected scope: §3.4 JS; header line 6
- Finding:
  - `afterSwap` is at `app.js:321`, not `:312`.
  - `closeModal` starts at `:117`; its id check is at `:127`, not `:126`.
  - `openTiming` starts at `:170`; `:175` is its `htmx.ajax` line.
  - D-081's row is at `decision-log.html:1656`, not `:1655`.
- Recommendation: Correct the anchors.

### One-commit phasing has no checkpoints - Severity: Nit
- Affected scope: §3.9
- Finding: The single commit is justified (QA rule, BR-05). But the five internal stages have no exit condition, so a failure is only found at the end.
- Recommendation: Give each stage a `go test -run` exit check. For example:
  - `-run 'TestMarkdown'` after markdown;
  - `-run 'TestEveryEmittedEvent|TestTheUnemittedNames'` after the catalog stage;
  - `-run 'TestStore'` after the stores;
  - `-run 'TestEveryRouteIsClassified|TestTheConversation'` after the routes.

### Doc-only changes have no verification line - Severity: Nit
- Affected scope: §4 rows 14–16
- Finding: The data-model §08 rows, `graph.html`, the BR-41 text, and the D-012 and J-6 cross-references are checked by nothing more than `pustaka check`. That check verifies structure, not content. The catalog test's map is hand-written (`events_test.go:10-17`) and does not read the document.
- Recommendation: Add a `grep -c` line per file with its expected result, for example `grep -c "ConversationPruned" artifacts/v1/gaos-arcadedb-model.html` ≥ 2 and `grep -c "D-082" pustaka/docs/product/brd.html` ≥ 2.

Sections that are solid:
- Glossary (§0) and scope (§3.1): accurate.
- Event table and payload (§3.5): decided.
- Routes table (§3.8): matches `TestEveryRouteIsClassified`.
- Test table (§3.10): complete, and the IDs are allocated once.
- Rollback (§10): verified against `fold.go:445`.

Counts: B=1 M=3 N=8
