package tenancy

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"malgary/backend/internal/audit"
	"malgary/backend/internal/httpapi"
	"malgary/backend/internal/httpapi/gen"
	"malgary/backend/internal/policy"
	"malgary/backend/internal/store/db"
)

// GetPostureRules returns the tenant's posture rules (FR-018).
func (s *Service) GetPostureRules(ctx context.Context, tenantID uuid.UUID) ([]gen.PostureRule, error) {
	rows, err := s.Store.ListPostureRules(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	out := make([]gen.PostureRule, 0, len(rows))
	for _, r := range rows {
		var os map[string]string
		_ = json.Unmarshal(r.MinOs, &os)
		id := r.ID
		out = append(out, gen.PostureRule{Id: &id, GroupId: r.AppliesToGroupID, MinClientVersion: r.MinClientVersion, MinOs: &os})
	}
	return out, nil
}

// PutPostureRules replaces posture rules. Needs the posture_checks entitlement (Principle XI).
func (s *Service) PutPostureRules(ctx context.Context, tenantID uuid.UUID, in []gen.PostureRule) ([]gen.PostureRule, error) {
	t, err := s.Store.GetTenant(ctx, tenantID)
	if err != nil {
		return nil, notFoundErr(err)
	}
	if s.Billing != nil && len(in) > 0 {
		if err := s.Billing.Require(ctx, tenantID, "posture_checks"); err != nil {
			return nil, err
		}
	}
	err = s.Store.WithTx(ctx, func(q *db.Queries, _ pgxTx) error {
		before, _ := q.ListPostureRules(ctx, tenantID)
		if err := storePosture(ctx, q, tenantID, in); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{TenantID: &tenantID, Action: "posture_rule.updated", TargetType: "tenant", TargetID: tenantID.String(), Before: before, After: in})
	})
	if err != nil {
		return nil, err
	}
	if t.Status == db.TenantStatusActive {
		go func() {
			ctx := context.WithoutCancel(ctx)
			if err := s.applyAccess(ctx, t); err != nil {
				slog.ErrorContext(ctx, "posture push failed; the reconciler retries", "tenant", t.Slug, "err", err)
			}
		}()
	}
	return s.GetPostureRules(ctx, tenantID)
}

func storePosture(ctx context.Context, q *db.Queries, tenantID uuid.UUID, in []gen.PostureRule) error {
	if err := q.DeletePostureRules(ctx, tenantID); err != nil {
		return err
	}
	for _, r := range in {
		if r.GroupId != nil {
			g, err := q.GetGroup(ctx, db.GetGroupParams{TenantID: tenantID, ID: *r.GroupId})
			if err != nil || g.Kind != db.GroupKindTeam {
				return httpapi.Errf(http.StatusUnprocessableEntity, gen.InvalidRequest, "groupId is not a team group of this tenant")
			}
		}
		os := map[string]string{}
		if r.MinOs != nil {
			for k, v := range *r.MinOs {
				k = strings.ToLower(k)
				if k != "windows" && k != "macos" && k != "linux" && k != "ios" && k != "android" {
					return httpapi.Errf(http.StatusUnprocessableEntity, gen.InvalidRequest, "minOs keys are windows, macos, linux, ios, android")
				}
				os[k] = v
			}
		}
		b, _ := json.Marshal(os)
		if err := q.InsertPostureRule(ctx, db.InsertPostureRuleParams{ID: uuid.Must(uuid.NewV7()), TenantID: tenantID, MinOs: b, MinClientVersion: r.MinClientVersion, AppliesToGroupID: r.GroupId}); err != nil {
			return err
		}
	}
	return nil
}

// postureFromSpec converts the definition's posture section to rules.
func postureFromSpec(p *gen.PostureSpec) []gen.PostureRule {
	if p == nil || (p.MinClientVersion == nil && p.MinOs == nil) {
		return nil
	}
	return []gen.PostureRule{{MinClientVersion: p.MinClientVersion, MinOs: p.MinOs}}
}

// applyAccess pushes access policies with posture checks to the tenant's stack.
func (s *Service) applyAccess(ctx context.Context, t db.Tenant) error {
	stack, err := s.Store.GetTenantStack(ctx, t.ID)
	if err != nil {
		return err
	}
	rows, err := s.Store.ListPostureRules(ctx, t.ID)
	if err != nil {
		return err
	}
	var rules []policy.Posture
	for _, r := range rows {
		var os map[string]string
		_ = json.Unmarshal(r.MinOs, &os)
		p := policy.Posture{MinOS: os}
		if r.MinClientVersion != nil {
			p.MinClientVersion = *r.MinClientVersion
		}
		if r.GroupName != nil {
			p.Group = *r.GroupName
		}
		rules = append(rules, p)
	}
	pat, err := s.Secrets.Resolve(refNetbirdPAT(t.Slug))
	if err != nil {
		return err
	}
	c, err := s.NetBird.For(ctx, stack.ManagementUrl, strings.TrimSpace(string(pat)))
	if err != nil {
		return err
	}
	enabled, err := s.accessAllowed(ctx, t.ID, stack.DesiredHash)
	if err != nil {
		return err
	}
	return policy.ApplyAccess(ctx, c, rules, groupGateways, groupAllUsers, enabled)
}

// accessAllowed is the interim isolation gate (feature 002, FR-009): when IsolationRequired is
// set, users reach the gateway only once an isolation test passed for the stack's current hash.
func (s *Service) accessAllowed(ctx context.Context, tenantID uuid.UUID, hash *string) (bool, error) {
	if !s.IsolationRequired {
		return true, nil
	}
	if hash == nil {
		return false, nil
	}
	return s.Store.IsolationPassedForHash(ctx, db.IsolationPassedForHashParams{TenantID: tenantID, StackHash: *hash})
}
