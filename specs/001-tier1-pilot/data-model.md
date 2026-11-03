# Data Model: Tier 1 Pilot

**Feature**: [spec.md](spec.md) | **Research**: [research.md](research.md)

This is the Malgary backend's own database (`malgary`), the system of record for placement and
commercial data (Principle X). NetBird stacks remain the system of record for network state.
The reconciler compares the two.

Conventions:
- IDs are UUIDv7.
- Timestamps are UTC `timestamptz`.
- Money is `amount_minor bigint` plus `currency char(3)`.
- Rows marked **append-only** are never updated or deleted.
- Every table that holds tenant data has a `tenant_id`, and every query is scoped by it.

## 1. Placement

### Cell
| Field | Type | Notes |
| --- | --- | --- |
| id | uuid | |
| code | text unique | e.g. `c01` |
| region | text | DO region, e.g. `nyc1` |
| status | enum | `active`, `draining`, `retired` |
| max_users | int | soft capacity, used by placement |

### Host
| Field | Type | Notes |
| --- | --- | --- |
| id | uuid | |
| cell_id | fk Cell | |
| role | enum | `control`, `gateway`, `relay` |
| name | text unique | DNS name, e.g. `ctl-1.c01.<domain>` |
| size_slug | text | from IaC, informational |
| status | enum | `active`, `draining`, `down` |

### GatewayPair
| Field | Type | Notes |
| --- | --- | --- |
| id | uuid | |
| cell_id | fk Cell | |
| name | text | e.g. `gw-nyc-1` |
| egress_ip | inet unique | reserved IP |
| pta_registered_at | timestamptz null | FR-011. Pair can't carry traffic while null. |
| pta_reference | text null | PTA registration reference |
| primary_host_id, standby_host_id | fk Host | |
| dedicated_tenant_id | fk Tenant null | set for dedicated-IP add-on |

### Tenant
| Field | Type | Notes |
| --- | --- | --- |
| id | uuid | |
| slug | text unique | DNS-safe, e.g. `onedge`. Gives `onedge.vpn.<domain>` |
| legal_name | text | |
| status | enum | `provisioning`, `active`, `suspended`, `offboarding`, `deleted` |
| cell_id | fk Cell | exactly one (Principle V) |
| created_at | timestamptz | |

State transitions: `provisioning → active → suspended ⇄ active`, then `active|suspended →
offboarding → deleted`. Suspension is driven only by subscription rules or an owner action.

### TenantStack
| Field | Type | Notes |
| --- | --- | --- |
| tenant_id | pk, fk Tenant | one stack per tenant |
| control_host_id | fk Host | |
| netbird_version | text | e.g. `0.82.0`. Staged rollouts change it. |
| management_url | text | `https://<slug>.vpn.malgary.com` |
| http_port | int | local port behind Traefik, 20000–29999, unique per control host |
| db_name | text | `nb_<slug>` |
| service_token_ref | text | secret reference, never the value |
| desired_hash, applied_hash | text | render output hash. Drift if they differ. |
| status | enum | `pending`, `applied`, `failed` |

### RoutingPeer
| Field | Type | Notes |
| --- | --- | --- |
| id | uuid | |
| tenant_id | fk Tenant | |
| gateway_pair_id | fk GatewayPair | |
| host_id | fk Host | primary or standby |
| netbird_peer_id | text | peer ID in the tenant stack |
| wg_interface, wg_port | text, int | unique per host |
| metric | int | primary 100, standby 200 |

Unique: (`host_id`, `wg_port`), (`host_id`, `wg_interface`), (`tenant_id`, `host_id`).

## 2. Identity and users

### IdentityLink
| Field | Type | Notes |
| --- | --- | --- |
| tenant_id | pk, fk Tenant | |
| zitadel_org_id | text | |
| zitadel_app_client_id | text | one OIDC app per tenant org (avoids zitadel#11869) |
| upstream_idps | jsonb | e.g. `[{"type":"google","domain":"onedge.co"}]` |
| allowed_domains | text[] | FR-003. Empty means allowlist only. |
| sign_in_methods | text[] | subset of `sso`, `managed` (CHECK constraint; text because pgx can't encode enum arrays). Chosen by the tenant (FR-002). Non-empty. |
| mfa_required | bool | always true for `managed`; for `sso`, true adds Malgary MFA on top of the provider's |

### TenantUser
| Field | Type | Notes |
| --- | --- | --- |
| id | uuid | |
| tenant_id | fk Tenant | |
| email | citext | unique per tenant |
| display_name | text | |
| role | enum | `member`, `tenant_admin` |
| status | enum | `invited`, `active`, `disabled`, `removed` |
| zitadel_user_id | text null | |
| netbird_user_id | text null | filled after first login |
| seat_held | bool | true from `active` until `removed` |

A seat is used by a user, not a device (spec edge case). The seat count is the number of users
with `seat_held = true` and must be ≤ the subscription quantity (FR-016).

### Group
| Field | Type | Notes |
| --- | --- | --- |
| id | uuid | |
| tenant_id | fk Tenant | |
| name | text | unique per tenant |
| netbird_group_id | text null | mirrored |
| kind | enum | `team`, `system` (`all-users`, and `user:<id>` single-user groups for per-user rules) |

GroupMember: (`group_id`, `tenant_user_id`).

## 3. Network policy

### RoutingRule
| Field | Type | Notes |
| --- | --- | --- |
| id | uuid | |
| tenant_id | fk Tenant | |
| scope | enum | `tenant`, `group`, `user` |
| scope_ref | uuid null | group or tenant user. Null for `tenant`. |
| mode | enum | `full_tunnel`, `destinations` |
| destinations | cidr[] / text[] | required and non-empty when `mode = destinations` |
| version | int | optimistic locking |

Resolution (FR-005), computed by the backend and not by NetBird:
1. A user's effective rule is their `user` rule if one exists.
2. Otherwise, the union of their groups' rules, where `full_tunnel` wins (spec edge case).
   Conflicts are flagged in the UI.
3. Otherwise, the `tenant` rule.

The backend applies the result by placing each user in exactly one system routing group in
NetBird (`route:full` or `route:dest:<hash>`). NetBird routes are distributed to those groups.
NetBird's additive route behaviour then can't produce an unintended mix.

Call traffic exclusion (FR-006) is a tenant-wide list of destination ranges. It is always
excluded from routes, whatever the rule.

### PostureRule
| Field | Type | Notes |
| --- | --- | --- |
| id | uuid | |
| tenant_id | fk Tenant | |
| min_os | jsonb | per-OS minimum versions |
| min_client_version | text | |
| applies_to_group_id | fk Group null | null means all users |
| netbird_posture_check_id | text null | |

## 4. Commercial (Principle XI)

### Plan
| Field | Type | Notes |
| --- | --- | --- |
| id | uuid | |
| code | text unique | e.g. `business`, `ent-onedge` |
| visibility | enum | `public`, `enterprise` |
| enterprise_tenant_slug | text null | required when `visibility = enterprise`; a slug (not an FK) so the plan can exist before its tenant |
| name | text | |
| status | enum | `active`, `retired` (retired: no new subscriptions) |

### PlanVersion (immutable once `published`)
| Field | Type | Notes |
| --- | --- | --- |
| id | uuid | |
| plan_id | fk Plan | |
| version | int | 1, 2, 3, … unique per plan |
| status | enum | `draft → published → superseded` |
| price_per_seat_minor | bigint | e.g. 400 |
| currency | char(3) | e.g. `USD` |
| billing_period | enum | `monthly`, `annual` |
| min_seats, max_seats | int, int null | |
| entitlements | jsonb | e.g. `{"posture_checks":true,"dedicated_ip":false,"usage_reports":"basic","max_groups":50}` |
| terms | text | enterprise-specific terms |
| published_at, published_by | | |

A database trigger rejects any update to a published version except `status → superseded`.

### Subscription
| Field | Type | Notes |
| --- | --- | --- |
| id | uuid | |
| tenant_id | fk Tenant | one non-terminal subscription per tenant |
| plan_version_id | fk PlanVersion | stays fixed until an explicit migration |
| quantity | int | paid seats |
| status | enum | `trialing`, `active`, `past_due`, `suspended`, `cancelled` |
| current_period_start, current_period_end | date | |
| payment_provider | text | `manual` in the pilot |
| provider_ref | text null | e.g. a future Stripe subscription ID |

### SubscriptionEvent (append-only, the history)
`id, subscription_id, type (created | quantity_changed | plan_migrated | status_changed |
period_renewed), from_value jsonb, to_value jsonb, actor, reason, at`.

### Invoice (immutable once `issued`)
`id, tenant_id, subscription_id, number (sequential per issuer, e.g. MV-2026-000001), period,
lines jsonb (plan version, quantity, unit price, proration), subtotal_minor, tax_minor,
total_minor, currency, status (draft | issued | paid | void), issued_at, due_at`. Corrections
are made with a CreditNote that references the invoice.

### Payment (append-only)
`id, invoice_id, provider (manual | stripe | payoneer | …), provider_payment_ref, amount_minor,
currency, received_at, method_note, proof_object_key (Spaces), recorded_by`.

### PaymentProviderEvent (append-only, idempotency)
`provider, provider_event_id (unique together), payload jsonb, processed_at`. Unused by the
manual provider. In place now so gateway webhooks need no schema change.

## 5. Operations

### AuditRecord (append-only, FR-026)
`id, at, actor_type (owner | tenant_admin | system), actor_id, tenant_id null, action (e.g.
tenant.created, routing_rule.updated, payment.recorded), target_type, target_id, before jsonb,
after jsonb, request_id, source_ip`. Written in the same transaction as the change. The
`malgary` DB role has INSERT but no UPDATE or DELETE on this table.

### UsageSample
`tenant_id, tenant_user_id, netbird_peer_id, sampled_at, connected bool, last_seen,
rx_bytes, tx_bytes` (from routing-peer WireGuard counters, research R9). Kept 90 days, then
rolled up daily.

### ReconcileRun
`id, tenant_id, started_at, finished_at, drift jsonb, actions jsonb, status`. Drift raises an
alert (Principle X).
