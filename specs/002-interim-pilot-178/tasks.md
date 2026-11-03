---
description: "Task list for feature 002: interim pilot on the shared Hetzner server"
---

# Tasks: Interim Pilot on the Shared Hetzner Server

**Input**: Design documents from `specs/002-interim-pilot-178/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md),
[data-model.md](data-model.md), [contracts/](contracts/), [quickstart.md](quickstart.md)

**Tests**: included. The spec requires an automated isolation test (FR-009), and feature 001's
practice is unit tests for renderers and integration tests (testcontainers) for gates.

**Organization**: by user story. US4 (backups) is **deferred** until the dedicated servers exist
(spec Clarifications, 2026-11-03); only its gate is built here, in Phase 2.

**Server rule**: every task marked 🖥 runs against 178.104.48.140 and needs the owner's go-ahead
first. Everything else is code in this repository.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: can run in parallel (different files, no dependency on an unfinished task)
- **[Story]**: US1, US2, US3, US5 (US4 deferred)

## Path conventions

- Backend: `backend/` (module `malgary/backend`)
- Ansible: `infra/ansible/`, images: `infra/images/`
- Deployment data: `deploy/`

---

## Phase 1: Setup

**Purpose**: configuration and scaffolding shared by every story.

- [X] T001 Add interim and compose settings to `backend/internal/config/config.go`: `Interim`
  (`MALGARY_INTERIM`, bool), `InterimMaxUsers` (`MALGARY_INTERIM_MAX_USERS`, default 10),
  `InterimUntil` (`MALGARY_INTERIM_UNTIL`, date, required when interim), `InterimTestTenants`
  and `InterimTestDomains` (comma lists), `InterimEgressNote`, `InviteDelivery`
  (`MALGARY_INVITE_DELIVERY`: `email` | `link`, default `email`), `Deployer`
  (`MALGARY_DEPLOYER`: `ansible` | `compose`, default `ansible`), and the compose host settings
  from [contracts/compose-deployer.md](contracts/compose-deployer.md) (`MALGARY_TRAEFIK_NETWORK`,
  `MALGARY_TRAEFIK_ENTRYPOINT`, `MALGARY_TRAEFIK_CERTRESOLVER`, `MALGARY_STACK_DB_HOST`,
  `MALGARY_GW_NETWORK`, `MALGARY_HOST_PUBLIC_IP`, `MALGARY_STACK_DIR` default
  `/opt/malgary/stacks`, `MALGARY_OPERATOR_ALLOWLIST`). Validate: unknown enum values and a
  missing `InterimUntil` while interim is on are errors.
- [X] T002 [P] Unit tests for the new settings in `backend/internal/config/config_test.go`
  (defaults, enum validation, interim requires an end date).
- [X] T003 [P] Create the image directories with READMEs: `infra/images/gw-guard/`,
  `infra/images/isolation-probe/` (the backup image is deferred).
- [X] T004 [P] Add `.dockerignore` at `backend/.dockerignore` (`*.test`, `*.out`, `.git`,
  `.env*`, `tests/`).

---

## Phase 2: Foundational (blocks every story)

**Purpose**: migration, the interim gate package and API, which US1–US3 all depend on.

- [X] T005 Write migration `backend/internal/store/migrations/0005_interim.sql` per
  [data-model.md](data-model.md): `isolation_test_result` (id uuid, tenant_id fk tenant,
  stack_hash text not null, ran_at timestamptz, passed bool not null, egress_ip inet null,
  results jsonb not null, ran_by text not null) and `backup_restore_test` (id, backup_key,
  backup_taken_at, tested_at, passed, details jsonb). Both **append-only**: revoke UPDATE and
  DELETE from `malgary_app`, as for `audit_record`. Index `(tenant_id, stack_hash, ran_at desc)`.
- [X] T006 Add queries to `backend/queries/interim.sql` (insert/list isolation results, latest
  result for tenant + hash, insert restore test, any passed restore test, count active users
  across all tenants) and regenerate with sqlc into `backend/internal/store/db/`.
- [X] T007 Allow a single-host gateway "pair" for the interim cell (done: `cell.interim` column,
  cell code `iNN`, nullable standby, no PTA requirement on interim cells): the standby may be absent
  (or equal the primary) in `backend/internal/tenancy/inventory.go`, `placement.go`, and a
  `0005` constraint change if `gateway_pair` requires two distinct hosts. Placement must still
  require two hosts when the cell is not marked `interim: true`.
- [X] T008 Create package `backend/internal/interim/` (`interim.go`): `Limits` built from config
  with `CheckImport(ctx, q, tenantSlug, emails []string) error` returning fixed errors
  `interim_user_limit` (would exceed `MaxUsers` across all tenants, FR-010),
  `interim_expired` (after `Until`, FR-011), `interim_tenant_not_allowed` (tenant not in the
  test list), `restore_test_required` (any email whose domain is not in `TestDomains`, or a
  non-test tenant, while no passed `backup_restore_test` exists, FR-015); `DaysLeft(now)`;
  `CheckPlanPrice(tenantSlug, price)` (non-zero price only for `onedge`, FR-010). All are
  no-ops when interim is off.
- [X] T009 [P] Unit tests in `backend/internal/interim/interim_test.go` for each error, the
  domain rule (case-insensitive), and the disabled path.
- [X] T010 Call `CheckImport` inside the user import transaction in
  `backend/internal/users/service.go` (before seats are counted), and `CheckPlanPrice` where
  subscriptions are created in `backend/internal/billing/`.
- [X] T011 Add the interim endpoints and error codes from
  [contracts/interim-api.md](contracts/interim-api.md) to `backend/api/openapi.yaml`
  (`POST/GET /v1/tenants/{tenantId}/isolation-tests`, `POST /v1/backups/restore-tests`,
  `GET /v1/interim`; `inviteLink` on the import result); regenerate server and client.
- [X] T012 Implement handlers in `backend/internal/httpapi/interim.go`: operator-only writes,
  tenant admins see only their own tenant (404 otherwise), all routes 404 when interim is off,
  every write audited with `audit.Record`.
- [X] T013 Add a `malgary_interim_days_left` gauge and `malgary_interim_users_used` gauge in
  `backend/cmd/malgaryd/main.go` metrics, and a 30-day warning rule in
  `infra/observability/alerts/platform.yaml` (FR-011).
- [X] T014 Integration test `backend/tests/integration/interim_test.go`: 11th user refused,
  expired date refused, non-test domain refused with no restore test and allowed after one is
  recorded, append-only tables reject UPDATE as `malgary_app`.
- [X] T015 Extend `backend/tests/contract/tenant_isolation_test.go` coverage so the new
  operations are walked (it reads the OpenAPI file; confirm the new paths pass).

**Checkpoint**: interim limits work in tests with no server involved.

---

## Phase 3: User Story 1 - A test user connects through the interim server (P1) 🎯 MVP

**Goal**: tenant stacks start on 178 through the ComposeDeployer, routed by Coolify's Traefik,
and a test user connects from the setup page.

**Independent test**: quickstart V1, V2, V9a.

### Tests for US1

- [X] T016 [P] [US1] Render tests (structural checks instead of golden files, plus a
  `docker compose config` validation run) in `backend/internal/tenancy/compose/render_test.go`:
  a sealed StackSpec renders `compose.yaml`, `config.yaml`, `dashboard.env` and
  `setup/index.html` byte-for-byte equal to files in `testdata/`; rendering twice gives the
  same bytes; secrets appear only in files with mode 0600 and never in `compose.yaml`.
- [X] T017 [P] [US1] Test that rendered `compose.yaml` has a memory limit on every service,
  `restart: unless-stopped` everywhere, no `network_mode: host`, no `privileged`, and the
  routing peer only on `MALGARY_GW_NETWORK` (also guards US2/US3).

### Implementation for US1

- [X] T018 [US1] Templates in `backend/internal/tenancy/compose/templates/` (Go
  `text/template`, embedded): port `config.yaml` and `dashboard.env` from
  `infra/ansible/roles/tenant_stack/templates/`; `compose.yaml` with services `netbird-server`
  (coolify network, STUN `3478 + (httpPort - 20000)`/udp, Traefik labels: gRPC paths →
  `h2c`, `/api`, `/oauth2`, `/relay`, `/ws-proxy` → http, entrypoint and certresolver from
  config), `dashboard` (Traefik `ipAllowList` from `MALGARY_OPERATOR_ALLOWLIST`), `setup`
  (static nginx, `/` and `/setup`), `routing-peer` and `guard` (filled in by US3), memory
  limits 256/64/16/96/32 MB per [research.md](research.md) I11.
- [X] T019 [US1] Setup page template `backend/internal/tenancy/compose/templates/setup.html`
  ported from `setup-page.html.j2`, plus the interim egress note when set (FR-012).
- [X] T020 [US1] `ComposeDeployer` in `backend/internal/tenancy/compose/deployer.go`
  implementing `tenancy.Deployer`: render into `<StackDir>/<tenant>/`, compare with existing
  files and skip when unchanged (obligation 1), keep `compose.yaml.prev`, run
  `docker compose -p nb-<tenant> up -d --remove-orphans`, poll `GET /api/instance` on the
  container until 200 within 2 minutes, otherwise restore `.prev` and return the error with the
  log tail (obligation 3). Tenant slug validated before any path is built.
- [X] T021 [US1] Database provisioning in `backend/internal/tenancy/compose/database.go`:
  connect as `malgary_provisioner` (DSN from a secret ref), create role `nb_<tenant>` with a
  generated password stored under `<SecretDir>/stacks/<tenant>/db-password` (0600) and database
  `nb_<tenant>` owned by it; idempotent; `REVOKE ALL ON DATABASE ... FROM PUBLIC`.
- [X] T022 [US1] Select the deployer from `MALGARY_DEPLOYER` in
  `backend/cmd/malgaryd/main.go`; set `StackSpec.Database.Host` from `MALGARY_STACK_DB_HOST`
  when compose is used.
- [X] T023 [US1] Invitation links (FR-012a) in `backend/internal/identity/users.go`: when
  `InviteDelivery=link`, call `CreateInviteCode` with `ReturnCode` and build the URL
  `https://id.<base>/ui/v2/login/verify?userId=…&code=…&invite=true` (confirm the path against
  Zitadel v4.20's login app); return it from `CreateUser`; pass it through
  `backend/internal/users/service.go` to the import response; never store or log it.
- [X] T024 [US1] Print invitation links in `backend/cmd/malgaryctl/users.go` after import
  (one line per user), with a note that each link works once.
- [X] T025 [US1] Dockerfile `backend/Dockerfile`: multi-stage build (`golang:1.27` →
  `gcr.io/distroless/static-debian12:nonroot`) with the `docker` CLI and compose plugin binaries
  copied in for the ComposeDeployer; `malgaryd` and `malgaryctl` in `/usr/local/bin`.
- [X] T026 [P] [US1] Cell inventory for the interim host `deploy/cells/i01.yaml`: cell `i01`,
  `interim: true`, `maxUsers: 10`, one host `h178` with roles control and gateway, one gateway
  "pair" with egress IP 178.104.48.140 and no standby. Add `i\d\d` to the reserved tenant slugs.
- [X] T027 [US1] Ansible inventory `infra/ansible/inventory/interim.yml` (178 only) and
  `group_vars` for interim (`infra/ansible/group_vars/interim.yml`): proxy entrypoint and
  resolver names, Postgres container name, memory limits, operator allowlist.
- [X] T028 [US1] Read-only facts playbook `infra/ansible/interim-check.yml`: reads the Coolify
  proxy compose for entrypoint and certresolver names, lists Docker networks, checks the
  Postgres container name and version, `citext` availability, free memory, UDP ports 3478–3480
  and 51821–51830 free, and writes `interim-facts.yml` locally. No changes (`check_mode`
  safe, every task `changed_when: false`).
- [X] T029 [US1] Role `infra/ansible/roles/interim_base/`: `/opt/malgary/{stacks,specs,secrets}`
  (0750, secrets 0700), Docker network `malgary-gw` (bridge, `10.250.0.0/24`,
  `com.docker.network.bridge.enable_icc=false`), and a check that the `malgary_provisioner` login
  (created by the owner, 2026-11-04) connects over TCP with the password in
  `/opt/malgary/secrets/provisioner-password` (0600). Malgary never needs the superuser
  password.
- [X] T030 [US1] Role `infra/ansible/roles/interim_zitadel/`: compose project `malgary-id` with
  Zitadel v4.20 and the login app on the `coolify` network, Traefik labels for
  `id.vpn.malgary.com`, memory 768 MB + 256 MB, database `zitadel` created by the provisioner,
  no SMTP (email deferred), first-instance machine key written to
  `/opt/malgary/secrets/zitadel-machine-key.json` (0600).
- [X] T031 [US1] Role `infra/ansible/roles/interim_backend/`: build the malgaryd image on the
  host from the repository checkout (or a pushed release), compose project `malgary-api` on the
  `coolify` network with Traefik labels for `api.vpn.malgary.com`, 256 MB limit, mounts for the
  Docker socket and `/opt/malgary/*`, env file (0600) with the interim settings from
  [data-model.md](data-model.md) (`MALGARY_INVITE_DELIVERY=link`,
  `MALGARY_INTERIM_TEST_DOMAINS=gmail.com`), databases `malgary` with owner and app roles.
- [X] T032 [US1] Playbook `infra/ansible/interim-site.yml` running `interim_base`,
  `interim_zitadel`, `interim_backend` and, only when Grafana credentials are set,
  `interim_alloy` (T046). Writes a firewall snapshot before and after (T037).
- [ ] T033 🖥 [US1] Run `interim-check.yml` on 178 and update [research.md](research.md) with any
  fact that differs (owner go-ahead).
- [ ] T034 🖥 [US1] Run `interim-site.yml`, apply the plans and the `testco` tenant, and walk
  quickstart V1, V2 and V9a with one test user (owner go-ahead; needs the Google OAuth
  client).

**Checkpoint**: one tenant stack runs on 178 and a test user connects (after US3's gate passes).

---

## Phase 4: User Story 2 - The other apps on the server are unaffected (P1)

**Goal**: prove nothing outside Malgary changes, memory stays within 3 GB, and cleanup can't
remove Malgary.

**Independent test**: quickstart V3, V4, V10.

- [X] T035 [P] [US2] Memory budget check in `backend/internal/tenancy/compose/budget.go`: sum the
  limits of all rendered stacks plus the platform's fixed 1,408 MB (Zitadel, login, malgaryd,
  Alloy) and refuse a deploy that would exceed `MALGARY_MEMORY_BUDGET_MB` (default 3072); test
  in `budget_test.go` (FR-005, SC-004).
- [X] T036 [P] [US2] Labels on every Malgary container and network: `com.malgary.managed=true`
  and `com.malgary.tenant=<slug>`, in the compose templates and the interim roles, so checks
  and removal find exactly Malgary's objects.
- [X] T037 [US2] Host snapshot tasks `infra/ansible/roles/interim_base/tasks/snapshot.yml`:
  `iptables-save`, `nft list ruleset`, `sysctl -a` for net keys, and `docker ps` of non-Malgary
  containers, saved to the operator's machine with a timestamp; `infra/tools/interim-diff.sh`
  compares two snapshots and fails on any change other than Docker's DNAT/ACCEPT rules for
  Malgary's published ports (FR-003, V3).
- [X] T038 [P] [US2] Script `infra/tools/interim-memory.sh`: `docker stats --no-stream` filtered
  by `com.malgary.managed=true`, prints each container against its limit and the total against
  3 GB, exits non-zero when over (V4).
- [ ] T039 🖥 [US2] Run the snapshot diff and memory check after T034; confirm in Coolify that
  "delete unused volumes" is off and run its Docker cleanup once to prove Malgary survives (V10).

**Checkpoint**: host unchanged except Docker's rules for Malgary's ports.

---

## Phase 5: User Story 3 - VPN users can't reach anything private (P1)

**Goal**: the gateway runs in `malgary-gw` behind the guard, and the isolation test gates user
access.

**Independent test**: quickstart V5, V6.

### Tests for US3

- [X] T040 [P] [US3] Test for the guard rules generator in
  `infra/images/gw-guard/rules_test.sh` (or Go test if written in Go): the generated nftables
  table drops forward traffic to `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`,
  `100.64.0.0/10` except the tenant's NetBird range, `169.254.0.0/16`, `127.0.0.0/8`,
  `224.0.0.0/4` and the host IP; it touches only table `inet malgary_guard`.

### Implementation for US3

- [X] T041 [US3] Guard image `infra/images/gw-guard/`: Alpine with `nftables`, `wireguard-tools`
  and the exporter from `infra/ansible/roles/routing_peer/files/wg-usage-exporter`; entrypoint
  applies the forward-drop table from `GUARD_HOST_IP` and `GUARD_ALLOW_CIDR` every 30 seconds,
  logs and counts drift (`malgary_guard_reapplied_total`), and serves metrics on `:9586`.
  Runs with `NET_ADMIN` only.
- [X] T042 [US3] Add `routing-peer` and `guard` to `compose.yaml` template: routing peer is the
  NetBird client image at the stack's version, on `MALGARY_GW_NETWORK`, `cap_add: NET_ADMIN`,
  `sysctls: net.ipv4.ip_forward=1`, `/dev/net/tun`, published `<wgPort>:<wgPort>/udp`, setup key
  from a 0600 file; guard with `network_mode: service:routing-peer`. Update golden files.
- [X] T043 [US3] Keep the tenant's access policy disabled until isolation passes: in
  `backend/internal/tenancy/apply.go` (or `rules.go`), when interim is on, set
  `malgary:access:*` policies `enabled=false` unless the latest `isolation_test_result` for the
  stack's current hash passed; recording a pass triggers a re-apply (FR-009).
- [X] T044 [US3] Probe image `infra/images/isolation-probe/`: NetBird client plus a probe script
  that tries TCP 22/80/443/5432 on the host IP, `10.0.0.1`, `10.250.0.1`, `172.17.0.1`, the
  Postgres container IP (argument), `192.168.0.1`, `169.254.169.254`, then
  `https://1.1.1.1` and an egress-IP lookup; prints a JSON result matching the
  `results` shape in [data-model.md](data-model.md).
- [X] T045 [US3] `malgaryctl isolation-test <tenant>` in `backend/cmd/malgaryctl/isolation.go`:
  creates a single-use ephemeral setup key in group `isolation-test` (which gets the full-tunnel
  route), runs the probe container on its own throwaway bridge network, posts the result to
  `POST /v1/tenants/{id}/isolation-tests`, removes the container, network and peer, and exits
  non-zero naming any reachable destination.
- [X] T046 [P] [US3] Role `infra/ansible/roles/interim_alloy/` (optional, runs only when Grafana
  Cloud credentials are set): Alloy container, 128 MB, scraping malgaryd, guards and NetBird
  metrics.
- [ ] T047 🖥 [US3] Build the guard and probe images on 178, redeploy `testco`, run
  `malgaryctl isolation-test testco` (V5), then change the stack and confirm users are blocked
  until a new pass (V6). Owner go-ahead.

**Checkpoint**: user access exists only for stacks with a passing isolation test.

---

## Phase 6: User Story 4 - Backups (P2) — DEFERRED

Deferred until the dedicated servers exist (owner, 2026-11-03). The FR-015 gate (T008, T014)
keeps real company users out until this is done. When resumed, generate the tasks from
[research.md](research.md) I8: backup image (`pg_dump` + `age` + `rclone`), `interim_backup` or
cell `backup` role, `malgaryctl backup verify`, the 26-hour alert, and quickstart V8.

---

## Phase 7: User Story 5 - Move to a dedicated cell (P3)

**Goal**: documented move and removal, rehearsed with testco.

**Independent test**: quickstart V11, V12.

- [X] T048 [P] [US5] Runbook `docs/runbooks/interim-exit.md`: final database dumps, restore on
  the new cell with 001's `restore.yml`, copy `/opt/malgary/{stacks,specs,secrets}`, re-point
  the three Cloudflare records, check clients reconnect, then removal (FR-016).
- [X] T049 [US5] Removal playbook `infra/ansible/interim-remove.yml`: `docker compose down -v`
  for every project labelled `com.malgary.managed=true`, remove `malgary-gw`, drop every
  database and role created by `malgary_provisioner`, then the provisioner role, then
  `/opt/malgary`; dry-run by default (`-e confirm=yes` to act), and a final check that nothing
  labelled Malgary remains (FR-017).
- [X] T050 [US5] `ComposeDeployer.Remove(tenant)` in
  `backend/internal/tenancy/compose/deployer.go` per the contract's teardown section, used by
  the playbook and tenant deletion.
- [ ] T051 🖥 [US5] Rehearse: move testco to a scratch host, reconnect an existing client,
  then dry-run `interim-remove.yml` on 178 (V11, V12). Needs a scratch server; owner go-ahead.

---

## Phase 8: Polish and cross-cutting

- [X] T052 [P] Update `deploy/cells/README.md` (done) and `docs/architecture-overview.html` (deferred until the interim layout is proven on 178) with the
  interim deployment (one host, compose, guard), and add `docs/decisions/0002-interim-host.md`.
- [X] T053 [P] CI: build `infra/images/*` and `backend/Dockerfile` (no push) and run the guard
  rules test in `.github/workflows/ci.yml`.
- [X] T054 Run `go vet`, `golangci-lint`, `go test ./...`, `ansible-lint` and `tofu validate`
  locally; fix findings.
- [ ] T055 Fill in `specs/002-interim-pilot-178/quickstart.md` results after the on-server runs
  and record pass/fail per scenario.

- [X] T056 Bootstrap the backend's project in Malgary sign-in on first start: "Malgary API"
  project with roles owner, operator, tenant_admin, deployer; `malgaryctl` device-flow app (JWT
  access tokens with roles); owner role for `MALGARY_OWNER_EMAIL`; IDs kept in the secret store
  (`backend/internal/identity/apiproject.go`, `cmd/malgaryd/main.go`). Found missing while
  writing the install: without it nobody can sign in to the API.
- [X] T057 Interim tenant definitions `deploy/tenants/interim/{testco,onedge}.yaml` (cell
  `i01`, gateway `gw-i01`, Gmail test users; testco uses managed accounts so it works before
  Google sign-in exists).

## Implementation notes

- The gateway container registers with `NB_HOSTNAME` equal to the routing peer's host
  (`gw.i01`), because the backend finds gateway peers by hostname (bug found by the
  integration test, fixed in the template).
- The isolation probe uses its own route (0.0.0.0/0 through the tenant's gateway) and access
  policy, both tagged `isolation-test` and removed after the run, so it works while users are
  still locked out and covers the worst routing rule a tenant could set. Its network uses
  192.0.2.0/28 so no private target is directly connected. Only a silent drop counts as
  "blocked"; a reset or answer counts as reachable, so a missing guard fails the test.
- Results are always recorded against the stack's current hash (the client's value is
  ignored), and access policies are created disabled until a pass exists for that hash.
- The provisioner gives itself membership of each role it creates (PostgreSQL 16+ grants the
  creator only ADMIN), which is what lets it create the role's database; tested on
  PostgreSQL 18 together with the checks that it can't alter, drop or reach other apps' roles
  and databases.

---

## Dependencies and execution order

- Phase 1 → Phase 2 → stories.
- US1 code (T016–T032) can be built without the server. T033–T034 need the owner's go-ahead.
- US3 needs US1's templates (T018) and the gate tables (T005). Users connect only after T047
  passes, so US1's on-server check V1 runs after T047.
- US2 needs the stacks running (T034).
- US5 needs everything else, and a scratch server for T051.

### Parallel opportunities

- T002, T003, T004 together.
- T009 with T011–T012 once T008 is in.
- T016, T017, T026 together; T035, T036, T038 together; T040, T046 together; T048, T052, T053.

## Implementation strategy

1. **MVP (no server)**: Phases 1–2, US1 code, US3 code, US2 code. Commit and push after each
   phase.
2. **On server, step by step with the owner**: T033 → T034 (testco) → T047 → T039 → onedge and
   up to 10 test users.
3. **Before any real company user or by the exit date**: dedicated servers, then US4 and US5.
