# Runbook: Operator access

**Rule** (constitution Principle VII, FR-029): no SSH from the internet. Operators reach
servers over the VPC through the `malgary-ops` tenant, with MFA.

## Normal access

1. Connect the NetBird app to `https://malgary-ops.vpn.malgary.com` and sign in with your
   `@malgary.com` Google account (MFA required).
2. SSH to hosts by private name, e.g. `ssh ctl-1.c01.vpn.malgary.com`.
3. Tenant NetBird dashboards (`https://<tenant>.vpn.malgary.com/`) only answer when your traffic
   comes from the VPC or the ops tenant's egress IP (Traefik `ipAllowList`).

## First boot (before malgary-ops exists) and break-glass

Use the DigitalOcean Droplet console (web) for the host. It needs the operator's DO account
with 2FA. Record every console session in the incident log.

1. Run `bin/run site.yml` from a Droplet console session, or temporarily add your IP to the
   cell firewall's SSH rule with `tofu apply -var 'admin_cidrs=["x.x.x.x/32"]'`, then
   remove it the same day.
2. Create `malgary-ops` with `malgaryctl tenant apply -f deploy/tenants/malgary-ops.yaml`.
3. Remove any temporary firewall rule.
