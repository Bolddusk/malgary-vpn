# Runbook: Resize the control host

Connected users are unaffected (Principle IV); sign-in, API and provisioning pause for a few minutes.

0. Restore rehearsal in staging first (Operational Standards).
1. Announce a 15-minute maintenance window to tenant admins.
2. Edit `control_host_size` in `infra/envs/pilot/terraform.tfvars`, `tofu apply`.
3. After boot: `systemctl status postgresql@17-main malgaryd`, `docker ps` shows every `nb-*` stack, Zitadel and Traefik.
4. `malgaryctl tenant list`: every tenant `active / applied`.
