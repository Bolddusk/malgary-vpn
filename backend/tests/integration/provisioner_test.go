//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	tcpg "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"malgary/backend/internal/tenancy/compose"
)

// TestProvisioner runs the ComposeDeployer's database step against PostgreSQL 18 (the shared
// server's version) with a login that has only CREATEDB and CREATEROLE, as on the interim host
// (research I3, FR-007).
func TestProvisioner(t *testing.T) {
	ctx := context.Background()
	c, err := tcpg.Run(ctx, "postgres:18-alpine",
		tcpg.WithDatabase("postgres"), tcpg.WithUsername("postgres"), tcpg.WithPassword("postgres"),
		testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(240*time.Second)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Terminate(context.Background()) })
	admin, _ := c.ConnectionString(ctx, "sslmode=disable")
	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	// Another app's database and role, which the provisioner must not be able to touch.
	for _, q := range []string{
		"CREATE ROLE malgary_provisioner LOGIN CREATEDB CREATEROLE PASSWORD 'prov'",
		"CREATE ROLE otherapp LOGIN PASSWORD 'other'",
		"CREATE DATABASE otherapp OWNER otherapp",
	} {
		if _, err := conn.Exec(ctx, q); err != nil {
			t.Fatal(q, err)
		}
	}
	_ = conn.Close(ctx)
	dsn := func(user, pw, db string) string {
		s := strings.Replace(admin, "postgres:postgres@", user+":"+pw+"@", 1)
		return strings.Replace(s, "/postgres?", "/"+db+"?", 1)
	}
	p := compose.Provisioner{DSN: dsn("malgary_provisioner", "prov", "postgres")}

	if err := p.Ensure(ctx, "nb_testco", "first'pw\"x"); err != nil {
		t.Fatal("create:", err)
	}
	if err := p.Ensure(ctx, "nb_testco", "second-pw"); err != nil {
		t.Fatal("repeat with a new password:", err)
	}
	tc, err := pgx.Connect(ctx, dsn("nb_testco", "second-pw", "nb_testco"))
	if err != nil {
		t.Fatal("tenant role can't use its database:", err)
	}
	if _, err := tc.Exec(ctx, "CREATE TABLE t (id int)"); err != nil {
		t.Fatal("tenant role must own its database:", err)
	}
	_ = tc.Close(ctx)
	if c, err := pgx.Connect(ctx, dsn("nb_testco", "second-pw", "otherapp")); err == nil {
		// PUBLIC has CONNECT on otherapp by default; the tenant must still have no rights there.
		if _, err := c.Exec(ctx, "CREATE TABLE x (id int)"); err == nil {
			t.Fatal("tenant role created a table in another app's database")
		}
		_ = c.Close(ctx)
	}

	pc, _ := pgx.Connect(ctx, dsn("malgary_provisioner", "prov", "postgres"))
	for _, q := range []string{"ALTER ROLE otherapp PASSWORD 'x'", "DROP DATABASE otherapp", "DROP ROLE otherapp"} {
		if _, err := pc.Exec(ctx, q); err == nil {
			t.Fatalf("provisioner must not be able to run %q", q)
		}
	}
	_ = pc.Close(ctx)

	if err := p.Ensure(ctx, "otherapp", "x"); err == nil {
		t.Fatal("only nb_<slug> names are accepted")
	}
	if err := p.Drop(ctx, "nb_testco"); err != nil {
		t.Fatal("drop:", err)
	}
	if err := p.Drop(ctx, "nb_testco"); err != nil {
		t.Fatal("drop is repeatable:", err)
	}
}
