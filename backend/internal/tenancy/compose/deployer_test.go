package compose

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"malgary/backend/internal/secrets"
)

type fakeRunner struct{ calls []string }

func (f *fakeRunner) Compose(_ context.Context, _, project string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, project+" "+strings.Join(args, " "))
	return nil, nil
}

// newTestDeployer returns a deployer whose database step is skipped by deploying through
// deployFiles directly (the provisioner needs a real PostgreSQL; see integration tests).
func newTestDeployer(t *testing.T) (*Deployer, *fakeRunner) {
	t.Helper()
	dir := t.TempDir()
	r := &fakeRunner{}
	res := secrets.Resolver{Dir: filepath.Join(dir, "secrets")}
	_ = res.Put("secret://tenants/testco/db", []byte("dbpw"))
	_ = res.Put("secret://tenants/testco/gw", []byte("setup-key\n"))
	return &Deployer{
		StackDir: filepath.Join(dir, "stacks"), Host: testHost, Memory: DefaultMemory, Secrets: res,
		DashboardVersion: "v2.40.0", GuardImage: "malgary/gw-guard:local", Runner: r,
		Health: func(context.Context, string) error { return nil },
	}, r
}

func TestSecretsGeneratedOnce(t *testing.T) {
	d, _ := newTestDeployer(t)
	a, err := d.secretsFor(testSpec(true))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := d.secretsFor(testSpec(true))
	if a != b || len(a.RelayAuth) != 48 || a.SetupKey != "setup-key" || a.DBPassword != "dbpw" {
		t.Fatalf("secrets: %+v vs %+v", a, b)
	}
	info, err := os.Stat(filepath.Join(d.Secrets.Dir, "tenants", "testco", "relay-auth"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("relay-auth secret mode: %v %v", info, err)
	}
}

func TestWriteSkipAndRollback(t *testing.T) {
	d, _ := newTestDeployer(t)
	sec, _ := d.secretsFor(testSpec(true))
	files, err := Render(testSpec(true), d.Host, d.Memory, sec, d.DashboardVersion, d.GuardImage)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(d.StackDir, "testco")
	if unchanged(dir, files) {
		t.Fatal("nothing written yet")
	}
	hadPrev, err := writeFiles(dir, files)
	if err != nil || hadPrev {
		t.Fatalf("first write: %v %v", hadPrev, err)
	}
	if !unchanged(dir, files) {
		t.Fatal("same render must count as unchanged (obligation 1)")
	}
	info, _ := os.Stat(filepath.Join(dir, "config.yaml"))
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config.yaml mode %v", info.Mode().Perm())
	}

	newer := testSpec(true)
	newer.NetbirdVersion = "0.83.0"
	sealed, _, _ := newer.Seal()
	files2, _ := Render(sealed, d.Host, d.Memory, sec, d.DashboardVersion, d.GuardImage)
	if hadPrev, err := writeFiles(dir, files2); err != nil || !hadPrev {
		t.Fatalf("second write: %v %v", hadPrev, err)
	}
	if err := restorePrev(dir, files2); err != nil {
		t.Fatal(err)
	}
	if !unchanged(dir, files) {
		t.Fatal("rollback must restore the previous files")
	}
}

func TestUpRetriesHealth(t *testing.T) {
	d, r := newTestDeployer(t)
	n := 0
	d.Health = func(context.Context, string) error {
		n++
		if n < 2 {
			return errors.New("not yet")
		}
		return nil
	}
	if err := d.up(context.Background(), t.TempDir(), "nb-testco", "testco", false); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 1 || r.calls[0] != "nb-testco up -d --remove-orphans" {
		t.Fatalf("calls: %v", r.calls)
	}
	r.calls = nil
	if err := d.up(context.Background(), t.TempDir(), "nb-testco", "testco", true); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 2 || r.calls[1] != "nb-testco up -d --no-deps --force-recreate netbird-server" {
		t.Fatalf("a changed config.yaml must recreate the server: %v", r.calls)
	}
}

func TestBudget(t *testing.T) {
	d, _ := newTestDeployer(t)
	d.BudgetMB, d.PlatformMB = 3072, 1408
	files, _ := Render(testSpec(true), d.Host, d.Memory, testSecrets, "v", "g")
	compose := files["compose.yaml"].Body
	per := MemoryOf(compose) // 464 MB with dashboard and peer
	for i, slug := range []string{"aaa", "bbb", "ccc"} {
		if err := d.checkBudget("testco", compose); err != nil {
			t.Fatalf("with %d other stacks (%d MB each): %v", i, per, err)
		}
		_ = os.MkdirAll(filepath.Join(d.StackDir, slug), 0o750)
		_ = os.WriteFile(filepath.Join(d.StackDir, slug, "compose.yaml"), compose, 0o600)
	}
	if err := d.checkBudget("testco", compose); err == nil {
		t.Fatalf("a 4th full stack (%d MB total) must exceed 3072 MB", 1408+4*per)
	}
	if err := d.checkBudget("aaa", compose); err != nil {
		t.Fatalf("re-deploying an existing stack counts it once: %v", err)
	}
}
