# Feature Specification: Interim Pilot on the Shared Hetzner Server

**Feature Branch**: `002-interim-pilot-178`

**Created**: 2026-11-03

**Status**: Draft

**Input**: User description: "Interim pilot on the shared Hetzner server 178.104.48.140 (Coolify 4.3.6 host, Ubuntu 24.04, kernel 6.8 with WireGuard, 16 vCPU / 30 GB with ~10 GB available and heavy swap, ufw inactive, Traefik as Coolify's proxy, shared Postgres 18 container, other production apps on the box), under constitution v2.2.0's Interim Hosting Exception. Run the feature-001 platform on this one host for up to 10 test users: tenant NetBird stacks, Zitadel sign-in, malgaryd backend, setup pages, all routed by the existing Coolify Traefik on *.vpn.malgary.com, id.vpn.malgary.com, api.vpn.malgary.com (DNS-only A records already point to 178.104.48.140). Tenant databases live in the existing Postgres 18 with one database and role per tenant. The gateway (routing peers) runs in an isolated Docker bridge network, not host networking, with WireGuard UDP ports published and NAT inside the container; VPN users must never reach private ranges, Docker networks, other apps or the host, proven by an automated test before any user connects. All Malgary containers have memory limits. No changes to the host firewall or other apps. Off-host scheduled Postgres backups before any real company user joins. Exit criteria: migrate to a dedicated cell before real customer users, before an 11th user, or by 2027-02-28, with clients keeping the same DNS names. Reuse feature-001 code and contracts; only deployment differs."

## Context

Feature 001 built the platform for a dedicated cloud cell (DigitalOcean). That cloud account
isn't in place yet. This feature runs the same platform, unchanged, on a server the business
already has, so testing can start now. It operates under the constitution's **Interim Hosting
Exception** (v2.2.0): one shared host, no standby gateway, a temporary egress IP, test users
only, and a fixed exit date.

The server also runs about 48 other Malgary applications. Protecting them is as important as
making the VPN work.

## Clarifications

### Session 2026-11-03

- Q: Is a Hetzner Cloud Firewall in front of the server? → A: No. The VPN's UDP ports are
  reachable once published; no firewall step is needed.
- Q: Are off-host backups needed now? → A: No. The project is in development; backups
  (FR-013, FR-014) are deferred. The FR-015 gate stays in force, so no real company user can be
  added until backups exist and a restore has passed. Backups and email will be set up with
  the dedicated servers (owner: "we will do it when we get the servers").
- Q: Is invitation email needed now? → A: No. Users who sign in with Google or Microsoft need no
  invitation. Managed-account users get a one-time invitation link that the operator or tenant
  admin passes on (FR-012a). Email delivery is added later without changing the flow.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - A test user connects through the interim server (Priority: P1)

A test user from tenant #1 opens their tenant's setup page, installs the NetBird app, signs in
with their company account, and browses through the VPN, leaving from the interim server's IP.

**Why this priority**: This is the reason for the feature: start real testing before the cloud
cell exists.

**Independent Test**: A test user given only `https://onedge.vpn.malgary.com/setup` connects and
"what is my IP" shows the interim server's IP.

**Acceptance Scenarios**:

1. **Given** the platform is running on the interim server, **When** a test user follows their
   setup page, **Then** they are connected within 10 minutes and their traffic leaves from the
   interim server's IP.
2. **Given** a connected user, **When** they sign in, **Then** sign-in happens on the
   Malgary-branded sign-in page with the method the tenant chose (SSO, managed account, or both).
3. **Given** the tenant's routing and posture rules from feature 001, **When** they are changed
   through the API, **Then** they behave exactly as in feature 001.

---

### User Story 2 - The other apps on the server are unaffected (Priority: P1)

The owner confirms that running the VPN on the shared server doesn't change, slow down, or expose
the other applications.

**Why this priority**: The server hosts production work. A VPN pilot must never put it at risk.

**Independent Test**: Before and after the VPN is running, the other apps answer as before, the
host's firewall and NAT configuration is unchanged, and the VPN's containers stay within their
memory limits.

**Acceptance Scenarios**:

1. **Given** the VPN is deployed, **When** the host's firewall and NAT rules are compared with a
   snapshot taken before deployment, **Then** the only differences are those Docker adds for the
   VPN's published ports.
2. **Given** the VPN is running, **When** memory use is checked, **Then** every VPN container is
   within its limit and the total stays within the agreed budget.
3. **Given** the VPN is removed, **When** its containers, networks and databases are deleted,
   **Then** nothing of it remains and the other apps are unaffected.

---

### User Story 3 - VPN users can't reach anything private (Priority: P1)

No matter what routes they are given, VPN users can reach the public internet only. They can't
reach the server itself, its private Docker networks, the other apps' containers, or the shared
database.

**Why this priority**: The gateway sits next to production apps and their database. This is the
main safety condition of the exception.

**Independent Test**: An automated test, run from a VPN client, tries to reach the host, the
Docker networks, private ranges and the database port, and every attempt fails; reaching a
public site succeeds.

**Acceptance Scenarios**:

1. **Given** a connected test client, **When** it tries the host's own public and private
   addresses, any private range (10/8, 172.16/12, 192.168/16, 100.64/10 except its own VPN
   range), and the shared database port, **Then** every attempt fails.
2. **Given** a routing rule that tries to route a private range, **When** it is applied, **Then**
   that traffic still can't reach anything private.
3. **Given** the isolation test fails, **When** the deployment is run, **Then** no user is
   allowed to connect (the deployment stops with a clear message).

---

### User Story 4 - Data is backed up before real users join (Priority: P2, deferred)

> **Deferred (2026-11-03)**: not built during development. It must be done before the first real
> company user joins; FR-015 enforces that.

All VPN data (tenant databases, sign-in database, backend database, stack configuration) is backed
up off the server on a schedule, and a restore has been tested.

**Why this priority**: The shared database currently has no backups. Test users can tolerate
data loss; real company users can't.

**Independent Test**: The nightly backup appears in off-host storage, and a restore into a
scratch database succeeds.

**Acceptance Scenarios**:

1. **Given** the backup schedule, **When** a night passes, **Then** an encrypted backup of every
   VPN database and the stack configuration exists off the server.
2. **Given** no successful backup in 26 hours, **When** the check runs, **Then** the operator is
   alerted.
3. **Given** a real company user is about to be added, **When** no tested backup exists, **Then**
   the import is refused.

---

### User Story 5 - The pilot moves to a dedicated cell without clients reconfiguring (Priority: P3)

When the exit criteria are met, the operator moves all tenants to a dedicated cell. Users keep
the same addresses and reconnect without changing their apps.

**Why this priority**: The exception is time-limited; the move must be planned from the start.

**Independent Test**: A documented, rehearsed procedure moves a test tenant from the interim
server to another host, and an existing client reconnects without reconfiguration.

**Acceptance Scenarios**:

1. **Given** the exit criteria are reached, **When** the move runs, **Then** tenants, users,
   policies and sign-in settings arrive intact and clients reconnect using the same DNS names.
2. **Given** the move is complete, **When** the interim server is checked, **Then** nothing of
   the VPN remains on it.

### Edge Cases

- The server is under memory pressure (it already swaps heavily). VPN containers hit their memory
  limits and restart rather than pushing other apps into swap. Sign-in is the most sensitive
  part, so its limit is set with headroom, and slow sign-in is reported.
- Coolify redeploys, restarts, or cleans up containers it manages. The VPN must not depend on
  Coolify managing its containers, and Coolify's cleanup must not remove them.
- The shared proxy is reconfigured or restarted for another app. VPN routes come back with it.
- An 11th user is imported, or a paying tenant is created. The system refuses: the interim limit
  is enforced, not just documented.
- The exit date (2027-02-28) approaches. The operator is warned 30 days before.
- A VPN user's traffic gets the shared IP blocklisted. The other apps' outbound email could be
  affected. This is a known risk of the exception and is why users are limited to trusted testers.
- The other apps' database container is restarted or upgraded by its owners. VPN stacks
  reconnect on their own.

## Requirements *(mandatory)*

### Functional Requirements

**Reuse**

- **FR-001**: The interim deployment MUST run the feature-001 platform unchanged: per-tenant
  NetBird stacks, Malgary sign-in, the backend, setup pages, routing and posture rules, plans and
  billing, audit and usage. Only how it is deployed differs.
- **FR-002**: Tenant, sign-in and service names MUST be the same as in feature 001
  (`<tenant>.vpn.malgary.com`, `id.vpn.malgary.com`, `api.vpn.malgary.com`), so moving to a
  dedicated cell later needs no client changes.

**Protecting the shared server**

- **FR-003**: The deployment MUST NOT change the host's firewall, NAT or system network settings,
  or any other application's configuration. Only Docker's own rules for the VPN's published
  ports may appear.
- **FR-004**: The gateway MUST run in its own isolated container network, not the host's network.
- **FR-005**: Every VPN container MUST have a memory limit. The total for all VPN containers MUST
  stay within 3 GB for up to 10 users.
- **FR-006**: VPN containers MUST be managed independently of the server's other applications,
  and the other applications' maintenance (redeploys, cleanups) MUST NOT remove or alter them.
- **FR-007**: The VPN's databases in the shared database server MUST each have their own database
  and login with access to nothing else, and the VPN's logins MUST have no administrative rights
  over the shared server.

**User isolation from private resources**

- **FR-008**: VPN users MUST only be able to reach public internet addresses through the gateway.
  The host, private address ranges, Docker networks, other apps and the shared database MUST be
  unreachable, whatever routing rules exist.
- **FR-009**: An automated isolation test MUST run from a real VPN client after every deployment
  and before any user is allowed to connect. A failure MUST block user connections and report
  which destination was reachable.

**Interim limits**

- **FR-010**: The system MUST refuse to add an 11th user across all tenants and MUST refuse
  paying plans (price per seat above zero is allowed only for tenant #1 at the agreed internal
  rate, invoiced as a test).
- **FR-011**: The system MUST warn the operator 30 days before 2027-02-28 and refuse new users
  after that date.
- **FR-012**: The interim egress IP MUST NOT be presented to tenants as their allowlisting
  address: setup pages and admin views label it as temporary.
- **FR-012a**: When email delivery isn't configured, inviting a managed-account user MUST return a
  one-time invitation link to the operator or tenant admin instead of sending an email. Users
  who sign in with Google or Microsoft need no invitation.

**Backups**

Deferred during development (see Clarifications). FR-015 stays in force.

- **FR-013**: Every VPN database and the stack configuration MUST be backed up nightly,
  encrypted, to storage off the server, and kept 30 days.
- **FR-014**: The operator MUST be alerted after 26 hours without a successful backup.
- **FR-015**: Importing a user from a real company (any tenant other than designated test
  tenants) MUST be refused until a backup has been restored successfully at least once.

**Exit**

- **FR-016**: A documented move procedure MUST take every tenant from the interim server to a
  dedicated cell, keeping users, groups, rules, sign-in settings and billing history, with
  clients reconnecting through the same DNS names.
- **FR-017**: After the move, a removal procedure MUST delete every VPN container, network,
  database and file from the interim server.

### Key Entities

- **Interim host**: the shared server. Has a public IP (the temporary egress IP), a memory budget
  for VPN containers, and the exit date.
- **Isolation test result**: the outcome of the automated reachability test for a deployment:
  time, destinations tried, pass or fail.
- **Backup record**: the time, location and restore-test status of each nightly backup.
- Everything else (tenants, users, groups, rules, plans, subscriptions, invoices, audit) is the
  feature-001 data model, unchanged.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: At least 9 of 10 test users connect using only their setup page, in under 10
  minutes each.
- **SC-002**: The isolation test reaches zero private destinations in every run.
- **SC-003**: Over the interim period, the other applications on the server see no outage or
  slowdown attributable to the VPN (no incident traced to it).
- **SC-004**: VPN containers together never exceed 3 GB of memory.
- **SC-005**: A nightly backup exists for every day after backups are enabled, and one restore
  test passes before any real company user joins.
- **SC-006**: The move rehearsal completes with an existing client reconnecting without
  reconfiguration.
- **SC-007**: Monthly extra cost of the interim pilot is under $15 (backup storage only, once
  backups are enabled; zero during development).

## Assumptions

- **Server facts (2026-11-03)**: Hetzner Cloud, Nuremberg, static IPv4 178.104.48.140; Ubuntu
  24.04, kernel 6.8 with WireGuard available; 16 vCPU, 30 GB RAM with about 10 GB available and
  13 GB of swap in use; Docker 27; host firewall (ufw) inactive; Coolify 4.3.6 with its Traefik
  proxy; a Coolify-managed PostgreSQL 18 shared with other apps; no backups configured.
- DNS: `*.vpn`, `id.vpn` and `api.vpn` A records exist in Cloudflare, DNS-only, pointing to the
  interim server (verified 2026-11-03).
- Root SSH access is available to the operator. Deployment does not need Coolify's API, so the
  Coolify version doesn't block this feature.
- Test tenants: tenant #1 (onedge, with up to 10 test users from the company) and the test tenant
  (testco). Real company users in tenant #1 count as real users for FR-015.
- Off-host backup storage (when backups are enabled): an S3-compatible bucket (for example
  Hetzner Object Storage), a small monthly cost covered by SC-007.
- Invitation email: not configured during development (FR-012a). Later it goes through an email
  provider on port 587, which is open from the server.
- No Hetzner Cloud Firewall is attached to the server (owner, 2026-11-03).
- The client portal, originally referred to as feature 002 in feature 001's documents, becomes
  feature 003.
- Out of scope: failover, the dedicated cell itself, IPv6, and any change to the other
  applications or to Coolify.
