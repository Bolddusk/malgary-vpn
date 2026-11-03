package tenancy

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"malgary/backend/internal/audit"
	"malgary/backend/internal/httpapi"
	"malgary/backend/internal/httpapi/gen"
	"malgary/backend/internal/store"
	"malgary/backend/internal/store/db"
)

// GetIdentity returns the tenant's sign-in settings in API form.
func (s *Service) GetIdentity(ctx context.Context, id uuid.UUID) (gen.Identity, error) {
	link, err := s.Store.GetIdentityLink(ctx, id)
	if store.IsNotFound(err) {
		return gen.Identity{}, httpapi.ErrNotFound
	}
	if err != nil {
		return gen.Identity{}, err
	}
	return identityView(link)
}

// PutIdentity validates and stores new sign-in settings, then pushes them to Malgary sign-in.
func (s *Service) PutIdentity(ctx context.Context, id uuid.UUID, in gen.Identity) (gen.Identity, error) {
	probe := gen.TenantDefinition{}
	probe.Spec.Identity = in
	probe.Spec.Gateways = append(probe.Spec.Gateways, struct {
		Pair string `json:"pair"`
	}{Pair: "-"})
	probe.Spec.Routing.Default.Mode = gen.RoutingTargetModeFullTunnel
	if err := validateDefinition(probe); err != nil {
		return gen.Identity{}, err
	}
	err := s.Store.WithTx(ctx, func(q *db.Queries, _ pgxTx) error {
		before, err := q.GetIdentityLink(ctx, id)
		if store.IsNotFound(err) {
			return httpapi.ErrNotFound
		}
		if err != nil {
			return err
		}
		if identityUnchanged(before, in) {
			return nil
		}
		if err := upsertIdentity(ctx, q, id, in, before.ZitadelOrgID, before.ZitadelAppClientID); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{TenantID: &id, Action: "tenant.identity_updated", TargetType: "tenant", TargetID: id.String(), Before: before, After: in})
	})
	if err != nil {
		return gen.Identity{}, err
	}
	if err := s.SyncSignIn(ctx, id); err != nil {
		return gen.Identity{}, err
	}
	return s.GetIdentity(ctx, id)
}

// MarkPTARegistered records the PTA registration of a pair's egress IP (FR-011).
func (s *Service) MarkPTARegistered(ctx context.Context, pair string, at time.Time, ref string) error {
	return s.Store.WithTx(ctx, func(q *db.Queries, _ pgxTx) error {
		p, err := q.MarkGatewayPairPTARegistered(ctx, db.MarkGatewayPairPTARegisteredParams{Name: pair, PtaRegisteredAt: &at, PtaReference: &ref})
		if store.IsNotFound(err) {
			return httpapi.Errf(http.StatusNotFound, gen.NotFound, "unknown gateway pair "+pair)
		}
		if err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{Action: "gateway_pair.pta_registered", TargetType: "gateway_pair", TargetID: p.ID.String(),
			After: map[string]any{"egressIp": p.EgressIp.String(), "reference": ref, "registeredAt": at}})
	})
}

func identityView(link db.IdentityLink) (gen.Identity, error) {
	var idps []gen.UpstreamIdp
	if err := json.Unmarshal(link.UpstreamIdps, &idps); err != nil {
		return gen.Identity{}, err
	}
	methods := make([]gen.IdentitySignInMethods, 0, len(link.SignInMethods))
	for _, m := range link.SignInMethods {
		methods = append(methods, gen.IdentitySignInMethods(m))
	}
	emails := make([]openapi_types.Email, 0, len(link.AllowedEmails))
	for _, e := range link.AllowedEmails {
		emails = append(emails, openapi_types.Email(e))
	}
	domains := link.AllowedDomains
	mfa := link.MfaRequired
	return gen.Identity{SignInMethods: methods, UpstreamIdps: &idps, AllowedDomains: &domains, AllowedEmails: &emails, MfaRequired: &mfa}, nil
}
