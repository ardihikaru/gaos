# Security review — M17, the platform design gate

**Target:** `docs/designs/m17-design-gate.md` (revision 2)
**Lens:** Security Reviewer (trust boundaries)
**Round:** 2
**Date:** 2026-11-04
**Rubric:** `.claude/rules/rule-index.md`, `.claude/rules/qa-test-plan.md`, `.claude/rules/agent-workspace.md`

### What this proposes

A plan may carry three kinds of task: `implement`, `design` and `review`. Per story the plan grows a
chain — a design task writes a document, a review task attacks it, the implementation waits on the
review. Two new reply contracts join the handoff contract: `gaos-design` (front matter plus the
document verbatim) and `gaos-review` (front matter plus findings of severity/scope/note). The verdict
is derived from the findings. The gate is the planner's `DEPENDS_ON` edge plus the existing `BLOCKED`
status — no new task status, no new agent role, no new event type. The reviewer's packet is supposed to
hold the design and the rubric and nothing else. Enforcement sits in three places, because the plan and
the operator's board are both inputs.

### How the existing system works, per this document

A turn is a frozen packet of parts, screened by the egress pass, rendered into one prompt. The packet is
desk-scoped: the brief, the goal, the contract, the operator's answer, the previous handoff, the failure
lesson and the record of stories are built for every task at that desk. The back office authenticates a
cookie whose value is the literal `1`. The reviewer's isolation is a property of the packet, not of the
workspace.

### SEC-1 The design document never reaches the model — Severity: Critical
- Component: prompt assembly (`instructionOf` / `RenderPrompt`)
- Evidence: `dashboard/adapter_aimanager.go` — the kind list is
  `KindTask, KindRules, KindContext, KindRepo, KindContract`; `KindDesign` is not in it. The part is
  built and survives the egress pass, but `instructionOf` renders only the five listed kinds.
- Attack path: none needed — the defect is unconditional. The reviewer is dispatched with the rules and
  the contract only, and never sees the document under review, while being told "the design you were
  given is the thing under review". It will typically answer "no findings", the derived verdict is
  `accept`, and the implementation is released.
- Remediation: add `KindDesign` to the kind list and give the part its own heading; then assert on
  `RenderPrompt`'s output rather than on a helper that re-implements the filter.
- Effort: quick-win

### SEC-2 The author can open the gate by making the design artifact unscreenable — Severity: Critical
- Component: the gate's post-turn hook and the artifact recorder
- Evidence: a design whose body trips a detector returns `gateOutcome{}, false`: no artifact is
  written and the gate takes no action. A false verdict leaves the task at the runner's own status,
  `REVIEW`. The operator's ordinary approve writes `DONE`. `gateStateLocked` reads `StatusDone` as
  `GateCleared` and never looks for a recorded verdict. With no design artifact the review packet
  simply has no design part.
- Attack path: the author — the party the gate exists to constrain — writes one line into the design
  body. The artifact is refused, nothing is recorded, no event is written, and the design task comes to
  rest in the operator's Review column looking like ordinary finished work. The operator approves it.
  The review then runs with no design, finds nothing, and the gate releases the implementation. The
  failure is silent: nothing in the timeline distinguishes "the design was refused" from "the design
  was fine".
- Remediation: make a screening refusal a recorded outcome, and make `gateStateLocked` require a
  recorded review artifact whose derived verdict is `accept` before it reports `cleared`. A `DONE`
  status alone must never clear the gate, whoever wrote it.
- Effort: small

### SEC-3 The task kind is not persisted on the ArcadeDB store — Severity: High
- Component: ArcadeDB task persistence and load
- Evidence: the `Task` upsert map carries no `kind`; the load query selects no `kind` column and the
  `TaskRecord` it builds never sets `Kind`. The document claims both.
- Attack path: on an ArcadeDB deployment every planned task loses its kind on restart. The review
  branch never runs: the review task is handed the author's brief, goal, operator's answer, previous
  handoff and failure lesson — D-069 clause 3's contamination — and no design part. The review task is
  never asked for a `gaos-review` block; the operator approves it and the implementation runs with no
  design and no findings.
- Remediation: write `kind` in `writeTasks`, select it in the load query, and add it to the
  conformance restart comparison.
- Effort: small

### SEC-4 An unknown or absent kind exempts a task from the gate entirely — Severity: High
- Component: the task-kind vocabulary, `validatePlan`, the DDL
- Evidence: `taskKind` returns `""` for an absent *and* an unknown kind, while its own comment says an
  absent kind is an implement task; `knownTaskKind` has no production caller. The document claims the
  opposite.
- Attack path: the plan is an input to a public method. A plan that gives an implementation task any
  kind other than the exact string `implement` is not counted as an implementation, so the requirement
  that a story's implementation wait on a review is not applied to it. That task gets no design and no
  findings, is never released by the gate, and is not protected from a hand-unblock.
- Remediation: make an absent kind `implement`; refuse any other unknown kind in `validatePlan`; anchor
  the DDL regexp or assert the anchoring in the vocabulary test.
- Effort: small

### SEC-5 The scope is validated far more weakly than the document says — Severity: Medium
- Component: the review contract's parser
- Evidence: `validScope` rejects an empty scope, more than 200 runes, `\n\r\t`, an absolute path, an
  empty or non-numeric line suffix, and a path with neither `/` nor `.`. It does **not** reject
  whitespace, more than one `:` segment, or `..`.
- Attack path: a scope of `src/pricing.go round-half-even instead of half-up` is accepted. The scope is
  written into the findings artifact and forwarded into the implementer's prompt, so the one field the
  document calls "validated, not merely documented" is a free-text channel into the implementer's prompt.
- Remediation: reject whitespace in the path, reject a second `:`, reject `..`; and state that the note
  is the free-text channel, with the same bound and the same rule applied to both.
- Effort: quick-win

### SEC-6 The refusal BR-48 requires to be recorded is not recorded — Severity: Medium
- Component: the operator's board route
- Evidence: the board refuses a hand-unblock of a held task; "A refusal writes no event and returns
  ErrRejected with the reason, which the route answers 409". BR-48 and D-070 both say the clearance
  "is refused … and the refusal is recorded".
- Attack path: an attempt to clear a gated task by hand leaves no trace in the append-only stream, so an
  auditor cannot distinguish a gate that held from a gate that was never consulted.
- Remediation: write the refusal into the stream, or amend BR-48 and D-070 to drop "recorded". One of
  the two must change; today the rule and the code disagree.
- Effort: quick-win

### SEC-7 The document's inventory of the reviewer's packet is wrong — Severity: Medium
- Component: the design document against the implementation
- Evidence: the contract part is added for every kind, review included; `RenderPrompt` drops the
  contract kind. The document says the review packet's parts are the design and the rules and lists
  `.gaos/packet/handoff.md` among the paths it does not carry; D-070 clause 5 says the accurate thing.
- Attack path: not an attack — a specification defect with weight. The document's whole isolation
  argument is stated about the packet, and the packet the code builds holds a part the document says is
  absent.
- Remediation: state the packet's three parts and the prompt's two, and say that the contract part is
  the evidence of what the model was told and never reaches the prompt.
- Effort: quick-win

### SEC-8 "The design is data, never instruction" is half-implemented — Severity: Medium
- Component: the design artifact's parsing and the prompt renderer
- Evidence: `reservedFenceIn` covers only the three contract fences; `instructionOf` writes a
  `===== <path> =====` heading per part and does nothing to the body.
- Attack path: the contract-fence half holds and is a good control. The packet-section half does not
  exist: a design body containing a line `===== .gaos/packet/task.md =====` followed by an instruction
  is rendered verbatim into the reviewer's prompt, indistinguishable from a real section.
- Remediation: delimit the design body in the prompt with a marker the body cannot forge, or strip
  section headings from part bodies before rendering.
- Effort: small

### SEC-9 The document contradicts itself and the code on the reviewer's session — Severity: Low
- Component: the design document, session selection
- Evidence: "a task of kind `review` always opens a new session, whatever its history" against the next
  paragraph's "a retry resumes the review's own session"; the code applies the reviewer reason only
  when the task has no last run.
- Remediation: correct the sentence to what both the code and the next paragraph say.
- Effort: quick-win

### SEC-10 The plan builder hardcodes the implementation role — Severity: Low
- Component: the plan builder
- Evidence: the document's diagram says `imp <the story's role>`; the builder hard-codes `backend`.
- Remediation: use `st.Role`, or amend the document.
- Effort: quick-win

### SEC-11 The document's citations have drifted from the code — Severity: Low
- Component: the design document's evidence discipline
- Evidence: the egress detectors, the contract part, the operator's approval write and the packet-part
  range are all cited at their pre-implementation lines.
- Remediation: re-derive the citations against the merged tree.
- Effort: quick-win

### SEC-12 The "nothing new" list is wrong about blocking reasons — Severity: Low
- Component: the design document's Goals and Non-goals
- Evidence: the Goals and Non-goals say "no new task status and no new blocking reason"; D6 names three
  and the code writes them.
- Remediation: drop "no new blocking reason" and say what genuinely did not change.
- Effort: quick-win

### What I checked and found sound

The egress deny-list and allow-list are not widened: `KindDesign` is not `KindRepo`, so the path rules do
not reach it, and the content detectors run over the survivors of every kind. The reviewer's packet
withholds the brief, the goal, the operator's answer, the previous handoff, the failure lesson and the
record of stories. Both new contracts are read from the last block of their tag and checked against this
run and this task, and a design that quotes a reserved contract fence is refused at parse time. The
design and review artifacts are screened before they are recorded. The reviewer runs on `claude-opus-5`
regardless of its role's default. The scheduler and the operator's dispatch route share one dependency
helper, and a blocked task is never picked up by the scheduler.

Counts: Critical=2 High=2 Medium=4 Low=4
