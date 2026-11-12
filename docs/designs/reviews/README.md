# Review reports

Every reviewer report lands here, named `<feature>-review-<n>.md` for the
general lenses and `<feature>-security-review-<n>.md` for the security lens.

**Append-only and numbered.** Never overwrite a report and never renumber one:
the sequence is the record of how a design converged, and a report that says
"this was fixed" is only useful if the "before" is still there.

The design under review is `../<feature>.md`. Its directory contract — what a
design document must contain — is in `../README.md`; the policy that says when a
review happens and how it is triaged is in `../../rules/gaos-design-review.md`.

## The triage table

The author's triage table lives at the end of the **newest** report, one row per
finding:

| Finding | Severity | Disposition | Note |
|---|---|---|---|
| `[title]` | Blocker | PROPOSE FIX | what changed in the design |

One disposition per finding, and four of them: `PROPOSE FIX`, `CONTEST`,
`ESCALATE`, `DECLINE`. A severity is never changed, and a comprehension gap is
never contested — the document is fixed instead.

## A report is a permanent record

The reports are committed and every later session reads them, so a report must
not carry a credential. Record **path and line, never the matched text**, and
run `deploy/check-workspace.sh` before committing: it scans this directory with
the detector set GAOS already uses in `dashboard/egress.go` and fails on a hit.

The isolation canary is the same rule from the other side: the probe plants a
unique string in each standing file and asserts it reached no report. **Never
write the canary's value into a report** — a report that quotes the canary is
indistinguishable from a report that leaked it. The validator looks for a
canary *value*, not for the prefix, so the prose that talks about the probe is
fine.
