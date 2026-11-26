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
	return s.withTx(ctx, func(tx *sql.Tx) error { return applyOutcome(ctx, tx, guildID, out, now) })
}

// ---- the Informers' Law ----

// Denunciation is a misdeed a player's spies reported. It sits in their
// dossier, and never goes stale, until they use it.
type Denunciation struct {
	ID        int64
	GuildID   string
	OwnerID   int64  // who can denounce
	ActorID   int64  // who did it
	Crime     string // the secret kind, e.g. "bribe"
	Detail    string
	CreatedAt time.Time
	Used      bool
}

// ErrDenounced means a report was already used.
var ErrDenounced = errors.New("you've already used that report")

// CreateDenunciation files a report in its owner's dossier. (expires_at is
// unused now; reports keep forever.)
func (s *Store) CreateDenunciation(ctx context.Context, d Denunciation, now time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO denunciations (guild_id, owner_id, actor_id, crime, detail, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, 0)`,
		d.GuildID, d.OwnerID, d.ActorID, d.Crime, d.Detail, now.Unix())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) GetDenunciation(ctx context.Context, id int64) (Denunciation, error) {
	var d Denunciation
	var created int64
	var used sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT id, guild_id, owner_id, actor_id, crime, detail, created_at, used_at FROM denunciations WHERE id = ?`, id).
		Scan(&d.ID, &d.GuildID, &d.OwnerID, &d.ActorID, &d.Crime, &d.Detail, &created, &used)
	if errors.Is(err, sql.ErrNoRows) {
		return d, ErrNotFound
	}
	d.CreatedAt, d.Used = time.Unix(created, 0), used.Valid
	return d, err
}

// Dossier is a player's unused reports, newest first.
func (s *Store) Dossier(ctx context.Context, ownerID int64) ([]Denunciation, error) {
	return queryAll(ctx, s.db,
		`SELECT id, guild_id, owner_id, actor_id, crime, detail, created_at FROM denunciations
		 WHERE owner_id = ? AND used_at IS NULL ORDER BY created_at DESC, id DESC`,
		func(r scanner) (Denunciation, error) {
			var d Denunciation
			var created int64
			err := r.Scan(&d.ID, &d.GuildID, &d.OwnerID, &d.ActorID, &d.Crime, &d.Detail, &created)
			d.CreatedAt = time.Unix(created, 0)
			return d, err
		}, ownerID)
}

// Denounce uses a report once and applies what it does. ErrDenounced if it
// was already used.
func (s *Store) Denounce(ctx context.Context, id int64, out Outcome, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var guildID string
	err = tx.QueryRowContext(ctx,
		`UPDATE denunciations SET used_at = ? WHERE id = ? AND used_at IS NULL RETURNING guild_id`,
		now.Unix(), id).Scan(&guildID)
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
