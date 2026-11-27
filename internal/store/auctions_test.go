package store

import (
	"errors"
	"testing"
	"time"

	"github.com/shantz14/idle-kings/internal/game"
)

func TestNPCAuction(t *testing.T) {
	s, ctx := estateStore(t)
	now := time.Unix(1_000_000, 0)
	a, b := newChar(t, s, "a", "merchant"), newChar(t, s, "b", "merchant")
	give(t, s, a, 1000)
	give(t, s, b, 1000)
	lot := game.AuctionLots[0]
	au, err := s.OpenAuction(ctx, Auction{GuildID: "g", ItemKey: lot.Key, Reserve: 100, StartedAt: now, EndsAt: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.OpenAuction(ctx, Auction{GuildID: "g", ItemKey: lot.Key, Reserve: 100, StartedAt: now, EndsAt: now.Add(time.Hour)}); !errors.Is(err, ErrOwned) {
		t.Fatalf("the same lot twice: %v", err)
	}
	if _, err := s.Bid(ctx, au.ID, a.ID, 99, now); !errors.Is(err, ErrLowBid) {
		t.Fatalf("under the reserve: %v", err)
	}
	if _, err := s.Bid(ctx, au.ID, a.ID, 100, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Bid(ctx, au.ID, a.ID, 200, now); !errors.Is(err, ErrOwnBid) {
		t.Fatalf("outbidding yourself: %v", err)
	}
	if _, err := s.Bid(ctx, au.ID, b.ID, 105, now); !errors.Is(err, ErrLowBid) {
		t.Fatalf("too small a step: %v", err)
	}
	prev, err := s.Bid(ctx, au.ID, b.ID, 110, now)
	if err != nil || prev.TopBidder != a.ID {
		t.Fatalf("outbid: %+v %v", prev, err)
	}
	ra, rb := reload(t, s, a), reload(t, s, b)
	if ra.Wealth != a.Wealth+1000 || rb.Wealth != b.Wealth+1000-110 {
		t.Fatalf("held gold: %d %d", ra.Wealth-a.Wealth, rb.Wealth-b.Wealth)
	}
	if _, err := s.Bid(ctx, au.ID, a.ID, 500, now.Add(time.Hour)); !errors.Is(err, ErrEnded) {
		t.Fatalf("after the end: %v", err)
	}
	if due, _ := s.DueAuctions(ctx, now.Add(time.Hour)); len(due) != 1 {
		t.Fatalf("due %v", due)
	}
	got, sold, err := s.SettleAuction(ctx, au.ID, now.Add(time.Hour))
	if err != nil || !sold || got.TopBidder != b.ID {
		t.Fatalf("settle %+v %v %v", got, sold, err)
	}
	if !hasItem(reload(t, s, b), lot.Key) {
		t.Fatal("the winner should have it")
	}
	if _, _, err := s.SettleAuction(ctx, au.ID, now.Add(time.Hour)); !errors.Is(err, ErrEnded) {
		t.Fatalf("settling twice: %v", err)
	}
}

func TestPlayerAuction(t *testing.T) {
	s, ctx := estateStore(t)
	now := time.Unix(1_000_000, 0)
	seller, buyer := newChar(t, s, "seller", "merchant"), newChar(t, s, "buyer", "merchant")
	give(t, s, buyer, 2000)
	lot := game.AuctionLots[1]
	hid, err := s.BuyItem(ctx, seller.ID, "g", lot.Key, 0, now)
	if err != nil {
		t.Fatal(err)
	}
	au, err := s.OpenAuction(ctx, Auction{GuildID: "g", ItemKey: lot.Key, SellerID: seller.ID, HoldingID: hid, Reserve: 300, StartedAt: now, EndsAt: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SellToShop(ctx, seller.ID, hid, 10); !errors.Is(err, ErrOnAuction) {
		t.Fatalf("selling a piece on auction: %v", err)
	}
	if _, err := s.Bid(ctx, au.ID, seller.ID, 300, now); !errors.Is(err, ErrOwnBid) {
		t.Fatalf("bidding on your own: %v", err)
	}
	if _, err := s.Bid(ctx, au.ID, buyer.ID, 400, now); err != nil {
		t.Fatal(err)
	}
	if _, sold, err := s.SettleAuction(ctx, au.ID, now.Add(time.Hour)); err != nil || !sold {
		t.Fatalf("settle %v %v", sold, err)
	}
	rs, rb := reload(t, s, seller), reload(t, s, buyer)
	if !hasItem(rb, lot.Key) || hasItem(rs, lot.Key) || rs.Wealth != seller.Wealth+400 {
		t.Fatalf("after the sale: seller %+v buyer %+v", rs.Items, rb.Items)
	}

	// No bids: the piece stays with its owner.
	au, _ = s.OpenAuction(ctx, Auction{GuildID: "g", ItemKey: lot.Key, SellerID: buyer.ID, HoldingID: hid, Reserve: 300, StartedAt: now, EndsAt: now.Add(time.Hour)})
	if _, sold, err := s.SettleAuction(ctx, au.ID, now.Add(time.Hour)); err != nil || sold || !hasItem(reload(t, s, buyer), lot.Key) {
		t.Fatalf("unsold: %v %v", sold, err)
	}
}

func hasItem(c Character, key string) bool {
	for _, k := range c.Items {
		if k == key {
			return true
		}
	}
	return false
}
