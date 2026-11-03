package billing

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"malgary/backend/internal/audit"
	"malgary/backend/internal/billing/providers"
	"malgary/backend/internal/httpapi"
	"malgary/backend/internal/httpapi/gen"
	"malgary/backend/internal/store/db"
)

// Line is one invoice line; amounts are integer minor units (Principle XI: money is exact).
type Line struct {
	Description    string `json:"description"`
	Quantity       int    `json:"quantity"`
	UnitPriceMinor int64  `json:"unitPriceMinor"`
	AmountMinor    int64  `json:"amountMinor"`
}

// DraftInvoice drafts the invoice for the period starting at periodStart: the plan version's
// seat price × the current quantity, plus proration for seats added during the previous period.
func (s *Service) DraftInvoice(ctx context.Context, tenantID uuid.UUID, periodStart time.Time) (gen.Invoice, error) {
	var inv db.Invoice
	err := s.Store.WithTx(ctx, func(q *db.Queries, _ pgx.Tx) error {
		sub, err := q.GetOpenSubscription(ctx, tenantID)
		if err != nil {
			return notFound(err)
		}
		v, err := q.GetPlanVersionByID(ctx, sub.PlanVersionID)
		if err != nil {
			return err
		}
		p, err := q.GetPlan(ctx, v.PlanID)
		if err != nil {
			return err
		}
		start := periodStart.UTC().Truncate(24 * time.Hour)
		end := periodEnd(start, v.BillingPeriod)
		lines := []Line{{
			Description:    fmt.Sprintf("%s v%d, %s to %s", p.Name, v.Version, start.Format("2 Jan 2006"), end.AddDate(0, 0, -1).Format("2 Jan 2006")),
			Quantity:       int(sub.Quantity),
			UnitPriceMinor: v.PricePerSeatMinor,
			AmountMinor:    v.PricePerSeatMinor * int64(sub.Quantity),
		}}
		pr, err := s.proration(ctx, q, tenantID, v, start)
		if err != nil {
			return err
		}
		lines = append(lines, pr...)
		var subtotal int64
		for _, l := range lines {
			subtotal += l.AmountMinor
		}
		body, _ := json.Marshal(lines)
		inv, err = q.CreateInvoice(ctx, db.CreateInvoiceParams{
			ID: uuid.Must(uuid.NewV7()), TenantID: tenantID, SubscriptionID: sub.ID, PeriodStart: start, PeriodEnd: end,
			Lines: body, SubtotalMinor: subtotal, TaxMinor: 0, TotalMinor: subtotal, Currency: v.Currency,
		})
		if isUnique(err) {
			return httpapi.Errf(http.StatusConflict, gen.InvalidRequest, "an invoice for this period already exists")
		}
		if err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{TenantID: &tenantID, Action: "invoice.drafted", TargetType: "invoice", TargetID: inv.ID.String(), After: lines})
	})
	if err != nil {
		return gen.Invoice{}, err
	}
	return s.invoiceView(ctx, inv)
}

// proration charges seats added during the previous period for the days they were used.
func (s *Service) proration(ctx context.Context, q *db.Queries, tenantID uuid.UUID, v db.PlanVersion, start time.Time) ([]Line, error) {
	prevStart := start.AddDate(0, -1, 0)
	if v.BillingPeriod == db.BillingPeriodAnnual {
		prevStart = start.AddDate(-1, 0, 0)
	}
	days := start.Sub(prevStart).Hours() / 24
	evs, err := q.ListSubscriptionEvents(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	var out []Line
	for _, e := range evs {
		if e.Type != "quantity_changed" || e.At.Before(prevStart) || !e.At.Before(start) {
			continue
		}
		var from, to struct{ Quantity int }
		if json.Unmarshal(e.FromValue, &from) != nil || json.Unmarshal(e.ToValue, &to) != nil || to.Quantity <= from.Quantity {
			continue // decreases take effect next period; no refunds mid-period
		}
		added := to.Quantity - from.Quantity
		used := start.Sub(e.At).Hours() / 24
		amount := int64(math.Round(float64(v.PricePerSeatMinor) * float64(added) * used / days))
		out = append(out, Line{
			Description:    fmt.Sprintf("Proration: %d seats added %s", added, e.At.Format("2 Jan 2006")),
			Quantity:       added,
			UnitPriceMinor: int64(math.Round(float64(v.PricePerSeatMinor) * used / days)),
			AmountMinor:    amount,
		})
	}
	return out, nil
}

// IssueInvoice numbers the invoice (it becomes immutable) and hands it to the subscription's
// payment provider. Manual leaves it awaiting a recorded payment.
func (s *Service) IssueInvoice(ctx context.Context, id uuid.UUID) (gen.Invoice, error) {
	var inv db.Invoice
	err := s.Store.WithTx(ctx, func(q *db.Queries, _ pgx.Tx) error {
		cur, err := q.GetInvoice(ctx, id)
		if err != nil {
			return notFound(err)
		}
		if cur.Status != db.InvoiceStatusDraft {
			return httpapi.Errf(http.StatusConflict, gen.InvalidRequest, "invoice is already "+string(cur.Status))
		}
		inv, err = q.IssueInvoice(ctx, id)
		if err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{TenantID: &inv.TenantID, Action: "invoice.issued", TargetType: "invoice", TargetID: id.String(),
			After: map[string]any{"number": inv.Number, "total": inv.TotalMinor, "currency": inv.Currency}})
	})
	if err != nil {
		return gen.Invoice{}, err
	}
	sub, err := s.Store.GetOpenSubscription(ctx, inv.TenantID)
	if err == nil {
		if p, ok := s.Providers[sub.PaymentProvider]; ok {
			if _, err := p.Collect(ctx, providers.Invoice{ID: inv.ID, Number: deref(inv.Number), TenantID: inv.TenantID,
				AmountMinor: inv.TotalMinor, Currency: inv.Currency, DueAt: derefTime(inv.DueAt)}); err != nil {
				return gen.Invoice{}, fmt.Errorf("collect via %s: %w", sub.PaymentProvider, err)
			}
		}
	}
	return s.invoiceView(ctx, inv)
}

// RecordPayment records a manual payment (FR-016c). The proof file goes to object storage; the
// invoice becomes paid once payments cover the total.
func (s *Service) RecordPayment(ctx context.Context, invoiceID uuid.UUID, in gen.PaymentInput) (gen.Payment, error) {
	inv, err := s.Store.GetInvoice(ctx, invoiceID)
	if err != nil {
		return gen.Payment{}, notFound(err)
	}
	if inv.Status != db.InvoiceStatusIssued {
		return gen.Payment{}, httpapi.Errf(http.StatusConflict, gen.InvalidRequest, "payments can only be recorded against issued, unpaid invoices")
	}
	if in.Currency != inv.Currency {
		return gen.Payment{}, bad("payment currency " + in.Currency + " doesn't match invoice currency " + inv.Currency)
	}
	var proofKey *string
	if in.ProofBase64 != nil && *in.ProofBase64 != "" {
		if s.Proofs == nil {
			return gen.Payment{}, errors.New("proof storage is not configured")
		}
		data, err := base64.StdEncoding.DecodeString(*in.ProofBase64)
		if err != nil {
			return gen.Payment{}, bad("proofBase64 is not valid base64")
		}
		name := "proof"
		if in.ProofFilename != nil {
			name = path.Base(strings.ReplaceAll(*in.ProofFilename, "\\", "/"))
		}
		key := fmt.Sprintf("payments/%s/%s/%s-%s", inv.TenantID, invoiceID, uuid.NewString()[:8], name)
		if err := s.Proofs.Put(ctx, key, data, http.DetectContentType(data)); err != nil {
			return gen.Payment{}, fmt.Errorf("store proof: %w", err)
		}
		proofKey = &key
	}
	var pay db.Payment
	err = s.Store.WithTx(ctx, func(q *db.Queries, _ pgx.Tx) error {
		note := ""
		if in.MethodNote != nil {
			note = *in.MethodNote
		}
		pay, err = q.CreatePayment(ctx, db.CreatePaymentParams{
			ID: uuid.Must(uuid.NewV7()), InvoiceID: invoiceID, Provider: "manual", ProviderPaymentRef: in.Reference,
			AmountMinor: in.AmountMinor, Currency: in.Currency, ReceivedAt: in.ReceivedAt, MethodNote: note,
			ProofObjectKey: proofKey, RecordedBy: audit.ActorFrom(ctx).ID,
		})
		if isUnique(err) {
			return httpapi.Errf(http.StatusConflict, gen.IdempotencyConflict, "a payment with reference "+in.Reference+" is already recorded")
		}
		if err != nil {
			return err
		}
		paid, err := q.SumPayments(ctx, invoiceID)
		if err != nil {
			return err
		}
		if paid >= inv.TotalMinor {
			if err := q.MarkInvoicePaid(ctx, invoiceID); err != nil {
				return err
			}
		}
		return audit.Record(ctx, q, audit.Entry{TenantID: &inv.TenantID, Action: "payment.recorded", TargetType: "payment", TargetID: pay.ID.String(),
			After: map[string]any{"invoice": inv.Number, "amount": in.AmountMinor, "currency": in.Currency, "reference": in.Reference}})
	})
	if err != nil {
		return gen.Payment{}, err
	}
	return gen.Payment{Id: pay.ID, InvoiceId: invoiceID, Provider: pay.Provider, ProviderPaymentRef: &pay.ProviderPaymentRef,
		AmountMinor: pay.AmountMinor, Currency: pay.Currency, ReceivedAt: pay.ReceivedAt, ProofObjectKey: pay.ProofObjectKey}, nil
}

// ListInvoices lists the tenant's invoices.
func (s *Service) ListInvoices(ctx context.Context, tenantID uuid.UUID) ([]gen.Invoice, error) {
	rows, err := s.Store.ListInvoices(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	out := make([]gen.Invoice, 0, len(rows))
	for _, r := range rows {
		v, err := s.invoiceView(ctx, r)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// HandleWebhook verifies a provider webhook and stores its events idempotently. Manual has no
// webhooks and answers 404 (contracts/backend-api.md).
func (s *Service) HandleWebhook(ctx context.Context, provider string, h http.Header, body []byte) error {
	p, ok := s.Providers[provider]
	if !ok {
		return httpapi.ErrNotFound
	}
	events, err := p.ParseWebhook(ctx, h, body)
	var unsupported providers.ErrWebhooksUnsupported
	if errors.As(err, &unsupported) {
		return httpapi.ErrNotFound
	}
	if err != nil {
		return httpapi.Errf(http.StatusBadRequest, gen.InvalidRequest, "webhook rejected")
	}
	for _, e := range events {
		payload, _ := json.Marshal(e)
		if _, err := s.Store.InsertProviderEvent(ctx, db.InsertProviderEventParams{Provider: provider, ProviderEventID: e.ProviderEventID, Payload: payload}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
	}
	return nil
}

func (s *Service) invoiceView(ctx context.Context, inv db.Invoice) (gen.Invoice, error) {
	var lines []Line
	if err := json.Unmarshal(inv.Lines, &lines); err != nil {
		return gen.Invoice{}, err
	}
	paid, err := s.Store.SumPayments(ctx, inv.ID)
	if err != nil {
		return gen.Invoice{}, err
	}
	out := gen.Invoice{
		Id: inv.ID, Number: inv.Number, Status: gen.InvoiceStatus(inv.Status), Currency: inv.Currency,
		PeriodStart: date(inv.PeriodStart), PeriodEnd: date(inv.PeriodEnd),
		SubtotalMinor: &inv.SubtotalMinor, TaxMinor: &inv.TaxMinor, TotalMinor: inv.TotalMinor, PaidMinor: &paid,
		IssuedAt: inv.IssuedAt, DueAt: inv.DueAt,
	}
	for _, l := range lines {
		d, q, u, a := l.Description, l.Quantity, l.UnitPriceMinor, l.AmountMinor
		out.Lines = append(out.Lines, struct {
			AmountMinor    *int64  `json:"amountMinor,omitempty"`
			Description    *string `json:"description,omitempty"`
			Quantity       *int    `json:"quantity,omitempty"`
			UnitPriceMinor *int64  `json:"unitPriceMinor,omitempty"`
		}{&a, &d, &q, &u})
	}
	return out, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func derefTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}
