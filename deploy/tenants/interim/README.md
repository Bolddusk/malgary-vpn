# Tenants on the interim host

Tenant definitions for the shared Hetzner server (feature 002). They differ from
`deploy/tenants/*.yaml` only in `cell: i01`, `gateways: [gw-i01]` and test-only identity
settings. Apply them with `malgaryctl tenant apply -f`, then run
`malgaryctl isolation-test <slug>` before any user connects.
