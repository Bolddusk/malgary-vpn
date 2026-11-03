package contract

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"malgary/backend/internal/httpapi"
	"malgary/backend/internal/httpapi/gen"
	"malgary/backend/internal/store/db"
)

type verifier map[string]httpapi.Principal

func (v verifier) Verify(_ context.Context, raw string) (httpapi.Principal, error) {
	if p, ok := v[raw]; ok {
		return p, nil
	}
	return httpapi.Principal{}, httpapi.ErrUnauthorized
}

// TestTenantAdminCannotReachOtherTenants walks every operation in the OpenAPI spec (T060,
// Principle I): a tenant admin of A gets 404 for every route about tenant B and for every
// route that isn't about a tenant at all, so no handler for B ever runs.
func TestTenantAdminCannotReachOtherTenants(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	spec, err := gen.GetSwagger()
	if err != nil {
		t.Fatal(err)
	}
	router := httpapi.NewRouter(&httpapi.Handlers{}, verifier{
		"adminA": {Subject: "u-a", Role: db.ActorTypeTenantAdmin, TenantID: &a},
	})
	ids := strings.NewReplacer(
		"{tenantId}", b.String(), "{userId}", uuid.NewString(), "{groupId}", uuid.NewString(),
		"{planId}", uuid.NewString(), "{invoiceId}", uuid.NewString(), "{cellId}", uuid.NewString(),
		"{version}", "1", "{name}", "gw-nyc-1", "{provider}", "stripe",
	)
	n := 0
	for path, item := range spec.Paths.Map() {
		if strings.HasPrefix(path, "/providers/") {
			continue // public webhook route, verified by signature instead
		}
		for method := range item.Operations() {
			req := httptest.NewRequest(method, "/v1"+ids.Replace(path), strings.NewReader("{}"))
			req.Header.Set("Authorization", "Bearer adminA")
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != 404 {
				t.Errorf("%s %s: tenant admin of another tenant got %d, want 404", method, path, rec.Code)
			}
			n++
		}
	}
	if n < 30 {
		t.Fatalf("only %d operations checked; spec walk is broken", n)
	}
}
