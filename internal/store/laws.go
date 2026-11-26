package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ApplyOutcome applies an outcome that belongs to no activity, e.g. what a
// law costs a player (a dueling ban, a bribe that comes out).
func (s *Store) ApplyOutcome(ctx context.Context, guildID string, out Outcome, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := applyOutcome(ctx, tx, guildID, out, now); err != nil {
		return err
	}
	return tx.Commit()
}

// ---- the Informers' Law ----

// Denunciation is a crime a player's spies reported, which they may denounce.
type Denunciation struct {
	ID        int64
	GuildID   string
	OwnerID   int64 // who can denounce
	ActorID   int64 // who committed it
	Crime     string
	Detail    string
	ExpiresAt time.Time
	Used      bool
}

// ErrDenounced means a denunciation was already made or has expired.
var ErrDenounced = errors.New("that is old news now")

func (s *Store) CreateDenunciation(ctx context.Context, d Denunciation, now time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO denunciations (guild_id, owner_id, actor_id, crime, detail, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		d.GuildID, d.OwnerID, d.ActorID, d.Crime, d.Detail, now.Unix(), d.ExpiresAt.Unix())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) GetDenunciation(ctx context.Context, id int64) (Denunciation, error) {
	var d Denunciation
	var exp int64
	var used sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT id, guild_id, owner_id, actor_id, crime, detail, expires_at, used_at FROM denunciations WHERE id = ?`, id).
		Scan(&d.ID, &d.GuildID, &d.OwnerID, &d.ActorID, &d.Crime, &d.Detail, &exp, &used)
	if errors.Is(err, sql.ErrNoRows) {
		return d, ErrNotFound
	}
	d.ExpiresAt, d.Used = time.Unix(exp, 0), used.Valid
	return d, err
}

// Denounce uses a denunciation once, before it expires, and applies what it
// does. ErrDenounced if it was used or has expired.
func (s *Store) Denounce(ctx context.Context, id int64, out Outcome, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var guildID string
	err = tx.QueryRowContext(ctx,
		`UPDATE denunciations SET used_at = ? WHERE id = ? AND used_at IS NULL AND expires_at > ? RETURNING guild_id`,
		now.Unix(), id, now.Unix()).Scan(&guildID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrDenounced
	} else if err != nil {
		return err
	}
	if err := applyOutcome(ctx, tx, guildID, out, now); err != nil {
		return err
	}
	return tx.Commit()
}
