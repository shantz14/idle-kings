package store

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/shantz14/idle-kings/internal/game"
)

func TestSabotageDamagesAndRepairs(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	now := time.Now()
	if err := s.CreateRealm(ctx, "g", game.StartingRealm(), nil, now); err != nil {
		t.Fatal(err)
	}
	a := newChar(t, s, "a", "merchant") // a merchant house's townhouse earns income
	b := newChar(t, s, "b", "merchant")
	for _, c := range []Character{a, b} {
		act, err := s.StartActivity(ctx, Activity{GuildID: "g", HostID: c.ID, Kind: game.KindInvest, Goal: string(game.SectorTrade), StartedAt: now, EndsAt: now}, 50, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.ApplyInvestment(ctx, Investment{ActivityID: act.ID, CharacterID: c.ID, GuildID: "g", Sector: game.SectorTrade, Stake: 50, Gold: 50}, now); err != nil {
			t.Fatal(err)
		}
	}
	a, b = reload(t, s, a), reload(t, s, b)
	home, _ := a.Residence()
	homeItem, _ := home.Item()
	before := a.Effects()
	if before.DailyIncome == 0 {
		t.Fatalf("test needs a home with income: %+v", homeItem)
	}

	out := Outcome{Sabotage: []Sabotage{
		{GuildID: "g", CharacterID: a.ID, Sector: game.SectorTrade, Factor: 0.5, Until: now.Add(time.Hour)},
		{GuildID: "g", CharacterID: a.ID, HoldingID: home.ID, RepairCost: 12},
	}}
	if err := s.ApplyOutcome(ctx, "g", out, now); err != nil {
		t.Fatal(err)
	}
	hit, b2 := reload(t, s, a), reload(t, s, b)
	if math.Abs(hit.Ventures[game.SectorTrade].Income-a.Ventures[game.SectorTrade].Income*0.5) > 1e-9 || b2.Ventures[game.SectorTrade].Income != b.Ventures[game.SectorTrade].Income {
		t.Fatalf("only the victim's slice should earn less: %v vs %v; bystander %v vs %v",
			hit.Ventures[game.SectorTrade].Income, a.Ventures[game.SectorTrade].Income, b2.Ventures[game.SectorTrade].Income, b.Ventures[game.SectorTrade].Income)
	}
	if hit.Effects().DailyIncome >= before.DailyIncome || hit.EstateValue() != a.EstateValue() {
		t.Fatalf("a damaged home loses its effects, not its value: %+v, value %v", hit.Effects(), hit.EstateValue())
	}
	if h, _ := hit.Residence(); h.Repair != 12 {
		t.Fatalf("repair cost %d", h.Repair)
	}

	if _, err := s.RepairItem(ctx, a.ID, home.ID); err != nil {
		t.Fatal(err)
	}
	fixed := reload(t, s, a)
	if fixed.Effects().DailyIncome != before.DailyIncome || fixed.Wealth != hit.Wealth-12 {
		t.Fatalf("after repair: %+v, wealth %d", fixed.Effects(), fixed.Wealth)
	}
	if _, err := s.RepairItem(ctx, a.ID, home.ID); err == nil {
		t.Fatal("nothing left to repair")
	}
}
