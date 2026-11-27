## Security review: `release-b4-unit-tests.md` (rev 7)

Design reviewed: `/home/ardi/devel/projects/ardihikaru/my-projects/gaos-unit-design/docs/designs/release-b4-unit-tests.md`

### What this proposes

A per-product switch `UnitQA`, off by default. When it is on, merging a pull request in a fix-target repository plans a `unit:qa` card instead of today's `unit:test` card. The card's key is `<owner>/<repo>#<n>:unit-qa`. Brain Town and the external-tasks API share that one card.

The QA-job runner drives the card. At start it:
- re-checks eligibility;
- reads the merge commit, `merged_at` and the base from GitHub;
- reads a fresh sandbox attestation from the QA tool and checks its shape and age;
- sends `unit-run` with the merge commit and a `watch` list (the test names of open unit defects).

Runs are capped per product and per tick. A blocked start retries every 15 minutes for 24 hours.

Cards the runner drives are identified by their planner-set key plus a `qa-job:` reason. They hold no BR-38 live-turn lease, take no agent seat, are never dispatched to an agent, and are skipped by boot reconciliation.

What the result does:
- A test or build failure files a deduplicated, capped `unit` defect behind a refusal gate.
- That defect's triage card is born BLOCKED with `ReasonUnitHeld`. Only an auto-fix admin can release it, cancel it, or close the defect. The guard is in the store, and the admin flag is resolved in the handler.
- A later merge on the same base, whose watched names all report `passed`, closes the defect. This cancels the held card, which is the only machine move of a held card.
- A failure of the QA job itself files nothing.

After release, the D-091 triage and fix path runs with a fenced unit packet. A unit fix may not change the failing tests' files, and the fix targets `UnitBase`, re-checked when it opens. Writing missing tests is moved to `release-b4b`.

### How the existing system works, per this document

- A merge plans a `unit:test` card. An agent picks it up with no checkout, and nothing runs the tests.
- The QA-job runner already drives e2e cards through `startAutomation` and polling.
- BR-38 counts every DISPATCHED or RUNNING task as the product's one live turn.
- `Reconcile` requeues DISPATCHED and RUNNING cards to READY at boot.
- `operatorHolds` protects certain BLOCKED reasons from a drift release.
- `ResolvePRCard` keeps operator-blocked cards where they are.
- Auto-fix admins are listed in a mode-600 file that `autoFixAdmin` reads.
- Defects feed D-091: triage, then the fixer, then a draft `gaos/fix-*` pull request.
- The QA tool is reached through `qaOutbound` with a bearer token. Its capability answer is cached for 10 minutes.

Every section could be paraphrased, so there is no comprehension finding.

### Trust boundaries touched

| From → To | Authenticates | Authorises | Validates | Far side fails |
|---|---|---|---|---|
| Operator browser → back office (new route, move/close guards) | HMAC-signed session cookie, `auth.go:329-341`, `:380-396`. The gate requires a session on every back-office pattern, `auth.go:568-586`. The brief's note that the cookie "checks nothing" no longer matches the code, so "only an auto-fix admin" can be implemented. | `autoFixAdmin` file, `handlers_autofix.go:17-37`; `CardMove.Admin` is `json:"-"` (§7.3) | `crossOriginOK` (`auth.go:595`), CSP | 401/403/409 |
| Dashboard → QA tool (`unitSandbox`, `unit-run`, poll) | Bearer token; URL limited to https or tunnel, `qaclient.go:102-140` | none (the QA tool is trusted for results) | `sandboxOK`, `Failing` screening (§6) | PENDING / BLOCKED retry, FAILED |
| Dashboard → GitHub (`unitSource`) | GAOS token | — | `sha40Re`, `pr.Merged` (§5.1) | transient retry, then BLOCKED |
| External-tasks API → planner | client bearer | product owns repo, case-insensitive, `prflow.go:383-391` | `parsePRRef`, `api_tasks.go:42-53` | 409/422/503 |
| Code under test → triager/fixer packet (via the QA tool's report) | none | the admin's release | printable filter, `stripBR53`, `screenDefectText`, `contextPathOK`, fence | — |
| Dashboard → model provider | gateway key | — | BR-32 `Screen` at dispatch, `store_mem.go:1027`, `runner.go:623` | refusal |

### Findings

### SEC-1 A watched-name "pass" from the code under test closes a held defect without a person - Severity: Medium
- Component: `PassUnitDefects`, `watch`/`watched`, the BR-54 amendment
- Evidence: design §7.4 (line 421), §6 (line 319), §7.3 table (line 417), §10 BR-54 (line 487); the QA contract keys `watched` by name only (line 500)
- Attack path: The only machine move of a held card is driven by a name-to-`passed` map produced by the test reporter of the code under test. Test names are not unique in most frameworks; the only uniqueness is the `≤ 200` character cap in §6. A later merge to the same base can produce a `passed` result for a failing test's name, either on purpose or by accident:
  - by deleting the failing test and adding a trivial test with the same name; or
  - through a custom reporter.

  Either way, all of the defect's names report `passed`, the defect goes to `fixed` with no person involved, and the held card is cancelled. `missing` protects only against deletion, not against a name being reused. Anyone who can land a merge on a unit base can therefore silently close a held defect. A compromised QA tool can close every open unit defect, because GAOS sends it the names. The design's central control ("held for a person") has an automatic exit that depends on data which neither GAOS nor the person verifies.
- Remediation:
  - Key watched entries by `(test, file)`, and record the failing file per name on the defect.
  - Require the QA tool to report a per-name count, and treat a count other than 1, or a different file, as `missing`.
  - For a defect whose triage card is still held, have the pass mark the defect "watched tests passed at `<sha12>`" and send the notice, leaving the card held, instead of cancelling it. An admin then confirms the close.
  - State the residual in D-105.
- Effort: small

### SEC-2 The "may not weaken the failing tests" guard protects a set derived from a truncated, optional field - Severity: Medium
- Component: `checkFix` unit rule
- Evidence: design §7.5 (line 433), "the files of its `Excerpt`"; §7.1 `Excerpt` cut to 4 KiB (line 371); §6 entries dropped or withheld (line 321); context includes failing files first (line 427); `autofixcheck.go:329-334` lets any packet file matching `AllowedGlobs` be edited
- Attack path: The protected set is reconstructed from `Excerpt`, which has two gaps:
  - `Excerpt` is cut at 4 KiB and ends `… and <k> more`, so failing tests listed beyond the cut have no protected file.
  - A `File` the reporter left empty, or that failed `contextPathOK` or screening, protects nothing.

  The context, meanwhile, loads the failing files from `Failing` (up to 6). A test file that is in the packet, matches `AllowedGlobs` and fell outside `Excerpt` can be edited by the fixer, which can weaken the assertion. Parsing `<test> — <file>` lines back out of `Excerpt` is also ambiguous when a test name contains ` — `. The only remaining defence is the human merging the draft (§14).
- Remediation:
  - Store the kept failing files as their own property, for example `UnitFiles`, set when the defect is filed or reopened.
  - Protect that set in `checkFix`, without parsing `Excerpt`.
  - Also refuse edits to any packet file matching `TestGlobs` for a unit defect.
  - Add a TC case with more than 4 KiB of failures and a case with an entry that has no `File`.
- Effort: small

### SEC-3 Case-variant API requests break "one card per merge" and cause extra sandbox runs - Severity: Low
- Component: external-tasks API mapping, `planUnitQASpecLocked`
- Evidence: the API key is built verbatim from the request's owner/repo (`api_cards.go:159-167`, `braintown_consumer.go:122-127`); `productOwns` is case-insensitive (`prflow.go:383-391`); the two-key lookup is exact (`api_cards.go:266`); design §4.2 lines 124, 133, 136
- Attack path: An API client sends `ACME/route-api#5`, then `Acme/Route-API#5`, and so on. Each request passes ownership, misses the Brain Town key and creates a new `unit:qa` card. Each card runs the same merge in the QA sandbox again. The per-client caps (`api_cards.go:224-225`, 5 open and 20 per day) bound the damage, but every client gets that budget. Defects still deduplicate, because `Repo` is lower-cased.
- Remediation: Canonicalise owner/repo to lower case before building `apiKey`, for every PR kind, and in `planUnitQASpecLocked`'s lookup. Add a TC-855 case with a mixed-case `pr`.
- Effort: quick-win

### SEC-4 A GitHub default-branch name enters keys and the fix base unvalidated - Severity: Low
- Component: §5.1 effective base, §7.1 `CaseID`/`Key`/`UnitBase`
- Evidence: design line 184 (`repoDefault` when `BaseBranch` is empty), line 362 (`unit:<owner>/<repo>@<base>@<h>`); `UnitQA.Bases` is validated (line 101), but no validation is stated for `base.ref` or the `repoDefault` answer; the branch rule is `autofix.go:122`
- Attack path: A repository whose default branch name falls outside `baseBranchRe` (it contains `@`, `+`, `:` or non-ASCII characters) puts that name into the defect key, the vertex id, the packet and the fix pull request's base. A name containing `@` makes `CaseID` ambiguous to parse.
- Remediation: Require the merged base to match `baseBranchRe` and not contain `..` before it counts as a unit base. Otherwise end the card DONE with "not a unit-test base". Add a TC-862 case.
- Effort: quick-win

### SEC-5 BR-32 is not named for the new packet content - Severity: Low
- Component: `renderUnitDefectPacket`, `buildUnitContext`
- Evidence: design §7.5 (lines 427, 431); the design never mentions BR-32 or egress. Dispatch screens packets at `store_mem.go:1027` and `runner.go:623`; BR-32 is `pustaka/docs/product/brd.html:1285`.
- Attack path: New content now reaches the provider: source at the merge commit, build output of up to 2 KiB, and file paths, all from the code under test. Screening probably happens already at dispatch, but the design neither states it nor tests it. A later refactor that renders the unit packet outside the screened parts would leak repository secrets without anyone noticing.
- Remediation: State in §7.5 that the triager and fixer unit packets are parts of the screened dispatch packet (BR-32). Add a TC-872 assertion that a credential-shaped `Message` or context file refuses the dispatch.
- Effort: quick-win

### SEC-6 The "every path out of BLOCKED" table omits the legacy task routes and send-back - Severity: Low
- Component: §7.3 hold
- Evidence: design §7.3 table (lines 404-417); routes `POST /tasks/{id}/dispatch|approve|retry|cancel` (`main.go:559-562`) and `send-back` (`main.go:591`); `DispatchTask` accepts a desk in `StateBlocked` (`store_mem.go:1017`)
- Attack path: These routes act on the desk projection (`store_mem.go:1006-1019`, `:1715-1764`), so they probably cannot move a held card. The table says nothing about them, though, so the claim of "no other path" is unverified for them, and a later change to the desk fold could open a path around the hold unnoticed.
- Remediation: Add rows for these routes and for `send-back`, stating why each cannot touch a `ReasonUnitHeld` card (desk-level, not the card). Extend TC-869 to call `DispatchTask`, `RetryTask` and `CancelTask` on the held card's product and assert that the card is still BLOCKED.
- Effort: quick-win

None of these findings is Critical or High, so none blocks the design under the policy's security gate (`docs/rules/gaos-design-review.md:99-100`).

Counts: Critical=0 High=0 Medium=2 Low=4
