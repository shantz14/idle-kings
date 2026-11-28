package game

import "testing"

func TestSplitHaul(t *testing.T) {
	got := SplitHaul(400, map[int64]int{1: 25, 2: 10})
	if got[1] != 100 || got[2] != 40 {
		t.Fatalf("cuts %v", got)
	}
	// Promising more than the haul shrinks every cut to fit.
	got = SplitHaul(300, map[int64]int{1: 50, 2: 50, 3: 50})
	if sum := got[1] + got[2] + got[3]; sum > 300 || got[1] != 100 {
		t.Fatalf("over-promised cuts %v", got)
	}
	if len(SplitHaul(0, map[int64]int{1: 50})) != 0 || len(SplitHaul(100, nil)) != 0 {
		t.Fatal("nothing to split")
	}
}

func TestPlotRewards(t *testing.T) {
	if d := DisgraceMemberReward(2, 0); !near(d.Influence, disgraceInfluence*disgraceMemberShare/2) {
		t.Fatalf("member influence %v", d.Influence)
	}
	if DisgraceMemberReward(0, 0).Influence != 0 {
		t.Fatal("no members, nothing")
	}
	sen := []Senator{{ID: 1}, {ID: 2}}
	ch := EnemiesPleased(sen, Opinions{1: -40, 2: 10}, 7, []int64{8, 9})
	if len(ch) != 3 || ch[0].SenatorID != 1 || ch[0].CharacterID != 7 || ch[0].Delta != enemyLeaderGain || ch[1].Delta != enemyMemberGain {
		t.Fatalf("enemies pleased %+v", ch)
	}
	if (PlotTerms{Gold: 50, Share: 25}).Describe() == "" || (PlotTerms{}).Describe() != "" {
		t.Fatal("describe")
	}
}
