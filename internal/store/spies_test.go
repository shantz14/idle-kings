package store

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/shantz14/idle-kings/internal/game"
)

func TestSpyNetworks(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	a, b, c := newChar(t, s, "a", "merchant"), newChar(t, s, "b", "scribe"), newChar(t, s, "c", "officer")

	if err := s.ChangeSpies(ctx, []game.SpyChange{
		{Owner: a.ID, Target: b.ID, Delta: 70},
		{Owner: a.ID, Target: b.ID, Delta: 70}, // clamps at 100
		{Owner: c.ID, Target: b.ID, Delta: 10},
		{Owner: c.ID, Target: b.ID, Delta: -30}, // clamps at 0
		{Owner: a.ID, Target: c.ID, Delta: 20},
		{Owner: a.ID, Target: a.ID, Delta: 20}, // no spying on yourself
	}); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.SpyLevel(ctx, a.ID, b.ID); v != 100 {
		t.Errorf("a on b = %v, want 100", v)
	}
	if v, _ := s.SpyLevel(ctx, c.ID, b.ID); v != 0 {
		t.Errorf("c on b = %v, want 0", v)
	}
	on, _ := s.SpyNetworksOn(ctx, b.ID)
	if len(on) != 1 || on[0].OwnerID != a.ID { // zero-value networks are hidden
		t.Errorf("networks on b = %+v", on)
	}
	by, _ := s.SpyNetworksBy(ctx, a.ID)
	if len(by) != 2 || by[0].TargetID != b.ID {
		t.Errorf("networks by a = %+v", by)
	}

	// Through an activity's outcome.
	now := time.Now()
	act, err := s.StartActivity(ctx, Activity{GuildID: "g", HostID: a.ID, Kind: game.KindPlantSpies, TargetID: c.ID, StartedAt: now, EndsAt: now}, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyResult(ctx, act.ID, Outcome{
		Deltas: map[int64]game.Delta{a.ID: game.Practice(game.Intrigue, true)},
		Spies:  []game.SpyChange{{Owner: a.ID, Target: c.ID, Delta: 10}},
	}, now); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.SpyLevel(ctx, a.ID, c.ID); v != 30 {
		t.Errorf("a on c = %v, want 30", v)
	}
	after, _ := s.GetCharacterByID(ctx, a.ID)
	if math.Abs(after.Stats[game.Intrigue]-a.Stats[game.Intrigue]-0.15) > 1e-9 {
		t.Errorf("practice not applied: %v → %v", a.Stats[game.Intrigue], after.Stats[game.Intrigue])
	}

	// Decay fades networks, and tiny ones vanish.
	_ = s.ChangeSpies(ctx, []game.SpyChange{{Owner: b.ID, Target: c.ID, Delta: 0.5}})
	tx, _ := s.db.BeginTx(ctx, nil)
	if err := decaySpies(ctx, tx, "g", 0.5); err != nil {
		t.Fatal(err)
	}
	_ = tx.Commit()
	if v, _ := s.SpyLevel(ctx, a.ID, c.ID); v != 15 {
		t.Errorf("decayed = %v, want 15", v)
	}
	if v, _ := s.SpyLevel(ctx, b.ID, c.ID); v != 0 {
		t.Errorf("tiny network = %v, want 0", v)
	}
}
