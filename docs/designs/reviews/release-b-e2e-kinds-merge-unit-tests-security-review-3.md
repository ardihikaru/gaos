# Security review: release-b-e2e-kinds-merge-unit-tests (rev 3, parts 1–3)

## What this proposes

- **Part 1 (B1), repository kinds.** The operator can give each product repository a kind, `be` or `fullstack`, on the Agents page through `POST /agents/assign`. The route takes only repositories of the product and only the values `be`, `fullstack` or empty. An e2e card made from a repository with a kind gets a planner-only `e2e-kind:*` label. When a job starts, GAOS reads that label and sends `kind` to the QA tool, but only if the tool's new `GET /api/v1/gaos/capabilities` route lists `kind`. For a `run *`, the kind filters cases by level. For a `create`, it limits the generator to one folder. A repository with no kind behaves as today.
- **Part 2 (B2), creates on a staging deploy.** The existing staging callback, which uses HTTP Basic per product, still plans one run card. When the deployed repository has a kind, it also plans up to `GAOS_E2E_CREATE_MAX` (default 2) create cards for the product's planned, unimplemented catalog cases at that kind's levels. All of this happens under one lock, with a separate open cap (default 4). Rules stop the same case being created twice: an open card, an open spec pull request (30 days), a merged spec not yet in the catalog (14 days), and a failed or closed attempt (7 days). The create card's key is `staging-create/<product>:<case>#<n>`. The reason line records the case so a restart can recover it. The card ends Done once the spec pull request is open.
- **Part 3 (B3), merge then run.** On `create`, GAOS sends `auto_merge: true` only when the product's effective readiness band is Working or Complete. After a person approves the spec, the QA tool turns on GitHub auto-merge pinned to the commit it pushed. It does this only when the default branch requires a fixed-source status check and the QA identity cannot bypass it; otherwise a person merges. GAOS stores the QA tool's `pushed_sha` on the Pull requests row as a new property, `qaSha`. When the poller sees the row merged with a head equal to `qaSha`, it plans one uncapped single-case run card, `qa-merged/<product>:<case>@<n>`, gated on the `qa` role. GAOS itself never merges.

## How the existing system works, per this document

- The staging callback (`staging.go:196`) plans an `e2e:run` card that runs every case. A `pr.opened` event plans an `e2e:automation` card for its `QA-case:` line.
- The QA job runner starts and polls jobs. It holds a card in Needs you while the QA tool waits for an approval, and fails a job after 40 active minutes.
- A job's spec pull request becomes a `qa` row that the fix-PR poller keeps refreshing.
- GAOS keeps a synced copy of the QA catalog.
- The QA tool (assumed) commits the approved spec, pushes `qa-auto/<code>` without force, and opens a pull request with its own token. Nothing merges that pull request today.
- The D-091 pipeline already turns defects into triage cards and draft fixes.

## Trust boundaries touched

| Boundary | Authenticates | Authorises | Validates | If the far side fails |
|---|---|---|---|---|
| Operator to `POST /agents/assign` (kinds) | A signed session cookie, checked (`auth.go:380-395`) | `autoFixAdmin` file, mode 600 (`handlers_assign.go:102`, `handlers_autofix.go:17-37`) | Keys and values per §2.1; 16 KiB body cap (`handlers_assign.go:106`) | 400 or 403 |
| Product CI to the staging callback (now also plans creates) | HTTP Basic per product, with a failure throttle (`staging.go:206-222`) | Product id match; the repository must belong to the product (`staging.go:224-258`) | Full SHA, `owner/repo`, 4 KiB body (`staging.go:233-243`) | 409, 422 |
| GAOS to the QA tool (capabilities, `kind`, `auto_merge`) | The existing bearer | None on the QA side beyond the bearer; it trusts the `auto_merge` flag | Unspecified for `pushed_sha` and `auto_merge` | Last known capability set kept; 404 means none |
| QA tool to GitHub (auto-merge) | `GH_TOKEN` (assumption) | Branch protection or a ruleset | The required check | The QA tool leaves the pull request for a person |
| GAOS to GitHub (head SHA) | The existing read token | — | Compares `qaSha` with the head SHA | The run is not planned |

GAOS gains no new endpoint. The staging credential gains new authority (SEC-4). GAOS's merge-detection guard is the only control that exists after a merge, and it has nowhere to store its result (SEC-1).

---

### SEC-1 The post-merge SHA guard detects an unapproved merge only after it lands, has nowhere to store the result, and does not stop the next full run - Severity: High
- Component: B3, the auto-merge pin and `AdvancePullRequest`
- Evidence: design `docs/designs/release-b-e2e-kinds-merge-unit-tests.md:99` ("disables it if the head ever moves"), `:103` (the `qa-auto/*` ruleset is only an operator precondition), `:113` ("the row's reason says `merged head is not the approved commit`"), `:185`. `dashboard/schema/18-pull-requests.sql:10-31`: `PullRequest` has no reason property, and the design adds only `qaSha` (`:109`, `:140`). `dashboard/qajobs.go:238-240`: a staging `run` runs `*` from the default branch.
- Attack path:
  - **The race.** My understanding of GitHub, which the design should confirm: `--match-head-commit` / `expectedHeadOid` is checked only when auto-merge is turned on, and a later push by an account with write access does not turn auto-merge off. So once auto-merge is on, the only thing that turns it off is the QA tool noticing a moved head ("disables it if the head ever moves"). That is a polling race. An account with write access to the automation repository pushes to `qa-auto/<code>` after the approval. The required check runs on the new head and passes, because it checks lint and type, not approval. GitHub merges before the QA tool reacts.
  - **What GAOS's guard does.** It only declines the targeted `qa-merged/` run. Its result has no field to go in, so the event leaves no record and sends no notice.
  - **Why that is not enough.** The very next staging deploy runs `*` from the default branch. The unapproved spec then executes in the executor against staging.
  - **The precondition is unchecked.** The ruleset that would close the race ("only the QA tool's identity may update `qa-auto/*`") is an operator precondition. It is not in the list of things the QA tool verifies (`:100`).
- Remediation:
  - Add the `qa-auto/*` update restriction to the QA tool's verified-and-reported preconditions (`:100`). Refuse auto-merge when it cannot be confirmed.
  - Give the mismatch a durable carrier: a row property or a `RecordUpdated` event with a fixed text.
  - Treat a mismatch as a security event. Send an `e2e:failure`-class notice and plan a Needs-you card for the product.
  - Until a person clears it, either hold the product's `run *` staging runs or exclude that case code from them.
  - Add a test for "qaSha unset → no run, recorded" beside TC-832.
- Effort: small

### SEC-2 Auto-merge is allowed in the Working band, but the BRD allows unattended merges only in Complete, and Complete's money/auth/schema gate is not addressed - Severity: Medium
- Component: B3, the band gate (D-103 "extends BR-08")
- Evidence: design `:96`, `:134-135`. `pustaka/docs/product/brd.html:806` (B-2: Working "gates on risk signals"; Complete "low-risk tasks may merge unattended; gates remain on money, auth and schema"). `pustaka/docs/product/brd.html:1240` (BR-08). `dashboard/model.go:729-740`.
- Attack path:
  - A product scoring 70–89 gets specs merged into its automation repository with no person at merge time.
  - The person approved the spec text in the QA tool. Neither GAOS nor the QA tool checks whether that case is about auth or money, and B-2 keeps such cases gated even in Complete.
  - The design claims only to extend BR-08, which talks about Minimum. It is silent on B-2's stricter Working and Complete wording, so a decision is being made by omission.
- Remediation: choose one, and record it in D-103.
  - Send `auto_merge: true` only in Complete, and keep a person at the merge for auth-, money- or schema-tagged cases, using a catalog field or a level rule.
  - Or amend B-2 explicitly to say that a human-approved spec counts as "attended" in Working.
- Effort: quick-win

### SEC-3 The `auto_merge` flag is read once at create time but is used up to 30 days later, and specs not started by GAOS have no stated default - Severity: Medium
- Component: B3, the GAOS-to-QA contract
- Evidence: design `:96` (the flag goes on the `create` request). `dashboard/qajobs.go:160-171` (an approval may wait up to 720 h). Design `:254` (§14: "the QA tool's auto-merge applies to every approved spec") contradicts `:98` (only with `auto_merge: true`).
- Attack path:
  - **Stale flag.** A product in Working starts a create. Drift drops it to Minimum while the spec waits days for approval. It still auto-merges, which BR-08 forbids.
  - **Unclear scope.** Under the §14 wording, specs created through the QA tool's own UI, or by another client, also auto-merge. Those specs never had a band check.
- Remediation:
  - Have GAOS re-send or confirm the flag when the job leaves the approval wait, or let the QA tool ask GAOS just before enabling auto-merge. If it cannot reach GAOS, it does not merge.
  - State that a create without `auto_merge: true`, whatever its origin, never auto-merges.
  - Correct the §14 note.
- Effort: small

### SEC-4 The staging credential can now trigger code generation from a case code alone, which is OQ-42 in practice, without a decision - Severity: Medium
- Component: B2, the staging callback's authority
- Evidence: design `:70-81`, `:83` ("OQ-42 … stays open"). `pustaka/docs/product/open-questions.html:955` ("A case with no pull request would need its own key namespace and a QA-tool call keyed on the case"). `dashboard/staging.go:212-229`.
- Attack path:
  - Until now, the per-product Basic credential, held in the product's CI, could only plan a run.
  - Under this design the same credential makes the QA tool generate code, open spec pull requests on the automation repository and, after approval, merge them.
  - It does this through exactly the case-keyed namespace OQ-42 calls undecided.
  - Anyone holding a leaked CI secret, or replaying a captured callback (the 200 duplicate path still plans creates, `:81`), can keep up to `GAOS_E2E_CREATE_OPEN` generation jobs and approval requests running for each product.
  - The callback has no replay protection: no nonce and no timestamp in `stagingBody`, `staging.go:188-193`.
- Remediation:
  - Record in D-103 that OQ-42 is answered for the staging surface, or that the staging credential's scope is widened on purpose.
  - Make creates opt-in per product, separate from setting a kind.
  - Plan creates only on a new commit (the 202 path), not on a replay (200).
  - List the widened authority in §8.
- Effort: small

### SEC-5 The required check is not specified to scan for secrets, and it runs generated code in CI with the automation repository's secrets - Severity: Medium
- Component: B3, the fixed-source required check
- Evidence: design `:100` ("that check runs the spec's lint, typecheck and a dry run"), `:186`. `pustaka/docs/product/open-questions.html:126` (OQ-40: a same-repository branch runs CI with secrets).
- Attack path:
  - The spec is model-generated from catalog and case text, which is QA data that can carry injected instructions.
  - The "dry run" executes that code in the automation repository's CI, from a same-repository branch, with that repository's secrets.
  - A generated spec can also hard-code a staging test account's password.
  - Auto-merge removes the second human look at the merge. Lint, typecheck and a dry run do not catch a hard-coded credential or an outbound call, so it lands on the default branch.
- Remediation:
  - Require a secret scan as part of the fixed-source check.
  - Require the dry run to have no repository secrets and no network beyond the staging allowlist. Name OQ-40 as related.
  - List both under the QA tool's verified preconditions.
- Effort: small

### SEC-6 What GAOS takes from the QA job (`pushed_sha`, `auto_merge`, the PR URL) is not validated before it reaches the store and the operator, and the Done reason reports capability, not outcome - Severity: Low
- Component: B2 and B3, the job-status parsing and card reasons
- Evidence: design `:90`, `:103`, `:109`, `:175`. `dashboard/qajobs.go:412-429` (the URL is parsed for the row, but the design does not say the reason text uses the parsed form). `dashboard/staging.go:186` (`shaRe` exists).
- Attack path:
  - A faulty or compromised QA tool can put arbitrary `pushed_sha`, `refused:<why>` or URL text into stored rows, card reasons or notices.
  - Separately, the card says "it merges when its checks pass" whenever the tool lists `auto-merge`, even when GAOS sent `false` (Minimum band) or the tool answered `refused:*`. The operator is told the merge is gated when it is waiting for a person, or the reverse.
- Remediation:
  - Validate `pushed_sha` with `shaRe` and store nothing otherwise.
  - Accept `auto_merge` only as one of `enabled`, `off` or `refused:<bounded token>`, screened like D-091 text.
  - Rebuild the PR URL in the reason from the row's owner, repo and number.
  - Choose the Done text from the job's `auto_merge` value, not from the capability.
- Effort: quick-win

### SEC-7 Any signed-in account can release a failed create card early, skipping the 7-day retry wait - Severity: Low
- Component: B2, the retry rule
- Evidence: design `:78` ("a person may release a Failed card sooner by moving it to Ready"). Card moves are not admin-gated: `autoFixAdmin` is called only at `dashboard/handlers_assign.go:80,102`, `dashboard/handlers_autofix.go:122`, `dashboard/handlers_prs.go:56` and `dashboard/prview.go:501`.
- Attack path: any back-office account, not just an admin, can restart generation for a case as often as it likes. It is bounded only by the approval step.
- Remediation: say who "a person" is. Either restrict releasing a `staging-create/` card to admins, or record the actor on the move and accept the risk explicitly.
- Effort: quick-win

### SEC-8 The security section and the published pages leave out the widened surface - Severity: Low
- Component: §8 and §6, the pages
- Evidence: design `:141` (`pustaka/docs/integrations/agents.html#release-b`, `pr-agents.html`), `:182-188`.
- Attack path:
  - §8 does not list the staging credential's new authority (SEC-4), CI execution of generated code (SEC-5), or how an unapproved merge is detected (SEC-1). A later reader will take §8 as complete.
  - The public docs page will describe the merge gate. If it names identities, repositories or ruleset gaps, it adds to the topology the site already exposes.
- Remediation:
  - Add those three items to §8.
  - State that the published page describes the mechanism generically, with no automation-repository names, identities or ruleset details (D-085).
- Effort: quick-win

Counts: Critical=0 High=1 Medium=4 Low=3
