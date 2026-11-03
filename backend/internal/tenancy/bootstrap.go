package tenancy

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/netbirdio/netbird/shared/management/client/rest"
	nbapi "github.com/netbirdio/netbird/shared/management/http/api"

	"malgary/backend/internal/identity"
	"malgary/backend/internal/netbird"
	"malgary/backend/internal/secrets"
)

// Secret reference paths for a tenant (never values).
func refNetbirdPAT(slug string) string { return secrets.Ref("tenants/" + slug + "/netbird-pat") }
func refOwnerPassword(slug string) string {
	return secrets.Ref("tenants/" + slug + "/netbird-owner-password")
}
func refGatewaySetupKey(slug string) string { return secrets.Ref("tenants/" + slug + "/gw") }
func refDBPassword(slug string) string      { return secrets.Ref("tenants/" + slug + "/db") }
func refOIDCSecret(slug string) string      { return secrets.Ref("tenants/" + slug + "/oidc") }

// System groups every tenant stack has (T041).
const (
	groupAllUsers = "all-users"
	groupGateways = "gateways"
)

// bootstrapper turns a freshly deployed, empty NetBird stack into a managed one:
//  1. /api/setup creates the break-glass owner and a short-lived PAT
//     (needs NB_SETUP_PAT_ENABLED=true on the stack);
//  2. a service user with a 1-year PAT becomes the backend's credential;
//  3. system groups and the gateway setup key are created;
//  4. Malgary sign-in (Zitadel) is added as the stack's identity provider.
type bootstrapper struct {
	nb      *netbird.Factory
	secrets secrets.Resolver
}

func (b bootstrapper) run(ctx context.Context, slug, mgmtURL string, app identity.TenantApp, issuer string) error {
	anon := b.nb.Anonymous(mgmtURL)
	st, err := anon.Instance.GetStatus(ctx)
	if err != nil {
		return fmt.Errorf("instance status: %w", err)
	}
	if st.SetupRequired {
		if err := b.setupOwner(ctx, anon, slug, mgmtURL); err != nil {
			return err
		}
	}
	pat, err := b.secrets.Resolve(refNetbirdPAT(slug))
	if err != nil {
		return fmt.Errorf("stack %s is set up but the backend has no PAT: %w", slug, err)
	}
	c, err := b.nb.For(ctx, mgmtURL, strings.TrimSpace(string(pat)))
	if err != nil {
		return err
	}
	gwGroup, err := ensureGroup(ctx, c, groupGateways)
	if err != nil {
		return err
	}
	if _, err := ensureGroup(ctx, c, groupAllUsers); err != nil {
		return err
	}
	if !b.secrets.Exists(refGatewaySetupKey(slug)) {
		key, err := c.SetupKeys.Create(ctx, nbapi.CreateSetupKeyRequest{
			Name:       "malgary-gateways",
			Type:       "reusable",
			AutoGroups: []string{gwGroup},
			ExpiresIn:  0,
			UsageLimit: 0,
		})
		if err != nil {
			return fmt.Errorf("gateway setup key: %w", err)
		}
		if err := b.secrets.Put(refGatewaySetupKey(slug), []byte(key.Key)); err != nil {
			return err
		}
	}
	if err := ensureAccountSettings(ctx, c); err != nil {
		return err
	}
	return ensureMalgarySignIn(ctx, c, issuer, app, b.secrets, slug)
}

// ensureAccountSettings turns on JWT group sync, so group membership kept by the backend flows
// to NetBird through the "groups" claim from Malgary sign-in, and hides the dashboard from
// regular users (customers only ever use the Malgary portal, Principle X).
func ensureAccountSettings(ctx context.Context, c *rest.Client) error {
	accts, err := c.Accounts.List(ctx)
	if err != nil || len(accts) == 0 {
		return fmt.Errorf("list accounts: %w", err)
	}
	a := accts[0]
	st := a.Settings
	on, claim := true, "groups"
	if st.JwtGroupsEnabled != nil && *st.JwtGroupsEnabled && st.JwtGroupsClaimName != nil && *st.JwtGroupsClaimName == claim && st.RegularUsersViewBlocked {
		return nil
	}
	st.JwtGroupsEnabled, st.JwtGroupsClaimName, st.GroupsPropagationEnabled = &on, &claim, &on
	st.RegularUsersViewBlocked = true
	if _, err := c.Accounts.Update(ctx, a.Id, nbapi.AccountRequest{Settings: st}); err != nil {
		return fmt.Errorf("account settings: %w", err)
	}
	return nil
}

// setupOwner creates the stack's first (break-glass) owner and the backend's service user.
func (b bootstrapper) setupOwner(ctx context.Context, anon *rest.Client, slug, mgmtURL string) error {
	pw, err := randomSecret(24)
	if err != nil {
		return err
	}
	if err := b.secrets.Put(refOwnerPassword(slug), []byte(pw)); err != nil {
		return err
	}
	createPAT, days := true, 1
	res, err := anon.Instance.Setup(ctx, nbapi.SetupRequest{
		Email:       "owner@" + slug + ".break-glass.malgary.invalid",
		Name:        "Malgary break-glass owner",
		Password:    pw,
		CreatePat:   &createPAT,
		PatExpireIn: &days,
	})
	if err != nil {
		return fmt.Errorf("instance setup: %w", err)
	}
	if res.PersonalAccessToken == nil || *res.PersonalAccessToken == "" {
		return errors.New("instance setup returned no PAT: is NB_SETUP_PAT_ENABLED=true on the stack?")
	}
	ownerClient, err := b.nb.For(ctx, mgmtURL, *res.PersonalAccessToken)
	if err != nil {
		return err
	}
	name := "malgary-backend"
	svc, err := ownerClient.Users.Create(ctx, nbapi.UserCreateRequest{
		Name: &name, Role: "admin", IsServiceUser: true, AutoGroups: []string{},
	})
	if err != nil {
		return fmt.Errorf("service user: %w", err)
	}
	tok, err := ownerClient.Tokens.Create(ctx, svc.Id, nbapi.PersonalAccessTokenRequest{Name: "malgary-backend", ExpiresIn: 365})
	if err != nil {
		return fmt.Errorf("service user token: %w", err)
	}
	return b.secrets.Put(refNetbirdPAT(slug), []byte(tok.PlainToken))
}

func ensureGroup(ctx context.Context, c *rest.Client, name string) (string, error) {
	if g, err := c.Groups.GetByName(ctx, name); err == nil && g != nil {
		return g.Id, nil
	}
	g, err := c.Groups.Create(ctx, nbapi.GroupRequest{Name: name})
	if err != nil {
		return "", fmt.Errorf("group %s: %w", name, err)
	}
	return g.Id, nil
}

// ensureMalgarySignIn registers the tenant's Zitadel app as the stack's identity provider.
func ensureMalgarySignIn(ctx context.Context, c *rest.Client, issuer string, app identity.TenantApp, r secrets.Resolver, slug string) error {
	if app.ClientID == "" {
		return nil // identity provider not configured yet (e.g. local development)
	}
	idps, err := c.IdentityProviders.List(ctx)
	if err != nil {
		return fmt.Errorf("list identity providers: %w", err)
	}
	for _, p := range idps {
		if p.Type == nbapi.IdentityProviderTypeZitadel {
			return nil
		}
	}
	secret, err := r.Resolve(refOIDCSecret(slug))
	if err != nil {
		return err
	}
	_, err = c.IdentityProviders.Create(ctx, nbapi.IdentityProviderRequest{
		Name:         "Malgary sign-in",
		Type:         nbapi.IdentityProviderTypeZitadel,
		Issuer:       issuer,
		ClientId:     app.ClientID,
		ClientSecret: strings.TrimSpace(string(secret)),
	})
	if err != nil {
		return fmt.Errorf("add Malgary sign-in to stack: %w", err)
	}
	return nil
}

func randomSecret(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
