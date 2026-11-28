package game

import "testing"

func TestAuctionRules(t *testing.T) {
	if MinBid(0, 120) != 120 || MinBid(100, 120) != 110 || MinBid(20, 10) != 25 {
		t.Fatalf("min bids %d %d %d", MinBid(0, 120), MinBid(100, 120), MinBid(20, 10))
	}
	seen := map[string]bool{}
	for _, it := range AuctionLots {
		got, ok := ItemByKey(it.Key)
		if !ok || got.Shelf != ShelfNone || !got.Unique || seen[it.Key] {
			t.Errorf("%s: must be a unique piece the shop never sells", it.Key)
		}
		seen[it.Key] = true
		if it.Price < 300 || it.Price > 900 || NPCReserve(it) >= it.Price/2 {
			t.Errorf("%s: price %d, opening %d", it.Key, it.Price, NPCReserve(it))
		}
		if !Auctionable(it) {
			t.Errorf("%s: not auctionable", it.Key)
		}
		fx := it.Effects
		if fx.PartyBonus > 2 || fx.DailyIncome > 3 || fx.VoteWeight > 1 || fx.RumorNotice > 2 {
			t.Errorf("%s is too strong for something that may go cheap: %+v", it.Key, fx)
		}
		for _, v := range fx.Stats {
			if v > 5 {
				t.Errorf("%s: +%v to a stat is too much", it.Key, v)
			}
		}
	}
	if len(AuctionLots) < 10 {
		t.Errorf("only %d lots", len(AuctionLots))
	}
	owned := map[string]bool{}
	for _, it := range AuctionLots[1:] {
		owned[it.Key] = true
	}
	if it, ok := PickLot(rolls(t, 0), owned); !ok || it.Key != AuctionLots[0].Key {
		t.Fatalf("the one left: %v %v", it.Key, ok)
	}
	owned[AuctionLots[0].Key] = true
	if _, ok := PickLot(rolls(t, 0), owned); ok {
		t.Fatal("all sold")
	}
	for _, it := range Items {
		if Auctionable(it) && it.IsProperty() {
			t.Fatal("houses aren't auctioned")
		}
	}
}
