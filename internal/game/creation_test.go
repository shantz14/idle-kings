package game

import "testing"

func goodBuild() Build {
	return Build{
		Name: "Ada", Family: "Vane",
		Crest:      Crest{Color: "red", Symbol: "lion", Motto: "We endure"},
		Points:     Stats{Diplomacy: 4, Intrigue: 2},
		Background: "fallen_nobility", Role: "cupbearer",
	}
}

func TestBuildValidate(t *testing.T) {
	if err := goodBuild().Validate(); err != nil {
		t.Fatal(err)
	}
	bad := map[string]func(*Build){
		"unspent":    func(b *Build) { b.Points = Stats{Diplomacy: 3} },
		"overspent":  func(b *Build) { b.Points = Stats{Diplomacy: 4, Intrigue: 3} },
		"over cap":   func(b *Build) { b.Points = Stats{Diplomacy: 5, Intrigue: 1} },
		"negative":   func(b *Build) { b.Points = Stats{Diplomacy: 4, Intrigue: 3, Martial: -1} },
		"bad stat":   func(b *Build) { b.Points = Stats{"luck": 6} },
		"no crest":   func(b *Build) { b.Crest.Symbol = "" },
		"bad bg":     func(b *Build) { b.Background = "pirate" },
		"bad role":   func(b *Build) { b.Role = "king" },
		"no family":  func(b *Build) { b.Family = " " },
		"long motto": func(b *Build) { b.Crest.Motto = string(make([]byte, 61)) },
	}
	for name, f := range bad {
		b := goodBuild()
		f(&b)
		if b.Validate() == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestBuildFinal(t *testing.T) {
	st := goodBuild().Final()
	// base 2 + points 4 + noble 2 + cupbearer 1
	if st.Stats[Diplomacy] != 9 || st.Stats[Intrigue] != 2+2+1+1 || st.Stats[Martial] != 2 {
		t.Fatalf("stats: %v", st.Stats)
	}
	if st.Influence != 8 || st.Renown != 6 || st.Wealth != 20 || st.DailyIncome != 8 {
		t.Fatalf("standing: %+v", st)
	}
	if len(st.Items) != 2 || st.Items[0] != "crumbling_manor" {
		t.Fatalf("items: %v", st.Items)
	}
}

func TestCatalogueConsistency(t *testing.T) {
	if len(Backgrounds) != 8 {
		t.Errorf("%d backgrounds", len(Backgrounds))
	}
	for _, b := range Backgrounds {
		total := 0.0
		for _, v := range b.StatBonus {
			total += v
		}
		if total != 3 {
			t.Errorf("%s: stat bonus totals %g", b.Key, total)
		}
		if len(b.Summary) > 100 {
			t.Errorf("%s: summary too long for a menu", b.Key)
		}
		home, ok := ItemByKey(b.Home)
		if !ok || home.Kind != KindProperty {
			t.Errorf("%s: bad home %s", b.Key, b.Home)
		}
		for _, k := range b.Items {
			if it, ok := ItemByKey(k); !ok || it.Kind != KindPossession {
				t.Errorf("%s: bad item %s", b.Key, k)
			}
		}
	}
	for _, r := range Roles {
		total := 0.0
		for _, v := range r.StatBonus {
			total += v
		}
		if total != 2 {
			t.Errorf("%s: role bonus totals %g", r.Key, total)
		}
	}
	if len(CrestColors) > 25 || len(CrestSymbols) > 25 {
		t.Error("crest lists must fit a Discord select (25)")
	}
	if c := (Crest{Color: "red", Symbol: "lion"}); c.Emoji() != "🟥🦁" {
		t.Errorf("crest emoji %q", c.Emoji())
	}
}

func TestItemEffectsReachRules(t *testing.T) {
	if EstateValue([]string{"crumbling_manor", "signet_ring"}) != 16 {
		t.Fatal("estate value")
	}
	if e := SumEffects([]string{"relic", "tapestry"}); e.PartyBonus != 7 {
		t.Fatalf("party bonus %+v", e)
	}

	// A party bonus turns a roll of 40 at 35% into a success.
	host := Person{ID: 1, Sheet: Sheet{AttrInfluence: 15}} // party chance 35
	if PartyChance(host.Sheet, PartyMinSpend) != 35 {
		t.Fatalf("chance %d", PartyChance(host.Sheet, PartyMinSpend))
	}
	if ResolveParty(rolls(t, 40), PartyInput{Host: host}).Success {
		t.Fatal("should fail without the relic")
	}
	host.Effects = SumEffects([]string{"relic", "tapestry"})
	if !ResolveParty(rolls(t, 40), PartyInput{Host: host}).Success {
		t.Fatal("relic and tapestry should carry it")
	}

	// Informants turn a near-miss notice into a notice.
	target := Person{ID: 3, Sheet: Sheet{AttrIntrigue: 4}} // notice chance 30
	spreader := Attendee{Person: Person{ID: 2, Sheet: Sheet{AttrIntrigue: 10}}, Goal: GoalSpreadRumors, Target: &target}
	in := PartyInput{Host: Person{ID: 1}, Attendees: []Attendee{spreader}}
	if res := ResolveParty(rolls(t, 99, 0, 35, 35, 99, 99), in); len(res.Notices[3]) != 0 {
		t.Fatal("should not notice without informants")
	}
	target.Effects = SumEffects([]string{"informants"})
	if res := ResolveParty(rolls(t, 99, 0, 35, 35, 99, 99), in); len(res.Notices[3]) == 0 {
		t.Fatal("informants should help notice")
	}
}

func TestRoleHooks(t *testing.T) {
	saved := PartyEvents
	defer func() { PartyEvents = saved }()
	PartyEvents = append(PartyEvents, PartyEvent{Key: "officers_only", Roles: []string{"officer"}})
	has := func(role string) bool {
		for _, e := range PartyEventsFor(role) {
			if e.Key == "officers_only" {
				return true
			}
		}
		return false
	}
	if !has("officer") || has("merchant") {
		t.Fatal("role-gated party event")
	}
	if len(ActionsFor(CatDiplomacy, "merchant")) == 0 {
		t.Fatal("ungated actions are for everyone")
	}
}
