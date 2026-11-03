# Contract: Malgary Backend API v1

The HTTP JSON API of the Malgary backend. In the pilot it is called by the `malgaryctl` CLI
(owners and operators). In feature 002 the client portal and back-office call the same API.
The full OpenAPI 3.1 file is produced during implementation at
`backend/api/openapi.yaml` and must match this contract.

## Conventions

- Base path `/v1`. JSON only. Errors use `application/problem+json` with
  `{type, title, status, detail, code}`.
- **Auth**: OIDC bearer tokens from Zitadel. Claims give `actor_type` (`owner`, `operator`,
  `tenant_admin`) and, for tenant admins, `tenant_id`.
- **Tenant scoping**: a `tenant_admin` token can only reach `/v1/tenants/{its own id}/…`.
  Any other tenant ID returns `404`, not `403`, so tenant existence doesn't leak
  (Principle X).
- **Idempotency**: every `POST` accepts `Idempotency-Key`. A repeat returns the first result
  (FR-015).
- **Concurrency**: mutable resources return `ETag`. Updates require `If-Match`, and stale
  updates return `412`.
- **Audit**: every mutating call writes one AuditRecord in the same transaction (FR-026).
- **Pagination**: `?cursor=&limit=` (max 200).

## Resources

| Method & path | Who | Purpose | Spec |
| --- | --- | --- | --- |
| `POST /v1/tenants` | owner | Create tenant from a [tenant definition](tenant-definition.md). Places it in a cell and starts stack provisioning. | FR-014 |
| `GET /v1/tenants/{id}` | owner, tenant_admin | Tenant, status, stack status, management URL | |
| `POST /v1/tenants/{id}:suspend` / `:resume` | owner | Suspend or resume (disables NetBird users, keeps data) | |
| `PUT /v1/tenants/{id}/identity` | owner | Set upstream IdPs, allowed domains, MFA | FR-002/003/013 |
| `POST /v1/tenants/{id}/users:import` | owner, tenant_admin | Bulk import `[{email, name, groups[]}]`. Sends invitations. Refuses past the seat limit. | FR-016 |
| `GET /v1/tenants/{id}/users` | owner, tenant_admin | Users, with status, last seen, connected and devices | |
| `PATCH /v1/tenants/{id}/users/{uid}` | owner, tenant_admin | Disable, enable, change role, change groups | |
| `DELETE /v1/tenants/{id}/users/{uid}` | owner, tenant_admin | Remove user and free the seat | edge case |
| `GET/POST/PATCH/DELETE /v1/tenants/{id}/groups…` | owner, tenant_admin | Manage groups | |
| `GET/PUT /v1/tenants/{id}/routing-rules…` | owner, tenant_admin | Tenant, group and user rules. Returns the effective rule per user and any conflicts. Empty destination lists are rejected (`422`). | FR-005 |
| `GET/PUT /v1/tenants/{id}/posture-rules…` | owner, tenant_admin | Posture rules | FR-018 |
| `GET /v1/tenants/{id}/usage?from=&to=&group=` | owner, tenant_admin | Per-user connected, last seen, bytes. Never-installed and inactive lists. | Principle X |
| `GET /v1/tenants/{id}/audit` | owner, tenant_admin | Audit records for the tenant | FR-026 |
| `GET/POST /v1/plans`, `POST /v1/plans/{id}/versions`, `POST /v1/plans/{id}/versions/{v}:publish` | owner | Plans and versions. Published versions are immutable. | FR-016b |
| `POST /v1/tenants/{id}/subscription` | owner | Subscribe the tenant to a plan version with a quantity and provider | FR-016a |
| `POST /v1/tenants/{id}/subscription:change` | owner | Change quantity or migrate the plan version (with reason). Writes a SubscriptionEvent. | FR-016b |
| `GET /v1/tenants/{id}/subscription/history` | owner, tenant_admin | Subscription events | FR-016b |
| `POST /v1/tenants/{id}/invoices` / `:issue` | owner | Draft and issue an invoice | FR-016c |
| `POST /v1/invoices/{id}/payments` | owner | Record a manual payment (amount, date, reference, proof upload) | FR-016c |
| `POST /v1/providers/{provider}/webhooks` | provider | Reserved for future gateways. Verifies the signature, then stores a PaymentProviderEvent. | Principle XI |
| `GET /v1/cells`, `GET /v1/cells/{id}/capacity` | owner, operator | Placement and capacity | Principle V |
| `POST /v1/tenants/{id}/stack:reconcile` | operator | Force a reconcile run | Principle X |
| `POST /v1/tenants/{id}/stack:report` | deployer | Report applied hash and component health ([tenant-stack](tenant-stack.md)) | Principle V |

## Errors with fixed codes

`seat_limit_reached`, `plan_version_immutable`, `entitlement_missing`,
`routing_rule_empty_destinations`, `tenant_suspended`, `idempotency_conflict`,
`stack_not_ready`.
