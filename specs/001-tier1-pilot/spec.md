# Feature Specification: Tier 1 Pilot (10 users, growing to 50)

**Feature Branch**: `001-tier1-pilot`

**Created**: 2026-11-03

**Status**: Draft

**Input**: User description: "Tier 1 pilot for 10 users (then 50), built so it grows without redesign: one cell (c01) in DigitalOcean NYC with NetBird management on PostgreSQL, signal and relay on one host; one primary+standby gateway pair with a reserved IP, keepalived failover and anchor-IP outbound traffic so clients always see the same egress IP; per-cell DNS names; SSO sign-in with Google/Microsoft Entra through an identity layer that must support a separate SSO connection per tenant (decide between NetBird's embedded IdP and Zitadel during the pilot by testing two tenants); tenant #1 (the internal company) created through a minimal provisioning script, not by hand; users install the official NetBird app (desktop and mobile) and point it at our management URL, guided by a setup page on our own domain (download links, one-line desktop command, mobile 'Change server' steps); reusable Terraform modules (cell, gateway-pair, relay) and Ansible; monitoring, offsite backups and an audit log from day one; pilot success criteria include failover under 60 seconds with unchanged egress IP, posture checks working, and the onboarding steps completed by pilot users without help."

## Context

This is the first deployment of Malgary VPN. It replaces Perimeter 81 for 10 pilot users of
the first customer (tenant #1), then grows to 50 users with no redesign. It must also prove
the decisions that are hard to reverse later: per-tenant sign-in, stable egress IPs, and a
deployment that grows by configuration only (Constitution Principles I, III, V and IX).

## Clarifications

### Session 2026-11-03

- Q: Which identity provider does tenant #1 use? → A: Google. The company creates a Google
  account for every user.
- Q: What traffic goes through the VPN? → A: Not fixed. Admins configure it in a UI, per tenant,
  per group of users, or per individual user, to keep fine-grained control.
- Q: What do customers see? → A: Only Malgary's own client portal. A customer buys a package
  (e.g. 1,000 seats at $4), imports its users by email, manages groups, routing, policies and
  posture, and monitors who is and isn't using the service. NetBird runs behind the scenes and
  is never exposed to customers. The portal is feature 002. This pilot builds the backend it
  will sit on.
- Q: How are plans and payments handled? → A: Owners create plans and keep their history.
  Updating a plan doesn't change what existing subscribers bought. Enterprise plans are custom,
  per company. Payments are manual for now, with several payment gateways added later.
- Q: How do a customer's users sign in? → A: The customer decides for its own users: SSO
  (Google, Microsoft or other OIDC), a username/email and password with MFA, or both
  (Constitution v2.1.0, Principle VII).
- Q: (from /speckit-analyze) What admin interface does the pilot use for routing? → A: The
  Malgary API through `malgaryctl`. The portal UI arrives in feature 002. The NetBird dashboard
  is operator view-only, because the backend owns routing rules.
- Q: Which domain does the service use? → A: Malgary's company domain, `malgary.com`, through
  subdomains only. The subdomain `vpn.malgary.com` is delegated to DigitalOcean DNS, so the
  company website and email DNS stay untouched. Staging uses `vpn-staging.malgary.com`.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Pilot user connects with company sign-in (Priority: P1)

A pilot user receives a link to the setup page, installs the official NetBird app on a laptop,
points it at the Malgary VPN service, signs in with their company account, and reaches the
internet and client systems through the VPN's fixed egress IP.

**Why this priority**: This is the product. Without it nothing else in the pilot matters.

**Independent Test**: Give one new pilot user only the setup page link. They install, sign in
and confirm that a "what is my IP" check shows the pilot's reserved IP, with no help from the
operator.

**Acceptance Scenarios**:

1. **Given** a user in tenant #1 who has not installed anything, **When** they follow the setup
   page on a Windows or macOS laptop, **Then** they are connected within 10 minutes and their
   traffic to in-scope destinations leaves from the reserved egress IP.
2. **Given** a connected user, **When** they sign in, **Then** sign-in happens through their
   company identity provider, including that provider's MFA. No VPN-specific password exists.
3. **Given** a user whose company account is disabled, **When** their session next needs
   re-authentication, **Then** they can no longer connect.
4. **Given** a user on iOS or Android, **When** they follow the setup page's mobile steps,
   **Then** they connect with the same sign-in and the same egress IP.

---

### User Story 2 - Gateway failure is invisible to clients' allowlists (Priority: P1)

When the active gateway server fails, the standby takes over the same egress IP, so customers'
allowlisted systems keep accepting pilot users' traffic.

**Why this priority**: Customers allowlist the egress IP. A changed IP is an outage for them.
This is the riskiest technical assumption in the business plan.

**Independent Test**: With users connected, power off the active gateway. Measure the time
until traffic flows again and confirm the egress IP is unchanged.

**Acceptance Scenarios**:

1. **Given** users connected through the active gateway, **When** the active gateway is
   stopped, **Then** traffic resumes through the standby within 60 seconds and the egress IP
   seen by external services is unchanged.
2. **Given** the standby is now active, **When** the failed server returns, **Then** it rejoins
   as standby without a second interruption.
3. **Given** a failover happened, **When** the operator checks monitoring, **Then** the event
   is recorded and an alert was sent.

---

### User Story 3 - Each tenant signs in with its own identity provider (Priority: P1)

The operator creates a second, test tenant with a different identity provider. Users of each
tenant sign in only through their own company's provider and see only their own tenant.

**Why this priority**: This decides the identity layer for the life of the product
(Principle IX). It must be settled before more users join.

**Independent Test**: Create a test tenant bound to a separate test identity provider. Sign in
as a user of each tenant and confirm neither can sign in through the other's provider or see the
other's users, devices or policies.

**Acceptance Scenarios**:

1. **Given** tenant #1 and a test tenant with different identity providers, **When** a test
   tenant user tries to sign in, **Then** they are authenticated only by the test tenant's
   provider and land only in the test tenant.
2. **Given** a tenant admin of tenant #1, **When** they view users, devices or policies,
   **Then** nothing belonging to the test tenant is visible or reachable.
3. **Given** a tenant set to Malgary-managed accounts, **When** an invited user signs in for
   the first time, **Then** they must set a password and MFA before connecting, and a later
   attempt without MFA fails.
4. **Given** the pilot's comparison of identity-layer options, **When** it is complete, **Then**
   a written decision records the chosen option and the evidence for it.

---

### User Story 4 - Operator provisions a tenant and its users without manual dashboard work (Priority: P2)

The operator creates a tenant, links its identity provider, sets its default access policy and
seat count with a single provisioning step, and the result is recorded in the audit log.

**Why this priority**: The provisioning path grows into the commercial provisioning service.
Starting by hand would mean redoing tenant #1 later.

**Independent Test**: Run the provisioning step for the test tenant from a clean state and
confirm the tenant, its policy and an audit record exist without anyone using the dashboard.

**Acceptance Scenarios**:

1. **Given** a tenant definition (name, identity provider, seat limit, default policy), **When**
   the operator runs provisioning, **Then** the tenant exists with that configuration and an
   audit record names who did it and when.
2. **Given** provisioning has run, **When** it is run again with the same definition, **Then**
   nothing is duplicated and nothing changes.
3. **Given** a tenant at its seat limit, **When** another user tries to register a device,
   **Then** registration is refused with a clear message, and the operator is alerted.

---

### User Story 5 - Admin controls which traffic uses the VPN, per group or per user (Priority: P2)

An admin opens the admin UI and chooses what traffic goes through the VPN for the whole
tenant, for a group of users (e.g. one client team), or for a single user. Choices are full
tunnel (all internet traffic except calls) or only specific destinations (e.g. one client's
systems). Changes reach users' devices without reinstalling or reconfiguring the app.

**Why this priority**: Different client teams need different routes, and the business wants
fine-grained control. It also keeps bandwidth down where full tunnel isn't needed.

**Independent Test**: Create two groups in tenant #1. Set group A to full tunnel and group B to
one client's address range only. Confirm A's users browse from the egress IP, and B's users
reach that client through the egress IP while other sites use their own connection. Then
override one user in group B to full tunnel and confirm only that user changes.

**Acceptance Scenarios**:

1. **Given** a tenant default of full tunnel, **When** an admin sets a group to "specific
   destinations only", **Then** within 5 minutes that group's devices send only those
   destinations through the VPN, and other users are unaffected.
2. **Given** a user in a group, **When** an admin sets a rule for that individual user,
   **Then** the user's rule takes precedence over the group's, and the group's takes precedence
   over the tenant default.
3. **Given** any routing change, **When** it is saved, **Then** an audit record names the admin,
   the change, the target (tenant, group or user) and the time.
4. **Given** an admin in tenant #1, **When** they open routing settings, **Then** they can see and
   change only tenant #1's groups, users and routes.

---

### User Story 6 - Unhealthy devices are blocked (Priority: P2)

Devices that fail the tenant's posture rules cannot reach protected destinations until fixed.

**Why this priority**: Perimeter 81 offers this today. The business plan marks it as an
unconfirmed capability.

**Independent Test**: Set a minimum operating-system version for tenant #1. Connect a device
below it and confirm it is blocked from protected destinations and told why. Upgrade it, and
confirm access returns.

**Acceptance Scenarios**:

1. **Given** a posture rule requiring a minimum OS and app version, **When** a non-compliant
   device connects, **Then** it cannot reach protected destinations.
2. **Given** that device is brought into compliance, **When** it reconnects, **Then** access is
   restored without operator action.

---

### User Story 7 - Pilot grows from 10 to 50 users by configuration only (Priority: P2)

The operator grows the pilot to 50 users, and if needed resizes the servers, by changing
configuration values only. No component is replaced.

**Why this priority**: This proves Principle IX on a small scale before the real migration.

**Independent Test**: Change the sizing values, apply them, and confirm users experience at most
one short reconnect, and that no component was replaced and no client was reconfigured.

**Acceptance Scenarios**:

1. **Given** the 10-user deployment, **When** the operator increases server sizes through
   configuration, **Then** the change applies with at most one reconnect of under 60 seconds
   per user, and the egress IP and service addresses are unchanged.
2. **Given** the full deployment is destroyed in a staging environment, **When** it is rebuilt
   from the repository and the latest backup, **Then** tenants, users and policies are
   restored and existing clients reconnect without being reconfigured.

---

### User Story 8 - Operator sees health, backups and audit trail (Priority: P3)

The operator has dashboards and alerts for every server, daily offsite backups that have been
test-restored, and an audit log of administrative actions.

**Why this priority**: Required from day one by the constitution. It supports the other stories
but delivers no user-facing value on its own.

**Independent Test**: Stop a service and confirm an alert arrives. Restore the latest backup
into staging. Query the audit log for the last provisioning action.

**Acceptance Scenarios**:

1. **Given** any server or core service stops, **When** 2 minutes pass, **Then** the operator
   has received an alert that links to a runbook.
2. **Given** daily backups, **When** the monthly restore drill runs, **Then** the restore
   succeeds and the result is recorded.

---

### Edge Cases

- A user installs the app but leaves it on NetBird's own cloud service instead of ours. The
  setup page and the sign-in screen must make the mistake obvious and easy to fix.
- Both gateways fail at once. Users lose VPN connectivity, and calls are unaffected because
  they don't go through the VPN. The operator is alerted immediately.
- The management service is down. Already-connected users stay connected; new sign-ins wait.
- A user signs in with a personal account (e.g. personal Gmail) instead of their company
  account. They are refused.
- A user leaves the company. Disabling them in the company identity provider, or removing them
  through provisioning, blocks new connections and frees a seat.
- A failed server returns while the other is active. Both must not claim the egress IP at the
  same time.
- A device clock is badly wrong, causing sign-in failures. The setup page lists this fix.
- Phone and laptop count as two devices but one user seat.
- A user belongs to two groups with different routing rules. A per-user rule, if any, wins.
  Otherwise the broader rule (full tunnel) wins, and the UI shows the conflict to the admin.
- An admin sets "specific destinations only" with an empty list. The UI refuses to save it, so
  a group can't be left with no VPN access by accident.

## Requirements *(mandatory)*

### Functional Requirements

**Connection and sign-in**

- **FR-001**: Users MUST be able to connect using the official, unmodified NetBird apps for
  Windows, macOS, Linux, iOS and Android, pointed at the Malgary VPN service address.
- **FR-002**: Each tenant MUST be able to choose how its users sign in: SSO through its
  identity provider, Malgary-managed accounts (email or username, password and mandatory MFA),
  or both. The choice is a tenant setting. Password-only sign-in MUST never be possible.
- **FR-003**: Only accounts the tenant has approved MAY sign in: accounts on the tenant's
  company domain, or, for tenants using individual Google accounts, an explicit list of
  approved addresses kept by provisioning. All other accounts MUST be refused.
- **FR-004**: Tenant #1 MUST sign in with Google. The test tenant MUST use Microsoft Entra ID,
  so the pilot covers both providers.
- **FR-005**: Admins MUST be able to set, through the Malgary admin interface (API and
  `malgaryctl` in this pilot; the portal UI in feature 002), which traffic goes through
  the VPN: full tunnel (all internet traffic) or specific destinations only (addresses or
  ranges). It can be set at three levels: tenant default, group of users, and individual user.
  The most specific level wins: user, then group, then tenant default.
- **FR-005a**: Routing changes MUST reach affected devices within 5 minutes, without
  reinstalling or reconfiguring the app, and MUST be written to the audit log.
- **FR-005b**: An admin MUST only be able to see and change routing for their own tenant.
- **FR-005c**: Tenant #1's default for the pilot is full tunnel. Admins change it per group
  or user as needed.
- **FR-005d**: Routing rules MUST be stored and applied only by the Malgary backend, so the
  portal (feature 002) uses them without migration. The NetBird dashboard is operator
  view-only. Changes made there are drift, and the reconciler reports or reverts them.
- **FR-006**: Voice and video call traffic MUST NOT pass through the VPN, whatever the routing
  rule. If the network layer cannot exclude destinations from a full tunnel, "full tunnel" MUST
  be implemented as all address ranges except the call ranges.

**Egress IP and failover**

- **FR-007**: All VPN traffic leaving for in-scope destinations MUST appear from one reserved
  egress IP, in the New York region.
- **FR-008**: The egress IP MUST stay the same when the active gateway fails, is replaced or is
  resized.
- **FR-009**: Failover from the active to the standby gateway MUST happen automatically,
  without operator action, and complete within 60 seconds.
- **FR-010**: Only one gateway MAY hold the egress IP at any time.
- **FR-011**: The egress IP MUST be registered with the PTA before carrying pilot traffic.

**Tenants and provisioning**

- **FR-012**: Each tenant MUST have its own users, devices, groups, policies and audit trail,
  isolated from every other tenant.
- **FR-013**: Each tenant MUST be able to use a different company identity provider. Users of
  Malgary-managed accounts MUST be onboarded by email invitation, for any email address the
  tenant imports, and MUST set up MFA at first sign-in. The sign-in page MUST be Malgary-branded and never NetBird's.
- **FR-014**: Tenants, their identity-provider link, seat limit and default policy MUST be
  created through a repeatable provisioning step, not manual dashboard edits.
- **FR-015**: Provisioning MUST be safe to run repeatedly with the same input (no duplicates).
- **FR-016**: Adding or enabling a user beyond a tenant's seat limit MUST be refused, and the
  operator alerted. A seat is a user; each user's devices don't count separately.
- **FR-016a**: A tenant's seat limit and entitlements MUST come from its subscription to a
  specific plan version, never from a value set directly on the tenant. Tenant #1 MUST be on an
  enterprise plan version (e.g. $4 per seat, monthly), created and recorded the same way any
  future plan will be.
- **FR-016b**: Plans MUST be versioned. Publishing a change creates a new version, and existing
  subscriptions keep their version until explicitly migrated. Every version and subscription
  change MUST be kept as history.
- **FR-016c**: Payments in the pilot MUST be manual: an owner records an invoice and its payment
  (amount, date, reference). The record MUST be provider-neutral, so gateways can be added later
  without changing existing records.
- **FR-017**: The pilot MUST include one test tenant with a separate identity provider, used
  to prove isolation and per-tenant sign-in.

**Device posture**

- **FR-018**: Each tenant MUST be able to require a minimum operating-system version and a
  minimum app version. Non-compliant devices are blocked from protected destinations until
  fixed.

**Onboarding**

- **FR-019**: A setup page on the Malgary domain MUST give, for each platform: the official
  download link, the steps (or one command, on desktop) to point the app at the Malgary
  service, and how to confirm the connection works.
- **FR-020**: The setup page MUST show users how to tell they are on the Malgary service rather
  than NetBird's own cloud.

**Operations**

- **FR-021**: Every service MUST be reached through a stable DNS name per tenant, cell and role
  (e.g. `<tenant>.vpn.<domain>` for the tenant's management URL and setup page,
  `gw-nyc-1a.c01.<domain>` for a gateway), never a raw server address.
- **FR-022**: The whole deployment MUST be reproducible from the repository plus the latest
  backup, with no manual steps beyond supplying secrets.
- **FR-023**: Changing server sizes or user capacity MUST require only configuration changes.
- **FR-024**: The operator MUST be alerted within 2 minutes when any server or core service is
  down, when a failover happens, or when a gateway passes 50% CPU at peak.
- **FR-025**: Tenant, user and policy data MUST be backed up daily to storage outside the
  region, and kept for at least 30 days.
- **FR-026**: Every provisioning action, policy change and operator action MUST be written to
  an append-only audit log, recording who, what, when and which tenant.
- **FR-027**: The service MUST NOT record the content of user traffic.
- **FR-029**: Owner and operator accounts MUST use MFA. Production SSH MUST be reachable only
  through the operator VPN or a bastion, never directly from the internet.
- **FR-030**: Moving a tenant to another control host MUST have a documented procedure,
  rehearsed in staging during the pilot.

**Identity-layer decision**

- **FR-028**: The pilot MUST compare at least two identity-layer options (NetBird's built-in
  identity layer and a dedicated multi-organization identity service) against FR-012, FR-013
  and FR-003, and record the decision before user 11 joins.

### Key Entities

- **Cell**: A set of shared hosts in one region (control hosts and gateway pairs) that runs one
  NetBird stack per tenant (the pilot has one, `c01`). Has a region and a capacity limit.
- **Tenant**: A customer company. Belongs to exactly one cell. Has an identity-provider link,
  seat limit, default policy, posture rules and audit trail.
- **User**: A person in a tenant, identified by their company account. Uses one seat.
- **Device**: A laptop or phone registered by a user. Has posture status.
- **Plan / Plan version**: An owner-defined offer (public or enterprise). Each published
  version is immutable: price per seat, currency, period, seat bounds, entitlements.
- **Subscription**: A tenant's agreement to one plan version, with seat quantity, status and
  payment provider (manual in the pilot). Its history of changes is kept.
- **Invoice / Payment**: An immutable, numbered invoice for a subscription period, and the
  payments recorded against it, each tagged with its provider.
- **Routing rule**: Which traffic goes through the VPN (full tunnel or a destination list),
  applied to a tenant, a group or a user, with precedence user > group > tenant.
- **Group**: A named set of users in a tenant (e.g. a client team), used for access policies
  and routing rules.
- **Gateway pair**: Two gateway servers (active and standby) sharing one egress IP, in one
  region.
- **Egress IP**: The fixed public address that tenant traffic leaves from. Registered with
  the PTA and allowlisted by customers.
- **Audit record**: An append-only entry: actor, action, target, tenant, time.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: At least 9 of 10 pilot users connect using only the setup page, with no help from
  the operator, in under 10 minutes each.
- **SC-002**: In 5 failover tests, traffic resumes within 60 seconds every time and the egress IP
  is unchanged every time.
- **SC-003**: Over 3 to 4 weeks of pilot use, VPN availability is at least 99.9% (under 45
  minutes of total downtime a month).
- **SC-004**: No cross-tenant sign-in or data access succeeds in any isolation test.
- **SC-005**: A non-compliant device is blocked within 1 minute of connecting, and allowed
  within 1 minute of becoming compliant.
- **SC-006**: Growing from 10 to 50 users needs no change other than configuration values, and
  no pilot user needs to reconfigure their app.
- **SC-007**: A full rebuild in staging from the repository plus the latest backup completes in
  under 4 hours, with no data loss beyond the last daily backup.
- **SC-008**: Pilot users rate their experience as equal to or better than Perimeter 81 (at
  least 8 of 10 users).
- **SC-009**: Monthly running cost of the pilot stays within about $110 (see research R11; the
  cost plan's $71 assumed one shared control plane and no identity service).

## Assumptions

- **Fixed by the constitution and business plan** (not re-decided here): NetBird open source as
  the VPN; DigitalOcean New York for the pilot; a primary + standby gateway pair sharing a
  reserved IP; PostgreSQL as the management database from day one; infrastructure and host
  setup defined as code in reusable `cell`, `gateway-pair` and `relay` modules; per-cell DNS
  names. The plan phase turns these into a design.
- Each tenant has its own NetBird stack (Constitution Principle V, research R1). In the pilot all
  tenant stacks, the identity service and the database share one control host. Splitting them
  later is a configuration change (Principle IX).
- The test tenant uses a separate, free or trial identity-provider account owned by the
  operator, not a real customer.
- The site-to-site IPsec tunnel (UMD-USA), extra regions and the hot standby for management are
  out of scope for this pilot.
- Pilot users keep Perimeter 81 installed as a fallback during the pilot.
- The Malgary domain and DNS are controlled by the operator.
- Branding is Level 1 only: our domain, the company's own sign-in page, our logo where the
  dashboard allows it. The apps stay NetBird-branded.
- Posture rules for the pilot are minimum OS version and minimum app version. Other checks
  (disk encryption, antivirus) can be added later.
- The client portal (sign-up, subscription, user import, usage monitoring, self-service
  controls) is out of scope here and specified as feature 002. Everything this pilot builds
  (tenant model, provisioning, routing rules, audit log) MUST be usable by it unchanged.
- The back-office UI for plans, the portal's billing pages, automatic invoicing, and payment
  gateways are out of scope here. The pilot creates the commercial data model and records
  (plans, versions, subscriptions, manual invoices and payments) through provisioning, so the
  back-office and portal (002) build on it unchanged.
