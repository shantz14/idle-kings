package store

import (
	"context"
	"math"
	"math/rand/v2"
	"path/filepath"
	"testing"
	"time"

	"github.com/shantz14/idle-kings/internal/game"
)

func TestOpinions(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_ = s.ClaimAdmin(ctx, "g", "a")
	_ = s.StartCourt(ctx, "g", "chan")
	c := newChar(t, s, "a", "merchant")
	now := time.Now()
	if err := s.CreateRealm(ctx, "g", game.StartingRealm(), game.GenerateSenate(rand.New(rand.NewPCG(1, 2)), game.SenateSize), now); err != nil {
		t.Fatal(err)
	}
	sen, _ := s.Senators(ctx, "g")
	a, b := sen[0].ID, sen[1].ID

	if err := s.ChangeOpinions(ctx, []game.OpinionChange{
		{SenatorID: a, CharacterID: c.ID, Delta: 70},
		{SenatorID: a, CharacterID: c.ID, Delta: 70}, // clamps at 100
		{SenatorID: b, CharacterID: c.ID, Delta: -30},
		{CharacterID: c.ID, Delta: 5}, // every senator
	}); err != nil {
		t.Fatal(err)
	}
	ops, err := s.Opinions(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ops[a] != 100 || ops[b] != -25 || ops[sen[2].ID] != 5 || len(ops) != game.SenateSize {
		t.Fatalf("opinions: %v", ops)
	}

	// A bribe warms the senator.
	m, _ := s.CreateMatter(ctx, "g", "grand_games", 0, now, now.Add(time.Hour), now.Add(2*time.Hour))
	if err := s.PayBribe(ctx, m.ID, b, c.ID, "none", 1, 3, now); err != nil {
		t.Fatal(err)
	}
	// Resolving decays every opinion, then applies vote changes.
	if err := s.ResolveMatter(ctx, m, MatterResolution{Realm: game.StartingRealm(), OpinionDecay: 0.5,
		Opinions: []game.OpinionChange{{SenatorID: a, CharacterID: c.ID, Delta: 2}}}, now); err != nil {
		t.Fatal(err)
	}
	all, err := s.CourtOpinions(ctx, "g")
	if err != nil {
		t.Fatal(err)
	}
	if all.Of(a, c.ID) != 52 || math.Abs(all.Of(b, c.ID)-(-11)) > 1e-9 || all.Of(sen[2].ID, c.ID) != 2.5 {
		t.Fatalf("after decay: %v", all)
	}

	// Courting is an activity with a target; it resolves with an opinion change.
	act, err := s.StartActivity(ctx, Activity{GuildID: "g", HostID: c.ID, Kind: game.KindCourt, Goal: "flatter", TargetID: a,
		StartedAt: now, EndsAt: now}, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetActivity(ctx, act.ID); got.TargetID != a || got.Kind != game.KindCourt {
		t.Fatalf("activity: %+v", got)
	}
	if err := s.ApplyResult(ctx, act.ID, Outcome{Opinions: []game.OpinionChange{{SenatorID: a, CharacterID: c.ID, Delta: -12}}}, now); err != nil {
		t.Fatal(err)
	}
	if ops, _ := s.Opinions(ctx, c.ID); ops[a] != 40 {
		t.Fatalf("after courting: %v", ops[a])
	}
}
