# Runbook: Restore a cell from backup

Target: under 4 hours, data loss no more than the last daily backup (SC-007, quickstart V12).

1. `tofu apply` in the env (recreates missing Droplets, volumes, IPs keep their reservation).
2. `bin/run site.yml` (base, PostgreSQL, Traefik, Zitadel, backend).
3. `AGE_IDENTITY=<owners' backup key> bin/run restore.yml -e restore_date=YYYY/MM/DD` restores every database and `/etc/malgary`, `/opt/malgary/stacks|specs`.
4. `systemctl restart malgaryd`; it redeploys any stack whose applied hash differs (reconciler, within 10 minutes; or `malgaryctl stack reconcile <tenant>`).
5. Verify: clients reconnect without reconfiguration; `GET /v1/tenants` all `active`; egress IP unchanged.
6. Record the duration and the backup age in `specs/001-tier1-pilot/pilot-results.md`.
