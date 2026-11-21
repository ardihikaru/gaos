# open-source-docs — zoom-out lens, round 1

Design reviewed: `docs/designs/open-source-docs.md` (v1). Lens: `gaos-goldfish-zoom-out`. Saved with light condensation of the prose; every finding, scope, severity and recommendation is kept. Sizes of the customer's data are redacted as `[size redacted]` (SEC-11 of this round).

**Isolation notice:** I never opened `.claude/CLAUDE.md`. The harness put its contents into my context anyway, as an automatic system reminder, so `omitClaudeMd` did not hold for this run. Nothing below uses it, quotes it, or relies on it.

**Arithmetic checked independently:**
- **Match count:** a case-insensitive search of the working tree with the private patterns finds **179 matches in 39 files**. The per-area split also matches: 37/5, 11/3, 13/9, 59/12, 58/9 and 1/1.
- **Test and register totals:** 509 + 3 = 512 register rows, 384 + 2 = 386 Go tests, and 362 + 2 = 364 tests in `dashboard/`. All three are correct.
- **Change list:** 12 files under `docs/designs/` minus the 2 renamed leaves the "10 others" the change list names.
- **ai-manager calls:** the eight calls exist at `aimanager.go:170,182,246`, `aimanager_questions.go:71` and `aimanager_admin.go:86,105,125,146`.
- **Back-office handler:** the anchors at `handlers_bo.go:141-152,148-150,222,320-326` say what the design says.

### Four deliverables of different risk are bundled into one release - Severity: Major
- Affected scope: §1 (four defects), §2 G1–G7, §3.6, §7
- Simplicity rationale: the scrub, the guard and the README rewrite make the repository fit an open licence and need no new code path. The *Integrations* view is a new product feature (four new types, a new file, a template, two tests, a screen that reads credential configuration) and the only reason the security lens is mandatory. Bundling means the urgent scrub waits on the security review of an unrelated feature; rollback is coupled (§10).
- Recommendation: split — v0.45.0 the scrub, `deploy/local/`, the guard, the README and the docs pages; v0.46.0 the view.

### TC-511's QA fixture is refused by the validator it relies on - Severity: Major
- Affected scope: §3.6 TC-511 (`https://qa.example.org/base?x=1`), the `urlHost` paragraph, `QAStatus.OutboundURL`
- Simplicity rationale: `validQaURL` refuses any query string (`dashboard/qaclient.go:70-71`) and any userinfo (`:68-69`); with `?x=1`, `newQaOutbound` sets `why` (`qaclient.go:107-108`), the outbound is off, `OutboundURL` is `""`, and the positive assertion cannot pass while the `x=1` and `/base` negatives test nothing. `urlHost` on the QA URL duplicates work: `s.qaOut.base` is already validated.
- Recommendation: use `https://qa.example.org/base`, keep the `/base` negative; render `base.Scheme + "://" + base.Host`; keep `urlHost` only for `GAOS_AIM_URL`; take `OutboundWhy` from `s.qaSyncGate` (`handlers_qa.go:97`); reuse the no-gateway wording at `vendors.go:388`.

### The seed move uses a mechanism that leaves the files tracked - Severity: Major
- Affected scope: §3.2.2 "Moves"
- Simplicity rationale: `.gitignore` never untracks a tracked file; `git mv` into `deploy/local/seed/<name>/` stages the new paths as tracked, so the customer's catalog [size redacted] would be committed under a new path and the guard would fail on its first run. The sentence also says "removed from the index", which contradicts `git mv`.
- Recommendation: plain `mv` on disk, then `git rm --cached` on the old paths; acceptance: `git ls-files deploy/local` lists only `README.md`.

### The generic `seed.sh` defaults to the fictional example against the production tunnel - Severity: Major
- Affected scope: §3.2.2, "`seed.sh` becomes generic"
- Simplicity rationale: a bare `seed.sh` on the VM would seed "Sample Shop" into production with the prod admin token by default (`seed.sh:19-20`). Idempotent, but a junk product in prod.
- Recommendation: make `--dir` required; with no `--dir`, print the usage and exit 2.

### TC-407 makes a tracked test depend on git-ignored files - Severity: Major
- Affected scope: §3.2.2 "The tests"
- Simplicity rationale: the same commit passes on a fresh clone and may fail on the owner's checkout because of data nobody else can see. The server already validates a seed (`seed_test.go:154-198`) and `seed.sh` stops on the first refusal.
- Recommendation: TC-407 checks the tracked example only, with exact counts; a private seed is checked with `seed.sh --dir <private> --dry-run` or a step beside it.

### The README rewrite repeats the cause of the staleness it fixes - Severity: Major
- Affected scope: §1.2, §3.3 last paragraph, §3.4
- Simplicity rationale: `readme_check` (`deploy/check-workspace.sh:133-144`) checks that the README says a fixed string and never compares it with reality, so it locked the stale numbers in place. New literals will be stale again at v0.46.0 with the check still green.
- Recommendation: (a) the README states no counts or version and the checks are deleted, or (b) `readme_check` compares with `$go_tests` (`:111`), `$qa_rows` (`:104`) and `product.semver` from `toc.js`.

### The root problem, published history, is deferred without saying whether the repository is already public - Severity: Major
- Affected scope: §1.1, N1, §6 A5, §9
- Simplicity rationale: a scrub of the latest commit leaves the catalog one `git checkout c407f11` away, and §10's rollback relies on exactly that. Whether this is a real fix depends on whether the repository is public today.
- Recommendation: state the repository's visibility; turn N1 into an explicit owner decision recorded before merge (an open question or a line in D-085).

### The guard reimplements `git grep` - Severity: Nit
- Affected scope: §3.3
- Simplicity rationale: `git ls-files -z | xargs -0 grep … | cut | sort -u` is what `git grep -IinE -f <file>` does; `2>/dev/null` hides an exit status 2; the full bash drops below the design's level.
- Recommendation: specify the contract (tracked content and paths, path and line only, list-is-tracked fails, invalid pattern fails, absent list skips, blank and comment lines filtered) and suggest `git grep`.

### No alternative versions the operator's private material - Severity: Nit
- Affected scope: §3.2.1, §3.2.4, §6
- Simplicity rationale: a git-ignored directory has no history and no backup; a private repository cloned into `deploy/local/` would fit the same ignore rule.
- Recommendation: add it as A6, adopted or rejected.

### View-model detail goes below the design's level and carries redundant fields - Severity: Nit
- Affected scope: §3.6 "View model"
- Simplicity rationale: `ClientRow.Token bool // always true` is a constant; `APIStatus.Admin/QA/Outbound` duplicate `State` (`api_v1.go:255-261`).
- Recommendation: drop `Token` and either the booleans or `State`; replace the structs with a table of section, fact, source, never-rendered.

### The back-office setup tutorials duplicate the docs tutorials - Severity: Nit
- Affected scope: §3.6 "Template" vs §3.5 page 2
- Simplicity rationale: the same steps in two places will drift.
- Recommendation: each block keeps a status and one "Set up" link; no `<ol>` in the template.

### The view can also be reached through the sheet route without `no-store` - Severity: Nit
- Affected scope: §3.6 "Route and auth"
- Simplicity rationale: `handleBOSheet` builds any view from `?view=` (`handlers_bo.go:137`); `no-store` is set only in `handleBOView` (`:148-150`).
- Recommendation: set the header in one place for both handlers.

### `check-workspace.sh` line citations have drifted - Severity: Nit
- Affected scope: §1.2, §3.3, §3.7
- Simplicity rationale: prose count checks are at `deploy/check-workspace.sh:127-131`, `readme_check` at `:133-144`, the Go-count block at `:111-117`.
- Recommendation: update the anchors.

The rest is solid: §3.1's vocabulary and renames, the §1.1 inventory, N3/A3, the eight-call scope (N5), the §5 arithmetic.

Counts: B=0 M=7 N=6
