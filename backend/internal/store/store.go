// Package store owns the database connection, migrations and transactions.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib" // database/sql driver for goose
	"github.com/pressly/goose/v3"

	"malgary/backend/internal/store/db"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Store wraps the pgx pool and the generated queries.
type Store struct {
	Pool *pgxpool.Pool
	*db.Queries
}

// Open connects to PostgreSQL as the application role.
func Open(ctx context.Context, url string) (*Store, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return &Store{Pool: pool, Queries: db.New(pool)}, nil
}

// Close releases the pool.
func (s *Store) Close() { s.Pool.Close() }

// Migrate applies embedded migrations using the owner role.
func Migrate(ctx context.Context, ownerURL string) error {
	sqlDB, err := sql.Open("pgx", ownerURL)
	if err != nil {
		return err
	}
	defer func() { _ = sqlDB.Close() }()
	goose.SetBaseFS(migrations)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	return goose.UpContext(ctx, sqlDB, "migrations")
}

// WithTx runs fn in one transaction. Audit records written inside fn commit or roll back with
// the change they describe (FR-026).
func (s *Store) WithTx(ctx context.Context, fn func(q *db.Queries, tx pgx.Tx) error) (err error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			if rbErr := tx.Rollback(ctx); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
				err = errors.Join(err, rbErr)
			}
		}
	}()
	if err = fn(s.Queries.WithTx(tx), tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// IsNotFound reports whether err means "no rows".
func IsNotFound(err error) bool { return errors.Is(err, pgx.ErrNoRows) }
