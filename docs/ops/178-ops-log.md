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
| 2026-11-03 22:08 | Claude | Install attempt 2 | `ansible-playbook interim-site.yml -e interim_owner_email=bolddusk@gmail.com` | base done (dirs, secrets, `malgary-gw` network, wireguard loaded, databases `malgary`/`malgary_zitadel`, citext, cell file); Zitadel failed its first setup: admin password needs a symbol (restart loop, own database only) |
| 2026-11-03 22:1x | Claude | Read Zitadel status and logs (read-only) | `ssh root@178… "docker ps -a --filter name=malgary-id …; docker logs --tail 40 malgary-id-zitadel-1"` | `PasswordComplexityPolicy.HasSymbol` |
| 2026-11-03 22:2x | Claude | Make the first admin password meet Zitadel's policy | `ssh root@178… 'grep -q "#" f \|\| printf "#Mv9" >> /opt/malgary/secrets/zitadel-first-admin-password'` | 28 chars, owner 65532, mode 600 |
| 2026-11-03 22:10 | Claude | Install attempt 3 | `ansible-playbook interim-site.yml -e interim_owner_email=bolddusk@gmail.com` | **succeeded** (ok=43, failed=0): Zitadel + login healthy, malgaryd up, `id.vpn.malgary.com` 200, `api.vpn.malgary.com/healthz` 204; Alloy skipped (no Grafana Cloud) |
| 2026-11-03 22:1x | Claude | Verify (read-only) | `ssh root@178… "docker ps --filter label=com.malgary.managed=true …; docker network ls …; lsmod"` | 3 containers up, `malgary-gw` present, wireguard loaded |
| 2026-11-03 22:1x | Claude | Host unchanged check (V3) | `infra/tools/interim-diff.sh before-1793743690 after-1793743841` | only 8 Docker rules for `br-malgary-gw` (isolation, no inter-container traffic, masquerade 10.250.0.0/24); other apps' containers, NAT, sysctls unchanged |
| 2026-11-03 22:20 | Claude | Create testco through the API | `malgaryctl plan apply -f deploy/plans/ent-testco.yaml`; `malgaryctl tenant apply -f deploy/tenants/interim/testco.yaml` | plan published; stack `nb-testco` deployed (server, setup, routing-peer, guard up); provisioning failed: backend couldn't find the gateway peer |
| 2026-11-03 22:2x | Claude | Diagnose (read-only) | `docker logs nb-testco-routing-peer-1`, `docker logs nb-testco-guard-1`, `GET /api/peers` with testco's PAT (on the server) | peer joined as `gw.i01` but hostname = container ID; gRPC streams cut every 60 s with 504 (Traefik default readTimeout) |
| 2026-11-03 22:3x | Claude | Read Coolify proxy flags (read-only) | `grep … /data/coolify/proxy/docker-compose.yml` | no respondingTimeouts set (Traefik default 60 s) |
