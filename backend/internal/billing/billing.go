// Package billing is Malgary's commercial domain (constitution Principle XI): owner-defined,
// versioned plans; subscriptions pinned to a plan version with full history; entitlements;
// invoices and payments through a provider interface (manual in the pilot).
package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"malgary/backend/internal/audit"
	"malgary/backend/internal/billing/providers"
	"malgary/backend/internal/httpapi"
	"malgary/backend/internal/httpapi/gen"
	"malgary/backend/internal/interim"
	"malgary/backend/internal/store"
	"malgary/backend/internal/store/db"
)

// ObjectStore keeps payment proofs (Spaces in production).
type ObjectStore interface {
	Put(ctx context.Context, key string, body []byte, contentType string) error
}

// Service implements plans, subscriptions, entitlements, invoices and payments.
type Service struct {
	Store     *store.Store
	Providers map[string]providers.Provider
	Proofs    ObjectStore
	Now       func() time.Time
	Interim   *interim.Limits // nil or inactive on dedicated cells
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

var (
	errImmutable = httpapi.Errf(http.StatusConflict, gen.PlanVersionImmutable, "published plan versions can't be changed; draft a new version")
	bad          = func(msg string) error { return httpapi.Errf(http.StatusUnprocessableEntity, gen.InvalidRequest, msg) }
)

// ---- Plans ----

// CreatePlan creates a plan. Enterprise plans name the one tenant (by slug) that may use them.
func (s *Service) CreatePlan(ctx context.Context, in gen.PlanInput) (gen.Plan, error) {
	if (in.Visibility == gen.PlanInputVisibilityEnterprise) != (in.EnterpriseTenantSlug != nil && *in.EnterpriseTenantSlug != "") {
		return gen.Plan{}, bad("enterprise plans need enterpriseTenantSlug; public plans must not have one")
	}
	var p db.Plan
	err := s.Store.WithTx(ctx, func(q *db.Queries, _ pgx.Tx) error {
		var err error
		p, err = q.CreatePlan(ctx, db.CreatePlanParams{
			ID: uuid.Must(uuid.NewV7()), Code: in.Code, Visibility: db.PlanVisibility(in.Visibility),
			EnterpriseTenantSlug: in.EnterpriseTenantSlug, Name: in.Name,
		})
		if isUnique(err) {
			return httpapi.Errf(http.StatusConflict, gen.InvalidRequest, "plan "+in.Code+" already exists")
		}
		if err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{Action: "plan.created", TargetType: "plan", TargetID: p.ID.String(), After: in})
	})
	if err != nil {
		return gen.Plan{}, err
	}
	return s.planView(ctx, p)
}

// DraftVersion adds a draft version with the next number.
func (s *Service) DraftVersion(ctx context.Context, planID uuid.UUID, in gen.PlanVersionInput) (gen.PlanVersion, error) {
	if in.MinSeats > 1_000_000 || (in.MaxSeats != nil && *in.MaxSeats > 1_000_000) {
		return gen.PlanVersion{}, bad("seat bounds above 1,000,000 are not supported")
	}
	if in.MaxSeats != nil && *in.MaxSeats < in.MinSeats {
		return gen.PlanVersion{}, bad("maxSeats must be at least minSeats")
	}
	ent, err := json.Marshal(in.Entitlements)
	if err != nil {
		return gen.PlanVersion{}, err
	}
	var v db.PlanVersion
	err = s.Store.WithTx(ctx, func(q *db.Queries, _ pgx.Tx) error {
		if _, err := q.GetPlan(ctx, planID); err != nil {
			return notFound(err)
		}
		n, err := q.NextPlanVersionNumber(ctx, planID)
		if err != nil {
			return err
		}
		terms := ""
		if in.Terms != nil {
			terms = *in.Terms
		}
		v, err = q.CreatePlanVersion(ctx, db.CreatePlanVersionParams{
			ID: uuid.Must(uuid.NewV7()), PlanID: planID, Version: n, PricePerSeatMinor: in.PricePerSeatMinor,
			Currency: in.Currency, BillingPeriod: db.BillingPeriod(in.BillingPeriod), MinSeats: int32(in.MinSeats), //nolint:gosec // bounded above
			MaxSeats: int32p(in.MaxSeats), Entitlements: ent, Terms: terms,
		})
		if err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{Action: "plan_version.drafted", TargetType: "plan_version", TargetID: v.ID.String(), After: in})
	})
	if err != nil {
		return gen.PlanVersion{}, err
	}
	return versionView(v)
}

// Publish publishes a draft; the previously published version becomes superseded. Existing
// subscriptions keep the version they bought (FR-016b).
func (s *Service) Publish(ctx context.Context, planID uuid.UUID, version int) (gen.PlanVersion, error) {
	var v db.PlanVersion
	err := s.Store.WithTx(ctx, func(q *db.Queries, _ pgx.Tx) error {
		cur, err := q.GetPlanVersion(ctx, db.GetPlanVersionParams{PlanID: planID, Version: int32(version)}) //nolint:gosec // version numbers are small
		if err != nil {
			return notFound(err)
		}
		if cur.Status != db.PlanVersionStatusDraft {
			return errImmutable
		}
		if err := q.SupersedePublished(ctx, planID); err != nil {
			return err
		}
		v, err = q.PublishPlanVersion(ctx, db.PublishPlanVersionParams{PlanID: planID, Version: int32(version), PublishedBy: strp(audit.ActorFrom(ctx).ID)}) //nolint:gosec // version numbers are small
		if err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{Action: "plan_version.published", TargetType: "plan_version", TargetID: v.ID.String(),
			After: map[string]any{"plan": planID, "version": version}})
	})
	if err != nil {
		return gen.PlanVersion{}, err
	}
	return versionView(v)
}

// ListPlans returns every plan with all its versions (history is never deleted).
func (s *Service) ListPlans(ctx context.Context) ([]gen.Plan, error) {
	ps, err := s.Store.ListPlans(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]gen.Plan, 0, len(ps))
	for _, p := range ps {
		v, err := s.planView(ctx, p)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// PlanByCode looks a plan up by code.
func (s *Service) PlanByCode(ctx context.Context, code string) (gen.Plan, error) {
	p, err := s.Store.GetPlanByCode(ctx, code)
	if err != nil {
		return gen.Plan{}, notFound(err)
	}
	return s.planView(ctx, p)
}

// ---- Subscriptions ----

// Subscribe pins the tenant to a published plan version (FR-016a).
func (s *Service) Subscribe(ctx context.Context, tenantID uuid.UUID, in gen.SubscriptionInput) (gen.Subscription, error) {
	var sub db.Subscription
	err := s.Store.WithTx(ctx, func(q *db.Queries, _ pgx.Tx) error {
		var err error
		sub, err = s.subscribeTx(ctx, q, tenantID, in)
		return err
	})
	if err != nil {
		return gen.Subscription{}, err
	}
	return s.subscriptionView(ctx, sub)
}

// SubscribeTx is Subscribe inside a caller's transaction (tenant apply).
func (s *Service) SubscribeTx(ctx context.Context, q *db.Queries, tenantID uuid.UUID, in gen.SubscriptionInput) error {
	_, err := s.subscribeTx(ctx, q, tenantID, in)
	return err
}

func (s *Service) subscribeTx(ctx context.Context, q *db.Queries, tenantID uuid.UUID, in gen.SubscriptionInput) (db.Subscription, error) {
	t, err := q.GetTenant(ctx, tenantID)
	if err != nil {
		return db.Subscription{}, notFound(err)
	}
	if _, err := q.GetOpenSubscription(ctx, tenantID); err == nil {
		return db.Subscription{}, httpapi.Errf(http.StatusConflict, gen.InvalidRequest, "tenant already has a subscription; use subscription:change")
	}
	v, err := s.usableVersion(ctx, q, t, in.PlanCode, in.PlanVersion)
	if err != nil {
		return db.Subscription{}, err
	}
	if err := seatBounds(v, in.Quantity); err != nil {
		return db.Subscription{}, err
	}
	provider := "manual"
	if in.PaymentProvider != "" {
		provider = in.PaymentProvider
	}
	if _, ok := s.Providers[provider]; !ok {
		return db.Subscription{}, bad("unknown payment provider " + provider)
	}
	start := firstOfMonth(s.now())
	if in.PeriodStart != nil {
		start = in.PeriodStart.Time
	}
	sub, err := q.CreateSubscription(ctx, db.CreateSubscriptionParams{
		ID: uuid.Must(uuid.NewV7()), TenantID: tenantID, PlanVersionID: v.ID, Quantity: int32(in.Quantity), //nolint:gosec // bounded by seatBounds
		CurrentPeriodStart: start, CurrentPeriodEnd: periodEnd(start, v.BillingPeriod), PaymentProvider: provider,
	})
	if err != nil {
		return db.Subscription{}, err
	}
	if err := event(ctx, q, sub.ID, "created", nil, map[string]any{"plan": in.PlanCode, "version": in.PlanVersion, "quantity": in.Quantity}, ""); err != nil {
		return db.Subscription{}, err
	}
	return sub, audit.Record(ctx, q, audit.Entry{TenantID: &tenantID, Action: "subscription.created", TargetType: "subscription", TargetID: sub.ID.String(), After: in})
}

// Change changes quantity and/or migrates to another plan version, always with a reason.
func (s *Service) Change(ctx context.Context, tenantID uuid.UUID, in gen.SubscriptionChange) (gen.Subscription, error) {
	err := s.Store.WithTx(ctx, func(q *db.Queries, _ pgx.Tx) error {
		return s.ChangeTx(ctx, q, tenantID, in)
	})
	if err != nil {
		return gen.Subscription{}, err
	}
	return s.Get(ctx, tenantID)
}

// ChangeTx is Change inside a caller's transaction (tenant apply).
func (s *Service) ChangeTx(ctx context.Context, q *db.Queries, tenantID uuid.UUID, in gen.SubscriptionChange) error {
	var sub db.Subscription
	err := func() error {
		cur, err := q.GetOpenSubscription(ctx, tenantID)
		if err != nil {
			return notFound(err)
		}
		t, err := q.GetTenant(ctx, tenantID)
		if err != nil {
			return err
		}
		version, err := q.GetPlanVersionByID(ctx, cur.PlanVersionID)
		if err != nil {
			return err
		}
		qty := int(cur.Quantity)
		if in.PlanCode != nil || in.PlanVersion != nil {
			if in.PlanCode == nil || in.PlanVersion == nil {
				return bad("planCode and planVersion go together")
			}
			nv, err := s.usableVersion(ctx, q, t, *in.PlanCode, *in.PlanVersion)
			if err != nil {
				return err
			}
			if err := event(ctx, q, cur.ID, "plan_migrated", map[string]any{"planVersionId": cur.PlanVersionID}, map[string]any{"plan": *in.PlanCode, "version": *in.PlanVersion}, in.Reason); err != nil {
				return err
			}
			version = nv
		}
		if in.Quantity != nil && *in.Quantity != qty {
			held, err := q.CountSeatsHeld(ctx, tenantID)
			if err != nil {
				return err
			}
			if int64(*in.Quantity) < held {
				return httpapi.Errf(http.StatusUnprocessableEntity, gen.SeatLimitReached,
					fmt.Sprintf("%d users hold seats; remove users before lowering the quantity to %d", held, *in.Quantity))
			}
			if err := event(ctx, q, cur.ID, "quantity_changed", map[string]any{"quantity": qty}, map[string]any{"quantity": *in.Quantity}, in.Reason); err != nil {
				return err
			}
			qty = *in.Quantity
		}
		if err := seatBounds(version, qty); err != nil {
			return err
		}
		sub, err = q.UpdateSubscription(ctx, db.UpdateSubscriptionParams{ID: cur.ID, PlanVersionID: version.ID, Quantity: int32(qty), Status: cur.Status}) //nolint:gosec // bounded by seatBounds
		if err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{TenantID: &tenantID, Action: "subscription.changed", TargetType: "subscription", TargetID: sub.ID.String(), Before: cur, After: in})
	}()
	return err
}

// Get returns the tenant's open subscription.
func (s *Service) Get(ctx context.Context, tenantID uuid.UUID) (gen.Subscription, error) {
	sub, err := s.Store.GetOpenSubscription(ctx, tenantID)
	if err != nil {
		return gen.Subscription{}, notFound(err)
	}
	return s.subscriptionView(ctx, sub)
}

// History returns every subscription event for the tenant.
func (s *Service) History(ctx context.Context, tenantID uuid.UUID) ([]gen.SubscriptionEvent, error) {
	evs, err := s.Store.ListSubscriptionEvents(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	out := make([]gen.SubscriptionEvent, 0, len(evs))
	for _, e := range evs {
		var from, to map[string]any
		_ = json.Unmarshal(e.FromValue, &from)
		_ = json.Unmarshal(e.ToValue, &to)
		out = append(out, gen.SubscriptionEvent{Id: e.ID, Type: e.Type, At: e.At, Actor: e.Actor, Reason: e.Reason, FromValue: &from, ToValue: &to})
	}
	return out, nil
}

// PaidSeats implements users.SeatChecker: seats come from the subscription, never from the
// tenant (FR-016a). No subscription means no seats.
func (s *Service) PaidSeats(ctx context.Context, tenantID uuid.UUID) (int, error) {
	sub, err := s.Store.GetOpenSubscription(ctx, tenantID)
	if store.IsNotFound(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return int(sub.Quantity), nil
}

// Entitled reports an entitlement value from the tenant's subscribed plan version. The product
// checks entitlements, never plan names or prices (Principle XI).
func (s *Service) Entitled(ctx context.Context, tenantID uuid.UUID, key string) (any, bool, error) {
	sub, err := s.Store.GetOpenSubscription(ctx, tenantID)
	if store.IsNotFound(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	v, err := s.Store.GetPlanVersionByID(ctx, sub.PlanVersionID)
	if err != nil {
		return nil, false, err
	}
	var ent map[string]any
	if err := json.Unmarshal(v.Entitlements, &ent); err != nil {
		return nil, false, err
	}
	val, ok := ent[key]
	if !ok || val == false || val == nil {
		return val, false, nil
	}
	return val, true, nil
}

// Require returns entitlement_missing unless the tenant has key.
func (s *Service) Require(ctx context.Context, tenantID uuid.UUID, key string) error {
	_, ok, err := s.Entitled(ctx, tenantID, key)
	if err != nil {
		return err
	}
	if !ok {
		return httpapi.Errf(http.StatusForbidden, gen.EntitlementMissing, "the tenant's plan doesn't include "+key)
	}
	return nil
}

// usableVersion returns a published version the tenant may subscribe to.
func (s *Service) usableVersion(ctx context.Context, q *db.Queries, t db.Tenant, code string, version int) (db.PlanVersion, error) {
	p, err := q.GetPlanByCode(ctx, code)
	if err != nil {
		return db.PlanVersion{}, bad("unknown plan " + code)
	}
	if p.Visibility == db.PlanVisibilityEnterprise && (p.EnterpriseTenantSlug == nil || *p.EnterpriseTenantSlug != t.Slug) {
		return db.PlanVersion{}, bad("plan " + code + " is an enterprise plan for another tenant")
	}
	if p.Status != db.PlanStatusActive {
		return db.PlanVersion{}, bad("plan " + code + " is retired")
	}
	v, err := q.GetPlanVersion(ctx, db.GetPlanVersionParams{PlanID: p.ID, Version: int32(version)}) //nolint:gosec // version numbers are small
	if err != nil {
		return db.PlanVersion{}, bad(fmt.Sprintf("plan %s has no version %d", code, version))
	}
	if v.Status != db.PlanVersionStatusPublished {
		return db.PlanVersion{}, bad(fmt.Sprintf("plan %s version %d is %s; only the published version can be subscribed to", code, version, v.Status))
	}
	if err := s.Interim.CheckPlanPrice(t.Slug, v.PricePerSeatMinor); err != nil {
		return db.PlanVersion{}, err
	}
	return v, nil
}

func seatBounds(v db.PlanVersion, qty int) error {
	if qty > 1_000_000 {
		return bad("quantities above 1,000,000 seats are not supported")
	}
	if qty < int(v.MinSeats) || (v.MaxSeats != nil && qty > int(*v.MaxSeats)) {
		max := "unlimited"
		if v.MaxSeats != nil {
			max = fmt.Sprint(*v.MaxSeats)
		}
		return bad(fmt.Sprintf("quantity %d is outside this plan version's seats (%d to %s)", qty, v.MinSeats, max))
	}
	return nil
}

func event(ctx context.Context, q *db.Queries, subID uuid.UUID, typ string, from, to any, reason string) error {
	f, _ := json.Marshal(from)
	t, _ := json.Marshal(to)
	if from == nil {
		f = nil
	}
	var r *string
	if strings.TrimSpace(reason) != "" {
		r = &reason
	}
	return q.InsertSubscriptionEvent(ctx, db.InsertSubscriptionEventParams{
		ID: uuid.Must(uuid.NewV7()), SubscriptionID: subID, Type: typ, FromValue: f, ToValue: t, Actor: audit.ActorFrom(ctx).ID, Reason: r,
	})
}

// ---- views and helpers ----

func (s *Service) planView(ctx context.Context, p db.Plan) (gen.Plan, error) {
	vs, err := s.Store.ListPlanVersions(ctx, p.ID)
	if err != nil {
		return gen.Plan{}, err
	}
	out := gen.Plan{Id: p.ID, Code: p.Code, Name: p.Name, Visibility: gen.PlanVisibility(p.Visibility), Status: gen.PlanStatus(p.Status), EnterpriseTenantSlug: p.EnterpriseTenantSlug}
	for _, v := range vs {
		gv, err := versionView(v)
		if err != nil {
			return gen.Plan{}, err
		}
		out.Versions = append(out.Versions, gv)
	}
	return out, nil
}

func versionView(v db.PlanVersion) (gen.PlanVersion, error) {
	var ent map[string]any
	if err := json.Unmarshal(v.Entitlements, &ent); err != nil {
		return gen.PlanVersion{}, err
	}
	var max *int
	if v.MaxSeats != nil {
		m := int(*v.MaxSeats)
		max = &m
	}
	terms := v.Terms
	return gen.PlanVersion{
		Id: v.ID, Version: int(v.Version), Status: gen.PlanVersionStatus(v.Status), PublishedAt: v.PublishedAt,
		PricePerSeatMinor: v.PricePerSeatMinor, Currency: v.Currency, BillingPeriod: gen.PlanVersionBillingPeriod(v.BillingPeriod),
		MinSeats: int(v.MinSeats), MaxSeats: max, Entitlements: ent, Terms: &terms,
	}, nil
}

func (s *Service) subscriptionView(ctx context.Context, sub db.Subscription) (gen.Subscription, error) {
	v, err := s.Store.GetPlanVersionByID(ctx, sub.PlanVersionID)
	if err != nil {
		return gen.Subscription{}, err
	}
	p, err := s.Store.GetPlan(ctx, v.PlanID)
	if err != nil {
		return gen.Subscription{}, err
	}
	var ent map[string]any
	_ = json.Unmarshal(v.Entitlements, &ent)
	return gen.Subscription{
		Id: sub.ID, PlanCode: p.Code, PlanVersion: int(v.Version), Quantity: int(sub.Quantity),
		Status: gen.SubscriptionStatus(sub.Status), PaymentProvider: sub.PaymentProvider,
		CurrentPeriodStart: date(sub.CurrentPeriodStart), CurrentPeriodEnd: date(sub.CurrentPeriodEnd), Entitlements: &ent,
	}, nil
}

func date(t time.Time) *openapi_types.Date { return &openapi_types.Date{Time: t} }

func firstOfMonth(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func periodEnd(start time.Time, p db.BillingPeriod) time.Time {
	if p == db.BillingPeriodAnnual {
		return start.AddDate(1, 0, 0)
	}
	return start.AddDate(0, 1, 0)
}

func notFound(err error) error {
	if store.IsNotFound(err) {
		return httpapi.ErrNotFound
	}
	return err
}

func isUnique(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505"
}

func int32p(p *int) *int32 {
	if p == nil {
		return nil
	}
	v := int32(*p) //nolint:gosec // callers bound seat counts
	return &v
}

func strp(s string) *string { return &s }
