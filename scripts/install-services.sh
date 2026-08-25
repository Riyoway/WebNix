#!/usr/bin/env bash
set -euo pipefail

if [[ ${EUID:-$(id -u)} -ne 0 ]]; then
  echo "error: run as root" >&2
  exit 1
fi

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GATEWAY_BIN="${GATEWAY_BIN:-$ROOT_DIR/bin/webnix-gateway}"
ORCHESTRATOR_BIN="${ORCHESTRATOR_BIN:-$ROOT_DIR/bin/webnix-orchestrator}"
FRONTEND_DIR="${FRONTEND_DIR:-$ROOT_DIR/frontend/dist}"

for path in "$GATEWAY_BIN" "$ORCHESTRATOR_BIN"; do
  [[ -x "$path" ]] || { echo "error: missing executable $path" >&2; exit 1; }
done
[[ -f "$FRONTEND_DIR/index.html" ]] || { echo "error: frontend build not found at $FRONTEND_DIR" >&2; exit 1; }
getent group incus-admin >/dev/null || { echo "error: incus-admin group does not exist" >&2; exit 1; }

getent group webnix-gateway >/dev/null || groupadd --system webnix-gateway
id webnix-gateway >/dev/null 2>&1 || useradd --system --gid webnix-gateway --home-dir /nonexistent --shell /usr/sbin/nologin webnix-gateway
id webnix-orchestrator >/dev/null 2>&1 || useradd --system --gid webnix-gateway --groups incus-admin --home-dir /nonexistent --shell /usr/sbin/nologin webnix-orchestrator
usermod -a -G incus-admin webnix-orchestrator

install -Dm0755 "$GATEWAY_BIN" /usr/local/bin/webnix-gateway
install -Dm0755 "$ORCHESTRATOR_BIN" /usr/local/bin/webnix-orchestrator
install -d -o root -g webnix-gateway -m0750 /etc/webnix
touch /etc/webnix/denied-tenants
chown root:webnix-gateway /etc/webnix/denied-tenants
chmod 0640 /etc/webnix/denied-tenants
install -d -o root -g webnix-gateway -m0755 /var/lib/webnix/frontend
cp -a "$FRONTEND_DIR"/. /var/lib/webnix/frontend/
chown -R root:webnix-gateway /var/lib/webnix/frontend
find /var/lib/webnix/frontend -type d -exec chmod 0755 {} +
find /var/lib/webnix/frontend -type f -exec chmod 0644 {} +

install -Dm0644 "$ROOT_DIR/infra/systemd/webnix-orchestrator.service" /etc/systemd/system/webnix-orchestrator.service
install -Dm0644 "$ROOT_DIR/infra/systemd/webnix-gateway.service" /etc/systemd/system/webnix-gateway.service

systemctl daemon-reload
systemctl enable webnix-orchestrator.service webnix-gateway.service

echo "Installed WebNix services. Review environment/domain settings, then start them with:"
echo "  systemctl start webnix-orchestrator webnix-gateway"
