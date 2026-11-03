# Runbook: Secrets

**Rule** (constitution Principle II): secrets are never stored in the repository in plain text,
in logs, or in StackSpecs.

## Where secrets live

| Secret | Store | Used by |
| --- | --- | --- |
| Infra secrets (DB passwords, Zitadel master key, Grafana, Spaces, email, DO reserved-IP token) | `infra/ansible/group_vars/all/secrets.age.yml`, encrypted with `age` to every operator's public key | Ansible via `bin/run` |
| Per-tenant secrets (NetBird PATs, setup keys, OIDC client secrets, DB passwords) | `/etc/malgary/secrets/` on the control host (root:malgary, 0770 dir, 0600 files) | backend, deployer |
| Backup private key | Offline with the owners (password manager plus a paper copy) | restores only |
| Operator credentials (DO token, Spaces key, age identity) | Each operator's machine / password manager | OpenTofu, `bin/run` |

## References

Code and StackSpecs use `secret://<path>`, resolved against `/etc/malgary/secrets/<path>`
at the moment of use (backend `internal/secrets`).

## Add or rotate an infra secret

1. `age -d -i $AGE_IDENTITY group_vars/all/secrets.age.yml > /dev/shm/s.yml`
2. Edit `/dev/shm/s.yml`.
3. `age -R operators.pub -o group_vars/all/secrets.age.yml /dev/shm/s.yml && shred -u /dev/shm/s.yml`
4. `bin/run site.yml --limit <hosts>`

## Add an operator

Append their age public key to `infra/ansible/operators.pub` and re-encrypt (step 3). Remove a
leaver the same way, then rotate every secret they could read.
