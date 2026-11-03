# Dashboards (T094)

Import into Grafana Cloud. Panels and the queries behind them:

| Dashboard | Panel | Query |
| --- | --- | --- |
| Cell overview | Hosts up | `up` by host |
| | CPU per host | `1 - avg by (host)(rate(node_cpu_seconds_total{mode="idle"}[5m]))` |
| | Tenant stacks running | `count(container_last_seen{container_label_com_docker_compose_project=~"nb-.*"})` |
| | Last backup age | `time() - malgary_backup_last_success_timestamp_seconds` |
| Gateway pair | Active member | `gateway_is_master` by host |
| | Failovers (30d) | `increase(gateway_failover_total[30d])` |
| | Throughput | `rate(node_network_transmit_bytes_total{role="gateway",device="eth0"}[5m])` |
| | Availability (30d) | `malgary:vpn_availability:ratio_30d` |
| Tenant | Bytes per tenant | `sum by (tenant)(rate(malgary_wg_peer_rx_bytes[5m]) + rate(malgary_wg_peer_tx_bytes[5m]))` |
| | Seat-limit hits | `increase(malgary_tenant_seat_limit_hit_total[1d])` |
| | Drifting tenants | `malgary_reconcile_drift_tenants` |

Per-user usage (connected now, last seen, never installed) is served by the backend
(`GET /v1/tenants/{id}/usage`) and shown in the portal (002), not in Grafana.
