package httpapi

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"malgary/backend/internal/httpapi/gen"
	"malgary/backend/internal/store/db"
)

type fakeVerifier map[string]Principal

func (f fakeVerifier) Verify(_ context.Context, raw string) (Principal, error) {
	if p, ok := f[raw]; ok {
		return p, nil
	}
	return Principal{}, ErrUnauthorized
}

type fakeTenancy struct{ TenancyService }

func (fakeTenancy) Get(_ context.Context, id uuid.UUID) (gen.Tenant, error) {
	return gen.Tenant{Id: id, Slug: "onedge", Status: gen.TenantStatusActive}, nil
}

func TestTenantScopingAndRouting(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	v := fakeVerifier{
		"owner":  {Subject: "o1", Role: db.ActorTypeOwner},
		"adminA": {Subject: "u1", Role: db.ActorTypeTenantAdmin, TenantID: &a},
	}
	h := NewRouter(&Handlers{Deps: Deps{Tenancy: fakeTenancy{}}}, v)

	cases := []struct {
		name, token, method, path string
		want                      int
	}{
		{"no token", "", "GET", "/v1/tenants/" + a.String(), 401},
		{"admin own tenant", "adminA", "GET", "/v1/tenants/" + a.String(), 200},
		{"admin other tenant is 404 not 403", "adminA", "GET", "/v1/tenants/" + b.String(), 404},
		{"admin non-tenant route is 404", "adminA", "GET", "/v1/plans", 404},
		{"admin other tenant via colon action", "adminA", "POST", "/v1/tenants/" + b.String() + ":suspend", 404},
		{"owner reaches colon action route", "owner", "POST", "/v1/tenants/" + a.String() + ":resume", 501},
		{"health is public", "", "GET", "/healthz", 204},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(c.method, c.path, nil)
			if c.token != "" {
				req.Header.Set("Authorization", "Bearer "+c.token)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != c.want {
				t.Fatalf("%s %s: got %d want %d (%s)", c.method, c.path, rec.Code, c.want, rec.Body.String())
			}
		})
	}
}

func TestPrincipalFromClaims(t *testing.T) {
	tid := uuid.New()
	p, err := principalFromClaims("s", map[string]any{
		claimRoles:    map[string]any{"tenant_admin": map[string]any{}},
		claimTenantID: tid.String(),
	})
	if err != nil || p.Role != db.ActorTypeTenantAdmin || *p.TenantID != tid {
		t.Fatalf("got %+v %v", p, err)
	}
	if _, err := principalFromClaims("s", map[string]any{claimRoles: map[string]any{"tenant_admin": 1}}); err == nil {
		t.Fatal("tenant admin without tenant claim must be rejected")
	}
	if _, err := principalFromClaims("s", map[string]any{}); err == nil {
		t.Fatal("no role must be rejected")
	}
}
