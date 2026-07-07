// Package db embeds the golang-migrate SQL migration stream so the binary
// carries its schema with it (no external migration files at runtime).
package db

import "embed"

//go:embed migrations/*.sql
var MigrationsFS embed.FS

// MigrationsDir is the sub-directory within MigrationsFS holding the
// migration files, for use with source/iofs.
const MigrationsDir = "migrations"
