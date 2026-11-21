# Readiness review: `answer-source` v1 (`docs/designs/answer-source.md`)

**Lens:** gaos-goldfish-readiness. Could two engineers implement this design and get the same result?

**Isolation note for the author:** the harness put the text of `.claude/CLAUDE.md` into my context unprompted, as a system reminder. I did not open it and did not use it. It also showed a copy of `.claude/rules/qa-test-plan.md` that says the highest case is TC-477. The copy in this worktree says TC-489, and I reviewed against the worktree file.

**What holds up:** almost every `file:line` claim I checked matches the code. The answer reaching the agent's packet carries only choice and text (`store_mem.go:1445-1451`), so §3.1's claim that the email stays out of the packet is correct. The test arithmetic is correct: 489+10, 343+9, 365+9. BRD 56 → 57 is correct, and BR-57 is free.

---

### The answer webhook sends every client's answers to the QA tool - Severity: Blocker
- Affected scope: §3.3 last paragraph ("The webhook worker is untouched"), G3, G5, §3.10 BR-57 ("delivered to the asking tool"), §7
- Finding: `ExternalPending` (`external_inbox.go:268-278`) returns every answered or expired item from every asker. `inbox_worker.go:12-19` sends each one to `{GAOS_QA_URL}/api/v1/gaos/inbox-answer` with the QA tool's outbound token. Once a registry client exists, the decisions on that client's items are pushed to the QA tool. Items the admin token asked (`askedBy: "admin"`) go there too. That contradicts BR-57 and leaks one client's decisions to another. The registry has no webhook field. If the worker skipped those items, they would stay `pending` forever.
- Recommendation: Decide the rule and write it into §3.3: either `ExternalPending` returns only items whose `AskedBy` is `qa-tools` or empty, or a non-QA item gets a defined terminal delivery state. Reword BR-57. Add the assertion to TC-493.

### The change list is incomplete and names the wrong files - Severity: Major
- Affected scope: whole document; §3.8; §3.9
- Finding: no consolidated list of files. Missing: `deploy/check-workspace.sh` (lines 112-113 and 127-131 hard-code the counts); `.claude/rules/qa-test-plan.md` (highest TC, totals, range map, count literals); `dashboard/handlers_qa.go:86` (reads `s.api.qaProducts`); `dashboard/store.go:304-317` and `dashboard/store_arcade_blueprint.go:165-188` (`mutate` returns `(Result, error)`, `store_arcade.go:443`, so the item must come back through a closure). Wrong targets: `README.md` and `deploy/STACKS.md` list no `GAOS_API_*` or `GAOS_QA_*` variable; they are listed in `pustaka/docs/infra/environments.html:203-205`. No test file paths.
- Recommendation: Add a "Files" section with the exact path of each change, every test next to its `_test.go` file; add `deploy/check-workspace.sh` and `.claude/rules/qa-test-plan.md`; replace `README.md`/`deploy/STACKS.md` with `pustaka/docs/infra/environments.html`.

### The decision-log count does not follow from the page - Severity: Major
- Affected scope: §6 ("decision-log entries 82 → 83"), §3.9
- Finding: `decision-log.html:18` says "Eighty-one entries", and so do `toc/product.js:129` and `check-workspace.sh:129`, with D-082 present. The implementer cannot tell which literal to write.
- Recommendation: Quote the current literal and the new one, in every file that carries it.

### The `apiConfig` refactor is not specified, and §3.8's promise about existing tests is false - Severity: Major
- Affected scope: §3.2, §3.8 last line
- Finding: §3.2 adds `clients []*apiClient` but does not say what happens to `qaSum`, `hasQA`, `qaProducts`, `qaBusy` or `newAPIConfig(admin, qa, out, products)`. Existing tests call that function 20 times; `api_v1_test.go:111,118` and `external_inbox_test.go:139,143` poke `srv.api.qaBusy`. No parser signature for the clients file; `beginWrite`'s new signature and `Describe`'s exact output are not stated.
- Recommendation: Write out the new `apiConfig` fields, the signatures of `newAPIConfig` and the registry parser, `beginWrite`'s signature and the exact `Describe` string. List which existing tests change and why.

### How the store's refusals map to API statuses is left open - Severity: Major
- Affected scope: §3.3 steps 2-4, §3.4 response table
- Finding: "sentinel types" is ambiguous (a sentinel is a variable; a type carrying a phrase needs `errors.As`). Bad choice and over-long text both come back as plain `reject(...)`, so the handler cannot tell `bad-choice` from `bad-text`. The retry comparison's location is not stated.
- Recommendation: Declare the error shapes; the handler pre-checks the 4000-character limit; every other `ErrRejected` maps to `bad-choice`; the retry comparison runs in the handler against the returned item.

### The "own items only" rule conflicts with the ask route, which does not change - Severity: Major
- Affected scope: §3.4, §3.5, N4, L1
- Finding: identity is still `(product, externalId)`; a second client asking a taken id gets the other's item with 200 and its status — exactly what GET hides. The 200-open-item cap is shared across clients.
- Recommendation: Decide: 409/404 on ask when the owner differs, or include the creator in the key, or accept as a limit. Say whether the cap is per client or per product.

### "The host of `GAOS_QA_URL`" is ambiguous about the port - Severity: Major
- Affected scope: §3.6 step 2, TC-494
- Finding: `url.URL.Host` includes the port; `Hostname()` drops it. The value is stored permanently.
- Recommendation: Pick one and give TC-494 a concrete input and output, e.g. `http://qa.example:8140` → `qa.example`.

### No phases, exit conditions or verification commands - Severity: Major
- Affected scope: whole document
- Finding: nothing orders the work; no phase has an exit condition; the "Before you call it done" commands are not cited.
- Recommendation: Add phases with files, exit command and checks; the last includes `./deploy/check-workspace.sh`.

### `qa-sync-api.html` has no inbox section to extend - Severity: Major
- Affected scope: §3.9 first bullet, §3.2 last paragraph
- Finding: `pustaka/docs/guide/qa-sync-api.html` never mentions `inbox`; the D-078 routes are not documented there.
- Recommendation: State the section to add (anchor and heading) and whether it documents the existing ask and GET routes.

### The registry may use the `qa-tool` kind - Severity: Nit
- Affected scope: §3.2, §3.1
- Finding: a registry line with kind `qa-tool` prints "via QA tool (…)".
- Recommendation: Reserve `qa-tool`, or key the phrase on the client name `qa-tools`.

### Event text and `Display` when the source is empty - Severity: Nit
- Affected scope: §3.1 (`Display`), §3.3 step 3
- Finding: when `by.Kind == ""`, it is not said whether the text gains a suffix. `Display` has no reader.
- Recommendation: "the suffix is added only when a source is set". Name where `Display` is shown or drop it.

### Small anchoring and naming gaps - Severity: Nit
- Affected scope: §3.7, §3.10, §3.3 item 4
- Finding: the conversation field belongs on `AttemptView` (`conversation.go:657-664`); `buildInbox` ends at `handlers_bo.go:757`; `RecentRow`'s fields and file are not given; BR-57's BRD subsection and D-083's reference column are not named; `RecentAnswers` does not say whether a lapsed, unswept item counts as expired.
- Recommendation: fix names and lines, list `RecentRow`'s fields, name `#rules-qa-sync` and the TC range, state the lapsed-item rule.

### Redeploy step versus N6 - Severity: Nit
- Affected scope: §3.6 last paragraph, N6
- Finding: "The redeploy line on rnd-v3-vpn gains `GAOS_QA_ORIGIN=…`" reads as part of this change, but N6 says no deploy.
- Recommendation: Mark it as an operator step after merge; until then production records an empty origin.

Counts: B=1 M=8 N=4
