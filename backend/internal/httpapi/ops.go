package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"

	"malgary/backend/internal/httpapi/gen"
	"malgary/backend/internal/store/db"
)

// OpsService covers usage, audit and reconcile (US8).
type OpsService interface {
	Usage(ctx context.Context, tenantID uuid.UUID, from, to time.Time) (gen.Usage, error)
	Reconcile(ctx context.Context, tenantID uuid.UUID) (string, error)
}

// GetUsage: who is connected, last seen, bytes, never-installed and inactive lists.
func (h *Handlers) GetUsage(w http.ResponseWriter, r *http.Request, tenantID gen.TenantId, p gen.GetUsageParams) {
	if err := Require(r.Context(), TenantReaders...); err != nil {
		WriteError(w, r, err)
		return
	}
	to := time.Now()
	if p.To != nil {
		to = *p.To
	}
	from := to.AddDate(0, 0, -30)
	if p.From != nil {
		from = *p.From
	}
	u, err := h.Ops.Usage(r.Context(), tenantID, from, to)
	respond(w, r, http.StatusOK, u, err)
}

// ListAudit pages through the tenant's audit records, newest first (FR-026).
func (h *Handlers) ListAudit(w http.ResponseWriter, r *http.Request, tenantID gen.TenantId, p gen.ListAuditParams) {
	if err := Require(r.Context(), TenantReaders...); err != nil {
		WriteError(w, r, err)
		return
	}
	limit := 50
	if p.Limit != nil {
		limit = *p.Limit
	}
	var before *time.Time
	if p.Cursor != nil {
		t, err := time.Parse(time.RFC3339Nano, *p.Cursor)
		if err != nil {
			WriteError(w, r, Errf(http.StatusBadRequest, gen.InvalidRequest, "bad cursor"))
			return
		}
		before = &t
	}
	rows, err := h.Store.ListAuditPage(r.Context(), db.ListAuditPageParams{TenantID: &tenantID, Limit: int32(limit), BeforeAt: before})
	if err != nil {
		WriteError(w, r, err)
		return
	}
	out := gen.AuditList{Items: []gen.AuditRecord{}}
	for _, a := range rows {
		rec := gen.AuditRecord{Id: a.ID, At: a.At, ActorType: string(a.ActorType), ActorId: a.ActorID, Action: a.Action, TargetType: a.TargetType, TargetId: a.TargetID}
		var b, af map[string]any
		if json.Unmarshal(a.Before, &b) == nil && b != nil {
			rec.Before = &b
		}
		if json.Unmarshal(a.After, &af) == nil && af != nil {
			rec.After = &af
		}
		out.Items = append(out.Items, rec)
	}
	if len(rows) == limit {
		c := rows[len(rows)-1].At.Format(time.RFC3339Nano)
		out.NextCursor = &c
	}
	WriteJSON(w, http.StatusOK, out)
}

// ReconcileStack forces a reconcile run (operator).
func (h *Handlers) ReconcileStack(w http.ResponseWriter, r *http.Request, tenantID gen.TenantId) {
	if err := Require(r.Context(), OwnersOperators...); err != nil {
		WriteError(w, r, err)
		return
	}
	go func() { _, _ = h.Ops.Reconcile(context.WithoutCancel(r.Context()), tenantID) }()
	w.WriteHeader(http.StatusAccepted)
}
