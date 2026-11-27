#!/bin/sh
# D-107: start the traffic store, or check its health. The password never appears in
# an argument, an environment variable of the server, or `docker inspect`: the ACL file
# in the tmpfs holds only its SHA-256, and redis-cli reads it from REDISCLI_AUTH.
set -eu
secret=/run/secrets/traffic-redis-auth
if [ "${1:-}" = health ]; then
  REDISCLI_AUTH="$(tr -d '\n' < "$secret")" redis-cli --no-auth-warning --user gaos ping | grep -qx PONG
  exit $?
fi
[ -s "$secret" ] || { echo "traffic-redis: $secret is empty or missing" >&2; exit 1; }
umask 077
hash="$(tr -d '\n' < "$secret" | sha256sum | cut -d' ' -f1)"
printf 'user default off\nuser gaos on #%s ~gaos:traffic:* resetchannels -@all +ping +xadd +xtrim +xrevrange\n' "$hash" > /data/users.acl
exec redis-server --save "" --appendonly no --maxmemory 64mb --maxmemory-policy noeviction --aclfile /data/users.acl
