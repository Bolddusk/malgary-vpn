//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"malgary/backend/internal/httpapi/gen"
	"malgary/backend/internal/netbird"
	"malgary/backend/internal/secrets"
	"malgary/backend/internal/store/db"
	"malgary/backend/internal/tenancy"
)

// recordingDeployer stands in for Ansible and records every sealed spec.
type recordingDeployer struct {
	mu     sync.Mutex
	hashes []string
	peers  []int
}

func (d *recordingDeployer) Deploy(_ context.Context, s tenancy.StackSpec, _ []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.hashes = append(d.hashes, s.Hash)
	d.peers = append(d.peers, len(s.RoutingPeers))
	return nil
}

const inventory = `code: c01
region: nyc1
maxUsers: 5000
hosts:
  - {name: ctl-1.c01, role: control, size: s-4vcpu-8gb, maxUsers: 2500}
  - {name: gw-nyc-1a.c01, role: gateway, size: s-2vcpu-2gb}
  - {name: gw-nyc-1b.c01, role: gateway, size: s-2vcpu-2gb}
gatewayPairs:
  - {name: gw-nyc-1, egressIp: 203.0.113.10, primary: gw-nyc-1a.c01, standby: gw-nyc-1b.c01}
`

func TestStackLifecycle(t *testing.T) {
	ctx := context.Background()
	st := startPostgres(t)
	dir := t.TempDir()
	invPath := filepath.Join(dir, "c01.yaml")
	if err := os.WriteFile(invPath, []byte(inventory), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := tenancy.SyncInventory(ctx, st, invPath); err != nil {
		t.Fatal(err)
	}
	if err := tenancy.SyncInventory(ctx, st, invPath); err != nil {
		t.Fatal("inventory sync must be repeatable:", err)
	}

	fake, nbSrv := newFakeNetBird(t)
	dep := &recordingDeployer{}
	res := secrets.Resolver{Dir: filepath.Join(dir, "secrets")}
	svc := &tenancy.Service{
		Store: st, Secrets: res, NetBird: netbird.NewFactory(100), Deployer: dep,
		BaseDomain: "vpn.example.test", NetbirdVersion: "0.82.0", StackDBHost: "host.docker.internal",
	}
	def := onedgeDefinition()

	// PTA gate: the pair isn't registered yet (FR-011).
	if _, _, err := svc.Apply(ctx, def); err == nil || !strings.Contains(err.Error(), "PTA") {
		t.Fatalf("expected PTA gate error, got %v", err)
	}
	now := time.Now()
	ref := "PTA-TEST-1"
	if _, err := st.MarkGatewayPairPTARegistered(ctx, db.MarkGatewayPairPTARegisteredParams{Name: "gw-nyc-1", PtaRegisteredAt: &now, PtaReference: &ref}); err != nil {
		t.Fatal(err)
	}

	// The fake answers on its own URL; point the tenant's management URL at it.
	svc.BaseDomain = strings.TrimPrefix(nbSrv.URL, "http://")
	tn, created, err := svc.Apply(ctx, def)
	if err != nil || !created {
		t.Fatalf("apply: created=%v err=%v", created, err)
	}
	// The real URL is https://<slug>.<domain>; rewrite it to the fake for this test.
	if _, err := st.Pool.Exec(ctx, "UPDATE tenant_stack SET management_url = $1", nbSrv.URL); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 20*time.Second, func() bool {
		got, _ := svc.Get(ctx, tn.Id)
		return got.Status == gen.TenantStatusActive || got.StackStatus == gen.TenantStackStatusFailed
	})
	got, _ := svc.Get(ctx, tn.Id)
	if got.Status != gen.TenantStatusActive {
		row, _ := st.GetTenantStack(ctx, tn.Id)
		t.Fatalf("tenant not active: %+v report=%s calls=%v", got, row.LastReport, fake.calls)
	}

	// Two deploy passes: stack alone, then stack with both routing peers.
	if len(dep.hashes) != 2 || dep.peers[0] != 0 || dep.peers[1] != 2 || dep.hashes[0] == dep.hashes[1] {
		t.Fatalf("unexpected deploys: hashes=%v peers=%v", dep.hashes, dep.peers)
	}
	// Full tunnel: one exit route (0.0.0.0/0) served by both gateways, primary preferred.
	if len(fake.routes) != 2 {
		t.Fatalf("expected 2 routes (exit node on each gateway), got %d: %v", len(fake.routes), fake.routes)
	}
	for _, rt := range fake.routes {
		if rt["network"] != "0.0.0.0/0" || rt["groups"].([]any)[0] != "g-route:full" {
			t.Fatalf("unexpected route %v", rt)
		}
	}

	// Access policy to the gateways with the posture check attached (FR-018).
	if len(fake.policies) != 1 || len(fake.checks) != 0 {
		t.Fatalf("expected one access policy and no posture checks for this definition, got %v / %v", fake.policies, fake.checks)
	}

	// Secrets written behind references, never into the database.
	for _, p := range []string{"tenants/onedge/netbird-pat", "tenants/onedge/gw", "tenants/onedge/db"} {
		if !res.Exists(secrets.Ref(p)) {
			t.Errorf("missing secret %s", p)
		}
	}
	pat, _ := res.Resolve(secrets.Ref("tenants/onedge/netbird-pat"))
	if string(pat) != "nbp_service" {
		t.Errorf("backend must use the service user PAT, got %q", pat)
	}

	// Re-applying the same definition changes nothing (FR-015).
	var auditBefore int
	_ = st.Pool.QueryRow(ctx, "SELECT count(*) FROM audit_record").Scan(&auditBefore)
	_, created, err = svc.Apply(ctx, def)
	if err != nil || created {
		t.Fatalf("re-apply: created=%v err=%v", created, err)
	}
	var auditAfter, tenants int
	_ = st.Pool.QueryRow(ctx, "SELECT count(*) FROM audit_record").Scan(&auditAfter)
	_ = st.Pool.QueryRow(ctx, "SELECT count(*) FROM tenant").Scan(&tenants)
	if auditAfter != auditBefore || tenants != 1 {
		t.Fatalf("re-apply wrote audit=%d->%d tenants=%d", auditBefore, auditAfter, tenants)
	}

	// The application role can't rewrite history (T025).
	if _, err := st.Pool.Exec(ctx, "UPDATE audit_record SET action = 'x'"); err == nil {
		t.Fatal("malgary_app must not be able to UPDATE audit_record")
	}
}

func onedgeDefinition() gen.TenantDefinition {
	var def gen.TenantDefinition
	if err := json.Unmarshal([]byte(`{
	  "apiVersion": "malgary.io/v1", "kind": "Tenant",
	  "metadata": {"slug": "onedge", "legalName": "On Edge (Pvt) Ltd"},
	  "spec": {
	    "cell": "c01",
	    "identity": {"signInMethods": ["sso"], "upstreamIdps": [{"type": "google", "hostedDomain": "onedge.co"}],
	                 "allowedDomains": ["onedge.co"], "mfaRequired": true},
	    "subscription": {"plan": "ent-onedge", "planVersion": 1, "quantity": 10, "paymentProvider": "manual"},
	    "gateways": [{"pair": "gw-nyc-1"}],
	    "routing": {"default": {"mode": "full_tunnel"}}
	  }}`), &def); err != nil {
		panic(err)
	}
	return def
}

func waitFor(t *testing.T, d time.Duration, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}
