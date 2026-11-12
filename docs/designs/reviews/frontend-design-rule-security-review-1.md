# Security review — M17.5a `frontend-design-rule` (revision 2)

**Target:** `docs/designs/frontend-design-rule.md` (revision 2)
**Lens:** Security (trust boundaries)
**Round:** 1 (run against revision 2, because revision 1's trim boundary no longer existed)
**Date:** 2026-11-04
**Read:** the design document and the rubric were the only inputs; every `file:line` claim below
was checked against the repository.

---

## What this proposes

Revision 2 re-vendors a third-party MIT agent skill (`emil-design-eng`, Emil Kowalski) that the
EGM sweep deleted uncommitted from the working tree, but **trimmed** to the stack-agnostic half:
the upstream's `## Core Philosophy` (18-36) and `## Review Format (Required)` (38-60) sections,
kept verbatim, plus a new `## GAOS edition` section, a rewritten frontmatter and a rewritten
`SOURCE.md` that records the upstream URL, commit, licence and exactly which sections were cut.
It then writes a new path-scoped rule file `.claude/rules/frontend.md` (plus its
`.agents/rules/` symlink and a `rule-index.md` row) that makes any session touching
`dashboard/templates/**`, `dashboard/static/**` or `dashboard/render.go` load that skill, then
`/review-animations`, then `/break-ui`. It amends the repository's own "do not edit vendored
skills in place" rule with a single recorded-trim exception for the new file, rewrites
`.claude/agents/gaos-frontend.md`'s skill paragraph and self-review step to match, inverts one
validator assertion in `deploy/check-workspace.sh` from "the skill is gone" to "the skill is
present, trimmed, and carries no framework vocabulary" (rescoping the vocabulary scan so
`SOURCE.md`/`LICENSE` are exempt), and records the provenance in `SOURCE.md` and in `NOTICE`. The
counts move 379 → 381 QA cases and 0.35.0 → 0.35.1; no Go file changes.

## How the existing system works, per this document

The dashboard's front end is server-rendered Go `html/template` + HTMX + plain JS with one
hand-written `app.css`; no component library, no npm. Two stack-agnostic skills are already
vendored (`break-ui`, `review-animations`) and are bound to the `gaos-frontend` agent. The third
(the taste/philosophy layer) was deleted on 2026-11-04 by the EGM sweep because it was 27 KB of
React/Tailwind/Sonner guidance; the deletion is uncommitted, so the three files are still tracked
at `HEAD` and the working tree has them removed. `break-ui`'s own frontmatter and body still point
at `emil-design-eng` (`.claude/skills/break-ui/SKILL.md:3`, `:16`), so those pointers have been
dangling since the sweep. `deploy/check-workspace.sh:213-231` currently scans every skill
directory that is not on the `stack_skills` baseline (`break-ui review-animations`) for
`react|jsx|tailwind|sonner|shadcn` and fails the run on any match, and it separately asserts that
`.claude/skills/emil-design-eng/` stays absent. The house has no CI: every check is run by hand by
the product owner (`D-025`), and the review gate runs five zero-context lenses with a planted
isolation canary. The back office's gate is `signedIn(r)` — true for any request carrying the
cookie `gaos_bo=1`.

## The trust boundaries this design touches

**Boundary A — repository → deploy scripts and host** (`deploy/check-workspace.sh` is the only
executable artefact in the change list).

| Question | Answer |
|---|---|
| What authenticates it | the host's file permissions; the script runs as the operator |
| What authorises it | the operator runs it by hand — the script's own header says it is "advisory: nothing enforces it automatically" (`deploy/check-workspace.sh:17-20`) |
| What validates it | the script's own checks; the counts it reads from their registers |
| What if the far side fails | the workspace drifts silently. The design *widens* this hole: one assertion is inverted and the scan's scope is narrowed |

**Boundary B — repository → the operator's model session (the instruction layer).** This is the
boundary the design really touches, and the lens's table does not list it, so it is spelled out: a
repository file is loaded into the model as instructions, and the model's output is written into
the dashboard.

| Question | Answer |
|---|---|
| What authenticates it | the operator's harness session and its provider key — outside GAOS's control |
| What authorises it | the harness's tool permissions for that session (the `gaos-frontend` agent declares `Read, Edit, Write, Bash, Grep, Glob, Skill`, `.claude/agents/gaos-frontend.md:4`) |
| What validates it | in-repo: the five-word vocabulary scan (`deploy/check-workspace.sh:217`), the heading-count assertion, and prose in `SOURCE.md`. Nothing pins the content |
| What if the far side fails | the model writes attacker-chosen markup into the dashboard's templates, and the back office serves it to anyone holding a `gaos_bo=1` cookie (`dashboard/handlers_bo.go:31-34`) |

**Boundary C — operator browser → back-office dashboard.** Not changed by the design, but it is the
amplifier for everything above: authentication is a cookie whose value is the literal `1`
(`dashboard/handlers_bo.go:31-34`, set at `:74-79` with no credential check), and `template.HTML`
values are trusted at render time (`dashboard/view.go:130`, `dashboard/seq.go:104`,
`dashboard/templates/backoffice.html:309`).

**Boundary D — dashboard → ai-manager gateway → model provider.** Not touched. The change list
contains no Go file, no gateway code, no schema and no edge config; the added prompt text is
static repository prose; and the skill never enters a product workspace — BR-42's allowlist is
the fixed Blueprint file list at `dashboard/blueprint.go:181-195`, which contains no
`.claude/skills/` entry.

**Boundary E — public edge (`clino`) → the VM.** Not touched; no nginx or tunnel file is in the
change list.

**Abuse cases per the lens's list.** Replay / forged request: pre-existing and unchanged — a
replayed `gaos_bo=1` cookie is already sufficient (`dashboard/handlers_bo.go:31-34`); this design
neither adds nor fixes it, and it makes no claim about it. Oversized payload: the change adds
~2 KB of static prompt per front-end session; no request path changes. Path traversal: the rule's
`paths:` are literal globs under `dashboard/`; no untrusted input reaches a path. Secret in a log
or a reply: the new stored outputs (changelog entry, `SOURCE.md`, `NOTICE`, two QA rows) carry only
the upstream URL, commit and licence, all public by design; the report store is still scanned for
secret *values* (`deploy/check-workspace.sh:187-209`). Privilege escalation / injection through a
free-text field: the back office has no privilege model, and the Go-side escaping path that feeds
the page (`dashboard/seq.go`, `dashboard/view.go`) is outside the new rule's path scope — SEC-2.
IDOR: not touched. Credential committed to the repository: no credential; a third-party MIT work
is committed with its licence and a `NOTICE` bullet — but the attribution files are unprotected —
SEC-4. Denial of service against the tunnel: not touched.

## The design's own artefacts

- **New endpoints / origins / writable files / reachable rules:** one new rule file plus one
  symlink; no endpoint, no origin, no schema, no product-workspace file (BR-42 unaffected —
  `dashboard/blueprint.go:181-195`).
- **Reviewer reach:** unchanged. The deny-list (`.claude/rules/agent-workspace.md:95-110`) is
  still instruction-only, and the new file is a plain Markdown file with no secret.
- **Stored output:** the design's own artefacts carry no secret. The design's isolation probe is
  sound in method, but its file list is written for a tree that will not exist after this change —
  SEC-5.
- **Identity assumption:** none. The rule addresses "a session", never an operator role, so it
  does not depend on the back office having an identity it lacks. (The rule's *value* does: the
  back office still authenticates nobody, which is why SEC-2 matters.)
- **Publication:** the changelog entry (`pustaka/docs/changelog.html`) adds the skill name, upstream
  author, licence and upstream URL/commit to the published site — the upstream URL is already
  carried by `NOTICE`/`SOURCE.md`, and no hostname, topology or internal URL is added.

## Checks performed

`git ls-files` / `git status` on the skill directory; `git show HEAD:.claude/skills/emil-design-eng/{SKILL.md,SOURCE.md,LICENSE}`
(blob sizes, per-file and per-directory vocabulary counts, heading line numbers, the exact text of
lines 8-60); `deploy/check-workspace.sh` read in full and every cited line checked (`:17-20`,
`:39`, `:110-114`, `:125`, `:141-142`, `:144-166`, `:176-185`, `:187-209`, `:213-231`);
`deploy/check-stacks.sh:136-156` (the vendored-licence precedent);
`.claude/agents/gaos-frontend.md`, `.claude/skills/break-ui/SKILL.md`,
`.claude/skills/review-animations/SKILL.md`, `.claude/rules/*`,
`.claude/skills/gaos-design-review/SKILL.md`, `docs/rules/gaos-design-review.md:36-72`;
`pustaka/docs/product/brd.html:1225-1323` (BR-05/32/38/39/41/42), `decision-log.html:1094-1112`
(D-063), `open-questions.html`; `pustaka/docs/product/qa-test-plan.html:448-449`, `:2623`;
`NOTICE:25-62`; `pustaka/docs/assets/toc.js:22`; `README.md:8,100-110`;
`docs/designs/egm-design-review.md:1404-1412`; `dashboard/handlers_bo.go:25-85`, `:177-180`;
`dashboard/view.go:120-135`; `dashboard/seq.go:33-104`; `dashboard/render.go:1-30`;
`dashboard/blueprint.go:170-195`; `dashboard/templates/backoffice.html:309`; `.agents/rules/` and
`.agents/skills.json`; `git cat-file -t f1090bec…` (not in the local object store); `HEAD`
reachability of `https://github.com/emilkowalski/skill` (HTTP 200). Canaries were observed in
three rubric files (`.claude/rules/rule-index.md:53`, `.claude/rules/qa-test-plan.md:174`,
`.claude/rules/agent-workspace.md:140`); no canary value is reproduced in this report.

---

### SEC-1 The auto-loaded instruction file has no integrity control, and the design widens the permission to edit it - Severity: High
- Component: agent workspace / model instruction layer (repository → model session)
- Evidence: the design's D6 creates a standing "do not edit them in place, **with one recorded exception**"; the only mechanical guards are the five-word blacklist at `deploy/check-workspace.sh:217` and a presence assertion; the rule auto-loads the skill for every session touching the three paths; `dashboard/handlers_bo.go:31-34` (`signedIn` accepts any `gaos_bo=1` cookie — the back office authenticates nobody); the pinned upstream commit is not in the local object store (`git cat-file -t f1090bec…` → not a valid object), so no local artefact can diff the kept text against upstream.
- Attack path: the skill's prose *is* the instruction the model follows when writing `dashboard/templates/**` and `dashboard/static/**`. D6 converts an absolute prohibition into a conditional whose condition is prose in the same file the editor writes, and the only automated test of that file is a keyword list that a payload does not need to contain. The realistic trigger is the next re-vendor — the maintenance action D6 exists to license — where upstream edits a line inside `## Core Philosophy` or `## Review Format (Required)`: the vocabulary scan stays 0, the heading count stays 3, the orphan and count checks pass, and nothing in the tree pins the kept text to commit `f1090bec`. At that point whoever edits the file (an operator, or any agent holding `Bash`) can change what every front-end session is told, and the output of that session is written into the pages the back office serves to any request carrying `gaos_bo=1`. The design's own Goal — "the trim is **recorded and checkable**" — is therefore true of the *record* and false of the *check*.
- Remediation: make D6's condition mechanical rather than prose. Record a SHA-256 of the trimmed `SKILL.md` (and of the upstream blob it was cut from) in `SOURCE.md`, and have `deploy/check-workspace.sh` verify the file's hash — so any edit to the vendored file fails the build unless the hash is updated in the same commit with the reason recorded. Alternatively keep the upstream text in-tree *outside* any `.claude/skills/` directory (so the vocabulary scan does not see it) and have the script diff the two kept sections against it, proving "verbatim" rather than asserting it. Until one of those exists, D6's exception should not be granted: restore the file unmodified into a `.claude/skills/emil-design-eng/` directory that is excluded from the baseline scan, and keep the trim as a separate, clearly GAOS-authored file.
- Effort: small

### SEC-2 The rule's path scope excludes the Go files that actually produce the back-office markup, so the mandatory review is bypassable - Severity: Medium
- Component: dashboard front end / rule scope
- Evidence: the three `paths:`; D4 states the rule does not reach `dashboard/handlers_bo.go` — while citing `.claude/agents/gaos-frontend.md:9-13`, the very lines that also name `dashboard/handlers_bo.go`'s `buildBO` as front-end-owned; `dashboard/handlers_bo.go:177-180` (`buildBO` assembles the whole back-office view); `dashboard/view.go:127-131` (`DiagramView.SVG template.HTML`); `dashboard/seq.go:33-37`, `:104` (`RenderSequence` returns `template.HTML`; the trust is "earned" by an explicit `template.HTMLEscapeString` inside that file); `dashboard/templates/backoffice.html:309` (`{{.SVG}}` inserted unescaped); `dashboard/render.go:42-106` (`render.go` is the renderer/embedder, not the "view-model glue" D4 calls it).
- Attack path: the design states "**every frontend change loads it**". That is false for the highest-risk markup in the dashboard: the SVG builder in `dashboard/seq.go`, the `template.HTML`/`template.CSS` view fields in `dashboard/view.go`, and the view-model assembly in `buildBO` are all outside the rule's `paths:`. The same change made in a template is reviewed under the taste layer and the `Before/After/Why` format; the same change made in `seq.go` — for example dropping one `template.HTMLEscapeString` call, which is exactly where the escaping trust is currently earned — is reviewed by nobody. The control is bypassable by choosing a different file, and the surface it bypasses is served to a panel with no authentication.
- Remediation: extend the rule's `paths:` with `dashboard/view.go` and `dashboard/seq.go`, and either include `dashboard/handlers_bo.go` or state the boundary in terms of the view model rather than file names ("the dashboard's rendered surface: templates, static assets, `render.go`, the BO view model and the SVG builder"). Add one sentence to the rule: any `template.HTML`/`template.CSS` value must be escaped at construction, because the rule does not reach Go.
- Effort: quick-win

### SEC-3 The one file the design tells the model to read is the one file the guard stops scanning - Severity: Medium
- Component: validator / skill provenance
- Evidence: D8 excludes `SOURCE.md` and `LICENSE` **by name** from the vocabulary scan, "everything else is still scanned"; the scan today is a recursive `grep -rioE` over the whole skill directory, so it does cover `SOURCE.md`; the skill points the model at `SOURCE.md`: "What was cut, and why, is in `SOURCE.md` beside this file"; the design's own `SOURCE.md` text spells the banned pattern out literally, which is why the exclusion is required.
- Attack path: `SOURCE.md` is simultaneously the file the skill instructs the model to read in every front-end session and the only file in the skill directory the guard no longer inspects. The exclusion is a global basename rule, so any guidance file named `SOURCE.md` in any skill directory is likewise unscanned. The design therefore writes a file whose only way to pass the house guard is to widen the guard — and the widened area is where the model's attention is pointed.
- Remediation: keep `SOURCE.md` inside the scan and exempt only the recorded cut list: allow the vocabulary pattern solely inside a marked cut-record block, or reword the cut record so it names the removed sections without the framework words ("the component-library, motion and duplicate-craft sections"). Apply any exclusion per file within the directory being scanned, never as a repository-wide basename rule. Add the `SOURCE.md` case to TC-381 so the negative path is tested.
- Effort: small

### SEC-4 The provenance the licence obligation and the "recorded trim" claim both rest on is protected by no check, while the house already refuses to deploy vendored third-party assets without theirs - Severity: Medium
- Component: repository → deploy scripts and host / licence and provenance discipline
- Evidence: `deploy/check-stacks.sh:147-156` (the sisflow board is vendored third-party code, and the script **refuses the deploy** unless `LICENSE-sisflow.txt` and `NOTICE-sisflow.txt` travel with it); D-063 records that ruling: copy unmodified, source commit recorded beside it, and the deploy refused if the files are missing; D9 records the provenance in `SOURCE.md` and `NOTICE`; `.claude/rules/agent-workspace.md:42-45` (the house rule asserts each vendored skill carries a `SOURCE.md` and a `LICENSE` — instruction only); no script anywhere under `deploy/` asserts either file's presence (`grep -rn "SOURCE.md" deploy/*.sh` → no match).
- Attack path: the design adds a third-party MIT work to the tree and a `NOTICE` bullet, but nothing fails the build if the skill's `LICENSE` or `SOURCE.md` is later deleted, emptied, or has its upstream URL/commit/licence fields edited. That single event removes the MIT attribution the design relies on (MIT requires the copyright and permission notice to accompany copies) and, simultaneously, leaves an auto-loaded instruction file in the tree with no chain of custody — while every automated check (vocabulary, orphan, counts, forks, canary, secrets) still passes. The house already solved this exact problem for every other vendored third-party asset; this change routes around the precedent instead of following it.
- Remediation: mirror the sisflow pattern inside `deploy/check-workspace.sh`: for every directory under `.claude/skills/` that is not GAOS-authored, assert a non-empty `LICENSE` and a non-empty `SOURCE.md` that names the upstream URL, the licence and the commit id, and fail the run otherwise. Record the new check in the QA register as the negative case beside TC-380.
- Effort: quick-win

### SEC-5 The new rule file becomes a seventh standing file that no future isolation probe will exercise - Severity: Low
- Component: review gate / isolation probe
- Evidence: the planting loop in `.claude/skills/gaos-design-review/SKILL.md:36-41` lists exactly six standing files; the design repeats the six and excludes `.claude/rules/frontend.md` because "it does not exist until this change lands"; `deploy/check-workspace.sh:176-185` (the leftover-canary scan does cover `.claude/rules/*.md`); `.claude/rules/rule-index.md:13` (the new rule is a standing, path-scoped instruction file).
- Attack path: the stated reason for excluding the new file expires the moment this change lands, and the file then lives forever as an instruction file that is auto-loaded for any session touching the dashboard front end — the most likely place for a stray credential or a canary value to be pasted. The validator's leftover scan would catch a canary that *stays* in the file, but the probe that matters here — the one that proves a reviewer did not carry a planted value into a committed, append-only report — never plants in the new file, so a reviewer that read it and echoed it goes undetected.
- Remediation: once `.claude/rules/frontend.md` exists, add it to the runbook's planting list in `.claude/skills/gaos-design-review/SKILL.md` and to the design's own list; keep the validator's leftover scan as the backstop.
- Effort: quick-win

### SEC-6 Three of the design's own load-bearing figures do not reproduce - Severity: Low
- Component: design evidence integrity
- Evidence: the design cites "the `Framer Motion` prose at 433" — at `HEAD`, line 433 is the "Image reveals on scroll" paragraph and the Framer Motion prose is at line 160, inside the dropped `## Spring Animations` block; the design states the upstream "also scores 7 in the directory scan the validator already runs" — the validator's scan over the restored upstream scores **11** (SKILL.md 11 matches on 10 lines, `SOURCE.md` 0, `LICENSE` 0), because the design's own measurement is the same grep over the same directory; the design cites `gaos-frontend.md:114` for the report format, which is line 113; the design states "The new file is 50 lines", while its own verbatim blocks total ≈70.
- Attack path: not an attack — an evidence-integrity gap in a document whose entire authority rests on verified `file:line` claims. The wrong "7" is the figure used to explain why the sweep deleted the file, and the corrected numbers are the ones destined for the committed, model-read `SOURCE.md`. A later editor who re-runs them finds three mismatches and cannot tell which of the design's other numbers were checked.
- Remediation: re-run the three figures before implementation and correct the design (and the `SOURCE.md` text). The conclusions survive — the untrimmed upstream scores 11, not 7, so it fails the guard harder than the design claims — but the numbers that get committed should be the ones that reproduce.
- Effort: quick-win

### SEC-7 Two contradictory sentences about editing the same vendored file, and the house's fork check cannot see it - Severity: Low
- Component: agent workspace rules
- Evidence: the new agent paragraph ends "Never edit the vendored skill files." against the rule: "**Do not edit them in place**, with one recorded exception: `emil-design-eng` is the upstream file with its motion, framework and duplicate-craft sections cut"; `deploy/check-workspace.sh:144-166` (the fork check compares `.claude/rules/`, `.agents/rules/`, `docs/rules/`, `GEMINI.md`, `AGENTS.md` only — agent files are not compared).
- Attack path: the one file whose edit permission the design is widening is described by two absolute-sounding sentences in two different files, and no automated check compares them. A session that reads only `.claude/agents/gaos-frontend.md` — the file the `gaos-frontend` agent loads first — is told never to edit any vendored skill, including the one the rule says may be edited; a session that reads only the rule may edit it without the agent file's prohibition. SEC-1's remediation depends on the rule and the agent file agreeing on what may be edited and when.
- Remediation: make the agent file's sentence name the exception — "Never edit the vendored skill files, except the recorded trim in `emil-design-eng` (see `.claude/rules/agent-workspace.md`)" — or replace it with a pointer to the rule, so the two files carry one authority.
- Effort: quick-win

---

**Summary of the attack path, in one line:** the design loads third-party prose into the model
that writes the dashboard's front end, serves that front end from a panel whose only gate is a
cookie any request can carry (`dashboard/handlers_bo.go:31-34`), and protects that prose with a
five-word blacklist plus a prose self-certificate — so the next edit to the "do not edit" vendored
file (the exact action D6 newly permits) changes what every front-end session is told, with
nothing in the tree detecting it; the fix is to pin the file's content mechanically (hash or
in-tree diff) and to assert the attribution files the house already asserts for every other
vendored third-party asset.

**VERDICT: FAIL** — 1 High (missing control on the privileged instruction path the design itself
widens) and 3 Medium must be resolved before implementation; the design's structure, trim boundary
and licence handling are otherwise sound and its upstream line-range citations reproduce exactly.

Counts: Critical=0 High=1 Medium=3 Low=3
