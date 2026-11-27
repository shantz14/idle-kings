package game

import "testing"

func TestSabotageTiers(t *testing.T) {
	in := SabotageInput{Actor: Person{ID: 1, Sheet: Sheet{AttrIntrigue: 4}}, Target: Person{ID: 2, Sheet: Sheet{AttrIntrigue: 2}}} // chance 50
	if SabotageChance(in) != 50 {
		t.Fatalf("chance %d", SabotageChance(in))
	}
	in.Spies, in.Purse = 50, SpyPurse{Bonus: 15}
	if SabotageChance(in) != 75 {
		t.Fatalf("spies and purse: %d", SabotageChance(in))
	}
	in.Spies, in.Purse = 0, SpyPurse{}
	sen := []Senator{{ID: 1, Traits: []Trait{TraitStern}}, {ID: 2}}
	cases := []struct {
		roll                      int
		ok, great, noticed, named bool
		factor                    float64
	}{
		{5, true, true, false, false, sabotageVentureGreat},    // margin 45
		{40, true, false, false, false, sabotageVentureFactor}, // margin 10
		{60, false, false, false, false, 0},                    // margin -10
		{80, false, false, true, false, 0},                     // margin -30
		{99, false, false, true, true, 0},                      // margin -49
	}
	for _, c := range cases {
		res := ResolveSabotage(rolls(t, c.roll), in, sen)
		if res.Success != c.ok || res.Great != c.great || res.Noticed != c.noticed || res.Named != c.named || (c.ok && res.VentureFactor != c.factor) {
			t.Errorf("roll %d: %+v", c.roll, res)
		}
		if c.named && (res.Actor.Renown >= 0 || len(res.Opinions) != 1) {
			t.Errorf("named saboteur: %+v", res)
		}
	}
	if RepairCost(Item{Price: 1000}) != 100 || RepairCost(Item{Price: 10}) != 5 {
		t.Fatal("repair cost")
	}
}

func TestDamagedHomeStillHome(t *testing.T) {
	var home, other Item
	for _, it := range Items {
		if it.IsProperty() && it.Effects.PartyBonus > 0 && home.Key == "" {
			home = it
		} else if it.IsProperty() && it.Effects.PartyBonus > 0 && other.Key == "" && it.Key != home.Key {
			other = it
		}
	}
	keys := []string{home.Key, other.Key}
	whole := SumEffectsDamaged(keys, nil)
	broken := SumEffectsDamaged(keys, map[int]bool{0: true})
	if broken.PartyBonus != 0 {
		t.Fatalf("a damaged home's party bonus should be gone, and the other house still counts as not home: %+v", broken)
	}
	if whole.PartyBonus != home.Effects.PartyBonus {
		t.Fatalf("undamaged: %+v", whole)
	}
}

func TestSmearTiers(t *testing.T) {
	a := Person{ID: 1, Name: "A", Sheet: Sheet{AttrIntrigue: 5}} // chance 55
	tg := Person{ID: 2, Name: "T"}                               // notice 10, disadvantage
	res := ResolveSmear(rolls(t, 10, 99, 99), SmearInput{Actor: a, Target: tg})
	if !res.Landed || !near(res.Deltas[2].Influence, -smearGreatInfl) || len(res.Opinions) != 1 || res.Opinions[0].Delta != smearGreatOp {
		t.Fatalf("great smear: %+v", res)
	}
	res = ResolveSmear(rolls(t, 50, 99, 99), SmearInput{Actor: a, Target: tg})
	if !res.Landed || !near(res.Deltas[2].Influence, -smearInfluence) {
		t.Fatalf("smear: %+v", res)
	}
	res = ResolveSmear(rolls(t, 90, 99, 99), SmearInput{Actor: a, Target: tg})
	if res.Landed || res.Deltas[2].Influence != 0 {
		t.Fatalf("failed smear: %+v", res)
	}
	// A sharp target catches it, names the smearer and stops it.
	sharp := Person{ID: 2, Name: "T", Sheet: Sheet{AttrIntrigue: 18}} // notice 100
	res = ResolveSmear(rolls(t, 0, 0, 0), SmearInput{Actor: a, Target: sharp})
	if res.Landed || res.Deltas[1].Renown >= 0 {
		t.Fatalf("stopped smear: %+v", res)
	}
}
