package game

import (
	"math/rand/v2"
	"testing"
)

func TestAllLeansDerivesCostRiskAndOrder(t *testing.T) {
	realm := StartingRealm()
	spend := MatterOption{Key: "spend", Success: MatterOutcome{Realm: Realm{AttrTreasury: -20, AttrStability: 8}}}
	l := spend.AllLeans(realm)
	if l[LeanCost] <= 0 || l[LeanProfit] != 0 || l[LeanRisk] != 0 || l[LeanOrder] <= 0 {
		t.Fatalf("spend leans = %v", l)
	}
	gamble := MatterOption{Key: "gamble", Check: &Check{Base: 50}, Success: MatterOutcome{Realm: Realm{AttrTreasury: 20}}}
	if l := gamble.AllLeans(realm); l[LeanRisk] != 1 || l[LeanProfit] <= 0 {
		t.Fatalf("gamble leans = %v", l)
	}
}

func TestWitchHuntFinesOnlyMagicUnderTheAct(t *testing.T) {
	var magic, plain string
	for _, it := range Items {
		if it.Magic && magic == "" {
			magic = it.Key
		}
		if !it.Magic && plain == "" {
			plain = it.Key
		}
	}
	players := []Player{{Person: Person{ID: 1}, Items: []string{magic}}, {Person: Person{ID: 2}, Items: []string{plain}}}
	act := Laws{{Key: LawWitchcraftAct}}
	r := rand.New(rand.NewPCG(1, 1))
	fined := 0
	for n := 0; n < 500; n++ {
		res := MatterResult{Deltas: map[int64]Delta{}, Notices: map[int64][]string{}}
		witchHunt(r, &res, players, act)
		if res.Deltas[2].Wealth != 0 {
			t.Fatal("a plain item was fined")
		}
		if res.Deltas[1].Wealth < 0 {
			fined++
		}
		res = MatterResult{Deltas: map[int64]Delta{}, Notices: map[int64][]string{}}
		witchHunt(r, &res, players, nil)
		if len(res.Deltas) != 0 {
			t.Fatal("fined without the Witchcraft Act")
		}
	}
	if fined == 0 || fined > 150 {
		t.Fatalf("fined %d of 500 days", fined)
	}
}
