# Quickstart and Validation: Tier 1 Pilot

How to stand up the pilot and prove each success criterion. Commands show intent. The exact
flags come from the implementation.

## Prerequisites

- DigitalOcean account and API token; a domain with DNS on DigitalOcean; a Spaces key.
- Request a reserved-IP limit increase if you need more than 3 (needed from Tier 2).
- Grafana Cloud stack (free) and an email provider API key (HTTPS API, since SMTP ports are
  blocked on DO).
- A Google Workspace for tenant #1. A Microsoft Entra test tenant for the test tenant.
- OpenTofu, Ansible, Go, and `age` for backup encryption, on the operator machine.
- Reserved IP registered with the PTA before any pilot user connects (FR-011).

## Stand up

1. `tofu -chdir=infra/envs/pilot apply` creates cell `c01`: VPC, firewall, control host,
   gateway pair, reserved IP, DNS and Spaces buckets.
2. `ansible-playbook infra/ansible/site.yml -l c01` configures the hosts. This includes
   Postgres, Traefik, Zitadel, the backend, Alloy, keepalived, the anchor route and backups.
3. `malgaryctl plan apply -f deploy/plans/ent-onedge.yaml` creates and publishes the enterprise
   plan, version 1, at $4 per seat.
4. `malgaryctl tenant apply -f deploy/tenants/onedge.yaml` creates tenant #1. See
   [tenant-definition](contracts/tenant-definition.md).
5. `malgaryctl tenant apply -f deploy/tenants/testco.yaml` creates the Entra test tenant.
6. `malgaryctl users import onedge pilot-users.csv` sends invitations to the 10 pilot users.

Expected: `malgaryctl tenant get onedge` shows `active`, stack `applied`, and routing peers on
both gateways.

## Validation scenarios

| # | Scenario | Steps | Pass when | Spec |
| --- | --- | --- | --- | --- |
| V1 | Self-service onboarding | A pilot user gets only the link `https://onedge.vpn.<domain>/setup` | Connected in under 10 minutes, no help. "What is my IP" shows the reserved IP. | US1, SC-001 |
| V2 | Company sign-in and MFA | Sign in with a Workspace account. Then try a personal Gmail. | Workspace works, with MFA. Gmail is refused. | FR-002/003 |
| V3 | Gateway failover | During an active download and SSH session, power off the active gateway. Repeat 5 times. | Traffic resumes in under 60 seconds and egress IP is unchanged, every time. Alert received. | US2, SC-002 |
| V4 | Failback | Power the failed gateway back on | It rejoins as standby with no second interruption. Never both active. | FR-010 |
| V5 | Tenant isolation | Testco user signs in to onedge URL, and the reverse. Admin token of one tenant calls the other's API. | All refused. The API returns `404`. | US3, SC-004 |
| V6 | Routing rules | Set group A to full tunnel and group B to destinations, then a user override in B | Effective rules match within 5 minutes. Audit records exist. Empty-list save is rejected. | US5, FR-005 |
| V7 | Posture | Device below the minimum OS connects, then upgrades | Blocked within 1 minute, allowed within 1 minute of upgrade | US6, SC-005 |
| V8 | Seat limit | Import an 11th user with quantity 10 | Refused with `seat_limit_reached`, and operator alerted | FR-016 |
| V9 | Plan versioning | Publish version 2 of the plan at a new price | onedge still on version 1, history shows both versions. Editing version 1 fails with `plan_version_immutable`. | FR-016b |
| V10 | Manual payment | Issue an invoice for the period and record payment with reference and proof | Invoice `paid`, payment and audit rows exist | FR-016c |
| V11 | Grow to 50 | Change sizes in `infra/envs/pilot` and set quantity 50, then apply | At most one reconnect under 60 seconds. No client reconfigured. | US7, SC-006 |
| V12 | Rebuild drill | In staging: destroy everything, then apply and restore the latest backup | Done in under 4 hours. Existing clients reconnect unchanged. | SC-007 |
| V14 | Managed account sign-in | testco set to `[sso, managed]`. Invite an address with no Entra account. | Must set a password and MFA at first sign-in. Sign-in without MFA fails. | FR-002, FR-013 |
| V15 | Tenant move | In staging with 2 control hosts, move testco to the second host per the runbook | Users reconnect without reconfiguring. No data lost. | FR-030 |
| V13 | Monitoring | Stop a tenant stack container | Alert in 2 minutes with a runbook link | FR-024 |

## Pilot research tests (from research.md)

| # | Question | Method | Decides |
| --- | --- | --- | --- |
| P-1 | Reserved IP reassignment time, and egress from the new holder | Measure with a 1-second probe loop to an external IP echo service during failover | Failover meets 60 seconds (R3) |
| P-2 | Several tenant routing-peer containers on one gateway host | Run onedge and testco peers together; check interfaces, ports and NAT | Shared-gateway model (R2) |
| P-3 | Zitadel as the only connector in the embedded IdP: skips the chooser? Group sync works? | Sign in through both tenants. Inspect the NetBird user's groups. | Identity layer, final (R4, FR-028) |
| P-4 | iOS Change Server flow | Pilot user on iOS | Setup page content (R10) |
| P-5 | Per-peer byte counters from routing peers | Compare `wg show transfer` with a known download | Usage reporting (R9) |
| P-7 | Excluding call ranges from full tunnel | Exit node plus call exclusions; else the complement route set (R12). Check that a Meet call's traffic leaves locally. | FR-006 approach |
| P-6 | Zitadel email delivery on DO | Send an invitation via the provider on port 2525 or HTTPS API | Email path (R9) |

Record each result in `specs/001-tier1-pilot/pilot-results.md` with a date and the evidence.
