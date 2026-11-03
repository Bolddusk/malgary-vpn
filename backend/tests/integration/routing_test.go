//go:build integration

package integration

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"malgary/backend/internal/httpapi/gen"
	"malgary/backend/internal/netbird"
	"malgary/backend/internal/secrets"
	"malgary/backend/internal/store/db"
	"malgary/backend/internal/tenancy"
	"malgary/backend/internal/users"
)

// TestRoutingRules covers quickstart V6 at the API layer (US5, FR-005).
func TestRoutingRules(t *testing.T) {
	ctx := context.Background()
	st := startPostgres(t)
	dir := t.TempDir()
	inv := filepath.Join(dir, "c01.yaml")
	_ = os.WriteFile(inv, []byte(inventory), 0o600)
	if err := tenancy.SyncInventory(ctx, st, inv); err != nil {
		t.Fatal(err)
	}
	now, ref := time.Now(), "PTA"
	_, _ = st.MarkGatewayPairPTARegistered(ctx, db.MarkGatewayPairPTARegisteredParams{Name: "gw-nyc-1", PtaRegisteredAt: &now, PtaReference: &ref})
	_, nb := newFakeNetBird(t)
	ten := &tenancy.Service{Store: st, Secrets: secrets.Resolver{Dir: filepath.Join(dir, "s")}, NetBird: netbird.NewFactory(100),
		Deployer: &recordingDeployer{}, BaseDomain: strings.TrimPrefix(nb.URL, "http://"), NetbirdVersion: "0.82.0"}
	tn, _, err := ten.Apply(ctx, onedgeDefinition())
	if err != nil {
		t.Fatal(err)
	}
	us := &users.Service{Store: st}
	if _, err := us.Import(ctx, tn.Id, []gen.UserImport{{Email: "a@onedge.co"}, {Email: "b@onedge.co"}, {Email: "c@onedge.co"}}); err != nil {
		t.Fatal(err)
	}
	b := []openapi_types.Email{"b@onedge.co", "c@onedge.co"}
	name := "client-x"
	grp, err := us.CreateGroup(ctx, tn.Id, gen.GroupInput{Name: &name, MemberEmails: &b})
	if err != nil {
		t.Fatal(err)
	}
	all, _ := us.List(ctx, tn.Id)
	var userC uuid.UUID
	for _, u := range all {
		if u.Email == "c@onedge.co" {
			userC = u.Id
		}
	}

	dest := []string{"198.51.100.0/24"}
	rules := []gen.RoutingRule{
		{Scope: gen.RoutingRuleScopeTenant, Mode: gen.RoutingRuleModeFullTunnel},
		{Scope: gen.RoutingRuleScopeGroup, ScopeRef: &grp.Id, Mode: gen.RoutingRuleModeDestinations, Destinations: &dest},
		{Scope: gen.RoutingRuleScopeUser, ScopeRef: &userC, Mode: gen.RoutingRuleModeFullTunnel},
	}
	rr, err := ten.PutRoutingRules(ctx, tn.Id, rules, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[uuid.UUID]string{}
	for _, e := range rr.Effective {
		got[e.UserId] = string(e.Mode) + "/" + string(*e.Source)
	}
	for _, u := range all {
		want := map[string]string{"a@onedge.co": "full_tunnel/tenant", "b@onedge.co": "destinations/group", "c@onedge.co": "full_tunnel/user"}[u.Email]
		if got[u.Id] != want {
			t.Errorf("%s: effective %s, want %s", u.Email, got[u.Id], want)
		}
	}
	// Each user is in exactly one routing group.
	all, _ = us.List(ctx, tn.Id)
	for _, u := range all {
		n := 0
		for _, g := range *u.Groups {
			if strings.HasPrefix(g, "route:") {
				n++
			}
		}
		if n != 1 {
			t.Errorf("%s is in %d routing groups: %v", u.Email, n, *u.Groups)
		}
	}
	// Empty destinations are rejected (edge case).
	empty := []string{}
	if _, err := ten.PutRoutingRules(ctx, tn.Id, []gen.RoutingRule{{Scope: gen.RoutingRuleScopeTenant, Mode: gen.RoutingRuleModeDestinations, Destinations: &empty}}, nil); err == nil {
		t.Fatal("empty destinations must be rejected")
	}
	// A scopeRef from another tenant is rejected (Principle I).
	foreign := uuid.New()
	if _, err := ten.PutRoutingRules(ctx, tn.Id, []gen.RoutingRule{rules[0], {Scope: gen.RoutingRuleScopeUser, ScopeRef: &foreign, Mode: gen.RoutingRuleModeFullTunnel}}, nil); err == nil {
		t.Fatal("foreign scopeRef must be rejected")
	}
	var audits int
	_ = st.Pool.QueryRow(ctx, "SELECT count(*) FROM audit_record WHERE action = 'routing_rule.updated'").Scan(&audits)
	if audits != 1 {
		t.Fatalf("expected 1 routing audit record, got %d", audits)
	}
}
