# deploy/local — one operator's instance material

Everything in this directory except this README is **git-ignored** (`.gitignore`,
D-085). It holds what belongs to one deployment of GAOS rather than to GAOS:
a private product seed, a second instance's edge blocks and redeploy
overrides, and the list of strings a tracked file must never contain. GAOS is
open source; none of this may be committed.

```
deploy/local/
  README.md                 tracked: this file
  forbidden-strings         one extended regex per line, case-insensitive; # comments
  seed/<label>/             a private product seed: product.json, cases.json (+ its own generator)
  edge/gaos-<label>.conf    a second instance's nginx blocks, made from deploy/edge/instance.conf.example
  instances/<label>.env     KEY=value overrides for a second instance's redeploy line
  private/                  optional: a clone of a private repository holding the above
```

## Rules

- **Neutral names.** `<label>` is a label you choose, such as `instance-2`. Never
  name a directory or file after a private product, customer or domain: a
  directory listing, `git status --ignored` or a script's error line would then
  print it.
- **Agents may not read it.** `.claude/settings.json` denies `Read` of
  `deploy/local/**` in every checkout, and `.claude/rules/agent-workspace.md`
  puts it on the reviewer deny-list.
- **History and backup.** This directory has neither. To keep them, clone a
  private repository into `deploy/local/private/` (the directory itself cannot be
  a clone target, because this README makes it non-empty).

## The forbidden-strings list

`deploy/check-workspace.sh` (section 7) reads the first non-empty list of:
`$GAOS_FORBIDDEN_STRINGS`, `deploy/local/forbidden-strings`, and the same path in
the main checkout (so an agent worktree, which never receives ignored files,
still finds it). It fails when a tracked or untracked-not-ignored file's content
or path matches a pattern, and prints the path and line, never the text. Without
a list it prints a skip line. Keep the file mode 600.

## A second instance

```bash
# the redeploy line, with the overrides from instances/<label>.env
set -a; . deploy/local/instances/<label>.env; set +a
./deploy/rebuild-restart.sh prod deploy --tag vX.Y.Z

# a private product seed
deploy/seed/seed.sh --dir deploy/local/seed/<label> --base http://<tunnel-ip>:18301/api/v1
```

The edge file is described in `deploy/edge/README.md`, *A second instance*.
