package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/campsite-booking/campsite-booking/db"
)

// Migrator wraps golang-migrate's *migrate.Migrate over the embedded
// migration stream.
type Migrator struct {
	m *migrate.Migrate
}

// NewMigrator builds a Migrator from the embedded db.MigrationsFS against
// the Postgres database identified by dsn.
func NewMigrator(dsn string) (*Migrator, error) {
	src, err := iofs.New(db.MigrationsFS, db.MigrationsDir)
	if err != nil {
		return nil, fmt.Errorf("open embedded migration source: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", src, toMigrateDSN(dsn))
	if err != nil {
		return nil, fmt.Errorf("build migrator: %w", err)
	}

	return &Migrator{m: m}, nil
}

// toMigrateDSN rewrites a standard postgres:// DSN to the pgx5:// scheme
// golang-migrate's pgx/v5 database driver expects.
func toMigrateDSN(dsn string) string {
	for _, prefix := range []string{"postgres://", "postgresql://"} {
		if len(dsn) >= len(prefix) && dsn[:len(prefix)] == prefix {
			return "pgx5://" + dsn[len(prefix):]
		}
	}
	return dsn
}

// Up applies all pending migrations. migrate.ErrNoChange is treated as
// success (nothing to do).
func (mg *Migrator) Up() error {
	if err := mg.m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate up: %w", err)
	}
	return nil
}

// Down reverts all applied migrations.
func (mg *Migrator) Down() error {
	if err := mg.m.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate down: %w", err)
	}
	return nil
}

// Steps applies (positive n) or reverts (negative n) n migrations.
func (mg *Migrator) Steps(n int) error {
	if err := mg.m.Steps(n); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate steps: %w", err)
	}
	return nil
}

// Version returns the current schema version. On an empty database it
// returns (0, false, nil).
func (mg *Migrator) Version() (version uint, dirty bool, err error) {
	version, dirty, err = mg.m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("migrate version: %w", err)
	}
	return version, dirty, nil
}

// Force sets the schema version without running migrations, clearing a
// dirty state.
func (mg *Migrator) Force(version int) error {
	if err := mg.m.Force(version); err != nil {
		return fmt.Errorf("migrate force: %w", err)
	}
	return nil
}

// Close releases the source and database driver resources.
func (mg *Migrator) Close() error {
	srcErr, dbErr := mg.m.Close()
	if srcErr != nil {
		return fmt.Errorf("close migration source: %w", srcErr)
	}
	if dbErr != nil {
		return fmt.Errorf("close migration database: %w", dbErr)
	}
	return nil
}

// RunMigrations builds a Migrator for dsn and applies all pending
// migrations, returning an error on a dirty or failed state.
func RunMigrations(ctx context.Context, dsn string) error {
	mg, err := NewMigrator(dsn)
	if err != nil {
		return err
	}
	defer mg.Close()

	return mg.Up()
}
