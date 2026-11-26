package game

import (
	"math/rand/v2"
	"testing"
)

func TestMatterCatalogue(t *testing.T) {
	seen := map[string]bool{}
	base := 0
	for _, e := range MatterEvents {
		if seen[e.Key] {
			t.Fatalf("duplicate event %s", e.Key)
		}
		seen[e.Key] = true
		if !e.FollowUpOnly {
			base++
		}
		if _, ok := e.Option(e.Default); !ok {
			t.Errorf("%s: default %q is not an option", e.Key, e.Default)
		}
		if len(e.Options) < 2 || len(e.Options) > 4 {
			t.Errorf("%s: %d options", e.Key, len(e.Options))
		}
		keys := map[string]bool{}
		for _, o := range e.Options {
			if keys[o.Key] {
				t.Errorf("%s: duplicate option %s", e.Key, o.Key)
			}
			keys[o.Key] = true
			if o.Check != nil && o.Failure.Text == "" {
				t.Errorf("%s/%s: rolled option has no failure", e.Key, o.Key)
			}
		}
	}
	for _, e := range MatterEvents {
		for _, o := range e.Options {
			for _, out := range []MatterOutcome{o.Success, o.Failure} {
				for _, f := range out.FollowUps {
					if _, ok := MatterByKey(f.Event); !ok {
						t.Errorf("%s/%s: unknown follow-up %s", e.Key, o.Key, f.Event)
					}
				}
			}
		}
	}
	if base < 15 {
		t.Errorf("only %d daily events", base)
	}
}

// A two-option event to test senator behavior.
var testMatter = MatterEvent{
	Key: "test", Default: "cheap",
	Options: []MatterOption{
		{Key: "cheap", Success: MatterOutcome{Realm: R{}}},
		{Key: "spend", Leans: L{LeanMilitary: 2}, Success: MatterOutcome{Realm: R{AttrTreasury: -20}}},
	},
}

func TestTraitsDriveVotes(t *testing.T) {
	realm := StartingRealm()
	miser := Senator{ID: 1, Traits: []Trait{TraitEconomical}, BasePrice: 20}
	hawk := Senator{ID: 2, Traits: []Trait{TraitHawk, TraitBigSpender}, BasePrice: 20}
	for m := int64(0); m < 20; m++ {
		if got := miser.Preferred(testMatter, realm, m); got != "cheap" {
			t.Fatalf("economical senator voted %s", got)
		}
		if got := hawk.Preferred(testMatter, realm, m); got != "spend" {
			t.Fatalf("hawk voted %s", got)
		}
	}
	agree, _ := miser.BribePrice(testMatter, "cheap", realm, 1)
	against, _ := miser.BribePrice(testMatter, "spend", realm, 1)
	if agree >= against {
		t.Fatalf("buying an unwanted vote should cost more: %d vs %d", agree, against)
	}
	if _, ok := (Senator{Traits: []Trait{TraitIncorruptible}}).BribePrice(testMatter, "spend", realm, 1); ok {
		t.Fatal("incorruptible senator took a bribe")
	}
}

func TestInfluenceAndBribes(t *testing.T) {
	realm := StartingRealm()
	misers := []Senator{
		{ID: 1, Name: "A", Traits: []Trait{TraitEconomical}, Influence: 10},
		{ID: 2, Name: "B", Traits: []Trait{TraitEconomical}, Influence: 10},
	}
	weak := Player{Person: Person{ID: 7, Name: "P"}, Role: "officer", Vote: "spend", Weight: 5}
	strong := weak
	strong.Weight = 25

	r := rand.New(rand.NewPCG(1, 2))
	if res := ResolveMatter(r, MatterInput{Event: testMatter, Realm: realm, Senators: misers, Players: []Player{weak}}); res.Winner.Key != "cheap" {
		t.Fatalf("a low-influence player shouldn't outvote the senate")
	}
	res := ResolveMatter(r, MatterInput{Event: testMatter, Realm: realm, Senators: misers, Players: []Player{strong}})
	if res.Winner.Key != "spend" {
		t.Fatalf("a high-influence player should carry the vote")
	}
	if res.Deltas[7].Influence != WinningSideInfluence || res.Realm[AttrTreasury] > realm[AttrTreasury]-15 {
		t.Fatalf("effects: %+v %+v", res.Deltas[7], res.Realm)
	}
	// Bribing one senator makes it 10+5 vs 10.
	res = ResolveMatter(r, MatterInput{Event: testMatter, Realm: realm, Senators: misers, Players: []Player{weak}, Bribes: map[int64]string{1: "spend"}})
	if res.Winner.Key != "spend" {
		t.Fatal("bribe should swing the vote")
	}
	// Tie goes to the default.
	res = ResolveMatter(r, MatterInput{Event: testMatter, Realm: realm, Senators: misers[:1],
		Players: []Player{{Person: Person{ID: 8}, Vote: "spend", Weight: 10}}})
	if res.Winner.Key != "cheap" {
		t.Fatal("tie should go to the default")
	}
}

func TestFallAndFollowUps(t *testing.T) {
	ev := MatterEvent{Key: "x", Default: "a", Options: []MatterOption{{Key: "a",
		Success: MatterOutcome{Realm: R{AttrTreasury: -100}, FollowUps: []FollowUp{{"bread_riots", 1, 1}}}}}}
	res := ResolveMatter(rand.New(rand.NewPCG(1, 1)), MatterInput{Event: ev, Realm: StartingRealm()})
	if !res.Fallen || len(res.FollowUps) != 0 {
		t.Fatalf("bankrupt realm should fall with no follow-ups: %+v", res)
	}
	ev.Options[0].Success.Realm = R{}
	res = ResolveMatter(rand.New(rand.NewPCG(1, 1)), MatterInput{Event: ev, Realm: StartingRealm()})
	if res.Fallen || len(res.FollowUps) != 1 {
		t.Fatalf("certain follow-up should queue: %+v", res)
	}
}

func TestPickMatterAndSenate(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	for range 200 {
		e := PickMatter(r, StartingRealm(), []string{"grand_games"})
		if e.FollowUpOnly || e.Key == "grand_games" {
			t.Fatalf("picked %s", e.Key)
		}
	}
	s := GenerateSenate(r, SenateSize)
	if len(s) != SenateSize {
		t.Fatalf("senate size %d", len(s))
	}
	for _, x := range s {
		if len(x.Traits) != 2 || x.Has(TraitGreedy) && x.Has(TraitIncorruptible) {
			t.Fatalf("bad traits %v", x.Traits)
		}
	}
}

func TestVoteOffers(t *testing.T) {
	realm := StartingRealm()
	hawk := Senator{ID: 2, Traits: []Trait{TraitHawk, TraitBigSpender}}
	opt, small, ok := hawk.VoteOffer(testMatter, 5, realm, 1)
	if !ok || opt != "spend" || small <= 0 {
		t.Fatalf("hawk offer: %s %d %v", opt, small, ok)
	}
	if _, big, _ := hawk.VoteOffer(testMatter, 50, realm, 1); big <= small {
		t.Fatalf("more influence should be worth more: %d vs %d", big, small)
	}
	if _, _, ok := (Senator{Traits: []Trait{TraitIncorruptible}}).VoteOffer(testMatter, 5, realm, 1); ok {
		t.Fatal("incorruptible senators don't buy votes")
	}
	offers := BestVoteOffers(testMatter, []Senator{hawk, {ID: 1, Traits: []Trait{TraitEconomical}}}, 5, realm, 1)
	if offers["spend"].Senator.ID != 2 || offers["cheap"].Senator.ID != 1 {
		t.Fatalf("offers: %+v", offers)
	}
}
