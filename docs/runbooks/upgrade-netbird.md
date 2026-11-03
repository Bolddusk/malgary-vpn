# Runbook: Upgrade NetBird (staged rollout)

Principle V (all tenant stacks on the same version, staged) and VI (security releases within 7 days, critical within 72 hours).

1. Read the release notes for breaking changes (store migrations, config keys).
2. Staging: set `MALGARY_NETBIRD_VERSION` and update `tenant_stack.netbird_version` for staging tenants; reconcile; run quickstart V1–V3.
3. Test tenant (testco): `UPDATE tenant_stack SET netbird_version='X' WHERE tenant_id=…`, `malgaryctl stack reconcile testco`. Health check failure rolls back automatically (tenant-stack contract obligation 5).
4. Wait 24 hours, then tenant #1, then everyone else in batches of 10.
5. Routing peers follow the stack version on the same reconcile.
6. Update the pinned version in `backend/go.mod` (REST client) when the API changes.
