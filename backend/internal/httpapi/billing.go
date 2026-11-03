package httpapi

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"

	"malgary/backend/internal/httpapi/gen"
)

// BillingService is implemented by internal/billing.
type BillingService interface {
	CreatePlan(ctx context.Context, in gen.PlanInput) (gen.Plan, error)
	DraftVersion(ctx context.Context, planID uuid.UUID, in gen.PlanVersionInput) (gen.PlanVersion, error)
	Publish(ctx context.Context, planID uuid.UUID, version int) (gen.PlanVersion, error)
	ListPlans(ctx context.Context) ([]gen.Plan, error)
	Subscribe(ctx context.Context, tenantID uuid.UUID, in gen.SubscriptionInput) (gen.Subscription, error)
	Change(ctx context.Context, tenantID uuid.UUID, in gen.SubscriptionChange) (gen.Subscription, error)
	Get(ctx context.Context, tenantID uuid.UUID) (gen.Subscription, error)
	History(ctx context.Context, tenantID uuid.UUID) ([]gen.SubscriptionEvent, error)
	DraftInvoice(ctx context.Context, tenantID uuid.UUID, periodStart time.Time) (gen.Invoice, error)
	IssueInvoice(ctx context.Context, id uuid.UUID) (gen.Invoice, error)
	RecordPayment(ctx context.Context, invoiceID uuid.UUID, in gen.PaymentInput) (gen.Payment, error)
	ListInvoices(ctx context.Context, tenantID uuid.UUID) ([]gen.Invoice, error)
	HandleWebhook(ctx context.Context, provider string, h http.Header, body []byte) error
}

// ListPlans: owners see every plan and version (history is kept).
func (h *Handlers) ListPlans(w http.ResponseWriter, r *http.Request) {
	if err := Require(r.Context(), Owners...); err != nil {
		WriteError(w, r, err)
		return
	}
	ps, err := h.Billing.ListPlans(r.Context())
	respond(w, r, http.StatusOK, ps, err)
}

// CreatePlan creates a plan (owner).
func (h *Handlers) CreatePlan(w http.ResponseWriter, r *http.Request, _ gen.CreatePlanParams) {
	var in gen.PlanInput
	if !ownerBody(w, r, &in) {
		return
	}
	p, err := h.Billing.CreatePlan(r.Context(), in)
	respond(w, r, http.StatusCreated, p, err)
}

// DraftPlanVersion drafts a new version (owner).
func (h *Handlers) DraftPlanVersion(w http.ResponseWriter, r *http.Request, planID gen.PlanId, _ gen.DraftPlanVersionParams) {
	var in gen.PlanVersionInput
	if !ownerBody(w, r, &in) {
		return
	}
	v, err := h.Billing.DraftVersion(r.Context(), planID, in)
	respond(w, r, http.StatusCreated, v, err)
}

// PublishPlanVersion publishes a draft (owner).
func (h *Handlers) PublishPlanVersion(w http.ResponseWriter, r *http.Request, planID gen.PlanId, version int, _ gen.PublishPlanVersionParams) {
	if err := Require(r.Context(), Owners...); err != nil {
		WriteError(w, r, err)
		return
	}
	v, err := h.Billing.Publish(r.Context(), planID, version)
	respond(w, r, http.StatusOK, v, err)
}

// GetSubscription returns the tenant's subscription.
func (h *Handlers) GetSubscription(w http.ResponseWriter, r *http.Request, tenantID gen.TenantId) {
	if err := Require(r.Context(), TenantReaders...); err != nil {
		WriteError(w, r, err)
		return
	}
	s, err := h.Billing.Get(r.Context(), tenantID)
	respond(w, r, http.StatusOK, s, err)
}

// CreateSubscription subscribes a tenant (owner).
func (h *Handlers) CreateSubscription(w http.ResponseWriter, r *http.Request, tenantID gen.TenantId, _ gen.CreateSubscriptionParams) {
	var in gen.SubscriptionInput
	if !ownerBody(w, r, &in) {
		return
	}
	s, err := h.Billing.Subscribe(r.Context(), tenantID, in)
	respond(w, r, http.StatusCreated, s, err)
}

// ChangeSubscription changes quantity or migrates the plan version (owner).
func (h *Handlers) ChangeSubscription(w http.ResponseWriter, r *http.Request, tenantID gen.TenantId, _ gen.ChangeSubscriptionParams) {
	var in gen.SubscriptionChange
	if !ownerBody(w, r, &in) {
		return
	}
	s, err := h.Billing.Change(r.Context(), tenantID, in)
	respond(w, r, http.StatusOK, s, err)
}

// GetSubscriptionHistory returns subscription events.
func (h *Handlers) GetSubscriptionHistory(w http.ResponseWriter, r *http.Request, tenantID gen.TenantId) {
	if err := Require(r.Context(), TenantReaders...); err != nil {
		WriteError(w, r, err)
		return
	}
	ev, err := h.Billing.History(r.Context(), tenantID)
	respond(w, r, http.StatusOK, ev, err)
}

// ListInvoices lists invoices.
func (h *Handlers) ListInvoices(w http.ResponseWriter, r *http.Request, tenantID gen.TenantId) {
	if err := Require(r.Context(), TenantReaders...); err != nil {
		WriteError(w, r, err)
		return
	}
	inv, err := h.Billing.ListInvoices(r.Context(), tenantID)
	respond(w, r, http.StatusOK, inv, err)
}

// DraftInvoice drafts an invoice (owner).
func (h *Handlers) DraftInvoice(w http.ResponseWriter, r *http.Request, tenantID gen.TenantId, _ gen.DraftInvoiceParams) {
	var in gen.DraftInvoiceJSONRequestBody
	if !ownerBody(w, r, &in) {
		return
	}
	inv, err := h.Billing.DraftInvoice(r.Context(), tenantID, in.PeriodStart.Time)
	respond(w, r, http.StatusCreated, inv, err)
}

// IssueInvoice issues a draft (owner).
func (h *Handlers) IssueInvoice(w http.ResponseWriter, r *http.Request, invoiceID gen.InvoiceId, _ gen.IssueInvoiceParams) {
	if err := Require(r.Context(), Owners...); err != nil {
		WriteError(w, r, err)
		return
	}
	inv, err := h.Billing.IssueInvoice(r.Context(), invoiceID)
	respond(w, r, http.StatusOK, inv, err)
}

// RecordPayment records a manual payment (owner).
func (h *Handlers) RecordPayment(w http.ResponseWriter, r *http.Request, invoiceID gen.InvoiceId, _ gen.RecordPaymentParams) {
	var in gen.PaymentInput
	if !ownerBody(w, r, &in) {
		return
	}
	p, err := h.Billing.RecordPayment(r.Context(), invoiceID, in)
	respond(w, r, http.StatusCreated, p, err)
}

// ProviderWebhook stores verified provider events; 404 for providers without webhooks.
func (h *Handlers) ProviderWebhook(w http.ResponseWriter, r *http.Request, provider string) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		WriteError(w, r, Errf(http.StatusBadRequest, gen.InvalidRequest, "body too large"))
		return
	}
	if err := h.Billing.HandleWebhook(r.Context(), provider, r.Header, body); err != nil {
		WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func ownerBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := Require(r.Context(), Owners...); err != nil {
		WriteError(w, r, err)
		return false
	}
	if err := DecodeJSON(r, v); err != nil {
		WriteError(w, r, err)
		return false
	}
	return true
}

func respond(w http.ResponseWriter, r *http.Request, status int, v any, err error) {
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, status, v)
}
