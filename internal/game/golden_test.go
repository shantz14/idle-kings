package game

import (
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand/v2"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

var updateGolden = flag.Bool("update", false, "rewrite testdata/golden.txt")

// TestGolden pins what the rules decide at fixed seeds, across every law,
// matter, item and resolver, so refactors can prove they change nothing.
// After an intended rule change, regenerate with:
//
//	go test ./internal/game -run TestGolden -update
func TestGolden(t *testing.T) {
	got := goldenCases()
	const path = "testdata/golden.txt"
	var lines []string
	for name, v := range got {
		b, err := goldenJSON(v)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		lines = append(lines, fmt.Sprintf("%s %x", name, sha256.Sum256(b)))
	}
	sort.Strings(lines)
	text := strings.Join(lines, "\n") + "\n"
	if *updateGolden {
		_ = os.MkdirAll("testdata", 0o755)
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}
	wantLines := map[string]string{}
	for _, l := range strings.Split(strings.TrimSpace(string(want)), "\n") {
		name, sum, _ := strings.Cut(l, " ")
		wantLines[name] = sum
	}
	for _, l := range lines {
		name, sum, _ := strings.Cut(l, " ")
		if w, ok := wantLines[name]; !ok {
			t.Errorf("%s: new case (run with -update)", name)
		} else if w != sum {
			t.Errorf("%s: result changed", name)
		}
		delete(wantLines, name)
	}
	for name := range wantLines {
		t.Errorf("%s: case disappeared", name)
	}
}

// goldenJSON encodes v with floats rounded to 9 significant digits: the
// rules sum over maps, so the last bits of a float vary from run to run.
func goldenJSON(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var tree any
	if err := json.Unmarshal(b, &tree); err != nil {
		return nil, err
	}
	return json.Marshal(roundFloats(tree))
}

func roundFloats(v any) any {
	switch x := v.(type) {
	case float64:
		if x == 0 {
			return 0.0
		}
		return strconv.FormatFloat(x, 'g', 9, 64)
	case []any:
		for i := range x {
			x[i] = roundFloats(x[i])
		}
	case map[string]any:
		for k := range x {
			x[k] = roundFloats(x[k])
		}
	}
	return v
}

// goldenLaws is no laws, each law alone, and every law at once.
func goldenLaws() map[string]Laws {
	since := time.Unix(1_700_000_000, 0)
	out := map[string]Laws{"none": nil}
	var all Laws
	for _, d := range LawDefs {
		out[string(d.Key)] = Laws{{Key: d.Key, Since: since}}
		all = append(all, Law{Key: d.Key, Since: since})
	}
	out["all"] = all
	return out
}

func goldenPeople() (Person, Person, Person) {
	a := Person{ID: 1, UserID: "u1", Name: "Ada Vane", Sheet: Sheet{AttrProwess: 4, AttrDiplomacy: 5, AttrIntrigue: 3, AttrStewardship: 4, AttrInfluence: 20, AttrEstate: 6}}
	b := Person{ID: 2, UserID: "u2", Name: "Bo Reed", Sheet: Sheet{AttrProwess: 6, AttrDiplomacy: 2, AttrIntrigue: 6, AttrStewardship: 2, AttrInfluence: 8, AttrEstate: 3}}
	c := Person{ID: 3, UserID: "u3", Name: "Cy Moor", Sheet: Sheet{AttrProwess: 2, AttrDiplomacy: 7, AttrIntrigue: 2, AttrStewardship: 7, AttrInfluence: 40, AttrEstate: 9}}
	return a, b, c
}

func goldenCases() map[string]any {
	out := map[string]any{}
	r := rand.New(rand.NewPCG(42, 42))
	sen := GenerateSenate(r, SenateSize)
	for i := range sen {
		sen[i].ID = int64(i + 1)
	}
	out["senate"] = sen
	a, b, c := goldenPeople()
	realm, sectors := StartingRealm(), StartingSectors()
	now := time.Unix(1_800_000_000, 0)

	for lname, ls := range goldenLaws() {
		k := func(s string) string { return "law/" + lname + "/" + s }
		// Prices and income.
		var prices []any
		for _, it := range Items {
			prices = append(prices, []any{it.Key, it.PriceUnder(ls), it.SellBackUnder(ls)})
		}
		out[k("prices")] = prices
		out[k("income")] = []any{IncomeUnder(100, []int{200, 900}, ls), IncomeUnder(7, nil, ls),
			FranchiseWeight(30, ls), FranchiseWeight(300, ls), PermitFee(ls), DeclineCost(ls)}
		d, ops := ChallengeCost(sen, a.ID, ls)
		out[k("challenge")] = []any{d, ops, DeclineOpinions(sen, b.ID, ls)}
		out[k("daily")] = []any{DailyLawRealm(ls)}
		var revs []any
		for _, sd := range SectorDefs {
			revs = append(revs, []any{sd.Key, sectors.Revenue(sd, realm, ls), sectors.Condition(sd, realm, ls), LawRevenue(ls, sd.Key)})
		}
		out[k("sectors")] = revs
		by, totals := EffectiveStakes(ls, map[int64]Stakes{1: {SectorBlackMarket: 5, SectorDefs[0].Key: 3}, 2: {SectorDefs[0].Key: 9}})
		out[k("stakes")] = []any{by, totals, VentureIncome(sectors, realm, ls, by[1], totals)}
		// Secrets and misdeeds.
		var crimes []any
		for _, s := range goldenSecrets() {
			for det := Vague; det <= Detail(3); det++ {
				crimes = append(crimes, []any{s.Kind, det, IsCrime(s.Kind, ls), IsMisdeed(s.Kind, ls), CanExpose(s.Kind, det, ls)})
			}
			out[k("denounce/"+s.Kind)] = Denounce(sen, s.Kind, b.ID, a.ID, 20, ls)
		}
		out[k("crimes")] = crimes
		lr := rand.New(rand.NewPCG(5, 5))
		var leaks []any
		for lvl := 0.0; lvl <= 100; lvl += 5 {
			d, ok := RollLeak(lr, lvl, ls)
			leaks = append(leaks, []any{d, ok})
		}
		out[k("leaks")] = leaks
		br := rand.New(rand.NewPCG(6, 6))
		var exposures []any
		for n := 0; n < 20; n++ {
			e, d, ops := BribeExposure(br, b, sen, ls)
			exposures = append(exposures, []any{e, d, ops})
		}
		out[k("bribe_exposure")] = exposures
		// Petitions and motions.
		out[k("petitions")] = Petitions(ls)
		var motions []any
		for _, ev := range MatterEvents {
			if ev.Motion != "" {
				motions = append(motions, []any{ev.Key, MotionAllowed(ev, ls)})
			}
		}
		out[k("motions")] = motions
		// Picks.
		pr := rand.New(rand.NewPCG(7, 7))
		var picks []string
		var recent []string
		for n := 0; n < 60; n++ {
			ev := PickMatterAvoiding(pr, realm, recent, map[string]bool{}, ls)
			picks = append(picks, ev.Key)
			recent = append(recent, ev.Key)
		}
		out[k("picks")] = picks
		// Every matter under these laws.
		mr := rand.New(rand.NewPCG(8, 8))
		var matters []any
		players := []Player{
			{Person: a, Role: "merchant", Weight: 20, Items: []string{Items[0].Key}},
			{Person: b, Role: "general", Weight: 8},
			{Person: c, Role: "priest", Weight: 40},
		}
		for i, ev := range MatterEvents {
			if lname != "none" && lname != "all" && i%7 != 0 {
				continue // a sample under each single law keeps it quick
			}
			ps := append([]Player(nil), players...)
			for j := range ps {
				ps[j].Vote = ev.Options[(i+j)%len(ev.Options)].Key
			}
			res := ResolveMatter(mr, MatterInput{MatterID: int64(i + 1), Event: ev, Realm: realm, Sectors: sectors, Senators: sen,
				Players: ps, Laws: ls, Now: now, Bribes: map[int64]string{sen[0].ID: ev.Options[0].Key},
				Opinions: CourtOpinions{sen[1].ID: {a.ID: 40, b.ID: -30}}})
			matters = append(matters, []any{ev.Key, res})
		}
		out[k("matters")] = matters
	}

	// Senators' prices and offers, with and without laws.
	for _, lname := range []string{"none", "all"} {
		ls := goldenLaws()[lname]
		var offers []any
		for i, ev := range MatterEvents {
			if i%3 != 0 {
				continue
			}
			for _, s := range sen {
				for _, o := range ev.Options {
					p, ok := s.BribePriceUnder(ev, o.Key, realm, int64(i), 25, ls)
					p2, ok2 := s.BribePriceFor(ev, o.Key, realm, int64(i), -25)
					offers = append(offers, []any{ev.Key, s.ID, o.Key, p, ok, p2, ok2, s.Score(o, realm, int64(i))})
				}
				opt, gold, ok := s.VoteOfferFor(ev, 20, realm, int64(i), 10)
				offers = append(offers, []any{s.Preferred(ev, realm, int64(i)), opt, gold, ok})
			}
			offers = append(offers, BestVoteOffers(ev, sen, 20, realm, int64(i), Opinions{sen[0].ID: 50}))
		}
		out["offers/"+lname] = offers
	}

	// Activities.
	for seed := uint64(1); seed <= 6; seed++ {
		sr := func() *rand.Rand { return rand.New(rand.NewPCG(seed, seed*31)) }
		k := func(s string) string { return fmt.Sprintf("seed%d/%s", seed, s) }
		for _, lname := range []string{"none", "all"} {
			ls := goldenLaws()[lname]
			bp, cp := b, c
			for _, g := range []Goal{GoalMakeFriends, GoalTalkBusiness} {
				out[k("party/"+lname+"/"+string(g))] = ResolveParty(sr(), PartyInput{Host: a, Goal: g, Spend: 120, Quality: 2, Laws: ls,
					Attendees: []Attendee{{Person: b, Goal: GoalMakeFriends}, {Person: c, Goal: GoalSpreadRumors, Target: &bp}}})
			}
			for _, st := range []string{"", StanceAttack, StanceTrick, StanceYield} {
				out[k("duel/"+lname+"/"+st)] = ResolveDuel(sr(), DuelInput{Challenger: a, Defender: b, ChallengerRenown: 5, DefenderRenown: 2,
					ChallengerStance: StanceAttack, DefenderStance: st, Wager: 40, Senators: sen, Laws: ls,
					Bets: []Bet{{CharacterID: 3, SideID: 1, Amount: 30}}})
			}
			out[k("rootout/"+lname)] = ResolveRootOut(sr(), RootInput{Player: a, Laws: ls,
				Networks: []Network{{Spy: b, Level: 60}, {Spy: c, Level: 15}}})
			_ = cp
		}
		for _, g := range []Goal{GoalHuntGlory, GoalSaddleTalk} {
			cp := c
			out[k("hunt/"+string(g))] = ResolveHunt(sr(), HuntInput{Host: a, Goal: g, Quality: 1, Senators: sen,
				Riders: []HuntRider{{Person: b, Goal: GoalAccident, Target: &cp}, {Person: c, Goal: GoalRideFriends}}})
		}
		for _, cause := range Causes {
			out[k("speech/"+cause.Key)] = ResolveSpeech(sr(), SpeechInput{Speaker: a, Cause: cause, Senators: sen,
				Audience: []Listener{{Person: b, Goal: SpeechGoals[0].Key}, {Person: c, Goal: SpeechGoals[len(SpeechGoals)-1].Key}}})
		}
		out[k("circus")] = ResolveCircus(sr(), a, 150, sen)
		out[k("canvass")] = ResolveCanvass(sr(), a)
		for _, sz := range EmbezzleSizes {
			out[k("embezzle/"+sz.Key)] = ResolveEmbezzle(sr(), b, sz, 40, sen)
		}
		for _, band := range BanditBands {
			out[k("ambush/"+band.Key)] = ResolveAmbush(sr(), a, b, band, 80, 1, sen)
		}
		for _, d := range JourneyDests {
			out[k("journey/"+d.Key)] = ResolveJourney(sr(), a, d, 1)
		}
		ev := MatterEvents[int(seed)%len(MatterEvents)]
		out[k("votesale")] = ResolveVoteSale(sr(), VoteSaleInput{Seller: b, Senator: sen[0], Matter: ev, Option: ev.Options[0].Key,
			Realm: realm, MatterID: 3, Gold: 50, Recent: int(seed % 3)}, sen)
	}
	for _, w := range WorkDefs {
		out["work/"+w.Key] = CompleteWork(w, a, "Vane", sen)
	}
	for _, t := range AidTargets {
		out["aid/"+t.Key] = ResolveAid(a, t, 100, 30, sen)
	}
	for _, s := range goldenSecrets() {
		out["whisper/"+s.Kind] = Whisper(sen, s.Kind, b.ID)
	}
	var estate []any
	for _, it := range Items {
		estate = append(estate, []any{it.Key, EstateValue([]string{it.Key}), SumEffects([]string{it.Key}), EstateScore(float64(it.Price))})
	}
	out["estate"] = estate
	return out
}

// goldenSecrets is one secret of every kind.
func goldenSecrets() []Secret {
	return []Secret{
		SecretBribe("A", "S", "M", "o", 10), SecretHiredBandits("A", "T", "b", 10), SecretSoldToSenator("A", "S", "M", "o", 10),
		SecretVoteOffer("A", 2, "B", "M", "o", 10, true), SecretVoteDeal("A", 2, "B", "M", "o", 10, true), SecretVote("A", "M", "o"),
		SecretCourt("A", "S", "x"), SecretCourtResult("A", "S", true, false), SecretRumor("A", 2, "B"), SecretWhisper("A", "B", "w"),
		SecretPlant("A", 2, "B"), SecretRootOut("A"), SecretInvest("A", "s", 10), SecretEstateOffer("A", 2, "B", "x"),
		SecretEstateDeal("A", 2, "B", "x"), SecretCanvass("A"), SecretEmbezzle("A", 10), SecretMagic("A", "i"),
	}
}
