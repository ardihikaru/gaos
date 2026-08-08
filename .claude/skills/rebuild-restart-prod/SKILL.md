---
name: rebuild-restart-prod
description: "Rebuild and restart the gaos prod stack (gaos.mfardiansyah.id, port 18301) -- the whole stack or one named service -- or restart without rebuilding, or just report status. Use for: deploying the latest gaos code to prod, restarting a stuck prod service, checking prod health."
trigger: /rebuild-restart-prod
---

# /rebuild-restart-prod

Deploys the gaos **prod** tier: `traefik`, `landing`, `dashboard`, `docs`,
behind the tunnel at `10.10.0.2:18301` / `gaos.mfardiansyah.id` -- the public
site. Prod has no separate git worktree yet (documented gap in
`deploy/STACKS.md`), so it builds from whatever is currently checked out in
this working tree, same as dev. **A dirty working tree blocks a prod
deploy/restart by design** -- this is a deliberate safety gate, not a bug.

## Usage

| Invocation | Action |
|---|---|
| `/rebuild-restart-prod` | Full rebuild + restart, all 4 services |
| `/rebuild-restart-prod <service>` | Rebuild + restart just that service |
| `/rebuild-restart-prod restart` | Restart-only, all services, no rebuild |
| `/rebuild-restart-prod restart <service>` | Restart-only, just that service |
| `/rebuild-restart-prod status` | Health/status report only, no changes |

`<service>` is one of `traefik`, `landing`, `dashboard`, `docs`.

## What to do

Parse the argument per the table above into `action` (`deploy`, `restart`, or
`status`) and an optional `service`, then invoke the `gaos-deploy` subagent
(Agent tool, `subagent_type: gaos-deploy`, run in the **foreground** since the
result is needed immediately) with a prompt naming: environment = `prod`,
action, and service (or "whole stack" if none given).

Relay the agent's report back to the user as-is.

If the agent reports the dirty-tree gate blocked it: **do not** pass
`--allow-dirty` yourself. Show the user the dirty files it reported and ask
whether they want to commit/stash first, or explicitly want you to re-invoke
with `--allow-dirty` -- that override is a one-off, user-authorized exception,
not a default you reach for.
