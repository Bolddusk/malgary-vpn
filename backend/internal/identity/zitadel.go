// Package identity manages Malgary sign-in (Zitadel): one organization per tenant, with the
// tenant's own sign-in methods, IdPs, MFA policy and branding (research R4, Principle VII).
package identity

import (
	"context"
	"fmt"
	"net/url"

	"github.com/zitadel/zitadel-go/v3/pkg/client"
	appv2 "github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/application/v2"
	orgv2 "github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/org/v2"
	projectv2 "github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/project/v2"
	"github.com/zitadel/zitadel-go/v3/pkg/zitadel"
)

// TenantApp is what a tenant's NetBird stack needs to use Zitadel as its only connector.
type TenantApp struct {
	OrgID        string
	ProjectID    string
	ClientID     string
	ClientSecret string // returned once at creation; stored behind a secret:// ref by the caller
}

// Provider is the identity operations the backend needs. Zitadel implements it; tests fake it.
type Provider interface {
	EnsureTenantOrg(ctx context.Context, slug, legalName, managementURL string) (TenantApp, error)
	ConfigureSignIn(ctx context.Context, orgID string, cfg SignInConfig) error
	ApplyBranding(ctx context.Context, orgID string) error
	CreateUser(ctx context.Context, orgID string, u NewUser) (string, error)
	Issuer() string
}

// Zitadel talks to the Zitadel APIs with the backend's machine key.
type Zitadel struct {
	c      *client.Client
	issuer string
	shared SharedIdPs
}

// NewZitadel connects using the machine key JSON at keyPath.
func NewZitadel(ctx context.Context, issuer, keyPath string, shared SharedIdPs) (*Zitadel, error) {
	u, err := url.Parse(issuer)
	if err != nil {
		return nil, err
	}
	c, err := client.New(ctx, zitadel.New(u.Hostname()),
		client.WithAuth(client.DefaultServiceUserAuthentication(keyPath, "openid", client.ScopeZitadelAPI())))
	if err != nil {
		return nil, fmt.Errorf("zitadel client: %w", err)
	}
	return &Zitadel{c: c, issuer: issuer, shared: shared}, nil
}

// Issuer is the OIDC issuer URL tenant stacks point their connector at.
func (z *Zitadel) Issuer() string { return z.issuer }

// EnsureTenantOrg creates the tenant's organization, a project that enforces the tenant org's
// login policy and branding, and one OIDC web app for the tenant's NetBird embedded IdP.
// One app per tenant org, not project grants, avoids zitadel#11869 (research R4).
//
// Callers persist the returned IDs and only call this when IdentityLink has no org yet, so it is
// not re-run for an existing tenant.
func (z *Zitadel) EnsureTenantOrg(ctx context.Context, slug, legalName, managementURL string) (TenantApp, error) {
	org, err := z.c.OrganizationServiceV2().AddOrganization(ctx, &orgv2.AddOrganizationRequest{
		Name: fmt.Sprintf("%s (%s)", legalName, slug),
	})
	if err != nil {
		return TenantApp{}, fmt.Errorf("add organization: %w", err)
	}
	proj, err := z.c.ProjectServiceV2().CreateProject(ctx, &projectv2.CreateProjectRequest{
		OrganizationId:       org.OrganizationId,
		Name:                 "netbird",
		ProjectRoleAssertion: true,
		// Only users of this org can use the app (Principle I).
		ProjectAccessRequired: true,
		// Sign-in uses this tenant's login policy and branding (Principle X).
		PrivateLabelingSetting: projectv2.PrivateLabelingSetting_PRIVATE_LABELING_SETTING_ENFORCE_PROJECT_RESOURCE_OWNER_POLICY,
	})
	if err != nil {
		return TenantApp{}, fmt.Errorf("create project: %w", err)
	}
	app, err := z.c.ApplicationServiceV2().CreateApplication(ctx, &appv2.CreateApplicationRequest{
		ProjectId: proj.ProjectId,
		Name:      "netbird-" + slug,
		ApplicationType: &appv2.CreateApplicationRequest_OidcConfiguration{
			OidcConfiguration: &appv2.CreateOIDCApplicationRequest{
				// NetBird's embedded Dex callback (idp/dex/connector.go GetRedirectURI).
				RedirectUris:             []string{managementURL + "/oauth2/callback"},
				ResponseTypes:            []appv2.OIDCResponseType{appv2.OIDCResponseType_OIDC_RESPONSE_TYPE_CODE},
				GrantTypes:               []appv2.OIDCGrantType{appv2.OIDCGrantType_OIDC_GRANT_TYPE_AUTHORIZATION_CODE},
				ApplicationType:          appv2.OIDCApplicationType_OIDC_APP_TYPE_WEB,
				AuthMethodType:           appv2.OIDCAuthMethodType_OIDC_AUTH_METHOD_TYPE_BASIC,
				Version:                  appv2.OIDCVersion_OIDC_VERSION_1_0,
				AccessTokenType:          appv2.OIDCTokenType_OIDC_TOKEN_TYPE_JWT,
				IdTokenRoleAssertion:     true,
				IdTokenUserinfoAssertion: true,
			},
		},
	})
	if err != nil {
		return TenantApp{}, fmt.Errorf("create oidc app: %w", err)
	}
	oidc, ok := app.ApplicationType.(*appv2.CreateApplicationResponse_OidcConfiguration)
	if !ok || oidc.OidcConfiguration == nil {
		return TenantApp{}, fmt.Errorf("create oidc app: unexpected response type")
	}
	return TenantApp{
		OrgID:        org.OrganizationId,
		ProjectID:    proj.ProjectId,
		ClientID:     oidc.OidcConfiguration.ClientId,
		ClientSecret: oidc.OidcConfiguration.ClientSecret,
	}, nil
}
