package httpapi

import (
	"context"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"

	"malgary/backend/internal/audit"
	"malgary/backend/internal/store/db"
)

// Claim names. Roles come from Zitadel project roles; the tenant ID is set on tenant admins'
// user metadata and exposed as a claim by a Zitadel Action.
const (
	claimRoles    = "urn:zitadel:iam:org:project:roles"
	claimTenantID = "malgary:tenant_id"
)

// Verifier validates bearer tokens. It is an interface so tests can stub it.
type Verifier interface {
	Verify(ctx context.Context, raw string) (Principal, error)
}

// Principal is the authenticated caller.
type Principal struct {
	Subject  string
	Role     db.ActorType
	TenantID *uuid.UUID
}

// OIDCVerifier verifies Zitadel-issued JWTs against the issuer's JWKS (cached by go-oidc).
type OIDCVerifier struct {
	v *oidc.IDTokenVerifier
}

// NewOIDCVerifier discovers the issuer. audience is the backend's Zitadel project ID.
func NewOIDCVerifier(ctx context.Context, issuer, audience string) (*OIDCVerifier, error) {
	p, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, err
	}
	return &OIDCVerifier{v: p.Verifier(&oidc.Config{ClientID: audience})}, nil
}

// Verify checks the token and maps claims to a Principal.
func (o *OIDCVerifier) Verify(ctx context.Context, raw string) (Principal, error) {
	tok, err := o.v.Verify(ctx, raw)
	if err != nil {
		return Principal{}, ErrUnauthorized
	}
	var c map[string]any
	if err := tok.Claims(&c); err != nil {
		return Principal{}, ErrUnauthorized
	}
	return principalFromClaims(tok.Subject, c)
}

// rolePriority picks the most privileged role when a user has several.
var rolePriority = []db.ActorType{db.ActorTypeOwner, db.ActorTypeOperator, db.ActorTypeDeployer, db.ActorTypeTenantAdmin}

func principalFromClaims(sub string, c map[string]any) (Principal, error) {
	roles, _ := c[claimRoles].(map[string]any)
	p := Principal{Subject: sub}
	for _, r := range rolePriority {
		if _, ok := roles[string(r)]; ok {
			p.Role = r
			break
		}
	}
	if p.Role == "" {
		return Principal{}, ErrUnauthorized
	}
	if p.Role == db.ActorTypeTenantAdmin {
		s, _ := c[claimTenantID].(string)
		id, err := uuid.Parse(s)
		if err != nil {
			return Principal{}, ErrUnauthorized
		}
		p.TenantID = &id
	}
	return p, nil
}

type principalKey struct{}

// PrincipalFrom returns the authenticated caller.
func PrincipalFrom(ctx context.Context) Principal {
	p, _ := ctx.Value(principalKey{}).(Principal)
	return p
}

var tenantPath = regexp.MustCompile(`^/v1/tenants/([0-9a-fA-F-]{36})(?:[/:]|$)`)

// Authenticate verifies the bearer token, enforces tenant scoping and records the actor for
// audit. A tenant admin addressing another tenant gets 404, not 403, so tenant existence
// doesn't leak (Principle X).
func Authenticate(v Verifier, public ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, p := range public {
				if strings.HasPrefix(r.URL.Path, p) {
					next.ServeHTTP(w, r)
					return
				}
			}
			raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if !ok || raw == "" {
				WriteError(w, r, ErrUnauthorized)
				return
			}
			p, err := v.Verify(r.Context(), raw)
			if err != nil {
				WriteError(w, r, ErrUnauthorized)
				return
			}
			if p.Role == db.ActorTypeTenantAdmin {
				m := tenantPath.FindStringSubmatch(r.URL.Path)
				if m == nil || !strings.EqualFold(m[1], p.TenantID.String()) {
					WriteError(w, r, ErrNotFound)
					return
				}
			}
			ctx := context.WithValue(r.Context(), principalKey{}, p)
			ctx = audit.WithActor(ctx, audit.Actor{Type: p.Role, ID: p.Subject, TenantID: p.TenantID},
				RequestIDFrom(ctx), clientIP(r))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// Require returns ErrNotFound unless the caller has one of roles. Tenant-scoped checks are
// already done by Authenticate.
func Require(ctx context.Context, roles ...db.ActorType) error {
	if slices.Contains(roles, PrincipalFrom(ctx).Role) {
		return nil
	}
	return ErrNotFound
}

// Owner-level shortcuts.
var (
	Owners          = []db.ActorType{db.ActorTypeOwner}
	OwnersOperators = []db.ActorType{db.ActorTypeOwner, db.ActorTypeOperator}
	TenantReaders   = []db.ActorType{db.ActorTypeOwner, db.ActorTypeOperator, db.ActorTypeTenantAdmin}
	TenantWriters   = []db.ActorType{db.ActorTypeOwner, db.ActorTypeTenantAdmin}
)

func clientIP(r *http.Request) string {
	// Traefik on the same host sets X-Forwarded-For; the API only listens on 127.0.0.1.
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ip, _, _ := strings.Cut(xff, ",")
		return strings.TrimSpace(ip)
	}
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i > 0 {
		host = host[:i]
	}
	return strings.Trim(host, "[]")
}
