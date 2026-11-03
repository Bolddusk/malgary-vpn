# Runbook: Backup missed

**Alert**: `BackupMissed` (no success for 26 hours, FR-025).

1. `systemctl status malgary-backup.service`, `journalctl -u malgary-backup -n 100`.
2. Spaces credentials expired or bucket lifecycle changed? Test: `rclone --config /etc/malgary/rclone.conf lsd spaces:`.
3. Run it now: `systemctl start malgary-backup.service`; confirm the alert clears.
