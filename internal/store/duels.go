package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/shantz14/idle-kings/internal/game"
)

var (
	ErrNotYourDuel = errors.New("that challenge isn't yours to answer")
	ErrNoBetting   = errors.New("duelists can't bet on their own duel")
)

// PendingDuelAgainst returns an unanswered challenge to this character.
func (s *Store) PendingDuelAgainst(ctx context.Context, defenderID int64) (Activity, error) {
	return scanActivity(s.db.QueryRowContext(ctx,
		`SELECT `+activityCols+` FROM activities WHERE kind = ? AND goal = ? AND target_id = ? AND resolved_at IS NULL LIMIT 1`,
		game.KindDuel, game.DuelPending, defenderID))
}

// openDuelTx loads a duel in the given state that is still running.
func openDuelTx(ctx context.Context, tx *sql.Tx, activityID int64, state string, now time.Time) (Activity, error) {
	a, err := scanActivity(tx.QueryRowContext(ctx, `SELECT `+activityCols+` FROM activities WHERE id = ?`, activityID))
	if err != nil {
		return a, err
	}
	if a.Kind != game.KindDuel || a.Resolved || a.Goal != state || !now.Before(a.EndsAt) {
		return a, ErrEnded
	}
	return a, nil
}

// AcceptDuel takes the defender's matching wager, sets the bout for
// boutEnds and schedules the duelists' events.
func (s *Store) AcceptDuel(ctx context.Context, activityID, defenderID int64, boutEnds time.Time, events []NewEvent, now time.Time) (Activity, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Activity{}, err
	}
	defer tx.Rollback()
	a, err := openDuelTx(ctx, tx, activityID, game.DuelPending, now)
	if err != nil {
		return a, err
	}
	if a.TargetID != defenderID {
		return a, ErrNotYourDuel
	}
	if a.Spend > 0 {
		if err := spendOr(ctx, tx, defenderID, a.Spend, fmt.Sprintf("you need %d gold to match the wager", a.Spend)); err != nil {
			return a, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE activities SET goal = ?, ends_at = ? WHERE id = ?`,
		game.DuelAccepted, boutEnds.Unix(), a.ID); err != nil {
		return a, err
	}
	if err := insertEvents(ctx, tx, a, events); err != nil {
		return a, err
	}
	a.Goal, a.EndsAt = game.DuelAccepted, boutEnds
	return a, tx.Commit()
}

// DeclineDuel ends a pending challenge: the challenger's wager comes back
// and out (the refusal's cost) is applied. defenderID 0 means it timed out,
// which anyone (the game loop) may resolve.
func (s *Store) DeclineDuel(ctx context.Context, activityID, defenderID int64, out Outcome, now time.Time) (Activity, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Activity{}, err
	}
	defer tx.Rollback()
	a, err := scanActivity(tx.QueryRowContext(ctx, `SELECT `+activityCols+` FROM activities WHERE id = ?`, activityID))
	if err != nil {
		return a, err
	}
	if a.Kind != game.KindDuel || a.Resolved || a.Goal != game.DuelPending {
		return a, ErrEnded
	}
	if defenderID != 0 && a.TargetID != defenderID {
		return a, ErrNotYourDuel
	}
	res, err := tx.ExecContext(ctx, `UPDATE activities SET resolved_at = ? WHERE id = ? AND resolved_at IS NULL`, now.Unix(), a.ID)
	if err != nil {
		return a, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return a, ErrEnded
	}
	if err := credit(ctx, tx, a.HostID, a.Spend); err != nil {
		return a, err
	}
	if err := applyOutcome(ctx, tx, a.GuildID, out, now); err != nil {
		return a, err
	}
	a.Resolved = true
	return a, tx.Commit()
}

// PlaceBet stakes a spectator's gold on one duelist of an accepted duel.
// One bet per player per duel.
func (s *Store) PlaceBet(ctx context.Context, activityID, charID, sideID int64, amount int, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	a, err := openDuelTx(ctx, tx, activityID, game.DuelAccepted, now)
	if err != nil {
		return err
	}
	if charID == a.HostID || charID == a.TargetID {
		return ErrNoBetting
	}
	if sideID != a.HostID && sideID != a.TargetID {
		return fmt.Errorf("that player isn't fighting in this duel")
	}
	if amount <= 0 {
		return fmt.Errorf("bet at least 1 gold")
	}
	res, err := tx.ExecContext(ctx,
		`INSERT INTO bets (activity_id, character_id, side_id, amount, placed_at) VALUES (?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
		a.ID, charID, sideID, amount, now.Unix())
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrExists
	}
	if err := spendOr(ctx, tx, charID, amount, fmt.Sprintf("you need %d gold for that bet", amount)); err != nil {
		return err
	}
	return tx.Commit()
}

// DuelBets lists the bets on a duel.
func (s *Store) DuelBets(ctx context.Context, activityID int64) ([]game.Bet, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT character_id, side_id, amount FROM bets WHERE activity_id = ? ORDER BY placed_at`, activityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []game.Bet
	for rows.Next() {
		var b game.Bet
		if err := rows.Scan(&b.CharacterID, &b.SideID, &b.Amount); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
