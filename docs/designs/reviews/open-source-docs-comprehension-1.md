# open-source-docs — comprehension lens, round 1

Design reviewed: `docs/designs/open-source-docs.md` (v1). Lens: `gaos-goldfish-comprehension`. Saved with light condensation of the prose; every finding, scope, severity and recommendation is kept.

**Isolation note.** The harness inserted the contents of `.claude/CLAUDE.md` into my context without my requesting it. I did not open that file and none of the findings below rely on it. I write no private string here; findings give path and line only.

## Paraphrase

### What this proposes

1. **Scrub.** Reword every mention of the sample product, its organisation and its instance domain in tracked files, using a fixed replacement table. Rename the two design files whose paths carry the product's name, rename one test function, and edit one testdata file. Register IDs, dates and facts stay the same.
2. **Move private material to `deploy/local/`.** A new git-ignored directory; only its `README.md` is tracked. The real seed moves there; `seed.sh` becomes generic at `deploy/seed/seed.sh` with `--dir` and one new FAIL line; an invented "Sample Shop" seed replaces the real one; TC-407 checks the shape of every seed it finds and exact counts only for the example; the edge config's second-instance blocks, the redeploy comment and a compose comment are made generic, the values move to private files; the operator copies the private files back before pulling.
3. **Guard.** Section 7 of `check-workspace.sh` reads regex patterns from the ignored `deploy/local/forbidden-strings` and fails on any match in a tracked file's content or path; path and line only; fails if the list is tracked or holds an invalid pattern; skips when absent. TC-510.
4. **README and GitHub metadata.** Rewrite the README with current figures (386 Go tests, 512 cases, v0.45.0) and update `readme_check`; set description and topics.
5. **Three new docs pages** under a new Integrations group: an overview, a webhooks-and-clients guide with three tutorials, and a reference for the eight ai-manager calls; existing pages get the scrub wording.
6. **A read-only back-office Integrations view** on `GET /ui/bo/{view}`, never rendering a secret; TC-511 and TC-512.
7. **Registers.** D-085, TC-510…TC-512, TC-406/407/411 amended, counts, changelog, v0.45.0. History not rewritten.

### How the existing system works, per this document

- 179 matching lines in 39 tracked files, plus two tracked paths, carry the private instance — reproduced, with every per-area figure.
- The README is stale on version, authentication, test counts and the dev tier; `check-workspace.sh` pins the stale figures.
- The back office is session-gated by `auth.go`'s gate; one handler serves every top-level view through `buildBO`; an unknown view falls through to the product list.
- The machine API has three built-in tokens that must differ, plus registry clients reaching only the inbox routes.
- Only the QA tools receive the answer webhook: 5 attempts with backoff, then gave-up; `ExternalPending` returns only undelivered items not given up on.
- ai-manager is reached through an adapter with a 60 s provider cache; the runner mode comes from `GAOS_RUNNER`.

## Findings

### TC-511's QA outbound fixture is a URL GAOS refuses - Severity: Major
- Affected scope: §3.6 Tests, TC-511; §3.6 `QAStatus.OutboundURL`.
- Finding: `newQaOutbound` runs `validQaURL` (`dashboard/qaclient.go:107`), which refuses any query (`qaclient.go:70-71`). The outbound is then off and `OutboundURL` is `""`; the positive assertion on `https://qa.example.org` cannot pass, and the `/base` and `x=1` negatives pass trivially.
- Recommendation: use `https://qa.example.org/base`, assert `/base` absent; drop `x=1` or move it to a separate negative check expecting the outbound off with the validator's reason.

### The source of the ai-manager host, the "Configured" flag and the fake's credentials is not stated - Severity: Major
- Affected scope: §3.6 `AIMStatus.Configured`, `AIMStatus.Host`; "Sources, all existing"; TC-511.
- Finding: the server keeps only `adapter Adapter` (`dashboard/main.go:84-87`); the URL lives in `ModelConfig` at boot (`modelconfig.go:98`) and on `AIMClient.Base` (`aimanager.go:72`); the mode is not kept. One implementer will read `os.Getenv` at render time, another `Client.Base`; under one reading the `login`/`pw` negatives test nothing. "AIMAdapter-shaped fake" is undefined.
- Recommendation: state the source of each field (e.g. `Host = urlHost(a.Client.Base)` when `s.adapter` is an `*AIMAdapter`); define the fixture as a real `*AIMAdapter` whose `Client` carries the URL and credentials.

### "git mv, then untracked by the ignore rule" does not untrack anything - Severity: Major
- Affected scope: §3.2.2 Moves; §7.
- Finding: an ignore rule never removes a tracked file from the index; `git mv`'d files stay tracked and the commit still publishes the customer's catalog; the guard would fail on them; §3.2.4 and §4 assume the opposite.
- Recommendation: plain `mv`, then `git rm --cached` on the old paths; check that `git ls-files deploy/local` lists only `README.md`, and make the guard assert it.

### How the second instance's override file is applied is never said - Severity: Major
- Affected scope: §3.2.1, §3.2.3, §3.2.4.
- Finding: the override values sit in the script comment today (`deploy/rebuild-restart.sh:95-104`), which warns that a missing value breaks the stack. The design moves them to `deploy/local/instances/<instance>.env` but never says how they reach the environment (`set -a`, or a command prefix), nor whether the manual-probe note (`:105-106`) survives.
- Recommendation: give the exact invocation in the generic comment and in `deploy/local/README.md`; say where the manual-probe note goes.

### Line references have drifted from the files they cite - Severity: Nit
- Finding: README "not built" is at `:89` (design `:93`); README counts `:100-110` (design `:104-113`); third `:443` block `gaos.conf:137-165` (design `:138-161`); `readme_check` `check-workspace.sh:133-144` (design `:130-142`); prose counts `:127-129` (design `:124-125`); the `buildBO` fall-through starts at `handlers_bo.go:277` (design `:289`); Go-count block `:111-117` (design `:111-115`); the GET inbox route is registered at `api_v1.go:404` (design cites `api_inbox.go:222`).
- Recommendation: re-anchor.

### "Eight variables", but nine entries and ten names are listed - Severity: Nit
- Affected scope: §3.2.3.
- Recommendation: fix the count.

### Example seed counts look inconsistent: 12 cases, 5 implemented, 9 planned - Severity: Nit
- Affected scope: §3.2.2.
- Finding: 5 + 9 = 14; the test counts implemented cases inside planned (`dashboard/seed_test.go:340-350`), which the document never says, nor what decision the other 3 carry.
- Recommendation: "9 planned, 5 of which are implemented; 3 with another decision (name it)".

### "TC-406 runs on the example unchanged" contradicts the scrub - Severity: Nit
- Affected scope: §3.2.2 and §1.1.
- Finding: `seed_test.go:45` is TC-406's own `t.Fatalf` and must be reworded, so its message and register prose change.
- Recommendation: say so.

### The provider-list failure mode relies on a cache that does not cover failures - Severity: Nit
- Affected scope: §4.
- Finding: `VendorInfo` caches only on success (`dashboard/vendors.go:288-291`); while ai-manager fails, every render waits up to 10 s.
- Recommendation: state it, or cache the error.

### TC-512 expects `GAOS_QA_URL is not set`, but nil `qaOut` is undefined - Severity: Nit
- Finding: that text exists only when `qaOut` was built with an empty URL (`qaclient.go:103`); test servers often leave `qaOut` nil (`main.go:76-78`).
- Recommendation: specify that a nil `qaOut` renders the same reason, and set up TC-512 explicitly.

### The exception to the append-only rule is not recorded in D-085 - Severity: Nit
- Finding: `docs/designs/README.md:36-37` makes review reports append-only; §3.1 makes an exception D-085 does not mention.
- Recommendation: add the exception and its limit to D-085.

### Undefined or loosely sourced terms - Severity: Nit
- Finding: "ai-claude-cli" is undefined; "the four paths table" is unexplained; "this worktree's commit" and "the report of this change" are author-context phrases; "the defaults' comment" is ambiguous; "ai-manager's own docs" are neither located nor linked; OQ-25 is cited as the source of "a third party", but concerns a vendored board (`pustaka/docs/product/open-questions.html:111`).
- Recommendation: define or link each term; state the wording choice on its own authority.

## Solid sections

§1.1 inventory (179 / 39, every area); the §0 glossary anchors; the §3.3 guard logic (path and line only, the invalid-pattern pre-check); §3.6 route, auth and `no-store` reasoning (`handlers_bo.go:141-152`, `auth.go:551-559`), including the 401 with and without `HX-Request`; §5 arithmetic.

Counts: B=0 M=4 N=8
