# docs/designs — the design-first directory

Every feature-sized change to GAOS gets a design document here **before** any
file outside this directory changes. The policy is `../rules/gaos-design-review.md`;
the runbook is `.claude/skills/gaos-design-review/SKILL.md`.

## Layout

```
docs/designs/
  README.md                            ← this file
  <feature>.md                         ← the design under review
  reviews/
    <feature>-review-<n>.md             ← goldfish reviewer reports
    <feature>-security-review-<n>.md   ← security goldfish reviewer reports
```

## Rules for a design document

1. **It stands alone.** A reviewer with no knowledge of the conversation must be
   able to judge it. No "as discussed above".
2. **Every claim carries a file and a line.** If it cannot be anchored, it is an
   assumption and it must be labelled as one.
3. **The change list is complete.** Every file the problem statement names
   appears in a change; every change names its file.
4. **The counts follow from the change list.** If the target says "12 files", the
   arithmetic that gets there is written down.
5. **Alternatives are real.** One rejected alternative with a reason beats three
   that are not considered.
6. **Nothing is left undecided.** If two engineers could implement this and get
   different results, the document is not ready.
7. **Non-goals are listed.** What the change deliberately does not touch.

## Reviews

Reports are append-only and numbered. Never overwrite a report: the sequence of
rounds is the record of how the design converged. The author's triage table
lives at the end of the newest report, one row per finding:

| Finding | Severity | Disposition | Note |
|---|---|---|---|
| `[title]` | Blocker | PROPOSE FIX | what changed in the design |

## Status

A design moves `Draft → In Review → Approved`. Only **Approved** designs may be
implemented. A fix (bug fix, typo, comment, one-line config, routine
maintenance) does not need a design — see the trigger list in the policy.
