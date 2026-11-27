# The edge — putting GAOS on clino

`clino` is the **only public machine** (its one public IP is whatever the DNS records below resolve to). It runs nginx and
certbot and nothing else: 1 vCPU, 957 MiB of RAM, **no Docker**. Every container
runs on the private VM (`vm-rnd-ardi`, WireGuard `10.10.0.2`), reachable only
through the tunnel.

These steps need `sudo` on clino and they touch a file that every other product
on that box depends on. **Run them yourself, in order, and do not skip the
`nginx -t`.**

```
browser --HTTPS--> clino (public IP)   nginx 1.30 (nginx.org stable) terminates TLS
       --plain HTTP over WireGuard--> 10.10.0.2:1<env>301 --> per-product Traefik :80
       --> landing / dashboard / docs      (path-routed, one origin)
```

| | dev | prod |
|---|---|---|
| Hostname | `dev.gaos.mfardiansyah.id` | `gaos.mfardiansyah.id` |
| Tunnel port | `10.10.0.2:19301` | `10.10.0.2:18301` |
| Certificate lineage | `gaos-dev` | `gaos` |

---

## 0. Before you start

**DNS is already done.** Both names resolve to the edge's public IP. Confirm anyway —
everything below assumes it, and certbot's failure message if it is wrong is not
especially clear:

```bash
dig +short gaos.mfardiansyah.id dev.gaos.mfardiansyah.id
# both must print the same address: the edge's public IP
```

**The stack must be up on the private VM first.** Issuing a certificate for a
hostname that proxies to nothing gives you a working TLS handshake in front of a
502, which is a confusing place to start debugging. Verify over the tunnel:

```bash
ssh clino "curl -sS -H 'Host: dev.gaos.mfardiansyah.id' http://10.10.0.2:19301/api/healthz"
ssh clino "curl -sS -H 'Host: gaos.mfardiansyah.id'     http://10.10.0.2:18301/api/healthz"
```

---

## 1. Install the `:80` block, without the `:443` blocks

**Order matters and it is circular:** nginx will not open `:443` without a
certificate on disk, and certbot cannot get a certificate without nginx already
answering `:80`. So the `:80` server block goes in first, alone.

```bash
scp deploy/edge/gaos.conf clino:/tmp/gaos.conf

ssh clino
sudo cp /tmp/gaos.conf /etc/nginx/conf.d/gaos.conf

# Comment out BOTH `server { listen 443 ... }` blocks for now. Leave the :80
# block and both `map` directives in place.
sudo nano /etc/nginx/conf.d/gaos.conf

sudo nginx -t          # MUST pass before the next line. No exceptions.
sudo systemctl reload nginx
```

If `nginx -t` complains about a **duplicate map variable**, stop. It means a
`$gaos_*` name collided with something another product defined — every variable
in this file is deliberately prefixed for exactly that reason, and a collision
means somebody edited it. Fix the name; do not force the reload. A failed reload
here takes `clino.id`, `cubiq.mfardiansyah.id` and GAOS down together.

Check the ACME path answers locally rather than being proxied:

```bash
echo ok | sudo tee /var/www/acme/.well-known/acme-challenge/probe >/dev/null
curl -sS http://gaos.mfardiansyah.id/.well-known/acme-challenge/probe   # -> ok
sudo rm /var/www/acme/.well-known/acme-challenge/probe
```

---

## 2. Issue the two certificates

**Two lineages, not one certificate with both names.** HTTP-01 validation is
all-or-nothing: if one hostname stops resolving, the renewal of every name on
that certificate fails. Splitting them means a broken dev record cannot take
production's certificate down with it. This mirrors the existing `clino` /
`clino-dev` split on the same box.

```bash
sudo certbot certonly --webroot -w /var/www/acme --cert-name gaos \
  -d gaos.mfardiansyah.id \
  --deploy-hook 'nginx -t && systemctl reload nginx'

sudo certbot certonly --webroot -w /var/www/acme --cert-name gaos-dev \
  -d dev.gaos.mfardiansyah.id \
  --deploy-hook 'nginx -t && systemctl reload nginx'

sudo ls /etc/letsencrypt/live/gaos /etc/letsencrypt/live/gaos-dev
```

The deploy hook is not optional. Without it a renewed certificate sits on disk
while nginx keeps serving the expired one from memory — and the failure arrives
ninety days later, with no recent change to blame it on.

---

## 3. Enable the `:443` blocks

```bash
ssh clino
sudo nano /etc/nginx/conf.d/gaos.conf     # uncomment both :443 server blocks
sudo nginx -t
sudo systemctl reload nginx
```

---

## 4. Verify, from anywhere

```bash
curl -sSI https://gaos.mfardiansyah.id/     | head -1     # 200
curl -sSI https://dev.gaos.mfardiansyah.id/ | head -1     # 200
curl -sS  https://gaos.mfardiansyah.id/api/healthz         # {"ok":true,...}

# /app and /docs must REDIRECT to their trailing-slash form. This is not
# cosmetic: without it the dashboard's <base href> resolves one level up and
# every asset 404s against the landing page, with a 200 on the document itself.
curl -sSI https://gaos.mfardiansyah.id/app  | head -1      # 301
curl -sSI https://gaos.mfardiansyah.id/docs | head -1      # 301

# An unknown Host must be closed without a reply.
EDGE_IP=$(dig +short A gaos.mfardiansyah.id | grep -E '^[0-9.]+$' | head -1)
curl -sS -k --resolve "nope.mfardiansyah.id:443:$EDGE_IP" \
  https://nope.mfardiansyah.id/ ; echo "exit=$?"           # empty reply

# Certificate lineages, separate as intended.
for h in gaos.mfardiansyah.id dev.gaos.mfardiansyah.id; do
  echo | openssl s_client -connect $h:443 -servername $h 2>/dev/null \
    | openssl x509 -noout -dates -ext subjectAltName
done
```

**The real client IP — do not skip this one.** Traefik on the private side
believes `X-Forwarded-For` only from a peer in its `trustedIPs`. From anyone
else it discards the header and substitutes the peer's own address, so a wrong
trust list collapses every caller on earth into one value while everything looks
completely healthy.

```bash
curl -sS -H 'X-Forwarded-For: 203.0.113.9' https://gaos.mfardiansyah.id/api/healthz >/dev/null
# then, on the private VM:
docker compose -f deploy/stacks/compose.prod.yaml logs --tail 1 traefik
```

The first field of the access-log line is `ClientHost`. It **must** read
`203.0.113.9`. If it shows `10.10.0.1`, the header was discarded and
`deploy/stacks/traefik.yml`'s trust list is wrong. Since v0.68.0 that list is
this box alone, `10.10.0.1/32` (D-106): a request that reaches a VM's Traefik
from any other tunnel peer is logged with that peer's own address, and so is
any `X-Real-IP` it sends. This box also limits the two Basic machine routes
(`/api/v1/products/*/staging` and `*/unit-results`) to 1 r/s per client, burst
10 (`gaos_machine_basic` in `gaos.conf`; a second instance's file uses the same
zone). `nginx -t` before the reload.

---

## 5. The host firewall on the tunnel port (D-106) — recommended hardening (D-107)

Since v0.69.0 the required step is reviewing the back office's **Traffic** page
**at least daily** (within the shortest "kept since" of its unknown-direct
stream). It records every request that reaches GAOS's dashboard without passing
through this box, every machine-API call, and every direct source since the
dashboard started. It is best-effort detection, not a control: it drops records
under load, keeps each kind of call only up to its cap and at most 48 hours,
loses its direct-source summary when the dashboard restarts, and does not see
requests Traefik serves without the dashboard. **An empty direct list is not
proof that no direct caller exists.** Until the rules below are applied, a
direct peer can reach every route on the port; applying them remains
recommended.

**Not part of any automated deploy; host state outside the repository.** Since
v0.68.0 Traefik trusts forwarded headers (`X-Forwarded-For`, `X-Real-IP`) from
the edge alone, `10.10.0.1/32` (`deploy/stacks/traefik.yml`; `check-stacks.sh`
asserts the single entry). The two Basic machine routes (the staging callback
and the unit results) key their failure lock on that address, so no tunnel
peer can pick another caller's bucket. Traefik is still *reachable* from every
WireGuard peer on the VM's published port, `18301`; a peer that is not the edge
only gets its own address as its lock source, so it can lock out nobody but
itself. To close the port to everyone but the edge and the known direct
callers, apply this on **each GAOS VM** (`10.10.0.2`, and a second instance's
VM at its own tunnel address):

```bash
# 1. Sample who calls the published port directly, over a day; every legitimate
#    caller that is not on this VM and not the edge gets a RETURN line below.
#    `conntrack -L` lists only the current table; for a period, log new connections
#    to a file and stop it at the end of the period:
sudo conntrack -E -e NEW -p tcp --orig-port-dst 18301 -o timestamp > ~/gaos-conntrack-<date>.log
#    Every peer in the log other than the edge should be on the Traffic page's
#    direct-source summary (design docs/designs/traffic-monitor.md §0).

# 2. Docker rewrites the destination before DOCKER-USER, so match the ORIGINAL one.
#    <vm-tunnel-ip> is this VM's WireGuard address (10.10.0.2 for the first instance).
sudo iptables -I DOCKER-USER -i wg0 -p tcp -m conntrack --ctorigdst <vm-tunnel-ip> \
  --ctorigdstport 18301 -s 10.10.0.1 -j RETURN
# ... one RETURN line per known direct caller on another tunnel host ...
sudo iptables -A DOCKER-USER -i wg0 -p tcp -m conntrack --ctorigdst <vm-tunnel-ip> \
  --ctorigdstport 18301 -j DROP
```

Traffic the VM sends to its own published port never crosses `DOCKER-USER`, so
the VM itself needs no line; a caller on the same VM (the seed script, a QA
backend that calls `http://<vm-tunnel-ip>:18301/api/v1`) is unaffected.

**When the rules are applied, the check is mandatory and recorded** (write the date and the two results in
the deploy note): from a tunnel peer that is not listed,
`curl -m 5 http://<vm-tunnel-ip>:18301/api/healthz` **times out**; and the edge
still answers, `ssh clino "curl -sS -H 'Host: <host>' http://<vm-tunnel-ip>:18301/api/healthz"`
reads `ok`. Re-apply the rules when a VM is rebuilt (they are not persisted by
the repository), and make them survive a reboot with the host's usual
mechanism (`iptables-persistent` or equivalent).

## A second instance

Another GAOS instance (its own VM, its own database, its own host name) is
served by the same edge from **its own file**, never from `gaos.conf`, whose
tracked copy carries only the two instances above (D-085). Its host name and
tunnel address are one operator's material and stay out of the repository.

1. Copy `deploy/edge/instance.conf.example` to `deploy/local/edge/gaos-<label>.conf`
   (git-ignored) and replace every placeholder. Keep the name `gaos-<label>.conf`:
   it sorts after `clino.conf`, which must stay the implicit `:443` default
   (rule 3 in `gaos.conf`), and before `gaos.conf`.
2. The file declares **no `map`** and **no `default_server`**: it reuses
   `$gaos_connection_upgrade` from `gaos.conf`, and refuses any other Host itself.
3. Install and test, in this order:

   ```bash
   scp deploy/local/edge/gaos-<label>.conf clino:/tmp/
   ssh clino
   sudo cp /tmp/gaos-<label>.conf /etc/nginx/conf.d/gaos-<label>.conf
   sudo nginx -t && sudo systemctl reload nginx
   sudo certbot renew --dry-run --cert-name gaos-<label>
   curl -sI --resolve instance.example.org:443:<edge-ip> https://instance.example.org/api/healthz
   ```

4. Only then install a new `gaos.conf` that no longer names the instance, and
   check that nothing collides and the default did not move:

   ```bash
   sudo nginx -t && sudo systemctl reload nginx
   sudo nginx -T 2>&1 | grep -c 'conflicting server name'    # 0
   sudo nginx -T | grep default_server                          # unchanged
   ```

A certificate lineage issued under another name keeps working: point the
`ssl_certificate` lines at it.

---

## Rolling back

```bash
ssh clino
sudo mv /etc/nginx/conf.d/gaos.conf /root/gaos.conf.disabled
sudo nginx -t && sudo systemctl reload nginx
```

Both hostnames then fall through to `clino.conf`'s default `:443` block, which
answers `444`. The certificates stay on disk and keep renewing; nothing else on
the box is affected.

---

## Notes for whoever comes next

- **clino runs no containers and holds no application state.** If you find
  yourself wanting to install Docker there, the answer is the private VM.
- `/etc/nginx/stream-clino.conf.retired-20260805` is the old L4 + PROXY-protocol
  model. **Do not resurrect it.** Traefik downstream is configured for
  `forwardedHeaders`, and a PROXY preamble sent to a proxy that is not expecting
  one is an HTTP 400 on every single request, not a degradation.
- `sites-enabled/` is empty on this box. Everything lives in `conf.d/`, loaded
  alphabetically.
