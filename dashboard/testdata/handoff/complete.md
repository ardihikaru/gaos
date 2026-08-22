--- # front matter: machine-read, all fields mandatory
task:        T-1042
run:         R-1042-2
role:        backend
status:      complete
criteria:
  - id: AC-7   met: true   evidence: "tests/pricing_test.go::TestRounding"
  - id: AC-8   met: true   evidence: "tests/pricing_test.go::TestBanding"
artifacts:
  - path: "src/pricing.go"     kind: code
  - path: ".gaos/handoff/schema-notes.md" kind: note
handoff:
  next_role:  qa
  must_know:  "Rounding is half-even by DR-3. Do not 'fix' it to half-up."
usage:
  tokens_in:  184320
  tokens_out: 9104
  basis:      measured
---

## What I did, and why
Prose for the human reading the timeline. Everything a machine needs is above.
