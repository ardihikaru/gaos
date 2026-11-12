# Design review — M17, the platform design gate

**Target:** `docs/designs/m17-design-gate.md` (revision 2)
**Lens:** hostile design critic + structural + style
**Round:** 2
**Date:** 2026-11-04

### The strongest counter-argument to the design's own thesis

The thesis is that a platform-run reviewer becomes *genuinely zero-context*. But the mechanism the
document builds measures **process, not outcome**. Nothing states what a review is expected to catch,
how often it catches something, what a false positive costs, or what the marginal cost per story is. The
only quantified outcome is the cost going *up*: `4 + 2N → 4 + 4N` tasks, each review pinned to
`claude-opus-5`, serialised by BR-38's one-live-turn-per-product, with the only cap (BR-17's budget
guard) explicitly **not built**. The document's own reply — "the cost is real and is named" — names the
cost without bounding it.

### The gate can open on a review that was given no design — Severity: Blocker
- Affected scope: D6's state table and "Who writes what"; §"Goals"
- Finding: the table has no row for "the design task produced no design artifact", and the code lets the
  gate clear anyway. A design task whose reply carries no readable `gaos-design` block returns `false`
  from `gateVerdict`, and so does one whose document trips the egress detectors. No artifact is recorded
  and the task falls back to `REVIEW`. The operator can approve that card, producing a `DONE` design
  task with no design artifact. The review task's packet then adds the design part only if the artifact
  exists, so the reviewer is handed the rubric and a contract telling it to attack a document that is
  not there. An empty finding list then clears the gate on nothing. The document's §"What revision 2
  changed" lists "The gate could not open" as the blocker it fixed; this is the same blocker in a new
  shape.
- Recommendation: Add the missing rows to D6, make the review refuse to clear when its packet carried
  no design, and state that the operator's approval of a design task must not substitute for the gate's
  DONE write.

### The review packet does carry the contract part; the guarantee is about the prompt — Severity: Blocker
- Affected scope: §"The mechanism, in one page"; §"Goals"; verification row 1
- Finding: the document states the review packet's "two parts are …" and lists the contract among the
  paths the review packet carries "**none**" of. The code adds that part unconditionally; only the
  rendered prompt drops it. D-070 clause 5 and the register agree with the code, not with the
  document.
- Recommendation: rewrite §"The mechanism" to distinguish the frozen slice from the rendered prompt.

### The design is Approved with a four-lens round, no round against revision 2, and no isolation probe — Severity: Blocker
- Affected scope: §"What revision 2 changed"; §"Status"
- Finding: the policy requires five lenses, an isolation canary probe with a positive control, a
  re-review if any Blocker survives, and approval only afterwards. The document says so itself — "a
  gate that skips its own probe is the thing this repository was written to stop" — and then skips its
  own probe. §"What revision 2 changed" says the round "ran **five** lenses"; the round's own triage
  says "**Four** of the five lenses reported". No round has ever been run against revision 2 — the
  version that was implemented.
- Recommendation: Correct the numbers, state that the comprehension lens did not run, and run a round
  against revision 2 with the probe.

### D1 misstates what `validatePlan` refuses — Severity: Major
- Affected scope: D1.1 and §"Goals"
- Finding: the code refuses something narrower: a *story* with no gated implementation. A story-less
  implement task is accepted ungated, so a hand-made plan whose "implementation" carries no story sails
  through. The Goals' "a hand-made plan without a review is refused" is false for a large class of
  hand-made plans.
- Recommendation: Restate D1.1 in the code's terms and say plainly that the guarantee is per story.

### The gate's release is not conditioned on the review task actually reaching DONE — Severity: Major
- Affected scope: D6
- Finding: the runner computes the gate's *intended* outcome and releases the implementation on that
  intent, not on the review task's actual status. A review whose own move was refused — `FAILED` — has
  not resolved, and the implementation is released anyway.
- Recommendation: Condition the release on the review's actual status, and add the FAILED-review row.

### The D6 state table has no row for three reachable states — Severity: Major
- Affected scope: D6
- Finding: the table covers five states; the code can reach at least three more, none of them described.
  The asymmetry is itself a finding: a missing review block is a first-class state, while a missing
  design block — the same class of contract violation — has no state at all.
- Recommendation: Extend the table to a complete one, and give each state a reason string.

### The blocking reason is written outside the event stream — Severity: Major
- Affected scope: D6, "The reason is written on the task, not as a move"
- Finding: the justification is that a same-status move is refused, which is correct, but it omits the
  consequence. The `TaskMoved` payload carries only `Rule`, `Reason: from→to` and `To` — never the
  blocking reason — and the fold never reconstructs `BlockReason`. On replay, which the product's own
  rules make authoritative, a gated card is `BLOCKED` with no reason, and the three new reason strings
  cannot be recovered. I-7 requires the blocking reason to be displayed explicitly.
- Recommendation: Carry the reason in the `TaskMoved` payload and reconstruct it in the fold, or state
  plainly that the gate's reason is a live-record annotation a replayed board does not show.

### The change list's counts do not follow from the phase tables — Severity: Major
- Affected scope: §"The phases"; §"The change list"
- Finding: phase 3's table lists nine files and the counts say seven; phase 4's lists twelve and the
  counts say eleven. The totals reconcile only because two files are counted once and twice, and the
  document never says which.
- Recommendation: Write the phase counts as "files touched in this phase" and mark the two repeats.

### Two count mirrors the document does not name are still stale — Severity: Major
- Affected scope: §"The counts that move"; phase 4
- Finding: the document enumerates where each count lives and presents the enumeration as complete. It
  is not: the QA rule's derived sentence still reads "**356 rows in total. 221 in `dashboard/` and 22 in
  `pustaka/`** — 243 Go tests", and `README.md` still reads "221 in `dashboard/`".
- Recommendation: Extend §"The counts that move" to every place the number appears.

### The scoreboard claim is false, and the implemented sub-module total is wrong — Severity: Major
- Affected scope: §"The registers"; phase 4
- Finding: the document says the scoreboard's "counts stay 4/0/0/5/0". The column order is `Module |
  Sub | ✅ | 🟡 | ❌ | ⊘`, so the implemented row is `4 | 5 | 0 | 0 | 0`. Worse, the same edit
  incremented the total row's sub-module count by eight instead of four: the total row says 79 while the
  eighteen module rows sum to 75, and the prose above says "seventy-five".
- Recommendation: Give the row's real values in the page's column order and correct the total.

### The verification plan claims a dispatch-route test that does not exist — Severity: Major
- Affected scope: verification rows 15 and 16
- Finding: row 16 says the test proves "`dispatchNow` refuses, and the board refuses to unblock a gated
  task". The test calls two unexported helpers; it never calls `dispatchNow`, and the board's unblock
  refusal is never exercised. The gate's *second door* has no test at all, and the product has no CI.
  Row 15 makes the same kind of claim.
- Recommendation: Add the route-level tests the plan claims, or lower the claims to what the tests
  assert.

### No success metric, and the cost is bounded by nothing that exists — Severity: Major
- Affected scope: §"Goals"; D8; §"Risks"
- Finding: the document has a Risks section, seven Alternatives and three Open questions, but no success
  metric. The Goal "a review turn is metered like any other turn" is an accounting assertion with no
  enforcement, while the cost of a review is the *most* expensive one in the plan.
- Recommendation: Add a §"What would show this works" with the cost per review, the finding rate and
  the false-positive rate, and say what BR-17's caps mean while the guard is unbuilt.

### The findings travel back re-rendered, not verbatim — Severity: Major
- Affected scope: D7
- Finding: D7's table says the implement packet carries the findings "**verbatim**". The code re-renders
  them: a header, `verdict:`, `open blockers:`, then `-<severity>  <scope>  <note>` lines. The
  structure, the front matter and the `no-findings` flag do not survive.
- Recommendation: Say "re-rendered in the gate's own record format", or forward the block verbatim.

### The quoted DDL does not exist, and "the regexp is anchored" is false — Severity: Major
- Affected scope: §"The data model"
- Finding: the document quotes a DDL that is not in the repository, and a security-relevant claim rests
  on it.
- Recommendation: Quote the file as it is, or change the DDL to the anchored form and say so.

### The `path:line` citations have drifted — Severity: Nit
- Affected scope: throughout
- Finding: the citations no longer name the thing they claim; the drift is systematic, so it is a rebase
  artefact rather than carelessness — but a reader auditing the claim finds the wrong statement.
- Recommendation: re-run the citation pass against the merged tree.

### "No new blocking reason" contradicts the three reasons the gate introduces — Severity: Nit
- Affected scope: §"Non-goals" against D6
- Finding: the Non-goals say "**No new task status, no new blocking reason**", while D6 defines three.
- Recommendation: change it to "no new task status, no new agent role and no new event type; the gate
  adds blocking reasons as free text".

### `#ni-egm` is a dead link, and TC-366…TC-375 are not forward specifications — Severity: Nit
- Affected scope: the header and §"The verification plan"
- Finding: the header links an anchor that does not exist, and calls the new rows forward
  specifications when they are new cases.
- Recommendation: Point the header at the live anchor and label the two ranges separately.

### An escalated finding has no home — Severity: Nit
- Affected scope: §"Status" against §"Open questions"
- Finding: the Status says two findings were escalated and "both are in §'Open questions'". Only one is.
  A rejected design is the *normal* outcome of the gate, and the design defines no loop for it.
- Recommendation: Add the re-review question to §"Open questions" and state what happens to a design
  that draws a Blocker.

### Two smaller mismatches between the plan the document specifies and the plan the code builds — Severity: Nit
- Affected scope: D3's diagram and D6
- Finding: the diagram gives the implementation task the story's role; the builder hard-codes `backend`.
  And the gate's rule string for the moment the graph is written is a fifth rule the document never names.
- Recommendation: Fix the diagram, and add the rule to D6's vocabulary.

Counts: B=3 M=9 N=5
