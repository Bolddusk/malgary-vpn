// Package routing keeps each user's routing-group membership in line with the tenant's
// routing rules (FR-005) and pushes the result to the tenant's NetBird stack.
package routing

import (
	"context"
	"fmt"
	"net/netip"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/netbirdio/netbird/shared/management/client/rest"
	nbapi "github.com/netbirdio/netbird/shared/management/http/api"

	"malgary/backend/internal/policy"
	"malgary/backend/internal/store/db"
)

// Rules loads the tenant's rules in policy form.
func Rules(ctx context.Context, q *db.Queries, tenantID uuid.UUID) ([]policy.Rule, error) {
	rows, err := q.ListRoutingRules(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	out := make([]policy.Rule, 0, len(rows))
	for _, r := range rows {
		pr := policy.Rule{Scope: string(r.Scope), Mode: policy.Mode(r.Mode), Destinations: r.Destinations}
		if r.ScopeRef != nil {
			pr.ScopeRef = *r.ScopeRef
		}
		out = append(out, pr)
	}
	return out, nil
}

// UserEffective is one user's resolved rule.
type UserEffective struct {
	UserID uuid.UUID
	Email  string
	policy.Effective
}

// Effective resolves every user in the tenant.
func Effective(ctx context.Context, q *db.Queries, tenantID uuid.UUID) ([]UserEffective, error) {
	rules, err := Rules(ctx, q, tenantID)
	if err != nil {
		return nil, err
	}
	users, err := q.ListTenantUsers(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	teams, err := teamGroupsByUser(ctx, q, tenantID)
	if err != nil {
		return nil, err
	}
	out := make([]UserEffective, 0, len(users))
	for _, u := range users {
		out = append(out, UserEffective{UserID: u.ID, Email: u.Email, Effective: policy.Resolve(u.ID, teams[u.ID], rules)})
	}
	return out, nil
}

// Recompute puts every user in exactly one routing group (route:full or route:dest:<hash>)
// and removes routing groups nobody uses. It returns the distinct routing groups in use.
func Recompute(ctx context.Context, q *db.Queries, tenantID uuid.UUID) ([]policy.RoutingGroup, error) {
	eff, err := Effective(ctx, q, tenantID)
	if err != nil {
		return nil, err
	}
	current, err := q.ListRouteGroupMemberships(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	have := map[uuid.UUID][]db.ListRouteGroupMembershipsRow{}
	for _, m := range current {
		have[m.TenantUserID] = append(have[m.TenantUserID], m)
	}
	groups := map[string]policy.RoutingGroup{}
	for _, e := range eff {
		want := e.Group()
		ok := false
		for _, m := range have[e.UserID] {
			if m.Name == want {
				ok = true
				continue
			}
			if err := q.RemoveGroupMember(ctx, db.RemoveGroupMemberParams{GroupID: m.GroupID, TenantUserID: e.UserID}); err != nil {
				return nil, err
			}
		}
		if want == "" {
			continue
		}
		if !ok {
			g, err := q.CreateGroup(ctx, db.CreateGroupParams{ID: uuid.Must(uuid.NewV7()), TenantID: tenantID, Name: want, Kind: db.GroupKindSystem})
			if err != nil {
				return nil, err
			}
			if err := q.AddGroupMember(ctx, db.AddGroupMemberParams{GroupID: g.ID, TenantUserID: e.UserID}); err != nil {
				return nil, err
			}
		}
		if _, seen := groups[want]; !seen {
			rg := policy.RoutingGroup{Name: want, FullTunnel: e.Mode == policy.FullTunnel}
			for _, d := range e.Destinations {
				p, err := netip.ParsePrefix(d)
				if err != nil {
					return nil, fmt.Errorf("destination %q: %w", d, err)
				}
				rg.Destinations = append(rg.Destinations, p.Masked())
			}
			groups[want] = rg
		}
	}
	if err := q.DeleteEmptyRouteGroups(ctx, tenantID); err != nil {
		return nil, err
	}
	out := make([]policy.RoutingGroup, 0, len(groups))
	for _, g := range groups {
		out = append(out, g)
	}
	slices.SortFunc(out, func(a, b policy.RoutingGroup) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

// PushUserGroups sets each NetBird user's auto-groups to their Malgary groups, so routing
// changes reach devices within minutes (FR-005a) instead of waiting for the next sign-in's
// JWT group sync.
func PushUserGroups(ctx context.Context, c *rest.Client, q *db.Queries, tenantID uuid.UUID) error {
	nbUsers, err := c.Users.List(ctx)
	if err != nil {
		return fmt.Errorf("list netbird users: %w", err)
	}
	byEmail := map[string]nbapi.User{}
	for _, u := range nbUsers {
		byEmail[strings.ToLower(u.Email)] = u
	}
	members, err := q.ListTenantMemberships(ctx, tenantID)
	if err != nil {
		return err
	}
	users, err := q.ListTenantUsers(ctx, tenantID)
	if err != nil {
		return err
	}
	names := map[uuid.UUID][]string{}
	for _, m := range members {
		names[m.TenantUserID] = append(names[m.TenantUserID], m.Name)
	}
	ids := map[string]string{}
	for _, u := range users {
		nb, ok := byEmail[strings.ToLower(u.Email)]
		if !ok {
			continue // not signed in yet; JWT group sync covers the first login
		}
		var want []string
		for _, n := range names[u.ID] {
			id, ok := ids[n]
			if !ok {
				if id, err = groupID(ctx, c, n); err != nil {
					return err
				}
				ids[n] = id
			}
			want = append(want, id)
		}
		slices.Sort(want)
		cur := slices.Clone(nb.AutoGroups)
		slices.Sort(cur)
		if slices.Equal(cur, want) {
			continue
		}
		blocked := u.Status == db.TenantUserStatusDisabled || u.Status == db.TenantUserStatusRemoved
		if _, err := c.Users.Update(ctx, nb.Id, nbapi.UserRequest{AutoGroups: want, Role: nb.Role, IsBlocked: blocked}); err != nil {
			return fmt.Errorf("update netbird user %s: %w", u.Email, err)
		}
	}
	return nil
}

func teamGroupsByUser(ctx context.Context, q *db.Queries, tenantID uuid.UUID) (map[uuid.UUID][]uuid.UUID, error) {
	groups, err := q.ListGroups(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	byName := map[string]uuid.UUID{}
	for _, g := range groups {
		if g.Kind == db.GroupKindTeam || g.Name == "all-users" {
			byName[g.Name] = g.ID
		}
	}
	members, err := q.ListTenantMemberships(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	out := map[uuid.UUID][]uuid.UUID{}
	for _, m := range members {
		if id, ok := byName[m.Name]; ok {
			out[m.TenantUserID] = append(out[m.TenantUserID], id)
		}
	}
	return out, nil
}

func groupID(ctx context.Context, c *rest.Client, name string) (string, error) {
	if g, err := c.Groups.GetByName(ctx, name); err == nil && g != nil {
		return g.Id, nil
	}
	g, err := c.Groups.Create(ctx, nbapi.GroupRequest{Name: name})
	if err != nil {
		return "", fmt.Errorf("group %s: %w", name, err)
	}
	return g.Id, nil
}
