# Architecture decisions

## Why not browser-side NixOS emulation?

A browser emulator is useful for demos and offline sandboxes, but it is the wrong primary runtime for a serious development environment. Native x86_64 NixOS gives the normal nixpkgs binary cache, native compiler performance, full Linux behavior and predictable compatibility with Rust/C/C++/Node/Python tooling.

## Why Incus VMs instead of containers?

Users are expected to run arbitrary code. A container shares the host kernel, so it is not the preferred tenant isolation boundary. Incus VMs use QEMU/KVM and give every tenant a separate kernel while retaining image, project, quota, snapshot and lifecycle management through one mature API.

Firecracker remains a reasonable future optimization for very large fleets, but Incus is the better v1 operational choice because NixOS has first-class Incus VM image support and Incus provides projects, limits and an exec agent out of the box.

## Why no SSH in the guest?

The terminal enters the VM through `incus exec` and the Incus agent. There is no reason to expose an SSH daemon just to operate the web terminal. Removing the management port reduces credentials, key lifecycle, port exposure and guest firewall complexity.

## Why Tailscale only for administration?

For a public multi-user app, adding every browser to an operator tailnet creates identity, key issuance, policy and lifecycle complexity. The user transport already has TLS + OIDC. Tailscale is more valuable as the private host-management and cluster/control-plane network.

## Trust boundaries

1. **Browser** — untrusted terminal input.
2. **Caddy + OIDC/OAuth2 Proxy** — Caddy strips client identity headers and copies the authenticated identity from OAuth2 Proxy `forward_auth`.
3. **Gateway** — Internet-facing but unprivileged and without Incus access.
4. **Orchestrator** — privileged local service; no TCP listener.
5. **Incus** — hypervisor control plane; Unix socket only.
6. **Workspace VM** — untrusted tenant workload.

The gateway derives a fixed-length tenant ID by hashing the authenticated principal. The orchestrator accepts only that strict opaque format and generates every Incus object name itself. User text is never interpolated into a shell command.

## Network model

- `webnix0`: managed IPv4-only NAT bridge; IPv6 is also disabled inside the workspace image.
- NIC-level ACL applied to every workspace.
- Default inbound action: reject.
- Default outbound action: allow.
- Explicit outbound rejects for RFC1918, CGNAT/Tailscale, loopback, link-local and benchmark ranges.
- MAC and IPv4 source filtering enabled.
- No inbound port forwards in v1.

A later preview-port feature should use an authenticated HTTP reverse proxy with explicit per-session grants rather than opening VM ports directly.

## Persistence and reproducibility

The VM disk persists across idle shutdowns. Reproducibility comes from project flakes (`flake.nix` + `flake.lock`) rather than sharing the host's `/nix/store` into tenant VMs. Sharing the store would increase coupling and weaken the isolation model.

For performance, use a copy-on-write Incus storage backend (ZFS or Btrfs) and optionally add a trusted binary cache such as Attic.
