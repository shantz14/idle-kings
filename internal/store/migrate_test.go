package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func userVersion(t *testing.T, s *Store) int {
	t.Helper()
	var v int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func schemaOf(t *testing.T, s *Store) string {
	t.Helper()
	rows, err := s.db.Query(`SELECT type, name, coalesce(sql, '') FROM sqlite_master WHERE name NOT LIKE 'sqlite_%' ORDER BY type, name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var typ, name, q string
		if err := rows.Scan(&typ, &name, &q); err != nil {
			t.Fatal(err)
		}
		b.WriteString(typ + " " + name + " " + q + "\n")
	}
	return b.String()
}

func mustMigrations(t *testing.T) []migration {
	t.Helper()
	ms, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	return ms
}

func TestMigrationsAreNumberedInOrder(t *testing.T) {
	ms := mustMigrations(t)
	if len(ms) == 0 || ms[0].Version != 1 {
		t.Fatal("expected a baseline migration 0001")
	}
	for i, m := range ms {
		if m.Version != i+1 {
			t.Errorf("%s: version %d at position %d", m.Name, m.Version, i+1)
		}
	}
}

func TestFreshDatabaseReachesLatestVersion(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "sub", "dir", "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got, want := userVersion(t, s), len(mustMigrations(t)); got != want {
		t.Fatalf("user_version = %d, want %d", got, want)
	}
}

func TestReopenIsANoOp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	before := schemaOf(t, s)
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if after := schemaOf(t, s); after != before {
		t.Fatal("reopening changed the schema")
	}
	if m, _ := filepath.Glob(path + ".pre-v*.bak"); len(m) != 0 {
		t.Fatalf("an up-to-date database was backed up: %v", m)
	}
}

// A database made before migrations existed has the baseline tables but
// user_version 0. It must be adopted with its data kept.
func TestLegacyDatabaseIsAdopted(t *testing.T) {
	ms := mustMigrations(t)
	path := filepath.Join(t.TempDir(), "t.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ms[0].SQL); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO courts (guild_id, admin_user_id, created_at) VALUES ('g', 'a', 1)`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got := userVersion(t, s); got != len(ms) {
		t.Fatalf("user_version = %d, want %d", got, len(ms))
	}
	if _, err := s.GetCourt(context.Background(), "g"); err != nil {
		t.Fatalf("legacy data lost: %v", err)
	}
	if m, _ := filepath.Glob(path + ".pre-v1.bak"); len(m) != 1 {
		t.Fatal("no backup taken before adopting a legacy database")
	}
}

func TestNewerDatabaseIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`PRAGMA user_version = 999`); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if _, err := Open(path); err == nil || !strings.Contains(err.Error(), "newer build") {
		t.Fatalf("err = %v, want a newer-build refusal", err)
	}
}

func TestMigrationAddsColumnToPopulatedDatabase(t *testing.T) {
	ms := mustMigrations(t)
	path := filepath.Join(t.TempDir(), "t.db")
	s, err := open(path, ms)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_ = s.ClaimAdmin(ctx, "g", "a")
	s.Close()

	next := append(append([]migration{}, ms...), migration{
		Version: len(ms) + 1, Name: "test_add_column.sql",
		SQL: `ALTER TABLE courts ADD COLUMN motto TEXT NOT NULL DEFAULT 'none';`,
	})
	s, err = open(path, next)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got := userVersion(t, s); got != len(next) {
		t.Fatalf("user_version = %d, want %d", got, len(next))
	}
	var motto string
	if err := s.db.QueryRow(`SELECT motto FROM courts WHERE guild_id = 'g'`).Scan(&motto); err != nil || motto != "none" {
		t.Fatalf("motto = %q, %v", motto, err)
	}
	if m, _ := filepath.Glob(path + ".pre-v*.bak"); len(m) != 1 {
		t.Fatalf("backups = %v, want one", m)
	}
}

func TestFailedMigrationRollsBack(t *testing.T) {
	ms := mustMigrations(t)
	path := filepath.Join(t.TempDir(), "t.db")
	s, err := open(path, ms)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()

	bad := append(append([]migration{}, ms...), migration{
		Version: len(ms) + 1, Name: "test_bad.sql",
		SQL: `CREATE TABLE half_done (id INTEGER); THIS IS NOT SQL;`,
	})
	if _, err := open(path, bad); err == nil {
		t.Fatal("a bad migration opened cleanly")
	}
	s, err = open(path, ms)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got := userVersion(t, s); got != len(ms) {
		t.Fatalf("user_version = %d after a failed migration, want %d", got, len(ms))
	}
	var n int
	_ = s.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name = 'half_done'`).Scan(&n)
	if n != 0 {
		t.Fatal("a failed migration left a table behind")
	}
}

func TestGoMigrationRunsInTheSameTransaction(t *testing.T) {
	ms := mustMigrations(t)
	path := filepath.Join(t.TempDir(), "t.db")
	s, err := open(path, ms)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()

	boom := errors.New("boom")
	next := append(append([]migration{}, ms...), migration{
		Version: len(ms) + 1, Name: "test_go.sql",
		SQL: `CREATE TABLE from_go (id INTEGER);`,
		Go:  func(ctx context.Context, tx *sql.Tx) error { return boom },
	})
	if _, err := open(path, next); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	s, err = open(path, ms)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var n int
	_ = s.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name = 'from_go'`).Scan(&n)
	if n != 0 {
		t.Fatal("the SQL half of a failed Go migration was kept")
	}
}

// A table rebuild with foreign keys off: the migrated schema must match what a
// fresh database would get from the same files.
func TestRebuildMigrationWithForeignKeysOff(t *testing.T) {
	ms := mustMigrations(t)
	path := filepath.Join(t.TempDir(), "t.db")
	s, err := open(path, ms)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.ClaimAdmin(context.Background(), "g", "a")
	s.Close()

	rebuild := migration{
		Version: len(ms) + 1, Name: "test_rebuild.sql", NoFKs: true,
		SQL: noFKsDirective + `
CREATE TABLE courts_new (
    guild_id                 TEXT PRIMARY KEY,
    admin_user_id            TEXT NOT NULL,
    announcements_channel_id TEXT,
    status                   TEXT NOT NULL DEFAULT 'setup',
    created_at               INTEGER NOT NULL
);
INSERT INTO courts_new SELECT guild_id, admin_user_id, announcements_channel_id, status, created_at FROM courts;
DROP TABLE courts;
ALTER TABLE courts_new RENAME TO courts;`,
	}
	next := append(append([]migration{}, ms...), rebuild)
	s, err = open(path, next)
	if err != nil {
		t.Fatal(err)
	}
	migrated := schemaOf(t, s)
	if _, err := s.GetCourt(context.Background(), "g"); err != nil {
		t.Fatalf("rebuild lost data: %v", err)
	}
	var fk int
	_ = s.db.QueryRow(`PRAGMA foreign_keys`).Scan(&fk)
	if fk != 1 {
		t.Fatal("foreign keys left off after a rebuild")
	}
	s.Close()

	fresh, err := open(filepath.Join(t.TempDir(), "fresh.db"), next)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	if got := schemaOf(t, fresh); got != migrated {
		t.Fatalf("migrated schema differs from fresh:\nmigrated:\n%s\nfresh:\n%s", migrated, got)
	}
}
