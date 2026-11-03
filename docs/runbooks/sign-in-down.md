# Runbook: Malgary sign-in down

**Alert**: `SignInDown` (Zitadel). Connected users are unaffected; new sign-ins and invitations fail.

1. `cd /opt/malgary/zitadel && docker compose ps && docker compose logs --tail 200 zitadel login`.
2. PostgreSQL healthy? If not, see [postgres-down](postgres-down.md).
3. `docker compose up -d`. Certificate errors: check Traefik logs and `/opt/malgary/traefik/acme/acme.json`.
