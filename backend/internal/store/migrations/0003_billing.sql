-- +goose Up
-- Commercial model (data-model.md section 4, constitution Principle XI).
CREATE TYPE plan_visibility AS ENUM ('public', 'enterprise');
CREATE TYPE plan_status AS ENUM ('active', 'retired');
CREATE TYPE plan_version_status AS ENUM ('draft', 'published', 'superseded');
CREATE TYPE billing_period AS ENUM ('monthly', 'annual');
CREATE TYPE subscription_status AS ENUM ('trialing', 'active', 'past_due', 'suspended', 'cancelled');
CREATE TYPE invoice_status AS ENUM ('draft', 'issued', 'paid', 'void');

CREATE TABLE plan (
    id                   uuid PRIMARY KEY,
    code                 text NOT NULL UNIQUE CHECK (code ~ '^[a-z][a-z0-9-]{1,40}$'),
    visibility           plan_visibility NOT NULL,
    -- Slug, not an FK, so an enterprise plan can be prepared before its tenant is created.
    enterprise_tenant_slug text,
    name                 text NOT NULL,
    status               plan_status NOT NULL DEFAULT 'active',
    created_at           timestamptz NOT NULL DEFAULT now(),
    -- enterprise tenant "required when visibility = enterprise" (data-model.md)
    CHECK ((visibility = 'enterprise') = (enterprise_tenant_slug IS NOT NULL))
);

CREATE TABLE plan_version (
    id                   uuid PRIMARY KEY,
    plan_id              uuid NOT NULL REFERENCES plan(id),
    version              integer NOT NULL CHECK (version >= 1),
    status               plan_version_status NOT NULL DEFAULT 'draft',
    price_per_seat_minor bigint NOT NULL CHECK (price_per_seat_minor >= 0),
    currency             char(3) NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    billing_period       billing_period NOT NULL,
    min_seats            integer NOT NULL CHECK (min_seats >= 1),
    max_seats            integer CHECK (max_seats IS NULL OR max_seats >= min_seats),
    entitlements         jsonb NOT NULL DEFAULT '{}',
    terms                text NOT NULL DEFAULT '',
    published_at         timestamptz,
    published_by         text,
    created_at           timestamptz NOT NULL DEFAULT now(),
    UNIQUE (plan_id, version)
);
CREATE UNIQUE INDEX plan_version_one_published ON plan_version (plan_id) WHERE status = 'published';

-- "A database trigger rejects any update to a published version except status → superseded"
-- (data-model.md, FR-016b).
-- +goose StatementBegin
CREATE FUNCTION plan_version_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.status = 'draft' THEN
        RETURN NEW;
    END IF;
    IF OLD.status = 'published' AND NEW.status = 'superseded'
       AND (to_jsonb(NEW) - 'status') = (to_jsonb(OLD) - 'status') THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'plan_version_immutable: version % of plan % is %', OLD.version, OLD.plan_id, OLD.status
        USING ERRCODE = 'check_violation';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER plan_version_immutable BEFORE UPDATE OR DELETE ON plan_version
    FOR EACH ROW WHEN (OLD.status <> 'draft') EXECUTE FUNCTION plan_version_immutable();

CREATE TABLE subscription (
    id                   uuid PRIMARY KEY,
    tenant_id            uuid NOT NULL REFERENCES tenant(id),
    plan_version_id      uuid NOT NULL REFERENCES plan_version(id),
    quantity             integer NOT NULL CHECK (quantity >= 1),
    status               subscription_status NOT NULL DEFAULT 'active',
    current_period_start date NOT NULL,
    current_period_end   date NOT NULL,
    payment_provider     text NOT NULL DEFAULT 'manual',
    provider_ref         text,
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now(),
    CHECK (current_period_end > current_period_start)
);
-- "one non-terminal subscription per tenant"
CREATE UNIQUE INDEX subscription_one_open ON subscription (tenant_id) WHERE status <> 'cancelled';

CREATE TABLE subscription_event (
    id              uuid PRIMARY KEY,
    subscription_id uuid NOT NULL REFERENCES subscription(id),
    type            text NOT NULL CHECK (type IN ('created', 'quantity_changed', 'plan_migrated', 'status_changed', 'period_renewed')),
    from_value      jsonb,
    to_value        jsonb,
    actor           text NOT NULL,
    reason          text,
    at              timestamptz NOT NULL DEFAULT now()
);

CREATE SEQUENCE invoice_number_seq;

CREATE TABLE invoice (
    id              uuid PRIMARY KEY,
    tenant_id       uuid NOT NULL REFERENCES tenant(id),
    subscription_id uuid NOT NULL REFERENCES subscription(id),
    number          text UNIQUE,                       -- MV-YYYY-NNNNNN, set when issued
    period_start    date NOT NULL,
    period_end      date NOT NULL,
    lines           jsonb NOT NULL,
    subtotal_minor  bigint NOT NULL CHECK (subtotal_minor >= 0),
    tax_minor       bigint NOT NULL DEFAULT 0 CHECK (tax_minor >= 0),
    total_minor     bigint NOT NULL CHECK (total_minor >= 0),
    currency        char(3) NOT NULL,
    status          invoice_status NOT NULL DEFAULT 'draft',
    issued_at       timestamptz,
    due_at          timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (subscription_id, period_start),
    CHECK ((status = 'draft') = (number IS NULL))
);

-- Issued invoices are immutable; only issued → paid/void transitions are allowed.
-- +goose StatementBegin
CREATE FUNCTION invoice_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'issued invoices cannot be deleted' USING ERRCODE = 'check_violation';
    END IF;
    IF (to_jsonb(NEW) - 'status') = (to_jsonb(OLD) - 'status')
       AND ((OLD.status = 'issued' AND NEW.status IN ('paid', 'void'))) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'invoice % is % and immutable; issue a credit note', OLD.number, OLD.status
        USING ERRCODE = 'check_violation';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER invoice_immutable BEFORE UPDATE OR DELETE ON invoice
    FOR EACH ROW WHEN (OLD.status <> 'draft') EXECUTE FUNCTION invoice_immutable();

CREATE TABLE credit_note (
    id           uuid PRIMARY KEY,
    invoice_id   uuid NOT NULL REFERENCES invoice(id),
    amount_minor bigint NOT NULL CHECK (amount_minor > 0),
    currency     char(3) NOT NULL,
    reason       text NOT NULL,
    created_by   text NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE payment (
    id                   uuid PRIMARY KEY,
    invoice_id           uuid NOT NULL REFERENCES invoice(id),
    provider             text NOT NULL,
    provider_payment_ref text NOT NULL,
    amount_minor         bigint NOT NULL CHECK (amount_minor > 0),
    currency             char(3) NOT NULL,
    received_at          timestamptz NOT NULL,
    method_note          text NOT NULL DEFAULT '',
    proof_object_key     text,
    recorded_by          text NOT NULL,
    created_at           timestamptz NOT NULL DEFAULT now(),
    UNIQUE (provider, provider_payment_ref)
);

CREATE TABLE payment_provider_event (
    provider          text NOT NULL,
    provider_event_id text NOT NULL,
    payload           jsonb NOT NULL,
    processed_at      timestamptz,
    received_at       timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (provider, provider_event_id)
);

GRANT SELECT, INSERT, UPDATE, DELETE ON plan, plan_version, subscription, invoice TO malgary_app;
GRANT SELECT, INSERT ON subscription_event, credit_note, payment, payment_provider_event TO malgary_app;
GRANT UPDATE (processed_at) ON payment_provider_event TO malgary_app;
GRANT USAGE ON SEQUENCE invoice_number_seq TO malgary_app;

-- +goose Down
DROP TABLE payment_provider_event, payment, credit_note, invoice, subscription_event, subscription, plan_version, plan;
DROP SEQUENCE invoice_number_seq;
DROP FUNCTION invoice_immutable, plan_version_immutable;
DROP TYPE invoice_status, subscription_status, billing_period, plan_version_status, plan_status, plan_visibility;
