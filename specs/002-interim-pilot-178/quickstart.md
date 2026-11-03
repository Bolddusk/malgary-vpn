# Quickstart and Validation: Interim Pilot on 178

## Prerequisites (owner)

- Root SSH to 178.104.48.140 for the operator's key.
- Cloudflare DNS-only A records `*.vpn`, `id.vpn`, `api.vpn` → 178.104.48.140 (done 2026-11-03).
- PostgreSQL superuser password (Coolify UI), used once to create `malgary_provisioner`.
- Grafana Cloud stack (free) for metrics and alerts (optional during development).
- No Hetzner Cloud Firewall is attached, so no port opening is needed (owner, 2026-11-03).
- Deferred until before real users: Hetzner Object Storage bucket + keys and owners' `age` key
  (backups), and email relay credentials for `mail.laxycone.com:587`.

## Stand up (each step needs the owner's go-ahead)

1. `bin/run interim-check.yml` — read-only: Coolify proxy entrypoint and resolver names, Docker
   cleanup settings, free memory. Writes `interim-facts.yml`.
2. `bin/run interim-site.yml` — creates `/opt/malgary`, the `malgary-gw` network, the provisioner
   role (operator enters the superuser password once), Zitadel, malgaryd and (if configured)
   Alloy containers. Snapshot of the host's iptables/nftables before and after (V3).
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
| V8 | Backups (deferred) | Nightly object in the bucket; `malgaryctl backup verify` passes | US4, SC-005 |
| V9 | Restore gate | Non-test-domain user import refused (`restore_test_required`) while no restore test has passed | FR-015 |
| V9a | Invitation links | Managed-account import returns a one-time link; opening it lets the user set a password and MFA | FR-012a |
| V10 | Coolify cleanup | Run Coolify's Docker cleanup; Malgary containers still running | FR-006 |
| V11 | Move rehearsal | Move testco to another host; existing client reconnects unchanged | US5, SC-006 |
| V12 | Removal | `interim-remove.yml` leaves nothing of Malgary on 178 | FR-017 |
