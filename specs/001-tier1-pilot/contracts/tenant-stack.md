# Contract: Tenant Stack Render

This is the boundary between the backend (desired state) and the deployer (Ansible in the
pilot; Nomad or Kubernetes later). The deployer may change. This contract may not change
without a constitution-reviewed migration (Principle IX).

## Input: StackSpec (JSON, produced by the backend per tenant)

```json
{
  "tenant": "onedge",
  "netbirdVersion": "0.82.0",
  "controlHost": "ctl-1.c01.<domain>",
  "managementDomain": "onedge.vpn.malgary.com",
  "httpPort": 20000,
  "database": { "host": "host.docker.internal", "name": "nb_onedge", "user": "nb_onedge", "passwordRef": "secret://tenants/onedge/db" },
  "relay": { "mode": "embedded" },
  "routingPeers": [
    { "host": "gw-nyc-1a.c01", "wgInterface": "wt-onedge-3f2", "wgPort": 51821, "metric": 100, "setupKeyRef": "secret://tenants/onedge/gw" },
    { "host": "gw-nyc-1b.c01", "wgInterface": "wt-onedge-3f2", "wgPort": 51821, "metric": 200, "setupKeyRef": "secret://tenants/onedge/gw" }
  ],
  "localAuthDisabled": true,
  "hash": "sha256:…"
}
```

Notes (amended during implementation, 2026-11-03):

- `localAuthDisabled` (added 2026-11-04, feature 002 testing): hides NetBird's own
  email/password login so Malgary sign-in is the only way in. Omitted (false) in the first
  deploy pass, because the server won't start without another identity provider; set in the
  second pass once bootstrap has registered Malgary sign-in.

- `httpPort` (20000–29999) is the stack's local port behind Traefik. Its STUN port is derived as
  `3478 + (httpPort − 20000)` and its operator dashboard port as `httpPort + 10000`.
- The identity provider is not part of the spec. Malgary sign-in is added to the stack through
  NetBird's API during bootstrap (`internal/tenancy/bootstrap.go`), after `/api/setup` with
  `NB_SETUP_PAT_ENABLED=true` creates the stack's break-glass owner and the backend's service user.
- Deployment is two passes: the stack alone, then the stack plus `routingPeers` once the
  gateway setup key exists.

## Deployer obligations

1. **On the control host**: run the tenant's `netbird-server` and dashboard with this
   configuration. The dashboard is bound to the VPC and operator VPN only. Route
   `managementDomain` through the host's shared reverse proxy (Traefik) with an automatic TLS
   certificate.
2. **On each gateway host**: run one NetBird client container per tenant entry, with its own
   interface and port, IP forwarding and NAT out of the host default route (the anchor
   gateway, so the reserved IP).
3. **Secrets**: resolve `*Ref` values from the secret store at deploy time. Never write them
   to the repository or to logs.
4. **Report back**: `applied_hash` and per-component health to
   `POST /v1/tenants/{id}/stack:report`.
5. **Rollback**: if health checks fail after an upgrade, revert to the previous version
   automatically.
6. **Idempotency**: applying the same `hash` again changes nothing.
