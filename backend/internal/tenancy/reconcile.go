package tenancy

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	nbapi "github.com/netbirdio/netbird/shared/management/http/api"

	"malgary/backend/internal/store/db"
)

// Reconcile checks one tenant's stack against the backend and repairs what it safely can:
// stack spec drift (redeploy), routes, users' groups and access policies (re-apply). It
// implements reconcile.Repairer.
func (s *Service) Reconcile(ctx context.Context, tenantID uuid.UUID) ([]string, []string, error) {
	t, err := s.Store.GetTenant(ctx, tenantID)
	if err != nil {
		return nil, nil, err
	}
	stack, err := s.Store.GetTenantStack(ctx, tenantID)
	if err != nil {
		return nil, nil, err
	}
	var drift, actions []string

	// 1. Stack spec: rendered hash vs applied hash.
	spec, err := s.render(ctx, t, stack, true)
	if err != nil {
		return nil, nil, err
	}
	sealed, _, err := spec.Seal()
	if err != nil {
		return nil, nil, err
	}
	if stack.AppliedHash == nil || *stack.AppliedHash != sealed.Hash || stack.Status != db.StackStatusApplied {
		drift = append(drift, "stack spec not applied")
		if err := s.deploy(ctx, t, stack, true); err != nil {
			return drift, actions, fmt.Errorf("redeploy: %w", err)
		}
		actions = append(actions, "redeployed stack")
	}

	// 2. Users who exist in NetBird but not in Malgary (added in the dashboard by hand) are
	//    reported, not deleted: an operator decides.
	pat, err := s.Secrets.Resolve(refNetbirdPAT(t.Slug))
	if err != nil {
		return drift, actions, err
	}
	c, err := s.NetBird.For(ctx, stack.ManagementUrl, strings.TrimSpace(string(pat)))
	if err != nil {
		return drift, actions, err
	}
	nbUsers, err := c.Users.List(ctx)
	if err != nil {
		return drift, actions, err
	}
	known := map[string]bool{}
	users, err := s.Store.ListTenantUsers(ctx, tenantID)
	if err != nil {
		return drift, actions, err
	}
	for _, u := range users {
		known[strings.ToLower(u.Email)] = true
	}
	for _, u := range nbUsers {
		if isServiceOrOwner(u) || known[strings.ToLower(u.Email)] {
			continue
		}
		drift = append(drift, "user in NetBird but not in Malgary: "+u.Email)
	}

	// 3. Routes, user groups and access policies: always re-applied (idempotent, cheap).
	if err := s.applyRouting(ctx, t, stack.ManagementUrl); err != nil {
		return drift, actions, fmt.Errorf("routing: %w", err)
	}
	if err := s.applyAccess(ctx, t); err != nil {
		return drift, actions, fmt.Errorf("access: %w", err)
	}
	return drift, actions, nil
}

func isServiceOrOwner(u nbapi.User) bool {
	return (u.IsServiceUser != nil && *u.IsServiceUser) || strings.HasSuffix(u.Email, ".break-glass.malgary.invalid")
}
