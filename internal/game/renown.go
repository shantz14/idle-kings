package game

import "math"

// ---- what renown is good for ----
//
// Besides its weight in rolls (parties, speeches, canvassing, hunts,
// courting) and the warmth senators show a famous house (RenownRegard),
// renown adds a little Senate vote weight and opens the doors of the
// grandest houses in the shop. Placeholder numbers — tune freely.

// renownWeightPer is the renown at which vote weight reaches +1; it grows
// with the log after that (+2 at three times as much).
const renownWeightPer = 100.0

// RenownWeight is the Senate vote weight a house's fame gives it.
func RenownWeight(renown float64) float64 {
	return math.Log2(1 + math.Max(renown, 0)/renownWeightPer)
}

// renownNeeded is the renown it takes to buy a property of a tier from the
// shop. Player deals aren't gated: a friend may still sell you a castle.
var renownNeeded = map[Tier]float64{TierGrand: 100, TierPalace: 200, TierCastle: 300}

// RenownNeeded is the renown needed to buy an item from the shop (0 = none).
func RenownNeeded(it Item) float64 {
	if !it.IsProperty() {
		return 0
	}
	return renownNeeded[it.Tier]
}
