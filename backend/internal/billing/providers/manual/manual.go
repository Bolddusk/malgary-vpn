// Package manual is the pilot payment provider: an owner records each payment with a reference
// and proof (FR-016c). It creates the same Payment rows a gateway would.
package manual

import (
	"context"
	"net/http"

	"malgary/backend/internal/billing/providers"
)

// Provider implements providers.Provider for manual payments.
type Provider struct{}

// Name is stored on subscriptions and payments.
func (Provider) Name() string { return "manual" }

// EnsureCustomer has nothing to set up.
func (Provider) EnsureCustomer(context.Context, providers.Tenant) (string, error) { return "", nil }

// Collect leaves the invoice for an owner to record payment.
func (Provider) Collect(context.Context, providers.Invoice) (providers.CollectResult, error) {
	return providers.CollectResult{Status: providers.StatusAwaitingManual}, nil
}

// ParseWebhook is not supported.
func (Provider) ParseWebhook(context.Context, http.Header, []byte) ([]providers.PaymentEvent, error) {
	return nil, providers.ErrWebhooksUnsupported{Provider: "manual"}
}

// Capabilities: manual supports any currency, nothing automatic.
func (Provider) Capabilities() providers.Capabilities {
	return providers.Capabilities{Currencies: []string{"*"}}
}
