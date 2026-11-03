# Runbook: Both gateways down / no master

**Alerts**: `GatewayBothDown`, `GatewayPairNoMaster` · **Severity**: critical

Users can't reach anything through the VPN. Calls are unaffected because they never use it.

1. DigitalOcean status page: is NYC1 affected? If yes, post a customer notice and wait.
2. DO console: are both droplets running? Power-cycle the primary first.
3. If droplets run but no master: reserved IP unassigned? Assign it to the healthy droplet in
   the DO console, then check `journalctl -u keepalived` on both.
4. If a droplet is lost: `tofu apply` in `infra/envs/pilot` recreates it, then
   `bin/run site.yml --limit <host>` and `malgaryctl stack reconcile <tenant>` for each tenant
   so routing peers return.
5. Confirm from a client that the egress IP is the reserved IP, then close the incident.
