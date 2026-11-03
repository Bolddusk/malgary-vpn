# Runbook: Backend down

**Alert**: `BackendDown` (malgaryd). Tunnels and sign-in keep working; provisioning, the API and the portal don't.

1. `systemctl status malgaryd`, `journalctl -u malgaryd -n 200`.
2. Migration failure at start-up: fix forward with a new migration; never edit an applied one.
3. Config error: check `/etc/malgary/malgaryd.env` against `roles/backend/templates/malgaryd.env.j2`, re-run `bin/run site.yml --limit control --tags backend`.
