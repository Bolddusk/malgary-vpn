package users

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"malgary/backend/internal/audit"
	"malgary/backend/internal/httpapi"
	"malgary/backend/internal/httpapi/gen"
	"malgary/backend/internal/routing"
	"malgary/backend/internal/store"
	"malgary/backend/internal/store/db"
)

// ListGroups lists the tenant's groups with member counts.
func (s *Service) ListGroups(ctx context.Context, tenantID uuid.UUID) ([]gen.Group, error) {
	gs, err := s.Store.ListGroups(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	out := make([]gen.Group, 0, len(gs))
	for _, g := range gs {
		n, err := s.Store.CountGroupMembers(ctx, g.ID)
		if err != nil {
			return nil, err
		}
		m := int(n)
		out = append(out, gen.Group{Id: g.ID, Name: g.Name, Kind: gen.GroupKind(g.Kind), Members: &m})
	}
	return out, nil
}

// CreateGroup creates a team group, optionally with members.
func (s *Service) CreateGroup(ctx context.Context, tenantID uuid.UUID, in gen.GroupInput) (gen.Group, error) {
	if in.Name == nil || strings.TrimSpace(*in.Name) == "" {
		return gen.Group{}, httpapi.Errf(http.StatusUnprocessableEntity, gen.InvalidRequest, "name is required")
	}
	name := strings.TrimSpace(*in.Name)
	if reserved(name) {
		return gen.Group{}, httpapi.Errf(http.StatusUnprocessableEntity, gen.InvalidRequest, "group names starting with route: and the name all-users are reserved")
	}
	var g db.Group
	err := s.Store.WithTx(ctx, func(q *db.Queries, _ pgxTx) error {
		if _, err := q.GetGroupByName(ctx, db.GetGroupByNameParams{TenantID: tenantID, Name: name}); err == nil {
			return httpapi.Errf(http.StatusConflict, gen.InvalidRequest, "group "+name+" already exists")
		}
		var err error
		g, err = q.CreateGroup(ctx, db.CreateGroupParams{ID: uuid.Must(uuid.NewV7()), TenantID: tenantID, Name: name, Kind: db.GroupKindTeam})
		if err != nil {
			return err
		}
		if err := s.setMembers(ctx, q, tenantID, g.ID, in.MemberEmails); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{TenantID: &tenantID, Action: "group.created", TargetType: "group", TargetID: g.ID.String(), After: in})
	})
	if err != nil {
		return gen.Group{}, err
	}
	return s.groupView(ctx, g)
}

// PatchGroup renames a team group and/or replaces its members.
func (s *Service) PatchGroup(ctx context.Context, tenantID, groupID uuid.UUID, in gen.GroupInput) (gen.Group, error) {
	var g db.Group
	err := s.Store.WithTx(ctx, func(q *db.Queries, _ pgxTx) error {
		cur, err := q.GetGroup(ctx, db.GetGroupParams{TenantID: tenantID, ID: groupID})
		if store.IsNotFound(err) || (err == nil && cur.Kind != db.GroupKindTeam) {
			return httpapi.ErrNotFound
		}
		if err != nil {
			return err
		}
		g = cur
		if in.Name != nil && strings.TrimSpace(*in.Name) != cur.Name {
			if reserved(*in.Name) {
				return httpapi.Errf(http.StatusUnprocessableEntity, gen.InvalidRequest, "reserved group name")
			}
			if g, err = q.RenameGroup(ctx, db.RenameGroupParams{TenantID: tenantID, ID: groupID, Name: strings.TrimSpace(*in.Name)}); err != nil {
				return err
			}
		}
		if in.MemberEmails != nil {
			if err := q.ClearGroupMembers(ctx, groupID); err != nil {
				return err
			}
			if err := s.setMembers(ctx, q, tenantID, groupID, in.MemberEmails); err != nil {
				return err
			}
		}
		if _, err := routing.Recompute(ctx, q, tenantID); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{TenantID: &tenantID, Action: "group.updated", TargetType: "group", TargetID: groupID.String(), Before: cur, After: in})
	})
	if err != nil {
		return gen.Group{}, err
	}
	return s.groupView(ctx, g)
}

// DeleteGroup deletes a team group; its routing rules disappear with it.
func (s *Service) DeleteGroup(ctx context.Context, tenantID, groupID uuid.UUID) error {
	return s.Store.WithTx(ctx, func(q *db.Queries, _ pgxTx) error {
		cur, err := q.GetGroup(ctx, db.GetGroupParams{TenantID: tenantID, ID: groupID})
		if store.IsNotFound(err) || (err == nil && cur.Kind != db.GroupKindTeam) {
			return httpapi.ErrNotFound
		}
		if err != nil {
			return err
		}
		if err := q.DeleteGroupRoutingRules(ctx, db.DeleteGroupRoutingRulesParams{TenantID: tenantID, ScopeRef: &groupID}); err != nil {
			return err
		}
		if err := q.DeleteTeamGroup(ctx, db.DeleteTeamGroupParams{TenantID: tenantID, ID: groupID}); err != nil {
			return err
		}
		if _, err := routing.Recompute(ctx, q, tenantID); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{TenantID: &tenantID, Action: "group.deleted", TargetType: "group", TargetID: groupID.String(), Before: cur})
	})
}

func (s *Service) setMembers(ctx context.Context, q *db.Queries, tenantID, groupID uuid.UUID, emails *[]openapi_types.Email) error {
	if emails == nil {
		return nil
	}
	for _, e := range *emails {
		u, err := q.GetTenantUserByEmail(ctx, db.GetTenantUserByEmailParams{TenantID: tenantID, Email: strings.ToLower(string(e))})
		if err != nil {
			return httpapi.Errf(http.StatusUnprocessableEntity, gen.InvalidRequest, string(e)+" is not a user of this tenant")
		}
		if err := q.AddGroupMember(ctx, db.AddGroupMemberParams{GroupID: groupID, TenantUserID: u.ID}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) groupView(ctx context.Context, g db.Group) (gen.Group, error) {
	n, err := s.Store.CountGroupMembers(ctx, g.ID)
	if err != nil {
		return gen.Group{}, err
	}
	m := int(n)
	return gen.Group{Id: g.ID, Name: g.Name, Kind: gen.GroupKind(g.Kind), Members: &m}, nil
}

func reserved(name string) bool {
	n := strings.TrimSpace(name)
	return n == "all-users" || n == "gateways" || strings.HasPrefix(n, "route:")
}
