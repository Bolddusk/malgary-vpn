# Contract: ComposeDeployer

A second implementation of feature 001's `Deployer` interface
(`backend/internal/tenancy/deployer.go`), used when `MALGARY_DEPLOYER=compose`. It consumes the
**same StackSpec** ([001 contracts/tenant-stack.md](../../001-tier1-pilot/contracts/tenant-stack.md))
and meets the same obligations. Only the mechanics differ.

## Inputs

- Sealed StackSpec (unchanged schema).
- Host settings (environment): `MALGARY_TRAEFIK_NETWORK=coolify`,
  `MALGARY_TRAEFIK_ENTRYPOINT=https`, `MALGARY_TRAEFIK_CERTRESOLVER=letsencrypt`,
  `MALGARY_STACK_DB_HOST=<shared postgres container name>`, `MALGARY_GW_NETWORK=malgary-gw`,
  `MALGARY_HOST_PUBLIC_IP=178.104.48.140`, memory limits per component.

## What it renders, per tenant, into `/opt/malgary/stacks/<tenant>/`

| File | Contents |
| --- | --- |
| `config.yaml` | netbird-server config (same as the Ansible template in 001), `store.engine: postgres` |
| `dashboard.env` | as in 001 |
| `compose.yaml` | services below |
| `setup/index.html` | the setup page (001 template), with the interim egress note |

Services in `compose.yaml` (project `nb-<tenant>`):

| Service | Network | Ports | Limits | Notes |
| --- | --- | --- | --- | --- |
| `netbird-server` | `coolify` | `<stunPort>:<stunPort>/udp` | 256 MB | Traefik labels: gRPC paths → h2c; `/api`, `/oauth2`, `/relay`, `/ws-proxy` → http; `NB_SETUP_PAT_ENABLED=true` |
| `dashboard` | `coolify` | — | 64 MB | Traefik label with `ipAllowList` middleware (operators only) |
| `setup` | `coolify` | — | 16 MB | static file server for `/` and `/setup` |
| `routing-peer` | `malgary-gw` | `<wgPort>:<wgPort>/udp` | 96 MB | NetBird client; `NET_ADMIN`; sysctl `net.ipv4.ip_forward=1`; `/dev/net/tun`; one per tenant (the interim cell has one "gateway", so `routingPeers` has one entry) |
| `guard` | `service:routing-peer` | — | 32 MB | nftables forward-drop table (research I4) + WireGuard byte exporter on `:9586` |

## Obligations (from 001, unchanged)

1. Same hash → no changes (compose files compared byte-for-byte; `docker compose up -d` only on
   change).
2. Secrets resolved from `/opt/malgary/secrets` at render time into files with mode 0600; never
   logged; StackSpec keeps references only.
3. Health check: `GET /api/instance` through the container network must answer 200 within
   2 minutes, otherwise restore `compose.yaml.prev` and return an error.
4. Database and role created with the `malgary_provisioner` login (research I3).

## Teardown

`Remove(tenant)`: `docker compose down -v`, drop database and role, delete
`/opt/malgary/stacks/<tenant>` and the tenant's secrets. Used by the exit procedure (FR-017).
