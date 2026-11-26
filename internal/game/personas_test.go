package game

import (
	"math/rand/v2"
	"testing"
)

func TestPersonas(t *testing.T) {
	if len(Personas) < SenateSize*2 {
		t.Fatalf("only %d personas; courts would all look alike", len(Personas))
	}
	seen := map[string]bool{}
	for _, p := range Personas {
		if seen[p.Name] || p.Bio == "" {
			t.Errorf("%q: duplicate or no bio", p.Name)
		}
		seen[p.Name] = true
		if len(p.Traits) < 1 || len(p.Traits) > 3 {
			t.Errorf("%s: %d traits", p.Name, len(p.Traits))
		}
		var have []Trait
		for _, tr := range p.Traits {
			ti, ok := TraitByKey(tr)
			if !ok {
				t.Errorf("%s: unknown trait %s", p.Name, tr)
				continue
			}
			if conflicts(have, ti) {
				t.Errorf("%s: %s conflicts with %v", p.Name, tr, have)
			}
			have = append(have, tr)
		}
	}
	// Every trait is used by someone.
	for _, ti := range TraitList {
		used := false
		for _, p := range Personas {
			for _, tr := range p.Traits {
				used = used || tr == ti.Key
			}
		}
		if !used {
			t.Errorf("no persona is %s", ti.Name)
		}
	}
}

func TestSenateUsesPersonas(t *testing.T) {
	sen := GenerateSenate(rand.New(rand.NewPCG(5, 6)), SenateSize)
	names := map[string]bool{}
	for _, s := range sen {
		p, ok := PersonaByName(s.Name)
		if !ok || names[s.Name] {
			t.Fatalf("%s is not a persona, or appears twice", s.Name)
		}
		names[s.Name] = true
		if len(p.Traits) != len(s.Traits) {
			t.Fatalf("%s: traits %v, persona has %v", s.Name, s.Traits, p.Traits)
		}
		for i := range p.Traits {
			if p.Traits[i] != s.Traits[i] {
				t.Fatalf("%s: traits %v, persona has %v", s.Name, s.Traits, p.Traits)
			}
		}
	}
}

// The new traits read what events do to the sectors and to order.
func TestSectorMindedTraits(t *testing.T) {
	realm := StartingRealm()
	score := func(tr Trait, event, opt string) float64 {
		ev, _ := MatterByKey(event)
		o, _ := ev.Option(opt)
		ti, _ := TraitByKey(tr)
		leans := o.AllLeans(realm)
		v := 0.0
		for l, w := range ti.Likes {
			v += w * leans[l]
		}
		return v
	}
	if score(TraitLandowner, "border_barbarians", "ignore") >= score(TraitLandowner, "border_barbarians", "tribute") {
		t.Error("a landowner should dislike leaving the farms to the barbarians")
	}
	if score(TraitMerchant, "bandit_roads", "patrols") <= score(TraitMerchant, "bandit_roads", "nothing") {
		t.Error("a merchant prince should want the roads patrolled")
	}
	if score(TraitStern, "bandit_roads", "patrols") <= score(TraitStern, "bandit_roads", "nothing") {
		t.Error("a stern senator should want order on the roads")
	}
	if score(TraitPious, "comet", "temple") <= score(TraitPious, "comet", "nothing") {
		t.Error("a pious senator should want the temple")
	}
}
