--- # a legal document: §7.6 says omit the usage block entirely when nothing was reported
task:        T-1042
run:         R-1042-4
role:        qa
status:      partial
criteria:
  - id: AC-9   met: true   evidence: "tests/smoke_test.go::TestBoot"
  - id: AC-10  met: false  evidence: "not reached"
---

## What I did, and why
Half of it. The tool reported no usage at all.
