package game

import (
	"math/rand/v2"
	"testing"
)

func plotter(id int64, intrigue, other float64, s Stat) Plotter {
	return Plotter{Person: Person{ID: id, Name: "p", Sheet: Sheet{AttrIntrigue: intrigue, Attr(s): other}}}
}

func TestSchemesAreWellFormed(t *testing.T) {
	for _, s := range SchemeDefs {
		if len(s.Targets) == 0 || s.Name == "" || s.Emoji == "" || s.Desc == "" || s.Verb() == "ruin" {
			t.Errorf("scheme %q is incomplete", s.Key)
		}
		m, ok := MisdeedDefs[s.Crime]
		if !ok || !m.AlwaysCrime {
			t.Errorf("scheme %q: its deed %q must be a misdeed and always a crime", s.Key, s.Crime)
		}
		if got, ok := SchemeByKey(s.Key); !ok || got.Key != s.Key {
			t.Errorf("SchemeByKey(%q) = %v", s.Key, ok)
		}
	}
}

func TestMorePlottersMoreRisk(t *testing.T) {
	sc, _ := SchemeByKey(SchemeAssassinate)
	target := Person{Sheet: Sheet{AttrIntrigue: 2, AttrProwess: 2}}
	d := Defender{Player: &target}
	alone := Plot{Scheme: sc, Leader: plotter(1, 3, 2, Prowess)}
	crowd := alone
	crowd.Members = []Plotter{plotter(2, 3, 2, Prowess), plotter(3, 3, 2, Prowess)}
	crowd.Senators = []Senator{{ID: 9, Influence: 12}}
	if crowd.Power() <= alone.Power() || crowd.DailyProgress() <= alone.DailyProgress() {
		t.Errorf("members should add power: %v vs %v", crowd.Power(), alone.Power())
	}
	if crowd.DiscoverChance(d) <= alone.DiscoverChance(d) {
		t.Errorf("more mouths should leak more: %d vs %d", crowd.DiscoverChance(d), alone.DiscoverChance(d))
	}
	if crowd.StrikeChance(d) <= alone.StrikeChance(d) {
		t.Errorf("more power should strike better: %d vs %d", crowd.StrikeChance(d), alone.StrikeChance(d))
	}
	ripe := crowd
	ripe.Progress = ProgressMax
	if ripe.StrikeChance(d) <= crowd.StrikeChance(d) {
		t.Errorf("a ripe plot should strike better: %d vs %d", ripe.StrikeChance(d), crowd.StrikeChance(d))
	}
	warned := ripe
	warned.Warned = Suspected
	if warned.StrikeChance(d) >= ripe.StrikeChance(d) {
		t.Errorf("a target on guard should be harder to reach")
	}
	purse, _ := ConspiracyPurseByKey("best")
	paid := alone
	paid.Purse = purse
	if paid.Power() <= alone.Power() || paid.DiscoverChance(d) != alone.DiscoverChance(d) {
		t.Error("hired hands add power and don't talk")
	}
}

func TestStepDayOnlyEverLearnsMore(t *testing.T) {
	sc, _ := SchemeByKey(SchemeFrame)
	target := Person{Sheet: Sheet{AttrIntrigue: 5}}
	d := Defender{Player: &target}
	p := Plot{Scheme: sc, Leader: plotter(1, 1, 1, Diplomacy), Members: []Plotter{plotter(2, 1, 1, Diplomacy)}}

	// 0: found; 10: a low detail roll, which still teaches them something new.
	p.Warned = Suspected
	res := StepDay(rolls(t, 0, 10, 0), p, d)
	if res.Found != MemberNamed || res.Named != 0 {
		t.Errorf("after suspecting, a find names someone: %+v", res)
	}
	if res.Progress <= p.Progress {
		t.Error("the plot ripens each day")
	}
	p.Warned = MemberNamed
	if res := StepDay(rolls(t, 0, 10), p, d); res.Found != LeaderNamed {
		t.Errorf("after a member, the leader: %+v", res)
	}
	p.Warned = Unsuspected
	if res := StepDay(rolls(t, 99), p, d); res.Found != Unsuspected {
		t.Errorf("a quiet day: %+v", res)
	}
	if res := StepDay(rolls(t, 0, 95), p, d); res.Found != LeaderNamed {
		t.Errorf("a great find goes straight to the leader: %+v", res)
	}
	if res := Uncover(rolls(t, 0, 10), p, d); res.Found != Suspected || res.Progress != p.Progress {
		t.Errorf("rooting out finds without ripening: %+v", res)
	}
}

func TestSenatorsJoinByCharacter(t *testing.T) {
	sc, _ := SchemeByKey(SchemeAssassinate)
	schemer := Senator{ID: 1, Traits: []Trait{TraitScheming}}
	honest := Senator{ID: 2, Traits: []Trait{TraitIncorruptible}}
	pious := Senator{ID: 3, Traits: []Trait{TraitPious}}
	if JoinChance(schemer, sc, 0, 0, false) <= JoinChance(honest, sc, 0, 0, false) {
		t.Error("schemers join more readily than the incorruptible")
	}
	if JoinChance(schemer, sc, 50, 0, false) <= JoinChance(schemer, sc, 0, 0, false) {
		t.Error("friends join more readily")
	}
	if JoinChance(schemer, sc, 0, 50, false) >= JoinChance(schemer, sc, 0, 0, false) {
		t.Error("nobody likes plotting against a friend")
	}
	if JoinChance(schemer, sc, 0, 0, true) <= JoinChance(schemer, sc, 0, 0, false) {
		t.Error("a gift helps")
	}
	frame, _ := SchemeByKey(SchemeFrame)
	if JoinChance(pious, sc, 0, 0, false) >= JoinChance(pious, frame, 0, 0, false) {
		t.Error("the pious balk at murder")
	}
	// A refusal (99) then a report roll (0): the incorruptible talk.
	res := ResolveJoin(rolls(t, 99, 0), honest, sc, 7, 0, 0, false)
	if res.Joined || !res.Reported || len(res.Opinions) != 1 || res.Opinions[0].CharacterID != 7 {
		t.Errorf("refusal with a report: %+v", res)
	}
	if res := ResolveJoin(rolls(t, 0), schemer, sc, 7, 0, 0, false); !res.Joined {
		t.Errorf("a low roll joins: %+v", res)
	}
}

func TestResolveStrike(t *testing.T) {
	sc, _ := SchemeByKey(SchemeAssassinate)
	target := Person{ID: 5, Sheet: Sheet{}}
	d := Defender{Player: &target}
	p := Plot{Scheme: sc, Leader: plotter(1, 4, 4, Prowess), Members: []Plotter{plotter(2, 2, 2, Prowess)}, Progress: ProgressMax}
	sen := []Senator{{ID: 1, Traits: []Trait{TraitStern}}, {ID: 2}}
	if res := ResolveStrike(rolls(t, 0), p, d, sen); !res.Success || res.Exposed || res.Deltas[2].Stats[Intrigue] == 0 {
		t.Errorf("a low roll succeeds, and everyone practises: %+v", res)
	}
	res := ResolveStrike(rolls(t, 99), p, d, sen)
	if res.Success || !res.Exposed || res.Public == "" || res.Deltas[1].Renown >= 0 || len(res.Opinions) != 2 {
		t.Errorf("a terrible roll exposes the leader: %+v", res)
	}
	if res.Opinions[0].Delta >= res.Opinions[1].Delta {
		t.Error("the Stern take murder worse")
	}
}

func TestInheritKeepsTheHouseGoing(t *testing.T) {
	start := float64(BaseStat * StatScale)
	h := Inherit("Livia", Stats{Diplomacy: start + 20, Intrigue: start}, 10, 4, 500)
	if h.Stats[Diplomacy] != start+10 || h.Stats[Intrigue] != start || h.Stats[Martial] != 0 {
		t.Errorf("stats: %v", h.Stats)
	}
	if h.Renown != 5 || h.Influence != 2 || h.Wealth != 400 || h.Name != "Livia" {
		t.Errorf("heir: %+v", h)
	}
	r := rand.New(rand.NewPCG(1, 2))
	for range 50 {
		if PickHeirName(r, "Livia") == "Livia" {
			t.Fatal("an heir shouldn't share the dead's name")
		}
	}
}

func TestReplaceSenatorPicksANewFace(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	seated := NewSenate(r, SenateStandard)
	for i := range seated {
		seated[i].ID = int64(i + 1)
	}
	for range 100 {
		n := ReplaceSenator(r, seated, 3)
		if n.ID != 3 || n.Name == "" {
			t.Fatalf("new senator: %+v", n)
		}
		for _, s := range seated {
			if s.Name == n.Name {
				t.Fatalf("%s is already seated", n.Name)
			}
		}
	}
}

func TestCofferTake(t *testing.T) {
	if CofferTake(100) != 30 || CofferTake(2000) != StealCofferMax+100 || CofferTake(100000) != StealCofferMax+9900 || CofferTake(0) != 0 {
		t.Error("a theft takes a share of the gold, up to a cap")
	}
}
