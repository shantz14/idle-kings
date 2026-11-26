package game

import (
	"math/rand/v2"
	"testing"
)

func TestHuntCatalogue(t *testing.T) {
	seen := map[string]bool{}
	for _, list := range [][]PartyEvent{PartyEvents, HuntEvents, DuelEvents} {
		for _, e := range list {
			if seen[e.Key] {
				t.Fatalf("duplicate event key %s", e.Key)
			}
			seen[e.Key] = true
			if _, ok := e.Choice(e.Default); !ok {
				t.Errorf("%s: default %q is not a choice", e.Key, e.Default)
			}
			if got, ok := EventByKey(e.Key); !ok || got.Key != e.Key {
				t.Errorf("EventByKey(%s)", e.Key)
			}
			for _, c := range e.Choices {
				if c.Check != nil && c.Failure.Text == "" {
					t.Errorf("%s/%s: rolled choice has no failure", e.Key, c.Key)
				}
				for _, o := range []EventOutcome{c.Success, c.Failure} {
					if o.Condition != "" {
						if _, ok := ConditionByKey(o.Condition); !ok {
							t.Errorf("%s/%s: unknown condition %s", e.Key, c.Key, o.Condition)
						}
					}
				}
			}
		}
	}
}

func TestHuntOdds(t *testing.T) {
	if HuntChance(Sheet{AttrProwess: 8}) <= HuntChance(Sheet{AttrProwess: 2}) {
		t.Fatal("prowess should help a hunt")
	}
	if HuntChance(Sheet{AttrMartial: 8}) <= HuntChance(Sheet{AttrMartial: 2}) {
		t.Fatal("martial should help a hunt")
	}
}

func TestHuntResolution(t *testing.T) {
	r := rand.New(rand.NewPCG(9, 9))
	host := Person{ID: 1, Name: "H", Sheet: Sheet{AttrProwess: 6, AttrMartial: 4}}
	hawk := Senator{ID: 3, Traits: []Trait{TraitHawk}}
	stranger := Person{ID: 5, Name: "S"}
	for n := 0; n < 300; n++ {
		res := ResolveHunt(r, HuntInput{Host: host, Goal: GoalHuntGlory, Senators: []Senator{hawk},
			Riders: []HuntRider{
				{Person: Person{ID: 2, Name: "G"}, Goal: GoalHuntGlory},
				{Person: Person{ID: 4, Name: "X"}, Goal: GoalAccident, Target: &stranger}, // not riding
			}})
		if res.Success != (len(res.Opinions) == 1) {
			t.Fatalf("a good hunt should please hawks: %+v", res.Opinions)
		}
		if len(res.Conditions) != 0 || len(res.Notices[4]) != 1 {
			t.Fatalf("an accident on someone not riding should fizzle: %+v %v", res.Conditions, res.Notices[4])
		}
	}
}

func TestAccidents(t *testing.T) {
	r := rand.New(rand.NewPCG(2, 3))
	host := Person{ID: 1, Name: "H"}
	victim := Person{ID: 2, Name: "V", Sheet: Sheet{AttrIntrigue: 10}}
	schemer := HuntRider{Person: Person{ID: 3, Name: "S", Sheet: Sheet{AttrIntrigue: 30}}, Goal: GoalAccident, Target: &victim}
	wounded, named := 0, 0
	for n := 0; n < 1000; n++ {
		res := ResolveHunt(r, HuntInput{Host: host, Riders: []HuntRider{{Person: victim, Goal: GoalRideFriends}, schemer}})
		for _, c := range res.Conditions {
			if c.CharacterID != victim.ID || c.Key != CondWounded {
				t.Fatalf("wrong wound: %+v", c)
			}
			wounded++
			if res.Deltas[victim.ID].Renown >= 0 {
				t.Fatal("a fall should cost renown")
			}
		}
		if res.Deltas[schemer.ID].Renown < 0 {
			named++
			if len(res.Opinions) != 1 || res.Opinions[0].SenatorID != 0 || res.Opinions[0].CharacterID != schemer.ID {
				t.Fatalf("a named culprit should lose the Senate's good opinion: %+v", res.Opinions)
			}
		}
	}
	if wounded < 500 {
		t.Fatalf("a skilled schemer should usually succeed: %d/1000", wounded)
	}
	if named == 0 {
		t.Fatal("culprits should sometimes be caught")
	}
}

func TestWoundsWeaken(t *testing.T) {
	e := ConditionEffects([]Condition{{Key: CondWounded}})
	if e.Stats[Prowess] >= 0 || e.Stats[Martial] >= 0 {
		t.Fatalf("wounds should weaken: %+v", e)
	}
}
