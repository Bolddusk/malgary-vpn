package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"golang.org/x/oauth2"

	"malgary/backend/internal/client"
)

type config struct {
	API, Issuer, ClientID string
}

func loadConfig() config {
	get := func(k, d string) string {
		if v := os.Getenv(k); v != "" {
			return v
		}
		return d
	}
	return config{
		API:      get("MALGARY_API", "https://api.vpn.malgary.com/v1"),
		Issuer:   get("MALGARY_ISSUER", "https://id.vpn.malgary.com"),
		ClientID: os.Getenv("MALGARY_CLIENT"),
	}
}

func oauthConfig(c config) *oauth2.Config {
	return &oauth2.Config{
		ClientID: c.ClientID,
		Scopes:   []string{"openid", "profile", "email", "offline_access", "urn:zitadel:iam:org:project:roles"},
		Endpoint: oauth2.Endpoint{
			DeviceAuthURL: c.Issuer + "/oauth/v2/device_authorization",
			TokenURL:      c.Issuer + "/oauth/v2/token",
		},
	}
}

func tokenPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "malgaryctl", "token.json"), nil
}

// login runs the OIDC device flow; MFA happens in the browser on Malgary sign-in.
func login(ctx context.Context, c config) error {
	if c.ClientID == "" {
		return errors.New("MALGARY_CLIENT is not set")
	}
	oc := oauthConfig(c)
	da, err := oc.DeviceAuth(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("Open %s and enter code %s\n", da.VerificationURI, da.UserCode)
	tok, err := oc.DeviceAccessToken(ctx, da)
	if err != nil {
		return err
	}
	p, err := tokenPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, _ := json.Marshal(tok) //nolint:gosec // the CLI stores its own token in a 0600 file
	if err := os.WriteFile(p, b, 0o600); err != nil {
		return err
	}
	fmt.Println("Signed in.")
	return nil
}

type apiClient struct {
	*client.ClientWithResponses
}

func newClient(ctx context.Context, c config) (*apiClient, error) {
	p, err := tokenPath()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p) //nolint:gosec // path is the user config dir
	if err != nil {
		return nil, errors.New("not signed in: run `malgaryctl login`")
	}
	var tok oauth2.Token
	if err := json.Unmarshal(b, &tok); err != nil {
		return nil, err
	}
	httpc := oauthConfig(c).Client(ctx, &tok)
	cl, err := client.NewClientWithResponses(c.API, client.WithHTTPClient(httpc))
	if err != nil {
		return nil, err
	}
	return &apiClient{cl}, nil
}

// problemError turns a non-2xx response body into a readable error.
func problemError(status int, body []byte) error {
	var p struct {
		Code, Detail string
	}
	if json.Unmarshal(body, &p) == nil && p.Code != "" {
		return fmt.Errorf("%s (%d): %s", p.Code, status, p.Detail)
	}
	return fmt.Errorf("%s: %s", http.StatusText(status), body)
}
