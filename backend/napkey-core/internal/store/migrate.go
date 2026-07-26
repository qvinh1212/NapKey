package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	"napkey-core/internal/logger"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// migrationLockID is the advisory lock key held while migrating. Two replicas
// starting at once would otherwise both try to create the same tables; one would
// fail and crash-loop. The number is arbitrary but must never change.
const migrationLockID int64 = 8_531_204_771

// migration is one file from the embedded set.
type migration struct {
	version int
	name    string
	sql     string
	// checksum detects a migration edited after it was applied, which silently
	// desynchronizes environments.
	checksum string
}

// Migrate applies every pending migration in one transaction per migration.
//
// Running DDL inside a transaction is possible in Postgres (unlike MySQL), so a
// migration that fails halfway leaves nothing behind.
func Migrate(ctx context.Context, db *sql.DB) error {
	migrations, err := loadMigrations()
	if err != nil {
		return err
	}
	if len(migrations) == 0 {
		return fmt.Errorf("store: no migrations found in the embedded filesystem")
	}

	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("store: acquiring a connection for migration: %w", err)
	}
	defer conn.Close()

	// The lock must be taken on the same connection that runs the migrations and
	// released on the same one; a pooled connection could otherwise hand the lock
	// to an unrelated query.
	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", migrationLockID); err != nil {
		return fmt.Errorf("store: taking the migration advisory lock: %w", err)
	}
	defer func() {
		if _, err := conn.ExecContext(context.WithoutCancel(ctx),
			"SELECT pg_advisory_unlock($1)", migrationLockID); err != nil {
			logger.Warnf("releasing the migration advisory lock failed: %v", err)
		}
	}()

	if _, err := conn.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    integer PRIMARY KEY,
			name       text NOT NULL,
			checksum   text NOT NULL,
			applied_at timestamptz NOT NULL DEFAULT now()
		)`); err != nil {
		return fmt.Errorf("store: creating schema_migrations: %w", err)
	}

	applied := map[int]string{}
	rows, err := conn.QueryContext(ctx, "SELECT version, checksum FROM schema_migrations")
	if err != nil {
		return fmt.Errorf("store: reading schema_migrations: %w", err)
	}
	for rows.Next() {
		var v int
		var sum string
		if err := rows.Scan(&v, &sum); err != nil {
			rows.Close()
			return fmt.Errorf("store: scanning schema_migrations: %w", err)
		}
		applied[v] = sum
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("store: iterating schema_migrations: %w", err)
	}
	rows.Close()

	pending := 0
	for _, m := range migrations {
		if existing, ok := applied[m.version]; ok {
			// A changed checksum means the file was edited after being applied.
			// Continuing would leave this environment on a schema nobody can
			// reproduce, so stop and make a human decide.
			if existing != m.checksum {
				return fmt.Errorf(
					"store: migration %04d_%s was modified after it was applied (recorded checksum %s, file checksum %s); "+
						"add a new migration instead of editing an applied one",
					m.version, m.name, existing, m.checksum)
			}
			continue
		}
		pending++
		logger.Infof("applying migration %04d_%s", m.version, m.name)

		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("store: beginning migration %04d: %w", m.version, err)
		}
		if _, err := tx.ExecContext(ctx, m.sql); err != nil {
			tx.Rollback()
			return fmt.Errorf("store: applying migration %04d_%s: %w", m.version, m.name, err)
		}
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO schema_migrations (version, name, checksum) VALUES ($1, $2, $3)",
			m.version, m.name, m.checksum); err != nil {
			tx.Rollback()
			return fmt.Errorf("store: recording migration %04d_%s: %w", m.version, m.name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("store: committing migration %04d_%s: %w", m.version, m.name, err)
		}
	}

	if pending == 0 {
		logger.Infof("database schema is up to date (%d migrations applied)", len(applied))
	} else {
		logger.Infof("applied %d migration(s)", pending)
	}
	return nil
}

// loadMigrations reads and orders the embedded migration files. Names must be
// NNNN_description.sql.
func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("store: reading embedded migrations: %w", err)
	}
	var out []migration
	seen := map[int]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		base := strings.TrimSuffix(e.Name(), ".sql")
		numPart, namePart, found := strings.Cut(base, "_")
		if !found {
			return nil, fmt.Errorf("store: migration %q must be named NNNN_description.sql", e.Name())
		}
		version, err := strconv.Atoi(numPart)
		if err != nil {
			return nil, fmt.Errorf("store: migration %q has a non-numeric version prefix", e.Name())
		}
		if prev, dup := seen[version]; dup {
			return nil, fmt.Errorf("store: migrations %q and %q share version %d", prev, e.Name(), version)
		}
		seen[version] = e.Name()

		body, err := migrationFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			return nil, fmt.Errorf("store: reading migration %q: %w", e.Name(), err)
		}
		sum := sha256.Sum256(body)
		out = append(out, migration{
			version:  version,
			name:     namePart,
			sql:      string(body),
			checksum: hex.EncodeToString(sum[:]),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}
