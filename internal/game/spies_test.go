package game

import (
	"math"
	"strings"
	"testing"
)

func spyPerson(id int64, intrigue float64) Person {
	return Person{ID: id, Name: "P" + string(rune('0'+id)), Sheet: Sheet{AttrIntrigue: intrigue}}
}

func TestSpyTiers(t *testing.T) {
	cases := map[float64]string{0: "None", 0.4: "None", 1: "Low", 14.9: "Low", 15: "Moderate", 35: "High", 60: "Very High", 85: "Extremely High", 100: "Extremely High"}
	for v, want := range cases {
		if got := SpyTierOf(v).Name; got != want {
			t.Errorf("SpyTierOf(%v) = %s, want %s", v, got, want)
		}
	}
	if TierIndex(90) != len(SpyTiers)-1 || TierIndex(0) != 0 {
		t.Error("TierIndex ends")
	}
}

func TestResolvePlant(t *testing.T) {
	spy, target := spyPerson(1, 10), spyPerson(2, 6) // chance 45 + 50 - 30 = 65
	none := SpyPurses[0]
	cases := []struct {
		name     string
		roll     int
		target   Person
		level    float64
		delta    float64
		exposure Exposure
		close    bool
	}{
		{"great", 0, target, 0, plantGreat, Unseen, false},
		{"good", 50, target, 0, plantGood, Unseen, false},
		{"nothing", 80, target, 0, 0, Unseen, false},
		{"detected", 95, target, 0, 0, Detected, false}, // nothing to lose at level 0
		{"detected loses", 95, target, 10, -plantDetectLoss, Detected, false},
		// target Intrigue 10: chance 45; roll 99 → margin -54
		{"caught", 99, spyPerson(2, 10), 10, -plantCaughtMin, Caught, false},
		// level 70: chance 45 - 17.5 → 27; roll 99 → caught, and close
		{"caught close", 99, spyPerson(2, 10), 70, -28, Caught, true},
	}
	for _, c := range cases {
		res := ResolvePlant(&seq{t: t, vals: []int{c.roll}}, PlantInput{Spy: spy, Target: c.target, Level: c.level, Purse: none})
		wantDelta := c.delta
		if c.delta > 0 {
			wantDelta = c.delta * (1 - c.level/150)
		}
		if math.Abs(res.Delta-wantDelta) > 1e-9 || res.Exposure != c.exposure || res.Close != c.close {
			t.Errorf("%s: got delta %v exposure %v close %v", c.name, res.Delta, res.Exposure, res.Close)
		}
		if g := res.Practice.Stats[Intrigue]; g <= 0 {
			t.Errorf("%s: no intrigue practice", c.name)
		}
		if res.Text == "" {
			t.Errorf("%s: no text", c.name)
		}
	}
	// Gold helps.
	if PlantMod(PlantInput{Target: target, Purse: SpyPurses[3]}) <= PlantMod(PlantInput{Target: target, Purse: none}) {
		t.Error("a fortune should help")
	}
}

func TestResolveRootOut(t *testing.T) {
	player := spyPerson(1, 10)
	nets := []Network{
		{Spy: spyPerson(2, 6), Level: 50}, // chance 40 + 50 - 30 + 10 = 70
		{Spy: spyPerson(3, 6), Level: 50},
		{Spy: spyPerson(4, 6), Level: 50},
		{Spy: spyPerson(5, 6), Level: 0}, // skipped, no roll
	}
	res := ResolveRootOut(&seq{t: t, vals: []int{0, 60, 90}}, RootInput{Player: player, Networks: nets})
	if len(res.Hits) != 2 {
		t.Fatalf("hits = %+v", res.Hits)
	}
	if h := res.Hits[0]; h.Spy.ID != 2 || h.Delta != -rootGreat || !h.Named {
		t.Errorf("great hit = %+v", h)
	}
	if h := res.Hits[1]; h.Spy.ID != 3 || h.Delta != -rootGood || h.Named {
		t.Errorf("good hit = %+v", h)
	}
	// Never below zero.
	res = ResolveRootOut(&seq{t: t, vals: []int{0}}, RootInput{Player: player, Networks: []Network{{Spy: spyPerson(2, 0), Level: 4}}})
	if res.Hits[0].Delta != -4 {
		t.Errorf("delta = %v, want -4", res.Hits[0].Delta)
	}
	// Practice whether or not anyone was found, so it gives nothing away.
	empty := ResolveRootOut(&seq{t: t}, RootInput{Player: player})
	if empty.Practice.Stats[Intrigue] != res.Practice.Stats[Intrigue] {
		t.Error("practice should not depend on what was found")
	}
}

func TestLeaks(t *testing.T) {
	prev := -1
	for v := 0.0; v <= 100; v += 5 {
		c := LeakChance(v)
		if c < prev {
			t.Fatalf("LeakChance not monotonic at %v", v)
		}
		prev = c
	}
	if _, ok := RollLeak(&seq{t: t}, 0); ok { // no network: no roll at all
		t.Error("leaked with no network")
	}
	if _, ok := RollLeak(&seq{t: t, vals: []int{99}}, 100); ok {
		t.Error("leaked on a miss")
	}
	if d, ok := RollLeak(&seq{t: t, vals: []int{10, 30}}, 100); !ok || d != Full {
		t.Errorf("deep network: %v %v, want full", d, ok)
	}
	if d, ok := RollLeak(&seq{t: t, vals: []int{10, 90}}, 100); !ok || d != Partial {
		t.Errorf("deep network: %v %v, want partial", d, ok)
	}
	if d, ok := RollLeak(&seq{t: t, vals: []int{5, 30}}, 20); !ok || d != Vague {
		t.Errorf("shallow network: %v %v, want vague", d, ok)
	}
	s := SecretBribe("Marcus Vane", "Corvus", "A Poor Harvest", "Open the granaries", 40)
	if !strings.Contains(s.Text(Full), "Corvus") || strings.Contains(s.Text(Partial), "Corvus") || strings.Contains(s.Text(Vague), "Corvus") {
		t.Error("only the full secret should name the senator")
	}
}

func TestCourtPractice(t *testing.T) {
	flatter, _ := ApproachByKey("flatter")
	sen := Senator{ID: 1, Name: "Corvus"}
	res := ResolveCourt(&seq{t: t, vals: []int{0}}, CourtInput{Player: Person{ID: 1, Sheet: Sheet{AttrDiplomacy: 10}}, Senator: sen, Approach: flatter})
	if !res.Success || res.Practice.Stats[Diplomacy] != practiceSuccess {
		t.Errorf("flatter practice = %+v", res.Practice)
	}
	for _, a := range Approaches {
		if a.Practice == "" {
			t.Errorf("%s has no practice stat", a.Key)
		}
	}
	gift, _ := ApproachByKey("gift")
	insulted := ResolveCourt(&seq{t: t}, CourtInput{Senator: Senator{Traits: []Trait{TraitIncorruptible}}, Approach: gift})
	if len(insulted.Practice.Stats) != 0 {
		t.Error("an insulted senator gives no practice")
	}
}
