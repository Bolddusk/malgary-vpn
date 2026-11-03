// Package netbird talks to each tenant's own NetBird management API (one stack per tenant,
// Principle V). Clients are built per tenant from the stack's URL and service-user PAT.
package netbird

import (
	"context"
	"net/http"
	"time"

	"github.com/netbirdio/netbird/shared/management/client/rest"
	"golang.org/x/time/rate"
)

// Factory builds per-tenant REST clients with a shared timeout, retry and rate limit.
type Factory struct {
	HTTP    *http.Client
	limiter *rate.Limiter
}

// NewFactory returns a factory limited to rps requests per second across all stacks.
func NewFactory(rps float64) *Factory {
	return &Factory{
		HTTP:    &http.Client{Timeout: 20 * time.Second, Transport: &retryTransport{base: http.DefaultTransport, tries: 3}},
		limiter: rate.NewLimiter(rate.Limit(rps), int(rps)+1),
	}
}

// For returns a client for one tenant stack, authenticated with the stack's PAT.
func (f *Factory) For(ctx context.Context, managementURL, pat string) (*rest.Client, error) {
	if err := f.limiter.Wait(ctx); err != nil {
		return nil, err
	}
	return rest.NewWithOptions(rest.WithManagementURL(managementURL), rest.WithPAT(pat), rest.WithHttpClient(f.HTTP)), nil
}

// Anonymous returns a client without credentials, for /api/instance and /api/setup.
func (f *Factory) Anonymous(managementURL string) *rest.Client {
	return rest.NewWithOptions(rest.WithManagementURL(managementURL), rest.WithHttpClient(f.HTTP))
}

// retryTransport retries idempotent requests on 5xx and network errors with backoff.
type retryTransport struct {
	base  http.RoundTripper
	tries int
}

func (t *retryTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	idempotent := r.Method == http.MethodGet || r.Method == http.MethodPut || r.Method == http.MethodDelete
	var resp *http.Response
	var err error
	for i := 0; i < t.tries; i++ {
		resp, err = t.base.RoundTrip(r)
		if !idempotent || (err == nil && resp.StatusCode < 500) {
			return resp, err
		}
		if i < t.tries-1 {
			if resp != nil {
				_ = resp.Body.Close()
			}
			select {
			case <-r.Context().Done():
				return nil, r.Context().Err()
			case <-time.After(time.Duration(1<<i) * 500 * time.Millisecond):
			}
		}
	}
	return resp, err
}
