# Implementation Plan: Interim Pilot on the Shared Hetzner Server

**Branch**: `002-interim-pilot-178` | **Date**: 2026-11-03 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/002-interim-pilot-178/spec.md`

## Summary

Run feature 001's platform on the existing Hetzner server 178.104.48.140 for up to 10 test users,
under constitution v2.2.0's Interim Hosting Exception. Nothing in the tenant model, API, billing or
policy code changes. What's new:

- a **ComposeDeployer** (second implementation of 001's `Deployer`) that renders each tenant's
  stack as a Docker Compose project routed by Coolify's existing Traefik through labels;
- the **gateway in an isolated bridge network** with a guard sidecar that blocks VPN users from
  every private range and the host (replacing 001's host-network routing peer for this host);
- **interim limits** in the backend (10 users, 2027-02-28, test tenants only, temporary-IP notice);
- an **isolation test** that must pass before a tenant's users can reach the gateway;
- **backups** to Hetzner Object Storage with a restore test that gates real users;
- Ansible playbooks to check, install, and later remove everything on 178 without touching the
  host firewall or other apps.

## Technical Context

**Language/Version**: Go (stable, 1.27) for backend additions; Ansible 2.21; shell/nftables in the
guard sidecar.

**Primary Dependencies**: feature 001 stack (NetBird v0.82.0, Zitadel v4.20, malgaryd); Coolify
4.3.6's Traefik (existing, labels only); `docker compose` CLI inside the malgaryd image;
`rclone` + `age` in the backup image; Grafana Alloy container.

**Storage**: shared PostgreSQL 18 (`buxe1z5wikkaftx5meckmr4w`) with databases `malgary`,
`zitadel`, `nb_<tenant>`; Hetzner Object Storage (`fsn1`) for backups.

**Testing**: unit tests for the ComposeDeployer renderer and interim limits; integration tests
(testcontainers) for the new migration and gates; on-host validation V1–V12
([quickstart.md](quickstart.md)).

**Target Platform**: Ubuntu 24.04, kernel 6.8, Docker 27 on 178.104.48.140.

**Project Type**: deployment variant of an existing backend + infrastructure.

**Performance Goals**: same as 001 (connect < 10 min, routing changes < 5 min).

**Constraints**: total Malgary memory ≤ 3 GB; no host firewall/NAT/sysctl changes; no change to
Coolify or other apps; UDP 443 is Traefik's; max 10 users; exit by 2027-02-28.

**Scale/Scope**: 3 tenants (onedge, testco, malgary-ops), ≤ 10 users, one host.

## Constitution Check

*GATE: before Phase 0 and after Phase 1. Constitution v2.2.0.*

| Principle | Compliance | Pre | Post |
| --- | --- | --- | --- |
| I. Tenant isolation | Unchanged: stack, database, role and Zitadel org per tenant; one routing-peer container (own network namespace) per tenant | ✅ | ✅ |
| II. Everything as code | Ansible playbooks + ComposeDeployer; nothing configured in Coolify's UI | ✅ | ✅ |
| III. Stable egress IPs | **Exempted** by the Interim Hosting Exception; FR-012 labels the IP temporary | ✅ (exception) | ✅ |
| IV. No SPOF on data path | **Exempted** (single host, no standby) | ✅ (exception) | ✅ |
| V. Cells, stack per tenant | Stack per tenant kept; dedicated-host assumption exempted | ✅ (exception) | ✅ |
| VI. Upstream-first | Unmodified NetBird and Zitadel images | ✅ | ✅ |
| VII. Security and audit | Tenant-chosen sign-in, MFA, audit unchanged. **Deviation**: SSH to 178 stays as the owner manages it (Complexity Tracking) | ✅ | ✅ |
| VIII. Cost discipline | Extra cost ≈ €5/month (object storage) | ✅ | ✅ |
| IX. Grow by configuration | Same DNS names and data model; moving out is restore + DNS change | ✅ | ✅ |
| X. Portal is the product | Unchanged; dashboards IP-restricted to operators | ✅ | ✅ |
| XI. Versioned plans/payments | Unchanged | ✅ | ✅ |
| Interim exception conditions | Isolated gateway network, isolation test gate, memory limits, 10 test users, backups before real users, exit date enforced | ✅ | ✅ |

Result: **PASS** with the deviations below.

## Project Structure

### Documentation

```text
specs/002-interim-pilot-178/
├── plan.md  research.md  data-model.md  quickstart.md
├── contracts/compose-deployer.md  contracts/interim-api.md
├── checklists/requirements.md
└── tasks.md   (/speckit-tasks)
```

### Source Code (additions only)

```text
backend/
├── internal/tenancy/compose/        # ComposeDeployer: renderer, templates, apply, remove
├── internal/interim/                # limits, egress note, isolation and restore gates
├── internal/store/migrations/0005_interim.sql
├── cmd/malgaryctl/isolation.go      # isolation-test command (runs the probe client)
├── cmd/malgaryctl/backup.go         # backup verify
└── Dockerfile                       # malgaryd image (distroless + docker compose CLI)

infra/ansible/
├── interim-check.yml                # read-only facts from 178
├── interim-site.yml                 # install on 178
├── interim-remove.yml               # exit: remove everything
├── inventory/interim.yml            # 178 only
└── roles/interim-{base,zitadel,backend,backup,alloy}/

infra/images/
├── gw-guard/                        # guard sidecar: nftables rules + wg exporter
├── isolation-probe/                 # disposable NetBird client + probe script
└── backup/                          # rclone + age + pg_dump
```

**Structure Decision**: additive. Feature 001's roles and modules stay for the dedicated cell;
interim-only roles are prefixed `interim-` and removed from use at exit.

## Phasing

1. **Facts** (read-only): `interim-check.yml` confirms proxy names, cleanup settings, firewall,
   memory. Stop if anything contradicts the research.
2. **Backend additions**: migration, interim limits, gates, ComposeDeployer, image, CLI commands,
   tests.
3. **Images**: gw-guard, isolation-probe, backup.
4. **Install on 178** (owner approval): base dirs and network, provisioner role, Zitadel,
   malgaryd, backup, Alloy. Host snapshot before/after.
5. **First tenant (testco)**, isolation test, then onedge and test users.
6. **Backups + restore test**, then real users allowed (up to 10 total).
7. **Exit rehearsal** (move testco to a scratch host) and removal playbook dry run.

## Complexity Tracking

| Deviation | Why needed | Simpler alternative rejected because |
| --- | --- | --- |
| malgaryd mounts the Docker socket | ComposeDeployer must start tenant stacks on the host | Ansible-over-SSH from inside a container needs keys to the same host; the socket is equivalent and simpler. Removed at exit. |
| SSH to 178 not restricted to a VPN/bastion (Principle VII) | Host access is managed by the owner; FR-003 forbids changing the host firewall | Changing SSH on a host with 48 apps is out of scope. Applies again on the dedicated cell. |
| Provisioner role with CREATEDB/CREATEROLE on a shared database | Tenant databases must be created automatically | A superuser credential would reach the other apps' data. PG16+ CREATEROLE only manages roles it created. |
| Guard sidecar re-applies rules every 30s | NetBird manages its own nftables tables in the same namespace and could reorder | A one-shot rule could be lost after a NetBird restart; the check is cheap and logs drift. |
