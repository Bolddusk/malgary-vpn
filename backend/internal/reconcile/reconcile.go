// Package reconcile compares the backend's desired state with each tenant's NetBird stack and
// Zitadel org every few minutes, repairs safe drift and reports the rest (constitution
// Principle X: both systems of record are reconciled automatically, drift raises an alert).
package reconcile

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"malgary/backend/internal/store"
	"malgary/backend/internal/store/db"
)

// Repairer re-applies a tenant's desired state. tenancy.Service implements it.
type Repairer interface {
	// Reconcile returns the drift found and the repair actions taken.
	Reconcile(ctx context.Context, tenantID uuid.UUID) (drift []string, actions []string, err error)
}

// Loop runs reconcile passes.
type Loop struct {
	Store    *store.Store
	Repair   Repairer
	Interval time.Duration
}

// DriftOpen is exported as malgary_reconcile_drift_tenants (alert ReconcileDrift).
var DriftOpen atomic.Int64

// Run reconciles every tenant each Interval until ctx ends.
func (l *Loop) Run(ctx context.Context) {
	if l.Interval == 0 {
		l.Interval = 10 * time.Minute
	}
	t := time.NewTicker(l.Interval)
	defer t.Stop()
	for {
		l.Pass(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Pass reconciles all tenants once.
func (l *Loop) Pass(ctx context.Context) {
	tenants, err := l.Store.ListActiveTenantsWithStacks(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "reconcile: list tenants", "err", err)
		return
	}
	var drifting int64
	for _, t := range tenants {
		if st, _ := l.One(ctx, t.ID); st == "drift" || st == "failed" {
			drifting++
		}
	}
	DriftOpen.Store(drifting)
}

// One reconciles a single tenant and records the run.
func (l *Loop) One(ctx context.Context, tenantID uuid.UUID) (string, error) {
	run, err := l.Store.StartReconcileRun(ctx, db.StartReconcileRunParams{ID: uuid.Must(uuid.NewV7()), TenantID: tenantID})
	if err != nil {
		return "", err
	}
	drift, actions, rerr := l.Repair.Reconcile(ctx, tenantID)
	status := "clean"
	switch {
	case rerr != nil:
		status = "failed"
		drift = append(drift, rerr.Error())
	case len(drift) > 0 && len(actions) >= len(drift):
		status = "repaired"
	case len(drift) > 0:
		status = "drift"
	}
	d, _ := json.Marshal(nonNil(drift))
	a, _ := json.Marshal(nonNil(actions))
	if err := l.Store.FinishReconcileRun(ctx, db.FinishReconcileRunParams{ID: run.ID, Drift: d, Actions: a, Status: status}); err != nil {
		return status, err
	}
	if status != "clean" {
		slog.WarnContext(ctx, "reconcile", "tenant", tenantID, "status", status, "drift", drift)
	}
	return status, rerr
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
