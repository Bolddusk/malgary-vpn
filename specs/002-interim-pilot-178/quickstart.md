# Quickstart and Validation: Interim Pilot on 178

## Prerequisites (owner)

- Root SSH to 178.104.48.140 for the operator's key.
- Cloudflare DNS-only A records `*.vpn`, `id.vpn`, `api.vpn` → 178.104.48.140 (done 2026-11-03).
- PostgreSQL superuser password (Coolify UI), used once to create `malgary_provisioner`.
- Hetzner Object Storage bucket + access keys (backups); owners' `age` key pair.
- Email relay credentials for `mail.laxycone.com:587`.
- Grafana Cloud stack (free) for metrics and alerts.
- If a Hetzner Cloud Firewall is attached to 178: UDP 3478–3480 and 51821–51830 opened there.

## Stand up (each step needs the owner's go-ahead)

1. `bin/run interim-check.yml` — read-only: Coolify proxy entrypoint and resolver names, Docker
   cleanup settings, Hetzner firewall presence, free memory. Writes `interim-facts.yml`.
2. `bin/run interim-site.yml` — creates `/opt/malgary`, the `malgary-gw` network, the provisioner
   role (operator enters the superuser password once), Zitadel, malgaryd, backup and Alloy
   containers. Snapshot of the host's iptables/nftables before and after (V3).
3. `malgaryctl plan apply -f deploy/plans/ent-onedge.yaml` (and `ent-testco`, `internal-ops`).
4. `malgaryctl tenant apply -f deploy/tenants/testco.yaml` → wait for `active`.
5. `malgaryctl isolation-test testco` → must pass before step 6.
6. Same for `onedge`; import up to 10 test users.

## Validation scenarios

| # | Scenario | Pass when | Spec |
| --- | --- | --- | --- |
| V1 | Test user onboarding via setup page | Connected < 10 min, egress 178.104.48.140 | US1, SC-001 |
| V2 | Sign-in methods | Branded page; method per tenant setting | US1 |
| V3 | Host unchanged | iptables/nft diff before vs after = only Docker's rules for Malgary's published ports; other apps' health checks unchanged | US2, FR-003 |
| V4 | Memory | `docker stats` for Malgary containers ≤ limits, total ≤ 3 GB | US2, SC-004 |
| V5 | Isolation | `malgaryctl isolation-test` → all private targets blocked, public OK | US3, SC-002 |
| V6 | Isolation gate | New stack hash without a passing test → users can't reach the gateway | US3, FR-009 |
| V7 | Interim limits | 11th user refused (`interim_user_limit`); non-test tenant refused | FR-010 |
| V8 | Backups | Nightly object in the bucket; `malgaryctl backup verify` passes | US4, SC-005 |
| V9 | Restore gate | Real-user import refused before V8 passes | FR-015 |
| V10 | Coolify cleanup | Run Coolify's Docker cleanup; Malgary containers still running | FR-006 |
| V11 | Move rehearsal | Move testco to another host; existing client reconnects unchanged | US5, SC-006 |
| V12 | Removal | `interim-remove.yml` leaves nothing of Malgary on 178 | FR-017 |
