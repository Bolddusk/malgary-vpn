package tenancy

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"malgary/backend/internal/audit"
	"malgary/backend/internal/httpapi"
	"malgary/backend/internal/httpapi/gen"
	"malgary/backend/internal/identity"
	"malgary/backend/internal/netbird"
	"malgary/backend/internal/secrets"
	"malgary/backend/internal/store"
	"malgary/backend/internal/store/db"
)

// Service implements httpapi.TenancyService.
type Service struct {
	Store          *store.Store
	Secrets        secrets.Resolver
	Identity       identity.Provider // nil disables Zitadel (local development)
	NetBird        *netbird.Factory
	Deployer       Deployer
	BaseDomain     string // vpn.malgary.com
	NetbirdVersion string
	StackDBHost    string // host.docker.internal
	// IsolationRequired keeps access policies off until an isolation test passed for the
	// current stack hash (interim host, feature 002 FR-009).
	IsolationRequired bool
	Billing           Billing // nil disables subscriptions (tests of provisioning only)
}

// Billing is what tenancy needs from internal/billing, inside the apply transaction.
type Billing interface {
	SubscribeTx(ctx context.Context, q *db.Queries, tenantID uuid.UUID, in gen.SubscriptionInput) error
	ChangeTx(ctx context.Context, q *db.Queries, tenantID uuid.UUID, in gen.SubscriptionChange) error
	Require(ctx context.Context, tenantID uuid.UUID, key string) error
}

var _ httpapi.TenancyService = (*Service)(nil)

// Apply creates a tenant from a definition, or updates an existing one. Provisioning of a new
// stack continues in the background; GET /v1/tenants/{id} shows progress.
func (s *Service) Apply(ctx context.Context, def gen.TenantDefinition) (gen.Tenant, bool, error) {
	if err := validateDefinition(def); err != nil {
		return gen.Tenant{}, false, err
	}
	slug := def.Metadata.Slug
	existing, err := s.Store.GetTenantBySlug(ctx, slug)
	switch {
	case err == nil:
		if err := s.applyDiff(ctx, existing, def); err != nil {
			return gen.Tenant{}, false, err
		}
		// A tenant whose first provisioning failed is retried by applying it again: every
		// step is idempotent. Active tenants are repaired by the reconcile loop instead.
		if existing.Status == db.TenantStatusProvisioning {
			go s.provision(context.WithoutCancel(ctx), existing.ID)
		}
		t, err := s.Get(ctx, existing.ID)
		return t, false, err
	case !store.IsNotFound(err):
		return gen.Tenant{}, false, err
	}

	var tenantID uuid.UUID
	err = s.Store.WithTx(ctx, func(q *db.Queries, _ pgxTx) error {
		cellCode := ""
		if def.Spec.Cell != nil {
			cellCode = *def.Spec.Cell
		}
		cell, err := pickCell(ctx, q, cellCode)
		if err != nil {
			return err
		}
		host, err := pickControlHost(ctx, q, cell.ID)
		if err != nil {
			return err
		}
		port, err := allocHTTPPort(ctx, q, host.ID)
		if err != nil {
			return err
		}
		t, err := q.CreateTenant(ctx, db.CreateTenantParams{ID: uuid.Must(uuid.NewV7()), Slug: slug, LegalName: def.Metadata.LegalName, CellID: cell.ID})
		if err != nil {
			return err
		}
		tenantID = t.ID
		if err := upsertIdentity(ctx, q, t.ID, def.Spec.Identity, nil, nil); err != nil {
			return err
		}
		if _, err := q.UpsertTenantStack(ctx, db.UpsertTenantStackParams{
			TenantID: t.ID, ControlHostID: host.ID, NetbirdVersion: s.NetbirdVersion,
			ManagementUrl: s.managementURL(slug), HttpPort: port, DbName: dbName(slug),
		}); err != nil {
			return err
		}
		for _, g := range def.Spec.Gateways {
			if err := s.placeRoutingPeers(ctx, q, t, g.Pair); err != nil {
				return err
			}
		}
		if err := storeRouting(ctx, q, t.ID, def); err != nil {
			return err
		}
		if err := storePosture(ctx, q, t.ID, postureFromSpec(def.Spec.Posture)); err != nil {
			return err
		}
		if s.Billing != nil {
			sub := def.Spec.Subscription
			if err := s.Billing.SubscribeTx(ctx, q, t.ID, gen.SubscriptionInput{
				PlanCode: sub.Plan, PlanVersion: sub.PlanVersion, Quantity: sub.Quantity, PaymentProvider: sub.PaymentProvider,
			}); err != nil {
				return err
			}
		}
		for _, name := range []string{groupAllUsers} {
			if _, err := q.CreateGroup(ctx, db.CreateGroupParams{ID: uuid.Must(uuid.NewV7()), TenantID: t.ID, Name: name, Kind: db.GroupKindSystem}); err != nil {
				return err
			}
		}
		return audit.Record(ctx, q, audit.Entry{
			TenantID: &t.ID, Action: "tenant.created", TargetType: "tenant", TargetID: t.ID.String(),
			After: map[string]any{"slug": slug, "cell": cell.Code, "controlHost": host.Name},
		})
	})
	if err != nil {
		return gen.Tenant{}, false, err
	}

	go s.provision(context.WithoutCancel(ctx), tenantID)
	t, err := s.Get(ctx, tenantID)
	return t, true, err
}

// placeRoutingPeers allocates one routing peer per member of the pair (primary metric 100,
// standby 200), refusing pairs whose egress IP isn't PTA-registered (FR-011).
func (s *Service) placeRoutingPeers(ctx context.Context, q *db.Queries, t db.Tenant, pairName string) error {
	pair, err := q.GetGatewayPairByName(ctx, pairName)
	if err != nil {
		return httpapi.Errf(http.StatusUnprocessableEntity, gen.InvalidRequest, "unknown gateway pair "+pairName)
	}
	if pair.CellID != t.CellID {
		return httpapi.Errf(http.StatusUnprocessableEntity, gen.InvalidRequest, "gateway pair "+pairName+" is not in the tenant's cell")
	}
	cell, err := q.GetCell(ctx, pair.CellID)
	if err != nil {
		return err
	}
	// The interim egress IP is temporary and never PTA-registered (Interim Hosting Exception).
	if pair.PtaRegisteredAt == nil && !cell.Interim {
		return httpapi.Errf(http.StatusUnprocessableEntity, gen.GatewayNotPtaRegistered,
			"egress IP of "+pairName+" is not PTA-registered yet (malgaryctl gateway mark-pta-registered)")
	}
	if pair.DedicatedTenantID != nil && *pair.DedicatedTenantID != t.ID {
		return httpapi.Errf(http.StatusUnprocessableEntity, gen.InvalidRequest, "gateway pair "+pairName+" is dedicated to another tenant")
	}
	members := map[uuid.UUID]int32{pair.PrimaryHostID: 100}
	if pair.StandbyHostID != nil {
		members[*pair.StandbyHostID] = 200
	}
	port, err := allocWGPort(ctx, q, slices.Collect(maps.Keys(members))...)
	if err != nil {
		return err
	}
	iface := wgInterfaceName(t.Slug)
	for host, metric := range members {
		if _, err := q.CreateRoutingPeer(ctx, db.CreateRoutingPeerParams{
			ID: uuid.Must(uuid.NewV7()), TenantID: t.ID, GatewayPairID: pair.ID, HostID: host,
			WgInterface: iface, WgPort: port, Metric: metric,
		}); err != nil {
			return err
		}
	}
	return nil
}

// SyncSignIn pushes the tenant's sign-in settings and Malgary branding to its Zitadel org
// (FR-002, FR-013, Principle X). Safe to repeat.
func (s *Service) SyncSignIn(ctx context.Context, tenantID uuid.UUID) error {
	if s.Identity == nil {
		return nil
	}
	link, err := s.Store.GetIdentityLink(ctx, tenantID)
	if err != nil {
		return err
	}
	if link.ZitadelOrgID == nil {
		return nil // org not created yet; provisioning calls this again
	}
	cfg, err := SignInConfigFrom(link)
	if err != nil {
		return err
	}
	if err := s.Identity.ConfigureSignIn(ctx, *link.ZitadelOrgID, cfg); err != nil {
		return err
	}
	return s.Identity.ApplyBranding(ctx, *link.ZitadelOrgID)
}

// SignInConfigFrom converts the stored identity link to the identity package's config.
func SignInConfigFrom(link db.IdentityLink) (identity.SignInConfig, error) {
	var idps []gen.UpstreamIdp
	if err := json.Unmarshal(link.UpstreamIdps, &idps); err != nil {
		return identity.SignInConfig{}, err
	}
	cfg := identity.SignInConfig{
		SSO:         slices.Contains(link.SignInMethods, "sso"),
		Managed:     slices.Contains(link.SignInMethods, "managed"),
		MFARequired: link.MfaRequired,
	}
	for _, i := range idps {
		cfg.Upstreams = append(cfg.Upstreams, string(i.Type))
	}
	return cfg, nil
}

// provision runs the slow steps for a new tenant: sign-in org, stack deploy, bootstrap, then
// routing peers. Each step is safe to repeat, so a failed run is retried by the reconciler.
func (s *Service) provision(ctx context.Context, tenantID uuid.UUID) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Minute)
	defer cancel()
	log := slog.With("tenant", tenantID)
	if err := s.provisionSteps(ctx, tenantID); err != nil {
		log.ErrorContext(ctx, "provisioning failed", "err", err)
		msg, _ := json.Marshal(map[string]string{"error": err.Error()})
		_, _ = s.Store.ReportTenantStack(ctx, db.ReportTenantStackParams{TenantID: tenantID, Status: db.StackStatusFailed, LastReport: msg})
		return
	}
	log.InfoContext(ctx, "tenant provisioned")
}

func (s *Service) provisionSteps(ctx context.Context, tenantID uuid.UUID) error {
	t, err := s.Store.GetTenant(ctx, tenantID)
	if err != nil {
		return err
	}
	stack, err := s.Store.GetTenantStack(ctx, tenantID)
	if err != nil {
		return err
	}
	if !s.Secrets.Exists(refDBPassword(t.Slug)) {
		pw, err := randomSecret(24)
		if err != nil {
			return err
		}
		if err := s.Secrets.Put(refDBPassword(t.Slug), []byte(pw)); err != nil {
			return err
		}
	}

	app, err := s.ensureSignInOrg(ctx, t, stack)
	if err != nil {
		return err
	}
	if err := s.SyncSignIn(ctx, t.ID); err != nil {
		return err
	}

	// Pass 1: the stack alone (no routing peers until the gateway setup key exists).
	if err := s.deploy(ctx, t, stack, false); err != nil {
		return err
	}
	issuer := ""
	if s.Identity != nil {
		issuer = s.Identity.Issuer()
	}
	if err := (bootstrapper{nb: s.NetBird, secrets: s.Secrets}).run(ctx, t.Slug, stack.ManagementUrl, app, issuer); err != nil {
		return fmt.Errorf("bootstrap: %w", err)
	}
	ref := refNetbirdPAT(t.Slug)
	if err := s.Store.SetTenantStackToken(ctx, db.SetTenantStackTokenParams{TenantID: t.ID, ServiceTokenRef: &ref}); err != nil {
		return err
	}
	// Pass 2: stack plus routing peers.
	if err := s.deploy(ctx, t, stack, true); err != nil {
		return err
	}
	if err := s.applyRouting(ctx, t, stack.ManagementUrl); err != nil {
		return fmt.Errorf("routing: %w", err)
	}
	if err := s.applyAccess(ctx, t); err != nil {
		return fmt.Errorf("access policies: %w", err)
	}
	return s.Store.WithTx(ctx, func(q *db.Queries, _ pgxTx) error {
		if _, err := q.SetTenantStatus(ctx, db.SetTenantStatusParams{ID: t.ID, Status: db.TenantStatusActive}); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{TenantID: &t.ID, Action: "tenant.activated", TargetType: "tenant", TargetID: t.ID.String()})
	})
}

// ensureSignInOrg creates the tenant's Zitadel organization and OIDC app once.
func (s *Service) ensureSignInOrg(ctx context.Context, t db.Tenant, stack db.TenantStack) (identity.TenantApp, error) {
	link, err := s.Store.GetIdentityLink(ctx, t.ID)
	if err != nil {
		return identity.TenantApp{}, err
	}
	if link.ZitadelOrgID != nil && link.ZitadelAppClientID != nil {
		return identity.TenantApp{OrgID: *link.ZitadelOrgID, ClientID: *link.ZitadelAppClientID}, nil
	}
	if s.Identity == nil {
		return identity.TenantApp{}, nil
	}
	app, err := s.Identity.EnsureTenantOrg(ctx, t.Slug, t.LegalName, stack.ManagementUrl)
	if err != nil {
		return identity.TenantApp{}, err
	}
	if err := s.Secrets.Put(refOIDCSecret(t.Slug), []byte(app.ClientSecret)); err != nil {
		return identity.TenantApp{}, err
	}
	app.ClientSecret = ""
	_, err = s.Store.UpsertIdentityLink(ctx, db.UpsertIdentityLinkParams{
		TenantID: t.ID, ZitadelOrgID: &app.OrgID, ZitadelAppClientID: &app.ClientID,
		UpstreamIdps: link.UpstreamIdps, AllowedDomains: link.AllowedDomains, AllowedEmails: link.AllowedEmails,
		SignInMethods: link.SignInMethods, MfaRequired: link.MfaRequired,
	})
	return app, err
}

// deploy renders the StackSpec, stores its hash as desired, runs the deployer and records the
// result.
func (s *Service) deploy(ctx context.Context, t db.Tenant, stack db.TenantStack, withPeers bool) error {
	spec, err := s.render(ctx, t, stack, withPeers)
	if err != nil {
		return err
	}
	sealed, doc, err := spec.Seal()
	if err != nil {
		return err
	}
	if _, err := s.Store.UpsertTenantStack(ctx, db.UpsertTenantStackParams{
		TenantID: t.ID, ControlHostID: stack.ControlHostID, NetbirdVersion: stack.NetbirdVersion,
		ManagementUrl: stack.ManagementUrl, HttpPort: stack.HttpPort, DbName: stack.DbName,
		ServiceTokenRef: stack.ServiceTokenRef, DesiredHash: &sealed.Hash,
	}); err != nil {
		return err
	}
	if err := s.Deployer.Deploy(ctx, sealed, doc); err != nil {
		return err
	}
	rep, _ := json.Marshal(map[string]string{"phase": map[bool]string{false: "stack", true: "stack+peers"}[withPeers]})
	_, err = s.Store.ReportTenantStack(ctx, db.ReportTenantStackParams{TenantID: t.ID, AppliedHash: &sealed.Hash, Status: db.StackStatusApplied, LastReport: rep})
	return err
}

func (s *Service) render(ctx context.Context, t db.Tenant, stack db.TenantStack, withPeers bool) (StackSpec, error) {
	host, err := s.Store.GetHost(ctx, stack.ControlHostID)
	if err != nil {
		return StackSpec{}, err
	}
	spec := StackSpec{
		Tenant:           t.Slug,
		NetbirdVersion:   stack.NetbirdVersion,
		ControlHost:      host.Name,
		ManagementDomain: strings.TrimPrefix(stack.ManagementUrl, "https://"),
		HTTPPort:         int(stack.HttpPort),
		Database:         SpecDatabase{Host: s.StackDBHost, Name: stack.DbName, User: stack.DbName, PasswordRef: refDBPassword(t.Slug)},
		Relay:            SpecRelay{Mode: "embedded"},
	}
	if withPeers {
		peers, err := s.Store.ListRoutingPeersByTenant(ctx, t.ID)
		if err != nil {
			return StackSpec{}, err
		}
		for _, p := range peers {
			spec.RoutingPeers = append(spec.RoutingPeers, RoutingPeer{
				Host: p.HostName, WGInterface: p.WgInterface, WGPort: int(p.WgPort), Metric: int(p.Metric),
				SetupKeyRef: refGatewaySetupKey(t.Slug),
			})
		}
	}
	return spec, nil
}

// Get returns the API view of a tenant.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (gen.Tenant, error) {
	t, err := s.Store.GetTenant(ctx, id)
	if store.IsNotFound(err) {
		return gen.Tenant{}, httpapi.ErrNotFound
	}
	if err != nil {
		return gen.Tenant{}, err
	}
	return s.view(ctx, t)
}

// List returns every non-deleted tenant.
func (s *Service) List(ctx context.Context) ([]gen.Tenant, error) {
	ts, err := s.Store.ListTenants(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]gen.Tenant, 0, len(ts))
	for _, t := range ts {
		v, err := s.view(ctx, t)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// Report records a deployer report for deployers that run outside malgaryd.
func (s *Service) Report(ctx context.Context, id uuid.UUID, rep gen.StackReport) error {
	body, _ := json.Marshal(rep)
	_, err := s.Store.ReportTenantStack(ctx, db.ReportTenantStackParams{
		TenantID: id, AppliedHash: &rep.AppliedHash, Status: db.StackStatus(rep.Status), LastReport: body,
	})
	if store.IsNotFound(err) {
		return httpapi.ErrNotFound
	}
	return err
}

func (s *Service) view(ctx context.Context, t db.Tenant) (gen.Tenant, error) {
	cell, err := s.Store.GetCell(ctx, t.CellID)
	if err != nil {
		return gen.Tenant{}, err
	}
	out := gen.Tenant{
		Id: t.ID, Slug: t.Slug, LegalName: t.LegalName, Status: gen.TenantStatus(t.Status),
		Cell: cell.Code, CreatedAt: &t.CreatedAt, StackStatus: gen.TenantStackStatusPending,
	}
	if st, err := s.Store.GetTenantStack(ctx, t.ID); err == nil {
		out.ManagementUrl = st.ManagementUrl
		setup := st.ManagementUrl + "/setup"
		out.SetupUrl = &setup
		out.StackStatus = gen.TenantStackStatus(st.Status)
		v := st.NetbirdVersion
		out.NetbirdVersion = &v
	}
	return out, nil
}

func (s *Service) managementURL(slug string) string { return "https://" + slug + "." + s.BaseDomain }

func dbName(slug string) string { return "nb_" + strings.ReplaceAll(slug, "-", "_") }

// validateDefinition enforces contracts/tenant-definition.md rules that the JSON schema can't.
func validateDefinition(def gen.TenantDefinition) error {
	bad := func(msg string) error { return httpapi.Errf(http.StatusUnprocessableEntity, gen.InvalidRequest, msg) }
	id := def.Spec.Identity
	if len(id.SignInMethods) == 0 {
		return bad("identity.signInMethods must not be empty")
	}
	if slices.Contains(id.SignInMethods, gen.Sso) && (id.UpstreamIdps == nil || len(*id.UpstreamIdps) == 0) {
		return bad("identity.signInMethods includes sso, so identity.upstreamIdps needs at least one entry")
	}
	if (id.AllowedDomains == nil || len(*id.AllowedDomains) == 0) && (id.AllowedEmails == nil || len(*id.AllowedEmails) == 0) {
		return bad("identity.allowedDomains or identity.allowedEmails must not be empty")
	}
	if len(def.Spec.Gateways) == 0 {
		return bad("spec.gateways needs at least one pair")
	}
	if r := def.Spec.Routing.Default; r.Mode == gen.RoutingTargetModeDestinations && (r.Destinations == nil || len(*r.Destinations) == 0) {
		return httpapi.Errf(http.StatusUnprocessableEntity, gen.RoutingRuleEmptyDestinations, "routing.default has mode destinations but no destinations")
	}
	return nil
}

func upsertIdentity(ctx context.Context, q *db.Queries, tenantID uuid.UUID, id gen.Identity, orgID, clientID *string) error {
	idps, err := json.Marshal(deref(id.UpstreamIdps))
	if err != nil {
		return err
	}
	methods := make([]string, 0, len(id.SignInMethods))
	for _, m := range id.SignInMethods {
		methods = append(methods, string(m))
	}
	mfa := true
	if id.MfaRequired != nil {
		mfa = *id.MfaRequired
	}
	if slices.Contains(methods, "managed") {
		mfa = true // managed accounts always require MFA (Principle VII)
	}
	_, err = q.UpsertIdentityLink(ctx, db.UpsertIdentityLinkParams{
		TenantID: tenantID, ZitadelOrgID: orgID, ZitadelAppClientID: clientID, UpstreamIdps: idps,
		AllowedDomains: deref(id.AllowedDomains), AllowedEmails: emails(id.AllowedEmails),
		SignInMethods: methods, MfaRequired: mfa,
	})
	return err
}

func identityUnchanged(cur db.IdentityLink, id gen.Identity) bool {
	idps, _ := json.Marshal(deref(id.UpstreamIdps))
	methods := make([]string, 0, len(id.SignInMethods))
	for _, m := range id.SignInMethods {
		methods = append(methods, string(m))
	}
	mfa := id.MfaRequired == nil || *id.MfaRequired || slices.Contains(methods, "managed")
	return sameJSON(cur.UpstreamIdps, idps) &&
		slices.Equal(cur.AllowedDomains, deref(id.AllowedDomains)) &&
		slices.Equal(cur.AllowedEmails, emails(id.AllowedEmails)) &&
		slices.Equal(cur.SignInMethods, methods) && cur.MfaRequired == mfa
}

// sameJSON compares two JSON documents semantically (jsonb reorders keys).
func sameJSON(a, b []byte) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}

func deref[T any](p *[]T) []T {
	if p == nil {
		return []T{}
	}
	return *p
}

func emails[T ~string](p *[]T) []string {
	out := []string{}
	if p != nil {
		for _, e := range *p {
			out = append(out, strings.ToLower(string(e)))
		}
	}
	return out
}
