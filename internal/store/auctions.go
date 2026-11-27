package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/shantz14/idle-kings/internal/game"
)

// ---- auctions ----

// Auction is one piece for sale to the highest bidder.
type Auction struct {
	ID        int64
	GuildID   string
	ItemKey   string
	SellerID  int64 // 0 = a merchant (NPC)
	HoldingID int64 // the seller's estate_items id (0 for an NPC)
	Reserve   int
	TopBid    int
	TopBidder int64
	StartedAt time.Time
	EndsAt    time.Time
	Settled   bool
	ChannelID string
	MessageID string
}

// MinBid is the least the next bid can be.
func (a Auction) MinBid() int { return game.MinBid(a.TopBid, a.Reserve) }

var (
	ErrOnAuction = errors.New("that's up for auction until the bidding ends")
	ErrLowBid    = errors.New("someone has bid more; look again")
	ErrOwnBid    = errors.New("you can't bid on your own piece, or outbid yourself")
)

const auctionCols = `id, guild_id, item_key, seller_id, holding_id, reserve, top_bid, top_bidder, started_at, ends_at, settled_at, COALESCE(channel_id, ''), COALESCE(message_id, '')`

func scanAuction(r scanner) (Auction, error) {
	var a Auction
	var started, ends int64
	var settled sql.NullInt64
	err := r.Scan(&a.ID, &a.GuildID, &a.ItemKey, &a.SellerID, &a.HoldingID, &a.Reserve, &a.TopBid, &a.TopBidder,
		&started, &ends, &settled, &a.ChannelID, &a.MessageID)
	a.StartedAt, a.EndsAt, a.Settled = time.Unix(started, 0), time.Unix(ends, 0), settled.Valid
	return a, err
}

// onAuction returns ErrOnAuction if any of the holdings is in an open auction.
func onAuction(ctx context.Context, tx *sql.Tx, holdingIDs ...int64) error {
	for _, id := range holdingIDs {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM auctions WHERE holding_id = ? AND settled_at IS NULL`, id).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return ErrOnAuction
		}
	}
	return nil
}

// OpenAuction starts an auction. A merchant's lot must be one nobody at the
// court owns; a player's piece must be theirs, auctionable, and not already
// on sale.
func (s *Store) OpenAuction(ctx context.Context, a Auction) (Auction, error) {
	it, ok := game.ItemByKey(a.ItemKey)
	if !ok {
		return a, ErrNotFound
	}
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		if a.SellerID == 0 {
			var n int
			if err := tx.QueryRowContext(ctx,
				`SELECT (SELECT COUNT(*) FROM estate_items WHERE guild_id = ? AND item_key = ?) +
				        (SELECT COUNT(*) FROM auctions WHERE guild_id = ? AND item_key = ? AND settled_at IS NULL)`,
				a.GuildID, a.ItemKey, a.GuildID, a.ItemKey).Scan(&n); err != nil {
				return err
			}
			if n > 0 {
				return ErrOwned
			}
		} else {
			if !game.Auctionable(it) {
				return errors.New("only a unique possession can go up for auction")
			}
			var key string
			err := tx.QueryRowContext(ctx, `SELECT item_key FROM estate_items WHERE id = ? AND character_id = ?`, a.HoldingID, a.SellerID).Scan(&key)
			if errors.Is(err, sql.ErrNoRows) || key != a.ItemKey {
				return ErrNotOwned
			} else if err != nil {
				return err
			}
			if err := onAuction(ctx, tx, a.HoldingID); err != nil {
				return err
			}
			if err := cancelOffersWith(ctx, tx, a.SellerID, []int64{a.HoldingID}, 0); err != nil {
				return err
			}
		}
		res, err := tx.ExecContext(ctx,
			`INSERT INTO auctions (guild_id, item_key, seller_id, holding_id, reserve, started_at, ends_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			a.GuildID, a.ItemKey, a.SellerID, a.HoldingID, a.Reserve, a.StartedAt.Unix(), a.EndsAt.Unix())
		if err != nil {
			return err
		}
		a.ID, _ = res.LastInsertId()
		return nil
	})
	return a, err
}

// SetAuctionMessage records where an auction was announced.
func (s *Store) SetAuctionMessage(ctx context.Context, id int64, channelID, messageID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE auctions SET channel_id = ?, message_id = ? WHERE id = ?`, channelID, messageID, id)
	return err
}

// GetAuction loads one auction.
func (s *Store) GetAuction(ctx context.Context, id int64) (Auction, error) {
	a, err := scanAuction(s.db.QueryRowContext(ctx, `SELECT `+auctionCols+` FROM auctions WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}

// OpenAuctions are a court's auctions still taking bids.
func (s *Store) OpenAuctions(ctx context.Context, guildID string) ([]Auction, error) {
	return queryAll(ctx, s.db, `SELECT `+auctionCols+` FROM auctions WHERE guild_id = ? AND settled_at IS NULL ORDER BY ends_at`, scanAuction, guildID)
}

// DueAuctions are auctions in any court whose time is up.
func (s *Store) DueAuctions(ctx context.Context, now time.Time) ([]Auction, error) {
	return queryAll(ctx, s.db, `SELECT `+auctionCols+` FROM auctions WHERE settled_at IS NULL AND ends_at <= ?`, scanAuction, now.Unix())
}

// Bid raises an auction's top bid: the bid is held from the bidder and the
// bidder it beats gets theirs back. It returns the auction before the bid
// (its TopBidder is the one outbid).
func (s *Store) Bid(ctx context.Context, id, bidderID int64, amount int, now time.Time) (Auction, error) {
	var before Auction
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		a, err := scanAuction(tx.QueryRowContext(ctx, `SELECT `+auctionCols+` FROM auctions WHERE id = ?`, id))
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		before = a
		switch {
		case a.Settled || !now.Before(a.EndsAt):
			return ErrEnded
		case bidderID == a.SellerID || bidderID == a.TopBidder:
			return ErrOwnBid
		case amount < a.MinBid():
			return ErrLowBid
		}
		if err := spendOr(ctx, tx, bidderID, amount, fmt.Sprintf("you need %d gold to bid that", amount)); err != nil {
			return err
		}
		if a.TopBidder != 0 {
			if err := credit(ctx, tx, a.TopBidder, a.TopBid); err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, `UPDATE auctions SET top_bid = ?, top_bidder = ? WHERE id = ?`, amount, bidderID, id)
		return err
	})
	return before, err
}

// SettleAuction ends an auction once: the piece goes to the top bidder and
// the gold to the seller (a merchant keeps it). With no bid the piece stays
// where it was. If a player's piece is somehow gone, the bid is refunded.
// sold is whether it changed hands. ErrEnded if already settled.
func (s *Store) SettleAuction(ctx context.Context, id int64, now time.Time) (a Auction, sold bool, err error) {
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE auctions SET settled_at = ? WHERE id = ? AND settled_at IS NULL`, now.Unix(), id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrEnded
		}
		if a, err = scanAuction(tx.QueryRowContext(ctx, `SELECT `+auctionCols+` FROM auctions WHERE id = ?`, id)); err != nil {
			return err
		}
		if a.TopBidder == 0 {
			return nil
		}
		if a.SellerID == 0 {
			_, err := tx.ExecContext(ctx,
				`INSERT INTO estate_items (character_id, guild_id, item_key, price_paid, acquired_at) VALUES (?, ?, ?, ?, ?)`,
				a.TopBidder, a.GuildID, a.ItemKey, a.TopBid, now.Unix())
			sold = err == nil
			return err
		}
		res, err = tx.ExecContext(ctx, `UPDATE estate_items SET character_id = ?, price_paid = ? WHERE id = ? AND character_id = ?`,
			a.TopBidder, a.TopBid, a.HoldingID, a.SellerID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 { // gone (stolen): the bidder gets their gold back
			return credit(ctx, tx, a.TopBidder, a.TopBid)
		}
		sold = true
		if err := cancelOffersWith(ctx, tx, a.SellerID, []int64{a.HoldingID}, 0); err != nil {
			return err
		}
		return credit(ctx, tx, a.SellerID, a.TopBid)
	})
	return a, sold, err
}
