# gaos-brain-town-consumer — review 3 (readiness)

> **Reviewer:** the zero-context readiness goldfish
> **Document reviewed:** `docs/designs/gaos-brain-town-consumer.md`
> **Rubric:** `.claude/rules/rule-index.md`, `.claude/rules/qa-test-plan.md`,
> `.claude/rules/agent-workspace.md`, plus the domain sources the rule index names.
> **Method:** the zero-decision rule — if the implementer would have to ask "which one
> did they mean?", the document is not ready.

## Summary

The design is well argued and its `file:line` claims mostly hold (verified
`dashboard/braintown.go:57,85-88,90-127,111,121-124,132-148,308-323`,
`dashboard/integrations.go:69`, `deploy/stacks/compose.prod.yaml:140-149`). It failed the
zero-decision rule in four places: the wire format of the event it consumes, the ownership
filter, the hand-over to the flow, and the completeness of the change list. Three `file:line`
claims were wrong or imprecise.

## Findings

### [The stored envelope's JSON shape is never specified] - Severity: Blocker
- Affected scope: §4.1, §5
- What is wrong: The reader parses `data:` as "the stored envelope as JSON" and filters on `productKey`, but `PREvent` has no product-key field, no JSON tags, and no statement of where `Org`, `Repo`, `PRNumber`, `Title`, `Draft` come from in the wire frame.
- Evidence: `dashboard/braintown.go:182-191` shows GAOS's own outbound envelope is an explicit struct with JSON tags; the consumer side has no equivalent. The contract is "in the Brain Town repository" — outside this repository and not labelled an assumption, against `docs/designs/README.md:24-25` (rule 2).
- Fix: Give the envelope's JSON schema inline (field names, types, which are optional), or cite the Brain Town document and label the shape an assumption.

### ["GAOS's own product repositories" is undefined and contradicts the flow design] - Severity: Blocker
- Affected scope: §4.1, §5
- What is wrong: `productKey` is a product identifier/alias, not a repository, and the document never says where GAOS's repository list comes from or how `productKey` maps to it.
- Evidence: `dashboard/model.go:388` — `Repos []string` on `Master`, shaped `owner/repo` (`dashboard/fixtures.go:167`). `docs/designs/gaos-pr-agent-flows.md:195-198` — "The repository comes from the product, never from the event."
- Fix: Name the source (`Master.Repos`, `owner/repo`), the comparison (exact string? case?), and how the reader turns a repo into the product the flow needs.

### [Change list omits the Go files the Integrations row needs] - Severity: Blocker
- Affected scope: §4.3, §6
- What is wrong: `dashboard/integrations.go` alone cannot add a row: the page model lives in `view.go` and the markup lives in the template.
- Evidence: `dashboard/view.go:192-198` (`IntegrationsPage` struct, `BrainTown` field), `dashboard/view.go:370-379` (`BrainTownRow`); `dashboard/templates/backoffice.html:730-736` renders the Brain Town block.
- Fix: Add `dashboard/view.go` (a `BrainTownConsumerRow` type and its field) and `dashboard/templates/backoffice.html` to the change list and to the counts.

### [Change list omits the QA register, the ToC and the release files] - Severity: Blocker
- Affected scope: §6, §11
- What is wrong: §11 requires the new cases to be registered in `qa-test-plan.html`, but §6 never lists that file; the counts it carries, the ToC entry for the new page, and the changelog/semver bump are all absent.
- Evidence: `.claude/rules/qa-test-plan.md:19-24` ("in the same change"), `:72-90` (the four counts), `:97-100` (changelog + `product.semver`). `pustaka/main.go:1340` — "exists on disk but is not registered in the ToC" fails `pustaka check ./docs`; every integration page is registered in `pustaka/docs/assets/toc/integrations.js:54`. `pustaka/docs/product/decision-log.html:18` and `pustaka/docs/assets/toc/product.js:129` say "Eighty-five"; `pustaka/docs/assets/toc.js:22` holds `semver: "0.46.0"`.
- Fix: List `pustaka/docs/product/qa-test-plan.html`, `pustaka/docs/assets/toc/product.js`, `pustaka/docs/assets/toc/integrations.js`, `pustaka/docs/changelog.html`, `pustaka/docs/assets/toc.js`; say whether the decision-log entry is one decision or three.

### [The counts do not add up] - Severity: Major
- Affected scope: §6 "Counts"
- What is wrong: "3 new Go files, 4 changed Go files" contradicts the next sentence ("2 new … 3 changed"), and the breakdown never accounts for the two deploy files.
- Evidence: `docs/designs/README.md:28-29` (rule 4: "the arithmetic that gets there is written down").
- Fix: Write one breakdown that sums to the row count, and say which rows are deploy files.

### [Is the last sequence number persisted across a restart?] - Severity: Major
- Affected scope: §4.2, §4.1, §8
- What is wrong: The reader "keeps the last sequence number it handled and nothing else", yet §8 claims that after a GAOS restart it "reconnects with the last sequence number it handled".
- Fix: Say whether the sequence number is persisted (where, in which file) or whether a restart replays from the beginning, and change §8 to match.

### [The hand-over is a channel here and a call there, and nobody drains the channel] - Severity: Major
- Affected scope: §2, §4.1
- What is wrong: The reader hands events over "through a bounded, non-blocking channel (capacity 256)", but the flow design has `prflow.go` take a `PREvent` by call; the goroutine that reads the channel is in neither document's change list.
- Fix: Name the draining function, its file, and whether drop-on-full is the flow's rule or the reader's.

### [Statuses other than 401, and the wait after 30 s] - Severity: Major
- Affected scope: §8, §4.1
- What is wrong: Only "unreachable" and "401" have defined outcomes; 403 (scope revoked), 404, 429 and 5xx are undefined, and the doc never says whether the 1 s / 5 s / 30 s cycle repeats or stays at 30 s.
- Evidence: `dashboard/braintown.go:512-543` — the outbound feed makes exactly `1+len(waits)` attempts and then stops.
- Fix: Give a status table and state the wait sequence after the third attempt.

### [The token file's mode rule is claimed from a function that only warns] - Severity: Major
- Affected scope: §3, §9
- What is wrong: The design says the rules "copy `readBrainTownToken` … the file must not be readable by others", but that function never inspects the mode — it only logs a warning. Refuse or warn is a real decision.
- Evidence: `dashboard/braintown.go:132-148`; `dashboard/auth.go:249-253` — `warnIfOpen` only logs.
- Fix: State whether the consumer refuses a world-readable file (and with what reason string) or only warns.

### [The `bt_c_` prefix is never enforced or defined] - Severity: Major
- Affected scope: §3, §9
- What is wrong: The token is described as "one `bt_c_…` token" but no rule says the reader checks that prefix.
- Evidence: `dashboard/braintown.go:121-124` refuses a **GAOS** credential, not a `bt_live_` token — the cited lines do not support the analogy as written.
- Fix: Say whether the prefix is required, and cite the right rule for the `bt_live_` refusal.

### [The kinds/types settings have no defined effect] - Severity: Major
- Affected scope: §3, §4.1
- What is wrong: `BRAIN_TOWN_CONSUMER_KINDS` is configurable but the request hardcodes `?kind=github`; `BRAIN_TOWN_CONSUMER_TYPES` is never sent, and an empty or unknown value is undefined.
- Evidence: `dashboard/braintown.go:98-102` — the outbound feed parses a comma list and lowercases it; `dashboard/braintown.go:92-97` — the enabled switch treats anything else as off.
- Fix: Say that the query is built from the configured kinds, that types are filtered client-side only, and what an empty list means.

### [The deploy half of the change is unverified and the token mount unnamed] - Severity: Major
- Affected scope: §6, §11
- What is wrong: The compose change adds a token-file setting but not the secret mount it needs, and "Done when" omits the two stack/workspace checks the repo requires.
- Evidence: `deploy/stacks/compose.prod.yaml:150-158`; `deploy/check-stacks.sh:155-156`; `.claude/rules/qa-test-plan.md:160-168`.
- Fix: Name the container path and the host secret file for the consumer token, add the mount to the change, and add both checks to §11.

### [Wrong line for the "renders no secret" quote] - Severity: Nit
- Affected scope: §4.3
- What is wrong: The quoted sentence is at `dashboard/integrations.go:18`, not `:21-23`.
- Evidence: `dashboard/integrations.go:18`.
- Fix: Correct the citation to `:18`.

### [`EventID` claims a de-duplication purpose the flow does not use] - Severity: Nit
- Affected scope: §5, §8
- What is wrong: The field comment says "for de-duplication", but the flow's key is `<repository>#<pull request number>:<role key>` written onto the card.
- Evidence: `docs/designs/gaos-pr-agent-flows.md:149-155`.
- Fix: Drop "for de-duplication" or say what the event id is used for.

### [The closed docs vocabulary is not addressed] - Severity: Nit
- Affected scope: §6
- What is wrong: A new published page touches the closed CSS-class vocabulary, but the design does not say it invents no classes.
- Evidence: `.claude/rules/rule-index.md:42-44`; `pustaka/docs/integrations/brain-town.html` is the template.
- Fix: Say the page uses existing classes only, and name `pustaka/ai/AUTHORING_SPEC.md` as the source.

---

Verdict: fix first — 4 Blockers, 7 Majors, 3 Nits.

## Triage

| Finding | Severity | Disposition | Note |
|---|---|---|---|
| The stored envelope's JSON shape is never specified | Blocker | PROPOSE FIX | §2 is now a table of the Brain Town contract, member by member, with the section it comes from; §5.2 gives the Go struct and says which frame member each field comes from. |
| "GAOS's own product repositories" is undefined | Blocker | PROPOSE FIX | §5.3 names the source (`Master.Repos`, `dashboard/model.go:388`), the shape (`owner/repo`), the comparison (lower-cased exact) and the outcome (skip and count). |
| Change list omits the Go files the Integrations row needs | Blocker | PROPOSE FIX | `dashboard/view.go` and `dashboard/templates/backoffice.html` are in §6. |
| Change list omits the QA register, the ToC and the release files | Blocker | PROPOSE FIX | The register, both ToC parts, the changelog pair and the four count pages are in §6, with the rule that names each. |
| The counts do not add up | Major | PROPOSE FIX | Recounted by script; the breakdown sums to the row count. |
| Is the last sequence number persisted? | Major | PROPOSE FIX | §4.2: the cursor is `BRAIN_TOWN_CONSUMER_CURSOR_FILE`, mode 600, written after the hand-over. §8 matches. |
| Nobody drains the channel | Major | PROPOSE FIX | §5.4 names `btConsumer.Drain(ctx, handle)` — a method on the reader, in the reader's file — as the channel's only reader, and says the full-channel rule is the reader's. |
| Statuses other than 401, and the wait after 30 s | Major | PROPOSE FIX | §5.1 has the status table (200/401/403/404/429/5xx) and the wait sequence, and says the reader holds at 30 s after the fourth failure. |
| The token file's mode rule is claimed from a warning-only function | Major | PROPOSE FIX | §4.1: the reader **refuses**, citing `dashboard/api_v1.go:133` and BR-52, and naming `readBrainTownToken` as the *other*, looser rule. |
| The `bt_c_` prefix is never enforced | Major | PROPOSE FIX | §4.1: the prefix is **required**; `bt_live_`, `gaos_` and GAOS's own tokens are refused, each with its own reason. |
| The kinds/types settings have no defined effect | Major | PROPOSE FIX | §3 and §5.1: `?kind=` is the configured list, comma-joined; the types are filtered client-side; an empty list means the defaults, and a list with an unknown entry is not an error. |
| The token mount is unnamed | Major | PROPOSE FIX | §6 names both mounts, their host files and their container paths. |
| Wrong line for the "renders no secret" quote | Nit | PROPOSE FIX | Corrected to `dashboard/integrations.go:18-20`. |
| `EventID` claims a de-duplication purpose | Nit | PROPOSE FIX | The comment now says what the event id is: the town's id, `gh-<delivery id>`. The flow's key is the card's, and the card carries it. |
| The closed docs vocabulary is not addressed | Nit | PROPOSE FIX | §6 says the page uses existing CSS classes only and names the authoring spec and the sibling page as its template. |
