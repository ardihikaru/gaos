# Readiness review, round 2: `answer-source` (design v2)

**Lens:** gaos-goldfish-readiness.

**Isolation note:** the harness attached `.claude/CLAUDE.md` and a stale main-checkout `.claude/rules/qa-test-plan.md` (TC-477) unasked; neither used; reviewed against the worktree's file (TC-489). One non-secret code file outside the worktree root (`dashboard/store_arcade_blueprint.go` in the main checkout) was read by mistake, then the worktree copy was used. Nothing from a deny-listed file is quoted.

**What holds up:** every round-1 readiness finding has a fix in v2; the counts (499, 352, 374) match `check-workspace.sh:111-131` and `qa-test-plan.md:129`; "Eighty-one entries" at `decision-log.html:18`; D-083 and BR-57 free; anchors checked hold; `AnswerClosed` follows `rejection`'s `Is` pattern (`store.go:203-212`) and `mutate` passes the error through unwrapped (`store_arcade.go:447-449`).

---

### An empty `AskedBy` and `Label` are not normalised, which breaks existing tests §3.8 says stay unchanged - Severity: Major
- Affected scope: §3.3 item 1, §3.3 item 2 step 5, §3.8
- Finding: the comparison `effectiveAsker(item)` vs `in.AskedBy` is literal; tests build `ExternalAsk` without it. `external_inbox_test.go:515` re-asks `r-done` and expects the item (`"qa-tools" != ""` gives `ErrIDTaken`). The cap test (`:188-197`) mixes bulk asks (`""`) and API asks (`qa-tools`) into different buckets. An empty `Label` gives an empty origin tag and the text " asks: …". The answer's `<Label>` has no stated source.
- Recommendation: normalise empty `AskedBy` to `qa-tools` and empty `Label` to `ExternalSource` before any comparison, count or emit; answer and expiry texts use the item's stored `Source`; list `:515` and `:188-197` as tests that must pass unchanged.

### `addClients` cannot do what it promises with the inputs it is given - Severity: Major
- Affected scope: §3.2
- Finding: no outbound digest in `apiConfig` (`api_v1.go:102-123`); `reserved`'s source is undefined (`apiFromEnv` and `authFromEnv` are built in one literal, `main.go:294-295`, so the raw env or `s.auth.label`?); an empty origin `-` collides with an unset QA origin and is dropped; clearing `why` is unspecified (clearing it unconditionally bypasses BR-52); case-sensitivity unstated.
- Recommendation: `outSum`/`hasOut` in `apiConfig`; exact call sites in `apiFromEnv`; `reserved = {qaOrigin, os.Getenv("GAOS_BO_LABEL")}` minus empties; compare case-insensitively and only when both are non-empty; `addClients` does nothing when `why` names equal tokens and clears `why` only when it is "no API token is configured" and a client loaded; TC-493 rows for both.

### Phase 1 cannot reach its exit condition, because the callers of the changed interface are in Phase 3 - Severity: Major
- Affected scope: §10
- Finding: the production callers `handlers_questions.go:65,113` are in Phase 3; the package does not compile at Phase 1's exit.
- Recommendation: move the two call-site edits into Phase 1.

### `dashboard/seed.go` is missing from the change list - Severity: Major
- Affected scope: §3.9, §3.2
- Finding: `seed.go:182` calls `apiGate`, `seed.go:186` calls `beginWrite`.
- Recommendation: add `dashboard/seed.go` to §3.9 and Phase 2.

### No phase owns the new tests, so two exit commands can pass with no tests run - Severity: Major
- Affected scope: §10, §3.8
- Finding: no phase lists the new test files; `go test -run` with no matching function prints `ok … [no tests to run]`; TC-496 is in no `-run`; `TestInbox` matches nothing today.
- Recommendation: assign each TC to a phase (TC-496 → 1; TC-490…494 → 2; TC-495, 497, 498 → 3; TC-499 → 4); exact function names in each `-run`; the output must not contain `no tests to run`.

### §3.8's call-site anchors for `AnswerExternal` are wrong - Severity: Nit
- Affected scope: §3.8
- Finding: actual: `external_inbox_test.go:33,176,359,369,399,440,450,480-481,582-583,625,643,690,704,777-783,822`, `store_conformance_test.go:848`; `events_test.go`/`runner_manual_test.go` call only `AnswerQuestion`.
- Recommendation: replace with the actual lines or "every match of `AnswerExternal(` in `dashboard/*_test.go`".

### The BR-52 "before" text does not exist on the page - Severity: Nit
- Affected scope: §3.10
- Finding: `brd.html:1345` says "equal tokens, or a token that is short or lacks the `gaos_` prefix, disable the API"; the citation `(D-072, D-074, D-075)` does not gain D-083.
- Recommendation: quote the exact clause and replacement; add D-083.

### Several "QA tool" wordings are left open or would become false for a registry client - Severity: Nit
- Affected scope: §3.7, §3.3 item 1
- Finding: the template strings are not given ("Note to the the QA tool" risk at `needs.html:119`); `:40` "the QA tool is told it expired" is false for a poll client; unmentioned: `handlers_bo.go:731`, `handlers_bo.go:844`, `external_inbox.go:259`, `:182`.
- Recommendation: list each string with its exact new form, per asker.

### The new `#inbox` h2 has no ToC section entry - Severity: Nit
- Affected scope: §3.9
- Finding: `pustaka/docs/assets/toc/guide.js:86-92` lists one section per h2 of that page.
- Recommendation: add `guide.js` (an `inbox` section, the desc and tags) to §3.9.

### The projection's copy of `AnsweredBy` is implicit - Severity: Nit
- Affected scope: §3.3, §3.9
- Finding: `externalItemsLocked` copies only `Choice`, `Text`, `AnsweredAt`, `Status` (`external_inbox.go:113-114`).
- Recommendation: state that the answered case also copies `AnsweredBy`.

### The conversation line repeats "answered" - Severity: Nit
- Affected scope: §3.7, TC-498
- Finding: "Answered Approve · answered via QA tool …".
- Recommendation: use the bare phrase or "source not recorded".

### Phase 3's browser check has no pass criterion - Severity: Nit
- Affected scope: §10 Phase 3
- Recommendation: name what must be seen (the recent list with a QA-tool row and a BO row; the refusal toast naming the first answer).

Counts: B=0 M=5 N=7
