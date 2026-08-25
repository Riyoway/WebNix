# Zero-cost deployment profile

This profile is designed for a non-commercial deployment using only free plans.

## Topology

```text
app.example.com      Vercel Hobby (static Vite/PWA only)
        |
        | wss://term.example.com/api/terminal
        v
term.example.com     Cloudflare Tunnel (free)
        |
        v
local x86_64 host    Caddy + OAuth2 Proxy + Gateway + Orchestrator + Incus
        |
        +-- NixOS VM per tenant
```

Vercel is deliberately kept out of the terminal data path. Do not proxy long-lived terminal WebSockets through a Vercel Function.

## Why this is the free baseline

- Vercel serves only static assets, so Function usage is zero.
- Cloudflare Tunnel publishes the backend without a public IP or inbound port forwarding and supports WebSockets.
- The only persistent compute is the NixOS/Incus host. Using hardware you already own has no cloud-hosting bill.
- Tailscale can remain optional for administrator access only.

## Domains

Use sibling names under one registrable domain:

- `app.example.com` -> Vercel
- `term.example.com` -> Cloudflare Tunnel

This is preferable to mixing `*.vercel.app` and an unrelated terminal domain because the OAuth session remains first-party/same-site.

Set Vercel environment variable:

```text
VITE_TERMINAL_ORIGIN=https://term.example.com
```

Set the gateway environment variable on the host:

```text
WEBNIX_PUBLIC_ORIGIN=https://app.example.com
```

OAuth2 Proxy's redirect URL belongs to the terminal host:

```text
https://term.example.com/oauth2/callback
```

After authentication, redirect the browser back to `https://app.example.com/`. The session cookie only needs to be valid for `term.example.com`; it is sent on the WebSocket handshake to that host.

## Compute options

### Recommended: hardware you already own

For untrusted multi-user workspaces, use an x86_64 Linux machine with KVM. This preserves the VM isolation model and is the only zero-cloud-cost option that does not weaken the design.

### Oracle Always Free

Oracle currently provides Ampere A1 Always Free capacity equivalent to 2 OCPUs and 12 GB RAM in total, subject to regional capacity. It is ARM and should not be assumed to provide nested KVM. It is suitable for a trusted/single-workspace ARM NixOS deployment, but it is not the recommended host for public untrusted per-user Incus VMs.

### Vercel-only

Not supported. A persistent NixOS VM, writable disk, process tree, and long-running terminal cannot live inside a serverless Function.

## Capacity defaults for a small free host

Start conservatively:

```text
WEBNIX_CPU=1
WEBNIX_MEMORY=2GiB
WEBNIX_DISK=15GiB
WEBNIX_MAX_RUNNING_WORKSPACES=1
WEBNIX_MAX_TENANTS=4
WEBNIX_IDLE_TIMEOUT=10m
```

Increase only after measuring host memory pressure. Persistent stopped workspaces still consume disk.

## Abuse policy

A free public shell service is attractive for abuse. Keep registration invite-only initially. VM isolation protects the host, but it does not prevent tenants from abusing third-party services over your Internet connection. Keep the egress ACLs, quotas, suspension tooling, and global running-workspace limit enabled.
