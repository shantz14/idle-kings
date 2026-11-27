package game

import "math"

// ---- death and the heir ----
//
// A murdered player's house goes on: their heir takes the same seat, with
// the house's estate, monuments, ventures and dossier, but with less of
// everything the dead had earned for themselves. The player names the heir.

const (
	DeathDuty        = 0.2 // share of the gold the realm takes
	HeirKeepsRenown  = 0.5
	HeirKeepsInfl    = 0.5
	HeirKeepsTrained = 0.5 // share of stat points above the starting level
	HeirKeepsOpinion = 0.5 // senators' opinions carry over at half
	HeirKeepsSpies   = 0.5 // the dead's agents serve the heir, at half
)

// Heir is what the heir inherits, as the new values.
type Heir struct {
	Name      string
	Stats     Stats
	Renown    float64
	Influence float64
	Wealth    int
}

// Inherit is what passes to the heir of a character with these values.
func Inherit(name string, stats Stats, renown, influence float64, wealth int) Heir {
	h := Heir{
		Name:      name,
		Stats:     Stats{},
		Renown:    math.Round(renown * HeirKeepsRenown),
		Influence: influence * HeirKeepsInfl,
		Wealth:    int(float64(wealth) * (1 - DeathDuty)),
	}
	start := float64(BaseStat * StatScale)
	for _, st := range AllStats {
		v := stats[st]
		if v > start {
			v = start + (v-start)*HeirKeepsTrained
		}
		h.Stats[st] = math.Round(v*4) / 4
	}
	return h
}

// HeirNames are given to an heir until the player names them.
var HeirNames = []string{
	"Aurelia", "Cassius", "Livia", "Decimus", "Octavia", "Lucan", "Junia", "Severus",
	"Flavia", "Quintus", "Valeria", "Tiberia", "Marcellus", "Drusilla", "Gaius", "Cornelia",
}

// PickHeirName picks a given name that isn't the dead's.
func PickHeirName(r Roller, dead string) string {
	for range 8 {
		if n := HeirNames[r.IntN(len(HeirNames))]; n != dead {
			return n
		}
	}
	return HeirNames[0]
}
