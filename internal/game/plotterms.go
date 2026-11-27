package game

import "math"

// ---- what a conspiracy's leader promises its members ----
//
// A leader inviting a player offers them terms: gold paid the moment they
// join, and a share of the haul if the plot works. Members of a plot that
// brings down a senator also share in the influence, and on any success the
// senators who disliked the target think better of everyone in it. The bot
// enforces all of it. Placeholder numbers — tune freely.

// PlotTerms is what a leader offered one player.
type PlotTerms struct {
	Gold  int // paid by the leader when they join
	Share int // percent of the haul, paid on success
}

// The offers the invitation panel shows.
var (
	PlotGoldOffers  = []int{0, 25, 50, 100, 250}
	PlotShareOffers = []int{0, 10, 25, 50}
)

// Describe is the offer in words, or "" for nothing.
func (t PlotTerms) Describe() string {
	switch {
	case t.Gold > 0 && t.Share > 0:
		return Num(float64(t.Gold)) + " gold now, and " + Num(float64(t.Share)) + "% of the haul"
	case t.Gold > 0:
		return Num(float64(t.Gold)) + " gold now"
	case t.Share > 0:
		return Num(float64(t.Share)) + "% of the haul"
	}
	return ""
}

// SplitHaul is each member's cut of a haul worth value gold. If the leader
// promised more than the whole haul, every cut shrinks to fit.
func SplitHaul(value int, shares map[int64]int) map[int64]int {
	out := map[int64]int{}
	total := 0
	for _, s := range shares {
		total += max(s, 0)
	}
	if value <= 0 || total == 0 {
		return out
	}
	scale := math.Min(1, 100/float64(total))
	for id, s := range shares {
		if s > 0 {
			out[id] = int(float64(value) * float64(s) / 100 * scale)
		}
	}
	return out
}

// disgraceMemberShare is how much influence the members of a plot that
// disgraced a senator share between them, against the leader's own gain.
const disgraceMemberShare = 0.5

// DisgraceMemberReward is one member's influence from a senator's fall.
func DisgraceMemberReward(members int, memberInfluence float64) Delta {
	if members <= 0 {
		return Delta{}
	}
	return Delta{Influence: ScaleInfluence(disgraceInfluence*disgraceMemberShare/float64(members), memberInfluence)}
}

// Senators who disliked a plot's victim are glad of whoever brought them low.
const (
	enemyOfTarget   = -20.0 // their opinion of the target must be below this
	enemyLeaderGain = 5.0
	enemyMemberGain = 3.0
)

// EnemiesPleased is how the victim's enemies in the Senate take a plot that
// worked. targetOps is every senator's opinion of the victim.
func EnemiesPleased(senators []Senator, targetOps Opinions, leaderID int64, members []int64) []OpinionChange {
	var out []OpinionChange
	for _, s := range senators {
		if targetOps[s.ID] >= enemyOfTarget {
			continue
		}
		out = append(out, OpinionChange{SenatorID: s.ID, CharacterID: leaderID, Delta: enemyLeaderGain})
		for _, m := range members {
			out = append(out, OpinionChange{SenatorID: s.ID, CharacterID: m, Delta: enemyMemberGain})
		}
	}
	return out
}
