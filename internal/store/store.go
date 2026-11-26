// Package store is the SQLite persistence layer.
package store

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

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

// Open opens (and creates) the database and applies the schema.
func Open(path string) (*Store, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // SQLite has one writer; keeps things simple
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("apply schema: %w", err)
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
		`UPDATE courts SET status = 'running', announcements_channel_id = ? WHERE guild_id = ?`,
		announcementsChannelID, guildID)
	return err
}
