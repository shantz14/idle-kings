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

func lawCourt(t *testing.T) (*Store, context.Context, time.Time) {
	t.Helper()
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	_ = s.ClaimAdmin(ctx, "g", "a")
	_ = s.StartCourt(ctx, "g", "chan")
	now := time.Now()
	sen := game.GenerateSenate(rand.New(rand.NewPCG(1, 2)), game.SenateSize)
	if err := s.CreateRealm(ctx, "g", game.StartingRealm(), sen, now); err != nil {
		t.Fatal(err)
	}
	return s, ctx, now
}

func passLaws(t *testing.T, s *Store, ctx context.Context, now time.Time, ls game.Laws) {
	t.Helper()
	m, err := s.CreateMatter(ctx, "g", "grand_games", 0, now, now, now)
	if err != nil {
		t.Fatal(err)
	}
	st, _ := s.GetRealm(ctx, "g")
	if err := s.ResolveMatter(ctx, m, MatterResolution{Realm: st.Trackers, Laws: ls}, now); err != nil {
		t.Fatal(err)
	}
}

func TestLawsAndPetitions(t *testing.T) {
	s, ctx, now := lawCourt(t)
	c := newChar(t, s, "a", "merchant")
	if st, _ := s.GetRealm(ctx, "g"); len(st.Laws) != 0 {
		t.Fatalf("a new realm has no laws: %v", st.Laws)
	}
	ls := game.Laws{{Key: game.LawGrainDole, Since: now.Truncate(time.Second)}, {Key: game.LawFranchise, Since: now.Truncate(time.Second)}}
	passLaws(t, s, ctx, now, ls)
	st, _ := s.GetRealm(ctx, "g")
	if len(st.Laws) != 2 || !st.Laws.Has(game.LawGrainDole) || !st.Laws[0].Since.Equal(ls[0].Since) {
		t.Fatalf("laws round trip: %v", st.Laws)
	}
	// Characters carry the court's laws, and the franchise counts their estate.
	got, _ := s.GetCharacterByID(ctx, c.ID)
	if !got.Laws.Has(game.LawFranchise) || got.VoteWeight() <= got.Influence+got.Effects().VoteWeight {
		t.Fatalf("character laws %v, weight %v", got.Laws, got.VoteWeight())
	}

	// A petition is queued with its petitioner, who rides along onto the matter.
	if err := s.QueuePetition(ctx, "g", game.RepealKey(game.LawGrainDole), c.ID, now); err != nil {
		t.Fatal(err)
	}
	// It isn't a follow-up: it opens on its own, alongside the day's matter.
	if _, _, err := s.NextQueued(ctx, "g", now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a petition isn't a follow-up: %v", err)
	}
	day, err := s.CreateMatter(ctx, "g", "grand_games", 0, now, now.Add(time.Hour), now.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	qid, key, err := s.NextPetition(ctx, "g", now)
	if err != nil || key != game.RepealKey(game.LawGrainDole) {
		t.Fatalf("queued: %v %s", err, key)
	}
	m, err := s.CreateMatter(ctx, "g", key, qid, now, now.Add(time.Hour), now.Add(24*time.Hour))
	if err != nil || m.PetitionerID != c.ID {
		t.Fatalf("matter petitioner: %v %d", err, m.PetitionerID)
	}
	if again, _ := s.GetMatter(ctx, m.ID); again.PetitionerID != c.ID {
		t.Fatalf("stored petitioner: %d", again.PetitionerID)
	}
	if open, err := s.OpenMatters(ctx, "g"); err != nil || len(open) != 2 || open[0].ID != day.ID || open[1].ID != m.ID {
		t.Fatalf("both matters open: %v %+v", err, open)
	}
	if _, _, err := s.NextPetition(ctx, "g", now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the petition left the queue: %v", err)
	}
}

func TestSecretBallotDealsRunOnTrust(t *testing.T) {
	s, ctx, now := lawCourt(t)
	seller := newChar(t, s, "a", "scribe")
	buyer := newChar(t, s, "b", "merchant")
	_ = s.ApplyOutcome(ctx, "g", Outcome{Deltas: map[int64]game.Delta{buyer.ID: {Wealth: 100}}}, now)
	passLaws(t, s, ctx, now, game.Laws{{Key: game.LawSecretBallot}})
	m, _ := s.CreateMatter(ctx, "g", "grand_games", 0, now, now.Add(time.Hour), now.Add(2*time.Hour))
	o, _ := s.CreateOffer(ctx, VoteOffer{MatterID: m.ID, Kind: OfferSell, FromID: seller.ID, ToID: buyer.ID, Price: 25}, now)
	if _, err := s.AcceptOffer(ctx, o.ID, "lavish", now); err != nil {
		t.Fatal(err)
	}
	if _, _, sold := s.SoldVote(ctx, m.ID, seller.ID); sold {
		t.Fatal("under the Secret Ballot a sold vote isn't locked")
	}
	if err := s.CastVote(ctx, m.ID, seller.ID, "none", now); err != nil {
		t.Fatalf("the seller can still vote as they like: %v", err)
	}
	if v, _ := s.Votes(ctx, m.ID); v[seller.ID] != "none" {
		t.Fatalf("votes: %v", v)
	}
}

func TestDenunciation(t *testing.T) {
	s, ctx, now := lawCourt(t)
	owner := newChar(t, s, "a", "scribe")
	actor := newChar(t, s, "b", "merchant")
	id, err := s.CreateDenunciation(ctx, Denunciation{GuildID: "g", OwnerID: owner.ID, ActorID: actor.ID,
		Crime: "embezzle", Detail: "they stole"}, now.Add(-1000*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.GetDenunciation(ctx, id)
	if err != nil || d.OwnerID != owner.ID || d.ActorID != actor.ID || d.Used {
		t.Fatalf("get: %v %+v", err, d)
	}
	before, _ := s.GetRealm(ctx, "g")
	out := Outcome{Deltas: map[int64]game.Delta{owner.ID: {Wealth: 25}}, Realm: game.Realm{game.AttrTreasury: -1}}
	if err := s.Denounce(ctx, id, out, now); err != nil {
		t.Fatal(err)
	}
	if err := s.Denounce(ctx, id, out, now); !errors.Is(err, ErrDenounced) {
		t.Fatalf("denounce twice: %v", err)
	}
	after, _ := s.GetRealm(ctx, "g")
	got, _ := s.GetCharacterByID(ctx, owner.ID)
	if got.Wealth != owner.Wealth+25 || after.Trackers[game.AttrTreasury] >= before.Trackers[game.AttrTreasury] {
		t.Fatalf("paid once from the treasury: %d gold, treasury %v", got.Wealth, after.Trackers[game.AttrTreasury])
	}
	// The dossier keeps unused reports, newest first.
	older, _ := s.CreateDenunciation(ctx, Denunciation{GuildID: "g", OwnerID: owner.ID, ActorID: actor.ID,
		Crime: "bribe", Detail: "older"}, now.Add(-time.Hour))
	newer, _ := s.CreateDenunciation(ctx, Denunciation{GuildID: "g", OwnerID: owner.ID, ActorID: actor.ID,
		Crime: "rumor", Detail: "newer"}, now)
	_, _ = s.CreateDenunciation(ctx, Denunciation{GuildID: "g", OwnerID: actor.ID, ActorID: owner.ID,
		Crime: "plant", Detail: "someone else's"}, now)
	dos, err := s.Dossier(ctx, owner.ID)
	if err != nil || len(dos) != 2 || dos[0].ID != newer || dos[1].ID != older {
		t.Fatalf("dossier: %v %+v", err, dos)
	}
}
