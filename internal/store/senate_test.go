package store

import (
	"context"
	"errors"
	"math/rand/v2"
	"path/filepath"
	"testing"
	"time"

	"github.com/shantz14/idle-kings/internal/game"
)

func TestSenateLifecycle(t *testing.T) {
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
	sen := game.GenerateSenate(rand.New(rand.NewPCG(1, 2)), game.SenateSize)
	if err := s.CreateRealm(ctx, "g", game.StartingRealm(), sen, now); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateRealm(ctx, "g", game.StartingRealm(), sen, now); err != nil { // idempotent
		t.Fatal(err)
	}
	got, _ := s.Senators(ctx, "g")
	if len(got) != game.SenateSize {
		t.Fatalf("senators: %d", len(got))
	}
	courts, _ := s.RunningCourts(ctx)
	if len(courts) != 1 {
		t.Fatalf("running courts: %v", courts)
	}

	m, err := s.CreateMatter(ctx, "g", "grand_games", 0, now, now.Add(time.Hour), now.Add(2*time.Hour), []string{"grand_games"})
	if err != nil {
		t.Fatal(err)
	}
	if open, err := s.OpenMatter(ctx, "g"); err != nil || open.ID != m.ID {
		t.Fatalf("open matter: %v %+v", err, open)
	}
	if err := s.CastVote(ctx, m.ID, c.ID, "lavish", now); err != nil {
		t.Fatal(err)
	}
	if err := s.CastVote(ctx, m.ID, c.ID, "none", now); err != nil { // change vote
		t.Fatal(err)
	}
	if v, _ := s.Votes(ctx, m.ID); v[c.ID] != "none" {
		t.Fatalf("votes: %v", v)
	}
	if err := s.PayBribe(ctx, m.ID, got[0].ID, c.ID, "none", 30, now); err != nil {
		t.Fatal(err)
	}
	if err := s.PayBribe(ctx, m.ID, got[0].ID, c.ID, "lavish", 30, now); !errors.Is(err, ErrBought) {
		t.Fatalf("double bribe: %v", err)
	}
	if err := s.PayBribe(ctx, m.ID, got[1].ID, c.ID, "none", 100000, now); err == nil {
		t.Fatal("bribe beyond means should fail")
	}
	if b, _ := s.Bribes(ctx, m.ID); len(b) != 1 {
		t.Fatalf("failed bribe should roll back: %v", b)
	}
	if err := s.CastVote(ctx, m.ID, c.ID, "none", now.Add(2*time.Hour)); !errors.Is(err, ErrEnded) {
		t.Fatalf("vote after close: %v", err)
	}

	closed, _ := s.ClosedMatters(ctx, now.Add(time.Hour))
	if len(closed) != 1 {
		t.Fatalf("closed: %v", closed)
	}
	realm := game.StartingRealm()
	realm[game.AttrTreasury] = 0
	err = s.ResolveMatter(ctx, m, MatterResolution{Realm: realm, Fallen: true, FallenReason: "bankrupt",
		Deltas: map[int64]game.Delta{c.ID: {Influence: 1}}, FollowUps: []game.QueuedEvent{{Event: "bread_riots", Days: 1}},
		DayLength: 24 * time.Hour}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ResolveMatter(ctx, m, MatterResolution{}, now); !errors.Is(err, ErrEnded) {
		t.Fatalf("double resolve: %v", err)
	}
	st, _ := s.GetRealm(ctx, "g")
	if st.Alive() || st.Recent[0] != "grand_games" {
		t.Fatalf("realm: %+v", st)
	}
	if courts, _ := s.RunningCourts(ctx); len(courts) != 0 {
		t.Fatal("fallen court should stop running")
	}
	if _, key, err := s.NextQueued(ctx, "g", now.Add(24*time.Hour)); err != nil || key != "bread_riots" {
		t.Fatalf("queue: %v %s", err, key)
	}
	ch, _ := s.GetCharacterByID(ctx, c.ID)
	if ch.Wealth != c.Wealth-30 || ch.Influence != c.Influence+1 {
		t.Fatalf("character: %+v", ch)
	}
}

func TestVoteSelling(t *testing.T) {
	ctx := context.Background()
	s, _ := Open(filepath.Join(t.TempDir(), "t.db"))
	defer s.Close()
	_ = s.ClaimAdmin(ctx, "g", "a")
	seller := newChar(t, s, "a", "scribe")
	buyer := newChar(t, s, "b", "merchant")
	third := newChar(t, s, "c", "cupbearer")
	now := time.Now()
	m, _ := s.CreateMatter(ctx, "g", "grand_games", 0, now, now.Add(time.Hour), now.Add(2*time.Hour), nil)

	// Player sells to player; buyer picks the option on accepting.
	o, err := s.CreateOffer(ctx, VoteOffer{MatterID: m.ID, Kind: OfferSell, FromID: seller.ID, ToID: buyer.ID, Price: 25}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AcceptOffer(ctx, o.ID, "lavish", now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AcceptOffer(ctx, o.ID, "none", now); !errors.Is(err, ErrHandled) {
		t.Fatalf("double accept: %v", err)
	}
	if opt, to, sold := s.SoldVote(ctx, m.ID, seller.ID); !sold || opt != "lavish" || to == "" {
		t.Fatalf("sold vote: %s %s %v", opt, to, sold)
	}
	if err := s.CastVote(ctx, m.ID, seller.ID, "none", now); !errors.Is(err, ErrVoteSold) {
		t.Fatalf("sold vote changed: %v", err)
	}
	// A second buyer can't buy an already sold vote; nothing changes hands.
	o2, _ := s.CreateOffer(ctx, VoteOffer{MatterID: m.ID, Kind: OfferBuy, FromID: third.ID, ToID: seller.ID, Option: "none", Price: 10}, now)
	if _, err := s.AcceptOffer(ctx, o2.ID, "", now); !errors.Is(err, ErrVoteSold) {
		t.Fatalf("resold vote: %v", err)
	}
	if c, _ := s.GetCharacterByID(ctx, third.ID); c.Wealth != third.Wealth {
		t.Fatalf("failed deal should roll back: %d", c.Wealth)
	}
	sel, _ := s.GetCharacterByID(ctx, seller.ID)
	buy, _ := s.GetCharacterByID(ctx, buyer.ID)
	if sel.Wealth != seller.Wealth+25 || buy.Wealth != buyer.Wealth-25 {
		t.Fatalf("gold: seller %d buyer %d", sel.Wealth, buy.Wealth)
	}

	// Selling to a senator pays and locks.
	if err := s.SellVoteToSenator(ctx, m.ID, third.ID, "modest", "Varro Kell", 12, now); err != nil {
		t.Fatal(err)
	}
	if err := s.SellVoteToSenator(ctx, m.ID, third.ID, "none", "Varro Kell", 12, now); !errors.Is(err, ErrVoteSold) {
		t.Fatalf("sold twice: %v", err)
	}
	if v, _ := s.Votes(ctx, m.ID); v[third.ID] != "modest" || v[seller.ID] != "lavish" {
		t.Fatalf("votes: %v", v)
	}
	// Declining.
	o3, _ := s.CreateOffer(ctx, VoteOffer{MatterID: m.ID, Kind: OfferBuy, FromID: buyer.ID, ToID: third.ID, Option: "none", Price: 5}, now)
	if _, err := s.DeclineOffer(ctx, o3.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AcceptOffer(ctx, o3.ID, "", now); !errors.Is(err, ErrHandled) {
		t.Fatalf("accept after decline: %v", err)
	}
}

func TestRecentDecisions(t *testing.T) {
	ctx := context.Background()
	s, _ := Open(filepath.Join(t.TempDir(), "t.db"))
	defer s.Close()
	_ = s.ClaimAdmin(ctx, "g", "a")
	now := time.Now()
	_ = s.CreateRealm(ctx, "g", game.StartingRealm(), nil, now)
	m, _ := s.CreateMatter(ctx, "g", "grand_games", 0, now, now, now, nil)
	err := s.ResolveMatter(ctx, m, MatterResolution{Summary: map[string]any{"winner": "lavish", "succeeded": true},
		Realm: game.StartingRealm()}, now)
	if err != nil {
		t.Fatal(err)
	}
	ds, err := s.RecentDecisions(ctx, "g", 5)
	if err != nil || len(ds) != 1 || ds[0].Winner != "lavish" || !ds[0].Succeeded {
		t.Fatalf("decisions: %v %+v", err, ds)
	}
}
