# autofix-dedup-and-tabs — security review round 1 (v1)

- **Target:** `docs/designs/autofix-dedup-and-tabs.md` v1, on `cf78d6c`.
- **Reviewer:** a zero-context `gaos-goldfish-security` agent. Its report is transcribed and condensed by the author.
- **Counts:** Critical=0 High=1 Medium=3 Low=3.
- **Trust boundaries the reviewer walked:**
  - the operator's browser to the new POST, behind the HMAC session (`auth.go:329-358`), the origin gate
    (`auth.go:612-660`), SameSite=Lax (`auth.go:503-508`) and `autoFixAdmin`;
  - the dashboard to GitHub, a new close write with the existing write token;
  - the dashboard to the store, with the replay `readOnly`.
- **Abuse cases checked:** forgery, IDOR across products, oversized body, injection, replay, privilege escalation,
  secrets in logs, DoS.

| # | Finding | Severity |
|---|---|---|
| SEC-1 | The design breaks BR-61's "never closes a pull request" (`brd.html:1308`) without amending it | High |
| SEC-2 | The route closes on GitHub before the replay check, so GitHub and the store can split (`main.go:158-164`, `store.go:394-398`) | Medium |
| SEC-3 | The rule X confirm hides that the defect is closed and its cards cancelled; a planted title can steer which defect closes | Medium |
| SEC-4 | `CloseFixPR` does not re-check the guard or the finding under its lock; the handler's checks run on a snapshot before a slow GitHub call | Medium |
| SEC-5 | "Never closes a pull request it did not open" rests on the branch prefix only; `findPR` adopts any author's pull request on that head (`githubwrite.go:413-426`) | Low |
| SEC-6 | Name the error classifier (`github.go:330-349`), apply `afLine` to every §5 string and to `hx-confirm`, cite the real CSRF control, and map the token, refused and rate classes to distinct toasts | Low |
| SEC-7 | The reviewer agent's own off-limits list lacks `deploy/local/**`, which the workspace deny-list has | Low |

## Triage (the author)

| Finding | Severity | Disposition | Note |
|---|---|---|---|
| SEC-1 | High | PROPOSE FIX | BR-61 is amended by D-108 (v2 header, §9), and every repeated promise is in §8 |
| SEC-2 | Medium | PROPOSE FIX | Step 3 refuses a replay before any GitHub call. The close goes through `s.mut(r).CloseFixPR`. TC-909 covers replay and another product's id |
| SEC-3 | Medium | PROPOSE FIX | Rule X is removed. No close path touches a defect, and the confirm says "the defect is not changed" |
| SEC-4 | Medium | PROPOSE FIX | `CloseFixPR` re-runs `closableFix` and the finding under the lock, and answers 409 (§4.3) |
| SEC-5 | Low | PROPOSE FIX (reword) | The claim is restated with its residual, which is accepted: it needs repository write access and is reversible (§4.1) |
| SEC-6 | Low | PROPOSE FIX | `ghErrorClass` is named, `afLine` is applied to every string, `auth.go:612-660` is cited, and the classes are mapped (§5.3) |
| SEC-7 | Low | ESCALATE | It concerns the reviewer agent definitions, not this feature. Recorded for a workspace change |
