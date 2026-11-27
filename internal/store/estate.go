package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/shantz14/idle-kings/internal/game"
)

var (
	ErrOwned        = errors.New("someone at court already owns that")
	ErrNotOwned     = errors.New("you don't own that anymore")
	ErrResidence    = errors.New("you live there; move out first")
	ErrLastProperty = errors.New("that would leave someone with nowhere to live")
	ErrNotProperty  = errors.New("you can only live in a property")
	ErrPartyHome    = errors.New("you can't change homes while hosting a party")
	ErrStale        = errors.New("the offer no longer matches what everyone owns")
)

// ItemOwners maps each owned unique item key in a court to its owner.
func (s *Store) ItemOwners(ctx context.Context, guildID string) (map[string]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT item_key, character_id FROM estate_items WHERE guild_id = ?`, guildID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var k string
		var id int64
		if err := rows.Scan(&k, &id); err != nil {
			return nil, err
		}
		if it, ok := game.ItemByKey(k); ok && it.Unique {
			out[k] = id
		}
	}
	return out, rows.Err()
}

// BuyItem pays the price and adds the item to the character's estate.
// Whether it's in stock is the caller's (game.InStock) business.
func (s *Store) BuyItem(ctx context.Context, charID int64, guildID, key string, price int, now time.Time) (int64, error) {
	it, ok := game.ItemByKey(key)
	if !ok {
		return 0, ErrNotFound
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if it.Unique {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM estate_items WHERE guild_id = ? AND item_key = ?`,
			guildID, key).Scan(&n); err != nil {
			return 0, err
		}
		if n > 0 {
			return 0, ErrOwned
		}
	}
	if err := spend(ctx, tx, charID, price); err != nil {
		return 0, err
	}
	res, err := tx.ExecContext(ctx,
		`INSERT INTO estate_items (character_id, guild_id, item_key, price_paid, acquired_at) VALUES (?, ?, ?, ?, ?)`,
		charID, guildID, key, price, now.Unix())
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	return id, tx.Commit()
}

// SellToShop sells an owned item back to the shop for payout gold. You can't
// sell the property you live in (so you always keep one).
func (s *Store) SellToShop(ctx context.Context, charID, holdingID int64, payout int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var owner, residence int64
	err = tx.QueryRowContext(ctx,
		`SELECT e.character_id, COALESCE(c.residence_id, 0) FROM estate_items e JOIN characters c ON c.id = e.character_id WHERE e.id = ?`,
		holdingID).Scan(&owner, &residence)
	if errors.Is(err, sql.ErrNoRows) || owner != charID {
		return ErrNotOwned
	} else if err != nil {
		return err
	}
	if residence == holdingID {
		return ErrResidence
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM damaged_items WHERE holding_id = ?`, holdingID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM estate_items WHERE id = ?`, holdingID); err != nil {
		return err
	}
	if err := fixResidence(ctx, tx, charID); err != nil { // keeps at least one property
		return err
	}
	if err := credit(ctx, tx, charID, payout); err != nil {
		return err
	}
	if err := cancelOffersWith(ctx, tx, charID, []int64{holdingID}, 0); err != nil {
		return err
	}
	return tx.Commit()
}

// MoveResidence makes an owned property the character's home.
func (s *Store) MoveResidence(ctx context.Context, charID, holdingID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var owner int64
	var key string
	err = tx.QueryRowContext(ctx, `SELECT character_id, item_key FROM estate_items WHERE id = ?`, holdingID).Scan(&owner, &key)
	if errors.Is(err, sql.ErrNoRows) || owner != charID {
		return ErrNotOwned
	} else if err != nil {
		return err
	}
	if it, ok := game.ItemByKey(key); !ok || !it.IsProperty() {
		return ErrNotProperty
	}
	if hosting, err := hostingParty(ctx, tx, charID); err != nil {
		return err
	} else if hosting {
		return ErrPartyHome
	}
	if _, err := tx.ExecContext(ctx, `UPDATE characters SET residence_id = ? WHERE id = ?`, holdingID, charID); err != nil {
		return err
	}
	return tx.Commit()
}

// hostingParty reports whether a character has a party running at home.
func hostingParty(ctx context.Context, tx *sql.Tx, charID int64) (bool, error) {
	var n int
	err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM activities WHERE host_id = ? AND kind = ? AND resolved_at IS NULL`, charID, game.KindParty).Scan(&n)
	return n > 0, err
}

// ---- player deals ----

// EstateOffer is a deal between two players over estate items and gold.
// Gold > 0 is paid by To to From; gold < 0 by From to To.
type EstateOffer struct {
	ID        int64
	GuildID   string
	FromID    int64
	ToID      int64
	Give      []int64 // From's estate_items ids
	Want      []int64 // To's estate_items ids
	Gold      int
	Status    string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// CreateEstateOffer records a pending offer after checking both sides own
// what's on the table.
func (s *Store) CreateEstateOffer(ctx context.Context, o EstateOffer) (EstateOffer, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return o, err
	}
	defer tx.Rollback()
	if err := ownsAll(ctx, tx, o.FromID, o.Give); err != nil {
		return o, err
	}
	if err := ownsAll(ctx, tx, o.ToID, o.Want); err != nil {
		return o, err
	}
	give, _ := json.Marshal(nonNil(o.Give))
	want, _ := json.Marshal(nonNil(o.Want))
	res, err := tx.ExecContext(ctx,
		`INSERT INTO estate_offers (guild_id, from_id, to_id, give, want, gold, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		o.GuildID, o.FromID, o.ToID, string(give), string(want), o.Gold, o.CreatedAt.Unix(), o.ExpiresAt.Unix())
	if err != nil {
		return o, err
	}
	o.ID, _ = res.LastInsertId()
	o.Status = "pending"
	return o, tx.Commit()
}

func nonNil(ids []int64) []int64 {
	if ids == nil {
		return []int64{}
	}
	return ids
}

func ownsAll(ctx context.Context, tx *sql.Tx, charID int64, ids []int64) error {
	for _, id := range ids {
		var owner int64
		err := tx.QueryRowContext(ctx, `SELECT character_id FROM estate_items WHERE id = ?`, id).Scan(&owner)
		if errors.Is(err, sql.ErrNoRows) || owner != charID {
			return ErrStale
		} else if err != nil {
			return err
		}
	}
	return nil
}

const offerCols = `id, guild_id, from_id, to_id, give, want, gold, status, created_at, expires_at`

func scanOffer(row scanner) (EstateOffer, error) {
	var o EstateOffer
	var give, want string
	var created, expires int64
	err := row.Scan(&o.ID, &o.GuildID, &o.FromID, &o.ToID, &give, &want, &o.Gold, &o.Status, &created, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return o, ErrNotFound
	} else if err != nil {
		return o, err
	}
	o.CreatedAt, o.ExpiresAt = time.Unix(created, 0), time.Unix(expires, 0)
	if err := json.Unmarshal([]byte(give), &o.Give); err != nil {
		return o, err
	}
	return o, json.Unmarshal([]byte(want), &o.Want)
}

func (s *Store) GetEstateOffer(ctx context.Context, id int64) (EstateOffer, error) {
	return scanOffer(s.db.QueryRowContext(ctx, `SELECT `+offerCols+` FROM estate_offers WHERE id = ?`, id))
}

// DeclineEstateOffer marks a pending offer declined. ErrHandled if it isn't
// pending any more.
func (s *Store) DeclineEstateOffer(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `UPDATE estate_offers SET status = 'declined' WHERE id = ? AND status = 'pending'`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrHandled
	}
	return nil
}

// AcceptEstateOffer carries out a deal, re-checking everything: both sides
// still own their items, the payer has the gold, both keep somewhere to
// live, and nobody's home changes during their party. Anyone who traded away
// their residence moves into their most valuable remaining property.
func (s *Store) AcceptEstateOffer(ctx context.Context, id int64, now time.Time) (EstateOffer, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return EstateOffer{}, err
	}
	defer tx.Rollback()
	o, err := scanOffer(tx.QueryRowContext(ctx, `SELECT `+offerCols+` FROM estate_offers WHERE id = ?`, id))
	if err != nil {
		return o, err
	}
	if o.Status != "pending" {
		return o, ErrHandled
	}
	if now.After(o.ExpiresAt) {
		return o, ErrEnded
	}
	if err := ownsAll(ctx, tx, o.FromID, o.Give); err != nil {
		return o, err
	}
	if err := ownsAll(ctx, tx, o.ToID, o.Want); err != nil {
		return o, err
	}
	payer, payee, gold := o.ToID, o.FromID, o.Gold
	if gold < 0 {
		payer, payee, gold = o.FromID, o.ToID, -gold
	}
	if err := spend(ctx, tx, payer, gold); err != nil {
		return o, err
	}
	if err := credit(ctx, tx, payee, gold); err != nil {
		return o, err
	}
	move := func(ids []int64, to int64) error {
		for _, hid := range ids {
			if _, err := tx.ExecContext(ctx, `UPDATE estate_items SET character_id = ? WHERE id = ?`, to, hid); err != nil {
				return err
			}
		}
		return nil
	}
	if err := move(o.Give, o.ToID); err != nil {
		return o, err
	}
	if err := move(o.Want, o.FromID); err != nil {
		return o, err
	}
	for _, cid := range []int64{o.FromID, o.ToID} {
		if err := fixResidence(ctx, tx, cid); err != nil {
			return o, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE estate_offers SET status = 'accepted' WHERE id = ?`, o.ID); err != nil {
		return o, err
	}
	if err := cancelOffersWith(ctx, tx, o.FromID, append(append([]int64{}, o.Give...), o.Want...), o.ID); err != nil {
		return o, err
	}
	o.Status = "accepted"
	return o, tx.Commit()
}

// fixResidence makes sure a character still lives in a property they own:
// if not, they move into their most valuable one. No property at all, or a
// forced move during their own party, fails the deal.
func fixResidence(ctx context.Context, tx *sql.Tx, charID int64) error {
	var residence int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(residence_id, 0) FROM characters WHERE id = ?`, charID).Scan(&residence); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id, item_key FROM estate_items WHERE character_id = ?`, charID)
	if err != nil {
		return err
	}
	best, bestValue, stillHome, props := int64(0), -1.0, false, 0
	for rows.Next() {
		var hid int64
		var key string
		if err := rows.Scan(&hid, &key); err != nil {
			rows.Close()
			return err
		}
		it, ok := game.ItemByKey(key)
		if !ok || !it.IsProperty() {
			continue
		}
		props++
		if hid == residence {
			stillHome = true
		}
		if it.Value > bestValue {
			best, bestValue = hid, it.Value
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	switch {
	case props == 0:
		return ErrLastProperty
	case stillHome:
		return nil
	}
	if hosting, err := hostingParty(ctx, tx, charID); err != nil {
		return err
	} else if hosting {
		return ErrPartyHome
	}
	_, err = tx.ExecContext(ctx, `UPDATE characters SET residence_id = ? WHERE id = ?`, best, charID)
	return err
}

// cancelOffersWith cancels pending offers (except keep) that involve any of
// the given estate items, since they can no longer be carried out. Only
// offers in charID's court are looked at.
func cancelOffersWith(ctx context.Context, tx *sql.Tx, charID int64, items []int64, keep int64) error {
	gone := map[int64]bool{}
	for _, id := range items {
		gone[id] = true
	}
	rows, err := tx.QueryContext(ctx, `SELECT `+offerCols+` FROM estate_offers
		 WHERE status = 'pending' AND guild_id = (SELECT guild_id FROM characters WHERE id = ?) AND id != ?`, charID, keep)
	if err != nil {
		return err
	}
	var cancel []int64
	for rows.Next() {
		o, err := scanOffer(rows)
		if err != nil {
			rows.Close()
			return err
		}
		for _, id := range append(append([]int64{}, o.Give...), o.Want...) {
			if gone[id] {
				cancel = append(cancel, o.ID)
				break
			}
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, id := range cancel {
		if _, err := tx.ExecContext(ctx, `UPDATE estate_offers SET status = 'cancelled' WHERE id = ?`, id); err != nil {
			return err
		}
	}
	return nil
}
