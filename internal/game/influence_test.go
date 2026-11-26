package game

import (
	"math/rand/v2"
	"testing"
)

func TestScaleInfluence(t *testing.T) {
	if ScaleInfluence(1, 0) != 1 || ScaleInfluence(1, 30) != 0.5 {
		t.Fatalf("scale: %v %v", ScaleInfluence(1, 0), ScaleInfluence(1, 30))
	}
	if ScaleInfluence(1, 60) >= ScaleInfluence(1, 30) {
		t.Fatal("gains should keep shrinking")
	}
	if ScaleInfluence(-1, 60) != -1 {
		t.Fatal("losses shouldn't be scaled")
	}
	if UsesSlot(KindWork) || !UsesSlot(KindCanvass) {
		t.Fatal("only public works should be free of the action slot")
	}
}

func TestCanvass(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	weak := Person{Sheet: Sheet{}}
	strong := Person{Sheet: Sheet{AttrDiplomacy: 10}}
	mighty := Person{Sheet: Sheet{AttrDiplomacy: 10, AttrInfluence: 60}}
	var w, s, m float64
	for n := 0; n < 3000; n++ {
		a, b, c := ResolveCanvass(r, weak), ResolveCanvass(r, strong), ResolveCanvass(r, mighty)
		if a.Influence <= 0 || b.Influence <= 0 || c.Influence <= 0 {
			t.Fatal("canvassing should always gain something")
		}
		w, s, m = w+a.Influence, s+b.Influence, m+c.Influence
	}
	if s <= w || m >= s {
		t.Fatalf("diplomacy should help and influence should slow it: weak %v strong %v mighty %v", w, s, m)
	}
}

func TestCircus(t *testing.T) {
	if CircusInfluence(400)/400 >= CircusInfluence(50)/50 {
		t.Fatal("each coin should help less")
	}
	if CircusInfluence(400) <= CircusInfluence(50) {
		t.Fatal("more gold should still help")
	}
	if CircusPopularity(100000) != circusPopularityMax {
		t.Fatal("popularity should be capped")
	}
	r := rand.New(rand.NewPCG(3, 3))
	sen := []Senator{{ID: 1, Traits: []Trait{TraitPopulist}}, {ID: 2, Traits: []Trait{TraitEconomical}}, {ID: 3, Traits: []Trait{TraitHawk}}}
	res := ResolveCircus(r, Person{ID: 7, Sheet: Sheet{AttrStewardship: 5}}, 150, sen)
	if len(res.Opinions) != 2 || res.Opinions[0].Delta <= 0 || res.Opinions[1].Delta >= 0 {
		t.Fatalf("circus opinions: %+v", res.Opinions)
	}
	if res.Delta.Influence <= 0 || res.Popularity <= 0 {
		t.Fatalf("circus gains: %+v", res)
	}
}

func TestSpeech(t *testing.T) {
	if SpeechChance(Sheet{AttrDiplomacy: 8}, 0) <= SpeechChance(Sheet{AttrDiplomacy: 2}, 0) {
		t.Fatal("diplomacy should carry a speech")
	}
	if HeckleChance(Sheet{AttrDiplomacy: 8}, Sheet{AttrDiplomacy: 2}) <= 50 || HeckleChance(Sheet{AttrDiplomacy: 2}, Sheet{AttrDiplomacy: 8}) >= 50 {
		t.Fatal("heckles should follow the diplomacy difference")
	}
	if HeckleChance(Sheet{AttrDiplomacy: 100}, Sheet{}) != MaxChance || HeckleChance(Sheet{}, Sheet{AttrDiplomacy: 100}) != MinChance {
		t.Fatal("heckle chance should be clamped")
	}

	r := rand.New(rand.NewPCG(5, 5))
	sp := Person{ID: 1, Name: "S", Sheet: Sheet{AttrDiplomacy: 6}}
	cause, _ := CauseByKey("strength")
	sen := []Senator{{ID: 1, Traits: []Trait{TraitHawk}}, {ID: 2, Traits: []Trait{TraitDove}}}
	cheer := Listener{Person: Person{ID: 2, Name: "C"}, Goal: GoalCheer}
	heckle := Listener{Person: Person{ID: 3, Name: "H", Sheet: Sheet{AttrDiplomacy: 10}}, Goal: GoalHeckle}
	wins := map[string]int{}
	for n := 0; n < 3000; n++ {
		for name, aud := range map[string][]Listener{"cheered": {cheer, cheer}, "alone": nil, "heckled": {heckle, heckle}} {
			res := ResolveSpeech(r, SpeechInput{Speaker: sp, Cause: cause, Audience: aud, Senators: sen})
			if res.Success {
				wins[name]++
				if len(res.Opinions) != 2 || res.Opinions[0].Delta <= 0 || res.Opinions[1].Delta >= 0 {
					t.Fatalf("cause opinions: %+v", res.Opinions)
				}
			} else if res.Deltas[sp.ID].Renown >= 0 {
				t.Fatal("a failed speech should cost renown")
			}
		}
	}
	if !(wins["cheered"] > wins["alone"] && wins["alone"] > wins["heckled"]) {
		t.Fatalf("cheers should help and heckles hurt: %v", wins)
	}
}

func TestWorks(t *testing.T) {
	seen := map[string]bool{}
	for _, w := range WorkDefs {
		if seen[w.Key] {
			t.Fatalf("duplicate work %s", w.Key)
		}
		seen[w.Key] = true
		if w.VoteWeight <= 0 || w.Cost <= 0 || w.Days <= 0 {
			t.Errorf("%s: bad numbers", w.Key)
		}
	}
	if e := MonumentEffects([]string{"statue", "aqueduct", "gone"}); e.VoteWeight != 3 {
		t.Fatalf("monument vote weight: %v", e.VoteWeight)
	}
	aq, _ := WorkByKey("aqueduct")
	res := CompleteWork(aq, Person{ID: 4, Name: "P"}, "Vane", []Senator{{ID: 1, Traits: []Trait{TraitPopulist}}, {ID: 2, Traits: []Trait{TraitHawk}}})
	if len(res.Opinions) != 1 || res.Realm[AttrPopularity] <= 0 || WorkTitle("Vane", aq) != "The Vane Aqueduct" {
		t.Fatalf("complete: %+v", res)
	}
}
