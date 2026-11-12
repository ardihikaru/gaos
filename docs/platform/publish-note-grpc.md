# Publishing a note on mfardiansyah.id over gRPC

**Owner:** Ardi (approves every publish) · **Scope:** the personal site at
`mfardiansyah.id` · **Status:** procedure, not a licence to publish. Every
publish needs Ardi's explicit OK.

This is the repo-local source of truth for the publish procedure. It is mirrored
into the memory store of each CLI that works in this repo — Claude Code,
Antigravity, MiniMax Code — so the procedure is available from any of them.
The mirrors point here; this file is the copy that is edited.

## Before you start

- **Proposal mode must be OFF.** Every token write is refused with
  `PermissionDenied: proposal mode is on: this call cannot be held as a
  proposal, use the back office` while it is on. Ardi toggles it in
  `/admin/proposals`. **Switch it back ON when the publish window closes.**
- **Toggling proposal mode needs a browser session — the token cannot do it.**
  `ProposalService/SetMode` answers `PermissionDenied: this action needs a
  browser session`, and the HTTP route rejects the api token with 401: the
  handler is wrapped in `sessionOnly` on purpose (*"a token must not switch
  the mode off or decide its own held calls"*). The working path is a real
  operator session:

  ```bash
  # 1. sign in with the Hikaru admin password -> cookie mfs_admin + csrf
  PW=$(cat ~/.config/hikaru/admin-login.txt)
  python3 -c 'import json,sys; json.dump({"password":sys.stdin.read().strip()}, sys.stdout)' <<<"$PW" \
    | curl -s -c /tmp/mfs.cookies -o /tmp/session.json \
        -H "Content-Type: application/json" -d @- \
        https://mfardiansyah.id/api/admin/session
  # -> {"user":"ardi","expiresAt":"...","csrf":"..."}

  # 2. flip the switch: cookie + X-CSRF-Token + Origin (all three)
  python3 -c "import json; open('/tmp/csrf.hdr','w').write('X-CSRF-Token: '+json.load(open('/tmp/session.json'))['csrf'])" 
  curl -s -b /tmp/mfs.cookies -X PUT -H "Content-Type: application/json" \
       -H @/tmp/csrf.hdr -H "Origin: https://mfardiansyah.id" \
       -d '{"enabled":true}' https://mfardiansyah.id/api/admin/proposals/mode
  # -> {"enabled":true,"pending":0}

  # 3. sign out again, then remove the session files
  curl -s -b /tmp/mfs.cookies -X DELETE -H @/tmp/csrf.hdr \
       -H "Origin: https://mfardiansyah.id" https://mfardiansyah.id/api/admin/session
  ```

  `Origin` must equal `SITE_URL` (`https://mfardiansyah.id`) or the CSRF check
  fails. The login is audited, and the back office's lock-out is 5 failures
  per IP — so get the password right the first time. While the mode is on,
  every token write over gRPC is *held as a proposal* rather than applied, and
  `Restore`, `Publish` and `PostService` writes are refused outright.
- **Never run a sub-step with `claude -p`.** The OAuth session expires in child
  processes ("OAuth session expired"). Run everything from the interactive
  session.
- **Never print the token.** Read it into a shell variable and pass it as a
  header. Never echo it, never commit it, never put it in a model prompt.

```bash
TOKEN=$(python3 -c "import json,os;print(json.load(open(os.path.expanduser('~/.config/hikaru/mfardiansyah.id-token.json')))['api_token'])")
# gRPC prod endpoint: 10.10.0.5:8122, package mfardiansyah.backoffice.v1
# away from the tunnel: ssh -L 18122:10.10.0.5:8122 clino
```

## The flow

All calls are `mfardiansyah.backoffice.v1.PostService/*`. Use `-d @ < file.json`
for long bodies — grpcurl reads the payload from stdin.

1. **Create both drafts.** `Create` × 2, body `{"body":{…postInput…}}`:

   | Field | English | Indonesian twin |
   |---|---|---|
   | `slug` | `<slug>` | `<slug>-id` |
   | `locale` | `en` | `id` |
   | `translationGroup` | the same value on both | same |
   | `status` | `draft` | `draft` |
   | `source` | `human` | `human` |
   | `shareLinkedin` | `true` | `false` |
   | `linkedinLang` | `en` | — |

   Required: `slug` (`^[a-z0-9]+(-[a-z0-9]+)*$`), `title`, `body`, an existing
   `category`. `excerpt` is auto-derived if blank but should be written by hand
   — it becomes the LinkedIn card description and the Hikaru push summary, and
   **it is capped at 200 runes**. `readTime` is **minutes**; set it explicitly
   (it is auto-derived only when sent as 0, at 220 wpm, and is **not**
   recomputed on a later patch). `title` is capped at 200 characters;
   `metaTitle` ≤ 60 runes; `metaDescription` ≤ 160 runes. Answer 201.

   Category for an AI-engineering note: `open-source-tools`. `tags` is up to
   six lowercase English words.

2. **Preview the LinkedIn commentary.**
   `PreviewLinkedin` `{"id":"<en-slug>","query":{"lang":"en"}}`
   (~10–60 s; use `-max-time 200`). The commentary is **rewritten at temperature
   0.8**, so the posted wording differs from the note — check the facts and the
   attribution in the preview, not the style.

3. **⛔ Gate — stop and show Ardi.** The full EN text, the ID text and the
   LinkedIn preview. **Publish only after his explicit OK.** This gate survives
   a general approval of a plan: it governs content he has not read, and the
   publish is a public, LinkedIn-visible action.

4. **Publish.** `Bulk` `{"body":{"op":"publish","keys":["<en>","<id>"]}}` →
   `{done, failed, warnings}`. Failures are per key, not fatal; a failed key
   stays a draft and can be republished on its own. **A pair posts one LinkedIn
   post, and only once both versions are live.**

5. **Verify.**
   - `curl` `/blog/<en>` and `/id/blog/<id>` → 200, with the EN|ID switch
     linking the twins.
   - `LinkedinHistory {"id":"<en-slug>"}` shows `ok:true` and a `linkedinUrl`.
   - **Read the posted text back** from
     `https://www.linkedin.com/feed/update/<urn>/` — a plain `curl -L` with a
     desktop UA answers 200 and the commentary is in
     `<meta name="description">`. The rewrite can drift the facts, so check
     every number and attribution against the note.
   - A patch on an already-shared note does **not** repost: `LinkedinHistory`
     must still show the single original attempt.

6. **Optional — internal links.** `SuggestInternalLinks {"id":"<slug>"}` → Ardi
   picks → `ApplyInternalLinks {"id":"<slug>","body":{"version":<v>,"links":[{phrase,slug}],"readAlso":[slugs]}}`.
   Live notes only, never shares on LinkedIn. EN notes link `-en` slugs, ID
   notes the ID slugs.

## Body Markdown subset

The renderer supports `##` and `###` headings, fenced code blocks, `>` quotes,
ordered and unordered lists, paragraphs, and inline `code` / `**bold**` /
`[link](url)`.

**Not supported:** tables (no `|` handling — a row renders as a plain
paragraph), images, `#` H1, and `---` horizontal rules (they render
literally). Keep the strongest facts early: the LinkedIn writer only sees the
first ~8000 runes of the body.

## House style for a hand-written note

1. First person, present tense, concrete — "I ran it on this site's own agent
   setup".
2. Open with 2–3 narrative paragraphs **before the first heading**.
3. Paragraphs of 2–6 sentences, one idea each.
4. Sentence-case, verb-ish headings that promise content: "What I found", "What
   I changed", "Caveats", "Sources", "Read also" (`## Baca juga` in
   Indonesian).
5. Versions, paths and flags in `backticks`: `v0.37.7`, `make validate`,
   `omitClaudeMd: true`.
6. Bold lead-ins for list items and caveats, never for whole paragraphs.
7. Numbered lists for transferable lessons; bullets for inventories.
8. Attribute claims — "Rensin estimates that about 30% of Goldfish suggestions
   are worth acting on".
9. A `## Caveats` section (bolded lead-ins, one line each) **before**
   `## Sources`.
10. `## Sources` as a bullet list of `- [Title](url)` links.
11. `## Read also` as a bullet list of internal `- [Title](/blog/<slug>)` links.

The Indonesian twin is a **real translation**: the same section order,
translated headings, `saya` pronouns, Indonesian code comments.

## What to check before publishing

- No secret value in the body. The token file is on the host, outside the
  repository.
- No host path, tunnel address, tenant or product identifier in the text
  unless Ardi explicitly approves that disclosure. A weakness already
  documented in the project's own public README or docs is not a new
  disclosure.
- The numbers are the real ones. A figure in a note is a claim about the
  repository, and the repository has a validator for exactly that kind of
  drift — run it and quote what it says.

## Related

- `docs/rules/gaos-design-review.md` — the design gate's policy, and the
  workflow this note came out of.
- `docs/designs/egm-design-review.md` — the design this note describes, with
  its two review rounds in `docs/designs/reviews/`.
