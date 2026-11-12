# Goldfish review — security lens

**Target:** `docs/designs/egm-design-review.md` (v1)
**Reviewer:** one fresh-context subagent, zero-context (the security lens — the
half EGM does not ship)
**Date:** 2026-11-04
**Method:** Elephant-Goldfish Model — the author drafted the design; a reviewer with
no transcript and one lens walked the trust boundaries and attacked them. Defensive
only: weakness, attack path, remediation.

## What I think this does

The document specifies a design gate for GAOS: a feature-sized change is written as
a standalone document under `docs/designs/`, five zero-context reviewer agents read
that document alone, each finding gets one disposition, and the fixes are applied to
the *design* rather than to the code. It also specifies — but does not build — the
same gate inside the product as decision D-068 plus three business rules, one open
question, one milestone and eight forward specifications, with no Go code in this
change. Alongside both layers it sweeps GAOS's agent workspace: three rule files
move out of the always-on layer, five reviewer agents and one skill are created, two
skills are deleted, a workspace validator is added, and cross-harness wiring is
created. It closes with a change list of 38 entries over 41 files, a count table,
six rejected alternatives, a risk table and a verification plan. Its security posture
is explicit in exactly one place — the decision to adopt a security reviewer because
EGM ships none — and the rest of the document treats security as a matter of rubric
coverage rather than as a property of the artefacts the change itself creates.

## Findings

### SEC-1 Review reports are a new permanent record with no secret-handling rule, written by agents with repo-wide read reach - Severity: High
- Component: Layer 1 harness — reviewer agents and the report store
- Evidence: design `:267-268` (all five carry `tools: Read, Grep, Glob`); `:271-273`
  (the rubric is every file in `.claude/rules/`); `:332` (reports →
  `docs/designs/reviews/<feature>-review-<n>.md`, append-only, numbered);
  `docs/designs/README.md:33-40`; `dashboard/egress.go:222-238` (GAOS's own rule
  that a record must never carry matched secret text)
- Attack path: A reviewer holds `Read` and `Grep` and is required by the rubric to
  read repository files. Nothing in the design forbids it from reading
  `deploy/stacks/*.env` or `deploy/stacks/*.aim-*` — the host-side files the repo
  itself calls "the gateway's two secrets (the ai-stack login, ai-manager's admin
  key)" and keeps git-ignored. A reviewer that greps for `password|token|secret`
  and quotes a matching line into a finding writes that credential into a file
  that is committed, append-only, read by the operator and by every later agent
  session, and permanent in git history. Every existing GAOS control is on the
  *egress* side; the report is a *storage* channel that no control covers.
- Remediation: Add three rules to `docs/rules/gaos-design-review.md` and to the
  rubric: (a) a deny-list the reviewer must not read — `*.env`, `*.aim-*`, `*.pem`,
  `*.key`, `**/credentials*`, `~/.ssh`, `~/.aws`; (b) findings record path and
  line, never the matched text, mirroring `dashboard/egress.go:222-238`;
  (c) `docs/designs/reviews/` is scanned with the detector set already written in
  `dashboard/egress.go:332-335` before commit, and a hit refuses the commit.
- Effort: small

### SEC-2 BR-48's "only the operator clears it" has no identity to check, and the non-goal forbids the fix - Severity: High
- Component: Layer 2 platform gate — BR-48 authorisation on the back-office control plane
- Evidence: design `:429-431` (BR-48); `:366-368` (D-068 clause 1); `:103-104`
  (non-goal: "No change to `dashboard/*.go`"); `:108-115` (the design itself names
  "its back-office cookie" as security-relevant); `:344` (operator approval is the
  gate's last step); `dashboard/handlers_bo.go:15-24` ("AUTH IS A PLACEHOLDER AND
  SAYS SO … a cookie, no password comparison, no session store"), `:31-34`
  (`signedIn` is `c.Value == "1"`), `:54-81` (login sets the cookie after only an
  email-shape check); `deploy/traefik/dynamic.yml:16-38`; `deploy/edge/gaos.conf:55-80`
- Attack path: The gate's only privileged act is clearing an open Blocker, which
  releases the implement dispatch. The surface where a Blocker is cleared is the
  back office, whose entire authentication is a cookie any visitor can obtain by
  submitting any string containing `@` to the login form — `signedIn()` then accepts
  `gaos_bo=1` from any request. Anyone who can reach the back office can clear the
  gate that BR-48 says only the operator may clear, and the operator has no way to
  distinguish their own clearance from a stranger's. The rule as written is
  unimplementable as stated, and the change list touches no dashboard code, so the
  gap is not closed anywhere in this design.
- Remediation: State the precondition in the rule text, not in a comment: BR-48 must
  name the identity the clearance is checked against and must say the clearance is
  refused until real auth exists. The shape is already written down next door —
  `dashboard/handlers_bo.go:18-20` names pustaka's HMAC-signed cookie, credential
  fingerprint and per-IP throttle as the thing to lift. Alternatively, move the
  clearance out of the dashboard's control plane until that auth lands, and record
  it as a blocking precondition on M17. This becomes Critical if M17 implements
  BR-48 before real auth exists.
- Effort: structural

### SEC-3 The public note's publication step has no credential rule, no disclosure rule and no proposal-mode gate - Severity: High
- Component: publishing step — `docs/articles/` → the public website
- Evidence: design `:99` (goal: "A published note (EN + ID) tells the story with
  the real numbers"), `:488` (entry 15), `:663` (the only treatment: "Drafts first,
  preview second, Ardi's OK third"); the risk table has no row for credentials or
  disclosure; `docs/README.md:16` (the publish mechanism lives outside this
  repository)
- Attack path: The document specifies the *outcome* of the publication step but none
  of its *mechanism*, so four rules are absent and the implementer will improvise
  them: where the publish credential is read from (it must be a host-side file,
  never committed, never passed on a command line or into a model prompt); that a
  failed publish prints the error and not the token; that the step runs in
  proposal/dry-run mode and posts only on an explicit confirmation of the exact
  diff; and that the text may not name host paths, tunnel addresses,
  tenant/product identifiers or the internal hostnames. The one control the design
  does name is a fact check, not a disclosure check, and the text is
  machine-generated at temperature 0.8. The disclosure surface is not
  hypothetical: the project's own published docs already expose the tunnel
  address, both tier hostnames and the database topology, and the article is about
  a product whose back office authenticates nobody.
- Remediation: Add four explicit rules to the design and to the article's own
  checklist: the publish credential is a host-side file outside the repository and
  is never passed as an argument or into a prompt; the publish runs in proposal mode
  by default and posts only after the operator confirms the exact diff; the
  rendered text is scanned with the detector set in `dashboard/egress.go:332-335`
  before posting; and the article must not name host paths, tunnel addresses,
  tenant/product identifiers, or the unauthenticated back office unless the
  operator explicitly approves that disclosure.
- Effort: small

### SEC-4 The reviewers' rubric excludes every rule that actually states the product's trust model - Severity: High
- Component: Layer 1 harness — the rubric; Layer 2 — BR-39/BR-42 dependencies
- Evidence: design `:22-23` ("GAOS's existing rules become the reviewers' rubric"),
  `:271-273` ("The rubric is **every file in `.claude/rules/`**"), `:281-285` (those
  three files hold an index, QA-register mechanics and workspace layout — no
  security content), `:422-431` (BR-46/47/48 land in the BRD, outside the rubric),
  `:445-446` (the isolation choice depends on "BR-39 … and BR-42");
  `pustaka/docs/product/brd.html:1292-1299`
- Attack path: The design's own goal is that a reviewer checks the design against
  the same rules an implementer must satisfy. As specified, that is true only of
  three workspace-hygiene files. The 48 business rules, the decision log, the
  open-question register and the risk register are not in the rubric, so the
  security lens — the one lens whose subject is trust boundaries — is asked to
  reason about the egress pass (BR-32), the drift gate (BR-39), the workspace
  allowlist (BR-42), the closed catalog (BR-05) and the very rules this change
  creates (BR-46/47/48) without ever reading them. The design cites BR-39 and
  BR-42 by number while excluding the file they are in.
- Remediation: Extend the rubric from `.claude/rules/*.md` to `.claude/rules/*.md`
  **plus** the authoritative domain sources — the BRD, the decision log and the
  open-question register — with `rule-index.md` naming which to read for which
  question. Keep the security lens's own scale, but state which domain rules it
  checks against.
- Effort: small

### SEC-5 The marker probe cannot prove the negative it is credited with, and has no positive control - Severity: Medium
- Component: Layer 1 harness — isolation verification
- Evidence: design `:329-330` (plant marker → grep reports → remove), `:352-359`
  ("the file a context-leaking reviewer would read"), `:657` (risk row: "The
  isolation marker proves the instruction, not the tooling"); no file in `.claude/`
  uses `omitClaudeMd`; the repo's two agents declare only `name`, `description`,
  `tools`
- Attack path: `omitClaudeMd: true` suppresses *automatic* loading only; the
  reviewer still holds `Read` and is required by the rubric to read repository
  files, so it can open `.claude/CLAUDE.md` explicitly and hold the marker in
  context without ever quoting it. The grep then returns empty and the probe
  "passes" — a false negative indistinguishable from true isolation. There is also
  no in-repo precedent for the `omitClaudeMd` key, so a runtime that silently
  ignores it fails open and the marker probe is the only backstop.
- Remediation: (a) Add a positive control: one run in which the marker is known to
  be read and the probe is required to fire. (b) Plant N canaries across N standing
  files instead of one token in one file. (c) Widen the check from exact marker
  equality to any verbatim line of the standing file appearing in a report.
  (d) State the probe's claim precisely — "the marker did not reach a report", not
  "the file was not read". GAOS already has a stronger mechanism available: BR-39's
  drift gate compares the *observed context* against a manifest.
- Effort: small

### SEC-6 The Layer 2 reviewer's standing files are a permanent channel the design never constrains by content - Severity: Medium
- Component: Layer 2 platform gate — reviewer context and the persisted review Artifact
- Evidence: design `:438-443` (OQ-32's three candidates are all about *mechanism*),
  `:445-446`, `:373-375` (D-068 clause 3), `:426-428` (BR-47);
  `dashboard/blueprint.go:181-209` (the Blueprint renders `CLAUDE.md`,
  `00-gaos-contract.md`, `10-brd.md`, `20-stories.md`, `30-decisions.md`, the 40-70
  policy files — including `60-security-policy.md`), `:135-150` (GAOS already knows
  a standing file "would reach every turn")
- Attack path: The standing files are inside the reviewer's context by construction,
  whether or not it asked for them. Whatever the product put in its BRD, stories and
  decisions — internal hostnames, tenant and product identifiers, host paths — is
  available to be quoted into the review, and the review is a persisted Artifact in
  the graph that the operator and other agents read. The design states what the
  reviewer must *not* see (the implement brief, the handoffs, the failure history)
  but never constrains what the standing files *may* contain or what the report may
  quote.
- Remediation: Add the content dimension to OQ-32 rather than only the mechanism:
  the reviewer workspace's standing files are limited to the rubric plus a redacted
  index; a review Artifact records path-and-line references and never the matched
  text of a standing file, the same principle as `dashboard/egress.go:222-238`; and
  BR-42's allowlist is named as the control that bounds what enters the workspace.
- Effort: small

### SEC-7 The new validator has no enforcement point and no output-hygiene rule - Severity: Medium
- Component: `deploy/check-workspace.sh` — the drift control the design leaves behind
- Evidence: design `:492` (entry 16), `:538` (TC-357), `:671-674` (the verification
  plan runs it by hand); the repository has no `.github/` directory, so there is no
  CI in which `deploy/check-stacks.sh`'s own header promise is kept;
  `deploy/rebuild-restart.sh:148-160` (the host's env and `.aim-*` secret files are
  symlinked into worktrees, so a script run on the host walks a tree containing them)
- Attack path: The control that "fails when the bloat or the drift comes back" is
  advisory: nothing runs it automatically, so drift returns silently until an
  operator remembers — and the design does not say where it runs. The design also
  does not constrain what the script may read or print. A validator that greps the
  tree and reports the offending line will echo a matched secret from
  `deploy/stacks/*.env` or `*.aim-*` into a terminal or a CI log, which is precisely
  the mistake GAOS's own `Finding` type was designed to avoid.
- Remediation: (a) Name the enforcement point — a pre-commit hook, a call from
  `deploy/rebuild-restart.sh`, or a scheduled job — or state explicitly that it is
  advisory and what compensates. (b) Constrain output: for any path matching
  `*.env`, `*.aim-*`, `*.pem`, `*.key`, print path and rule only, never the matched
  line. (c) Add two validator assertions that close other findings: no
  `EGM-MARKER-` string anywhere in the tree, and no secret-detector hit in
  `docs/designs/reviews/`.
- Effort: quick-win

### SEC-8 `GEMINI.md` and `.agents/skills.json` are unspecified and can fork a security rule silently - Severity: Medium
- Component: cross-harness wiring — entries 17, 18 and 19-21
- Evidence: design `:457` (change list), `:492-496`, `:72-82` (the design's own
  "sharp" observation that a standing file is loaded on every turn), `:272-273`
- Attack path: The rubric question is answered for `.agents/rules/`: those three
  entries are **symlinks**, not copies, so there is no second divergent copy of a
  rule there — that worry is closed by the design as written. The same question is
  *not* answered for `GEMINI.md` and `.agents/skills.json`: neither has specified
  content, and `GEMINI.md` is a standing file loaded by a second harness on every
  turn, the same property the design calls "the sharp one". If `GEMINI.md` restates
  a rule by copy instead of by reference, the security rule forks: one harness
  enforces the new wording and the other enforces the old, and the validator's
  stated scope has no rule that forbids a copied rule.
- Remediation: Specify that `GEMINI.md` contains pointers to `.agents/rules/` and
  no rule text of its own, that `.agents/skills.json` only registers the skill by
  path, and add a validator check that no rule sentence appears in more than one
  file so a forked rule fails the check.
- Effort: quick-win

### SEC-9 The marker is planted in an always-on file with a manual cleanup step and no fence - Severity: Low
- Component: Layer 1 harness — the isolation probe's side effects
- Evidence: design `:329-330`, `:180-181` (the root `CLAUDE.md` exists only to
  `@`-import `.claude/CLAUDE.md`, so that file *is* the always-on layer), `:675-678`
  (the only cleanup is a post-hoc `git diff .claude/CLAUDE.md`)
- Attack path: `.claude/CLAUDE.md` is loaded by every session in the repository. If
  a run aborts — a reviewer crash, a timeout, an operator interrupt — the marker
  survives in the always-on layer and is committed by the next commit, silently
  polluting every later session's context. The marker itself is a hex string and
  carries no secret, so the blast radius today is context pollution; the mechanism
  would be far worse for any token-shaped marker, and nothing in the design
  prevents one being substituted.
- Remediation: Make removal unconditional — plant the marker in a separate file that
  only the standing layer imports, or guard the run with a trap that restores the
  file — and add a validator assertion that the string `EGM-MARKER-` appears nowhere
  in the tree, so an aborted run is caught by the validator rather than by a later
  `git diff`.
- Effort: quick-win

## Not verified

- **All three rubric files could not be read** — they are created by this change, so
  no reviewer can actually run the rubric as specified today; SEC-4 and SEC-8 are
  therefore assessed against the design's *description* of those files.
- `.claude/rules/agent-workspace.md` is specified as holding the workspace layout,
  the trigger list and the vendored-skill policy. Whether it states a reviewer
  deny-list for secret-bearing paths is unknown; SEC-1 assumes it does not, because
  the design does not mention one.
- **The publish mechanism and its token could not be verified.** No gRPC client,
  token file or publish script exists anywhere in this repository. SEC-3 is
  therefore a statement about the *absence of rules in the design*, not a claim that
  any existing publish path handles the token unsafely.
- The `omitClaudeMd` frontmatter key has no precedent in this repository. SEC-5's
  claim is that the design's runtime assertion is unverified in-repo and fails open
  if ignored — not that the key is unsupported.
- `dashboard/egress.go`'s detector list and `pustaka/main.go`'s HMAC cookie are cited
  as *existing* precedents to reuse; only that a reusable set exists was verified.
- The counts, the change-list arithmetic and the non-security claims in F1-F8 were
  out of this review's scope.

Counts: Critical=0 High=4 Medium=4 Low=1
