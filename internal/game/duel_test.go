package game

import (
	"math/rand/v2"
	"testing"
)

func TestStances(t *testing.T) {
	all := []string{StanceAttack, StanceGuard, StanceTrick}
	for _, a := range all {
		wins := 0
		for _, b := range all {
			if StanceMod(a, b) != -StanceMod(b, a) {
				t.Fatalf("stance mods not symmetric: %s/%s", a, b)
			}
			if StanceMod(a, b) > 0 {
				wins++
			}
		}
		if wins != 1 {
			t.Fatalf("%s beats %d stances, want 1", a, wins)
		}
	}
	if StanceMod(StanceAttack, StanceTrick) <= 0 || StanceMod(StanceTrick, StanceGuard) <= 0 || StanceMod(StanceGuard, StanceAttack) <= 0 {
		t.Fatal("wrong matchups")
	}
}

func TestDuelChance(t *testing.T) {
	weak, strong := Sheet{AttrProwess: 2}, Sheet{AttrProwess: 8}
	if DuelChance(strong, weak, 0) <= DuelChance(weak, weak, 0) || DuelChance(weak, strong, 0) >= 50 {
		t.Fatal("prowess should decide duels")
	}
	if DuelChance(Sheet{AttrProwess: 100}, weak, 0) != MaxChance || DuelChance(weak, Sheet{AttrProwess: 100}, 0) != MinChance {
		t.Fatal("duel chance should be clamped")
	}
}

func duelPeople() (Person, Person) {
	return Person{ID: 1, Name: "A", Sheet: Sheet{AttrProwess: 5}}, Person{ID: 2, Name: "B", Sheet: Sheet{AttrProwess: 5}}
}

func TestDuelResolution(t *testing.T) {
	r := rand.New(rand.NewPCG(7, 8))
	a, b := duelPeople()
	dove := Senator{ID: 9, Traits: []Trait{TraitDove}}
	for n := 0; n < 500; n++ {
		res := ResolveDuel(r, DuelInput{Challenger: a, Defender: b, ChallengerRenown: 3, DefenderRenown: 3,
			ChallengerStance: StanceAttack, DefenderStance: StanceTrick, Wager: 50, Senators: []Senator{dove}})
		w, l := res.Deltas[res.Winner.ID], res.Deltas[res.Loser.ID]
		if w.Wealth != 100 || l.Wealth != 0 {
			t.Fatalf("purse: %+v %+v", w, l)
		}
		if w.Renown < -l.Renown || l.Renown >= 0 {
			t.Fatalf("renown should change hands: %+v %+v", w, l)
		}
		if res.Wounded != (len(res.Conditions) == 1) {
			t.Fatal("wound without a condition")
		}
		if len(res.Opinions) != 2 || res.Opinions[0].Delta >= 0 {
			t.Fatalf("doves should disapprove of both: %+v", res.Opinions)
		}
	}
	// Yielding loses, without wounds, and costs half.
	for n := 0; n < 200; n++ {
		res := ResolveDuel(r, DuelInput{Challenger: a, Defender: b, ChallengerRenown: 3, DefenderRenown: 3, DefenderStance: StanceYield})
		if res.Winner.ID != a.ID || res.Wounded || res.Deltas[b.ID].Renown != -duelRenown/2 {
			t.Fatalf("yield: %+v", res)
		}
	}
	// A loser with no renown loses none.
	res := ResolveDuel(r, DuelInput{Challenger: a, Defender: b, DefenderStance: StanceYield})
	if res.Deltas[b.ID].Renown != 0 {
		t.Fatalf("renown below zero: %+v", res.Deltas[b.ID])
	}
}

func TestBetPayoutsConserveGold(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 1))
	for n := 0; n < 2000; n++ {
		var bets []Bet
		pot := 0
		for i := 0; i < 1+r.IntN(6); i++ {
			b := Bet{CharacterID: int64(10 + i), SideID: int64(1 + r.IntN(2)), Amount: 1 + r.IntN(200)}
			bets = append(bets, b)
			pot += b.Amount
		}
		out := BetPayouts(bets, 1)
		paid, backed := 0, false
		for _, b := range bets {
			paid += out[b.CharacterID]
			if b.SideID == 1 {
				backed = true
				if out[b.CharacterID] < b.Amount {
					t.Fatalf("a winning bet lost gold: %+v → %d", b, out[b.CharacterID])
				}
			}
		}
		if paid != pot {
			t.Fatalf("paid %d of a %d pot: %+v", paid, pot, bets)
		}
		for _, b := range bets {
			if backed && b.SideID != 1 && out[b.CharacterID] != 0 {
				t.Fatalf("a losing bet was paid: %+v", b)
			}
			if !backed && out[b.CharacterID] != b.Amount {
				t.Fatalf("no winners should refund everyone: %+v", out)
			}
		}
	}
}

func TestDeclineOpinions(t *testing.T) {
	sen := []Senator{{ID: 1, Traits: []Trait{TraitHawk}}, {ID: 2, Traits: []Trait{TraitDove}}}
	if ops := DeclineOpinions(sen, 5); len(ops) != 1 || ops[0].SenatorID != 1 || ops[0].Delta >= 0 {
		t.Fatalf("decline opinions: %+v", ops)
	}
}
