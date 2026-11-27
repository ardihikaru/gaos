## Security review: agent-seats-and-pr-agents (rev 3)

The design has one High finding, so under `docs/rules/gaos-design-review.md:99-100` it cannot go to Approved yet. That finding is SEC-1: the daily cap on review fixes does not actually limit how many fixes get planned, and the queued ones keep calling GitHub on the shared token.

The rubric says the back office's `signedIn()` accepts any cookie. That is no longer true. `dashboard/auth.go:354-358` checks the cookie's signature, expiry and account. `dashboard/auth.go:564-586` refuses cross-origin and unsigned requests, and `dashboard/handlers_autofix.go:17-37` reads the admin list from a mode-600 file. So the "admin-only" rule in §3.3 can be enforced, and I did not raise it as an identity gap.

### What this proposes

1. **Assignment.** A product records which agents are assigned to it. Once an admin has saved that list (`AssignedSet`), a PR-flow card is planned only for a role with an assigned agent. A product never configured behaves as today. Only accounts in `GAOS_AUTOFIX_ADMINS` can change the list, through `POST /agents/assign`, and each change emits a `RecordUpdated` event naming the actor's alias. The Agents page gains a product filter, checked against the product list.
2. **Severity label.** The reviewer's severity becomes a `severity:*` label. It shows as a chip on the board, plus a table column, a sort and a filter. An operator can correct it, but that changes only the record.
3. **`REQUEST_CHANGES`.** On a configured product, a medium or worse finding posts `REQUEST_CHANGES` instead of `COMMENT`. If the PR's author is the reviewing account, or either login is unknown, GAOS falls back to `COMMENT`. GAOS reviews once and never dismisses its review. This amends D-088.
4. **Review fixes.** A low review can become a "review defect". It goes through the D-091 auto-fix path and ends as a draft PR into the reviewed PR's own head branch. It runs only when all of these hold: a fixer is assigned, auto-fix is on, the head branch matches a glob in `ReviewFixHeads`, the repo is a fix target, and the daily `ReviewFixCap` is not reached. The fix is pinned to the reviewed SHA and may edit only files the PR already changes. It is refused for forks, for `.github/` changes, for incomplete file lists and for "shared" branches. Refusals are checked at two points before any branch exists.
5. **Lifecycle (§7).** The fix-PR poller closes the GAOS draft if its base changes, and a merge closes the defect.

### How the existing system works, per this document

- PR events reach GAOS from Brain Town (`pr.opened`, `pr.merged`). `PlanPullRequestCards` plans review, e2e and unit-test cards for every product that owns the repository.
- The reviewer parses a severity but only uses it to choose the card's end state, and always posts `COMMENT`.
- D-091's auto-fix triages a QA defect and dispatches an `easy` fix card itself, under per-day caps. GAOS then opens a draft PR with a separate write token, after `fixEligible`, `checkFix`, the CI-secret check and the egress screen.
- Back-office POSTs need a signed-in session and pass a same-origin check. Auto-fix settings are restricted to `GAOS_AUTOFIX_ADMINS`.

I could paraphrase every section, so there is no comprehension finding.

### Trust boundaries touched

| Boundary | Authenticates | Authorises | Validates | Far side fails |
|---|---|---|---|---|
| Browser to back office (`/agents/assign`, Agents filter, severity filter) | signed session cookie (`auth.go:354-358`) | `autoFixAdmin` (`handlers_autofix.go:17`) | 16 KiB cap, agent ids checked against the store, product checked against `Masters` | 401 or 403 |
| Dashboard to GitHub, read/review token (`GAOS_GITHUB_TOKEN_FILE`, `github.go:101`) | a PAT | the token's scopes (D-090 (3): the operator's broad classic token) | the response is untrusted text | `COMMENT` fallback; card Failed |
| Dashboard to GitHub, write token | a PAT | the token's scopes; the `ghWriter` method list (`githubwrite.go:155-160`) | `checkFix`, `ciSecretsRisk`, `reviewFixRefusal` | card Blocked or Failed |
| Brain Town to dashboard (`pr.merged`) | the `bt_c_` consumer token | — | ownership of the repo by the product | event skipped |
| PR author's text to reviewer model to fixer model | — | association check (`prflow.go:338-345`) | egress pass, `screenDefectText` | dispatch refused |

### SEC-1 The review-fix cap does not bound planning, and queued cards keep polling GitHub on the shared token - Severity: High
- Component: §6.1 condition 5, §6.4 check point 1, `autoFixPass`
- Evidence:
  - `docs/designs/agent-seats-and-pr-agents.md:254`: the cap counts only cards "automatically dispatched in the current UTC day". The same line exempts review defects from D-091 (7)'s new-key limit "because this cap bounds them".
  - `docs/designs/agent-seats-and-pr-agents.md:367`: check point 1 (a PR read, a files read and a repo read) runs before `AutoDispatch`.
  - `dashboard/autofix.go:176-188`: the cap counts only cards stamped with `AutoAt`.
  - `dashboard/autofix.go:283-325`: every Ready card is visited on every pass, a refused dispatch moves on with `continue`, and the loop stops only after one success (`:325`).
  - `dashboard/autofix.go:23`: a pass runs every 15 seconds.
  - `dashboard/github.go:101`: one token file serves both reads and reviews.
  - `docs/designs/agent-seats-and-pr-agents.md:409`: any signed-in account can dispatch a Ready review fix card by hand.
- Attack path:
  - A collaborator opens several PRs written so the reviewer calls them "low". Every one passes condition 5 until the first fix is dispatched, so with `ReviewFixCap` 3, ten cards can be planned.
  - The cards beyond the cap stay Ready all day. Each pass makes three GitHub reads per Ready card, about 720 calls per hour per card. Roughly seven queued cards use up a 5,000-per-hour budget on the token that the reviewer, the poller and triage also use. GAOS's PR review then stops for every product.
  - The same thing happens without an attacker whenever lows arrive in a burst while a fixer turn is running (BR-38).
  - Any signed-in account can also dispatch the queued cards past the cap by hand, so the spend bound §8 claims does not hold.
- Remediation:
  - In `PlanReviewDefect`, under the lock, count review fix cards *planned* that UTC day that are not Canceled, not just dispatched ones. Keep D-091 (7)'s new-key limit as a second bound.
  - In `autoFixPass`, run the cheap in-memory gates (switch, cap, BR-38 live turn) before check point 1. Run check point 1 at most once per card per head SHA, or with a backoff.
  - Either count a manual dispatch against `ReviewFixCap` or refuse it once the cap is reached. Add a test (TC-778 or a new one) that a burst of N lows with cap C plans at most C cards.
- Effort: small

### SEC-2 The "never targets a shared branch" claim rests on a three-item deny list and an allowlist that accepts `*` - Severity: Medium
- Component: §6.1 condition 3, §6.4 `review-shared-branch`, §8
- Evidence:
  - `docs/designs/agent-seats-and-pr-agents.md:252`: `ReviewFixHeads` globs are checked by `globOK`.
  - `dashboard/autofix.go:35-50`: `globOK` accepts `*` and `**`, because it was written for file paths.
  - `dashboard/egress.go:408-433`: `*` matches any single-segment name, such as `staging` or `develop`, and `**` matches everything.
  - `docs/designs/agent-seats-and-pr-agents.md:357`: the only branches refused are the default branch, `BaseBranch` and `AllowedBases`.
  - `docs/designs/agent-seats-and-pr-agents.md:331` and `:399` claim GAOS "never opens a review fix directly against a shared branch".
- Attack path: an admin sets `ReviewFixHeads` to `*`, or to a broad pattern. A collaborator opens a PR whose head is a long-lived branch that is not one of the three refused ones, for example `staging` into `main`, or `develop` or `release/x`. GAOS then opens a GAOS-authored draft straight into that shared branch. The draft still needs a person to merge it, but the claim the design makes is not what the code checks.
- Remediation:
  - Validate `ReviewFixHeads` with its own rule: reject `*` and `**`, and require a literal first segment followed by a wildcard (e.g. `feature/*`).
  - At both check points, also refuse a `ReviewBranch` that GitHub reports as protected (`GET /repos/{o}/{r}/branches/{b}`, field `protected`).
  - Change §8 to say exactly what is enforced.
- Effort: small

### SEC-3 The write token gains new capabilities (close a PR, comment on a human's PR) that are not in the change list or the register - Severity: Medium
- Component: §6.4 final paragraph, §7, `ghWriter`, the fix-PR poller
- Evidence:
  - `docs/designs/agent-seats-and-pr-agents.md:381`: `addComment` posts on the *reviewed* PR, not on GAOS's own draft.
  - `docs/designs/agent-seats-and-pr-agents.md:389`: the poller closes a draft "through the write token".
  - `dashboard/githubwrite.go:155-160`: "Its methods are the whole list", and closing a PR is not on it.
  - `dashboard/autofixpr.go:336`: today `addComment` is used only on GAOS's own draft.
  - `pustaka/docs/product/decision-log.html:1959-1961`: D-091 (2) says the write token is "used for that path only".
  - `pustaka/docs/product/decision-log.html:1991-1993`: D-092 (3) says the poller works with the read token.
  - `pustaka/docs/product/decision-log.html:1996-1997`: D-092 (5) says GAOS never closes the old PR.
  - `docs/designs/agent-seats-and-pr-agents.md:450`: change-list row 19 names only `fixprpoll.go`, not `githubwrite.go`.
  - `docs/designs/agent-seats-and-pr-agents.md:417`: D-099 (4) does not amend D-091 (2) or D-092 (3)/(5).
- Attack path: a close method that is not tied to GAOS's own drafts is a general "close any PR" ability. A wrong or corrupted `FixPR.Number` would then let the poller close a human's PR using the write token. Commenting on human PRs also widens what that token is used for, beyond what D-091 recorded.
- Remediation:
  - Add `closeFixPR` to `ghWriter`. Before closing, it re-reads the PR and refuses unless the head ref starts with `fixRefPrefix` (`gaos/fix-`), the number matches a stored `FixPR`, and the head SHA matches `FixPR.CommitSHA`.
  - List `githubwrite.go` in §10.
  - Record in D-099 that the write token now closes GAOS's own drafts and comments on the reviewed PR, as amendments of D-091 (2), D-092 (3) and D-092 (5).
  - Alternatively, post the "draft fix" comment with the review token, which already comments on that PR.
- Effort: small

### SEC-4 `REQUEST_CHANGES` and `GET /user` run under the operator's personal broad token, and the design treats the "read" and "review" tokens as if they were separate - Severity: Medium
- Component: §5, §8
- Evidence:
  - `docs/designs/agent-seats-and-pr-agents.md:219-222` and `:396` talk of "the read token", "the review token" and "the reviewing account".
  - `dashboard/github.go:85-105`: one `ghClient` reads one file, `GAOS_GITHUB_TOKEN_FILE`, for both reading and posting reviews.
  - `pustaka/docs/product/decision-log.html:1937-1940`: D-090 (3) records that this token is the operator's classic personal token, with `repo`, `workflow`, `admin:org` and `admin:enterprise`.
  - `docs/designs/agent-seats-and-pr-agents.md:221`: the own login is cached once and never refreshed.
- Attack path: no outside attacker is needed. A verdict written by the model, which the PR's author can steer, is posted as a formal "changes requested" review under the operator's own GitHub identity. Where that identity is a code owner or required reviewer, it blocks the merge until a maintainer dismisses it, and it looks like the operator's personal judgement. If the token file is rotated to another account, the cached login is stale and the own-PR rule compares against the wrong person.
- Remediation:
  - State in §5 and D-099 which identity posts `REQUEST_CHANGES`, and that it is a personal account today.
  - Make a non-personal token (a bot account or a fine-grained token) a precondition for `REQUEST_CHANGES`, or record the residual explicitly.
  - Drop the cached login when the token file's mtime or fingerprint changes.
  - Use one name for the one token throughout.
- Effort: quick-win

### SEC-5 The changed-files bound does not say what happens to new test files, so §8's claim is unverifiable - Severity: Medium
- Component: §6.3 `checkFix` rule, §8
- Evidence:
  - `docs/designs/agent-seats-and-pr-agents.md:340`: "refuses any *edited* path that is not one of `d.Files`".
  - `docs/designs/agent-seats-and-pr-agents.md:401` claims "it touches only files the pull request already changes".
  - `dashboard/autofixcheck.go:313-327`: new files are allowed when they match `TestGlobs`, and are handled apart from edits.
  - `dashboard/autofixcheck.go:358-360`: every fix must add or edit a test.
- Attack path: if new files are exempt, a prompt-steered fixer can add new test files outside the PR's file set. Those files run in the branch's CI, so the §8 bound is false. If new files are not exempt, every PR that changes no test file is refused. Two implementers would ship either version.
- Remediation: decide and state it. The safest option: for `Kind == "review"`, a new file is allowed only under a directory that already contains one of `d.Files`, and §8 says so. Add both cases to TC-781.
- Effort: quick-win

### SEC-6 Attacker-steerable review text and unmerged PR code enter the fixer packet unfenced or without a label - Severity: Medium
- Component: §6.2 `Excerpt` and `buildReviewContext`, §6.3 packet
- Evidence:
  - `docs/designs/agent-seats-and-pr-agents.md:285` and `:302`: the excerpt is the posted review body, which the model wrote from an attacker-authored diff.
  - `dashboard/defecttriage.go:494-502`: the excerpt is written into the packet raw, under a plain heading.
  - `dashboard/defecttriage.go:509-512`: source files, by contrast, are fenced.
  - `dashboard/defects.go:240`: `stripBR53` removes backticks and angle brackets but not markdown headings.
  - `docs/designs/agent-seats-and-pr-agents.md:275`: the source slice is now the PR's unmerged head.
  - `dashboard/defecttriage.go:432-436`: until now, source slices came from `BaseBranch` (merged code).
- Attack path: a PR author writes the PR so the reviewer repeats text that, inside the packet, looks like the packet's own sections (for example a fake "## The triage verdict" or "## Source files"). This pushes the fixer toward chosen edits. The heading at `defecttriage.go:489` marks the block as untrusted, but nothing marks where it ends. The changed-files rule and `AllowedGlobs` limit the damage, but this is the only content control on that path.
- Remediation:
  - Wrap the excerpt in `fenceFor` as the source files are, and put the triage-verdict section before the untrusted data.
  - For `Kind == "review"`, label the source files "unmerged pull-request code: untrusted data".
  - Add a TC that a review body containing a fake heading stays inside the fence.
- Effort: quick-win

### SEC-7 A merge-driven close trusts the Brain Town event without checking GitHub - Severity: Low
- Component: §7, first bullet
- Evidence:
  - `docs/designs/agent-seats-and-pr-agents.md:385-387`: on `pr.merged`, the fix card is canceled and the defect closed with rule `pr-merged`.
  - `dashboard/prflow.go:365`: the flow re-reads the PR from GitHub only for `pr.opened`.
- Attack path: a replayed or wrongly mapped `pr.merged` frame closes a live review defect and cancels its card. The impact is denial of one fix. The consumer token limits who can send frames.
- Remediation: before closing, re-read the PR with the read token and require `Merged == true`.
- Effort: quick-win

### SEC-8 Saving any assignment silently turns on `REQUEST_CHANGES` - Severity: Low
- Component: §3.2, §5
- Evidence:
  - `docs/designs/agent-seats-and-pr-agents.md:85`: saving even an empty list sets `AssignedSet`.
  - `docs/designs/agent-seats-and-pr-agents.md:215`: "Configuring a product is therefore the opt-in to GAOS asking for changes."
  - `docs/designs/agent-seats-and-pr-agents.md:153`: the editor shows checkboxes plus Save and Reset, with no notice of that effect.
- Attack path: an admin who only wants to switch off unit-test cards also, without meaning to, lets GAOS post blocking reviews under the identity in SEC-4. Consent is implied, not given.
- Remediation: put a sentence on the Edit assignments form saying that saving turns on `REQUEST_CHANGES` for medium and worse, and name that effect in the `RecordUpdated` text.
- Effort: quick-win

Counts: Critical=0 High=1 Medium=5 Low=2
