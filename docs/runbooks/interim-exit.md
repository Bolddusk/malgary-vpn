# Runbook: leave the interim host

**When**: before the first real customer user, before an 11th user, or by **2027-02-28**,
whichever comes first (constitution, Interim Hosting Exception; feature 002 FR-016, FR-017).
The `InterimExitDue` alert fires 30 days before the date.

**Goal**: every tenant runs on a dedicated cell; users keep their apps and addresses; nothing
of Malgary is left on 178.104.48.140.

## 1. Prepare the dedicated cell

1. Create the cell with OpenTofu and Ansible as in [restore-cell.md](restore-cell.md)
   (`tofu apply` in `infra/envs/pilot`, then `site.yml`). Backups must be running there.
2. Register the new gateway pair's egress IP with the PTA before telling tenants about it.

## 2. Final copy from the interim host

On 178, stop changes first: `docker compose -p malgary-api stop` (the API and reconciler).
Then, as root:

```bash
# Databases, each with its own login (passwords in /opt/malgary/secrets).
for db in malgary malgary_zitadel $(ls /opt/malgary/stacks | sed 's/^/nb_/; s/-/_/g'); do
  docker exec <pg-container> pg_dump -Fc -h 127.0.0.1 -U malgary_provisioner -d "$db" > "/root/exit-$db.dump"
done
tar czf /root/exit-files.tgz /opt/malgary/stacks /opt/malgary/secrets /opt/malgary/platform/zitadel
```

Copy the dumps and the archive to the new control host over SSH. They contain secrets: delete
them from 178 and from any laptop once the move is verified.

## 3. Restore on the dedicated cell

1. Restore the databases with `restore.yml` (see [restore-cell.md](restore-cell.md)).
2. Put the tenant secrets in `/etc/malgary/secrets/tenants/` and the Zitadel master key and
   machine keys where the `zitadel` role expects them.
3. Re-apply every tenant with `cell: c01` and its new gateway pair
   (`deploy/tenants/<slug>.yaml`); the Ansible deployer recreates the stacks from the same data.
4. Run the new cell's checks (feature 001 quickstart V1–V3) with one test user.

## 4. Switch DNS

In Cloudflare, change the three A records (`*.vpn`, `id.vpn`, `api.vpn`) from 178.104.48.140 to
the new control host (or switch to the delegated zone, research R14). Clients reconnect by name;
nobody reinstalls or changes settings. Tell tenant admins the new egress IP for allowlists.

## 5. Remove Malgary from 178

```bash
ansible-playbook -i inventory/interim.yml interim-remove.yml               # dry run: review the list
ansible-playbook -i inventory/interim.yml interim-remove.yml -e confirm=yes
```

Then, in Coolify's PostgreSQL terminal as the superuser: `DROP ROLE malgary_provisioner;`.
Remove the operator Mac's root key (comment `malgary-178`) from `/root/.ssh/authorized_keys`,
leaving the original key and Coolify's key in place.
Check the other apps' health in Coolify, and record the move in `pilot-results.md`.
