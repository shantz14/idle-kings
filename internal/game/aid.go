package game

import (
	"fmt"
	"math"
)

// ---- Aid the Realm (Monetary) ----
//
// Players spend their own gold to shore up a tracker. It's instant and
// doesn't take the character's action. Gifts are worth more, in renown and
// influence, when the realm needs them.

// KindAid is the menu key for Aid the Realm (instant, not an activity).
const KindAid = "aid"

// AidTarget is a tracker players can fund.
type AidTarget struct {
	Key     string
	Tracker Attr
	Name    string // the action, e.g. "Donate to the Treasury"
	Verb    string // for announcements: "has given %d gold to the treasury"
	Fan     Trait  // senators with this trait warm to the giver
}

var AidTargets = []AidTarget{
	{"treasury", AttrTreasury, "Donate to the Treasury", "has given %d gold to the treasury", TraitEconomical},
	{"military", AttrMilitary, "Pay for Troops", "has paid %d gold to raise troops for the Republic", TraitHawk},
	{"stability", AttrStability, "Fund the City Watch", "has paid %d gold for more men in the city watch", TraitConservative},
}

func AidTargetByKey(k string) (AidTarget, bool) {
	for _, a := range AidTargets {
		if a.Key == k {
			return a, true
		}
	}
	return AidTarget{}, false
}

var AidAmounts = []int{25, 50, 100, 250}

const (
	AidMin          = 10
	AidMax          = 5000
	aidRate         = 25.0   // gold per tracker point, before diminishing returns
	aidTaper        = 1000.0 // big gifts buy less per coin
	aidRenown       = 0.2    // per point
	aidInfluence    = 0.03   // per point
	AidCrisis       = 25.0   // below this, rewards triple, and influence comes on top
	aidCrisisMult   = 3.0
	aidCrisisPer    = 100.0 // gold per extra point of influence in a crisis
	aidOpinionPer   = 5.0   // points per +1 opinion with the senators who care
	AidPublicAmount = 50    // gifts this big are announced
)

// AidPoints is how much a gift raises the tracker.
func AidPoints(gold int) float64 {
	g := float64(gold)
	return g / aidRate / (1 + g/aidTaper)
}

type AidResult struct {
	Points   float64
	Delta    Delta
	Opinions []OpinionChange
	Crisis   bool
	Text     string
	Public   string // empty for small gifts
}

// ResolveAid works out a gift. current is the tracker's value before it.
func ResolveAid(p Person, t AidTarget, gold int, current float64, senators []Senator) AidResult {
	pts := math.Min(AidPoints(gold), TrackerMax-current)
	pts = math.Max(pts, 0)
	res := AidResult{Points: pts, Crisis: current < AidCrisis}
	mult, extra := 1.0, 0.0
	if res.Crisis { // a saviour is remembered
		mult, extra = aidCrisisMult, float64(gold)/aidCrisisPer
	}
	res.Delta = Delta{
		Renown:    pts * aidRenown * mult,
		Influence: ScaleInfluence(pts*aidInfluence*mult+extra, p.Sheet[AttrInfluence]),
	}
	if op := math.Floor(pts / aidOpinionPer); op > 0 {
		res.Opinions = opinionsFrom(senators, p.ID, op, t.Fan)
	}
	res.Text = fmt.Sprintf("You gave **%d gold**. %s +%s.", gold, trackerName(t.Tracker), Num(pts))
	if res.Crisis {
		res.Text += " The realm needed it, and the court noticed."
	}
	if gold >= AidPublicAmount {
		res.Public = fmt.Sprintf("**%s** "+t.Verb+".", p.Name, gold)
	}
	return res
}
