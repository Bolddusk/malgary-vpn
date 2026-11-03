//go:build integration

package integration

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"malgary/backend/internal/config"
	"malgary/backend/internal/httpapi"
	"malgary/backend/internal/httpapi/gen"
	"malgary/backend/internal/interim"
	"malgary/backend/internal/netbird"
	"malgary/backend/internal/secrets"
	"malgary/backend/internal/store/db"
	"malgary/backend/internal/tenancy"
	"malgary/backend/internal/users"
)

// The interim host: one machine, one gateway without a standby, egress IP not PTA-registered.
const interimInventory = `code: i01
region: nbg1
maxUsers: 10
interim: true
hosts:
  - {name: ctl.i01, role: control, size: shared, maxUsers: 10}
  - {name: gw.i01, role: gateway, size: shared}
gatewayPairs:
  - {name: gw-i01, egressIp: 178.104.48.140, primary: gw.i01}
`

// TestInterimLimits covers feature 002 quickstart V7 and V9 (FR-010, FR-011, FR-015) and the
// append-only result tables.
func TestInterimLimits(t *testing.T) {
	ctx := context.Background()
	st := startPostgres(t)
	dir := t.TempDir()
	inv := filepath.Join(dir, "i01.yaml")
	_ = os.WriteFile(inv, []byte(interimInventory), 0o600)
	if err := tenancy.SyncInventory(ctx, st, inv); err != nil {
		t.Fatal("interim inventory:", err)
	}
	bad := filepath.Join(dir, "c09.yaml")
	_ = os.WriteFile(bad, []byte(strings.NewReplacer("i01", "c09", "interim: true\n", "", "178.104.48.140", "203.0.113.9").Replace(interimInventory)), 0o600)
	if err := tenancy.SyncInventory(ctx, st, bad); err == nil {
		t.Fatal("a dedicated cell without a standby gateway must be refused (Principle IV)")
	}

	_, nbSrv := newFakeNetBird(t)
	ten := &tenancy.Service{Store: st, Secrets: secrets.Resolver{Dir: filepath.Join(dir, "s")}, NetBird: netbird.NewFactory(100),
		Deployer: &recordingDeployer{}, BaseDomain: strings.TrimPrefix(nbSrv.URL, "http://"), NetbirdVersion: "0.82.0"}
	def := onedgeDefinition()
	cell := "i01"
	def.Spec.Cell = &cell
	def.Spec.Gateways[0].Pair = "gw-i01"
	domains := []string{"onedge.co", "gmail.com"}
	def.Spec.Identity.AllowedDomains = &domains
	tn, _, err := ten.Apply(ctx, def)
	if err != nil {
		t.Fatal("apply on the interim cell (no PTA registration needed):", err)
	}

	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	limits := &interim.Limits{Config: config.Interim{
		Active: true, MaxUsers: 10, Until: time.Date(2027, 2, 28, 0, 0, 0, 0, time.UTC),
		TestTenants: []string{"onedge", "testco"}, TestDomains: []string{"gmail.com"}, PaidTenants: []string{"onedge"},
	}, Now: func() time.Time { return now }}
	us := &users.Service{Store: st, Interim: limits}
	svc := &interim.Service{Limits: limits, Store: st}

	wantCode := func(err error, c gen.ProblemCode) {
		t.Helper()
		var ae *httpapi.APIError
		if !errors.As(err, &ae) || ae.Code != c {
			t.Fatalf("want %s, got %v", c, err)
		}
	}

	// V9: a real company address is refused while no restore test has passed.
	_, err = us.Import(ctx, tn.Id, []gen.UserImport{{Email: "ali@onedge.co"}})
	wantCode(err, gen.RestoreTestRequired)

	// Test users fill the 10 places; the 11th is refused, and nothing of a refused batch stays.
	var batch []gen.UserImport
	for i := 0; i < 10; i++ {
		batch = append(batch, gen.UserImport{Email: openapi_types.Email(fmt.Sprintf("tester%d@gmail.com", i))})
	}
	if res, err := us.Import(ctx, tn.Id, batch); err != nil || res.Created != 10 {
		t.Fatalf("import 10 test users: %+v %v", res, err)
	}
	_, err = us.Import(ctx, tn.Id, []gen.UserImport{{Email: "eleventh@gmail.com"}})
	wantCode(err, gen.InterimUserLimit)
	if list, _ := us.List(ctx, tn.Id); len(list) != 10 {
		t.Fatalf("refused import must roll back, have %d users", len(list))
	}

	// After a passed restore test real users are allowed (still within the limit).
	if err := svc.RecordRestoreTest(ctx, gen.RestoreTestInput{BackupKey: "k", BackupTakenAt: now, Passed: true, Details: map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Pool.Exec(ctx, "UPDATE tenant_user SET seat_held = false WHERE email = 'tester0@gmail.com'"); err != nil {
		t.Fatal(err)
	}
	if _, err := us.Import(ctx, tn.Id, []gen.UserImport{{Email: "ali@onedge.co"}}); err != nil {
		t.Fatal("real user after restore test:", err)
	}

	// FR-011: no new users after the end date.
	now = time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC)
	_, err = us.Import(ctx, tn.Id, []gen.UserImport{{Email: "late@gmail.com"}})
	wantCode(err, gen.InterimExpired)

	// FR-009 records: the server recomputes "passed" from the probes.
	res, err := svc.RecordIsolationTest(ctx, tn.Id, gen.IsolationTestInput{StackHash: "sha256:abcdef01", Passed: true, Results: []gen.IsolationProbe{
		{Target: "178.104.48.140:5432", Expected: gen.IsolationProbeExpectedBlocked, Got: gen.IsolationProbeGotReachable},
	}}, "operator-1")
	if err != nil || res.Passed {
		t.Fatalf("a reachable private target must fail the test: %+v %v", res, err)
	}
	if ok, _ := st.IsolationPassedForHash(ctx, db.IsolationPassedForHashParams{TenantID: tn.Id, StackHash: "sha256:abcdef01"}); ok {
		t.Fatal("hash must not count as passed")
	}
	ip := "178.104.48.140"
	if _, err := svc.RecordIsolationTest(ctx, tn.Id, gen.IsolationTestInput{StackHash: "sha256:abcdef01", Passed: true, EgressIp: &ip, Results: []gen.IsolationProbe{
		{Target: "178.104.48.140:5432", Expected: gen.IsolationProbeExpectedBlocked, Got: gen.IsolationProbeGotBlocked},
		{Target: "https://1.1.1.1", Expected: gen.IsolationProbeExpectedReachable, Got: gen.IsolationProbeGotReachable},
	}}, "operator-1"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := st.IsolationPassedForHash(ctx, db.IsolationPassedForHashParams{TenantID: tn.Id, StackHash: "sha256:abcdef01"}); !ok {
		t.Fatal("latest result passed: hash must count as passed")
	}
	if ok, _ := st.IsolationPassedForHash(ctx, db.IsolationPassedForHashParams{TenantID: tn.Id, StackHash: "sha256:other"}); ok {
		t.Fatal("a new stack hash needs its own pass")
	}

	// Append-only for the application role.
	for _, q := range []string{"UPDATE isolation_test_result SET passed = true", "DELETE FROM backup_restore_test"} {
		if _, err := st.Pool.Exec(ctx, q); err == nil {
			t.Fatalf("%q must be refused for malgary_app", q)
		}
	}
}
