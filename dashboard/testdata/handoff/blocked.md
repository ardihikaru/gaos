--- # the D-050 trap: this document rides out on exit code 0
task:        T-1042
run:         R-1042-3
role:        backend
status:      blocked
criteria:
  - id: AC-8   met: false  evidence: "blocked — see blockers[0]"
blockers:
  - "AC-8 contradicts domain rule DR-3; needs a decision, not a retry"
handoff:
  next_role:  architect
  must_know:  "The criterion and the rule cannot both hold."
---

## What I did, and why
I could not do this. The process exited 0 anyway.
