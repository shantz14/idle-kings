// Package store is the SQLite persistence layer.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

var (
	ErrNotFound = errors.New("not found")
	ErrExists   = errors.New("already exists")
)

type Store struct{ db *sql.DB }

type Court struct {
	GuildID                string
	AdminUserID            string
	AnnouncementsChannelID string
	Status                 string
}

// Open opens (and creates) the database and brings its schema up to date
// with the migrations in migrations/.
func Open(path string) (*Store, error) {
	ms, err := loadMigrations()
	if err != nil {
		return nil, err
	}
	return open(path, ms)
}

func open(path string, ms []migration) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	}
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(1)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // SQLite has one writer; keeps things simple
	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("open database %s: %w", path, err)
	}
	if err := migrate(ctx, db, path, ms); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// ---- courts ----

// ClaimAdmin makes userID the court admin if nobody has claimed it yet.
func (s *Store) ClaimAdmin(ctx context.Context, guildID, userID string) error {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO courts (guild_id, admin_user_id, created_at) VALUES (?, ?, ?)
		 ON CONFLICT (guild_id) DO NOTHING`, guildID, userID, time.Now().Unix())
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrExists
	}
	return nil
}

func (s *Store) GetCourt(ctx context.Context, guildID string) (Court, error) {
	var c Court
	var ch sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT guild_id, admin_user_id, announcements_channel_id, status FROM courts WHERE guild_id = ?`, guildID).
		Scan(&c.GuildID, &c.AdminUserID, &ch, &c.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	c.AnnouncementsChannelID = ch.String
	return c, err
}

func (s *Store) StartCourt(ctx context.Context, guildID, announcementsChannelID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE courts SET status = ?, announcements_channel_id = ? WHERE guild_id = ?`, courtRunning,
		announcementsChannelID, guildID)
	return err
}
