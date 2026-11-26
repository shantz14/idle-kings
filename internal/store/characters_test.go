package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/shantz14/idle-kings/internal/game"
)

func TestCreateCharacterWithEstate(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_ = s.ClaimAdmin(ctx, "g", "a")

	b := game.Build{Name: "Ada", Family: "Vane", Crest: game.Crest{Color: "red", Symbol: "lion", Motto: "We endure"},
		Points: game.Stats{game.Diplomacy: 4, game.Intrigue: 2}, Background: "foreign_exile", Role: "emissary"}
	c, err := s.CreateCharacter(ctx, "g", "a", b)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetCharacter(ctx, "g", "a")
	if err != nil {
		t.Fatal(err)
	}
	if got.Crest.Motto != "We endure" || got.Background != "foreign_exile" || len(got.Items) != 3 || got.Items[0] != "hill_villa" {
		t.Fatalf("loaded: %+v", got)
	}
	// Letters of Introduction add vote weight; base 2 + 4 points + 2 exile + 2 emissary.
	if got.VoteWeight() != c.Influence+2 || got.Stats[game.Diplomacy] != 10 || got.EstateValue() != 9+2+6 {
		t.Fatalf("derived: weight %g diplomacy %g estate %g", got.VoteWeight(), got.Stats[game.Diplomacy], got.EstateValue())
	}
	if got.Person().Effects.PartyBonus != 2+3 {
		t.Fatalf("party bonus %+v", got.Person().Effects)
	}

	if _, err := s.CreateCharacter(ctx, "g", "a", b); !errors.Is(err, ErrExists) {
		t.Fatalf("second character: %v", err)
	}
	if taken, _ := s.FamilyTaken(ctx, "g", " vane "); !taken {
		t.Fatal("family name should be taken, ignoring case and spaces")
	}
	if _, err := s.CreateCharacter(ctx, "g", "b", b); !errors.Is(err, ErrFamilyTaken) {
		t.Fatalf("same family: %v", err)
	}
	b.Points = game.Stats{game.Diplomacy: 1}
	b.Family = "Other"
	if _, err := s.CreateCharacter(ctx, "g", "b", b); err == nil {
		t.Fatal("invalid build should be rejected")
	}

	list, _ := s.CharactersInCourt(ctx, "g")
	if len(list) != 1 || len(list[0].Items) != 3 {
		t.Fatalf("list with items: %+v", list)
	}
}

func TestDrafts(t *testing.T) {
	ctx := context.Background()
	s, _ := Open(filepath.Join(t.TempDir(), "t.db"))
	defer s.Close()
	var d game.Build
	if ok, err := s.GetDraft(ctx, "g", "u", &d); ok || err != nil {
		t.Fatalf("empty: %v %v", ok, err)
	}
	if err := s.SaveDraft(ctx, "g", "u", game.Build{Name: "Ada"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveDraft(ctx, "g", "u", game.Build{Name: "Bea"}); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.GetDraft(ctx, "g", "u", &d); !ok || d.Name != "Bea" {
		t.Fatalf("draft: %+v", d)
	}
	_ = s.DeleteDraft(ctx, "g", "u")
	if ok, _ := s.GetDraft(ctx, "g", "u", &d); ok {
		t.Fatal("draft should be gone")
	}
}
