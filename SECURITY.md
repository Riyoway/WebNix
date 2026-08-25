# Security policy

WebNix is designed to execute untrusted tenant code, so security fixes to the host kernel, QEMU, Incus, the reverse proxy, the OIDC layer, and the workspace image are part of normal operations.

## Supported deployment model

The supported security boundary is:

- public Caddy on TCP 80/443 only;
- OAuth2 Proxy on loopback and used through Caddy `forward_auth`;
- WebNix gateway on loopback with no Incus permissions;
- WebNix orchestrator on a local Unix socket;
- Incus API not exposed publicly;
- one full Incus VM and one restricted Incus project per tenant;
- no host filesystem, Docker socket, device passthrough, nested virtualization, or shared host Nix store in tenant VMs.

Deployments that directly expose the gateway, orchestrator, Incus API, guest SSH, or host resources are outside this threat model. Anonymous/open registration without additional abuse controls is also outside the supported baseline.

## Reporting

Use the repository's private security-advisory mechanism for vulnerability reports. Avoid opening a public issue for an unpatched vulnerability.
