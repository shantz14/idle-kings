package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/shantz14/idle-kings/internal/game"
)

// GuildStakes returns every player's stake in a court, by character, and the
// players' combined stake in each sector.
func (s *Store) GuildStakes(ctx context.Context, guildID string) (map[int64]game.Stakes, game.Stakes, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT character_id, sector, stake FROM sector_stakes WHERE guild_id = ?`, guildID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	by, totals := map[int64]game.Stakes{}, game.Stakes{}
	for rows.Next() {
		var id int64
		var sec string
		var st float64
		if err := rows.Scan(&id, &sec, &st); err != nil {
			return nil, nil, err
		}
		if by[id] == nil {
			by[id] = game.Stakes{}
		}
		by[id][game.SectorKey(sec)] = st
		totals[game.SectorKey(sec)] += st
	}
	return by, totals, rows.Err()
}

// fillVentures loads stakes and venture income for the given characters.
// Like fillItems, call it only with no rows open.
func (s *Store) fillVentures(ctx context.Context, chars []*Character) error {
	guilds := map[string][]*Character{}
	for _, c := range chars {
		c.Ventures, c.VentureIncome, c.Laws, c.RealmFallen, c.FallenReason, c.Sabotaged = nil, 0, nil, false, "", nil
		guilds[c.GuildID] = append(guilds[c.GuildID], c)
	}
	for g, cs := range guilds {
		st, err := s.GetRealm(ctx, g)
		if errors.Is(err, ErrNotFound) {
			continue
		} else if err != nil {
			return err
		}
		raw, _, err := s.GuildStakes(ctx, g)
		if err != nil {
			return err
		}
		by, totals := game.EffectiveStakes(st.Laws, raw)
		hurt, err := s.ventureDamage(ctx, g, time.Now())
		if err != nil {
			return err
		}
		for _, c := range cs {
			c.Sabotaged = hurt[c.ID]
			c.Laws = st.Laws
			c.RealmFallen, c.FallenReason = !st.Alive(), st.FallenReason
			for k, stake := range raw[c.ID] {
				d, ok := game.SectorByKey(k)
				if !ok || stake <= 0 {
					continue
				}
				share := game.Share(d, by[c.ID][k], totals[k])
				v := Venture{Stake: stake, Share: share, Income: share * st.Sectors.Revenue(d, st.Trackers, st.Laws)}
				if f, ok := c.Sabotaged[k]; ok {
					v.Income *= f
				}
				if c.Ventures == nil {
					c.Ventures = map[game.SectorKey]Venture{}
				}
				c.Ventures[k] = v
				c.VentureIncome += v.Income
			}
		}
	}
	return nil
}

// fillAll loads everything a Character carries beyond its own row.
func (s *Store) fillAll(ctx context.Context, chars []*Character) error {
	if err := s.fillItems(ctx, chars); err != nil {
		return err
	}
	if err := s.fillConditions(ctx, chars, time.Now()); err != nil {
		return err
	}
	if err := s.fillMonuments(ctx, chars); err != nil {
		return err
	}
	return s.fillVentures(ctx, chars)
}

// Investment is a resolved Invest in a Venture.
type Investment struct {
	ActivityID  int64
	CharacterID int64
	GuildID     string
	Sector      game.SectorKey
	Stake       float64    // stake gained
	Gold        int        // gold invested: grows the sector (SectorDef.Grow)
	Delta       game.Delta // e.g. renown lost to exposure
}

// ApplyInvestment resolves an investment once: the stake, the sector's
// growth and any delta. ErrEnded if it was already resolved.
func (s *Store) ApplyInvestment(ctx context.Context, inv Investment, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := resolveOnce(ctx, tx, inv.ActivityID, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO sector_stakes (character_id, guild_id, sector, stake) VALUES (?, ?, ?, ?)
		 ON CONFLICT (character_id, sector) DO UPDATE SET stake = stake + excluded.stake`,
		inv.CharacterID, inv.GuildID, string(inv.Sector), inv.Stake); err != nil {
		return err
	}
	var raw string
	if err := tx.QueryRowContext(ctx, `SELECT sectors FROM realms WHERE guild_id = ?`, inv.GuildID).Scan(&raw); err != nil {
		return err
	}
	var sec game.SectorState
	if err := json.Unmarshal([]byte(raw), &sec); err != nil {
		return err
	}
	sec = sec.Fill()
	if d, ok := game.SectorByKey(inv.Sector); ok {
		cur := sec[inv.Sector]
		sec.Apply(map[game.SectorKey]game.SectorChange{inv.Sector: {Capacity: d.Grow(cur.Capacity, inv.Gold) - cur.Capacity}})
	}
	b, _ := json.Marshal(sec)
	if _, err := tx.ExecContext(ctx, `UPDATE realms SET sectors = ? WHERE guild_id = ?`, string(b), inv.GuildID); err != nil {
		return err
	}
	if !isZero(inv.Delta) {
		if err := applyDelta(ctx, tx, inv.CharacterID, inv.Delta); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func isZero(d game.Delta) bool { return strings.TrimSpace(d.Describe()) == "" }
