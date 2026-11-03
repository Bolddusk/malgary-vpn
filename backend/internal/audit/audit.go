// Package audit writes append-only audit records inside the caller's transaction (FR-026).
package audit

import (
	"context"
	"encoding/json"
	"net/netip"

	"github.com/google/uuid"

	"malgary/backend/internal/store/db"
)

// Actor identifies who performed an action.
type Actor struct {
	Type     db.ActorType
	ID       string
	TenantID *uuid.UUID // set for tenant admins
}

// System is the actor for automated actions (reconciler, sampler).
var System = Actor{Type: db.ActorTypeSystem, ID: "malgaryd"}

type ctxKey struct{}

// WithActor stores the actor and request metadata on the context.
func WithActor(ctx context.Context, a Actor, requestID string, sourceIP string) context.Context {
	return context.WithValue(ctx, ctxKey{}, meta{actor: a, requestID: requestID, sourceIP: sourceIP})
}

// ActorFrom returns the actor on the context, or System.
func ActorFrom(ctx context.Context) Actor {
	if m, ok := ctx.Value(ctxKey{}).(meta); ok {
		return m.actor
	}
	return System
}

type meta struct {
	actor     Actor
	requestID string
	sourceIP  string
}

// Entry describes one change.
type Entry struct {
	TenantID   *uuid.UUID
	Action     string // e.g. tenant.created, routing_rule.updated, payment.recorded
	TargetType string
	TargetID   string
	Before     any
	After      any
}

// Record writes e with q, which must be bound to the transaction that makes the change.
func Record(ctx context.Context, q *db.Queries, e Entry) error {
	m, _ := ctx.Value(ctxKey{}).(meta)
	if m.actor.ID == "" {
		m.actor = System
	}
	before, err := marshal(e.Before)
	if err != nil {
		return err
	}
	after, err := marshal(e.After)
	if err != nil {
		return err
	}
	var ip *netip.Addr
	if a, err := netip.ParseAddr(m.sourceIP); err == nil {
		ip = &a
	}
	var reqID *string
	if m.requestID != "" {
		reqID = &m.requestID
	}
	return q.InsertAudit(ctx, db.InsertAuditParams{
		ID:         uuid.Must(uuid.NewV7()),
		ActorType:  m.actor.Type,
		ActorID:    m.actor.ID,
		TenantID:   e.TenantID,
		Action:     e.Action,
		TargetType: e.TargetType,
		TargetID:   e.TargetID,
		Before:     before,
		After:      after,
		RequestID:  reqID,
		SourceIp:   ip,
	})
}

func marshal(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	return json.Marshal(v)
}
