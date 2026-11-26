package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/shantz14/idle-kings/internal/game"
)

// newChar creates a character from the Merchant House background (100
// gold) with the given role; the user name doubles as the family name.
func newChar(t *testing.T, s *Store, user, role string) Character {
	t.Helper()
	c, err := s.CreateCharacter(context.Background(), "g", user, game.Build{
		Name: user, Family: user, Crest: game.Crest{Color: "red", Symbol: "lion"},
		Points: game.Stats{game.Diplomacy: 3, game.Stewardship: 3}, Background: "merchant_house", Role: role,
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestPartyLifecycle(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if err := s.ClaimAdmin(ctx, "g", "host"); err != nil {
		t.Fatal(err)
	}
	if err := s.ClaimAdmin(ctx, "g", "other"); !errors.Is(err, ErrExists) {
		t.Fatalf("second claim: %v", err)
	}
	host := newChar(t, s, "host", "merchant")
	guest := newChar(t, s, "guest", "cupbearer")

	now := time.Now()
	a, err := s.StartActivity(ctx, Activity{GuildID: "g", HostID: host.ID, Kind: game.KindParty, Goal: "make_friends",
		Public: true, StartedAt: now, EndsAt: now.Add(time.Hour)}, game.PartyMinSpend,
		[]NewEvent{{Key: "wine", FiresAt: now.Add(time.Minute), Deadline: now.Add(2 * time.Minute)}})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.GetActivity(ctx, a.ID); err != nil || got.Spend != game.PartyMinSpend {
		t.Fatalf("spend not stored: %+v %v", got, err)
	}
	if _, err := s.StartActivity(ctx, Activity{GuildID: "g", HostID: host.ID, Kind: game.KindParty, StartedAt: now, EndsAt: now}, 0, nil); !errors.Is(err, ErrBusy) {
		t.Fatalf("double host: %v", err)
	}

	if err := s.AddParticipant(ctx, a.ID, host.ID, "make_friends", 0, now); !errors.Is(err, ErrOwnEvent) {
		t.Fatalf("own party: %v", err)
	}
	if err := s.AddParticipant(ctx, a.ID, guest.ID, "spread_rumors", host.ID, now); err != nil {
		t.Fatal(err)
	}
	if err := s.AddParticipant(ctx, a.ID, guest.ID, "make_friends", 0, now); !errors.Is(err, ErrExists) {
		t.Fatalf("attend twice: %v", err)
	}
	ps, _ := s.Participants(ctx, a.ID)
	if len(ps) != 1 || ps[0].TargetID != host.ID {
		t.Fatalf("participants: %+v", ps)
	}

	due, _ := s.DueEvents(ctx, now.Add(time.Minute))
	if len(due) != 1 || due[0].UserID != "host" {
		t.Fatalf("due: %+v", due)
	}
	_ = s.MarkEventSent(ctx, due[0].ID, "dm", "msg", now)
	out := game.EventOutcome{Text: "x", Quality: 5, Delta: game.Delta{Wealth: -15}}
	if err := s.ResolveEvent(ctx, due[0].ID, "buy", out, now); err != nil {
		t.Fatal(err)
	}
	if err := s.ResolveEvent(ctx, due[0].ID, "end", out, now); !errors.Is(err, ErrEnded) {
		t.Fatalf("double resolve: %v", err)
	}
	res, _ := s.EventResults(ctx, a.ID)
	if len(res) != 1 || res[0].Result.Quality != 5 {
		t.Fatalf("results: %+v", res)
	}

	ended, _ := s.EndedActivities(ctx, now.Add(time.Hour))
	if len(ended) != 1 {
		t.Fatalf("ended: %+v", ended)
	}
	deltas := map[int64]game.Delta{host.ID: {Influence: 2, Wealth: -15, Stats: game.Stats{game.Diplomacy: 0.3}}}
	if err := s.ApplyResult(ctx, a.ID, deltas, now); err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyResult(ctx, a.ID, deltas, now); !errors.Is(err, ErrEnded) {
		t.Fatalf("double apply: %v", err)
	}
	got, _ := s.GetCharacterByID(ctx, host.ID)
	if got.Wealth != host.Wealth-game.PartyMinSpend-15 || got.Influence != host.Influence+2 || got.Stats[game.Diplomacy] != host.Stats[game.Diplomacy]+0.3 {
		t.Fatalf("host after: %+v", got)
	}
}

func TestPayIncome(t *testing.T) {
	ctx := context.Background()
	s, _ := Open(filepath.Join(t.TempDir(), "t.db"))
	defer s.Close()
	_ = s.ClaimAdmin(ctx, "g", "a")
	c := newChar(t, s, "a", "merchant")
	if err := s.PayIncome(ctx, time.Now().Add(49*time.Hour)); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetCharacterByID(ctx, c.ID)
	if got.Wealth != c.Wealth+2*c.TotalIncome() || c.TotalIncome() != 12+3+3 {
		t.Fatalf("wealth %d", got.Wealth)
	}
	_ = s.PayIncome(ctx, time.Now().Add(50*time.Hour)) // same day: nothing more
	got2, _ := s.GetCharacterByID(ctx, c.ID)
	if got2.Wealth != got.Wealth {
		t.Fatalf("paid twice: %d", got2.Wealth)
	}
	if list, _ := s.CharactersForUser(ctx, "a"); len(list) != 1 {
		t.Fatalf("for user: %v", list)
	}
}
