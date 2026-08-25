#!/usr/bin/env bash
set -euo pipefail

require() { command -v "$1" >/dev/null || { echo "missing required command: $1" >&2; exit 1; }; }
require incus

if [[ ! -c /dev/kvm ]]; then
  echo "error: /dev/kvm is unavailable; WebNix requires hardware virtualization for production workspaces" >&2
  exit 1
fi

NETWORK="${WEBNIX_NETWORK:-webnix0}"
PROFILE="${WEBNIX_PROFILE:-webnix}"
POOL="${WEBNIX_STORAGE_POOL:-default}"
BANDWIDTH="${WEBNIX_BANDWIDTH:-100Mbit}"

version="$(incus version | head -n1 | grep -oE '[0-9]+(\.[0-9]+){2}' | head -n1)"
min_version="7.3.0"
if [[ -z "$version" ]]; then
  echo "error: could not determine Incus version" >&2
  exit 1
fi
printf 'Incus version: %s\n' "$version"
if [[ "$(printf '%s\n%s\n' "$min_version" "$version" | sort -V | head -n1)" != "$min_version" ]]; then
  echo "error: Incus $min_version or newer is required" >&2
  exit 1
fi

if ! incus storage show "$POOL" >/dev/null 2>&1; then
  echo "error: Incus storage pool '$POOL' is missing. Initialize Incus first." >&2
  exit 1
fi

storage_driver="$(incus storage list -c nD --format csv,noheader | awk -F, -v p="$POOL" '$1 == p { print $2; exit }')"
case "$storage_driver" in
  zfs|btrfs) ;;
  *) echo "warning: storage pool '$POOL' uses '$storage_driver'; ZFS or Btrfs is recommended for fast copy-on-write workspace clones" >&2 ;;
esac

if ! incus network show "$NETWORK" >/dev/null 2>&1; then
  incus network create "$NETWORK" \
    ipv4.address=10.231.0.1/16 \
    ipv4.nat=true \
    ipv6.address=none
fi

if ! incus network acl show webnix-isolated >/dev/null 2>&1; then
  incus network acl create webnix-isolated
  # Deny private, loopback, link-local, CGNAT/Tailscale and benchmark ranges.
  # Incus bridge baseline DHCP/DNS rules are evaluated separately.
  incus network acl rule add webnix-isolated egress action=reject destination=0.0.0.0/8
  incus network acl rule add webnix-isolated egress action=reject destination=10.0.0.0/8
  incus network acl rule add webnix-isolated egress action=reject destination=100.64.0.0/10
  incus network acl rule add webnix-isolated egress action=reject destination=127.0.0.0/8
  incus network acl rule add webnix-isolated egress action=reject destination=169.254.0.0/16
  incus network acl rule add webnix-isolated egress action=reject destination=172.16.0.0/12
  incus network acl rule add webnix-isolated egress action=reject destination=192.0.0.0/24
  incus network acl rule add webnix-isolated egress action=reject destination=192.168.0.0/16
  incus network acl rule add webnix-isolated egress action=reject destination=198.18.0.0/15
  incus network acl rule add webnix-isolated egress action=reject destination=224.0.0.0/4
  incus network acl rule add webnix-isolated egress action=reject destination=240.0.0.0/4
  incus network acl rule add webnix-isolated egress action=reject protocol=tcp destination_port=25
  incus network acl rule add webnix-isolated egress action=reject protocol=tcp destination_port=465
  incus network acl rule add webnix-isolated egress action=reject protocol=tcp destination_port=587
  incus network acl rule add webnix-isolated egress action=reject protocol=tcp destination_port=445
fi

if ! incus profile show "$PROFILE" >/dev/null 2>&1; then
  incus profile create "$PROFILE"
fi

# Idempotently recreate the profile devices we own.
incus profile device remove "$PROFILE" root >/dev/null 2>&1 || true
incus profile device remove "$PROFILE" eth0 >/dev/null 2>&1 || true
incus profile device add "$PROFILE" root disk path=/ pool="$POOL" size=30GiB
incus profile device add "$PROFILE" eth0 nic network="$NETWORK" name=eth0 \
  security.acls=webnix-isolated \
  security.acls.default.ingress.action=reject \
  security.acls.default.egress.action=allow \
  security.mac_filtering=true \
  security.ipv4_filtering=true \
  security.ipv6_filtering=true \
  limits.max="$BANDWIDTH"

incus profile set "$PROFILE" limits.cpu=2 limits.memory=4GiB security.nesting=false security.secureboot=false

echo "Incus WebNix network/profile configured."
