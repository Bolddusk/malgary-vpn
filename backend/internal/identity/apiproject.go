package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"

	appv2 "github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/application/v2"
	authzv2 "github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/authorization/v2"
	"github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/management"
	objv2 "github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/object/v2"
	projectv2 "github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/project/v2"
	userv2 "github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/user/v2"
)

// APIRoles are the backend's roles, carried in tokens as project roles (httpapi/auth.go).
var APIRoles = []string{"owner", "operator", "tenant_admin", "deployer"}

// APIProject identifies the backend's project in Malgary sign-in: ProjectID is the audience of
// API tokens, CLIClientID the device-flow client used by malgaryctl.
type APIProject struct {
	ProjectID   string
	CLIClientID string
}

// EnsureAPIProject creates the "Malgary API" project in the Malgary organization, its roles and
// the malgaryctl device-flow app, and gives ownerEmail the owner role. existing holds the IDs
// from an earlier run (empty on first start); only missing parts are created, so it is safe to
// repeat. It runs once, on the first start of a fresh installation.
func (z *Zitadel) EnsureAPIProject(ctx context.Context, existing APIProject, ownerEmail string) (APIProject, error) {
	org, err := z.c.ManagementService().GetMyOrg(ctx, &management.GetMyOrgRequest{})
	if err != nil {
		return existing, fmt.Errorf("own organization: %w", err)
	}
	orgID := org.GetOrg().GetId()
	out := existing
	if out.ProjectID == "" {
		proj, err := z.c.ProjectServiceV2().CreateProject(ctx, &projectv2.CreateProjectRequest{
			OrganizationId: orgID, Name: "Malgary API", ProjectRoleAssertion: true,
		})
		if err != nil {
			return out, fmt.Errorf("create API project: %w", err)
		}
		out.ProjectID = proj.GetProjectId()
		for _, r := range APIRoles {
			if _, err := z.c.ProjectServiceV2().AddProjectRole(ctx, &projectv2.AddProjectRoleRequest{
				ProjectId: out.ProjectID, RoleKey: r, DisplayName: strings.ReplaceAll(r, "_", " "),
			}); err != nil {
				return out, fmt.Errorf("add role %s: %w", r, err)
			}
		}
	}
	if out.CLIClientID == "" {
		app, err := z.c.ApplicationServiceV2().CreateApplication(ctx, &appv2.CreateApplicationRequest{
			ProjectId: out.ProjectID,
			Name:      "malgaryctl",
			ApplicationType: &appv2.CreateApplicationRequest_OidcConfiguration{
				OidcConfiguration: &appv2.CreateOIDCApplicationRequest{
					ResponseTypes:            []appv2.OIDCResponseType{appv2.OIDCResponseType_OIDC_RESPONSE_TYPE_CODE},
					GrantTypes:               []appv2.OIDCGrantType{appv2.OIDCGrantType_OIDC_GRANT_TYPE_DEVICE_CODE, appv2.OIDCGrantType_OIDC_GRANT_TYPE_REFRESH_TOKEN},
					ApplicationType:          appv2.OIDCApplicationType_OIDC_APP_TYPE_NATIVE,
					AuthMethodType:           appv2.OIDCAuthMethodType_OIDC_AUTH_METHOD_TYPE_NONE,
					AccessTokenType:          appv2.OIDCTokenType_OIDC_TOKEN_TYPE_JWT,
					AccessTokenRoleAssertion: true,
					IdTokenRoleAssertion:     true,
				},
			},
		})
		if err != nil {
			return out, fmt.Errorf("create malgaryctl app: %w", err)
		}
		oidc, ok := app.GetApplicationType().(*appv2.CreateApplicationResponse_OidcConfiguration)
		if !ok {
			return out, errors.New("create malgaryctl app: unexpected response type")
		}
		out.CLIClientID = oidc.OidcConfiguration.GetClientId()
	}
	if ownerEmail == "" {
		return out, nil
	}
	users, err := z.c.UserServiceV2().ListUsers(ctx, &userv2.ListUsersRequest{Queries: []*userv2.SearchQuery{{
		Query: &userv2.SearchQuery_EmailQuery{EmailQuery: &userv2.EmailQuery{
			EmailAddress: ownerEmail, Method: objv2.TextQueryMethod_TEXT_QUERY_METHOD_EQUALS_IGNORE_CASE,
		}},
	}}})
	if err != nil {
		return out, fmt.Errorf("find owner %s: %w", ownerEmail, err)
	}
	if len(users.GetResult()) != 1 {
		return out, fmt.Errorf("owner %s: found %d sign-in accounts, want 1", ownerEmail, len(users.GetResult()))
	}
	if _, err := z.c.AuthorizationServiceV2().CreateAuthorization(ctx, &authzv2.CreateAuthorizationRequest{
		UserId: users.GetResult()[0].GetUserId(), ProjectId: out.ProjectID, OrganizationId: orgID, RoleKeys: []string{"owner"},
	}); err != nil && !strings.Contains(strings.ToLower(err.Error()), "already exists") {
		return out, fmt.Errorf("grant owner role: %w", err)
	}
	return out, nil
}
