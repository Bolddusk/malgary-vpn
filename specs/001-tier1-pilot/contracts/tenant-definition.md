# Contract: Tenant Definition

The input to `POST /v1/tenants` and to `malgaryctl tenant apply -f <file>`. The same document is
applied repeatedly. Applying it again with no changes does nothing (FR-015).

```yaml
apiVersion: malgary.io/v1
kind: Tenant
metadata:
  slug: onedge                 # DNS-safe, 3–32 chars, unique. Gives onedge.vpn.<domain>
  legalName: "On Edge (Pvt) Ltd"
spec:
  cell: c01                    # optional. Placement picks one if omitted.
  identity:
    signInMethods: [sso]       # sso | managed | both listed. Chosen by the customer (FR-002).
    upstreamIdps:
      - type: google           # google | entra | oidc
        hostedDomain: onedge.co
    allowedDomains: [onedge.co]
    allowedEmails: []          # for tenants using individual Gmail or other addresses
    mfaRequired: true
  subscription:
    plan: ent-onedge           # Plan code
    planVersion: 1             # pinned. Never "latest".
    quantity: 10
    paymentProvider: manual
  gateways:
    - pair: gw-nyc-1           # shared pair, or a dedicated pair if entitled
  routing:
    default: { mode: full_tunnel }
    callExclusions: [ "142.250.82.0/24" ]   # IPv4 CIDRs only, always outside the VPN (FR-006, R12)
  posture:
    minClientVersion: "0.82.0"
    minOs: { windows: "10.0.19045", macos: "14.0", ios: "17.0", android: "13" }
  admins:
    - email: it-admin@onedge.co
```

## Validation

- `subscription.plan` must exist, and `planVersion` must be `published`. Enterprise plans must
  belong to this tenant.
- `quantity` must be within the plan version's min and max seats.
- `gateways[].pair` must be in the tenant's cell. Dedicated pairs require the `dedicated_ip`
  entitlement.
- `allowedDomains` or `allowedEmails` must be non-empty.
- `signInMethods` must be non-empty. `sso` requires at least one `upstreamIdps` entry.
  `managed` always enforces MFA, whatever `mfaRequired` says.
- Unknown fields are rejected.
