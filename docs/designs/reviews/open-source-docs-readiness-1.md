# open-source-docs — readiness lens, round 1

Design reviewed: `docs/designs/open-source-docs.md` (v1). Lens: `gaos-goldfish-readiness`. Saved with light condensation of the prose; every finding, scope, severity and recommendation is kept. Sizes of the customer's data are redacted as `[size redacted]` (SEC-11 of this round).

The design is not ready to implement: 2 Blockers. One step would put the customer's seed back into git, and the implementer cannot check "zero private strings" because the guard skips when its list is missing.

**Isolation note:** the harness put the contents of `.claude/CLAUDE.md` into my context automatically. I did not ask for it, did not use it, and nothing below relies on it.

### `git mv` into an ignored directory leaves the customer seed tracked - Severity: Blocker
- Affected scope: §3.2.2 "Moves", §7
- Finding: `.gitignore` never removes a file that is already in the index; `git mv` moves the index entries, so the customer catalog [size redacted] would be committed again under `deploy/local/`. The order of the `.gitignore` edit and the move is not stated.
- Recommendation: exact commands in order — plain `mv` (or `cp`), `git rm --cached` on the four files, then the `.gitignore` lines; check: `git ls-files deploy/local` prints only `deploy/local/README.md`.

### No way to verify G1 in the worktree that implements it - Severity: Blocker
- Affected scope: §1.1, §3.3, §3.2.4, §10
- Finding: the patterns live only in the ignored list; worktrees do not share ignored files; §3.2.4 writes the list after merge. Section 7 skips in the implementing worktree, and the implementer must reword 179 matches without a runnable check.
- Recommendation: a gating step before any scrub edit: the list is supplied to the implementing worktree outside git. Exit condition: section 7 prints `ok … (N pattern(s))` with N ≥ 1, not the skip line. Pre-check that the list matches 179 hits at `c407f11`.

### No phases, checkpoints or verification commands - Severity: Major
- Affected scope: whole document; §7, §10
- Finding: ordering matters and is unstated (list before guard; seed move before `seedDir`; renames before references; counts with register rows); `gh repo edit` has no owner, timing or check.
- Recommendation: a phase table with files, dependency and exit command (`go vet`/`go test`, `pustaka check`, `check-workspace.sh` with section 7 not skipped, `seed.sh --dry-run`, `gh repo view`); `gh repo edit` as an operator step after merge.

### The edge config edit, as written, breaks the second instance and contradicts itself - Severity: Major
- Affected scope: §3.2.3, §4 row 2, §7
- Finding: (a) `:53-54` — line 54 is `default`, the instance's comment is on 52; (b) the removed block (`gaos.conf:152`) holds the `$gaos_backend` empty-check returning 444 — copied with only `proxy_pass` swapped, every request gets 444; (c) removing the domain from the `:80` `server_name` removes ACME and the redirect (rule 4, `:21-23`); (d) §4 cites an edge README step §3.2.3 says is not written.
- Recommendation: exact lines 31 (one name), 52-53, 137-165; specify the private file (a `:80` ACME block; a `:443` block with no `$gaos_backend` check and a literal `proxy_pass`); add the README step and list it, or drop the §4 reference.

### The change list relies on descriptions instead of paths - Severity: Major
- Affected scope: §7
- Finding: "the 10 others" are never named (`docs/designs/runner-manual.md` is cited in §3.1 but missing); `deploy/edge/README.md` and the removed `seed.sh` are not rows.
- Recommendation: list all 39 files plus the new files by path; make the total add up.

### TC-511's QA URL is one that `validQaURL` refuses - Severity: Major
- Affected scope: §3.6 TC-511
- Finding: a query is refused (`dashboard/qaclient.go:70-71`); `Outbound` is false and `OutboundURL` empty; the positive assertion cannot pass.
- Recommendation: `https://qa.example.org/base`; test query stripping through `GAOS_AIM_URL`, or add a refused-URL case asserting the `why` text.

### Rendered error strings can break the "never rendered" rule - Severity: Major
- Affected scope: §3.6 `AIMStatus.ProviderErr`, `QAStatus.LastDelivery`, "Never rendered"
- Finding: `ProviderErr` renders `err.Error()`; a Go HTTP client error includes the request URL with its path and username. The delivery reason is `err.Error()` from `w.send` (`dashboard/inbox_worker.go:139`), which includes the full webhook URL including the QA base path. TC-511 does not exercise either.
- Recommendation: render a fixed category, or strip configured URL and host strings; make TC-511 record a delivery whose reason contains the base path and a catalog error containing the AIM URL, then assert absence.

### Behaviour when `s.api` or `s.qaOut` is nil is not defined - Severity: Major
- Affected scope: §3.6 view model, TC-512
- Finding: `newTestServer` leaves `api`, `qaOut` and `adapter` nil (`dashboard/main_test.go:34`); `(*apiConfig).Describe` dereferences its receiver (`api_v1.go:255-261`) and panics on nil; `GAOS_QA_URL is not set` exists only from `newQaOutbound("",…)` (`qaclient.go:102-103`); the AIM URL's source (env at render vs boot config) and `GAOS_MODEL_MODE` are undecided.
- Recommendation: state the nil cases; name the AIM URL's source and use it in both tests.

### The generalised TC-407 is not specified well enough to write or register - Severity: Major
- Affected scope: §3.2.2, §5
- Finding: `readSeedFile` uses the const `seedDir` (`seed_test.go:14-18`); the discovery rule and the new `t.Errorf` messages are not given; the existing count messages (`seed_test.go:289`, `:355`) print the customer's real counts; the result depends on untracked files.
- Recommendation: give the helper signature, the discovery method and the exact new messages; state that the count literals are removed.

### Wrong line references - Severity: Nit
- Finding: README checks are at `check-workspace.sh:133-144`; QA count `prose_check` lines `:127-128`; decision count `:129`; README authentication line `:88-89`.

### The update to `.claude/rules/qa-test-plan.md` is incomplete - Severity: Nit
- Finding: stale count text at `:73-81` ("four hundred and ninety-nine", "five hundred and nine" twice).
- Recommendation: list every line to update.

### Example seed details are left open - Severity: Nit
- Finding: "12 cases (5 implemented, 9 planned)" needs "of which"; whether the example has `source.commit`; whether `--dir` resolves against the current or the script's directory (today the script uses its own, `seed.sh:17`).

### The README keeps a "not built" claim that may now be false - Severity: Nit
- Finding: "Reviews, scoring, retry guards: not built" (`README.md:88`) vs a feature list that includes the design gate's review.

### Rewording append-only review reports is not recorded in D-085 - Severity: Nit

### "Eight variables" is a miscount - Severity: Nit

### BR-53 is cited for the wrong thing - Severity: Nit
- Finding: BR-53 is batch bounds and text screening (`brd.html:1347`); the token rule is BR-52.

### A second route can show the view - Severity: Nit
- Finding: `handleBOSheet` reads `view` from the query (`handlers_bo.go:137`); `GET /ui/bo?view=integrations` renders without `no-store` and TC-512 does not cover it.

### `bo-main` dispatch edit not named - Severity: Nit
- Finding: `templates/backoffice.html:121-126` needs an `else if eq .View "integrations"` branch.

### Failure-mode row overstates the cache - Severity: Nit
- Finding: `VendorInfo` caches only successes (`vendors.go:283-290`).

Sections that hold up: the §1.1 arithmetic; D-085, TC-510…512 and v0.45.0 are the next free values; the gate classification and TC-512's 401 match `auth.go:551-559`.

Counts: B=2 M=7 N=10
