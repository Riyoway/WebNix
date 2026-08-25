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
PROJECT="wn-$TENANT"
install -d -m0750 -o root -g webnix-gateway "$(dirname "$DENYLIST")"
touch "$DENYLIST"
chown root:webnix-gateway "$DENYLIST"
chmod 0640 "$DENYLIST"

exec 9>"$DENYLIST.lock"
flock 9
if ! grep -Fxq "$TENANT" "$DENYLIST"; then
  printf '%s\n' "$TENANT" >> "$DENYLIST"
fi

if incus project show "$PROJECT" >/dev/null 2>&1; then
  incus --project "$PROJECT" stop workspace --timeout 10 --force >/dev/null 2>&1 || true
fi

echo "Suspended tenant: $TENANT"
