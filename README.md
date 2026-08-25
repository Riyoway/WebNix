# WebNix

WebNix is a self-hostable web terminal for persistent, reproducible NixOS development workspaces.

The browser does **not** emulate NixOS. Each user receives an isolated x86_64 NixOS virtual machine managed by Incus. The browser only renders a terminal and transports terminal bytes over an authenticated WebSocket connection.

## Architecture

```text
Browser / PWA
    |
    +-- static UI --> Vercel Hobby
    |
    +-- WSS terminal --> Cloudflare Tunnel
                          |
                          v
                        Caddy
    |
    v
Caddy forward_auth -> OAuth2 Proxy (OIDC)
    |
    v
Gateway (unprivileged, loopback only)
    |
    | Unix-domain WebSocket
    v
Orchestrator (Incus control, local socket only)
    |
    v
Incus VM per identity
    |
    +-- NixOS x86_64
    +-- persistent root disk
    +-- flakes + direnv
    +-- no inbound network services
```

Tailscale is intentionally **not** required for end users. It is recommended only for private host administration. This avoids placing browsers into an operator-owned tailnet and avoids exposing auth keys to the browser.

## Security model

- One full virtual machine per user identity. No shared shell accounts or shared kernels between tenants.
- Incus project per tenant with one-VM, CPU, memory, and disk limits.
- Incus project restrictions block low-level VM configuration, device passthrough, nesting, backups, and snapshots from tenant context.
- The public gateway never receives access to the Incus Unix socket.
- The privileged orchestrator is not network-exposed; it accepts requests only through a local Unix socket.
- Guest SSH is disabled. Interactive shells are created through the Incus agent/exec path, so the VM needs no management port.
- A NIC-level Incus ACL blocks private/link-local/Tailscale address ranges while allowing public Internet egress. IPv6 is disabled in the default workspace network to avoid policy bypasses.
- OIDC sessions are handled by OAuth2 Proxy through Caddy `forward_auth`. Authenticated application traffic does not traverse OAuth2 Proxy, and the frontend never stores OAuth tokens.
- WebSocket Origin is checked by the gateway.
- Caddy applies CSP, HSTS, anti-framing, MIME sniffing, referrer and permissions policies.
- The Incus API should remain bound to its Unix socket. Do not expose port 8443 publicly.

## Current production baseline

Use **Incus 7.3.0 or newer** and **OAuth2 Proxy 7.15.3 or newer**. Both projects shipped important security fixes in 2026. For an Internet-facing multi-tenant service, track security releases instead of leaving either control-plane component pinned indefinitely.

## Repository layout

```text
frontend/       Vite + TypeScript + xterm.js PWA
gateway/        Unprivileged HTTP/WebSocket frontend gateway
orchestrator/   Local-only Incus workspace lifecycle + PTY broker
nix/            NixOS host module and NixOS workspace image
infra/          Caddy, OAuth2 Proxy and systemd examples
scripts/        Host bootstrap and base-image import helpers
docs/           Architecture and deployment notes
```

## Free/Vercel deployment

For the zero-cost profile, deploy only `frontend/` to Vercel Hobby and keep the persistent terminal backend on an x86_64 KVM host you control. Publish that backend with Cloudflare Tunnel. Vercel Functions are intentionally not used. See `docs/FREE_DEPLOYMENT.md`.

Set `VITE_TERMINAL_ORIGIN=https://term.example.com` in Vercel and `WEBNIX_PUBLIC_ORIGIN=https://app.example.com` on the gateway. Use sibling subdomains under the same registrable domain.

## Build

Build all user-space components with:

```bash
make
```

Run the test/static-check suite with:

```bash
make check
```

GitHub Actions runs the same frontend build, Go tests/vet/build, formatting and script syntax checks on pushes and pull requests. Dependabot is configured for npm, Go modules and GitHub Actions.

Or build them individually:

### Frontend

```bash
cd frontend
npm install
npm run build
```

### Gateway

```bash
cd gateway
go build -trimpath -ldflags='-s -w' -o webnix-gateway .
```

### Orchestrator

```bash
cd orchestrator
go build -trimpath -ldflags='-s -w' -o webnix-orchestrator .
```

## NixOS workspace image

The infrastructure flake pins nixpkgs to a tested `nixos-unstable` commit. Build the image on a Nix machine:

```bash
nix build ./nix#nixosConfigurations.workspace.config.system.build.qemuImage
nix build ./nix#nixosConfigurations.workspace.config.system.build.metadata
```

Then import it into Incus with:

```bash
./scripts/import-base-image.sh
```

The imported alias is `webnix-base`.

## Host preparation

1. Install/configure Incus >= 7.3.0.
2. Create a storage pool named `default` (ZFS or Btrfs recommended).
3. Run `scripts/setup-incus.sh` as an Incus administrator.
4. Import `webnix-base`.
5. Build the frontend and both Go services.
6. Run `scripts/install-services.sh` (or install the systemd units manually).
7. Configure OAuth2 Proxy with your OIDC provider and an explicit allow-list/domain policy.
8. Configure Caddy for your domain.
9. Keep host administration on Tailscale or another private management network.

## Environment

### Gateway

- `WEBNIX_LISTEN` default `127.0.0.1:8080`
- `WEBNIX_ORCHESTRATOR_SOCKET` default `/run/webnix/orchestrator.sock`
- `WEBNIX_STATIC_DIR` default `./frontend/dist`
- `WEBNIX_IDENTITY_HEADER` default `X-Auth-Request-Email`; use a provider-stable authenticated identifier if email changes must preserve the same workspace
- `WEBNIX_PUBLIC_ORIGIN` required in production, e.g. `https://nix.example.com`

### Orchestrator

- `WEBNIX_SOCKET` default `/run/webnix/orchestrator.sock`
- `WEBNIX_VM_IMAGE` default `webnix-base`
- `WEBNIX_PROFILE` default `webnix`
- `WEBNIX_NETWORK` default `webnix0`
- `WEBNIX_STORAGE_POOL` default `default`
- `WEBNIX_BANDWIDTH` default `100Mbit` is consumed by `scripts/setup-incus.sh` when creating the managed NIC profile
- `WEBNIX_CPU` default `2`
- `WEBNIX_MEMORY` default `4GiB`
- `WEBNIX_DISK` default `30GiB`
- `WEBNIX_IDLE_TIMEOUT` default `20m`
- `WEBNIX_MAX_SESSIONS` default `4` per tenant
- `WEBNIX_MAX_TENANTS` default `16` persistent tenant workspaces
- `WEBNIX_MAX_RUNNING_WORKSPACES` default `3`; set this from actual host RAM/CPU capacity

## Workspace behavior

Each authenticated principal maps to a deterministic opaque tenant ID. The user never controls Incus project or instance names. A workspace is started on first terminal connection and stopped after the configured idle timeout when all terminal sessions are closed. Its VM disk remains intact.

Inside the VM:

```bash
nix develop
```

works normally. `direnv` + `nix-direnv` are preconfigured. Users are allowed passwordless sudo **inside their own VM**, which does not grant host privileges.

## Scope of v1

Included:

- terminal access
- persistent NixOS VM
- flakes/devShell support
- mobile terminal toolbar
- PWA shell
- automatic idle stop
- tenant resource limits
- global tenant/running-workspace admission limits
- network egress isolation

Deliberately excluded from v1:

- arbitrary inbound port forwarding
- Docker socket passthrough
- host filesystem mounts
- GPU/USB/PCI passthrough
- nested virtualization
- shared `/nix/store` between tenants
- browser-side Tailscale auth keys

Those exclusions are intentional security boundaries.

## Public-service warning

WebNix gives authenticated users a real root-capable Linux VM with public Internet egress. The default configuration is suitable for an invite-only/team service, not anonymous open registration. Before opening registration broadly, add abuse handling, account admission controls, monitoring, per-account quotas, egress policy appropriate to your product, and a process for suspending malicious tenants. The default ACL blocks private/link-local/CGNAT ranges and common SMTP/SMB abuse ports, but it is not a general-purpose Internet abuse prevention system.
