# Design iterate: a blocked design goes back to its author, and comes back for a fresh review

**Status:** Approved (v4). Round 1 reviewed v1 with five lenses (`reviews/design-iterate-*-1.md`, triage `reviews/design-iterate-review-1.md`); round 2 reviewed v2 with the critic, readiness and security lenses (`*-2.md`, triage `design-iterate-review-2.md`); round 3 reviewed v3 with the same three (`*-3.md`, triage `design-iterate-review-3.md`). Every `PROPOSE FIX` of the three rounds is applied below. Round 3's one Blocker (readiness) and three of its Majors targeted the verbatim-refusal mechanism of v3, which v4 removes (§3.5); its one High (security) is closed by its own remediation — the change is recorded and the cap is stated as the bound (§3.6a). Approval in §11.
**Feature:** `design-iterate`
**Version target:** the next free minor at merge (§13).
**Numbers claimed:** the next free ones at merge, under the two merge orders in §13. In the text below they are written for the expected order — `feat/conversations` (v0.42.0, D-082, TC-478…TC-489) merges first — as **D-083**, **BR-57**, **TC-490…TC-499**, **v0.43.0**. No Go string carries a D-number, so the code does not change with the order.
**BRD rules this must obey:** BR-04 (append-only), BR-05 (closed event catalog), BR-38 (one live turn per product), BR-47 / BR-49 (reviewer isolation, `pustaka/docs/product/brd.html:1326,1328`), BR-48 (an open Blocker goes to the operator's queue, `brd.html:1327`), BR-55 (a verified back-office session, `brd.html:1348`). BR-48 is amended; BR-57 is added.
**Security lens:** required — two back-office routes, an operator's identity recorded, and operator-typed text reaching a model's packet.

## 0. Glossary

- **Task / card** — one `TaskRecord` (`dashboard/model.go:685-714`), one card on a product's board (`BoardCard`, `dashboard/board.go:40-61`).
- **Task kinds** — `implement`, `design`, `review` (`dashboard/designgate.go:41-45`).
- **The design gate** — M17 (D-068, D-069). A planned story gets a design task, a review task that depends on it, and an implementation task that depends on the review (`dashboard/store_mem.go:1986-1987`). The gate is the `DEPENDS_ON` edge (`designgate.go:518-540`).
- **Design artifact / review artifact** — `ArtifactRecord`s of kind `design` / `review`, ids `<run>-design` / `<run>-review`, written by `gateVerdict` (`designgate.go:702-707, 739-744`); the newest of a kind for a task is the one read (`artifactOfKindFor`, `designgate.go:573-586`). The review artifact's body is `renderReviewArtifact` (`designgate.go:594-612`): line 1 the heading `# Design review — <task>`, line 2 empty, line 3 `verdict: accept|iterate` (`designgate.go:596-597`), line 4 `open blockers: <n>`, an optional `drift:` line, an empty line, then one line per finding, `- <severity>  <scope>  <note>` (two spaces between fields).
- **Verdict** — `VerdictAccept = "accept"`, `VerdictIterate = "iterate"` (`designgate.go:214-217`), derived from the findings: any Blocker means `iterate`. This document's new move rule is deliberately **not** called `iterate` (§3.2), so a search for one never matches the other.
- **Status** — closed vocabulary of eleven values (`dashboard/taskstate.go:32-46`), held equal to the DDL by `TestTheTaskStatusVocabularyMatchesTheDDL`. Legal changes are `taskTransitions` (`taskstate.go:79-95`); `DONE` and `CANCELED` have no exits (`taskstate.go:87,89`). `BLOCKED → PENDING` is legal (`taskstate.go:85`).
- **Move** — every status change writes one `TaskMoved` through `moveTaskLocked` (`dashboard/runner.go:430-444`), payload `Task`, `Rule` (who acted), `Reason = "FROM→TO"`, `To`. The task fold replays `To` without consulting the state machine and does not rebuild `BlockReason` (`dashboard/taskfold.go:49-62`).
- **Block reason** — `TaskRecord.BlockReason`, persisted as `Task.blockReason` (`dashboard/schema/04-execution.sql:39`); not a closed vocabulary.
- **Scheduler promotion** — a `PENDING` task whose dependencies are all `DONE` goes to `READY` with rule `schedule`: under `GAOS_RUNNER=manual` by `readyPending` (`runner.go:553-576`, called at `runner.go:478-480`), under `on` immediately followed by the dispatch (`runner.go:524-525`). Under `off` nothing moves (`runner.go:482-487`).
- **Session** — the gateway session a task's turns run in. A task's next attempt resumes its last session unless a rule opens a new one (`sessionForLocked`, `dashboard/turns.go:102-143`, D-056). A review's first attempt always opens a fresh one (`SessionReviewer`, `turns.go:111-112`, D-069).
- **Packet** — what one attempt is given, frozen by `freezePacketFor` (`store_mem.go:1338-1560`).
- **Back office (BO)** — every route is session-gated and same-origin unless listed public or bearer (`dashboard/auth.go:523-555`; `crossOriginOK` refuses a cross-site **non-GET**, `auth.go:557-575`). The signed-in account is read as `ss, ok := sessionOf(r)` → `ss.Email` (`auth.go:355-358`).
- **The four dispositions** — `CONTEST`, `ESCALATE`, `PROPOSE FIX`, `DECLINE` (`docs/rules/gaos-design-review.md`, "The flow", step 5).
- **The brief** — the product owner's request this design answers, restated in §1-§2: build the iterate loop; per-finding triage with the four dispositions; the designer's session resumed; a fresh re-review; a cap; an operator override *only if the review agrees it is safe*.

## 1. Problem

Found on production (`gaos.sismedika.com`, product p-6, workflow wf-9):

1. Design task **wf-9-t-3** completed with a valid `gaos-design` block. `gateVerdict` recorded the design artifact and moved it `REVIEW → DONE` with rule `design-delivered` (`designgate.go:708`; the two-move order is `runner.go:296-317`).
2. Review task **wf-9-t-4** returned a valid `gaos-review` with 1 blocker, 1 major, 2 nits. `gateVerdict` recorded the review artifact and moved it to `BLOCKED` with rule and reason `design-blocked` (`designgate.go:745-746`). `gateReleaseLocked` left implementation **wf-9-t-5** `BLOCKED`, reason `design-blocked` (`designgate.go:791-792`).

Nothing can move after that:

- The design card is `DONE`. `DONE` has no exits (`taskstate.go:87`) and the board refuses any move out of done (`board.go:171-172`).
- Unblocking the review (`BLOCKED → READY`, rule `unblock`, `board.go:183-184`) re-runs the review against the **same** design (`store_mem.go:1491-1500` reads the newest design artifact, which has not changed). At best it blocks again; at worst a reroll of a zero-context reviewer clears a design nobody changed — a gate bypass by repetition.
- A Blueprint sync's drift release does the same reroll without anybody asking: `releaseDriftLocked` moves **every** `BLOCKED` task of the product to `READY`, whatever its reason (`dashboard/workspace.go:384-397`).
- The implementation cannot be unblocked by hand: `gateHoldsLocked` refuses it while the review is not `DONE` (`designgate.go:860-873`, `board.go:285-294`).
- The published doc says only "the task goes to the operator's queue — and the clearance is refused until the back office has a real identity" (`pustaka/docs/architecture/design-gate.html:178-180`). The identity exists now (v0.39.0, D-079, BR-55). BR-48 says the same and adds that *which* operator acted is not recorded (`brd.html:1327`).

**Any design that draws a Blocker stalls its workflow forever.** The only exit is `cancel` on the review and the implementation, which throws the story away.

## 2. Goals and non-goals

**Goals**

- G1. **Send the design back.** On a review blocked with reason `design-blocked`, a signed-in operator sends the design back to its author: the design re-opens `DONE → READY` and the review waits `BLOCKED → PENDING`, both with the new move rule `send-back`.
- G2. **The revision sees why.** The design's next attempt carries the design it is revising as `.gaos/packet/design.md` and the review's findings as `.gaos/packet/context/findings.md` (the path the implementer gets them on, `store_mem.go:1528-1535`), with the operator's optional per-finding triage — one of the four dispositions and a note.
- G3. **Same author session, where possible.** The revision resumes the designer's session (D-056). It does not depend on it: the packet carries the design and the findings, so a rotated or lost session loses nothing.
- G4. **Automatic re-review on a fresh session.** The review waits in `PENDING` on its `DEPENDS_ON` edge; when the revision is delivered (`DONE`), the scheduler promotes it as it promoted the first review. Its next attempt opens a **fresh** session (§3.5) with a packet holding the new design and the rubric. The gate then clears or blocks as always.
- G5. **A cap.** At most **3** send-backs per design. At the cap the button is replaced by a line saying so, and the route refuses (§3.7).
- G6. **The reroll is closed** on every route that could take it: the board's unblock and the drift release (§3.6).
- G7. **Audit.** The send-back records who (the signed-in account, required) and the triage, on the `TaskMoved` events themselves.
- G8. **UI.** A design-gate block reason is visible on its card; a design-blocked review card carries a "Revise design" button that opens a triage sheet.
- G9. **Docs and registers.** `design-gate.html`, BR-48 amended, BR-57 added, OQ-11 and OQ-34 narrowed, D-083, TC-490…TC-499 with every count, the changelog and `toc.js`.

**Non-goals**

- N1. **No unreviewed override.** The brief's conditional override — clear the gate when the operator contests the findings and the review agrees — is delivered as G1 + G4: the operator CONTESTs (or DECLINEs) in the triage, the designer keeps the design and states why as design rationale, and a fresh review either clears it or blocks it again. What is not built is a clearance with no review at all (§6 A5).
- N2. No new task status, no new event type (BR-05), no new task kind, no new artifact kind, no schema statement.
- N3. The re-reviewer's *packet* holds no findings and no triage. Whether the revised *design document* carries them, quoted or paraphrased, is a soft boundary enforced by an instruction only, not a guarantee (§3.5, R2).
- N4. No change to the implementation's packet: it already reads the newest design and the newest review (`store_mem.go:1528-1545`).
- N5. No change to how a review is parsed or scored.
- N6. No per-account authorisation: any signed-in account may send a design back, as any may move any card today (BR-55 is one trust domain).
- N7. This change records an actor on the send-back only. It does not change how any other move is attributed (§3.2, and §13 for the wording under each merge order); OQ-11 and OQ-34 are narrowed, not answered.
- N8. No deploy and no VM change. The prod move for wf-9 is §9, for the operator to make after a deploy they run.

## 3. Design

### 3.1 The state machine: one guarded edge out of DONE

`taskTransitions` (`taskstate.go:79-95`) gains `StatusDone: {StatusReady}` — the **send-back row**, with a comment in the style of the three argued rows above it (`taskstate.go:57-76`). It reverses, for design tasks only, the argument recorded on the illegal pair it replaces — "the work shipped; re-running it is a new task, not a new status" (`dashboard/taskstate_test.go:47`): a design the gate rejected did not ship, and a new task would need the review's and the implementation's edges to move (§6 A2).

An edge in the table is global, so it is guarded:

1. **`moveTaskLocked`** (`runner.go:430-444`) holds the task and the rule. It refuses any move whose `from == StatusDone` unless `taskKind(t.Kind) == TaskKindDesign && rule == MoveSendBack`, returning `(Event{}, false)` as for any refused move. This is the guard.
2. **`moveTask`** (`taskstate.go:145-157`) additionally refuses `DONE → READY` for any task whose kind is not `design`, with `reject("A task cannot go from DONE to READY.")` — the sentence the table produced before. It is defensive: the only other path to `moveTask` is `canMoveTask` from the board (`board.go:297`), which `legalMove` already refuses out of Done (`board.go:171-172`).

`legalMove` is unchanged, so a drag out of Done is still refused. The fold needs no change.

**Card timing (D-081).** `cardTiming` (`dashboard/timing.go:153-226`) gives a `DONE` segment `BucketDone` (`timing.go:132-133`), which is in none of the five partition sums (`timing.go:239-252`). A `DONE` segment that is later left would drop out of the lead. Fix: in the loop, when a move closes a segment whose status is terminal, that segment's `Bucket` becomes `BucketBlocked` — a convention that keeps the partition exact; the time was the review's turn and the operator's wait, which the review card's own timing shows. The last segment's handling (`timing.go:213-223`) is unchanged.

### 3.2 The names

In `designgate.go`, beside the existing reasons and rules (`designgate.go:454-469`):

```go
maxDesignSendBacks = 3
```

In `board.go`, beside the operator's actions (`board.go:136-145`): `MoveSendBack = "send-back"`.

`EventPayload` (`model.go:215-275`) gains:

```go
// Actor is the signed-in back-office account that sent a design back.
// Set on the two TaskMoved events of a send-back and on nothing
// else: every other move, the operator's and the machine's, leaves it empty.
Actor string
// Triage is the operator's disposition of the review's findings, carried by
// the design's send-back TaskMoved and by nothing else.
Triage []FindingTriage
```

```go
type FindingTriage struct {
    N           int    // the finding's 1-based position in the review artifact
    Disposition string // "", "CONTEST", "ESCALATE", "PROPOSE FIX", "DECLINE"
    Note        string // one line, at most maxFindingNote runes
}
```

`Clone` (`model.go:285-306`) copies `Triage`. Adding fields to `EventPayload` has precedent (`Question`, `Genesis`, `model.go:267-275`); it is not a new event type (BR-05 closes types, `events.go:29-31`). `writeEvent` (`dashboard/store_arcade.go:1063-1069`) binds the row's `actor` column to `Payload.Actor` when it is set and keeps the literal `'operator'` otherwise, so the column and the payload never disagree for the send-back (round-3 SEC-2). Nothing reads the column back (`store_arcade.go:1745-1756`); it is for a query against the database.

**If `feat/conversations` has merged first** it will already define `EventPayload.Actor string` with the same meaning ("the signed-in account that made this operator action") and the same `actor` column binding. Then this change adds only `Triage` and `FindingTriage`. The author of whichever branch merges second reconciles the one field at rebase; the semantics above are the contract either way.

### 3.3 The shared check, the write and the read

**The check.** `sendBackCheckLocked(productID, reviewTaskID string) (sendBack, error)` (new, `designgate.go`, caller holds `s.mu`), with

```go
type sendBack struct {
    Review, Design string
    Sent           int             // send-backs so far
    Findings       []ReviewFinding // read back from the review artifact
    Artifact       ArtifactRecord  // the review artifact
}
```

Steps, each refusal a `reject(...)` with the sentence shown (`<x>` is a task id):

1. `t := taskByIDLocked(reviewTaskID)`; `t == nil || t.DeskID != productID` → `ErrNotFound`.
2. `taskKind(t.Kind) != TaskKindReview` → `"<review> is not a design review; only a review the design gate blocked sends a design back"`.
3. `t.Status != StatusBlocked || t.BlockReason != ReasonDesignBlocked` → `"<review> is <STATUS> (<reason or ->); a design goes back only from a review whose findings blocked it"`.
4. `designID := gateDesignDepLocked(t)`; empty, or the design task's status is not `DONE` → `"<review> has no delivered design to send back"`.
5. `art, ok := reviewArtifactFor(reviewTaskID)`; `!ok || reviewVerdictOf(art.Body) != VerdictIterate` → `"<review> has no recorded verdict that asks for a revision"`.
6. `sent := s.sendBacksLocked(designID)` — one scan of `s.events` counting `TaskMoved` with `Payload.Task == designID && Payload.Rule == MoveSendBack`, returning the count and the newest such event. `sent >= maxDesignSendBacks` → `"<design> has been sent back <sent> times, which is the limit; cancel <review> and the work it holds, and plan the change again"`. Signature: `func (s *MemStore) sendBacksLocked(designID string) (int, Event)`, the zero `Event` when the count is 0.

`reviewVerdictOf(body string) string` (new, pure): split the body on `\n`; require line 1 to start with `# Design review` and line 2 to be empty; line 3 must match `^verdict: (accept|iterate)$` and the captured word is returned. Any other shape returns `""`. (`renderReviewArtifact` writes the heading and a blank line, `designgate.go:596`, then the verdict, `:597`.) It is anchored so that a reviewer's note containing the text `verdict: accept` cannot satisfy it (SEC-6). `gateStateLocked`'s own check (`designgate.go:503-504`, a substring search over the whole body) is switched to it in the same change; TC-490 asserts `reviewVerdictOf(renderReviewArtifact(...))` returns `accept` for no blockers, `iterate` for one, and the same with a `drift:` line, and that an unmodified cleared review still releases its implementation (`gateStateLocked` → `GateCleared`).

`reviewFindingsOf(body string) []ReviewFinding` (new, pure) reads every line starting `"- "`: severity is up to the first `"  "`, scope up to the next `"  "`, the note is the rest. Severity and scope cannot contain a space (`validScope`, `designgate.go:413-415`), so the split is exact even when a note holds double spaces. A line that does not split into three non-empty parts is skipped.

**The write.** New `Store` method (`dashboard/store.go`, beside `MoveCard` at :126):

```go
SendBackDesign(ctx context.Context, productID, reviewTaskID, actor string, triage []FindingTriage) (Result, error)
```

`MemStore.SendBackDesign`, under `s.mu`:

1. `actor == ""` → `reject("a send-back needs a signed-in account")`.
2. `sb, err := sendBackCheckLocked(...)`; an error is returned as is.
3. The triage, normalised (`normalizeTriage(triage, len(sb.Findings))`, pure): each `Note` is trimmed; an entry whose disposition and note are both empty is dropped; then, in order, the first violation refuses with the sentence shown: `N < 1 || N > len(findings)` → `"finding <N> does not exist; the review has <len> findings"`; a repeated `N` → `"finding <N> is triaged twice"`; a disposition not empty and not exactly one of the four → `"finding <N>: <d> is not a disposition; use CONTEST, ESCALATE, PROPOSE FIX or DECLINE"`; a note over `maxFindingNote` (300, `designgate.go:289`) runes → `"finding <N>: the note is <n> characters and the limit is 300"`; a note holding any rune of Unicode category Cc, Cf, Zl or Zp (`unicode.In(r, unicode.Cc, unicode.Cf, unicode.Zl, unicode.Zp)`, which covers line breaks, NUL, bidi overrides and zero-width characters — SEC-5) → `"finding <N>: the note is one line of plain text"`; a reserved fence (`reservedFenceIn`, `designgate.go:621-628`) → `"finding <N>: the note carries a reserved block"`. The kept entries are sorted by `N`.
4. The rendered triage (`renderTriage`, §3.4) is screened with `screenArtifactBody(".gaos/packet/context/findings.md", body)` (`designgate.go:847-849`); a hit → `"the triage was refused by the egress screen; remove the secret and send it again"`. All of this happens before anything is written, because the events are append-only (BR-04).
5. The writes, in this order:
   - `moveTaskWithLocked(sb.Design, StatusReady, MoveSendBack, EventPayload{Actor: actor, Triage: kept})`;
   - `moveTaskWithLocked(sb.Review, StatusPending, MoveSendBack, EventPayload{Actor: actor})`, and the review's `BlockReason` is cleared;
   - every implementation task gated on the review (`gateReviewDepLocked(t) == sb.Review`) keeps `BLOCKED`, its reason set to `ReasonDesignUnreviewed` — what `gateStateLocked` reports for a pending review (`designgate.go:513-514`).

   `moveTaskWithLocked(id, to, rule string, extra EventPayload)` is `moveTaskLocked` with `extra`'s `Actor` and `Triage` copied into the event's payload; `moveTaskLocked(id, to, rule)` becomes a call to it with an empty `extra`. The event's `Text` is today's `"<id>: <rule> (<FROM> → <TO>)"` (`runner.go:441`) with `" by <actor>"` appended **only when `extra.Actor != ""`**; every other caption is unchanged byte for byte.
6. Toast: `"<design> goes back to its author (revision <sent+1> of <maxDesignSendBacks>); <review> reviews it again when it is delivered."` Every "of 3" in this document is `maxDesignSendBacks`, never a literal in code or template.

Two names for the counter, used everywhere: **`sent`** is send-backs made so far; **`revision`** is `sent + 1` at the moment of a send-back, and is the number the designer is told.

`ArcadeStore.SendBackDesign` is `s.mutate(ctx, func() { return s.proj.SendBackDesign(...) })`, as `MoveCard` is (`dashboard/store_arcade_blueprint.go:39-41`): the two events and the task records persist in one transaction. `readOnly.SendBackDesign` refuses with `replayRefusal`, as `readOnly.MoveCard` does (`store.go:287-289`).

**The read**, for the sheet (round-1 Blocker). New `Store` method:

```go
SendBackSheet(ctx context.Context, productID, reviewTaskID string) (SendBackView, error)

type SendBackView struct {
    Review, Design string
    Sent, Cap      int
    Findings       []ReviewFinding
    Refusal        string // "" when the design may be sent back now
    DesignBytes    int    // the size of the design being sent back
    PartLimit      int    // the egress per-part limit (EgressPolicy.MaxPartBytes)
}
```

`MemStore.SendBackSheet` takes `s.mu.RLock`, calls `sendBackCheckLocked`; `ErrNotFound` is returned as is; any other refusal fills `Refusal` with its sentence and leaves `Findings` empty; success fills everything with `Refusal == ""`. `ArcadeStore.SendBackSheet` returns `s.proj.SendBackSheet(...)` (a read, like `CardTiming`, `store_arcade_blueprint.go:36-38`). `readOnly` embeds the store, so reads pass through unchanged.

What the send-back is not: a dispatch. Under `GAOS_RUNNER=manual` the design waits in Ready for the operator's dispatch (D-080); under `on` the scheduler picks it up; under `off` nothing runs.

### 3.3a The drift line of a re-review

`blueprintPinsLocked` (`designgate.go:807-824`) takes the review's pin from the **first** run of the review task it finds (`designgate.go:817-822`). With re-reviews a review has several runs, possibly under newer Blueprints, and the `drift:` line `renderReviewArtifact` writes (`designgate.go:599-602`) would compare the new design's pin with the first review's. Fix: `blueprintPinsLocked(reviewTaskID, runID string)` takes the review pin from `blueprintPinForRunLocked(runID)` (`designgate.go:827-834`), the run being judged; its one caller passes the `runID` it already has (`designgate.go:734`). TC-493 asserts a re-review under a newer Blueprint names the new pins.

### 3.4 The revision's packet

`renderTriage(findings []ReviewFinding, triage []FindingTriage, revision int) string` (new, pure) returns exactly:

```
## Sent back for revision <revision> of 3

The operator's triage of the findings above. A finding with no line here was not triaged.

- finding 1 (blocker, src/x.go:12): CONTEST — "the order is stated in section 3.2"
- finding 2 (major, docs/a.md): PROPOSE FIX

Revise the design in .gaos/packet/design.md and deliver it whole. Change the design so it stands on its own: where you keep a choice a finding questioned, state why the choice is right as a property of the design, without referring to any review, finding or triage. A fresh reviewer who has seen none of them reads the result.
```

When no finding is triaged the list is the single line `- none`. A finding line omits ` — "<note>"` when the note is empty, and writes `—` alone when the disposition is empty (`- finding 3 (nit, a.md): — "the note"`). The note is rendered with `strconv.Quote`, so it is always one double-quoted Go string literal: a `"` inside the note is escaped and cannot end the quotation (round-3 SEC-5). No actor appears: the email never enters a prompt.

In `freezePacketFor`, inside the existing `if kind != TaskKindReview` block (`store_mem.go:1443-1463`), after the previous handoff, **for a design task only**:

```go
if kind == TaskKindDesign {
    if sent, last := s.sendBacksLocked(taskID); sent > 0 {
        if reviewID := s.reviewOfDesignLocked(taskID); reviewID != "" {
            if art, ok := s.reviewArtifactFor(reviewID); ok {
                const path = ".gaos/packet/context/findings.md"
                body := art.Body + "\n" + renderTriage(reviewFindingsOf(art.Body), last.Payload.Triage, sent)
                add(path, KindContext, body)
                cite("Artifact", art.ID, CiteLesson, path, body)
            }
        }
        if art, ok := s.designArtifactFor(taskID); ok {
            const path = ".gaos/packet/design.md"
            add(path, KindDesign, art.Body)
            cite("Artifact", art.ID, CiteDesign, path, art.Body)
        }
    }
}
```

Here `sent` already counts the send-back being answered, so it is the revision number. `reviewOfDesignLocked(designID)` is the first task in `s.tasks` with `taskKind == review` whose `gateDesignDepLocked` is `designID`; the plan builds exactly one per design (`store_mem.go:1986-1987, 1996-1997`). The design artifact is the one the implementer's packet already carries the same way (`store_mem.go:1536-1541`).

The packet is deterministic in the events and artifacts present at freeze, so two freezes of one attempt give one digest. A design never sent back gets neither part.

**The per-part limit.** The egress pass drops, rather than refuses, a packet part larger than `MaxPartBytes` (`egress.go:515-519`; default 64 KiB, `modelconfig.go:75`). A design over that size is dropped from the revision's packet as it is already dropped from the review's (an inherited limit of the gate, not new here). The revision then sees its previous design only through its resumed session. The sheet says so before the operator sends it back: when `DesignBytes > PartLimit` it shows `The design is <n> KiB and a packet part carries at most <m> KiB; the designer sees it only through its own session.` The send-back is still allowed.

The packet also carries the product's newest handoff (`latestHandoffFor`, `store_mem.go:1281-1305`), which is desk-scoped and so may be the reviewer's or another story's. That is unchanged and is not the direction D-069 withholds.

### 3.5 Sessions, and what the re-reviewer sees

**The designer** resumes its last session (`turns.go:142`) when no rule opens a new one — no code change. Rotation by age, a lost session or a vendor change opens a new one (`turns.go:118-141`); the packet (§3.4) carries everything the revision needs either way.

**The reviewer** must not resume. Its last session holds the previous design and its own previous findings: (1) a reviewer that sees its earlier findings anchors on them and checks whether they were addressed instead of attacking the document (`docs/rules/gaos-design-review.md`, "Why this rule exists"); (2) the operator's triage argues with the findings and is the author's side of the desk, which BR-47 / BR-49 withhold; (3) D-069 already opens a fresh session for a review's first attempt for this reason (`turns.go:44-48`), and a review of a new design is a first attempt for isolation purposes. A review's **retry** on the same design still resumes (D-056).

The rule is decided **from the event stream, not from clocks** (SEC-2). A new case directly after the `last == nil` review case (`turns.go:111-112`):

```go
case s.taskIsKindLocked(taskID, TaskKindReview) && s.reviewSentBackSinceRunLocked(taskID):
    return "", SessionReviewer
```

`reviewSentBackSinceRunLocked(reviewID)`: in one scan of `s.events`, the highest `Seq` of a `TaskMoved` with `Task == reviewID && Rule == MoveSendBack` (`sb`) and the highest `Seq` of a `TaskMoved` with `Task == reviewID && To == StatusRunning` (`rn`). True iff `sb > 0 && sb > rn`. A task reaches `RUNNING` only after its run record exists (`startRun` before the move, `runner.go:139` and `:156`; recovery's `adopted` move is for a run that already exists, `turns.go:203-205`), so `rn` marks the newest attempt that really ran; a dispatch that never started a run (`not-started`, `requeued`, a restart between the dispatch move and `startRun`) writes no `RUNNING` move and cannot hide a send-back. `sessionForLocked` is called while the packet is cleared, before the new attempt's `DISPATCHED` move (`runner.go:595`, inside `clearFor`, which `dispatchNow` calls at `board.go:410` and `RunOnce` at `runner.go:498`, each before its `DISPATCHED` move), so for the first attempt after a send-back the newest `RUNNING` move is the pre-send-back attempt's and the answer is true; for a retry of the re-review that actually ran, the newest `RUNNING` move is the re-review's own, after the send-back, and the answer is false. Seq is strictly increasing (`caseEventSeq`), so there is no tie. Fail closed: a send-back with no `RUNNING` move after it is fresh.

**The re-review's packet** is the existing review packet (`store_mem.go:1484-1500`): the newest design artifact and the rubric. The §3.4 block is under `kind == TaskKindDesign`, so no `findings.md` and no triage.

**What the revised document may carry (SEC-1).** The designer is given the findings and the triage and could carry them, quoted or paraphrased, into the design, which then reaches the fresh reviewer. The one measure is the instruction at the end of `renderTriage` (§3.4): state reasons as properties of the design, without referring to any review. A mechanical check was designed in v3 (a normalised verbatim match) and removed in v4: it refused legitimate revisions that kept text a reviewer or the operator had quoted from the design itself — exactly what a CONTEST asks the designer to keep — and it could not reach a paraphrase anyway. **The limit, stated in BR-57:** the isolation of the re-reviewer is over its packet and its session; the content of the revised design is the author's, and may carry the author's reading of the review.

**The workspace (SEC-3).** BR-47 records that the gateway documents a write boundary, not a read boundary, between the author's folder and the reviewer's sibling folder (`brd.html:1326`). GAOS writes no packet part to any folder — the parts travel as prompt text (`RenderPrompt`, `adapter_aimanager.go:112-122`) — so `findings.md` and the triage exist in the author's session, not as files GAOS put there; a designer could still write them to its folder itself. The reviewer's workspace is reset before **every** review turn, a re-review included, because the reset is in `clearFor` for every review packet (`runner.go:607-612`); TC-493 asserts it ran for the re-review. The remaining read risk is the inherited limit of BR-47, stated in §8 and in BR-57's own text.

### 3.6 The reroll, closed on every route

**The board.** `MoveCard` (`board.go:255-308`) gains, next to the existing `gateHoldsLocked` refusal (`board.go:285-294`):

```go
if action == MoveUnblock && taskKind(t.Kind) == TaskKindReview && t.BlockReason == ReasonDesignBlocked {
    s.mu.Unlock()
    return Result{}, reject(fmt.Sprintf(
        "%s's findings block its design; send the design back to its author (Revise design) instead of re-running the review on the same design", t.ID))
}
```

`legalMove` stays pure over the fold's fields (§3.9 says why); the board hides the `→ ready` button through the decoration in §3.9.

**The drift release.** `releaseDriftLocked` (`workspace.go:384-397`) skips a `BLOCKED` task whose `BlockReason` is `design-blocked` or `design-unreviewed` (a verdict or a gate hold) or `qa-epic-added` (BR-54: such a card is "released only by an operator's board move", `brd.html:1347`, which today's drift release breaks) through a new `operatorHolds(reason string) bool` (`designgate.go`). A Blueprint sync fixes none of these, and its own comment says it releases what drift blocked (`workspace.go:380-383`). Every other reason is released as today.

**Stale reasons.** A design or review task's reason is written by the gate (`runner.go:311`) and, today, never cleared when the operator unblocks it (`board.go:295-301`); `refuseDispatch` blocks a task without writing one (`runner.go:623-647`). So `MoveCard` clears `BlockReason` on every `unblock`, and `SendBackDesign` clears the review's (§3.3). With that, a `BLOCKED` task carries a reason only when the gate (or the QA catalog, `qacatalog_create.go:222`, or the question expiry, `questions.go:236`) wrote it for **this** block.

**Approve.** `MoveCard` refuses `approve` (`REVIEW → DONE`) for a design or a review task: `"<id> is a <kind> task; the design gate decides when it is done, not an approval"`. D-069 clause 6 makes the gate the only writer of their `DONE` (the comment at `runner.go:288-292`). Without this, a revision whose reply carried no design block — which lands in `REVIEW` (`runner.go:275-284`; `gateVerdict` steps aside, `designgate.go:685-690`) — could be approved to `DONE` and its review would run on the **previous** design (round-2 Blocker). Such a design card leaves `REVIEW` by `redo` or `cancel`.

**What stays unblockable:** a review blocked for `review-failed`, `design-missing` or `review-screened-out` (failures of the review), and a design blocked for `design-screened-out`.

**The board's buttons.** `decorateBoardGate` (§3.9) removes `ready` from a `design-blocked` review's `Moves` and `done` from a design or review card in `REVIEW`, so the board offers neither refused move.

### 3.6a The re-review reviews a new design or nothing

Independently of how the design reached `DONE`, `gateVerdict`'s review branch (`designgate.go:715-724`) gains a precondition: when `sent := sendBacksLocked(designID)` is more than zero, the number of design artifacts of the design task (artifacts with `Kind == design && TaskID == designID`) must be **greater than `sent`** — one per delivery, and every send-back follows a delivery. Otherwise the review goes `BLOCKED` with `ReasonDesignMissing`, as when there is no design at all (`designgate.go:722-724`). TC-492 asserts it.

**A barely changed revision is a new design** (round-3 security High). A revision may keep the design almost whole — that is what a CONTEST asks for — and its re-review is a fresh sample of the gate. Nothing refuses it; two things bound and expose it. (1) The cap: at most three send-backs, so at most four reviews of one story's design in all. (2) The record: when the design branch records a revision's artifact (`sent > 0`), the `RunExited` caption gains `"; revision <sent> changes <k> of <m> lines of the design it replaces"`, where `m` is the line count of the previous design artifact and `k` the number of its lines (compared after trimming trailing spaces) that do not occur in the new one — a set difference, not a diff. The caption is assembled through a new `gateOutcome.note` field, appended after the gate's move as `"; " + note`; the existing `detail` suffix keeps its `"; the review was refused: "` prefix and is only ever set by the review branch. BR-57 and D-083 say the cap, not an identity check, bounds re-sampling.

**During the revision** the review is `PENDING`: the board refuses backlog → ready (`board.go:173-174`), the drift release touches only `BLOCKED`, and the scheduler promotes it only when the design is `DONE` (`unmetDeps`, `scheduler.go:370-385`). A design canceled mid-revision leaves the review `PENDING` with an unmet dependency; its exit is cancel, like any card whose prerequisite was canceled.

### 3.7 The cap

`maxDesignSendBacks = 3`, counted from the events (§3.3 step 6), so it survives a restart: the ArcadeDB projection loads every event on boot (`store_arcade.go:1739-1767`). Each round costs a design turn and a review turn, and a design that has failed four fresh reviews points at the story, not the document.

**At the cap** the review is `BLOCKED` / `design-blocked` and stays so; the card shows `revision limit reached (3 of 3)` instead of the button; the sheet shows the step-6 sentence and no form; the route refuses. The exit is `cancel` on the review and on the implementation (`board.go:187-188`), then a new workflow for the story. There is no override (N1).

The count is a scan of `s.events`, done only for review cards that are `BLOCKED` / `design-blocked` (§3.9), on a send-back, and on a design task's packet freeze — not on every card.

### 3.8 The routes

Both back-office: session and same-origin through the gate (`auth.go:523-555`), in neither `publicPatterns` nor the bearer set. Registered in `dashboard/main.go` beside the board routes (`main.go:376-380`) and the sheets (`main.go:388-393`):

- `GET /ui/send-back/{id}/{taskId}` → `handleSendBackSheet` (`handlers_bo.go`): starts with `if !s.requireBO(w, r) { return }` as the sibling sheets do (`handlers_bo.go:758, 785`); `s.store.SendBackSheet(r.Context(), id, taskId)`; `ErrNotFound` → `s.fail` (404 toast), so a card of another product is a 404 (SEC-9). Otherwise sets `v.SendBack = &SendBackSheetView{ProductName: <the product's name, looked up as handleTimingSheet does, handlers_bo.go:794-801>, SendBackView: sv}` and renders `send-back-modal` (§3.9). Listed in `readOnlyGET` (`dashboard/auth_test.go:258-274`) as `"renders the send-back sheet"`.
- `POST /products/{id}/board/cards/{taskId}/send-back` → `handleSendBack` (`handlers_board.go`):
  1. `ss, ok := sessionOf(r)`; `!ok || ss.Email == ""` → 401 `{"error":"sign in to the back office"}` (the gate has already refused a request with no session; this is the belt to that brace).
  2. `application/x-www-form-urlencoded` only: any other `Content-Type` (JSON, multipart, none) → 415 `{"error":"the send-back is a form post"}`. Body capped with `http.MaxBytesReader(w, r.Body, 256<<10)`, then `r.ParseForm()`; a parse error (including the cap) → 400 `{"error":"the triage could not be read"}` (JSON client) / the same sentence as a toast (HTMX). Values are read with `r.PostFormValue`, so nothing in the query string is ever a triage (round-3 SEC-3).
  3. For `N = 1, 2, …` while the posted form has a key `d<N>` or `n<N>` (stopping at the first `N` with neither; at most 1000), `FindingTriage{N, r.PostFormValue("d<N>"), r.PostFormValue("n<N>")}`. `SendBackDesign` validates `N` against the actual findings.
  4. `s.mut(r).SendBackDesign(r.Context(), id, taskId, ss.Email, triage)`.
  5. HTMX: success → `renderBOAfter(w, r, res, id, "board")` (`handlers_bo.go:494`); refusal → `s.fail`. JSON: 200 `{"card": <the review card>}`, 404 `{"error":"no such card"}`, 409 `{"error":"move refused","reason":...}` — the board's existing shapes (`handlers_board.go:51-90`).

`crossOriginOK` refuses a cross-site POST and passes any GET (`auth.go:560-564`); TC-497 asserts the 403 on the POST only.

### 3.9 The board

`BoardCard` (`board.go:40-61`) gains, as **read-time decoration** like `Timing` and `Qa`:

```go
// Reason is why a BLOCKED card is held: its BlockReason. Display only.
Reason string `json:"reason,omitempty"`
// SendBack is set on a review the gate blocked with design-blocked.
SendBack *BoardSendBack `json:"sendBack,omitempty"`
```

```go
type BoardSendBack struct {
    Design string `json:"design"` // the design task id
    Sent   int    `json:"sent"`   // send-backs so far
    Cap    int    `json:"cap"`    // maxDesignSendBacks
    Open   bool   `json:"open"`   // Sent < Cap
}
```

`boardOf` (`board.go:224-251`) is **not** changed: it is the pure projection that `TestTheTaskFoldReproducesTheBoard` (TC-308, `dashboard/taskfold_test.go:127-152`) compares, by `reflect.DeepEqual`, with the board rebuilt from events, and the fold does not rebuild `BlockReason` (round-1 Blocker). Instead `MemStore.Board` (`board.go:207-218`) calls a new `s.decorateBoardGate(&b)` after `decorateBoardTiming`. For each card: if the task is `BLOCKED` and `t.BlockReason != ""`, set `Reason`; if it is a review with `design-blocked`, set `SendBack` and remove `ColReady` from `Moves` (the unblock §3.6 refuses); if it is a design or review task in `REVIEW`, remove `ColDone` from `Moves` (the approve §3.6 refuses).

`foldedBoard` (`taskfold_test.go:114-126`) already clears the read-time decoration (`Timing`, `Workflows`); it also clears `Reason` and `SendBack` and, for every card, sets the live card's `Moves` to `legalMoves` of the folded task before comparing — the gate's removal of moves is read-time decoration too. TC-308's prose gains one sentence: the gate's decoration is read-time, like the timing.

Every reason of a `BLOCKED` task is shown: with §3.6's clearing on unblock, a reason belongs to the current block. A runner block that writes no reason (`HandoffBlocked`, `runner.go:282-283`) shows none.

**Server board** (`dashboard/templates/backoffice.html:395-405`): directly after `{{$card := .ID}}` (`:400`), `{{with .Reason}}<div class="mono card-reason">blocked: {{.}}</div>{{end}}`; then `{{with .SendBack}}` either `<button type="button" class="btn card-sendback" id="sb-{{$card}}" data-sendback-open="{{sendBackURL $id $card}}" aria-label="Send the design {{.Design}} back to its author">Revise design</button>` when `.Open`, or `<div class="mono card-reason">revision limit reached ({{.Sent}} of {{.Cap}})</div>`. `sendBackURL` is a new template func beside `timingURL` (`dashboard/render.go:141`, `timing.go:374-376`) that path-escapes both ids; `board.js` builds the same URL with `encodeURIComponent`, as `needsBadge` does (`board.js:28`).
**Client board** (`dashboard/static/board.js`): the same elements from `card.reason` and `card.sendBack`, built with `createElement` and `textContent` as `needsBadge` and `qaBadge` are (`board.js:20-63`); the button stops `Enter`, `Space`, `F2`, `Delete` and `Backspace` from reaching sisflow, as the badge does (`board.js:30-32`).
**app.js** (`dashboard/static/app.js`): `[data-sendback-open]` joins the delegated selector (`:40`) and opens the sheet with the same body as `openTiming` (`:170-176`, recording the trigger's id for focus return); `"sendBackModal"` joins the two id lists that today name `answerModal` and `timingModal` — the focus return in `closeModal` (`:127`) and the enter animation in `afterSwap` (`:324`); and the `htmx:afterRequest` hook that closes the answer sheet on a 2xx (`:308-315`) also closes `sendBackModal` when `e.detail.elt.id === "sendBackForm"`.
**View** (`dashboard/view.go:71-74`): `SendBack *SendBackSheetView` beside `Timing`, with `type SendBackSheetView struct { ProductName string; SendBackView }`.
**The sheet** (`dashboard/templates/needs.html`, new `define "send-back-modal"`, included from `templates/page.html` beside `timing-modal`, `page.html:69-70`): a `<div class="modal-back modal-anim" id="sendBackModal" {{if not .SendBack}}hidden{{end}} hx-swap-oob="true">` swap unit like `timing-modal` (`needs.html:140-195`), its form `id="sendBackForm"`, with the same enter/exit and focus handling. Header: `<product> · <review> · sends <design> back`; title `Revise design`. Body: when `Refusal` is set, that sentence and no form. Otherwise `revision <Sent+1> of <Cap>`, then, when `DesignBytes > PartLimit`, the §3.4 warning, then one row per finding: severity (mono), scope (mono, `overflow-wrap:anywhere`), the note (`dir="auto"`), a `<select name="d<N>">` (blank "—", then the four dispositions) and `<input name="n<N>" maxlength="300" dir="auto" autocomplete="off" placeholder="Note to the designer (optional)">`. Footer: Cancel and `Send back to the designer` (`hx-post`, `hx-disabled-elt`). Every value is printed by `html/template`; nothing is `template.HTML`.
**CSS** (`dashboard/static/app.css`): `.card-reason` (11px, `var(--ink-3)`, `overflow-wrap:anywhere`), `.card-sendback` (the existing small-button size used on the board, `backoffice.html:403`), and the sheet's finding rows on the existing tokens; no new colour. Motion is the existing modal's (`modal-anim`), including its reduced-motion rule.
**Worst cases rendered by TC-497** (the break-ui pass, `.claude/rules/frontend.md`): 40 findings; a 300-rune note in Arabic and a 300-rune note without spaces; a scope of 200 characters; a note holding `<script>` (escaped); zero parsed findings with no refusal (only possible if every line of the artifact failed to split, F8): the sheet shows the sentence `The review's findings could not be read; the design can still be sent back without triage.`, no rows, and the footer.

### 3.10 Docs and registers

- `pustaka/docs/architecture/design-gate.html`: the sentence at `:178-180` ("the clearance is refused until…") becomes the loop in one sentence and a link to a new `h2 id="send-back"` section that says: the operator sends the design back with an optional triage; the design re-opens and the review waits in backlog; the designer revises in its own session with the design and the findings in its packet; the review re-runs on a fresh session; three send-backs, then cancel; no override and why; the reroll closed on unblock and on the drift release; the soft boundary of §3.5. "five rules" → "six rules" at `:170` and `:212`.
- `pustaka/docs/assets/toc/architecture.js:104`: "the five rules" → "the six rules"; `:116`'s "until the back office has a real identity" → the send-back loop in one clause.
- `pustaka/docs/product/brd.html`: BR-48 (`:1327`) becomes: "An open Blocker stops the dispatch and holds the review in the operator's queue. The operator resolves it by sending the design back (<a href="#rules-design-review">BR-57</a>); nothing clears the gate without a fresh review. The send-back records the signed-in account that made it<ACTOR-TAIL>. The cancel that ends a loop at the cap is not attributed. The operator's former power to clear the gate directly is replaced on purpose by the contested send-back and a fresh review. *(D-068, D-070, D-079, D-083)*" — `<ACTOR-TAIL>` per §13. New **BR-57** after BR-50 in §9.7: "A design the review blocked goes back to its author only by an operator's send-back, at most three times per design; then the work is canceled. The design re-opens to ready with its own previous version, the review's findings and the operator's optional triage in its packet, in its own session where the session survives; the review waits in backlog on its dependency and, when the revision is delivered, runs on a fresh session that sees the new design and the rubric only. The revision is told to state its reasons as properties of the design without referring to the review; whether it does is not checked, so the revised design may carry the author's reading of the review to the fresh reviewer. The reviewer's workspace has no read boundary to the author's (BR-47). A review blocked by its own verdict is never re-run on a design that was not delivered again — not by hand, not by a Blueprint sync, not through an approval of a design that delivered nothing; a revision that changes little is a new design, and the cap, not an identity check, bounds how often the gate is re-sampled. The operator's triage notes are not stripped as BR-53 strips external text: the operator is the trusted party, and the notes reach only the author, quoted. *(D-083)*". "Fifty-six rules" → "Fifty-seven rules" (`brd.html:1217`).
- `pustaka/docs/product/open-questions.html`: the register table's status cells of OQ-11 (`:103`, "Open — narrowed by D-026, D-079" → "…, D-079, D-083") and OQ-34 (`:120`, gains "narrowed by D-083"); OQ-11's D-079 paragraph (`:514-519`) and OQ-34 (`:806-813`) each gain a "Narrowed by D-083" sentence: a send-back records its signed-in account on its events. The rest of the sentence depends on the merge order (§13).
- `pustaka/docs/product/decision-log.html`: the D-083 row (text §3.11) after D-081 / D-082; the entry count per §13.
- `pustaka/docs/assets/toc/product.js`: the decision-log `desc` count, the QA counts (§5), and the BRD's if it carries one (it does not today: `grep -n "Fifty-six" pustaka/docs/assets/toc/product.js` finds nothing).
- `deploy/check-workspace.sh`: `:112-116` (the Go test count and its words), `:127-128` (the QA case words), `:129` (the decision count), `:131` (the rule count). `.claude/rules/qa-test-plan.md`: the highest/next TC, the four counts, the range map row, the Go test totals, the BRD and decision counts.
- `pustaka/docs/changelog.html` + `pustaka/docs/assets/toc.js` `product.semver`.
- `pustaka/docs/architecture/graph.html` is not changed: it does not list payload keys (`graph.html:130-135`).

### 3.11 D-083 (text)

**A design the review blocked is sent back to its author by the operator, revised with its findings in its packet, and reviewed again on a fresh session; three times at most; no override.** Context: wf-9 on production stalled — a valid review with one Blocker left the design `DONE`, the review `BLOCKED` and the implementation held, with no move that could change the design; and unblocking the review, by hand or by a Blueprint sync, would only re-run it on the same design. Decision: a board action `send-back` on the blocked review re-opens the design `DONE → READY` (the one guarded exit from `DONE`) and puts the review back in `PENDING` on its dependency; both moves record the signed-in account, the design's carries the operator's optional triage in the four dispositions; the design's next packet carries its previous version, the review artifact and the triage, and an instruction to state its reasons as properties of the design; it resumes its session where the session survives (D-056); the scheduler promotes the review when the revision is delivered, and the review opens a fresh session decided from the event stream (D-069); a review blocked by its own verdict is never unblocked by hand or by the drift release. Rejected: re-running the review on the same design (a reroll, not a review); a new task per revision (the graph's edges would move); a new status or event type (closed); an artifact kind for the triage (a schema statement for what the event already carries); a clearance with no review (a gate one account can open alone) — the brief's conditional override is a contested send-back that a fresh review clears; a verbatim check on the revision (it refused what a CONTEST asks the designer to keep, and missed paraphrase). Consequences: `DONE` is no longer terminal for a design task — `canTaskTransition(DONE, READY)` is true and `moveTaskLocked` keeps it to the send-back; card timing books a left `DONE` segment as blocked, a convention that keeps the partition exact rather than a claim about why the time passed; the gate reads a review's verdict from its anchored line; design and review tasks are no longer approvable; a QA-added card is no longer released by a Blueprint sync (BR-54); the revised design may carry the author's reading of the review, and a barely changed revision is a new design whose re-review the cap bounds and the run caption records. The send-back's account is kept in the append-only stream (payload and caption) for as long as the stream is (BR-04).

## 4. Failure modes

| # | What goes wrong | What happens |
|---|---|---|
| F1 | Two operators send back at once | `SendBackDesign` runs under `s.mu` (and ArcadeStore's `mutate` lock); the second finds the review `PENDING` and is refused by check step 3. |
| F2 | The revision carries no design block | It lands in `REVIEW` (the runner's default, `runner.go:275-284`); `approve` is refused for a design task (§3.6), so the operator `redo`es it (same session) or cancels; the review waits in `PENDING`. Were it somehow `DONE`, §3.6a blocks the review with `design-missing`. |
| F3 | The revision is screened | `BLOCKED` with `design-screened-out`; unblock re-runs it; the review waits. |
| F10 | The design is larger than a packet part | Dropped from the revision's packet by the egress pass; the sheet warns before the send-back; the session carries it (§3.4). |
| F4 | The design is canceled mid-revision | The review stays `PENDING` with an unmet dependency; the exit is cancel on it and on the implementation. |
| F5 | Restart between send-back and delivery | Events, statuses and reasons persist in one transaction; the count, the triage and the fresh-session decision are read from events after boot. |
| F6 | The triage carries a credential | Refused at write step 4, before the append-only write. |
| F7 | A Blueprint sync while the review is `design-blocked` | `releaseDriftLocked` skips it (§3.6). |
| F8 | A review artifact whose lines do not split | `reviewFindingsOf` skips them; the sheet lists fewer findings; the packet still carries the whole body. |
| F9 | The operator sends back with no triage | Allowed; the triage list reads `- none`. |

## 5. Tests and the register

Ten new top-level tests, one register row each, all in `dashboard/`. New file `dashboard/design_iterate_test.go` unless named otherwise; it reuses `gateFixtureNew`, `finish`, `designReply`, `reviewReply` (`dashboard/designgate_test.go:31-197`).

| TC | Test (file) | Kind | Asserts |
|---|---|---|---|
| TC-490 | `TestAnOperatorSendsABlockedDesignBackToItsAuthor` | Positive | `reviewVerdictOf` of `renderReviewArtifact` output: `accept` / `iterate`, with and without a `drift:` line, and a cleared review still yields `GateCleared`; design `DONE → READY`, review `BLOCKED → PENDING`, each one `TaskMoved` with `Rule == "send-back"` and `Actor`; the design's carries the kept `Triage` sorted by N; the review's reason empty; the implementation `BLOCKED` with `design-unreviewed`; the toast text; caption `… by <actor>` on these two and on no other move; `reviewFindingsOf(renderReviewArtifact(...))` round-trips notes holding double spaces; `reviewVerdictOf` ignores a note that says `verdict: accept` |
| TC-491 | `TestTheRevisionCarriesItsDesignTheFindingsAndTheTriage` | Positive / Negative | the design's next packet has `design.md` (the previous design) and `findings.md` = review body + `renderTriage` output, compared exactly, with `revision 1 of 3`, the instruction, and a note holding `"` escaped; no part of the packet, nor `RenderPrompt` of it, contains the actor; `sessionForLocked(design)` resumes the last session; a design never sent back has neither part |
| TC-492 | `TestTheReviewReturnsToReadyWhenTheRevisionIsDelivered` | Positive / Negative | delivery of the revision: design `DONE` with a second design artifact and the `RunExited` caption `revision 1 changes <k> of <m> lines`; a design forced to `DONE` with no new artifact leaves its review `BLOCKED` with `design-missing`; under manual, `RunOnce` moves the review `PENDING → READY` with rule `schedule`; a clearing re-review releases the implementation and its packet carries the new design and the new findings |
| TC-493 | `TestTheReReviewOpensAFreshSessionAndSeesOnlyTheNewDesign` | Positive / Negative | after a send-back: `sessionForLocked(review)` → `("", "reviewer")`; the review packet's `design.md` is the new document and no part holds the triage note or the old findings; after the re-review's own dispatch move, a retry resumes; the decision holds when every event and run carries the same second; send back, dispatch, requeue without a run, dispatch again → still fresh; the reviewer workspace reset ran before the re-review; after a Blueprint republish the re-review artifact's `drift:` line names the revision's pin and the re-review run's pin (§3.3a) |
| TC-494 | `TestOnlyADesignLeavesDoneAndOnlyBySendBack` (`taskstate_test.go`) | Negative | `moveTask` refuses `DONE → READY` for implement and review tasks; `moveTaskLocked` refuses it for a design under every other rule (`approve`, `redo`, `retry`, `unblock`, `schedule`, `requeued`) and allows it under `send-back`; `DONE → FAILED` and `CANCELED → READY` still refused |
| TC-495 | `TestSendingADesignBackIsRefusedUnlessItsReviewBlockedIt` | Negative | an empty actor, each of check steps 1-5 and each triage rule (out-of-range N, a repeated N, a bad disposition, 301 runes, U+2028, U+202E, a reserved fence, a credential) refused with its sentence; no event written, nothing moved |
| TC-496 | `TestADesignIsSentBackAtMostThreeTimes` | Negative | three full loops succeed; the fourth is refused with the cap sentence; the board card's `sendBack` is `{sent: 3, open: false}` with no `ready` in `moves`; the sheet shows the sentence and no form |
| TC-497 | `TestTheSendBackRoutesAreBackOfficeAndSameOrigin` | Positive / Negative | JSON without a session → 401; HTMX without a session → the login sheet; `POST` with `Sec-Fetch-Site: cross-site`, with `same-site`, and with a foreign `Origin` → 403 each; a JSON or multipart content type → 415; a triage in the query string is ignored; the sheet warns for a design over the part limit; another product's id → 404; signed in → 200 and the events' `Actor` is the session email; the sheet lists the findings escaped; the server board shows `blocked: design-blocked` and the button; the §3.9 worst cases render with no unescaped text |
| TC-498 | `TestALeftDoneSegmentIsBookedAsBlocked` (`timing_test.go`) | Positive | a history built from hand-made `TaskPlanned` and `TaskMoved` events (no store needed) of a design sent back once: the first `DONE` segment's bucket is `blocked`; lead = Σ buckets exactly |
| TC-499 | `TestTheGatesVerdictIsNeverRerolled` (`board_test.go`) | Negative | `MoveCard` refuses unblock of a `design-blocked` review with the §3.6 sentence, and refuses `approve` of a design and of a review task in `REVIEW`; the board's `moves` offer neither; an unblock clears the reason; `releaseDriftLocked` leaves `design-blocked`, `design-unreviewed` and `qa-epic-added` tasks `BLOCKED` and releases a drift-blocked task carrying any other reason; a `review-failed` review still unblocks |

**Changed:** TC-208 (`TestEveryUndeclaredTaskTransitionIsRefused`, `qa-test-plan.html:299`, prose at `:1718`): the `{DONE, READY}` pair is removed from its list; "Fifteen undeclared task moves" → "Fourteen". TC-308 (`TestTheTaskFoldReproducesTheBoard`): `foldedBoard` clears the gate's decoration (§3.9); its prose gains the sentence. A `storeContract` case `"ASendBackRoundTrips"` (`store_conformance_test.go:41-89`), covered by TC-124 / TC-125: on both stores, a send-back persists the design's and the review's statuses and reasons and the events' `Actor` and `Triage` across a reopen (the Arcade half needs `GAOS_ARCADE_TEST_URL`).

**Scenario prose:** one new `h3 id="go-send-back"` under `#implemented`, after the card-timing section, with the positive/negative pair for each of the ten and each test's `t.Fatalf` / `t.Errorf` messages quoted from the file.

**Counts** — see §13 for both merge orders.

## 6. Alternatives

- **A1. Unblock re-runs the review (today).** Rejected: the design has not changed, so the review is a reroll; a reroll that eventually clears is a bypass.
- **A2. A new design task per revision** (a plan addition, as the QA catalog adds a card, D-072). Rejected: the review's and the implementation's `DEPENDS_ON` edges name the old design; moving an edge is a graph edit with no event, and D-056's resume would not apply to a new task.
- **A3. A new status `REVISING` or a new event `DesignSentBack`.** Rejected: both vocabularies are closed, and `TaskMoved` with a rule says the same.
- **A4. An artifact kind `triage`.** Rejected: a schema statement (`schema/14-design-gate.sql:36`) for a record the append-only `TaskMoved` already carries.
- **A5. Operator override** — clear the gate by marking every Blocker CONTEST or DECLINE. Left out: (a) D-069 clause 6 makes the gate the only writer of `DONE` for an unapproved task; an override is a human writing it with nothing reviewed; (b) BR-55 is one trust domain, so "one signed-in account said so" has no second person to check it; (c) the send-back already carries CONTEST — the designer may keep the design and argue for it as design rationale, and a fresh reviewer decides. If production shows the cap hit by reviewers that are wrong, an override is its own design with its own review.
- **A6. Resume the reviewer's session on re-review.** Rejected, §3.5.
- **A7. The review stays BLOCKED with a reason `design-revising`, and a hook in `finishRun` returns it to READY on delivery** (v1 of this design). Rejected in round 1: `BLOCKED → PENDING` lets the existing dependency edge and the scheduler do it, with no new reason, rule or hook.
- **A8. One free-text note instead of per-finding triage** (round-1 zoom-out). Declined: the brief asks for a disposition per finding in the four-word vocabulary, and a per-finding record is what lets the designer and the audit see which finding was contested.

## 7. Change list

| # | File | Change |
|---|---|---|
| 1 | `dashboard/taskstate.go` | the send-back row; `moveTask`'s defensive kind check |
| 2 | `dashboard/runner.go` | `moveTaskWithLocked`; `moveTaskLocked`'s DONE guard |
| 3 | `dashboard/designgate.go` | names (§3.2); `blueprintPinsLocked` takes the judged run (§3.3a); `sendBack`, `sendBackCheckLocked`, `sendBacksLocked`, `reviewOfDesignLocked`, `reviewVerdictOf`, `reviewFindingsOf`, `normalizeTriage`, `renderTriage`, `operatorHolds`, `reviewSentBackSinceRunLocked`, `MemStore.SendBackDesign`, `MemStore.SendBackSheet`; `gateOutcome.note` and the revision's lines-changed caption, and the review precondition (§3.6a); `gateStateLocked` reads `reviewVerdictOf` |
| 4 | `dashboard/model.go` | `EventPayload.Actor`, `.Triage`, `FindingTriage`, `Clone` |
| 5 | `dashboard/store.go` | `SendBackDesign`, `SendBackSheet` on `Store`; `readOnly.SendBackDesign`; `SendBackView` |
| 6 | `dashboard/store_arcade_blueprint.go` | `ArcadeStore.SendBackDesign` via `mutate`, `ArcadeStore.SendBackSheet` via `proj` |
| 7 | `dashboard/store_mem.go` | the design-task block in `freezePacketFor` |
| 8 | `dashboard/turns.go` | the fresh-reviewer case |
| 9 | `dashboard/workspace.go` | `releaseDriftLocked` skips the gate's reasons |
| 10 | `dashboard/board.go` | `MoveSendBack`; `MoveCard`'s unblock refusal, its `approve` refusal for design and review tasks, and its clearing of `BlockReason` on unblock; `BoardCard.Reason`, `.SendBack`, `BoardSendBack`, `decorateBoardGate` |
| 11 | `dashboard/timing.go` | a left terminal segment is booked as blocked |
| 12 | `dashboard/handlers_board.go`, `dashboard/handlers_bo.go`, `dashboard/main.go`, `dashboard/view.go` | the two routes; `View.SendBack`, `SendBackSheetView` |
| 13 | `dashboard/templates/backoffice.html`, `templates/needs.html`, `templates/page.html` | reason line, button, `send-back-modal` |
| 14 | `dashboard/static/board.js`, `static/app.js`, `static/app.css` | the client board's elements; the sheet opener; the classes |
| 15 | `dashboard/design_iterate_test.go` (new), `taskstate_test.go`, `board_test.go`, `timing_test.go`, `taskfold_test.go`, `store_conformance_test.go`, `auth_test.go` | §5 |
| 16 | `pustaka/docs/architecture/design-gate.html`, `pustaka/docs/assets/toc/architecture.js` | §3.10 |
| 17 | `pustaka/docs/product/brd.html`, `decision-log.html`, `open-questions.html`, `qa-test-plan.html` | BR-48, BR-57, OQ-11, OQ-34, D-083, the register |
| 18 | `pustaka/docs/assets/toc/product.js`, `pustaka/docs/assets/toc.js`, `pustaka/docs/changelog.html` | counts, semver, release note |
| 19 | `deploy/check-workspace.sh`, `.claude/rules/qa-test-plan.md` | the counts |

## 8. Risks

- R1. **`DONE` stops being terminal for design tasks.** One guard where kind and rule are both known (§3.1), one defensive check, TC-494.
- R2. **Operator and reviewer text reaches the author's prompt, and through the author possibly the re-reviewer.** The operator's notes are bounded, refused for control and invisible characters and reserved fences, screened before they are stored, and rendered as quoted literals; they go only into the author's packet (TC-491, TC-493). The revised document is told to state its reasons without referring to the review; nothing checks it (§3.5). BR-53's stripping (backticks, angle brackets) is for text from an external tool (`brd.html:1346`); the operator is the trusted party here and the note is never interpreted as markup, so the narrower rule applies.
- R3. **An operator's email in the append-only stream.** In the payload and in the caption of the two send-back events, which the BO timeline prints (`backoffice.html:430`). That is the audit trail BR-48 asks for, visible only to signed-in BO accounts; the email never enters a packet (TC-491). It is permanent (BR-04).
- R4. **The reviewer's workspace has no documented read boundary to the author's** (BR-47, `brd.html:1326`). Inherited, unchanged, stated in BR-57's docs.
- R6. **Review notes shown on the sheet may hold bidi or zero-width characters** (`ParseReview` refuses only line breaks and NUL, `designgate.go:344`). The sheet prints them escaped with `dir="auto"`; refusing them at parse time is a change to the review contract, outside this design (ESCALATE).
- R5. **Cost.** Each send-back buys a design turn and a review turn; three at most per design; under `manual` each still needs an operator dispatch.

## 9. The move for wf-9 on production (after a deploy the operator runs)

1. Back office → product p-6 → Board. Card **wf-9-t-4** (Blocked) shows `blocked: design-blocked` and **Revise design**.
2. Revise design → the sheet lists the four findings. Triage any of them (for example the Blocker `PROPOSE FIX`, a nit `DECLINE` with a note), or none. **Send back to the designer.**
3. **wf-9-t-3** moves to Ready; **wf-9-t-4** to Backlog; **wf-9-t-5** stays Blocked with `design-unreviewed`. Under `GAOS_RUNNER=manual`, drag wf-9-t-3 to Working (dispatch). It runs in its own session with its previous design and the findings.
4. When it is delivered, the scheduler moves **wf-9-t-4** to Ready. Dispatch it. It reviews the new design on a fresh session.
5. Clears → **wf-9-t-5** goes to Ready. Blocks again → back to step 2, at most three send-backs in all; then cancel wf-9-t-4 and wf-9-t-5.

## 10. Rollout and rollback

No migration: no schema statement; the payload fields are additive and absent on old rows. **Rollback** is a revert, with one step first: list the reviews that are `BLOCKED` / `design-blocked`, the design tasks between a send-back and a delivery, and every review whose newest `send-back` move has no later `RUNNING` move (the §3.5 predicate — an older build would resume its pre-send-back session), and resolve or cancel them, because the older build's unblock re-runs a review on the same design and its drift release releases every blocked task (SEC-7). An older build replays a `send-back` `TaskMoved` through the fold (which applies `To`) and ignores the unknown payload fields.

## 11. Approval

**Approved (v4)** by the orchestrating agent under the product owner's standing instruction ("work among your agents, never as my confirmation") — approval by delegation, not by the product owner in person. The one trade-off a product owner might weigh is recorded here rather than buried: the brief's conditional override is delivered as a contested send-back plus a fresh review (N1), and BR-48 replaces the operator's direct clearance with it. No Blocker and no Critical or High security finding is open: round 3's Blocker and High are closed as the status line says, and every other finding of the three rounds is dispositioned in the three triage files.

## 12. Phases

Each phase ends with its command green before the next starts. The phases are checkpoints inside **one** commit: the register rows, prose and counts (P4) land in the same commit as the tests (the QA rule).

| Phase | Changes (§7 rows) | Exit |
|---|---|---|
| P1 — the state machine and the timing | 1, 2, 4, 11, and the `MoveSendBack` constant of row 10; TC-494, TC-498; the TC-208 edit | `cd dashboard && go vet ./... && go test ./...` |
| P2 — the store, the packet, the sessions, the reroll, the board decoration | 3, 5-10; TC-490…TC-496, TC-499; the conformance case; the `foldedBoard` (TC-308) edit | `go vet ./... && go test ./...`, with and without `GAOS_ARCADE_TEST_URL` |
| P3 — routes and UI | 12-14; TC-497; `auth_test.go` | `go vet ./... && go test -race ./...` (TC-497 renders the server board and the sheet through the real handlers and templates); `node --check dashboard/static/board.js && node --check dashboard/static/app.js` where `node` is installed. The client board's runtime behaviour (the sisflow card elements, focus return) has no automated check; that gap is stated in the PR's Testing section, as for the earlier board features |
| P4 — docs and counts | 16-19 | `cd pustaka && go run . check ./docs && go test ./...`; `./deploy/check-workspace.sh`; `./deploy/check-stacks.sh` |

## 13. Numbers under the two merge orders

| | conversations merged first (expected) | this merges first |
|---|---|---|
| Version | v0.43.0 | v0.42.0 (conversations then takes v0.43.0) |
| Decision | D-083; "Eighty-two entries" (`decision-log.html:18`, `check-workspace.sh:129`, the rule file) and "Eighty-two decisions" (`toc/product.js:129`) | D-082; "Eighty-one entries" / "Eighty-one decisions" |
| `EventPayload.Actor` | already defined by conversations with the same meaning; this change adds `Triage` only, and its actor reaches the `actor` column through conversations' binding | defined here; the `actor` column keeps its literal; conversations adds the binding at its rebase |
| `<ACTOR-TAIL>` in BR-48, and the OQ-11 / OQ-34 sentences | ", as the archive and prune actions do (D-082); the other board moves are not attributed yet" | "; no other move is attributed yet" |
| BRD rule | BR-57; "Fifty-seven rules" | BR-57; "Fifty-seven rules" |
| Tests | TC-490…TC-499; highest TC-499, next TC-500 | TC-478…TC-487; highest TC-487, next TC-488 |
| Register rows | 499, "four hundred and ninety-nine" | 487, "four hundred and eighty-seven" |
| Go tests | 375, "three hundred and seventy-five"; `dashboard/` 353 | 363, "three hundred and sixty-three"; `dashboard/` 341 |
| Range map row | `TC-453…TC-499` | `TC-453…TC-487` |

Whichever branch merges second renumbers its own claims at rebase; the code carries no number.
