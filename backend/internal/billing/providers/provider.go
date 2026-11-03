// Package providers defines the payment provider interface (contracts/payment-provider.md,
// constitution Principle XI). The billing domain owns invoices, numbers, amounts, proration,
// dunning and suspension; providers only collect money and report what happened.
package providers

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// Tenant is the payer as providers see it.
type Tenant struct {
	ID        uuid.UUID
	Slug      string
	LegalName string
}

// Invoice is an issued invoice handed to a provider for collection.
type Invoice struct {
	ID          uuid.UUID
	Number      string
	TenantID    uuid.UUID
	AmountMinor int64
	Currency    string
	DueAt       time.Time
}

// CollectStatus is what a provider did with an invoice.
type CollectStatus string

// Collect outcomes.
const (
	StatusAwaitingManual CollectStatus = "awaiting_manual"
	StatusPending        CollectStatus = "pending"
	StatusPaid           CollectStatus = "paid"
)

// CollectResult describes a collection attempt.
type CollectResult struct {
	Status      CollectStatus
	ProviderRef string
	PayURL      string // hosted payment page, when the provider has one
}

// PaymentEvent is a provider event translated to Malgary's model. Handling is idempotent on
// (provider, ProviderEventID).
type PaymentEvent struct {
	ProviderEventID string
	Kind            string // payment_succeeded | payment_failed | refunded | dispute_opened
	InvoiceID       uuid.UUID
	AmountMinor     int64
	Currency        string
	OccurredAt      time.Time
}

// Capabilities are discovered rather than assumed.
type Capabilities struct {
	AutoCollect  bool
	Refunds      bool
	SavedMethods bool
	Currencies   []string
}

// Provider is the billing domain's only view of a payment gateway.
type Provider interface {
	Name() string
	EnsureCustomer(ctx context.Context, t Tenant) (providerCustomerRef string, err error)
	Collect(ctx context.Context, inv Invoice) (CollectResult, error)
	ParseWebhook(ctx context.Context, headers http.Header, body []byte) ([]PaymentEvent, error)
	Capabilities() Capabilities
}

// ErrWebhooksUnsupported is returned by providers without webhooks (manual).
type ErrWebhooksUnsupported struct{ Provider string }

func (e ErrWebhooksUnsupported) Error() string { return e.Provider + " has no webhooks" }
