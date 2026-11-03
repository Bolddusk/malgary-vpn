# Implementation Plan: Tier 1 Pilot (10 users, growing to 50)

**Branch**: `001-tier1-pilot` | **Date**: 2026-11-03 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/001-tier1-pilot/spec.md`

**Architecture diagram**: [docs/architecture-overview.html](../../docs/architecture-overview.html)
(system map of this plan; keep it in step with this file)

## Summary

Stand up cell `c01` in DigitalOcean New York for 10 pilot users of tenant #1 (onedge, Google
sign-in) plus an Entra test tenant. The pilot then grows to 50 by configuration only.

Research found that self-hosted NetBird can't isolate several accounts on one stack. So every
tenant gets its **own NetBird stack** (one `netbird-server` container plus a Postgres database),
running on a shared control host. Each tenant also gets a **routing-peer container on each
gateway** of a primary/standby pair. All tenants leave through the pair's reserved IP, and
keepalived moves that IP on failure.

**Zitadel** is the branded, multi-organization identity layer. Each tenant is one
organization, used as the only connector of that tenant's NetBird embedded IdP. This is
provisional until pilot test P-3.

A **Go backend** is the system of record for placement, users, routing rules, plans, plan
versions, subscriptions, manual invoices, payments and the audit log. It provisions and
reconciles stacks through the [tenant stack contract](contracts/tenant-stack.md) and exposes
the [API](contracts/backend-api.md) that the portal (002) will use unchanged. Infrastructure
is built with OpenTofu modules and Ansible.

## Technical Context

**Language/Version**: Go (current stable, 1.26+) for the backend and `malgaryctl`. HCL
(OpenTofu 1.10+) and Ansible (core 2.19+) for infrastructure. Bash only inside small host
scripts (keepalived notify).

**Primary Dependencies**:
- NetBird v0.82.0 (`netbirdio/netbird-server`, `netbirdio/netbird` client, dashboard)
- Zitadel v4.20+
- Traefik v3
- keepalived
- DigitalOcean provider v2.106
- Go libraries: NetBird REST client (`shared/management/client/rest`), Zitadel Go SDK, `pgx`,
  `sqlc`, `goose`, `chi`, `oapi-codegen`
- Grafana Alloy
- `age` (backup encryption)

**Storage**: PostgreSQL 17 on the control host. Databases: `malgary`, `zitadel`, `nb_<tenant>`
per tenant. DigitalOcean Spaces in AMS3 for backups, payment proofs and OpenTofu state.

**Testing**:
- Go: `go test` with unit tests; integration tests against real NetBird and Zitadel containers
  (testcontainers); contract tests generated from `openapi.yaml`.
- Infrastructure: `tofu validate`/`plan` in CI, `ansible-lint`, molecule for key roles.
- End to end: the [quickstart](quickstart.md) validation scenarios V1–V13, run in a staging
  cell first.

**Target Platform**: Ubuntu 24.04 LTS Droplets in DO NYC1 (control host and gateways). Clients:
official NetBird apps on Windows, macOS, Linux, iOS and Android.

**Project Type**: infrastructure plus a backend web service with a CLI client.

**Performance Goals**:
- Gateway failover under 60 seconds.
- Routing or posture changes reach devices within 5 minutes.
- Alerts within 2 minutes.
- Backend API p95 under 300 ms for tenant reads.

**Constraints**:
- Egress IP never changes.
- No user traffic content is logged.
- SMTP ports blocked on DO (use an email provider over HTTPS or port 2525).
- 3 reserved IPs per account by default.
- Pilot budget about $108 a month (research R11).

**Scale/Scope**:
- Pilot: 2 tenants, 10 users, then 50.
- Design scale: about 50 small tenant stacks per control host and 20+ cells, up to 100,000 users,
  reached by adding hosts and cells.

## Constitution Check

*GATE: checked before Phase 0 and again after Phase 1 design. Constitution v2.1.0.*

| Principle | How the design complies | Pre | Post |
| --- | --- | --- | --- |
| I. Tenant isolation | Separate NetBird stack and database per tenant. Zitadel organization per tenant. Backend scopes every query by tenant and returns `404` across tenants. Isolation tests V5. | ✅ | ✅ |
| II. Everything as code | OpenTofu modules and Ansible roles. Stacks rendered from the tenant directory. Secrets by reference only. | ✅ | ✅ |
| III. Stable egress IPs | Reserved IP per gateway pair. Anchor route on both gateways. keepalived moves the IP. Pair blocked until PTA-registered. | ✅ | ✅ |
| IV. No SPOF on data path | Primary and standby gateways, plus NetBird routing-peer HA via metrics. Management loss doesn't drop connected peers. Control host restore within 4 hours (V12). Single control host is accepted for the pilot (Complexity Tracking). | ✅ | ✅ |
| V. Cells, stack per tenant | Cell `c01` with tenant directory, placement and capacity in the backend from day one | ✅ | ✅ |
| VI. Upstream-first | Unmodified NetBird and Zitadel images. Integration only through APIs and config. | ✅ | ✅ |
| VII. Security and audit | Tenant-chosen sign-in (SSO, managed accounts with mandatory MFA, or both) via Zitadel. Owners and operators use MFA. SSH only from the VPC, reached through the `malgary-ops` tenant (R13). Append-only audit in the same transaction. Webhook table idempotent. No traffic content logged. | ✅ | ✅ |
| VIII. Cost discipline | Pilot cost stated (R11): $108 against the plan's $71, explained | ✅ | ✅ |
| IX. Grow by configuration | Postgres from day one. DNS names per tenant and role. Sizes and counts as module variables. The same contracts serve 10 or 100,000 users. | ✅ | ✅ |
| X. Portal is the product | Backend API is the only tenant-facing surface. NetBird dashboards bound to VPC or VPN only. Reconciler detects drift. | ✅ | ✅ |
| XI. Versioned plans and payments | Immutable plan versions, subscription event history, provider interface with `manual`, integer money, numbered invoices | ✅ | ✅ |

Result: **PASS**, with one justified deviation below.

## Project Structure

### Documentation (this feature)

```text
specs/001-tier1-pilot/
├── plan.md              # This file
├── research.md          # Phase 0
├── data-model.md        # Phase 1
├── quickstart.md        # Phase 1: stand-up and validation
├── contracts/
│   ├── backend-api.md
│   ├── tenant-definition.md
│   ├── tenant-stack.md
│   └── payment-provider.md
├── checklists/requirements.md
└── tasks.md             # Phase 2 (/speckit-tasks)
```

### Source Code (repository root)

```text
infra/
├── modules/
│   ├── cell/              # VPC, firewall, DNS records, Spaces buckets
│   ├── control-host/      # Droplet + volume for Postgres
│   ├── gateway-pair/      # 2 Droplets, reserved IP + assignment
│   └── relay/             # standalone relay (from Tier 2; unused in pilot)
├── envs/
│   ├── staging/
│   └── pilot/             # c01 sizes and counts: the only file changed to grow
└── ansible/
    ├── site.yml
    └── roles/             # base, postgres, traefik, zitadel, backend, alloy,
                           # gateway (anchor route, nftables NAT, keepalived),
                           # tenant-stack, routing-peer, backup

backend/
├── cmd/
│   ├── malgaryd/          # API server + reconciler
│   └── malgaryctl/        # operator/owner CLI
├── api/openapi.yaml
├── internal/
│   ├── tenancy/           # tenants, cells, placement, stack specs
│   ├── identity/          # Zitadel orgs, users, invitations
│   ├── netbird/           # per-stack client: users, groups, routes, posture, peers
│   ├── policy/            # routing-rule resolution, posture rules
│   ├── billing/           # plans, versions, subscriptions, invoices, payments
│   │   └── providers/manual/
│   ├── usage/             # peer status + WireGuard counters
│   ├── audit/
│   ├── reconcile/
│   └── store/             # sqlc queries, goose migrations
└── tests/
    ├── contract/
    ├── integration/       # testcontainers: Postgres, NetBird, Zitadel
    └── unit/

deploy/
├── plans/                 # plan definitions applied by malgaryctl
├── tenants/               # tenant definitions
└── setup-page/            # static per-tenant onboarding page template

docs/
├── runbooks/              # one per alert
└── decisions/             # ADRs (identity layer after P-3, etc.)
```

**Structure Decision**: three top-level areas.
- `infra/` holds the cell, host and gateway layout.
- `backend/` is the system of record and control loop, reused by the portal in 002.
- `deploy/` holds declarative business data (plans, tenants) and the onboarding page.

The portal will be added as `portal/` in 002 without moving anything.

## Phasing inside this feature

1. **Foundation**: OpenTofu modules, staging cell, Ansible base roles, Postgres, Traefik,
   Alloy and Grafana Cloud alerts, backups to AMS3.
2. **Gateway pair**: anchor route, nftables NAT, keepalived and the DO API notify script. Run
   P-1 here, before anything else depends on it.
3. **Backend core**: store and migrations, tenancy, audit, billing (plans, versions,
   subscriptions, manual invoices and payments), `malgaryctl`.
4. **Identity and stacks**: Zitadel organizations, tenant stack render and deploy, routing
   peers. Run P-2 and P-3, then write ADR-001 on the identity layer.
5. **Policy and usage**: routing-rule resolution, posture, usage sampling, reconciler. Run
   P-5.
6. **Onboarding and pilot**: setup page, PTA registration, invite 10 users, run V1–V13, record
   results.

## Complexity Tracking

| Deviation | Why needed | Simpler alternative rejected because |
| --- | --- | --- |
| One NetBird stack per tenant (more processes than one shared stack) | Self-hosted NetBird can't isolate accounts on one stack (R1) | Group-based separation in one account violates Principle I |
| Zitadel added as an extra component | Branded sign-in, per-tenant IdPs, email invitations (R4, Principle X) | The embedded IdP alone is NetBird-branded and has no invitations for arbitrary emails |
| Single control host in the pilot (Principle IV covers the data path, not the control plane) | Cost at 10 users | A second control host doubles pilot cost. Connected users are unaffected by its loss. Restore drill V12 bounds recovery to 4 hours. Revisit at Tier 2. |
