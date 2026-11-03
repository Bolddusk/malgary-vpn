package tenancy

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/netip"
	"slices"
	"strings"

	"github.com/google/uuid"

	"malgary/backend/internal/audit"
	"malgary/backend/internal/httpapi/gen"
	"malgary/backend/internal/store"
	"malgary/backend/internal/store/db"
)

// applyDiff updates an existing tenant to match its definition, touching only what changed
// (T076, FR-015). Nothing changed means no writes and no audit records.
func (s *Service) applyDiff(ctx context.Context, t db.Tenant, def gen.TenantDefinition) error {
	var identityChanged, routingChanged, gatewaysAdded, postureChanged bool
	err := s.Store.WithTx(ctx, func(q *db.Queries, _ pgxTx) error {
		// Identity.
		before, err := q.GetIdentityLink(ctx, t.ID)
		if err != nil {
			return err
		}
		if !identityUnchanged(before, def.Spec.Identity) {
			if err := upsertIdentity(ctx, q, t.ID, def.Spec.Identity, before.ZitadelOrgID, before.ZitadelAppClientID); err != nil {
				return err
			}
			if err := audit.Record(ctx, q, audit.Entry{TenantID: &t.ID, Action: "tenant.identity_updated", TargetType: "tenant", TargetID: t.ID.String(), Before: before, After: def.Spec.Identity}); err != nil {
				return err
			}
			identityChanged = true
		}

		// Subscription (FR-016a/b).
		if s.Billing != nil {
			if err := s.diffSubscription(ctx, q, t, def); err != nil {
				return err
			}
		}

		// Routing default and call exclusions.
		changed, err := routingDiffers(ctx, q, t.ID, def)
		if err != nil {
			return err
		}
		if changed {
			if err := storeRouting(ctx, q, t.ID, def); err != nil {
				return err
			}
			if err := audit.Record(ctx, q, audit.Entry{TenantID: &t.ID, Action: "routing_rule.updated", TargetType: "tenant", TargetID: t.ID.String(), After: def.Spec.Routing}); err != nil {
				return err
			}
			routingChanged = true
		}

		// Posture (tenant-wide rule from the definition).
		cur, err := q.ListPostureRules(ctx, t.ID)
		if err != nil {
			return err
		}
		if !samePosture(cur, postureFromSpec(def.Spec.Posture)) {
			if err := storePosture(ctx, q, t.ID, postureFromSpec(def.Spec.Posture)); err != nil {
				return err
			}
			if err := audit.Record(ctx, q, audit.Entry{TenantID: &t.ID, Action: "posture_rule.updated", TargetType: "tenant", TargetID: t.ID.String(), After: def.Spec.Posture}); err != nil {
				return err
			}
			postureChanged = true
		}

		// Gateway pairs: additions only (removal moves egress IPs and needs customer notice,
		// Principle III, so it is a runbook step, not an apply).
		peers, err := q.ListRoutingPeersByTenant(ctx, t.ID)
		if err != nil {
			return err
		}
		have := map[uuid.UUID]bool{}
		for _, p := range peers {
			have[p.GatewayPairID] = true
		}
		for _, g := range def.Spec.Gateways {
			pair, err := q.GetGatewayPairByName(ctx, g.Pair)
			if err == nil && have[pair.ID] {
				continue
			}
			if err := s.placeRoutingPeers(ctx, q, t, g.Pair); err != nil {
				return err
			}
			if err := audit.Record(ctx, q, audit.Entry{TenantID: &t.ID, Action: "tenant.gateway_added", TargetType: "tenant", TargetID: t.ID.String(), After: g}); err != nil {
				return err
			}
			gatewaysAdded = true
		}
		return nil
	})
	if err != nil {
		return err
	}
	if identityChanged {
		if err := s.SyncSignIn(ctx, t.ID); err != nil {
			return err
		}
	}
	if (routingChanged || gatewaysAdded) && t.Status == db.TenantStatusActive {
		go s.refreshNetwork(context.WithoutCancel(ctx), t.ID, gatewaysAdded)
	}
	if postureChanged && t.Status == db.TenantStatusActive {
		go func() {
			if err := s.applyAccess(context.WithoutCancel(ctx), t); err != nil {
				slog.ErrorContext(ctx, "posture push failed", "tenant", t.Slug, "err", err)
			}
		}()
	}
	return nil
}

func (s *Service) diffSubscription(ctx context.Context, q *db.Queries, t db.Tenant, def gen.TenantDefinition) error {
	want := def.Spec.Subscription
	cur, err := q.GetOpenSubscription(ctx, t.ID)
	if store.IsNotFound(err) {
		provider := want.PaymentProvider
		return s.Billing.SubscribeTx(ctx, q, t.ID, gen.SubscriptionInput{PlanCode: want.Plan, PlanVersion: want.PlanVersion, Quantity: want.Quantity, PaymentProvider: provider})
	}
	if err != nil {
		return err
	}
	v, err := q.GetPlanVersionByID(ctx, cur.PlanVersionID)
	if err != nil {
		return err
	}
	p, err := q.GetPlan(ctx, v.PlanID)
	if err != nil {
		return err
	}
	if p.Code == want.Plan && int(v.Version) == want.PlanVersion && int(cur.Quantity) == want.Quantity {
		return nil
	}
	ch := gen.SubscriptionChange{Reason: "tenant definition applied"}
	if int(cur.Quantity) != want.Quantity {
		ch.Quantity = &want.Quantity
	}
	if p.Code != want.Plan || int(v.Version) != want.PlanVersion {
		ch.PlanCode, ch.PlanVersion = &want.Plan, &want.PlanVersion
	}
	return s.Billing.ChangeTx(ctx, q, t.ID, ch)
}

func routingDiffers(ctx context.Context, q *db.Queries, tenantID uuid.UUID, def gen.TenantDefinition) (bool, error) {
	rules, err := q.ListRoutingRules(ctx, tenantID)
	if err != nil {
		return false, err
	}
	var tenantRule *db.RoutingRule
	for i := range rules {
		if rules[i].Scope == db.RuleScopeTenant {
			tenantRule = &rules[i]
		}
	}
	d := def.Spec.Routing.Default
	if tenantRule == nil || string(tenantRule.Mode) != string(d.Mode) || !sameSet(tenantRule.Destinations, deref(d.Destinations)) {
		return true, nil
	}
	cur, err := q.ListCallExclusions(ctx, tenantID)
	if err != nil {
		return false, err
	}
	var want []string
	for _, c := range deref(def.Spec.Routing.CallExclusions) {
		if p, err := netip.ParsePrefix(strings.TrimSpace(c)); err == nil {
			want = append(want, p.Masked().String())
		} else {
			want = append(want, c) // invalid: forces storeRouting to report it
		}
	}
	return !sameSet(cur, want), nil
}

// refreshNetwork redeploys routing peers (when pairs were added) and re-applies routes.
func (s *Service) refreshNetwork(ctx context.Context, tenantID uuid.UUID, redeploy bool) {
	t, err := s.Store.GetTenant(ctx, tenantID)
	if err != nil {
		return
	}
	stack, err := s.Store.GetTenantStack(ctx, tenantID)
	if err != nil {
		return
	}
	if redeploy {
		if err := s.deploy(ctx, t, stack, true); err != nil {
			slog.ErrorContext(ctx, "redeploy after gateway change failed", "tenant", t.Slug, "err", err)
			return
		}
	}
	if err := s.applyRouting(ctx, t, stack.ManagementUrl); err != nil {
		slog.ErrorContext(ctx, "routing refresh failed", "tenant", t.Slug, "err", err)
	}
}

func sameSet(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(slices.Compact(a), slices.Compact(b))
}

func samePosture(cur []db.ListPostureRulesRow, want []gen.PostureRule) bool {
	if len(cur) != len(want) {
		return false
	}
	for i, w := range want {
		c := cur[i]
		if c.AppliesToGroupID != nil || w.GroupId != nil {
			return false
		}
		if fmt.Sprint(deref1(c.MinClientVersion)) != fmt.Sprint(deref1(w.MinClientVersion)) {
			return false
		}
		var os map[string]string
		_ = json.Unmarshal(c.MinOs, &os)
		wo := map[string]string{}
		if w.MinOs != nil {
			for k, v := range *w.MinOs {
				wo[strings.ToLower(k)] = v
			}
		}
		if fmt.Sprint(os) != fmt.Sprint(wo) {
			return false
		}
	}
	return true
}

func deref1(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
