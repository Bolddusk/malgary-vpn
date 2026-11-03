# Runbook: Seat limit hit

**Alert**: `SeatLimitHit` (FR-016). Informational: a tenant tried to add users beyond paid seats.

1. `malgaryctl tenant get <id>` and the tenant's subscription quantity.
2. Contact the customer: raise the quantity (`malgaryctl subscription change <id> --quantity N --reason "..."`) or remove unused users. The change is prorated on the next invoice.
