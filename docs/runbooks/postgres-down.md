# Runbook: PostgreSQL down

**Alert**: `PostgresDown` · **Severity**: critical — every tenant stack, Zitadel and malgaryd depend on it.

1. `systemctl status postgresql@17-main`, `journalctl -u postgresql@17-main -n 200`.
2. Disk full? `df -h /mnt/pgdata`. Grow the volume (`db_volume_gb` in tfvars, `tofu apply`, `resize2fs`).
3. Volume detached? DO console → Volumes. Reattach, then `mount -a`.
4. Data corruption: stop, then [restore-cell](restore-cell.md) databases only from the latest backup (RPO 24 hours).
