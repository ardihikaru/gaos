# Security review 1 — `board-and-list-ux.md`

- Lens: `gaos-goldfish-security`, zero context: the design path and the rubric path list only.
- Isolation: canaries planted in the seven standing files before the run, removed after; no canary value appears in this report. Positive control: a `general-purpose` agent told to read `.claude/rules/frontend.md` and `.claude/CLAUDE.md` returned both canaries, so the probe could see them.

**Overall:** no Critical or High findings. The most important is SEC-1: the prune form binds to cards the operator cannot see.

The reviewer also noted that the rubric's boundary table is out of date: the back-office cookie is checked (`dashboard/auth.go:361-382`), the gate refuses non-public routes without a session (`auth.go:500-574`), and cross-site writes are refused (`auth.go:581-598`). Every new or changed surface sits behind that gate. Five abuse cases were checked and found closed: CSRF, an oversized state header (`boardMaxState`), a huge page number (`maxPage` plus the clamp), a stale prune list (the store re-checks at commit, `conversation.go:553-593`), and the PR admin flag (it gates retargeting only).

### SEC-1 Prune's typed consent now covers conversations the operator cannot see — Severity: Medium
- Evidence: design §3.3; D-082 (`decision-log.html:1692`, "a typed `PRUNE` bound to the listed cards"); `templates/backoffice.html:811-817`.
- Attack path: On Archive page 1 the operator sees 5 rows, but the form posts every archived card. Typing PRUNE permanently deletes conversations on pages they never saw. This breaks D-082, and no decision amendment is recorded.
- Remediation: List every card in `AllCards` inside the prune form, or amend D-082 to bind by count. Extend TC-757 so it checks that the form names all 7.

### SEC-2 `BOView.Query` as `url.Values` invites copying the request query into the address — Severity: Low
- Evidence: design §3.3; `urls.go:237-239`; D-095 (no search text in a URL).
- Remediation: Use typed integer fields (`Page`, `APage`, `PPage`) set from `pageParam`, and build the query only in `address()`. Add a test that `q=` never reaches `bo.Href` or `HX-Replace-Url`.

### SEC-3 Not every page key is required to go through `pageParam` — Severity: Low
- Remediation: State that `page`, `apage`, `ppage` and the PR and Auto-fix pages are read only through `pageParam`, and that `prHref` and `afHref` write the clamped page. Add a negative case to TC-758.

### SEC-4 The wizard lock is presentation only; tie it to OQ-37 — Severity: Low
- Remediation: State that `lock` and `product` are untrusted client values, and name OQ-37 (back-office roles) as the trigger for a server-side check.

### SEC-5 Published changelog and register edits do not name the publication guards — Severity: Low
- Remediation: Require placeholder product names in published prose, and run `./deploy/check-workspace.sh` (the D-085 and TC-751 guards) in verification.

Counts: Critical=0 High=0 Medium=1 Low=4

## Triage

| Finding | Severity | Disposition | Note |
|---|---|---|---|
| SEC-1 prune consent | Medium | PROPOSE FIX | The prune confirmation now lists every card it will prune, outside the pager, so the typed PRUNE stays bound to cards the operator has seen. D-097 records the amendment to D-082. TC-757 asserts that all 7 are named. |
| SEC-2 typed query | Low | PROPOSE FIX | `BOView` gets `Page`, `APage` and `PPage` as integers, encoded only in `address()`. TC-756 asserts a stray `q=` never reaches the address. |
| SEC-3 pageParam everywhere | Low | PROPOSE FIX | §3.1 now says every page key is read through `pageParam`, and the href writers receive the clamped page. A negative case was added to TC-758. |
| SEC-4 lock and OQ-37 | Low | PROPOSE FIX | §3.6 and §6 now name OQ-37. |
| SEC-5 publication guards | Low | PROPOSE FIX | §5 requires placeholder names, and §9 runs `check-workspace.sh`. |
