package game

import (
	"fmt"
	"time"
)

// ---- Embezzle from the Treasury (Intrigue) ----
//
// A player turns the realm's treasury into their own gold. Senators can't
// stop it; only greed has to be kept in check, by the risk of a scandal.

const KindEmbezzle = "embezzle"

const (
	DefaultEmbezzleDuration = 2 * time.Hour
	FastEmbezzleDuration    = 2 * time.Minute
)

// GoldPerTreasury is how much gold one point of the treasury tracker is worth
// to a player, both when stealing it and when giving it (see aid.go).
const GoldPerTreasury = 20

// EmbezzleSize is how much a player tries to take.
type EmbezzleSize struct {
	Key     string
	Name    string
	Points  float64 // treasury points taken
	Penalty float64 // percent off the roll
}

var EmbezzleSizes = []EmbezzleSize{
	{"skim", "Skim a little", 3, 0},
	{"dip", "Dip into it", 6, 10},
	{"plunder", "Plunder it", 12, 25},
}

func EmbezzleSizeByKey(k string) (EmbezzleSize, bool) {
	for _, e := range EmbezzleSizes {
		if e.Key == k {
			return e, true
		}
	}
	return EmbezzleSize{}, false
}

// EmbezzleGold is what a size pays out.
func (e EmbezzleSize) Gold() int { return int(e.Points * GoldPerTreasury) }

var embezzleCheck = Check{Base: 50, Weights: map[Attr]float64{AttrStewardship: 3, AttrIntrigue: 3}}

const (
	embezzleCaughtMargin  = -25
	embezzleCaughtRenown  = 20
	embezzleCaughtOpinion = -8.0
	embezzleOutraged      = -15.0 // Incorruptible and Economical senators
)

type EmbezzleResult struct {
	Success  bool
	Caught   bool
	Treasury float64 // change to the treasury tracker
	Delta    Delta
	Opinions []OpinionChange
	Text     string
	Public   string // posted when caught
}

// ResolveEmbezzle decides how a theft went. The treasury can't give what it
// doesn't have.
func ResolveEmbezzle(r Roller, p Person, size EmbezzleSize, treasury float64, senators []Senator) EmbezzleResult {
	ok, margin := embezzleCheck.Roll(r, p.Sheet, -size.Penalty, Normal)
	res := EmbezzleResult{Success: ok}
	switch {
	case ok:
		pts := min(size.Points, max(treasury, 0))
		res.Treasury = -pts
		res.Delta = Delta{Wealth: int(pts * GoldPerTreasury)}
		res.Delta.Merge(Practice(Stewardship, true))
		res.Text = fmt.Sprintf("The ledgers balance, more or less. You are **%d gold** richer, and the treasury is poorer.", res.Delta.Wealth)
	case margin > embezzleCaughtMargin:
		res.Delta = Practice(Stewardship, false)
		res.Text = "The clerks were watching too closely today. You took nothing, and nobody noticed you trying."
	default:
		res.Caught = true
		res.Delta = Delta{Renown: -embezzleCaughtRenown}
		res.Delta.Merge(Practice(Stewardship, false))
		res.Text = "A clerk saw you with the strongbox open. The silver went back, and the story went everywhere."
		res.Public = fmt.Sprintf("**%s** was caught with their hand in the treasury. The silver has been returned; their good name has not.", p.Name)
		for _, s := range senators {
			d := embezzleCaughtOpinion
			if s.Has(TraitIncorruptible) || s.Has(TraitEconomical) {
				d = embezzleOutraged
			}
			res.Opinions = append(res.Opinions, OpinionChange{SenatorID: s.ID, CharacterID: p.ID, Delta: d})
		}
	}
	return res
}
