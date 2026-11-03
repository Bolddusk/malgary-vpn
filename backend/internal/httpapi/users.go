package httpapi

import (
	"net/http"

	"malgary/backend/internal/httpapi/gen"
)

// ImportUsers bulk-imports users and sends invitations (FR-003, FR-013, FR-016).
func (h *Handlers) ImportUsers(w http.ResponseWriter, r *http.Request, tenantID gen.TenantId, _ gen.ImportUsersParams) {
	if err := Require(r.Context(), TenantWriters...); err != nil {
		WriteError(w, r, err)
		return
	}
	var body gen.ImportUsersJSONRequestBody
	if err := DecodeJSON(r, &body); err != nil {
		WriteError(w, r, err)
		return
	}
	res, err := h.Users.Import(r.Context(), tenantID, body.Users)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, res)
}

// ListUsers lists the tenant's users with their groups.
func (h *Handlers) ListUsers(w http.ResponseWriter, r *http.Request, tenantID gen.TenantId, _ gen.ListUsersParams) {
	if err := Require(r.Context(), TenantReaders...); err != nil {
		WriteError(w, r, err)
		return
	}
	us, err := h.Users.List(r.Context(), tenantID)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, gen.UserList{Items: us})
}

// GetIdentity returns the tenant's sign-in settings.
func (h *Handlers) GetIdentity(w http.ResponseWriter, r *http.Request, tenantID gen.TenantId) {
	if err := Require(r.Context(), TenantReaders...); err != nil {
		WriteError(w, r, err)
		return
	}
	id, err := h.Tenancy.GetIdentity(r.Context(), tenantID)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	w.Header().Set("ETag", ETag(id))
	WriteJSON(w, http.StatusOK, id)
}

// PutIdentity sets sign-in methods, upstream IdPs, allowed domains and MFA (FR-002). The
// customer admin chooses the methods for their own users (constitution v2.1.0 Principle VII).
func (h *Handlers) PutIdentity(w http.ResponseWriter, r *http.Request, tenantID gen.TenantId, _ gen.PutIdentityParams) {
	if err := Require(r.Context(), TenantWriters...); err != nil {
		WriteError(w, r, err)
		return
	}
	cur, err := h.Tenancy.GetIdentity(r.Context(), tenantID)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	if err := CheckIfMatch(r, ETag(cur)); err != nil {
		WriteError(w, r, err)
		return
	}
	var in gen.Identity
	if err := DecodeJSON(r, &in); err != nil {
		WriteError(w, r, err)
		return
	}
	out, err := h.Tenancy.PutIdentity(r.Context(), tenantID, in)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	w.Header().Set("ETag", ETag(out))
	WriteJSON(w, http.StatusOK, out)
}

// MarkGatewayPairPTA records PTA registration of a pair's egress IP (FR-011, T050).
func (h *Handlers) MarkGatewayPairPTA(w http.ResponseWriter, r *http.Request, name string) {
	if err := Require(r.Context(), Owners...); err != nil {
		WriteError(w, r, err)
		return
	}
	var body gen.MarkGatewayPairPTAJSONRequestBody
	if err := DecodeJSON(r, &body); err != nil {
		WriteError(w, r, err)
		return
	}
	if err := h.Tenancy.MarkPTARegistered(r.Context(), name, body.RegisteredAt, body.Reference); err != nil {
		WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}
