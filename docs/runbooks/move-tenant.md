# Runbook: Move a tenant to another control host

Principle V, FR-030, quickstart V15. Clients don't change: their management URL is DNS.

1. Ensure the target host is in the cell inventory (`deploy/cells/<cell>.yaml`) and malgaryd restarted.
2. Freeze changes for the tenant (tell the tenant admin; ~10 minutes of sign-in downtime for that tenant only).
3. On the source host: `docker compose -p nb-<tenant> stop`, `pg_dump -Fc nb_<tenant> > /tmp/nb_<tenant>.dump`.
4. Restore into the target host's PostgreSQL (create role/db with the same password from `/etc/malgary/secrets/tenants/<tenant>/db`), `pg_restore -d nb_<tenant>`.
5. Copy `/etc/malgary/secrets/tenants/<tenant>/` to the target host.
6. `UPDATE tenant_stack SET control_host_id = '<target host id>', http_port = <free port on target> WHERE tenant_id = '<id>';`
7. `malgaryctl stack reconcile <tenant>`: renders and deploys the stack on the target, writes the Traefik route there.
8. Point DNS: in the pilot `*.vpn` covers it; with several control hosts, add an `A` record for `<tenant>.vpn` to the target.
9. Verify sign-in and an existing client reconnecting, then `docker compose -p nb-<tenant> down -v` on the source.
