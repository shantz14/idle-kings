package game

import (
	"math/rand/v2"
	"testing"
)

func TestOpinionPrices(t *testing.T) {
	realm := StartingRealm()
	s := Senator{ID: 1, Traits: []Trait{TraitEconomical}, BasePrice: 30}
	neutral, _ := s.BribePriceFor(testMatter, "spend", realm, 1, 0)
	if base, _ := s.BribePrice(testMatter, "spend", realm, 1); base != neutral {
		t.Fatalf("neutral opinion should not change price: %d vs %d", neutral, base)
	}
	friend, ok := s.BribePriceFor(testMatter, "spend", realm, 1, 80)
	if !ok || friend >= neutral {
		t.Fatalf("a friend should be cheaper: %d vs %d", friend, neutral)
	}
	cold, ok := s.BribePriceFor(testMatter, "spend", realm, 1, -40)
	if !ok || cold <= neutral {
		t.Fatalf("a cold senator should cost more: %d vs %d", cold, neutral)
	}
	if _, ok := s.BribePriceFor(testMatter, "spend", realm, 1, -70); ok {
		t.Fatal("a hostile senator should refuse your gold")
	}

	_, base, _ := s.VoteOfferFor(testMatter, 10, realm, 1, 0)
	if _, more, _ := s.VoteOfferFor(testMatter, 10, realm, 1, 80); more <= base {
		t.Fatalf("a friend should pay more: %d vs %d", more, base)
	}
	if _, _, ok := s.VoteOfferFor(testMatter, 10, realm, 1, -70); ok {
		t.Fatal("a hostile senator shouldn't buy your vote")
	}
}

func TestVotingChangesOpinion(t *testing.T) {
	realm := StartingRealm()
	miser := Senator{ID: 1, Name: "Miser", Traits: []Trait{TraitEconomical}, Influence: 10}
	hawk := Senator{ID: 2, Name: "Hawk", Traits: []Trait{TraitHawk, TraitBigSpender}, Influence: 10}
	p := Player{Person: Person{ID: 7}, Vote: "cheap", Weight: 1}
	abstain := Player{Person: Person{ID: 8}, Weight: 1}
	res := ResolveMatter(rand.New(rand.NewPCG(1, 1)), MatterInput{Event: testMatter, Realm: realm,
		Senators: []Senator{miser, hawk}, Players: []Player{p, abstain}})
	got := map[int64]float64{}
	for _, c := range res.Opinions {
		if c.CharacterID == 8 {
			t.Fatal("abstaining shouldn't change opinion")
		}
		got[c.SenatorID] = c.Delta
	}
	if got[1] <= 0 || got[2] >= 0 {
		t.Fatalf("opinion changes: %v", got)
	}
}

func TestSenatorsFollowFriends(t *testing.T) {
	realm := StartingRealm()
	// Find a matter this senator only barely prefers "cheap" on, so a beloved
	// player can tip them and a hated one can push them away.
	s := Senator{ID: 1, Name: "S", Traits: []Trait{TraitRiskTaker, TraitPopulist}, Influence: 10}
	var matter int64 = -1
	for m := int64(0); m < 200; m++ {
		a, _ := testMatter.Option("cheap")
		b, _ := testMatter.Option("spend")
		if d := s.Score(a, realm, m) - s.Score(b, realm, m); d > 0 && d < 1 {
			matter = m
			break
		}
	}
	if matter < 0 {
		t.Skip("no close matter found")
	}
	friend := Player{Person: Person{ID: 7}, Vote: "spend", Weight: 0.1}
	ops := CourtOpinions{1: {7: 100}}
	if got := s.decide(testMatter, realm, matter, []Player{friend}, nil); got != "cheap" {
		t.Fatalf("without opinion they vote %s", got)
	}
	if got := s.decide(testMatter, realm, matter, []Player{friend}, ops); got != "spend" {
		t.Fatal("a devoted senator should follow a friend on a close call")
	}
	enemy := Player{Person: Person{ID: 9}, Vote: "cheap", Weight: 0.1}
	ops = CourtOpinions{1: {9: -100}}
	if got := s.decide(testMatter, realm, matter, []Player{enemy}, ops); got != "spend" {
		t.Fatal("a hostile senator should vote against an enemy on a close call")
	}
}

func TestCourting(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	good := Person{ID: 1, Sheet: Sheet{AttrDiplomacy: 10, AttrStewardship: 8, AttrIntrigue: 10, AttrMartial: 10}}
	bad := Person{ID: 2, Sheet: Sheet{}}
	plain := Senator{ID: 1, Name: "S", Traits: []Trait{TraitPopulist}, BasePrice: 20}
	for _, a := range Approaches {
		var gGood, gBad float64
		for n := 0; n < 2000; n++ {
			gGood += ResolveCourt(r, CourtInput{Player: good, Senator: plain, Approach: a}).Delta
			gBad += ResolveCourt(r, CourtInput{Player: bad, Senator: plain, Approach: a}).Delta
		}
		if gGood <= gBad || gGood <= 0 {
			t.Errorf("%s: skilled courtiers should do better (%v vs %v)", a.Key, gGood/2000, gBad/2000)
		}
	}
	gift, _ := ApproachByKey("gift")
	honest := Senator{ID: 2, Name: "H", Traits: []Trait{TraitIncorruptible}}
	if res := ResolveCourt(r, CourtInput{Player: good, Senator: honest, Approach: gift}); res.Delta >= 0 {
		t.Fatal("the incorruptible should take offense at gifts")
	}
	// Gains shrink near the top, and opinion never leaves its range.
	flatter, _ := ApproachByKey("flatter")
	var low, high float64
	for n := 0; n < 2000; n++ {
		if d := ResolveCourt(r, CourtInput{Player: good, Senator: plain, Approach: flatter, Opinion: 0}).Delta; d > 0 {
			low += d
		}
		if d := ResolveCourt(r, CourtInput{Player: good, Senator: plain, Approach: flatter, Opinion: 90}).Delta; d > 0 {
			high += d
		}
	}
	if high >= low {
		t.Fatalf("gains should shrink at high opinion: %v vs %v", high, low)
	}
	if ClampOpinion(250) != OpinionMax || ClampOpinion(-250) != OpinionMin {
		t.Fatal("clamp")
	}
}

func TestPartyMovesOpinion(t *testing.T) {
	r := rand.New(rand.NewPCG(5, 6))
	host := Person{ID: 1, Name: "H", Sheet: Sheet{AttrInfluence: 10, AttrDiplomacy: 8}}
	res := ResolveParty(r, PartyInput{Host: host, Goal: GoalMakeFriends, Spend: 30})
	if len(res.Opinions) != 1 || res.Opinions[0].SenatorID != 0 || res.Opinions[0].CharacterID != 1 {
		t.Fatalf("party opinions: %+v", res.Opinions)
	}
	want := OpinionPartyFailure
	if res.Success {
		want = OpinionPartySuccess
	}
	if res.Opinions[0].Delta != want {
		t.Fatalf("party opinion %v, want %v", res.Opinions[0].Delta, want)
	}
}
