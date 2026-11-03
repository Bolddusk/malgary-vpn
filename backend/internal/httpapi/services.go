package httpapi

import (
	"context"
	"time"

	"github.com/google/uuid"

	"malgary/backend/internal/httpapi/gen"
)

// TenancyService is implemented by internal/tenancy. Declared here so handlers don't import
// the domain package's internals and tests can substitute a fake.
type TenancyService interface {
	Apply(ctx context.Context, def gen.TenantDefinition) (gen.Tenant, bool, error) // bool: created
	Get(ctx context.Context, id uuid.UUID) (gen.Tenant, error)
	List(ctx context.Context) ([]gen.Tenant, error)
	Report(ctx context.Context, id uuid.UUID, rep gen.StackReport) error
	GetIdentity(ctx context.Context, id uuid.UUID) (gen.Identity, error)
	PutIdentity(ctx context.Context, id uuid.UUID, in gen.Identity) (gen.Identity, error)
	MarkPTARegistered(ctx context.Context, pair string, at time.Time, ref string) error
	GetRoutingRules(ctx context.Context, tenantID uuid.UUID) (gen.RoutingRules, error)
	PutRoutingRules(ctx context.Context, tenantID uuid.UUID, rules []gen.RoutingRule, exclusions *[]string) (gen.RoutingRules, error)
	GetPostureRules(ctx context.Context, tenantID uuid.UUID) ([]gen.PostureRule, error)
	PutPostureRules(ctx context.Context, tenantID uuid.UUID, in []gen.PostureRule) ([]gen.PostureRule, error)
}

// UsersService is implemented by internal/users.
type UsersService interface {
	Import(ctx context.Context, tenantID uuid.UUID, in []gen.UserImport) (gen.ImportResult, error)
	List(ctx context.Context, tenantID uuid.UUID) ([]gen.User, error)
	GroupsForSignInUser(ctx context.Context, zitadelUserID string) ([]string, error)
	ListGroups(ctx context.Context, tenantID uuid.UUID) ([]gen.Group, error)
	CreateGroup(ctx context.Context, tenantID uuid.UUID, in gen.GroupInput) (gen.Group, error)
	PatchGroup(ctx context.Context, tenantID, groupID uuid.UUID, in gen.GroupInput) (gen.Group, error)
	DeleteGroup(ctx context.Context, tenantID, groupID uuid.UUID) error
}
