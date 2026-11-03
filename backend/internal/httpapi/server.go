// Package httpapi serves the Malgary backend API (contracts/backend-api.md).
package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"malgary/backend/internal/httpapi/gen"
	"malgary/backend/internal/store"
)

// Handlers implements gen.ServerInterface. Feature packages add methods to it in their own
// files; operations not built yet answer 501 through the embedded Unimplemented.
type Handlers struct {
	gen.Unimplemented
	Deps
}

// Deps are the services handlers call. Fields are filled as features land.
type Deps struct {
	Store   *store.Store
	Tenancy TenancyService
	Users   UsersService
	Billing BillingService
	Ops     OpsService
	// ClaimsSigningKey verifies Zitadel Actions v2 calls to /hooks/zitadel/claims.
	ClaimsSigningKey []byte
}

// NewRouter assembles middleware and generated routes under /v1.
func NewRouter(h *Handlers, v Verifier) http.Handler {
	r := chi.NewRouter()
	r.Use(RequestID, AccessLog, middleware.Recoverer)
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	r.Post("/hooks/zitadel/claims", h.ZitadelClaims)

	r.Route("/v1", func(api chi.Router) {
		api.Use(Authenticate(v, "/v1/providers/"))
		if h.Store != nil {
			api.Use(Idempotency(h.Store))
		}
		gen.HandlerWithOptions(h, gen.ChiServerOptions{
			BaseRouter: api,
			ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
				WriteError(w, r, Errf(http.StatusBadRequest, gen.InvalidRequest, err.Error()))
			},
		})
	})
	return r
}
