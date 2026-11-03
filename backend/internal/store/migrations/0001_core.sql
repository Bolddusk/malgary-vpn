-- +goose Up
-- Core placement, identity, users and audit tables (data-model.md sections 1, 2, 5).
-- Run as malgary_owner. The application connects as malgary_app.

CREATE EXTENSION IF NOT EXISTS citext;

CREATE TYPE cell_status AS ENUM ('active', 'draining', 'retired');
CREATE TYPE host_role AS ENUM ('control', 'gateway', 'relay');
CREATE TYPE host_status AS ENUM ('active', 'draining', 'down');
CREATE TYPE tenant_status AS ENUM ('provisioning', 'active', 'suspended', 'offboarding', 'deleted');
CREATE TYPE stack_status AS ENUM ('pending', 'applied', 'failed');
CREATE TYPE tenant_user_role AS ENUM ('member', 'tenant_admin');
CREATE TYPE tenant_user_status AS ENUM ('invited', 'active', 'disabled', 'removed');
CREATE TYPE group_kind AS ENUM ('team', 'system');
CREATE TYPE actor_type AS ENUM ('owner', 'operator', 'tenant_admin', 'system', 'deployer');

CREATE TABLE cell (
    id         uuid PRIMARY KEY,
    code       text NOT NULL UNIQUE CHECK (code ~ '^c[0-9]{2}$'),
    region     text NOT NULL,
    status     cell_status NOT NULL DEFAULT 'active',
    max_users  integer NOT NULL CHECK (max_users > 0),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE host (
    id         uuid PRIMARY KEY,
    cell_id    uuid NOT NULL REFERENCES cell(id),
    role       host_role NOT NULL,
    name       text NOT NULL UNIQUE,
    size_slug  text NOT NULL,
    status     host_status NOT NULL DEFAULT 'active',
    max_users  integer NOT NULL DEFAULT 2500 CHECK (max_users > 0),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE tenant (
    id         uuid PRIMARY KEY,
    -- DNS-safe, unique (data-model.md); reserved names rejected (research R14).
    slug       text NOT NULL UNIQUE
               CHECK (slug ~ '^[a-z][a-z0-9-]{1,30}[a-z0-9]$')
               CHECK (slug NOT IN ('id', 'api', 'www', 'portal', 'status', 'admin', 'ops'))
               CHECK (slug !~ '^c[0-9]{2}$'),
    legal_name text NOT NULL,
    status     tenant_status NOT NULL DEFAULT 'provisioning',
    cell_id    uuid NOT NULL REFERENCES cell(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE gateway_pair (
    id                  uuid PRIMARY KEY,
    cell_id             uuid NOT NULL REFERENCES cell(id),
    name                text NOT NULL UNIQUE,
    egress_ip           inet NOT NULL UNIQUE,
    pta_registered_at   timestamptz,
    pta_reference       text,
    primary_host_id     uuid NOT NULL REFERENCES host(id),
    standby_host_id     uuid NOT NULL REFERENCES host(id),
    dedicated_tenant_id uuid REFERENCES tenant(id),
    CHECK (primary_host_id <> standby_host_id)
);

CREATE TABLE tenant_stack (
    tenant_id         uuid PRIMARY KEY REFERENCES tenant(id),
    control_host_id   uuid NOT NULL REFERENCES host(id),
    netbird_version   text NOT NULL,
    management_url    text NOT NULL,
    http_port         integer NOT NULL CHECK (http_port BETWEEN 20000 AND 29999),
    db_name           text NOT NULL UNIQUE,
    service_token_ref text,
    desired_hash      text,
    applied_hash      text,
    status            stack_status NOT NULL DEFAULT 'pending',
    last_report       jsonb,
    updated_at        timestamptz NOT NULL DEFAULT now(),
    UNIQUE (control_host_id, http_port)
);

CREATE TABLE routing_peer (
    id              uuid PRIMARY KEY,
    tenant_id       uuid NOT NULL REFERENCES tenant(id),
    gateway_pair_id uuid NOT NULL REFERENCES gateway_pair(id),
    host_id         uuid NOT NULL REFERENCES host(id),
    netbird_peer_id text,
    wg_interface    text NOT NULL CHECK (length(wg_interface) <= 15),
    wg_port         integer NOT NULL CHECK (wg_port BETWEEN 51821 AND 52820),
    metric          integer NOT NULL CHECK (metric BETWEEN 1 AND 9999),
    UNIQUE (host_id, wg_port),
    UNIQUE (host_id, wg_interface),
    UNIQUE (tenant_id, host_id)
);

CREATE TABLE identity_link (
    tenant_id             uuid PRIMARY KEY REFERENCES tenant(id),
    zitadel_org_id        text,
    zitadel_app_client_id text,
    upstream_idps         jsonb NOT NULL DEFAULT '[]',
    allowed_domains       text[] NOT NULL DEFAULT '{}',
    allowed_emails        citext[] NOT NULL DEFAULT '{}',
    -- text[] rather than an enum array: pgx can't encode arrays of custom enums.
    sign_in_methods       text[] NOT NULL
                          CHECK (cardinality(sign_in_methods) > 0)
                          CHECK (sign_in_methods <@ ARRAY['sso', 'managed']::text[]),
    mfa_required          boolean NOT NULL DEFAULT true
);

CREATE TABLE tenant_user (
    id              uuid PRIMARY KEY,
    tenant_id       uuid NOT NULL REFERENCES tenant(id),
    email           citext NOT NULL,
    display_name    text NOT NULL DEFAULT '',
    role            tenant_user_role NOT NULL DEFAULT 'member',
    status          tenant_user_status NOT NULL DEFAULT 'invited',
    zitadel_user_id text,
    netbird_user_id text,
    seat_held       boolean NOT NULL DEFAULT true,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, email)
);

CREATE TABLE "group" (
    id               uuid PRIMARY KEY,
    tenant_id        uuid NOT NULL REFERENCES tenant(id),
    name             text NOT NULL,
    netbird_group_id text,
    kind             group_kind NOT NULL DEFAULT 'team',
    UNIQUE (tenant_id, name)
);

CREATE TABLE group_member (
    group_id       uuid NOT NULL REFERENCES "group"(id) ON DELETE CASCADE,
    tenant_user_id uuid NOT NULL REFERENCES tenant_user(id) ON DELETE CASCADE,
    PRIMARY KEY (group_id, tenant_user_id)
);

-- Append-only audit log (FR-026). Written in the same transaction as the change.
CREATE TABLE audit_record (
    id          uuid PRIMARY KEY,
    at          timestamptz NOT NULL DEFAULT now(),
    actor_type  actor_type NOT NULL,
    actor_id    text NOT NULL,
    tenant_id   uuid REFERENCES tenant(id),
    action      text NOT NULL,
    target_type text NOT NULL,
    target_id   text NOT NULL,
    before      jsonb,
    after       jsonb,
    request_id  text,
    source_ip   inet
);
CREATE INDEX audit_record_tenant_at ON audit_record (tenant_id, at DESC);

-- Idempotency keys for POST requests (contracts/backend-api.md).
CREATE TABLE idempotency_key (
    key           text NOT NULL,
    actor_id      text NOT NULL,
    method        text NOT NULL,
    path          text NOT NULL,
    request_hash  text NOT NULL,
    status_code   integer NOT NULL,
    response_body bytea NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (actor_id, key)
);

-- Application role: no UPDATE/DELETE/TRUNCATE on the audit log (T025).
GRANT USAGE ON SCHEMA public TO malgary_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO malgary_app;
REVOKE UPDATE, DELETE, TRUNCATE ON audit_record FROM malgary_app;

-- +goose Down
DROP TABLE idempotency_key, audit_record, group_member, "group", tenant_user, identity_link,
    routing_peer, tenant_stack, gateway_pair, tenant, host, cell;
DROP TYPE actor_type, group_kind, tenant_user_status, tenant_user_role,
    stack_status, tenant_status, host_status, host_role, cell_status;
