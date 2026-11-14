#!/usr/bin/env bash
# Seed HISv3 into a running GAOS: the product, its BRD, stories, Blueprint and workflow, then the test catalog.
#
#   deploy/seed/hisv3/seed.sh [--base <url>] [--token-file <path>] [--dry-run]
#
# Run it ON THE VM that serves the instance, against the tunnel listener -- not through the public edge, so the
# admin token never crosses the shared edge host (design §4.9, SEC-6). Defaults for the second prod on rnd-v3-vpn:
#   --base        http://${GAOS_TUNNEL_IP:-10.10.0.2}:18301/api/v1
#   --token-file  deploy/stacks/prod.api-admin           (gaos_<hex>, mode 600, gitignored)
#
# The token is read from the file inside python and sent as a header; it is never an argument, never in the
# environment of a child process and never printed. Needs python3 (stdlib only). IDEMPOTENT: a second run changes
# nothing. It ends by asserting the catalog's totals are AT LEAST the committed files' and printing one line:
#   ok   summary matches ...   |   FAIL <what differs>
set -u

here="$(cd "$(dirname "$0")" && pwd)"
root="$(cd "$here/../../.." && pwd)"
base="http://${GAOS_TUNNEL_IP:-10.10.0.2}:18301/api/v1"
tokfile="$root/deploy/stacks/prod.api-admin"
dry=0
while [ $# -gt 0 ]; do
  case "$1" in
    --base) base="$2"; shift 2 ;;
    --token-file) tokfile="$2"; shift 2 ;;
    --dry-run) dry=1; shift ;;
    -h|--help) sed -n '2,13p' "$0"; exit 0 ;;
    *) echo "FAIL unknown argument: $1"; exit 2 ;;
  esac
done
command -v python3 >/dev/null 2>&1 || { echo "FAIL python3 is required"; exit 2; }
[ "$dry" = 1 ] || [ -r "$tokfile" ] || { echo "FAIL cannot read the token file $tokfile"; exit 2; }

exec python3 - "$base" "$tokfile" "$here" "$dry" <<'PY'
import json, sys, urllib.request, urllib.error

base, tokfile, here, dry = sys.argv[1].rstrip('/'), sys.argv[2], sys.argv[3], sys.argv[4] == '1'
import ipaddress, urllib.parse

# The bearer is sent to --base and only there: https, or http to a loopback or private (tunnel) address, and never
# through a redirect (urllib would forward the header to the new host).
u = urllib.parse.urlparse(base)
host_ok = u.scheme == 'https'
if u.scheme == 'http' and u.hostname:
    try:
        ip = ipaddress.ip_address(u.hostname)
        host_ok = ip.is_loopback or ip.is_private
    except ValueError:
        host_ok = False
if not host_ok or u.username or u.password:
    print('FAIL --base must be https, or http to a loopback or private address, without credentials')
    sys.exit(2)

class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *a, **k):
        return None
opener = urllib.request.build_opener(NoRedirect)

token = ''
if not dry:
    token = open(tokfile).read().strip()
    if not token.startswith('gaos_') or len(token) < 37:
        print('FAIL the token file does not hold a gaos_ token (an empty file disables the admin token)')
        sys.exit(2)

def call(method, path, body=None):
    data = None if body is None else json.dumps(body).encode()
    req = urllib.request.Request(base + path, data=data, method=method, headers={
        'Authorization': 'Bearer ' + token, 'Content-Type': 'application/json'})
    try:
        with opener.open(req, timeout=120) as r:
            raw, status = r.read(), r.status
    except urllib.error.HTTPError as e:
        raw, status = e.read(), e.code
    except OSError as e:
        print('FAIL cannot reach %s: %s' % (base, e.__class__.__name__))
        sys.exit(1)
    try:
        return status, json.loads(raw or b'{}')
    except ValueError:
        print('FAIL the answer to %s %s (HTTP %d) is not JSON; is --base the API?' % (method, path, status))
        sys.exit(1)

product = json.load(open(here + '/product.json'))
cases = json.load(open(here + '/cases.json'))['cases']
want_epics = len(product['epics'])
want_cases = len(cases)
want_auto = sum(1 for c in cases if c['implemented'] and c['decision'] == 'planned')
print('seed: %d modules, %d epics, %d cases (%d automated)' % (len(product['modules']), want_epics, want_cases, want_auto))
if dry:
    print('ok   dry run: nothing sent')
    sys.exit(0)

st, out = call('POST', '/seed/product', product)
if st != 200:
    print('FAIL the product seed answered %d: %s %s' % (st, out.get('error'), out.get('reason', '')))
    sys.exit(1)
pid = out['productId']
print('ok   product %s %s: %d stories added, %d policies added, Blueprint v%d, %d tasks, readiness %d (%s)' % (
    pid, out['product'], out['stories'], out['policies'], out['blueprintVersion'], out['tasks'], out['readinessScore'], out['readinessBand']))

created = updated = unchanged = 0
for i in range(0, want_cases, 200):
    st, o = call('POST', '/products/%s/qa/catalog' % pid, {'seed': True, 'cases': cases[i:i + 200]})
    if st != 200:
        print('FAIL catalog slice at %d answered %d: %s %s' % (i, st, o.get('error'), o.get('reason', '')))
        sys.exit(1)
    if o.get('rejected'):
        print('FAIL catalog slice at %d rejected %d case(s), first %s' % (i, len(o['rejected']), o['rejected'][0]))
        sys.exit(1)
    created += o['created']; updated += o['updated']; unchanged += o['unchanged']
print('ok   catalog: %d created, %d updated, %d unchanged' % (created, updated, unchanged))

st, s = call('GET', '/products/%s/qa/summary' % pid)
if st != 200:
    print('FAIL the summary answered %d' % st)
    sys.exit(1)
got = (s['totals']['cases'], s['totals']['automated'], len(s['epics']))
exp = (want_cases, want_auto, want_epics)
# AT LEAST the seed's: after the QA tools create cases, add epics and report scripts, the totals only grow,
# and a healthy re-run must not print FAIL (design section 4.9).
if any(g < e for g, e in zip(got, exp)):
    print('FAIL summary is below the seed: cases/automated/epics = %s, want at least %s' % (got, exp))
    sys.exit(1)
print('ok   summary matches: %d cases, %d automated, %d epics (at least the seed\'s %d, %d, %d), coverage %d%%' % (got[0], got[1], got[2], exp[0], exp[1], exp[2], s['totals']['coveragePct']))
print('next: set GAOS_API_QA_PRODUCTS=%s for the QA token, then restart the dashboard' % pid)
PY
