package identity

import (
	"context"
	"fmt"
	"net/url"
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
	// InviteAsLink returns the invitation as a one-time URL instead of emailing it, while no
	// email delivery is configured (feature 002, FR-012a).
	InviteAsLink bool
}

// CreatedUser is the result of CreateUser. InviteURL is set only for link invitations; it is
// a credential, so callers show it once and never store or log it.
type CreatedUser struct {
	ID        string
	InviteURL string
}

// CreateUser adds a user to the tenant's org. The email is trusted as verified because the
// tenant imported it.
func (z *Zitadel) CreateUser(ctx context.Context, orgID string, u NewUser) (CreatedUser, error) {
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
			return CreatedUser{}, fmt.Errorf("user %s already exists in sign-in: %w", u.Email, err)
		}
		return CreatedUser{}, fmt.Errorf("add user %s: %w", u.Email, err)
	}
	out := CreatedUser{ID: res.UserId}
	if !u.Invite {
		return out, nil
	}
	req := &userv2.CreateInviteCodeRequest{UserId: res.UserId}
	if u.InviteAsLink {
		req.Verification = &userv2.CreateInviteCodeRequest_ReturnCode{ReturnCode: &userv2.ReturnInviteCode{}}
	} else {
		app := "Malgary VPN"
		req.Verification = &userv2.CreateInviteCodeRequest_SendCode{SendCode: &userv2.SendInviteCode{ApplicationName: &app}}
	}
	inv, err := z.c.UserServiceV2().CreateInviteCode(ctx, req)
	if err != nil {
		return out, fmt.Errorf("invite %s: %w", u.Email, err)
	}
	if u.InviteAsLink {
		out.InviteURL = z.inviteURL(orgID, res.UserId, u.Email, inv.GetInviteCode())
	}
	return out, nil
}

// inviteURL builds the link Zitadel's v2 login app would have emailed (its default invite
// template), so the user lands on "set password and MFA".
func (z *Zitadel) inviteURL(orgID, userID, loginName, code string) string {
	q := url.Values{"userId": {userID}, "loginName": {loginName}, "code": {code}, "orgId": {orgID}, "invite": {"true"}}
	return z.issuer + "/ui/v2/login/verify?" + q.Encode()
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
