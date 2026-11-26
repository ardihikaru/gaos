# Security review: `pustaka-loading-and-motion` (design v1, In Review)

## What this proposes

1. **A page that looks right while it loads.** Today the header, sidebar and table of contents only appear once `site.js` has run, so the article jumps and the theme flashes. The CSS is changed so the first paint already has its final layout: named grid areas, a reserved strip for the header, a sidebar placeholder, and landing-page styles keyed on `data-layout`. The eight table-of-contents files are fetched in parallel but still run in order. The runtime marks `<html>` with `ready` before hiding anything for the reveal animation.
2. **Motion.** Partial navigation animates only the article. There is a progress bar for slow fetches, a press state on buttons, separate open and close timings for the drawer, and nothing moves under reduced motion.
3. **A server-side head snippet.** `pustaka/main.go`'s static handler reads every non-underscore `.html` file into memory. It inserts a marker, an inline theme script (reads `localStorage`, sets `data-theme`) and a font preload whose base path comes from the page's own stylesheet link. Then it serves the buffer. No CSP exists and none is added. This is tested by TC-721.
4. **Public docs.** On both prod VMs, `PUSTAKA_AUTH` is set to empty in the untracked `prod.docs.env`. That turns the docs login off without removing its code, so every page, the search index and the partials become readable by anyone (G5, R4). The old user, password and secret values stay in the file. This is recorded as D-093.

## How the existing system works, per this document

- pustaka is a vendored fork of an upstream docs engine. A Go server serves the HTML pages, and the browser runtime builds the page frame around them.
- The login layer is a signed-cookie gate. It is on only when `PUSTAKA_AUTH` is truthy, and each prod VM's `prod.docs.env` sets it. Both prods have it on today.
- Pages are served through `http.ServeContent`, behind a gzip middleware. A partial endpoint returns a page's `<main>` content as JSON.
- R4 says the docs were already checked for private strings under D-085, and that `deploy/check-workspace.sh` section 7 enforces this.

## Trust-boundary walk

| Boundary | Touched? | Authn / Authz / Validation / If the far side fails |
|---|---|---|
| Anonymous internet → edge → Traefik `/docs` → pustaka | **Yes, opened** | Authn goes from a signed cookie to nothing. Authz becomes none. Validation is `safeJoin` (`pustaka/main.go:435-449`). If pustaka fails, only the docs go down. No rate limit or resource cap exists on this path (see SEC-5). |
| Docs origin ↔ back office `/app` | Indirectly | Both sit on **the same origin** (`deploy/traefik/dynamic.yml:1-2`, `40-48`). The back office now verifies signed sessions (`dashboard/auth.go:361-382`, D-079), so the rubric's premise that "`signedIn()` checks nothing" is out of date. However, its CSRF control only refuses non-GET requests that the browser marks as cross-origin (`pustaka/docs/product/decision-log.html:1618`). Script running under `/docs` counts as same-origin. |
| Repository → published site | **Yes** | Every tracked file under `pustaka/docs/` becomes world-readable (`deploy/Dockerfile.pustaka:15`). |
| Host env file → container | Yes | This is a manual, untracked edit on two hosts. The credentials stay on disk. |

Abuse cases I considered and found **not** to apply:
- Path traversal: `safeJoin` is unchanged.
- XSS through search: the query is escaped at `pustaka/docs/assets/site.js:456`.
- Request-derived data in the injected snippet: the design takes `<base>` from the page file, not from a header.

## Findings

### SEC-1 Turning the login off publishes the deployment's network topology and its public edge address - Severity: Critical
- Component: published docs content / the `/docs` boundary
- Evidence:
  - The design calls this exposure "the point of G5": `docs/designs/pustaka-loading-and-motion.md:160` (R4).
  - The published pages name:
    - the edge's public address, its SSH alias and login user, and both hosts' tunnel and LAN interfaces: `pustaka/docs/infra/overview.html:38-42`;
    - the WireGuard subnet, listener and UDP port: `pustaka/docs/infra/overview.html:69-71`;
    - the published bind addresses and ports for each environment, and the database URLs: `pustaka/docs/infra/environments.html:69`, `73`, `110`;
    - the ai-manager tunnel address: `pustaka/docs/infra/environments.html:199`;
    - working probe commands with real host headers: `pustaka/docs/infra/environments.html:218-231`, `252-253`;
    - an example public ai-manager host: `pustaka/docs/architecture/gateway.html:238`.
  - In total, 68 host, IP or tunnel references across 12 published files.
- Attack path: any anonymous internet user reads `/docs/infra/*`. They get the one public IP, the SSH user to target, the WireGuard port, the internal addressing plan, and which machine holds "everything an attacker would actually want" (`overview.html:30-33`). Today this is behind a login. After D-093 it is on the public internet and in the search index (`/__pustaka/index.json`, `main.go:499-504`). I rated this Critical rather than High because the rubric defines data exposure as Critical, and nothing in the design limits the exposure to non-sensitive pages.
- Remediation: before the login is turned off, add a step that audits the published content against an explicit "not for the public web" class: IPs, SSH users, ports, interface names, real hostnames. Then do one of these:
  - redact `pustaka/docs/infra/*` and the cited lines to placeholders (as D-085 did with `gaos.example.org`);
  - stop shipping the `infra/` section in the public image;
  - keep the login on and publish a reviewed subset.

  Add those patterns to the section 7 guard so they cannot return. Make "the audit is clean" a precondition in §3.6 and a check in §7.
- Effort: small (redact) to structural (split the site)

### SEC-2 R4's claimed control does not cover this exposure, and it passes without checking anything when its list is missing - Severity: High
- Component: `deploy/check-workspace.sh` section 7 / design R4
- Evidence:
  - `docs/designs/pustaka-loading-and-motion.md:160` says D-085 reviewed the docs and section 7 "enforces that".
  - D-085 covered only "one operator's private instance": a sample product, an organisation, a domain (`pustaka/docs/product/decision-log.html:1788-1794`).
  - Section 7 matches only a git-ignored pattern list. When the list is absent it prints an `info` skip, not a failure (`deploy/check-workspace.sh:301-310`, `360-361`).
  - D-085 says "the deployment's domain" must never appear in the repository (`decision-log.html:1791`). A real (non-`example.org`) deployment domain still appears 24 times under `pustaka/docs/` (`infra/overview.html`, `infra/environments.html`, `infra/neighbours.html`, `index.html`, `architecture/gateway.html`). Either the guard does not hold that pattern, or that domain is not the one D-085 meant. The design does not say which.
- Attack path: the operator relies on R4 and on a `check-workspace.sh` run that is green (or skipped) in §7 Verification. They turn the login off, believing the content has been vetted, and SEC-1's material goes public.
- Remediation:
  - Restate R4 accurately: what D-085 covered, and what it did not (topology, security posture).
  - Make §7 Verification run section 7 with the list present, and fail if the step is skipped.
  - State whether the deployment domain cited above is meant to be public. If it is not, add it to the guard.
- Effort: quick-win

### SEC-3 The design makes the docs public while the gate on going public is still open - Severity: High
- Component: decision governance / D-093
- Evidence:
  - `docs/designs/pustaka-loading-and-motion.md:6` says "no OQ is opened or answered", and `:33-35` says "GAOS is going public".
  - D-085 says the repository "is not made public until [OQ-36] is answered" (`pustaka/docs/product/decision-log.html:1804-1805`).
  - OQ-36 is still open: "today: the repository stays private" (`pustaka/docs/product/open-questions.html:122`, `884-902`).
  - BRD R-02 also says the open authentication gap "blocks publishing anything that binds beyond localhost" (`pustaka/docs/product/brd.html:1426`).
- Attack path: this is not an exploit. A ship-blocking control (the OQ-36 gate) is bypassed sideways. The docs site serves the current tree's design record — BRD, decision log, QA plan, changelog — to the public before the operator has decided the repository may be public.
- Remediation: in D-093, cite OQ-36, D-085 and BRD R-02, and record the operator's explicit ruling on whether publishing the docs site falls inside or outside the OQ-36 gate. If it falls inside, G5 waits for OQ-36. Correct line 6.
- Effort: quick-win

### SEC-4 The site going public states that the back office "checks nothing" and spells out residual authorisation gaps - Severity: Medium
- Component: published security-posture text
- Evidence:
  - Stale claims that the back-office login accepts anything: `pustaka/docs/product/brd.html:157`, `335`, `1426`; `pustaka/docs/product/faq.html:294`; `pustaka/docs/assets/toc/product.js:50`.
  - These contradict D-079 (`decision-log.html:1607-1618`) and the code (`dashboard/auth.go:361-382`).
  - The decision log also tells readers that a rollback to an older tag "brings the forgeable" cookie back (`decision-log.html:1623-1624`). Other published pages list gaps that are still open: no roles (`open-questions.html:904`), and events that carry no actor (`decision-log.html:1621`).
- Attack path: an anonymous reader learns that `/app` on the same origin is (they are told) unauthenticated, and so worth probing. They also learn that a rollback would reopen a session-forgery bypass, which is a timing signal to watch for.
- Remediation: correct the stale "checks nothing" statements before D-093 ships. Review pages that describe known weaknesses or rollback hazards, and decide which belong only in a private operator runbook.
- Effort: small

### SEC-5 Anonymous resource use: per-request page buffering, open search, no limits - Severity: Medium
- Component: `pustaka/main.go` static handler, `/__pustaka/search`, deployment
- Evidence:
  - The design buffers every `.html` request whole, up to 654 KB (`docs/designs/pustaka-loading-and-motion.md:87`, `158`).
  - R2 accepts this "per page view" without considering that, after D-093, page views are anonymous.
  - `/search` and `/index.json` become anonymous too (`pustaka/main.go:499-510`).
  - The docs service has no `mem_limit` or `cpus` (`deploy/stacks/compose.prod.yaml:209-223`).
  - The edge config has no `limit_req` or `limit_conn` (`deploy/edge/gaos.conf`, which has only `client_max_body_size`).
  - That VM runs "every container, every product" (`pustaka/docs/infra/overview.html:44`).
- Attack path: an anonymous client repeatedly requests the largest pages and `/search` queries, making the server read, buffer, gzip and search on every request. This consumes CPU and memory on the shared VM and bandwidth across the 1-vCPU edge and tunnel.
- Remediation:
  - In `--prod`, compute the injected bytes once per file (at startup, or cached by path and mtime) instead of on every request.
  - Set `mem_limit` and `cpus` on the `docs` service.
  - Add an nginx `limit_req` zone for `/docs/__pustaka/search` and `index.json`.
  - Restate R2 with anonymous traffic in mind.
- Effort: small

### SEC-6 The new inline script runs on the same origin as the back office, and the design does not analyse it - Severity: Medium
- Component: `injectHead` / origin layout
- Evidence:
  - The design adds an inline script to every page and dismisses CSP in one line (`docs/designs/pustaka-loading-and-motion.md:93`, `102`).
  - `/docs` and `/app` share one origin (`deploy/traefik/dynamic.yml:1-2`, `40-48`).
  - The back office's CSRF defence only refuses requests the browser marks cross-origin (`pustaka/docs/product/decision-log.html:1618`).
  - The theme key in `localStorage` is shared with the back office's origin.
- Attack path: the snippet as specified is static and safe. But nothing in the design or in TC-721 requires it to stay free of request-derived data (headers, query, `X-Forwarded-Prefix`, which `sitePrefix` reads at `main.go:836`). A later edit that copies such a value into the snippet would give script on the back office's origin, where a signed-in operator's session works with no CSRF barrier. Because the design adds no CSP, there is no backstop.
- Remediation:
  - In §3.4, state the same-origin fact and the invariant: "the snippet contains no request-derived bytes".
  - Add a TC-721 assertion that responses for different `Host` and `X-Forwarded-Prefix` values carry identical snippet bytes.
  - Record the script's SHA-256 so a later CSP can allow it by hash rather than `'unsafe-inline'`.
- Effort: quick-win

### SEC-7 `reAssetBase` is unspecified, yet its match is copied into an HTML attribute - Severity: Low
- Component: `injectHead`
- Evidence: `docs/designs/pustaka-loading-and-motion.md:93` and `168` name `reAssetBase` but give no pattern and no escaping rule.
- Attack path: the input is repository-authored, so this is a robustness and future-proofing gap, not a live path. A loose pattern could carry quotes or `>` from a malformed link into the injected `<link>`.
- Remediation: specify the regex so `<base>` can only be `(\.\./)*`, and say that anything else skips the preload. Add a negative case to TC-721.
- Effort: quick-win

### SEC-8 Leftover docs credentials stay on both hosts, and turning the login back on revives the old secret - Severity: Low
- Component: `prod.docs.env` (host, untracked)
- Evidence: `docs/designs/pustaka-loading-and-motion.md:138`. Cookies are signed under that secret (`pustaka/main.go:704-716`). §3.6 edits `deploy/stacks/docs.env.example`, which matches the reviewer deny-list pattern `*.env.*`, so this reviewer could not verify that change.
- Attack path: an unused password and signing key sit on two hosts with no purpose. If the layer is turned back on, cookies issued under the unchanged secret are accepted again until their TTL runs out, and a reused password stays valid.
- Remediation: blank `PUSTAKA_AUTH_PASS` and `PUSTAKA_AUTH_SECRET` when turning the login off, and document "generate a new secret" as part of turning it back on. Name the comment line to be added to the example file inside the design, so reviewers can check it without opening the file.
- Effort: quick-win

### SEC-9 The dev docs stack's status is not stated - Severity: Low
- Component: `deploy/stacks/compose.dev.yaml:105` (`dev.docs.env`)
- Evidence: §3.6 covers only the two prod VMs. Dev is reachable through the same edge under a dev host (`pustaka/docs/infra/environments.html:230`).
- Attack path: the docs say nothing about whether dev stays gated. If someone turns dev's login off "for consistency", unreleased pages are published as well.
- Remediation: state explicitly in §3.6 whether dev stays gated, and why.
- Effort: quick-win

### SEC-10 The design document itself adds host names and home-directory paths to a tracked file - Severity: Low
- Component: `docs/designs/pustaka-loading-and-motion.md`
- Evidence: `docs/designs/pustaka-loading-and-motion.md:136` names both prod VMs and their checkout paths.
- Attack path: the repository is meant to go public eventually (D-085 and OQ-36). Each such line adds to what SEC-1 and SEC-2 must later scrub.
- Remediation: refer to "both prod VMs" and "the deploy checkout" instead.
- Effort: quick-win

Counts: Critical=1 High=2 Medium=3 Low=4
