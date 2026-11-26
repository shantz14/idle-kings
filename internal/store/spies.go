package store

import (
	"context"
	"database/sql"

	"github.com/shantz14/idle-kings/internal/game"
)

// SpyNetwork is one player's spies in another player's court.
type SpyNetwork struct {
	OwnerID  int64
	TargetID int64
	Value    float64
}

// SpyLevel is owner's spy level in target's court (0 if none).
func (s *Store) SpyLevel(ctx context.Context, ownerID, targetID int64) (float64, error) {
	var v float64
	err := s.db.QueryRowContext(ctx,
		`SELECT value FROM spy_networks WHERE owner_id = ? AND target_id = ?`, ownerID, targetID).Scan(&v)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return v, err
}

func (s *Store) spyNetworks(ctx context.Context, where string, arg int64) ([]SpyNetwork, error) {
	return queryAll(ctx, s.db,
		`SELECT owner_id, target_id, value FROM spy_networks WHERE `+where+` AND value > 0 ORDER BY value DESC`,
		func(r scanner) (SpyNetwork, error) {
			var n SpyNetwork
			return n, r.Scan(&n.OwnerID, &n.TargetID, &n.Value)
		}, arg)
}

// SpyNetworksOn lists every network in the target's court.
func (s *Store) SpyNetworksOn(ctx context.Context, targetID int64) ([]SpyNetwork, error) {
	return s.spyNetworks(ctx, `target_id = ?`, targetID)
}

// SpyNetworksBy lists every network the owner runs.
func (s *Store) SpyNetworksBy(ctx context.Context, ownerID int64) ([]SpyNetwork, error) {
	return s.spyNetworks(ctx, `owner_id = ?`, ownerID)
}

// ChangeSpies applies spy changes outside any other transaction.
func (s *Store) ChangeSpies(ctx context.Context, changes []game.SpyChange) error {
	return s.withTx(ctx, func(tx *sql.Tx) error { return changeSpies(ctx, tx, changes) })
}

// changeSpies adds each change, clamped to 0..100.
func changeSpies(ctx context.Context, tx *sql.Tx, changes []game.SpyChange) error {
	for _, c := range changes {
		if c.Delta == 0 || c.Owner == c.Target {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO spy_networks (owner_id, target_id, value) VALUES (?, ?, ?)
			 ON CONFLICT (owner_id, target_id) DO UPDATE
			 SET value = MAX(?, MIN(?, spy_networks.value + ?))`,
			c.Owner, c.Target, game.ClampSpy(c.Delta), game.SpyMin, game.SpyMax, c.Delta); err != nil {
			return err
		}
	}
	return nil
}

// decaySpies fades every network at a court a little; tiny ones vanish.
func decaySpies(ctx context.Context, tx *sql.Tx, guildID string, keep float64) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE spy_networks SET value = CASE WHEN value * ? < 0.5 THEN 0 ELSE value * ? END
		 WHERE owner_id IN (SELECT id FROM characters WHERE guild_id = ?)`,
		keep, keep, guildID)
	return err
}
