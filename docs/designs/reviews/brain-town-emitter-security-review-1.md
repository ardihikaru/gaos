# Security review round 1 — brain-town-emitter

Canary and positive-control notes: see `brain-town-emitter-review-1.md` (same run). The reviewer reports the
design's private-instance domain by line number only; it is not repeated here (D-085).

## Findings (Critical=2 High=2 Medium=6 Low=3)

| ID | Finding | Severity |
|---|---|---|
| SEC-1 | Hidden products' names and titles sent to a public service; only the receiver hides them | Critical |
| SEC-2 | Design names the operator's private deployment domain (D-085) | Critical |
| SEC-3 | `product.key` from the name lets a hidden and a public product share a key | High |
| SEC-4 | `BRAIN_TOWN_TOKEN` env fallback breaks the file-only secret convention | High |
| SEC-5 | Free-text titles go to a public site unscreened and unbounded | Medium |
| SEC-6 | `agent` ambiguous; the obvious source is an account email | Medium |
| SEC-7 | New egress path missing from the BRD data inventory (L-2) | Medium |
| SEC-8 | Settle check does not guard commit; copies the whole log per event | Medium |
| SEC-9 | A GAOS credential could be used as the Brain Town token; no `bt_live_` detector | Medium |
| SEC-10 | Reviewer deny-list enforced by instruction only; harness loaded `.claude/CLAUDE.md` | Medium |
| SEC-11 | Plain HTTP into the tunnel subnet allowed for a public receiver | Low |
| SEC-12 | Event ids reveal GAOS's global sequence | Low |
| SEC-13 | Response and TLS handling unspecified | Low |

## Triage

| Finding | Severity | Disposition | Note |
|---|---|---|---|
| SEC-1 | Critical | PROPOSE FIX | redaction at source: hidden → name "Private product", title = task id (§1, §3.3); deliberate deviation from the brief, recorded for the receiver's author |
| SEC-2 | Critical | PROPOSE FIX | `town.example.org` placeholder; real URL only in the deploy shell |
| SEC-3 | High | PROPOSE FIX | key = product id (§3.3) |
| SEC-4 | High | PROPOSE FIX | env token removed; `prod.bt-token` mount; `check-stacks.sh` asserts no `BRAIN_TOWN_TOKEN:` key (TC-523) |
| SEC-5 | Medium | PROPOSE FIX | 200-rune cap, control characters stripped, egress detectors, fall back to task id |
| SEC-6 | Medium | PROPOSE FIX | closed field list; `agent` = role only; TC-515 asserts no `@` |
| SEC-7 | Medium | PROPOSE FIX | BRD §8.12.1 row (change 12) |
| SEC-8 | Medium | PROPOSE FIX | post-commit tap; no per-event `Events()` call |
| SEC-9 | Medium | PROPOSE FIX | refuse `gaos_`/equal tokens; `bt_live_` detector in `egress.go` and `check-workspace.sh` |
| SEC-10 | Medium | ESCALATE | tool-permission enforcement of the deny-list is a harness/workspace change outside this feature |
| SEC-11 | Low | PROPOSE FIX | https only; QA host added to the deny list |
| SEC-12 | Low | PROPOSE FIX | ids are `SHA-256(bootKey ‖ seq ‖ index)`; no sequence exposed |
| SEC-13 | Low | PROPOSE FIX | body ≤ 4 KiB discarded; TLS verification never disabled (§3.4) |

No Critical or High finding is left open, so the design may be marked Approved. Approval is by delegation: the
operator asked for an unattended full run, and approval is recorded here by the orchestrating agent.
