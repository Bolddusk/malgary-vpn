# gw-guard

Sidecar that shares a tenant routing peer's network namespace on the interim host
(feature 002, research I4). It installs the nftables table `inet malgary_guard`, which drops
forwarded traffic from VPN users to private ranges, Docker networks and the host, re-applies it
every 30 seconds, and exports per-peer WireGuard byte counters on `:9586`.

Needs `NET_ADMIN` only. Built on the host; not pushed to a registry.
