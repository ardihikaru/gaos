# Conversations: every attempt's prompt and reply on its card, with archive and prune

**Status:** Approved (v3). Round 1 reviewed v1 (`reviews/conversations-*-1.md`, triage `reviews/conversations-review-1.md`); round 2 reviewed v2 (`reviews/conversations-*-2.md`, triage `reviews/conversations-review-2.md`). Every Blocker of both rounds is fixed in this version; no Critical or High security finding is open (round 2 raised none). Approval in §11.
**Feature:** `conversations`
**Version target:** v0.42.0
**Decision number claimed:** D-082 (§3.12). D-081 is the newest row (`pustaka/docs/product/decision-log.html:1656`), eighty entries (`:18`); D-082 makes eighty-one.
**Registers this changes:** **BR-41 amended** in place (§3.11). **D-012** (`decision-log.html:116`) and BRD **J-6** (`pustaka/docs/product/brd.html:985`) gain a cross-reference to the one exception. **BR-05**: the catalog opens by two names (§3.5).
**Rules this must obey:** BR-02 (every change emits an event, `brd.html:1226`), BR-04 (append-only), BR-22 (one run record per attempt), BR-32 (egress; its record, the context slice, is kept), L-7 / L-8 / L-9 (deletion destroys the payload, leaves a certificate of what went and what survived, and binds backups, `brd.html:1185-1187`), D-043 (EXTERNAL destroyability, `decision-log.html:141-155`), OQ-29 (the admin key is over-broad, `pustaka/docs/product/open-questions.html:733-738`).
**Security lens:** required and run in both rounds.

## 0. Glossary

- **Card / task** — one `TaskRecord` (`dashboard/model.go:685-714`). Task ids are qualified by workflow (`dashboard/store_mem.go:2083-2091`).
- **Attempt / run** — one `TaskRunRecord` per attempt (`model.go:766-802`); `SessionID` and `Provider` (`model.go:795,798`) name the gateway session. **Turn** means only a live model call.
- **Terminal** — `DONE` or `CANCELED`, no exits (`dashboard/taskstate.go:87,89`); helper `terminal()` (`dashboard/timing.go:138`).
- **Artifact** — `ArtifactRecord` (`model.go:889-899`). After every attempt the runner records `<run>-prompt` and `<run>-reply` when non-empty (`dashboard/runner.go:217-228`), `<run>-handoff` (`:229-238`), and `<run>-question` when the turn asked (`:241-249`). Persisted as `Artifact` vertices keyed `(tenantId, id) UNIQUE` (`dashboard/schema/09-indexes.sql:42`), upserted from memory when changed (`dashboard/store_arcade.go:893-905`); `body` is `EXTERNAL` (`dashboard/schema/10-storage.sql:48`).
- **Context slice** — the frozen packet an attempt was given after egress (BR-32), `ContextSlice.rendered` (`model.go:843-868`, written by `recordSlice`, `store_mem.go:1093-1109`), served by `GET /tasks/{id}/context` (`dashboard/main.go:354`). The prompt is `RenderPrompt` over the same parts plus fixed contract text (`dashboard/adapter_aimanager.go:112-122`), so **the prompt's content is also in the slice**.
- **Question / answer** — `QuestionAsked` / `QuestionAnswered` carry the full question, context, options and the operator's full answer (`dashboard/questions.go:53-64`, `:204-207`); their `Event.Text` carries the question and, for an answer in the operator's own words, that text verbatim (`runner.go:322`, `questions.go:278`). The product Timeline prints `Event.Text` (`dashboard/templates/backoffice.html:430`).
- **Projection** — `ArcadeStore` keeps a `MemStore` (`s.proj`) and answers reads from it (`dashboard/store_arcade_blueprint.go:33-35`); its boot load reads only `handoff`, `design`, `review` artifacts (`store_arcade.go:1654-1667`).
- **Sisflow board** — the vendored client board over the server board (`dashboard/static/board.js`, `dashboard/board.go:20`).
- **Gateway** — ai-manager; admin bearer `GAOS_AIM_ADMIN_KEY_FILE` (`dashboard/aimanager_admin.go:45-71`). **External contract** (not in this repository; read at ai-manager commit `ef26c8eb`, 2026-11-13, `internal/server/handlers_sessions.go:11-34` and `docs/claude-sessions.md` "Maintenance"): `DELETE /admin/sessions/{id}` proxies to the session worker — `ai-claude-cli` by default, `ai-antigravity-cli` with `?provider=antigravity`, "one shared store for every account" (the pool fan-out in `onVendor`'s comment, `aimanager_admin.go:72-75`, is about workspace routes, not this one); `2xx` deleted, `404` not found, `409` in use; session ids are UUIDs; transcripts live in a bucket that expires objects after 30 days. There is no endpoint to read a transcript. The gateway also keeps each question record (30 days, `dashboard/aimanager_questions.go:12-17`) and each turn's result by request id (`dashboard/turns.go:80-83`); session deletion is not documented to remove either.
- **Back office (BO)** — every route session-gated by `gate` (`dashboard/auth.go:523-555`), the session HMAC-verified (`auth.go:315-382`); a non-GET carrying `Sec-Fetch-Site: cross-site` or a foreign `Origin` is refused (`auth.go:557-577`); cookie `SameSite=Lax` (`auth.go:493`). Replay handlers get `readOnly` (`main.go:112-118`, `dashboard/store.go:244-304`).

## 1. Problem

The product owner asked for the AI conversation of every card, readable as written (markdown with nested bullets, code, inline code), an **archive** button, an **archive page**, and in it **prune all archived**, "which permanently deletes them".

1. No conversation UI exists: prompt, reply and question artifacts are written (`runner.go:217-249`) and read by nothing.
2. After a restart they are only in the database (`store_arcade.go:1654-1667`).
3. Nothing deletes them, and BR-41 says they stay readable.

## 2. Goals and non-goals

**Goals**

- G1. A **conversation sheet** per card: every attempt in order — what GAOS sent, what the AI replied, the question it asked, the operator's answer.
- G2. **Safe markdown** (§3.3).
- G3. **Archive** a finished card's conversation, and **restore** it. An archived conversation's chip disappears from the card; it stays readable from the Archive page.
- G4. An **Archive page**: archived conversations (open read-only, restore), certificates, and **Prune all archived** behind a typed confirmation bound to the listed cards.
- G5. **Prune** empties the stored prompt, reply and question documents of those cards and deletes their gateway sessions, and leaves a certificate per card (L-8) naming the account, the time, each emptied document's SHA-256 and length, the session outcome, and — in plain words — what survives and where (§3.7).

**Non-goals**

- N1. No raw-transcript view (no gateway read endpoint).
- N2. Prune does not touch events, run records, context slices, artifact rows, or `handoff` / `design` / `review` artifacts (§3.7 says what that leaves readable).
- N3. No archive of unfinished cards; no per-attempt archive; no QA-tool questions.
- N4. No schema statement.
- N5. A full `script-src` CSP is not in this change (§9 R5); the headers that need no template audit are (§3.8).
- N6. No deploy, no VM change.

## 3. Design

### 3.1 Scope: the card, finished, with no open run

A card's attempts share a gateway session (`answerResumesLocked`, `questions.go:261`; `sessionForLocked`, `dashboard/turns.go:102`). The operator sees a card, so archive, restore and prune act on a card. **Archive is allowed only when the card is terminal and none of its runs is `STARTED` or `RUNNING`** (a card cancelled while a turn was still being supervised would otherwise receive that turn's bodies later, `runner.go:220-228`, `turns.go:171-178`). Such a card never gains an attempt, so a pruned card stays pruned.

### 3.2 The conversation, as data

New `dashboard/conversation.go`:

```go
type CardRef struct{ ProductID, TaskID string }

type Conversation struct {
	ProductID, ProductName, TaskID, TaskTitle, Status string
	State    ConversationState
	Attempts []ConversationAttempt // ascending Attempt
}
type ConversationState struct {
	Archived   bool;  ArchivedAt int64; ArchivedBy string
	Pruned     bool;  PrunedAt   int64; PrunedBy   string
	Bodies     *PruneRecord // the bodies certificate, when Pruned
	Sessions   *PruneRecord // the sessions certificate, when recorded
}
type ConversationAttempt struct {
	Attempt int; RunID, Status, Model, Provider string; StartedAt, EndedAt int64
	Prompt, Reply string      // bodies; "" when absent or pruned
	Question *QuestionPayload // from QuestionAsked for this run; answer filled from QuestionAnswered
}
```

`conversationOf(t TaskRecord, runs []TaskRunRecord, arts map[string]ArtifactRecord, events []Event) Conversation` — pure; counts only events with `DeskID == t.DeskID`; per run of `t` by `Attempt`: bodies from `arts[run+"-prompt"]`, `arts[run+"-reply"]`; the question from the `QuestionAsked` with `Payload.Task == t.ID` and `Payload.Question.RunID == run`; the answer from the `QuestionAnswered` with the same `Question.ID`. `conversationStateOf(t TaskRecord, events []Event) ConversationState` — same filter; walk by `Seq`: `ConversationArchived` with `To == "archived"` sets archived (at `Event.At`, by `Payload.Actor`), `To == "active"` clears it; `ConversationPruned` with `Prune.Stage == "bodies"` sets pruned and clears archived; with `Stage == "sessions"` attaches the session outcome.

`Store.Conversation(ctx, productID, taskID) (Conversation, error)`:
- **MemStore** — under `s.mu.RLock()`: the task (`ErrNotFound` if absent or another product), `runsFor`, `s.artifacts`, events from the per-task index (renamed `taskEventsLocked`, widened to `QuestionAsked`, `QuestionAnswered`, `ConversationArchived`, `ConversationPruned` by `Payload.Task`; `cardTiming` ignores other types, `timing.go:161-165`).
- **ArcadeStore** — the projection's answer; then, if not pruned and any prompt or reply is missing, **one** query, not holding ArcadeStore's `s.mu`: `SELECT id, body FROM Artifact WHERE tenantId = :t AND id IN :ids` with a bound list of the missing `<run>-prompt` / `<run>-reply` ids (a bound list is accepted by ArcadeDB 26.7.2, measured on the scratch image).

### 3.3 Safe markdown

`dashboard/markdown.go`, `renderMarkdown(src string) template.HTML`, stdlib only. Escape, then structure: every text run through `template.HTMLEscapeString` (the function `.claude/rules/frontend.md` names) before any tag is written; tags are Go literals from the allowlist; `template.HTML` is built only here.

Block rules (line based; "indent" counts a tab as 4):

| Input | Output |
|---|---|
| fence: ```` ``` ```` or `~~~` (≥3) opens; the same marker closes; unclosed runs to the end | `<pre class="md-code"><code>` + escaped lines joined by `\n` + `</code></pre>`; info string dropped; no inline rules inside |
| a fence whose marker is indented at least to the open list item's text column | the fence belongs to that `<li>` (that indent is stripped from its lines) |
| a fence indented less than every open item's text column | closes all open lists, then opens at top level (if indented ≤ 3) |
| `#`…`######` + space + text | `<h4>` (1–3), `<h5>` (4), `<h6>` (5–6); `#text` is a paragraph |
| `-`/`*`/`+` + space; digits + `.`/`)` + space | `<ul>`/`<ol>` `<li>`; `<ol start="n">` when n ≠ 1; item text not wrapped in `<p>` |
| nesting | a marker indented ≥ 2 more than its parent's marker is a child; less, a sibling of the deepest ancestor whose marker indent is ≤ it |
| a marker of the other list type at the same depth | closes that list, opens a new one |
| an indented non-marker line under an item | continues it, joined with `<br>` |
| `> ` | `<blockquote>` (one paragraph) |
| `---` / `***` / `___` alone | `<hr>` |
| blank line | ends paragraphs and lists |
| other lines | `<p>`, lines joined with `<br>` |
| tables, HTML, images | escaped text |

Inline rules, one pass per line: `` `x` `` → `<code>`; `**x**`/`__x__` → `<strong>`; `*x*`/`_x_` → `<em>` (`_` opens only at a word start and closes only at a word end; `*` opens only before a non-space); no emphasis inside code. `[text](url)` → a link **only** for an `https` URL whose host is ASCII: `<a href="…" rel="noopener noreferrer nofollow" target="_blank">text</a> <span class="md-host">(host)</span>`, `href` and host escaped; any other URL, a non-ASCII host, or a `[` inside the text → literal text. Bidi controls (U+202A–U+202E, U+2066–U+2069) and zero-width characters (U+200B–U+200D, U+FEFF) are removed from link text and hosts. `![…](…)` is text. **Linear:** each delimiter kind looks for its closer at most once per line and, after a miss, is literal for the rest of that line. Over 512 KiB: one escaped `<pre class="md-code">`.

Output is wrapped in `<div class="md">`. **Invariant** (TC-489): only `div p br h4 h5 h6 ul ol li pre code strong em a span blockquote hr`; balanced; no `<` outside a tag.

**TC-478's fixed example.** Input:

```
## Plan
1. Read `x`
   - a **b**
     - deep _c_ and snake_case_name
2. Run:
   ```sh
   echo "<hi>"
   ```
3. Done: [site](https://example.com/p)
```

Exact output (one line):

`<div class="md"><h4>Plan</h4><ol><li>Read <code>x</code><ul><li>a <strong>b</strong><ul><li>deep <em>c</em> and snake_case_name</li></ul></li></ul></li><li>Run:<pre class="md-code"><code>echo &#34;&lt;hi&gt;&#34;</code></pre></li><li>Done: <a href="https://example.com/p" rel="noopener noreferrer nofollow" target="_blank">site</a> <span class="md-host">(example.com)</span></li></ol></div>`

### 3.4 The chip and the sheet

**Board JSON.** `BoardCard` gains `Conversation *BoardConversation \`json:"conversation,omitempty"\`` = `{attempts, archived, pruned}`, set for every card with a run. **Chip** (server board and sisflow): `<button type="button" class="cv-chip mono" data-sheet-open="{conversationURL}">{n} attempt(s)</button>` only when `!archived && !pruned`. `conversationURL(productID, taskID)` beside `timingURL` (`timing.go:374`), path-escaped; `encodeURIComponent` in `board.js`.

**Sheet.** `GET /ui/conversation/{id}/{taskId}` (`Cache-Control: no-store`) → template `conversation-modal`, `View.Talk *ConversationView` (the `Conversation` plus rendered HTML per body):

```html
<div class="modal-back modal-anim" id="conversationModal" {{if not .Talk}}hidden{{end}} hx-swap-oob="true">
```

included in `page.html` beside `timing-modal`. Modes:
- **active** — eyebrow `{product} · {task} · {status}`, title, attempts; **Archive** when §3.1 allows, else "Archive is available when the card is done or canceled and no attempt is running.";
- **archived** — banner "Archived {when} by {who}", attempts read-only, **Restore**;
- **pruned** — banner "Pruned {when} by {who}", each attempt's header, then per emptied document one line "{kind}: {bytes} bytes emptied, SHA-256 {first 12 hex}", then the survivors paragraph (§3.7).

Attempt: header `Attempt n · {model} · {provider} · {status} · {started UTC}`; **GAOS sent** (closed `<details>`, rendered prompt); **AI replied** (rendered reply, or "No reply was recorded."); **Asked you** (escaped question, context, options) and **You answered** `{option label | in your own words}: {text}` + time, or "Not answered yet".

**JS** (`static/app.js`): the click selector (`:40`) gains `[data-sheet-open]`; the branch at `:49` routes it to `openTiming` (`:170`), whose `htmx.ajax` (`:175`) reads `t.dataset.timingOpen || t.dataset.sheetOpen`; `closeModal`'s id check (`:127`) and `afterSwap`'s (`:321`) gain `conversationModal`.

### 3.5 Two new event types (BR-05)

Landing in one commit with `dashboard/events.go`, `dashboard/fold.go` (decided no-ops beside `QuestionAsked`, `fold.go:378-382`), the catalog test (`dashboard/events_test.go:17-45`), the driver `drive` (`events_test.go:124`, which at its end gives the cancelled board card one recorded run and then archives, restores, archives and prunes it, so both names are emitted and `TestTheUnemittedNamesAreExactlyTheOnesStillOwedToLaterModules` stays unchanged), `dashboard/broker.go` (`sseName` → `"task"`, `:46-54`), the data-model document (`artifacts/v1/gaos-arcadedb-model.html:768` and a table beside `:1405-1413`) and `pustaka/docs/architecture/graph.html`.

| Event | Group | Payload | `Text` | `Kind` |
|---|---|---|---|---|
| `ConversationArchived` | execution | `Task`, `To` = `archived`\|`active`, `Actor` | `{task}: conversation archived by {actor}` / `{task}: conversation restored by {actor}` | info |
| `ConversationPruned` | execution | `Task`, `Actor`, `Prune` | stage bodies: `{task}: conversation pruned by {actor} — {n} documents emptied`; stage sessions: `{task}: {d} of {named} gateway sessions deleted, {g} already gone, {f} failed, {s} kept (shared)` | info |

`To` already means "the state reached" (`model.go:223`). `EventPayload` (`model.go:215`) gains `Actor string`, `Prune *PruneRecord`; `Clone` (`model.go:285`) deep-copies `Prune` and its slices. `writeEvent` binds `actor = :actor` with `def(e.Payload.Actor, "operator")` in place of the literal (`store_arcade.go:1066-1069`). `{actor}` in `Text` is `operator` when empty.

```go
type PruneRecord struct {
	Stage           string       // "bodies" | "sessions"
	Runs            []string     // bodies: the attempts emptied
	Bodies          []PrunedBody // bodies: one per emptied document
	Kept            []string     // bodies: what survives, the fixed list below
	SessionsNamed   int          // sessions: distinct non-empty ids its runs name
	SessionsDeleted int          // 2xx
	SessionsGone    int          // 404
	SessionsFailed  int          // any other answer, no gateway, no admin key, a malformed id, an unsupported vendor
	SessionsShared  int          // also named by a run of a card outside this prune: kept
}
type PrunedBody struct{ ID, Kind, SHA256 string; Bytes int }
```

`Kept` is exactly:
`[]string{"events, including the full question and answer", "run records", "context slices, including the prompt's packet content", "artifact rows (id, kind, title, run, attempt)", "handoff, design and review artifacts", "gateway question records and turn results, until the gateway expires them", "database backups and exports taken before this prune", "this certificate"}`.

No session id is written into a certificate (the stream already carries ids in `SessionOpened`/`SessionLost`, `turns.go:152-160`; the certificate is about content).

`ArchiveConversation(ctx, productID, taskID, actor string, archive bool) (Result, error)`: (1) unknown card → `ErrNotFound` (404); then `reject` (422): (2) no run → `{task} has no conversation yet.`; (3) not terminal or a run open → `{task} is {status}; a conversation is archived when its card is done or canceled and no attempt is running.`; (4) pruned → `{task}'s conversation was pruned; there is nothing to archive or restore.`; (5) `{task}'s conversation is already archived.` / `{task}'s conversation is not archived.` Then one `ConversationArchived`; toast `Conversation archived — find it on the Archive page.` / `Conversation restored to {task}.` ArcadeStore: `mutate`. `readOnly` refuses it.

### 3.6 The Archive page

BO view `archive` via `GET /ui/bo/{view}` (`handlers_bo.go:141-149`, `buildBO` `:186`), `Cache-Control: no-store`; nav button **Archive** after **Agents** (`backoffice.html:92`) with the archived count. The per-task index also keeps `convIdx []int` (positions of every `ConversationArchived` / `ConversationPruned`) on the same high-water mark; `Store.ArchivedConversations(ctx) (ArchiveView, error)` folds it into `Archived []ArchivedRow{ProductID, ProductName, TaskID, TaskTitle, Attempts, ArchivedAt, ArchivedBy}` and `Pruned []PruneRow{ProductID, TaskID, TaskTitle, PrunedAt, PrunedBy, Bodies, Sessions *PruneRecord}`, newest first; `buildBO` uses `len(Archived)` for the count.

Page: archived table (product, card, title, attempts, archived when/by, **Open** (archived mode), **Restore**); **Prune all archived** — a button revealing (`data-toggle`, `static/app.js:54-62`) a form with one hidden `card` per listed row (`{productID}/{taskID}`), the text below, a field "Type PRUNE to confirm", and **Prune {n} conversations**; then the certificates table (when, by, card, documents and bytes, sessions deleted/gone/failed/shared, **Open** (pruned mode)). Empty states: "No archived conversations." / "Nothing has been pruned."

Form text, exactly: *"Prune empties the stored prompt, reply and question documents of these conversations and deletes their gateway sessions. It cannot be undone. What survives: the events — including each question and your full answer, which stay on the product Timeline; the context slices — the packet each prompt was built from; the run records; the handoff, design and review documents; the gateway's own question records and turn results until it expires them; and any backup taken before now. Each card gets a certificate of what was emptied."*

### 3.7 Prune

`POST /conversations/prune` (form `confirm`, `card`…). The handler answers 422 `Type PRUNE to confirm; nothing was deleted.` unless `confirm == "PRUNE"`, and 422 `Nothing to prune.` with no `card`. Then `Store.PruneConversations(ctx, actor string, cards []CardRef) (Result, error)`; the response re-renders the Archive view (`buildBO(r, "archive", "", "")`) with the toast `Pruned {n} conversation(s); {k} skipped (no longer archived).` — or, when n = 0, `Nothing was pruned; {k} skipped (no longer archived).`

**Order.** MemStore's `s.mu` is "the lock".

1. **Select** (read lock): for each listed card still archived and not pruned → a `pruneTarget{Card, Runs, ArtifactIDs, Sessions []sessionRef{Vendor, ID}}`, with the candidate ids `<run>-prompt`, `<run>-reply`, `<run>-question`, and its sessions minus any id also named by a run of a task not in the selection (those are counted `SessionsShared`). `func (s *MemStore) pruneSelectLocked(cards []CardRef) []pruneTarget`.
2. **Read bodies** (no lock): bodies in memory are taken from `s.artifacts`; ArcadeStore reads the rest with one bound-list query `SELECT id, body FROM Artifact WHERE tenantId = :t AND id IN :ids`. Only ids that exist become documents to empty; their SHA-256 and length are computed from this read.
3. **Commit** (write lock, held through the database write): `func (s *MemStore) pruneCommitLocked(actor string, targets []pruneTarget, bodies map[string]string, commit func(evs []Event, emptied []string) error) Result`. Under the lock it re-checks each target (still archived, not pruned, same runs; else skipped), builds one `ConversationPruned` (stage `bodies`) per remaining card with sequence numbers `s.seq+1…` **without appending them yet**, and calls `commit`. ArcadeStore's `commit` writes, in **one transaction** with a 30 s budget, the events and `UPDATE Artifact SET body = '' WHERE tenantId = :t AND id = :id` per emptied id. Only if `commit` returns nil does MemStore append the events (advancing `s.seq`, publishing), set those bodies to `""` in `s.artifacts`, and return them in the `Result`. If it fails, memory is unchanged and the error is returned (toast). MemStore alone passes a `commit` that returns nil. The board and sheets wait on the lock for at most the budget; this is an operator action, not a hot path.
4. **Delete sessions** (no lock), only for committed cards: one call at a time, 10 s each, 60 s for the step (the rest counted failed). Before a call: an id that is not a UUID → failed, not sent; a vendor other than `claude` or `antigravity` → failed, not sent.
5. **Record sessions**: one `ConversationPruned` (stage `sessions`) per committed card, through `mutate` (ArcadeStore) / directly (MemStore). If this write fails, the deletions are logged as counts only (`prune: sessions outcome not recorded for {n} cards`) and the cards' sheets say "Session outcome not recorded".

Failure per step: 1–2 change nothing. 3 commits bodies and certificates together or not at all, in memory and in the database. 4 deletes only sessions of cards already certified as pruned. 5 is the only unrecorded window, and it records counts in the log.

`SessionJanitor` (type-asserted capability, like `WorkspaceGateway`, `dashboard/workspace.go:99-112`):

```go
type SessionJanitor interface {
	DeleteSession(ctx context.Context, vendor, sessionID string) error
}
var ErrSessionBusy = errors.New("ai-manager: the session is in use (409)")
var ErrSessionGone = errors.New("ai-manager: no such session (404)")
var ErrNoAdminKey  = errors.New("ai-manager: no admin key")
```

`*AIMAdapter.DeleteSession` → `AIMClient.DeleteSession`: returns `ErrNoAdminKey` when `Creds.AdminKey == ""`; calls `c.admin(ctx, http.MethodDelete, "/admin/sessions/"+url.PathEscape(id)+onVendor(vendor), nil)` (`aimanager_admin.go:45,76`); maps `2xx` → nil, `404` → `ErrSessionGone`, `409` → `ErrSessionBusy`, anything else or a transport error → the existing `ErrGatewayTransient` (`dashboard/aimanager.go:33`); never wraps `admin()`'s error (it names the path, `aimanager_admin.go:47,59,65`). Prune counts `ErrSessionGone` as `SessionsGone`, the others as failed, and logs nothing per session.

**What prune leaves readable** (the honest part, repeated in the form, the certificate and D-082): the prompt's packet content in the context slice; the question and the operator's full answer in the events and on the Timeline; the handoff, design and review blocks; the gateway's question records and turn results until it expires them; backups (L-9). What is actually gone from GAOS is the prompt and reply **documents** — in practice the reply's prose outside its handoff, design and review blocks — and, from the gateway, the session transcripts with their tool calls. That is what the product owner's ruling, as relayed (§11), names.

**Storage.** The body becomes `''` in the database. D-043 says EXTERNAL destroyability is verified in the scratch pass (`decision-log.html:141-155`); `dashboard/schema/RECONCILIATION.md:241` records only that `EXTERNAL` was applied. This change does not re-verify page reclamation; D-082 records that the prune empties the property and claims nothing about the disk pages, and the UI says "empties the stored … documents".

### 3.8 Routes, guards and headers

| Route | Class | Guard |
|---|---|---|
| `GET /ui/conversation/{id}/{taskId}` | BO read | `gate`; `readOnlyGET` (`dashboard/auth_test.go:258`); `no-store` |
| `POST /products/{id}/board/cards/{taskId}/conversation/archive` | BO mutation | `gate`; `crossOriginOK`; `SameSite=Lax`; `s.mut(r)` |
| `POST /products/{id}/board/cards/{taskId}/conversation/restore` | BO mutation | same |
| `POST /conversations/prune` | BO destructive | same, plus `confirm=PRUNE` and the listed `card` set |
| `GET /ui/bo/archive` | existing | existing; `no-store` |

Responses: archive (from the sheet over the board) → `renderBOAfter(w, r, res, productID, "board")` (`handlers_bo.go:494`) plus the hidden `conversation-modal`; restore (from the Archive page) and prune → the Archive view plus the hidden modal. Actor: `ss, _ := sessionOf(r); ss.Email` (`auth.go:355`). Authorisation is one trust domain: any signed-in account may archive, restore and prune; the certificate names it.

Every back-office response (in `gate`, for non-bearer classes) gains `Content-Security-Policy: frame-ancestors 'none'; object-src 'none'; base-uri 'self'; form-action 'self'` — `base-uri 'self'` because `page.html:25` sets `<base href>`. `script-src` is left for §9 R5.

### 3.9 Building it — one commit

Code, register rows, data-model rows, BRD and decision log land in one commit (QA rule; BR-05). Checkpoints while building: `go test -run TestMarkdown ./...`; then `-run 'TestEveryEmittedEvent|TestTheUnemittedNames'`; then `-run 'TestStoreConformance|TestConversation|TestPrune|TestArchive'` **with `GAOS_ARCADE_TEST_URL` set**; then `-run 'TestEveryRouteIsClassified|TestTheConversationRoutes'`. Exit: `go vet ./... && go test -race ./...` with the ArcadeDB URL set; `pustaka check` and tests; `check-workspace.sh`; `check-stacks.sh`; and content greps: `grep -c ConversationPruned artifacts/v1/gaos-arcadedb-model.html pustaka/docs/architecture/graph.html` ≥ 1 each, `grep -c D-082 pustaka/docs/product/brd.html` ≥ 2. A headless-Chrome render is reported too.

### 3.10 Tests

| TC | Test (file) | Kind | Asserts |
|---|---|---|---|
| TC-478 | `TestMarkdownRendersNestedListsCodeAndInlineCode` (`markdown_test.go`) | Positive | §3.3's example → its exact output; `3.` → `<ol start="3">`; `- a` then `1. b` → two lists |
| TC-479 | `TestMarkdownEscapesEverythingTheModelWrote` (`markdown_test.go`) | Negative | `<script>`, `<img onerror>`, `"` in a link, `javascript:`, `http:`, `data:`, relative, non-ASCII host, bidi override, `[` in link text, `![x](https://x)`, HTML in a fence and in code → escaped or text; 500 KiB of `*`, `[`, `` ` ``, `_` in under 2 s; 600 KiB → one `<pre>` |
| TC-480 | `TestTheConversationListsEveryAttemptWithItsQuestionAndAnswer` (`conversation_test.go`) | Positive | two attempts in order with prompt, reply, question, answer; another product's events ignored |
| TC-481 | `TestArchiveHidesTheChipAndRestoreBringsItBack` (`conversation_test.go`) | Positive | events with actor and `To`; board JSON `conversation.archived: true` and no `data-sheet-open` for that card in the server board; the Archive page lists it; archived mode shows attempts and Restore; restore returns the chip |
| TC-482 | `TestArchiveIsRefusedForAnUnfinishedCardOrTwice` (`conversation_test.go`) | Negative | READY, AWAITING_INPUT, CANCELED with a `RUNNING` run → the §3.5 (3) text; already archived; restore of an active one; no run; replay handle |
| TC-483 | `TestPruneEmptiesDocumentsAndSessionsAndLeavesACertificate` (`conversation_test.go`) | Positive | prompt/reply/question `""`; handoff, context slice, events, runs unchanged; the fake janitor got each session once; a `bodies` certificate with actor, runs, per-document SHA-256 and length equal to the originals', `Kept` equal to §3.5's list; a `sessions` certificate with the counts; no session id in the result, events or toast |
| TC-484 | `TestPruneSkipsWhatChangedAndKeepsWhatIsShared` (`conversation_test.go`) | Negative | a card restored after the page rendered → skipped, toast `… 1 skipped …`; a card archived after it → untouched; a session also named by another card's run → kept, `SessionsShared` 1; `ErrSessionBusy` → failed, no id in any string; a `commit` that fails → no event, bodies intact, sheet not pruned |
| TC-485 | `TestPruneWithoutTheTypedConfirmationDeletesNothing` (`conversation_test.go`) | Negative | missing / wrong `confirm`, or no `card` → 422 with the exact toasts; no event; bodies intact |
| TC-486 | `TestTheConversationRoutesAreBackOfficeAndSameOrigin` (`conversation_test.go`) | Negative | `Sec-Fetch-Site: cross-site` and a foreign `Origin` on archive/restore/prune → 403; unknown card → 404 toast; sheet and Archive view `no-store`; a BO response carries the CSP header |
| TC-487 | `TestTheGatewayDeletesASessionAndSaysOnlyWhatFailed` (`aimanager_admin_test.go`, new) | Positive / Negative | fake ai-manager: method, path, `?provider=antigravity`, admin bearer; 200 → nil; 404 → `ErrSessionGone`; 409 → `ErrSessionBusy`; 500 → `ErrGatewayTransient`; no key → `ErrNoAdminKey`; no error string contains the id; a non-UUID id is never sent (prune-level) |
| TC-488 | `TestAPrunedConversationStaysPrunedAfterARestart` (`conversation_test.go`, ArcadeDB) | Positive | after a reopen the reply is read from the database; archive, prune, reopen: bodies `''` in the database, both certificates and the question events present, the certificate's SHA-256 equals the original text's, the `actor` column is the account, state pruned |
| TC-489 | `TestMarkdownOutputIsWellFormedForAnyInput` (`markdown_test.go`) | Negative | 2 000 seeded random inputs: only allowlisted tags, balanced, no raw `<` |

Plus `storeContract` case `ConversationArchiveRoundTrip` (TC-124/125); `caseReadOnlyHandleRefusesEveryMutation` gains the two mutations; catalog map and `drive` gain the two names; `taskfold_test.go`'s `foldedBoard` clears `Conversation` like `Timing`.

**Counts and every string that moves:** 477 → **489** rows ("four hundred and eighty-nine"); Go tests 353 → **365** ("three hundred and sixty-five"), `dashboard/` 331 → 343; highest TC-489, next TC-490; range map `TC-453…TC-489`; "Eighty entries" → "Eighty-one entries" (page, ToC, `check-workspace.sh:129`, the rule file). BRD rule count unchanged.

### 3.11 The BR-41 amendment

BR-41 (`brd.html:1300`) gains: "… and remain readable after the session expires, **until an operator prunes the archived conversation of a finished card (D-082). The prune empties the prompt, reply and question documents, deletes the gateway sessions, keeps the context slice (BR-32) and the events (BR-04), and leaves a certificate of what was emptied and what survives (L-8).**" D-012's row and J-6 gain one line each: "Exception: D-082."

### 3.12 D-082

**Title:** *A finished card's conversation is readable, archivable and prunable; prune empties the conversation's documents and deletes its sessions, never the record.*
**Decision:** each card shows every attempt's prompt and reply (escaped markdown) and its question and answer. A finished card with no open attempt can be archived and restored (`ConversationArchived`); from the Archive page, with a typed confirmation bound to the listed cards, archived conversations are pruned (`ConversationPruned`, two stages): the prompt, reply and question documents are emptied in one transaction with a certificate per card (account, time, each document's SHA-256 and length, what survives), and then the gateway sessions of those cards are deleted and their outcome recorded. What survives, said in the form and the certificate: events (with the full question and answer), context slices (with the prompt's packet), run records, artifact rows, handoff/design/review documents, the gateway's question records and turn results until they expire, and backups. The prune empties the database property; it makes no claim about disk-page reclamation (D-043's check is not recorded in `RECONCILIATION.md`). Amends BR-41; an exception to D-012 and J-6. Back-office responses gain a framing/object/base/form-action CSP. **Rejected:** archive per session; archive of unfinished cards; archive state on a property; deleting events, run records or context slices; clearing question text from events; a raw-transcript view.

## 4. Change list

| # | File | Change |
|---|---|---|
| 1 | `dashboard/markdown.go` (new) | `renderMarkdown` |
| 2 | `dashboard/conversation.go` (new) | `CardRef`, types, `conversationOf`, `conversationStateOf`, `pruneSelectLocked`, `pruneCommitLocked`, MemStore methods, `SessionJanitor`, sentinels, `ConversationView` |
| 3 | `dashboard/events.go`, `fold.go`, `model.go`, `broker.go` | two names; no-op cases; `Actor`, `Prune`, `PruneRecord`, `PrunedBody`, `Clone`; `sseName` |
| 4 | `dashboard/store.go` | four `Store` methods; two `readOnly` refusals |
| 5a | `dashboard/store_arcade_blueprint.go` | `Conversation`, `ArchiveConversation`, `ArchivedConversations` |
| 5b | `dashboard/store_arcade.go` | `PruneConversations` (steps 1–5 with the `commit` transaction); `writeEvent` binds `actor` |
| 6 | `dashboard/aimanager_admin.go` | `AIMClient.DeleteSession`, `AIMAdapter.DeleteSession` |
| 7 | `dashboard/timing.go` | `taskEventsLocked`, four types, `convIdx`; `conversationURL` |
| 8 | `dashboard/board.go` | `BoardCard.Conversation` |
| 9 | `dashboard/handlers_bo.go`, `main.go`, `auth.go` | handlers; `archive` view; four routes; CSP header in `gate` |
| 10 | `dashboard/view.go`, `render.go` | `View.Talk`, `BOView.Archive`; funcs `conversationURL`, `markdown` |
| 11 | `dashboard/templates/needs.html`, `page.html`, `backoffice.html` | modal; include; chip; nav; `bo-archive` |
| 12 | `dashboard/static/board.js`, `app.js`, `app.css` | chip; opener; `cv-`, `md-` blocks |
| 13 | tests | §3.10 (+ `events_test.go`, `auth_test.go`, `store_conformance_test.go`, `taskfold_test.go`) |
| 14 | `artifacts/v1/gaos-arcadedb-model.html`, `pustaka/docs/architecture/graph.html` | two events |
| 15 | `pustaka/docs/product/brd.html` | BR-41, J-6 |
| 16 | `pustaka/docs/product/decision-log.html`, `pustaka/docs/assets/toc/product.js` | D-082, D-012 note, eighty-one |
| 17 | `pustaka/docs/product/qa-test-plan.html`, `pustaka/docs/assets/toc/product.js`, `.claude/rules/qa-test-plan.md`, `deploy/check-workspace.sh` | rows, `h3`, the strings in §3.10 |
| 18 | `pustaka/docs/changelog.html`, `pustaka/docs/assets/toc.js` | v0.42.0 |

## 5. Accepted limits

- L1. The full question and answer stay in the events and on the Timeline (BR-04).
- L2. The prompt's packet stays in the context slice (BR-32).
- L3. A failed session deletion is not retried automatically; the certificate counts it; the gateway's bucket expires it in 30 days.
- L4. Disk-page reclamation is not claimed (§3.7 Storage).
- L5. One trust domain; the certificate names the account.
- L6. Step 5's write can fail after sessions were deleted; counts go to the log.

## 6. Alternatives

- Raw transcript — no gateway read endpoint.
- Archive per session; archive of unfinished cards — round 1.
- The conversation inside the timing sheet — rejected: archive must remove the conversation from the card while the timing stays, and the two have different lifetimes.
- Leave sessions to the 30-day expiry — rejected: the gateway transcript holds tool calls and file contents GAOS never stores, and "permanently deletes" was asked for.
- Prune also clearing question text and context slices — rejected: rewrites events (BR-04) and removes the egress record (BR-32).

## 9. Risks

- R1. XSS via model text — §3.3, TC-479, TC-489, the CSP headers.
- R2. A forged destructive POST — `crossOriginOK`, `SameSite=Lax`, typed confirm, bound set; TC-486.
- R3. A live session deleted — terminal-only, no open run, shared ids kept, the gateway's `409`.
- R4. OQ-29 — the admin key is used only for ids GAOS's own run records name, validated as UUIDs.
- R5. No `script-src` CSP yet — needs an audit of every template's inline handlers and htmx attributes; a separate change.

## 10. Rollout

v0.42.0. No migration. Rollback = revert; an older build's fold skips the two types (`fold.go:445-449`).

## 11. Approval

The product owner's Phase 2 brief, relayed by the orchestrating agent on 2026-11-21 (a conversation message, not a repository file — labelled here as such), says: prune "permanently deletes" archived conversations; "May be deleted: artifact bodies and the ai-manager session transcript"; "Must stay: events, metadata and a tombstone recording who pruned what and when"; "Record the decision as a D-number"; and that approval of this design is delegated to the orchestrating agent ("approval delegated"), under the same standing instruction recorded in `card-timing.md` §11 and `runner-manual.md` §11. v3 deletes exactly what that brief allows and keeps what it requires, and says in the form what else survives. Approved 2026-11-21, v3, by the orchestrating agent under that delegation. Basis: round 1's four Blockers and one High and round 2's two Blockers are fixed (`reviews/conversations-review-1.md`, `-review-2.md`); round 2 raised no Critical or High security finding; no Blocker survives, so no third round (policy step 7).


## 12. As built (v0.42.0)

Built as designed, with these differences in naming and placement only:

- The 404 sentinel is `ErrSessionNotFound` — `ErrSessionGone` already names the chat path's 410 (`dashboard/aimanager.go:30`).
- The four handlers live in a new `dashboard/handlers_conversation.go`, not `handlers_bo.go`; the `archive` view is built in `buildBO` as designed, and its response sets `no-store`.
- `prunePlan` gains a `guard` so ArcadeStore takes its own lock around the bodies commit (in the same order `mutate` does) and not while the gateway deletes sessions.
- The sheet's view field is `View.Talk *ConversationView`; after archive, restore or prune the response carries template `bo-main-close-talk` (the sheet plus a hidden `conversation-modal`).

Verified: `go test -race ./...` green on both stores (ArcadeDB on the scratch image); `pustaka check`, pustaka tests, `check-workspace.sh`, `check-stacks.sh` pass; rendered in headless Chrome against the in-memory store — the chip on the card, the sheet with nested lists, a fence inside a list item and the model's `<b>` shown as text, archive closing the sheet, and the Archive page with the prune form.
