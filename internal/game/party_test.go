package game

import (
	"strings"
	"testing"
)

// seq is a Roller that returns fixed rolls in order.
type seq struct {
	t    *testing.T
	vals []int
}

func (s *seq) IntN(int) int {
	if len(s.vals) == 0 {
		s.t.Fatal("unexpected extra roll")
	}
	v := s.vals[0]
	s.vals = s.vals[1:]
	return v
}

func rolls(t *testing.T, v ...int) *seq { return &seq{t: t, vals: v} }

func person(id int64, s Sheet) Person {
	return Person{ID: id, UserID: "u" + string(rune('0'+id)), Name: "P" + string(rune('0'+id)), Sheet: s}
}

func TestPartyWeightOrder(t *testing.T) {
	base := Sheet{AttrInfluence: 5, AttrEstate: 5, AttrDiplomacy: 5, AttrRenown: 5}
	gain := func(a Attr) int {
		s := Sheet{}
		for k, v := range base {
			s[k] = v
		}
		s[a] += 5
		return PartyChance(s, PartyMinSpend) - PartyChance(base, PartyMinSpend)
	}
	order := []Attr{AttrInfluence, AttrEstate, AttrDiplomacy, AttrRenown}
	for i := 1; i < len(order); i++ {
		if gain(order[i-1]) <= gain(order[i]) {
			t.Fatalf("%s should matter more than %s", order[i-1], order[i])
		}
	}
}

func TestPartyHostSuccessGoals(t *testing.T) {
	host := person(1, Sheet{AttrInfluence: 10, AttrStewardship: 5})
	res := ResolveParty(rolls(t, 0), PartyInput{Host: host, Goal: GoalMakeFriends})
	if !res.Success || res.Deltas[1].Influence != hostSuccess.Influence+hostFriends.Influence {
		t.Fatalf("make friends: %+v", res)
	}
	res = ResolveParty(rolls(t, 0, 0), PartyInput{Host: host, Goal: GoalTalkBusiness})
	if res.Deltas[1].DailyIncome != hostBusiness.DailyIncome {
		t.Fatalf("talk business: %+v", res.Deltas[1])
	}
	res = ResolveParty(rolls(t, 99), PartyInput{Host: host, Goal: GoalMakeFriends})
	if res.Success || res.Deltas[1].Influence != 0 {
		t.Fatalf("failure: %+v", res)
	}
}

func TestGuestBoostedByParty(t *testing.T) {
	host := person(1, Sheet{})
	guest := Attendee{Person: person(2, Sheet{AttrDiplomacy: 4, AttrInfluence: 5}), Goal: GoalMakeFriends} // 45%
	// Roll 50: succeeds at a good party (60%), fails at a bad one (35%).
	good := ResolveParty(rolls(t, 0, 50), PartyInput{Host: host, Goal: GoalMakeFriends, Quality: 100, Attendees: []Attendee{guest}})
	bad := ResolveParty(rolls(t, 99, 50), PartyInput{Host: host, Goal: GoalMakeFriends, Attendees: []Attendee{guest}})
	if good.Deltas[2].Influence != guestFriends.Influence || bad.Deltas[2].Influence != 0 {
		t.Fatalf("good %+v bad %+v", good.Deltas[2], bad.Deltas[2])
	}
}

func TestRumorTiers(t *testing.T) {
	host := person(1, Sheet{AttrIntrigue: 14})   // host notice chance 60
	target := person(3, Sheet{AttrIntrigue: 10}) // target notice chance 60
	spreader := Attendee{Person: person(2, Sheet{AttrIntrigue: 10}), Goal: GoalSpreadRumors, Target: &target}
	run := func(r ...int) PartyResult {
		// party roll fails (99), rumor lands (0), then target x2, host x2
		return ResolveParty(rolls(t, append([]int{99, 0}, r...)...), PartyInput{Host: host, Attendees: []Attendee{spreader}})
	}

	unnoticed := run(99, 99, 99, 99)
	if unnoticed.Deltas[3].Influence != -rumorDamage {
		t.Fatalf("rumor should land: %+v", unnoticed.Deltas[3])
	}
	if len(unnoticed.Notices[3]) != 0 {
		t.Fatalf("unnoticed target must not be told: %v", unnoticed.Notices[3])
	}

	vague := run(50, 10, 99, 99) // disadvantage keeps 50 -> margin 10
	if n := strings.Join(vague.Notices[3], ""); !strings.Contains(n, "someone") || strings.Contains(n, "P2") {
		t.Fatalf("small success should be anonymous: %q", n)
	}

	named := run(30, 30, 99, 99) // margin 30
	if n := strings.Join(named.Notices[3], ""); !strings.Contains(n, "P2") || named.Deltas[3].Influence != -rumorDamage {
		t.Fatalf("large success should name, rumor still lands: %q %+v", n, named.Deltas[3])
	}

	cancelled := run(5, 5, 99, 99) // margin 55
	if cancelled.Deltas[3].Influence != 0 || !strings.Contains(strings.Join(cancelled.Notices[3], ""), "P2") {
		t.Fatalf("very large success should name and cancel: %+v", cancelled)
	}

	hostCancel := run(99, 99, 5, 5)
	if hostCancel.Deltas[3].Influence != 0 || len(hostCancel.Notices[3]) != 0 {
		t.Fatalf("host cancel: %+v", hostCancel)
	}
	if !strings.Contains(strings.Join(hostCancel.Notices[1], ""), "stop") {
		t.Fatalf("host should be told: %v", hostCancel.Notices[1])
	}
}

func TestEventDefaultOnUnknownChoice(t *testing.T) {
	ev, _ := PartyEventByKey("wine")
	c, out := ResolveEventChoice(rolls(t), ev, "", Sheet{})
	if c.Key != ev.Default || out.Quality >= 0 {
		t.Fatalf("default: %s %+v", c.Key, out)
	}
}

func TestPartySpend(t *testing.T) {
	host := person(1, Sheet{AttrInfluence: 10})
	if PartySpendBonus(PartyMinSpend) != 0 || PartyRewardScale(PartyMinSpend) != 1 {
		t.Fatal("the minimum spend should be the baseline")
	}
	if PartySpendBonus(PartyMaxSpend*10) != PartySpendBonus(PartyMaxSpend) {
		t.Fatal("spend should cap")
	}
	prev := PartyChance(host.Sheet, PartyMinSpend)
	for _, b := range PartyBudgets[1:] {
		if c := PartyChance(host.Sheet, b.Gold); c <= prev {
			t.Fatalf("%s (%d gold) should beat %d%%, got %d%%", b.Name, b.Gold, prev, c)
		} else {
			prev = c
		}
	}
	if PartyBudgetName(100) != "Fine" || PartyBudgetName(10) != "Modest" {
		t.Fatal("budget names")
	}

	// Roll 30: fails at the minimum (25%), succeeds at 120 gold (+16%).
	if ResolveParty(rolls(t, 30), PartyInput{Host: host, Goal: GoalMakeFriends, Spend: PartyMinSpend}).Success {
		t.Fatal("cheap party should fail")
	}
	res := ResolveParty(rolls(t, 30), PartyInput{Host: host, Goal: GoalMakeFriends, Spend: 120})
	want := (hostSuccess.Influence + hostFriends.Influence) * 2 // two doublings
	if !res.Success || res.Deltas[1].Influence != want {
		t.Fatalf("lavish party: %+v, want influence %v", res.Deltas[1], want)
	}

	// Failure costs the same however much was spent.
	bad := ResolveParty(rolls(t, 99), PartyInput{Host: host, Goal: GoalMakeFriends, Spend: 300})
	if bad.Deltas[1].Renown != hostFailure.Renown {
		t.Fatalf("failure scaled: %+v", bad.Deltas[1])
	}
}
