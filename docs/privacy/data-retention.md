# Data retention (basis for the customer privacy policy)

Constitution Principle VII, FR-027, T113.

| Data | Where | Kept | Why |
| --- | --- | --- | --- |
| Content of user traffic | Never stored | — | Not logged anywhere: no packet capture, no traffic events, Traefik logs without query strings or headers |
| Connection status and last seen per device | `usage_sample` | 90 days, then daily totals | Usage reports for the customer admin |
| Bytes through the gateways per device | `usage_sample` | 90 days, then daily totals | Usage reports; capacity planning |
| Daily usage totals | `usage_daily` | Life of the subscription + 12 months | Billing disputes, trends |
| Audit log (who changed what) | `audit_record` | Life of the subscription + 24 months | Security and accountability |
| Users, groups, rules | Backend DB, tenant NetBird DB, Zitadel | Life of the subscription; deleted 30 days after offboarding | Running the service |
| Invoices and payments | Backend DB, proofs in Spaces | 10 years | Tax and accounting law |
| Backups | Spaces (Amsterdam), encrypted | 30 days | Disaster recovery |
| System logs (journald, container logs) | Hosts and Grafana Cloud | 30 days | Operations |

Customers' data is processed in DigitalOcean New York (service) and Amsterdam (backups), and
Grafana Cloud (operational metrics, no user content). Have this reviewed by counsel before the
public launch.
