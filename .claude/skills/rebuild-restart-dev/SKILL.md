---
name: rebuild-restart-dev
description: "Rebuild and restart the gaos dev stack (dev.gaos.mfardiansyah.id, port 19301) -- the whole stack or one named service -- or restart without rebuilding, or just report status. Use for: deploying the latest gaos code to dev, restarting a stuck dev service, checking dev health."
trigger: /rebuild-restart-dev
---

# /rebuild-restart-dev

Deploys the gaos **dev** tier: `traefik`, `landing`, `dashboard`, `docs`,
behind the tunnel at `10.10.0.2:19301` / `dev.gaos.mfardiansyah.id`. Dev is
never blocked by a dirty working tree (unlike prod) -- it's meant to move
fast.

## Usage

| Invocation | Action |
|---|---|
| `/rebuild-restart-dev` | Full rebuild + restart, all 4 services |
| `/rebuild-restart-dev <service>` | Rebuild + restart just that service |
| `/rebuild-restart-dev restart` | Restart-only, all services, no rebuild |
| `/rebuild-restart-dev restart <service>` | Restart-only, just that service |
| `/rebuild-restart-dev status` | Health/status report only, no changes |

`<service>` is one of `traefik`, `landing`, `dashboard`, `docs`.

## What to do

Parse the argument per the table above into `action` (`deploy`, `restart`, or
`status`) and an optional `service`, then invoke the `gaos-deploy` subagent
(Agent tool, `subagent_type: gaos-deploy`, run in the **foreground** since the
result is needed immediately) with a prompt naming: environment = `dev`,
action, and service (or "whole stack" if none given). Give it the context of
why this is being run if the user said anything beyond the bare command.

Relay the agent's report back to the user as-is. If it reports a failure
(check-stacks.sh, tests, or verification), don't retry automatically --
surface what failed and let the user decide next steps.
