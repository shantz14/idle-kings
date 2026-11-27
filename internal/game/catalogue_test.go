package game

import (
	"testing"
	"time"
)

func TestCatalogue(t *testing.T) {
	seen := map[string]bool{}
	tiers := map[Tier]bool{}
	for _, it := range Items {
		if seen[it.Key] {
			t.Errorf("duplicate item %s", it.Key)
		}
		seen[it.Key] = true
		if it.Price <= 0 || it.Value <= 0 || it.Name == "" || it.Description == "" {
			t.Errorf("%s: missing price, value, name or description", it.Key)
		}
		if it.Shelf == ShelfMarket && !it.Unique {
			t.Errorf("%s: market pieces are unique", it.Key)
		}
		if it.IsProperty() {
			if it.Shelf != ShelfNone && it.Tier == 0 {
				t.Errorf("%s: shop property without a tier", it.Key)
			}
			tiers[it.Tier] = true
		}
		if it.SellBack() >= it.Price {
			t.Errorf("%s: sells back for more than it costs", it.Key)
		}
	}
	for tr := TierHumble; tr <= TierCastle; tr++ {
		if !tiers[tr] {
			t.Errorf("no %s properties", tr)
		}
	}
	for _, bg := range Backgrounds {
		for _, k := range bg.EstateItems() {
			if it, ok := ItemByKey(k); !ok || it.Shelf != ShelfNone || it.Unique {
				t.Errorf("starting item %s should exist, not be sold, and not be unique", k)
			}
		}
	}
}

func TestEstateValueResidence(t *testing.T) {
	// Residence first: the villa counts fully, the townhouse half.
	villa, _ := ItemByKey("bag_end")
	town, _ := ItemByKey("townhouse")
	relic, _ := ItemByKey("relic")
	got := EstateValue([]string{"bag_end", "townhouse", "relic"})
	if want := villa.Value + town.Value*OtherPropertyShare + relic.Value; got != want {
		t.Fatalf("value %v, want %v", got, want)
	}
	// A property you don't live in gives income but not its party bonus.
	fx := SumEffects([]string{"townhouse", "bag_end", "arbor_vineyards"})
	if fx.PartyBonus != 0 || fx.DailyIncome != 3+8 {
		t.Fatalf("effects %+v", fx)
	}
	if fx := SumEffects([]string{"bag_end", "townhouse"}); fx.PartyBonus != 2 || fx.DailyIncome != 3 {
		t.Fatalf("living in the villa: %+v", fx)
	}
}

func TestEstateScoreDiminishes(t *testing.T) {
	if EstateScore(0) != 0 || EstateScore(10) >= 10 || EstateScore(10) < 8 {
		t.Fatalf("score(10) = %v", EstateScore(10))
	}
	if EstateScore(120)-EstateScore(90) >= EstateScore(30)-EstateScore(0) {
		t.Fatal("should diminish")
	}
	// An average courtier in a castle full of art does well, but isn't certain.
	avg := Stats{Diplomacy: 5}
	castle := NewSheet(avg, 6, 3, EstateValue([]string{"camelot", "venus_de_milo", "winged_victory"}), 0)
	cottage := NewSheet(avg, 6, 3, EstateValue([]string{"forum_rooms"}), 0)
	c, h := PartyChance(castle, PartyMinSpend), PartyChance(cottage, PartyMinSpend)
	if c <= h+30 || c >= MaxChance {
		t.Fatalf("castle %d%% vs rooms %d%%", c, h)
	}
}

func TestMarketRotation(t *testing.T) {
	day := 24 * time.Hour
	now := time.Date(2026, 11, 26, 15, 0, 0, 0, time.UTC)
	p, ends := MarketPeriod(now, day)
	if !ends.After(now) || ends.Sub(now) > day {
		t.Fatalf("period ends %v", ends)
	}
	a, b := Market("g", p), Market("g", p)
	if len(a) != MarketPerSection*len(Sections) {
		t.Fatalf("%d pieces", len(a))
	}
	for i := range a {
		if a[i].Key != b[i].Key {
			t.Fatal("same period, different stock")
		}
	}
	per := map[Section]int{}
	for _, it := range a {
		per[it.Section()]++
		if it.Shelf != ShelfMarket {
			t.Errorf("%s is not a market piece", it.Key)
		}
		if !InStock(it.Key, "g", p) {
			t.Errorf("%s listed but not in stock", it.Key)
		}
	}
	for _, sec := range Sections {
		if per[sec.Key] != MarketPerSection {
			t.Fatalf("%d %s in the market", per[sec.Key], sec.Name)
		}
	}
	changed := false
	for q := p + 1; q < p+5; q++ {
		if Market("g", q)[0].Key != a[0].Key || Market("g", q)[3].Key != a[3].Key {
			changed = true
		}
	}
	if !changed {
		t.Fatal("market never rotates")
	}
	if !InStock("gladius", "g", p) || InStock("townhouse", "g", p) {
		t.Fatal("fixed shelf always, starting items never")
	}
}

func TestSections(t *testing.T) {
	for _, k := range Kinds {
		if _, ok := SectionByKey(k.Section); !ok {
			t.Errorf("kind %s has no section", k.Kind)
		}
	}
	fixed, market := map[Section]int{}, map[Section]int{}
	for _, it := range Items {
		switch it.Shelf {
		case ShelfFixed:
			fixed[it.Section()]++
		case ShelfMarket:
			market[it.Section()]++
		}
		if it.Section() == SectionArmory && it.Effects.Stats[Prowess] <= 0 && it.Effects.Stats[Martial] <= 0 {
			t.Errorf("%s is in the armory but doesn't help you fight", it.Key)
		}
		if it.Section() != SectionArmory && it.Effects.Stats[Prowess] > 0 && !it.IsProperty() {
			t.Errorf("%s raises prowess, so it belongs in the armory", it.Key)
		}
	}
	for _, s := range Sections {
		if fixed[s.Key] == 0 || market[s.Key] < 2*MarketPerSection {
			t.Errorf("%s: %d always in stock, %d on the market", s.Name, fixed[s.Key], market[s.Key])
		}
	}
}
