package game

// Check describes how a character's sheet turns into a success chance.
// Every roll in the game goes through one of these, so this is the single
// place to make the math more complex later (traits, opinions, rivals...).
// Rolls are never shown to players.
type Check struct {
	Base       float64          // flat chance before attributes, in percent
	Weights    map[Attr]float64 // percent gained per point of each attribute
	Difficulty float64          // flat penalty, in percent
}

const (
	MinChance = 5
	MaxChance = 95
)

// Chance returns the success percent, clamped so nothing is ever certain.
// mod is an extra situational bonus or penalty in percent.
func (c Check) Chance(s Sheet, mod float64) int {
	f := c.Base - c.Difficulty + mod
	for a, w := range c.Weights {
		f += w * s[a]
	}
	n := int(f)
	if n < MinChance {
		return MinChance
	}
	if n > MaxChance {
		return MaxChance
	}
	return n
}

// Roller is the randomness source; *math/rand/v2.Rand satisfies it.
type Roller interface{ IntN(n int) int }

// Mode is how a roll is made.
type Mode int

const (
	Normal Mode = iota
	// Disadvantage rolls twice and keeps the worse result.
	Disadvantage
)

// Margin rolls d100 and returns chance - roll. A positive margin is a
// success; larger margins are bigger successes.
func Margin(r Roller, chance int, mode Mode) int {
	roll := r.IntN(100)
	if mode == Disadvantage {
		if r2 := r.IntN(100); r2 > roll {
			roll = r2
		}
	}
	return chance - roll
}

// Roll makes a check and reports success and margin.
func (c Check) Roll(r Roller, s Sheet, mod float64, mode Mode) (bool, int) {
	m := Margin(r, c.Chance(s, mod), mode)
	return m > 0, m
}
