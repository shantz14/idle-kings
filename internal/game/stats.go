// Package game holds the pure game rules: stats, roles, actions and checks.
// Nothing in here touches Discord or the database, so it is easy to test and
// to change as the design evolves.
package game

import (
	"fmt"
	"math"
	"strconv"
)

// Stat is one of a character's five core skills.
type Stat string

const (
	Diplomacy   Stat = "diplomacy"
	Martial     Stat = "martial"
	Stewardship Stat = "stewardship" // money
	Intrigue    Stat = "intrigue"
	Prowess     Stat = "prowess"
)

// StatScale is how many shown stat points make one point in a Check. Stats
// are stored and shown on the big scale (characters start at 10, legendary
// gear gives +10 or more); NewSheet divides, so Check weights are per
// StatScale shown points.
const StatScale = 5

// RenownScale is how renown is stored and shown: ×10, in whole numbers, so
// nothing ever gives a player a tenth of a point. Checks see renown divided
// by it (see NewSheet), so their weights are per 10 shown renown.
const RenownScale = 10

// RoundRenown makes a renown change whole: rounded, but never less than 1
// either way, so a small gain or loss still counts.
func RoundRenown(v float64) float64 {
	switch {
	case v == 0:
		return 0
	case v > 0 && v < 1:
		return 1
	case v < 0 && v > -1:
		return -1
	}
	return math.Round(v)
}

// AllStats is the display order for stats.
var AllStats = []Stat{Diplomacy, Martial, Stewardship, Intrigue, Prowess}

// Stats maps each stat to its value. Values are fractional so small gains
// accumulate; missing stats count as 0.
type Stats map[Stat]float64

// Clone returns a copy so callers can mutate without aliasing.
func (s Stats) Clone() Stats {
	out := make(Stats, len(s))
	for k, v := range s {
		out[k] = v
	}
	return out
}

// Add applies a delta in place.
func (s Stats) Add(delta Stats) {
	for k, v := range delta {
		s[k] += v
	}
}

func (s Stats) String() string {
	out := ""
	for i, st := range AllStats {
		if i > 0 {
			out += "  "
		}
		out += fmt.Sprintf("%s %s", st, Num(s[st]))
	}
	return out
}

// Num formats a game number with at most one decimal.
func Num(f float64) string {
	return strconv.FormatFloat(float64(int64(f*10+sign(f)*0.5))/10, 'f', -1, 64)
}

func sign(f float64) float64 {
	if f < 0 {
		return -1
	}
	return 1
}
