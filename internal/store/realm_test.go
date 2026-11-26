package store

import (
	"context"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/shantz14/idle-kings/internal/game"
)

func TestActivitiesCanEndTheRealm(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	c := newChar(t, s, "a", "merchant")
	now := time.Now()
	realm := game.StartingRealm()
	realm[game.AttrTreasury] = 5
	if err := s.CreateRealm(ctx, "g", realm, game.GenerateSenate(rand.New(rand.NewPCG(1, 2)), game.SenateSize), now); err != nil {
		t.Fatal(err)
	}

	// A gift lifts the treasury and costs gold.
	before, _ := s.GetCharacterByID(ctx, c.ID)
	if err := s.ApplyAid(ctx, c.ID, "g", 10, game.Realm{game.AttrTreasury: 1}, game.Delta{Renown: 0.1}, nil); err != nil {
		t.Fatal(err)
	}
	after, _ := s.GetCharacterByID(ctx, c.ID)
	if after.Wealth != before.Wealth-10 {
		t.Errorf("wealth %d → %d", before.Wealth, after.Wealth)
	}
	if err := s.ApplyAid(ctx, c.ID, "g", 1_000_000, game.Realm{game.AttrTreasury: 1}, game.Delta{}, nil); err == nil {
		t.Error("gave gold they don't have")
	}

	// Stealing the rest ends it.
	act, err := s.StartActivity(ctx, Activity{GuildID: "g", HostID: c.ID, Kind: game.KindEmbezzle, StartedAt: now, EndsAt: now}, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyResult(ctx, act.ID, Outcome{Realm: game.Realm{game.AttrTreasury: -6}}, now); err != nil {
		t.Fatal(err)
	}
	st, _ := s.GetRealm(ctx, "g")
	if st.Alive() || st.FallenReason == "" {
		t.Fatalf("realm should have fallen: %+v", st)
	}
	if err := s.ApplyAid(ctx, c.ID, "g", 10, game.Realm{game.AttrTreasury: 1}, game.Delta{}, nil); err != ErrEnded {
		t.Errorf("aid after the fall = %v, want ErrEnded", err)
	}
}
