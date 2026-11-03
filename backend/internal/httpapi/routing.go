package httpapi

import (
	"net/http"

	"github.com/google/uuid"

	"malgary/backend/internal/httpapi/gen"
)

// GetRoutingRules returns the tenant's rules and every user's effective rule (FR-005).
func (h *Handlers) GetRoutingRules(w http.ResponseWriter, r *http.Request, tenantID gen.TenantId) {
	if err := Require(r.Context(), TenantReaders...); err != nil {
		WriteError(w, r, err)
		return
	}
	rr, err := h.Tenancy.GetRoutingRules(r.Context(), tenantID)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	w.Header().Set("ETag", ETag(rr.Rules, rr.CallExclusions))
	WriteJSON(w, http.StatusOK, rr)
}

// PutRoutingRules replaces the rules; empty destination lists are rejected (422).
func (h *Handlers) PutRoutingRules(w http.ResponseWriter, r *http.Request, tenantID gen.TenantId, _ gen.PutRoutingRulesParams) {
	if err := Require(r.Context(), TenantWriters...); err != nil {
		WriteError(w, r, err)
		return
	}
	cur, err := h.Tenancy.GetRoutingRules(r.Context(), tenantID)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	if err := CheckIfMatch(r, ETag(cur.Rules, cur.CallExclusions)); err != nil {
		WriteError(w, r, err)
		return
	}
	var body gen.PutRoutingRulesJSONRequestBody
	if err := DecodeJSON(r, &body); err != nil {
		WriteError(w, r, err)
		return
	}
	rr, err := h.Tenancy.PutRoutingRules(r.Context(), tenantID, body.Rules, body.CallExclusions)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	w.Header().Set("ETag", ETag(rr.Rules, rr.CallExclusions))
	WriteJSON(w, http.StatusOK, rr)
}

// ListGroups lists groups.
func (h *Handlers) ListGroups(w http.ResponseWriter, r *http.Request, tenantID gen.TenantId) {
	if err := Require(r.Context(), TenantReaders...); err != nil {
		WriteError(w, r, err)
		return
	}
	gs, err := h.Users.ListGroups(r.Context(), tenantID)
	respond(w, r, http.StatusOK, gs, err)
}

// CreateGroup creates a team group.
func (h *Handlers) CreateGroup(w http.ResponseWriter, r *http.Request, tenantID gen.TenantId, _ gen.CreateGroupParams) {
	var in gen.GroupInput
	if !writerBody(w, r, &in) {
		return
	}
	g, err := h.Users.CreateGroup(r.Context(), tenantID, in)
	respond(w, r, http.StatusCreated, g, err)
}

// PatchGroup renames a group or replaces its members.
func (h *Handlers) PatchGroup(w http.ResponseWriter, r *http.Request, tenantID gen.TenantId, groupID uuid.UUID, _ gen.PatchGroupParams) {
	var in gen.GroupInput
	if !writerBody(w, r, &in) {
		return
	}
	g, err := h.Users.PatchGroup(r.Context(), tenantID, groupID, in)
	respond(w, r, http.StatusOK, g, err)
}

// DeleteGroup deletes a team group.
func (h *Handlers) DeleteGroup(w http.ResponseWriter, r *http.Request, tenantID gen.TenantId, groupID uuid.UUID) {
	if err := Require(r.Context(), TenantWriters...); err != nil {
		WriteError(w, r, err)
		return
	}
	if err := h.Users.DeleteGroup(r.Context(), tenantID, groupID); err != nil {
		WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writerBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := Require(r.Context(), TenantWriters...); err != nil {
		WriteError(w, r, err)
		return false
	}
	if err := DecodeJSON(r, v); err != nil {
		WriteError(w, r, err)
		return false
	}
	return true
}

// GetPostureRules returns posture rules (FR-018).
func (h *Handlers) GetPostureRules(w http.ResponseWriter, r *http.Request, tenantID gen.TenantId) {
	if err := Require(r.Context(), TenantReaders...); err != nil {
		WriteError(w, r, err)
		return
	}
	pr, err := h.Tenancy.GetPostureRules(r.Context(), tenantID)
	respond(w, r, http.StatusOK, pr, err)
}

// PutPostureRules replaces posture rules; needs the posture_checks entitlement.
func (h *Handlers) PutPostureRules(w http.ResponseWriter, r *http.Request, tenantID gen.TenantId, _ gen.PutPostureRulesParams) {
	var in []gen.PostureRule
	if !writerBody(w, r, &in) {
		return
	}
	pr, err := h.Tenancy.PutPostureRules(r.Context(), tenantID, in)
	respond(w, r, http.StatusOK, pr, err)
}
