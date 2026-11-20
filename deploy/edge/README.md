# The edge — putting GAOS on clino

`clino` (103.127.134.25) is the **only public machine**. It runs nginx and
certbot and nothing else: 1 vCPU, 957 MiB of RAM, **no Docker**. Every container
runs on the private VM (`vm-rnd-ardi`, WireGuard `10.10.0.2`), reachable only
through the tunnel.

These steps need `sudo` on clino and they touch a file that every other product
on that box depends on. **Run them yourself, in order, and do not skip the
`nginx -t`.**

```
browser --HTTPS--> clino 103.127.134.25   nginx 1.30 (nginx.org stable) terminates TLS
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

**DNS is already done.** Both names resolve to `103.127.134.25`. Confirm anyway —
everything below assumes it, and certbot's failure message if it is wrong is not
especially clear:

```bash
dig +short gaos.mfardiansyah.id dev.gaos.mfardiansyah.id
# both must print 103.127.134.25
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
curl -sS -k --resolve nope.mfardiansyah.id:443:103.127.134.25 \
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
`deploy/stacks/traefik.yml`'s trust list is wrong.

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
