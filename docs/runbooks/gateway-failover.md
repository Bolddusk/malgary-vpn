# Runbook: Gateway failover

**Alerts**: `GatewayFailover`, `GatewaySplitBrain` · **Spec**: US2, FR-008–FR-010

## What happened

keepalived on the standby stopped hearing the active gateway (or the active one's health check
failed) and asked the DigitalOcean API to move the pair's reserved IP to itself. Clients
reconnect within 60 seconds and keep the same egress IP.

## Check

1. Grafana → Gateway pair dashboard: which host is `gateway_is_master = 1` now?
2. On the new master (over the ops VPN): `journalctl -t reserved-ip-notify -n 50` shows
   "reserved IP … is assigned to this droplet".
3. From a client: `curl https://api.ipify.org` returns the reserved IP.
4. On the failed node: `journalctl -u keepalived -n 100` and `/usr/local/bin/gw-health; echo $?`
   tell you why it was demoted (route, NAT table, routing peers or split brain).

## Fix

- **Node crashed or rebooted**: once healthy it rejoins as BACKUP (`nopreempt`); nothing to do.
- **Health check failing**: fix the cause (re-run `bin/run site.yml --limit <host>`), then check
  that `gw-health` exits 0.
- **Split brain** (`GatewaySplitBrain`): both nodes believed they were master. Check the DO
  console for the reserved IP's droplet. Stop keepalived on the node that doesn't hold it
  (`systemctl stop keepalived`), confirm traffic, then start it again: it returns as BACKUP.

## Afterwards

Record the time to recovery from the egress probe and add it to the incident log.
