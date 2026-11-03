package httpapi

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"malgary/backend/internal/httpapi/gen"
)

// InterimService is implemented by internal/interim (feature 002). Nil, or inactive, on
// dedicated cells: every interim route then answers 404 (contracts/interim-api.md).
type InterimService interface {
	Active() bool
	Status(ctx context.Context) (gen.InterimStatus, error)
	RecordIsolationTest(ctx context.Context, tenantID uuid.UUID, in gen.IsolationTestInput, ranBy string) (gen.IsolationTest, error)
	ListIsolationTests(ctx context.Context, tenantID uuid.UUID) ([]gen.IsolationTest, error)
	RunIsolationTest(ctx context.Context, tenantID uuid.UUID, ranBy string) (gen.IsolationTest, error)
	RecordRestoreTest(ctx context.Context, in gen.RestoreTestInput) error
}

func (h *Handlers) interimOn() bool { return h.Interim != nil && h.Interim.Active() }

// GetInterim reports the interim limits and usage (owner, operator).
func (h *Handlers) GetInterim(w http.ResponseWriter, r *http.Request) {
	if err := Require(r.Context(), OwnersOperators...); err != nil || !h.interimOn() {
		WriteError(w, r, ErrNotFound)
		return
	}
	st, err := h.Interim.Status(r.Context())
	respond(w, r, http.StatusOK, st, err)
}

// RecordIsolationTest stores a result from `malgaryctl isolation-test` (operator, FR-009).
func (h *Handlers) RecordIsolationTest(w http.ResponseWriter, r *http.Request, tenantID gen.TenantId, _ gen.RecordIsolationTestParams) {
	if err := Require(r.Context(), OwnersOperators...); err != nil || !h.interimOn() {
		WriteError(w, r, ErrNotFound)
		return
	}
	var in gen.IsolationTestInput
	if err := DecodeJSON(r, &in); err != nil {
		WriteError(w, r, err)
		return
	}
	res, err := h.Interim.RecordIsolationTest(r.Context(), tenantID, in, PrincipalFrom(r.Context()).Subject)
	respond(w, r, http.StatusCreated, res, err)
}

// RunIsolationTest runs the probe now and records the result (operator, FR-009).
func (h *Handlers) RunIsolationTest(w http.ResponseWriter, r *http.Request, tenantID gen.TenantId) {
	if err := Require(r.Context(), OwnersOperators...); err != nil || !h.interimOn() {
		WriteError(w, r, ErrNotFound)
		return
	}
	res, err := h.Interim.RunIsolationTest(r.Context(), tenantID, PrincipalFrom(r.Context()).Subject)
	respond(w, r, http.StatusCreated, res, err)
}

// ListIsolationTests returns the tenant's isolation history (owner, operator, its admins).
func (h *Handlers) ListIsolationTests(w http.ResponseWriter, r *http.Request, tenantID gen.TenantId) {
	if err := Require(r.Context(), TenantReaders...); err != nil || !h.interimOn() {
		WriteError(w, r, ErrNotFound)
		return
	}
	res, err := h.Interim.ListIsolationTests(r.Context(), tenantID)
	respond(w, r, http.StatusOK, res, err)
}

// RecordRestoreTest stores a backup restore test (operator, FR-015).
func (h *Handlers) RecordRestoreTest(w http.ResponseWriter, r *http.Request, _ gen.RecordRestoreTestParams) {
	if err := Require(r.Context(), OwnersOperators...); err != nil || !h.interimOn() {
		WriteError(w, r, ErrNotFound)
		return
	}
	var in gen.RestoreTestInput
	if err := DecodeJSON(r, &in); err != nil {
		WriteError(w, r, err)
		return
	}
	if err := h.Interim.RecordRestoreTest(r.Context(), in); err != nil {
		WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
}
