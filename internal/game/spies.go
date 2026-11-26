package game

import (
	"fmt"
	"math"
	"time"
)

// ---- spy networks ----
//
// Every player has a hidden spy level (0–100) in every other player's court.
// Players only ever see the tier word. Plant Spies raises it, Root Out Spies
// lowers other players' levels on you, and spies leak the target's private
// actions (see secrets.go).

const (
	KindPlantSpies = "spy"
	KindRootOut    = "rootout"

	SpyMin = 0.0
	SpyMax = 100.0

	// SpyDecay is how much of a spy level survives each Senate day.
	SpyDecay = 0.98
)

const (
	DefaultPlantDuration   = time.Hour
	FastPlantDuration      = 90 * time.Second
	DefaultRootOutDuration = 2 * time.Hour
	FastRootOutDuration    = 3 * time.Minute
)

// SpyTier is how deep a network runs, as shown to players.
type SpyTier struct {
	Min    float64 // lowest value in this tier
	Name   string
	Flavor string // %s is the target's name
}

// SpyTiers are in ascending order.
var SpyTiers = []SpyTier{
	{0, "None", "You have no spies in %s's court."},
	{0.5, "Low", "A stable hand or a kitchen maid in %s's house hears things, now and then."},
	{15, "Moderate", "A clerk who copies %s's letters is in your pay."},
	{35, "High", "A trusted servant of %s's household, one who keeps the keys, reports to you."},
	{60, "Very High", "%s's own steward answers to you."},
	{85, "Extremely High", "You have someone incredibly close to %s: a trusted advisor, a friend, perhaps a lover."},
}

// SpyTierOf returns the tier for a spy level.
func SpyTierOf(v float64) SpyTier {
	t := SpyTiers[0]
	for _, st := range SpyTiers {
		if v >= st.Min {
			t = st
		}
	}
	return t
}

// TierIndex is the tier's position in SpyTiers (0 = None).
func TierIndex(v float64) int {
	n := 0
	for i, st := range SpyTiers {
		if v >= st.Min {
			n = i
		}
	}
	return n
}

func ClampSpy(v float64) float64 { return math.Max(SpyMin, math.Min(SpyMax, v)) }

// SpyChange adds Delta to Owner's network in Target's court.
type SpyChange struct {
	Owner  int64
	Target int64
	Delta  float64
}

// ---- Plant Spies ----

// SpyPurse is how much gold a spymaster spends to grease the way in.
type SpyPurse struct {
	Key   string
	Name  string
	Gold  int
	Bonus float64 // percent added to the roll
}

var SpyPurses = []SpyPurse{
	{"none", "No gold", 0, 0},
	{"coin", "A little coin", 25, 8},
	{"bribes", "Bribes", 60, 15},
	{"fortune", "A fortune", 150, 22},
}

func PurseByKey(k string) (SpyPurse, bool) {
	for _, p := range SpyPurses {
		if p.Key == k {
			return p, true
		}
	}
	return SpyPurse{}, false
}

var (
	plantCheck   = Check{Base: 45, Weights: map[Attr]float64{AttrIntrigue: 5}}
	rootOutCheck = Check{Base: 40, Weights: map[Attr]float64{AttrIntrigue: 5}}
)

// opposedWeight is how much each point of the opponent's Intrigue counts against you.
const opposedWeight = 5.0

const (
	plantGreatMargin  = 30
	plantGreat        = 18.0
	plantGood         = 10.0
	plantDetectMargin = -25 // at or below: the target notices someone tried
	plantCaughtMargin = -45 // at or below: the spy is caught and names you
	plantDetectLoss   = 3.0
	plantCaughtShare  = 0.4 // share of the network lost when a spy is caught
	plantCaughtMin    = 5.0
	// CloseTier is the tier from which a caught spy is "someone very close to you".
	CloseTier = 4
)

// Exposure is how badly a spy attempt went wrong.
type Exposure int

const (
	Unseen   Exposure = iota
	Detected          // the target knows someone tried
	Caught            // the target knows who
)

type PlantInput struct {
	Spy    Person
	Target Person
	Level  float64 // the spy's current level on the target
	Purse  SpyPurse
}

type PlantResult struct {
	Delta    float64 // change to the spy level
	Success  bool
	Exposure Exposure
	Close    bool  // the caught spy was close to the target
	Practice Delta // stat gain for the spymaster
	Text     string
}

// PlantMod is the situational modifier for planting spies.
func PlantMod(in PlantInput) float64 {
	return -opposedWeight*in.Target.Sheet[AttrIntrigue] - in.Level/4 + in.Purse.Bonus - in.Target.Effects.RumorNotice
}

// ResolvePlant decides how an attempt to plant spies went.
func ResolvePlant(r Roller, in PlantInput) PlantResult {
	ok, margin := plantCheck.Roll(r, in.Spy.Sheet, PlantMod(in), Normal)
	scale := 1 - in.Level/150
	name := in.Target.Name
	res := PlantResult{Success: ok, Practice: Practice(Intrigue, ok)}
	switch {
	case ok && margin >= plantGreatMargin:
		res.Delta = plantGreat * scale
		res.Text = fmt.Sprintf("Your agent has done better than you hoped, and has the ear of someone who matters in %s's house.", name)
	case ok:
		res.Delta = plantGood * scale
		res.Text = fmt.Sprintf("Your agent has found a place in %s's household.", name)
	case margin > plantDetectMargin:
		res.Text = fmt.Sprintf("Your agent found no way into %s's house. Nobody noticed the attempt.", name)
	case margin > plantCaughtMargin:
		res.Delta = -math.Min(plantDetectLoss, in.Level)
		res.Exposure = Detected
		res.Text = fmt.Sprintf("Your agent was turned away at %s's door, and they seemed suspicious. %s may know someone is prying.", name, name)
	default:
		res.Delta = -math.Min(in.Level, math.Max(plantCaughtMin, in.Level*plantCaughtShare))
		res.Exposure = Caught
		res.Close = TierIndex(in.Level) >= CloseTier
		res.Text = fmt.Sprintf("Your agent was caught in %s's house, and gave up your name. Your network there is shaken.", name)
	}
	return res
}

// ---- Root Out Spies ----

const (
	rootGreatMargin = 30
	rootGood        = 15.0
	rootGreat       = 30.0
)

// Network is one spymaster's network on the player rooting out spies.
type Network struct {
	Spy   Person
	Level float64
}

type RootInput struct {
	Player   Person
	Networks []Network
}

// RootHit is a network the player damaged.
type RootHit struct {
	Spy   Person
	Delta float64 // negative
	Named bool    // the player learned who ran it
}

type RootResult struct {
	Hits     []RootHit
	Practice Delta
}

// RootMod is the situational modifier for rooting out one spymaster's network.
func RootMod(player Person, n Network) float64 {
	return -opposedWeight*n.Spy.Sheet[AttrIntrigue] + player.Effects.RumorNotice + n.Level/5
}

// ResolveRootOut rolls against each network on the player.
func ResolveRootOut(r Roller, in RootInput) RootResult {
	res := RootResult{Practice: Practice(Intrigue, true)}
	for _, n := range in.Networks {
		if n.Level <= 0 {
			continue
		}
		ok, margin := rootOutCheck.Roll(r, in.Player.Sheet, RootMod(in.Player, n), Normal)
		if !ok {
			continue
		}
		h := RootHit{Spy: n.Spy, Delta: -math.Min(n.Level, rootGood)}
		if margin >= rootGreatMargin {
			h.Delta, h.Named = -math.Min(n.Level, rootGreat), true
		}
		res.Hits = append(res.Hits, h)
	}
	return res
}

// ---- practice ----

const (
	practiceSuccess = 0.15
	practiceFailure = 0.05
)

// Practice is the small stat gain from doing something that uses a stat.
func Practice(s Stat, success bool) Delta {
	g := practiceFailure
	if success {
		g = practiceSuccess
	}
	return Delta{Stats: Stats{s: g}}
}
