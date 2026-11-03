package identity

import (
	"context"
	"fmt"
	"strings"

	objv2 "github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/object/v2"
	userv2 "github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/user/v2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// NewUser is one imported user.
type NewUser struct {
	Email       string
	DisplayName string
	// Invite sends a Zitadel invitation so a managed-account user sets a password and MFA
	// (FR-013). SSO-only users get a verified email and sign in with their own IdP instead.
	Invite bool
}

// CreateUser adds a user to the tenant's org and returns the Zitadel user ID. The email is
// trusted as verified because the tenant imported it.
func (z *Zitadel) CreateUser(ctx context.Context, orgID string, u NewUser) (string, error) {
	given, family := splitName(u.DisplayName, u.Email)
	//nolint:staticcheck // AddHumanUser is deprecated in favour of CreateUser; migrate once verified in pilot test P-3
	res, err := z.c.UserServiceV2().AddHumanUser(ctx, &userv2.AddHumanUserRequest{
		Username:     &u.Email,
		Organization: &objv2.Organization{Org: &objv2.Organization_OrgId{OrgId: orgID}},
		Profile:      &userv2.SetHumanProfile{GivenName: given, FamilyName: family},
		Email:        &userv2.SetHumanEmail{Email: u.Email, Verification: &userv2.SetHumanEmail_IsVerified{IsVerified: true}},
	})
	if err != nil {
		if status.Code(err) == codes.AlreadyExists {
			return "", fmt.Errorf("user %s already exists in sign-in: %w", u.Email, err)
		}
		return "", fmt.Errorf("add user %s: %w", u.Email, err)
	}
	if u.Invite {
		app := "Malgary VPN"
		if _, err := z.c.UserServiceV2().CreateInviteCode(ctx, &userv2.CreateInviteCodeRequest{
			UserId:       res.UserId,
			Verification: &userv2.CreateInviteCodeRequest_SendCode{SendCode: &userv2.SendInviteCode{ApplicationName: &app}},
		}); err != nil {
			return res.UserId, fmt.Errorf("invite %s: %w", u.Email, err)
		}
	}
	return res.UserId, nil
}

func splitName(display, email string) (string, string) {
	display = strings.TrimSpace(display)
	if display == "" {
		display, _, _ = strings.Cut(email, "@")
	}
	given, family, ok := strings.Cut(display, " ")
	if !ok || family == "" {
		family = "-"
	}
	return given, family
}
