package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/shantz14/idle-kings/internal/game"
)

// ---- sabotage ----

// ventureDamage is each character's sabotaged ventures in a court: the
// lowest income factor still in force per sector.
func (s *Store) ventureDamage(ctx context.Context, guildID string, now time.Time) (map[int64]map[game.SectorKey]float64, error) {
	type row struct {
		id     int64
		sector string
		factor float64
	}
	rows, err := queryAll(ctx, s.db,
		`SELECT character_id, sector, MIN(factor) FROM venture_damage WHERE guild_id = ? AND ends_at > ? GROUP BY character_id, sector`,
		func(r scanner) (row, error) {
			var x row
			err := r.Scan(&x.id, &x.sector, &x.factor)
			return x, err
		}, guildID, now.Unix())
	if err != nil {
		return nil, err
	}
	out := map[int64]map[game.SectorKey]float64{}
	for _, x := range rows {
		if out[x.id] == nil {
			out[x.id] = map[game.SectorKey]float64{}
		}
		out[x.id][game.SectorKey(x.sector)] = x.factor
	}
	return out, nil
}

// Sabotage is what a successful sabotage damages: a venture (Sector) or a
// property (HoldingID).
type Sabotage struct {
	GuildID     string
	CharacterID int64 // the victim
	Sector      game.SectorKey
	Factor      float64
	Until       time.Time
	HoldingID   int64
	RepairCost  int
}

// applySabotage damages a venture or a property. A property already damaged
// (or no longer theirs) is left as it is.
func applySabotage(ctx context.Context, tx *sql.Tx, sb Sabotage, now time.Time) error {
	if sb.HoldingID != 0 {
		_, err := tx.ExecContext(ctx, `INSERT INTO damaged_items (holding_id, repair_cost, damaged_at)
			SELECT id, ?, ? FROM estate_items WHERE id = ? AND character_id = ? ON CONFLICT DO NOTHING`,
			sb.RepairCost, now.Unix(), sb.HoldingID, sb.CharacterID)
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO venture_damage (guild_id, character_id, sector, factor, ends_at) VALUES (?, ?, ?, ?, ?)`,
		sb.GuildID, sb.CharacterID, string(sb.Sector), sb.Factor, sb.Until.Unix())
	return err
}

// RepairItem pays to repair a damaged holding.
func (s *Store) RepairItem(ctx context.Context, charID, holdingID int64) (int, error) {
	var cost int
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, `SELECT d.repair_cost FROM damaged_items d JOIN estate_items e ON e.id = d.holding_id
			WHERE d.holding_id = ? AND e.character_id = ?`, holdingID, charID).Scan(&cost)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if err := spendOr(ctx, tx, charID, cost, fmt.Sprintf("the repairs cost %d gold", cost)); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM damaged_items WHERE holding_id = ?`, holdingID)
		return err
	})
	return cost, err
}
