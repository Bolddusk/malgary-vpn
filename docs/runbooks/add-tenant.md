# Runbook: Add a tenant

1. Owner prepares the plan: copy `deploy/plans/ent-onedge.yaml`, set `code`, `enterpriseTenant`, price and entitlements; `malgaryctl plan apply -f deploy/plans/<code>.yaml`.
2. Tenant definition: copy `deploy/tenants/onedge.yaml`, set slug, legal name, sign-in methods chosen by the customer (`sso`, `managed` or both), allowed domains, quantity, gateway pair.
3. `malgaryctl tenant apply -f deploy/tenants/<slug>.yaml`, then `malgaryctl tenant get <id>` until `active / applied` (about 3–5 minutes).
4. Import users: `malgaryctl users import <id> users.csv`. SSO users sign in at once; managed-account users get an invitation email.
5. Send the tenant admin `https://<slug>.vpn.malgary.com/setup`.

**One-time Zitadel setup (owners)**: create instance-level identity providers for Google (Malgary OAuth client) and Microsoft (multi-tenant app) with auto-creation off and auto-linking by email; put their IDs in `MALGARY_ZITADEL_GOOGLE_IDP_ID` / `MALGARY_ZITADEL_ENTRA_IDP_ID`. Create an Actions v2 target pointing at `https://api.vpn.malgary.com/hooks/zitadel/claims` for functions `preuserinfo` and `preaccesstoken`; put its signing key in the secrets file as `zitadel.claims_signing_key`.
