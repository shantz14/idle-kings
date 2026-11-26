package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/shantz14/idle-kings/internal/game"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	_ = s.ClaimAdmin(context.Background(), "g", "a")
	_ = s.StartCourt(context.Background(), "g", "chan")
	return s
}

func wealth(t *testing.T, s *Store, id int64) int {
	t.Helper()
	c, err := s.GetCharacterByID(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return c.Wealth
}

func TestDuelLifecycle(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	a, d, x := newChar(t, s, "a", "officer"), newChar(t, s, "d", "officer"), newChar(t, s, "x", "merchant")
	now := time.Now()
	start := func(wager int) Activity {
		t.Helper()
		act, err := s.StartActivity(ctx, Activity{GuildID: "g", HostID: a.ID, Kind: game.KindDuel, Goal: game.DuelPending,
			TargetID: d.ID, Public: true, StartedAt: now, EndsAt: now.Add(time.Hour)}, wager, nil)
		if err != nil {
			t.Fatal(err)
		}
		return act
	}

	// Declining refunds the challenger and costs the defender.
	duel := start(40)
	if wealth(t, s, a.ID) != a.Wealth-40 {
		t.Fatal("wager not held")
	}
	if p, err := s.PendingDuelAgainst(ctx, d.ID); err != nil || p.ID != duel.ID {
		t.Fatalf("pending duel: %v", err)
	}
	if _, err := s.DeclineDuel(ctx, duel.ID, x.ID, Outcome{}, now); !errors.Is(err, ErrNotYourDuel) {
		t.Fatalf("a stranger declined: %v", err)
	}
	if _, err := s.DeclineDuel(ctx, duel.ID, d.ID, Outcome{Deltas: map[int64]game.Delta{d.ID: {Renown: -0.3}}}, now); err != nil {
		t.Fatal(err)
	}
	if wealth(t, s, a.ID) != a.Wealth {
		t.Fatal("wager not refunded")
	}
	if _, err := s.AcceptDuel(ctx, duel.ID, d.ID, now.Add(time.Hour), nil, now); !errors.Is(err, ErrEnded) {
		t.Fatalf("accepted a declined duel: %v", err)
	}

	// The defender must match the wager.
	duel = start(40)
	if _, err := s.db.ExecContext(ctx, `UPDATE characters SET wealth = 10 WHERE id = ?`, d.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AcceptDuel(ctx, duel.ID, d.ID, now.Add(time.Hour), nil, now); err == nil {
		t.Fatal("accepted without the gold")
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE characters SET wealth = ? WHERE id = ?`, d.Wealth, d.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeclineDuel(ctx, duel.ID, 0, Outcome{}, now); err != nil { // timed out
		t.Fatal(err)
	}

	// Accepting takes the defender's wager and schedules both duelists' events.
	duel = start(30)
	events := []NewEvent{
		{CharacterID: a.ID, Key: game.DuelStanceEvent, FiresAt: now, Deadline: now.Add(time.Minute)},
		{CharacterID: d.ID, Key: game.DuelStanceEvent, FiresAt: now, Deadline: now.Add(time.Minute)},
	}
	acc, err := s.AcceptDuel(ctx, duel.ID, d.ID, now.Add(time.Hour), events, now)
	if err != nil || acc.Goal != game.DuelAccepted {
		t.Fatalf("accept: %v %+v", err, acc)
	}
	if wealth(t, s, d.ID) != d.Wealth-30 {
		t.Fatal("defender's wager not taken")
	}
	due, _ := s.DueEvents(ctx, now)
	if len(due) != 2 || due[0].CharacterID == due[1].CharacterID {
		t.Fatalf("stance events: %+v", due)
	}
	if err := s.ResolveEvent(ctx, due[1].ID, game.StanceTrick, game.EventOutcome{}, now); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetEvent(ctx, due[1].ID); got.Choice != game.StanceTrick {
		t.Fatalf("choice not stored: %q", got.Choice)
	}

	// Bets: spectators only, one each, paid up front.
	if err := s.PlaceBet(ctx, duel.ID, a.ID, a.ID, 5, now); !errors.Is(err, ErrNoBetting) {
		t.Fatalf("a duelist bet: %v", err)
	}
	if err := s.PlaceBet(ctx, duel.ID, x.ID, d.ID, 25, now); err != nil {
		t.Fatal(err)
	}
	if err := s.PlaceBet(ctx, duel.ID, x.ID, a.ID, 5, now); !errors.Is(err, ErrExists) {
		t.Fatalf("second bet: %v", err)
	}
	if err := s.PlaceBet(ctx, duel.ID, newChar(t, s, "y", "scribe").ID, d.ID, 1_000_000, now); err == nil {
		t.Fatal("bet more gold than they have")
	}
	if wealth(t, s, x.ID) != x.Wealth-25 {
		t.Fatal("bet not paid")
	}
	if bets, _ := s.DuelBets(ctx, duel.ID); len(bets) != 1 || bets[0].SideID != d.ID {
		t.Fatalf("bets: %+v", bets)
	}

	// The bout resolves through ApplyResult, wounds included.
	err = s.ApplyResult(ctx, duel.ID, Outcome{
		Deltas:     map[int64]game.Delta{d.ID: {Wealth: 60}},
		Conditions: []NewCondition{{CharacterID: a.ID, Key: game.CondWounded, EndsAt: now.Add(time.Hour)}},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PlaceBet(ctx, duel.ID, x.ID, d.ID, 1, now); !errors.Is(err, ErrEnded) {
		t.Fatalf("bet after the bout: %v", err)
	}
	loser, _ := s.GetCharacterByID(ctx, a.ID)
	if len(loser.Conditions) != 1 || loser.Effective()[game.Prowess] >= a.Effective()[game.Prowess] {
		t.Fatalf("wound: %+v %v", loser.Conditions, loser.Effective())
	}
}

func TestConditionsExpireAndExtend(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	c := newChar(t, s, "a", "officer")
	now := time.Now()
	apply := func(ends time.Time) {
		t.Helper()
		act, err := s.StartActivity(ctx, Activity{GuildID: "g", HostID: c.ID, Kind: game.KindHunt, StartedAt: now, EndsAt: now}, 0, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.ApplyResult(ctx, act.ID, Outcome{Conditions: []NewCondition{{CharacterID: c.ID, Key: game.CondWounded, EndsAt: ends}}}, now); err != nil {
			t.Fatal(err)
		}
	}
	apply(now.Add(-time.Second)) // already healed
	if got, _ := s.GetCharacterByID(ctx, c.ID); len(got.Conditions) != 0 {
		t.Fatalf("expired condition still active: %+v", got.Conditions)
	}
	apply(now.Add(2 * time.Hour))
	apply(now.Add(time.Hour)) // a shorter wound doesn't shorten the longer one
	got, _ := s.GetCharacterByID(ctx, c.ID)
	if len(got.Conditions) != 1 || got.Conditions[0].EndsAt.Unix() != now.Add(2*time.Hour).Unix() {
		t.Fatalf("conditions: %+v", got.Conditions)
	}
}
