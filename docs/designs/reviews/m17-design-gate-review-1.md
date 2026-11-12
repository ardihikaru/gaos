# Design review — M17, the platform design gate

**Target:** `docs/designs/m17-design-gate.md` (revision 1)
**Lens:** Tech Lead / the zero-decision rule (readiness)
**Date:** 2026-11-04
**Rubric:** `.claude/rules/rule-index.md`, `.claude/rules/qa-test-plan.md`, `.claude/rules/agent-workspace.md`

Every finding below was reported against revision 1. The author's triage is in
`m17-design-gate-consolidated-1.md`.

### Change list omits five files the change must touch - Severity: Blocker
- Affected scope: §"The change list"
- Finding: The arithmetic (3+12+6=21) is self-consistent, but the set is not. Five files are
  absent: `dashboard/schema/08-edges.sql` (the `USES_CONTEXT.role` CREATE is refused at commit
  unless widened), `pustaka/docs/assets/toc/product.js` (carries five counts the validator
  enforces), `README.md` (three counts and the version), `pustaka/docs/assets/toc.js` (the
  semver, named in prose but never listed), and `dashboard/store_conformance_test.go` (the plan
  case breaks under this design).
- Recommendation: Extend the change list and restate the arithmetic.

### No phases, no exit conditions, no stated ordering between changes - Severity: Blocker
- Affected scope: §"The change list", §"The verification plan"
- Finding: The document partitions nothing, though the milestone page already has the partition
  (M17.1–M17.4), and the change list contains hard dependencies the document never states: the
  schema's ALTERs must land with the CREATEs; `TaskRecord.Kind` must exist before the ArcadeDB
  upsert and load paths can carry it; the parsers must exist before the fences call them; the
  gate's moves must exist before the rule strings and the graph-time write.
- Recommendation: Add a §"The phases" with each phase's files, its dependencies and a testable exit
  condition stated as a command and its expected result.

### Nine of the sixteen new tests have no register ID, and no target count is stated - Severity: Blocker
- Affected scope: §"The verification plan", the register item
- Finding: The QA rule requires one register row per `func Test…`, and the design names sixteen new
  test functions while allocating seven IDs. Nine tests have no identifier, and no target number is
  given for the case count, the Go test count or the dashboard count — so the house check the design
  lists fails.
- Recommendation: Allocate the missing IDs, give the test→TC mapping, and state the target strings.

### TC-362 asserts a `verdict` of `ITERATE`, which this design deliberately has no field for - Severity: Blocker
- Affected scope: D5, the register item
- Finding: The forward spec states "the review's `verdict` is `ITERATE`", a field D-068 clause 5
  provided and D-069 clause 8 replaced with an `Artifact`. A forward spec the code makes obsolete
  is retired, never reused, and a retired row must name the case that replaces it. The design says
  all seven move to implemented.
- Recommendation: State that TC-362 is retired and name its replacement, or reverse D5 and give the
  review a real verdict field.

### The reviewer's session reason is specified twice, differently - Severity: Blocker
- Affected scope: §"The session"
- Finding: One paragraph says a review's first attempt gets `SessionFirst`; the next says the reason
  is a new `reviewer` constant written into the `SessionOpened` event. In the code
  `sessionForLocked` returns `("", SessionFirst)` for a first attempt, and the document never says
  where `reviewer` is injected.
- Recommendation: Choose one reason, name the injection point, and delete the other sentence.

### How the implement task's reason becomes `design-blocked` while it stays `BLOCKED` is unspecified - Severity: Blocker
- Affected scope: §"The verdict, and what the gate does with it"
- Finding: `moveTaskLocked` refuses a same-status move and the transition table has no BLOCKED→
  BLOCKED edge, while the reason lives in a field a transition does not write. So the implementer
  must choose between rewriting the field in place and moving BLOCKED→PENDING→BLOCKED — two
  different timelines and two different operator views.
- Recommendation: Name the mechanism for rewriting a reason without a status change, say whether a
  `TaskMoved` is emitted, and give its rule string.

### `RenderPrompt` cannot choose a contract by task kind, because the packet does not carry the kind - Severity: Blocker
- Affected scope: §"The prompt"
- Finding: `RenderPrompt` receives only the packet and hard-codes the handoff contract; `Packet`
  has no kind field and the change list covers none of the three ways to fix it.
- Recommendation: State which it is, list the files that change, and say whether the field is
  inside or outside the digest.

### D3's task counts do not follow from D3's own plan diagram - Severity: Major
- Affected scope: D3, the Risks table
- Finding: `PlanWorkflow` emits `4 + 2N` tasks; D3's diagram emits `4 + 4N`. Two stories go 8 → 12,
  not 8 → 10, and ten stories go 24 → 44, not "eighteen to thirty". The understated figure is the
  one the BR-38 argument rests on.
- Recommendation: Recompute from the diagram, state the formula, and correct both places.

### The graph-time `BLOCKED` move breaks an existing conformance assertion - Severity: Major
- Affected scope: §"The verdict", the change list
- Finding: `MemStore.CreateWorkflow` states that every task lands PENDING because READY is the
  scheduler's word, and `casePlanWritesGraph` fails any task not written PENDING — against both
  stores. The change breaks the house check unless the conformance file is modified, which the
  change list omits.
- Recommendation: Choose between writing the task BLOCKED and relaxing the assertion, or keeping
  PENDING and letting the dependency produce the state. Say which and list the file.

### Two files are named as the owner of the same new code - Severity: Major
- Affected scope: the change list
- Finding: `dashboard/designgate.go` is given "the contracts and their parsers" and
  `dashboard/handoff_reply.go` "the fences and their contracts". `handoffBlocks` is fence-specific
  by construction.
- Recommendation: Give each new symbol one file, and say whether the block reader is generalised.

### The `gaos-design` block's format is undefined, and its position contradicts the handoff contract - Severity: Major
- Affected scope: D4
- Finding: The design task must end its reply with one `gaos-design` block and also owe the
  handoff block, but the handoff contract says "Copy it as the last thing in your reply" and the
  parser takes the last handoff block: two blocks, one "last" position, and the order is not
  stated. And the design block has no stated fields and no stated place for the document.
- Recommendation: Give the block the same front-matter treatment, state the order of the two
  blocks, and say exactly which slice becomes the artifact body.

### The review contract and the design contract — the strings the model reads — are not specified - Severity: Major
- Affected scope: §"The prompt", D4
- Finding: `gatewayContract` is a fixed template with a fixed example and a test keeps the
  contract and the parser honest. The design says only that a review task's contract is "the
  review contract": no text, no field list, no example. The implementer would be authoring the
  strings the model is held to.
- Recommendation: Give the fields, the mandatory set, the failure signature and a filled example the
  contract's own parse test can use.

### `Artifact.kind` now has three declarations, and the document does not say which is authoritative - Severity: Major
- Affected scope: §"The data model", the change list
- Finding: `Artifact.kind` is declared three times — the CREATE, the 11 ALTER and the 12 ALTER —
  and the change makes a fourth while citing the 11 ALTER as "the precedent", which is the pattern
  that says add a new file and leave the old one alone.
- Recommendation: Say which file owns the current list, say that the historical ALTERs are left
  as they are, and write the new test to compare the CREATE and the newest ALTER.

### D-068 clause 7's "consumes none of the three attempt-scoped limits" is not carried into the design - Severity: Major
- Affected scope: §"The verdict"
- Finding: The table says only that the review task goes to BLOCKED with reason `review-failed`;
  it never says whether the failed review increments the retry or escalation count or writes a
  failure signature. A failed review that increments the retry count is a different product.
- Recommendation: State that the attempt consumes no retry, no escalation and no failure signature,
  and name the fields left untouched.

### BR-48 — the operator's queue and the `signedIn()` precondition — is absent - Severity: Major
- Affected scope: the header, §"The verdict", the Risks table
- Finding: BR-48 says only the operator clears an open Blocker and that the clearance is refused
  until the back office has a real identity. The document never mentions the operator's queue, never
  mentions `signedIn()`, and leaves unreconciled the fact that the gate itself performs
  BLOCKED→READY — a clearance the operator is supposed to be the only party to give.
- Recommendation: Scope BR-48 out explicitly, naming which half the gate implements, or implement
  the queue and say how it differs from BLOCKED. Reconcile it with the gate's automatic release.

### BR-46's rewritten wording is left to the implementer - Severity: Major
- Affected scope: the register item, the Risks table
- Finding: D3's shape leaves the workflow-wide design task with no review and gives the verifier,
  the security review and the rollout no design and no review — three implementation tasks with
  neither — and the document itself calls one of them a hole. So BR-46 must be narrowed, and
  "named exactly" gives the implementer no text.
- Recommendation: Write the new BR-46 text, and give the BR-49/BR-50 text or say they are
  unchanged.

### The register edits carry no exact text and no target numbers - Severity: Major
- Affected scope: §"The registers"
- Finding: The six register items are summaries of intent. The QA rule requires the counts to move
  together and names where each lives, and the validator enforces the exact prose strings — and the
  document states no target string for any of them, including why the rule count moves at all when
  no rule is added.
- Recommendation: Give the exact new strings, or the exact rule for computing them.

### `Task.kind`'s ArcadeDB round trip is called "a gap this change does not close" and then closed - Severity: Major
- Affected scope: §"The data model", the verification plan
- Finding: The paragraph says the bidirectional test "needs" to exist under a heading that says the
  gap is not closed, and the plan then lists exactly that test. The round trip itself has no test,
  and whether it becomes a conformance case or a standalone test changes the register.
- Recommendation: Decide the scope in one sentence, name the test that proves the round trip, and say
  whether it adds a register row.

### The implement task's role is left as "backend|frontend" - Severity: Major
- Affected scope: D3's diagram
- Finding: An "A, or B" with no selection criterion, which the zero-decision rule forbids. The
  current plan hard-codes `backend`, and the story record carries its own role.
- Recommendation: Say which role the implement task takes, and name the line that reads it.

### The review task's forced-opus override has no stated mechanism - Severity: Major
- Affected scope: D2, D3's diagram
- Finding: D2 says the review task "takes the same override" as the gated tasks, but the diagram
  gives the review task no gate marker, and the flag is the approval gate — persisted as
  `approvalRequired` and counted by the wizard. The implementer chooses between two architectures.
- Recommendation: State the mechanism, and say whether the flag is reused or a separate field is
  introduced.

### A `gaos-review` block with no findings, or with malformed findings, is in no row of the state machine - Severity: Major
- Affected scope: §"The verdict", D5
- Finding: The most natural way to say "this design is fine" — a well-formed block with an empty
  `findings:` — falls in none of the three rows. An unknown severity, a missing scope, a
  disposition outside the four and a block naming another run are all unspecified, and the parser's
  obligations are not stated.
- Recommendation: Add the missing rows and state the validation rules field by field.

### The retry paragraph contradicts itself - Severity: Nit
- Affected scope: §"The session"
- Finding: "A review task's *retry* must not resume either" is followed by "a retry of the review
  resumes the review's own session … That is correct." Either reading is implementable.
- Recommendation: Rewrite the first sentence and state what reason a review retry's session carries.

### The four disposition machine forms are only half-stated - Severity: Nit
- Affected scope: D5
- Finding: The design gives the machine form `propose-fix` and says only that it is `PROPOSE FIX`;
  the other three mappings are inferred.
- Recommendation: State all four mappings in one line.

### TC-364 asserts a new packet *kind* named `findings`, which this design does not create - Severity: Nit
- Affected scope: §"The findings travel back"
- Finding: The forward spec says the implementer's next packet carries "exactly one new kind,
  `findings`", and the design rides the findings as a `KindContext` part. The row's scenario text is
  not replaced by the flip to Implemented, so the row would assert a kind the packet does not have.
- Recommendation: Correct the row's wording, or add the kind and list it as a change.

Counts: B=7 M=14 N=3
