-- +goose Up
-- Interim pilot on a shared host (feature 002, data-model.md). Additive: dedicated cells are
-- unaffected, and moving off the interim host needs no data migration.

-- An interim cell (code iNN) is one shared host: its gateway "pair" has no standby and its
-- egress IP is never PTA-registered (constitution, Interim Hosting Exception).
ALTER TABLE cell ADD COLUMN interim boolean NOT NULL DEFAULT false;
ALTER TABLE cell DROP CONSTRAINT cell_code_check;
ALTER TABLE cell ADD CONSTRAINT cell_code_check
    CHECK ((NOT interim AND code ~ '^c[0-9]{2}$') OR (interim AND code ~ '^i[0-9]{2}$'));
ALTER TABLE tenant ADD CONSTRAINT tenant_slug_not_interim_cell CHECK (slug !~ '^i[0-9]{2}$');

-- Required for dedicated cells; enforced in the inventory sync, which knows the cell.
ALTER TABLE gateway_pair ALTER COLUMN standby_host_id DROP NOT NULL;

-- FR-009: a tenant's access policy stays disabled until the latest result for its current
-- stack hash passed.
CREATE TABLE isolation_test_result (
    id         uuid PRIMARY KEY,
    tenant_id  uuid NOT NULL REFERENCES tenant(id),
    stack_hash text NOT NULL,
    ran_at     timestamptz NOT NULL DEFAULT now(),
    passed     boolean NOT NULL,
    egress_ip  inet,
    results    jsonb NOT NULL,
    ran_by     text NOT NULL
);
CREATE INDEX isolation_test_result_latest ON isolation_test_result (tenant_id, stack_hash, ran_at DESC);

-- FR-015: real company users are refused until one restore test passed.
CREATE TABLE backup_restore_test (
    id              uuid PRIMARY KEY,
    backup_key      text NOT NULL,
    backup_taken_at timestamptz NOT NULL,
    tested_at       timestamptz NOT NULL DEFAULT now(),
    passed          boolean NOT NULL,
    details         jsonb NOT NULL
);

-- Both are append-only records, like audit_record.
GRANT SELECT, INSERT ON isolation_test_result, backup_restore_test TO malgary_app;

-- +goose Down
DROP TABLE backup_restore_test;
DROP TABLE isolation_test_result;
DELETE FROM gateway_pair WHERE standby_host_id IS NULL;
ALTER TABLE gateway_pair ALTER COLUMN standby_host_id SET NOT NULL;
ALTER TABLE tenant DROP CONSTRAINT tenant_slug_not_interim_cell;
ALTER TABLE cell DROP CONSTRAINT cell_code_check;
ALTER TABLE cell ADD CONSTRAINT cell_code_check CHECK (code ~ '^c[0-9]{2}$');
ALTER TABLE cell DROP COLUMN interim;
