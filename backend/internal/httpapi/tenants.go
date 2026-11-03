package httpapi

import (
	"net/http"

	"malgary/backend/internal/httpapi/gen"
	"malgary/backend/internal/store/db"
)

// CreateTenant applies a tenant definition (FR-014, FR-015).
func (h *Handlers) CreateTenant(w http.ResponseWriter, r *http.Request, _ gen.CreateTenantParams) {
	if err := Require(r.Context(), Owners...); err != nil {
		WriteError(w, r, err)
		return
	}
	var def gen.TenantDefinition
	if err := DecodeJSON(r, &def); err != nil {
		WriteError(w, r, err)
		return
	}
	t, created, err := h.Tenancy.Apply(r.Context(), def)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	WriteJSON(w, status, t)
}

// GetTenant returns one tenant.
func (h *Handlers) GetTenant(w http.ResponseWriter, r *http.Request, tenantID gen.TenantId) {
	if err := Require(r.Context(), TenantReaders...); err != nil {
		WriteError(w, r, err)
		return
	}
	t, err := h.Tenancy.Get(r.Context(), tenantID)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	w.Header().Set("ETag", ETag(t.Id, t.Status, t.StackStatus, t.NetbirdVersion))
	WriteJSON(w, http.StatusOK, t)
}

// ListTenants lists tenants for owners and operators.
func (h *Handlers) ListTenants(w http.ResponseWriter, r *http.Request, _ gen.ListTenantsParams) {
	if err := Require(r.Context(), OwnersOperators...); err != nil {
		WriteError(w, r, err)
		return
	}
	ts, err := h.Tenancy.List(r.Context())
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, gen.TenantList{Items: ts})
}

// ReportStack records the deployer's result (contracts/tenant-stack.md obligation 4).
func (h *Handlers) ReportStack(w http.ResponseWriter, r *http.Request, tenantID gen.TenantId) {
	if err := Require(r.Context(), db.ActorTypeDeployer, db.ActorTypeOperator); err != nil {
		WriteError(w, r, err)
		return
	}
	var rep gen.StackReport
	if err := DecodeJSON(r, &rep); err != nil {
		WriteError(w, r, err)
		return
	}
	if err := h.Tenancy.Report(r.Context(), tenantID, rep); err != nil {
		WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
