package store

import (
	"context"
	"math/rand/v2"
	"path/filepath"
	"testing"
	"time"

	"github.com/shantz14/idle-kings/internal/game"
)

func TestVisitsAndVoteAsks(t *testing.T) {
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
	sid := sen[0].ID

	for want := 0; want < 3; want++ {
		if got, err := s.RecordVisit(ctx, sid, c.ID, now); err != nil || got != want {
			t.Fatalf("visit %d: got %d, %v", want, got, err)
		}
	}
	if n, _ := s.Visits(ctx, sid, c.ID); n != 3 {
		t.Fatalf("visits: %d", n)
	}
	if n, _ := s.Visits(ctx, sen[1].ID, c.ID); n != 0 {
		t.Fatalf("never visited: %d", n)
	}

	m, err := s.CreateMatter(ctx, "g", "grand_games", 0, now, now.Add(time.Hour), now.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	first := VoteAsk{MatterID: m.ID, SenatorID: sid, Reply: "lie", Option: "lavish", Text: "one"}
	if got, fresh, err := s.SaveVoteAsk(ctx, c.ID, first, now); err != nil || !fresh || got != first {
		t.Fatalf("first ask: %+v %v %v", got, fresh, err)
	}
	// Asking again can't change the answer.
	if got, fresh, err := s.SaveVoteAsk(ctx, c.ID, VoteAsk{MatterID: m.ID, SenatorID: sid, Reply: "truth", Text: "two"}, now); err != nil || fresh || got != first {
		t.Fatalf("second ask: %+v %v %v", got, fresh, err)
	}
	asks, err := s.VoteAsks(ctx, sid, c.ID, []int64{m.ID, 12345})
	if err != nil || len(asks) != 1 || asks[m.ID] != first {
		t.Fatalf("asks: %+v %v", asks, err)
	}
}
