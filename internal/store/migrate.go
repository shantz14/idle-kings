package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Migrations live in migrations/NNNN_name.sql and run in order, each in its
// own transaction that also sets PRAGMA user_version to its number. A released
// migration is never edited: change the schema by adding the next file.
//
// A file whose first line is "-- migrate: foreign_keys=off" runs with foreign
// keys switched off (SQLite can't change that inside a transaction), which a
// table rebuild needs; foreign_key_check runs before they go back on.
//
// goMigrations adds a Go step to a version, run in the same transaction after
// that version's SQL: for data fixes SQL can't express (rewriting JSON, say).

//go:embed migrations/*.sql
var migrationFiles embed.FS

var goMigrations = map[int]func(ctx context.Context, tx *sql.Tx) error{}

type migration struct {
	Version int
	Name    string
	SQL     string
	NoFKs   bool
	Go      func(ctx context.Context, tx *sql.Tx) error
}

const noFKsDirective = "-- migrate: foreign_keys=off"

// keepBackups is how many pre-migration backups are kept next to the database.
const keepBackups = 3

// loadMigrations reads the embedded migration files.
func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return nil, err
	}
	var ms []migration
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".sql") {
			continue
		}
		num, _, ok := strings.Cut(name, "_")
		v, err := strconv.Atoi(num)
		if !ok || err != nil || v <= 0 {
			return nil, fmt.Errorf("migration %q: name must be NNNN_name.sql", name)
		}
		b, err := migrationFiles.ReadFile("migrations/" + name)
		if err != nil {
			return nil, err
		}
		text := string(b)
		ms = append(ms, migration{
			Version: v, Name: name, SQL: text,
			NoFKs: strings.HasPrefix(strings.TrimSpace(text), noFKsDirective),
			Go:    goMigrations[v],
		})
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i].Version < ms[j].Version })
	for i, m := range ms {
		if m.Version != i+1 {
			return nil, fmt.Errorf("migration %s: expected version %d (versions must run 1, 2, 3… with no gaps or repeats)", m.Name, i+1)
		}
	}
	return ms, nil
}

// migrate brings the database at path up to the latest migration.
func migrate(ctx context.Context, db *sql.DB, path string, ms []migration) error {
	var version int
	if err := db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	latest := len(ms)
	if version > latest {
		return fmt.Errorf("database is at schema version %d but this build only knows %d: it was written by a newer build", version, latest)
	}
	if version == latest {
		return nil
	}
	var tables int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type = 'table'`).Scan(&tables); err != nil {
		return err
	}
	if version == 0 && tables > 0 {
		// A database from before migrations existed. The baseline is all
		// IF NOT EXISTS, so running it over the old tables is safe.
		slog.Info("database predates migrations; adopting it at the baseline")
	}
	if tables > 0 {
		if err := backup(ctx, db, path, version); err != nil {
			return fmt.Errorf("back up before migrating: %w", err)
		}
	}
	for _, m := range ms[version:] {
		if err := apply(ctx, db, m); err != nil {
			return fmt.Errorf("migration %s: %w", m.Name, err)
		}
		slog.Info("applied migration", "version", m.Version, "name", m.Name)
	}
	return nil
}

func apply(ctx context.Context, db *sql.DB, m migration) (err error) {
	if m.NoFKs {
		if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
			return err
		}
		defer func() {
			if _, e := db.ExecContext(ctx, `PRAGMA foreign_keys = ON`); e != nil && err == nil {
				err = e
			}
		}()
	}
	return (&Store{db: db}).withTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, m.SQL); err != nil {
			return err
		}
		if m.Go != nil {
			if err := m.Go(ctx, tx); err != nil {
				return err
			}
		}
		if m.NoFKs {
			rows, err := tx.QueryContext(ctx, `PRAGMA foreign_key_check`)
			if err != nil {
				return err
			}
			bad := rows.Next()
			rows.Close()
			if bad {
				return fmt.Errorf("foreign key check failed after the migration")
			}
		}
		// PRAGMA takes no parameters; the version is an int we made.
		_, err := tx.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d`, m.Version))
		return err
	})
}

// backup copies the database aside before it is migrated, keeping the newest
// few copies.
func backup(ctx context.Context, db *sql.DB, path string, version int) error {
	if path == "" || strings.Contains(path, ":memory:") {
		return nil
	}
	dest := fmt.Sprintf("%s.pre-v%d.bak", path, version+1)
	_ = os.Remove(dest)
	if _, err := db.ExecContext(ctx, `VACUUM INTO ?`, dest); err != nil {
		return err
	}
	slog.Info("backed up database before migrating", "to", dest)
	old, _ := filepath.Glob(path + ".pre-v*.bak")
	if len(old) <= keepBackups {
		return nil
	}
	sort.Slice(old, func(i, j int) bool { return backupVersion(path, old[i]) < backupVersion(path, old[j]) })
	for _, f := range old[:len(old)-keepBackups] {
		_ = os.Remove(f)
	}
	return nil
}

func backupVersion(path, f string) int {
	v, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(f, path+".pre-v"), ".bak"))
	return v
}
