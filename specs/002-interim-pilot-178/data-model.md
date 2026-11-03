# Data Model: Interim Pilot (delta on feature 001)

Feature 001's data model ([../001-tier1-pilot/data-model.md](../001-tier1-pilot/data-model.md))
applies unchanged. This feature adds two tables (migration `0005_interim.sql`) and one config
block. Nothing is removed, so moving to a dedicated cell needs no data migration.

## IsolationTestResult (append-only)

| Field | Type | Notes |
| --- | --- | --- |
| id | uuid | |
| tenant_id | fk Tenant | |
| stack_hash | text | the StackSpec hash tested; a new hash needs a new pass |
| ran_at | timestamptz | |
| passed | bool | |
| egress_ip | inet null | observed egress IP |
| results | jsonb | `[{"target":"178.104.48.140:5432","expected":"blocked","got":"blocked"}, …]` |
| ran_by | text | actor |

Rule: a tenant's access policy is enabled only when the latest result for its current
`desired_hash` has `passed = true` (FR-009).

## BackupRestoreTest (append-only)

| Field | Type | Notes |
| --- | --- | --- |
| id | uuid | |
| backup_key | text | object key of the backup set restored |
| backup_taken_at | timestamptz | |
| tested_at | timestamptz | |
| passed | bool | |
| details | jsonb | per-database row-count comparison |

Rule: importing users into a tenant not in `MALGARY_INTERIM_TEST_TENANTS` — or any user marked as
a real company user — is refused until one row has `passed = true` (FR-015).

## Interim configuration (environment, not a table)

| Setting | Pilot value | Meaning |
| --- | --- | --- |
| `MALGARY_INTERIM` | `true` | turns the limits on |
| `MALGARY_INTERIM_MAX_USERS` | `10` | users across all tenants (FR-010) |
| `MALGARY_INTERIM_UNTIL` | `2027-02-28` | refuse imports after; 30-day warning (FR-011) |
| `MALGARY_INTERIM_TEST_TENANTS` | `onedge,testco,malgary-ops` | tenants allowed |
| `MALGARY_INTERIM_EGRESS_NOTE` | `Temporary test IP; do not allowlist` | FR-012 |
| `MALGARY_DEPLOYER` | `compose` | selects the ComposeDeployer (I1) |
