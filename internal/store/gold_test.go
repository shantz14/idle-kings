package store

import (
	"context"
	"testing"
	"time"

	"github.com/shantz14/idle-kings/internal/game"
)

// Gold moves pay only what the payer has, so the total never grows.
func TestGoldMovesNeverMintGold(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	a, b, c := newChar(t, s, "a", "scribe"), newChar(t, s, "b", "scribe"), newChar(t, s, "c", "scribe")
	// Leave a with 30 gold.
	if err := s.ApplyOutcome(ctx, "g", Outcome{Deltas: map[int64]game.Delta{a.ID: {Wealth: 30 - a.Wealth}}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	total := func() int {
		n := 0
		for _, id := range []int64{a.ID, b.ID, c.ID} {
			ch, err := s.GetCharacterByID(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			n += ch.Wealth
		}
		return n
	}
	before := total()
	out := Outcome{GoldMoves: []GoldMove{{From: a.ID, To: b.ID, Gold: 25}, {From: a.ID, To: c.ID, Gold: 25}}}
	if err := s.ApplyOutcome(ctx, "g", out, time.Now()); err != nil {
		t.Fatal(err)
	}
	if after := total(); after != before {
		t.Fatalf("total gold %d → %d", before, after)
	}
	ga, _ := s.GetCharacterByID(ctx, a.ID)
	gc, _ := s.GetCharacterByID(ctx, c.ID)
	if ga.Wealth != 0 || gc.Wealth != c.Wealth+5 {
		t.Fatalf("a has %d (want 0), c got %d (want 5)", ga.Wealth, gc.Wealth-c.Wealth)
	}
}

// A death mid-duel gives back everything the duel held: both wagers and the
// bets, the dead's own share going to the heir.
func TestDeathRefundsHeldGold(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	a, d, x := newChar(t, s, "a", "officer"), newChar(t, s, "d", "officer"), newChar(t, s, "x", "merchant")
	now := time.Now()
	duel, err := s.StartActivity(ctx, Activity{GuildID: "g", HostID: a.ID, Kind: game.KindDuel, Goal: game.DuelPending,
		TargetID: d.ID, Public: true, StartedAt: now, EndsAt: now.Add(time.Hour)}, 40, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AcceptDuel(ctx, duel.ID, d.ID, now.Add(time.Hour), nil, now); err != nil {
		t.Fatal(err)
	}
	if err := s.PlaceBet(ctx, duel.ID, x.ID, d.ID, 15, now); err != nil {
		t.Fatal(err)
	}
	heir := game.Inherit("Heir", a.Stats, a.Renown, a.Influence, wealth(t, s, a.ID))
	if err := s.ApplyOutcome(ctx, "g", Outcome{Deaths: []Death{{CharacterID: a.ID, Heir: heir}}}, now); err != nil {
		t.Fatal(err)
	}
	if got := wealth(t, s, a.ID); got != heir.Wealth+40 {
		t.Errorf("heir has %d, want %d (inheritance plus the wager)", got, heir.Wealth+40)
	}
	if got := wealth(t, s, d.ID); got != d.Wealth {
		t.Errorf("defender has %d, want their %d back", got, d.Wealth)
	}
	if got := wealth(t, s, x.ID); got != x.Wealth {
		t.Errorf("bettor has %d, want their %d back", got, x.Wealth)
	}
}
