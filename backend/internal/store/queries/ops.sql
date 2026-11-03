-- name: InsertUsageSample :exec
INSERT INTO usage_sample (tenant_id, tenant_user_id, netbird_peer_id, sampled_at, connected, last_seen, rx_bytes, tx_bytes)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8) ON CONFLICT DO NOTHING;

-- name: LatestUsagePerUser :many
SELECT DISTINCT ON (tenant_user_id) tenant_user_id, connected, last_seen, rx_bytes, tx_bytes
FROM usage_sample WHERE tenant_id = $1 AND tenant_user_id IS NOT NULL AND sampled_at >= $2
ORDER BY tenant_user_id, sampled_at DESC;

-- name: UsageTotalsPerUser :many
SELECT tenant_user_id, max(last_seen)::timestamptz AS last_seen,
       (max(rx_bytes) - min(rx_bytes))::bigint AS rx, (max(tx_bytes) - min(tx_bytes))::bigint AS tx
FROM usage_sample WHERE tenant_id = $1 AND tenant_user_id IS NOT NULL AND sampled_at BETWEEN $2 AND $3
GROUP BY tenant_user_id;

-- name: RollupUsage :exec
INSERT INTO usage_daily (tenant_id, tenant_user_id, day, connected_samples, rx_bytes, tx_bytes, last_seen)
SELECT tenant_id, tenant_user_id, sampled_at::date, count(*) FILTER (WHERE connected),
       COALESCE(max(rx_bytes) - min(rx_bytes), 0), COALESCE(max(tx_bytes) - min(tx_bytes), 0), max(last_seen)
FROM usage_sample WHERE sampled_at < $1
GROUP BY tenant_id, tenant_user_id, sampled_at::date
ON CONFLICT (tenant_id, tenant_user_id, day) DO NOTHING;

-- name: PruneUsage :exec
DELETE FROM usage_sample WHERE sampled_at < $1;

-- name: ListActiveTenantsWithStacks :many
SELECT t.*, ts.management_url FROM tenant t JOIN tenant_stack ts ON ts.tenant_id = t.id
WHERE t.status IN ('active', 'suspended') ORDER BY t.slug;

-- name: StartReconcileRun :one
INSERT INTO reconcile_run (id, tenant_id, started_at, status) VALUES ($1, $2, now(), 'running') RETURNING *;

-- name: FinishReconcileRun :exec
UPDATE reconcile_run SET finished_at = now(), drift = $2, actions = $3, status = $4 WHERE id = $1;

-- name: ListAuditPage :many
SELECT * FROM audit_record
WHERE tenant_id = $1 AND (sqlc.narg('before_at')::timestamptz IS NULL OR at < sqlc.narg('before_at'))
ORDER BY at DESC LIMIT $2;

-- name: ListAllGatewayHosts :many
SELECT name FROM host WHERE role = 'gateway' AND status = 'active' ORDER BY name;
