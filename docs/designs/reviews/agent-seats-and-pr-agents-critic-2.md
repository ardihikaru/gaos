# Design review: agent-seats-and-pr-agents (rev 2), critic lens

**Document:** `/home/ardi/devel/projects/ardihikaru/my-projects/gaos-agent-seats/docs/designs/agent-seats-and-pr-agents.md`

**The strongest argument against the design.** For the gated roles, "assignment" is a per-product on/off switch for each role, dressed up as a many-to-many model. The roster is a fixed fixture with one agent for each gated role: `pr-reviewer`=a-10, `unit-test`=a-11, `fixer`=a-13 (`fixtures.go:45-48`, copied by `store_mem.go:716-718`). The scheduler restriction in §3.5 therefore cannot change who takes a gated card. QA and defect cards never pass through `freeAgent` at all. Only the §3.4 planning gate does real work. Most of §6 cannot be built as written, because its state machine conflicts with the existing D-091 and D-092 code in four places.

### §3.5's scheduler rule does nothing for the gated roles, and the fixer bypasses it - Severity: Major
- Affected scope: §1.1 ("Kite works on Route Planner, not on the others"), §3.5, §10 rows 3 and 5, TC-774
- Finding and rationale:
  - **Gated roles:** each has exactly one agent (`fixtures.go:45-48`). So "only assigned agents are candidates" picks the same single agent as today, or none, and the gate already covers "none".
  - **QA cards:** `e2e:*` cards are never dispatched by the scheduler (`labels.go:113`, `scheduler.go:286`).
  - **Fixer cards:** every `defect:*` card is dispatched by `AutoDispatch` → `dispatch`, which names no agent (`board.go:528`; `Dispatch{TaskID, DeskID}`). The fixer is therefore never routed through `freeAgent`.
  - **Where it does bite:** only on ungated roles that have several agents (backend a-2/a-3, qa a-5/a-6). The design never motivates that case.
  - **Cost:** the change to `liveAgentSlots` and the rewritten "Working on" text add risk around the exact code that caused the v0.40.1 incident (`store_mem.go:2395-2401`). In return there is no behaviour an operator can observe for the roles the feature is about.
- Recommendation: Either state plainly that §3.5 applies only to roles with several agents and justify it, or drop §3.5 and the `liveAgentSlots` change from Release A and describe assignment as a role gate. Either way, §1.1's example must not promise a scheduler effect that cannot happen.

### BR-59 says something the mechanism does not do - Severity: Major
- Affected scope: §3.5 ("otherwise every agent of the role is a candidate"), §9 BR-59, §16 bullet 4
- Finding and rationale:
  - BR-59 says a configured product "gives its cards only to assigned agents". Under §3.5, a configured product with no assigned agent of a role gives that role's cards to **any** agent.
  - Cards planned before an admin saves assignments stay live after a role is switched off. So do cards created by hand (wizard *New task* with role `pr-reviewer`/`unit-test`). Both fall back to any agent.
  - Fixer cards ignore assignment entirely (see the finding above).
  - So the rule cannot be enforced as written, and the design does not say what happens to open cards when a role is switched off.
- Recommendation: Reword BR-59 to what the code does: "plans PR-flow cards only for assigned roles; a role with assigned agents runs only on them". Then decide what happens to open cards of a role that is switched off: cancel them, block them, or leave them. Add a test for that choice.

### The unassigned path loses the Pull requests row it claims to keep - Severity: Major
- Affected scope: §3.4 "Where the gate sits", TC-773
- Finding and rationale:
  - `ArcadeStore.mutate` throws away the whole `Result` when `apply` returns an error (`store_arcade.go:556-559`). `PlanPullRequestCards` runs inside `mutate` (`store_arcade.go:504-508`).
  - When the gate empties `fresh` and nothing was canceled, the design returns `ErrPRUnassigned`. The row that `reviewRowLocked` wrote into the projection at `prflow.go:253` is then never persisted. Memory and ArcadeDB disagree until the next restart erases the row.
  - This is exactly the case §3.4 says is handled ("still writes its Pull requests row, when unit-test is off"). It holds only when a cancel happened, which is the only case TC-773 names.
- Recommendation: Either return the rows with the sentinel and make `mutate` persist the rows on that error, or treat "gated out" as a success with zero planned and let the callers infer the reason. Extend TC-773 to a merge with nothing to cancel, run against ArcadeDB.

### The own-pull-request login comes from a cache that forgets - Severity: Major
- Affected scope: §5 "The own-pull-request rule"
- Finding and rationale:
  - The author login is stored "with the review context". That context is the in-memory `prContextCache`: it is lost on restart and holds at most 64 entries (`prreview.go:141-150`).
  - After a restart, or once a busy product pushes an entry out, the author is unknown. The design does not say what happens then. The likely result is `REQUEST_CHANGES` on GAOS's own pull request, a 422, and a card ending `Failed github-…`. The design forbids interpreting a 422.
  - It also does not say what happens if `GET /user` fails at start.
  - The reviewer already gets a `prReader` in §6.2, so it could read `pull.user.login` fresh at post time.
- Recommendation: Read the pull request in `afterRun` just before posting, and compare logins there. If either login is unknown, post `COMMENT`. Add both "unknown" cases to TC-777.

### The review defect's generation breaks the schema and the eligibility check - Severity: Major
- Affected scope: §6.2 table and "The fix card" (`defectCardKey(key, 0, "fix")`)
- Finding and rationale:
  - `Defect.gen` is `INTEGER (mandatory true, min 1)` (`schema/17-defects.sql:29`). The existing path uses generation 1 (`labels_test.go:173`).
  - A key built with generation 0 implies `Gen = 0`. ArcadeDB refuses that at commit, which is the v0.52.0 lesson the document itself cites. If `Gen` were set to 1 instead, `fixEligible` would refuse with "older generation" (`autofixpr.go:86`).
  - The table also leaves out the other mandatory columns: `gen`, `firstSeen` and `lastSeen` (`schema/17-defects.sql:26-27, 29`).
- Recommendation: Use generation 1. List every mandatory property with its value in the §6.2 table.

### Who builds the review context, and with which client, is not decided - Severity: Major
- Affected scope: §6.2 (the injected `prReader` has `pullRequest` and `pullFiles` only), §6.3, §10 row 8
- Finding and rationale:
  - `buildDefectContext` takes a `codeSource` and holds no lock (`defecttriage.go:294`). `PlanReviewDefect` is a store mutation that "stores this context". So someone must run the network reads (`fileAt`) before the store call.
  - The reviewer is not given `fileAt` (`githubwrite.go:83`). `autoFixPass` builds a context only for a triage card, or for a fix card with a `BaseOverride` whose context is not built (`autofix.go:293-313`).
  - Two engineers would put this in different places. A careless choice could also route the read through the write token's client, which D-091 (2) forbids for the review flow.
- Recommendation: State that `prReviewer.afterRun` calls `buildDefectContext` with the read-token `ghClient` (as a `codeSource`) and then calls `PlanReviewDefect(…, ctx)`. Add `fileAt`/`codeSource` to the wiring in §10 row 8.

### `reviewFixRefusal` has no source for its default branch, and the check is weak - Severity: Major
- Affected scope: §6.4 predicate signature, `review-default-branch`
- Finding and rationale:
  - `ghPR` does not carry `default_branch` (`github.go:43-68`). Only `ghWriter.head` reads it, from the repository endpoint (`githubwrite.go:191-211`).
  - None of the three call points says where `defaultBranch` comes from, or which token reads it.
  - The check also stops only one long-lived branch. A git-flow pull request `develop → main` passes, so a fix draft would target `develop`, which may be the product's own `AutoFix.BaseBranch`. The rule's reason is never given.
- Recommendation: Name the read: repository GET with the read token, made at each point. Also refuse `af.BaseBranch` and every `AllowedBases` entry, or justify why those are acceptable targets.

### After the reviewed pull request merges, the draft fix can end up on the default branch - Severity: Major
- Affected scope: §6.4 "If the reviewed pull request merges first", §15
- Finding and rationale:
  - **GitHub retargets.** By GitHub's documented behaviour (not checked in this repository), deleting a merged pull request's head branch retargets open pull requests based on it to the merged pull request's base, usually the default branch. A review-fix draft that is already open then targets `main`, which bypasses `review-default-branch` and D-092 (6).
  - **The poller does not notice.** It back-fills `Base` only when it is empty (`fixprpoll.go:364`), so the base change goes undetected.
  - **Closing the fix pull request misfires.** `AdvanceFixPR` clears `BaseOverride`, raises `Gen` and plans a person-only `defect:medium` card (`fixprpoll.go:409-416`). For a review defect, that card is certain to be refused (`review-base-changed`), or, if someone gets past the kind check, it targets the default base.
  - **§6.4 covers only part of the lifecycle.** It handles unstarted and running fixes, not a draft that is already open.
- Recommendation: Have the poller compare `FixPR.Base` to the base it reads. For a review defect, a changed base closes the record and comments, rather than being accepted. Make `AdvanceFixPR`'s closed branch close a review defect, not plan a new generation. Add both cases to TC-780.

### "Block the card, then CloseDefect" ends with the card Canceled, not Blocked - Severity: Major
- Affected scope: §6.4 "Any refusal blocks the card … and closes the defect with `CloseDefect`", §16 bullet 7
- Finding and rationale:
  - `CloseDefect` calls `cancelDefectCardsLocked` (`defects.go:598`). That moves every defect card that is not finished, dispatched or running to Canceled (`defects.go:363-369`), and Blocked is included.
  - So the card ends Canceled under rule `autofix-closed`, and the refusal reason the design promises is not where an operator looks.
  - `CloseDefect` is also documented as "an operator's close" (`defects.go:580`). Using it with actor `gaos` misattributes the close.
- Recommendation: Add a store method that closes the record without canceling its cards, or order the steps so the Blocked state and its reason survive. Assert the card's final status in TC-780.

### Review defects pile up and draw on the QA fixes' spend budget - Severity: Major
- Affected scope: §6.1, §6.2 "One per pull request", §13
- Finding and rationale:
  - **Low reviews are frequent.** With a fixer assigned, each one triggers a fixer turn.
  - **The cap is shared.** That turn counts against `FixCap` (`autofix.go:171-189`), the same daily cap as QA defect fixes. A busy week of low reviews starves the D-091 fixes.
  - **The new-key limit is unaddressed.** The design does not say whether review defects count against D-091 (7)'s limit of 50 new keys a day.
  - **Cards are stranded.** With auto-fix off, each low review leaves a Ready fixer card. A pull request closed without merging produces no Brain Town event, so its card is never canceled.
  - **Unstated arithmetic.** §13 gives no expected volume or spend.
- Recommendation: Give review fixes their own daily cap, or explicitly accept sharing the cap. State the position on the 50-keys limit. Plan a review defect only when `AutoFix.Enabled` is on, so no card waits for a switch that is off. Close review defects whose pull request is closed when the poller runs.

### D-099 does not amend D-088, which this design contradicts - Severity: Major
- Affected scope: §9 D-099 (4)
- Finding and rationale:
  - D-088 says GAOS "posts the screened review as a **comment**" (`decision-log.html:1866`). §5 adds `REQUEST_CHANGES`.
  - TC-553 and TC-565 describe the comment-only behaviour (`qa-test-plan.html:644, 656`).
  - D-099 lists amendments to D-091 (8) and D-092 (6) only.
- Recommendation: Add "D-088: medium and worse post `REQUEST_CHANGES`" to D-099. Update the TC-553 and TC-565 rows.

### The residual-risk mitigation is inverted - Severity: Major
- Affected scope: §15 "Residual risk"
- Finding and rationale:
  - The fix opens "against the collaborator's own branch, and is merged by a person". That person is the collaborator who steered the review, so the "person" check is the attacker.
  - What the attacker gets is a commit authored by GAOS, carrying the `GAOS-Defect` trailer, inside their own pull request. They also get a fixer turn spent for every pull request they open.
- Recommendation: State the residual honestly: GAOS authorship is laundered into the pull request, and spend is amplified. The controls that actually apply are `AllowedGlobs`, the line caps, `FixCap`, and the base branch's own review requirement when the feature branch merges.

### Test count and names do not match the repository - Severity: Nit
- Affected scope: §11 ("The existing `TestLabelVocabulary` is updated"), §13 "16 new, 1 updated"
- Finding and rationale:
  - No test named `TestLabelVocabulary` exists. The vocabulary test is `TestTheLabelVocabularyIsClosed` (`labels_test.go:105`), and it has no fixed count, so it needs no change.
  - Changing `postReview`'s signature breaks `github_test.go:129,143,180,202,210` and the `fakePoster` at `prreview_test.go:52`.
  - Changing the signatures of `freeAgent` and `liveAgentSlots` breaks their callers' tests.
- Recommendation: List the tests that are actually updated, and recount.

### Defect cards are made Ready by `readyDefectCards`, not by the scheduler - Severity: Nit
- Affected scope: §6.2 "born PENDING, and moved by the scheduler"
- Finding and rationale: The scheduler skips every `defect:*` card (`scheduler.go:286`). `readyDefectCards` moves them to Ready (`defecttriage.go:789-807`).
- Recommendation: Correct the sentence.

### The third check runs after the branch exists and leaves it behind - Severity: Nit
- Affected scope: §6.4 point 3
- Finding and rationale: A refusal after the ref is created goes through `block`, which does not delete the ref (`autofixpr.go:180-184`). Only the `createPR` failure path deletes it (`autofixpr.go:322-327`). The `gaos/fix-*` branch is left in the collaborator's repository.
- Recommendation: Run the check before `adoptOrCreate`, or delete a ref that this run created when the check refuses.

### Anchor drift - Severity: Nit
- Affected scope: §3.5, §6.4, §3.4
- Finding and rationale:
  - `schedulerInputLocked` is at `store_mem.go:2368`, not 2372.
  - `ciSecretsRisk` is called at `autofixpr.go:257`, not 259.
  - The egress screen is at `autofixpr.go:239`, not 236.
  - `PlanPullRequestCards` spans `prflow.go:202-309`, not 202-290.
- Recommendation: Re-anchor these.

### A 409 for a permanent configuration state - Severity: Nit
- Affected scope: §3.4 staging callback
- Finding and rationale: The route's existing 409 means `capped`, a transient condition (`staging.go:262-263`). Reusing 409 for `unassigned`, which does not clear by itself, invites callers to retry.
- Recommendation: Use 422 or 200 with `accepted:false, code:unassigned`, and document the code in the integration page.

### Placeholder sections - Severity: Nit
- Affected scope: §7, §8
- Finding and rationale: §7 repeats §0, and §8 is "(unused)". The headings promise content that is not there.
- Recommendation: Delete both and renumber, or say why the numbering is pinned.

**What holds up:**
- The severity-as-label approach in §4 is sound: the vocabulary has 10 labels and goes to 15 (`labels.go:44-55`), and family exclusivity is enforced at `prflow.go:446-459`.
- The register ids are free: D-099 (the log ends at D-098, `decision-log.html:2118`), BR-59 (the highest is BR-58) and TC-767 (the highest is TC-766, `qa-test-plan.html:857`).
- `RecordUpdated` is in the catalog and in the fold's no-op list (`fold.go:442`), so BR-05 holds.

Counts: B=0 M=12 N=6
