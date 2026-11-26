package game

import "strings"

// Realm trackers. They are Attrs so realm rolls can use Check like
// everything else.
const (
	AttrTreasury   Attr = "treasury"
	AttrMilitary   Attr = "military"
	AttrStability  Attr = "stability"       // law and order; bandits thrive when it's low
	AttrForeign    Attr = "foreign_opinion" // how the neighbors see us
	AttrPopularity Attr = "popularity"      // the common people's mood
)

type TrackerInfo struct {
	Key   Attr
	Name  string
	Emoji string
	// Fall is why the realm is lost when this tracker hits 0. Empty means
	// hitting 0 doesn't end the game.
	Fall string
}

var Trackers = []TrackerInfo{
	{AttrTreasury, "Treasury", "💰", "The treasury is empty. The realm is bankrupt."},
	{AttrMilitary, "Military", "⚔️", "With no army left to stop them, invaders overrun the realm."},
	{AttrStability, "Stability", "⚖️", "Order collapses. Bandits and rebels overrun the realm."},
	{AttrForeign, "Foreign opinion", "🌍", ""},
	{AttrPopularity, "Popularity", "👥", "The people rise up and burn the Senate house."},
}

const (
	TrackerMin = 0
	TrackerMax = 100
)

// Realm holds the trackers, 0–100.
type Realm map[Attr]float64

func StartingRealm() Realm {
	return Realm{AttrTreasury: 60, AttrMilitary: 50, AttrStability: 60, AttrForeign: 50, AttrPopularity: 55}
}

func (r Realm) Clone() Realm {
	out := make(Realm, len(r))
	for k, v := range r {
		out[k] = v
	}
	return out
}

// Apply adds a change, clamped to the tracker range.
func (r Realm) Apply(d Realm) {
	for k, v := range d {
		n := r[k] + v
		if n < TrackerMin {
			n = TrackerMin
		}
		if n > TrackerMax {
			n = TrackerMax
		}
		r[k] = n
	}
}

func (r Realm) Sheet() Sheet { return Sheet(r) }

// DriftRate is how far each tracker moves back toward DriftTarget per day:
// taxes trickle in, order slowly returns, people forget.
const (
	DriftRate   = 0.04
	DriftTarget = 50
	// DailyTaxes is the treasury's steady income.
	DailyTaxes = 2
)

// Drift returns one day's natural recovery (or decline, above the target),
// plus taxes.
func (r Realm) Drift() Realm {
	d := Realm{AttrTreasury: DailyTaxes}
	for _, t := range Trackers {
		if v := (DriftTarget - r[t.Key]) * DriftRate; v != 0 {
			d[t.Key] += v
		}
	}
	return d
}

// Fallen reports whether the realm is lost, and why.
func (r Realm) Fallen() (bool, string) {
	for _, t := range Trackers {
		if t.Fall != "" && r[t.Key] <= TrackerMin {
			return true, t.Fall
		}
	}
	return false, ""
}

func trackerName(a Attr) string {
	for _, t := range Trackers {
		if t.Key == a {
			return t.Name
		}
	}
	return string(a)
}

// Describe renders a change like "Treasury -12 · Stability +4".
func (r Realm) Describe() string {
	var parts []string
	for _, t := range Trackers {
		if v := r[t.Key]; v != 0 {
			s := Num(v)
			if v > 0 {
				s = "+" + s
			}
			parts = append(parts, t.Emoji+" "+t.Name+" "+s)
		}
	}
	return strings.Join(parts, " · ")
}
