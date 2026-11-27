package game

import (
	"math"
	"strings"
)

// Attr is anything a Check can weigh: the five stats plus a character's
// standing (influence, renown, estate, wealth).
type Attr string

const (
	AttrDiplomacy   = Attr(Diplomacy)
	AttrMartial     = Attr(Martial)
	AttrStewardship = Attr(Stewardship)
	AttrIntrigue    = Attr(Intrigue)
	AttrProwess     = Attr(Prowess)

	// Influence is your pull in the senate/council: the share of votes you can sway.
	AttrInfluence Attr = "influence"
	// Renown is your family's fame and recognition.
	AttrRenown Attr = "renown"
	// Estate is how fine your house and holdings are.
	AttrEstate Attr = "estate"
	// Wealth is gold on hand.
	AttrWealth Attr = "wealth"
)

// Sheet is a flat view of a character for checks.
type Sheet map[Attr]float64

// NewSheet builds a Sheet from a character's parts. estate is the estate
// value; checks see its EstateScore.
func NewSheet(stats Stats, influence, renown, estate float64, wealth int) Sheet {
	s := Sheet{
		AttrInfluence: influence,
		AttrRenown:    renown / RenownScale,
		AttrEstate:    EstateScore(estate),
		AttrWealth:    float64(wealth),
	}
	for st, v := range stats {
		s[Attr(st)] = v / StatScale
	}
	return s
}

// Delta is a change to a character, applied when an activity resolves.
type Delta struct {
	Stats       Stats   `json:"stats,omitempty"`
	Influence   float64 `json:"influence,omitempty"`
	Renown      float64 `json:"renown,omitempty"`
	Wealth      int     `json:"wealth,omitempty"`
	DailyIncome int     `json:"daily_income,omitempty"`
}

// Merge adds o into d.
func (d *Delta) Merge(o Delta) {
	if len(o.Stats) > 0 {
		if d.Stats == nil {
			d.Stats = Stats{}
		}
		d.Stats.Add(o.Stats)
	}
	d.Influence += o.Influence
	d.Renown += o.Renown
	d.Wealth += o.Wealth
	d.DailyIncome += o.DailyIncome
}

// Scale multiplies every part of the delta. Whole-number parts are rounded.
func (d Delta) Scale(f float64) Delta {
	out := Delta{
		Influence:   d.Influence * f,
		Renown:      d.Renown * f,
		Wealth:      int(math.Round(float64(d.Wealth) * f)),
		DailyIncome: int(math.Round(float64(d.DailyIncome) * f)),
	}
	if len(d.Stats) > 0 {
		out.Stats = Stats{}
		for k, v := range d.Stats {
			out.Stats[k] = v * f
		}
	}
	return out
}

// Describe renders the delta as "+2 influence · +1.5 diplomacy".
func (d Delta) Describe() string {
	var parts []string
	add := func(v float64, name string) {
		if v != 0 {
			s := Num(v)
			if v > 0 {
				s = "+" + s
			}
			parts = append(parts, s+" "+name)
		}
	}
	add(d.Influence, "influence")
	add(RoundRenown(d.Renown), "renown")
	for _, st := range AllStats {
		add(d.Stats[st], string(st))
	}
	add(float64(d.Wealth), "gold")
	add(float64(d.DailyIncome), "gold/day")
	return strings.Join(parts, " · ")
}
