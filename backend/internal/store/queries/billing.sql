-- name: CreatePlan :one
INSERT INTO plan (id, code, visibility, enterprise_tenant_slug, name) VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetPlanByCode :one
SELECT * FROM plan WHERE code = $1;

-- name: GetPlan :one
SELECT * FROM plan WHERE id = $1;

-- name: ListPlans :many
SELECT * FROM plan ORDER BY code;

-- name: NextPlanVersionNumber :one
SELECT COALESCE(max(version), 0)::int + 1 FROM plan_version WHERE plan_id = $1;

-- name: CreatePlanVersion :one
INSERT INTO plan_version (id, plan_id, version, price_per_seat_minor, currency, billing_period, min_seats, max_seats, entitlements, terms)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

-- name: ListPlanVersions :many
SELECT * FROM plan_version WHERE plan_id = $1 ORDER BY version;

-- name: GetPlanVersion :one
SELECT * FROM plan_version WHERE plan_id = $1 AND version = $2;

-- name: GetPlanVersionByID :one
SELECT * FROM plan_version WHERE id = $1;

-- name: SupersedePublished :exec
UPDATE plan_version SET status = 'superseded' WHERE plan_id = $1 AND status = 'published';

-- name: PublishPlanVersion :one
UPDATE plan_version SET status = 'published', published_at = now(), published_by = $3
WHERE plan_id = $1 AND version = $2 AND status = 'draft'
RETURNING *;

-- name: UpdatePlanVersionTerms :exec
UPDATE plan_version SET terms = $2 WHERE id = $1;

-- name: CreateSubscription :one
INSERT INTO subscription (id, tenant_id, plan_version_id, quantity, current_period_start, current_period_end, payment_provider)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetOpenSubscription :one
SELECT * FROM subscription WHERE tenant_id = $1 AND status <> 'cancelled';

-- name: UpdateSubscription :one
UPDATE subscription SET plan_version_id = $2, quantity = $3, status = $4, updated_at = now()
WHERE id = $1 RETURNING *;

-- name: InsertSubscriptionEvent :exec
INSERT INTO subscription_event (id, subscription_id, type, from_value, to_value, actor, reason)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: ListSubscriptionEvents :many
SELECT se.* FROM subscription_event se JOIN subscription s ON s.id = se.subscription_id
WHERE s.tenant_id = $1 ORDER BY se.at;

-- name: CreateInvoice :one
INSERT INTO invoice (id, tenant_id, subscription_id, period_start, period_end, lines, subtotal_minor, tax_minor, total_minor, currency)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

-- name: GetInvoice :one
SELECT * FROM invoice WHERE id = $1;

-- name: ListInvoices :many
SELECT * FROM invoice WHERE tenant_id = $1 ORDER BY period_start DESC;

-- name: IssueInvoice :one
UPDATE invoice SET status = 'issued', issued_at = now(), due_at = now() + interval '15 days',
    number = 'MV-' || to_char(now(), 'YYYY') || '-' || lpad(nextval('invoice_number_seq')::text, 6, '0')
WHERE id = $1 AND status = 'draft'
RETURNING *;

-- name: MarkInvoicePaid :exec
UPDATE invoice SET status = 'paid' WHERE id = $1 AND status = 'issued';

-- name: CreatePayment :one
INSERT INTO payment (id, invoice_id, provider, provider_payment_ref, amount_minor, currency, received_at, method_note, proof_object_key, recorded_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

-- name: SumPayments :one
SELECT COALESCE(sum(amount_minor), 0)::bigint FROM payment WHERE invoice_id = $1;

-- name: InsertProviderEvent :one
INSERT INTO payment_provider_event (provider, provider_event_id, payload) VALUES ($1, $2, $3)
ON CONFLICT DO NOTHING
RETURNING provider_event_id;
