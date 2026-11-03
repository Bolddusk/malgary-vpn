# Contract: Interim additions to the backend API

Added to `backend/api/openapi.yaml`; all are no-ops (404) when `MALGARY_INTERIM=false`.

| Method & path | Who | Purpose | Spec |
| --- | --- | --- | --- |
| `POST /v1/tenants/{id}/isolation-tests` | operator | Record a result `{stackHash, passed, egressIp, results[]}` from `malgaryctl isolation-test`. A pass for the current hash enables the tenant's access policy. | FR-009 |
| `GET /v1/tenants/{id}/isolation-tests` | owner, operator | History | FR-009 |
| `POST /v1/backups/restore-tests` | operator | Record a restore test `{backupKey, backupTakenAt, passed, details}` | FR-015 |
| `GET /v1/interim` | owner, operator (tenants see the egress note on their setup page) | `{active, maxUsers, usersUsed, until, daysLeft, egressNote}` | FR-010–FR-012 |

Changed (not interim-only), FR-012a: when `MALGARY_INVITE_DELIVERY=link`, the user import
response includes `inviteLink` (one-time Zitadel invitation URL) for each managed-account user
created. The link is shown once and never stored by Malgary. `malgaryctl users import` prints
them. SSO-only tenants get no links.

Fixed error codes added: `interim_user_limit`, `interim_expired`, `interim_tenant_not_allowed`,
`isolation_test_required`, `restore_test_required`.
