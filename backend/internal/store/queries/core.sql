-- name: CreateCell :one
INSERT INTO cell (id, code, region, max_users) VALUES ($1, $2, $3, $4) RETURNING *;

-- name: GetCellByCode :one
SELECT * FROM cell WHERE code = $1;

-- name: ListActiveCells :many
SELECT * FROM cell WHERE status = 'active' ORDER BY code;

-- name: CellUserCount :one
SELECT count(*)::bigint FROM tenant_user tu JOIN tenant t ON t.id = tu.tenant_id
WHERE t.cell_id = $1 AND tu.seat_held;

-- name: CreateHost :one
INSERT INTO host (id, cell_id, role, name, size_slug, max_users)
VALUES ($1, $2, $3, $4, $5, $6) RETURNING *;

-- name: GetHost :one
SELECT * FROM host WHERE id = $1;

-- name: ListHostsByCellRole :many
SELECT * FROM host WHERE cell_id = $1 AND role = $2 AND status = 'active' ORDER BY name;

-- name: HostUserCount :one
SELECT count(*)::bigint FROM tenant_user tu JOIN tenant_stack ts ON ts.tenant_id = tu.tenant_id
WHERE ts.control_host_id = $1 AND tu.seat_held;

-- name: CreateGatewayPair :one
INSERT INTO gateway_pair (id, cell_id, name, egress_ip, primary_host_id, standby_host_id, dedicated_tenant_id)
VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING *;

-- name: GetGatewayPairByName :one
SELECT * FROM gateway_pair WHERE name = $1;

-- name: MarkGatewayPairPTARegistered :one
UPDATE gateway_pair SET pta_registered_at = $2, pta_reference = $3 WHERE name = $1 RETURNING *;

-- name: CreateTenant :one
INSERT INTO tenant (id, slug, legal_name, cell_id) VALUES ($1, $2, $3, $4) RETURNING *;

-- name: GetTenant :one
SELECT * FROM tenant WHERE id = $1;

-- name: GetTenantBySlug :one
SELECT * FROM tenant WHERE slug = $1;

-- name: ListTenants :many
SELECT * FROM tenant WHERE status <> 'deleted' ORDER BY slug;

-- name: SetTenantStatus :one
UPDATE tenant SET status = $2, updated_at = now() WHERE id = $1 RETURNING *;

-- name: UpsertTenantStack :one
INSERT INTO tenant_stack (tenant_id, control_host_id, netbird_version, management_url, http_port, db_name, service_token_ref, desired_hash)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (tenant_id) DO UPDATE SET
    control_host_id = EXCLUDED.control_host_id,
    netbird_version = EXCLUDED.netbird_version,
    management_url = EXCLUDED.management_url,
    http_port = EXCLUDED.http_port,
    db_name = EXCLUDED.db_name,
    service_token_ref = COALESCE(EXCLUDED.service_token_ref, tenant_stack.service_token_ref),
    desired_hash = EXCLUDED.desired_hash,
    status = CASE WHEN tenant_stack.desired_hash = EXCLUDED.desired_hash THEN tenant_stack.status ELSE 'pending' END,
    updated_at = now()
RETURNING *;

-- name: GetTenantStack :one
SELECT * FROM tenant_stack WHERE tenant_id = $1;

-- name: UsedHTTPPorts :many
SELECT http_port FROM tenant_stack WHERE control_host_id = $1;

-- name: ReportTenantStack :one
UPDATE tenant_stack SET applied_hash = $2, status = $3, last_report = $4, updated_at = now()
WHERE tenant_id = $1 RETURNING *;

-- name: SetTenantStackToken :exec
UPDATE tenant_stack SET service_token_ref = $2, updated_at = now() WHERE tenant_id = $1;

-- name: CreateRoutingPeer :one
INSERT INTO routing_peer (id, tenant_id, gateway_pair_id, host_id, wg_interface, wg_port, metric)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (tenant_id, host_id) DO UPDATE SET metric = EXCLUDED.metric
RETURNING *;

-- name: ListRoutingPeersByTenant :many
SELECT rp.*, h.name AS host_name FROM routing_peer rp JOIN host h ON h.id = rp.host_id
WHERE rp.tenant_id = $1 ORDER BY rp.metric, h.name;

-- name: UsedWGPortsOnHost :many
SELECT wg_port FROM routing_peer WHERE host_id = $1;

-- name: UpsertIdentityLink :one
INSERT INTO identity_link (tenant_id, zitadel_org_id, zitadel_app_client_id, upstream_idps, allowed_domains, allowed_emails, sign_in_methods, mfa_required)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (tenant_id) DO UPDATE SET
    zitadel_org_id = COALESCE(EXCLUDED.zitadel_org_id, identity_link.zitadel_org_id),
    zitadel_app_client_id = COALESCE(EXCLUDED.zitadel_app_client_id, identity_link.zitadel_app_client_id),
    upstream_idps = EXCLUDED.upstream_idps,
    allowed_domains = EXCLUDED.allowed_domains,
    allowed_emails = EXCLUDED.allowed_emails,
    sign_in_methods = EXCLUDED.sign_in_methods,
    mfa_required = EXCLUDED.mfa_required
RETURNING *;

-- name: GetIdentityLink :one
SELECT * FROM identity_link WHERE tenant_id = $1;

-- name: CreateTenantUser :one
INSERT INTO tenant_user (id, tenant_id, email, display_name, role)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (tenant_id, email) DO NOTHING
RETURNING *;

-- name: GetTenantUser :one
SELECT * FROM tenant_user WHERE tenant_id = $1 AND id = $2;

-- name: ListTenantUsers :many
SELECT * FROM tenant_user WHERE tenant_id = $1 AND status <> 'removed' ORDER BY email;

-- name: CountSeatsHeld :one
SELECT count(*)::bigint FROM tenant_user WHERE tenant_id = $1 AND seat_held;

-- name: UpdateTenantUser :one
UPDATE tenant_user SET status = $3, role = $4, seat_held = $5, zitadel_user_id = COALESCE($6, zitadel_user_id),
    netbird_user_id = COALESCE($7, netbird_user_id), updated_at = now()
WHERE tenant_id = $1 AND id = $2 RETURNING *;

-- name: CreateGroup :one
INSERT INTO "group" (id, tenant_id, name, kind) VALUES ($1, $2, $3, $4)
ON CONFLICT (tenant_id, name) DO UPDATE SET kind = EXCLUDED.kind
RETURNING *;

-- name: ListGroups :many
SELECT * FROM "group" WHERE tenant_id = $1 ORDER BY name;

-- name: SetGroupNetbirdID :exec
UPDATE "group" SET netbird_group_id = $2 WHERE id = $1;

-- name: AddGroupMember :exec
INSERT INTO group_member (group_id, tenant_user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING;

-- name: InsertAudit :exec
INSERT INTO audit_record (id, actor_type, actor_id, tenant_id, action, target_type, target_id, before, after, request_id, source_ip)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11);

-- name: ListAudit :many
SELECT * FROM audit_record
WHERE tenant_id = $1 AND (sqlc.narg('before_at')::timestamptz IS NULL OR at < sqlc.narg('before_at'))
ORDER BY at DESC LIMIT $2;

-- name: GetIdempotency :one
SELECT * FROM idempotency_key WHERE actor_id = $1 AND key = $2;

-- name: PutIdempotency :exec
INSERT INTO idempotency_key (key, actor_id, method, path, request_hash, status_code, response_body)
VALUES ($1, $2, $3, $4, $5, $6, $7) ON CONFLICT DO NOTHING;

-- name: UpsertCell :one
INSERT INTO cell (id, code, region, max_users) VALUES ($1, $2, $3, $4)
ON CONFLICT (code) DO UPDATE SET region = EXCLUDED.region, max_users = EXCLUDED.max_users
RETURNING *;

-- name: UpsertHost :one
INSERT INTO host (id, cell_id, role, name, size_slug, max_users) VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (name) DO UPDATE SET size_slug = EXCLUDED.size_slug, max_users = EXCLUDED.max_users, role = EXCLUDED.role
RETURNING *;

-- name: GetHostByName :one
SELECT * FROM host WHERE name = $1;

-- name: UpsertGatewayPair :one
INSERT INTO gateway_pair (id, cell_id, name, egress_ip, primary_host_id, standby_host_id)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (name) DO UPDATE SET egress_ip = EXCLUDED.egress_ip,
    primary_host_id = EXCLUDED.primary_host_id, standby_host_id = EXCLUDED.standby_host_id
RETURNING *;

-- name: ListCells :many
SELECT * FROM cell ORDER BY code;

-- name: ListTenantStacksPending :many
SELECT ts.*, t.slug FROM tenant_stack ts JOIN tenant t ON t.id = ts.tenant_id
WHERE ts.status <> 'applied' OR ts.applied_hash IS DISTINCT FROM ts.desired_hash;

-- name: GetCell :one
SELECT * FROM cell WHERE id = $1;

-- name: GetTenantUserByZitadelID :one
SELECT * FROM tenant_user WHERE zitadel_user_id = $1;

-- name: ListUserGroupNames :many
SELECT g.name FROM "group" g JOIN group_member gm ON gm.group_id = g.id
WHERE gm.tenant_user_id = $1 ORDER BY g.name;

-- name: ListTenantMemberships :many
SELECT gm.tenant_user_id, g.name FROM group_member gm JOIN "group" g ON g.id = gm.group_id
WHERE g.tenant_id = $1;

-- name: GetGroupByName :one
SELECT * FROM "group" WHERE tenant_id = $1 AND name = $2;

-- name: SetTenantUserZitadelID :exec
UPDATE tenant_user SET zitadel_user_id = $2, updated_at = now() WHERE id = $1;
