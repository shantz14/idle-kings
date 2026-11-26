package game

import "testing"

func TestJourneyContentIsWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, d := range JourneyDests {
		if seen[d.Key] || d.Name == "" || d.Place == "" || d.Cost <= 0 || d.GoodText == "" || d.BadText == "" || len(d.Check.Weights) == 0 {
			t.Errorf("destination %q is incomplete or repeated", d.Key)
		}
		seen[d.Key] = true
		if d.Gift != "" {
			if it, ok := ItemByKey(d.Gift); !ok || it.Unique || it.Shelf != ShelfNone {
				t.Errorf("%s: gift %q must be a known, non-unique item the shop doesn't sell", d.Key, d.Gift)
			}
		}
	}
	prev := -1.0
	for _, b := range BanditBands {
		if b.Price <= 0 || b.Strength < prev || b.Reward.Renown <= 0 {
			t.Errorf("band %q: dearer bands should be tougher and pay off when beaten", b.Key)
		}
		prev = b.Strength
		if got, ok := BandFromGoal(BanditGoal(b.Key)); !ok || got.Key != b.Key {
			t.Errorf("band %q doesn't round-trip through its goal", b.Key)
		}
	}
	for _, e := range append(append([]PartyEvent{}, JourneyEvents...), JourneyAbroadEvents...) {
		if _, ok := e.Choice(e.Default); !ok {
			t.Errorf("road event %q has no valid default", e.Key)
		}
		if got, ok := EventByKey(e.Key); !ok || got.Title != e.Title {
			t.Errorf("road event %q isn't found by EventByKey", e.Key)
		}
	}
	home, _ := JourneyDestByKey("caravan")
	abroad, _ := JourneyDestByKey("carthage")
	if len(JourneyEventsFor(home)) >= len(JourneyEventsFor(abroad)) {
		t.Error("abroad should add its own road events")
	}
	if abroad.Scaled(DefaultJourneyTiming).Duration != 2*DefaultJourneyTiming.Duration {
		t.Error("abroad should take twice as long")
	}
}

func TestResolveJourney(t *testing.T) {
	d, _ := JourneyDestByKey("carthage")
	p := person(1, Sheet{AttrDiplomacy: 6})
	// Roll 0: a great success, then the gift roll (0 < 50).
	res := ResolveJourney(rolls(t, 0, 0), p, d, 0)
	if !res.Success || res.Gift != d.Gift || res.Realm[AttrForeign] <= 0 {
		t.Errorf("great success: %+v", res)
	}
	if res.Purse(d) != d.Cost+d.Success.Wealth {
		t.Errorf("purse %d, want cost plus earnings", res.Purse(d))
	}
	res = ResolveJourney(rolls(t, 99), p, d, 0)
	if res.Success || res.Gift != "" || len(res.Realm) != 0 {
		t.Errorf("failure: %+v", res)
	}
	if res.Purse(d) != d.Cost {
		t.Errorf("a failed journey still carries its travel funds: %d", res.Purse(d))
	}
}

func TestResolveAmbush(t *testing.T) {
	traveler := person(1, Sheet{AttrMartial: 5, AttrProwess: 5}) // defence 65 against cutthroats
	plain := person(2, Sheet{})
	cunning := person(2, Sheet{AttrIntrigue: 16})
	band, _ := BanditBandByKey("cutthroats")
	sen := []Senator{{ID: 1}, {ID: 2, Traits: []Trait{TraitStern}}}
	const purse = 100

	cases := []struct {
		name  string
		roll  []int
		hirer Person
		want  AmbushOutcome
	}{
		{"robbed", []int{99, 0}, plain, AmbushRobbed}, // margin -34, then wounded
		{"partly", []int{80}, plain, AmbushPartly},    // margin -15
		{"driven off", []int{50}, plain, AmbushDrivenOff},
		{"named", []int{0}, plain, AmbushNamed},
		{"cunning hirer stays hidden", []int{0}, cunning, AmbushDrivenOff},
	}
	for _, c := range cases {
		res := ResolveAmbush(rolls(t, c.roll...), traveler, c.hirer, band, purse, 0, sen)
		if res.Outcome != c.want {
			t.Errorf("%s: outcome %d, want %d", c.name, res.Outcome, c.want)
			continue
		}
		if res.Loot > purse || res.Cut != int(float64(res.Loot)*journeyHirerShare) {
			t.Errorf("%s: loot %d, cut %d", c.name, res.Loot, res.Cut)
		}
		if res.Traveler.Wealth != -res.Loot || res.Hirer.Wealth != res.Cut {
			t.Errorf("%s: gold moves wrong: %+v / %+v", c.name, res.Traveler, res.Hirer)
		}
		won := c.want == AmbushDrivenOff || c.want == AmbushNamed
		if won != (res.Traveler.Stats[Prowess] > 0 && res.Traveler.Renown > 0) {
			t.Errorf("%s: beating bandits should (only then) pay prowess and renown: %+v", c.name, res.Traveler)
		}
		named := c.want == AmbushNamed
		if named != (res.Public != "" && len(res.Opinions) == len(sen) && res.Hirer.Renown < 0) {
			t.Errorf("%s: only a named hirer is exposed: %+v", c.name, res)
		}
	}
	if res := ResolveAmbush(rolls(t, 99, 0), traveler, plain, band, purse, 0, sen); !res.Wounded {
		t.Error("the wound roll should have landed")
	}
	// A tougher band turns a win into a robbery.
	sell, _ := BanditBandByKey("sellswords")
	if res := ResolveAmbush(rolls(t, 50), traveler, plain, sell, purse, 0, sen); res.Outcome != AmbushPartly {
		t.Errorf("sellswords at roll 50: %d, want partly robbed", res.Outcome)
	}
	// Nothing to take, nothing taken.
	if res := ResolveAmbush(rolls(t, 99, 99), traveler, plain, band, 0, 0, sen); res.Loot != 0 || res.Cut != 0 {
		t.Errorf("empty purse: %+v", res)
	}
}
