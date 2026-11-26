package store

import (
	"context"
	"time"

	"github.com/shantz14/idle-kings/internal/game"
)

// HireBandits pays a band to waylay a traveller. The hirer is stored as a
// participant on the journey (goal "bandits:<band>"), so each player can
// hire one band per journey. Nobody else is told.
func (s *Store) HireBandits(ctx context.Context, journeyID, hirerID int64, band game.BanditBand, now time.Time) error {
	a, err := s.GetActivity(ctx, journeyID)
	if err != nil {
		return err
	}
	switch {
	case a.Kind != game.KindJourney:
		return ErrNotFound
	case a.Resolved || !now.Before(a.EndsAt):
		return ErrEnded
	case a.HostID == hirerID:
		return ErrOwnEvent
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx,
		`INSERT INTO participants (activity_id, character_id, goal, target_id, joined_at)
		 SELECT ?, ?, ?, ?, ? WHERE EXISTS (SELECT 1 FROM activities WHERE id = ? AND resolved_at IS NULL AND ends_at > ?)
		 ON CONFLICT DO NOTHING`, journeyID, hirerID, game.BanditGoal(band.Key), nil, now.Unix(), journeyID, now.Unix())
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return s.whyNotJoined(ctx, tx, journeyID, hirerID)
	}
	if err := spend(ctx, tx, hirerID, band.Price); err != nil {
		return err
	}
	return tx.Commit()
}
