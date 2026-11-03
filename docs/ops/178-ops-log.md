# Operations log: interim server 178.104.48.140

Every command run against the shared Hetzner/Coolify server from the operator's Mac (feature
002). Owner's rule (2026-11-04): Claude may run SSH and Ansible from the Mac, carefully, and
logs everything here. Full Ansible output for each run is kept in `infra/ansible/logs/`
(git-ignored, may contain host details). Secrets are never printed (`no_log`).

| When (UTC) | Who | What and why | Command | Result |
| --- | --- | --- | --- | --- |
| 2026-11-03 19:18 | owner | Create the provisioner login and its password file | `CREATE ROLE malgary_provisioner …` (Coolify terminal); password to `/opt/malgary/secrets/provisioner-password` | done, checked over TCP |
| 2026-11-04 (approx.) | owner | Read-only facts by hand | 12-section check block | matched the plan except docker0 = 10.0.0.1 and wireguard not loaded |
| 2026-11-04 (approx.) | owner | SSH key for the Mac | `malgary-178` key appended to `/root/.ssh/authorized_keys` | key login works |
| 2026-11-03 22:02 | Claude | Read-only check (no changes) | `ansible-playbook -i inventory/interim.yml interim-check.yml` | passed, all assertions ok |
| 2026-11-03 22:2x | Claude | Install attempt 1 | `ansible-playbook interim-site.yml -e interim_owner_email=bolddusk@gmail.com` | stopped at the first read-only snapshot step (`docker ps --filter label!=` unsupported on Docker 27); **no changes made** |
| 2026-11-03 22:3x | Claude | Test the fixed container listing (read-only) | `ssh root@178… "docker ps --format … \| grep -v 'malgary=true$' …"` | 91 containers listed, ok |
