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

// A vote applies its own change on top of the realm as it is when it's
// stored, so changes made while it was being worked out survive, and a fallen
// realm stays fallen.
func TestResolveMatterMergesRealmChanges(t *testing.T) {
	s, ctx, now := lawCourt(t)
	st, _ := s.GetRealm(ctx, "g")
	before := st.Trackers.Clone()
	decided := before.Clone()
	decided.Apply(game.Realm{game.AttrTreasury: 5})

	// Meanwhile, something else lifts stability.
	if err := s.ApplyOutcome(ctx, "g", Outcome{Realm: game.Realm{game.AttrStability: 3}}, now); err != nil {
		t.Fatal(err)
	}
	m, err := s.CreateMatter(ctx, "g", "grand_games", 0, now, now, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ResolveMatter(ctx, m, MatterResolution{Realm: decided, Before: before, Laws: st.Laws}, now); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetRealm(ctx, "g")
	if got.Trackers[game.AttrTreasury] != before[game.AttrTreasury]+5 || got.Trackers[game.AttrStability] != before[game.AttrStability]+3 {
		t.Fatalf("trackers %v, want treasury +5 and stability +3 over %v", got.Trackers, before)
	}

	// A realm that fell in between isn't revived by the vote.
	if err := s.ApplyOutcome(ctx, "g", Outcome{Realm: game.Realm{game.AttrTreasury: -1000}}, now); err != nil {
		t.Fatal(err)
	}
	m, _ = s.CreateMatter(ctx, "g", "grand_games", 0, now, now, now)
	if err := s.ResolveMatter(ctx, m, MatterResolution{Realm: decided, Before: before, Laws: st.Laws}, now); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetRealm(ctx, "g"); got.Alive() {
		t.Fatal("a vote revived a fallen realm")
	}
}
