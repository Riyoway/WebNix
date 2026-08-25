> Using Vercel Hobby / free infrastructure? Start with [`FREE_DEPLOYMENT.md`](./FREE_DEPLOYMENT.md).

# Deployment notes

## Minimum host

- x86_64 Linux with KVM
- 4+ CPU cores recommended
- 16 GiB RAM is a practical minimum for the conservative default of three 4 GiB running workspaces; size `WEBNIX_MAX_RUNNING_WORKSPACES` to leave memory for the host/QEMU page cache
- Incus >= 7.3.0
- Caddy
- OAuth2 Proxy >= 7.15.3
- Tailscale for host administration (recommended, not required for users)

## Storage

Prefer ZFS when you control a dedicated disk and want fast copy-on-write VM creation/snapshots. Btrfs is a reasonable simpler single-host option. Avoid a loop-backed storage pool for serious production use.

## Public ports

Expose only TCP 80/443 for Caddy. Keep the Incus API, gateway, orchestrator and host SSH off the public interface.

## Updates

Because this service intentionally runs untrusted code, host kernel/QEMU/Incus security updates are operationally important. Track Incus security releases and rebuild the NixOS host regularly.

## Workspace image updates

Build and import a new `webnix-base`, move the alias, then let newly-created workspaces use the new image. Existing persistent VMs are not silently replaced. Users can update their system normally through NixOS if that capability is exposed in your product policy.

## Authentication boundary

The sample enables OIDC nonce verification, PKCE S256, a `__Host-` session cookie, and OAuth2 Proxy's trusted-proxy allow-list. Keep OAuth2 Proxy bound to loopback.


Caddy should use OAuth2 Proxy only through `forward_auth`. Before authentication, Caddy removes any client-supplied `X-Auth-Request-Email` header and then copies the authenticated value from OAuth2 Proxy's `/oauth2/auth` response. The gateway binds to loopback and must never be published directly.

If your identity provider exposes a stable immutable user identifier through OAuth2 Proxy, prefer that over email for `WEBNIX_IDENTITY_HEADER`. Changing the identity key intentionally creates a different workspace mapping.

The sample OAuth2 Proxy environment deliberately uses `OAUTH2_PROXY_EMAIL_DOMAINS=example.com`. Replace it with an explicit domain or authenticated-emails allow-list. Set `*` only when open registration for every account accepted by your provider is intentional.

## Service installation

After `make`, `scripts/install-services.sh` creates separate unprivileged service accounts, grants Incus administration only to the local orchestrator account, installs the binaries/frontend, and installs the systemd units. Review the example domain and environment values before starting the services.

## Capacity controls

The orchestrator enforces both `WEBNIX_MAX_TENANTS` and `WEBNIX_MAX_RUNNING_WORKSPACES`. New instances are created with `incus init` and are started only after the current running-workspace count is checked, avoiding a race where `incus launch` would start a VM before admission control. Per-tenant project limits remain a second layer of resource control.

## Registration and abuse controls

Treat the default deployment as invite-only. A workspace user has passwordless sudo inside a hardware-virtualized VM and can make arbitrary public Internet connections except for the explicit ACL denies. Open registration therefore needs product-level admission, monitoring, quotas, suspension, and an egress-abuse strategy. Do not confuse tenant isolation from the host with protection of third-party Internet services from a malicious tenant.
