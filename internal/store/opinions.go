package store

import (
	"context"
	"database/sql"

	"github.com/shantz14/idle-kings/internal/game"
)

// Opinions is every senator's opinion of one character, by senator ID.
func (s *Store) Opinions(ctx context.Context, charID int64) (game.Opinions, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT senator_id, value FROM senator_opinions WHERE character_id = ?`, charID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := game.Opinions{}
	for rows.Next() {
		var id int64
		var v float64
		if err := rows.Scan(&id, &v); err != nil {
			return nil, err
		}
		out[id] = v
	}
	return out, rows.Err()
}

// CourtOpinions is every senator's opinion of every player at a court.
func (s *Store) CourtOpinions(ctx context.Context, guildID string) (game.CourtOpinions, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT o.senator_id, o.character_id, o.value FROM senator_opinions o
		 JOIN senators s ON s.id = o.senator_id WHERE s.guild_id = ?`, guildID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := game.CourtOpinions{}
	for rows.Next() {
		var sid, cid int64
		var v float64
		if err := rows.Scan(&sid, &cid, &v); err != nil {
			return nil, err
		}
		if out[sid] == nil {
			out[sid] = game.Opinions{}
		}
		out[sid][cid] = v
	}
	return out, rows.Err()
}

// ChangeOpinions applies opinion changes outside any other transaction.
func (s *Store) ChangeOpinions(ctx context.Context, changes []game.OpinionChange) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := changeOpinions(ctx, tx, changes); err != nil {
		return err
	}
	return tx.Commit()
}

// changeOpinions adds each change, clamped to -100..100. SenatorID 0 means
// every senator at the character's court.
func changeOpinions(ctx context.Context, tx *sql.Tx, changes []game.OpinionChange) error {
	const upsert = ` ON CONFLICT (senator_id, character_id) DO UPDATE
		SET value = MAX(?, MIN(?, senator_opinions.value + excluded.value))`
	for _, c := range changes {
		if c.Delta == 0 {
			continue
		}
		v := game.ClampOpinion(c.Delta)
		var err error
		if c.SenatorID == 0 {
			_, err = tx.ExecContext(ctx,
				`INSERT INTO senator_opinions (senator_id, character_id, value)
				 SELECT s.id, ?, ? FROM senators s JOIN characters c ON c.guild_id = s.guild_id WHERE c.id = ?`+upsert,
				c.CharacterID, v, c.CharacterID, game.OpinionMin, game.OpinionMax)
		} else {
			_, err = tx.ExecContext(ctx,
				`INSERT INTO senator_opinions (senator_id, character_id, value) VALUES (?, ?, ?)`+upsert,
				c.SenatorID, c.CharacterID, v, game.OpinionMin, game.OpinionMax)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// decayOpinions fades every opinion at a court a little toward 0.
func decayOpinions(ctx context.Context, tx *sql.Tx, guildID string, keep float64) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE senator_opinions SET value = value * ? WHERE senator_id IN (SELECT id FROM senators WHERE guild_id = ?)`,
		keep, guildID)
	return err
}
