# Security review 1: `gaos-third-party-taskmgr.md` (revision 1)

This is the `gaos-goldfish-security` report, with zero context. Private instance, product and address strings
are replaced by placeholders (D-085): `<product>`, `<taskmgr-addr>`, `<edge>`. The triage is in
`gaos-third-party-taskmgr-review-1.md`, rows 15–21.

**Counts:** Critical 0, High 4, Medium 6, Low 4.

The reviewer confirmed that `signedIn()` now verifies an HMAC-signed cookie against the accounts file
(`dashboard/auth.go:330-396`), that every non-public route is gated (`:541-590`), and that cross-site non-GET
requests are refused (`:595-612`). An "admin-only" route can therefore be enforced.

## Trust boundaries

| From → To | Authenticates | Authorises | Validates |
|---|---|---|---|
| operator browser → `POST /products/{id}/third-party` | signed session cookie | `autoFixAdmin` file | kind, code pattern, one binding per kind |
| dashboard → TaskMgr gRPC | Basic, a credential with Admin power | TaskMgr's `CanActForOthers` only | `privateAddr`; 4 MiB cap |
| PR body → TaskMgr lookup, routing, write-back | GitHub | none: the author chooses any code | the code pattern; product checked for alerts only |
| QA tool reply → TaskMgr comment | QA tool token | — | none on `caseCode` |
| repository → host secret file | file permissions | — | no existing guard matched the proposed name |

## Findings

| ID | Severity | Finding | Remediation proposed |
|---|---|---|---|
| SEC-1 | High | A `GRPCAdmin` rest-client is full TaskMgr Admin: it can reset passwords, delete users, grant gRPC access and rewrite any task (`TM/internal/grpcapi/auth.go:42-48`, `TM/proto/taskmgr/v1/*.proto`). "The client has no code path to any other" constrains only GAOS. | A narrow TaskMgr grant for the four methods, scoped to one product; an expiry; a rotation procedure. |
| SEC-2 | High | `prod.taskmgr-auth` matches no `.gitignore` class, no `check-stacks.sh` tracked-file check, and no tag-worktree link loop, so `git add -A` would commit an admin credential to a public repository. | Name it `prod.pr-taskmgr-auth` so every existing guard applies. |
| SEC-3 | High | The documented WireGuard topology is hub-and-spoke through `<edge>`. Plaintext gRPC between two private VMs is decrypted on the hub, exposing the Basic header and staff phone numbers. | Verify the path; if it is relayed, use TLS (TaskMgr already supports it); dial only the configured IP. |
| SEC-4 | High | Write-back does not check validation, so a PR author can make GAOS post an Admin-authored "all merged" comment on any task, including another product's (confused deputy). | A fresh `GetTask` just before `AddComment`; write only on `ok` with the bound product code. |
| SEC-5 | Medium | Role routing lets a PR author redirect notices to any colleague's WhatsApp, and steps 2–3 override a contact's `NotifyOn` opt-out. | Stop at the group when a contact exists and refuses the kind; limit the role step. |
| SEC-6 | Medium | The people table (name, role, login, masked number) is shown to every signed-in account, against the registry's "counts only" rule (`dashboard/contacts.go:26-27`). `ListUsers` loads every product's staff. | Counts for everyone; the table admin-only; state it in BR-62. |
| SEC-7 | Medium | No backoff on auth failure. TaskMgr blocks a peer after 10 failures in 5 minutes and answers code 8 (`TM/internal/auth/basic.go:19-61`), so GAOS would lock itself out and load bcrypt. | Stop all calls on `errAuth` or code 8; back off; rate-limit; cache "no result" briefly. |
| SEC-8 | Medium | QA `caseCode` strings (unvalidated, `dashboard/testplanner.go:146-151`) and TaskMgr's `product_code` flow into comments. | Filter with `apiCaseCodePattern` (`dashboard/api_cards.go:96`) and the code pattern. |
| SEC-9 | Medium | The design names the private instance, product, sibling repository path and gRPC address (D-085). | Placeholders; keep real values in `deploy/local/`. |
| SEC-10 | Medium | The reviewer deny-list names `*.aim-*`, `*.api-*`, `*.bo-*`, but not `*.pr-*`, `*.bt-*` or the new file. It is instruction-only. | One pattern `deploy/stacks/prod.*` plus `/run/secrets/**`, enforced by harness deny rules. |
| SEC-11 | Low | TaskMgr does not keep `github_login` unique (`TM/internal/domain/user/admin_service_update.go:48-54`), so routing by login is nondeterministic. | Drop duplicated logins; count them. |
| SEC-12 | Low | Anyone who can comment can forge the write-back marker, and only the newest 100 comments are read. | Match on GAOS's own author, or page. |
| SEC-13 | Low | Two alert texts tell a PR author whether a code exists and which product owns it. | One text for both cases. |
| SEC-14 | Low | One admin list grants both auto-fix and rebinding; the cache ignores the bound product; behaviour on an oversized answer is not stated. | State the reuse; key the cache by (product code, code) and clear it on change; an oversized answer is never decoded. |
