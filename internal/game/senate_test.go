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
	offers := BestVoteOffers(testMatter, []Senator{hawk, {ID: 1, Traits: []Trait{TraitEconomical}}}, 5, realm, 1, nil)
	if offers["spend"].Senator.ID != 2 || offers["cheap"].Senator.ID != 1 {
		t.Fatalf("offers: %+v", offers)
	}
}

func TestPickMatterVariety(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	var recent []string
	for day := 0; day < 200; day++ {
		ev := PickMatter(r, StartingRealm(), recent)
		if ev.Key == "" || ev.FollowUpOnly {
			t.Fatalf("day %d: bad pick %+v", day, ev.Key)
		}
		for _, k := range recent[max(0, len(recent)-MatterCooldown):] {
			if k == ev.Key {
				t.Fatalf("day %d: %s repeated within %d matters", day, ev.Key, MatterCooldown)
			}
		}
		if len(recent) > 0 && lastTheme(recent) == ev.Theme {
			t.Fatalf("day %d: theme %s twice in a row", day, ev.Theme)
		}
		recent = append(recent, ev.Key)
	}
}

func TestEveryMatterHasATheme(t *testing.T) {
	random := 0
	for _, e := range MatterEvents {
		if e.Theme == "" {
			t.Errorf("%s has no theme", e.Key)
		}
		if !e.FollowUpOnly {
			random++
		}
		for _, o := range e.Options {
			for _, out := range []MatterOutcome{o.Success, o.Failure} {
				for _, f := range out.FollowUps {
					if _, ok := MatterByKey(f.Event); !ok {
						t.Errorf("%s/%s: unknown follow-up %s", e.Key, o.Key, f.Event)
					}
				}
			}
		}
		if _, ok := e.Option(e.Default); !ok {
			t.Errorf("%s: default %s is not an option", e.Key, e.Default)
		}
	}
	if random < MatterCooldown+10 {
		t.Fatalf("only %d random events; the cooldown would leave too few to pick from", random)
	}
}

func TestFollowUpNeverRepeatsLastMatter(t *testing.T) {
	if FollowUpAllowed("plague", []string{"grand_games", "plague"}) {
		t.Fatal("a follow-up must not repeat the matter just before")
	}
	if !FollowUpAllowed("plague", []string{"plague", "grand_games"}) || !FollowUpAllowed("plague", nil) {
		t.Fatal("follow-ups may break the cooldown")
	}
}

func TestSenatorsWontEndTheRealm(t *testing.T) {
	ev := MatterEvent{Key: "test", Default: "keep", Options: []MatterOption{
		{Key: "spend", Leans: map[Lean]float64{LeanPopular: 3, LeanMilitary: 3, LeanTradition: 3},
			Success: MatterOutcome{Realm: Realm{AttrTreasury: -15}}},
		{Key: "keep", Success: MatterOutcome{}},
	}}
	broke := Realm{AttrTreasury: 11, AttrMilitary: 70, AttrStability: 70, AttrPopularity: 70, AttrForeign: 70}
	if _, fatal := ev.Options[0].RealmDanger(broke); !fatal {
		t.Fatal("spending 15 from 11 should be fatal")
	}
	r := rand.New(rand.NewPCG(3, 4))
	for g := 0; g < 50; g++ {
		for i, s := range GenerateSenate(r, SenateSize) {
			s.ID = int64(i + 1)
			if p := s.Preferred(ev, broke, int64(g)); p != "keep" {
				t.Fatalf("senator %s (%v) voted to bankrupt the realm", s.Name, s.Traits)
			}
			if _, ok := s.BribePrice(ev, "spend", broke, int64(g)); ok {
				t.Fatalf("senator %s took a bribe to bankrupt the realm", s.Name)
			}
		}
	}
	rich := Realm{AttrTreasury: 60, AttrMilitary: 70, AttrStability: 70, AttrPopularity: 70, AttrForeign: 70}
	if d, fatal := ev.Options[0].RealmDanger(rich); fatal || d != 0 {
		t.Errorf("a rich realm shouldn't fear spending: %v %v", d, fatal)
	}
}
