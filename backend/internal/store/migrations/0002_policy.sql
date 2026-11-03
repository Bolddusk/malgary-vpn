-- +goose Up
-- Routing and posture rules (data-model.md section 3). Brought forward from US5 because the
-- tenant default route (T046) needs a persisted rule.
CREATE TYPE rule_scope AS ENUM ('tenant', 'group', 'user');
CREATE TYPE routing_mode AS ENUM ('full_tunnel', 'destinations');

CREATE TABLE routing_rule (
    id           uuid PRIMARY KEY,
    tenant_id    uuid NOT NULL REFERENCES tenant(id),
    scope        rule_scope NOT NULL,
    scope_ref    uuid,
    mode         routing_mode NOT NULL,
    -- "required and non-empty when mode = destinations" (data-model.md)
    destinations text[] NOT NULL DEFAULT '{}',
    version      integer NOT NULL DEFAULT 1,
    updated_at   timestamptz NOT NULL DEFAULT now(),
    CHECK (mode <> 'destinations' OR cardinality(destinations) > 0),
    CHECK ((scope = 'tenant') = (scope_ref IS NULL))
);
CREATE UNIQUE INDEX routing_rule_scope ON routing_rule (tenant_id, scope, COALESCE(scope_ref, '00000000-0000-0000-0000-000000000000'));

-- Call traffic never uses the VPN (FR-006): tenant-wide CIDR list excluded from every route.
CREATE TABLE call_exclusion (
    tenant_id uuid NOT NULL REFERENCES tenant(id),
    cidr      cidr NOT NULL,
    PRIMARY KEY (tenant_id, cidr)
);

CREATE TABLE posture_rule (
    id                       uuid PRIMARY KEY,
    tenant_id                uuid NOT NULL REFERENCES tenant(id),
    min_os                   jsonb NOT NULL DEFAULT '{}',
    min_client_version       text,
    applies_to_group_id      uuid REFERENCES "group"(id) ON DELETE CASCADE,
    netbird_posture_check_id text
);

GRANT SELECT, INSERT, UPDATE, DELETE ON routing_rule, call_exclusion, posture_rule TO malgary_app;

-- +goose Down
DROP TABLE posture_rule, call_exclusion, routing_rule;
DROP TYPE routing_mode, rule_scope;
