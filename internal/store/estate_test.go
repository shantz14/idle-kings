package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func estateStore(t *testing.T) (*Store, context.Context) {
	t.Helper()
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	_ = s.ClaimAdmin(ctx, "g", "a")
	return s, ctx
}

func give(t *testing.T, s *Store, c Character, gold int) {
	t.Helper()
	if _, err := s.db.Exec(`UPDATE characters SET wealth = wealth + ? WHERE id = ?`, gold, c.ID); err != nil {
		t.Fatal(err)
	}
}

func reload(t *testing.T, s *Store, c Character) Character {
	t.Helper()
	got, err := s.GetCharacterByID(context.Background(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestBuySellMove(t *testing.T) {
	s, ctx := estateStore(t)
	now := time.Now()
	a := newChar(t, s, "a", "merchant") // townhouse + ledger, 100 gold
	b := newChar(t, s, "b", "officer")
	give(t, s, a, 2000)
	home, ok := a.Residence()
	if !ok || home.Key != "townhouse" || a.Items[0] != "townhouse" {
		t.Fatalf("starting residence: %+v %v", a.Holdings, a.ResidenceID)
	}

	villa, err := s.BuyItem(ctx, a.ID, "g", "bag_end", 800, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.BuyItem(ctx, b.ID, "g", "bag_end", 800, now); !errors.Is(err, ErrOwned) {
		t.Fatalf("second buyer of a unique piece: %v", err)
	}
	if _, err := s.BuyItem(ctx, b.ID, "g", "winterfell", 5000, now); err == nil {
		t.Fatal("bought a castle without the gold")
	}
	if _, err := s.BuyItem(ctx, b.ID, "g", "gladius", 300, now); err == nil {
		t.Fatal("officer can't afford a gladius")
	}
	// Common goods can be owned by many.
	give(t, s, b, 1000)
	if _, err := s.BuyItem(ctx, a.ID, "g", "gladius", 300, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BuyItem(ctx, b.ID, "g", "gladius", 300, now); err != nil {
		t.Fatal(err)
	}
	owners, _ := s.ItemOwners(ctx, "g")
	if owners["bag_end"] != a.ID || owners["gladius"] != 0 {
		t.Fatalf("owners %v", owners)
	}

	// Living in the villa: it counts fully, the townhouse half.
	before := reload(t, s, a)
	if err := s.MoveResidence(ctx, a.ID, villa); err != nil {
		t.Fatal(err)
	}
	after := reload(t, s, a)
	if after.Items[0] != "bag_end" || after.EstateValue() <= before.EstateValue() {
		t.Fatalf("after moving: %v, value %v → %v", after.Items, before.EstateValue(), after.EstateValue())
	}
	ledger := after.Holdings[len(after.Holdings)-1]
	for _, h := range after.Holdings {
		if h.Key == "ledger" {
			ledger = h
		}
	}
	if err := s.MoveResidence(ctx, a.ID, ledger.ID); !errors.Is(err, ErrNotProperty) {
		t.Fatalf("moved into a ledger: %v", err)
	}

	// Sell-back: never the residence, and the gold comes back.
	if err := s.SellToShop(ctx, a.ID, villa, 480); !errors.Is(err, ErrResidence) {
		t.Fatalf("sold the residence: %v", err)
	}
	if err := s.SellToShop(ctx, b.ID, home.ID, 1); !errors.Is(err, ErrNotOwned) {
		t.Fatalf("sold someone else's house: %v", err)
	}
	w := reload(t, s, a).Wealth
	if err := s.SellToShop(ctx, a.ID, home.ID, 72); err != nil {
		t.Fatal(err)
	}
	if got := reload(t, s, a); got.Wealth != w+72 || len(got.Holdings) != len(after.Holdings)-1 {
		t.Fatalf("after selling: %+v", got)
	}
	if owners, _ := s.ItemOwners(ctx, "g"); owners["bag_end"] != a.ID {
		t.Fatal("villa should still be a's")
	}
}

func TestEstateOffers(t *testing.T) {
	s, ctx := estateStore(t)
	now := time.Now()
	a := newChar(t, s, "a", "merchant")
	b := newChar(t, s, "b", "officer")
	give(t, s, a, 2000)
	villa, _ := s.BuyItem(ctx, a.ID, "g", "bag_end", 800, now)
	aHome, _ := a.Residence()
	bHome, _ := b.Residence()
	offer := func(o EstateOffer) EstateOffer {
		t.Helper()
		o.GuildID, o.CreatedAt, o.ExpiresAt = "g", now, now.Add(time.Hour)
		got, err := s.CreateEstateOffer(ctx, o)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}

	// Offers must match what people own.
	if _, err := s.CreateEstateOffer(ctx, EstateOffer{GuildID: "g", FromID: b.ID, ToID: a.ID, Give: []int64{villa},
		CreatedAt: now, ExpiresAt: now.Add(time.Hour)}); !errors.Is(err, ErrStale) {
		t.Fatalf("offered someone else's villa: %v", err)
	}

	// b can't pay 500 for the villa.
	o := offer(EstateOffer{FromID: a.ID, ToID: b.ID, Give: []int64{villa}, Gold: 500})
	if _, err := s.AcceptEstateOffer(ctx, o.ID, now); err == nil {
		t.Fatal("accepted without the gold")
	}
	give(t, s, b, 600)
	bw := reload(t, s, b).Wealth
	if _, err := s.AcceptEstateOffer(ctx, o.ID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AcceptEstateOffer(ctx, o.ID, now); !errors.Is(err, ErrHandled) {
		t.Fatalf("accepted twice: %v", err)
	}
	gotB := reload(t, s, b)
	if _, ok := gotB.Holding(villa); !ok || gotB.Wealth != bw-500 {
		t.Fatalf("b after buying: %+v", gotB)
	}

	// A trade that leaves a with no property is refused.
	o = offer(EstateOffer{FromID: a.ID, ToID: b.ID, Give: []int64{aHome.ID}})
	if _, err := s.AcceptEstateOffer(ctx, o.ID, now); !errors.Is(err, ErrLastProperty) {
		t.Fatalf("left a homeless: %v", err)
	}

	// Trading b's home away moves b into the villa.
	o = offer(EstateOffer{FromID: a.ID, ToID: b.ID, Want: []int64{bHome.ID}, Gold: -50})
	if _, err := s.AcceptEstateOffer(ctx, o.ID, now); err != nil {
		t.Fatal(err)
	}
	if got := reload(t, s, b); got.ResidenceID != villa {
		t.Fatalf("b should live in the villa now: %d", got.ResidenceID)
	}

	// Selling an item cancels offers that involve it.
	o2 := offer(EstateOffer{FromID: a.ID, ToID: b.ID, Give: []int64{bHome.ID}, Gold: 10})
	if err := s.SellToShop(ctx, a.ID, bHome.ID, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AcceptEstateOffer(ctx, o2.ID, now); !errors.Is(err, ErrHandled) {
		t.Fatalf("offer on a sold item: %v", err)
	}

	// Expired offers can't be accepted.
	o3 := offer(EstateOffer{FromID: b.ID, ToID: a.ID, Gold: -1})
	if _, err := s.AcceptEstateOffer(ctx, o3.ID, now.Add(2*time.Hour)); !errors.Is(err, ErrEnded) {
		t.Fatalf("accepted an expired offer: %v", err)
	}
}
