package game

import (
	"math"
	"math/rand/v2"
	"strings"
	"testing"
)

func TestRecklessSenators(t *testing.T) {
	// A gamble: probably fills the treasury, but a failure would bankrupt it.
	gamble := MatterOption{Key: "gamble", Leans: map[Lean]float64{LeanRisk: 3}, Check: flat(80),
		Success: MatterOutcome{Realm: Realm{AttrTreasury: 20}},
		Failure: MatterOutcome{Realm: Realm{AttrTreasury: -20}}}
	ev := MatterEvent{Key: "t", Default: "keep", Options: []MatterOption{gamble, {Key: "keep"}}}
	realm := Realm{AttrTreasury: 15, AttrMilitary: 60, AttrStability: 60, AttrPopularity: 60, AttrForeign: 60}

	if _, fatal := gamble.RealmDanger(realm); !fatal {
		t.Fatal("the worst case should be fatal")
	}
	if _, fatal := gamble.RecklessDanger(realm); fatal {
		t.Fatal("the expected case shouldn't be")
	}
	careful := Senator{ID: 1, Traits: []Trait{TraitRiskTaker, TraitHawk}}
	reckless := Senator{ID: 2, Traits: []Trait{TraitReckless, TraitRiskTaker}}
	for m := int64(0); m < 20; m++ {
		if careful.Preferred(ev, realm, m) != "keep" {
			t.Fatal("a careful senator gambled the realm")
		}
		if reckless.Preferred(ev, realm, m) != "gamble" {
			t.Fatal("a reckless senator passed up a good gamble")
		}
	}
	if _, ok := careful.BribePrice(ev, "gamble", realm, 1); ok {
		t.Error("a careful senator took a bribe to gamble the realm")
	}
	if _, ok := reckless.BribePrice(ev, "gamble", realm, 1); !ok {
		t.Error("a reckless senator should be bribable for it")
	}
	// Certain ruin scares even the reckless.
	ruin := MatterOption{Key: "ruin", Success: MatterOutcome{Realm: Realm{AttrTreasury: -20}}}
	if _, fatal := ruin.RecklessDanger(realm); !fatal {
		t.Error("certain ruin should scare everyone")
	}
}

func TestBillsAndAutoEvents(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 1))
	for i := 0; i < 500; i++ {
		if e := PickMatter(r, StartingRealm(), nil); e.Auto {
			t.Fatalf("picked auto event %s", e.Key)
		}
	}
	ev, _ := MatterByKey("raise_taxes")
	borrow, ok := ev.Option("borrow")
	if !ok || len(borrow.Success.FollowUps) == 0 {
		t.Fatal("borrowing should queue a bill")
	}
	bill, _ := MatterByKey(borrow.Success.FollowUps[0].Event)
	if !bill.Auto {
		t.Fatal("the bankers' bill should be automatic")
	}
	// The senators don't see it coming: borrowing looks like pure profit.
	if d, _ := borrow.RealmDanger(Realm{AttrTreasury: 5}); d != 0 {
		t.Error("borrowing shouldn't look dangerous")
	}
	realm := Realm{AttrTreasury: 20, AttrMilitary: 50, AttrStability: 50, AttrPopularity: 50}
	players := []Player{{Person: Person{ID: 1}, Role: "merchant", Vote: "pay", Weight: 5}}
	res := ResolveMatter(r, MatterInput{MatterID: 1, Event: bill, Realm: realm, Senators: GenerateSenate(r, SenateSize), Players: players})
	if res.Realm[AttrTreasury] != 0 || !res.Fallen {
		t.Errorf("a bill of 27 on a treasury of 20 should bankrupt it, got %v", res.Realm[AttrTreasury])
	}
	if len(res.Votes) != 0 || len(res.Opinions) != 0 {
		t.Error("nobody votes on a bill")
	}
	realm[AttrTreasury] = 60
	res = ResolveMatter(r, MatterInput{MatterID: 2, Event: bill, Realm: realm})
	if res.Realm[AttrTreasury] != 33 { // no drift: a bill isn't a Senate day
		t.Errorf("treasury after the bill = %v, want 33", res.Realm[AttrTreasury])
	}
}

func TestEmbezzle(t *testing.T) {
	thief := Person{ID: 1, Name: "Vane", Sheet: Sheet{AttrStewardship: 5, AttrIntrigue: 5}} // chance 80
	sen := []Senator{{ID: 1}, {ID: 2, Traits: []Trait{TraitIncorruptible}}}
	skim, _ := EmbezzleSizeByKey("skim")
	res := ResolveEmbezzle(&seq{t: t, vals: []int{0}}, thief, skim, 50, sen)
	if !res.Success || res.Treasury != -3 || res.Delta.Wealth != 60 {
		t.Errorf("skim = %+v", res)
	}
	res = ResolveEmbezzle(&seq{t: t, vals: []int{0}}, thief, skim, 2, sen) // only 2 left
	if res.Treasury != -2 || res.Delta.Wealth != 40 {
		t.Errorf("near-empty skim = %+v", res)
	}
	res = ResolveEmbezzle(&seq{t: t, vals: []int{90}}, thief, skim, 50, sen) // margin -10
	if res.Success || res.Caught || res.Treasury != 0 || res.Delta.Wealth != 0 {
		t.Errorf("quiet miss = %+v", res)
	}
	plunder, _ := EmbezzleSizeByKey("plunder") // chance 55
	res = ResolveEmbezzle(&seq{t: t, vals: []int{99}}, thief, plunder, 50, sen)
	if !res.Caught || res.Treasury != 0 || res.Delta.Renown >= 0 || res.Public == "" || len(res.Opinions) != 2 || res.Opinions[1].Delta != embezzleOutraged {
		t.Errorf("caught = %+v", res)
	}
}

func TestAid(t *testing.T) {
	if AidPoints(25) <= 0.9 || AidPoints(25) >= 1 {
		t.Errorf("25 gold = %v points", AidPoints(25))
	}
	if AidPoints(1000)/1000 >= AidPoints(100)/100 {
		t.Error("big gifts should buy less per coin")
	}
	p := Person{ID: 1, Name: "Vane", Sheet: Sheet{AttrInfluence: 0}}
	treasury, _ := AidTargetByKey("treasury")
	sen := []Senator{{ID: 1, Traits: []Trait{TraitEconomical}}, {ID: 2}}
	calm := ResolveAid(p, treasury, 250, 60, sen)
	crisis := ResolveAid(p, treasury, 250, 10, sen)
	if !crisis.Crisis || math.Abs(crisis.Delta.Renown-2*calm.Delta.Renown) > 1e-9 {
		t.Errorf("crisis should double rewards: %v vs %v", crisis.Delta, calm.Delta)
	}
	if len(calm.Opinions) != 1 || calm.Opinions[0].SenatorID != 1 {
		t.Errorf("only the economical senator should care: %+v", calm.Opinions)
	}
	if calm.Public == "" || ResolveAid(p, treasury, 25, 60, sen).Public != "" {
		t.Error("only big gifts are announced")
	}
	if full := ResolveAid(p, treasury, 250, 99.5, sen); full.Points > 0.5 {
		t.Errorf("can't fill past the top: %v", full.Points)
	}
}

func TestRealmWarnings(t *testing.T) {
	before := Realm{AttrTreasury: 30, AttrMilitary: 12, AttrStability: 50, AttrForeign: 30, AttrPopularity: 50}
	after := Realm{AttrTreasury: 20, AttrMilitary: 8, AttrStability: 50, AttrForeign: 5, AttrPopularity: 50}
	w := RealmWarnings(before, after)
	if len(w) != 2 || !strings.Contains(w[0], "treasury") || !strings.Contains(w[1], "danger") {
		t.Errorf("warnings = %q", w)
	}
	if len(RealmWarnings(after, after)) != 0 {
		t.Error("no crossing, no warning")
	}
}

func TestSectorPreview(t *testing.T) {
	ev, _ := MatterByKey("silver_vein")
	mine, _ := ev.Option("state_mine")
	s := DescribeArrows(SectorPreview(mine))
	if !strings.Contains(s, "Mining ▲▲ (lasting)") || !strings.Contains(s, "if it fails: ⛏️ Mining ▼▼") {
		t.Errorf("preview = %q", s)
	}
	keep, _ := ev.Option("leave")
	if DescribeArrows(SectorPreview(keep)) != "" {
		t.Error("no sector change, no preview")
	}
}
