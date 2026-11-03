# isolation-probe

Disposable NetBird client used by `malgaryctl isolation-test <tenant>` (feature 002, FR-009).
It joins the tenant with a single-use ephemeral setup key, tries every private destination
that must be blocked and one public one that must work, and prints a JSON result that the
command records through `POST /v1/tenants/{id}/isolation-tests`.
