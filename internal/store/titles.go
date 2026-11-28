package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/shantz14/idle-kings/internal/game"
)

// ---- seasons ----

// Season is a court's current season and the Senate days it has held.
type Season struct {
	Number     int
	SenateDays int
}

// DaysLeft is how many Senate days the season has to run.
func (s Season) DaysLeft() int { return max(game.SeasonDays-s.SenateDays, 0) }

// Season is a court's current season. A court that hasn't held a Senate day
// is in season 1, day 0.
func (s *Store) Season(ctx context.Context, guildID string) (Season, error) {
	out := Season{Number: 1}
	err := s.db.QueryRowContext(ctx, `SELECT season, senate_days FROM seasons WHERE guild_id = ?`, guildID).
		Scan(&out.Number, &out.SenateDays)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	return out, err
}

// senateDay counts a Senate day toward the season, and fades everyone's
// influence a little (game.InfluenceDecay), in the matter's transaction.
func senateDay(ctx context.Context, tx *sql.Tx, guildID string) error {
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO seasons (guild_id, season, senate_days) VALUES (?, 1, 1)
		 ON CONFLICT (guild_id) DO UPDATE SET senate_days = senate_days + 1`, guildID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE characters SET influence = influence * ? WHERE guild_id = ? AND influence > 0`,
		game.InfluenceDecay, guildID)
	return err
}

// TitleAward is a title given to a character.
type TitleAward struct {
	CharacterID int64
	Key         string
}

// EndSeason ends a court's season once: it gives the season's titles and
// starts the next season. ErrEnded if that season was already ended.
func (s *Store) EndSeason(ctx context.Context, guildID string, season int, awards []TitleAward, now time.Time) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE seasons SET season = season + 1, senate_days = 0 WHERE guild_id = ? AND season = ?`,
			guildID, season)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			// A court that never held a Senate day has no row yet.
			res, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO seasons (guild_id, season, senate_days) SELECT ?, ?, 0 WHERE ? = 1`,
				guildID, season+1, season)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				return ErrEnded
			}
		}
		for _, a := range awards {
			if _, err := tx.ExecContext(ctx,
				`INSERT OR IGNORE INTO titles (guild_id, character_id, key, season, earned_at) VALUES (?, ?, ?, ?, ?)`,
				guildID, a.CharacterID, a.Key, season, now.Unix()); err != nil {
				return err
			}
		}
		return nil
	})
}

// ---- deeds ----

// Deed is something a season's deed titles count.
type Deed struct {
	CharacterID int64
	Kind        string // game.Deed…
	Amount      float64
}

// AddDeeds records deeds in the court's current season.
func (s *Store) AddDeeds(ctx context.Context, guildID string, deeds []Deed, now time.Time) error {
	if len(deeds) == 0 {
		return nil
	}
	season, err := s.Season(ctx, guildID)
	if err != nil {
		return err
	}
	return s.withTx(ctx, func(tx *sql.Tx) error {
		for _, d := range deeds {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO deeds (guild_id, character_id, kind, amount, season, at) VALUES (?, ?, ?, ?, ?, ?)`,
				guildID, d.CharacterID, d.Kind, d.Amount, season.Number, now.Unix()); err != nil {
				return err
			}
		}
		return nil
	})
}

// SeasonTotals sums a kind of deed for every character in a season.
func (s *Store) SeasonTotals(ctx context.Context, guildID string, season int, kind string) (map[int64]float64, error) {
	type row struct {
		id  int64
		sum float64
	}
	rows, err := queryAll(ctx, s.db,
		`SELECT character_id, SUM(amount) FROM deeds WHERE guild_id = ? AND season = ? AND kind = ? GROUP BY character_id`,
		func(r scanner) (row, error) {
			var x row
			err := r.Scan(&x.id, &x.sum)
			return x, err
		}, guildID, season, kind)
	out := make(map[int64]float64, len(rows))
	for _, x := range rows {
		out[x.id] = x.sum
	}
	return out, err
}

// ---- titles ----

// AwardFeat gives a character a feat, once. fresh is false if they already
// had it.
func (s *Store) AwardFeat(ctx context.Context, guildID string, charID int64, key string, now time.Time) (fresh bool, err error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO titles (guild_id, character_id, key, season, earned_at) VALUES (?, ?, ?, 0, ?)`,
		guildID, charID, key, now.Unix())
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// TitlesOf is the titles each character holds, oldest first.
func (s *Store) TitlesOf(ctx context.Context, charIDs []int64) (map[int64][]game.Title, error) {
	out := map[int64][]game.Title{}
	if len(charIDs) == 0 {
		return out, nil
	}
	in, args := inList(charIDs)
	type row struct {
		id int64
		t  game.Title
	}
	rows, err := queryAll(ctx, s.db,
		`SELECT character_id, key, season FROM titles WHERE character_id IN `+in+` ORDER BY earned_at, id`,
		func(r scanner) (row, error) {
			var x row
			err := r.Scan(&x.id, &x.t.Key, &x.t.Season)
			return x, err
		}, args...)
	for _, x := range rows {
		out[x.id] = append(out[x.id], x.t)
	}
	return out, err
}
