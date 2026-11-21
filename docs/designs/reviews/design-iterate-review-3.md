# design-iterate — round 3 triage, and approval

**Design reviewed:** `docs/designs/design-iterate.md` v3. **Result:** v4, approved.
**Lenses:** critic (B0 M7 N6), readiness (B1 M3 N4), security (Critical 0 High 1 Medium 1 Low 5). Reports: `design-iterate-critic-3.md`, `design-iterate-readiness-3.md`, `design-iterate-security-review-3.md`.

## Isolation

- Canaries re-planted in the seven standing files before the round and removed after it; `git status` shows those files unchanged.
- The validator's value pattern finds no canary in any of the three reports.
- Positive control: an agent with no exclusion quoted the new rule-index canary.
- Same stated limit as rounds 1 and 2: readiness reported the harness attached `.claude/CLAUDE.md` unasked; not used, and no canary reached a report.

## Triage

| Finding | Lens | Severity | Disposition | Note |
|---|---|---|---|---|
| A design refused for quoting the review does not get the refusal in its next packet | readiness | Blocker | PROPOSE FIX | the verbatim-refusal mechanism is removed (§3.5); with it go `design-quotes-review`, its signature and the failure-lesson path the finding showed would not work; the identical-revision refusal goes too (§3.6a) |
| The verbatim check is underspecified and will refuse legitimate revisions | readiness | Major | PROPOSE FIX | removed, as above |
| The verbatim check defeats CONTEST | critic | Major | PROPOSE FIX | removed, as above — the reason the removal is right, recorded in §3.5 and D-083 |
| The refused revision's failure lesson does not carry the refusal | critic | Major | PROPOSE FIX | removed, as above |
| The verbatim check's normalisation contradicts itself | critic | Nit | PROPOSE FIX | removed, as above |
| An identical revision is labelled `design-missing` | critic | Nit | PROPOSE FIX | the identical-revision refusal is removed |
| SEC-1 The byte-identical check leaves the reroll open | security | High | PROPOSE FIX | the finding's own remediation items 2 and 3: the revision's lines-changed count is recorded on its `RunExited` caption (§3.6a), and BR-57 and D-083 state that the cap, not an identity check, bounds re-sampling; item 1 (a normalised identity refusal) is not taken, because any refusal needs the feedback path the readiness Blocker showed is missing, and a one-sentence edit defeats it anyway |
| "Never re-run on the same design" is beaten by a one-byte edit | critic | Major | PROPOSE FIX | as SEC-1: BR-57 reworded to "a design that was not delivered again", the bound stated |
| A design over 64 KiB is silently dropped from the revision packet | critic | Major | PROPOSE FIX | the per-part limit named in §3.4 and F10; the sheet warns before the send-back (`DesignBytes`, `PartLimit`); TC-497 |
| The drift release keeps breaking BR-54 | critic | Major | PROPOSE FIX | `qa-epic-added` joins the skip list (`operatorHolds`); TC-499 |
| The rollback procedure misses reviews promoted but not yet run | critic | Major | PROPOSE FIX | §10 widened with the §3.5 predicate |
| The dropped override is a scope cut the agent approves on its own | critic | Major | PROPOSE FIX | N1 states the argued reading — the conditional override is delivered as a contested send-back and a fresh review; §11 records it |
| P1 does not compile on its own | readiness | Major | PROPOSE FIX | row 4 (`model.go`) moved into P1 |
| P3's manual check cannot be set up as written | readiness | Major | PROPOSE FIX | the manual check is replaced by TC-497's handler rendering and `node --check`; the client board's runtime gap is stated |
| SEC-2 The audit is partial: the `actor` column contradicts the payload; the cancel is unattributed | security | Medium | PROPOSE FIX (column) / PROPOSE FIX (wording) | `writeEvent` binds the column to `Actor` when set, under either order; BR-48 says the cancel at the cap is not attributed |
| SEC-3 The triage is read with `r.FormValue` | security | Low | PROPOSE FIX | url-encoded only, `PostFormValue`; TC-497 |
| SEC-4 Reviewer notes reach the sheet without the invisible-character refusal | security | Low | ESCALATE | a change to the review contract (`ParseReview`); R6 records it |
| SEC-5 An operator note can close its own quotation | security | Low | PROPOSE FIX | `strconv.Quote`; TC-491 |
| SEC-6 The sheet URL is concatenated | security | Low | PROPOSE FIX | `sendBackURL` template func; `encodeURIComponent` in `board.js` |
| SEC-7 BR-57 omits the read-boundary limit | security | Low | PROPOSE FIX | BR-57's text names BR-47's limit |
| "No Go string carries a D-number" is not true | readiness | Nit | PROPOSE FIX | the D-number is out of the `readOnlyGET` value and the `Actor` comment |
| The RunExited caption calls a design refusal a review refusal | readiness | Nit | PROPOSE FIX | design refusals are gone; the revision's note uses a separate `gateOutcome.note` |
| The board still offers approve on design and review cards | readiness, critic | Nit | PROPOSE FIX | `decorateBoardGate` removes `done`; `foldedBoard` restores every card's `Moves` |
| The cap of 3 is hard-coded | readiness | Nit | PROPOSE FIX | `BoardSendBack.Cap`; every "of 3" is the constant |
| The open-question register table is not in the change list | critic | Nit | PROPOSE FIX | OQ-11 and OQ-34 status cells |
| The revision instruction contradicts itself | critic | Nit | PROPOSE FIX | reworded |
| TC-499 overloaded and in the wrong file | critic | Nit | PROPOSE FIX | the gate and packet assertions moved to TC-492; TC-499 is board and drift only |

No severity was changed.

## Approval

No Blocker and no Critical or High security finding is open after v4. A fourth round was not run: the round-3 Blocker and three Majors are closed by deleting the mechanism they found defective rather than by new mechanism, and the High is closed by its own stated remediation. **Approved (v4)** by the orchestrating agent under the product owner's standing instruction ("work among your agents, never as my confirmation") — approval by delegation, not by the product owner in person.
