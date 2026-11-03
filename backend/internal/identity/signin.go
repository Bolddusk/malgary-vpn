package identity

import (
	"context"
	"fmt"
	"slices"

	"github.com/zitadel/zitadel-go/v3/pkg/client/middleware"
	"github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/idp"
	"github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/management"
	"github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/policy"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// SignInConfig is a tenant's chosen sign-in setup (constitution Principle VII, FR-002).
type SignInConfig struct {
	SSO         bool     // sign in through the customer's identity provider
	Managed     bool     // Malgary-managed accounts: email/username, password and MFA
	MFARequired bool     // also require Malgary MFA for SSO users (managed accounts always MFA)
	Upstreams   []string // "google", "entra"; instance-level IdPs shared by all tenants
}

// SharedIdPs holds the IDs of instance-level identity providers created once by the owners in
// the Zitadel console (docs/runbooks/add-tenant.md). One Malgary Google OAuth app and one
// multi-tenant Microsoft app serve every tenant; users are only linked when they were imported
// first (auto-creation off), which enforces the tenant's allowed list (FR-003).
type SharedIdPs struct {
	Google string
	Entra  string
}

// ConfigureSignIn sets the tenant org's custom login policy. Password-only sign-in is never
// possible: whenever passwords are allowed, MFA is forced for them.
func (z *Zitadel) ConfigureSignIn(ctx context.Context, orgID string, cfg SignInConfig) error {
	if !cfg.SSO && !cfg.Managed {
		return fmt.Errorf("at least one sign-in method is required")
	}
	ctx = middleware.SetOrgID(ctx, orgID)
	idps, err := z.idpIDs(cfg)
	if err != nil {
		return err
	}
	forceMFA := cfg.Managed || cfg.MFARequired
	// ForceMfaLocalOnly: force MFA for passwords but trust the customer's IdP for SSO users,
	// unless the tenant asked for Malgary MFA on SSO too.
	forceLocalOnly := cfg.Managed && !cfg.MFARequired

	mgmt := z.c.ManagementService()
	cur, err := mgmt.GetLoginPolicy(ctx, &management.GetLoginPolicyRequest{})
	if err != nil {
		return fmt.Errorf("get login policy: %w", err)
	}
	second := []policy.SecondFactorType{policy.SecondFactorType_SECOND_FACTOR_TYPE_OTP, policy.SecondFactorType_SECOND_FACTOR_TYPE_U2F}
	multi := []policy.MultiFactorType{policy.MultiFactorType_MULTI_FACTOR_TYPE_U2F_WITH_VERIFICATION}
	if cur.IsDefault {
		req := &management.AddCustomLoginPolicyRequest{
			AllowUsernamePassword:  cfg.Managed,
			AllowExternalIdp:       cfg.SSO,
			AllowRegister:          false,
			ForceMfa:               forceMFA,
			ForceMfaLocalOnly:      forceLocalOnly,
			IgnoreUnknownUsernames: true,
			SecondFactors:          second,
			MultiFactors:           multi,
		}
		for _, id := range idps {
			req.Idps = append(req.Idps, &management.AddCustomLoginPolicyRequest_IDP{IdpId: id, OwnerType: idp.IDPOwnerType_IDP_OWNER_TYPE_SYSTEM})
		}
		if _, err := mgmt.AddCustomLoginPolicy(ctx, req); err != nil {
			return fmt.Errorf("add login policy: %w", err)
		}
		return nil
	}
	if _, err := mgmt.UpdateCustomLoginPolicy(ctx, &management.UpdateCustomLoginPolicyRequest{
		AllowUsernamePassword:  cfg.Managed,
		AllowExternalIdp:       cfg.SSO,
		AllowRegister:          false,
		ForceMfa:               forceMFA,
		ForceMfaLocalOnly:      forceLocalOnly,
		IgnoreUnknownUsernames: true,
	}); err != nil && status.Code(err) != codes.FailedPrecondition { // unchanged policy
		return fmt.Errorf("update login policy: %w", err)
	}
	for _, id := range idps {
		_, err := mgmt.AddIDPToLoginPolicy(ctx, &management.AddIDPToLoginPolicyRequest{IdpId: id, OwnerType: idp.IDPOwnerType_IDP_OWNER_TYPE_SYSTEM})
		if err != nil && status.Code(err) != codes.AlreadyExists {
			return fmt.Errorf("add idp %s to login policy: %w", id, err)
		}
	}
	return nil
}

func (z *Zitadel) idpIDs(cfg SignInConfig) ([]string, error) {
	if !cfg.SSO {
		return nil, nil
	}
	var ids []string
	for _, u := range cfg.Upstreams {
		switch u {
		case "google":
			if z.shared.Google == "" {
				return nil, fmt.Errorf("shared Google IdP is not configured (MALGARY_ZITADEL_GOOGLE_IDP_ID)")
			}
			ids = append(ids, z.shared.Google)
		case "entra":
			if z.shared.Entra == "" {
				return nil, fmt.Errorf("shared Microsoft IdP is not configured (MALGARY_ZITADEL_ENTRA_IDP_ID)")
			}
			ids = append(ids, z.shared.Entra)
		default:
			return nil, fmt.Errorf("upstream IdP type %q is not supported in the pilot", u)
		}
	}
	slices.Sort(ids)
	return slices.Compact(ids), nil
}
