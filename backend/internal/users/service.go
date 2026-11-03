// Package users imports and lists a tenant's users. Imported users are the only ones who can
// sign in (auto-creation is off in Malgary sign-in), which enforces the tenant's allowed
// domains and addresses (FR-003).
package users

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/google/uuid"

	"malgary/backend/internal/audit"
	"malgary/backend/internal/httpapi"
	"malgary/backend/internal/httpapi/gen"
	"malgary/backend/internal/identity"
	"malgary/backend/internal/routing"
	"malgary/backend/internal/store"
	"malgary/backend/internal/store/db"
)

// SeatChecker reports how many seats a tenant has paid for (billing, US4). Nil disables the
// check until subscriptions exist.
type SeatChecker interface {
	PaidSeats(ctx context.Context, tenantID uuid.UUID) (int, error)
}

// Service implements user import and listing.
type Service struct {
	Store    *store.Store
	Identity identity.Provider // nil in tests without sign-in
	Seats    SeatChecker
}

// Import creates users, adds them to groups and sends invitations where needed.
func (s *Service) Import(ctx context.Context, tenantID uuid.UUID, in []gen.UserImport) (gen.ImportResult, error) {
	link, err := s.Store.GetIdentityLink(ctx, tenantID)
	if store.IsNotFound(err) {
		return gen.ImportResult{}, httpapi.ErrNotFound
	}
	if err != nil {
		return gen.ImportResult{}, err
	}
	var rejected []string
	for _, u := range in {
		if !allowed(string(u.Email), link) {
			rejected = append(rejected, string(u.Email))
		}
	}
	if len(rejected) > 0 {
		return gen.ImportResult{}, httpapi.Errf(http.StatusUnprocessableEntity, gen.InvalidRequest,
			"not in the tenant's allowed domains or addresses: "+strings.Join(rejected, ", "))
	}

	var res gen.ImportResult
	var created []db.TenantUser
	err = s.Store.WithTx(ctx, func(q *db.Queries, _ pgxTx) error {
		held, err := q.CountSeatsHeld(ctx, tenantID)
		if err != nil {
			return err
		}
		baseGroups := []string{"all-users"}
		for _, u := range in {
			role := db.TenantUserRoleMember
			if u.Role != nil {
				role = db.TenantUserRole(*u.Role)
			}
			name := ""
			if u.Name != nil {
				name = *u.Name
			}
			row, err := q.CreateTenantUser(ctx, db.CreateTenantUserParams{
				ID: uuid.Must(uuid.NewV7()), TenantID: tenantID, Email: strings.ToLower(string(u.Email)), DisplayName: name, Role: role,
			})
			if store.IsNotFound(err) { // ON CONFLICT DO NOTHING: already imported
				res.Existing++
				continue
			}
			if err != nil {
				return err
			}
			created = append(created, row)
			if err := s.addToGroups(ctx, q, tenantID, row.ID, append(slices.Clone(baseGroups), deref(u.Groups)...)); err != nil {
				return err
			}
			if err := audit.Record(ctx, q, audit.Entry{TenantID: &tenantID, Action: "user.imported", TargetType: "tenant_user", TargetID: row.ID.String(),
				After: map[string]any{"email": row.Email, "role": row.Role}}); err != nil {
				return err
			}
		}
		if len(created) > 0 {
			if _, err := routing.Recompute(ctx, q, tenantID); err != nil {
				return err
			}
		}
		res.Created = len(created)
		res.SeatsUsed = int(held) + len(created)
		res.SeatsPaid = -1
		if s.Seats != nil {
			paid, err := s.Seats.PaidSeats(ctx, tenantID)
			if err != nil {
				return err
			}
			res.SeatsPaid = paid
			if res.SeatsUsed > paid {
				seatLimitHit.Inc()
				return httpapi.Errf(http.StatusUnprocessableEntity, gen.SeatLimitReached,
					fmt.Sprintf("import needs %d seats; the subscription has %d", res.SeatsUsed, paid))
			}
		}
		return nil
	})
	if err != nil {
		return gen.ImportResult{}, err
	}

	// Sign-in accounts are created after commit; a failure leaves the user `invited` without a
	// Zitadel ID, and the reconciler retries it.
	if s.Identity != nil && link.ZitadelOrgID != nil {
		invite := slices.Contains(link.SignInMethods, "managed")
		for _, u := range created {
			zid, err := s.Identity.CreateUser(ctx, *link.ZitadelOrgID, identity.NewUser{Email: u.Email, DisplayName: u.DisplayName, Invite: invite})
			if err != nil {
				slog.WarnContext(ctx, "sign-in account not created yet", "user", u.ID, "err", err)
			}
			if zid != "" {
				if err := s.Store.SetTenantUserZitadelID(ctx, db.SetTenantUserZitadelIDParams{ID: u.ID, ZitadelUserID: &zid}); err != nil {
					return res, err
				}
			}
		}
	}
	return res, nil
}

func (s *Service) addToGroups(ctx context.Context, q *db.Queries, tenantID, userID uuid.UUID, names []string) error {
	slices.Sort(names)
	for _, n := range slices.Compact(names) {
		kind := db.GroupKindTeam
		if n == "all-users" || strings.HasPrefix(n, "route:") {
			kind = db.GroupKindSystem
		}
		g, err := q.CreateGroup(ctx, db.CreateGroupParams{ID: uuid.Must(uuid.NewV7()), TenantID: tenantID, Name: n, Kind: kind})
		if err != nil {
			return err
		}
		if err := q.AddGroupMember(ctx, db.AddGroupMemberParams{GroupID: g.ID, TenantUserID: userID}); err != nil {
			return err
		}
	}
	return nil
}

// List returns the tenant's users with their groups.
func (s *Service) List(ctx context.Context, tenantID uuid.UUID) ([]gen.User, error) {
	rows, err := s.Store.ListTenantUsers(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	members, err := s.Store.ListTenantMemberships(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	groups := map[uuid.UUID][]string{}
	for _, m := range members {
		groups[m.TenantUserID] = append(groups[m.TenantUserID], m.Name)
	}
	out := make([]gen.User, 0, len(rows))
	for _, r := range rows {
		g := groups[r.ID]
		name := r.DisplayName
		out = append(out, gen.User{Id: r.ID, Email: r.Email, Name: &name, Role: gen.UserRole(r.Role), Status: gen.UserStatus(r.Status), Groups: &g})
	}
	return out, nil
}

// GroupsForSignInUser returns the group names for a Zitadel user, for the "groups" claim.
func (s *Service) GroupsForSignInUser(ctx context.Context, zitadelUserID string) ([]string, error) {
	u, err := s.Store.GetTenantUserByZitadelID(ctx, &zitadelUserID)
	if err != nil {
		return nil, err
	}
	if u.Status == db.TenantUserStatusDisabled || u.Status == db.TenantUserStatusRemoved {
		return []string{}, nil
	}
	return s.Store.ListUserGroupNames(ctx, u.ID)
}

// allowed implements FR-003: the email's domain is allowed, or the address is listed.
func allowed(email string, link db.IdentityLink) bool {
	email = strings.ToLower(email)
	if slices.Contains(link.AllowedEmails, email) {
		return true
	}
	_, domain, ok := strings.Cut(email, "@")
	return ok && slices.Contains(link.AllowedDomains, domain)
}

func deref[T any](p *[]T) []T {
	if p == nil {
		return nil
	}
	return *p
}
