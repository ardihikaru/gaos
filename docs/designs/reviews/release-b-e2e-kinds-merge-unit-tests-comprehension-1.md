# Goldfish comprehension review: `release-b-e2e-kinds-merge-unit-tests.md` (rev 1)

## What this proposes

The design finishes the last two agents of D-102's "three agents on one product": an e2e automation builder and a unit-test builder. It ships in four parts, each its own phase and release, in this order.

1. **B1, e2e kinds by repository.** Each product repository gets a kind, either `be` (case levels API and GQL, specs under `tests/api/rest/`) or `fullstack` (levels WEB and E2E, specs under `tests/web/`). A repository with no kind is `fullstack`. Admins set the kind on the Agents page through `POST /agents/assign`. Every e2e card gets a planner-only label, `e2e-kind:be` or `e2e-kind:fullstack`. GAOS asks the QA tool what it supports (`GET /api/v1/gaos/capabilities`), at start and every 10 minutes. It sends `kind` on the automation call only when the tool lists the `kind` capability. With `kind`, a create job writes only into that kind's folder, and a run of every case (`*`) runs only that kind's levels.
2. **B2, create then run on a staging deploy.** The staging callback still plans the run card. Before that, it plans up to `GAOS_E2E_CREATE_MAX` create cards. These are for catalog cases that are planned but not implemented, whose level matches the deployed repository's kind. Each create card is deduplicated per repository and case. Create cards have their own open cap. The run card lists the create cards as dependencies. The runner waits for them for at most `GAOS_E2E_RUN_HOLD` (30 minutes), then runs whatever specs exist. BR-54 is amended to allow this. With merging off, a create card ends Done once its spec pull request is open.
3. **B3, guarded merge of an approved spec pull request.** The QA tool's job status gains `pushed_sha`, `pr_author` and `spec_path` (capability `merge-facts`). A new client, `ghMerger`, uses a new fine-grained token and squash-merges the spec pull request pinned to `pushed_sha`. It does so only when all of these hold:
   - a product switch is on;
   - the readiness band is working or complete;
   - the pull request is in an automation repository, open, not a draft, on the default base, from the same repository, on a `qa-auto/` branch, opened by `pr_author`;
   - its head SHA is the approved SHA;
   - it changes exactly one spec file;
   - all checks and statuses are green and it merges cleanly.

   While it waits for checks, the card stays Running ("merging") for up to 30 minutes. Every outcome ends Done. A refusal sends a WhatsApp notice. New decision D-103, new rule BR-62.
4. **B4, unit tests through the QA tool.** The QA tool gains a `unit_run` executor and `POST /api/v1/gaos/unit` (capability `unit`).
   - Behind a product switch, GAOS's QA job runner drives the `unit:test` card instead of an agent.
   - The mode is `run` if a changed source file has a test beside it; otherwise it is `create`. In `create`, the QA tool writes tests, a person approves them, and the tool opens a draft pull request.
   - If the tests fail, GAOS plans a Blocked `unit:fix` card. A person releases it to an agent, and the card ends in Review.
   - New decision D-104, new label `unit:fix`.

## How the existing system works, per this document

- The staging callback plans one `e2e:run` card that runs every case.
- `pr.opened` plans an `e2e:automation` card whose case code comes from a `QA-case:` line.
- The QA job runner starts and polls jobs, uses Needs you during approval waits (BR-44), and ends Done on success.
- GAOS keeps a synced copy of the QA catalog.
- The `unit:test` card goes to an agent whose workspace holds no product code.
- The QA tool (stated assumptions):
  - It has four case levels and one generator.
  - It cannot generate GraphQL specs.
  - It commits one spec file per approval and pushes a `qa-auto/` branch.
  - It opens the pull request under its own token, records no SHA, and nothing merges these pull requests.
  - It runs only Playwright `case_run` jobs, against staging.
  - It has no unit-test runner.

Most code anchors check out: `qajobs.go:64`, `:204-227`, `:232-255`, `:274`, `:332-336`, `staging.go:196`, `qacatalog.go:81-105`, `qacatalog_store.go:60`, `scheduler.go:379`, `githubwrite.go:123`, `model.go:729`, `sendback.go:153`, label count 17, and `POST /agents/assign`. The exceptions are listed in the findings below.

---

### The unit:fix card's work has no mechanism - Severity: Blocker
- Affected scope: §5.2 "Run failed" bullet; §1 GAOS bullet 5; §7 B4 exit
- Finding: I cannot say how a `unit:fix` card produces anything.
  - §1 says the `unit-test` role's agent has a workspace with no product code, and that GAOS runs no code.
  - §5.2 releases `unit:fix` (role `unit-test`) "as an ordinary card" that "ends in Review", so an agent is meant to "fix the failing tests".
  - The design never says how the agent reads the product code or the failing tests. It does not say what it delivers (a diff artifact, a draft pull request via D-091's write path, something else), who opens the pull request, or which branch it targets.
  - It also does not say whether "fix" means fixing the tests or fixing the product code. That matters, because a merged change broke them.
- Recommendation: State what the `unit:fix` agent receives (beyond the failure summary) and what it produces. Say which existing path carries the output to GitHub (for example D-091's draft PR on `GAOS_GITHUB_WRITE_TOKEN_FILE`, with its globs and caps). Say whether product files may change. If none of this exists yet, make `unit:fix` a human-only card (not dispatchable) for this release.

### Merging a spec is a write that BR-61 forbids, and the register list omits it - Severity: Major
- Affected scope: §4.1, §6
- Finding: BR-61 (`brd.html:1307`) ends "GAOS never opens a review fix against a shared or protected branch, never merges and never closes a pull request." §4.1 and §6 list the amendments (BR-54, the out-of-scope line, the scope boundary, D-091 (2)) but never mention BR-61. After B3, the BRD would contradict itself.
- Recommendation: Add BR-61 to §4.1 and §6, with its amended wording (for example "…never merges a pull request other than a QA spec pull request under BR-62…").

### D-102 (5) says BR-08 must be amended; this design says it is honoured - Severity: Major
- Affected scope: §4.1 ("honours BR-08"), §6
- Finding: D-102 (5) (`decision-log.html:2144`) says Release B "takes effect only when … BR-08, D-091 (2) and the out-of-scope merge line are amended." BR-08 (`brd.html:1240`) only says that Minimum forbids auto-merge. It does not grant merging in any band. The design treats BR-08 as already satisfied and amends nothing. A reader cannot tell which register is right.
- Recommendation: Either amend BR-08 explicitly (for example "Working and Complete may auto-merge a QA spec pull request under BR-62; Minimum and Insufficient never"), or say why D-102 (5)'s condition is met without amending it.

### Needs you for a unit:test card widens BR-44, which the design says is unchanged - Severity: Major
- Affected scope: §5.2 "Create → Needs you … (the existing BR-44 path)"; §3.3 and §4.4 "BR-44 is not widened / unchanged"
- Finding: BR-44's exception (`brd.html:1304`) is narrow: "the QA-job runner may put an **e2e** card there." A `unit:test` card in AWAITING_INPUT is not an e2e card. This is a widening, not "the existing path". Also, `isE2ECard` (`qajobs.go:193-201`) only recognises e2e labels, so the runner path itself has to change.
- Recommendation: List a BR-44 amendment under D-104 in §6 ("…an e2e card, or a `unit:test` card of a product with `UnitTestsViaQA`…"). Name the change to `isE2ECard` / `Tick`.

### A QA result planning unit:fix needs a BR-54 amendment too - Severity: Major
- Affected scope: §5.2, §6
- Finding: BR-54 (`brd.html:1350`) says the only card QA data may add is one card for a new epic, plus defect-route cards under auto-fix. In §5.2, a failed `unit_run` (a QA-tool result) plans a new `unit:fix` card. The §3.1 amendment covers only staging create cards. `unit:fix` is not covered.
- Recommendation: Extend the BR-54 amendment in §6 to D-104's `unit:fix` (born Blocked, released only by a person). Alternatively, explain why this is not "QA data adding a card".

### Merge-on cannot deliver the "create, merge, run" promised in §0 - Severity: Major
- Affected scope: §0 first bullet; §3.2; §4.4
- Finding: §0 promises a staging deploy that will "create it first, merge it, and run it". The timings don't allow that:
  - A create job can run 40 minutes of active time (`qaJobTimeout`, `qajobs.go:157`) plus up to 24 hours waiting for approval.
  - The merge then waits up to 30 more minutes for checks.
  - The run's hold is 30 minutes from when the run card was planned.

  In practice, the run almost always starts before any new spec is merged, so the new spec runs only on the next deploy. With merge off, the dependency does nothing: the spec stays on an unmerged branch, and the run never sees it.
- Recommendation: State plainly that a new spec runs on the next deploy, not this one, and fix §0's wording. Otherwise, define a follow-up run after a merge (for example, when a spec merges, plan one `e2e:run` of that case). Say whether `Deps` is worth keeping in the merge-off mode.

### Create cards would count against GAOS_PR_MAX_OPEN; the exclusion is not named - Severity: Major
- Affected scope: §3.1 last paragraph
- Finding: §3.1 says create cards count against `GAOS_E2E_CREATE_OPEN` and not against `GAOS_PR_MAX_OPEN`. But `openPRCardsLocked` (`prflow.go:178-195`) counts every open card with a dedup key, except the `defect:` keys, API cards and `bundle/` keys. A `staging-create/…` key would be counted, so open create cards would make the run card hit the 409 cap (`staging.go:266-268`). Several things are also unspecified:
  - the order of planning when the run card is capped (are creates still planned?);
  - what a replay of the same commit (200 duplicate) does to creates;
  - whether `gateUnassigned` (role `qa`) applies to create cards.
- Recommendation: Name the `openPRCardsLocked` change. Give the order: the run cap check, the create cap check, then the assignment gate. Give the response for each case: 202 with `creates`, 200 duplicate, 409.

### Where the uncovered-case selection runs, and its atomicity, is unstated - Severity: Major
- Affected scope: §3.1 steps 1–3
- Finding: Planning goes through `PlanPullRequestCards` under the MemStore lock (`prflow.go:206-208`). The design says the case list comes from `QaCases`, which is a separate Store read (paged, `QaQuery`; ArcadeDB: `store_arcade_qa.go:14`). The "open create card for this case on any repository" check in step 2 only prevents duplicates if it runs under the same lock as the plan. The design does not say whether `apiStaging` reads the catalog first and passes the list in, whether a new store method does both, or how two concurrent deploys of two repositories are serialised.
- Recommendation: Name the store method that does steps 1–4. Say that the step-2 check and the planning happen under one lock.

### Dedup per repository plus "open only" produces duplicate creates and stuck cases - Severity: Major
- Affected scope: §3.1 steps 2–3; §3.3 merge-off
- Finding: The case stays `!Implemented` until a person merges the spec pull request. Once its create card is Done (merge off), step 2 no longer skips the case. A deploy of a second repository of the same kind then plans a new create for it, with a new key. The QA tool pushes `qa-auto/<code-lower>` and never forces (§1), so the second job collides with the first branch. Meanwhile, on the first repository the key is permanent, so a spec pull request that is closed without merging can never be re-created there.
- Recommendation: Make the skip "any create card for this case whose spec pull request is still open, or that is open itself", or key the create per product and case. Define how a closed, unmerged spec pull request lets the case be planned again.

### GQL cases would be planned for create, but they cannot be generated - Severity: Major
- Affected scope: §2.1 table; §3.1 step 1
- Finding: For `be`, step 1 selects cases with `Level` in {API, GQL}. §1 and §2.1 both say GraphQL specs cannot be generated. Every planned, unimplemented GQL case would get a create card that fails or writes a REST spec instead.
- Recommendation: For create, step 1 should select API only for `be` (and WEB and E2E for `fullstack`). State what happens to GQL cases: they are never created by GAOS.

### Unit mode choice cannot be computed from pullFiles - Severity: Major
- Affected scope: §5.2 "Which mode"
- Finding: `pullFiles` (`github.go:227-228`) returns only the files the pull request changed. To know whether "a changed source file has a test file beside it", GAOS must read the repository tree, which no named client does. Several things are undefined: what "beside" means (same directory, a naming rule?), what "a changed source file" means (any or all of them), and how a mixed pull request is handled (some files covered, some not). Two engineers would get different modes.
- Recommendation: Define the rule exactly, for example "`run` when every changed non-test file has a match of `TestGlobs` in the same directory, per the tree at the merge commit". Name the GitHub call (the contents or trees API) and say what happens when it fails.

### No commit SHA is available for POST /gaos/unit - Severity: Major
- Affected scope: §5.1 `{product, repo, sha, …}`; §5.2
- Finding: `PREvent` (`braintown_consumer.go:95-114`) has a `Commit` only for staging events. A `pr.merged` card carries the PR number, not the merge SHA. The design does not say where GAOS gets `sha` (for example `merge_commit_sha` from `pullRequest`) or what happens when it is missing.
- Recommendation: Name the source of `sha` and the refusal reason when it cannot be read.

### The merge token's permission list omits commit statuses - Severity: Major
- Affected scope: §4.3 token paragraph and guard 5
- Finding: Guard 5 reads `GET …/commits/{sha}/status`. A fine-grained token needs the "Commit statuses: read" permission for that call, and the design does not list it. As written, every merge attempt could end `github-<class>`, so the feature would be silently inert.
- Recommendation: List every permission the client needs: contents write, pull requests write, checks read, commit statuses read, and metadata read, which is implicit. Say whether the client refuses to start when a probe call answers 403.

### Merge scope is unclear: staging creates only, or every e2e:automation card? - Severity: Major
- Affected scope: §3.3, §4.4
- Finding: §3.3 describes "a create card", introduced in the staging context. `pr.opened` also plans `e2e:automation` create jobs (`prflow.go:86`). The design never says whether `AutoMergeSpecs` also merges the spec pull requests of `pr.opened` jobs (and so lands an automation spec before the product pull request it tests is merged), or of API `automation-create` cards.
- Recommendation: State which card sources the merge applies to.

### The kind of an API automation-run card is undefined - Severity: Major
- Affected scope: §2.2 "`PlanAPICard`'s e2e kinds do the same"
- Finding: An `automation-run` API card is keyed by product, client and case code (`api_cards.go:159-166`). Nothing shows it carries a repository, so "the kind label of `ev.Repo`" has no input. The card always gets a kind label (one per family), but which one?
- Recommendation: Say which kind an API run gets: from a request field, `fullstack` by default, or no kind label (and then "one kind per card" is not universal).

### Undefined terms - Severity: Nit
- Affected scope: §2.1 "the same admins"; §5.1 "the same two doors"; §5.2 "read by the packet hook", "an `Artifact` of kind `context`"; header "D-099…D-101 of `main`"
- Finding: These are project-specific terms that are not defined:
  - "The same two doors" is not explained.
  - No artifact kind `context` exists. `model.go:1067-1097` lists handoff, prompt, reply, question, design, review, sequence, erd, flow, diff, commit, report, log and upload. So this is a new kind, not an existing one.
  - "Packet hook" has no anchor.
  - "The same admins" presumably means the auto-fix admins (`open-questions.html:916`).
- Recommendation: Define each term, or link it. Mark `context` as a new artifact kind in the change list.

### Wrong or loose anchors - Severity: Nit
- Affected scope: §3.2 `labels.go:110-114`; §4.1 `brd.html:1456-1457`; §2.2 `prflow.go:78`; §2.3 `qajobs.go:64` for the capability poll; §1 "BR-42, BR-50"
- Finding:
  - `labels.go:110-114` is `labelOrder`. The dispatch refusal is `labelsAllowDispatch` at `labels.go:167-176`.
  - `brd.html:1456-1457` is the "generic edge type" bullet. "Performing the merge" is at `brd.html:1459-1460`.
  - `prCardsFor` is at `prflow.go:80`.
  - `qajobs.go:64` is `startAutomation`. No periodic read exists, and the design does not say where the capability set is held or what a transient error does to it.
  - BR-42 and BR-50 do not say that the unit-test workspace holds no product code.
- Recommendation: Correct the anchors. State how the capability cache behaves on an error: keep the last known set, or clear it.

### Labels `e2e-kind` and `unit:fix` can be changed through SetTaskLabel - Severity: Nit
- Affected scope: §2.2, §5.2
- Finding: `SetTaskLabel` replaces a label within its family (`prflow.go:555-568`). §2.2 refuses the `e2e-kind` family, but `unit:fix` shares family `unit` with `unit:test`. An operator could relabel a held `unit:fix` card as `unit:test`, or the reverse, and change who drives it.
- Recommendation: Say whether `unit:fix` is planner-only, or give it its own family.

### §0 table, guard regex and reason formats drift from the body - Severity: Nit
- Affected scope: §0 table row 3; §4.3 guard 4; §4.4
- Finding:
  - Row 3 lists `pushed_sha` and `pr_author`, but §4.2 adds `spec_path` as well.
  - Guard 4 admits `tests/api/graphql/`, which the QA tool never writes (§1) and §9 excludes. It also doesn't check the folder against the card's kind.
  - §4.4 gives two reason forms: `qa-job:<id> merging` and `merging since <unix>`.
- Recommendation: Align the table. Narrow the regex to the kind's folder. Give one exact reason string.

### No change list or alternatives, and the worked example is unused - Severity: Nit
- Affected scope: whole document; header "Terms"
- Finding: `docs/designs/README.md` rules 3 and 5 ask for a complete change list naming each file, and for at least one real rejected alternative. The document has neither. New files (`githubmerge.go`) and changed functions are scattered through prose. The "Route Planner" worked example and the term "Configured product" are defined in the header and never used.
- Recommendation: Add a per-phase change list (file → change). Add one rejected alternative for each risky choice, for example "merge by a person only" for B3. Use the worked example or remove it.

### Interplay with OQ-38 and OQ-42 is not mentioned - Severity: Nit
- Affected scope: §1, §3.1
- Finding: OQ-38 (`open-questions.html:921-929`) records that the QA tool's automation and job routes may not exist yet, and that cards then fail with `qa-routes-missing`. OQ-42 asks whether to create an automation from a case code with no pull request, which §3.1's staging create cards now do. The design resolves or depends on both without saying so.
- Recommendation: Say whether B1 and B2 depend on OQ-38 being closed, and whether this design answers OQ-42 (and update that register).

Sections that hold up: §1's GAOS facts match the code. The token-kind check (`ghTokenKind`), the `ReadinessBand` bands, and `start`'s re-adoption by reason (`qajobs.go:279-281`) are as described.

Counts: B=1 M=15 N=6
