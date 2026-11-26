package store

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/shantz14/idle-kings/internal/game"
)

// NewCondition gives a character a condition until EndsAt. If they already
// have it, it lasts until whichever end is later.
type NewCondition struct {
	CharacterID int64
	Key         string
	EndsAt      time.Time
}

// fillConditions loads each character's active conditions.
func (s *Store) fillConditions(ctx context.Context, chars []*Character, now time.Time) error {
	if len(chars) == 0 {
		return nil
	}
	byID := map[int64]*Character{}
	args := make([]any, 0, len(chars)+1)
	args = append(args, now.Unix())
	for _, c := range chars {
		byID[c.ID] = c
		c.Conditions = nil
		args = append(args, c.ID)
	}
	q := `SELECT character_id, key, ends_at FROM conditions WHERE ends_at > ? AND character_id IN (?` +
		strings.Repeat(",?", len(chars)-1) + `) ORDER BY key`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, ends int64
		var key string
		if err := rows.Scan(&id, &key, &ends); err != nil {
			return err
		}
		byID[id].Conditions = append(byID[id].Conditions, game.Condition{Key: key, EndsAt: time.Unix(ends, 0)})
	}
	return rows.Err()
}

func addConditions(ctx context.Context, tx *sql.Tx, cs []NewCondition) error {
	for _, c := range cs {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO conditions (character_id, key, ends_at) VALUES (?, ?, ?)
			 ON CONFLICT (character_id, key) DO UPDATE SET ends_at = MAX(conditions.ends_at, excluded.ends_at)`,
			c.CharacterID, c.Key, c.EndsAt.Unix()); err != nil {
			return err
		}
	}
	return nil
}
