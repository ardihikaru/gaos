# Security review: design-iterate (`docs/designs/design-iterate.md`)

## Paraphrase

**What this proposes**

Today, when a design review finds a Blocker, the workflow is stuck for good. This design gives a signed-in back-office operator a "Revise design" action on a review card that the gate blocked with reason `design-blocked`.

- **The send-back.** The design task, which is `DONE`, goes back to `READY` under a new move rule, `iterate`. Two guards keep it to that: the state machine refuses `DONE → READY` for any task that is not a design, and `moveTaskLocked` refuses any move out of `DONE` unless the rule is `iterate`.
- **What is recorded.** The move records the operator's email (`Actor`) and an optional triage of each finding (one of four dispositions and a one-line note of at most 300 characters). Both are stored on the `TaskMoved` event. The note is checked for length, line breaks and reserved fences, and is run through the egress secret screen before anything is written.
- **The revision.** The designer's next attempt resumes its own session. Its packet carries the review artifact plus the rendered triage as `findings.md`.
- **The re-review.** When the revised design is delivered, the review returns to `READY` by itself (rule `design-revised`). Its next attempt opens a fresh session, chosen by comparing the start time of the newest design run against the review's last run. Its packet holds only the new design and the rubric.
- **The reroll is closed.** A review blocked by its own verdict, or waiting on a revision, can no longer be unblocked by hand.
- **The cap.** At most 3 send-backs per design, counted from events. After that the only exit is to cancel.
- **The routes.** Two new back-office routes: `GET /ui/iterate/{id}/{taskId}` (the triage sheet) and `POST /products/{id}/board/cards/{taskId}/iterate`.
- **What it does not do.** No operator override, no new status, no new event type, no schema statement.

**How the existing system works, per this document**

- Every back-office route needs a verified session and must be same-origin, unless it is on the public list or is a bearer route (`auth.go:523-575`). The signed-in email is `sessionOf(r).Email`.
- A review runs in a fresh session, in a reset reviewer workspace, with only the design and the rubric (D-069, D-071).
- The gate is the only thing that writes `DONE` for a design or a review.
- The implementation task is held until its review is `DONE` with `verdict: accept`.
- Events are append-only (BR-04), and the event types are a closed list (BR-05).

## Trust boundaries touched

| Boundary | Authenticates | Authorises | Validates | If the far side fails |
|---|---|---|---|---|
| Operator browser → dashboard (new routes) | A verified signed cookie (BR-55, `auth.go:541-551`) | Any signed-in account (N6, one trust domain) | Same-origin check (`auth.go:560-577`), 64 KiB body cap, triage rules (§3.3 step 7) | Refused with no write |
| Operator text → designer's prompt | Inherited from the session | None | Egress secret screen at send-back and at dispatch (BR-32) | Refused before the append-only write |
| Designer output → reviewer's input (the revised design) | None | None | Egress screen on the design body only (`designgate.go:696`) | **Nothing stops triage or findings being copied through (SEC-1)** |
| Dashboard → ArcadeDB (`actor` column) | Database user | Grants | Parameter binding (`store_arcade.go:1066-1073`) | Same transaction (`mutate`) |

---

### SEC-1 Triage and earlier findings reach the re-reviewer through the revised design - Severity: High
- **Component:** reviewer isolation (BR-47, BR-49, D-069), the design packet, the design contract.
- **Evidence:**
  - The design claims the re-reviewer sees neither the previous findings nor the triage: `docs/designs/design-iterate.md:58` (N3), `:188` (§3.5 reason 2), `:202`, `:355` (R2).
  - The only check is at packet level: TC-493 at `:305`.
  - The designer is handed both the findings and the triage and asked to revise (`:158-173`, `:166`).
  - The revised design body is screened for secrets only before it becomes the reviewer's input (`dashboard/designgate.go:696`). The review packet carries that body verbatim (`dashboard/store_mem.go:1491-1500`).
- **Attack path:**
  - A signed-in operator writes, for example, "CONTEST — this is fine" into a note.
  - Or the previous reviewer (a model whose input was an author-controlled document) writes a note addressed to "the next reviewer".
  - The designer, asked to revise in light of `findings.md`, naturally adds a "Response to review" section quoting the findings and the triage.
  - The "fresh" reviewer then reads exactly the lobbying that §3.5 reason 2 says it must never see. This also gives one signed-in account an indirect way to write into the reviewer's input, which weakens the stated reason for N1 / A5(b): no single account can clear the gate.
- **Why High rather than Medium:** the design presents this isolation as a property it guarantees, and the gate is a security control (A5(b)). Packet-level separation alone does not deliver it.
- **Remediation:**
  1. Add a clause to the revision's design contract (`contractsFor`, `adapter_aimanager.go:134-144`): the design document must not cite, quote or answer the review, its findings or the triage, because a fresh, independent reviewer reads it.
  2. In `gateVerdict` for a design task that has an `iterate` move, refuse the delivered body if it contains any stored triage note, or any finding note of a minimum length, verbatim. Block it with a recorded reason, as `design-screened-out` is.
  3. Extend TC-493 with a fake designer reply that echoes the triage and assert the refusal.
  4. Or, if you accept the leak, restate N3 and R2 as limits instead of guarantees, and put that in BR-57.
- **Effort:** small

### SEC-2 The fresh-reviewer decision depends on second-resolution wall-clock order and fails open - Severity: Medium
- **Component:** `sessionForLocked` and `designRevisedSinceLocked` (§3.5).
- **Evidence:**
  - The decision is the design run's start time compared to the review's last start time: `docs/designs/design-iterate.md:196-200`.
  - The value compared is whole Unix seconds, read once per run: `dashboard/runner.go:122`.
  - If the check returns false, the code falls through to resuming the session (`dashboard/turns.go:142`).
- **Attack path:** a clock step backwards (NTP), a tie within the same second (a fast fake gateway, a recovery path), or a restored database with skewed times makes the check false. The re-review then resumes the reviewer session that holds the previous design and its own earlier findings. That is the anchoring the design says it must prevent (§3.5 reason 1). Nothing records that isolation was lost.
- **Remediation:**
  - Decide from structure, not time. The review's last frozen slice cites the design artifact it reviewed (`cite("Artifact", art.ID, CiteDesign, …)`, `store_mem.go:1497`). Open a fresh session when that ID differs from the newest design artifact's ID.
  - When the cited ID cannot be determined, open a fresh session (fail closed).
  - TC-493 should cover the same-second case.
- **Effort:** small

### SEC-3 The author's session now carries the triage, and the reviewer's sibling folder has no documented read boundary - Severity: Medium
- **Component:** the workspace-level isolation behind N3.
- **Evidence:**
  - BR-47 says the gateway documents a write boundary, not a read boundary, between the author's and the reviewer's sibling folders, and that isolation is "over the packet and the workspace" (`pustaka/docs/product/brd.html:1326`; `pustaka/docs/product/decision-log.html:1427-1428`).
  - The author's workspace is "the key the author's transcript is stored under" (`dashboard/reviewer_workspace.go:11-15`).
  - The design resumes the designer's session with `findings.md` and the triage in its prompt (`docs/designs/design-iterate.md:183`, `:166`).
- **Attack path:** a re-reviewer with file-read tools, possibly steered by an instruction inside the design it is reviewing, reads the author's transcript or files in the sibling folder. Those now contain the earlier findings and the operator's dispositions. The reset of the reviewer workspace (`reviewer_workspace.go:53-61`) does not cover the other folder.
- **Remediation:**
  - State this in N3, R2 and BR-57 as an inherited limit.
  - Add it to §8 as a risk with a link to BR-47.
  - Either confirm and cite a gateway read boundary, or record a follow-up. Do not claim the re-reviewer "sees neither".
- **Effort:** quick-win (documentation); structural (an actual read boundary)

### SEC-4 The actor is not guaranteed, and machine moves stay labelled "operator" - Severity: Medium
- **Component:** the audit trail (G7, BR-48 amendment).
- **Evidence:**
  - An empty actor is accepted and rendered as "by operator": `docs/designs/design-iterate.md:132`.
  - `writeEvent` keeps the literal `'operator'` unless an actor is set: `:112`, `dashboard/store_arcade.go:1068`.
  - The DDL says the column holds "agent id, user id, or 'system'": `dashboard/schema/07-events.sql:26`.
  - The other operator moves stay unattributed, including `cancel`, which is the cap's only exit (`:232`, `dashboard/board.go:295-301`).
- **Attack path:** any future caller of `IterateDesign` (a bearer API route, a seed, a test helper) can send a design back with no attribution, and the record cannot tell it from a human move. Once this lands, every scheduler, gate and recovery move written by `ArcadeStore` is still stored as `actor = 'operator'`. That defeats the "who did it" question BR-48 is amended to answer, and makes a later forensic query on `actor` actively misleading.
- **Remediation:**
  1. `IterateDesign` refuses an empty actor.
  2. `handleIterate` returns 401 if `sessionOf(r)` is not ok, instead of passing an empty email.
  3. `writeEvent` writes `'system'` when `Actor` is empty, as the DDL says.
  4. Either attribute the other board moves too (`MoveCard` already sits behind the same gate), or state in BR-48 that only `iterate` is attributed.
- **Effort:** small

### SEC-5 The note validation allows other control and invisible characters - Severity: Low
- **Component:** triage normalisation (§3.3 step 7).
- **Evidence:** only `\n`, `\r` and `\x00` are refused: `docs/designs/design-iterate.md:130`. The reviewer-note rule it copies does the same (`dashboard/designgate.go:344`).
- **Attack path:**
  - Characters that a model or a Markdown renderer may treat as line or layout breaks still pass: U+2028/U+2029, U+0085, `\v`, `\f`.
  - So do bidirectional overrides and zero-width characters. These can make a note look different in the sheet than it reads in the designer's prompt.
  - The operator is trusted, but reviewer notes shown in the sheet are model output.
- **Remediation:** refuse Unicode categories Cc (except none), Cf, Zl and Zp in triage notes. Apply the same rule in `ParseReview`.
- **Effort:** quick-win

### SEC-6 The verdict is read by substring search over a body that contains model-written notes - Severity: Low
- **Component:** §3.3 step 5, and the existing `gateStateLocked`.
- **Evidence:**
  - Step 5 checks whether the body contains `verdict: iterate` anywhere: `docs/designs/design-iterate.md:128`.
  - The body includes reviewer-authored free-text notes: `dashboard/designgate.go:608-609`.
  - The same pattern guards the gate's release: `dashboard/designgate.go:503-504`.
- **Attack path:** a reviewer note containing the text "verdict: accept" or "verdict: iterate" makes the substring check true regardless of the real verdict line. It is not exploitable along the paths in this design, because state checks come first. But the new check adds a second gate decision on the same weak basis, and SEC-1 widens who can influence reviewer output.
- **Remediation:** parse the anchored `verdict:` line (the second line, written by `renderReviewArtifact`) instead of searching the whole body. Better still, keep the verdict as a structured field on the artifact. Use it in both places.
- **Effort:** quick-win

### SEC-7 A rollback re-opens the reroll that this design closes - Severity: Low
- **Component:** §10 Rollout.
- **Evidence:** `docs/designs/design-iterate.md:369`. The older `legalMove` allows `BLOCKED → READY` for any review (`dashboard/board.go:183-184`).
- **Attack path:** after a revert, a review left in `design-revising` or `design-blocked` can be unblocked by hand. It is then re-run on whatever design artifact is newest, possibly the unchanged one. That is the gate bypass by repetition from §1.
- **Remediation:** add a rollback step to §10: before reverting, list the reviews in `design-revising` and resolve or cancel them, and record that the older build has no reroll guard.
- **Effort:** quick-win

### SEC-8 The operator's email goes permanently into event text, and the registers do not record the narrowing - Severity: Low
- **Component:** personal data in the append-only store, and cross-references.
- **Evidence:**
  - The event `Text` carries "by <actor>": `docs/designs/design-iterate.md:132`.
  - That text is served whole by `GET /events` (`dashboard/handlers_tasks.go:29-44`) and the live stream (`store_mem.go:457`), and is never removed (BR-04).
  - R3 (`:356`) mentions only the payload field.
  - TC-491 asserts "no email in any part" for the design packet only (`:303`).
  - BR-48 currently says recording an actor "touches the closed event catalog" (`pustaka/docs/product/brd.html:1327`).
  - The open-questions register lists "an actor on every event" as open (`pustaka/docs/product/open-questions.html:519`, `:811-813`), and §3.10 (`:271-277`) does not update it.
- **Attack path:** no direct attacker. The email is duplicated into a text field that is permanent and readable by every account, which widens what can never be erased. A later packet builder that includes event text would leak the email to a model provider without any test catching it.
- **Remediation:**
  - Keep the email in `Payload.Actor` only, and leave the text as "by operator", or show the account in the template from the payload.
  - Extend TC-491 to the implementation and review packets and to `RenderPrompt` output.
  - In §3.10, add the OQ-16 / OQ-34 narrowing and the removal of BR-48's "closed catalog" sentence, so the registers agree.
- **Effort:** quick-win

### SEC-9 The sheet route does not say it checks the product - Severity: Low
- **Component:** `GET /ui/iterate/{id}/{taskId}` (§3.8).
- **Evidence:**
  - The sheet answers "404 toast for an unknown card", without saying it checks `DeskID == productID`: `docs/designs/design-iterate.md:238`.
  - Only the POST path's step 1 states that check (`:124`).
  - Per-tenant scoping is still open (`pustaka/docs/product/open-questions.html:518`).
- **Attack path:** a signed-in account requests the sheet with another product's ID in the path and reads that product's review findings. This is an IDOR-shaped gap if tenant scoping arrives later and this route is forgotten.
- **Remediation:** the sheet resolves the card with the same `taskByIDLocked` plus `DeskID == productID` check, or simply calls a shared lookup. TC-497 asserts a 404 for a mismatched product.
- **Effort:** quick-win

Counts: Critical=0 High=1 Medium=3 Low=5
