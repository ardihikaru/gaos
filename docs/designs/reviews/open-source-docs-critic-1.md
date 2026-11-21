# open-source-docs — critic lens, round 1

Design reviewed: `docs/designs/open-source-docs.md` (v1). Lens: `gaos-goldfish-critic`. Saved with light condensation of the prose; every finding, scope, severity and recommendation is kept. Figures about the customer's data are redacted as `[redacted]` (SEC-11 of this round).

The design's file references mostly hold up: the 179 matches in 39 files, the 362 `dashboard/` tests, the eight ai-manager calls and the event names all check out. The problems are in what the design asks the system to do.

### The guard cannot prove G1 in the checkout where the scrub is done - Severity: Blocker
- Affected scope: §1.1 last paragraph, §3.2.2, §3.2.4, §3.3 skip branch
- Finding and rationale: the pattern list lives only in the git-ignored `deploy/local/forbidden-strings`; git never copies ignored files into a new worktree; §3.2.4 has the operator write it after the merge. So the guard skips in the implementing worktree and in every agent worktree; the only proof runs after the merge, on one machine. §3.3 treats a missing list as a pass.
- Recommendation: (a) fall back to the main worktree's list (`$(git rev-parse --git-common-dir)/../deploy/local/forbidden-strings`) and/or a `GAOS_FORBIDDEN_STRINGS=<path>` override; or (b) the approval condition says the operator runs §7 in the main checkout with the list present before the merge. State which.

### `git mv` into an ignored directory leaves the seed tracked - Severity: Major
- Affected scope: §3.2.2; change list row `deploy/seed/<name>/*`
- Finding and rationale: `.gitignore` never untracks a tracked file; `git mv` only renames index entries; the catalog stays tracked under a path that still contains the product's name.
- Recommendation: `git rm -r --cached` on the four data files plus a plain `mv`; `git mv` for `seed.sh` only; check `git ls-files deploy/local` lists only `deploy/local/README.md`.

### The guard's content scan stops checking silently once the file list is large - Severity: Major
- Affected scope: §3.3 (`git ls-files -z | xargs -0 grep ... -f <(printf ...)`)
- Finding and rationale: `<(…)` is one pipe; `xargs` splits a long list into several `grep` runs; the first drains the pipe and every later run gets an empty pattern file, matches nothing, exits 1, and `2>/dev/null` hides it. Files past the first batch are never scanned and the check prints `ok`.
- Recommendation: write the patterns once to a `mktemp` file (removed on exit) and use `git grep -IinoE -f "$tmp" | cut -d: -f1,2 | sort -u`; its exit status distinguishes no match (1) from error (2).

### "History is not rewritten" is decided in D-085, yet called the owner's call, and no open question is raised - Severity: Major
- Affected scope: header ("no OQ moves"), N1, §3.7 D-085, §3.2.4, §9, §10
- Finding and rationale: N1 and §9 say the rewrite is the owner's decision, but D-085 records it as decided. The repository is already public under Apache-2.0, and `c407f11` holds [redacted] real customer test cases; a scrub at HEAD removes none of that exposure. §3.2.4 and §10 recover files from `c407f11`, a path a rewrite would remove. Each new tag raises the rewrite's cost.
- Recommendation: open a new OQ ("Is git history rewritten?") with §9 as its options and the cost of waiting as its "Blocks" line; have the operator answer it before the v0.45.0 tag; put the recovery steps on a source that survives a rewrite.

### Rewording OQ-25 erases the only record of who granted the vendored code's licence - Severity: Major
- Affected scope: §3.1 replacement table row 3, §3.5 "open questions"
- Finding and rationale: the quote is wrong — `pustaka/docs/product/open-questions.html:675` has no such sentence; the real one names the organisation. OQ-25 (`:672-694`) is the record of an Apache-2.0 grant for vendored code; `dashboard/static/vendor/sisflow/NOTICE-sisflow.txt:2` says only "the sisflow project owner" and D-063 (`decision-log.html:1094-1105`) names no grantor. After the scrub no file identifies the copyright holder of vendored code — a provenance gap TC-310 and D-063 are meant to guard.
- Recommendation: keep the grantor's legal name in `NOTICE-sisflow.txt` and OQ-25 (and exclude it from the guard explicitly), or record in D-085 that anonymising the grantor was accepted knowingly. Fix the misquote either way.

### Moving the edge block drops the second instance's port-80 handling and certificate renewal - Severity: Major
- Affected scope: §3.2.3, §3.2.4, §4 row 2, R2
- Finding and rationale: removing the domain from the `:80` `server_name` (`deploy/edge/gaos.conf:31`) removes its ACME HTTP-01 location (`:33-38`) and redirect; §3.2.4 rebuilds the private file "from the removed block", the `:443` one (`:137-165`); nothing adds an `:80` block; renewal fails silently (rule 4, `:21-23`). §4 relies on an edge README step §3.2.3 says is not written.
- Recommendation: the private template contains both an `:80` block and the `:443` block; add `deploy/edge/README.md` to the change list with the step §4 cites; correct the range: the map's comment and entry are `:52-53`, `:54` is `default "";`.

### Moving `seed.sh` up one directory breaks its token path and makes prod its default target - Severity: Major
- Affected scope: §3.2.2
- Finding and rationale: `seed.sh:18` sets `root="$(cd "$here/../../.." && pwd)"` and `:20` derives `--token-file` from it; one level up, `root` is the repository's parent. `--base` keeps the prod tunnel default (`:19`) and `--dir` defaults to the example, so a bare run on the VM seeds the invented shop into production.
- Recommendation: `root` = `$here/../..`; make `--dir` required (FAIL, exit 2) or the default base loopback; quote both new FAIL strings for TC-411.

### `GET /ui/bo?view=integrations` reaches the view without `no-store` and without a test - Severity: Major
- Affected scope: §3.6, TC-511, TC-512
- Finding and rationale: `handleBOSheet` (`dashboard/handlers_bo.go:129-139`, route `main.go:359`, public at `auth.go:509`) passes `?view=` to `buildBO` and renders `bo-sheet`, the template `bo-main-oob` uses (`templates/backoffice.html:134`); a signed-in sheet request renders the view with no `no-store`.
- Recommendation: set `no-store` in `buildBO` for the view or in both handlers; add the sheet path to TC-511 and TC-512.

### TC-511's QA URL is one that `validQaURL` refuses, so the test cannot pass - Severity: Major
- Affected scope: §3.6 TC-511 fixture
- Finding and rationale: `validQaURL` refuses any query (`dashboard/qaclient.go:70-71`); `why` is set, the positive assertion fails, the `x=1` and `/base` negatives test nothing.
- Recommendation: `https://qa.example.org/base`; test the query case separately as `OutboundWhy` text.

### The provider lookup is not cached on failure, so the view stalls while ai-manager is down - Severity: Major
- Affected scope: §3.6 `AIMStatus.Providers`, §4
- Finding and rationale: `AIMAdapter.VendorInfo` (`dashboard/vendors.go:280-305`) caches only success and holds `a.vendors.mu` for the whole 10 s call; while ai-manager is down every render waits up to 10 s and queues every other vendor reader behind the lock. §4's claim holds only when ai-manager is healthy.
- Recommendation: cache failures briefly, or have the view read only the last cached result with its age and never block. Correct §4.

### A gave-up count needs no new store read - Severity: Major
- Affected scope: §3.6, the paragraph after the types
- Finding and rationale: the view already scans `s.store.Events` newest-first; each `ExternalAnswerDelivery` carries its outcome (`dashboard/events.go:92-98`); the latest outcome per item comes out of the same scan. As designed, an undelivered decision disappears from the view once a newer delivery succeeds.
- Recommendation: add `GaveUp int` (latest outcome per item is gave-up) from the existing scan; assert it in TC-511.

### BR-53 is cited for the wrong rule - Severity: Nit
- Recommendation: cite BR-52 alone (`brd.html:1346`); consider BR-57.

### Line references have drifted - Severity: Nit
- Finding: `check-workspace.sh:127-128` (not `:124-125`), "Eighty-three entries" `:129`; `readme_check` `:133-144`; README "not built" `:88-89`, counts `:100-110`; `buildBO` fall-through `handlers_bo.go:277`.
- Recommendation: re-anchor against `c407f11`.

### "Eight variables" lists ten - Severity: Nit
- Recommendation: write "ten" (`rebuild-restart.sh:97-104`).

### The template dispatch branch is missing from the change list - Severity: Nit
- Finding: `bo-main` (`templates/backoffice.html:118-129`) needs an `eq .View "integrations"` branch or the view falls through to `bo-rollup`.

### Missing configuration and the ai-manager address have no defined source - Severity: Nit
- Finding: no rendering defined for a nil `s.qaOut` (as at `qaclient_test.go:246`) or nil `s.api`, yet TC-512 expects `GAOS_QA_URL is not set`; `GAOS_AIM_URL` is read once at boot (`modelconfig.go:78-98`) and not kept.
- Recommendation: define the nil cases; take the host from the adapter's `Client.Base`.

### Rewording review reports breaks a written append-only rule - Severity: Nit
- Finding: `docs/rules/gaos-design-review.md:70-71` and `docs/designs/README.md:36` say reports are append-only.
- Recommendation: record the exception in D-085 and the policy file, or say why it need not be.

### Rollback puts the private files back into the repository - Severity: Nit
- Finding: `git revert` of the merge re-adds the seed and the edge block.
- Recommendation: rollback is a forward fix; revert the view and the pages only.

### Claims about ai-manager itself carry no reference - Severity: Nit
- Finding: §3.5 "Known gaps" ("600 s CLI timeout") has no `file:line` and no "assumption" label.

### TC-407 depends on files that differ between checkouts - Severity: Nit
- Recommendation: gate the private-seed pass behind an environment variable, and say so in the prose.

### The frontend checks are not named - Severity: Nit
- Finding: `.claude/rules/frontend.md:16-28` requires a `/break-ui` pass on new lists and tables; the clients table is not covered.

The other sections hold up: §1.1's arithmetic; §3.5's eight ai-manager calls; the BR-05 claim.

Counts: B=1 M=10 N=10
