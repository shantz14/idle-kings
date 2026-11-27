package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/shantz14/idle-kings/internal/game"
)

// Happening is something abroad in a realm (game.HappeningDefs).
type Happening struct {
	ID        int64
	GuildID   string
	Key       string
	StartedAt time.Time
	EndsAt    time.Time
}

const happeningCols = `id, guild_id, key, started_at, ends_at`

func scanHappening(r scanner) (Happening, error) {
	var h Happening
	var started, ends int64
	err := r.Scan(&h.ID, &h.GuildID, &h.Key, &started, &ends)
	h.StartedAt, h.EndsAt = time.Unix(started, 0), time.Unix(ends, 0)
	return h, err
}

// StartHappening puts a happening abroad in a court until ends, and shocks
// the court's industries by sectors (nil for none), together.
func (s *Store) StartHappening(ctx context.Context, guildID, key string, now, ends time.Time, sectors map[game.SectorKey]game.SectorChange) (Happening, error) {
	h := Happening{GuildID: guildID, Key: key, StartedAt: now, EndsAt: ends}
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `INSERT INTO happenings (guild_id, key, started_at, ends_at) VALUES (?, ?, ?, ?)`,
			guildID, key, now.Unix(), ends.Unix())
		if err != nil {
			return err
		}
		h.ID, _ = res.LastInsertId()
		if len(sectors) == 0 {
			return nil
		}
		var raw string
		if err := tx.QueryRowContext(ctx, `SELECT sectors FROM realms WHERE guild_id = ?`, guildID).Scan(&raw); err != nil {
			return err
		}
		var sec game.SectorState
		if err := json.Unmarshal([]byte(raw), &sec); err != nil {
			return err
		}
		sec = sec.Fill()
		sec.Apply(sectors)
		b, _ := json.Marshal(sec)
		_, err = tx.ExecContext(ctx, `UPDATE realms SET sectors = ? WHERE guild_id = ?`, string(b), guildID)
		return err
	})
	return h, err
}

// ActiveHappenings are a court's happenings still abroad, oldest first.
func (s *Store) ActiveHappenings(ctx context.Context, guildID string) ([]Happening, error) {
	return queryAll(ctx, s.db, `SELECT `+happeningCols+` FROM happenings WHERE guild_id = ? AND ended_at IS NULL ORDER BY id`,
		scanHappening, guildID)
}

// LastHappening is the key of the court's most recently ended happening,
// or "".
func (s *Store) LastHappening(ctx context.Context, guildID string) (string, error) {
	var key string
	err := s.db.QueryRowContext(ctx, `SELECT key FROM happenings WHERE guild_id = ? AND ended_at IS NOT NULL ORDER BY ended_at DESC, id DESC LIMIT 1`,
		guildID).Scan(&key)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return key, err
}

// DueHappenings are happenings in any court whose time is up.
func (s *Store) DueHappenings(ctx context.Context, now time.Time) ([]Happening, error) {
	return queryAll(ctx, s.db, `SELECT `+happeningCols+` FROM happenings WHERE ended_at IS NULL AND ends_at <= ?`,
		scanHappening, now.Unix())
}

// EndHappening ends a happening, by a character (0 when it fades). It ends
// only once: a second call returns ErrEnded.
func (s *Store) EndHappening(ctx context.Context, id, by int64, now time.Time) error {
	var who any
	if by != 0 {
		who = by
	}
	res, err := s.db.ExecContext(ctx, `UPDATE happenings SET ended_at = ?, ended_by = ? WHERE id = ? AND ended_at IS NULL`, now.Unix(), who, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrEnded
	}
	return nil
}
