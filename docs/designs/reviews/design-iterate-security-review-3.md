# Security review: design-iterate (v3)

## What this proposes

Today a design that draws a Blocker from the zero-context reviewer leaves the workflow stuck. The design task is `DONE`, and `DONE` has no exit. The review is `BLOCKED` with reason `design-blocked`. The implementation is held by the gate. Unblocking the review only re-runs it on the same document, which is a "reroll", and a Blueprint sync's drift release does the same without anyone asking.

The document proposes a `send-back` action that a signed-in operator takes on such a review:
- **The moves.** The design moves `DONE → READY`. This is a new edge, limited to design tasks and the `send-back` rule. The review moves `BLOCKED → PENDING` and waits on its existing `DEPENDS_ON` edge. Both `TaskMoved` events record the session email as `Actor`, and the design's event carries an optional per-finding triage (CONTEST / ESCALATE / PROPOSE FIX / DECLINE, plus a note of at most 300 runes).
- **The designer's next packet.** It carries the previous design, the review artifact and the rendered triage, along with an instruction not to quote any of them. The designer resumes its own session.
- **The re-review.** When the revision is delivered, the scheduler promotes the review. Its session opens fresh, decided from the event stream. Its packet holds only the new design and the rubric.
- **The cap.** At most 3 send-backs per design, counted from the events. There is no operator override.
- **Closing the reroll.** The board's `unblock` of a `design-blocked` review is refused. The drift release skips `design-blocked` and `design-unreviewed`. `approve` is refused for design and review tasks. A revision that copies a note of 24 runes or more is refused, as is a byte-identical revision. A re-review needs more design artifacts than send-backs.
- **The surface.** Two back-office routes: `GET /ui/send-back/{id}/{taskId}` (the sheet) and `POST /products/{id}/board/cards/{taskId}/send-back`. Board decoration shows the block reason, a "Revise design" button, and a line when the limit is reached.
- **The records.** BR-48 is amended, BR-57 and D-083 are added, OQ-11 and OQ-34 are narrowed, and TC-490 to TC-499 are added.

## How the existing system works, per this document

- Every board move goes through `moveTaskLocked` and writes one `TaskMoved` with `Rule`, `Reason` and `To`. The task fold replays `To` without rebuilding `BlockReason`.
- `gateVerdict` records design and review artifacts after an egress screen of the body, and moves the review to `DONE` or to `BLOCKED/design-blocked`. `gateStateLocked` reads the newest review artifact to decide whether the implementation is released.
- Back-office routes are session-gated and same-origin unless they are on the public or bearer list.
- A review's first attempt always opens a fresh session (D-069). Packets travel as prompt text, never as files. The reviewer workspace is reset in `clearFor` before every review turn.

## Trust boundaries touched

| Boundary | Authenticates | Authorises | Validates | Far side fails |
|---|---|---|---|---|
| Operator browser → back office (both new routes) | Signed, expiring MAC cookie, checked against the account and its credential fingerprint (`dashboard/auth.go:361-382`, `:323-344`). **The rubric's premise that `signedIn()` accepts any cookie is out of date:** BR-55 (`brd.html:1348`) is implemented. | Any signed-in account (N6, a single trust domain). Product scoping is `t.DeskID == productID` (design:134). | `crossOriginOK` on POST (`auth.go:560-577`), with a `SameSite=Lax` cookie (`auth.go:493`). Body capped at 256 KiB. Triage normalised and egress-screened before the append-only write. | A refusal writes no event (the checks run before the writes, design:156). |
| Dashboard → gateway (designer and re-reviewer packets) | Unchanged | Unchanged | The egress pass at dispatch, plus the up-front screen of the triage | Unchanged |
| Dashboard → ArcadeDB | Unchanged | Unchanged | One `mutate` transaction for both events and both records | Restart-safe (F5) |
| Author workspace ↔ reviewer workspace | — | — | Write boundary only (BR-47, `brd.html:1326`) | Inherited limit |

**Abuse cases already handled:**
- Forged cross-site POST: refused, and the cookie is Lax.
- IDOR across products: 404.
- Double send-back: serialised under `s.mu`, the second is refused at check step 3.
- Oversized payload: capped.
- Credential in the triage: screened before storage.
- Control or bidi characters and reserved fences in operator notes: refused.
- Actor forgery: `Actor` comes only from the session.

**No other route reaches DONE.** `approve` in `MoveCard` is the only human route to `DONE` (`dashboard/board.go:295`). The legacy `/tasks/{id}/approve` acts on desks, not task status (`dashboard/store_mem.go:894-910`), so the new `approve` refusal is complete.

## Findings

### SEC-1 The byte-identical check leaves the reroll open, up to three fresh reviews of an essentially unchanged design - Severity: High
- Component: design gate, re-review (§3.6a)
- Evidence: `docs/designs/design-iterate.md:286` (the identical-revision refusal compares bytes only); `docs/designs/design-iterate.md:37` and `:400` (the design itself defines re-running a zero-context reviewer on an unchanged design as "a gate bypass by repetition"); `docs/designs/design-iterate.md:404` (an override is refused because "one signed-in account said so" has no second check); `docs/designs/design-iterate.md:202` (the CONTEST path invites the designer to keep the design).
- Attack path: Any signed-in account can CONTEST every finding and send the design back. The designer, following CONTEST, can deliver the same design with one added sentence or a whitespace change. That passes the byte comparison, opens a fresh, non-deterministic reviewer, and repeats up to three times. A single account therefore gets three rerolls of the gate. That is the same unchecked one-person power A5 refuses as an override, gained by repetition instead. The designer model, if a product source steers it, can do the same on its own.
  - Why High and not Medium: the design itself names this mechanism a bypass and says it closes it on every route (G6, design:53). The control that is supposed to close it here is trivially bypassed, so this is a missing control on a privileged path, not merely an incomplete defence.
- Remediation:
  1. Compare revisions after the same normalisation the verbatim check uses (design:257).
  2. Record the size of the change, for example lines changed against the previous design artifact, on the design's `design-delivered` move and on the re-review artifact's header. A near-unchanged resubmission is then visible in the audit trail and to the operator on the sheet.
  3. State in BR-57 and D-083 that the cap of three is the explicit upper bound on re-reviews of a contested, substantially unchanged design. A reader should not believe the reroll is closed.
- Effort: small

### SEC-2 The send-back's audit is partial: the database `actor` column contradicts the payload, and the moves that end the loop are unattributed - Severity: Medium
- Component: event store, audit (§3.2, §13)
- Evidence: `docs/designs/design-iterate.md:115` and `:474` (under the "this merges first" order the `actor` column "keeps its literal"); `dashboard/store_arcade.go:1068` (the literal is a fixed operator label written on every event); `docs/designs/design-iterate.md:294` (the cap exit is `cancel` on the review and the implementation); `docs/designs/design-iterate.md:66` (N7: no other move is attributed).
- Attack path: An auditor querying ArcadeDB's `actor` column gets the same literal for the send-back as for every machine move, while the payload names an email. The two records disagree. The act that actually throws a story away, cancelling the review and the implementation at the cap, records no account. Within BR-55's one trust domain, an account can cancel gated work with no trace of who did it, and BR-48's amended text (design:349) implies the gate's outcome is now attributed.
- Remediation:
  1. Under either merge order, bind the `actor` column to `Payload.Actor` when it is set, keeping the literal otherwise, so the column and the payload never disagree.
  2. At minimum, extend `Actor` to `cancel` on a review or implementation that is gated by the design gate, so both ends of the loop are attributed.
  3. Otherwise, make the BR-48 wording say explicitly that the cancel that ends the loop is not attributed.
- Effort: small

### SEC-3 The triage is read with `r.FormValue`, which accepts URL query parameters and multipart bodies - Severity: Low
- Component: `handleSendBack` (§3.8)
- Evidence: `docs/designs/design-iterate.md:305-306`.
- Attack path: `FormValue` merges the query string, so a triage note, including one carrying a credential the store-side screen would later refuse, can arrive in the URL. A URL is written to proxy and tunnel access logs before any screen runs. Only `application/json` is refused, so multipart is parsed too. This needs a hand-built request, not the shipped form, which is why it is Low.
- Remediation: Call `r.ParseForm()` on the `MaxBytesReader`-wrapped body, read with `r.PostFormValue`, and refuse any `Content-Type` other than `application/x-www-form-urlencoded` with the 415 shape. Add a TC-497 case: a triage in the query string is ignored.
- Effort: quick-win

### SEC-4 The reviewer's finding notes reach the new sheet without the invisible-character refusal applied to operator notes - Severity: Low
- Component: send-back sheet (§3.9), review parser
- Evidence: `dashboard/designgate.go:344` (`ParseReview` refuses only line breaks and NUL in a note); `docs/designs/design-iterate.md:155` (operator notes are refused for Cc, Cf, Zl and Zp, the fix the design labels SEC-5); `docs/designs/design-iterate.md:341` (reviewer notes rendered with `dir="auto"`).
- Attack path: A review note carrying bidi overrides or zero-width characters displays differently from its stored text. The operator then triages a finding whose on-screen meaning differs from what the designer receives. `html/template` escaping does not neutralise these characters.
- Remediation: Apply the same `unicode.In(r, Cc, Cf, Zl, Zp)` refusal to review notes in `ParseReview`, which is a parse failure (`review-failed`), and add a U+202E note to the review-note tests.
- Effort: quick-win

### SEC-5 An operator note can close its own quotation inside the designer's prompt - Severity: Low
- Component: `renderTriage` (§3.4)
- Evidence: `docs/designs/design-iterate.md:199`, `:205` (the note is written inside double quotes "so it reads as the operator's words and not as an instruction of GAOS's"); `:155` (no rule on `"`).
- Attack path: A note that contains `"` ends the quoted span early. The rest of the note then reads as GAOS's own text in the designer's packet, for example contradicting the "do not quote this review" instruction that the SEC-1 boundary relies on. The operator is trusted, but N6 makes that any signed-in account.
- Remediation: Render the note with Go `%q`-style escaping, or refuse `"` in `normalizeTriage`, and assert it in TC-491 or TC-495.
- Effort: quick-win

### SEC-6 The sheet URL is concatenated in the template rather than path-escaped like its sibling - Severity: Low
- Component: server board template (§3.9)
- Evidence: `docs/designs/design-iterate.md:337` (`data-sendback-open="ui/send-back/{{$id}}/{{$card}}"`); `dashboard/timing.go:374-376` and `dashboard/render.go:141` (`timingURL` path-escapes both ids for the same pattern).
- Attack path: Ids are generated by the server today, so there is no live path. A future id containing `/`, `?` or `..` would make the button fetch a different back-office route. This is hardening.
- Remediation: Add a `sendBackURL` template func that mirrors `timingURL`, and use it in both the server board and `board.js`.
- Effort: quick-win

### SEC-7 BR-57's text omits the inherited read-boundary limit that §3.5 and R4 promise it states - Severity: Low
- Component: domain docs (§3.10)
- Evidence: `docs/designs/design-iterate.md:260` and `:438` ("stated in BR-57's docs"); `docs/designs/design-iterate.md:349` (the proposed BR-57 text mentions only the undetected paraphrase); `pustaka/docs/product/brd.html:1326` (BR-47: a write boundary, not a read boundary).
- Attack path: The triage, which is the author's side of the argument, now enters the author's session for the first time. A designer that writes it to its own folder leaves it readable from the reviewer's sibling folder. The rule a later reader consults does not say so.
- Remediation: Add one clause to BR-57 citing BR-47's missing read boundary, and link it from the new `#send-back` section of `design-gate.html`.
- Effort: quick-win

Counts: Critical=0 High=1 Medium=1 Low=5
