package game

import (
	"math/rand/v2"
	"testing"
)

func askIn(s Senator, op float64, diplomacy float64) AskInput {
	realm := StartingRealm()
	return AskInput{
		Player:  Person{ID: 7, Name: "P", Sheet: Sheet{AttrDiplomacy: diplomacy}},
		Senator: s, Opinion: op, Event: testMatter, Realm: realm, MatterID: 3,
		Truth: SenatorIntent(testMatter, realm, 3, s, nil, nil, ""),
	}
}

func TestAskBribedAlwaysTruth(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	in := askIn(Senator{ID: 1, Name: "S", Traits: []Trait{TraitScheming}}, -100, 0)
	in.BribedByYou = true
	for range 200 {
		if res := ResolveAsk(r, in); res.Kind != AskTruth || res.Option != in.Truth {
			t.Fatalf("a senator you paid answered %v %q", res.Kind, res.Option)
		}
	}
}

func TestAskHonestNeverLie(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for _, tr := range askHonest {
		for _, op := range []float64{-100, -30, 0, 50} {
			in := askIn(Senator{ID: 1, Name: "S", Traits: []Trait{tr}}, op, 0)
			for range 300 {
				if res := ResolveAsk(r, in); res.Kind == AskLie {
					t.Fatalf("%s senator lied at opinion %v", tr, op)
				}
			}
		}
	}
}

func TestAskHostileSchemerLies(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	in := askIn(Senator{ID: 1, Name: "S", Traits: []Trait{TraitScheming, TraitEconomical}}, -80, 20)
	for range 100 {
		res := ResolveAsk(r, in)
		if res.Kind != AskLie {
			t.Fatalf("hostile schemer answered %v", res.Kind)
		}
		if res.Option == in.Truth || res.Option == "" {
			t.Fatalf("a lie named %q (truth %q)", res.Option, in.Truth)
		}
	}
	plain := askIn(Senator{ID: 1, Name: "S", Traits: []Trait{TraitEconomical}}, -80, 20)
	if res := ResolveAsk(r, plain); res.Kind != AskRefuse {
		t.Fatalf("hostile senator answered %v, want a refusal", res.Kind)
	}
}

func TestAskLiesNeverTellTheTruth(t *testing.T) {
	r := rand.New(rand.NewPCG(5, 6))
	for _, tr := range []Trait{TraitEconomical, TraitScheming, TraitHawk, TraitGreedy} {
		in := askIn(Senator{ID: 2, Name: "S", Traits: []Trait{tr}}, 0, 0)
		for range 300 {
			res := ResolveAsk(r, in)
			switch res.Kind {
			case AskLie:
				if res.Option == in.Truth {
					t.Fatalf("%s: a lie told the truth", tr)
				}
			case AskTruth:
				if res.Option != in.Truth {
					t.Fatalf("%s: the truth named %q, not %q", tr, res.Option, in.Truth)
				}
			case AskRefuse:
				if res.Option != "" {
					t.Fatalf("%s: a refusal named %q", tr, res.Option)
				}
			}
			if res.Text == "" {
				t.Fatalf("%s: no text for %v", tr, res.Kind)
			}
		}
	}
}

func TestAskDiplomacyAndOpinionHelp(t *testing.T) {
	r := rand.New(rand.NewPCG(7, 8))
	truths := func(in AskInput) int {
		n := 0
		for range 1000 {
			if ResolveAsk(r, in).Kind == AskTruth {
				n++
			}
		}
		return n
	}
	s := Senator{ID: 1, Name: "S", Traits: []Trait{TraitEconomical}}
	low, high := truths(askIn(s, 0, 2)), truths(askIn(s, 80, 12))
	if high < 800 || low >= high {
		t.Fatalf("a devoted friend with high Diplomacy should mostly hear the truth: %d vs %d", low, high)
	}
}

// SenatorIntent must be how ResolveMatter actually counts the senator.
func TestSenatorIntentMatchesTheVote(t *testing.T) {
	realm := StartingRealm()
	sen := []Senator{
		{ID: 1, Name: "A", Traits: []Trait{TraitEconomical}, Influence: 10},
		{ID: 2, Name: "B", Traits: []Trait{TraitHawk, TraitBigSpender}, Influence: 10},
		{ID: 3, Name: "C", Traits: []Trait{TraitDove}, Influence: 10},
	}
	players := []Player{
		{Person: Person{ID: 7, Name: "P"}, Vote: "spend", Weight: 5},
		{Person: Person{ID: 8, Name: "Q"}, Vote: "cheap", Weight: 5},
	}
	for m := int64(0); m < 30; m++ {
		ops := CourtOpinions{1: {7: 100}, 3: {8: -100, 7: 60}}
		bribes := map[int64]string{}
		if m%3 == 0 {
			bribes[2] = "cheap"
		}
		res := ResolveMatter(rand.New(rand.NewPCG(1, 1)), MatterInput{MatterID: m, Event: testMatter, Realm: realm,
			Senators: sen, Players: players, Opinions: ops, Bribes: bribes})
		for _, s := range sen {
			want := SenatorIntent(testMatter, realm, m, s, players, ops, bribes[s.ID])
			for _, v := range res.Votes {
				if v.AI && v.Name == s.Name && v.Option != want {
					t.Fatalf("matter %d: %s voted %s, intent said %s", m, s.Name, v.Option, want)
				}
			}
		}
	}
}

func TestEveryGreetingExists(t *testing.T) {
	s := Senator{ID: 1, Name: "S"}
	for _, band := range OpinionBands {
		for _, again := range []bool{false, true} {
			if len(greetings[greetingKey{band, again}]) == 0 {
				t.Errorf("no greetings for %s again=%v", band, again)
			}
		}
	}
	for _, op := range []float64{-100, -60, -59, -20, 0, 19.9, 20, 59.9, 60, 100} {
		if BandOf(op) == "" || Greeting(s, op, 0) == "" || Greeting(s, op, 3) == "" {
			t.Errorf("no greeting at %v", op)
		}
		if string(BandOf(op)) != lower(OpinionWord(op)) {
			t.Errorf("band %s doesn't match %s", BandOf(op), OpinionWord(op))
		}
	}
}

func lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}
