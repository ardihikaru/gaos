# Design review — M17, the platform design gate

**Target:** `docs/designs/m17-design-gate.md` (revision 1)
**Lens:** Hostile Design Critic (architecture, structure and style)
**Date:** 2026-11-04
**Rubric:** `.claude/rules/rule-index.md`, `.claude/rules/qa-test-plan.md`, `.claude/rules/agent-workspace.md`

**The strongest counter-argument to the design's own thesis:** the gate as specified cannot open,
cannot close, and cannot deliver its findings. The review task is never dispatched (its
prerequisite is never `DONE`), the rejected design has no path back to the author, and the findings
are attached to a task that by construction never runs. Everything below is smaller.

### The rejected design has no way back to the author - Severity: Blocker
- Affected scope: §"The verdict, and what the gate does with it", §"The findings travel back"
- Finding: The findings are attached to the implement task's *next* packet, but the implement task is
  BLOCKED for as long as the review is open, and a BLOCKED task is never dispatched, so the packet
  is never built for the case it exists for. The plan shape has no edge from `rev` back to `des`: a
  rejected design cannot be revised and re-reviewed without hand-editing the graph.
- Recommendation: State where the findings go for an open Blocker, and draw the loop edge.

### BR-48's operator's queue and identity precondition are absent - Severity: Blocker
- Affected scope: the header, §"The verdict", the Risks table, the change list
- Finding: BR-48 is binding on M17 and calls the identity precondition a blocking precondition. The
  design never mentions the queue, the clearance or `signedIn()`, and its change list touches no
  back-office file — while the board lets any operator move a BLOCKED card to READY today. The
  design therefore ships a gate whose clearance is unauthenticated.
- Recommendation: Add a BR-48 section naming the queue, the clearance path and the identity
  precondition as a blocking condition, or amend BR-48 with the operator's ruling.

### Nothing moves the design task to `DONE`, so the review is never dispatched - Severity: Blocker
- Affected scope: D3's shape, §"The verdict"
- Finding: A prerequisite is met only by DONE, a design task's run ends at REVIEW because the
  runner's furthest move is REVIEW and M5 is not built, and the only production write of DONE is
  the operator's board approval. So the review waits on a dependency only the operator can satisfy,
  one card at a time, per story — or never runs.
- Recommendation: Name the design task's terminal transition and who writes it.

### The design artifact is written but never read back - Severity: Blocker
- Affected scope: the change list, §"How the reviewer's packet finds the design"
- Finding: The artifact upsert is generic and writes any kind, but the LOAD path is not: it reads
  `WHERE kind = :k` with `k = ArtifactHandoff`, so only handoff artifacts come back. After a
  restart — and BR-01 says the state lives in the graph — the design artifact and the review
  artifact are gone from the projection.
- Recommendation: Name the artifact load path as a change site, state the filter that must widen,
  and say which store method the gate reads through.

### The change list assigns three constants to two files, and the tree does not compile - Severity: Blocker
- Affected scope: the change list
- Finding: The document never says which file owns a constant, and the repository already contains
  the result: the same three names are declared in two files, and `go build ./...` fails with three
  `redeclared in this block` errors. The verification plan's own gate cannot run.
- Recommendation: Give every new constant exactly one home, and state the repository's position.

### BR-46 is violated by the plan shape the document justifies with BR-46 - Severity: Major
- Affected scope: D3
- Finding: D3 rejects the workflow-wide alternative because BR-46 says every implementation task has
  a design and a review — and D3's own shape gives three implementation tasks per story neither,
  and gives the workflow-wide design task no review. The plan is weaker than the rule it invokes.
- Recommendation: Narrow BR-46 in the register change, or extend the plan.

### A review invocation that owes no handoff contradicts BR-30 - Severity: Major
- Affected scope: D4, the change list
- Finding: BR-30 says every invocation produces a handoff and a missing one is a failed attempt.
  D4 says a review task owes only the review block, and the register change does not amend BR-30.
- Recommendation: Amend BR-30, or make the review task owe both blocks.

### The finding shape carries no substance - Severity: Major
- Affected scope: D5, §"The findings travel back"
- Finding: The rule the design cites is about an egress detector recording where a credential
  pattern fired, not about a reviewer's finding about a design. Applied here it empties the
  deliverable: the implementer receives "blocker / propose-fix / src/pricing.go:42" and no statement
  of what is wrong at that line.
- Recommendation: Allow a bounded note, or state that the gate records only locations and name the
  file that carries the substance.

### The disposition is the reviewer's to make, but the policy gives triage to the author - Severity: Major
- Affected scope: D5
- Finding: The policy assigns triage to the AUTHOR after the review; a CONTEST is the author saying
  "this finding is wrong". A model emitting a contest of its own finding is nonsense, and the
  document never says who triages.
- Recommendation: Decide who dispositions the findings and write it into the contract.

### The drift report has no data carrier - Severity: Major
- Affected scope: D6, §"The data model"
- Finding: The document specifies a property the data model does not have, and `ArtifactRecord` has no
  version field either. The pin lives on the run, so the report could be derived by joining the
  artifact to its run — but the document specifies a property that does not exist.
- Recommendation: Add the property, or state the join and say where the report enumerates.

### The `reviewer` session reason is never selected, and the retry paragraph contradicts itself - Severity: Major
- Affected scope: §"The session"
- Finding: `sessionForLocked` returns `("", SessionFirst)` for a task with no previous run, and the
  event's rule is that reason, so the reason the design names is never selected. Neither
  `sessionForLocked` nor the packet carries the task's kind.
- Recommendation: Say where `reviewer` is chosen, and resolve the retry question in one sentence.

### A failed review cannot land on `BLOCKED` with reason `review-failed` - Severity: Major
- Affected scope: the mechanism table, the change list
- Finding: The runner classifies any attempt with a failure signature as FAILED, and the signature
  is set where the handoff is extracted. Implement the list literally and every review task fails
  on `handoff-missing` and lands in FAILED, never REVIEW.
- Recommendation: Name the adapter change explicitly and state how the attempt is kept out of the
  FAILED path.

### The plan-size arithmetic does not follow from the code - Severity: Major
- Affected scope: D3, the Risks table
- Finding: "eight tasks to ten; eighteen to thirty" — every number is wrong except the first, and
  the understated figure is the one the BR-38 argument rests on.
- Recommendation: Recompute from `PlanWorkflow` and quote the formula.

### The register's count mirrors are missing from the change list - Severity: Major
- Affected scope: §"The registers"
- Finding: The QA rule names four places the case count lives and three more mirrors, and the
  ToC part file carries all of them and is not in the change list. The document also never says to
  what the counts move.
- Recommendation: Add the file, name every count that moves and its new value, and name the new
  TC IDs.

### `Task.kind` has no carrier in the plan - Severity: Major
- Affected scope: the change list
- Finding: `PlanTask` has no kind field, and the mapping that builds the task records copies a
  fixed list of fields that does not include it. Without it the kind never reaches a task and every
  branch in the document is dead code.
- Recommendation: Name `PlanTask.Kind` and the copy, and say whether the kind is a plan-time field.

### The `Store` interface is not in the change list - Severity: Major
- Affected scope: the change list
- Finding: A lookup that only exists on `MemStore` is a lookup the production deployment cannot
  make, and the seam is not in the list.
- Recommendation: Name the interface method and the implementations that must provide it.

### BR-47 and BR-49 collide: the stories are in the reviewer's standing files - Severity: Major
- Affected scope: the Goals, §"The packet", Alternative A
- Finding: BR-47 requires that the reviewer not see the stories; BR-49 requires that the standing
  files stay whole; and the Blueprint renders the stories into `20-stories.md` and the decisions
  into `30-decisions.md`. The reviewer's real context contains the stories whatever the packet
  withholds, and the document never flags it.
- Recommendation: Say plainly which standing-file contents re-enter the reviewer's context, and
  amend BR-47 to that wording or record the collision as open.

### The reply contract has no access to the task's kind - Severity: Major
- Affected scope: §"The prompt", the change list
- Finding: The carrier is missing from the list, and the change list specifies only the constant
  block for the part-kind file.
- Recommendation: Name the carrier and the file.

### The design's own DDL contradicts the schema file already in the tree - Severity: Major
- Affected scope: §"The data model"
- Finding: The document specifies a `default 'implement'` the file in the tree does not have, and the
  file's comment gives the stronger reason — a property added to an existing type does not
  backfill. Two implementations of the same migration disagree about what an existing row's kind is.
- Recommendation: Reconcile the document with the file.

### The packet's "exactly two parts" is not guaranteed - Severity: Nit
- Affected scope: §"The packet"
- Finding: The rules part is behind the master check, and the document does not say where the
  review branch goes — before the check, and a masterless desk gets a review packet with no
  rubric, or after it, and a masterless desk gets no design at all.
- Recommendation: Name the branch's position and state the degraded case.

### The Problem statement misstates the current `Artifact.kind` vocabulary - Severity: Nit
- Affected scope: §"The problem"
- Finding: The regexp admits twelve values; the code writes four.
- Recommendation: Rewrite the bullet.

### Line-citation drift - Severity: Nit
- Affected scope: §"The events", §"The data model", D4
- Finding: Three offsets are off, and `DEPENDS_ON.kind` is itself a closed vocabulary the design
  does not say which kind the gate's edge carries.
- Recommendation: Correct the offsets and name the edge's kind.

### The document has no success metric and no open-questions section - Severity: Nit
- Affected scope: the Goals, the Risks table
- Finding: The Goals are capabilities rather than measures, and the biggest open items here — the
  operator's queue, the identity precondition, the review's failure path — are precisely what the
  open-questions register exists to hold.
- Recommendation: Add a success metric and an open-questions section.

### A redundant ALTER, and a mis-attributed ruling - Severity: Nit
- Affected scope: §"The data model", the change list
- Finding: Item 15 widens the 12 ALTER to the same list the new file sets, so one of the two is dead
  code and M15's migration file is edited retroactively. And the `reviewer` reason is named by
  D-068 clause 2, not by D-069.
- Recommendation: Keep the widening in one file, and fix the attribution.

### "Seven-ish parts" is not a claim with a file and a line - Severity: Nit
- Affected scope: §"The problem"
- Finding: The parts are conditional, so the count is between four and seven.
- Recommendation: Say "the same four to seven parts, all of them desk-scoped" and cite the range.

Counts: B=5 M=13 N=7
