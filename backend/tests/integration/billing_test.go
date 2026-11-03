//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"malgary/backend/internal/billing"
	"malgary/backend/internal/billing/providers"
	"malgary/backend/internal/billing/providers/manual"
	"malgary/backend/internal/httpapi"
	"malgary/backend/internal/httpapi/gen"
	"malgary/backend/internal/netbird"
	"malgary/backend/internal/secrets"
	"malgary/backend/internal/store/db"
	"malgary/backend/internal/tenancy"
	"malgary/backend/internal/users"
)

// TestPlansSubscriptionsAndPayments covers quickstart V8, V9, V10 (T066–T068, FR-015/016).
func TestPlansSubscriptionsAndPayments(t *testing.T) {
	ctx := context.Background()
	st := startPostgres(t)
	dir := t.TempDir()
	inv := filepath.Join(dir, "c01.yaml")
	_ = os.WriteFile(inv, []byte(inventory), 0o600)
	if err := tenancy.SyncInventory(ctx, st, inv); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	ref := "PTA-1"
	_, _ = st.MarkGatewayPairPTARegistered(ctx, db.MarkGatewayPairPTARegisteredParams{Name: "gw-nyc-1", PtaRegisteredAt: &now, PtaReference: &ref})

	bill := &billing.Service{Store: st, Providers: map[string]providers.Provider{"manual": manual.Provider{}},
		Now: func() time.Time { return time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC) }}

	// Owner prepares the enterprise plan before the tenant exists (slug reference).
	slug := "onedge"
	plan, err := bill.CreatePlan(ctx, gen.PlanInput{Code: "ent-onedge", Name: "On Edge enterprise", Visibility: gen.PlanInputVisibilityEnterprise, EnterpriseTenantSlug: &slug})
	if err != nil {
		t.Fatal(err)
	}
	v1, err := bill.DraftVersion(ctx, plan.Id, gen.PlanVersionInput{PricePerSeatMinor: 400, Currency: "USD", BillingPeriod: gen.PlanVersionInputBillingPeriodMonthly, MinSeats: 1,
		Entitlements: map[string]any{"posture_checks": true, "dedicated_ip": false, "usage_reports": "basic"}})
	if err != nil || v1.Version != 1 {
		t.Fatalf("draft v1: %+v %v", v1, err)
	}
	if _, err := bill.Publish(ctx, plan.Id, 1); err != nil {
		t.Fatal(err)
	}

	// Tenant apply subscribes to the pinned version (FR-016a).
	fake, nbSrv := newFakeNetBird(t)
	_ = fake
	ten := &tenancy.Service{Store: st, Secrets: secrets.Resolver{Dir: filepath.Join(dir, "s")}, NetBird: netbird.NewFactory(100),
		Deployer: &recordingDeployer{}, BaseDomain: strings.TrimPrefix(nbSrv.URL, "http://"), NetbirdVersion: "0.82.0", Billing: bill}
	tn, _, err := ten.Apply(ctx, onedgeDefinition())
	if err != nil {
		t.Fatal("apply:", err)
	}
	sub, err := bill.Get(ctx, tn.Id)
	if err != nil || sub.PlanVersion != 1 || sub.Quantity != 10 {
		t.Fatalf("subscription %+v %v", sub, err)
	}

	// V9: publishing v2 at a new price leaves the tenant on v1; v1 can't be edited.
	if _, err := bill.DraftVersion(ctx, plan.Id, gen.PlanVersionInput{PricePerSeatMinor: 450, Currency: "USD", BillingPeriod: gen.PlanVersionInputBillingPeriodMonthly, MinSeats: 1, Entitlements: map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := bill.Publish(ctx, plan.Id, 2); err != nil {
		t.Fatal(err)
	}
	if sub, _ := bill.Get(ctx, tn.Id); sub.PlanVersion != 1 {
		t.Fatalf("existing subscription moved to v%d without a migration", sub.PlanVersion)
	}
	_, err = st.Pool.Exec(ctx, "UPDATE plan_version SET price_per_seat_minor = 1 WHERE version = 1")
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || !strings.Contains(pg.Message, "plan_version_immutable") {
		t.Fatalf("editing a superseded version must fail, got %v", err)
	}
	if _, err := bill.Publish(ctx, plan.Id, 1); err == nil {
		t.Fatal("re-publishing v1 must fail")
	}
	plans, _ := bill.ListPlans(ctx)
	if len(plans) != 1 || len(plans[0].Versions) != 2 {
		t.Fatalf("history must keep both versions: %+v", plans)
	}

	// V8: seat limit from the subscription (FR-016).
	us := &users.Service{Store: st, Seats: bill}
	var batch []gen.UserImport
	for i := 0; i < 10; i++ {
		batch = append(batch, gen.UserImport{Email: openapi_types.Email(strings.Repeat("u", i+1) + "@onedge.co")})
	}
	if res, err := us.Import(ctx, tn.Id, batch); err != nil || res.Created != 10 {
		t.Fatalf("import 10: %+v %v", res, err)
	}
	_, err = us.Import(ctx, tn.Id, []gen.UserImport{{Email: "eleventh@onedge.co"}})
	var ae *httpapi.APIError
	if !errors.As(err, &ae) || ae.Code != gen.SeatLimitReached {
		t.Fatalf("11th user must hit seat_limit_reached, got %v", err)
	}
	if _, err := us.Import(ctx, tn.Id, []gen.UserImport{{Email: "someone@gmail.com"}}); err == nil {
		t.Fatal("addresses outside allowed domains must be refused (FR-003)")
	}
	if users, _ := us.List(ctx, tn.Id); len(users) != 10 || !contains(*users[0].Groups, "route:full") {
		t.Fatalf("users must be in all-users and the default routing group: %+v", users[0])
	}

	// Raising quantity through the definition is a recorded change; re-applying is a no-op.
	def := onedgeDefinition()
	def.Spec.Subscription.Quantity = 50
	if _, created, err := ten.Apply(ctx, def); err != nil || created {
		t.Fatalf("apply quantity 50: %v", err)
	}
	var auditN int
	_ = st.Pool.QueryRow(ctx, "SELECT count(*) FROM audit_record").Scan(&auditN)
	if _, _, err := ten.Apply(ctx, def); err != nil {
		t.Fatal(err)
	}
	var auditN2 int
	_ = st.Pool.QueryRow(ctx, "SELECT count(*) FROM audit_record").Scan(&auditN2)
	if auditN2 != auditN {
		t.Fatalf("re-apply wrote %d audit records", auditN2-auditN)
	}
	hist, _ := bill.History(ctx, tn.Id)
	if len(hist) != 2 || hist[1].Type != "quantity_changed" {
		b, _ := json.Marshal(hist)
		t.Fatalf("history: %s", b)
	}

	// V10: manual invoice and payment.
	inv1, err := bill.DraftInvoice(ctx, tn.Id, time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if inv1.TotalMinor < 50*400 {
		t.Fatalf("invoice total %d must cover 50 seats × $4 plus proration", inv1.TotalMinor)
	}
	issued, err := bill.IssueInvoice(ctx, inv1.Id)
	if err != nil || issued.Number == nil || !strings.HasPrefix(*issued.Number, "MV-") {
		t.Fatalf("issue: %+v %v", issued, err)
	}
	if _, err := st.Pool.Exec(ctx, "UPDATE invoice SET total_minor = 1 WHERE id = $1", inv1.Id); err == nil {
		t.Fatal("issued invoices must be immutable")
	}
	if _, err := bill.RecordPayment(ctx, inv1.Id, gen.PaymentInput{AmountMinor: issued.TotalMinor, Currency: "USD", ReceivedAt: time.Now(), Reference: "BANK-REF-1"}); err != nil {
		t.Fatal(err)
	}
	invs, _ := bill.ListInvoices(ctx, tn.Id)
	if invs[0].Status != gen.InvoiceStatusPaid {
		t.Fatalf("invoice should be paid, is %s", invs[0].Status)
	}
	if _, err := bill.RecordPayment(ctx, inv1.Id, gen.PaymentInput{AmountMinor: 1, Currency: "USD", ReceivedAt: time.Now(), Reference: "BANK-REF-1"}); err == nil {
		t.Fatal("a paid invoice must refuse further payments")
	}
	if err := bill.HandleWebhook(ctx, "manual", nil, []byte("{}")); err == nil {
		t.Fatal("manual provider has no webhooks: expect 404")
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
