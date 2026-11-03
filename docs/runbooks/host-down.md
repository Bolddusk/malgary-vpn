# Runbook: Host down

**Alert**: `HostDown` · **Severity**: critical

1. Which host? Gateway → follow [gateway-failover](gateway-failover.md) (the standby should already hold the IP). Control host → continue.
2. DO console: is the Droplet running? If not, power it on and watch the console.
3. Control host down means: no new sign-ins, no management changes, no API. Connected users keep working (Principle IV).
4. If it doesn't come back within 30 minutes, start [restore-cell](restore-cell.md) onto a new control host (RTO 4 hours).
5. After recovery: `malgaryctl stack reconcile <tenant>` for every tenant, check the `ReconcileDrift` alert clears.
