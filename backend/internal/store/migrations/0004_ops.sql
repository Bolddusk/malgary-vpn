-- +goose Up
-- Usage samples and reconcile runs (data-model.md section 5).
CREATE TABLE usage_sample (
    tenant_id       uuid NOT NULL REFERENCES tenant(id),
    tenant_user_id  uuid REFERENCES tenant_user(id) ON DELETE SET NULL,
    netbird_peer_id text NOT NULL,
    sampled_at      timestamptz NOT NULL,
    connected       boolean NOT NULL,
    last_seen       timestamptz,
    rx_bytes        bigint,
    tx_bytes        bigint,
    PRIMARY KEY (tenant_id, netbird_peer_id, sampled_at)
);
CREATE INDEX usage_sample_user ON usage_sample (tenant_id, tenant_user_id, sampled_at DESC);

-- "Kept 90 days, then rolled up daily" (data-model.md).
CREATE TABLE usage_daily (
    tenant_id      uuid NOT NULL REFERENCES tenant(id),
    tenant_user_id uuid REFERENCES tenant_user(id) ON DELETE SET NULL,
    day            date NOT NULL,
    connected_samples integer NOT NULL,
    rx_bytes       bigint NOT NULL DEFAULT 0,
    tx_bytes       bigint NOT NULL DEFAULT 0,
    last_seen      timestamptz,
    UNIQUE (tenant_id, tenant_user_id, day)
);

CREATE TABLE reconcile_run (
    id          uuid PRIMARY KEY,
    tenant_id   uuid NOT NULL REFERENCES tenant(id),
    started_at  timestamptz NOT NULL,
    finished_at timestamptz,
    drift       jsonb NOT NULL DEFAULT '[]',
    actions     jsonb NOT NULL DEFAULT '[]',
    status      text NOT NULL CHECK (status IN ('running', 'clean', 'repaired', 'drift', 'failed'))
);
CREATE INDEX reconcile_run_tenant ON reconcile_run (tenant_id, started_at DESC);

GRANT SELECT, INSERT, UPDATE, DELETE ON usage_sample, usage_daily, reconcile_run TO malgary_app;

-- +goose Down
DROP TABLE reconcile_run, usage_daily, usage_sample;
