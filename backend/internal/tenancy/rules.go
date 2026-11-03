package tenancy

import (
	"context"
	"log/slog"
	"net/http"
	"net/netip"
	"strings"

	"github.com/google/uuid"

	"malgary/backend/internal/audit"
	"malgary/backend/internal/httpapi"
	"malgary/backend/internal/httpapi/gen"
	"malgary/backend/internal/policy"
	"malgary/backend/internal/routing"
	"malgary/backend/internal/store"
	"malgary/backend/internal/store/db"
)

// GetRoutingRules returns rules, call exclusions and each user's effective rule (FR-005).
func (s *Service) GetRoutingRules(ctx context.Context, tenantID uuid.UUID) (gen.RoutingRules, error) {
	if _, err := s.Store.GetTenant(ctx, tenantID); err != nil {
		return gen.RoutingRules{}, notFoundErr(err)
	}
	rows, err := s.Store.ListRoutingRules(ctx, tenantID)
	if err != nil {
		return gen.RoutingRules{}, err
	}
	out := gen.RoutingRules{Rules: []gen.RoutingRule{}, Effective: []struct {
		Conflict     *bool                            `json:"conflict,omitempty"`
		Destinations *[]string                        `json:"destinations,omitempty"`
		Mode         gen.RoutingRulesEffectiveMode    `json:"mode"`
		Source       *gen.RoutingRulesEffectiveSource `json:"source,omitempty"`
		UserId       uuid.UUID                        `json:"userId"`
	}{}}
	for _, r := range rows {
		d := r.Destinations
		out.Rules = append(out.Rules, gen.RoutingRule{Scope: gen.RoutingRuleScope(r.Scope), ScopeRef: r.ScopeRef, Mode: gen.RoutingRuleMode(r.Mode), Destinations: &d})
	}
	ex, err := s.Store.ListCallExclusions(ctx, tenantID)
	if err != nil {
		return gen.RoutingRules{}, err
	}
	out.CallExclusions = &ex
	eff, err := routing.Effective(ctx, s.Store.Queries, tenantID)
	if err != nil {
		return gen.RoutingRules{}, err
	}
	for _, e := range eff {
		if e.Source == "none" {
			continue
		}
		d, c, src := e.Destinations, e.Conflict, gen.RoutingRulesEffectiveSource(e.Source)
		out.Effective = append(out.Effective, struct {
			Conflict     *bool                            `json:"conflict,omitempty"`
			Destinations *[]string                        `json:"destinations,omitempty"`
			Mode         gen.RoutingRulesEffectiveMode    `json:"mode"`
			Source       *gen.RoutingRulesEffectiveSource `json:"source,omitempty"`
			UserId       uuid.UUID                        `json:"userId"`
		}{Conflict: &c, Destinations: &d, Mode: gen.RoutingRulesEffectiveMode(e.Mode), Source: &src, UserId: e.UserID})
	}
	return out, nil
}

// PutRoutingRules replaces all routing rules (and optionally call exclusions), recomputes every
// user's routing group and pushes the result to the stack in the background (FR-005, FR-005a).
func (s *Service) PutRoutingRules(ctx context.Context, tenantID uuid.UUID, rules []gen.RoutingRule, exclusions *[]string) (gen.RoutingRules, error) {
	t, err := s.Store.GetTenant(ctx, tenantID)
	if err != nil {
		return gen.RoutingRules{}, notFoundErr(err)
	}
	tenantRules := 0
	for _, r := range rules {
		pr := policy.Rule{Mode: policy.Mode(r.Mode), Destinations: deref(r.Destinations)}
		if !policy.Validate(pr) {
			return gen.RoutingRules{}, httpapi.Errf(http.StatusUnprocessableEntity, gen.RoutingRuleEmptyDestinations,
				"a rule with mode destinations needs at least one destination")
		}
		for _, d := range pr.Destinations {
			if _, err := netip.ParsePrefix(strings.TrimSpace(d)); err != nil {
				return gen.RoutingRules{}, httpapi.Errf(http.StatusUnprocessableEntity, gen.InvalidRequest, "destination "+d+" is not an IPv4 CIDR")
			}
		}
		if r.Scope == gen.RoutingRuleScopeTenant {
			tenantRules++
		}
		if (r.Scope == gen.RoutingRuleScopeTenant) != (r.ScopeRef == nil) {
			return gen.RoutingRules{}, httpapi.Errf(http.StatusUnprocessableEntity, gen.InvalidRequest, "tenant rules have no scopeRef; group and user rules need one")
		}
	}
	if tenantRules != 1 {
		return gen.RoutingRules{}, httpapi.Errf(http.StatusUnprocessableEntity, gen.InvalidRequest, "exactly one tenant-wide default rule is required")
	}
	err = s.Store.WithTx(ctx, func(q *db.Queries, _ pgxTx) error {
		before, err := q.ListRoutingRules(ctx, tenantID)
		if err != nil {
			return err
		}
		if err := q.DeleteRoutingRules(ctx, tenantID); err != nil {
			return err
		}
		for _, r := range rules {
			if r.ScopeRef != nil && !s.ownsRef(ctx, q, tenantID, r) {
				return httpapi.Errf(http.StatusUnprocessableEntity, gen.InvalidRequest, "scopeRef "+r.ScopeRef.String()+" is not a group or user of this tenant")
			}
			if _, err := q.InsertRoutingRule(ctx, db.InsertRoutingRuleParams{
				ID: uuid.Must(uuid.NewV7()), TenantID: tenantID, Scope: db.RuleScope(r.Scope), ScopeRef: r.ScopeRef,
				Mode: db.RoutingMode(r.Mode), Destinations: normalizeCIDRs(deref(r.Destinations)),
			}); err != nil {
				return err
			}
		}
		if exclusions != nil {
			if err := q.ReplaceCallExclusionsDelete(ctx, tenantID); err != nil {
				return err
			}
			for _, c := range *exclusions {
				p, err := netip.ParsePrefix(strings.TrimSpace(c))
				if err != nil {
					return httpapi.Errf(http.StatusUnprocessableEntity, gen.InvalidRequest, "call exclusion "+c+" is not an IPv4 CIDR")
				}
				if err := q.AddCallExclusion(ctx, db.AddCallExclusionParams{TenantID: tenantID, Cidr: p.Masked()}); err != nil {
					return err
				}
			}
		}
		if _, err := routing.Recompute(ctx, q, tenantID); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{TenantID: &tenantID, Action: "routing_rule.updated", TargetType: "tenant", TargetID: tenantID.String(),
			Before: before, After: map[string]any{"rules": rules, "callExclusions": exclusions}})
	})
	if err != nil {
		return gen.RoutingRules{}, err
	}
	if t.Status == db.TenantStatusActive {
		go func() {
			ctx := context.WithoutCancel(ctx)
			stack, err := s.Store.GetTenantStack(ctx, tenantID)
			if err == nil {
				err = s.applyRouting(ctx, t, stack.ManagementUrl)
			}
			if err != nil {
				slog.ErrorContext(ctx, "routing push failed; the reconciler retries", "tenant", t.Slug, "err", err)
			}
		}()
	}
	return s.GetRoutingRules(ctx, tenantID)
}

func (s *Service) ownsRef(ctx context.Context, q *db.Queries, tenantID uuid.UUID, r gen.RoutingRule) bool {
	switch r.Scope {
	case gen.RoutingRuleScopeGroup:
		g, err := q.GetGroup(ctx, db.GetGroupParams{TenantID: tenantID, ID: *r.ScopeRef})
		return err == nil && g.Kind == db.GroupKindTeam
	case gen.RoutingRuleScopeUser:
		_, err := q.GetTenantUser(ctx, db.GetTenantUserParams{TenantID: tenantID, ID: *r.ScopeRef})
		return err == nil
	}
	return false
}

func normalizeCIDRs(in []string) []string {
	out := make([]string, 0, len(in))
	for _, d := range in {
		if p, err := netip.ParsePrefix(strings.TrimSpace(d)); err == nil {
			out = append(out, p.Masked().String())
		}
	}
	return out
}

func notFoundErr(err error) error {
	if store.IsNotFound(err) {
		return httpapi.ErrNotFound
	}
	return err
}
