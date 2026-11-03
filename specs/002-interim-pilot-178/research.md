# Research: Interim Pilot on the Shared Hetzner Server

**Feature**: [spec.md](spec.md) | **Date**: 2026-11-03 | **Constitution**: v2.2.0 (Interim Hosting Exception)

Server facts come from the owner's read-only command output and the Coolify operator session's
API inspection (2026-11-03). Items marked **verify** are checked in the first deployment task
before anything else is installed.

## I1. How the VPN is deployed on a Coolify host

**Decision**: Malgary runs as plain Docker Compose projects under `/opt/malgary/` on 178,
deployed over SSH by Ansible (platform) and by malgaryd's new **ComposeDeployer** (tenant stacks).
Coolify does not manage them; Coolify's Traefik routes them through Docker labels.

**Rationale**:
- Coolify 4.3.6's API is thin (operator: deployment history and server-resource endpoints 404),
  so driving it by API is fragile. Root SSH is available.
- Feature 001's `Deployer` interface exists for exactly this: a new implementation, no change to
  the tenant directory, StackSpec or the rest of the backend (research R7 of 001).
- Compose projects outside Coolify aren't touched by Coolify redeploys. Coolify's Docker cleanup
  removes unused images and stopped containers only; Malgary containers run with
  `restart: unless-stopped` and named volumes in use, so they survive it. **Verify** the cleanup
  settings on 178 (Settings → Docker cleanup) and turn off "delete unused volumes" if set.

**Alternatives rejected**: Coolify services via API (4.3.6 limits; upgrading Coolify on a host with
48 apps is out of scope); host-level systemd services for everything (needs host packages and
can't reach the shared Postgres, which isn't published to the host).

## I2. Routing through Coolify's Traefik

**Decision**: Malgary containers that serve HTTP join the external Docker network `coolify` and
carry Traefik labels: `Host()` rules for `<tenant>.vpn.malgary.com`, `id.vpn.malgary.com`,
`api.vpn.malgary.com`; gRPC paths to an `h2c` service; TLS from Coolify's ACME resolver.

**Verify** on 178 (first task, read-only): entrypoint names (`http`/`https` in Coolify's default
proxy config) and the certificate resolver name (`letsencrypt`), from
`/data/coolify/proxy/docker-compose.yml`. The labels are templated, so names are variables.

**Rationale**: zero change to the proxy's configuration files; a container with
`traefik.enable=true` on the `coolify` network is how Coolify routes its own apps.

## I3. Database

**Decision**: use the existing Coolify-managed PostgreSQL 18 (`buxe1z5wikkaftx5meckmr4w`, on
the `coolify` network, not published). Create one login role **`malgary_provisioner`** with only
`CREATEDB` and `CREATEROLE` (no superuser). It creates `malgary`, `zitadel` and one `nb_<tenant>`
database + role per tenant.

**Rationale**:
- PostgreSQL 16+ limits a `CREATEROLE` role to managing roles it created, so the provisioner
  can't touch the other apps' roles or databases (FR-007). Tenant roles get no extra rights.
- Zitadel supports PostgreSQL 14–18 (001 research R4); NetBird's postgres store works on 18.
- `citext` ships with the official image; `CREATE EXTENSION citext` runs as the `malgary`
  database owner (allowed for trusted extensions on 13+). **Verify** in the first task.
- The owner created the provisioner login in Coolify's terminal and stored its password at
  `/opt/malgary/secrets/provisioner-password` (0600), checked over TCP on 2026-11-04. Malgary
  never sees the superuser password.

**Risk accepted**: the database server is shared with the event app; a database restart affects
both. NetBird stacks and malgaryd reconnect automatically.

## I4. The gateway in an isolated network (replaces 001's host-network routing peer)

**Decision**: each tenant's routing peer is a NetBird client container on a dedicated bridge
network `malgary-gw` (`10.250.0.0/24`), with `cap_add: NET_ADMIN`, the namespaced sysctl
`net.ipv4.ip_forward=1`, `/dev/net/tun`, and its WireGuard port published as UDP
(`51821+N:51821+N/udp`). NetBird masquerades routed traffic inside the container's network
namespace; Docker then SNATs it out of the host IP like any app's traffic.

A **guard sidecar** shares the routing peer's network namespace (`network_mode: service:...`)
and installs an nftables table that, in the **forward** path only, drops traffic to:
`10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, 100.64.0.0/10` (except the tenant's own NetBird
range), `169.254.0.0/16`, `127.0.0.0/8`, multicast, and the host's public IP `178.104.48.140`.
It re-applies every 30 seconds and exports per-peer WireGuard byte counters (001 R9) on
`:9586` inside that namespace.

**Rationale**:
- No host networking, no host firewall or NAT edits (FR-003, FR-004): Docker's own rules for the
  published port are the only host change.
- Forward-only filtering lets the routing peer itself reach its management URL (which resolves
  to the same host IP) while VPN users can't.
- Different bridge networks are isolated from each other by Docker, so the gateway network can't
  reach `coolify` or any app network even before the guard's rules.
- **Bonus**: one container per tenant, each with its own network namespace, removes 001's open
  question P-2 (several NetBird clients in one namespace).

**Kernel**: WireGuard is built into 178's kernel 6.8 (`wireguard.ko` present), usable from a
container namespace with `NET_ADMIN`.

## I5. Ports on 178

| Port | Use | Status |
| --- | --- | --- |
| 80/443 TCP | Coolify Traefik (unchanged) | in use |
| 443 UDP | Traefik HTTP/3 | in use: Malgary must not use it |
| 3478–3480 UDP | STUN, one per tenant stack (3478 + index) | free |
| 51821–51830 UDP | WireGuard, one per tenant routing peer | free |

`ufw` is inactive and no Hetzner Cloud Firewall is attached (owner, 2026-11-03), so published
ports are reachable directly.

## I6. Backend on 178

**Decision**: malgaryd runs as a container (static binary in a distroless image built on the
host from the release binary), on the `coolify` network, with Traefik labels for
`api.vpn.malgary.com`. It mounts `/var/run/docker.sock` and `/opt/malgary/{secrets,stacks,specs}`
so the ComposeDeployer can run `docker compose` for tenant stacks.

**Risk accepted** (Complexity Tracking): the Docker socket gives the backend root-equivalent
access to the host. On a dedicated cell the Ansible deployer with a sudo wrapper replaces it.

## I7. Sign-in, email

- Zitadel v4.20 (`zitadel` + `login` containers) with memory limits 768 MB + 256 MB, on the
  `coolify` network, database `zitadel` on the shared PostgreSQL. Sign-in is the most
  memory-sensitive piece on a swapping host; slow sign-in is alerted (spec edge case).
- **Email: deferred** (owner, 2026-11-03). Zitadel runs without SMTP. Managed-account
  invitations use Zitadel's `ReturnCode` option: the backend builds the invitation URL and
  returns it to the admin once (FR-012a, `MALGARY_INVITE_DELIVERY=link`). SSO users need no
  invitation. Password resets are done by the operator in Zitadel's console until email exists.
  Later: `mail.laxycone.com:587` (works from 178), sender `no-reply@vpn.malgary.com` with
  SPF/DKIM; switching is one setting.

## I8. Backups (FR-013–FR-015): deferred

**Deferred during development** (owner, 2026-11-03). Only the gate is built now: the
`backup_restore_test` table and the FR-015 check, so real company users stay blocked until the
design below is in place. The design is kept for when it's built.

**Decision**: a `malgary-backup` container (cron, 128 MB) on the `coolify` network dumps every
Malgary database nightly with each database's own credentials, plus `/opt/malgary/{stacks,specs}`
and the secrets directory, encrypts with `age` (owners' public key; private key offline), and
uploads with `rclone` to **Hetzner Object Storage** (S3-compatible, `fsn1`, a different site from
Nuremberg), 30-day lifecycle. It writes a success timestamp the backend reads.

**Restore gate** (FR-015): malgaryd records a restore test (`backup_restore_test` table) when the
operator runs `malgaryctl backup verify`, which restores the latest dump into a scratch database
and compares row counts. Real-user imports are refused until one has passed.

## I9. Interim limits in the backend (FR-010–FR-012)

New configuration, all off on dedicated cells:
- `MALGARY_INTERIM_MAX_USERS=10`: user import counts users across all tenants.
- `MALGARY_INTERIM_UNTIL=2027-02-28`: imports refused after it; a metric
  `malgary_interim_days_left` drives a 30-day warning alert.
- `MALGARY_INTERIM_TEST_TENANTS=onedge,testco,malgary-ops`: tenants allowed while interim; plans
  with a non-zero price only for `onedge` at the internal rate.
- `MALGARY_INTERIM_EGRESS_NOTE`: shown on setup pages and in the tenant API as "temporary IP; do
  not allowlist" (FR-012).

## I10. Isolation test (FR-009)

**Decision**: `malgaryctl isolation-test <tenant>` starts a disposable NetBird client container
on its own bridge network, enrolls it with a single-use, ephemeral setup key into the tenant's
`isolation-test` group (which gets the full-tunnel route), and from inside the VPN tries:
the host's public IP on 22/80/443/5432; `10.0.0.1`, `10.250.0.1`, `172.17.0.1`; the shared
PostgreSQL container's IP; `192.168.0.1`; `169.254.169.254` (Hetzner metadata). All must fail.
Then `https://1.1.1.1` must succeed and the egress IP must be 178.104.48.140. The result is
stored; the tenant's access policy (001 `malgary:access:*`) stays **disabled** until a pass is
recorded for the current StackSpec hash.

## I11. Memory budget (FR-005, SC-004: 3 GB)

| Component | Count | Limit | Total |
| --- | --- | --- | --- |
| Zitadel + login | 1 | 768 + 256 MB | 1,024 MB |
| malgaryd | 1 | 256 MB | 256 MB |
| Backup | 1 | 128 MB | 128 MB |
| Alloy (metrics/logs to Grafana Cloud) | 1 | 128 MB | 128 MB |
| NetBird server | 3 tenants | 256 MB | 768 MB |
| NetBird dashboard | 3 | 64 MB | 192 MB |
| Routing peer + guard | 3 | 96 + 32 MB | 384 MB |
| **Total** | | | **2,880 MB** |

Three tenants: `onedge`, `testco`, and `malgary-ops` (optional on interim; see I12).

## I12. Operator access on 178

178's SSH is managed outside this project and stays as it is (FR-003). The `malgary-ops` tenant
is optional on interim; NetBird dashboards are restricted by Traefik IP allowlist to the
operators' office/VPN egress IPs, configured as a variable. Principle VII's "SSH only via
bastion/VPN" applies again from the dedicated cell (Complexity Tracking).

## I13. Exit (FR-016, FR-017)

The move to a dedicated cell reuses 001's restore path: final backup → `tofu apply` + `site.yml`
on the new cell → restore databases and `/opt/malgary` → change the three Cloudflare A records
(or switch to the delegated zone) → clients reconnect by name. A removal playbook
(`interim-remove.yml`) deletes every Malgary container, network, volume, database and role, and
`/opt/malgary`, from 178.
