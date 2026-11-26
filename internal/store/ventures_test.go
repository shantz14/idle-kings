package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/shantz14/idle-kings/internal/game"
)

func TestInvestmentLifecycle(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_ = s.ClaimAdmin(ctx, "g", "a")
	now := time.Now()
	if err := s.CreateRealm(ctx, "g", game.StartingRealm(), nil, now); err != nil {
		t.Fatal(err)
	}
	st, err := s.GetRealm(ctx, "g")
	if err != nil || st.Sectors[game.SectorMining].Capacity != 40 || st.Sectors[game.SectorMining].Health != 1 {
		t.Fatalf("seeded sectors: %+v %v", st.Sectors, err)
	}

	a := newChar(t, s, "a", "merchant")
	b := newChar(t, s, "b", "officer")
	invest := func(c Character, sec game.SectorKey, gold int, stake float64) Activity {
		t.Helper()
		act, err := s.StartActivity(ctx, Activity{GuildID: "g", HostID: c.ID, Kind: game.KindInvest, Goal: string(sec),
			StartedAt: now, EndsAt: now}, gold, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.ApplyInvestment(ctx, Investment{ActivityID: act.ID, CharacterID: c.ID, GuildID: "g", Sector: sec,
			Stake: stake, Capacity: 4}, now); err != nil {
			t.Fatal(err)
		}
		return act
	}
	act := invest(a, game.SectorAgriculture, 50, 300)
	if err := s.ApplyInvestment(ctx, Investment{ActivityID: act.ID, CharacterID: a.ID, GuildID: "g",
		Sector: game.SectorAgriculture, Stake: 300}, now); !errors.Is(err, ErrEnded) {
		t.Fatalf("double resolve: %v", err)
	}

	got, _ := s.GetCharacterByID(ctx, a.ID)
	v := got.Ventures[game.SectorAgriculture]
	if got.Wealth != a.Wealth-50 || v.Stake != 300 || v.Share != 0.2 || v.Income <= 0 {
		t.Fatalf("after investing: wealth %d, %+v", got.Wealth, v)
	}
	if got.TotalIncome() <= a.TotalIncome() {
		t.Fatal("venture income should count toward total income")
	}
	st, _ = s.GetRealm(ctx, "g")
	if st.Sectors[game.SectorAgriculture].Capacity != 54 {
		t.Fatalf("capacity %v", st.Sectors[game.SectorAgriculture].Capacity)
	}

	// A second investor dilutes the first; stakes add up.
	invest(b, game.SectorAgriculture, 100, 300)
	invest(a, game.SectorAgriculture, 25, 100)
	got, _ = s.GetCharacterByID(ctx, a.ID)
	if v := got.Ventures[game.SectorAgriculture]; v.Stake != 400 || v.Share != 400.0/1900 {
		t.Fatalf("diluted: %+v", v)
	}

	// Senate changes to sectors are stored.
	m, _ := s.CreateMatter(ctx, "g", "grand_games", 0, now, now, now)
	st, _ = s.GetRealm(ctx, "g")
	sec := st.Sectors.Clone()
	sec.Apply(map[game.SectorKey]game.SectorChange{game.SectorLuxury: {Health: 0.15}})
	if err := s.ResolveMatter(ctx, m, MatterResolution{Realm: st.Trackers, Sectors: sec}, now); err != nil {
		t.Fatal(err)
	}
	st, _ = s.GetRealm(ctx, "g")
	if st.Sectors[game.SectorLuxury].Health != 1.15 || st.Sectors[game.SectorAgriculture].Capacity != 62 {
		t.Fatalf("after matter: %+v", st.Sectors)
	}
}
