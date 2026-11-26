package store

import (
	"context"
	"errors"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/shantz14/idle-kings/internal/game"
)

func TestWorksAndRealmOutcomes(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	c := newChar(t, s, "a", "merchant")
	now := time.Now()
	if err := s.CreateRealm(ctx, "g", game.StartingRealm(), game.GenerateSenate(rand.New(rand.NewPCG(1, 2)), game.SenateSize), now); err != nil {
		t.Fatal(err)
	}

	work, err := s.StartActivity(ctx, Activity{GuildID: "g", HostID: c.ID, Kind: game.KindWork, Goal: "statue",
		StartedAt: now, EndsAt: now.Add(time.Hour)}, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Building doesn't take up the action, but you can only build one at a time.
	if _, err := s.HostedActivity(ctx, c.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a work counted as the action: %v", err)
	}
	if got, err := s.WorkInProgress(ctx, c.ID); err != nil || got.ID != work.ID {
		t.Fatalf("work in progress: %v", err)
	}
	act, err := s.StartActivity(ctx, Activity{GuildID: "g", HostID: c.ID, Kind: game.KindCanvass, StartedAt: now, EndsAt: now}, 0, nil)
	if err != nil {
		t.Fatalf("a work blocked canvassing: %v", err)
	}
	if _, err := s.StartActivity(ctx, Activity{GuildID: "g", HostID: c.ID, Kind: game.KindWork, Goal: "road",
		StartedAt: now, EndsAt: now}, 0, nil); !errors.Is(err, ErrBuilding) {
		t.Fatalf("second work: %v", err)
	}
	if _, err := s.StartActivity(ctx, Activity{GuildID: "g", HostID: c.ID, Kind: game.KindSpeech,
		StartedAt: now, EndsAt: now}, 0, nil); !errors.Is(err, ErrBusy) {
		t.Fatalf("two actions at once: %v", err)
	}

	// Realm changes from an activity are clamped.
	if err := s.ApplyResult(ctx, act.ID, Outcome{Realm: game.Realm{game.AttrPopularity: 500}}, now); err != nil {
		t.Fatal(err)
	}
	if st, _ := s.GetRealm(ctx, "g"); st.Trackers[game.AttrPopularity] != game.TrackerMax {
		t.Fatalf("popularity: %v", st.Trackers[game.AttrPopularity])
	}

	// Finishing the work adds a monument with lasting vote weight.
	before := c.VoteWeight()
	if err := s.ApplyResult(ctx, work.ID, Outcome{Monuments: []NewMonument{{CharacterID: c.ID, Key: "statue"}}}, now); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetCharacterByID(ctx, c.ID)
	if len(got.Monuments) != 1 || got.VoteWeight() <= before || got.EstateValue() <= c.EstateValue() {
		t.Fatalf("monument: %+v vote %v estate %v", got.Monuments, got.VoteWeight(), got.EstateValue())
	}
	if _, err := s.StartActivity(ctx, Activity{GuildID: "g", HostID: c.ID, Kind: game.KindWork, Goal: "statue",
		StartedAt: now, EndsAt: now}, 0, nil); !errors.Is(err, ErrBuilt) {
		t.Fatalf("built twice: %v", err)
	}
}
