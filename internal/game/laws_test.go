package game

import (
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode"
)

// zero always rolls 0: every check passes, every chance hits.
type zero struct{}

func (zero) IntN(int) int { return 0 }

// top always rolls the highest: every check fails, no chance hits.
type top struct{}

func (top) IntN(n int) int { return n - 1 }

func lawsOf(keys ...LawKey) Laws {
	var ls Laws
	for _, k := range keys {
		ls = append(ls, Law{Key: k})
	}
	return ls
}

func TestLawsAreWellFormed(t *testing.T) {
	seen := map[LawKey]bool{}
	for _, d := range LawDefs {
		if seen[d.Key] {
			t.Fatalf("%s twice", d.Key)
		}
		seen[d.Key] = true
		if d.Name == "" || d.Emoji == "" || d.Desc == "" || d.Motion == "" || d.Repeal == "" || len(d.Traits) == 0 {
			t.Errorf("%s: missing text or traits", d.Key)
		}
		if strings.IndexFunc(d.Desc, unicode.IsDigit) >= 0 {
			t.Errorf("%s: players never see numbers, but the description has one", d.Key)
		}
		for tr := range d.Traits {
			if _, ok := TraitByKey(tr); !ok {
				t.Errorf("%s: unknown trait %s", d.Key, tr)
			}
		}
		if d.Rival != "" {
			r, ok := LawByKey(d.Rival)
			if !ok || r.Rival != d.Key {
				t.Errorf("%s: rival %s isn't its rival back", d.Key, d.Rival)
			}
		}
		for _, k := range []string{MotionKey(d.Key), RepealKey(d.Key)} {
			e, ok := MatterByKey(k)
			if !ok || e.Theme != ThemeLaw || e.Motion != d.Key || e.FollowUpOnly {
				t.Errorf("%s: motion %s is missing or wrong", d.Key, k)
			}
		}
	}
	// Every law an event passes or repeals is in the catalogue.
	for _, e := range MatterEvents {
		for _, o := range e.Options {
			for _, out := range []MatterOutcome{o.Success, o.Failure} {
				for _, k := range append(append([]LawKey(nil), out.Enact...), out.Repeal...) {
					if _, ok := LawByKey(k); !ok {
						t.Errorf("%s/%s: unknown law %s", e.Key, o.Key, k)
					}
				}
			}
		}
	}
}

func TestLawsApply(t *testing.T) {
	now := time.Unix(1000, 0)
	ls, ch := Laws(nil).Apply([]LawKey{LawGrainDole, LawGrainDole}, nil, now)
	if len(ls) != 1 || len(ch.Enacted) != 1 || !ls.Has(LawGrainDole) || !ls[0].Since.Equal(now) {
		t.Fatalf("enacting twice: %v %v", ls, ch)
	}
	// A rival is repealed.
	ls, _ = ls.Apply([]LawKey{LawDuelBan}, nil, now)
	ls, ch = ls.Apply([]LawKey{LawHonor}, nil, now)
	if ls.Has(LawDuelBan) || !ls.Has(LawHonor) || len(ch.Repealed) != 1 || ch.Repealed[0] != LawDuelBan {
		t.Fatalf("rival: %v %v", ls, ch)
	}
	// Past the cap, the oldest lapses.
	ls, _ = ls.Apply([]LawKey{LawSlander, LawInformers, LawMartial}, nil, now)
	if len(ls) != MaxLaws || !ls.Full() {
		t.Fatalf("want a full set: %v", ls)
	}
	ls, ch = ls.Apply([]LawKey{LawSorcery}, nil, now)
	if len(ls) != MaxLaws || ls.Has(LawGrainDole) || len(ch.Lapsed) != 1 || ch.Lapsed[0] != LawGrainDole {
		t.Fatalf("lapse: %v %v", ls, ch)
	}
	// Repealing what isn't law does nothing.
	if _, ch := ls.Apply(nil, []LawKey{LawDebasement}, now); !ch.Empty() {
		t.Fatalf("repealing nothing: %v", ch)
	}
	if _, ch := ls.Apply(nil, []LawKey{LawSorcery}, now); len(ch.Repealed) != 1 || ch.Describe() == "" {
		t.Fatalf("repeal: %v", ch)
	}
}

func TestMotionsComeUpOnlyWhenTheyMakeSense(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 3))
	for i := 0; i < 3000; i++ {
		if e := PickMatter(r, StartingRealm(), nil); e.Motion != "" {
			t.Fatalf("PickMatter knows no laws, but picked %s", e.Key)
		}
	}
	ls := lawsOf(LawGrainDole)
	motions := 0
	for i := 0; i < 3000; i++ {
		e := PickMatterAvoiding(r, StartingRealm(), nil, nil, ls)
		if e.Motion == "" {
			continue
		}
		motions++
		if !MotionAllowed(e, ls) || (e.Repeal && e.Motion != LawGrainDole) {
			t.Fatalf("picked %s with only the Grain Dole in force", e.Key)
		}
	}
	if motions == 0 || motions > 600 {
		t.Fatalf("%d motions in 3000 picks: laws should come up, but not too often", motions)
	}
	full := lawsOf(LawGrainDole, LawSlander, LawInformers, LawMartial, LawSorcery)
	for i := 0; i < 3000; i++ {
		if e := PickMatterAvoiding(r, StartingRealm(), nil, nil, full); e.Motion != "" && !e.Repeal {
			t.Fatalf("no room for a law, but %s came up", e.Key)
		}
	}
}

func TestSenatorsJudgeLawsByTheirTraits(t *testing.T) {
	ev, _ := MatterByKey(MotionKey(LawWitchcraftAct))
	pious := Senator{ID: 1, Traits: []Trait{TraitPious}}
	worldly := Senator{ID: 2, Traits: []Trait{TraitCosmopolitan, TraitRiskTaker}}
	piousYes, worldlyNo := 0, 0
	for m := int64(0); m < 100; m++ {
		if pious.Preferred(ev, StartingRealm(), m) == "pass" {
			piousYes++
		}
		if worldly.Preferred(ev, StartingRealm(), m) == "reject" {
			worldlyNo++
		}
	}
	// A little noise is allowed; the traits should decide most votes.
	if piousYes < 95 || worldlyNo < 70 {
		t.Fatalf("pious for %d/100, worldly against %d/100", piousYes, worldlyNo)
	}
	rep, _ := MatterByKey(RepealKey(LawWitchcraftAct))
	if got := pious.Preferred(rep, StartingRealm(), 1); got != "keep" {
		t.Fatalf("a pious senator voted %s on repealing it", got)
	}
}

func TestMotionPassesAndDailyLawsApply(t *testing.T) {
	ev, _ := MatterByKey(MotionKey(LawGrainDole))
	sen := []Senator{{ID: 1, Name: "S", Traits: []Trait{TraitPopulist}, Influence: 10}}
	now := time.Unix(5000, 0)
	res := ResolveMatter(rand.New(rand.NewPCG(1, 1)), MatterInput{Event: ev, Realm: StartingRealm(), Sectors: StartingSectors(), Senators: sen, Now: now})
	if res.Winner.Key != "pass" || !res.Laws.Has(LawGrainDole) || len(res.LawChanges.Enacted) != 1 {
		t.Fatalf("the dole should pass: %s %v", res.Winner.Key, res.Laws)
	}
	// The dole's daily cost and gain are on top of the day's drift.
	plain := StartingRealm()
	plain.Apply(plain.Drift())
	if res.Realm[AttrTreasury] >= plain[AttrTreasury] || res.Realm[AttrPopularity] <= plain[AttrPopularity] {
		t.Fatalf("daily dole: %v vs %v", res.Realm, plain)
	}
	if res.Sectors[SectorAgriculture].Capacity <= StartingSectors()[SectorAgriculture].Capacity {
		t.Fatal("the dole should grow the farms")
	}
}

func TestDailyLawsCanSinkTheRealm(t *testing.T) {
	// Martial Law left in force grinds the people's mood down to nothing.
	realm, sectors := StartingRealm(), StartingSectors()
	ls := lawsOf(LawMartial)
	r := rand.New(rand.NewPCG(2, 2))
	quiet := MatterEvent{Key: "quiet", Default: "a", Options: []MatterOption{{Key: "a"}}}
	for day := 0; day < 365; day++ {
		res := ResolveMatter(r, MatterInput{Event: quiet, Realm: realm, Sectors: sectors, Laws: ls})
		realm, sectors = res.Realm, res.Sectors
		if res.Fallen {
			if !strings.Contains(res.FallenReason, "Senate house") {
				t.Fatalf("fell for the wrong reason: %s", res.FallenReason)
			}
			return
		}
	}
	t.Fatalf("a year of martial law should end the realm, popularity %v", realm[AttrPopularity])
}

func TestSecretBallotAndAbstainers(t *testing.T) {
	sen := []Senator{{ID: 1, Name: "S", Traits: []Trait{TraitEconomical}, Influence: 10}}
	players := []Player{
		{Person: Person{ID: 1, Name: "Voter", Sheet: Sheet{AttrInfluence: 5}}, Vote: "spend", Weight: 5},
		{Person: Person{ID: 2, Name: "Quiet", Sheet: Sheet{AttrInfluence: 5}}, Weight: 5},
	}
	in := MatterInput{Event: testMatter, Realm: StartingRealm(), Senators: sen, Players: players}
	open := ResolveMatter(rand.New(rand.NewPCG(1, 1)), in)
	if open.Secret || len(open.Opinions) == 0 || open.Deltas[2].Influence != 0 {
		t.Fatalf("no laws: secret %v, %d opinions, abstainer %v", open.Secret, len(open.Opinions), open.Deltas[2])
	}
	in.Laws = lawsOf(LawSecretBallot, LawAbstainers)
	secret := ResolveMatter(rand.New(rand.NewPCG(1, 1)), in)
	if !secret.Secret || len(secret.Opinions) != 0 {
		t.Fatalf("under the Secret Ballot no senator knows how you voted: %v", secret.Opinions)
	}
	if secret.Deltas[2].Influence <= 0 || secret.Deltas[1].Influence > open.Deltas[1].Influence {
		t.Fatalf("abstainer %v, voter %v", secret.Deltas[2], secret.Deltas[1])
	}
}

func TestPetitionerIsJudgedByTheVote(t *testing.T) {
	ev, _ := MatterByKey(MotionKey(LawGrainDole))
	pet := []Player{{Person: Person{ID: 7, Name: "P", Sheet: Sheet{AttrInfluence: 5}}}}
	yes := []Senator{{ID: 1, Traits: []Trait{TraitPopulist}, Influence: 10}}
	no := []Senator{{ID: 1, Traits: []Trait{TraitEconomical}, Influence: 10}}
	won := ResolveMatter(rand.New(rand.NewPCG(1, 1)), MatterInput{Event: ev, Realm: StartingRealm(), Senators: yes, Players: pet, Petitioner: 7})
	lost := ResolveMatter(rand.New(rand.NewPCG(1, 1)), MatterInput{Event: ev, Realm: StartingRealm(), Senators: no, Players: pet, Petitioner: 7})
	if won.Deltas[7].Influence <= 0 || won.Deltas[7].Renown <= 0 || lost.Deltas[7].Renown >= 0 {
		t.Fatalf("won %v, lost %v", won.Deltas[7], lost.Deltas[7])
	}
	// Petitions follow the laws in force and the cap.
	if _, ok := PetitionByKey("pass:"+string(LawGrainDole), lawsOf(LawGrainDole)); ok {
		t.Fatal("can't petition for a law already in force")
	}
	if _, ok := PetitionByKey("repeal:"+string(LawGrainDole), lawsOf(LawGrainDole)); !ok {
		t.Fatal("should be able to petition to repeal a law in force")
	}
	// The cap never blocks a petition: passing one pushes out the oldest law,
	// unless it replaces its own rival.
	full := lawsOf(LawGrainDole, LawSlander, LawInformers, LawMartial, LawSorcery)
	passes := 0
	for _, p := range Petitions(full) {
		if !strings.HasPrefix(p.Key, "pass:") {
			if len(p.Lapses) != 0 {
				t.Fatalf("a repeal pushes nothing out: %s %v", p.Key, p.Lapses)
			}
			continue
		}
		passes++
		k := LawKey(strings.TrimPrefix(p.Key, "pass:"))
		_, ch := full.Apply([]LawKey{k}, nil, time.Time{})
		if !slices.Equal(p.Lapses, ch.Lapsed) {
			t.Fatalf("%s: warns %v, but %v lapse", p.Key, p.Lapses, ch.Lapsed)
		}
		if d, _ := LawByKey(k); d.Rival == LawSorcery {
			if len(p.Lapses) != 0 {
				t.Fatalf("%s replaces its rival, nothing lapses: %v", p.Key, p.Lapses)
			}
		} else if len(p.Lapses) != 1 || p.Lapses[0] != LawGrainDole {
			t.Fatalf("%s should push out the oldest law: %v", p.Key, p.Lapses)
		}
	}
	if passes == 0 {
		t.Fatal("a full set of laws should still allow pass petitions")
	}
	if p, _ := PetitionByKey("pass:"+string(LawSecretBallot), lawsOf(LawGrainDole)); len(p.Lapses) != 0 {
		t.Fatalf("with room, nothing lapses: %v", p.Lapses)
	}
	if !ResolvePetition(zero{}, Person{}, Petitions(nil)[0]).Success || !ResolvePetition(top{}, Person{}, Petitions(nil)[0]).Success {
		t.Fatal("a petition always reaches the floor")
	}
}

func TestWitchcraftAct(t *testing.T) {
	players := []Player{{Person: Person{ID: 1}, Items: []string{"one_ring", "painted_vase"}}}
	in := MatterInput{Event: testMatter, Realm: StartingRealm(), Players: players}
	if res := ResolveMatter(zero{}, in); res.Deltas[1].Wealth != 0 {
		t.Fatal("no fines without the Act")
	}
	in.Laws = lawsOf(LawWitchcraftAct)
	res := ResolveMatter(zero{}, in)
	ring, _ := ItemByKey("one_ring")
	if want := -int(float64(ring.Price) * LawWitchFineRate); res.Deltas[1].Wealth != want {
		t.Fatalf("only the enchanted ring is fined: %v, want %d", res.Deltas[1], want)
	}
	if ring.SellBackUnder(in.Laws) <= ring.SellBack() {
		t.Fatal("magic sells back for more under the Act")
	}
	vase, _ := ItemByKey("painted_vase")
	if vase.Magic || vase.SellBackUnder(in.Laws) != vase.SellBack() {
		t.Fatal("a vase is just a vase")
	}
}

func TestLawsBendEverydayRules(t *testing.T) {
	realm := StartingRealm()
	s := Senator{ID: 1, Traits: []Trait{TraitEconomical}, BasePrice: 30}
	plain, _ := s.BribePriceUnder(testMatter, "spend", realm, 1, 0, nil)
	dear, _ := s.BribePriceUnder(testMatter, "spend", realm, 1, 0, lawsOf(LawBribery))
	if dear <= plain {
		t.Fatalf("bribery law: %d vs %d", dear, plain)
	}
	if exposed, _, _ := BribeExposure(top{}, Person{}, nil, nil); exposed {
		t.Fatal("no law, no crime")
	}
	stern := []Senator{{ID: 3, Traits: []Trait{TraitStern}}, {ID: 4, Traits: []Trait{TraitGreedy}}}
	if exposed, d, ops := BribeExposure(top{}, Person{ID: 9}, stern, lawsOf(LawBribery)); !exposed || d.Renown >= 0 || len(ops) != 1 || ops[0].SenatorID != 3 {
		t.Fatalf("exposed %v %v %v", exposed, d, ops)
	}

	if FranchiseWeight(50, nil) != 0 || FranchiseWeight(50, lawsOf(LawFranchise)) <= FranchiseWeight(5, lawsOf(LawFranchise)) {
		t.Fatal("franchise weight should grow with the estate")
	}

	if IncomeUnder(20, []int{500}, nil) != 20 {
		t.Fatal("no laws, no change")
	}
	if got := IncomeUnder(20, []int{500}, lawsOf(LawPropertyTax)); got != 15 {
		t.Fatalf("property tax: %d", got)
	}
	if got := IncomeUnder(20, nil, lawsOf(LawDebasement)); got != 17 {
		t.Fatalf("debasement: %d", got)
	}
	if IncomeUnder(2, []int{5000}, lawsOf(LawPropertyTax)) != 0 {
		t.Fatal("income never goes negative")
	}
	ring, _ := ItemByKey("one_ring")
	if ring.PriceUnder(lawsOf(LawDebasement)) <= ring.Price {
		t.Fatal("debased coins buy less")
	}
	if PermitFee(nil) != 0 || PermitFee(lawsOf(LawMartial)) == 0 {
		t.Fatal("permits are for martial law")
	}

	// Revenue multipliers.
	if LawRevenue(lawsOf(LawProhibition), SectorAlchemy) >= 0.5 || LawRevenue(lawsOf(LawProhibition), SectorBlackMarket) <= 1 {
		t.Fatal("prohibition moves alchemy to the black market")
	}
	if LawRevenue(lawsOf(LawSorcery), SectorAlchemy) <= 1 || LawRevenue(nil, SectorAlchemy) != 1 {
		t.Fatal("licensed sorcery helps the alchemists")
	}

	// Monopolies: the leader's share grows, everyone else's shrinks.
	by := map[int64]Stakes{1: {SectorTrade: 100}, 2: {SectorTrade: 50}}
	plainBy, plainTot := EffectiveStakes(nil, by)
	monoBy, monoTot := EffectiveStakes(lawsOf(LawMonopolies), by)
	d, _ := SectorByKey(SectorTrade)
	if Share(d, monoBy[1][SectorTrade], monoTot[SectorTrade]) <= Share(d, plainBy[1][SectorTrade], plainTot[SectorTrade]) ||
		Share(d, monoBy[2][SectorTrade], monoTot[SectorTrade]) >= Share(d, plainBy[2][SectorTrade], plainTot[SectorTrade]) {
		t.Fatal("the charter should favor the biggest stake")
	}

	// Spies.
	if _, ok := RollLeak(rolls(t, 15), 20, nil); ok {
		t.Fatal("a low network shouldn't hear this without informers")
	}
	if d, ok := RollLeak(rolls(t, 15, 99), 20, lawsOf(LawInformers)); !ok || d != Partial {
		t.Fatalf("informers hear more, and more of it: %v %v", d, ok)
	}
	den := Denounce(stern, "embezzle", 5, 6, 10, lawsOf(LawInformers))
	if den.Accused.Renown >= 0 || den.Informer.Wealth <= 0 || den.Realm[AttrTreasury] >= 0 || !den.Paid || !den.Crime {
		t.Fatalf("denunciation: %+v", den)
	}
}

func TestDuelLaws(t *testing.T) {
	hawk := []Senator{{ID: 1, Traits: []Trait{TraitHawk}}, {ID: 2, Traits: []Trait{TraitStern}}}
	if ops := DeclineOpinions(hawk, 5, lawsOf(LawDuelBan)); len(ops) != 0 {
		t.Fatal("no shame in refusing a banned duel")
	}
	if ops := DeclineOpinions(hawk, 5, lawsOf(LawHonor)); len(ops) != 2 {
		t.Fatalf("under the Code of Honor the Stern join the Hawks: %v", ops)
	}
	if d := DeclineCost(lawsOf(LawHonor)); d.Renown >= 0 {
		t.Fatal("refusing is a disgrace under the Code of Honor")
	}
	if d, ops := ChallengeCost(hawk, 5, lawsOf(LawDuelBan)); d.Renown >= 0 || len(ops) != 1 {
		t.Fatalf("challenging under the ban: %v %v", d, ops)
	}

	a, b := duelPeople()
	bets := []Bet{{CharacterID: 3, SideID: 1, Amount: 100}, {CharacterID: 4, SideID: 2, Amount: 100}}
	in := DuelInput{Challenger: a, Defender: b, ChallengerRenown: 10, DefenderRenown: 10, ChallengerStance: StanceYield, Bets: bets}
	plain := ResolveDuel(zero{}, in)
	in.Laws = lawsOf(LawHonor)
	honor := ResolveDuel(zero{}, in)
	w := plain.Winner.ID
	if honor.Deltas[w].Renown <= plain.Deltas[w].Renown {
		t.Fatalf("honor renown: %v vs %v", honor.Deltas[w], plain.Deltas[w])
	}
	if honor.Realm[AttrTreasury] <= 0 || honor.Deltas[3].Wealth+honor.Deltas[4].Wealth >= 200 {
		t.Fatalf("the court takes a cut: %v %v %v", honor.Realm, honor.Deltas[3], honor.Deltas[4])
	}
}

func TestSlanderFine(t *testing.T) {
	host := Person{ID: 1, Name: "Host", Sheet: Sheet{}}
	target := Person{ID: 2, Name: "Target", Sheet: Sheet{AttrIntrigue: 100}}
	liar := Attendee{Person: Person{ID: 3, Name: "Liar"}, Goal: GoalSpreadRumors, Target: &target}
	res := PartyResult{Ledger: newLedger()}
	resolveRumor(zero{}, &res, host, liar, lawsOf(LawSlander))
	if res.Deltas[3].Wealth != -LawSlanderFine || res.Deltas[2].Wealth != LawSlanderFine {
		t.Fatalf("a named slanderer pays: %v", res.Deltas)
	}
	res = PartyResult{Ledger: newLedger()}
	resolveRumor(zero{}, &res, host, liar, nil)
	if res.Deltas[3].Wealth != 0 {
		t.Fatal("no law, no fine")
	}
}
