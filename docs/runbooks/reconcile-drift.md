# Runbook: Reconcile drift

**Alert**: `ReconcileDrift` (constitution Principle X).

1. Latest runs: `SELECT tenant_id, status, drift, actions FROM reconcile_run ORDER BY started_at DESC LIMIT 20;`
2. "user in NetBird but not in Malgary": someone added a user in the NetBird dashboard. Customers must use the portal; either import the user (`malgaryctl users import`) or delete them in NetBird.
3. "stack spec not applied" repeating: the deploy fails; see `tenant_stack.last_report` and [tenant-stack-down](tenant-stack-down.md).
4. Errors talking to NetBird: PAT expired? Tokens last 365 days; rotate by re-running bootstrap (`malgaryctl stack reconcile`).
