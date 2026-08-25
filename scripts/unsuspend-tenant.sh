#!/usr/bin/env bash
set -euo pipefail

if [[ ${EUID:-$(id -u)} -ne 0 ]]; then
  echo "error: run as root" >&2
  exit 1
fi
[[ $# -eq 1 ]] || { echo "usage: $0 <tenant-id>" >&2; exit 2; }
TENANT="$1"
[[ "$TENANT" =~ ^[a-z2-7]{26}$ ]] || { echo "error: invalid tenant ID" >&2; exit 2; }

DENYLIST="${WEBNIX_DENYLIST:-/etc/webnix/denied-tenants}"
[[ -f "$DENYLIST" ]] || { echo "Tenant is not suspended: $TENANT"; exit 0; }

exec 9>"$DENYLIST.lock"
flock 9
TMP="$(mktemp "${DENYLIST}.XXXXXX")"
trap 'rm -f "$TMP"' EXIT
grep -vxF "$TENANT" "$DENYLIST" > "$TMP" || true
cat "$TMP" > "$DENYLIST"
chmod 0640 "$DENYLIST"

echo "Unsuspended tenant: $TENANT"
