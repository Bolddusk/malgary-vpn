-- name: UpsertTenantRoutingRule :one
INSERT INTO routing_rule (id, tenant_id, scope, mode, destinations)
VALUES ($1, $2, 'tenant', $3, $4)
ON CONFLICT (tenant_id, scope, COALESCE(scope_ref, '00000000-0000-0000-0000-000000000000'))
DO UPDATE SET mode = EXCLUDED.mode, destinations = EXCLUDED.destinations,
    version = routing_rule.version + 1, updated_at = now()
RETURNING *;

-- name: ListRoutingRules :many
SELECT * FROM routing_rule WHERE tenant_id = $1 ORDER BY scope, scope_ref;

-- name: ReplaceCallExclusionsDelete :exec
DELETE FROM call_exclusion WHERE tenant_id = $1;

-- name: AddCallExclusion :exec
INSERT INTO call_exclusion (tenant_id, cidr) VALUES ($1, $2) ON CONFLICT DO NOTHING;

-- name: ListCallExclusions :many
SELECT cidr::text FROM call_exclusion WHERE tenant_id = $1 ORDER BY cidr;

-- name: DeleteRoutingRules :exec
DELETE FROM routing_rule WHERE tenant_id = $1;

-- name: InsertRoutingRule :one
INSERT INTO routing_rule (id, tenant_id, scope, scope_ref, mode, destinations)
VALUES ($1, $2, $3, $4, $5, $6) RETURNING *;

-- name: ListRouteGroupMemberships :many
SELECT gm.tenant_user_id, g.id AS group_id, g.name FROM group_member gm JOIN "group" g ON g.id = gm.group_id
WHERE g.tenant_id = $1 AND g.name LIKE 'route:%';

-- name: RemoveGroupMember :exec
DELETE FROM group_member WHERE group_id = $1 AND tenant_user_id = $2;

-- name: DeleteEmptyRouteGroups :exec
DELETE FROM "group" g WHERE g.tenant_id = $1 AND g.name LIKE 'route:%'
  AND NOT EXISTS (SELECT 1 FROM group_member gm WHERE gm.group_id = g.id);

-- name: RenameGroup :one
UPDATE "group" SET name = $3 WHERE tenant_id = $1 AND id = $2 AND kind = 'team' RETURNING *;

-- name: DeleteTeamGroup :exec
DELETE FROM "group" WHERE tenant_id = $1 AND id = $2 AND kind = 'team';

-- name: GetGroup :one
SELECT * FROM "group" WHERE tenant_id = $1 AND id = $2;

-- name: ClearGroupMembers :exec
DELETE FROM group_member WHERE group_id = $1;

-- name: GetTenantUserByEmail :one
SELECT * FROM tenant_user WHERE tenant_id = $1 AND email = $2;

-- name: CountGroupMembers :one
SELECT count(*)::bigint FROM group_member WHERE group_id = $1;

-- name: DeleteGroupRoutingRules :exec
DELETE FROM routing_rule WHERE tenant_id = $1 AND scope = 'group' AND scope_ref = $2;

-- name: ListPostureRules :many
SELECT pr.*, g.name AS group_name FROM posture_rule pr LEFT JOIN "group" g ON g.id = pr.applies_to_group_id
WHERE pr.tenant_id = $1 ORDER BY g.name NULLS FIRST;

-- name: DeletePostureRules :exec
DELETE FROM posture_rule WHERE tenant_id = $1;

-- name: InsertPostureRule :exec
INSERT INTO posture_rule (id, tenant_id, min_os, min_client_version, applies_to_group_id)
VALUES ($1, $2, $3, $4, $5);
