# Runbook: Resize a gateway pair (one short reconnect)

Principle IX: a variable change only. US7, quickstart V11.

0. **Restore rehearsal first** (Operational Standards: before every tier change): run [restore-cell](restore-cell.md) into staging and record the result.
1. Edit `infra/envs/pilot/terraform.tfvars`: the pair's `size`.
2. Find the standby: Grafana `gateway_is_master == 0`.
3. `tofu apply -target='module.gateway_pair["gw-nyc-1"].digitalocean_droplet.member["b"]'` (the standby). It powers off, resizes, returns as BACKUP.
4. Fail over to it: on the current master `systemctl stop keepalived`; watch the egress probe recover within 60 seconds.
5. `tofu apply` for the old primary, then `systemctl start keepalived` on it; it stays BACKUP (`nopreempt`).
6. `tofu plan` shows no changes. Record the reconnect time.
