# Contract: Payment Provider Interface

This is the billing domain's only view of a payment gateway (Principle XI). The pilot ships
`manual`. `stripe`, `payoneer` and others are added later by implementing this interface. No
changes to plans, subscriptions, invoices or payments are needed.

```go
type Provider interface {
    Name() string // "manual", "stripe", …

    // Set up a payer for a tenant (e.g. Stripe customer). Manual: no-op, returns "".
    EnsureCustomer(ctx context.Context, t Tenant) (providerCustomerRef string, err error)

    // Collect an issued invoice. Manual: returns StatusAwaitingManual.
    // Gateways: create a payment intent or hosted invoice and return its reference and URL.
    Collect(ctx context.Context, inv Invoice) (CollectResult, error)

    // Verify and translate an incoming webhook into internal events.
    // Manual: not supported. Implementations must be idempotent on provider_event_id.
    ParseWebhook(ctx context.Context, headers http.Header, body []byte) ([]PaymentEvent, error)

    // Optional capabilities, discovered rather than assumed.
    Capabilities() Capabilities // {AutoCollect, Refunds, SavedMethods, Currencies []string}
}

type PaymentEvent struct {
    ProviderEventID string
    Kind            string // payment_succeeded | payment_failed | refunded | dispute_opened
    InvoiceID       uuid.UUID
    AmountMinor     int64
    Currency        string
    OccurredAt      time.Time
}
```

## Rules

- The billing domain owns invoice numbers, amounts, proration, dunning and suspension.
  Providers only collect money and report what happened.
- Manual payments are recorded through `POST /v1/invoices/{id}/payments` by an owner with a
  reference and optional proof. This creates the same `Payment` row a gateway would.
- A tenant's subscription names its provider. Changing provider affects future invoices only.
