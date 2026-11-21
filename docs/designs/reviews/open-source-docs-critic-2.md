# open-source-docs — critic lens, round 2

Design reviewed: `docs/designs/open-source-docs.md` (v2). Lens: `gaos-goldfish-critic`. Saved with light condensation of the prose; every finding, scope, severity and recommendation is kept. The harness flagged instruction-shaped text in the raw output; none was acted on.

**Isolation note.** Reading a file under `.claude/` attached `.claude/CLAUDE.md` unasked; not used. `rule-index.md:54` holds a marker comment whose value I have not copied. I opened no `deploy/seed/*/` data file and nothing under `deploy/local/`.

**Checked and correct:** the §1.1 arithmetic and the 39-file split; the register counts at `decision-log.html:18`, `open-questions.html:17,76`, `toc/product.js:78,110,114,129,162,168`, `check-workspace.sh:111-131`; 509+3, 384+2, 362+2; `describeAdapter` (`main.go:91-96`), `Smoke` (`:197`), the two view routes (`:359-360`), `buildBO`'s switch (`handlers_bo.go:222`), `requireBO` (`:117-125`), the public list (`auth.go:509`), the gate (`:527-560`), `newAPIConfig`/`addClients`/`Describe` (`api_v1.go:144-261`), `validQaURL` on loopback (`qaclient.go:82-87`), the `url.Error` unwrap (`:174-179`), backoff and attempts (`inbox_worker.go:30-36`), `ExternalPending`/`RecordExternalDelivery` (`external_inbox.go:354-393`); the server builds TC-406's card titles itself (`seed.go:470-485`), so the invented example can pass.

### The phase 1 exit criterion contradicts the guard's own output contract - Severity: Major
- Affected scope: §12 phase 1; §3.3 "Content"; §1.1
- Finding and rationale: one FAIL per `path:line` gives about 179 content FAILs plus path FAILs; §12 says "39+2". §1.1's "matches" are lines.
- Recommendation: choose one granularity and align §3.3 and §12; "matching lines" in §1.1.

### Path masking can fail open and print the private string - Severity: Major
- Affected scope: §3.3 "Masking"
- Finding and rationale: patterns are not lower-cased; awk ERE is not `grep -E` ERE (`mawk` intervals); the FAIL fires and the mask does nothing; the output reaches hand-off reports.
- Recommendation: do not print the matched path — print its index in `git ls-files` or a short hash; or mask with one engine and case folding on both sides; a TC-510 negative that re-greps the output.

### The reviewer deny rules miss the list's canonical location and are narrower than the stated rule - Severity: Major
- Affected scope: §3.2.1; §3.3 "The list"
- Finding and rationale: the main checkout's list is three levels above the worktree; a project `Read(**/…)` rule is anchored to the session's directory; "outside the repository" does not settle whether the main checkout counts; the settings cover less than the rule.
- Recommendation: keep the list outside every checkout (e.g. `${XDG_CONFIG_HOME:-$HOME/.config}/gaos/forbidden-strings`), with the env and `deploy/local/` as overrides; add `Read(**/deploy/local/*)`; the security lens should confirm the anchoring.

### The second-instance edge template can break the :443 default and the transition test checks the wrong blocks - Severity: Major
- Affected scope: §3.2.3, the template and the README section; §4; R2
- Finding and rationale: (1) a name sorting before `clino.conf` takes the implicit `:443` default (rule 3, `gaos.conf:17-19`); (2) a copied `map … $gaos_connection_upgrade` (`:43`) stops nginx (rule 1, `:9-13`); (3) while both files name the host, the first loaded wins and the dry-run may exercise the old blocks.
- Recommendation: name `gaos-<label>.conf`; no `map`, no `default_server`; `nginx -T 2>&1 | grep -c 'conflicting server name'` → 0 after; `curl -sI --resolve` to verify.

### The append-only exception is recorded in the README but not in the policy that is the source of truth - Severity: Major
- Affected scope: §3.1; §7
- Finding and rationale: `docs/rules/gaos-design-review.md:71` says "Append-only, numbered"; round-1 reports were already redacted before D-085 grants the exception.
- Recommendation: the same sentence in the policy file; D-085 names the round-1 redaction as the first use.

### Nobody is told to write the forbidden-strings list before implementation, though phase 1 needs it - Severity: Major
- Affected scope: §12 phase 1; §3.2.4; §10
- Recommendation: §10 step 0 — the list is written in the main checkout before implementation; phase 1 checks it first.

### TC-512's check on the sheet route is vacuous - Severity: Major
- Affected scope: §3.6 TC-511, TC-512
- Finding and rationale: nothing sets `bo.Title = "Integrations"`; TC-511 never asserts `Integrations</h2>`; TC-512's `newTestServer` has no client to leak. The sheet route is public (`auth.go:509`) and answers 200 signed out, so this is the only proof BR-55 holds on it.
- Recommendation: specify the title; TC-511 asserts it on both routes; TC-512's unsigned cases run on TC-511's fixture and lack `mobile-app`, the label and the outbound host.

### Line anchors and function names have drifted - Severity: Nit
- Finding: inbox routes `api_v1.go:403-405`, poll `:404`; README four-path table `README.md:39-44`; "not built" `:89`; `bootChecked` is set where `srv` is built (`main.go:294`), not in `run`.

### The delivery reason is not purely GAOS's own sentence - Severity: Nit
- Finding: `the QA tools could not be reached: <transport error>` (`qaclient.go:179`) can carry an address.
- Recommendation: "GAOS's sentence plus at most 100 characters of the transport error".

### Option A6 cannot be followed as written - Severity: Nit
- Finding: the tracked README makes `deploy/local/` non-empty; `git clone` refuses.
- Recommendation: clone into `deploy/local/private/`, or `git init` + fetch.

### The design does not say how a historical path reference is reworded - Severity: Nit
- Finding: mapping the old seed path to `example` points historical records at different, invented content.

### Ignore the files before moving them, not after - Severity: Nit

### The ai-manager reference omits the vendor query - Severity: Nit
- Finding: every admin call appends `onVendor(vendor)` (`aimanager_admin.go:86,105,125,146`).

### The README env table covers only part of the configuration - Severity: Nit
- Finding: omits `GAOS_EGRESS_*` (`modelconfig.go:89-94`), `GAOS_MODEL`, `GAOS_PROVIDER`, `GAOS_ADDR`, `GAOS_COOKIE_SECURE`.

### TC-511's positive checks use common words - Severity: Nit
- Recommendation: check each fact inside the `bo-integrations` block.

### The back-office docs links assume a sibling /docs/ mount - Severity: Nit
- Recommendation: state the assumption, or make the docs base configurable.

No finding on §2, §6, §9 and §3.7's register arithmetic.

Counts: B=0 M=7 N=8
