---

description: "Task list for feature 001: Tier 1 Pilot"
---

# Tasks: Tier 1 Pilot (10 users, growing to 50)

**Input**: Design documents from `/specs/001-tier1-pilot/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md),
[data-model.md](data-model.md), [contracts/](contracts/), [quickstart.md](quickstart.md)

**Tests**: Included only where the constitution or the spec requires them:
- cross-tenant isolation (Principle I)
- idempotency and webhook replay (FR-015, Principle VII)
- plan-version immutability (FR-016b)
- routing-rule resolution (FR-005)
- the quickstart validation scenarios V1–V13

**Organization**: Tasks are grouped by user story from spec.md so each can be built and
verified on its own.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependency on unfinished tasks)
- **[Story]**: User story the task belongs to (US1–US8)
- Paths are relative to the repository root. The Go module is `malgary/backend`, rooted at
  `backend/`.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Repository skeleton, toolchains and CI.

- [X] T001 Create the top-level layout from plan.md: `infra/modules/{cell,control-host,gateway-pair,relay}/`, `infra/envs/{staging,pilot}/`, `infra/ansible/roles/`, `backend/`, `deploy/{plans,tenants,setup-page}/`, `docs/{runbooks,decisions}/`, with a `.gitignore` that excludes `*.tfstate*`, `.env*`, `*.age` keys, `secrets/` and service-account JSON files.
- [X] T002 Initialize the Go module in `backend/go.mod` (module `malgary/backend`, current stable Go) with dependencies `github.com/jackc/pgx/v5`, `github.com/go-chi/chi/v5`, `github.com/pressly/goose/v3`, `github.com/oapi-codegen/oapi-codegen/v2`, `github.com/netbirdio/netbird/shared/management/client/rest` (pinned to v0.82.0), `github.com/zitadel/zitadel-go/v3`, `github.com/google/uuid`, `github.com/testcontainers/testcontainers-go`.
- [X] T003 [P] Add `backend/sqlc.yaml` (engine postgresql, queries `internal/store/queries`, schema `internal/store/migrations`, output `internal/store/db`) and `backend/Makefile` targets `generate`, `lint`, `test`, `test-integration`, `build`.
- [X] T004 [P] Configure `golangci-lint` in `backend/.golangci.yml` (govet, staticcheck, errcheck, gosec, revive).
- [X] T005 [P] Pin OpenTofu ≥1.10 and provider `digitalocean/digitalocean` `~> 2.106` in `infra/versions.tf`, and remote state in a Spaces bucket in `infra/envs/pilot/backend.tf` and `infra/envs/staging/backend.tf`.
- [X] T006 [P] Create `infra/ansible/ansible.cfg`, `infra/ansible/requirements.yml` (community.general, community.docker, community.postgresql, ansible.posix) and inventory plugin config `infra/ansible/inventory/digitalocean.yml` (tag-based groups: `control`, `gateway`, `cell_c01`).
- [X] T007 [P] Add CI workflow `.github/workflows/ci.yml`: `tofu fmt -check` and `tofu validate` for every env, `ansible-lint`, `golangci-lint`, `go test ./...`, `sqlc diff`, and an OpenAPI lint of `backend/api/openapi.yaml`.
- [X] T008 [P] Add `docs/decisions/0000-record-architecture-decisions.md` and `docs/decisions/0001-one-netbird-stack-per-tenant.md`, summarizing research R1 and constitution v2.0.0 Principle V.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The cell, the control host, the backend core and the tenant-stack pipeline that
every story needs.

**⚠️ CRITICAL**: No user story work starts until this phase is complete.

### Infrastructure

- [X] T009 Implement `infra/modules/cell/`: `digitalocean_vpc` (variable `ip_range`), `digitalocean_firewall` for control hosts (allow 443/tcp and 80/tcp from anywhere, everything from the VPC, and **no SSH from the internet**) and gateways (allow `var.wg_port_range` UDP from anywhere, everything from the VPC, no SSH from the internet). SSH is reached only from the VPC through the `malgary-ops` tenant (research R13, FR-029). Break-glass access is the DO Droplet console, `digitalocean_record` set for `*.vpn.<domain>` → control host, Spaces buckets `malgary-<cell>-backups` in region `var.backup_region` (default `ams3`) and `malgary-<cell>-artifacts`. Outputs: `vpc_id`, `firewall_ids`, bucket names.
- [X] T010 [P] Implement `infra/modules/control-host/`: `digitalocean_droplet` (Ubuntu 24.04, `var.size` default `s-4vcpu-8gb`, `backups = true`, `vpc_uuid`, tags `control`, `cell_<code>`), a `digitalocean_volume` for PostgreSQL data (`var.db_volume_gb`, default 50) and a DNS record `ctl-<n>.<cell>.<domain>`.
- [X] T011 [P] Implement `infra/modules/gateway-pair/`: two `digitalocean_droplet` (`var.size` default `s-2vcpu-2gb`, tags `gateway`, `pair_<name>`, `cell_<code>`), `digitalocean_reserved_ip` in the same region, `digitalocean_reserved_ip_assignment` to the primary (with `lifecycle { ignore_changes = [droplet_id] }`, because keepalived moves it at runtime), and DNS records `<name>a.<cell>.<domain>` and `<name>b.<cell>.<domain>`. Outputs: `egress_ip`, droplet IDs.
- [X] T012 [P] Create `infra/modules/relay/` as a documented stub (variables and outputs only, no resources by default; `count = var.enabled ? 1 : 0`) so Tier 2 is a variable change.
- [X] T013 Compose the envs: `infra/envs/staging/main.tf` and `infra/envs/pilot/main.tf` call `cell`, `control-host` and `gateway-pair` (`gw-nyc-1`) for cell `c01`, region `nyc1`. All sizes and counts live in `infra/envs/<env>/terraform.tfvars`, the only file edited to grow (Principle IX).

### Host configuration

- [X] T014 Ansible role `infra/ansible/roles/base/`: unattended security upgrades, admin users with SSH keys only, nftables default-deny (mirrors the cloud firewall), chrony, Docker Engine plus the compose plugin, node exporter via Alloy, and `/etc/malgary/host.json` (cell, role, name).
- [X] T015 [P] Ansible role `infra/ansible/roles/postgres/`: PostgreSQL 17 on the attached volume, listening on the VPC address only, `scram-sha-256`, databases and roles `malgary`, `zitadel`, and a `pg_hba` include file managed per tenant (`nb_<slug>`), tuned for 8 GB RAM.
- [X] T016 [P] Ansible role `infra/ansible/roles/traefik/`: Traefik v3 in Docker with the Let's Encrypt HTTP-01 resolver, a file provider directory `/etc/traefik/dynamic/` where each tenant stack drops a router file, HTTP/2 and gRPC passthrough to NetBird (h2c), and dashboards bound to the VPC only.
- [X] T017 [P] Ansible role `infra/ansible/roles/zitadel/`: Zitadel v4.20+ in Docker on PostgreSQL database `zitadel`, external domain `id.<domain>` through Traefik, a first instance admin from an `age`-encrypted vars file, an SMTP or HTTP notification provider set from vars (research R9: port 2525 or HTTPS API, never 25/465/587), a machine user `malgary-backend` with IAM_OWNER whose key is written to the secret store, and a login policy on the admin organization that requires MFA for every owner and operator (FR-029).
- [X] T019 [P] Ansible role `infra/ansible/roles/alloy/`: Grafana Alloy collecting node, Docker, PostgreSQL and Traefik metrics plus journald logs, with `remote_write` to Grafana Cloud from encrypted vars. Labels: `cell`, `role`, `host`, `tenant` (where applicable).
- [X] T020 [P] Ansible role `infra/ansible/roles/backup/`: a nightly systemd timer that runs `pg_dump -Fc` per database plus `/etc/malgary` and `/opt/malgary/stacks`, encrypts with `age` to the backup recipient, uploads to the `malgary-<cell>-backups` bucket under `YYYY/MM/DD/<host>/`, applies a 30-day lifecycle (FR-025), and pushes a success metric to Alloy.
- [X] T021 Ansible role `infra/ansible/roles/gateway/` (base data path only; failover is US2): read the anchor gateway from `http://169.254.169.254/metadata/v1/interfaces/public/0/anchor_ipv4/gateway`, set it as the persistent default route via netplan with cloud-init network config disabled, `net.ipv4.ip_forward=1`, nftables masquerade for `wt-*` interfaces out of `eth0`, and MSS clamping.
- [X] T022 Playbook `infra/ansible/site.yml` applying `base` to all hosts; `postgres`, `traefik`, `zitadel`, `alloy`, `backup` and `backend` to `control`; `gateway`, `alloy` to `gateway`.
- [X] T023 Secret store convention: `infra/ansible/group_vars/all/secrets.age.yml` (age-encrypted, decrypted at run time by a vars plugin or wrapper `infra/ansible/bin/run`), plus a resolver for `secret://` references used by the backend and the deployer (backed by a root-only file `/etc/malgary/secrets/` on the control host in the pilot). Document it in `docs/runbooks/secrets.md`.

### Backend core

- [X] T024 Create migration `backend/internal/store/migrations/0001_core.sql` for the data-model.md tables `cell`, `host`, `gateway_pair`, `tenant`, `tenant_stack`, `routing_peer`, `identity_link`, `tenant_user`, `"group"`, `group_member` and `audit_record`. Constraints verbatim from data-model.md:
  - `tenant.slug` "DNS-safe, unique"
  - `tenant.status` enum `provisioning, active, suspended, offboarding, deleted`
  - `gateway_pair.egress_ip` unique
  - `routing_peer` unique (`host_id`, `wg_port`), (`host_id`, `wg_interface`), (`tenant_id`, `host_id`)
  - `tenant_user.email` citext, "unique per tenant"
  - `"group".name` "unique per tenant"
- [X] T025 In the same migration, make `audit_record` append-only: `REVOKE UPDATE, DELETE, TRUNCATE ON audit_record FROM malgary_app`, with application role `malgary_app` separate from migration owner `malgary_owner`.
- [X] T026 [P] Implement config loading in `backend/internal/config/config.go`: DB DSN, Zitadel issuer, machine key ref, base domain, secret dir and listen address, from env with validation.
- [X] T027 [P] Implement the secret resolver in `backend/internal/secrets/resolver.go` for `secret://` refs (file backend). Never log values.
- [X] T028 Implement the store layer in `backend/internal/store/` (pgx pool, transaction helper `WithTx(ctx, fn)`, sqlc queries for core tables in `backend/internal/store/queries/core.sql`).
- [X] T029 Implement audit in `backend/internal/audit/audit.go`: `Record(ctx, tx, Entry)` writes inside the caller's transaction, with fields `actor_type, actor_id, tenant_id, action, target_type, target_id, before, after, request_id, source_ip` (FR-026).
- [X] T030 Implement the HTTP server in `backend/cmd/malgaryd/main.go` and `backend/internal/httpapi/server.go`: chi router, request ID, structured JSON logging (no bodies, no secrets), `application/problem+json` errors with fixed codes from contracts/backend-api.md, `Idempotency-Key` middleware backed by table `idempotency_key` (add it to migration 0001), and `ETag`/`If-Match` helpers.
- [X] T031 Implement authentication in `backend/internal/httpapi/auth.go`: validate Zitadel-issued JWTs (JWKS cache), map claims to `actor_type` (`owner`, `operator`, `tenant_admin`, `deployer`) and `tenant_id`. Tenant scoping middleware returns `404` (not `403`) when a `tenant_admin` addresses another tenant (Principle X).
- [X] T032 Write `backend/api/openapi.yaml` (OpenAPI 3.1) covering every route in contracts/backend-api.md, then generate server interfaces with oapi-codegen into `backend/internal/httpapi/gen/`.
- [X] T033 Implement the `malgaryctl` skeleton in `backend/cmd/malgaryctl/main.go`: OIDC device-code login against Zitadel, a generated API client, and subcommands `tenant`, `users`, `plan`, `subscription`, `invoice`, `payment`, `stack` as stubs.

- [X] T114 Operator access tenant: once T042 works, create `deploy/tenants/malgary-ops.yaml` (operators only, `signInMethods: [sso]` via the admin org, MFA required, a route to the cell VPC range `10.10.0.0/16`). Until it exists, operators use the DO Droplet console. Document it in `docs/runbooks/operator-access.md` (FR-029, research R13).

### Tenant stack pipeline (Principle V)

- [X] T034 Implement placement in `backend/internal/tenancy/placement.go`: choose the cell (explicit or least-loaded `active` cell) and control host (most free capacity under `max_users`). Allocate a `wg_interface` (`wt-<slug>` truncated to 15 chars) and a `wg_port` unique per gateway host from the range 51821–52820.
- [X] T035 Implement the StackSpec renderer in `backend/internal/tenancy/stackspec.go` exactly per [contracts/tenant-stack.md](contracts/tenant-stack.md): a deterministic JSON field order, a `hash` equal to sha256 of the canonical JSON, and `*Ref` fields only (no secret values).
- [X] T036 Implement the NetBird per-stack client factory in `backend/internal/netbird/client.go`: one REST client per tenant from `management_url` plus a service-user PAT resolved from `service_token_ref`, with timeouts, retries on 5xx and a rate limit.
- [X] T037 Implement the Zitadel client in `backend/internal/identity/zitadel.go`: create an organization, create the per-tenant OIDC app (Web, code flow, redirect URIs from the tenant's NetBird embedded IdP), and get the org. Use a per-tenant OIDC app, not project grants (avoids zitadel#11869).
- [X] T038 Ansible role `infra/ansible/roles/tenant_stack/`: input is a StackSpec JSON file. It renders `/opt/malgary/stacks/<slug>/compose.yaml` running `netbirdio/netbird-server:<netbirdVersion>` (config.yaml with `server.store.engine: postgres`, DSN from secret, embedded IdP with exactly one connector: Zitadel issuer, client and org scope) and `netbirdio/dashboard` bound to the VPC IP only. It creates DB `nb_<slug>` and its role, drops a Traefik router file for `managementDomain`, runs `docker compose up -d`, waits for health, and rolls back to the previous compose file on failure.
- [X] T039 Ansible role `infra/ansible/roles/routing_peer/`: for each StackSpec `routingPeers` entry on this host, run container `netbirdio/netbird:<version>` with `NB_SETUP_KEY` from secret, `NB_MANAGEMENT_URL`, interface name `wgInterface`, `--wireguard-port wgPort`, `network_mode: host`, `cap_add: NET_ADMIN, SYS_MODULE`, a persistent config volume per tenant, and restart `unless-stopped`.
- [X] T040 Implement the deployer bridge in `backend/internal/tenancy/deployer.go`: write the StackSpec to `/opt/malgary/specs/<slug>.json`, invoke `infra/ansible/bin/run tenant-stack.yml -e spec=<path>` (pilot deployer), and record `applied_hash`/`status` from `POST /v1/tenants/{id}/stack:report`. Keep it behind interface `Deployer`, so it can move to Nomad or Kubernetes later.
- [X] T041 Implement stack bootstrap in `backend/internal/tenancy/bootstrap.go`: after the first deploy, create the NetBird service user and PAT via the stack's first-run admin, store the PAT under `secret://tenants/<slug>/netbird-pat`, create a reusable setup key for routing peers (`secret://tenants/<slug>/gw`), and create system groups `all-users` and `gateways`.
- [X] T042 Implement `POST /v1/tenants` (minimal create: definition → tenant + identity link + Zitadel org + stack + routing peers, status `provisioning` → `active` when the stack reports healthy) in `backend/internal/httpapi/tenants.go`, with audit records. Wire `malgaryctl tenant apply -f`.
- [X] T043 [P] Integration test `backend/tests/integration/stack_lifecycle_test.go`: with testcontainers (Postgres, Zitadel, netbird-server v0.82.0), create a tenant, check StackSpec hash stability, check that a deploy with an identical hash is a no-op, and that management API calls succeed with the PAT.

**Checkpoint**: A tenant can be created and gets its own running NetBird stack and routing peers on the gateway pair.

---

## Phase 3: User Story 1 - Pilot user connects with company sign-in (Priority: P1) 🎯 MVP

**Goal**: A tenant #1 user installs the official app, signs in with Google through Malgary sign-in, and browses from the reserved IP.

**Independent Test**: Quickstart V1 and V2: a new pilot user given only the setup page link connects in under 10 minutes, and "what is my IP" shows the reserved IP.

- [X] T044 [US1] Configure tenant #1 identity in `backend/internal/identity/upstream.go`: add a Google IdP to the tenant's Zitadel org (from `identity.upstreamIdps`), set the org login policy from `identity.signInMethods` (`sso` only for tenant #1), require MFA (`mfaRequired: true`), and restrict sign-in to `allowedDomains`. When `allowedDomains` is empty, only invited users or `allowedEmails` can sign in (FR-002, FR-003). Expose it via `PUT /v1/tenants/{id}/identity` in `backend/internal/httpapi/identity.go`.
- [X] T045 [P] [US1] Zitadel org branding in `backend/internal/identity/branding.go`: Malgary logo, colours and the "Malgary" name for each org's login (Principle X). Brand assets live in `deploy/branding/`.
- [X] T046 [US1] Configure the default full-tunnel route in `backend/internal/netbird/routes.go`: create a NetBird network route `0.0.0.0/0` with HA routing peers (metric 100 primary, 200 standby) distributed to group `route:full`, and add every tenant #1 user to it (FR-005c). Apply the tenant `callExclusions` as more-specific routes excluded from the tunnel (FR-006). Run pilot test P-7 (research R12): try exit-node exclusions first; if NetBird can't exclude, implement the fallback in `backend/internal/policy/complement.go` (all public IPv4 minus the call ranges as a CIDR set) and record the client route count and the result in `specs/001-tier1-pilot/pilot-results.md`.
- [X] T047 [US1] Implement user invitation in `backend/internal/identity/users.go` and `POST /v1/tenants/{id}/users:import` (minimal: create the Zitadel user in the org and send an invitation). Group sync: a Zitadel Action adds a `groups` claim from user metadata, so NetBird puts users in `all-users` and their routing group on first login.
- [X] T048 [P] [US1] Build the setup page template in `deploy/setup-page/` (static HTML, Malgary-branded). For each tenant it renders: download links (netbird.io/download, App Store, Google Play); the desktop one-liner `netbird up --management-url https://<slug>.vpn.<domain>:443`; mobile "Change server" steps; how to check you're on Malgary and not NetBird cloud (FR-020); a "what's my IP" check; and fixes for a wrong clock. Served by Traefik at `https://<slug>.vpn.<domain>/setup`.
- [X] T049 [US1] Have the tenant-stack role publish the rendered setup page for each tenant in `infra/ansible/roles/tenant_stack/templates/setup-page.html.j2`.
- [X] T050 [US1] Add a PTA gate in `backend/internal/tenancy/placement.go`: refuse to place routing peers on a gateway pair whose `pta_registered_at` is null (FR-011). Add `malgaryctl gateway mark-pta-registered <pair> --date --ref`.
- [X] T051 [US1] Write `docs/runbooks/onboarding-support.md`: the common user problems (app left on NetBird cloud, wrong clock, personal account refused) and their fixes.
- [ ] T052 [US1] Run quickstart V1 and V2 in staging with 2 test users, covering Windows, macOS, Linux and Android clients (FR-001), and record the results in `specs/001-tier1-pilot/pilot-results.md`.

**Checkpoint**: The MVP. Tenant #1 users can connect with Google sign-in and a fixed egress IP.

---

## Phase 4: User Story 2 - Gateway failure is invisible to clients' allowlists (Priority: P1)

**Goal**: Automatic failover to the standby within 60 seconds, with the same egress IP.

**Independent Test**: Quickstart V3 and V4, plus pilot test P-1: power off gw-a 5 times; traffic resumes in under 60 seconds and the IP is unchanged every time.

- [X] T053 [US2] Add keepalived to `infra/ansible/roles/gateway/`: unicast VRRP over the VPC (peers from inventory, no multicast), `nopreempt` on both nodes so a returning node stays standby (FR-010), a health script `/usr/local/bin/gw-health` (checks the default route via the anchor gateway, nftables loaded, and at least one routing-peer container healthy), and nftables rules allowing VRRP (proto 112) between the pair's VPC IPs only.
- [X] T054 [US2] Write the notify script `infra/ansible/roles/gateway/files/reserved-ip-notify`. On `MASTER`, it calls `POST https://api.digitalocean.com/v2/reserved_ips/<ip>/actions` with `{"type":"assign","droplet_id":<self>}` using a token scoped to reserved IPs (from secret), retries with backoff for up to 30 seconds, and logs and pushes a `gateway_failover_total` metric. It is idempotent if the IP is already assigned to self.
- [X] T055 [P] [US2] Add a split-brain guard in `infra/ansible/roles/gateway/files/gw-health`: if the DO API reports the reserved IP on the peer and keepalived is MASTER locally, demote and alert (FR-010).
- [X] T056 [P] [US2] Probe tool `infra/tools/egress-probe/` (small Go or bash script): every second, record the egress IP seen by an external echo service and connection success through the VPN, writing CSV for P-1 and V3 measurements.
- [X] T057 [US2] Alert rules in `infra/observability/alerts/gateway.yaml` (Grafana Cloud provisioning): failover event, both gateways unhealthy, reserved IP unassigned, gateway CPU >50% at peak (FR-024). Each links to a runbook.
- [X] T058 [P] [US2] Write `docs/runbooks/gateway-failover.md` and `docs/runbooks/gateway-both-down.md`.
- [ ] T059 [US2] Run pilot test P-1 and quickstart V3 (5 runs) and V4 in staging. Record the timings and egress-IP evidence in `specs/001-tier1-pilot/pilot-results.md`. If over 60 seconds, tune `advert_int`, health-check intervals and NetBird route metrics, then repeat.

**Checkpoint**: The gateway pair survives the loss of either node with no IP change.

---

## Phase 5: User Story 3 - Each tenant signs in with its own identity provider (Priority: P1)

**Goal**: The test tenant with Microsoft Entra is fully isolated from tenant #1. The identity-layer decision is recorded.

**Independent Test**: Quickstart V5 and pilot test P-3. Cross-tenant sign-in and API access all fail; ADR-002 is written.

- [X] T060 [P] [US3] Integration test `backend/tests/integration/tenant_isolation_test.go`: two tenants. A tenant A admin token gets `404` on every `/v1/tenants/{B}/…` route (iterate all routes from the OpenAPI spec). A tenant A NetBird PAT can't read tenant B's stack. A user from org A can't complete OIDC login against tenant B's stack.
- [X] T061 [US3] Add Entra support to `backend/internal/identity/upstream.go`: a Microsoft IdP with tenant ID, client ID and secret ref per org, and the login policy restricted to that IdP.
- [X] T062 [US3] Create `deploy/tenants/testco.yaml` (Entra test tenant, 3 test users, quantity 5, on a test enterprise plan) and apply it in staging.
- [ ] T063 [US3] Run pilot test P-3:
  - (a) Zitadel as the only connector: does the NetBird login skip the Dex chooser?
  - (b) group claim sync through Dex to NetBird
  - (c) baseline comparison: the embedded IdP with a direct Google connector

  Record the results in `specs/001-tier1-pilot/pilot-results.md`.
- [ ] T064 [US3] Write `docs/decisions/0002-identity-layer.md` with the final choice and evidence from T063, before user 11 joins (FR-028). If Zitadel is rejected, list the tasks in US1 and US3 to redo.
- [X] T109 [US3] Implement tenant-chosen sign-in methods in `backend/internal/identity/signin.go`: map `identity.signInMethods` (`sso`, `managed`, or both) to the Zitadel org login policy. For `managed`: allow username/password, force MFA setup at first sign-in, and send invitations via `CreateInviteCode`. Never allow password-only sign-in. Validate per [contracts/tenant-definition.md](contracts/tenant-definition.md) and expose it in `PUT /v1/tenants/{id}/identity` (FR-002, FR-013).
- [ ] T110 [P] [US3] Integration test `backend/tests/integration/signin_methods_test.go`: a tenant with `[sso]` refuses password sign-in; a tenant with `[managed]` refuses sign-in without MFA; a tenant with both allows either method.
- [ ] T065 [US3] Run quickstart V5 and V14 in staging and record the results.

**Checkpoint**: The per-tenant identity and isolation decisions are proven.

---

## Phase 6: User Story 4 - Operator provisions a tenant and its users without manual dashboard work (Priority: P2)

**Goal**: An idempotent tenant definition apply, seat limits from subscriptions, versioned plans and manual invoices and payments (Principle XI).

**Independent Test**: Quickstart V8, V9 and V10. Re-applying a definition changes nothing; the 11th user is refused; version 2 of a plan leaves onedge on version 1; a manual payment marks an invoice paid.

### Tests

- [X] T066 [P] [US4] Contract tests in `backend/tests/contract/tenants_test.go`: `POST /v1/tenants` with the same `Idempotency-Key` returns the same result. Applying the same definition twice yields no new rows and no new audit records (FR-015). Unknown fields are rejected.
- [X] T067 [P] [US4] Unit tests in `backend/internal/billing/planversion_test.go`: a published version can't be modified ("A database trigger rejects any update to a published version except `status → superseded`"), and publishing creates the next `version` number per plan.
- [X] T068 [P] [US4] Contract test in `backend/tests/contract/seats_test.go`: importing users beyond `subscription.quantity` returns `seat_limit_reached` and fires an alert metric (FR-016).

### Implementation

- [X] T069 [US4] Migration `backend/internal/store/migrations/0002_billing.sql` with tables `plan`, `plan_version`, `subscription`, `subscription_event`, `invoice`, `credit_note`, `payment`, `payment_provider_event`. Constraints verbatim from data-model.md:
  - `plan.visibility` enum `public, enterprise`, with `enterprise_tenant_id` "required when `visibility = enterprise`"
  - `plan_version.status` `draft → published → superseded`, plus the immutability trigger
  - `subscription.status` enum `trialing, active, past_due, suspended, cancelled`, "one non-terminal subscription per tenant" (partial unique index)
  - money as `amount_minor bigint` plus `currency char(3)`
  - `invoice.number` sequential per issuer (`MV-YYYY-NNNNNN`, via a sequence)
  - `payment_provider_event` unique (`provider`, `provider_event_id`)
  - `subscription_event` and `payment` append-only (revoke UPDATE/DELETE)
- [X] T070 [US4] Implement plans in `backend/internal/billing/plans.go`: create a plan, draft a version (price per seat, currency, period, min and max seats, entitlements JSON, terms), and publish (the previous published version becomes `superseded`; existing subscriptions are untouched). Endpoints `GET/POST /v1/plans`, `POST /v1/plans/{id}/versions`, `POST /v1/plans/{id}/versions/{v}:publish`.
- [X] T071 [US4] Implement subscriptions in `backend/internal/billing/subscriptions.go`: subscribe a tenant to a pinned plan version (enterprise plans only for their tenant, quantity within min and max seats); `:change` for quantity or explicit migration to another version with a required `reason`; every change writes a `subscription_event` and an audit record (FR-016a, FR-016b). Endpoints per contracts/backend-api.md.
- [X] T072 [US4] Implement entitlements in `backend/internal/billing/entitlements.go`: `Entitled(ctx, tenantID, key)`, read from the subscription's plan version. No plan names or prices in code (Principle XI).
- [X] T073 [US4] Implement the payment provider interface in `backend/internal/billing/providers/provider.go` and the `manual` provider in `backend/internal/billing/providers/manual/manual.go`, exactly per [contracts/payment-provider.md](contracts/payment-provider.md). Add the webhook endpoint `POST /v1/providers/{provider}/webhooks`, which stores `payment_provider_event` idempotently and returns `404` for `manual`.
- [X] T074 [US4] Implement invoices and payments in `backend/internal/billing/invoices.go`: draft an invoice for a subscription period (lines from the plan version × quantity), issue it (assign a number, then immutable), and `POST /v1/invoices/{id}/payments` (amount, date, reference, proof upload to the `malgary-<cell>-artifacts` bucket); the invoice becomes `paid` when payments ≥ total. Corrections only via credit notes (FR-016c).
- [X] T075 [US4] Enforce seat limits in `backend/internal/tenancy/seats.go`: count `tenant_user.seat_held = true` against `subscription.quantity` on import and enable; raise `seat_limit_reached` and the `tenant_seat_limit_hit_total` metric.
- [X] T076 [US4] Make tenant definition apply idempotent in `backend/internal/tenancy/apply.go`: parse and validate per [contracts/tenant-definition.md](contracts/tenant-definition.md) (validation rules verbatim: published `planVersion`, quantity within the version's seat bounds, gateway pair in the tenant's cell, `dedicated_ip` entitlement for dedicated pairs, `allowedDomains` or `allowedEmails` non-empty, unknown fields rejected), diff against current state, and apply only the differences.
- [X] T077 [US4] Build out `malgaryctl` in `backend/cmd/malgaryctl/`: `plan apply -f`, `plan publish`, `tenant apply -f`, `subscription change`, `invoice draft|issue`, `payment record --proof <file>`, `users import <slug> <csv>`.
- [X] T078 [P] [US4] Add declarative data (applied through the API by `malgaryctl`, not compiled into code; Principle XI, until the back-office in 002) `deploy/plans/ent-onedge.yaml` (enterprise, onedge, $4/seat monthly, `price_per_seat_minor: 400`, `currency: USD`, entitlements `posture_checks: true, usage_reports: basic, dedicated_ip: false`) and `deploy/tenants/onedge.yaml` (Google, `onedge.co`, quantity 10, `gw-nyc-1`, full tunnel default).
- [ ] T079 [US4] Run quickstart V8, V9 and V10 in staging and record the results.

**Checkpoint**: The commercial model and idempotent provisioning work. The pilot runs on plan records, not hand edits.

---

## Phase 7: User Story 5 - Admin controls which traffic uses the VPN, per group or per user (Priority: P2)

**Goal**: Tenant, group and user routing rules with "most specific wins" resolution, applied through single routing groups in NetBird.

**Independent Test**: Quickstart V6: group A full tunnel, group B destinations only, and a user override in B behave as set within 5 minutes; changes are audited; an empty list is rejected.

- [X] T080 [P] [US5] Unit tests in `backend/internal/policy/resolve_test.go` for the data-model.md resolution rules:
  - user rule wins
  - otherwise the union of group rules, where "`full_tunnel` wins" and conflicts are reported
  - otherwise the tenant rule
  - a `destinations` mode with an empty list is invalid
- [X] T081 [US5] Migration `backend/internal/store/migrations/0003_policy.sql`: `routing_rule` (`scope` enum `tenant, group, user`; `mode` enum `full_tunnel, destinations`; `destinations` "required and non-empty when `mode = destinations`" as a CHECK constraint; `version` for optimistic locking) and `posture_rule`.
- [X] T082 [US5] Implement resolution in `backend/internal/policy/resolve.go`: compute each user's effective rule and its routing group key (`route:full` or `route:dest:<sha256 of sorted destinations>`).
- [X] T083 [US5] Implement apply in `backend/internal/policy/apply.go`: ensure that one NetBird route set exists per distinct routing group (HA routing peers, metrics 100/200), each user is in exactly one routing group, unused route groups are removed, and call exclusions are always applied (FR-006). Changes are pushed within 5 minutes (FR-005a).
- [X] T084 [US5] Endpoints `GET/PUT /v1/tenants/{id}/routing-rules…` in `backend/internal/httpapi/routing.go` (returning the effective rule per user and conflicts; empty list → `422 routing_rule_empty_destinations`), plus groups CRUD in `backend/internal/httpapi/groups.go` mirrored to NetBird groups. Audit every change.
- [ ] T085 [US5] Run quickstart V6 in staging and record the results.

**Checkpoint**: Fine-grained routing control works, through the API the portal will use.

---

## Phase 8: User Story 6 - Unhealthy devices are blocked (Priority: P2)

**Goal**: Minimum OS and client version posture rules per tenant or group.

**Independent Test**: Quickstart V7: a non-compliant device is blocked within 1 minute and allowed within 1 minute of becoming compliant.

- [X] T086 [US6] Implement posture in `backend/internal/policy/posture.go`: map `posture_rule` (`min_os` per-OS versions, `min_client_version`, optional group) to NetBird posture checks and attach them to the tenant's access policies and routes. Gate on the `posture_checks` entitlement.
- [X] T087 [US6] Endpoints `GET/PUT /v1/tenants/{id}/posture-rules…` in `backend/internal/httpapi/posture.go`, with audit records. Add a `posture` section to the tenant definition apply in `backend/internal/tenancy/apply.go`.
- [ ] T088 [US6] Run quickstart V7 with a deliberately outdated test VM and record the results (confirms the spec's unconfirmed posture capability).

**Checkpoint**: Posture parity with Perimeter 81 is proven or the gap is documented.

---

## Phase 9: User Story 7 - Pilot grows from 10 to 50 users by configuration only (Priority: P2)

**Goal**: Prove Principle IX: resize and rebuild with no component change and no client reconfiguration.

**Independent Test**: Quickstart V11 and V12.

- [X] T089 [US7] Write `docs/runbooks/resize-gateway-pair.md`: resize the standby first, fail over, then resize the old primary (one reconnect), using only `infra/envs/pilot/terraform.tfvars` changes plus `tofu apply`. Step 0 is a restore rehearsal from the latest backup (constitution Operational Standards: before every tier change).
- [X] T090 [P] [US7] Write `docs/runbooks/resize-control-host.md` (control host resize with a maintenance window; connected users unaffected).
- [X] T091 [US7] Implement the restore playbook `infra/ansible/restore.yml`: fetch the latest backup from Spaces, decrypt, restore every database and stack config, and re-deploy all tenant stacks from the backend's StackSpecs. Document it in `docs/runbooks/restore-cell.md`.
- [X] T111 [US7] Write `docs/runbooks/move-tenant.md` (drain, dump and restore `nb_<slug>`, move the stack to a new control host by changing `tenant_stack.control_host_id` and re-rendering, switch DNS, verify) and rehearse it in staging with a second control host. This is quickstart V15 (FR-030, Principle V).
- [ ] T092 [US7] Run quickstart V11 (grow to 50: tfvars change plus quantity 50 via `subscription change`) and V12 (destroy staging and rebuild from the repository plus backup in under 4 hours). Record the timings in `specs/001-tier1-pilot/pilot-results.md`.

**Checkpoint**: Growth and disaster recovery are proven by configuration only.

---

## Phase 10: User Story 8 - Operator sees health, backups and audit trail (Priority: P3)

**Goal**: Monitoring, usage visibility, reconciliation and the audit trail from day one.

**Independent Test**: Quickstart V13: stop a tenant stack container and get an alert in 2 minutes with a runbook link; a restore drill passes; the audit log shows the last provisioning action.

- [X] T093 [P] [US8] Alert rules in `infra/observability/alerts/platform.yaml`: host down, a tenant stack unhealthy, Postgres down, Zitadel down, backup missed for over 26 hours, reconcile drift, seat limit hit, bandwidth over 80% of the included transfer, and a new NetBird or Zitadel security release unapplied for more than 5 days (Principle VI: 7-day patch window). Each alert has a `runbook_url` annotation (FR-024).
- [X] T094 [P] [US8] Dashboards in `infra/observability/dashboards/`: cell overview, gateway pair (active node, failovers, throughput), and tenant (users, connected now, last seen).
- [X] T095 [US8] Migration `backend/internal/store/migrations/0004_ops.sql`: `usage_sample` ("Kept 90 days, then rolled up daily") and `reconcile_run`.
- [X] T096 [US8] Implement usage sampling in `backend/internal/usage/sampler.go`: every 5 minutes, per tenant, read NetBird peers (`connected`, `last_seen`) and the per-peer WireGuard counters from routing peers via a small exporter. `GET /v1/tenants/{id}/usage` returns never-installed and inactive-30-day lists.
- [X] T097 [P] [US8] Build the routing-peer usage exporter in `infra/ansible/roles/routing_peer/files/wg-usage-exporter`: expose `wg show <if> transfer` as Prometheus metrics labelled by peer public key. Run pilot test P-5 and record whether the counters match a known download.
- [X] T098 [US8] Implement the reconciler in `backend/internal/reconcile/reconcile.go`: every 10 minutes, compare backend state (users, groups, routing groups, posture, StackSpec hash) with each NetBird stack and Zitadel org; write `reconcile_run`; repair safe drift; alert on the rest (Principle X).
- [X] T099 [US8] Add the `GET /v1/tenants/{id}/audit` endpoint with cursor pagination in `backend/internal/httpapi/audit.go`.
- [X] T100 [P] [US8] Write runbooks for every alert in `docs/runbooks/` (one file per alert name in T057 and T093).
- [X] T112 [US8] Availability measurement (SC-003): run `infra/tools/egress-probe/` continuously from an external vantage point (a small Droplet in another region), feeding a Grafana SLO for 99.9% monthly availability per region, in `infra/observability/slo/availability.yaml`.
- [ ] T101 [US8] Monthly restore drill and quarterly failover drill (Principle IV): a systemd timer on a staging host runs `infra/ansible/restore.yml` against the latest backup into staging and pushes a success metric. A second timer runs the gateway failover test (quickstart V3, one run) each quarter. Run both once and record the results.

**Checkpoint**: The operator can see, recover and audit everything.

---

## Phase 11: Polish & Cross-Cutting Concerns

- [ ] T102 [P] Security pass: confirm NetBird dashboards are not reachable from the internet; confirm secrets are absent from the repo (`gitleaks` in CI, `.github/workflows/ci.yml`), from logs and from StackSpecs; run `gosec` clean; confirm the DO API token scopes; confirm no user traffic content is recorded (Traefik access logs without bodies or query strings, NetBird traffic events off, no packet capture tools on gateways) (FR-027).
- [X] T113 [P] Write `docs/privacy/data-retention.md`: what connection metadata is kept (usage samples for 90 days then daily roll-ups, audit records, backups for 30 days), and that traffic content is never kept. This is the basis for the customer privacy policy (Principle VII).
- [ ] T103 [P] Run pilot test P-4 (iOS Change Server flow) and update `deploy/setup-page/` with screenshots.
- [ ] T104 [P] Run pilot test P-6 (Zitadel invitation delivery on DO) and finalize the notification provider in the `zitadel` role.
- [X] T105 Write `docs/runbooks/upgrade-netbird.md` (staged rollout: staging, then the test tenant, then tenant #1; automatic rollback per tenant-stack contract obligation 5) and `docs/runbooks/add-tenant.md`.
- [ ] T106 Production stand-up: `tofu apply` for `infra/envs/pilot`, then `site.yml`, then register the reserved IP with the PTA (T050 gate), apply `ent-onedge` and `onedge`, and invite 10 pilot users.
- [ ] T107 Run the full quickstart (V1–V13) in production with pilot users over 3 to 4 weeks. Record SC-001 to SC-009 outcomes and the pilot cost against $108/month (research R11) in `specs/001-tier1-pilot/pilot-results.md`.
- [ ] T108 Update `docs/architecture-overview.html` and the constitution if any pilot result changes a decision (via the constitution's amendment procedure).

---

## Dependencies & Execution Order

### Phase dependencies

- **Setup (1)**: no dependencies.
- **Foundational (2)**: depends on Setup. Blocks every user story.
- **US1 (3)**: depends on Foundational. It is the MVP.
- **US2 (4)**: depends on Foundational (gateway role T021, routing peers T039). Independent of US1's identity work. Run it early, because P-1 is the riskiest assumption.
- **US3 (5)**: depends on US1 (T044, T047 identity plumbing).
- **US4 (6)**: depends on Foundational. Independent of US1–US3, but T078 data is used by T106.
- **US5 (7)**: depends on US1 (T046 route plumbing).
- **US6 (8)**: depends on US5 (policy package and migration T081).
- **US7 (9)**: depends on US2 (failover for one-reconnect resizes) and T020 backups.
- **US8 (10)**: depends on Foundational. T096 and T098 benefit from US5 and US6 being done.
- **Polish (11)**: depends on all the stories the pilot launches with (US1–US8).

### Within each story

Tests marked in a story run before its implementation tasks. Then: migrations, then domain
packages, then endpoints and CLI, then the quickstart validation run.

### Parallel opportunities

- Setup: T003–T008 in parallel after T001–T002.
- Foundational: T010, T011 and T012 in parallel after T009. T015, T016, T017, T019 and T020 in parallel after T014. T026 and T027 in parallel with the store work. T043 in parallel once T042 lands.
- After Foundational, **US1, US2 and US4 can proceed in parallel** (different files: `identity/`+`setup-page/`, `roles/gateway/`, `billing/`).
- US8 alert, dashboard and runbook tasks (T093, T094, T100) can be done in parallel with any story.

## Parallel Example: after Foundational

```text
Developer A (US1): T044 → T046 → T047 → T048/T049 → T052
Developer B (US2): T053 → T054 → T055/T056 → T057 → T059
Developer C (US4): T066/T067/T068 → T069 → T070 → T071 → T073 → T074 → T076
```

## Implementation Strategy

### MVP first

1. Phases 1 and 2 (Setup and Foundational).
2. Phase 4 **first steps (T053, T054, T059)** in staging, right after the gateway role. If P-1
   fails the 60-second target, the design changes before more is built.
3. Phase 3 (US1). **Stop and validate**: one real user connects with Google sign-in through
   the fixed IP.

### Incremental delivery

1. US1 + US2: a usable, resilient VPN for tenant #1 staff in staging.
2. US3: identity decision locked (ADR-002) before user 11.
3. US4: commercial records in place. tenant #1 runs on its enterprise plan.
4. US5 + US6: routing control and posture parity with Perimeter 81.
5. US7 + US8: growth, recovery and operations proven, then production stand-up (T106) and the
   3-to-4-week pilot (T107).

## Notes

- T018 is intentionally unused (removed during drafting). T109–T114 were added after `/speckit-analyze`, placed in their phases. IDs stay stable for references, so they aren't in numeric order.
- T114 depends on T042 even though it's listed earlier in Phase 2.

## Implementation notes (2026-11-03, `/speckit-implement`)

- **Migration numbering differs from the plan**: `0002_policy.sql` (routing, call exclusions,
  posture; brought forward for T046), `0003_billing.sql`, `0004_ops.sql`.
- **Test locations**: T066–T068 are in `backend/tests/integration/billing_test.go` (they need
  PostgreSQL for the immutability triggers); T060 is `backend/tests/contract/tenant_isolation_test.go`
  and walks every route in the OpenAPI spec; routing (T080) has unit tests in
  `internal/policy/resolve_test.go` and an integration test in `tests/integration/routing_test.go`.
- **Design adjustments found while building** (contracts and data model updated to match):
  StackSpec gained `httpPort`, the identity provider is added through NetBird's API at bootstrap
  instead of the spec, enterprise plans reference their tenant by slug, sign-in methods are
  `text[]`, call exclusions must be IPv4 CIDRs (research R12 complement fallback implemented in
  `internal/policy/complement.go`), and upstream Google/Microsoft sign-in uses shared
  instance-level IdPs in Zitadel with import-only linking (enforces FR-003).
- **Built but not yet exercised against real services** (verified only with a stand-in NetBird
  API and real PostgreSQL): Zitadel calls, the Actions v2 groups-claim payload format, NetBird
  `/api/setup` PAT bootstrap, keepalived/DO failover, several clients on one gateway. These are
  exactly pilot tests P-1, P-2, P-3, P-5, P-6, P-7 and the staging runs still open below.
- **T111**: runbook written; the staging rehearsal (quickstart V15) is still to do with T092.
- **T101**: drill timers written in `infra/ansible/drills.yml`; first runs need the staging cell.
- **T110**: needs a real Zitadel; run in staging together with T063/T065.
- Every task that changes tenant or commercial state must write an audit record in the same
  transaction (T029).
- Commit after each task or logical group. Never commit secrets (`.gitignore` in T001,
  gitleaks in T102).
