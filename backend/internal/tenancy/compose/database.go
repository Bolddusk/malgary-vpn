package compose

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/jackc/pgx/v5"
)

// dbIdent matches the names the backend generates for tenant databases and roles (nb_<slug>).
var dbIdent = regexp.MustCompile(`^nb_[a-z][a-z0-9_]{1,40}$`)

// Provisioner creates tenant databases and roles in the shared PostgreSQL with the
// malgary_provisioner login, which has CREATEDB and CREATEROLE only (research I3). Since
// PostgreSQL 16 it can manage only the roles it created, so it can't touch other apps' data.
type Provisioner struct {
	// DSN connects as malgary_provisioner to the "postgres" database.
	DSN string
}

// Ensure creates or updates role and database name (both nb_<slug>) with password. Safe to
// repeat; the password is reset each time so it always matches the stack's secret.
func (p Provisioner) Ensure(ctx context.Context, name, password string) error {
	if !dbIdent.MatchString(name) {
		return fmt.Errorf("invalid tenant database name %q", name)
	}
	if password == "" {
		return errors.New("empty database password")
	}
	conn, err := pgx.Connect(ctx, p.DSN)
	if err != nil {
		return fmt.Errorf("connect as provisioner: %w", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	var roleExists, dbExists bool
	if err := conn.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1)", name).Scan(&roleExists); err != nil {
		return err
	}
	// Identifiers and the password literal are quoted by PostgreSQL's own format(). ALTER only
	// sets the password: naming SUPERUSER attributes at all needs a superuser.
	stmt := "SELECT format('CREATE ROLE %I LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE PASSWORD %L', $1::text, $2::text)"
	if roleExists {
		stmt = "SELECT format('ALTER ROLE %I LOGIN PASSWORD %L', $1::text, $2::text)"
	}
	if err := execFormatted(ctx, conn, stmt, name, password); err != nil {
		return fmt.Errorf("role %s: %w", name, err)
	}
	// Membership lets the provisioner create the database owned by the role (PostgreSQL 16+
	// grants the creator only ADMIN on a new role).
	if err := execFormatted(ctx, conn, "SELECT format('GRANT %I TO current_user', $1::text)", name); err != nil {
		return fmt.Errorf("grant %s: %w", name, err)
	}
	if err := conn.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)", name).Scan(&dbExists); err != nil {
		return err
	}
	if !dbExists {
		if err := execFormatted(ctx, conn, "SELECT format('CREATE DATABASE %I OWNER %I ENCODING ''UTF8''', $1::text, $1::text)", name); err != nil {
			return fmt.Errorf("database %s: %w", name, err)
		}
	}
	return execFormatted(ctx, conn, "SELECT format('REVOKE ALL ON DATABASE %I FROM PUBLIC', $1::text)", name)
}

// Drop removes a tenant's database and role (exit procedure, FR-017).
func (p Provisioner) Drop(ctx context.Context, name string) error {
	if !dbIdent.MatchString(name) {
		return fmt.Errorf("invalid tenant database name %q", name)
	}
	conn, err := pgx.Connect(ctx, p.DSN)
	if err != nil {
		return fmt.Errorf("connect as provisioner: %w", err)
	}
	defer func() { _ = conn.Close(ctx) }()
	if err := execFormatted(ctx, conn, "SELECT format('DROP DATABASE IF EXISTS %I WITH (FORCE)', $1::text)", name); err != nil {
		return err
	}
	return execFormatted(ctx, conn, "SELECT format('DROP ROLE IF EXISTS %I', $1::text)", name)
}

// execFormatted builds a statement with format() server-side, then runs it.
func execFormatted(ctx context.Context, conn *pgx.Conn, formatSQL string, args ...any) error {
	var stmt string
	if err := conn.QueryRow(ctx, formatSQL, args...).Scan(&stmt); err != nil {
		return err
	}
	_, err := conn.Exec(ctx, stmt)
	return err
}
