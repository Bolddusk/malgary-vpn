<!--
Sync Impact Report
- Version change: 2.1.0 → 2.2.0 (MINOR: added section Interim Hosting Exception, time-limited,
  exit by 2027-02-28)
- Earlier: 2.0.0 → 2.1.0 (MINOR: Principle VII expanded. Tenants choose SSO,
  Malgary-managed accounts with mandatory MFA, or both. Owner/operator MFA and bastion-or-VPN-only
  SSH made explicit. Existing designs stay valid, since SSO is still allowed, so not MAJOR.)
- Modified principles: VII. Security and Auditability (title unchanged)
- Earlier: 1.3.0 → 2.0.0 (Principle V redefined: one NetBird stack per tenant); 1.2.0 → 1.3.0 (added Principle XI, Versioned Plans and Pluggable Payments); 1.1.0 → 1.2.0 (added Principle X, The Portal Is the Product); 1.0.0 → 1.1.0 (added Principle IX; rollout starts at 10 users);
  template → 1.0.0 (initial ratification)
- Principles added: I. Tenant Isolation; II. Everything as Code; III. Stable Egress IPs;
  IV. No Single Point of Failure on the Data Path; V. Scale by Cells;
  VI. Upstream-First and Licence Compliance; VII. Security and Auditability; VIII. Cost Discipline
- Sections added: Operational Standards, Development Workflow, Governance
- Removed sections: none
- Deferred TODOs: none (SLO targets are initial values, to be confirmed after the pilot)
-->

# Malgary VPN Constitution

Malgary VPN is a self-hosted, WireGuard-based business VPN built on NetBird and DigitalOcean.
It serves one internal customer first (about 900 users), then other companies under its own
brand, with a design target of 100,000 users in total.

## Core Principles

### I. Tenant Isolation (NON-NEGOTIABLE)

- Every customer company is a separate tenant: its own NetBird account, users, groups,
  policies, keys and audit trail.
- No tenant can see, reach or affect another tenant's peers, policies or data. This holds even
  when tenants share servers.
- The first (internal) customer is provisioned exactly like any external tenant, through the
  same provisioning path. No special-cased "house" tenant.
- Every feature that touches tenant data includes a test proving cross-tenant access fails.

Rationale: The product is only sellable if isolation is guaranteed. Retrofitting it is a rewrite.

### II. Everything as Code

- All infrastructure (Droplets, reserved IPs, firewalls, DNS, VPCs) is defined in Terraform or
  OpenTofu. All host configuration (NetBird, keepalived, strongSwan, monitoring) is defined in
  Ansible or an equivalent declarative tool.
- No manual changes in production. Emergency fixes are written back to code within 24 hours.
- Any environment, including a full region, can be rebuilt from the repository plus backups.
- Secrets live in a secrets manager or encrypted files. They never appear in the repository in
  plain text, nor in logs or task output.

Rationale: Growing from Tier 1 to many cells is only safe if every server is reproducible.

### III. Stable Egress IPs

- Each gateway pair has one reserved IP. Clients and third parties see that IP and no other,
  including during and after failover.
- Changes that could alter a customer-visible egress IP require a migration plan and customer
  notice. Silent changes are forbidden.
- Dedicated per-tenant IPs are an add-on: a dedicated gateway pair per tenant, never a shared
  IP relabelled as dedicated.
- Every reserved IP is registered as required (e.g. PTA) before it carries production traffic.

Rationale: Customers allowlist these IPs. An IP change is an outage for them.

### IV. No Single Point of Failure on the Data Path

- Every gateway runs as a primary + standby pair with automated failover of the reserved IP.
- Relays run as at least two instances once past Tier 1.
- If the management server is lost, connected users stay connected. Recovery is from tested
  backups within the RTO in Operational Standards, or from a hot standby once licensed.
- Failover is tested before every tier change and at least quarterly.

Rationale: The data path is what users feel. The control plane may degrade; tunnels must not.

### V. Scale by Cells, One NetBird Stack per Tenant

- Self-hosted NetBird runs one account per stack, so every tenant gets its own NetBird stack
  (management, signal, relay, embedded IdP, its own database). Tenants never share a NetBird
  account. Separating them by groups inside one account is forbidden.
- A cell is a set of shared hosts in one region that runs many tenant stacks: control hosts
  (tenant stacks and a PostgreSQL server with one database per tenant) and gateway pairs. Each
  tenant has its own routing-peer instance on each gateway it uses, and all of them leave through
  the gateway pair's reserved IP.
- The 100,000-user target is reached by adding control hosts, gateway pairs and cells. A
  single tenant stack is never enlarged past NetBird's tested limits; a very large tenant gets
  dedicated hosts.
- A tenant lives in exactly one cell. A tenant directory, owned by the provisioning service, is
  the single source of truth for placement, stack version, service names and gateway
  assignments.
- Tenant stacks are cattle: rendered from the tenant directory by automation, never edited by
  hand, and all upgraded to the same NetBird version through a staged rollout.
- Tooling, dashboards and provisioning MUST work across many tenants and cells from the first
  implementation, even while only one of each exists.
- Moving a tenant between hosts or cells has a documented, rehearsed procedure.

Rationale: Self-hosted NetBird does not support many isolated accounts on one stack (embedded
IdP requires single-account mode; the MSP features are cloud-only). One stack per tenant gives
hard isolation and per-tenant SSO without forking NetBird, and shared hosts keep it cheap.

### IX. Grow by Configuration, Not Redesign

The first deployment (10 users) has the same shape as the largest (100,000 users). Growth MUST
only resize, add instances, add regions or add cells. It never replaces a component, engine or
interface. Concretely, from day one:

- **Database**: the management server uses PostgreSQL, never SQLite. At small sizes it may run on
  the same host. Growing moves it to its own server, which is a data move, not an engine change.
- **Endpoints**: every service is reached through a stable DNS name per tenant, cell and role
  (e.g. `<tenant>.vpn.<domain>` for a tenant's management URL, `gw-nyc-1.c01.<domain>`), never by
  raw IP. Moving or splitting a service changes DNS, not client configuration.
- **Identity**: one identity layer that supports a separate SSO connection per tenant is chosen
  in the first spec and kept. Switching identity providers later counts as a redesign.
- **Provisioning**: tenants, users and seats are created through the provisioning service (or its
  first, minimal version), never by hand in the NetBird dashboard, including for the first tenant.
- **Modules**: infrastructure is built from reusable modules (`cell`, `gateway-pair`, `relay`)
  whose size, count and region are variables. A tier change is a variable change plus a reviewed
  plan, never new module code.
- **Operations**: monitoring, backups to offsite object storage, and the audit log exist from the
  first deployment, at a size that matches it.

Allowed to differ between sizes: server sizes, instance counts, regions, number of cells,
whether a role shares a host, and the management hot standby (once licensed).

Rationale: Starting small is fine. Starting with a shape that must be thrown away is not.

### X. The Portal Is the Product

- Customers and their admins use only the Malgary client portal and a Malgary-branded sign-in
  page. They never get access to the NetBird dashboard, NetBird API or NetBird credentials.
- The portal calls NetBird only through Malgary's backend (the provisioning service). That
  backend enforces tenant scoping on every request, so one tenant can never act on another
  tenant's resources, even if the portal is bypassed.
- Every NetBird capability offered to customers (users, groups, access policies, routing,
  posture checks, devices, usage and activity) is wrapped by the portal. Capabilities not yet
  wrapped are unavailable to customers rather than exposed raw.
- The NetBird dashboard is an operator-only tool, reachable only over the VPN or from the
  bastion host.
- The portal's own data model (tenants, subscriptions, seats, invitations, usage) is the system
  of record for commercial data. NetBird is the system of record for network state. Both are
  reconciled automatically, and drift raises an alert.

Rationale: The portal is what customers buy and what separates Malgary from reselling NetBird.
It also keeps NetBird replaceable behind a stable product surface.

### XI. Versioned Plans and Pluggable Payments

- **Owner-defined plans**: owners create and change plans in an internal back-office, never in
  code. A plan defines price per seat, currency, billing period, minimum and maximum seats, and
  entitlements (features and limits, e.g. dedicated IP, posture checks, usage reports).
- **Plans are versioned and immutable once published.** Changing a plan publishes a new
  version. Existing subscriptions stay on the version they bought until they are moved on
  purpose, with customer notice, through a recorded migration. The full history of every plan
  version and every subscription change is kept.
- **Public and enterprise plans**: public plans are offered to everyone. Enterprise plans are
  private to one tenant, with custom price, seats, entitlements and terms. Both use the same
  model.
- **Entitlements drive behaviour.** The product checks what a subscription is entitled to. It
  never checks plan names or prices in code.
- **Payments go through a provider interface.** The first provider is manual: an invoice is
  issued, and an owner records the payment with a reference and proof. Stripe, Payoneer and
  others are added later as further providers, without changes to plans, subscriptions or
  invoices. Each tenant can use a different provider. Provider events (webhooks) are turned
  into internal payment events, which are idempotent.
- **Billing logic is ours.** Seat counts, proration, invoice numbering, dunning and suspension
  rules live in Malgary's billing domain, not in any payment gateway.
- **Money is exact.** Amounts are stored as integer minor units plus a currency code. Issued
  invoices are immutable and sequentially numbered. Corrections are credit notes. Every
  commercial change is audited.

Rationale: Customers will be on many plan versions at once, enterprise deals are custom, and
payment methods will change. A model that assumes one price or one gateway would need a
rewrite.

### VI. Upstream-First and Licence Compliance

- Use NetBird unmodified wherever possible. Integrate through its API, not by forking.
- If server code is modified, publish the modified source to users as AGPLv3 requires, and track
  the patch set so upstream upgrades stay routine.
- Branded clients remove all NetBird names and logos. Licence notices (BSD-3, AGPLv3) are kept.
- Upstream security releases are applied within 7 days; critical ones within 72 hours.

Rationale: The value is in operations and the tenant layer, not in maintaining a VPN fork.

### VII. Security and Auditability

- **Tenant-chosen sign-in**: each tenant decides how its users sign in, as a tenant setting the
  customer admin controls:
  - SSO through the customer's identity provider (Google, Microsoft Entra ID, generic OIDC);
  - Malgary-managed accounts (email or username, password, and MFA);
  - or both. The tenant may require one method per user group.
- MFA is mandatory for Malgary-managed accounts. Password-only sign-in is never allowed. For SSO,
  MFA is enforced by the customer's provider, and the tenant can additionally require Malgary MFA.
- All sign-in methods use the Malgary-branded sign-in page. Only users the tenant has imported or
  invited, or who match its allowed domains, can sign in.
- **Owner and operator access** requires MFA for every account, with least privilege.
  Production SSH is reachable only through a bastion host or the operator VPN, never directly
  from the internet, with keys issued per person.
- Every billing event, provisioning action, policy change and operator action writes an
  append-only audit record: who, what, when, tenant.
- Stripe webhooks and other external events are verified, idempotent and replay-safe.
- The service does not log user traffic content. Connection metadata retention is defined per
  tenant contract and documented in the privacy policy.

Rationale: Enterprise buyers and later SOC 2 audits require these. They are cheap if built now.

### VIII. Cost Discipline

- Every plan that adds infrastructure states its monthly cost and its effect on cost per user.
- The list price MUST stay at least 30% below the cheapest managed competitor with the same
  features. The internal customer is charged the agreed per-user rate (currently $4).
- Prefer resizing (vertical) before adding servers, and adding servers before adding regions,
  unless latency or a customer requirement justifies otherwise.

Rationale: Price is the product's main advantage. It must not erode quietly.

## Operational Standards

- **Initial SLOs** (confirm after the pilot): data-path availability 99.9% monthly per region;
  gateway failover under 60 seconds; management RTO 4 hours, RPO 24 hours.
- **Monitoring**: every gateway, relay and control plane exports health and capacity metrics,
  with alerts for down hosts, failover events, CPU over 50% at peak, and bandwidth nearing the
  included transfer.
- **Backups**: management data is backed up daily, offsite from its region. A restore is
  rehearsed monthly and before every tier change.
- **Capacity**: when a gateway passes 50% CPU at peak, resize the pair (standby first). Past about
  1,000 active users in one region, add a gateway pair. Past the cell limit, open a new cell.
- **Runbooks**: every alert links to a runbook. Every incident gets a written review.

## Interim Hosting Exception (time-limited)

Until the first production cell exists, the pilot MAY run as a single host on the existing shared
Hetzner server `178.104.48.140` (Nuremberg; a Coolify host that also runs other Malgary apps).

**Exempted, and only these:**

- **Principle III**: the egress IP is the shared server's IP and will change once, at migration.
  It MUST NOT be given to any client for allowlisting and is not registered with the PTA as a
  production egress IP.
- **Principle IV**: no standby gateway; the single host is a single point of failure.
- **Principle V**: the dedicated-host assumptions (separate control hosts and gateway pairs per
  cell). Each tenant still gets its own NetBird stack, database and routing peer.

**Conditions (all MUST hold for the whole interim period):**

- The gateway (routing peers) runs in an isolated Docker network: no host networking and no
  changes to the host's firewall or NAT rules.
- VPN users can never reach private address ranges, Docker networks, or the host itself; an
  automated test proves it before any user connects.
- Every Malgary container has a memory limit, because the host is shared and already swapping.
- Test users only: at most 10, and no paying customers.
- Scheduled PostgreSQL backups to off-host storage are running before any real company user joins.
- Every other principle applies unchanged (tenant isolation, everything as code, security and
  audit, versioned plans).

**Exit criteria**: move to a dedicated cell (all exemptions end) before the first real customer
users, before an 11th user, or by 2027-02-28, whichever comes first. The move is a planned
migration with notice to pilot users; clients reconnect to the same DNS names.

Rationale: lets the pilot start on hardware the business already pays for, while keeping the
risks to the other apps on that server and to future customers bounded and dated.

## Development Workflow

- Work follows Spec Kit: `/speckit-specify` → `/speckit-clarify` → `/speckit-plan` →
  `/speckit-tasks` → `/speckit-analyze` → `/speckit-implement`. One feature per spec.
- Rollout: 10 pilot users, then 50, then growth as customers move over. Each step is a variable
  change under Principle IX.
- Planned feature order: (1) Tier 1 pilot infrastructure; (2) ops baseline (monitoring,
  backups, failover tests); (3) tenant provisioning service (NetBird API + billing domain with
  manual payments + cell directory); (4) customer and admin portal; (5) branded clients.
- Pilot gate: no work on the provisioning service or portal starts until the pilot confirms
  reserved-IP failover, outbound traffic via the reserved IP, posture checks, and NetBird
  multi-account isolation.
- Testing: infrastructure changes are tested in a staging cell first. The provisioning service
  has integration tests against a real NetBird test instance and Stripe test mode, including
  cross-tenant isolation and webhook replay tests.
- Every plan's Constitution Check covers all eleven principles. Justify any deviation in the
  plan's Complexity Tracking section.

## Governance

- This constitution overrides other project practices. Specs, plans and tasks that conflict
  with it are revised, or the constitution is amended first.
- Amendments are proposed as a change to this file, with rationale and a migration note for
  anything already built. The project owner approves them.
- Versioning follows semantic versioning: MAJOR for removing or redefining a principle, MINOR for
  a new principle or materially expanded guidance, PATCH for wording fixes.
- Compliance is reviewed at each `/speckit-plan` (Constitution Check) and `/speckit-analyze`
  step, and again before each tier change in production.

**Version**: 2.2.0 | **Ratified**: 2026-11-03 | **Last Amended**: 2026-11-03
