-- Interim pilot (feature 002, data-model.md).

-- name: CountSeatsHeldAll :one
SELECT count(*)::bigint FROM tenant_user WHERE seat_held;

-- name: AnyRestoreTestPassed :one
SELECT EXISTS (SELECT 1 FROM backup_restore_test WHERE passed);

-- name: CreateIsolationTestResult :one
INSERT INTO isolation_test_result (id, tenant_id, stack_hash, passed, egress_ip, results, ran_by)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: ListIsolationTestResults :many
SELECT * FROM isolation_test_result WHERE tenant_id = $1 ORDER BY ran_at DESC LIMIT 100;

-- name: IsolationPassedForHash :one
SELECT COALESCE((
    SELECT passed FROM isolation_test_result
    WHERE tenant_id = $1 AND stack_hash = $2
    ORDER BY ran_at DESC LIMIT 1), false)::boolean;

-- name: CreateRestoreTest :one
INSERT INTO backup_restore_test (id, backup_key, backup_taken_at, passed, details)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;
