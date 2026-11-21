# open-source-docs — readiness lens, round 2

Design reviewed: `docs/designs/open-source-docs.md` (v2). Lens: `gaos-goldfish-readiness`. Saved with light condensation of the prose; every finding, scope, severity and recommendation is kept.

**Isolation note.** Reading files under `.claude/rules/` attached `.claude/CLAUDE.md` to my context unasked; I did not use it. To check the change list I listed the private seed directory's file names and counted matches per file, paths only; I opened no seed data file and nothing under `deploy/local/`. No private string, size or count is in this report.

**Holds up after checking:** the 39-file change list (the union of matching files is exactly the 39 listed); D-085, OQ-36, TC-510…TC-512 and v0.45.0 are the next free values; the gate and `requireBO` claims; the `qaOutbound` and webhook claims; `seed.sh --dry-run` skips the token check; 386 Go tests and 512 rows.

### Phase 1's exit count cannot be reached as the guard is specified - Severity: Major
- Affected scope: §12 phase 1 exit ("39+2 FAIL lines"); §1.1; §3.3 Content and Paths
- Finding: §3.3 prints one content FAIL per `path:line`; §1.1's 179 is a line count, so content FAILs alone are about 179. The path check also matches the five files under the seed directory: 7 path FAILs, not 2.
- Recommendation: state the baseline the contract produces (179 + 7); §1.1 "seven tracked paths"; or a structural exit.

### Masking can leave the private string unmasked - Severity: Major
- Affected scope: §3.3 "Masking"
- Finding: the patterns are never lower-cased against the lower-cased path; awk ERE differs from `grep -E` (older mawk lacks `{n}`); two implementations can mask differently.
- Recommendation: `tolower()` on both, or `sed -E` with the same `-f` file and `I`; assert no printed line still matches.

### TC-510's negative branches and masking have no verification step - Severity: Major
- Affected scope: §3.3; §12 phases 1 and 3; §5
- Finding: the invalid-pattern FAIL, the list-tracked FAIL, the `deploy/local/` tracked FAIL, the skip line and the masking are never run by a stated command, yet the register must quote each.
- Recommendation: one command per branch in phase 1 via `$GAOS_FORBIDDEN_STRINGS` (an invalid pattern; a masked path; a fresh clone; `git add -f` then reset); each run once and quoted.

### BRD's open-question count and range are left stale - Severity: Major
- Affected scope: §3.7 OQ-36; §7
- Finding: `pustaka/docs/product/brd.html:1670-1671` (the total and `OQ-01…OQ-35`) and `:1658` go stale with OQ-36; `brd.html` is not in §7 and no check catches it.
- Recommendation: add it, with the new wording; the answered count unchanged.

### The view's Title and Sub are unspecified, so TC-512's negative check can pass vacuously - Severity: Major
- Affected scope: §3.6, `case "integrations"`; TC-512
- Finding: every `buildBO` case sets `bo.Title`/`bo.Sub` rendered as `<h2>{{.Title}}</h2>` (`backoffice.html:610,637,655`); §3.6 gives neither; TC-511 never asserts `Integrations</h2>`; TC-512's unsigned server is `newTestServer`, with no client to leak.
- Recommendation: `bo.Title = "Integrations"` and an exact `bo.Sub`; TC-511 asserts `Integrations</h2>`; TC-512's unsigned requests use the TC-511 fixture.

### The archive's new no-store on the sheet route has no test - Severity: Major
- Affected scope: §3.6 (`noStoreView` for `archive` on `handleBOSheet`)
- Finding: `GET /ui/bo?view=archive` gains a D-082 header and nothing asserts it.
- Recommendation: TC-511 also asserts `no-store` on both archive routes, message `the %s view on %s is not no-store: %q`.

### Two §3.2 steps are assigned to two different phases - Severity: Nit
- Finding: phase 1 adds the `.gitignore` lines and §3.2.2 step 4 adds them again.

### `bootChecked` is said to be set in `run`, but the function is `serve` and `server` does not exist yet - Severity: Nit
- Finding: `Smoke` is called in `serve` (`main.go:197`); the struct is built at `main.go:294`.
- Recommendation: capture `time.Now()` after `Smoke` and set it in the literal at `:294`.

### New function signatures are not stated - Severity: Nit
- Recommendation: e.g. `func (a *AIMAdapter) CachedVendorInfo() (infos []VendorInfo, at time.Time, busy bool)`, `func (s *server) buildIntegrations(ctx context.Context) *IntegrationsPage`; a stale cache is shown with its age.

### TC-511's external item: the asker and the route are unstated - Severity: Nit
- Finding: `ExternalPending` returns only `qa-tools` items (`external_inbox.go:359-360`).
- Recommendation: asked with `tokQ` on `p-1` through the route, answered through the store, then `RunOnce(ctx, now)`.

### `/base` is a fixed needle where everything else uses a sentinel - Severity: Nit
- Recommendation: `qaSrv.URL + "/" + pathSentinel`.

### The reviewer Read deny rules do not match the agent-workspace rule - Severity: Nit
- Finding: the rule covers `deploy/local/**` except README; the settings cover only `deploy/local/*/**` and the list; the rules bind every session, unstated.

### The design-review policy file is not given the new exception - Severity: Nit
- Finding: `docs/rules/gaos-design-review.md:71` is the source of truth.

### `instance.conf.example` and the replaced qa-sync paragraph lack a check and an anchor - Severity: Nit
- Recommendation: a phase 3 check (`grep -c acme-challenge` → 1, `grep -c gaos_backend` → 0); name `#inbox-clients` (`guide/qa-sync-api.html:233`).

Counts: B=0 M=6 N=7
