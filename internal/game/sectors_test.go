package game

import (
	"math"
	"math/rand/v2"
	"testing"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func mustSector(t *testing.T, k SectorKey) SectorDef {
	t.Helper()
	d, ok := SectorByKey(k)
	if !ok {
		t.Fatalf("no sector %s", k)
	}
	return d
}

func TestSectorCatalogue(t *testing.T) {
	seen := map[SectorKey]bool{}
	for _, d := range SectorDefs {
		if seen[d.Key] || d.HouseStake <= 0 || d.BaseRevenue <= 0 || d.Growth <= 0 {
			t.Fatalf("bad sector %+v", d)
		}
		seen[d.Key] = true
	}
	// Every Senate sector effect points at a real sector.
	for _, ev := range MatterEvents {
		for _, o := range ev.Options {
			for _, out := range []MatterOutcome{o.Success, o.Failure} {
				for k := range out.Sectors {
					if !seen[k] {
						t.Errorf("%s/%s: unknown sector %s", ev.Key, o.Key, k)
					}
				}
			}
		}
	}
}

func TestShareAndDilution(t *testing.T) {
	d := mustSector(t, SectorAgriculture) // house stake 1200
	if !near(Share(d, 300, 300), 0.2) {
		t.Fatalf("share %v", Share(d, 300, 300))
	}
	// A second investor dilutes the first.
	if Share(d, 300, 600) >= Share(d, 300, 300) {
		t.Fatal("no dilution")
	}
	if Share(d, 0, 500) != 0 {
		t.Fatal("no stake, no share")
	}
}

func TestVentureIncome(t *testing.T) {
	s := StartingSectors()
	realm := Realm{AttrPopularity: 50, AttrStability: 50, AttrForeign: 50, AttrMilitary: 50}
	mine := Stakes{SectorAgriculture: 300}
	got := VentureIncome(s, realm, nil, mine, Stakes{SectorAgriculture: 300})
	if !near(got, 0.2*50*IncomeScale) {
		t.Fatalf("income %v", got)
	}
	// A shock halves it.
	s.Apply(map[SectorKey]SectorChange{SectorAgriculture: {Health: -0.5}})
	if !near(VentureIncome(s, realm, nil, mine, Stakes{SectorAgriculture: 300}), 0.2*25*IncomeScale) {
		t.Fatal("shock")
	}
}

func TestTrackerModifier(t *testing.T) {
	shadow := mustSector(t, SectorBlackMarket)
	trade := mustSector(t, SectorTrade)
	lawless := Realm{AttrStability: 10, AttrForeign: 50}
	if shadow.Modifier(lawless) <= 1 || trade.Modifier(lawless) >= 1 {
		t.Fatalf("low stability: shadow %v trade %v", shadow.Modifier(lawless), trade.Modifier(lawless))
	}
	if m := shadow.Modifier(Realm{AttrStability: -1000}); m != trackerModMax {
		t.Fatalf("clamp %v", m)
	}
}

func TestSectorHealthClampAndDrift(t *testing.T) {
	s := StartingSectors()
	s.Apply(map[SectorKey]SectorChange{SectorMining: {Health: -5, Capacity: 12}})
	if s[SectorMining].Health != HealthMin || s[SectorMining].Capacity != 52 {
		t.Fatalf("apply %+v", s[SectorMining])
	}
	for range 60 {
		s.Drift()
	}
	if math.Abs(s[SectorMining].Health-1) > 0.01 {
		t.Fatalf("drift %v", s[SectorMining].Health)
	}
	if s[SectorMining].Capacity != 52 {
		t.Fatal("capacity is permanent")
	}
}

func TestSectorGrowthDiminishes(t *testing.T) {
	agri := mustSector(t, SectorAgriculture)
	base := agri.BaseRevenue
	// Below its starting size, a sector grows linearly back up to it.
	if got := agri.Grow(base-10, 100); !near(got, base-10+100*agri.Growth) {
		t.Fatalf("below base: %v", got)
	}
	// At its start, the first gold grows it at the full rate...
	if got := agri.Grow(base, 1) - base; math.Abs(got-agri.Growth) > 1e-3 {
		t.Fatalf("first gold adds %v", got)
	}
	// ...and at twice its start, at about half the rate.
	if got := agri.Grow(2*base, 1) - 2*base; math.Abs(got-agri.Growth/2) > 1e-3 {
		t.Fatalf("at 2x adds %v", got)
	}
	// Splitting a sum gives the same result, across the base too.
	for _, start := range []float64{base - 3, base, 3 * base} {
		whole := agri.Grow(start, 1000)
		split := agri.Grow(agri.Grow(agri.Grow(start, 250), 250), 500)
		if math.Abs(whole-split) > 1e-9 {
			t.Fatalf("from %v: whole %v, split %v", start, whole, split)
		}
	}
}

func TestInvestTiers(t *testing.T) {
	agri := mustSector(t, SectorAgriculture)
	steward := Sheet{AttrStewardship: 5} // deal chance 60
	cases := []struct {
		roll int
		tier DealTier
	}{{10, DealGreat}, {40, DealFair}, {70, DealBad}}
	for _, c := range cases {
		res := Invest(rolls(t, c.roll), steward, agri, 100)
		if res.Tier != c.tier || !near(res.Stake, 100*dealStake[c.tier]) || res.Exposed {
			t.Errorf("roll %d: %+v", c.roll, res)
		}
	}
	// Only the Black Market exposes, and only on a bad miss.
	if Invest(rolls(t, 99), Sheet{}, agri, 100).Exposed {
		t.Fatal("agriculture exposed")
	}
	shadow := mustSector(t, SectorBlackMarket) // intrigue 0: chance 30
	if !Invest(rolls(t, 80), Sheet{}, shadow, 100).Exposed {
		t.Fatal("shadow should expose at margin -50")
	}
	if Invest(rolls(t, 50), Sheet{}, shadow, 100).Exposed {
		t.Fatal("shadow exposed at margin -20")
	}
}

// A fair deal in a steady sector should pay for itself in about two weeks.
func TestInvestPayback(t *testing.T) {
	realm := StartingRealm()
	for _, d := range SectorDefs {
		s := StartingSectors()
		s.Apply(map[SectorKey]SectorChange{d.Key: {Capacity: 100 * d.Growth}})
		inc := VentureIncome(s, realm, nil, Stakes{d.Key: 100}, Stakes{d.Key: 100})
		days := 100 / inc
		if days < 8 || days > 20 {
			t.Errorf("%s: 100 gold pays back in %.0f days", d.Key, days)
		}
	}
}

func TestMatterMovesSectors(t *testing.T) {
	ev, _ := MatterByKey("border_barbarians")
	in := MatterInput{MatterID: 1, Event: ev, Realm: StartingRealm(), Sectors: StartingSectors(),
		Players: []Player{{Person: person(1, Sheet{}), Vote: "ignore", Weight: 5}}}
	res := ResolveMatter(rand.New(rand.NewPCG(1, 2)), in)
	if res.Winner.Key != "ignore" {
		t.Fatalf("winner %s", res.Winner.Key)
	}
	want := 0.85 + 0.15*HealthDrift // the shock, then a day's recovery
	if !near(res.Sectors[SectorAgriculture].Health, want) {
		t.Fatalf("agriculture health %v, want %v", res.Sectors[SectorAgriculture].Health, want)
	}
	if in.Sectors[SectorAgriculture].Health != 1 {
		t.Fatal("input mutated")
	}
}
