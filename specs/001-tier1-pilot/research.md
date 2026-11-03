# Research: Tier 1 Pilot

**Feature**: [spec.md](spec.md) | **Date**: 2026-11-03

Facts were checked on 2026-11-03 against NetBird v0.82.0 (released 2026-10-29) source and docs,
Zitadel v4.20.1, and DigitalOcean docs. Items marked **unconfirmed** have no direct evidence yet
and are listed as pilot tests in [quickstart.md](quickstart.md).

## R1. Multi-tenancy on self-hosted NetBird (blocking finding)

**Decision**: One NetBird stack per tenant, on shared hosts (Constitution v2.0.0, Principle V).

**Findings**:
- The current self-hosted server (`netbirdio/netbird-server`, combined management, signal, relay
  and STUN) always runs the embedded IdP (Dex). The embedded IdP refuses to start in
  multi-account mode: "embedded IdP requires single account mode".
  Source: `management/cmd/management.go`, [quickstart docs](https://docs.netbird.io/selfhosted/selfhosted-quickstart).
- Multi-account mode (`--disable-single-account-mode`) works only with the legacy setup and an
  external IdP. Account grouping depends on custom JWT claims (`wt_account_domain`, category
  `private`). Public domains such as gmail.com give every user a separate account.
- There is no API to create accounts. The MSP tenants API is `x-cloud-only`. The Enterprise
  licence does not add multi-tenancy: "each customer needs a separate stack with its own
  license" ([enterprise docs](https://docs.netbird.io/selfhosted/enterprise)).

**Rationale**: A separate stack per tenant gives hard isolation (separate process and database),
per-tenant SSO connectors in each stack's embedded IdP, and per-tenant upgrades and restores,
without forking NetBird. A stack is small (one container plus a database), so many fit on one
host.

**Alternatives rejected**:
- One account, tenants separated by groups and policies: weak isolation, and tenant admins
  would share one account. Violates Principle I.
- Legacy multi-account mode with custom claims: unsupported path, no account-creation API,
  and known open bugs (netbird#2213, #2620).
- NetBird Cloud MSP: not self-hosted, and per-user cost removes the price advantage.
- Forking NetBird for multi-tenancy: permanent maintenance burden. Violates Principle VI.

## R2. Shared egress IP across tenants

**Decision**: Each gateway pair runs one NetBird routing-peer container per tenant. Each
container is a peer in its own tenant's stack and advertises the routes (exit node 0.0.0.0/0
and/or specific networks) for that tenant. All containers NAT through the host, whose default
route goes via the reserved IP's anchor gateway.

**Rationale**: A routing peer belongs to one account, so tenants can't share a single routing
peer. Containers on the same host share its egress path, so all tenants on a pair keep the same
reserved IP. Adding a tenant adds a container, not a server.

**Findings**: exit nodes can be distributed to chosen groups. Routes are additive across groups.
The metric (1–9999, lower wins) applies only within an HA group of routing peers serving the same
network ([network routes](https://docs.netbird.io/manage/network-routes)). Primary and standby
containers form one HA route group, so metric decides the active one.

**Unconfirmed, pilot test P-2**: several NetBird clients on one host, each with its own WireGuard
interface name and port, running together without conflict.

## R3. Gateway failover and egress IP

**Decision**: keepalived on both gateways in unicast mode over the VPC (no multicast VRRP),
with a notify script that calls the DigitalOcean API to assign the reserved IP
(`POST /v2/reserved_ips/{ip}/actions`, `{"type":"assign","droplet_id":N}`). Both gateways have
the anchor-gateway default route preconfigured persistently (netplan, cloud-init network
management turned off). NetBird routing-peer HA (metric) runs alongside, so clients switch
routing peer as the primary goes away.

**Findings**:
- A reserved IP gives no HA by itself; failure detection and reassignment are ours to build
  ([features](https://docs.digitalocean.com/products/networking/reserved-ips/details/features/)).
- Outbound via reserved IP needs a default route via the anchor gateway, read from
  `169.254.169.254/metadata/v1/interfaces/public/0/anchor_ipv4/gateway`
  ([outbound traffic](https://docs.digitalocean.com/products/networking/reserved-ips/how-to/outbound-traffic/)).
- The DO keepalived tutorial targets Ubuntu 14.04. Use it for concepts only. DO Cloud Firewall
  reportedly doesn't pass VRRP, so peers use unicast and nftables host rules.
- Default limit is 3 reserved IPs per account. Request an increase before Tier 2.
- The DO VPC NAT Gateway gives a random egress IP unless you bring your own IP block, and its
  HA status is unconfirmed. It is not used.

**Unconfirmed, pilot test P-1**: reassignment time, and whether the new holder's outbound
traffic uses the reserved IP immediately. Target: under 60 seconds end to end.

## R4. Identity layer

**Decision (provisional, final after pilot test P-3)**: Zitadel as Malgary's central identity
service. Each tenant is a Zitadel organization with its own IdPs (Google, Entra, OIDC), login
policy (MFA required) and branding. Each tenant's NetBird stack has its embedded IdP configured
with one connector: Zitadel, scoped to that tenant's organization.

**Findings**:
- The embedded IdP supports Google, Entra, Okta, Zitadel and generic OIDC connectors, and TOTP MFA
  for local users. Branding is hard-coded ("NetBird"), and it cannot be disabled in the combined
  server.
- Zitadel organizations support their own IdPs, forced MFA, branding and verified domains.
  Email invitations (`CreateInviteCode`) let imported users with any email set a password and
  MFA. One project can be granted to many organizations. The organization is selected by scope
  `urn:zitadel:iam:org:id:{id}` or by domain discovery.
- Known issue: zitadel#11869 (after v4.12.3, the `org:id` scope rejects users who only hold project
  grants). Avoid it by creating one OIDC application per tenant organization, not relying on
  project grants.
- NetBird does not map Zitadel organizations itself. Not needed here, because each stack sees
  only one organization.
- Zitadel is AGPL-3.0 since v3 and needs PostgreSQL 14–18.

**Rationale**: Principle X requires a Malgary-branded sign-in and invitations for any email
address. The embedded IdP alone gives neither. Zitadel gives both, plus one place to manage
users across all tenant stacks. The portal (002) creates users in Zitadel. NetBird creates the
user on first login, with group sync from a claim.

**Alternatives**: embedded IdP with Google/Entra connectors directly (simplest, but NetBird-branded
and no email invitations; kept as the baseline in test P-3). Keycloak (heavier, Java). Authentik
(fine, but per-tenant branding is weaker).

**Unconfirmed, pilot test P-3**:
- With Zitadel as the only connector, does the NetBird login skip Dex's chooser page and go
  straight to the branded Zitadel page?
- Does group sync work through Dex to NetBird?

## R5. Database

**Decision**: one PostgreSQL 17 server per cell, on the control host in the pilot. It holds one
database and role per tenant NetBird stack, plus `zitadel` and `malgary` (the backend) databases.
NetBird config: `server.store.engine: postgres`, DSN via `NB_STORE_ENGINE_POSTGRES_DSN`
([postgres store](https://docs.netbird.io/selfhosted/postgres-store)).

**Rationale**: Principle IX requires PostgreSQL from day one. DO Managed PostgreSQL starts at
$15/month single node and about $60/month for HA. Self-run in the pilot, and move to a managed
cluster or a dedicated host later as a data move. Note: v0.82.0 removed the MySQL store, and
there is no SQLite-to-Postgres tool (pgloader only), which confirms starting on Postgres.

## R6. Backend language and structure

**Decision**: Go (current stable) for the Malgary backend: provisioning, tenant directory,
billing domain, reconciliation and audit log. HTTP JSON API (OpenAPI 3.1). PostgreSQL via `pgx`
with `sqlc`, migrations with `goose`. Portal frontend (002) is a separate TypeScript app.

**Rationale**: NetBird ships an official Go REST client
(`github.com/netbirdio/netbird/shared/management/client/rest`), and Zitadel has a Go SDK.
Go gives a single static binary, strong concurrency for reconciling many stacks, and the same
language as NetBird for reading its source.

**Alternatives**: TypeScript/NestJS (one language with the portal, but no official NetBird client).
Python (fine for scripts, weaker for a long-running reconciler).

## R7. Deploying tenant stacks

**Decision**: the backend owns the desired state (tenant directory). A deploy step renders a
Docker Compose project per tenant (`netbird-server` plus config) and applies it on the
assigned control host. For each tenant, a routing-peer container per gateway. In the pilot
the deploy step is an Ansible role run by the provisioning command. The render contract
([contracts/tenant-stack.md](contracts/tenant-stack.md)) is fixed, so the deployer can move to
Nomad or Kubernetes later without changing the tenant directory.

**Rationale**: meets Principles II and IX with the least machinery for the pilot. The contract,
not the tool, is what must not change.

## R8. Infrastructure as code

**Decision**: OpenTofu with the `digitalocean/digitalocean` provider (v2.106.0, 2026-10-12).
Modules `cell` (VPC, firewall, DNS zone records, Spaces bucket), `control-host`, `gateway-pair`
(two Droplets, a `digitalocean_reserved_ip` and `digitalocean_reserved_ip_assignment`), and
`relay` (unused in the pilot: the tenant stack's built-in relay is used). Remote state in a
Spaces bucket. Ansible for host configuration.

## R9. Monitoring, backups, email

**Decisions**:
- **Monitoring**: Grafana Alloy on every host, sending to Grafana Cloud (free tier) via Prometheus
  remote_write. Alerts are evaluated off-site, so they still fire when a host is down.
  Swappable later for self-hosted Prometheus/Mimir.
- **Per-user usage**: NetBird OSS exposes peer `connected` and `last_seen`. Traffic events are
  Enterprise-only and cloud-flagged. For usage volume, the backend reads per-peer byte counters
  from each tenant routing peer's WireGuard interface (`wg show <if> transfer`). Unconfirmed
  (pilot test P-5).
- **Backups**: nightly `pg_dump` per database, plus tenant stack configs, encrypted (age) and sent
  to a Spaces bucket in AMS3, outside the NYC region. 30-day retention. Spaces costs $5/month
  for 250 GiB.
- **Email**: DigitalOcean blocks SMTP ports 25, 465 and 587. Send invitations and alerts through
  an email provider's HTTPS API (e.g. Postmark or Amazon SES). Zitadel SMTP must use a provider
  port that DO doesn't block (e.g. 2525). Unconfirmed (pilot test P-6).

## R10. Client onboarding

**Findings**:
- Desktop: `netbird up --management-url https://<tenant>.vpn.<domain>:443`.
- Windows installers have no property for the management URL. Pre-seed
  `C:\ProgramData\Netbird\default.json` with `ManagementURL` for MDM rollouts
  ([bootstrap via config file](https://docs.netbird.io/manage/peers/bootstrap-via-config-file)).
- Android: menu, then Change Server, then enter the URL. iOS steps are unconfirmed (pilot test P-4).

**Decision**: the setup page is per tenant (`https://<tenant>.vpn.<domain>/setup`), showing that
tenant's own management URL, so users can't mistype it.

## R11. Pilot cost (updates SC-009)

| Item | Monthly |
| --- | --- |
| Control host, 4 vCPU / 8 GB shared, with weekly backups (tenant stacks, Zitadel, Postgres, backend) | $57.60 |
| Gateway pair, 2 × 2 vCPU / 2 GB | $36 |
| Reserved IP (assigned) | $0 |
| Spaces (backups and state) | $5 |
| Grafana Cloud free tier, email provider free tier | $0 |
| 10% reserve | $9.86 |
| **Total** | **about $108** |

About $37 more than the business plan's $71, mainly the larger control host for Zitadel and
per-tenant stacks. At $4 a seat it breaks even at 27 users. The cost grows by about $0 to $2 per
tenant stack until the host is full.

## R12. Excluding call traffic from a full tunnel (FR-006)

**Decision**: Test first (pilot test P-7). Preferred: the full-tunnel exit node, plus NetBird's
client-side handling of routes so that call ranges stay on the local connection. Fallback: if
an exit node can't exclude destinations, "full tunnel" is a set of routes covering all
public IPv4 ranges except the call ranges (computed by the backend as a CIDR complement and
applied as one route set per routing group).

**Rationale**: NetBird routes are additive, and a 0.0.0.0/0 exit node has no documented exclusion
list. The complement approach uses only standard routes, so it needs no fork. Its cost is a
longer route list (hundreds of prefixes), which the pilot must measure on clients.

## R13. Operator access

**Decision**: An internal tenant `malgary-ops` (its own NetBird stack, operators only, MFA) with
a route to the cell's VPC range. SSH on all hosts accepts connections only from the VPC. Before
the ops tenant exists, and for break-glass, use the DigitalOcean Droplet console. Owners and
operators sign in to Zitadel's admin organization with MFA required.

**Rationale**: Meets Principle VII (SSH only via bastion or VPN) and uses the product itself, so
operators find problems first.

## R14. Domain and DNS

**Decision**: Use subdomains of `malgary.com`. Delegate `vpn.malgary.com` (and
`vpn-staging.malgary.com` for staging) to DigitalOcean's nameservers with NS records at the
current DNS host of `malgary.com`. All service names live inside that zone:

| Name | Purpose |
| --- | --- |
| `<tenant>.vpn.malgary.com` | tenant management URL and setup page (e.g. `onedge.vpn.malgary.com`) |
| `id.vpn.malgary.com` | Malgary sign-in (Zitadel) |
| `api.vpn.malgary.com` | Malgary backend API (portal in 002) |
| `ctl-1.c01.vpn.malgary.com`, `gw-nyc-1a.c01.vpn.malgary.com` | hosts (private VPC addresses) |

Tenant slugs `id`, `api`, `www`, `portal`, `status`, `admin`, `ops` and anything starting with
`c` plus two digits are reserved, so they never collide with service or cell names.

**Where**: `malgary.com` DNS is managed in Cloudflare (registrar not relevant: Cloudflare is
authoritative). In the Cloudflare dashboard for `malgary.com`, add three records of type `NS`,
name `vpn`, values `ns1.digitalocean.com`, `ns2.digitalocean.com`, `ns3.digitalocean.com`
(and the same for `vpn-staging`). NS records are never proxied. Cloudflare's proxy (orange
cloud) must not be used for these names anyway: it can't carry WireGuard or NetBird's gRPC.

**Rationale**: No second domain needed, and the delegated zone keeps the website and email
records out of OpenTofu's reach. Changing to a dedicated product domain later is a DNS move
(Principle IX: services are only ever reached by name).

**Alternatives**: Move all of `malgary.com` to DigitalOcean DNS. That would put the website and
email records under this project's automation. Rejected.

