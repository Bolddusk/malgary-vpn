# Runbook: Tenant stack down

**Alert**: `TenantStackUnhealthy`

1. Over the ops VPN: `ssh ctl-1.c01.vpn.malgary.com`, `cd /opt/malgary/stacks/<tenant>`, `docker compose -p nb-<tenant> ps` and `logs --tail 200 netbird-server`.
2. Database reachable? `psql -h host.docker.internal -U nb_<tenant> nb_<tenant>` from the host (password in `/etc/malgary/secrets/tenants/<tenant>/db`).
3. Restart: `docker compose -p nb-<tenant> up -d`. If a recent upgrade caused it, the deploy already rolled back; check `compose.yaml.prev`.
4. Still failing: `malgaryctl stack reconcile <tenant>` (redeploys from the StackSpec), then escalate.
Connected users of that tenant keep their tunnels; new sign-ins for that tenant fail until it's back.
