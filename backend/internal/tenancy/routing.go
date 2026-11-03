package tenancy

import (
	"context"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/google/uuid"

	"malgary/backend/internal/httpapi"
	"malgary/backend/internal/httpapi/gen"
	"malgary/backend/internal/policy"
	"malgary/backend/internal/routing"
	"malgary/backend/internal/store/db"
)

// storeRouting persists the tenant default rule and call exclusions from a definition.
func storeRouting(ctx context.Context, q *db.Queries, tenantID uuid.UUID, def gen.TenantDefinition) error {
	r := def.Spec.Routing
	dests := []string{}
	if r.Default.Destinations != nil {
		dests = *r.Default.Destinations
	}
	if _, err := q.UpsertTenantRoutingRule(ctx, db.UpsertTenantRoutingRuleParams{
		ID: uuid.Must(uuid.NewV7()), TenantID: tenantID, Mode: db.RoutingMode(r.Default.Mode), Destinations: dests,
	}); err != nil {
		return err
	}
	if err := q.ReplaceCallExclusionsDelete(ctx, tenantID); err != nil {
		return err
	}
	for _, c := range deref(r.CallExclusions) {
		p, err := netip.ParsePrefix(strings.TrimSpace(c))
		if err != nil {
			return httpapi.Errf(http.StatusUnprocessableEntity, gen.InvalidRequest,
				"routing.callExclusions must be IPv4 CIDRs (got "+c+"); use the provider's published ranges")
		}
		if err := q.AddCallExclusion(ctx, db.AddCallExclusionParams{TenantID: tenantID, Cidr: p.Masked()}); err != nil {
			return err
		}
	}
	return nil
}

// applyRouting recomputes every user's routing group, then pushes routes and user groups to
// the tenant's stack once its routing peers have joined (FR-005, FR-005a).
func (s *Service) applyRouting(ctx context.Context, t db.Tenant, mgmtURL string) error {
	var groups []policy.RoutingGroup
	if err := s.Store.WithTx(ctx, func(q *db.Queries, _ pgxTx) error {
		var err error
		groups, err = routing.Recompute(ctx, q, t.ID)
		return err
	}); err != nil {
		return err
	}
	// A tenant with no users yet still gets its default route, so the first user works at once.
	if len(groups) == 0 {
		rules, err := s.Store.ListRoutingRules(ctx, t.ID)
		if err != nil {
			return err
		}
		for _, r := range rules {
			if r.Scope != db.RuleScopeTenant {
				continue
			}
			g := policy.RoutingGroup{Name: policy.FullTunnelGroup, FullTunnel: r.Mode == db.RoutingModeFullTunnel}
			if !g.FullTunnel {
				g.Name = policy.GroupForDestinations(r.Destinations)
				for _, d := range r.Destinations {
					if p, err := netip.ParsePrefix(d); err == nil {
						g.Destinations = append(g.Destinations, p.Masked())
					}
				}
			}
			groups = append(groups, g)
		}
	}
	exStr, err := s.Store.ListCallExclusions(ctx, t.ID)
	if err != nil {
		return err
	}
	var ex []netip.Prefix
	for _, e := range exStr {
		if p, err := netip.ParsePrefix(e); err == nil {
			ex = append(ex, p)
		}
	}
	peers, err := s.Store.ListRoutingPeersByTenant(ctx, t.ID)
	if err != nil {
		return err
	}
	var gws []policy.GatewayPeer
	for _, p := range peers {
		gws = append(gws, policy.GatewayPeer{Hostname: p.HostName, Metric: int(p.Metric)})
	}
	pat, err := s.Secrets.Resolve(refNetbirdPAT(t.Slug))
	if err != nil {
		return err
	}
	c, err := s.NetBird.For(ctx, mgmtURL, strings.TrimSpace(string(pat)))
	if err != nil {
		return err
	}
	// Routing peers join a few seconds after their containers start.
	var lastErr error
	for i := 0; i < 36; i++ {
		if lastErr = policy.ApplyRoutes(ctx, c, groups, ex, gws); lastErr == nil {
			return routing.PushUserGroups(ctx, c, s.Store.Queries, t.ID)
		}
		if !strings.Contains(lastErr.Error(), "has not joined") {
			return lastErr
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
	return lastErr
}
